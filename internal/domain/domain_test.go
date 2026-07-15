package domain

import (
	"testing"
	"time"
)

func TestParseTimeOfDay(t *testing.T) {
	tests := []struct {
		in      string
		want    TimeOfDay
		wantErr bool
	}{
		{"0545", 345, false},
		{"05:45", 345, false},
		{"0000", 0, false},
		{"2359", 1439, false},
		{"2210", 1330, false},
		{"2400", 0, true}, // hour out of range
		{"0560", 0, true}, // minute out of range
		{"545", 0, true},  // wrong length
		{"abcd", 0, true}, // not numeric
	}
	for _, tt := range tests {
		got, err := ParseTimeOfDay(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseTimeOfDay(%q): expected error, got %v", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseTimeOfDay(%q): unexpected error %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseTimeOfDay(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestTimeOfDayRoundTrip(t *testing.T) {
	for _, s := range []string{"0000", "0545", "1210", "2210", "2359"} {
		tod := MustParseTimeOfDay(s)
		if got := tod.String(); got != s {
			t.Errorf("String round-trip: parsed %q -> %s", s, got)
		}
	}
}

func TestTimeOfDayAddSub(t *testing.T) {
	start := MustParseTimeOfDay("0545")
	if got := start.Add(8 * time.Hour); got != MustParseTimeOfDay("1345") {
		t.Errorf("0545 + 8h = %s, want 1345", got)
	}
	if got := start.Add(2 * time.Hour); got != MustParseTimeOfDay("0745") {
		t.Errorf("0545 + 2h = %s, want 0745", got)
	}
	rest := MustParseTimeOfDay("0745").Sub(MustParseTimeOfDay("2010").Add(-24 * time.Hour))
	_ = rest // Sub is exercised in turnaround tests; here just ensure it compiles/computes
	if d := MustParseTimeOfDay("2210").Sub(MustParseTimeOfDay("0545")); d != 16*time.Hour+25*time.Minute {
		t.Errorf("2210 - 0545 = %s, want 16h25m", d)
	}
}

func TestDateWeekday(t *testing.T) {
	// 2026-07-15 is a Wednesday.
	d := Date{Year: 2026, Month: time.July, Day: 15}
	if d.Weekday() != time.Wednesday {
		t.Errorf("Weekday(2026-07-15) = %v, want Wednesday", d.Weekday())
	}
	if got := d.AddDays(1); got.Weekday() != time.Thursday {
		t.Errorf("AddDays crossing: %v is %v, want Thursday", got, got.Weekday())
	}
	// Month/year rollover.
	if got := (Date{2026, time.December, 31}).AddDays(1); got != (Date{2027, time.January, 1}) {
		t.Errorf("AddDays rollover = %v, want 2027-01-01", got)
	}
}

func TestQualSet(t *testing.T) {
	cpc := CPC(true)
	if !cpc.HasAll(CapAP, CapLC, CapCIC) {
		t.Errorf("CPC(true) should hold AP, LC, CIC")
	}
	lc := LCOnly(false)
	if lc.Has(CapAP) {
		t.Errorf("LCOnly should not hold AP")
	}
	if !cpc.Superset(lc) {
		t.Errorf("CPC should be a superset of LC-only")
	}
	if lc.Superset(cpc) {
		t.Errorf("LC-only should not be a superset of CPC")
	}
	// Without/Clone independence.
	down := cpc.Without(CapAP)
	if down.Has(CapAP) {
		t.Errorf("Without(AP) should drop AP")
	}
	if !cpc.Has(CapAP) {
		t.Errorf("Without must not mutate the original")
	}
}

func TestNewFacility(t *testing.T) {
	pos := []Position{{ID: "AP", Requires: CapAP}, {ID: "LC", Requires: CapLC}}
	if _, err := NewFacility("HLN", "Helena", MustParseTimeOfDay("0545"), MustParseTimeOfDay("2210"), pos); err != nil {
		t.Errorf("valid facility rejected: %v", err)
	}
	// Overnight (close <= open) must error, not panic.
	if _, err := NewFacility("X", "X", MustParseTimeOfDay("2200"), MustParseTimeOfDay("0600"), pos); err == nil {
		t.Errorf("overnight facility should be rejected")
	}
	// No positions.
	if _, err := NewFacility("X", "X", MustParseTimeOfDay("0600"), MustParseTimeOfDay("2200"), nil); err == nil {
		t.Errorf("facility with no positions should be rejected")
	}
}

func TestFacilityPositionsRequiring(t *testing.T) {
	f := Facility{Positions: []Position{{ID: "AP", Requires: CapAP}, {ID: "LC", Requires: CapLC}}}
	if got := f.PositionsRequiring(CapAP); got != 1 {
		t.Errorf("PositionsRequiring(AP) = %d, want 1", got)
	}
	if got := f.PositionsRequiring(CapLC); got != 1 {
		t.Errorf("PositionsRequiring(LC) = %d, want 1", got)
	}
}

func TestShiftTemplateEnd(t *testing.T) {
	s, err := NewShiftTemplate("E8", MustParseTimeOfDay("0545"), 8*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.End() != MustParseTimeOfDay("1345") {
		t.Errorf("E8 ends %s, want 1345", s.End())
	}
	if _, err := NewShiftTemplate("bad", 0, 0); err == nil {
		t.Errorf("zero-duration template should be rejected")
	}
}
