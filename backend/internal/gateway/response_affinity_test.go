package gateway

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestResponseAffinityKnownRoutesStayOnOriginalAccount(t *testing.T) {
	for _, path := range []string{
		"/v1/responses/resp_known",
		"/v1/responses/resp_known/input_items",
	} {
		t.Run(path, func(t *testing.T) {
			store := newMemoryResponseAffinityStore()
			store.affinities[affinityTestKey("resp_known", 1)] = ResponseAffinity{ProviderAccountID: 11}
			accounts := newAffinityTestAccountProvider()
			accounts.exact = SelectedAccount{AccountID: 11, AccountType: "api_upstream", AuthorizationToken: "token-a"}
			var authorization string
			proxy := newAffinityTestProxy(store, accounts, roundTripFunc(func(req *http.Request) (*http.Response, error) {
				authorization = req.Header.Get("Authorization")
				return affinityJSONResponse(http.StatusOK, `{"id":"resp_known","object":"response"}`), nil
			}), nil)

			recorder := performAffinityRequest(proxy, http.MethodGet, path, "")
			if recorder.Code != http.StatusOK || authorization != "Bearer token-a" {
				t.Fatalf("status/authorization = %d/%q, want 200/Bearer token-a", recorder.Code, authorization)
			}
			if len(accounts.exactCalls) != 1 || accounts.exactCalls[0].accountID != 11 || accounts.exactCalls[0].routingPoolID != 1 {
				t.Fatalf("exact affinity calls = %+v", accounts.exactCalls)
			}
			if accounts.fakeSelectedAccountProvider.calls != 0 {
				t.Fatalf("ordinary selection calls = %d, want 0", accounts.fakeSelectedAccountProvider.calls)
			}
		})
	}
}

func TestResponseAffinityKeepsResponsesOnEachCreatingAccount(t *testing.T) {
	store := newMemoryResponseAffinityStore()
	accounts := newAffinityTestAccountProvider()
	accounts.fakeSelectedAccountProvider.accounts = []SelectedAccount{
		{AccountID: 11, AccountType: "api_upstream", AuthorizationToken: "token-a"},
		{AccountID: 22, AccountType: "api_upstream", AuthorizationToken: "token-b"},
	}
	createdByAuthorization := map[string]string{
		"Bearer token-a": "resp_account_a",
		"Bearer token-b": "resp_account_b",
	}
	proxy := newAffinityTestProxy(store, accounts, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		responseID := createdByAuthorization[req.Header.Get("Authorization")]
		if responseID == "" {
			t.Fatalf("unexpected authorization %q", req.Header.Get("Authorization"))
		}
		return affinityJSONResponse(http.StatusOK, `{"id":"`+responseID+`","object":"response"}`), nil
	}), nil)

	for _, responseID := range []string{"resp_account_a", "resp_account_b"} {
		recorder := performAffinityRequest(proxy, http.MethodPost, "/v1/responses", `{"model":"gpt-test","input":"create"}`)
		if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), responseID) {
			t.Fatalf("create %s = %d %s", responseID, recorder.Code, recorder.Body.String())
		}
	}

	for _, testCase := range []struct {
		responseID string
		accountID  int64
		token      string
	}{
		{responseID: "resp_account_a", accountID: 11, token: "token-a"},
		{responseID: "resp_account_b", accountID: 22, token: "token-b"},
	} {
		accounts.exact = SelectedAccount{AccountID: testCase.accountID, AccountType: "api_upstream", AuthorizationToken: testCase.token}
		recorder := performAffinityRequest(proxy, http.MethodGet, "/v1/responses/"+testCase.responseID, "")
		if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), testCase.responseID) {
			t.Fatalf("read %s = %d %s", testCase.responseID, recorder.Code, recorder.Body.String())
		}
	}
}

