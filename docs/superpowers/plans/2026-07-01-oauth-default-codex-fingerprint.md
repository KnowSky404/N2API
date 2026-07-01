# OAuth Default Codex Fingerprint Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` for inline execution tracking. For delegated scanning or code execution, use the user's local Codex agent configuration: `deepseek-flash` for read-only scans and `deepseek-worker` for bounded implementation work. Do not rely on Superpowers-derived subagents as the implementation default.

**Goal:** Make Codex OAuth accounts bind to a durable built-in `Default Codex CLI` fingerprint profile when OAuth connect does not specify a custom fingerprint profile, while preserving explicit custom profile selection and existing account profile choices.

**Architecture:** Add a stable system key for built-in fingerprint profiles, ensure one default Codex CLI profile at OAuth connect time, store the resolved profile id in OAuth state, persist `provider_accounts.fingerprint_profile_id` through account save/load paths, and adjust the admin OAuth add UI to label the default behavior clearly.

**Tech Stack:** Go backend, PostgreSQL/goose migrations, pgx, SvelteKit/Tailwind admin UI, Bun, Docker Compose.

**Execution Constraint:** The main `gpt-5.5` session owns architecture, ambiguity resolution, code review, final verification, and Docker refresh. If implementation is delegated, run local Codex agents with these command shapes:

```bash
codex exec --sandbox read-only --cd /root/Clouds/N2API -m deepseek/deepseek-v4-flash -c model_context_window=1000000 -c model_auto_compact_token_limit=900000 -c model_reasoning_effort='"high"' "<read-only task prompt>"
```

```bash
codex exec --sandbox workspace-write --cd /root/Clouds/N2API -m deepseek/deepseek-v4-pro -c model_context_window=1000000 -c model_auto_compact_token_limit=900000 -c model_reasoning_effort='"max"' "<bounded implementation task prompt>"
```

---

## File Structure

- Create `backend/internal/store/migrations/00032_fingerprint_profile_system_key.sql`: add `fingerprint_profiles.system_key` and a partial unique index.
- Modify `backend/internal/store/migrations_test.go`: assert migration embedding and SQL contract.
- Modify `backend/internal/provider/service.go`: add default Codex fingerprint constants, repository method, and OAuth connect default resolution.
- Modify `backend/internal/provider/service_test.go`: cover default resolution, custom profile precedence, callback preservation, and target reauthorization update.
- Modify `backend/internal/store/provider.go`: ensure built-in default profile and persist `fingerprint_profile_id` on account reads/inserts/updates.
- Modify `backend/internal/store/provider_test.go`: cover default profile SQL contract and account fingerprint persistence contract.
- Modify `backend/internal/httpapi/server_test.go`: cover the `0` fingerprint sentinel as default selection if current coverage is not explicit.
- Modify `frontend/src/routes/providers/+page.svelte`: label OAuth add default as `Default Codex CLI`; keep account edit `None`.
- Modify `frontend/src/routes/providers/provider-page.test.mjs`: cover the add-form default label and edit-form clear option.

## Task 1: Add Stable Built-In Fingerprint Profile Key

**Files:**
- Create: `backend/internal/store/migrations/00032_fingerprint_profile_system_key.sql`
- Modify: `backend/internal/store/migrations_test.go`

- [ ] **Step 1: Write the migration contract test**

Add this test to `backend/internal/store/migrations_test.go`:

```go
func TestFingerprintProfileSystemKeyMigrationIsEmbedded(t *testing.T) {
	sql, err := MigrationSQL("00032_fingerprint_profile_system_key.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"ALTER TABLE fingerprint_profiles ADD COLUMN IF NOT EXISTS system_key TEXT NOT NULL DEFAULT ''",
		"CREATE UNIQUE INDEX IF NOT EXISTS fingerprint_profiles_system_key_unique_idx",
		"ON fingerprint_profiles (system_key)",
		"WHERE system_key <> ''",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Run the failing targeted test**

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store -run TestFingerprintProfileSystemKeyMigrationIsEmbedded
```

Expected result: fails until the migration file exists.

- [ ] **Step 3: Add the migration**

