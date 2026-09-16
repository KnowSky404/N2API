# N2API Compatibility, Reliability, Diagnostics, and Maintainability Iteration

## Goal

Complete one repository-scoped iteration that improves endpoint-aware account
scheduling, Codex OAuth Responses compatibility, model-catalog cache lifecycle,
request diagnostics, and core-module maintainability while preserving the V1
personal self-hosted scope and the existing Go, PostgreSQL, Bun, SvelteKit,
Tailwind, and Docker Compose baseline.

This file is the single progress and acceptance record for the iteration.

## Non-goals and constraints

- No billing, recharge, merchant accounting, payment providers, sponsor flows,
  public registration, broad SaaS tenancy, or required Redis.
- No general Chat Completions/Responses bidirectional conversion and no broad
  provider-protocol adapter rewrite.
- No production deployment, real provider account, real email, remote push, PR,
  or GitHub mutation.
- Preserve routing pools, fallback chains, health/sticky/stateful routing,
  Responses affinity, limits, budgets, request logs, events, and alerts.
- Keep OAuth and refresh credentials encrypted at rest and out of cache keys,
  logs, diagnostics, and metrics.
- Preserve existing `gatewayAttemptCount`, `gatewayFallbackCount`, and latency
  semantics unless a new field explicitly documents a narrower meaning.

## Baseline and confirmed starting point

Baseline HEAD: `33b8d686` (`fix: scope oauth model cache by account`). The main
worktree is authoritative; the old upstream-model-sync handoff was already
integrated into this HEAD. The stale retry worktree is not used.

Baseline command: `make test`

Baseline result: passed Go tests, frontend `svelte-check`, 222 frontend tests,
and the SvelteKit production build on 2026-09-14.

Confirmed capabilities and issues:

- API-upstream model synchronization and OAuth catalog synchronization already
  exist, but manual OAuth catalog sync shares the ordinary TTL cache path.
- OAuth Codex Responses requests are normalized to upstream SSE and successful
  responses are currently treated as streaming even when the client requested
  non-streaming output.
- Selection is model-aware and pool-aware but has no explicit endpoint
  capability dimension.
- Request logs retain aggregate attempt/fallback counts and full-request
  latency, but not bounded attempt records or response-stream phase timings.
- `proxy.go`, `provider/service.go`, and `admin-state.svelte.js` remain large
  responsibility mixtures suitable for incremental same-package/domain splits.
- OpenAI JavaScript and Python contract fixtures are pinned in
  `tests/contracts`; the dependency/toolchain matrix must be verified before any
  candidate upgrade is retained.

## Work items

| ID | Work item | Status | Dependencies |
| --- | --- | --- | --- |
| A | Endpoint-aware scheduling and stable endpoint diagnostics | complete | baseline |
| B | OAuth Responses non-stream aggregation and SSE terminal handling | complete | baseline |
| C | Model-catalog cache lifecycle, forced refresh, freshness, and invalidation | complete | baseline |
| D | Bounded request-attempt and stream diagnostics | complete | A, B |
| E | Same-package gateway/provider/frontend responsibility splits | complete | A-D behavior tests |
| F | Dependency inventory, SDK contract matrix, consistency checks, and docs | complete | baseline, A-D |
| G | Full regression, browser verification, migrations, and local Compose refresh | complete | A-F |

## Acceptance criteria

### A. Endpoint-aware scheduling

- Candidate filtering receives the request endpoint and applies model, API-key,
  pool/fallback, health, concurrency, sticky/session, and Responses-affinity
  constraints together.
- Explicitly unsupported account/endpoint combinations are rejected before an
  upstream request; unknown capability metadata remains eligible and is not
  presented as verified support.
- API-upstream accounts retain a compatible default when capability metadata is
  absent; existing accounts are not collectively disabled.
- No legal candidate produces a stable endpoint-aware diagnostic without
  bypassing the configured pool, moving stateful requests, or changing model.
