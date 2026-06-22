package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/KnowSky404/N2API/backend/internal/config"
	"github.com/KnowSky404/N2API/backend/internal/provider"
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
	logs               []admin.RequestLog
	errorOnEmptyLogout bool
	logoutTokens       []string
	modelSettings      admin.ModelSettings
	modelPolicyKeyID   int64
	modelPolicy        string
	modelPolicyModels  []string
	modelPolicyErr     error
}

type fakeProviderService struct {
	status                provider.Status
	connect               provider.ConnectResult
	connectOptions        provider.ConnectOptions
	createdAPIUpstream    provider.APIUpstreamInput
	accounts              []provider.Account
	accountModels         map[int64][]provider.AccountModel
	exposedModels         []provider.ExposedModel
	updateErr             error
	accountModelsErr      error
	replaceModelsErr      error
	exposedModelsErr      error
	refreshErr            error
	disconnectErr         error
	callbackErr           error
	callbackCode          string
	callbackState         string
	disconnected          bool
	refreshedAccountID    int64
	disconnectedAccountID int64
}

type fakeGatewayHandler struct {
	called bool
}

func (h *fakeGatewayHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.called = true
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
}

func newFakeAdminService() *fakeAdminService {
	return &fakeAdminService{
		keys: []admin.APIKey{
			{ID: 7, Name: "codex laptop", Prefix: "n2api_abc", CreatedAt: time.Unix(1000, 0).UTC()},
		},
		logs: []admin.RequestLog{
			{ID: 3, RequestID: "req_3", ClientKey: "codex laptop (n2api_abc)", Provider: "openai", Route: "/v1/models", Method: http.MethodGet, StatusCode: 200, LatencyMS: 12, CreatedAt: time.Unix(4000, 0).UTC()},
		},
		modelSettings: admin.ModelSettings{
			DefaultModel:  "gpt-4.1",
			AllowedModels: []string{"gpt-4.1", "gpt-4.1-mini"},
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

func (s *fakeAdminService) UpdateAPIKeyModelPolicy(_ context.Context, id int64, policy string, models []string) (admin.APIKey, error) {
	s.modelPolicyKeyID = id
	s.modelPolicy = policy
	s.modelPolicyModels = append([]string(nil), models...)
	if s.modelPolicyErr != nil {
		return admin.APIKey{}, s.modelPolicyErr
	}
	for i, key := range s.keys {
		if key.ID == id {
			key.ModelPolicy = policy
			if policy == admin.APIKeyModelPolicyAll {
				key.AllowedModels = nil
			} else {
				key.AllowedModels = append([]string(nil), models...)
			}
			s.keys[i] = key
			return key, nil
		}
	}
	return admin.APIKey{}, admin.ErrNotFound
}

func (s *fakeAdminService) ListRequestLogs(_ context.Context, limit int) ([]admin.RequestLog, error) {
	if limit > len(s.logs) {
		limit = len(s.logs)
	}
	return s.logs[:limit], nil
}

func (s *fakeAdminService) GetModelSettings(_ context.Context) (admin.ModelSettings, error) {
	return s.modelSettings, nil
}

func (s *fakeAdminService) UpdateModelSettings(_ context.Context, settings admin.ModelSettings) (admin.ModelSettings, error) {
	defaultModel := strings.TrimSpace(settings.DefaultModel)
	if defaultModel == "" {
		return admin.ModelSettings{}, admin.ErrInvalidInput
	}
	defaultAllowed := false
	for _, model := range settings.AllowedModels {
		if strings.TrimSpace(model) == defaultModel {
			defaultAllowed = true
			break
		}
	}
	if !defaultAllowed {
		return admin.ModelSettings{}, admin.ErrInvalidInput
	}
	s.modelSettings = settings
	return s.modelSettings, nil
}

func (s *fakeAdminService) DefaultModel(ctx context.Context) (string, error) {
	settings, err := s.GetModelSettings(ctx)
	if err != nil {
		return "", err
	}
	return settings.DefaultModel, nil
}

func (s *fakeAdminService) IsModelAllowed(ctx context.Context, model string) (bool, error) {
	settings, err := s.GetModelSettings(ctx)
	if err != nil {
		return false, err
	}
	model = strings.TrimSpace(model)
	for _, allowed := range settings.AllowedModels {
		if model == allowed {
			return true, nil
		}
	}
	return false, nil
}

func newFakeProviderService() *fakeProviderService {
	return &fakeProviderService{
		status: provider.Status{
			Provider:    "openai",
			Configured:  true,
			Connected:   true,
			DisplayName: "Codex Account",
		},
		connect:       provider.ConnectResult{AuthorizationURL: "https://auth.example.test/authorize?state=oauth_state"},
		accountModels: map[int64][]provider.AccountModel{},
	}
}

func (s *fakeProviderService) Status(_ context.Context) (provider.Status, error) {
	return s.status, nil
}

func (s *fakeProviderService) StartConnect(_ context.Context, options provider.ConnectOptions) (provider.ConnectResult, error) {
	if !s.status.Configured {
		return provider.ConnectResult{}, provider.ErrNotConfigured
	}
	s.connectOptions = options
	return s.connect, nil
}

func (s *fakeProviderService) ListAccounts(_ context.Context) ([]provider.Account, error) {
	return s.accounts, nil
}

func (s *fakeProviderService) CreateAPIUpstreamAccount(_ context.Context, input provider.APIUpstreamInput) (provider.Account, error) {
	s.createdAPIUpstream = input
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.BaseURL) == "" || strings.TrimSpace(input.APIKey) == "" {
		return provider.Account{}, provider.ErrInvalidInput
	}
	account := provider.Account{
		ID:          int64(len(s.accounts) + 1),
		Provider:    "openai",
		AccountType: provider.AccountTypeAPIUpstream,
		Name:        strings.TrimSpace(input.Name),
		DisplayName: strings.TrimSpace(input.Name),
		Enabled:     input.Enabled,
		Priority:    input.Priority,
		Status:      provider.AccountStatusActive,
		Credential: provider.AccountCredential{
			CredentialType: provider.CredentialTypeAPIKey,
			BaseURL:        strings.TrimRight(strings.TrimSpace(input.BaseURL), "/"),
		},
	}
	s.accounts = append(s.accounts, account)
	if len(input.Models) > 0 {
		models := make([]provider.AccountModel, 0, len(input.Models))
		for i, model := range input.Models {
			models = append(models, provider.AccountModel{
				ID:        int64(i + 1),
				AccountID: account.ID,
				Provider:  "openai",
				Model:     strings.TrimSpace(model),
				Enabled:   true,
				Source:    provider.AccountModelSourceManual,
			})
		}
		s.accountModels[account.ID] = models
	}
	return account, nil
}

func (s *fakeProviderService) ListAccountModels(_ context.Context, accountID int64) ([]provider.AccountModel, error) {
	if s.accountModelsErr != nil {
		return nil, s.accountModelsErr
	}
	models, ok := s.accountModels[accountID]
	if !ok {
		return nil, provider.ErrNotConnected
	}
	return append([]provider.AccountModel(nil), models...), nil
}

func (s *fakeProviderService) ReplaceAccountModels(_ context.Context, accountID int64, models []provider.AccountModelInput) ([]provider.AccountModel, error) {
	if s.replaceModelsErr != nil {
		return nil, s.replaceModelsErr
	}
	if _, ok := s.accountModels[accountID]; !ok {
		return nil, provider.ErrNotConnected
	}
	saved := make([]provider.AccountModel, 0, len(models))
	for i, model := range models {
		saved = append(saved, provider.AccountModel{
			ID:        int64(i + 1),
			AccountID: accountID,
			Provider:  "openai",
			Model:     strings.TrimSpace(model.Model),
			Enabled:   model.Enabled,
			Source:    provider.AccountModelSourceManual,
		})
	}
	s.accountModels[accountID] = saved
	return append([]provider.AccountModel(nil), saved...), nil
}

func (s *fakeProviderService) ListExposedModels(_ context.Context, allowedModels []string) ([]provider.ExposedModel, error) {
	if s.exposedModelsErr != nil {
		return nil, s.exposedModelsErr
	}
	if s.exposedModels != nil {
		return append([]provider.ExposedModel(nil), s.exposedModels...), nil
	}
	models := make([]provider.ExposedModel, 0, len(allowedModels))
	for _, model := range allowedModels {
		models = append(models, provider.ExposedModel{ID: model, OwnedBy: "openai"})
	}
	return models, nil
}

func (s *fakeProviderService) CompleteCallback(_ context.Context, code, state string) (provider.Account, error) {
	s.callbackCode = code
	s.callbackState = state
	if s.callbackErr != nil {
		return provider.Account{}, s.callbackErr
	}
	return provider.Account{Provider: "openai", DisplayName: "Codex Account"}, nil
}

func (s *fakeProviderService) UpdateAccount(_ context.Context, id int64, update provider.AccountUpdate) (provider.Account, error) {
	if s.updateErr != nil {
		return provider.Account{}, s.updateErr
	}
	if update.Enabled == nil && update.Priority == nil {
		return provider.Account{}, provider.ErrInvalidInput
	}
	if update.Priority != nil && *update.Priority < 0 {
		return provider.Account{}, provider.ErrInvalidInput
	}
	for i, account := range s.accounts {
		if account.ID == id {
			if update.Enabled != nil {
				account.Enabled = *update.Enabled
			}
			if update.Priority != nil {
				account.Priority = *update.Priority
			}
			s.accounts[i] = account
			return account, nil
		}
	}
	return provider.Account{}, provider.ErrNotConnected
}

func (s *fakeProviderService) RefreshAccount(_ context.Context, id int64) (provider.Account, error) {
	if s.refreshErr != nil {
		return provider.Account{}, s.refreshErr
	}
	for i, account := range s.accounts {
		if account.ID == id {
			now := time.Now()
			account.LastRefreshAt = &now
			account.Status = provider.AccountStatusActive
			account.StatusReason = ""
			s.accounts[i] = account
			s.refreshedAccountID = id
			return account, nil
		}
	}
	return provider.Account{}, provider.ErrNotConnected
}

func (s *fakeProviderService) DisconnectAccount(_ context.Context, id int64) error {
	if s.disconnectErr != nil {
		return s.disconnectErr
	}
	for i, account := range s.accounts {
		if account.ID == id {
			s.disconnectedAccountID = id
			s.accounts = append(s.accounts[:i], s.accounts[i+1:]...)
			return nil
		}
	}
	return provider.ErrNotConnected
}

func (s *fakeProviderService) Disconnect(_ context.Context) error {
	s.disconnected = true
	return nil
}

func TestHealthzReturnsOK(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{err: nil}, nil, nil)
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
	server := NewServer(config.Config{}, staticHealth{err: nil}, nil, nil)
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
	server := NewServer(config.Config{}, staticHealth{err: errHealth}, nil, nil)
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
	server := NewServer(cfg, staticHealth{err: nil}, nil, nil)
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
	server := NewServer(config.Config{PublicURL: "http://localhost:3000"}, staticHealth{}, admins, nil)
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
	server := NewServer(config.Config{PublicURL: "https://n2api.example.com"}, staticHealth{}, newFakeAdminService(), nil)
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
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil)
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
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/me", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestAdminMeReturnsUsernameWithoutPasswordHash(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil)
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
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil)
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
	server := NewServer(config.Config{}, staticHealth{}, admins, nil)
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
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil)
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
	server := NewServer(config.Config{}, staticHealth{}, admins, nil)
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
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil)
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