Create `backend/internal/store/migrations/00032_fingerprint_profile_system_key.sql`:

```sql
-- +goose Up
ALTER TABLE fingerprint_profiles ADD COLUMN IF NOT EXISTS system_key TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS fingerprint_profiles_system_key_unique_idx
    ON fingerprint_profiles (system_key)
    WHERE system_key <> '';

-- +goose Down
DROP INDEX IF EXISTS fingerprint_profiles_system_key_unique_idx;

ALTER TABLE fingerprint_profiles DROP COLUMN IF EXISTS system_key;
```

- [ ] **Step 4: Run the targeted migration test again**

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store -run TestFingerprintProfileSystemKeyMigrationIsEmbedded
```

- [ ] **Step 5: Commit this schema change**

```bash
git add backend/internal/store/migrations/00032_fingerprint_profile_system_key.sql backend/internal/store/migrations_test.go
git commit -m "feat: add fingerprint profile system key"
```

## Task 2: Add Default Codex Fingerprint Domain And Store Primitive

**Files:**
- Modify: `backend/internal/provider/service.go`
- Modify: `backend/internal/store/provider.go`
- Modify: `backend/internal/store/provider_test.go`
- Modify: `backend/internal/provider/service_test.go`

- [ ] **Step 1: Add provider constants and repository contract**

In `backend/internal/provider/service.go`, add exported constants near provider/account constants:

```go
const (
	DefaultCodexFingerprintSystemKey   = "codex_cli_default"
	DefaultCodexFingerprintName        = "Default Codex CLI"
	DefaultCodexFingerprintDescription = "Built-in Codex CLI-style outbound identity for OAuth accounts."
	DefaultCodexFingerprintUserAgent   = "codex_cli_rs/0.125.0 (Ubuntu 22.4.0; x86_64) xterm-256color"
	DefaultCodexFingerprintTLS         = ""
)

func DefaultCodexFingerprintHeaders() map[string]string {
	return map[string]string{
		"originator": "codex_cli_rs",
	}
}
```

Extend the provider repository interface:

```go
EnsureDefaultCodexFingerprintProfile(ctx context.Context) (int64, error)
```

- [ ] **Step 2: Add store SQL contract coverage**

Add a source-contract test to `backend/internal/store/provider_test.go`:

```go
func TestEnsureDefaultCodexFingerprintProfileUsesSystemKey(t *testing.T) {
	source := readProviderStoreSource(t)
	for _, want := range []string{
		"func (r *ProviderRepository) EnsureDefaultCodexFingerprintProfile",
		"provider.DefaultCodexFingerprintSystemKey",
		"ON CONFLICT (system_key) WHERE system_key <> ''",
		"DO UPDATE SET enabled = true",
		"RETURNING id",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("provider store source missing %q", want)
		}
	}
}
```

If `readProviderStoreSource` does not exist, add it once in the same test file:

```go
func readProviderStoreSource(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("provider.go")
	if err != nil {
		t.Fatalf("read provider.go: %v", err)
	}
	return string(data)
}
```

- [ ] **Step 3: Implement default profile ensure**

In `backend/internal/store/provider.go`, add:

```go
func (r *ProviderRepository) EnsureDefaultCodexFingerprintProfile(ctx context.Context) (int64, error) {
	headersJSON, err := json.Marshal(provider.DefaultCodexFingerprintHeaders())
	if err != nil {
		return 0, err
	}

	var id int64
	err = r.pool.QueryRow(ctx, `
		INSERT INTO fingerprint_profiles (
			system_key, name, description, user_agent, tls_fingerprint, headers_json, enabled
		)
		VALUES ($1, $2, $3, $4, $5, $6, true)
		ON CONFLICT (system_key) WHERE system_key <> ''
		DO UPDATE SET enabled = true, updated_at = now()
		RETURNING id
	`,
		provider.DefaultCodexFingerprintSystemKey,
		provider.DefaultCodexFingerprintName,
		provider.DefaultCodexFingerprintDescription,
		provider.DefaultCodexFingerprintUserAgent,
		provider.DefaultCodexFingerprintTLS,
		headersJSON,
	).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}
