# Provider Account Scheduling Window Visibility Design

## Goal

Make temporary unschedulable provider account states easier to understand by showing the remaining scheduling block window in the Provider accounts table.

## Context

sub2api exposes temporary unschedulable status with an expiry and remaining time. N2API already stores the V1 equivalent through provider account `rateLimitedUntil` and `circuitOpenUntil`. The Provider accounts page currently shows only the absolute expiry date, which makes it harder to scan active scheduling blocks during daily operation.

## Scope

In scope:

- Add a small frontend helper that formats a future timestamp as a remaining window label.
- Show remaining time for rate-limited and circuit-open provider accounts in the Provider accounts table.
- Keep exact timestamps available through the existing status hover detail.
- Document that pause/rate-limit rows show the remaining scheduling block.

Out of scope:

- New backend fields or database schema.
- A status-detail modal.
- Polling countdown timers.
- sub2api's full trigger metadata such as rule index, matched keyword, or status code.

## Behavior

For a future timestamp, the UI shows compact labels such as:

- `5m remaining`
- `2h 15m remaining`
- `1d 3h remaining`

Expired, missing, or invalid timestamps render no remaining label and fall back to the existing absolute timestamp formatting.

## Verification

- Unit tests cover the timestamp formatter.
- Provider page source tests prove the status column renders remaining labels for `rateLimitedUntil` and `circuitOpenUntil`.
- Documentation tests require README and deploy README to mention the remaining scheduling block display.
- Full gates remain backend `go test ./...`, frontend `bun run check`, and frontend `bun run build`.
