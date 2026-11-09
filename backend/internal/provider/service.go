package provider

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

	"github.com/KnowSky404/N2API/backend/internal/secret"
	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

const (
	defaultCircuitOpen = 5 * time.Minute
	defaultManualPause = 5 * time.Minute
	maxManualPause     = 24 * time.Hour
)

func withProviderEventIntent(ctx context.Context, intent systemevent.EventIntent) context.Context {
	if parent, ok := systemevent.IntentFromContext(ctx); ok {
		if batchID, ok := parent.Metadata["batch_id"].(string); ok && strings.TrimSpace(batchID) != "" {
			if intent.Metadata == nil {
				intent.Metadata = map[string]any{}
			}
			intent.Metadata["batch_id"] = batchID
		}
	}
	return systemevent.WithIntent(ctx, intent)
}

func providerAccountTarget(id int64, name string) systemevent.Target {
	target := systemevent.Target{Type: "provider_account", Name: strings.TrimSpace(name)}
	if id > 0 {
		target.ID = strconv.FormatInt(id, 10)
	}
	return target
}

func providerAuditIntent(action systemevent.Action, id int64, name string) systemevent.EventIntent {
	return systemevent.EventIntent{
		Category: systemevent.CategoryAudit,
		Severity: systemevent.SeverityInfo,
		Action:   action,
		Outcome:  systemevent.OutcomeSuccess,
		Target:   providerAccountTarget(id, name),
	}
}

func runtimeAccountIntent(action systemevent.Action, id int64, status string) systemevent.EventIntent {
	severity := systemevent.SeverityWarning
	outcome := systemevent.OutcomeFailure
	if action == systemevent.ActionProviderAccountRecovered {
		severity = systemevent.SeverityInfo
		outcome = systemevent.OutcomeSuccess
	}
	return systemevent.EventIntent{
		Category: systemevent.CategoryRuntime,
		Severity: severity,
		Action:   action,
		Outcome:  outcome,
		Target:   providerAccountTarget(id, ""),
		Metadata: map[string]any{"status": status},
	}
}

const (
	AccountTypeCodexOAuth  = "codex_oauth"
	AccountTypeAPIUpstream = "api_upstream"
)

const (
	RoutingPoolErrorDisabled    = "routing_pool_disabled"
	RoutingPoolErrorUnavailable = "routing_pool_unavailable"
	RoutingPoolErrorEmpty       = "routing_pool_empty"
	RoutingPoolErrorExhausted   = "routing_pool_exhausted"
	RoutingPoolErrorCycle       = "routing_pool_cycle"
)

const (
	CredentialTypeOAuthToken = "oauth_token"
	CredentialTypeAPIKey     = "api_key"
)

const (
	AccountStatusActive      = "active"
	AccountStatusDisabled    = "disabled"
	AccountStatusRateLimited = "rate_limited"
	AccountStatusCircuitOpen = "circuit_open"
	AccountStatusExpired     = "expired"
)

const (
	AccountTestStatusPassed = "passed"
	AccountTestStatusFailed = "failed"

	defaultAccountTestResultsLimit = 20
	maxAccountTestResultsLimit     = 100
	accountTestRequestLogTimeout   = 5 * time.Second
)

const (
	AccountModelSourceManual       = "manual"
	AccountModelSourceUpstream     = "upstream"
	AccountModelSourceOAuthCatalog = "oauth_catalog"

	maxAccountModels        = 100
	maxModelNameLen         = 128
	accountModelTestTimeout = 20 * time.Second
	maxModelProbeBody       = 4 << 20
	maxModelProbeErrorBody  = 64 << 10
)

var (
	ErrNotConfigured          = errors.New("provider not configured")
	ErrNotConnected           = errors.New("provider not connected")
	ErrInvalidState           = errors.New("invalid oauth state")
	ErrInvalidInput           = errors.New("invalid provider input")
	ErrAccountsDisabled       = errors.New("provider accounts disabled")
	ErrAccountsUnavailable    = errors.New("provider accounts unavailable")
	ErrModelUnavailable       = errors.New("model unavailable")
	ErrSessionBindingNotFound = errors.New("provider session binding not found")
	ErrRoutingPoolNotFound    = errors.New("routing pool not found")
	ErrRoutingPoolEmpty       = errors.New("routing pool empty")
	ErrRoutingPoolCycle       = errors.New("routing pool fallback cycle")
	ErrRoutingPoolExhausted   = errors.New("routing pool fallback chain exhausted")
	ErrEndpointUnavailable    = errors.New("provider endpoint unavailable")
)

type Config struct {
	Provider              string
	APIBaseURL            string
	ProxyURL              string
	Secret                string
	EncryptionKeyring     *secret.Keyring
	AllowHTTPAPIUpstreams bool
	AccountTestLogger     AccountTestRequestLogger
	RequestLogObserver    RequestLogWriteObserver
	Metrics               MetricsObserver
}

type MetricsObserver interface {
	ObserveProviderRefresh(mode, outcome string)
}

type Status struct {
	Provider             string     `json:"provider"`
	Configured           bool       `json:"configured"`
	Connected            bool       `json:"connected"`
	DisplayName          string     `json:"displayName"`
	AccessTokenExpiresAt *time.Time `json:"accessTokenExpiresAt"`
	LastRefreshAt        *time.Time `json:"lastRefreshAt"`
}

type OAuthState struct {
	Provider                    string
	StateHash                   string
	RedirectAfter               string
	ExpiresAt                   time.Time
	ConsumedAt                  *time.Time
	CodeVerifier                string `json:"-"`
	EncryptedCodeVerifier       string `json:"-"`
	CodeVerifierHash            string
	ClientID                    string
	TargetAccountID             int64
	PendingAccountName          string
	PendingPriority             int
	PendingEnabled              *bool
	PendingFingerprintProfileID *int64
	FingerprintHash             string
	UserAgentHash               string
	IPHash                      string
}

type Account struct {
	ID                    int64             `json:"id"`
	Provider              string            `json:"provider"`
	AccountType           string            `json:"accountType"`
	Subject               string            `json:"subject"`
	Name                  string            `json:"name"`
	DisplayName           string            `json:"displayName"`
	BaseURL               string            `json:"baseUrl,omitempty"`
	ProxyURLConfigured    bool              `json:"proxyUrlConfigured"`
	ProxyURLSummary       string            `json:"proxyUrlSummary,omitempty"`
	Credential            AccountCredential `json:"-"`
	EncryptedAccessToken  string            `json:"-"`
	EncryptedRefreshToken string            `json:"-"`
	EncryptedIDToken      string            `json:"-"`
	AccessTokenExpiresAt  *time.Time        `json:"accessTokenExpiresAt"`
	LastRefreshAt         *time.Time        `json:"lastRefreshAt"`
	Enabled               bool              `json:"enabled"`
	Priority              int               `json:"priority"`
	GlobalPriority        int               `json:"-"`
	RoutingPoolPriority   *int              `json:"-"`
	LoadFactor            int               `json:"loadFactor"`
	MaxConcurrentRequests int               `json:"maxConcurrentRequests"`
	LastUsedAt            *time.Time        `json:"lastUsedAt"`
	LastError             string            `json:"lastError"`
	LastErrorAt           *time.Time        `json:"lastErrorAt"`
	LastTestAt            *time.Time        `json:"lastTestAt"`
	LastTestStatus        string            `json:"lastTestStatus"`
	LastTestError         string            `json:"lastTestError"`
	Metadata              map[string]string `json:"metadata"`
	Status                string            `json:"status"`
	StatusReason          string            `json:"statusReason"`
	FingerprintHash       string            `json:"-"`
	UserAgentHash         string            `json:"userAgentHash"`
	IPHash                string            `json:"ipHash"`
	FailureCount          int               `json:"failureCount"`
	CircuitOpenUntil      *time.Time        `json:"circuitOpenUntil"`
	RateLimitedUntil      *time.Time        `json:"rateLimitedUntil"`
	FingerprintProfileID  *int64            `json:"-"`
	LastRefreshError      string            `json:"lastRefreshError"`
	LastRefreshErrorAt    *time.Time        `json:"lastRefreshErrorAt"`
	CreatedAt             time.Time         `json:"createdAt"`
	UpdatedAt             time.Time         `json:"updatedAt"`
}

type AccountTestResult struct {
	ID        int64     `json:"id"`
	AccountID int64     `json:"accountId"`
	Provider  string    `json:"provider"`
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	CheckedAt time.Time `json:"checkedAt"`
	CreatedAt time.Time `json:"createdAt"`
}

type AccountTestRequestLog struct {
	RequestID           string
	Provider            string
	ProviderAccountID   int64
	ProviderAccountType string
	ProviderAccountName string
	Model               string
	Route               string
	Method              string
	StatusCode          int
	Latency             time.Duration
	Error               string
	CreatedAt           time.Time
}

type AccountTestRequestLogger interface {
	CreateAccountTestRequestLog(ctx context.Context, entry AccountTestRequestLog) error
}

