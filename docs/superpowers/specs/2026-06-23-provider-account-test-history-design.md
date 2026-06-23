# Provider Account Test History Design

## Goal

Persist recent provider account test executions so account health checks and scheduled auto tests have an inspectable history, not only the latest `lastTest*` fields.

## Context

sub2api's scheduled test subsystem stores test plans and result rows. N2API already has manual **Test account**, **Test all accounts**, backend auto tests, and latest result fields on `provider_accounts`, but it does not preserve previous probe results.

For N2API V1, the useful next step is result history, not the full multi-plan scheduler. The existing global auto-test schedule and manual probes can all write the same history rows.

## Scope

In scope:

- Add `provider_account_test_results` table.
- Record one row every time `RecordAccountTestResult` updates latest test state.
- Expose recent results through provider service and admin HTTP API.
- Keep existing `provider_accounts.last_test_at`, `last_test_status`, and `last_test_error` as the fast latest-result summary.

Out of scope:

- Per-account cron plans.
- Per-model test results.
- Response body storage.
- Latency measurement in this first history slice.
- UI rendering of history; the API enables that in a follow-up slice.
- Retention cleanup jobs.

## Data Model

`provider_account_test_results`:

- `id BIGSERIAL PRIMARY KEY`
- `account_id BIGINT NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE`
- `provider TEXT NOT NULL`
- `status TEXT NOT NULL`
- `message TEXT NOT NULL DEFAULT ''`
- `checked_at TIMESTAMPTZ NOT NULL`
- `created_at TIMESTAMPTZ NOT NULL DEFAULT now()`

Indexes:

- `(account_id, checked_at DESC, id DESC)` for account history.
- `(provider, checked_at DESC, id DESC)` for future provider-wide diagnostics.

## API

Add:

`GET /api/admin/provider-accounts/{id}/test-results?limit=20`

Response:

```json
{
  "results": [
    {
      "id": 1,
      "accountId": 7,
      "provider": "openai",
      "status": "passed",
      "message": "",
      "checkedAt": "2026-06-23T00:00:00Z",
      "createdAt": "2026-06-23T00:00:00Z"
    }
  ]
}
```

Limits:

- Missing or non-positive `limit` defaults to `20`.
- Maximum `limit` is `100`.
- Invalid path ID returns `400`.
- Unknown account returns `404`.

## Verification

Required gates:

- Migration embedded test.
- Store integration test proving `RecordAccountTestResult` updates latest state and inserts history rows.
- Provider service test proving history is listed with default/max limits.
- HTTP API tests for list, limit, auth, and unknown account.
- `go test ./...`
- `bun run check`
- `bun run build`
