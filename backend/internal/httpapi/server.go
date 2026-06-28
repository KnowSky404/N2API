package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/KnowSky404/N2API/backend/internal/config"
	"github.com/KnowSky404/N2API/backend/internal/provider"
)

const adminSessionCookieName = "n2api_admin_session"

type HealthChecker interface {
	Ping(ctx context.Context) error
}

type AdminService interface {
	Login(ctx context.Context, username, password string) (admin.Session, error)
	Logout(ctx context.Context, token string) error
	ValidateSession(ctx context.Context, token string) (admin.Admin, error)
	ListAPIKeys(ctx context.Context) ([]admin.APIKey, error)
	CreateAPIKey(ctx context.Context, name string) (admin.CreatedAPIKey, error)
	RevokeAPIKey(ctx context.Context, id int64) (admin.APIKey, error)
	UpdateAPIKeyName(ctx context.Context, id int64, name string) (admin.APIKey, error)
	SetAPIKeyDisabled(ctx context.Context, id int64, disabled bool) (admin.APIKey, error)
	UpdateAPIKeyModelPolicy(ctx context.Context, id int64, policy string, models []string) (admin.APIKey, error)
	UpdateAPIKeyLimits(ctx context.Context, id int64, requestsPerMinute, tokensPerMinute int) (admin.APIKey, error)
	UpdateAPIKeyBudgets(ctx context.Context, id int64, requestBudget24h, tokenBudget24h, requestBudget30d, tokenBudget30d int) (admin.APIKey, error)
	ListRoutingPools(ctx context.Context) ([]admin.RoutingPool, error)
	CreateRoutingPool(ctx context.Context, name, description string, enabled bool, fallbackPoolID *int64) (admin.RoutingPool, error)
	UpdateRoutingPool(ctx context.Context, id int64, name, description string, enabled bool, fallbackPoolID *int64) (admin.RoutingPool, error)
	DeleteRoutingPool(ctx context.Context, id int64) error
	ReplaceRoutingPoolAccounts(ctx context.Context, id int64, accounts []admin.RoutingPoolAccount) (admin.RoutingPool, error)
	UpdateAPIKeyRoutingPool(ctx context.Context, id int64, routingPoolID *int64) (admin.APIKey, error)
	GetAPIKeyBudgetUsage(ctx context.Context, key admin.APIKey, now time.Time) (admin.APIKeyBudgetUsage, error)
	ListRequestLogs(ctx context.Context, filter admin.RequestLogFilter) ([]admin.RequestLog, error)
	CleanupRequestLogs(ctx context.Context, now time.Time) (admin.RequestLogCleanupResult, error)
	GetUsageSummary(ctx context.Context, rangeName, groupBy string) (admin.UsageSummary, error)
	GetUsagePricing(ctx context.Context) (admin.UsagePricing, error)
	UpdateUsagePricing(ctx context.Context, pricing admin.UsagePricing) (admin.UsagePricing, error)
	GetModelSettings(ctx context.Context) (admin.ModelSettings, error)
	UpdateModelSettings(ctx context.Context, settings admin.ModelSettings) (admin.ModelSettings, error)
	GetGatewaySettings(ctx context.Context) (admin.GatewaySettings, error)
	UpdateGatewaySettings(ctx context.Context, settings admin.GatewaySettings) (admin.GatewaySettings, error)
	GetOpsErrorStats(ctx context.Context, since time.Time) (admin.OpsErrorStats, error)
	GetOpsThroughputTrend(ctx context.Context, since time.Time, interval string) (admin.OpsThroughputTrend, error)
	GetOpsErrorTrend(ctx context.Context, since time.Time, interval string) (admin.OpsErrorTrend, error)
	GetOpsLatencyDistribution(ctx context.Context, since time.Time) (admin.OpsLatencyDistribution, error)
	GetOpsAccountHealth(ctx context.Context, since time.Time) (admin.OpsAccountHealth, error)
	ListOpsAccountTests(ctx context.Context, since time.Time, limit int) ([]admin.OpsAccountTest, error)
	ListFingerprintProfiles(ctx context.Context) ([]admin.FingerprintProfile, error)
	CreateFingerprintProfile(ctx context.Context, input admin.FingerprintProfileInput) (admin.FingerprintProfile, error)
	UpdateFingerprintProfile(ctx context.Context, id int64, input admin.FingerprintProfileInput) (admin.FingerprintProfile, error)
	DeleteFingerprintProfile(ctx context.Context, id int64) error
	ListErrorPassthroughRules(ctx context.Context) ([]admin.ErrorPassthroughRule, error)
	CreateErrorPassthroughRule(ctx context.Context, input admin.ErrorPassthroughRuleInput) (admin.ErrorPassthroughRule, error)
	UpdateErrorPassthroughRule(ctx context.Context, id int64, input admin.ErrorPassthroughRuleInput) (admin.ErrorPassthroughRule, error)
	DeleteErrorPassthroughRule(ctx context.Context, id int64) error
	DefaultModel(ctx context.Context) (string, error)
	IsModelAllowed(ctx context.Context, model string) (bool, error)
}

type ProviderService interface {
	Status(ctx context.Context) (provider.Status, error)
	ListAccounts(ctx context.Context) ([]provider.Account, error)
	CreateAPIUpstreamAccount(ctx context.Context, input provider.APIUpstreamInput) (provider.Account, error)
	StartConnect(ctx context.Context, options provider.ConnectOptions) (provider.ConnectResult, error)
	CompleteCallback(ctx context.Context, code, state string) (provider.Account, error)
	UpdateAccount(ctx context.Context, id int64, update provider.AccountUpdate) (provider.Account, error)
	ListAccountModels(ctx context.Context, accountID int64) ([]provider.AccountModel, error)
	ReplaceAccountModels(ctx context.Context, accountID int64, models []provider.AccountModelInput) ([]provider.AccountModel, error)
	ListExposedModels(ctx context.Context, allowedModels []string) ([]provider.ExposedModel, error)
	PreviewAccountSelection(ctx context.Context, model, sessionID string, excludedAccountIDs ...int64) (provider.SelectionPreview, error)
	PreviewAccountSelectionInRoutingPool(ctx context.Context, routingPoolID int64, model, sessionID string, excludedAccountIDs ...int64) (provider.SelectionPreview, error)
	RefreshAccount(ctx context.Context, id int64) (provider.Account, error)
	TestAccount(ctx context.Context, id int64) (provider.Account, error)
	TestAccounts(ctx context.Context) ([]provider.Account, error)
	ListAccountTestResults(ctx context.Context, accountID int64, limit int) ([]provider.AccountTestResult, error)
	PauseAccountScheduling(ctx context.Context, id int64, duration time.Duration) (provider.Account, error)
	ResetAccountStatus(ctx context.Context, id int64) (provider.Account, error)
	DisconnectAccount(ctx context.Context, id int64) error
	Disconnect(ctx context.Context) error
}

