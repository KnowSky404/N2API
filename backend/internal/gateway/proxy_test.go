package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/KnowSky404/N2API/backend/internal/provider"
)

type fakeAPIKeyAuthenticator struct {
	gotKey string
	err    error
}

func (a *fakeAPIKeyAuthenticator) AuthenticateAPIKey(_ context.Context, apiKey string) (admin.APIKey, error) {
	a.gotKey = apiKey
	if a.err != nil {
		return admin.APIKey{}, a.err
	}
	return admin.APIKey{ID: 1, Name: "test key"}, nil
}

type fakeAccessTokenProvider struct {
	token string
	err   error
}

func (p fakeAccessTokenProvider) AccessToken(_ context.Context) (string, error) {
	if p.err != nil {
		return "", p.err
	}
	return p.token, nil
}

func TestProxyRequiresBearerAPIKey(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("upstream should not be called without client API key")
	}))
	defer upstream.Close()
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, fakeAccessTokenProvider{token: "upstream-token"}, Config{
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

func TestProxyForwardsModelsWithProviderBearerToken(t *testing.T) {
	var gotAuthorization string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %q, want /v1/models", r.URL.Path)
		}
		gotAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	defer upstream.Close()
	auth := &fakeAPIKeyAuthenticator{}
	proxy := NewProxy(auth, fakeAccessTokenProvider{token: "upstream-token"}, Config{
		UpstreamBaseURL: upstream.URL,
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
	if gotAuthorization != "Bearer upstream-token" {
		t.Fatalf("upstream Authorization = %q, want provider token", gotAuthorization)
	}
	if recorder.Body.String() != `{"object":"list","data":[]}` {
		t.Fatalf("body = %q", recorder.Body.String())
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
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, fakeAccessTokenProvider{token: "upstream-token"}, Config{
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

func TestProxyReportsMissingProviderAccount(t *testing.T) {
	proxy := NewProxy(&fakeAPIKeyAuthenticator{}, fakeAccessTokenProvider{err: provider.ErrNotConnected}, Config{
		UpstreamBaseURL: "https://api.example.test",
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
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
