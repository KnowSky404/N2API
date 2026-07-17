# System Event and Audit Log Implementation Plan

**Goal:** Implement the approved system event and audit log design with complete
admin/OAuth/scheduler coverage, secret-safe persistence, and an operational admin
page.

**Architecture:** Add a typed `systemevent` domain package and PostgreSQL repository.
The HTTP layer supplies request and actor context; service/store mutation boundaries
emit semantic events. Successful mutations and audit inserts share a transaction.
System/OAuth failures use fixed error codes and action-specific message templates,
plus secret-safe structured `slog` alarms. The frontend queries a cursor-paginated
admin endpoint.

**Tech stack:** Go 1.26, pgx/PostgreSQL, SvelteKit/Svelte 5, Tailwind CSS, Bun,
Docker Compose, Bunx Playwright.

---

## Task 1: Add the event schema and domain contract

Files:

- Add `backend/internal/store/migrations/00036_system_events.sql`
- Add `backend/internal/systemevent/event.go`
- Add `backend/internal/systemevent/event_test.go`
- Modify `backend/internal/store/migrations_test.go`

Steps:

- [ ] Add the constrained schema and access-pattern indexes from the design.
- [ ] Define typed event constants, actor/request/target context, and safe metadata
  constructors.
- [ ] Enforce string, message, and JSON-size limits.
- [ ] Reject metadata keys matching the secret denylist.
- [ ] Test every constraint marker and redaction boundary.
- [ ] Run `cd backend && go test ./internal/systemevent ./internal/store`.

## Task 2: Add persistence, filters, and keyset pagination

Files:

- Add `backend/internal/store/system_event.go`
- Add `backend/internal/store/system_event_test.go`
- Modify `backend/internal/admin/service.go`
- Modify `backend/internal/admin/service_test.go`

Steps:

- [ ] Implement insert-on-pool and insert-on-`pgx.Tx` APIs plus context-carried
  `EventIntent` helpers.
- [ ] Implement typed filter validation and `limit + 1` pagination.
- [ ] Encode and validate an opaque URL-safe `(occurred_at, id)` cursor and query
  deterministically without looking up a retained row.
- [ ] Add bounded search over safe text fields only.
- [ ] Add batched retention deletion and boundary tests.
- [ ] Run `cd backend && go test ./internal/store ./internal/admin`.

## Task 3: Add request, actor, and security event context

Files:

- Add `backend/internal/httpapi/system_event.go`
- Add `backend/internal/httpapi/system_event_test.go`
- Modify `backend/internal/httpapi/server.go`
- Modify `backend/internal/httpapi/server_test.go`

Steps:

- [ ] Generate or validate a correlation ID and return `X-Request-ID`.
- [ ] Capture `Request.Pattern`, method, direct remote IP, status, and duration without
  buffering request or response bodies.
- [ ] Inject the authenticated admin snapshot into context.
- [ ] Record login/session/password rejection and failure events that do not commit
  a business change; leave success events for the admin service transaction.
- [ ] Record API key secret view and request-log export before returning protected
  data; fail closed if their security event cannot be stored.
- [ ] Do not trust forwarded IP headers until trusted-proxy support exists.
- [ ] Add a coverage test that fails when a mutating admin route lacks an action.
- [ ] Run `cd backend && go test ./internal/httpapi`.

## Task 4: Make admin mutations transactionally audited

Files:

- Modify `backend/internal/admin/service.go`
- Modify `backend/internal/admin/fingerprint.go`
- Modify `backend/internal/admin/error_passthrough.go`
- Modify `backend/internal/admin/official_pricing.go`
- Modify `backend/internal/store/admin.go`
- Modify `backend/internal/store/fingerprint.go`
- Modify `backend/internal/store/error_passthrough.go`
- Modify related tests

Steps:

- [ ] Add action constants for API keys, routing pools, settings, pricing,
  fingerprints, passthrough rules, request-log cleanup, and model settings.
- [ ] Make each concrete PostgreSQL mutation read the context intent, enrich it from
  `RETURNING` or a locked row, and insert it in the mutation's own transaction.
- [ ] Add a coverage test for audited store mutations; never call an independent
  recorder after a successful repository method.
- [ ] Make login session creation, logout session revocation, password changes, and
  bootstrap create/username update commit their success events transactionally.
- [ ] Pass allowlisted `changed_fields`; represent credential changes as booleans.
- [ ] Commit successful mutations and event inserts in the same transaction.
- [ ] Emit fixed-code, fixed-template failure events only after the business
  transaction rolls back; never persist `err.Error()` or upstream text.
- [ ] Remove expired API key purge from the `ListAPIKeys` read path.
- [ ] Preserve startup and hourly purge behavior with deletion and its scheduler
  event in the same transaction.
- [ ] Run `cd backend && go test ./internal/admin ./internal/store ./internal/httpapi`.

## Task 5: Add provider, OAuth, batch, and runtime events

Files:

- Modify `backend/internal/provider/service.go`
- Modify `backend/internal/provider/service_test.go`
- Modify `backend/internal/provider/auto_test_runner.go`
- Modify `backend/internal/provider/auto_test_runner_test.go`
- Modify `backend/internal/store/provider.go`
- Modify `backend/internal/store/provider_test.go`
- Modify `backend/internal/httpapi/server.go`
- Modify `backend/internal/httpapi/server_test.go`

