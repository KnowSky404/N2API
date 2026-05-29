package store

import (
	"testing"

	"github.com/KnowSky404/N2API/backend/internal/admin"
)

func TestAdminRepositoryImplementsInterface(t *testing.T) {
	var _ admin.Repository = (*AdminRepository)(nil)
}
