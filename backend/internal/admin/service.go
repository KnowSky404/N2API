package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/secret"
)

const (
	defaultSessionTTL           = 7 * 24 * time.Hour
	sessionTokenName            = "admin_session"
	apiKeyTokenName             = "n2api"
	defaultModel                = "gpt-4.1"
	defaultUsagePricingModel    = "gpt-5"
	maxModels                   = 100
	maxModelNameLen             = 128
	maxRequestLogQueryLen       = 200
	APIKeyModelPolicyAll        = "all"
	APIKeyModelPolicySelected   = "selected"
	RequestLogStatusAll         = "all"
	RequestLogStatusSuccess     = "success"
	RequestLogStatusClientError = "client_error"
	RequestLogStatusServerError = "server_error"
)

var (
	ErrNotFound     = errors.New("not found")
	ErrUnauthorized = errors.New("unauthorized")
	ErrInvalidInput = errors.New("invalid input")
)

type Config struct {
	SessionTTL             time.Duration
	DefaultGatewaySettings GatewaySettings
}

type Admin struct {
	ID           int64
	Username     string
	PasswordHash string `json:"-"`
}

type Session struct {
	Token     string
	AdminID   int64
	ExpiresAt time.Time
}

type APIKey struct {
	ID                int64      `json:"id"`
	Name              string     `json:"name"`
	Prefix            string     `json:"prefix"`
	CreatedAt         time.Time  `json:"createdAt"`
	LastUsedAt        *time.Time `json:"lastUsedAt"`
	RevokedAt         *time.Time `json:"revokedAt"`
	DisabledAt        *time.Time `json:"disabledAt"`
	ModelPolicy       string     `json:"modelPolicy"`
	AllowedModels     []string   `json:"allowedModels"`
	RequestsPerMinute int        `json:"requestsPerMinute"`
	TokensPerMinute   int        `json:"tokensPerMinute"`
	RequestBudget24h  int        `json:"requestBudget24h"`
	TokenBudget24h    int        `json:"tokenBudget24h"`
	RequestBudget30d  int        `json:"requestBudget30d"`
	TokenBudget30d    int        `json:"tokenBudget30d"`
	RoutingPoolID     *int64     `json:"routingPoolId"`
	RoutingPoolName   string     `json:"routingPoolName"`
}

type RoutingPool struct {
	ID          int64                `json:"id"`
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Enabled     bool                 `json:"enabled"`
	AccountIDs  []int64              `json:"accountIds"`
	Accounts    []RoutingPoolAccount `json:"accounts,omitempty"`
	CreatedAt   time.Time            `json:"createdAt"`
	UpdatedAt   time.Time            `json:"updatedAt"`
}

type RoutingPoolAccount struct {
	AccountID int64 `json:"accountId"`
	Priority  int   `json:"priority"`
}

type APIKeyBudgetUsage struct {
	KeyID                 int64  `json:"-"`
	RequestsUsed24h       int64  `json:"requestsUsed24h"`
	TokensUsed24h         int64  `json:"tokensUsed24h"`
	RequestsUsed30d       int64  `json:"requestsUsed30d"`
	TokensUsed30d         int64  `json:"tokensUsed30d"`
	RequestsRemaining24h  *int64 `json:"requestsRemaining24h"`
	TokensRemaining24h    *int64 `json:"tokensRemaining24h"`
	RequestsRemaining30d  *int64 `json:"requestsRemaining30d"`
	TokensRemaining30d    *int64 `json:"tokensRemaining30d"`
	RequestBudgetExceeded bool   `json:"requestBudgetExceeded"`
	TokenBudgetExceeded   bool   `json:"tokenBudgetExceeded"`
}

