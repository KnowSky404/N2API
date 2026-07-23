# Gateway E2E Compatibility Plan

Status: in progress
Public API changes: none; tests validate existing `/v1/*` contracts
Data migration: none

## Evidence Status (2026-07-23)

| Dimension | Status | Evidence and remaining gate |
| --- | --- | --- |
| `design` | complete | Tasks 1-5 define PostgreSQL-backed gateway, protocol/client, mock-upstream, and sanitized failure-evidence contracts. |
| `implementation` | complete | Local commits `e67600a`, `db02f13`, `3dd65df`, `f22c678`, and `9fcf9d6` implement the planned harness; later boundary, transport, affinity, cancellation, and correlation fixes build on it. |
| `merged` | pending | The cited commits and later fixes exist only on the local `main` branch, which is ahead of `origin/main`; no remote merge is claimed. |
| `local_tests` | partial | Mock PostgreSQL/upstream, raw HTTP, SDK, streaming, fallback, affinity, proxy, response-bound, cancellation, and artifact-redaction coverage exists. Real OAuth/Codex and reverse-proxy acceptance remain external. |
| `ci` | pending | No GitHub Actions run contains the local commits or later gateway fixes. |
| `release_artifact` | pending | No tested or published image digest contains the local commits. |
| `operator_acceptance` | pending | Exercise a real OpenAI OAuth account, Codex CLI, reverse proxy, streaming, and request-log attribution without exposing credentials. |
| `owner_decision` | complete | Protected real-account checks remain manual and secret-safe; they must not upload tokens, bodies, dumps, or callback material. |

## Current Baseline

`backend/internal/gateway/proxy_test.go` already covers most gateway branches
with fake services and `httptest` upstreams. Store tests that need PostgreSQL
skip unless `N2API_STORE_TEST_DATABASE_URL` is set, and CI does not set it. A
real Codex OAuth path was manually accepted in July 2026, but no ordinary CI
test proves the full persisted loop. Real-account tests must remain manual.

## Task 1: Run PostgreSQL Integration Tests in CI

Status: completed locally on 2026-07-21; pending the next authorized push and
GitHub Actions run.

### Goal

Stop silently skipping repository integration tests in the `Test` job.

### Dependencies

None.

### Files

- Modify: `.github/workflows/ci-image.yml`
- Test: `backend/internal/store/admin_test.go`, `provider_test.go`, and related store tests
- Migrate: none
- Document: CI comments only if the service contract is non-obvious

### Implementation

1. Add a PostgreSQL service with a unique CI password and health options.
2. Set `N2API_STORE_TEST_DATABASE_URL` only for backend tests.
3. Add an explicit preflight that fails if the database is unreachable.

### Tests And Verification

Run `go test -v ./internal/store` and confirm database-backed test names run
without skip messages, then run `go test ./...`.

### Compatibility And Security

No production behavior changes. CI credentials are disposable and must not be
printed outside the workflow process environment.

### Risks And Rollback

The store suite truncates shared tables and must not use parallel tests against
one database. Roll back the workflow service/env block if CI isolation fails.

### Manual Acceptance

Not required.

### Completion Criteria

The CI `Test` job runs the real PostgreSQL repository suite.

### Commit

`ci: run PostgreSQL integration tests`

## Task 2: Add a Scriptable Mock Upstream

Status: completed locally on 2026-07-21; pending the next authorized push and
GitHub Actions run.

### Goal

Provide deterministic JSON, SSE, missing/wrong content type, malformed usage,
status, timeout, and disconnect scenarios without external credentials.

### Dependencies

Task 1.

### Files

- Create: `backend/cmd/mock-openai/main.go`, `backend/cmd/mock-openai/main_test.go`
- Create: `deploy/compose.e2e.yaml`
- Modify: `.github/workflows/ci-image.yml`
- Test: mock scenario handler tests
- Migrate: none
- Document: scenario names in this plan

### Implementation

1. Accept scenario selection through a bounded request header used only by the
   E2E account; never reflect arbitrary input into response headers.
2. Implement protocol-correct `/v1/models`, `/v1/chat/completions`, and
   `/v1/responses` fixtures and explicit failure fixtures.
