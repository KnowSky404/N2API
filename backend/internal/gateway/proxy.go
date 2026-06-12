package gateway

import (
	"bytes"
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
const defaultCodexResponsesBaseURL = "https://chatgpt.com/backend-api/codex"
const defaultCodexUserAgent = "codex_cli_rs/0.125.0 (Ubuntu 22.4.0; x86_64) xterm-256color"
const defaultCodexInstructions = "You are Codex, a coding agent."
const maxReplayableRequestBody = 1 << 20
const maxFailureBody = 64 << 10

type APIKeyAuthenticator interface {
	AuthenticateAPIKey(ctx context.Context, apiKey string) (admin.APIKey, error)
}

type SelectedToken struct {
	AccountID        int64
	Token            string
	ChatGPTAccountID string
}

type AccessTokenProvider interface {
	SelectAccessToken(ctx context.Context, excludedAccountIDs ...int64) (SelectedToken, error)
}

type AccountFailureReporter interface {
	RecordAccountFailure(ctx context.Context, accountID int64, statusCode int, retryAfter, message string) error
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
	UpstreamBaseURL       string
	CodexResponsesBaseURL string
	Logger                RequestLogger
}

type Proxy struct {
	auth            APIKeyAuthenticator
	tokens          AccessTokenProvider
	client          *http.Client
	upstreamBaseURL string
	codexBaseURL    string
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
	codexBaseURL := strings.TrimRight(strings.TrimSpace(cfg.CodexResponsesBaseURL), "/")
	if codexBaseURL == "" {
		codexBaseURL = defaultCodexResponsesBaseURL
	}
	return &Proxy{
		auth:            auth,
		tokens:          tokens,
		client:          client,
		upstreamBaseURL: upstreamBaseURL,
		codexBaseURL:    codexBaseURL,
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

	bodyFactory, maxAttempts, err := requestBodyFactory(r)
	if err != nil {
		errorCode = "upstream_request_error"
		writeOpenAIError(recorder, http.StatusBadGateway, errorCode, "could not read upstream request")
		return
	}

	var firstAccountID int64
	var lastRetryableResp *http.Response
	for attempt := 0; attempt < maxAttempts; attempt++ {
		var excluded []int64
		if attempt > 0 && firstAccountID > 0 {
			excluded = append(excluded, firstAccountID)
		}
		selected, err := p.tokens.SelectAccessToken(r.Context(), excluded...)
		if err != nil {
			if lastRetryableResp != nil {
				copyResponseHeaders(recorder.Header(), lastRetryableResp.Header)
				recorder.WriteHeader(lastRetryableResp.StatusCode)
				_, _ = io.Copy(flushWriter{ResponseWriter: recorder}, lastRetryableResp.Body)
				_ = lastRetryableResp.Body.Close()
				return
			}
			errorCode = providerErrorCode(err)
			writeOpenAIError(recorder, http.StatusServiceUnavailable, errorCode, providerErrorMessage(errorCode))
			return
		}
		if lastRetryableResp != nil {
			_ = lastRetryableResp.Body.Close()
			lastRetryableResp = nil
		}
		if attempt == 0 {
			firstAccountID = selected.AccountID
		} else if selected.AccountID != 0 && selected.AccountID == firstAccountID {
			errorCode = "upstream_unavailable"
			writeOpenAIError(recorder, http.StatusBadGateway, errorCode, "upstream request failed")
			return
		}

		upstreamReq, err := p.newUpstreamRequest(r, selected, bodyFactory())
		if err != nil {
			errorCode = "upstream_request_error"
			writeOpenAIError(recorder, http.StatusBadGateway, errorCode, "could not create upstream request")
			return
		}
		upstreamResp, err := p.client.Do(upstreamReq)
		if err != nil {
			p.recordAccountFailure(r.Context(), selected.AccountID, http.StatusBadGateway, "", err.Error())
			if attempt+1 < maxAttempts {
				continue
			}
			errorCode = "upstream_unavailable"
			writeOpenAIError(recorder, http.StatusBadGateway, errorCode, "upstream request failed")
			return
		}

		if retryableUpstreamStatus(upstreamResp.StatusCode) {
			message := captureFailureMessage(upstreamResp)
			p.recordAccountFailure(r.Context(), selected.AccountID, upstreamResp.StatusCode, upstreamResp.Header.Get("Retry-After"), message)
			if attempt+1 < maxAttempts {
				lastRetryableResp = upstreamResp
				continue
			}
		}
		defer upstreamResp.Body.Close()

		copyResponseHeaders(recorder.Header(), upstreamResp.Header)
		recorder.WriteHeader(upstreamResp.StatusCode)
		_, _ = io.Copy(flushWriter{ResponseWriter: recorder}, upstreamResp.Body)
		return
	}
}

func (p *Proxy) recordAccountFailure(ctx context.Context, accountID int64, statusCode int, retryAfter, message string) {
	if accountID <= 0 {
		return
	}
	reporter, ok := p.tokens.(AccountFailureReporter)
	if !ok {
		return
	}
	_ = reporter.RecordAccountFailure(ctx, accountID, statusCode, retryAfter, message)
}

func retryableUpstreamStatus(statusCode int) bool {
	return statusCode == http.StatusUnauthorized ||
		statusCode == http.StatusForbidden ||
		statusCode == http.StatusTooManyRequests ||
		statusCode >= http.StatusInternalServerError
}

func captureFailureMessage(resp *http.Response) string {
	if resp == nil || resp.Body == nil {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFailureBody+1))
	if err != nil {
		return ""
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return http.StatusText(resp.StatusCode)
	}
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && strings.TrimSpace(payload.Error.Message) != "" {
		return strings.TrimSpace(payload.Error.Message)
	}
	if len(body) > maxFailureBody {
		body = body[:maxFailureBody]
	}
	return string(body)
}

func (p *Proxy) newUpstreamRequest(r *http.Request, selected SelectedToken, body io.ReadCloser) (*http.Request, error) {
	useCodexEndpoint := r.Method == http.MethodPost && r.URL.Path == "/v1/responses" && strings.TrimSpace(selected.ChatGPTAccountID) != ""
	upstreamPath := r.URL.Path
	upstreamBaseURL := p.upstreamBaseURL
	if useCodexEndpoint {
		upstreamBaseURL = p.codexBaseURL
		upstreamPath = "/responses"
		var err error
		body, err = normalizeCodexResponsesBody(body)
		if err != nil {
			return nil, err
		}
	}
	upstreamURL, err := url.Parse(upstreamBaseURL + upstreamPath)
	if err != nil {
		return nil, fmt.Errorf("parse upstream url: %w", err)
	}
	upstreamURL.RawQuery = r.URL.RawQuery
	req, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL.String(), body)
	if err != nil {
		return nil, err
	}
	copyRequestHeaders(req.Header, r.Header)
	req.Header.Set("Authorization", "Bearer "+selected.Token)
	if useCodexEndpoint {
		req.Host = "chatgpt.com"
		req.Header.Set("chatgpt-account-id", strings.TrimSpace(selected.ChatGPTAccountID))
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("OpenAI-Beta", "responses=experimental")
		req.Header.Set("originator", "codex_cli_rs")
		req.Header.Set("User-Agent", defaultCodexUserAgent)
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func normalizeCodexResponsesBody(body io.ReadCloser) (io.ReadCloser, error) {
	if body == nil {
		return nil, nil
	}
	raw, err := io.ReadAll(body)
	if closeErr := body.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return io.NopCloser(bytes.NewReader(raw)), nil
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return io.NopCloser(bytes.NewReader(raw)), nil
	}
	payload["stream"] = true
	payload["store"] = false
	if instructions, ok := payload["instructions"].(string); !ok || strings.TrimSpace(instructions) == "" {
		payload["instructions"] = defaultCodexInstructions
	}
	if input, ok := payload["input"].(string); ok {
		payload["input"] = []any{
			map[string]any{
				"type":    "message",
				"role":    "user",
				"content": input,
			},
		}
	}
	normalized, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(normalized)), nil
}

