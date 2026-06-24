# API Key Rename Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let admins rename existing client API keys without rotating or exposing the key secret.

**Architecture:** Add a narrow `UpdateAPIKeyName` path through admin service, store, HTTP, and Svelte state. Reuse existing API key row replacement behavior used by model-policy and limit updates.

**Tech Stack:** Go backend, PostgreSQL repository, SvelteKit admin UI, Bun frontend tests.

---

### Task 1: Document Design And Plan

**Files:**
- Create: `docs/superpowers/specs/2026-06-24-api-key-rename-design.md`
- Create: `docs/superpowers/plans/2026-06-24-api-key-rename.md`

- [ ] **Step 1: Commit docs**

Run:

```bash
git add docs/superpowers/specs/2026-06-24-api-key-rename-design.md docs/superpowers/plans/2026-06-24-api-key-rename.md
git commit -m "docs: plan api key rename"
```

Expected: commit succeeds.

### Task 2: Admin Service And Store

**Files:**
- Modify: `backend/internal/admin/service.go`
- Modify: `backend/internal/admin/service_test.go`
- Modify: `backend/internal/store/admin.go`
- Modify: `backend/internal/store/admin_test.go`

- [ ] **Step 1: Write failing admin service tests**

Add:

```go
func TestUpdateAPIKeyNameTrimsAndPersistsName(t *testing.T) {
  repo := newMemoryRepo()
  service := NewService(repo, Config{SessionTTL: time.Hour})
  result, err := service.CreateAPIKey(context.Background(), "codex laptop")
  if err != nil {
    t.Fatalf("CreateAPIKey returned error: %v", err)
  }

  updated, err := service.UpdateAPIKeyName(context.Background(), result.Key.ID, " renamed workstation ")
  if err != nil {
    t.Fatalf("UpdateAPIKeyName returned error: %v", err)
  }
  if updated.Name != "renamed workstation" {
    t.Fatalf("Name = %q, want trimmed rename", updated.Name)
  }

  keys, err := service.ListAPIKeys(context.Background())
  if err != nil {
    t.Fatalf("ListAPIKeys returned error: %v", err)
  }
  if len(keys) != 1 || keys[0].Name != "renamed workstation" {
    t.Fatalf("keys = %+v, want renamed key", keys)
  }
}

func TestUpdateAPIKeyNameRejectsInvalidName(t *testing.T) {
  repo := newMemoryRepo()
  service := NewService(repo, Config{SessionTTL: time.Hour})

  if _, err := service.UpdateAPIKeyName(context.Background(), 7, " \t "); !errors.Is(err, ErrInvalidInput) {
    t.Fatalf("UpdateAPIKeyName error = %v, want ErrInvalidInput", err)
  }
}
```

- [ ] **Step 2: Verify service tests fail**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/admin -run 'UpdateAPIKeyName'
```

Expected: FAIL because `UpdateAPIKeyName` does not exist.

- [ ] **Step 3: Implement service and memory repo**

Add `UpdateAPIKeyName` to `admin.Repository`, `admin.Service`, and the test memory repo. Service trims and rejects empty names before calling the repository.

- [ ] **Step 4: Verify service tests pass**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/admin -run 'UpdateAPIKeyName'
```

Expected: PASS.

- [ ] **Step 5: Add store coverage and implementation**

Extend `TestAdminRepositoryAPIKeyModelPolicyBehavior` or add a focused store test to call `UpdateAPIKeyName`, assert the name updates, and assert revoked keys return `admin.ErrNotFound`.

Implement:

```sql
UPDATE client_api_keys
SET name = $2
WHERE id = $1
  AND revoked_at IS NULL
RETURNING id, name, prefix, created_at, last_used_at, revoked_at, model_policy, requests_per_minute, tokens_per_minute
```

Populate selected models when the renamed key uses selected model policy.

- [ ] **Step 6: Verify admin and store package**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/admin ./internal/store
```

Expected: PASS or store integration tests skip when no `N2API_STORE_TEST_DATABASE_URL` is configured.

- [ ] **Step 7: Commit backend service/store**

Run:

```bash
git add backend/internal/admin/service.go backend/internal/admin/service_test.go backend/internal/store/admin.go backend/internal/store/admin_test.go
git commit -m "feat: rename api keys in admin service"
```

Expected: commit succeeds.

### Task 3: HTTP Endpoint

**Files:**
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/server_test.go`

- [ ] **Step 1: Write failing HTTP tests**

