package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/KnowSky404/N2API/backend/internal/alerting"
	"github.com/KnowSky404/N2API/backend/internal/buildinfo"
	"github.com/KnowSky404/N2API/backend/internal/config"
	"github.com/KnowSky404/N2API/backend/internal/gateway"
	"github.com/KnowSky404/N2API/backend/internal/provider"
)

var errHealth = errors.New("database unavailable")

type staticHealth struct {
	err error
}

func (h staticHealth) Ping(ctx context.Context) error {
	return h.err
}

type fakeAutoTestStatusSource struct {
	status provider.AutoTestStatus
}

type fakeRequestLogRetentionStatusSource struct {
	status admin.RequestLogRetentionStatus
}

type fakeResponseAffinityRetentionStatusSource struct {
	status gateway.ResponseAffinityRetentionStatus
}

type fakeAlertDeliveryStatusSource struct {
	status alerting.DeliveryStatus
}

func (s fakeRequestLogRetentionStatusSource) RequestLogRetentionStatus() admin.RequestLogRetentionStatus {
	return s.status
}

func (s fakeResponseAffinityRetentionStatusSource) ResponseAffinityRetentionStatus() gateway.ResponseAffinityRetentionStatus {
	return s.status
}

func (s fakeAlertDeliveryStatusSource) AlertDeliveryStatus() alerting.DeliveryStatus {
	return s.status
}

func (s fakeAutoTestStatusSource) ProviderAccountAutoTestStatus() provider.AutoTestStatus {
	return s.status
}

type fakeAdminService struct {
	loginMu              sync.Mutex
	loginCalls           int
	loginStarted         chan<- struct{}
	loginRelease         <-chan struct{}
	loginErr             error
	loginPanic           any
	loginMetadata        admin.SessionMetadata
	sessions             []admin.AdminSession
	sessionsErr          error
	revokedSessionID     int64
	revokeSessionToken   string
	revokeSessionCurrent bool
	revokeSessionErr     error
	revokeOthersToken    string
	revokeOthersCount    int64
	revokeOthersErr      error
	changePasswordErr    error
	keys                 []admin.APIKey
	deletedKeyID         int64
	deleteKeyErr         error
	logs                 []admin.RequestLog
	requestLogHasMore    bool
	requestLogNextCursor string
	requestLogFilter     admin.RequestLogFilter
	requestLogErr        error
	requestLogErrAfter   int
	requestLogExportWait bool
	requestLogStarted    chan struct{}
	requestLogCanceled   chan struct{}
	requestLogExportRows int
	systemEventPage      admin.SystemEventPage
	systemEventFilter    admin.SystemEventFilter
	systemEventErr       error
	configurationExport  admin.ConfigurationSnapshot
	configurationErr     error
	errorOnEmptyLogout   bool
	logoutTokens         []string
	modelSettings        admin.ModelSettings
	modelPolicyKeyID     int64
	modelPolicy          string
	modelPolicyModels    []string
	modelPolicyErr       error
	renameKeyID          int64
	renameName           string
	renameErr            error
	disabledKeyID        int64
	disabledValue        bool
	disabledErr          error
	limitKeyID           int64
	requestsPerMinute    int
	tokensPerMinute      int
	limitsErr            error
	budgetKeyID          int64
	requestBudget24h     int
	tokenBudget24h       int
	costBudget24h        int64
	requestBudget30d     int
	tokenBudget30d       int
	costBudget30d        int64
	budgetsErr           error
	routingPools         []admin.RoutingPool
	createFallbackID     *int64
	updateFallbackID     *int64
	routingPoolKeyID     int64
	routingPoolID        *int64
	budgetUsage          map[int64]admin.APIKeyBudgetUsage
	usageSummary         admin.UsageSummary
	usageRange           string
	usageGroupBy         string
	usagePricing         admin.UsagePricing
	gatewaySettings      admin.GatewaySettings
	gatewaySettingsErr   error
	retentionStats       admin.RequestLogRetentionStats
	retentionStatsErr    error
	cleanupResult        admin.RequestLogCleanupResult
	cleanupCalled        bool
	cleanupErr           error
	opsAccountHealth     admin.OpsAccountHealth
	opsAccountSince      time.Time
	opsAccountTests      []admin.OpsAccountTest
	opsAccountTestsSince time.Time
	opsAccountTestsLimit int
	opsCostBreakdown     admin.OpsCostBreakdown
	opsCostSince         time.Time
	fingerprintInput     admin.FingerprintProfileInput
	fingerprintID        int64
	fingerprintErr       error
	errorRuleInput       admin.ErrorPassthroughRuleInput
	errorRuleID          int64
	errorRuleErr         error

	syncOfficialPricing   admin.UsagePricing
	syncOfficialSummary   admin.UsagePricingSyncSummary
	syncOfficialErr       error
	removeShutdownModels  []string
	removeShutdownPricing admin.UsagePricing
	removeShutdownRemoved []string
	removeShutdownErr     error
	ignoreUpcomingModels  []string
	ignoreUpcomingPricing admin.UsagePricing
	ignoreUpcomingIgnored []string
	ignoreUpcomingErr     error
}

type fakeProviderService struct {
	status                 provider.Status
	connect                provider.ConnectResult
	connectOptions         provider.ConnectOptions
	createdAPIUpstream     provider.APIUpstreamInput
	accounts               []provider.Account
	accountModels          map[int64][]provider.AccountModel
	accountTestResults     []provider.AccountTestResult
	accountModelTestResult provider.AccountModelTestResult
	accountModelTestErr    error
	testedModelAccountID   int64
	testedModel            string
	selectionPreview       provider.SelectionPreview
	previewModel           string
	previewSessionID       string
	previewExcludedIDs     []int64
	previewRoutingPoolID   int64
	lastAccountUpdate      provider.AccountUpdate
	accountUpdateIDs       []int64
	accountUpdates         []provider.AccountUpdate
	replacedModelIDs       []int64
	replacedModels         [][]provider.AccountModelInput
	updateErr              error
	accountModelsErr       error
	accountTestResultsErr  error
	replaceModelsErr       error
	refreshErr             error
	resetStatusErr         error
	disconnectErr          error
	callbackErr            error
	callbackCode           string
	callbackState          string
	disconnected           bool
	refreshedAccountID     int64
	refreshedAccountIDs    []int64
	testedAccountID        int64
	testedAccountIDs       []int64
	testResultsAccountID   int64
	testResultsLimit       int
	testedAllAccounts      bool
	pausedAccountID        int64
	pausedAccountIDs       []int64
	pauseDuration          time.Duration
	resetStatusAccountID   int64
	resetStatusAccountIDs  []int64
	disconnectedAccountID  int64
	disconnectedAccountIDs []int64

	syncModelsResult  []provider.AccountModel
	syncModelsSummary provider.AccountModelSyncSummary
	syncModelsErr     error
}
type fakeGatewayHandler struct {
	called             bool
	accountConcurrency map[int64]int
	apiKeyConcurrency  map[int64]int
	apiKeyRequestRate  map[int64]int
	apiKeyTokenRate    map[int64]int
}

func (h *fakeGatewayHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.called = true
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
}

func (h *fakeGatewayHandler) AccountConcurrencySnapshot() map[int64]int {
	return h.accountConcurrency
}

func (h *fakeGatewayHandler) APIKeyConcurrencySnapshot() map[int64]int {
	return h.apiKeyConcurrency
}

func (h *fakeGatewayHandler) APIKeyRequestRateSnapshot() map[int64]int {
	return h.apiKeyRequestRate
}

func (h *fakeGatewayHandler) APIKeyTokenRateSnapshot() map[int64]int {
	return h.apiKeyTokenRate
}

func newFakeAdminService() *fakeAdminService {
	return &fakeAdminService{
		budgetUsage: map[int64]admin.APIKeyBudgetUsage{},
		routingPools: []admin.RoutingPool{
			{ID: 3, Name: "primary", Description: "daily", Enabled: true},
			{ID: 4, Name: "secondary", Description: "fallback", Enabled: true},
		},
		keys: []admin.APIKey{
			{ID: 7, Name: "codex laptop", Prefix: "n2api_abc", SecretAvailable: true, CreatedAt: time.Unix(1000, 0).UTC()},
		},
		logs: []admin.RequestLog{
			{ID: 3, RequestID: "req_3", ClientKey: "codex laptop (n2api_abc)", Provider: "openai", Route: "/v1/models", Method: http.MethodGet, StatusCode: 200, LatencyMS: 12, GatewayAttemptCount: 2, GatewayFallbackCount: 1, CreatedAt: time.Unix(4000, 0).UTC()},
		},
		modelSettings: admin.ModelSettings{DefaultModel: "gpt-4.1"},
	}
}

func (s *fakeAdminService) Login(_ context.Context, username, password string, metadata admin.SessionMetadata) (admin.Session, error) {
	s.loginMu.Lock()
	s.loginCalls++
	s.loginMetadata = metadata
	started := s.loginStarted
	release := s.loginRelease
	loginErr := s.loginErr
	loginPanic := s.loginPanic
	s.loginMu.Unlock()
	if started != nil {
		started <- struct{}{}
	}
	if release != nil {
		<-release
	}
	if loginErr != nil {
		return admin.Session{}, loginErr
	}
	if loginPanic != nil {
		panic(loginPanic)
	}
	if username != "admin" || password != "secret" {
		return admin.Session{}, admin.ErrUnauthorized
	}
	return admin.Session{Token: "valid-session", AdminID: 1, ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (s *fakeAdminService) ListSessions(_ context.Context, _ int64, _ string) ([]admin.AdminSession, error) {
	return append([]admin.AdminSession(nil), s.sessions...), s.sessionsErr
}

func (s *fakeAdminService) RevokeSessionByID(_ context.Context, _ int64, sessionID int64, currentToken string) (bool, error) {
	s.revokedSessionID = sessionID
	s.revokeSessionToken = currentToken
	return s.revokeSessionCurrent, s.revokeSessionErr
}

func (s *fakeAdminService) RevokeOtherSessions(_ context.Context, _ int64, currentToken string) (int64, error) {
	s.revokeOthersToken = currentToken
	return s.revokeOthersCount, s.revokeOthersErr
}

func (s *fakeAdminService) loginCallCount() int {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	return s.loginCalls
}

func (s *fakeAdminService) Logout(_ context.Context, token string) error {
	s.logoutTokens = append(s.logoutTokens, token)
	if s.errorOnEmptyLogout && token == "" {
		return errors.New("empty logout token")
	}
	return nil
}

func (s *fakeAdminService) ChangePassword(_ context.Context, _ int64, currentPassword, newPassword string) error {
	if s.changePasswordErr != nil {
		return s.changePasswordErr
	}
	if currentPassword == "" || newPassword == "" {
		return admin.ErrInvalidInput
	}
	return nil
}

func (s *fakeAdminService) ValidateSession(_ context.Context, token string) (admin.Admin, error) {
	if token != "valid-session" {
		return admin.Admin{}, admin.ErrUnauthorized
	}
	return admin.Admin{ID: 1, Username: "admin", PasswordHash: "secret-hash"}, nil
}

func (s *fakeAdminService) ListAPIKeys(_ context.Context) ([]admin.APIKey, error) {
	return s.keys, nil
}

func (s *fakeAdminService) ExportConfiguration(_ context.Context) (admin.ConfigurationSnapshot, error) {
	return s.configurationExport, s.configurationErr
}

func (s *fakeAdminService) CreateAPIKey(_ context.Context, name string, routingPoolID *int64) (admin.CreatedAPIKey, error) {
	if strings.TrimSpace(name) == "" {
		return admin.CreatedAPIKey{}, admin.ErrInvalidInput
	}
	key := admin.APIKey{ID: 9, Name: name, Prefix: "n2api_new", CreatedAt: time.Unix(2000, 0).UTC()}
	if routingPoolID != nil && *routingPoolID > 0 {
		var pool *admin.RoutingPool
		for i := range s.routingPools {
			if s.routingPools[i].ID == *routingPoolID {
				pool = &s.routingPools[i]
				break
			}
		}
		if pool == nil {
			return admin.CreatedAPIKey{}, admin.ErrNotFound
		}
		key.RoutingPoolID = routingPoolID
		key.RoutingPoolName = pool.Name
	}
	return admin.CreatedAPIKey{Key: key, Secret: "n2api_new_secret"}, nil
}

func (s *fakeAdminService) GetAPIKeySecret(_ context.Context, id int64) (string, error) {
	if id == 7 {
		return "n2api_abc_secret", nil
	}
	return "", admin.ErrNotFound
}

func (s *fakeAdminService) RevokeAPIKey(_ context.Context, id int64) (admin.APIKey, error) {
	for _, key := range s.keys {
		if key.ID == id {
			now := time.Unix(3000, 0).UTC()
			key.RevokedAt = &now
			return key, nil
		}
	}
	return admin.APIKey{}, admin.ErrNotFound
}

func (s *fakeAdminService) DeleteRevokedAPIKey(_ context.Context, id int64) error {
	s.deletedKeyID = id
	return s.deleteKeyErr
}

func (s *fakeAdminService) UpdateAPIKeyName(_ context.Context, id int64, name string) (admin.APIKey, error) {
	s.renameKeyID = id
	s.renameName = name
	if s.renameErr != nil {
		return admin.APIKey{}, s.renameErr
	}
	for i, key := range s.keys {
		if key.ID == id {
			key.Name = strings.TrimSpace(name)
			s.keys[i] = key
			return key, nil
		}
	}
	return admin.APIKey{}, admin.ErrNotFound
}

func (s *fakeAdminService) SetAPIKeyDisabled(_ context.Context, id int64, disabled bool) (admin.APIKey, error) {
	s.disabledKeyID = id
	s.disabledValue = disabled
	if s.disabledErr != nil {
		return admin.APIKey{}, s.disabledErr
	}
	for i, key := range s.keys {
		if key.ID == id {
			if disabled {
				now := time.Unix(3500, 0).UTC()
				key.DisabledAt = &now
			} else {
				key.DisabledAt = nil
			}
			s.keys[i] = key
			return key, nil
		}
	}
	return admin.APIKey{}, admin.ErrNotFound
}

func (s *fakeAdminService) UpdateAPIKeyModelPolicy(_ context.Context, id int64, policy string, models []string) (admin.APIKey, error) {
	s.modelPolicyKeyID = id
	s.modelPolicy = policy
	s.modelPolicyModels = append([]string(nil), models...)
	if s.modelPolicyErr != nil {
		return admin.APIKey{}, s.modelPolicyErr
	}
	for i, key := range s.keys {
		if key.ID == id {
			key.ModelPolicy = policy
			if policy == admin.APIKeyModelPolicyAll {
				key.AllowedModels = nil
			} else {
				key.AllowedModels = append([]string(nil), models...)
			}
			s.keys[i] = key
			return key, nil
		}
	}
	return admin.APIKey{}, admin.ErrNotFound
}

func (s *fakeAdminService) UpdateAPIKeyLimits(_ context.Context, id int64, requestsPerMinute, tokensPerMinute int) (admin.APIKey, error) {
	s.limitKeyID = id
	s.requestsPerMinute = requestsPerMinute
	s.tokensPerMinute = tokensPerMinute
	if s.limitsErr != nil {
		return admin.APIKey{}, s.limitsErr
	}
	for i, key := range s.keys {
		if key.ID == id {
			key.RequestsPerMinute = requestsPerMinute
			key.TokensPerMinute = tokensPerMinute
			s.keys[i] = key
			return key, nil
		}
	}
	return admin.APIKey{}, admin.ErrNotFound
}

func (s *fakeAdminService) UpdateAPIKeyBudgets(_ context.Context, id int64, requestBudget24h, tokenBudget24h int, costBudgetMicrousd24h int64, requestBudget30d, tokenBudget30d int, costBudgetMicrousd30d int64) (admin.APIKey, error) {
	s.budgetKeyID = id
	s.requestBudget24h = requestBudget24h
	s.tokenBudget24h = tokenBudget24h
	s.costBudget24h = costBudgetMicrousd24h
	s.requestBudget30d = requestBudget30d
	s.tokenBudget30d = tokenBudget30d
	s.costBudget30d = costBudgetMicrousd30d
	if s.budgetsErr != nil {
		return admin.APIKey{}, s.budgetsErr
	}
	for i, key := range s.keys {
		if key.ID == id {
			key.RequestBudget24h = requestBudget24h
			key.TokenBudget24h = tokenBudget24h
			key.CostBudgetMicrousd24h = costBudgetMicrousd24h
			key.RequestBudget30d = requestBudget30d
			key.TokenBudget30d = tokenBudget30d
			key.CostBudgetMicrousd30d = costBudgetMicrousd30d
			s.keys[i] = key
			return key, nil
		}
	}
	return admin.APIKey{}, admin.ErrNotFound
}

func (s *fakeAdminService) ListRoutingPools(_ context.Context) ([]admin.RoutingPool, error) {
	return append([]admin.RoutingPool(nil), s.routingPools...), nil
}

func (s *fakeAdminService) CreateRoutingPool(_ context.Context, name, description string, enabled bool, fallbackPoolID *int64) (admin.RoutingPool, error) {
	if strings.TrimSpace(name) == "" {
		return admin.RoutingPool{}, admin.ErrInvalidInput
	}
	s.createFallbackID = fallbackPoolID
	pool := admin.RoutingPool{ID: int64(len(s.routingPools) + 10), Name: strings.TrimSpace(name), Description: strings.TrimSpace(description), Enabled: enabled, FallbackPoolID: fallbackPoolID}
	s.routingPools = append(s.routingPools, pool)
	return pool, nil
}

func (s *fakeAdminService) UpdateRoutingPool(_ context.Context, id int64, name, description string, enabled bool, fallbackPoolID *int64) (admin.RoutingPool, error) {
	s.updateFallbackID = fallbackPoolID
	for i, pool := range s.routingPools {
		if pool.ID == id {
			pool.Name = strings.TrimSpace(name)
			pool.Description = strings.TrimSpace(description)
			pool.Enabled = enabled
			pool.FallbackPoolID = fallbackPoolID
			s.routingPools[i] = pool
			return pool, nil
		}
	}
	return admin.RoutingPool{}, admin.ErrNotFound
}

func (s *fakeAdminService) DeleteRoutingPool(_ context.Context, id int64) error {
	for i, pool := range s.routingPools {
		if pool.ID == id {
			s.routingPools = append(s.routingPools[:i], s.routingPools[i+1:]...)
			return nil
		}
	}
	return admin.ErrNotFound
}

func (s *fakeAdminService) ReplaceRoutingPoolAccounts(_ context.Context, id int64, accounts []admin.RoutingPoolAccount) (admin.RoutingPool, error) {
	for i, pool := range s.routingPools {
		if pool.ID == id {
			pool.Accounts = append([]admin.RoutingPoolAccount(nil), accounts...)
			pool.AccountIDs = make([]int64, 0, len(accounts))
			for _, account := range accounts {
				pool.AccountIDs = append(pool.AccountIDs, account.AccountID)
			}
			s.routingPools[i] = pool
			return pool, nil
		}
	}
	return admin.RoutingPool{}, admin.ErrNotFound
}

func (s *fakeAdminService) UpdateAPIKeyRoutingPool(_ context.Context, id int64, routingPoolID *int64) (admin.APIKey, error) {
	s.routingPoolKeyID = id
	s.routingPoolID = routingPoolID
	for i, key := range s.keys {
		if key.ID == id {
			key.RoutingPoolID = routingPoolID
			key.RoutingPoolName = ""
			if routingPoolID != nil {
				for _, pool := range s.routingPools {
					if pool.ID == *routingPoolID {
						key.RoutingPoolName = pool.Name
					}
				}
			}
			s.keys[i] = key
			return key, nil
		}
	}
	return admin.APIKey{}, admin.ErrNotFound
}

func (s *fakeAdminService) GetAPIKeyBudgetUsage(_ context.Context, key admin.APIKey, _ time.Time) (admin.APIKeyBudgetUsage, error) {
	usage := s.budgetUsage[key.ID]
	usage.KeyID = key.ID
	return usage, nil
}

func (s *fakeAdminService) ListRequestLogs(_ context.Context, filter admin.RequestLogFilter) (admin.RequestLogPage, error) {
	s.requestLogFilter = filter
	if s.requestLogErr != nil {
		return admin.RequestLogPage{}, s.requestLogErr
	}
	if filter.StatusClass == "bad" {
		return admin.RequestLogPage{}, admin.ErrInvalidInput
	}
	limit := filter.Limit
	if limit > len(s.logs) {
		limit = len(s.logs)
	}
	return admin.RequestLogPage{
		Logs:       s.logs[:limit],
		HasMore:    s.requestLogHasMore,
		NextCursor: s.requestLogNextCursor,
	}, nil
}

func (s *fakeAdminService) StreamRequestLogs(ctx context.Context, filter admin.RequestLogFilter, maxRows int, visit func(admin.RequestLog) error) (admin.RequestLogExportResult, error) {
	s.requestLogFilter = filter
	s.requestLogExportRows = maxRows
	if s.requestLogExportWait {
		if s.requestLogStarted != nil {
			close(s.requestLogStarted)
		}
		<-ctx.Done()
		if s.requestLogCanceled != nil {
			close(s.requestLogCanceled)
		}
		return admin.RequestLogExportResult{}, ctx.Err()
	}
	if s.requestLogErr != nil && s.requestLogErrAfter <= 0 {
		return admin.RequestLogExportResult{}, s.requestLogErr
	}
	result := admin.RequestLogExportResult{}
	for index, log := range s.logs {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if index == maxRows {
			result.LimitReached = true
			break
		}
		if err := visit(log); err != nil {
			return result, err
		}
		result.RowCount++
		if s.requestLogErr != nil && result.RowCount == s.requestLogErrAfter {
			return result, s.requestLogErr
		}
	}
	return result, nil
}

func (s *fakeAdminService) ListSystemEvents(_ context.Context, filter admin.SystemEventFilter) (admin.SystemEventPage, error) {
	s.systemEventFilter = filter
	return s.systemEventPage, s.systemEventErr
}

func (s *fakeAdminService) GetUsageSummary(_ context.Context, rangeName, groupBy string) (admin.UsageSummary, error) {
	s.usageRange = rangeName
	s.usageGroupBy = groupBy
	if rangeName == "bad" || groupBy == "bad" {
		return admin.UsageSummary{}, admin.ErrInvalidInput
	}
	return s.usageSummary, nil
}

func (s *fakeAdminService) GetUsagePricing(_ context.Context) (admin.UsagePricing, error) {
	return s.usagePricing, nil
}

func (s *fakeAdminService) UpdateUsagePricing(_ context.Context, pricing admin.UsagePricing) (admin.UsagePricing, error) {
	if strings.TrimSpace(pricing.Currency) != "USD" || strings.TrimSpace(pricing.Unit) != "1M_tokens" || len(pricing.Models) == 0 {
		return admin.UsagePricing{}, admin.ErrInvalidInput
	}
	s.usagePricing = pricing
	return pricing, nil
}

func (s *fakeAdminService) SyncOfficialUsagePricing(_ context.Context) (admin.UsagePricing, admin.UsagePricingSyncSummary, error) {
	return s.syncOfficialPricing, s.syncOfficialSummary, s.syncOfficialErr
}

func (s *fakeAdminService) RemoveShutdownUsagePricing(_ context.Context, models []string) (admin.UsagePricing, []string, error) {
	s.removeShutdownModels = append([]string(nil), models...)
	return s.removeShutdownPricing, s.removeShutdownRemoved, s.removeShutdownErr
}

func (s *fakeAdminService) IgnoreUpcomingUsagePricing(_ context.Context, models []string) (admin.UsagePricing, []string, error) {
	s.ignoreUpcomingModels = append([]string(nil), models...)
	return s.ignoreUpcomingPricing, s.ignoreUpcomingIgnored, s.ignoreUpcomingErr
}

func (s *fakeAdminService) GetModelSettings(_ context.Context) (admin.ModelSettings, error) {
	return s.modelSettings, nil
}

func (s *fakeAdminService) UpdateModelSettings(_ context.Context, settings admin.ModelSettings) (admin.ModelSettings, error) {
	defaultModel := strings.TrimSpace(settings.DefaultModel)
	if defaultModel == "" {
		return admin.ModelSettings{}, admin.ErrInvalidInput
	}
	s.modelSettings = admin.ModelSettings{DefaultModel: defaultModel}
	return s.modelSettings, nil
}

func (s *fakeAdminService) GetGatewaySettings(_ context.Context) (admin.GatewaySettings, error) {
	if s.gatewaySettingsErr != nil {
		return admin.GatewaySettings{}, s.gatewaySettingsErr
	}
	return s.gatewaySettings, nil
}

func (s *fakeAdminService) GetRequestLogRetentionStats(_ context.Context, _ time.Time) (admin.RequestLogRetentionStats, error) {
	return s.retentionStats, s.retentionStatsErr
}

func (s *fakeAdminService) UpdateGatewaySettings(_ context.Context, settings admin.GatewaySettings) (admin.GatewaySettings, error) {
	if settings.MaxConcurrentGatewayRequests < 0 ||
		settings.MaxConcurrentRequestsPerAccount < 0 ||
		settings.MaxConcurrentRequestsPerKey < 0 ||
		settings.RequestsPerMinutePerKey < 0 ||
		settings.TokensPerMinutePerKey < 0 ||
		settings.ProviderAccountAutoTestIntervalSeconds < 0 ||
		settings.RequestLogRetentionDays < 0 ||
		(settings.ProviderAccountAutoTestEnabled && settings.ProviderAccountAutoTestIntervalSeconds < 60) {
		return admin.GatewaySettings{}, admin.ErrInvalidInput
	}
	if settings.ProviderAccountAutoTestIntervalSeconds == 0 {
		settings.ProviderAccountAutoTestIntervalSeconds = 300
	}
	s.gatewaySettings = settings
	return s.gatewaySettings, nil
}

func (s *fakeAdminService) CleanupRequestLogs(_ context.Context, _ time.Time) (admin.RequestLogCleanupResult, error) {
	s.cleanupCalled = true
	if s.cleanupErr != nil {
		return admin.RequestLogCleanupResult{}, s.cleanupErr
	}
	return s.cleanupResult, nil
}

func (s *fakeAdminService) DefaultModel(ctx context.Context) (string, error) {
	settings, err := s.GetModelSettings(ctx)
	if err != nil {
		return "", err
	}
	return settings.DefaultModel, nil
}

func newFakeProviderService() *fakeProviderService {
	return &fakeProviderService{
		status: provider.Status{
			Provider:    "openai",
			Configured:  true,
			Connected:   true,
			DisplayName: "Codex Account",
		},
		connect:       provider.ConnectResult{AuthorizationURL: "https://auth.example.test/authorize?state=oauth_state"},
		accountModels: map[int64][]provider.AccountModel{},
	}
}

func (s *fakeProviderService) Status(_ context.Context) (provider.Status, error) {
	return s.status, nil
}

func (s *fakeProviderService) StartConnect(_ context.Context, options provider.ConnectOptions) (provider.ConnectResult, error) {
	if !s.status.Configured {
		return provider.ConnectResult{}, provider.ErrNotConfigured
	}
	s.connectOptions = options
	return s.connect, nil
}

func (s *fakeProviderService) ListAccounts(_ context.Context) ([]provider.Account, error) {
	return s.accounts, nil
}

func (s *fakeProviderService) CreateAPIUpstreamAccount(_ context.Context, input provider.APIUpstreamInput) (provider.Account, error) {
	s.createdAPIUpstream = input
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.BaseURL) == "" || strings.TrimSpace(input.APIKey) == "" {
		return provider.Account{}, provider.ErrInvalidInput
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	loadFactor := input.LoadFactor
	if loadFactor == 0 {
		loadFactor = 1
	}
	account := provider.Account{
		ID:                   int64(len(s.accounts) + 1),
		Provider:             "openai",
		AccountType:          provider.AccountTypeAPIUpstream,
		Name:                 strings.TrimSpace(input.Name),
		DisplayName:          strings.TrimSpace(input.Name),
		Enabled:              enabled,
		Priority:             input.Priority,
		LoadFactor:           loadFactor,
		Status:               provider.AccountStatusActive,
		FingerprintProfileID: input.FingerprintProfileID,
		Credential: provider.AccountCredential{
			CredentialType: provider.CredentialTypeAPIKey,
			BaseURL:        strings.TrimRight(strings.TrimSpace(input.BaseURL), "/"),
		},
	}
	s.accounts = append(s.accounts, account)
	if len(input.Models) > 0 {
		models := make([]provider.AccountModel, 0, len(input.Models))
		for i, model := range input.Models {
			models = append(models, provider.AccountModel{
				ID:        int64(i + 1),
				AccountID: account.ID,
				Provider:  "openai",
				Model:     strings.TrimSpace(model),
				Enabled:   true,
				Source:    provider.AccountModelSourceManual,
			})
		}
		s.accountModels[account.ID] = models
	}
	return account, nil
}

func (s *fakeProviderService) ListAccountModels(_ context.Context, accountID int64) ([]provider.AccountModel, error) {
	if s.accountModelsErr != nil {
		return nil, s.accountModelsErr
	}
	models, ok := s.accountModels[accountID]
	if !ok {
		return nil, provider.ErrNotConnected
	}
	return append([]provider.AccountModel(nil), models...), nil
}

func (s *fakeProviderService) ListAccountTestResults(_ context.Context, accountID int64, limit int) ([]provider.AccountTestResult, error) {
	s.testResultsAccountID = accountID
	s.testResultsLimit = limit
	if s.accountTestResultsErr != nil {
		return nil, s.accountTestResultsErr
	}
	results := make([]provider.AccountTestResult, 0, len(s.accountTestResults))
	for _, result := range s.accountTestResults {
		if result.AccountID == accountID {
			results = append(results, result)
		}
	}
	return results, nil
}

func (s *fakeProviderService) TestAccountModel(_ context.Context, accountID int64, model string) (provider.AccountModelTestResult, error) {
	s.testedModelAccountID = accountID
	s.testedModel = model
	if s.accountModelTestErr != nil {
		return provider.AccountModelTestResult{}, s.accountModelTestErr
	}
	result := s.accountModelTestResult
	if result.AccountID == 0 {
		result.AccountID = accountID
	}
	if result.Model == "" {
		result.Model = strings.TrimSpace(model)
	}
	return result, nil
}

func (s *fakeProviderService) ReplaceAccountModels(_ context.Context, accountID int64, models []provider.AccountModelInput) ([]provider.AccountModel, error) {
	if s.replaceModelsErr != nil {
		return nil, s.replaceModelsErr
	}
	if _, ok := s.accountModels[accountID]; !ok {
		return nil, provider.ErrNotConnected
	}
	s.replacedModelIDs = append(s.replacedModelIDs, accountID)
	s.replacedModels = append(s.replacedModels, append([]provider.AccountModelInput(nil), models...))
	saved := make([]provider.AccountModel, 0, len(models))
	for i, model := range models {
		saved = append(saved, provider.AccountModel{
			ID:        int64(i + 1),
			AccountID: accountID,
			Provider:  "openai",
			Model:     strings.TrimSpace(model.Model),
			Enabled:   model.Enabled,
			Source:    provider.AccountModelSourceManual,
		})
	}
	s.accountModels[accountID] = saved
	return append([]provider.AccountModel(nil), saved...), nil
}

func (s *fakeProviderService) PreviewAccountSelection(_ context.Context, model, sessionID string, excludedAccountIDs ...int64) (provider.SelectionPreview, error) {
	s.previewModel = model
	s.previewSessionID = sessionID
	s.previewExcludedIDs = append([]int64(nil), excludedAccountIDs...)
	if s.selectionPreview.Model == "" {
		return provider.SelectionPreview{}, provider.ErrModelUnavailable
	}
	return s.selectionPreview, nil
}

func (s *fakeProviderService) PreviewAccountSelectionInRoutingPool(_ context.Context, routingPoolID int64, model, sessionID string, excludedAccountIDs ...int64) (provider.SelectionPreview, error) {
	s.previewRoutingPoolID = routingPoolID
	s.previewModel = model
	s.previewSessionID = sessionID
	s.previewExcludedIDs = append([]int64(nil), excludedAccountIDs...)
	if s.selectionPreview.Model == "" {
		return provider.SelectionPreview{}, provider.ErrModelUnavailable
	}
	return s.selectionPreview, nil
}

func (s *fakeProviderService) CompleteCallback(_ context.Context, code, state string) (provider.Account, error) {
	s.callbackCode = code
	s.callbackState = state
	if s.callbackErr != nil {
		return provider.Account{}, s.callbackErr
	}
	return provider.Account{Provider: "openai", DisplayName: "Codex Account"}, nil
}

func (s *fakeProviderService) UpdateAccount(_ context.Context, id int64, update provider.AccountUpdate) (provider.Account, error) {
	s.lastAccountUpdate = update
	s.accountUpdateIDs = append(s.accountUpdateIDs, id)
	s.accountUpdates = append(s.accountUpdates, update)
	if s.updateErr != nil {
		return provider.Account{}, s.updateErr
	}
	if update.Enabled == nil && update.Priority == nil && update.LoadFactor == nil && update.MaxConcurrentRequests == nil && !update.ClearStatus && update.Name == nil && update.APIUpstreamBaseURL == nil && update.APIUpstreamAPIKey == nil && !update.FingerprintProfileIDSet {
		return provider.Account{}, provider.ErrInvalidInput
	}
	if update.Priority != nil && *update.Priority < 0 {
		return provider.Account{}, provider.ErrInvalidInput
	}
	if update.LoadFactor != nil && (*update.LoadFactor < 1 || *update.LoadFactor > 100) {
		return provider.Account{}, provider.ErrInvalidInput
	}
	if update.MaxConcurrentRequests != nil && *update.MaxConcurrentRequests < 0 {
		return provider.Account{}, provider.ErrInvalidInput
	}
	for i, account := range s.accounts {
		if account.ID == id {
			if update.Enabled != nil {
				account.Enabled = *update.Enabled
			}
			if update.Priority != nil {
				account.Priority = *update.Priority
			}
			if update.LoadFactor != nil {
				account.LoadFactor = *update.LoadFactor
			}
			if update.MaxConcurrentRequests != nil {
				account.MaxConcurrentRequests = *update.MaxConcurrentRequests
			}
			if update.Name != nil {
				account.Name = strings.TrimSpace(*update.Name)
			}
			if update.APIUpstreamBaseURL != nil {
				account.Credential.BaseURL = strings.TrimSpace(*update.APIUpstreamBaseURL)
				account.BaseURL = account.Credential.BaseURL
			}
			if update.APIUpstreamAPIKey != nil {
				account.Credential.EncryptedAPIKey = "updated-encrypted-api-key"
			}
			if update.FingerprintProfileIDSet {
				account.FingerprintProfileID = update.FingerprintProfileID
			}
			if update.ClearStatus || update.APIUpstreamBaseURL != nil || update.APIUpstreamAPIKey != nil {
				account.Status = provider.AccountStatusActive
				account.StatusReason = ""
				account.LastError = ""
				account.LastErrorAt = nil
				account.RateLimitedUntil = nil
				account.CircuitOpenUntil = nil
				account.FailureCount = 0
			}
			s.accounts[i] = account
			return account, nil
		}
	}
	return provider.Account{}, provider.ErrNotConnected
}

func (s *fakeProviderService) ResetAccountStatus(_ context.Context, id int64) (provider.Account, error) {
	if s.resetStatusErr != nil {
		return provider.Account{}, s.resetStatusErr
	}
	account, err := s.UpdateAccount(context.Background(), id, provider.AccountUpdate{ClearStatus: true})
	if err != nil {
		return provider.Account{}, err
	}
	s.resetStatusAccountID = id
	s.resetStatusAccountIDs = append(s.resetStatusAccountIDs, id)
	return account, nil
}

func (s *fakeProviderService) RefreshAccount(_ context.Context, id int64) (provider.Account, error) {
	if s.refreshErr != nil {
		return provider.Account{}, s.refreshErr
	}
	for i, account := range s.accounts {
		if account.ID == id {
			now := time.Now()
			account.LastRefreshAt = &now
			account.Status = provider.AccountStatusActive
			account.StatusReason = ""
			s.accounts[i] = account
			s.refreshedAccountID = id
			s.refreshedAccountIDs = append(s.refreshedAccountIDs, id)
			return account, nil
		}
	}
	return provider.Account{}, provider.ErrNotConnected
}

func (s *fakeProviderService) TestAccount(_ context.Context, id int64) (provider.Account, error) {
	for i, account := range s.accounts {
		if account.ID == id {
			account.Status = provider.AccountStatusActive
			account.StatusReason = ""
			account.LastError = ""
			account.LastErrorAt = nil
			account.RateLimitedUntil = nil
			account.CircuitOpenUntil = nil
			account.FailureCount = 0
			s.accounts[i] = account
			s.testedAccountID = id
			s.testedAccountIDs = append(s.testedAccountIDs, id)
			return account, nil
		}
	}
	return provider.Account{}, provider.ErrNotConnected
}

func (s *fakeProviderService) TestAccounts(ctx context.Context) ([]provider.Account, error) {
	tested := make([]provider.Account, 0, len(s.accounts))
	for _, account := range s.accounts {
		updated, err := s.TestAccount(ctx, account.ID)
		if err != nil {
			return nil, err
		}
		tested = append(tested, updated)
	}
	s.testedAllAccounts = true
	return tested, nil
}

func (s *fakeProviderService) PauseAccountScheduling(_ context.Context, id int64, duration time.Duration) (provider.Account, error) {
	for i, account := range s.accounts {
		if account.ID == id {
			now := time.Now()
			until := now.Add(duration)
			account.Status = provider.AccountStatusCircuitOpen
			account.StatusReason = "manually paused"
			account.LastError = "manually paused"
			account.LastErrorAt = &now
			account.CircuitOpenUntil = &until
			s.accounts[i] = account
			s.pausedAccountID = id
			s.pausedAccountIDs = append(s.pausedAccountIDs, id)
			s.pauseDuration = duration
			return account, nil
		}
	}
	return provider.Account{}, provider.ErrNotConnected
}

func (s *fakeProviderService) DisconnectAccount(_ context.Context, id int64) error {
	if s.disconnectErr != nil {
		return s.disconnectErr
	}
	for i, account := range s.accounts {
		if account.ID == id {
			s.disconnectedAccountID = id
			s.disconnectedAccountIDs = append(s.disconnectedAccountIDs, id)
			s.accounts = append(s.accounts[:i], s.accounts[i+1:]...)
			return nil
		}
	}
	return provider.ErrNotConnected
}

func (s *fakeProviderService) Disconnect(_ context.Context) error {
	s.disconnected = true
	return nil
}

func (s *fakeProviderService) SyncUpstreamAccountModels(_ context.Context, accountID int64) ([]provider.AccountModel, provider.AccountModelSyncSummary, error) {
	if s.syncModelsErr != nil {
		return nil, provider.AccountModelSyncSummary{}, s.syncModelsErr
	}
	return append([]provider.AccountModel(nil), s.syncModelsResult...), s.syncModelsSummary, nil
}

func TestLivezReturnsOK(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{err: nil}, nil, nil)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/livez", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type = %q, want application/json", got)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("status body = %q, want ok", body["status"])
	}
}

