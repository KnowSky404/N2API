# API Key Log Filter Design

## Goal

Close the API-key management troubleshooting loop by allowing Request Logs to filter exactly by client API key ID and linking API key rows directly to that filtered log view.

## Context

N2API already records `request_logs.client_key_id`, shows API key usage summaries, and exposes Request Logs filters for text, status class, and provider account. API Keys also expose runtime concurrency and rate-window state. The missing workflow is a precise way to inspect requests for one client key without relying on fuzzy text search over the rendered key label.

## Scope

In scope:

- Add `clientKeyId` to `admin.RequestLogFilter`.
- Accept `clientKeyId` on `GET /api/admin/request-logs`.
- Validate `clientKeyId` as a positive integer when present.
- Add `l.client_key_id = $N` to the store filter SQL.
- Add `clientKeyId` filter state to the Request Logs page.
- Initialize `clientKeyId` from URL query params.
- Add a compact `Logs` link in each API Keys table row:
  - `/request-logs?clientKeyId=<key.id>`

Out of scope:

- A client-key dropdown on Request Logs. The direct deep link is the first useful workflow.
- Updating the browser URL when filters change manually.
- Filtering unknown/unassigned logs.
- Pagination, cursoring, or changing the default `limit=50`.
- Any billing, recharge, tenant, or public-user behavior.

## API

`GET /api/admin/request-logs?clientKeyId=12` returns only logs where `request_logs.client_key_id = 12`.

The parameter composes with existing filters:

- `q`
- `statusClass`
- `providerAccountId`
- `limit`

Invalid values return `400 invalid_input`:

- `clientKeyId=0`
- `clientKeyId=-1`
- `clientKeyId=abc`

## UI Behavior

API Keys table:

- The action cell gains a compact `Logs` link before `Revoke`.
- The link is disabled only by absence of the key ID, which should not happen for persisted keys.
- Revoked keys keep the link because historical log inspection remains useful.

Request Logs route:

- On first authenticated render, read `window.location.search`.
- If `clientKeyId` is a positive integer string, set `requestLogs.clientKeyId` to that string.
- `loadRequestLogs()` sends `clientKeyId` when it is not `all`.

## Verification

- Admin service tests verify positive IDs pass through and non-positive IDs return `ErrInvalidInput`.
- HTTP tests verify query-param translation and invalid value handling.
- Store tests verify the filter helper emits `l.client_key_id = $N`.
- Frontend source tests verify API Keys row links to Request Logs by `clientKeyId`.
- Frontend source tests verify Request Logs initializes and sends `clientKeyId`.
- Existing backend and frontend checks remain green.
