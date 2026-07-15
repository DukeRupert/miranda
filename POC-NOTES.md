# ATC Scheduling Engine — Proof of Concept

Status: **working end-to-end.** Built from `spec.md` on branch `poc/scheduling-engine`.

The point of this pass was to prove the core idea works: **define a facility's
operating hours, positions, and labor rules → have the engine _derive_ the
staffing demand → explore how a candidate schedule fares → see the projected
(scheduled) overtime liability.** All of that runs, with tests, and is drivable
from a web page.

## What you can do right now

```bash
mage installtailwind   # one-time, downloads the Tailwind CLI (gitignored)
mage dev               # build + run
# then open http://localhost:8080/explore
```

`/explore` lets you edit the operating window, minimum staffing, max time on
position, and max shift length, then shows — recomputed server-side on submit:

- the **derived demand timeline** (shoulder / core / shoulder, with per-interval
  headcount and qualification minimums),
- the **minimum shifts per day / week** that can cover it,
- how the **HLN 9-line reference schedule** validates under those rules
  (illegal / warning list),
- the **projected overtime liability** for a materialized two-week pay period,
  with an optional "E1 on bid leave" scenario to see leave drive OT up.

Everything the page shows is also exercised by `go test ./...`.

## The staffing model (read this first)

The rule that shapes everything is the **maximum-time-on-position** cap (default
2h). The engine enforces it as a *rotation-aware* rule, not a static per-minute
headcount:

> At all times the facility is open it needs at least `MinStaffWhenOpen` bodies
> with every position fillable and a CIC on position. Beyond that, **no
> continuous bare-minimum window may run longer than `MaxTimeOnPosition`** —
> because a bare-minimum window has everyone pinned on a position with no relief.
> The same applies per qualification: no window with only one AP-capable body,
> and none with only one CIC holder, may exceed the cap.

The consequence is the familiar shoulder / core / shoulder shape — the middle of
the day needs a relief body (and a second AP-capable, and a second CIC) so people
can rotate off — **but brief sub-cap dips at shift handoffs are legal.** A
schedule can momentarily drop to two bodies during a shift change and still be
valid, as long as that dip is shorter than the cap. `ComputeDemand` produces the
shoulder/core/shoulder timeline as the *target* staffing (useful to visualize);
the coverage validator enforces the window rule against the *actual* schedule.

This matters, and it was a mid-build correction: an earlier version implemented
the coarse "the core must be staffed at three *everywhere*" reading. That is
stricter than the real rule and produced two wrong answers — `MinDailyShiftInstances`
came out 7 at an 8h cap (should be 6), and the 9-line reference fixture looked
like it had recurring coverage gaps at the 13:45–14:10 handoff (it doesn't; that
25-minute dip is legal). The rotation-aware rule fixes both, and the engine now
agrees with the spec's R2 (6 shifts at 8h and 10h) and V5 (fixture validates
clean). Credit to the domain review that caught it with an explicit 6-shift
counterexample.

## Architecture

Pure engine packages (no I/O, standard library only — spec phases 1–3, 5):

| Package | Role |
|---|---|
| `internal/domain` | Value types: `TimeOfDay`, `Date`, `QualSet`, `Facility`, `RuleSet`, `ShiftTemplate`, `Line`, `Controller`, `Leave`. Constructors validate invariants. |
| `internal/coverage` | The derivation engine: `ComputeDemand`, `MinDailyShiftInstances`, `Satisfies`, `LineQualRequirements`, plus `DayCoverageGaps` (snapshot + tight-run checks). |
| `internal/validate` | Structured validators: shift length, coverage, position-cap, six-of-seven, biweekly hours, turnaround, line qualification. Returns `Violation`s, never bare bools. |
| `internal/materialize` | Expands lines × controllers × leave into a dated pay period with a gap report and projected OT. |
| `internal/fixtures` | The HLN facility, rules, four shift templates, 9-line schedule, and controllers — shared by tests and the web UI. |