type ProviderAccountAutoTestStatusSource interface {
	ProviderAccountAutoTestStatus() provider.AutoTestStatus
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

type gatewaySettingsResponse struct {
	admin.GatewaySettings
	ProviderAccountAutoTestStatus provider.AutoTestStatus `json:"providerAccountAutoTestStatus,omitempty"`
}

type apiKeyResponse struct {
	admin.APIKey
	admin.APIKeyBudgetUsage
	CurrentConcurrentRequests      int  `json:"currentConcurrentRequests"`
	EffectiveMaxConcurrentRequests int  `json:"effectiveMaxConcurrentRequests"`
	ConcurrencyBlocked             bool `json:"concurrencyBlocked"`
	CurrentRequestsThisMinute      int  `json:"currentRequestsThisMinute"`
	EffectiveRequestsPerMinute     int  `json:"effectiveRequestsPerMinute"`
	RequestRateRemaining           int  `json:"requestRateRemaining"`
	RequestRateLimited             bool `json:"requestRateLimited"`
	CurrentTokensThisMinute        int  `json:"currentTokensThisMinute"`
	EffectiveTokensPerMinute       int  `json:"effectiveTokensPerMinute"`
	TokenRateRemaining             int  `json:"tokenRateRemaining"`
	TokenRateLimited               bool `json:"tokenRateLimited"`
}

type providerAccountResponse struct {
	provider.Account
	CurrentConcurrentRequests      int `json:"currentConcurrentRequests"`
	EffectiveMaxConcurrentRequests int `json:"effectiveMaxConcurrentRequests"`
}

type selectionPreviewResponse struct {
	provider.SelectionPreview
	DiagnosisStatus     string                        `json:"diagnosisStatus"`
	DiagnosisSummary    string                        `json:"diagnosisSummary"`
	DiagnosisHints      []string                      `json:"diagnosisHints"`
	BlockedReasonCounts []selectionBlockedReasonCount `json:"blockedReasonCounts"`
	Candidates          []selectionCandidateResponse  `json:"candidates"`
}

type selectionCandidateResponse struct {
	provider.SelectionCandidate
	CurrentConcurrentRequests      int  `json:"currentConcurrentRequests"`
	EffectiveMaxConcurrentRequests int  `json:"effectiveMaxConcurrentRequests"`
	ConcurrencyBlocked             bool `json:"concurrencyBlocked"`
}

type selectionBlockedReasonCount struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

func NewServer(cfg config.Config, health HealthChecker, admins AdminService, providers ProviderService, options ...any) http.Handler {
	mux := http.NewServeMux()
	secureCookie := strings.HasPrefix(cfg.PublicURL, "https://")
	gateway, webFS, autoTestStatusSource := parseServerOptions(options...)
	accountConcurrencySource, _ := gateway.(AccountConcurrencySnapshotProvider)
	apiKeyConcurrencySource, _ := gateway.(APIKeyConcurrencySnapshotProvider)
	apiKeyRateSource, _ := gateway.(APIKeyRateSnapshotProvider)

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("GET /api/admin/health", func(w http.ResponseWriter, r *http.Request) {
		if health == nil {
			writeJSON(w, http.StatusOK, map[string]string{
				"status":   "ok",
				"database": "not_configured",
			})
			return
		}

		if err := health.Ping(r.Context()); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status":   "degraded",
				"database": "error",
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"status":   "ok",
			"database": "ok",
		})
	})

	mux.HandleFunc("GET /api/admin/bootstrap", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"publicUrl":     cfg.PublicURL,
			"adminUsername": cfg.AdminUsername,
		})
	})

	mux.HandleFunc("POST /api/admin/login", func(w http.ResponseWriter, r *http.Request) {
		if admins == nil {
			writeError(w, http.StatusServiceUnavailable, "service_unavailable")
			return
		}

		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}

		session, err := admins.Login(r.Context(), req.Username, req.Password)
		if err != nil {
			if errors.Is(err, admin.ErrUnauthorized) {
				writeError(w, http.StatusUnauthorized, "invalid_credentials")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}

		setSessionCookie(w, session.Token, session.ExpiresAt, secureCookie)
		writeJSON(w, http.StatusOK, map[string]string{"username": req.Username})
	})

	mux.HandleFunc("POST /api/admin/logout", func(w http.ResponseWriter, r *http.Request) {
		if admins == nil {
			writeError(w, http.StatusServiceUnavailable, "service_unavailable")
			return
		}

		token, ok := readSessionCookie(r)
		if ok {
			if err := admins.Logout(r.Context(), token); err != nil {
				writeError(w, http.StatusInternalServerError, "internal_error")
				return
			}
		}
		clearSessionCookie(w, secureCookie)
		w.WriteHeader(http.StatusNoContent)
	})

	requireAdmin := func(next func(http.ResponseWriter, *http.Request, admin.Admin)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if admins == nil {
				writeError(w, http.StatusServiceUnavailable, "service_unavailable")
				return
			}
			token, ok := readSessionCookie(r)
			if !ok {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			currentAdmin, err := admins.ValidateSession(r.Context(), token)
			if err != nil {
				if errors.Is(err, admin.ErrUnauthorized) {
					writeError(w, http.StatusUnauthorized, "unauthorized")
					return
				}
				writeError(w, http.StatusInternalServerError, "internal_error")
				return
			}
			next(w, r, currentAdmin)
		}
	}

	mux.HandleFunc("GET /api/admin/me", requireAdmin(func(w http.ResponseWriter, r *http.Request, currentAdmin admin.Admin) {
		writeJSON(w, http.StatusOK, map[string]string{"username": currentAdmin.Username})
	}))

	mux.HandleFunc("GET /api/admin/keys", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		keys, err := admins.ListAPIKeys(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		settings, err := admins.GetGatewaySettings(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		concurrency := map[int64]int{}
		if apiKeyConcurrencySource != nil {
			concurrency = apiKeyConcurrencySource.APIKeyConcurrencySnapshot()
		}
		requestRate := map[int64]int{}
		tokenRate := map[int64]int{}
		if apiKeyRateSource != nil {
			requestRate = apiKeyRateSource.APIKeyRequestRateSnapshot()
			tokenRate = apiKeyRateSource.APIKeyTokenRateSnapshot()
		}
		budgetUsage := map[int64]admin.APIKeyBudgetUsage{}
		now := time.Now()
		for _, key := range keys {
			usage, err := admins.GetAPIKeyBudgetUsage(r.Context(), key, now)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal_error")
				return
			}
			budgetUsage[key.ID] = usage
		}
		writeJSON(w, http.StatusOK, map[string][]apiKeyResponse{
			"keys": apiKeyResponses(keys, budgetUsage, settings, concurrency, requestRate, tokenRate),
		})
	}))

	mux.HandleFunc("POST /api/admin/keys", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		var req struct {
			Name string `json:"name"`
		}
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}

		created, err := admins.CreateAPIKey(r.Context(), req.Name)
		if err != nil {
			if errors.Is(err, admin.ErrInvalidInput) {
				writeError(w, http.StatusBadRequest, "invalid_input")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}

		writeJSON(w, http.StatusCreated, map[string]any{
			"key":    created.Key,
			"secret": created.Secret,
		})
	}))

	mux.HandleFunc("POST /api/admin/keys/{id}/revoke", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}

		key, err := admins.RevokeAPIKey(r.Context(), id)
		if err != nil {
			if errors.Is(err, admin.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]admin.APIKey{"key": key})
	}))

	mux.HandleFunc("PATCH /api/admin/keys/{id}", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		id, err := parsePositivePathID(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}

		var req struct {
			Name string `json:"name"`
		}
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		key, err := admins.UpdateAPIKeyName(r.Context(), id, req.Name)
		if err != nil {
			if errors.Is(err, admin.ErrInvalidInput) {
				writeError(w, http.StatusBadRequest, "invalid_input")
				return
			}
			if errors.Is(err, admin.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]admin.APIKey{"key": key})
	}))

	mux.HandleFunc("PUT /api/admin/keys/{id}/disabled", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		id, err := parsePositivePathID(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}

		var req struct {
			Disabled bool `json:"disabled"`
		}
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		key, err := admins.SetAPIKeyDisabled(r.Context(), id, req.Disabled)
		if err != nil {
			if errors.Is(err, admin.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]admin.APIKey{"key": key})
	}))

	mux.HandleFunc("PUT /api/admin/keys/{id}/model-policy", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		id, err := parsePositivePathID(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}

		var req struct {
			ModelPolicy string   `json:"modelPolicy"`
			Models      []string `json:"models"`
		}
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		key, err := admins.UpdateAPIKeyModelPolicy(r.Context(), id, req.ModelPolicy, req.Models)
		if err != nil {
			if errors.Is(err, admin.ErrInvalidInput) {
				writeError(w, http.StatusBadRequest, "invalid_input")
				return
			}
			if errors.Is(err, admin.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]admin.APIKey{"key": key})
	}))

	mux.HandleFunc("PUT /api/admin/keys/{id}/limits", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		id, err := parsePositivePathID(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}

		var req struct {
			RequestsPerMinute int `json:"requestsPerMinute"`
			TokensPerMinute   int `json:"tokensPerMinute"`
		}
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		key, err := admins.UpdateAPIKeyLimits(r.Context(), id, req.RequestsPerMinute, req.TokensPerMinute)
		if err != nil {
			if errors.Is(err, admin.ErrInvalidInput) {
				writeError(w, http.StatusBadRequest, "invalid_input")
				return
			}
			if errors.Is(err, admin.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]admin.APIKey{"key": key})
	}))

	mux.HandleFunc("PUT /api/admin/keys/{id}/budgets", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		id, err := parsePositivePathID(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}

		var req struct {
			RequestBudget24h int `json:"requestBudget24h"`
			TokenBudget24h   int `json:"tokenBudget24h"`
			RequestBudget30d int `json:"requestBudget30d"`
			TokenBudget30d   int `json:"tokenBudget30d"`
		}
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		key, err := admins.UpdateAPIKeyBudgets(r.Context(), id, req.RequestBudget24h, req.TokenBudget24h, req.RequestBudget30d, req.TokenBudget30d)
		if err != nil {
			if errors.Is(err, admin.ErrInvalidInput) {
				writeError(w, http.StatusBadRequest, "invalid_input")
				return
			}
			if errors.Is(err, admin.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]admin.APIKey{"key": key})
	}))

	mux.HandleFunc("PUT /api/admin/keys/{id}/routing-pool", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		id, err := parsePositivePathID(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}

		var req struct {
			RoutingPoolID *int64 `json:"routingPoolId"`
		}
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		key, err := admins.UpdateAPIKeyRoutingPool(r.Context(), id, req.RoutingPoolID)
		if err != nil {
			if errors.Is(err, admin.ErrInvalidInput) {
				writeError(w, http.StatusBadRequest, "invalid_input")
				return
			}
			if errors.Is(err, admin.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]admin.APIKey{"key": key})
	}))

	mux.HandleFunc("GET /api/admin/routing-pools", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		pools, err := admins.ListRoutingPools(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string][]admin.RoutingPool{"pools": pools})
	}))

	mux.HandleFunc("POST /api/admin/routing-pools", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		var req struct {
			Name           string `json:"name"`
			Description    string `json:"description"`
			Enabled        bool   `json:"enabled"`
			FallbackPoolID *int64 `json:"fallbackPoolId"`
		}
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		pool, err := admins.CreateRoutingPool(r.Context(), req.Name, req.Description, req.Enabled, req.FallbackPoolID)
		if err != nil {
			if errors.Is(err, admin.ErrInvalidInput) {
				writeError(w, http.StatusBadRequest, "invalid_input")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]admin.RoutingPool{"pool": pool})
	}))

	mux.HandleFunc("PATCH /api/admin/routing-pools/{id}", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		id, err := parsePositivePathID(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		var req struct {
			Name           string `json:"name"`
			Description    string `json:"description"`
			Enabled        bool   `json:"enabled"`
			FallbackPoolID *int64 `json:"fallbackPoolId"`
		}
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		pool, err := admins.UpdateRoutingPool(r.Context(), id, req.Name, req.Description, req.Enabled, req.FallbackPoolID)
		if err != nil {
			if errors.Is(err, admin.ErrInvalidInput) {
				writeError(w, http.StatusBadRequest, "invalid_input")
				return
			}
			if errors.Is(err, admin.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]admin.RoutingPool{"pool": pool})
	}))

	mux.HandleFunc("DELETE /api/admin/routing-pools/{id}", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		id, err := parsePositivePathID(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		if err := admins.DeleteRoutingPool(r.Context(), id); err != nil {
			if errors.Is(err, admin.ErrInvalidInput) {
				writeError(w, http.StatusBadRequest, "invalid_input")
				return
			}
			if errors.Is(err, admin.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	mux.HandleFunc("PUT /api/admin/routing-pools/{id}/accounts", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		id, err := parsePositivePathID(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		var req struct {
			Accounts []admin.RoutingPoolAccount `json:"accounts"`
		}
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		pool, err := admins.ReplaceRoutingPoolAccounts(r.Context(), id, req.Accounts)
		if err != nil {
			if errors.Is(err, admin.ErrInvalidInput) {
				writeError(w, http.StatusBadRequest, "invalid_input")
				return
			}
			if errors.Is(err, admin.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]admin.RoutingPool{"pool": pool})
	}))

	mux.HandleFunc("GET /api/admin/request-logs", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		limit := 0
		if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
			parsed, err := strconv.Atoi(rawLimit)
			if err != nil {
				writeError(w, http.StatusBadRequest, "bad_request")
				return
			}
			limit = parsed
		}
		var providerAccountID int64
		if rawAccountID := r.URL.Query().Get("providerAccountId"); rawAccountID != "" {
			parsed, err := strconv.ParseInt(rawAccountID, 10, 64)
			if err != nil || parsed < 1 {
				writeError(w, http.StatusBadRequest, "invalid_input")
				return
			}
			providerAccountID = parsed
		}
		var routingPoolID int64
		if rawRoutingPoolID := r.URL.Query().Get("routingPoolId"); rawRoutingPoolID != "" {
			parsed, err := strconv.ParseInt(rawRoutingPoolID, 10, 64)
			if err != nil || parsed < 1 {
				writeError(w, http.StatusBadRequest, "invalid_input")
				return
			}
			routingPoolID = parsed
		}
		var clientKeyID int64
		if rawClientKeyID := r.URL.Query().Get("clientKeyId"); rawClientKeyID != "" {
			parsed, err := strconv.ParseInt(rawClientKeyID, 10, 64)
			if err != nil || parsed < 1 {
				writeError(w, http.StatusBadRequest, "invalid_input")
				return
			}
			clientKeyID = parsed
		}
		statusCode := 0
		if rawStatusCode := r.URL.Query().Get("statusCode"); rawStatusCode != "" {
			parsed, err := strconv.Atoi(rawStatusCode)
			if err != nil || parsed < 100 || parsed > 599 {
				writeError(w, http.StatusBadRequest, "invalid_input")
				return
			}
			statusCode = parsed
		}
		gatewayFallbacks := false
		if rawGatewayFallbacks := r.URL.Query().Get("gatewayFallbacks"); rawGatewayFallbacks != "" {
			switch rawGatewayFallbacks {
			case "1", "true":
				gatewayFallbacks = true
			case "0", "false":
				gatewayFallbacks = false
			default:
				writeError(w, http.StatusBadRequest, "invalid_input")
				return
			}
		}
		since := parseSinceParam(r)
		filter := admin.RequestLogFilter{
			Limit:             limit,
			Since:             since,
			RequestID:         r.URL.Query().Get("requestId"),
			Query:             r.URL.Query().Get("q"),
			StatusClass:       r.URL.Query().Get("statusClass"),
			StatusCode:        statusCode,
			ProviderAccountID: providerAccountID,
			RoutingPoolID:     routingPoolID,
			ClientKeyID:       clientKeyID,
			Model:             r.URL.Query().Get("model"),
			SessionID:         r.URL.Query().Get("sessionId"),
			Error:             r.URL.Query().Get("error"),
			UsageSource:       r.URL.Query().Get("usageSource"),
			RoutingPoolError:  r.URL.Query().Get("routingPoolError"),
			RoutingPoolChain:  r.URL.Query().Get("routingPoolChain"),
			GatewayFallbacks:  gatewayFallbacks,
		}
		logs, err := admins.ListRequestLogs(r.Context(), filter)
		if err != nil {
			if errors.Is(err, admin.ErrInvalidInput) {
				writeError(w, http.StatusBadRequest, "invalid_input")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string][]admin.RequestLog{"logs": logs})
	}))

	mux.HandleFunc("GET /api/admin/request-logs/export", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleExportRequestLogs(w, r, admins)
	}))
	mux.HandleFunc("POST /api/admin/request-logs/cleanup", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		result, err := admins.CleanupRequestLogs(r.Context(), time.Now())
		if err != nil {
			if errors.Is(err, admin.ErrInvalidInput) {
				writeError(w, http.StatusBadRequest, "invalid_input")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, result)
	}))
	mux.HandleFunc("GET /api/admin/gateway-settings", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		settings, err := admins.GetGatewaySettings(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		if autoTestStatusSource == nil {
			writeJSON(w, http.StatusOK, settings)
			return
		}
		writeJSON(w, http.StatusOK, gatewaySettingsResponse{
			GatewaySettings:               settings,
			ProviderAccountAutoTestStatus: autoTestStatusSource.ProviderAccountAutoTestStatus(),
		})
	}))

	mux.HandleFunc("PUT /api/admin/gateway-settings", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		var req admin.GatewaySettings
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		settings, err := admins.UpdateGatewaySettings(r.Context(), req)
		if err != nil {
			if errors.Is(err, admin.ErrInvalidInput) {
				writeError(w, http.StatusBadRequest, "invalid_input")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, settings)
	}))

	mux.HandleFunc("GET /api/admin/usage-summary", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		summary, err := admins.GetUsageSummary(r.Context(), r.URL.Query().Get("range"), r.URL.Query().Get("groupBy"))
		if err != nil {
			if errors.Is(err, admin.ErrInvalidInput) {
				writeError(w, http.StatusBadRequest, "invalid_input")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, summary)
	}))

	mux.HandleFunc("GET /api/admin/usage-pricing", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		pricing, err := admins.GetUsagePricing(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, pricing)
	}))

	mux.HandleFunc("PUT /api/admin/usage-pricing", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		var req admin.UsagePricing
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		pricing, err := admins.UpdateUsagePricing(r.Context(), req)
		if err != nil {
			if errors.Is(err, admin.ErrInvalidInput) {
				writeError(w, http.StatusBadRequest, "invalid_input")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, pricing)
	}))

	mux.HandleFunc("GET /api/admin/ops/error-stats", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		since := parseSinceParam(r)
		stats, err := admins.GetOpsErrorStats(r.Context(), since)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, stats)
	}))

	mux.HandleFunc("GET /api/admin/ops/throughput-trend", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		since := parseSinceParam(r)
		interval := r.URL.Query().Get("interval")
		trend, err := admins.GetOpsThroughputTrend(r.Context(), since, interval)
		if err != nil {
			if errors.Is(err, admin.ErrInvalidInput) {
				writeError(w, http.StatusBadRequest, "invalid_input")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, trend)
	}))

	mux.HandleFunc("GET /api/admin/ops/error-trend", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		since := parseSinceParam(r)
		interval := r.URL.Query().Get("interval")
		trend, err := admins.GetOpsErrorTrend(r.Context(), since, interval)
		if err != nil {
			if errors.Is(err, admin.ErrInvalidInput) {
				writeError(w, http.StatusBadRequest, "invalid_input")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, trend)
	}))

	mux.HandleFunc("GET /api/admin/ops/latency-distribution", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		since := parseSinceParam(r)
		dist, err := admins.GetOpsLatencyDistribution(r.Context(), since)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, dist)
	}))

	mux.HandleFunc("GET /api/admin/ops/account-health", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		since := parseSinceParam(r)
		health, err := admins.GetOpsAccountHealth(r.Context(), since)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, health)
	}))

	mux.HandleFunc("GET /api/admin/ops/account-tests", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		since := parseSinceParam(r)
		limit, err := parseLimitParam(r, 20, 100)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		tests, err := admins.ListOpsAccountTests(r.Context(), since, limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string][]admin.OpsAccountTest{"tests": tests})
	}))

	mux.HandleFunc("GET /api/admin/fingerprint-profiles", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		profiles, err := admins.ListFingerprintProfiles(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string][]admin.FingerprintProfile{"profiles": profiles})
	}))

	mux.HandleFunc("POST /api/admin/fingerprint-profiles", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		var req admin.FingerprintProfileInput
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		profile, err := admins.CreateFingerprintProfile(r.Context(), req)
		if err != nil {
			if errors.Is(err, admin.ErrInvalidInput) {
				writeError(w, http.StatusBadRequest, "invalid_input")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]admin.FingerprintProfile{"profile": profile})
	}))

	mux.HandleFunc("PATCH /api/admin/fingerprint-profiles/{id}", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		id, err := parsePositivePathID(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		var req admin.FingerprintProfileInput
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		profile, err := admins.UpdateFingerprintProfile(r.Context(), id, req)
		if err != nil {
			if errors.Is(err, admin.ErrInvalidInput) {
				writeError(w, http.StatusBadRequest, "invalid_input")
				return
			}
			if errors.Is(err, admin.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]admin.FingerprintProfile{"profile": profile})
	}))

	mux.HandleFunc("DELETE /api/admin/fingerprint-profiles/{id}", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		id, err := parsePositivePathID(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		if err := admins.DeleteFingerprintProfile(r.Context(), id); err != nil {
			if errors.Is(err, admin.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	mux.HandleFunc("GET /api/admin/error-passthrough-rules", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		rules, err := admins.ListErrorPassthroughRules(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string][]admin.ErrorPassthroughRule{"rules": rules})
	}))

	mux.HandleFunc("POST /api/admin/error-passthrough-rules", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		var req admin.ErrorPassthroughRuleInput
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		rule, err := admins.CreateErrorPassthroughRule(r.Context(), req)
		if err != nil {
			if errors.Is(err, admin.ErrInvalidInput) {
				writeError(w, http.StatusBadRequest, "invalid_input")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]admin.ErrorPassthroughRule{"rule": rule})
	}))

	mux.HandleFunc("PATCH /api/admin/error-passthrough-rules/{id}", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		id, err := parsePositivePathID(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		var req admin.ErrorPassthroughRuleInput
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		rule, err := admins.UpdateErrorPassthroughRule(r.Context(), id, req)
		if err != nil {
			if errors.Is(err, admin.ErrInvalidInput) {
				writeError(w, http.StatusBadRequest, "invalid_input")
				return
			}
			if errors.Is(err, admin.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]admin.ErrorPassthroughRule{"rule": rule})
	}))

	mux.HandleFunc("DELETE /api/admin/error-passthrough-rules/{id}", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		id, err := parsePositivePathID(r, "id")
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		if err := admins.DeleteErrorPassthroughRule(r.Context(), id); err != nil {
			if errors.Is(err, admin.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	mux.HandleFunc("GET /api/admin/model-settings", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		settings, err := admins.GetModelSettings(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, settings)
	}))

	mux.HandleFunc("PUT /api/admin/model-settings", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		var req admin.ModelSettings
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		settings, err := admins.UpdateModelSettings(r.Context(), req)
		if err != nil {
			if errors.Is(err, admin.ErrInvalidInput) {
				writeError(w, http.StatusBadRequest, "invalid_input")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, settings)
	}))

	mux.HandleFunc("GET /api/admin/model-routing", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		if providers == nil {
			writeError(w, http.StatusServiceUnavailable, "service_unavailable")
			return
		}
		status, err := modelRoutingStatus(r.Context(), admins, providers)
		if err != nil {
			if errors.Is(err, admin.ErrInvalidInput) || errors.Is(err, provider.ErrInvalidInput) {
				writeError(w, http.StatusBadRequest, "invalid_input")
				return
			}
			if errors.Is(err, provider.ErrNotConnected) {
				writeError(w, http.StatusNotFound, "not_found")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, status)
	}))

	mux.HandleFunc("GET /api/admin/model-routing/preview", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleModelRoutingPreview(w, r, admins, providers, accountConcurrencySource)
	}))

	mux.HandleFunc("GET /api/admin/provider-accounts", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleListProviderAccounts(w, r, admins, providers, accountConcurrencySource)
	}))

	mux.HandleFunc("POST /api/admin/provider-accounts/api-upstream", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		if providers == nil {
			writeError(w, http.StatusServiceUnavailable, "service_unavailable")
			return
		}
		var req struct {
			Name                 string   `json:"name"`
			BaseURL              string   `json:"baseUrl"`
			APIKey               string   `json:"apiKey"`
			ProxyURL             string   `json:"proxyUrl"`
			Enabled              *bool    `json:"enabled"`
			Priority             int      `json:"priority"`
			LoadFactor           int      `json:"loadFactor"`
			FingerprintProfileID *int64   `json:"fingerprintProfileId"`
			Models               []string `json:"models"`
		}
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		fingerprintProfileID := req.FingerprintProfileID
		if fingerprintProfileID != nil && *fingerprintProfileID == 0 {
			fingerprintProfileID = nil
		}
		account, err := providers.CreateAPIUpstreamAccount(r.Context(), provider.APIUpstreamInput{
			Name:                 req.Name,
			BaseURL:              req.BaseURL,
			APIKey:               req.APIKey,
			ProxyURL:             req.ProxyURL,
			Enabled:              req.Enabled,
			Priority:             req.Priority,
			LoadFactor:           req.LoadFactor,
			FingerprintProfileID: fingerprintProfileID,
			Models:               req.Models,
		})
		if err != nil {
			writeProviderAccountError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]provider.Account{"account": account})
	}))

	mux.HandleFunc("POST /api/admin/provider-accounts/test", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleTestAllProviderAccounts(w, r, providers)
	}))

	mux.HandleFunc("POST /api/admin/provider-accounts/bulk-update", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleBulkUpdateProviderAccounts(w, r, providers)
	}))

	mux.HandleFunc("POST /api/admin/provider-accounts/bulk-test", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleBulkTestProviderAccounts(w, r, providers)
	}))

	mux.HandleFunc("POST /api/admin/provider-accounts/bulk-refresh", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleBulkRefreshProviderAccounts(w, r, providers)
	}))

	mux.HandleFunc("POST /api/admin/provider-accounts/bulk-disconnect", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleBulkDisconnectProviderAccounts(w, r, providers)
	}))

	mux.HandleFunc("POST /api/admin/provider-accounts/bulk-pause", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleBulkPauseProviderAccountScheduling(w, r, providers)
	}))

	mux.HandleFunc("POST /api/admin/provider-accounts/bulk-reset-status", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleBulkResetProviderAccountStatus(w, r, providers)
	}))

	mux.HandleFunc("POST /api/admin/provider-accounts/bulk-models", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleBulkReplaceProviderAccountModels(w, r, providers)
	}))

	mux.HandleFunc("GET /api/admin/provider-accounts/codex-oauth/status", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleProviderStatus(w, r, providers)
	}))

	mux.HandleFunc("POST /api/admin/provider-accounts/codex-oauth/connect", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleProviderConnect(w, r, providers)
	}))

	mux.HandleFunc("POST /api/admin/provider-accounts/codex-oauth/callback", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleProviderCallback(w, r, providers)
	}))

	mux.HandleFunc("PATCH /api/admin/provider-accounts/{id}", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handlePatchProviderAccount(w, r, providers)
	}))

	mux.HandleFunc("DELETE /api/admin/provider-accounts/{id}", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleDeleteProviderAccount(w, r, providers)
	}))

	mux.HandleFunc("POST /api/admin/provider-accounts/{id}/disconnect", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleDeleteProviderAccount(w, r, providers)
	}))

	mux.HandleFunc("GET /api/admin/provider-accounts/{id}/models", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleListProviderAccountModels(w, r, providers)
	}))

	mux.HandleFunc("PUT /api/admin/provider-accounts/{id}/models", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleReplaceProviderAccountModels(w, r, providers)
	}))

	mux.HandleFunc("POST /api/admin/provider-accounts/{id}/refresh", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleRefreshProviderAccount(w, r, providers)
	}))

	mux.HandleFunc("POST /api/admin/provider-accounts/{id}/test", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleTestProviderAccount(w, r, providers)
	}))

	mux.HandleFunc("GET /api/admin/provider-accounts/{id}/test-results", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleListProviderAccountTestResults(w, r, providers)
	}))

	mux.HandleFunc("POST /api/admin/provider-accounts/{id}/pause", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handlePauseProviderAccountScheduling(w, r, providers)
	}))

	mux.HandleFunc("POST /api/admin/provider-accounts/{id}/reset-status", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleResetProviderAccountStatus(w, r, providers)
	}))

	mux.HandleFunc("GET /api/admin/providers/openai", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleProviderStatus(w, r, providers)
	}))

	mux.HandleFunc("POST /api/admin/providers/openai/connect", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleProviderConnect(w, r, providers)
	}))

	mux.HandleFunc("POST /api/admin/providers/openai/callback", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleProviderCallback(w, r, providers)
	}))

	mux.HandleFunc("POST /api/admin/providers/openai/disconnect", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		if providers == nil {
			writeError(w, http.StatusServiceUnavailable, "service_unavailable")
			return
		}
		if err := providers.Disconnect(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	mux.HandleFunc("GET /api/admin/providers/openai/accounts", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleListProviderAccounts(w, r, admins, providers, accountConcurrencySource)
	}))

	mux.HandleFunc("PATCH /api/admin/providers/openai/accounts/{id}", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handlePatchProviderAccount(w, r, providers)
	}))

	mux.HandleFunc("GET /api/admin/providers/openai/accounts/{id}/models", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleListProviderAccountModels(w, r, providers)
	}))

	mux.HandleFunc("PUT /api/admin/providers/openai/accounts/{id}/models", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleReplaceProviderAccountModels(w, r, providers)
	}))

	mux.HandleFunc("POST /api/admin/providers/openai/accounts/{id}/refresh", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleRefreshProviderAccount(w, r, providers)
	}))

	mux.HandleFunc("POST /api/admin/providers/openai/accounts/{id}/test", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleTestProviderAccount(w, r, providers)
	}))

	mux.HandleFunc("GET /api/admin/providers/openai/accounts/{id}/test-results", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleListProviderAccountTestResults(w, r, providers)
	}))

	mux.HandleFunc("POST /api/admin/providers/openai/accounts/{id}/pause", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handlePauseProviderAccountScheduling(w, r, providers)
	}))

	mux.HandleFunc("POST /api/admin/providers/openai/accounts/{id}/reset-status", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleResetProviderAccountStatus(w, r, providers)
	}))

	mux.HandleFunc("POST /api/admin/providers/openai/accounts/{id}/disconnect", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		if providers == nil {
			writeError(w, http.StatusServiceUnavailable, "service_unavailable")
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}

		if err := providers.DisconnectAccount(r.Context(), id); err != nil {
			if errors.Is(err, provider.ErrInvalidInput) {
				writeError(w, http.StatusBadRequest, "invalid_input")
				return
			}
			if errors.Is(err, provider.ErrNotConnected) {
				writeError(w, http.StatusNotFound, "not_found")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	mux.HandleFunc("GET /oauth/openai/callback", func(w http.ResponseWriter, r *http.Request) {
		writeManualOAuthCallbackPage(w, r)
	})

	if gateway != nil {
		mux.Handle("/v1/", gateway)
	}

	mux.HandleFunc("/api/admin", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found")
	})

	mux.HandleFunc("/api/admin/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found")
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if serveWeb(w, r, webFS) {
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("N2API bootstrap server\n"))
	})

	return mux
}

