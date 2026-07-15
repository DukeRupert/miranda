# ATC Facility Scheduling Engine — Implementation Specification

**Audience:** coding agent implementing from scratch
**Language/stack:** Go (standard library preferred; zero external dependencies for phases 1–3)
**Working style:** small, testable, git-committable steps. Each phase below is one or more
commits. Do not proceed to the next phase until the current phase's tests pass. Do not
implement ahead of the phase you are on.

---

## 1. Problem Statement

Build the domain model and coverage engine for an FAA ATC facility scheduling application.
The system must:

1. Accept a facility definition (operating hours, positions) and a parameterized rule set
   (labor constraints).
2. **Derive** the staffing demand timeline — how many bodies, with which qualifications,
   must be present at every minute of the operating day. Demand is computed from rules,
   never hand-entered.
3. Validate proposed schedules (fixed weekly lines assigned to controllers) against demand
   and labor rules, returning structured violations.
4. Materialize fixed lines + controller assignments + leave into a concrete two-week
   pay-period schedule with a gap report (uncovered shifts, qualification requirements for
   each gap, projected overtime).

Phases 1–3 are pure functions and types with no persistence and no UI. Persistence and UI
come later and are **out of scope for this document**.

### Critical domain insight (read this twice)

The constraint that shapes everything is the **maximum-time-on-position rule** (default
2 hours). It interacts with facility geometry and qualifications to *generate* requirements
that are never explicitly entered:

- It caps how long any window with exactly `MinStaffWhenOpen` bodies may last, which forces
  a minimum daily shift count larger than naive hours-division suggests.
- It forces **≥2 AP-capable controllers** through any continuous window longer than the cap
  (a lone AP-capable controller would be pinned on AP with no qualified relief).
- It forces **≥2 CIC-qualified controllers** through any such window (a lone CIC holder can
  never rotate to break, since CIC is a combined duty performed while working a position).

All three derivations must live in `ComputeDemand`, covered by tests. A schedule that looks
fine by headcount can be illegal by qualification pinning; the engine must catch this.

---

## 2. Domain Vocabulary

| Term | Definition |
|---|---|
| **Position** | A working station that must be staffed (e.g., AP, LC). |
| **CPC** | Controller qualified on all positions (here: AP + LC). |
| **LC-only** | Controller qualified only on LC. Useful for staffing but cannot hold AP. |
| **CIC** | Controller-in-Charge. An orthogonal qualification tag, not a position. It is a combined duty: the CIC must be actively working a position. At least one on-position body must hold CIC at all times while open. LC-only controllers CAN hold CIC (uncommon but legal — the model must support it). |
| **Shift template** | A start time + duration, e.g. 0545×8h. |
| **Line** | A fixed repeating weekly pattern of 7 slots, each a shift template or RDO (regular day off). Lines are the negotiated, contractual artifact — immutable per posting cycle. |
| **Shift-instance** | One body walking in the door for one shift. The unit of coverage supply/demand. |
| **Materialization** | Lines × controller assignments × leave → concrete dated schedule for one pay period. Leave and sick calls create exceptions against the materialized schedule; they never mutate lines. |
| **Demand timeline** | Computed sequence of intervals, each with a vector of minimum staffing requirements. |
| **Overlap shift** | A scheduled shift-instance beyond the daily minimum; absorbs absences. |

---

## 3. Types (Phase 1)

Package layout: `domain` (types), `coverage` (demand engine + satisfiability), `validate`
(schedule validators, phase 3), `materialize` (phase 5).

