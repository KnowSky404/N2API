# N2API Production Correctness Hardening Plan

Status: in progress
Design: `docs/specs/2026-07-29-n2api-production-correctness-hardening.md`
Baseline: `4037187c36198bf50b9c328a557a29a7ca413e56`

Statuses use `pending`, `in progress`, `completed`, or `blocked`. A task is
completed only when its implementation, focused tests, documentation, diff
review, and atomic commit are complete.

## Evidence Ledger

| Area | Status | Current evidence |
| --- | --- | --- |
| Design and implementation plan | completed | Required documents created, reviewed, and committed as the first atomic change |
| PostgreSQL control connections | completed | Dedicated connections, serialized startup, bounded-pool process tests, LISTEN reconnect, vet, and race verification committed in Task 2 |
| Lifecycle and graceful drain | pending | Current signal context cancels requests before `Shutdown` |
| Gateway Settings runtime | pending | Current request path calls `GetGatewaySettings` |
| API key authentication touch | pending | Current successful authentication writes every request |
| Durable budget ledger | pending | Current budget reads aggregate Request Logs |
| Bounded admin lists | pending | Current key list has N+1 budget reads and both lists are unbounded |
| Database TLS identity | pending | Current check only detects possible plaintext |
| Secret reveal step-up | pending | Current GET returns the full secret for an ordinary session |
| Password hash migration | pending | Current PBKDF2 verifier is fixed-parameter and passwords are trimmed |
| Secondary deployment and CI work | pending | Existing hardening is partial; required gates and resource bounds are absent |
| Final acceptance | pending | Task 2 passed `make test`, `make test-control-connections`, focused `go vet`, Store race tests, `bash -n`, and `git diff --check` |

## Task 1: Commit Design And Plan

Status: completed
Dependencies: none

Implementation:

- Review current HEAD, migrations, tests, docs, Compose, and CI against every
  mandatory workstream.
- Record connection ownership, state machines, settings behavior, budget
  semantics, pagination, migration/rollback, failure policy, and compatibility.
- Keep this evidence ledger current after every atomic implementation commit.

Tests and acceptance:

- Markdown paths exactly match the requested names.
- Design covers all ten required decisions and does not claim unrun evidence.
- `git diff --check` passes and the docs are committed independently.

Commit: `docs: design production correctness hardening`

## Task 2: Isolate PostgreSQL Control Connections

Status: completed
Dependencies: Task 1

Implementation:

- Add parsed pgx connection factories with copied `application_name` values.
- Move the instance lock to a dedicated `pgx.Conn` acquired before the app
  pool, migrations, bootstrap, or listeners.
- Add the cross-mode migration advisory lock and dedicated migration executor.
- Move System Event LISTEN/reconnect to dedicated connections.
- Remove the alert-related business-pool minimum and sanitize control errors.

Tests and acceptance:

- Unit tests cover copied settings/application names and idempotent close.
- PostgreSQL tests cover pool sizes 1 and 2, alert on/off, safe concurrent cold
  start, unsafe serialized migration, lock termination, LISTEN reconnect, and
  ordinary business queries during held controls.
- Main listener never binds in the losing safe process.

Commit: `fix(store): isolate postgres control connections`

## Task 3: Add Supervised Graceful Draining

Status: pending
Dependencies: Task 2

Implementation:

- Add readiness state and a lifecycle supervisor with separate signal,
  request, and background contexts.
- Register every background runner and listener as a supervised component.
- Implement one global deadline and request-drain allocation.
- Keep metrics alive through main-server drain and close resources once.
- Add validated shutdown configuration and set Compose grace to 35 seconds.

Tests and acceptance:

- Real listeners prove long HTTP and SSE requests complete inside the drain
  window and are canceled only at the deadline.
- Slow upload and waiting-upstream cases exit without leaked goroutines.
- Metrics exposes draining during main drain; queued alerts stop predictably.
- Lock loss uses the same path and exits nonzero.
- Container exits within Compose grace.

Commit: `fix(runtime): implement graceful request draining`