3. Add health and sanitized diagnostic endpoints for CI only.

Implemented scenarios selected by the exact `X-N2API-E2E-Scenario` header:

- `happy`
- `status-401`, `status-403`, `status-429`, `status-500`, `status-503`
- `missing-content-type`, `wrong-content-type`
- `missing-usage`, `malformed-usage`
- `missing-completion`, `status-503-once`
- `timeout-before-headers`, `disconnect-before-headers`,
  `disconnect-after-first-event`

The mock also exposes `GET /healthz`, `GET /__mock/state`, and
`POST /__mock/reset` only inside the E2E network. Diagnostics contain canonical
scenario, method, route, status, count, and timestamp fields; they never retain
headers, query strings, or request bodies. All `/v1/*` routes require the fixed
synthetic E2E Bearer configured through `N2API_MOCK_API_KEY`, proving the stored
API-upstream credential reaches the upstream without retaining it.

### Tests And Verification

Run mock handler tests, build its image target, and exercise every scenario
with `curl` inside the E2E network.

### Compatibility And Security

The mock is not included in release Compose or the production image. Fixtures
contain no real tokens, prompts, or responses.

### Risks And Rollback

Tests can overfit fixtures. Keep raw transport assertions and delete the E2E
Compose service to roll back.

### Manual Acceptance

Not required.

### Completion Criteria

Every required transport failure can be reproduced deterministically.

### Commit

`test(e2e): add mock OpenAI upstream`

## Task 3: Prove the PostgreSQL-backed Happy Path

Status: completed locally on 2026-07-21; pending the next authorized push and
GitHub Actions run.

### Goal

Drive a real N2API process through authentication, policy, pool selection,
upstream streaming, usage, pricing, and Request Log attribution.

### Dependencies

Tasks 1-2 and readiness endpoints from the deployment plan.

### Files

- Create: `backend/e2e/gateway_e2e_test.go`, `backend/e2e/helpers_test.go`
- Modify: `deploy/compose.e2e.yaml`, `.github/workflows/ci-image.yml`
- Test: the new E2E package
- Migrate: none
- Document: `docs/manual.md` only for an operator-run local command

### Implementation

1. Provision an API-upstream account, model, routing pool, pricing, and client
   key through supported admin APIs.
2. Call models, JSON Chat Completions, and streaming Responses.
3. Query PostgreSQL and assert account, key, pool, session, tokens, pricing,
   attempt count, and fallback count.
4. Always remove temporary keys/data and containers.

### Tests And Verification

Run the E2E Compose project from a clean database twice to prove repeatability.

Completed locally with two distinct Compose project names. Both clean runs
passed `TestGatewayPostgresBackedHappyPath`, and each project was removed with
its PostgreSQL volume after completion.

### Compatibility And Security

Existing APIs only. Logs and artifacts redact authorization, cookies, and
request/response bodies.

### Risks And Rollback

Startup races are handled by readiness, not sleeps. Remove the isolated E2E
job to roll back without affecting production.

### Manual Acceptance

Review the sanitized failure artifact once.

### Completion Criteria

One CI command proves the complete happy path and persisted attribution.

### Commit

`test(e2e): prove gateway attribution loop`

## Task 4: Expand Protocol And Client Contracts

Status: completed locally on 2026-07-21; pending the next authorized push and
GitHub Actions run. Protected real-account acceptance remains deferred until
Task 5 verifies sanitized failure artifacts.

### Goal

Cover the recommended error/stream matrix and official SDK behavior.

### Dependencies

Task 3.

### Files

- Modify: `backend/e2e/gateway_e2e_test.go`, mock upstream scenarios
- Create: `tests/contracts/javascript/`, `tests/contracts/python/`
- Modify: `.github/workflows/ci-image.yml`
- Test: Go E2E, OpenAI JS SDK, OpenAI Python SDK
- Migrate: none
- Document: supported contract matrix in `docs/manual.md`

### Implementation

Add independent cases for 401/403/429/5xx, timeout, disconnect, missing
completion, missing/malformed usage, stream-before/after retry boundaries,
single-flight refresh, limits, sticky routing, and fallback. Pin SDK versions
and keep their lockfiles inside their test directories. Add a protected manual
workflow for real OAuth and Codex CLI only after secret redaction is reviewed.

