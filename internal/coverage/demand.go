// Package coverage is the derivation engine: from a Facility + RuleSet it
// computes the staffing demand timeline (how many bodies, with which
// qualifications, at every minute), decides whether a set of present
// controllers can legally staff an interval, and reports the minimum
// qualification a line requires. Everything here is a pure function.
//
// The load-bearing idea (see spec §1): the maximum-time-on-position rule
// *generates* requirements that are never hand-entered. A window staffed at the
// bare open-minimum has everyone pinned on a position, so it may last at most
// MaxTimeOnPosition before someone is owed a break; that forces a higher-staffed
// "core" in the middle of the day, and — because a lone AP-capable or lone CIC
// holder can never be relieved — it forces a second AP-capable and a second CIC
// holder through that core. All three fall out of the same rule parameter.
package coverage

import (
	"time"

	"github.com/dukerupert/miranda/internal/domain"
)

// DemandInterval is a contiguous span with the minimum staffing it requires.
// MinCapable is keyed by capability: e.g. {AP:2, CIC:2, LC:1} in the core.
type DemandInterval struct {
	Start      domain.TimeOfDay          `json:"start"`
	End        domain.TimeOfDay          `json:"end"`
	MinTotal   int                       `json:"min_total"`
	MinCapable map[domain.Capability]int `json:"min_capable"`
}

// Duration is the length of the interval.
func (d DemandInterval) Duration() time.Duration { return d.End.Sub(d.Start) }

// DemandTimeline is a contiguous, non-overlapping sequence covering Open..Close.
type DemandTimeline []DemandInterval

// At returns the interval covering time t, and whether one was found.
func (dt DemandTimeline) At(t domain.TimeOfDay) (DemandInterval, bool) {
	for _, iv := range dt {
		if t >= iv.Start && t < iv.End {
			return iv, true
		}
	}
	return DemandInterval{}, false
}

// breakRelief is the single extra body a rotation-bearing window needs beyond
// the bare open-minimum: one floater lets each on-position controller step off
// for a break without dropping a position. It is the same "+1" that lifts the
// core's MinTotal above the shoulders and that lifts the AP-capable and CIC
// minimums from 1 to 2 — the mirrored consequences of the one rule. It is
// deliberately a named constant, not a literal "2"/"3" sprinkled through the
// logic (spec §10 grep-test).
const breakRelief = 1

// cicOnPositionMin is the floor of CIC holders required to be *working a
// position* at any instant the facility is open: at least one. The core lifts
// this by breakRelief so the CIC duty survives every individual's break.
const cicOnPositionMin = 1

// ComputeDemand derives the demand timeline from facility hours and the rule
// set. Pure function, no I/O. The shape is shoulder / core / shoulder:
//
//	[Open,               Open+cap]  MinTotal = MinStaffWhenOpen        (shoulder)
//	[Open+cap,          Close-cap]  MinTotal = MinStaffWhenOpen + 1    (core)
//	[Close-cap,             Close]  MinTotal = MinStaffWhenOpen        (shoulder)
//
// where cap = MaxTimeOnPosition. When the core span is non-positive (a short
// day, Close-Open <= 2*cap) the whole window collapses to a single low interval.
//
// PositionSwapIsBreak (spec §4.4): when set, swapping positions resets the
// on-position clock, so a bare-minimum window is no longer time-capped *provided
// the present bodies can actually swap* — a fact ComputeDemand cannot know
// (it has no bodies), so it emits flat low demand and defers the swap-capability
// check to Satisfies. All HLN fixtures use false.
func ComputeDemand(f domain.Facility, r domain.RuleSet) (DemandTimeline, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if f.CloseTime <= f.OpenTime {
		return nil, errOvernight(f)
	}

	posCap := domain.TimeOfDay(r.MaxTimeOnPosition / time.Minute)
	open, close := f.OpenTime, f.CloseTime

	apPositions := f.PositionsRequiring(domain.CapAP)
	lcPositions := f.PositionsRequiring(domain.CapLC)

	low := func(start, end domain.TimeOfDay) DemandInterval {
		return DemandInterval{
			Start:    start,
			End:      end,
			MinTotal: r.MinStaffWhenOpen,
			MinCapable: map[domain.Capability]int{
				domain.CapAP:  apPositions,      // fill every AP position
				domain.CapLC:  lcPositions,      // fill every LC position
				domain.CapCIC: cicOnPositionMin, // one CIC on position
			},
		}
	}

	coreStart := open + posCap
	coreEnd := close - posCap
	if r.PositionSwapIsBreak || coreStart >= coreEnd {
		// Flat low demand for the whole day (swap relief, or a day too short to
		// have a distinct core).
		return DemandTimeline{low(open, close)}, nil
	}

	core := DemandInterval{
		Start:    coreStart,
		End:      coreEnd,
		MinTotal: r.MinStaffWhenOpen + breakRelief,
		MinCapable: map[domain.Capability]int{
			domain.CapAP:  apPositions + breakRelief,      // AP must be handed off => a second AP-capable
			domain.CapLC:  lcPositions,                    // LC is relay-coverable; no extra beyond position count
			domain.CapCIC: cicOnPositionMin + breakRelief, // CIC must stay on-position through every break => a second CIC
		},
	}
	return DemandTimeline{
		low(open, coreStart),
		core,
		low(coreEnd, close),
	}, nil
}

// MinDailyShiftInstances is the minimum number of shift-instances (each no
// longer than MaxShiftHours, none extending past Close) needed to cover the
// demand timeline's MinTotal at every minute of the operating day.
//
// It is a covering optimization, not hours ÷ shift-length. The engine solves it
// with a left-to-right greedy: whenever the count of active shifts drops below
// demand, open a new shift that starts at that minute and runs as far right as
// the rules allow (min(now+MaxShiftHours, Close)). Starting at the deficit
// minute and maximizing right-reach is an exchange-argument-optimal placement.
//
// For the HLN parameters this returns 6 at a 10h cap. Note it returns 7 at an 8h
// cap — NOT 6 as spec R2 asserts: the close-shoulder relay cannot run past
// Close, so a bare pair of openers (pinned to Open+8h) and closers (pinned to
// Close-8h) leave a ~25-minute core band that a single mid shift can't lift to
// three, forcing a seventh instance. See POC-NOTES.md.
func MinDailyShiftInstances(f domain.Facility, r domain.RuleSet) (int, error) {
	dt, err := ComputeDemand(f, r)
	if err != nil {
		return 0, err
	}
	maxLen := domain.TimeOfDay(r.MaxShiftHours / time.Minute)
	open, close := f.OpenTime, f.CloseTime

	demandAt := func(t domain.TimeOfDay) int {
		if iv, ok := dt.At(t); ok {
			return iv.MinTotal
		}
		return 0
	}

	count := 0
	var ends []domain.TimeOfDay // end times of currently-active shifts
	for t := open; t < close; t++ {
		// Expire shifts that have ended by t.
		active := ends[:0]
		for _, e := range ends {
			if e > t {
				active = append(active, e)
			}
		}
		ends = active
		for len(ends) < demandAt(t) {
			end := t + maxLen
			if end > close {
				end = close
			}
			ends = append(ends, end)
			count++
		}
	}
	return count, nil
}
