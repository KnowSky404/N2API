package main

import (
	"os"
	"strings"
	"testing"

	"github.com/KnowSky404/N2API/backend/internal/gateway"
)

func TestGatewayAccountProviderReportsAccountFailures(t *testing.T) {
	var _ gateway.AccountFailureReporter = gatewayAccountProvider{}
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
		"ProviderAccountAutoTestEnabled",
		"ProviderAccountAutoTestInterval",
		"ProviderAccountAutoTestIntervalSeconds",
		"go autoTestRunner.Run(ctx)",
		"autoTestRunner, os.DirFS(\"frontend/build\")",
		"server.Shutdown",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("main.go missing %q", want)
		}
	}
}
