# Provider Account Bulk Status Actions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add selected-account bulk pause and reset-status operations for provider account scheduling management.

**Architecture:** Reuse the existing `ProviderService` single-account status methods from new admin-only bulk HTTP handlers. Reuse the provider accounts page selected-ID state and existing pause duration form for frontend controls.

**Tech Stack:** Go `net/http` backend, provider service interfaces, SvelteKit admin UI, Bun frontend tests, Go `testing`.

---

## File Structure

- Modify `backend/internal/httpapi/server.go`: register bulk status routes and implement handlers.
- Modify `backend/internal/httpapi/server_test.go`: add backend red/green coverage and extend fake provider tracking.
- Modify `frontend/src/lib/admin-state.svelte.js`: add selected bulk pause/reset actions.
- Modify `frontend/src/routes/providers/+page.svelte`: import and render selected bulk buttons.
- Modify `frontend/src/routes/providers/provider-page.test.mjs`: add source-level frontend coverage.
- Modify docs and docs tests after behavior lands.

### Task 1: Backend Bulk Status API

**Files:**
- Modify: `backend/internal/httpapi/server_test.go`
- Modify: `backend/internal/httpapi/server.go`

- [ ] **Step 1: Write failing backend tests**

Add tests next to the existing bulk provider account tests:

```go
func TestAdminCanBulkPauseUnifiedProviderAccountScheduling(t *testing.T) {
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{
		{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true, Status: provider.AccountStatusActive},
		{ID: 8, Provider: "openai", DisplayName: "Account B", Enabled: true, Status: provider.AccountStatusActive},
	}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/bulk-pause", strings.NewReader(`{"accountIds":[7,8,7],"durationSeconds":600}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if !reflect.DeepEqual(providers.pausedAccountIDs, []int64{7, 8}) {
		t.Fatalf("paused ids = %+v, want [7 8]", providers.pausedAccountIDs)
	}
	if providers.pauseDuration != 10*time.Minute {
		t.Fatalf("pause duration = %s, want 10m", providers.pauseDuration)
	}
	var body struct {
		Accounts []provider.Account `json:"accounts"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Accounts) != 2 || body.Accounts[0].Status != provider.AccountStatusCircuitOpen || body.Accounts[1].Status != provider.AccountStatusCircuitOpen {
		t.Fatalf("accounts = %+v, want two paused accounts", body.Accounts)
	}
}
```

- [ ] **Step 2: Run backend test to verify red**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run 'TestAdminCanBulkPauseUnifiedProviderAccountScheduling|TestAdminCanBulkResetUnifiedProviderAccountStatus|TestAdminBulkProviderAccountPauseValidatesInput|TestAdminBulkProviderAccountResetStatusValidatesInput'
```

Expected: fail because the bulk routes or fake tracking fields do not exist yet.

- [ ] **Step 3: Implement minimal backend API**

Add route registration:

```go
mux.HandleFunc("POST /api/admin/provider-accounts/bulk-pause", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
	handleBulkPauseProviderAccountScheduling(w, r, providers)
}))
mux.HandleFunc("POST /api/admin/provider-accounts/bulk-reset-status", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
	handleBulkResetProviderAccountStatus(w, r, providers)
}))
```

Add helper and handlers in `server.go`:

```go
func parseBulkProviderAccountIDs(w http.ResponseWriter, r *http.Request, accountIDs []int64) ([]int64, bool) {
	if len(accountIDs) == 0 || len(accountIDs) > 100 {
		writeError(w, http.StatusBadRequest, "invalid_input")
		return nil, false
	}
	ids := make([]int64, 0, len(accountIDs))
	seen := map[int64]struct{}{}
	for _, id := range accountIDs {
		if id <= 0 {
			writeError(w, http.StatusBadRequest, "invalid_input")
			return nil, false
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, true
}
```

- [ ] **Step 4: Run backend tests to verify green**

Run the same targeted `go test` command. Expected: PASS.

- [ ] **Step 5: Commit backend API**

```bash
git add backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go
git commit -m "feat: add provider account bulk status actions"
```

### Task 2: Frontend Bulk Status Controls

**Files:**
- Modify: `frontend/src/routes/providers/provider-page.test.mjs`
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/providers/+page.svelte`

- [ ] **Step 1: Write failing frontend source tests**

Add assertions that admin state exposes `pauseSelectedProviderAccounts`, `resetSelectedProviderAccountStatus`, `/bulk-pause`, and `/bulk-reset-status`, and that the page exposes `Pause selected` and `Reset selected`.

- [ ] **Step 2: Run frontend test to verify red**

Run:

```bash
cd frontend && bun test src/routes/providers/provider-page.test.mjs
```

Expected: fail because the selected bulk status actions do not exist yet.

- [ ] **Step 3: Implement frontend actions and buttons**

Add exported actions in `admin-state.svelte.js` that gather selected IDs, validate pause duration for pause, call the new backend endpoints, clear selection, and reload provider account plus model routing state.

Add buttons in `+page.svelte` near existing selected bulk controls:

```svelte
<button type="button" class="secondary-button" disabled={selectedProviderAccountCount === 0 || providerAccounts.saving} onclick={pauseSelectedProviderAccounts}>
  Pause selected
</button>
<button type="button" class="secondary-button" disabled={selectedProviderAccountCount === 0 || providerAccounts.saving} onclick={resetSelectedProviderAccountStatus}>
  Reset selected
</button>
```

- [ ] **Step 4: Run frontend test to verify green**

Run:

```bash
cd frontend && bun test src/routes/providers/provider-page.test.mjs
```

Expected: PASS.

- [ ] **Step 5: Commit frontend controls**

```bash
git add frontend/src/lib/admin-state.svelte.js frontend/src/routes/providers/+page.svelte frontend/src/routes/providers/provider-page.test.mjs
git commit -m "feat: add provider account bulk status controls"
```

### Task 3: Docs and Verification

**Files:**
- Modify: project docs that already describe provider account selected bulk operations.
- Modify: existing docs source tests for provider account bulk actions.

- [ ] **Step 1: Add failing docs assertions**

Add assertions for `Pause selected`, `Reset selected`, and selected bulk pause/reset wording.

- [ ] **Step 2: Update docs**

Document that selected provider accounts can be paused together for the configured duration and reset together after local recovery.

- [ ] **Step 3: Run docs tests**

Run the relevant docs test command discovered from existing scripts. Expected: PASS.

- [ ] **Step 4: Commit docs**

```bash
git add docs README.md frontend/src/routes/providers/provider-page.test.mjs
git commit -m "docs: document provider account bulk status actions"
```

- [ ] **Step 5: Full verification**

Run:

```bash
git diff --check
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
cd frontend && bun test src/routes/providers/provider-page.test.mjs
cd frontend && bun run check
cd frontend && bun run build
```

Expected: all commands pass.
