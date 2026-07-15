package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/dukerupert/miranda/internal/db"
	"github.com/dukerupert/miranda/internal/domain"
	"github.com/dukerupert/miranda/internal/fixtures"
)

// This file is the persistence layer for the scheduling domain. It maps DB rows
// (integer PKs) to the pure engine's string-keyed domain types and back. The
// engine packages (coverage/validate/materialize) never see the db package;
// they only ever receive domain values assembled here.

const defaultPPStart = "2026-07-12" // a Sunday; the reference pay period start

// Scenario is the metadata for one saved candidate schedule.
type Scenario struct {
	ID      int64
	Name    string
	PPStart domain.Date
}

// LeaveRow is one editable leave entry, carrying its DB id (the domain.Leave
// type has none) so the editor can offer a delete control.
type LeaveRow struct {
	ID           int64
	ControllerID int64
	Date         domain.Date
	Hours        time.Duration
	Type         domain.LeaveType
}

// ScenarioData is everything /explore needs for one scenario: the domain slices
// fed verbatim to the engine, plus the editor-facing metadata (display codes,
// leave row ids) the domain types don't carry.
type ScenarioData struct {
	Scenario    Scenario
	Lines       []domain.Line             // engine input; ID = strconv(line db id)
	LineCodes   map[string]string         // domain line ID -> display code (e.g. "E1")
	Controllers []domain.Controller       // engine input; ID = strconv(controller db id)
	Occupants   map[string]domain.QualSet // line ID -> occupant quals
	Leave       []domain.Leave            // engine input
	LeaveRows   []LeaveRow                // editor rows (carry DB id)
}

// ---- scenario CRUD --------------------------------------------------------

// ListScenarios returns all scenarios ordered by id.
func (s *Store) ListScenarios(ctx context.Context) ([]Scenario, error) {
	rows, err := s.q.ListScenarios(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Scenario, 0, len(rows))
	for _, r := range rows {
		d, err := parseDate(r.PpStart)
		if err != nil {
			return nil, err
		}
		out = append(out, Scenario{ID: r.ID, Name: r.Name, PPStart: d})
	}
	return out, nil
}

// GetScenario returns one scenario's metadata.
func (s *Store) GetScenario(ctx context.Context, id int64) (Scenario, error) {
	r, err := s.q.GetScenario(ctx, id)
	if err != nil {
		return Scenario{}, err
	}
	d, err := parseDate(r.PpStart)
	if err != nil {
		return Scenario{}, err
	}
	return Scenario{ID: r.ID, Name: r.Name, PPStart: d}, nil
}

// CreateScenario inserts an empty scenario with the default pay-period start.
func (s *Store) CreateScenario(ctx context.Context, name string) (int64, error) {
	sc, err := s.q.CreateScenario(ctx, db.CreateScenarioParams{Name: name, PpStart: defaultPPStart})
	if err != nil {
		return 0, err
	}
	return sc.ID, nil
}

// UpdateScenario renames a scenario and sets its pay-period start.
func (s *Store) UpdateScenario(ctx context.Context, id int64, name, ppStart string) error {
	return s.q.UpdateScenario(ctx, db.UpdateScenarioParams{Name: name, PpStart: ppStart, ID: id})
}

// DeleteScenario removes a scenario and (via ON DELETE CASCADE) its lines,
// controllers, and leave.
func (s *Store) DeleteScenario(ctx context.Context, id int64) error {
	return s.q.DeleteScenario(ctx, id)
}

// ---- line CRUD ------------------------------------------------------------

// CreateLine inserts a line and returns its db id.
func (s *Store) CreateLine(ctx context.Context, scenarioID int64, code string, sortOrder int) (int64, error) {
	l, err := s.q.CreateLine(ctx, db.CreateLineParams{ScenarioID: scenarioID, Code: code, SortOrder: int64(sortOrder)})
	if err != nil {
		return 0, err
	}
	return l.ID, nil
}

