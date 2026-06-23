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

type SelectedAccount struct {
	AccountID          int64
	Provider           string
	AccountType        string
	AuthorizationToken string
	BaseURL            string
	ChatGPTAccountID   string
}

type AccountProvider interface {
	SelectAccountForModel(ctx context.Context, model string, excludedAccountIDs ...int64) (SelectedAccount, error)
}

type AccountFailureReporter interface {
	RecordAccountFailure(ctx context.Context, accountID int64, statusCode int, retryAfter, message string) error
}

type ExposedModel struct {
	ID      string
	OwnedBy string
}

type ModelProvider interface {
	DefaultModel(ctx context.Context) (string, error)
	IsModelAllowed(ctx context.Context, model string) (bool, error)
	ListExposedModels(ctx context.Context) ([]ExposedModel, error)
}

type RequestLogger interface {
	CreateRequestLog(ctx context.Context, entry RequestLog) error
}

type UsagePricer interface {
	EstimateUsageCost(ctx context.Context, usage Usage) (UsageCostEstimate, error)
}

type UsageCostEstimate struct {
	Matched      bool
	CostMicrousd int64
	Snapshot     map[string]any
}

type RequestLog struct {
	RequestID             string
	ClientKeyID           int64
	Provider              string
	ProviderAccountID     int64
	ProviderAccountType   string
	Model                 string
	Route                 string
	Method                string
	StatusCode            int
	Latency               time.Duration
	Error                 string
	InputTokens           int
	OutputTokens          int
	TotalTokens           int
	CachedInputTokens     int
	ReasoningTokens       int
	UsageSource           string
	EstimatedCostMicrousd int64
	PricingSnapshot       map[string]any
	CreatedAt             time.Time
}

type Config struct {
	UpstreamBaseURL       string
	CodexResponsesBaseURL string
	Logger                RequestLogger
	ModelProvider         ModelProvider
	UsagePricer           UsagePricer
}

type Proxy struct {
	auth            APIKeyAuthenticator
	accounts        AccountProvider
	client          *http.Client
	upstreamBaseURL string
	codexBaseURL    string
	logger          RequestLogger
	models          ModelProvider
	usagePricer     UsagePricer
}

func NewProxy(auth APIKeyAuthenticator, accounts AccountProvider, cfg Config) *Proxy {
	return NewProxyWithClient(auth, accounts, cfg, http.DefaultClient)
}

