# Gateway E2E Compatibility Plan

Status: planned
Public API changes: none; tests validate existing `/v1/*` contracts
Data migration: none

## Current Baseline

`backend/internal/gateway/proxy_test.go` already covers most gateway branches
with fake services and `httptest` upstreams. Store tests that need PostgreSQL
skip unless `N2API_STORE_TEST_DATABASE_URL` is set, and CI does not set it. A
real Codex OAuth path was manually accepted in July 2026, but no ordinary CI
test proves the full persisted loop. Real-account tests must remain manual.

## Task 1: Run PostgreSQL Integration Tests in CI

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

### Tests And Verification

Run raw HTTP and both SDK suites. Trigger the manual workflow with a dedicated
test account and verify cleanup.

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

### Completion Criteria

Artifacts identify the failing stage and contain no bearer token, cookie,
encrypted credential, prompt, or response body.

### Risks And Rollback

Artifact leakage is blocking. Disable upload immediately if redaction tests
fail.

### Commit

`ci: upload sanitized gateway test diagnostics`
