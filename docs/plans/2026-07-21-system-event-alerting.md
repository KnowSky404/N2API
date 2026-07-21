# System Event Alerting Plan

Status: in progress; Tasks 1-3 and the first thirteen Task 4 slices completed locally on 2026-07-21
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

Task status: in progress; repeated automatic OAuth refresh failures, Request
Log retention failures, Provider account auto-test cycle failures, Provider
account expiry, and Provider account circuit-open completed locally on
2026-07-21

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

The third slice closes the existing Provider account auto-test scheduler signal
and adds `provider-auto-test-failed-v1`. Successful cycles keep
`scheduler.provider_account_auto_test.completed`; full and partial failures use
the new `scheduler.provider_account_auto_test.failed` action. Parent-context
cancellation during shutdown updates task status without emitting a failed
event. Both actions retain the stable
`provider_account_scheduler/auto_test` target and bounded count metadata; raw
probe errors never enter the System Event. The disabled template matches both
`error/failure` and `warning/partial` events by leaving severity unrestricted,
requires two failed cycles in 15 minutes, uses target-scoped deduplication and a
one-hour cooldown, and recognizes the next successful cycle as recovery.

This slice modifies `backend/internal/systemevent/event.go`,
`backend/internal/provider/auto_test_runner.go`, and the alert template catalog.
Focused provider, system-event, matcher, service, store, and HTTP tests must
prove exact event fields, cancellation suppression, stable target hashing,
threshold/cooldown/recovery transitions, catalog order, and idempotent install.
It is additive and requires no migration or public API shape change. Rollback is
the single feature commit; existing installed rules cannot exist before the
template ships, and every newly installed rule starts disabled.

The fourth slice must make Provider account recovery a confirmed health
transition before account-expiry or circuit presets are added. Selecting an
account and acquiring its concurrency slot records an attempt by updating only
`last_used_at`; it must not clear health state, erase refresh diagnostics, or
emit `provider_account.recovered`. A gateway request confirms recovery only
after its final upstream response is 2xx. Network errors, 3xx, 4xx, 5xx,
authorization failures, and configured error passthrough never confirm
recovery. When one account fails and a fallback succeeds, only the successful
account recovers.

Account probes use the same strict success boundary: only 2xx clears health
state. Network errors and every non-2xx response record a failed test without a
recovery transition. Refreshing credentials for an existing OAuth account uses
the credential-only update path so token refresh success preserves account
health until a later gateway or probe 2xx confirms recovery. The existing
transactional `UpdateAccount(ClearStatus: true)` path remains the recovery
write, including its state-change guard so repeated healthy requests cannot
emit duplicate recovery events. Recovery persistence is best effort in the
gateway and must not replace an already successful, potentially billable
upstream response with a local 500.

Focused gateway tests must cover 2xx, network failure, authorization refresh,
fallback, non-2xx, and error-passthrough branches. Provider and Store tests must
prove that attempts preserve every health field, probes require 2xx, existing
OAuth refreshes preserve health until confirmation, and only the first actual
health transition emits `provider_account.recovered`. Streaming responses use
their 2xx headers as the confirmation point because the current copy pipeline
does not expose a distinct successful end-of-stream result. Concurrent success
and failure writes remain last-writer-wins for V1; timestamp-ordered health
transitions are deferred unless real concurrency evidence requires them.

Fourth-slice status: completed locally on 2026-07-21. Focused gateway,
Provider, and isolated PostgreSQL Store tests cover the confirmed-recovery
boundary, best-effort detached persistence, credential-only refresh behavior,
and recovery-event deduplication. Account-expiry and circuit templates remain
the next independent slices.

The fifth slice closes the remaining health-clear paths before those templates
ship. Updating an API-upstream key, base URL, or proxy and reauthorizing an
existing OAuth account replace credentials or configuration but preserve the
account's current health fields. They do not emit `provider_account.recovered`;
the operator must run an account test, complete a refresh probe, or explicitly
reset local status before an expired or open-circuit account becomes schedulable
again. This prevents configuration writes from masquerading as upstream health
confirmation.

`Reset local status` remains the intentional escape hatch. When it actually
clears unhealthy fields, its single transaction keeps the audit
`provider_account.status_reset` event and also emits one Runtime
`provider_account.recovered` event with `confirmation: operator_reset`. A reset
of an already healthy account writes the audit event only. This operator
override is the sole non-2xx recovery path and must be explicit in the manual;
automatic credential writes never use it. Negative provider Runtime transitions
(`expired`, `circuit.opened`, and `rate_limited`) use failure outcome, while
confirmed or operator-overridden recovery uses success outcome.

