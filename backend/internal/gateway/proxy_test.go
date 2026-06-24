package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/KnowSky404/N2API/backend/internal/provider"
)

type fakeAPIKeyAuthenticator struct {
	gotKey string
	err    error
	key    admin.APIKey
	keys   map[string]admin.APIKey
}

func (a *fakeAPIKeyAuthenticator) AuthenticateAPIKey(_ context.Context, apiKey string) (admin.APIKey, error) {
	a.gotKey = apiKey
	if a.err != nil {
		return admin.APIKey{}, a.err
	}
	if a.keys != nil {
		key, ok := a.keys[apiKey]
		if !ok {
			return admin.APIKey{}, admin.ErrUnauthorized
		}
		return key, nil
	}
	if a.key.ID != 0 {
		return a.key, nil
	}
	return admin.APIKey{ID: 42, Name: "test key"}, nil
}

type fakeSelectedAccountProvider struct {
	accounts     []SelectedAccount
	errs         []error
	calls        int
	models       []string
	sessions     []string
	poolIDs      []int64
	chainPoolIDs []int64
	exclusions   [][]int64
	failures     []reportedAccountFailure
	used         []int64
}

type reportedAccountFailure struct {
	accountID  int64
	statusCode int
	retryAfter string
	message    string
}

func (p *fakeSelectedAccountProvider) SelectAccountForModel(ctx context.Context, model string, excludedAccountIDs ...int64) (SelectedAccount, error) {
	i := p.calls
	p.calls++
	p.models = append(p.models, model)
	p.exclusions = append(p.exclusions, append([]int64(nil), excludedAccountIDs...))
	if i < len(p.errs) && p.errs[i] != nil {
		if i < len(p.accounts) {
			return p.accounts[i], p.errs[i]
		}
		return SelectedAccount{}, p.errs[i]
	}
	if i < len(p.accounts) {
		return p.accounts[i], nil
	}
	return SelectedAccount{}, provider.ErrAccountsUnavailable
}

func (p *fakeSelectedAccountProvider) SelectAccountForModelAndSession(ctx context.Context, model, sessionID string, excludedAccountIDs ...int64) (SelectedAccount, error) {
	p.sessions = append(p.sessions, sessionID)
	return p.SelectAccountForModel(ctx, model, excludedAccountIDs...)
}

func (p *fakeSelectedAccountProvider) SelectAccountForModelInRoutingPool(ctx context.Context, routingPoolID int64, model string, excludedAccountIDs ...int64) (SelectedAccount, error) {
	p.poolIDs = append(p.poolIDs, routingPoolID)
	return p.SelectAccountForModel(ctx, model, excludedAccountIDs...)
}

func (p *fakeSelectedAccountProvider) SelectAccountForModelAndSessionInRoutingPool(ctx context.Context, routingPoolID int64, model, sessionID string, excludedAccountIDs ...int64) (SelectedAccount, error) {
	p.poolIDs = append(p.poolIDs, routingPoolID)
	p.sessions = append(p.sessions, sessionID)
	return p.SelectAccountForModel(ctx, model, excludedAccountIDs...)
}

func (p *fakeSelectedAccountProvider) SelectAccountForModelInRoutingPoolChain(ctx context.Context, routingPoolID int64, model string, excludedAccountIDs ...int64) (SelectedAccount, error) {
	p.chainPoolIDs = append(p.chainPoolIDs, routingPoolID)
	return p.SelectAccountForModel(ctx, model, excludedAccountIDs...)
}

func (p *fakeSelectedAccountProvider) SelectAccountForModelAndSessionInRoutingPoolChain(ctx context.Context, routingPoolID int64, model, sessionID string, excludedAccountIDs ...int64) (SelectedAccount, error) {
	p.chainPoolIDs = append(p.chainPoolIDs, routingPoolID)
	p.sessions = append(p.sessions, sessionID)
	return p.SelectAccountForModel(ctx, model, excludedAccountIDs...)
}

func (p *fakeSelectedAccountProvider) RecordAccountFailure(_ context.Context, accountID int64, statusCode int, retryAfter, message string) error {
	p.failures = append(p.failures, reportedAccountFailure{
		accountID:  accountID,
		statusCode: statusCode,
		retryAfter: retryAfter,
		message:    message,
	})
	return nil
}

func (p *fakeSelectedAccountProvider) RecordAccountUsed(_ context.Context, accountID int64) error {
	p.used = append(p.used, accountID)
	return nil
}

type fakeModelProvider struct {
	defaultModel       string
	allowedModels      []string
	exposedModels      []ExposedModel
	chainExposedModels []ExposedModel
	chainPoolIDs       []int64
}

func (p fakeModelProvider) DefaultModel(context.Context) (string, error) {
	return p.defaultModel, nil
}

func (p fakeModelProvider) IsModelAllowed(_ context.Context, model string) (bool, error) {
	for _, allowed := range p.allowedModels {
		if allowed == model {
			return true, nil
		}
	}
	return false, nil
}

func (p fakeModelProvider) ListExposedModels(context.Context) ([]ExposedModel, error) {
	return append([]ExposedModel(nil), p.exposedModels...), nil
}

func (p *fakeModelProvider) ListExposedModelsForRoutingPoolChain(_ context.Context, routingPoolID int64) ([]ExposedModel, error) {
	p.chainPoolIDs = append(p.chainPoolIDs, routingPoolID)
	return append([]ExposedModel(nil), p.chainExposedModels...), nil
}

type fakeRequestLogger struct {
	entries []RequestLog
}

func (l *fakeRequestLogger) CreateRequestLog(_ context.Context, entry RequestLog) error {
	l.entries = append(l.entries, entry)
	return nil
}

func assertLastLoggedError(t *testing.T, logger *fakeRequestLogger, want string) {
	t.Helper()
	if len(logger.entries) == 0 {
		t.Fatalf("logged entries = 0, want last error %q", want)
	}
	if got := logger.entries[len(logger.entries)-1].Error; got != want {
		t.Fatalf("last logged error = %q, want %q", got, want)
	}
}

type fakeUsagePricer struct {
	estimate UsageCostEstimate
	err      error
	usage    Usage
}

func (p *fakeUsagePricer) EstimateUsageCost(_ context.Context, usage Usage) (UsageCostEstimate, error) {
	p.usage = usage
	if p.err != nil {
		return UsageCostEstimate{}, p.err
	}
	return p.estimate, nil
}

type fakeGatewaySettingsProvider struct {
	settings admin.GatewaySettings
	err      error
}

func (p *fakeGatewaySettingsProvider) GetGatewaySettings(context.Context) (admin.GatewaySettings, error) {
	if p.err != nil {
		return admin.GatewaySettings{}, p.err
	}
	return p.settings, nil
}

type fakeBudgetProvider struct {
	usage admin.APIKeyBudgetUsage
	err   error
	calls int
	key   admin.APIKey
}

func (p *fakeBudgetProvider) GetAPIKeyBudgetUsage(_ context.Context, key admin.APIKey, _ time.Time) (admin.APIKeyBudgetUsage, error) {
	p.calls++
	p.key = key
	if p.err != nil {
		return admin.APIKeyBudgetUsage{}, p.err
	}
	return p.usage, nil
}

func TestProxyRequiresBearerAPIKey(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("upstream should not be called without client API key")
	}))
	defer upstream.Close()
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "upstream-token"}}}, Config{
		UpstreamBaseURL: upstream.URL,
	})
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/models", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "unauthorized") {
		t.Fatalf("body = %q, want unauthorized error", recorder.Body.String())
	}
}

func TestProxyModelsReturnsLocalAggregateList(t *testing.T) {
	upstreamCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalled = true
		t.Fatal("upstream should not be called for local models list")
	}))
	defer upstream.Close()
	auth := &fakeAPIKeyAuthenticator{}
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "upstream-token"}}}
	proxy := NewProxy(auth, tokens, Config{
		UpstreamBaseURL: upstream.URL,
		ModelProvider:   fakeModelProvider{exposedModels: []ExposedModel{{ID: "gpt-5", OwnedBy: "openai"}}},
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if auth.gotKey != "n2api_client_secret" {
		t.Fatalf("authenticated key = %q, want client secret", auth.gotKey)
	}
	if upstreamCalled {
		t.Fatal("upstream was called")
	}
	if tokens.calls != 0 {
		t.Fatalf("token calls = %d, want 0", tokens.calls)
	}
	var body struct {
		Object string `json:"object"`
		Data   []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Created int64  `json:"created"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("Unmarshal response returned error: %v; body=%s", err, recorder.Body.String())
	}
	if body.Object != "list" || len(body.Data) != 1 {
		t.Fatalf("models response = %+v", body)
	}
	if got := body.Data[0]; got.ID != "gpt-5" || got.Object != "model" || got.Created != 0 || got.OwnedBy != "openai" {
		t.Fatalf("model = %+v, want local gpt-5 model", got)
	}
}

func TestProxyModelsForPoolBoundKeyUseRoutingPoolChain(t *testing.T) {
	poolID := int64(1)
	models := &fakeModelProvider{
		exposedModels:      []ExposedModel{{ID: "global-only", OwnedBy: "openai"}},
		chainExposedModels: []ExposedModel{{ID: "gpt-5", OwnedBy: "openai"}},
	}
	proxy := NewProxy(&fakeAPIKeyAuthenticator{key: admin.APIKey{ID: 42, RoutingPoolID: &poolID}}, &fakeSelectedAccountProvider{}, Config{
		ModelProvider: models,
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if !slices.Equal(models.chainPoolIDs, []int64{1}) {
		t.Fatalf("model chain pool calls = %+v, want pool 1", models.chainPoolIDs)
	}
	if !strings.Contains(recorder.Body.String(), `"id":"gpt-5"`) {
		t.Fatalf("models body = %q, want fallback chain model gpt-5", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "global-only") {
		t.Fatalf("models body = %q, should not include global-only model", recorder.Body.String())
	}
}

func TestProxyModelsFiltersLocalListForSelectedAPIKey(t *testing.T) {
	auth := &fakeAPIKeyAuthenticator{key: admin.APIKey{
		ID:            42,
		Name:          "test key",
		ModelPolicy:   admin.APIKeyModelPolicySelected,
		AllowedModels: []string{"gpt-5-mini", "gpt-4o"},
	}}
	accounts := &fakeSelectedAccountProvider{}
	proxy := NewProxy(auth, accounts, Config{
		ModelProvider: fakeModelProvider{exposedModels: []ExposedModel{
			{ID: "gpt-5", OwnedBy: "openai"},
			{ID: "gpt-5-mini", OwnedBy: "openai"},
			{ID: "gpt-4o", OwnedBy: "openai"},
		}},
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if accounts.calls != 0 {
		t.Fatalf("account calls = %d, want 0", accounts.calls)
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("Unmarshal response returned error: %v; body=%s", err, recorder.Body.String())
	}
	var ids []string
	for _, model := range body.Data {
		ids = append(ids, model.ID)
	}
	if !slices.Equal(ids, []string{"gpt-5-mini", "gpt-4o"}) {
		t.Fatalf("model IDs = %+v, want selected intersection", ids)
	}
}

func TestProxyRoutesChatCompletionByRequestedModel(t *testing.T) {
	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll returned error: %v", err)
		}
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chatcmpl_123"}`))
	}))
	defer upstream.Close()
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "upstream-token"}}}
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, tokens, Config{
		UpstreamBaseURL: upstream.URL,
		ModelProvider: fakeModelProvider{
			defaultModel:  "gpt-5-mini",
			allowedModels: []string{"gpt-5", "gpt-5-mini"},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if tokens.calls != 1 || len(tokens.models) != 1 || tokens.models[0] != "gpt-5" {
		t.Fatalf("requested models = %+v, calls=%d; want gpt-5", tokens.models, tokens.calls)
	}
	if gotBody != `{"model":"gpt-5","messages":[]}` {
		t.Fatalf("upstream body = %q", gotBody)
	}
}

func TestProxyRoutesRequestWithSessionIDForStickySelection(t *testing.T) {
	const requestBody = `{"model":"gpt-5","session_id":" workspace-123 ","messages":[]}`
	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll returned error: %v", err)
		}
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chatcmpl_123"}`))
	}))
	defer upstream.Close()
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "upstream-token"}}}
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, tokens, Config{
		UpstreamBaseURL: upstream.URL,
		ModelProvider: fakeModelProvider{
			defaultModel:  "gpt-5-mini",
			allowedModels: []string{"gpt-5", "gpt-5-mini"},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(requestBody))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if tokens.calls != 1 || len(tokens.models) != 1 || tokens.models[0] != "gpt-5" {
		t.Fatalf("requested models = %+v, calls=%d; want gpt-5", tokens.models, tokens.calls)
	}
	if !slices.Equal(tokens.sessions, []string{"workspace-123"}) {
		t.Fatalf("sessions = %+v, want trimmed workspace-123", tokens.sessions)
	}
	if gotBody != requestBody {
		t.Fatalf("upstream body = %q, want original body", gotBody)
	}
}

