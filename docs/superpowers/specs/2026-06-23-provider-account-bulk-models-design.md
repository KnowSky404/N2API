# Provider Account Bulk Models Design

## Goal

Let the admin apply one model capability list to selected provider accounts from the Provider accounts page.

This ports the useful part of sub2api's account bulk edit/model whitelist workflow into N2API's personal gateway model without adding account groups, billing, users, or sub2api's full channel system.

## Context

N2API already stores per-account model capability and uses it for scheduler routing:

- `GET /api/admin/provider-accounts/{id}/models`
- `PUT /api/admin/provider-accounts/{id}/models`
- `POST /api/admin/provider-accounts/bulk-update`
- selected-account UI state for bulk enable, test, pause, reset, priority, and load factor

The missing workflow is applying the same model capability list to multiple accounts. Today, an admin must open each account row and save the same manual model list one by one. That slows down account onboarding and can leave accounts invisible to the model scheduler.

## Scope

In scope:

- Admin-only endpoint to replace model capability lists for selected provider accounts.
- Frontend selected-account controls for bulk model text and apply action.
- Reuse existing model text parser and existing provider service validation.
- Reload provider accounts and model routing after success.
- Clear selected IDs and bulk model text after success.

Out of scope:

- Account groups.
- Per-model merge semantics.
- Bulk credential rotation.
- Bulk delete/disconnect.
- Import/export.
- Copying models from one source account by ID.

## Backend API

Add:

`POST /api/admin/provider-accounts/bulk-models`

Request:

```json
{
  "accountIds": [7, 8],
  "models": [
    { "model": "gpt-5", "enabled": true },
    { "model": "codex-mini", "enabled": true }
  ]
}
```

Response:

```json
{
  "accounts": [
    { "accountId": 7, "models": [] },
    { "accountId": 8, "models": [] }
  ]
}
```

Rules:

- Require admin authentication.
- Reject empty IDs, non-positive IDs, and more than 100 IDs with `400 invalid_input`.
- Deduplicate account IDs while preserving request order.
- Reject an empty model list with `400 invalid_input`.
- Delegate model validation to `ProviderService.ReplaceAccountModels`.
- Stop and return provider error if any selected account update fails.

## Frontend

Add selected-account controls near the existing bulk scheduling controls:

- `Bulk models` textarea.
- `Apply models` button.

Behavior:

- Parse text with the existing `parseAccountModelsText`.
- Reject empty selected IDs.
- Reject empty model text.
- POST parsed models to `/api/admin/provider-accounts/bulk-models`.
- On success: clear selection, clear bulk model text, reload provider accounts, reload model routing, and prune account-model state for affected accounts so stale expanded rows do not show old capabilities.
- On failure: reload provider accounts and keep the error visible in `providerAccounts.error`.

## Testing

Backend tests:

- Successful bulk model replace deduplicates account IDs, preserves order, calls `ReplaceAccountModels` for each account, and returns per-account model lists.
- Input validation rejects empty IDs, bad IDs, too many IDs, and empty model list.

Frontend tests:

- `providerAccountBulkModelsForm` exists.
- `bulkReplaceSelectedProviderAccountModels` calls `/api/admin/provider-accounts/bulk-models`.
- Provider page exposes `Bulk models` and `Apply models`.

Docs tests:

- README and deploy README mention selected provider accounts can receive the same model capability list with **Apply models**.
