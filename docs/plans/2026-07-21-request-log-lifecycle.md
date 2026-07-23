# Request Log Lifecycle Plan

Status: completed locally on 2026-07-21
Public API changes: additive cursor fields; export limits become explicit
Data migration: cursor/query index changes only after measurement

## Evidence Status (2026-07-23)

| Dimension | Status | Evidence and remaining gate |
| --- | --- | --- |
| `design` | complete | Cursor paging, URL-backed UI state, bounded retention, streaming export, and measured index contracts are defined. |
| `implementation` | complete | Local commits `3084733`, `e9aea72`, `1a5be6e`, `8eb9b10`, and `37526b3` implement Tasks 1-5; `7e9d5d5`, `5c2a9b4`, `00aab64`, and `05c5083` close correlation, independent upstream IDs, and write-failure observability. |
| `merged` | partial | The five planned lifecycle commits are on GitHub `main` at `3664abe`; the three later observability commits remain local. |
| `local_tests` | complete | Local tests cover signed cursors, filtering/paging, retention locks and cancellation, bounded CSV/JSONL export, representative query plans, independent upstream IDs, and best-effort write-failure state. Commit `226951f` adds PostgreSQL-backed concurrent correlation, failure, recovery-health, response-preservation, and redaction acceptance coverage. |
| `ci` | pending | No GitHub Actions run contains the local commits. |
| `release_artifact` | pending | No tested release digest contains the lifecycle changes. |
| `operator_acceptance` | pending | Validate representative production-scale query plans and export behavior, then enable retention only after accepted real-backup restore evidence. |
| `owner_decision` | complete | Request Log writes remain best effort and retention enablement remains an explicit operator action. |

## Current Baseline

`AdminRepository.ListRequestLogs` orders by `(created_at DESC,id DESC)` and
returns a fixed `LIMIT`; the service clamps to 200. The UI slices those rows
locally. Export supports JSON/CSV/JSONL but first loads the same bounded slice,
so exports silently stop at 200. Retention is a manual unbounded delete in one
transaction. System Events already provide the preferred signed cursor and
batched retention patterns. The live local database had only 42 Request Log
rows during this review, so it cannot justify speculative indexes.

## Task 1: Add A Signed Cursor Page Contract

Status: completed locally on 2026-07-21; representative index measurement
remains deferred to Task 5.

### Goal

Page older rows stably with existing filters and no offset scans.

### Dependencies

None.

### Files

- Modify: `backend/internal/admin/service.go`, `service_test.go`
- Modify: `backend/internal/store/admin.go`, `admin_test.go`
- Modify: `backend/internal/httpapi/server.go`, `server_test.go`
- Create: `backend/internal/store/migrations/00039_request_log_cursor_index.sql` if EXPLAIN shows the existing index is insufficient
- Test: service, store PostgreSQL integration, HTTP, migration
- Document: `docs/manual.md`

### Implementation

1. Add `Cursor` and a `RequestLogPage` with `logs`, `hasMore`, and
   `nextCursor`, preserving `logs` compatibility.
2. Use an HMAC-domain-separated payload containing `(created_at,id)` and a
   canonical filter digest; fetch `limit+1` with `<` tuple comparison.
3. Return `400 invalid_input` for malformed, tampered, or filter-mismatched
   cursors.
4. Measure the current index and add `(created_at DESC,id DESC)` only if needed.

### Tests And Verification

Test equal timestamps, page boundary deletion, no duplicates, no omissions,
tamper, filter mismatch, and service limits. Run real PostgreSQL store tests and
`go test ./...`.

### Compatibility And Security

Cursor contents are opaque and signed, not encrypted. Existing clients reading
`logs` continue to work.

### Risks And Rollback

Filter canonicalization drift can invalidate cursors; version payloads. Roll
back API/store changes and leave any harmless composite index.

### Manual Acceptance

Not required for backend contract.

### Completion Criteria

Repeated page requests traverse all matching rows exactly once.

### Commit

`feat(request-logs): add cursor pagination contract`

