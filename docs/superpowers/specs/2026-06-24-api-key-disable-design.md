# API Key Disable Design

## Goal

Allow the admin to temporarily disable and re-enable client API keys without revoking, deleting, rotating, or revealing the cleartext secret.

## Context

N2API already supports API key creation, rename, revoke, model access policy, per-key limits, usage visibility, and request-log drill-down. Revoke is intentionally permanent because the secret is not recoverable. Day-to-day gateway management still needs a reversible way to pause a client, device, or agent key while keeping its configuration and logs.

## Scope

- Add `client_api_keys.disabled_at`.
- Return `disabledAt` in API key responses.
- Treat disabled keys as not authenticatable for `/v1/*`.
- Keep disabled keys listable and editable by admin.
- Add admin endpoint to enable/disable a key.
- Add API Keys UI actions and status display for disabled keys.
- Update active key counts/readiness to exclude revoked and disabled keys.

## Non-goals

- Expiring keys by schedule.
- Bulk API key enable/disable.
- Re-enabling revoked keys.
- Deleting keys.
- Returning cleartext secrets after creation.

## Backend Behavior

`disabled_at` is nullable. A key is usable only when both `revoked_at IS NULL` and `disabled_at IS NULL`.

The new service method is:

```go
SetAPIKeyDisabled(ctx context.Context, id int64, disabled bool) (APIKey, error)
```

It delegates to the repository. The store updates only non-revoked rows:

- `disabled=true`: set `disabled_at = COALESCE(disabled_at, now())`
- `disabled=false`: set `disabled_at = NULL`

Missing or revoked keys return `ErrNotFound`.

## HTTP Behavior

Add:

```http
PUT /api/admin/keys/{id}/disabled
{ "disabled": true }
```

Responses return `{ "key": ... }`. Error mapping follows existing key update endpoints:

- invalid id or bad JSON: `400`
- missing/revoked key: `404`
- unexpected error: `500`

## Frontend Behavior

API Keys status becomes:

- `Revoked` when `revokedAt` exists.
- `Disabled` when `disabledAt` exists.
- `Active` otherwise.

The status filter gains **Disabled keys**. The Action cell gains:

- **Disable** for active keys.
- **Enable** for disabled keys.
- no enable/disable for revoked keys.

Model policy, limits, and name editing remain disabled only for revoked keys. Disabled keys can still be reconfigured before re-enabling.

## Verification

- Migration test checks the new column and rollback.
- Admin service/store tests verify disabled keys fail auth and can be re-enabled.
- HTTP tests cover endpoint success and error mapping.
- Frontend source tests cover status/filter/actions and `setAPIKeyDisabled`.
- Docs tests cover reversible disable behavior.
