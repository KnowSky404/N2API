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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/KnowSky404/N2API/backend/internal/provider"
	"github.com/KnowSky404/N2API/backend/internal/secret"
)

const defaultUpstreamBaseURL = "https://api.openai.com"
const defaultCodexResponsesBaseURL = "https://chatgpt.com/backend-api/codex"
const defaultCodexUserAgent = provider.DefaultCodexFingerprintUserAgent
const defaultCodexInstructions = "You are Codex, a coding agent."
const maxReplayableRequestBody = 1 << 20
const maxReplayableAttempts = 5
const maxFailureBody = 64 << 10
const requestLogWriteTimeout = 5 * time.Second
const accountRecoveryWriteTimeout = 2 * time.Second

type APIKeyAuthenticator interface {
	AuthenticateAPIKey(ctx context.Context, apiKey string) (admin.APIKey, error)
}

type SelectedAccount struct {
	AccountID                int64
	Provider                 string
	AccountType              string
	DisplayName              string
	AuthorizationToken       string
	BaseURL                  string
	ProxyURL                 string
	ChatGPTAccountID         string
	MaxConcurrentRequests    int
	RoutingPoolID            int64
	RoutingPoolName          string
	RoutingPoolFallbackDepth int
	RoutingPoolFallbackChain string
	RoutingPoolError         string
	FingerprintUA            string
	FingerprintTLS           string
	FingerprintHeaders       map[string]string
}

type AccountProvider interface {
	SelectAccountForModel(ctx context.Context, model string, excludedAccountIDs ...int64) (SelectedAccount, error)
}

type StickyAccountProvider interface {
	SelectAccountForModelAndSession(ctx context.Context, model, sessionID string, excludedAccountIDs ...int64) (SelectedAccount, error)
}

type RoutingPoolAccountProvider interface {
	SelectAccountForModelInRoutingPool(ctx context.Context, routingPoolID int64, model string, excludedAccountIDs ...int64) (SelectedAccount, error)
	SelectAccountForModelAndSessionInRoutingPool(ctx context.Context, routingPoolID int64, model, sessionID string, excludedAccountIDs ...int64) (SelectedAccount, error)
}

type RoutingPoolChainAccountProvider interface {
	SelectAccountForModelInRoutingPoolChain(ctx context.Context, routingPoolID int64, model string, excludedAccountIDs ...int64) (SelectedAccount, error)
	SelectAccountForModelAndSessionInRoutingPoolChain(ctx context.Context, routingPoolID int64, model, sessionID string, excludedAccountIDs ...int64) (SelectedAccount, error)
}

type AccountFailureReporter interface {
	RecordAccountFailure(ctx context.Context, accountID int64, statusCode int, retryAfter, message string) error
}

type AccountAuthorizationRefresher interface {
	RefreshAccountAuthorization(ctx context.Context, accountID int64, rejectedAccessToken string, statusCode int, message string) (accessToken string, retry bool, failureRecorded bool, err error)
}

type AccountUsageRecorder interface {
	RecordAccountUsed(ctx context.Context, accountID int64) error
}

type AccountRecoveryRecorder interface {
	RecordAccountRecovered(ctx context.Context, accountID int64) error
}

type AccountConcurrencySnapshotProvider interface {
	AccountConcurrencySnapshot() map[int64]int
}

type APIKeyConcurrencySnapshotProvider interface {
	APIKeyConcurrencySnapshot() map[int64]int
}

type APIKeyRateSnapshotProvider interface {
	APIKeyRequestRateSnapshot() map[int64]int
	APIKeyTokenRateSnapshot() map[int64]int
}

type ExposedModel struct {
	ID      string
	OwnedBy string
}

type ModelProvider interface {
	DefaultModel(ctx context.Context) (string, error)
}

type RoutingPoolModelProvider interface {
	ListExposedModelsForRoutingPoolChain(ctx context.Context, routingPoolID int64) ([]ExposedModel, error)
}

type RequestLogger interface {
	CreateRequestLog(ctx context.Context, entry RequestLog) error
}

type UsagePricer interface {
	EstimateUsageCost(ctx context.Context, usage Usage) (UsageCostEstimate, error)
}

type GatewaySettingsProvider interface {
	GetGatewaySettings(ctx context.Context) (admin.GatewaySettings, error)
}

type ErrorPassthroughRulesProvider interface {
	ListErrorPassthroughRules(ctx context.Context) ([]admin.ErrorPassthroughRule, error)
}

type BudgetProvider interface {
	GetAPIKeyBudgetUsage(ctx context.Context, key admin.APIKey, now time.Time) (admin.APIKeyBudgetUsage, error)
}

type UsageCostEstimate struct {
	Matched      bool
	CostMicrousd int64
	Snapshot     map[string]any
}

type RequestLog struct {
	RequestID                string
	ClientKeyID              int64
	Provider                 string
	ProviderAccountID        int64
	ProviderAccountType      string
	ProviderAccountName      string
	RoutingPoolID            int64
	RoutingPoolName          string
	RoutingPoolFallbackDepth int
	RoutingPoolFallbackChain string
	RoutingPoolError         string
	FingerprintUA            string
	FingerprintTLS           string
	FingerprintHeaders       map[string]string
	Model                    string
	SessionID                string
	Route                    string
	Method                   string
	StatusCode               int
	Latency                  time.Duration
	Error                    string
	InputTokens              int
	OutputTokens             int
	TotalTokens              int
	CachedInputTokens        int
	ReasoningTokens          int
	UsageSource              string
	EstimatedCostMicrousd    int64
	PricingSnapshot          map[string]any
	GatewayAttemptCount      int
	GatewayFallbackCount     int
	CreatedAt                time.Time
}

type Config struct {
	UpstreamBaseURL                 string
	CodexResponsesBaseURL           string
	MaxConcurrentGatewayRequests    int
	MaxConcurrentRequestsPerAccount int
	MaxConcurrentRequestsPerKey     int
	MaxRequestsPerMinutePerKey      int
	MaxTokensPerMinutePerKey        int
	Logger                          RequestLogger
	ModelProvider                   ModelProvider
	UsagePricer                     UsagePricer
	SettingsProvider                GatewaySettingsProvider
	BudgetProvider                  BudgetProvider
	ErrorPassthroughRulesProvider   ErrorPassthroughRulesProvider
}

