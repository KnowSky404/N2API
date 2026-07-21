# N2API Reliability and Operations Design

Status: accepted baseline for phased implementation
Date: 2026-07-21
Scope: personal and small self-hosted deployments, single application instance by default

## 1. Background

N2API already provides a functional OpenAI-compatible gateway with Codex OAuth
accounts, API-key upstreams, routing pools, sticky sessions, local limits,
budgets, pricing, request attribution, system events, and a static admin UI.
The next stage is not broader platform functionality. It is the work required
to operate the existing gateway safely and predictably for months or years.

This design treats the current source and tests as the system of record. Older
documents remain useful implementation history, but do not override runtime
behavior.

## 2. Current State

The repository is a Go monolith backed by PostgreSQL. `cmd/n2api/main.go` runs
migrations before listening, bootstraps the single administrator, starts API
key cleanup, optional System Event cleanup, and provider auto-tests, then
serves the gateway and the SvelteKit static build from one HTTP server.

The gateway path is already materially complete: authentication, routing-pool
scoping and fallback, account health and concurrency checks, serialized OAuth
refresh, streaming proxying, usage parsing, pricing, and request-log
attribution exist. Unit and component coverage is extensive, and a real Codex
CLI/OAuth path has been manually proven. What is missing is a repeatable,
secret-free full-stack contract suite and the surrounding operational safety.

## 3. Repository Research Scope

The baseline review covered:

- `README.md`, `docs/README.md`, `docs/manual.md`, `DESIGN.md`, and `AGENTS.md`.
- Existing records under `docs/superpowers/specs/` and
  `docs/superpowers/plans/`.
- `.github/workflows/ci-image.yml` and `.github/workflows/release.yml`.
- Backend HTTP, gateway, provider, admin, store, migration, encryption, and
  background-task code.
- Frontend admin state, Gateway, Request Logs, System Logs, provider, API key,
  and operations pages and tests.
- `deploy/Dockerfile`, development and release Compose files, and
  `.env.example`.
- The latest 100 commits through `27d1e36`.

## 4. Existing Capability Inventory

| Area | Current evidence | State |
| --- | --- | --- |
| Gateway protocol | `internal/gateway/proxy.go` and broad `proxy_test.go` cases for models, Responses, Chat Completions, SSE, errors, retries, and usage | Implemented at component level |
| Real gateway closure | `docs/superpowers/specs/2026-07-14-oauth-gateway-closure-design.md` and related commits | Manually proven, not repeatable PR CI |
| OAuth refresh | Account-scoped refresh serialization and rejected-token recovery in `internal/provider/service.go` | Implemented |
| Routing | Required pool binding, explicit fallback chain, sticky sessions, budgets, and concurrency limits | Implemented |
| Request Logs | Rich filters and attribution in `AdminRepository.ListRequestLogs`; local UI pagination | Partially operationalized |
| System Events | Structured events, signed `(occurred_at,id)` cursor, filters, audit coverage, and batched retention | Implemented foundation |
| Admin sessions | Hashed PostgreSQL sessions, HttpOnly/SameSite cookies, logout and password change | Partial security baseline |
| Health | `/healthz` liveness and database-backed `/api/admin/health` | Insufficient probe separation |
| Container delivery | Multi-architecture CI smoke tests, digest-preserving publishing, CalVer release flow | Strong supply path, weak runtime hardening |
| Backups | Manual `pg_dump` and rollback guidance in `docs/manual.md` | Documented, not restore-tested |
| Observability | Operations queries and admin dashboard | No metrics or tracing export |

## 5. Confirmed Gaps

1. No deterministic full-stack test drives a client key through PostgreSQL,
   routing, mock upstream, streaming, usage pricing, and persisted attribution.
2. `/healthz` cannot distinguish liveness from readiness, and the application
   container has no Compose healthcheck.
3. Login has no IP or username throttling; session TTL is fixed at seven days;
   active-session listing and revocation are absent.
4. `clientIP` trusts `X-Forwarded-For` and `X-Real-IP` from every peer. This can
   poison OAuth fingerprints and future rate limits.
5. There is no uniform security-header middleware or explicit same-origin
   mutation policy.
6. Startup rejects missing secrets but not known placeholders, weak passwords,
   equal admin/encryption secrets, invalid public origins, or risky deployment
   combinations.
7. Request Log pagination is local over a bounded newest-row fetch; the backend
   has no cursor page contract.
