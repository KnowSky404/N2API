# Runtime Provider Account Auto-Test Settings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make provider account automatic testing manageable through N2API Gateway Settings instead of startup-only environment variables.

**Architecture:** Extend the existing `admin.GatewaySettings` JSON shape and API. Keep environment variables as startup defaults, then have the auto-test runner load the latest admin settings between cycles. Add Gateway page controls that save the new fields through the existing settings endpoint.

**Tech Stack:** Go backend, PostgreSQL settings JSON row, SvelteKit admin UI, Bun frontend tooling.

---

## File Structure

- `backend/internal/admin/service.go` and `service_test.go`: add Gateway Settings fields, defaults, and validation.
- `backend/internal/httpapi/server_test.go`: cover GET/PUT JSON fields through the existing endpoint.
- `backend/cmd/n2api/main.go` and `main_test.go`: pass env defaults into admin service and wire runner to dynamic settings.
- `backend/internal/provider/auto_test_runner.go` and `auto_test_runner_test.go`: add a dynamic config source while keeping static config behavior.
- `frontend/src/lib/admin-state.svelte.js`: map, validate, and save new fields.
- `frontend/src/routes/gateway/+page.svelte`: add controls to the Gateway Settings form.
- `frontend/src/routes/navigation.test.mjs`: source-level regression tests for the controls and payload.
- `README.md`, `deploy/README.md`, `.env.example`: document that env values are defaults and UI/API settings can override them.

## Task 1: Gateway Settings Shape

**Files:**
- Modify: `backend/internal/admin/service.go`
- Modify: `backend/internal/admin/service_test.go`
- Modify: `backend/internal/httpapi/server_test.go`

- [ ] **Step 1: Add failing admin service tests**

Add tests that assert defaults include `ProviderAccountAutoTestIntervalSeconds: 300`, saved settings preserve `ProviderAccountAutoTestEnabled` and interval, enabled intervals below `60` are rejected, and disabled interval `0` normalizes to `300`.

- [ ] **Step 2: Run admin tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/admin -run 'TestGatewaySettings'
```

Expected: FAIL because the new fields do not exist.

- [ ] **Step 3: Implement settings fields and validation**

Add `ProviderAccountAutoTestEnabled bool` and `ProviderAccountAutoTestIntervalSeconds int` to `admin.GatewaySettings`. In `normalizeGatewaySettings`, normalize interval `0` to `300`, reject negative values, and reject enabled intervals below `60`.

- [ ] **Step 4: Add HTTP API JSON tests**

Extend existing `/api/admin/gateway-settings` tests so GET returns default env-derived auto-test fields and PUT stores and returns them.

- [ ] **Step 5: Run admin and HTTP tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/admin ./internal/httpapi -run 'GatewaySettings'
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/admin/service.go backend/internal/admin/service_test.go backend/internal/httpapi/server_test.go
git commit -m "feat: add auto test gateway settings"
```

## Task 2: Dynamic Runner Settings

**Files:**
- Modify: `backend/internal/provider/auto_test_runner.go`
- Modify: `backend/internal/provider/auto_test_runner_test.go`
- Modify: `backend/cmd/n2api/main.go`
- Modify: `backend/cmd/n2api/main_test.go`

- [ ] **Step 1: Add failing runner test**

Add a test proving the runner can start disabled from a config source, observe a later enabled config, and then call `TestAccounts`.

- [ ] **Step 2: Run runner tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/provider -run TestAutoTestRunner
```

Expected: FAIL because the dynamic config source does not exist.

- [ ] **Step 3: Implement dynamic config source**

Add `AutoTestRunnerConfigSource func(context.Context) (AutoTestRunnerConfig, error)` and `NewAutoTestRunnerWithConfigSource`. Static `NewAutoTestRunner` keeps the current disabled-immediate-return behavior. Dynamic runners keep polling even while disabled.

- [ ] **Step 4: Wire main to admin settings**

Pass env defaults into `admin.Config.DefaultGatewaySettings`. Wire `NewAutoTestRunnerWithConfigSource` so each read calls `adminService.GetGatewaySettings(ctx)` and maps the interval seconds to `time.Duration`.

- [ ] **Step 5: Run provider and cmd tests**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/provider ./cmd/n2api -run 'AutoTestRunner|MainWires'
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/provider/auto_test_runner.go backend/internal/provider/auto_test_runner_test.go backend/cmd/n2api/main.go backend/cmd/n2api/main_test.go
git commit -m "feat: reload auto test schedule settings"
```

## Task 3: Gateway Page Controls

**Files:**
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/gateway/+page.svelte`
- Modify: `frontend/src/routes/navigation.test.mjs`

- [ ] **Step 1: Add failing frontend source tests**

Add assertions that the Gateway page includes `Provider account auto tests`, binds `gatewaySettings.data.providerAccountAutoTestEnabled`, binds `gatewaySettings.data.providerAccountAutoTestIntervalSeconds`, and admin state sends both JSON fields.

- [ ] **Step 2: Run frontend source tests**

Run:

```bash
cd frontend
bun test src/routes/navigation.test.mjs
```

Expected: FAIL because the controls and payload mapping do not exist.

- [ ] **Step 3: Implement admin state mapping and validation**

Load and save `providerAccountAutoTestEnabled` and `providerAccountAutoTestIntervalSeconds`. Reject non-integer or negative interval values, and reject enabled intervals below `60`.

- [ ] **Step 4: Implement Gateway controls**

Add checkbox and number input inside the existing Gateway Settings form. Keep one save button.

- [ ] **Step 5: Run frontend source tests**

Run:

```bash
cd frontend
bun test src/routes/navigation.test.mjs
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/lib/admin-state.svelte.js frontend/src/routes/gateway/+page.svelte frontend/src/routes/navigation.test.mjs
git commit -m "feat: manage auto test settings in gateway UI"
```

## Task 4: Documentation And Verification

**Files:**
- Modify: `README.md`
- Modify: `deploy/README.md`
- Modify: `.env.example`
- Modify: `backend/internal/gateway/documentation_test.go`

- [ ] **Step 1: Update documentation test**

Require docs to mention that env values are startup defaults and that Gateway Settings can save runtime auto-test settings.

- [ ] **Step 2: Update docs**

Update README and deploy README wording from startup-only env controls to env defaults plus Gateway Settings override.

- [ ] **Step 3: Run documentation test**

Run:

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run TestGatewayDocumentationMentionsProviderAccountAutoTests
```

Expected: PASS.

- [ ] **Step 4: Run full verification**

Run:

```bash
git diff --check
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
cd ../frontend
bun run check
bun run build
```

Expected: all commands exit 0. If sandbox blocks `httptest` sockets, rerun backend tests with the approved escalation path and record it.

- [ ] **Step 5: Commit**

```bash
git add README.md deploy/README.md .env.example backend/internal/gateway/documentation_test.go
git commit -m "docs: document runtime auto test settings"
```
