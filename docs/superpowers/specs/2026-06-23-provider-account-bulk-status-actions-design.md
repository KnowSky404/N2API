# Provider Account Bulk Status Actions Design

## Goal

Add selected-account bulk actions for operational status management on the provider accounts page:

- Pause scheduling for selected accounts for the configured pause duration.
- Reset local status for selected accounts after manual recovery.

This extends existing single-account pause/reset behavior and existing selected-account bulk actions without changing provider selection algorithms.

## Scope

In scope:

- Admin-only backend endpoints under `/api/admin/provider-accounts`.
- Validation for selected account IDs and pause duration.
- Frontend selected-account buttons on the provider accounts page.
- Reuse of the existing pause duration form.
- Refresh provider account and model routing state after successful bulk actions.

Out of scope:

- Account groups, tags, or scheduling profiles.
- Persistent bulk operation history.
- Background job queues.
- Changes to automatic account health classification.

## Backend API

Add two endpoints:

- `POST /api/admin/provider-accounts/bulk-pause`
  - Request: `{"accountIds":[7,8],"durationSeconds":300}`
  - Response: `{"accounts":[...]}`
- `POST /api/admin/provider-accounts/bulk-reset-status`
  - Request: `{"accountIds":[7,8]}`
  - Response: `{"accounts":[...]}`

Both endpoints:

- Require admin authentication.
- Reject empty lists, non-positive IDs, and more than 100 IDs with `400 invalid_input`.
- Deduplicate IDs while preserving request order.
- Stop and return the provider error if any account action fails.

`bulk-pause` delegates to `ProviderService.PauseAccountScheduling` for each selected ID. The existing provider service owns duration validation, so invalid durations continue to map through `writeProviderAccountError`.

`bulk-reset-status` delegates to `ProviderService.ResetAccountStatus` for each selected ID.

## Frontend

The provider accounts page already tracks selected account IDs and exposes selected bulk test, enable, disable, priority, and load factor controls. Add two selected actions near the existing selected-account buttons:

- `Pause selected`
- `Reset selected`

`Pause selected` uses `providerAccountPauseForm.durationSeconds`, matching the single-account pause action. Both actions disable while saving or when no accounts are selected.

After success:

- Clear the selected account IDs.
- Reload provider accounts.
- Reload model routing state.
- Keep passive success feedback inline-free, following the current admin UI pattern.

On failure, surface an error in `providerAccounts.error`.

## Testing

Backend tests:

- Bulk pause deduplicates selected IDs, preserves order, returns paused accounts, and records the configured duration.
- Bulk pause rejects empty IDs, bad IDs, too many IDs, and invalid durations without calling the provider action.
- Bulk reset deduplicates selected IDs, preserves order, and returns reset accounts.
- Bulk reset rejects empty IDs, bad IDs, and too many IDs without calling the provider action.

Frontend source tests:

- Admin state exports `pauseSelectedProviderAccounts` and calls `/api/admin/provider-accounts/bulk-pause`.
- Admin state exports `resetSelectedProviderAccountStatus` and calls `/api/admin/provider-accounts/bulk-reset-status`.
- Provider accounts page imports and renders `Pause selected` and `Reset selected`.

Docs tests:

- Document selected accounts can be paused and reset together.
