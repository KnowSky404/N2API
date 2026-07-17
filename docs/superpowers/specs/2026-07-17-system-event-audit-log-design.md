# System Event and Audit Log Design

Date: 2026-07-17

## Goal

Add a durable, searchable system event log that answers:

- who performed an administrative or security-sensitive action;
- what resource and action were involved;
- whether the action succeeded, failed, or partially succeeded;
- when it happened and how long it took;
- why an OAuth refresh, scheduler cycle, or runtime account-state change failed.

This log complements `request_logs`. It does not replace gateway request usage
accounting or provider account test history.

## Design Principles

1. **Complete attribution:** administrative events carry a stable actor snapshot,
   target snapshot, outcome, timestamp, and correlation ID.
2. **No secret capture:** never store request or response bodies, credentials,
   tokens, cookies, authorization codes, OAuth state, PKCE material, or arbitrary
   headers.
3. **Domain events over HTTP access logs:** record meaningful actions in service
   boundaries so legacy route aliases, schedulers, and gateway-triggered refreshes
   produce one correctly classified event.
4. **Atomic success records:** a successful state-changing operation and its audit
   event commit in the same PostgreSQL transaction. A missing success audit event
   must not be reported as a successful sensitive mutation.
5. **Separate failure records:** rejected security actions and rolled-back domain
   operations write a fixed-code, fixed-template failure event in a separate
   best-effort insert.
6. **Append-only application contract:** application code can insert and query
   events. It cannot edit existing events. Deletion is available only through the
   retention workflow.
7. **Bounded operational cost:** use typed columns for common filters, keyset
   pagination, conservative indexes, retention, and aggregate scheduler events.
8. **Honest integrity claim:** the first version is append-only at the application
   layer, not tamper-proof against the PostgreSQL owner or host administrator.

## Scope

In scope:

- authenticated admin mutations;
- login, logout, password changes, rejected sessions, API key secret reads, and
  log exports;
- OAuth authorization, callback, manual refresh, and automatic lazy refresh;
- provider account scheduler cycles and API key purge jobs;
- provider account circuit, rate-limit, expiry, and recovery transitions;
- filtered admin API and a dedicated responsive `System logs` page;
- configurable retention with a safe default;
- tests proving event coverage and secret exclusion.

Out of scope:

- logging ordinary authenticated reads and health checks;
- duplicating every model request already present in `request_logs`;
- storing full before/after objects or arbitrary request JSON;
- external SIEM shipping, cryptographic hash chains, or WORM storage;
- billing, multi-tenant actor models, or public-user audit trails.

## Event Taxonomy

### Categories

| Category | Purpose | Examples |
| --- | --- | --- |
| `audit` | Human-initiated configuration and lifecycle changes | API key create, provider update, routing pool delete |
| `security` | Authentication and sensitive access | login failure, password change, secret view, export |
| `oauth` | OAuth credential lifecycle | connect, callback, manual or automatic refresh |
| `scheduler` | Background cycle summaries | provider auto test, expired API key purge |
| `runtime` | Important automatic state transitions | circuit opened, rate limited, recovered |

### Outcomes and severity

- `outcome`: `success`, `failure`, or `partial`.
- `severity`: `info`, `warning`, or `error`.
- Expected validation or authentication rejection is normally `warning`.
- Infrastructure failure and failed credential refresh are `error`.
- Successful changes and normal scheduler completion are `info`.

### Action naming

Actions use stable dot-separated identifiers. Display labels are resolved by the
frontend so stored events remain language-neutral and queryable.

Representative actions:

- `auth.login.succeeded`, `auth.login.failed`, `auth.session.rejected`
- `auth.logout.succeeded`, `auth.password.changed`
- `api_key.created`, `api_key.revoked`, `api_key.deleted`
- `api_key.secret.viewed`, `api_key.model_policy.updated`
- `routing_pool.created`, `routing_pool.accounts.replaced`
- `provider_account.created`, `provider_account.updated`,
  `provider_account.disconnected`
- `provider_account.models.replaced`, `provider_account.models.synced`
- `oauth.connect.started`, `oauth.callback.completed`
- `oauth.refresh.manual.succeeded`, `oauth.refresh.automatic.failed`
- `scheduler.provider_account_auto_test.completed`
- `scheduler.api_key_purge.completed`
- `provider_account.circuit.opened`, `provider_account.recovered`

