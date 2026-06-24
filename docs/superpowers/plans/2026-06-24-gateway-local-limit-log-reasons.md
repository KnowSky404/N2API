# Gateway Local Limit Log Reasons Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Store precise request-log error reasons for local gateway limit rejections while preserving client-facing OpenAI-compatible error codes.

**Architecture:** Split response error code from log error code in the proxy's local guard branches. The deferred request logger continues using one `errorCode` variable, but local guard branches assign precise internal values before writing the generic client response code. The Request Logs UI already formats underscore-separated codes, so only tests and docs need UI coverage.

**Tech Stack:** Go gateway proxy, SvelteKit admin source tests, README/deploy documentation.

---

### Task 1: Gateway Log Error Reasons

**Files:**
- Modify: `backend/internal/gateway/proxy.go`
- Modify: `backend/internal/gateway/proxy_test.go`

- [ ] Write failing tests that exercise API-key request rate, API-key token rate, gateway concurrency, API-key concurrency, and provider-account concurrency rejections with a `fakeRequestLogger`.
- [ ] Run targeted gateway tests and confirm at least one fails because logs still store `rate_limit_exceeded`.
- [ ] Change only the logged `errorCode` values for local guard branches.
- [ ] Keep `writeOpenAIError` response code arguments as `rate_limit_exceeded`.
- [ ] Run targeted gateway tests until green.
- [ ] Commit with `feat: log precise gateway limit reasons`.

### Task 2: Request Logs UI Contract

**Files:**
- Modify: `frontend/src/routes/navigation.test.mjs`
- Modify: `frontend/src/routes/request-logs/+page.svelte`

- [ ] Add source-test coverage that `errorLabel(log.error)` remains the rendered Request Logs error label.
- [ ] Add representative strings for the new local-limit errors only if the page needs explicit examples.
- [ ] Run `bun test src/routes/navigation.test.mjs`.
- [ ] Commit with `test: cover gateway limit error labels` if source tests changed.

### Task 3: Documentation And Verification

**Files:**
- Modify: `README.md`
- Modify: `deploy/README.md`
- Modify: `backend/internal/gateway/documentation_test.go`

- [ ] Write a failing documentation test requiring local limit log error examples.
- [ ] Update docs to explain the client/log error distinction.
- [ ] Run backend and frontend verification gates.
- [ ] Commit with `docs: document gateway limit log reasons`.