func TestUpdateAPIKeyModelPolicyEndpoint(t *testing.T) {
	admins := newFakeAdminService()
	server := NewServer(config.Config{}, staticHealth{}, admins, nil)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/keys/7/model-policy", strings.NewReader(`{"modelPolicy":"selected","models":["gpt-5","gpt-4.1"]}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Key admin.APIKey `json:"key"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Key.ID != 7 || body.Key.ModelPolicy != admin.APIKeyModelPolicySelected || !slices.Equal(body.Key.AllowedModels, []string{"gpt-5", "gpt-4.1"}) {
		t.Fatalf("key = %+v, want selected model policy", body.Key)
	}
	if admins.modelPolicyKeyID != 7 || admins.modelPolicy != admin.APIKeyModelPolicySelected || !slices.Equal(admins.modelPolicyModels, []string{"gpt-5", "gpt-4.1"}) {
		t.Fatalf("recorded model policy = id:%d policy:%q models:%v", admins.modelPolicyKeyID, admins.modelPolicy, admins.modelPolicyModels)
	}
}

func TestUpdateAPIKeyModelPolicyEndpointMapsErrors(t *testing.T) {
	for _, tc := range []struct {
		name       string
		path       string
		body       string
		serviceErr error
		wantStatus int
	}{
		{
			name:       "invalid id",
			path:       "/api/admin/keys/not-a-number/model-policy",
			body:       `{"modelPolicy":"selected","models":["gpt-5"]}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid policy",
			path:       "/api/admin/keys/7/model-policy",
			body:       `{"modelPolicy":"invalid","models":["gpt-5"]}`,
			serviceErr: admin.ErrInvalidInput,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty selected models",
			path:       "/api/admin/keys/7/model-policy",
			body:       `{"modelPolicy":"selected","models":[]}`,
			serviceErr: admin.ErrInvalidInput,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "not found",
			path:       "/api/admin/keys/99/model-policy",
			body:       `{"modelPolicy":"all"}`,
			serviceErr: admin.ErrNotFound,
			wantStatus: http.StatusNotFound,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			admins := newFakeAdminService()
			admins.modelPolicyErr = tc.serviceErr
			server := NewServer(config.Config{}, staticHealth{}, admins, nil)
			req := httptest.NewRequest(http.MethodPut, tc.path, strings.NewReader(tc.body))
			req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != tc.wantStatus {
				t.Fatalf("status = %d body=%s, want %d", recorder.Code, recorder.Body.String(), tc.wantStatus)
			}
		})
	}
}

func TestProviderStatusRequiresSessionAndReturnsStatus(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/providers/openai", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/providers/openai", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder = httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body provider.Status
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !body.Configured || !body.Connected || body.DisplayName != "Codex Account" {
		t.Fatalf("provider status = %+v", body)
	}
}

func TestProviderConnectReturnsAuthorizationURL(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	req := httptest.NewRequest(http.MethodPost, "/api/admin/providers/openai/connect", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body struct {
		AuthorizationURL string `json:"authorizationUrl"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.AuthorizationURL == "" {
		t.Fatal("authorizationUrl is empty")
	}
}

func TestProviderConnectAcceptsAccountOptionsAndFingerprint(t *testing.T) {
	providers := newFakeProviderService()
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/providers/openai/connect", strings.NewReader(`{"name":"Work Codex","priority":7,"enabled":false,"targetAccountId":42,"fingerprint":"browser-fp"}`))
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("X-Forwarded-For", "203.0.113.10, 198.51.100.2")
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if providers.connectOptions.RedirectAfter != "/" ||
		providers.connectOptions.Name != "Work Codex" ||
		providers.connectOptions.Priority != 7 ||
		providers.connectOptions.Enabled == nil ||
		*providers.connectOptions.Enabled ||
		providers.connectOptions.TargetAccountID != 42 {
		t.Fatalf("connectOptions = %+v", providers.connectOptions)
	}
	if providers.connectOptions.Fingerprint.Value != "browser-fp" ||
		providers.connectOptions.Fingerprint.UserAgent != "Mozilla/5.0" ||
		providers.connectOptions.Fingerprint.IP != "203.0.113.10" {
		t.Fatalf("fingerprint = %+v", providers.connectOptions.Fingerprint)
	}
}

func TestProviderConnectReturnsConflictWhenUnconfigured(t *testing.T) {
	providers := newFakeProviderService()
	providers.status.Configured = false
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/providers/openai/connect", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", recorder.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "provider_not_configured" {
		t.Fatalf("error = %q, want provider_not_configured", body["error"])
	}
}

func TestProviderDisconnectReturnsNoContent(t *testing.T) {
	providers := newFakeProviderService()
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/providers/openai/disconnect", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", recorder.Code)
	}
	if !providers.disconnected {
		t.Fatal("provider service was not disconnected")
	}
}

