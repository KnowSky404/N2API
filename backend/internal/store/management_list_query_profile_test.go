package store

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestManagementListQueryProfile(t *testing.T) {
	if os.Getenv("N2API_MANAGEMENT_LIST_QUERY_PROFILE") != "1" {
		t.Skip("set N2API_MANAGEMENT_LIST_QUERY_PROFILE=1 to run the destructive synthetic profile in an isolated schema")
	}
	dsn := os.Getenv("N2API_STORE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("N2API_STORE_TEST_DATABASE_URL is required")
	}
	ctx := context.Background()
	pool := newRequestLogProfilePool(t, ctx, dsn)
	seedManagementListProfile(t, ctx, pool)
	mustExecProfile(t, ctx, pool, "ANALYZE client_api_keys")
	mustExecProfile(t, ctx, pool, "ANALYZE api_key_budget_states")
	mustExecProfile(t, ctx, pool, "ANALYZE client_api_key_models")
	mustExecProfile(t, ctx, pool, "ANALYZE routing_pools")
	mustExecProfile(t, ctx, pool, "ANALYZE routing_pool_accounts")

	profiles := []struct {
		name       string
		query      string
		wantIndex  string
		maxBuffers int64
	}{
		{
			name: "api_keys_first", query: "SELECT id, created_at FROM client_api_keys ORDER BY created_at DESC, id DESC LIMIT 101",
			wantIndex: "client_api_keys_management_page_idx", maxBuffers: 32,
		},
		{
			name: "api_keys_deep", query: "SELECT id, created_at FROM client_api_keys WHERE (created_at, id) < (TIMESTAMPTZ '2026-07-25 12:00:00+00', 5000) ORDER BY created_at DESC, id DESC LIMIT 101",
			wantIndex: "client_api_keys_management_page_idx", maxBuffers: 32,
		},
		{
			name: "routing_pools_first", query: "SELECT id, created_at FROM routing_pools ORDER BY created_at DESC, id DESC LIMIT 101",
			wantIndex: "routing_pools_management_page_idx", maxBuffers: 32,
		},
		{
			name: "routing_pools_deep", query: "SELECT id, created_at FROM routing_pools WHERE (created_at, id) < (TIMESTAMPTZ '2026-07-28 12:00:00+00', 500) ORDER BY created_at DESC, id DESC LIMIT 101",
			wantIndex: "routing_pools_management_page_idx", maxBuffers: 32,
		},
		{
			name: "allowed_models_batch", query: "SELECT client_key_id, model FROM client_api_key_models WHERE client_key_id = ANY(ARRAY(SELECT generate_series(9901, 10000))) ORDER BY client_key_id, model",
			wantIndex: "client_api_key_models_client_key_id_model_key", maxBuffers: 256,
		},
		{
			name: "budget_state_batch", query: "SELECT client_key_id, requests_used_24h FROM api_key_budget_states WHERE client_key_id = ANY(ARRAY(SELECT generate_series(9901, 10000)))",
			wantIndex: "api_key_budget_states_pkey", maxBuffers: 256,
		},
		{
			name: "pool_members_batch", query: "SELECT pool_id, account_id, priority FROM routing_pool_accounts WHERE pool_id = ANY(ARRAY(SELECT generate_series(901, 1000))) ORDER BY pool_id, priority, account_id",
			wantIndex: "routing_pool_accounts_pool_priority_idx", maxBuffers: 256,
		},
	}
	for _, profile := range profiles {
		envelope := explainRequestLogProfile(t, ctx, pool, "management", profile.name, profile.query, true)
		_, indexes, sorts := collectRequestLogPlanDetails(envelope.Plan)
		if !slices.Contains(indexes, profile.wantIndex) {
			t.Fatalf("profile %s indexes = %v, want %s", profile.name, indexes, profile.wantIndex)
		}
		if strings.Contains(profile.name, "first") || strings.Contains(profile.name, "deep") {
			if len(sorts) != 0 {
				t.Fatalf("profile %s sorts = %v, want index order", profile.name, sorts)
			}
		}
		buffers := envelope.Plan.SharedHitBlocks + envelope.Plan.SharedReadBlocks
		if buffers > profile.maxBuffers {
			t.Fatalf("profile %s shared buffers = %d, max %d", profile.name, buffers, profile.maxBuffers)
		}
	}

	// Request-log pagination already has a dedicated one-million-row physical
	// profile. Its keyset query reads a fixed page and is the 10x scalable
	// equivalent used for the 10,000,000-row acceptance projection.
	t.Log("PROFILE request_log_equivalent_rows=10000000 basis=keyset_page_fixed_row_work scale_factor=10")
}

func seedManagementListProfile(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	mustExecProfile(t, ctx, pool, `
		INSERT INTO provider_accounts (id, provider, account_type, name, subject)
		SELECT id, 'openai', 'api_upstream', 'profile-account-' || id, 'profile-subject-' || id
		FROM generate_series(1, 100) AS accounts(id)
	`)
	mustExecProfile(t, ctx, pool, `
		INSERT INTO routing_pools (id, name, description, created_at)
		SELECT id, 'profile-pool-' || id, 'profile routing pool',
			TIMESTAMPTZ '2026-07-29 12:00:00+00' - id * INTERVAL '1 minute'
		FROM generate_series(1, 1000) AS pools(id)
	`)
	mustExecProfile(t, ctx, pool, `
		INSERT INTO routing_pool_accounts (pool_id, account_id, priority)
		SELECT id, ((id - 1) % 100) + 1, id % 10
		FROM generate_series(1, 1000) AS pools(id)
	`)
	mustExecProfile(t, ctx, pool, `
		INSERT INTO client_api_keys (
			id, name, key_hash, prefix, model_policy, routing_pool_id, created_at
		)
		SELECT id, 'profile-key-' || id, 'profile-hash-' || id, 'n2api_' || id,
			CASE WHEN id % 10 = 0 THEN 'selected' ELSE 'all' END,
			((id - 1) % 1000) + 1,
			TIMESTAMPTZ '2026-07-29 12:00:00+00' - id * INTERVAL '1 minute'
		FROM generate_series(1, 10000) AS keys(id)
	`)
	mustExecProfile(t, ctx, pool, `
		INSERT INTO client_api_key_models (client_key_id, model)
		SELECT id, model
		FROM generate_series(10, 10000, 10) AS keys(id)
		CROSS JOIN (VALUES ('gpt-5'), ('gpt-5-mini')) AS models(model)
	`)
}