```

- [ ] **Step 4: Update provider service test fakes**

In `backend/internal/provider/service_test.go`, add fields and method to the in-memory repository:

```go
defaultFingerprintProfileID          int64
ensureDefaultFingerprintProfileCalls int
```

```go
func (r *memoryRepo) EnsureDefaultCodexFingerprintProfile(ctx context.Context) (int64, error) {
	r.ensureDefaultFingerprintProfileCalls++
	if r.defaultFingerprintProfileID == 0 {
		r.defaultFingerprintProfileID = 9001
	}
	if r.fingerprintProfiles == nil {
		r.fingerprintProfiles = make(map[int64]provider.FingerprintProfileData)
	}
	r.fingerprintProfiles[r.defaultFingerprintProfileID] = provider.FingerprintProfileData{
		UserAgent: provider.DefaultCodexFingerprintUserAgent,
		Headers:   provider.DefaultCodexFingerprintHeaders(),
	}
	return r.defaultFingerprintProfileID, nil
}
```

- [ ] **Step 5: Run targeted compile/tests**

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/provider ./internal/store -run 'EnsureDefaultCodexFingerprintProfile|TestStartConnect'
```

- [ ] **Step 6: Commit this domain/store primitive**

```bash
git add backend/internal/provider/service.go backend/internal/provider/service_test.go backend/internal/store/provider.go backend/internal/store/provider_test.go
git commit -m "feat: ensure default codex fingerprint profile"
```

## Task 3: Persist Account Fingerprint Profile IDs End To End

**Files:**
- Modify: `backend/internal/store/provider.go`
- Modify: `backend/internal/store/provider_test.go`

- [ ] **Step 1: Add persistence contract coverage**

Add a test in `backend/internal/store/provider_test.go`:

```go
func TestProviderAccountSaveAndScanIncludeFingerprintProfileID(t *testing.T) {
	source := readProviderStoreSource(t)
	for _, want := range []string{
		"a.fingerprint_profile_id",
		"&account.FingerprintProfileID",
		"fingerprint_profile_id = $19",
		"fingerprint_profile_id",
		"account.FingerprintProfileID",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("provider store source missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Extend account select columns and scans**

Add `a.fingerprint_profile_id` to provider account select column constants in `backend/internal/store/provider.go`, and add `&account.FingerprintProfileID` to the corresponding scan order.

The select list must contain this field before `created_at` or at another stable position that matches the scan:

```sql
a.fingerprint_profile_id,
a.created_at,
a.updated_at,
...
```

The scan must include:

```go
&account.FingerprintProfileID,
```

- [ ] **Step 3: Persist ID update path**

In the `SaveAccount` branch that updates by existing account id, add:

```sql
fingerprint_profile_id = $19,
```

Pass `account.FingerprintProfileID` as the matching argument.

- [ ] **Step 4: Persist insert path**

In the `SaveAccount` insert statement, add `fingerprint_profile_id` to the inserted columns and add a matching value parameter:

```sql
fingerprint_profile_id,
updated_at
```

```sql
$18,
now()
```

Pass `account.FingerprintProfileID` as argument `$18`.

Do not add `fingerprint_profile_id` to the duplicate-identity conflict update. Identity reconnect without target account id must preserve the existing account profile.

- [ ] **Step 5: Run targeted store tests**

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store -run 'ProviderAccountSaveAndScanIncludeFingerprintProfileID|UpdateAccountCanClearFingerprintProfileColumn|EnsureDefaultCodexFingerprintProfile'
```

- [ ] **Step 6: Commit persistence fix**

```bash
git add backend/internal/store/provider.go backend/internal/store/provider_test.go
git commit -m "fix: persist provider account fingerprint profile"
```

## Task 4: Resolve Default Profile During OAuth Connect

**Files:**
- Modify: `backend/internal/provider/service.go`
- Modify: `backend/internal/provider/service_test.go`

- [ ] **Step 1: Add service tests**

Add a test that omitted fingerprint selection resolves the default:

```go
func TestStartConnectStoresDefaultFingerprintProfileWhenUnset(t *testing.T) {
	repo := newMemoryRepo()
	svc := newTestService(t, repo)

	result, err := svc.StartConnect(context.Background(), provider.ConnectOptions{
		Name: "default-fingerprint-account",
	})
	if err != nil {
		t.Fatalf("StartConnect returned error: %v", err)
	}

	state := repo.states[result.State]
	if state.PendingFingerprintProfileID == nil {
		t.Fatalf("PendingFingerprintProfileID is nil")
	}
	if got, want := *state.PendingFingerprintProfileID, repo.defaultFingerprintProfileID; got != want {
		t.Fatalf("PendingFingerprintProfileID = %d, want %d", got, want)
	}
	if repo.ensureDefaultFingerprintProfileCalls != 1 {
		t.Fatalf("ensureDefaultFingerprintProfileCalls = %d, want 1", repo.ensureDefaultFingerprintProfileCalls)
	}
}
```

Update the existing custom profile test to assert that custom selection still wins and the default ensure method is not called:

```go
if repo.ensureDefaultFingerprintProfileCalls != 0 {
	t.Fatalf("ensureDefaultFingerprintProfileCalls = %d, want 0", repo.ensureDefaultFingerprintProfileCalls)
}
```

- [ ] **Step 2: Implement default resolution**

In `StartConnect`, replace direct assignment of `options.FingerprintProfileID` with resolved logic:

```go
fingerprintProfileID := options.FingerprintProfileID
if fingerprintProfileID != nil {
	if *fingerprintProfileID <= 0 {
		return ConnectResult{}, fmt.Errorf("fingerprint profile id must be positive")
	}
	if _, err := s.repo.FindFingerprintProfileByID(ctx, *fingerprintProfileID); err != nil {
		return ConnectResult{}, err
	}
} else {
	id, err := s.repo.EnsureDefaultCodexFingerprintProfile(ctx)
	if err != nil {
		return ConnectResult{}, err
	}
	fingerprintProfileID = &id
}
```

Use `fingerprintProfileID` when creating `OAuthState`:

```go
PendingFingerprintProfileID: fingerprintProfileID,
```

- [ ] **Step 3: Run targeted provider tests**

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/provider -run 'TestStartConnectStoresDefaultFingerprintProfileWhenUnset|TestStartConnectStoresPendingAccountOptionsAndFingerprintHashes'
```

- [ ] **Step 4: Commit OAuth connect default resolution**

```bash
git add backend/internal/provider/service.go backend/internal/provider/service_test.go
git commit -m "feat: default codex oauth fingerprint on connect"
```

## Task 5: Cover Callback Preservation And Target Reauthorization

**Files:**
- Modify: `backend/internal/provider/service_test.go`

- [ ] **Step 1: Add existing-identity preservation test**

Add a provider service test that creates an existing Codex OAuth account with `FingerprintProfileID = 11`, starts OAuth connect without a target account, completes callback for the same identity, and asserts the account still has profile id `11` after save.

The final assertion must be:

```go
if account.FingerprintProfileID == nil || *account.FingerprintProfileID != 11 {
	t.Fatalf("FingerprintProfileID = %v, want 11", account.FingerprintProfileID)
}
```

- [ ] **Step 2: Add target reauthorization update test**

Add a provider service test that creates a target account with `FingerprintProfileID = 11`, starts reauthorization with `TargetAccountID` set and no custom fingerprint selection, completes callback, and asserts the account uses the ensured default id.

The final assertion must be:

```go
if account.FingerprintProfileID == nil || *account.FingerprintProfileID != repo.defaultFingerprintProfileID {
	t.Fatalf("FingerprintProfileID = %v, want %d", account.FingerprintProfileID, repo.defaultFingerprintProfileID)
}
```

- [ ] **Step 3: Adjust implementation only if tests expose a gap**

The callback builder should already follow this rule:

```go
if state.PendingFingerprintProfileID != nil && (previous == nil || state.TargetAccountID > 0) {
	account.FingerprintProfileID = state.PendingFingerprintProfileID
}
```

If tests fail, keep the fix scoped to that conditional or to store persistence. Do not overwrite existing-identity profile choices during ordinary reconnect.

- [ ] **Step 4: Run targeted callback tests**

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/provider -run 'TestCompleteCallback.*Fingerprint|TestCompleteCallbackReauthorizesTargetAccount'
```

