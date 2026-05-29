# N2API V1 Bootstrap Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Initialize the N2API repository with project constraints, documentation, baseline Go backend, SvelteKit frontend, PostgreSQL-oriented Docker Compose, and verification commands.

**Architecture:** N2API is a Go service that owns API traffic, OAuth flows, admin APIs, and static frontend serving. PostgreSQL is the default V1 persistence layer. The frontend is a Bun-managed SvelteKit/Tailwind app built into static assets for the Go service to serve.

**Tech Stack:** Go, PostgreSQL, Bun, SvelteKit, Tailwind CSS, Docker Compose.

---

### Task 1: Repository Policy and Product Direction

**Files:**
- Modify: `AGENTS.md`
- Create: `docs/superpowers/specs/2026-05-29-n2api-v1-design.md`
- Create: `README.md`
- Create: `.gitignore`
- Create: `.env.example`

- [x] **Step 1: Write project-level agent constraints**

Record communication rules, Context7 usage, project scope, backend/frontend constraints, PostgreSQL default, Redis optional status, and no-sub2api-copy policy in `AGENTS.md`.

- [x] **Step 2: Save the V1 design spec**

Write `docs/superpowers/specs/2026-05-29-n2api-v1-design.md` with goals, non-goals, architecture, data/security rules, and Docker deployment baseline.

- [x] **Step 3: Add README and local configuration templates**

Create `README.md`, `.gitignore`, and `.env.example` so a new contributor can understand the project and expected configuration.

### Task 2: Backend Bootstrap

**Files:**
- Create: `backend/go.mod`
- Create: `backend/cmd/n2api/main.go`

- [x] **Step 1: Initialize Go module**

Run:

```bash
cd backend
go mod init github.com/KnowSky404/N2API/backend
```

Expected: `backend/go.mod` exists with module path `github.com/KnowSky404/N2API/backend`.

- [x] **Step 2: Add minimal health server**

Create `backend/cmd/n2api/main.go` with an HTTP server exposing `GET /healthz`.

- [x] **Step 3: Verify backend**

Run:

```bash
cd backend
go test ./...
```

Expected: tests complete successfully or report no test files.

### Task 3: Frontend Bootstrap

**Files:**
- Create: `frontend/package.json`
- Create: `frontend/svelte.config.js`
- Create: `frontend/vite.config.ts`
- Create: `frontend/src/routes/+layout.js`
- Create: `frontend/src/routes/+page.svelte`
- Create: `frontend/src/app.css`

- [x] **Step 1: Initialize a Bun-managed SvelteKit static app**

Use SvelteKit with static adapter and Tailwind CSS through the Vite plugin.

- [x] **Step 2: Add a minimal admin dashboard shell**

The first page should identify N2API, show V1 status placeholders, and avoid marketing-page layout.

- [x] **Step 3: Verify frontend**

Run:

```bash
cd frontend
bun install
bun run check
bun run build
```

Expected: dependencies install, Svelte checks pass, and a static build is generated.

### Task 4: Docker Compose Baseline

**Files:**
- Create: `deploy/compose.yaml`
- Create: `deploy/README.md`

- [x] **Step 1: Define required services**

Add `n2api` and `postgres` services. PostgreSQL must have a persistent volume. The app service should depend on PostgreSQL and read configuration from `.env`.

- [x] **Step 2: Document local startup**

Document copying `.env.example` to `.env`, editing secrets, and running Docker Compose.

### Task 5: Final Verification

**Files:**
- Review all initialized files.

- [x] **Step 1: Check repository status**

Run:

```bash
git status --short
```

Expected: only intentional bootstrap files are modified or created.

- [x] **Step 2: Check V1 database direction**

Run:

```bash
rg -n "SQLite|sqlite|Redis.*required|billing|recharge|payment" AGENTS.md README.md docs
```

Expected: SQLite appears only as excluded V1 scope, Redis appears only as optional future infrastructure, and billing/payment appear only as non-goals.
