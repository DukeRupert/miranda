package domain

import (
	"fmt"
	"time"
)

// Position is a working station that must be staffed while the facility is open.
type Position struct {
	ID   string `json:"id"`   // "AP", "LC"
	Name string `json:"name"` // human label
	// Requires is the capability a body must hold to work this position. For a
	// facility with positions AP and LC, this is CapAP / CapLC respectively.
	Requires Capability `json:"requires"`
}

// Facility is the physical facility: operating hours and the set of positions
// that must be staffed while open.
type Facility struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	OpenTime  TimeOfDay  `json:"open_time"`  // e.g. 0545
	CloseTime TimeOfDay  `json:"close_time"` // e.g. 2210; must be > OpenTime (no overnight in v1)
	Positions []Position `json:"positions"`
}

// NewFacility validates the invariants a Facility must uphold: at least one
// position and a same-day operating window (CloseTime strictly after
// OpenTime). Overnight facilities are out of scope for v1 and return an error
// rather than silently producing a negative-length day.
func NewFacility(id, name string, open, close TimeOfDay, positions []Position) (Facility, error) {
	if close <= open {
		return Facility{}, fmt.Errorf("facility %s: close time %s must be after open time %s (overnight not supported)", id, close, open)
	}
	if open < 0 || close >= MinutesPerDay {
		return Facility{}, fmt.Errorf("facility %s: operating window %s-%s out of range", id, open, close)
	}
	if len(positions) == 0 {
		return Facility{}, fmt.Errorf("facility %s: at least one position required", id)
	}
	return Facility{ID: id, Name: name, OpenTime: open, CloseTime: close, Positions: positions}, nil
}

// OpenDuration is the length of the operating day.
func (f Facility) OpenDuration() time.Duration { return f.CloseTime.Sub(f.OpenTime) }

// PositionsRequiring returns how many positions require capability c. This is
// the derived source of per-capability staffing floors — the engine never
// hard-codes "1 AP position", it counts them here.
func (f Facility) PositionsRequiring(c Capability) int {
	n := 0
	for _, p := range f.Positions {
		if p.Requires == c {
			n++
		}
	}
	return n
}

// RuleSet is the parameterized labor constraint set. It is DATA — versionable
// per facility — and every derivation in the coverage engine must trace back to
// one of these fields rather than a baked-in constant.
type RuleSet struct {
	MaxShiftHours       time.Duration `json:"max_shift_hours"`        // e.g. 10h
	MaxDaysPerWindow    int           `json:"max_days_per_window"`    // e.g. 6
	WindowDays          int           `json:"window_days"`            // e.g. 7 (max 6 worked in any rolling 7)
	RequiredHoursPerPP  time.Duration `json:"required_hours_per_pp"`  // e.g. 80h per two-week PP, incl. paid leave
	MaxTimeOnPosition   time.Duration `json:"max_time_on_position"`   // e.g. 2h
	MinBreak            time.Duration `json:"min_break"`              // e.g. 15m
	MinStaffWhenOpen    int           `json:"min_staff_when_open"`    // e.g. 2
	TurnaroundWarnHours time.Duration `json:"turnaround_warn_hours"`  // e.g. 10h; rest below this is a warning
	PositionSwapIsBreak bool          `json:"position_swap_is_break"` // facility interpretation flag; see coverage §4.4
}

// Validate checks a RuleSet for internally coherent values.
func (r RuleSet) Validate() error {
	if r.MaxShiftHours <= 0 {
		return fmt.Errorf("ruleset: max shift hours must be positive")
	}
	if r.MaxTimeOnPosition <= 0 {
		return fmt.Errorf("ruleset: max time on position must be positive")
	}
	if r.MinStaffWhenOpen < 1 {
		return fmt.Errorf("ruleset: min staff when open must be at least 1")
	}
	if r.WindowDays < 1 || r.MaxDaysPerWindow < 1 || r.MaxDaysPerWindow > r.WindowDays {
		return fmt.Errorf("ruleset: invalid work-window (%d of %d)", r.MaxDaysPerWindow, r.WindowDays)
	}
	return nil
}

// ShiftTemplate is a start time plus a duration — the reusable shape of a shift.
type ShiftTemplate struct {
	ID       string        `json:"id"`       // e.g. "E8" or "0545x8"
	Start    TimeOfDay     `json:"start"`    // e.g. 0545
	Duration time.Duration `json:"duration"` // e.g. 8h
}

// NewShiftTemplate validates that the duration is positive.
func NewShiftTemplate(id string, start TimeOfDay, dur time.Duration) (ShiftTemplate, error) {
	if dur <= 0 {
		return ShiftTemplate{}, fmt.Errorf("shift template %s: duration must be positive", id)
	}
	return ShiftTemplate{ID: id, Start: start, Duration: dur}, nil
}

// End is the time the shift finishes (Start + Duration).
func (s ShiftTemplate) End() TimeOfDay { return s.Start.Add(s.Duration) }

// Line is a fixed repeating weekly pattern of 7 slots. Index 0 is Sunday to
// match time.Weekday. A nil slot is an RDO (regular day off). Lines are the
// negotiated contractual artifact and are immutable per posting cycle.
type Line struct {
	ID   string            `json:"id"`
	Days [7]*ShiftTemplate `json:"days"`
}

// WorkedDays returns the number of non-RDO slots in the week.
func (l Line) WorkedDays() int {
	n := 0
	for _, s := range l.Days {
		if s != nil {
			n++
		}
	}
	return n
}

// Controller is a person with a qualification set, optionally assigned to a Line.
type Controller struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Quals  QualSet `json:"quals"`
	LineID *string `json:"line_id"` // nil = unassigned
}

// LeaveType classifies an absence.
type LeaveType string

const (
	LeaveAnnual LeaveType = "annual"
	LeaveSick   LeaveType = "sick"
	LeaveBid    LeaveType = "bid" // known at schedule-build time
)

// Leave is an input to materialization. Paid leave counts toward
// RequiredHoursPerPP. Leave never mutates a Line; it creates an exception
// against the materialized schedule.
type Leave struct {
	ControllerID string        `json:"controller_id"`
	Date         Date          `json:"date"`
	Hours        time.Duration `json:"hours"`
	Type         LeaveType     `json:"type"`
}