func NewProxyWithClient(auth APIKeyAuthenticator, accounts AccountProvider, cfg Config, client *http.Client) *Proxy {
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
		accounts:        accounts,
		client:          client,
		upstreamBaseURL: upstreamBaseURL,
		codexBaseURL:    codexBaseURL,
		logger:          cfg.Logger,
		models:          cfg.ModelProvider,
		usagePricer:     cfg.UsagePricer,
	}
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !isSupportedRoute(r) {
		writeOpenAIError(w, http.StatusNotFound, "unsupported_route", "unsupported route")
		return
	}
	if p.auth == nil {
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
	var loggedAccount SelectedAccount
	requestModel := ""
	observedUsage := Usage{Source: "missing"}
	defer func() {
		if observedUsage.Model == "" {
			observedUsage.Model = requestModel
		}
		costEstimate := p.estimateUsageCost(r.Context(), observedUsage)
		p.logRequest(r.Context(), RequestLog{
			RequestID:             newRequestID(),
			ClientKeyID:           key.ID,
			Provider:              "openai",
			ProviderAccountID:     loggedAccount.AccountID,
			ProviderAccountType:   loggedAccount.AccountType,
			Model:                 requestModel,
			Route:                 r.URL.Path,
			Method:                r.Method,
			StatusCode:            recorder.statusCode(),
			Latency:               time.Since(startedAt),
			Error:                 errorCode,
			InputTokens:           observedUsage.InputTokens,
			OutputTokens:          observedUsage.OutputTokens,
			TotalTokens:           observedUsage.TotalTokens,
			CachedInputTokens:     observedUsage.CachedInputTokens,
			ReasoningTokens:       observedUsage.ReasoningTokens,
			UsageSource:           observedUsage.Source,
			EstimatedCostMicrousd: costEstimate.CostMicrousd,
			PricingSnapshot:       costEstimate.Snapshot,
			CreatedAt:             startedAt,
		})
	}()

	if r.Method == http.MethodGet && r.URL.Path == "/v1/models" {
		if err := p.writeLocalModels(r.Context(), recorder, key); err != nil {
			errorCode = "internal_error"
			writeOpenAIError(recorder, http.StatusInternalServerError, errorCode, "could not list models")
		}
		return
	}
	if p.accounts == nil {
		errorCode = "service_unavailable"
		writeOpenAIError(recorder, http.StatusServiceUnavailable, errorCode, "gateway service unavailable")
		return
	}

	bodyFactory, maxAttempts, model, err := p.requestBodyFactory(r)
	if err != nil {
		errorCode = requestBodyErrorCode(err)
		writeOpenAIError(recorder, requestBodyErrorStatus(err), errorCode, requestBodyErrorMessage(err))
		return
	}
	requestModel = model
	if model != "" && !apiKeyAllowsModel(key, model) {
		errorCode = "model_not_found"
		writeOpenAIError(recorder, http.StatusNotFound, errorCode, "requested model is not available")
		return
	}
	if model != "" {
		allowed, err := p.modelAllowed(r.Context(), model)
		if err != nil {
			errorCode = requestBodyErrorCode(err)
			writeOpenAIError(recorder, requestBodyErrorStatus(err), errorCode, requestBodyErrorMessage(err))
			return
		}
		if !allowed {
			errorCode = "model_not_found"
			writeOpenAIError(recorder, http.StatusBadRequest, errorCode, "requested model is not available")
			return
		}
	}

	var firstAccountID int64
	var lastRetryableResp *http.Response
	for attempt := 0; attempt < maxAttempts; attempt++ {
		var excluded []int64
		if attempt > 0 && firstAccountID > 0 {
			excluded = append(excluded, firstAccountID)
		}
		selected, err := p.accounts.SelectAccountForModel(r.Context(), model, excluded...)
		if err != nil {
			if lastRetryableResp != nil {
				_ = lastRetryableResp.Body.Close()
				lastRetryableResp = nil
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
		loggedAccount = selected

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
		observedUsage = copyUpstreamResponse(recorder, upstreamResp, r.URL.Path)
		return
	}
}

func copyUpstreamResponse(w http.ResponseWriter, resp *http.Response, route string) Usage {
	if resp == nil || resp.Body == nil {
		return Usage{Source: "missing"}
	}
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		return copyStreamingResponse(w, resp.Body, route)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Usage{Source: "missing"}
	}
	_, _ = w.Write(body)
	return ParseUsageFromJSON(route, body)
}

func copyStreamingResponse(w http.ResponseWriter, body io.Reader, route string) Usage {
	observer := NewSSEUsageObserver(route)
	buffer := make([]byte, 32*1024)
	writer := flushWriter{ResponseWriter: w}
	for {
		n, readErr := body.Read(buffer)
		if n > 0 {
			chunk := buffer[:n]
			observer.Observe(chunk)
			if _, writeErr := writer.Write(chunk); writeErr != nil {
				return observer.Usage()
			}
		}
		if readErr != nil {
			break
		}
	}
	return observer.Usage()
}

func (p *Proxy) estimateUsageCost(ctx context.Context, usage Usage) UsageCostEstimate {
	if p.usagePricer == nil {
		return UsageCostEstimate{Snapshot: map[string]any{"matched": false}}
	}
	estimate, err := p.usagePricer.EstimateUsageCost(ctx, usage)
	if err != nil {
		return UsageCostEstimate{Snapshot: map[string]any{"matched": false, "error": "pricing_error"}}
	}
	if estimate.Snapshot == nil {
		estimate.Snapshot = map[string]any{"matched": estimate.Matched}
	}
	return estimate
}

func (p *Proxy) recordAccountFailure(ctx context.Context, accountID int64, statusCode int, retryAfter, message string) {
	if accountID <= 0 {
		return
	}
	reporter, ok := p.accounts.(AccountFailureReporter)
	if !ok {
		return
	}
	_ = reporter.RecordAccountFailure(ctx, accountID, statusCode, retryAfter, message)
}

func (p *Proxy) writeLocalModels(ctx context.Context, w http.ResponseWriter, key admin.APIKey) error {
	models := []ExposedModel{}
	if p.models != nil {
		var err error
		models, err = p.models.ListExposedModels(ctx)
		if err != nil {
			return err
		}
	}
	type openAIModel struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}
	data := make([]openAIModel, 0, len(models))
	for _, model := range models {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		if !apiKeyAllowsModel(key, id) {
			continue
		}
		ownedBy := strings.TrimSpace(model.OwnedBy)
		if ownedBy == "" {
			ownedBy = "openai"
		}
		data = append(data, openAIModel{
			ID:      id,
			Object:  "model",
			Created: 0,
			OwnedBy: ownedBy,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(map[string]any{
		"object": "list",
		"data":   data,
	})
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

func (p *Proxy) newUpstreamRequest(r *http.Request, selected SelectedAccount, body io.ReadCloser) (*http.Request, error) {
	useCodexEndpoint := selected.AccountType == provider.AccountTypeCodexOAuth && r.Method == http.MethodPost && r.URL.Path == "/v1/responses" && strings.TrimSpace(selected.ChatGPTAccountID) != ""
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
	} else if selectedBaseURL := strings.TrimRight(strings.TrimSpace(selected.BaseURL), "/"); selectedBaseURL != "" {
		upstreamBaseURL, upstreamPath = upstreamURLBaseAndPath(selectedBaseURL, upstreamPath)
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
	req.Header.Set("Authorization", "Bearer "+selected.AuthorizationToken)
	if useCodexEndpoint {
		req.Header.Set("chatgpt-account-id", strings.TrimSpace(selected.ChatGPTAccountID))
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("OpenAI-Beta", "responses=experimental")
		req.Header.Set("originator", "codex_cli_rs")
		req.Header.Set("User-Agent", defaultCodexUserAgent)
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func upstreamURLBaseAndPath(baseURL, routePath string) (string, string) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	routePath = "/" + strings.TrimLeft(strings.TrimSpace(routePath), "/")
	if strings.HasSuffix(baseURL, "/v1") && strings.HasPrefix(routePath, "/v1/") {
		return strings.TrimSuffix(baseURL, "/v1"), routePath
	}
	return baseURL, routePath
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

var (
	errReplayBodyTooLarge = errors.New("request body too large to replay")
	errInvalidJSONBody    = errors.New("request body must be valid json")
	errModelNotFound      = errors.New("model not found")
)

func (p *Proxy) requestBodyFactory(r *http.Request) (func() io.ReadCloser, int, string, error) {
	if r.Method != http.MethodPost || r.Body == nil {
		return func() io.ReadCloser { return nil }, 2, "", nil
	}

	limitedBody, err := io.ReadAll(io.LimitReader(r.Body, maxReplayableRequestBody+1))
	if err != nil {
		return nil, 0, "", err
	}
	if len(limitedBody) > maxReplayableRequestBody {
		_ = r.Body.Close()
		return nil, 0, "", errReplayBodyTooLarge
	}
	_ = r.Body.Close()
	model := ""
	if routeRequiresModel(r) {
		var body []byte
		body, model, err = p.normalizeModelRequestBody(r.Context(), limitedBody)
		if err != nil {
			return nil, 0, "", err
		}
		limitedBody = body
	}
	return func() io.ReadCloser {
		return io.NopCloser(bytes.NewReader(limitedBody))
	}, 2, model, nil
}

func routeRequiresModel(r *http.Request) bool {
	return r.Method == http.MethodPost && (r.URL.Path == "/v1/chat/completions" || r.URL.Path == "/v1/responses")
}

func (p *Proxy) normalizeModelRequestBody(ctx context.Context, raw []byte) ([]byte, string, error) {
	payload := map[string]any{}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, "", errInvalidJSONBody
		}
	}

	rawModel, hasModel := payload["model"]
	model := ""
	if hasModel {
		modelValue, ok := rawModel.(string)
		if !ok {
			return nil, "", errInvalidJSONBody
		}
		model = strings.TrimSpace(modelValue)
	}
	if model == "" {
		defaultModel, err := p.defaultModel(ctx)
		if err != nil {
			return nil, "", err
		}
		model = defaultModel
		payload["model"] = model
		raw, err = json.Marshal(payload)
		if err != nil {
			return nil, "", err
		}
	} else if rawModel != model {
		payload["model"] = model
		normalized, err := json.Marshal(payload)
		if err != nil {
			return nil, "", err
		}
		raw = normalized
	}

	return raw, model, nil
}

func (p *Proxy) defaultModel(ctx context.Context) (string, error) {
	if p.models == nil {
		return "", errModelNotFound
	}
	model, err := p.models.DefaultModel(ctx)
	if err != nil {
		return "", err
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return "", errModelNotFound
	}
	return model, nil
}

func (p *Proxy) modelAllowed(ctx context.Context, model string) (bool, error) {
	if p.models == nil {
		return true, nil
	}
	return p.models.IsModelAllowed(ctx, model)
}

func apiKeyAllowsModel(key admin.APIKey, model string) bool {
	switch key.ModelPolicy {
	case "", admin.APIKeyModelPolicyAll:
		return true
	case admin.APIKeyModelPolicySelected:
		model = strings.TrimSpace(model)
		for _, allowed := range key.AllowedModels {
			if model == allowed {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func requestBodyErrorCode(err error) string {
	switch {
	case errors.Is(err, errReplayBodyTooLarge), errors.Is(err, errInvalidJSONBody):
		return "invalid_request"
	case errors.Is(err, errModelNotFound), errors.Is(err, admin.ErrInvalidInput):
		return "model_not_found"
	default:
		return "upstream_request_error"
	}
}

func requestBodyErrorStatus(err error) int {
	switch {
	case errors.Is(err, errReplayBodyTooLarge), errors.Is(err, errInvalidJSONBody), errors.Is(err, errModelNotFound), errors.Is(err, admin.ErrInvalidInput):
		return http.StatusBadRequest
	default:
		return http.StatusBadGateway
	}
}

func requestBodyErrorMessage(err error) string {
	switch {
	case errors.Is(err, errReplayBodyTooLarge):
		return "request body is too large to replay"
	case errors.Is(err, errInvalidJSONBody):
		return "request body must be valid JSON"
	case errors.Is(err, errModelNotFound), errors.Is(err, admin.ErrInvalidInput):
		return "requested model is not available"
	default:
		return "could not read upstream request"
	}
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
	case errors.Is(err, provider.ErrModelUnavailable):
		return "model_unavailable"
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
	case "model_unavailable":
		return "requested model is not available"
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
