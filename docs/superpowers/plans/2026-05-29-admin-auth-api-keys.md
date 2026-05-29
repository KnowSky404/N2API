# Admin Auth and API Keys Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add single-admin login, PostgreSQL-backed admin sessions, protected admin APIs, and client API key lifecycle management for N2API V1.

**Architecture:** Keep HTTP routing in `internal/httpapi`, business rules in a new `internal/admin` package, reusable secret primitives in `internal/secret`, and PostgreSQL persistence in `internal/store`. The frontend remains one static SvelteKit admin page using Svelte 5 `$state`/`$derived` patterns confirmed from current Svelte docs.

**Tech Stack:** Go 1.26 standard library crypto, pgx v5, Goose v3, PostgreSQL, Bun, SvelteKit 2, Svelte 5, Tailwind CSS.

---

## File Structure

- Modify: `backend/internal/secret/crypto.go` and `backend/internal/secret/crypto_test.go`
  - Adds password hashing, random token generation, and display prefix helpers.
- Create: `backend/internal/store/migrations/00002_admin_sessions.sql`
  - Adds PostgreSQL session persistence.
- Modify: `backend/internal/store/migrations_test.go`
  - Verifies the embedded migration contains the expected session table and indexes.
- Create: `backend/internal/admin/service.go` and `backend/internal/admin/service_test.go`
  - Owns admin bootstrap, login/session behavior, and API key lifecycle rules behind repository interfaces.
- Create: `backend/internal/store/admin.go`
  - Implements `internal/admin` repository interfaces with pgx.
- Modify: `backend/internal/httpapi/server.go` and `backend/internal/httpapi/server_test.go`
  - Adds login/logout/me/key endpoints and session middleware.
- Modify: `backend/cmd/n2api/main.go`
  - Bootstraps the configured admin and wires the admin service into HTTP routes.
- Modify: `frontend/src/routes/+page.svelte`
  - Adds login, session state, API key table, one-time key reveal, create, revoke, and logout UI.

---

### Task 1: Secret Primitives

**Files:**
- Modify: `backend/internal/secret/crypto.go`
- Modify: `backend/internal/secret/crypto_test.go`

- [x] **Step 1: Write failing secret tests**

Add tests for password hash verification, random token format, key prefix extraction, and token hash reuse:

```go
func TestPasswordHashVerifiesOriginalPassword(t *testing.T) {
	hash, err := HashPassword("owner-password")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if hash == "" || hash == "owner-password" {
		t.Fatalf("HashPassword returned unsafe hash %q", hash)
	}
	if !VerifyPassword(hash, "owner-password") {
		t.Fatal("VerifyPassword returned false for original password")
	}
	if VerifyPassword(hash, "wrong-password") {
		t.Fatal("VerifyPassword returned true for wrong password")
	}
}

func TestGenerateTokenUsesPrefixAndRandomSecret(t *testing.T) {
	first, err := GenerateToken("n2api")
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}
	second, err := GenerateToken("n2api")
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}
	if !strings.HasPrefix(first, "n2api_") {
		t.Fatalf("token = %q, want n2api_ prefix", first)
	}
	if first == second {
		t.Fatal("GenerateToken returned duplicate tokens")
	}
}

func TestTokenPrefixReturnsDisplayPrefix(t *testing.T) {
	prefix := TokenPrefix("n2api_abcdefghijklmnopqrstuvwxyz")
	if prefix != "n2api_abcdefgh" {
		t.Fatalf("TokenPrefix = %q, want n2api_abcdefgh", prefix)
	}
}
```

- [x] **Step 2: Run tests and verify failure**

Run from `backend`:

```bash
GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/secret
```

Expected: compile failure for missing `HashPassword`, `VerifyPassword`, `GenerateToken`, and `TokenPrefix`.

- [x] **Step 3: Implement minimal secret helpers**

Add helpers using standard library only. Use `crypto/pbkdf2` with SHA-256 so no new dependency is needed:

```go
const (
	passwordHashVersion = "pbkdf2-sha256"
	passwordIterations  = 210000
	passwordSaltBytes   = 16
	passwordKeyBytes    = 32
	randomTokenBytes    = 32
)

func HashPassword(password string) (string, error) {
	salt := make([]byte, passwordSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, passwordKeyBytes)
	if err != nil {
		return "", fmt.Errorf("derive password hash: %w", err)
	}
	return fmt.Sprintf("%s$%d$%s$%s", passwordHashVersion, passwordIterations, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func VerifyPassword(hash, password string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 4 || parts[0] != passwordHashVersion {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < 1 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(want) == 0 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

func GenerateToken(prefix string) (string, error) {
	raw := make([]byte, randomTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func TokenPrefix(token string) string {
	if len(token) <= 13 {
		return token
	}
	return token[:13]
}
```