func TestPreviousResponseAffinityForcesOriginalAccount(t *testing.T) {
	store := newMemoryResponseAffinityStore()
	store.affinities[affinityTestKey("resp_parent", 1)] = ResponseAffinity{ProviderAccountID: 11}
	accounts := newAffinityTestAccountProvider()
	accounts.exact = SelectedAccount{AccountID: 11, AccountType: "api_upstream", AuthorizationToken: "token-a"}
	var authorization string
	proxy := newAffinityTestProxy(store, accounts, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		authorization = req.Header.Get("Authorization")
		return affinityJSONResponse(http.StatusOK, `{"id":"resp_child","object":"response"}`), nil
	}), nil)

	recorder := performAffinityRequest(proxy, http.MethodPost, "/v1/responses", `{"model":"gpt-test","previous_response_id":"resp_parent","input":"next"}`)
	if recorder.Code != http.StatusOK || authorization != "Bearer token-a" {
		t.Fatalf("status/authorization = %d/%q", recorder.Code, authorization)
	}
	if len(store.writes) != 1 || store.writes[0].responseID != "resp_child" || store.writes[0].providerAccountID != 11 || store.writes[0].routingPoolID != 1 {
		t.Fatalf("affinity writes = %+v", store.writes)
	}
}

func TestUnknownResponseAffinityFailsClosedWithMultipleAccounts(t *testing.T) {
	store := newMemoryResponseAffinityStore()
	accounts := newAffinityTestAccountProvider()
	accounts.unique = false
	transportCalls := 0
	proxy := newAffinityTestProxy(store, accounts, roundTripFunc(func(*http.Request) (*http.Response, error) {
		transportCalls++
		return affinityJSONResponse(http.StatusOK, `{}`), nil
	}), nil)

	recorder := performAffinityRequest(proxy, http.MethodGet, "/v1/responses/resp_unknown", "")
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "response_affinity_unknown") {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
	if transportCalls != 0 || accounts.fakeSelectedAccountProvider.calls != 0 {
		t.Fatalf("transport/ordinary selection calls = %d/%d, want 0/0", transportCalls, accounts.fakeSelectedAccountProvider.calls)
	}
}

func TestUnknownResponseAffinityUsesSingleAccountCompatibility(t *testing.T) {
	store := newMemoryResponseAffinityStore()
	accounts := newAffinityTestAccountProvider()
	accounts.unique = true
	accounts.single = SelectedAccount{AccountID: 22, AccountType: "api_upstream", AuthorizationToken: "token-only"}
	proxy := newAffinityTestProxy(store, accounts, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return affinityJSONResponse(http.StatusOK, `{"id":"resp_unknown","object":"response"}`), nil
	}), nil)

	recorder := performAffinityRequest(proxy, http.MethodGet, "/v1/responses/resp_unknown", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
	if len(store.writes) != 1 || store.writes[0].providerAccountID != 22 {
		t.Fatalf("compatibility affinity writes = %+v", store.writes)
	}
}

func TestResponseAffinityPersistsFinalFallbackAccount(t *testing.T) {
	store := newMemoryResponseAffinityStore()
	accounts := newAffinityTestAccountProvider()
	accounts.fakeSelectedAccountProvider.accounts = []SelectedAccount{
		{AccountID: 11, AccountType: "api_upstream", AuthorizationToken: "token-a"},
		{AccountID: 22, AccountType: "api_upstream", AuthorizationToken: "token-b"},
	}
	transportCalls := 0
	proxy := newAffinityTestProxy(store, accounts, roundTripFunc(func(*http.Request) (*http.Response, error) {
		transportCalls++
		if transportCalls == 1 {
			return affinityJSONResponse(http.StatusServiceUnavailable, `{"error":{"message":"retry"}}`), nil
		}
		return affinityJSONResponse(http.StatusOK, `{"id":"resp_fallback","object":"response"}`), nil
	}), nil)

	recorder := performAffinityRequest(proxy, http.MethodPost, "/v1/responses", `{"model":"gpt-test","input":"hello"}`)
	if recorder.Code != http.StatusOK || transportCalls != 2 {
		t.Fatalf("response/transport calls = %d/%d body=%s", recorder.Code, transportCalls, recorder.Body.String())
	}
	if len(store.writes) != 1 || store.writes[0].responseID != "resp_fallback" || store.writes[0].providerAccountID != 22 {
		t.Fatalf("fallback affinity writes = %+v", store.writes)
	}
}