```go
package domain

import "time"

// TimeOfDay is minutes since midnight, 0–1439. Implement as its own type
// with parsing ("0545" -> 345), formatting, comparison, and Add(d time.Duration).
// Do NOT use time.Time for time-of-day values.
type TimeOfDay int

// Date is a calendar date without time zone concerns. Weekday() is required.
type Date struct{ Year int; Month time.Month; Day int }

type Capability string

const (
    CapAP  Capability = "AP"
    CapLC  Capability = "LC"
    CapCIC Capability = "CIC"
)

// QualSet is a controller's capabilities. CPC ≡ {AP:true, LC:true}.
// CIC is orthogonal: any combination of {AP,LC} may or may not include CIC.
type QualSet map[Capability]bool

type Position struct {
    ID   string // "AP", "LC"
    Name string
}

type Facility struct {
    ID        string
    Name      string
    OpenTime  TimeOfDay // e.g. 0545
    CloseTime TimeOfDay // e.g. 2210; assume Close > Open (no overnight) in v1,
                        // return a clear error if violated rather than panicking
    Positions []Position
}

// RuleSet is DATA, not code. Parameterized per facility, versionable.
type RuleSet struct {
    MaxShiftHours       time.Duration // 10h
    MaxDaysPerWindow    int           // 6
    WindowDays          int           // 7 (together: max 6 worked days in any rolling 7)
    RequiredHoursPerPP  time.Duration // 80h per two-week pay period, INCLUDING paid leave
    MaxTimeOnPosition   time.Duration // 2h
    MinBreak            time.Duration // 15m
    MinStaffWhenOpen    int           // 2 (equals position count here, but keep separate)
    PositionSwapIsBreak bool          // facility interpretation flag; see §4.4
}

type ShiftTemplate struct {
    ID       string // e.g. "E8" or "0545x8"
    Start    TimeOfDay
    Duration time.Duration
}

func (s ShiftTemplate) End() TimeOfDay // Start + Duration

// Line: 7 slots, index 0 = Sunday. nil = RDO.
type Line struct {
    ID   string
    Days [7]*ShiftTemplate
}

type Controller struct {
    ID     string
    Name   string
    Quals  QualSet
    LineID *string // nil = unassigned
}

type LeaveType string

const (
    LeaveAnnual LeaveType = "annual"
    LeaveSick   LeaveType = "sick"
    LeaveBid    LeaveType = "bid" // known at schedule-build time
)

// Leave is an input to materialization. Paid leave counts toward RequiredHoursPerPP.
type Leave struct {
    ControllerID string
    Date         Date
    Hours        time.Duration
    Type         LeaveType
}
```

Phase 1 deliverable: these types, `TimeOfDay` parsing/formatting/arithmetic with unit tests,
`QualSet` helpers (`HasAll`, `Superset`), and constructors that validate invariants
(Close > Open, Duration > 0, etc.). **Commit.**

---

## 4. Coverage Engine (Phase 2)

### 4.1 Demand timeline

```go
package coverage

type DemandInterval struct {
    Start, End domain.TimeOfDay
    MinTotal   int
    MinCapable map[domain.Capability]int // e.g. {AP:2, CIC:2} core, {AP:1, CIC:1} shoulders
}

type DemandTimeline []DemandInterval // contiguous, non-overlapping, covers Open..Close

// ComputeDemand derives the demand timeline from facility + rules. Pure function. No I/O.
func ComputeDemand(f domain.Facility, r domain.RuleSet) (DemandTimeline, error)
```

**Derivation logic:**

1. While open, `MinTotal ≥ MinStaffWhenOpen` everywhere (all positions staffed).
2. Any continuous window staffed at exactly `MinStaffWhenOpen` (no break capacity — everyone
   on position) may last at most `MaxTimeOnPosition`. The timeline therefore takes the
   shape: a shoulder of length ≤ `MaxTimeOnPosition` at open with
   `MinTotal = MinStaffWhenOpen`, a core with `MinTotal = MinStaffWhenOpen + 1` (break
   capacity), and a shoulder ≤ `MaxTimeOnPosition` at close. Core span =
   `(Close − Open) − 2×MaxTimeOnPosition` when positive.
3. Qualification minimums per interval:
   - Shoulders (duration ≤ `MaxTimeOnPosition`, no rotation required):
     `MinCapable = {AP:1, CIC:1}`.
   - Core (rotation required): `MinCapable = {AP:2, CIC:2}`. Rationale: AP must be handed
     off before the cap, so ≥2 AP-capable present; the CIC tag must remain on-position while
     every individual still receives breaks, so ≥2 CIC holders present. These are
     **derived**, mirrored consequences of the same rule — implement from rule parameters,
     not as literals.
4. Derive `MinCapable[LC]` generically ("bodies able to hold LC") so the engine doesn't
   silently assume the HLN qual structure.

Also provide:

```go
// MinDailyShiftInstances computes the minimum number of shift-instances per day achievable
// by ANY legal set of shift templates (lengths ≤ MaxShiftHours) covering the demand
// timeline. For HLN parameters this must return 6.
func MinDailyShiftInstances(f domain.Facility, r domain.RuleSet) (int, error)
```

