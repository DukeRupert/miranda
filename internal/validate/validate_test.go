package validate_test

import (
	"testing"
	"time"

	"github.com/dukerupert/miranda/internal/domain"
	"github.com/dukerupert/miranda/internal/fixtures"
	"github.com/dukerupert/miranda/internal/validate"
)

func tod(s string) domain.TimeOfDay { return domain.MustParseTimeOfDay(s) }

// byRule returns the violations matching a rule name.
func byRule(vs []validate.Violation, rule string) []validate.Violation {
	var out []validate.Violation
	for _, v := range vs {
		if v.Rule == rule {
			out = append(out, v)
		}
	}
	return out
}

// V1: an 11h template triggers max-shift-hours Illegal.
func TestV1_ShiftLength(t *testing.T) {
	f, r := fixtures.HLNFacility(), fixtures.HLNRules()
	long := domain.ShiftTemplate{ID: "X11", Start: tod("0600"), Duration: 11 * time.Hour}
	line := domain.Line{ID: "BAD", Days: [7]*domain.ShiftTemplate{&long, nil, nil, nil, nil, nil, nil}}
	vs, err := validate.ValidateWeek([]domain.Line{line}, map[string]domain.QualSet{"BAD": domain.CPC(true)}, f, r)
	if err != nil {
		t.Fatal(err)
	}
	hits := byRule(vs, "max-shift-hours")
	if len(hits) != 1 || hits[0].Severity != validate.Illegal {
		t.Fatalf("expected one max-shift-hours Illegal, got %+v", hits)
	}
}

// V2: removing the 1210 (M8b) line opens coverage violations for the late core,
// with the missing vector in Detail. We drop M8b from the 9-line fixture on
// Monday and expect new coverage gaps in the 1545-2010 late-core band.
func TestV2_CoverageGapDetail(t *testing.T) {
	f, r := fixtures.HLNFacility(), fixtures.HLNRules()

	// Baseline Monday is clean (7 instances). Remove the M3 line's Monday M8b.
	lines := fixtures.HLNLines()
	for i := range lines {
		if lines[i].ID == "M3" {
			lines[i].Days[1] = nil // Monday
		}
	}
	vs, err := validate.ValidateWeek(lines, fixtures.OccupantQuals(), f, r)
	if err != nil {
		t.Fatal(err)
	}
	gaps := byRule(vs, "coverage-gap")
	// Find a Monday gap covering part of the late core after M8a shifts end (1545).
	var lateGap *validate.Violation
	for i := range gaps {
		if gaps[i].Detail["weekday"] == "Mon" {
			lateGap = &gaps[i]
		}
	}
	if lateGap == nil {
		t.Fatalf("expected Monday coverage gaps after removing M8b, got %+v", gaps)
	}
	if _, ok := lateGap.Detail["missing"]; !ok {
		t.Errorf("coverage gap should carry a missing vector, got %+v", lateGap.Detail)
	}
	if lateGap.Interval == nil {
		t.Errorf("coverage gap should reference the demand interval")
	}
}

// V3: a line working 7 consecutive days is Illegal under six-of-seven, including
// runs that straddle the repeating week boundary.
func TestV3_SixOfSeven(t *testing.T) {
	f, r := fixtures.HLNFacility(), fixtures.HLNRules()
	e8 := fixtures.Templates()["E8"]
	// Works all 7 days.
	var days [7]*domain.ShiftTemplate
	for i := range days {
		s := e8
		days[i] = &s
	}
	line := domain.Line{ID: "GRIND", Days: days}
	vs, err := validate.ValidateWeek([]domain.Line{line}, map[string]domain.QualSet{"GRIND": domain.CPC(true)}, f, r)
	if err != nil {
		t.Fatal(err)
	}
	hits := byRule(vs, "six-of-seven")
	if len(hits) != 1 || hits[0].Severity != validate.Illegal {
		t.Fatalf("expected one six-of-seven Illegal, got %+v", hits)
	}
	if hits[0].Detail["worst_in_window"] != 7 {
		t.Errorf("expected worst_in_window 7, got %v", hits[0].Detail["worst_in_window"])
	}
}

