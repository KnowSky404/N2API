# Routing Preview Diagnosis Summary Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an actionable diagnosis summary to model routing preview responses and show it in the Models admin page.

**Architecture:** Keep scheduler behavior unchanged. Derive diagnosis fields from the existing selection preview response after concurrency data is attached, so the API can summarize routable, degraded, and blocked states without changing account selection.

**Tech Stack:** Go HTTP/admin gateway code, SvelteKit admin UI, Bun frontend tests, Go package tests.

---

### Task 1: Backend Diagnosis Fields

**Files:**
- Modify: `backend/internal/httpapi/server.go`
- Test: `backend/internal/httpapi/server_test.go`

- [ ] **Step 1: Write failing tests**

Add tests around `/api/admin/model-routing/preview` that assert:
- selected preview returns `diagnosisStatus: "routable"` and a selected-account summary
- blocked preview returns `diagnosisStatus: "blocked"`, grouped `blockedReasonCounts`, and repair hints
- concurrency-full selected preview returns `diagnosisStatus: "degraded"` and a concurrency hint

- [ ] **Step 2: Run backend targeted tests**

Run:

```bash
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi
```

Expected: fail because diagnosis fields do not exist yet.

- [ ] **Step 3: Implement minimal derivation**

Extend `selectionPreviewResponse` with:
- `DiagnosisStatus string`
- `DiagnosisSummary string`
- `DiagnosisHints []string`
- `BlockedReasonCounts []selectionBlockedReasonCount`

Populate them in `selectionPreviewWithConcurrency` from the already-enriched candidate list.

- [ ] **Step 4: Re-run targeted tests**

Run the same backend command. Expected: pass.

### Task 2: Frontend Display

**Files:**
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/models/+page.svelte`
- Test: `frontend/src/routes/navigation.test.mjs`

- [ ] **Step 1: Write failing frontend structure tests**

Assert the Models page references `diagnosisStatus`, `diagnosisSummary`, `diagnosisHints`, and `blockedReasonCounts`, and the admin state typedef exposes these fields.

- [ ] **Step 2: Run frontend navigation test**

Run:

```bash
bun test src/routes/navigation.test.mjs
```

Expected: fail because the UI does not show the diagnosis summary yet.

- [ ] **Step 3: Implement UI and typedefs**

Add a compact diagnosis block above the candidate chips inside the existing Routing diagnostics result panel. Keep the operational dashboard style: status pill, short summary, inline hints, and compact reason-count chips.

- [ ] **Step 4: Re-run frontend navigation test**

Run the same Bun command. Expected: pass.

### Task 3: Full Verification and Commit

**Files:**
- All files changed above

- [ ] **Step 1: Run full verification**

Run:

```bash
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
bun test src/routes/navigation.test.mjs
bun run check
bun run build
git diff --check
```

- [ ] **Step 2: Commit**

Commit with:

```bash
git add docs/superpowers/plans/2026-06-28-routing-preview-diagnosis-summary.md backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go frontend/src/lib/admin-state.svelte.js frontend/src/routes/models/+page.svelte frontend/src/routes/navigation.test.mjs
git commit -m "feat: summarize routing preview diagnosis"
```
