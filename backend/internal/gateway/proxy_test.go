package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	n2metrics "github.com/KnowSky404/N2API/backend/internal/metrics"
	"github.com/KnowSky404/N2API/backend/internal/provider"
	"github.com/KnowSky404/N2API/backend/internal/requestlog"
)

type fakeAPIKeyAuthenticator struct {
	gotKey  string
	err     error
	key     admin.APIKey
	keys    map[string]admin.APIKey
	unbound bool
}

var defaultTestRoutingPoolID int64 = 1

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
		return withTestRoutingPool(key, a.unbound), nil
	}
	if a.key.ID != 0 {
		return withTestRoutingPool(a.key, a.unbound), nil
	}
	return withTestRoutingPool(admin.APIKey{ID: 42, Name: "test key"}, a.unbound), nil
}

func withTestRoutingPool(key admin.APIKey, unbound bool) admin.APIKey {
	if key.RoutingPoolID == nil && !unbound {
		key.RoutingPoolID = &defaultTestRoutingPoolID
	}
	return key
}

type fakeSelectedAccountProvider struct {
	accounts                    []SelectedAccount
	errs                        []error
	calls                       int
	models                      []string
	sessions                    []string
	poolIDs                     []int64
	chainPoolIDs                []int64
	exclusions                  [][]int64
	failures                    []reportedAccountFailure
	used                        []int64
	recovered                   []int64
	recoveryErr                 error
	recoveryContextErr          error
	recoveryContextHasDeadline  bool
	authorizationRefreshes      []authorizationRefreshCall
	refreshedAuthorizationToken string
	refreshAuthorizationRetry   bool
	refreshFailureRecorded      bool
	refreshAuthorizationErr     error
}

type reportedAccountFailure struct {
	accountID  int64
	statusCode int
	retryAfter string
	message    string
}

type authorizationRefreshCall struct {
	accountID           int64
	rejectedAccessToken string
	statusCode          int
	message             string
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

func (p *fakeSelectedAccountProvider) RefreshAccountAuthorization(_ context.Context, accountID int64, rejectedAccessToken string, statusCode int, message string) (string, bool, bool, error) {
	p.authorizationRefreshes = append(p.authorizationRefreshes, authorizationRefreshCall{
		accountID:           accountID,
		rejectedAccessToken: rejectedAccessToken,
		statusCode:          statusCode,
		message:             message,
	})
	return p.refreshedAuthorizationToken, p.refreshAuthorizationRetry, p.refreshFailureRecorded, p.refreshAuthorizationErr
}

func (p *fakeSelectedAccountProvider) RecordAccountUsed(_ context.Context, accountID int64) error {
	p.used = append(p.used, accountID)
	return nil
}

func (p *fakeSelectedAccountProvider) RecordAccountRecovered(ctx context.Context, accountID int64) error {
	p.recovered = append(p.recovered, accountID)
	p.recoveryContextErr = ctx.Err()
	_, p.recoveryContextHasDeadline = ctx.Deadline()
	return p.recoveryErr
}

type fakeModelProvider struct {
	defaultModel       string
	chainExposedModels []ExposedModel
	chainPoolIDs       []int64
}

func (p fakeModelProvider) DefaultModel(context.Context) (string, error) {
	return p.defaultModel, nil
}

func (p *fakeModelProvider) ListExposedModelsForRoutingPoolChain(_ context.Context, routingPoolID int64) ([]ExposedModel, error) {
	p.chainPoolIDs = append(p.chainPoolIDs, routingPoolID)
	return append([]ExposedModel(nil), p.chainExposedModels...), nil
}

type fakeRequestLogger struct {
	entries []RequestLog
	err     error
}

func (l *fakeRequestLogger) CreateRequestLog(_ context.Context, entry RequestLog) error {
	l.entries = append(l.entries, entry)
	return l.err
}

type contextRecordingRequestLogger struct {
	contextErr error
	entries    []RequestLog
}

func (l *contextRecordingRequestLogger) CreateRequestLog(ctx context.Context, entry RequestLog) error {
	l.contextErr = ctx.Err()
	l.entries = append(l.entries, entry)
	return nil
}

type flushRecordingResponseWriter struct {
	header  http.Header
	body    strings.Builder
	status  int
	flushes int
}

func (w *flushRecordingResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *flushRecordingResponseWriter) WriteHeader(status int) {
	w.status = status
}

func (w *flushRecordingResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(data)
}

func (w *flushRecordingResponseWriter) Flush() {
	w.flushes++
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

func TestProxyLogsWithDetachedContextAfterClientCancellation(t *testing.T) {
	logger := &contextRecordingRequestLogger{}
	proxy := NewProxy(nil, nil, Config{Logger: logger})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	proxy.logRequest(ctx, RequestLog{RequestID: "req_cancelled", StatusCode: http.StatusOK})

	if logger.contextErr != nil {
		t.Fatalf("logging context error = %v, want detached live context", logger.contextErr)
	}
	if len(logger.entries) != 1 || logger.entries[0].RequestID != "req_cancelled" {
		t.Fatalf("logged entries = %+v, want cancelled request persisted", logger.entries)
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

type fakeErrorPassthroughRuleProvider struct {
	rules []admin.ErrorPassthroughRule
	err   error
	calls int
}

func (p *fakeErrorPassthroughRuleProvider) ListErrorPassthroughRules(context.Context) ([]admin.ErrorPassthroughRule, error) {
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	return append([]admin.ErrorPassthroughRule(nil), p.rules...), nil
}

type fakeBudgetProvider struct {
	usage admin.APIKeyBudgetUsage
	err   error
	calls int
	key   admin.APIKey
}

type captureMetricsObserver struct {
	started   int
	finished  []capturedGatewayRequest
	upstream  [][2]string
	fallbacks []string
	routing   []string
	limits    [][2]string
	streams   [][2]string
	usage     []capturedUsage
}

type capturedGatewayRequest struct {
	route, accountType string
	status             int
	stream             bool
}

type capturedUsage struct {
	source                                string
	priced                                bool
	input, output, cachedInput, reasoning int
	costMicrousd                          int64
}

func (m *captureMetricsObserver) GatewayRequestStarted() { m.started++ }
func (m *captureMetricsObserver) GatewayRequestFinished(route string, statusCode int, stream bool, accountType string, _ time.Duration) {
	m.finished = append(m.finished, capturedGatewayRequest{route: route, status: statusCode, stream: stream, accountType: accountType})
}
func (m *captureMetricsObserver) ObserveUpstreamAttempt(accountType, outcome string) {
	m.upstream = append(m.upstream, [2]string{accountType, outcome})
}
func (m *captureMetricsObserver) ObserveFallback(reason string) {
	m.fallbacks = append(m.fallbacks, reason)
}
func (m *captureMetricsObserver) ObserveRoutingFailure(reason string) {
	m.routing = append(m.routing, reason)
}
func (m *captureMetricsObserver) ObserveLimitRejection(scope, reason string) {
	m.limits = append(m.limits, [2]string{scope, reason})
}
func (m *captureMetricsObserver) ObserveStream(route, outcome string) {
	m.streams = append(m.streams, [2]string{route, outcome})
}
func (m *captureMetricsObserver) ObserveUsage(source string, priced bool, input, output, cachedInput, reasoning int, costMicrousd int64) {
	m.usage = append(m.usage, capturedUsage{source: source, priced: priced, input: input, output: output, cachedInput: cachedInput, reasoning: reasoning, costMicrousd: costMicrousd})
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

func TestProxyMetricsCoverSupportedAuthenticationFailure(t *testing.T) {
	observer := &captureMetricsObserver{}
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, &fakeSelectedAccountProvider{}, Config{Metrics: observer})
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/models", nil))

	if observer.started != 1 || len(observer.finished) != 1 {
		t.Fatalf("request lifecycle = started:%d finished:%+v", observer.started, observer.finished)
	}
	got := observer.finished[0]
	if got.route != "/v1/models" || got.status != http.StatusUnauthorized || got.stream || got.accountType != "none" {
		t.Fatalf("finished request = %+v", got)
	}
	if len(observer.usage) != 0 {
		t.Fatalf("unauthenticated usage observations = %+v", observer.usage)
	}
}

func TestProxyTrafficUpdatesPrometheusRegistry(t *testing.T) {
	registry := n2metrics.New(nil)
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, &fakeSelectedAccountProvider{}, Config{Metrics: registry})
	proxy.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/models", nil))

	families, err := registry.Gatherer().Gather()
	if err != nil {
		t.Fatal(err)
	}
	var requestCount, activeRequests float64
	for _, family := range families {
		switch family.GetName() {
		case "n2api_gateway_requests_total":
			for _, metric := range family.Metric {
				labels := map[string]string{}
				for _, label := range metric.Label {
					labels[label.GetName()] = label.GetValue()
				}
				if labels["route"] == "models" && labels["status_class"] == "4xx" && labels["stream"] == "false" && labels["account_type"] == "none" {
					requestCount = metric.GetCounter().GetValue()
				}
			}
		case "n2api_gateway_active_requests":
			activeRequests = family.Metric[0].GetGauge().GetValue()
		}
	}
	if requestCount != 1 || activeRequests != 0 {
		t.Fatalf("prometheus request metrics = count:%v active:%v", requestCount, activeRequests)
	}
}

func TestProxyMetricsCoverUpstreamUsageAndCompletedStream(t *testing.T) {
	observer := &captureMetricsObserver{}
	accounts := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{
		AccountID: 7, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "upstream-token",
	}}}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":3,\"output_tokens\":2}}}\n\n")),
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, accounts, Config{Metrics: observer}, client)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-test","stream":true}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK || len(observer.finished) != 1 || !observer.finished[0].stream || observer.finished[0].accountType != provider.AccountTypeAPIUpstream {
		t.Fatalf("response/finished = %d %+v", recorder.Code, observer.finished)
	}
	if !slices.Equal(observer.upstream, [][2]string{{provider.AccountTypeAPIUpstream, "success"}}) {
		t.Fatalf("upstream observations = %+v", observer.upstream)
	}
	if !slices.Equal(observer.streams, [][2]string{{"/v1/responses", "completed"}}) {
		t.Fatalf("stream observations = %+v", observer.streams)
	}
	if len(observer.usage) != 1 || observer.usage[0].source != "stream" || observer.usage[0].input != 3 || observer.usage[0].output != 2 {
		t.Fatalf("usage observations = %+v", observer.usage)
	}
}

