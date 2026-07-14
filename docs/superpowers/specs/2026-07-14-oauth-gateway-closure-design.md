# OAuth Gateway Closure Design

## Summary

N2API must provide a verified personal gateway path from a connected Codex OAuth
account to downstream API keys. A request is not considered closed-loop merely
because the model returns text: authentication, model routing, OAuth refresh,
streaming, usage accounting, pricing, limits, failure state, and request-log
attribution must all agree on the same request.

## Scope

- Codex OAuth provider accounts using the ChatGPT Codex Responses upstream.
- Downstream bearer authentication with N2API client API keys.
- `/v1/models` and `POST /v1/responses`, which are the surfaces required by a
  Codex CLI custom provider using `wire_api = "responses"`.
- Provider-account selection, routing pools, retry-before-stream behavior,
  request and token limits, rolling budgets, pricing snapshots, and logs.

The work remains personal and self-hosted. It does not add public registration,
balances, payments, invoices, or merchant billing.

## Streaming Contract

Codex OAuth Responses requests are always normalized to `stream = true` before
they are sent to the ChatGPT Codex upstream. Successful upstream responses for
that path must therefore be exposed downstream as HTTP SSE even when the
upstream omits `Content-Type: text/event-stream` or labels the body as plain
text.

For successful Codex OAuth Responses requests, N2API must:

- set downstream `Content-Type: text/event-stream`;
- pass each upstream chunk through immediately and flush it;
- preserve SSE bytes without rewriting events;
- parse the final `response.completed.response.usage` object while forwarding;
- record `usage_source = stream` when usage is present;
- keep a successful response successful when usage parsing fails.

Error responses must retain their upstream error content type and must not be
relabeled as SSE solely because the request targeted the Codex endpoint.

## Closed-Loop Invariants

1. A valid active API key authenticates; a missing, invalid, disabled, revoked,
   or deleted key fails before provider selection.
2. The requested model must be globally allowed, visible to the API key, and
   enabled on an eligible account in the key's routing scope.
3. Expiring OAuth credentials refresh once per account under concurrent load,
   and refreshed credentials remain encrypted at rest.
4. Selection respects account health, priority, load, concurrency, sticky
   sessions, routing pools, and explicit fallback chains.
5. Retry and fallback occur only before downstream streaming starts.
6. The request log records the client key, provider account, model, route,
   status, latency, attempts, fallbacks, usage, price snapshot, and estimated
   cost from the same request.
7. Request/token rate limits, concurrency limits, and rolling budgets reject
   locally with OpenAI-compatible errors and retain a precise internal reason.
8. A successful priced response contributes non-zero usage and a matched price
   snapshot when the configured model price is non-zero.

## Acceptance Criteria

- A proxy test reproduces a Codex OAuth upstream that emits SSE without a valid
  SSE content type and proves immediate flush, downstream SSE content type, and
  final usage capture.
- Existing JSON and correctly labeled SSE upstream behavior remains unchanged.
- Backend and frontend verification suites pass.
- The Docker Compose deployment is rebuilt and healthy.
- A real `free0` OAuth account passes account test and forced refresh.
- A temporary downstream API key can list models and complete a real Codex CLI
  request through N2API.
- The real request log contains `usage_source = stream`, positive token counts,
  provider account `free0`, and a pricing snapshot consistent with current
  pricing configuration.
- Temporary verification API keys are physically removed after the test.

