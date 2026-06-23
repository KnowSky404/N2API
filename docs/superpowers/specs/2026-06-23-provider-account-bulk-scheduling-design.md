# Provider Account Bulk Scheduling Design

## Goal

Let the admin apply scheduling parameters to selected provider accounts in one action. This continues the sub2api-inspired account management migration by making priority and load-factor tuning practical for a small account pool.

## Context

N2API already supports per-account scheduling controls:

- enabled state
- priority
- load factor
- manual pause/reset
- selected-account bulk enable/disable
- selected-account testing

The remaining friction is that tuning several accounts requires editing each row. For personal gateway use, the useful next slice is a constrained bulk scheduling editor, not the full sub2api bulk editor.

## Scope

In scope:

- Extend `POST /api/admin/provider-accounts/bulk-update` to accept `priority` and `loadFactor` in addition to `enabled`.
- Require at least one scheduling field among `enabled`, `priority`, and `loadFactor`.
- Preserve existing ID validation: at least one positive ID, no more than 100 IDs, dedupe repeated IDs in first-seen order.
- Reuse provider `UpdateAccount` validation for priority and load factor.
- Add Provider accounts page controls for selected-account bulk priority and load factor.
- Reload provider account and model routing state after a successful bulk scheduling update.
- Document that selected accounts can have scheduling parameters applied together.

Out of scope:

- Bulk account model editing.
- Bulk credential rotation.
- Bulk pause duration changes.
- Per-provider scheduling plans or weighted policies beyond the existing priority/load-factor fields.

## API

`POST /api/admin/provider-accounts/bulk-update`

Existing request remains valid:

```json
{
  "accountIds": [7, 8],
  "enabled": false
}
```

New request shape:

```json
{
  "accountIds": [7, 8],
  "priority": 20,
  "loadFactor": 5
}
```

The endpoint applies the same patch to every unique account ID. It returns the updated account rows:

```json
{
  "accounts": []
}
```

## UI

The Provider accounts bulk action bar adds:

- `Bulk priority` number input, minimum `0`
- `Bulk load factor` number input, range `1` to `100`
- **Apply scheduling** button

The button is disabled when no account is selected or a provider-account action is saving. Frontend validation mirrors existing row validation:

- priority must be a non-negative whole number
- load factor must be a whole number from `1` to `100`
- at least one field must be provided before sending

## Verification

- HTTP API tests prove bulk priority/load-factor updates are passed to `UpdateAccount`.
- HTTP API tests prove missing scheduling fields are rejected.
- Frontend source tests prove state and UI controls are wired to `/bulk-update`.
- Documentation tests prove README and deploy notes mention bulk scheduling parameters.
- Standard verification remains `go test ./...`, `bun test`, `bun run check`, and `bun run build`.