func TestProxyMetricsRetainSelectedAccountTypeAfterConcurrencyExhaustion(t *testing.T) {
	observer := &captureMetricsObserver{}
	accounts := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{
		AccountID: 7, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "upstream-token",
	}}}
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, accounts, Config{
		MaxConcurrentRequestsPerAccount: 1,
		Metrics:                         observer,
	})
	release, ok := proxy.tryAcquireAccountSlot(7, 1)
	if !ok {
		t.Fatal("failed to acquire setup account slot")
	}
	defer release()
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusTooManyRequests || len(observer.finished) != 1 {
		t.Fatalf("response/finished = %d %+v", recorder.Code, observer.finished)
	}
	if observer.finished[0].accountType != provider.AccountTypeAPIUpstream {
		t.Fatalf("account type = %q", observer.finished[0].accountType)
	}
}

func TestStatusRecorderKeepsFirstCommittedStatus(t *testing.T) {
	underlying := httptest.NewRecorder()
	recorder := &statusRecorder{ResponseWriter: underlying}
	recorder.WriteHeader(http.StatusCreated)
	recorder.WriteHeader(http.StatusInternalServerError)
	if recorder.statusCode() != http.StatusCreated || underlying.Code != http.StatusCreated {
		t.Fatalf("status = recorder:%d underlying:%d", recorder.statusCode(), underlying.Code)
	}
}

