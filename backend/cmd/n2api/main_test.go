package main

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/KnowSky404/N2API/backend/internal/gateway"
	"github.com/KnowSky404/N2API/backend/internal/provider"
)

func TestGatewayAccountProviderReportsAccountFailures(t *testing.T) {
	var _ gateway.AccountFailureReporter = gatewayAccountProvider{}
	var _ gateway.AccountAuthorizationRefresher = gatewayAccountProvider{}
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
		"ProviderAccountAutoTestEnabled",
		"ProviderAccountAutoTestInterval",
		"ProviderAccountAutoTestIntervalSeconds",
		"go autoTestRunner.Run(ctx)",
		"go runAPIKeyCleanup(ctx, adminService, time.Hour)",
		"service.PurgeExpiredAPIKeys(ctx)",
		"autoTestRunner, requestLogRetentionRunner, os.DirFS(\"frontend/build\")",
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