func TestProxyRoutesRequestWithSessionIDHeaderForStickySelection(t *testing.T) {
	const requestBody = `{"model":"gpt-5","messages":[]}`
	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll returned error: %v", err)
		}
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chatcmpl_123"}`))
	}))
	defer upstream.Close()
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "upstream-token"}}}
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, tokens, Config{
		UpstreamBaseURL: upstream.URL,
		ModelProvider: fakeModelProvider{
			defaultModel:  "gpt-5-mini",
			allowedModels: []string{"gpt-5", "gpt-5-mini"},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(requestBody))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("session_id", " workspace-header-123 ")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if tokens.calls != 1 || len(tokens.models) != 1 || tokens.models[0] != "gpt-5" {
		t.Fatalf("requested models = %+v, calls=%d; want gpt-5", tokens.models, tokens.calls)
	}
	if !slices.Equal(tokens.sessions, []string{"workspace-header-123"}) {
		t.Fatalf("sessions = %+v, want trimmed header workspace-header-123", tokens.sessions)
	}
	if gotBody != requestBody {
		t.Fatalf("upstream body = %q, want original body", gotBody)
	}
}

func TestProxyRoutesRequestWithN2APISessionHeaderForStickySelection(t *testing.T) {
	const requestBody = `{"model":"gpt-5","messages":[]}`
	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll returned error: %v", err)
		}
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chatcmpl_123"}`))
	}))
	defer upstream.Close()
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "upstream-token"}}}
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, tokens, Config{
		UpstreamBaseURL: upstream.URL,
		ModelProvider: fakeModelProvider{
			defaultModel:  "gpt-5-mini",
			allowedModels: []string{"gpt-5", "gpt-5-mini"},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(requestBody))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-N2API-Session-ID", " workspace-header-456 ")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if tokens.calls != 1 || len(tokens.models) != 1 || tokens.models[0] != "gpt-5" {
		t.Fatalf("requested models = %+v, calls=%d; want gpt-5", tokens.models, tokens.calls)
	}
	if !slices.Equal(tokens.sessions, []string{"workspace-header-456"}) {
		t.Fatalf("sessions = %+v, want trimmed header workspace-header-456", tokens.sessions)
	}
	if gotBody != requestBody {
		t.Fatalf("upstream body = %q, want original body", gotBody)
	}
}

func TestProxyRoutesResponsesGetWithSessionHeaderForStickySelection(t *testing.T) {
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "upstream-token"}}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/responses/resp_123" {
			t.Fatalf("upstream path = %q, want responses lookup", r.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"resp_123"}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, tokens, Config{UpstreamBaseURL: "https://upstream.example.test"}, client)
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	req.Header.Set("X-N2API-Session-ID", " workspace-header-789 ")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if tokens.calls != 1 || len(tokens.models) != 1 || tokens.models[0] != "" {
		t.Fatalf("requested models = %+v, calls=%d; want empty model for GET responses route", tokens.models, tokens.calls)
	}
	if !slices.Equal(tokens.sessions, []string{"workspace-header-789"}) {
		t.Fatalf("sessions = %+v, want trimmed header workspace-header-789", tokens.sessions)
	}
}

func TestProxyPrefersBodySessionIDOverHeaderForStickySelection(t *testing.T) {
	const requestBody = `{"model":"gpt-5","session_id":" body-session ","messages":[]}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chatcmpl_123"}`))
	}))
	defer upstream.Close()
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "upstream-token"}}}
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, tokens, Config{
		UpstreamBaseURL: upstream.URL,
		ModelProvider: fakeModelProvider{
			defaultModel:  "gpt-5-mini",
			allowedModels: []string{"gpt-5", "gpt-5-mini"},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(requestBody))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("session_id", "header-session")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if !slices.Equal(tokens.sessions, []string{"body-session"}) {
		t.Fatalf("sessions = %+v, want body session to take precedence", tokens.sessions)
	}
}

func TestProxyRejectsWhenConcurrentRequestLimitIsFull(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan struct{})
	var transportCalls int32
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{
		{AccountID: 1, AuthorizationToken: "first-token"},
		{AccountID: 2, AuthorizationToken: "second-token"},
	}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		call := atomic.AddInt32(&transportCalls, 1)
		if call == 1 {
			close(firstStarted)
			<-releaseFirst
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, tokens, Config{
		UpstreamBaseURL:              "https://upstream.example.test",
		MaxConcurrentGatewayRequests: 1,
		ModelProvider: fakeModelProvider{
			defaultModel:  "gpt-5",
			allowedModels: []string{"gpt-5"},
		},
	}, client)
	firstReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	firstReq.Header.Set("Authorization", "Bearer n2api_client_secret")
	firstReq.Header.Set("Content-Type", "application/json")
	firstRecorder := httptest.NewRecorder()
	go func() {
		defer close(firstDone)
		proxy.ServeHTTP(firstRecorder, firstReq)
	}()
	<-firstStarted

	secondReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	secondReq.Header.Set("Authorization", "Bearer n2api_client_secret")
	secondReq.Header.Set("Content-Type", "application/json")
	secondRecorder := httptest.NewRecorder()
	proxy.ServeHTTP(secondRecorder, secondReq)

	close(releaseFirst)
	<-firstDone
	if secondRecorder.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d body=%s, want 429", secondRecorder.Code, secondRecorder.Body.String())
	}
	if !strings.Contains(secondRecorder.Body.String(), "rate_limit_exceeded") {
		t.Fatalf("second body = %q, want rate_limit_exceeded", secondRecorder.Body.String())
	}
	if tokens.calls != 1 {
		t.Fatalf("account selections = %d, want only first request selection", tokens.calls)
	}
	if got := atomic.LoadInt32(&transportCalls); got != 1 {
		t.Fatalf("transport calls = %d, want only first request upstream call", got)
	}
	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("first status = %d body=%s, want 200", firstRecorder.Code, firstRecorder.Body.String())
	}
}

func TestProxyUsesDynamicGatewaySettingsForConcurrencyLimit(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan struct{})
	var transportCalls int32
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{
		{AccountID: 1, AuthorizationToken: "first-token"},
		{AccountID: 2, AuthorizationToken: "second-token"},
	}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		call := atomic.AddInt32(&transportCalls, 1)
		if call == 1 {
			close(firstStarted)
			<-releaseFirst
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})}
	settings := &fakeGatewaySettingsProvider{settings: admin.GatewaySettings{MaxConcurrentGatewayRequests: 1}}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, tokens, Config{
		UpstreamBaseURL:  "https://upstream.example.test",
		SettingsProvider: settings,
		ModelProvider: fakeModelProvider{
			defaultModel:  "gpt-5",
			allowedModels: []string{"gpt-5"},
		},
	}, client)
	firstReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	firstReq.Header.Set("Authorization", "Bearer n2api_client_secret")
	firstReq.Header.Set("Content-Type", "application/json")
	firstRecorder := httptest.NewRecorder()
	go func() {
		defer close(firstDone)
		proxy.ServeHTTP(firstRecorder, firstReq)
	}()
	<-firstStarted

	secondReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	secondReq.Header.Set("Authorization", "Bearer n2api_client_secret")
	secondReq.Header.Set("Content-Type", "application/json")
	secondRecorder := httptest.NewRecorder()
	proxy.ServeHTTP(secondRecorder, secondReq)

	close(releaseFirst)
	<-firstDone
	if secondRecorder.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d body=%s, want 429", secondRecorder.Code, secondRecorder.Body.String())
	}
	if !strings.Contains(secondRecorder.Body.String(), "rate_limit_exceeded") {
		t.Fatalf("second body = %q, want rate_limit_exceeded", secondRecorder.Body.String())
	}
	if tokens.calls != 1 {
		t.Fatalf("account selections = %d, want only first request selection", tokens.calls)
	}
	if got := atomic.LoadInt32(&transportCalls); got != 1 {
		t.Fatalf("transport calls = %d, want only first request upstream call", got)
	}
	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("first status = %d body=%s, want 200", firstRecorder.Code, firstRecorder.Body.String())
	}
}

func TestProxyRetriesAnotherAccountWhenSelectedAccountConcurrencyLimitIsFull(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan struct{})
	var transportCalls int32
	var secondAuthorization string
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{
		{AccountID: 1, AuthorizationToken: "first-token"},
		{AccountID: 1, AuthorizationToken: "first-token"},
		{AccountID: 2, AuthorizationToken: "second-token"},
	}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		call := atomic.AddInt32(&transportCalls, 1)
		if call == 1 {
			close(firstStarted)
			<-releaseFirst
		} else {
			secondAuthorization = r.Header.Get("Authorization")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, tokens, Config{
		UpstreamBaseURL:                 "https://upstream.example.test",
		MaxConcurrentRequestsPerAccount: 1,
		ModelProvider: fakeModelProvider{
			defaultModel:  "gpt-5",
			allowedModels: []string{"gpt-5"},
		},
	}, client)
	firstReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	firstReq.Header.Set("Authorization", "Bearer n2api_client_secret")
	firstReq.Header.Set("Content-Type", "application/json")
	firstRecorder := httptest.NewRecorder()
	go func() {
		defer close(firstDone)
		proxy.ServeHTTP(firstRecorder, firstReq)
	}()
	<-firstStarted

	secondReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	secondReq.Header.Set("Authorization", "Bearer n2api_client_secret")
	secondReq.Header.Set("Content-Type", "application/json")
	secondRecorder := httptest.NewRecorder()
	proxy.ServeHTTP(secondRecorder, secondReq)

	close(releaseFirst)
	<-firstDone
	if secondRecorder.Code != http.StatusOK {
		t.Fatalf("second status = %d body=%s, want 200", secondRecorder.Code, secondRecorder.Body.String())
	}
	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("first status = %d body=%s, want 200", firstRecorder.Code, firstRecorder.Body.String())
	}
	if tokens.calls != 3 {
		t.Fatalf("account selections = %d, want first request plus retry selection", tokens.calls)
	}
	if !slices.Equal(tokens.exclusions[2], []int64{1}) {
		t.Fatalf("third selection exclusions = %+v, want busy account 1 excluded", tokens.exclusions[2])
	}
	if got := atomic.LoadInt32(&transportCalls); got != 2 {
		t.Fatalf("transport calls = %d, want both requests upstream", got)
	}
	if secondAuthorization != "Bearer second-token" {
		t.Fatalf("second upstream Authorization = %q, want fallback account token", secondAuthorization)
	}
	if !slices.Equal(tokens.used, []int64{1, 2}) {
		t.Fatalf("used accounts = %+v, want only successfully acquired accounts 1 and 2", tokens.used)
	}
	if len(tokens.failures) != 0 {
		t.Fatalf("recorded failures = %+v, want none for local account concurrency", tokens.failures)
	}
}

func TestProxyUsesSelectedAccountConcurrencyOverride(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan struct{})
	var transportCalls int32
	var secondAuthorization string
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{
		{AccountID: 1, AuthorizationToken: "first-token", MaxConcurrentRequests: 1},
		{AccountID: 1, AuthorizationToken: "first-token", MaxConcurrentRequests: 1},
		{AccountID: 2, AuthorizationToken: "second-token"},
	}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		call := atomic.AddInt32(&transportCalls, 1)
		if call == 1 {
			close(firstStarted)
			<-releaseFirst
		} else {
			secondAuthorization = r.Header.Get("Authorization")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, tokens, Config{
		UpstreamBaseURL:                 "https://upstream.example.test",
		MaxConcurrentRequestsPerAccount: 5,
		ModelProvider: fakeModelProvider{
			defaultModel:  "gpt-5",
			allowedModels: []string{"gpt-5"},
		},
	}, client)
	firstReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	firstReq.Header.Set("Authorization", "Bearer n2api_client_secret")
	firstReq.Header.Set("Content-Type", "application/json")
	firstRecorder := httptest.NewRecorder()
	go func() {
		defer close(firstDone)
		proxy.ServeHTTP(firstRecorder, firstReq)
	}()
	<-firstStarted

	secondReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	secondReq.Header.Set("Authorization", "Bearer n2api_client_secret")
	secondReq.Header.Set("Content-Type", "application/json")
	secondRecorder := httptest.NewRecorder()
	proxy.ServeHTTP(secondRecorder, secondReq)

	close(releaseFirst)
	<-firstDone
	if secondRecorder.Code != http.StatusOK {
		t.Fatalf("second status = %d body=%s, want 200", secondRecorder.Code, secondRecorder.Body.String())
	}
	if tokens.calls != 3 {
		t.Fatalf("account selections = %d, want first request plus override retry selection", tokens.calls)
	}
	if !slices.Equal(tokens.exclusions[2], []int64{1}) {
		t.Fatalf("third selection exclusions = %+v, want busy account 1 excluded", tokens.exclusions[2])
	}
	if secondAuthorization != "Bearer second-token" {
		t.Fatalf("second upstream Authorization = %q, want fallback account token", secondAuthorization)
	}
	if !slices.Equal(tokens.used, []int64{1, 2}) {
		t.Fatalf("used accounts = %+v, want acquired accounts 1 and 2", tokens.used)
	}
}

