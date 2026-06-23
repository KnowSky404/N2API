# Provider Account Bulk Enable Design

## Goal

Add a V1 bulk account management action so the admin can enable or disable multiple provider accounts in one operation.

## Context

sub2api has account bulk action UI and bulk edit workflows. N2API currently supports single-account enable, disable, priority, load factor, pause, reset, test, refresh, and disconnect actions. For personal V1 use, the next useful migration is not the full sub2api bulk editor; it is a safe bulk scheduling control for quickly removing or restoring several accounts from gateway traffic.

## Scope

In scope:

- Add admin API `POST /api/admin/provider-accounts/bulk-update`.
- Request shape: `{ "accountIds": [7, 8], "enabled": false }`.
- Response shape: `{ "accounts": [...] }`, with updated account snapshots in request order.
- Validate:
  - `accountIds` must be non-empty.
  - every id must be positive.
  - at most `100` ids per request.
  - duplicate ids are collapsed by first occurrence.
  - `enabled` must be present.
- Use existing `provider.Service.UpdateAccount` validation and persistence.
- Add Provider accounts page row checkboxes plus a bulk action bar.
- Supported UI actions:
  - `Enable selected`
  - `Disable selected`
  - `Clear selection`
- Refresh provider accounts and model routing after a bulk update.
- Keep failures simple: if any account update fails, the API returns the mapped provider error and the UI reloads current account state.

Out of scope:

- Bulk priority or load factor editing.
- Bulk delete/disconnect.
- Bulk model updates.
- Per-account partial success reporting.
- Account groups and import/export workflows.

## Behavior

The selected set is frontend-only state. It is pruned after account list reloads so deleted or filtered-out stale ids do not remain selected forever. Bulk actions send selected ids to the backend and clear the selection after a successful update.

The backend applies updates sequentially in request order after id dedupe. This keeps the implementation simple and avoids adding a repository-specific bulk operation before V1 needs it.

## Verification

- HTTP API tests cover successful bulk disable/enable, duplicate id dedupe, empty ids, missing enabled, and invalid ids.
- Frontend state tests cover selection toggles, pruning, and bulk request payload.
- Provider page source tests cover row checkboxes and bulk action labels.
- Documentation tests require README and deploy README to mention bulk enable/disable.
- Full gates remain backend `go test ./...`, frontend `bun run check`, and frontend `bun run build`.
