// Package validate holds the schedule validators (spec §5). Each validator is a
// pure function over proposed lines + assignments that returns structured
// Violations — never a bare boolean or a panic — so a caller (the web UI, a CLI,
// a test) can render exactly what is wrong and how far off it is.
package validate

import (
	"fmt"
	"time"

	"github.com/dukerupert/miranda/internal/coverage"
	"github.com/dukerupert/miranda/internal/domain"
)

// Severity classifies a violation.
type Severity string

const (
	Illegal Severity = "illegal" // violates a federal/contract rule
	Warning Severity = "warning" // legal but flagged (e.g. short turnaround)
)

// Violation is a single finding. Detail carries machine-readable specifics
// (required vs present vectors, deltas) so the finding is actionable.
type Violation struct {
	Rule     string                   `json:"rule"`
	Severity Severity                 `json:"severity"`
	LineID   string                   `json:"line_id,omitempty"`
	Date     *domain.Date             `json:"date,omitempty"`
	Interval *coverage.DemandInterval `json:"interval,omitempty"`
	Message  string                   `json:"message"`
	Detail   map[string]any           `json:"detail,omitempty"`
}

// weekdayName is for human-readable messages; index 0 = Sunday.
var weekdayName = [7]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

// ValidateWeek runs every structural validator against the repeating weekly
// pattern: shift length, coverage (per weekday), six-of-seven, line
// qualification, and turnaround. occupants maps line ID -> the assigned
// controller's quals (used by coverage and line-qualification). The returned
// slice is ordered Illegal-before-Warning within each rule group.
func ValidateWeek(lines []domain.Line, occupants map[string]domain.QualSet, f domain.Facility, r domain.RuleSet) ([]Violation, error) {
	dt, err := coverage.ComputeDemand(f, r)
	if err != nil {
		return nil, err
	}

	var vs []Violation
	vs = append(vs, shiftLengthViolations(lines, r)...)
	vs = append(vs, coverageViolations(lines, occupants, f, r, dt)...)
	vs = append(vs, sixOfSevenViolations(lines, r)...)

	lineViol, err := lineQualificationViolations(lines, occupants, f, r)
	if err != nil {
		return nil, err
	}
	vs = append(vs, lineViol...)
	vs = append(vs, turnaroundViolations(lines, r)...)
	return vs, nil
}

// shiftLengthViolations: every template must be no longer than MaxShiftHours.
func shiftLengthViolations(lines []domain.Line, r domain.RuleSet) []Violation {
	var vs []Violation
	for _, line := range lines {
		seen := map[string]bool{}
		for day, s := range line.Days {
			if s == nil || seen[s.ID] {
				continue
			}
			if s.Duration > r.MaxShiftHours {
				seen[s.ID] = true
				vs = append(vs, Violation{
					Rule:     "max-shift-hours",
					Severity: Illegal,
					LineID:   line.ID,
					Message:  fmt.Sprintf("line %s: shift %s is %s, exceeds max %s (first on %s)", line.ID, s.ID, s.Duration, r.MaxShiftHours, weekdayName[day]),
					Detail:   map[string]any{"template": s.ID, "duration_h": s.Duration.Hours(), "max_h": r.MaxShiftHours.Hours()},
				})
			}
		}
	}
	return vs
}

// coverageViolations: for each weekday, the union of on-duty lines must satisfy
// the demand at every minute. Each gap becomes one Illegal with the missing
// capability vector in Detail.
func coverageViolations(lines []domain.Line, occupants map[string]domain.QualSet, f domain.Facility, r domain.RuleSet, dt coverage.DemandTimeline) []Violation {
	var vs []Violation
	for day := 0; day < 7; day++ {
		var working []coverage.WorkingShift
		for _, line := range lines {
			if s := line.Days[day]; s != nil {
				q := occupants[line.ID]
				if q == nil {
					q = domain.CPC(true) // unassigned line assumed best-case for coverage
				}
				working = append(working, coverage.WorkingShift{LineID: line.ID, Template: *s, Quals: q})
			}
		}
		for _, g := range coverage.DayCoverageGaps(working, f, r, dt) {
			gap := g
			missing := map[string]any{}
			for c, n := range gap.Missing {
				missing[string(c)] = n
			}
			vs = append(vs, Violation{
				Rule:     "coverage-gap",
				Severity: Illegal,
				Interval: &gap.Demand,
				Message:  fmt.Sprintf("%s %s-%s: %d present, demand %d (short %v)", weekdayName[day], gap.Start, gap.End, gap.PresentN, gap.Demand.MinTotal, missing),
				Detail: map[string]any{
					"weekday": weekdayName[day],
					"start":   gap.Start.String(),
					"end":     gap.End.String(),
					"present": gap.PresentN,
					"missing": missing,
				},
			})
		}
	}
	return vs
}