func TestProxyRejectsWhenAllSelectedAccountsHitConcurrencyLimit(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan struct{})
	var transportCalls int32
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{
		{AccountID: 1, AuthorizationToken: "first-token"},
		{AccountID: 1, AuthorizationToken: "first-token"},
	}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		call := atomic.AddInt32(&transportCalls, 1)
		if call == 1 {
			close(firstStarted)
			<-releaseFirst
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, tokens, Config{
		UpstreamBaseURL:                 "https://upstream.example.test",
		MaxConcurrentRequestsPerAccount: 1,
		ModelProvider: fakeModelProvider{
			defaultModel:  "gpt-5",
			allowedModels: []string{"gpt-5"},
		},
	}, client)
	firstReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	firstReq.Header.Set("Authorization", "Bearer n2api_client_secret")
	firstReq.Header.Set("Content-Type", "application/json")
	firstRecorder := httptest.NewRecorder()
	go func() {
		defer close(firstDone)
		proxy.ServeHTTP(firstRecorder, firstReq)
	}()
	<-firstStarted

	secondReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	secondReq.Header.Set("Authorization", "Bearer n2api_client_secret")
	secondReq.Header.Set("Content-Type", "application/json")
	secondRecorder := httptest.NewRecorder()
	proxy.ServeHTTP(secondRecorder, secondReq)

	close(releaseFirst)
	<-firstDone
	if secondRecorder.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d body=%s, want 429", secondRecorder.Code, secondRecorder.Body.String())
	}
	if !strings.Contains(secondRecorder.Body.String(), "rate_limit_exceeded") {
		t.Fatalf("second body = %q, want rate_limit_exceeded", secondRecorder.Body.String())
	}
	if tokens.calls != 3 {
		t.Fatalf("account selections = %d, want first request plus busy retry and exhausted selection", tokens.calls)
	}
	if !slices.Equal(tokens.exclusions[2], []int64{1}) {
		t.Fatalf("third selection exclusions = %+v, want busy account 1 excluded", tokens.exclusions[2])
	}
	if got := atomic.LoadInt32(&transportCalls); got != 1 {
		t.Fatalf("transport calls = %d, want only first request upstream", got)
	}
	if len(tokens.failures) != 0 {
		t.Fatalf("recorded failures = %+v, want none for local account concurrency", tokens.failures)
	}
	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("first status = %d body=%s, want 200", firstRecorder.Code, firstRecorder.Body.String())
	}
}

func TestAccountConcurrencyLimiterSnapshotIsImmutable(t *testing.T) {
	limiter := newAccountConcurrencyLimiter()
	releaseOne, ok := limiter.Acquire(7, 2)
	if !ok {
		t.Fatal("first acquire returned false")
	}
	defer releaseOne()
	releaseTwo, ok := limiter.Acquire(7, 2)
	if !ok {
		t.Fatal("second acquire returned false")
	}
	defer releaseTwo()

	snapshot := limiter.Snapshot()
	if snapshot[7] != 2 {
		t.Fatalf("snapshot[7] = %d, want 2", snapshot[7])
	}
	snapshot[7] = 99
	if got := limiter.Snapshot()[7]; got != 2 {
		t.Fatalf("mutated snapshot changed limiter count to %d, want 2", got)
	}
}

func TestProxyRejectsWhenAPIKeyConcurrencyLimitIsFull(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan struct{})
	var transportCalls int32
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{
		{AccountID: 1, AuthorizationToken: "first-token"},
		{AccountID: 2, AuthorizationToken: "second-token"},
	}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		call := atomic.AddInt32(&transportCalls, 1)
		if call == 1 {
			close(firstStarted)
			<-releaseFirst
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{key: admin.APIKey{ID: 42, Name: "limited key"}}, tokens, Config{
		UpstreamBaseURL:             "https://upstream.example.test",
		MaxConcurrentRequestsPerKey: 1,
		ModelProvider: fakeModelProvider{
			defaultModel:  "gpt-5",
			allowedModels: []string{"gpt-5"},
		},
	}, client)
	firstReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	firstReq.Header.Set("Authorization", "Bearer n2api_client_secret")
	firstReq.Header.Set("Content-Type", "application/json")
	firstRecorder := httptest.NewRecorder()
	go func() {
		defer close(firstDone)
		proxy.ServeHTTP(firstRecorder, firstReq)
	}()
	<-firstStarted

	secondReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	secondReq.Header.Set("Authorization", "Bearer n2api_client_secret")
	secondReq.Header.Set("Content-Type", "application/json")
	secondRecorder := httptest.NewRecorder()
	proxy.ServeHTTP(secondRecorder, secondReq)

	close(releaseFirst)
	<-firstDone
	if secondRecorder.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d body=%s, want 429", secondRecorder.Code, secondRecorder.Body.String())
	}
	if !strings.Contains(secondRecorder.Body.String(), "rate_limit_exceeded") {
		t.Fatalf("second body = %q, want rate_limit_exceeded", secondRecorder.Body.String())
	}
	if tokens.calls != 1 {
		t.Fatalf("account selections = %d, want only first request selection", tokens.calls)
	}
	if got := atomic.LoadInt32(&transportCalls); got != 1 {
		t.Fatalf("transport calls = %d, want only first request upstream call", got)
	}
	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("first status = %d body=%s, want 200", firstRecorder.Code, firstRecorder.Body.String())
	}
}

func TestProxyAllowsDifferentAPIKeysWhenOneKeyConcurrencyLimitIsFull(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan struct{})
	var transportCalls int32
	var secondAuthorization string
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{
		{AccountID: 1, AuthorizationToken: "first-token"},
		{AccountID: 2, AuthorizationToken: "second-token"},
	}}
	auth := &fakeAPIKeyAuthenticator{keys: map[string]admin.APIKey{
		"first-client-secret":  {ID: 42, Name: "first key"},
		"second-client-secret": {ID: 43, Name: "second key"},
	}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		call := atomic.AddInt32(&transportCalls, 1)
		if call == 1 {
			close(firstStarted)
			<-releaseFirst
		} else {
			secondAuthorization = r.Header.Get("Authorization")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(auth, tokens, Config{
		UpstreamBaseURL:             "https://upstream.example.test",
		MaxConcurrentRequestsPerKey: 1,
		ModelProvider: fakeModelProvider{
			defaultModel:  "gpt-5",
			allowedModels: []string{"gpt-5"},
		},
	}, client)
	firstReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	firstReq.Header.Set("Authorization", "Bearer first-client-secret")
	firstReq.Header.Set("Content-Type", "application/json")
	firstRecorder := httptest.NewRecorder()
	go func() {
		defer close(firstDone)
		proxy.ServeHTTP(firstRecorder, firstReq)
	}()
	<-firstStarted

	secondReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	secondReq.Header.Set("Authorization", "Bearer second-client-secret")
	secondReq.Header.Set("Content-Type", "application/json")
	secondRecorder := httptest.NewRecorder()
	proxy.ServeHTTP(secondRecorder, secondReq)

	close(releaseFirst)
	<-firstDone
	if secondRecorder.Code != http.StatusOK {
		t.Fatalf("second status = %d body=%s, want 200", secondRecorder.Code, secondRecorder.Body.String())
	}
	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("first status = %d body=%s, want 200", firstRecorder.Code, firstRecorder.Body.String())
	}
	if tokens.calls != 2 {
		t.Fatalf("account selections = %d, want both requests selected", tokens.calls)
	}
	if got := atomic.LoadInt32(&transportCalls); got != 2 {
		t.Fatalf("transport calls = %d, want both requests upstream", got)
	}
	if secondAuthorization != "Bearer second-token" {
		t.Fatalf("second upstream Authorization = %q, want second selected account token", secondAuthorization)
	}
}

func TestAPIKeyConcurrencyLimiterSnapshotIsImmutable(t *testing.T) {
	limiter := newAPIKeyConcurrencyLimiter()
	releaseOne, ok := limiter.Acquire(42, 2)
	if !ok {
		t.Fatal("first acquire returned false")
	}
	defer releaseOne()
	releaseTwo, ok := limiter.Acquire(42, 2)
	if !ok {
		t.Fatal("second acquire returned false")
	}
	defer releaseTwo()

	snapshot := limiter.Snapshot()
	if snapshot[42] != 2 {
		t.Fatalf("snapshot[42] = %d, want 2", snapshot[42])
	}
	snapshot[42] = 99
	if got := limiter.Snapshot()[42]; got != 2 {
		t.Fatalf("mutated snapshot changed limiter count to %d, want 2", got)
	}
}

func TestProxyRejectsWhenAPIKeyRequestRateLimitIsExceeded(t *testing.T) {
	var transportCalls int32
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{
		{AccountID: 1, AuthorizationToken: "first-token"},
		{AccountID: 2, AuthorizationToken: "second-token"},
	}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		atomic.AddInt32(&transportCalls, 1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{key: admin.APIKey{ID: 42, Name: "limited key"}}, tokens, Config{
		UpstreamBaseURL:            "https://upstream.example.test",
		MaxRequestsPerMinutePerKey: 1,
		ModelProvider: fakeModelProvider{
			defaultModel:  "gpt-5",
			allowedModels: []string{"gpt-5"},
		},
	}, client)
	proxy.rateLimiter.now = func() time.Time {
		return time.Date(2026, 6, 23, 12, 0, 1, 0, time.UTC)
	}
	firstReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	firstReq.Header.Set("Authorization", "Bearer n2api_client_secret")
	firstReq.Header.Set("Content-Type", "application/json")
	firstRecorder := httptest.NewRecorder()

	proxy.ServeHTTP(firstRecorder, firstReq)

	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("first status = %d body=%s, want 200", firstRecorder.Code, firstRecorder.Body.String())
	}
	secondReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	secondReq.Header.Set("Authorization", "Bearer n2api_client_secret")
	secondReq.Header.Set("Content-Type", "application/json")
	secondRecorder := httptest.NewRecorder()

	proxy.ServeHTTP(secondRecorder, secondReq)

	if secondRecorder.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d body=%s, want 429", secondRecorder.Code, secondRecorder.Body.String())
	}
	if !strings.Contains(secondRecorder.Body.String(), "rate_limit_exceeded") {
		t.Fatalf("second body = %q, want rate_limit_exceeded", secondRecorder.Body.String())
	}
	if got := secondRecorder.Header().Get("Retry-After"); got != "59" {
		t.Fatalf("second Retry-After = %q, want 59", got)
	}
	if tokens.calls != 1 {
		t.Fatalf("account selections = %d, want only first request selection", tokens.calls)
	}
	if got := atomic.LoadInt32(&transportCalls); got != 1 {
		t.Fatalf("transport calls = %d, want only first request upstream call", got)
	}
}

func TestAPIKeyRateLimiterSnapshotReportsActiveWindowOnly(t *testing.T) {
	now := time.Date(2026, 6, 24, 10, 15, 12, 0, time.UTC)
	limiter := newAPIKeyRateLimiter(10, func() time.Time { return now })
	if _, ok := limiter.Allow(42, 0); !ok {
		t.Fatal("first request rejected")
	}
	if _, ok := limiter.Allow(42, 0); !ok {
		t.Fatal("second request rejected")
	}
	limiter.keys[7] = apiKeyRateWindow{
		start: now.Add(-time.Minute).Truncate(time.Minute),
		count: 9,
	}

	snapshot := limiter.Snapshot()

	if snapshot[42] != 2 {
		t.Fatalf("snapshot[42] = %d, want 2", snapshot[42])
	}
	if _, ok := snapshot[7]; ok {
		t.Fatalf("snapshot includes stale key 7: %+v", snapshot)
	}
	snapshot[42] = 99
	if got := limiter.Snapshot()[42]; got != 2 {
		t.Fatalf("mutated snapshot changed limiter count to %d, want 2", got)
	}
}

func TestAPIKeyTokenLimiterSnapshotReportsActiveWindowOnly(t *testing.T) {
	now := time.Date(2026, 6, 24, 10, 15, 12, 0, time.UTC)
	limiter := newAPIKeyTokenLimiter(100, func() time.Time { return now })
	limiter.Record(42, 12, 0)
	limiter.Record(42, 8, 0)
	limiter.keys[7] = apiKeyTokenWindow{
		start:  now.Add(-time.Minute).Truncate(time.Minute),
		tokens: 90,
	}

	snapshot := limiter.Snapshot()

	if snapshot[42] != 20 {
		t.Fatalf("snapshot[42] = %d, want 20", snapshot[42])
	}
	if _, ok := snapshot[7]; ok {
		t.Fatalf("snapshot includes stale key 7: %+v", snapshot)
	}
	snapshot[42] = 99
	if got := limiter.Snapshot()[42]; got != 20 {
		t.Fatalf("mutated snapshot changed limiter count to %d, want 20", got)
	}
}

func TestProxyUsesAPIKeyRequestRateLimitOverride(t *testing.T) {
	var transportCalls int32
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{
		{AccountID: 1, AuthorizationToken: "first-token"},
		{AccountID: 2, AuthorizationToken: "second-token"},
	}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		atomic.AddInt32(&transportCalls, 1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{key: admin.APIKey{
		ID:                42,
		Name:              "limited key",
		RequestsPerMinute: 1,
	}}, tokens, Config{
		UpstreamBaseURL:            "https://upstream.example.test",
		MaxRequestsPerMinutePerKey: 100,
		ModelProvider: fakeModelProvider{
			defaultModel:  "gpt-5",
			allowedModels: []string{"gpt-5"},
		},
	}, client)
	proxy.rateLimiter.now = func() time.Time {
		return time.Date(2026, 6, 23, 12, 0, 1, 0, time.UTC)
	}
	firstReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	firstReq.Header.Set("Authorization", "Bearer n2api_client_secret")
	firstReq.Header.Set("Content-Type", "application/json")
	firstRecorder := httptest.NewRecorder()

	proxy.ServeHTTP(firstRecorder, firstReq)

	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("first status = %d body=%s, want 200", firstRecorder.Code, firstRecorder.Body.String())
	}
	secondReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	secondReq.Header.Set("Authorization", "Bearer n2api_client_secret")
	secondReq.Header.Set("Content-Type", "application/json")
	secondRecorder := httptest.NewRecorder()

	proxy.ServeHTTP(secondRecorder, secondReq)

	if secondRecorder.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d body=%s, want 429", secondRecorder.Code, secondRecorder.Body.String())
	}
	if !strings.Contains(secondRecorder.Body.String(), "rate_limit_exceeded") {
		t.Fatalf("second body = %q, want rate_limit_exceeded", secondRecorder.Body.String())
	}
	if got := secondRecorder.Header().Get("Retry-After"); got != "59" {
		t.Fatalf("second Retry-After = %q, want 59", got)
	}
	if tokens.calls != 1 {
		t.Fatalf("account selections = %d, want only first request selection", tokens.calls)
	}
	if got := atomic.LoadInt32(&transportCalls); got != 1 {
		t.Fatalf("transport calls = %d, want only first request upstream call", got)
	}
}