type RequestLog struct {
	ID                    int64     `json:"id"`
	RequestID             string    `json:"requestId"`
	ClientKey             string    `json:"clientKey"`
	Provider              string    `json:"provider"`
	ProviderAccountID     int64     `json:"providerAccountId"`
	ProviderAccountType   string    `json:"providerAccountType"`
	ProviderAccountName   string    `json:"providerAccountName"`
	RoutingPoolID         int64     `json:"routingPoolId"`
	RoutingPoolName       string    `json:"routingPoolName"`
	Model                 string    `json:"model"`
	SessionID             string    `json:"sessionId"`
	Route                 string    `json:"route"`
	Method                string    `json:"method"`
	StatusCode            int       `json:"statusCode"`
	LatencyMS             int       `json:"latencyMs"`
	Error                 string    `json:"error"`
	InputTokens           int       `json:"inputTokens"`
	OutputTokens          int       `json:"outputTokens"`
	TotalTokens           int       `json:"totalTokens"`
	CachedInputTokens     int       `json:"cachedInputTokens"`
	ReasoningTokens       int       `json:"reasoningTokens"`
	UsageSource           string    `json:"usageSource"`
	EstimatedCostMicrousd int64     `json:"estimatedCostMicrousd"`
	PricingMatched        bool      `json:"pricingMatched"`
	GatewayAttemptCount   int       `json:"gatewayAttemptCount"`
	GatewayFallbackCount  int       `json:"gatewayFallbackCount"`
	CreatedAt             time.Time `json:"createdAt"`
}

type RequestLogFilter struct {
	Limit             int
	Query             string
	StatusClass       string
	ProviderAccountID int64
	ClientKeyID       int64
	Model             string
	SessionID         string
}

type UsageSummary struct {
	Range                 string            `json:"range"`
	GroupBy               string            `json:"groupBy"`
	TotalRequests         int64             `json:"totalRequests"`
	TotalInputTokens      int64             `json:"totalInputTokens"`
	TotalOutputTokens     int64             `json:"totalOutputTokens"`
	TotalTokens           int64             `json:"totalTokens"`
	EstimatedCostMicrousd int64             `json:"estimatedCostMicrousd"`
	Rows                  []UsageSummaryRow `json:"rows"`
}

type UsageSummaryRow struct {
	ID                    string `json:"id"`
	Label                 string `json:"label"`
	Requests              int64  `json:"requests"`
	InputTokens           int64  `json:"inputTokens"`
	OutputTokens          int64  `json:"outputTokens"`
	TotalTokens           int64  `json:"totalTokens"`
	EstimatedCostMicrousd int64  `json:"estimatedCostMicrousd"`
}

type UsagePricing struct {
	Version   int                   `json:"version"`
	Currency  string                `json:"currency"`
	Unit      string                `json:"unit"`
	UpdatedAt time.Time             `json:"updatedAt"`
	Models    map[string]UsagePrice `json:"models"`
}

type UsagePrice struct {
	InputMicrousdPerMillion       int64 `json:"inputMicrousdPerMillion"`
	CachedInputMicrousdPerMillion int64 `json:"cachedInputMicrousdPerMillion"`
	OutputMicrousdPerMillion      int64 `json:"outputMicrousdPerMillion"`
}

type UsageCostInput struct {
	Model             string
	InputTokens       int
	OutputTokens      int
	TotalTokens       int
	CachedInputTokens int
	ReasoningTokens   int
	Source            string
}

type UsageCostEstimate struct {
	Matched      bool
	CostMicrousd int64
	Snapshot     map[string]any
}

type ModelSettings struct {
	DefaultModel  string   `json:"defaultModel"`
	AllowedModels []string `json:"allowedModels"`
}

type GatewaySettings struct {
	MaxConcurrentGatewayRequests           int  `json:"maxConcurrentGatewayRequests"`
	MaxConcurrentRequestsPerAccount        int  `json:"maxConcurrentRequestsPerAccount"`
	MaxConcurrentRequestsPerKey            int  `json:"maxConcurrentRequestsPerKey"`
	RequestsPerMinutePerKey                int  `json:"requestsPerMinutePerKey"`
	TokensPerMinutePerKey                  int  `json:"tokensPerMinutePerKey"`
	ProviderAccountAutoTestEnabled         bool `json:"providerAccountAutoTestEnabled"`
	ProviderAccountAutoTestIntervalSeconds int  `json:"providerAccountAutoTestIntervalSeconds"`
}

type ModelRoutingStatus struct {
	DefaultModel  string              `json:"defaultModel"`
	AllowedModels []string            `json:"allowedModels"`
	Models        []ModelRoutingModel `json:"models"`
	Warnings      []string            `json:"warnings"`
}

type ModelRoutingModel struct {
	Model           string                `json:"model"`
	Allowed         bool                  `json:"allowed"`
	ConfiguredCount int                   `json:"configuredCount"`
	EnabledCount    int                   `json:"enabledCount"`
	Accounts        []ModelRoutingAccount `json:"accounts,omitempty"`
}