type Proxy struct {
	auth            APIKeyAuthenticator
	accounts        AccountProvider
	client          *http.Client
	upstreamBaseURL string
	codexBaseURL    string
	staticSettings  admin.GatewaySettings
	settings        GatewaySettingsProvider
	limiter         *gatewayConcurrencyLimiter
	accountLimiter  *accountConcurrencyLimiter
	keyLimiter      *apiKeyConcurrencyLimiter
	rateLimiter     *apiKeyRateLimiter
	tokenLimiter    *apiKeyTokenLimiter
	logger          RequestLogger
	models          ModelProvider
	usagePricer     UsagePricer
	budgets         BudgetProvider
	errorRules      ErrorPassthroughRulesProvider
}

func NewProxy(auth APIKeyAuthenticator, accounts AccountProvider, cfg Config) *Proxy {
	return NewProxyWithClient(auth, accounts, cfg, &http.Client{Transport: newTLSFingerprintTransport(http.DefaultTransport)})
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
	staticSettings := admin.GatewaySettings{
		MaxConcurrentGatewayRequests:    cfg.MaxConcurrentGatewayRequests,
		MaxConcurrentRequestsPerAccount: cfg.MaxConcurrentRequestsPerAccount,
		MaxConcurrentRequestsPerKey:     cfg.MaxConcurrentRequestsPerKey,
		RequestsPerMinutePerKey:         cfg.MaxRequestsPerMinutePerKey,
		TokensPerMinutePerKey:           cfg.MaxTokensPerMinutePerKey,
	}
	rateLimiter := newAPIKeyRateLimiter(0, time.Now)
	tokenLimiter := newAPIKeyTokenLimiter(0, time.Now)
	return &Proxy{
		auth:            auth,
		accounts:        accounts,
		client:          client,
		upstreamBaseURL: upstreamBaseURL,
		codexBaseURL:    codexBaseURL,
		staticSettings:  staticSettings,
		settings:        cfg.SettingsProvider,
		limiter:         newGatewayConcurrencyLimiter(),
		accountLimiter:  newAccountConcurrencyLimiter(),
		keyLimiter:      newAPIKeyConcurrencyLimiter(),
		rateLimiter:     rateLimiter,
		tokenLimiter:    tokenLimiter,
		logger:          cfg.Logger,
		models:          cfg.ModelProvider,
		usagePricer:     cfg.UsagePricer,
		budgets:         cfg.BudgetProvider,
		errorRules:      cfg.ErrorPassthroughRulesProvider,
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
	settings := p.gatewaySettings(r.Context())
	recorder := &statusRecorder{ResponseWriter: w}
	startedAt := time.Now()
	errorCode := ""
	var loggedAccount SelectedAccount
	loggedRoutingPoolID, loggedRoutingPoolName := apiKeyRoutingPool(key)
	loggedRoutingPoolFallbackDepth := 0
	loggedRoutingPoolFallbackChain := ""
	loggedRoutingPoolError := ""
	requestModel := ""
	requestSessionID := ""
	observedUsage := Usage{Source: "missing"}
	gatewayAttemptCount := 0
	gatewayFallbackCount := 0
	defer func() {
		if observedUsage.Model == "" {
			observedUsage.Model = requestModel
		}
		p.recordAPIKeyUsage(key.ID, observedUsage.TotalTokens, effectiveAPIKeyLimit(key.TokensPerMinute, settings.TokensPerMinutePerKey))
		costEstimate := p.estimateUsageCost(r.Context(), observedUsage)
		p.logRequest(r.Context(), RequestLog{
			RequestID:                newRequestID(),
			ClientKeyID:              key.ID,
			Provider:                 selectedProviderName(loggedAccount),
			ProviderAccountID:        loggedAccount.AccountID,
			ProviderAccountType:      loggedAccount.AccountType,
			ProviderAccountName:      loggedAccount.DisplayName,
			RoutingPoolID:            loggedRoutingPoolID,
			RoutingPoolName:          loggedRoutingPoolName,
			RoutingPoolFallbackDepth: loggedRoutingPoolFallbackDepth,
			RoutingPoolFallbackChain: loggedRoutingPoolFallbackChain,
			RoutingPoolError:         loggedRoutingPoolError,
			Model:                    requestModel,
			SessionID:                requestSessionID,
			Route:                    r.URL.Path,
			Method:                   r.Method,
			StatusCode:               recorder.statusCode(),
			Latency:                  time.Since(startedAt),
			Error:                    errorCode,
			InputTokens:              observedUsage.InputTokens,
			OutputTokens:             observedUsage.OutputTokens,
			TotalTokens:              observedUsage.TotalTokens,
			CachedInputTokens:        observedUsage.CachedInputTokens,
			ReasoningTokens:          observedUsage.ReasoningTokens,
			UsageSource:              observedUsage.Source,
			EstimatedCostMicrousd:    costEstimate.CostMicrousd,
			PricingSnapshot:          costEstimate.Snapshot,
			GatewayAttemptCount:      gatewayAttemptCount,
			GatewayFallbackCount:     gatewayFallbackCount,
			CreatedAt:                startedAt,
		})
	}()

	if budgetErrorCode, err := p.apiKeyBudgetErrorCode(r.Context(), key, startedAt); err != nil {
		errorCode = "internal_error"
		writeOpenAIError(recorder, http.StatusInternalServerError, errorCode, "could not check api key budget")
		return
	} else if budgetErrorCode != "" {
		errorCode = budgetErrorCode
		writeOpenAIError(recorder, http.StatusTooManyRequests, "rate_limit_exceeded", "api key budget exceeded")
		return
	}
	if retryAfter, ok := p.allowAPIKeyRequest(key.ID, key.RequestsPerMinute, settings.RequestsPerMinutePerKey); !ok {
		errorCode = "api_key_request_rate_limited"
		setRetryAfterHeader(recorder.Header(), retryAfter)
		writeOpenAIError(recorder, http.StatusTooManyRequests, "rate_limit_exceeded", "api key request rate limit exceeded")
		return
	}

	if r.Method == http.MethodGet && r.URL.Path == "/v1/models" {
		if err := p.writeLocalModels(r.Context(), recorder, key); err != nil {
			errorCode = "internal_error"
			writeOpenAIError(recorder, http.StatusInternalServerError, errorCode, "could not list models")
		}
		return
	}
	if key.RoutingPoolID == nil || *key.RoutingPoolID <= 0 {
		errorCode = "routing_pool_required"
		loggedRoutingPoolError = errorCode
		writeOpenAIError(recorder, http.StatusServiceUnavailable, errorCode, "api key must be bound to a routing pool")
		return
	}
	if p.accounts == nil {
		errorCode = "service_unavailable"
		writeOpenAIError(recorder, http.StatusServiceUnavailable, errorCode, "gateway service unavailable")
		return
	}
	if retryAfter, ok := p.allowAPIKeyTokens(key.ID, key.TokensPerMinute, settings.TokensPerMinutePerKey); !ok {
		errorCode = "api_key_token_rate_limited"
		setRetryAfterHeader(recorder.Header(), retryAfter)
		writeOpenAIError(recorder, http.StatusTooManyRequests, "rate_limit_exceeded", "api key token rate limit exceeded")
		return
	}
	bodyFactory, maxAttempts, model, sessionID, err := p.requestBodyFactory(r)
	if err != nil {
		errorCode = requestBodyErrorCode(err)
		writeOpenAIError(recorder, requestBodyErrorStatus(err), errorCode, requestBodyErrorMessage(err))
		return
	}
	requestModel = model
	requestSessionID = sessionID
	if model != "" && !apiKeyAllowsModel(key, model) {
		errorCode = "model_not_found"
		writeOpenAIError(recorder, http.StatusNotFound, errorCode, "requested model is not available")
		return
	}

	release, ok := p.tryAcquireGatewaySlot(settings.MaxConcurrentGatewayRequests)
	if !ok {
		errorCode = "gateway_concurrency_limited"
		writeOpenAIError(recorder, http.StatusTooManyRequests, "rate_limit_exceeded", "gateway concurrency limit exceeded")
		return
	}
	defer release()

	releaseKey, ok := p.tryAcquireAPIKeySlot(key.ID, settings.MaxConcurrentRequestsPerKey)
	if !ok {
		errorCode = "api_key_concurrency_limited"
		writeOpenAIError(recorder, http.StatusTooManyRequests, "rate_limit_exceeded", "api key concurrency limit exceeded")
		return
	}
	defer releaseKey()

	failedAccountIDs := []int64{}
	accountConcurrencyLimited := false
	var lastRetryableResp *http.Response
	for attempt := 0; attempt < maxAttempts; attempt++ {
		selected, err := p.selectAccountForKey(r.Context(), key, model, sessionID, failedAccountIDs...)
		if err != nil {
			loggedRoutingPoolID, loggedRoutingPoolName, loggedRoutingPoolFallbackDepth, loggedRoutingPoolFallbackChain, loggedRoutingPoolError = selectedRoutingPoolLogFields(
				selected,
				loggedRoutingPoolID,
				loggedRoutingPoolName,
			)
			if lastRetryableResp != nil {
				_ = lastRetryableResp.Body.Close()
				lastRetryableResp = nil
			}
			if accountConcurrencyLimited && errors.Is(err, provider.ErrAccountsUnavailable) {
				errorCode = "provider_account_concurrency_limited"
				writeOpenAIError(recorder, http.StatusTooManyRequests, "rate_limit_exceeded", "provider account concurrency limit exceeded")
				return
			}
			errorCode = providerErrorCodeForSelection(err, selected)
			writeOpenAIError(recorder, http.StatusServiceUnavailable, errorCode, providerErrorMessage(errorCode))
			return
		}
		gatewayAttemptCount++
		if lastRetryableResp != nil {
			_ = lastRetryableResp.Body.Close()
			lastRetryableResp = nil
		}
		if selected.AccountID != 0 && containsInt64(failedAccountIDs, selected.AccountID) {
			errorCode = "upstream_unavailable"
			writeOpenAIError(recorder, http.StatusBadGateway, errorCode, "upstream request failed")
			return
		}
		accountLimit := effectiveAccountConcurrencyLimit(selected.MaxConcurrentRequests, settings.MaxConcurrentRequestsPerAccount)
		releaseAccount, ok := p.tryAcquireAccountSlot(selected.AccountID, accountLimit)
		if !ok {
			accountConcurrencyLimited = true
			failedAccountIDs = appendUniqueInt64(failedAccountIDs, selected.AccountID)
			gatewayFallbackCount++
			if attempt+1 < maxAttempts {
				continue
			}
			errorCode = "provider_account_concurrency_limited"
			writeOpenAIError(recorder, http.StatusTooManyRequests, "rate_limit_exceeded", "provider account concurrency limit exceeded")
			return
		}
		if err := p.recordAccountUsed(r.Context(), selected.AccountID); err != nil {
			releaseAccount()
			errorCode = "internal_error"
			writeOpenAIError(recorder, http.StatusInternalServerError, errorCode, "could not record provider account use")
			return
		}
		loggedAccount = selected
		loggedRoutingPoolID, loggedRoutingPoolName, loggedRoutingPoolFallbackDepth, loggedRoutingPoolFallbackChain, loggedRoutingPoolError = selectedRoutingPoolLogFields(
			selected,
			loggedRoutingPoolID,
			loggedRoutingPoolName,
		)

		var upstreamResp *http.Response
		var upstreamErr error
		authorizationRetried := false
		authorizationRefreshFailureRecorded := false
		for {
			upstreamReq, err := p.newUpstreamRequest(r, selected, bodyFactory())
			if err != nil {
				releaseAccount()
				errorCode = "upstream_request_error"
				writeOpenAIError(recorder, http.StatusBadGateway, errorCode, "could not create upstream request")
				return
			}
			upstreamResp, upstreamErr = p.clientForSelectedAccount(selected).Do(upstreamReq)
			if upstreamErr != nil || authorizationRetried || !authorizationFailureStatus(upstreamResp.StatusCode) {
				break
			}

			message, failureBody := captureFailure(upstreamResp)
			if p.shouldPassThroughUpstreamError(r.Context(), upstreamResp.StatusCode, failureBody) {
				break
			}
			refreshedToken, retry, failureRecorded, refreshErr := p.refreshAccountAuthorization(r.Context(), selected, upstreamResp.StatusCode, message)
			if !retry {
				break
			}
			authorizationRetried = true
			if refreshErr != nil || strings.TrimSpace(refreshedToken) == "" {
				authorizationRefreshFailureRecorded = failureRecorded
				break
			}
			_ = upstreamResp.Body.Close()
			selected.AuthorizationToken = refreshedToken
			gatewayAttemptCount++
		}
		if upstreamErr != nil {
			releaseAccount()
			p.recordAccountFailure(r.Context(), selected.AccountID, http.StatusBadGateway, "", upstreamErr.Error())
			failedAccountIDs = appendUniqueInt64(failedAccountIDs, selected.AccountID)
			if attempt+1 < maxAttempts {
				gatewayFallbackCount++
				continue
			}
			errorCode = "upstream_unavailable"
			writeOpenAIError(recorder, http.StatusBadGateway, errorCode, "upstream request failed")
			return
		}

		if retryableUpstreamStatus(upstreamResp.StatusCode) {
			message, failureBody := captureFailure(upstreamResp)
			if p.shouldPassThroughUpstreamError(r.Context(), upstreamResp.StatusCode, failureBody) {
				errorCode = upstreamStatusErrorCode(upstreamResp.StatusCode)
				defer releaseAccount()
				defer upstreamResp.Body.Close()

				streamResponse := shouldStreamUpstreamResponse(r, selected, upstreamResp)
				copyResponseHeaders(recorder.Header(), upstreamResp.Header)
				if streamResponse {
					recorder.Header().Set("Content-Type", "text/event-stream")
				}
				recorder.WriteHeader(upstreamResp.StatusCode)
				observedUsage = copyUpstreamResponse(recorder, upstreamResp, r.URL.Path, streamResponse)
				return
			}
			if !authorizationRefreshFailureRecorded {
				p.recordAccountFailure(r.Context(), selected.AccountID, upstreamResp.StatusCode, upstreamResp.Header.Get("Retry-After"), message)
			}
			failedAccountIDs = appendUniqueInt64(failedAccountIDs, selected.AccountID)
			if attempt+1 < maxAttempts {
				releaseAccount()
				gatewayFallbackCount++
				lastRetryableResp = upstreamResp
				continue
			}
			errorCode = upstreamStatusErrorCode(upstreamResp.StatusCode)
		}
		if upstreamResp.StatusCode >= http.StatusOK && upstreamResp.StatusCode < http.StatusMultipleChoices {
			p.recordAccountRecovered(r.Context(), selected.AccountID)
		}
		defer releaseAccount()
		defer upstreamResp.Body.Close()

		streamResponse := shouldStreamUpstreamResponse(r, selected, upstreamResp)
		copyResponseHeaders(recorder.Header(), upstreamResp.Header)
		if streamResponse {
			recorder.Header().Set("Content-Type", "text/event-stream")
		}
		recorder.WriteHeader(upstreamResp.StatusCode)
		observedUsage = copyUpstreamResponse(recorder, upstreamResp, r.URL.Path, streamResponse)
		return
	}
}

func (p *Proxy) clientForSelectedAccount(selected SelectedAccount) *http.Client {
	proxyURL := strings.TrimSpace(selected.ProxyURL)
	if proxyURL == "" {
		return p.client
	}
	parsed, err := url.Parse(proxyURL)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" {
		return p.client
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(parsed)
	client := &http.Client{Transport: transport}
	if p.client != nil {
		client.Timeout = p.client.Timeout
		client.CheckRedirect = p.client.CheckRedirect
		client.Jar = p.client.Jar
	}
	return client
}

func (p *Proxy) tryAcquireAccountSlot(accountID int64, limit int) (func(), bool) {
	if p.accountLimiter == nil || accountID <= 0 {
		return func() {}, true
	}
	return p.accountLimiter.Acquire(accountID, limit)
}

func (p *Proxy) AccountConcurrencySnapshot() map[int64]int {
	if p.accountLimiter == nil {
		return map[int64]int{}
	}
	return p.accountLimiter.Snapshot()
}

func (p *Proxy) APIKeyConcurrencySnapshot() map[int64]int {
	if p.keyLimiter == nil {
		return map[int64]int{}
	}
	return p.keyLimiter.Snapshot()
}

func (p *Proxy) APIKeyRequestRateSnapshot() map[int64]int {
	if p.rateLimiter == nil {
		return map[int64]int{}
	}
	return p.rateLimiter.Snapshot()
}

func (p *Proxy) APIKeyTokenRateSnapshot() map[int64]int {
	if p.tokenLimiter == nil {
		return map[int64]int{}
	}
	return p.tokenLimiter.Snapshot()
}

func (p *Proxy) tryAcquireAPIKeySlot(keyID int64, limit int) (func(), bool) {
	if p.keyLimiter == nil || keyID <= 0 {
		return func() {}, true
	}
	return p.keyLimiter.Acquire(keyID, limit)
}

func (p *Proxy) tryAcquireGatewaySlot(limit int) (func(), bool) {
	if p.limiter == nil {
		return func() {}, true
	}
	return p.limiter.Acquire(limit)
}

func (p *Proxy) allowAPIKeyRequest(keyID int64, requestsPerMinute, defaultRequestsPerMinute int) (int, bool) {
	if p.rateLimiter == nil {
		return 0, true
	}
	return p.rateLimiter.Allow(keyID, effectiveAPIKeyLimit(requestsPerMinute, defaultRequestsPerMinute))
}

func (p *Proxy) allowAPIKeyTokens(keyID int64, tokensPerMinute, defaultTokensPerMinute int) (int, bool) {
	if p.tokenLimiter == nil {
		return 0, true
	}
	return p.tokenLimiter.Allow(keyID, effectiveAPIKeyLimit(tokensPerMinute, defaultTokensPerMinute))
}

func (p *Proxy) apiKeyBudgetErrorCode(ctx context.Context, key admin.APIKey, now time.Time) (string, error) {
	if p.budgets == nil {
		return "", nil
	}
	if key.RequestBudget24h <= 0 && key.TokenBudget24h <= 0 && key.CostBudgetMicrousd24h <= 0 && key.RequestBudget30d <= 0 && key.TokenBudget30d <= 0 && key.CostBudgetMicrousd30d <= 0 {
		return "", nil
	}
	usage, err := p.budgets.GetAPIKeyBudgetUsage(ctx, key, now)
	if err != nil {
		return "", err
	}
	if usage.RequestBudgetExceeded {
		return "api_key_request_budget_exceeded", nil
	}
	if usage.TokenBudgetExceeded {
		return "api_key_token_budget_exceeded", nil
	}
	if usage.CostBudgetExceeded {
		return "api_key_cost_budget_exceeded", nil
	}
	return "", nil
}

func effectiveAPIKeyLimit(keyLimit, defaultLimit int) int {
	if keyLimit > 0 {
		return keyLimit
	}
	return defaultLimit
}

func effectiveAccountConcurrencyLimit(accountLimit, defaultLimit int) int {
	if accountLimit > 0 {
		return accountLimit
	}
	return defaultLimit
}

func setRetryAfterHeader(header http.Header, seconds int) {
	if seconds <= 0 {
		return
	}
	header.Set("Retry-After", strconv.Itoa(seconds))
}

func (p *Proxy) recordAPIKeyUsage(keyID int64, tokens, tokensPerMinute int) {
	if p.tokenLimiter == nil || tokens <= 0 {
		return
	}
	p.tokenLimiter.Record(keyID, tokens, tokensPerMinute)
}

func (p *Proxy) gatewaySettings(ctx context.Context) admin.GatewaySettings {
	if p.settings == nil {
		return p.staticSettings
	}
	settings, err := p.settings.GetGatewaySettings(ctx)
	if err != nil {
		return p.staticSettings
	}
	return settings
}

func (p *Proxy) selectAccountForKey(ctx context.Context, key admin.APIKey, model, sessionID string, excludedAccountIDs ...int64) (SelectedAccount, error) {
	if key.RoutingPoolID != nil && *key.RoutingPoolID > 0 {
		if chainProvider, ok := p.accounts.(RoutingPoolChainAccountProvider); ok {
			if strings.TrimSpace(sessionID) != "" {
				return chainProvider.SelectAccountForModelAndSessionInRoutingPoolChain(ctx, *key.RoutingPoolID, model, sessionID, excludedAccountIDs...)
			}
			return chainProvider.SelectAccountForModelInRoutingPoolChain(ctx, *key.RoutingPoolID, model, excludedAccountIDs...)
		}
		poolProvider, ok := p.accounts.(RoutingPoolAccountProvider)
		if !ok {
			return SelectedAccount{}, provider.ErrAccountsUnavailable
		}
		if strings.TrimSpace(sessionID) != "" {
			return poolProvider.SelectAccountForModelAndSessionInRoutingPool(ctx, *key.RoutingPoolID, model, sessionID, excludedAccountIDs...)
		}
		return poolProvider.SelectAccountForModelInRoutingPool(ctx, *key.RoutingPoolID, model, excludedAccountIDs...)
	}
	return p.selectGlobalAccount(ctx, model, sessionID, excludedAccountIDs...)
}

func (p *Proxy) selectGlobalAccount(ctx context.Context, model, sessionID string, excludedAccountIDs ...int64) (SelectedAccount, error) {
	if sessionID != "" {
		if sticky, ok := p.accounts.(StickyAccountProvider); ok {
			return sticky.SelectAccountForModelAndSession(ctx, model, sessionID, excludedAccountIDs...)
		}
	}
	return p.accounts.SelectAccountForModel(ctx, model, excludedAccountIDs...)
}

func apiKeyRoutingPool(key admin.APIKey) (int64, string) {
	if key.RoutingPoolID == nil || *key.RoutingPoolID <= 0 {
		return 0, ""
	}
	return *key.RoutingPoolID, strings.TrimSpace(key.RoutingPoolName)
}

func selectedRoutingPoolLogFields(selected SelectedAccount, fallbackID int64, fallbackName string) (int64, string, int, string, string) {
	routingPoolID := fallbackID
	routingPoolName := strings.TrimSpace(fallbackName)
	if selected.RoutingPoolID > 0 {
		routingPoolID = selected.RoutingPoolID
	}
	if strings.TrimSpace(selected.RoutingPoolName) != "" {
		routingPoolName = strings.TrimSpace(selected.RoutingPoolName)
	}
	return routingPoolID,
		routingPoolName,
		selected.RoutingPoolFallbackDepth,
		strings.TrimSpace(selected.RoutingPoolFallbackChain),
		strings.TrimSpace(selected.RoutingPoolError)
}

func (p *Proxy) recordAccountUsed(ctx context.Context, accountID int64) error {
	if recorder, ok := p.accounts.(AccountUsageRecorder); ok {
		return recorder.RecordAccountUsed(ctx, accountID)
	}
	return nil
}

func (p *Proxy) recordAccountRecovered(ctx context.Context, accountID int64) {
	if accountID <= 0 {
		return
	}
	recorder, ok := p.accounts.(AccountRecoveryRecorder)
	if !ok {
		return
	}
	recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), accountRecoveryWriteTimeout)
	defer cancel()
	_ = recorder.RecordAccountRecovered(recoveryCtx, accountID)
}

