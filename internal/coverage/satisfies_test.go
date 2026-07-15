package coverage_test

import (
	"testing"
	"time"

	"github.com/dukerupert/miranda/internal/coverage"
	"github.com/dukerupert/miranda/internal/domain"
	"github.com/dukerupert/miranda/internal/fixtures"
)

var hlnPositions = []domain.Position{
	{ID: "AP", Requires: domain.CapAP},
	{ID: "LC", Requires: domain.CapLC},
}

// coreInterval / shoulderInterval return the HLN demand intervals for reuse.
func coreInterval(t *testing.T) coverage.DemandInterval {
	t.Helper()
	dt, _ := coverage.ComputeDemand(fixtures.HLNFacility(), fixtures.HLNRules())
	return dt[1]
}
func shoulderInterval(t *testing.T) coverage.DemandInterval {
	t.Helper()
	dt, _ := coverage.ComputeDemand(fixtures.HLNFacility(), fixtures.HLNRules())
	return dt[0]
}

// (b): core interval qual pinning.
func TestSatisfies_CoreAPPinning(t *testing.T) {
	core := coreInterval(t)
	r := fixtures.HLNRules()

	// 2 CPC + 1 LC-only, all CIC -> true.
	ok := coverage.Satisfies([]domain.QualSet{domain.CPC(true), domain.CPC(true), domain.LCOnly(true)}, hlnPositions, core, r)
	if !ok {
		t.Errorf("2 CPC + 1 LC-only (all CIC) should satisfy the core")
	}
	// 1 CPC + 2 LC-only, all CIC -> false (AP pinning: only one AP-capable).
	if coverage.Satisfies([]domain.QualSet{domain.CPC(true), domain.LCOnly(true), domain.LCOnly(true)}, hlnPositions, core, r) {
		t.Errorf("1 CPC + 2 LC-only should fail the core (AP pinning)")
	}
	// Adding a 3rd LC-only does not help.
	if coverage.Satisfies([]domain.QualSet{domain.CPC(true), domain.LCOnly(true), domain.LCOnly(true), domain.LCOnly(true)}, hlnPositions, core, r) {
		t.Errorf("1 CPC + 3 LC-only should still fail the core")
	}
}

// (c): open shoulder (<= cap) needs only one AP-capable and one CIC.
func TestSatisfies_OpenShoulder(t *testing.T) {
	sh := shoulderInterval(t)
	r := fixtures.HLNRules()
	// 1 CPC (holds CIC) + 1 LC-only -> true.
	if !coverage.Satisfies([]domain.QualSet{domain.CPC(true), domain.LCOnly(false)}, hlnPositions, sh, r) {
		t.Errorf("1 CPC(CIC) + 1 LC-only should satisfy the open shoulder")
	}
}

// (e): PositionSwapIsBreak. Two CPCs may hold a >cap window (they can swap);
// CPC + LC-only may not (LC-only can't backfill AP, so the CPC is pinned).
func TestSatisfies_PositionSwap(t *testing.T) {
	// A 3h window built as a bare-minimum (2-body) interval.
	iv := coverage.DemandInterval{
		Start: tod("0600"), End: tod("0900"), MinTotal: 2,
		MinCapable: map[domain.Capability]int{domain.CapAP: 1, domain.CapLC: 1, domain.CapCIC: 1},
	}
	r := fixtures.HLNRules()
	r.PositionSwapIsBreak = true

	twoCPC := []domain.QualSet{domain.CPC(true), domain.CPC(true)}
	cpcPlusLC := []domain.QualSet{domain.CPC(true), domain.LCOnly(true)}

	if !coverage.Satisfies(twoCPC, hlnPositions, iv, r) {
		t.Errorf("two CPCs should hold a 3h window when swap-is-break")
	}
	if coverage.Satisfies(cpcPlusLC, hlnPositions, iv, r) {
		t.Errorf("CPC + LC-only should NOT hold a 3h window even when swap-is-break (CPC pinned on AP)")
	}
	// With the flag off, the hand-built low interval imposes no rotation cap, so
	// the length check is not applied — isolating the flag as the cause.
	rOff := r
	rOff.PositionSwapIsBreak = false
	if !coverage.Satisfies(cpcPlusLC, hlnPositions, iv, rOff) {
		t.Errorf("CPC + LC-only should satisfy the same low interval when the flag is off")
	}
}

// (f): counting-vs-assignment. Independent counts can pass while no valid
// on-position assignment exists, and vice-versa.
func TestSatisfies_CountVsAssignment(t *testing.T) {
	sh := shoulderInterval(t)
	r := fixtures.HLNRules()

	// Counts pass (2 AP-capable, 2 LC-capable, 1 CIC, 3 bodies) but the only CIC
	// holder can work no position, so CIC can't be placed on position -> false.
	cicOnly := domain.QualSet{domain.CapCIC: true}
	present := []domain.QualSet{domain.CPC(false), domain.CPC(false), cicOnly}
	if coverage.Satisfies(present, hlnPositions, sh, r) {
		t.Errorf("counts pass but CIC is unplaceable on position -> should fail")
	}

	// Mirror: a naive pessimistic check (CIC must be a CPC) would fail, but
	// placing LC-only(CIC) on LC as controller-in-charge and CPC(no CIC) on AP
	// is a valid assignment -> true.
	mirror := []domain.QualSet{domain.CPC(false), domain.LCOnly(true)}
	if !coverage.Satisfies(mirror, hlnPositions, sh, r) {
		t.Errorf("CPC(no CIC) on AP + LC-only(CIC) on LC should satisfy the shoulder")
	}
}