8. Request Log cleanup is manual and deletes the complete eligible set in one
   transaction. Export first loads rows and is capped by the service at 200.
9. The runtime image uses root; Compose has no application `read_only`,
   `cap_drop`, `no-new-privileges`, `tmpfs`, or application healthcheck. Ports
   bind all host interfaces by default.
10. Encrypted values have no versioned envelope or rotation workflow.
11. Backups are not automatically restored and verified.
12. System Events have no notification rules or delivery actions.
13. `load_factor` is a descending sort tier, not proportional load balancing.
14. There is no Prometheus endpoint or optional OpenTelemetry tracing.
15. Governance lacks a security policy, contribution templates, vulnerability
    jobs, container scanning, and SBOM publication.

## 6. Documentation and Code Differences

- The Request Logs UI test accurately calls its behavior local pagination;
  external planning must not describe it as server pagination.
- Request Log export links advertise CSV and JSONL, but `ListRequestLogs`
  clamps the export fetch to 200 rows. It is not a large-data export path.
- Request Log retention in the manual is explicitly manual; it is not a
  background lifecycle policy. System Event retention is background and
  batched, and should be reused as the pattern.
- `load_factor` presentation implies weight/capacity, while selection orders
  higher values first and only balances equivalent tiers using last-use and ID.
- `/api/admin/health` is described as an admin status endpoint but is currently
  public and only reports database reachability.
- Existing active-looking documents under `docs/superpowers/` are dated
  feature delivery records. This design establishes `docs/specs/` and
  `docs/plans/` as the active cross-cutting operations roadmap without deleting
  historical context.

## 7. Non-goals

No public registration, tenant model, billing, recharge, payment, merchant
accounting, complex RBAC, microservices, Kafka, Kubernetes, mandatory Redis,
bulk provider expansion, UI redesign, desktop/mobile client, hosted control
plane, or user telemetry is introduced by this program.

## 8. Design Principles

- Preserve the single-process, PostgreSQL-backed deployment as the default.
- Fail closed at authentication, proxy trust, secret, and routing boundaries.
- Keep liveness independent of dependencies and readiness independent of
  transient provider availability.
- Prefer bounded work, cancellation, stable cursors, and small transactions.
- Reuse System Events, existing admin settings, routing preview, provider test
  status, and request attribution.
- Do not place real OpenAI credentials in ordinary CI.
- Make new controls disabled or conservative by default and backward
  compatible where safety permits.
- Every phase must be independently testable, deployable, and reversible.

## 9. Overall Architecture

```text
SDK / Codex / operator
        |
  HTTP safety middleware
        |
  auth + limits + routing -------- /livez (process only)
        |                           /readyz (DB + initialized assets)
  provider adapter                /metrics (optional, no secrets)
        |
  mock or real upstream
        |
  usage + pricing + Request Log
        |
     PostgreSQL
        |
  bounded background tasks
  retention / auto-test / alerts / rotation status
```

PostgreSQL remains authoritative for durable configuration and operational
history. In-process coordination is acceptable for single-node throttling,
task schedules, and bounded notification delivery. Features that require
cross-instance coordination must use PostgreSQL advisory locks before another
service is considered.

## 10. Workflow Detailed Design

### Gateway validation

A dedicated mock OpenAI-compatible service exposes scripted JSON, SSE, status,
timeout, malformed usage, and disconnect scenarios. A Compose E2E stack starts
PostgreSQL, mock upstream, and N2API, provisions data through supported service
or admin APIs, drives HTTP/SDK contracts, then verifies Request Log rows in
PostgreSQL. Ordinary CI uses no external account. A separate manual workflow
may validate a protected real account and must clean created keys and logs.

### Security

Trusted proxy CIDRs are parsed at startup. Forwarded headers are considered
only when the direct peer is trusted, and the client address is selected by
walking the chain from the closest proxy toward the client until the first
untrusted hop. Login throttling combines normalized IP and normalized username
keys with uniform error bodies and `Retry-After`. PostgreSQL sessions gain
operator-visible metadata and revocation controls. TOTP remains optional and
is sequenced after rate limiting and session management.

SameSite=Lax, HttpOnly cookies and same-origin JSON requests materially reduce
CSRF risk, but do not cover every deployment. State-changing cookie-authenticated
admin requests will validate `Origin` when present and reject cross-origin
requests; CORS remains disabled. A synchronizer token is not added unless a
browser compatibility test demonstrates that origin enforcement is
insufficient.

