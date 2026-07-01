# Upstream Model Sync Design

## Summary

Provider account model capability stays account-scoped, but API-upstream accounts should no longer require the admin to type every model name by hand. N2API will add an explicit admin action that asks an OpenAI-compatible upstream for `GET /v1/models`, stores discovered models as upstream-sourced account model rows, and keeps manual model entries as a separate admin-owned override.

The first implementation is conservative: newly discovered upstream models are saved disabled. The admin must enable a model before it can participate in routing.

## Goals

- Let an admin sync available models from an API-upstream account's configured base URL and API key.
- Preserve manual model entry for models that are usable but not returned by the upstream model list.
- Keep synced and manual model sources separate in storage and visible in the UI.
- Never let sync overwrite or delete manual models.
- Add newly discovered upstream models disabled by default.
- Preserve enabled/disabled state for previously synced models across later syncs.
- Use the account's configured proxy for the sync request when present.
- Surface sync status and errors inline in the provider account edit modal.

## Non-Goals

- No background or scheduled model sync job.
- No sync for Codex OAuth accounts in this phase.
- No automatic enablement of newly discovered models.
- No broad provider abstraction for non-OpenAI-compatible model-list shapes.
- No billing, public SaaS behavior, recharge flow, or multi-tenant account model.
- No change to API-key model policies.
- No change to the public `/v1/models` response contract beyond naturally reflecting enabled account models.

## Current Baseline

N2API already stores account model capability in `provider_account_models` with these important columns:

- `model`
- `enabled`
- `source`
- `last_seen_at`
- `last_error`
- `metadata`

The current manual model editor uses:

- `GET /api/admin/provider-accounts/{id}/models`
- `PUT /api/admin/provider-accounts/{id}/models`

The service/repository path currently treats edited models as `source = 'manual'`. `ReplaceAccountModels` deletes and replaces only manual rows, which is the right extension point for upstream-sourced rows.

The provider account edit UI is a modal in `frontend/src/routes/providers/+page.svelte`. It currently has a `Manual models` section with a textarea, save button, enabled checkboxes, and remove actions.

## Documentation Reference

OpenAI's current model-list API returns a list object with `data[]` model objects. Each model has an `id`, `object`, `created`, and `owned_by`; requests authenticate with `Authorization: Bearer <API key>`.

N2API should use only `data[].id` as the model name for routing capability. `owned_by`, `created`, and raw object details may be stored as metadata when useful, but routing must not depend on them.

## Source Model

Add a second account model source:

- `manual`: admin-entered model rows.
- `upstream`: rows discovered from the account's upstream `GET /v1/models`.

Rules:

- Manual rows are authoritative when a model exists in both manual input and upstream results.
- Upstream sync must never delete or rewrite manual rows.
- A manual row with the same model name blocks inserting a duplicate upstream row because `provider_account_models` has `UNIQUE (account_id, model)`.
- When the upstream returns a model that already exists as manual, the implementation may record that the manual model was seen upstream in metadata, but it must not change `source` or `enabled`.
- Public routing and `/v1/models` continue to ignore source and only care about enabled model rows on eligible enabled accounts.

## Backend Design

### Store Layer

Add repository support for replacing upstream-sourced model rows for a single account:

```go
SyncAccountModels(ctx context.Context, provider string, accountID int64, models []AccountModelInput, seenAt time.Time) ([]AccountModel, error)
```

Behavior:

1. Normalize model input with the same trimming, dedupe, length, and count limits as manual model input.
2. Start a transaction.
3. Lock the parent `provider_accounts` row with `FOR UPDATE`.
4. Read existing upstream rows for the account into a map of `model -> enabled`.
5. Read existing manual models for conflict detection.
6. Delete existing `source = 'upstream'` rows for the account.
7. Insert upstream rows for returned models that do not already have a manual row.
8. Preserve previous upstream enabled state when a model existed before; insert newly discovered models with `enabled = false`.
9. Set `source = 'upstream'`, `last_seen_at = seenAt`, `last_error = ''`, and metadata from the upstream response when available.
10. Return the full account model list ordered by model, including manual rows.

If the upstream returns a model that is manual-only, the sync should not insert a duplicate. If metadata for "seen upstream" is added to the manual row, that update must not affect enabled state or source.

For V1 stale upstream models should be removed when they are not returned by the latest successful sync. This keeps the synced list truthful. Manual rows remain untouched.

### Service Layer

Add a provider service operation:

```go
SyncUpstreamAccountModels(ctx context.Context, accountID int64) ([]AccountModel, error)
```

Behavior:

1. Validate `accountID > 0`.
2. Load the account by id.
3. Reject non-`api_upstream` accounts with `ErrInvalidInput`.
4. Decrypt the account API key.
5. Decrypt the account proxy URL if configured.
6. Build a request to the account base URL plus `/models`, respecting existing base URL normalization that treats a configured `/v1` base as OpenAI-compatible.
7. Send `GET` with `Authorization: Bearer <decrypted API key>`.
8. Use the account proxy for the request when present.
9. Require a successful `2xx` response and an OpenAI-compatible JSON list shape.
10. Parse `data[].id`, ignore blank ids, normalize and dedupe.
11. Store results with `source = 'upstream'` through the repository sync method.

