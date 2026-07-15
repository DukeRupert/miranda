package view

import (
	"fmt"
	"strconv"
	"time"

	"github.com/dukerupert/miranda/internal/coverage"
	"github.com/dukerupert/miranda/internal/domain"
	"github.com/dukerupert/miranda/internal/materialize"
	"github.com/dukerupert/miranda/internal/validate"
)

// ScheduleForm is the raw, re-displayable form state for the explorer.
type ScheduleForm struct {
	Open               string
	Close              string
	MinStaff           int
	MaxTimeOnPositionH float64
	MaxShiftH          float64
	IncludeLeave       bool
}

// ScheduleView is the full result the explorer renders: the derived demand, the
// minimum shift count, how the HLN reference schedule validates under the chosen
// rules, and the materialized pay period's projected overtime.
type ScheduleView struct {
	Form      ScheduleForm
	Err       string
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