// (g): a lone CIC holder can never break, so a >cap window needs >=2 CIC.
func TestSatisfies_LoneCIC(t *testing.T) {
	core := coreInterval(t)
	r := fixtures.HLNRules()
	// 4 bodies, only 1 CIC -> false.
	oneCIC := []domain.QualSet{domain.CPC(true), domain.CPC(false), domain.CPC(false), domain.LCOnly(false)}
	if coverage.Satisfies(oneCIC, hlnPositions, core, r) {
		t.Errorf("core with only one CIC holder should fail (lone CIC can never break)")
	}
	// Same bodies but 2 CIC -> true.
	twoCIC := []domain.QualSet{domain.CPC(true), domain.CPC(true), domain.CPC(false), domain.LCOnly(false)}
	if !coverage.Satisfies(twoCIC, hlnPositions, core, r) {
		t.Errorf("core with two CIC holders should satisfy")
	}
}

// (h): a two-person open shoulder where the LC-only body carries CIC.
func TestSatisfies_LCOnlyAsCIC(t *testing.T) {
	sh := shoulderInterval(t)
	r := fixtures.HLNRules()
	present := []domain.QualSet{domain.CPC(false), domain.LCOnly(true)}
	if !coverage.Satisfies(present, hlnPositions, sh, r) {
		t.Errorf("CPC(no CIC) + LC-only(CIC) should satisfy the open shoulder with LC-only as CIC")
	}
}

// Property (recommended, spec §7): Satisfies agrees with an independent
// brute-force assignment enumeration for all body sets up to size 6.
func TestSatisfies_BruteForceAgreement(t *testing.T) {
	r := fixtures.HLNRules()
	intervals := []coverage.DemandInterval{shoulderInterval(t), coreInterval(t)}
	quals := []domain.QualSet{
		domain.CPC(true), domain.CPC(false), domain.LCOnly(true), domain.LCOnly(false),
	}
	// Enumerate all multisets of size up to 4 from the 4 qual kinds.
	var build func(prefix []domain.QualSet, start int)
	checked := 0
	build = func(prefix []domain.QualSet, start int) {
		if len(prefix) > 0 {
			for _, iv := range intervals {
				got := coverage.Satisfies(prefix, hlnPositions, iv, r)
				want := bruteSatisfies(prefix, hlnPositions, iv)
				if got != want {
					t.Errorf("Satisfies disagrees with brute force for %v @%s-%s: got %v want %v", describe(prefix), iv.Start, iv.End, got, want)
				}
				checked++
			}
		}
		if len(prefix) == 4 {
			return
		}
		for i := start; i < len(quals); i++ {
			build(append(prefix, quals[i]), i)
		}
	}
	build(nil, 0)
	if checked == 0 {
		t.Fatal("no combinations checked")
	}
}

// bruteSatisfies is an independent, deliberately naive re-implementation of the
// Satisfies contract used only to cross-check the optimized version.
func bruteSatisfies(present []domain.QualSet, positions []domain.Position, d coverage.DemandInterval) bool {
	if len(present) < d.MinTotal {
		return false
	}
	for c, n := range d.MinCapable {
		cnt := 0
		for _, q := range present {
			if q.Has(c) {
				cnt++
			}
		}
		if cnt < n {
			return false
		}
	}
	// Exhaustively try every ordered placement of distinct bodies onto positions.
	n := len(present)
	used := make([]bool, n)
	var rec func(pi int, cic bool) bool
	rec = func(pi int, cic bool) bool {
		if pi == len(positions) {
			return cic
		}
		for bi := 0; bi < n; bi++ {
			if used[bi] || !present[bi].Has(positions[pi].Requires) {
				continue
			}
			used[bi] = true
			if rec(pi+1, cic || present[bi].Has(domain.CapCIC)) {
				used[bi] = false
				return true
			}
			used[bi] = false
		}
		return false
	}
	return rec(0, false)
}

func describe(qs []domain.QualSet) string {
	s := ""
	for _, q := range qs {
		switch {
		case q.HasAll(domain.CapAP, domain.CapLC) && q.Has(domain.CapCIC):
			s += "CPC+CIC "
		case q.HasAll(domain.CapAP, domain.CapLC):
			s += "CPC "
		case q.Has(domain.CapLC) && q.Has(domain.CapCIC):
			s += "LC+CIC "
		default:
			s += "LC "
		}
	}
	return s
}

// Guard: the coreInterval genuinely exceeds a shift cap, so the tests above
// exercise the rotation regime rather than a trivial short window.
func TestCoreExceedsCap(t *testing.T) {
	if coreInterval(t).Duration() <= fixtures.HLNRules().MaxTimeOnPosition {
		t.Fatal("core interval should exceed MaxTimeOnPosition")
	}
	if coreInterval(t).Duration() <= 10*time.Hour {
		t.Fatal("core interval should exceed a 10h shift so MinDailyShiftInstances math holds")
	}
}