Implementation guidance: the core (3-body region) has length
`L = (Close−Open) − 2×MaxTimeOnPosition`. If `L > MaxShiftHours`, no single shift covers it,
so the third-body region alone needs ≥2 shifts, giving a floor of 2 openers + 2 closers +
2 mid-coverage = 6 for HLN. Implement the general computation (interval covering with
bounded-length segments), not the hard-coded answer.

### 4.2 Satisfiability — assignment existence, NOT independent counting

```go
// Satisfies reports whether the given present controllers can legally staff the interval.
// This is an assignment-existence check: there must exist an assignment of bodies to
// {position slots..., break} such that
//   - every position slot is filled by a body qualified for that position,
//   - at least one ON-POSITION body holds CIC,
//   - counts meet MinTotal / MinCapable.
// Three independent count checks are INSUFFICIENT — see test (f).
func Satisfies(present []domain.QualSet, d DemandInterval) bool
```

Because the qual structure is a nested hierarchy (CPC ⊃ LC-only) plus one orthogonal tag
(CIC), full bipartite matching is unnecessary. Greedy scarcest-first assignment:

1. Fill AP slots from AP-capable bodies, preferring CIC holders only when CIC would
   otherwise be unsatisfiable on-position.
2. Fill LC slots from remaining bodies.
3. Verify ≥1 assigned (on-position) body holds CIC; if not, attempt one swap between an
   assigned non-CIC body and an unassigned (break) CIC body with compatible quals.
4. Any slot unfillable or CIC unplaceable → false.

Structure the code so a general matching implementation could replace the greedy one behind
the same signature (the greedy path is an optimization for hierarchical qual structures, not
the definition of correctness).

### 4.3 Line qualification requirements

```go
// LineQualRequirements computes, per line, the minimum QualSet a controller must hold to
// legally occupy that line, GIVEN the other lines and their occupants' quals. A line is
// "CPC-required" if downgrading its occupant to LC-only makes any interval in the week
// unsatisfiable; similarly for the CIC tag. The result can express the middle case
// {LC sufficient, CIC required}.
func LineQualRequirements(
    lines []domain.Line,
    occupants map[string]domain.QualSet, // lineID -> occupant quals
    f domain.Facility,
    r domain.RuleSet,
) (map[string]domain.QualSet, error)
```

Method: for each line and each candidate downgrade (drop AP; drop CIC), re-run `Satisfies`
over every demand interval of every day of the week with the hypothetical quals; the
requirement is the minimal QualSet under which all intervals remain satisfiable. Basis for
bid-eligibility filtering and the derived metric "how many LC-only controllers can this line
structure absorb."

### 4.4 The PositionSwapIsBreak flag

When `true`, swapping positions resets the on-position clock, relaxing derivations: 2-body
windows are no longer capped at `MaxTimeOnPosition` *provided both bodies are qualified to
swap into each other's positions*. Two CPCs may swap indefinitely; CPC + LC-only may NOT —
the CPC has nowhere to swap to that the LC-only can backfill, so AP remains continuously
theirs and the cap still applies. `ComputeDemand` and `Satisfies` must both honor the flag,
including this qualification subtlety. Default and all HLN fixtures use `false`. **Pin the
CPC+LC-only-still-capped behavior with a test so nobody "fixes" it later** — test (e).

Phase 2 commits: (1) `ComputeDemand` + `MinDailyShiftInstances` + scalar HLN regression
tests; (2) `Satisfies` + qual tests; (3) `LineQualRequirements` + tests. **Do not combine.**

---

## 5. Schedule Validators (Phase 3)

Pure functions over proposed lines + assignments. Each validator returns structured
violations, never booleans or panics.

```go
package validate

type Severity string

const (
    Illegal Severity = "illegal" // violates federal/contract rules
    Warning Severity = "warning" // legal but flagged (e.g. short turnaround)
)

type Violation struct {
    Rule     string // machine-readable, e.g. "max-shift-hours", "coverage-gap"
    Severity Severity
    LineID   string
    Date     *domain.Date
    Interval *coverage.DemandInterval
    Message  string
    Detail   map[string]any // e.g. {"required": {"AP":2}, "present": {"AP":1}}
}
```

Validators (each its own function, composed by `ValidateWeek` / `ValidatePayPeriod`):

1. **Shift length** — every template ≤ `MaxShiftHours`.
2. **Coverage** — for every minute of every day, the union of on-duty lines satisfies the
   demand interval via `coverage.Satisfies` using each occupant's actual `QualSet`. Report
   gaps with the missing capability vector in `Detail`.
