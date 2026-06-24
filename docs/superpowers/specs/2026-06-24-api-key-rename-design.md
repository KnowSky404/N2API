# API Key Rename Design

## Goal

Allow the admin to rename existing client API keys after creation so key labels can stay aligned with devices, agents, or usage purpose without rotating credentials.

## Context

API Keys now owns client access policy, per-key limits, usage visibility, request-log drill-down, and local filtering. It still treats the key name as immutable after creation. Provider accounts can be renamed in-place, and client API keys should have the same basic account-management ergonomics.

## Scope

- Add a backend service method to update only an API key name.
- Add `PATCH /api/admin/keys/{id}` with JSON body `{ "name": "..." }`.
- Validate names the same way as creation: trim whitespace and reject empty names.
- Do not return, regenerate, or expose the cleartext API key secret.
- Do not allow renaming revoked or missing keys.
- Add an inline name editor on the API Keys page with a **Save name** action.

## Non-goals

- API key rotation or cleartext key recovery.
- Updating prefix/hash/created time.
- Bulk API key renaming.
- Restoring revoked keys.

## Backend Behavior

`admin.Service.UpdateAPIKeyName(ctx, id, name)` trims `name`, rejects empty names with `ErrInvalidInput`, and delegates persistence to the repository. The store updates `client_api_keys.name` only when `revoked_at IS NULL`. Missing or revoked keys return `ErrNotFound`.

The HTTP endpoint maps errors consistently with existing key update endpoints:

- invalid id or empty name: `400`
- missing or revoked key: `404`
- unexpected errors: `500`

## Frontend Behavior

The API Keys table replaces the static name text with a small form:

- input bound to the current row name;
- **Save name** button;
- disabled when the key is revoked;
- on success, the returned key replaces the row in `apiKeys.items`.

Search continues to use the current row name, so renamed keys become searchable immediately.

## Verification

- Admin service tests cover rename success and invalid names.
- HTTP tests cover endpoint success and error mapping.
- Frontend source test covers `updateAPIKeyName`, row input, and **Save name** action.
- Documentation test covers the behavior in README and deploy README.
