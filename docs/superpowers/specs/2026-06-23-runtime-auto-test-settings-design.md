# Runtime Provider Account Auto-Test Settings Design

## Goal

Move provider account automatic testing from startup-only environment configuration into the existing Gateway Settings management surface. This continues the sub2api-inspired scheduling migration while keeping N2API V1 scoped to a personal gateway.

## Context

N2API already has manual **Test account**, **Test all accounts**, scheduling pause/reset actions, routing diagnostics, and an automatic backend runner controlled by:

- `N2API_PROVIDER_ACCOUNT_AUTO_TEST_ENABLED`
- `N2API_PROVIDER_ACCOUNT_AUTO_TEST_INTERVAL_SECONDS`

Those env values are useful defaults, but they are not gateway management. sub2api exposes scheduled test plan management through admin APIs; N2API should not copy that multi-plan system yet, but the administrator should be able to view and adjust the global account-test schedule from the same operational Gateway page that manages runtime limits.

## Scope

In scope:

- Extend `admin.GatewaySettings` with:
  - `providerAccountAutoTestEnabled`
  - `providerAccountAutoTestIntervalSeconds`
- Use startup env values as `DefaultGatewaySettings`.
- Persist the new fields in the existing `settings` JSON row keyed by `gateway_settings`.
- Normalize old stored settings that do not contain these fields to disabled plus `300` seconds.
- Reject negative intervals and reject enabled intervals below `60` seconds.
- Change the backend runner wiring so it reads the latest admin gateway settings between cycles.
- Add Gateway page controls to view and save the schedule settings.
- Update documentation and tests.

Out of scope:

- Per-account scheduled test plans.
- New scheduled test result history tables.
- Channel monitor rollups, public monitor pages, or availability timelines.
- Redis, distributed locking, queues, or multi-instance coordination.
- Auto-enabling disabled accounts after a successful test.

## Behavior

`N2API_PROVIDER_ACCOUNT_AUTO_TEST_ENABLED` and `N2API_PROVIDER_ACCOUNT_AUTO_TEST_INTERVAL_SECONDS` remain startup defaults. On first boot with no stored gateway settings, `GET /api/admin/gateway-settings` returns the configured defaults. Once the admin saves Gateway Settings, the stored JSON becomes authoritative.

The auto-test runner asks for current settings before each cycle. If auto tests are disabled, it skips `TestAccounts` and waits for the configured interval before checking again. If enabled, it runs the same provider service method used by **Test all accounts**, then waits for the latest interval. A long-running probe must not overlap with another probe.

## UI

The Gateway page keeps runtime limits in the existing Gateway Settings form and adds a small **Provider account auto tests** section in the same form. The controls are:

- Checkbox: `Enable auto tests`
- Number input: `Interval seconds`

The save action remains a single Gateway Settings save so runtime limits and scheduling settings stay consistent.

## Validation

Backend validation:

- Runtime limit fields must remain non-negative.
- `ProviderAccountAutoTestIntervalSeconds < 0` is invalid.
- When `ProviderAccountAutoTestEnabled` is true, interval must be at least `60`.
- Interval `0` normalizes to the default `300` seconds so older saved settings render predictably.

Frontend validation mirrors the backend before sending:

- Runtime limits and interval must be non-negative whole numbers.
- Enabled interval must be at least `60`.

## Verification

Required gates:

- Backend admin service tests for defaults, persistence, and validation.
- HTTP API tests for GET and PUT JSON fields.
- Runner tests for dynamic settings reload.
- Frontend source tests for Gateway page controls and admin-state payload mapping.
- `go test ./...`
- `bun run check`
- `bun run build`
