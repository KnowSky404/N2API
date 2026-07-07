# OpenAI Official Pricing Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Seed request-log pricing with current OpenAI official Standard token prices and add a one-click admin sync action.

**Architecture:** Keep the existing editable `UsagePricing` table as the durable source used for request-log cost estimates. Add a backend official-pricing fetch/parse/sync path that saves the same `UsagePricing` shape, and add a Request Logs pricing-panel button that calls it. Do not change provider-account upstream model sync.

**Tech Stack:** Go backend, PostgreSQL settings store, SvelteKit/Svelte 5 frontend, Bun tests, Docker Compose local stack.

---

## File Structure

- Modify `backend/internal/admin/service.go`: official pricing defaults, parser, sync method, fetcher injection.
- Modify `backend/internal/admin/service_test.go`: default-pricing tests, parser tests, sync save/error tests, memory repo support if needed.
- Modify `backend/internal/httpapi/server.go`: admin service interface and `POST /api/admin/usage-pricing/sync-official`.
- Modify `backend/internal/httpapi/server_test.go`: endpoint auth/success/error tests and fake service fields.
- Modify `frontend/src/lib/admin-state.svelte.js`: pricing sync state and `syncOfficialUsagePricing()`.
- Modify `frontend/src/routes/request-logs/+page.svelte`: button and inline success/error wiring.
- Modify `frontend/src/routes/navigation.test.mjs`: source-level UI/API assertions.

## Tasks

### Task 1: Backend Defaults And Parser

- [ ] Write failing admin service tests proving default pricing has non-zero official rates for representative models.
- [ ] Write parser tests using a compact Astro-props-like fixture containing Standard rows, unsupported rows, and missing cached-input cells.
- [ ] Implement a static official default table in `defaultUsagePricing()`.
- [ ] Implement parser helpers that convert dollars per 1M tokens to micro-USD per 1M tokens, strip context annotations from model names, skip unsupported rows, and dedupe rows.
- [ ] Run `GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/admin -run 'TestUsagePricing|Official' -count=1` from `backend/`.

### Task 2: Backend Sync API

- [ ] Add a small official pricing fetcher dependency to `admin.Service`, defaulting to `GET https://developers.openai.com/api/docs/pricing` with a bounded timeout.
- [ ] Add `SyncOfficialUsagePricing(ctx)` that fetches, parses, normalizes, saves, and returns a summary.
- [ ] Add `POST /api/admin/usage-pricing/sync-official` in `httpapi`.
- [ ] Test admin-required behavior, successful response shape, and invalid-source error mapping.
- [ ] Run focused admin and httpapi Go tests.

### Task 3: Frontend Sync Control

- [ ] Add `usagePricing.syncing`, `usagePricing.syncMessage`, and `syncOfficialUsagePricing()` to admin state.
- [ ] Add `Sync official` / `Syncing` button to the Request Logs pricing panel.
- [ ] On success, replace pricing rows with returned official rows and show `Synced official pricing for N models.`
- [ ] On failure, show an inline error without clearing edited rows.
- [ ] Update source-level frontend tests for the button, endpoint, and state transitions.
- [ ] Run `bun test`, `bun run check`, and `bun run build` from `frontend/`.

### Task 4: Final Verification And Delivery

- [ ] Run full backend tests from `backend/` with the local Go caches.
- [ ] Run full frontend tests/check/build.
- [ ] Review `git diff` for scope and generated artifacts.
- [ ] Commit implementation as a single Conventional Commit.
- [ ] Push `main`.
- [ ] Rebuild/recreate local Docker Compose with `docker compose -f deploy/compose.yaml build --no-cache` and `docker compose -f deploy/compose.yaml up -d --force-recreate`.
- [ ] Verify `docker compose -f deploy/compose.yaml ps` shows the app bound on port 3000 and smoke test the public status endpoint from inside the container.