type gatewayConcurrencyLimiter struct {
	mu       sync.Mutex
	inFlight int
}

func newGatewayConcurrencyLimiter() *gatewayConcurrencyLimiter {
	return &gatewayConcurrencyLimiter{}
}

func (l *gatewayConcurrencyLimiter) Acquire(limit int) (func(), bool) {
	if l == nil || limit <= 0 {
		return func() {}, true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inFlight >= limit {
		return nil, false
	}
	l.inFlight++
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.inFlight--
	}, true
}

type accountConcurrencyLimiter struct {
	mu       sync.Mutex
	inFlight map[int64]int
}

func newAccountConcurrencyLimiter() *accountConcurrencyLimiter {
	return &accountConcurrencyLimiter{inFlight: map[int64]int{}}
}

func (l *accountConcurrencyLimiter) Acquire(accountID int64, limit int) (func(), bool) {
	if l == nil || limit <= 0 || accountID <= 0 {
		return func() {}, true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inFlight[accountID] >= limit {
		return nil, false
	}
	l.inFlight[accountID]++
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.inFlight[accountID]--
		if l.inFlight[accountID] <= 0 {
			delete(l.inFlight, accountID)
		}
	}, true
}

func (l *accountConcurrencyLimiter) Snapshot() map[int64]int {
	if l == nil {
		return map[int64]int{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	snapshot := make(map[int64]int, len(l.inFlight))
	for accountID, count := range l.inFlight {
		if count > 0 {
			snapshot[accountID] = count
		}
	}
	return snapshot
}

type apiKeyConcurrencyLimiter struct {
	mu       sync.Mutex
	inFlight map[int64]int
}

func newAPIKeyConcurrencyLimiter() *apiKeyConcurrencyLimiter {
	return &apiKeyConcurrencyLimiter{inFlight: map[int64]int{}}
}

func (l *apiKeyConcurrencyLimiter) Acquire(keyID int64, limit int) (func(), bool) {
	if l == nil || limit <= 0 || keyID <= 0 {
		return func() {}, true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inFlight[keyID] >= limit {
		return nil, false
	}
	l.inFlight[keyID]++
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.inFlight[keyID]--
		if l.inFlight[keyID] <= 0 {
			delete(l.inFlight, keyID)
		}
	}, true
}

func (l *apiKeyConcurrencyLimiter) Snapshot() map[int64]int {
	if l == nil {
		return map[int64]int{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	snapshot := make(map[int64]int, len(l.inFlight))
	for keyID, count := range l.inFlight {
		if count > 0 {
			snapshot[keyID] = count
		}
	}
	return snapshot
}

type apiKeyRateLimiter struct {
	defaultLimit int
	now          func() time.Time
	mu           sync.Mutex
	keys         map[int64]apiKeyRateWindow
}

type apiKeyRateWindow struct {
	start time.Time
	count int
}

func newAPIKeyRateLimiter(limit int, now func() time.Time) *apiKeyRateLimiter {
	return &apiKeyRateLimiter{
		defaultLimit: limit,
		now:          now,
		keys:         map[int64]apiKeyRateWindow{},
	}
}

func (l *apiKeyRateLimiter) Allow(keyID int64, limit int) (int, bool) {
	effectiveLimit := l.effectiveLimit(limit)
	if effectiveLimit <= 0 {
		return 0, true
	}
	now := l.now()
	windowStart := now.Truncate(time.Minute)

	l.mu.Lock()
	defer l.mu.Unlock()
	window := l.keys[keyID]
	if window.start.IsZero() || !window.start.Equal(windowStart) {
		l.keys[keyID] = apiKeyRateWindow{start: windowStart, count: 1}
		return 0, true
	}
	if window.count >= effectiveLimit {
		return secondsUntilNextMinute(now), false
	}
	window.count++
	l.keys[keyID] = window
	return 0, true
}

func (l *apiKeyRateLimiter) effectiveLimit(limit int) int {
	if l == nil {
		return 0
	}
	if limit > 0 {
		return limit
	}
	return l.defaultLimit
}

func (l *apiKeyRateLimiter) Snapshot() map[int64]int {
	if l == nil {
		return map[int64]int{}
	}
	now := l.now()
	windowStart := now.Truncate(time.Minute)
	l.mu.Lock()
	defer l.mu.Unlock()
	snapshot := make(map[int64]int, len(l.keys))
	for keyID, window := range l.keys {
		if window.start.Equal(windowStart) && window.count > 0 {
			snapshot[keyID] = window.count
		}
	}
	return snapshot
}

type apiKeyTokenLimiter struct {
	defaultLimit int
	now          func() time.Time
	mu           sync.Mutex
	keys         map[int64]apiKeyTokenWindow
}

type apiKeyTokenWindow struct {
	start  time.Time
	tokens int
}

func newAPIKeyTokenLimiter(limit int, now func() time.Time) *apiKeyTokenLimiter {
	return &apiKeyTokenLimiter{
		defaultLimit: limit,
		now:          now,
		keys:         map[int64]apiKeyTokenWindow{},
	}
}

func (l *apiKeyTokenLimiter) Allow(keyID int64, limit int) (int, bool) {
	effectiveLimit := l.effectiveLimit(limit)
	if effectiveLimit <= 0 {
		return 0, true
	}
	now := l.now()
	windowStart := now.Truncate(time.Minute)
	l.mu.Lock()
	defer l.mu.Unlock()
	window := l.keys[keyID]
	if window.start.IsZero() || !window.start.Equal(windowStart) {
		return 0, true
	}
	if window.tokens >= effectiveLimit {
		return secondsUntilNextMinute(now), false
	}
	return 0, true
}

func (l *apiKeyTokenLimiter) Record(keyID int64, tokens, limit int) {
	if l.effectiveLimit(limit) <= 0 || tokens <= 0 {
		return
	}
	now := l.now()
	windowStart := now.Truncate(time.Minute)
	l.mu.Lock()
	defer l.mu.Unlock()
	window := l.keys[keyID]
	if window.start.IsZero() || !window.start.Equal(windowStart) {
		l.keys[keyID] = apiKeyTokenWindow{start: windowStart, tokens: tokens}
		return
	}
	window.tokens += tokens
	l.keys[keyID] = window
}

func (l *apiKeyTokenLimiter) effectiveLimit(limit int) int {
	if l == nil {
		return 0
	}
	if limit > 0 {
		return limit
	}
	return l.defaultLimit
}

func (l *apiKeyTokenLimiter) Snapshot() map[int64]int {
	if l == nil {
		return map[int64]int{}
	}
	now := l.now()
	windowStart := now.Truncate(time.Minute)
	l.mu.Lock()
	defer l.mu.Unlock()
	snapshot := make(map[int64]int, len(l.keys))
	for keyID, window := range l.keys {
		if window.start.Equal(windowStart) && window.tokens > 0 {
			snapshot[keyID] = window.tokens
		}
	}
	return snapshot
}

func secondsUntilNextMinute(now time.Time) int {
	next := now.Truncate(time.Minute).Add(time.Minute)
	seconds := int(next.Sub(now).Seconds())
	if seconds < 1 {
		return 1
	}
	return seconds
}

func copyUpstreamResponse(w http.ResponseWriter, resp *http.Response, route string, stream bool) Usage {
	if resp == nil || resp.Body == nil {
		return Usage{Source: "missing"}
	}
	if stream {
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

func (p *Proxy) refreshAccountAuthorization(ctx context.Context, selected SelectedAccount, statusCode int, message string) (string, bool, bool, error) {
	if selected.AccountID <= 0 || selected.AccountType != provider.AccountTypeCodexOAuth {
		return "", false, false, nil
	}
	refresher, ok := p.accounts.(AccountAuthorizationRefresher)
	if !ok {
		return "", false, false, nil
	}
	return refresher.RefreshAccountAuthorization(ctx, selected.AccountID, selected.AuthorizationToken, statusCode, message)
}

func containsInt64(values []int64, target int64) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func appendUniqueInt64(values []int64, value int64) []int64 {
	if value == 0 || containsInt64(values, value) {
		return values
	}
	return append(values, value)
}

func (p *Proxy) writeLocalModels(ctx context.Context, w http.ResponseWriter, key admin.APIKey) error {
	models := []ExposedModel{}
	if p.models != nil && key.RoutingPoolID != nil && *key.RoutingPoolID > 0 {
		poolModels, ok := p.models.(RoutingPoolModelProvider)
		if !ok {
			return writeModelList(w, key, models)
		}
		var err error
		models, err = poolModels.ListExposedModelsForRoutingPoolChain(ctx, *key.RoutingPoolID)
		if err != nil {
			return err
		}
	}
	return writeModelList(w, key, models)
}

func writeModelList(w http.ResponseWriter, key admin.APIKey, models []ExposedModel) error {
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

func authorizationFailureStatus(statusCode int) bool {
	return statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden
}

func upstreamStatusErrorCode(statusCode int) string {
	switch statusCode {
	case http.StatusUnauthorized:
		return "upstream_unauthorized"
	case http.StatusForbidden:
		return "upstream_forbidden"
	case http.StatusTooManyRequests:
		return "upstream_rate_limited"
	default:
		if statusCode >= http.StatusInternalServerError {
			return "upstream_unavailable"
		}
		return "upstream_error"
	}
}

func (p *Proxy) shouldPassThroughUpstreamError(ctx context.Context, statusCode int, failureBody string) bool {
	if p.errorRules == nil {
		return false
	}
	rules, err := p.errorRules.ListErrorPassthroughRules(ctx)
	if err != nil {
		return false
	}
	errorCode, errorMessage := upstreamErrorFields(failureBody)
	if strings.TrimSpace(errorMessage) == "" {
		errorMessage = failureBody
	}
	status := strconv.Itoa(statusCode)
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		pattern := strings.TrimSpace(rule.Pattern)
		if pattern == "" {
			continue
		}
		switch rule.MatchType {
		case "status_code":
			if pattern == status {
				return true
			}
		case "error_code":
			if strings.EqualFold(pattern, strings.TrimSpace(errorCode)) {
				return true
			}
		case "error_message":
			if strings.Contains(strings.ToLower(errorMessage), strings.ToLower(pattern)) {
				return true
			}
		}
	}
	return false
}

func upstreamErrorFields(body string) (string, string) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", ""
	}
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return "", ""
	}
	return strings.TrimSpace(payload.Error.Code), strings.TrimSpace(payload.Error.Message)
}

func captureFailure(resp *http.Response) (string, string) {
	if resp == nil || resp.Body == nil {
		return "", ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFailureBody+1))
	if err != nil {
		return "", ""
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	failureBody := string(bytes.TrimSpace(body))
	message := http.StatusText(resp.StatusCode)
	if strings.TrimSpace(failureBody) == "" {
		return message, failureBody
	}
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && strings.TrimSpace(payload.Error.Message) != "" {
		return strings.TrimSpace(payload.Error.Message), failureBody
	}
	if len(body) > maxFailureBody {
		body = body[:maxFailureBody]
	}
	return string(bytes.TrimSpace(body)), failureBody
}

func captureFailureMessage(resp *http.Response) string {
	message, _ := captureFailure(resp)
	return message
}

func (p *Proxy) newUpstreamRequest(r *http.Request, selected SelectedAccount, body io.ReadCloser) (*http.Request, error) {
	useCodexEndpoint := usesCodexResponsesEndpoint(r, selected)
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
		req.Header.Set("originator", provider.DefaultCodexFingerprintOriginator)
		req.Header.Set("User-Agent", defaultCodexUserAgent)
		req.Header.Set("Version", provider.DefaultCodexFingerprintVersion)
		req.Header.Set("Content-Type", "application/json")
	}

	// Apply fingerprint profile overrides (User-Agent and custom headers)
	if strings.TrimSpace(selected.FingerprintUA) != "" {
		req.Header.Set("User-Agent", strings.TrimSpace(selected.FingerprintUA))
	}
	for key, value := range selected.FingerprintHeaders {
		req.Header.Set(key, value)
	}
	if strings.TrimSpace(selected.FingerprintTLS) != "" {
		req = req.WithContext(contextWithTLSFingerprint(req.Context(), selected.FingerprintTLS))
	}

	return req, nil
}

func usesCodexResponsesEndpoint(r *http.Request, selected SelectedAccount) bool {
	return r != nil &&
		selected.AccountType == provider.AccountTypeCodexOAuth &&
		r.Method == http.MethodPost &&
		r.URL.Path == "/v1/responses" &&
		strings.TrimSpace(selected.ChatGPTAccountID) != ""
}

func shouldStreamUpstreamResponse(r *http.Request, selected SelectedAccount, resp *http.Response) bool {
	if resp == nil {
		return false
	}
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		return true
	}
	return resp.StatusCode >= http.StatusOK &&
		resp.StatusCode < http.StatusMultipleChoices &&
		usesCodexResponsesEndpoint(r, selected)
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

func (p *Proxy) requestBodyFactory(r *http.Request) (func() io.ReadCloser, int, string, string, error) {
	if r.Method != http.MethodPost || r.Body == nil {
		return func() io.ReadCloser { return nil }, maxReplayableAttempts, "", stickySessionIDFromHeader(r.Header), nil
	}

	limitedBody, err := io.ReadAll(io.LimitReader(r.Body, maxReplayableRequestBody+1))
	if err != nil {
		return nil, 0, "", "", err
	}
	if len(limitedBody) > maxReplayableRequestBody {
		_ = r.Body.Close()
		return nil, 0, "", "", errReplayBodyTooLarge
	}
	_ = r.Body.Close()
	model := ""
	sessionID := ""
	if routeRequiresModel(r) {
		var body []byte
		body, model, sessionID, err = p.normalizeModelRequestBody(r.Context(), limitedBody)
		if err != nil {
			return nil, 0, "", "", err
		}
		limitedBody = body
	}
	if sessionID == "" {
		sessionID = stickySessionIDFromHeader(r.Header)
	}
	return func() io.ReadCloser {
		return io.NopCloser(bytes.NewReader(limitedBody))
	}, maxReplayableAttempts, model, sessionID, nil
}

func stickySessionIDFromHeader(header http.Header) string {
	if sessionID := strings.TrimSpace(header.Get("session_id")); sessionID != "" {
		return sessionID
	}
	return strings.TrimSpace(header.Get("X-N2API-Session-ID"))
}

func routeRequiresModel(r *http.Request) bool {
	return r.Method == http.MethodPost && (r.URL.Path == "/v1/chat/completions" || r.URL.Path == "/v1/responses")
}

func (p *Proxy) normalizeModelRequestBody(ctx context.Context, raw []byte) ([]byte, string, string, error) {
	payload := map[string]any{}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, "", "", errInvalidJSONBody
		}
	}

	rawModel, hasModel := payload["model"]
	model := ""
	if hasModel {
		modelValue, ok := rawModel.(string)
		if !ok {
			return nil, "", "", errInvalidJSONBody
		}
		model = strings.TrimSpace(modelValue)
	}
	sessionID := ""
	if rawSessionID, ok := payload["session_id"]; ok {
		sessionValue, ok := rawSessionID.(string)
		if !ok {
			return nil, "", "", errInvalidJSONBody
		}
		sessionID = strings.TrimSpace(sessionValue)
	}
	if model == "" {
		defaultModel, err := p.defaultModel(ctx)
		if err != nil {
			return nil, "", "", err
		}
		model = defaultModel
		payload["model"] = model
		raw, err = json.Marshal(payload)
		if err != nil {
			return nil, "", "", err
		}
	} else if rawModel != model {
		payload["model"] = model
		normalized, err := json.Marshal(payload)
		if err != nil {
			return nil, "", "", err
		}
		raw = normalized
	}

	return raw, model, sessionID, nil
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
	case errors.Is(err, provider.ErrRoutingPoolCycle):
		return "routing_pool_cycle"
	case errors.Is(err, provider.ErrRoutingPoolNotFound):
		return "routing_pool_unavailable"
	case errors.Is(err, provider.ErrRoutingPoolEmpty):
		return "routing_pool_empty"
	case errors.Is(err, provider.ErrRoutingPoolExhausted):
		return "routing_pool_exhausted"
	case errors.Is(err, provider.ErrAccountsUnavailable):
		return "provider_accounts_unavailable"
	case errors.Is(err, provider.ErrModelUnavailable):
		return "model_unavailable"
	default:
		return "upstream_token_error"
	}
}