The service should use a bounded request timeout so a broken upstream cannot hang the admin UI. It should not log decrypted credentials or response bodies.

### HTTP API

Add an admin-only endpoint:

```http
POST /api/admin/provider-accounts/{id}/models/sync
```

Response:

```json
{
  "models": [
    {
      "id": 1,
      "accountId": 7,
      "provider": "openai",
      "model": "gpt-5",
      "enabled": false,
      "source": "upstream",
      "lastSeenAt": "2026-07-01T08:00:00Z",
      "lastError": "",
      "metadata": {},
      "createdAt": "2026-07-01T08:00:00Z",
      "updatedAt": "2026-07-01T08:00:00Z"
    }
  ],
  "synced": {
    "total": 12,
    "new": 3,
    "preserved": 9,
    "skippedManual": 1
  }
}
```

The UI can function with only `models`, but returning summary counts makes the inline status useful without recomputing every case on the client.

## Frontend Design

Rename the edit-modal section from `Manual models` to `Models`.

### Section Header

Show a compact operational summary:

```text
8 total · 5 synced · 3 manual · 2 enabled
```

For API-upstream accounts, show a secondary `Sync from upstream` button next to `Save manual`. Codex OAuth accounts should not show the sync action.

Button states:

- `Sync from upstream`: idle.
- `Syncing`: request in flight.
- Disabled while account models are loading, manual save is in flight, or another sync is in flight.

### Manual Input

Keep the textarea because it is efficient for bulk edits and matches current user workflow.

Label it `Manual models` inside the broader `Models` section. It should continue to accept one model per line. `Save manual` updates only manual rows.

### Model List

Show one row per account model:

- checkbox for enabled state
- model id as monospace link to routing diagnostics
- source badge: `Synced`, `Manual`, or `Manual · seen upstream`
- status text: `On` or `Off`
- remove action only for manual rows

For upstream rows, removal is intentionally not offered in V1. The admin should disable the row; a later sync would otherwise recreate it.

### Inline Status

Show inline results, not toasts:

- sync success: `Synced 12 models. 3 new models were added disabled.`
- sync failure: red inline error with the upstream status or parse failure.
- manual save success: keep existing `Saved.` behavior.
- no enabled models: keep the existing warning that the account cannot receive model-routed POST traffic.

### Add API Upstream Modal

Keep the existing `Manual models` textarea in the Add API upstream modal. Do not call the upstream during account creation in V1, because creation already handles credential validation and model sync failure would make the add flow harder to reason about.

After creation, the admin can open the edit modal and run `Sync from upstream`.

## Error Handling

- Missing account id: `400 bad_request`.
- Unknown account: existing provider account not-connected/not-found mapping.
- Non-API-upstream account sync: `400 invalid_input`.
- Missing base URL or API key: `400 invalid_input`.
- Upstream authentication failure: return an admin-safe error such as `upstream returned 401 while listing models`.
- Upstream timeout: return `upstream model sync timed out`.
- Invalid JSON or missing `data` array: return `invalid upstream model list response`.
- Too many models or overlong model ids: return `400 invalid_input` and do not partially update rows.

Failed sync must not modify existing upstream or manual rows. Only a successful parse and validation should replace upstream rows.

## Security Rules

- Sync endpoints require an admin session.
- Decrypted API keys and proxy URLs must stay inside the service call.
- Do not log decrypted credentials.
- Do not expose upstream API keys in UI responses.
- Do not persist raw upstream response bodies.
- Model ids are not secrets, but they should still be treated as account configuration rather than public account metadata.

## Testing Strategy

Backend tests:

- repository sync inserts upstream rows disabled by default.
- repository sync preserves enabled state for existing upstream rows.
- repository sync removes stale upstream rows on successful sync.
- repository sync leaves manual rows untouched.
- repository sync skips upstream duplicates when manual rows have the same model.
- service rejects sync for Codex OAuth accounts.
- service uses API-upstream credentials and base URL to call `/v1/models`.
- service sends `Authorization: Bearer <key>`.
- service parses OpenAI-compatible `data[].id`.
- service does not update rows when upstream returns an error or invalid response.
- HTTP endpoint requires admin and returns models plus sync summary.

Frontend tests:

- providers page contains `Models`, `Manual models`, and `Sync from upstream` in the edit modal.
- sync button is shown for API-upstream accounts and hidden or disabled for Codex OAuth accounts.
- account model summary counts total, synced, manual, and enabled rows.
- synced rows show source badges and no remove button.
- manual rows keep remove behavior.
- successful sync updates local account model state and refreshes routing diagnostics.
- failed sync shows inline error without clearing the manual textarea.

Verification:

- `go test ./...` from `backend/`.
- `bun run check`, `bun run build`, and `bun test` from `frontend/`.
- After implementation, rebuild/recreate the Docker Compose app service and smoke check the provider page.
