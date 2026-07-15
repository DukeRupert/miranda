package handler

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/dukerupert/miranda/internal/coverage"
	"github.com/dukerupert/miranda/internal/domain"
	"github.com/dukerupert/miranda/internal/fixtures"
	"github.com/dukerupert/miranda/internal/materialize"
	"github.com/dukerupert/miranda/internal/store"
	vld "github.com/dukerupert/miranda/internal/validate"
	"github.com/dukerupert/miranda/internal/view"
)

// Explore handles GET /explore: the scheduling-engine explorer. It loads the
// selected scenario's lines/controllers/leave from the store, reads the rule set
// from query params (defaulting to HLN), derives the demand and shift-count
// minimum, validates the scenario's schedule under those rules, and materializes
// its pay period to project the overtime liability.
func Explore(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v := buildScheduleView(r.Context(), st, r)
		if err := view.ExplorePage(v).Render(r.Context(), w); err != nil {
			slog.Error("render error", "err", err)
		}
	}
}

// buildScheduleView runs the whole engine pipeline for the selected scenario and
// current rule-form state.
func buildScheduleView(ctx context.Context, st *store.Store, r *http.Request) view.ScheduleView {
	q := r.URL.Query()
	def := fixtures.HLNRules()
	form := view.ScheduleForm{
		Open:               qstr(q, "open", "0545"),
		Close:              qstr(q, "close", "2210"),
		MinStaff:           qint(q, "min_staff", def.MinStaffWhenOpen),
		MaxTimeOnPositionH: qfloat(q, "max_pos", def.MaxTimeOnPosition.Hours()),
		MaxShiftH:          qfloat(q, "max_shift", def.MaxShiftHours.Hours()),
	}
	v := view.ScheduleView{Form: form}

	scenarios, err := st.ListScenarios(ctx)
	if err != nil {
		v.Err = "load scenarios: " + err.Error()
		return v
	}
	v.Scenarios = scenarios
	if len(scenarios) == 0 {
		return v // empty state; the view offers a "Seed HLN reference" action
	}

	// Resolve the selected scenario, falling back to the first if the query id
	// is absent or stale.
	sel := qint64(q, "scenario", scenarios[0].ID)
	if !scenarioExists(scenarios, sel) {
		sel = scenarios[0].ID
	}
	v.ScenarioID = sel

	data, err := st.LoadScenario(ctx, sel)
	if err != nil {
		v.Err = "load scenario: " + err.Error()
		return v
	}
	v.Data = data

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

	if vios, err := vld.ValidateWeek(data.Lines, data.Occupants, f, rules); err == nil {
		v.Vios = vios
		v.Illegal, v.Warning = vld.Count(vios)
	}

	if pay, err := materialize.Materialize(data.Lines, data.Controllers, data.Leave, data.Scenario.PPStart, f, rules); err == nil {
		v.Pay = pay
	}
	return v
}

// ---- scenario mutations ---------------------------------------------------

// SeedScenario handles POST /explore/seed: create a new scenario populated with
// the HLN reference schedule, then redirect to it.
func SeedScenario(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := st.SeedHLN(r.Context(), "HLN reference")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		redirectToScenario(w, r, id)
	}
}

// CreateScenario handles POST /explore/scenarios: create an empty named scenario.
func CreateScenario(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.FormValue("name")
		if name == "" {
			name = "New scenario"
		}
		id, err := st.CreateScenario(r.Context(), name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		redirectToScenario(w, r, id)
	}
}

// DuplicateScenario handles POST /explore/scenarios/duplicate: deep-copy a
// scenario and redirect to the copy.
func DuplicateScenario(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		src := formInt64(r, "scenario")
		id, err := st.DuplicateScenario(r.Context(), src)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		redirectToScenario(w, r, id)
	}
}

// DeleteScenario handles POST /explore/scenarios/delete: delete a scenario and
// redirect to whatever remains.
func DeleteScenario(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := formInt64(r, "scenario")
		if err := st.DeleteScenario(r.Context(), id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Redirect to the first remaining scenario, or the bare page.
		scenarios, _ := st.ListScenarios(r.Context())
		if len(scenarios) > 0 {
			redirectToScenario(w, r, scenarios[0].ID)
			return
		}
		redirect(w, r, "/explore")
	}
}

// ---- helpers --------------------------------------------------------------

func hours(h float64) time.Duration { return time.Duration(h * float64(time.Hour)) }

func scenarioExists(scenarios []store.Scenario, id int64) bool {
	for _, s := range scenarios {
		if s.ID == id {
			return true
		}
	}
	return false
}

// redirectToScenario issues both HX-Redirect and a plain 303 to /explore for the
// given scenario so htmx and non-JS posts both land on the right page.
func redirectToScenario(w http.ResponseWriter, r *http.Request, id int64) {
	redirect(w, r, "/explore?scenario="+strconv.FormatInt(id, 10))
}

func redirect(w http.ResponseWriter, r *http.Request, url string) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", url)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, url, http.StatusSeeOther)
}

func formInt64(r *http.Request, key string) int64 {
	n, _ := strconv.ParseInt(r.FormValue(key), 10, 64)
	return n
}

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
func qint64(q map[string][]string, key string, def int64) int64 {
	if vs, ok := q[key]; ok && len(vs) > 0 {
		if n, err := strconv.ParseInt(vs[0], 10, 64); err == nil {
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
