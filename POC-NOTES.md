# ATC Scheduling Engine — Proof of Concept

Status: **working end-to-end.** Built from `spec.md` on branch `poc/scheduling-engine`.

The point of this pass was to prove the core idea works: **define a facility's
operating hours, positions, and labor rules → have the engine _derive_ the
staffing demand → explore how a candidate schedule fares → see the projected
(scheduled) overtime liability.** All of that runs, with tests, and is drivable
from a web page.

## What you can do right now

```bash
mage dev          # build + run (or: go run ./cmd/server)
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

## Architecture

Pure engine packages (no I/O, standard library only — spec phases 1–3, 5):

| Package | Role |
|---|---|
| `internal/domain` | Value types: `TimeOfDay`, `Date`, `QualSet`, `Facility`, `RuleSet`, `ShiftTemplate`, `Line`, `Controller`, `Leave`. Constructors validate invariants. |
| `internal/coverage` | The derivation engine: `ComputeDemand`, `MinDailyShiftInstances`, `Satisfies`, `LineQualRequirements`, plus `DayCoverageGaps`. |
| `internal/validate` | Structured validators: shift length, coverage, six-of-seven, biweekly hours, turnaround, line qualification. Returns `Violation`s, never bare bools. |
| `internal/materialize` | Expands lines × controllers × leave into a dated pay period with a gap report and projected OT. |
| `internal/fixtures` | The HLN facility, rules, four shift templates, 9-line schedule, and controllers — shared by tests and the web UI. |

Web layer (on the existing Firefly template): `internal/handler/schedule.go` +
`internal/view/schedule.templ` + `internal/view/schedule.go`, wired at
`GET /explore`.

The load-bearing idea, implemented in `coverage`: the **max-time-on-position**
rule generates requirements nobody hand-enters. A window staffed at the bare
open-minimum has everyone pinned on a position, so it can't last longer than the
cap — which forces a higher-staffed "core" in the middle of the day, and forces a
_second_ AP-capable body and a _second_ CIC holder through that core (a lone one
of either can never be relieved for a break). All three fall out of the single
rule parameter; see the `breakRelief` constant and its comment.

## Test coverage

`go test ./...` — all green. The suite encodes the spec's non-negotiable §7 cases:

- **R1** demand timeline, **R2/R3** shift-count minimums.
- Qualification cases **(a)–(i)** for `Satisfies`, including a brute-force
  cross-check for all body sets up to size 4.
- Validator cases **V1–V6**.
- Materialization: baseline OT, leave-driven OT, and absorbed-overlap.

## Two places the engine disagrees with the spec (on purpose)

Both are cases where a faithful, minute-level model contradicts a number the spec
states. I implemented the correct behavior and asserted it in tests, because the
whole value of the engine is catching exactly this kind of thing. Worth a review
decision before this hardens.

### 1. `MinDailyShiftInstances` is 7 at an 8-hour cap, not 6 (spec R2)

Spec R2 says the daily minimum "remains 6 whether `MaxShiftHours` is 8h or 10h."
It's 6 at 10h but **7 at 8h**. A shift can't run past closing, so the two closers
are pinned to `Close−8h` and the two openers to `Open+8h`; that leaves a ~25-minute
band in the core (around 13:45–14:10) that a single mid-day shift can't lift to
three, forcing a seventh instance. The engine's greedy covering finds this; the
value is 6 at a 10h cap and 5 at a 13h cap, both matching the spec.

### 2. The 9-line reference fixture has recurring mid-day coverage dips

Under the spec's own (deliberately conservative) demand model — which requires the
_entire_ core be staffed at three — the reference 9-line schedule dips to two
bodies during the **13:45–14:10 handoff** on its four 6-instance days
(Sun/Tue/Wed/Thu). The E8 line has clocked out (13:45) and the L8 line hasn't
clocked in (14:10), leaving only the two M-shifts. The 7-instance days
(Mon/Fri/Sat) carry an extra M-shift and are clean.

So `ValidateWeek` on the reference fixture reports **4 coverage-gap Illegals**
(the dips) plus the **one known M1 Thu→Fri turnaround Warning** — not the "zero
Illegal" of spec V5. This is a real result, and it feeds directly into the OT
number: even with no leave, the schedule carries **3h20m of structural OT** per
pay period (eight 25-minute dips × one body).

Two ways to read this:

- **The schedule is genuinely thin at the handoff** and the org should know it
  (this is the tool doing its job), or
- **The demand model is too strict** — a brief sub-cap dip at a shift change may be
  acceptable in practice, and the model could be refined to allow a short 2-body
  window mid-core as long as no _continuous_ 2-body stretch exceeds the cap.

The second is a real modeling decision, not a bug; I left the conservative model
in place (it's what the spec specifies) and surfaced the consequence rather than
hiding it. The minor arithmetic slips in the spec (core "12h15m" is 12h25m; M1
turnaround "11h45m" is 11h35m) are noted in the relevant test comments.

## Notable smaller decisions

- **`Satisfies` signature** takes the facility positions and rule set in addition
  to the spec's `(present, interval)` — the position→capability map and the
  `PositionSwapIsBreak` flag are both needed and aren't recoverable from the
  interval alone.
- **`LineQualRequirements`** compares gap _sets_ (does dropping a capability add a
  gap?) rather than a bare satisfiable/not bool, so it stays meaningful even when
  the base schedule already dips somewhere else.
- **Projected OT** is reported as person-hours of uncovered demand
  (Σ deficit × band duration) — the minimum overtime coverage you'd have to buy.
  It includes structural holes, not just leave-induced ones (the honest total).
- **`flint-ui` not pulled in.** You offered it for components; since UI/UX wasn't
  the priority I kept the page to plain templ + Tailwind (matching the existing
  site) and avoided the extra dependency. Easy to adopt later if the UI grows.

## Out of scope (per spec §9), not built

No overnight facilities (rejected with a clear error), no intra-shift rotation /
break scheduling, no schedule _generation_/optimization, no persistence for the
scheduling domain (the engine is pure; the web page recomputes from the fixture
each request). Editing the candidate schedule from the browser is stubbed to the
HLN fixture — only the rules are editable in this pass.

## Suggested next steps

1. Decide the demand-model question in deviation #2 (conservative vs. refined).
2. Make the candidate schedule editable (persist lines/controllers/leave —
   the `internal/store` + sqlc layer is already here for it).
3. Add the property-based checks the spec recommends (random facilities;
   `MinDailyShiftInstances` achievability by construction).