The implementation must keep an explicit action catalog in code. Unknown actions
are rejected instead of silently creating inconsistent names.

## PostgreSQL Model

Add migration `00036_system_events.sql`:

```sql
CREATE TABLE IF NOT EXISTS system_events (
    id BIGSERIAL PRIMARY KEY,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    category TEXT NOT NULL,
    severity TEXT NOT NULL,
    action TEXT NOT NULL,
    outcome TEXT NOT NULL,
    actor_type TEXT NOT NULL,
    actor_id BIGINT,
    actor_name TEXT NOT NULL DEFAULT '',
    target_type TEXT NOT NULL DEFAULT '',
    target_id TEXT NOT NULL DEFAULT '',
    target_name TEXT NOT NULL DEFAULT '',
    correlation_id TEXT NOT NULL,
    source_ip INET,
    http_method TEXT NOT NULL DEFAULT '',
    route_pattern TEXT NOT NULL DEFAULT '',
    status_code INTEGER,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    error_code TEXT NOT NULL DEFAULT '',
    message TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    CONSTRAINT system_events_category_check
        CHECK (category IN ('audit', 'security', 'oauth', 'scheduler', 'runtime')),
    CONSTRAINT system_events_severity_check
        CHECK (severity IN ('info', 'warning', 'error')),
    CONSTRAINT system_events_outcome_check
        CHECK (outcome IN ('success', 'failure', 'partial')),
    CONSTRAINT system_events_actor_type_check
        CHECK (actor_type IN ('admin', 'system')),
    CONSTRAINT system_events_correlation_id_check
        CHECK (correlation_id <> ''),
    CONSTRAINT system_events_status_code_check
        CHECK (status_code IS NULL OR status_code BETWEEN 100 AND 599),
    CONSTRAINT system_events_duration_check CHECK (duration_ms >= 0),
    CONSTRAINT system_events_metadata_object_check
        CHECK (jsonb_typeof(metadata) = 'object')
);
```

`actor_id` deliberately has no foreign key. Audit attribution must survive admin
deletion. `target_id` is text because targets include numeric rows, singleton
settings, collections, OAuth state attempts, and batches. Names are snapshots and
must not require joining a resource that may have been deleted.

Indexes follow the actual filter and pagination paths:

```sql
CREATE INDEX IF NOT EXISTS system_events_occurred_id_idx
    ON system_events (occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS system_events_category_occurred_id_idx
    ON system_events (category, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS system_events_action_occurred_id_idx
    ON system_events (action, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS system_events_target_occurred_id_idx
    ON system_events (target_type, target_id, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS system_events_non_success_idx
    ON system_events (occurred_at DESC, id DESC)
    WHERE outcome <> 'success';
```

Do not add a general GIN index to `metadata`; metadata is for bounded drill-down,
not the primary query interface. Add another typed column before promoting a JSON
key into a frequent filter.

## Event Context and Recording

Introduce a small `systemevent` package with:

- typed constants for category, severity, outcome, actor type, and action;
- `Actor`, `RequestContext`, `Target`, and `Event` types;
- `WithRequestContext` and `FromContext` helpers;
- allowlisted metadata builders for each domain;
- length limits and a final secret-key rejection guard.

The HTTP layer creates a correlation ID for every request, returns it as
`X-Request-ID`, and puts the matched `Request.Pattern`, remote IP, method, and
authenticated admin snapshot into the context. Forwarded headers are ignored until
a trusted-proxy configuration exists; otherwise they allow actor IP spoofing.

Non-HTTP producers generate a fresh cryptographically random correlation ID. A
scheduler cycle or batch creates one root correlation ID and reuses it for its
related per-target events. An incoming request ID is accepted only after strict
character and length validation; otherwise N2API replaces it.

Admin and provider services create a semantic `EventIntent` and attach it to the
operation context. The concrete store mutation method reads that intent, enriches it
with the final target snapshot obtained from `RETURNING` or the row locked by its
own transaction, and inserts the event through that same `pgx.Tx` before commit.
The standalone event repository is only for queries, retention, and failure or
HTTP-only events; it must not be called by a service after a successful business
repository call. Route middleware is a safety net for authentication rejection and
missing action coverage, not the source of successful domain audit events.

