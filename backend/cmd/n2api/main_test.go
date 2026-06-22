package main

import (
	"testing"

	"github.com/KnowSky404/N2API/backend/internal/gateway"
)

func TestGatewayAccountProviderReportsAccountFailures(t *testing.T) {
	var _ gateway.AccountFailureReporter = gatewayAccountProvider{}
}