// UpdateLine renames a line.
func (s *Store) UpdateLine(ctx context.Context, id int64, code string) error {
	// Preserve sort order: read then write. Cheap for these small tables.
	cur, err := s.q.GetLine(ctx, id)
	if err != nil {
		return err
	}
	return s.q.UpdateLine(ctx, db.UpdateLineParams{Code: code, SortOrder: cur.SortOrder, ID: id})
}

// DeleteLine removes a line (its slots cascade; controllers unassign).
func (s *Store) DeleteLine(ctx context.Context, id int64) error {
	return s.q.DeleteLine(ctx, id)
}

// Slot is one worked day of a line, in the units the editor collects.
type Slot struct {
	StartMin    int // minutes since midnight
	DurationMin int
}

// SaveLineDays replaces all of a line's slots. A nil entry in days is an RDO.
func (s *Store) SaveLineDays(ctx context.Context, lineID int64, days [7]*Slot) error {
	if err := s.q.DeleteLineDays(ctx, lineID); err != nil {
		return err
	}
	for i, sl := range days {
		if sl == nil {
			continue
		}
		if err := s.q.CreateLineDay(ctx, db.CreateLineDayParams{
			LineID:      lineID,
			DayIndex:    int64(i),
			StartMin:    int64(sl.StartMin),
			DurationMin: int64(sl.DurationMin),
		}); err != nil {
			return err
		}
	}
	return nil
}

// ---- controller CRUD ------------------------------------------------------

// CreateController inserts a controller and returns its db id. lineID is 0 for
// unassigned.
func (s *Store) CreateController(ctx context.Context, scenarioID int64, name string, ap, lc, cic bool, lineID int64, sortOrder int) (int64, error) {
	c, err := s.q.CreateController(ctx, db.CreateControllerParams{
		ScenarioID: scenarioID,
		Name:       name,
		QualAp:     boolInt(ap),
		QualLc:     boolInt(lc),
		QualCic:    boolInt(cic),
		LineID:     nullInt(lineID),
		SortOrder:  int64(sortOrder),
	})
	if err != nil {
		return 0, err
	}
	return c.ID, nil
}

// UpdateController updates a controller's name, quals, and line assignment.
func (s *Store) UpdateController(ctx context.Context, id int64, name string, ap, lc, cic bool, lineID int64) error {
	return s.q.UpdateController(ctx, db.UpdateControllerParams{
		Name:    name,
		QualAp:  boolInt(ap),
		QualLc:  boolInt(lc),
		QualCic: boolInt(cic),
		LineID:  nullInt(lineID),
		ID:      id,
	})
}

// DeleteController removes a controller (its leave cascades).
func (s *Store) DeleteController(ctx context.Context, id int64) error {
	return s.q.DeleteController(ctx, id)
}

// ---- leave CRUD -----------------------------------------------------------

// CreateLeave inserts a leave entry.
func (s *Store) CreateLeave(ctx context.Context, scenarioID, controllerID int64, date string, hoursMin int, leaveType string) error {
	return s.q.CreateLeave(ctx, db.CreateLeaveParams{
		ScenarioID:   scenarioID,
		ControllerID: controllerID,
		LeaveDate:    date,
		HoursMin:     int64(hoursMin),
		LeaveType:    leaveType,
	})
}

// DeleteLeave removes a leave entry.
func (s *Store) DeleteLeave(ctx context.Context, id int64) error {
	return s.q.DeleteLeave(ctx, id)
}

// ---- load + seed ----------------------------------------------------------

