# Routing Weight Semantics Plan

Status: decision required before behavior change
Public API changes: labels/explanation first; scheduler behavior only after owner approval
Data migration: optional field rename or scheduler state

## Current Baseline

Provider repository queries and routing-preview sorting order lower `priority`
first, then higher `load_factor`, healthy/never-used/least-recently-used, then ID.
`stickySessionHashCandidates` only hashes within an exactly equal priority,
load-factor, and error group. Therefore `load_factor` is a scheduling preference
tier, not a proportional request weight. A high value can continuously precede
lower values inside the same priority.

## Task 1: Document And Measure Existing Semantics

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

Test priority, load factor, last use, errors, active concurrency, sticky session,
pool membership priority, fallback, and recovered accounts across long request
sequences. Routing Preview must explain the selected tier and tie breaker.

### Completion Criteria

Tests prove the current algorithm and documentation no longer implies weighted
distribution.

### Commit

`docs(routing): clarify load factor scheduling semantics`

## Task 2: Choose Rename Or Weighted Scheduling

### Goal

Obtain an owner decision based on measured tradeoffs.

### Dependencies

Task 1.

### Options

- Option A, recommended for current personal V1: preserve behavior and rename
  the field to `Scheduling preference` or `Capacity tier`. This is deterministic,
  stateless, and low-risk.
- Option B: implement weighted least connections using
  `active_requests / weight`, with LRU/ID tie breakers. This gives weight real
  capacity meaning without persistent round-robin state, but requires careful
  concurrency and restart tests.

Smooth weighted round robin is not recommended initially because state must be
shared or reconstructed and multi-instance semantics become misleading.

### Completion Criteria

The chosen semantics, compatibility path, and expected distribution tolerance
are approved before code changes.

## Task 3A: Rename The Existing Preference

### Goal

Align API/UI language without changing scheduling.

### Dependencies

Owner selects Option A.

### Files

- Modify: frontend labels/tests, manual, routing preview explanation
- Prefer keeping JSON/database `loadFactor` for compatibility in V1
- Migrate: none unless a later major version renames storage

### Verification

Run provider/backend tests, Bun checks/build, and Playwright modal/table review.

### Completion Criteria

Operators cannot reasonably infer proportional traffic distribution.

### Commit

`fix(routing): describe load factor as scheduling preference`

## Task 3B: Implement Weighted Least Connections

### Goal

Provide genuine weighted selection if Option B is approved.

### Dependencies

Owner selects Option B; Task 1 baselines and concurrency metrics exist.

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