type SessionBinding struct {
	ID         int64     `json:"id"`
	Provider   string    `json:"provider"`
	Model      string    `json:"model"`
	SessionID  string    `json:"sessionId"`
	AccountID  int64     `json:"accountId"`
	LastUsedAt time.Time `json:"lastUsedAt"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type RoutingPool struct {
	ID             int64
	Name           string
	Enabled        bool
	FallbackPoolID *int64
}

type RoutingPoolAccount struct {
	AccountID int64
	Priority  int
}

type AccountCredential struct {
	CredentialType        string            `json:"credentialType"`
	EncryptedAccessToken  string            `json:"-"`
	EncryptedRefreshToken string            `json:"-"`
	EncryptedIDToken      string            `json:"-"`
	AccessTokenExpiresAt  *time.Time        `json:"accessTokenExpiresAt"`
	LastRefreshAt         *time.Time        `json:"lastRefreshAt"`
	LastRefreshError      string            `json:"lastRefreshError"`
	LastRefreshErrorAt    *time.Time        `json:"lastRefreshErrorAt"`
	EncryptedAPIKey       string            `json:"-"`
	EncryptedProxyURL     string            `json:"-"`
	BaseURL               string            `json:"baseUrl"`
	Metadata              map[string]string `json:"metadata"`
}

type AccountModel struct {
	ID                 int64             `json:"id"`
	AccountID          int64             `json:"accountId"`
	Provider           string            `json:"provider"`
	Model              string            `json:"model"`
	Enabled            bool              `json:"enabled"`
	Source             string            `json:"source"`
	LastSeenAt         *time.Time        `json:"lastSeenAt"`
	LastError          string            `json:"lastError"`
	LastTestAt         *time.Time        `json:"lastTestAt"`
	LastTestStatus     string            `json:"lastTestStatus"`
	LastTestHTTPStatus int               `json:"lastTestHttpStatus"`
	LastTestLatencyMS  int64             `json:"lastTestLatencyMs"`
	Metadata           map[string]string `json:"metadata"`
	CreatedAt          time.Time         `json:"createdAt"`
	UpdatedAt          time.Time         `json:"updatedAt"`
}

type AccountModelInput struct {
	Model    string            `json:"model"`
	Enabled  bool              `json:"enabled"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type AccountModelTestResult struct {
	AccountID  int64     `json:"accountId"`
	Model      string    `json:"model"`
	Status     string    `json:"status"`
	ErrorCode  string    `json:"errorCode"`
	HTTPStatus int       `json:"httpStatus"`
	LatencyMS  int64     `json:"latencyMs"`
	Message    string    `json:"message"`
	CheckedAt  time.Time `json:"checkedAt"`
}

// AccountModelSyncSummary describes what happened during a sync operation.
type AccountModelSyncSummary struct {
	Total         int `json:"total"`
	New           int `json:"new"`
	Preserved     int `json:"preserved"`
	SkippedManual int `json:"skippedManual"`
}

type APIUpstreamInput struct {
	Name                 string   `json:"name"`
	BaseURL              string   `json:"baseUrl"`
	APIKey               string   `json:"apiKey"`
	ProxyURL             string   `json:"proxyUrl"`
	Enabled              *bool    `json:"enabled"`
	Priority             int      `json:"priority"`
	LoadFactor           int      `json:"loadFactor"`
	FingerprintProfileID *int64   `json:"-"`
	Models               []string `json:"models"`
}

type ExposedModel struct {
	ID      string `json:"id"`
	OwnedBy string `json:"ownedBy"`
}

type AccountUpdate struct {
	Enabled                    *bool
	Priority                   *int
	LoadFactor                 *int
	MaxConcurrentRequests      *int
	ClearStatus                bool
	Name                       *string
	APIUpstreamBaseURL         *string
	APIUpstreamAPIKey          *string
	EncryptedAPIUpstreamAPIKey *string
	ProxyURL                   *string
	EncryptedProxyURL          *string
	FingerprintProfileIDSet    bool
	FingerprintProfileID       *int64
}

type SelectedAccount struct {
	AccountID                int64
	Provider                 string
	AccountType              string
	DisplayName              string
	AuthorizationToken       string
	BaseURL                  string
	ProxyURL                 string
	MaxConcurrentRequests    int
	RoutingPoolID            int64
	RoutingPoolName          string
	RoutingPoolFallbackDepth int
	RoutingPoolFallbackChain string
	RoutingPoolError         string
}

type SelectionPreview struct {
	Model                    string `json:"model"`
	Endpoint                 string `json:"endpoint,omitempty"`
	SessionID                string `json:"sessionId"`
	SelectedAccountID        int64  `json:"selectedAccountId"`
	StickyBoundAccountID     int64  `json:"stickyBoundAccountId,omitempty"`
	RoutingPoolID            int64  `json:"routingPoolId,omitempty"`
	RoutingPoolName          string `json:"routingPoolName,omitempty"`
	RoutingPoolFallbackDepth int    `json:"routingPoolFallbackDepth,omitempty"`
	RoutingPoolFallbackChain string `json:"routingPoolFallbackChain,omitempty"`
	RoutingPoolError         string
	Candidates               []SelectionCandidate `json:"candidates"`
}

type SelectionCandidate struct {
	ID                  int64      `json:"id"`
	DisplayName         string     `json:"displayName"`
	AccountType         string     `json:"accountType"`
	Priority            int        `json:"priority"`
	LoadFactor          int        `json:"loadFactor"`
	Status              string     `json:"status"`
	LastUsedAt          *time.Time `json:"lastUsedAt"`
	LastTestAt          *time.Time `json:"lastTestAt"`
	LastTestStatus      string     `json:"lastTestStatus"`
	LastTestError       string     `json:"lastTestError"`
	ScheduleRank        int        `json:"scheduleRank"`
	ScheduleReason      string     `json:"scheduleReason"`
	Selected            bool       `json:"selected"`
	StickyBound         bool       `json:"stickyBound"`
	Schedulable         bool       `json:"schedulable"`
	UnschedulableReason string     `json:"unschedulableReason"`
	EndpointCapability  string     `json:"endpointCapability,omitempty"`
}

type Repository interface {
	ListAccounts(ctx context.Context, provider string) ([]Account, error)
	HasEnabledAccounts(ctx context.Context, provider string) (bool, error)
	FindAccountByID(ctx context.Context, provider string, id int64) (Account, error)
	SaveAccount(ctx context.Context, account Account) (Account, error)
	UpdateAccount(ctx context.Context, provider string, id int64, update AccountUpdate) (Account, error)
	DeleteAccount(ctx context.Context, provider string, id int64) error
	MarkAccountUsed(ctx context.Context, provider string, id int64, usedAt time.Time) error
	MarkAccountError(ctx context.Context, provider string, id int64, message string, at time.Time) error
	RecordAccountStatus(ctx context.Context, provider string, id int64, status, reason string, at time.Time, rateLimitedUntil, circuitOpenUntil *time.Time) error
	RecordAccountTestResult(ctx context.Context, provider string, id int64, status, message string, at time.Time) error
	ListAccountTestResults(ctx context.Context, provider string, accountID int64, limit int) ([]AccountTestResult, error)
	ListAccountModels(ctx context.Context, provider string, accountID int64) ([]AccountModel, error)
	ListAccountModelsForAccounts(ctx context.Context, provider string, accountIDs []int64) (map[int64][]AccountModel, error)
	RecordAccountModelTestResult(ctx context.Context, provider string, result AccountModelTestResult) error
	ReplaceAccountModels(ctx context.Context, provider string, accountID int64, models []AccountModelInput) ([]AccountModel, error)
	SyncAccountModels(ctx context.Context, provider string, accountID int64, models []AccountModelInput, seenAt time.Time) ([]AccountModel, AccountModelSyncSummary, error)
	ListExposedModelsForRoutingPools(ctx context.Context, provider string, poolIDs []int64) ([]ExposedModel, error)
	ListEligibleAccountsForModel(ctx context.Context, provider string, model string, excludedAccountIDs []int64, now time.Time) ([]Account, error)
	FindRoutingPool(ctx context.Context, poolID int64) (RoutingPool, error)
	RoutingPoolHasAccounts(ctx context.Context, poolID int64) (bool, error)
	ListAccountsForRoutingPool(ctx context.Context, provider string, poolID int64, model string, excludedAccountIDs []int64, now time.Time) ([]Account, error)
	ListRoutingPoolAccounts(ctx context.Context, provider string, poolID int64) ([]Account, error)
	FindSessionBinding(ctx context.Context, provider string, model string, sessionID string) (SessionBinding, error)
	UpsertSessionBinding(ctx context.Context, provider string, model string, sessionID string, accountID int64) error
	FindSessionBindingInRoutingPool(ctx context.Context, provider string, routingPoolID int64, model string, sessionID string) (SessionBinding, error)
	UpsertSessionBindingInRoutingPool(ctx context.Context, provider string, routingPoolID int64, model string, sessionID string, accountID int64) error
}

// EndpointAwareRepository is optional so older repository fakes and external
// integrations retain the legacy model-only contract. Implementations that
// provide it can exclude explicitly unsupported endpoint/model combinations;
// missing capability metadata must remain eligible.
type EndpointAwareRepository interface {
	ListEligibleAccountsForModelAndEndpoint(ctx context.Context, provider, model, endpoint string, excludedAccountIDs []int64, now time.Time) ([]Account, error)
	ListAccountsForRoutingPoolAndEndpoint(ctx context.Context, provider string, poolID int64, model, endpoint string, excludedAccountIDs []int64, now time.Time) ([]Account, error)
}

type AccountIdentities struct {
	ChatGPTAccountID  string
	ChatGPTUserID     string
	Email             string
	AccessTokenSHA256 string
}

type accountStatusProber interface {
	ProbeAccountStatus(ctx context.Context, cfg Config, accessToken string) (probeResult, error)
}

type accountModelProber interface {
	ProbeAccountModel(ctx context.Context, cfg Config, selected SelectedAccount, model string) modelProbeResult
}

type probeResult struct {
	statusCode int
	retryAfter string
	message    string
}

type modelProbeResult struct {
	statusCode int
	errorCode  string
	message    string
}

type HTTPClient struct {
	client *http.Client
}

type AccountTransportInvalidator interface {
	InvalidateAccountTransport(accountID int64)
	InvalidateAllAccountTransports()
}

type Service struct {
	repo                     Repository
	prober                   accountStatusProber
	modelProber              accountModelProber
	accountTestRequestLogger AccountTestRequestLogger
	requestLogWriteObserver  RequestLogWriteObserver
	metrics                  MetricsObserver
	cfg                      Config
	encryptionKeyring        *secret.Keyring
	httpClient               *HTTPClient
	transportInvalidatorMu   sync.RWMutex
	transportInvalidator     AccountTransportInvalidator
}

type RequestLogWriteObserver interface {
	Observe(correlationID string, err error)
}

func NewHTTPClient(client *http.Client) *HTTPClient {
	if client == nil {
		client = http.DefaultClient
	}
	return &HTTPClient{client: client}
}

func NewService(repo Repository, client any, cfg Config) *Service {
	if cfg.Provider == "" {
		cfg.Provider = "openai"
	}
	if cfg.APIBaseURL == "" {
		cfg.APIBaseURL = "https://api.openai.com"
	}
	prober, _ := client.(accountStatusProber)
	httpClient := NewHTTPClient(nil)
	return &Service{
		repo:                     repo,
		prober:                   prober,
		modelProber:              httpClient,
		accountTestRequestLogger: cfg.AccountTestLogger,
		requestLogWriteObserver:  cfg.RequestLogObserver,
		metrics:                  cfg.Metrics,
		cfg:                      cfg,
		encryptionKeyring:        cfg.EncryptionKeyring,
		httpClient:               httpClient,
	}
}

func (s *Service) SetAccountTransportInvalidator(invalidator AccountTransportInvalidator) {
	s.transportInvalidatorMu.Lock()
	s.transportInvalidator = invalidator
	s.transportInvalidatorMu.Unlock()
}

func (s *Service) invalidateAccountTransport(accountID int64) {
	s.transportInvalidatorMu.RLock()
	invalidator := s.transportInvalidator
	s.transportInvalidatorMu.RUnlock()
	if invalidator != nil {
		invalidator.InvalidateAccountTransport(accountID)
	}
}

func (c *HTTPClient) ProbeAccountStatus(ctx context.Context, cfg Config, accessToken string) (probeResult, error) {
	apiBaseURL := strings.TrimRight(strings.TrimSpace(cfg.APIBaseURL), "/")
	if apiBaseURL == "" {
		apiBaseURL = "https://api.openai.com"
	}
	return c.probeURL(ctx, upstreamModelsURL(apiBaseURL), accessToken, cfg.ProxyURL)
}

func (c *HTTPClient) probeURL(ctx context.Context, targetURL, accessToken, proxyURL string) (probeResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return probeResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.clientForProxy(proxyURL).Do(req)
	if err != nil {
		return probeResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		return probeResult{statusCode: resp.StatusCode}, nil
	}
	return probeResult{
		statusCode: resp.StatusCode,
		retryAfter: resp.Header.Get("Retry-After"),
		message:    readErrorMessage(resp.Body, resp.StatusCode),
	}, nil
}

func (c *HTTPClient) ProbeAccountModel(ctx context.Context, cfg Config, selected SelectedAccount, model string) modelProbeResult {
	model = strings.TrimSpace(model)
	var targetURL string
	var payload any
	targetURL = upstreamChatCompletionsURL(selected.BaseURL)
	payload = map[string]any{"model": model, "messages": []map[string]string{{"role": "user", "content": "Reply with OK only."}}, "stream": false}

	body, err := json.Marshal(payload)
	if err != nil {
		return modelProbeResult{errorCode: "invalid_response", message: "could not encode model probe request"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return modelProbeResult{errorCode: "network_error", message: "could not create model probe request"}
	}
	req.Header.Set("Authorization", "Bearer "+selected.AuthorizationToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	probeClient := c.clientForProxy(selected.ProxyURL)

	resp, err := probeClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return modelProbeResult{errorCode: "timeout", message: "model test timed out"}
		}
		return modelProbeResult{errorCode: "network_error", message: "could not reach upstream"}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		message := readModelProbeError(resp.Body, resp.StatusCode)
		return modelProbeResult{
			statusCode: resp.StatusCode,
			errorCode:  classifyModelProbeHTTPError(resp.StatusCode, message),
			message:    message,
		}
	}

	raw, tooLarge, err := readBoundedBody(resp.Body, maxModelProbeBody)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return modelProbeResult{statusCode: resp.StatusCode, errorCode: "timeout", message: "model test timed out"}
		}
		return modelProbeResult{statusCode: resp.StatusCode, errorCode: "invalid_response", message: "could not read upstream response"}
	}
	var responsePayload struct {
		Choices []json.RawMessage `json:"choices"`
	}
	if tooLarge || json.Unmarshal(raw, &responsePayload) != nil || responsePayload.Choices == nil {
		return modelProbeResult{statusCode: resp.StatusCode, errorCode: "invalid_response", message: "upstream returned invalid JSON"}
	}
	return modelProbeResult{statusCode: resp.StatusCode}
}