func TestHealthzRemainsLivenessAlias(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{err: errHealth}, nil, nil)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("status body = %q, want ok", body["status"])
	}
}

func TestVersionReturnsOnlyPublicBuildVersion(t *testing.T) {
	build := buildinfo.Info{
		Version: "sha-0123456789ab",
		Commit:  "0123456789abcdef0123456789abcdef01234567",
		BuiltAt: "2026-07-21T08:30:00Z",
	}
	server := NewServer(config.Config{}, staticHealth{err: nil}, nil, nil, build)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/version", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["version"] != build.Version {
		t.Fatalf("version = %q, want %q", body["version"], build.Version)
	}
	if len(body) != 1 {
		t.Fatalf("body = %+v, want public version only", body)
	}
}

func TestReadyzReturnsComponentStatus(t *testing.T) {
	webFS := fstest.MapFS{"200.html": {Data: []byte("ready")}}
	server := NewServer(config.Config{}, staticHealth{err: nil}, nil, nil, webFS)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	for field, want := range map[string]string{
		"status": "ok", "database": "ok", "staticAssets": "ok",
	} {
		if body[field] != want {
			t.Fatalf("%s = %q, want %q", field, body[field], want)
		}
	}
}

func TestReadyzReportsDatabaseError(t *testing.T) {
	webFS := fstest.MapFS{"index.html": {Data: []byte("ready")}}
	server := NewServer(config.Config{}, staticHealth{err: errHealth}, nil, nil, webFS)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "not_ready" || body["database"] != "error" || body["staticAssets"] != "ok" {
		t.Fatalf("body = %+v, want database error only", body)
	}
}

func TestReadyzReportsMissingStaticAssets(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{err: nil}, nil, nil, fstest.MapFS{})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "not_ready" || body["database"] != "ok" || body["staticAssets"] != "error" {
		t.Fatalf("body = %+v, want static asset error only", body)
	}
}

func TestAdminHealthIncludesDatabaseStatus(t *testing.T) {
	build := buildinfo.Info{Version: "sha-0123456789ab", Commit: "secret-detailed-commit", BuiltAt: "2026-07-21T08:30:00Z"}
	server := NewServer(config.Config{}, staticHealth{err: nil}, newFakeAdminService(), nil, build)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/health", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body struct {
		Status   string          `json:"status"`
		Database string          `json:"database"`
		Build    *buildinfo.Info `json:"build"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "ok" {
		t.Fatalf("Status = %q, want ok", body.Status)
	}
	if body.Database != "ok" {
		t.Fatalf("Database = %q, want ok", body.Database)
	}
	if body.Build != nil {
		t.Fatalf("Build = %+v, want omitted without an authenticated session", body.Build)
	}
}

func TestAdminHealthIncludesBuildIdentityForAuthenticatedSession(t *testing.T) {
	build := buildinfo.Info{
		Version: "sha-0123456789ab",
		Commit:  "0123456789abcdef0123456789abcdef01234567",
		BuiltAt: "2026-07-21T08:30:00Z",
	}
	server := NewServer(config.Config{}, staticHealth{err: nil}, newFakeAdminService(), nil, build)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/admin/health", nil)
	request.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body struct {
		Status   string          `json:"status"`
		Database string          `json:"database"`
		Build    *buildinfo.Info `json:"build"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "ok" || body.Database != "ok" {
		t.Fatalf("health = %q/%q, want ok/ok", body.Status, body.Database)
	}
	if body.Build == nil || *body.Build != build {
		t.Fatalf("Build = %+v, want %+v", body.Build, build)
	}
}

func TestAdminHealthShowsUnsafeMultiInstanceWarningOnlyWhenAuthenticated(t *testing.T) {
	server := NewServer(config.Config{AllowUnsafeMultiInstance: true}, staticHealth{}, newFakeAdminService(), nil)
	unauthenticated := httptest.NewRecorder()
	server.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/admin/health", nil))
	if strings.Contains(unauthenticated.Body.String(), "unsafe_multi_instance_enabled") {
		t.Fatalf("unauthenticated health leaked warning: %s", unauthenticated.Body.String())
	}

	request := httptest.NewRequest(http.MethodGet, "/api/admin/health", nil)
	request.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if !strings.Contains(recorder.Body.String(), "unsafe_multi_instance_enabled") {
		t.Fatalf("authenticated health missing warning: %s", recorder.Body.String())
	}
}

func TestAdminHealthIncludesRequestLogRetentionTaskOnlyForAuthenticatedSession(t *testing.T) {
	started := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	source := fakeRequestLogRetentionStatusSource{status: admin.RequestLogRetentionStatus{
		AutomaticEnabled: true, Running: true, LastStartedAt: &started,
	}}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil, source)

	unauthenticated := httptest.NewRecorder()
	server.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/admin/health", nil))
	if strings.Contains(unauthenticated.Body.String(), "requestLogRetention") {
		t.Fatalf("unauthenticated health leaked task status: %s", unauthenticated.Body.String())
	}

	request := httptest.NewRequest(http.MethodGet, "/api/admin/health", nil)
	request.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	var body struct {
		Tasks map[string]admin.RequestLogRetentionStatus `json:"tasks"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	status := body.Tasks["requestLogRetention"]
	if !status.AutomaticEnabled || !status.Running || status.LastStartedAt == nil || !status.LastStartedAt.Equal(started) {
		t.Fatalf("retention task status = %+v", status)
	}
}

func TestAdminHealthIncludesResponseAffinityRetentionOnlyForAuthenticatedSession(t *testing.T) {
	started := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	source := fakeResponseAffinityRetentionStatusSource{status: gateway.ResponseAffinityRetentionStatus{
		AutomaticEnabled: true, Running: true, LastStartedAt: &started,
	}}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil, source)

	unauthenticated := httptest.NewRecorder()
	server.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/admin/health", nil))
	if strings.Contains(unauthenticated.Body.String(), "responseAffinityRetention") {
		t.Fatalf("unauthenticated health leaked response affinity status: %s", unauthenticated.Body.String())
	}

	request := httptest.NewRequest(http.MethodGet, "/api/admin/health", nil)
	request.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	var body struct {
		Tasks map[string]json.RawMessage `json:"tasks"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	var status gateway.ResponseAffinityRetentionStatus
	if err := json.Unmarshal(body.Tasks["responseAffinityRetention"], &status); err != nil {
		t.Fatalf("decode response affinity status: %v", err)
	}
	if !status.AutomaticEnabled || !status.Running || status.LastStartedAt == nil || !status.LastStartedAt.Equal(started) {
		t.Fatalf("response affinity retention status = %+v", status)
	}
}

func TestAdminHealthIncludesAlertDeliveryTaskOnlyForAuthenticatedSession(t *testing.T) {
	source := fakeAlertDeliveryStatusSource{status: alerting.DeliveryStatus{
		Enabled: true, Running: true, QueueDepth: 3, DroppedCount: 2,
	}}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil, source)

	for _, testCase := range []struct {
		name   string
		cookie string
	}{
		{name: "unauthenticated"},
		{name: "invalid session", cookie: "expired-session"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/admin/health", nil)
			if testCase.cookie != "" {
				request.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: testCase.cookie})
			}
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, request)
			if strings.Contains(recorder.Body.String(), "alertDelivery") {
				t.Fatalf("health leaked alert delivery task status: %s", recorder.Body.String())
			}
		})
	}

	request := httptest.NewRequest(http.MethodGet, "/api/admin/health", nil)
	request.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	var body struct {
		Tasks map[string]json.RawMessage `json:"tasks"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if _, ok := body.Tasks["alertDelivery"]; !ok {
		t.Fatalf("authenticated health tasks = %s, want alertDelivery", recorder.Body.String())
	}
	var status alerting.DeliveryStatus
	if err := json.Unmarshal(body.Tasks["alertDelivery"], &status); err != nil {
		t.Fatalf("decode alert delivery status: %v", err)
	}
	if !status.Enabled || !status.Running || status.QueueDepth != 3 || status.DroppedCount != 2 {
		t.Fatalf("alert delivery status = %+v, want configured status", status)
	}
}

func TestAdminHealthMergesBackgroundTaskStatuses(t *testing.T) {
	server := NewServer(
		config.Config{}, staticHealth{}, newFakeAdminService(), nil,
		fakeRequestLogRetentionStatusSource{}, fakeAlertDeliveryStatusSource{},
	)
	request := httptest.NewRequest(http.MethodGet, "/api/admin/health", nil)
	request.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)

	var body struct {
		Tasks map[string]json.RawMessage `json:"tasks"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if _, ok := body.Tasks["requestLogRetention"]; !ok {
		t.Fatalf("health tasks = %s, want requestLogRetention", recorder.Body.String())
	}
	if _, ok := body.Tasks["alertDelivery"]; !ok {
		t.Fatalf("health tasks = %s, want alertDelivery", recorder.Body.String())
	}
}

func TestAdminHealthOmitsBuildIdentityForInvalidSession(t *testing.T) {
	build := buildinfo.Info{
		Version: "sha-0123456789ab",
		Commit:  "0123456789abcdef0123456789abcdef01234567",
		BuiltAt: "2026-07-21T08:30:00Z",
	}
	server := NewServer(config.Config{}, staticHealth{err: nil}, newFakeAdminService(), nil, build)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/admin/health", nil)
	request.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "expired-session"})

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body struct {
		Status   string          `json:"status"`
		Database string          `json:"database"`
		Build    *buildinfo.Info `json:"build"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "ok" || body.Database != "ok" {
		t.Fatalf("health = %q/%q, want ok/ok", body.Status, body.Database)
	}
	if body.Build != nil {
		t.Fatalf("Build = %+v, want omitted for invalid session", body.Build)
	}
}

func TestAdminHealthReportsDatabaseError(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{err: errHealth}, nil, nil)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/health", nil))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
	var body struct {
		Status   string `json:"status"`
		Database string `json:"database"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "degraded" {
		t.Fatalf("Status = %q, want degraded", body.Status)
	}
	if body.Database != "error" {
		t.Fatalf("Database = %q, want error", body.Database)
	}
}

func TestBootstrapReturnsPublicConfiguration(t *testing.T) {
	cfg := config.Config{
		PublicURL:     "https://n2api.example.com",
		AdminUsername: "owner",
	}
	server := NewServer(cfg, staticHealth{err: nil}, nil, nil)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/bootstrap", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body struct {
		PublicURL     string `json:"publicUrl"`
		AdminUsername string `json:"adminUsername"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.PublicURL != "https://n2api.example.com" {
		t.Fatalf("PublicURL = %q, want configured public URL", body.PublicURL)
	}
	if body.AdminUsername != "owner" {
		t.Fatalf("AdminUsername = %q, want owner", body.AdminUsername)
	}
}

func TestAdminLoginSetsSessionCookie(t *testing.T) {
	admins := newFakeAdminService()
	server := NewServer(config.Config{PublicURL: "http://localhost:3000"}, staticHealth{}, admins, nil)
	recorder := httptest.NewRecorder()
	body := strings.NewReader(`{"username":"admin","password":"secret"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/login", body)
	req.RemoteAddr = "203.0.113.42:4321"
	req.Header.Set("User-Agent", "N2API browser test")

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}
	if cookie := cookies[0]; cookie.Name != "n2api_admin_session" || !cookie.HttpOnly {
		t.Fatalf("session cookie = %+v", cookie)
	}
	if admins.loginMetadata.CreatedIP != "203.0.113.42" || admins.loginMetadata.UserAgent != "N2API browser test" {
		t.Fatalf("login metadata = %+v", admins.loginMetadata)
	}
}

func TestAdminLoginSetsSecureCookieForHTTPSPublicURL(t *testing.T) {
	for _, publicURL := range []string{"https://n2api.example.com", "HTTPS://n2api.example.com"} {
		t.Run(publicURL, func(t *testing.T) {
			server := NewServer(config.Config{PublicURL: publicURL}, staticHealth{}, newFakeAdminService(), nil)
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(`{"username":"admin","password":"secret"}`)))

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", recorder.Code)
			}
			if cookie := recorder.Result().Cookies()[0]; !cookie.Secure {
				t.Fatalf("Secure = false, want true")
			}
		})
	}
}

func TestInvalidAdminLoginReturnsUnauthorized(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil)
	for _, payload := range []string{
		`{"username":"admin","password":"wrong"}`,
		`{"username":"missing","password":"wrong"}`,
		`{"username":"","password":"wrong"}`,
	} {
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(payload)))

		if recorder.Code != http.StatusUnauthorized || strings.TrimSpace(recorder.Body.String()) != `{"error":"invalid_credentials"}` {
			t.Fatalf("status/body = %d/%s, want uniform 401 response", recorder.Code, recorder.Body.String())
		}
	}
}

func TestAdminLoginThrottleReturnsRetryAfterAndUniformBody(t *testing.T) {
	cfg := config.Config{AdminLoginThrottleEnabled: true, AdminLoginThrottleFailures: 2, AdminLoginThrottleMaxEntries: 128}
	server := NewServer(cfg, staticHealth{}, newFakeAdminService(), nil)

	for attempt := 1; attempt <= 3; attempt++ {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(`{"username":"admin","password":"wrong"}`))
		req.RemoteAddr = "192.0.2.10:1234"
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, req)

		wantStatus := http.StatusUnauthorized
		if attempt == 3 {
			wantStatus = http.StatusTooManyRequests
		}
		if recorder.Code != wantStatus || strings.TrimSpace(recorder.Body.String()) != `{"error":"invalid_credentials"}` {
			t.Fatalf("attempt %d status/body = %d/%s", attempt, recorder.Code, recorder.Body.String())
		}
		if attempt >= 2 && recorder.Header().Get("Retry-After") != "1" {
			t.Fatalf("attempt %d Retry-After = %q, want 1", attempt, recorder.Header().Get("Retry-After"))
		}
	}
}

func TestAdminLoginThrottleAtomicallyBoundsConcurrentPasswordChecks(t *testing.T) {
	const (
		threshold = 2
		requests  = 20
	)
	started := make(chan struct{}, requests)
	release := make(chan struct{})
	admins := newFakeAdminService()
	admins.loginStarted = started
	admins.loginRelease = release
	server := NewServer(config.Config{
		AdminLoginThrottleEnabled:    true,
		AdminLoginThrottleFailures:   threshold,
		AdminLoginThrottleMaxEntries: 128,
	}, staticHealth{}, admins, nil)

	responses := make(chan int, requests)
	for range requests {
		go func() {
			req := httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(`{"username":"admin","password":"wrong"}`))
			req.RemoteAddr = "192.0.2.10:1234"
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, req)
			responses <- recorder.Code
		}()
	}

	for range threshold {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for reserved password checks")
		}
	}
	for range requests - threshold {
		select {
		case status := <-responses:
			if status != http.StatusTooManyRequests {
				t.Fatalf("unreserved concurrent status = %d, want 429", status)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for throttled concurrent requests")
		}
	}
	if got := admins.loginCallCount(); got != threshold {
		t.Fatalf("password checks = %d, want threshold %d", got, threshold)
	}

	close(release)
	for range threshold {
		select {
		case status := <-responses:
			if status != http.StatusUnauthorized {
				t.Fatalf("reserved concurrent status = %d, want 401", status)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for reserved concurrent requests")
		}
	}
	select {
	case <-started:
		t.Fatal("more password checks entered than the configured threshold")
	default:
	}
}

func TestAdminLoginThrottleCancelsReservationAfterInternalError(t *testing.T) {
	admins := newFakeAdminService()
	admins.loginErr = errors.New("database unavailable")
	server := NewServer(config.Config{
		AdminLoginThrottleEnabled:    true,
		AdminLoginThrottleFailures:   1,
		AdminLoginThrottleMaxEntries: 128,
	}, staticHealth{}, admins, nil)
	send := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(`{"username":"admin","password":"wrong"}`))
		req.RemoteAddr = "192.0.2.10:1234"
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, req)
		return recorder
	}

	if recorder := send(); recorder.Code != http.StatusInternalServerError {
		t.Fatalf("internal-error status = %d, want 500", recorder.Code)
	}
	admins.loginMu.Lock()
	admins.loginErr = nil
	admins.loginMu.Unlock()
	if recorder := send(); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("post-cancel status = %d body=%s, want 401", recorder.Code, recorder.Body.String())
	}
}