func TestProxyRejectsWhenAPIKeyTokenRateLimitIsExceeded(t *testing.T) {
	var transportCalls int32
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{
		{AccountID: 1, AuthorizationToken: "first-token"},
		{AccountID: 2, AuthorizationToken: "second-token"},
	}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		atomic.AddInt32(&transportCalls, 1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
				"id":"chatcmpl_123",
				"usage":{"prompt_tokens":8,"completion_tokens":7,"total_tokens":15}
			}`)),
			Request: r,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{key: admin.APIKey{ID: 42, Name: "token limited key"}}, tokens, Config{
		UpstreamBaseURL:          "https://upstream.example.test",
		MaxTokensPerMinutePerKey: 10,
		ModelProvider: fakeModelProvider{
			defaultModel:  "gpt-5",
			allowedModels: []string{"gpt-5"},
		},
	}, client)
	proxy.tokenLimiter.now = func() time.Time {
		return time.Date(2026, 6, 23, 12, 0, 1, 0, time.UTC)
	}
	firstReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	firstReq.Header.Set("Authorization", "Bearer n2api_client_secret")
	firstReq.Header.Set("Content-Type", "application/json")
	firstRecorder := httptest.NewRecorder()

	proxy.ServeHTTP(firstRecorder, firstReq)

	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("first status = %d body=%s, want 200", firstRecorder.Code, firstRecorder.Body.String())
	}
	secondReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	secondReq.Header.Set("Authorization", "Bearer n2api_client_secret")
	secondReq.Header.Set("Content-Type", "application/json")
	secondRecorder := httptest.NewRecorder()

	proxy.ServeHTTP(secondRecorder, secondReq)

	if secondRecorder.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d body=%s, want 429", secondRecorder.Code, secondRecorder.Body.String())
	}
	if !strings.Contains(secondRecorder.Body.String(), "rate_limit_exceeded") {
		t.Fatalf("second body = %q, want rate_limit_exceeded", secondRecorder.Body.String())
	}
	if got := secondRecorder.Header().Get("Retry-After"); got != "59" {
		t.Fatalf("second Retry-After = %q, want 59", got)
	}
	if tokens.calls != 1 {
		t.Fatalf("account selections = %d, want only first request selection", tokens.calls)
	}
	if got := atomic.LoadInt32(&transportCalls); got != 1 {
		t.Fatalf("transport calls = %d, want only first request upstream call", got)
	}
}

func TestProxyUsesAPIKeyTokenRateLimitOverride(t *testing.T) {
	var transportCalls int32
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{
		{AccountID: 1, AuthorizationToken: "first-token"},
		{AccountID: 2, AuthorizationToken: "second-token"},
	}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		atomic.AddInt32(&transportCalls, 1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
				"id":"chatcmpl_123",
				"usage":{"prompt_tokens":8,"completion_tokens":7,"total_tokens":15}
			}`)),
			Request: r,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{key: admin.APIKey{
		ID:              42,
		Name:            "token limited key",
		TokensPerMinute: 10,
	}}, tokens, Config{
		UpstreamBaseURL:          "https://upstream.example.test",
		MaxTokensPerMinutePerKey: 100,
		ModelProvider: fakeModelProvider{
			defaultModel:  "gpt-5",
			allowedModels: []string{"gpt-5"},
		},
	}, client)
	proxy.tokenLimiter.now = func() time.Time {
		return time.Date(2026, 6, 23, 12, 0, 1, 0, time.UTC)
	}
	firstReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	firstReq.Header.Set("Authorization", "Bearer n2api_client_secret")
	firstReq.Header.Set("Content-Type", "application/json")
	firstRecorder := httptest.NewRecorder()

	proxy.ServeHTTP(firstRecorder, firstReq)

	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("first status = %d body=%s, want 200", firstRecorder.Code, firstRecorder.Body.String())
	}
	secondReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	secondReq.Header.Set("Authorization", "Bearer n2api_client_secret")
	secondReq.Header.Set("Content-Type", "application/json")
	secondRecorder := httptest.NewRecorder()

	proxy.ServeHTTP(secondRecorder, secondReq)

	if secondRecorder.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d body=%s, want 429", secondRecorder.Code, secondRecorder.Body.String())
	}
	if !strings.Contains(secondRecorder.Body.String(), "rate_limit_exceeded") {
		t.Fatalf("second body = %q, want rate_limit_exceeded", secondRecorder.Body.String())
	}
	if got := secondRecorder.Header().Get("Retry-After"); got != "59" {
		t.Fatalf("second Retry-After = %q, want 59", got)
	}
	if tokens.calls != 1 {
		t.Fatalf("account selections = %d, want only first request selection", tokens.calls)
	}
	if got := atomic.LoadInt32(&transportCalls); got != 1 {
		t.Fatalf("transport calls = %d, want only first request upstream call", got)
	}
}

func TestProxyModelsBypassesAPIKeyTokenRateLimit(t *testing.T) {
	var transportCalls int32
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "first-token"}}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		atomic.AddInt32(&transportCalls, 1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
				"id":"chatcmpl_123",
				"usage":{"prompt_tokens":8,"completion_tokens":7,"total_tokens":15}
			}`)),
			Request: r,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{key: admin.APIKey{ID: 42, Name: "token limited key"}}, tokens, Config{
		UpstreamBaseURL:          "https://upstream.example.test",
		MaxTokensPerMinutePerKey: 10,
		ModelProvider: fakeModelProvider{
			defaultModel:  "gpt-5",
			allowedModels: []string{"gpt-5"},
			exposedModels: []ExposedModel{{ID: "gpt-5", OwnedBy: "openai"}},
		},
	}, client)
	proxy.tokenLimiter.now = func() time.Time {
		return time.Date(2026, 6, 23, 12, 0, 1, 0, time.UTC)
	}
	firstReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	firstReq.Header.Set("Authorization", "Bearer n2api_client_secret")
	firstReq.Header.Set("Content-Type", "application/json")
	firstRecorder := httptest.NewRecorder()

	proxy.ServeHTTP(firstRecorder, firstReq)

	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("first status = %d body=%s, want 200", firstRecorder.Code, firstRecorder.Body.String())
	}
	modelsReq := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	modelsReq.Header.Set("Authorization", "Bearer n2api_client_secret")
	modelsRecorder := httptest.NewRecorder()

	proxy.ServeHTTP(modelsRecorder, modelsReq)

	if modelsRecorder.Code != http.StatusOK {
		t.Fatalf("models status = %d body=%s, want 200", modelsRecorder.Code, modelsRecorder.Body.String())
	}
	if !strings.Contains(modelsRecorder.Body.String(), `"id":"gpt-5"`) {
		t.Fatalf("models body = %q, want gpt-5", modelsRecorder.Body.String())
	}
	if got := atomic.LoadInt32(&transportCalls); got != 1 {
		t.Fatalf("transport calls = %d, want only the chat request upstream", got)
	}
	if tokens.calls != 1 {
		t.Fatalf("account selections = %d, want only the chat request selection", tokens.calls)
	}
}

func TestProxyRejectsUnlistedModelForSelectedAPIKeyBeforeAccountSelection(t *testing.T) {
	accounts := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "upstream-token"}}}
	auth := &fakeAPIKeyAuthenticator{key: admin.APIKey{
		ID:            42,
		Name:          "selected key",
		ModelPolicy:   admin.APIKeyModelPolicySelected,
		AllowedModels: []string{"gpt-5-mini"},
	}}
	proxy := NewProxy(auth, accounts, Config{
		UpstreamBaseURL: "https://upstream.example.test",
		ModelProvider: fakeModelProvider{
			defaultModel:  "gpt-5-mini",
			allowedModels: []string{"gpt-5", "gpt-5-mini"},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "model_not_found") || !strings.Contains(recorder.Body.String(), "requested model is not available") {
		t.Fatalf("body = %q, want model_not_found with unavailable message", recorder.Body.String())
	}
	if accounts.calls != 0 {
		t.Fatalf("account calls = %d, want 0", accounts.calls)
	}
}

func TestProxyRejectsUnlistedGloballyHiddenModelForSelectedAPIKeyWithoutLeakingGlobalPolicy(t *testing.T) {
	accounts := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "upstream-token"}}}
	auth := &fakeAPIKeyAuthenticator{key: admin.APIKey{
		ID:            42,
		Name:          "selected key",
		ModelPolicy:   admin.APIKeyModelPolicySelected,
		AllowedModels: []string{"gpt-5-mini"},
	}}
	proxy := NewProxy(auth, accounts, Config{
		UpstreamBaseURL: "https://upstream.example.test",
		ModelProvider: fakeModelProvider{
			defaultModel:  "gpt-5-mini",
			allowedModels: []string{"gpt-5-mini"},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"hidden-model","messages":[]}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want selected-key 404; body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "model_not_found") || !strings.Contains(recorder.Body.String(), "requested model is not available") {
		t.Fatalf("body = %q, want model_not_found with unavailable message", recorder.Body.String())
	}
	if accounts.calls != 0 {
		t.Fatalf("account calls = %d, want 0", accounts.calls)
	}
}

func TestProxyRejectsGloballyHiddenModelWithOpenAICompatibleNotFound(t *testing.T) {
	accounts := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "upstream-token"}}}
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, accounts, Config{
		UpstreamBaseURL: "https://upstream.example.test",
		ModelProvider: fakeModelProvider{
			defaultModel:  "gpt-5-mini",
			allowedModels: []string{"gpt-5-mini"},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"hidden-model","messages":[]}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "model_not_found") || !strings.Contains(recorder.Body.String(), "requested model is not available") {
		t.Fatalf("body = %q, want model_not_found with unavailable message", recorder.Body.String())
	}
	if accounts.calls != 0 {
		t.Fatalf("account calls = %d, want 0", accounts.calls)
	}
}

func TestProxyRejectsInjectedDefaultModelWhenSelectedAPIKeyDoesNotAllowIt(t *testing.T) {
	accounts := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "upstream-token"}}}
	auth := &fakeAPIKeyAuthenticator{key: admin.APIKey{
		ID:            42,
		Name:          "selected key",
		ModelPolicy:   admin.APIKeyModelPolicySelected,
		AllowedModels: []string{"gpt-5-mini"},
	}}
	proxy := NewProxy(auth, accounts, Config{
		UpstreamBaseURL: "https://upstream.example.test",
		ModelProvider: fakeModelProvider{
			defaultModel:  "gpt-5",
			allowedModels: []string{"gpt-5", "gpt-5-mini"},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"messages":[]}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want selected-key 404; body=%s", recorder.Code, recorder.Body.String())
	}
	if accounts.calls != 0 {
		t.Fatalf("account calls = %d, want 0", accounts.calls)
	}
}

func TestProxyInjectsDefaultModelWhenMissing(t *testing.T) {
	var gotBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll returned error: %v", err)
		}
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Fatalf("Unmarshal body returned error: %v; body=%s", err, string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"resp_123"}`))
	}))
	defer upstream.Close()
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "upstream-token"}}}
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, tokens, Config{
		UpstreamBaseURL: upstream.URL,
		ModelProvider: fakeModelProvider{
			defaultModel:  "gpt-5",
			allowedModels: []string{"gpt-5"},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"input":"hi"}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if tokens.calls != 1 || len(tokens.models) != 1 || tokens.models[0] != "gpt-5" {
		t.Fatalf("requested models = %+v, calls=%d; want gpt-5", tokens.models, tokens.calls)
	}
	if gotBody["model"] != "gpt-5" {
		t.Fatalf("forwarded model = %#v, want default gpt-5; body=%+v", gotBody["model"], gotBody)
	}
}

func TestProxyRejectsNonStringModel(t *testing.T) {
	upstreamCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalled = true
		t.Fatal("upstream should not be called for invalid model field")
	}))
	defer upstream.Close()
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "upstream-token"}}}
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, tokens, Config{
		UpstreamBaseURL: upstream.URL,
		ModelProvider: fakeModelProvider{
			defaultModel:  "gpt-5",
			allowedModels: []string{"gpt-5"},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":123,"messages":[]}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "invalid_request") {
		t.Fatalf("body = %q, want invalid_request", recorder.Body.String())
	}
	if tokens.calls != 0 {
		t.Fatalf("token calls = %d, want 0", tokens.calls)
	}
	if upstreamCalled {
		t.Fatal("upstream was called")
	}
}

func TestProxyTrimsRequestedModelBeforeRoutingAndForwarding(t *testing.T) {
	var gotBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll returned error: %v", err)
		}
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Fatalf("Unmarshal body returned error: %v; body=%s", err, string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chatcmpl_123"}`))
	}))
	defer upstream.Close()
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "upstream-token"}}}
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, tokens, Config{
		UpstreamBaseURL: upstream.URL,
		ModelProvider: fakeModelProvider{
			defaultModel:  "gpt-5-mini",
			allowedModels: []string{"gpt-5"},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":" gpt-5 ","messages":[]}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if tokens.calls != 1 || len(tokens.models) != 1 || tokens.models[0] != "gpt-5" {
		t.Fatalf("requested models = %+v, calls=%d; want gpt-5", tokens.models, tokens.calls)
	}
	if gotBody["model"] != "gpt-5" {
		t.Fatalf("forwarded model = %#v, want trimmed gpt-5; body=%+v", gotBody["model"], gotBody)
	}
}