func upstreamChatCompletionsURL(baseURL string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(trimmed, "/v1") {
		return trimmed + "/chat/completions"
	}
	return trimmed + "/v1/chat/completions"
}

func readBoundedBody(body io.Reader, limit int64) ([]byte, bool, error) {
	raw, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(raw)) > limit {
		return raw[:limit], true, nil
	}
	return raw, false, nil
}

func readModelProbeError(body io.Reader, statusCode int) string {
	raw, _, err := readBoundedBody(body, maxModelProbeErrorBody)
	if err != nil {
		return http.StatusText(statusCode)
	}
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return http.StatusText(statusCode)
	}
	message := payload.Error.Message
	if strings.TrimSpace(message) == "" {
		message = payload.Message
	}
	message = sanitizeDiagnosticMessage(message)
	if message == "" {
		return http.StatusText(statusCode)
	}
	return message
}

func classifyModelProbeHTTPError(statusCode int, message string) string {
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "unauthorized"
	case http.StatusTooManyRequests:
		return "rate_limited"
	case http.StatusNotFound:
		return "model_not_found"
	}
	lower := strings.ToLower(message)
	if strings.Contains(lower, "model") && (strings.Contains(lower, "not found") || strings.Contains(lower, "does not exist") || strings.Contains(lower, "unknown") || strings.Contains(lower, "unsupported")) {
		return "model_not_found"
	}
	return "upstream_error"
}

func sanitizeDiagnosticMessage(message string) string {
	message = strings.Join(strings.Fields(strings.TrimSpace(message)), " ")
	const maxLen = 512
	if len(message) > maxLen {
		message = message[:maxLen]
	}
	return message
}

func redactModelProbeSecrets(message string, selected SelectedAccount) string {
	secrets := []string{strings.TrimSpace(selected.AuthorizationToken)}
	if parsed, err := url.Parse(strings.TrimSpace(selected.ProxyURL)); err == nil && parsed.User != nil {
		if password, ok := parsed.User.Password(); ok {
			secrets = append(secrets, password)
		}
	}
	for _, value := range secrets {
		if len(value) >= 4 {
			message = strings.ReplaceAll(message, value, "[redacted]")
		}
	}
	return sanitizeDiagnosticMessage(message)
}

func readErrorMessage(body io.Reader, statusCode int) string {
	if body == nil {
		return http.StatusText(statusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(body, 64<<10))
	if err != nil || len(strings.TrimSpace(string(raw))) == 0 {
		return http.StatusText(statusCode)
	}
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &payload); err == nil && strings.TrimSpace(payload.Error.Message) != "" {
		return strings.TrimSpace(payload.Error.Message)
	}
	return strings.TrimSpace(string(raw))
}

func (c *HTTPClient) clientForProxy(proxyURL string) *http.Client {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" {
		return c.client
	}
	parsed, err := url.Parse(proxyURL)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" {
		return c.client
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(parsed)
	client := &http.Client{Transport: transport}
	if c.client != nil {
		client.Timeout = c.client.Timeout
		client.CheckRedirect = c.client.CheckRedirect
		client.Jar = c.client.Jar
	}
	return client
}

func (s *Service) Configured() bool {
	return s.encryptionKeyring != nil || strings.TrimSpace(s.cfg.Secret) != ""
}

func (s *Service) encryptString(kind secret.SecretKind, value string) (string, error) {
	if s.encryptionKeyring != nil {
		return s.encryptionKeyring.EncryptStringFor(kind, value)
	}
	keyring, err := secret.NewKeyring(secret.EncryptionKey{ID: secret.DefaultEncryptionKeyID, Secret: s.cfg.Secret}, nil)
	if err != nil {
		return "", err
	}
	return keyring.EncryptStringFor(kind, value)
}