func TestAdminLoginThrottleCancelsReservationAfterPanic(t *testing.T) {
	admins := newFakeAdminService()
	admins.loginPanic = "login panic"
	server := NewServer(config.Config{
		AdminLoginThrottleEnabled:    true,
		AdminLoginThrottleFailures:   1,
		AdminLoginThrottleMaxEntries: 128,
	}, staticHealth{}, admins, nil)
	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(`{"username":"admin","password":"wrong"}`))
		req.RemoteAddr = "192.0.2.10:1234"
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, req)
		return recorder
	}

	func() {
		defer func() {
			if recovered := recover(); recovered != "login panic" {
				t.Fatalf("recovered = %v, want login panic", recovered)
			}
		}()
		request()
	}()
	admins.loginMu.Lock()
	admins.loginPanic = nil
	admins.loginMu.Unlock()
	if recorder := request(); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("post-panic status = %d body=%s, want 401", recorder.Code, recorder.Body.String())
	}
}

func TestAdminLoginThrottleAppliesIPAndUsernameDimensions(t *testing.T) {
	cfg := config.Config{AdminLoginThrottleEnabled: true, AdminLoginThrottleFailures: 1, AdminLoginThrottleMaxEntries: 128}
	server := NewServer(cfg, staticHealth{}, newFakeAdminService(), nil)
	first := httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(`{"username":"admin","password":"wrong"}`))
	first.RemoteAddr = "192.0.2.10:1234"
	server.ServeHTTP(httptest.NewRecorder(), first)

	tests := []struct {
		name, username, remoteAddr string
	}{
		{name: "same IP different username", username: "other", remoteAddr: "192.0.2.10:4321"},
		{name: "same username different IP", username: "admin", remoteAddr: "198.51.100.20:1234"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(`{"username":"`+tt.username+`","password":"wrong"}`))
			req.RemoteAddr = tt.remoteAddr
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, req)
			if recorder.Code != http.StatusTooManyRequests {
				t.Fatalf("status = %d body=%s, want 429", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestAdminLoginSuccessResetsThrottleIdentities(t *testing.T) {
	cfg := config.Config{AdminLoginThrottleEnabled: true, AdminLoginThrottleFailures: 3, AdminLoginThrottleMaxEntries: 128}
	server := NewServer(cfg, staticHealth{}, newFakeAdminService(), nil)
	send := func(password string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(`{"username":"admin","password":"`+password+`"}`))
		req.RemoteAddr = "192.0.2.10:1234"
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, req)
		return recorder
	}
	for range 2 {
		if recorder := send("wrong"); recorder.Code != http.StatusUnauthorized {
			t.Fatalf("pre-success status = %d", recorder.Code)
		}
	}
	if recorder := send("secret"); recorder.Code != http.StatusOK {
		t.Fatalf("success status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	for range 2 {
		recorder := send("wrong")
		if recorder.Code != http.StatusUnauthorized || recorder.Header().Get("Retry-After") != "" {
			t.Fatalf("post-reset status/retry = %d/%q", recorder.Code, recorder.Header().Get("Retry-After"))
		}
	}
}

func TestAdminMeRequiresSession(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/me", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestAdminChangePasswordWrongCurrentPasswordKeepsSessionAuthenticated(t *testing.T) {
	admins := newFakeAdminService()
	admins.changePasswordErr = admin.ErrUnauthorized
	server := NewServer(config.Config{}, staticHealth{}, admins, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/change-password", strings.NewReader(`{"currentPassword":"wrong","newPassword":"new-secret"}`))
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", recorder.Code, recorder.Body.String())
	}
	if recorder.Body.String() != "{\"error\":\"invalid_current_password\"}\n" {
		t.Fatalf("body = %q, want invalid_current_password", recorder.Body.String())
	}
	if recorder.Header().Get("Set-Cookie") != "" {
		t.Fatalf("wrong current password cleared valid session: %q", recorder.Header().Get("Set-Cookie"))
	}
}

func TestAdminMeReturnsUsernameWithoutPasswordHash(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/me", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["username"] != "admin" {
		t.Fatalf("username = %v, want admin", body["username"])
	}
	if _, ok := body["passwordHash"]; ok {
		t.Fatalf("body includes passwordHash: %v", body)
	}
}

func TestAdminLogoutClearsSessionCookie(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/logout", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", recorder.Code)
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}
	if cookie := cookies[0]; cookie.Name != "n2api_admin_session" || cookie.Value != "" || cookie.MaxAge >= 0 {
		t.Fatalf("cleared cookie = %+v", cookie)
	}
}

func TestAdminLogoutWithoutSessionClearsCookieWithoutRevoking(t *testing.T) {
	admins := newFakeAdminService()
	admins.errorOnEmptyLogout = true
	server := NewServer(config.Config{}, staticHealth{}, admins, nil)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/admin/logout", nil))

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", recorder.Code)
	}
	if len(admins.logoutTokens) != 0 {
		t.Fatalf("logout tokens = %+v, want no logout call", admins.logoutTokens)
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}
	if cookie := cookies[0]; cookie.Name != "n2api_admin_session" || cookie.Value != "" || cookie.MaxAge >= 0 {
		t.Fatalf("cleared cookie = %+v", cookie)
	}
}

func TestAdminSessionsListReturnsCurrentSessionsWithoutSecrets(t *testing.T) {
	admins := newFakeAdminService()
	admins.sessions = []admin.AdminSession{{
		ID: 7, Current: true, CreatedAt: time.Unix(1000, 0).UTC(), LastUsedAt: time.Unix(1100, 0).UTC(),
		ExpiresAt: time.Unix(2000, 0).UTC(), CreatedIP: "203.0.113.0/24", UserAgent: "Test browser",
	}}
	server := NewServer(config.Config{}, staticHealth{}, admins, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/sessions", nil)
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Sessions []admin.AdminSession `json:"sessions"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Sessions) != 1 || !body.Sessions[0].Current || body.Sessions[0].CreatedIP != "203.0.113.0/24" {
		t.Fatalf("sessions = %+v", body.Sessions)
	}
	if strings.Contains(recorder.Body.String(), "valid-session") || strings.Contains(strings.ToLower(recorder.Body.String()), "tokenhash") {
		t.Fatalf("session response leaks credential material: %s", recorder.Body.String())
	}
}

func TestAdminSessionRevocationHandlesCurrentOtherAndNotFound(t *testing.T) {
	for _, tt := range []struct {
		name           string
		revokedCurrent bool
		err            error
		wantStatus     int
		wantClear      bool
	}{
		{name: "other session", wantStatus: http.StatusNoContent},
		{name: "current session", revokedCurrent: true, wantStatus: http.StatusNoContent, wantClear: true},
		{name: "missing or wrong owner", err: admin.ErrNotFound, wantStatus: http.StatusNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			admins := newFakeAdminService()
			admins.revokeSessionCurrent = tt.revokedCurrent
			admins.revokeSessionErr = tt.err
			server := NewServer(config.Config{}, staticHealth{}, admins, nil)
			req := httptest.NewRequest(http.MethodDelete, "/api/admin/sessions/42", nil)
			req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d body=%s, want %d", recorder.Code, recorder.Body.String(), tt.wantStatus)
			}
			if admins.revokedSessionID != 42 || admins.revokeSessionToken != "valid-session" {
				t.Fatalf("revoke args = %d/%q", admins.revokedSessionID, admins.revokeSessionToken)
			}
			if got := recorder.Header().Get("Set-Cookie"); (got != "") != tt.wantClear {
				t.Fatalf("Set-Cookie = %q, want clear %t", got, tt.wantClear)
			}
		})
	}
}

func TestAdminSessionRevocationRejectsInvalidID(t *testing.T) {
	admins := newFakeAdminService()
	server := NewServer(config.Config{}, staticHealth{}, admins, nil)
	req := httptest.NewRequest(http.MethodDelete, "/api/admin/sessions/invalid", nil)
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest || admins.revokedSessionID != 0 {
		t.Fatalf("status/revoked ID = %d/%d, want 400/0", recorder.Code, admins.revokedSessionID)
	}
}

func TestAdminSessionRevocationRejectsCrossOriginBeforeService(t *testing.T) {
	admins := newFakeAdminService()
	server := NewServer(config.Config{}, staticHealth{}, admins, nil)
	req := httptest.NewRequest(http.MethodDelete, "http://n2api.example/api/admin/sessions/42", nil)
	req.Header.Set("Origin", "https://attacker.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusForbidden || admins.revokedSessionID != 0 {
		t.Fatalf("status/revoked ID = %d/%d, want 403/0", recorder.Code, admins.revokedSessionID)
	}
}

func TestAdminRevokeOtherSessionsPreservesCurrentToken(t *testing.T) {
	admins := newFakeAdminService()
	admins.revokeOthersCount = 3
	server := NewServer(config.Config{}, staticHealth{}, admins, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/sessions/revoke-others", nil)
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK || admins.revokeOthersToken != "valid-session" {
		t.Fatalf("status/token = %d/%q", recorder.Code, admins.revokeOthersToken)
	}
	if recorder.Header().Get("Set-Cookie") != "" {
		t.Fatalf("revoke others cleared current cookie: %q", recorder.Header().Get("Set-Cookie"))
	}
	var body map[string]int64
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || body["revoked"] != 3 {
		t.Fatalf("body/error = %s/%v", recorder.Body.String(), err)
	}
}

func TestListAPIKeysRequiresSessionAndReturnsKeys(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/keys", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/keys", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder = httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body struct {
		Keys []admin.APIKey `json:"keys"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Keys) != 1 || body.Keys[0].ID != 7 {
		t.Fatalf("keys = %+v, want key 7", body.Keys)
	}
}

func TestListAPIKeysIncludesConcurrencyState(t *testing.T) {
	admins := newFakeAdminService()
	admins.gatewaySettings.MaxConcurrentRequestsPerKey = 2
	gateway := &fakeGatewayHandler{apiKeyConcurrency: map[int64]int{7: 2}}
	server := NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService(), gateway)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/keys", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Keys []struct {
			admin.APIKey
			CurrentConcurrentRequests      int  `json:"currentConcurrentRequests"`
			EffectiveMaxConcurrentRequests int  `json:"effectiveMaxConcurrentRequests"`
			ConcurrencyBlocked             bool `json:"concurrencyBlocked"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Keys) != 1 {
		t.Fatalf("keys = %+v, want one key", body.Keys)
	}
	if body.Keys[0].CurrentConcurrentRequests != 2 || body.Keys[0].EffectiveMaxConcurrentRequests != 2 || !body.Keys[0].ConcurrencyBlocked {
		t.Fatalf("key concurrency = %+v, want current 2 effective 2 blocked", body.Keys[0])
	}
}

func TestListAPIKeysIncludesRateWindowState(t *testing.T) {
	admins := newFakeAdminService()
	admins.keys[0].RequestsPerMinute = 0
	admins.keys[0].TokensPerMinute = 90
	admins.gatewaySettings.RequestsPerMinutePerKey = 12
	admins.gatewaySettings.TokensPerMinutePerKey = 200
	gateway := &fakeGatewayHandler{
		apiKeyRequestRate: map[int64]int{7: 12},
		apiKeyTokenRate:   map[int64]int{7: 42},
	}
	server := NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService(), gateway)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/keys", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Keys []struct {
			admin.APIKey
			CurrentRequestsThisMinute  int  `json:"currentRequestsThisMinute"`
			EffectiveRequestsPerMinute int  `json:"effectiveRequestsPerMinute"`
			RequestRateRemaining       int  `json:"requestRateRemaining"`
			RequestRateLimited         bool `json:"requestRateLimited"`
			CurrentTokensThisMinute    int  `json:"currentTokensThisMinute"`
			EffectiveTokensPerMinute   int  `json:"effectiveTokensPerMinute"`
			TokenRateRemaining         int  `json:"tokenRateRemaining"`
			TokenRateLimited           bool `json:"tokenRateLimited"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Keys) != 1 {
		t.Fatalf("keys = %+v, want one key", body.Keys)
	}
	key := body.Keys[0]
	if key.CurrentRequestsThisMinute != 12 || key.EffectiveRequestsPerMinute != 12 || key.RequestRateRemaining != 0 || !key.RequestRateLimited {
		t.Fatalf("request window = %+v, want current 12 effective 12 remaining 0 limited", key)
	}
	if key.CurrentTokensThisMinute != 42 || key.EffectiveTokensPerMinute != 90 || key.TokenRateRemaining != 48 || key.TokenRateLimited {
		t.Fatalf("token window = %+v, want current 42 effective 90 remaining 48 not limited", key)
	}
}

func TestListAPIKeysIncludesBudgetUsageState(t *testing.T) {
	admins := newFakeAdminService()
	remainingRequests24h := int64(3)
	remainingCost24h := int64(500)
	remainingTokens30d := int64(0)
	admins.keys[0].RequestBudget24h = 10
	admins.keys[0].CostBudgetMicrousd24h = 1500
	admins.keys[0].TokenBudget30d = 100
	admins.budgetUsage[7] = admin.APIKeyBudgetUsage{
		KeyID:                    7,
		RequestsUsed24h:          7,
		TokensUsed24h:            42,
		CostMicrousd24h:          1000,
		RequestsUsed30d:          12,
		TokensUsed30d:            120,
		CostMicrousd30d:          5000,
		RequestsRemaining24h:     &remainingRequests24h,
		CostRemainingMicrousd24h: &remainingCost24h,
		TokensRemaining30d:       &remainingTokens30d,
		RequestBudgetExceeded:    false,
		TokenBudgetExceeded:      true,
		CostBudgetExceeded:       true,
	}
	server := NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService())
	req := httptest.NewRequest(http.MethodGet, "/api/admin/keys", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Keys []struct {
			admin.APIKey
			RequestsUsed24h          int64  `json:"requestsUsed24h"`
			CostMicrousd24h          int64  `json:"costMicrousd24h"`
			TokensUsed30d            int64  `json:"tokensUsed30d"`
			RequestsRemaining24h     *int64 `json:"requestsRemaining24h"`
			CostRemainingMicrousd24h *int64 `json:"costRemainingMicrousd24h"`
			TokensRemaining30d       *int64 `json:"tokensRemaining30d"`
			RequestBudgetExceeded    bool   `json:"requestBudgetExceeded"`
			TokenBudgetExceeded      bool   `json:"tokenBudgetExceeded"`
			CostBudgetExceeded       bool   `json:"costBudgetExceeded"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Keys) != 1 {
		t.Fatalf("keys = %+v, want one key", body.Keys)
	}
	key := body.Keys[0]
	if key.RequestBudget24h != 10 || key.CostBudgetMicrousd24h != 1500 || key.TokenBudget30d != 100 || key.RequestsUsed24h != 7 || key.CostMicrousd24h != 1000 || key.TokensUsed30d != 120 {
		t.Fatalf("budget fields = %+v, want configured budgets and usage", key)
	}
	if key.RequestsRemaining24h == nil || *key.RequestsRemaining24h != 3 || key.CostRemainingMicrousd24h == nil || *key.CostRemainingMicrousd24h != 500 || key.TokensRemaining30d == nil || *key.TokensRemaining30d != 0 {
		t.Fatalf("budget remaining = %+v, want request 3 and token 0", key)
	}
	if key.RequestBudgetExceeded || !key.TokenBudgetExceeded || !key.CostBudgetExceeded {
		t.Fatalf("budget exceeded flags = request:%v token:%v cost:%v, want request false token/cost true", key.RequestBudgetExceeded, key.TokenBudgetExceeded, key.CostBudgetExceeded)
	}
}

func TestCreateAPIKeyReturnsOneTimeSecret(t *testing.T) {
	admins := newFakeAdminService()
	server := NewServer(config.Config{}, staticHealth{}, admins, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/keys", strings.NewReader(`{"name":"codex laptop","routingPoolId":3}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", recorder.Code)
	}
	var body struct {
		Secret string       `json:"secret"`
		Key    admin.APIKey `json:"key"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Secret == "" {
		t.Fatal("secret is empty")
	}
	if body.Key.RoutingPoolID == nil || *body.Key.RoutingPoolID != 3 || body.Key.RoutingPoolName != "primary" {
		t.Fatalf("created key = %+v, want primary routing pool", body.Key)
	}
}

func TestCreateAPIKeyRejectsMissingRoutingPool(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/keys", strings.NewReader(`{"name":"codex laptop","routingPoolId":999}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestGetAPIKeySecretReturnsReusableSecret(t *testing.T) {
	admins := newFakeAdminService()
	server := NewServer(config.Config{}, staticHealth{}, admins, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/keys/7/secret", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Secret != "n2api_abc_secret" {
		t.Fatalf("secret = %q, want reusable secret", body.Secret)
	}
}

func TestRevokeAPIKeyParsesIDAndReturnsRevokedKey(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/keys/7/revoke", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body struct {
		Key admin.APIKey `json:"key"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Key.ID != 7 || body.Key.RevokedAt == nil {
		t.Fatalf("revoked key = %+v, want revoked key 7", body)
	}
}

func TestListAPIKeysIncludesPhysicalDeleteAtForRevokedKeys(t *testing.T) {
	admins := newFakeAdminService()
	revokedAt := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	admins.keys = []admin.APIKey{
		{
			ID:        7,
			Name:      "deleted workstation",
			Prefix:    "n2_test",
			CreatedAt: revokedAt.Add(-time.Hour),
			RevokedAt: &revokedAt,
		},
		{
			ID:        8,
			Name:      "active workstation",
			Prefix:    "n2_live",
			CreatedAt: revokedAt.Add(-time.Hour),
		},
	}
	server := NewServer(config.Config{}, staticHealth{}, admins, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/keys", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Keys []struct {
			ID               int64      `json:"id"`
			PhysicalDeleteAt *time.Time `json:"physicalDeleteAt"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Keys) != 2 {
		t.Fatalf("keys length = %d, want 2", len(body.Keys))
	}
	want := revokedAt.Add(7 * 24 * time.Hour)
	if body.Keys[0].ID != 7 || body.Keys[0].PhysicalDeleteAt == nil || !body.Keys[0].PhysicalDeleteAt.Equal(want) {
		t.Fatalf("revoked key physicalDeleteAt = %+v, want %s", body.Keys[0], want.Format(time.RFC3339))
	}
	if body.Keys[1].ID != 8 || body.Keys[1].PhysicalDeleteAt != nil {
		t.Fatalf("active key physicalDeleteAt = %+v, want nil", body.Keys[1])
	}
}

func TestDeleteRevokedAPIKeyParsesIDAndReturnsNoContent(t *testing.T) {
	admins := newFakeAdminService()
	server := NewServer(config.Config{}, staticHealth{}, admins, nil)
	req := httptest.NewRequest(http.MethodDelete, "/api/admin/keys/7", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d body=%s, want 204", recorder.Code, recorder.Body.String())
	}
	if admins.deletedKeyID != 7 {
		t.Fatalf("deleted key ID = %d, want 7", admins.deletedKeyID)
	}
}

func TestDeleteRevokedAPIKeyReturnsNotFoundForNonRevokedKey(t *testing.T) {
	admins := newFakeAdminService()
	admins.deleteKeyErr = admin.ErrNotFound
	server := NewServer(config.Config{}, staticHealth{}, admins, nil)
	req := httptest.NewRequest(http.MethodDelete, "/api/admin/keys/7", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s, want 404", recorder.Code, recorder.Body.String())
	}
}

func TestUpdateAPIKeyNameEndpoint(t *testing.T) {
	admins := newFakeAdminService()
	server := NewServer(config.Config{}, staticHealth{}, admins, nil)
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/keys/7", strings.NewReader(`{"name":" renamed codex "}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Key admin.APIKey `json:"key"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Key.ID != 7 || body.Key.Name != "renamed codex" {
		t.Fatalf("key = %+v, want renamed key 7", body.Key)
	}
	if admins.renameKeyID != 7 || admins.renameName != " renamed codex " {
		t.Fatalf("recorded rename = id:%d name:%q", admins.renameKeyID, admins.renameName)
	}
	if strings.Contains(recorder.Body.String(), `"secret"`) || strings.Contains(recorder.Body.String(), "n2api_new_secret") {
		t.Fatalf("response leaked secret: %s", recorder.Body.String())
	}
}

func TestUpdateAPIKeyNameEndpointMapsErrors(t *testing.T) {
	for _, tc := range []struct {
		name       string
		path       string
		body       string
		serviceErr error
		wantStatus int
	}{
		{
			name:       "invalid id",
			path:       "/api/admin/keys/not-a-number",
			body:       `{"name":"renamed"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid name",
			path:       "/api/admin/keys/7",
			body:       `{"name":" "}`,
			serviceErr: admin.ErrInvalidInput,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "not found",
			path:       "/api/admin/keys/99",
			body:       `{"name":"renamed"}`,
			serviceErr: admin.ErrNotFound,
			wantStatus: http.StatusNotFound,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			admins := newFakeAdminService()
			admins.renameErr = tc.serviceErr
			server := NewServer(config.Config{}, staticHealth{}, admins, nil)
			req := httptest.NewRequest(http.MethodPatch, tc.path, strings.NewReader(tc.body))
			req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != tc.wantStatus {
				t.Fatalf("status = %d body=%s, want %d", recorder.Code, recorder.Body.String(), tc.wantStatus)
			}
		})
	}
}

func TestSetAPIKeyDisabledEndpoint(t *testing.T) {
	admins := newFakeAdminService()
	server := NewServer(config.Config{}, staticHealth{}, admins, nil)

	req := httptest.NewRequest(http.MethodPut, "/api/admin/keys/7/disabled", strings.NewReader(`{"disabled":true}`))
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Key admin.APIKey `json:"key"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Key.ID != 7 || body.Key.DisabledAt == nil {
		t.Fatalf("key = %+v, want disabled key 7", body.Key)
	}
	if admins.disabledKeyID != 7 || !admins.disabledValue {
		t.Fatalf("disabled call = (%d, %t), want (7, true)", admins.disabledKeyID, admins.disabledValue)
	}

	req = httptest.NewRequest(http.MethodPut, "/api/admin/keys/7/disabled", strings.NewReader(`{"disabled":false}`))
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	recorder = httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("enable status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode enable response: %v", err)
	}
	if body.Key.ID != 7 || body.Key.DisabledAt != nil {
		t.Fatalf("key = %+v, want enabled key 7", body.Key)
	}
	if admins.disabledKeyID != 7 || admins.disabledValue {
		t.Fatalf("disabled call = (%d, %t), want (7, false)", admins.disabledKeyID, admins.disabledValue)
	}
}

func TestSetAPIKeyDisabledEndpointMapsErrors(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		body       string
		serviceErr error
		wantStatus int
	}{
		{
			name:       "invalid id",
			path:       "/api/admin/keys/not-a-number/disabled",
			body:       `{"disabled":true}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "bad json",
			path:       "/api/admin/keys/7/disabled",
			body:       `{`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "not found",
			path:       "/api/admin/keys/99/disabled",
			body:       `{"disabled":true}`,
			serviceErr: admin.ErrNotFound,
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			admins := newFakeAdminService()
			admins.disabledErr = tt.serviceErr
			server := NewServer(config.Config{}, staticHealth{}, admins, nil)
			req := httptest.NewRequest(http.MethodPut, tt.path, strings.NewReader(tt.body))
			req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, body = %s, want %d", recorder.Code, recorder.Body.String(), tt.wantStatus)
			}
		})
	}
}

func TestUpdateAPIKeyModelPolicyEndpoint(t *testing.T) {
	admins := newFakeAdminService()
	server := NewServer(config.Config{}, staticHealth{}, admins, nil)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/keys/7/model-policy", strings.NewReader(`{"modelPolicy":"selected","models":["gpt-5","gpt-4.1"]}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Key admin.APIKey `json:"key"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Key.ID != 7 || body.Key.ModelPolicy != admin.APIKeyModelPolicySelected || !slices.Equal(body.Key.AllowedModels, []string{"gpt-5", "gpt-4.1"}) {
		t.Fatalf("key = %+v, want selected model policy", body.Key)
	}
	if admins.modelPolicyKeyID != 7 || admins.modelPolicy != admin.APIKeyModelPolicySelected || !slices.Equal(admins.modelPolicyModels, []string{"gpt-5", "gpt-4.1"}) {
		t.Fatalf("recorded model policy = id:%d policy:%q models:%v", admins.modelPolicyKeyID, admins.modelPolicy, admins.modelPolicyModels)
	}
}

func TestUpdateAPIKeyModelPolicyEndpointMapsErrors(t *testing.T) {
	for _, tc := range []struct {
		name       string
		path       string
		body       string
		serviceErr error
		wantStatus int
	}{
		{
			name:       "invalid id",
			path:       "/api/admin/keys/not-a-number/model-policy",
			body:       `{"modelPolicy":"selected","models":["gpt-5"]}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid policy",
			path:       "/api/admin/keys/7/model-policy",
			body:       `{"modelPolicy":"invalid","models":["gpt-5"]}`,
			serviceErr: admin.ErrInvalidInput,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty selected models",
			path:       "/api/admin/keys/7/model-policy",
			body:       `{"modelPolicy":"selected","models":[]}`,
			serviceErr: admin.ErrInvalidInput,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "not found",
			path:       "/api/admin/keys/99/model-policy",
			body:       `{"modelPolicy":"all"}`,
			serviceErr: admin.ErrNotFound,
			wantStatus: http.StatusNotFound,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			admins := newFakeAdminService()
			admins.modelPolicyErr = tc.serviceErr
			server := NewServer(config.Config{}, staticHealth{}, admins, nil)
			req := httptest.NewRequest(http.MethodPut, tc.path, strings.NewReader(tc.body))
			req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != tc.wantStatus {
				t.Fatalf("status = %d body=%s, want %d", recorder.Code, recorder.Body.String(), tc.wantStatus)
			}
		})
	}
}

func TestUpdateAPIKeyLimitsEndpoint(t *testing.T) {
	admins := newFakeAdminService()
	server := NewServer(config.Config{}, staticHealth{}, admins, nil)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/keys/7/limits", strings.NewReader(`{"requestsPerMinute":12,"tokensPerMinute":40000}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Key admin.APIKey `json:"key"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Key.ID != 7 || body.Key.RequestsPerMinute != 12 || body.Key.TokensPerMinute != 40000 {
		t.Fatalf("key = %+v, want key limit updates", body.Key)
	}
	if admins.limitKeyID != 7 || admins.requestsPerMinute != 12 || admins.tokensPerMinute != 40000 {
		t.Fatalf("recorded limits = id:%d requests:%d tokens:%d", admins.limitKeyID, admins.requestsPerMinute, admins.tokensPerMinute)
	}
}