### Operations

Background tasks expose last start, last success, last error code, and running
state through a shared in-memory status registry. Durable outcomes remain
System Events. Request Log retention deletes ordered batches and uses a
PostgreSQL advisory lock to avoid duplicate multi-instance cleanup. Alerts
consume explicit operational signals and never block gateway requests.

## 11. API Design

- `GET /livez`: always `200 {"status":"ok"}` while HTTP is responsive.
- `GET /readyz`: `200` when database and static admin assets are available;
  otherwise `503` with stable component codes and no secret detail.
- `GET /healthz`: compatibility alias with documented semantics; initially
  retains liveness behavior while deployment probes migrate to `/readyz`.
- `GET /api/admin/health`: authenticated detailed component and task status.
- `GET /api/admin/request-logs?cursor=&limit=`: signed keyset pagination with
  `logs`, `hasMore`, and `nextCursor`.
- `GET /api/admin/request-logs/export?format=csv|jsonl`: bounded streaming
  output; JSON array export is retained only for small compatibility requests.
- Session, alert, backup/import, rotation, and metrics APIs are introduced only
  in their specific plans and remain under admin authentication except public
  probes and optional metrics.

## 12. Database Design

Schema changes are additive and phased:

- Session metadata and revocation indexes.
- Optional login-throttle persistence only if restart persistence is selected;
  the recommended first implementation is bounded memory for one node.
- Request Log query indexes based on measured `EXPLAIN (ANALYZE, BUFFERS)`.
- Notification actions/rules and encrypted action secrets.
- Version markers for ciphertext envelopes and resumable rotation runs.
- Import jobs only if configuration restore cannot remain synchronous and
  bounded.

No PostgreSQL extension is required initially. Trigram search is deferred until
real row counts and query plans justify its write and storage cost.

## 13. Configuration Design

Planned configuration includes:

- `N2API_TRUSTED_PROXY_CIDRS` (empty means trust no proxy).
- Configurable session TTL and login throttle thresholds.
- Request Log retention interval and batch size with safe bounds.
- Build-time `N2API_VERSION`, commit, and build time values.
- Metrics and tracing enablement, bind address, sampling, and exporter values.
- Current and explicitly named previous encryption keys during rotation.

Configuration parsing must fail on invalid values without echoing secret
contents. New options are documented in `.env.example` with conservative
defaults.

## 14. Frontend Interaction Design

Operational additions follow `DESIGN.md`: dense status rows and tables, no new
landing surface, and details in focused modals. Health and task summaries fit
the Dashboard or Operations pages. Sessions live under the existing account
menu/settings flow. Request Logs keep URL-backed filters; a cursor-backed Load
more interaction replaces client-side page slicing. Destructive rotation,
restore, and session revocation use explicit confirmation and preserve Save,
Cancel, and close behavior.

## 15. Security Analysis

The most immediate risks are spoofed client identity, unbounded login attempts,
weak deployment secrets, root containers, and bulk operational queries. The
program addresses these before optional TOTP, tracing, or update checks.
Secrets remain encrypted at rest, excluded from System Event metadata, and
never returned by backup/export endpoints without a separate encrypted backup
format. Metrics labels and traces never include request IDs, session IDs, API
key values, token values, response bodies, or full error strings.

## 16. Threat Model

In scope:

- Internet clients reaching the gateway and login endpoint.
- Attackers spoofing forwarding headers through an untrusted direct peer.
- Cross-site browser requests against cookie-authenticated admin APIs.
- Compromised or malformed upstreams returning misleading content types,
  infinite streams, errors, or malformed usage.
- Accidental weak production configuration and excessive host exposure.
- Stolen database backups or historical ciphertext.
- Resource exhaustion through logs, exports, metrics labels, or notifications.

Out of scope for V1 is protection after full host/root compromise. The operator
is responsible for TLS termination, host firewalling, PostgreSQL access, and
secure secret distribution.

## 17. Observability

Prometheus metrics provide bounded labels for route class, status class,
provider type, and outcome. Account, pool, model, and key identifiers are
excluded by default. OpenTelemetry is optional, off by default, body-free, and
able to run without a collector. System Events remain the durable operator
timeline and are not replaced by metrics or traces.

## 18. Data Retention

Request Logs and System Events have independent retention. Request Log cleanup
uses `(created_at,id)` batches, reports oldest/newest/count estimates, records
successful and failed runs, and can be disabled. Export enforces time and row
bounds and respects cancellation. Notification delivery records retain only
redacted destinations and bounded failure detail.