func parsePositivePathID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid path id")
	}
	return id, nil
}

func parseOptionalPositiveInt64(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}

func writeProviderAccountError(w http.ResponseWriter, err error) {
	if errors.Is(err, provider.ErrInvalidInput) {
		writeError(w, http.StatusBadRequest, "invalid_input")
		return
	}
	if errors.Is(err, provider.ErrNotConnected) || errors.Is(err, admin.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	writeError(w, http.StatusInternalServerError, "internal_error")
}

func apiKeyResponses(keys []admin.APIKey, budgetUsage map[int64]admin.APIKeyBudgetUsage, settings admin.GatewaySettings, concurrency, requestRate, tokenRate map[int64]int) []apiKeyResponse {
	responses := make([]apiKeyResponse, 0, len(keys))
	effectiveMaxConcurrentRequests := settings.MaxConcurrentRequestsPerKey
	for _, key := range keys {
		currentConcurrentRequests := concurrency[key.ID]
		currentRequestsThisMinute := requestRate[key.ID]
		effectiveRequestsPerMinute := effectiveAPIKeyRateLimit(key.RequestsPerMinute, settings.RequestsPerMinutePerKey)
		requestRateRemaining, requestRateLimited := rateWindowState(currentRequestsThisMinute, effectiveRequestsPerMinute)
		currentTokensThisMinute := tokenRate[key.ID]
		effectiveTokensPerMinute := effectiveAPIKeyRateLimit(key.TokensPerMinute, settings.TokensPerMinutePerKey)
		tokenRateRemaining, tokenRateLimited := rateWindowState(currentTokensThisMinute, effectiveTokensPerMinute)
		responses = append(responses, apiKeyResponse{
			APIKey:                         key,
			APIKeyBudgetUsage:              budgetUsage[key.ID],
			CurrentConcurrentRequests:      currentConcurrentRequests,
			EffectiveMaxConcurrentRequests: effectiveMaxConcurrentRequests,
			ConcurrencyBlocked:             effectiveMaxConcurrentRequests > 0 && currentConcurrentRequests >= effectiveMaxConcurrentRequests,
			CurrentRequestsThisMinute:      currentRequestsThisMinute,
			EffectiveRequestsPerMinute:     effectiveRequestsPerMinute,
			RequestRateRemaining:           requestRateRemaining,
			RequestRateLimited:             requestRateLimited,
			CurrentTokensThisMinute:        currentTokensThisMinute,
			EffectiveTokensPerMinute:       effectiveTokensPerMinute,
			TokenRateRemaining:             tokenRateRemaining,
			TokenRateLimited:               tokenRateLimited,
		})
	}
	return responses
}

func effectiveAPIKeyRateLimit(override, fallback int) int {
	if override > 0 {
		return override
	}
	return fallback
}

func rateWindowState(current, limit int) (int, bool) {
	if limit <= 0 {
		return 0, false
	}
	remaining := limit - current
	if remaining < 0 {
		remaining = 0
	}
	return remaining, current >= limit
}

func handleListProviderAccounts(w http.ResponseWriter, r *http.Request, admins AdminService, providers ProviderService, concurrencySource AccountConcurrencySnapshotProvider) {
	if providers == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable")
		return
	}
	accounts, err := providers.ListAccounts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	settings := admin.GatewaySettings{}
	if admins != nil {
		var err error
		settings, err = admins.GetGatewaySettings(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
	}
	concurrency := map[int64]int{}
	if concurrencySource != nil {
		concurrency = concurrencySource.AccountConcurrencySnapshot()
	}
	writeJSON(w, http.StatusOK, map[string][]providerAccountResponse{
		"accounts": providerAccountResponses(accounts, settings, concurrency),
	})
}

func providerAccountResponses(accounts []provider.Account, settings admin.GatewaySettings, concurrency map[int64]int) []providerAccountResponse {
	responses := make([]providerAccountResponse, 0, len(accounts))
	for _, account := range accounts {
		effectiveMaxConcurrentRequests := effectiveAccountMaxConcurrentRequests(account.MaxConcurrentRequests, settings)
		responses = append(responses, providerAccountResponse{
			Account:                        account,
			CurrentConcurrentRequests:      concurrency[account.ID],
			EffectiveMaxConcurrentRequests: effectiveMaxConcurrentRequests,
		})
	}
	return responses
}

func effectiveAccountMaxConcurrentRequests(accountMax int, settings admin.GatewaySettings) int {
	if accountMax > 0 {
		return accountMax
	}
	return settings.MaxConcurrentRequestsPerAccount
}

func handleModelRoutingPreview(w http.ResponseWriter, r *http.Request, admins AdminService, providers ProviderService, concurrencySource AccountConcurrencySnapshotProvider) {
	if providers == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable")
		return
	}
	model := strings.TrimSpace(r.URL.Query().Get("model"))
	if model == "" {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	excludedIDs, err := parseExcludedAccountIDs(r.URL.Query().Get("excludedAccountIds"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	routingPoolID, err := parseOptionalPositiveInt64(r.URL.Query().Get("routingPoolId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	var preview provider.SelectionPreview
	if routingPoolID > 0 {
		preview, err = providers.PreviewAccountSelectionInRoutingPool(r.Context(), routingPoolID, model, r.URL.Query().Get("sessionId"), excludedIDs...)
	} else {
		preview, err = providers.PreviewAccountSelection(r.Context(), model, r.URL.Query().Get("sessionId"), excludedIDs...)
	}
	if err != nil {
		if errors.Is(err, provider.ErrInvalidInput) {
			writeError(w, http.StatusBadRequest, "invalid_input")
			return
		}
		if errors.Is(err, provider.ErrNotConfigured) {
			writeError(w, http.StatusConflict, "provider_not_configured")
			return
		}
		if errors.Is(err, provider.ErrModelUnavailable) || errors.Is(err, provider.ErrAccountsUnavailable) || errors.Is(err, provider.ErrAccountsDisabled) || errors.Is(err, provider.ErrNotConnected) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	settings := admin.GatewaySettings{}
	if admins != nil {
		var err error
		settings, err = admins.GetGatewaySettings(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
	}
	accounts, err := providers.ListAccounts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	concurrency := map[int64]int{}
	if concurrencySource != nil {
		concurrency = concurrencySource.AccountConcurrencySnapshot()
	}
	writeJSON(w, http.StatusOK, selectionPreviewWithConcurrency(preview, accounts, settings, concurrency))
}

func selectionPreviewWithConcurrency(preview provider.SelectionPreview, accounts []provider.Account, settings admin.GatewaySettings, concurrency map[int64]int) selectionPreviewResponse {
	accountsByID := make(map[int64]provider.Account, len(accounts))
	for _, account := range accounts {
		accountsByID[account.ID] = account
	}
	response := selectionPreviewResponse{
		SelectionPreview: preview,
		Candidates:       make([]selectionCandidateResponse, 0, len(preview.Candidates)),
	}
	for _, candidate := range preview.Candidates {
		effectiveMaxConcurrentRequests := settings.MaxConcurrentRequestsPerAccount
		if account, ok := accountsByID[candidate.ID]; ok {
			effectiveMaxConcurrentRequests = effectiveAccountMaxConcurrentRequests(account.MaxConcurrentRequests, settings)
		}
		currentConcurrentRequests := concurrency[candidate.ID]
		response.Candidates = append(response.Candidates, selectionCandidateResponse{
			SelectionCandidate:             candidate,
			CurrentConcurrentRequests:      currentConcurrentRequests,
			EffectiveMaxConcurrentRequests: effectiveMaxConcurrentRequests,
			ConcurrencyBlocked:             effectiveMaxConcurrentRequests > 0 && currentConcurrentRequests >= effectiveMaxConcurrentRequests,
		})
	}
	diagnoseSelectionPreview(&response)
	return response
}

func diagnoseSelectionPreview(response *selectionPreviewResponse) {
	response.BlockedReasonCounts = blockedReasonCounts(response.Candidates)
	selected := selectedSelectionCandidate(response.SelectedAccountID, response.Candidates)
	if response.SelectedAccountID <= 0 || selected == nil {
		response.DiagnosisStatus = "blocked"
		response.DiagnosisSummary = blockedSelectionSummary(response.Model, response.BlockedReasonCounts)
		response.DiagnosisHints = selectionDiagnosisHints(response.BlockedReasonCounts, response.RoutingPoolError)
		return
	}

	name := selectionCandidateDisplayName(*selected)
	if selected.ConcurrencyBlocked {
		response.DiagnosisStatus = "degraded"
		response.DiagnosisSummary = fmt.Sprintf("Selected %s for %s, but current concurrency is at the effective limit.", name, response.Model)
		response.DiagnosisHints = selectionDiagnosisHints([]selectionBlockedReasonCount{{Reason: "concurrency limit reached", Count: 1}}, response.RoutingPoolError)
		return
	}

	response.DiagnosisStatus = "routable"
	response.DiagnosisSummary = fmt.Sprintf("Selected %s for %s.", name, response.Model)
	response.DiagnosisHints = selectionDiagnosisHints(response.BlockedReasonCounts, response.RoutingPoolError)
}

func selectedSelectionCandidate(selectedAccountID int64, candidates []selectionCandidateResponse) *selectionCandidateResponse {
	for i := range candidates {
		if candidates[i].Selected || candidates[i].ID == selectedAccountID {
			return &candidates[i]
		}
	}
	return nil
}

func selectionCandidateDisplayName(candidate selectionCandidateResponse) string {
	name := strings.TrimSpace(candidate.DisplayName)
	if name != "" {
		return name
	}
	return fmt.Sprintf("Account %d", candidate.ID)
}

func blockedReasonCounts(candidates []selectionCandidateResponse) []selectionBlockedReasonCount {
	counts := map[string]int{}
	for _, candidate := range candidates {
		reason := strings.TrimSpace(candidate.UnschedulableReason)
		if reason == "" {
			continue
		}
		counts[reason]++
	}
	reasons := make([]selectionBlockedReasonCount, 0, len(counts))
	for reason, count := range counts {
		reasons = append(reasons, selectionBlockedReasonCount{Reason: reason, Count: count})
	}
	sort.Slice(reasons, func(i, j int) bool {
		if reasons[i].Count != reasons[j].Count {
			return reasons[i].Count > reasons[j].Count
		}
		return reasons[i].Reason < reasons[j].Reason
	})
	return reasons
}

func blockedSelectionSummary(model string, reasons []selectionBlockedReasonCount) string {
	if len(reasons) == 0 {
		return fmt.Sprintf("No schedulable account for %s.", model)
	}
	return fmt.Sprintf("No schedulable account for %s: %s.", model, reasons[0].Reason)
}

func selectionDiagnosisHints(reasons []selectionBlockedReasonCount, routingPoolError string) []string {
	hints := make([]string, 0, len(reasons)+1)
	seen := map[string]struct{}{}
	addHint := func(hint string) {
		if hint == "" {
			return
		}
		if _, ok := seen[hint]; ok {
			return
		}
		seen[hint] = struct{}{}
		hints = append(hints, hint)
	}

	for _, reasonCount := range reasons {
		reason := strings.ToLower(reasonCount.Reason)
		switch {
		case strings.Contains(reason, "model not configured"):
			addHint("Configure the requested model on at least one enabled provider account.")
		case strings.Contains(reason, "concurrency"):
			addHint("Reduce concurrent requests or raise the selected account concurrency limit.")
		case strings.Contains(reason, "disabled"):
			addHint("Enable at least one provider account in the routing scope.")
		case strings.Contains(reason, "rate limit"):
			addHint("Wait for the rate limit window to reset or add another account to the routing pool.")
		case strings.Contains(reason, "excluded"):
			addHint("Remove excluded account IDs or choose a routing pool with other schedulable accounts.")
		}
	}

	if strings.TrimSpace(routingPoolError) != "" {
		addHint("Check the routing pool membership, enabled state, and fallback chain.")
	}
	return hints
}

func parseExcludedAccountIDs(raw string) ([]int64, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	ids := make([]int64, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id <= 0 {
			return nil, errors.New("invalid excluded account id")
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func handleProviderStatus(w http.ResponseWriter, r *http.Request, providers ProviderService) {
	if providers == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable")
		return
	}
	status, err := providers.Status(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func handleProviderConnect(w http.ResponseWriter, r *http.Request, providers ProviderService) {
	if providers == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable")
		return
	}
	options, err := decodeConnectOptions(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	result, err := providers.StartConnect(r.Context(), options)
	if err != nil {
		if errors.Is(err, provider.ErrNotConfigured) {
			writeError(w, http.StatusConflict, "provider_not_configured")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"authorizationUrl": result.AuthorizationURL})
}

func handleProviderCallback(w http.ResponseWriter, r *http.Request, providers ProviderService) {
	if providers == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable")
		return
	}
	code, state, err := decodeCallbackURL(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	account, err := providers.CompleteCallback(r.Context(), code, state)
	if err != nil {
		if errors.Is(err, provider.ErrInvalidState) {
			writeError(w, http.StatusBadRequest, "invalid_oauth_callback")
			return
		}
		if errors.Is(err, provider.ErrNotConfigured) {
			writeError(w, http.StatusConflict, "provider_not_configured")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]provider.Account{"account": account})
}

func handlePatchProviderAccount(w http.ResponseWriter, r *http.Request, providers ProviderService) {
	if providers == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable")
		return
	}
	id, err := parsePositivePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}

	var req struct {
		Enabled               *bool   `json:"enabled"`
		Priority              *int    `json:"priority"`
		LoadFactor            *int    `json:"loadFactor"`
		MaxConcurrentRequests *int    `json:"maxConcurrentRequests"`
		Name                  *string `json:"name"`
		BaseURL               *string `json:"baseUrl"`
		APIKey                *string `json:"apiKey"`
		ProxyURL              *string `json:"proxyUrl"`
		FingerprintProfileID  *int64  `json:"fingerprintProfileId"`
	}
	body, err := readJSONBody(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if err := decodeJSONBytes(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	fingerprintProfileIDSet, err := jsonFieldPresent(body, "fingerprintProfileId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if req.Enabled == nil && req.Priority == nil && req.LoadFactor == nil && req.MaxConcurrentRequests == nil && req.Name == nil && req.BaseURL == nil && req.APIKey == nil && req.ProxyURL == nil && !fingerprintProfileIDSet {
		writeError(w, http.StatusBadRequest, "invalid_input")
		return
	}

	account, err := providers.UpdateAccount(r.Context(), id, provider.AccountUpdate{
		Enabled:                 req.Enabled,
		Priority:                req.Priority,
		LoadFactor:              req.LoadFactor,
		MaxConcurrentRequests:   req.MaxConcurrentRequests,
		Name:                    req.Name,
		APIUpstreamBaseURL:      req.BaseURL,
		APIUpstreamAPIKey:       req.APIKey,
		ProxyURL:                req.ProxyURL,
		FingerprintProfileIDSet: fingerprintProfileIDSet,
		FingerprintProfileID:    req.FingerprintProfileID,
	})
	if err != nil {
		writeProviderAccountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]provider.Account{"account": account})
}

func handleBulkUpdateProviderAccounts(w http.ResponseWriter, r *http.Request, providers ProviderService) {
	if providers == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable")
		return
	}
	var req struct {
		AccountIDs            []int64 `json:"accountIds"`
		Enabled               *bool   `json:"enabled"`
		Priority              *int    `json:"priority"`
		LoadFactor            *int    `json:"loadFactor"`
		MaxConcurrentRequests *int    `json:"maxConcurrentRequests"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if (req.Enabled == nil && req.Priority == nil && req.LoadFactor == nil && req.MaxConcurrentRequests == nil) || len(req.AccountIDs) == 0 || len(req.AccountIDs) > 100 {
		writeError(w, http.StatusBadRequest, "invalid_input")
		return
	}
	accountIDs, ok := parseBulkProviderAccountIDs(w, req.AccountIDs)
	if !ok {
		return
	}

	accounts := make([]provider.Account, 0, len(accountIDs))
	for _, id := range accountIDs {
		account, err := providers.UpdateAccount(r.Context(), id, provider.AccountUpdate{
			Enabled:               req.Enabled,
			Priority:              req.Priority,
			LoadFactor:            req.LoadFactor,
			MaxConcurrentRequests: req.MaxConcurrentRequests,
		})
		if err != nil {
			writeProviderAccountError(w, err)
			return
		}
		accounts = append(accounts, account)
	}
	writeJSON(w, http.StatusOK, map[string][]provider.Account{"accounts": accounts})
}

func handleBulkTestProviderAccounts(w http.ResponseWriter, r *http.Request, providers ProviderService) {
	if providers == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable")
		return
	}
	var req struct {
		AccountIDs []int64 `json:"accountIds"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if len(req.AccountIDs) == 0 || len(req.AccountIDs) > 100 {
		writeError(w, http.StatusBadRequest, "invalid_input")
		return
	}
	accountIDs, ok := parseBulkProviderAccountIDs(w, req.AccountIDs)
	if !ok {
		return
	}

	accounts := make([]provider.Account, 0, len(accountIDs))
	for _, id := range accountIDs {
		account, err := providers.TestAccount(r.Context(), id)
		if err != nil {
			writeProviderAccountError(w, err)
			return
		}
		accounts = append(accounts, account)
	}
	writeJSON(w, http.StatusOK, map[string][]provider.Account{"accounts": accounts})
}

func parseBulkProviderAccountIDs(w http.ResponseWriter, ids []int64) ([]int64, bool) {
	if len(ids) == 0 || len(ids) > 100 {
		writeError(w, http.StatusBadRequest, "invalid_input")
		return nil, false
	}
	accountIDs := make([]int64, 0, len(ids))
	seen := map[int64]struct{}{}
	for _, id := range ids {
		if id <= 0 {
			writeError(w, http.StatusBadRequest, "invalid_input")
			return nil, false
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		accountIDs = append(accountIDs, id)
	}
	return accountIDs, true
}

func handleBulkRefreshProviderAccounts(w http.ResponseWriter, r *http.Request, providers ProviderService) {
	if providers == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable")
		return
	}
	var req struct {
		AccountIDs []int64 `json:"accountIds"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	accountIDs, ok := parseBulkProviderAccountIDs(w, req.AccountIDs)
	if !ok {
		return
	}

	accounts := make([]provider.Account, 0, len(accountIDs))
	for _, id := range accountIDs {
		account, err := providers.RefreshAccount(r.Context(), id)
		if err != nil {
			writeProviderAccountError(w, err)
			return
		}
		accounts = append(accounts, account)
	}
	writeJSON(w, http.StatusOK, map[string][]provider.Account{"accounts": accounts})
}

func handleBulkDisconnectProviderAccounts(w http.ResponseWriter, r *http.Request, providers ProviderService) {
	if providers == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable")
		return
	}
	var req struct {
		AccountIDs []int64 `json:"accountIds"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	accountIDs, ok := parseBulkProviderAccountIDs(w, req.AccountIDs)
	if !ok {
		return
	}

	for _, id := range accountIDs {
		if err := providers.DisconnectAccount(r.Context(), id); err != nil {
			writeProviderAccountError(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func handleBulkPauseProviderAccountScheduling(w http.ResponseWriter, r *http.Request, providers ProviderService) {
	if providers == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable")
		return
	}
	var req struct {
		AccountIDs      []int64 `json:"accountIds"`
		DurationSeconds int     `json:"durationSeconds"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if req.DurationSeconds <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_input")
		return
	}
	accountIDs, ok := parseBulkProviderAccountIDs(w, req.AccountIDs)
	if !ok {
		return
	}

	accounts := make([]provider.Account, 0, len(accountIDs))
	for _, id := range accountIDs {
		account, err := providers.PauseAccountScheduling(r.Context(), id, time.Duration(req.DurationSeconds)*time.Second)
		if err != nil {
			writeProviderAccountError(w, err)
			return
		}
		accounts = append(accounts, account)
	}
	writeJSON(w, http.StatusOK, map[string][]provider.Account{"accounts": accounts})
}

func handleBulkResetProviderAccountStatus(w http.ResponseWriter, r *http.Request, providers ProviderService) {
	if providers == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable")
		return
	}
	var req struct {
		AccountIDs []int64 `json:"accountIds"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	accountIDs, ok := parseBulkProviderAccountIDs(w, req.AccountIDs)
	if !ok {
		return
	}

	accounts := make([]provider.Account, 0, len(accountIDs))
	for _, id := range accountIDs {
		account, err := providers.ResetAccountStatus(r.Context(), id)
		if err != nil {
			writeProviderAccountError(w, err)
			return
		}
		accounts = append(accounts, account)
	}
	writeJSON(w, http.StatusOK, map[string][]provider.Account{"accounts": accounts})
}

type bulkProviderAccountModelsResponse struct {
	AccountID int64                   `json:"accountId"`
	Models    []provider.AccountModel `json:"models"`
}

func handleBulkReplaceProviderAccountModels(w http.ResponseWriter, r *http.Request, providers ProviderService) {
	if providers == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable")
		return
	}
	var req struct {
		AccountIDs []int64                      `json:"accountIds"`
		Models     []provider.AccountModelInput `json:"models"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if len(req.Models) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_input")
		return
	}
	accountIDs, ok := parseBulkProviderAccountIDs(w, req.AccountIDs)
	if !ok {
		return
	}

	accounts := make([]bulkProviderAccountModelsResponse, 0, len(accountIDs))
	for _, id := range accountIDs {
		models, err := providers.ReplaceAccountModels(r.Context(), id, req.Models)
		if err != nil {
			writeProviderAccountError(w, err)
			return
		}
		accounts = append(accounts, bulkProviderAccountModelsResponse{AccountID: id, Models: models})
	}
	writeJSON(w, http.StatusOK, map[string][]bulkProviderAccountModelsResponse{"accounts": accounts})
}

func handleDeleteProviderAccount(w http.ResponseWriter, r *http.Request, providers ProviderService) {
	if providers == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable")
		return
	}
	id, err := parsePositivePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}

	if err := providers.DisconnectAccount(r.Context(), id); err != nil {
		writeProviderAccountError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func handleListProviderAccountModels(w http.ResponseWriter, r *http.Request, providers ProviderService) {
	if providers == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable")
		return
	}
	id, err := parsePositivePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}

	models, err := providers.ListAccountModels(r.Context(), id)
	if err != nil {
		writeProviderAccountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string][]provider.AccountModel{"models": models})
}

func handleReplaceProviderAccountModels(w http.ResponseWriter, r *http.Request, providers ProviderService) {
	if providers == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable")
		return
	}
	id, err := parsePositivePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}

	var req struct {
		Models []provider.AccountModelInput `json:"models"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}

	models, err := providers.ReplaceAccountModels(r.Context(), id, req.Models)
	if err != nil {
		writeProviderAccountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string][]provider.AccountModel{"models": models})
}

func handleRefreshProviderAccount(w http.ResponseWriter, r *http.Request, providers ProviderService) {
	if providers == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable")
		return
	}
	id, err := parsePositivePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}

	account, err := providers.RefreshAccount(r.Context(), id)
	if err != nil {
		if errors.Is(err, provider.ErrInvalidInput) {
			writeError(w, http.StatusBadRequest, "invalid_input")
			return
		}
		if errors.Is(err, provider.ErrNotConnected) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]provider.Account{"account": account})
}

func handleTestProviderAccount(w http.ResponseWriter, r *http.Request, providers ProviderService) {
	if providers == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable")
		return
	}
	id, err := parsePositivePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}

	account, err := providers.TestAccount(r.Context(), id)
	if err != nil {
		writeProviderAccountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]provider.Account{"account": account})
}

func handleTestAllProviderAccounts(w http.ResponseWriter, r *http.Request, providers ProviderService) {
	if providers == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable")
		return
	}
	accounts, err := providers.TestAccounts(r.Context())
	if err != nil {
		writeProviderAccountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string][]provider.Account{"accounts": accounts})
}

func handleListProviderAccountTestResults(w http.ResponseWriter, r *http.Request, providers ProviderService) {
	if providers == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable")
		return
	}
	id, err := parsePositivePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	limit := 0
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		limit = parsed
	}

	results, err := providers.ListAccountTestResults(r.Context(), id, limit)
	if err != nil {
		writeProviderAccountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string][]provider.AccountTestResult{"results": results})
}

func handlePauseProviderAccountScheduling(w http.ResponseWriter, r *http.Request, providers ProviderService) {
	if providers == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable")
		return
	}
	id, err := parsePositivePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}

	var req struct {
		DurationSeconds int `json:"durationSeconds"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	account, err := providers.PauseAccountScheduling(r.Context(), id, time.Duration(req.DurationSeconds)*time.Second)
	if err != nil {
		writeProviderAccountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]provider.Account{"account": account})
}

func handleResetProviderAccountStatus(w http.ResponseWriter, r *http.Request, providers ProviderService) {
	if providers == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable")
		return
	}
	id, err := parsePositivePathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}

	account, err := providers.ResetAccountStatus(r.Context(), id)
	if err != nil {
		writeProviderAccountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]provider.Account{"account": account})
}

func writeManualOAuthCallbackPage(w http.ResponseWriter, r *http.Request) {
	callbackURL := absoluteRequestURL(r)
	escapedURL := html.EscapeString(callbackURL)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>N2API OAuth Callback</title>
  <style>
    body { margin: 0; font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; background: #fafafa; color: #0d0d0d; }
    main { max-width: 760px; margin: 10vh auto; padding: 0 24px; }
    code { display: block; margin-top: 16px; padding: 14px; overflow-x: auto; border: 1px solid #e5e5e5; border-radius: 8px; background: #fff; font: 13px/1.6 ui-monospace, SFMono-Regular, Menlo, monospace; }
  </style>
</head>
<body>
  <main>
    <h1>OAuth callback received</h1>
    <p>Copy this callback URL into the N2API admin provider form to complete the account connection.</p>
    <code>`+escapedURL+`</code>
  </main>
</body>
</html>`)
}

