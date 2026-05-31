package admin

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

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

func TestBootstrapRenamesExistingAdminAndPreservesPasswordHash(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: 7 * 24 * time.Hour})

	if err := service.BootstrapAdmin(context.Background(), "admin", "first-password"); err != nil {
		t.Fatalf("BootstrapAdmin returned error: %v", err)
	}
	firstID := repo.admin.ID
	firstHash := repo.admin.PasswordHash

	if err := service.BootstrapAdmin(context.Background(), "owner", "second-password"); err != nil {
		t.Fatalf("BootstrapAdmin returned error: %v", err)
	}

	if repo.admin.ID != firstID {
		t.Fatalf("admin ID = %d, want existing ID %d", repo.admin.ID, firstID)
	}
	if repo.admin.Username != "owner" {
		t.Fatalf("admin username = %q, want owner", repo.admin.Username)
	}
	if repo.admin.PasswordHash != firstHash {
		t.Fatal("BootstrapAdmin changed existing password hash")
	}
	if _, err := service.Login(context.Background(), "admin", "first-password"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("old username login error = %v, want ErrUnauthorized", err)
	}
	if _, err := service.Login(context.Background(), "owner", "first-password"); err != nil {
		t.Fatalf("new username login returned error: %v", err)
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

func TestAdminJSONOmitsPasswordHash(t *testing.T) {
	payload, err := json.Marshal(Admin{ID: 1, Username: "admin", PasswordHash: "secret-hash"})
	if err != nil {
		t.Fatalf("json.Marshal returned error: %v", err)
	}
	if strings.Contains(string(payload), "secret-hash") {
		t.Fatalf("json payload contains password hash: %s", payload)
	}
}

func TestLoginRejectsInvalidCredentials(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})
	requireBootstrap(t, service, "admin", "secret")

	if _, err := service.Login(context.Background(), "admin", "wrong"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Login wrong password error = %v, want ErrUnauthorized", err)
	}
	if _, err := service.Login(context.Background(), "missing", "secret"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Login missing username error = %v, want ErrUnauthorized", err)
	}
}

func TestLogoutRevokesSession(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})
	requireBootstrap(t, service, "admin", "secret")
	session, err := service.Login(context.Background(), "admin", "secret")
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}

	if err := service.Logout(context.Background(), session.Token); err != nil {
		t.Fatalf("Logout returned error: %v", err)
	}
	if _, err := service.ValidateSession(context.Background(), session.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("ValidateSession after logout error = %v, want ErrUnauthorized", err)
	}
	if err := service.Logout(context.Background(), ""); err != nil {
		t.Fatalf("Logout empty token returned error: %v", err)
	}
	if err := service.Logout(context.Background(), "unknown-token"); err != nil {
		t.Fatalf("Logout unknown token returned error: %v", err)
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

func TestAuthenticateAPIKeyMapsTouchNotFoundToUnauthorized(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})
	result, err := service.CreateAPIKey(context.Background(), "codex laptop")
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}
	repo.touchErr = ErrNotFound

	if _, err := service.AuthenticateAPIKey(context.Background(), result.Secret); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("AuthenticateAPIKey error = %v, want ErrUnauthorized", err)
	}
}

func TestCreateAPIKeyRejectsInvalidName(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})

	if _, err := service.CreateAPIKey(context.Background(), " \t "); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("CreateAPIKey error = %v, want ErrInvalidInput", err)
	}
}

func TestListAPIKeysReturnsRepositoryKeys(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})
	first, err := service.CreateAPIKey(context.Background(), "first")
	if err != nil {
		t.Fatalf("CreateAPIKey first returned error: %v", err)
	}
	second, err := service.CreateAPIKey(context.Background(), "second")
	if err != nil {
		t.Fatalf("CreateAPIKey second returned error: %v", err)
	}

	keys, err := service.ListAPIKeys(context.Background())
	if err != nil {
		t.Fatalf("ListAPIKeys returned error: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("ListAPIKeys returned %d keys, want 2", len(keys))
	}
	if keys[0].ID != first.Key.ID || keys[1].ID != second.Key.ID {
		t.Fatalf("ListAPIKeys IDs = [%d %d], want [%d %d]", keys[0].ID, keys[1].ID, first.Key.ID, second.Key.ID)
	}
}

func TestListRequestLogsClampsLimitAndReturnsRepositoryLogs(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, Config{SessionTTL: time.Hour})
	repo.logs = []RequestLog{
		{ID: 2, RequestID: "req_2", Route: "/v1/chat/completions", StatusCode: 200},
		{ID: 1, RequestID: "req_1", Route: "/v1/models", StatusCode: 503},
	}

	logs, err := service.ListRequestLogs(context.Background(), 0)
	if err != nil {
		t.Fatalf("ListRequestLogs returned error: %v", err)
	}
	if repo.lastLogLimit != 50 {
		t.Fatalf("repository limit = %d, want default 50", repo.lastLogLimit)
	}
	if len(logs) != 2 || logs[0].RequestID != "req_2" {
		t.Fatalf("logs = %+v", logs)
	}

	if _, err := service.ListRequestLogs(context.Background(), 500); err != nil {
		t.Fatalf("ListRequestLogs returned error: %v", err)
	}
	if repo.lastLogLimit != 200 {
		t.Fatalf("repository limit = %d, want max 200", repo.lastLogLimit)
	}
}