type ModelRoutingAccount struct {
	ID                  int64      `json:"id"`
	DisplayName         string     `json:"displayName"`
	AccountType         string     `json:"accountType"`
	Enabled             bool       `json:"enabled"`
	Priority            int        `json:"priority"`
	LoadFactor          int        `json:"loadFactor"`
	Status              string     `json:"status"`
	StatusReason        string     `json:"statusReason"`
	LastError           string     `json:"lastError"`
	LastErrorAt         *time.Time `json:"lastErrorAt"`
	LastUsedAt          *time.Time `json:"lastUsedAt"`
	ScheduleRank        int        `json:"scheduleRank"`
	Schedulable         bool       `json:"schedulable"`
	UnschedulableReason string     `json:"unschedulableReason"`
}

type CreatedAPIKey struct {
	Key    APIKey
	Secret string
}

type Repository interface {
	FindBootstrapAdmin(ctx context.Context) (Admin, error)
	FindAdminByUsername(ctx context.Context, username string) (Admin, error)
	CreateAdmin(ctx context.Context, username, passwordHash string) (Admin, error)
	UpdateAdminUsername(ctx context.Context, id int64, username string) (Admin, error)
	CreateSession(ctx context.Context, adminID int64, tokenHash string, expiresAt time.Time) error
	FindAdminBySessionHash(ctx context.Context, tokenHash string, now time.Time) (Admin, error)
	RevokeSession(ctx context.Context, tokenHash string) error
	CreateAPIKey(ctx context.Context, name, hash, prefix string) (APIKey, error)
	ListAPIKeys(ctx context.Context) ([]APIKey, error)
	RevokeAPIKey(ctx context.Context, id int64) (APIKey, error)
	FindAPIKeyByHash(ctx context.Context, hash string, now time.Time) (APIKey, error)
	UpdateAPIKeyName(ctx context.Context, id int64, name string) (APIKey, error)
	SetAPIKeyDisabled(ctx context.Context, id int64, disabled bool) (APIKey, error)
	UpdateAPIKeyModelPolicy(ctx context.Context, id int64, policy string, models []string) (APIKey, error)
	UpdateAPIKeyLimits(ctx context.Context, id int64, requestsPerMinute, tokensPerMinute int) (APIKey, error)
	UpdateAPIKeyBudgets(ctx context.Context, id int64, requestBudget24h, tokenBudget24h, requestBudget30d, tokenBudget30d int) (APIKey, error)
	ListRoutingPools(ctx context.Context) ([]RoutingPool, error)
	CreateRoutingPool(ctx context.Context, name, description string, enabled bool) (RoutingPool, error)
	UpdateRoutingPool(ctx context.Context, id int64, name, description string, enabled bool) (RoutingPool, error)
	DeleteRoutingPool(ctx context.Context, id int64) error
	ReplaceRoutingPoolAccounts(ctx context.Context, id int64, accounts []RoutingPoolAccount) (RoutingPool, error)
	UpdateAPIKeyRoutingPool(ctx context.Context, id int64, routingPoolID *int64) (APIKey, error)
	GetAPIKeyBudgetUsage(ctx context.Context, keyID int64, now time.Time) (APIKeyBudgetUsage, error)
	ListAPIKeyModels(ctx context.Context, id int64) ([]string, error)
	TouchAPIKey(ctx context.Context, id int64, usedAt time.Time) error
	ListRequestLogs(ctx context.Context, filter RequestLogFilter) ([]RequestLog, error)
	GetUsageSummary(ctx context.Context, since time.Time, groupBy string) (UsageSummary, error)
	GetUsagePricing(ctx context.Context) (UsagePricing, error)
	SaveUsagePricing(ctx context.Context, pricing UsagePricing) (UsagePricing, error)
	GetModelSettings(ctx context.Context) (ModelSettings, error)
	SaveModelSettings(ctx context.Context, settings ModelSettings) (ModelSettings, error)
	GetGatewaySettings(ctx context.Context) (GatewaySettings, error)
	SaveGatewaySettings(ctx context.Context, settings GatewaySettings) (GatewaySettings, error)
}

type Service struct {
	repo                   Repository
	sessionTTL             time.Duration
	defaultGatewaySettings GatewaySettings
}

func NewService(repo Repository, cfg Config) *Service {
	sessionTTL := cfg.SessionTTL
	if sessionTTL <= 0 {
		sessionTTL = defaultSessionTTL
	}

	return &Service{
		repo:                   repo,
		sessionTTL:             sessionTTL,
		defaultGatewaySettings: cfg.DefaultGatewaySettings,
	}
}