func absoluteRequestURL(r *http.Request) string {
	if r.URL.IsAbs() {
		return r.URL.String()
	}
	scheme := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	if host == "" {
		return r.URL.RequestURI()
	}
	return scheme + "://" + host + r.URL.RequestURI()
}

func modelRoutingStatus(ctx context.Context, admins AdminService, providers ProviderService) (admin.ModelRoutingStatus, error) {
	defaultModel, err := admins.DefaultModel(ctx)
	if err != nil {
		return admin.ModelRoutingStatus{}, err
	}
	allowed, err := admins.IsModelAllowed(ctx, defaultModel)
	if err != nil {
		return admin.ModelRoutingStatus{}, err
	}
	if !allowed {
		return admin.ModelRoutingStatus{}, admin.ErrInvalidInput
	}

	settings, err := admins.GetModelSettings(ctx)
	if err != nil {
		return admin.ModelRoutingStatus{}, err
	}
	accounts, err := providers.ListAccounts(ctx)
	if err != nil {
		return admin.ModelRoutingStatus{}, err
	}
	routingPools, err := admins.ListRoutingPools(ctx)
	if err != nil {
		return admin.ModelRoutingStatus{}, err
	}
	accountRoutingPoolIDs := routingPoolIDsByAccount(routingPools)
	exposed, err := providers.ListExposedModels(ctx, settings.AllowedModels)
	if err != nil {
		return admin.ModelRoutingStatus{}, err
	}

	status := admin.ModelRoutingStatus{
		DefaultModel:  defaultModel,
		AllowedModels: append([]string(nil), settings.AllowedModels...),
		Models:        make([]admin.ModelRoutingModel, 0, len(settings.AllowedModels)),
	}
	allowedSet := make(map[string]struct{}, len(settings.AllowedModels))
	modelIndexes := make(map[string]int, len(settings.AllowedModels))
	for _, model := range settings.AllowedModels {
		allowedSet[model] = struct{}{}
		status.Models = append(status.Models, admin.ModelRoutingModel{
			Model:   model,
			Allowed: true,
		})
		modelIndexes[model] = len(status.Models) - 1
	}

	extraModels := []string{}
	extraModelSet := map[string]struct{}{}
	now := time.Now()
	for _, account := range accounts {
		models, err := providers.ListAccountModels(ctx, account.ID)
		if err != nil {
			return admin.ModelRoutingStatus{}, err
		}
		accountReady := provider.AccountSchedulable(account, now)
		for _, model := range models {
			modelEnabled := model.Enabled
			index, ok := modelIndexes[model.Model]
			if !ok {
				status.Models = append(status.Models, admin.ModelRoutingModel{
					Model: model.Model,
				})
				index = len(status.Models) - 1
				modelIndexes[model.Model] = index
				if _, seen := extraModelSet[model.Model]; !seen {
					extraModelSet[model.Model] = struct{}{}
					extraModels = append(extraModels, model.Model)
				}
			}
			status.Models[index].ConfiguredCount++
			schedulable := accountReady && modelEnabled
			if schedulable {
				status.Models[index].EnabledCount++
			}
			status.Models[index].Accounts = append(status.Models[index].Accounts, admin.ModelRoutingAccount{
				ID:                  account.ID,
				DisplayName:         account.DisplayName,
				AccountType:         account.AccountType,
				RoutingPoolIDs:      append([]int64(nil), accountRoutingPoolIDs[account.ID]...),
				Enabled:             account.Enabled,
				Priority:            account.Priority,
				LoadFactor:          normalizedProviderAccountLoadFactor(account.LoadFactor),
				Status:              account.Status,
				StatusReason:        account.StatusReason,
				LastError:           account.LastError,
				LastErrorAt:         account.LastErrorAt,
				LastUsedAt:          account.LastUsedAt,
				Schedulable:         schedulable,
				UnschedulableReason: modelRoutingUnschedulableReason(account, modelEnabled, now),
			})
		}
	}
	for i := range status.Models {
		sortModelRoutingAccounts(status.Models[i].Accounts, accounts)
		for index := range status.Models[i].Accounts {
			status.Models[i].Accounts[index].ScheduleRank = index + 1
		}
	}
	if len(extraModels) > 1 {
		sort.Strings(extraModels)
		extras := make([]admin.ModelRoutingModel, 0, len(extraModels))
		for _, model := range extraModels {
			extras = append(extras, status.Models[modelIndexes[model]])
		}
		status.Models = append(status.Models[:len(settings.AllowedModels)], extras...)
	}

	exposedSet := map[string]struct{}{}
	for _, model := range exposed {
		exposedSet[model.ID] = struct{}{}
	}

	for _, model := range status.Models {
		if _, ok := exposedSet[model.Model]; model.Allowed && !ok {
			status.Warnings = append(status.Warnings, "allowed model "+model.Model+" has no enabled account")
		}
	}
	return status, nil
}

