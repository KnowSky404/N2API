# Routing Preview Schedule Reason Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add schedule-reason diagnostics to routing preview candidates without changing scheduler behavior.

**Architecture:** Extend the provider `SelectionCandidate` DTO with a concise `scheduleReason` string. Populate it while building preview candidates because that code already knows selected/sticky state. The HTTP layer embeds provider candidates, so it should preserve the field automatically. The Svelte models page renders the field in each candidate chip.

**Tech Stack:** Go provider service and HTTP admin API, SvelteKit admin UI, Bun source tests.

---

### Task 1: Provider Schedule Reasons

**Files:**
- Modify: `backend/internal/provider/service.go`
- Modify: `backend/internal/provider/service_test.go`

- [ ] Write failing tests that assert routing preview candidates include `scheduleReason`.
- [ ] Run `go test ./internal/provider -run PreviewAccountSelection` and confirm failure.
- [ ] Add `ScheduleReason string` to `SelectionCandidate`.
- [ ] Populate selected, sticky-bound, and ordered reasons in `PreviewAccountSelection`.
- [ ] Run provider targeted tests until green.
- [ ] Commit with `feat: explain routing preview schedule reasons`.

### Task 2: HTTP Preview Contract

**Files:**
- Modify: `backend/internal/httpapi/server_test.go`

- [ ] Write failing or contract-strengthening test that decodes `scheduleReason` from `GET /api/admin/model-routing/preview`.
- [ ] Run `go test ./internal/httpapi -run PreviewAccountSelection`.
- [ ] If needed, adjust HTTP response enrichment to preserve the field.
- [ ] Run HTTP targeted tests until green.
- [ ] Commit with `test: cover routing preview schedule reasons`.

### Task 3: Frontend Rendering

**Files:**
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/models/+page.svelte`
- Modify: `frontend/src/routes/navigation.test.mjs`
- Modify: `frontend/src/routes/providers/provider-page.test.mjs`

- [ ] Write failing source tests for `scheduleReason` and `Schedule reason`.
- [ ] Run `bun test src/routes/navigation.test.mjs src/routes/providers/provider-page.test.mjs`.
- [ ] Add the field to the selection candidate typedef.
- [ ] Render `Schedule reason {account.scheduleReason}` in routing preview chips.
- [ ] Run targeted Bun source tests until green.
- [ ] Commit with `feat: show routing preview schedule reasons`.

### Task 4: Documentation And Verification

**Files:**
- Modify: `README.md`
- Modify: `deploy/README.md`
- Modify: `backend/internal/gateway/documentation_test.go`

- [ ] Write failing documentation test for Routing diagnostics schedule reasons.
- [ ] Update README and deploy notes.
- [ ] Run backend and frontend verification gates.
- [ ] Commit with `docs: document routing preview schedule reasons`.