// LoadScenario reads a whole scenario and assembles the engine inputs plus the
// editor metadata. Returns the domain lines/controllers/leave ready to feed to
// coverage/validate/materialize unchanged.
func (s *Store) LoadScenario(ctx context.Context, id int64) (ScenarioData, error) {
	meta, err := s.GetScenario(ctx, id)
	if err != nil {
		return ScenarioData{}, err
	}
	data := ScenarioData{
		Scenario:  meta,
		LineCodes: map[string]string{},
		Occupants: map[string]domain.QualSet{},
	}

	lineRows, err := s.q.ListLinesByScenario(ctx, id)
	if err != nil {
		return ScenarioData{}, err
	}
	dayRows, err := s.q.ListLineDaysByScenario(ctx, id)
	if err != nil {
		return ScenarioData{}, err
	}
	// Group slots by line id.
	slotsByLine := map[int64][7]*domain.ShiftTemplate{}
	for _, d := range dayRows {
		lineID := strconv.FormatInt(d.LineID, 10)
		arr := slotsByLine[d.LineID]
		st := domain.ShiftTemplate{
			ID:       fmt.Sprintf("%s-d%d", lineID, d.DayIndex),
			Start:    domain.TimeOfDay(d.StartMin),
			Duration: time.Duration(d.DurationMin) * time.Minute,
		}
		if d.DayIndex >= 0 && d.DayIndex < 7 {
			arr[d.DayIndex] = &st
			slotsByLine[d.LineID] = arr
		}
	}
	for _, l := range lineRows {
		lineID := strconv.FormatInt(l.ID, 10)
		days := slotsByLine[l.ID]
		data.Lines = append(data.Lines, domain.Line{ID: lineID, Days: days})
		data.LineCodes[lineID] = l.Code
	}

	ctrlRows, err := s.q.ListControllersByScenario(ctx, id)
	if err != nil {
		return ScenarioData{}, err
	}
	for _, c := range ctrlRows {
		cID := strconv.FormatInt(c.ID, 10)
		quals := qualSet(c.QualAp, c.QualLc, c.QualCic)
		var lineIDPtr *string
		if c.LineID.Valid {
			lid := strconv.FormatInt(c.LineID.Int64, 10)
			lineIDPtr = &lid
			data.Occupants[lid] = quals
		}
		data.Controllers = append(data.Controllers, domain.Controller{
			ID:     cID,
			Name:   c.Name,
			Quals:  quals,
			LineID: lineIDPtr,
		})
	}

	leaveRows, err := s.q.ListLeaveByScenario(ctx, id)
	if err != nil {
		return ScenarioData{}, err
	}
	for _, lv := range leaveRows {
		d, err := parseDate(lv.LeaveDate)
		if err != nil {
			return ScenarioData{}, err
		}
		hours := time.Duration(lv.HoursMin) * time.Minute
		data.LeaveRows = append(data.LeaveRows, LeaveRow{
			ID:           lv.ID,
			ControllerID: lv.ControllerID,
			Date:         d,
			Hours:        hours,
			Type:         domain.LeaveType(lv.LeaveType),
		})
		data.Leave = append(data.Leave, domain.Leave{
			ControllerID: strconv.FormatInt(lv.ControllerID, 10),
			Date:         d,
			Hours:        hours,
			Type:         domain.LeaveType(lv.LeaveType),
		})
	}

	return data, nil
}

// SeedHLN creates a new scenario populated with the HLN 9-line reference
// schedule and its nine CPC+CIC controllers (spec §8). It seeds no leave, so the
// scenario materializes to zero projected OT — the clean reference baseline.
// Returns the new scenario id.
func (s *Store) SeedHLN(ctx context.Context, name string) (int64, error) {
	scenarioID, err := s.CreateScenario(ctx, name)
	if err != nil {
		return 0, err
	}

	// Insert lines + slots, mapping each fixture line code to its new db id so
	// controllers can be assigned by the same code.
	codeToLineID := map[string]int64{}
	for i, line := range fixtures.HLNLines() {
		lineID, err := s.CreateLine(ctx, scenarioID, line.ID, i)
		if err != nil {
			return 0, err
		}
		codeToLineID[line.ID] = lineID
		var slots [7]*Slot
		for d, st := range line.Days {
			if st == nil {
				continue
			}
			slots[d] = &Slot{
				StartMin:    int(st.Start),
				DurationMin: int(st.Duration / time.Minute),
			}
		}
		if err := s.SaveLineDays(ctx, lineID, slots); err != nil {
			return 0, err
		}
	}

	for i, c := range fixtures.HLNControllers() {
		var lineID int64
		if c.LineID != nil {
			lineID = codeToLineID[*c.LineID] // 0 if not found -> unassigned
		}
		if _, err := s.CreateController(ctx, scenarioID, c.Name,
			c.Quals[domain.CapAP], c.Quals[domain.CapLC], c.Quals[domain.CapCIC],
			lineID, i); err != nil {
			return 0, err
		}
	}

	return scenarioID, nil
}

