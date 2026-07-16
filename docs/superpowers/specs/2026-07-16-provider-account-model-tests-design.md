# Provider Account Model Tests Design

## Goal

Let the admin test one or more configured models against one exact provider
account from the Providers page, with progressive per-model results and no
fallback to another account.

## Context

N2API already has account-level probes, account test history, per-account model
configuration, API-upstream model sync, and a production gateway pipeline. The
existing account probe does not prove that every configured model can complete
a request: Codex OAuth uses one fixed probe model, while API-upstream accounts
only fetch `/v1/models`.

This feature is model diagnostics, not another account-health scheduler. It
must use the selected provider account's stored OAuth token or API key and must
not use a process-level `OPENAI_API_KEY`.

## Scope

In scope:

- Add one authenticated admin endpoint that tests one configured model against
  one provider account.
- Persist the latest result on `provider_account_models`.
- Add a Providers-page model-test modal with search, status filters, row
  selection, select-all for the current filtered result set, and progressive
  execution.
- Support testing one model, an arbitrary selection, or all currently filtered
  models.
- Preserve disabled models in the list and allow the admin to test them.
- Use bounded frontend concurrency so results appear as individual requests
  finish.

Out of scope:

- Scheduled per-model tests.
- Per-model historical result tables or retention jobs.
- Automatic model enable, disable, deletion, or routing changes.
- Fallback to another provider account.
- A new API credential or a process-level OpenAI API key.

## UI Behavior

The Providers table keeps its compact account rows. The pinned Actions cell
adds an icon button named `Test models`. It opens a page-level modal following
the existing Providers modal pattern.

The modal contains:

- Account name and account type.
- Search by model name.
- Filters for enabled state and latest test status.
- A compact model table with `Select`, `Model`, `Enabled`, `Last result`,
  `Latency`, and `Checked` columns.
- A checkbox on every row.
- A tri-state header checkbox. It selects or clears only the models in the
  current filtered result set. With no filters, it applies to all configured
  models.
- A `Clear selection` action.
- A `Test selected (N)` action that is disabled when no models are selected.
- A row action for a one-click single-model test that does not modify the batch
  selection.

No model is selected when the modal first opens. Selection survives filtering
and remains after a test run so the admin can retry the same set. The selected
count always reflects all selected models, including models hidden by current
filters.

Batch execution uses at most three concurrent requests. Each selected model
progresses through `queued`, `testing`, and its persisted terminal result.
Closing the modal does not erase persisted results. Reopening it reloads the
account's model state.

## Admin API

Add:

`POST /api/admin/provider-accounts/{id}/model-tests`

Request:

```json
{
  "model": "gpt-5.4-mini"
}
```

The model is required, trimmed, and must exactly match a configured model for
the account. An account or model that does not exist returns `404`. Invalid
input returns `400`. Credential, storage, and internal failures use the
existing provider error mapping.

Successful HTTP handling returns `200` even when the upstream model test
fails. Upstream failure is a diagnostic result, not an admin API transport
failure.

Response:

```json
{
  "result": {
    "accountId": 7,
    "model": "gpt-5.4-mini",
    "status": "passed",
    "errorCode": "",
    "httpStatus": 200,
    "latencyMs": 842,
    "message": "",
    "checkedAt": "2026-07-16T10:00:00Z"
  }
}
```

Result statuses are `passed` and `failed`. `errorCode` distinguishes at least
`unauthorized`, `rate_limited`, `model_not_found`, `upstream_error`,
`network_error`, `timeout`, and `invalid_response`.

## Probe Semantics

Every probe must force the requested provider account. It reuses the existing
provider service path for credential decryption, OAuth refresh, proxy URL, and
fingerprint profile data.

Codex OAuth accounts:

- Send a minimal request for the selected model to the Codex Responses
  endpoint.
- Keep the existing Codex headers and fingerprint behavior.
- Consume the typed SSE stream until `response.completed`.
- Treat an `error` event, malformed terminal data, connection failure, or
  timeout as a failed result. HTTP `2xx` without a completion event is not a
  passed result.

API-upstream accounts:

- Send a minimal non-streaming `/v1/chat/completions` request using the
  account's base URL and encrypted API key.
- Treat a valid `2xx` JSON response as passed.
- This first version proves the Chat Completions path. A configurable probe
  route for Responses-only upstreams is a separate feature.

Each request has a 20-second timeout. Response bodies and error bodies are
bounded before parsing. Tokens, API keys, authorization headers, proxy
credentials, full prompts, and full upstream response bodies are never stored
or logged.

Model diagnostics do not mutate account scheduling state. They do not call
`RecordAccountFailure`, reset status, change priority, or change model enabled
state. Existing account-level `Test account` remains the source of account
health mutations.

## Persistence

Add these columns to `provider_account_models`:

- `last_test_at TIMESTAMPTZ`
- `last_test_status TEXT NOT NULL DEFAULT ''`
- `last_test_http_status INTEGER NOT NULL DEFAULT 0`
- `last_test_latency_ms BIGINT NOT NULL DEFAULT 0`

The existing model-level `last_error` stores the latest sanitized failure
message and is cleared on a passed result. Repository recording updates one
configured model row by `(provider, account_id, model)` and returns not found
when no row matches.

`GET /api/admin/provider-accounts/{id}/models` returns these latest fields so
the modal can restore results after reload.

## Implementation Plan

1. Add the migration, model result types, repository recording method, and
   scan fields.
2. Add a provider model prober that uses exact-account credentials and
   transport settings.
3. Add the service method, admin endpoint, and backend tests.
4. Add frontend state for selected models and transient execution status.
5. Add the model-test modal, filtered tri-state selection, row test action, and
   bounded-concurrency runner.
6. Update source-level frontend tests and operational documentation.
7. Run backend, frontend, rendered-browser, and Docker Compose verification.

## Acceptance Criteria

- Opening Model tests selects no models and sends no request.
- One row, several rows, or every currently filtered row can be selected.
- Header select-all never silently selects models outside the current filtered
  result set.
- `Test selected (N)` sends exactly one request per selected model and never
  tests an unselected model.
- At most three model-test requests run concurrently.
- Each test uses the selected account's stored provider credential and cannot
  fall back to another account.
- Codex OAuth success requires a `response.completed` event.
- Results persist and reappear after the model list is reloaded.
- Failed model tests do not automatically mutate account health or model
  routing configuration.
- Disabled models remain visible and can be selected deliberately.
- No credential or full upstream response content is exposed.
- Backend tests, frontend tests, frontend checks/build, rendered interaction
  QA, and Docker Compose smoke checks pass.

## Documentation Reference

OpenAI's Responses migration guide documents typed server-sent events and
identifies `response.completed` and `error` as events streaming consumers must
handle:

https://developers.openai.com/api/docs/guides/migrate-to-responses#7-update-streaming-consumers