func TestUpdateAPIKeyLimitsEndpointMapsErrors(t *testing.T) {
	for _, tc := range []struct {
		name       string
		path       string
		body       string
		serviceErr error
		wantStatus int
	}{
		{
			name:       "invalid id",
			path:       "/api/admin/keys/not-a-number/limits",
			body:       `{"requestsPerMinute":12,"tokensPerMinute":40000}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid input",
			path:       "/api/admin/keys/7/limits",
			body:       `{"requestsPerMinute":-1,"tokensPerMinute":40000}`,
			serviceErr: admin.ErrInvalidInput,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "not found",
			path:       "/api/admin/keys/99/limits",
			body:       `{"requestsPerMinute":12,"tokensPerMinute":40000}`,
			serviceErr: admin.ErrNotFound,
			wantStatus: http.StatusNotFound,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			admins := newFakeAdminService()
			admins.limitsErr = tc.serviceErr
			server := NewServer(config.Config{}, staticHealth{}, admins, nil)
			req := httptest.NewRequest(http.MethodPut, tc.path, strings.NewReader(tc.body))
			req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != tc.wantStatus {
				t.Fatalf("status = %d body=%s, want %d", recorder.Code, recorder.Body.String(), tc.wantStatus)
			}
		})
	}
}

func TestUpdateAPIKeyBudgetsEndpoint(t *testing.T) {
	admins := newFakeAdminService()
	server := NewServer(config.Config{}, staticHealth{}, admins, nil)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/keys/7/budgets", strings.NewReader(`{"requestBudget24h":10,"tokenBudget24h":1000,"costBudgetMicrousd24h":1500000,"requestBudget30d":300,"tokenBudget30d":30000,"costBudgetMicrousd30d":9000000}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Key admin.APIKey `json:"key"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Key.ID != 7 || body.Key.RequestBudget24h != 10 || body.Key.TokenBudget24h != 1000 || body.Key.CostBudgetMicrousd24h != 1500000 || body.Key.RequestBudget30d != 300 || body.Key.TokenBudget30d != 30000 || body.Key.CostBudgetMicrousd30d != 9000000 {
		t.Fatalf("key = %+v, want key budget updates", body.Key)
	}
	if admins.budgetKeyID != 7 || admins.requestBudget24h != 10 || admins.tokenBudget24h != 1000 || admins.costBudget24h != 1500000 || admins.requestBudget30d != 300 || admins.tokenBudget30d != 30000 || admins.costBudget30d != 9000000 {
		t.Fatalf("recorded budgets = id:%d request24h:%d token24h:%d cost24h:%d request30d:%d token30d:%d cost30d:%d", admins.budgetKeyID, admins.requestBudget24h, admins.tokenBudget24h, admins.costBudget24h, admins.requestBudget30d, admins.tokenBudget30d, admins.costBudget30d)
	}
}

func TestUpdateAPIKeyBudgetsEndpointMapsErrors(t *testing.T) {
	for _, tc := range []struct {
		name       string
		path       string
		body       string
		serviceErr error
		wantStatus int
	}{
		{
			name:       "invalid id",
			path:       "/api/admin/keys/not-a-number/budgets",
			body:       `{"requestBudget24h":10,"tokenBudget24h":1000,"requestBudget30d":300,"tokenBudget30d":30000}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid input",
			path:       "/api/admin/keys/7/budgets",
			body:       `{"requestBudget24h":-1,"tokenBudget24h":1000,"requestBudget30d":300,"tokenBudget30d":30000}`,
			serviceErr: admin.ErrInvalidInput,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "not found",
			path:       "/api/admin/keys/99/budgets",
			body:       `{"requestBudget24h":10,"tokenBudget24h":1000,"requestBudget30d":300,"tokenBudget30d":30000}`,
			serviceErr: admin.ErrNotFound,
			wantStatus: http.StatusNotFound,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			admins := newFakeAdminService()
			admins.budgetsErr = tc.serviceErr
			server := NewServer(config.Config{}, staticHealth{}, admins, nil)
			req := httptest.NewRequest(http.MethodPut, tc.path, strings.NewReader(tc.body))
			req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != tc.wantStatus {
				t.Fatalf("status = %d body=%s, want %d", recorder.Code, recorder.Body.String(), tc.wantStatus)
			}
		})
	}
}

func TestRoutingPoolsEndpoints(t *testing.T) {
	admins := newFakeAdminService()
	server := NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService())
	req := httptest.NewRequest(http.MethodPost, "/api/admin/routing-pools", strings.NewReader(`{"name":"primary plus","description":"daily","enabled":true,"fallbackPoolId":4}`))
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s, want 201", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Pool admin.RoutingPool `json:"pool"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Pool.Name != "primary plus" || !body.Pool.Enabled {
		t.Fatalf("pool = %+v, want primary plus enabled", body.Pool)
	}
	if admins.createFallbackID == nil || *admins.createFallbackID != 4 || body.Pool.FallbackPoolID == nil || *body.Pool.FallbackPoolID != 4 {
		t.Fatalf("fallback pool = service:%v body:%v, want 4", admins.createFallbackID, body.Pool.FallbackPoolID)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/routing-pools", nil)
	listReq.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	listRecorder := httptest.NewRecorder()
	server.ServeHTTP(listRecorder, listReq)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s, want 200", listRecorder.Code, listRecorder.Body.String())
	}
	var listBody struct {
		Pools []admin.RoutingPool `json:"pools"`
	}
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("decode list body: %v", err)
	}
	if len(listBody.Pools) != 3 {
		t.Fatalf("pools = %+v, want initial pools plus created pool", listBody.Pools)
	}
}

func TestRoutingPoolUpdateEndpointClearsFallback(t *testing.T) {
	admins := newFakeAdminService()
	poolID := int64(4)
	admins.routingPools[0].FallbackPoolID = &poolID
	server := NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService())
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/routing-pools/3", strings.NewReader(`{"name":"primary","description":"daily","enabled":true,"fallbackPoolId":null}`))
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if admins.updateFallbackID != nil {
		t.Fatalf("update fallback id = %v, want nil", admins.updateFallbackID)
	}
	var body struct {
		Pool admin.RoutingPool `json:"pool"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Pool.FallbackPoolID != nil {
		t.Fatalf("pool fallback id = %v, want nil", body.Pool.FallbackPoolID)
	}
}

func TestUpdateAPIKeyRoutingPoolEndpoint(t *testing.T) {
	admins := newFakeAdminService()
	server := NewServer(config.Config{}, staticHealth{}, admins, nil)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/keys/7/routing-pool", strings.NewReader(`{"routingPoolId":3}`))
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if admins.routingPoolKeyID != 7 || admins.routingPoolID == nil || *admins.routingPoolID != 3 {
		t.Fatalf("recorded key pool = id:%d pool:%v, want 7/3", admins.routingPoolKeyID, admins.routingPoolID)
	}
	var body struct {
		Key admin.APIKey `json:"key"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Key.RoutingPoolID == nil || *body.Key.RoutingPoolID != 3 || body.Key.RoutingPoolName != "primary" {
		t.Fatalf("key = %+v, want primary routing pool", body.Key)
	}
}

func TestProviderStatusRequiresSessionAndReturnsStatus(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/providers/openai", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/providers/openai", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder = httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body provider.Status
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !body.Configured || !body.Connected || body.DisplayName != "Codex Account" {
		t.Fatalf("provider status = %+v", body)
	}
}

func TestUnifiedProviderAccountCodexOAuthStatusReturnsStatus(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	req := httptest.NewRequest(http.MethodGet, "/api/admin/provider-accounts/codex-oauth/status", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body provider.Status
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !body.Configured || !body.Connected || body.DisplayName != "Codex Account" {
		t.Fatalf("provider status = %+v", body)
	}
}

func TestProviderConnectReturnsAuthorizationURL(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	req := httptest.NewRequest(http.MethodPost, "/api/admin/providers/openai/connect", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body struct {
		AuthorizationURL string `json:"authorizationUrl"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.AuthorizationURL == "" {
		t.Fatal("authorizationUrl is empty")
	}
}

func TestProviderConnectAcceptsAccountOptionsAndFingerprint(t *testing.T) {
	providers := newFakeProviderService()
	server := NewServer(config.Config{TrustedProxyCIDRs: []netip.Prefix{
		netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("198.51.100.0/24"),
	}}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/providers/openai/connect", strings.NewReader(`{"name":"Work Codex","priority":7,"enabled":false,"targetAccountId":42,"fingerprint":"browser-fp","fingerprintProfileId":9}`))
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("X-Forwarded-For", "203.0.113.10, 198.51.100.2")
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if providers.connectOptions.RedirectAfter != "/" ||
		providers.connectOptions.Name != "Work Codex" ||
		providers.connectOptions.Priority != 7 ||
		providers.connectOptions.Enabled == nil ||
		*providers.connectOptions.Enabled ||
		providers.connectOptions.TargetAccountID != 42 ||
		providers.connectOptions.FingerprintProfileID == nil ||
		*providers.connectOptions.FingerprintProfileID != 9 {
		t.Fatalf("connectOptions = %+v", providers.connectOptions)
	}
	if providers.connectOptions.Fingerprint.Value != "browser-fp" ||
		providers.connectOptions.Fingerprint.UserAgent != "Mozilla/5.0" ||
		providers.connectOptions.Fingerprint.IP != "203.0.113.10" {
		t.Fatalf("fingerprint = %+v", providers.connectOptions.Fingerprint)
	}
}

func TestUnifiedProviderAccountCodexOAuthConnectDelegatesToProviderConnect(t *testing.T) {
	providers := newFakeProviderService()
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/codex-oauth/connect", strings.NewReader(`{"name":"Work Codex","priority":7,"enabled":false,"targetAccountId":42,"fingerprint":"browser-fp","fingerprintProfileId":9}`))
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("X-Forwarded-For", "203.0.113.10, 198.51.100.2")
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body struct {
		AuthorizationURL string `json:"authorizationUrl"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.AuthorizationURL == "" {
		t.Fatal("authorizationUrl is empty")
	}
	if providers.connectOptions.Name != "Work Codex" ||
		providers.connectOptions.Priority != 7 ||
		providers.connectOptions.Enabled == nil ||
		*providers.connectOptions.Enabled ||
		providers.connectOptions.TargetAccountID != 42 ||
		providers.connectOptions.FingerprintProfileID == nil ||
		*providers.connectOptions.FingerprintProfileID != 9 {
		t.Fatalf("connectOptions = %+v", providers.connectOptions)
	}
}
func TestAdminProviderConnectTreatsZeroFingerprintProfileAsDefaultSentinel(t *testing.T) {
	providers := newFakeProviderService()
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/providers/openai/connect", strings.NewReader("{\"name\":\"Zero FP\",\"fingerprintProfileId\":0}"))
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("X-Forwarded-For", "203.0.113.10, 198.51.100.2")
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if providers.connectOptions.FingerprintProfileID != nil {
		t.Fatalf("fingerprintProfileId = %v, want nil (resolved to default sentinel)", *providers.connectOptions.FingerprintProfileID)
	}
}
func TestProviderConnectReturnsConflictWhenUnconfigured(t *testing.T) {
	providers := newFakeProviderService()
	providers.status.Configured = false
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/providers/openai/connect", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", recorder.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "provider_not_configured" {
		t.Fatalf("error = %q, want provider_not_configured", body["error"])
	}
}

func TestProviderDisconnectReturnsNoContent(t *testing.T) {
	providers := newFakeProviderService()
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/providers/openai/disconnect", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", recorder.Code)
	}
	if !providers.disconnected {
		t.Fatal("provider service was not disconnected")
	}
}

func TestAdminProviderAccountsRequireSession(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	req := httptest.NewRequest(http.MethodGet, "/api/admin/providers/openai/accounts", nil)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestAdminProviderAccountsEndpointsRequireSession(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "list", method: http.MethodGet, path: "/api/admin/provider-accounts"},
		{name: "create api upstream", method: http.MethodPost, path: "/api/admin/provider-accounts/api-upstream", body: `{"name":"Upstream","baseUrl":"https://upstream.example.test","apiKey":"secret"}`},
		{name: "test all", method: http.MethodPost, path: "/api/admin/provider-accounts/test"},
		{name: "connect codex oauth", method: http.MethodPost, path: "/api/admin/provider-accounts/codex-oauth/connect", body: `{"name":"Work Codex"}`},
		{name: "patch", method: http.MethodPatch, path: "/api/admin/provider-accounts/7", body: `{"enabled":true}`},
		{name: "disconnect", method: http.MethodPost, path: "/api/admin/provider-accounts/7/disconnect"},
		{name: "test", method: http.MethodPost, path: "/api/admin/provider-accounts/7/test"},
		{name: "pause", method: http.MethodPost, path: "/api/admin/provider-accounts/7/pause", body: `{"durationSeconds":300}`},
		{name: "reset status", method: http.MethodPost, path: "/api/admin/provider-accounts/7/reset-status"},
		{name: "list test results", method: http.MethodGet, path: "/api/admin/provider-accounts/7/test-results?limit=2"},
		{name: "list models", method: http.MethodGet, path: "/api/admin/provider-accounts/7/models"},
		{name: "replace models", method: http.MethodPut, path: "/api/admin/provider-accounts/7/models", body: `{"models":[{"model":"gpt-5","enabled":true}]}`},
		{name: "test model", method: http.MethodPost, path: "/api/admin/provider-accounts/7/model-tests", body: `{"model":"gpt-5"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", recorder.Code)
			}
		})
	}
}

func TestAdminProviderAccountsEndpointsRequireProviderService(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil)
	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "list", method: http.MethodGet, path: "/api/admin/provider-accounts"},
		{name: "create api upstream", method: http.MethodPost, path: "/api/admin/provider-accounts/api-upstream", body: `{"name":"Upstream","baseUrl":"https://upstream.example.test","apiKey":"secret"}`},
		{name: "test all", method: http.MethodPost, path: "/api/admin/provider-accounts/test"},
		{name: "connect codex oauth", method: http.MethodPost, path: "/api/admin/provider-accounts/codex-oauth/connect", body: `{"name":"Work Codex"}`},
		{name: "patch", method: http.MethodPatch, path: "/api/admin/provider-accounts/7", body: `{"enabled":true}`},
		{name: "disconnect", method: http.MethodPost, path: "/api/admin/provider-accounts/7/disconnect"},
		{name: "test", method: http.MethodPost, path: "/api/admin/provider-accounts/7/test"},
		{name: "pause", method: http.MethodPost, path: "/api/admin/provider-accounts/7/pause", body: `{"durationSeconds":300}`},
		{name: "reset status", method: http.MethodPost, path: "/api/admin/provider-accounts/7/reset-status"},
		{name: "list test results", method: http.MethodGet, path: "/api/admin/provider-accounts/7/test-results?limit=2"},
		{name: "list models", method: http.MethodGet, path: "/api/admin/provider-accounts/7/models"},
		{name: "replace models", method: http.MethodPut, path: "/api/admin/provider-accounts/7/models", body: `{"models":[{"model":"gpt-5","enabled":true}]}`},
		{name: "test model", method: http.MethodPost, path: "/api/admin/provider-accounts/7/model-tests", body: `{"model":"gpt-5"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503", recorder.Code)
			}
			var body map[string]string
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["error"] != "service_unavailable" {
				t.Fatalf("error = %q, want service_unavailable", body["error"])
			}
		})
	}
}

func TestCreateAPIUpstreamAccount(t *testing.T) {
	providers := newFakeProviderService()
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/api-upstream", strings.NewReader(`{"name":" Upstream ","baseUrl":"https://upstream.example.test/v1/","apiKey":" secret ","proxyUrl":" http://proxy-user:proxy-pass@proxy.example.test:8080 ","enabled":true,"priority":8,"fingerprintProfileId":9,"models":[" gpt-5 ","gpt-4.1"]}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s, want 201", recorder.Code, recorder.Body.String())
	}
	if providers.createdAPIUpstream.Name != " Upstream " || providers.createdAPIUpstream.BaseURL != "https://upstream.example.test/v1/" || providers.createdAPIUpstream.APIKey != " secret " {
		t.Fatalf("created input = %+v", providers.createdAPIUpstream)
	}
	if providers.createdAPIUpstream.ProxyURL != " http://proxy-user:proxy-pass@proxy.example.test:8080 " {
		t.Fatalf("created proxy URL = %q", providers.createdAPIUpstream.ProxyURL)
	}
	if providers.createdAPIUpstream.Enabled == nil || !*providers.createdAPIUpstream.Enabled || providers.createdAPIUpstream.Priority != 8 || len(providers.createdAPIUpstream.Models) != 2 {
		t.Fatalf("created input scheduling/models = %+v", providers.createdAPIUpstream)
	}
	if providers.createdAPIUpstream.FingerprintProfileID == nil || *providers.createdAPIUpstream.FingerprintProfileID != 9 {
		t.Fatalf("created input fingerprint profile = %+v, want 9", providers.createdAPIUpstream.FingerprintProfileID)
	}
	var body struct {
		Account provider.Account `json:"account"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Account.ID == 0 || body.Account.AccountType != provider.AccountTypeAPIUpstream {
		t.Fatalf("account = %+v", body.Account)
	}
	if strings.Contains(recorder.Body.String(), "secret") {
		t.Fatalf("response leaked api key: %s", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "proxy-pass") {
		t.Fatalf("response leaked proxy credential: %s", recorder.Body.String())
	}
}

func TestCreateAPIUpstreamAccountDefaultsEnabledWhenOmitted(t *testing.T) {
	providers := newFakeProviderService()
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/api-upstream", strings.NewReader(`{"name":"Upstream","baseUrl":"https://upstream.example.test","apiKey":"secret"}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s, want 201", recorder.Code, recorder.Body.String())
	}
	if providers.createdAPIUpstream.Enabled != nil {
		t.Fatalf("created input enabled = %v, want omitted enabled to remain nil for service defaulting", *providers.createdAPIUpstream.Enabled)
	}
	var body struct {
		Account provider.Account `json:"account"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !body.Account.Enabled {
		t.Fatalf("response account enabled = false, want service default true")
	}
}

func TestCreateAPIUpstreamAccountPreservesExplicitDisabled(t *testing.T) {
	providers := newFakeProviderService()
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/api-upstream", strings.NewReader(`{"name":"Upstream","baseUrl":"https://upstream.example.test","apiKey":"secret","enabled":false}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s, want 201", recorder.Code, recorder.Body.String())
	}
	if providers.createdAPIUpstream.Enabled == nil || *providers.createdAPIUpstream.Enabled {
		t.Fatalf("created input enabled = %+v, want explicit false to be preserved", providers.createdAPIUpstream.Enabled)
	}
}

func TestCreateAPIUpstreamAccountMapsErrors(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/api-upstream", strings.NewReader(`{"name":"","baseUrl":"https://upstream.example.test","apiKey":"secret"}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", recorder.Code, recorder.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "invalid_input" {
		t.Fatalf("error = %q, want invalid_input", body["error"])
	}
}

func TestAdminProviderAccountMutationsRequireSession(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "patch", method: http.MethodPatch, path: "/api/admin/providers/openai/accounts/7", body: `{"enabled":true}`},
		{name: "refresh", method: http.MethodPost, path: "/api/admin/providers/openai/accounts/7/refresh"},
		{name: "disconnect", method: http.MethodPost, path: "/api/admin/providers/openai/accounts/7/disconnect"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", recorder.Code)
			}
		})
	}
}

func TestAdminCanListUnifiedProviderAccounts(t *testing.T) {
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true, Priority: 10}}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/provider-accounts", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"id":7`) {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestAdminProviderAccountsIncludeConcurrencyState(t *testing.T) {
	admins := newFakeAdminService()
	admins.gatewaySettings.MaxConcurrentRequestsPerAccount = 5
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{
		{ID: 7, Provider: "openai", DisplayName: "Busy", Enabled: true, Priority: 10, MaxConcurrentRequests: 3},
		{ID: 8, Provider: "openai", DisplayName: "Inherited", Enabled: true, Priority: 10},
	}
	gateway := &fakeGatewayHandler{accountConcurrency: map[int64]int{7: 2, 8: 1}}
	server := NewServer(config.Config{}, staticHealth{}, admins, providers, gateway)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/provider-accounts", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Accounts []struct {
			provider.Account
			CurrentConcurrentRequests      int `json:"currentConcurrentRequests"`
			EffectiveMaxConcurrentRequests int `json:"effectiveMaxConcurrentRequests"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Accounts) != 2 {
		t.Fatalf("accounts = %+v, want two accounts", body.Accounts)
	}
	if body.Accounts[0].CurrentConcurrentRequests != 2 || body.Accounts[0].EffectiveMaxConcurrentRequests != 3 {
		t.Fatalf("first concurrency fields = %+v, want current 2 effective 3", body.Accounts[0])
	}
	if body.Accounts[1].CurrentConcurrentRequests != 1 || body.Accounts[1].EffectiveMaxConcurrentRequests != 5 {
		t.Fatalf("second concurrency fields = %+v, want current 1 effective 5", body.Accounts[1])
	}
}

func TestAdminCanUpdateUnifiedProviderAccount(t *testing.T) {
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true, Priority: 10, LoadFactor: 1}}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/provider-accounts/7", strings.NewReader(`{"name":" Renamed ","enabled":false,"priority":2,"loadFactor":5,"maxConcurrentRequests":3}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Account provider.Account `json:"account"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Account.ID != 7 || body.Account.Name != "Renamed" || body.Account.Enabled || body.Account.Priority != 2 || body.Account.LoadFactor != 5 || body.Account.MaxConcurrentRequests != 3 {
		t.Fatalf("account = %+v, want renamed disabled account 7 priority 2 load factor 5 max concurrency 3", body.Account)
	}
	if providers.lastAccountUpdate.LoadFactor == nil || *providers.lastAccountUpdate.LoadFactor != 5 {
		t.Fatalf("load factor update = %+v, want 5", providers.lastAccountUpdate.LoadFactor)
	}
	if providers.lastAccountUpdate.MaxConcurrentRequests == nil || *providers.lastAccountUpdate.MaxConcurrentRequests != 3 {
		t.Fatalf("max concurrency update = %+v, want 3", providers.lastAccountUpdate.MaxConcurrentRequests)
	}
}

func TestAdminCanClearProviderAccountFingerprintProfile(t *testing.T) {
	profileID := int64(42)
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true, Priority: 10, LoadFactor: 1, FingerprintProfileID: &profileID}}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/provider-accounts/7", strings.NewReader(`{"fingerprintProfileId":null}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Account provider.Account `json:"account"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Account.FingerprintProfileID != nil {
		t.Fatalf("fingerprintProfileId = %v, want cleared nil", *body.Account.FingerprintProfileID)
	}
	if !providers.lastAccountUpdate.FingerprintProfileIDSet || providers.lastAccountUpdate.FingerprintProfileID != nil {
		t.Fatalf("fingerprint update = set %v value %+v, want explicit clear", providers.lastAccountUpdate.FingerprintProfileIDSet, providers.lastAccountUpdate.FingerprintProfileID)
	}
}

func TestAdminCanBulkDisableUnifiedProviderAccounts(t *testing.T) {
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{
		{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true, Priority: 10, LoadFactor: 1},
		{ID: 8, Provider: "openai", DisplayName: "Account B", Enabled: true, Priority: 20, LoadFactor: 1},
	}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/bulk-update", strings.NewReader(`{"accountIds":[7,8,7],"enabled":false}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if !reflect.DeepEqual(providers.accountUpdateIDs, []int64{7, 8}) {
		t.Fatalf("updated ids = %+v, want [7 8]", providers.accountUpdateIDs)
	}
	for index, update := range providers.accountUpdates {
		if update.Enabled == nil || *update.Enabled {
			t.Fatalf("update %d enabled = %+v, want false", index, update.Enabled)
		}
	}
	var body struct {
		Accounts []provider.Account `json:"accounts"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Accounts) != 2 || body.Accounts[0].Enabled || body.Accounts[1].Enabled {
		t.Fatalf("accounts = %+v, want two disabled accounts", body.Accounts)
	}
}

func TestAdminCanBulkUpdateUnifiedProviderAccountScheduling(t *testing.T) {
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{
		{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true, Priority: 10, LoadFactor: 1},
		{ID: 8, Provider: "openai", DisplayName: "Account B", Enabled: true, Priority: 20, LoadFactor: 1},
	}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/bulk-update", strings.NewReader(`{"accountIds":[7,8,7],"priority":2,"loadFactor":5,"maxConcurrentRequests":3}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if !reflect.DeepEqual(providers.accountUpdateIDs, []int64{7, 8}) {
		t.Fatalf("updated ids = %+v, want [7 8]", providers.accountUpdateIDs)
	}
	for index, update := range providers.accountUpdates {
		if update.Priority == nil || *update.Priority != 2 {
			t.Fatalf("update %d priority = %+v, want 2", index, update.Priority)
		}
		if update.LoadFactor == nil || *update.LoadFactor != 5 {
			t.Fatalf("update %d load factor = %+v, want 5", index, update.LoadFactor)
		}
		if update.MaxConcurrentRequests == nil || *update.MaxConcurrentRequests != 3 {
			t.Fatalf("update %d max concurrency = %+v, want 3", index, update.MaxConcurrentRequests)
		}
	}
	var body struct {
		Accounts []provider.Account `json:"accounts"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Accounts) != 2 || body.Accounts[0].Priority != 2 || body.Accounts[1].LoadFactor != 5 || body.Accounts[0].MaxConcurrentRequests != 3 || body.Accounts[1].MaxConcurrentRequests != 3 {
		t.Fatalf("accounts = %+v, want two accounts with priority 2 load factor 5 max concurrency 3", body.Accounts)
	}
}

func TestAdminBulkProviderAccountUpdateValidatesInput(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{name: "empty ids", body: `{"accountIds":[],"enabled":false}`},
		{name: "bad id", body: `{"accountIds":[0],"enabled":false}`},
		{name: "missing enabled", body: `{"accountIds":[7]}`},
		{name: "too many ids", body: `{"accountIds":[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33,34,35,36,37,38,39,40,41,42,43,44,45,46,47,48,49,50,51,52,53,54,55,56,57,58,59,60,61,62,63,64,65,66,67,68,69,70,71,72,73,74,75,76,77,78,79,80,81,82,83,84,85,86,87,88,89,90,91,92,93,94,95,96,97,98,99,100,101],"enabled":false}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			providers := newFakeProviderService()
			providers.accounts = []provider.Account{{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true}}
			server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
			req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/bulk-update", strings.NewReader(tc.body))
			req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s, want 400", recorder.Code, recorder.Body.String())
			}
			if len(providers.accountUpdateIDs) != 0 {
				t.Fatalf("updated ids = %+v, want no updates", providers.accountUpdateIDs)
			}
		})
	}
}

func TestAdminCanTestSelectedUnifiedProviderAccounts(t *testing.T) {
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{
		{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true, Priority: 10, LoadFactor: 1, Status: provider.AccountStatusCircuitOpen, LastError: "paused"},
		{ID: 8, Provider: "openai", DisplayName: "Account B", Enabled: true, Priority: 20, LoadFactor: 1, Status: provider.AccountStatusRateLimited, LastError: "rate limited"},
	}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/bulk-test", strings.NewReader(`{"accountIds":[7,8,7]}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if !reflect.DeepEqual(providers.testedAccountIDs, []int64{7, 8}) {
		t.Fatalf("tested ids = %+v, want [7 8]", providers.testedAccountIDs)
	}
	var body struct {
		Accounts []provider.Account `json:"accounts"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Accounts) != 2 || body.Accounts[0].Status != provider.AccountStatusActive || body.Accounts[1].Status != provider.AccountStatusActive {
		t.Fatalf("accounts = %+v, want two tested active accounts", body.Accounts)
	}
}

func TestAdminBulkProviderAccountTestValidatesInput(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{name: "empty ids", body: `{"accountIds":[]}`},
		{name: "bad id", body: `{"accountIds":[0]}`},
		{name: "too many ids", body: `{"accountIds":[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33,34,35,36,37,38,39,40,41,42,43,44,45,46,47,48,49,50,51,52,53,54,55,56,57,58,59,60,61,62,63,64,65,66,67,68,69,70,71,72,73,74,75,76,77,78,79,80,81,82,83,84,85,86,87,88,89,90,91,92,93,94,95,96,97,98,99,100,101]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			providers := newFakeProviderService()
			providers.accounts = []provider.Account{{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true}}
			server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
			req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/bulk-test", strings.NewReader(tc.body))
			req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s, want 400", recorder.Code, recorder.Body.String())
			}
			if len(providers.testedAccountIDs) != 0 {
				t.Fatalf("tested ids = %+v, want no tests", providers.testedAccountIDs)
			}
		})
	}
}

