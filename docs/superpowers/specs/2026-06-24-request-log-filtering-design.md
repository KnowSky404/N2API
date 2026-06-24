# Request Log Filtering Design

## Goal

Make Request Logs useful as an operational troubleshooting surface by allowing the admin to query recent gateway logs by keyword and status class.

## Scope

In scope:

- Add server-side filters to `GET /api/admin/request-logs`.
- Support a trimmed free-text query across request ID, client key name/prefix, provider/account attribution, model, session, route, method, status code, and gateway error code.
- Support status class filters:
  - `all`
  - `success` for `2xx` and `3xx`
  - `client_error` for `4xx`
  - `server_error` for `5xx+`
- Add compact controls to the Request Logs page.
- Keep existing `limit` behavior: default `50`, max `200`.

Out of scope:

- Pagination, cursors, exports, log retention, or background indexing.
- Changing gateway response error bodies.
- Adding tenant or billing reporting behavior.

## Architecture

`admin.RequestLogFilter` is the contract between HTTP, admin service, and store. The service normalizes and validates filter input before the store builds a parameterized SQL query. The frontend stores filter state next to `requestLogs.items` and submits filters through query params.

## Validation

- Empty query is ignored.
- Query length over 200 characters returns `admin.ErrInvalidInput`.
- Empty status class is normalized to `all`.
- Unknown status class returns `admin.ErrInvalidInput`.
- Invalid `limit` syntax remains HTTP `400 bad_request`.

## Verification

- Admin service unit tests cover limit clamping, query trimming, status class normalization, and invalid filters.
- HTTP tests cover query-param translation and invalid filters.
- Store source tests cover parameterized `ILIKE` search and status class SQL.
- Frontend source tests cover filter controls and request URL params.
- Run backend targeted tests, full backend tests, frontend source tests, `bun run check`, `bun run build`, and `git diff --check`.