func TestAdminProviderAccountsRequireSession(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	req := httptest.NewRequest(http.MethodGet, "/api/admin/providers/openai/accounts", nil)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestAdminProviderAccountsEndpointsRequireSession(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "list", method: http.MethodGet, path: "/api/admin/provider-accounts"},
		{name: "create api upstream", method: http.MethodPost, path: "/api/admin/provider-accounts/api-upstream", body: `{"name":"Upstream","baseUrl":"https://upstream.example.test","apiKey":"secret"}`},
		{name: "patch", method: http.MethodPatch, path: "/api/admin/provider-accounts/7", body: `{"enabled":true}`},
		{name: "list models", method: http.MethodGet, path: "/api/admin/provider-accounts/7/models"},
		{name: "replace models", method: http.MethodPut, path: "/api/admin/provider-accounts/7/models", body: `{"models":[{"model":"gpt-5","enabled":true}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", recorder.Code)
			}
		})
	}
}

func TestAdminProviderAccountsEndpointsRequireProviderService(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil)
	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "list", method: http.MethodGet, path: "/api/admin/provider-accounts"},
		{name: "create api upstream", method: http.MethodPost, path: "/api/admin/provider-accounts/api-upstream", body: `{"name":"Upstream","baseUrl":"https://upstream.example.test","apiKey":"secret"}`},
		{name: "patch", method: http.MethodPatch, path: "/api/admin/provider-accounts/7", body: `{"enabled":true}`},
		{name: "list models", method: http.MethodGet, path: "/api/admin/provider-accounts/7/models"},
		{name: "replace models", method: http.MethodPut, path: "/api/admin/provider-accounts/7/models", body: `{"models":[{"model":"gpt-5","enabled":true}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503", recorder.Code)
			}
			var body map[string]string
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["error"] != "service_unavailable" {
				t.Fatalf("error = %q, want service_unavailable", body["error"])
			}
		})
	}
}

