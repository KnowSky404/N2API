# API Key Lifecycle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Update API key lifecycle handling so keys show active, disabled, and deleted states, deleted keys expose a physical deletion time, status toggles live in the table status column, and per-key logs open an in-page modal.

**Architecture:** Keep `revoked_at` as the logical deletion timestamp. Add a backend response field derived from `revoked_at + 30 days`, and add repository/service cleanup that physically deletes keys whose logical deletion timestamp is older than the retention cutoff. Keep UI changes localized to the API Keys page and existing shared admin state.

**Tech Stack:** Go backend with PostgreSQL store, SvelteKit/Svelte 5 frontend, Bun tests/check/build, Go tests.

---

## Files

- Modify: `backend/internal/admin/service.go`
  - Add API key retention duration and service cleanup before listing keys.
  - Extend the repository interface with physical purge behavior.
- Modify: `backend/internal/admin/service_test.go`
  - Add service-level coverage that `ListAPIKeys` triggers purge with the correct cutoff.
  - Update `memoryRepo` to satisfy the new repository method.
- Modify: `backend/internal/store/admin.go`
  - Add repository method that physically deletes revoked keys at or before a cutoff.
- Modify: `backend/internal/store/admin_test.go`
  - Add store-level cleanup coverage for old revoked, recent revoked, disabled, and active keys.
- Modify: `backend/internal/httpapi/server.go`
  - Add `physicalDeleteAt` to `apiKeyResponse` for revoked keys.
- Modify: `backend/internal/httpapi/server_test.go`
  - Add response coverage for `physicalDeleteAt`.
- Modify: `frontend/src/lib/admin-state.svelte.js`
  - Add the `physicalDeleteAt` API key property to JSDoc.
  - Keep `revokeKey` using the existing backend route.
- Modify: `frontend/src/routes/api-keys/+page.svelte`
  - Move enable/disable control into the status column.
  - Rename revoke/delete UI wording.
  - Replace Request Logs link with an in-page Logs modal.
  - Show the physical deletion time in the deleted status tooltip.
- Modify: `frontend/src/routes/providers/provider-page.test.mjs`
  - Extend existing source-level assertions for API Keys UI behavior.
- Modify: `README.md`
  - Update API key lifecycle wording.
- Modify: `deploy/README.md`
  - Update deployment-facing API key lifecycle wording.

## Task 1: Backend Response Field

