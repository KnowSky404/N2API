package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/KnowSky404/N2API/backend/internal/buildinfo"
	"github.com/KnowSky404/N2API/backend/internal/config"
	"github.com/KnowSky404/N2API/backend/internal/gateway"
	"github.com/KnowSky404/N2API/backend/internal/httpapi"
	"github.com/KnowSky404/N2API/backend/internal/provider"
	"github.com/KnowSky404/N2API/backend/internal/store"
	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

type gatewayAccountProvider struct {
	service *provider.Service
}

var _ gateway.AccountProvider = gatewayAccountProvider{}
var _ gateway.StickyAccountProvider = gatewayAccountProvider{}
var _ gateway.RoutingPoolAccountProvider = gatewayAccountProvider{}
var _ gateway.RoutingPoolChainAccountProvider = gatewayAccountProvider{}
var _ gateway.AccountUsageRecorder = gatewayAccountProvider{}

func (p gatewayAccountProvider) SelectAccountForModel(ctx context.Context, model string, excludedAccountIDs ...int64) (gateway.SelectedAccount, error) {
	selected, err := p.service.SelectAccountForModel(ctx, model, excludedAccountIDs...)
	return selectedGatewayAccount(selected, err)
}

func (p gatewayAccountProvider) SelectAccountForModelAndSession(ctx context.Context, model, sessionID string, excludedAccountIDs ...int64) (gateway.SelectedAccount, error) {
	selected, err := p.service.SelectAccountForModelAndSession(ctx, model, sessionID, excludedAccountIDs...)
	return selectedGatewayAccount(selected, err)
}

func (p gatewayAccountProvider) SelectAccountForModelInRoutingPool(ctx context.Context, routingPoolID int64, model string, excludedAccountIDs ...int64) (gateway.SelectedAccount, error) {
	selected, err := p.service.SelectAccountForModelInRoutingPool(ctx, routingPoolID, model, excludedAccountIDs...)
	return selectedGatewayAccount(selected, err)
}

func (p gatewayAccountProvider) SelectAccountForModelAndSessionInRoutingPool(ctx context.Context, routingPoolID int64, model, sessionID string, excludedAccountIDs ...int64) (gateway.SelectedAccount, error) {
	selected, err := p.service.SelectAccountForModelAndSessionInRoutingPool(ctx, routingPoolID, model, sessionID, excludedAccountIDs...)
	return selectedGatewayAccount(selected, err)
}

func (p gatewayAccountProvider) SelectAccountForModelInRoutingPoolChain(ctx context.Context, routingPoolID int64, model string, excludedAccountIDs ...int64) (gateway.SelectedAccount, error) {
	selected, err := p.service.SelectAccountForModelInRoutingPoolChain(ctx, routingPoolID, model, excludedAccountIDs...)
	return selectedGatewayAccount(selected, err)
}

func (p gatewayAccountProvider) SelectAccountForModelAndSessionInRoutingPoolChain(ctx context.Context, routingPoolID int64, model, sessionID string, excludedAccountIDs ...int64) (gateway.SelectedAccount, error) {
	selected, err := p.service.SelectAccountForModelAndSessionInRoutingPoolChain(ctx, routingPoolID, model, sessionID, excludedAccountIDs...)
	return selectedGatewayAccount(selected, err)
}

func selectedGatewayAccount(selected provider.SelectedAccount, err error) (gateway.SelectedAccount, error) {
	mapped := gateway.SelectedAccount{
		AccountID:                selected.AccountID,
		Provider:                 selected.Provider,
		AccountType:              selected.AccountType,
		DisplayName:              selected.DisplayName,
		AuthorizationToken:       selected.AuthorizationToken,
		BaseURL:                  selected.BaseURL,
		ProxyURL:                 selected.ProxyURL,
		ChatGPTAccountID:         selected.ChatGPTAccountID,
		MaxConcurrentRequests:    selected.MaxConcurrentRequests,
		RoutingPoolID:            selected.RoutingPoolID,
		RoutingPoolName:          selected.RoutingPoolName,
		RoutingPoolFallbackDepth: selected.RoutingPoolFallbackDepth,
		RoutingPoolFallbackChain: selected.RoutingPoolFallbackChain,
		RoutingPoolError:         selected.RoutingPoolError,
		FingerprintUA:            selected.FingerprintUA,
		FingerprintTLS:           selected.FingerprintTLS,
		FingerprintHeaders:       selected.FingerprintHeaders,
	}
	if err != nil {
		return mapped, err
	}
	return mapped, nil
}

