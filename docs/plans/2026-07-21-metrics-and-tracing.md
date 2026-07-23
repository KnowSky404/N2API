# Metrics And Tracing Plan

Status: selected metrics scope completed locally; OpenTelemetry tracing is not selected for this iteration
Public API changes: optional `/metrics` on a separate listener; no telemetry by default
Data migration: none

## Evidence Status (2026-07-23)

| Dimension | Status | Evidence and remaining gate |
| --- | --- | --- |
| `design` | complete | Commit `20b42e6` defines the bounded metrics contract. The current iteration explicitly excludes OpenTelemetry tracing. |
| `implementation` | complete | Local commits `39389e6` and `57029b9` implement the selected optional Prometheus scope, including alerting, readiness, and background-task state. |
| `merged` | partial | The contract commit `20b42e6` is on GitHub `main` at `3664abe`; the two implementation commits remain local. |
| `local_tests` | complete | Focused tests cover disabled mode, bind/auth policy, loopback listener, bind failure, concurrency, SSE lifetime, cancellation, shutdown, labels, secret canaries, readiness, alert queue/delivery, histogram output, and a measured 1,516-series owned budget below 1,600. Commits `ada47b4` and `bbb1a39` add real scrape behavior for SSE cancellation and unavailable PostgreSQL pools plus process-level disabled-listener and bind-failure exit tests. |
| `ci` | pending | No GitHub Actions run contains the metrics commits. |
| `release_artifact` | pending | No published image digest contains the metrics listener. |
| `operator_acceptance` | pending | Scrape the deployed loopback/private listener, verify bearer handling where applicable, and connect an external readiness monitor without exposing the listener publicly. |
| `owner_decision` | complete | Metrics are disabled by default on a separate loopback listener; non-loopback requires bearer auth; tracing is out of scope. |

## Current Baseline

The Operations page derives errors, throughput, latency, account health, and
cost from PostgreSQL. There is no Prometheus endpoint or trace propagation.
Request logs and System Events remain the durable records; metrics/traces must
not duplicate their high-cardinality identifiers or sensitive content.

## Task 1: Define A Cardinality Budget

Status: completed locally on 2026-07-21

### Goal

Freeze metric names, labels, and prohibited data before adding a dependency.

### Dependencies

Health/task status and gateway E2E.

### Files

- Create: `docs/specs/2026-07-21-n2api-metrics-contract.md`
- Modify: this plan after review

### Implementation

Define request/status/latency/active/fallback/provider-health/refresh/limit/
budget/token/cost/log-write/event-write/task/alert/database-pool metrics. Ban
request ID, session ID, full key/account/pool names, token values, bodies, and
full errors. Decide whether model/account/pool dimensions are omitted or
allowlisted with hard bounds.

The accepted contract is
[`docs/specs/2026-07-21-n2api-metrics-contract.md`](../specs/2026-07-21-n2api-metrics-contract.md).
It omits model, account, pool, key, request, session, and correlation dimensions;
maps every runtime enum through a closed allowlist with `other` as the only
fallback; caps N2API-owned metrics at 1,600 series and the complete scrape at
2,000; and fixes histogram buckets before an implementation library is chosen.

### Completion Criteria

Every metric has type, unit, label set, cardinality estimate, and owner use case.

Local evidence: the contract maps the current gateway route patterns, routing
errors, account types/states, usage sources, task status, and `pgxpool.Stat`
signals to a worst-case 1,301 emitted N2API-owned series. Future alert metrics
are reserved but not registered before alerting exists.

### Commit

`docs(metrics): define bounded observability contract`

## Task 2: Add Optional Prometheus Metrics

Status: completed locally with a loopback default and mandatory bearer token for
every non-loopback bind; implementation remains disabled by default.

### Goal

Expose the approved metrics without changing request behavior.

### Dependencies

Task 1 and Context7 review of the selected current Go metrics library.

### Files

- Modify: `backend/go.mod`, `go.sum`
- Create: `backend/internal/metrics/` and tests
- Modify: gateway/provider/admin/store instrumentation and `main.go`
- Modify: config, `.env.example`, Compose example, manual

### Implementation

Default disabled. Bind on a separate `127.0.0.1:9090` listener. Require an
operator bearer token for every non-loopback bind and never expose the handler
through the public gateway. Use a private Prometheus registry with explicit Go,
process, build, N2API, and PostgreSQL pool collectors. Record errors using
stable codes and instrument `pgxpool.Stat()` without query text.

### Tests And Verification

Test disabled behavior, auth/bind, label bounds, concurrent metrics, SSE
lifetime, and gateway correctness under instrumentation. Run a scrape smoke.

### Compatibility And Security

N2API starts without Prometheus and makes no outbound connection.

### Risks And Rollback

Metrics can create memory/cardinality pressure. Disable registration/export.

### Completion Criteria

A bounded scrape reflects gateway and background-task traffic, remains available
during PostgreSQL failure, and contains no prohibited value. The implementation
stays below 1,600 N2API-owned series and 2,000 complete scrape series.

### Commit

`feat(metrics): expose optional Prometheus metrics`

## Task 3: Add Optional OpenTelemetry Tracing

### Goal

Trace gateway stages without bodies, credentials, or collector dependency.

### Dependencies

Task 2 and explicit operator need.

### Files

- Modify: Go dependencies/config/manual
- Create: `backend/internal/telemetry/` and tests
- Modify: gateway/provider/store boundaries

### Implementation

Default off; no-op provider without configuration. Create spans for auth,
limits, routing, selection, refresh, upstream, stream, usage, pricing, and
persistence. Define head sampling, SSE completion/cancellation, and detached
log-persistence parent linkage. Exporter failure never affects requests.

### Tests And Verification

Use an in-memory exporter to assert names/attributes and absence of secrets;
test missing collector, timeout, SSE disconnect, and request cancellation.

### Risks And Rollback

Tracing can increase allocation and leak attributes. Disable with configuration
and remove instrumentation provider.

### Completion Criteria

Opt-in traces show stage timing with only approved bounded attributes.

### Commit

`feat(tracing): add optional gateway OpenTelemetry spans`