func requireBootstrap(t *testing.T, service *Service, username, password string) {
	t.Helper()

	if err := service.BootstrapAdmin(context.Background(), username, password); err != nil {
		t.Fatalf("BootstrapAdmin returned error: %v", err)
	}
}

type memoryRepo struct {
	admin        Admin
	nextAdminID  int64
	sessions     map[string]memorySession
	keys         map[int64]memoryAPIKey
	nextAPIKeyID int64
	touchErr     error
	logs         []RequestLog
	lastLogLimit int
}

type memorySession struct {
	adminID   int64
	expiresAt time.Time
	revokedAt *time.Time
}

type memoryAPIKey struct {
	APIKey
	Hash string
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{
		nextAdminID:  1,
		sessions:     map[string]memorySession{},
		keys:         map[int64]memoryAPIKey{},
		nextAPIKeyID: 1,
	}
}

func (r *memoryRepo) FindAdminByUsername(_ context.Context, username string) (Admin, error) {
	if r.admin.ID == 0 || r.admin.Username != username {
		return Admin{}, ErrNotFound
	}
	return r.admin, nil
}

func (r *memoryRepo) FindBootstrapAdmin(_ context.Context) (Admin, error) {
	if r.admin.ID == 0 {
		return Admin{}, ErrNotFound
	}
	return r.admin, nil
}

func (r *memoryRepo) CreateAdmin(_ context.Context, username, passwordHash string) (Admin, error) {
	r.admin = Admin{ID: r.nextAdminID, Username: username, PasswordHash: passwordHash}
	r.nextAdminID++
	return r.admin, nil
}

func (r *memoryRepo) UpdateAdminUsername(_ context.Context, id int64, username string) (Admin, error) {
	if r.admin.ID != id {
		return Admin{}, ErrNotFound
	}
	r.admin.Username = username
	return r.admin, nil
}

func (r *memoryRepo) CreateSession(_ context.Context, adminID int64, tokenHash string, expiresAt time.Time) error {
	r.sessions[tokenHash] = memorySession{adminID: adminID, expiresAt: expiresAt}
	return nil
}

func (r *memoryRepo) FindAdminBySessionHash(_ context.Context, tokenHash string, now time.Time) (Admin, error) {
	session, ok := r.sessions[tokenHash]
	if !ok || session.revokedAt != nil || !session.expiresAt.After(now) || r.admin.ID != session.adminID {
		return Admin{}, ErrNotFound
	}
	return r.admin, nil
}

func (r *memoryRepo) RevokeSession(_ context.Context, tokenHash string) error {
	session, ok := r.sessions[tokenHash]
	if !ok {
		return ErrNotFound
	}
	now := time.Now()
	session.revokedAt = &now
	r.sessions[tokenHash] = session
	return nil
}

func (r *memoryRepo) CreateAPIKey(_ context.Context, name, hash, prefix string) (APIKey, error) {
	key := APIKey{
		ID:        r.nextAPIKeyID,
		Name:      name,
		Prefix:    prefix,
		CreatedAt: time.Now(),
	}
	r.nextAPIKeyID++
	r.keys[key.ID] = memoryAPIKey{APIKey: key, Hash: hash}
	return key, nil
}

func (r *memoryRepo) ListAPIKeys(_ context.Context) ([]APIKey, error) {
	keys := make([]APIKey, 0, len(r.keys))
	for _, key := range r.keys {
		keys = append(keys, key.APIKey)
	}
	slices.SortFunc(keys, func(a, b APIKey) int {
		return int(a.ID - b.ID)
	})
	return keys, nil
}

func (r *memoryRepo) RevokeAPIKey(_ context.Context, id int64) (APIKey, error) {
	key, ok := r.keys[id]
	if !ok {
		return APIKey{}, ErrNotFound
	}
	now := time.Now()
	key.RevokedAt = &now
	r.keys[id] = key
	return key.APIKey, nil
}

func (r *memoryRepo) FindAPIKeyByHash(_ context.Context, hash string, _ time.Time) (APIKey, error) {
	for _, key := range r.keys {
		if key.Hash == hash && key.RevokedAt == nil {
			return key.APIKey, nil
		}
	}
	return APIKey{}, ErrNotFound
}

func (r *memoryRepo) TouchAPIKey(_ context.Context, id int64, usedAt time.Time) error {
	if r.touchErr != nil {
		return r.touchErr
	}
	key, ok := r.keys[id]
	if !ok {
		return ErrNotFound
	}
	key.LastUsedAt = &usedAt
	r.keys[id] = key
	return nil
}

func (r *memoryRepo) ListRequestLogs(_ context.Context, limit int) ([]RequestLog, error) {
	r.lastLogLimit = limit
	if limit > len(r.logs) {
		limit = len(r.logs)
	}
	return append([]RequestLog(nil), r.logs[:limit]...), nil
}