This context-based intent keeps the existing narrow repository interfaces usable by
tests while making atomic event handling the responsibility of the PostgreSQL store.
A coverage test enumerates every audited store mutation and fails when a method does
not consume an intent or explicitly document why it is not auditable.

Login session creation, logout session revocation, password changes, and bootstrap
admin create/username update follow the same rule: their success event is inserted
inside the admin repository transaction. The HTTP layer supplies request context
and records only rejected or failed attempts that did not commit a business change.

For a domain event, `duration_ms` measures the service operation through its commit.
`status_code` is nullable because the final wire-level status and full handler
duration do not exist yet when the business transaction commits. HTTP-only security
events may carry the observed response status. This system does not duplicate a
general HTTP access log.

OAuth refresh calls must pass an explicit trigger:

- `manual`
- `gateway_request`
- `account_test`
- `model_test`
- `model_sync`
- `status_probe`

Automatic refresh events include only safe metadata such as trigger, previous and
new expiry timestamps, failure count, circuit-open deadline, and duration. They do
not include any token, raw OAuth payload, response body, authorization URL, code,
state, or verifier.

## Batch Semantics

Current provider batch handlers execute one target at a time and can stop after a
partial mutation. Every successful per-target mutation must commit its own event in
the same target transaction. All per-target events share a `batch_id` in allowlisted
metadata. After processing, the batch writes a summary event containing:

- requested target count;
- attempted target count;
- succeeded and failed counts;
- skipped count for targets not attempted after the first failure;
- bounded arrays of numeric target IDs;
- the first fixed-catalog error code;
- `partial` outcome when at least one target changed and another failed.

V1 preserves the existing stop-on-first-error behavior and HTTP error mapping.
Therefore `attempted = succeeded + failed` and
`skipped = requested - attempted`. A fully successful batch returns its existing
success response. A failed or partial batch returns the existing mapped error while
the system event retains the accurate counts and IDs.

The summary does not replace per-target events: a crash after target N must still
leave audit evidence for targets 1 through N. The UI groups matching `batch_id`
events under their summary by default to avoid visual double counting. The existing
provider test history remains the per-account diagnostic detail source for auto-test
cycles; system events describe the administrative batch action and outcome.

## Sensitive Data Policy

Never persist:

- passwords or password hashes;
- session cookies, session tokens, or token hashes;
- client API keys or encrypted API keys;
- OAuth access, refresh, or ID tokens;
- OAuth code, state, nonce, code verifier, or code challenge;
- `Authorization`, `Cookie`, or `Set-Cookie` headers;
- arbitrary request/response bodies or query strings;
- complete proxy URLs when they can contain credentials;
- raw upstream error bodies.

Allowed metadata is action-specific and uses field names such as
`changed_fields`, `model_count`, `deleted_count`, `retention_days`, `trigger`,
`previous_expiry`, `new_expiry`, and `circuit_open_until`. Credential changes are
represented only as booleans such as `api_key_changed: true`.

Persisted `error_code` values come from a fixed catalog. Persisted messages come
from action-specific templates and never from `err.Error()`, an upstream message,
status reason, response fragment, or regex-based redaction attempt. Raw errors may
go only to structured `slog` after applying the same secret-safe logging policy.

Messages are single-line and capped at 500 characters. Metadata is capped at 8 KiB
after JSON encoding. Target and actor names are capped at 128 characters.
Correlation and error codes are capped at 100 characters. Tests inject canary
secrets and assert that no event text field or metadata value contains them.

Sensitive reads such as API key secret retrieval and log export insert their
security event before returning the protected data. If that event cannot be stored,
the read fails closed and no secret or export body is returned.

## Admin API

Add:

`GET /api/admin/system-events`

Query parameters:

- `limit`: 1-100, default 50;
- `cursor`: opaque URL-safe keyset cursor for the next page;
- `since`: Unix timestamp;
- `category`: one supported category;
- `outcome`: one supported outcome;
- `severity`: one supported severity;
- `action`: exact action identifier;
- `actor`: case-insensitive actor-name search;
- `targetType` and `targetId`;
- `q`: bounded search over action, actor snapshot, target snapshot, error code,
  and message.

Response:

```json
{
  "events": [],
  "nextCursor": "opaque-cursor",
  "hasMore": true
}
```

The endpoint returns `limit + 1` rows and removes the extra row to compute
`hasMore`. It orders by `occurred_at DESC, id DESC`. The opaque cursor encodes and
authenticates the last `(occurred_at, id)` pair, so retention deleting the cursor row
does not invalidate pagination. Invalid cursors return `400` and the frontend resets
to the first page.

No generic event-creation endpoint is exposed. The browser cannot fabricate audit
events.

## Retention

- Add `N2API_SYSTEM_EVENT_RETENTION_DAYS`, default `365`.
- Accept `0` to disable automatic deletion and `30..3650` for enabled retention.
- Run cleanup at startup and every 24 hours.
- Delete in bounded batches to avoid a long transaction and table bloat.
- Record one `scheduler.system_event_retention.completed` event after cleanup,
  containing the cutoff and deleted count.
- Retention summary events are best-effort operational telemetry, explicitly exempt
  from the atomic sensitive-mutation guarantee because the table is maintaining
  itself. A process crash can omit one cleanup summary without erasing retained
  business audit events.
- A future export endpoint must itself emit a `security.system_event.exported`
  event. V1 UI does not need export unless explicitly added to scope.

## UI Design

Add `/system-logs` and a `System logs` navigation item beside `Request Logs` and
`Ops`.

The page is an operational table, not a card dashboard:

- header with a quiet description and icon-only refresh action;
- compact filters for category, outcome, severity, time range, target, and search;
- active-filter summary and reset action;
- columns: `Time`, `Category`, `Action`, `Actor`, `Target`, `Outcome`, `Details`;
- an unframed detail modal for safe metadata, correlation ID, route pattern,
  status code, and duration;
- `Load older` keyset pagination rather than numbered client-side pages;
- clear empty, loading, stale-data, and error states;
- desktop horizontal containment and mobile stacked rows without dropping fields.

Category and outcome use restrained semantic text and icons. Do not use a rainbow
badge palette. Metadata renders as a definition list, not raw JSON by default.

URL-backed filters use a GET-form-compatible query string and client navigation in
the existing SSR-disabled SvelteKit application. Refresh and pagination must not
reset the active filters.

## Coverage Catalog

The implementation action catalog must cover these groups:

- admin login, logout, session rejection, password change, and bootstrap;
- API key create, secret view, rename, enable/disable, policy, limits, budgets,
  routing pool assignment, revoke, physical delete, and automatic purge;
- routing pool create, update, delete, and account replacement;
- request-log cleanup and export;
- gateway settings, model settings, and usage pricing mutations;
- fingerprint profile and error passthrough rule lifecycle;
- provider account create, update, disconnect, pause, reset, test, model test,
  model replacement, sync, and all batch variants;
- disconnecting all accounts for a provider as `provider_account.disconnect_all`,
  with bounded target IDs and a deleted count;
- OAuth connect, callback, manual refresh, and automatic lazy refresh;
- provider auto-test scheduler summaries;
- provider account circuit, rate-limit, expiry, and recovery transitions.

`ListAPIKeys` currently purges expired keys as a side effect. Implementation must
remove that read-path mutation and keep purge in startup/hourly jobs so event actor
and scheduler attribution remain correct.

## Acceptance Criteria

- Every supported action uses a typed catalog constant and has a test.
- Successful admin changes and their audit records commit atomically.
- Session creation/revocation, password changes, bootstrap changes, and API key purge
  are included in the transactional success guarantee.
- Failed or partial batch operations report accurate outcomes and target counts.
- Every committed batch target has a correlated per-target event even if the process
  stops before the batch summary is written.
- Automatic and manual OAuth refreshes are distinguishable in stored events.
- Deleting a resource does not erase its prior actor or target snapshots.
- Tests demonstrate that secrets, raw errors, and raw bodies never enter any event
  text field or metadata value.
- Query filters, deterministic keyset pagination, and retention boundaries work.
- The System logs page is usable at desktop and mobile widths and exposes all safe
  event detail without nested cards or clipped table content.
- Go tests, frontend tests, `bun run check`, `bun run build`, Docker Compose
  rebuild, authenticated browser checks, and container-local smoke checks pass.