func TestProxyModelsReturnsEmptyListForUnboundKey(t *testing.T) {
	upstreamCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalled = true
		t.Fatal("upstream should not be called for local models list")
	}))
	defer upstream.Close()
	auth := &fakeAPIKeyAuthenticator{unbound: true}
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "upstream-token"}}}
	proxy := NewProxy(auth, tokens, Config{
		UpstreamBaseURL: upstream.URL,
		ModelProvider:   fakeModelProvider{},
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
	if body.Object != "list" || len(body.Data) != 0 {
		t.Fatalf("models response = %+v", body)
	}
}

func TestProxyRequiresRoutingPoolBeforeParsingOrCallingUpstream(t *testing.T) {
	var transportCalls int32
	accounts := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "upstream-token"}}}
	logger := &fakeRequestLogger{}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		atomic.AddInt32(&transportCalls, 1)
		return nil, errors.New("unexpected upstream call")
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{unbound: true}, accounts, Config{
		Logger:        logger,
		ModelProvider: fakeModelProvider{},
	}, client)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"code":"routing_pool_required"`) {
		t.Fatalf("body = %q, want routing_pool_required", recorder.Body.String())
	}
	if accounts.calls != 0 || atomic.LoadInt32(&transportCalls) != 0 {
		t.Fatalf("account calls = %d transport calls = %d, want no upstream work", accounts.calls, transportCalls)
	}
	if len(logger.entries) != 1 || logger.entries[0].Error != "routing_pool_required" || logger.entries[0].RoutingPoolError != "routing_pool_required" {
		t.Fatalf("request logs = %+v, want routing_pool_required error fields", logger.entries)
	}
}

func TestProxyModelsForPoolBoundKeyUseRoutingPoolChain(t *testing.T) {
	poolID := int64(1)
	models := &fakeModelProvider{
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
	poolID := int64(1)
	auth := &fakeAPIKeyAuthenticator{key: admin.APIKey{
		ID:            42,
		Name:          "test key",
		ModelPolicy:   admin.APIKeyModelPolicySelected,
		AllowedModels: []string{"gpt-5-mini", "gpt-4o"},
		RoutingPoolID: &poolID,
	}}
	accounts := &fakeSelectedAccountProvider{}
	proxy := NewProxy(auth, accounts, Config{
		ModelProvider: &fakeModelProvider{chainExposedModels: []ExposedModel{
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
			defaultModel: "gpt-5-mini",
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

func TestProxyBoundsNonStreamingUpstreamResponseBeforeCommittingHeaders(t *testing.T) {
	logger := &fakeRequestLogger{}
	upstreamBody := &countingReadCloser{reader: strings.NewReader(strings.Repeat("response-canary", 8))}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusCreated,
			Header: http.Header{
				"Content-Type":      []string{"application/json"},
				"X-Upstream-Canary": []string{"must-not-be-copied"},
			},
			Body:    upstreamBody,
			Request: request,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "token"}}}, Config{
		MaxUpstreamResponseBodyBytes: 32,
		Logger:                       logger,
	}, client)
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadGateway || !strings.Contains(recorder.Body.String(), `"code":"upstream_response_too_large"`) {
		t.Fatalf("status/body=%d/%s, want stable oversized response", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "response-canary") || recorder.Header().Get("X-Upstream-Canary") != "" {
		t.Fatalf("oversized upstream data leaked: headers=%v body=%s", recorder.Header(), recorder.Body.String())
	}
	if upstreamBody.bytesRead != 33 {
		t.Fatalf("upstream bytes read=%d, want max+1", upstreamBody.bytesRead)
	}
	if len(logger.entries) != 1 || logger.entries[0].Error != "upstream_response_too_large" || logger.entries[0].UsageSource != "missing" {
		t.Fatalf("request log=%+v", logger.entries)
	}
}

func TestProxyAllowsNonStreamingUpstreamResponseAtExactLimit(t *testing.T) {
	const responseBody = `{"id":"response-exact-limit"}`
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusCreated,
			Header:     http.Header{"Content-Type": []string{"application/json"}, "X-Upstream": []string{"preserved"}},
			Body:       io.NopCloser(strings.NewReader(responseBody)),
			Request:    request,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "token"}}}, Config{
		MaxUpstreamResponseBodyBytes: len(responseBody),
	}, client)
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated || recorder.Body.String() != responseBody || recorder.Header().Get("X-Upstream") != "preserved" {
		t.Fatalf("status=%d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
	}
}

func TestProxyBoundsFinalRetryableUpstreamResponseAfterFailureCapture(t *testing.T) {
	const responseLimit = maxFailureBody + 128
	upstreamPayload := `{"error":{"message":"temporary failure"},"padding":"` + strings.Repeat("x", responseLimit) + `"}`
	upstreamBody := &countingReadCloser{reader: strings.NewReader(upstreamPayload)}
	logger := &fakeRequestLogger{}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Header: http.Header{
				"Content-Type":      []string{"application/json"},
				"Content-Length":    []string{strconv.Itoa(len(upstreamPayload))},
				"X-Upstream-Canary": []string{"must-not-be-copied"},
			},
			Body:    upstreamBody,
			Request: request,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "token"}}}, Config{
		MaxAcceptedRequestBodyBytes:  1024,
		MaxInMemoryReplayBodyBytes:   1,
		MaxUpstreamResponseBodyBytes: responseLimit,
		Logger:                       logger,
	}, client)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5"}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadGateway || !strings.Contains(recorder.Body.String(), `"code":"upstream_response_too_large"`) {
		t.Fatalf("status/body-length=%d/%d, want bounded upstream response error", recorder.Code, recorder.Body.Len())
	}
	if recorder.Header().Get("Content-Length") != "" || recorder.Header().Get("X-Upstream-Canary") != "" || strings.Contains(recorder.Body.String(), "temporary failure") {
		t.Fatalf("upstream response leaked: headers=%v body=%s", recorder.Header(), recorder.Body.String())
	}
	if !upstreamBody.closed {
		t.Fatal("original upstream response body was not closed")
	}
	if len(logger.entries) != 1 || logger.entries[0].GatewayAttemptCount != 1 || logger.entries[0].GatewayFallbackCount != 0 || logger.entries[0].Error != "upstream_response_too_large" {
		t.Fatalf("request log=%+v, want attempts/fallbacks/error 1/0/upstream_response_too_large", logger.entries)
	}
}

func TestProxyBoundsMatchingPassThroughResponseAfterFailureCapture(t *testing.T) {
	const responseLimit = maxFailureBody + 128
	upstreamPayload := `{"error":{"code":"insufficient_quota","message":"quota exceeded"},"padding":"` + strings.Repeat("x", responseLimit) + `"}`
	rules := &fakeErrorPassthroughRuleProvider{rules: []admin.ErrorPassthroughRule{{
		Pattern:   "429",
		MatchType: "status_code",
		Enabled:   true,
		Priority:  1,
	}}}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header: http.Header{
				"Content-Type":      []string{"application/json"},
				"Content-Length":    []string{strconv.Itoa(len(upstreamPayload))},
				"X-Upstream-Canary": []string{"must-not-be-copied"},
			},
			Body:    io.NopCloser(strings.NewReader(upstreamPayload)),
			Request: request,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "token"}}}, Config{
		ErrorPassthroughRulesProvider: rules,
		MaxUpstreamResponseBodyBytes:  responseLimit,
	}, client)
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadGateway || !strings.Contains(recorder.Body.String(), `"code":"upstream_response_too_large"`) {
		t.Fatalf("status/body-length=%d/%d, want bounded pass-through error", recorder.Code, recorder.Body.Len())
	}
	if recorder.Header().Get("Content-Length") != "" || recorder.Header().Get("X-Upstream-Canary") != "" || strings.Contains(recorder.Body.String(), "insufficient_quota") {
		t.Fatalf("upstream response leaked: headers=%v body=%s", recorder.Header(), recorder.Body.String())
	}
}

func TestProxyRedactsNonStreamingUpstreamReadFailure(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}, "X-Upstream-Canary": []string{"must-not-be-copied"}},
			Body:       &brokenReader{},
			Request:    request,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "token"}}}, Config{
		MaxUpstreamResponseBodyBytes: 1024,
	}, client)
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadGateway || !strings.Contains(recorder.Body.String(), `"code":"upstream_response_error"`) {
		t.Fatalf("status/body=%d/%s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "partial") || recorder.Header().Get("X-Upstream-Canary") != "" || strings.Contains(recorder.Body.String(), "stream broke") {
		t.Fatalf("upstream read failure leaked details: headers=%v body=%s", recorder.Header(), recorder.Body.String())
	}
}

func TestProxyDoesNotApplyTotalBodyLimitToSSE(t *testing.T) {
	const streamBody = "data: {\"type\":\"response.completed\"}\n\n"
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(streamBody)),
			Request:    request,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "token"}}}, Config{
		MaxUpstreamResponseBodyBytes: 1,
	}, client)
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := &flushRecordingResponseWriter{}

	proxy.ServeHTTP(recorder, req)

	if recorder.status != http.StatusOK || recorder.body.String() != streamBody {
		t.Fatalf("status/body=%d/%q, want complete SSE", recorder.status, recorder.body.String())
	}
}

func TestProxyReturnsStableUpstreamTimeoutWithoutNetworkDetails(t *testing.T) {
	logger := &fakeRequestLogger{}
	accounts := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "token"}}}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, timeoutCanaryError{}
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, accounts, Config{
		UpstreamBaseURL:             "https://upstream.example.test",
		MaxAcceptedRequestBodyBytes: 1024,
		MaxInMemoryReplayBodyBytes:  1,
		Logger:                      logger,
	}, client)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5"}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadGateway || !strings.Contains(recorder.Body.String(), `"code":"upstream_timeout"`) || strings.Contains(recorder.Body.String(), "timeout-address-canary") {
		t.Fatalf("status/body=%d/%s", recorder.Code, recorder.Body.String())
	}
	if len(accounts.failures) != 1 || accounts.failures[0].message != "upstream request timed out" {
		t.Fatalf("account failures = %+v", accounts.failures)
	}
	if len(logger.entries) != 1 || logger.entries[0].Error != "upstream_timeout" {
		t.Fatalf("request logs = %+v", logger.entries)
	}
}

func TestProxyEnforcesResponseHeaderTimeout(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()
	accounts := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "token"}}}
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, accounts, Config{
		UpstreamBaseURL:               upstream.URL,
		MaxAcceptedRequestBodyBytes:   1024,
		MaxInMemoryReplayBodyBytes:    1,
		UpstreamResponseHeaderTimeout: 20 * time.Millisecond,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5"}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadGateway || !strings.Contains(recorder.Body.String(), `"code":"upstream_timeout"`) {
		t.Fatalf("status/body=%d/%s", recorder.Code, recorder.Body.String())
	}
	if len(accounts.failures) != 1 || accounts.failures[0].message != "upstream request timed out" {
		t.Fatalf("account failures = %+v", accounts.failures)
	}
}

func TestProxyStopsWithoutFallbackWhenClientCancelsBeforeUpstreamHeaders(t *testing.T) {
	logger := &fakeRequestLogger{}
	accounts := &fakeSelectedAccountProvider{accounts: []SelectedAccount{
		{AccountID: 1, AuthorizationToken: "first-token"},
		{AccountID: 2, AuthorizationToken: "second-token"},
	}}
	transportStarted := make(chan struct{})
	var transportStartedOnce sync.Once
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		transportStartedOnce.Do(func() { close(transportStarted) })
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, accounts, Config{
		MaxConcurrentRequestsPerAccount: 1,
		Logger:                          logger,
	}, client)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		proxy.ServeHTTP(recorder, req)
		close(done)
	}()

	<-transportStarted
	cancel()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("canceled request did not stop promptly")
	}

	if recorder.Body.Len() != 0 {
		t.Fatalf("canceled client received response body %q", recorder.Body.String())
	}
	if len(accounts.failures) != 0 || accounts.calls != 1 {
		t.Fatalf("account failures/selections=%+v/%d, want none/one", accounts.failures, accounts.calls)
	}
	if len(logger.entries) != 1 || logger.entries[0].Error != "request_canceled" || logger.entries[0].GatewayAttemptCount != 1 || logger.entries[0].GatewayFallbackCount != 0 {
		t.Fatalf("request log=%+v, want request_canceled with attempts/fallbacks 1/0", logger.entries)
	}
	if snapshot := proxy.AccountConcurrencySnapshot(); len(snapshot) != 0 {
		t.Fatalf("account slots not released after cancellation: %+v", snapshot)
	}
}

func TestProxyClosesStalledSSEWithoutAppendingJSONError(t *testing.T) {
	logger := &fakeRequestLogger{}
	body := newBlockingReadCloser()
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       body,
			Request:    request,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "token"}}}, Config{
		UpstreamBaseURL:        "https://upstream.example.test",
		UpstreamSSEIdleTimeout: 25 * time.Millisecond,
		Logger:                 logger,
	}, client)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","stream":true}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK || recorder.Body.Len() != 0 || !body.isClosed() {
		t.Fatalf("status/body/closed=%d/%q/%v", recorder.Code, recorder.Body.String(), body.isClosed())
	}
	if len(logger.entries) != 1 || logger.entries[0].Error != "upstream_sse_idle_timeout" {
		t.Fatalf("request logs = %+v", logger.entries)
	}
}

func TestCopyStreamingResponseResetsIdleTimeoutAndClosesOnCancellation(t *testing.T) {
	periodicBody := &delayedChunkReadCloser{
		chunks: [][]byte{[]byte("data: one\n\n"), []byte("data: two\n\n")},
		delay:  5 * time.Millisecond,
	}
	periodicRecorder := httptest.NewRecorder()
	_, err := copyStreamingResponse(context.Background(), periodicRecorder, periodicBody, "/v1/responses", 25*time.Millisecond)
	if err != nil || periodicRecorder.Body.String() != "data: one\n\ndata: two\n\n" {
		t.Fatalf("periodic stream body/error = %q/%v", periodicRecorder.Body.String(), err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stalledBody := newBlockingReadCloser()
	done := make(chan error, 1)
	go func() {
		_, streamErr := copyStreamingResponse(ctx, httptest.NewRecorder(), stalledBody, "/v1/responses", time.Second)
		done <- streamErr
	}()
	cancel()
	select {
	case streamErr := <-done:
		if streamErr != nil || !stalledBody.isClosed() {
			t.Fatalf("cancelled stream error/closed = %v/%v", streamErr, stalledBody.isClosed())
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("cancelled stream did not stop promptly")
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
			defaultModel: "gpt-5-mini",
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
			defaultModel: "gpt-5-mini",
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
			defaultModel: "gpt-5-mini",
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
			defaultModel: "gpt-5-mini",
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
			defaultModel: "gpt-5",
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
			defaultModel: "gpt-5",
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
			defaultModel: "gpt-5",
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
			defaultModel: "gpt-5",
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
			defaultModel: "gpt-5",
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
			defaultModel: "gpt-5",
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
			defaultModel: "gpt-5",
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
			defaultModel: "gpt-5",
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
			defaultModel: "gpt-5",
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
			defaultModel: "gpt-5",
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
			defaultModel: "gpt-5",
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
		ModelProvider: &fakeModelProvider{
			defaultModel:       "gpt-5",
			chainExposedModels: []ExposedModel{{ID: "gpt-5", OwnedBy: "openai"}},
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
			defaultModel: "gpt-5-mini",
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

func TestProxyRejectsUnlistedModelForSelectedAPIKeyWithCompatibleError(t *testing.T) {
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
			defaultModel: "gpt-5-mini",
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

func TestProxyRoutesModelWithoutGlobalFilter(t *testing.T) {
	accounts := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "upstream-token"}}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"chatcmpl_123"}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, accounts, Config{
		UpstreamBaseURL: "https://upstream.example.test",
		ModelProvider: fakeModelProvider{
			defaultModel: "gpt-5-mini",
		},
	}, client)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"hidden-model","messages":[]}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if accounts.calls != 1 {
		t.Fatalf("account calls = %d, want one pool-scoped selection", accounts.calls)
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
			defaultModel: "gpt-5",
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
			defaultModel: "gpt-5",
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
			defaultModel: "gpt-5",
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
			defaultModel: "gpt-5-mini",
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
			defaultModel: "gpt-5-mini",
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

func TestProxyUsesSelectedAccountProxyURLForUpstreamRequest(t *testing.T) {
	defaultUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("default upstream should not be called for proxied account")
	}))
	defer defaultUpstream.Close()
	directUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("direct upstream should not be called when account proxy is configured")
	}))
	defer directUpstream.Close()

	var proxiedTarget string
	var proxiedAuthorization string
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxiedTarget = r.URL.String()
		proxiedAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chatcmpl_proxy"}`))
	}))
	defer proxyServer.Close()

	accounts := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{
		AccountID:          9,
		AccountType:        provider.AccountTypeAPIUpstream,
		AuthorizationToken: "sk-upstream",
		BaseURL:            directUpstream.URL,
		ProxyURL:           proxyServer.URL,
	}}}
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, accounts, Config{UpstreamBaseURL: defaultUpstream.URL})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.HasPrefix(proxiedTarget, directUpstream.URL+"/v1/chat/completions") {
		t.Fatalf("proxied target = %q, want absolute upstream URL through proxy", proxiedTarget)
	}
	if proxiedAuthorization != "Bearer sk-upstream" {
		t.Fatalf("proxied Authorization = %q, want selected account token", proxiedAuthorization)
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
		ModelProvider:         fakeModelProvider{defaultModel: "gpt-5"},
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
	var gotVersion string
	var gotHost string
	var gotBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuthorization = r.Header.Get("Authorization")
		gotChatGPTAccountID = r.Header.Get("chatgpt-account-id")
		gotOpenAIBeta = r.Header.Get("OpenAI-Beta")
		gotOriginator = r.Header.Get("originator")
		gotUserAgent = r.Header.Get("User-Agent")
		gotVersion = r.Header.Get("Version")
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
	if gotOriginator != provider.DefaultCodexFingerprintOriginator {
		t.Fatalf("originator = %q", gotOriginator)
	}
	if gotUserAgent != provider.DefaultCodexFingerprintUserAgent {
		t.Fatalf("User-Agent = %q, want %q", gotUserAgent, provider.DefaultCodexFingerprintUserAgent)
	}
	if gotVersion != provider.DefaultCodexFingerprintVersion {
		t.Fatalf("Version = %q, want %q", gotVersion, provider.DefaultCodexFingerprintVersion)
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

func TestProxyTreatsSuccessfulOAuthResponsesAsSSEWithoutUpstreamSSEHeader(t *testing.T) {
	const stream = "event: response.output_text.delta\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"OK\"}\n\n" +
		"event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-5.4-mini\",\"usage\":{\"input_tokens\":11,\"output_tokens\":2,\"total_tokens\":13,\"input_tokens_details\":{\"cached_tokens\":3},\"output_tokens_details\":{\"reasoning_tokens\":1}}}}\n\n"
	logger := &fakeRequestLogger{}
	pricer := &fakeUsagePricer{estimate: UsageCostEstimate{
		Matched:      true,
		CostMicrousd: 14,
		Snapshot: map[string]any{
			"matched": true,
			"model":   "gpt-5.4-mini",
		},
	}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/plain; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader(stream)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(
		&fakeAPIKeyAuthenticator{},
		&fakeSelectedAccountProvider{accounts: []SelectedAccount{{
			AccountID:          14,
			AccountType:        provider.AccountTypeCodexOAuth,
			AuthorizationToken: "oauth-access-token",
			ChatGPTAccountID:   "acct_chatgpt",
			DisplayName:        "free0",
		}}},
		Config{
			CodexResponsesBaseURL: "https://chatgpt.example.test/backend-api/codex",
			Logger:                logger,
			UsagePricer:           pricer,
		},
		client,
	)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.4-mini","input":"hi","stream":true}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	req.Header.Set("Content-Type", "application/json")
	recorder := &flushRecordingResponseWriter{}

	proxy.ServeHTTP(recorder, req)

	if recorder.status != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.status, recorder.body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	if recorder.body.String() != stream {
		t.Fatalf("stream body = %q, want exact upstream bytes", recorder.body.String())
	}
	if recorder.flushes == 0 {
		t.Fatal("flushes = 0, want streamed chunks to flush")
	}
	if len(logger.entries) != 1 {
		t.Fatalf("logged entries = %d, want 1", len(logger.entries))
	}
	entry := logger.entries[0]
	if entry.UsageSource != "stream" || entry.InputTokens != 11 || entry.OutputTokens != 2 || entry.TotalTokens != 13 || entry.CachedInputTokens != 3 || entry.ReasoningTokens != 1 {
		t.Fatalf("logged usage = %+v, want parsed Codex stream usage", entry)
	}
	if pricer.usage != (Usage{Model: "gpt-5.4-mini", InputTokens: 11, OutputTokens: 2, TotalTokens: 13, CachedInputTokens: 3, ReasoningTokens: 1, Source: "stream"}) {
		t.Fatalf("priced usage = %+v, want parsed Codex stream usage", pricer.usage)
	}
	if entry.EstimatedCostMicrousd != 14 || entry.PricingSnapshot["matched"] != true || entry.PricingSnapshot["model"] != "gpt-5.4-mini" {
		t.Fatalf("logged pricing = %d/%+v, want matched Codex stream pricing", entry.EstimatedCostMicrousd, entry.PricingSnapshot)
	}
}

func TestShouldStreamUpstreamResponseDoesNotRelabelErrorsOrAPIUpstreams(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	oauth := SelectedAccount{
		AccountType:      provider.AccountTypeCodexOAuth,
		ChatGPTAccountID: "acct_chatgpt",
	}
	plainSuccess := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/plain; charset=utf-8"}},
	}
	plainError := &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
	apiUpstream := SelectedAccount{AccountType: provider.AccountTypeAPIUpstream}

	if !shouldStreamUpstreamResponse(req, oauth, plainSuccess) {
		t.Fatal("successful Codex OAuth response should stream")
	}
	if shouldStreamUpstreamResponse(req, oauth, plainError) {
		t.Fatal("Codex OAuth error response should keep its error content type")
	}
	if shouldStreamUpstreamResponse(req, apiUpstream, plainSuccess) {
		t.Fatal("plain API upstream response should not be relabeled as SSE")
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
			defaultModel: "gpt-5",
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

func TestProxyLogsPreciseAPIKeyCostBudgetReason(t *testing.T) {
	logger := &fakeRequestLogger{}
	budgets := &fakeBudgetProvider{usage: admin.APIKeyBudgetUsage{CostBudgetExceeded: true}}
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "upstream-token"}}}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{key: admin.APIKey{ID: 42, Name: "budgeted key", CostBudgetMicrousd24h: 1}}, tokens, Config{
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
	if tokens.calls != 0 {
		t.Fatalf("account calls = %d, want 0 when key cost budget is exceeded", tokens.calls)
	}
	assertLastLoggedError(t, logger, "api_key_cost_budget_exceeded")
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

func TestProxyAcquiresGatewayAndAPIKeySlotsBeforeReadingRequestBody(t *testing.T) {
	tests := []struct {
		name       string
		config     Config
		occupySlot func(*Proxy) (func(), bool)
	}{
		{
			name:   "gateway",
			config: Config{MaxConcurrentGatewayRequests: 1},
			occupySlot: func(proxy *Proxy) (func(), bool) {
				return proxy.tryAcquireGatewaySlot(1)
			},
		},
		{
			name:   "api key",
			config: Config{MaxConcurrentRequestsPerKey: 1},
			occupySlot: func(proxy *Proxy) (func(), bool) {
				return proxy.tryAcquireAPIKeySlot(42, 1)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, &fakeSelectedAccountProvider{}, test.config, http.DefaultClient)
			release, ok := test.occupySlot(proxy)
			if !ok {
				t.Fatal("failed to occupy limiter slot")
			}
			defer release()
			body := &trackingReadCloser{reader: strings.NewReader(`{"model":"gpt-5"}`)}
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			req.Body = body
			req.ContentLength = -1
			req.Header.Set("Authorization", "Bearer n2api_client_secret")
			recorder := httptest.NewRecorder()

			proxy.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusTooManyRequests || body.reads != 0 {
				t.Fatalf("status=%d reads=%d body=%s, want 429 before body read", recorder.Code, body.reads, recorder.Body.String())
			}
		})
	}
}

func TestProxyReleasesAdmissionSlotsAfterInvalidJSONBody(t *testing.T) {
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, &fakeSelectedAccountProvider{}, Config{
		MaxConcurrentGatewayRequests: 1,
		MaxConcurrentRequestsPerKey:  1,
	}, http.DefaultClient)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want invalid JSON rejection", recorder.Code, recorder.Body.String())
	}
	releaseGateway, ok := proxy.tryAcquireGatewaySlot(1)
	if !ok {
		t.Fatal("gateway slot was not released after invalid JSON")
	}
	releaseGateway()
	releaseKey, ok := proxy.tryAcquireAPIKeySlot(42, 1)
	if !ok {
		t.Fatal("API key slot was not released after invalid JSON")
	}
	releaseKey()
}