Steps:

- [ ] Add explicit refresh triggers to manual and automatic refresh callers.
- [ ] Record OAuth connect/callback/refresh without authorization URLs, state,
  verifier, code, tokens, or raw upstream errors.
- [ ] Record provider lifecycle mutations once at the service boundary so legacy
  route aliases do not duplicate events.
- [ ] Rework batch helpers to produce accurate success, failure, and partial counts.
- [ ] Commit a per-target event with every target mutation, attach a shared
  `batch_id`, and write a separate best-effort summary after processing.
- [ ] Preserve stop-on-first-error behavior and record requested, attempted,
  succeeded, failed, and skipped counts in the summary.
- [ ] Cover `provider_account.disconnect_all` for the legacy provider-wide route.
- [ ] Emit one auto-test cycle summary while keeping current per-account test history.
- [ ] Record runtime account events only when state actually changes.
- [ ] Run `cd backend && go test ./internal/provider ./internal/store ./internal/httpapi`.

## Task 6: Add retention configuration and runtime wiring

Files:

- Modify `backend/internal/config/config.go`
- Modify `backend/internal/config/config_test.go`
- Modify `backend/cmd/n2api/main.go`
- Modify `.env.example`

Steps:

- [ ] Parse `N2API_SYSTEM_EVENT_RETENTION_DAYS` with default `365`, disabled `0`,
  and enabled range `30..3650`.
- [ ] Wire one event repository into admin, provider, HTTP, and scheduler services.
- [ ] Run cleanup at startup and every 24 hours in bounded batches.
- [ ] Generate cryptographically random correlation IDs for bootstrap, runtime,
  scheduler, and retention events; reuse a root ID across a cycle or batch.
- [ ] Treat system-event retention summaries as best-effort self-maintenance
  telemetry, not atomic business audit records.
- [ ] Log persistence failures through a tested structured `slog` redactor without
  raw upstream errors or secret fields.
- [ ] Run `cd backend && go test ./...`.

## Task 7: Add the authenticated query endpoint

Files:

- Modify `backend/internal/httpapi/server.go`
- Modify `backend/internal/httpapi/server_test.go`

Steps:

- [ ] Add `GET /api/admin/system-events` with validated filters.
- [ ] Return `events`, `nextCursor`, and `hasMore`.
- [ ] Confirm there is no browser-facing event creation, edit, or delete endpoint.
- [ ] Add authentication, invalid-filter, pagination, empty, and storage-error tests.
- [ ] Run `cd backend && go test ./internal/httpapi ./internal/admin ./internal/store`.

## Task 8: Add frontend state and source tests

Files:

- Modify `frontend/src/lib/admin-state.svelte.js`
- Modify `frontend/src/routes/navigation.test.mjs`

Steps:

- [ ] Define the event type, filter state, opaque cursor pagination state, reset,
  refresh, and load-older helpers.
- [ ] Preserve current rows when a refresh fails and show a stale-data error.
- [ ] Add source tests for navigation, filter labels, secret-safe details, and cursor
  pagination.
- [ ] Run `cd frontend && bun test src/routes/navigation.test.mjs`.

## Task 9: Build the System logs page

Files:

- Add `frontend/src/routes/system-logs/+page.svelte`
- Modify `frontend/src/routes/+layout.svelte`
- Modify `frontend/src/routes/navigation.test.mjs`

Steps:

- [ ] Add the `System logs` navigation item with a Lucide icon.
- [ ] Build the compact filter bar, table, mobile rows, empty/loading/error states,
  load-older control, and safe detail modal.
- [ ] Use URL-backed GET-compatible filters and client-side SvelteKit navigation.
- [ ] Keep metadata in a definition list; never render arbitrary HTML.
- [ ] Group per-target events beneath a matching batch summary by default while
  keeping every event inspectable.
- [ ] Run `cd frontend && bun test`.
- [ ] Run `cd frontend && bun run check`.
- [ ] Run `cd frontend && bun run build`.

## Task 10: Full verification and delivery

Steps:

- [ ] Run `cd backend && go test ./...`.
- [ ] Run `cd frontend && bun test`.
- [ ] Run `cd frontend && bun run check`.
- [ ] Run `cd frontend && bun run build`.
- [ ] Review `git diff --check`, the full diff, and `git status --short`.
- [ ] Create atomic Conventional Commits per completed implementation slice.
- [ ] Push and wait for the matching `CI Image` run; confirm `Test`, `Build and smoke
  test image`, and the `main` image push step.
- [ ] Rebuild and recreate the local Compose stack with the
  `n2api-refresh-docker` workflow.
- [ ] Verify Compose status, `/healthz`, the migrated `system_events` table, and a
  real admin mutation event from inside the container network.
- [ ] Because the Browser plugin is absent, run the required Bunx Playwright path:
  `cd frontend && bunx playwright --version`, then an authenticated temporary test
  outside the repository at desktop and mobile viewports.
- [ ] Verify page identity, nonblank content, no framework overlay, console health,
  filter interaction, detail modal, pagination, and screenshots.