- [ ] **Step 5: Commit callback coverage**

```bash
git add backend/internal/provider/service.go backend/internal/provider/service_test.go
git commit -m "test: cover oauth fingerprint callback rules"
```

## Task 6: Align HTTP Sentinel And Admin UI Labels

**Files:**
- Modify: `backend/internal/httpapi/server_test.go`
- Modify: `frontend/src/routes/providers/+page.svelte`
- Modify: `frontend/src/routes/providers/provider-page.test.mjs`

- [ ] **Step 1: Add HTTP sentinel coverage if missing**

In `backend/internal/httpapi/server_test.go`, add or extend a provider connect test to submit:

```json
{"fingerprintProfileId":0}
```

Assert the captured `ConnectOptions.FingerprintProfileID` is `nil`. The service layer resolves `nil` to the default profile.

- [ ] **Step 2: Update OAuth add label only**

In `frontend/src/routes/providers/+page.svelte`, change the OAuth add form's fingerprint selector default option from `None` to:

```svelte
<option value="0">Default Codex CLI</option>
```

Keep the provider account edit selector clear option as:

```svelte
<option value="0">None</option>
```

- [ ] **Step 3: Update frontend source test**

In `frontend/src/routes/providers/provider-page.test.mjs`, assert both labels exist in their intended surfaces:

```js
assert.match(source, /<option value="0">Default Codex CLI<\/option>/)
assert.match(source, /<option value="0">None<\/option>/)
```

If the test already scans by selector block, keep it stricter by checking the add form block and edit block separately.

- [ ] **Step 4: Run targeted tests**

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi -run 'TestAdminCanStartProviderConnect|TestAdminCanStartUnifiedProviderConnect|TestAdminProviderConnectTreatsZeroFingerprintProfileAsDefaultSentinel'
```

```bash
cd frontend
bun test src/routes/providers/provider-page.test.mjs
```

- [ ] **Step 5: Commit UI/API sentinel alignment**

```bash
git add backend/internal/httpapi/server_test.go frontend/src/routes/providers/+page.svelte frontend/src/routes/providers/provider-page.test.mjs
git commit -m "feat: label default codex oauth fingerprint"
```

## Task 7: Full Verification, Local Review, And Docker Refresh

**Files:**
- No planned source edits except fixes found by verification.

- [ ] **Step 1: Run backend focused suite**

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/provider ./internal/store ./internal/httpapi -run 'Fingerprint|StartConnect|CompleteCallback|ProviderConnect'
```

- [ ] **Step 2: Run full backend suite**

```bash
cd backend
GOMODCACHE=/root/Clouds/N2API/.cache/go-mod GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
```

If this fails only because the sandbox cannot bind local network listeners, rerun the same command with sandbox escalation and record that reason.

- [ ] **Step 3: Run frontend gates**

```bash
cd frontend
bun test src/routes/providers/provider-page.test.mjs
bun run check
bun run build
```

- [ ] **Step 4: Review the final diff in the main session**

```bash
git status --short
git diff --stat
git diff -- backend/internal/provider/service.go backend/internal/store/provider.go frontend/src/routes/providers/+page.svelte
```

Confirm:

- custom fingerprint id wins over default
- omitted or `0` OAuth add selection resolves to default
- existing-identity reconnect preserves current profile
- target reauthorization can update profile
- API-upstream account fingerprint behavior is unchanged
- the default profile is a Codex CLI profile, not a browser profile

- [ ] **Step 5: Refresh Docker after code/functionality changes**

Before claiming implementation is complete, read and follow the `n2api-refresh-docker` skill. Record the exact rebuild/recreate and smoke-check commands that pass.

- [ ] **Step 6: Final commit state**

After the last verification fix, ensure every coherent change is committed with Conventional Commits and generated build output is not staged:

```bash
git status --short
```
