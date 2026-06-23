package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/KnowSky404/N2API/backend/internal/config"
	"github.com/KnowSky404/N2API/backend/internal/gateway"
	"github.com/KnowSky404/N2API/backend/internal/httpapi"
	"github.com/KnowSky404/N2API/backend/internal/provider"
	"github.com/KnowSky404/N2API/backend/internal/store"
)

type gatewayAccountProvider struct {
	service *provider.Service
}

var _ gateway.AccountProvider = gatewayAccountProvider{}
var _ gateway.StickyAccountProvider = gatewayAccountProvider{}
var _ gateway.AccountUsageRecorder = gatewayAccountProvider{}

func (p gatewayAccountProvider) SelectAccountForModel(ctx context.Context, model string, excludedAccountIDs ...int64) (gateway.SelectedAccount, error) {
	selected, err := p.service.SelectAccountForModel(ctx, model, excludedAccountIDs...)
	return selectedGatewayAccount(selected, err)
}

func (p gatewayAccountProvider) SelectAccountForModelAndSession(ctx context.Context, model, sessionID string, excludedAccountIDs ...int64) (gateway.SelectedAccount, error) {
	selected, err := p.service.SelectAccountForModelAndSession(ctx, model, sessionID, excludedAccountIDs...)
	return selectedGatewayAccount(selected, err)
}

func selectedGatewayAccount(selected provider.SelectedAccount, err error) (gateway.SelectedAccount, error) {
	if err != nil {
		return gateway.SelectedAccount{}, err
	}
	return gateway.SelectedAccount{
		AccountID:          selected.AccountID,
		Provider:           selected.Provider,
		AccountType:        selected.AccountType,
		DisplayName:        selected.DisplayName,
		AuthorizationToken: selected.AuthorizationToken,
		BaseURL:            selected.BaseURL,
		ChatGPTAccountID:   selected.ChatGPTAccountID,
	}, nil
}

func (p gatewayAccountProvider) RecordAccountFailure(ctx context.Context, accountID int64, statusCode int, retryAfter, message string) error {
	return p.service.RecordAccountFailure(ctx, accountID, statusCode, retryAfter, message)
}

func (p gatewayAccountProvider) RecordAccountUsed(ctx context.Context, accountID int64) error {
	return p.service.RecordAccountUsed(ctx, accountID)
}

type gatewayModelProvider struct {
	admins    *admin.Service
	providers *provider.Service
}

func (p gatewayModelProvider) DefaultModel(ctx context.Context) (string, error) {
	settings, err := p.admins.GetModelSettings(ctx)
	if err != nil {
		return "", err
	}
	return settings.DefaultModel, nil
}

func (p gatewayModelProvider) IsModelAllowed(ctx context.Context, model string) (bool, error) {
	settings, err := p.admins.GetModelSettings(ctx)
	if err != nil {
		return false, err
	}
	model = strings.TrimSpace(model)
	for _, allowed := range settings.AllowedModels {
		if strings.TrimSpace(allowed) == model {
			return true, nil
		}
	}
	return false, nil
}

func (p gatewayModelProvider) ListExposedModels(ctx context.Context) ([]gateway.ExposedModel, error) {
	settings, err := p.admins.GetModelSettings(ctx)
	if err != nil {
		return nil, err
	}
	models, err := p.providers.ListExposedModels(ctx, settings.AllowedModels)
	if err != nil {
		return nil, err
	}
	exposed := make([]gateway.ExposedModel, 0, len(models))
	for _, model := range models {
		exposed = append(exposed, gateway.ExposedModel{
			ID:      model.ID,
			OwnedBy: model.OwnedBy,
		})
	}
	return exposed, nil
}

type gatewayUsagePricer struct {
	admins *admin.Service
}

var _ gateway.UsagePricer = gatewayUsagePricer{}

