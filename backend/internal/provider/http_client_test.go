package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
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
	var gotBody string
	client := NewHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotPath = r.URL.Path
		gotAuthorization = r.Header.Get("Authorization")
		gotChatGPTAccountID = r.Header.Get("chatgpt-account-id")
		gotOpenAIBeta = r.Header.Get("OpenAI-Beta")
		gotOriginator = r.Header.Get("originator")
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
	if gotAuthorization != "Bearer access-token" || gotChatGPTAccountID != "acct_chatgpt" || gotOpenAIBeta != "responses=experimental" || gotOriginator != "codex_cli_rs" {
		t.Fatalf("headers auth=%q account=%q beta=%q originator=%q", gotAuthorization, gotChatGPTAccountID, gotOpenAIBeta, gotOriginator)
	}
	if !strings.Contains(gotBody, `"model":"gpt-5.4-mini"`) {
		t.Fatalf("body = %q, want supported Codex probe model", gotBody)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
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