func (s *Service) decryptString(kind secret.SecretKind, value string) (string, error) {
	if s.encryptionKeyring != nil {
		return s.encryptionKeyring.DecryptStringFor(kind, value)
	}
	keyring, err := secret.NewKeyring(secret.EncryptionKey{ID: secret.DefaultEncryptionKeyID, Secret: s.cfg.Secret}, nil)
	if err != nil {
		return "", err
	}
	return keyring.DecryptStringFor(kind, value)
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	status := Status{
		Provider:   s.cfg.Provider,
		Configured: s.Configured(),
	}

	accounts, err := s.ListAccounts(ctx)
	if err != nil {
		return Status{}, err
	}
	if len(accounts) > 0 {
		status.Connected = true
		status.DisplayName = accountDisplayName(accounts[0])
	}

	return status, nil
}

func (s *Service) ListAccounts(ctx context.Context) ([]Account, error) {
	accounts, err := s.repo.ListAccounts(ctx, s.cfg.Provider)
	if err != nil {
		return nil, err
	}
	filtered := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		if account.AccountType == AccountTypeAPIUpstream {
			filtered = append(filtered, s.withProxySummary(account))
		}
	}
	return filtered, nil
}

func (s *Service) findAPIAccount(ctx context.Context, id int64) (Account, error) {
	if id <= 0 {
		return Account{}, ErrInvalidInput
	}
	account, err := s.repo.FindAccountByID(ctx, s.cfg.Provider, id)
	if err != nil {
		return Account{}, err
	}
	if account.AccountType != AccountTypeAPIUpstream {
		return Account{}, ErrNotConnected
	}
	return account, nil
}

func (s *Service) UpdateAccount(ctx context.Context, id int64, update AccountUpdate) (Account, error) {
	if id <= 0 {
		return Account{}, ErrInvalidInput
	}
	if update.Enabled == nil && update.Priority == nil && update.LoadFactor == nil && update.MaxConcurrentRequests == nil && !update.ClearStatus && update.Name == nil && update.APIUpstreamBaseURL == nil && update.APIUpstreamAPIKey == nil && update.ProxyURL == nil {
		return Account{}, ErrInvalidInput
	}
	if update.Priority != nil && *update.Priority < 0 {
		return Account{}, ErrInvalidInput
	}
	if update.LoadFactor != nil && (*update.LoadFactor < 1 || *update.LoadFactor > 100) {
		return Account{}, ErrInvalidInput
	}
	if update.MaxConcurrentRequests != nil && *update.MaxConcurrentRequests < 0 {
		return Account{}, ErrInvalidInput
	}
	if update.Name != nil {
		name := strings.TrimSpace(*update.Name)
		if name == "" || len(name) > 128 {
			return Account{}, ErrInvalidInput
		}
		update.Name = &name
	}
	baseURLChanged := update.APIUpstreamBaseURL != nil
	apiKeyChanged := update.APIUpstreamAPIKey != nil
	proxyChanged := update.ProxyURL != nil
	if _, err := s.findAPIAccount(ctx, id); err != nil {
		return Account{}, err
	}
	if update.APIUpstreamBaseURL != nil {
		baseURL := normalizeOpenAICompatibleBaseURL(*update.APIUpstreamBaseURL)
		parsedBaseURL, err := url.Parse(baseURL)
		if err != nil ||
			!parsedBaseURL.IsAbs() ||
			parsedBaseURL.Host == "" ||
			!s.apiUpstreamSchemeAllowed(parsedBaseURL.Scheme) {
			return Account{}, ErrInvalidInput
		}
		update.APIUpstreamBaseURL = &baseURL
	}
	if update.APIUpstreamAPIKey != nil {
		apiKey := strings.TrimSpace(*update.APIUpstreamAPIKey)
		if apiKey == "" {
			return Account{}, ErrInvalidInput
		}
		encryptedAPIKey, err := s.encryptString(secret.SecretKindProviderAPIKey, apiKey)
		if err != nil {
			return Account{}, err
		}
		update.EncryptedAPIUpstreamAPIKey = &encryptedAPIKey
		update.APIUpstreamAPIKey = nil
	}
	if update.ProxyURL != nil {
		proxyURL, err := normalizeProxyURL(*update.ProxyURL)
		if err != nil {
			return Account{}, err
		}
		encryptedProxyURL := ""
		if proxyURL != "" {
			encryptedProxyURL, err = s.encryptString(secret.SecretKindProviderProxyURL, proxyURL)
			if err != nil {
				return Account{}, err
			}
		}
		update.ProxyURL = &proxyURL
		update.EncryptedProxyURL = &encryptedProxyURL
	}
	intent := providerAuditIntent(systemevent.ActionProviderAccountUpdated, id, "")
	changed := make([]string, 0, 9)
	if update.Enabled != nil {
		changed = append(changed, "enabled")
	}
	if update.Priority != nil {
		changed = append(changed, "priority")
	}
	if update.LoadFactor != nil {
		changed = append(changed, "load_factor")
	}
	if update.MaxConcurrentRequests != nil {
		changed = append(changed, "max_concurrent_requests")
	}
	if update.Name != nil {
		changed = append(changed, "name")
	}
	if baseURLChanged {
		changed = append(changed, "base_url")
	}
	if apiKeyChanged {
		changed = append(changed, "api_key")
	}
	if proxyChanged {
		changed = append(changed, "proxy_url")
	}
	intent.Metadata = map[string]any{
		"changed_fields":  changed,
		"api_key_changed": apiKeyChanged,
		"proxy_changed":   proxyChanged,
	}
	if parent, ok := systemevent.IntentFromContext(ctx); ok && parent.Action == systemevent.ActionProviderAccountStatusReset {
		intent = parent
	}
	account, err := s.repo.UpdateAccount(withProviderEventIntent(ctx, intent), s.cfg.Provider, id, update)
	if err != nil {
		return Account{}, err
	}
	s.invalidateAccountTransport(id)
	return s.withProxySummary(account), nil
}

func (s *Service) ResetAccountStatus(ctx context.Context, id int64) (Account, error) {
	ctx = withProviderEventIntent(ctx, providerAuditIntent(systemevent.ActionProviderAccountStatusReset, id, ""))
	return s.UpdateAccount(ctx, id, AccountUpdate{ClearStatus: true})
}