func modelRoutingUnschedulableReason(account provider.Account, modelEnabled bool, now time.Time) string {
	if !modelEnabled {
		return "model disabled"
	}
	if !account.Enabled {
		return "account disabled"
	}
	switch account.Status {
	case "", provider.AccountStatusActive:
	case provider.AccountStatusRateLimited:
		if account.RateLimitedUntil == nil || account.RateLimitedUntil.After(now) {
			return "rate limited"
		}
	case provider.AccountStatusCircuitOpen:
		if account.CircuitOpenUntil == nil || account.CircuitOpenUntil.After(now) {
			return "circuit open"
		}
	case provider.AccountStatusDisabled:
		return "account disabled"
	case provider.AccountStatusExpired:
		return "account expired"
	default:
		return "status " + account.Status
	}
	if account.RateLimitedUntil != nil && account.RateLimitedUntil.After(now) {
		return "rate limited"
	}
	if account.CircuitOpenUntil != nil && account.CircuitOpenUntil.After(now) {
		return "circuit open"
	}
	return ""
}

func routingPoolIDsByAccount(pools []admin.RoutingPool) map[int64][]int64 {
	index := make(map[int64][]int64)
	for _, pool := range pools {
		for _, accountID := range pool.AccountIDs {
			index[accountID] = append(index[accountID], pool.ID)
		}
	}
	for accountID := range index {
		sort.Slice(index[accountID], func(i, j int) bool {
			return index[accountID][i] < index[accountID][j]
		})
	}
	return index
}