func TestAdminCanBulkPauseUnifiedProviderAccountScheduling(t *testing.T) {
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{
		{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true, Status: provider.AccountStatusActive},
		{ID: 8, Provider: "openai", DisplayName: "Account B", Enabled: true, Status: provider.AccountStatusActive},
	}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/bulk-pause", strings.NewReader(`{"accountIds":[7,8,7],"durationSeconds":600}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if !reflect.DeepEqual(providers.pausedAccountIDs, []int64{7, 8}) {
		t.Fatalf("paused ids = %+v, want [7 8]", providers.pausedAccountIDs)
	}
	if providers.pauseDuration != 10*time.Minute {
		t.Fatalf("pause duration = %s, want 10m", providers.pauseDuration)
	}
	var body struct {
		Accounts []provider.Account `json:"accounts"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Accounts) != 2 || body.Accounts[0].Status != provider.AccountStatusCircuitOpen || body.Accounts[1].Status != provider.AccountStatusCircuitOpen {
		t.Fatalf("accounts = %+v, want two paused accounts", body.Accounts)
	}
}

func TestAdminBulkProviderAccountPauseValidatesInput(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{name: "empty ids", body: `{"accountIds":[],"durationSeconds":300}`},
		{name: "bad id", body: `{"accountIds":[0],"durationSeconds":300}`},
		{name: "bad duration", body: `{"accountIds":[7],"durationSeconds":0}`},
		{name: "too many ids", body: `{"accountIds":[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33,34,35,36,37,38,39,40,41,42,43,44,45,46,47,48,49,50,51,52,53,54,55,56,57,58,59,60,61,62,63,64,65,66,67,68,69,70,71,72,73,74,75,76,77,78,79,80,81,82,83,84,85,86,87,88,89,90,91,92,93,94,95,96,97,98,99,100,101],"durationSeconds":300}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			providers := newFakeProviderService()
			providers.accounts = []provider.Account{{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true}}
			server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
			req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/bulk-pause", strings.NewReader(tc.body))
			req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s, want 400", recorder.Code, recorder.Body.String())
			}
			if len(providers.pausedAccountIDs) != 0 {
				t.Fatalf("paused ids = %+v, want no pauses", providers.pausedAccountIDs)
			}
		})
	}
}

func TestAdminCanBulkResetUnifiedProviderAccountStatus(t *testing.T) {
	future := time.Now().Add(time.Hour)
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{
		{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true, Status: provider.AccountStatusRateLimited, RateLimitedUntil: &future, CircuitOpenUntil: &future, FailureCount: 2},
		{ID: 8, Provider: "openai", DisplayName: "Account B", Enabled: true, Status: provider.AccountStatusCircuitOpen, RateLimitedUntil: &future, CircuitOpenUntil: &future, FailureCount: 3},
	}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/bulk-reset-status", strings.NewReader(`{"accountIds":[7,8,7]}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if !reflect.DeepEqual(providers.resetStatusAccountIDs, []int64{7, 8}) {
		t.Fatalf("reset ids = %+v, want [7 8]", providers.resetStatusAccountIDs)
	}
	var body struct {
		Accounts []provider.Account `json:"accounts"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Accounts) != 2 || body.Accounts[0].Status != provider.AccountStatusActive || body.Accounts[1].FailureCount != 0 {
		t.Fatalf("accounts = %+v, want two reset active accounts", body.Accounts)
	}
}

func TestAdminBulkProviderAccountResetStatusValidatesInput(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{name: "empty ids", body: `{"accountIds":[]}`},
		{name: "bad id", body: `{"accountIds":[0]}`},
		{name: "too many ids", body: `{"accountIds":[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33,34,35,36,37,38,39,40,41,42,43,44,45,46,47,48,49,50,51,52,53,54,55,56,57,58,59,60,61,62,63,64,65,66,67,68,69,70,71,72,73,74,75,76,77,78,79,80,81,82,83,84,85,86,87,88,89,90,91,92,93,94,95,96,97,98,99,100,101]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			providers := newFakeProviderService()
			providers.accounts = []provider.Account{{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true}}
			server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
			req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/bulk-reset-status", strings.NewReader(tc.body))
			req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s, want 400", recorder.Code, recorder.Body.String())
			}
			if len(providers.resetStatusAccountIDs) != 0 {
				t.Fatalf("reset ids = %+v, want no resets", providers.resetStatusAccountIDs)
			}
		})
	}
}

func TestAdminCanBulkRefreshUnifiedProviderAccounts(t *testing.T) {
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{
		{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true, Status: provider.AccountStatusCircuitOpen},
		{ID: 8, Provider: "openai", DisplayName: "Account B", Enabled: true, Status: provider.AccountStatusExpired},
	}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/bulk-refresh", strings.NewReader(`{"accountIds":[7,8,7]}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if !reflect.DeepEqual(providers.refreshedAccountIDs, []int64{7, 8}) {
		t.Fatalf("refresh ids = %+v, want [7 8]", providers.refreshedAccountIDs)
	}
	var body struct {
		Accounts []provider.Account `json:"accounts"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Accounts) != 2 || body.Accounts[0].ID != 7 || body.Accounts[1].ID != 8 {
		t.Fatalf("accounts = %+v, want entries for accounts 7 and 8", body.Accounts)
	}
	if body.Accounts[0].Status != provider.AccountStatusActive || body.Accounts[0].LastRefreshAt == nil || body.Accounts[1].LastRefreshAt == nil {
		t.Fatalf("accounts = %+v, want refreshed active accounts", body.Accounts)
	}
}

func TestAdminBulkProviderAccountRefreshValidatesInput(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{name: "empty ids", body: `{"accountIds":[]}`},
		{name: "bad id", body: `{"accountIds":[0]}`},
		{name: "too many ids", body: `{"accountIds":[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33,34,35,36,37,38,39,40,41,42,43,44,45,46,47,48,49,50,51,52,53,54,55,56,57,58,59,60,61,62,63,64,65,66,67,68,69,70,71,72,73,74,75,76,77,78,79,80,81,82,83,84,85,86,87,88,89,90,91,92,93,94,95,96,97,98,99,100,101]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			providers := newFakeProviderService()
			providers.accounts = []provider.Account{{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true}}
			server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
			req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/bulk-refresh", strings.NewReader(tc.body))
			req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s, want 400", recorder.Code, recorder.Body.String())
			}
			if len(providers.refreshedAccountIDs) != 0 {
				t.Fatalf("refresh ids = %+v, want no refreshes", providers.refreshedAccountIDs)
			}
		})
	}
}

func TestAdminCanBulkDisconnectUnifiedProviderAccounts(t *testing.T) {
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{
		{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true},
		{ID: 8, Provider: "openai", DisplayName: "Account B", Enabled: true},
	}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/bulk-disconnect", strings.NewReader(`{"accountIds":[7,8,7]}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d body=%s, want 204", recorder.Code, recorder.Body.String())
	}
	if !reflect.DeepEqual(providers.disconnectedAccountIDs, []int64{7, 8}) {
		t.Fatalf("disconnect ids = %+v, want [7 8]", providers.disconnectedAccountIDs)
	}
	if len(providers.accounts) != 0 {
		t.Fatalf("accounts = %+v, want all selected accounts removed", providers.accounts)
	}
}

func TestAdminBulkProviderAccountDisconnectValidatesInput(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{name: "empty ids", body: `{"accountIds":[]}`},
		{name: "bad id", body: `{"accountIds":[0]}`},
		{name: "too many ids", body: `{"accountIds":[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33,34,35,36,37,38,39,40,41,42,43,44,45,46,47,48,49,50,51,52,53,54,55,56,57,58,59,60,61,62,63,64,65,66,67,68,69,70,71,72,73,74,75,76,77,78,79,80,81,82,83,84,85,86,87,88,89,90,91,92,93,94,95,96,97,98,99,100,101]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			providers := newFakeProviderService()
			providers.accounts = []provider.Account{{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true}}
			server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
			req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/bulk-disconnect", strings.NewReader(tc.body))
			req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s, want 400", recorder.Code, recorder.Body.String())
			}
			if len(providers.disconnectedAccountIDs) != 0 {
				t.Fatalf("disconnect ids = %+v, want no disconnects", providers.disconnectedAccountIDs)
			}
		})
	}
}

func TestAdminCanBulkReplaceUnifiedProviderAccountModels(t *testing.T) {
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{
		{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true},
		{ID: 8, Provider: "openai", DisplayName: "Account B", Enabled: true},
	}
	providers.accountModels[7] = []provider.AccountModel{{AccountID: 7, Provider: "openai", Model: "old-model", Enabled: true}}
	providers.accountModels[8] = []provider.AccountModel{{AccountID: 8, Provider: "openai", Model: "old-model", Enabled: true}}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/bulk-models", strings.NewReader(`{"accountIds":[7,8,7],"models":[{"model":"gpt-5","enabled":true},{"model":"codex-mini","enabled":true}]}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if !reflect.DeepEqual(providers.replacedModelIDs, []int64{7, 8}) {
		t.Fatalf("replaced model ids = %+v, want [7 8]", providers.replacedModelIDs)
	}
	for index, models := range providers.replacedModels {
		if len(models) != 2 || models[0].Model != "gpt-5" || !models[0].Enabled || models[1].Model != "codex-mini" || !models[1].Enabled {
			t.Fatalf("replace payload %d = %+v, want gpt-5 and codex-mini enabled", index, models)
		}
	}
	var body struct {
		Accounts []struct {
			AccountID int64                   `json:"accountId"`
			Models    []provider.AccountModel `json:"models"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Accounts) != 2 || body.Accounts[0].AccountID != 7 || body.Accounts[1].AccountID != 8 {
		t.Fatalf("accounts = %+v, want entries for account 7 and 8", body.Accounts)
	}
	if len(body.Accounts[0].Models) != 2 || body.Accounts[0].Models[0].Model != "gpt-5" {
		t.Fatalf("account 7 models = %+v, want replaced models", body.Accounts[0].Models)
	}
}

func TestAdminBulkProviderAccountModelsValidatesInput(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{name: "empty ids", body: `{"accountIds":[],"models":[{"model":"gpt-5","enabled":true}]}`},
		{name: "bad id", body: `{"accountIds":[0],"models":[{"model":"gpt-5","enabled":true}]}`},
		{name: "empty models", body: `{"accountIds":[7],"models":[]}`},
		{name: "too many ids", body: `{"accountIds":[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33,34,35,36,37,38,39,40,41,42,43,44,45,46,47,48,49,50,51,52,53,54,55,56,57,58,59,60,61,62,63,64,65,66,67,68,69,70,71,72,73,74,75,76,77,78,79,80,81,82,83,84,85,86,87,88,89,90,91,92,93,94,95,96,97,98,99,100,101],"models":[{"model":"gpt-5","enabled":true}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			providers := newFakeProviderService()
			providers.accounts = []provider.Account{{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true}}
			providers.accountModels[7] = []provider.AccountModel{{AccountID: 7, Provider: "openai", Model: "old-model", Enabled: true}}
			server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
			req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/bulk-models", strings.NewReader(tc.body))
			req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s, want 400", recorder.Code, recorder.Body.String())
			}
			if len(providers.replacedModelIDs) != 0 {
				t.Fatalf("replaced model ids = %+v, want no replacements", providers.replacedModelIDs)
			}
		})
	}
}

func TestAdminCanUpdateAPIUpstreamCredentialAndProxy(t *testing.T) {
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{{
		ID:          7,
		Provider:    "openai",
		AccountType: provider.AccountTypeAPIUpstream,
		DisplayName: "API Upstream",
		Enabled:     true,
		Priority:    10,
		Status:      provider.AccountStatusCircuitOpen,
		LastError:   "old upstream credential failed",
		Credential:  provider.AccountCredential{BaseURL: "https://old.example.test"},
	}}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/provider-accounts/7", strings.NewReader(`{"baseUrl":" https://new.example.test/v1 ","apiKey":" new-secret ","proxyUrl":"https://proxy.example.test:8443"}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if providers.lastAccountUpdate.APIUpstreamBaseURL == nil || *providers.lastAccountUpdate.APIUpstreamBaseURL != " https://new.example.test/v1 " {
		t.Fatalf("base URL update = %+v", providers.lastAccountUpdate.APIUpstreamBaseURL)
	}
	if providers.lastAccountUpdate.APIUpstreamAPIKey == nil || *providers.lastAccountUpdate.APIUpstreamAPIKey != " new-secret " {
		t.Fatalf("API key update = %+v", providers.lastAccountUpdate.APIUpstreamAPIKey)
	}
	if providers.lastAccountUpdate.ProxyURL == nil || *providers.lastAccountUpdate.ProxyURL != "https://proxy.example.test:8443" {
		t.Fatalf("proxy URL update = %+v", providers.lastAccountUpdate.ProxyURL)
	}
	var body struct {
		Account provider.Account `json:"account"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Account.BaseURL != "https://new.example.test/v1" {
		t.Fatalf("account base URL = %q, want updated base URL", body.Account.BaseURL)
	}
	if body.Account.Status != provider.AccountStatusActive || body.Account.LastError != "" {
		t.Fatalf("account status = %+v, want local failure state cleared", body.Account)
	}
}

func TestAdminCanListProviderAccounts(t *testing.T) {
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true, Priority: 10}}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/providers/openai/accounts", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"id":7`) {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func TestAdminCanUpdateProviderAccount(t *testing.T) {
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true, Priority: 10}}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/providers/openai/accounts/7", strings.NewReader(`{"enabled":false,"priority":2}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Account provider.Account `json:"account"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Account.ID != 7 || body.Account.Enabled || body.Account.Priority != 2 {
		t.Fatalf("account = %+v, want disabled account 7 priority 2", body.Account)
	}
}

func TestAdminUpdateProviderAccountRejectsEmptyPatch(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/providers/openai/accounts/7", strings.NewReader(`{}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "invalid_input" {
		t.Fatalf("error = %q, want invalid_input", body["error"])
	}
}

func TestAdminUpdateProviderAccountRejectsInvalidID(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/providers/openai/accounts/not-an-id", strings.NewReader(`{"priority":1}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "bad_request" {
		t.Fatalf("error = %q, want bad_request", body["error"])
	}
}

func TestAdminUpdateProviderAccountRejectsNegativePriority(t *testing.T) {
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true, Priority: 10}}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/providers/openai/accounts/7", strings.NewReader(`{"priority":-1}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "invalid_input" {
		t.Fatalf("error = %q, want invalid_input", body["error"])
	}
}

func TestAdminUpdateProviderAccountRejectsUnknownJSONField(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/providers/openai/accounts/7", strings.NewReader(`{"priority":1,"extra":true}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "bad_request" {
		t.Fatalf("error = %q, want bad_request", body["error"])
	}
}

func TestAdminUpdateProviderAccountMapsErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
		code string
	}{
		{name: "invalid input", err: provider.ErrInvalidInput, want: http.StatusBadRequest, code: "invalid_input"},
		{name: "not found", err: provider.ErrNotConnected, want: http.StatusNotFound, code: "not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			providers := newFakeProviderService()
			providers.updateErr = tc.err
			server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
			req := httptest.NewRequest(http.MethodPatch, "/api/admin/providers/openai/accounts/7", strings.NewReader(`{"priority":1}`))
			req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != tc.want {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.want)
			}
			var body map[string]string
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["error"] != tc.code {
				t.Fatalf("error = %q, want %s", body["error"], tc.code)
			}
		})
	}
}

func TestAdminCanRefreshProviderAccount(t *testing.T) {
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true, Priority: 10, Status: provider.AccountStatusCircuitOpen}}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/providers/openai/accounts/7/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if providers.refreshedAccountID != 7 {
		t.Fatalf("refreshedAccountID = %d, want 7", providers.refreshedAccountID)
	}
	var body struct {
		Account provider.Account `json:"account"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Account.ID != 7 || body.Account.Status != provider.AccountStatusActive || body.Account.LastRefreshAt == nil {
		t.Fatalf("account = %+v, want refreshed active account 7", body.Account)
	}
}

func TestAdminCanRefreshUnifiedProviderAccount(t *testing.T) {
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true, Priority: 10, Status: provider.AccountStatusCircuitOpen}}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/7/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if providers.refreshedAccountID != 7 {
		t.Fatalf("refreshedAccountID = %d, want 7", providers.refreshedAccountID)
	}
	var body struct {
		Account provider.Account `json:"account"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Account.ID != 7 || body.Account.Status != provider.AccountStatusActive || body.Account.LastRefreshAt == nil {
		t.Fatalf("account = %+v, want refreshed active account 7", body.Account)
	}
}

func TestAdminCanTestUnifiedProviderAccount(t *testing.T) {
	future := time.Now().Add(time.Hour)
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{{
		ID:               7,
		Provider:         "openai",
		DisplayName:      "Account A",
		Enabled:          true,
		Priority:         10,
		Status:           provider.AccountStatusCircuitOpen,
		StatusReason:     "previous failure",
		LastError:        "previous failure",
		CircuitOpenUntil: &future,
		FailureCount:     2,
	}}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/7/test", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if providers.testedAccountID != 7 {
		t.Fatalf("testedAccountID = %d, want 7", providers.testedAccountID)
	}
	var body struct {
		Account provider.Account `json:"account"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Account.ID != 7 || body.Account.Status != provider.AccountStatusActive || body.Account.LastError != "" || body.Account.CircuitOpenUntil != nil || body.Account.FailureCount != 0 {
		t.Fatalf("account = %+v, want tested active account 7", body.Account)
	}
}

func TestAdminCanTestAllUnifiedProviderAccounts(t *testing.T) {
	future := time.Now().Add(time.Hour)
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{
		{
			ID:          7,
			Provider:    "openai",
			DisplayName: "Account A",
			Enabled:     true,
			Priority:    10,
			Status:      provider.AccountStatusActive,
		},
		{
			ID:               8,
			Provider:         "openai",
			DisplayName:      "Account B",
			Enabled:          true,
			Priority:         20,
			Status:           provider.AccountStatusCircuitOpen,
			StatusReason:     "previous failure",
			LastError:        "previous failure",
			CircuitOpenUntil: &future,
			FailureCount:     2,
		},
	}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/test", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if !providers.testedAllAccounts {
		t.Fatal("testedAllAccounts = false, want true")
	}
	var body struct {
		Accounts []provider.Account `json:"accounts"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Accounts) != 2 || body.Accounts[1].ID != 8 || body.Accounts[1].Status != provider.AccountStatusActive || body.Accounts[1].LastError != "" {
		t.Fatalf("accounts = %+v, want all tested accounts with cleared local failures", body.Accounts)
	}
}

func TestAdminCanListUnifiedProviderAccountTestResults(t *testing.T) {
	checkedAt := time.Unix(2000, 0).UTC()
	createdAt := time.Unix(2001, 0).UTC()
	providers := newFakeProviderService()
	providers.accountTestResults = []provider.AccountTestResult{
		{
			ID:        11,
			AccountID: 7,
			Provider:  "openai",
			Status:    provider.AccountTestStatusFailed,
			Message:   "quota window",
			CheckedAt: checkedAt,
			CreatedAt: createdAt,
		},
		{
			ID:        12,
			AccountID: 8,
			Provider:  "openai",
			Status:    provider.AccountTestStatusPassed,
			CheckedAt: checkedAt,
			CreatedAt: createdAt,
		},
	}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/provider-accounts/7/test-results?limit=2", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if providers.testResultsAccountID != 7 || providers.testResultsLimit != 2 {
		t.Fatalf("test results call = id:%d limit:%d, want account 7 limit 2", providers.testResultsAccountID, providers.testResultsLimit)
	}
	var body struct {
		Results []provider.AccountTestResult `json:"results"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Results) != 1 || body.Results[0].ID != 11 || body.Results[0].Message != "quota window" || !body.Results[0].CheckedAt.Equal(checkedAt) {
		t.Fatalf("results = %+v, want account 7 failed result", body.Results)
	}
}

func TestAdminListProviderAccountTestResultsMapsErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		err  error
		want int
		code string
	}{
		{name: "bad id", path: "/api/admin/provider-accounts/not-a-number/test-results", want: http.StatusBadRequest, code: "bad_request"},
		{name: "bad limit", path: "/api/admin/provider-accounts/7/test-results?limit=bad", want: http.StatusBadRequest, code: "bad_request"},
		{name: "not found", path: "/api/admin/provider-accounts/7/test-results", err: provider.ErrNotConnected, want: http.StatusNotFound, code: "not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			providers := newFakeProviderService()
			providers.accountTestResultsErr = tc.err
			server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != tc.want {
				t.Fatalf("status = %d body=%s, want %d", recorder.Code, recorder.Body.String(), tc.want)
			}
			var body map[string]string
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["error"] != tc.code {
				t.Fatalf("error = %q, want %q", body["error"], tc.code)
			}
		})
	}
}

func TestAdminCanPauseUnifiedProviderAccountScheduling(t *testing.T) {
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{{
		ID:          7,
		Provider:    "openai",
		DisplayName: "Account A",
		Enabled:     true,
		Priority:    10,
		Status:      provider.AccountStatusActive,
	}}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/7/pause", strings.NewReader(`{"durationSeconds":300}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if providers.pausedAccountID != 7 || providers.pauseDuration != 5*time.Minute {
		t.Fatalf("pause call = id:%d duration:%s, want account 7 for 5m", providers.pausedAccountID, providers.pauseDuration)
	}
	var body struct {
		Account provider.Account `json:"account"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Account.ID != 7 || body.Account.Status != provider.AccountStatusCircuitOpen || body.Account.CircuitOpenUntil == nil {
		t.Fatalf("account = %+v, want paused circuit-open account 7", body.Account)
	}
}

func TestAdminCanResetUnifiedProviderAccountStatus(t *testing.T) {
	future := time.Now().Add(time.Hour)
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{{
		ID:                 7,
		Provider:           "openai",
		DisplayName:        "Account A",
		Enabled:            true,
		Priority:           10,
		Status:             provider.AccountStatusRateLimited,
		StatusReason:       "rate limited",
		LastError:          "rate limited",
		RateLimitedUntil:   &future,
		CircuitOpenUntil:   &future,
		FailureCount:       2,
		LastRefreshError:   "refresh failed",
		LastRefreshErrorAt: &future,
	}}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/7/reset-status", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if providers.resetStatusAccountID != 7 {
		t.Fatalf("resetStatusAccountID = %d, want 7", providers.resetStatusAccountID)
	}
	var body struct {
		Account provider.Account `json:"account"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Account.ID != 7 || body.Account.Status != provider.AccountStatusActive || body.Account.RateLimitedUntil != nil || body.Account.CircuitOpenUntil != nil || body.Account.FailureCount != 0 {
		t.Fatalf("account = %+v, want active account with cleared local status", body.Account)
	}
	if body.Account.LastRefreshError != "refresh failed" || body.Account.LastRefreshErrorAt == nil {
		t.Fatalf("refresh diagnostics = %+v, want preserved", body.Account)
	}
}

func TestAdminCanDisconnectProviderAccount(t *testing.T) {
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{{ID: 7, Provider: "openai", DisplayName: "Account A", Enabled: true, Priority: 10}}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/providers/openai/accounts/7/disconnect", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if providers.disconnectedAccountID != 7 {
		t.Fatalf("disconnectedAccountID = %d, want 7", providers.disconnectedAccountID)
	}
}

func TestAdminCanDeleteUnifiedProviderAccount(t *testing.T) {
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{{ID: 7, Provider: "openai", AccountType: provider.AccountTypeAPIUpstream, DisplayName: "Upstream", Enabled: true, Priority: 10}}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodDelete, "/api/admin/provider-accounts/7", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if providers.disconnectedAccountID != 7 {
		t.Fatalf("disconnectedAccountID = %d, want 7", providers.disconnectedAccountID)
	}
}

func TestAdminCanDisconnectUnifiedProviderAccountAction(t *testing.T) {
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{{ID: 7, Provider: "openai", AccountType: provider.AccountTypeCodexOAuth, DisplayName: "Account A", Enabled: true, Priority: 10}}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/7/disconnect", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if providers.disconnectedAccountID != 7 {
		t.Fatalf("disconnectedAccountID = %d, want 7", providers.disconnectedAccountID)
	}
}

func TestAccountModelsRequireSession(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())

	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/api/admin/providers/openai/accounts/7/models"},
		{method: http.MethodPut, path: "/api/admin/providers/openai/accounts/7/models", body: `{"models":[{"model":"gpt-5","enabled":true}]}`},
	} {
		t.Run(tc.method, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", recorder.Code)
			}
		})
	}
}

func TestListAccountModelsReturnsModels(t *testing.T) {
	providers := newFakeProviderService()
	providers.accountModels[7] = []provider.AccountModel{
		{ID: 11, AccountID: 7, Provider: "openai", Model: "gpt-5", Enabled: true, Source: provider.AccountModelSourceManual},
		{ID: 12, AccountID: 7, Provider: "openai", Model: "gpt-5-mini", Enabled: false, Source: provider.AccountModelSourceManual},
	}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/providers/openai/accounts/7/models", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Models []provider.AccountModel `json:"models"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Models) != 2 || body.Models[0].Model != "gpt-5" || body.Models[1].Enabled {
		t.Fatalf("models = %+v", body.Models)
	}
}

func TestReplaceAccountModelsReturnsSavedModels(t *testing.T) {
	providers := newFakeProviderService()
	providers.accountModels[7] = []provider.AccountModel{}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/providers/openai/accounts/7/models", strings.NewReader(`{"models":[{"model":"gpt-5","enabled":true},{"model":"gpt-5-mini","enabled":false}]}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Models []provider.AccountModel `json:"models"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Models) != 2 || body.Models[0].Model != "gpt-5" || body.Models[1].Enabled {
		t.Fatalf("models = %+v", body.Models)
	}
	if got := providers.accountModels[7]; len(got) != 2 || got[0].Model != "gpt-5" {
		t.Fatalf("saved models = %+v", got)
	}
}

func TestLegacyProviderAccountModelsRouteDelegatesToUnifiedModels(t *testing.T) {
	providers := newFakeProviderService()
	providers.accountModels[7] = []provider.AccountModel{
		{ID: 11, AccountID: 7, Provider: "openai", Model: "gpt-5", Enabled: true, Source: provider.AccountModelSourceManual},
	}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/providers/openai/accounts/7/models", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Models []provider.AccountModel `json:"models"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Models) != 1 || body.Models[0].AccountID != 7 || body.Models[0].Model != "gpt-5" {
		t.Fatalf("models = %+v", body.Models)
	}
}

func TestUnifiedAccountModelsEndpoints(t *testing.T) {
	providers := newFakeProviderService()
	providers.accountModels[7] = []provider.AccountModel{
		{ID: 11, AccountID: 7, Provider: "openai", Model: "gpt-5", Enabled: true, Source: provider.AccountModelSourceManual},
	}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)

	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/provider-accounts/7/models", nil)
	listReq.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	listRecorder := httptest.NewRecorder()
	server.ServeHTTP(listRecorder, listReq)

	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s, want 200", listRecorder.Code, listRecorder.Body.String())
	}
	var listBody struct {
		Models []provider.AccountModel `json:"models"`
	}
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("decode list body: %v", err)
	}
	if len(listBody.Models) != 1 || listBody.Models[0].Model != "gpt-5" {
		t.Fatalf("list models = %+v", listBody.Models)
	}

	replaceReq := httptest.NewRequest(http.MethodPut, "/api/admin/provider-accounts/7/models", strings.NewReader(`{"models":[{"model":"gpt-4.1","enabled":true},{"model":"gpt-5","enabled":false}]}`))
	replaceReq.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	replaceRecorder := httptest.NewRecorder()
	server.ServeHTTP(replaceRecorder, replaceReq)

	if replaceRecorder.Code != http.StatusOK {
		t.Fatalf("replace status = %d body=%s, want 200", replaceRecorder.Code, replaceRecorder.Body.String())
	}
	var replaceBody struct {
		Models []provider.AccountModel `json:"models"`
	}
	if err := json.Unmarshal(replaceRecorder.Body.Bytes(), &replaceBody); err != nil {
		t.Fatalf("decode replace body: %v", err)
	}
	if len(replaceBody.Models) != 2 || replaceBody.Models[0].Model != "gpt-4.1" || replaceBody.Models[1].Enabled {
		t.Fatalf("replace models = %+v", replaceBody.Models)
	}
}

func TestAccountModelsMapProviderErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		method string
		err    error
		want   int
		code   string
	}{
		{name: "list invalid input", method: http.MethodGet, err: provider.ErrInvalidInput, want: http.StatusBadRequest, code: "invalid_input"},
		{name: "list not found", method: http.MethodGet, err: provider.ErrNotConnected, want: http.StatusNotFound, code: "not_found"},
		{name: "replace invalid input", method: http.MethodPut, err: provider.ErrInvalidInput, want: http.StatusBadRequest, code: "invalid_input"},
		{name: "replace not found", method: http.MethodPut, err: provider.ErrNotConnected, want: http.StatusNotFound, code: "not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			providers := newFakeProviderService()
			if tc.method == http.MethodGet {
				providers.accountModelsErr = tc.err
			} else {
				providers.replaceModelsErr = tc.err
			}
			server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
			req := httptest.NewRequest(tc.method, "/api/admin/providers/openai/accounts/7/models", strings.NewReader(`{"models":[{"model":"gpt-5","enabled":true}]}`))
			req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != tc.want {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.want)
			}
			var body map[string]string
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["error"] != tc.code {
				t.Fatalf("error = %q, want %s", body["error"], tc.code)
			}
		})
	}
}

func TestSyncProviderAccountModelsReturnsModelsAndSummary(t *testing.T) {
	providers := newFakeProviderService()
	now := time.Now()
	providers.syncModelsResult = []provider.AccountModel{
		{ID: 1, AccountID: 7, Provider: "openai", Model: "gpt-5", Enabled: true, Source: provider.AccountModelSourceUpstream, LastSeenAt: &now, CreatedAt: now, UpdatedAt: now},
	}
	providers.syncModelsSummary = provider.AccountModelSyncSummary{Total: 1, New: 1, Preserved: 0, SkippedManual: 0}

	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/7/models/sync", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Models []provider.AccountModel          `json:"models"`
		Synced provider.AccountModelSyncSummary `json:"synced"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Models) != 1 || body.Models[0].Model != "gpt-5" {
		t.Fatalf("models = %+v", body.Models)
	}
	if body.Models[0].Source != provider.AccountModelSourceUpstream {
		t.Fatalf("source = %q, want %q", body.Models[0].Source, provider.AccountModelSourceUpstream)
	}
	if body.Synced.New != 1 {
		t.Fatalf("synced.new = %d, want 1", body.Synced.New)
	}
}

func TestSyncProviderAccountModelsMapsProviderErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
		code string
	}{
		{name: "invalid input", err: provider.ErrInvalidInput, want: http.StatusBadRequest, code: "invalid_input"},
		{name: "not found", err: provider.ErrNotConnected, want: http.StatusNotFound, code: "not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			providers := newFakeProviderService()
			providers.syncModelsErr = tc.err
			server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
			req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/7/models/sync", nil)
			req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != tc.want {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.want)
			}
			var body map[string]string
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["error"] != tc.code {
				t.Fatalf("error = %q, want %s", body["error"], tc.code)
			}
		})
	}
}

func TestTestProviderAccountModelReturnsDiagnosticResult(t *testing.T) {
	checkedAt := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	providers := newFakeProviderService()
	providers.accountModelTestResult = provider.AccountModelTestResult{
		AccountID:  7,
		Model:      "gpt-test",
		Status:     provider.AccountTestStatusFailed,
		ErrorCode:  "rate_limited",
		HTTPStatus: http.StatusTooManyRequests,
		LatencyMS:  842,
		Message:    "quota window",
		CheckedAt:  checkedAt,
	}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/7/model-tests", strings.NewReader(`{"model":"gpt-test"}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if providers.testedModelAccountID != 7 || providers.testedModel != "gpt-test" {
		t.Fatalf("test call account=%d model=%q", providers.testedModelAccountID, providers.testedModel)
	}
	var body struct {
		Result provider.AccountModelTestResult `json:"result"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Result.Status != provider.AccountTestStatusFailed || body.Result.ErrorCode != "rate_limited" || body.Result.HTTPStatus != http.StatusTooManyRequests || !body.Result.CheckedAt.Equal(checkedAt) {
		t.Fatalf("result = %+v", body.Result)
	}
}

func TestTestProviderAccountModelValidatesInputAndMapsNotFound(t *testing.T) {
	tests := []struct {
		name string
		path string
		body string
		err  error
		want int
		code string
	}{
		{name: "bad id", path: "/api/admin/provider-accounts/nope/model-tests", body: `{"model":"gpt-test"}`, want: http.StatusBadRequest, code: "bad_request"},
		{name: "bad body", path: "/api/admin/provider-accounts/7/model-tests", body: `{`, want: http.StatusBadRequest, code: "bad_request"},
		{name: "invalid model", path: "/api/admin/provider-accounts/7/model-tests", body: `{"model":""}`, err: provider.ErrInvalidInput, want: http.StatusBadRequest, code: "invalid_input"},
		{name: "missing model", path: "/api/admin/provider-accounts/7/model-tests", body: `{"model":"missing"}`, err: provider.ErrNotConnected, want: http.StatusNotFound, code: "not_found"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			providers := newFakeProviderService()
			providers.accountModelTestErr = test.err
			server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
			req := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
			req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != test.want {
				t.Fatalf("status = %d body=%s, want %d", recorder.Code, recorder.Body.String(), test.want)
			}
			var response struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if response.Error != test.code {
				t.Fatalf("error = %q, want %q", response.Error, test.code)
			}
		})
	}
}

func TestAdminDisconnectProviderAccountMapsErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
		code string
	}{
		{name: "invalid input", err: provider.ErrInvalidInput, want: http.StatusBadRequest, code: "invalid_input"},
		{name: "not found", err: provider.ErrNotConnected, want: http.StatusNotFound, code: "not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			providers := newFakeProviderService()
			providers.disconnectErr = tc.err
			server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
			req := httptest.NewRequest(http.MethodPost, "/api/admin/providers/openai/accounts/7/disconnect", nil)
			req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != tc.want {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.want)
			}
			var body map[string]string
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["error"] != tc.code {
				t.Fatalf("error = %q, want %s", body["error"], tc.code)
			}
		})
	}
}

func TestAdminDeleteUnifiedProviderAccountMapsErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		err  error
		want int
		code string
	}{
		{name: "bad id", path: "/api/admin/provider-accounts/not-a-number", want: http.StatusBadRequest, code: "bad_request"},
		{name: "invalid input", path: "/api/admin/provider-accounts/7", err: provider.ErrInvalidInput, want: http.StatusBadRequest, code: "invalid_input"},
		{name: "not found", path: "/api/admin/provider-accounts/7", err: provider.ErrNotConnected, want: http.StatusNotFound, code: "not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			providers := newFakeProviderService()
			providers.disconnectErr = tc.err
			server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
			req := httptest.NewRequest(http.MethodDelete, tc.path, nil)
			req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			if recorder.Code != tc.want {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.want)
			}
			var body map[string]string
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body["error"] != tc.code {
				t.Fatalf("error = %q, want %s", body["error"], tc.code)
			}
		})
	}
}

func TestProviderCallbackDoesNotConsumeManualCallback(t *testing.T) {
	providers := newFakeProviderService()
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://localhost:3000/oauth/openai/callback?code=abc&state=state", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if providers.callbackCode != "" || providers.callbackState != "" {
		t.Fatalf("callback was called with code %q state %q", providers.callbackCode, providers.callbackState)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "code=abc") || !strings.Contains(body, "state=state") {
		t.Fatalf("body did not include callback values: %s", body)
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", got)
	}
}

func TestManualOAuthCallbackUsesTrustedRequestInfo(t *testing.T) {
	tests := []struct {
		name       string
		cfg        config.Config
		target     string
		remoteAddr string
		host       string
		headers    http.Header
		want       string
		reject     string
	}{
		{
			name:       "untrusted forwarded metadata ignored",
			cfg:        config.Config{PublicURL: "http://canonical.example:3000"},
			target:     "/oauth/openai/callback?code=abc&state=state",
			remoteAddr: "192.0.2.10:1234",
			host:       "direct.example:3000",
			headers: http.Header{
				"X-Forwarded-Proto": {"https"},
				"X-Forwarded-Host":  {"attacker.example"},
			},
			want:   "http://canonical.example:3000/oauth/openai/callback?code=abc",
			reject: "attacker.example",
		},
		{
			name:       "trusted forwarded metadata accepted",
			cfg:        config.Config{TrustedProxyCIDRs: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}},
			target:     "/oauth/openai/callback?code=abc&state=state",
			remoteAddr: "10.0.0.2:1234",
			host:       "internal.example:3000",
			headers: http.Header{
				"X-Forwarded-Proto": {"https"},
				"X-Forwarded-Host":  {"public.example"},
			},
			want: "https://public.example/oauth/openai/callback?code=abc",
		},
		{
			name:       "absolute form cannot bypass direct host",
			cfg:        config.Config{PublicURL: "https://canonical.example"},
			target:     "http://attacker.example/oauth/openai/callback?code=abc&state=state",
			remoteAddr: "192.0.2.10:1234",
			host:       "direct.example",
			want:       "https://canonical.example/oauth/openai/callback?code=abc",
			reject:     "attacker.example",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewServer(tt.cfg, staticHealth{}, newFakeAdminService(), newFakeProviderService())
			req := httptest.NewRequest(http.MethodGet, tt.target, nil)
			req.RemoteAddr = tt.remoteAddr
			req.Host = tt.host
			req.Header = tt.headers
			recorder := httptest.NewRecorder()

			server.ServeHTTP(recorder, req)

			body := recorder.Body.String()
			if recorder.Code != http.StatusOK || !strings.Contains(body, tt.want) {
				t.Fatalf("status = %d body = %s, want URL containing %q", recorder.Code, body, tt.want)
			}
			if tt.reject != "" && strings.Contains(body, tt.reject) {
				t.Fatalf("body contains rejected host %q: %s", tt.reject, body)
			}
		})
	}
}

func TestProviderManualCallbackCompletesFromCallbackURL(t *testing.T) {
	providers := newFakeProviderService()
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/providers/openai/callback", strings.NewReader(`{"callbackUrl":"http://localhost:3000/oauth/openai/callback?code=abc&state=oauth_state"}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if providers.callbackCode != "abc" || providers.callbackState != "oauth_state" {
		t.Fatalf("callback args = code %q state %q, want parsed callback URL values", providers.callbackCode, providers.callbackState)
	}
}

func TestUnifiedProviderAccountCodexOAuthCallbackCompletesFromCallbackURL(t *testing.T) {
	providers := newFakeProviderService()
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/provider-accounts/codex-oauth/callback", strings.NewReader(`{"callbackUrl":"http://localhost:3000/oauth/openai/callback?code=abc&state=oauth_state"}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if providers.callbackCode != "abc" || providers.callbackState != "oauth_state" {
		t.Fatalf("callback args = code %q state %q, want parsed callback URL values", providers.callbackCode, providers.callbackState)
	}
}

func TestProviderManualCallbackRejectsMissingCallbackValues(t *testing.T) {
	providers := newFakeProviderService()
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), providers)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/providers/openai/callback", strings.NewReader(`{"callbackUrl":"http://localhost:3000/oauth/openai/callback?code=abc"}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	if providers.callbackCode != "" || providers.callbackState != "" {
		t.Fatalf("callback was called with code %q state %q", providers.callbackCode, providers.callbackState)
	}
}

func TestListRequestLogsRequiresSessionAndReturnsLogs(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/request-logs", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}

	admins := newFakeAdminService()
	server = NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService())
	admins.requestLogHasMore = true
	admins.requestLogNextCursor = "opaque-next"
	req := httptest.NewRequest(http.MethodGet, "/api/admin/request-logs?limit=20&cursor=opaque-current&requestId=req_3&q=codex&statusClass=server_error&statusCode=503&providerAccountId=7&routingPoolId=9&clientKeyId=12&model=gpt-5&sessionId=workspace-123&error=api_key_token_rate_limited&routingPoolError=routing_pool_unavailable&routingPoolChain=primary+-%3E+secondary&gatewayFallbacks=1&since=2000", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder = httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body struct {
		Logs       []admin.RequestLog `json:"logs"`
		HasMore    bool               `json:"hasMore"`
		NextCursor string             `json:"nextCursor"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Logs) != 1 || body.Logs[0].RequestID != "req_3" {
		t.Fatalf("logs = %+v", body.Logs)
	}
	if !body.HasMore || body.NextCursor != "opaque-next" {
		t.Fatalf("page metadata = hasMore:%t nextCursor:%q, want true/opaque-next", body.HasMore, body.NextCursor)
	}
	if admins.requestLogFilter.Cursor != "opaque-current" {
		t.Fatalf("request log cursor = %q, want opaque-current", admins.requestLogFilter.Cursor)
	}
	if admins.requestLogFilter.Limit != 20 || admins.requestLogFilter.Query != "codex" || admins.requestLogFilter.StatusClass != admin.RequestLogStatusServerError {
		t.Fatalf("request log filter = %+v, want limit 20 query codex status server_error", admins.requestLogFilter)
	}
	if admins.requestLogFilter.RequestID != "req_3" {
		t.Fatalf("request log request ID = %q, want req_3", admins.requestLogFilter.RequestID)
	}
	if admins.requestLogFilter.StatusCode != 503 {
		t.Fatalf("request log status code = %d, want 503", admins.requestLogFilter.StatusCode)
	}
	if admins.requestLogFilter.ProviderAccountID != 7 {
		t.Fatalf("request log provider account ID = %d, want 7", admins.requestLogFilter.ProviderAccountID)
	}
	if admins.requestLogFilter.RoutingPoolID != 9 {
		t.Fatalf("request log routing pool ID = %d, want 9", admins.requestLogFilter.RoutingPoolID)
	}
	if admins.requestLogFilter.ClientKeyID != 12 {
		t.Fatalf("request log client key ID = %d, want 12", admins.requestLogFilter.ClientKeyID)
	}
	if admins.requestLogFilter.Model != "gpt-5" || admins.requestLogFilter.SessionID != "workspace-123" {
		t.Fatalf("request log model/session = %q/%q, want gpt-5/workspace-123", admins.requestLogFilter.Model, admins.requestLogFilter.SessionID)
	}
	if admins.requestLogFilter.Error != "api_key_token_rate_limited" {
		t.Fatalf("request log error = %q, want api_key_token_rate_limited", admins.requestLogFilter.Error)
	}
	if admins.requestLogFilter.RoutingPoolError != "routing_pool_unavailable" {
		t.Fatalf("request log routing pool error = %q, want routing_pool_unavailable", admins.requestLogFilter.RoutingPoolError)
	}
	if admins.requestLogFilter.RoutingPoolChain != "primary -> secondary" {
		t.Fatalf("request log routing pool chain = %q, want primary -> secondary", admins.requestLogFilter.RoutingPoolChain)
	}
	if !admins.requestLogFilter.GatewayFallbacks {
		t.Fatal("request log gateway fallback filter = false, want true")
	}
	if !admins.requestLogFilter.Since.Equal(time.Unix(2000, 0).UTC()) {
		t.Fatalf("request log since = %s, want unix 2000", admins.requestLogFilter.Since)
	}
	if body.Logs[0].GatewayAttemptCount != 2 || body.Logs[0].GatewayFallbackCount != 1 {
		t.Fatalf("gateway diagnostics = attempts:%d fallbacks:%d, want 2/1", body.Logs[0].GatewayAttemptCount, body.Logs[0].GatewayFallbackCount)
	}
}

func TestExportRequestLogsRequiresSessionAndReturnsCSV(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/request-logs/export?format=csv", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}

	admins := newFakeAdminService()
	admins.logs = []admin.RequestLog{{
		ID:                       9,
		RequestID:                "req_csv",
		ClientKey:                `codex "daily", key`,
		Provider:                 "openai",
		ProviderAccountID:        7,
		ProviderAccountType:      "codex_oauth",
		ProviderAccountName:      `primary "oauth"`,
		RoutingPoolID:            9,
		RoutingPoolName:          "primary",
		RoutingPoolFallbackDepth: 1,
		RoutingPoolFallbackChain: "primary -> secondary",
		RoutingPoolError:         "routing_pool_exhausted",
		Model:                    "gpt-5",
		SessionID:                "workspace-123",
		Route:                    "/v1/chat/completions",
		Method:                   http.MethodPost,
		StatusCode:               429,
		LatencyMS:                123,
		Error:                    "upstream_rate_limited",
		InputTokens:              10,
		OutputTokens:             20,
		TotalTokens:              30,
		CachedInputTokens:        4,
		ReasoningTokens:          6,
		UsageSource:              "stream",
		EstimatedCostMicrousd:    42,
		PricingMatched:           true,
		GatewayAttemptCount:      2,
		GatewayFallbackCount:     1,
		CreatedAt:                time.Unix(5000, 0).UTC(),
	}}
	server = NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService())
	req := httptest.NewRequest(http.MethodGet, "/api/admin/request-logs/export?format=csv&since=100&before=200&limit=10000&requestId=req_csv&q=codex&statusClass=client_error&providerAccountId=7&routingPoolId=9&clientKeyId=12&model=gpt-5&sessionId=workspace-123&error=upstream_rate_limited&routingPoolChain=primary+-%3E+secondary&gatewayFallbacks=1", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder = httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "text/csv; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want CSV", contentType)
	}
	if disposition := recorder.Header().Get("Content-Disposition"); !strings.Contains(disposition, "n2api-request-logs.csv") {
		t.Fatalf("Content-Disposition = %q, want csv attachment", disposition)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "id,request_id,client_key,provider,provider_account_id,provider_account_type,provider_account_name,routing_pool_id,routing_pool_name,routing_pool_fallback_depth,routing_pool_fallback_chain,routing_pool_error,model,session_id,route,method,status_code,latency_ms,error,input_tokens,output_tokens,total_tokens,cached_input_tokens,reasoning_tokens,usage_source,estimated_cost_microusd,pricing_matched,gateway_attempt_count,gateway_fallback_count,created_at") {
		t.Fatalf("CSV body missing header: %q", body)
	}
	if !strings.Contains(body, `9,req_csv,"codex ""daily"", key",openai,7,codex_oauth,"primary ""oauth""",9,primary,1,primary -> secondary,routing_pool_exhausted,gpt-5,workspace-123,/v1/chat/completions,POST,429,123,upstream_rate_limited,10,20,30,4,6,stream,42,true,2,1,1970-01-01T01:23:20Z`) {
		t.Fatalf("CSV body missing escaped row: %q", body)
	}
	if admins.requestLogExportRows != 10000 || admins.requestLogFilter.Query != "codex" || admins.requestLogFilter.StatusClass != admin.RequestLogStatusClientError {
		t.Fatalf("request log filter = %+v, want export query filters", admins.requestLogFilter)
	}
	if admins.requestLogFilter.RequestID != "req_csv" {
		t.Fatalf("request log request ID = %q, want req_csv", admins.requestLogFilter.RequestID)
	}
	if admins.requestLogFilter.ProviderAccountID != 7 || admins.requestLogFilter.RoutingPoolID != 9 || admins.requestLogFilter.ClientKeyID != 12 {
		t.Fatalf("request log ids = provider:%d pool:%d key:%d, want 7/9/12", admins.requestLogFilter.ProviderAccountID, admins.requestLogFilter.RoutingPoolID, admins.requestLogFilter.ClientKeyID)
	}
	if admins.requestLogFilter.Model != "gpt-5" || admins.requestLogFilter.SessionID != "workspace-123" || admins.requestLogFilter.Error != "upstream_rate_limited" || admins.requestLogFilter.RoutingPoolChain != "primary -> secondary" || !admins.requestLogFilter.GatewayFallbacks {
		t.Fatalf("request log filter = %+v, want model/session/error/chain/fallback filters", admins.requestLogFilter)
	}
}

func TestExportRequestLogsReturnsJSONAndRejectsUnknownFormat(t *testing.T) {
	admins := newFakeAdminService()
	server := NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService())
	req := httptest.NewRequest(http.MethodGet, "/api/admin/request-logs/export?format=json&limit=1", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Logs []admin.RequestLog `json:"logs"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode export JSON: %v", err)
	}
	if len(body.Logs) != 1 || body.Logs[0].RequestID != "req_3" {
		t.Fatalf("logs = %+v, want exported req_3", body.Logs)
	}

	admins.logs = []admin.RequestLog{
		{ID: 1, RequestID: "req_jsonl_1", Model: "gpt-5", StatusCode: 200, CreatedAt: time.Unix(6000, 0).UTC()},
		{ID: 2, RequestID: "req_jsonl_2", Model: "gpt-5-mini", StatusCode: 429, Error: "rate_limited", CreatedAt: time.Unix(6001, 0).UTC()},
	}
	req = httptest.NewRequest(http.MethodGet, "/api/admin/request-logs/export?format=jsonl&since=100&before=200&limit=2", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder = httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("jsonl status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/x-ndjson; charset=utf-8" {
		t.Fatalf("jsonl Content-Type = %q, want ndjson", contentType)
	}
	if disposition := recorder.Header().Get("Content-Disposition"); !strings.Contains(disposition, "n2api-request-logs.jsonl") {
		t.Fatalf("jsonl Content-Disposition = %q, want jsonl attachment", disposition)
	}
	lines := strings.Split(strings.TrimSpace(recorder.Body.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("jsonl lines = %d body=%q, want 2", len(lines), recorder.Body.String())
	}
	for index, line := range lines {
		var log admin.RequestLog
		if err := json.Unmarshal([]byte(line), &log); err != nil {
			t.Fatalf("decode jsonl line %d: %v", index, err)
		}
		if log.RequestID != admins.logs[index].RequestID {
			t.Fatalf("jsonl line %d request ID = %q, want %q", index, log.RequestID, admins.logs[index].RequestID)
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/request-logs/export?format=xml", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder = httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unknown format status = %d body=%s, want 400", recorder.Code, recorder.Body.String())
	}
}

func TestErrorPassthroughRulesRouteIsRegisteredImmediately(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	req := httptest.NewRequest(http.MethodGet, "/api/admin/error-passthrough-rules", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Rules []admin.ErrorPassthroughRule `json:"rules"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Rules) != 1 || body.Rules[0].Pattern != "insufficient_quota" {
		t.Fatalf("rules = %+v, want seeded error passthrough rule", body.Rules)
	}
}

func TestFingerprintProfilesCRUDRequiresSessionAndMapsInputs(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/fingerprint-profiles", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}

	admins := newFakeAdminService()
	server = NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService())
	req := httptest.NewRequest(http.MethodGet, "/api/admin/fingerprint-profiles", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder = httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var listBody struct {
		Profiles []admin.FingerprintProfile `json:"profiles"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("decode list body: %v", err)
	}
	if len(listBody.Profiles) != 1 || listBody.Profiles[0].TLSFingerprint != "chrome" || listBody.Profiles[0].Headers["X-Test"] != "1" {
		t.Fatalf("profiles = %+v, want seeded fingerprint profile", listBody.Profiles)
	}

	payload := `{"name":"Firefox","description":"desktop","userAgent":"Mozilla/5.0","tlsFingerprint":"firefox","headers":{"X-FP":"yes"},"enabled":true}`
	req = httptest.NewRequest(http.MethodPost, "/api/admin/fingerprint-profiles", strings.NewReader(payload))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder = httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s, want 201", recorder.Code, recorder.Body.String())
	}
	if admins.fingerprintInput.Name != "Firefox" || admins.fingerprintInput.TLSFingerprint != "firefox" || admins.fingerprintInput.Headers["X-FP"] != "yes" {
		t.Fatalf("fingerprint create input = %+v", admins.fingerprintInput)
	}

	req = httptest.NewRequest(http.MethodPatch, "/api/admin/fingerprint-profiles/8", strings.NewReader(payload))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder = httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if admins.fingerprintID != 8 || admins.fingerprintInput.Name != "Firefox" {
		t.Fatalf("fingerprint update id/input = %d/%+v, want 8/Firefox", admins.fingerprintID, admins.fingerprintInput)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/admin/fingerprint-profiles/8", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder = httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d body=%s, want 204", recorder.Code, recorder.Body.String())
	}
	if admins.fingerprintID != 8 {
		t.Fatalf("fingerprint delete id = %d, want 8", admins.fingerprintID)
	}
}

func TestFingerprintProfilesCRUDMapsValidationAndNotFoundErrors(t *testing.T) {
	admins := newFakeAdminService()
	admins.fingerprintErr = admin.ErrInvalidInput
	server := NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService())
	req := httptest.NewRequest(http.MethodPost, "/api/admin/fingerprint-profiles", strings.NewReader(`{"name":""}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid create status = %d body=%s, want 400", recorder.Code, recorder.Body.String())
	}

	admins = newFakeAdminService()
	admins.fingerprintErr = admin.ErrNotFound
	server = NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService())
	req = httptest.NewRequest(http.MethodPatch, "/api/admin/fingerprint-profiles/404", strings.NewReader(`{"name":"Missing"}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder = httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("not found update status = %d body=%s, want 404", recorder.Code, recorder.Body.String())
	}
}

func TestErrorPassthroughRulesCRUDMapsInputsAndErrors(t *testing.T) {
	admins := newFakeAdminService()
	server := NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService())
	payload := `{"pattern":"insufficient_quota","matchType":"error_code","description":"quota passthrough","enabled":true,"priority":5}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/error-passthrough-rules", strings.NewReader(payload))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s, want 201", recorder.Code, recorder.Body.String())
	}
	if admins.errorRuleInput.Pattern != "insufficient_quota" || admins.errorRuleInput.MatchType != "error_code" || admins.errorRuleInput.Priority != 5 {
		t.Fatalf("error rule create input = %+v", admins.errorRuleInput)
	}

	req = httptest.NewRequest(http.MethodPatch, "/api/admin/error-passthrough-rules/2", strings.NewReader(payload))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder = httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if admins.errorRuleID != 2 || admins.errorRuleInput.Description != "quota passthrough" {
		t.Fatalf("error rule update id/input = %d/%+v, want 2/description", admins.errorRuleID, admins.errorRuleInput)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/admin/error-passthrough-rules/2", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder = httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d body=%s, want 204", recorder.Code, recorder.Body.String())
	}
	if admins.errorRuleID != 2 {
		t.Fatalf("error rule delete id = %d, want 2", admins.errorRuleID)
	}

	admins = newFakeAdminService()
	admins.errorRuleErr = admin.ErrInvalidInput
	server = NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService())
	req = httptest.NewRequest(http.MethodPost, "/api/admin/error-passthrough-rules", strings.NewReader(payload))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder = httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid create status = %d body=%s, want 400", recorder.Code, recorder.Body.String())
	}

	admins = newFakeAdminService()
	admins.errorRuleErr = admin.ErrNotFound
	server = NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService())
	req = httptest.NewRequest(http.MethodPatch, "/api/admin/error-passthrough-rules/404", strings.NewReader(payload))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder = httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("not found update status = %d body=%s, want 404", recorder.Code, recorder.Body.String())
	}
}

func TestListRequestLogsRejectsInvalidFilter(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	req := httptest.NewRequest(http.MethodGet, "/api/admin/request-logs?statusClass=bad", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestListRequestLogsRejectsInvalidCursor(t *testing.T) {
	admins := newFakeAdminService()
	admins.requestLogErr = admin.ErrInvalidInput
	server := NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService())
	req := httptest.NewRequest(http.MethodGet, "/api/admin/request-logs?cursor=tampered", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"error":"invalid_input"`) {
		t.Fatalf("status/body = %d %s, want 400 invalid_input", recorder.Code, recorder.Body.String())
	}
}