func (s *Service) BootstrapAdmin(ctx context.Context, username, password string) error {
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	if username == "" || password == "" {
		return ErrInvalidInput
	}

	existing, err := s.repo.FindBootstrapAdmin(ctx)
	if err == nil {
		if existing.Username == username {
			return nil
		}
		_, err = s.repo.UpdateAdminUsername(ctx, existing.ID, username)
		return err
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}

	hash, err := secret.HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash admin password: %w", err)
	}

	_, err = s.repo.CreateAdmin(ctx, username, hash)
	return err
}

func (s *Service) Login(ctx context.Context, username, password string) (Session, error) {
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	if username == "" || password == "" {
		return Session{}, ErrUnauthorized
	}

	admin, err := s.repo.FindAdminByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Session{}, ErrUnauthorized
		}
		return Session{}, err
	}
	if !secret.VerifyPassword(admin.PasswordHash, password) {
		return Session{}, ErrUnauthorized
	}

	token, err := secret.GenerateToken(sessionTokenName)
	if err != nil {
		return Session{}, fmt.Errorf("generate admin session token: %w", err)
	}
	expiresAt := time.Now().Add(s.sessionTTL)
	if err := s.repo.CreateSession(ctx, admin.ID, secret.HashAPIKey(token), expiresAt); err != nil {
		return Session{}, err
	}

	return Session{Token: token, AdminID: admin.ID, ExpiresAt: expiresAt}, nil
}

func (s *Service) ValidateSession(ctx context.Context, token string) (Admin, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Admin{}, ErrUnauthorized
	}

	admin, err := s.repo.FindAdminBySessionHash(ctx, secret.HashAPIKey(token), time.Now())
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Admin{}, ErrUnauthorized
		}
		return Admin{}, err
	}
	return admin, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	if err := s.repo.RevokeSession(ctx, secret.HashAPIKey(token)); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	return nil
}

func (s *Service) CreateAPIKey(ctx context.Context, name string) (CreatedAPIKey, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return CreatedAPIKey{}, ErrInvalidInput
	}

	token, err := secret.GenerateToken(apiKeyTokenName)
	if err != nil {
		return CreatedAPIKey{}, fmt.Errorf("generate api key: %w", err)
	}
	key, err := s.repo.CreateAPIKey(ctx, name, secret.HashAPIKey(token), secret.TokenPrefix(token))
	if err != nil {
		return CreatedAPIKey{}, err
	}

	return CreatedAPIKey{Key: key, Secret: token}, nil
}

func (s *Service) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	return s.repo.ListAPIKeys(ctx)
}

func (s *Service) RevokeAPIKey(ctx context.Context, id int64) (APIKey, error) {
	return s.repo.RevokeAPIKey(ctx, id)
}

func (s *Service) UpdateAPIKeyName(ctx context.Context, id int64, name string) (APIKey, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return APIKey{}, ErrInvalidInput
	}
	return s.repo.UpdateAPIKeyName(ctx, id, name)
}

func (s *Service) SetAPIKeyDisabled(ctx context.Context, id int64, disabled bool) (APIKey, error) {
	return s.repo.SetAPIKeyDisabled(ctx, id, disabled)
}

func (s *Service) UpdateAPIKeyModelPolicy(ctx context.Context, id int64, policy string, models []string) (APIKey, error) {
	policy = strings.TrimSpace(policy)
	switch policy {
	case APIKeyModelPolicyAll:
		return s.repo.UpdateAPIKeyModelPolicy(ctx, id, policy, nil)
	case APIKeyModelPolicySelected:
		normalized, err := normalizeModelList(models)
		if err != nil {
			return APIKey{}, err
		}
		if len(normalized) == 0 {
			return APIKey{}, ErrInvalidInput
		}
		return s.repo.UpdateAPIKeyModelPolicy(ctx, id, policy, normalized)
	default:
		return APIKey{}, ErrInvalidInput
	}
}

func (s *Service) UpdateAPIKeyLimits(ctx context.Context, id int64, requestsPerMinute, tokensPerMinute int) (APIKey, error) {
	if requestsPerMinute < 0 || tokensPerMinute < 0 {
		return APIKey{}, ErrInvalidInput
	}
	return s.repo.UpdateAPIKeyLimits(ctx, id, requestsPerMinute, tokensPerMinute)
}

