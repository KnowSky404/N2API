# Provider Auto Test Status Design

## Goal

Expose provider account auto-test runtime status in the admin gateway page so the operator can tell whether scheduled account probes are running, when the last cycle started and finished, how many accounts were tested, and whether the last cycle failed.

This moves N2API closer to sub2api's scheduling and monitoring management surface without importing sub2api's full channel monitor, account group, or scheduled-test-plan subsystem.

## Scope

In scope:

- Track auto-test runner state in memory.
- Expose that state through the existing admin gateway settings API.
- Show the state near the existing Provider account auto tests controls.
- Document the runtime status fields.

Out of scope:

- Persistent auto-test history.
- Per-account cron plans.
- Channel monitor tables or request templates.
- Alerting, notification rules, or daily rollups.

## Backend Runtime State

Extend `provider.AutoTestRunner` with a status snapshot:

- `running`
- `lastStartedAt`
- `lastFinishedAt`
- `lastAccountCount`
- `lastError`

The runner updates the snapshot:

- Before each cycle starts: `running=true`, set `lastStartedAt`, clear `lastError`.
- On cycle success: set `running=false`, `lastFinishedAt`, `lastAccountCount`, clear `lastError`.
- On cycle failure: set `running=false`, `lastFinishedAt`, `lastError`, and keep `lastAccountCount=0`.

Use a mutex to make reads and writes race-free.

## Admin API

Keep the existing `GET /api/admin/gateway-settings` endpoint and add an optional `providerAccountAutoTestStatus` object in the JSON response. This avoids adding another endpoint for one small runtime field group and keeps the Gateway page's existing data load path.

The response shape is:

```json
{
  "providerAccountAutoTestEnabled": true,
  "providerAccountAutoTestIntervalSeconds": 300,
  "providerAccountAutoTestStatus": {
    "running": false,
    "lastStartedAt": "2026-06-23T12:00:00Z",
    "lastFinishedAt": "2026-06-23T12:00:03Z",
    "lastAccountCount": 3,
    "lastError": ""
  }
}
```

For unit tests and non-production server construction, the field may be omitted or zero-valued when no runner is provided.

## Wiring

Add a small HTTP server option type in `backend/internal/httpapi` that carries a status provider:

```go
type ProviderAccountAutoTestStatusSource interface {
	ProviderAccountAutoTestStatus() provider.AutoTestStatus
}
```

`provider.AutoTestRunner` implements that interface.

`backend/cmd/n2api/main.go` passes the `autoTestRunner` into `httpapi.NewServer(...)` after the gateway proxy and static file system.

## Frontend

Extend `GatewaySettingsData` and the gateway page:

- Show `Running` when a cycle is currently active.
- Show last finished time when available.
- Show tested account count from the last cycle.
- Show last error inline in amber/red text.
- Show `Not run yet` when the runner has no recorded cycle.

This stays in the existing Gateway management page; no new page or sidebar entry is needed.

## Testing

Backend tests:

- `AutoTestRunner.Status` reports no cycle before running.
- `AutoTestRunner.runCycle` reports started/finished/count on success.
- `AutoTestRunner.runCycle` reports last error on failure.
- `GET /api/admin/gateway-settings` includes `providerAccountAutoTestStatus` when a status source option is provided.
- `backend/cmd/n2api/main_test.go` proves `autoTestRunner` is passed to `httpapi.NewServer`.

Frontend tests:

- Admin state initializes and normalizes `providerAccountAutoTestStatus`.
- Gateway page contains the status labels and rendering branches.

Docs tests:

- README and deploy README mention the auto-test runtime status.
