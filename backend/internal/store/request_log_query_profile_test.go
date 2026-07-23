package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const requestLogProfileRows = 1_000_000

func TestRequestLogQueryProfile(t *testing.T) {
	if os.Getenv("N2API_REQUEST_LOG_QUERY_PROFILE") != "1" {
		t.Skip("set N2API_REQUEST_LOG_QUERY_PROFILE=1 to run the destructive synthetic profile in an isolated schema")
	}
	dsn := os.Getenv("N2API_STORE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("N2API_STORE_TEST_DATABASE_URL is required")
	}

	ctx := context.Background()
	pool := newRequestLogProfilePool(t, ctx, dsn)
	dropRequestLogProfileSecondaryIndexes(t, ctx, pool)
	seedRequestLogProfile(t, ctx, pool, requestLogProfileRows)

	applyLegacyRequestLogIndexes(t, ctx, pool)
	mustExecProfile(t, ctx, pool, "ANALYZE request_logs")
	logRequestLogIndexSizes(t, ctx, pool, "legacy")
	legacyDuplicates := countRequestLogDuplicateIndexes(t, ctx, pool)
	if legacyDuplicates != 2 {
		t.Fatalf("legacy duplicate index groups = %d, want 2", legacyDuplicates)
	}
	runRequestLogQueryProfile(t, ctx, pool, "legacy")

	applyCandidateRequestLogIndexes(t, ctx, pool)
	mustExecProfile(t, ctx, pool, "ANALYZE request_logs")
	logRequestLogIndexSizes(t, ctx, pool, "candidate")
	if duplicates := countRequestLogDuplicateIndexes(t, ctx, pool); duplicates != 0 {
		t.Fatalf("candidate duplicate index groups = %d, want 0", duplicates)
	}
	runRequestLogQueryProfile(t, ctx, pool, "candidate")
	runRequestLogWriteProfile(t, ctx, pool)
}

func newRequestLogProfilePool(t *testing.T, ctx context.Context, dsn string) *pgxpool.Pool {
	t.Helper()
	adminPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect profile database: %v", err)
	}
	t.Cleanup(adminPool.Close)
	requireStoreTestDatabase(t, ctx, adminPool)

	schema := fmt.Sprintf("request_log_profile_%d", time.Now().UnixNano())
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create profile schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := adminPool.Exec(cleanupCtx, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("drop profile schema: %v", err)
		}
	})

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse profile database URL: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	config.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("connect isolated profile schema: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatalf("migrate isolated profile schema: %v", err)
	}
	return pool
}

func seedRequestLogProfile(t *testing.T, ctx context.Context, pool *pgxpool.Pool, rowCount int) {
	t.Helper()
	mustExecProfile(t, ctx, pool, `
		INSERT INTO client_api_keys (id, name, key_hash, prefix)
		SELECT id, 'profile-key-' || id, 'profile-hash-' || id, 'n2api_' || id
		FROM generate_series(1, 100) AS keys(id)
	`)
	mustExecProfile(t, ctx, pool, `
		INSERT INTO provider_accounts (id, provider, account_type, name, subject)
		SELECT id, 'openai', 'codex_oauth', 'profile-account-' || id, 'profile-subject-' || id
		FROM generate_series(1, 20) AS accounts(id)
	`)
	mustExecProfile(t, ctx, pool, `
		INSERT INTO routing_pools (id, name)
		SELECT id, 'profile-pool-' || id
		FROM generate_series(1, 5) AS pools(id)
	`)

	started := time.Now()
	if _, err := pool.Exec(ctx, `
		INSERT INTO request_logs (
			request_id, client_key_id, provider, route, method, status_code,
			latency_ms, error, created_at, provider_account_id,
			provider_account_type, provider_account_name, model, session_id,
			input_tokens, output_tokens, total_tokens, cached_input_tokens,
			reasoning_tokens, estimated_cost_microusd, usage_source,
			routing_pool_id, routing_pool_name, gateway_attempt_count,
			gateway_fallback_count
		)
		SELECT
			'profile-request-' || n,
			CASE WHEN n % 10 < 5 THEN 1 ELSE (n % 100) + 1 END,
			CASE WHEN n % 20 = 0 THEN 'openai-api' ELSE 'openai' END,
			'/v1/responses',
			'POST',
			CASE WHEN n % 20 = 0 THEN 500 ELSE 200 END,
			20 + (n % 800)::integer,
			CASE WHEN n % 20 = 0 THEN 'upstream_error' ELSE '' END,
			TIMESTAMPTZ '2026-07-21 12:00:00+00' - ((n - 1) / 8) * INTERVAL '1 minute',
			CASE WHEN n % 10 < 4 THEN 1 ELSE (n % 20) + 1 END,
			'codex_oauth',
			'profile-account-' || CASE WHEN n % 10 < 4 THEN 1 ELSE (n % 20) + 1 END,
			CASE WHEN n % 10 < 6 THEN 'gpt-5.4' ELSE 'gpt-' || (n % 10) END,
			'profile-session-' || (n % 1000),
			100 + (n % 400)::integer,
			20 + (n % 100)::integer,
			120 + (n % 500)::integer,
			(n % 80)::integer,
			(n % 30)::integer,
			1000 + (n % 5000),
			CASE WHEN n % 20 = 0 THEN 'missing' ELSE 'stream' END,
			CASE WHEN n % 10 < 6 THEN 1 ELSE (n % 5) + 1 END,
			'profile-pool-' || CASE WHEN n % 10 < 6 THEN 1 ELSE (n % 5) + 1 END,
			CASE WHEN n % 50 = 0 THEN 2 ELSE 1 END,
			CASE WHEN n % 50 = 0 THEN 1 ELSE 0 END
		FROM generate_series(1, $1::bigint) AS rows(n)
	`, rowCount); err != nil {
		t.Fatalf("seed %d request logs: %v", rowCount, err)
	}
	t.Logf("PROFILE seed rows=%d elapsed_ms=%.3f", rowCount, float64(time.Since(started).Microseconds())/1000)
}

