package provider

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	utls "github.com/refraction-networking/utls"
)

func TestHTTPClientExchangeCodePostsAuthorizationCodeGrant(t *testing.T) {
	var gotGrantType string
	var gotCode string
	var gotClientID string
	var gotClientSecret string
	var gotRedirectURI string
	var gotCodeVerifier string
	client := NewHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm returned error: %v", err)
		}
		gotGrantType = r.Form.Get("grant_type")
		gotCode = r.Form.Get("code")
		gotClientID = r.Form.Get("client_id")
		gotClientSecret = r.Form.Get("client_secret")
		gotRedirectURI = r.Form.Get("redirect_uri")
		gotCodeVerifier = r.Form.Get("code_verifier")
		return jsonResponse(http.StatusOK, map[string]any{
			"access_token":  "access-token",
			"refresh_token": "refresh-token",
			"id_token":      "id-token",
			"expires_in":    3600,
			"subject":       "acct_1",
			"display_name":  "Codex Account",
			"account_id":    "acct_chatgpt",
			"email":         "owner@example.com",
			"plan_type":     "plus",
		})
	})})

	token, err := client.ExchangeCode(context.Background(), Config{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RedirectURL:  "http://localhost/oauth/openai/callback",
		TokenURL:     "https://auth.example.test/token",
		CodeVerifier: "pkce-verifier",
	}, "auth-code")
	if err != nil {
		t.Fatalf("ExchangeCode returned error: %v", err)
	}
	if token.AccessToken != "access-token" || token.RefreshToken != "refresh-token" || token.IDToken != "id-token" || token.Email != "owner@example.com" || token.AccountID != "acct_chatgpt" || token.PlanType != "plus" {
		t.Fatalf("token = %+v", token)
	}
	if gotGrantType != "authorization_code" || gotCode != "auth-code" || gotClientID != "client-id" || gotClientSecret != "client-secret" || gotRedirectURI != "http://localhost/oauth/openai/callback" || gotCodeVerifier != "pkce-verifier" {
		t.Fatalf("posted form = grant_type:%q code:%q client_id:%q client_secret:%q redirect_uri:%q code_verifier:%q", gotGrantType, gotCode, gotClientID, gotClientSecret, gotRedirectURI, gotCodeVerifier)
	}
}

func TestHTTPClientRefreshTokenPostsRefreshGrant(t *testing.T) {
	var gotGrantType string
	var gotRefreshToken string
	client := NewHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm returned error: %v", err)
		}
		gotGrantType = r.Form.Get("grant_type")
		gotRefreshToken = r.Form.Get("refresh_token")
		return jsonResponse(http.StatusOK, map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"expires_in":    3600,
		})
	})})

	token, err := client.RefreshToken(context.Background(), Config{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		TokenURL:     "https://auth.example.test/token",
	}, "old-refresh")
	if err != nil {
		t.Fatalf("RefreshToken returned error: %v", err)
	}
	if token.AccessToken != "new-access" || token.RefreshToken != "new-refresh" {
		t.Fatalf("token = %+v", token)
	}
	if gotGrantType != "refresh_token" || gotRefreshToken != "old-refresh" {
		t.Fatalf("posted form = grant_type:%q refresh_token:%q", gotGrantType, gotRefreshToken)
	}
}

func TestHTTPClientTokenErrorDoesNotIncludeBodySecret(t *testing.T) {
	client := NewHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Body:       io.NopCloser(strings.NewReader("secret-token-value")),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})})

	_, err := client.RefreshToken(context.Background(), Config{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		TokenURL:     "https://auth.example.test/token",
	}, "old-refresh")
	if err == nil {
		t.Fatal("RefreshToken returned nil error, want token endpoint error")
	}
	if strings.Contains(err.Error(), "secret-token-value") {
		t.Fatalf("error leaked response body: %v", err)
	}
}