func (s *Service) UpdateAPIKeyBudgets(ctx context.Context, id int64, requestBudget24h, tokenBudget24h, requestBudget30d, tokenBudget30d int) (APIKey, error) {
	if requestBudget24h < 0 || tokenBudget24h < 0 || requestBudget30d < 0 || tokenBudget30d < 0 {
		return APIKey{}, ErrInvalidInput
	}
	return s.repo.UpdateAPIKeyBudgets(ctx, id, requestBudget24h, tokenBudget24h, requestBudget30d, tokenBudget30d)
}

func (s *Service) ListRoutingPools(ctx context.Context) ([]RoutingPool, error) {
	return s.repo.ListRoutingPools(ctx)
}

func (s *Service) CreateRoutingPool(ctx context.Context, name, description string, enabled bool) (RoutingPool, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if name == "" {
		return RoutingPool{}, ErrInvalidInput
	}
	return s.repo.CreateRoutingPool(ctx, name, description, enabled)
}

func (s *Service) UpdateRoutingPool(ctx context.Context, id int64, name, description string, enabled bool) (RoutingPool, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if id <= 0 || name == "" {
		return RoutingPool{}, ErrInvalidInput
	}
	return s.repo.UpdateRoutingPool(ctx, id, name, description, enabled)
}

func (s *Service) DeleteRoutingPool(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrInvalidInput
	}
	return s.repo.DeleteRoutingPool(ctx, id)
}

func (s *Service) ReplaceRoutingPoolAccounts(ctx context.Context, id int64, accounts []RoutingPoolAccount) (RoutingPool, error) {
	if id <= 0 {
		return RoutingPool{}, ErrInvalidInput
	}
	normalized := make([]RoutingPoolAccount, 0, len(accounts))
	seen := map[int64]struct{}{}
	for _, account := range accounts {
		if account.AccountID <= 0 || account.Priority < 0 {
			return RoutingPool{}, ErrInvalidInput
		}
		if _, ok := seen[account.AccountID]; ok {
			continue
		}
		seen[account.AccountID] = struct{}{}
		normalized = append(normalized, account)
	}
	return s.repo.ReplaceRoutingPoolAccounts(ctx, id, normalized)
}

func (s *Service) UpdateAPIKeyRoutingPool(ctx context.Context, id int64, routingPoolID *int64) (APIKey, error) {
	if id <= 0 {
		return APIKey{}, ErrInvalidInput
	}
	if routingPoolID != nil && *routingPoolID < 0 {
		return APIKey{}, ErrInvalidInput
	}
	if routingPoolID != nil && *routingPoolID == 0 {
		routingPoolID = nil
	}
	return s.repo.UpdateAPIKeyRoutingPool(ctx, id, routingPoolID)
}

func (s *Service) GetAPIKeyBudgetUsage(ctx context.Context, key APIKey, now time.Time) (APIKeyBudgetUsage, error) {
	usage, err := s.repo.GetAPIKeyBudgetUsage(ctx, key.ID, now)
	if err != nil {
		return APIKeyBudgetUsage{}, err
	}
	applyBudgetRemaining(&usage, key)
	return usage, nil
}

func applyBudgetRemaining(usage *APIKeyBudgetUsage, key APIKey) {
	usage.KeyID = key.ID
	usage.RequestsRemaining24h = remainingBudget(key.RequestBudget24h, usage.RequestsUsed24h)
	usage.TokensRemaining24h = remainingBudget(key.TokenBudget24h, usage.TokensUsed24h)
	usage.RequestsRemaining30d = remainingBudget(key.RequestBudget30d, usage.RequestsUsed30d)
	usage.TokensRemaining30d = remainingBudget(key.TokenBudget30d, usage.TokensUsed30d)
	usage.RequestBudgetExceeded = budgetExceeded(key.RequestBudget24h, usage.RequestsUsed24h) ||
		budgetExceeded(key.RequestBudget30d, usage.RequestsUsed30d)
	usage.TokenBudgetExceeded = budgetExceeded(key.TokenBudget24h, usage.TokensUsed24h) ||
		budgetExceeded(key.TokenBudget30d, usage.TokensUsed30d)
}

func remainingBudget(limit int, used int64) *int64 {
	if limit <= 0 {
		return nil
	}
	remaining := int64(limit) - used
	if remaining < 0 {
		remaining = 0
	}
	return &remaining
}

