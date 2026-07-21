# System Event Alerting Plan

Status: in progress; Tasks 1-3 and the first two Task 4 rules completed locally on 2026-07-21
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

Task status: completed locally on 2026-07-21

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

Expose authenticated action and rule list/create/update/delete routes plus one
saved-action test route. Action responses report only whether a destination is
configured. Omitting `destination` from an action update preserves the stored
secret, while changing the action kind requires a replacement destination.
Action and rule updates carry `expectedUpdatedAt`; stale revisions fail with a
conflict instead of overwriting a concurrent edit. Deleting an action that is
still referenced by a rule also returns a conflict. Successful CRUD mutations
and their audit System Events commit in the same PostgreSQL transaction.

The test route accepts only the saved action ID and expected revision. It never
accepts an alternate URL, body, or headers, remains usable while the dispatcher
or action is disabled, performs one bounded attempt, and returns a sanitized
result without the destination, response body, response headers, or raw network
error. At most one test runs at a time and each action has a persistent 30-second
admission window. The last sanitized test result persists without changing the
action configuration revision. Test audit events are excluded from rule
matching so a broad audit rule cannot recursively turn one test into another
notification.

Delivery jobs retain both the rule and action revisions that produced the
decision. A worker silently discards a job if either configuration changed,
was disabled, or was deleted before delivery, preventing an old event from
being sent to a newly configured destination.

Add a focused `/alerting` operational page with compact delivery status,
Actions and Rules views, shared tables, and responsive stacked rows. Action and
rule editors use one modal-level Save path with explicit Save, Cancel, and X.
Save refreshes the editor revision but keeps the dialog open; only Cancel or X
closes it. Saved destinations are never refilled into the form. CRUD and test
success use transient top-right notifications, while failures preserve the
editable draft.

### Tests And Verification

Cover authentication, strict JSON decoding, destination redaction and preserve
semantics, stale updates, foreign-key conflicts, transactional audit events,
test admission, single-attempt delivery, fixed error codes, and dispatcher
revision checks. Run `go test ./...`, `go vet ./...`, `bun test`,
`bun run check`, and `bun run build`. Use Playwright for the authenticated
create, test, create-rule, disable, conflict, cleanup, refresh, and mobile modal
flows, then refresh the local Compose stack without build cache.

### Completion Criteria

An owner can create, test, disable, and inspect an action without revealing its
secret. Concurrent edits do not overwrite one another, old jobs never cross an
action revision, last test results survive refresh/restart, and the rendered
desktop/mobile workflow passes browser verification.

### Commit

`feat(alerting): manage notification rules in admin UI`

## Task 4: Add Operational Rules Incrementally

Task status: in progress; repeated automatic OAuth refresh failures and Request
Log retention failures completed locally on 2026-07-21

### Goal

Map existing signals to useful defaults without alert noise.

### Dependencies

Tasks 1-3 plus request-log retention/task status.

### Implementation

Add independent commits for repeated OAuth refresh failure/account expiry;
circuit/routing exhaustion; 80/100 percent budget thresholds; 5xx/latency/fallback
windows; cleanup/storage failures; and optional version availability. Version
checks are off by default and never required for startup.

The first slice adds a versioned server-side rule-template catalog rather than
seeding a database rule. An owner explicitly selects an existing delivery
action and installs `oauth-refresh-repeated-v1`; the installed rule starts
disabled and remains an ordinary editable/deletable rule. Its persisted
`template_key` makes retries and concurrent installation idempotent without
overwriting the selected action, enabled state, or later edits. The template
matches three `oauth.refresh.automatic.failed` warning events for one target in
15 minutes, applies a one-hour cooldown, and recognizes
`oauth.refresh.automatic.succeeded` as recovery. Model-test refresh failures
emit `oauth.refresh.diagnostic.failed` so diagnostics do not inflate the
operational failure window.

The second slice adds `request-log-retention-failed-v1`. It matches both full
`error` failures and partial `warning` failures by leaving severity unrestricted,
fires on the first failed scheduled cycle, uses rule-scoped deduplication and a
24-hour cooldown, and recognizes the next successful scheduled cycle as
recovery. Parent-context cancellation during shutdown updates task status but
does not emit the failed action, preventing a normal restart from triggering
the rule. The runner can be configured as frequently as every five minutes;
alternating success/failure cycles can still flap because recovery ends the
firing incident, so the template remains disabled until an owner reviews the
deployment's interval and failure behavior.

Account-expiry and circuit presets remain deferred until their recovery events
are emitted only after confirmed recovery. Request-derived thresholds require a
bounded periodic aggregator with persistent crossing state before templates can
be added without per-request event noise.

### Completion Criteria

Every event has trigger, aggregation, cooldown, recovery, and test coverage.

### Commit

`feat(alerting): add <signal> notification rule`
