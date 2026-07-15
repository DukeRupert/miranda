package coverage

import (
	"github.com/dukerupert/miranda/internal/domain"
)

// Satisfies reports whether the present controllers can legally staff the
// interval. This is an assignment-existence question, not three independent
// count checks (spec §4.2): there must exist a placement of bodies onto the
// facility's position slots such that
//   - every position slot is filled by a body qualified for it,
//   - at least one ON-POSITION body holds CIC,
//   - the present headcount meets MinTotal and each MinCapable floor.
//
// The signature takes the facility positions and rule set in addition to the
// spec's (present, interval): the position→capability map and the
// PositionSwapIsBreak flag are both needed to decide feasibility, and neither
// is recoverable from the interval alone.
func Satisfies(present []domain.QualSet, positions []domain.Position, d DemandInterval, r domain.RuleSet) bool {
	// 1. Headcount and per-capability floors. These encode rotation relief:
	//    a rotation-bearing core carries MinCapable{AP:2,CIC:2}, so a lone
	//    AP-capable or lone CIC holder fails here (the "pinning" cases).
	if len(present) < d.MinTotal {
		return false
	}
	for c, n := range d.MinCapable {
		if countHolding(present, c) < n {
			return false
		}
	}

	// 2. Assignment existence, including CIC-on-position. Pure counting can pass
	//    while placement fails — e.g. the only CIC holder qualifies for no
	//    position and can sit only on break (spec test (f)).
	if !canAssign(present, positions) {
		return false
	}

	// 3. PositionSwapIsBreak subtlety (spec §4.4). When the flag is set and the
	//    window exceeds MaxTimeOnPosition, the on-position clock only resets if
	//    the present bodies can actually swap into each other's positions. Two
	//    CPCs can (true); a CPC + LC-only cannot — the LC-only can't backfill AP,
	//    so the CPC stays pinned on AP past the cap (false). Pinned by test (e).
	if r.PositionSwapIsBreak && d.Duration() > r.MaxTimeOnPosition {
		if !allCrossQualified(present, positions) {
			return false
		}
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
