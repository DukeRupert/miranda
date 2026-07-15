package coverage

import (
	"fmt"
	"sort"
	"time"

	"github.com/dukerupert/miranda/internal/domain"
)

// WorkingShift is one body on the clock for one day: the shift they work and the
// qualifications they bring. It is the unit fed to the coverage evaluator.
type WorkingShift struct {
	LineID   string
	Template domain.ShiftTemplate
	Quals    domain.QualSet
}

// Gap kinds.
const (
	GapUnderstaffed = "understaffed" // fewer than MinStaffWhenOpen present
	GapUnfillable   = "unfillable"   // positions can't all be filled / no CIC placeable
	GapCapTotal     = "cap-total"    // bare-minimum staffing held past MaxTimeOnPosition
	GapCapAP        = "cap-ap"       // a lone AP-capable body pinned past the cap
	GapCapCIC       = "cap-cic"      // a lone CIC holder pinned past the cap
)

// CoverageGap is a span of a day where staffing fails. Snapshot kinds
// (understaffed/unfillable) report the shortfall in Missing; cap kinds report a
// continuous tight window that ran longer than MaxTimeOnPosition (Over = excess).
type CoverageGap struct {
	Start    domain.TimeOfDay          `json:"start"`
	End      domain.TimeOfDay          `json:"end"`
	Kind     string                    `json:"kind"`
	PresentN int                       `json:"present_n"`
	Missing  map[domain.Capability]int `json:"missing,omitempty"`
}

// Duration is the length of the gap span.
func (g CoverageGap) Duration() time.Duration { return g.End.Sub(g.Start) }

// Deficit is how many bodies short the gap is: the headcount shortfall for a
// snapshot gap, or one relief body for a cap-breach run.
func (g CoverageGap) Deficit() int {
	if n := g.Missing[TotalKey]; n > 0 {
		return n
	}
	return 1
}

// TotalKey is the pseudo-capability under which a headcount shortfall is reported.
const TotalKey domain.Capability = "TOTAL"

// slice is one atomic time span of a day with the bodies present throughout it.
type slice struct {
	start, end domain.TimeOfDay
	present    []domain.QualSet
	okSnapshot bool
}

// DayCoverageGaps evaluates one operating day under the rotation-aware rule and
// returns every failing span. It works in two passes over the atomic slices
// (cut at every shift boundary):
//
//  1. Snapshot: each slice must have >= MinStaffWhenOpen bodies and a valid
//     position assignment with a CIC on position — else it is an understaffed or
//     unfillable gap.
//  2. Continuous tight-runs: a maximal run of snapshot-OK slices that is "tight"
//     on total headcount, on AP-capable bodies, or on CIC holders (no relief in
//     that dimension) is a cap breach if it runs longer than MaxTimeOnPosition.
//     Runs at or under the cap — the legal handoff dips — are fine.
func DayCoverageGaps(working []WorkingShift, f domain.Facility, r domain.RuleSet) []CoverageGap {
	open, close := f.OpenTime, f.CloseTime
	capDur := r.MaxTimeOnPosition
	numPos := len(f.Positions)
	apPos := f.PositionsRequiring(domain.CapAP)

	// Boundaries: open, close, and every shift start/end inside the window.
	bset := map[domain.TimeOfDay]bool{open: true, close: true}
	for _, w := range working {
		for _, t := range []domain.TimeOfDay{w.Template.Start, w.Template.End()} {
			if t > open && t < close {
				bset[t] = true
			}
		}
	}
	bounds := make([]domain.TimeOfDay, 0, len(bset))
	for t := range bset {
		bounds = append(bounds, t)
	}
	sort.Slice(bounds, func(i, j int) bool { return bounds[i] < bounds[j] })

	// Build slices and run the snapshot pass.
	var slices []slice
	var gaps []CoverageGap
	for i := 0; i+1 < len(bounds); i++ {
		a, b := bounds[i], bounds[i+1]
		var present []domain.QualSet
		for _, w := range working {
			if w.Template.Start <= a && w.Template.End() >= b {
				present = append(present, w.Quals)
			}
		}
		ok := len(present) >= r.MinStaffWhenOpen && canAssign(present, f.Positions)
		slices = append(slices, slice{start: a, end: b, present: present, okSnapshot: ok})
		if !ok {
			kind := GapUnfillable
			if len(present) < r.MinStaffWhenOpen {
				kind = GapUnderstaffed
			}
			gaps = append(gaps, CoverageGap{
				Start: a, End: b, Kind: kind, PresentN: len(present),
				Missing: snapshotShortfall(present, f, r),
			})
		}
	}

	// Tight-run pass: one predicate per pinned dimension.
	tight := []struct {
		kind string
		is   func(s slice) bool
	}{
		{GapCapTotal, func(s slice) bool { return len(s.present) <= numPos }},
		{GapCapAP, func(s slice) bool { return countHolding(s.present, domain.CapAP) <= apPos }},
		{GapCapCIC, func(s slice) bool { return countHolding(s.present, domain.CapCIC) <= cicOnPositionMin }},
	}
	for _, tp := range tight {
		gaps = append(gaps, capRuns(slices, tp.is, capDur, tp.kind)...)
	}
	return gaps
}

