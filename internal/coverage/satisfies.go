package coverage

import (
	"github.com/dukerupert/miranda/internal/domain"
)

// Satisfies reports whether the present controllers can legally staff a single
// continuous interval. It is duration-aware, which is the crux of the
// rotation-aware model (spec §4.2 as corrected in POC-NOTES.md):
//
//   - Always: at least MinStaffWhenOpen bodies present, and an assignment must
//     exist that fills every position with a qualified body and puts at least one
//     CIC holder ON a position. (Pure counting is insufficient — the only CIC
//     holder might qualify for no position and be stuck on break.)
//   - Only when the interval is LONGER than MaxTimeOnPosition does rotation
//     relief become mandatory: a spare body, a second AP-capable, and a second
//     CIC — so nobody is pinned on a position past the cap. A window no longer
//     than the cap needs none of that (everyone can just stay put).
//
// PositionSwapIsBreak (spec §4.4): when set, a longer window needs no extra
// relief provided every present body can swap into every position (two CPCs can
// hold a 3h window; a CPC + an LC-only cannot, because the LC-only can't backfill
// AP, pinning the CPC).
//
// The signature takes the facility positions and rule set in addition to the
// spec's (present, interval): the position→capability map, the on-position cap,
// and the swap flag are all needed and none are recoverable from the interval's
// staffing vector alone.
func Satisfies(present []domain.QualSet, positions []domain.Position, d DemandInterval, r domain.RuleSet) bool {
	// Snapshot feasibility — holds for any interval length.
	if len(present) < r.MinStaffWhenOpen {
		return false
	}
	if !canAssign(present, positions) {
		return false
	}

	// Windows within the cap need no rotation relief.
	if d.Duration() <= r.MaxTimeOnPosition {
		return true
	}

	// Longer windows require relief — unless swap-is-break and everyone can swap.
	if r.PositionSwapIsBreak && allCrossQualified(present, positions) {
		return true
	}
	apPos := positionsRequiring(positions, domain.CapAP)
	if len(present) < r.MinStaffWhenOpen+breakRelief {
		return false
	}
	if countHolding(present, domain.CapAP) < apPos+breakRelief {
		return false
	}
	if countHolding(present, domain.CapCIC) < cicOnPositionMin+breakRelief {
		return false
	}
	return true
}

// countHolding counts present bodies holding capability c.
func countHolding(present []domain.QualSet, c domain.Capability) int {
	n := 0
	for _, q := range present {
		if q.Has(c) {
			n++
		}
	}
	return n
}

// positionsRequiring counts positions whose required capability is c.
func positionsRequiring(positions []domain.Position, c domain.Capability) int {
	n := 0
	for _, p := range positions {
		if p.Requires == c {
			n++
		}
	}
	return n
}

// canAssign tries to place bodies onto every position slot (distinct body per
// slot, qualified for it) such that at least one placed body holds CIC. It is a
// small backtracking matcher — the qual structure here is a nested hierarchy
// plus one orthogonal tag, so this stands in for (and could be replaced by) a
// general bipartite matching behind the same call.
func canAssign(present []domain.QualSet, positions []domain.Position) bool {
	used := make([]bool, len(present))
	var rec func(pi int, cicOnPosition bool) bool
	rec = func(pi int, cicOnPosition bool) bool {
		if pi == len(positions) {
			return cicOnPosition
		}
		for bi, q := range present {
			if used[bi] || !q.Has(positions[pi].Requires) {
				continue
			}
			used[bi] = true
			if rec(pi+1, cicOnPosition || q.Has(domain.CapCIC)) {
				used[bi] = false
				return true
			}
			used[bi] = false
		}
		return false
	}
	return rec(0, false)
}

// allCrossQualified reports whether every present body is qualified for every
// position — the condition under which PositionSwapIsBreak actually lets bodies
// swap to reset the on-position clock.
func allCrossQualified(present []domain.QualSet, positions []domain.Position) bool {
	for _, q := range present {
		for _, p := range positions {
			if !q.Has(p.Requires) {
				return false
			}
		}
	}
	return true
}
