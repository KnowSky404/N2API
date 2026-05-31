package store

import (
	"testing"

	"github.com/KnowSky404/N2API/backend/internal/gateway"
)

func TestGatewayRepositoryImplementsRequestLogger(t *testing.T) {
	var _ gateway.RequestLogger = (*GatewayRepository)(nil)
}
