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

// The 9-line reference fixture (spec §8) does NOT fully satisfy its own derived
// demand: the four 6-instance days (Sun/Tue/Wed/Thu) dip to 2 bodies during the
// 1345-1410 mid-day handoff, when the E8 line has ended (1345) and the L8 line
// has not yet started (1410), leaving only the two M-shifts against a core that
// the conservative demand model requires be staffed at 3. The 7-instance days
// (Mon/Fri/Sat) carry an extra M-shift and are clean.
//
// This is a real engine finding, not a test contrivance — it is exactly the kind
// of non-obvious coverage hole the tool exists to surface. See POC-NOTES.md.
func TestNineLineCoverage_KnownDips(t *testing.T) {
	f, r := fixtures.HLNFacility(), fixtures.HLNRules()
	dt, _ := coverage.ComputeDemand(f, r)
	occ := fixtures.OccupantQuals()

	dipDays := map[int]bool{0: true, 2: true, 3: true, 4: true} // Sun, Tue, Wed, Thu
	cleanDays := map[int]bool{1: true, 5: true, 6: true}        // Mon, Fri, Sat

	for day := 0; day < 7; day++ {
		gaps := coverage.DayCoverageGaps(dayShifts(fixtures.HLNLines(), occ, day), f, r, dt)
		switch {
		case dipDays[day]:
			if len(gaps) != 1 {
				t.Errorf("day %d: expected exactly 1 dip, got %d: %+v", day, len(gaps), gaps)
				continue
			}
			g := gaps[0]
			if g.Start != tod("1345") || g.End != tod("1410") {
				t.Errorf("day %d: dip at %s-%s, expected 1345-1410", day, g.Start, g.End)
			}
			if g.PresentN != 2 || g.Missing[coverage.TotalKey] != 1 {
				t.Errorf("day %d: expected present=2 total-short=1, got present=%d missing=%v", day, g.PresentN, g.Missing)
			}
		case cleanDays[day]:
			if len(gaps) != 0 {
				t.Errorf("day %d: expected clean coverage, got %+v", day, gaps)
			}
		}
	}
}

// (a): the all-CPC-with-CIC skeleton for a 6-instance day (2xE8, M8a, M8b, 2xL8)
// covers the whole day EXCEPT the 1345-1410 handoff band — the same dip as
// above. A corrected day that adds a second M8a (mirroring the 7-instance days)
// is fully clean. This pins the fact that the skeleton's coverage hole is about
// the mid-day handoff, not qualifications.
func TestSkeletonCoverage(t *testing.T) {
	f, r := fixtures.HLNFacility(), fixtures.HLNRules()
	dt, _ := coverage.ComputeDemand(f, r)
	tpl := fixtures.Templates()
	cpc := domain.CPC(true)

	shift := func(id string) coverage.WorkingShift {
		return coverage.WorkingShift{LineID: id, Template: tpl[id], Quals: cpc}
	}

	// 6-instance skeleton.
	skeleton := []coverage.WorkingShift{shift("E8"), shift("E8"), shift("M8a"), shift("M8b"), shift("L8"), shift("L8")}
	gaps := coverage.DayCoverageGaps(skeleton, f, r, dt)
	if len(gaps) != 1 || gaps[0].Start != tod("1345") || gaps[0].End != tod("1410") {
		t.Errorf("6-instance skeleton: expected one 1345-1410 dip, got %+v", gaps)
	}

	// Corrected: add a second M8a (7 instances) -> fully clean.
	corrected := append(skeleton, shift("M8a"))
	if g := coverage.DayCoverageGaps(corrected, f, r, dt); len(g) != 0 {
		t.Errorf("corrected 7-instance day should be clean, got %+v", g)
	}
}

// (a) qualification angle: swapping the corrected day's controllers for a mix
// that still covers all positions keeps it clean; removing the only AP-capable
// bodies opens qualification gaps.
func TestCoverage_QualSensitivity(t *testing.T) {
	f, r := fixtures.HLNFacility(), fixtures.HLNRules()
	dt, _ := coverage.ComputeDemand(f, r)
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
	gaps := coverage.DayCoverageGaps(allLC, f, r, dt)
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
	f, err := domain.NewFacility("SM", "small", tod("0900"), tod("1300"), pos) // 4h == 2*cap -> no core
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