func TestListRequestLogsRejectsInvalidProviderAccountID(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	req := httptest.NewRequest(http.MethodGet, "/api/admin/request-logs?providerAccountId=abc", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestListRequestLogsRejectsInvalidClientKeyID(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	req := httptest.NewRequest(http.MethodGet, "/api/admin/request-logs?clientKeyId=abc", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestListRequestLogsPassesUsageSourceFilter(t *testing.T) {
	admins := newFakeAdminService()
	server := NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService())
	req := httptest.NewRequest(http.MethodGet, "/api/admin/request-logs?usageSource=missing", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if admins.requestLogFilter.UsageSource != "missing" {
		t.Fatalf("usage source filter = %q, want missing", admins.requestLogFilter.UsageSource)
	}
}

func TestGatewaySettingsRequiresSessionAndReturnsRuntimeLimits(t *testing.T) {
	admins := newFakeAdminService()
	admins.gatewaySettings = admin.GatewaySettings{
		MaxConcurrentGatewayRequests:           10,
		MaxConcurrentRequestsPerAccount:        2,
		MaxConcurrentRequestsPerKey:            3,
		RequestsPerMinutePerKey:                60,
		TokensPerMinutePerKey:                  60000,
		ProviderAccountAutoTestEnabled:         true,
		ProviderAccountAutoTestIntervalSeconds: 120,
		RequestLogRetentionDays:                14,
	}
	cfg := config.Config{
		GatewayMaxConcurrentRequests:           10,
		GatewayMaxConcurrentRequestsPerAccount: 2,
		GatewayMaxConcurrentRequestsPerKey:     3,
		GatewayRequestsPerMinutePerKey:         60,
		GatewayTokensPerMinutePerKey:           60000,
		ProviderAccountAutoTestEnabled:         true,
		ProviderAccountAutoTestInterval:        120 * time.Second,
	}
	server := NewServer(cfg, staticHealth{}, admins, newFakeProviderService())
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/gateway-settings", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/gateway-settings", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder = httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body struct {
		MaxConcurrentGatewayRequests           int  `json:"maxConcurrentGatewayRequests"`
		MaxConcurrentRequestsPerAccount        int  `json:"maxConcurrentRequestsPerAccount"`
		MaxConcurrentRequestsPerKey            int  `json:"maxConcurrentRequestsPerKey"`
		RequestsPerMinutePerKey                int  `json:"requestsPerMinutePerKey"`
		TokensPerMinutePerKey                  int  `json:"tokensPerMinutePerKey"`
		ProviderAccountAutoTestEnabled         bool `json:"providerAccountAutoTestEnabled"`
		ProviderAccountAutoTestIntervalSeconds int  `json:"providerAccountAutoTestIntervalSeconds"`
		RequestLogRetentionDays                int  `json:"requestLogRetentionDays"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.MaxConcurrentGatewayRequests != 10 ||
		body.MaxConcurrentRequestsPerAccount != 2 ||
		body.MaxConcurrentRequestsPerKey != 3 ||
		body.RequestsPerMinutePerKey != 60 ||
		body.TokensPerMinutePerKey != 60000 ||
		!body.ProviderAccountAutoTestEnabled ||
		body.ProviderAccountAutoTestIntervalSeconds != 120 ||
		body.RequestLogRetentionDays != 14 {
		t.Fatalf("gateway settings = %+v, want configured runtime limits", body)
	}
}

func TestGatewaySettingsIncludesProviderAccountAutoTestStatus(t *testing.T) {
	admins := newFakeAdminService()
	started := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	finished := started.Add(3 * time.Second)
	server := NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService(), fakeAutoTestStatusSource{
		status: provider.AutoTestStatus{
			Running:          true,
			LastStartedAt:    &started,
			LastFinishedAt:   &finished,
			LastAccountCount: 3,
			LastError:        "probe failed",
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/gateway-settings", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body struct {
		ProviderAccountAutoTestStatus provider.AutoTestStatus `json:"providerAccountAutoTestStatus"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	status := body.ProviderAccountAutoTestStatus
	if !status.Running || status.LastStartedAt == nil || status.LastFinishedAt == nil || status.LastAccountCount != 3 || status.LastError != "probe failed" {
		t.Fatalf("auto test status = %+v, want provided status", status)
	}
}

func TestGatewaySettingsIncludesRequestLogRetentionStatusAndStats(t *testing.T) {
	admins := newFakeAdminService()
	cutoff := time.Date(2026, time.June, 21, 12, 0, 0, 0, time.UTC)
	observed := cutoff.Add(30 * 24 * time.Hour)
	admins.retentionStats = admin.RequestLogRetentionStats{
		Cutoff: cutoff, TotalCountEstimate: 5000, EligibleCount: 1250, ObservedAt: observed,
	}
	source := fakeRequestLogRetentionStatusSource{status: admin.RequestLogRetentionStatus{
		AutomaticEnabled: true, LastDeletedCount: 1000, LastBatchCount: 1, LastCutoff: &cutoff,
	}}
	server := NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService(), source)
	request := httptest.NewRequest(http.MethodGet, "/api/admin/gateway-settings", nil)
	request.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var body gatewaySettingsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !body.RequestLogRetentionStatus.AutomaticEnabled || body.RequestLogRetentionStatus.LastDeletedCount != 1000 ||
		body.RequestLogRetentionStats.EligibleCount != 1250 || body.RequestLogRetentionStats.TotalCountEstimate != 5000 ||
		!body.RequestLogRetentionStats.Cutoff.Equal(cutoff) || !body.RequestLogRetentionStats.ObservedAt.Equal(observed) {
		t.Fatalf("request log retention response = status %+v stats %+v", body.RequestLogRetentionStatus, body.RequestLogRetentionStats)
	}
}

func TestGatewaySettingsPrefersStoredAdminSettings(t *testing.T) {
	admins := newFakeAdminService()
	admins.gatewaySettings = admin.GatewaySettings{
		MaxConcurrentGatewayRequests:    4,
		MaxConcurrentRequestsPerAccount: 5,
		MaxConcurrentRequestsPerKey:     6,
		RequestsPerMinutePerKey:         70,
		TokensPerMinutePerKey:           70000,
	}
	cfg := config.Config{
		GatewayMaxConcurrentRequests:           10,
		GatewayMaxConcurrentRequestsPerAccount: 2,
		GatewayMaxConcurrentRequestsPerKey:     3,
		GatewayRequestsPerMinutePerKey:         60,
		GatewayTokensPerMinutePerKey:           60000,
	}
	server := NewServer(cfg, staticHealth{}, admins, newFakeProviderService())
	req := httptest.NewRequest(http.MethodGet, "/api/admin/gateway-settings", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body admin.GatewaySettings
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body != admins.gatewaySettings {
		t.Fatalf("gateway settings = %+v, want stored %+v", body, admins.gatewaySettings)
	}
}

func TestGatewaySettingsUpdatesStoredLimits(t *testing.T) {
	admins := newFakeAdminService()
	server := NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService())
	req := httptest.NewRequest(http.MethodPut, "/api/admin/gateway-settings", strings.NewReader(`{
		"maxConcurrentGatewayRequests": 4,
		"maxConcurrentRequestsPerAccount": 5,
		"maxConcurrentRequestsPerKey": 6,
		"requestsPerMinutePerKey": 70,
		"tokensPerMinutePerKey": 70000,
		"providerAccountAutoTestEnabled": true,
		"providerAccountAutoTestIntervalSeconds": 180,
		"requestLogRetentionDays": 30
	}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	want := admin.GatewaySettings{
		MaxConcurrentGatewayRequests:           4,
		MaxConcurrentRequestsPerAccount:        5,
		MaxConcurrentRequestsPerKey:            6,
		RequestsPerMinutePerKey:                70,
		TokensPerMinutePerKey:                  70000,
		ProviderAccountAutoTestEnabled:         true,
		ProviderAccountAutoTestIntervalSeconds: 180,
		RequestLogRetentionDays:                30,
	}
	if admins.gatewaySettings != want {
		t.Fatalf("stored gateway settings = %+v, want %+v", admins.gatewaySettings, want)
	}
	var body admin.GatewaySettings
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body != want {
		t.Fatalf("response gateway settings = %+v, want %+v", body, want)
	}
}

func TestCleanupRequestLogsRequiresSessionAndReturnsDeletedCount(t *testing.T) {
	admins := newFakeAdminService()
	before := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	admins.cleanupResult = admin.RequestLogCleanupResult{
		RetentionDays: 14,
		Deleted:       3,
		Before:        before,
	}
	server := NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService())
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/admin/request-logs/cleanup", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/request-logs/cleanup", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder = httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if !admins.cleanupCalled {
		t.Fatal("CleanupRequestLogs was not called")
	}
	var body admin.RequestLogCleanupResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.RetentionDays != 14 || body.Deleted != 3 || !body.Before.Equal(before) {
		t.Fatalf("cleanup body = %+v, want retention 14 deleted 3 before %s", body, before)
	}
}

func TestCleanupRequestLogsReturnsConflictWhileRetentionRunnerOwnsLock(t *testing.T) {
	admins := newFakeAdminService()
	admins.cleanupErr = admin.ErrConflict
	server := NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService())
	request := httptest.NewRequest(http.MethodPost, "/api/admin/request-logs/cleanup", nil)
	request.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "conflict") {
		t.Fatalf("status = %d body=%s, want 409 conflict", recorder.Code, recorder.Body.String())
	}
}

func TestGatewaySettingsRejectsInvalidLimits(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	req := httptest.NewRequest(http.MethodPut, "/api/admin/gateway-settings", strings.NewReader(`{"maxConcurrentGatewayRequests": -1}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "invalid_input") {
		t.Fatalf("body = %q, want invalid_input", recorder.Body.String())
	}
}

func TestUsageSummaryRequiresSessionAndReturnsSummary(t *testing.T) {
	admins := newFakeAdminService()
	admins.usageSummary = admin.UsageSummary{Range: "7d", GroupBy: "model", TotalRequests: 2}
	server := NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService())
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/usage-summary", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/usage-summary?range=30d&groupBy=provider_account", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder = httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if admins.usageRange != "30d" || admins.usageGroupBy != "provider_account" {
		t.Fatalf("usage query = %q/%q, want 30d/provider_account", admins.usageRange, admins.usageGroupBy)
	}
	var body admin.UsageSummary
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.TotalRequests != 2 {
		t.Fatalf("summary = %+v, want total requests 2", body)
	}
}

func TestUsageSummaryRejectsInvalidQuery(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	req := httptest.NewRequest(http.MethodGet, "/api/admin/usage-summary?range=bad&groupBy=model", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", recorder.Code, recorder.Body.String())
	}
}

func TestOpsAccountHealthRequiresSessionAndReturnsHealth(t *testing.T) {
	admins := newFakeAdminService()
	since := time.Unix(4000, 0).UTC()
	admins.opsAccountHealth = admin.OpsAccountHealth{
		WindowStart:       since,
		WindowEnd:         time.Unix(5000, 0).UTC(),
		TotalAccounts:     5,
		EnabledAccounts:   4,
		Schedulable:       3,
		Disabled:          1,
		RateLimited:       1,
		CircuitOpen:       1,
		Expired:           1,
		TestedAccounts:    4,
		TestPassed:        3,
		TestFailed:        1,
		TestMissing:       1,
		RecentTestFailure: 1,
	}
	server := NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService())
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/ops/account-health", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/ops/account-health?since=4000", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder = httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if !admins.opsAccountSince.Equal(since) {
		t.Fatalf("ops account since = %v, want %v", admins.opsAccountSince, since)
	}
	var body admin.OpsAccountHealth
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body != admins.opsAccountHealth {
		t.Fatalf("account health = %+v, want %+v", body, admins.opsAccountHealth)
	}
}

func TestOpsAccountTestsRequiresSessionAndReturnsRows(t *testing.T) {
	admins := newFakeAdminService()
	since := time.Unix(4000, 0).UTC()
	checkedAt := time.Unix(5000, 0).UTC()
	admins.opsAccountTests = []admin.OpsAccountTest{{
		ID:          91,
		AccountID:   7,
		Provider:    "openai",
		AccountName: "Work Codex",
		AccountType: "codex_oauth",
		Status:      "failed",
		Message:     "quota exceeded",
		CheckedAt:   checkedAt,
		CreatedAt:   checkedAt,
	}}
	server := NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService())
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/ops/account-tests", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/ops/account-tests?since=4000&limit=20", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder = httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if !admins.opsAccountTestsSince.Equal(since) || admins.opsAccountTestsLimit != 20 {
		t.Fatalf("ops account tests args = since:%v limit:%d", admins.opsAccountTestsSince, admins.opsAccountTestsLimit)
	}
	var body struct {
		Tests []admin.OpsAccountTest `json:"tests"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Tests) != 1 || body.Tests[0] != admins.opsAccountTests[0] {
		t.Fatalf("account tests = %+v, want %+v", body.Tests, admins.opsAccountTests)
	}
}

func TestOpsCostBreakdownRequiresSessionAndReturnsBreakdown(t *testing.T) {
	admins := newFakeAdminService()
	since := time.Unix(4000, 0).UTC()
	admins.opsCostBreakdown = admin.OpsCostBreakdown{
		WindowStart:           since,
		WindowEnd:             time.Unix(5000, 0).UTC(),
		EstimatedCostMicrousd: 7500,
		TopModels: []admin.OpsCostBucket{{
			Key:                   "gpt-5",
			Label:                 "gpt-5",
			Requests:              3,
			EstimatedCostMicrousd: 4500,
		}},
		TopProviderAccounts: []admin.OpsCostBucket{{
			Key:                   "7",
			Label:                 "openai / Work Codex",
			Requests:              2,
			EstimatedCostMicrousd: 3000,
		}},
		TopClientKeys: []admin.OpsCostBucket{{
			Key:                   "12",
			Label:                 "desktop",
			Requests:              1,
			EstimatedCostMicrousd: 2000,
		}},
	}
	server := NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService())
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/ops/cost-breakdown", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/ops/cost-breakdown?since=4000", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder = httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if !admins.opsCostSince.Equal(since) {
		t.Fatalf("ops cost since = %v, want %v", admins.opsCostSince, since)
	}
	var body admin.OpsCostBreakdown
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.EstimatedCostMicrousd != 7500 || len(body.TopModels) != 1 || len(body.TopProviderAccounts) != 1 || len(body.TopClientKeys) != 1 {
		t.Fatalf("cost breakdown = %+v, want populated model/account/key buckets", body)
	}
}

func TestUsagePricingRequiresSessionAndReturnsPricing(t *testing.T) {
	admins := newFakeAdminService()
	admins.usagePricing = admin.UsagePricing{
		Version:  1,
		Currency: "USD",
		Unit:     "1M_tokens",
		Models: map[string]admin.UsagePrice{
			"gpt-5": {InputMicrousdPerMillion: 1_000_000},
		},
	}
	server := NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService())
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/usage-pricing", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/usage-pricing", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder = httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body admin.UsagePricing
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Currency != "USD" || body.Models["gpt-5"].InputMicrousdPerMillion != 1_000_000 {
		t.Fatalf("usage pricing = %+v", body)
	}
}

func TestUpdateUsagePricingReturnsSavedPricing(t *testing.T) {
	admins := newFakeAdminService()
	server := NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService())
	req := httptest.NewRequest(http.MethodPut, "/api/admin/usage-pricing", strings.NewReader(`{"version":1,"currency":"USD","unit":"1M_tokens","models":{"gpt-5":{"inputMicrousdPerMillion":1000000,"cachedInputMicrousdPerMillion":100000,"outputMicrousdPerMillion":4000000}}}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body admin.UsagePricing
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Models["gpt-5"].OutputMicrousdPerMillion != 4_000_000 {
		t.Fatalf("usage pricing = %+v", body)
	}
}

func TestUpdateUsagePricingReturnsBadRequestForInvalidInput(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	req := httptest.NewRequest(http.MethodPut, "/api/admin/usage-pricing", strings.NewReader(`{"version":1,"currency":"EUR","unit":"1M_tokens","models":{"gpt-5":{}}}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "invalid_input" {
		t.Fatalf("error = %q, want invalid_input", body["error"])
	}
}

func TestModelSettingsRequiresSessionAndReturnsSettings(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/model-settings", nil))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/model-settings", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder = httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body admin.ModelSettings
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.DefaultModel != "gpt-4.1" {
		t.Fatalf("model settings = %+v", body)
	}
}

func TestUpdateModelSettingsReturnsSavedSettings(t *testing.T) {
	admins := newFakeAdminService()
	server := NewServer(config.Config{}, staticHealth{}, admins, newFakeProviderService())
	req := httptest.NewRequest(http.MethodPut, "/api/admin/model-settings", strings.NewReader(`{"defaultModel":"gpt-5"}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body admin.ModelSettings
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.DefaultModel != "gpt-5" {
		t.Fatalf("model settings = %+v", body)
	}
}

func TestUpdateModelSettingsReturnsBadRequestForInvalidInput(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	req := httptest.NewRequest(http.MethodPut, "/api/admin/model-settings", strings.NewReader(`{"defaultModel":" "}`))
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "invalid_input" {
		t.Fatalf("error = %q, want invalid_input", body["error"])
	}
}

func TestModelRoutingReturnsStatus(t *testing.T) {
	admins := newFakeAdminService()
	admins.modelSettings = admin.ModelSettings{DefaultModel: "gpt-5"}
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{
		{ID: 7, Provider: "openai", Enabled: true},
		{ID: 8, Provider: "openai", Enabled: false},
	}
	admins.routingPools[0].Accounts = []admin.RoutingPoolAccount{{AccountID: 7, Priority: 0}}
	admins.routingPools[0].AccountIDs = []int64{7}
	providers.accountModels[7] = []provider.AccountModel{
		{ID: 11, AccountID: 7, Provider: "openai", Model: "gpt-5", Enabled: true, Source: provider.AccountModelSourceManual},
		{ID: 12, AccountID: 7, Provider: "openai", Model: "gpt-5-mini", Enabled: false, Source: provider.AccountModelSourceManual},
	}
	providers.accountModels[8] = []provider.AccountModel{
		{ID: 13, AccountID: 8, Provider: "openai", Model: "gpt-5", Enabled: true, Source: provider.AccountModelSourceManual},
		{ID: 14, AccountID: 8, Provider: "openai", Model: "unallowed-model", Enabled: true, Source: provider.AccountModelSourceManual},
	}
	server := NewServer(config.Config{}, staticHealth{}, admins, providers)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/model-routing", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body admin.ModelRoutingStatus
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.DefaultModel != "gpt-5" {
		t.Fatalf("routing settings = %+v", body)
	}
	if len(body.Models) != 3 {
		t.Fatalf("models length = %d, want 3 configured models: %+v", len(body.Models), body.Models)
	}
	if body.Models[0].Model != "gpt-5" || body.Models[0].ConfiguredCount != 2 || body.Models[0].EnabledCount != 1 {
		t.Fatalf("first model = %+v", body.Models[0])
	}
	if len(body.Models[0].Accounts) == 0 || !reflect.DeepEqual(body.Models[0].Accounts[0].RoutingPoolIDs, []int64{3}) {
		t.Fatalf("first model account routing pools = %+v, want [3]", body.Models[0].Accounts)
	}
	if body.Models[2].Model != "unallowed-model" || body.Models[2].ConfiguredCount != 1 || body.Models[2].EnabledCount != 0 {
		t.Fatalf("third model = %+v", body.Models[2])
	}
	if len(body.Warnings) != 2 {
		t.Fatalf("warnings = %+v, want warnings for both unschedulable models", body.Warnings)
	}
}

func TestModelRoutingStatusEnabledCountUsesSchedulableAccountRules(t *testing.T) {
	admins := newFakeAdminService()
	admins.modelSettings = admin.ModelSettings{DefaultModel: "gpt-5"}
	now := time.Now()
	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{
		{ID: 7, Provider: "openai", Enabled: true, Status: provider.AccountStatusExpired},
		{ID: 8, Provider: "openai", Enabled: true, Status: provider.AccountStatusRateLimited, RateLimitedUntil: &future},
		{ID: 9, Provider: "openai", Enabled: true, Status: provider.AccountStatusCircuitOpen, CircuitOpenUntil: &future},
		{ID: 10, Provider: "openai", Enabled: true, Status: provider.AccountStatusRateLimited, RateLimitedUntil: &past},
		{ID: 11, Provider: "openai", Enabled: true, Status: provider.AccountStatusCircuitOpen, CircuitOpenUntil: &past},
	}
	for _, account := range providers.accounts {
		providers.accountModels[account.ID] = []provider.AccountModel{
			{AccountID: account.ID, Provider: "openai", Model: "gpt-5", Enabled: true, Source: provider.AccountModelSourceManual},
		}
	}
	server := NewServer(config.Config{}, staticHealth{}, admins, providers)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/model-routing", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body admin.ModelRoutingStatus
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Models) != 1 {
		t.Fatalf("models = %+v, want one model", body.Models)
	}
	if body.Models[0].Model != "gpt-5" || body.Models[0].ConfiguredCount != 5 || body.Models[0].EnabledCount != 2 {
		t.Fatalf("model = %+v, want gpt-5 configured=5 enabled=2", body.Models[0])
	}
}

func TestModelRoutingStatusIncludesUnschedulableAccountReasons(t *testing.T) {
	admins := newFakeAdminService()
	admins.modelSettings = admin.ModelSettings{DefaultModel: "gpt-5"}
	now := time.Now()
	future := now.Add(time.Hour)
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{
		{ID: 7, Provider: "openai", DisplayName: "Disabled", Enabled: false, Status: provider.AccountStatusActive},
		{ID: 8, Provider: "openai", DisplayName: "Expired", Enabled: true, Status: provider.AccountStatusExpired},
		{ID: 9, Provider: "openai", DisplayName: "Limited", Enabled: true, Status: provider.AccountStatusRateLimited, StatusReason: "quota window", LastError: "upstream quota", LastErrorAt: &now, RateLimitedUntil: &future},
		{ID: 10, Provider: "openai", DisplayName: "Model off", Enabled: true, Status: provider.AccountStatusActive},
	}
	for _, account := range providers.accounts {
		providers.accountModels[account.ID] = []provider.AccountModel{
			{AccountID: account.ID, Provider: "openai", Model: "gpt-5", Enabled: account.ID != 10, Source: provider.AccountModelSourceManual},
		}
	}
	server := NewServer(config.Config{}, staticHealth{}, admins, providers)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/model-routing", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Models []struct {
			Model    string `json:"model"`
			Accounts []struct {
				ID                  int64  `json:"id"`
				Schedulable         bool   `json:"schedulable"`
				UnschedulableReason string `json:"unschedulableReason"`
				StatusReason        string `json:"statusReason"`
				LastError           string `json:"lastError"`
			} `json:"accounts"`
		} `json:"models"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Models) != 1 || body.Models[0].Model != "gpt-5" {
		t.Fatalf("models = %+v, want gpt-5 only", body.Models)
	}
	reasons := map[int64]string{}
	schedulable := map[int64]bool{}
	statusReasons := map[int64]string{}
	lastErrors := map[int64]string{}
	for _, account := range body.Models[0].Accounts {
		reasons[account.ID] = account.UnschedulableReason
		schedulable[account.ID] = account.Schedulable
		statusReasons[account.ID] = account.StatusReason
		lastErrors[account.ID] = account.LastError
	}
	wantReasons := map[int64]string{
		7:  "account disabled",
		8:  "account expired",
		9:  "rate limited",
		10: "model disabled",
	}
	if !slices.Equal(slices.Sorted(maps.Keys(reasons)), []int64{7, 8, 9, 10}) {
		t.Fatalf("account ids = %+v, want all configured accounts", reasons)
	}
	for id, want := range wantReasons {
		if schedulable[id] {
			t.Fatalf("account %d schedulable = true, want false", id)
		}
		if reasons[id] != want {
			t.Fatalf("account %d reason = %q, want %q", id, reasons[id], want)
		}
	}
	if statusReasons[9] != "quota window" || lastErrors[9] != "upstream quota" {
		t.Fatalf("account 9 diagnostics = statusReason:%q lastError:%q", statusReasons[9], lastErrors[9])
	}
}

func TestModelRoutingStatusIncludesSchedulableAccountOrder(t *testing.T) {
	admins := newFakeAdminService()
	admins.modelSettings = admin.ModelSettings{DefaultModel: "gpt-5"}
	now := time.Now()
	recent := now.Add(-time.Minute)
	older := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	providers := newFakeProviderService()
	providers.accounts = []provider.Account{
		{ID: 7, Provider: "openai", AccountType: provider.AccountTypeCodexOAuth, DisplayName: "Preferred", Enabled: true, Priority: 1, Status: provider.AccountStatusActive, LastUsedAt: &recent},
		{ID: 8, Provider: "openai", AccountType: provider.AccountTypeAPIUpstream, DisplayName: "Older same priority", Enabled: true, Priority: 1, Status: provider.AccountStatusActive, LastUsedAt: &older},
		{ID: 9, Provider: "openai", AccountType: provider.AccountTypeCodexOAuth, DisplayName: "Fallback", Enabled: true, Priority: 5, Status: provider.AccountStatusActive},
		{ID: 10, Provider: "openai", AccountType: provider.AccountTypeCodexOAuth, DisplayName: "Rate limited", Enabled: true, Priority: 0, Status: provider.AccountStatusRateLimited, RateLimitedUntil: &future},
		{ID: 11, Provider: "openai", AccountType: provider.AccountTypeCodexOAuth, DisplayName: "Model disabled", Enabled: true, Priority: 0, Status: provider.AccountStatusActive},
	}
	for _, account := range providers.accounts {
		providers.accountModels[account.ID] = []provider.AccountModel{
			{AccountID: account.ID, Provider: "openai", Model: "gpt-5", Enabled: account.ID != 11, Source: provider.AccountModelSourceManual},
		}
	}
	server := NewServer(config.Config{}, staticHealth{}, admins, providers)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/model-routing", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Models []struct {
			Model    string `json:"model"`
			Accounts []struct {
				ID           int64  `json:"id"`
				DisplayName  string `json:"displayName"`
				AccountType  string `json:"accountType"`
				Enabled      bool   `json:"enabled"`
				Priority     int    `json:"priority"`
				Status       string `json:"status"`
				LastUsedAt   string `json:"lastUsedAt"`
				ScheduleRank int    `json:"scheduleRank"`
				Schedulable  bool   `json:"schedulable"`
			} `json:"accounts"`
		} `json:"models"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Models) != 1 || body.Models[0].Model != "gpt-5" {
		t.Fatalf("models = %+v, want gpt-5 only", body.Models)
	}
	accounts := body.Models[0].Accounts
	if len(accounts) != 5 {
		t.Fatalf("accounts = %+v, want five configured model accounts", accounts)
	}
	if accounts[0].ID != 8 || accounts[1].ID != 7 || accounts[2].ID != 9 {
		t.Fatalf("account order = %+v, want last-used then priority order [8 7 9]", accounts)
	}
	for index, account := range accounts {
		if account.ScheduleRank != index+1 {
			t.Fatalf("account %d schedule rank = %d, want %d", account.ID, account.ScheduleRank, index+1)
		}
	}
	for index, account := range accounts[:3] {
		if !account.Schedulable {
			t.Fatalf("account %d at index %d schedulable = false, want true", account.ID, index)
		}
	}
	for index, account := range accounts[3:] {
		if account.Schedulable {
			t.Fatalf("account %d at trailing index %d schedulable = true, want false", account.ID, index)
		}
	}
	if accounts[0].DisplayName != "Older same priority" || accounts[0].AccountType != provider.AccountTypeAPIUpstream || !accounts[0].Enabled || accounts[0].Priority != 1 || accounts[0].Status != provider.AccountStatusActive {
		t.Fatalf("first account summary = %+v", accounts[0])
	}
	if accounts[0].LastUsedAt != older.Format(time.RFC3339Nano) {
		t.Fatalf("first account lastUsedAt = %q, want %q", accounts[0].LastUsedAt, older.Format(time.RFC3339Nano))
	}
}

func TestModelRoutingPreviewReturnsSessionAwareSelection(t *testing.T) {
	admins := newFakeAdminService()
	providers := newFakeProviderService()
	stickyReason := "reused sticky session binding for account priority 1, load factor 1, recent-error tier clean; new sticky FNV hashes stay within the highest exactly equal scheduling tier; base tie-breakers least-recently-used then account ID 8"
	fallbackReason := "ordered after sticky FNV hash, which only changes order within the highest exactly equal scheduling tier: account priority 1, load factor 1, recent-error tier clean; base tie-breakers least-recently-used then account ID 7"
	providers.selectionPreview = provider.SelectionPreview{
		Model:                "gpt-5",
		SessionID:            "workspace-123",
		SelectedAccountID:    8,
		StickyBoundAccountID: 8,
		Candidates: []provider.SelectionCandidate{
			{ID: 8, DisplayName: "Sticky", AccountType: provider.AccountTypeAPIUpstream, Priority: 1, ScheduleRank: 1, ScheduleReason: stickyReason, Selected: true, StickyBound: true},
			{ID: 7, DisplayName: "Fallback", AccountType: provider.AccountTypeCodexOAuth, Priority: 1, ScheduleRank: 2, ScheduleReason: fallbackReason},
		},
	}
	server := NewServer(config.Config{}, staticHealth{}, admins, providers)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/model-routing/preview?model=gpt-5&sessionId=workspace-123", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if providers.previewModel != "gpt-5" || providers.previewSessionID != "workspace-123" {
		t.Fatalf("preview call = model:%q session:%q", providers.previewModel, providers.previewSessionID)
	}
	var body struct {
		provider.SelectionPreview
		DiagnosisStatus  string   `json:"diagnosisStatus"`
		DiagnosisSummary string   `json:"diagnosisSummary"`
		DiagnosisHints   []string `json:"diagnosisHints"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.SelectedAccountID != 8 || len(body.Candidates) != 2 || !body.Candidates[0].Selected {
		t.Fatalf("preview = %+v, want selected sticky candidate", body)
	}
	if body.StickyBoundAccountID != 8 || !body.Candidates[0].StickyBound {
		t.Fatalf("preview sticky binding = %+v, want sticky bound account 8", body)
	}
	if body.Candidates[0].ScheduleReason != stickyReason || body.Candidates[1].ScheduleReason != fallbackReason {
		t.Fatalf("preview schedule reasons = %+v, want provider reasons preserved", body.Candidates)
	}
	if body.DiagnosisStatus != "routable" {
		t.Fatalf("diagnosis status = %q, want routable", body.DiagnosisStatus)
	}
	if !strings.Contains(body.DiagnosisSummary, "Sticky") || !strings.Contains(body.DiagnosisSummary, "gpt-5") {
		t.Fatalf("diagnosis summary = %q, want selected account and model", body.DiagnosisSummary)
	}
	if len(body.DiagnosisHints) != 0 {
		t.Fatalf("diagnosis hints = %+v, want none for routable preview", body.DiagnosisHints)
	}
}

func TestModelRoutingPreviewSupportsRoutingPoolScope(t *testing.T) {
	admins := newFakeAdminService()
	providers := newFakeProviderService()
	providers.selectionPreview = provider.SelectionPreview{
		Model:                    "gpt-5",
		SessionID:                "workspace-123",
		SelectedAccountID:        8,
		RoutingPoolID:            2,
		RoutingPoolName:          "secondary",
		RoutingPoolFallbackDepth: 1,
		RoutingPoolFallbackChain: "primary -> secondary",
		Candidates: []provider.SelectionCandidate{
			{ID: 8, DisplayName: "Pool account", AccountType: provider.AccountTypeAPIUpstream, Priority: 1, ScheduleRank: 1, Selected: true},
		},
	}
	server := NewServer(config.Config{}, staticHealth{}, admins, providers)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/model-routing/preview?model=gpt-5&sessionId=workspace-123&routingPoolId=7&excludedAccountIds=9", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if providers.previewRoutingPoolID != 7 || providers.previewModel != "gpt-5" || providers.previewSessionID != "workspace-123" || !reflect.DeepEqual(providers.previewExcludedIDs, []int64{9}) {
		t.Fatalf("preview call = pool:%d model:%q session:%q excluded:%+v, want pool 7 gpt-5 workspace-123 [9]", providers.previewRoutingPoolID, providers.previewModel, providers.previewSessionID, providers.previewExcludedIDs)
	}
	var body provider.SelectionPreview
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.RoutingPoolID != 2 || body.RoutingPoolName != "secondary" || body.RoutingPoolFallbackDepth != 1 || body.RoutingPoolFallbackChain != "primary -> secondary" {
		t.Fatalf("routing pool metadata = %+v, want fallback pool metadata", body)
	}
}

func TestModelRoutingPreviewIncludesConcurrencyState(t *testing.T) {
	admins := newFakeAdminService()
	admins.gatewaySettings.MaxConcurrentRequestsPerAccount = 5
	providers := newFakeProviderService()
	providers.selectionPreview = provider.SelectionPreview{
		Model:             "gpt-5",
		SelectedAccountID: 7,
		Candidates: []provider.SelectionCandidate{
			{ID: 7, DisplayName: "Busy", AccountType: provider.AccountTypeAPIUpstream, Priority: 1, ScheduleRank: 1, Selected: true},
			{ID: 8, DisplayName: "Inherited", AccountType: provider.AccountTypeCodexOAuth, Priority: 2, ScheduleRank: 2},
		},
	}
	providers.accounts = []provider.Account{
		{ID: 7, Provider: "openai", DisplayName: "Busy", MaxConcurrentRequests: 2},
		{ID: 8, Provider: "openai", DisplayName: "Inherited"},
	}
	gateway := &fakeGatewayHandler{accountConcurrency: map[int64]int{7: 2, 8: 1}}
	server := NewServer(config.Config{}, staticHealth{}, admins, providers, gateway)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/model-routing/preview?model=gpt-5", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body struct {
		DiagnosisStatus  string   `json:"diagnosisStatus"`
		DiagnosisSummary string   `json:"diagnosisSummary"`
		DiagnosisHints   []string `json:"diagnosisHints"`
		Candidates       []struct {
			ID                             int64 `json:"id"`
			CurrentConcurrentRequests      int   `json:"currentConcurrentRequests"`
			EffectiveMaxConcurrentRequests int   `json:"effectiveMaxConcurrentRequests"`
			ConcurrencyBlocked             bool  `json:"concurrencyBlocked"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Candidates) != 2 {
		t.Fatalf("candidates = %+v, want two", body.Candidates)
	}
	if body.Candidates[0].CurrentConcurrentRequests != 2 || body.Candidates[0].EffectiveMaxConcurrentRequests != 2 || !body.Candidates[0].ConcurrencyBlocked {
		t.Fatalf("first candidate concurrency = %+v, want current 2 effective 2 blocked", body.Candidates[0])
	}
	if body.Candidates[1].CurrentConcurrentRequests != 1 || body.Candidates[1].EffectiveMaxConcurrentRequests != 5 || body.Candidates[1].ConcurrencyBlocked {
		t.Fatalf("second candidate concurrency = %+v, want current 1 effective 5 not blocked", body.Candidates[1])
	}
	if body.DiagnosisStatus != "degraded" {
		t.Fatalf("diagnosis status = %q, want degraded", body.DiagnosisStatus)
	}
	if !strings.Contains(body.DiagnosisSummary, "Busy") || !strings.Contains(body.DiagnosisSummary, "concurrency") {
		t.Fatalf("diagnosis summary = %q, want selected busy account concurrency warning", body.DiagnosisSummary)
	}
	if !slices.Contains(body.DiagnosisHints, "Reduce concurrent requests or raise the selected account concurrency limit.") {
		t.Fatalf("diagnosis hints = %+v, want selected account concurrency hint", body.DiagnosisHints)
	}
}

func TestModelRoutingPreviewPassesExcludedAccountIDs(t *testing.T) {
	admins := newFakeAdminService()
	providers := newFakeProviderService()
	providers.selectionPreview = provider.SelectionPreview{
		Model:             "gpt-5",
		SessionID:         "workspace-123",
		SelectedAccountID: 9,
		Candidates: []provider.SelectionCandidate{
			{ID: 9, DisplayName: "Fallback", AccountType: provider.AccountTypeAPIUpstream, Priority: 1, ScheduleRank: 1, Selected: true},
			{ID: 7, DisplayName: "Excluded", AccountType: provider.AccountTypeCodexOAuth, Priority: 1, Schedulable: false, UnschedulableReason: "account excluded"},
		},
	}
	server := NewServer(config.Config{}, staticHealth{}, admins, providers)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/model-routing/preview?model=gpt-5&sessionId=workspace-123&excludedAccountIds=7,8", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if providers.previewModel != "gpt-5" || providers.previewSessionID != "workspace-123" {
		t.Fatalf("preview call = model:%q session:%q", providers.previewModel, providers.previewSessionID)
	}
	if !reflect.DeepEqual(providers.previewExcludedIDs, []int64{7, 8}) {
		t.Fatalf("preview excluded ids = %+v, want [7 8]", providers.previewExcludedIDs)
	}
}

