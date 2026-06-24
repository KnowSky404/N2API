package httpapi

import (
	"context"
	"encoding/json"
	"errors"
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
	UpdateAPIKeyModelPolicy(ctx context.Context, id int64, policy string, models []string) (admin.APIKey, error)
	UpdateAPIKeyLimits(ctx context.Context, id int64, requestsPerMinute, tokensPerMinute int) (admin.APIKey, error)
	ListRequestLogs(ctx context.Context, filter admin.RequestLogFilter) ([]admin.RequestLog, error)
	GetUsageSummary(ctx context.Context, rangeName, groupBy string) (admin.UsageSummary, error)
	GetUsagePricing(ctx context.Context) (admin.UsagePricing, error)
	UpdateUsagePricing(ctx context.Context, pricing admin.UsagePricing) (admin.UsagePricing, error)
	GetModelSettings(ctx context.Context) (admin.ModelSettings, error)
	UpdateModelSettings(ctx context.Context, settings admin.ModelSettings) (admin.ModelSettings, error)
	GetGatewaySettings(ctx context.Context) (admin.GatewaySettings, error)
	UpdateGatewaySettings(ctx context.Context, settings admin.GatewaySettings) (admin.GatewaySettings, error)
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
	Candidates []selectionCandidateResponse `json:"candidates"`
}

type selectionCandidateResponse struct {
	provider.SelectionCandidate
	CurrentConcurrentRequests      int  `json:"currentConcurrentRequests"`
	EffectiveMaxConcurrentRequests int  `json:"effectiveMaxConcurrentRequests"`
	ConcurrencyBlocked             bool `json:"concurrencyBlocked"`
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
		writeJSON(w, http.StatusOK, map[string][]apiKeyResponse{
			"keys": apiKeyResponses(keys, settings, concurrency, requestRate, tokenRate),
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
		var clientKeyID int64
		if rawClientKeyID := r.URL.Query().Get("clientKeyId"); rawClientKeyID != "" {
			parsed, err := strconv.ParseInt(rawClientKeyID, 10, 64)
			if err != nil || parsed < 1 {
				writeError(w, http.StatusBadRequest, "invalid_input")
				return
			}
			clientKeyID = parsed
		}
		filter := admin.RequestLogFilter{
			Limit:             limit,
			Query:             r.URL.Query().Get("q"),
			StatusClass:       r.URL.Query().Get("statusClass"),
			ProviderAccountID: providerAccountID,
			ClientKeyID:       clientKeyID,
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
			Name       string   `json:"name"`
			BaseURL    string   `json:"baseUrl"`
			APIKey     string   `json:"apiKey"`
			Enabled    *bool    `json:"enabled"`
			Priority   int      `json:"priority"`
			LoadFactor int      `json:"loadFactor"`
			Models     []string `json:"models"`
		}
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		account, err := providers.CreateAPIUpstreamAccount(r.Context(), provider.APIUpstreamInput{
			Name:       req.Name,
			BaseURL:    req.BaseURL,
			APIKey:     req.APIKey,
			Enabled:    req.Enabled,
			Priority:   req.Priority,
			LoadFactor: req.LoadFactor,
			Models:     req.Models,
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

func apiKeyResponses(keys []admin.APIKey, settings admin.GatewaySettings, concurrency, requestRate, tokenRate map[int64]int) []apiKeyResponse {
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
	preview, err := providers.PreviewAccountSelection(r.Context(), model, r.URL.Query().Get("sessionId"), excludedIDs...)
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
	return response
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
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if req.Enabled == nil && req.Priority == nil && req.LoadFactor == nil && req.MaxConcurrentRequests == nil && req.Name == nil && req.BaseURL == nil && req.APIKey == nil {
		writeError(w, http.StatusBadRequest, "invalid_input")
		return
	}

	account, err := providers.UpdateAccount(r.Context(), id, provider.AccountUpdate{
		Enabled:               req.Enabled,
		Priority:              req.Priority,
		LoadFactor:            req.LoadFactor,
		MaxConcurrentRequests: req.MaxConcurrentRequests,
		Name:                  req.Name,
		APIUpstreamBaseURL:    req.BaseURL,
		APIUpstreamAPIKey:     req.APIKey,
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
		RedirectAfter   string `json:"redirectAfter"`
		Name            string `json:"name"`
		Priority        int    `json:"priority"`
		Enabled         *bool  `json:"enabled"`
		TargetAccountID int64  `json:"targetAccountId"`
		Fingerprint     string `json:"fingerprint"`
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
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
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