Focused Provider and isolated PostgreSQL Store tests must prove that API
credential edits and OAuth reauthorization preserve status, reason, last-error,
failure-count, and blocking windows; that the reset audit and recovery events
commit together only for a real transition; and that event action, category,
severity, outcome, target, and bounded metadata are exact. Once this slice is
complete, add `provider-account-expired-v1` and
`provider-account-circuit-open-v1` as separate disabled, target-scoped template
commits. Routing exhaustion remains deferred until a bounded persistent
aggregator emits dedicated trigger and recovery System Events.

Fifth-slice status: completed locally on 2026-07-21. Focused Provider and
isolated PostgreSQL Store tests cover preserved health during OAuth
reauthorization and API-upstream configuration changes, transactional operator
reset recovery, and failure outcomes for negative Runtime transitions. The
account-expiry and circuit-open templates are now unblocked as separate slices.

The sixth slice adds `provider-account-expired-v1` as a disabled Runtime warning
template. The first `provider_account.expired` event fires for its Provider
account target, a 24-hour cooldown limits repeat notifications, and
`provider_account.recovered` for the same target ends the incident and can send
a recovery notification. Target-scoped deduplication keeps unrelated accounts
independent. Catalog, matcher, service, Store, HTTP, and documentation tests
cover exact fields, trigger/cooldown/recovery transitions, stable order, and
idempotent installation. No migration or automatic rule installation is added.

Sixth-slice status: completed locally on 2026-07-21. The template is additive,
explicitly installed by an owner, and disabled until that owner reviews and
enables it.

The seventh slice adds `provider-account-circuit-open-v1` as a disabled Runtime
warning template. The first `provider_account.circuit.opened` event fires for
its Provider account target, a one-hour cooldown limits repeat notifications,
and `provider_account.recovered` for the same target ends the incident and can
send a recovery notification. Target-scoped deduplication keeps unrelated
accounts independent. Catalog, matcher, service, Store, HTTP, and documentation
tests cover exact fields, trigger/cooldown/recovery transitions, stable order,
and idempotent installation. No migration or automatic rule installation is
added.

Seventh-slice status: completed locally on 2026-07-21. The template is additive,
explicitly installed by an owner, and disabled until that owner reviews and
enables it. Routing exhaustion remains deferred because it has no dedicated
persistent trigger and recovery System Events.

The eighth slice projects API Key budget threshold crossings from the existing
rolling request-log aggregates. It covers all six configured budget streams:
request count, token count, and estimated cost over 24-hour and 30-day windows.
Each configured stream maintains independent 80-percent and 100-percent
crossings. A stream that moves directly from below 80 percent to at least 100
percent opens both incidents; falling below 100 percent recovers only the
100-percent incident, and falling below 80 percent recovers the remaining one.
The crossing boundary is `used >= ceil(limit * threshold / 100)`, while a zero
limit means the stream is not configured.

Migration `00044` adds `api_key_budget_threshold_states`, containing only
currently crossed rows keyed by API Key, budget kind, window, and threshold.
The monitor starts immediately with the server, repeats every five minutes, and
processes at most 100 keys per cycle with an ID cursor. It always runs so an
installed rule cannot silently lose its source signal behind a second feature
gate. Keys with configured budgets or existing crossing state are eligible.
Each key is evaluated in one transaction that locks the key row, aggregates no
more than its rolling 30-day Request Logs, diffs persisted crossings, and writes
state changes and System Events atomically. Multiple instances therefore
serialize on the key row; a failed event write rolls back the crossing state and
can be retried in the next cycle. Server shutdown cancellation emits no failure
event, and other cycle failures produce only a sanitized log before retrying.

The trigger actions are `api_key.budget.threshold_80.crossed` and
`api_key.budget.threshold_100.crossed`; their recovery actions use the same
prefix with `.recovered`. The 80-percent trigger is Runtime warning/partial, the
100-percent trigger is Runtime error/failure, and both recoveries are Runtime
info/success. Every event targets `client_api_key_budget` with ID
`<key-id>:<request|token|cost>:<24h|30d>` and the API Key name. Safe metadata is
limited to `client_key_id`, `budget_kind`, `window`, `threshold_percent`,
`used`, and `limit`, plus `confirmation` on key revocation; it never includes
the key prefix or secret, request data, or error text. The event message names
the kind, window, and threshold so
notification destinations remain useful even though alert payloads do not
include metadata.

