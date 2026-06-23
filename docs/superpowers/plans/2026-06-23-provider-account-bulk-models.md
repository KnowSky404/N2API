# Provider Account Bulk Models Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add selected-account bulk model capability replacement for provider accounts.

**Architecture:** Add an admin HTTP bulk endpoint that reuses `ProviderService.ReplaceAccountModels` for each selected account. Add Svelte admin state and Provider accounts controls that reuse existing model text parsing.

**Tech Stack:** Go `net/http`, provider service, SvelteKit, Bun tests.

---

## File Structure

- Modify `backend/internal/httpapi/server.go`: register and implement `POST /api/admin/provider-accounts/bulk-models`.
- Modify `backend/internal/httpapi/server_test.go`: add backend red/green coverage and fake tracking.
- Modify `frontend/src/lib/admin-state.svelte.js`: add bulk model form state and selected bulk request action.
- Modify `frontend/src/routes/providers/+page.svelte`: add textarea and button to the selected-account bulk controls.
- Modify `frontend/src/routes/providers/provider-page.test.mjs`: add source tests.
- Modify `backend/internal/gateway/documentation_test.go`, `README.md`, and `deploy/README.md`: document the workflow.

### Task 1: Backend Bulk Models API

**Files:**
- Modify: `backend/internal/httpapi/server_test.go`
- Modify: `backend/internal/httpapi/server.go`

- [ ] **Step 1: Write failing backend tests**

Add tests near existing provider account bulk tests:

```go
func TestAdminCanBulkReplaceUnifiedProviderAccountModels(t *testing.T)
func TestAdminBulkProviderAccountModelsValidatesInput(t *testing.T)
```

The success test posts:

```json
{"accountIds":[7,8,7],"models":[{"model":"gpt-5","enabled":true},{"model":"codex-mini","enabled":true}]}
```

Assert HTTP 200, fake replacement IDs `[7, 8]`, and response entries for account 7 and 8.

- [ ] **Step 2: Run backend test to verify red**

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run 'BulkProviderAccountModels'
```

Expected: FAIL with 404 because the route does not exist.

- [ ] **Step 3: Implement backend route and handler**

Register:

```go
mux.HandleFunc("POST /api/admin/provider-accounts/bulk-models", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
	handleBulkReplaceProviderAccountModels(w, r, providers)
}))
```

Add handler:

```go
func handleBulkReplaceProviderAccountModels(w http.ResponseWriter, r *http.Request, providers ProviderService) {
	if providers == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable")
		return
	}
	var req struct {
		AccountIDs []int64 `json:"accountIds"`
		Models []provider.AccountModelInput `json:"models"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if len(req.Models) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_input")
		return
	}
	accountIDs, ok := parseBulkProviderAccountIDs(w, req.AccountIDs)
	if !ok {
		return
	}
	// loop over accountIDs and call providers.ReplaceAccountModels
}
```

- [ ] **Step 4: Run backend test to verify green**

Run the same targeted `go test` command. Expected: PASS.

- [ ] **Step 5: Commit backend API**

```bash
git add backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go
git commit -m "feat: add provider account bulk model api"
```

### Task 2: Frontend Bulk Models Control

**Files:**
- Modify: `frontend/src/routes/providers/provider-page.test.mjs`
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/providers/+page.svelte`

- [ ] **Step 1: Write failing frontend tests**

Add assertions for:

```js
providerAccountBulkModelsForm
bulkReplaceSelectedProviderAccountModels
/api/admin/provider-accounts/bulk-models
Bulk models
Apply models
```

- [ ] **Step 2: Run frontend test to verify red**

```bash
cd frontend && bun test src/routes/providers/provider-page.test.mjs
```

Expected: FAIL because the controls and action do not exist.

- [ ] **Step 3: Implement admin state**

Add:

```js
export const providerAccountBulkModelsForm = $state({ text: '' });
```

Add:

```js
export async function bulkReplaceSelectedProviderAccountModels() {
  // collect selected ids
  // parse providerAccountBulkModelsForm.text with parseAccountModelsText
  // validate non-empty
  // POST /api/admin/provider-accounts/bulk-models
  // clear selection and text, reload provider accounts and model routing
}
```

- [ ] **Step 4: Implement Provider page controls**

Import the state/action and add a compact textarea plus **Apply models** button in the selected-account bulk control area.

- [ ] **Step 5: Run frontend test to verify green**

```bash
cd frontend && bun test src/routes/providers/provider-page.test.mjs
```

Expected: PASS.

- [ ] **Step 6: Commit frontend controls**

```bash
git add frontend/src/lib/admin-state.svelte.js frontend/src/routes/providers/+page.svelte frontend/src/routes/providers/provider-page.test.mjs
git commit -m "feat: add provider account bulk model controls"
```

### Task 3: Docs and Verification

**Files:**
- Modify: `backend/internal/gateway/documentation_test.go`
- Modify: `README.md`
- Modify: `deploy/README.md`

- [ ] **Step 1: Add failing docs test**

Require docs to mention:

```go
"Apply models"
"selected provider accounts"
"same model capability list"
```

- [ ] **Step 2: Update docs**

Document that selected provider accounts can receive the same model capability list with **Apply models**.

- [ ] **Step 3: Run docs test**

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run BulkModels
```

- [ ] **Step 4: Commit docs**

```bash
git add backend/internal/gateway/documentation_test.go README.md deploy/README.md
git commit -m "docs: document provider account bulk models"
```

- [ ] **Step 5: Full verification**

```bash
git diff --check
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
cd frontend && bun test src/routes/providers/provider-page.test.mjs
cd frontend && bun run check
cd frontend && bun run build
```