// DuplicateScenario deep-copies a scenario (lines + slots + controllers + leave)
// into a new one named "<name> (copy)" and returns the new id.
func (s *Store) DuplicateScenario(ctx context.Context, srcID int64) (int64, error) {
	src, err := s.GetScenario(ctx, srcID)
	if err != nil {
		return 0, err
	}
	newID, err := s.CreateScenario(ctx, src.Name+" (copy)")
	if err != nil {
		return 0, err
	}
	if err := s.UpdateScenario(ctx, newID, src.Name+" (copy)", src.PPStart.String()); err != nil {
		return 0, err
	}

	lineRows, err := s.q.ListLinesByScenario(ctx, srcID)
	if err != nil {
		return 0, err
	}
	dayRows, err := s.q.ListLineDaysByScenario(ctx, srcID)
	if err != nil {
		return 0, err
	}
	slotsByLine := map[int64][7]*Slot{}
	for _, d := range dayRows {
		if d.DayIndex < 0 || d.DayIndex >= 7 {
			continue
		}
		arr := slotsByLine[d.LineID]
		arr[d.DayIndex] = &Slot{StartMin: int(d.StartMin), DurationMin: int(d.DurationMin)}
		slotsByLine[d.LineID] = arr
	}
	oldToNewLine := map[int64]int64{}
	for i, l := range lineRows {
		nl, err := s.CreateLine(ctx, newID, l.Code, i)
		if err != nil {
			return 0, err
		}
		oldToNewLine[l.ID] = nl
		if slots, ok := slotsByLine[l.ID]; ok {
			if err := s.SaveLineDays(ctx, nl, slots); err != nil {
				return 0, err
			}
		}
	}

	ctrlRows, err := s.q.ListControllersByScenario(ctx, srcID)
	if err != nil {
		return 0, err
	}
	oldToNewCtrl := map[int64]int64{}
	for i, c := range ctrlRows {
		var lineID int64
		if c.LineID.Valid {
			lineID = oldToNewLine[c.LineID.Int64]
		}
		nc, err := s.CreateController(ctx, newID, c.Name,
			c.QualAp != 0, c.QualLc != 0, c.QualCic != 0, lineID, i)
		if err != nil {
			return 0, err
		}
		oldToNewCtrl[c.ID] = nc
	}

	leaveRows, err := s.q.ListLeaveByScenario(ctx, srcID)
	if err != nil {
		return 0, err
	}
	for _, lv := range leaveRows {
		if err := s.CreateLeave(ctx, newID, oldToNewCtrl[lv.ControllerID], lv.LeaveDate, int(lv.HoursMin), lv.LeaveType); err != nil {
			return 0, err
		}
	}

	return newID, nil
}

// ---- helpers --------------------------------------------------------------

func parseDate(s string) (domain.Date, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return domain.Date{}, fmt.Errorf("parse date %q: %w", s, err)
	}
	return domain.Date{Year: t.Year(), Month: t.Month(), Day: t.Day()}, nil
}

func qualSet(ap, lc, cic int64) domain.QualSet {
	q := domain.QualSet{}
	if ap != 0 {
		q[domain.CapAP] = true
	}
	if lc != 0 {
		q[domain.CapLC] = true
	}
	if cic != 0 {
		q[domain.CapCIC] = true
	}
	return q
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// nullInt maps 0 to a NULL line assignment (unassigned).
func nullInt(v int64) sql.NullInt64 {
	if v == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: v, Valid: true}
}
