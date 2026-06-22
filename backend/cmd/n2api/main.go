package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/KnowSky404/N2API/backend/internal/config"
	"github.com/KnowSky404/N2API/backend/internal/gateway"
	"github.com/KnowSky404/N2API/backend/internal/httpapi"
	"github.com/KnowSky404/N2API/backend/internal/provider"
	"github.com/KnowSky404/N2API/backend/internal/store"
)

type gatewayTokenProvider struct {
	service *provider.Service
}

func (p gatewayTokenProvider) SelectAccessToken(ctx context.Context, excludedAccountIDs ...int64) (gateway.SelectedToken, error) {
	selected, err := p.service.SelectAccessToken(ctx, "", excludedAccountIDs...)
	if err != nil {
		return gateway.SelectedToken{}, err
	}
	return gateway.SelectedToken{
		AccountID:        selected.AccountID,
		Token:            selected.Token,
		ChatGPTAccountID: selected.ChatGPTAccountID,
	}, nil
}

func (p gatewayTokenProvider) RecordAccountFailure(ctx context.Context, accountID int64, statusCode int, retryAfter, message string) error {
	return p.service.RecordAccountFailure(ctx, accountID, statusCode, retryAfter, message)
}

func main() {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	ctx := context.Background()
	pool, err := store.OpenPool(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("database unavailable", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := store.RunMigrations(ctx, pool); err != nil {
		slog.Error("database migration failed", "error", err)
		os.Exit(1)
	}

	adminRepo := store.NewAdminRepository(pool)
	adminService := admin.NewService(adminRepo, admin.Config{SessionTTL: 7 * 24 * time.Hour})
	if err := adminService.BootstrapAdmin(ctx, cfg.AdminUsername, cfg.AdminPassword); err != nil {
		slog.Error("admin bootstrap failed", "error", err)
		os.Exit(1)
	}

	providerRepo := store.NewProviderRepository(pool)
	providerService := provider.NewService(providerRepo, provider.NewHTTPClient(http.DefaultClient), provider.Config{
		Provider:     "openai",
		ClientID:     cfg.OpenAIOAuthClientID,
		ClientSecret: cfg.OpenAIOAuthSecret,
		RedirectURL:  cfg.OpenAIOAuthRedirectURL,
		AuthURL:      cfg.OpenAIOAuthAuthURL,
		TokenURL:     cfg.OpenAIOAuthTokenURL,
		APIBaseURL:   cfg.OpenAIAPIBaseURL,
		Secret:       cfg.EncryptionSecret,
	})
	gatewayProxy := gateway.NewProxy(adminService, gatewayTokenProvider{service: providerService}, gateway.Config{
		UpstreamBaseURL: cfg.OpenAIAPIBaseURL,
		Logger:          store.NewGatewayRepository(pool),
	})

	server := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           httpapi.NewServer(cfg, pool, adminService, providerService, gatewayProxy, os.DirFS("frontend/build")),
		ReadHeaderTimeout: 5 * time.Second,
	}

	slog.Info("starting n2api", "addr", cfg.Addr())
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