func TestProxyArmsRequestBodyDeadlineAfterAdmissionAndClearsIt(t *testing.T) {
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, &fakeSelectedAccountProvider{}, Config{
		RequestBodyTimeout: 15 * time.Second,
	}, http.DefaultClient)
	deadlineActive := false
	deadlineCalls := 0
	proxy.setReadDeadline = func(_ http.ResponseWriter, deadline time.Time) error {
		deadlineCalls++
		deadlineActive = !deadline.IsZero()
		return nil
	}
	body := &trackingReadCloser{
		reader: strings.NewReader(`{"model":`),
		onRead: func() {
			if !deadlineActive {
				t.Fatal("request body was read before deadline was armed")
			}
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Body = body
	req.ContentLength = -1
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest || deadlineCalls != 2 || deadlineActive {
		t.Fatalf("status=%d deadlineCalls=%d active=%v", recorder.Code, deadlineCalls, deadlineActive)
	}
}

func TestProxyReturnsStableRequestBodyTimeoutAndReleasesSlots(t *testing.T) {
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, &fakeSelectedAccountProvider{}, Config{
		MaxConcurrentGatewayRequests: 1,
		MaxConcurrentRequestsPerKey:  1,
		RequestBodyTimeout:           time.Second,
	}, http.DefaultClient)
	proxy.setReadDeadline = func(http.ResponseWriter, time.Time) error { return nil }
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Body = &errorReadCloser{err: timeoutCanaryError{}}
	req.ContentLength = -1
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusRequestTimeout || !strings.Contains(recorder.Body.String(), `"code":"request_body_timeout"`) || strings.Contains(recorder.Body.String(), "timeout-address-canary") {
		t.Fatalf("status/body=%d/%s", recorder.Code, recorder.Body.String())
	}
	releaseGateway, gatewayOK := proxy.tryAcquireGatewaySlot(1)
	if gatewayOK {
		releaseGateway()
	}
	releaseKey, keyOK := proxy.tryAcquireAPIKeySlot(42, 1)
	if keyOK {
		releaseKey()
	}
	if !gatewayOK || !keyOK {
		t.Fatalf("slots not released after timeout: gateway=%v key=%v", gatewayOK, keyOK)
	}
}

