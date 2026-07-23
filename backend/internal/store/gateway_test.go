package store

import (
	"os"
	"strings"
	"testing"

	"github.com/KnowSky404/N2API/backend/internal/gateway"
	"github.com/KnowSky404/N2API/backend/internal/provider"
)

func TestGatewayRepositoryImplementsRequestLogger(t *testing.T) {
	var _ gateway.RequestLogger = (*GatewayRepository)(nil)
	var _ provider.AccountTestRequestLogger = (*GatewayRepository)(nil)
}

func TestCreateRequestLogSQLIncludesProviderAccountAttribution(t *testing.T) {
	sql := createRequestLogSQL()
	for _, want := range []string{"upstream_request_id", "provider_account_id", "provider_account_type", "provider_account_name", "routing_pool_fallback_depth", "routing_pool_fallback_chain", "routing_pool_error", "model", "session_id", "input_tokens", "total_tokens", "usage_source", "estimated_cost_microusd", "pricing_snapshot", "gateway_attempt_count", "gateway_fallback_count", "VALUES ($1, $2, $3", "$29, $30)"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("CreateRequestLog SQL missing %q: %s", want, sql)
		}
	}
	sourceBytes, err := os.ReadFile("gateway.go")
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	source := string(sourceBytes)
	for _, want := range []string{"entry.UpstreamRequestID", "entry.RoutingPoolFallbackDepth", "entry.RoutingPoolFallbackChain", "entry.RoutingPoolError", "entry.GatewayAttemptCount", "entry.GatewayFallbackCount"} {
		if !strings.Contains(source, want) {
			t.Fatalf("CreateRequestLog source missing %q", want)
		}
	}
}

func TestCreateRequestLogNullsMissingProviderAccountID(t *testing.T) {
	sourceBytes, err := os.ReadFile("gateway.go")
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	source := string(sourceBytes)
	if !strings.Contains(source, "nullInt64(entry.ProviderAccountID)") {
		t.Fatal("CreateRequestLog must store ProviderAccountID 0 as NULL so local gateway rejections can be logged without violating the provider account foreign key")
	}
	if !strings.Contains(source, "nullInt64(entry.ClientKeyID)") {
		t.Fatal("CreateRequestLog must store ClientKeyID 0 as NULL so provider tests can be logged without a downstream API key")
	}
}