func budgetExceeded(limit int, used int64) bool {
	return limit > 0 && used >= int64(limit)
}

func (s *Service) APIKeyAllowsModel(key APIKey, model string) bool {
	switch key.ModelPolicy {
	case "", APIKeyModelPolicyAll:
		return true
	case APIKeyModelPolicySelected:
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

func (s *Service) AuthenticateAPIKey(ctx context.Context, apiKey string) (APIKey, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return APIKey{}, ErrUnauthorized
	}

	key, err := s.repo.FindAPIKeyByHash(ctx, secret.HashAPIKey(apiKey), time.Now())
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return APIKey{}, ErrUnauthorized
		}
		return APIKey{}, err
	}
	if key.RevokedAt != nil {
		return APIKey{}, ErrUnauthorized
	}
	if key.DisabledAt != nil {
		return APIKey{}, ErrUnauthorized
	}
	if err := s.repo.TouchAPIKey(ctx, key.ID, time.Now()); err != nil {
		if errors.Is(err, ErrNotFound) {
			return APIKey{}, ErrUnauthorized
		}
		return APIKey{}, err
	}

	return key, nil
}

func (s *Service) ListRequestLogs(ctx context.Context, filter RequestLogFilter) ([]RequestLog, error) {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 200 {
		filter.Limit = 200
	}
	filter.Query = strings.TrimSpace(filter.Query)
	if len(filter.Query) > maxRequestLogQueryLen {
		return nil, ErrInvalidInput
	}
	filter.Model = strings.TrimSpace(filter.Model)
	filter.SessionID = strings.TrimSpace(filter.SessionID)
	if len(filter.Model) > 100 || len(filter.SessionID) > 100 {
		return nil, ErrInvalidInput
	}
	filter.StatusClass = strings.TrimSpace(filter.StatusClass)
	if filter.StatusClass == "" {
		filter.StatusClass = RequestLogStatusAll
	}
	switch filter.StatusClass {
	case RequestLogStatusAll, RequestLogStatusSuccess, RequestLogStatusClientError, RequestLogStatusServerError:
	default:
		return nil, ErrInvalidInput
	}
	if filter.ProviderAccountID < 0 {
		return nil, ErrInvalidInput
	}
	if filter.ClientKeyID < 0 {
		return nil, ErrInvalidInput
	}
	return s.repo.ListRequestLogs(ctx, filter)
}

func (s *Service) GetUsageSummary(ctx context.Context, rangeName, groupBy string) (UsageSummary, error) {
	rangeName = strings.TrimSpace(rangeName)
	if rangeName == "" {
		rangeName = "7d"
	}
	groupBy = strings.TrimSpace(groupBy)
	if groupBy == "" {
		groupBy = "model"
	}
	since, ok := usageSummarySince(rangeName, time.Now())
	if !ok {
		return UsageSummary{}, ErrInvalidInput
	}
	if !validUsageSummaryGroup(groupBy) {
		return UsageSummary{}, ErrInvalidInput
	}
	summary, err := s.repo.GetUsageSummary(ctx, since, groupBy)
	if err != nil {
		return UsageSummary{}, err
	}
	summary.Range = rangeName
	summary.GroupBy = groupBy
	summary.recalculateTotals()
	return summary, nil
}

func usageSummarySince(rangeName string, now time.Time) (time.Time, bool) {
	switch rangeName {
	case "24h":
		return now.Add(-24 * time.Hour), true
	case "7d":
		return now.Add(-7 * 24 * time.Hour), true
	case "30d":
		return now.Add(-30 * 24 * time.Hour), true
	default:
		return time.Time{}, false
	}
}

func validUsageSummaryGroup(groupBy string) bool {
	switch groupBy {
	case "client_key", "provider_account", "model", "session":
		return true
	default:
		return false
	}
}

func (s *UsageSummary) recalculateTotals() {
	s.TotalRequests = 0
	s.TotalInputTokens = 0
	s.TotalOutputTokens = 0
	s.TotalTokens = 0
	s.EstimatedCostMicrousd = 0
	for _, row := range s.Rows {
		s.TotalRequests += row.Requests
		s.TotalInputTokens += row.InputTokens
		s.TotalOutputTokens += row.OutputTokens
		s.TotalTokens += row.TotalTokens
		s.EstimatedCostMicrousd += row.EstimatedCostMicrousd
	}
}