func (s *Service) CreateAPIUpstreamAccount(ctx context.Context, input APIUpstreamInput) (Account, error) {
	name := strings.TrimSpace(input.Name)
	baseURL := normalizeOpenAICompatibleBaseURL(input.BaseURL)
	apiKey := strings.TrimSpace(input.APIKey)
	if name == "" || baseURL == "" || apiKey == "" {
		return Account{}, ErrInvalidInput
	}
	parsedBaseURL, err := url.Parse(baseURL)
	if err != nil ||
		!parsedBaseURL.IsAbs() ||
		parsedBaseURL.Host == "" ||
		!s.apiUpstreamSchemeAllowed(parsedBaseURL.Scheme) {
		return Account{}, ErrInvalidInput
	}

	encryptedAPIKey, err := s.encryptString(secret.SecretKindProviderAPIKey, apiKey)
	if err != nil {
		return Account{}, err
	}
	proxyURL, err := normalizeProxyURL(input.ProxyURL)
	if err != nil {
		return Account{}, err
	}
	encryptedProxyURL := ""
	if proxyURL != "" {
		encryptedProxyURL, err = s.encryptString(secret.SecretKindProviderProxyURL, proxyURL)
		if err != nil {
			return Account{}, err
		}
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	createCtx := withProviderEventIntent(ctx, providerAuditIntent(systemevent.ActionProviderAccountCreated, 0, name))
	account, err := s.repo.SaveAccount(createCtx, Account{
		Provider:    s.cfg.Provider,
		AccountType: AccountTypeAPIUpstream,
		Name:        name,
		DisplayName: name,
		Enabled:     enabled,
		Priority:    input.Priority,
		LoadFactor:  normalizedLoadFactor(input.LoadFactor),
		Status:      AccountStatusActive,

		Credential: AccountCredential{
			CredentialType:    CredentialTypeAPIKey,
			EncryptedAPIKey:   encryptedAPIKey,
			EncryptedProxyURL: encryptedProxyURL,
			BaseURL:           baseURL,
		},
	})
	if err != nil {
		return Account{}, err
	}

	if len(input.Models) > 0 {
		models := make([]AccountModelInput, 0, len(input.Models))
		for _, model := range input.Models {
			models = append(models, AccountModelInput{
				Model:   model,
				Enabled: true,
			})
		}
		if _, err := s.ReplaceAccountModels(ctx, account.ID, models); err != nil {
			if deleteErr := s.DisconnectAccount(ctx, account.ID); deleteErr != nil {
				return Account{}, fmt.Errorf("replace account models: %w; cleanup account: %v", err, deleteErr)
			}
			return Account{}, err
		}
	}
	return s.withProxySummary(account), nil
}

func normalizeOpenAICompatibleBaseURL(value string) string {
	baseURL := strings.TrimRight(strings.TrimSpace(value), "/")
	if strings.HasSuffix(baseURL, "/v1") {
		return strings.TrimSuffix(baseURL, "/v1")
	}
	return baseURL
}

func (s *Service) DisconnectAccount(ctx context.Context, id int64) error {
	if _, err := s.findAPIAccount(ctx, id); err != nil {
		return err
	}
	ctx = withProviderEventIntent(ctx, providerAuditIntent(systemevent.ActionProviderAccountDisconnected, id, ""))
	if err := s.repo.DeleteAccount(ctx, s.cfg.Provider, id); err != nil {
		return err
	}
	s.invalidateAccountTransport(id)
	return nil
}

func normalizeAccountModelInputs(inputs []AccountModelInput) ([]AccountModelInput, error) {
	models := make([]AccountModelInput, 0, len(inputs))
	seen := map[string]bool{}
	for _, input := range inputs {
		model := strings.TrimSpace(input.Model)
		if model == "" {
			continue
		}
		if len(model) > maxModelNameLen {
			return nil, ErrInvalidInput
		}
		if seen[model] {
			continue
		}
		seen[model] = true
		metadata, err := normalizeAccountModelMetadata(input.Metadata)
		if err != nil {
			return nil, err
		}
		models = append(models, AccountModelInput{
			Model:    model,
			Enabled:  input.Enabled,
			Metadata: metadata,
		})
		if len(models) > maxAccountModels {
			return nil, ErrInvalidInput
		}
	}
	return models, nil
}

func (s *Service) ListAccountModels(ctx context.Context, accountID int64) ([]AccountModel, error) {
	if _, err := s.findAPIAccount(ctx, accountID); err != nil {
		return nil, err
	}
	return s.repo.ListAccountModels(ctx, s.cfg.Provider, accountID)
}

func (s *Service) ListAccountModelsForAccounts(ctx context.Context, accountIDs []int64) (map[int64][]AccountModel, error) {
	unique := make([]int64, 0, len(accountIDs))
	seen := make(map[int64]struct{}, len(accountIDs))
	for _, accountID := range accountIDs {
		if accountID <= 0 {
			return nil, ErrInvalidInput
		}
		if _, ok := seen[accountID]; ok {
			continue
		}
		seen[accountID] = struct{}{}
		unique = append(unique, accountID)
	}
	if len(unique) > 0 {
		accounts, err := s.repo.ListAccounts(ctx, s.cfg.Provider)
		if err != nil {
			return nil, err
		}
		allowed := make(map[int64]bool, len(accounts))
		for _, account := range accounts {
			if account.AccountType == AccountTypeAPIUpstream {
				allowed[account.ID] = true
			}
		}
		for _, id := range unique {
			if !allowed[id] {
				return nil, ErrNotConnected
			}
		}
	}
	return s.repo.ListAccountModelsForAccounts(ctx, s.cfg.Provider, unique)
}

func (s *Service) TestAccountModel(ctx context.Context, accountID int64, model string) (AccountModelTestResult, error) {
	model = strings.TrimSpace(model)
	if accountID <= 0 || model == "" || len(model) > maxModelNameLen {
		return AccountModelTestResult{}, ErrInvalidInput
	}
	account, err := s.findAPIAccount(ctx, accountID)
	if err != nil {
		return AccountModelTestResult{}, err
	}
	models, err := s.repo.ListAccountModels(ctx, s.cfg.Provider, accountID)
	if err != nil {
		return AccountModelTestResult{}, err
	}
	configured := false
	for _, accountModel := range models {
		if accountModel.Model == model {
			configured = true
			break
		}
	}
	if !configured {
		return AccountModelTestResult{}, ErrNotConnected
	}

	selected, err := s.selectedAccount(ctx, account)
	if err != nil {
		return AccountModelTestResult{}, err
	}
	if s.modelProber == nil {
		return AccountModelTestResult{}, errors.New("account model prober is unavailable")
	}

	startedAt := time.Now()
	probeCtx, cancel := context.WithTimeout(ctx, accountModelTestTimeout)
	defer cancel()
	probe := s.modelProber.ProbeAccountModel(probeCtx, s.cfg, selected, model)
	s.logAccountTestRequest(ctx, selected, model, accountModelTestRoute(s.cfg, selected), http.MethodPost, probe.statusCode, probe.errorCode, startedAt)
	checkedAt := time.Now().UTC()
	result := AccountModelTestResult{
		AccountID:  accountID,
		Model:      model,
		Status:     AccountTestStatusPassed,
		HTTPStatus: probe.statusCode,
		LatencyMS:  time.Since(startedAt).Milliseconds(),
		CheckedAt:  checkedAt,
	}
	if probe.errorCode != "" {
		result.Status = AccountTestStatusFailed
		result.ErrorCode = probe.errorCode
		result.Message = redactModelProbeSecrets(probe.message, selected)
	}
	testSeverity := systemevent.SeverityInfo
	testOutcome := systemevent.OutcomeSuccess
	testErrorCode := ""
	if result.Status != AccountTestStatusPassed {
		testSeverity = systemevent.SeverityWarning
		testOutcome = systemevent.OutcomeFailure
		testErrorCode = "provider_account_model_test_failed"
	}
	testCtx := withProviderEventIntent(ctx, systemevent.EventIntent{
		Category:  systemevent.CategoryAudit,
		Severity:  testSeverity,
		Action:    systemevent.ActionProviderAccountModelTested,
		Outcome:   testOutcome,
		Target:    providerAccountTarget(accountID, accountDisplayName(account)),
		ErrorCode: testErrorCode,
		Metadata:  map[string]any{"model": model, "test_status": result.Status, "http_status": result.HTTPStatus},
	})
	if err := s.repo.RecordAccountModelTestResult(testCtx, s.cfg.Provider, result); err != nil {
		return AccountModelTestResult{}, err
	}
	return result, nil
}

func (s *Service) ReplaceAccountModels(ctx context.Context, accountID int64, models []AccountModelInput) ([]AccountModel, error) {
	if _, err := s.findAPIAccount(ctx, accountID); err != nil {
		return nil, err
	}
	normalized, err := normalizeAccountModelInputs(models)
	if err != nil {
		return nil, err
	}
	ctx = withProviderEventIntent(ctx, systemevent.EventIntent{
		Category: systemevent.CategoryAudit,
		Severity: systemevent.SeverityInfo,
		Action:   systemevent.ActionProviderAccountModelsReplaced,
		Outcome:  systemevent.OutcomeSuccess,
		Target:   providerAccountTarget(accountID, ""),
		Metadata: map[string]any{"model_count": len(normalized)},
	})
	return s.repo.ReplaceAccountModels(ctx, s.cfg.Provider, accountID, normalized)
}

// SyncUpstreamAccountModels fetches the OpenAI-compatible /v1/models list from an
// API-upstream account and persists the result via repo.SyncAccountModels.
func (s *Service) SyncUpstreamAccountModels(ctx context.Context, accountID int64) ([]AccountModel, AccountModelSyncSummary, error) {
	if accountID <= 0 {
		return nil, AccountModelSyncSummary{}, ErrInvalidInput
	}
	account, err := s.findAPIAccount(ctx, accountID)
	if err != nil {
		return nil, AccountModelSyncSummary{}, err
	}
	selected, err := s.selectedAccount(ctx, account)
	if err != nil {
		return nil, AccountModelSyncSummary{}, err
	}
	targetURL := upstreamModelsURL(selected.BaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, AccountModelSyncSummary{}, err
	}
	req.Header.Set("Authorization", "Bearer "+selected.AuthorizationToken)
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.clientForProxy(selected.ProxyURL).Do(req)
	if err != nil {
		return nil, AccountModelSyncSummary{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, AccountModelSyncSummary{}, fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, AccountModelSyncSummary{}, err
	}

	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, AccountModelSyncSummary{}, err
	}
	if parsed.Data == nil {
		return nil, AccountModelSyncSummary{}, errors.New("upstream response missing data array")
	}

	models := make([]AccountModelInput, 0, len(parsed.Data))
	for _, m := range parsed.Data {
		mid := strings.TrimSpace(m.ID)
		if mid == "" {
			continue
		}
		models = append(models, AccountModelInput{Model: mid, Enabled: true})
	}
	if len(models) == 0 {
		return nil, AccountModelSyncSummary{}, errors.New("no models found in upstream response")
	}

	normalized, err := normalizeAccountModelInputs(models)
	if err != nil {
		return nil, AccountModelSyncSummary{}, err
	}

	syncCtx := withProviderEventIntent(ctx, systemevent.EventIntent{
		Category: systemevent.CategoryAudit,
		Severity: systemevent.SeverityInfo,
		Action:   systemevent.ActionProviderAccountModelsSynced,
		Outcome:  systemevent.OutcomeSuccess,
		Target:   providerAccountTarget(accountID, accountDisplayName(account)),
		Metadata: map[string]any{"upstream_model_count": len(normalized)},
	})
	return s.repo.SyncAccountModels(syncCtx, s.cfg.Provider, accountID, normalized, time.Now().UTC())
}

func (s *Service) ListExposedModelsForRoutingPoolChain(ctx context.Context, primaryPoolID int64) ([]ExposedModel, error) {
	if primaryPoolID <= 0 {
		return []ExposedModel{}, nil
	}
	pools, _, err := s.routingPoolChain(ctx, primaryPoolID)
	if err != nil {
		return nil, err
	}
	poolIDs := make([]int64, 0, len(pools))
	for depth, pool := range pools {
		if !pool.Enabled {
			if depth == 0 {
				return []ExposedModel{}, nil
			}
			continue
		}
		poolIDs = append(poolIDs, pool.ID)
	}
	return s.repo.ListExposedModelsForRoutingPools(ctx, s.cfg.Provider, poolIDs)
}

func (s *Service) RecordAccountFailure(ctx context.Context, accountID int64, statusCode int, retryAfter, message string) error {
	if accountID <= 0 {
		return ErrInvalidInput
	}
	now := time.Now()
	reason := strings.TrimSpace(message)
	if reason == "" {
		reason = http.StatusText(statusCode)
	}

	switch {
	case statusCode == http.StatusTooManyRequests:
		until := retryAfterTime(retryAfter, now, time.Minute)
		ctx = withProviderEventIntent(ctx, runtimeAccountIntent(systemevent.ActionProviderAccountRateLimited, accountID, AccountStatusRateLimited))
		return s.repo.RecordAccountStatus(ctx, s.cfg.Provider, accountID, AccountStatusRateLimited, reason, now, &until, nil)
	case statusCode == http.StatusForbidden && isEndpointPermissionError(reason):
		return s.repo.MarkAccountError(ctx, s.cfg.Provider, accountID, reason, now)
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		ctx = withProviderEventIntent(ctx, runtimeAccountIntent(systemevent.ActionProviderAccountExpired, accountID, AccountStatusExpired))
		return s.repo.RecordAccountStatus(ctx, s.cfg.Provider, accountID, AccountStatusExpired, reason, now, nil, nil)
	case statusCode >= http.StatusInternalServerError:
		until := now.Add(defaultCircuitOpen)
		ctx = withProviderEventIntent(ctx, runtimeAccountIntent(systemevent.ActionProviderAccountCircuitOpened, accountID, AccountStatusCircuitOpen))
		return s.repo.RecordAccountStatus(ctx, s.cfg.Provider, accountID, AccountStatusCircuitOpen, reason, now, nil, &until)
	default:
		return s.repo.MarkAccountError(ctx, s.cfg.Provider, accountID, reason, now)
	}
}

func isEndpointPermissionError(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return false
	}
	return strings.Contains(lower, "missing scopes") ||
		strings.Contains(lower, "api.responses.write") ||
		(strings.Contains(lower, "insufficient permissions") && strings.Contains(lower, "scope"))
}