**Files:**
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/server_test.go`

- [ ] **Step 1: Write the failing HTTP response test**

Add this test near `TestRevokeAPIKeyParsesIDAndReturnsRevokedKey` in `backend/internal/httpapi/server_test.go`:

```go
func TestListAPIKeysIncludesPhysicalDeleteAtForRevokedKeys(t *testing.T) {
	admins := newFakeAdminService()
	revokedAt := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	admins.keys = []admin.APIKey{
		{
			ID:        7,
			Name:      "deleted workstation",
			Prefix:    "n2_test",
			CreatedAt: revokedAt.Add(-time.Hour),
			RevokedAt: &revokedAt,
		},
		{
			ID:        8,
			Name:      "active workstation",
			Prefix:    "n2_live",
			CreatedAt: revokedAt.Add(-time.Hour),
		},
	}
	server := NewServer(config.Config{}, staticHealth{}, admins, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/keys", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Keys []struct {
			ID               int64      `json:"id"`
			PhysicalDeleteAt *time.Time `json:"physicalDeleteAt"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Keys) != 2 {
		t.Fatalf("keys length = %d, want 2", len(body.Keys))
	}
	want := revokedAt.Add(30 * 24 * time.Hour)
	if body.Keys[0].ID != 7 || body.Keys[0].PhysicalDeleteAt == nil || !body.Keys[0].PhysicalDeleteAt.Equal(want) {
		t.Fatalf("revoked key physicalDeleteAt = %+v, want %s", body.Keys[0], want.Format(time.RFC3339))
	}
	if body.Keys[1].ID != 8 || body.Keys[1].PhysicalDeleteAt != nil {
		t.Fatalf("active key physicalDeleteAt = %+v, want nil", body.Keys[1])
	}
}
```

- [ ] **Step 2: Run the focused failing test**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run TestListAPIKeysIncludesPhysicalDeleteAtForRevokedKeys -count=1
```

Expected: FAIL because `physicalDeleteAt` is not present in the response.

- [ ] **Step 3: Add the response field and retention helper**

In `backend/internal/httpapi/server.go`, update `apiKeyResponse`:

```go
type apiKeyResponse struct {
	admin.APIKey
	admin.APIKeyBudgetUsage
	PhysicalDeleteAt              *time.Time `json:"physicalDeleteAt"`
	CurrentConcurrentRequests      int        `json:"currentConcurrentRequests"`
	EffectiveMaxConcurrentRequests int        `json:"effectiveMaxConcurrentRequests"`
	ConcurrencyBlocked             bool       `json:"concurrencyBlocked"`
	CurrentRequestsThisMinute      int        `json:"currentRequestsThisMinute"`
	EffectiveRequestsPerMinute     int        `json:"effectiveRequestsPerMinute"`
	RequestRateRemaining           int        `json:"requestRateRemaining"`
	RequestRateLimited             bool       `json:"requestRateLimited"`
	CurrentTokensThisMinute        int        `json:"currentTokensThisMinute"`
	EffectiveTokensPerMinute       int        `json:"effectiveTokensPerMinute"`
	TokenRateRemaining             int        `json:"tokenRateRemaining"`
	TokenRateLimited               bool       `json:"tokenRateLimited"`
}
```

Add this helper near `apiKeyResponses`:

```go
func apiKeyPhysicalDeleteAt(key admin.APIKey) *time.Time {
	if key.RevokedAt == nil {
		return nil
	}
	value := key.RevokedAt.Add(admin.APIKeyPhysicalDeleteRetention)
	return &value
}
```

In `apiKeyResponses`, set the field:

```go
PhysicalDeleteAt: apiKeyPhysicalDeleteAt(key),
```

- [ ] **Step 4: Export the retention duration from admin service**

In `backend/internal/admin/service.go`, add near the API key constants or type definitions:

```go
const APIKeyPhysicalDeleteRetention = 30 * 24 * time.Hour
```

- [ ] **Step 5: Run the focused passing test**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run TestListAPIKeysIncludesPhysicalDeleteAtForRevokedKeys -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit Task 1**

Run:

```bash
git add backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go backend/internal/admin/service.go
git commit -m "feat: expose api key physical delete time"
```

## Task 2: Backend Physical Cleanup

**Files:**
- Modify: `backend/internal/admin/service.go`
- Modify: `backend/internal/admin/service_test.go`
- Modify: `backend/internal/store/admin.go`
- Modify: `backend/internal/store/admin_test.go`

- [ ] **Step 1: Extend the repository interface and memory repo**

In `backend/internal/admin/service.go`, extend `Repository`:

```go
PurgeRevokedAPIKeys(ctx context.Context, cutoff time.Time) (int64, error)
```

In `backend/internal/admin/service_test.go`, add this method to `memoryRepo`:

```go
func (r *memoryRepo) PurgeRevokedAPIKeys(_ context.Context, cutoff time.Time) (int64, error) {
	var deleted int64
	for id, key := range r.keys {
		if key.RevokedAt != nil && !key.RevokedAt.After(cutoff) {
			delete(r.keys, id)
			deleted++
		}
	}
	return deleted, nil
}
```

- [ ] **Step 2: Write the failing service cleanup test**

Add this test near the API key list tests in `backend/internal/admin/service_test.go`:

```go
func TestListAPIKeysPurgesRevokedKeysPastRetention(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, time.Hour, GatewaySettings{})
	now := time.Now().UTC()
	oldDeleted := APIKey{
		ID:        1,
		Name:      "old deleted",
		Prefix:    "n2_old",
		CreatedAt: now.Add(-60 * 24 * time.Hour),
	}
	oldDeleted.RevokedAt = ptrTime(now.Add(-31 * 24 * time.Hour))
	recentDeleted := APIKey{
		ID:        2,
		Name:      "recent deleted",
		Prefix:    "n2_recent",
		CreatedAt: now.Add(-2 * 24 * time.Hour),
	}
	recentDeleted.RevokedAt = ptrTime(now.Add(-2 * 24 * time.Hour))
	active := APIKey{
		ID:        3,
		Name:      "active",
		Prefix:    "n2_active",
		CreatedAt: now.Add(-time.Hour),
	}
	repo.keys[1] = memoryAPIKey{APIKey: oldDeleted}
	repo.keys[2] = memoryAPIKey{APIKey: recentDeleted}
	repo.keys[3] = memoryAPIKey{APIKey: active}

	keys, err := service.ListAPIKeys(context.Background())
	if err != nil {
		t.Fatalf("ListAPIKeys returned error: %v", err)
	}

	for _, key := range keys {
		if key.ID == 1 {
			t.Fatalf("old deleted key remained in ListAPIKeys result: %+v", keys)
		}
	}
	if _, ok := repo.keys[1]; ok {
		t.Fatalf("old deleted key remained in repository after purge")
	}
	if _, ok := repo.keys[2]; !ok {
		t.Fatalf("recent deleted key was purged")
	}
	if _, ok := repo.keys[3]; !ok {
		t.Fatalf("active key was purged")
	}
}
```

If `ptrTime` is not already available in the file, add:

```go
func ptrTime(value time.Time) *time.Time {
	return &value
}
```

- [ ] **Step 3: Run the focused service test**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/admin -run TestListAPIKeysPurgesRevokedKeysPastRetention -count=1
```

Expected: FAIL because `Service.ListAPIKeys` does not call purge yet.

- [ ] **Step 4: Implement service cleanup before listing keys**

In `backend/internal/admin/service.go`, update `ListAPIKeys`:

```go
func (s *Service) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	cutoff := time.Now().Add(-APIKeyPhysicalDeleteRetention)
	if _, err := s.repo.PurgeRevokedAPIKeys(ctx, cutoff); err != nil {
		return nil, err
	}
	return s.repo.ListAPIKeys(ctx)
}
```

- [ ] **Step 5: Add the PostgreSQL repository method**

In `backend/internal/store/admin.go`, add near `RevokeAPIKey`:

```go
func (r *AdminRepository) PurgeRevokedAPIKeys(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM client_api_keys
		WHERE revoked_at IS NOT NULL
			AND revoked_at <= $1
	`, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
```

- [ ] **Step 6: Write the store cleanup test**

Add this test after the existing revoke/update API key assertions in `backend/internal/store/admin_test.go`:

```go
func TestPurgeRevokedAPIKeysRemovesOnlyExpiredRevokedKeys(t *testing.T) {
	ctx := context.Background()
	repo := newTestAdminRepository(t)
	oldKey, err := repo.CreateAPIKey(ctx, "old deleted", "hash-old-deleted", "n2_old")
	if err != nil {
		t.Fatalf("CreateAPIKey old returned error: %v", err)
	}
	recentKey, err := repo.CreateAPIKey(ctx, "recent deleted", "hash-recent-deleted", "n2_recent")
	if err != nil {
		t.Fatalf("CreateAPIKey recent returned error: %v", err)
	}
	disabledKey, err := repo.CreateAPIKey(ctx, "disabled", "hash-disabled", "n2_disabled")
	if err != nil {
		t.Fatalf("CreateAPIKey disabled returned error: %v", err)
	}
	activeKey, err := repo.CreateAPIKey(ctx, "active", "hash-active", "n2_active")
	if err != nil {
		t.Fatalf("CreateAPIKey active returned error: %v", err)
	}
	cutoff := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	if _, err := repo.pool.Exec(ctx, `UPDATE client_api_keys SET revoked_at = $2 WHERE id = $1`, oldKey.ID, cutoff.Add(-time.Second)); err != nil {
		t.Fatalf("mark old revoked: %v", err)
	}
	if _, err := repo.pool.Exec(ctx, `UPDATE client_api_keys SET revoked_at = $2 WHERE id = $1`, recentKey.ID, cutoff.Add(time.Second)); err != nil {
		t.Fatalf("mark recent revoked: %v", err)
	}
	if _, err := repo.SetAPIKeyDisabled(ctx, disabledKey.ID, true); err != nil {
		t.Fatalf("SetAPIKeyDisabled returned error: %v", err)
	}

	deleted, err := repo.PurgeRevokedAPIKeys(ctx, cutoff)
	if err != nil {
		t.Fatalf("PurgeRevokedAPIKeys returned error: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	keys, err := repo.ListAPIKeys(ctx)
	if err != nil {
		t.Fatalf("ListAPIKeys returned error: %v", err)
	}
	ids := map[int64]bool{}
	for _, key := range keys {
		ids[key.ID] = true
	}
	if ids[oldKey.ID] {
		t.Fatalf("old revoked key remained after purge")
	}
	if !ids[recentKey.ID] || !ids[disabledKey.ID] || !ids[activeKey.ID] {
		t.Fatalf("remaining keys = %+v, want recent, disabled, and active keys", ids)
	}
}
```

- [ ] **Step 7: Run focused backend tests**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/admin ./internal/store -run 'TestListAPIKeysPurgesRevokedKeysPastRetention|TestPurgeRevokedAPIKeysRemovesOnlyExpiredRevokedKeys' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit Task 2**

Run:

```bash
git add backend/internal/admin/service.go backend/internal/admin/service_test.go backend/internal/store/admin.go backend/internal/store/admin_test.go
git commit -m "feat: purge deleted api keys after retention"
```

## Task 3: API Keys Table Interactions

**Files:**
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/api-keys/+page.svelte`
- Modify: `frontend/src/routes/providers/provider-page.test.mjs`

- [ ] **Step 1: Write failing source-level UI assertions**

In `frontend/src/routes/providers/provider-page.test.mjs`, add this test near the existing API Keys tests:

```js
test('api keys table keeps lifecycle actions in status and action cells', () => {
  assert.match(apiKeysSource, /physicalDeleteAt/);
  assert.match(apiKeysSource, /keyPhysicalDeleteTitle/);
  assert.match(apiKeysSource, /onclick=\{\(\) => setAPIKeyDisabled\(key\.id, !key\.disabledAt\)\}/);
  assert.match(apiKeysSource, /Delete/);
  assert.doesNotMatch(apiKeysSource, /Revoke/);
  assert.doesNotMatch(apiKeysSource, /href=\{`\\/request-logs\\?clientKeyId=\$\{key\.id\}`\}/);
  assert.match(apiKeysSource, /openKeyLogsModal\(key\.id\)/);
  assert.match(apiKeysSource, /aria-label="API key logs"/);
});
```

- [ ] **Step 2: Run the focused failing frontend test**

Run:

```bash
cd frontend && bun test src/routes/providers/provider-page.test.mjs -t "api keys table keeps lifecycle actions in status and action cells"
```

Expected: FAIL because the status column does not yet contain the toggle, the logs link still navigates, and Revoke wording remains.

- [ ] **Step 3: Add frontend API key field documentation**

In `frontend/src/lib/admin-state.svelte.js`, update the `APIKey` JSDoc with:

```js
 * @property {string | null | undefined} physicalDeleteAt
```

- [ ] **Step 4: Add API Keys page state helpers**

In `frontend/src/routes/api-keys/+page.svelte`, add state near `editingKeyId`:

```js
  let logsKeyId = $state(0);
  const logsKey = $derived(apiKeys.items.find((key) => key.id === logsKeyId) ?? null);
```

Add helpers near the other API key helper functions:

```js
  /** @param {import('$lib/admin-state.svelte.js').APIKey} key */
  function keyStatusLabel(key) {
    if (key.revokedAt) return 'Deleted';
    if (key.disabledAt) return 'Disabled';
    return 'Active';
  }

  /** @param {import('$lib/admin-state.svelte.js').APIKey} key */
  function keyPhysicalDeleteTitle(key) {
    if (!key.revokedAt) return keyStatusLabel(key);
    const value = key.physicalDeleteAt ? formatDate(key.physicalDeleteAt) : '30 days after deletion';
    return `Physical delete after ${value}`;
  }

  /** @param {number} keyId */
  function openKeyLogsModal(keyId) {
    logsKeyId = keyId;
  }

  function closeKeyLogsModal() {
    logsKeyId = 0;
  }
```

- [ ] **Step 5: Add the Logs modal markup**

In `frontend/src/routes/api-keys/+page.svelte`, add this modal block before the filters:

```svelte
  {#if logsKey}
    <!-- svelte-ignore a11y_click_events_have_key_events,a11y_no_static_element_interactions,a11y_interactive_supports_focus -->
    <div
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/30 p-4"
      onclick={(e) => e.target === e.currentTarget && closeKeyLogsModal()}
      role="dialog"
      aria-modal="true"
      aria-label="API key logs"
    >
      <div class="w-full max-w-2xl max-h-[calc(100vh-4rem)] overflow-y-auto rounded-lg border border-[#ededed] bg-white p-6 shadow-lg">
        <div class="mb-4 flex items-center justify-between gap-3">
          <div class="min-w-0">
            <h3 class="truncate text-lg font-semibold text-[#0d0d0d]">Logs · {logsKey.name}</h3>
            <p class="mt-1 font-mono text-xs text-[#6e6e6e]">{logsKey.prefix}</p>
          </div>
          <button
            class="rounded-lg border border-[#d9d9d9] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d]"
            type="button"
            onclick={closeKeyLogsModal}
          >
            Close
          </button>
        </div>
        <div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
          <p class="text-sm font-medium text-[#0d0d0d]">Request log preview</p>
          <p class="mt-1 text-sm text-[#6e6e6e]">Log query controls will appear here. This modal keeps API key workflow on the current page.</p>
        </div>
      </div>
    </div>
  {/if}
```

- [ ] **Step 6: Move the enable/disable control into the Status cell**

Replace the current status `<td>` in `frontend/src/routes/api-keys/+page.svelte` with:

```svelte
        <td class="px-4 py-3">
          <div class="flex flex-wrap items-center gap-2" title={keyPhysicalDeleteTitle(key)}>
            <span
              class={[
                'inline-flex rounded-full px-2.5 py-1 text-xs font-medium',
                key.revokedAt
                  ? 'bg-red-50 text-red-700'
                  : key.disabledAt
                    ? 'bg-amber-50 text-amber-700'
                  : 'bg-[#e8f5f0] text-[#0a7a5e]'
              ]}
            >
              {keyStatusLabel(key)}
            </span>
            {#if !key.revokedAt}
              <button
                class="rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]"
                type="button"
                onclick={() => setAPIKeyDisabled(key.id, !key.disabledAt)}
              >
                {key.disabledAt ? 'Enable' : 'Disable'}
              </button>
            {/if}
          </div>
        </td>
```

- [ ] **Step 7: Update the Action cell**

Replace the Logs link and revoke button in `frontend/src/routes/api-keys/+page.svelte` with:

```svelte
          <button
            class="mr-2 inline-flex rounded-lg border border-[#e5e5e5] bg-white px-3 py-1.5 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]"
            type="button"
            onclick={() => openKeyLogsModal(key.id)}
            title="View request logs"
            aria-label="View request logs"
          >
            Logs
          </button>
          <button
            class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-1.5 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
            type="button"
            disabled={Boolean(key.revokedAt)}
            onclick={() => revokeKey(key.id)}
          >
            Delete
          </button>
```

Remove the old separate Enable/Disable button from the Action cell.

- [ ] **Step 8: Run focused frontend test**

Run:

```bash
cd frontend && bun test src/routes/providers/provider-page.test.mjs -t "api keys table keeps lifecycle actions in status and action cells"
```

Expected: PASS.

- [ ] **Step 9: Commit Task 3**

Run:

```bash
git add frontend/src/lib/admin-state.svelte.js frontend/src/routes/api-keys/+page.svelte frontend/src/routes/providers/provider-page.test.mjs
git commit -m "feat: refine api key lifecycle table"
```

## Task 4: Documentation And Regression Coverage

**Files:**
- Modify: `README.md`
- Modify: `deploy/README.md`
- Modify: `backend/internal/gateway/documentation_test.go`
- Modify: `frontend/src/routes/providers/provider-page.test.mjs`

- [ ] **Step 1: Update docs wording**

In `README.md` and `deploy/README.md`, replace the API key temporary disable/re-enable wording with:

```markdown
API keys have three visible states: active, disabled, and deleted. Active and disabled keys can be toggled directly from the API Keys table status column. Deleting a key performs a logical delete immediately, keeps the row visible during its 30 day retention window, and exposes the scheduled physical deletion time in the deleted status tooltip. Keys past the retention window are physically removed during API key listing cleanup.
```

- [ ] **Step 2: Update documentation tests**

In `backend/internal/gateway/documentation_test.go`, update the API key documentation expectations so the test checks for:

```go
"API keys have three visible states",
"30 day retention window",
"physically removed during API key listing cleanup",
```

Keep existing expectations for disabled keys where they still describe authentication behavior.

- [ ] **Step 3: Run documentation test**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run TestDocumentation -count=1
```

Expected: PASS.

- [ ] **Step 4: Run API Keys source-level test file**

Run:

```bash
cd frontend && bun test src/routes/providers/provider-page.test.mjs
```

Expected: PASS.

- [ ] **Step 5: Commit Task 4**

Run:

```bash
git add README.md deploy/README.md backend/internal/gateway/documentation_test.go frontend/src/routes/providers/provider-page.test.mjs
git commit -m "docs: document api key lifecycle states"
```

## Task 5: Final Verification And Local Stack Refresh

**Files:**
- Read-only verification across changed files.

- [ ] **Step 1: Run backend tests**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
```

Expected: PASS.

- [ ] **Step 2: Run frontend checks**

Run:

```bash
cd frontend && bun run check
```

Expected: PASS.

- [ ] **Step 3: Run frontend build**

Run:

```bash
cd frontend && bun run build
```

Expected: PASS.

- [ ] **Step 4: Run frontend tests**

Run:

```bash
cd frontend && bun test
```

Expected: PASS.

- [ ] **Step 5: Refresh Docker Compose stack**

Use the `n2api-refresh-docker` skill. Expected command shape:

```bash
docker compose -f deploy/compose.yaml up -d --build n2api
docker compose -f deploy/compose.yaml ps
docker exec deploy-n2api-1 wget -qO- http://127.0.0.1:3000/healthz
```

Expected: the `n2api` service is recreated and the health check returns an OK response.

- [ ] **Step 6: Inspect git status**

Run:

```bash
git status --short
```

Expected: clean worktree after committed code/doc changes and no generated artifacts staged.
