package store

import (
	"testing"

	"github.com/KnowSky404/N2API/backend/internal/provider"
)

func TestProviderRepositoryImplementsInterface(t *testing.T) {
	var _ provider.Repository = (*ProviderRepository)(nil)
}
