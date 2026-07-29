# N2API Production Correctness Hardening Design

Status: accepted for implementation
Date: 2026-07-29
Baseline: `4037187c36198bf50b9c328a557a29a7ca413e56`
Scope: personal and small self-hosted deployments, one safe application instance by default

## 1. Goals And Boundaries

This design fixes production correctness, failure safety, and bounded hot-path
behavior without expanding the product into a billing platform or distributed
control plane. PostgreSQL remains the only required coordination and durable
state service. Redis, message brokers, microservices, public registration, and
payment behavior remain out of scope.

The existing OpenAI-compatible routes, Codex behavior, encrypted provider
credentials, API keys, routing pools, request logs, system events, static admin
application, Compose topology, and published image path remain compatible.
Security-sensitive admin APIs may change when the old behavior is unsafe.

No secret, credential-bearing URL, request or response body, database DSN, or
raw network error may enter logs, metrics, System Events, HTTP errors, or test
artifacts. External failures use stable, enumerated error codes.

## 2. Confirmed Baseline

The current source still has the reviewed failure modes:

- `InstanceLock` reserves a `pgxpool.Conn`, and System Event `LISTEN` reserves a
  second business-pool connection.
- migrations and administrator bootstrap run before the instance lock.
- the signal context is the HTTP server base context and is canceled before
  `Shutdown`, so SIGTERM cancels active requests instead of draining them.
- gateway settings and API key budget usage are loaded from PostgreSQL on the
  request path; budget usage scans rolling Request Log windows.
- API key authentication updates `last_used_at` on every successful request.
- API key and routing-pool admin lists are unbounded and perform N+1 queries.
- remote TLS validation distinguishes encryption from plaintext, but not
  verified identity from unverified TLS.
- full API key reveal is an authenticated GET without password step-up.
- password input is trimmed and the PBKDF2 verifier rejects otherwise valid
  hashes after a future parameter change.

## 3. PostgreSQL Connection Ownership

All connection configurations are parsed by pgx and copied before mutation.
`RuntimeParams["application_name"]` identifies each owner. Parsed TLS,
fallback-host, authentication, timeout, and runtime parameters are preserved.

| Owner | Type | Application name | Lifetime |
| --- | --- | --- | --- |
| Safe instance lock | dedicated `pgx.Conn` | `n2api-instance-lock` | before migrations through final shutdown |
| Migration lock | dedicated `pgx.Conn` | `n2api-migration-lock` | startup database phase only |
| Migration executor | one-connection pool | `n2api-migration` | migration phase only |
| Application work | `pgxpool.Pool` | `n2api-app-pool` | after instance lock through final shutdown |
| System Event listener | reconnectable dedicated `pgx.Conn` | `n2api-system-event-listener` | alert dispatcher lifetime |

The migration advisory lock is acquired in both safe and unsafe modes. This
prevents an unsafe process from racing a safe process. It covers migrations,
administrator bootstrap, and any startup database mutation, and is released
before listeners start. A migration executor is separate because the session
holding the advisory lock cannot also be monopolized by Goose.

The server connection requirement is:

```text
steady state = application MaxConns + instance lock + optional LISTEN
startup peak = steady state + migration lock + one migration executor + operator reserve
```

`pool_max_conns=1` remains valid when alert delivery is disabled. Enabling
alerts does not increase the business-pool minimum because LISTEN is external
to that pool. The DSN remains the only source of pool sizing.

An unexpected instance-lock connection loss marks readiness unavailable,
blocks new gateway admission, and enters the same supervised shutdown path as
a critical component failure. It records only `instance_lock_lost` and exits
nonzero. Explicit shutdown releases the lock idempotently and is not reported
as a loss.

## 4. Startup State Machine

The application moves through these states:

```text
configuring -> locking-instance -> opening-app-pool -> locking-migrations
-> migrating -> bootstrapping -> constructing -> loading-runtime-settings
-> binding-listeners -> starting-components -> ready
```

The exact sequence is:

1. load and validate configuration and secret files;
2. in safe mode, open the instance connection and acquire the instance lock;
3. open and ping the application pool;
4. open the migration lock connection and acquire the migration lock;
5. run migrations through the migration executor;
6. bootstrap the single administrator;
7. release migration resources;
8. construct repositories and services;
9. open the initial dedicated LISTEN connection when alerts are enabled;
10. load and validate the first gateway-settings snapshot;
11. construct supervised background components;
12. bind the main and optional metrics listeners;
13. start components and transition to Ready.

