# Backend Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the N2API backend foundation: validated configuration, PostgreSQL connectivity, embedded migrations, secret utilities, admin health/bootstrap APIs, and a frontend health status fetch.

**Architecture:** The Go backend remains one service with small internal packages for config, crypto, database, migrations, and HTTP routing. PostgreSQL access uses `pgxpool`; migrations use embedded SQL files with Goose. The SvelteKit admin shell fetches a JSON health endpoint from the same origin.

**Tech Stack:** Go, pgx v5, Goose v3, PostgreSQL, Bun, SvelteKit, Tailwind CSS.

---

### Task 1: Backend Configuration

**Files:**
- Create: `backend/internal/config/config.go`
- Create: `backend/internal/config/config_test.go`

- [x] **Step 1: Write failing config tests**

Cover defaults, required `DATABASE_URL`, required `N2API_ENCRYPTION_SECRET`, default host/port, and invalid port rejection.

- [x] **Step 2: Implement config loader**

Expose `Load(lookup func(string) string) (Config, error)` and keep environment parsing isolated from `main`.

- [x] **Step 3: Verify and commit**

Run `GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...` from `backend`, then commit as `feat: add backend config loader`.

### Task 2: Security Utilities

**Files:**
- Create: `backend/internal/secret/crypto.go`
- Create: `backend/internal/secret/crypto_test.go`

- [x] **Step 1: Write failing secret tests**

Cover API key hashing/verification and AES-GCM encrypt/decrypt for OAuth token payloads.

- [x] **Step 2: Implement secret utilities**

Use SHA-256 for API key hashes and AES-GCM with a SHA-256-derived key for reversible token encryption.

- [x] **Step 3: Verify and commit**

Run `GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...` from `backend`, then commit as `feat: add backend secret utilities`.

### Task 3: PostgreSQL Pool and Migrations

**Files:**
- Create: `backend/internal/store/postgres.go`
- Create: `backend/internal/store/migrate.go`
- Create: `backend/internal/store/migrations/00001_init.sql`
- Create: `backend/internal/store/migrations_test.go`
- Modify: `backend/go.mod`

- [x] **Step 1: Write failing migration tests**

Use testcontainers only if already practical; otherwise unit-test embedded migration discovery and keep live database verification to Docker Compose. The migration file must include `admins`, `oauth_accounts`, `client_api_keys`, `settings`, and `request_logs`.

- [x] **Step 2: Add pgx and goose dependencies**

Use `github.com/jackc/pgx/v5/pgxpool`, `github.com/jackc/pgx/v5/stdlib`, and `github.com/pressly/goose/v3`.

- [x] **Step 3: Implement pool and migration helpers**

Expose `OpenPool(ctx, databaseURL)` and `RunMigrations(ctx, pool)`.

- [x] **Step 4: Verify and commit**

Run `GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...` from `backend`, then commit as `feat: add postgres migrations`.

### Task 4: HTTP Health and Bootstrap API

**Files:**
- Create: `backend/internal/httpapi/server.go`
- Create: `backend/internal/httpapi/server_test.go`
- Modify: `backend/cmd/n2api/main.go`

- [x] **Step 1: Write failing HTTP tests**

Cover `GET /healthz`, `GET /api/admin/health`, `GET /api/admin/bootstrap`, JSON content type, and DB health status behavior.

- [x] **Step 2: Implement HTTP server package**

Move route setup out of `main`; inject config and optional store health checker.

- [x] **Step 3: Wire main**

Load config, open PostgreSQL pool, run migrations, and start the HTTP server.

- [x] **Step 4: Verify and commit**

Run `GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...` from `backend`, then commit as `feat: add backend health APIs`.

### Task 5: Frontend Health Status

**Files:**
- Modify: `frontend/src/routes/+page.svelte`

- [x] **Step 1: Add health state fetch**

Use Svelte `onMount` to fetch `/api/admin/health`, show loading, connected, and error states.

- [x] **Step 2: Verify and commit**

Run `bun run check` and `bun run build` from `frontend`, then commit as `feat: show backend health in admin UI`.

### Task 6: Full Verification

**Files:**
- Review repository state.

- [x] **Step 1: Run backend tests**

Run `GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...` from `backend`.

- [x] **Step 2: Run frontend checks**

Run `bun run check` and `bun run build` from `frontend`.

- [x] **Step 3: Validate Docker Compose config**

Run `docker compose -f deploy/compose.yaml --env-file .env.example config` from the repository root.