func TestCreateAPIUpstreamAccount(t *testing.T) {
	providers := newFakeProviderService()
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/api-upstream", strings.NewReader(`{"name":" Upstream ","baseUrl":"https://upstream.example.test/v1/","apiKey":" secret ","enabled":true,"priority":8,"models":[" gpt-5 ","gpt-4.1"]}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s, want 201", recorder.Code, recorder.Body.String())
	}
	if providers.createdAPIUpstream.Name != " Upstream " || providers.createdAPIUpstream.BaseURL != "https://upstream.example.test/v1/" || providers.createdAPIUpstream.APIKey != " secret " {
		t.Fatalf("created input = %+v", providers.createdAPIUpstream)
	}
	if !providers.createdAPIUpstream.Enabled || providers.createdAPIUpstream.Priority != 8 || len(providers.createdAPIUpstream.Models) != 2 {
		t.Fatalf("created input scheduling/models = %+v", providers.createdAPIUpstream)
	}
	var body struct {
		Account provider.Account `json:"account"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Account.ID == 0 || body.Account.AccountType != provider.AccountTypeAPIUpstream {
		t.Fatalf("account = %+v", body.Account)
	}
	if strings.Contains(recorder.Body.String(), "secret") {
		t.Fatalf("response leaked api key: %s", recorder.Body.String())
	}
}

func TestCreateAPIUpstreamAccountMapsErrors(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/api-upstream", strings.NewReader(`{"name":"","baseUrl":"https://upstream.example.test","apiKey":"secret"}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", recorder.Code, recorder.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "invalid_input" {
		t.Fatalf("error = %q, want invalid_input", body["error"])
	}
}

func TestAdminProviderAccountMutationsRequireSession(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "patch", method: http.MethodPatch, path: "/api/admin/providers/openai/accounts/7", body: `{"enabled":true}`},
		{name: "refresh", method: http.MethodPost, path: "/api/admin/providers/openai/accounts/7/refresh"},
		{name: "disconnect", method: http.MethodPost, path: "/api/admin/providers/openai/accounts/7/disconnect"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", recorder.Code)
			}
		})
	}
}

