// Package materialize turns the repeating lines + controller assignments +
// leave into a concrete two-week pay-period schedule with a gap report and a
// projected-overtime figure (spec §6). Leave never mutates a line; it vacates
// specific dated shift-instances and creates gaps carrying the qualification a
// replacement must hold, so the callout list is pre-filtered to controllers who
// can legally take the shift.
package materialize

import (
	"fmt"
	"time"

	"github.com/dukerupert/miranda/internal/coverage"
	"github.com/dukerupert/miranda/internal/domain"
)

// PayPeriodDays is the length of a pay period.
const PayPeriodDays = 14

// ScheduledShift is one body on the clock for one dated shift.
type ScheduledShift struct {
	Date         domain.Date          `json:"date"`
	ControllerID string               `json:"controller_id"`
	Template     domain.ShiftTemplate `json:"template"`
	Source       string               `json:"source"` // "line" | "exception"
}

// Gap is a vacated or unfilled shift-instance. Requires carries the minimum
// quals a replacement must hold. Absorbed is true when the day still satisfies
// coverage without this instance (an overlap covered it), so it needs no OT.
type Gap struct {
	Date     domain.Date          `json:"date"`
	LineID   string               `json:"line_id"`
	Template domain.ShiftTemplate `json:"template"`
	Requires domain.QualSet       `json:"requires"`
	Reason   string               `json:"reason"` // "leave:annual" | "leave:bid" | "leave:sick" | "unassigned-line"
	Absorbed bool                 `json:"absorbed"`
}

// UncoveredBand is a time slice on a date where actual coverage falls below
// demand after leave is applied. Deficit is the headcount short; Requires is the
// capability shortfall, for pre-filtering the callout list.
type UncoveredBand struct {
	Date     domain.Date      `json:"date"`
	Start    domain.TimeOfDay `json:"start"`
	End      domain.TimeOfDay `json:"end"`
	Deficit  int              `json:"deficit"`
	Requires domain.QualSet   `json:"requires"`
}

// Duration is the length of the band.
func (u UncoveredBand) Duration() time.Duration { return u.End.Sub(u.Start) }

// PayPeriod is the materialized schedule for one 14-day period.
type PayPeriod struct {
	Start       domain.Date      `json:"start"`
	Shifts      []ScheduledShift `json:"shifts"`
	Gaps        []Gap            `json:"gaps"`
	Uncovered   []UncoveredBand  `json:"uncovered"`
	ProjectedOT time.Duration    `json:"projected_ot"` // person-hours of uncovered demand
}

// Materialize builds the pay period. ProjectedOT is the person-hours of
// uncovered demand (Σ deficit × band duration) — the minimum overtime coverage
// the facility must buy to fill the holes. Note the projected OT includes any
// *structural* holes the base schedule already carries (e.g. HLN's mid-day
// handoff dips), not only leave-induced ones; that is the honest total liability.
func Materialize(
	lines []domain.Line,
	controllers []domain.Controller,
	leave []domain.Leave,
	start domain.Date,
	f domain.Facility,
	r domain.RuleSet,
) (PayPeriod, error) {
	// Validate rules / operating window up front (same gate ComputeDemand uses).
	if _, err := coverage.ComputeDemand(f, r); err != nil {
		return PayPeriod{}, err
	}

	controllerByLine := map[string]domain.Controller{}
	occupants := map[string]domain.QualSet{}
	for _, c := range controllers {
		if c.LineID != nil {
			controllerByLine[*c.LineID] = c
			occupants[*c.LineID] = c.Quals
		}
	}
	req, err := coverage.LineQualRequirements(lines, occupants, f, r)
	if err != nil {
		return PayPeriod{}, err
	}

	onLeave := map[string]map[domain.Date]domain.LeaveType{}
	for _, lv := range leave {
		if onLeave[lv.ControllerID] == nil {
			onLeave[lv.ControllerID] = map[domain.Date]domain.LeaveType{}
		}
		onLeave[lv.ControllerID][lv.Date] = lv.Type
	}

	pp := PayPeriod{Start: start}
	for i := 0; i < PayPeriodDays; i++ {
		date := start.AddDays(i)
		wd := int(date.Weekday())

		var working []coverage.WorkingShift
		for _, line := range lines {
			s := line.Days[wd]
			if s == nil {
				continue
			}
			ctrl, assigned := controllerByLine[line.ID]
			if !assigned {
				pp.Gaps = append(pp.Gaps, Gap{Date: date, LineID: line.ID, Template: *s, Requires: req[line.ID], Reason: "unassigned-line"})
				continue
			}
			if lt, off := onLeave[ctrl.ID][date]; off {
				pp.Gaps = append(pp.Gaps, Gap{Date: date, LineID: line.ID, Template: *s, Requires: req[line.ID], Reason: "leave:" + string(lt)})
				continue
			}
			working = append(working, coverage.WorkingShift{LineID: line.ID, Template: *s, Quals: ctrl.Quals})
			pp.Shifts = append(pp.Shifts, ScheduledShift{Date: date, ControllerID: ctrl.ID, Template: *s, Source: "line"})
		}

		// Actual coverage after leave/vacancies -> uncovered bands + OT. A snapshot
		// gap costs deficit bodies for its whole span; a cap-breach run costs one
		// relief body for the time it runs over the on-position cap.
		for _, g := range coverage.DayCoverageGaps(working, f, r) {
			otSpan := g.Duration()
			switch g.Kind {
			case coverage.GapCapTotal, coverage.GapCapAP, coverage.GapCapCIC:
				otSpan = g.Duration() - r.MaxTimeOnPosition // excess over the cap
			}
			pp.Uncovered = append(pp.Uncovered, UncoveredBand{
				Date:     date,
				Start:    g.Start,
				End:      g.End,
				Deficit:  g.Deficit(),
				Requires: capsShort(g.Missing),
			})
			pp.ProjectedOT += time.Duration(g.Deficit()) * otSpan
		}
	}

	// A vacated/unfilled instance is "absorbed" when its window does not overlap
	// any uncovered band on its date: the remaining bodies covered it.
	for i := range pp.Gaps {
		pp.Gaps[i].Absorbed = !overlapsUncovered(pp.Gaps[i], pp.Uncovered)
	}
	return pp, nil
}

// capsShort turns a coverage shortfall vector into the QualSet a replacement
// must hold (ignoring the pseudo total-headcount key).
func capsShort(missing map[domain.Capability]int) domain.QualSet {
	out := domain.QualSet{}
	for c, n := range missing {
		if c == coverage.TotalKey || n <= 0 {
			continue
		}
		out[c] = true
	}
	return out
}

// overlapsUncovered reports whether the gap's shift window intersects any
// uncovered band on the same date.
func overlapsUncovered(g Gap, bands []UncoveredBand) bool {
	for _, b := range bands {
		if b.Date != g.Date {
			continue
		}
		if g.Template.Start < b.End && b.Start < g.Template.End() {
			return true
		}
	}
	return false
}

// Summary is a compact human line for logs/UI.
func (pp PayPeriod) Summary() string {
	absorbed := 0
	for _, g := range pp.Gaps {
		if g.Absorbed {
			absorbed++
		}
	}
	return fmt.Sprintf("%d shifts, %d gaps (%d absorbed), %d uncovered bands, %s projected OT",
		len(pp.Shifts), len(pp.Gaps), absorbed, len(pp.Uncovered), pp.ProjectedOT)
}
