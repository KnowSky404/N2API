# Provider Account Log Deeplink Design

## Goal

Let the admin move directly from a provider account row to Request Logs filtered for that account.

## Context

N2API now supports exact Request Logs filtering by `providerAccountId`. The Provider accounts table already exposes operational actions for test, history, pause, refresh, status reset, reauthorize, and disconnect. The missing workflow is the direct troubleshooting path from an account row to the gateway requests that used that account.

## Scope

In scope:

- Add a compact `Logs` action in each provider account row.
- Link to `/request-logs?providerAccountId=<account.id>`.
- Teach Request Logs to initialize `requestLogs.providerAccountId`, `requestLogs.query`, and `requestLogs.statusClass` from URL query params.
- Auto-load Request Logs once authenticated, after URL params are applied.
- Keep the existing manual Refresh button.

Out of scope:

- Updating the browser URL when the Request Logs filters are changed manually.
- Deep-linking unassigned logs.
- Adding pagination or changing the default limit.
- Adding a backend endpoint beyond the existing `providerAccountId` filter.

## UI Behavior

Provider account row:

- The action button uses the same compact action style as the other row actions.
- `title` and `aria-label` are `View request logs`.
- The button is an `<a>` link, not a JavaScript navigation action.

Request Logs route:

- On first authenticated render, read `window.location.search`.
- If `providerAccountId` is a positive integer string, set `requestLogs.providerAccountId` to that string.
- If `q` is present, set `requestLogs.query`.
- If `statusClass` is one of `all`, `success`, `client_error`, `server_error`, set `requestLogs.statusClass`.
- Then load provider accounts if needed and load request logs.

## Verification

- Frontend source tests prove the Providers page contains the account-scoped Request Logs link.
- Frontend source tests prove Request Logs reads `URLSearchParams(window.location.search)`.
- Frontend source tests prove Request Logs auto-loads logs after authenticated URL initialization.
- Existing frontend and backend checks remain green.
