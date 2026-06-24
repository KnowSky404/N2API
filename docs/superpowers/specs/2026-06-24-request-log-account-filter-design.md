# Request Log Account Filter Design

## Goal

Close the account-management troubleshooting loop by allowing Request Logs to filter exactly by provider account ID.

## Context

N2API already records `request_logs.provider_account_id`, exposes provider account attribution in each log row, indexes provider-account log lookups, and shows provider account usage summaries. The current Request Logs page supports keyword and status-class filtering, but an operator cannot ask for only the requests handled by one selected account without relying on fuzzy search text.

## Scope

In scope:

- Add `providerAccountId` to `admin.RequestLogFilter`.
- Accept `providerAccountId` on `GET /api/admin/request-logs`.
- Validate `providerAccountId` as a positive integer when present.
- Add `l.provider_account_id = $N` to the store filter SQL.
- Add a Provider account filter dropdown on the Request Logs page.
- Populate the dropdown from existing `providerAccounts.items`, with `All provider accounts` as the default.
- Load provider accounts when the Request Logs route is authenticated and the list is empty, so the filter is available without visiting the Providers page first.

Out of scope:

- Deep links from provider rows to request logs.
- Filtering unassigned logs.
- Pagination, cursoring, or changing the default `limit=50`.
- Changing usage summary grouping.

## API

`GET /api/admin/request-logs?providerAccountId=7` returns only logs where `request_logs.provider_account_id = 7`.

The parameter composes with existing filters:

- `q`
- `statusClass`
- `limit`

Invalid values return `400 invalid_input`:

- `providerAccountId=0`
- `providerAccountId=-1`
- `providerAccountId=abc`

## UI

The Request Logs toolbar adds a Provider account `<select>` next to Search and Status.

Values:

- `all`
- one option per `providerAccounts.items`, using `providerAccountName`-style display data from the provider account list.

The state value is stored as a string (`'all'` or an ID string) to match the Svelte `<select>` binding and converted to a query param only when not `all`.

## Verification

- Admin service tests verify positive IDs pass through and non-positive IDs return `ErrInvalidInput`.
- HTTP tests verify query-param translation and invalid value handling.
- Store tests verify the filter helper emits `l.provider_account_id = $N`.
- Frontend source tests verify the Request Logs route loads provider accounts, binds the select, and sends `providerAccountId`.
- Existing backend and frontend checks remain green.
