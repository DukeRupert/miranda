package handler

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
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

// ---- line mutations -------------------------------------------------------

// SaveLine handles POST /explore/lines: create (line_id=0) or update a line and
// replace its seven day-slots. Each day d reads d{d}_start (HHMM, blank = RDO)
// and d{d}_dur (hours).
func SaveLine(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scenarioID := formInt64(r, "scenario")
		lineID := formInt64(r, "line_id")
		code := strings.TrimSpace(r.FormValue("code"))
		if code == "" {
			code = "New"
		}
		var days [7]*store.Slot
		for d := 0; d < 7; d++ {
			days[d] = parseSlot(r.FormValue(fmt.Sprintf("d%d_start", d)), r.FormValue(fmt.Sprintf("d%d_dur", d)))
		}

		if lineID == 0 {
			id, err := st.CreateLine(r.Context(), scenarioID, code, 1000) // appended after seeded lines
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			lineID = id
		} else if err := st.UpdateLine(r.Context(), lineID, code); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := st.SaveLineDays(r.Context(), lineID, days); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		redirectToScenario(w, r, scenarioID)
	}
}

// DeleteLine handles POST /explore/lines/delete.
func DeleteLine(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scenarioID := formInt64(r, "scenario")
		if err := st.DeleteLine(r.Context(), formInt64(r, "line_id")); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		redirectToScenario(w, r, scenarioID)
	}
}

// parseSlot builds a worked slot from a start (HHMM) and duration (hours). A
// blank/invalid start or a non-positive duration means the day is an RDO (nil).
func parseSlot(startStr, durStr string) *store.Slot {
	startStr = strings.TrimSpace(startStr)
	if startStr == "" {
		return nil
	}
	tod, err := domain.ParseTimeOfDay(startStr)
	if err != nil {
		return nil
	}
	h, err := strconv.ParseFloat(strings.TrimSpace(durStr), 64)
	if err != nil || h <= 0 {
		return nil
	}
	return &store.Slot{StartMin: int(tod), DurationMin: int(math.Round(h * 60))}
}

// ---- controller mutations -------------------------------------------------

// SaveController handles POST /explore/controllers: create (controller_id=0) or
// update a controller's name, AP/LC/CIC quals, and line assignment (line_id=0 =
// unassigned).
func SaveController(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scenarioID := formInt64(r, "scenario")
		ctrlID := formInt64(r, "controller_id")
		name := strings.TrimSpace(r.FormValue("name"))
		if name == "" {
			name = "New"
		}
		ap := formCheck(r, "qual_ap")
		lc := formCheck(r, "qual_lc")
		cic := formCheck(r, "qual_cic")
		lineID := formInt64(r, "line_id")

		var err error
		if ctrlID == 0 {
			_, err = st.CreateController(r.Context(), scenarioID, name, ap, lc, cic, lineID, 1000)
		} else {
			err = st.UpdateController(r.Context(), ctrlID, name, ap, lc, cic, lineID)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		redirectToScenario(w, r, scenarioID)
	}
}

// DeleteController handles POST /explore/controllers/delete.
func DeleteController(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scenarioID := formInt64(r, "scenario")
		if err := st.DeleteController(r.Context(), formInt64(r, "controller_id")); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		redirectToScenario(w, r, scenarioID)
	}
}

// ---- leave mutations ------------------------------------------------------

// CreateLeave handles POST /explore/leave: add a leave entry for a controller.
// The date must be YYYY-MM-DD (guaranteed by the date input); a malformed date
// is rejected rather than persisted so it can't poison scenario loads.
func CreateLeave(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scenarioID := formInt64(r, "scenario")
		controllerID := formInt64(r, "controller_id")
		date := strings.TrimSpace(r.FormValue("leave_date"))
		if _, err := time.Parse("2006-01-02", date); err != nil {
			http.Error(w, "leave date must be YYYY-MM-DD", http.StatusBadRequest)
			return
		}
		h, _ := strconv.ParseFloat(strings.TrimSpace(r.FormValue("hours")), 64)
		if h <= 0 {
			h = 8
		}
		leaveType := r.FormValue("leave_type")
		switch domain.LeaveType(leaveType) {
		case domain.LeaveAnnual, domain.LeaveSick, domain.LeaveBid:
		default:
			leaveType = string(domain.LeaveAnnual)
		}
		if controllerID == 0 {
			http.Error(w, "select a controller", http.StatusBadRequest)
			return
		}
		if err := st.CreateLeave(r.Context(), scenarioID, controllerID, date, int(math.Round(h*60)), leaveType); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		redirectToScenario(w, r, scenarioID)
	}
}

// DeleteLeave handles POST /explore/leave/delete.
func DeleteLeave(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scenarioID := formInt64(r, "scenario")
		if err := st.DeleteLeave(r.Context(), formInt64(r, "leave_id")); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		redirectToScenario(w, r, scenarioID)
	}
}

// ---- helpers --------------------------------------------------------------

func hours(h float64) time.Duration { return time.Duration(h * float64(time.Hour)) }

// formCheck reports whether a checkbox with value "1" was submitted.
func formCheck(r *http.Request, key string) bool { return r.FormValue(key) == "1" }

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