func (s *Service) GetUsagePricing(ctx context.Context) (UsagePricing, error) {
	pricing, err := s.repo.GetUsagePricing(ctx)
	if err == nil {
		return normalizeUsagePricing(pricing)
	}
	if errors.Is(err, ErrNotFound) {
		return defaultUsagePricing(), nil
	}
	return UsagePricing{}, err
}

func (s *Service) UpdateUsagePricing(ctx context.Context, pricing UsagePricing) (UsagePricing, error) {
	normalized, err := normalizeUsagePricing(pricing)
	if err != nil {
		return UsagePricing{}, err
	}
	normalized.UpdatedAt = time.Now().UTC()
	return s.repo.SaveUsagePricing(ctx, normalized)
}

func (s *Service) EstimateUsageCost(ctx context.Context, usage UsageCostInput) (UsageCostEstimate, error) {
	pricing, err := s.GetUsagePricing(ctx)
	if err != nil {
		return UsageCostEstimate{}, err
	}
	price, ok := pricing.Models[strings.TrimSpace(usage.Model)]
	snapshot := map[string]any{
		"matched":   ok,
		"model":     strings.TrimSpace(usage.Model),
		"currency":  pricing.Currency,
		"unit":      pricing.Unit,
		"version":   pricing.Version,
		"updatedAt": pricing.UpdatedAt,
	}
	if !ok {
		return UsageCostEstimate{Matched: false, Snapshot: snapshot}, nil
	}
	snapshot["inputMicrousdPerMillion"] = price.InputMicrousdPerMillion
	snapshot["cachedInputMicrousdPerMillion"] = price.CachedInputMicrousdPerMillion
	snapshot["outputMicrousdPerMillion"] = price.OutputMicrousdPerMillion
	return UsageCostEstimate{
		Matched:      true,
		CostMicrousd: estimateCostMicrousd(usage, price),
		Snapshot:     snapshot,
	}, nil
}

func (s *Service) GetModelSettings(ctx context.Context) (ModelSettings, error) {
	settings, err := s.repo.GetModelSettings(ctx)
	if err == nil {
		return normalizeModelSettings(settings)
	}
	if errors.Is(err, ErrNotFound) {
		return defaultModelSettings(), nil
	}
	return ModelSettings{}, err
}

func (s *Service) UpdateModelSettings(ctx context.Context, settings ModelSettings) (ModelSettings, error) {
	normalized, err := normalizeModelSettings(settings)
	if err != nil {
		return ModelSettings{}, err
	}
	return s.repo.SaveModelSettings(ctx, normalized)
}

func (s *Service) GetGatewaySettings(ctx context.Context) (GatewaySettings, error) {
	settings, err := s.repo.GetGatewaySettings(ctx)
	if err == nil {
		return normalizeGatewaySettings(settings)
	}
	if errors.Is(err, ErrNotFound) {
		return normalizeGatewaySettings(s.defaultGatewaySettings)
	}
	return GatewaySettings{}, err
}

func (s *Service) UpdateGatewaySettings(ctx context.Context, settings GatewaySettings) (GatewaySettings, error) {
	normalized, err := normalizeGatewaySettings(settings)
	if err != nil {
		return GatewaySettings{}, err
	}
	return s.repo.SaveGatewaySettings(ctx, normalized)
}

func (s *Service) DefaultModel(ctx context.Context) (string, error) {
	settings, err := s.GetModelSettings(ctx)
	if err != nil {
		return "", err
	}
	return settings.DefaultModel, nil
}

func (s *Service) IsModelAllowed(ctx context.Context, model string) (bool, error) {
	settings, err := s.GetModelSettings(ctx)
	if err != nil {
		return false, err
	}
	model = strings.TrimSpace(model)
	for _, allowed := range settings.AllowedModels {
		if model == allowed {
			return true, nil
		}
	}
	return false, nil
}

func defaultUsagePricing() UsagePricing {
	return UsagePricing{
		Version:   1,
		Currency:  "USD",
		Unit:      "1M_tokens",
		UpdatedAt: time.Now().UTC(),
		Models: map[string]UsagePrice{
			defaultUsagePricingModel: {},
		},
	}
}