func TestProxyReturnsModelUnavailableBeforeUpstream(t *testing.T) {
	upstreamCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalled = true
		t.Fatal("upstream should not be called when no account supports requested model")
	}))
	defer upstream.Close()
	tokens := &fakeSelectedAccountProvider{errs: []error{provider.ErrModelUnavailable}}
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, tokens, Config{
		UpstreamBaseURL: upstream.URL,
		ModelProvider: fakeModelProvider{
			defaultModel:  "gpt-5-mini",
			allowedModels: []string{"gpt-5"},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "model_unavailable") {
		t.Fatalf("body = %q, want model_unavailable", recorder.Body.String())
	}
	if upstreamCalled {
		t.Fatal("upstream was called")
	}
	if tokens.calls != 1 || len(tokens.models) != 1 || tokens.models[0] != "gpt-5" {
		t.Fatalf("requested models = %+v, calls=%d; want gpt-5", tokens.models, tokens.calls)
	}
}

func TestProxyStreamsChatCompletionsResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %q, want /v1/chat/completions", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer upstream-token" {
			t.Fatalf("upstream Authorization = %q", r.Header.Get("Authorization"))
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll returned error: %v", err)
		}
		if !strings.Contains(string(body), `"stream":true`) {
			t.Fatalf("body = %q, want stream request body", string(body))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"delta\":\"hi\"}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "upstream-token"}}}, Config{
		UpstreamBaseURL: upstream.URL,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[],"stream":true}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	if recorder.Body.String() != "data: {\"delta\":\"hi\"}\n\ndata: [DONE]\n\n" {
		t.Fatalf("stream body = %q", recorder.Body.String())
	}
}

func TestProxyForwardsResponsesCreateWithStreaming(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("path = %q, want /v1/responses", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %q, want POST", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer upstream-token" {
			t.Fatalf("upstream Authorization = %q", r.Header.Get("Authorization"))
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll returned error: %v", err)
		}
		if !strings.Contains(string(body), `"stream":true`) {
			t.Fatalf("body = %q, want stream request body", string(body))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\"}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "upstream-token"}}}, Config{
		UpstreamBaseURL: upstream.URL,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5","input":"hi","stream":true}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	if recorder.Body.String() != "data: {\"type\":\"response.output_text.delta\"}\n\ndata: [DONE]\n\n" {
		t.Fatalf("stream body = %q", recorder.Body.String())
	}
}

func TestProxyRoutesAPIUpstreamAccountToConfiguredBaseURLAndToken(t *testing.T) {
	defaultUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("default upstream should not be called for API upstream account")
	}))
	defer defaultUpstream.Close()
	var gotPath string
	var gotAuthorization string
	var gotChatGPTAccountID string
	var gotOpenAIBeta string
	apiUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuthorization = r.Header.Get("Authorization")
		gotChatGPTAccountID = r.Header.Get("chatgpt-account-id")
		gotOpenAIBeta = r.Header.Get("OpenAI-Beta")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"resp_123"}`))
	}))
	defer apiUpstream.Close()
	accounts := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{
		AccountID:          9,
		AccountType:        provider.AccountTypeAPIUpstream,
		AuthorizationToken: "sk-upstream",
		BaseURL:            apiUpstream.URL,
	}}}
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, accounts, Config{
		UpstreamBaseURL: defaultUpstream.URL,
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if gotPath != "/v1/responses/resp_123" {
		t.Fatalf("path = %q, want API upstream responses path", gotPath)
	}
	if gotAuthorization != "Bearer sk-upstream" {
		t.Fatalf("Authorization = %q, want upstream API key", gotAuthorization)
	}
	if gotChatGPTAccountID != "" || gotOpenAIBeta != "" {
		t.Fatalf("codex headers = chatgpt:%q beta:%q, want none", gotChatGPTAccountID, gotOpenAIBeta)
	}
	if accounts.calls != 1 {
		t.Fatalf("account calls = %d, want 1", accounts.calls)
	}
}

func TestProxyDoesNotDuplicateV1ForAPIUpstreamBaseURL(t *testing.T) {
	defaultUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("default upstream should not be called for API upstream account")
	}))
	defer defaultUpstream.Close()
	var gotPath string
	apiUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"resp_123"}`))
	}))
	defer apiUpstream.Close()
	accounts := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{
		AccountID:          9,
		AccountType:        provider.AccountTypeAPIUpstream,
		AuthorizationToken: "sk-upstream",
		BaseURL:            apiUpstream.URL + "/v1",
	}}}
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, accounts, Config{
		UpstreamBaseURL: defaultUpstream.URL,
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if gotPath != "/v1/responses/resp_123" {
		t.Fatalf("path = %q, want single /v1 responses path", gotPath)
	}
}

func TestProxyRoutesAPIUpstreamResponsesCreateWithoutCodexTransform(t *testing.T) {
	defaultUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("default upstream should not be called for API upstream responses create")
	}))
	defer defaultUpstream.Close()
	var gotPath string
	var gotAuthorization string
	var gotChatGPTAccountID string
	var gotOpenAIBeta string
	var gotOriginator string
	var gotBody map[string]any
	apiUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuthorization = r.Header.Get("Authorization")
		gotChatGPTAccountID = r.Header.Get("chatgpt-account-id")
		gotOpenAIBeta = r.Header.Get("OpenAI-Beta")
		gotOriginator = r.Header.Get("originator")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll returned error: %v", err)
		}
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Fatalf("Unmarshal body returned error: %v; body=%s", err, string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"resp_api"}`))
	}))
	defer apiUpstream.Close()
	accounts := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{
		AccountID:          9,
		AccountType:        provider.AccountTypeAPIUpstream,
		AuthorizationToken: "sk-upstream",
		BaseURL:            apiUpstream.URL,
	}}}
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, accounts, Config{
		UpstreamBaseURL:       defaultUpstream.URL,
		CodexResponsesBaseURL: "http://codex.invalid",
		ModelProvider:         fakeModelProvider{defaultModel: "gpt-5", allowedModels: []string{"gpt-5"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5","input":"hi","stream":false}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if gotPath != "/v1/responses" {
		t.Fatalf("path = %q, want API upstream responses path", gotPath)
	}
	if gotAuthorization != "Bearer sk-upstream" {
		t.Fatalf("Authorization = %q, want upstream API key", gotAuthorization)
	}
	if gotChatGPTAccountID != "" || gotOpenAIBeta != "" || gotOriginator != "" {
		t.Fatalf("codex headers = chatgpt:%q beta:%q originator:%q, want none", gotChatGPTAccountID, gotOpenAIBeta, gotOriginator)
	}
	if gotBody["stream"] != false || gotBody["store"] != nil {
		t.Fatalf("body = %+v, want original API upstream body without codex normalization", gotBody)
	}
}

func TestProxyForwardsOAuthResponsesCreateToCodexEndpoint(t *testing.T) {
	var gotPath string
	var gotAuthorization string
	var gotChatGPTAccountID string
	var gotOpenAIBeta string
	var gotOriginator string
	var gotUserAgent string
	var gotHost string
	var gotBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuthorization = r.Header.Get("Authorization")
		gotChatGPTAccountID = r.Header.Get("chatgpt-account-id")
		gotOpenAIBeta = r.Header.Get("OpenAI-Beta")
		gotOriginator = r.Header.Get("originator")
		gotUserAgent = r.Header.Get("User-Agent")
		gotHost = r.Host
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll returned error: %v", err)
		}
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Fatalf("Unmarshal body returned error: %v; body=%s", err, string(body))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\"}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, &fakeSelectedAccountProvider{accounts: []SelectedAccount{{
		AccountID:          1,
		AccountType:        provider.AccountTypeCodexOAuth,
		AuthorizationToken: "upstream-token",
		ChatGPTAccountID:   "acct_chatgpt",
	}}}, Config{
		UpstreamBaseURL:       "https://api.example.test",
		CodexResponsesBaseURL: upstream.URL + "/backend-api/codex",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4-mini","input":"hi"}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if gotPath != "/backend-api/codex/responses" {
		t.Fatalf("path = %q, want Codex responses endpoint", gotPath)
	}
	if gotAuthorization != "Bearer upstream-token" {
		t.Fatalf("Authorization = %q", gotAuthorization)
	}
	if gotChatGPTAccountID != "acct_chatgpt" {
		t.Fatalf("chatgpt-account-id = %q", gotChatGPTAccountID)
	}
	if gotOpenAIBeta != "responses=experimental" {
		t.Fatalf("OpenAI-Beta = %q", gotOpenAIBeta)
	}
	if gotOriginator != "codex_cli_rs" {
		t.Fatalf("originator = %q", gotOriginator)
	}
	if !strings.HasPrefix(gotUserAgent, "codex_cli_rs/") {
		t.Fatalf("User-Agent = %q, want codex_cli_rs prefix", gotUserAgent)
	}
	if gotHost != strings.TrimPrefix(upstream.URL, "http://") {
		t.Fatalf("Host = %q, want configured upstream host", gotHost)
	}
	if gotBody["stream"] != true {
		t.Fatalf("stream = %#v, want true", gotBody["stream"])
	}
	if gotBody["store"] != false {
		t.Fatalf("store = %#v, want false", gotBody["store"])
	}
	if strings.TrimSpace(gotBody["instructions"].(string)) == "" {
		t.Fatalf("instructions = %#v, want non-empty string", gotBody["instructions"])
	}
	input, ok := gotBody["input"].([]any)
	if !ok || len(input) != 1 {
		t.Fatalf("input = %#v, want one message", gotBody["input"])
	}
	message, ok := input[0].(map[string]any)
	if !ok || message["type"] != "message" || message["role"] != "user" || message["content"] != "hi" {
		t.Fatalf("input message = %#v", input[0])
	}
}

func TestProxyForwardsResponsesRetrieveAndInputItems(t *testing.T) {
	var gotPaths []string
	var gotQueries []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		gotQueries = append(gotQueries, r.URL.RawQuery)
		if r.Header.Get("Authorization") != "Bearer upstream-token" {
			t.Fatalf("upstream Authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"resp_123"}`))
	}))
	defer upstream.Close()
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, &fakeSelectedAccountProvider{accounts: []SelectedAccount{
		{AccountID: 1, AuthorizationToken: "upstream-token"},
		{AccountID: 1, AuthorizationToken: "upstream-token"},
	}}, Config{
		UpstreamBaseURL: upstream.URL,
	})

	for _, path := range []string{
		"/v1/responses/resp_123?include[]=message.output_text.logprobs",
		"/v1/responses/resp_123/input_items?limit=20",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer n2api_client_secret")
		recorder := httptest.NewRecorder()

		proxy.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200; body=%s", path, recorder.Code, recorder.Body.String())
		}
	}

	if strings.Join(gotPaths, ",") != "/v1/responses/resp_123,/v1/responses/resp_123/input_items" {
		t.Fatalf("paths = %+v", gotPaths)
	}
	if gotQueries[0] != "include[]=message.output_text.logprobs" || gotQueries[1] != "limit=20" {
		t.Fatalf("queries = %+v", gotQueries)
	}
}

func TestProxyReportsMissingProviderAccount(t *testing.T) {
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, &fakeSelectedAccountProvider{errs: []error{provider.ErrNotConnected}}, Config{
		UpstreamBaseURL: "https://api.example.test",
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "provider_not_connected") {
		t.Fatalf("body = %q, want provider_not_connected", recorder.Body.String())
	}
}

func TestProxyLogsAuthenticatedRequests(t *testing.T) {
	logger := &fakeRequestLogger{}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"object":"list","data":[]}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(
		&fakeAPIKeyAuthenticator{},
		&fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 7, Provider: "openrouter", AccountType: provider.AccountTypeAPIUpstream, DisplayName: "Upstream A", AuthorizationToken: "upstream-token"}}},
		Config{UpstreamBaseURL: "https://upstream.example.test", Logger: logger},
		client,
	)
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if len(logger.entries) != 1 {
		t.Fatalf("logged entries = %d, want 1", len(logger.entries))
	}
	entry := logger.entries[0]
	if entry.RequestID == "" {
		t.Fatal("RequestID is empty")
	}
	if entry.ClientKeyID != 42 || entry.Provider != "openrouter" || entry.Route != "/v1/responses/resp_123" || entry.Method != http.MethodGet {
		t.Fatalf("log entry = %+v", entry)
	}
	if entry.ProviderAccountID != 7 || entry.ProviderAccountType != provider.AccountTypeAPIUpstream {
		t.Fatalf("log account attribution = id:%d type:%q, want 7/%s", entry.ProviderAccountID, entry.ProviderAccountType, provider.AccountTypeAPIUpstream)
	}
	if entry.ProviderAccountName != "Upstream A" {
		t.Fatalf("log account name = %q, want snapshot name", entry.ProviderAccountName)
	}
	if entry.StatusCode != http.StatusOK || entry.Error != "" {
		t.Fatalf("log status/error = %d/%q, want 200/empty", entry.StatusCode, entry.Error)
	}
	if entry.Latency < 0 || entry.CreatedAt.After(time.Now().Add(time.Second)) {
		t.Fatalf("log timing = latency:%s created:%s", entry.Latency, entry.CreatedAt)
	}
}

