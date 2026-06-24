# Request Log Fallback Diagnostics Design

## Goal

Make gateway scheduling behavior visible in Request Logs by recording whether a request needed provider-account fallback before it completed.

## Context

sub2api's useful operational model is not only that multiple upstream accounts exist, but that the operator can understand how the scheduler is using them. N2API already has provider accounts, priority/load ordering, sticky sessions, concurrency skips, pre-stream fallback, account health updates, and request logs. The missing diagnostic is per-request fallback visibility: the log shows the final provider account but not whether earlier accounts were skipped or failed before that final account.

## Scope

In scope:

- Add request-log fields for gateway attempt count and fallback count.
- Count account-slot skips and retryable pre-stream upstream failures as fallback decisions.
- Keep local guard rejections at zero attempts and zero fallbacks.
- Expose the fields through the admin request-log API.
- Show compact attempt/fallback diagnostics on the Request Logs page.
- Document that Request Logs include gateway fallback diagnostics.

Out of scope:

- Storing every attempted account id or upstream error chain.
- Changing scheduler order or retry behavior.
- Adding distributed tracing or OpenTelemetry.
- Adding SaaS user, billing, balance, or payment behavior.

## Behavior

The gateway stores:

- `gateway_attempt_count`: number of selected provider-account attempts for this request.
- `gateway_fallback_count`: number of times the proxy moved past a selected provider account before returning a final response.

Examples:

- First account succeeds: attempts `1`, fallbacks `0`.
- First account returns a retryable 429 before streaming, second succeeds: attempts `2`, fallbacks `1`.
- First account is at its local concurrency cap, second succeeds: attempts `2`, fallbacks `1`.
- All candidate accounts are busy and the proxy returns local 429: attempts is the number of selected busy accounts, fallbacks is the number of busy-account skips.
- API-key request-rate rejection before provider selection: attempts `0`, fallbacks `0`.

The Request Logs UI displays a compact `Attempts X / Fallbacks Y` readout. It does not add a modal or a per-attempt timeline in this slice.

## Data Model

Add nullable-safe integer columns to `request_logs`:

- `gateway_attempt_count INTEGER NOT NULL DEFAULT 0`
- `gateway_fallback_count INTEGER NOT NULL DEFAULT 0`

The insert path writes both values for new requests. Existing rows remain at zero.

## Verification

- Gateway tests prove successful retry and account-concurrency fallback logs include attempt/fallback counts.
- Store tests prove the migration is embedded and the insert/select SQL includes the new fields.
- HTTP API tests prove request logs serialize the fields.
- Frontend source tests prove the Request Logs page renders the new diagnostics.
- Documentation tests require README and deploy README to mention Request Logs gateway fallback diagnostics.