Revoking an API Key closes and deletes every active crossing in the existing
revoke transaction, emitting the matching recoveries with
`confirmation: key_revoked`. This prevents physical key cleanup from cascading
away source state while leaving alert-rule state firing. Setting a budget to
zero or raising it above current use recovers on the next monitor cycle. Disabled
keys continue to age out naturally because their rolling usage can still fall
below the thresholds.

After the source-event slice, add two separate default-disabled templates.
`api-key-budget-80-percent-v1` matches the 80-percent trigger and recovery with
target deduplication, one-event aggregation, no aggregation window, a 24-hour
cooldown, and recovery notifications. `api-key-budget-100-percent-v1` uses the
same shape for the 100-percent actions with a one-hour cooldown. Installing a
template remains explicit and idempotent, and no rule or outbound delivery is
enabled automatically.

Focused migration and PostgreSQL tests must cover six-stream aggregation,
integer boundaries, direct jumps to 100 percent, independent downgrade
recoveries, unchanged-state idempotence, transaction rollback, two concurrent
evaluators, cursor bounds, zeroed budgets, and revoke recovery. Runner and main
wiring tests must prove immediate execution, the five-minute cadence, bounded
cycles, cancellation, cursor wrap, and sanitized logging. System Event tests
must assert exact actions, category, severity, outcome, target, and metadata.
Each template then requires the existing catalog, matcher, service, Store, HTTP,
manual, and documentation-contract coverage for ordering, cooldown, recovery,
target isolation, and idempotent installation.

Eighth-slice source-event status: completed locally on 2026-07-21. Migration 44,
the bounded always-on monitor, transactional crossing state, exact Runtime
events, revoke recovery, and main wiring are covered by focused unit and isolated
PostgreSQL tests. The two templates are tracked below as independent commits.

The ninth slice adds `api-key-budget-80-percent-v1` as a disabled Runtime
warning template. The first 80-percent crossing fires independently for each
`client_api_key_budget` target, a 24-hour cooldown limits repeats, and the exact
80-percent recovery action ends only that stream's incident. Catalog, matcher,
service, Store, HTTP, and documentation tests cover exact fields, target
isolation, cooldown, recovery, stable order, and idempotent installation.

Ninth-slice status: completed locally on 2026-07-21. Installation remains
explicit and the created rule starts disabled.

The tenth slice adds `api-key-budget-100-percent-v1` as a disabled Runtime error
template. The first exhausted crossing fires independently for each
`client_api_key_budget` target, a one-hour cooldown limits repeats, and only the
exact 100-percent recovery action ends that incident. The 80-percent recovery
action cannot close it. Catalog, matcher, service, Store, HTTP, and documentation
tests cover exact fields, target isolation, cooldown, recovery isolation, stable
order, and idempotent installation.

Tenth-slice status: completed locally on 2026-07-21. Installation remains
explicit and the created rule starts disabled.

The eleventh source-event slice projects per-API-Key routing exhaustion from
Request Logs. A trigger row must have
`routing_pool_error = 'routing_pool_exhausted'` and a non-null client key. A
firing incident recovers only after a later log for the same key records a real
upstream `2xx`, a non-null `provider_account_id`, and an empty
`routing_pool_error`. Local `/v1/models` responses, no-traffic periods, budget or
rate-limit rejections, and every other failure do not recover it. The alert
target is the API Key rather than a routing pool because a successful fallback
log identifies the pool that actually served the request, not necessarily the
pool whose chain was previously exhausted.

Migration `00045` adds one persisted `routing_exhaustion_v1` Request Log
checkpoint and an `api_key_routing_exhaustion_states` table containing only
currently firing keys. The migration initializes the checkpoint to the current
maximum Request Log ID so an upgrade does not replay retained history or create
stale incidents; only logs committed after migration 45 are projected. The
always-on monitor starts with the server, runs immediately and once per minute,
reads at most 1000 log rows in ID order, and emits at most 100 transitions per
cycle. It stops before a row that would exceed the transition bound and resumes
from the last committed checkpoint. A PostgreSQL transaction advisory lock
allows only one instance to project a cycle at a time. A short Request Log table
`SHARE ... NOWAIT` lock also prevents sequence IDs from committing out of order
across the checkpoint by capturing a committed safe maximum ID, then releases
the table lock before projecting `checkpoint < id <= safe-max`. Either form of
lock contention is a normal skip. The migration takes the blocking equivalent
before establishing its non-replay baseline.

