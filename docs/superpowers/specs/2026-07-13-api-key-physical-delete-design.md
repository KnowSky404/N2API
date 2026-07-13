# API Key Physical Delete Design

Date: 2026-07-13

## Goal

Complete the API key lifecycle with a seven-day logical-delete retention period
and an explicit irreversible physical-delete action.

## State Machine

```text
enabled <-> disabled
enabled  -> deleted (logical)
disabled -> deleted (logical)
deleted (logical) -> deleted (physical)
deleted (logical) -> deleted (physical) after seven days
```

`disabled_at` remains reversible. `revoked_at` remains the logical deletion
timestamp and is irreversible. A physically deleted key no longer exists.

## Backend Contract

- `POST /api/admin/keys/{id}/revoke` logically deletes an enabled or disabled
  key and preserves the first `revoked_at` timestamp.
- `DELETE /api/admin/keys/{id}` physically deletes only an already-revoked key.
  Active and disabled keys are rejected as not found by this endpoint.
- `physicalDeleteAt` is `revokedAt + 7 days` for logically deleted keys.
- The service purges logically deleted rows whose `revoked_at` is at or before
  the seven-day cutoff at startup and hourly. Listing keys also performs the
  same cleanup as a fallback.
- Request logs remain, while their `client_key_id` becomes `NULL` through the
  existing foreign-key behavior.

## Frontend Contract

- Enabled and Disabled remain switchable.
- Deleting an Enabled or Disabled key performs logical deletion.
- A Deleted row keeps an enabled delete action. Activating it requires explicit
  confirmation and then performs physical deletion.
- Successful physical deletion removes the row and its selection locally.
- Bulk delete continues to logically delete only Enabled and Disabled keys.

## Acceptance Criteria

- Seven-day response and purge boundaries are covered by backend tests.
- Physical deletion cannot remove an Enabled or Disabled key.
- The admin endpoint returns `204 No Content` after physical deletion.
- The API Keys page confirms permanent deletion and calls the physical-delete
  endpoint only for a logically deleted row.
- Go tests, frontend tests, frontend checks, frontend build, Docker Compose
  rebuild, and container-local health checks pass.
