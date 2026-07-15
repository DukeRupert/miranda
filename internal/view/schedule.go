package view

import (
	"fmt"
	"strconv"
	"time"

	"github.com/dukerupert/miranda/internal/coverage"
	"github.com/dukerupert/miranda/internal/domain"
	"github.com/dukerupert/miranda/internal/materialize"
	"github.com/dukerupert/miranda/internal/store"
	"github.com/dukerupert/miranda/internal/validate"
)

// ScheduleForm is the raw, re-displayable form state for the explorer's rule set.
type ScheduleForm struct {
	Open               string
	Close              string
	MinStaff           int
	MaxTimeOnPositionH float64
	MaxShiftH          float64
}

// ScheduleView is the full result the explorer renders: the selected scenario's
// editable schedule, the derived demand, the minimum shift count, how the
// scenario validates under the chosen rules, and the materialized pay period's
// projected overtime.
type ScheduleView struct {
	Form ScheduleForm
	Err  string

	// Scenario selection + editable data.
	Scenarios  []store.Scenario
	ScenarioID int64
	Data       store.ScenarioData

	Facility  domain.Facility
	Rules     domain.RuleSet
	Demand    coverage.DemandTimeline
	MinDaily  int
	WeeklyMin int
	Vios      []validate.Violation
	Illegal   int
	Warning   int
	Pay       materialize.PayPeriod
}

// HasScenarios reports whether any scenario exists (drives the empty state).
func (v ScheduleView) HasScenarios() bool { return len(v.Scenarios) > 0 }

// Hours renders a duration compactly, e.g. "12h25m" or "3h20m".
func Hours(d time.Duration) string {
	d = d.Round(time.Minute)
	h := int(d / time.Hour)
	m := int((d % time.Hour) / time.Minute)
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh%02dm", h, m)
}

// CapVec renders a MinCapable map in a stable AP/LC/CIC order.
func CapVec(m map[domain.Capability]int) string {
	out := ""
	for _, c := range []domain.Capability{domain.CapAP, domain.CapLC, domain.CapCIC} {
		if n, ok := m[c]; ok {
			if out != "" {
				out += " "
			}
			out += fmt.Sprintf("%s:%d", c, n)
		}
	}
	if out == "" {
		return "—"
	}
	return out
}

// QualLabel renders a QualSet as e.g. "AP·LC·CIC" or "—".
func QualLabel(q domain.QualSet) string {
	out := ""
	for _, c := range []domain.Capability{domain.CapAP, domain.CapLC, domain.CapCIC} {
		if q[c] {
			if out != "" {
				out += "·"
			}
			out += string(c)
		}
	}
	if out == "" {
		return "—"
	}
	return out
}

// SeverityClass maps a severity to a Tailwind text color.
func SeverityClass(s validate.Severity) string {
	if s == validate.Illegal {
		return "text-red-400"
	}
	return "text-amber-300"
}

// AbsorbedGaps / OpenGaps split the pay-period gaps for display.
func (v ScheduleView) OpenGapCount() int {
	n := 0
	for _, g := range v.Pay.Gaps {
		if !g.Absorbed {
			n++
		}
	}
	return n
}

// Weekdays is Sun..Sat, index-aligned to Line.Days and Date.Weekday.
var Weekdays = [7]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

// trim renders a float without trailing zeros (2.00 -> "2", 2.25 -> "2.25").
func trim(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// hiddenField is a name/value pair rendered as a hidden input inside a small
// standalone POST form (see the postButton component).
type hiddenField struct {
	Name  string
	Value string
}

// scenarioField carries the current scenario id as a hidden form field.
func scenarioField(id int64) hiddenField {
	return hiddenField{Name: "scenario", Value: strconv.FormatInt(id, 10)}
}

// scenarioPPStart is the selected scenario's pay-period start, for display.
func scenarioPPStart(v ScheduleView) string {
	for _, sc := range v.Scenarios {
		if sc.ID == v.ScenarioID {
			return sc.PPStart.String()
		}
	}
	return "—"
}

// postButtonClass maps a button variant to Tailwind classes.
func postButtonClass(variant string) string {
	base := "rounded-sm border px-3 py-2 text-[12px] font-medium transition "
	switch variant {
	case "danger":
		return base + "border-red-500/40 text-red-300 hover:border-red-500 hover:text-red-200"
	case "ghost":
		return base + "border-ff-border text-ff-cream hover:border-ff-ember"
	default:
		return base + "border-ff-border text-ff-cream hover:border-ff-ember"
	}
}
