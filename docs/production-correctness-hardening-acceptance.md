# Production Correctness Hardening Acceptance

- Date: 2026-07-29
- Implementation head tested: `7eab2c9`
- Baseline: `4037187c36198bf50b9c328a557a29a7ca413e56`

## Result

The production correctness hardening scope is accepted locally. Every item in
the source brief's Sections 18.1-18.7 has source, test, or runtime evidence
below. This is not production deployment evidence: no commit was pushed, no
GitHub-hosted workflow or release was run, and the operator checks listed under
Unrun Checks remain open.

## 18.1 Database Connections

- The instance lock and migration lock use dedicated `pgx.Conn` ownership;
  migration execution uses a separate one-connection pool. They do not consume
  the business pool. See `backend/internal/store/instance_lock.go`,
  `backend/internal/store/migrations.go`, and `backend/internal/store/postgres.go`.
- System Event `LISTEN` owns a dedicated connection and reconnects after
  connection loss. See `backend/internal/store/system_event_subscription.go`.
- `make test-control-connections` passed the pool-size 1 and 2 cases, Alert
  enabled and disabled cases, business queries while controls are held, lock
  termination, LISTEN reconnect, and real process lifecycle tests.
- The unsafe cold-start process test proves migration and bootstrap are
  serialized. The safe process test proves that the losing instance does not
  bind its listener. See `backend/cmd/n2api/instance_lock_process_test.go`.
- Lock loss enters the supervised nonzero shutdown path. Migration lock close
  also rejects a previously observed connection loss even if unlock succeeds.

## 18.2 Shutdown

- Signal, request, background, and global shutdown contexts are separate.
  Readiness changes to false before HTTP drain starts. See
  `backend/cmd/n2api/runtime_shutdown.go` and
  `backend/internal/lifecycle/readiness.go`.
- Real listener tests cover normal HTTP requests, SSE, slow upload, waiting
  upstream responses, deadline cancellation, metrics availability during
  drain, and ordered Alert queue shutdown.
- Background components run through a supervisor and are waited before the
  Alert dispatcher and shared resources close. Close paths are idempotent and
  race-tested.
- The application shutdown maximum is 30 seconds. Development, E2E, and
  release Compose use a 35-second stop grace period.
- The Task 3 container SIGTERM check exited cleanly in 144 ms with exit code 0
  and `OOMKilled=false`; the full lifecycle race suite passed again during
  final acceptance.

## 18.3 Settings

- Gateway Settings are loaded and semantically validated before Ready, then
  published as immutable atomic snapshots. See
  `backend/internal/admin/gateway_settings_runtime.go`.
- Gateway requests and provider auto-tests read the runtime snapshot rather
  than querying the settings row. A real Proxy test issued five requests with
  no increase in settings loader calls.
- Refresh failure preserves the last-known-good snapshot, marks it stale, and
  exposes bounded health and metrics state; it does not silently restore
  permissive defaults.
- Initial database or validation failure leaves the service not Ready and
  blocks the gateway.
- A committed admin update publishes immediately. Version conflicts or an
  uncertain publication trigger reload behavior so the database remains the
  authority.

## 18.4 API Key

- Authentication uses one active-state statement that returns the key and
  selected models while touching `last_used_at` at most once per UTC minute.
  Tests proved one touch across 1,000 sequential and 1,000 concurrent
  authentications.
- Disable, re-enable, and revoke commits are observed by the next
  authentication. Database read failure remains fail-closed.
- Secret reveal is a password-bearing POST. The old GET cannot reveal a
  secret. Responses are `no-store`, audit records are sanitized, and revealed
  secrets exist only in dialog-local frontend state.
- Reveal admission is bounded independently by IP, administrator, and key,
  with stable `Retry-After` behavior and a 4,096-entry fail-closed cap. Password
  verification uses the shared process concurrency bound.

## 18.5 Budget

- Migration 49 adds authoritative budget state and admission tables with
  idempotency constraints, bounded work indexes, downgrade guards, and a
  durable `admitted -> settlement_pending -> settled` outbox.
- Real PostgreSQL concurrency tests prove that exactly 10 of 100 requests are
  admitted for a request budget of 10, including separate unsafe-mode
  processes. Request admission is independent of Request Log persistence.
- Settlement uses the first persisted payload by admission ID. Retry,
  fallback, worker concurrency, and restart recovery do not double charge.
- Hot-path admission reads bounded state and does not scan 30 days of Request
  Logs. Legacy initialization is bounded and fails closed while pending.
- Pre-ledger history is backfilled once without treating post-ledger Request
  Logs as a second authority, so existing budgets do not reset to zero after
  upgrade.
- API, UI, metrics, and documentation consistently describe request limits as
  strict and token/cost limits as observed-usage limits.

## 18.6 Query Performance

- API Key pages use exactly three queries for both 1 and 100 rows: page,
  selected models, and budget state. Routing Pool pages use exactly two:
  page and memberships.
- Both lists use signed, resource- and filter-bound keyset cursors with default
  size 50, maximum size 100, stable `(created_at DESC, id DESC)` ordering, and
  explicit invalid-cursor handling.
