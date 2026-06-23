# Persistent Sticky Sessions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist sticky-session routing decisions so a given provider/model/session tuple keeps using the same schedulable provider account.

**Architecture:** Add a small PostgreSQL repository surface for `provider_session_bindings`, wire provider selection to prefer a valid stored binding before the existing hash-based order, and expose binding status through routing preview. The gateway call shape stays the same; fallback works by passing excluded account IDs and letting the provider service rebind after successful credential preparation.

**Tech Stack:** Go backend, PostgreSQL migrations, SvelteKit admin UI, Bun tests, existing provider/gateway/admin APIs.

---

### Task 1: Store Provider Session Bindings

**Files:**
- Create: `backend/internal/store/migrations/00020_provider_session_bindings.sql`
- Modify: `backend/internal/store/migrations_test.go`
- Modify: `backend/internal/store/provider.go`
- Test: `backend/internal/store/provider_test.go`

- [ ] **Step 1: Write the failing migration test**

Add `TestProviderSessionBindingsMigration` to `backend/internal/store/migrations_test.go`:

```go
func TestProviderSessionBindingsMigration(t *testing.T) {
	sql, err := MigrationSQL("00020_provider_session_bindings.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS provider_session_bindings",
		"provider_session_bindings_provider_model_session_unique",
		"UNIQUE (provider, model, session_id)",
		"REFERENCES provider_accounts(id) ON DELETE CASCADE",
		"provider_session_bindings_provider_account_idx",
		"DROP TABLE IF EXISTS provider_session_bindings",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}
```

Update `TestMigrationSourcesOrdered` to expect `sources[19].Path == "00020_provider_session_bindings.sql"` and `len(sources) == 20`.

- [ ] **Step 2: Run the migration test red**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store -run ProviderSessionBindingsMigration
```

Expected: FAIL because `00020_provider_session_bindings.sql` does not exist.

- [ ] **Step 3: Add the migration**

Create `backend/internal/store/migrations/00020_provider_session_bindings.sql`:

```sql
-- +goose Up
CREATE TABLE IF NOT EXISTS provider_session_bindings (
    id BIGSERIAL PRIMARY KEY,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    session_id TEXT NOT NULL,
    account_id BIGINT NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
    last_used_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT provider_session_bindings_model_non_empty CHECK (length(trim(model)) > 0),
    CONSTRAINT provider_session_bindings_session_id_non_empty CHECK (length(trim(session_id)) > 0),
    CONSTRAINT provider_session_bindings_provider_model_session_unique UNIQUE (provider, model, session_id)
);

CREATE INDEX IF NOT EXISTS provider_session_bindings_provider_account_idx
    ON provider_session_bindings (provider, account_id);

