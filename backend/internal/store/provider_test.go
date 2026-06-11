package store

import (
	"os"
	"strings"
	"testing"

	"github.com/KnowSky404/N2API/backend/internal/provider"
)

func TestProviderRepositoryImplementsInterface(t *testing.T) {
	var _ provider.Repository = (*ProviderRepository)(nil)
}

func TestSaveAccountSubjectConflictPreservesSchedulingFields(t *testing.T) {
	source, err := os.ReadFile("provider.go")
	if err != nil {
		t.Fatalf("ReadFile provider.go returned error: %v", err)
	}
	sql := string(source)
	for _, forbidden := range []string{
		"enabled = EXCLUDED.enabled",
		"priority = EXCLUDED.priority",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("SaveAccount subject conflict must preserve scheduling field, found %q", forbidden)
		}
	}
}
