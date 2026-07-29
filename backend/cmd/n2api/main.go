package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
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
	"github.com/KnowSky404/N2API/backend/internal/lifecycle"
	"github.com/KnowSky404/N2API/backend/internal/metrics"
	"github.com/KnowSky404/N2API/backend/internal/provider"
	"github.com/KnowSky404/N2API/backend/internal/requestlog"
	"github.com/KnowSky404/N2API/backend/internal/store"
	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

type gatewayAccountProvider struct {
	service *provider.Service
}

type responseAffinityRetentionStore struct {
	repository *store.ResponseAffinityRepository
}

func (s responseAffinityRetentionStore) TryAcquireResponseAffinityRetention(ctx context.Context) (gateway.ResponseAffinityRetentionLease, bool, error) {
	return s.repository.TryAcquireRetention(ctx)
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

func (p gatewayAccountProvider) SelectAccountByIDInRoutingPoolChain(ctx context.Context, routingPoolID, accountID int64, model string) (gateway.SelectedAccount, error) {
	selected, err := p.service.SelectAccountByIDInRoutingPoolChain(ctx, routingPoolID, accountID, model)
	return selectedGatewayAccount(selected, err)
}

func (p gatewayAccountProvider) SelectSingleAccountInRoutingPoolChain(ctx context.Context, routingPoolID int64, model string) (gateway.SelectedAccount, bool, error) {
	selected, unique, err := p.service.SelectSingleAccountInRoutingPoolChain(ctx, routingPoolID, model)
	mapped, mappedErr := selectedGatewayAccount(selected, err)
	return mapped, unique, mappedErr
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
	if exitCode := runServer(); exitCode != 0 {
		os.Exit(exitCode)
	}
}

func runServer() int {
	build := buildinfo.Current()
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		return 1
	}

	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	connectionFactory, err := store.NewPostgresConnectionFactory(cfg.DatabaseURL)
	if err != nil {
		slog.Error("database configuration unavailable", "error_code", "database_configuration_unavailable")
		return 1
	}

	var instanceLock *store.InstanceLock
	var instanceLockLost <-chan struct{}
	if cfg.AllowUnsafeMultiInstance {
		slog.Warn("unsafe multi-instance operation enabled", "error_code", "unsafe_multi_instance_enabled")
	} else {
		instanceConnection, connectErr := connectionFactory.Connect(signalCtx, store.PostgresApplicationNameInstanceLock)
		if connectErr != nil {
			slog.Error("instance lock unavailable", "error_code", "instance_lock_unavailable")
			return 1
		}
		var acquired bool
		instanceLock, acquired, err = store.TryAcquireInstanceLock(signalCtx, instanceConnection)
		if err != nil {
			slog.Error("instance lock unavailable", "error_code", "instance_lock_unavailable")
			return 1
		}
		if !acquired {
			slog.Error("another n2api instance is active", "error_code", "instance_already_running")
			return 1
		}
		instanceLockLost = instanceLock.Lost()
	}

	pool, err := store.OpenPool(signalCtx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("database unavailable", "error_code", "database_unavailable")
		if instanceLock != nil {
			_ = instanceLock.Close()
		}
		return 1
	}
	poolOwnedByRuntime := false
	defer func() {
		if !poolOwnedByRuntime {
			pool.Close()
		}
	}()
	instanceLockOwnedByRuntime := false
	if instanceLock != nil {
		defer func() {
			if instanceLockOwnedByRuntime {
				return
			}
			if err := instanceLock.Close(); err != nil {
				slog.Error("instance lock release failed", "error_code", "instance_lock_release_failed")
			}
		}()
	}
	var metricsRegistry *metrics.Registry
	var taskMetrics backgroundTaskObserver
	var gatewayMetrics gateway.MetricsObserver
	var providerMetrics provider.MetricsObserver
	if cfg.MetricsEnabled {
		metricsRegistry = metrics.New(pool)
		taskMetrics = metricsRegistry
		gatewayMetrics = metricsRegistry
		providerMetrics = metricsRegistry
	}
	startupCtx := signalCtx
	if metricsRegistry != nil {
		startupCtx = systemevent.WithWriteObserver(startupCtx, metricsRegistry)
	}

	migrationConnection, err := connectionFactory.Connect(startupCtx, store.PostgresApplicationNameMigrationLock)
	if err != nil {
		slog.Error("database migration lock unavailable", "error_code", "database_migration_lock_unavailable")
		return 1
	}
	migrationLock, err := store.AcquireMigrationLock(startupCtx, migrationConnection)
	if err != nil {
		slog.Error("database migration lock unavailable", "error_code", "database_migration_lock_unavailable")
		return 1
	}
	defer func() {
		if err := migrationLock.Close(); err != nil {
			slog.Error("database migration lock release failed", "error_code", "database_migration_lock_release_failed")
		}
	}()
	migrationCriticalCtx, cancelMigrationCritical := context.WithCancel(startupCtx)
	migrationWatchCtx, stopMigrationWatch := context.WithCancel(context.Background())
	go func() {
		select {
		case <-migrationLock.Lost():
			cancelMigrationCritical()
		case <-migrationWatchCtx.Done():
		}
	}()
	defer stopMigrationWatch()
	defer cancelMigrationCritical()
	migrationPool, err := store.OpenMigrationPool(migrationCriticalCtx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("database migration connection unavailable", "error_code", "database_migration_connection_unavailable")
		return 1
	}
	if err := store.RunMigrations(migrationCriticalCtx, migrationPool); err != nil {
		migrationPool.Close()
		if migrationCriticalCtx.Err() != nil && signalCtx.Err() == nil {
			slog.Error("database migration lock lost", "error_code", "database_migration_lock_lost")
			return 1
		}
		slog.Error("database migration failed", "error_code", "database_migration_failed")
		return 1
	}
	migrationPool.Close()

	systemEventRepo := store.NewSystemEventRepositoryWithSubscriptionFactory(
		pool,
		cfg.EncryptionSecret,
		connectionFactory.Connector(store.PostgresApplicationNameSystemEventListener),
	)
	if metricsRegistry != nil {
		systemEventRepo.SetWriteObserver(metricsRegistry)
	}
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
	if err := adminService.BootstrapAdmin(migrationCriticalCtx, cfg.AdminUsername, cfg.AdminPassword); err != nil {
		if migrationCriticalCtx.Err() != nil && signalCtx.Err() == nil {
			slog.Error("database migration lock lost", "error_code", "database_migration_lock_lost")
			return 1
		}
		slog.Error("admin bootstrap failed", "error_code", "admin_bootstrap_failed")
		return 1
	}
	if err := migrationLock.Close(); err != nil {
		if errors.Is(err, store.ErrMigrationLockLost) {
			slog.Error("database migration lock lost", "error_code", "database_migration_lock_lost")
		} else {
			slog.Error("database migration lock release failed", "error_code", "database_migration_lock_release_failed")
		}
		return 1
	}
	stopMigrationWatch()
	cancelMigrationCritical()

	alertingRepo := store.NewAlertingRepository(pool)
	alertingService := alerting.NewService(alertingRepo, cfg.EncryptionKeyring)
	alertHTTPAdapter := alerting.NewHTTPAdapter(nil)
	alertActionTester := alerting.NewActionTester(alertingService, alertHTTPAdapter)
	var initialAlertSubscription alerting.EventSubscription
	if cfg.AlertDeliveryEnabled {
		initialAlertSubscription, err = systemEventRepo.Subscribe(startupCtx)
		if err != nil {
			slog.Error("alert delivery listener unavailable", "error_code", "alert_delivery_listener_unavailable")
			return 1
		}
	}
	alertDispatcher := alerting.NewDispatcher(alerting.DispatcherConfig{
		Enabled: cfg.AlertDeliveryEnabled, Service: alertingService, Recorder: systemEventRepo,
		Adapter: alertHTTPAdapter, Metrics: metricsRegistry, InitialSubscription: initialAlertSubscription,
		Subscribe: func(ctx context.Context) (alerting.EventSubscription, error) {
			return systemEventRepo.Subscribe(ctx)
		},
		GetEvent: systemEventRepo.GetByID,
	})
	alertDispatcher.Start()
	alertShutdownHandled := false
	defer func() {
		if alertShutdownHandled {
			return
		}
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancelCleanup()
		if err := alertDispatcher.Shutdown(cleanupCtx); err != nil {
			slog.Error("alert delivery startup cleanup failed", "error_code", "alert_delivery_startup_cleanup_failed")
		}
	}()

	providerRepo := store.NewProviderRepository(pool)
	requestLogRepo := store.NewGatewayRepository(pool)
	requestLogWriteMonitor := requestlog.NewWriteMonitor(slog.Default())
	if metricsRegistry != nil {
		requestLogWriteMonitor.SetObserver(metricsRegistry)
	}
	responseAffinityRepo := store.NewResponseAffinityRepository(pool, cfg.EncryptionSecret)
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
		RequestLogObserver:    requestLogWriteMonitor,
		Metrics:               providerMetrics,
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
	if metricsRegistry != nil {
		autoTestRunner.SetMetricsObserver(metricsRegistry)
	}
	requestLogRetentionRunner := admin.NewRequestLogRetentionRunner(adminService, admin.RequestLogRetentionRunnerConfig{
		Enabled: cfg.RequestLogRetentionRunnerEnabled, Interval: cfg.RequestLogRetentionInterval, BatchSize: cfg.RequestLogRetentionBatchSize,
	}, slog.Default())
	requestLogRetentionRunner.SetSystemEventRecorder(systemEventRepo)
	if metricsRegistry != nil {
		requestLogRetentionRunner.SetMetricsObserver(metricsRegistry)
	}
	responseAffinityRetentionRunner := gateway.NewResponseAffinityRetentionRunner(
		responseAffinityRetentionStore{repository: responseAffinityRepo},
		gateway.ResponseAffinityRetentionRunnerConfig{
			Enabled: cfg.ResponseAffinityRetentionRunnerEnabled, Interval: cfg.ResponseAffinityRetentionInterval, BatchSize: cfg.ResponseAffinityRetentionBatchSize,
		},
		slog.Default(),
	)
	responseAffinityRetentionRunner.SetSystemEventRecorder(systemEventRepo)
	if metricsRegistry != nil {
		responseAffinityRetentionRunner.SetMetricsObserver(metricsRegistry)
	}
	apiKeyBudgetMonitor := admin.NewAPIKeyBudgetMonitor(adminRepo, admin.APIKeyBudgetMonitorConfig{}, slog.Default())
	if metricsRegistry != nil {
		apiKeyBudgetMonitor.SetMetricsObserver(metricsRegistry)
	}
	routingExhaustionProjector := admin.NewRoutingExhaustionProjector(adminRepo, admin.RoutingExhaustionProjectorConfig{}, slog.Default())
	if metricsRegistry != nil {
		routingExhaustionProjector.SetMetricsObserver(metricsRegistry)
	}

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
		UpstreamResponseHeaderTimeout:   cfg.UpstreamResponseHeaderTimeout,
		UpstreamConnectTimeout:          cfg.UpstreamConnectTimeout,
		UpstreamTLSHandshakeTimeout:     cfg.UpstreamTLSHandshakeTimeout,
		UpstreamSSEIdleTimeout:          cfg.UpstreamSSEIdleTimeout,
		SettingsProvider:                adminService,
		BudgetProvider:                  adminService,
		ErrorPassthroughRulesProvider:   adminService,
		ResponseAffinityStore:           responseAffinityRepo,
		ResponseAffinityTTL:             cfg.ResponseAffinityTTL,
		ProcessLogger:                   slog.Default(),
		RequestLogWriteMonitor:          requestLogWriteMonitor,
		Metrics:                         gatewayMetrics,
		Logger:                          requestLogRepo,
		ModelProvider: gatewayModelProvider{
			admins:    adminService,
			providers: providerService,
		},
		UsagePricer: gatewayUsagePricer{admins: adminService},
	})
	providerService.SetAccountTransportInvalidator(gatewayProxy)
	gatewayOwnedByRuntime := false
	defer func() {
		if !gatewayOwnedByRuntime {
			gatewayProxy.Close()
		}
	}()
	if metricsRegistry != nil {
		updateProviderAccountMetrics(startupCtx, providerService, metricsRegistry)
	}
	requestBaseCtx, cancelRequests := context.WithCancel(context.Background())
	requestRootCtx := requestBaseCtx
	backgroundRootCtx := context.Background()
	if metricsRegistry != nil {
		requestRootCtx = systemevent.WithWriteObserver(requestRootCtx, metricsRegistry)
		backgroundRootCtx = systemevent.WithWriteObserver(backgroundRootCtx, metricsRegistry)
	}
	metricsRootCtx, cancelMetricsRoot := context.WithCancel(context.Background())
	runtimeReadiness := lifecycle.NewReadiness()
	requestTracker := newRuntimeRequestTracker()

	server := newHTTPServer(
		cfg,
		requestTracker.Wrap(httpapi.NewServer(cfg, pool, adminService, providerService, gatewayProxy, autoTestRunner, requestLogRetentionRunner, responseAffinityRetentionRunner, requestLogWriteMonitor, os.DirFS("frontend/build"), systemEventRepo, build, alertDispatcher, alertingService, alertActionTester, metricsRegistry, runtimeReadiness)),
		requestRootCtx,
	)
	var metricsServer *http.Server
	if metricsRegistry != nil {
		metricsServer = metrics.NewHTTPServer(cfg.MetricsAddr(), cfg.MetricsBearerToken, metricsRegistry.Handler(), metricsRootCtx)
	}

	mainListener, err := net.Listen("tcp", cfg.Addr())
	if err != nil {
		cancelRequests()
		cancelMetricsRoot()
		slog.Error("server listener unavailable", "error_code", "server_listener_unavailable")
		return 1
	}
	var metricsListener net.Listener
	if metricsServer != nil {
		metricsListener, err = net.Listen("tcp", cfg.MetricsAddr())
		if err != nil {
			_ = mainListener.Close()
			cancelRequests()
			cancelMetricsRoot()
			slog.Error("metrics server stopped", "error_code", "metrics_server_stopped")
			return 1
		}
	}

	serverSupervisor := lifecycle.NewSupervisor(context.Background())
	backgroundSupervisor := lifecycle.NewSupervisor(backgroundRootCtx)
	if err := serverSupervisor.Start("http_server", func(context.Context) error {
		slog.Info("starting n2api", "addr", cfg.Addr(), "version", build.Version, "commit", build.Commit, "built_at", build.BuiltAt)
		serveErr := server.Serve(mainListener)
		if errors.Is(serveErr, http.ErrServerClosed) {
			return nil
		}
		return serveErr
	}); err != nil {
		_ = mainListener.Close()
		if metricsListener != nil {
			_ = metricsListener.Close()
		}
		cancelRequests()
		cancelMetricsRoot()
		slog.Error("server startup failed", "error_code", "server_startup_failed")
		return 1
	}
	if metricsServer != nil {
		if err := serverSupervisor.Start("metrics_server", func(context.Context) error {
			slog.Info("starting n2api metrics", "addr", cfg.MetricsAddr())
			serveErr := metricsServer.Serve(metricsListener)
			if errors.Is(serveErr, http.ErrServerClosed) {
				return nil
			}
			return serveErr
		}); err != nil {
			serverSupervisor.Stop()
			_ = server.Close()
			_ = metricsListener.Close()
			cancelRequests()
			cancelMetricsRoot()
			slog.Error("metrics server stopped", "error_code", "metrics_server_stopped")
			return 1
		}
	}

	startBackground := func(name string, run func(context.Context)) error {
		return backgroundSupervisor.Start(name, func(ctx context.Context) error {
			run(ctx)
			return nil
		})
	}
	backgroundStartErr := startBackground("api_key_cleanup", func(ctx context.Context) {
		runAPIKeyCleanup(ctx, adminService, systemEventRepo, time.Hour, taskMetrics)
	})
	if backgroundStartErr == nil && cfg.SystemEventRetentionDays > 0 {
		backgroundStartErr = startBackground("system_event_retention", func(ctx context.Context) {
			runSystemEventCleanup(ctx, systemEventRepo, cfg.SystemEventRetentionDays, 24*time.Hour, taskMetrics)
		})
	}
	if backgroundStartErr == nil {
		backgroundStartErr = startBackground("provider_account_auto_test", autoTestRunner.Run)
	}
	if backgroundStartErr == nil && cfg.RequestLogRetentionRunnerEnabled {
		backgroundStartErr = startBackground("request_log_retention", requestLogRetentionRunner.Run)
	}
	if backgroundStartErr == nil && cfg.ResponseAffinityRetentionRunnerEnabled {
		backgroundStartErr = startBackground("response_affinity_retention", responseAffinityRetentionRunner.Run)
	}
	if backgroundStartErr == nil {
		backgroundStartErr = startBackground("api_key_budget_monitor", apiKeyBudgetMonitor.Run)
	}
	if backgroundStartErr == nil {
		backgroundStartErr = startBackground("routing_exhaustion_projector", routingExhaustionProjector.Run)
	}
	if backgroundStartErr == nil && metricsRegistry != nil {
		backgroundStartErr = startBackground("provider_account_metrics", func(ctx context.Context) {
			runProviderAccountMetrics(ctx, providerService, metricsRegistry, time.Minute)
		})
	}

	runtime := runtimeShutdown{
		mainServer: server, metricsServer: metricsServer, readiness: runtimeReadiness,
		metrics: metricsRegistry, background: backgroundSupervisor, servers: serverSupervisor,
		alerts: alertDispatcher, requests: requestTracker, cancelRequests: cancelRequests, cancelMetricsRoot: cancelMetricsRoot,
		finalizeResources: func(context.Context) error {
			gatewayProxy.Close()
			var closeErr error
			if instanceLock != nil {
				closeErr = instanceLock.Close()
			}
			pool.Close()
			return closeErr
		},
		totalTimeout: cfg.ShutdownTimeout, requestDrain: cfg.RequestDrainTimeout,
	}
	gatewayOwnedByRuntime = true
	instanceLockOwnedByRuntime = true
	poolOwnedByRuntime = true
	if backgroundStartErr != nil {
		slog.Error("background component startup failed", "error_code", "background_component_startup_failed")
		_ = runtime.run("startup_failure")
		alertShutdownHandled = true
		return 1
	}
	runtimeReadiness.MarkReady()
	if metricsRegistry != nil {
		metricsRegistry.SetReadiness("runtime", true)
	}

	exitCode := 0
	shutdownReason := "signal"
	select {
	case <-serverSupervisor.Failures():
		slog.Error("listener stopped unexpectedly", "error_code", "listener_stopped")
		shutdownReason = "listener_failure"
		exitCode = 1
	case <-backgroundSupervisor.Failures():
		slog.Error("background component stopped unexpectedly", "error_code", "background_component_stopped")
		shutdownReason = "background_failure"
		exitCode = 1
	case <-instanceLockLost:
		slog.Error("instance lock connection lost", "error_code", "instance_lock_lost")
		shutdownReason = "instance_lock_lost"
		exitCode = 1
	case <-signalCtx.Done():
	}
	shutdownReason, exitCode = preferCriticalRuntimeFailure(
		shutdownReason,
		exitCode,
		serverSupervisor,
		backgroundSupervisor,
		instanceLockLost,
	)
	if err := runtime.run(shutdownReason); err != nil {
		slog.Error("runtime shutdown failed", "error_code", "runtime_shutdown_failed")
		exitCode = 1
	}
	alertShutdownHandled = true
	return exitCode
}

