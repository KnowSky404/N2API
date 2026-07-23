package store

import (
	"context"
	"os"
	"strings"
	"testing"
	"unicode"

	"github.com/jackc/pgx/v5/pgxpool"
)

const storeTestDestructiveOptIn = "N2API_STORE_TEST_ALLOW_DESTRUCTIVE"

func requireStoreTestDatabase(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	if os.Getenv(storeTestDestructiveOptIn) != "1" {
		t.Fatalf("refusing PostgreSQL store test: set %s=1 only for an isolated test database", storeTestDestructiveOptIn)
	}

	var databaseName string
	if err := pool.QueryRow(ctx, `SELECT current_database()`).Scan(&databaseName); err != nil {
		t.Fatalf("identify PostgreSQL store test database: %v", err)
	}
	if !isIsolatedStoreTestDatabaseName(databaseName) {
		t.Fatalf("refusing PostgreSQL store test against database %q: name must contain a test, e2e, or restore segment", databaseName)
	}
}

func isIsolatedStoreTestDatabaseName(name string) bool {
	segments := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(name)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, segment := range segments {
		switch segment {
		case "test", "e2e", "restore":
			return true
		}
	}
	return false
}

func TestIsIsolatedStoreTestDatabaseName(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		want bool
	}{
		{name: "n2api_store_test", want: true},
		{name: "n2api-e2e-123", want: true},
		{name: "n2api_restore_fixture", want: true},
		{name: " N2API_TEST_42 ", want: true},
		{name: "n2api"},
		{name: "postgres"},
		{name: "template1"},
		{name: "n2api_contest"},
		{name: "n2api_tested"},
		{name: "production"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := isIsolatedStoreTestDatabaseName(test.name); got != test.want {
				t.Fatalf("isIsolatedStoreTestDatabaseName(%q) = %v, want %v", test.name, got, test.want)
			}
		})
	}
}