func (p gatewayAccountProvider) RecordAccountFailure(ctx context.Context, accountID int64, statusCode int, retryAfter, message string) error {
	return p.service.RecordAccountFailure(ctx, accountID, statusCode, retryAfter, message)
}

func (p gatewayAccountProvider) RefreshAccountAuthorization(ctx context.Context, accountID int64, rejectedAccessToken string, statusCode int, message string) (string, bool, bool, error) {
	return p.service.RefreshAccountAuthorization(ctx, accountID, rejectedAccessToken, statusCode, message)
}

func (p gatewayAccountProvider) RecordAccountUsed(ctx context.Context, accountID int64) error {
	return p.service.RecordAccountUsed(ctx, accountID)
}

type gatewayModelProvider struct {
	admins    *admin.Service
	providers *provider.Service
}

var _ gateway.RoutingPoolModelProvider = gatewayModelProvider{}

func (p gatewayModelProvider) DefaultModel(ctx context.Context) (string, error) {
	settings, err := p.admins.GetModelSettings(ctx)
	if err != nil {
		return "", err
	}
	return settings.DefaultModel, nil
}

func (p gatewayModelProvider) ListExposedModelsForRoutingPoolChain(ctx context.Context, routingPoolID int64) ([]gateway.ExposedModel, error) {
	models, err := p.providers.ListExposedModelsForRoutingPoolChain(ctx, routingPoolID)
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
	build := buildinfo.Current()
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
	systemEventRepo := store.NewSystemEventRepository(pool, cfg.EncryptionSecret)

	adminRepo := store.NewAdminRepository(pool)
	adminService := admin.NewService(adminRepo, admin.Config{
		SessionTTL:       cfg.AdminSessionTTL,
		EncryptionSecret: cfg.EncryptionSecret,
		SystemEvents:     systemEventRepo,
		DefaultGatewaySettings: admin.GatewaySettings{
			MaxConcurrentGatewayRequests:           cfg.GatewayMaxConcurrentRequests,
			MaxConcurrentRequestsPerAccount:        cfg.GatewayMaxConcurrentRequestsPerAccount,
			MaxConcurrentRequestsPerKey:            cfg.GatewayMaxConcurrentRequestsPerKey,
			RequestsPerMinutePerKey:                cfg.GatewayRequestsPerMinutePerKey,
			TokensPerMinutePerKey:                  cfg.GatewayTokensPerMinutePerKey,
			ProviderAccountAutoTestEnabled:         cfg.ProviderAccountAutoTestEnabled,
			ProviderAccountAutoTestIntervalSeconds: int(cfg.ProviderAccountAutoTestInterval / time.Second),
		},
	})
	if err := adminService.BootstrapAdmin(ctx, cfg.AdminUsername, cfg.AdminPassword); err != nil {
		slog.Error("admin bootstrap failed", "error", err)
		os.Exit(1)
	}
	go runAPIKeyCleanup(ctx, adminService, time.Hour)
	if cfg.SystemEventRetentionDays > 0 {
		go runSystemEventCleanup(ctx, systemEventRepo, cfg.SystemEventRetentionDays, 24*time.Hour)
	}

	providerRepo := store.NewProviderRepository(pool)
	requestLogRepo := store.NewGatewayRepository(pool)
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
		AccountTestLogger:     requestLogRepo,
	})
	autoTestRunner := provider.NewAutoTestRunnerWithConfigSource(providerService, func(ctx context.Context) (provider.AutoTestRunnerConfig, error) {
		settings, err := adminService.GetGatewaySettings(ctx)
		if err != nil {
			return provider.AutoTestRunnerConfig{}, err
		}
		return provider.AutoTestRunnerConfig{
			Enabled:  settings.ProviderAccountAutoTestEnabled,
			Interval: time.Duration(settings.ProviderAccountAutoTestIntervalSeconds) * time.Second,
		}, nil
	}, slog.Default())
	autoTestRunner.SetSystemEventRecorder(systemEventRepo)
	go autoTestRunner.Run(ctx)

	gatewayProxy := gateway.NewProxy(adminService, gatewayAccountProvider{service: providerService}, gateway.Config{
		UpstreamBaseURL:                 cfg.OpenAIAPIBaseURL,
		MaxConcurrentGatewayRequests:    cfg.GatewayMaxConcurrentRequests,
		MaxConcurrentRequestsPerAccount: cfg.GatewayMaxConcurrentRequestsPerAccount,
		MaxConcurrentRequestsPerKey:     cfg.GatewayMaxConcurrentRequestsPerKey,
		MaxRequestsPerMinutePerKey:      cfg.GatewayRequestsPerMinutePerKey,
		MaxTokensPerMinutePerKey:        cfg.GatewayTokensPerMinutePerKey,
		SettingsProvider:                adminService,
		BudgetProvider:                  adminService,
		ErrorPassthroughRulesProvider:   adminService,
		Logger:                          requestLogRepo,
		ModelProvider: gatewayModelProvider{
			admins:    adminService,
			providers: providerService,
		},
		UsagePricer: gatewayUsagePricer{admins: adminService},
	})

	server := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           httpapi.NewServer(cfg, pool, adminService, providerService, gatewayProxy, autoTestRunner, os.DirFS("frontend/build"), systemEventRepo, build),
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		slog.Info("starting n2api", "addr", cfg.Addr(), "version", build.Version, "commit", build.Commit, "built_at", build.BuiltAt)
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