func normalizeUsagePricing(pricing UsagePricing) (UsagePricing, error) {
	version := pricing.Version
	if version == 0 {
		version = 1
	}
	currency := strings.ToUpper(strings.TrimSpace(pricing.Currency))
	if currency == "" {
		currency = "USD"
	}
	unit := strings.TrimSpace(pricing.Unit)
	if unit == "" {
		unit = "1M_tokens"
	}
	if version != 1 || currency != "USD" || unit != "1M_tokens" || len(pricing.Models) == 0 {
		return UsagePricing{}, ErrInvalidInput
	}

	models := make(map[string]UsagePrice, len(pricing.Models))
	for rawModel, price := range pricing.Models {
		model := strings.TrimSpace(rawModel)
		if model == "" || len(model) > maxModelNameLen {
			return UsagePricing{}, ErrInvalidInput
		}
		if price.InputMicrousdPerMillion < 0 || price.CachedInputMicrousdPerMillion < 0 || price.OutputMicrousdPerMillion < 0 {
			return UsagePricing{}, ErrInvalidInput
		}
		models[model] = price
	}
	if len(models) == 0 {
		return UsagePricing{}, ErrInvalidInput
	}

	return UsagePricing{
		Version:   version,
		Currency:  currency,
		Unit:      unit,
		UpdatedAt: pricing.UpdatedAt,
		Models:    models,
	}, nil
}

func estimateCostMicrousd(usage UsageCostInput, price UsagePrice) int64 {
	cachedInput := usage.CachedInputTokens
	if cachedInput < 0 {
		cachedInput = 0
	}
	billableInput := usage.InputTokens - cachedInput
	if billableInput < 0 {
		billableInput = 0
	}
	return costPartMicrousd(billableInput, price.InputMicrousdPerMillion) +
		costPartMicrousd(cachedInput, price.CachedInputMicrousdPerMillion) +
		costPartMicrousd(usage.OutputTokens, price.OutputMicrousdPerMillion)
}

func costPartMicrousd(tokens int, rateMicrousdPerMillion int64) int64 {
	if tokens <= 0 || rateMicrousdPerMillion <= 0 {
		return 0
	}
	return (int64(tokens)*rateMicrousdPerMillion + 500_000) / 1_000_000
}

func defaultModelSettings() ModelSettings {
	return ModelSettings{
		DefaultModel: defaultModel,
		AllowedModels: []string{
			defaultModel,
			"gpt-4.1-mini",
			"gpt-4o",
			"gpt-4o-mini",
		},
	}
}

func normalizeModelSettings(settings ModelSettings) (ModelSettings, error) {
	defaultName := strings.TrimSpace(settings.DefaultModel)
	if defaultName == "" {
		return ModelSettings{}, ErrInvalidInput
	}

	allowed, err := normalizeModelList(settings.AllowedModels)
	if err != nil {
		return ModelSettings{}, err
	}
	defaultAllowed := false
	for _, model := range allowed {
		if model == defaultName {
			defaultAllowed = true
		}
	}
	if len(allowed) == 0 || len(defaultName) > maxModelNameLen || !defaultAllowed {
		return ModelSettings{}, ErrInvalidInput
	}

	return ModelSettings{DefaultModel: defaultName, AllowedModels: allowed}, nil
}

func normalizeModelList(models []string) ([]string, error) {
	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(models))
	for _, raw := range models {
		model := strings.TrimSpace(raw)
		if model == "" {
			continue
		}
		if len(model) > maxModelNameLen {
			return nil, ErrInvalidInput
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		normalized = append(normalized, model)
		if len(normalized) > maxModels {
			return nil, ErrInvalidInput
		}
	}
	return normalized, nil
}

func normalizeGatewaySettings(settings GatewaySettings) (GatewaySettings, error) {
	if settings.MaxConcurrentGatewayRequests < 0 ||
		settings.MaxConcurrentRequestsPerAccount < 0 ||
		settings.MaxConcurrentRequestsPerKey < 0 ||
		settings.RequestsPerMinutePerKey < 0 ||
		settings.TokensPerMinutePerKey < 0 ||
		settings.ProviderAccountAutoTestIntervalSeconds < 0 {
		return GatewaySettings{}, ErrInvalidInput
	}
	if settings.ProviderAccountAutoTestIntervalSeconds == 0 {
		settings.ProviderAccountAutoTestIntervalSeconds = 300
	}
	if settings.ProviderAccountAutoTestEnabled && settings.ProviderAccountAutoTestIntervalSeconds < 60 {
		return GatewaySettings{}, ErrInvalidInput
	}
	return settings, nil
}
