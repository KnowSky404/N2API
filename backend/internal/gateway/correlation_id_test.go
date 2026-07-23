package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

type correlationAuth struct {
	mu    sync.Mutex
	event systemevent.Event
}

func (a *correlationAuth) AuthenticateAPIKey(ctx context.Context, _ string) (admin.APIKey, error) {
	event := systemevent.BuildEvent(ctx, systemevent.EventIntent{
		Category: systemevent.CategoryRuntime,
		Severity: systemevent.SeverityInfo,
		Action:   systemevent.ActionProviderAccountRecovered,
		Outcome:  systemevent.OutcomeSuccess,
		Target:   systemevent.Target{Type: "provider_account", ID: "1", Name: "test"},
	}, systemevent.Target{}, time.Now(), 0)
	a.mu.Lock()
	a.event = event
	a.mu.Unlock()
	poolID := int64(1)
	return admin.APIKey{ID: 42, RoutingPoolID: &poolID}, nil
}

type correlationAccounts struct{}

func (correlationAccounts) SelectAccountForModel(context.Context, string, ...int64) (SelectedAccount, error) {
	return SelectedAccount{AccountID: 1, AccountType: "api_upstream", AuthorizationToken: "upstream-token"}, nil
}

func (p correlationAccounts) SelectAccountForModelInRoutingPool(ctx context.Context, _ int64, model string, excluded ...int64) (SelectedAccount, error) {
	return p.SelectAccountForModel(ctx, model, excluded...)
}

func (p correlationAccounts) SelectAccountForModelAndSessionInRoutingPool(ctx context.Context, _ int64, model, _ string, excluded ...int64) (SelectedAccount, error) {
	return p.SelectAccountForModel(ctx, model, excluded...)
}

type synchronizedRequestLogger struct {
	mu      sync.Mutex
	entries []RequestLog
}

func (l *synchronizedRequestLogger) CreateRequestLog(_ context.Context, entry RequestLog) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, entry)
	return nil
}

func (l *synchronizedRequestLogger) snapshot() []RequestLog {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]RequestLog(nil), l.entries...)
}

func TestProxyUsesOneCorrelationIDAcrossResponseUpstreamLogAndEvent(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		incoming string
		want     string
	}{
		{name: "valid", incoming: "caller.request-42", want: "caller.request-42"},
		{name: "invalid", incoming: "bad request\nvalue"},
		{name: "missing"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			auth := &correlationAuth{}
			logger := &synchronizedRequestLogger{}
			upstreamRequestID := ""
			proxy := NewProxyWithClient(auth, correlationAccounts{}, Config{
				UpstreamBaseURL: "https://upstream.example.test",
				Logger:          logger,
			}, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				upstreamRequestID = req.Header.Get("X-Request-ID")
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"object":"list","data":[]}`))}, nil
			})})
			req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_test", nil)
			req.Header.Set("Authorization", "Bearer client-key")
			if testCase.incoming != "" {
				req.Header.Set("X-Request-ID", testCase.incoming)
			}
			recorder := httptest.NewRecorder()

			proxy.ServeHTTP(recorder, req)

			responseRequestID := recorder.Header().Get("X-Request-ID")
			entries := logger.snapshot()
			if len(entries) != 1 {
				t.Fatalf("request log entries = %d, want 1", len(entries))
			}
			if !systemevent.ValidCorrelationID(responseRequestID) {
				t.Fatalf("response request ID = %q, want valid", responseRequestID)
			}
			if testCase.want != "" && responseRequestID != testCase.want {
				t.Fatalf("response request ID = %q, want %q", responseRequestID, testCase.want)
			}
			if responseRequestID == testCase.incoming && testCase.want == "" && testCase.incoming != "" {
				t.Fatalf("invalid incoming request ID was preserved: %q", responseRequestID)
			}
			if upstreamRequestID != responseRequestID || entries[0].RequestID != responseRequestID || auth.event.CorrelationID != responseRequestID {
				t.Fatalf("correlation IDs = response:%q upstream:%q log:%q event:%q", responseRequestID, upstreamRequestID, entries[0].RequestID, auth.event.CorrelationID)
			}
		})
	}
}

func TestProxyPreservesCorrelationIDAcrossFallbackAttempts(t *testing.T) {
	logger := &synchronizedRequestLogger{}
	accounts := &fakeSelectedAccountProvider{accounts: []SelectedAccount{
		{AccountID: 1, AccountType: "api_upstream", AuthorizationToken: "first"},
		{AccountID: 2, AccountType: "api_upstream", AuthorizationToken: "second"},
	}}
	var requestIDs []string
	proxy := NewProxyWithClient(&fakeAPIKeyAuthenticator{}, accounts, Config{
		UpstreamBaseURL: "https://upstream.example.test",
		Logger:          logger,
	}, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestIDs = append(requestIDs, req.Header.Get("X-Request-ID"))
		status := http.StatusServiceUnavailable
		if len(requestIDs) == 2 {
			status = http.StatusOK
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"object":"list","data":[]}`))}, nil
	})})
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_test", nil)
	req.Header.Set("Authorization", "Bearer client-key")
	req.Header.Set("X-Request-ID", "fallback-request-7")
	recorder := httptest.NewRecorder()

	proxy.ServeHTTP(recorder, req)

	entries := logger.snapshot()
	if len(requestIDs) != 2 || requestIDs[0] != "fallback-request-7" || requestIDs[1] != requestIDs[0] || len(entries) != 1 || entries[0].RequestID != requestIDs[0] {
		t.Fatalf("fallback correlation = attempts:%+v logs:%+v", requestIDs, entries)
	}
}

func TestProxyCorrelationIDsDoNotCrossConcurrentRequests(t *testing.T) {
	logger := &synchronizedRequestLogger{}
	upstreamIDs := map[string]struct{}{}
	var upstreamMu sync.Mutex
	proxy := NewProxyWithClient(&correlationAuth{}, correlationAccounts{}, Config{
		UpstreamBaseURL: "https://upstream.example.test",
		Logger:          logger,
	}, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		upstreamMu.Lock()
		upstreamIDs[req.Header.Get("X-Request-ID")] = struct{}{}
		upstreamMu.Unlock()
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"object":"list","data":[]}`))}, nil
	})})

	const requests = 24
	var wg sync.WaitGroup
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/v1/responses/resp_test", nil)
			req.Header.Set("Authorization", "Bearer client-key")
			proxy.ServeHTTP(httptest.NewRecorder(), req)
		}()
	}
	wg.Wait()

	entries := logger.snapshot()
	if len(entries) != requests || len(upstreamIDs) != requests {
		t.Fatalf("unique concurrent IDs = logs:%d upstream:%d, want %d", len(entries), len(upstreamIDs), requests)
	}
	seen := make(map[string]struct{}, requests)
	for _, entry := range entries {
		if !systemevent.ValidCorrelationID(entry.RequestID) {
			t.Fatalf("invalid concurrent request ID %q", entry.RequestID)
		}
		seen[entry.RequestID] = struct{}{}
	}
	if len(seen) != requests {
		t.Fatalf("unique request log IDs = %d, want %d", len(seen), requests)
	}
}
