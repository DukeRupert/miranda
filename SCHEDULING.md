# ATC Scheduling Engine — status & backlog

Canonical reference for the scheduling feature. Replaces the old `POC-NOTES.md`
now that the proof of concept has graduated. For build/run/layering conventions
see `CLAUDE.md`; the domain spec is `spec.md` (untracked).

## Current state (as of the editable-scenarios merge)

- **Pure engine** in `internal/{domain,coverage,validate,materialize}` — no I/O,
  standard library only. `internal/fixtures` holds the shared HLN reference data.
- **Explorer UI** at `GET /explore` (`internal/handler/schedule.go`,
  `internal/view/schedule.templ`).
- **Editable + persisted candidate schedule.** Lines, controllers, and leave are
  user-editable from `/explore` and persisted in SQLite as **named scenarios**
  (`?scenario=N`): migration `003`, `queries/scheduling.sql`, and
  `internal/store/schedule.go` (`LoadScenario` / `SeedHLN` / `DuplicateScenario`
  + CRUD). The store maps DB integer PKs to the engine's string IDs, so the
  **engine packages stay pure and never import `db`**. `SeedHLN` reseeds the
  reference in one click (clean, zero-OT baseline). Guard test:
  `internal/store/schedule_test.go`.
- `go test ./...` green.

## The rotation-aware staffing model (load-bearing — do not regress)

Referenced by `internal/coverage/demand.go` and `internal/coverage/satisfies.go`.

The rule that shapes everything is the **maximum-time-on-position** cap (default
2h), enforced as a *rotation-aware* rule, not a static per-minute headcount:

> While open, the facility needs at least `MinStaffWhenOpen` bodies with every
> position fillable and a CIC on position. Beyond that, **no continuous
> bare-minimum window may run longer than `MaxTimeOnPosition`** — a bare-minimum
> window has everyone pinned on a position with no relief. The same applies per
> qualification: no window with only one AP-capable body, and none with only one
> CIC holder, may exceed the cap.

Consequence: the familiar shoulder / core / shoulder shape — the middle of the
day needs a relief body (and a second AP-capable, and a second CIC) so people can
rotate off — **but brief sub-cap dips at shift handoffs are legal.** A schedule
can momentarily drop to two bodies during a shift change and stay valid, as long
as the dip is shorter than the cap. `ComputeDemand` produces the timeline as the
*target* staffing; the coverage validator enforces the window rule against the
*actual* schedule.

This was a mid-build correction. An earlier version used the coarse "the core
must be staffed at three *everywhere*" reading, which is stricter than the real
rule and gave two wrong answers — `MinDailyShiftInstances` came out 7 at an 8h
cap (should be 6), and the 9-line fixture looked like it had recurring gaps at
the 13:45–14:10 handoff (it doesn't; that 25-minute dip is legal). The
rotation-aware rule fixes both, so the engine agrees with spec R2 (6 shifts at 8h
and 10h), R3 (5 at 13h), and V5 (fixture validates clean bar the known M1
warning).

**If you touch `coverage`/`Satisfies`/`MinDailyShiftInstances`:** preserve the
window-rule semantics and those R2/R3/V5 answers. Test facilities smaller than
2×cap must use a window ≤ cap or they trip a legitimate position-cap breach.

## Backlog

Work items to pick up next, roughly in priority order.

### 1. Resolve the two engine-vs-spec numeric deviations — *needs a product decision*

CLAUDE.md and the project memory note that the engine deliberately disagrees with
the spec's stated numbers in two places, pending a call on whether to make the
engine match the spec or update the spec to match the (better-reasoned) engine.
**The exact pair is not written down** — before acting, pin down and record which
two. Concrete candidates:

- **Materialization residual.** Spec §8 says E1 on bid leave for a week leaves
  "residual ≈ 2 gaps"; the engine currently reports 5 unabsorbed instances
  (~17h15m projected OT) for that scenario. Confirm which is intended.
- **Demand/shift-count derivation.** The rotation-aware model departs from the
  spec §4.1 prose even though it matches the spec's R2/R3/V5 *test numbers* —
  check whether any stated number in §4.1/§6 still diverges.

Decision owner: product/domain. Once decided, encode it in a test so it can't
silently drift.

### 2. Generalize `MinDailyShiftInstances`

Move from the closed form to the constructive-plus-verify form so it is exact for
arbitrary position counts and staffing minimums, not just the standard HLN shape.
The current closed form covers the standard operating geometry; edge geometries
(short/degenerate days, >2 positions) deserve the general computation. See the
formula and caveats in `internal/coverage/demand.go`.

### 3. Property-based checks (spec §7 "recommended")

- Random legal facilities/rules: `MinDailyShiftInstances` is always achievable by
  an explicitly constructed template set, and never improvable by exhaustive
  search on small instances.
- `Satisfies` agrees with brute-force assignment enumeration for all body sets
  ≤ 6 (a bounded version already exists in `satisfies_test.go`; broaden it).

### 4. Persist the rule set + facility per scenario

Today the `RuleSet` and facility open/close are an ephemeral GET query-param
overlay on `/explore` that resets when you switch scenarios. Persist them per
scenario (new columns on `scenarios`, or a `rulesets` table) so a saved scenario
carries its own rules and facility window.

### 5. Access control (if this graduates from POC)

Editing routes are currently fully open (a POC decision), though still under CSRF.
If `/explore` becomes shared/multi-user, wrap the mutating routes in
`session.RequireAuth` (view can stay public) — mirrors the repo's
"admins via `mage seed`" model.

## Out of scope (spec §9)

No overnight facilities (rejected with a clear error), no intra-shift rotation /
break *scheduling* (the engine checks that a legal rotation *exists* via the
window rule, but doesn't produce the minute-by-minute plan), and no schedule
generation/optimization.