func TestHTTPClientProbeUsesCodexResponsesForChatGPTAccounts(t *testing.T) {
	var gotPath string
	var gotAuthorization string
	var gotChatGPTAccountID string
	var gotOpenAIBeta string
	var gotOriginator string
	var gotUserAgent string
	var gotVersion string
	var gotBody string
	client := NewHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotPath = r.URL.Path
		gotAuthorization = r.Header.Get("Authorization")
		gotChatGPTAccountID = r.Header.Get("chatgpt-account-id")
		gotOpenAIBeta = r.Header.Get("OpenAI-Beta")
		gotOriginator = r.Header.Get("originator")
		gotUserAgent = r.Header.Get("User-Agent")
		gotVersion = r.Header.Get("Version")
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		return jsonResponse(http.StatusTooManyRequests, map[string]any{
			"error": map[string]any{"message": "usage limit reached"},
		})
	})})

	result, err := client.ProbeAccountStatus(context.Background(), Config{
		APIBaseURL:            "https://api.example.test",
		CodexResponsesBaseURL: "https://chatgpt.example.test/backend-api/codex",
		ProbeChatGPTAccountID: "acct_chatgpt",
	}, "access-token")
	if err != nil {
		t.Fatalf("ProbeAccountStatus returned error: %v", err)
	}
	if result.statusCode != http.StatusTooManyRequests || result.message != "usage limit reached" {
		t.Fatalf("probe result = %+v", result)
	}
	if gotPath != "/backend-api/codex/responses" {
		t.Fatalf("path = %q, want Codex responses endpoint", gotPath)
	}
	if gotAuthorization != "Bearer access-token" || gotChatGPTAccountID != "acct_chatgpt" || gotOpenAIBeta != "responses=experimental" || gotOriginator != DefaultCodexFingerprintOriginator || gotUserAgent != DefaultCodexFingerprintUserAgent || gotVersion != DefaultCodexFingerprintVersion {
		t.Fatalf("headers auth=%q account=%q beta=%q originator=%q user-agent=%q version=%q", gotAuthorization, gotChatGPTAccountID, gotOpenAIBeta, gotOriginator, gotUserAgent, gotVersion)
	}
	var payload struct {
		Model        string `json:"model"`
		Instructions string `json:"instructions"`
		Input        []struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"input"`
		Stream bool `json:"stream"`
		Store  bool `json:"store"`
	}
	if err := json.Unmarshal([]byte(gotBody), &payload); err != nil {
		t.Fatalf("decode probe body: %v", err)
	}
	if payload.Model != "gpt-5.4-mini" || payload.Instructions != "You are Codex, a coding agent." || !payload.Stream || payload.Store {
		t.Fatalf("probe payload = %+v", payload)
	}
	if len(payload.Input) != 1 || payload.Input[0].Type != "message" || payload.Input[0].Role != "user" || payload.Input[0].Content != "n2api account status probe" {
		t.Fatalf("probe input = %+v", payload.Input)
	}
}

func TestHTTPClientProbeDoesNotReadSuccessfulCodexStream(t *testing.T) {
	body := &failOnReadBody{t: t}
	client := NewHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       body,
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})})

	result, err := client.ProbeAccountStatus(context.Background(), Config{
		CodexResponsesBaseURL: "https://chatgpt.example.test/backend-api/codex",
		ProbeChatGPTAccountID: "acct_chatgpt",
	}, "access-token")
	if err != nil {
		t.Fatalf("ProbeAccountStatus returned error: %v", err)
	}
	if result.statusCode != http.StatusOK {
		t.Fatalf("statusCode = %d, want %d", result.statusCode, http.StatusOK)
	}
	if !body.closed {
		t.Fatal("successful probe response body was not closed")
	}
}

func TestHTTPClientProbeAccountModelRequiresCodexCompletedEvent(t *testing.T) {
	tests := []struct {
		name      string
		stream    string
		wantCode  string
		wantError string
	}{
		{
			name:   "completed",
			stream: "event: response.created\ndata: {\"type\":\"response.created\"}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{}}\n\n",
		},
		{
			name:      "failed",
			stream:    "event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"error\":{\"message\":\"model failed\"}}}\n\n",
			wantCode:  "upstream_error",
			wantError: "model failed",
		},
		{
			name:      "incomplete",
			stream:    "event: response.incomplete\ndata: {\"type\":\"response.incomplete\",\"response\":{}}\n\n",
			wantCode:  "invalid_response",
			wantError: "upstream response was incomplete",
		},
		{
			name:      "missing completion",
			stream:    "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"OK\"}\n\n",
			wantCode:  "invalid_response",
			wantError: "upstream stream ended before response.completed",
		},
		{
			name:      "malformed terminal",
			stream:    "event: response.completed\ndata: not-json\n\n",
			wantCode:  "invalid_response",
			wantError: "upstream returned malformed terminal event",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var gotPath string
			var gotModel string
			var gotAccountID string
			client := NewHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				gotPath = r.URL.Path
				gotAccountID = r.Header.Get("chatgpt-account-id")
				var payload struct {
					Model  string `json:"model"`
					Stream bool   `json:"stream"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatalf("decode model probe request: %v", err)
				}
				gotModel = payload.Model
				if !payload.Stream {
					t.Fatal("Codex model probe stream = false, want true")
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(test.stream)),
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
					Request:    r,
				}, nil
			})})

			result := client.ProbeAccountModel(context.Background(), Config{
				CodexResponsesBaseURL: "https://chatgpt.example.test/backend-api/codex",
			}, SelectedAccount{
				AccountType:        AccountTypeCodexOAuth,
				AuthorizationToken: "oauth-token",
				ChatGPTAccountID:   "acct-chatgpt",
			}, "gpt-test")

			if result.statusCode != http.StatusOK || result.errorCode != test.wantCode || result.message != test.wantError {
				t.Fatalf("ProbeAccountModel result = %+v, want code=%q error=%q", result, test.wantCode, test.wantError)
			}
			if gotPath != "/backend-api/codex/responses" || gotModel != "gpt-test" || gotAccountID != "acct-chatgpt" {
				t.Fatalf("request path=%q model=%q account=%q", gotPath, gotModel, gotAccountID)
			}
		})
	}
}