The automated SDK baseline is pinned to OpenAI JavaScript `6.48.0` on Bun
`1.3.14` and OpenAI Python `2.46.0` on Python `3.12.13`. Real OAuth and Codex
CLI remain a clearly manual contract until Task 5 proves that failure artifacts
are safe for protected-account runs.

### Tests And Verification

Run raw HTTP and both SDK suites. Trigger the manual workflow with a dedicated
test account and verify cleanup.

Completed locally with a clean PostgreSQL-backed Compose run covering the raw
HTTP matrix and isolated Compose runs for the pinned JavaScript and Python SDK
contracts. Real OAuth and Codex CLI are not automated yet because their
protected workflow depends on Task 5's artifact-redaction gate.

### Compatibility And Security

No real account is required by PR CI. Protected jobs save no sensitive body.

### Risks And Rollback

SDK drift can create noise; version updates are isolated dependency commits.

### Manual Acceptance

Required only for real OAuth/Codex CLI.

### Completion Criteria

The published compatibility matrix maps every case to an automated or clearly
manual test.

### Commit

`test(e2e): expand gateway protocol contracts`

## Task 5: Upload Sanitized Failure Artifacts

Status: completed locally on 2026-07-21; pending the next authorized push and
GitHub Actions run.

### Goal

Make E2E failures diagnosable without exposing secrets.

### Dependencies

Task 3.

### Files

- Modify: `.github/workflows/ci-image.yml`, E2E diagnostic helpers
- Test: redaction tests and a deliberately failed local run
- Migrate: none
- Document: artifact retention and content in this plan

### Implementation

Collect bounded N2API/PostgreSQL/mock logs, scenario IDs, test reports, and
redacted Request Log rows on failure. Upload with short retention.

The implemented collector converts raw runner and service output into a strict
allowlist artifact. It emits only:

- `manifest.json`: schema version, suite, run ID, generation time, counts, and
  a truncation flag.
- `events.jsonl`: normalized test lifecycle and bounded failure-stage events.
- `services.json`: service name, state, health, and exit code.
- `scenarios.json`: canonical mock scenario, method, route, status, and count.
- `request-logs.jsonl`: request attribution IDs, canonical route/status/error
  fields, latency, usage source, retry counts, and timestamp.
- `safe.marker`: upload gate written only after all output passes the secret
  and exact-canary scan.

Collection is limited to 500 events, 32 scenarios, and 50 Request Log rows.
Each output file is limited to 256 KiB and the complete artifact to 1 MiB.
Artifacts use a run ID and attempt-specific name and are retained for three
days. Raw logs remain under the Actions runner temporary directory and are
never uploaded or printed by failure-handling steps.

Gateway and SDK fixtures preserve database state only after a failed test when
the CI runner explicitly enables the preservation flag. Successful and normal
local runs continue to clean up through the supported admin API, and the
Compose cleanup step always removes containers and volumes.

### Tests And Verification

Unit tests cover allowlist conversion, unknown-field dropping, exact-canary
rejection, sensitive-pattern rejection, and all event/row/file/artifact bounds.
A local deliberately failed gateway run emitted Bearer, Cookie, and request-body
canaries into its raw runner log. The collector produced a gated safe artifact
containing only normalized test-stage events, with zero fixed or dynamic canary
matches, and the isolated Compose project and volume were removed afterward.

The normal PostgreSQL-backed gateway suite and the pinned JavaScript and Python
SDK suites also pass with failure-state preservation enabled, proving the
success cleanup path is unchanged.

### Completion Criteria

Artifacts identify the failing stage and contain no bearer token, cookie,
encrypted credential, prompt, or response body.

Local completion is proven by the sanitizer tests and deliberate failure
drill. Remote completion requires one authorized GitHub Actions run so the
uploaded artifact can be inspected without exposing protected-account data.

### Risks And Rollback

Artifact leakage is blocking. Disable upload immediately if redaction tests
fail.

### Commit

`ci: upload sanitized gateway test diagnostics`
