# Codex OAuth Account Pool Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Codex/OpenAI OAuth account pool where each upstream account has isolated OAuth state, identity metadata, operator controls, refresh state, gateway scheduling state, and UI management.

**Architecture:** Reuse N2API's existing `provider` package and `oauth_accounts` table, extending them into an upstream account pool. Keep tokens encrypted in dedicated columns and store only identity/status metadata in non-secret columns or JSON. Keep provider routes compatible while adding account creation, re-authorization, manual refresh, and scheduling state operations.

**Tech Stack:** Go backend, PostgreSQL/goose migrations, SvelteKit/Tailwind admin UI, Docker Compose.

---

### Task 1: Account Pool Schema and Repository

**Files:**
- Modify: `backend/internal/provider/service.go`
- Modify: `backend/internal/store/provider.go`
- Modify: `backend/internal/store/migrations_test.go`
- Create: `backend/internal/store/migrations/00006_codex_account_pool_state.sql`
- Modify: `backend/internal/provider/service_test.go`

- [ ] Write failing tests for account fields, pending OAuth fields, duplicate identity lookup, and scheduling-state persistence.
- [ ] Add migration columns for `oauth_accounts` and `oauth_states`.
- [ ] Extend provider domain structs and repository scans/inserts/updates.
- [ ] Add repository methods for finding by identity metadata, target account update, refresh state, rate limit, circuit breaker, and error clearing.
- [ ] Run targeted store/provider tests.

### Task 2: OAuth Account Creation and Re-Authorization

**Files:**
- Modify: `backend/internal/provider/service.go`
- Modify: `backend/internal/provider/http_client_test.go`
- Modify: `backend/internal/provider/service_test.go`
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/server_test.go`

- [ ] Write failing tests for `StartConnect` with new-account options and request fingerprint hashes.
- [ ] Write failing tests for callback creating a new account.
- [ ] Write failing tests for callback updating target account during re-authorization.
- [ ] Write failing tests for identity-key duplicate update when no target id is provided.
- [ ] Implement `ConnectOptions`, `ReauthorizeAccount`, callback account resolution, and identity metadata extraction.
- [ ] Add admin API request parsing for account creation options and re-authorization.
- [ ] Run provider and httpapi tests.

### Task 3: Refresh, Circuit Breaker, Rate Limit, and Selection

**Files:**
- Modify: `backend/internal/provider/service.go`
- Modify: `backend/internal/provider/service_test.go`
- Modify: `backend/internal/gateway/proxy.go`
- Modify: `backend/internal/gateway/proxy_test.go`
- Modify: `backend/internal/store/provider.go`

- [ ] Write failing tests showing disabled/rate-limited/circuit-open accounts are skipped.
- [ ] Write failing tests showing refresh failures increment `failure_count` and open the circuit after three failures.
- [ ] Write failing tests showing successful token use clears transient error and failure state.
- [ ] Add provider methods for manual refresh and gateway failure classification.
- [ ] Teach gateway to mark account rate-limited or failed before retrying another account when upstream fails before streaming.
- [ ] Run provider and gateway tests.

### Task 4: Admin UI Account Management

**Files:**
- Modify: `frontend/src/routes/+page.svelte`
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/server_test.go`

- [ ] Add UI state for new-account form, rename, re-authorize, manual refresh, status display, and fingerprint payload.
- [ ] Update provider table columns to show status, email, ChatGPT account id, plan type, expiry, refresh, last used, and error state.
- [ ] Wire `connect`, `reauthorize`, `refresh`, `patch`, and `disconnect` actions.
- [ ] Run `bun run check` and `bun run build`.

### Task 5: Documentation and Docker Verification

**Files:**
- Modify: `README.md`
- Modify: `deploy/README.md`
- Modify: `.env.example`

- [ ] Document multi-account OAuth login, re-authorization, account status, and testing flow.
- [ ] Run `GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...` from `backend`.
- [ ] Run `bun run check` and `bun run build` from `frontend`.
- [ ] Run `docker compose -f deploy/compose.yaml --env-file .env.example up --build -d`.
- [ ] Verify `docker compose ... ps` and `curl -i -sS http://127.0.0.1:3000/api/public/status`.
- [ ] Commit the implementation.