func TestHTTPClientProbeAccountModelClassifiesCodexStreamDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	client := NewHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       &contextDeadlineBody{ctx: r.Context()},
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Request:    r,
		}, nil
	})})

	result := client.ProbeAccountModel(ctx, Config{
		CodexResponsesBaseURL: "https://chatgpt.example.test/backend-api/codex",
	}, SelectedAccount{
		AccountType:        AccountTypeCodexOAuth,
		AuthorizationToken: "oauth-token",
		ChatGPTAccountID:   "acct-chatgpt",
	}, "gpt-test")

	if result.statusCode != http.StatusOK || result.errorCode != "timeout" || result.message != "model test timed out" {
		t.Fatalf("ProbeAccountModel result = %+v, want timeout", result)
	}
}

func TestHTTPClientProbeAccountModelUsesTLSFingerprintThroughProxy(t *testing.T) {
	for _, tc := range []struct {
		name     string
		proxyTLS bool
	}{
		{name: "HTTP proxy"},
		{name: "HTTPS proxy", proxyTLS: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstreamReached := make(chan struct{}, 1)
			upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upstreamReached <- struct{}{}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"choices":[]}`)
			}))
			defer upstream.Close()

			var mu sync.Mutex
			var gotMethod string
			var gotTarget string
			var gotProxyAuthorization string
			proxy := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				gotMethod = r.Method
				gotTarget = r.Host
				gotProxyAuthorization = r.Header.Get("Proxy-Authorization")
				mu.Unlock()
				if r.Method != http.MethodConnect {
					http.Error(w, "CONNECT required", http.StatusMethodNotAllowed)
					return
				}
				upstreamConn, err := net.Dial("tcp", r.Host)
				if err != nil {
					http.Error(w, "upstream unavailable", http.StatusBadGateway)
					return
				}
				hijacker, ok := w.(http.Hijacker)
				if !ok {
					upstreamConn.Close()
					http.Error(w, "hijacking unavailable", http.StatusInternalServerError)
					return
				}
				clientConn, buffered, err := hijacker.Hijack()
				if err != nil {
					upstreamConn.Close()
					return
				}
				defer clientConn.Close()
				defer upstreamConn.Close()
				if _, err := buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
					return
				}
				if err := buffered.Flush(); err != nil {
					return
				}
				copyDone := make(chan struct{}, 1)
				go func() {
					_, _ = io.Copy(upstreamConn, buffered)
					copyDone <- struct{}{}
				}()
				_, _ = io.Copy(clientConn, bufio.NewReader(upstreamConn))
				<-copyDone
			}))
			if tc.proxyTLS {
				proxy.StartTLS()
			} else {
				proxy.Start()
			}
			defer proxy.Close()

			proxyURL, err := url.Parse(proxy.URL)
			if err != nil {
				t.Fatalf("Parse proxy URL returned error: %v", err)
			}
			proxyURL.User = url.UserPassword("proxy-user", "proxy-pass")
			client := NewHTTPClient(&http.Client{})
			client.modelProbeTLSConfig = &utls.Config{InsecureSkipVerify: true}
			if tc.proxyTLS {
				client.modelProbeProxyTLSConfig = &tls.Config{InsecureSkipVerify: true}
			}

			result := client.ProbeAccountModel(context.Background(), Config{}, SelectedAccount{
				AccountType:        AccountTypeAPIUpstream,
				AuthorizationToken: "api-secret",
				BaseURL:            upstream.URL + "/v1",
				ProxyURL:           proxyURL.String(),
				FingerprintTLS:     "chrome",
			}, "gpt-test")

			if result.statusCode != http.StatusOK || result.errorCode != "" {
				t.Fatalf("ProbeAccountModel result = %+v, want success", result)
			}
			select {
			case <-upstreamReached:
			case <-time.After(time.Second):
				t.Fatal("TLS upstream was not reached through proxy tunnel")
			}
			mu.Lock()
			defer mu.Unlock()
			if gotMethod != http.MethodConnect || gotTarget != strings.TrimPrefix(upstream.URL, "https://") {
				t.Fatalf("proxy request method=%q target=%q, want CONNECT %q", gotMethod, gotTarget, strings.TrimPrefix(upstream.URL, "https://"))
			}
			if gotProxyAuthorization != "Basic cHJveHktdXNlcjpwcm94eS1wYXNz" {
				t.Fatalf("Proxy-Authorization = %q, want Basic credentials", gotProxyAuthorization)
			}
		})
	}
}

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
				FingerprintUA:      "profile-agent",
				FingerprintHeaders: map[string]string{"X-Fingerprint": "profile"},
			}, "gpt-test")

			if result.statusCode != test.statusCode || result.errorCode != test.wantCode {
				t.Fatalf("ProbeAccountModel result = %+v, want status=%d code=%q", result, test.statusCode, test.wantCode)
			}
			if gotPath != "/v1/chat/completions" || gotAuthorization != "Bearer api-secret" || gotUserAgent != "profile-agent" || gotHeader != "profile" {
				t.Fatalf("request path=%q auth=%q ua=%q fingerprint=%q", gotPath, gotAuthorization, gotUserAgent, gotHeader)
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type contextDeadlineBody struct {
	ctx context.Context
}

func (b *contextDeadlineBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *contextDeadlineBody) Close() error {
	return nil
}

type failOnReadBody struct {
	t      *testing.T
	closed bool
}

func (b *failOnReadBody) Read(_ []byte) (int, error) {
	b.t.Fatal("successful codex probe stream body should not be read")
	return 0, io.EOF
}

func (b *failOnReadBody) Close() error {
	b.closed = true
	return nil
}

func jsonResponse(status int, value any) (*http.Response, error) {
	body := new(strings.Builder)
	if err := json.NewEncoder(body).Encode(value); err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body.String())),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Request:    &http.Request{URL: &url.URL{Scheme: "https", Host: "auth.example.test"}},
	}, nil
}