A second safe instance exits before steps 3-13. Unsafe mode skips only the
runtime instance lock, emits `unsafe_multi_instance_enabled`, and still runs
steps 3-7 under the migration lock.

## 5. Lifecycle And Request Draining

Three roots have separate ownership:

- `signalCtx` receives SIGINT/SIGTERM and is never an HTTP base context.
- `requestRootCtx` owns accepted HTTP requests and remains live during drain.
- `backgroundCtx` owns periodic runners and dispatch work.

A small supervisor owns component start, unexpected-exit reporting, stop, and
wait. Components have narrow `Start`, `Stop`, and `Wait` behavior; cleanup is
idempotent. No unobserved `go runner.Run(ctx)` remains in `main.go`.

Shutdown uses one absolute deadline. Default internal timeout is 25 seconds,
with a 20-second request-drain allocation. Both are range-validated, and the
request-drain timeout cannot exceed the global timeout. Compose uses a
35-second grace period.

```text
signal or critical failure
-> draining=true and readiness=false
-> stop main listener from accepting new connections
-> HTTP Shutdown while requestRootCtx and dependencies remain live
-> cancel requestRootCtx only when drain allocation expires
-> cancel and wait for background components
-> stop alert dispatcher and LISTEN
-> stop metrics listener last
-> release instance lock
-> close application pool
-> return signal-success or critical-failure exit code
```

Metrics remains readable while the main server drains. A metrics-listener
failure is critical. The global deadline is not reset for individual
components.

Readiness is false before startup completes, while draining, after instance
lock loss, without a valid settings snapshot, when static assets are missing,
or when PostgreSQL cannot be reached. `/livez` continues to report only that
the process and handler can respond. Provider availability does not affect
liveness or readiness.

## 6. Gateway Settings Last-Known-Good Runtime

`GatewaySettingsRuntime` owns an immutable snapshot published through an
atomic pointer. A snapshot contains the validated settings, persisted
`updated_at` version, `LoadedAt`, refresh attempt/success/failure timestamps,
stable last error code, consecutive failures, source, stale state, and whether
a persisted record has ever loaded successfully.

Valid sources are `persisted`, `startup-default`, and `last-known-good`. A
missing settings row is not an error: validated startup defaults become a
valid `startup-default` snapshot. A database error or invalid persisted JSON
at first load leaves no snapshot and prevents readiness.

Gateway requests read only this runtime. They do not query or deserialize the
settings row. The runtime refreshes periodically with bounded jitter. An admin
update commits PostgreSQL first, publishes the committed value immediately,
and schedules an immediate reload when publication cannot be confirmed. A
failed refresh retains the prior value, marks it stale, emits a bounded stable
error, and never falls back to zero-value limits.

Authenticated health and low-cardinality metrics expose validity, stale state,
snapshot age, refresh counts, and the stable last error code. Values and
sensitive configuration are never labels.

## 7. API Key Authentication And Last-Used Semantics

Authentication uses one PostgreSQL statement and one UTC timestamp. The
statement matches the key hash and active state as one consistent snapshot and
conditionally updates `last_used_at` only when it is null or older than one
minute. It returns the authenticated key from the same statement.

This provides at most one authentication database round trip per request and
at most one persisted `last_used_at` write per key per minute, including under
concurrency. A disable or revoke committed before a new authentication
statement begins is observed by that statement. Database errors fail closed
with `authentication_unavailable`; a skipped or failed touch never turns a
valid authentication into an unrelated 500.

Time is injected for deterministic tests and normalized to UTC. Shutdown has
no asynchronous touch queue to flush because the conditional write is part of
authentication.

## 8. Durable Budget Ledger

Request Log remains observational and is no longer a budget authority.
PostgreSQL receives three additive structures:

- per-key budget state with current 24-hour and 30-day counters, initialization
  status, runner status, and version;
- one server-generated admission record per external gateway request, with
  unique idempotency ID, admitted time, settlement state, observed tokens and
  cost, and the two expiry states;
- indexes for pending initialization, unsettled admissions, and the next 24h
  and 30d expiry work.

### Request budget

An authenticated, structurally valid external gateway request consumes one
request unit immediately before upstream selection. Local authentication,
validation, rate-limit, concurrency-limit, and budget rejections consume none.
Once admitted, upstream errors, cancellation, client disconnect, and an empty
usage result do not refund the request unit. Fallback and retry reuse the same
admission ID and cannot charge again.

Admission locks one key-state row, applies already-processed counters, checks
both request limits, increments both counters, and inserts the unique admission
in one transaction. A rejection performs no insert or increment. Database
failure and incomplete legacy initialization fail closed.

