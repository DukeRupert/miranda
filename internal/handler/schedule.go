package handler

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/dukerupert/miranda/internal/coverage"
	"github.com/dukerupert/miranda/internal/domain"
	"github.com/dukerupert/miranda/internal/fixtures"
	"github.com/dukerupert/miranda/internal/materialize"
	vld "github.com/dukerupert/miranda/internal/validate"
	"github.com/dukerupert/miranda/internal/view"
)

// Explore handles GET /explore: the scheduling-engine proof-of-concept. It reads
// the rule set from query params (defaulting to HLN), derives the demand and
// shift-count minimum, validates the HLN 9-line reference schedule under those
// rules, and materializes a pay period to project the overtime liability.
func Explore() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v := buildScheduleView(r)
		if err := view.ExplorePage(v).Render(r.Context(), w); err != nil {
			slog.Error("render error", "err", err)
		}
	}
}

// buildScheduleView runs the whole engine pipeline for the current form state.
func buildScheduleView(r *http.Request) view.ScheduleView {
	q := r.URL.Query()
	// Default to the HLN fixture; the presence of any query param means the user
	// submitted the form.
	def := fixtures.HLNRules()
	form := view.ScheduleForm{
		Open:               qstr(q, "open", "0545"),
		Close:              qstr(q, "close", "2210"),
		MinStaff:           qint(q, "min_staff", def.MinStaffWhenOpen),
		MaxTimeOnPositionH: qfloat(q, "max_pos", def.MaxTimeOnPosition.Hours()),
		MaxShiftH:          qfloat(q, "max_shift", def.MaxShiftHours.Hours()),
		IncludeLeave:       q.Get("leave") == "1",
	}
	v := view.ScheduleView{Form: form}

	open, err := domain.ParseTimeOfDay(form.Open)
	if err != nil {
		v.Err = "Open time: " + err.Error()
		return v
	}
	close, err := domain.ParseTimeOfDay(form.Close)
	if err != nil {
		v.Err = "Close time: " + err.Error()
		return v
	}
	f, err := domain.NewFacility("HLN", "Helena", open, close, fixtures.HLNFacility().Positions)
	if err != nil {
		v.Err = err.Error()
		return v
	}

	rules := def
	rules.MinStaffWhenOpen = form.MinStaff
	rules.MaxTimeOnPosition = hours(form.MaxTimeOnPositionH)
	rules.MaxShiftHours = hours(form.MaxShiftH)

	demand, err := coverage.ComputeDemand(f, rules)
	if err != nil {
		v.Err = err.Error()
		return v
	}
	minDaily, err := coverage.MinDailyShiftInstances(f, rules)
	if err != nil {
		v.Err = err.Error()
		return v
	}

	v.Facility = f
	v.Rules = rules
	v.Demand = demand
	v.MinDaily = minDaily
	v.WeeklyMin = minDaily * 7

	lines := fixtures.HLNLines()
	occ := fixtures.OccupantQuals()
	if vios, err := vld.ValidateWeek(lines, occ, f, rules); err == nil {
		v.Vios = vios
		v.Illegal, v.Warning = vld.Count(vios)
	}

	// Materialize a pay period starting on the reference Sunday. Optionally place
	// the E1 controller on bid leave for week-1 Mon..Fri to show the OT impact.
	start := domain.Date{Year: 2026, Month: time.July, Day: 12} // a Sunday
	controllers := fixtures.HLNControllers()
	var leave []domain.Leave
	if form.IncludeLeave {
		for _, c := range controllers {
			if c.LineID != nil && *c.LineID == "E1" {
				for i := 1; i <= 5; i++ {
					leave = append(leave, domain.Leave{ControllerID: c.ID, Date: start.AddDays(i), Hours: 8 * time.Hour, Type: domain.LeaveBid})
				}
			}
		}
	}
	if pay, err := materialize.Materialize(lines, controllers, leave, start, f, rules); err == nil {
		v.Pay = pay
	}
	return v
}

func hours(h float64) time.Duration { return time.Duration(h * float64(time.Hour)) }

func qstr(q map[string][]string, key, def string) string {
	if vs, ok := q[key]; ok && len(vs) > 0 && vs[0] != "" {
		return vs[0]
	}
	return def
}
func qint(q map[string][]string, key string, def int) int {
	if vs, ok := q[key]; ok && len(vs) > 0 {
		if n, err := strconv.Atoi(vs[0]); err == nil {
			return n
		}
	}
	return def
}
func qfloat(q map[string][]string, key string, def float64) float64 {
	if vs, ok := q[key]; ok && len(vs) > 0 {
		if n, err := strconv.ParseFloat(vs[0], 64); err == nil {
			return n
		}
	}
	return def
}