- [x] **Step 4: Verify and commit**

Run:

```bash
GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
git add backend/internal/secret/crypto.go backend/internal/secret/crypto_test.go
git commit -m "feat: add admin secret primitives"
```

Expected: all backend tests pass.

### Task 2: Admin Session Migration

**Files:**
- Create: `backend/internal/store/migrations/00002_admin_sessions.sql`
- Modify: `backend/internal/store/migrations_test.go`

- [x] **Step 1: Write failing migration discovery test**

Add a test that reads `00002_admin_sessions.sql` and checks the table and indexes:

```go
func TestAdminSessionsMigrationIsEmbedded(t *testing.T) {
	sql, err := MigrationSQL("00002_admin_sessions.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS admin_sessions",
		"admin_id BIGINT NOT NULL REFERENCES admins(id) ON DELETE CASCADE",
		"token_hash TEXT NOT NULL UNIQUE",
		"admin_sessions_token_hash_idx",
		"admin_sessions_expires_at_idx",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}
```

- [x] **Step 2: Run test and verify failure**

Run from `backend`:

```bash
GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store
```

Expected: failure because `00002_admin_sessions.sql` does not exist.

- [x] **Step 3: Add migration**

Create:

```sql
-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS admin_sessions (
    id BIGSERIAL PRIMARY KEY,
    admin_id BIGINT NOT NULL REFERENCES admins(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS admin_sessions_token_hash_idx ON admin_sessions (token_hash);
CREATE INDEX IF NOT EXISTS admin_sessions_expires_at_idx ON admin_sessions (expires_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS admin_sessions;
-- +goose StatementEnd
```

- [x] **Step 4: Verify and commit**

Run:

```bash
GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
git add backend/internal/store/migrations/00002_admin_sessions.sql backend/internal/store/migrations_test.go
git commit -m "feat: add admin session migration"
```

Expected: all backend tests pass.

### Task 3: Admin Service

**Files:**
- Create: `backend/internal/admin/service.go`
- Create: `backend/internal/admin/service_test.go`

- [x] **Step 1: Write failing service tests**

Cover bootstrap preservation, login, session validation, logout, API key creation/list/revoke/authentication with an in-memory fake repository:

```go
func TestBootstrapCreatesAdminOnceAndPreservesExistingHash(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: 7 * 24 * time.Hour})

	if err := service.BootstrapAdmin(context.Background(), "admin", "first-password"); err != nil {
		t.Fatalf("BootstrapAdmin returned error: %v", err)
	}
	firstHash := repo.admin.PasswordHash
	if err := service.BootstrapAdmin(context.Background(), "admin", "second-password"); err != nil {
		t.Fatalf("BootstrapAdmin returned error: %v", err)
	}
	if repo.admin.PasswordHash != firstHash {
		t.Fatal("BootstrapAdmin changed existing password hash")
	}
}

func TestLoginCreatesSessionAndValidateSessionReturnsAdmin(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})
	requireBootstrap(t, service, "admin", "secret")

	session, err := service.Login(context.Background(), "admin", "secret")
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	if session.Token == "" || session.ExpiresAt.IsZero() {
		t.Fatalf("invalid session: %+v", session)
	}
	admin, err := service.ValidateSession(context.Background(), session.Token)
	if err != nil {
		t.Fatalf("ValidateSession returned error: %v", err)
	}
	if admin.Username != "admin" {
		t.Fatalf("Username = %q, want admin", admin.Username)
	}
}

func TestCreateAPIKeyReturnsSecretOnceAndAuthenticateRejectsRevoked(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})
	result, err := service.CreateAPIKey(context.Background(), "codex laptop")
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}
	if result.Secret == "" || result.Key.Prefix == "" {
		t.Fatalf("missing secret or prefix: %+v", result)
	}
	if strings.Contains(repo.keys[result.Key.ID].Hash, result.Secret) {
		t.Fatal("repository stored cleartext key")
	}
	if _, err := service.AuthenticateAPIKey(context.Background(), result.Secret); err != nil {
		t.Fatalf("AuthenticateAPIKey returned error: %v", err)
	}
	if _, err := service.RevokeAPIKey(context.Background(), result.Key.ID); err != nil {
		t.Fatalf("RevokeAPIKey returned error: %v", err)
	}
	if _, err := service.AuthenticateAPIKey(context.Background(), result.Secret); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("AuthenticateAPIKey error = %v, want ErrUnauthorized", err)
	}
}
```