## Task 2: Move The UI To Server Pagination

Status: completed locally on 2026-07-21.

### Goal

Replace bounded local page slicing with URL-backed older-page loading.

### Dependencies

Task 1.

### Files

- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/request-logs/+page.svelte`
- Modify: `frontend/src/routes/navigation.test.mjs`
- Test: frontend tests and Playwright
- Migrate: none
- Document: `docs/manual.md` only if operator behavior changes materially

### Implementation

Reset pages on filter change; keep filter state in the URL; append by cursor;
recover from expired/invalid cursors with a fresh first page; preserve details
selection by ID when still loaded.

### Tests And Verification

Run `bun test`, `bun run check`, `bun run build`, and Playwright at desktop and
mobile widths against more than 200 seeded rows.

### Compatibility And Security

No records or columns are removed. Errors do not expose cursor internals.

### Risks And Rollback

Concurrent inserts should not reorder already loaded pages. Restore local
pagination while the additive backend contract remains available.

### Manual Acceptance

Required for filter, Load older, and detail interactions.

### Completion Criteria

An operator can reach rows older than the first 200 without losing filters.

### Commit

`feat(request-logs): load cursor pages in admin UI`

## Task 3: Batch Automatic Retention

Status: completed locally on 2026-07-21; production enablement remains an
owner-controlled operational action after isolated backup restore verification.

### Goal

Apply saved retention safely in the background and expose task status.

### Dependencies

Task 1 cursor/index baseline.

### Files

- Modify: admin repository/service and tests
- Modify: `backend/cmd/n2api/main.go`, `main_test.go`
- Modify: System Event actions/tests
- Modify: health/admin status endpoint and Gateway UI status
- Modify: `.env.example`, `docs/manual.md`
- Test: PostgreSQL batch integration and runner clock tests
- Migrate: none unless task status is later made durable

### Implementation

Delete ordered ID batches selected by cutoff, commit each batch, honor context,
take a PostgreSQL advisory lock, run once at startup then on a bounded interval,
and record one summary or failure event. Expose last start/success/error and
oldest/count estimates. Maximum rows and storage remain a later measured task.

### Tests And Verification

Seed multiple batches, interrupt between batches, run two workers, and verify
surviving/new rows and event counts.

### Compatibility And Security

Retention `0` remains disabled. Default behavior does not delete logs unless a
positive saved policy exists and the separate automatic runner environment
gate is explicitly enabled.

### Risks And Rollback

Wrong cutoffs are destructive. Require UTC tests and a verified backup; disable
the runner to roll back.

### Manual Acceptance

Review displayed cutoff/count before enabling on a long-running instance.

### Completion Criteria

Cleanup is bounded, observable, single-run, cancellable, and restart-safe.

### Commit

`feat(request-logs): automate batched retention`

## Task 4: Stream Bounded CSV And JSONL Exports

Status: completed locally on 2026-07-21.

### Goal

Export large ranges without accumulating all rows in memory.

### Dependencies

Task 1 page/query primitives.

### Files

- Add repository row-stream callback/iterator in admin/store
- Modify `handleExportRequestLogs` and tests
- Modify frontend export controls/tests
- Document limits in `.env.example` and `docs/manual.md`
- Test: cancellation, limit, CSV injection, large fixture, disconnect
- Migrate: none

### Implementation

Require an explicit time range for large exports, enforce row/time limits,
support CSV and JSONL with optional gzip, protect `=`, `+`, `-`, and `@` CSV
cells, name files by UTC range, and cancel database work on disconnect. Audit
the accepted export before body streaming and record a final outcome separately
without recursive failure.

The implementation uses one ordered `LIMIT max+1` scan and writes at most
`max` rows. The extra row marks the download and final event as explicitly
truncated; it does not run a second preflight scan. JSON remains a compatibility
download with a fixed 200-row cap. CSV and JSONL use the half-open range
`since <= created_at < before`, a bounded execution timeout, and an explicit
export DTO so future fields are not added to downloads accidentally.

### Tests And Verification

Measure memory over a large seeded set; disconnect mid-stream; validate gzip
and spreadsheet-safe CSV.

### Compatibility And Security

Small JSON array export may remain with an explicit low cap. No secret fields
are added.

### Risks And Rollback

Errors after headers cannot return JSON status. Stop streaming and record a
bounded event; restore the small export handler to roll back.

### Completion Criteria

Export memory stays bounded and limits/cancellation are enforced.

### Commit

`feat(request-logs): stream bounded exports`

## Task 5: Measure And Rationalize Indexes

Status: completed locally on 2026-07-21.

### Goal

Keep only indexes justified by representative queries.

### Dependencies

Tasks 1-4 and a synthetic long-running data profile.

### Files

- Create: a migration for approved additions/removals
- Create: `backend/internal/store/request_log_query_test.go` or benchmark fixture
- Modify: migration tests and operations docs

### Implementation

The opt-in `TestRequestLogQueryProfile` creates a random isolated schema, runs
all migrations, generates one million skewed Request Logs spanning almost 90
days, runs `ANALYZE`, and captures warm `EXPLAIN (ANALYZE, BUFFERS, FORMAT
JSON)` evidence. The matrix covers first/deep cursor pages, hot and cold
account/model/pool exports, client-key pages, hot and cold budget aggregation,
request/session exact filters, usage summary, retention candidates, index
bytes, and a 100,000-row write probe. Page and export cases reuse the production
Request Log select and joins. The schema is dropped after the test.

The approved migration:

- removes the physically duplicate provider-account and model usage indexes;
- removes the provider/time index because no production predicate filters by
  provider and the usage summary did not select it;
- adds `(client_key_id, created_at DESC, id DESC)` for client-key filtering and
  rolling budget reads; and
- bounds the budget aggregate itself to the oldest relevant 30-day cutoff.

The existing `(created_at DESC)` index remains. Expanding it to `(created_at
DESC,id DESC)` reduced sub-millisecond cursor work but added about 22 MB and
caused a representative hot account export to select a bitmap scan with an
external merge spill. Extending the account index did not prevent that bitmap
plan. Request ID, session, trigram, status, error, usage-source, and fallback
indexes remain deferred because their measured benefit did not justify the
additional write and storage matrix.

### Measurement Evidence

The accepted legacy/candidate run on PostgreSQL 18.4 produced:

| Measurement | Legacy | Candidate |
| --- | ---: | ---: |
| Total Request Log index bytes | 159,391,744 | 140,419,072 |
| 100,000-row indexed write probe | 1,342.424 ms | 968.899 ms |
| Client-key page | 3.738 ms | 0.575 ms |
| Cold-key bounded budget aggregate | 62.562 ms | 3.867 ms |
| Default cursor page | 0.410 ms | 0.397 ms |
| Deep cursor page | 0.395 ms | 0.410 ms |
| Hot account export | 270.428 ms | 258.640 ms |
| Retention candidate batch | 5.633 ms | 5.689 ms |

The candidate removed all duplicate index definitions, reduced total index
storage by about 11.9%, and reduced the synthetic write probe by about 27.8%.
The accepted export and retention plans had no temporary reads or writes. The
hot key remains a deliberate worst case: PostgreSQL can prefer the bounded time
index or a sequential scan when one key owns half the table, while the new
client-key index materially improves selective keys and list filters.

These figures are comparative evidence from one synthetic run, not production
latency guarantees. Rerun the profile after materially changing data shape,
PostgreSQL, request filters, or retention behavior.

### Operational Risk And Rollback

The forward migration creates one index and drops three. Normal N2API startup
runs migrations before readiness, so Request Log writes are not served during
the build. On a very large external database, schedule the upgrade with enough
temporary disk and a maintenance window because a normal index build blocks
table writes. The down migration restores the exact legacy index set.

### Completion Criteria

Every changed index has before/after plan evidence and stated write/storage
cost. The accepted candidate has no duplicate definitions or representative
export spill, and preserves bounded cursor and retention behavior.

### Commit

`perf(request-logs): align indexes with measured queries`
