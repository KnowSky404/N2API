package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/KnowSky404/N2API/backend/internal/provider"
)

const defaultUpstreamBaseURL = "https://api.openai.com"

type APIKeyAuthenticator interface {
	AuthenticateAPIKey(ctx context.Context, apiKey string) (admin.APIKey, error)
}

type AccessTokenProvider interface {
	AccessToken(ctx context.Context) (string, error)
}

type Config struct {
	UpstreamBaseURL string
}

type Proxy struct {
	auth            APIKeyAuthenticator
	tokens          AccessTokenProvider
	client          *http.Client
	upstreamBaseURL string
}

func NewProxy(auth APIKeyAuthenticator, tokens AccessTokenProvider, cfg Config) *Proxy {
	return NewProxyWithClient(auth, tokens, cfg, http.DefaultClient)
}

func NewProxyWithClient(auth APIKeyAuthenticator, tokens AccessTokenProvider, cfg Config, client *http.Client) *Proxy {
	if client == nil {
		client = http.DefaultClient
	}
	upstreamBaseURL := strings.TrimRight(strings.TrimSpace(cfg.UpstreamBaseURL), "/")
	if upstreamBaseURL == "" {
		upstreamBaseURL = defaultUpstreamBaseURL
	}
	return &Proxy{
		auth:            auth,
		tokens:          tokens,
		client:          client,
		upstreamBaseURL: upstreamBaseURL,
	}
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !isSupportedRoute(r) {
		writeOpenAIError(w, http.StatusNotFound, "unsupported_route", "unsupported route")
		return
	}
	if p.auth == nil || p.tokens == nil {
		writeOpenAIError(w, http.StatusServiceUnavailable, "service_unavailable", "gateway service unavailable")
		return
	}

	apiKey, ok := bearerToken(r.Header.Get("Authorization"))
	if !ok {
		writeOpenAIError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
		return
	}
	if _, err := p.auth.AuthenticateAPIKey(r.Context(), apiKey); err != nil {
		if errors.Is(err, admin.ErrUnauthorized) {
			writeOpenAIError(w, http.StatusUnauthorized, "unauthorized", "invalid bearer token")
			return
		}
		writeOpenAIError(w, http.StatusInternalServerError, "internal_error", "api key authentication failed")
		return
	}

	accessToken, err := p.tokens.AccessToken(r.Context())
	if err != nil {
		if errors.Is(err, provider.ErrNotConnected) {
			writeOpenAIError(w, http.StatusServiceUnavailable, "provider_not_connected", "provider account is not connected")
			return
		}
		if errors.Is(err, provider.ErrNotConfigured) {
			writeOpenAIError(w, http.StatusServiceUnavailable, "provider_not_configured", "provider account is not configured")
			return
		}
		writeOpenAIError(w, http.StatusBadGateway, "upstream_token_error", "provider token lookup failed")
		return
	}

	upstreamReq, err := p.newUpstreamRequest(r, accessToken)
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "upstream_request_error", "could not create upstream request")
		return
	}
	upstreamResp, err := p.client.Do(upstreamReq)
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "upstream_unavailable", "upstream request failed")
		return
	}
	defer upstreamResp.Body.Close()

	copyResponseHeaders(w.Header(), upstreamResp.Header)
	w.WriteHeader(upstreamResp.StatusCode)
	_, _ = io.Copy(flushWriter{ResponseWriter: w}, upstreamResp.Body)
}

func (p *Proxy) newUpstreamRequest(r *http.Request, accessToken string) (*http.Request, error) {
	upstreamURL, err := url.Parse(p.upstreamBaseURL + r.URL.Path)
	if err != nil {
		return nil, fmt.Errorf("parse upstream url: %w", err)
	}
	upstreamURL.RawQuery = r.URL.RawQuery
	req, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL.String(), r.Body)
	if err != nil {
		return nil, err
	}
	copyRequestHeaders(req.Header, r.Header)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	return req, nil
}

func isSupportedRoute(r *http.Request) bool {
	return (r.Method == http.MethodGet && r.URL.Path == "/v1/models") ||
		(r.Method == http.MethodPost && r.URL.Path == "/v1/chat/completions")
}

func bearerToken(header string) (string, bool) {
	scheme, token, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
		return "", false
	}
	return strings.TrimSpace(token), true
}

func copyRequestHeaders(dst, src http.Header) {
	for key, values := range src {
		if isHopByHopHeader(key) || strings.EqualFold(key, "Authorization") {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func copyResponseHeaders(dst, src http.Header) {
	for key, values := range src {
		if isHopByHopHeader(key) {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func isHopByHopHeader(key string) bool {
	switch strings.ToLower(key) {
	case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}

func writeOpenAIError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "n2api_error",
			"code":    code,
		},
	})
}

type flushWriter struct {
	http.ResponseWriter
}

func (w flushWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
	return n, err
}