func TestModelRoutingPreviewRejectsInvalidExcludedAccountIDs(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	req := httptest.NewRequest(http.MethodGet, "/api/admin/model-routing/preview?model=gpt-5&excludedAccountIds=7,abc", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "bad_request") {
		t.Fatalf("body = %q, want bad_request", recorder.Body.String())
	}
}

func TestModelRoutingPreviewReturnsBlockedCandidatesWhenNoneSchedulable(t *testing.T) {
	admins := newFakeAdminService()
	providers := newFakeProviderService()
	providers.selectionPreview = provider.SelectionPreview{
		Model:             "gpt-5",
		SelectedAccountID: 0,
		Candidates: []provider.SelectionCandidate{
			{ID: 2, DisplayName: "Missing model", AccountType: provider.AccountTypeAPIUpstream, Schedulable: false, UnschedulableReason: "model not configured"},
		},
	}
	server := NewServer(config.Config{}, staticHealth{}, admins, providers)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/model-routing/preview?model=gpt-5", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	var body struct {
		provider.SelectionPreview
		DiagnosisStatus     string `json:"diagnosisStatus"`
		DiagnosisSummary    string `json:"diagnosisSummary"`
		DiagnosisHints      []string
		BlockedReasonCounts []struct {
			Reason string `json:"reason"`
			Count  int    `json:"count"`
		} `json:"blockedReasonCounts"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.SelectedAccountID != 0 || len(body.Candidates) != 1 || body.Candidates[0].Schedulable || body.Candidates[0].UnschedulableReason != "model not configured" {
		t.Fatalf("preview = %+v, want blocked diagnostic candidate", body)
	}
	if body.DiagnosisStatus != "blocked" {
		t.Fatalf("diagnosis status = %q, want blocked", body.DiagnosisStatus)
	}
	if !strings.Contains(body.DiagnosisSummary, "No schedulable account") || !strings.Contains(body.DiagnosisSummary, "model not configured") {
		t.Fatalf("diagnosis summary = %q, want blocked reason summary", body.DiagnosisSummary)
	}
	if !slices.Contains(body.DiagnosisHints, "Configure the requested model on at least one enabled provider account.") {
		t.Fatalf("diagnosis hints = %+v, want model configuration hint", body.DiagnosisHints)
	}
	if len(body.BlockedReasonCounts) != 1 || body.BlockedReasonCounts[0].Reason != "model not configured" || body.BlockedReasonCounts[0].Count != 1 {
		t.Fatalf("blocked reason counts = %+v, want model not configured count 1", body.BlockedReasonCounts)
	}
}

func TestModelRoutingPreviewRequiresModel(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService())
	req := httptest.NewRequest(http.MethodGet, "/api/admin/model-routing/preview?sessionId=workspace-123", nil)
	req.AddCookie(&http.Cookie{Name: "n2api_admin_session", Value: "valid-session"})
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "bad_request") {
		t.Fatalf("body = %q, want bad_request", recorder.Body.String())
	}
}

func TestV1RoutesUseGatewayHandler(t *testing.T) {
	gateway := &fakeGatewayHandler{}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService(), gateway)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/models", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if !gateway.called {
		t.Fatal("gateway handler was not called")
	}
	if recorder.Body.String() != `{"object":"list","data":[]}` {
		t.Fatalf("body = %q", recorder.Body.String())
	}
}

func TestServesStaticFrontendAndSPAFallback(t *testing.T) {
	web := fstest.MapFS{
		"index.html":            {Data: []byte("<!doctype html><title>N2API</title><main>index</main>")},
		"200.html":              {Data: []byte("<!doctype html><title>N2API</title><main>fallback</main>")},
		"_app/immutable/app.js": {Data: []byte("console.log('app')")},
	}
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), newFakeProviderService(), nil, web)

	for _, tc := range []struct {
		path string
		want string
	}{
		{path: "/", want: "index"},
		{path: "/settings/provider", want: "fallback"},
		{path: "/_app/immutable/app.js", want: "console.log('app')"},
	} {
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", tc.path, recorder.Code)
		}
		if !strings.Contains(recorder.Body.String(), tc.want) {
			t.Fatalf("%s body = %q, want %q", tc.path, recorder.Body.String(), tc.want)
		}
	}

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodHead, "/", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("HEAD / status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.Contains(contentType, "text/html") {
		t.Fatalf("HEAD / Content-Type = %q, want text/html", contentType)
	}
}

func TestBadAdminJSONReturnsBadRequest(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(`{`)))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestAdminJSONWithTrailingGarbageReturnsBadRequest(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil)
	recorder := httptest.NewRecorder()
	body := strings.NewReader(`{"username":"admin","password":"secret"} garbage`)

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/admin/login", body))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestAdminJSONWithSecondValueReturnsBadRequest(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil)
	recorder := httptest.NewRecorder()
	body := strings.NewReader(`{"username":"admin","password":"secret"} {}`)

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/admin/login", body))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestUnknownAdminPathReturnsJSONNotFound(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/missing", nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type = %q, want application/json", got)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "not_found" {
		t.Fatalf("error = %q, want not_found", body["error"])
	}
}

func TestAdminRootPathReturnsJSONNotFoundWithoutRedirect(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin", nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	if got := recorder.Header().Get("Location"); got != "" {
		t.Fatalf("Location = %q, want empty", got)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type = %q, want application/json", got)
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "not_found" {
		t.Fatalf("error = %q, want not_found", body["error"])
	}
}

func TestWrongMethodAdminPathDoesNotReturnRootFallback(t *testing.T) {
	server := NewServer(config.Config{}, staticHealth{}, newFakeAdminService(), nil)
	recorder := httptest.NewRecorder()

	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/login", nil))

	if recorder.Code == http.StatusOK {
		t.Fatalf("status = 200, want non-200")
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type = %q, want application/json", got)
	}
}

func (s *fakeAdminService) GetOpsErrorStats(_ context.Context, _ time.Time) (admin.OpsErrorStats, error) {
	return admin.OpsErrorStats{}, nil
}

func (s *fakeAdminService) GetOpsThroughputTrend(_ context.Context, _ time.Time, _ string) (admin.OpsThroughputTrend, error) {
	return admin.OpsThroughputTrend{}, nil
}

func (s *fakeAdminService) GetOpsErrorTrend(_ context.Context, _ time.Time, _ string) (admin.OpsErrorTrend, error) {
	return admin.OpsErrorTrend{}, nil
}

func (s *fakeAdminService) GetOpsLatencyDistribution(_ context.Context, _ time.Time) (admin.OpsLatencyDistribution, error) {
	return admin.OpsLatencyDistribution{}, nil
}

func (s *fakeAdminService) GetOpsAccountHealth(_ context.Context, since time.Time) (admin.OpsAccountHealth, error) {
	s.opsAccountSince = since
	return s.opsAccountHealth, nil
}

func (s *fakeAdminService) ListOpsAccountTests(_ context.Context, since time.Time, limit int) ([]admin.OpsAccountTest, error) {
	s.opsAccountTestsSince = since
	s.opsAccountTestsLimit = limit
	return s.opsAccountTests, nil
}

func (s *fakeAdminService) GetOpsCostBreakdown(_ context.Context, since time.Time) (admin.OpsCostBreakdown, error) {
	s.opsCostSince = since
	return s.opsCostBreakdown, nil
}

func (s *fakeAdminService) ListFingerprintProfiles(_ context.Context) ([]admin.FingerprintProfile, error) {
	return []admin.FingerprintProfile{{
		ID:             8,
		SystemKey:      "codex_cli_default",
		Name:           "Chrome",
		Description:    "browser preset",
		UserAgent:      "Mozilla/5.0",
		TLSFingerprint: "chrome",
		Headers:        map[string]string{"X-Test": "1"},
		Enabled:        true,
	}}, nil
}

func (s *fakeAdminService) CreateFingerprintProfile(_ context.Context, input admin.FingerprintProfileInput) (admin.FingerprintProfile, error) {
	s.fingerprintInput = input
	if s.fingerprintErr != nil {
		return admin.FingerprintProfile{}, s.fingerprintErr
	}
	return admin.FingerprintProfile{ID: 9, Name: input.Name, Description: input.Description, UserAgent: input.UserAgent, TLSFingerprint: input.TLSFingerprint, Headers: input.Headers, Enabled: input.Enabled}, nil
}

func (s *fakeAdminService) UpdateFingerprintProfile(_ context.Context, id int64, input admin.FingerprintProfileInput) (admin.FingerprintProfile, error) {
	s.fingerprintID = id
	s.fingerprintInput = input
	if s.fingerprintErr != nil {
		return admin.FingerprintProfile{}, s.fingerprintErr
	}
	return admin.FingerprintProfile{ID: id, Name: input.Name, Description: input.Description, UserAgent: input.UserAgent, TLSFingerprint: input.TLSFingerprint, Headers: input.Headers, Enabled: input.Enabled}, nil
}

func (s *fakeAdminService) DeleteFingerprintProfile(_ context.Context, id int64) error {
	s.fingerprintID = id
	return s.fingerprintErr
}

func (s *fakeAdminService) ListErrorPassthroughRules(_ context.Context) ([]admin.ErrorPassthroughRule, error) {
	return []admin.ErrorPassthroughRule{{ID: 1, Pattern: "insufficient_quota", MatchType: "error_code", Enabled: true}}, nil
}

func (s *fakeAdminService) CreateErrorPassthroughRule(_ context.Context, input admin.ErrorPassthroughRuleInput) (admin.ErrorPassthroughRule, error) {
	s.errorRuleInput = input
	if s.errorRuleErr != nil {
		return admin.ErrorPassthroughRule{}, s.errorRuleErr
	}
	return admin.ErrorPassthroughRule{ID: 2, Pattern: input.Pattern, MatchType: input.MatchType, Description: input.Description, Enabled: input.Enabled, Priority: input.Priority}, nil
}

func (s *fakeAdminService) UpdateErrorPassthroughRule(_ context.Context, id int64, input admin.ErrorPassthroughRuleInput) (admin.ErrorPassthroughRule, error) {
	s.errorRuleID = id
	s.errorRuleInput = input
	if s.errorRuleErr != nil {
		return admin.ErrorPassthroughRule{}, s.errorRuleErr
	}
	return admin.ErrorPassthroughRule{ID: id, Pattern: input.Pattern, MatchType: input.MatchType, Description: input.Description, Enabled: input.Enabled, Priority: input.Priority}, nil
}

func (s *fakeAdminService) DeleteErrorPassthroughRule(_ context.Context, id int64) error {
	s.errorRuleID = id
	return s.errorRuleErr
}

func TestAdminSyncOfficialUsagePricingRequiresAuth(t *testing.T) {
	admins := newFakeAdminService()
	srv := NewServer(config.Config{}, staticHealth{}, admins, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/usage-pricing/sync-official", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAdminSyncOfficialUsagePricingReturnsPricingAndSummary(t *testing.T) {
	admins := newFakeAdminService()
	admins.syncOfficialPricing = admin.UsagePricing{
		Version:  1,
		Currency: "USD",
		Unit:     "1M_tokens",
		Models: map[string]admin.UsagePrice{
			"gpt-5.5": {InputMicrousdPerMillion: 5_000_000, CachedInputMicrousdPerMillion: 500_000, OutputMicrousdPerMillion: 30_000_000},
		},
	}
	admins.syncOfficialSummary = admin.UsagePricingSyncSummary{Total: 1, Sources: admin.UsagePricingSyncSources{Pricing: "https://developers.openai.com/api/docs/pricing"}}

	srv := NewServer(config.Config{}, staticHealth{}, admins, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/usage-pricing/sync-official", nil)
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body struct {
		Pricing admin.UsagePricing            `json:"pricing"`
		Synced  admin.UsagePricingSyncSummary `json:"synced"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Pricing.Models["gpt-5.5"].InputMicrousdPerMillion != 5_000_000 {
		t.Errorf("pricing.gpt-5.5.input = %d", body.Pricing.Models["gpt-5.5"].InputMicrousdPerMillion)
	}
	if body.Synced.Total != 1 {
		t.Errorf("synced.total = %d, want 1", body.Synced.Total)
	}
	if body.Synced.Sources.Pricing == "" {
		t.Error("synced.sources.pricing is empty")
	}
}

func TestAdminRemoveShutdownUsagePricingRequiresAuth(t *testing.T) {
	admins := newFakeAdminService()
	srv := NewServer(config.Config{}, staticHealth{}, admins, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/usage-pricing/remove-shutdown", strings.NewReader(`{"models":["gpt-4-0314"]}`))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAdminRemoveShutdownUsagePricingReturnsRemovedModels(t *testing.T) {
	admins := newFakeAdminService()
	admins.removeShutdownPricing = admin.UsagePricing{
		Version: 1, Currency: "USD", Unit: "1M_tokens",
		Models: map[string]admin.UsagePrice{"gpt-5.5": {InputMicrousdPerMillion: 5_000_000}},
	}
	admins.removeShutdownRemoved = []string{"gpt-4-0314"}
	srv := NewServer(config.Config{}, staticHealth{}, admins, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/usage-pricing/remove-shutdown", strings.NewReader(`{"models":["gpt-4-0314"]}`))
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got, want := admins.removeShutdownModels, []string{"gpt-4-0314"}; !slices.Equal(got, want) {
		t.Fatalf("models = %v, want %v", got, want)
	}
	var body struct {
		Pricing admin.UsagePricing `json:"pricing"`
		Removed []string           `json:"removed"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(body.Removed, []string{"gpt-4-0314"}) {
		t.Fatalf("removed = %v", body.Removed)
	}
}

func TestAdminRemoveShutdownUsagePricingMapsInvalidInputTo400(t *testing.T) {
	admins := newFakeAdminService()
	admins.removeShutdownErr = admin.ErrInvalidInput
	srv := NewServer(config.Config{}, staticHealth{}, admins, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/usage-pricing/remove-shutdown", strings.NewReader(`{"models":["gpt-5.3-chat-latest"]}`))
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestAdminRemoveShutdownUsagePricingRejectsMalformedJSON(t *testing.T) {
	admins := newFakeAdminService()
	srv := NewServer(config.Config{}, staticHealth{}, admins, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/usage-pricing/remove-shutdown", strings.NewReader(`{"models":`))
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "bad_request" {
		t.Fatalf("error = %v, want bad_request", body["error"])
	}
}

func TestAdminIgnoreUpcomingUsagePricingRequiresAuth(t *testing.T) {
	admins := newFakeAdminService()
	srv := NewServer(config.Config{}, staticHealth{}, admins, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/usage-pricing/ignore-upcoming", strings.NewReader(`{"models":["gpt-5.3-chat-latest"]}`))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAdminIgnoreUpcomingUsagePricingReturnsIgnoredModels(t *testing.T) {
	admins := newFakeAdminService()
	admins.ignoreUpcomingPricing = admin.UsagePricing{
		Version:       1,
		Currency:      "USD",
		Unit:          "1M_tokens",
		Models:        map[string]admin.UsagePrice{"local-model": {}},
		IgnoredModels: []string{"gpt-5.3-chat-latest"},
	}
	admins.ignoreUpcomingIgnored = []string{"gpt-5.3-chat-latest"}
	srv := NewServer(config.Config{}, staticHealth{}, admins, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/usage-pricing/ignore-upcoming", strings.NewReader(`{"models":["gpt-5.3-chat-latest"]}`))
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got, want := admins.ignoreUpcomingModels, []string{"gpt-5.3-chat-latest"}; !slices.Equal(got, want) {
		t.Fatalf("models = %v, want %v", got, want)
	}
	var body struct {
		Pricing admin.UsagePricing `json:"pricing"`
		Ignored []string           `json:"ignored"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(body.Ignored, []string{"gpt-5.3-chat-latest"}) {
		t.Fatalf("ignored = %v", body.Ignored)
	}
	if !slices.Equal(body.Pricing.IgnoredModels, []string{"gpt-5.3-chat-latest"}) {
		t.Fatalf("pricing ignored models = %v", body.Pricing.IgnoredModels)
	}
}

func TestAdminIgnoreUpcomingUsagePricingMapsInvalidInputTo400(t *testing.T) {
	admins := newFakeAdminService()
	admins.ignoreUpcomingErr = admin.ErrInvalidInput
	srv := NewServer(config.Config{}, staticHealth{}, admins, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/usage-pricing/ignore-upcoming", strings.NewReader(`{"models":["gpt-5.3-chat-latest"]}`))
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestAdminIgnoreUpcomingUsagePricingRejectsMalformedJSON(t *testing.T) {
	admins := newFakeAdminService()
	srv := NewServer(config.Config{}, staticHealth{}, admins, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/usage-pricing/ignore-upcoming", strings.NewReader(`{"models":`))
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "bad_request" {
		t.Fatalf("error = %v, want bad_request", body["error"])
	}
}

func TestAdminSyncOfficialUsagePricingMapsInvalidInputTo400(t *testing.T) {
	admins := newFakeAdminService()
	admins.syncOfficialErr = admin.ErrInvalidInput

	srv := NewServer(config.Config{}, staticHealth{}, admins, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/usage-pricing/sync-official", nil)
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "valid-session"})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	var body map[string]any
	json.NewDecoder(rec.Body).Decode(&body)
	if v, _ := body["error"].(string); v != "invalid_input" {
		t.Errorf("error = %q, want invalid_input", v)
	}
}
