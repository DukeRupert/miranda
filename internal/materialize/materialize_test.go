package materialize_test

import (
	"testing"
	"time"

	"github.com/dukerupert/miranda/internal/domain"
	"github.com/dukerupert/miranda/internal/fixtures"
	"github.com/dukerupert/miranda/internal/materialize"
)

// 2026-07-12 is a Sunday, so the pay period aligns to the fixture's week
// (index 0 == Sunday).
var ppStart = domain.Date{Year: 2026, Month: time.July, Day: 12}

func TestMaterialize_Baseline(t *testing.T) {
	f, r := fixtures.HLNFacility(), fixtures.HLNRules()
	pp, err := materialize.Materialize(fixtures.HLNLines(), fixtures.HLNControllers(), nil, ppStart, f, r)
	if err != nil {
		t.Fatal(err)
	}

	if ppStart.Weekday() != time.Sunday {
		t.Fatalf("test assumes a Sunday start, got %v", ppStart.Weekday())
	}

	// No leave, all lines assigned -> no vacated/unfilled gaps.
	if len(pp.Gaps) != 0 {
		t.Errorf("baseline should have no vacated gaps, got %d: %+v", len(pp.Gaps), pp.Gaps)
	}

	// The structural mid-day dips still cost OT: the dip appears on Sun/Tue/Wed/Thu
	// (4 days/week -> 8 days over the 14-day PP), each a 25-minute band short one
	// body => 8 * 25min = 3h20m of projected OT.
	wantOT := 8 * 25 * time.Minute
	if pp.ProjectedOT != wantOT {
		t.Errorf("baseline projected OT = %s, want %s", pp.ProjectedOT, wantOT)
	}
	if len(pp.Uncovered) != 8 {
		t.Errorf("expected 8 uncovered bands, got %d", len(pp.Uncovered))
	}
	// Every uncovered band is the 1345-1410 handoff, short by one body.
	for _, b := range pp.Uncovered {
		if b.Start != domain.MustParseTimeOfDay("1345") || b.End != domain.MustParseTimeOfDay("1410") || b.Deficit != 1 {
			t.Errorf("unexpected uncovered band %+v", b)
		}
	}

	// Sanity: 14 days of the 9-line pattern schedule a lot of shifts.
	if len(pp.Shifts) == 0 {
		t.Error("expected scheduled shifts")
	}
}

func TestMaterialize_LeaveDrivesOT(t *testing.T) {
	f, r := fixtures.HLNFacility(), fixtures.HLNRules()

	// Put the E1 controller on bid leave for week-1 Mon..Fri (the days E1 works).
	var e1 string
	for _, c := range fixtures.HLNControllers() {
		if c.LineID != nil && *c.LineID == "E1" {
			e1 = c.ID
		}
	}
	if e1 == "" {
		t.Fatal("E1 controller not found")
	}
	var leave []domain.Leave
	for i := 1; i <= 5; i++ { // Mon(13)..Fri(17)
		leave = append(leave, domain.Leave{ControllerID: e1, Date: ppStart.AddDays(i), Hours: 8 * time.Hour, Type: domain.LeaveBid})
	}

	base, _ := materialize.Materialize(fixtures.HLNLines(), fixtures.HLNControllers(), nil, ppStart, f, r)
	pp, err := materialize.Materialize(fixtures.HLNLines(), fixtures.HLNControllers(), leave, ppStart, f, r)
	if err != nil {
		t.Fatal(err)
	}

	// Five vacated instances, all reason leave:bid on line E1.
	if len(pp.Gaps) != 5 {
		t.Fatalf("expected 5 vacated gaps, got %d: %+v", len(pp.Gaps), pp.Gaps)
	}
	for _, g := range pp.Gaps {
		if g.Reason != "leave:bid" || g.LineID != "E1" {
			t.Errorf("unexpected gap %+v", g)
		}
	}

	// E1 is a load-bearing opener: the open shoulder needs both E8s, so losing E1
	// opens a 0545-0745 hole every day it works. None of these vacancies are
	// absorbable, and OT rises well above the structural baseline.
	for _, g := range pp.Gaps {
		if g.Absorbed {
			t.Errorf("E1 (an opener) vacancy should not be absorbable, got %+v", g)
		}
	}
	if pp.ProjectedOT <= base.ProjectedOT {
		t.Errorf("leave should increase projected OT: base %s, with-leave %s", base.ProjectedOT, pp.ProjectedOT)
	}
	// At least one uncovered band is the vacated open shoulder.
	sawOpener := false
	for _, b := range pp.Uncovered {
		if b.Start == domain.MustParseTimeOfDay("0545") && b.End == domain.MustParseTimeOfDay("0745") {
			sawOpener = true
		}
	}
	if !sawOpener {
		t.Errorf("expected a vacated open-shoulder (0545-0745) uncovered band")
	}
}

// A genuinely redundant overlap body's leave IS absorbed: a short single-interval
// day covered by three all-day lines needs only two, so one line's leave leaves
// coverage intact — no gap, no OT.
func TestMaterialize_AbsorbedOverlap(t *testing.T) {
	pos := []domain.Position{{ID: "AP", Requires: domain.CapAP}, {ID: "LC", Requires: domain.CapLC}}
	f, err := domain.NewFacility("SM", "small", domain.MustParseTimeOfDay("0900"), domain.MustParseTimeOfDay("1300"), pos)
	if err != nil {
		t.Fatal(err)
	}
	r := fixtures.HLNRules()

	all := func(id string) domain.Line {
		s := domain.ShiftTemplate{ID: id, Start: domain.MustParseTimeOfDay("0900"), Duration: 4 * time.Hour}
		var days [7]*domain.ShiftTemplate
		for i := range days {
			days[i] = &s
		}
		return domain.Line{ID: id, Days: days}
	}
	lines := []domain.Line{all("A"), all("B"), all("C")}
	ctrls := []domain.Controller{}
	for _, id := range []string{"A", "B", "C"} {
		lid := id
		ctrls = append(ctrls, domain.Controller{ID: "c" + id, Quals: domain.CPC(true), LineID: &lid})
	}

	// C on annual leave one day: A+B still cover the 2-body demand.
	leave := []domain.Leave{{ControllerID: "cC", Date: ppStart.AddDays(1), Hours: 4 * time.Hour, Type: domain.LeaveAnnual}}
	pp, err := materialize.Materialize(lines, ctrls, leave, ppStart, f, r)
	if err != nil {
		t.Fatal(err)
	}

	if len(pp.Gaps) != 1 {
		t.Fatalf("expected 1 vacated gap, got %d: %+v", len(pp.Gaps), pp.Gaps)
	}
	if !pp.Gaps[0].Absorbed {
		t.Errorf("redundant overlap vacancy should be absorbed, got %+v", pp.Gaps[0])
	}
	if pp.ProjectedOT != 0 {
		t.Errorf("absorbed vacancy should add no OT, got %s", pp.ProjectedOT)
	}
	if len(pp.Uncovered) != 0 {
		t.Errorf("absorbed vacancy should leave no uncovered bands, got %+v", pp.Uncovered)
	}
}