func providerErrorCodeForSelection(err error, selected SelectedAccount) string {
	if errors.Is(err, provider.ErrAccountsDisabled) && strings.TrimSpace(selected.RoutingPoolError) == provider.RoutingPoolErrorDisabled {
		return provider.RoutingPoolErrorDisabled
	}
	if errors.Is(err, provider.ErrModelUnavailable) && strings.TrimSpace(selected.RoutingPoolError) == provider.RoutingPoolErrorExhausted {
		return provider.RoutingPoolErrorExhausted
	}
	return providerErrorCode(err)
}

func providerErrorMessage(code string) string {
	switch code {
	case "provider_not_connected":
		return "provider account is not connected"
	case "provider_not_configured":
		return "provider account is not configured"
	case "provider_accounts_disabled":
		return "provider accounts are disabled"
	case "routing_pool_cycle":
		return "routing pool fallback chain contains a cycle"
	case "routing_pool_unavailable":
		return "routing pool is unavailable"
	case "routing_pool_empty":
		return "routing pool has no eligible accounts"
	case "routing_pool_disabled":
		return "routing pool is disabled"
	case "routing_pool_exhausted":
		return "routing pool fallback chain is exhausted"
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
	logCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), requestLogWriteTimeout)
	defer cancel()
	_ = p.logger.CreateRequestLog(logCtx, entry)
}

func selectedProviderName(account SelectedAccount) string {
	providerName := strings.TrimSpace(account.Provider)
	if providerName != "" {
		return providerName
	}
	return "openai"
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