func preferCriticalRuntimeFailure(
	reason string,
	exitCode int,
	servers *lifecycle.Supervisor,
	background *lifecycle.Supervisor,
	instanceLockLost <-chan struct{},
) (string, int) {
	if servers.LastFailure() != nil {
		return "listener_failure", 1
	}
	if background.LastFailure() != nil {
		return "background_failure", 1
	}
	select {
	case <-instanceLockLost:
		return "instance_lock_lost", 1
	default:
		return reason, exitCode
	}
}

type providerAccountMetricsSource interface {
	ListAccounts(ctx context.Context) ([]provider.Account, error)
}

func runProviderAccountMetrics(ctx context.Context, source providerAccountMetricsSource, registry *metrics.Registry, interval time.Duration) {
	if source == nil || registry == nil {
		return
	}
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			updateProviderAccountMetrics(ctx, source, registry)
		}
	}
}

func updateProviderAccountMetrics(ctx context.Context, source providerAccountMetricsSource, registry *metrics.Registry) {
	accounts, err := source.ListAccounts(ctx)
	if err != nil {
		slog.Warn("provider account metrics refresh failed", "error_code", "provider_account_metrics_refresh_failed")
		return
	}
	now := time.Now()
	snapshot := make([]metrics.ProviderAccount, 0, len(accounts))
	for _, account := range accounts {
		state := account.Status
		switch {
		case !account.Enabled:
			state = provider.AccountStatusDisabled
		case state == provider.AccountStatusRateLimited && (account.RateLimitedUntil == nil || !account.RateLimitedUntil.After(now)):
			state = provider.AccountStatusActive
		case state == provider.AccountStatusCircuitOpen && (account.CircuitOpenUntil == nil || !account.CircuitOpenUntil.After(now)):
			state = provider.AccountStatusActive
		case state == "":
			state = provider.AccountStatusActive
		}
		snapshot = append(snapshot, metrics.ProviderAccount{AccountType: account.AccountType, State: state})
	}
	registry.UpdateProviderAccounts(snapshot)
}