## Task 4: Add Last-Known-Good Gateway Settings

Status: pending
Dependencies: Task 3

Implementation:

- Introduce a narrow settings source and atomic immutable runtime snapshot.
- Load and validate before Ready; refresh periodically and after admin writes.
- Use the runtime for all gateway hot-path settings and auto-test configuration.
- Expose bounded health and metrics state.

Tests and acceptance:

- Save is immediately visible; failed save retains the old snapshot.
- Database and corrupt-JSON refresh failures retain limits and mark stale.
- First-load failure prevents Ready.
- Concurrent reads pass race tests, and query instrumentation proves zero
  settings SQL during gateway requests.

Commit: `feat(runtime): add last-known-good gateway settings`

## Task 5: Bound API Key Last-Used Writes

Status: pending
Dependencies: Task 2

Implementation:

- Replace read-then-touch with one active-state authentication statement and
  conditional one-minute UTC touch.
- Inject time and expose bounded touch failures without failing open.

Tests and acceptance:

- 1,000 sequential and concurrent authentications produce at most one write
  per minute per key.
- Disable/revoke commits are observed by subsequent authentications.
- Read failure fails closed; touch behavior is deterministic and race-clean.

Commit: `perf(auth): bound api key last-used writes`

## Task 6: Add Durable Budget Admission And Settlement

Status: pending
Dependencies: Tasks 2, 4, and 5

Implementation:

- Add sequential migrations for state, admissions, expiry work, constraints,
  and indexes.
- Implement restartable legacy initialization and fail-closed pending state.
- Atomically admit strict request budget with a server idempotency ID.
- Settle observed token/cost independently from Request Log persistence.
- Add bounded expiry, abandoned-admission, and alert projection runners.
- Update API and UI terminology/state.

Tests and acceptance:

- Exactly 10 of 100 concurrent requests acquire a request budget of 10,
  including separate unsafe-mode processes.
- Retry/fallback and repeated settlement do not double charge.
- Request Log failure and restart do not lose budget usage.
- Crash-between-admit-and-settle, cleanup idempotency, revoke, modification,
  24h/30d expiry, legacy initialization, and database failure are covered.
- Admission work/query count is independent of Request Log history size.

Commit: `feat(budget): add atomic budget admission and settlement`

## Task 7: Batch And Paginate Management Lists

Status: pending
Dependencies: Task 6

Implementation:

- Add signed filter-bound keyset cursors and bounded `limit` to API keys and
  routing pools.
- Batch allowed models, budget state, and pool membership.
- Update frontend load-more, partial/error/empty states, and explicit selection
  boundaries.
- Instrument other named admin surfaces and change only proven N+1 queries.

Tests and acceptance:

- Tampered and filter-mismatched cursors fail with `invalid_cursor`.
- API key and routing-pool query counts remain constant as page size grows.
- Frontend does not assume one response contains every row.
- Profiles cover 10,000 keys, 1,000 pools, and scalable 10,000,000-log
  equivalents with stable `EXPLAIN (ANALYZE, BUFFERS)` assertions.

Commit: `perf(admin): batch and paginate management queries`

## Task 8: Enforce Database TLS Identity Policy

Status: pending
Dependencies: Task 2

Implementation:

- Classify every parsed pgx primary and fallback TLS configuration.
- Add `database-unverified-tls` independently of `database-plaintext`.
- Update examples and operations guidance for `verify-full`.

Tests and acceptance:

- Cover all sslmodes, URL and keyword DSNs, fallback hosts, missing roots,
  hostname verification, and both accepted-risk paths.
- Errors contain no DSN, password, or certificate content.

Commit: `fix(security): require verified database tls or explicit risk`

## Task 9: Require Password Step-Up For Secret Reveal

Status: pending
Dependencies: Task 5 and Task 10 hasher interface

Implementation:

- Replace the reveal GET with password-bearing POST and no-store response.
- Revalidate the session, reserve bounded IP/admin/key throttle admission, then
  verify the password through the shared verification concurrency limit.