-- +goose Down
DROP TABLE IF EXISTS provider_session_bindings;
```

- [ ] **Step 4: Add repository method tests**

Add store tests that verify:

```go
func TestProviderRepositoryUpsertsAndFindsSessionBinding(t *testing.T) {
	ctx := context.Background()
	repo := newTestProviderRepository(t)
	account := testStoredProviderAccount(t, 0, "openai", "subject-1")
	saved, err := repo.SaveAccount(ctx, account)
	if err != nil {
		t.Fatalf("SaveAccount returned error: %v", err)
	}
	if err := repo.UpsertSessionBinding(ctx, "openai", "gpt-5", "workspace-123", saved.ID); err != nil {
		t.Fatalf("UpsertSessionBinding returned error: %v", err)
	}
	binding, err := repo.FindSessionBinding(ctx, "openai", "gpt-5", "workspace-123")
	if err != nil {
		t.Fatalf("FindSessionBinding returned error: %v", err)
	}
	if binding.AccountID != saved.ID || binding.Model != "gpt-5" || binding.SessionID != "workspace-123" {
		t.Fatalf("binding = %+v, want saved account binding", binding)
	}
}
```

- [ ] **Step 5: Implement repository methods**

Add to `backend/internal/provider/service.go` if not already placed there:

```go
type SessionBinding struct {
	ID         int64     `json:"id"`
	Provider   string    `json:"provider"`
	Model      string    `json:"model"`
	SessionID  string    `json:"sessionId"`
	AccountID  int64     `json:"accountId"`
	LastUsedAt time.Time `json:"lastUsedAt"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}
```

Add repository interface methods:

```go
FindSessionBinding(ctx context.Context, providerName, model, sessionID string) (SessionBinding, error)
UpsertSessionBinding(ctx context.Context, providerName, model, sessionID string, accountID int64) error
```

Implement in `backend/internal/store/provider.go` using `pgx.ErrNoRows` mapped to a provider-level `ErrSessionBindingNotFound`.

- [ ] **Step 6: Run store tests green**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/store/migrations/00020_provider_session_bindings.sql backend/internal/store/migrations_test.go backend/internal/store/provider.go backend/internal/store/provider_test.go backend/internal/provider/service.go
git commit -m "feat: store provider session bindings"
```

### Task 2: Prefer And Rebind Stored Sticky Sessions

**Files:**
- Modify: `backend/internal/provider/service.go`
- Modify: `backend/internal/provider/service_test.go`

- [ ] **Step 1: Write failing provider tests**

Add tests proving:

```go
func TestSelectAccountForModelAndSessionPersistsAndReusesBinding(t *testing.T) {
	// First call selects an account and creates a binding.
	// Then LastUsedAt ordering changes.
	// Second call for same model/session returns the bound account.
}

func TestSelectAccountForModelAndSessionRebindsWhenBoundAccountExcluded(t *testing.T) {
	// Seed a binding to account 1.
	// Call selection with excluded account 1.
	// It selects account 2 and updates the binding to account 2.
}

func TestSelectAccountForModelDoesNotCreateSessionBinding(t *testing.T) {
	// Non-session selection succeeds and memory repo binding map remains empty.
}
```

Update the memory repo to track session bindings and fail tests until service methods call it.

- [ ] **Step 2: Run provider sticky tests red**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/provider -run 'Session.*Binding|DoesNotCreateSessionBinding'
```

Expected: FAIL because selection does not persist or reuse bindings.

- [ ] **Step 3: Implement stored binding preference**

In `SelectAccountForModelAndSession`, after `selectionCandidates`, call a helper:

```go
accounts, bindingAccountID, err := s.stickySessionCandidates(ctx, accounts, model, sessionID)
if err != nil {
	return SelectedAccount{}, err
}
selected, err := s.selectFromCandidates(ctx, accounts, hasEnabled, notFoundErr)
if err != nil {
	return SelectedAccount{}, err
}
if err := s.repo.UpsertSessionBinding(ctx, s.cfg.Provider, strings.TrimSpace(model), sessionID, selected.AccountID); err != nil {
	return SelectedAccount{}, fmt.Errorf("upsert provider session binding: %w", err)
}
selected.StickyBound = bindingAccountID == selected.AccountID
return selected, nil
```

Use existing hash order only when no stored binding is present in the schedulable candidate list.

- [ ] **Step 4: Run provider tests green**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/provider -run 'Session.*Binding|DoesNotCreateSessionBinding'
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/provider
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/provider/service.go backend/internal/provider/service_test.go
git commit -m "feat: persist sticky session selection"
```

### Task 3: Expose Sticky Binding In Preview

**Files:**
- Modify: `backend/internal/provider/service.go`
- Modify: `backend/internal/provider/service_test.go`
- Modify: `backend/internal/httpapi/server_test.go`
- Modify: `frontend/src/lib/admin-state.svelte.js`
- Modify: `frontend/src/routes/models/+page.svelte`
- Modify: `frontend/src/routes/navigation.test.mjs`

- [ ] **Step 1: Write failing preview tests**

Extend provider preview test to seed a binding and assert:

```go
if preview.StickyBoundAccountID != 2 {
	t.Fatalf("StickyBoundAccountID = %d, want 2", preview.StickyBoundAccountID)
}
if !preview.Candidates[0].StickyBound {
	t.Fatalf("first candidate = %+v, want sticky-bound marker", preview.Candidates[0])
}
```

Extend HTTP preview test to assert JSON contains `stickyBoundAccountId` and candidate `stickyBound`.

Extend frontend navigation/source test to assert the models page renders `Sticky bound`.

- [ ] **Step 2: Run preview tests red**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/provider -run PreviewAccountSelection
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run ModelRoutingPreview
cd frontend && bun test src/routes/navigation.test.mjs
```

Expected: FAIL because preview DTO/UI does not expose sticky binding fields.

- [ ] **Step 3: Implement preview fields**

Add:

```go
StickyBoundAccountID int64 `json:"stickyBoundAccountId,omitempty"`
StickyBound          bool  `json:"stickyBound"`
```

Use the same binding-aware ordering helper in `PreviewAccountSelection`, but do not call `UpsertSessionBinding`.

In Svelte state JSDoc, include `stickyBoundAccountId` on preview and `stickyBound` on candidates. In `frontend/src/routes/models/+page.svelte`, render a compact `Sticky bound` label when `candidate.stickyBound` is true.

- [ ] **Step 4: Run preview tests green**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/provider -run PreviewAccountSelection
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run ModelRoutingPreview
cd frontend && bun test src/routes/navigation.test.mjs
cd frontend && bun run check
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/provider/service.go backend/internal/provider/service_test.go backend/internal/httpapi/server_test.go frontend/src/lib/admin-state.svelte.js frontend/src/routes/models/+page.svelte frontend/src/routes/navigation.test.mjs
git commit -m "feat: show sticky session bindings in routing preview"
```

### Task 4: Document Persistent Sticky Sessions

**Files:**
- Modify: `backend/internal/gateway/documentation_test.go`
- Modify: `README.md`
- Modify: `deploy/README.md`

- [ ] **Step 1: Write failing documentation test**

Add:

```go
func TestGatewayDocumentationMentionsPersistentStickySessionBindings(t *testing.T) {
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"persisted by provider, model, and `session_id`",
			"bound account",
			"rebind",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in persistent sticky session documentation", path, want)
			}
		}
	}
}
```

- [ ] **Step 2: Run documentation test red**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run PersistentStickySessionBindings
```

Expected: FAIL because docs do not mention persisted bindings.

- [ ] **Step 3: Update docs**

In README and deploy README sticky-session sections, add:

```markdown
Sticky session bindings are persisted by provider, model, and `session_id`. A healthy bound account is reused while it remains schedulable; if fallback excludes it before streaming starts, the successful fallback account can rebind that session.
```

- [ ] **Step 4: Run docs test green**

Run:

```bash
cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/gateway -run PersistentStickySessionBindings
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/gateway/documentation_test.go README.md deploy/README.md
git commit -m "docs: document persistent sticky sessions"
```

### Final Verification

- [ ] Run `git diff --check`.
- [ ] Run `cd backend && GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...`.
- [ ] Run `cd frontend && bun test src/routes/navigation.test.mjs`.
- [ ] Run `cd frontend && bun run check`.
- [ ] Run `cd frontend && bun run build`.
- [ ] Run `git status --short` and confirm the worktree is clean.
