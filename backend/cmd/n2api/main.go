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
	"github.com/KnowSky404/N2API/backend/internal/alerting"
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

func (p gatewayAccountProvider) RecordAccountRecovered(ctx context.Context, accountID int64) error {
	return p.service.RecordAccountRecovered(ctx, accountID)
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
	if len(os.Args) > 1 {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if exitCode := runAdminCommandWithOperations(
			ctx, os.Args[1:], os.Stdout, os.Stderr,
			newVerifyEncryptionFunc(os.Getenv), newCleanupOAuthStatesFunc(os.Getenv),
			newCheckEncryptionRotationFunc(os.Getenv),
		); exitCode != 0 {
			os.Exit(exitCode)
		}
		return
	}
	runServer()
}

func runServer() {
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
	alertingRepo := store.NewAlertingRepository(pool)
	alertingService := alerting.NewService(alertingRepo, cfg.EncryptionKeyring)
	alertHTTPAdapter := alerting.NewHTTPAdapter(nil)
	alertActionTester := alerting.NewActionTester(alertingService, alertHTTPAdapter)
	var initialAlertSubscription alerting.EventSubscription
	if cfg.AlertDeliveryEnabled {
		initialAlertSubscription, err = systemEventRepo.Subscribe(ctx)
		if err != nil {
			slog.Error("alert delivery listener unavailable", "error_code", "alert_delivery_listener_unavailable")
			os.Exit(1)
		}
	}
	alertDispatcher := alerting.NewDispatcher(alerting.DispatcherConfig{
		Enabled: cfg.AlertDeliveryEnabled, Service: alertingService, Recorder: systemEventRepo,
		Adapter: alertHTTPAdapter, InitialSubscription: initialAlertSubscription,
		Subscribe: func(ctx context.Context) (alerting.EventSubscription, error) {
			return systemEventRepo.Subscribe(ctx)
		},
		GetEvent: systemEventRepo.GetByID,
	})

	adminRepo := store.NewAdminRepository(pool, cfg.EncryptionSecret)
	adminService := admin.NewService(adminRepo, admin.Config{
		SessionTTL:        cfg.AdminSessionTTL,
		EncryptionSecret:  cfg.EncryptionSecret,
		EncryptionKeyring: cfg.EncryptionKeyring,
		SystemEvents:      systemEventRepo,
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
	alertDispatcher.Start()
	go runAPIKeyCleanup(ctx, adminService, systemEventRepo, time.Hour)
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
		EncryptionKeyring:     cfg.EncryptionKeyring,
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
	requestLogRetentionRunner := admin.NewRequestLogRetentionRunner(adminService, admin.RequestLogRetentionRunnerConfig{
		Enabled: cfg.RequestLogRetentionRunnerEnabled, Interval: cfg.RequestLogRetentionInterval, BatchSize: cfg.RequestLogRetentionBatchSize,
	}, slog.Default())
	requestLogRetentionRunner.SetSystemEventRecorder(systemEventRepo)
	go requestLogRetentionRunner.Run(ctx)
	apiKeyBudgetMonitor := admin.NewAPIKeyBudgetMonitor(adminRepo, admin.APIKeyBudgetMonitorConfig{}, slog.Default())
	go apiKeyBudgetMonitor.Run(ctx)
	routingExhaustionProjector := admin.NewRoutingExhaustionProjector(adminRepo, admin.RoutingExhaustionProjectorConfig{}, slog.Default())
	go routingExhaustionProjector.Run(ctx)

	gatewayProxy := gateway.NewProxy(adminService, gatewayAccountProvider{service: providerService}, gateway.Config{
		UpstreamBaseURL:                 cfg.OpenAIAPIBaseURL,
		MaxConcurrentGatewayRequests:    cfg.GatewayMaxConcurrentRequests,
		MaxConcurrentRequestsPerAccount: cfg.GatewayMaxConcurrentRequestsPerAccount,
		MaxConcurrentRequestsPerKey:     cfg.GatewayMaxConcurrentRequestsPerKey,
		MaxRequestsPerMinutePerKey:      cfg.GatewayRequestsPerMinutePerKey,
		MaxTokensPerMinutePerKey:        cfg.GatewayTokensPerMinutePerKey,
		MaxAcceptedRequestBodyBytes:     cfg.GatewayMaxAcceptedRequestBodyBytes,
		MaxInMemoryReplayBodyBytes:      cfg.GatewayMaxInMemoryReplayBodyBytes,
		MaxUpstreamResponseBodyBytes:    cfg.GatewayMaxUpstreamResponseBodyBytes,
		RequestBodyTimeout:              cfg.HTTPRequestBodyTimeout,
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

	server := newHTTPServer(
		cfg,
		httpapi.NewServer(cfg, pool, adminService, providerService, gatewayProxy, autoTestRunner, requestLogRetentionRunner, os.DirFS("frontend/build"), systemEventRepo, build, alertDispatcher, alertingService, alertActionTester),
	)

	serverErrors := make(chan error, 1)
	go func() {
		slog.Info("starting n2api", "addr", cfg.Addr(), "version", build.Version, "commit", build.Commit, "built_at", build.BuiltAt)
		serverErrors <- server.ListenAndServe()
	}()

	exitCode := 0
	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server stopped", "error", err)
			exitCode = 1
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("server shutdown failed", "error", err)
			exitCode = 1
		}
		cancel()
	}
	stop()
	dispatcherShutdownCtx, cancelDispatcher := context.WithTimeout(context.Background(), 10*time.Second)
	if err := alertDispatcher.Shutdown(dispatcherShutdownCtx); err != nil {
		slog.Error("alert delivery shutdown failed", "error_code", "alert_delivery_shutdown_failed")
		exitCode = 1
	}
	cancelDispatcher()
	if exitCode != 0 {
		pool.Close()
		os.Exit(exitCode)
	}
}

func newHTTPServer(cfg config.Config, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              cfg.Addr(),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       cfg.HTTPIdleTimeout,
		MaxHeaderBytes:    cfg.HTTPMaxHeaderBytes,
	}
}

type apiKeyCleanupService interface {
	PurgeExpiredAPIKeys(ctx context.Context) (int64, error)
}

type apiKeyCleanupEventRecorder interface {
	Insert(ctx context.Context, event systemevent.Event) error
}

func runAPIKeyCleanup(ctx context.Context, service apiKeyCleanupService, events apiKeyCleanupEventRecorder, interval time.Duration) {
	cleanup := func() {
		runAPIKeyCleanupCycle(ctx, service, events, slog.Default(), time.Now)
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

func runAPIKeyCleanupCycle(ctx context.Context, service apiKeyCleanupService, events apiKeyCleanupEventRecorder, logger *slog.Logger, now func() time.Time) {
	started := now().UTC()
	deleted, err := service.PurgeExpiredAPIKeys(ctx)
	if err == nil {
		if deleted > 0 {
			logger.Info("physically deleted expired API keys", "count", deleted)
		}
		return
	}
	if ctx.Err() != nil {
		return
	}

	finished := now().UTC()
	metadata, _ := systemevent.SafeMetadata(map[string]any{
		"retention_days": int64(admin.APIKeyPhysicalDeleteRetention / (24 * time.Hour)),
	}, "retention_days")
	eventCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	eventCtx = systemevent.WithRequestContext(eventCtx, systemevent.RequestContext{
		CorrelationID: systemevent.NewCorrelationID(),
		Actor:         systemevent.Actor{Type: systemevent.ActorSystem, Name: "api_key_purge"},
	})
	event := systemevent.BuildEvent(eventCtx, systemevent.EventIntent{
		Category:  systemevent.CategoryScheduler,
		Severity:  systemevent.SeverityError,
		Action:    systemevent.ActionSchedulerAPIKeyPurgeFailed,
		Outcome:   systemevent.OutcomeFailure,
		Target:    systemevent.Target{Type: "client_api_key_collection"},
		ErrorCode: "api_key_purge_failed",
		Message:   "API key purge failed",
		Metadata:  metadata,
	}, systemevent.Target{}, finished, finished.Sub(started))
	if events != nil {
		if recordErr := events.Insert(eventCtx, event); recordErr != nil {
			logger.Error("API key purge failure event recording failed", "error_code", "api_key_purge_event_record_failed")
		}
	}
	logger.Error("api key cleanup failed", "error_code", "api_key_purge_failed")
}

type systemEventRetentionStore interface {
	DeleteBeforeBatch(ctx context.Context, before time.Time, batchSize int) (int64, error)
	Insert(ctx context.Context, event systemevent.Event) error
}

func runSystemEventCleanup(ctx context.Context, events systemEventRetentionStore, retentionDays int, interval time.Duration) {
	cleanup := func() {
		runSystemEventCleanupCycle(ctx, events, retentionDays, slog.Default(), time.Now)
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

func runSystemEventCleanupCycle(ctx context.Context, events systemEventRetentionStore, retentionDays int, logger *slog.Logger, now func() time.Time) {
	started := now().UTC()
	cutoff := started.Add(-time.Duration(retentionDays) * 24 * time.Hour)
	var deleted int64
	for {
		count, err := events.DeleteBeforeBatch(ctx, cutoff, 1000)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			recordSystemEventRetentionFailure(ctx, events, logger, started, now().UTC(), cutoff, retentionDays, deleted)
			logger.Error("system event retention failed", "error_code", "system_event_retention_failed")
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
	}, systemevent.Target{}, now().UTC(), 0)
	if err := events.Insert(ctx, event); err != nil && ctx.Err() == nil {
		logger.Error("system event retention summary failed", "error_code", "system_event_retention_summary_failed")
	}
}

func recordSystemEventRetentionFailure(ctx context.Context, events systemEventRetentionStore, logger *slog.Logger, started, finished, cutoff time.Time, retentionDays int, deleted int64) {
	severity := systemevent.SeverityError
	outcome := systemevent.OutcomeFailure
	if deleted > 0 {
		severity = systemevent.SeverityWarning
		outcome = systemevent.OutcomePartial
	}
	metadata, _ := systemevent.SafeMetadata(map[string]any{
		"cutoff": cutoff.Format(time.RFC3339), "deleted_count": deleted, "retention_days": retentionDays,
	}, "cutoff", "deleted_count", "retention_days")
	eventCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	eventCtx = systemevent.WithRequestContext(eventCtx, systemevent.RequestContext{
		CorrelationID: systemevent.NewCorrelationID(), Actor: systemevent.Actor{Type: systemevent.ActorSystem},
	})
	event := systemevent.BuildEvent(eventCtx, systemevent.EventIntent{
		Category: systemevent.CategoryScheduler, Severity: severity,
		Action: systemevent.ActionSchedulerEventRetentionFailed, Outcome: outcome,
		Target:    systemevent.Target{Type: "system_events", ID: "retention", Name: "System event retention"},
		ErrorCode: "system_event_retention_failed", Message: "System event retention failed", Metadata: metadata,
	}, systemevent.Target{}, finished, finished.Sub(started))
	if err := events.Insert(eventCtx, event); err != nil {
		logger.Error("system event retention failure event recording failed", "error_code", "system_event_retention_failure_event_record_failed")
	}
}