Checkpoint movement, firing-state insert/delete, and Runtime System Events
commit in one transaction. A failed event write therefore rolls back the
transition and checkpoint together. Trigger events use Runtime error/failure
action `api_key.routing_pool.exhausted`; recovery events use Runtime info/success
action `api_key.routing_pool.recovered`. Both target `client_api_key/<key-id>`
with the current key name. Trigger metadata is limited to `client_key_id`,
`request_log_id`, `routing_pool_id`, and `fallback_depth`; recovery metadata is
limited to `client_key_id`, `request_log_id`, and `provider_account_id`. No model,
request or response content, error text, pool name, key prefix, or secret is
copied into the event.

Revoking an API Key closes an active routing-exhaustion state in the existing
revoke transaction and emits `api_key.routing_pool.recovered` with
`confirmation: key_revoked`. This prevents the state-table foreign-key cascade
from leaving the matching alert-rule state firing after physical cleanup.

Eleventh source-event status: completed locally on 2026-07-21. The migration,
bounded projector, revoke recovery, startup wiring, and isolated PostgreSQL
coverage are in place. Alert delivery now establishes its PostgreSQL `LISTEN`
subscription before either source-event monitor can emit its startup cycle. The
default template remains the next independent slice.

After the source events exist, add `routing-pool-exhausted-v1` in an independent
commit. The template starts disabled, matches the first Runtime error trigger,
uses target-scoped deduplication and a one-hour cooldown, and can notify on the
exact recovery action. Installation remains explicit and idempotent.

Twelfth-slice status: completed locally on 2026-07-21. Catalog, matcher,
service, Store, HTTP, manual, and documentation-contract coverage assert exact
fields, key-target isolation, cooldown, recovery, stable order, and idempotent
installation.

Focused migration and isolated PostgreSQL tests must cover the non-replay
baseline, ordered checkpoint advancement, the 1000-row and 100-transition
bounds, lock contention, unchanged firing idempotence, exact recovery predicates,
different-key isolation, transaction rollback, and revoke recovery. Runner and
main tests must prove immediate execution, one-minute cadence, cancellation,
cursor-free restart from the database checkpoint, and sanitized failure logs.
System Event, catalog, matcher, service, Store, HTTP, manual, and documentation
tests must assert exact fields, cooldown/recovery behavior, stable template
order, and idempotent installation.

The fallback-window slice remains owner-blocked until its product thresholds
are selected. Request Logs already persist `gateway_attempt_count` and
`gateway_fallback_count`, but the latter measures fallback pressure: it can
include a final concurrency-full selection that did not successfully move to
another account. A future rule must therefore avoid describing it as a
successful-switch rate and must explicitly choose its rolling window, minimum
sample size, trigger threshold, recovery hysteresis, and API-Key target
semantics. The current implementation must not silently adopt provisional
values for those policy decisions.

The thirteenth source-event slice closes API Key physical-cleanup failures. The
existing hourly cleanup already writes
`scheduler.api_key_purge.completed` transactionally after every successful
cycle, including zero-row cycles. A failed cycle emits the new Scheduler
error/failure action `scheduler.api_key_purge.failed` after the failed purge
transaction has rolled back. Both actions use the stable
`client_api_key_collection` target so the next successful cycle is an exact
recovery for the same incident. Parent-context cancellation during shutdown
does not emit a failure event.

The failure event is best effort because the System Event store shares
PostgreSQL with the cleanup transaction. It contains no database error text,
SQL, key identity, prefix, or secret. Its fixed error code is
`api_key_purge_failed`, its message is static, and its only metadata is the
scheduled retention duration in days. Event-recording failure is logged with a
fixed error code rather than the storage error. Focused runner tests must prove
immediate execution, interval behavior, success and failure paths, cancellation
suppression, exact event fields, and sanitized logs. Existing Store tests must
continue proving that a successful purge and its completion event commit
atomically.

Thirteenth source-event status: completed locally on 2026-07-21. The hourly
runner emits the exact sanitized failure event on purge errors, suppresses
shutdown cancellation, and keeps successful completion ownership in the Store
transaction. Focused tests cover immediate and interval execution, exact event
fields, cancellation, failure-event recording errors, and log sanitization.

After that source event exists, add `api-key-purge-failed-v1` in an independent
commit. The template starts disabled, fires on the first cleanup failure, uses
target-scoped deduplication and a 24-hour cooldown, and recognizes
`scheduler.api_key_purge.completed` as recovery with recovery notifications
enabled. Catalog, matcher, service, Store, HTTP, manual, and documentation
tests must cover exact fields, cooldown, recovery, stable order, and idempotent
installation.

### Completion Criteria

Every event has trigger, aggregation, cooldown, recovery, and test coverage.

### Commit

`feat(alerting): add <signal> notification rule`