- [x] **Step 2: Run tests and verify failure**

Run:

```bash
GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/admin
```

Expected: compile failure because the package does not exist.

- [x] **Step 3: Implement service types and rules**

Create `service.go` with these public shapes:

```go
package admin

type Config struct {
	SessionTTL time.Duration
}

type Admin struct {
	ID           int64
	Username     string
	PasswordHash string
}

type Session struct {
	Token     string
	AdminID   int64
	ExpiresAt time.Time
}

type APIKey struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
	RevokedAt  *time.Time `json:"revokedAt"`
}

type CreatedAPIKey struct {
	Key    APIKey
	Secret string
}

type Repository interface {
	FindAdminByUsername(ctx context.Context, username string) (Admin, error)
	CreateAdmin(ctx context.Context, username, passwordHash string) (Admin, error)
	CreateSession(ctx context.Context, adminID int64, tokenHash string, expiresAt time.Time) error
	FindAdminBySessionHash(ctx context.Context, tokenHash string, now time.Time) (Admin, error)
	RevokeSession(ctx context.Context, tokenHash string) error
	CreateAPIKey(ctx context.Context, name, hash, prefix string) (APIKey, error)
	ListAPIKeys(ctx context.Context) ([]APIKey, error)
	RevokeAPIKey(ctx context.Context, id int64) (APIKey, error)
	FindAPIKeyByHash(ctx context.Context, hash string, now time.Time) (APIKey, error)
	TouchAPIKey(ctx context.Context, id int64, usedAt time.Time) error
}
```

Define errors:

```go
var (
	ErrNotFound      = errors.New("not found")
	ErrUnauthorized = errors.New("unauthorized")
	ErrInvalidInput = errors.New("invalid input")
)
```

Implementation rules:
- `BootstrapAdmin` creates only when `FindAdminByUsername` returns `ErrNotFound`.
- `Login` verifies password with `secret.VerifyPassword`, generates `admin_session` token, stores `secret.HashAPIKey(token)`.
- `ValidateSession` hashes the cookie token and asks the repository for the active admin.
- `Logout` hashes the cookie token and revokes it.
- `CreateAPIKey` trims and validates name, generates `n2api` token, stores hash and `secret.TokenPrefix(token)`.
- `AuthenticateAPIKey` rejects empty, unknown, or revoked keys and touches `last_used_at` on success.

- [x] **Step 4: Verify and commit**

Run:

```bash
GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
git add backend/internal/admin/service.go backend/internal/admin/service_test.go
git commit -m "feat: add admin auth service"
```

Expected: all backend tests pass.

### Task 4: PostgreSQL Admin Repository and Startup Bootstrap

**Files:**
- Create: `backend/internal/store/admin.go`
- Modify: `backend/cmd/n2api/main.go`

- [x] **Step 1: Write compile-focused repository test**

Add a small test in `backend/internal/store/admin_test.go` to ensure the repository satisfies the admin interface:

```go
func TestAdminRepositoryImplementsInterface(t *testing.T) {
	var _ admin.Repository = (*AdminRepository)(nil)
}
```

- [x] **Step 2: Run test and verify failure**

Run:

```bash
GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/store
```

Expected: compile failure because `AdminRepository` does not exist.

- [x] **Step 3: Implement PostgreSQL repository**

Create constructor and methods:

```go
type AdminRepository struct {
	pool *pgxpool.Pool
}

func NewAdminRepository(pool *pgxpool.Pool) *AdminRepository {
	return &AdminRepository{pool: pool}
}
```

SQL behavior:
- `FindAdminByUsername`: select `id`, `username`, `password_hash`; return `admin.ErrNotFound` for `pgx.ErrNoRows`.
- `CreateAdmin`: insert `username`, `password_hash`; return inserted admin.
- `CreateSession`: insert `admin_id`, `token_hash`, `expires_at`.
- `FindAdminBySessionHash`: join `admin_sessions` to `admins` where token hash matches, `expires_at > now`, and `revoked_at IS NULL`.
- `RevokeSession`: update matching session `revoked_at = now()`; no error when no rows change.
- `CreateAPIKey`: insert `name`, `key_hash`, `prefix`; return public key fields.
- `ListAPIKeys`: select public key fields ordered by `created_at DESC`.
- `RevokeAPIKey`: update `revoked_at = COALESCE(revoked_at, now())` and return public key fields.
- `FindAPIKeyByHash`: select only where `revoked_at IS NULL`.
- `TouchAPIKey`: update `last_used_at`.

