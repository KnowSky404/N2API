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
	for _, want := range []string{"provider_account_id", "provider_account_type", "model", "input_tokens", "total_tokens", "usage_source"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("CreateRequestLog SQL missing %q: %s", want, sql)
		}
	}
}