func (s *Service) TestAccount(ctx context.Context, id int64) (Account, error) {
	if id <= 0 {
		return Account{}, ErrInvalidInput
	}

	account, err := s.findAPIAccount(ctx, id)
	if err != nil {
		return Account{}, err
	}

	selected, err := s.selectedAccount(ctx, account)
	if err != nil {
		now := time.Now()
		if markErr := s.recordAccountTestResult(ctx, account, AccountTestStatusFailed, err.Error(), now); markErr != nil {
			return Account{}, markErr
		}
		if markErr := s.recordSelectionFailure(ctx, account.ID, err); markErr != nil {
			return Account{}, markErr
		}
		return s.repo.FindAccountByID(ctx, s.cfg.Provider, account.ID)
	}
	if s.prober == nil || strings.TrimSpace(selected.AuthorizationToken) == "" {
		return account, nil
	}

	cfg := s.cfg
	cfg.APIBaseURL = selected.BaseURL
	cfg.ProxyURL = selected.ProxyURL

	startedAt := time.Now()
	result, err := s.prober.ProbeAccountStatus(ctx, cfg, selected.AuthorizationToken)
	s.logAccountTestRequest(ctx, selected, accountStatusTestModel(cfg), accountStatusTestRoute(cfg), accountStatusTestMethod(cfg), result.statusCode, accountStatusTestError(result, err), startedAt)
	if err != nil {
		now := time.Now()
		until := now.Add(defaultCircuitOpen)
		if markErr := s.recordAccountTestResult(ctx, account, AccountTestStatusFailed, err.Error(), now); markErr != nil {
			return Account{}, markErr
		}
		statusCtx := withProviderEventIntent(ctx, runtimeAccountIntent(systemevent.ActionProviderAccountCircuitOpened, account.ID, AccountStatusCircuitOpen))
		if markErr := s.repo.RecordAccountStatus(statusCtx, s.cfg.Provider, account.ID, AccountStatusCircuitOpen, err.Error(), now, nil, &until); markErr != nil {
			return Account{}, markErr
		}
		return s.repo.FindAccountByID(ctx, s.cfg.Provider, account.ID)
	}
	if !isSuccessfulAccountStatus(result.statusCode) {
		now := time.Now()
		message := strings.TrimSpace(result.message)
		if message == "" {
			message = http.StatusText(result.statusCode)
		}
		if markErr := s.recordAccountTestResult(ctx, account, AccountTestStatusFailed, message, now); markErr != nil {
			return Account{}, markErr
		}
		if err := s.RecordAccountFailure(ctx, account.ID, result.statusCode, result.retryAfter, result.message); err != nil {
			return Account{}, err
		}
		return s.repo.FindAccountByID(ctx, s.cfg.Provider, account.ID)
	}
	if err := s.recordAccountTestResult(ctx, account, AccountTestStatusPassed, "", time.Now()); err != nil {
		return Account{}, err
	}
	if err := s.RecordAccountRecovered(ctx, account.ID); err != nil {
		return Account{}, err
	}
	recovered, err := s.repo.FindAccountByID(ctx, s.cfg.Provider, account.ID)
	if err != nil {
		return Account{}, err
	}
	return s.withProxySummary(recovered), nil
}

func (s *Service) recordAccountTestResult(ctx context.Context, account Account, status, message string, at time.Time) error {
	severity := systemevent.SeverityInfo
	outcome := systemevent.OutcomeSuccess
	errorCode := ""
	if status != AccountTestStatusPassed {
		severity = systemevent.SeverityWarning
		outcome = systemevent.OutcomeFailure
		errorCode = "provider_account_test_failed"
	}
	testCtx := withProviderEventIntent(ctx, systemevent.EventIntent{
		Category:  systemevent.CategoryAudit,
		Severity:  severity,
		Action:    systemevent.ActionProviderAccountTested,
		Outcome:   outcome,
		Target:    providerAccountTarget(account.ID, accountDisplayName(account)),
		ErrorCode: errorCode,
		Metadata:  map[string]any{"test_status": status},
	})
	return s.repo.RecordAccountTestResult(testCtx, s.cfg.Provider, account.ID, status, message, at)
}

func (s *Service) logAccountTestRequest(ctx context.Context, selected SelectedAccount, model, route, method string, statusCode int, errorCode string, startedAt time.Time) {
	if s.accountTestRequestLogger == nil {
		return
	}
	if statusCode == 0 {
		statusCode = http.StatusBadGateway
	}
	providerName := strings.TrimSpace(selected.Provider)
	if providerName == "" {
		providerName = s.cfg.Provider
	}
	requestID := systemevent.NewCorrelationID()
	if request, ok := systemevent.FromContext(ctx); ok && systemevent.ValidCorrelationID(request.CorrelationID) {
		requestID = request.CorrelationID
	}
	entry := AccountTestRequestLog{
		RequestID:           requestID,
		Provider:            providerName,
		ProviderAccountID:   selected.AccountID,
		ProviderAccountType: selected.AccountType,
		ProviderAccountName: selected.DisplayName,
		Model:               model,
		Route:               route,
		Method:              method,
		StatusCode:          statusCode,
		Latency:             time.Since(startedAt),
		Error:               errorCode,
		CreatedAt:           startedAt,
	}
	logCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), accountTestRequestLogTimeout)
	defer cancel()
	err := s.accountTestRequestLogger.CreateAccountTestRequestLog(logCtx, entry)
	if s.requestLogWriteObserver != nil {
		s.requestLogWriteObserver.Observe(requestID, err)
	}
}

func accountStatusTestError(result probeResult, probeErr error) string {
	if probeErr != nil {
		if errors.Is(probeErr, context.DeadlineExceeded) {
			return "timeout"
		}
		return "network_error"
	}
	if result.statusCode >= http.StatusOK && result.statusCode <= 299 {
		return ""
	}
	switch result.statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "unauthorized"
	case http.StatusTooManyRequests:
		return "rate_limited"
	default:
		return "upstream_error"
	}
}

func requestRoutePath(rawURL, fallback string) string {
	target, err := url.Parse(rawURL)
	if err != nil || target.Path == "" {
		return fallback
	}
	return target.Path
}

func (s *Service) TestAccounts(ctx context.Context) ([]Account, error) {
	accounts, err := s.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	tested := make([]Account, 0, len(accounts))
	for index, account := range accounts {
		updated, err := s.TestAccount(ctx, account.ID)
		if err != nil {
			return tested, &accountBatchError{
				Err: err, Requested: len(accounts), Attempted: index + 1,
				Succeeded: len(tested), Failed: 1, Skipped: len(accounts) - index - 1,
			}
		}
		tested = append(tested, updated)
	}
	return tested, nil
}