- `/v1/models` remains the standard public contract; capability detail is
  available through admin/docs only.

### B. OAuth Responses compatibility

- OAuth Responses upstream calls continue to use SSE, while client
  `stream:true` remains end-to-end streaming and client `stream:false` or an
  omitted flag receives one complete JSON Response object.
- Non-stream aggregation preserves response IDs, output text, tool/function
  calls, structured output, usage, status, and retained response fields.
- `response.completed`, `response.failed`, `response.incomplete`, `error`,
  malformed events, early EOF, size limits, idle limits, and cancellation have
  distinct tested outcomes; HTTP 200 or EOF alone is never treated as success.
- The parser handles chunk boundaries, CRLF/LF, comments, and multi-line data;
  no retry is attempted after upstream execution or client response commit.

### C. Model-catalog lifecycle

- Ordinary reads may use a valid cache; forced catalog refresh bypasses a
  completed cache and performs an upstream fetch. Credential refresh remains a
  separate operation.
- Same account/config-generation refreshes coalesce in flight, admission is
  bounded, account permissions/cache entries remain isolated, and late old
  requests cannot overwrite newer auth/config state.
- Freshness exposes last attempt, last success, last failure, source/cache-hit,
  upstream-fetch and local-apply timing; unknown history stays unknown.
- Failed refreshes retain prior models and do not claim success. Manual enabled,
  disabled, override, and default policy remains unchanged.
- Admin UI distinguishes credential refresh from catalog refresh and protects
  loading, duplicate, error, stale, and cancellation states.

### D. Request diagnostics

- Logs expose a bounded, redacted attempt timeline with order, type, account,
  pool, start/end/duration, HTTP status, stable error, fallback reason, and
  upstream request ID.
- Diagnostics distinguish selection, concurrency rejection, HTTP, auth-refresh
  retry, and cross-account fallback while preserving legacy aggregate counters.
- Response timing fields distinguish header wait, first useful output, and stream
  finish. Comments, heartbeats, and creation-only events do not count as useful
  output; absent observations remain null/unknown.
- Model errors, transport errors, client cancellation, truncation, terminal
  failure/incomplete, and HTTP 200 without generation success remain distinct.
- Metadata is bounded and excludes prompts, outputs, tool parameters, tokens,
  raw sensitive headers/errors, per-chunk database writes, and high-cardinality
  metric labels. Logging/observability failure cannot change gateway, budget,
  usage, or slot behavior.
- Request-log UI renders a timeline and phase details, and older rows without
  fields say `未记录`.

### E. Maintainability

- Behavior tests land before structure-only edits.
- Gateway normalization/endpoint/retry/response/limits/observation, provider
  accounts/OAuth/catalog/scheduling, and frontend provider/request-log domains
  are split into focused files/modules without behavior, schema, or default
  changes in structural commits.
- New interfaces remain small and same-package/domain boundaries are preferred.

### F. Dependencies and SDK contracts

- Real dependencies, lockfiles, Go/toolchain, Bun, image, CI, and manual matrix
  are inventoried. Candidate versions are tested in focused groups and only
  passing upgrades are kept.
- Official OpenAI JavaScript/Python contract fixtures cover Models, Chat JSON,
  Responses streaming/non-streaming, tools, errors, cancellation, and early
  termination. Support is labeled supported, verified, or unsupported.
- Version consistency checks prevent drift across package manifests, locks,
  Docker/CI, and docs. Functionality, refactor, and dependency commits remain
  separate.

### G. Verification and delivery

- Relevant Go, race, contract, frontend, and browser/mock tests pass; browser
  checks use Browser tooling first or the required Bunx Playwright fallback.
- Migrations are additive and compatible; published migrations are not rewritten.
- `make test` and scoped heavy checks pass before finalization.
- After code changes, run the required non-destructive local Compose refresh:
  builder prune, no-cache build, force recreation, container/probe/admin and
  simulated-gateway verification, then builder prune again. Preserve the
  PostgreSQL volume and report local/mock evidence separately from production.
