# Gateway Usage Log Drilldown Design

## Goal

Make every Gateway 24h usage dimension drill down into exact Request Logs filters: model, session, provider account, and client API key.

## Context

N2API already records request logs with `model`, `session_id`, `provider_account_id`, and `client_key_id`. It also exposes usage summaries for the same four dimensions on the Gateway page. Model and session rows already link to filtered logs, while provider-account and client-key rows still require navigating to separate pages or relying on text search.

sub2api-style operations are strongest when monitoring rows lead directly to the requests behind them. This slice completes that monitoring-to-diagnostics loop without changing accounting, scheduling, or gateway routing.

## Scope

In scope:

- Add an API key exact-filter control to the Request Logs page.
- Load API keys on Request Logs when needed for the dropdown.
- Keep existing `clientKeyId` URL initialization and request parameter behavior.
- Extend Gateway usage row links:
  - **Top models** -> `model`
  - **Top sessions** -> `sessionId`
  - **Top provider accounts** -> `providerAccountId`
  - **Top client keys** -> `clientKeyId`
- Skip links for unknown/unassigned/no-session buckets.
- Document that Gateway 24h usage rows drill down to exact Request Logs filters.

Out of scope:

- New backend filtering fields.
- Changing usage summary identifiers.
- Adding multi-select filters.
- Linking from the Request Logs usage summary table in this slice.

## Frontend Behavior

Request Logs:

- The filter bar includes an **API key** dropdown.
- The dropdown has `All API keys`, then active and revoked key labels from `apiKeys.items`.
- If a URL contains `clientKeyId=<positive id>`, the existing URL initialization keeps that value.
- `loadRequestLogs()` already sends `clientKeyId`; this slice only makes it user-selectable in the Request Logs UI.

Gateway:

- Usage rows render as links when the section and row id can map to an exact filter.
- Provider account usage row IDs are in the existing `provider/id` shape. Only positive numeric IDs map to `providerAccountId`.
- Client key usage row IDs are numeric strings when known. Only positive numeric IDs map to `clientKeyId`.
- Unknown buckets stay plain text.

## Verification

Required gates:

- Frontend source tests prove Request Logs imports/loads API keys and renders an API key dropdown bound to `requestLogs.clientKeyId`.
- Frontend source tests prove Gateway usage links include `providerAccountId` and `clientKeyId`.
- Documentation test proves README/deploy mention 24h usage drill-down for all four dimensions.
- `bun test src/routes/navigation.test.mjs`
- `bun run check`
- `bun run build`