type accountBatchError struct {
	Err                                     error
	Requested, Attempted, Succeeded, Failed int
	Skipped                                 int
}

func (e *accountBatchError) Error() string { return e.Err.Error() }
func (e *accountBatchError) Unwrap() error { return e.Err }

func (s *Service) ListAccountTestResults(ctx context.Context, id int64, limit int) ([]AccountTestResult, error) {
	if id <= 0 {
		return nil, ErrInvalidInput
	}
	if _, err := s.findAPIAccount(ctx, id); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = defaultAccountTestResultsLimit
	}
	if limit > maxAccountTestResultsLimit {
		limit = maxAccountTestResultsLimit
	}
	return s.repo.ListAccountTestResults(ctx, s.cfg.Provider, id, limit)
}

func (s *Service) PauseAccountScheduling(ctx context.Context, id int64, duration time.Duration) (Account, error) {
	if id <= 0 {
		return Account{}, ErrInvalidInput
	}
	if duration <= 0 {
		duration = defaultManualPause
	}
	if duration > maxManualPause {
		return Account{}, ErrInvalidInput
	}
	if _, err := s.findAPIAccount(ctx, id); err != nil {
		return Account{}, err
	}
	now := time.Now()
	until := now.Add(duration)
	pauseCtx := withProviderEventIntent(ctx, providerAuditIntent(systemevent.ActionProviderAccountPaused, id, ""))
	if err := s.repo.RecordAccountStatus(pauseCtx, s.cfg.Provider, id, AccountStatusCircuitOpen, "manually paused", now, nil, &until); err != nil {
		return Account{}, err
	}
	return s.findAPIAccount(ctx, id)
}

func isSuccessfulAccountStatus(statusCode int) bool {
	return statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices
}

func (s *Service) selectedAccount(ctx context.Context, account Account) (SelectedAccount, error) {
	account = normalizeAccountCredentialFields(account)
	accountType := strings.TrimSpace(account.AccountType)
	if accountType != AccountTypeAPIUpstream {
		return SelectedAccount{}, ErrNotConnected
	}
	selected := SelectedAccount{
		AccountID:             account.ID,
		Provider:              valueOrDefault(strings.TrimSpace(account.Provider), s.cfg.Provider),
		AccountType:           accountType,
		DisplayName:           accountDisplayName(account),
		MaxConcurrentRequests: account.MaxConcurrentRequests,
	}
	if strings.TrimSpace(account.Credential.EncryptedProxyURL) != "" {
		proxyURL, err := s.decryptString(secret.SecretKindProviderProxyURL, account.Credential.EncryptedProxyURL)
		if err != nil {
			return SelectedAccount{}, err
		}
		selected.ProxyURL = strings.TrimSpace(proxyURL)
	}
	switch accountType {
	case AccountTypeAPIUpstream:
		token, err := s.decryptString(secret.SecretKindProviderAPIKey, account.Credential.EncryptedAPIKey)
		if err != nil {
			return SelectedAccount{}, err
		}
		selected.AuthorizationToken = token
		baseURL := strings.TrimRight(strings.TrimSpace(account.Credential.BaseURL), "/")
		parsedBaseURL, err := url.Parse(baseURL)
		if err != nil ||
			!parsedBaseURL.IsAbs() ||
			parsedBaseURL.Host == "" ||
			!s.apiUpstreamSchemeAllowed(parsedBaseURL.Scheme) {
			return SelectedAccount{}, ErrInvalidInput
		}
		selected.BaseURL = baseURL
	default:
		return SelectedAccount{}, fmt.Errorf("unsupported account type %q", accountType)
	}

	return selected, nil
}

func (s *Service) withProxySummary(account Account) Account {
	account = normalizeAccountCredentialFields(account)
	encryptedProxyURL := strings.TrimSpace(account.Credential.EncryptedProxyURL)
	account.ProxyURLConfigured = encryptedProxyURL != ""
	account.ProxyURLSummary = ""
	if encryptedProxyURL == "" {
		return account
	}
	proxyURL, err := s.decryptString(secret.SecretKindProviderProxyURL, encryptedProxyURL)
	if err != nil {
		account.ProxyURLSummary = "configured"
		return account
	}
	account.ProxyURLSummary = proxyURLSummary(proxyURL)
	return account
}

func normalizeProxyURL(value string) (string, error) {
	proxyURL := strings.TrimSpace(value)
	if proxyURL == "" {
		return "", nil
	}
	parsed, err := url.Parse(proxyURL)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" {
		return "", ErrInvalidInput
	}
	switch strings.ToLower(strings.TrimSpace(parsed.Scheme)) {
	case "http", "https":
		return parsed.String(), nil
	default:
		return "", ErrInvalidInput
	}
}

func proxyURLSummary(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" {
		return "configured"
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func (s *Service) apiUpstreamSchemeAllowed(scheme string) bool {
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case "https":
		return true
	case "http":
		return s.cfg.AllowHTTPAPIUpstreams
	default:
		return false
	}
}

func normalizeAccountCredentialFields(account Account) Account {
	if strings.TrimSpace(account.AccountType) == "" {
		account.AccountType = AccountTypeCodexOAuth
	}
	if account.Metadata == nil {
		account.Metadata = account.Credential.Metadata
	}
	if account.Metadata == nil {
		account.Metadata = map[string]string{}
	}
	if account.Credential.Metadata == nil {
		account.Credential.Metadata = account.Metadata
	}
	if account.EncryptedAccessToken == "" {
		account.EncryptedAccessToken = account.Credential.EncryptedAccessToken
	}
	if account.EncryptedRefreshToken == "" {
		account.EncryptedRefreshToken = account.Credential.EncryptedRefreshToken
	}
	if account.EncryptedIDToken == "" {
		account.EncryptedIDToken = account.Credential.EncryptedIDToken
	}
	if account.AccessTokenExpiresAt == nil {
		account.AccessTokenExpiresAt = account.Credential.AccessTokenExpiresAt
	}
	if account.LastRefreshAt == nil {
		account.LastRefreshAt = account.Credential.LastRefreshAt
	}
	if account.LastRefreshError == "" {
		account.LastRefreshError = account.Credential.LastRefreshError
	}
	if account.LastRefreshErrorAt == nil {
		account.LastRefreshErrorAt = account.Credential.LastRefreshErrorAt
	}
	if account.Credential.EncryptedAccessToken == "" {
		account.Credential.EncryptedAccessToken = account.EncryptedAccessToken
	}
	if account.Credential.EncryptedRefreshToken == "" {
		account.Credential.EncryptedRefreshToken = account.EncryptedRefreshToken
	}
	if account.Credential.EncryptedIDToken == "" {
		account.Credential.EncryptedIDToken = account.EncryptedIDToken
	}
	if account.Credential.AccessTokenExpiresAt == nil {
		account.Credential.AccessTokenExpiresAt = account.AccessTokenExpiresAt
	}
	if account.Credential.LastRefreshAt == nil {
		account.Credential.LastRefreshAt = account.LastRefreshAt
	}
	if account.Credential.LastRefreshError == "" {
		account.Credential.LastRefreshError = account.LastRefreshError
	}
	if account.Credential.LastRefreshErrorAt == nil {
		account.Credential.LastRefreshErrorAt = account.LastRefreshErrorAt
	}
	if account.BaseURL == "" {
		account.BaseURL = account.Credential.BaseURL
	}
	if account.Credential.BaseURL == "" {
		account.Credential.BaseURL = account.BaseURL
	}
	return account
}