func TestAdminCanListUnifiedProviderAccounts(t *testing.T) {
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true, Priority: 10}}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/provider-accounts", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"id":7`) {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestAdminCanUpdateUnifiedProviderAccount(t *testing.T) {
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true, Priority: 10}}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/provider-accounts/7", strings.NewReader(`{"enabled":false,"priority":2}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Account provider.Account `json:"account"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Account.ID != 7 || body.Account.Enabled || body.Account.Priority != 2 {
		t.Fatalf("account = %+v, want disabled account 7 priority 2", body.Account)
	}
}

func TestAdminCanListProviderAccounts(t *testing.T) {
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true, Priority: 10}}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/providers/openai/accounts", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"id":7`) {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestAdminCanUpdateProviderAccount(t *testing.T) {
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true, Priority: 10}}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/providers/openai/accounts/7", strings.NewReader(`{"enabled":false,"priority":2}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Account provider.Account `json:"account"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Account.ID != 7 || body.Account.Enabled || body.Account.Priority != 2 {
		t.Fatalf("account = %+v, want disabled account 7 priority 2", body.Account)
	}
}

func TestAdminUpdateProviderAccountRejectsEmptyPatch(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/providers/openai/accounts/7", strings.NewReader(`{}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "invalid_input" {
		t.Fatalf("error = %q, want invalid_input", body["error"])
	}
}

func TestAdminUpdateProviderAccountRejectsInvalidID(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/providers/openai/accounts/not-an-id", strings.NewReader(`{"priority":1}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "bad_request" {
		t.Fatalf("error = %q, want bad_request", body["error"])
	}
}

func TestAdminUpdateProviderAccountRejectsNegativePriority(t *testing.T) {
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true, Priority: 10}}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/providers/openai/accounts/7", strings.NewReader(`{"priority":-1}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "invalid_input" {
		t.Fatalf("error = %q, want invalid_input", body["error"])
	}
}

func TestAdminUpdateProviderAccountRejectsUnknownJSONField(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/providers/openai/accounts/7", strings.NewReader(`{"priority":1,"extra":true}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "bad_request" {
		t.Fatalf("error = %q, want bad_request", body["error"])
	}
}

func TestAdminUpdateProviderAccountMapsErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
		code string
	}{
		{name: "invalid input", err: provider.ErrInvalidInput, want: http.StatusBadRequest, code: "invalid_input"},
		{name: "not found", err: provider.ErrNotConnected, want: http.StatusNotFound, code: "not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			providers := newFakeProviderService()
			providers.updateErr = tc.err
			server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
			req := httptest.NewRequest(http.MethodPatch, "/api/admin/providers/openai/accounts/7", strings.NewReader(`{"priority":1}`))
			req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != tc.want {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.want)
			}
			var body map[string]string
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["error"] != tc.code {
				t.Fatalf("error = %q, want %s", body["error"], tc.code)
			}
		})
	}
}

func TestAdminCanRefreshProviderAccount(t *testing.T) {
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true, Priority: 10, Status: provider.AccountStatusCircuitOpen}}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/providers/openai/accounts/7/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if providers.refreshedAccountID != 7 {
		t.Fatalf("refreshedAccountID = %d, want 7", providers.refreshedAccountID)
	}
	var body struct {
		Account provider.Account `json:"account"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Account.ID != 7 || body.Account.Status != provider.AccountStatusActive || body.Account.LastRefreshAt == nil {
		t.Fatalf("account = %+v, want refreshed active account 7", body.Account)
	}
}

func TestAdminCanDisconnectProviderAccount(t *testing.T) {
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true, Priority: 10}}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/providers/openai/accounts/7/disconnect", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if providers.disconnectedAccountID != 7 {
		t.Fatalf("disconnectedAccountID = %d, want 7", providers.disconnectedAccountID)
	}
}

func TestAdminCanDeleteUnifiedProviderAccount(t *testing.T) {
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{{ID: 7, Provider: "openai", AccountType: provider.AccountTypeAPIUpstream, DisplayName: "Upstream", Enabled: true, Priority: 10}}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodDelete, "/api/admin/provider-accounts/7", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if providers.disconnectedAccountID != 7 {
		t.Fatalf("disconnectedAccountID = %d, want 7", providers.disconnectedAccountID)
	}
}

func TestAccountModelsRequireSession(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())

	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/api/admin/providers/openai/accounts/7/models"},
		{method: http.MethodPut, path: "/api/admin/providers/openai/accounts/7/models", body: `{"models":[{"model":"gpt-5","enabled":true}]}`},
	} {
		t.Run(tc.method, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", recorder.Code)
			}
		})
	}
}

func TestListAccountModelsReturnsModels(t *testing.T) {
	providers := newFakeProviderService()
	providers.accountModels[7] = []provider.AccountModel{
		{ID: 11, AccountID: 7, Provider: "openai", Model: "gpt-5", Enabled: true, Source: provider.AccountModelSourceManual},
		{ID: 12, AccountID: 7, Provider: "openai", Model: "gpt-5-mini", Enabled: false, Source: provider.AccountModelSourceManual},
	}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/providers/openai/accounts/7/models", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Models []provider.AccountModel `json:"models"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Models) != 2 || body.Models[0].Model != "gpt-5" || body.Models[1].Enabled {
		t.Fatalf("models = %+v", body.Models)
	}
}