func TestProxyDistinguishesBodyDeadlineFromEarlyCancellation(t *testing.T) {
	tests := []struct {
		name           string
		bodyTimeout    time.Duration
		readDelay      time.Duration
		wantStatus     int
		wantErrorCode  string
		wantEmptyReply bool
	}{
		{name: "body deadline", bodyTimeout: 5 * time.Millisecond, readDelay: 20 * time.Millisecond, wantStatus: http.StatusRequestTimeout, wantErrorCode: "request_body_timeout"},
		{name: "early cancellation", bodyTimeout: time.Second, wantStatus: http.StatusOK, wantErrorCode: "request_canceled", wantEmptyReply: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logger := &fakeRequestLogger{}
			proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, &fakeSelectedAccountProvider{}, Config{
				RequestBodyTimeout: test.bodyTimeout,
				Logger:             logger,
			}, http.DefaultClient)
			proxy.setReadDeadline = func(http.ResponseWriter, time.Time) error { return nil }
			ctx, cancel := context.WithCancel(context.Background())
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
			req.Body = &cancelingErrorReadCloser{cancel: cancel, delay: test.readDelay, err: timeoutCanaryError{}}
			req.ContentLength = -1
			req.Header.Set("Authorization", "Bearer n2api_client_secret")
			recorder := httptest.NewRecorder()

			proxy.ServeHTTP(recorder, req)

			if recorder.Code != test.wantStatus {
				t.Fatalf("status=%d body=%s, want %d", recorder.Code, recorder.Body.String(), test.wantStatus)
			}
			if test.wantEmptyReply {
				if recorder.Body.Len() != 0 {
					t.Fatalf("body=%q, want no response after client cancellation", recorder.Body.String())
				}
			} else if !strings.Contains(recorder.Body.String(), `"code":"request_body_timeout"`) {
				t.Fatalf("body=%q, want stable timeout error", recorder.Body.String())
			}
			if len(logger.entries) != 1 || logger.entries[0].Error != test.wantErrorCode {
				t.Fatalf("request logs=%+v, want error %q", logger.entries, test.wantErrorCode)
			}
		})
	}
}