- Each coherent work item is committed atomically with a Conventional Commits
  message. No push or deployment is performed.

## Progress log

| Date | Item | Evidence / commit |
| --- | --- | --- |
| 2026-09-14 | Baseline | `make test` passed at HEAD `33b8d686`; no worktree changes before this plan. |
| 2026-09-14 | Context7 contract lookup | Official OpenAI API Responses streaming events and `openai-python` response stream union consulted; implementation must preserve terminal response objects and typed event categories. |
| 2026-09-14 | A complete | `aa47f96` (`feat: route accounts by endpoint capability`); endpoint-aware global/pool/fallback/affinity selection, stable endpoint diagnostics, metadata persistence, and HTTP preview coverage. `make test`, `make test-go-quality`, and focused endpoint tests passed. |
| 2026-09-14 | B complete | `4c907c4` (`feat: support OAuth Responses non-streaming clients`); bounded SSE parser, terminal-event validation, complete Response aggregation, raw streaming terminal tracking, and cancellation/truncation/error coverage passed targeted and race tests. |
| 2026-09-14 | C complete | `71f754d` (`feat: add forced OAuth catalog refresh`); forced refresh endpoint/UI, account-generation invalidation, bounded/coalesced catalog fetches, freshness status, stale-response protection, failure retention, and duplicate/cancellation coverage passed targeted backend/frontend checks. |
| 2026-09-14 | D complete | `529355a` (`feat: add bounded request diagnostics`); bounded/redacted attempt timelines, fallback/auth/transport classifications, request-relative response phases, HTTP-200 model/missing-generation diagnostics, PostgreSQL migration/round-trip/export coverage, and request-log detail UI. `make test`, `make test-go-quality`, and `make test-critical-race` passed; `51742ab` extends the existing process-lifecycle race-test context budget exposed by the full race run. |
| 2026-09-14 | E complete | `2bc7fa6` and `9f60ac4` split gateway/provider/frontend responsibilities into focused same-package/domain files after behavior coverage; managed unit tests and static checks passed with no schema or default changes. |
| 2026-09-14 | F complete | `d64fabe` adds the dependency/toolchain contract matrix and pinned consistency checks; `1cc6abd` expands the local mock contract fixtures. `bash -n dev/ci/verify-pinned-dependencies.sh && bash dev/ci/verify-pinned-dependencies.sh` passed, and `DOCKER_CONFIG=/tmp/n2api-docker-config make test-contracts` passed official OpenAI JavaScript 1/1 and Python 1/1 fixtures. |
| 2026-09-14 | G complete | `make test`, `make test-go-quality`, `make test-critical-race`, `DOCKER_CONFIG=/tmp/n2api-docker-config make test-e2e`, `make test-request-log-profile`, `make test-control-connections`, and `make test-postgres-faults` passed. Bunx Playwright `1.61.1` fallback passed the mobile unauthenticated-shell check. Local Compose was rebuilt with no cache, force recreated, `/livez` and `/readyz` returned 200, all services were healthy, and `deploy_n2api-postgres` was preserved. |

## Blockers, skips, and follow-ups

- Browser tooling was unavailable in this session; the required Bunx Playwright
  fallback passed with `PLAYWRIGHT_BROWSERS_PATH=/tmp/n2api-playwright-browsers`.
  The first default Bun temp path returned `EROFS`; task-scoped Bun install/cache
  paths under `/tmp` were used for the successful rerun. Browser evidence is
  local and unauthenticated.
- The host has no Docker Buildx plugin and its default Docker config is
  read-only. Compose verification used the writable task-scoped
  `DOCKER_CONFIG=/tmp/n2api-docker-config`; this is an environment workaround,
  not a repository change.
- Restore-fixture tests remain intentionally skipped unless their explicit
  isolated-database environment variables are enabled. No production, real
  provider, OAuth, email, push, or GitHub evidence was claimed.
