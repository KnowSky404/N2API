package store

import (
	"strings"
	"testing"

	"github.com/KnowSky404/N2API/backend/internal/gateway"
)

func TestGatewayRepositoryImplementsRequestLogger(t *testing.T) {
	var _ gateway.RequestLogger = (*GatewayRepository)(nil)
}

func TestCreateRequestLogSQLIncludesProviderAccountAttribution(t *testing.T) {
	sql := createRequestLogSQL()
	for _, want := range []string{"provider_account_id", "provider_account_type", "provider_account_name", "model", "session_id", "input_tokens", "total_tokens", "usage_source", "estimated_cost_microusd", "pricing_snapshot"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("CreateRequestLog SQL missing %q: %s", want, sql)
		}
	}
}
