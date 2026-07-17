# Admin Navigation Split Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split the current one-page N2API admin dashboard into a ChatGPT-like left-sidebar app shell with focused pages for dashboard, providers, models, API keys, and request logs.

**Architecture:** Keep the backend API unchanged. Move shared admin session, health, provider, model, key, and log client state into one Svelte runes state module, provide it from `src/routes/+layout.svelte`, and render focused child route pages through SvelteKit file-based routing.

**Tech Stack:** SvelteKit 2, Svelte 5 runes, Bun, Tailwind CSS classes, static adapter.

---

### Task 1: Route Structure Guard

**Files:**
- Create: `frontend/src/routes/navigation.test.mjs`

- [ ] Add a Bun test that asserts the intended route files and shared state module exist.
- [ ] Run `bun test src/routes/navigation.test.mjs` from `frontend/` and confirm it fails before implementation because the new route files are missing.

### Task 2: Shared Admin State

**Files:**
- Create: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/+page.svelte`

- [ ] Extract the existing admin data loading and mutation functions into `admin-state.svelte.js`.
- [ ] Keep API paths, validation behavior, OAuth callback handling, copy behavior, and formatting behavior unchanged.

### Task 3: App Shell And Pages

**Files:**
- Create: `frontend/src/routes/+layout.svelte`
- Modify: `frontend/src/routes/+page.svelte`
- Create: `frontend/src/routes/providers/+page.svelte`
- Create: `frontend/src/routes/models/+page.svelte`
- Create: `frontend/src/routes/api-keys/+page.svelte`
- Create: `frontend/src/routes/request-logs/+page.svelte`

- [ ] Add a fixed left sidebar with `Dashboard`, `Providers`, `Models`, `API Keys`, and `Request Logs`.
- [ ] Put signed-in admin identity and `Sign out` in the lower-left user area.
- [ ] Keep unauthenticated users on the shared login surface.
- [ ] Move each existing functional section to its corresponding route.

### Task 4: Verification

**Files:**
- Verify only.

- [ ] Run `bun test src/routes/navigation.test.mjs` from `frontend/`.
- [ ] Run `bun run check` from `frontend/`.
- [ ] Run `bun run build` from `frontend/`.
- [ ] Smoke the rendered app route structure with Playwright or HTTP checks against the running local stack/dev server.
