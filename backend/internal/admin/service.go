package admin

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/KnowSky404/N2API/backend/internal/secret"
	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

const (
	defaultSessionTTL             = 7 * 24 * time.Hour
	sessionTokenName              = "admin_session"
	apiKeyTokenName               = "n2api"
	defaultModel                  = "gpt-4.1"
	maxModels                     = 100
	maxModelNameLen               = 128
	maxRequestLogQueryLen         = 200
	maxSessionCreatedIPBytes      = 64
	maxSessionUserAgentBytes      = 256
	sessionTouchInterval          = time.Minute
	APIKeyModelPolicyAll          = "all"
	APIKeyModelPolicySelected     = "selected"
	APIKeyPhysicalDeleteRetention = 7 * 24 * time.Hour
	RequestLogStatusAll           = "all"
	RequestLogStatusSuccess       = "success"
	RequestLogStatusClientError   = "client_error"
	RequestLogStatusServerError   = "server_error"
	dummyAdminPasswordHash        = "pbkdf2-sha256$210000$AAECAwQFBgcICQoLDA0ODw$6M7ZGtW4Xq6fsrLYeKn/xgsZw5E2huTtOgwzsiPv+Vk"
)

const DefaultRequestLogRetentionBatchSize = 1000

var (
	ErrNotFound     = errors.New("not found")
	ErrUnauthorized = errors.New("unauthorized")
	ErrInvalidInput = errors.New("invalid input")
	ErrConflict     = errors.New("conflict")
)

type Config struct {
	SessionTTL             time.Duration
	EncryptionSecret       string
	DefaultGatewaySettings GatewaySettings
	SystemEvents           SystemEventRepository
}

type SystemEventFilter = systemevent.Filter
type SystemEventPage = systemevent.Page