func (p gatewayUsagePricer) EstimateUsageCost(ctx context.Context, usage gateway.Usage) (gateway.UsageCostEstimate, error) {
	estimate, err := p.admins.EstimateUsageCost(ctx, admin.UsageCostInput{
		Model:             usage.Model,
		InputTokens:       usage.InputTokens,
		OutputTokens:      usage.OutputTokens,
		TotalTokens:       usage.TotalTokens,
		CachedInputTokens: usage.CachedInputTokens,
		ReasoningTokens:   usage.ReasoningTokens,
		Source:            usage.Source,
	})
	if err != nil {
		return gateway.UsageCostEstimate{}, err
	}
	return gateway.UsageCostEstimate{
		Matched:      estimate.Matched,
		CostMicrousd: estimate.CostMicrousd,
		Snapshot:     estimate.Snapshot,
	}, nil
}

func main() {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
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
	adminService := admin.NewService(adminRepo, admin.Config{
		SessionTTL: 7 * 24 * time.Hour,
		DefaultGatewaySettings: admin.GatewaySettings{
			MaxConcurrentGatewayRequests:    cfg.GatewayMaxConcurrentRequests,
			MaxConcurrentRequestsPerAccount: cfg.GatewayMaxConcurrentRequestsPerAccount,
			MaxConcurrentRequestsPerKey:     cfg.GatewayMaxConcurrentRequestsPerKey,
			RequestsPerMinutePerKey:         cfg.GatewayRequestsPerMinutePerKey,
			TokensPerMinutePerKey:           cfg.GatewayTokensPerMinutePerKey,
		},
	})
	if err := adminService.BootstrapAdmin(ctx, cfg.AdminUsername, cfg.AdminPassword); err != nil {
		slog.Error("admin bootstrap failed", "error", err)
		os.Exit(1)
	}

	providerRepo := store.NewProviderRepository(pool)
	providerService := provider.NewService(providerRepo, provider.NewHTTPClient(http.DefaultClient), provider.Config{
		Provider:              "openai",
		ClientID:              cfg.OpenAIOAuthClientID,
		ClientSecret:          cfg.OpenAIOAuthSecret,
		RedirectURL:           cfg.OpenAIOAuthRedirectURL,
		AuthURL:               cfg.OpenAIOAuthAuthURL,
		TokenURL:              cfg.OpenAIOAuthTokenURL,
		APIBaseURL:            cfg.OpenAIAPIBaseURL,
		Secret:                cfg.EncryptionSecret,
		AllowHTTPAPIUpstreams: cfg.AllowHTTPAPIUpstreams,
	})
	autoTestRunner := provider.NewAutoTestRunner(providerService, provider.AutoTestRunnerConfig{
		Enabled:  cfg.ProviderAccountAutoTestEnabled,
		Interval: cfg.ProviderAccountAutoTestInterval,
	}, slog.Default())
	go autoTestRunner.Run(ctx)

	gatewayProxy := gateway.NewProxy(adminService, gatewayAccountProvider{service: providerService}, gateway.Config{
		UpstreamBaseURL:                 cfg.OpenAIAPIBaseURL,
		MaxConcurrentGatewayRequests:    cfg.GatewayMaxConcurrentRequests,
		MaxConcurrentRequestsPerAccount: cfg.GatewayMaxConcurrentRequestsPerAccount,
		MaxConcurrentRequestsPerKey:     cfg.GatewayMaxConcurrentRequestsPerKey,
		MaxRequestsPerMinutePerKey:      cfg.GatewayRequestsPerMinutePerKey,
		MaxTokensPerMinutePerKey:        cfg.GatewayTokensPerMinutePerKey,
		SettingsProvider:                adminService,
		Logger:                          store.NewGatewayRepository(pool),
		ModelProvider: gatewayModelProvider{
			admins:    adminService,
			providers: providerService,
		},
		UsagePricer: gatewayUsagePricer{admins: adminService},
	})

	server := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           httpapi.NewServer(cfg, pool, adminService, providerService, gatewayProxy, os.DirFS("frontend/build")),
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		slog.Info("starting n2api", "addr", cfg.Addr())
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server stopped", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("server shutdown failed", "error", err)
			os.Exit(1)
		}
	}
}