func TestReplaceAccountModelsReturnsSavedModels(t *testing.T) {
	providers := newFakeProviderService()
	providers.accountModels[7] = []provider.AccountModel{}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/providers/openai/accounts/7/models", strings.NewReader(`{"models":[{"model":"gpt-5","enabled":true},{"model":"gpt-5-mini","enabled":false}]}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Models []provider.AccountModel `json:"models"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Models) != 2 || body.Models[0].Model != "gpt-5" || body.Models[1].Enabled {
		t.Fatalf("models = %+v", body.Models)
	}
	if got := providers.accountModels[7]; len(got) != 2 || got[0].Model != "gpt-5" {
		t.Fatalf("saved models = %+v", got)
	}
}

func TestUnifiedAccountModelsEndpoints(t *testing.T) {
	providers := newFakeProviderService()
	providers.accountModels[7] = []provider.AccountModel{
		{ID: 11, AccountID: 7, Provider: "openai", Model: "gpt-5", Enabled: true, Source: provider.AccountModelSourceManual},
	}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)

	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/provider-accounts/7/models", nil)
	listReq.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	listRecorder := httptest.NewRecorder()
	server.ServeHTTP(listRecorder, listReq)

	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s, want 200", listRecorder.Code, listRecorder.Body.String())
	}
	var listBody struct {
		Models []provider.AccountModel `json:"models"`
	}
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("decode list body: %v", err)
	}
	if len(listBody.Models) != 1 || listBody.Models[0].Model != "gpt-5" {
		t.Fatalf("list models = %+v", listBody.Models)
	}

	replaceReq := httptest.NewRequest(http.MethodPut, "/api/admin/provider-accounts/7/models", strings.NewReader(`{"models":[{"model":"gpt-4.1","enabled":true},{"model":"gpt-5","enabled":false}]}`))
	replaceReq.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	replaceRecorder := httptest.NewRecorder()
	server.ServeHTTP(replaceRecorder, replaceReq)

	if replaceRecorder.Code != http.StatusOK {
		t.Fatalf("replace status = %d body=%s, want 200", replaceRecorder.Code, replaceRecorder.Body.String())
	}
	var replaceBody struct {
		Models []provider.AccountModel `json:"models"`
	}
	if err := json.Unmarshal(replaceRecorder.Body.Bytes(), &replaceBody); err != nil {
		t.Fatalf("decode replace body: %v", err)
	}
	if len(replaceBody.Models) != 2 || replaceBody.Models[0].Model != "gpt-4.1" || replaceBody.Models[1].Enabled {
		t.Fatalf("replace models = %+v", replaceBody.Models)
	}
}

func TestAccountModelsMapProviderErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		method string
		err    error
		want   int
		code   string
	}{
		{name: "list invalid input", method: http.MethodGet, err: provider.ErrInvalidInput, want: http.StatusBadRequest, code: "invalid_input"},
		{name: "list not found", method: http.MethodGet, err: provider.ErrNotConnected, want: http.StatusNotFound, code: "not_found"},
		{name: "replace invalid input", method: http.MethodPut, err: provider.ErrInvalidInput, want: http.StatusBadRequest, code: "invalid_input"},
		{name: "replace not found", method: http.MethodPut, err: provider.ErrNotConnected, want: http.StatusNotFound, code: "not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			providers := newFakeProviderService()
			if tc.method == http.MethodGet {
				providers.accountModelsErr = tc.err
			} else {
				providers.replaceModelsErr = tc.err
			}
			server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
			req := httptest.NewRequest(tc.method, "/api/admin/providers/openai/accounts/7/models", strings.NewReader(`{"models":[{"model":"gpt-5","enabled":true}]}`))
			req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != tc.want {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.want)
			}
			var body map[string]string
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["error"] != tc.code {
				t.Fatalf("error = %q, want %s", body["error"], tc.code)
			}
		})
	}
}

func TestAdminDisconnectProviderAccountMapsErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
		code string
	}{
		{name: "invalid input", err: provider.ErrInvalidInput, want: http.StatusBadRequest, code: "invalid_input"},
		{name: "not found", err: provider.ErrNotConnected, want: http.StatusNotFound, code: "not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			providers := newFakeProviderService()
			providers.disconnectErr = tc.err
			server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
			req := httptest.NewRequest(http.MethodPost, "/api/admin/providers/openai/accounts/7/disconnect", nil)
			req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != tc.want {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.want)
			}
			var body map[string]string
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["error"] != tc.code {
				t.Fatalf("error = %q, want %s", body["error"], tc.code)
			}
		})
	}
}

