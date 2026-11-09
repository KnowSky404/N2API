package provider

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestHTTPClientProbeAccountModelValidatesAPIUpstreamJSON(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantCode   string
	}{
		{name: "valid", statusCode: http.StatusOK, body: `{"choices":[]}`},
		{name: "invalid json", statusCode: http.StatusOK, body: `not-json`, wantCode: "invalid_response"},
		{name: "missing choices", statusCode: http.StatusOK, body: `{"object":"chat.completion"}`, wantCode: "invalid_response"},
		{name: "error envelope with success status", statusCode: http.StatusOK, body: `{"error":{"message":"model failed"}}`, wantCode: "invalid_response"},
		{name: "model missing", statusCode: http.StatusNotFound, body: `{"error":{"message":"model not found"}}`, wantCode: "model_not_found"},
		{name: "rate limited", statusCode: http.StatusTooManyRequests, body: `{"error":{"message":"slow down"}}`, wantCode: "rate_limited"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var gotPath string
			var gotAuthorization string
			var gotUserAgent string
			var gotHeader string
			client := NewHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				gotPath = r.URL.Path
				gotAuthorization = r.Header.Get("Authorization")
				gotUserAgent = r.Header.Get("User-Agent")
				gotHeader = r.Header.Get("X-Fingerprint")
				for _, name := range []string{"Originator", "Version", "ChatGPT-Account-ID", "OpenAI-Beta"} {
					if value := r.Header.Get(name); value != "" {
						t.Fatalf("unexpected identity header %s=%q", name, value)
					}
				}
				return &http.Response{
					StatusCode: test.statusCode,
					Body:       io.NopCloser(strings.NewReader(test.body)),
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Request:    r,
				}, nil
			})})

			result := client.ProbeAccountModel(context.Background(), Config{}, SelectedAccount{
				AccountType:        AccountTypeAPIUpstream,
				AuthorizationToken: "api-secret",
				BaseURL:            "https://upstream.example.test/v1",
			}, "gpt-test")

			if result.statusCode != test.statusCode || result.errorCode != test.wantCode {
				t.Fatalf("ProbeAccountModel result = %+v, want status=%d code=%q", result, test.statusCode, test.wantCode)
			}
			if gotPath != "/v1/chat/completions" || gotAuthorization != "Bearer api-secret" || strings.Contains(gotUserAgent, "codex") || gotHeader != "" {
				t.Fatalf("request path=%q auth=%q ua=%q fingerprint=%q", gotPath, gotAuthorization, gotUserAgent, gotHeader)
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