- [x] **Step 4: Wire startup bootstrap**

In `backend/cmd/n2api/main.go`, after migrations:

```go
adminRepo := store.NewAdminRepository(pool)
adminService := admin.NewService(adminRepo, admin.Config{SessionTTL: 7 * 24 * time.Hour})
if err := adminService.BootstrapAdmin(ctx, cfg.AdminUsername, cfg.AdminPassword); err != nil {
	slog.Error("admin bootstrap failed", "error", err)
	os.Exit(1)
}
```

Pass `adminService` to `httpapi.NewServer`.

- [x] **Step 5: Verify and commit**

Run:

```bash
GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
git add backend/internal/store/admin.go backend/internal/store/admin_test.go backend/cmd/n2api/main.go
git commit -m "feat: persist admin auth data"
```

Expected: all backend tests pass.

### Task 5: Admin HTTP API

**Files:**
- Modify: `backend/internal/httpapi/server.go`
- Modify: `backend/internal/httpapi/server_test.go`

- [x] **Step 1: Write failing HTTP tests**

Add tests using a fake admin service:

```go
func TestAdminLoginSetsSessionCookie(t *testing.T) {
	admins := newFakeAdminService()
	server := NewServer(config.Config{PublicURL: "http://localhost:3000"}, staticHealth{}, admins)
	recorder := httptest.NewRecorder()
	body := strings.NewReader(`{"username":"admin","password":"secret"}`)

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/admin/login", body))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if cookie := recorder.Result().Cookies()[0]; cookie.Name != "n2api_admin_session" || !cookie.HttpOnly {
		t.Fatalf("session cookie = %+v", cookie)
	}
}

func TestAdminMeRequiresSession(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService())
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/me", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestCreateAPIKeyReturnsOneTimeSecret(t *testing.T) {
	admins := newFakeAdminService()
	server := NewServer(config.Config{}, staticHealth{}, admins)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/keys", strings.NewReader(`{"name":"codex laptop"}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", recorder.Code)
	}
	var body struct {
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Secret == "" {
		t.Fatal("secret is empty")
	}
}
```

- [x] **Step 2: Run tests and verify failure**

Run:

```bash
GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./internal/httpapi
```

Expected: compile failure because `NewServer` does not accept an admin service and routes do not exist.

- [x] **Step 3: Add admin service interface to HTTP package**

Define a narrow interface in `server.go` so tests can fake it:

```go
type AdminService interface {
	Login(ctx context.Context, username, password string) (admin.Session, error)
	Logout(ctx context.Context, token string) error
	ValidateSession(ctx context.Context, token string) (admin.Admin, error)
	ListAPIKeys(ctx context.Context) ([]admin.APIKey, error)
	CreateAPIKey(ctx context.Context, name string) (admin.CreatedAPIKey, error)
	RevokeAPIKey(ctx context.Context, id int64) (admin.APIKey, error)
}
```

Change constructor:

```go
func NewServer(cfg config.Config, health HealthChecker, admins AdminService) http.Handler
```

- [x] **Step 4: Implement routes and helpers**

Add:
- bounded JSON decoding with `http.MaxBytesReader(w, r.Body, 1<<20)`
- `writeError(w, status, code)`
- `requireAdmin(next func(http.ResponseWriter, *http.Request, admin.Admin))`
- cookie helpers for set, read, and clear
- `secureCookie := strings.HasPrefix(cfg.PublicURL, "https://")`

Route behavior:
- `POST /api/admin/login`: validates JSON, calls `Login`, sets cookie, returns username.
- `POST /api/admin/logout`: revokes current token when present, clears cookie, returns 204.
- `GET /api/admin/me`: requires session, returns username.
- `GET /api/admin/keys`: requires session, returns key list.
- `POST /api/admin/keys`: requires session, creates key, returns 201 with public key and one-time secret.
- `POST /api/admin/keys/{id}/revoke`: requires session, parses id, revokes idempotently.

- [x] **Step 5: Verify and commit**

Run:

```bash
GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
git add backend/internal/httpapi/server.go backend/internal/httpapi/server_test.go
git commit -m "feat: add admin auth api"
```

Expected: all backend tests pass.

### Task 6: Frontend Login and API Key Management

**Files:**
- Modify: `frontend/src/routes/+page.svelte`

- [x] **Step 1: Add unauthenticated and authenticated state**

Use Svelte 5 runes consistent with current code and Context7-confirmed syntax:

```svelte
let session = $state({
  loading: true,
  authenticated: false,
  username: '',
  error: ''
});