// V4: biweekly-hours. 72 scheduled + 8h annual leave = clean; 72 with no leave =
// Illegal with delta -8h.
func TestV4_BiweeklyHours(t *testing.T) {
	r := fixtures.HLNRules()                                    // RequiredHoursPerPP = 80h
	start := domain.Date{Year: 2026, Month: time.July, Day: 12} // a Sunday

	// A line that schedules 72h over the PP: 9 eight-hour shifts. Build a weekly
	// pattern of 5 workdays (=40h/wk => 80h/PP) then knock one shift out on the
	// second week via leave to model "72 scheduled + 8h leave".
	e8 := fixtures.Templates()["E8"]
	ref := func() *domain.ShiftTemplate { s := e8; return &s }
	// Mon-Fri workdays (index 1..5), RDO Sun/Sat => 5/wk => 80h/PP.
	line := domain.Line{ID: "L", Days: [7]*domain.ShiftTemplate{nil, ref(), ref(), ref(), ref(), ref(), nil}}
	lineID := "L"
	ctrl := domain.Controller{ID: "C1", Name: "One", Quals: domain.CPC(true), LineID: &lineID}
	linesByID := map[string]domain.Line{"L": line}

	// No leave: 80h scheduled, exactly required -> clean.
	if vs := validate.ValidateControllerHours([]domain.Controller{ctrl}, linesByID, nil, start, r); len(vs) != 0 {
		t.Fatalf("80h line with no leave should be clean, got %+v", vs)
	}

	// 8h annual leave on one workday: 72 scheduled + 8 leave = 80 -> clean.
	leaveDay := start.AddDays(1) // Monday, a workday
	leave := []domain.Leave{{ControllerID: "C1", Date: leaveDay, Hours: 8 * time.Hour, Type: domain.LeaveAnnual}}
	if vs := validate.ValidateControllerHours([]domain.Controller{ctrl}, linesByID, leave, start, r); len(vs) != 0 {
		t.Fatalf("72 scheduled + 8h leave should be clean, got %+v", vs)
	}

	// Same missing shift but recorded as unpaid absence (0h leave): 72 total,
	// Illegal, delta -8h.
	unpaid := []domain.Leave{{ControllerID: "C1", Date: leaveDay, Hours: 0, Type: domain.LeaveAnnual}}
	vs := validate.ValidateControllerHours([]domain.Controller{ctrl}, linesByID, unpaid, start, r)
	hits := byRule(vs, "biweekly-hours")
	if len(hits) != 1 || hits[0].Severity != validate.Illegal {
		t.Fatalf("expected one biweekly-hours Illegal, got %+v", vs)
	}
	if d := hits[0].Detail["delta_h"].(float64); d != -8 {
		t.Errorf("expected delta -8h, got %v", d)
	}
}

// V5: the HLN 9-line fixture has no structural Illegals (shift length,
// six-of-seven, line-qualification) and exactly the known M1 turnaround Warning.
// The only Illegals are the documented mid-day coverage dips.
func TestV5_NineLineFixture(t *testing.T) {
	f, r := fixtures.HLNFacility(), fixtures.HLNRules()
	vs, err := validate.ValidateWeek(fixtures.HLNLines(), fixtures.OccupantQuals(), f, r)
	if err != nil {
		t.Fatal(err)
	}

	for _, rule := range []string{"max-shift-hours", "six-of-seven", "line-qualification"} {
		if hits := byRule(vs, rule); len(hits) != 0 {
			t.Errorf("expected no %s violations, got %+v", rule, hits)
		}
	}

	turns := byRule(vs, "turnaround")
	if len(turns) != 1 || turns[0].LineID != "M1" || turns[0].Severity != validate.Warning {
		t.Errorf("expected exactly the M1 turnaround Warning, got %+v", turns)
	}

	// The only Illegals are coverage dips, all at the 1345-1410 handoff.
	for _, v := range vs {
		if v.Severity != validate.Illegal {
			continue
		}
		if v.Rule != "coverage-gap" {
			t.Errorf("unexpected Illegal rule %q: %s", v.Rule, v.Message)
		}
	}
	gaps := byRule(vs, "coverage-gap")
	if len(gaps) != 4 { // Sun, Tue, Wed, Thu
		t.Errorf("expected 4 coverage-dip Illegals, got %d", len(gaps))
	}
}

// V6: an LC-only(no CIC) controller on a line that requires {LC, CIC} is a
// line-qualification Illegal; LC-only(CIC) is clean.
func TestV6_LineQualification(t *testing.T) {
	// Reuse the small two-line facility where line A must carry AP+CIC and B is
	// LC-only. Here we make the *coverage-critical* line require CIC and assign an
	// occupant that lacks it.
	pos := []domain.Position{{ID: "AP", Requires: domain.CapAP}, {ID: "LC", Requires: domain.CapLC}}
	f, err := domain.NewFacility("SM", "small", tod("0900"), tod("1300"), pos)
	if err != nil {
		t.Fatal(err)
	}
	r := fixtures.HLNRules()

	all := func(id string) domain.Line {
		s := domain.ShiftTemplate{ID: id, Start: tod("0900"), Duration: 4 * time.Hour}
		var days [7]*domain.ShiftTemplate
		for i := range days {
			days[i] = &s
		}
		return domain.Line{ID: id, Days: days}
	}
	lines := []domain.Line{all("A"), all("B")}

	// A occupant lacks CIC while B is LC-only(no CIC): A must supply AP+CIC, so an
	// A without CIC is a line-qualification violation.
	bad := map[string]domain.QualSet{"A": domain.CPC(false), "B": domain.LCOnly(false)}
	vs, err := validate.ValidateWeek(lines, bad, f, r)
	if err != nil {
		t.Fatal(err)
	}
	if hits := byRule(vs, "line-qualification"); len(hits) == 0 {
		t.Errorf("expected a line-qualification Illegal for A lacking CIC, got none")
	}

	// A holding CIC: clean line-qualification.
	good := map[string]domain.QualSet{"A": domain.CPC(true), "B": domain.LCOnly(false)}
	vs, err = validate.ValidateWeek(lines, good, f, r)
	if err != nil {
		t.Fatal(err)
	}
	if hits := byRule(vs, "line-qualification"); len(hits) != 0 {
		t.Errorf("expected clean line-qualification with A CPC+CIC, got %+v", hits)
	}
}