Expiry is exact to the admission timestamp. A bounded runner decrements 24h
and 30d counters from due admission rows under row locks. If it falls behind,
state over-counts rather than under-counts and may conservatively reject until
caught up. Admission never performs a 30-day scan and its query count and row
work are constant.

### Token and cost budgets

The request body and upstream response do not provide a trustworthy universal
upper bound. Token and cost limits therefore become explicitly named
**Observed Usage Budgets**, not hard limits. Admission rejects when already
settled observed usage is at or above a configured limit, but one request or
concurrent requests can exceed it.

Settlement uses the admission ID and is atomic and idempotent. The first
settlement adds actual parsed usage and pricing to both rolling counters;
retries return the prior result without charging twice. Missing or unknown
usage settles as zero and remains observable. Request Log write failure does
not affect settlement. Unsettled records are reclaimed by a bounded runner:
request units remain charged, token/cost settle to zero, and the state records
the abandoned outcome.

### Upgrade and compatibility

Migration creates pending state for existing keys with any configured budget.
A bounded, restartable backfill initializes each key from the current 30-day
Request Log window under a per-key advisory lock, then marks it ready. New keys
start ready at zero. Until a legacy key is ready, budgeted gateway requests
fail closed with `budget_initializing`; unbudgeted keys continue normally.
Backfill is idempotent and does not rewrite Request Logs.

Existing numeric budget fields remain the configuration source. API responses
add the initialization/stale state and the observed token/cost terminology.
Budget alerts consume the durable state and admission transitions, preserving
80%, 100%, recovery, restart idempotency, and revoke recovery without scanning
Request Logs.

## 9. Bounded Management Queries

API key and routing-pool lists use signed keyset cursors. The cursor is an
opaque base64url payload authenticated with HMAC derived from the encryption
secret. It binds resource type, ordering values, and a digest of normalized
filters so it cannot be replayed under different filters.

- default page size: 50;
- maximum page size: 100;
- stable order: `created_at DESC, id DESC`;
- response: collection, `nextCursor`, and `hasMore`;
- no exact total-count query;
- malformed, tampered, expired, or filter-mismatched cursors return
  `invalid_cursor`.

API keys use one page query, one batched allowed-model query, one batched
budget-state query, and in-memory runtime snapshots for concurrency and rate
data. Routing pools use one page query and one batched member query. Query
counts are constant with page size. Bulk mutations continue to operate only on
explicit IDs; frontend selection never implicitly includes unloaded pages.

Other management surfaces are changed only where query instrumentation proves
an N+1. Profiles seed scalable synthetic data, run `EXPLAIN (ANALYZE, BUFFERS)`,
and store only stable plan summaries and assertions.

## 10. PostgreSQL Transport Classification

Startup classifies every parsed pgx primary and fallback attempt:

- `plaintext`: `TLSConfig == nil`, requiring `database-plaintext`;
- `unverified-tls`: TLS exists but does not verify both the certificate chain
  and hostname, requiring `database-unverified-tls`;
- `verified-full`: TLS verifies the chain and hostname, requiring no risk.

For pgx, full verification requires a non-nil TLS configuration with
`InsecureSkipVerify == false` and a non-empty server name for every network
attempt. `verify-ca` verifies the chain but intentionally skips the hostname,
so it remains `unverified-tls`. Any plaintext fallback makes the connection
plaintext-capable; any unverified fallback requires the independent risk.
Unix-socket local use follows the existing plaintext acceptance path.

Validation uses parsed configuration, not string matching, and never returns
the DSN, password, or certificate contents in an error. Production examples use
`sslmode=verify-full` and a trusted root certificate.

## 11. API Key Secret Reveal Step-Up

`GET /api/admin/keys/{id}/secret` is removed and receives method-not-allowed
behavior. The replacement is:

```text
POST /api/admin/keys/{id}/reveal-secret
{"currentPassword":"..."}
```

The handler revalidates the current session, reserves admission in a bounded
in-memory throttle keyed independently by normalized client IP, admin ID, and
key ID, and only then performs the expensive password verification. Every
attempt counts, including successes. The throttle has bounded storage, retry
delay, and stable `Retry-After` output. Restart reset is an accepted
single-instance limitation.

Success and failure emit sanitized security events without password, secret,
key prefix, or raw errors. Successful responses use `Cache-Control: no-store`.
The frontend requests the password for every reveal/copy, keeps the returned
secret only in dialog-local state, clears password and secret on close, and
uses neither URLs nor browser storage. The one-time secret returned when a key
is created moves out of the module-global state and follows the same
dialog-local lifetime.

