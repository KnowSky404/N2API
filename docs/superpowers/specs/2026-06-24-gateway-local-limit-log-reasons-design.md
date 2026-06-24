# Gateway Local Limit Log Reasons Design

## Goal

Make Request Logs identify which local gateway guard rejected a request, while keeping OpenAI-compatible client error responses unchanged.

## Context

N2API already logs authenticated gateway requests after API-key authentication. It also enforces several local guards:

- per-key request rate limit;
- per-key observed-token limit;
- global gateway concurrency;
- per-key concurrency;
- provider-account concurrency.

Those client responses intentionally use OpenAI-compatible `rate_limit_exceeded`. That is fine for clients, but the admin Request Logs need sharper operational detail. When several guards exist, a single logged `rate_limit_exceeded` value is not enough to diagnose whether the bottleneck is a key, the process, or a provider account.

## Scope

In scope:

- Keep external response error code `rate_limit_exceeded`.
- Store more specific internal request-log errors for local gateway rejections.
- Keep upstream 429 logging as `upstream_rate_limited`.
- Show the existing formatted error labels in Request Logs; no new UI layout is needed.
- Document the distinction between client response code and admin log error.

Out of scope:

- Changing HTTP status codes.
- Changing OpenAI-compatible error payloads.
- Adding new database columns.
- Adding charts or filters.

## Backend Behavior

The proxy keeps using `writeOpenAIError(..., "rate_limit_exceeded", ...)` for local 429 responses. Only the deferred `RequestLog.Error` value changes:

- request minute guard: `api_key_request_rate_limited`
- token minute guard: `api_key_token_rate_limited`
- global gateway concurrency: `gateway_concurrency_limited`
- per-key concurrency: `api_key_concurrency_limited`
- provider-account concurrency: `provider_account_concurrency_limited`

Other gateway errors keep their current logged values.

## Frontend Behavior

The Request Logs page already formats underscore-separated error codes for scanning. The new values should render as readable labels automatically, for example `Api Key Request Rate Limited`.

## Verification

Required gates:

- gateway tests prove local limit responses still contain `rate_limit_exceeded`;
- gateway tests prove request logs store the precise internal error values;
- frontend source test proves Request Logs still uses `errorLabel(log.error)`;
- documentation test proves README and deploy notes explain precise local limit log errors;
- `go test ./internal/gateway -run 'Logs.*Limit|RateLimit|Concurrency'`;
- `bun test src/routes/navigation.test.mjs`;
- `bun run check`;
- `bun run build`.