func TestAdminDeleteUnifiedProviderAccountMapsErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		err  error
		want int
		code string
	}{
		{name: "bad id", path: "/api/admin/provider-accounts/not-a-number", want: http.StatusBadRequest, code: "bad_request"},
		{name: "invalid input", path: "/api/admin/provider-accounts/7", err: provider.ErrInvalidInput, want: http.StatusBadRequest, code: "invalid_input"},
		{name: "not found", path: "/api/admin/provider-accounts/7", err: provider.ErrNotConnected, want: http.StatusNotFound, code: "not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			providers := newFakeProviderService()
			providers.disconnectErr = tc.err
			server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
			req := httptest.NewRequest(http.MethodDelete, tc.path, nil)
			req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != tc.want {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.want)
			}
			var body map[string]string
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["error"] != tc.code {
				t.Fatalf("error = %q, want %s", body["error"], tc.code)
			}
		})
	}
}

func TestProviderCallbackDoesNotConsumeManualCallback(t *testing.T) {
	providers := newFakeProviderService()
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://localhost:3000/oauth/openai/callback?code=abc&state=state", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if providers.callbackCode != "" || providers.callbackState != "" {
		t.Fatalf("callback was called with code %q state %q", providers.callbackCode, providers.callbackState)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "code=abc") || !strings.Contains(body, "state=state") {
		t.Fatalf("body did not include callback values: %s", body)
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", got)
	}
}

func TestProviderManualCallbackCompletesFromCallbackURL(t *testing.T) {
	providers := newFakeProviderService()
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/providers/openai/callback", strings.NewReader(`{"callbackUrl":"http://localhost:3000/oauth/openai/callback?code=abc&state=oauth_state"}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if providers.callbackCode != "abc" || providers.callbackState != "oauth_state" {
		t.Fatalf("callback args = code %q state %q, want parsed callback URL values", providers.callbackCode, providers.callbackState)
	}
}

func TestProviderManualCallbackRejectsMissingCallbackValues(t *testing.T) {
	providers := newFakeProviderService()
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/providers/openai/callback", strings.NewReader(`{"callbackUrl":"http://localhost:3000/oauth/openai/callback?code=abc"}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	if providers.callbackCode != "" || providers.callbackState != "" {
		t.Fatalf("callback was called with code %q state %q", providers.callbackCode, providers.callbackState)
	}
}