func requestBodyFactory(r *http.Request) (func() io.ReadCloser, int, error) {
	if r.Method != http.MethodPost || r.Body == nil {
		return func() io.ReadCloser { return nil }, 2, nil
	}

	limitedBody, err := io.ReadAll(io.LimitReader(r.Body, maxReplayableRequestBody+1))
	if err != nil {
		return nil, 0, err
	}
	if len(limitedBody) > maxReplayableRequestBody {
		body := io.NopCloser(io.MultiReader(bytes.NewReader(limitedBody), r.Body))
		return func() io.ReadCloser { return body }, 1, nil
	}
	_ = r.Body.Close()
	return func() io.ReadCloser {
		return io.NopCloser(bytes.NewReader(limitedBody))
	}, 2, nil
}

func isSupportedRoute(r *http.Request) bool {
	return (r.Method == http.MethodGet && r.URL.Path == "/v1/models") ||
		(r.Method == http.MethodPost && r.URL.Path == "/v1/chat/completions") ||
		(r.Method == http.MethodPost && r.URL.Path == "/v1/responses") ||
		(r.Method == http.MethodGet && isResponsesSubroute(r.URL.Path))
}

func isResponsesSubroute(path string) bool {
	if !strings.HasPrefix(path, "/v1/responses/") {
		return false
	}
	rest := strings.TrimPrefix(path, "/v1/responses/")
	if rest == "" || strings.Contains(rest, "//") {
		return false
	}
	parts := strings.Split(rest, "/")
	return len(parts) == 1 || (len(parts) == 2 && parts[1] == "input_items")
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

func providerErrorCode(err error) string {
	switch {
	case errors.Is(err, provider.ErrNotConnected):
		return "provider_not_connected"
	case errors.Is(err, provider.ErrNotConfigured):
		return "provider_not_configured"
	case errors.Is(err, provider.ErrAccountsDisabled):
		return "provider_accounts_disabled"
	case errors.Is(err, provider.ErrAccountsUnavailable):
		return "provider_accounts_unavailable"
	default:
		return "upstream_token_error"
	}
}

func providerErrorMessage(code string) string {
	switch code {
	case "provider_not_connected":
		return "provider account is not connected"
	case "provider_not_configured":
		return "provider account is not configured"
	case "provider_accounts_disabled":
		return "provider accounts are disabled"
	case "provider_accounts_unavailable":
		return "provider accounts are unavailable"
	default:
		return "provider token lookup failed"
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
