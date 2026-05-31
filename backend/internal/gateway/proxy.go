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
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/KnowSky404/N2API/backend/internal/provider"
	"github.com/KnowSky404/N2API/backend/internal/secret"
)

const defaultUpstreamBaseURL = "https://api.openai.com"

type APIKeyAuthenticator interface {
	AuthenticateAPIKey(ctx context.Context, apiKey string) (admin.APIKey, error)
}

type AccessTokenProvider interface {
	AccessToken(ctx context.Context) (string, error)
}

type RequestLogger interface {
	CreateRequestLog(ctx context.Context, entry RequestLog) error
}

type RequestLog struct {
	RequestID   string
	ClientKeyID int64
	Provider    string
	Route       string
	Method      string
	StatusCode  int
	Latency     time.Duration
	Error       string
	CreatedAt   time.Time
}

type Config struct {
	UpstreamBaseURL string
	Logger          RequestLogger
}

type Proxy struct {
	auth            APIKeyAuthenticator
	tokens          AccessTokenProvider
	client          *http.Client
	upstreamBaseURL string
	logger          RequestLogger
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
		logger:          cfg.Logger,
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
	key, err := p.auth.AuthenticateAPIKey(r.Context(), apiKey)
	if err != nil {
		if errors.Is(err, admin.ErrUnauthorized) {
			writeOpenAIError(w, http.StatusUnauthorized, "unauthorized", "invalid bearer token")
			return
		}
		writeOpenAIError(w, http.StatusInternalServerError, "internal_error", "api key authentication failed")
		return
	}
	recorder := &statusRecorder{ResponseWriter: w}
	startedAt := time.Now()
	errorCode := ""
	defer func() {
		p.logRequest(r.Context(), RequestLog{
			RequestID:   newRequestID(),
			ClientKeyID: key.ID,
			Provider:    "openai",
			Route:       r.URL.Path,
			Method:      r.Method,
			StatusCode:  recorder.statusCode(),
			Latency:     time.Since(startedAt),
			Error:       errorCode,
			CreatedAt:   startedAt,
		})
	}()

	accessToken, err := p.tokens.AccessToken(r.Context())
	if err != nil {
		if errors.Is(err, provider.ErrNotConnected) {
			errorCode = "provider_not_connected"
			writeOpenAIError(recorder, http.StatusServiceUnavailable, errorCode, "provider account is not connected")
			return
		}
		if errors.Is(err, provider.ErrNotConfigured) {
			errorCode = "provider_not_configured"
			writeOpenAIError(recorder, http.StatusServiceUnavailable, errorCode, "provider account is not configured")
			return
		}
		errorCode = "upstream_token_error"
		writeOpenAIError(recorder, http.StatusBadGateway, errorCode, "provider token lookup failed")
		return
	}

	upstreamReq, err := p.newUpstreamRequest(r, accessToken)
	if err != nil {
		errorCode = "upstream_request_error"
		writeOpenAIError(recorder, http.StatusBadGateway, errorCode, "could not create upstream request")
		return
	}
	upstreamResp, err := p.client.Do(upstreamReq)
	if err != nil {
		errorCode = "upstream_unavailable"
		writeOpenAIError(recorder, http.StatusBadGateway, errorCode, "upstream request failed")
		return
	}
	defer upstreamResp.Body.Close()

	copyResponseHeaders(recorder.Header(), upstreamResp.Header)
	recorder.WriteHeader(upstreamResp.StatusCode)
	_, _ = io.Copy(flushWriter{ResponseWriter: recorder}, upstreamResp.Body)
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

func (p *Proxy) logRequest(ctx context.Context, entry RequestLog) {
	if p.logger == nil {
		return
	}
	if entry.StatusCode == 0 {
		entry.StatusCode = http.StatusOK
	}
	_ = p.logger.CreateRequestLog(ctx, entry)
}

func newRequestID() string {
	token, err := secret.GenerateToken("req")
	if err != nil {
		return fmt.Sprintf("req_%d", time.Now().UnixNano())
	}
	return token
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(data)
}

func (r *statusRecorder) statusCode() int {
	if r.status == 0 {
		return http.StatusOK
	}
	return r.status
}

func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
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