func TestListRequestLogsRequiresSessionAndReturnsLogs(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/request-logs", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/request-logs?limit=20", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder = httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body struct {
		Logs []admin.RequestLog `json:"logs"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Logs) != 1 || body.Logs[0].RequestID != "req_3" {
		t.Fatalf("logs = %+v", body.Logs)
	}
}

func TestModelSettingsRequiresSessionAndReturnsSettings(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/model-settings", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/model-settings", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder = httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body admin.ModelSettings
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.DefaultModel != "gpt-4.1" || len(body.AllowedModels) != 2 {
		t.Fatalf("model settings = %+v", body)
	}
}

func TestUpdateModelSettingsReturnsSavedSettings(t *testing.T) {
	admins := newFakeAdminService()
	server := NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService())
	req := httptest.NewRequest(http.MethodPut, "/api/admin/model-settings", strings.NewReader(`{"defaultModel":"gpt-5","allowedModels":["gpt-5","gpt-5-mini"]}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body admin.ModelSettings
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.DefaultModel != "gpt-5" || !slices.Equal(body.AllowedModels, []string{"gpt-5", "gpt-5-mini"}) {
		t.Fatalf("model settings = %+v", body)
	}
}

func TestUpdateModelSettingsReturnsBadRequestForInvalidInput(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	req := httptest.NewRequest(http.MethodPut, "/api/admin/model-settings", strings.NewReader(`{"defaultModel":"gpt-5","allowedModels":["gpt-5-mini"]}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "invalid_input" {
		t.Fatalf("error = %q, want invalid_input", body["error"])
	}
}

func TestModelRoutingReturnsStatus(t *testing.T) {
	admins := newFakeAdminService()
	admins.modelSettings = admin.ModelSettings{
		DefaultModel:  "gpt-5",
		AllowedModels: []string{"gpt-5", "gpt-5-mini", "codex-mini"},
	}
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{
		{ID: 7, Provider: "openai", Enabled: true},
		{ID: 8, Provider: "openai", Enabled: false},
	}
	providers.accountModels[7] = []provider.AccountModel{
		{ID: 11, AccountID: 7, Provider: "openai", Model: "gpt-5", Enabled: true, Source: provider.AccountModelSourceManual},
		{ID: 12, AccountID: 7, Provider: "openai", Model: "gpt-5-mini", Enabled: false, Source: provider.AccountModelSourceManual},
	}
	providers.accountModels[8] = []provider.AccountModel{
		{ID: 13, AccountID: 8, Provider: "openai", Model: "gpt-5", Enabled: true, Source: provider.AccountModelSourceManual},
		{ID: 14, AccountID: 8, Provider: "openai", Model: "unallowed-model", Enabled: true, Source: provider.AccountModelSourceManual},
	}
	providers.exposedModels = []provider.ExposedModel{
		{ID: "gpt-5", OwnedBy: "openai"},
		{ID: "gpt-5-mini", OwnedBy: "openai"},
	}
	server := NewServer(config.Config{}, staticHealth{}, admins, providers)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/model-routing", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body admin.ModelRoutingStatus
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.DefaultModel != "gpt-5" || !slices.Equal(body.AllowedModels, []string{"gpt-5", "gpt-5-mini", "codex-mini"}) {
		t.Fatalf("routing settings = %+v", body)
	}
	if len(body.Models) != 4 {
		t.Fatalf("models length = %d, want 4: %+v", len(body.Models), body.Models)
	}
	if body.Models[0] != (admin.ModelRoutingModel{Model: "gpt-5", Allowed: true, ConfiguredCount: 2, EnabledCount: 1}) {
		t.Fatalf("first model = %+v", body.Models[0])
	}
	if body.Models[2] != (admin.ModelRoutingModel{Model: "codex-mini", Allowed: true}) {
		t.Fatalf("third model = %+v", body.Models[2])
	}
	if body.Models[3] != (admin.ModelRoutingModel{Model: "unallowed-model", ConfiguredCount: 1}) {
		t.Fatalf("fourth model = %+v", body.Models[3])
	}
	if len(body.Warnings) != 1 || !strings.Contains(body.Warnings[0], "codex-mini") {
		t.Fatalf("warnings = %+v, want missing codex-mini warning", body.Warnings)
	}
}

func TestModelRoutingStatusEnabledCountUsesSchedulableAccountRules(t *testing.T) {
	admins := newFakeAdminService()
	admins.modelSettings = admin.ModelSettings{DefaultModel: "gpt-5", AllowedModels: []string{"gpt-5"}}
	now := time.Now()
	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{
		{ID: 7, Provider: "openai", Enabled: true, Status: provider.AccountStatusExpired},
		{ID: 8, Provider: "openai", Enabled: true, Status: provider.AccountStatusRateLimited, RateLimitedUntil: &future},
		{ID: 9, Provider: "openai", Enabled: true, Status: provider.AccountStatusCircuitOpen, CircuitOpenUntil: &future},
		{ID: 10, Provider: "openai", Enabled: true, Status: provider.AccountStatusRateLimited, RateLimitedUntil: &past},
		{ID: 11, Provider: "openai", Enabled: true, Status: provider.AccountStatusCircuitOpen, CircuitOpenUntil: &past},
	}
	for _, account := range providers.accounts {
		providers.accountModels[account.ID] = []provider.AccountModel{
			{AccountID: account.ID, Provider: "openai", Model: "gpt-5", Enabled: true, Source: provider.AccountModelSourceManual},
		}
	}
	providers.exposedModels = []provider.ExposedModel{{ID: "gpt-5", OwnedBy: "openai"}}
	server := NewServer(config.Config{}, staticHealth{}, admins, providers)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/model-routing", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body admin.ModelRoutingStatus
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Models) != 1 {
		t.Fatalf("models = %+v, want one model", body.Models)
	}
	want := admin.ModelRoutingModel{Model: "gpt-5", Allowed: true, ConfiguredCount: 5, EnabledCount: 2}
	if body.Models[0] != want {
		t.Fatalf("model = %+v, want %+v", body.Models[0], want)
	}
}

func TestV1RoutesUseGatewayHandler(t *testing.T) {
	gateway := &fakeGatewayHandler{}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService(), gateway)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/models", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if !gateway.called {
		t.Fatal("gateway handler was not called")
	}
	if recorder.Body.String() != `{"object":"list","data":[]}` {
		t.Fatalf("body = %q", recorder.Body.String())
	}
}

func TestServesStaticFrontendAndSPAFallback(t *testing.T) {
	web := fstest.MapFS{
		"index.html":            {Data: []byte("<!doctype html><title>N2API</title><main>index</main>")},
		"200.html":              {Data: []byte("<!doctype html><title>N2API</title><main>fallback</main>")},
		"_app/immutable/app.js": {Data: []byte("console.log('app')")},
	}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService(), nil, web)

	for _, tc := range []struct {
		path string
		want string
	}{
		{path: "/", want: "index"},
		{path: "/settings/provider", want: "fallback"},
		{path: "/_app/immutable/app.js", want: "console.log('app')"},
	} {
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", tc.path, recorder.Code)
		}
		if !strings.Contains(recorder.Body.String(), tc.want) {
			t.Fatalf("%s body = %q, want %q", tc.path, recorder.Body.String(), tc.want)
		}
	}
}

func TestBadAdminJSONReturnsBadRequest(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(`{`)))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestAdminJSONWithTrailingGarbageReturnsBadRequest(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil)
	recorder := httptest.NewRecorder()
	body := strings.NewReader(`{"username":"admin","password":"secret"} garbage`)

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/admin/login", body))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestAdminJSONWithSecondValueReturnsBadRequest(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil)
	recorder := httptest.NewRecorder()
	body := strings.NewReader(`{"username":"admin","password":"secret"} {}`)

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/admin/login", body))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestUnknownAdminPathReturnsJSONNotFound(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil)
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

func TestAdminRootPathReturnsJSONNotFoundWithoutRedirect(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin", nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	if got := recorder.Header().Get("Location"); got != "" {
		t.Fatalf("Location = %q, want empty", got)
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
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/login", nil))

	if recorder.Code == http.StatusOK {
		t.Fatalf("status = 200, want non-200")
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type = %q, want application/json", got)
	}
}
