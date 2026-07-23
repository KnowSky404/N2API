package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/config"
	"github.com/KnowSky404/N2API/backend/internal/gateway"
	"github.com/KnowSky404/N2API/backend/internal/provider"
)

func TestNewHTTPServerAppliesInboundResourceBoundariesWithoutWriteTimeout(t *testing.T) {
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	server := newHTTPServer(config.Config{
		Host:               "127.0.0.1",
		Port:               3000,
		HTTPIdleTimeout:    75 * time.Second,
		HTTPMaxHeaderBytes: 512 << 10,
	}, handler, context.Background())
	if server.Addr != "127.0.0.1:3000" || server.Handler == nil || server.ReadHeaderTimeout != 5*time.Second || server.IdleTimeout != 75*time.Second || server.MaxHeaderBytes != 512<<10 {
		t.Fatalf("server = %+v", server)
	}
	if server.WriteTimeout != 0 || server.ReadTimeout != 0 {
		t.Fatalf("global timeouts = read:%s write:%s, want zero", server.ReadTimeout, server.WriteTimeout)
	}
}

func TestHTTPServerCancelsActiveRequestsWhenBaseContextEnds(t *testing.T) {
	baseContext, cancelBase := context.WithCancel(context.Background())
	requestStarted := make(chan struct{})
	requestCanceled := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-r.Context().Done()
		close(requestCanceled)
	})
	server := newHTTPServer(config.Config{}, handler, baseContext)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned error: %v", err)
	}
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		response, requestErr := (&http.Client{Timeout: 2 * time.Second}).Get("http://" + listener.Addr().String())
		if requestErr == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
	}()
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("active request did not start")
	}

	cancelBase()
	select {
	case <-requestCanceled:
	case <-time.After(time.Second):
		t.Fatal("active request was not canceled with server base context")
	}
	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownContext); err != nil {
		t.Fatalf("Shutdown returned error: %v", err)
	}
	select {
	case err := <-serveErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("Serve returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not stop")
	}
	select {
	case <-clientDone:
	case <-time.After(time.Second):
		t.Fatal("client request did not stop")
	}
}

func TestGatewayAccountProviderReportsAccountFailures(t *testing.T) {
	var _ gateway.AccountFailureReporter = gatewayAccountProvider{}
	var _ gateway.AccountAuthorizationRefresher = gatewayAccountProvider{}
	var _ gateway.AccountUsageRecorder = gatewayAccountProvider{}
	var _ gateway.AccountRecoveryRecorder = gatewayAccountProvider{}
}

func TestGatewayAccountProviderMapsDisplayNameForRequestLogs(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("ReadFile main.go returned error: %v", err)
	}
	text := string(source)
	for _, want := range []string{
		"DisplayName:",
		"selected.DisplayName",
		"MaxConcurrentRequests:",
		"selected.MaxConcurrentRequests",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("gatewayAccountProvider mapping missing %q", want)
		}
	}
}

func TestSelectedGatewayAccountPreservesDiagnosticsOnSelectionError(t *testing.T) {
	selected, err := selectedGatewayAccount(provider.SelectedAccount{
		RoutingPoolID:            7,
		RoutingPoolName:          "primary",
		RoutingPoolFallbackDepth: 1,
		RoutingPoolFallbackChain: "primary -> secondary",
		RoutingPoolError:         provider.RoutingPoolErrorExhausted,
	}, provider.ErrModelUnavailable)

	if !errors.Is(err, provider.ErrModelUnavailable) {
		t.Fatalf("error = %v, want ErrModelUnavailable", err)
	}
	if selected.RoutingPoolID != 7 ||
		selected.RoutingPoolName != "primary" ||
		selected.RoutingPoolFallbackDepth != 1 ||
		selected.RoutingPoolFallbackChain != "primary -> secondary" ||
		selected.RoutingPoolError != provider.RoutingPoolErrorExhausted {
		t.Fatalf("selected diagnostics = %+v, want provider routing pool diagnostics preserved", selected)
	}
}

func TestMainWiresProviderAccountAutoTestRunner(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("ReadFile main.go returned error: %v", err)
	}
	text := string(source)
	for _, want := range []string{
		"signal.NotifyContext",
		"provider.NewAutoTestRunnerWithConfigSource",
		"adminService.GetGatewaySettings",
		"admin.NewRequestLogRetentionRunner",
		"requestLogRetentionRunner.Run",
		"store.NewResponseAffinityRepository",
		"gateway.NewResponseAffinityRetentionRunner",
		"responseAffinityRetentionRunner.Run",
		"ResponseAffinityStore:           responseAffinityRepo",
		"ResponseAffinityTTL:             cfg.ResponseAffinityTTL",
		"admin.NewAPIKeyBudgetMonitor",
		"go apiKeyBudgetMonitor.Run(ctx)",
		"admin.NewRoutingExhaustionProjector",
		"go routingExhaustionProjector.Run(ctx)",
		"initialAlertSubscription, err = systemEventRepo.Subscribe(ctx)",
		"InitialSubscription: initialAlertSubscription",
		"ProviderAccountAutoTestEnabled",
		"ProviderAccountAutoTestInterval",
		"ProviderAccountAutoTestIntervalSeconds",
		"go autoTestRunner.Run(ctx)",
		"go runAPIKeyCleanup(ctx, adminService, systemEventRepo, time.Hour)",
		"service.PurgeExpiredAPIKeys(ctx)",
		"go runSystemEventCleanup(ctx, systemEventRepo, cfg.SystemEventRetentionDays, 24*time.Hour)",
		"runSystemEventCleanupCycle(ctx, events, retentionDays, slog.Default(), time.Now)",
		"autoTestRunner, requestLogRetentionRunner, responseAffinityRetentionRunner, os.DirFS(\"frontend/build\")",
		"server.Shutdown",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("main.go missing %q", want)
		}
	}
}

func TestMainWiresAlertDispatcherAfterDatabaseCommitNotifications(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("ReadFile main.go returned error: %v", err)
	}
	text := string(source)
	for _, want := range []string{
		"store.NewAlertingRepository",
		"alerting.NewService",
		"alerting.NewHTTPAdapter",
		"alerting.NewActionTester",
		"alerting.NewDispatcher",
		"cfg.AlertDeliveryEnabled",
		"systemEventRepo.Subscribe",
		"systemEventRepo.GetByID",
		"alertDispatcher.Start()",
		"build, alertDispatcher, alertingService, alertActionTester",
		"server.Shutdown",
		"alertDispatcher.Shutdown",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("main.go missing alert dispatcher wiring %q", want)
		}
	}
	if strings.Index(text, "server.Shutdown") > strings.Index(text, "alertDispatcher.Shutdown") {
		t.Fatal("alert dispatcher shutdown must follow HTTP shutdown")
	}
}
