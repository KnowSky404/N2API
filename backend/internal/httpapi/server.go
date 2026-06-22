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
	ListRequestLogs(ctx context.Context, limit int) ([]admin.RequestLog, error)
	GetModelSettings(ctx context.Context) (admin.ModelSettings, error)
	UpdateModelSettings(ctx context.Context, settings admin.ModelSettings) (admin.ModelSettings, error)
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
	RefreshAccount(ctx context.Context, id int64) (provider.Account, error)
	DisconnectAccount(ctx context.Context, id int64) error
	Disconnect(ctx context.Context) error
}

func NewServer(cfg config.Config, health HealthChecker, admins AdminService, providers ProviderService, options ...any) http.Handler {
	mux := http.NewServeMux()
	secureCookie := strings.HasPrefix(cfg.PublicURL, "https://")
	gateway, webFS := parseServerOptions(options...)

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
		writeJSON(w, http.StatusOK, map[string][]admin.APIKey{"keys": keys})
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
		logs, err := admins.ListRequestLogs(r.Context(), limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string][]admin.RequestLog{"logs": logs})
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

	mux.HandleFunc("GET /api/admin/provider-accounts", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleListProviderAccounts(w, r, providers)
	}))

	mux.HandleFunc("POST /api/admin/provider-accounts/api-upstream", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		if providers == nil {
			writeError(w, http.StatusServiceUnavailable, "service_unavailable")
			return
		}
		var req provider.APIUpstreamInput
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		account, err := providers.CreateAPIUpstreamAccount(r.Context(), req)
		if err != nil {
			writeProviderAccountError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]provider.Account{"account": account})
	}))

	mux.HandleFunc("PATCH /api/admin/provider-accounts/{id}", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handlePatchProviderAccount(w, r, providers)
	}))

	mux.HandleFunc("DELETE /api/admin/provider-accounts/{id}", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleDeleteProviderAccount(w, r, providers)
	}))

	mux.HandleFunc("GET /api/admin/provider-accounts/{id}/models", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleListProviderAccountModels(w, r, providers)
	}))

	mux.HandleFunc("PUT /api/admin/provider-accounts/{id}/models", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
		handleReplaceProviderAccountModels(w, r, providers)
	}))

	mux.HandleFunc("GET /api/admin/providers/openai", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
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
	}))

	mux.HandleFunc("POST /api/admin/providers/openai/connect", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
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
	}))

	mux.HandleFunc("POST /api/admin/providers/openai/callback", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ admin.Admin) {
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
		handleListProviderAccounts(w, r, providers)
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
		if providers == nil {
			writeError(w, http.StatusServiceUnavailable, "service_unavailable")
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
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

func handleListProviderAccounts(w http.ResponseWriter, r *http.Request, providers ProviderService) {
	if providers == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable")
		return
	}
	accounts, err := providers.ListAccounts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string][]provider.Account{"accounts": accounts})
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
		Enabled  *bool `json:"enabled"`
		Priority *int  `json:"priority"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if req.Enabled == nil && req.Priority == nil {
		writeError(w, http.StatusBadRequest, "invalid_input")
		return
	}

	account, err := providers.UpdateAccount(r.Context(), id, provider.AccountUpdate{
		Enabled:  req.Enabled,
		Priority: req.Priority,
	})
	if err != nil {
		writeProviderAccountError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]provider.Account{"account": account})
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
		accountEnabled := provider.AccountSchedulable(account, now)
		for _, model := range models {
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
			if accountEnabled && model.Enabled {
				status.Models[index].EnabledCount++
			}
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

func parseServerOptions(options ...any) (http.Handler, fs.FS) {
	var gateway http.Handler
	var webFS fs.FS
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
		}
	}
	return gateway, webFS
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