3. **Six-of-seven** — no line (and, after assignment, no controller including cross-line
   substitutions) works more than `MaxDaysPerWindow` days in any rolling `WindowDays`
   window. Evaluate across week boundaries of the repeating pattern.
4. **Biweekly hours** — per controller: scheduled hours + paid leave =
   `RequiredHoursPerPP` exactly. Under/over → violation with delta.
5. **Turnaround** (Warning) — rest between consecutive shifts below a configurable threshold
   (default 10h); include computed rest in `Detail`.
6. **Line qualification** — assigned controller's `QualSet` ⊇ `LineQualRequirements` for
   their line.

Phase 3 deliverable: validators + runner + tests, including the HLN 9-line fixture (§8)
passing clean except known, asserted warnings. **Commit per validator or small groups.**

---

## 6. Pay-Period Materialization (Phase 5 — spec only; implement after 1–3 are green)

```go
package materialize

type ScheduledShift struct {
    Date         domain.Date
    ControllerID string
    Template     domain.ShiftTemplate
    Source       string // "line" | "exception"
}

type Gap struct {
    Date     domain.Date
    Template domain.ShiftTemplate
    Requires domain.QualSet // minimum quals a replacement must hold
    Reason   string         // "leave:annual", "leave:bid", "unassigned-line"
}

type PayPeriod struct {
    Start  domain.Date // 14 consecutive days
    Shifts []ScheduledShift
    Gaps   []Gap
    // Projected OT hours = sum of gap durations not absorbable by overlap instances
}

func Materialize(
    lines []domain.Line,
    controllers []domain.Controller,
    leave []domain.Leave,
    start domain.Date,
    f domain.Facility,
    r domain.RuleSet,
) (PayPeriod, error)
```

Rules: leave removes the controller from their line's shift-instances for the leave dates
and creates a `Gap` carrying the qualification requirements of the vacated instance — a
vacated CPC-required shift cannot be marked absorbable by an LC-only. Overlap instances
absorb gaps only when the overlap body's quals AND shift window actually cover the vacated
demand. Everything else surfaces as projected OT with the qualification filter attached, so
the callout list is pre-filtered to controllers who can legally take the shift.

---

## 7. Test Suite (non-negotiable; these encode the domain analysis)

### HLN regression fixture

```
Facility: HLN. Open 0545, Close 2210. Positions: AP, LC.
Rules: MaxShiftHours=10h, 6-of-7, 80h/PP, MaxTimeOnPosition=2h,
       MinBreak=15m, MinStaffWhenOpen=2, PositionSwapIsBreak=false.
```

**R1.** `ComputeDemand(HLN)` yields exactly: 0545–0745 `{total:2, AP:1, CIC:1}`;
0745–2010 `{total:3, AP:2, CIC:2}`; 2010–2210 `{total:2, AP:1, CIC:1}`. (Core = 12h15m.)
**R2.** `MinDailyShiftInstances(HLN)` = 6, and remains 6 whether `MaxShiftHours` is 8h or
10h (the 12h15m core exceeds either). Weekly minimum = 42.
**R3.** With `MaxShiftHours = 13h` (hypothetical), result drops to 5 — verifies the
computation is general, not hard-coded.

### Qualification tests

**(a)** All-CPC-with-CIC bodies: the HLN skeleton (2×0545×8, 0745×8, 1210×8, 2×1410×8)
fully satisfies R1's timeline via the coverage validator.
**(b)** Core interval: 2 CPC + 1 LC-only (all CIC) → true. 1 CPC + 2 LC-only (all CIC) →
false (AP pinning). Adding a 3rd LC-only → still false.
**(c)** Open shoulder (exactly 2h): 1 CPC + 1 LC-only (CPC holds CIC) → true.
**(d)** Sweep interval length across `MaxTimeOnPosition`: derived `MinCapable[AP]` flips
1→2 exactly at the boundary; same for CIC.
**(e)** `PositionSwapIsBreak = true`: two CPCs alone may hold a 3h window (true);
CPC + LC-only may NOT (false). Pin both.
**(f)** Counting-vs-assignment discrimination: a body set + interval where independent count
checks pass but no valid on-position assignment exists → `Satisfies` false; and the mirror
case where a naive pessimistic check would fail but a valid assignment exists
(LC-only(CIC) on LC as controller-in-charge, CPC(no CIC) on AP) → true.
**(g)** 4 bodies, only 1 CIC, window 3h > cap → false (lone CIC can never break). Same
bodies with 2 CIC → true.
**(h)** 2-person open shoulder: CPC(no CIC) + LC-only(CIC) → true, LC-only as CIC.
**(i)** In `ComputeDemand` output, `MinCapable[CIC] ≥ 2` appears on exactly the intervals
where `MinCapable[AP] ≥ 2` does.

