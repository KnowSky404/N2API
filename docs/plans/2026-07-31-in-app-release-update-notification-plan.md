# N2API In-App Release Update Notification Implementation Plan

Date: 2026-07-31
Specification: `docs/specs/2026-07-31-in-app-release-update-notification.md`

## Phase 1: Backend Release Checker

1. Add `backend/internal/updatecheck` with normalized public types, a bounded
   GitHub client, ETag support, commit comparison, snapshot caching, refresh
   cooldown, and a context-driven background loop.
2. Add deterministic `httptest` coverage for current, available, ahead,
   diverged, development, malformed, `304`, timeout, and stale-cache behavior.
3. Add `N2API_UPDATE_CHECK_ENABLED` to backend configuration and `.env.example`.
4. Inject the checker into the HTTP server and background supervisor.
5. Add authenticated `GET /api/admin/update-status` and `POST
   /api/admin/update-status/refresh` endpoints with session and response tests.

## Phase 2: Admin Notification UI

1. Add an update-status state model and authenticated load/refresh functions.
2. Add a global update affordance to the existing app shell and a responsive,
   accessible release-details modal.
3. Render release Markdown through a maintained parser and sanitizer, with raw
   HTML and unsafe link behavior blocked.
4. Store exact-version dismissal locally while keeping update details
   discoverable.
5. Extend Bun tests for state transitions, dismissal semantics, sanitization,
   and required shell structure.

## Phase 3: Documentation

1. Document the update-check environment switch and outbound GitHub request.
2. Document the distinction between an update notification and an authorized
   host upgrade.
3. Link the notice to the immutable-image, backup, restore-drill, plan/apply,
   and verification path already described in `docs/agent-operations.md`.

## Phase 4: Verification And Delivery

1. Run focused Go and Bun tests during implementation.
2. Run `make test` as the repository-wide managed gate.
3. Rebuild and recreate the local Compose stack using the required builder
   cache cleanup sequence.
4. Verify container state and container-local `/livez`, `/readyz`, `/version`,
   and admin UI responses.
5. Use Bunx Playwright for the authenticated update flow at desktop and mobile
   sizes because the Browser plugin is unavailable in this session.
6. Review the final diff and worktree; keep each implementation phase in an
   atomic Conventional Commit and do not push without explicit authorization.

