package coverage

import (
	"sort"

	"github.com/dukerupert/miranda/internal/domain"
)

// WorkingShift is one body on the clock for one day: the shift they work and the
// qualifications they bring. It is the unit fed to the coverage evaluator.
type WorkingShift struct {
	LineID   string
	Template domain.ShiftTemplate
	Quals    domain.QualSet
}

// CoverageGap is a sub-interval of a day where the bodies actually present fail
// to satisfy the demand. Missing carries the shortfall per requirement so the UI
// and validators can say exactly what qualification was short.
type CoverageGap struct {
	Start    domain.TimeOfDay          `json:"start"`
	End      domain.TimeOfDay          `json:"end"`
	Demand   DemandInterval            `json:"demand"`
	PresentN int                       `json:"present_n"`
	Missing  map[domain.Capability]int `json:"missing"` // capability -> shortfall; key "" reserved for total headcount
}

// TotalKey is the pseudo-capability under which a headcount (MinTotal) shortfall
// is reported in CoverageGap.Missing.
const TotalKey domain.Capability = "TOTAL"

// DayCoverageGaps evaluates one operating day and returns every sub-interval
// where the present bodies fail Satisfies. It refines the coarse demand timeline
// at every shift boundary: because a shift may start or end inside the core, the
// present-set changes there, and coverage must be checked on each atomic slice —
// not just per coarse demand interval. This is what catches a mid-day handoff
// dip that a per-coarse-interval check would miss.
func DayCoverageGaps(working []WorkingShift, f domain.Facility, r domain.RuleSet, dt DemandTimeline) []CoverageGap {
	open, close := f.OpenTime, f.CloseTime

	// Collect boundaries within the operating window: open, close, demand edges,
	// and every shift start/end (clamped).
	bset := map[domain.TimeOfDay]bool{open: true, close: true}
	for _, iv := range dt {
		bset[iv.Start] = true
		bset[iv.End] = true
	}
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

	var gaps []CoverageGap
	for i := 0; i+1 < len(bounds); i++ {
		a, b := bounds[i], bounds[i+1]
		coarse, ok := dt.At(a)
		if !ok {
			continue // outside the operating window
		}
		// Bodies present for the whole slice [a,b): shift covers it start-to-end.
		var present []domain.QualSet
		for _, w := range working {
			if w.Template.Start <= a && w.Template.End() >= b {
				present = append(present, w.Quals)
			}
		}
		sub := DemandInterval{Start: a, End: b, MinTotal: coarse.MinTotal, MinCapable: coarse.MinCapable}
		if !Satisfies(present, f.Positions, sub, r) {
			gaps = append(gaps, CoverageGap{
				Start:    a,
				End:      b,
				Demand:   sub,
				PresentN: len(present),
				Missing:  shortfall(present, sub),
			})
		}
	}
	return gaps
}

// shortfall reports how far the present bodies fall below the demand, per
// requirement. Only positive deficits are included.
func shortfall(present []domain.QualSet, d DemandInterval) map[domain.Capability]int {
	out := map[domain.Capability]int{}
	if def := d.MinTotal - len(present); def > 0 {
		out[TotalKey] = def
	}
	for c, need := range d.MinCapable {
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
	dt, err := ComputeDemand(f, r)
	if err != nil {
		return nil, err
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
		result := domain.QualSet{}
		for _, c := range []domain.Capability{domain.CapAP, domain.CapLC, domain.CapCIC} {
			hypo := map[string]domain.QualSet{}
			for _, other := range lines {
				if other.ID == line.ID {
					hypo[other.ID] = full.Without(c)
				} else {
					hypo[other.ID] = eff(other.ID)
				}
			}
			if !weekSatisfiable(lines, hypo, f, r, dt) {
				result[c] = true
			}
		}
		req[line.ID] = result
	}
	return req, nil
}

// weekSatisfiable reports whether every day of the repeating week is fully
// covered given the occupant quals.
func weekSatisfiable(lines []domain.Line, occupants map[string]domain.QualSet, f domain.Facility, r domain.RuleSet, dt DemandTimeline) bool {
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
		if len(DayCoverageGaps(working, f, r, dt)) > 0 {
			return false
		}
	}
	return true
}