func TestProxyClosesBlockingRequestBodyOnCancellation(t *testing.T) {
	logger := &fakeRequestLogger{}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, &fakeSelectedAccountProvider{}, Config{
		MaxConcurrentGatewayRequests: 1,
		MaxConcurrentRequestsPerKey:  1,
		Logger:                       logger,
	}, http.DefaultClient)
	var readDeadlines []time.Time
	proxy.setReadDeadline = func(_ http.ResponseWriter, deadline time.Time) error {
		readDeadlines = append(readDeadlines, deadline)
		return nil
	}
	body := newBlockingRequestBody()
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
	req.Body = body
	req.ContentLength = -1
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		proxy.ServeHTTP(recorder, req)
		close(done)
	}()

	<-body.started
	cancel()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("blocking request body did not stop after cancellation")
	}

	if !body.isClosed() || recorder.Body.Len() != 0 {
		t.Fatalf("closed/body=%v/%q, want closed body and no canceled-client response", body.isClosed(), recorder.Body.String())
	}
	if len(logger.entries) != 1 || logger.entries[0].Error != "request_canceled" || logger.entries[0].GatewayAttemptCount != 0 || logger.entries[0].GatewayFallbackCount != 0 {
		t.Fatalf("request log=%+v, want request_canceled before upstream attempt", logger.entries)
	}
	if len(readDeadlines) != 2 || readDeadlines[0].IsZero() || readDeadlines[1].IsZero() {
		t.Fatalf("read deadlines=%v, want initial and cancellation deadlines without clearing canceled connection", readDeadlines)
	}
	releaseGateway, gatewayOK := proxy.tryAcquireGatewaySlot(1)
	if gatewayOK {
		releaseGateway()
	}
	releaseKey, keyOK := proxy.tryAcquireAPIKeySlot(42, 1)
	if keyOK {
		releaseKey()
	}
	if !gatewayOK || !keyOK {
		t.Fatalf("slots not released after cancellation: gateway=%v key=%v", gatewayOK, keyOK)
	}
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

func TestProxyDoesNotCountFallbackForTerminalAccountConcurrencyLimit(t *testing.T) {
	logger := &fakeRequestLogger{}
	proxy := NewProxyWithClient(
		&fakeAPIKeyAuthenticator{},
		&fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 7, AuthorizationToken: "busy-token"}}},
		Config{
			MaxAcceptedRequestBodyBytes:     1024,
			MaxInMemoryReplayBodyBytes:      1,
			MaxConcurrentRequestsPerAccount: 1,
			Logger:                          logger,
		},
		http.DefaultClient,
	)
	release, ok := proxy.tryAcquireAccountSlot(7, 1)
	if !ok {
		t.Fatal("failed to occupy account slot")
	}
	defer release()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5"}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status/body=%d/%s, want provider account concurrency rejection", recorder.Code, recorder.Body.String())
	}
	if len(logger.entries) != 1 || logger.entries[0].GatewayAttemptCount != 1 || logger.entries[0].GatewayFallbackCount != 0 {
		t.Fatalf("request log=%+v, want attempts/fallbacks 1/0", logger.entries)
	}
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

func TestProxyRequestLogWriteFailureDoesNotChangeSuccessfulResponse(t *testing.T) {
	logger := &fakeRequestLogger{err: errors.New("database unavailable")}
	monitor := requestlog.NewWriteMonitor(nil)
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{
		AccountID: 1, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "token",
	}}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"resp_123","object":"response"}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, tokens, Config{
		UpstreamBaseURL:        "https://upstream.example.test",
		Logger:                 logger,
		RequestLogWriteMonitor: monitor,
	}, client)
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5"}`))
	request.Header.Set("Authorization", "Bearer n2api_client_secret")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", "request-log-failure-42")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"id":"resp_123"`) {
		t.Fatalf("response = %d %s, want unchanged success", recorder.Code, recorder.Body.String())
	}
	status := monitor.RequestLogWriteStatus()
	if status.LastFailedAt == nil || status.LastErrorCode != requestlog.WriteFailedErrorCode || status.ConsecutiveFailures != 1 || status.TotalFailures != 1 {
		t.Fatalf("request log write status = %+v", status)
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
	if !slices.Equal(tokens.used, []int64{1, 2}) || !slices.Equal(tokens.recovered, []int64{2}) {
		t.Fatalf("account attempts/recoveries = %+v/%+v, want [1 2]/[2]", tokens.used, tokens.recovered)
	}
}

func TestProxyConfirmsAccountRecoveryOnlyForSuccessfulUpstreamStatus(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		statusCode    int
		wantRecovered bool
	}{
		{name: "ok", statusCode: http.StatusOK, wantRecovered: true},
		{name: "no content", statusCode: http.StatusNoContent, wantRecovered: true},
		{name: "redirect", statusCode: http.StatusFound},
		{name: "client error", statusCode: http.StatusBadRequest},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 7, AuthorizationToken: "upstream-token"}}}
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: testCase.statusCode,
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

			if !slices.Equal(tokens.used, []int64{7}) {
				t.Fatalf("account attempts = %+v, want [7]", tokens.used)
			}
			if testCase.wantRecovered && !slices.Equal(tokens.recovered, []int64{7}) {
				t.Fatalf("recovered accounts = %+v, want [7]", tokens.recovered)
			}
			if !testCase.wantRecovered && len(tokens.recovered) != 0 {
				t.Fatalf("recovered accounts = %+v, want none for status %d", tokens.recovered, testCase.statusCode)
			}
		})
	}
}

func TestProxyKeepsSuccessfulUpstreamResponseWhenRecoveryPersistenceFails(t *testing.T) {
	tokens := &fakeSelectedAccountProvider{
		accounts:    []SelectedAccount{{AccountID: 7, AuthorizationToken: "upstream-token"}},
		recoveryErr: errors.New("recovery store unavailable"),
	}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
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

	if recorder.Code != http.StatusOK || recorder.Body.String() != `{"ok":true}` {
		t.Fatalf("response = %d %q, want successful upstream response", recorder.Code, recorder.Body.String())
	}
	if !slices.Equal(tokens.recovered, []int64{7}) {
		t.Fatalf("recovered accounts = %+v, want attempted recovery for account 7", tokens.recovered)
	}
}

func TestProxyRecordAccountRecoveredDetachesFromRequestCancellation(t *testing.T) {
	tokens := &fakeSelectedAccountProvider{}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, tokens, Config{UpstreamBaseURL: "https://upstream.example.test"}, http.DefaultClient)
	requestCtx, cancel := context.WithCancel(context.Background())
	cancel()

	proxy.recordAccountRecovered(requestCtx, 7)

	if !slices.Equal(tokens.recovered, []int64{7}) {
		t.Fatalf("recovered accounts = %+v, want account 7", tokens.recovered)
	}
	if tokens.recoveryContextErr != nil || !tokens.recoveryContextHasDeadline {
		t.Fatalf("recovery context err/deadline = %v/%t, want detached bounded context", tokens.recoveryContextErr, tokens.recoveryContextHasDeadline)
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
	if failure.accountID != 1 || failure.statusCode != http.StatusTooManyRequests || failure.retryAfter != "120" || failure.message != "upstream_rate_limited" {
		t.Fatalf("failure = %+v", failure)
	}
	if !slices.Equal(tokens.used, []int64{1, 2}) || !slices.Equal(tokens.recovered, []int64{2}) {
		t.Fatalf("account attempts/recoveries = %+v/%+v, want [1 2]/[2]", tokens.used, tokens.recovered)
	}
}

func TestCaptureFailureSeparatesRuleBodyFromPersistedSummary(t *testing.T) {
	const canary = "upstream-body-secret-canary"
	tests := []struct {
		name        string
		status      int
		body        string
		wantMessage string
	}{
		{
			name:        "json error",
			status:      http.StatusTooManyRequests,
			body:        `{"error":{"message":"rate limited ` + canary + `"}}`,
			wantMessage: "upstream_rate_limited",
		},
		{
			name:        "plain text error",
			status:      http.StatusBadGateway,
			body:        "temporary failure " + canary,
			wantMessage: "upstream_unavailable",
		},
		{
			name:        "endpoint permission error",
			status:      http.StatusForbidden,
			body:        `{"error":{"message":"missing scopes: api.responses.write ` + canary + `"}}`,
			wantMessage: "upstream_endpoint_permission_denied: missing scopes",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: test.status,
				Body:       io.NopCloser(strings.NewReader(test.body)),
			}
			message, ruleBody := captureFailure(resp)
			if message != test.wantMessage {
				t.Fatalf("message = %q, want %q", message, test.wantMessage)
			}
			if strings.Contains(message, canary) {
				t.Fatalf("persisted summary contains canary: %q", message)
			}
			if !strings.Contains(ruleBody, canary) {
				t.Fatalf("rule body = %q, want in-memory canary for matching", ruleBody)
			}
		})
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
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{
		{AccountID: 1, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "expired-token"},
		{AccountID: 2, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "second-token"},
	}}
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
	if len(tokens.authorizationRefreshes) != 0 {
		t.Fatalf("API upstream authorization refreshes = %+v, want none", tokens.authorizationRefreshes)
	}
}