let loginForm = $state({
  username: '',
  password: '',
  submitting: false,
  error: ''
});

let apiKeys = $state({
  loading: false,
  creating: false,
  error: '',
  items: [],
  newKeyName: '',
  oneTimeSecret: ''
});

const activeKeys = $derived(apiKeys.items.filter((key) => !key.revokedAt));
```

- [x] **Step 2: Add API helpers**

Add helpers near the top of the component:

```svelte
async function requestJSON(path, options = {}) {
  const response = await fetch(path, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...(options.headers ?? {})
    }
  });
  if (!response.ok) {
    const payload = await response.json().catch(() => ({}));
    throw new Error(payload.error ?? `Request failed with ${response.status}`);
  }
  if (response.status === 204) {
    return null;
  }
  return response.json();
}
```

- [x] **Step 3: Add login/session/key actions**

Implement:

```svelte
async function loadSession() {
  try {
    const payload = await requestJSON('/api/admin/me');
    session = { loading: false, authenticated: true, username: payload.username, error: '' };
    await loadKeys();
  } catch (error) {
    session = { loading: false, authenticated: false, username: '', error: '' };
  }
}

async function login(event) {
  event.preventDefault();
  loginForm.submitting = true;
  loginForm.error = '';
  try {
    await requestJSON('/api/admin/login', {
      method: 'POST',
      body: JSON.stringify({ username: loginForm.username, password: loginForm.password })
    });
    loginForm.password = '';
    await loadSession();
  } catch (error) {
    loginForm.error = error instanceof Error ? error.message : 'Login failed';
  } finally {
    loginForm.submitting = false;
  }
}

async function logout() {
  await fetch('/api/admin/logout', { method: 'POST' });
  session = { loading: false, authenticated: false, username: '', error: '' };
  apiKeys.items = [];
  apiKeys.oneTimeSecret = '';
}
```

Implement key actions against `/api/admin/keys`:

```svelte
async function loadKeys() {
  apiKeys.loading = true;
  apiKeys.error = '';
  try {
    const payload = await requestJSON('/api/admin/keys');
    apiKeys.items = payload.keys ?? [];
  } catch (error) {
    apiKeys.error = error instanceof Error ? error.message : 'Failed to load API keys';
  } finally {
    apiKeys.loading = false;
  }
}

async function createKey(event) {
  event.preventDefault();
  apiKeys.creating = true;
  apiKeys.error = '';
  apiKeys.oneTimeSecret = '';
  try {
    const payload = await requestJSON('/api/admin/keys', {
      method: 'POST',
      body: JSON.stringify({ name: apiKeys.newKeyName })
    });
    apiKeys.items = [payload.key, ...apiKeys.items];
    apiKeys.oneTimeSecret = payload.secret;
    apiKeys.newKeyName = '';
  } catch (error) {
    apiKeys.error = error instanceof Error ? error.message : 'Failed to create API key';
  } finally {
    apiKeys.creating = false;
  }
}