### Validator tests

**V1.** 11h template → `max-shift-hours` Illegal.
**V2.** Remove the 1210 line from the skeleton → coverage violations reported precisely for
1545–2010, with the missing vector in `Detail`.
**V3.** A line working 7 consecutive days across the week boundary → `six-of-seven` Illegal.
**V4.** Controller with 72 scheduled hours + 8h annual leave in the PP → clean; 72 with no
leave → `biweekly-hours` Illegal, delta −8h.
**V5.** HLN 9-line fixture (§8): zero Illegal; exactly the known turnaround warnings,
including M1 Thursday 1210 → Friday 0745 (11h45m rest) — assert Warning, not Illegal.
**V6.** LC-only(no CIC) assigned to a line requiring `{LC, CIC}` → `line-qualification`
Illegal; LC-only(CIC) → clean.

### Property-based checks (recommended)

- Random legal facilities/rules: `MinDailyShiftInstances` is always achievable by an
  explicitly constructed template set, and never improvable by exhaustive search on small
  instances.
- `Satisfies` agrees with brute-force assignment enumeration for all body sets ≤ 6.

---

## 8. HLN 9-Line Reference Fixture

Templates: `E8` 0545×8h (ends 1345), `M8a` 0745×8h (ends 1545), `M8b` 1210×8h (ends 2010),
`L8` 1410×8h (ends 2210). Days Sun..Sat, `—` = RDO.

| Line | Sun | Mon | Tue | Wed | Thu | Fri | Sat |
|---|---|---|---|---|---|---|---|
| E1 | — | E8 | E8 | E8 | E8 | E8 | — |
| E2 | E8 | M8a | — | — | E8 | E8 | E8 |
| E3 | E8 | E8 | E8 | E8 | — | — | E8 |
| M1 | M8a | M8a | — | — | M8b | M8a | M8a |
| M2 | — | — | M8a | M8a | M8a | M8a | M8a |
| M3 | M8b | M8b | M8b | M8b | — | — | M8b |
| L1 | — | L8 | L8 | L8 | L8 | M8b | — |
| L2 | L8 | — | — | L8 | L8 | L8 | L8 |
| L3 | L8 | L8 | L8 | — | — | L8 | L8 |

Expected daily counts (E8/M8a/M8b/L8): Sun 2/1/1/2=6, Mon 2/2/1/2=7, Tue 6, Wed 6, Thu 6,
Fri 7, Sat 7. Weekly total 45 (42 minimum + 3 overlap on Mon/Fri/Sat). Every line:
5 workdays, 2 consecutive RDOs, 40h/week. Known warnings: M1 Thu→Fri turnaround 11h45m.
Non-warnings to spot-check: E2 Sun E8→Mon M8a = 18h rest; L1 Thu L8→Fri M8b = 14h rest.

Use this fixture for V5 and as seed data for phase-5 materialization tests (e.g., E1 on bid
leave for a week vacates 5 instances; Mon/Fri/Sat overlaps absorb up to 3 subject to
shift-window and qual compatibility; expected residual ≈ 2 gaps).

---

## 9. Explicit Non-Goals for This Pass

- **No intra-shift position rotation / break scheduling** (who is on LC 0800–1000). The
  2-hour rule enters only through §4's demand derivations. Daily rotation remains a human
  (CIC) task.
- **No persistence, no HTTP, no UI.** Pure packages + tests only.
- **No overnight facilities** (Close < Open) — return a clear error.
- **No schedule generation/optimization** (auto-building lines). Validation and
  materialization only; generation must not leak into these packages' APIs.

## 10. Engineering Conventions

- Go standard library only for phases 1–3. Table-driven tests. No global state; every
  function in §4–§6 is pure.
- Every derivation in §4.1 must be traceable to a `RuleSet` parameter — grep-test: the
  literals `2` and `3` for staffing minimums must not appear in coverage logic except via
  `MinStaffWhenOpen` arithmetic.
- Violations and demand intervals are the public API surface; keep them JSON-serializable
  (struct tags only, no encoding logic yet).
- Commit messages: one logical unit per commit, stating phase/step, e.g.
  `coverage: ComputeDemand + HLN regression R1–R3`.