func newHTTPServer(cfg config.Config, handler http.Handler, baseContext context.Context) *http.Server {
	if baseContext == nil {
		baseContext = context.Background()
	}
	return &http.Server{
		Addr:              cfg.Addr(),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       cfg.HTTPIdleTimeout,
		MaxHeaderBytes:    cfg.HTTPMaxHeaderBytes,
		BaseContext: func(net.Listener) context.Context {
			return baseContext
		},
	}
}

type apiKeyCleanupService interface {
	PurgeExpiredAPIKeys(ctx context.Context) (int64, error)
}

type apiKeyCleanupEventRecorder interface {
	Insert(ctx context.Context, event systemevent.Event) error
}

type backgroundTaskObserver interface {
	BeginBackgroundTask(task string) func(outcome string)
	ObserveBackgroundTaskRun(task, outcome string, duration time.Duration)
}

func runAPIKeyCleanup(ctx context.Context, service apiKeyCleanupService, events apiKeyCleanupEventRecorder, interval time.Duration, observers ...backgroundTaskObserver) {
	cleanup := func() {
		runAPIKeyCleanupCycle(ctx, service, events, slog.Default(), time.Now, observers...)
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

func runAPIKeyCleanupCycle(ctx context.Context, service apiKeyCleanupService, events apiKeyCleanupEventRecorder, logger *slog.Logger, now func() time.Time, observers ...backgroundTaskObserver) {
	outcome := "failure"
	finishMetrics := func(string) {}
	if len(observers) > 0 && observers[0] != nil {
		finishMetrics = observers[0].BeginBackgroundTask("api_key_purge")
	}
	defer func() { finishMetrics(outcome) }()
	started := now().UTC()
	deleted, err := service.PurgeExpiredAPIKeys(ctx)
	if err == nil {
		outcome = "success"
		if deleted > 0 {
			logger.Info("physically deleted expired API keys", "count", deleted)
		}
		return
	}
	if ctx.Err() != nil {
		outcome = "canceled"
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

func runSystemEventCleanup(ctx context.Context, events systemEventRetentionStore, retentionDays int, interval time.Duration, observers ...backgroundTaskObserver) {
	cleanup := func() {
		runSystemEventCleanupCycle(ctx, events, retentionDays, slog.Default(), time.Now, observers...)
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

func runSystemEventCleanupCycle(ctx context.Context, events systemEventRetentionStore, retentionDays int, logger *slog.Logger, now func() time.Time, observers ...backgroundTaskObserver) {
	outcome := "failure"
	finishMetrics := func(string) {}
	if len(observers) > 0 && observers[0] != nil {
		finishMetrics = observers[0].BeginBackgroundTask("system_event_retention")
	}
	defer func() { finishMetrics(outcome) }()
	started := now().UTC()
	cutoff := started.Add(-time.Duration(retentionDays) * 24 * time.Hour)
	var deleted int64
	for {
		count, err := events.DeleteBeforeBatch(ctx, cutoff, 1000)
		if err != nil {
			if ctx.Err() != nil {
				outcome = "canceled"
				return
			}
			if deleted > 0 {
				outcome = "partial"
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
	outcome = "success"
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
