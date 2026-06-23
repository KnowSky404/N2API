# Provider Account Bulk Refresh Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add selected-account bulk credential refresh for provider accounts.

**Architecture:** Add an admin HTTP bulk endpoint that reuses `ProviderService.RefreshAccount` for each selected account. Add Svelte admin state and Provider accounts controls that reuse the existing selected-account state.

**Tech Stack:** Go `net/http`, provider service, SvelteKit, Bun tests.

---

## File Structure

- Modify `backend/internal/httpapi/server.go`: register and implement `POST /api/admin/provider-accounts/bulk-refresh`.
- Modify `backend/internal/httpapi/server_test.go`: add backend red/green coverage and fake refresh tracking.
- Modify `frontend/src/lib/admin-state.svelte.js`: add selected bulk refresh request action.
- Modify `frontend/src/routes/providers/+page.svelte`: add **Refresh selected** to the selected-account bulk controls.
- Modify `frontend/src/routes/providers/provider-page.test.mjs`: add source tests.
- Modify `backend/internal/gateway/documentation_test.go`, `README.md`, and `deploy/README.md`: document the workflow.

### Task 1: Backend Bulk Refresh API

**Files:**
- Modify: `backend/internal/httpapi/server_test.go`
- Modify: `backend/internal/httpapi/server.go`

- [ ] **Step 1: Write failing backend tests**

Add tests near existing provider account bulk tests:

```go
func TestAdminCanBulkRefreshUnifiedProviderAccounts(t *testing.T)
func TestAdminBulkProviderAccountRefreshValidatesInput(t *testing.T)
```

The success test posts:

```json
{"accountIds":[7,8,7]}
```

Assert HTTP 200, fake refresh IDs `[7, 8]`, and response entries for account 7 and 8 with non-nil `lastRefreshAt`.

- [ ] **Step 2: Run backend test to verify red**

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run 'BulkProviderAccountRefresh|BulkRefreshUnifiedProviderAccounts'
```

Expected: FAIL with 404 because the route does not exist.

- [ ] **Step 3: Implement backend route and handler**

Register:

```go
mux.HandleFunc("POST /api/admin/provider-accounts/bulk-refresh", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
	handleBulkRefreshProviderAccounts(w, r, providers)
}))
```

Add handler:

```go
func handleBulkRefreshProviderAccounts(w http.ResponseWriter, r *http.Request, providers ProviderService) {
	if providers == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable")
		return
	}
	var req struct {
		AccountIDs []int64 `json:"accountIds"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	accountIDs, ok := parseBulkProviderAccountIDs(w, req.AccountIDs)
	if !ok {
		return
	}
	accounts := make([]provider.Account, 0, len(accountIDs))
	for _, id := range accountIDs {
		account, err := providers.RefreshAccount(r.Context(), id)
		if err != nil {
			writeProviderAccountError(w, err)
			return
		}
		accounts = append(accounts, account)
	}
	writeJSON(w, http.StatusOK, map[string][]provider.Account{"accounts": accounts})
}
```

- [ ] **Step 4: Run backend test to verify green**

Run the same targeted `go test` command. Expected: PASS.

- [ ] **Step 5: Commit backend API**

```bash
git add backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go
git commit -m "feat: add provider account bulk refresh api"
```

### Task 2: Frontend Bulk Refresh Control

**Files:**
- Modify: `frontend/src/routes/providers/provider-page.test.mjs`
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/providers/+page.svelte`

- [ ] **Step 1: Write failing frontend tests**

Add assertions for:

```js
refreshSelectedProviderAccounts
/api/admin/provider-accounts/bulk-refresh
Refresh selected
```

- [ ] **Step 2: Run frontend test to verify red**

```bash
cd frontend && bun test src/routes/providers/provider-page.test.mjs
```

Expected: FAIL because the selected bulk refresh control and action do not exist.

- [ ] **Step 3: Implement admin state**

Add:

```js
export async function refreshSelectedProviderAccounts() {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;
  const accountIds = Object.keys(selectedProviderAccountIds)
    .map((id) => Number(id))
    .filter((id) => Number.isFinite(id) && id > 0);
  if (accountIds.length === 0) {
    providerAccounts.error = 'Select at least one provider account';
    return;
  }
  providerAccounts.saving = true;
  providerAccounts.error = '';
  try {
    await requestJSON('/api/admin/provider-accounts/bulk-refresh', {
      method: 'POST',
      body: JSON.stringify({ accountIds })
    });
    if (!isCurrentAuthenticated(version)) return;
    clearProviderAccountSelection();
    await loadProvider();
    await loadProviderAccounts();
    await loadModelRouting();
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    const message = error instanceof Error ? error.message : 'Selected account refresh failed';
    providerAccounts.error = message;
    await loadProviderAccounts();
    if (!isCurrentAuthenticated(version)) return;
    providerAccounts.error = message;
  } finally {
    if (isCurrentAuthenticated(version)) providerAccounts.saving = false;
  }
}
```

- [ ] **Step 4: Implement Provider page control**

Import `refreshSelectedProviderAccounts` and add a **Refresh selected** button in the selected-account bulk control area.

- [ ] **Step 5: Run frontend tests to verify green**

```bash
cd frontend && bun test src/routes/providers/provider-page.test.mjs
cd frontend && bun run check
```

Expected: PASS.

- [ ] **Step 6: Commit frontend controls**

```bash
git add frontend/src/lib/admin-state.svelte.js frontend/src/routes/providers/+page.svelte frontend/src/routes/providers/provider-page.test.mjs
git commit -m "feat: add provider account bulk refresh controls"
```

### Task 3: Docs and Verification

**Files:**
- Modify: `backend/internal/gateway/documentation_test.go`
- Modify: `README.md`
- Modify: `deploy/README.md`

- [ ] **Step 1: Add failing docs test**

Require docs to mention:

```go
"Refresh selected"
"selected provider accounts"
"force credential refresh"
```

- [ ] **Step 2: Update docs**

Document that selected provider accounts can be refreshed together with **Refresh selected**.

- [ ] **Step 3: Run docs test**

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run BulkRefresh
```

- [ ] **Step 4: Commit docs**

```bash
git add backend/internal/gateway/documentation_test.go README.md deploy/README.md
git commit -m "docs: document provider account bulk refresh"
```

- [ ] **Step 5: Full verification**

```bash
git diff --check
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
cd frontend && bun test src/routes/providers/provider-page.test.mjs
cd frontend && bun run check
cd frontend && bun run build
```