// capRuns finds maximal runs of snapshot-OK slices where the tightness predicate
// holds, and emits a gap for each run longer than the cap. Slices are contiguous
// by construction, so a run is just a consecutive stretch matching the predicate.
func capRuns(slices []slice, tight func(slice) bool, capDur time.Duration, kind string) []CoverageGap {
	var gaps []CoverageGap
	i := 0
	for i < len(slices) {
		if !slices[i].okSnapshot || !tight(slices[i]) {
			i++
			continue
		}
		j := i
		for j < len(slices) && slices[j].okSnapshot && tight(slices[j]) {
			j++
		}
		start, end := slices[i].start, slices[j-1].end
		if end.Sub(start) > capDur {
			gaps = append(gaps, CoverageGap{Start: start, End: end, Kind: kind, PresentN: len(slices[i].present)})
		}
		i = j
	}
	return gaps
}

// snapshotShortfall reports how far a slice falls below the snapshot minimum.
func snapshotShortfall(present []domain.QualSet, f domain.Facility, r domain.RuleSet) map[domain.Capability]int {
	out := map[domain.Capability]int{}
	if def := r.MinStaffWhenOpen - len(present); def > 0 {
		out[TotalKey] = def
	}
	for _, c := range []domain.Capability{domain.CapAP, domain.CapLC, domain.CapCIC} {
		need := f.PositionsRequiring(c)
		if c == domain.CapCIC {
			need = cicOnPositionMin
		}
		if def := need - countHolding(present, c); def > 0 {
			out[c] = def
		}
	}
	return out
}

// LineQualRequirements computes, per line, the minimum QualSet a controller must
// hold to legally occupy that line, given the other lines and their occupants'
// quals. A capability is "required" on a line when dropping it from that line's
// occupant makes some interval on some day of the week unsatisfiable. The result
// can express the middle case {LC sufficient, CIC required}.
//
// Method (spec §4.3): for each line, probe from a fully-qualified baseline and
// drop one capability at a time; re-run the whole week's coverage with that
// hypothetical occupant (others unchanged). If coverage breaks, that capability
// is load-bearing for the line.
func LineQualRequirements(
	lines []domain.Line,
	occupants map[string]domain.QualSet,
	f domain.Facility,
	r domain.RuleSet,
) (map[string]domain.QualSet, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if f.CloseTime <= f.OpenTime {
		return nil, errOvernight(f)
	}

	// Effective occupant quals: provided value, or best-case CPC+CIC when a line
	// has no stated occupant (so a missing entry doesn't spuriously break others).
	eff := func(id string) domain.QualSet {
		if q, ok := occupants[id]; ok {
			return q
		}
		return domain.CPC(true)
	}

	full := domain.QualSet{domain.CapAP: true, domain.CapLC: true, domain.CapCIC: true}
	req := make(map[string]domain.QualSet, len(lines))
	for _, line := range lines {
		// Baseline: probed line at full quals, others at their actual quals.
		occWith := func(cap domain.Capability, drop bool) map[string]domain.QualSet {
			m := map[string]domain.QualSet{}
			for _, other := range lines {
				if other.ID == line.ID {
					if drop {
						m[other.ID] = full.Without(cap)
					} else {
						m[other.ID] = full
					}
				} else {
					m[other.ID] = eff(other.ID)
				}
			}
			return m
		}
		baseGaps := len(weekGapSet(lines, occWith("", false), f, r))

		result := domain.QualSet{}
		for _, c := range []domain.Capability{domain.CapAP, domain.CapLC, domain.CapCIC} {
			// Dropping quals can only add coverage gaps, never remove them, so a
			// capability is load-bearing for this line exactly when dropping it
			// introduces MORE gaps than the full-quals baseline. Comparing gap
			// counts (rather than a bare satisfiable bool) keeps the result
			// meaningful even when the schedule already dips somewhere else.
			if len(weekGapSet(lines, occWith(c, true), f, r)) > baseGaps {
				result[c] = true
			}
		}
		req[line.ID] = result
	}
	return req, nil
}

// weekGapSet returns the set of distinct coverage-gap slices across the repeating
// week, keyed by day+span, given the occupant quals.
func weekGapSet(lines []domain.Line, occupants map[string]domain.QualSet, f domain.Facility, r domain.RuleSet) map[string]bool {
	out := map[string]bool{}
	for day := 0; day < 7; day++ {
		var working []WorkingShift
		for _, line := range lines {
			if s := line.Days[day]; s != nil {
				q := occupants[line.ID]
				if q == nil {
					q = domain.CPC(true)
				}
				working = append(working, WorkingShift{LineID: line.ID, Template: *s, Quals: q})
			}
		}
		for _, g := range DayCoverageGaps(working, f, r) {
			out[fmt.Sprintf("%d|%s|%s|%s", day, g.Kind, g.Start, g.End)] = true
		}
	}
	return out
}
