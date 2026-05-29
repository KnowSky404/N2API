package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/KnowSky404/N2API/backend/internal/config"
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
}

func NewServer(cfg config.Config, health HealthChecker, admins AdminService) http.Handler {
	mux := http.NewServeMux()
	secureCookie := strings.HasPrefix(cfg.PublicURL, "https://")

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

	mux.HandleFunc("/api/admin", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found")
	})

	mux.HandleFunc("/api/admin/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found")
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("N2API bootstrap server\n"))
	})

	return mux
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