func dropRequestLogProfileSecondaryIndexes(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT indexrelname
		FROM pg_stat_user_indexes
		WHERE schemaname = current_schema()
			AND relname = 'request_logs'
			AND indexrelname <> 'request_logs_pkey'
	`)
	if err != nil {
		t.Fatalf("list profile secondary indexes: %v", err)
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			t.Fatalf("scan profile secondary index: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatalf("iterate profile secondary indexes: %v", err)
	}
	rows.Close()
	for _, name := range names {
		mustExecProfile(t, ctx, pool, "DROP INDEX "+pgx.Identifier{name}.Sanitize())
	}
}

func applyLegacyRequestLogIndexes(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, statement := range []string{
		"DROP INDEX IF EXISTS request_logs_created_at_id_idx",
		"DROP INDEX IF EXISTS request_logs_client_key_created_at_id_idx",
		"CREATE INDEX IF NOT EXISTS request_logs_created_at_idx ON request_logs (created_at DESC)",
		"CREATE INDEX IF NOT EXISTS request_logs_provider_created_at_idx ON request_logs (provider, created_at DESC)",
		"CREATE INDEX IF NOT EXISTS request_logs_provider_account_created_at_idx ON request_logs (provider_account_id, created_at DESC)",
		"CREATE INDEX IF NOT EXISTS request_logs_provider_account_usage_idx ON request_logs (provider_account_id, created_at DESC)",
		"CREATE INDEX IF NOT EXISTS request_logs_model_created_at_idx ON request_logs (model, created_at DESC)",
		"CREATE INDEX IF NOT EXISTS request_logs_model_usage_idx ON request_logs (model, created_at DESC)",
		"CREATE INDEX IF NOT EXISTS request_logs_routing_pool_created_at_idx ON request_logs (routing_pool_id, created_at DESC)",
	} {
		mustExecProfile(t, ctx, pool, statement)
	}
}

func applyCandidateRequestLogIndexes(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, statement := range []string{
		"DROP INDEX IF EXISTS request_logs_provider_account_usage_idx",
		"DROP INDEX IF EXISTS request_logs_model_usage_idx",
		"DROP INDEX IF EXISTS request_logs_provider_created_at_idx",
		"CREATE INDEX request_logs_client_key_created_at_id_idx ON request_logs (client_key_id, created_at DESC, id DESC)",
	} {
		mustExecProfile(t, ctx, pool, statement)
	}
}

func runRequestLogQueryProfile(t *testing.T, ctx context.Context, pool *pgxpool.Pool, phase string) {
	t.Helper()
	deepCursor := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC).Add(-time.Duration((750_000-1)/8) * time.Minute)
	queries := []struct {
		name string
		sql  string
	}{
		{name: "default_page", sql: requestLogProfileListQuery("", 51)},
		{name: "deep_cursor", sql: requestLogProfileListQuery(fmt.Sprintf("WHERE (l.created_at, l.id) < (TIMESTAMPTZ '%s', 750000)", deepCursor.Format(time.RFC3339)), 51)},
		{name: "account_export", sql: requestLogProfileListQuery("WHERE l.provider_account_id = 1 AND l.created_at >= TIMESTAMPTZ '2026-06-21 12:00:00+00' AND l.created_at < TIMESTAMPTZ '2026-07-21 12:01:00+00'", 100001)},
		{name: "account_cold_export", sql: requestLogProfileListQuery("WHERE l.provider_account_id = 5 AND l.created_at >= TIMESTAMPTZ '2026-06-21 12:00:00+00' AND l.created_at < TIMESTAMPTZ '2026-07-21 12:01:00+00'", 100001)},
		{name: "model_export", sql: requestLogProfileListQuery("WHERE l.model = 'gpt-5.4' AND l.created_at >= TIMESTAMPTZ '2026-06-21 12:00:00+00' AND l.created_at < TIMESTAMPTZ '2026-07-21 12:01:00+00'", 100001)},
		{name: "model_cold_export", sql: requestLogProfileListQuery("WHERE l.model = 'gpt-6' AND l.created_at >= TIMESTAMPTZ '2026-06-21 12:00:00+00' AND l.created_at < TIMESTAMPTZ '2026-07-21 12:01:00+00'", 100001)},
		{name: "pool_export", sql: requestLogProfileListQuery("WHERE l.routing_pool_id = 1 AND l.created_at >= TIMESTAMPTZ '2026-06-21 12:00:00+00' AND l.created_at < TIMESTAMPTZ '2026-07-21 12:01:00+00'", 100001)},
		{name: "pool_cold_export", sql: requestLogProfileListQuery("WHERE l.routing_pool_id = 2 AND l.created_at >= TIMESTAMPTZ '2026-06-21 12:00:00+00' AND l.created_at < TIMESTAMPTZ '2026-07-21 12:01:00+00'", 100001)},
		{name: "client_key_page", sql: requestLogProfileListQuery("WHERE l.client_key_id = 6 AND l.created_at >= TIMESTAMPTZ '2026-06-21 12:00:00+00'", 201)},
		{name: "budget_current", sql: "SELECT count(*) FILTER (WHERE created_at >= TIMESTAMPTZ '2026-07-20 12:00:00+00'), sum(total_tokens) FILTER (WHERE created_at >= TIMESTAMPTZ '2026-06-21 12:00:00+00') FROM request_logs WHERE client_key_id = 1"},
		{name: "budget_bounded", sql: "SELECT count(*) FILTER (WHERE created_at >= TIMESTAMPTZ '2026-07-20 12:00:00+00'), sum(total_tokens) FILTER (WHERE created_at >= TIMESTAMPTZ '2026-06-21 12:00:00+00') FROM request_logs WHERE client_key_id = 1 AND created_at >= TIMESTAMPTZ '2026-06-21 12:00:00+00'"},
		{name: "budget_cold_current", sql: "SELECT count(*) FILTER (WHERE created_at >= TIMESTAMPTZ '2026-07-20 12:00:00+00'), sum(total_tokens) FILTER (WHERE created_at >= TIMESTAMPTZ '2026-06-21 12:00:00+00') FROM request_logs WHERE client_key_id = 6"},
		{name: "budget_cold_bounded", sql: "SELECT count(*) FILTER (WHERE created_at >= TIMESTAMPTZ '2026-07-20 12:00:00+00'), sum(total_tokens) FILTER (WHERE created_at >= TIMESTAMPTZ '2026-06-21 12:00:00+00') FROM request_logs WHERE client_key_id = 6 AND created_at >= TIMESTAMPTZ '2026-06-21 12:00:00+00'"},
		{name: "request_id_exact", sql: requestLogProfileListQuery("WHERE l.request_id = 'profile-request-750000'", 51)},
		{name: "session_exact", sql: requestLogProfileListQuery("WHERE l.session_id = 'profile-session-1'", 51)},
		{name: "usage_summary", sql: "SELECT model, count(*), sum(total_tokens) FROM request_logs WHERE created_at >= TIMESTAMPTZ '2026-07-14 12:00:00+00' GROUP BY model ORDER BY sum(estimated_cost_microusd) DESC"},
		{name: "retention_candidates", sql: "SELECT id FROM request_logs WHERE created_at < TIMESTAMPTZ '2026-05-01 12:00:00+00' ORDER BY created_at ASC, id ASC LIMIT 10000"},
	}
	for _, query := range queries {
		_ = explainRequestLogProfile(t, ctx, pool, phase, query.name, query.sql, false)
		explainRequestLogProfile(t, ctx, pool, phase, query.name, query.sql, true)
	}
}

func requestLogProfileListQuery(where string, limit int) string {
	return requestLogSelectSQL + where + fmt.Sprintf(" ORDER BY l.created_at DESC, l.id DESC LIMIT %d", limit)
}

func runRequestLogWriteProfile(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, statement := range []string{
		"CREATE UNLOGGED TABLE request_logs_write_legacy AS TABLE request_logs WITH NO DATA",
		"CREATE UNIQUE INDEX request_logs_write_legacy_pkey ON request_logs_write_legacy (id)",
		"CREATE INDEX request_logs_write_legacy_created_idx ON request_logs_write_legacy (created_at DESC)",
		"CREATE INDEX request_logs_write_legacy_provider_idx ON request_logs_write_legacy (provider, created_at DESC)",
		"CREATE INDEX request_logs_write_legacy_account_idx ON request_logs_write_legacy (provider_account_id, created_at DESC)",
		"CREATE INDEX request_logs_write_legacy_account_duplicate_idx ON request_logs_write_legacy (provider_account_id, created_at DESC)",
		"CREATE INDEX request_logs_write_legacy_model_idx ON request_logs_write_legacy (model, created_at DESC)",
		"CREATE INDEX request_logs_write_legacy_model_duplicate_idx ON request_logs_write_legacy (model, created_at DESC)",
		"CREATE INDEX request_logs_write_legacy_pool_idx ON request_logs_write_legacy (routing_pool_id, created_at DESC)",
		"CREATE UNLOGGED TABLE request_logs_write_candidate AS TABLE request_logs WITH NO DATA",
		"CREATE UNIQUE INDEX request_logs_write_candidate_pkey ON request_logs_write_candidate (id)",
		"CREATE INDEX request_logs_write_candidate_created_idx ON request_logs_write_candidate (created_at DESC)",
		"CREATE INDEX request_logs_write_candidate_account_idx ON request_logs_write_candidate (provider_account_id, created_at DESC)",
		"CREATE INDEX request_logs_write_candidate_model_idx ON request_logs_write_candidate (model, created_at DESC)",
		"CREATE INDEX request_logs_write_candidate_pool_idx ON request_logs_write_candidate (routing_pool_id, created_at DESC)",
		"CREATE INDEX request_logs_write_candidate_client_key_idx ON request_logs_write_candidate (client_key_id, created_at DESC, id DESC)",
	} {
		mustExecProfile(t, ctx, pool, statement)
	}
	explainRequestLogProfile(t, ctx, pool, "write", "legacy_100k", "INSERT INTO request_logs_write_legacy SELECT * FROM request_logs WHERE id <= 100000", true)
	explainRequestLogProfile(t, ctx, pool, "write", "candidate_100k", "INSERT INTO request_logs_write_candidate SELECT * FROM request_logs WHERE id <= 100000", true)
}

type requestLogExplainEnvelope struct {
	Plan          requestLogExplainPlan `json:"Plan"`
	PlanningTime  float64               `json:"Planning Time"`
	ExecutionTime float64               `json:"Execution Time"`
}

type requestLogExplainPlan struct {
	NodeType          string                  `json:"Node Type"`
	IndexName         string                  `json:"Index Name"`
	SortMethod        string                  `json:"Sort Method"`
	SharedHitBlocks   int64                   `json:"Shared Hit Blocks"`
	SharedReadBlocks  int64                   `json:"Shared Read Blocks"`
	TempReadBlocks    int64                   `json:"Temp Read Blocks"`
	TempWrittenBlocks int64                   `json:"Temp Written Blocks"`
	Plans             []requestLogExplainPlan `json:"Plans"`
}

func explainRequestLogProfile(t *testing.T, ctx context.Context, pool *pgxpool.Pool, phase, name, query string, logResult bool) requestLogExplainEnvelope {
	t.Helper()
	var raw []byte
	if err := pool.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query).Scan(&raw); err != nil {
		t.Fatalf("explain %s/%s: %v", phase, name, err)
	}
	var envelopes []requestLogExplainEnvelope
	if err := json.Unmarshal(raw, &envelopes); err != nil || len(envelopes) != 1 {
		t.Fatalf("decode explain %s/%s: envelopes=%d err=%v", phase, name, len(envelopes), err)
	}
	if logResult {
		nodes, indexes, sorts := collectRequestLogPlanDetails(envelopes[0].Plan)
		t.Logf("PROFILE phase=%s query=%s execution_ms=%.3f planning_ms=%.3f root=%s nodes=%s indexes=%s sorts=%s shared_hit=%d shared_read=%d temp_read=%d temp_written=%d",
			phase,
			name,
			envelopes[0].ExecutionTime,
			envelopes[0].PlanningTime,
			envelopes[0].Plan.NodeType,
			strings.Join(nodes, ","),
			strings.Join(indexes, ","),
			strings.Join(sorts, ","),
			envelopes[0].Plan.SharedHitBlocks,
			envelopes[0].Plan.SharedReadBlocks,
			envelopes[0].Plan.TempReadBlocks,
			envelopes[0].Plan.TempWrittenBlocks,
		)
	}
	return envelopes[0]
}

func collectRequestLogPlanDetails(plan requestLogExplainPlan) ([]string, []string, []string) {
	nodes := map[string]struct{}{}
	indexes := map[string]struct{}{}
	sorts := map[string]struct{}{}
	var visit func(requestLogExplainPlan)
	visit = func(node requestLogExplainPlan) {
		nodes[node.NodeType] = struct{}{}
		if node.IndexName != "" {
			indexes[node.IndexName] = struct{}{}
		}
		if node.SortMethod != "" {
			sorts[node.SortMethod] = struct{}{}
		}
		for _, child := range node.Plans {
			visit(child)
		}
	}
	visit(plan)
	nodeNames := make([]string, 0, len(nodes))
	for name := range nodes {
		nodeNames = append(nodeNames, name)
	}
	sort.Strings(nodeNames)
	indexNames := make([]string, 0, len(indexes))
	for name := range indexes {
		indexNames = append(indexNames, name)
	}
	sort.Strings(indexNames)
	sortMethods := make([]string, 0, len(sorts))
	for method := range sorts {
		sortMethods = append(sortMethods, method)
	}
	sort.Strings(sortMethods)
	return nodeNames, indexNames, sortMethods
}

func logRequestLogIndexSizes(t *testing.T, ctx context.Context, pool *pgxpool.Pool, phase string) {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT indexrelname, pg_relation_size(indexrelid)
		FROM pg_stat_user_indexes
		WHERE schemaname = current_schema() AND relname = 'request_logs'
		ORDER BY indexrelname
	`)
	if err != nil {
		t.Fatalf("query %s index sizes: %v", phase, err)
	}
	defer rows.Close()
	var total int64
	for rows.Next() {
		var name string
		var size int64
		if err := rows.Scan(&name, &size); err != nil {
			t.Fatalf("scan %s index size: %v", phase, err)
		}
		total += size
		t.Logf("PROFILE phase=%s index=%s bytes=%d", phase, name, size)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate %s index sizes: %v", phase, err)
	}
	t.Logf("PROFILE phase=%s index_total_bytes=%d", phase, total)
}

func countRequestLogDuplicateIndexes(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM (
			SELECT indkey, indclass, indcollation, indoption, indexprs, indpred
			FROM pg_index
			WHERE indrelid = 'request_logs'::regclass
			GROUP BY indkey, indclass, indcollation, indoption, indexprs, indpred
			HAVING count(*) > 1
		) AS duplicate_definitions
	`).Scan(&count); err != nil {
		t.Fatalf("count duplicate request log indexes: %v", err)
	}
	return count
}

func mustExecProfile(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql); err != nil {
		t.Fatalf("profile SQL failed: %v\n%s", err, sql)
	}
}