func TestResponseAffinityExtractsStreamingResponseID(t *testing.T) {
	store := newMemoryResponseAffinityStore()
	accounts := newAffinityTestAccountProvider()
	accounts.fakeSelectedAccountProvider.accounts = []SelectedAccount{{AccountID: 11, AccountType: "api_upstream", AuthorizationToken: "token-a"}}
	proxy := newAffinityTestProxy(store, accounts, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(
				"event: response.created\n" +
					"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_stream\"}}\n\n" +
					"event: response.completed\n" +
					"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_stream\"}}\n\n")),
		}, nil
	}), nil)

	recorder := performAffinityRequest(proxy, http.MethodPost, "/v1/responses", `{"model":"gpt-test","input":"hello","stream":true}`)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "response.completed") {
		t.Fatalf("stream response = %d %s", recorder.Code, recorder.Body.String())
	}
	if len(store.writes) != 1 || store.writes[0].responseID != "resp_stream" || store.writes[0].providerAccountID != 11 {
		t.Fatalf("stream affinity writes = %+v", store.writes)
	}
}

func TestResponseAffinityPersistsStreamingResponseIDAfterReadError(t *testing.T) {
	store := newMemoryResponseAffinityStore()
	accounts := newAffinityTestAccountProvider()
	accounts.fakeSelectedAccountProvider.accounts = []SelectedAccount{{AccountID: 11, AccountType: "api_upstream", AuthorizationToken: "token-a"}}
	proxy := newAffinityTestProxy(store, accounts, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: &responseCreatedThenErrorReader{payload: []byte(
				"event: response.created\n" +
					"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_stream_error\"}}\n\n"),
				err: errors.New("stream failed after response creation")},
		}, nil
	}), nil)

	recorder := performAffinityRequest(proxy, http.MethodPost, "/v1/responses", `{"model":"gpt-test","input":"hello","stream":true}`)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "resp_stream_error") {
		t.Fatalf("stream response = %d %s", recorder.Code, recorder.Body.String())
	}
	if len(store.writes) != 1 || store.writes[0].responseID != "resp_stream_error" || store.writes[0].providerAccountID != 11 {
		t.Fatalf("stream error affinity writes = %+v", store.writes)
	}
}

func TestResponseAffinityExtractsChunkedCRLFStreamingResponseID(t *testing.T) {
	observer := NewSSEUsageObserver("/v1/responses")
	chunks := []string{
		"event: response.cre",
		"ated\r\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_crlf\"}}\r\n",
		"\r\n",
	}
	for _, chunk := range chunks {
		observer.Observe([]byte(chunk))
	}
	if observer.ResponseID() != "resp_crlf" {
		t.Fatalf("CRLF streaming response ID = %q, want resp_crlf", observer.ResponseID())
	}
}

func TestResponseAffinityWriteFailureDoesNotChangeResponseOrLeakID(t *testing.T) {
	store := newMemoryResponseAffinityStore()
	store.writeErr = errors.New("database unavailable")
	accounts := newAffinityTestAccountProvider()
	accounts.fakeSelectedAccountProvider.accounts = []SelectedAccount{{AccountID: 11, AccountType: "api_upstream", AuthorizationToken: "token-a"}}
	var processLog bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&processLog, nil))
	proxy := newAffinityTestProxy(store, accounts, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return affinityJSONResponse(http.StatusOK, `{"id":"resp_secret_canary","object":"response"}`), nil
	}), logger)

	recorder := performAffinityRequest(proxy, http.MethodPost, "/v1/responses", `{"model":"gpt-test","input":"hello"}`)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "resp_secret_canary") {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(processLog.String(), "response_affinity_write_failed") || strings.Contains(processLog.String(), "resp_secret_canary") || strings.Contains(processLog.String(), "database unavailable") {
		t.Fatalf("unsafe or incomplete process log = %q", processLog.String())
	}
}