async function revokeKey(id) {
  apiKeys.error = '';
  try {
    const payload = await requestJSON(`/api/admin/keys/${id}/revoke`, { method: 'POST' });
    apiKeys.items = apiKeys.items.map((key) => (key.id === id ? payload.key : key));
  } catch (error) {
    apiKeys.error = error instanceof Error ? error.message : 'Failed to revoke API key';
  }
}
```

- [x] **Step 4: Replace markup with login and key management views**

Keep the existing health panels, but render login when `!session.authenticated`:

```svelte
{#if session.loading}
  <section class="rounded-lg border border-slate-200 bg-white p-6">Loading admin session...</section>
{:else if !session.authenticated}
  <form class="rounded-lg border border-slate-200 bg-white p-6" onsubmit={login}>
    <h2 class="text-lg font-semibold text-slate-950">Admin sign in</h2>
    <label class="mt-4 block text-sm font-medium text-slate-700">
      Username
      <input class="mt-2 w-full rounded-md border border-slate-300 px-3 py-2" bind:value={loginForm.username} autocomplete="username" />
    </label>
    <label class="mt-4 block text-sm font-medium text-slate-700">
      Password
      <input class="mt-2 w-full rounded-md border border-slate-300 px-3 py-2" type="password" bind:value={loginForm.password} autocomplete="current-password" />
    </label>
    {#if loginForm.error}
      <p class="mt-3 text-sm text-red-700">{loginForm.error}</p>
    {/if}
    <button class="mt-5 rounded-md bg-slate-950 px-4 py-2 text-sm font-medium text-white" disabled={loginForm.submitting}>
      {loginForm.submitting ? 'Signing in' : 'Sign in'}
    </button>
  </form>
{:else}
  <section class="rounded-lg border border-slate-200 bg-white p-6">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div>
        <h2 class="text-lg font-semibold text-slate-950">API keys</h2>
        <p class="mt-1 text-sm text-slate-500">Signed in as {session.username}</p>
      </div>
      <button class="rounded-md border border-slate-300 px-3 py-2 text-sm font-medium" onclick={logout}>Logout</button>
    </div>

    <form class="mt-6 flex flex-col gap-3 sm:flex-row" onsubmit={createKey}>
      <input class="min-w-0 flex-1 rounded-md border border-slate-300 px-3 py-2" bind:value={apiKeys.newKeyName} placeholder="Key name" />
      <button class="rounded-md bg-slate-950 px-4 py-2 text-sm font-medium text-white" disabled={apiKeys.creating}>
        {apiKeys.creating ? 'Creating' : 'Create key'}
      </button>
    </form>

    {#if apiKeys.oneTimeSecret}
      <div class="mt-4 rounded-md border border-emerald-200 bg-emerald-50 p-4">
        <p class="text-sm font-medium text-emerald-900">Copy this key now. It will not be shown again.</p>
        <code class="mt-2 block overflow-x-auto text-sm text-emerald-950">{apiKeys.oneTimeSecret}</code>
      </div>
    {/if}

    {#if apiKeys.error}
      <p class="mt-4 text-sm text-red-700">{apiKeys.error}</p>
    {/if}

    <div class="mt-6 overflow-x-auto">
      <table class="w-full text-left text-sm">
        <thead class="border-b border-slate-200 text-slate-500">
          <tr>
            <th class="py-2 font-medium">Name</th>
            <th class="py-2 font-medium">Prefix</th>
            <th class="py-2 font-medium">Created</th>
            <th class="py-2 font-medium">Status</th>
            <th class="py-2 text-right font-medium">Action</th>
          </tr>
        </thead>
        <tbody>
          {#each apiKeys.items as key}
            <tr class="border-b border-slate-100">
              <td class="py-3">{key.name}</td>
              <td class="py-3 font-mono text-xs">{key.prefix}</td>
              <td class="py-3">{new Date(key.createdAt).toLocaleString()}</td>
              <td class="py-3">{key.revokedAt ? 'Revoked' : 'Active'}</td>
              <td class="py-3 text-right">
                <button class="rounded-md border border-slate-300 px-3 py-1.5 text-sm" disabled={key.revokedAt} onclick={() => revokeKey(key.id)}>
                  Revoke
                </button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  </section>
{/if}
```

In the authenticated view, include:
- signed-in username and logout button
- create-key form
- one-time secret panel with monospace secret text
- table or stacked rows for keys
- disabled revoke button for revoked keys

- [x] **Step 5: Verify and commit**

Run from `frontend`:

```bash
bun run check
bun run build
git add frontend/src/routes/+page.svelte
git commit -m "feat: add admin key management ui"
```

Expected: Svelte diagnostics report `0 errors and 0 warnings`; build exits 0.

### Task 7: Full Verification

**Files:**
- Review repository state.

- [ ] **Step 1: Run backend tests**

Run from `backend`:

```bash
GOCACHE=/root/Clouds/N2API/.cache/go-build go test ./...
```

Expected: all backend packages pass.

- [ ] **Step 2: Run frontend checks**

Run from `frontend`:

```bash
bun run check
bun run build
```

Expected: Svelte diagnostics report `0 errors and 0 warnings`; build exits 0.

- [ ] **Step 3: Validate Docker Compose config**

Run from repository root:

```bash
docker compose -f deploy/compose.yaml --env-file .env.example config
```

Expected: config renders with `n2api` and `postgres` services and no errors.

- [ ] **Step 4: Confirm repository status**

Run:

```bash
git status --short --branch
```

Expected: clean worktree with only intentional commits ahead of `origin/main`.
