package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/KnowSky404/N2API/backend/internal/config"
)

var errHealth = errors.New("database unavailable")

type staticHealth struct {
	err error
}

func (h staticHealth) Ping(ctx context.Context) error {
	return h.err
}

type fakeAdminService struct {
	keys               []admin.APIKey
	errorOnEmptyLogout bool
	logoutTokens       []string
}

func newFakeAdminService() *fakeAdminService {
	return &fakeAdminService{
		keys: []admin.APIKey{
			{ID: 7, Name: "codex laptop", Prefix: "n2api_abc", CreatedAt: time.Unix(1000, 0).UTC()},
		},
	}
}

func (s *fakeAdminService) Login(_ context.Context, username, password string) (admin.Session, error) {
	if username != "admin" || password != "secret" {
		return admin.Session{}, admin.ErrUnauthorized
	}
	return admin.Session{Token: "valid-session", AdminID: 1, ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (s *fakeAdminService) Logout(_ context.Context, token string) error {
	s.logoutTokens = append(s.logoutTokens, token)
	if s.errorOnEmptyLogout && token == "" {
		return errors.New("empty logout token")
	}
	return nil
}

func (s *fakeAdminService) ValidateSession(_ context.Context, token string) (admin.Admin, error) {
	if token != "valid-session" {
		return admin.Admin{}, admin.ErrUnauthorized
	}
	return admin.Admin{ID: 1, Username: "admin", PasswordHash: "secret-hash"}, nil
}

func (s *fakeAdminService) ListAPIKeys(_ context.Context) ([]admin.APIKey, error) {
	return s.keys, nil
}

func (s *fakeAdminService) CreateAPIKey(_ context.Context, name string) (admin.CreatedAPIKey, error) {
	if strings.TrimSpace(name) == "" {
		return admin.CreatedAPIKey{}, admin.ErrInvalidInput
	}
	key := admin.APIKey{ID: 9, Name: name, Prefix: "n2api_new", CreatedAt: time.Unix(2000, 0).UTC()}
	return admin.CreatedAPIKey{Key: key, Secret: "n2api_new_secret"}, nil
}

func (s *fakeAdminService) RevokeAPIKey(_ context.Context, id int64) (admin.APIKey, error) {
	for _, key := range s.keys {
		if key.ID == id {
			now := time.Unix(3000, 0).UTC()
			key.RevokedAt = &now
			return key, nil
		}
	}
	return admin.APIKey{}, admin.ErrNotFound
}

func TestHealthzReturnsOK(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{err: nil}, nil)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type = %q, want application/json", got)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("status body = %q, want ok", body["status"])
	}
}

func TestAdminHealthIncludesDatabaseStatus(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{err: nil}, nil)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/health", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body struct {
		Status   string `json:"status"`
		Database string `json:"database"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "ok" {
		t.Fatalf("Status = %q, want ok", body.Status)
	}
	if body.Database != "ok" {
		t.Fatalf("Database = %q, want ok", body.Database)
	}
}

func TestAdminHealthReportsDatabaseError(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{err: errHealth}, nil)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/health", nil))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
	var body struct {
		Status   string `json:"status"`
		Database string `json:"database"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "degraded" {
		t.Fatalf("Status = %q, want degraded", body.Status)
	}
	if body.Database != "error" {
		t.Fatalf("Database = %q, want error", body.Database)
	}
}