Web layer (on the existing Firefly template): `internal/handler/schedule.go` +
`internal/view/schedule.templ` + `internal/view/schedule.go`, wired at
`GET /explore`.

### How coverage validation works (the interesting part)

`coverage.DayCoverageGaps` evaluates one day in two passes over the atomic time
slices (cut at every shift boundary):

1. **Snapshot** — each slice must have `≥ MinStaffWhenOpen` bodies and a valid
   position assignment with a CIC on position (a pure count is insufficient: the
   only CIC holder might qualify for no position and be stuck on break).
2. **Continuous tight-runs** — a maximal run of slices that is tight on total
   headcount, on AP-capable bodies, or on CIC holders is a **position-cap breach**
   only if it runs longer than `MaxTimeOnPosition`. Sub-cap runs (the legal
   handoff dips) pass.

`Satisfies` is the single-window version of the same logic: snapshot feasibility
always, plus rotation relief (a spare body, a second AP-capable, a second CIC)
only when the window exceeds the cap — waived when `PositionSwapIsBreak` is set
and every body can swap into every position.

## Test coverage

`go test ./...` — all green. The suite encodes the spec's §7 cases:

- **R1** demand timeline; **R2/R3** shift-count minimums (6 at 8h and 10h, 5 at 13h).
- Qualification cases **(a)–(i)** for `Satisfies`, including a brute-force
  cross-check for all body sets up to size 4.
- Validator cases **V1–V6** (V2 now asserts a position-cap breach when a mid
  shift is removed; V5 asserts the fixture is clean bar the known M1 warning).
- Materialization: zero-OT clean baseline, leave-driven OT, and absorbed-overlap.

## Notable decisions

- **`Satisfies` signature** takes the facility positions and rule set in addition
  to the spec's `(present, interval)` — the position→capability map, the
  on-position cap, and the swap flag are all needed and none are recoverable from
  the interval's staffing vector alone.
- **`MinDailyShiftInstances`** uses the closed form
  `2·MinStaffWhenOpen + ceil(max(0, W−2·cap) / (MaxShiftHours + cap))` for a
  standard day (longer than one shift): openers + closers + a single relief layer
  whose shifts each reach `MaxShiftHours + cap` of core (shift length plus one
  legal dip). Exact for the standard operating shape; short/degenerate days are
  handled separately. Verified against 6/6/5 at 8h/10h/13h.
- **`LineQualRequirements`** compares gap *sets* (does dropping a capability add a
  gap?) rather than a bare satisfiable/not bool, so it stays meaningful even when
  the base schedule already has an unrelated breach.
- **Projected OT** is person-hours of uncovered demand: a snapshot gap costs its
  deficit × its span; a position-cap breach costs one relief body × the time it
  runs over the cap. The clean reference schedule therefore carries **zero**
  structural OT; OT appears only from leave/vacancies.
- **`flint-ui` not pulled in.** You offered it for components; since UI/UX wasn't
  the priority I kept the page to plain templ + Tailwind (matching the existing
  site) and avoided the extra dependency. Easy to adopt later if the UI grows.

## Out of scope (per spec §9), not built

No overnight facilities (rejected with a clear error), no intra-shift rotation /
break *scheduling* (the engine checks that a legal rotation *exists* via the
window rule, but doesn't produce the minute-by-minute who's-on-which-position
plan), no schedule generation/optimization, and no persistence for the scheduling
domain (the engine is pure; the web page recomputes from the fixture each
request). Editing the candidate schedule from the browser is stubbed to the HLN
fixture — only the rules are editable in this pass.

## Suggested next steps

1. Make the candidate schedule editable (persist lines/controllers/leave — the
   `internal/store` + sqlc layer is already here for it).
2. Generalize `MinDailyShiftInstances` to the constructive-plus-verify form for
   arbitrary position counts / staffing minimums (the closed form covers the
   standard shape; edge geometries deserve a check).
3. Add the property-based checks the spec recommends (random facilities;
   `MinDailyShiftInstances` achievability by construction and non-improvability by
   exhaustive search on small instances).