- Audit sanitized success and failure outcomes.
- Add a dialog-local frontend password/reveal flow that clears all secrets.

Tests and acceptance:

- Old GET cannot reveal a secret; POST requires a current session and password.
- Rate limits, uniform failures, cache policy, browser storage absence, dialog
  cleanup for created and revealed secrets, CLI invocation, and security events
  are covered.
- Browser verification exercises reveal, copy, close, and retry behavior.

Commit: `fix(security): require step-up for api key reveal`

## Task 10: Migrate Administrator Password Hashes

Status: pending
Dependencies: Task 2

Implementation:

- Add bounded Argon2id PHC hashing and legacy PBKDF2 verification.
- Preserve exact password bytes and keep username normalization.
- Compare-and-swap a legacy hash after successful login.
- Use a precomputed current-algorithm dummy hash for unknown users.
- Bound all login, password-change, reveal, and dummy verification work with one
  process semaphore.

Tests and acceptance:

- Cover legacy login/upgrade, new login, wrong password, leading/trailing
  spaces, Unicode, concurrent migration/password change, malformed and
  excessive PHC parameters, dummy verification, and race behavior.
- Record a small-VPS benchmark without making wall-clock timing a flaky gate.

Commit: `feat(auth): migrate administrator password hashing`

## Task 11: Complete Secondary Deployment And CI Gates

Status: pending
Dependencies: Tasks 2-10 complete

Implementation:

- Add parameterized release resource limits, log rotation, nofile/PID/tmpfs,
  and separate application/PostgreSQL defaults.
- Add bounded `_FILE` secret loading with conflict checks and Compose examples.
- Add digest validation and running-image comparison tooling.
- Expose retention choice/risk and best-effort alert delivery state.
- Add pinned staticcheck, vet, critical race, migration, lifecycle, budget,
  query-count, and Compose configuration gates.
- Update README, manual, release checklist, restore guidance, metrics, security,
  E2E, and repository acceptance documents.

Tests and acceptance:

- All Compose combinations validate with safe test environments.
- Secret-file regular-file, size, conflict, and newline semantics are tested.
- CI scripts run locally where possible and pin external tools/actions.
- No real release or remote control-plane mutation occurs.

Commits:

- `chore(deploy): bound production resources and secrets`
- `chore(ci): add correctness and race gates`
- `docs(operations): document production correctness changes`

## Task 12: Full Acceptance And Runtime Refresh

Status: pending
Dependencies: Tasks 2-11

Implementation:

- Create `docs/production-correctness-hardening-acceptance.md` with exact local
  evidence and explicit unrun remote/operator checks.
- Review every requirement against source, tests, runtime, migrations, and UI.
- Run the complete required suite and repair every regression.
- Prune builder cache, rebuild/recreate Compose without cache, verify container
  health and routes, then prune builder cache again.

Required commands:

```bash
make test
make test-e2e
make test-contracts
make test-request-log-profile
make test-restore-backup
make test-dev-artifacts
cd backend && go test -count=1 ./...
cd backend && go vet ./...
cd frontend && bun install --frozen-lockfile
cd frontend && bun run check
cd frontend && bun test
cd frontend && bun run build
docker compose -f deploy/compose.yaml config --quiet
docker compose -f deploy/compose.release.yaml --env-file <safe-test-env> config --quiet
docker compose -f deploy/compose.yaml -f deploy/compose.metrics.yaml config --quiet
```

Also run the pinned staticcheck command, all specified package race tests,
migration Up/Down/upgrade/restore checks, real PostgreSQL fault injection,
network-level drain cases, scalable query profiles, and every new Compose
override combination.

Acceptance:

- Every item in Sections 18.1-18.7 of the source brief has direct evidence.
- The worktree contains only intentional committed changes.
- Local Compose is healthy and serves the refreshed build.
- GitHub-hosted CI, real OAuth, real reverse proxy, real release, and real
  production backup restore are reported as unrun unless actually performed
  with owner authorization.
