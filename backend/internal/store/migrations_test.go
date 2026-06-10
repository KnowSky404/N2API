package store

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestInitialMigrationDefinesRequiredTables(t *testing.T) {
	sql, err := MigrationSQL("00001_init.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}

	for _, table := range []string{
		"admins",
		"oauth_accounts",
		"client_api_keys",
		"settings",
		"request_logs",
	} {
		if !strings.Contains(sql, "CREATE TABLE IF NOT EXISTS "+table) {
			t.Fatalf("initial migration missing table %s", table)
		}
	}
}

func TestInitialMigrationHasDownSection(t *testing.T) {
	sql, err := MigrationSQL("00001_init.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	if !strings.Contains(sql, "-- +goose Down") {
		t.Fatal("initial migration missing goose down section")
	}
}

func TestAdminSessionsMigrationIsEmbedded(t *testing.T) {
	sql, err := MigrationSQL("00002_admin_sessions.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS admin_sessions",
		"admin_id BIGINT NOT NULL REFERENCES admins(id) ON DELETE CASCADE",
		"token_hash TEXT NOT NULL",
		"CONSTRAINT admin_sessions_token_hash_idx UNIQUE (token_hash)",
		"admin_sessions_expires_at_idx",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
	if strings.Contains(sql, "CREATE INDEX IF NOT EXISTS admin_sessions_token_hash_idx") {
		t.Fatal("migration should not create a duplicate non-unique token_hash index")
	}
}

func TestOAuthStatesMigrationIsEmbedded(t *testing.T) {
	sql, err := MigrationSQL("00003_oauth_states.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS oauth_states",
		"provider TEXT NOT NULL",
		"state_hash TEXT NOT NULL UNIQUE",
		"redirect_after TEXT NOT NULL DEFAULT '/'",
		"oauth_states_state_hash_idx",
		"oauth_states_expires_at_idx",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}

func TestOAuthAccountPoolMigrationIsEmbedded(t *testing.T) {
	sql, err := MigrationSQL("00004_oauth_account_pool.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS enabled BOOLEAN NOT NULL DEFAULT true",
		"ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS priority INTEGER NOT NULL DEFAULT 100",
		"ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS last_used_at TIMESTAMPTZ",
		"ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS last_error TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS last_error_at TIMESTAMPTZ",
		"oauth_accounts_pool_order_idx",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}

func TestMigrationProviderSeesEmbeddedMigrations(t *testing.T) {
	migrations, err := migrationDirFS()
	if err != nil {
		t.Fatalf("migrationDirFS returned error: %v", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, &sql.DB{}, migrations)
	if err != nil {
		t.Fatalf("NewProvider returned error: %v", err)
	}
	sources := provider.ListSources()
	if len(sources) != 4 {
		t.Fatalf("migration sources = %d, want 4", len(sources))
	}
	if sources[0].Path != "00001_init.sql" || sources[3].Path != "00004_oauth_account_pool.sql" {
		t.Fatalf("migration source paths = %+v", sources)
	}
}
