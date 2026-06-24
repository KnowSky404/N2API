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

func TestRequestLogProviderAccountNameMigrationIsEmbedded(t *testing.T) {
	sql, err := MigrationSQL("00012_request_log_provider_account_name.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS provider_account_name",
		"ALTER TABLE request_logs DROP COLUMN IF EXISTS provider_account_name",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}

func TestRequestLogSessionIDMigrationIsEmbedded(t *testing.T) {
	sql, err := MigrationSQL("00013_request_log_session_id.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS session_id",
		"ALTER TABLE request_logs DROP COLUMN IF EXISTS session_id",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}

func TestClientAPIKeyLimitsMigrationIsEmbedded(t *testing.T) {
	sql, err := MigrationSQL("00014_client_api_key_limits.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"ALTER TABLE client_api_keys ADD COLUMN IF NOT EXISTS requests_per_minute INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE client_api_keys ADD COLUMN IF NOT EXISTS tokens_per_minute INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE client_api_keys DROP COLUMN IF EXISTS tokens_per_minute",
		"ALTER TABLE client_api_keys DROP COLUMN IF EXISTS requests_per_minute",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}

func TestSingleAccountModelBackfillMigrationIsEmbedded(t *testing.T) {
	sql, err := MigrationSQL("00015_single_account_model_backfill.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"provider_accounts",
		"provider_account_models",
		"settings",
		"key = 'model_settings'",
		"value->'allowedModels'",
		"HAVING COUNT(*) = 1",
		"NOT EXISTS",
		"backfilled_from",
		"single_account_model_backfill",
		"ON CONFLICT (account_id, model) DO NOTHING",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}

func TestSingleAccountModelBackfillMigrationIsConservative(t *testing.T) {
	sql, err := MigrationSQL("00015_single_account_model_backfill.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"GROUP BY provider",
		"HAVING COUNT(*) = 1",
		"WHERE existing.account_id = single_provider_accounts.account_id",
		"WHERE allowed_models.model <> ''",
		"DELETE FROM provider_account_models",
		"metadata->>'backfilled_from' = 'single_account_model_backfill'",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration conservative guard missing %q", want)
		}
	}
}

func TestProviderAccountLoadFactorMigrationIsEmbedded(t *testing.T) {
	sql, err := MigrationSQL("00016_provider_account_load_factor.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"ALTER TABLE provider_accounts ADD COLUMN IF NOT EXISTS load_factor INTEGER NOT NULL DEFAULT 1",
		"provider_accounts_load_factor_positive",
		"load_factor BETWEEN 1 AND 100",
		"provider_accounts_schedulable_idx",
		"priority, load_factor DESC, last_used_at, id",
		"ALTER TABLE provider_accounts DROP COLUMN IF EXISTS load_factor",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}

func TestProviderAccountMaxConcurrentRequestsMigrationIsEmbedded(t *testing.T) {
	sql, err := MigrationSQL("00019_provider_account_max_concurrent_requests.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"max_concurrent_requests INTEGER NOT NULL DEFAULT 0",
		"provider_accounts_max_concurrent_requests_non_negative",
		"CHECK (max_concurrent_requests >= 0)",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}

func TestProviderSessionBindingsMigrationIsEmbedded(t *testing.T) {
	sql, err := MigrationSQL("00020_provider_session_bindings.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS provider_session_bindings",
		"provider_session_bindings_provider_model_session_unique",
		"UNIQUE (provider, model, session_id)",
		"REFERENCES provider_accounts(id) ON DELETE CASCADE",
		"provider_session_bindings_provider_account_idx",
		"DROP TABLE IF EXISTS provider_session_bindings",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}

func TestProviderAccountTestResultsMigrationIsEmbedded(t *testing.T) {
	sql, err := MigrationSQL("00017_provider_account_test_results.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"ALTER TABLE provider_accounts ADD COLUMN IF NOT EXISTS last_test_at TIMESTAMPTZ",
		"ADD COLUMN IF NOT EXISTS last_test_status TEXT NOT NULL DEFAULT ''",
		"ADD COLUMN IF NOT EXISTS last_test_error TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE provider_accounts DROP COLUMN IF EXISTS last_test_error",
		"ALTER TABLE provider_accounts DROP COLUMN IF EXISTS last_test_status",
		"ALTER TABLE provider_accounts DROP COLUMN IF EXISTS last_test_at",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}

func TestProviderAccountTestResultHistoryMigrationIsEmbedded(t *testing.T) {
	sql, err := MigrationSQL("00018_provider_account_test_result_history.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS provider_account_test_results",
		"account_id BIGINT NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE",
		"provider_account_test_results_account_idx",
		"provider_account_test_results_provider_idx",
		"DROP TABLE IF EXISTS provider_account_test_results",
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

func TestRequestUsageAccountingMigrationIsEmbedded(t *testing.T) {
	sql, err := MigrationSQL("00011_request_usage_accounting.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS input_tokens INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS output_tokens INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS total_tokens INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS cached_input_tokens INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS reasoning_tokens INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS estimated_cost_microusd BIGINT NOT NULL DEFAULT 0",
		"ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS pricing_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb",
		"ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS usage_source TEXT NOT NULL DEFAULT 'missing'",
		"request_logs_provider_account_usage_idx",
		"request_logs_model_usage_idx",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}

func TestRequestLogFallbackDiagnosticsMigrationIsEmbedded(t *testing.T) {
	sql, err := MigrationSQL("00021_request_log_fallback_diagnostics.sql")
	if err != nil {
		t.Fatalf("MigrationSQL returned error: %v", err)
	}
	for _, want := range []string{
		"ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS gateway_attempt_count INTEGER NOT NULL DEFAULT 0",
		"ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS gateway_fallback_count INTEGER NOT NULL DEFAULT 0",
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
	if len(sources) != 20 {
		t.Fatalf("migration sources = %d, want 20", len(sources))
	}
	if sources[0].Path != "00001_init.sql" || sources[19].Path != "00020_provider_session_bindings.sql" {
		t.Fatalf("migration source paths = %+v", sources)
	}
}