func TestBootstrapReturnsPublicConfiguration(t *testing.T) {
	cfg := config.Config{
		PublicURL:     "https://n2api.example.com",
		AdminUsername: "owner",
	}
	server := NewServer(cfg, staticHealth{err: nil}, nil)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/bootstrap", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body struct {
		PublicURL     string `json:"publicUrl"`
		AdminUsername string `json:"adminUsername"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.PublicURL != "https://n2api.example.com" {
		t.Fatalf("PublicURL = %q, want configured public URL", body.PublicURL)
	}
	if body.AdminUsername != "owner" {
		t.Fatalf("AdminUsername = %q, want owner", body.AdminUsername)
	}
}

func TestAdminLoginSetsSessionCookie(t *testing.T) {
	admins := newFakeAdminService()
	server := NewServer(config.Config{PublicURL: "http://localhost:3000"}, staticHealth{}, admins)
	recorder := httptest.NewRecorder()
	body := strings.NewReader(`{"username":"admin","password":"secret"}`)

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/admin/login", body))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}
	if cookie := cookies[0]; cookie.Name != "n2api_admin_session" || !cookie.HttpOnly {
		t.Fatalf("session cookie = %+v", cookie)
	}
}

func TestAdminLoginSetsSecureCookieForHTTPSPublicURL(t *testing.T) {
	server := NewServer(config.Config{PublicURL: "https://n2api.example.com"}, staticHealth{}, newFakeAdminService())
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(`{"username":"admin","password":"secret"}`)))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if cookie := recorder.Result().Cookies()[0]; !cookie.Secure {
		t.Fatalf("Secure = false, want true")
	}
}

func TestInvalidAdminLoginReturnsUnauthorized(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService())
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(`{"username":"admin","password":"wrong"}`)))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "invalid_credentials" {
		t.Fatalf("error = %q, want invalid_credentials", body["error"])
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

func TestAdminMeReturnsUsernameWithoutPasswordHash(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService())
	req := httptest.NewRequest(http.MethodGet, "/api/admin/me", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["username"] != "admin" {
		t.Fatalf("username = %v, want admin", body["username"])
	}
	if _, ok := body["passwordHash"]; ok {
		t.Fatalf("body includes passwordHash: %v", body)
	}
}

func TestAdminLogoutClearsSessionCookie(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService())
	req := httptest.NewRequest(http.MethodPost, "/api/admin/logout", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", recorder.Code)
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}
	if cookie := cookies[0]; cookie.Name != "n2api_admin_session" || cookie.Value != "" || cookie.MaxAge >= 0 {
		t.Fatalf("cleared cookie = %+v", cookie)
	}
}

func TestAdminLogoutWithoutSessionClearsCookieWithoutRevoking(t *testing.T) {
	admins := newFakeAdminService()
	admins.errorOnEmptyLogout = true
	server := NewServer(config.Config{}, staticHealth{}, admins)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/admin/logout", nil))

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", recorder.Code)
	}
	if len(admins.logoutTokens) != 0 {
		t.Fatalf("logout tokens = %+v, want no logout call", admins.logoutTokens)
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}
	if cookie := cookies[0]; cookie.Name != "n2api_admin_session" || cookie.Value != "" || cookie.MaxAge >= 0 {
		t.Fatalf("cleared cookie = %+v", cookie)
	}
}

func TestListAPIKeysRequiresSessionAndReturnsKeys(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService())
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/keys", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/keys", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder = httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body struct {
		Keys []admin.APIKey `json:"keys"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Keys) != 1 || body.Keys[0].ID != 7 {
		t.Fatalf("keys = %+v, want key 7", body.Keys)
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

func TestRevokeAPIKeyParsesIDAndReturnsRevokedKey(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService())
	req := httptest.NewRequest(http.MethodPost, "/api/admin/keys/7/revoke", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body struct {
		Key admin.APIKey `json:"key"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Key.ID != 7 || body.Key.RevokedAt == nil {
		t.Fatalf("revoked key = %+v, want revoked key 7", body)
	}
}

func TestBadAdminJSONReturnsBadRequest(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService())
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(`{`)))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestAdminJSONWithTrailingGarbageReturnsBadRequest(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService())
	recorder := httptest.NewRecorder()
	body := strings.NewReader(`{"username":"admin","password":"secret"} garbage`)

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/admin/login", body))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestUnknownAdminPathReturnsJSONNotFound(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService())
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/missing", nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type = %q, want application/json", got)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "not_found" {
		t.Fatalf("error = %q, want not_found", body["error"])
	}
}

func TestWrongMethodAdminPathDoesNotReturnRootFallback(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService())
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/login", nil))

	if recorder.Code == http.StatusOK {
		t.Fatalf("status = 200, want non-200")
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type = %q, want application/json", got)
	}
}
