# Provider Account Bulk Refresh Design

## Goal

Add selected-account bulk refresh for provider accounts so an admin can force credential/token refresh on a chosen subset of gateway exits.

This mirrors the useful part of sub2api's account bulk refresh workflow while keeping N2API's V1 personal-gateway scope: no background queue, no multi-tenant account groups, and no channel-monitor system.

## Scope

In scope:

- Admin-only backend endpoint under `/api/admin/provider-accounts`.
- Validation for selected provider account IDs.
- Reuse of the existing `ProviderService.RefreshAccount` behavior.
- Frontend selected-account button on the Provider accounts page.
- Refresh provider, provider account, and model routing state after success.
- README and deploy documentation.

Out of scope:

- Cron-based per-account scheduled test plans.
- Account groups or filtered bulk operations.
- Persistent bulk operation history.
- Automatic re-enable or priority changes after refresh.

## Backend API

Add:

- `POST /api/admin/provider-accounts/bulk-refresh`
  - Request: `{"accountIds":[7,8]}`
  - Response: `{"accounts":[...]}`

The endpoint:

- Requires admin authentication.
- Rejects empty lists, non-positive IDs, and more than 100 IDs with `400 invalid_input`.
- Deduplicates IDs while preserving request order.
- Stops and returns the provider error if any account refresh fails.
- Delegates to `ProviderService.RefreshAccount` for each selected ID.

## Frontend

Add a `Refresh selected` action near the existing selected-account controls.

The action:

- Uses the existing selected provider account state.
- Disables while no accounts are selected or while provider account state is saving.
- Shows `Select at least one provider account` for an empty selection.
- Calls `/api/admin/provider-accounts/bulk-refresh`.
- On success, clears selection and reloads provider status, provider accounts, and model routing.
- On failure, reloads provider accounts and preserves an inline error in `providerAccounts.error`.

No passive success toast is added.

## Testing

Backend tests:

- Bulk refresh deduplicates selected IDs, preserves order, returns refreshed accounts, and records that each selected account was refreshed.
- Bulk refresh rejects empty IDs, bad IDs, and too many IDs without calling refresh.

Frontend source tests:

- Admin state exports `refreshSelectedProviderAccounts`.
- Admin state calls `/api/admin/provider-accounts/bulk-refresh`.
- Provider accounts page imports and renders `Refresh selected`.

Docs tests:

- README and deploy README mention **Refresh selected**, selected provider accounts, and forcing credential refresh together.
