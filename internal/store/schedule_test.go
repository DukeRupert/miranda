package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/dukerupert/miranda/internal/domain"
	"github.com/dukerupert/miranda/internal/fixtures"
	"github.com/dukerupert/miranda/internal/materialize"
	vld "github.com/dukerupert/miranda/internal/validate"
	"github.com/dukerupert/miranda/migrations"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

// newTestStore spins up a fresh migrated SQLite database in a temp file.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if _, err := sqlDB.Exec("PRAGMA foreign_keys=ON;"); err != nil {
		t.Fatalf("pragma: %v", err)
	}
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("dialect: %v", err)
	}
	if err := goose.Up(sqlDB, "."); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return New(sqlDB)
}

// TestSeedHLNRoundTrip is the DB-path regression guard: seeding the HLN
// reference and reloading it through the store must reproduce the fixture's
// line/controller structure, and the reloaded schedule must validate clean
// (only the known M1 turnaround warning) and materialize to zero projected OT —
// mirroring spec V5 through the persistence layer.
func TestSeedHLNRoundTrip(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	id, err := st.SeedHLN(ctx, "HLN reference")
	if err != nil {
		t.Fatalf("SeedHLN: %v", err)
	}
	data, err := st.LoadScenario(ctx, id)
	if err != nil {
		t.Fatalf("LoadScenario: %v", err)
	}

	// Structure matches the fixture (order preserved via sort_order).
	fixLines := fixtures.HLNLines()
	if len(data.Lines) != len(fixLines) {
		t.Fatalf("line count = %d, want %d", len(data.Lines), len(fixLines))
	}
	for i, ln := range data.Lines {
		want := fixLines[i]
		if got := data.LineCodes[ln.ID]; got != want.ID {
			t.Errorf("line %d code = %q, want %q", i, got, want.ID)
		}
		for d := 0; d < 7; d++ {
			gotSlot, wantSlot := ln.Days[d], want.Days[d]
			if (gotSlot == nil) != (wantSlot == nil) {
				t.Errorf("line %s day %d: RDO mismatch (got nil=%v want nil=%v)", want.ID, d, gotSlot == nil, wantSlot == nil)
				continue
			}
			if gotSlot == nil {
				continue
			}
			if gotSlot.Start != wantSlot.Start || gotSlot.Duration != wantSlot.Duration {
				t.Errorf("line %s day %d: got %v+%v, want %v+%v",
					want.ID, d, gotSlot.Start, gotSlot.Duration, wantSlot.Start, wantSlot.Duration)
			}
		}
	}

	fixCtrls := fixtures.HLNControllers()
	if len(data.Controllers) != len(fixCtrls) {
		t.Fatalf("controller count = %d, want %d", len(data.Controllers), len(fixCtrls))
	}
	for i, c := range data.Controllers {
		want := fixCtrls[i]
		if c.Name != want.Name {
			t.Errorf("controller %d name = %q, want %q", i, c.Name, want.Name)
		}
		for _, cap := range []domain.Capability{domain.CapAP, domain.CapLC, domain.CapCIC} {
			if c.Quals[cap] != want.Quals[cap] {
				t.Errorf("controller %s qual %s = %v, want %v", want.Name, cap, c.Quals[cap], want.Quals[cap])
			}
		}
		if (c.LineID == nil) != (want.LineID == nil) {
			t.Errorf("controller %s assignment presence mismatch", want.Name)
		}
	}

	// Validates clean except the known M1 turnaround warning.
	f := fixtures.HLNFacility()
	rules := fixtures.HLNRules()
	vios, err := vld.ValidateWeek(data.Lines, data.Occupants, f, rules)
	if err != nil {
		t.Fatalf("ValidateWeek: %v", err)
	}
	illegal, warning := vld.Count(vios)
	if illegal != 0 {
		t.Errorf("illegal violations = %d, want 0: %+v", illegal, vios)
	}
	if warning != 1 {
		t.Errorf("warnings = %d, want 1 (M1 turnaround): %+v", warning, vios)
	}

	// Zero-leave reference materializes to zero projected OT.
	pay, err := materialize.Materialize(data.Lines, data.Controllers, data.Leave, data.Scenario.PPStart, f, rules)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if pay.ProjectedOT != 0 {
		t.Errorf("projected OT = %v, want 0", pay.ProjectedOT)
	}
}
