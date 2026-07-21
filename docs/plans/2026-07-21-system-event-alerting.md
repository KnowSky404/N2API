# System Event Alerting Plan

Status: in progress; Tasks 1-2 completed locally on 2026-07-21
Public API changes: additive authenticated alert settings and test endpoint
Data migration: alert rules/actions and delivery state

## Current Baseline

System Events provide validated categories, severities, outcomes, bounded
metadata, audit context, filters, a signed cursor, and retention. Notification
actions, exact-match rules, and bounded per-rule state now persist without
starting delivery. Provider failures, budgets, routing exhaustion, cleanup, and
runtime operations already generate many of the source signals that alerting
should consume.

## Task 1: Define Rules And Delivery Actions

Task status: completed locally on 2026-07-21

### Goal

Persist minimal rules and encrypted destinations without changing gateway paths.

### Dependencies

Trusted proxy/security headers and versioned encryption-envelope design.

### Files

- Create: migration for `alert_actions`, `alert_rules`, and bounded delivery state
- Create: `backend/internal/alerting/` domain and tests
- Modify: secret encryption callers and System Event actions
- Test: migrations, validation, encryption, rule matching
- Document: `docs/manual.md`

### Implementation

Model severity/category/action filters, aggregation window, cooldown,
deduplication key, recovery notification, enabled state, and one encrypted
action destination. Start with Generic Webhook plus either ntfy or Gotify;
Telegram/Slack/Discord adapters follow as separate commits.

The local implementation selects ntfy alongside Generic Webhook. Destinations
use a dedicated authenticated encryption kind and are redacted from every
returned action. Rules support exact category, severity, trigger-action, and
explicit recovery-action fields; aggregation and cooldown boundaries; rule- or
target-scoped SHA-256 deduplication; and at most 1024 persisted states per rule.
The store serializes event evaluation and new-state admission by locking the
rule, evicts only the oldest idle state at capacity, and refuses admission when
every state is firing. A rule update clears its previous aggregation and firing
state in the same transaction. No default rules, dispatcher, outbound request,
Admin API, or UI are part of this task.

### Tests And Verification

Test invalid URLs, secret redaction, cooldown boundaries, recovery transitions,
and metadata size bounds.

### Compatibility And Security

No rule exists by default, so no outbound network behavior is introduced.

### Risks And Rollback

Rule complexity can become a second query language. Keep exact fields only;
disable all rules to roll back.

### Completion Criteria

Rules and actions round-trip without exposing encrypted destinations.

### Commit

`feat(alerting): persist notification rules and actions`

## Task 2: Add A Bounded Asynchronous Dispatcher

Task status: completed locally on 2026-07-21

### Goal

Deliver notifications without blocking requests or recursively alerting.

### Dependencies

Task 1 and shared background-task status.

### Files

- Create: dispatcher, adapters, tests under `backend/internal/alerting/`
- Modify: `backend/cmd/n2api/main.go`, System Event recorder/wiring
- Test: queue saturation, retry/backoff, timeout, shutdown, recursion guard

### Implementation

Use a bounded in-process queue for the default single node, fixed worker count,
short HTTP timeout, capped exponential retries, cooldown/dedupe state, and
recovery transitions. Alert-delivery failure emits a System Event explicitly
excluded from notification matching. Queue overflow increments one aggregate
counter/event. Persistent delivery is deferred until loss on restart proves
unacceptable.

The local implementation adds an `AFTER INSERT` PostgreSQL notification that
publishes only the committed System Event ID. A dedicated pgx listener reads the
event after commit, so transactional audit/runtime events are never dispatched
before commit and rolled-back events are not dispatched. One evaluator preserves
commit order and stably shards each rule/deduplication stream to one of two fixed
HTTP workers, so firing and recovery stay ordered while unrelated streams can run
in parallel.
The event and delivery queues are bounded; event saturation never waits on the
gateway path and produces a periodic aggregate overflow event.

Rule state and its cooldown are committed before an HTTP delivery attempt. A
failed attempt or process crash does not roll that state back, which prevents a
retry storm but can lose a notification. Listener disconnects and process
restarts can also lose notifications; a durable outbox remains deferred. Generic
Webhook and ntfy use a dedicated client with a five-second timeout, no environment
proxy, no redirects, bounded response draining, three capped attempts, and fixed
sanitized error codes. Network failures, `408`, `425`, `429`, and `5xx` retry;
other non-`2xx` responses do not. `alert_delivery.failed` and
`alert_delivery.queue_overflow` are rejected by rule validation, event intake,
and matching to prevent recursion.

`N2API_ALERT_DELIVERY_ENABLED` is an independent startup gate and defaults to
`false`. Enabling it requires at least two PostgreSQL pool connections because
the listener reserves one. Authenticated admin health exposes bounded delivery
status at `tasks.alertDelivery`; public health endpoints do not expose it.

### Completion Criteria

Gateway latency is independent of notification destination latency or failure.

### Risks And Rollback

Notification storms are blocking defects. Disable dispatcher startup and rules.

### Commit

`feat(alerting): dispatch bounded webhook notifications`

## Task 3: Add Admin Management And Test Notification

### Goal

Manage actions/rules safely and verify connectivity.

### Dependencies

Tasks 1-2.

### Files

- Modify: admin service/store/HTTP interfaces and tests
- Create: focused frontend alerting route or settings section and tests
- Test: Bun checks/build, Playwright create/test/disable flow

### Implementation

Use redacted destinations, explicit Save/Cancel/X dialogs, test notification
with rate limiting, delivery status, last result, and no raw response body.

### Completion Criteria

An owner can create, test, disable, and inspect an action without revealing its
secret.

### Commit

`feat(alerting): manage notification rules in admin UI`

## Task 4: Add Operational Rules Incrementally

### Goal

Map existing signals to useful defaults without alert noise.

### Dependencies

Tasks 1-3 plus request-log retention/task status.

### Implementation

Add independent commits for repeated OAuth refresh failure/account expiry;
circuit/routing exhaustion; 80/100 percent budget thresholds; 5xx/latency/fallback
windows; cleanup/storage failures; and optional version availability. Version
checks are off by default and never required for startup.

### Completion Criteria

Every event has trigger, aggregation, cooldown, recovery, and test coverage.

### Commit

`feat(alerting): add <signal> notification rule`