func TestProxyRefreshesRejectedOAuthTokenAndRetriesSameAccountOnce(t *testing.T) {
	transportCalls := 0
	authorizations := []string{}
	tokens := &fakeSelectedAccountProvider{
		accounts:                    []SelectedAccount{{AccountID: 1, AccountType: provider.AccountTypeCodexOAuth, AuthorizationToken: "old-token"}},
		refreshedAuthorizationToken: "new-token",
		refreshAuthorizationRetry:   true,
	}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		transportCalls++
		authorizations = append(authorizations, r.Header.Get("Authorization"))
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
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if !slices.Equal(authorizations, []string{"Bearer old-token", "Bearer new-token"}) {
		t.Fatalf("upstream authorizations = %+v", authorizations)
	}
	if tokens.calls != 1 || len(tokens.authorizationRefreshes) != 1 || len(tokens.failures) != 0 {
		t.Fatalf("selection/refresh/failures = %d/%+v/%+v", tokens.calls, tokens.authorizationRefreshes, tokens.failures)
	}
	refresh := tokens.authorizationRefreshes[0]
	if refresh.accountID != 1 || refresh.rejectedAccessToken != "old-token" || refresh.statusCode != http.StatusUnauthorized {
		t.Fatalf("authorization refresh = %+v", refresh)
	}
	if !slices.Equal(tokens.used, []int64{1}) || !slices.Equal(tokens.recovered, []int64{1}) {
		t.Fatalf("account attempts/recoveries = %+v/%+v, want [1]/[1]", tokens.used, tokens.recovered)
	}
}

func TestProxyFallsBackAfterRefreshedOAuthTokenIsStillUnauthorized(t *testing.T) {
	transportCalls := 0
	tokens := &fakeSelectedAccountProvider{
		accounts: []SelectedAccount{
			{AccountID: 1, AccountType: provider.AccountTypeCodexOAuth, AuthorizationToken: "old-token"},
			{AccountID: 2, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "fallback-token"},
		},
		refreshedAuthorizationToken: "new-token",
		refreshAuthorizationRetry:   true,
	}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		transportCalls++
		if transportCalls <= 2 {
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"invalid access token"}}`)),
				Request:    r,
			}, nil
		}
		if got := r.Header.Get("Authorization"); got != "Bearer fallback-token" {
			t.Fatalf("fallback authorization = %q", got)
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
	if transportCalls != 3 || len(tokens.authorizationRefreshes) != 1 || len(tokens.failures) != 1 {
		t.Fatalf("transport/refresh/failures = %d/%+v/%+v", transportCalls, tokens.authorizationRefreshes, tokens.failures)
	}
	if len(tokens.exclusions) != 2 || !slices.Equal(tokens.exclusions[1], []int64{1}) {
		t.Fatalf("selection exclusions = %+v, want failed OAuth account excluded", tokens.exclusions)
	}
	if !slices.Equal(tokens.used, []int64{1, 2}) || !slices.Equal(tokens.recovered, []int64{2}) {
		t.Fatalf("account attempts/recoveries = %+v/%+v, want [1 2]/[2]", tokens.used, tokens.recovered)
	}
}

func TestProxyRecordsAccountFailureWhenOAuthAuthorizationRefreshFailsBeforeRecording(t *testing.T) {
	transportCalls := 0
	tokens := &fakeSelectedAccountProvider{
		accounts: []SelectedAccount{
			{AccountID: 1, AccountType: provider.AccountTypeCodexOAuth, AuthorizationToken: "old-token"},
			{AccountID: 2, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "fallback-token"},
		},
		refreshAuthorizationRetry: true,
		refreshAuthorizationErr:   errors.New("refresh failed"),
	}
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
		if got := r.Header.Get("Authorization"); got != "Bearer fallback-token" {
			t.Fatalf("fallback authorization = %q", got)
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
	if transportCalls != 2 || len(tokens.authorizationRefreshes) != 1 {
		t.Fatalf("transport/refresh = %d/%+v, want 2/1", transportCalls, tokens.authorizationRefreshes)
	}
	if len(tokens.failures) != 1 || tokens.failures[0].accountID != 1 || tokens.failures[0].statusCode != http.StatusUnauthorized {
		t.Fatalf("gateway account failures = %+v, want rejected authorization recorded", tokens.failures)
	}
	if len(tokens.exclusions) != 2 || !slices.Equal(tokens.exclusions[1], []int64{1}) {
		t.Fatalf("selection exclusions = %+v, want failed OAuth account excluded", tokens.exclusions)
	}
}

func TestProxyDoesNotDuplicatePersistedOAuthAuthorizationRefreshFailure(t *testing.T) {
	tokens := &fakeSelectedAccountProvider{
		accounts: []SelectedAccount{
			{AccountID: 1, AccountType: provider.AccountTypeCodexOAuth, AuthorizationToken: "old-token"},
		},
		refreshAuthorizationRetry: true,
		refreshFailureRecorded:    true,
		refreshAuthorizationErr:   errors.New("refresh rejected"),
	}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"invalid access token"}}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, tokens, Config{UpstreamBaseURL: "https://upstream.example.test"}, client)
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_123", nil)
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s, want 503", recorder.Code, recorder.Body.String())
	}
	if len(tokens.authorizationRefreshes) != 1 {
		t.Fatalf("authorization refreshes = %+v, want one", tokens.authorizationRefreshes)
	}
	if len(tokens.failures) != 0 {
		t.Fatalf("gateway account failures = %+v, want provider record preserved without duplication", tokens.failures)
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
	if len(tokens.recovered) != 0 {
		t.Fatalf("recovered accounts = %+v, want none after upstream 5xx", tokens.recovered)
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
	if !slices.Equal(tokens.used, []int64{1}) || len(tokens.recovered) != 0 {
		t.Fatalf("account attempts/recoveries = %+v/%+v, want [1]/none", tokens.used, tokens.recovered)
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
			defaultModel: "gpt-5-mini",
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
	requestBody := `{"model":"gpt-5","messages":[{"role":"user","content":"` + strings.Repeat("a", 128) + `"}]}`
	transportCalls := 0
	logger := &fakeRequestLogger{}
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AuthorizationToken: "first-token"}, {AccountID: 2, AuthorizationToken: "second-token"}}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		transportCalls++
		return nil, errors.New("upstream unavailable")
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, tokens, Config{
		UpstreamBaseURL:             "https://upstream.example.test",
		MaxAcceptedRequestBodyBytes: 1024,
		MaxInMemoryReplayBodyBytes:  64,
		Logger:                      logger,
	}, client)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(requestBody))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadGateway || !strings.Contains(recorder.Body.String(), "upstream_unavailable") {
		t.Fatalf("status/body = %d/%s, want one failed upstream attempt", recorder.Code, recorder.Body.String())
	}
	if tokens.calls != 1 || transportCalls != 1 {
		t.Fatalf("selection/transport calls = %d/%d, want 1/1", tokens.calls, transportCalls)
	}
	if len(logger.entries) != 1 || logger.entries[0].GatewayAttemptCount != 1 || logger.entries[0].GatewayFallbackCount != 0 {
		t.Fatalf("request log = %+v, want attempts 1 fallbacks 0", logger.entries)
	}
}

func TestProxyDoesNotAuthorizationRetryBodyAboveReplayLimit(t *testing.T) {
	requestBody := `{"model":"gpt-5","messages":[{"role":"user","content":"` + strings.Repeat("a", 128) + `"}]}`
	transportCalls := 0
	accounts := &fakeSelectedAccountProvider{
		accounts:                    []SelectedAccount{{AccountID: 1, AuthorizationToken: "rejected-token"}},
		refreshedAuthorizationToken: "refreshed-token",
		refreshAuthorizationRetry:   true,
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		transportCalls++
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"expired"}}`)),
			Request:    request,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, accounts, Config{
		MaxAcceptedRequestBodyBytes: 1024,
		MaxInMemoryReplayBodyBytes:  64,
	}, client)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(requestBody))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if transportCalls != 1 || len(accounts.authorizationRefreshes) != 0 {
		t.Fatalf("transport/refresh calls = %d/%d, want 1/0", transportCalls, len(accounts.authorizationRefreshes))
	}
}