func accountDisplayName(account Account) string {
	for _, value := range []string{account.Name, account.DisplayName, account.Subject, account.Provider} {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (s *Service) selectionCandidatesForEndpoint(ctx context.Context, model, endpoint string, excludedAccountIDs []int64) ([]Account, bool, error, error) {
	model = strings.TrimSpace(model)
	if model != "" {
		now := time.Now()
		excluded := normalizedExcludedAccountIDs(excludedAccountIDs)
		accounts, err := s.listEligibleAccountsForModelAndEndpoint(ctx, model, endpoint, excluded, now)
		if err != nil {
			return nil, false, ErrModelUnavailable, err
		}
		notFoundErr := ErrAccountsUnavailable
		if len(accounts) == 0 {
			hasEnabled, err := s.repo.HasEnabledAccounts(ctx, s.cfg.Provider)
			if err != nil {
				return nil, false, ErrAccountsUnavailable, err
			}
			if !hasEnabled {
				return accounts, false, ErrAccountsDisabled, nil
			}
			if len(excluded) > 0 {
				availableWithoutExclusions, err := s.listEligibleAccountsForModelAndEndpoint(ctx, model, endpoint, nil, now)
				if err != nil {
					return nil, false, ErrModelUnavailable, err
				}
				if len(availableWithoutExclusions) > 0 {
					return accounts, true, ErrAccountsUnavailable, nil
				}
			}
			baseAccounts, err := s.repo.ListEligibleAccountsForModel(ctx, s.cfg.Provider, model, nil, now)
			if err != nil {
				return nil, false, ErrModelUnavailable, err
			}
			if endpoint != "" && len(baseAccounts) > 0 {
				notFoundErr = ErrEndpointUnavailable
			} else {
				notFoundErr = ErrModelUnavailable
			}
		}
		return accounts, true, notFoundErr, nil
	}

	accounts, err := s.repo.ListAccounts(ctx, s.cfg.Provider)
	if err != nil {
		return nil, false, ErrAccountsUnavailable, err
	}
	if len(accounts) == 0 {
		return nil, false, ErrAccountsUnavailable, ErrNotConnected
	}

	excluded := make(map[int64]struct{}, len(excludedAccountIDs))
	for _, id := range excludedAccountIDs {
		if id > 0 {
			excluded[id] = struct{}{}
		}
	}

	candidates := make([]Account, 0, len(accounts))
	hasEnabled := false
	now := time.Now()
	for _, account := range accounts {
		if !account.Enabled {
			continue
		}
		hasEnabled = true
		if _, ok := excluded[account.ID]; ok {
			continue
		}
		if !accountSchedulable(account, now) {
			continue
		}
		candidates = append(candidates, account)
	}
	return candidates, hasEnabled, ErrAccountsUnavailable, nil
}

func (s *Service) selectionCandidatesForRoutingPoolForEndpoint(ctx context.Context, routingPoolID int64, model, endpoint string, excludedAccountIDs []int64) ([]Account, bool, error, error) {
	pool, err := s.repo.FindRoutingPool(ctx, routingPoolID)
	if err != nil {
		if errors.Is(err, ErrRoutingPoolNotFound) {
			return nil, true, ErrRoutingPoolNotFound, nil
		}
		return nil, false, ErrAccountsUnavailable, err
	}
	if !pool.Enabled {
		return nil, false, ErrAccountsDisabled, nil
	}

	model = strings.TrimSpace(model)
	now := time.Now()
	excluded := normalizedExcludedAccountIDs(excludedAccountIDs)
	accounts, err := s.listAccountsForRoutingPoolAndEndpoint(ctx, routingPoolID, model, endpoint, excluded, now)
	if err != nil {
		return nil, false, ErrAccountsUnavailable, err
	}
	if len(accounts) > 0 {
		return accounts, true, ErrAccountsUnavailable, nil
	}

	availableWithoutExclusions, err := s.listAccountsForRoutingPoolAndEndpoint(ctx, routingPoolID, model, endpoint, nil, now)
	if err != nil {
		return nil, false, ErrAccountsUnavailable, err
	}
	if len(availableWithoutExclusions) > 0 {
		return accounts, true, ErrAccountsUnavailable, nil
	}
	hasPoolAccounts, err := s.repo.RoutingPoolHasAccounts(ctx, routingPoolID)
	if err != nil {
		return nil, false, ErrAccountsUnavailable, err
	}
	if !hasPoolAccounts {
		return accounts, true, ErrRoutingPoolEmpty, nil
	}
	if model != "" {
		baseAccounts, err := s.repo.ListAccountsForRoutingPool(ctx, s.cfg.Provider, routingPoolID, model, nil, now)
		if err != nil {
			return nil, false, ErrAccountsUnavailable, err
		}
		if endpoint != "" && len(baseAccounts) > 0 {
			return accounts, true, ErrEndpointUnavailable, nil
		}
		return accounts, true, ErrModelUnavailable, nil
	}
	return accounts, true, ErrRoutingPoolEmpty, nil
}

func (s *Service) listEligibleAccountsForModelAndEndpoint(ctx context.Context, model, endpoint string, excluded []int64, now time.Time) ([]Account, error) {
	var (
		accounts []Account
		err      error
	)
	if endpoint != "" {
		if repo, ok := s.repo.(EndpointAwareRepository); ok {
			accounts, err = repo.ListEligibleAccountsForModelAndEndpoint(ctx, s.cfg.Provider, model, endpoint, excluded, now)
		} else {
			accounts, err = s.repo.ListEligibleAccountsForModel(ctx, s.cfg.Provider, model, excluded, now)
		}
	} else {
		accounts, err = s.repo.ListEligibleAccountsForModel(ctx, s.cfg.Provider, model, excluded, now)
	}
	if err != nil {
		return nil, err
	}
	return s.filterAccountsByEndpoint(ctx, model, endpoint, accounts)
}

func (s *Service) listAccountsForRoutingPoolAndEndpoint(ctx context.Context, routingPoolID int64, model, endpoint string, excluded []int64, now time.Time) ([]Account, error) {
	var (
		accounts []Account
		err      error
	)
	if endpoint != "" {
		if repo, ok := s.repo.(EndpointAwareRepository); ok {
			accounts, err = repo.ListAccountsForRoutingPoolAndEndpoint(ctx, s.cfg.Provider, routingPoolID, model, endpoint, excluded, now)
		} else {
			accounts, err = s.repo.ListAccountsForRoutingPool(ctx, s.cfg.Provider, routingPoolID, model, excluded, now)
		}
	} else {
		accounts, err = s.repo.ListAccountsForRoutingPool(ctx, s.cfg.Provider, routingPoolID, model, excluded, now)
	}
	if err != nil {
		return nil, err
	}
	return s.filterAccountsByEndpoint(ctx, model, endpoint, accounts)
}

func (s *Service) filterAccountsByEndpoint(ctx context.Context, model, endpoint string, accounts []Account) ([]Account, error) {
	filteredAccounts := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		if account.AccountType == AccountTypeAPIUpstream {
			filteredAccounts = append(filteredAccounts, account)
		}
	}
	accounts = filteredAccounts
	if strings.TrimSpace(model) == "" || strings.TrimSpace(endpoint) == "" || len(accounts) == 0 {
		return accounts, nil
	}
	accountIDs := make([]int64, 0, len(accounts))
	for _, account := range accounts {
		accountIDs = append(accountIDs, account.ID)
	}
	modelsByAccount, err := s.repo.ListAccountModelsForAccounts(ctx, s.cfg.Provider, accountIDs)
	if err != nil {
		return nil, err
	}
	filtered := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		unsupported := false
		for _, modelItem := range modelsByAccount[account.ID] {
			if modelItem.Model == model && modelItem.Enabled && endpointCapabilityUnsupported(modelItem.Metadata, endpoint) {
				unsupported = true
				break
			}
		}
		if !unsupported {
			filtered = append(filtered, account)
		}
	}
	return filtered, nil
}

func normalizedExcludedAccountIDs(ids []int64) []int64 {
	excluded := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id > 0 {
			excluded = append(excluded, id)
		}
	}
	return excluded
}

func AccountSchedulable(account Account, now time.Time) bool {
	if account.AccountType != AccountTypeAPIUpstream {
		return false
	}
	if !account.Enabled {
		return false
	}
	switch account.Status {
	case "", AccountStatusActive:
	case AccountStatusRateLimited:
		if account.RateLimitedUntil == nil || account.RateLimitedUntil.After(now) {
			return false
		}
	case AccountStatusCircuitOpen:
		if account.CircuitOpenUntil == nil || account.CircuitOpenUntil.After(now) {
			return false
		}
	case AccountStatusDisabled, AccountStatusExpired:
		return false
	default:
		return false
	}
	if account.RateLimitedUntil != nil && account.RateLimitedUntil.After(now) {
		return false
	}
	if account.CircuitOpenUntil != nil && account.CircuitOpenUntil.After(now) {
		return false
	}
	return true
}

func accountUnschedulableReason(account Account, now time.Time) string {
	if account.AccountType != AccountTypeAPIUpstream {
		return "unsupported account type"
	}
	if !account.Enabled {
		return "account disabled"
	}
	switch account.Status {
	case "", AccountStatusActive:
	case AccountStatusRateLimited:
		if account.RateLimitedUntil == nil || account.RateLimitedUntil.After(now) {
			return "account rate limited"
		}
	case AccountStatusCircuitOpen:
		if account.CircuitOpenUntil == nil || account.CircuitOpenUntil.After(now) {
			return "account circuit open"
		}
	case AccountStatusDisabled:
		return "account disabled"
	case AccountStatusExpired:
		return "account expired"
	default:
		return "status " + account.Status
	}
	if account.RateLimitedUntil != nil && account.RateLimitedUntil.After(now) {
		return "account rate limited"
	}
	if account.CircuitOpenUntil != nil && account.CircuitOpenUntil.After(now) {
		return "account circuit open"
	}
	return ""
}

func accountSchedulable(account Account, now time.Time) bool {
	return AccountSchedulable(account, now)
}

func retryAfterTime(value string, now time.Time, fallback time.Duration) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return now.Add(fallback)
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return now.Add(time.Duration(seconds) * time.Second)
	}
	if parsed, err := http.ParseTime(value); err == nil && parsed.After(now) {
		return parsed
	}
	return now.Add(fallback)
}

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func normalizedLoadFactor(value int) int {
	if value <= 0 {
		return 1
	}
	return value
}

func accountStatusTestRoute(cfg Config) string {
	return requestRoutePath(upstreamModelsURL(cfg.APIBaseURL), "/v1/models")
}
func accountStatusTestMethod(cfg Config) string { return http.MethodGet }
func accountStatusTestModel(cfg Config) string  { return "" }
func accountModelTestRoute(cfg Config, selected SelectedAccount) string {
	return requestRoutePath(upstreamChatCompletionsURL(selected.BaseURL), "/v1/chat/completions")
}

func upstreamModelsURL(baseURL string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(trimmed, "/v1") {
		return trimmed + "/models"
	}
	return trimmed + "/v1/models"
}
