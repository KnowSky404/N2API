# Token Usage Accounting Design

## Summary

N2API should record token usage and estimated USD cost for each successful OpenAI-compatible gateway request. The feature is for personal observability and cost awareness, not platform billing, user balances, recharge flows, invoices, or merchant accounting.

The first implementation should extend the existing request log path. Each request log records the client API key, upstream OAuth account, model, token counts, usage source, pricing snapshot, and estimated cost at the time the response was handled. Admin summary views then aggregate those durable request-log facts by API key, upstream account, model, and time range.

## Goals

- Record token usage for supported `/v1/chat/completions` and `/v1/responses` requests when upstream responses expose usage metadata.
- Preserve end-to-end response streaming behavior.
- Attribute each measured request to both the local client API key and the upstream OAuth account used for the request.
- Estimate USD cost using OpenAI-style input, cached-input, and output token pricing.
- Keep historical estimated costs stable by storing the pricing snapshot used for each request.
- Allow the admin to inspect total usage, recent usage, per-API-key usage, per-account usage, per-model usage, and per-request usage.
- Keep the pricing table editable because OpenAI prices and model names change over time.
- Add focused backend tests for usage parsing, cost calculation, persistence, and summary queries.

## Non-Goals

- No public registration or end-user account system.
- No platform billing, recharge, quotas, balances, invoices, payments, sponsor flows, or merchant accounting.
- No hard dependency on OpenAI organization usage APIs.
- No attempt to make N2API the billing source of truth for OpenAI accounts.
- No automatic historical repricing in the first implementation.
- No per-client enforcement or spend limits in the first implementation.
- No token estimation by tokenizer when upstream usage is missing.
- No Redis or background worker requirement.

## Current Baseline

The gateway already authenticates local client API keys, selects an OpenAI/Codex OAuth account from the account pool, proxies a small supported route set, preserves streaming, and writes `request_logs`.

`request_logs` currently stores request id, local client key id, provider, route, method, status code, latency, error, and created time. It does not store upstream account id, model, token usage, or cost.

The gateway currently copies upstream response bodies directly to the client. Usage accounting therefore needs a small observer around the response copy path. That observer must not buffer full streaming responses or delay stream chunks.

## OpenAI Usage Mapping

Use upstream response `usage` fields as the source of truth.

For Chat Completions responses:

- `usage.prompt_tokens` maps to `input_tokens`.
- `usage.completion_tokens` maps to `output_tokens`.
- `usage.total_tokens` maps to `total_tokens`.
- `usage.prompt_tokens_details.cached_tokens` maps to `cached_input_tokens`.
- `usage.completion_tokens_details.reasoning_tokens` maps to `reasoning_tokens`.

For Responses API responses:

- `usage.input_tokens` maps to `input_tokens`.
- `usage.output_tokens` maps to `output_tokens`.
- `usage.total_tokens` maps to `total_tokens`.
- `usage.input_token_details.cached_tokens` maps to `cached_input_tokens`.
- `usage.output_token_details.reasoning_tokens` maps to `reasoning_tokens`.

Reasoning tokens are recorded as a breakdown only. They should not be charged a second time because they are expected to be included in output or completion token totals for billing and context accounting.

If usage metadata is absent or cannot be parsed, N2API still writes the request log with zero token counts and an explicit `usage_source`.

## Data Model

Extend `request_logs` instead of introducing a separate ledger table in the first implementation:

- `oauth_account_id BIGINT REFERENCES oauth_accounts(id) ON DELETE SET NULL`
- `model TEXT NOT NULL DEFAULT ''`
- `input_tokens INTEGER NOT NULL DEFAULT 0`
- `output_tokens INTEGER NOT NULL DEFAULT 0`
- `total_tokens INTEGER NOT NULL DEFAULT 0`
- `cached_input_tokens INTEGER NOT NULL DEFAULT 0`
- `reasoning_tokens INTEGER NOT NULL DEFAULT 0`
- `estimated_cost_microusd BIGINT NOT NULL DEFAULT 0`
- `pricing_snapshot JSONB NOT NULL DEFAULT '{}'`
- `usage_source TEXT NOT NULL DEFAULT 'missing'`

Add indexes for summary queries:

- `(created_at DESC)` remains.
- `(client_key_id, created_at DESC)` for per-key usage.
- `(oauth_account_id, created_at DESC)` for per-account usage.
- `(model, created_at DESC)` for per-model usage.

Use integer micro-USD for estimated cost. One USD is `1_000_000` micro-USD. This avoids floating-point drift while keeping sub-cent estimates.

## Pricing Settings

Store editable pricing configuration in `settings` under key `usage_pricing`.

The first shape should be:

```json
{
  "version": 1,
  "currency": "USD",
  "unit": "1M_tokens",
  "updatedAt": "2026-06-12T00:00:00Z",
  "models": {
    "gpt-5": {
      "inputMicrousdPerMillion": 0,
      "cachedInputMicrousdPerMillion": 0,
      "outputMicrousdPerMillion": 0
    }
  }
}
```

The implementation may seed OpenAI-style defaults, but the admin must be able to update the table. Default values should be treated as convenience data, not a durable guarantee that prices are current.

When a request is logged, copy the matched price entry into `pricing_snapshot` with:

- matched model name
- configured rate values
- currency
- unit
- pricing config version
- pricing config updated time
- `matched: true` or `matched: false`

If no model price matches, token counts are still stored, `estimated_cost_microusd` is zero, and the snapshot records `matched: false`.

## Cost Calculation

Calculate costs from stored token counts and the matched model price:

- `billable_input_tokens = max(input_tokens - cached_input_tokens, 0)`
- `input_cost = billable_input_tokens * input_rate / 1_000_000`
- `cached_input_cost = cached_input_tokens * cached_input_rate / 1_000_000`
- `output_cost = output_tokens * output_rate / 1_000_000`
- `estimated_cost_microusd = input_cost + cached_input_cost + output_cost`

All intermediate math should use integers and round deterministically. Rounding to nearest micro-USD is acceptable; truncation is also acceptable if documented and covered by tests. The first implementation should choose one behavior and keep it stable.

## Gateway Response Observation

Add a usage observer to the gateway copy path.

For non-stream JSON responses:

- Copy the response body to the client.
- Buffer only up to a bounded response-size limit that is sufficient for normal JSON completions.
- Parse the buffered JSON for `model` and `usage`.
- If the body exceeds the limit, preserve the response and mark usage as `parse_error` or `missing` without failing the request.

For SSE streaming responses:

- Preserve chunked streaming and flush behavior.
- Inspect streamed lines while passing bytes through to the client.
- Parse only final events that contain response-level usage, such as a completed response payload or final chat completion chunk with usage.
- Do not retry, delay, or reassemble the full stream for accounting.

For Codex-routed `/v1/responses` calls, account for usage from the upstream Codex response format when it exposes OpenAI-compatible response usage. If the upstream Codex stream does not expose parseable usage, log `usage_source=missing`.

Parsing failure must not affect the client-visible upstream response. Usage accounting is diagnostics and estimation, not part of request success.

## Admin API

Add protected endpoints:

- `GET /api/admin/usage-summary?range=24h|7d|30d&groupBy=client_key|oauth_account|model`
  - Returns totals for requests, input tokens, cached input tokens, output tokens, total tokens, reasoning tokens, estimated cost, and grouped rows.
- `GET /api/admin/usage-pricing`
  - Returns the current editable pricing table.
- `PUT /api/admin/usage-pricing`
  - Validates and stores the pricing table.

Extend `GET /api/admin/request-logs` responses with:

- upstream account display label when available
- model
- input tokens
- output tokens
- cached input tokens
- reasoning tokens
- total tokens
- estimated cost
- usage source
- pricing matched flag

Validation rules:

- Unknown `range` or `groupBy` returns `400 bad_request`.
- Pricing updates must reject negative rates, empty model names, invalid unit, and unsupported currency.
- Missing pricing settings should fall back to a default empty or seeded table, not fail request logging.

## Admin UI

Keep the UI as an operational dashboard. The first implementation should extend the Request logs area instead of introducing a full billing section.

Request logs page additions:

- Summary cards for 24 hours, 7 days, and 30 days.
- A grouped usage table, defaulting to API key grouping.
- Optional group selector for API key, upstream account, or model.
- Request log table columns for model, tokens, estimated cost, and usage source.

Pricing settings:

- A compact admin-editable pricing table for model name, input rate, cached input rate, and output rate.
- Show rates as USD per 1M tokens while storing micro-USD per million in the backend.
- Make unknown-model behavior clear in UI by showing unpriced usage instead of hiding it.

Display rules:

- Token counts use tabular numbers.
- Costs display as approximate USD, for example `$0.0123`.
- Rows with missing or parse-error usage should remain visible and marked plainly.
- Unknown pricing should display as unpriced, not as a successful zero-cost request.

## Error Handling

- Usage parsing errors do not change upstream status code or response body.
- Request logging remains best effort. If accounting persistence fails, the gateway should still return the upstream response.
- Summary endpoints fail with `500 internal_error` only when the repository query fails.
- Pricing update failures return `400 invalid_input` for validation errors.
- Unknown models do not fail requests.

## Security and Privacy

- Do not store prompts, response text, tool arguments, or streamed content for usage accounting.
- Do not store cleartext API keys or OAuth tokens.
- Do not log request bodies while parsing usage.
- Keep account attribution to internal numeric ids and display labels already visible to the admin.
- Keep all usage and pricing endpoints behind the existing admin session.

## Testing Strategy

Backend tests should cover:

- request log migration defaults
- chat completions non-stream usage parsing
- responses non-stream usage parsing
- SSE usage parsing without delaying chunks
- streaming parse failure still preserves response bytes
- cached input token cost calculation
- reasoning tokens are not double-charged
- unknown model records tokens with zero estimated cost and `matched=false`
- request logs include selected OAuth account id
- pricing settings validation and persistence
- usage summary queries by client key, OAuth account, and model
- bounded buffering behavior for oversized non-stream responses

Frontend checks should cover:

- formatting token counts and estimated USD
- rendering unpriced or missing usage states
- existing navigation and request-log page checks

Verification commands:

- `GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...` from `backend/`
- `bun run check` from `frontend/`
- `bun run build` from `frontend/`

## Deployment and Operations

The existing Docker Compose deployment remains the target. No new infrastructure is required.

After deployment, existing request logs keep default zero usage fields. New requests start recording usage when the upstream response contains parseable usage metadata.

Because this feature adds columns to `request_logs`, operators should back up PostgreSQL before applying the migration if the instance has important historical logs.

## Acceptance Criteria

- Each measured gateway request records client key, upstream account id, model, usage source, token counts, pricing snapshot, and estimated micro-USD cost.
- Streaming responses still flush to clients while accounting observes usage opportunistically.
- Missing or unparsable usage never breaks a request.
- Admin can view 24-hour, 7-day, and 30-day usage totals.
- Admin can group usage by local API key, upstream OAuth account, and model.
- Admin can view and edit OpenAI-style model pricing.
- Unknown model prices keep token counts visible and mark cost as unpriced.
- Backend and frontend verification commands pass.
