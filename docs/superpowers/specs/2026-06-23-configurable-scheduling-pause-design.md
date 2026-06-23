# Configurable Provider Account Scheduling Pause Design

## Goal

Let the admin choose how long a provider account should be temporarily removed from scheduling, instead of always pausing for five minutes from the UI.

## Context

sub2api has a temporary unschedulable account concept with active state, expiry time, trigger details, and recovery. N2API already has the V1 subset of that model: `Pause scheduling` records a local circuit-open status with `circuitOpenUntil`, and `Reset local status` clears it. The backend already accepts `durationSeconds`; the missing management surface is that the Provider accounts page always sends `300`.

## Scope

In scope:

- Add a Provider accounts page control named `Pause duration seconds`.
- Keep `300` seconds as the default UI value.
- Pass the configured value to `POST /api/admin/provider-accounts/{id}/pause`.
- Validate the value on the frontend before sending:
  - whole number only
  - at least `60`
  - at most `86400`
- Keep backend validation authoritative through the existing pause API.
- Document the configurable pause window.

Out of scope:

- Per-account saved pause defaults.
- Full sub2api temporary unschedulable trigger rules.
- New status-detail endpoints or modals.
- Automatic recovery after successful test beyond the existing reset/status behavior.

## Behavior

The Provider accounts page shows one global input for the manual pause window. Clicking an account's `Pause scheduling` action sends the current value as `durationSeconds`. If the value is invalid, the page shows an inline provider account error and does not call the API.

The backend continues to default invalid omitted durations to `defaultManualPause` and continues to reject values above its max. Existing API clients remain compatible.

## Verification

- Frontend source tests prove the input and payload are wired.
- Admin state tests prove invalid pause durations are rejected before the request helper call.
- Backend HTTP tests continue to prove `durationSeconds` reaches provider service.
- Documentation tests require README and deploy README to mention the configurable pause window.
- Full gates remain `go test ./...`, `bun run check`, and `bun run build`.
