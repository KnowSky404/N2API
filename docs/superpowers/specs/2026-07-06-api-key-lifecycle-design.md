# API Key Lifecycle Design

Date: 2026-07-06

## Goal

Make the API Keys table match the intended lifecycle and interaction model:

- Client keys have three visible states: active, disabled, and logically deleted.
- Active and disabled keys can be toggled directly from the table status column.
- Deleted keys remain visible for a 30 day retention window, then can be physically removed.
- The table should not navigate away to Request Logs for per-key log access during this slice.

## Existing Context

The backend already stores `disabled_at` and `revoked_at` on `client_api_keys`.
Authentication rejects keys with either field set. The frontend currently renders
active, disabled, and revoked states, with Enable/Disable and Revoke actions in
the table action column. The API Keys page also links to Request Logs with a
`clientKeyId` query.

This design keeps `revoked_at` as the logical deletion timestamp to avoid a
larger schema and API rename. The UI may label this state as deleted while the
backend keeps the existing revoke semantics.

## Recommended Approach

Use the existing `revoked_at` value as the logical delete time and expose a
physical deletion time derived from it:

```text
physicalDeleteAt = revokedAt + 30 days
```

The backend should calculate this value in the API key response so the frontend,
tests, and cleanup policy share one source of truth. The repository/service layer
should add a physical cleanup operation that deletes keys whose `revoked_at` is
older than or equal to the retention cutoff.

## User Experience

The API Keys table should present:

- `Status` column:
  - Active key: active status plus an inline toggle to disable it.
  - Disabled key: disabled status plus an inline toggle to enable it.
  - Deleted key: deleted/revoked badge only. It is not toggleable.
- Deleted badge hover text:
  - Shows the scheduled physical deletion time from `physicalDeleteAt`.
- `Action` column:
  - Keeps edit/configuration actions where applicable.
  - Replaces Revoke wording with Delete.
  - Delete performs logical deletion through the existing revoke path.
  - Removes the direct Request Logs page navigation.
  - Adds a Logs button that opens an in-page modal.
- Logs modal:
  - Opens without route navigation.
  - Shows basic key identity and a static empty state for future log query UI.
  - Does not need to fetch or render real logs in this slice.

Copy-to-clipboard behavior for the one-time key secret remains unchanged.

## Backend Behavior

- Keep `revoked_at` as the logical delete timestamp.
- Add an API response field such as `physicalDeleteAt` for revoked keys.
- Return `null` or omit the physical deletion value for active and disabled keys.
- Add repository/service cleanup behavior that physically deletes keys whose
  logical deletion time is at least 30 days old.
- Preserve authentication behavior:
  - Active keys authenticate.
  - Disabled keys do not authenticate.
  - Revoked/deleted keys do not authenticate.

The cleanup operation may be exposed as an internal repository/service method in
this slice. It does not need a new admin UI control unless the implementation
finds an existing startup or settings cleanup path that already fits.

## Testing

Backend tests should cover:

- API key responses include `physicalDeleteAt` for revoked/deleted keys.
- Cleanup deletes keys older than the 30 day retention window.
- Cleanup does not delete active, disabled, or recently revoked keys.
- Existing authentication rejection for disabled and revoked keys remains intact.

Frontend tests should cover:

- Active and disabled state toggles are rendered in the status column.
- Deleted/revoked keys show a physical deletion tooltip and cannot be toggled.
- Delete is available from the action column and uses the existing revoke flow.
- Logs opens an in-page modal instead of linking to `/request-logs`.

## Out Of Scope

- Full Request Logs query UI inside the API Key modal.
- Renaming database columns from `revoked_at` to `deleted_at`.
- Hiding deleted keys before their physical retention window expires.
- Billing, tenant, or public registration behavior.