func sortModelRoutingAccounts(accounts []admin.ModelRoutingAccount, sourceAccounts []provider.Account) {
	accountIndexes := make(map[int64]provider.Account, len(sourceAccounts))
	for _, account := range sourceAccounts {
		accountIndexes[account.ID] = account
	}
	sort.SliceStable(accounts, func(i, j int) bool {
		left := accountIndexes[accounts[i].ID]
		right := accountIndexes[accounts[j].ID]
		if accounts[i].Schedulable != accounts[j].Schedulable {
			return accounts[i].Schedulable
		}
		if left.Priority != right.Priority {
			return left.Priority < right.Priority
		}
		if normalizedProviderAccountLoadFactor(left.LoadFactor) != normalizedProviderAccountLoadFactor(right.LoadFactor) {
			return normalizedProviderAccountLoadFactor(left.LoadFactor) > normalizedProviderAccountLoadFactor(right.LoadFactor)
		}
		leftHasError := left.LastErrorAt != nil
		rightHasError := right.LastErrorAt != nil
		if leftHasError != rightHasError {
			return !leftHasError
		}
		if left.LastUsedAt == nil && right.LastUsedAt != nil {
			return true
		}
		if left.LastUsedAt != nil && right.LastUsedAt == nil {
			return false
		}
		if left.LastUsedAt != nil && right.LastUsedAt != nil && !left.LastUsedAt.Equal(*right.LastUsedAt) {
			return left.LastUsedAt.Before(*right.LastUsedAt)
		}
		return left.ID < right.ID
	})
}

