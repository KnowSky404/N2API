# Request Log Model And Session Filter Design

## Goal

Add exact Request Logs filters for `model` and sticky `sessionId`, then link Gateway usage rows for top models and sessions to the filtered log view.

## Context

N2API already records request model and session attribution, shows 24h usage by model/session on the Gateway page, and supports exact request-log filters for provider account and client API key. The remaining diagnostic gap is that an operator can see a busy model or sticky session in usage summaries but must fall back to fuzzy search to inspect the exact matching requests.

This is the personal-gateway equivalent of a monitoring dashboard drill-down. It does not change usage aggregation, scheduling, sticky-session binding, or gateway behavior.

## Scope

In scope:

- Add optional exact `model` and `sessionId` filters to `GET /api/admin/request-logs`.
- Validate both filters as short, non-empty strings when present.
- Include both filters in frontend Request Logs state and URL initialization.
- Render Request Logs controls for model and session filters.
- Link Gateway 24h **Top models** rows to `/request-logs?model=...`.
- Link Gateway 24h **Top sessions** rows to `/request-logs?sessionId=...`, except the `none` usage bucket should not create a session filter link.
- Document the monitoring drill-down.

Out of scope:

- Changing usage summary grouping.
- Adding new database indexes.
- Adding multi-select filters.
- Changing sticky-session routing or persistence.

## Backend Behavior

`admin.RequestLogFilter` gains:

- `Model string`
- `SessionID string`

The admin service rejects filters longer than 100 characters after trimming. Empty strings mean no exact filter.

The SQL repository adds exact predicates:

- `l.model = $n`
- `l.session_id = $n`

These predicates combine with existing status, provider-account, client-key, and text query filters.

## Frontend Behavior

Request Logs state gains `model` and `sessionId` string fields.

The Request Logs page:

- reads `model` and `sessionId` from the URL;
- shows compact text inputs named **Model filter** and **Session filter**;
- includes both values when loading logs;
- clears both values from **Reset filters**.

The Gateway usage table wraps row labels in links only for supported drill-down groups:

- `Top models` -> `model=<row.id>`
- `Top sessions` -> `sessionId=<row.id>` when `row.id !== 'none'`

Provider account and client key drill-down already exist elsewhere, so this slice avoids duplicating those links in the Gateway usage table.

## Verification

Required gates:

- Admin service test proves model/session filters are passed and invalid lengths are rejected.
- HTTP test proves request-log endpoint parses model/session filters.
- Store test proves SQL has exact `l.model` and `l.session_id` predicates.
- Frontend source tests prove Request Logs state, URL initialization, controls, and Gateway usage links exist.
- Documentation test proves README/deploy mention usage row drill-down to model/session Request Logs.
