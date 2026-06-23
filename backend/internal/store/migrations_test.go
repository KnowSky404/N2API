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

func TestOAuthAuthorizationMetadataMigrationIsEmbedded(t *testing.T) {
	sql, err := MigrationSQL("00005_oauth_authorization_metadata.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"ALTER TABLE oauth_states ADD COLUMN IF NOT EXISTS encrypted_code_verifier TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE oauth_states ADD COLUMN IF NOT EXISTS code_verifier_hash TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE oauth_states ADD COLUMN IF NOT EXISTS client_id TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS encrypted_id_token TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}'::jsonb",
		"oauth_accounts_metadata_gin_idx",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}

func TestCodexAccountPoolStateMigrationIsEmbedded(t *testing.T) {
	sql, err := MigrationSQL("00006_codex_account_pool_state.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active'",
		"ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS fingerprint_hash TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS failure_count INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS circuit_open_until TIMESTAMPTZ",
		"ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS rate_limited_until TIMESTAMPTZ",
		"ALTER TABLE oauth_states ADD COLUMN IF NOT EXISTS target_account_id BIGINT",
		"ALTER TABLE oauth_states ADD COLUMN IF NOT EXISTS pending_account_name TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE oauth_states ADD COLUMN IF NOT EXISTS fingerprint_hash TEXT NOT NULL DEFAULT ''",
		"oauth_accounts_schedulable_idx",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}

func TestOAuthAccountModelsMigrationIsEmbedded(t *testing.T) {
	sql, err := MigrationSQL("00007_oauth_account_models.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS oauth_account_models",
		"account_id BIGINT NOT NULL REFERENCES oauth_accounts(id) ON DELETE CASCADE",
		"source TEXT NOT NULL DEFAULT 'manual'",
		"UNIQUE (account_id, model)",
		"oauth_account_models_provider_model_enabled_idx",
		"oauth_account_models_account_enabled_idx",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}

func TestUnifiedProviderAccountsMigrationIsEmbedded(t *testing.T) {
	sql, err := MigrationSQL("00008_unified_provider_accounts.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS provider_accounts",
		"CREATE TABLE IF NOT EXISTS provider_account_credentials",
		"CREATE TABLE IF NOT EXISTS provider_account_models",
		"CREATE TABLE IF NOT EXISTS client_api_key_models",
		"ALTER TABLE client_api_keys ADD COLUMN IF NOT EXISTS model_policy",
		"INSERT INTO provider_accounts",
		"FROM oauth_accounts",
		"ON CONFLICT (id) DO NOTHING",
		"INSERT INTO provider_account_models",
		"FROM oauth_account_models",
		"provider_accounts_schedulable_idx",
		"provider_account_models_provider_model_enabled_idx",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}

func TestUnifiedProviderAccountMigrationCopiesOAuthData(t *testing.T) {
	sql, err := MigrationSQL("00008_unified_provider_accounts.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"id, provider, 'codex_oauth'",
		"encrypted_access_token, encrypted_refresh_token, encrypted_id_token",
		"FROM oauth_accounts",
		"SELECT id, account_id, provider, model, enabled",
		"FROM oauth_account_models",
		"client_api_keys ADD COLUMN IF NOT EXISTS model_policy TEXT NOT NULL DEFAULT 'all'",
		"setval(pg_get_serial_sequence('provider_accounts', 'id'), COALESCE((SELECT MAX(id) FROM provider_accounts), 1), (SELECT MAX(id) FROM provider_accounts) IS NOT NULL)",
		"setval(pg_get_serial_sequence('provider_account_models', 'id'), COALESCE((SELECT MAX(id) FROM provider_account_models), 1), (SELECT MAX(id) FROM provider_account_models) IS NOT NULL)",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration copy SQL missing %q", want)
		}
	}
}

func TestRequestLogProviderAccountAttributionMigrationIsEmbedded(t *testing.T) {
	sql, err := MigrationSQL("00009_request_log_provider_account_attribution.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS provider_account_id",
		"ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS provider_account_type",
		"request_logs_provider_account_created_at_idx",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}

func TestRequestLogModelAttributionMigrationIsEmbedded(t *testing.T) {
	sql, err := MigrationSQL("00010_request_log_model_attribution.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS model",
		"request_logs_model_created_at_idx",
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
	if len(sources) != 10 {
		t.Fatalf("migration sources = %d, want 10", len(sources))
	}
	if sources[0].Path != "00001_init.sql" || sources[9].Path != "00010_request_log_model_attribution.sql" {
		t.Fatalf("migration source paths = %+v", sources)
	}
}
