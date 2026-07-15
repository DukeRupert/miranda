package validate

import (
	"fmt"
	"time"

	"github.com/dukerupert/miranda/internal/domain"
)

// ValidateControllerHours checks the biweekly-hours rule (spec §5.4): for each
// assigned controller over the 14-day pay period starting at `start`, scheduled
// hours plus paid leave must equal RequiredHoursPerPP exactly. A day the
// controller is on leave contributes leave hours instead of the shift's
// scheduled hours (leave never mutates the line; it substitutes at
// materialization/accounting time).
func ValidateControllerHours(
	controllers []domain.Controller,
	linesByID map[string]domain.Line,
	leave []domain.Leave,
	start domain.Date,
	r domain.RuleSet,
) []Violation {
	// Index leave by controller+date for O(1) lookup, and total paid leave hours.
	onLeave := map[string]map[domain.Date]bool{}
	leaveHours := map[string]time.Duration{}
	for _, lv := range leave {
		if onLeave[lv.ControllerID] == nil {
			onLeave[lv.ControllerID] = map[domain.Date]bool{}
		}
		onLeave[lv.ControllerID][lv.Date] = true
		leaveHours[lv.ControllerID] += lv.Hours // annual/bid/sick all treated as paid
	}

	var vs []Violation
	for _, c := range controllers {
		if c.LineID == nil {
			continue
		}
		line, ok := linesByID[*c.LineID]
		if !ok {
			continue
		}
		var scheduled time.Duration
		for i := 0; i < 14; i++ {
			d := start.AddDays(i)
			s := line.Days[int(d.Weekday())]
			if s == nil {
				continue
			}
			if onLeave[c.ID][d] {
				continue // leave substitutes for this shift's scheduled hours
			}
			scheduled += s.Duration
		}
		total := scheduled + leaveHours[c.ID]
		if total != r.RequiredHoursPerPP {
			delta := total - r.RequiredHoursPerPP
			vs = append(vs, Violation{
				Rule:     "biweekly-hours",
				Severity: Illegal,
				LineID:   *c.LineID,
				Message:  fmt.Sprintf("controller %s: %s scheduled + %s leave = %s, requires %s (delta %s)", c.ID, scheduled, leaveHours[c.ID], total, r.RequiredHoursPerPP, signed(delta)),
				Detail: map[string]any{
					"controller":  c.ID,
					"scheduled_h": scheduled.Hours(),
					"leave_h":     leaveHours[c.ID].Hours(),
					"required_h":  r.RequiredHoursPerPP.Hours(),
					"delta_h":     delta.Hours(),
				},
			})
		}
	}
	return vs
}

func signed(d time.Duration) string {
	if d >= 0 {
		return "+" + d.String()
	}
	return d.String()
}
