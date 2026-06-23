# Provider Account Auto Test Design

## Summary
This phase adds a lightweight account-level automatic test loop to N2API. It brings the useful part of sub2api's scheduled account testing into the existing personal gateway model: connected provider accounts can be probed periodically, and the result feeds the same `lastTest*` and local health fields already used by manual **Test account**, **Test all accounts**, Provider accounts, and Routing diagnostics.

The design intentionally does not copy sub2api's full channel monitor system. N2API V1 remains a personal self-hosted gateway with one admin, unified provider accounts, no billing, no public user system, no Redis requirement, and no platform monitoring dashboard.

## Reference Boundary
The sub2api reference has two related feature families:

- Scheduled account tests: per-account plans and background runners for account connectivity checks.
- Channel monitor: independent monitor entities with endpoint/API key/model configuration, histories, rollups, request templates, public/user views, and feature flags.

N2API should migrate the account-health value first, not the whole platform monitor. The V1 slice should use existing provider accounts and existing probe code, so automatic checks are visible where admins already manage scheduling.

## Goals
- Add an optional background loop that periodically probes provider accounts.
- Reuse `provider.Service.TestAccounts(ctx)` so manual and automatic tests share behavior.
- Keep the feature disabled by default for self-hosted installs that do not want background upstream traffic.
- Make interval configuration explicit through environment variables and `.env.example`.
- Prevent overlapping auto-test runs in one process.
- Keep automatic test results visible through existing `lastTestAt`, `lastTestStatus`, `lastTestError`, account status, Provider accounts, and Routing diagnostics.
- Add graceful process shutdown so the HTTP server and auto-test loop stop from the same root context.
- Keep verification focused on unit tests and existing frontend build gates.

## Non-Goals
- No `channel_monitors`, `channel_monitor_histories`, daily rollups, request templates, or public monitor pages in this phase.
- No per-account custom test plans in the first implementation.
- No Redis, distributed locks, or cross-process singleton guarantee.
- No automatic model discovery.
- No UI settings form in the first implementation; environment configuration is enough for personal deployment.
- No automatic re-enabling of disabled accounts. Disabled remains an admin decision.

## Configuration
Add two backend configuration values:

- `N2API_PROVIDER_ACCOUNT_AUTO_TEST_ENABLED`
  - Boolean.
  - Default: `false`.
  - When false, no background probe loop starts.
- `N2API_PROVIDER_ACCOUNT_AUTO_TEST_INTERVAL_SECONDS`
  - Non-negative integer.
  - Default: `300`.
  - Minimum when enabled: `60`.
  - If unset while enabled, use `300`.

Invalid configuration should fail startup with a clear error. An enabled interval below 60 seconds should fail configuration validation instead of silently clamping, because it can create unexpected upstream traffic.

## Backend Design

### Auto-Test Runner
Create a small runner in `backend/internal/provider`:

```go
type AutoTestRunnerConfig struct {
	Enabled  bool
	Interval time.Duration
}

type AutoTestRunner struct {
	service *Service
	cfg     AutoTestRunnerConfig
	logger  *slog.Logger
}
```

Responsibilities:

- `Run(ctx)` returns immediately when disabled.
- When enabled, it runs one immediate cycle, then ticks on the configured interval.
- Each cycle calls `service.TestAccounts(ctx)`.
- It serializes cycles inside one process. If a cycle is still running when a tick arrives, the tick is skipped.
- It logs cycle start/end/failure with account count and duration, without logging tokens or credentials.
- It stops promptly when `ctx` is canceled.

The runner should not know about HTTP, config env parsing, database internals, or UI state. It depends only on `provider.Service`.

### Service Behavior
`TestAccounts(ctx)` already tests all accounts and records result fields. The auto-test runner should not invent a second probing path. If `TestAccounts` returns an error for one account, the current service behavior stops and returns the error. For this first automatic loop, keep that behavior and log the cycle error; do not partially mask errors in the runner.

Future improvements can add per-account result collection if automatic testing needs to continue after a single account failure. That should be a separate change to `provider.Service`, covered by tests.

### Main Lifecycle
`backend/cmd/n2api/main.go` should move from a plain `context.Background()` and blocking `ListenAndServe()` to a shared root context:

- Use `signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)`.
- Open the database and run migrations with that root context.
- Start the auto-test runner in a goroutine after provider service construction when enabled.
- Start the HTTP server in a goroutine or coordinate it with a channel.
- On signal, call `server.Shutdown` with a short timeout.
- Cancel the root context so the auto-test loop exits.

This gives the first background task a real lifecycle and improves deployment reliability.

## Admin and UI Behavior
No new UI is required in the first implementation. Existing screens already surface automatic test results:

- Provider accounts row: last test status/time/error.
- Routing diagnostics: candidate last test status/time/error.
- Manual actions remain available: **Test account**, **Test all accounts**, **Pause scheduling**, and **Reset local status**.

Documentation should explain that automatic tests are configured by environment variables and are disabled by default.

## Error Handling
- Invalid env values fail startup.
- If auto-test is disabled, startup and runtime behavior are unchanged.
- If no provider accounts exist, the runner logs a successful zero-account cycle if `TestAccounts` returns an empty list.
- If `TestAccounts` returns an error, the runner logs it and waits for the next interval.
- Context cancellation should stop the runner without logging it as a probe failure.
- The runner must not panic on transient probe errors.

## Security and Operational Rules
- Never log tokens, API keys, authorization headers, OAuth codes, or encrypted credential values.
- Keep credentials encrypted at rest through the existing provider repository path.
- Keep the default disabled to avoid surprising upstream usage.
- Do not require Redis or another process coordinator.
- Do not mutate `enabled`; admin disable remains authoritative.

## Testing Strategy
Backend tests:

- Config parsing accepts default disabled values.
- Config parsing accepts enabled interval values.
- Config parsing rejects enabled intervals below 60 seconds.
- Auto-test runner returns without calling the service when disabled.
- Auto-test runner runs an immediate cycle when enabled.
- Auto-test runner does not overlap cycles when the interval fires while a prior cycle is running.
- Main wiring should have a compile-time path that constructs the runner from config.

Docs/source tests:

- README and deploy README mention the disabled-by-default auto-test env vars.
- `.env.example` documents both env vars.

Full verification:

- `git diff --check`
- `GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...` from `backend/`
- `bun run check` from `frontend/`
- `bun run build` from `frontend/`

## Acceptance Criteria
- Setting `N2API_PROVIDER_ACCOUNT_AUTO_TEST_ENABLED=true` starts a background loop after service startup.
- The loop runs `TestAccounts` immediately and then on the configured interval.
- The loop stops when the process receives cancellation.
- Auto-test results reuse the same persisted fields and UI surfaces as manual account tests.
- Disabled-by-default behavior is documented.
- No new infrastructure dependency is introduced.
- All verification commands pass.
