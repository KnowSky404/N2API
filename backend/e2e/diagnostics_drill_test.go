package e2e_test

import (
	"os"
	"strings"
	"testing"
)

func TestShouldPreserveFailureState(t *testing.T) {
	tests := []struct {
		name   string
		failed bool
		raw    string
		want   bool
	}{
		{name: "successful test with preservation enabled", failed: false, raw: "true", want: false},
		{name: "failed test without setting", failed: true, want: false},
		{name: "failed test with preservation disabled", failed: true, raw: "false", want: false},
		{name: "failed test with preservation enabled", failed: true, raw: "true", want: true},
		{name: "failed test with normalized preservation setting", failed: true, raw: " TRUE ", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldPreserveFailureState(tt.failed, tt.raw); got != tt.want {
				t.Fatalf("shouldPreserveFailureState(%t, %q) = %t, want %t", tt.failed, tt.raw, got, tt.want)
			}
		})
	}
}

func TestDiagnosticSanitizerDrill(t *testing.T) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("N2API_E2E_DIAGNOSTIC_DRILL")), "true") {
		t.Skip("diagnostic sanitizer drill is disabled")
	}

	secretCanary := requiredDiagnosticCanary(t, "N2API_E2E_DIAGNOSTIC_SECRET_CANARY")
	bodyCanary := requiredDiagnosticCanary(t, "N2API_E2E_DIAGNOSTIC_BODY_CANARY")

	t.Fatalf(
		"stage=diagnostic_drill authorization=Bearer %s cookie=session=%s body=%q",
		secretCanary,
		secretCanary,
		bodyCanary,
	)
}

func requiredDiagnosticCanary(t *testing.T, name string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		t.Fatalf("stage=diagnostic_drill missing=%s", name)
	}
	return value
}
