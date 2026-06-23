# Persistent Sticky Sessions Design

## Goal

Persist gateway sticky-session routing decisions so the same `session_id` keeps using the same provider account while that account remains schedulable for the requested model.

## Context

N2API already accepts sticky session IDs from POST body `session_id`, `session_id` header, and `X-N2API-Session-ID`. The current selector hashes the session ID into the highest-priority candidate group. That is deterministic for a stable candidate set, but the binding can move when account health, last-use ordering, or pool membership changes.

sub2api's gateway behavior treats scheduling state as operational data. N2API should keep the useful personal-gateway part: remember the account chosen for a client workspace/session, without adding users, billing, channel groups, Redis, or multi-tenant routing.

## Scope

In scope:

- Add a PostgreSQL table for provider session bindings.
- Bind by provider, model, and session ID so one session can target different accounts for different models.
- Use the persisted binding before hash-based sticky ordering when the bound account is still enabled, healthy, model-capable, and not explicitly excluded by gateway fallback.
- Create or update the binding after a selected account is successfully prepared for use.
- Rebind to a fallback account when the previously bound account is excluded or unschedulable before streaming starts.
- Show persisted sticky binding status in routing preview candidates and documentation.

Out of scope:

- Redis or distributed sticky-session storage.
- User-level tenant routing, account groups, billing, balances, or public SaaS controls.
- Admin CRUD for deleting individual bindings in this slice.
- Binding after response streaming has started; the existing no-retry-after-streaming rule remains unchanged.

## Data Model

Add `provider_session_bindings`:

- `id BIGSERIAL PRIMARY KEY`
- `provider TEXT NOT NULL`
- `model TEXT NOT NULL`
- `session_id TEXT NOT NULL`
- `account_id BIGINT NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE`
- `last_used_at TIMESTAMPTZ NOT NULL DEFAULT now()`
- `created_at TIMESTAMPTZ NOT NULL DEFAULT now()`
- `updated_at TIMESTAMPTZ NOT NULL DEFAULT now()`

Constraints:

- unique `(provider, model, session_id)`
- non-empty `model`
- non-empty `session_id`
- index `(provider, account_id)`

The service stores the exact trimmed session ID. It does not hash the value yet because session IDs already appear in request logs and admin diagnostics.

## Provider Selection

`SelectAccountForModelAndSession` should:

1. Trim `model` and `sessionID`.
2. Load normal schedulable candidates with existing health, model, exclusion, priority, load-factor, and last-use rules.
3. If a persisted binding exists for `(provider, model, sessionID)` and that account is in the schedulable candidate list, move that account to the front.
4. Otherwise use the existing hash-based sticky order inside the highest eligible group.
5. Select the first account that can produce credentials.
6. Upsert the binding to the selected account after credential preparation succeeds.

Fallback keeps the same shape. When the gateway excludes a failed or concurrency-capped account and calls `SelectAccountForModelAndSession` again, the old binding is ignored if the account is excluded, and the successful fallback account replaces it.

`SelectAccountForModel` without a session ID remains unchanged and does not create bindings.

## Routing Preview

`PreviewAccountSelection` should use the same persisted binding preference, but it must not mutate binding rows or account `last_used_at`. Candidate rows should include whether the selected account came from a persisted binding. A minimal API-compatible addition is acceptable:

- `SelectionPreview.StickyBoundAccountID`
- `SelectionCandidate.StickyBound`

The UI can display this as a small `Sticky bound` marker in Selection preview. If no stored binding exists, preview continues to show the hash-derived selected account.

## Error Handling

If binding lookup fails because the database is unavailable, selection returns the error instead of silently routing differently. This is consistent with other provider repository failures.

If binding upsert fails after credentials are prepared, selection returns an error before the gateway sends an upstream request. This avoids claiming sticky behavior that was not persisted.

## Documentation

README and deploy notes should say:

- sticky sessions are persisted by provider, model, and `session_id`;
- a healthy bound account is reused while it remains schedulable;
- fallback before streaming can rebind the session to another eligible account.

## Verification

Required gates:

- migration tests prove the table, unique key, and rollback exist;
- provider service tests prove first selection creates a binding, next selection reuses it despite account ordering changes, fallback excludes and rebinds, and preview reports a stored binding without mutation;
- HTTP/API tests prove model routing preview includes sticky binding fields;
- frontend source tests prove Selection preview renders the sticky-bound marker;
- documentation tests prove README/deploy notes mention persisted sticky bindings;
- `go test ./...`;
- `bun test src/routes/navigation.test.mjs`;
- `bun run check`;
- `bun run build`.