func TestProxyRoutesPoolBoundAPIKeyThroughRoutingPool(t *testing.T) {
	logger := &fakeRequestLogger{}
	accounts := &fakeSelectedAccountProvider{
		accounts: []SelectedAccount{{
			AccountID:          9,
			AccountType:        provider.AccountTypeAPIUpstream,
			DisplayName:        "Pool Upstream",
			AuthorizationToken: "pool-token",
			RoutingPoolID:      7,
			RoutingPoolName:    "primary",
		}},
	}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})}
	poolID := int64(7)
	proxy := NewProxyWithClient(
		&fakeAPIKeyAuthenticator{key: admin.APIKey{ID: 42, Name: "pool key", RoutingPoolID: &poolID, RoutingPoolName: "primary"}},
		accounts,
		Config{UpstreamBaseURL: "https://upstream.example.test", Logger: logger},
		client,
	)
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if !slices.Equal(accounts.chainPoolIDs, []int64{7}) {
		t.Fatalf("routing pool chain calls = %+v, want pool 7", accounts.chainPoolIDs)
	}
	if len(logger.entries) != 1 {
		t.Fatalf("logged entries = %d, want 1", len(logger.entries))
	}
	if logger.entries[0].RoutingPoolID != 7 || logger.entries[0].RoutingPoolName != "primary" {
		t.Fatalf("logged pool = %d/%q, want 7/primary", logger.entries[0].RoutingPoolID, logger.entries[0].RoutingPoolName)
	}
}

func TestProxyRoutesPoolBoundKeyThroughFallbackPool(t *testing.T) {
	logger := &fakeRequestLogger{}
	poolID := int64(1)
	accounts := &fakeSelectedAccountProvider{
		accounts: []SelectedAccount{{
			AccountID:                20,
			AccountType:              provider.AccountTypeAPIUpstream,
			DisplayName:              "Fallback Account",
			AuthorizationToken:       "fallback-token",
			RoutingPoolID:            2,
			RoutingPoolName:          "secondary",
			RoutingPoolFallbackDepth: 1,
			RoutingPoolFallbackChain: "primary -> secondary",
		}},
	}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("Authorization"); got != "Bearer fallback-token" {
			t.Fatalf("Authorization = %q, want fallback token", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(
		&fakeAPIKeyAuthenticator{key: admin.APIKey{ID: 42, Name: "pool key", RoutingPoolID: &poolID, RoutingPoolName: "primary"}},
		accounts,
		Config{UpstreamBaseURL: "https://upstream.example.test", Logger: logger},
		client,
	)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5","input":"hi"}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if !slices.Equal(accounts.chainPoolIDs, []int64{1}) {
		t.Fatalf("routing pool chain calls = %+v, want primary pool 1", accounts.chainPoolIDs)
	}
	if len(logger.entries) != 1 || logger.entries[0].RoutingPoolID != 2 || logger.entries[0].RoutingPoolFallbackDepth != 1 {
		t.Fatalf("log entry = %+v, want fallback pool diagnostics", logger.entries)
	}
	if logger.entries[0].RoutingPoolFallbackChain != "primary -> secondary" {
		t.Fatalf("fallback chain = %q, want primary -> secondary", logger.entries[0].RoutingPoolFallbackChain)
	}
}

func TestProxyReturnsRoutingPoolUnavailableForMissingPool(t *testing.T) {
	logger := &fakeRequestLogger{}
	poolID := int64(7)
	proxy := NewProxyWithClient(
		&fakeAPIKeyAuthenticator{key: admin.APIKey{ID: 42, Name: "pool key", RoutingPoolID: &poolID, RoutingPoolName: "primary"}},
		&fakeSelectedAccountProvider{
			accounts: []SelectedAccount{{
				RoutingPoolError: provider.RoutingPoolErrorUnavailable,
			}},
			errs: []error{provider.ErrRoutingPoolNotFound},
		},
		Config{UpstreamBaseURL: "https://upstream.example.test", Logger: logger},
		&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			t.Fatal("upstream should not be called for unavailable routing pool")
			return nil, nil
		})},
	)
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s, want 503", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "routing_pool_unavailable") {
		t.Fatalf("body = %s, want routing_pool_unavailable", recorder.Body.String())
	}
	if len(logger.entries) != 1 || logger.entries[0].RoutingPoolID != 7 || logger.entries[0].Error != "routing_pool_unavailable" || logger.entries[0].RoutingPoolError != provider.RoutingPoolErrorUnavailable {
		t.Fatalf("logged entry = %+v, want pool attribution and routing_pool_unavailable", logger.entries)
	}
}

func TestProxyLogsRoutingPoolExhaustedDiagnosticsOnSelectionError(t *testing.T) {
	logger := &fakeRequestLogger{}
	poolID := int64(1)
	proxy := NewProxyWithClient(
		&fakeAPIKeyAuthenticator{key: admin.APIKey{ID: 42, Name: "pool key", RoutingPoolID: &poolID, RoutingPoolName: "primary"}},
		&fakeSelectedAccountProvider{
			accounts: []SelectedAccount{{
				RoutingPoolID:            1,
				RoutingPoolName:          "primary",
				RoutingPoolFallbackChain: "primary -> secondary",
				RoutingPoolError:         provider.RoutingPoolErrorExhausted,
			}},
			errs: []error{provider.ErrModelUnavailable},
		},
		Config{UpstreamBaseURL: "https://upstream.example.test", Logger: logger},
		&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			t.Fatal("upstream should not be called when the routing pool chain is exhausted")
			return nil, nil
		})},
	)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5","input":"hi"}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s, want 503", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "routing_pool_exhausted") {
		t.Fatalf("body = %s, want routing_pool_exhausted", recorder.Body.String())
	}
	if len(logger.entries) != 1 {
		t.Fatalf("logged entries = %d, want 1", len(logger.entries))
	}
	entry := logger.entries[0]
	if entry.RoutingPoolID != 1 || entry.RoutingPoolFallbackChain != "primary -> secondary" || entry.RoutingPoolError != provider.RoutingPoolErrorExhausted || entry.Error != provider.RoutingPoolErrorExhausted {
		t.Fatalf("routing pool diagnostics = %+v, want exhausted chain", entry)
	}
}

func TestProxyLogsRoutingPoolCycleDiagnosticsOnSelectionError(t *testing.T) {
	logger := &fakeRequestLogger{}
	poolID := int64(1)
	proxy := NewProxyWithClient(
		&fakeAPIKeyAuthenticator{key: admin.APIKey{ID: 42, Name: "pool key", RoutingPoolID: &poolID, RoutingPoolName: "primary"}},
		&fakeSelectedAccountProvider{
			accounts: []SelectedAccount{{
				RoutingPoolID:    1,
				RoutingPoolName:  "primary",
				RoutingPoolError: provider.RoutingPoolErrorCycle,
			}},
			errs: []error{provider.ErrRoutingPoolCycle},
		},
		Config{UpstreamBaseURL: "https://upstream.example.test", Logger: logger},
		&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			t.Fatal("upstream should not be called when the routing pool chain has a cycle")
			return nil, nil
		})},
	)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5","input":"hi"}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s, want 503", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "routing_pool_cycle") {
		t.Fatalf("body = %s, want routing_pool_cycle", recorder.Body.String())
	}
	if len(logger.entries) != 1 || logger.entries[0].RoutingPoolError != provider.RoutingPoolErrorCycle {
		t.Fatalf("logged entry = %+v, want routing_pool_cycle diagnostics", logger.entries)
	}
}

func TestProxyLogsRoutingPoolDisabledDiagnosticsOnSelectionError(t *testing.T) {
	logger := &fakeRequestLogger{}
	poolID := int64(1)
	proxy := NewProxyWithClient(
		&fakeAPIKeyAuthenticator{key: admin.APIKey{ID: 42, Name: "pool key", RoutingPoolID: &poolID, RoutingPoolName: "primary"}},
		&fakeSelectedAccountProvider{
			accounts: []SelectedAccount{{
				RoutingPoolID:    1,
				RoutingPoolName:  "primary",
				RoutingPoolError: provider.RoutingPoolErrorDisabled,
			}},
			errs: []error{provider.ErrAccountsDisabled},
		},
		Config{UpstreamBaseURL: "https://upstream.example.test", Logger: logger},
		&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			t.Fatal("upstream should not be called when the primary routing pool is disabled")
			return nil, nil
		})},
	)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5","input":"hi"}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s, want 503", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "routing_pool_disabled") {
		t.Fatalf("body = %s, want routing_pool_disabled", recorder.Body.String())
	}
	if len(logger.entries) != 1 || logger.entries[0].RoutingPoolError != provider.RoutingPoolErrorDisabled || logger.entries[0].Error != provider.RoutingPoolErrorDisabled {
		t.Fatalf("logged entry = %+v, want routing_pool_disabled diagnostics", logger.entries)
	}
}

func TestProviderErrorCodeForSelectionKeepsGlobalDisabledDistinctFromRoutingPoolDisabled(t *testing.T) {
	if got := providerErrorCodeForSelection(provider.ErrAccountsDisabled, SelectedAccount{}); got != "provider_accounts_disabled" {
		t.Fatalf("global disabled error code = %q, want provider_accounts_disabled", got)
	}
	selected := SelectedAccount{RoutingPoolError: provider.RoutingPoolErrorDisabled}
	if got := providerErrorCodeForSelection(provider.ErrAccountsDisabled, selected); got != provider.RoutingPoolErrorDisabled {
		t.Fatalf("routing pool disabled error code = %q, want %s", got, provider.RoutingPoolErrorDisabled)
	}
}

func TestProviderErrorCodeForSelectionKeepsModelUnavailableDistinctFromRoutingPoolExhausted(t *testing.T) {
	if got := providerErrorCodeForSelection(provider.ErrModelUnavailable, SelectedAccount{}); got != "model_unavailable" {
		t.Fatalf("model unavailable error code = %q, want model_unavailable", got)
	}
	selected := SelectedAccount{RoutingPoolError: provider.RoutingPoolErrorExhausted}
	if got := providerErrorCodeForSelection(provider.ErrModelUnavailable, selected); got != provider.RoutingPoolErrorExhausted {
		t.Fatalf("routing pool exhausted error code = %q, want %s", got, provider.RoutingPoolErrorExhausted)
	}
	if got := providerErrorCodeForSelection(provider.ErrRoutingPoolExhausted, SelectedAccount{}); got != provider.RoutingPoolErrorExhausted {
		t.Fatalf("routing pool exhausted provider error code = %q, want %s", got, provider.RoutingPoolErrorExhausted)
	}
}

func TestProxyLogsRoutingPoolEmptyDiagnosticsOnSelectionError(t *testing.T) {
	logger := &fakeRequestLogger{}
	poolID := int64(1)
	proxy := NewProxyWithClient(
		&fakeAPIKeyAuthenticator{key: admin.APIKey{ID: 42, Name: "pool key", RoutingPoolID: &poolID, RoutingPoolName: "primary"}},
		&fakeSelectedAccountProvider{
			accounts: []SelectedAccount{{
				RoutingPoolID:    1,
				RoutingPoolName:  "primary",
				RoutingPoolError: provider.RoutingPoolErrorEmpty,
			}},
			errs: []error{provider.ErrRoutingPoolEmpty},
		},
		Config{UpstreamBaseURL: "https://upstream.example.test", Logger: logger},
		&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			t.Fatal("upstream should not be called when the primary routing pool is empty")
			return nil, nil
		})},
	)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5","input":"hi"}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s, want 503", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "routing_pool_empty") {
		t.Fatalf("body = %s, want routing_pool_empty", recorder.Body.String())
	}
	if len(logger.entries) != 1 || logger.entries[0].RoutingPoolError != provider.RoutingPoolErrorEmpty || logger.entries[0].Error != "routing_pool_empty" {
		t.Fatalf("logged entry = %+v, want routing_pool_empty diagnostics", logger.entries)
	}
}

func TestProxyLogsRequestModel(t *testing.T) {
	logger := &fakeRequestLogger{}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(
		&fakeAPIKeyAuthenticator{},
		&fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 7, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "upstream-token"}}},
		Config{UpstreamBaseURL: "https://upstream.example.test", Logger: logger},
		client,
	)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":" gpt-5 ","messages":[]}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if len(logger.entries) != 1 {
		t.Fatalf("logged entries = %d, want 1", len(logger.entries))
	}
	if logger.entries[0].Model != "gpt-5" {
		t.Fatalf("logged model = %q, want gpt-5", logger.entries[0].Model)
	}
}

func TestProxyLogsStickySessionID(t *testing.T) {
	logger := &fakeRequestLogger{}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(
		&fakeAPIKeyAuthenticator{},
		&fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 7, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "upstream-token"}}},
		Config{UpstreamBaseURL: "https://upstream.example.test", Logger: logger},
		client,
	)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","session_id":" workspace-123 ","messages":[]}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if len(logger.entries) != 1 {
		t.Fatalf("logged entries = %d, want 1", len(logger.entries))
	}
	if logger.entries[0].SessionID != "workspace-123" {
		t.Fatalf("logged session ID = %q, want workspace-123", logger.entries[0].SessionID)
	}
}

