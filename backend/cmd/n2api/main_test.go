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
	if !strings.Contains(string(source), "DisplayName:        selected.DisplayName") {
		t.Fatal("gatewayAccountProvider must map provider selected account display name to gateway selected account")
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
		"provider.NewAutoTestRunner",
		"ProviderAccountAutoTestEnabled",
		"ProviderAccountAutoTestInterval",
		"go autoTestRunner.Run(ctx)",
		"server.Shutdown",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("main.go missing %q", want)
		}
	}
}
