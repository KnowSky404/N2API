# Routing Preview Runtime Concurrency Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enrich Routing diagnostics preview candidates with active account concurrency and the effective account concurrency cap.

**Architecture:** Keep provider-service selection preview unchanged. The admin HTTP layer maps `provider.SelectionPreview` into an enriched response using Gateway Settings plus the optional gateway `AccountConcurrencySnapshot` interface. The models page renders the new candidate fields as compact runtime diagnostics.

**Tech Stack:** Go backend, net/http admin API, SvelteKit admin UI, Bun tests.

---

### Task 1: Enrich Preview Candidate JSON

**Files:**
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/server_test.go`

- [ ] **Step 1: Write failing HTTP test**

Add a focused test after `TestModelRoutingPreviewReturnsSessionAwareSelection`:

```go
func TestModelRoutingPreviewIncludesConcurrencyState(t *testing.T) {
	admins := newFakeAdminService()
	admins.gatewaySettings.MaxConcurrentRequestsPerAccount = 5
	providers := newFakeProviderService()
	providers.selectionPreview = provider.SelectionPreview{
		Model:             "gpt-5",
		SelectedAccountID: 7,
		Candidates: []provider.SelectionCandidate{
			{ID: 7, DisplayName: "Busy", AccountType: provider.AccountTypeAPIUpstream, Priority: 1, ScheduleRank: 1, Selected: true},
			{ID: 8, DisplayName: "Inherited", AccountType: provider.AccountTypeCodexOAuth, Priority: 2, ScheduleRank: 2},
		},
	}
	providers.accounts = []provider.Account{
		{ID: 7, Provider: "openai", DisplayName: "Busy", MaxConcurrentRequests: 2},
		{ID: 8, Provider: "openai", DisplayName: "Inherited"},
	}
	gateway := &fakeGatewayHandler{accountConcurrency: map[int64]int{7: 2, 8: 1}}
	server := NewServer(config.Config{}, staticHealth{}, admins, providers, gateway)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/model-routing/preview?model=gpt-5", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Candidates []struct {
			ID                             int64 `json:"id"`
			CurrentConcurrentRequests      int   `json:"currentConcurrentRequests"`
			EffectiveMaxConcurrentRequests int   `json:"effectiveMaxConcurrentRequests"`
			ConcurrencyBlocked             bool  `json:"concurrencyBlocked"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Candidates) != 2 {
		t.Fatalf("candidates = %+v, want two", body.Candidates)
	}
	if body.Candidates[0].CurrentConcurrentRequests != 2 || body.Candidates[0].EffectiveMaxConcurrentRequests != 2 || !body.Candidates[0].ConcurrencyBlocked {
		t.Fatalf("first candidate concurrency = %+v, want current 2 effective 2 blocked", body.Candidates[0])
	}
	if body.Candidates[1].CurrentConcurrentRequests != 1 || body.Candidates[1].EffectiveMaxConcurrentRequests != 5 || body.Candidates[1].ConcurrencyBlocked {
		t.Fatalf("second candidate concurrency = %+v, want current 1 effective 5 not blocked", body.Candidates[1])
	}
}
```

- [ ] **Step 2: Run red test**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run ModelRoutingPreviewIncludesConcurrencyState
```

Expected: FAIL because preview candidates do not include the new fields.

- [ ] **Step 3: Implement HTTP enrichment**

Add response DTOs in `backend/internal/httpapi/server.go`:

```go
type selectionPreviewResponse struct {
	provider.SelectionPreview
	Candidates []selectionCandidateResponse `json:"candidates"`
}

type selectionCandidateResponse struct {
	provider.SelectionCandidate
	CurrentConcurrentRequests      int  `json:"currentConcurrentRequests"`
	EffectiveMaxConcurrentRequests int  `json:"effectiveMaxConcurrentRequests"`
	ConcurrencyBlocked             bool `json:"concurrencyBlocked"`
}
```

Update the model-routing preview route to call:

```go
handleModelRoutingPreview(w, r, admins, providers, accountConcurrencySource)
```

Load settings and accounts in the handler, read the concurrency snapshot, and write `selectionPreviewResponse`.

- [ ] **Step 4: Run HTTP preview tests**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run ModelRoutingPreview
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go
git commit -m "feat: show preview account concurrency state"
```

### Task 2: Render Preview Runtime Concurrency

**Files:**
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/models/+page.svelte`
- Modify: `frontend/src/routes/navigation.test.mjs`

- [ ] **Step 1: Write failing frontend test**

In `test('models page shows scheduling diagnostics for routing candidates'...)`, add assertions:

```js
assert.match(adminState, /currentConcurrentRequests/);
assert.match(adminState, /effectiveMaxConcurrentRequests/);
assert.match(adminState, /concurrencyBlocked/);
assert.match(modelsPage, /previewConcurrencyLimitLabel/);
assert.match(modelsPage, /Active/);
assert.match(modelsPage, /Concurrency full/);
```

- [ ] **Step 2: Run red frontend test**

Run:

```bash
cd frontend && bun test src/routes/navigation.test.mjs
```

Expected: FAIL because the new fields and preview text are missing.

- [ ] **Step 3: Implement frontend rendering**

Add JSDoc fields to `SelectionPreviewCandidate`:

```js
 * @property {number} currentConcurrentRequests
 * @property {number} effectiveMaxConcurrentRequests
 * @property {boolean} concurrencyBlocked
```

Add a helper in `frontend/src/routes/models/+page.svelte`:

```js
function previewConcurrencyLimitLabel(value) {
  const limit = Number(value ?? 0);
  return limit > 0 ? String(limit) : 'unlimited';
}
```

In preview candidate chips, render:

```svelte
<span>Active {account.currentConcurrentRequests || 0} / {previewConcurrencyLimitLabel(account.effectiveMaxConcurrentRequests)}</span>
{#if account.concurrencyBlocked}
  <span class="font-medium text-amber-800">Concurrency full</span>
{/if}
```

- [ ] **Step 4: Run frontend tests and checks**

Run:

```bash
cd frontend && bun test src/routes/navigation.test.mjs
cd frontend && bun run check
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/admin-state.svelte.js frontend/src/routes/models/+page.svelte frontend/src/routes/navigation.test.mjs
git commit -m "feat: render preview concurrency diagnostics"
```

### Task 3: Document Preview Runtime Concurrency

**Files:**
- Modify: `backend/internal/gateway/documentation_test.go`
- Modify: `README.md`
- Modify: `deploy/README.md`

- [ ] **Step 1: Write failing documentation test**

Add:

```go
func TestGatewayDocumentationMentionsRoutingPreviewConcurrency(t *testing.T) {
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Routing preview",
			"active concurrency",
			"Concurrency full",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in routing preview concurrency documentation", path, want)
			}
		}
	}
}
```

- [ ] **Step 2: Run red documentation test**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run RoutingPreviewConcurrency
```

Expected: FAIL because docs do not mention Routing preview concurrency.

- [ ] **Step 3: Update README files**

Add one sentence near Routing diagnostics:

```markdown
Routing preview also shows each candidate's active concurrency and effective account cap; candidates at a positive cap are marked **Concurrency full**.
```

- [ ] **Step 4: Run documentation test**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run RoutingPreviewConcurrency
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/gateway/documentation_test.go README.md deploy/README.md
git commit -m "docs: document routing preview concurrency"
```