## 19. Migration Compatibility

Public OpenAI-compatible routes remain unchanged. `/healthz` remains available
while new probes are adopted. Cursor pagination is added without removing the
current `logs` field until the frontend and external operators migrate. Legacy
ciphertext remains readable throughout key rotation. Every schema migration is
forward-only in production with a documented backup/restore rollback path when
down migration would risk data loss.

## 20. Performance Impact

Readiness adds a bounded database ping and static file stat only when probed.
Login throttles and task state are O(1) bounded maps in the first single-node
implementation. Cursor pagination avoids increasing offsets. Retention and
export operations use ordered batches and request cancellation. Indexes are
added only after measuring representative query plans.

## 21. Failure Handling

- Provider failure never makes the whole application not ready.
- Readiness reports stable component codes and `503`, never stack traces.
- Cleanup stops on the first failed batch, preserves undeleted rows, and emits
  one failure signal per cooldown.
- Notification queues are bounded; overflow is counted and aggregated without
  recursively alerting on alert-delivery events.
- Rotation verifies every rewritten ciphertext before advancing durable state.
- Restore always targets a temporary database before an operator swaps data.

## 22. Rollback Strategy

Each task is delivered as an atomic Conventional Commit. Runtime flags keep
metrics, tracing, notifications, TOTP, and automatic cleanup independently
disableable. Probe adoption is rolled back by pointing Compose/CI to
`/healthz`. Additive columns may remain unused after rollback. Destructive data
operations require a verified PostgreSQL backup; rollback is restore, not an
unsafe down migration.

## 23. Test Strategy

- Unit: parsers, proxy-chain selection, cursor signatures, encryption envelopes,
  alert dedupe, and bounded task state.
- Component: HTTP handlers, gateway proxy, store queries, retention batches,
  and notification adapters.
- Integration: Go services with a real PostgreSQL database and mock upstream.
- Protocol contract: raw HTTP plus official OpenAI JavaScript and Python SDKs.
- Browser E2E: login, session management, operational health, Request Log
  filters/pagination/export, and alert test actions.
- Manual external acceptance: protected workflow for real OAuth and Codex CLI,
  never a required PR check.

CI uploads sanitized container logs, mock scenario identifiers, JUnit/Playwright
reports, and bounded test request records on failure. It never uploads tokens or
response bodies from real-account tests.

## 24. Release Strategy

Continue the current tested-digest, multi-architecture, CalVer release model.
Phase changes land in small PRs. Migrations, container identity, and probe
changes receive explicit upgrade notes. The release workflow promotes the
already-smoke-tested image and does not rebuild it.

## 25. Acceptance Criteria

- A secret-free Compose E2E suite proves the full gateway attribution loop.
- Login and forwarded-address handling have explicit abuse and trust boundaries.
- Liveness, readiness, and authenticated operational health have distinct
  semantics and automated tests.
- Request Logs can retain, page, and export large histories without loading or
  deleting the entire eligible set at once.
- The application image runs as a fixed non-root user under a hardened Compose
  profile and reports build identity.
- A backup can be automatically restored into an isolated database and pass a
  minimal gateway test.
- Encryption keys can be rotated resumably without losing legacy readability.
- Alerts, metrics, and optional traces cannot block requests or leak secrets.
- Governance checks produce actionable artifacts without selecting a license
  on the owner's behalf.

## 26. Open Questions

1. Which license should the owner select after reviewing the governance plan?
2. Should release Compose default to loopback binding, accepting a deployment
   behavior change, or retain all-interface binding with a prominent warning?
3. Should password change revoke all other sessions by default?
4. Is TOTP required in the first security milestone or after session controls?
5. Should encryption rotation be CLI-only initially, or also expose an admin
   operation after the CLI path is proven?
6. Is an encrypted full configuration backup needed, or are PostgreSQL backups
   plus a non-sensitive portable export sufficient?
7. Should `/metrics` be loopback-only by default or authenticated through an
   operator-configured bearer token?

## 27. Follow-up Direction

Implementation follows the indexed plans in `docs/plans/README.md`. The first
executable slice is liveness/readiness separation because it is additive,
low-risk, and immediately strengthens local Compose and CI. Gateway E2E and
trusted proxy/login protections then proceed in parallelizable small commits.
Later phases must not block these foundations on alerting, tracing, or license
selection.