## 12. Administrator Password Hashing

Passwords are never trimmed. Usernames retain normalization. A narrow
`PasswordHasher` supports:

```go
Hash(password string) (string, error)
Verify(encoded, password string) (valid bool, needsRehash bool)
```

New hashes use an Argon2id PHC string with a 16-byte random salt and 32-byte
output. The initial small-VPS parameters are 64 MiB memory, two iterations, and
one lane. Parsing rejects unknown versions, malformed fields, memory above
128 MiB, iterations above 5, lanes above 4, and oversized salt or output before
allocating work. Benchmarks are recorded but are not pass/fail timing tests.

Legacy `pbkdf2-sha256$210000$...` hashes remain verifiable. Successful legacy
login computes Argon2id and applies a compare-and-swap update against the hash
that was verified. A concurrent password change therefore wins and cannot be
overwritten. Rehash failure is observable but does not invalidate an otherwise
successful login or create an extra session. New and changed passwords use
Argon2id immediately.

Unknown and empty usernames verify a precomputed valid dummy Argon2id hash so
the path has comparable work without allocating attacker-controlled parameters.
A shared process semaphore bounds concurrent password verification across
login, password change, reveal, and dummy paths so distributed identities
cannot multiply Argon2id memory use without limit.

## 13. Migration And Rollback Boundaries

Migrations are additive, use the next sequential numbers, preserve all current
entities, use `timestamptz`, and add explicit checks, foreign keys, unique
idempotency constraints, and bounded indexes. No migration performs a long
Request Log rewrite. Legacy budget initialization is post-migration background
work.

Before rollout, take a tested backup. Application rollback is allowed while
new additive tables and indexes remain. Database Down is supported before new
budget admissions exist. Once the new ledger has accepted requests, rolling
back the schema or old application would discard authoritative budget state;
the supported rollback is restore from the pre-upgrade backup or forward-fix.

Migration tests cover empty Up, previous-current to latest, Down, restored
fixtures, interrupted budget initialization, and encryption-key compatibility.

## 14. Failure Policy

| Failure | Policy |
| --- | --- |
| Authentication database read | fail closed |
| Budget admission/initialization | fail closed |
| No valid settings snapshot | not Ready; block gateway |
| Settings refresh with prior snapshot | use last-known-good; stale and observable |
| Conditional last-used touch | authentication remains valid; observable only if the statement can still establish active state |
| Request Log write | request result and budget remain authoritative; bounded observability failure |
| Budget settlement | durable retry by admission ID; never double charge |
| Instance lock loss | not Ready; supervised nonzero shutdown |
| LISTEN disconnect | bounded reconnect; visible degraded alert delivery |
| Alert delivery | best effort, bounded queue/retry, explicitly not transactional delivery |
| Provider unavailable | gateway error; no liveness/readiness failure |

## 15. Deployment, CI, And Operations

Release Compose receives configurable CPU, memory, PID, `nofile`, logging, and
`/tmp` bounds with small-VPS defaults, and a stop grace period longer than the
internal shutdown timeout. Secret values can come from `_FILE` alternatives;
setting both forms fails startup, only bounded regular files are accepted, and
exactly one trailing newline is removed.

Production deployment validates an immutable `image@sha256:...` reference and
can compare it with the running container. CalVer remains a human label.
Retention stays opt-in but health exposes whether it is configured, oldest log
time, bounded row estimate, relation size, explicit persisted policy selection,
and `unknown`, `ok`, `watch`, or `high` disk-risk state. Alert delivery remains
best-effort in this scope and its queue drops, listener state, reconnect count,
and last failure are explicit in UI and health.

CI adds pinned static analysis, `go vet`, critical-package race tests,
migration Up/Down/upgrade tests, shutdown/SSE tests, budget concurrency tests,
query-count tests, and Compose configuration validation. Full race testing may
run on a scheduled/manual workflow when it exceeds pull-request time limits.

## 16. Compatibility Impact

- Gateway routes and downstream authentication remain unchanged.
- `GET /api/admin/keys/{id}/secret` is intentionally replaced by step-up POST.
- API key and routing-pool list responses become cursor-paginated; the bundled
  frontend migrates in the same change.
- Request budget remains a strict rolling hard limit. Token and cost budgets
  are relabeled observed-usage limits to make their actual guarantee explicit.
- Existing budgeted keys temporarily fail closed during bounded initialization
  rather than receiving an empty budget.
- New configuration has validated defaults; existing local Compose plaintext
  operation remains available through its explicit accepted risk.