func runAPIKeyCleanup(ctx context.Context, service *admin.Service, interval time.Duration) {
	cleanup := func() {
		deleted, err := service.PurgeExpiredAPIKeys(ctx)
		if err != nil {
			if ctx.Err() == nil {
				slog.Error("api key cleanup failed", "error", err)
			}
			return
		}
		if deleted > 0 {
			slog.Info("physically deleted expired API keys", "count", deleted)
		}
	}

	cleanup()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanup()
		}
	}
}

type systemEventRetentionStore interface {
	DeleteBeforeBatch(ctx context.Context, before time.Time, batchSize int) (int64, error)
	Insert(ctx context.Context, event systemevent.Event) error
}

func runSystemEventCleanup(ctx context.Context, events systemEventRetentionStore, retentionDays int, interval time.Duration) {
	cleanup := func() {
		cutoff := time.Now().UTC().Add(-time.Duration(retentionDays) * 24 * time.Hour)
		var deleted int64
		for {
			count, err := events.DeleteBeforeBatch(ctx, cutoff, 1000)
			if err != nil {
				if ctx.Err() == nil {
					slog.Error("system event retention failed", "error_code", "system_event_retention_failed")
				}
				return
			}
			deleted += count
			if count < 1000 {
				break
			}
		}
		metadata, _ := systemevent.SafeMetadata(map[string]any{
			"cutoff": cutoff.Format(time.RFC3339), "deleted_count": deleted, "retention_days": retentionDays,
		}, "cutoff", "deleted_count", "retention_days")
		requestContext := systemevent.RequestContext{
			CorrelationID: systemevent.NewCorrelationID(), Actor: systemevent.Actor{Type: systemevent.ActorSystem},
		}
		event := systemevent.BuildEvent(systemevent.WithRequestContext(ctx, requestContext), systemevent.EventIntent{
			Category: systemevent.CategoryScheduler, Severity: systemevent.SeverityInfo,
			Action: systemevent.ActionSchedulerEventRetentionCompleted, Outcome: systemevent.OutcomeSuccess,
			Target:  systemevent.Target{Type: "system_events", ID: "retention", Name: "System event retention"},
			Message: "System event retention completed", Metadata: metadata,
		}, systemevent.Target{}, time.Now().UTC(), 0)
		if err := events.Insert(ctx, event); err != nil && ctx.Err() == nil {
			slog.Error("system event retention summary failed", "error_code", "system_event_retention_summary_failed")
		}
	}
	cleanup()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanup()
		}
	}
}