type SystemEventRepository interface {
	List(ctx context.Context, filter systemevent.Filter) (systemevent.Page, error)
	Insert(ctx context.Context, event systemevent.Event) error
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

type SessionMetadata struct {
	CreatedIP string
	UserAgent string
}

type AdminSession struct {
	ID         int64     `json:"id"`
	Current    bool      `json:"current"`
	CreatedAt  time.Time `json:"createdAt"`
	LastUsedAt time.Time `json:"lastUsedAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
	CreatedIP  string    `json:"createdIp"`
	UserAgent  string    `json:"userAgent"`
	TokenHash  string    `json:"-"`
}

type APIKey struct {
	ID                    int64      `json:"id"`
	Name                  string     `json:"name"`
	Prefix                string     `json:"prefix"`
	SecretAvailable       bool       `json:"secretAvailable"`
	CreatedAt             time.Time  `json:"createdAt"`
	LastUsedAt            *time.Time `json:"lastUsedAt"`
	RevokedAt             *time.Time `json:"revokedAt"`
	DisabledAt            *time.Time `json:"disabledAt"`
	ModelPolicy           string     `json:"modelPolicy"`
	AllowedModels         []string   `json:"allowedModels"`
	RequestsPerMinute     int        `json:"requestsPerMinute"`
	TokensPerMinute       int        `json:"tokensPerMinute"`
	RequestBudget24h      int        `json:"requestBudget24h"`
	TokenBudget24h        int        `json:"tokenBudget24h"`
	CostBudgetMicrousd24h int64      `json:"costBudgetMicrousd24h"`
	RequestBudget30d      int        `json:"requestBudget30d"`
	TokenBudget30d        int        `json:"tokenBudget30d"`
	CostBudgetMicrousd30d int64      `json:"costBudgetMicrousd30d"`
	RoutingPoolID         *int64     `json:"routingPoolId"`
	RoutingPoolName       string     `json:"routingPoolName"`
}

type RoutingPool struct {
	ID               int64                `json:"id"`
	Name             string               `json:"name"`
	Description      string               `json:"description"`
	Enabled          bool                 `json:"enabled"`
	FallbackPoolID   *int64               `json:"fallbackPoolId"`
	FallbackPoolName string               `json:"fallbackPoolName"`
	AccountIDs       []int64              `json:"accountIds"`
	Accounts         []RoutingPoolAccount `json:"accounts,omitempty"`
	CreatedAt        time.Time            `json:"createdAt"`
	UpdatedAt        time.Time            `json:"updatedAt"`
}

type RoutingPoolAccount struct {
	AccountID int64 `json:"accountId"`
	Priority  int   `json:"priority"`
}

type APIKeyBudgetUsage struct {
	KeyID                    int64  `json:"-"`
	RequestsUsed24h          int64  `json:"requestsUsed24h"`
	TokensUsed24h            int64  `json:"tokensUsed24h"`
	CostMicrousd24h          int64  `json:"costMicrousd24h"`
	RequestsUsed30d          int64  `json:"requestsUsed30d"`
	TokensUsed30d            int64  `json:"tokensUsed30d"`
	CostMicrousd30d          int64  `json:"costMicrousd30d"`
	RequestsRemaining24h     *int64 `json:"requestsRemaining24h"`
	TokensRemaining24h       *int64 `json:"tokensRemaining24h"`
	CostRemainingMicrousd24h *int64 `json:"costRemainingMicrousd24h"`
	RequestsRemaining30d     *int64 `json:"requestsRemaining30d"`
	TokensRemaining30d       *int64 `json:"tokensRemaining30d"`
	CostRemainingMicrousd30d *int64 `json:"costRemainingMicrousd30d"`
	RequestBudgetExceeded    bool   `json:"requestBudgetExceeded"`
	TokenBudgetExceeded      bool   `json:"tokenBudgetExceeded"`
	CostBudgetExceeded       bool   `json:"costBudgetExceeded"`
}

type RequestLog struct {
	ID                       int64     `json:"id"`
	RequestID                string    `json:"requestId"`
	ClientKey                string    `json:"clientKey"`
	Provider                 string    `json:"provider"`
	ProviderAccountID        int64     `json:"providerAccountId"`
	ProviderAccountType      string    `json:"providerAccountType"`
	ProviderAccountName      string    `json:"providerAccountName"`
	RoutingPoolID            int64     `json:"routingPoolId"`
	RoutingPoolName          string    `json:"routingPoolName"`
	RoutingPoolFallbackDepth int       `json:"routingPoolFallbackDepth"`
	RoutingPoolFallbackChain string    `json:"routingPoolFallbackChain"`
	RoutingPoolError         string    `json:"routingPoolError"`
	Model                    string    `json:"model"`
	SessionID                string    `json:"sessionId"`
	Route                    string    `json:"route"`
	Method                   string    `json:"method"`
	StatusCode               int       `json:"statusCode"`
	LatencyMS                int       `json:"latencyMs"`
	Error                    string    `json:"error"`
	InputTokens              int       `json:"inputTokens"`
	OutputTokens             int       `json:"outputTokens"`
	TotalTokens              int       `json:"totalTokens"`
	CachedInputTokens        int       `json:"cachedInputTokens"`
	ReasoningTokens          int       `json:"reasoningTokens"`
	UsageSource              string    `json:"usageSource"`
	EstimatedCostMicrousd    int64     `json:"estimatedCostMicrousd"`
	PricingMatched           bool      `json:"pricingMatched"`
	GatewayAttemptCount      int       `json:"gatewayAttemptCount"`
	GatewayFallbackCount     int       `json:"gatewayFallbackCount"`
	CreatedAt                time.Time `json:"createdAt"`
}

type RequestLogPage struct {
	Logs       []RequestLog `json:"logs"`
	NextCursor string       `json:"nextCursor"`
	HasMore    bool         `json:"hasMore"`
}

type RequestLogFilter struct {
	Limit             int
	Cursor            string
	Since             time.Time
	RequestID         string
	Query             string
	StatusClass       string
	StatusCode        int
	ProviderAccountID int64
	RoutingPoolID     int64
	ClientKeyID       int64
	Model             string
	SessionID         string
	Error             string
	UsageSource       string
	RoutingPoolError  string
	RoutingPoolChain  string
	GatewayFallbacks  bool
}

type UsageSummary struct {
	Range                  string            `json:"range"`
	GroupBy                string            `json:"groupBy"`
	TotalRequests          int64             `json:"totalRequests"`
	TotalInputTokens       int64             `json:"totalInputTokens"`
	TotalOutputTokens      int64             `json:"totalOutputTokens"`
	TotalTokens            int64             `json:"totalTokens"`
	TotalCachedInputTokens int64             `json:"totalCachedInputTokens"`
	TotalReasoningTokens   int64             `json:"totalReasoningTokens"`
	EstimatedCostMicrousd  int64             `json:"estimatedCostMicrousd"`
	Rows                   []UsageSummaryRow `json:"rows"`
}

type UsageSummaryRow struct {
	ID                    string `json:"id"`
	Label                 string `json:"label"`
	Requests              int64  `json:"requests"`
	InputTokens           int64  `json:"inputTokens"`
	OutputTokens          int64  `json:"outputTokens"`
	TotalTokens           int64  `json:"totalTokens"`
	CachedInputTokens     int64  `json:"cachedInputTokens"`
	ReasoningTokens       int64  `json:"reasoningTokens"`
	EstimatedCostMicrousd int64  `json:"estimatedCostMicrousd"`
}

type UsagePricing struct {
	Version       int                   `json:"version"`
	Currency      string                `json:"currency"`
	Unit          string                `json:"unit"`
	UpdatedAt     time.Time             `json:"updatedAt"`
	Models        map[string]UsagePrice `json:"models"`
	IgnoredModels []string              `json:"ignoredModels,omitempty"`
}

type UsagePrice struct {
	InputMicrousdPerMillion           int64 `json:"inputMicrousdPerMillion"`
	CachedInputMicrousdPerMillion     int64 `json:"cachedInputMicrousdPerMillion"`
	OutputMicrousdPerMillion          int64 `json:"outputMicrousdPerMillion"`
	LongInputMicrousdPerMillion       int64 `json:"longInputMicrousdPerMillion"`
	LongCachedInputMicrousdPerMillion int64 `json:"longCachedInputMicrousdPerMillion"`
	LongOutputMicrousdPerMillion      int64 `json:"longOutputMicrousdPerMillion"`
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
	DefaultModel string `json:"defaultModel"`
}

type GatewaySettings struct {
	MaxConcurrentGatewayRequests           int  `json:"maxConcurrentGatewayRequests"`
	MaxConcurrentRequestsPerAccount        int  `json:"maxConcurrentRequestsPerAccount"`
	MaxConcurrentRequestsPerKey            int  `json:"maxConcurrentRequestsPerKey"`
	RequestsPerMinutePerKey                int  `json:"requestsPerMinutePerKey"`
	TokensPerMinutePerKey                  int  `json:"tokensPerMinutePerKey"`
	ProviderAccountAutoTestEnabled         bool `json:"providerAccountAutoTestEnabled"`
	ProviderAccountAutoTestIntervalSeconds int  `json:"providerAccountAutoTestIntervalSeconds"`
	RequestLogRetentionDays                int  `json:"requestLogRetentionDays"`
}

type RequestLogCleanupResult struct {
	RetentionDays int       `json:"retentionDays"`
	Deleted       int64     `json:"deleted"`
	Batches       int       `json:"batches"`
	Before        time.Time `json:"before"`
}

type RequestLogRetentionStats struct {
	Cutoff             time.Time  `json:"cutoff"`
	OldestLogAt        *time.Time `json:"oldestLogAt,omitempty"`
	NewestLogAt        *time.Time `json:"newestLogAt,omitempty"`
	TotalCountEstimate int64      `json:"totalCountEstimate"`
	EligibleCount      int64      `json:"eligibleCount"`
	ObservedAt         time.Time  `json:"observedAt"`
}

type RequestLogRetentionLease interface {
	DeleteBeforeBatch(ctx context.Context, before time.Time, batchSize int) (int64, error)
	Close() error
}

type ModelRoutingStatus struct {
	DefaultModel string              `json:"defaultModel"`
	Models       []ModelRoutingModel `json:"models"`
	Warnings     []string            `json:"warnings"`
}

type ModelRoutingModel struct {
	Model           string                `json:"model"`
	ConfiguredCount int                   `json:"configuredCount"`
	EnabledCount    int                   `json:"enabledCount"`
	Accounts        []ModelRoutingAccount `json:"accounts,omitempty"`
}

type ModelRoutingAccount struct {
	ID                  int64      `json:"id"`
	DisplayName         string     `json:"displayName"`
	AccountType         string     `json:"accountType"`
	RoutingPoolIDs      []int64    `json:"routingPoolIds,omitempty"`
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
	UpdateAdminPassword(ctx context.Context, id int64, passwordHash string) error
	CreateSession(ctx context.Context, adminID int64, tokenHash string, metadata SessionMetadata, createdAt, expiresAt time.Time) error
	FindAdminBySessionHash(ctx context.Context, tokenHash string, now time.Time) (Admin, error)
	RevokeSession(ctx context.Context, tokenHash string, revokedAt time.Time) error
	ListAdminSessions(ctx context.Context, adminID int64, currentHash string, now time.Time) ([]AdminSession, error)
	RevokeAdminSession(ctx context.Context, adminID, sessionID int64, revokedAt time.Time) (AdminSession, error)
	RevokeOtherAdminSessions(ctx context.Context, adminID int64, currentHash string, revokedAt time.Time) (int64, error)
	CreateAPIKey(ctx context.Context, name, hash, prefix, encryptedSecret string, routingPoolID *int64) (APIKey, error)
	ListAPIKeys(ctx context.Context) ([]APIKey, error)
	PurgeRevokedAPIKeys(ctx context.Context, cutoff time.Time) (int64, error)
	RevokeAPIKey(ctx context.Context, id int64) (APIKey, error)
	DeleteRevokedAPIKey(ctx context.Context, id int64) error
	GetAPIKeyEncryptedSecret(ctx context.Context, id int64) (string, error)
	FindAPIKeyByHash(ctx context.Context, hash string, now time.Time) (APIKey, error)
	UpdateAPIKeyName(ctx context.Context, id int64, name string) (APIKey, error)
	SetAPIKeyDisabled(ctx context.Context, id int64, disabled bool) (APIKey, error)
	UpdateAPIKeyModelPolicy(ctx context.Context, id int64, policy string, models []string) (APIKey, error)
	UpdateAPIKeyLimits(ctx context.Context, id int64, requestsPerMinute, tokensPerMinute int) (APIKey, error)
	UpdateAPIKeyBudgets(ctx context.Context, id int64, requestBudget24h, tokenBudget24h int, costBudgetMicrousd24h int64, requestBudget30d, tokenBudget30d int, costBudgetMicrousd30d int64) (APIKey, error)
	ListRoutingPools(ctx context.Context) ([]RoutingPool, error)
	CreateRoutingPool(ctx context.Context, name, description string, enabled bool, fallbackPoolID *int64) (RoutingPool, error)
	UpdateRoutingPool(ctx context.Context, id int64, name, description string, enabled bool, fallbackPoolID *int64) (RoutingPool, error)
	DeleteRoutingPool(ctx context.Context, id int64) error
	ReplaceRoutingPoolAccounts(ctx context.Context, id int64, accounts []RoutingPoolAccount) (RoutingPool, error)
	UpdateAPIKeyRoutingPool(ctx context.Context, id int64, routingPoolID *int64) (APIKey, error)
	GetAPIKeyBudgetUsage(ctx context.Context, keyID int64, now time.Time) (APIKeyBudgetUsage, error)
	ListAPIKeyModels(ctx context.Context, id int64) ([]string, error)
	TouchAPIKey(ctx context.Context, id int64, usedAt time.Time) error
	ListRequestLogs(ctx context.Context, filter RequestLogFilter) (RequestLogPage, error)
	TryAcquireRequestLogRetention(ctx context.Context) (RequestLogRetentionLease, bool, error)
	GetRequestLogRetentionStats(ctx context.Context, before time.Time) (RequestLogRetentionStats, error)
	GetUsageSummary(ctx context.Context, since time.Time, groupBy string) (UsageSummary, error)
	GetUsagePricing(ctx context.Context) (UsagePricing, error)
	SaveUsagePricing(ctx context.Context, pricing UsagePricing) (UsagePricing, error)
	GetModelSettings(ctx context.Context) (ModelSettings, error)
	SaveModelSettings(ctx context.Context, settings ModelSettings) (ModelSettings, error)
	GetGatewaySettings(ctx context.Context) (GatewaySettings, error)
	SaveGatewaySettings(ctx context.Context, settings GatewaySettings) (GatewaySettings, error)
	GetOpsErrorStats(ctx context.Context, since time.Time) (OpsErrorStats, error)
	GetOpsThroughputTrend(ctx context.Context, since time.Time, interval string) (OpsThroughputTrend, error)
	GetOpsErrorTrend(ctx context.Context, since time.Time, interval string) (OpsErrorTrend, error)
	GetOpsLatencyDistribution(ctx context.Context, since time.Time) (OpsLatencyDistribution, error)
	GetOpsAccountHealth(ctx context.Context, since time.Time) (OpsAccountHealth, error)
	ListOpsAccountTests(ctx context.Context, since time.Time, limit int) ([]OpsAccountTest, error)
	GetOpsCostBreakdown(ctx context.Context, since time.Time) (OpsCostBreakdown, error)
	ListFingerprintProfiles(ctx context.Context) ([]FingerprintProfile, error)
	CreateFingerprintProfile(ctx context.Context, input FingerprintProfileInput) (FingerprintProfile, error)
	UpdateFingerprintProfile(ctx context.Context, id int64, input FingerprintProfileInput) (FingerprintProfile, error)
	DeleteFingerprintProfile(ctx context.Context, id int64) error
	ListErrorPassthroughRules(ctx context.Context) ([]ErrorPassthroughRule, error)
	CreateErrorPassthroughRule(ctx context.Context, input ErrorPassthroughRuleInput) (ErrorPassthroughRule, error)
	UpdateErrorPassthroughRule(ctx context.Context, id int64, input ErrorPassthroughRuleInput) (ErrorPassthroughRule, error)
	DeleteErrorPassthroughRule(ctx context.Context, id int64) error
}

type Service struct {
	repo                    Repository
	sessionTTL              time.Duration
	encryptionSecret        string
	defaultGatewaySettings  GatewaySettings
	officialDocumentFetcher OfficialDocumentFetcher
	now                     func() time.Time
	systemEvents            SystemEventRepository
}

func NewService(repo Repository, cfg Config) *Service {
	sessionTTL := cfg.SessionTTL
	if sessionTTL <= 0 {
		sessionTTL = defaultSessionTTL
	}

	return &Service{
		repo:                    repo,
		sessionTTL:              sessionTTL,
		encryptionSecret:        cfg.EncryptionSecret,
		defaultGatewaySettings:  cfg.DefaultGatewaySettings,
		officialDocumentFetcher: NewHTTPOfficialDocumentFetcher(30 * time.Second),
		now:                     time.Now,
		systemEvents:            cfg.SystemEvents,
	}
}

func (s *Service) ListSystemEvents(ctx context.Context, filter SystemEventFilter) (SystemEventPage, error) {
	filter.Cursor = strings.TrimSpace(filter.Cursor)
	filter.Actor = strings.TrimSpace(filter.Actor)
	filter.TargetType = strings.TrimSpace(filter.TargetType)
	filter.TargetID = strings.TrimSpace(filter.TargetID)
	filter.Query = strings.TrimSpace(filter.Query)
	if filter.Limit == 0 {
		filter.Limit = 50
	}
	if filter.Limit < 1 || filter.Limit > 100 || len(filter.Cursor) > 1024 || len(filter.Actor) > 128 ||
		len(filter.TargetType) > 128 || len(filter.TargetID) > 128 || len(filter.Query) > maxRequestLogQueryLen ||
		(!filter.Since.IsZero() && filter.Since.After(s.now().Add(time.Minute))) ||
		(filter.Category != "" && !systemevent.IsValidCategory(filter.Category)) ||
		(filter.Outcome != "" && !systemevent.IsValidOutcome(filter.Outcome)) ||
		(filter.Severity != "" && !systemevent.IsValidSeverity(filter.Severity)) ||
		(filter.Action != "" && !systemevent.IsKnownAction(filter.Action)) {
		return SystemEventPage{}, ErrInvalidInput
	}
	if filter.TargetID != "" && filter.TargetType == "" {
		return SystemEventPage{}, ErrInvalidInput
	}
	if s.systemEvents == nil {
		return SystemEventPage{}, errors.New("system event repository is not configured")
	}
	page, err := s.systemEvents.List(ctx, filter)
	if errors.Is(err, systemevent.ErrInvalidEvent) || errors.Is(err, systemevent.ErrInvalidCursor) {
		return SystemEventPage{}, ErrInvalidInput
	}
	return page, err
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
		ctx = withSecurityIntent(ctx, systemevent.ActionAuthBootstrapUsernameUpdated, "admin", auditID(existing.ID), username)
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

	ctx = withSecurityIntent(ctx, systemevent.ActionAuthBootstrapCreated, "admin", "", username)
	_, err = s.repo.CreateAdmin(ctx, username, hash)
	return err
}

func (s *Service) Login(ctx context.Context, username, password string, metadata SessionMetadata) (Session, error) {
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	if username == "" || password == "" {
		secret.VerifyPassword(dummyAdminPasswordHash, password)
		return Session{}, ErrUnauthorized
	}

	admin, err := s.repo.FindAdminByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			secret.VerifyPassword(dummyAdminPasswordHash, password)
			return Session{}, ErrUnauthorized
		}
		return Session{}, err
	}
	if !secret.VerifyPassword(admin.PasswordHash, password) {
		return Session{}, ErrUnauthorized
	}
	ctx = withAuthenticatedActor(ctx, admin)

	token, err := secret.GenerateToken(sessionTokenName)
	if err != nil {
		return Session{}, fmt.Errorf("generate admin session token: %w", err)
	}
	now := s.now().UTC()
	expiresAt := now.Add(s.sessionTTL)
	metadata = normalizeSessionMetadata(metadata)
	ctx = withSecurityIntent(ctx, systemevent.ActionAuthLoginSucceeded, "admin", auditID(admin.ID), admin.Username)
	if err := s.repo.CreateSession(ctx, admin.ID, secret.HashAPIKey(token), metadata, now, expiresAt); err != nil {
		return Session{}, err
	}

	return Session{Token: token, AdminID: admin.ID, ExpiresAt: expiresAt}, nil
}

func (s *Service) ValidateSession(ctx context.Context, token string) (Admin, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Admin{}, ErrUnauthorized
	}

	admin, err := s.repo.FindAdminBySessionHash(ctx, secret.HashAPIKey(token), s.now().UTC())
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Admin{}, ErrUnauthorized
		}
		return Admin{}, err
	}
	return admin, nil
}

// ChangePassword updates the password for an already-authenticated admin.
func (s *Service) ChangePassword(ctx context.Context, adminID int64, currentPassword, newPassword string) error {
	currentPassword = strings.TrimSpace(currentPassword)
	newPassword = strings.TrimSpace(newPassword)
	if currentPassword == "" || newPassword == "" {
		return ErrInvalidInput
	}
	if len(newPassword) < 8 {
		return ErrInvalidInput
	}
	adminRecord, err := s.repo.FindBootstrapAdmin(ctx)
	if err != nil {
		return err
	}
	if adminRecord.ID != adminID {
		return ErrUnauthorized
	}
	if !secret.VerifyPassword(adminRecord.PasswordHash, currentPassword) {
		return ErrUnauthorized
	}
	newHash, hashErr := secret.HashPassword(newPassword)
	if hashErr != nil {
		return fmt.Errorf("hash new password: %w", hashErr)
	}
	ctx = withSecurityIntent(ctx, systemevent.ActionAuthPasswordChanged, "admin", auditID(adminID), adminRecord.Username)
	return s.repo.UpdateAdminPassword(ctx, adminID, newHash)
}

func (s *Service) Logout(ctx context.Context, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	now := s.now().UTC()
	current, err := s.repo.FindAdminBySessionHash(ctx, secret.HashAPIKey(token), now)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	ctx = withAuthenticatedActor(ctx, current)
	ctx = withSecurityIntent(ctx, systemevent.ActionAuthLogoutSucceeded, "admin_session", "", "")
	if err := s.repo.RevokeSession(ctx, secret.HashAPIKey(token), now); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	return nil
}

func (s *Service) ListSessions(ctx context.Context, adminID int64, currentToken string) ([]AdminSession, error) {
	currentToken = strings.TrimSpace(currentToken)
	if adminID <= 0 || currentToken == "" {
		return nil, ErrUnauthorized
	}
	return s.repo.ListAdminSessions(ctx, adminID, secret.HashAPIKey(currentToken), s.now().UTC())
}

func (s *Service) RevokeSessionByID(ctx context.Context, adminID, sessionID int64, currentToken string) (bool, error) {
	currentToken = strings.TrimSpace(currentToken)
	if adminID <= 0 || sessionID <= 0 || currentToken == "" {
		return false, ErrInvalidInput
	}
	currentHash := secret.HashAPIKey(currentToken)
	ctx = withSecurityIntent(ctx, systemevent.ActionAuthSessionRevoked, "admin_session", auditID(sessionID), "")
	revoked, err := s.repo.RevokeAdminSession(ctx, adminID, sessionID, s.now().UTC())
	if err != nil {
		return false, err
	}
	return revoked.TokenHash == currentHash, nil
}

func (s *Service) RevokeOtherSessions(ctx context.Context, adminID int64, currentToken string) (int64, error) {
	currentToken = strings.TrimSpace(currentToken)
	if adminID <= 0 || currentToken == "" {
		return 0, ErrInvalidInput
	}
	ctx = withSecurityIntent(ctx, systemevent.ActionAuthSessionsRevokedOthers, "admin_session", "", "")
	return s.repo.RevokeOtherAdminSessions(ctx, adminID, secret.HashAPIKey(currentToken), s.now().UTC())
}

func normalizeSessionMetadata(metadata SessionMetadata) SessionMetadata {
	metadata.CreatedIP = summarizeSessionIP(metadata.CreatedIP)
	metadata.UserAgent = normalizeSessionUserAgent(metadata.UserAgent)
	return metadata
}

func summarizeSessionIP(value string) string {
	address, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	address = address.Unmap()
	bits := 64
	if address.Is4() {
		bits = 24
	}
	summary := netip.PrefixFrom(address, bits).Masked().String()
	if len(summary) > maxSessionCreatedIPBytes {
		return ""
	}
	return summary
}

func normalizeSessionUserAgent(value string) string {
	value = strings.ToValidUTF8(value, "")
	var normalized strings.Builder
	normalized.Grow(min(len(value), maxSessionUserAgentBytes))
	spacePending := false
	for _, char := range value {
		if unicode.IsControl(char) || unicode.IsSpace(char) {
			spacePending = normalized.Len() > 0
			continue
		}
		if spacePending {
			normalized.WriteByte(' ')
			spacePending = false
		}
		normalized.WriteRune(char)
	}
	return strings.TrimSpace(truncateUTF8Bytes(normalized.String(), maxSessionUserAgentBytes))
}

func truncateUTF8Bytes(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	end := limit
	for end > 0 && !utf8.ValidString(value[:end]) {
		end--
	}
	return value[:end]
}

func (s *Service) CreateAPIKey(ctx context.Context, name string, routingPoolID *int64) (CreatedAPIKey, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return CreatedAPIKey{}, ErrInvalidInput
	}
	if routingPoolID != nil && *routingPoolID < 0 {
		return CreatedAPIKey{}, ErrInvalidInput
	}
	if routingPoolID != nil && *routingPoolID == 0 {
		routingPoolID = nil
	}
	if routingPoolID != nil {
		pools, err := s.repo.ListRoutingPools(ctx)
		if err != nil {
			return CreatedAPIKey{}, err
		}
		found := false
		for _, pool := range pools {
			if pool.ID == *routingPoolID {
				found = true
				break
			}
		}
		if !found {
			return CreatedAPIKey{}, ErrNotFound
		}
	}

	token, err := secret.GenerateToken(apiKeyTokenName)
	if err != nil {
		return CreatedAPIKey{}, fmt.Errorf("generate api key: %w", err)
	}
	encryptedSecret := ""
	if s.encryptionSecret != "" {
		encryptedSecret, err = secret.EncryptString(s.encryptionSecret, token)
		if err != nil {
			return CreatedAPIKey{}, fmt.Errorf("encrypt api key: %w", err)
		}
	}
	ctx = withAuditIntent(ctx, systemevent.ActionAPIKeyCreated, "client_api_key", "", name, nil)
	key, err := s.repo.CreateAPIKey(ctx, name, secret.HashAPIKey(token), secret.TokenPrefix(token), encryptedSecret, routingPoolID)
	if err != nil {
		return CreatedAPIKey{}, err
	}

	return CreatedAPIKey{Key: key, Secret: token}, nil
}

func (s *Service) GetAPIKeySecret(ctx context.Context, id int64) (string, error) {
	encryptedSecret, err := s.repo.GetAPIKeyEncryptedSecret(ctx, id)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(encryptedSecret) == "" {
		return "", ErrNotFound
	}
	value, err := secret.DecryptString(s.encryptionSecret, encryptedSecret)
	if err != nil {
		return "", fmt.Errorf("decrypt api key: %w", err)
	}
	return value, nil
}

func (s *Service) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	return s.repo.ListAPIKeys(ctx)
}

func (s *Service) PurgeExpiredAPIKeys(ctx context.Context) (int64, error) {
	cutoff := s.now().Add(-APIKeyPhysicalDeleteRetention)
	ctx = withSchedulerIntent(ctx, systemevent.ActionSchedulerAPIKeyPurgeCompleted, "client_api_key_collection", map[string]any{"cutoff": cutoff.UTC().Format(time.RFC3339)})
	return s.repo.PurgeRevokedAPIKeys(ctx, cutoff)
}

func (s *Service) RevokeAPIKey(ctx context.Context, id int64) (APIKey, error) {
	ctx = withAuditIntent(ctx, systemevent.ActionAPIKeyRevoked, "client_api_key", auditID(id), "", nil)
	return s.repo.RevokeAPIKey(ctx, id)
}

func (s *Service) DeleteRevokedAPIKey(ctx context.Context, id int64) error {
	ctx = withAuditIntent(ctx, systemevent.ActionAPIKeyDeleted, "client_api_key", auditID(id), "", nil)
	return s.repo.DeleteRevokedAPIKey(ctx, id)
}

func (s *Service) UpdateAPIKeyName(ctx context.Context, id int64, name string) (APIKey, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return APIKey{}, ErrInvalidInput
	}
	ctx = withAuditIntent(ctx, systemevent.ActionAPIKeyRenamed, "client_api_key", auditID(id), name, changedFields("name"))
	return s.repo.UpdateAPIKeyName(ctx, id, name)
}

func (s *Service) SetAPIKeyDisabled(ctx context.Context, id int64, disabled bool) (APIKey, error) {
	action := systemevent.ActionAPIKeyEnabled
	if disabled {
		action = systemevent.ActionAPIKeyDisabled
	}
	ctx = withAuditIntent(ctx, action, "client_api_key", auditID(id), "", changedFields("disabled"))
	return s.repo.SetAPIKeyDisabled(ctx, id, disabled)
}

func (s *Service) UpdateAPIKeyModelPolicy(ctx context.Context, id int64, policy string, models []string) (APIKey, error) {
	policy = strings.TrimSpace(policy)
	switch policy {
	case APIKeyModelPolicyAll:
		ctx = withAuditIntent(ctx, systemevent.ActionAPIKeyModelPolicyUpdated, "client_api_key", auditID(id), "", map[string]any{"changed_fields": []string{"model_policy"}, "model_count": 0})
		return s.repo.UpdateAPIKeyModelPolicy(ctx, id, policy, nil)
	case APIKeyModelPolicySelected:
		normalized, err := normalizeModelList(models)
		if err != nil {
			return APIKey{}, err
		}
		if len(normalized) == 0 {
			return APIKey{}, ErrInvalidInput
		}
		ctx = withAuditIntent(ctx, systemevent.ActionAPIKeyModelPolicyUpdated, "client_api_key", auditID(id), "", map[string]any{"changed_fields": []string{"model_policy"}, "model_count": len(normalized)})
		return s.repo.UpdateAPIKeyModelPolicy(ctx, id, policy, normalized)
	default:
		return APIKey{}, ErrInvalidInput
	}
}

func (s *Service) UpdateAPIKeyLimits(ctx context.Context, id int64, requestsPerMinute, tokensPerMinute int) (APIKey, error) {
	if requestsPerMinute < 0 || tokensPerMinute < 0 {
		return APIKey{}, ErrInvalidInput
	}
	ctx = withAuditIntent(ctx, systemevent.ActionAPIKeyLimitsUpdated, "client_api_key", auditID(id), "", changedFields("requests_per_minute", "tokens_per_minute"))
	return s.repo.UpdateAPIKeyLimits(ctx, id, requestsPerMinute, tokensPerMinute)
}

func (s *Service) UpdateAPIKeyBudgets(ctx context.Context, id int64, requestBudget24h, tokenBudget24h int, costBudgetMicrousd24h int64, requestBudget30d, tokenBudget30d int, costBudgetMicrousd30d int64) (APIKey, error) {
	if requestBudget24h < 0 || tokenBudget24h < 0 || costBudgetMicrousd24h < 0 || requestBudget30d < 0 || tokenBudget30d < 0 || costBudgetMicrousd30d < 0 {
		return APIKey{}, ErrInvalidInput
	}
	ctx = withAuditIntent(ctx, systemevent.ActionAPIKeyBudgetsUpdated, "client_api_key", auditID(id), "", changedFields("request_budget_24h", "token_budget_24h", "cost_budget_24h", "request_budget_30d", "token_budget_30d", "cost_budget_30d"))
	return s.repo.UpdateAPIKeyBudgets(ctx, id, requestBudget24h, tokenBudget24h, costBudgetMicrousd24h, requestBudget30d, tokenBudget30d, costBudgetMicrousd30d)
}

func (s *Service) ListRoutingPools(ctx context.Context) ([]RoutingPool, error) {
	return s.repo.ListRoutingPools(ctx)
}

func (s *Service) CreateRoutingPool(ctx context.Context, name, description string, enabled bool, fallbackPoolID *int64) (RoutingPool, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if name == "" {
		return RoutingPool{}, ErrInvalidInput
	}
	normalizedFallbackPoolID, err := normalizeRoutingPoolFallbackID(fallbackPoolID)
	if err != nil {
		return RoutingPool{}, err
	}
	ctx = withAuditIntent(ctx, systemevent.ActionRoutingPoolCreated, "routing_pool", "", name, nil)
	return s.repo.CreateRoutingPool(ctx, name, description, enabled, normalizedFallbackPoolID)
}

func (s *Service) UpdateRoutingPool(ctx context.Context, id int64, name, description string, enabled bool, fallbackPoolID *int64) (RoutingPool, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if id <= 0 || name == "" {
		return RoutingPool{}, ErrInvalidInput
	}
	normalizedFallbackPoolID, err := normalizeRoutingPoolFallbackID(fallbackPoolID)
	if err != nil {
		return RoutingPool{}, err
	}
	if normalizedFallbackPoolID != nil && *normalizedFallbackPoolID == id {
		return RoutingPool{}, ErrInvalidInput
	}
	ctx = withAuditIntent(ctx, systemevent.ActionRoutingPoolUpdated, "routing_pool", auditID(id), name, changedFields("name", "description", "enabled", "fallback_pool"))
	return s.repo.UpdateRoutingPool(ctx, id, name, description, enabled, normalizedFallbackPoolID)
}

func normalizeRoutingPoolFallbackID(id *int64) (*int64, error) {
	if id == nil || *id == 0 {
		return nil, nil
	}
	if *id < 0 {
		return nil, ErrInvalidInput
	}
	normalized := *id
	return &normalized, nil
}

func (s *Service) DeleteRoutingPool(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrInvalidInput
	}
	ctx = withAuditIntent(ctx, systemevent.ActionRoutingPoolDeleted, "routing_pool", auditID(id), "", nil)
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
	ctx = withAuditIntent(ctx, systemevent.ActionRoutingPoolAccountsReplaced, "routing_pool", auditID(id), "", map[string]any{"account_count": len(normalized)})
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
	ctx = withAuditIntent(ctx, systemevent.ActionAPIKeyRoutingPoolUpdated, "client_api_key", auditID(id), "", changedFields("routing_pool"))
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
	usage.CostRemainingMicrousd24h = remainingBudget64(key.CostBudgetMicrousd24h, usage.CostMicrousd24h)
	usage.RequestsRemaining30d = remainingBudget(key.RequestBudget30d, usage.RequestsUsed30d)
	usage.TokensRemaining30d = remainingBudget(key.TokenBudget30d, usage.TokensUsed30d)
	usage.CostRemainingMicrousd30d = remainingBudget64(key.CostBudgetMicrousd30d, usage.CostMicrousd30d)
	usage.RequestBudgetExceeded = budgetExceeded(key.RequestBudget24h, usage.RequestsUsed24h) ||
		budgetExceeded(key.RequestBudget30d, usage.RequestsUsed30d)
	usage.TokenBudgetExceeded = budgetExceeded(key.TokenBudget24h, usage.TokensUsed24h) ||
		budgetExceeded(key.TokenBudget30d, usage.TokensUsed30d)
	usage.CostBudgetExceeded = budgetExceeded64(key.CostBudgetMicrousd24h, usage.CostMicrousd24h) ||
		budgetExceeded64(key.CostBudgetMicrousd30d, usage.CostMicrousd30d)
}

func remainingBudget(limit int, used int64) *int64 {
	return remainingBudget64(int64(limit), used)
}

func remainingBudget64(limit int64, used int64) *int64 {
	if limit <= 0 {
		return nil
	}
	remaining := limit - used
	if remaining < 0 {
		remaining = 0
	}
	return &remaining
}

func budgetExceeded(limit int, used int64) bool {
	return budgetExceeded64(int64(limit), used)
}

func budgetExceeded64(limit int64, used int64) bool {
	return limit > 0 && used >= limit
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

func (s *Service) ListRequestLogs(ctx context.Context, filter RequestLogFilter) (RequestLogPage, error) {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 200 {
		filter.Limit = 200
	}
	filter.Cursor = strings.TrimSpace(filter.Cursor)
	filter.Query = strings.TrimSpace(filter.Query)
	if len(filter.Cursor) > 1024 || len(filter.Query) > maxRequestLogQueryLen {
		return RequestLogPage{}, ErrInvalidInput
	}
	filter.RequestID = strings.TrimSpace(filter.RequestID)
	filter.Model = strings.TrimSpace(filter.Model)
	filter.SessionID = strings.TrimSpace(filter.SessionID)
	filter.Error = strings.TrimSpace(filter.Error)
	filter.UsageSource = strings.TrimSpace(filter.UsageSource)
	filter.RoutingPoolError = strings.TrimSpace(filter.RoutingPoolError)
	filter.RoutingPoolChain = strings.TrimSpace(filter.RoutingPoolChain)
	if len(filter.RequestID) > 100 || len(filter.Model) > 100 || len(filter.SessionID) > 100 || len(filter.Error) > 100 || len(filter.UsageSource) > 100 || len(filter.RoutingPoolError) > 100 || len(filter.RoutingPoolChain) > 200 {
		return RequestLogPage{}, ErrInvalidInput
	}
	filter.StatusClass = strings.TrimSpace(filter.StatusClass)
	if filter.StatusClass == "" {
		filter.StatusClass = RequestLogStatusAll
	}
	switch filter.StatusClass {
	case RequestLogStatusAll, RequestLogStatusSuccess, RequestLogStatusClientError, RequestLogStatusServerError:
	default:
		return RequestLogPage{}, ErrInvalidInput
	}
	if filter.StatusCode != 0 && (filter.StatusCode < 100 || filter.StatusCode > 599) {
		return RequestLogPage{}, ErrInvalidInput
	}
	if filter.ProviderAccountID < 0 {
		return RequestLogPage{}, ErrInvalidInput
	}
	if filter.RoutingPoolID < 0 {
		return RequestLogPage{}, ErrInvalidInput
	}
	if filter.ClientKeyID < 0 {
		return RequestLogPage{}, ErrInvalidInput
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
	case "client_key", "provider_account", "routing_pool", "routing_pool_chain", "model", "session", "usage_source":
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
	s.TotalCachedInputTokens = 0
	s.TotalReasoningTokens = 0
	s.EstimatedCostMicrousd = 0
	for _, row := range s.Rows {
		s.TotalRequests += row.Requests
		s.TotalInputTokens += row.InputTokens
		s.TotalOutputTokens += row.OutputTokens
		s.TotalTokens += row.TotalTokens
		s.TotalCachedInputTokens += row.CachedInputTokens
		s.TotalReasoningTokens += row.ReasoningTokens
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
	current, err := s.GetUsagePricing(ctx)
	if err != nil {
		return UsagePricing{}, err
	}
	normalized.IgnoredModels = append([]string(nil), current.IgnoredModels...)
	for model := range normalized.Models {
		normalized.IgnoredModels = slices.DeleteFunc(normalized.IgnoredModels, func(ignored string) bool {
			return ignored == model
		})
	}
	normalized, err = normalizeUsagePricing(normalized)
	if err != nil {
		return UsagePricing{}, err
	}
	normalized.UpdatedAt = time.Now().UTC()
	ctx = withAuditIntent(ctx, systemevent.ActionUsagePricingUpdated, "usage_pricing", "default", "", map[string]any{"model_count": len(normalized.Models)})
	return s.repo.SaveUsagePricing(ctx, normalized)
}

func (s *Service) EstimateUsageCost(ctx context.Context, usage UsageCostInput) (UsageCostEstimate, error) {
	pricing, err := s.GetUsagePricing(ctx)
	if err != nil {
		return UsageCostEstimate{}, err
	}
	model := strings.TrimSpace(usage.Model)
	pricingModel, price, ok := usagePriceForModel(pricing.Models, model)
	snapshot := map[string]any{
		"matched":   ok,
		"model":     model,
		"currency":  pricing.Currency,
		"unit":      pricing.Unit,
		"version":   pricing.Version,
		"updatedAt": pricing.UpdatedAt,
	}
	if !ok {
		return UsageCostEstimate{Matched: false, Snapshot: snapshot}, nil
	}
	snapshot["pricingModel"] = pricingModel
	snapshot["inputMicrousdPerMillion"] = price.InputMicrousdPerMillion
	snapshot["cachedInputMicrousdPerMillion"] = price.CachedInputMicrousdPerMillion
	snapshot["outputMicrousdPerMillion"] = price.OutputMicrousdPerMillion
	if price.LongInputMicrousdPerMillion != 0 || price.LongCachedInputMicrousdPerMillion != 0 || price.LongOutputMicrousdPerMillion != 0 {
		snapshot["longInputMicrousdPerMillion"] = price.LongInputMicrousdPerMillion
		snapshot["longCachedInputMicrousdPerMillion"] = price.LongCachedInputMicrousdPerMillion
		snapshot["longOutputMicrousdPerMillion"] = price.LongOutputMicrousdPerMillion
	}
	return UsageCostEstimate{
		Matched:      true,
		CostMicrousd: estimateCostMicrousd(usage, price),
		Snapshot:     snapshot,
	}, nil
}

func usagePriceForModel(prices map[string]UsagePrice, model string) (string, UsagePrice, bool) {
	if price, ok := prices[model]; ok {
		return model, price, true
	}
	const dateLength = len("2006-01-02")
	if len(model) <= dateLength || model[len(model)-dateLength-1] != '-' {
		return "", UsagePrice{}, false
	}
	dateSuffix := model[len(model)-dateLength:]
	if _, err := time.Parse("2006-01-02", dateSuffix); err != nil {
		return "", UsagePrice{}, false
	}
	baseModel := model[:len(model)-dateLength-1]
	price, ok := prices[baseModel]
	if !ok {
		return "", UsagePrice{}, false
	}
	return baseModel, price, true
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
	ctx = withAuditIntent(ctx, systemevent.ActionModelSettingsUpdated, "model_settings", "default", "", changedFields("default_model"))
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
	ctx = withAuditIntent(ctx, systemevent.ActionGatewaySettingsUpdated, "gateway_settings", "default", "", changedFields("limits", "auto_test", "request_log_retention"))
	return s.repo.SaveGatewaySettings(ctx, normalized)
}

func (s *Service) CleanupRequestLogs(ctx context.Context, now time.Time) (RequestLogCleanupResult, error) {
	started := s.now()
	settings, err := s.GetGatewaySettings(ctx)
	if err != nil {
		return RequestLogCleanupResult{}, err
	}
	if settings.RequestLogRetentionDays <= 0 {
		return RequestLogCleanupResult{}, ErrInvalidInput
	}
	if now.IsZero() {
		now = s.now()
	}
	before := now.Add(-time.Duration(settings.RequestLogRetentionDays) * 24 * time.Hour)
	result, runErr := s.RunRequestLogRetention(ctx, settings.RequestLogRetentionDays, before, DefaultRequestLogRetentionBatchSize)
	s.recordRequestLogCleanupEvent(ctx, started, result, runErr)
	return result, runErr
}

func (s *Service) recordRequestLogCleanupEvent(ctx context.Context, started time.Time, result RequestLogCleanupResult, cleanupErr error) {
	if s.systemEvents == nil {
		return
	}
	outcome := systemevent.OutcomeSuccess
	severity := systemevent.SeverityInfo
	errorCode := ""
	if cleanupErr != nil {
		outcome = systemevent.OutcomeFailure
		severity = systemevent.SeverityError
		errorCode = "request_log_cleanup_failed"
		if result.Deleted > 0 {
			outcome = systemevent.OutcomePartial
			severity = systemevent.SeverityWarning
		}
	}
	metadata, _ := systemevent.SafeMetadata(map[string]any{
		"cutoff": result.Before.UTC().Format(time.RFC3339), "retention_days": result.RetentionDays,
		"deleted_count": result.Deleted, "batch_count": result.Batches,
	}, "cutoff", "retention_days", "deleted_count", "batch_count")
	intent := systemevent.EventIntent{
		Category: systemevent.CategoryAudit, Severity: severity, Action: systemevent.ActionRequestLogCleanupCompleted,
		Outcome: outcome, ErrorCode: errorCode, Target: systemevent.Target{Type: "request_log_collection"}, Metadata: metadata,
	}
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	now := s.now()
	event := systemevent.BuildEvent(recordCtx, intent, intent.Target, now.UTC(), now.Sub(started))
	_ = s.systemEvents.Insert(recordCtx, event)
}

func (s *Service) RunRequestLogRetention(ctx context.Context, retentionDays int, before time.Time, batchSize int) (result RequestLogCleanupResult, err error) {
	if retentionDays <= 0 || before.IsZero() || batchSize <= 0 {
		return RequestLogCleanupResult{}, ErrInvalidInput
	}
	result.RetentionDays = retentionDays
	result.Before = before.UTC()
	lease, acquired, err := s.repo.TryAcquireRequestLogRetention(ctx)
	if err != nil {
		return result, err
	}
	if !acquired {
		return result, ErrConflict
	}
	defer func() {
		if closeErr := lease.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		deleted, err := lease.DeleteBeforeBatch(ctx, result.Before, batchSize)
		if err != nil {
			return result, err
		}
		result.Deleted += deleted
		if deleted > 0 {
			result.Batches++
		}
		if deleted < int64(batchSize) {
			return result, nil
		}
	}
}

func (s *Service) GetRequestLogRetentionStats(ctx context.Context, now time.Time) (RequestLogRetentionStats, error) {
	settings, err := s.GetGatewaySettings(ctx)
	if err != nil {
		return RequestLogRetentionStats{}, err
	}
	if now.IsZero() {
		now = s.now()
	}
	cutoff := time.Unix(0, 0).UTC()
	if settings.RequestLogRetentionDays > 0 {
		cutoff = now.UTC().Add(-time.Duration(settings.RequestLogRetentionDays) * 24 * time.Hour)
	}
	stats, err := s.repo.GetRequestLogRetentionStats(ctx, cutoff)
	if err != nil {
		return RequestLogRetentionStats{}, err
	}
	if settings.RequestLogRetentionDays > 0 {
		stats.Cutoff = cutoff
	}
	stats.ObservedAt = now.UTC()
	return stats, nil
}

func (s *Service) DefaultModel(ctx context.Context) (string, error) {
	settings, err := s.GetModelSettings(ctx)
	if err != nil {
		return "", err
	}
	return settings.DefaultModel, nil
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
		if price.InputMicrousdPerMillion < 0 || price.CachedInputMicrousdPerMillion < 0 || price.OutputMicrousdPerMillion < 0 ||
			price.LongInputMicrousdPerMillion < 0 || price.LongCachedInputMicrousdPerMillion < 0 || price.LongOutputMicrousdPerMillion < 0 {
			return UsagePricing{}, ErrInvalidInput
		}
		models[model] = price
	}
	if len(models) == 0 {
		return UsagePricing{}, ErrInvalidInput
	}
	ignoredModels := make([]string, 0, len(pricing.IgnoredModels))
	seenIgnored := make(map[string]struct{}, len(pricing.IgnoredModels))
	for _, rawModel := range pricing.IgnoredModels {
		model := strings.TrimSpace(rawModel)
		if model == "" || len(model) > maxModelNameLen {
			return UsagePricing{}, ErrInvalidInput
		}
		if _, duplicate := seenIgnored[model]; duplicate {
			return UsagePricing{}, ErrInvalidInput
		}
		seenIgnored[model] = struct{}{}
		if _, restored := models[model]; !restored {
			ignoredModels = append(ignoredModels, model)
		}
	}
	sort.Strings(ignoredModels)

	return UsagePricing{
		Version:       version,
		Currency:      currency,
		Unit:          unit,
		UpdatedAt:     pricing.UpdatedAt,
		Models:        models,
		IgnoredModels: ignoredModels,
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
	return ModelSettings{DefaultModel: defaultModel}
}

func normalizeModelSettings(settings ModelSettings) (ModelSettings, error) {
	defaultName := strings.TrimSpace(settings.DefaultModel)
	if defaultName == "" {
		return ModelSettings{}, ErrInvalidInput
	}

	if len(defaultName) > maxModelNameLen {
		return ModelSettings{}, ErrInvalidInput
	}

	return ModelSettings{DefaultModel: defaultName}, nil
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
		settings.ProviderAccountAutoTestIntervalSeconds < 0 ||
		settings.RequestLogRetentionDays < 0 {
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

func (s *Service) GetOpsErrorStats(ctx context.Context, since time.Time) (OpsErrorStats, error) {
	if since.IsZero() {
		since = time.Now().Add(-7 * 24 * time.Hour)
	}
	return s.repo.GetOpsErrorStats(ctx, since)
}

func (s *Service) GetOpsThroughputTrend(ctx context.Context, since time.Time, interval string) (OpsThroughputTrend, error) {
	if since.IsZero() {
		since = time.Now().Add(-24 * time.Hour)
	}
	if interval == "" {
		interval = "hour"
	}
	if !validOpsInterval(interval) {
		return OpsThroughputTrend{}, ErrInvalidInput
	}
	return s.repo.GetOpsThroughputTrend(ctx, since, interval)
}

func (s *Service) GetOpsErrorTrend(ctx context.Context, since time.Time, interval string) (OpsErrorTrend, error) {
	if since.IsZero() {
		since = time.Now().Add(-24 * time.Hour)
	}
	if interval == "" {
		interval = "hour"
	}
	if !validOpsInterval(interval) {
		return OpsErrorTrend{}, ErrInvalidInput
	}
	return s.repo.GetOpsErrorTrend(ctx, since, interval)
}

func (s *Service) GetOpsLatencyDistribution(ctx context.Context, since time.Time) (OpsLatencyDistribution, error) {
	if since.IsZero() {
		since = time.Now().Add(-7 * 24 * time.Hour)
	}
	return s.repo.GetOpsLatencyDistribution(ctx, since)
}

func (s *Service) GetOpsAccountHealth(ctx context.Context, since time.Time) (OpsAccountHealth, error) {
	if since.IsZero() {
		since = time.Now().Add(-24 * time.Hour)
	}
	return s.repo.GetOpsAccountHealth(ctx, since)
}

func (s *Service) ListOpsAccountTests(ctx context.Context, since time.Time, limit int) ([]OpsAccountTest, error) {
	if since.IsZero() {
		since = time.Now().Add(-24 * time.Hour)
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return s.repo.ListOpsAccountTests(ctx, since, limit)
}

func (s *Service) GetOpsCostBreakdown(ctx context.Context, since time.Time) (OpsCostBreakdown, error) {
	if since.IsZero() {
		since = time.Now().Add(-24 * time.Hour)
	}
	return s.repo.GetOpsCostBreakdown(ctx, since)
}

func validOpsInterval(interval string) bool {
	switch interval {
	case "minute", "hour", "day":
		return true
	}
	return false
}

func (s *Service) ListFingerprintProfiles(ctx context.Context) ([]FingerprintProfile, error) {
	return s.repo.ListFingerprintProfiles(ctx)
}

func (s *Service) CreateFingerprintProfile(ctx context.Context, input FingerprintProfileInput) (FingerprintProfile, error) {
	if strings.TrimSpace(input.Name) == "" {
		return FingerprintProfile{}, ErrInvalidInput
	}
	if err := input.Normalize(); err != nil {
		return FingerprintProfile{}, err
	}
	ctx = withAuditIntent(ctx, systemevent.ActionFingerprintProfileCreated, "fingerprint_profile", "", input.Name, nil)
	return s.repo.CreateFingerprintProfile(ctx, input)
}

func (s *Service) UpdateFingerprintProfile(ctx context.Context, id int64, input FingerprintProfileInput) (FingerprintProfile, error) {
	if id <= 0 {
		return FingerprintProfile{}, ErrInvalidInput
	}
	if strings.TrimSpace(input.Name) == "" {
		return FingerprintProfile{}, ErrInvalidInput
	}
	if err := input.Normalize(); err != nil {
		return FingerprintProfile{}, err
	}
	ctx = withAuditIntent(ctx, systemevent.ActionFingerprintProfileUpdated, "fingerprint_profile", auditID(id), input.Name, changedFields("name", "description", "user_agent", "tls_fingerprint", "headers", "enabled"))
	return s.repo.UpdateFingerprintProfile(ctx, id, input)
}

func (s *Service) DeleteFingerprintProfile(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrInvalidInput
	}
	ctx = withAuditIntent(ctx, systemevent.ActionFingerprintProfileDeleted, "fingerprint_profile", auditID(id), "", nil)
	return s.repo.DeleteFingerprintProfile(ctx, id)
}

func (s *Service) ListErrorPassthroughRules(ctx context.Context) ([]ErrorPassthroughRule, error) {
	return s.repo.ListErrorPassthroughRules(ctx)
}

func (s *Service) CreateErrorPassthroughRule(ctx context.Context, input ErrorPassthroughRuleInput) (ErrorPassthroughRule, error) {
	if strings.TrimSpace(input.Pattern) == "" {
		return ErrorPassthroughRule{}, ErrInvalidInput
	}
	if !validErrorPassthroughMatchType(input.MatchType) {
		return ErrorPassthroughRule{}, ErrInvalidInput
	}
	ctx = withAuditIntent(ctx, systemevent.ActionErrorPassthroughRuleCreated, "error_passthrough_rule", "", "", nil)
	return s.repo.CreateErrorPassthroughRule(ctx, input)
}

func (s *Service) UpdateErrorPassthroughRule(ctx context.Context, id int64, input ErrorPassthroughRuleInput) (ErrorPassthroughRule, error) {
	if id <= 0 {
		return ErrorPassthroughRule{}, ErrInvalidInput
	}
	if strings.TrimSpace(input.Pattern) == "" {
		return ErrorPassthroughRule{}, ErrInvalidInput
	}
	if !validErrorPassthroughMatchType(input.MatchType) {
		return ErrorPassthroughRule{}, ErrInvalidInput
	}
	ctx = withAuditIntent(ctx, systemevent.ActionErrorPassthroughRuleUpdated, "error_passthrough_rule", auditID(id), "", changedFields("match_type", "pattern", "description", "enabled", "priority"))
	return s.repo.UpdateErrorPassthroughRule(ctx, id, input)
}

func (s *Service) DeleteErrorPassthroughRule(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrInvalidInput
	}
	ctx = withAuditIntent(ctx, systemevent.ActionErrorPassthroughRuleDeleted, "error_passthrough_rule", auditID(id), "", nil)
	return s.repo.DeleteErrorPassthroughRule(ctx, id)
}

func validErrorPassthroughMatchType(matchType string) bool {
	switch matchType {
	case "status_code", "error_message", "error_code":
		return true
	}
	return false
}