func normalizedProviderAccountLoadFactor(value int) int {
	if value <= 0 {
		return 1
	}
	return value
}

func decodeCallbackURL(w http.ResponseWriter, r *http.Request) (string, string, error) {
	var req struct {
		CallbackURL string `json:"callbackUrl"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return "", "", err
	}
	parsed, err := url.Parse(strings.TrimSpace(req.CallbackURL))
	if err != nil {
		return "", "", err
	}
	code := strings.TrimSpace(parsed.Query().Get("code"))
	state := strings.TrimSpace(parsed.Query().Get("state"))
	if code == "" || state == "" {
		return "", "", provider.ErrInvalidState
	}
	return code, state, nil
}

func decodeConnectOptions(w http.ResponseWriter, r *http.Request) (provider.ConnectOptions, error) {
	options := provider.ConnectOptions{
		RedirectAfter: "/",
		Fingerprint: provider.Fingerprint{
			UserAgent: strings.TrimSpace(r.UserAgent()),
			IP:        clientIP(r),
		},
	}
	if r.Body == nil {
		return options, nil
	}
	if r.ContentLength == 0 {
		return options, nil
	}
	var req struct {
		RedirectAfter        string `json:"redirectAfter"`
		Name                 string `json:"name"`
		Priority             int    `json:"priority"`
		Enabled              *bool  `json:"enabled"`
		TargetAccountID      int64  `json:"targetAccountId"`
		FingerprintProfileID *int64 `json:"fingerprintProfileId"`
		Fingerprint          string `json:"fingerprint"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		return provider.ConnectOptions{}, err
	}
	if strings.TrimSpace(req.RedirectAfter) != "" {
		options.RedirectAfter = strings.TrimSpace(req.RedirectAfter)
	}
	options.Name = strings.TrimSpace(req.Name)
	options.Priority = req.Priority
	options.Enabled = req.Enabled
	options.TargetAccountID = req.TargetAccountID
	options.FingerprintProfileID = req.FingerprintProfileID
	if options.FingerprintProfileID != nil && *options.FingerprintProfileID == 0 {
		options.FingerprintProfileID = nil
	}
	options.Fingerprint.Value = strings.TrimSpace(req.Fingerprint)
	return options, nil
}

