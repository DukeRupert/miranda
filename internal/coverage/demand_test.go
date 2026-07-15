package coverage_test

import (
	"testing"
	"time"

	"github.com/dukerupert/miranda/internal/coverage"
	"github.com/dukerupert/miranda/internal/domain"
	"github.com/dukerupert/miranda/internal/fixtures"
)

// R1: ComputeDemand(HLN) yields exactly the three-interval shoulder/core/shoulder
// timeline. (Spec §7 R1.)
func TestComputeDemand_HLN_R1(t *testing.T) {
	dt, err := coverage.ComputeDemand(fixtures.HLNFacility(), fixtures.HLNRules())
	if err != nil {
		t.Fatalf("ComputeDemand: %v", err)
	}
	want := []coverage.DemandInterval{
		{Start: tod("0545"), End: tod("0745"), MinTotal: 2, MinCapable: map[domain.Capability]int{domain.CapAP: 1, domain.CapLC: 1, domain.CapCIC: 1}},
		{Start: tod("0745"), End: tod("2010"), MinTotal: 3, MinCapable: map[domain.Capability]int{domain.CapAP: 2, domain.CapLC: 1, domain.CapCIC: 2}},
		{Start: tod("2010"), End: tod("2210"), MinTotal: 2, MinCapable: map[domain.Capability]int{domain.CapAP: 1, domain.CapLC: 1, domain.CapCIC: 1}},
	}
	if len(dt) != len(want) {
		t.Fatalf("got %d intervals, want %d: %+v", len(dt), len(want), dt)
	}
	for i, w := range want {
		g := dt[i]
		if g.Start != w.Start || g.End != w.End || g.MinTotal != w.MinTotal {
			t.Errorf("interval %d = %s-%s total=%d, want %s-%s total=%d", i, g.Start, g.End, g.MinTotal, w.Start, w.End, w.MinTotal)
		}
		for c, n := range w.MinCapable {
			if g.MinCapable[c] != n {
				t.Errorf("interval %d MinCapable[%s] = %d, want %d", i, c, g.MinCapable[c], n)
			}
		}
	}
	// Core span is 0745..2010 = 12h25m (> both an 8h and 10h shift).
	if got := dt[1].Duration(); got != 12*time.Hour+25*time.Minute {
		t.Errorf("core duration = %s, want 12h25m", got)
	}
}

// (i): MinCapable[CIC] >= 2 appears on exactly the intervals where
// MinCapable[AP] >= 2 — the two are mirrored consequences of one rule.
func TestComputeDemand_CICMirrorsAP(t *testing.T) {
	dt, _ := coverage.ComputeDemand(fixtures.HLNFacility(), fixtures.HLNRules())
	for _, iv := range dt {
		apHi := iv.MinCapable[domain.CapAP] >= 2
		cicHi := iv.MinCapable[domain.CapCIC] >= 2
		if apHi != cicHi {
			t.Errorf("interval %s-%s: AP>=2 is %v but CIC>=2 is %v", iv.Start, iv.End, apHi, cicHi)
		}
	}
}

// (d): sweeping the operating window across 2*MaxTimeOnPosition flips the derived
// core minimums 1->2 exactly at the boundary.
func TestComputeDemand_BoundaryFlip(t *testing.T) {
	r := fixtures.HLNRules() // MaxTimeOnPosition = 2h
	pos := []domain.Position{{ID: "AP", Requires: domain.CapAP}, {ID: "LC", Requires: domain.CapLC}}

	// Window exactly 2*cap = 4h: no core, single low interval, AP/CIC == 1.
	fEq, _ := domain.NewFacility("EQ", "eq", tod("0600"), tod("1000"), pos)
	dtEq, _ := coverage.ComputeDemand(fEq, r)
	if len(dtEq) != 1 {
		t.Fatalf("window == 2*cap should collapse to 1 interval, got %d", len(dtEq))
	}
	if dtEq[0].MinCapable[domain.CapAP] != 1 || dtEq[0].MinCapable[domain.CapCIC] != 1 {
		t.Errorf("collapsed interval should have AP=1 CIC=1, got %v", dtEq[0].MinCapable)
	}

	// One minute past 2*cap: a one-minute core appears with AP/CIC == 2.
	fHi, _ := domain.NewFacility("HI", "hi", tod("0600"), tod("1001"), pos)
	dtHi, _ := coverage.ComputeDemand(fHi, r)
	if len(dtHi) != 3 {
		t.Fatalf("window > 2*cap should produce 3 intervals, got %d", len(dtHi))
	}
	core := dtHi[1]
	if core.MinCapable[domain.CapAP] != 2 || core.MinCapable[domain.CapCIC] != 2 {
		t.Errorf("core just past boundary should have AP=2 CIC=2, got %v", core.MinCapable)
	}
}

// R2/R3: MinDailyShiftInstances is the general covering minimum.
//
// NOTE ON SPEC DEVIATION: spec R2 asserts the answer stays 6 whether the shift
// cap is 8h or 10h. A faithful minute-level model gives 6 at 10h but 7 at 8h —
// the close-shoulder relay can't run past Close, so the openers/closers can't
// stretch to cover the mid-core band, forcing a 7th instance. We assert the
// engine's correct values. See POC-NOTES.md.
func TestMinDailyShiftInstances(t *testing.T) {
	f := fixtures.HLNFacility()
	cases := []struct {
		hours time.Duration
		want  int
	}{
		{10, 6}, // spec R2
		{8, 7},  // spec R2 says 6; correct value is 7 (documented deviation)
		{13, 5}, // spec R3
	}
	for _, tc := range cases {
		r := fixtures.HLNRules()
		r.MaxShiftHours = tc.hours * time.Hour
		got, err := coverage.MinDailyShiftInstances(f, r)
		if err != nil {
			t.Fatalf("@%dh: %v", tc.hours, err)
		}
		if got != tc.want {
			t.Errorf("MinDailyShiftInstances @%dh = %d, want %d", tc.hours, got, tc.want)
		}
	}
}

func TestComputeDemand_OvernightRejected(t *testing.T) {
	pos := []domain.Position{{ID: "AP", Requires: domain.CapAP}}
	// Bypass the NewFacility guard to feed ComputeDemand a bad facility directly.
	bad := domain.Facility{ID: "X", OpenTime: tod("2200"), CloseTime: tod("0600"), Positions: pos}
	if _, err := coverage.ComputeDemand(bad, fixtures.HLNRules()); err == nil {
		t.Errorf("overnight facility should be rejected by ComputeDemand")
	}
}

func tod(s string) domain.TimeOfDay { return domain.MustParseTimeOfDay(s) }