func TestProxyLogsNonStreamingUsage(t *testing.T) {
	logger := &fakeRequestLogger{}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
				"model":"gpt-5",
				"usage":{
					"prompt_tokens":20,
					"completion_tokens":5,
					"total_tokens":25,
					"prompt_tokens_details":{"cached_tokens":4},
					"completion_tokens_details":{"reasoning_tokens":2}
				}
			}`)),
			Request: r,
		}, nil
	})}
	proxy := NewProxyWithClient(
		&fakeAPIKeyAuthenticator{},
		&fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 7, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "upstream-token"}}},
		Config{UpstreamBaseURL: "https://upstream.example.test", Logger: logger},
		client,
	)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"usage"`) {
		t.Fatalf("body = %s, want passthrough usage payload", recorder.Body.String())
	}
	if len(logger.entries) != 1 {
		t.Fatalf("logged entries = %d, want 1", len(logger.entries))
	}
	entry := logger.entries[0]
	if entry.UsageSource != "chat_completions" || entry.InputTokens != 20 || entry.OutputTokens != 5 || entry.TotalTokens != 25 || entry.CachedInputTokens != 4 || entry.ReasoningTokens != 2 {
		t.Fatalf("logged usage = %+v, want parsed non-streaming usage", entry)
	}
}

func TestProxyLogsStreamingUsage(t *testing.T) {
	logger := &fakeRequestLogger{}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader("event: response.completed\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-5\",\"usage\":{\"input_tokens\":3,\"output_tokens\":4,\"total_tokens\":7}}}\n\n")),
			Request: r,
		}, nil
	})}
	proxy := NewProxyWithClient(
		&fakeAPIKeyAuthenticator{},
		&fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 7, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "upstream-token"}}},
		Config{UpstreamBaseURL: "https://upstream.example.test", Logger: logger},
		client,
	)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5","input":"hello","stream":true}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "response.completed") {
		t.Fatalf("body = %s, want passthrough SSE event", recorder.Body.String())
	}
	if len(logger.entries) != 1 {
		t.Fatalf("logged entries = %d, want 1", len(logger.entries))
	}
	entry := logger.entries[0]
	if entry.UsageSource != "stream" || entry.InputTokens != 3 || entry.OutputTokens != 4 || entry.TotalTokens != 7 {
		t.Fatalf("logged usage = %+v, want parsed streaming usage", entry)
	}
}

func TestProxyLogsEstimatedUsageCost(t *testing.T) {
	logger := &fakeRequestLogger{}
	pricer := &fakeUsagePricer{estimate: UsageCostEstimate{
		Matched:      true,
		CostMicrousd: 1234,
		Snapshot:     map[string]any{"matched": true, "model": "gpt-5"},
	}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
				"model":"gpt-5",
				"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}
			}`)),
			Request: r,
		}, nil
	})}
	proxy := NewProxyWithClient(
		&fakeAPIKeyAuthenticator{},
		&fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 7, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "upstream-token"}}},
		Config{UpstreamBaseURL: "https://upstream.example.test", Logger: logger, UsagePricer: pricer},
		client,
	)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5","input":"hello"}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if len(logger.entries) != 1 {
		t.Fatalf("logged entries = %d, want 1", len(logger.entries))
	}
	entry := logger.entries[0]
	if pricer.usage.Model != "gpt-5" || pricer.usage.InputTokens != 10 || pricer.usage.OutputTokens != 5 || pricer.usage.TotalTokens != 15 {
		t.Fatalf("priced usage = %+v, want parsed response usage", pricer.usage)
	}
	if entry.EstimatedCostMicrousd != 1234 || entry.PricingSnapshot["matched"] != true || entry.PricingSnapshot["model"] != "gpt-5" {
		t.Fatalf("entry cost/snapshot = %d/%+v, want priced log entry", entry.EstimatedCostMicrousd, entry.PricingSnapshot)
	}
}

func TestProxyLogsProviderErrorsAfterAuthentication(t *testing.T) {
	logger := &fakeRequestLogger{}
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, &fakeSelectedAccountProvider{errs: []error{provider.ErrNotConnected}}, Config{
		UpstreamBaseURL: "https://api.example.test",
		Logger:          logger,
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if len(logger.entries) != 1 {
		t.Fatalf("logged entries = %d, want 1", len(logger.entries))
	}
	entry := logger.entries[0]
	if entry.StatusCode != http.StatusServiceUnavailable || entry.Error != "provider_not_connected" {
		t.Fatalf("log entry = %+v, want provider_not_connected 503", entry)
	}
}

func TestProxyLogsFinalRetryableUpstreamStatusAsError(t *testing.T) {
	logger := &fakeRequestLogger{}
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{
		{AccountID: 7, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "rate-limited-token"},
		{AccountID: 8, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "rate-limited-token"},
		{AccountID: 9, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "rate-limited-token"},
		{AccountID: 10, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "rate-limited-token"},
		{AccountID: 11, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "rate-limited-token"},
	}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{"Content-Type": []string{"application/json"}, "Retry-After": []string{"60"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"upstream rate limited"}}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, tokens, Config{
		UpstreamBaseURL: "https://upstream.example.test",
		Logger:          logger,
	}, client)
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d body=%s, want upstream 429", recorder.Code, recorder.Body.String())
	}
	if len(logger.entries) != 1 {
		t.Fatalf("logged entries = %d, want 1", len(logger.entries))
	}
	entry := logger.entries[0]
	if entry.StatusCode != http.StatusTooManyRequests || entry.Error != "upstream_rate_limited" {
		t.Fatalf("log status/error = %d/%q, want 429/upstream_rate_limited", entry.StatusCode, entry.Error)
	}
	if entry.ProviderAccountID != 11 {
		t.Fatalf("logged account = %d, want final attempted account 11", entry.ProviderAccountID)
	}
}

func TestProxyLogsPreciseAPIKeyRequestRateLimitReason(t *testing.T) {
	logger := &fakeRequestLogger{}
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{
		{AccountID: 1, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "first-token"},
		{AccountID: 2, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "second-token"},
	}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, tokens, Config{
		UpstreamBaseURL:            "https://upstream.example.test",
		MaxRequestsPerMinutePerKey: 1,
		Logger:                     logger,
	}, client)
	proxy.rateLimiter.now = func() time.Time {
		return time.Date(2026, 6, 24, 12, 0, 1, 0, time.UTC)
	}

	firstReq := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	firstReq.Header.Set("Authorization", "Bearer n2api_client_secret")
	proxy.ServeHTTP(httptest.NewRecorder(), firstReq)
	secondReq := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	secondReq.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, secondReq)

	if recorder.Code != http.StatusTooManyRequests || !strings.Contains(recorder.Body.String(), "rate_limit_exceeded") {
		t.Fatalf("status/body = %d/%s, want 429 rate_limit_exceeded", recorder.Code, recorder.Body.String())
	}
	assertLastLoggedError(t, logger, "api_key_request_rate_limited")
}

func TestProxyLogsPreciseAPIKeyTokenRateLimitReason(t *testing.T) {
	logger := &fakeRequestLogger{}
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{
		{AccountID: 1, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "first-token"},
		{AccountID: 2, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "second-token"},
	}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"usage":{"prompt_tokens":8,"completion_tokens":7,"total_tokens":15}}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, tokens, Config{
		UpstreamBaseURL:          "https://upstream.example.test",
		MaxTokensPerMinutePerKey: 10,
		Logger:                   logger,
		ModelProvider: fakeModelProvider{
			defaultModel:  "gpt-5",
			allowedModels: []string{"gpt-5"},
		},
	}, client)
	proxy.tokenLimiter.now = func() time.Time {
		return time.Date(2026, 6, 24, 12, 0, 1, 0, time.UTC)
	}

	firstReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	firstReq.Header.Set("Authorization", "Bearer n2api_client_secret")
	firstReq.Header.Set("Content-Type", "application/json")
	proxy.ServeHTTP(httptest.NewRecorder(), firstReq)
	secondReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	secondReq.Header.Set("Authorization", "Bearer n2api_client_secret")
	secondReq.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, secondReq)

	if recorder.Code != http.StatusTooManyRequests || !strings.Contains(recorder.Body.String(), "rate_limit_exceeded") {
		t.Fatalf("status/body = %d/%s, want 429 rate_limit_exceeded", recorder.Code, recorder.Body.String())
	}
	assertLastLoggedError(t, logger, "api_key_token_rate_limited")
}

func TestProxyRejectsWhenAPIKeyBudgetIsExceeded(t *testing.T) {
	logger := &fakeRequestLogger{}
	budgets := &fakeBudgetProvider{usage: admin.APIKeyBudgetUsage{RequestBudgetExceeded: true}}
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "upstream-token"}}}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{key: admin.APIKey{ID: 42, Name: "budgeted key", RequestBudget24h: 1}}, tokens, Config{
		UpstreamBaseURL: "https://upstream.example.test",
		Logger:          logger,
		BudgetProvider:  budgets,
	}, http.DefaultClient)
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusTooManyRequests || !strings.Contains(recorder.Body.String(), "rate_limit_exceeded") {
		t.Fatalf("status/body = %d/%s, want 429 rate_limit_exceeded", recorder.Code, recorder.Body.String())
	}
	if budgets.calls != 1 || budgets.key.ID != 42 {
		t.Fatalf("budget calls/key = %d/%+v, want one call for key 42", budgets.calls, budgets.key)
	}
	if tokens.calls != 0 {
		t.Fatalf("account calls = %d, want 0 when key budget is exceeded", tokens.calls)
	}
	assertLastLoggedError(t, logger, "api_key_request_budget_exceeded")
}

func TestProxyChecksAPIKeyBudgetBeforeRequestRateWindow(t *testing.T) {
	logger := &fakeRequestLogger{}
	budgets := &fakeBudgetProvider{usage: admin.APIKeyBudgetUsage{RequestBudgetExceeded: true}}
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "upstream-token"}}}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{key: admin.APIKey{ID: 42, Name: "budgeted key", RequestsPerMinute: 1, RequestBudget24h: 1}}, tokens, Config{
		UpstreamBaseURL: "https://upstream.example.test",
		Logger:          logger,
		BudgetProvider:  budgets,
	}, http.DefaultClient)
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, body = %s, want 429", recorder.Code, recorder.Body.String())
	}
	if got := proxy.APIKeyRequestRateSnapshot(); len(got) != 0 {
		t.Fatalf("request rate snapshot = %+v, want no request-window usage for budget rejection", got)
	}
	assertLastLoggedError(t, logger, "api_key_request_budget_exceeded")
}

func TestProxyLogsPreciseAPIKeyTokenBudgetReason(t *testing.T) {
	logger := &fakeRequestLogger{}
	budgets := &fakeBudgetProvider{usage: admin.APIKeyBudgetUsage{TokenBudgetExceeded: true}}
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "upstream-token"}}}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{key: admin.APIKey{ID: 42, Name: "budgeted key", TokenBudget30d: 1}}, tokens, Config{
		UpstreamBaseURL: "https://upstream.example.test",
		Logger:          logger,
		BudgetProvider:  budgets,
	}, http.DefaultClient)
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusTooManyRequests || !strings.Contains(recorder.Body.String(), "rate_limit_exceeded") {
		t.Fatalf("status/body = %d/%s, want 429 rate_limit_exceeded", recorder.Code, recorder.Body.String())
	}
	assertLastLoggedError(t, logger, "api_key_token_budget_exceeded")
}

func TestProxyFailsClosedWhenAPIKeyBudgetUsageCannotBeChecked(t *testing.T) {
	logger := &fakeRequestLogger{}
	budgets := &fakeBudgetProvider{err: errors.New("budget store unavailable")}
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "upstream-token"}}}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{key: admin.APIKey{ID: 42, Name: "budgeted key", RequestBudget24h: 1}}, tokens, Config{
		UpstreamBaseURL: "https://upstream.example.test",
		Logger:          logger,
		BudgetProvider:  budgets,
	}, http.DefaultClient)
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusInternalServerError || !strings.Contains(recorder.Body.String(), "internal_error") {
		t.Fatalf("status/body = %d/%s, want 500 internal_error", recorder.Code, recorder.Body.String())
	}
	if tokens.calls != 0 {
		t.Fatalf("account calls = %d, want 0 when budget usage cannot be checked", tokens.calls)
	}
	assertLastLoggedError(t, logger, "internal_error")
}

func TestProxyLogsPreciseGatewayConcurrencyLimitReason(t *testing.T) {
	logger := &fakeRequestLogger{}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, &fakeSelectedAccountProvider{}, Config{
		UpstreamBaseURL:                 "https://upstream.example.test",
		MaxConcurrentGatewayRequests:    1,
		MaxConcurrentRequestsPerAccount: 0,
		Logger:                          logger,
	}, http.DefaultClient)
	release, ok := proxy.tryAcquireGatewaySlot(1)
	if !ok {
		t.Fatal("failed to acquire setup gateway slot")
	}
	defer release()
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusTooManyRequests || !strings.Contains(recorder.Body.String(), "rate_limit_exceeded") {
		t.Fatalf("status/body = %d/%s, want 429 rate_limit_exceeded", recorder.Code, recorder.Body.String())
	}
	assertLastLoggedError(t, logger, "gateway_concurrency_limited")
}

func TestProxyLogsPreciseAPIKeyConcurrencyLimitReason(t *testing.T) {
	logger := &fakeRequestLogger{}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{key: admin.APIKey{ID: 42, Name: "busy key"}}, &fakeSelectedAccountProvider{}, Config{
		UpstreamBaseURL:             "https://upstream.example.test",
		MaxConcurrentRequestsPerKey: 1,
		Logger:                      logger,
	}, http.DefaultClient)
	release, ok := proxy.tryAcquireAPIKeySlot(42, 1)
	if !ok {
		t.Fatal("failed to acquire setup api key slot")
	}
	defer release()
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusTooManyRequests || !strings.Contains(recorder.Body.String(), "rate_limit_exceeded") {
		t.Fatalf("status/body = %d/%s, want 429 rate_limit_exceeded", recorder.Code, recorder.Body.String())
	}
	assertLastLoggedError(t, logger, "api_key_concurrency_limited")
}

func TestProxyLogsPreciseProviderAccountConcurrencyLimitReason(t *testing.T) {
	logger := &fakeRequestLogger{}
	proxy := NewProxyWithClient(
		&fakeAPIKeyAuthenticator{},
		&fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 7, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "busy-token"}}},
		Config{
			UpstreamBaseURL:                 "https://upstream.example.test",
			MaxConcurrentRequestsPerAccount: 1,
			Logger:                          logger,
		},
		http.DefaultClient,
	)
	release, ok := proxy.tryAcquireAccountSlot(7, 1)
	if !ok {
		t.Fatal("failed to acquire setup account slot")
	}
	defer release()
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusTooManyRequests || !strings.Contains(recorder.Body.String(), "rate_limit_exceeded") {
		t.Fatalf("status/body = %d/%s, want 429 rate_limit_exceeded", recorder.Code, recorder.Body.String())
	}
	assertLastLoggedError(t, logger, "provider_account_concurrency_limited")
}

func TestProxyLogsGatewayFallbackCountsForRetryableUpstreamFailure(t *testing.T) {
	logger := &fakeRequestLogger{}
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{
		{AccountID: 1, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "first-token"},
		{AccountID: 2, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "second-token"},
	}}
	transportCalls := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		transportCalls++
		if transportCalls == 1 {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     http.Header{"Retry-After": []string{"30"}},
				Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"rate limited"}}`)),
				Request:    r,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, tokens, Config{UpstreamBaseURL: "https://upstream.example.test", Logger: logger}, client)
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if len(logger.entries) != 1 {
		t.Fatalf("logged entries = %d, want 1", len(logger.entries))
	}
	entry := logger.entries[0]
	if entry.GatewayAttemptCount != 2 || entry.GatewayFallbackCount != 1 {
		t.Fatalf("gateway diagnostics = attempts:%d fallbacks:%d, want 2/1", entry.GatewayAttemptCount, entry.GatewayFallbackCount)
	}
}