func TestProxyRejectsContentLengthAboveAcceptedLimitBeforeReadingBody(t *testing.T) {
	body := &trackingReadCloser{reader: strings.NewReader(`{"model":"gpt-5"}`)}
	accounts := &fakeSelectedAccountProvider{}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, accounts, Config{
		MaxAcceptedRequestBodyBytes: 16,
		MaxInMemoryReplayBodyBytes:  8,
	}, http.DefaultClient)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Body = body
	req.ContentLength = 17
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusRequestEntityTooLarge || body.reads != 0 || accounts.calls != 0 {
		t.Fatalf("status=%d reads=%d accountCalls=%d body=%s", recorder.Code, body.reads, accounts.calls, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"code":"request_too_large"`) {
		t.Fatalf("body=%s, want stable request_too_large error", recorder.Body.String())
	}
}

func TestProxyRejectsChunkedBodyAboveAcceptedLimit(t *testing.T) {
	accounts := &fakeSelectedAccountProvider{}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, accounts, Config{
		MaxAcceptedRequestBodyBytes: 32,
		MaxInMemoryReplayBodyBytes:  16,
	}, http.DefaultClient)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(strings.Repeat("x", 33)))
	req.ContentLength = -1
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusRequestEntityTooLarge || accounts.calls != 0 || !strings.Contains(recorder.Body.String(), `"code":"request_too_large"`) {
		t.Fatalf("status=%d accountCalls=%d body=%s", recorder.Code, accounts.calls, recorder.Body.String())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type brokenReader struct {
	sent bool
}

type trackingReadCloser struct {
	reader io.Reader
	reads  int
	onRead func()
}

func (body *trackingReadCloser) Read(buffer []byte) (int, error) {
	body.reads++
	if body.onRead != nil {
		body.onRead()
	}
	return body.reader.Read(buffer)
}

func (body *trackingReadCloser) Close() error { return nil }

type countingReadCloser struct {
	reader    io.Reader
	bytesRead int
	closed    bool
}

func (body *countingReadCloser) Read(buffer []byte) (int, error) {
	count, err := body.reader.Read(buffer)
	body.bytesRead += count
	return count, err
}

func (body *countingReadCloser) Close() error {
	body.closed = true
	return nil
}

type errorReadCloser struct{ err error }

func (body *errorReadCloser) Read([]byte) (int, error) { return 0, body.err }
func (body *errorReadCloser) Close() error             { return nil }

type cancelingErrorReadCloser struct {
	cancel context.CancelFunc
	delay  time.Duration
	err    error
}

func (body *cancelingErrorReadCloser) Read([]byte) (int, error) {
	time.Sleep(body.delay)
	body.cancel()
	return 0, body.err
}

func (body *cancelingErrorReadCloser) Close() error { return nil }

type timeoutCanaryError struct{}

func (timeoutCanaryError) Error() string   { return "read tcp timeout-address-canary" }
func (timeoutCanaryError) Timeout() bool   { return true }
func (timeoutCanaryError) Temporary() bool { return true }

type blockingReadCloser struct {
	closed chan struct{}
	once   sync.Once
}

type blockingRequestBody struct {
	started   chan struct{}
	closed    chan struct{}
	startOnce sync.Once
	closeOnce sync.Once
}

func newBlockingRequestBody() *blockingRequestBody {
	return &blockingRequestBody{started: make(chan struct{}), closed: make(chan struct{})}
}

func (body *blockingRequestBody) Read([]byte) (int, error) {
	body.startOnce.Do(func() { close(body.started) })
	<-body.closed
	return 0, errors.New("request body closed")
}

func (body *blockingRequestBody) Close() error {
	body.closeOnce.Do(func() { close(body.closed) })
	return nil
}

func (body *blockingRequestBody) isClosed() bool {
	select {
	case <-body.closed:
		return true
	default:
		return false
	}
}

func newBlockingReadCloser() *blockingReadCloser {
	return &blockingReadCloser{closed: make(chan struct{})}
}

func (body *blockingReadCloser) Read([]byte) (int, error) {
	<-body.closed
	return 0, errors.New("stream closed")
}

func (body *blockingReadCloser) Close() error {
	body.once.Do(func() { close(body.closed) })
	return nil
}

func (body *blockingReadCloser) isClosed() bool {
	select {
	case <-body.closed:
		return true
	default:
		return false
	}
}

type delayedChunkReadCloser struct {
	chunks [][]byte
	delay  time.Duration
	index  int
}

func (body *delayedChunkReadCloser) Read(buffer []byte) (int, error) {
	if body.index >= len(body.chunks) {
		return 0, io.EOF
	}
	time.Sleep(body.delay)
	n := copy(buffer, body.chunks[body.index])
	body.index++
	return n, nil
}

func (*delayedChunkReadCloser) Close() error { return nil }

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

func TestProxyAppliesSelectedAccountFingerprintHeaders(t *testing.T) {
	var gotUserAgent string
	var gotHeader string
	var gotTLS string
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{{
		AccountID:          1,
		AuthorizationToken: "upstream-token",
		FingerprintUA:      "N2API-Fingerprint/1.0",
		FingerprintTLS:     "chrome",
		FingerprintHeaders: map[string]string{"X-Fingerprint-Test": "enabled"},
	}}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotUserAgent = r.Header.Get("User-Agent")
		gotHeader = r.Header.Get("X-Fingerprint-Test")
		gotTLS = tlsFingerprintFromContext(r.Context())
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, tokens, Config{UpstreamBaseURL: "https://upstream.example.test"}, client)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if gotUserAgent != "N2API-Fingerprint/1.0" {
		t.Fatalf("User-Agent = %q, want fingerprint override", gotUserAgent)
	}
	if gotHeader != "enabled" {
		t.Fatalf("X-Fingerprint-Test = %q, want fingerprint custom header", gotHeader)
	}
	if gotTLS != "chrome" {
		t.Fatalf("TLS fingerprint = %q, want chrome", gotTLS)
	}
}

func TestProxyPassesThroughMatchingRetryableUpstreamError(t *testing.T) {
	transportCalls := 0
	rules := &fakeErrorPassthroughRuleProvider{rules: []admin.ErrorPassthroughRule{{
		Pattern:   "insufficient_quota",
		MatchType: "error_code",
		Enabled:   true,
		Priority:  1,
	}}}
	tokens := &fakeSelectedAccountProvider{accounts: []SelectedAccount{
		{AccountID: 1, AuthorizationToken: "first-token"},
		{AccountID: 2, AuthorizationToken: "second-token"},
	}}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		transportCalls++
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{"Content-Type": []string{"application/json"}, "Retry-After": []string{"60"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":{"code":"insufficient_quota","message":"quota exceeded"}}`)),
			Request:    r,
		}, nil
	})}
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, tokens, Config{
		UpstreamBaseURL:               "https://upstream.example.test",
		ErrorPassthroughRulesProvider: rules,
	}, client)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5","messages":[]}`))
	req.Header.Set("Authorization", "Bearer n2api_client_secret")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d body=%s, want upstream 429", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "insufficient_quota") {
		t.Fatalf("body = %q, want upstream error body", recorder.Body.String())
	}
	if transportCalls != 1 {
		t.Fatalf("transport calls = %d, want no fallback retry", transportCalls)
	}
	if tokens.calls != 1 {
		t.Fatalf("account selections = %d, want one selected account", tokens.calls)
	}
	if rules.calls != 1 {
		t.Fatalf("rule lookups = %d, want one lookup", rules.calls)
	}
	if !slices.Equal(tokens.used, []int64{1}) || len(tokens.recovered) != 0 {
		t.Fatalf("account attempts/recoveries = %+v/%+v, want [1]/none", tokens.used, tokens.recovered)
	}
}
