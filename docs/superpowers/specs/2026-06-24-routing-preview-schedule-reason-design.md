# Routing Preview Schedule Reason Design

## Goal

Show why Routing diagnostics ranks each schedulable provider account in its current order, so gateway scheduling decisions are inspectable without sending traffic.

## Context

N2API already supports provider account priority, load factor, least-recently-used ordering, sticky session bindings, account health filtering, model capability filtering, runtime concurrency diagnostics, and blocked-candidate reasons. The current Routing diagnostics preview shows `Schedule rank`, priority, load, last-used time, active concurrency, sticky markers, and unschedulable reasons, but it does not summarize the ranking reason in one operator-readable field.

sub2api-style scheduling management is useful because it explains why a gateway selected a channel/account. N2API should keep the useful diagnostic behavior while staying a personal gateway: no channel groups, billing, tenant routing, Redis, or new scheduling policy engine.

## Scope

In scope:

- Add a `scheduleReason` string to `SelectionCandidate`.
- Populate it for schedulable candidates in preview responses.
- Keep blocked candidates using `unschedulableReason`; they do not need `scheduleReason`.
- Show `scheduleReason` in the Routing diagnostics candidate chips.
- Document that schedule reasons are diagnostic text and do not change scheduler behavior.

Out of scope:

- Changing the scheduler order.
- Adding configurable scheduling policies.
- Adding account groups or tenant-specific routing.
- Persisting schedule explanations.

## Backend Behavior

For schedulable candidates:

- Sticky-bound selected accounts report `sticky session binding`.
- Selected accounts without a stored sticky binding report `selected by priority, load factor, and least-recently-used order`.
- Other schedulable accounts report `ordered by priority, load factor, and least-recently-used order`.

The field is intentionally concise. Detailed numeric factors are already present on the candidate object as priority, load factor, last-used time, and runtime concurrency.

## Frontend Behavior

Routing diagnostics candidate chips show the schedule reason beside rank, priority, load, active concurrency, and last-used time. The existing blocked-candidate reason remains unchanged.

## Verification

Required gates:

- provider service test proves preview candidates include schedule reasons for selected, sticky-bound, and non-selected schedulable candidates;
- HTTP preview enrichment preserves the new field;
- frontend source tests prove models page and admin state know `scheduleReason`;
- documentation test proves README and deploy notes mention schedule reasons;
- `go test ./internal/provider -run PreviewAccountSelection`;
- `go test ./internal/httpapi -run PreviewAccountSelection`;
- `bun test src/routes/navigation.test.mjs src/routes/providers/provider-page.test.mjs`;
- `bun run check`;
- `bun run build`.