func TestProxyLogsGatewayFallbackCountsForBusyAccountFallback(t *testing.T) {
	logger := &fakeRequestLogger{}
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{
		{AccountID: 1, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "first-token"},
		{AccountID: 2, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "second-token"},
	}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, tokens, Config{
		UpstreamBaseURL:                 "https://upstream.example.test",
		MaxConcurrentRequestsPerAccount: 1,
		Logger:                          logger,
	}, client)
	release, ok := proxy.tryAcquireAccountSlot(1, 1)
	if !ok {
		t.Fatal("failed to acquire setup account slot")
	}
	defer release()
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if len(logger.entries) != 1 {
		t.Fatalf("logged entries = %d, want 1", len(logger.entries))
	}
	entry := logger.entries[0]
	if entry.GatewayAttemptCount != 2 || entry.GatewayFallbackCount != 1 {
		t.Fatalf("gateway diagnostics = attempts:%d fallbacks:%d, want 2/1", entry.GatewayAttemptCount, entry.GatewayFallbackCount)
	}
}

func TestProxyRetriesAnotherAccountBeforeStreaming(t *testing.T) {
	transportCalls := 0
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "first-token"}, {AccountID: 2, AuthorizationToken: "second-token"}}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		transportCalls++
		if transportCalls == 1 {
			return nil, errors.New("upstream unavailable")
		}
		if r.Header.Get("Authorization") != "Bearer second-token" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, tokens, Config{UpstreamBaseURL: "https://upstream.example.test"}, client)
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if tokens.calls != 2 {
		t.Fatalf("token calls = %d, want 2", tokens.calls)
	}
	if len(tokens.exclusions) != 2 || len(tokens.exclusions[1]) != 1 || tokens.exclusions[1][0] != 1 {
		t.Fatalf("exclusions = %+v, want second call excluding account 1", tokens.exclusions)
	}
	if transportCalls != 2 {
		t.Fatalf("transport calls = %d, want 2", transportCalls)
	}
}

func TestProxyExcludesEveryFailedAccountBeforeStreaming(t *testing.T) {
	transportCalls := 0
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{
		{AccountID: 1, AuthorizationToken: "first-token"},
		{AccountID: 2, AuthorizationToken: "second-token"},
		{AccountID: 3, AuthorizationToken: "third-token"},
	}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		transportCalls++
		if transportCalls < 3 {
			return nil, errors.New("upstream unavailable")
		}
		if r.Header.Get("Authorization") != "Bearer third-token" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, tokens, Config{UpstreamBaseURL: "https://upstream.example.test"}, client)
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if tokens.calls != 3 {
		t.Fatalf("token calls = %d, want 3", tokens.calls)
	}
	if !slices.Equal(tokens.exclusions[1], []int64{1}) || !slices.Equal(tokens.exclusions[2], []int64{1, 2}) {
		t.Fatalf("exclusions = %+v, want retry calls excluding all failed accounts", tokens.exclusions)
	}
}

func TestProxyRecordsRateLimitAndRetriesAnotherAccountBeforeStreaming(t *testing.T) {
	transportCalls := 0
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "rate-limited-token"}, {AccountID: 2, AuthorizationToken: "second-token"}}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		transportCalls++
		if transportCalls == 1 {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     http.Header{"Retry-After": []string{"120"}},
				Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"rate limited"}}`)),
				Request:    r,
			}, nil
		}
		if r.Header.Get("Authorization") != "Bearer second-token" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, tokens, Config{UpstreamBaseURL: "https://upstream.example.test"}, client)
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if tokens.calls != 2 {
		t.Fatalf("token calls = %d, want 2", tokens.calls)
	}
	if len(tokens.failures) != 1 {
		t.Fatalf("failures = %+v, want one rate-limit report", tokens.failures)
	}
	failure := tokens.failures[0]
	if failure.accountID != 1 || failure.statusCode != http.StatusTooManyRequests || failure.retryAfter != "120" || !strings.Contains(failure.message, "rate limited") {
		t.Fatalf("failure = %+v", failure)
	}
}

func TestProxyReturnsAccountsUnavailableWhenRetryableUpstreamHasNoFallbackAccount(t *testing.T) {
	tokens := &fakeSelectedAccountProvider{
		accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "rate-limited-token"}},
		errs:     []error{nil, provider.ErrAccountsUnavailable},
	}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{"Content-Type": []string{"application/json"}, "Retry-After": []string{"60"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"The usage limit has been reached"}}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, tokens, Config{UpstreamBaseURL: "https://upstream.example.test"}, client)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4-mini","input":"hi"}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Retry-After"); got != "" {
		t.Fatalf("Retry-After = %q, want empty", got)
	}
	if !strings.Contains(recorder.Body.String(), "provider_accounts_unavailable") {
		t.Fatalf("body = %q, want provider_accounts_unavailable", recorder.Body.String())
	}
	if tokens.calls != 2 {
		t.Fatalf("token calls = %d, want fallback lookup after first 429", tokens.calls)
	}
}

func TestProxyRecordsExpiredAccountOnUnauthorizedAndRetriesAnotherAccount(t *testing.T) {
	transportCalls := 0
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "expired-token"}, {AccountID: 2, AuthorizationToken: "second-token"}}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		transportCalls++
		if transportCalls == 1 {
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"invalid access token"}}`)),
				Request:    r,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, tokens, Config{UpstreamBaseURL: "https://upstream.example.test"}, client)
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(tokens.failures) != 1 {
		t.Fatalf("failures = %+v, want one unauthorized report", tokens.failures)
	}
	if tokens.failures[0].accountID != 1 || tokens.failures[0].statusCode != http.StatusUnauthorized {
		t.Fatalf("failure = %+v", tokens.failures[0])
	}
}

func TestProxyRecordsCircuitOpenOnUpstreamServerErrorAndReturnsFinalError(t *testing.T) {
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "bad-token"}, {AccountID: 1, AuthorizationToken: "same-account-token"}}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"upstream unavailable"}}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, tokens, Config{UpstreamBaseURL: "https://upstream.example.test"}, client)
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(tokens.failures) != 1 {
		t.Fatalf("failures = %+v, want one upstream status report", tokens.failures)
	}
	if tokens.failures[0].accountID != 1 || tokens.failures[0].statusCode != http.StatusBadGateway {
		t.Fatalf("failure = %+v", tokens.failures[0])
	}
}

func TestProxyDoesNotRetrySameAccountBeforeStreaming(t *testing.T) {
	transportCalls := 0
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "first-token"}, {AccountID: 1, AuthorizationToken: "same-account-token"}}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		transportCalls++
		return nil, errors.New("upstream unavailable")
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, tokens, Config{UpstreamBaseURL: "https://upstream.example.test"}, client)
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "upstream_unavailable") {
		t.Fatalf("body = %q, want upstream_unavailable", recorder.Body.String())
	}
	if tokens.calls != 2 {
		t.Fatalf("token calls = %d, want 2", tokens.calls)
	}
	if len(tokens.exclusions) != 2 || len(tokens.exclusions[1]) != 1 || tokens.exclusions[1][0] != 1 {
		t.Fatalf("exclusions = %+v, want second call excluding account 1", tokens.exclusions)
	}
	if transportCalls != 1 {
		t.Fatalf("transport calls = %d, want 1", transportCalls)
	}
}

func TestProxyReplaysSmallPOSTBodyOnFallback(t *testing.T) {
	const requestBody = `{"model":"gpt-5","messages":[],"stream":false}`
	transportCalls := 0
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "first-token"}, {AccountID: 2, AuthorizationToken: "second-token"}}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		transportCalls++
		if transportCalls == 1 {
			return nil, errors.New("upstream unavailable")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll returned error: %v", err)
		}
		if string(body) != requestBody {
			t.Fatalf("body = %q, want %q", string(body), requestBody)
		}
		if r.Header.Get("Authorization") != "Bearer second-token" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, tokens, Config{UpstreamBaseURL: "https://upstream.example.test"}, client)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(requestBody))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if tokens.calls != 2 {
		t.Fatalf("token calls = %d, want 2", tokens.calls)
	}
	if len(tokens.models) != 2 || tokens.models[0] != "gpt-5" || tokens.models[1] != "gpt-5" {
		t.Fatalf("requested models = %+v, want gpt-5 for both attempts", tokens.models)
	}
	if transportCalls != 2 {
		t.Fatalf("transport calls = %d, want 2", transportCalls)
	}
}

func TestProxyCarriesStickySessionOnFallbackSelection(t *testing.T) {
	const requestBody = `{"model":"gpt-5","session_id":" workspace-123 ","messages":[],"stream":false}`
	transportCalls := 0
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "first-token"}, {AccountID: 2, AuthorizationToken: "second-token"}}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		transportCalls++
		if transportCalls == 1 {
			return nil, errors.New("upstream unavailable")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, tokens, Config{
		UpstreamBaseURL: "https://upstream.example.test",
		ModelProvider: fakeModelProvider{
			defaultModel:  "gpt-5-mini",
			allowedModels: []string{"gpt-5", "gpt-5-mini"},
		},
	}, client)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(requestBody))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if !slices.Equal(tokens.models, []string{"gpt-5", "gpt-5"}) {
		t.Fatalf("selected models = %+v, want gpt-5 for both attempts", tokens.models)
	}
	if !slices.Equal(tokens.sessions, []string{"workspace-123", "workspace-123"}) {
		t.Fatalf("selected sessions = %+v, want sticky session on both attempts", tokens.sessions)
	}
	if len(tokens.exclusions) != 2 || !slices.Equal(tokens.exclusions[1], []int64{1}) {
		t.Fatalf("exclusions = %+v, want second attempt to exclude failed account 1", tokens.exclusions)
	}
}

func TestProxyDoesNotRetryLargePOSTBody(t *testing.T) {
	transportCalls := 0
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "first-token"}, {AccountID: 2, AuthorizationToken: "second-token"}}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		transportCalls++
		return nil, errors.New("upstream unavailable")
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, tokens, Config{UpstreamBaseURL: "https://upstream.example.test"}, client)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(strings.Repeat("a", maxReplayableRequestBody+1)))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "invalid_request") {
		t.Fatalf("body = %q, want invalid_request", recorder.Body.String())
	}
	if tokens.calls != 0 {
		t.Fatalf("token calls = %d, want 0", tokens.calls)
	}
	if transportCalls != 0 {
		t.Fatalf("transport calls = %d, want 0", transportCalls)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type brokenReader struct {
	sent bool
}

func (r *brokenReader) Read(p []byte) (int, error) {
	if !r.sent {
		r.sent = true
		return copy(p, "data: partial\n\n"), nil
	}
	return 0, errors.New("stream broke")
}

func (r *brokenReader) Close() error {
	return nil
}

func TestProxyDoesNotRetryAfterStreamingBegins(t *testing.T) {
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "first-token"}, {AccountID: 2, AuthorizationToken: "second-token"}}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       &brokenReader{},
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, tokens, Config{UpstreamBaseURL: "https://upstream.example.test"}, client)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","stream":true}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if tokens.calls != 1 {
		t.Fatalf("token calls = %d, want 1", tokens.calls)
	}
	if !strings.Contains(recorder.Body.String(), "data: partial") {
		t.Fatalf("body = %q", recorder.Body.String())
	}
}
