# API Key Rate Window Visibility Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show current API-key request and token minute-window usage in the admin API Keys surface.

**Architecture:** The gateway request and token limiters expose mutex-protected read-only snapshots for active fixed-minute windows. The admin HTTP layer combines those counts with per-key overrides and gateway defaults. The Svelte API Keys page renders the new runtime fields next to existing per-key limit controls.

**Tech Stack:** Go backend, existing net/http admin API, SvelteKit admin UI, Bun tests.

---

### Task 1: Gateway Rate Snapshot

**Files:**
- Modify: `backend/internal/gateway/proxy.go`
- Modify: `backend/internal/gateway/proxy_test.go`

- [ ] Write failing tests for request and token window snapshots.
- [ ] Run targeted gateway tests and confirm they fail because snapshot methods are missing.
- [ ] Add `APIKeyRequestRateSnapshot()` and `APIKeyTokenRateSnapshot()` on `Proxy`.
- [ ] Add `Snapshot()` methods on `apiKeyRateLimiter` and `apiKeyTokenLimiter` that copy active-window counts and omit stale windows.
- [ ] Run targeted gateway tests until they pass.
- [ ] Commit with `feat: expose api key rate window snapshots`.

### Task 2: Admin API Response

**Files:**
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/server_test.go`

- [ ] Write failing HTTP test for `GET /api/admin/keys` rate-window fields.
- [ ] Run targeted HTTP test and confirm it fails because fields are absent.
- [ ] Add `APIKeyRateSnapshotProvider` optional interface.
- [ ] Enrich `apiKeyResponse` with current/effective/remaining/blocked request and token window fields.
- [ ] Run targeted HTTP tests until they pass.
- [ ] Commit with `feat: expose api key rate window state`.

### Task 3: API Keys UI

**Files:**
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/api-keys/+page.svelte`
- Modify: `frontend/src/routes/navigation.test.mjs`
- Modify: `frontend/src/routes/providers/provider-page.test.mjs`

- [ ] Write failing frontend source tests for rate-window fields and labels.
- [ ] Run targeted Bun source tests and confirm they fail because the UI lacks those labels.
- [ ] Add API key typedef fields.
- [ ] Add a small label helper for effective rate-window limits.
- [ ] Render request and token window readouts and full markers near existing limit controls.
- [ ] Run targeted Bun source tests until they pass.
- [ ] Commit with `feat: show api key rate windows`.

### Task 4: Documentation And Verification

**Files:**
- Modify: `README.md`
- Modify: `backend/internal/gateway/documentation_test.go`

- [ ] Write failing documentation test for process-local API-key rate-window visibility.
- [ ] Run targeted documentation test and confirm it fails.
- [ ] Update README runtime-limit documentation.
- [ ] Run backend and frontend verification commands.
- [ ] Commit with `docs: document api key rate windows`.