Add `UpdateAPIKeyName` to the fake service and tests for `PATCH /api/admin/keys/{id}` success plus bad id, invalid name, and not found mappings.

- [ ] **Step 2: Verify HTTP tests fail**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run 'UpdateAPIKeyName'
```

Expected: FAIL because the endpoint is missing.

- [ ] **Step 3: Implement endpoint**

Add `UpdateAPIKeyName` to `httpapi.AdminService` and route:

```go
mux.HandleFunc("PATCH /api/admin/keys/{id}", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
  id, err := parsePositivePathID(r, "id")
  if err != nil {
    writeError(w, http.StatusBadRequest, "bad_request")
    return
  }
  var req struct {
    Name string `json:"name"`
  }
  if err := decodeJSON(w, r, &req); err != nil {
    writeError(w, http.StatusBadRequest, "bad_request")
    return
  }
  key, err := admins.UpdateAPIKeyName(r.Context(), id, req.Name)
  if err != nil {
    if errors.Is(err, admin.ErrInvalidInput) {
      writeError(w, http.StatusBadRequest, "invalid_input")
      return
    }
    if errors.Is(err, admin.ErrNotFound) {
      writeError(w, http.StatusNotFound, "not_found")
      return
    }
    writeError(w, http.StatusInternalServerError, "internal_error")
    return
  }
  writeJSON(w, http.StatusOK, map[string]admin.APIKey{"key": key})
}))
```

- [ ] **Step 4: Verify HTTP tests pass**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run 'UpdateAPIKeyName'
```

Expected: PASS.

- [ ] **Step 5: Commit HTTP endpoint**

Run:

```bash
git add backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go
git commit -m "feat: expose api key rename endpoint"
```

Expected: commit succeeds.

### Task 4: Frontend Rename Form

**Files:**
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/api-keys/+page.svelte`
- Modify: `frontend/src/routes/navigation.test.mjs`

- [ ] **Step 1: Write failing frontend source test**

Extend API Keys tests to assert `updateAPIKeyName`, `Save name`, `bind:value={key.name}`, and `/api/admin/keys/${keyId}` are present.

- [ ] **Step 2: Verify frontend test fails**

Run:

```bash
cd frontend && bun test src/routes/navigation.test.mjs
```

Expected: FAIL because frontend rename support is missing.

- [ ] **Step 3: Implement state function**

Add:

```js
export async function updateAPIKeyName(keyId, name) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  const nextName = String(name ?? '').trim();
  if (!nextName) {
    apiKeys.error = 'API key name cannot be empty';
    return;
  }

  apiKeys.error = '';
  try {
    const payload = await requestJSON(`/api/admin/keys/${keyId}`, {
      method: 'PATCH',
      body: JSON.stringify({ name: nextName })
    });
    if (!isCurrentAuthenticated(version)) return;
    apiKeys.items = apiKeys.items.map((key) => (key.id === keyId ? payload.key : key));
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    apiKeys.error = error instanceof Error ? error.message : 'Failed to update API key name';
  }
}
```

- [ ] **Step 4: Add row form**

Import `updateAPIKeyName` and replace the static name cell with a small form that binds `key.name`, submits `updateAPIKeyName(key.id, key.name)`, and disables controls for revoked keys.

- [ ] **Step 5: Verify frontend**

Run:

```bash
cd frontend && bun test src/routes/navigation.test.mjs
cd frontend && bun run check
```

Expected: PASS.

- [ ] **Step 6: Commit frontend**

Run:

```bash
git add frontend/src/lib/admin-state.svelte.js frontend/src/routes/api-keys/+page.svelte frontend/src/routes/navigation.test.mjs
git commit -m "feat: rename api keys in admin ui"
```

Expected: commit succeeds.

### Task 5: Documentation And Final Verification

**Files:**
- Modify: `README.md`
- Modify: `deploy/README.md`
- Modify: `backend/internal/gateway/documentation_test.go`

- [ ] **Step 1: Add docs test and update docs**

Document that API key names can be renamed without rotating or revealing the secret, and add a documentation test for that sentence.

- [ ] **Step 2: Commit docs**

Run:

```bash
git add README.md deploy/README.md backend/internal/gateway/documentation_test.go
git commit -m "docs: document api key rename"
```

Expected: commit succeeds.

- [ ] **Step 3: Final verification**

Run:

```bash
git diff --check
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
cd frontend && bun test src/routes/navigation.test.mjs
cd frontend && bun run check
cd frontend && bun run build
git status --short
```

Expected: all commands pass and worktree is clean.
