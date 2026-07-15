package coverage_test

import (
	"testing"
	"time"

	"github.com/dukerupert/miranda/internal/coverage"
	"github.com/dukerupert/miranda/internal/domain"
	"github.com/dukerupert/miranda/internal/fixtures"
)

// dayShifts builds the WorkingShift set for one weekday of a line list, using the
// given occupant quals.
func dayShifts(lines []domain.Line, occ map[string]domain.QualSet, day int) []coverage.WorkingShift {
	var out []coverage.WorkingShift
	for _, line := range lines {
		if s := line.Days[day]; s != nil {
			out = append(out, coverage.WorkingShift{LineID: line.ID, Template: *s, Quals: occ[line.ID]})
		}
	}
	return out
}

// Under the rotation-aware rule the 9-line reference fixture (spec §8) is fully
// clean: the 1345-1410 mid-day handoff on the 6-instance days drops to 2 bodies
// for only 25 minutes — well under the 2h on-position cap — so it is a legal dip,
// not a coverage gap. Every day validates with zero gaps.
func TestNineLineCoverage_Clean(t *testing.T) {
	f, r := fixtures.HLNFacility(), fixtures.HLNRules()
	occ := fixtures.OccupantQuals()
	for day := 0; day < 7; day++ {
		if gaps := coverage.DayCoverageGaps(dayShifts(fixtures.HLNLines(), occ, day), f, r); len(gaps) != 0 {
			t.Errorf("day %d: expected clean coverage, got %+v", day, gaps)
		}
	}
}

// (a): the all-CPC-with-CIC skeleton for a 6-instance day (2xE8, M8a, M8b, 2xL8)
// covers the whole day. Its only 2-body stretch mid-day (13:45-14:10, 25 min) is
// under the cap, so it is clean. Stretching that dip past the cap (delaying the
// closers) turns it into a position-cap breach.
func TestSkeletonCoverage(t *testing.T) {
	f, r := fixtures.HLNFacility(), fixtures.HLNRules()
	tpl := fixtures.Templates()
	cpc := domain.CPC(true)
	shift := func(id string) coverage.WorkingShift {
		return coverage.WorkingShift{LineID: id, Template: tpl[id], Quals: cpc}
	}

	// 6-instance skeleton -> clean under the rotation-aware rule.
	skeleton := []coverage.WorkingShift{shift("E8"), shift("E8"), shift("M8a"), shift("M8b"), shift("L8"), shift("L8")}
	if gaps := coverage.DayCoverageGaps(skeleton, f, r); len(gaps) != 0 {
		t.Errorf("6-instance skeleton should be clean, got %+v", gaps)
	}

	// A 4h facility staffed by exactly two bodies for the whole window: that is a
	// continuous bare-minimum (2-body) stretch of 4h, twice the on-position cap,
	// so it is a cap-total breach.
	sf, err := domain.NewFacility("CB", "capbreach", tod("0600"), tod("1000"), f.Positions)
	if err != nil {
		t.Fatal(err)
	}
	span := domain.ShiftTemplate{ID: "D4", Start: tod("0600"), Duration: 4 * time.Hour}
	two := []coverage.WorkingShift{{Template: span, Quals: cpc}, {Template: span, Quals: cpc}}
	sawCap := false
	for _, g := range coverage.DayCoverageGaps(two, sf, r) {
		if g.Kind == coverage.GapCapTotal && g.Start == tod("0600") && g.End == tod("1000") {
			sawCap = true
		}
	}
	if !sawCap {
		t.Errorf("a 4h two-body window should be a cap-total breach")
	}
}

// (a) qualification angle: swapping the corrected day's controllers for a mix
// that still covers all positions keeps it clean; removing the only AP-capable
// bodies opens qualification gaps.
func TestCoverage_QualSensitivity(t *testing.T) {
	f, r := fixtures.HLNFacility(), fixtures.HLNRules()
	tpl := fixtures.Templates()

	// All LC-only: every position-AP requirement fails across the day.
	lc := domain.LCOnly(true)
	shift := func(id string, q domain.QualSet) coverage.WorkingShift {
		return coverage.WorkingShift{LineID: id, Template: tpl[id], Quals: q}
	}
	allLC := []coverage.WorkingShift{
		shift("E8", lc), shift("E8", lc), shift("M8a", lc), shift("M8a", lc),
		shift("M8b", lc), shift("L8", lc), shift("L8", lc),
	}
	gaps := coverage.DayCoverageGaps(allLC, f, r)
	if len(gaps) == 0 {
		t.Fatal("all-LC-only day should have AP coverage gaps")
	}
	sawAP := false
	for _, g := range gaps {
		if g.Missing[domain.CapAP] > 0 {
			sawAP = true
		}
	}
	if !sawAP {
		t.Errorf("all-LC-only day should report an AP shortfall, got %+v", gaps)
	}
}

// LineQualRequirements: on a short single-interval day covered by two all-day
// lines where one occupant is LC-only(no CIC), the other line is forced to carry
// {AP, CIC} but NOT LC — the "middle case" the spec calls out. (spec §4.3)
func TestLineQualRequirements(t *testing.T) {
	pos := []domain.Position{{ID: "AP", Requires: domain.CapAP}, {ID: "LC", Requires: domain.CapLC}}
	// A cap-length window (2h) so two all-day bodies are legal without a relief
	// layer — isolating the qualification requirement from the rotation rule.
	f, err := domain.NewFacility("SM", "small", tod("0900"), tod("1100"), pos)
	if err != nil {
		t.Fatal(err)
	}
	r := fixtures.HLNRules()

	all := func(id string) domain.Line {
		s := domain.ShiftTemplate{ID: id, Start: tod("0900"), Duration: 2 * time.Hour}
		var days [7]*domain.ShiftTemplate
		for i := range days {
			days[i] = &s
		}
		return domain.Line{ID: id, Days: days}
	}
	lines := []domain.Line{all("A"), all("B")}
	occ := map[string]domain.QualSet{"A": domain.CPC(true), "B": domain.LCOnly(false)}

	req, err := coverage.LineQualRequirements(lines, occ, f, r)
	if err != nil {
		t.Fatal(err)
	}
	// A must supply AP and CIC (B, being LC-only-no-CIC, supplies neither) but
	// not LC (B covers LC).
	if !req["A"].HasAll(domain.CapAP, domain.CapCIC) {
		t.Errorf("line A should require AP and CIC, got %v", req["A"])
	}
	if req["A"].Has(domain.CapLC) {
		t.Errorf("line A should NOT require LC (B covers it), got %v", req["A"])
	}
	// B, sitting alongside a full CPC+CIC on A, is not individually required to
	// hold anything.
	if len(req["B"]) != 0 {
		t.Errorf("line B should have no hard requirement given A is CPC+CIC, got %v", req["B"])
	}
}
