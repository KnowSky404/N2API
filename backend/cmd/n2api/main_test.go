package main

import (
	"testing"

	"github.com/KnowSky404/N2API/backend/internal/gateway"
)

func TestGatewayTokenProviderReportsAccountFailures(t *testing.T) {
	var _ gateway.AccountFailureReporter = gatewayTokenProvider{}
}