func clientIP(r *http.Request) string {
	forwardedFor := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if forwardedFor != "" {
		parts := strings.Split(forwardedFor, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	realIP := strings.TrimSpace(r.Header.Get("X-Real-IP"))
	if realIP != "" {
		return realIP
	}
	remoteAddr := strings.TrimSpace(r.RemoteAddr)
	if remoteAddr == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return strings.TrimSpace(host)
	}
	return remoteAddr
}

func parseServerOptions(options ...any) (http.Handler, fs.FS, ProviderAccountAutoTestStatusSource) {
	var gateway http.Handler
	var webFS fs.FS
	var autoTestStatusSource ProviderAccountAutoTestStatusSource
	for _, option := range options {
		switch value := option.(type) {
		case http.Handler:
			if gateway == nil {
				gateway = value
			}
		case fs.FS:
			if webFS == nil {
				webFS = value
			}
		case ProviderAccountAutoTestStatusSource:
			if autoTestStatusSource == nil {
				autoTestStatusSource = value
			}
		}
	}
	return gateway, webFS, autoTestStatusSource
}

func serveWeb(w http.ResponseWriter, r *http.Request, webFS fs.FS) bool {
	if webFS == nil || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		return false
	}
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/v1/") || strings.HasPrefix(r.URL.Path, "/oauth/") {
		return false
	}

	cleanPath := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if cleanPath == "." || cleanPath == "" {
		cleanPath = "index.html"
	}
	if info, err := fs.Stat(webFS, cleanPath); err == nil && !info.IsDir() {
		http.ServeFileFS(w, r, webFS, cleanPath)
		return true
	}
	if _, err := fs.Stat(webFS, "200.html"); err == nil {
		http.ServeFileFS(w, r, webFS, "200.html")
		return true
	}
	return false
}

func decodeJSON(w http.ResponseWriter, r *http.Request, value any) error {
	body, err := readJSONBody(w, r)
	if err != nil {
		return err
	}
	return decodeJSONBytes(body, value)
}

func readJSONBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	return io.ReadAll(r.Body)
}

func decodeJSONBytes(body []byte, value any) error {
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return errors.New("request body must contain a single JSON value")
	} else if err != io.EOF {
		return err
	}
	return nil
}

func jsonFieldPresent(body []byte, field string) (bool, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return false, err
	}
	_, ok := raw[field]
	return ok, nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

func setSessionCookie(w http.ResponseWriter, token string, expiresAt time.Time, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func readSessionCookie(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(adminSessionCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return "", false
	}
	return cookie.Value, true
}

func clearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func parseSinceParam(r *http.Request) time.Time {
	raw := r.URL.Query().Get("since")
	if raw == "" {
		return time.Time{}
	}
	seconds, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || seconds <= 0 {
		return time.Time{}
	}
	return time.Unix(seconds, 0)
}

func parseLimitParam(r *http.Request, defaultLimit, maxLimit int) (int, error) {
	limit := defaultLimit
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed <= 0 {
			return 0, fmt.Errorf("invalid limit")
		}
		limit = parsed
	}
	if maxLimit > 0 && limit > maxLimit {
		limit = maxLimit
	}
	return limit, nil
}

func handleExportRequestLogs(w http.ResponseWriter, r *http.Request, admins AdminService) {
	filter := buildRequestLogFilter(r)
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}
	if format != "json" && format != "csv" {
		writeError(w, http.StatusBadRequest, "invalid_input")
		return
	}

	logs, err := admins.ListRequestLogs(r.Context(), filter)
	if err != nil {
		if errors.Is(err, admin.ErrInvalidInput) {
			writeError(w, http.StatusBadRequest, "invalid_input")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}

	switch format {
	case "csv":
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="n2api-request-logs.csv"`)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "id,request_id,client_key,provider,provider_account_id,provider_account_type,provider_account_name,routing_pool_id,routing_pool_name,routing_pool_fallback_depth,routing_pool_fallback_chain,routing_pool_error,model,session_id,route,method,status_code,latency_ms,error,input_tokens,output_tokens,total_tokens,cached_input_tokens,reasoning_tokens,usage_source,estimated_cost_microusd,pricing_matched,gateway_attempt_count,gateway_fallback_count,created_at\n")
		for _, log := range logs {
			_, _ = fmt.Fprintf(w, "%d,%s,%s,%s,%d,%s,%s,%d,%s,%d,%s,%s,%s,%s,%s,%s,%d,%d,%s,%d,%d,%d,%d,%d,%s,%d,%t,%d,%d,%s\n",
				log.ID, csvEscape(log.RequestID), csvEscape(log.ClientKey), csvEscape(log.Provider),
				log.ProviderAccountID, csvEscape(log.ProviderAccountType), csvEscape(log.ProviderAccountName),
				log.RoutingPoolID, csvEscape(log.RoutingPoolName), log.RoutingPoolFallbackDepth,
				csvEscape(log.RoutingPoolFallbackChain), csvEscape(log.RoutingPoolError),
				csvEscape(log.Model), csvEscape(log.SessionID), csvEscape(log.Route), csvEscape(log.Method),
				log.StatusCode, log.LatencyMS, csvEscape(log.Error),
				log.InputTokens, log.OutputTokens, log.TotalTokens, log.CachedInputTokens, log.ReasoningTokens,
				csvEscape(log.UsageSource), log.EstimatedCostMicrousd, log.PricingMatched,
				log.GatewayAttemptCount, log.GatewayFallbackCount, log.CreatedAt.Format(time.RFC3339))
		}
	default:
		writeJSON(w, http.StatusOK, map[string][]admin.RequestLog{"logs": logs})
	}
}

func csvEscape(s string) string {
	if strings.ContainsAny(s, "\",\n") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

func buildRequestLogFilter(r *http.Request) admin.RequestLogFilter {
	filter := admin.RequestLogFilter{
		Limit:       200,
		Since:       parseSinceParam(r),
		StatusClass: "all",
	}
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		if parsed, err := strconv.Atoi(rawLimit); err == nil && parsed > 0 && parsed <= 10000 {
			filter.Limit = parsed
		}
	}
	if q := r.URL.Query().Get("q"); q != "" {
		filter.Query = q
	}
	filter.RequestID = r.URL.Query().Get("requestId")
	switch sc := r.URL.Query().Get("statusClass"); sc {
	case "all", "success", "client_error", "server_error":
		filter.StatusClass = sc
	}
	if raw := r.URL.Query().Get("statusCode"); raw != "" {
		if code, err := strconv.Atoi(raw); err == nil && code >= 100 && code <= 599 {
			filter.StatusCode = code
		}
	}
	if raw := r.URL.Query().Get("providerAccountId"); raw != "" {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil && id > 0 {
			filter.ProviderAccountID = id
		}
	}
	if raw := r.URL.Query().Get("routingPoolId"); raw != "" {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil && id > 0 {
			filter.RoutingPoolID = id
		}
	}
	if raw := r.URL.Query().Get("clientKeyId"); raw != "" {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil && id > 0 {
			filter.ClientKeyID = id
		}
	}
	filter.Model = r.URL.Query().Get("model")
	filter.SessionID = r.URL.Query().Get("sessionId")
	filter.Error = r.URL.Query().Get("error")
	filter.UsageSource = r.URL.Query().Get("usageSource")
	filter.RoutingPoolError = r.URL.Query().Get("routingPoolError")
	filter.RoutingPoolChain = r.URL.Query().Get("routingPoolChain")
	if raw := r.URL.Query().Get("gatewayFallbacks"); raw == "1" || raw == "true" {
		filter.GatewayFallbacks = true
	}
	return filter
}