- `make test-management-list-profile` passed with 10,000 API Keys and 1,000
  Routing Pools. First and deep pages used the management indexes without a
  Sort node, and association batches used their expected indexes.
- `make test-request-log-profile` passed against 1,000,000 rows. The fixed-row
  profile also validated the 10,000,000-row equivalent projection.
- Migration 50 provides `client_api_keys_management_page_idx` and
  `routing_pools_management_page_idx`; both are present in the refreshed
  development database.

## 18.7 Security

- Parsed pgx primary and fallback attempts are classified independently as
  plaintext, unverified TLS, or verified-full. Remote database deployment can
  require hostname and CA identity verification.
- Plaintext and unverified TLS require separate accepted-risk tokens. Tests
  cover URL and keyword DSNs, every supported SSL mode, fallback hosts, system
  roots, missing roots, and sanitized errors.
- New and changed administrator passwords use bounded Argon2id PHC hashes.
  Legacy PBKDF2 remains verifiable and is upgraded by compare-and-swap after a
  successful login.
- Password bytes, including leading or trailing whitespace and Unicode, are
  preserved. Unknown-user verification uses a precomputed current-algorithm
  dummy hash.
- Secret-file loading accepts bounded regular files only, rejects links and
  special files, removes exactly one trailing newline, rejects direct/file
  conflicts, and does not include values or paths in errors.
- Tests and diagnostic artifact checks use canaries to ensure secrets do not
  enter logs, errors, metrics, health payloads, or retained failure artifacts.

## Verification

The following local commands passed on 2026-07-29:

- `make test`: all Go packages, Svelte diagnostics with 0 errors and 0
  warnings, 208 Bun tests, and the production static build.
- `make test-e2e`: PostgreSQL-backed gateway, timeout, SSE, cancellation,
  boundary, and affinity scenarios.
- `make test-contracts`: OpenAI JavaScript and Python SDK contracts.
- `make test-request-log-profile`: 1,000,000-row profile; the main profile test
  completed in 97.78 seconds.
- `make test-management-list-profile`: 10,000 keys, 1,000 pools, fixed query
  counts, first/deep keyset pages, and index-plan assertions.
- `make test-restore-backup`: schema 50 current restore, real migration Down to
  schema 47 and Up to 50, wrong-key, corrupt-archive, termination cleanup, and
  test-resource cleanup.
- `make test-dev-artifacts`: managed artifact lifecycle and PostgreSQL backup
  script tests.
- `make test-go-quality`: pinned Staticcheck 2026.1 (`v0.7.0`) and `go vet`.
- `make test-critical-race`: Admin, Alerting, HTTP API, Store, and main-process
  race suites with isolated PostgreSQL.
- `make test-control-connections`: Store, Store race, and real process fault
  injection suites.
- `make test-production-deploy`: plaintext, core-secret, metrics plaintext,
  metrics-secret, default/custom resource, invalid-value, and release-image
  verification cases.
- `cd backend && go test -count=1 ./...` and `go vet ./...`, using the managed
  repository caches.
- `cd frontend && bun install --frozen-lockfile`, `bun run check`, `bun test`,
  and `bun run build`; the frozen install reported no changes.
- Development, release with a safe test env, and metrics Compose
  `config --quiet` commands.
- Actionlint `v1.7.12` and `dev/ci/verify-pinned-dependencies.sh`.

One initial full critical-race run observed a non-reproduced immediate
advisory-lock reacquire contention in the OAuth cleanup test. The same focused
race test passed in a new isolated database, and an unchanged full critical
race rerun passed all five packages. No code was changed to hide the signal.

## Refreshed Development Runtime

- Pre-build `docker builder prune --all --force` reclaimed 3.051 GB.
- `docker compose -f deploy/compose.yaml build --no-cache` succeeded.
- `docker compose -f deploy/compose.yaml up -d --force-recreate` recreated the
  stack while preserving the development PostgreSQL volume.
- `n2api`, `postgres`, and `postgres-backup` are healthy. N2API is bound on
  `0.0.0.0:3000` and `[::]:3000`.
- Container-local `/healthz` returned `{"status":"ok"}`. `/readyz` reported
  database, Gateway Settings, runtime, and static assets ready. Bootstrap
  reported administrator `admin` and the configured public URL.
- The database reports schema 50 and both migration 50 management indexes.
- `http://oc-de-fra-1.knowsky.uk:3000/healthz` returned `{"status":"ok"}`.
- Post-verification builder cleanup reclaimed 1.069 GB.

## Unrun Checks

These checks require owner-authorized remote or operator activity and were not
performed as part of this local acceptance:

- Push and exact-SHA GitHub-hosted `CI Image` execution.
- Real OpenAI OAuth and downstream Codex CLI traffic.
- A real external reverse proxy or TLS edge deployment.
- Release publication or immutable published-image deployment.
- Restore of a current real production backup. The passing restore command
  above uses generated isolated fixtures and is not operator-backup evidence.
- Any remote production control-plane mutation.
