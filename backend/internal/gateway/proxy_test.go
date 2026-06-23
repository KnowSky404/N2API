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
}

func (a *fakeAPIKeyAuthenticator) AuthenticateAPIKey(_ context.Context, apiKey string) (admin.APIKey, error) {
	a.gotKey = apiKey
	if a.err != nil {
		return admin.APIKey{}, a.err
	}
	if a.key.ID != 0 {
		return a.key, nil
	}
	return admin.APIKey{ID: 42, Name: "test key"}, nil
}

type fakeSelectedAccountProvider struct {
	accounts   []SelectedAccount
	errs       []error
	calls      int
	models     []string
	sessions   []string
	exclusions [][]int64
	failures   []reportedAccountFailure
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

func (p *fakeSelectedAccountProvider) RecordAccountFailure(_ context.Context, accountID int64, statusCode int, retryAfter, message string) error {
	p.failures = append(p.failures, reportedAccountFailure{
		accountID:  accountID,
		statusCode: statusCode,
		retryAfter: retryAfter,
		message:    message,
	})
	return nil
}

type fakeModelProvider struct {
	defaultModel  string
	allowedModels []string
	exposedModels []ExposedModel
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

type fakeRequestLogger struct {
	entries []RequestLog
}

func (l *fakeRequestLogger) CreateRequestLog(_ context.Context, entry RequestLog) error {
	l.entries = append(l.entries, entry)
	return nil
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
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	defer upstream.Close()
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 7, AccountType: provider.AccountTypeAPIUpstream, DisplayName: "Upstream A", AuthorizationToken: "upstream-token"}}}, Config{
		UpstreamBaseURL: upstream.URL,
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
	if entry.RequestID == "" {
		t.Fatal("RequestID is empty")
	}
	if entry.ClientKeyID != 42 || entry.Provider != "openai" || entry.Route != "/v1/responses/resp_123" || entry.Method != http.MethodGet {
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
