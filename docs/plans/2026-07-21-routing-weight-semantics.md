# Scheduling Preference Semantics Plan

Status: Tasks 1 and 3A locally completed; strict scheduling-preference semantics selected
Public API changes: operator-facing labels and diagnostics renamed; `loadFactor` and scheduler behavior unchanged
Data migration: none

## Current Baseline

Provider repository queries and routing-preview sorting order lower pool
membership priority when scoped, lower account/global priority, higher
`load_factor`, no-recent-error before recent-error, never-used or
least-recently-used, then ID. `stickySessionHashCandidates` only hashes within
the highest exactly equal pool priority, account/global priority, scheduling
preference, and recent-error group. The Go, JSON/API, and PostgreSQL compatibility
identifiers remain `LoadFactor`, `loadFactor`, and `load_factor`. Operator-facing
UI and API diagnostic text uses **Scheduling preference** and **scheduling
preference tier**. This is a strict descending tier inside the same priority,
not a proportional request weight: a high value can continuously precede lower
values.

## Task 1: Document And Measure Existing Semantics

Status: locally completed on 2026-07-21

### Goal

Make current behavior explicit and quantify its distribution.

### Dependencies

Gateway E2E harness.

### Files

- Modify: provider scheduler tests and routing-preview documentation/UI copy
- Create: deterministic/statistical scheduler test helper
- Modify: `docs/manual.md`
- Migrate: none

### Implementation

Test priority, scheduling preference (`loadFactor`), last use, errors, active
concurrency, sticky session, pool membership priority, fallback, and recovered
accounts across long request sequences. Routing Preview must explain the
selected tier and tie breaker.

### Completion Criteria

Tests prove the current algorithm and documentation no longer implies weighted
distribution.

### Local Completion Evidence

- Existing focused tests cover account priority, scheduling preference,
  least-recently-used ordering, recent errors, concurrency exclusion and retry,
  sticky bindings and hashes, pool membership priority, fallback chains, and
  recovered accounts.
- A deterministic 10,000-session preview baseline proves that a lower
  scheduling preference tier receives zero selections while two accounts in the
  equal highest tier are stably distributed by the existing FNV session hash.
- Routing Preview schedule reasons now expose the actual pool/global priority,
  scheduling preference, recent-error, least-recently-used, account-ID, and
  sticky tie-breakers without changing scheduler behavior.

### Commit

`docs(routing): clarify scheduling preference semantics`

## Task 2: Confirm Strict Scheduling Preference Semantics

Status: completed; strict scheduling-preference semantics selected

### Goal

Record the selected operator contract based on measured scheduler behavior.

### Dependencies

Task 1.

### Decision

- Selected for personal V1: preserve behavior and use **Scheduling preference**
  in the UI and **scheduling preference tier** in API diagnostics. Keep
  `loadFactor` for compatibility. This is deterministic, stateless, and
  low-risk.
- Not selected: proportional weighted scheduling or weighted least connections.
  The configured value does not describe traffic share or account capacity.

Smooth weighted round robin is not recommended initially because state must be
shared or reconstructed and multi-instance semantics become misleading.

### Completion Criteria

The strict descending tier semantics and compatibility path are explicit, and
operators are not led to expect proportional distribution.

## Task 3A: Rename The Existing Preference

Status: locally completed on 2026-07-23

### Goal

Align UI and API diagnostic language without changing scheduling.

### Dependencies

Task 2 decision.

### Files

- Modify: frontend labels/tests, manual, routing preview explanation
- Keep JSON/API `loadFactor` and database `load_factor` for compatibility in V1
- Migrate: none

### Verification

Run provider/backend tests, Bun checks/build, and Playwright modal/table review.

### Completion Criteria

Operators cannot reasonably infer proportional traffic distribution.

### Local Completion Evidence

- Provider-account controls display **Scheduling preference** while requests and
  responses retain `loadFactor`.
- Routing Preview diagnostic text reports a **scheduling preference tier**.
- The manual states that higher values form a strict descending tier within the
  same priority and are not proportional request weights.

### Commit

`fix(routing): describe strict scheduling preference tiers`

## Task 3B: Rejected Weighted Least-Connections Alternative

Status: not selected; requires a new owner decision before implementation

### Goal

Provide genuine weighted selection only if the current strict-tier decision is
explicitly reopened.

### Dependencies

New owner approval; Task 1 baselines and concurrency metrics exist.

### Files

- Modify: `backend/internal/provider/service.go`, scheduler/store ordering, tests
- Modify: `backend/internal/gateway/proxy.go` concurrency handoff if needed
- Modify: routing preview API/UI and manual
- Migrate: none if the existing 1-100 field is retained as weight

### Implementation

Keep priority as the first tier, filter health/capability, prefer sticky binding
when eligible, then minimize normalized active load by weight. Define exact
fallback/recovery and deterministic ties. Do not persist per-request scheduler
state.

### Tests And Verification

Run deterministic concurrency tests and at least 10,000 seeded selections;
assert configured vs observed proportions within an approved tolerance, plus
fault/recovery/fallback/sticky cases.

### Compatibility And Security

This changes traffic distribution and must have release notes and a runtime
compatibility switch for one release.

### Risks And Rollback

Weight changes can overload accounts. Restore the legacy scheduler switch.

### Manual Acceptance

Required against multiple non-production accounts.

### Completion Criteria

Observed allocation matches documented semantics without violating concurrency.

### Commit

`feat(routing): implement weighted least-connections scheduling`