func TestKnownResponseAffinityAccountFailureDoesNotFallback(t *testing.T) {
	store := newMemoryResponseAffinityStore()
	store.affinities[affinityTestKey("resp_known", 1)] = ResponseAffinity{ProviderAccountID: 11}
	accounts := newAffinityTestAccountProvider()
	accounts.exactErr = errors.New("account disabled")
	accounts.fakeSelectedAccountProvider.accounts = []SelectedAccount{{AccountID: 22, AuthorizationToken: "token-b"}}
	proxy := newAffinityTestProxy(store, accounts, roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("transport must not run when affinity account is unavailable")
		return nil, nil
	}), nil)

	recorder := performAffinityRequest(proxy, http.MethodGet, "/v1/responses/resp_known", "")
	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), "response_affinity_account_unavailable") {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
	if accounts.fakeSelectedAccountProvider.calls != 0 {
		t.Fatalf("ordinary fallback selection calls = %d, want 0", accounts.fakeSelectedAccountProvider.calls)
	}
}

type affinityTestAccountProvider struct {
	*fakeSelectedAccountProvider
	exact      SelectedAccount
	exactErr   error
	exactCalls []affinityExactCall
	single     SelectedAccount
	unique     bool
	singleErr  error
}

type responseCreatedThenErrorReader struct {
	payload []byte
	err     error
	sent    bool
}

func (r *responseCreatedThenErrorReader) Read(buffer []byte) (int, error) {
	if !r.sent {
		r.sent = true
		return copy(buffer, r.payload), nil
	}
	return 0, r.err
}

func (*responseCreatedThenErrorReader) Close() error { return nil }

type affinityExactCall struct {
	routingPoolID int64
	accountID     int64
	model         string
}

func newAffinityTestAccountProvider() *affinityTestAccountProvider {
	return &affinityTestAccountProvider{fakeSelectedAccountProvider: &fakeSelectedAccountProvider{}}
}

func (p *affinityTestAccountProvider) SelectAccountByIDInRoutingPoolChain(_ context.Context, routingPoolID, accountID int64, model string) (SelectedAccount, error) {
	p.exactCalls = append(p.exactCalls, affinityExactCall{routingPoolID: routingPoolID, accountID: accountID, model: model})
	return p.exact, p.exactErr
}

func (p *affinityTestAccountProvider) SelectSingleAccountInRoutingPoolChain(_ context.Context, _ int64, _ string) (SelectedAccount, bool, error) {
	return p.single, p.unique, p.singleErr
}

type memoryResponseAffinityStore struct {
	mu         sync.Mutex
	affinities map[string]ResponseAffinity
	findErr    error
	writeErr   error
	writes     []affinityWrite
}

type affinityWrite struct {
	responseID        string
	providerAccountID int64
	routingPoolID     int64
	expiresAt         time.Time
}

func newMemoryResponseAffinityStore() *memoryResponseAffinityStore {
	return &memoryResponseAffinityStore{affinities: make(map[string]ResponseAffinity)}
}

func (s *memoryResponseAffinityStore) FindResponseAffinity(_ context.Context, responseID string, routingPoolID int64, _ time.Time) (ResponseAffinity, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.findErr != nil {
		return ResponseAffinity{}, false, s.findErr
	}
	affinity, ok := s.affinities[affinityTestKey(responseID, routingPoolID)]
	return affinity, ok, nil
}

func (s *memoryResponseAffinityStore) UpsertResponseAffinity(_ context.Context, responseID string, providerAccountID, routingPoolID int64, expiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writeErr != nil {
		return s.writeErr
	}
	s.affinities[affinityTestKey(responseID, routingPoolID)] = ResponseAffinity{ProviderAccountID: providerAccountID}
	s.writes = append(s.writes, affinityWrite{responseID: responseID, providerAccountID: providerAccountID, routingPoolID: routingPoolID, expiresAt: expiresAt})
	return nil
}

func affinityTestKey(responseID string, routingPoolID int64) string {
	return responseID + ":" + strconv.FormatInt(routingPoolID, 10)
}

func newAffinityTestProxy(store ResponseAffinityStore, accounts AccountProvider, transport http.RoundTripper, logger *slog.Logger) *Proxy {
	return NewProxyWithClient(
		&fakeAPIKeyAuthenticator{},
		accounts,
		Config{UpstreamBaseURL: "https://upstream.example.test", ResponseAffinityStore: store, ProcessLogger: logger},
		&http.Client{Transport: transport},
	)
}

func performAffinityRequest(proxy *Proxy, method, path, body string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", "Bearer client-key")
	recorder := httptest.NewRecorder()
	proxy.ServeHTTP(recorder, req)
	return recorder
}

func affinityJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