// sixOfSevenViolations: no line works more than MaxDaysPerWindow days in any
// rolling WindowDays window. Evaluated over the doubled pattern so a run that
// straddles the Sat->Sun boundary of the repeating week is caught.
func sixOfSevenViolations(lines []domain.Line, r domain.RuleSet) []Violation {
	var vs []Violation
	for _, line := range lines {
		var worked [14]bool
		for i := 0; i < 14; i++ {
			worked[i] = line.Days[i%7] != nil
		}
		worst := 0
		for start := 0; start+r.WindowDays <= 14; start++ {
			c := 0
			for i := start; i < start+r.WindowDays; i++ {
				if worked[i] {
					c++
				}
			}
			if c > worst {
				worst = c
			}
		}
		if worst > r.MaxDaysPerWindow {
			vs = append(vs, Violation{
				Rule:     "six-of-seven",
				Severity: Illegal,
				LineID:   line.ID,
				Message:  fmt.Sprintf("line %s works %d days in a rolling %d-day window (max %d)", line.ID, worst, r.WindowDays, r.MaxDaysPerWindow),
				Detail:   map[string]any{"worst_in_window": worst, "window_days": r.WindowDays, "max": r.MaxDaysPerWindow},
			})
		}
	}
	return vs
}

// lineQualificationViolations: each assigned controller's quals must be a
// superset of their line's computed requirement.
func lineQualificationViolations(lines []domain.Line, occupants map[string]domain.QualSet, f domain.Facility, r domain.RuleSet) ([]Violation, error) {
	req, err := coverage.LineQualRequirements(lines, occupants, f, r)
	if err != nil {
		return nil, err
	}
	var vs []Violation
	for _, line := range lines {
		occ, ok := occupants[line.ID]
		if !ok {
			continue // unassigned line: no controller to check
		}
		need := req[line.ID]
		if !occ.Superset(need) {
			vs = append(vs, Violation{
				Rule:     "line-qualification",
				Severity: Illegal,
				LineID:   line.ID,
				Message:  fmt.Sprintf("line %s occupant lacks required quals: needs %v, holds %v", line.ID, keys(need), keys(occ)),
				Detail:   map[string]any{"required": keys(need), "held": keys(occ)},
			})
		}
	}
	return vs, nil
}

// turnaroundViolations (Warning): rest between a line's consecutive shifts below
// TurnaroundWarnHours. Evaluated around the week (Sat->Sun wraps).
func turnaroundViolations(lines []domain.Line, r domain.RuleSet) []Violation {
	if r.TurnaroundWarnHours <= 0 {
		return nil
	}
	var vs []Violation
	for _, line := range lines {
		for d := 0; d < 7; d++ {
			cur, nxt := line.Days[d], line.Days[(d+1)%7]
			if cur == nil || nxt == nil {
				continue
			}
			restMin := (domain.MinutesPerDay - int(cur.End())) + int(nxt.Start)
			rest := time.Duration(restMin) * time.Minute
			if rest < r.TurnaroundWarnHours {
				vs = append(vs, Violation{
					Rule:     "turnaround",
					Severity: Warning,
					LineID:   line.ID,
					Message:  fmt.Sprintf("line %s %s(%s)->%s(%s): %s rest, below %s", line.ID, weekdayName[d], cur.ID, weekdayName[(d+1)%7], nxt.ID, rest, r.TurnaroundWarnHours),
					Detail:   map[string]any{"rest_h": rest.Hours(), "threshold_h": r.TurnaroundWarnHours.Hours(), "from": cur.ID, "to": nxt.ID},
				})
			}
		}
	}
	return vs
}

func keys(q domain.QualSet) []string {
	var out []string
	for _, c := range []domain.Capability{domain.CapAP, domain.CapLC, domain.CapCIC} {
		if q[c] {
			out = append(out, string(c))
		}
	}
	return out
}

// Count returns how many violations of each severity are present.
func Count(vs []Violation) (illegal, warning int) {
	for _, v := range vs {
		switch v.Severity {
		case Illegal:
			illegal++
		case Warning:
			warning++
		}
	}
	return
}
