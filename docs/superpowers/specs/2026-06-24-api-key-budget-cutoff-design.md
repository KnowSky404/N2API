# API Key Budget Cutoff Design

## Summary

N2API should support per-client API key usage budgets that can stop a key before it consumes more personal gateway capacity. This brings the useful part of sub2api-style quota distribution into N2API without adding platform billing, recharge balances, public tenants, payment providers, merchant accounting, or sponsor flows.

The first version should enforce request-count and token-count budgets over rolling 24-hour and 30-day windows. It should use durable `request_logs` as the usage source so budget behavior survives backend restarts. USD/cost budgets are intentionally deferred because they depend on editable pricing configuration and are easier to explain after request/token budgets are stable.

## Goals

- Let the admin configure four optional budget limits per client API key:
  - requests in the last 24 hours
  - tokens in the last 24 hours
  - requests in the last 30 days
  - tokens in the last 30 days
- Treat `0` as disabled for each budget field.
- Enforce budgets after API key authentication and before provider account selection.
- Return OpenAI-compatible local `429 rate_limit_exceeded` responses when a key is over budget.
- Store local request-log error reasons that identify which budget blocked the request.
- Show current budget usage and remaining budget on the API Keys page.
- Keep budget settings editable for disabled keys and unavailable only after key revocation.
- Document that budgets are personal operational safeguards, not billing balances.

## Non-Goals

- No public registration or end-user account system.
- No recharge balance, payment, invoice, merchant accounting, sponsor, reseller, or multi-tenant SaaS behavior.
- No USD spend budget in the first version.
- No tokenizer-based preflight estimate for a request that has not completed yet.
- No Redis, queue, or distributed counter requirement.
- No attempt to prevent small overshoot from a request that starts below token budget and returns enough tokens to cross the budget.
- No hard deletion or automatic revocation of keys when a budget is exceeded.

## Current Baseline

N2API already has:

- `client_api_keys` with name, revoked state, disabled state, model policy, per-minute request/token limits, and active runtime limiter visibility.
- `request_logs` with client key attribution, provider account attribution, model, token usage, estimated cost, and local error code fields.
- Admin usage summary queries by client key, provider account, model, and session.
- Local gateway rejections for per-minute limits, gateway concurrency, API key concurrency, and provider account concurrency.
- API Keys page controls for name, model policy, per-key request/token minute limits, status filtering, and enable/disable.

The budget cutoff should reuse these foundations instead of adding a separate accounting ledger.

## Data Model

Add nullable-free integer columns to `client_api_keys`, all defaulting to `0`:

- `request_budget_24h INTEGER NOT NULL DEFAULT 0`
- `token_budget_24h INTEGER NOT NULL DEFAULT 0`
- `request_budget_30d INTEGER NOT NULL DEFAULT 0`
- `token_budget_30d INTEGER NOT NULL DEFAULT 0`

These values are operational caps. A value of `0` means no cap for that window and metric.

The API key admin DTO should expose matching camel-case fields:

- `requestBudget24h`
- `tokenBudget24h`
- `requestBudget30d`
- `tokenBudget30d`

The API key list response should also include current budget state derived from request logs:

- `requestsUsed24h`
- `tokensUsed24h`
- `requestsRemaining24h`
- `tokensRemaining24h`
- `requestsUsed30d`
- `tokensUsed30d`
- `requestsRemaining30d`
- `tokensRemaining30d`
- `requestBudgetExceeded`
- `tokenBudgetExceeded`

Remaining values are `null` when the corresponding budget is disabled. This avoids pretending that an uncapped key has a numeric remaining allowance.

## Budget Usage Query

The store should aggregate budget usage from `request_logs` by client key id and time window:

- request usage counts rows with `created_at >= since`.
- token usage sums `total_tokens` for rows with `created_at >= since`.
- local budget-rejection rows count as requests but normally add zero tokens.

This intentionally means a request that is accepted below the token budget can push the key over the token budget after completion. The next request is rejected until enough usage leaves the rolling window. This is deterministic, simple, and does not require guessing output tokens.

For first-version performance, query only the relevant key during gateway enforcement and query all visible keys for the admin list. Current N2API is a personal single-node deployment, and `request_logs` already has time and client-key indexes for usage summaries. Materialized usage tables and Redis-backed rolling counters are outside this design and should not be introduced for this slice.

## Enforcement Flow

Gateway request flow should become:

1. Authenticate API key.
2. Reject revoked or disabled keys as today.
3. Load budget usage for the authenticated key.
4. If any enabled request budget is exhausted, reject with HTTP `429`.
5. If any enabled token budget is exhausted, reject with HTTP `429`.
6. Continue with model policy, per-minute limits, concurrency, and provider account selection.

Budget rejection should use OpenAI-compatible response shape:

- status: `429`
- error code returned to client: `rate_limit_exceeded`
- message: `api key budget exceeded`

The durable request log should record more specific local error codes:

- `api_key_request_budget_exceeded`
- `api_key_token_budget_exceeded`

If both request and token budgets are exhausted, prefer `api_key_request_budget_exceeded` because request-count budget is checked first and is easier to explain from zero-token rejection rows.

## Admin API

Add a protected endpoint:

- `PUT /api/admin/keys/{id}/budgets`

Request body:

```json
{
  "requestBudget24h": 1000,
  "tokenBudget24h": 1000000,
  "requestBudget30d": 20000,
  "tokenBudget30d": 20000000
}
```

Validation:

- Missing fields are treated as `0` for this endpoint.
- Values must be non-negative integers.
- Revoked keys return `404 not_found`.
- Unknown ids return `404 not_found`.
- Validation failures return `400 invalid_input`.

The response returns the updated `key` object with budget settings. The admin list endpoint should include the derived budget state for each key.

## Admin UI

The API Keys page should add a compact budget controls group beside existing key limits:

- 24h requests
- 24h tokens
- 30d requests
- 30d tokens

Each field uses a non-negative integer input; `0` means uncapped. The save action should be explicit, similar to existing key limits.

The key runtime/status area should show budget usage when budgets are enabled:

- `24h requests 42 / 1000`
- `24h tokens 120K / 1M`
- `30d requests 2.1K / 20K`
- `30d tokens 8.4M / 20M`

When an enabled budget is exhausted, show a concise amber/red state such as `Request budget exceeded` or `Token budget exceeded`. This should be included in local key search text so filtering/search can find budget-blocked keys.

Budget controls remain editable while a key is disabled. They become disabled after revocation, matching existing key configuration behavior.

## Request Logs and Diagnostics

Request Logs should continue showing local gateway rejection reasons. Add documentation and tests that local budget rejections are stored as:

- `api_key_request_budget_exceeded`
- `api_key_token_budget_exceeded`

Client-visible responses remain OpenAI-compatible `rate_limit_exceeded`. Admin diagnostics use the more precise stored error code.

## Error Handling

- If the budget usage query fails, the gateway should fail closed with `500 internal_error`, because accepting traffic after losing the durable budget source could overspend the configured personal cap.
- If request-log persistence fails after a budget rejection, still return the rejection to the client; request logging remains best effort.
- If a key has no request logs, budget usage is zero.
- Negative admin budget inputs are rejected.
- Overflow-like values that do not fit Go `int` or PostgreSQL `INTEGER` are rejected as invalid input.

## Security and Privacy

- Do not store prompts, responses, tool arguments, or request bodies for budget enforcement.
- Do not store cleartext API keys or provider credentials.
- Keep budget settings and usage visible only behind the existing admin session.
- Keep client-facing errors generic enough to preserve OpenAI-compatible behavior while admin logs keep precise local reasons.

## Testing Strategy

Backend tests should cover:

- migration embeds the four budget columns.
- service validation rejects negative budget values.
- repository updates budget fields and refuses revoked keys.
- admin HTTP endpoint parses and maps success, bad input, and not found.
- gateway rejects when 24h request budget is exhausted.
- gateway rejects when 30d request budget is exhausted.
- gateway rejects when 24h token budget is exhausted from prior logs.
- gateway allows a request below budget and records its eventual token usage normally.
- request logs store precise budget rejection error codes.
- API key list response includes budget usage and remaining values.

Frontend tests should cover:

- API Keys page renders budget inputs and save action.
- API Keys page shows budget usage text and exceeded labels.
- shared admin state calls `PUT /api/admin/keys/{id}/budgets`.
- budget status is included in local search text.

Verification commands:

- `GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...` from `backend/`
- `bun test src/routes/navigation.test.mjs` from `frontend/`
- `bun run check` from `frontend/`
- `bun run build` from `frontend/`

## Deployment and Operations

The feature adds a PostgreSQL migration only. Docker Compose remains the deployment target, and no Redis or new service is required.

After upgrade, existing API keys have all budgets set to `0`, so behavior remains unchanged until the admin configures a budget. Existing request logs immediately count toward a newly configured rolling budget because the budget source is durable historical usage.
