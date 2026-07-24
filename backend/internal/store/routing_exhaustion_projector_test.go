package store

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/KnowSky404/N2API/backend/internal/systemevent"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

func TestRoutingExhaustionProjectorMigrationBaselinesExistingLogs(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	key := createRoutingProjectorKey(t, repo, "migration-baseline")

	migrations, err := migrationDirFS()
	if err != nil {
		t.Fatalf("migrationDirFS returned error: %v", err)
	}
	db := stdlib.OpenDBFromPool(repo.pool)
	t.Cleanup(func() { _ = db.Close() })
	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		db,
		migrations,
		goose.WithTableName("schema_migrations"),
		goose.WithDisableGlobalRegistry(true),
	)
	if err != nil {
		t.Fatalf("create migration provider: %v", err)
	}
	for _, wantVersion := range []int64{48, 47, 46, 45} {
		result, err := provider.Down(ctx)
		if err != nil {
			t.Fatalf("roll back migration %d: %v", wantVersion, err)
		}
		if result == nil || result.Source.Version != wantVersion {
			t.Fatalf("migration down result = %+v, want version %d", result, wantVersion)
		}
	}

	historicalID := insertRoutingProjectorLog(t, repo, routingProjectorLog{
		Suffix:           "historical-exhaustion",
		ClientKeyID:      key.ID,
		StatusCode:       503,
		RoutingPoolError: "routing_pool_exhausted",
	})
	if err := RunMigrations(ctx, repo.pool); err != nil {
		t.Fatalf("reapply migrations: %v", err)
	}

	if got := routingProjectorCheckpoint(t, repo); got != historicalID {
		t.Fatalf("migration checkpoint = %d, want historical max ID %d", got, historicalID)
	}
	cycle, err := repo.RunRoutingExhaustionProjectorCycle(ctx, 1000, 100, time.Now().UTC())
	if err != nil {
		t.Fatalf("RunRoutingExhaustionProjectorCycle returned error: %v", err)
	}
	if cycle.Processed != 0 || cycle.Transitions != 0 || cycle.LastRequestLogID != historicalID || cycle.Contended {
		t.Fatalf("baseline cycle = %+v, want no replay at checkpoint %d", cycle, historicalID)
	}
	assertRoutingProjectorStateCount(t, repo, key.ID, 0)
	assertRoutingProjectorEventCounts(t, repo, key.ID, map[systemevent.Action]int{})
}

func TestAdminRepositoryRoutingExhaustionProjectorLifecycle(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	key := createRoutingProjectorKey(t, repo, "lifecycle")
	accountID := insertProviderAccount(t, repo.pool, "openai", "api_key", "routing-lifecycle-account")
	routingPool, err := repo.CreateRoutingPool(ctx, "routing-lifecycle-pool", "", true, nil)
	if err != nil {
		t.Fatalf("CreateRoutingPool returned error: %v", err)
	}
	seedRoutingProjectorCheckpoint(t, repo, 0)

	triggerID := insertRoutingProjectorLog(t, repo, routingProjectorLog{
		Suffix:           "lifecycle-trigger",
		ClientKeyID:      key.ID,
		StatusCode:       503,
		RoutingPoolID:    routingPool.ID,
		FallbackDepth:    2,
		RoutingPoolError: "routing_pool_exhausted",
	})
	cycle, err := repo.RunRoutingExhaustionProjectorCycle(ctx, 1000, 100, time.Now().UTC())
	if err != nil || cycle.Processed != 1 || cycle.Transitions != 1 || cycle.LastRequestLogID != triggerID || cycle.Contended {
		t.Fatalf("trigger cycle = %+v, err=%v", cycle, err)
	}
	assertRoutingProjectorStateCount(t, repo, key.ID, 1)

	repeatedID := insertRoutingProjectorLog(t, repo, routingProjectorLog{
		Suffix:           "lifecycle-repeated",
		ClientKeyID:      key.ID,
		StatusCode:       503,
		RoutingPoolID:    routingPool.ID,
		FallbackDepth:    3,
		RoutingPoolError: "routing_pool_exhausted",
	})
	cycle, err = repo.RunRoutingExhaustionProjectorCycle(ctx, 1000, 100, time.Now().UTC())
	if err != nil || cycle.Processed != 1 || cycle.Transitions != 0 || cycle.LastRequestLogID != repeatedID {
		t.Fatalf("repeated trigger cycle = %+v, err=%v", cycle, err)
	}

	recoveryID := insertRoutingProjectorLog(t, repo, routingProjectorLog{
		Suffix:            "lifecycle-recovery",
		ClientKeyID:       key.ID,
		ProviderAccountID: accountID,
		StatusCode:        204,
	})
	cycle, err = repo.RunRoutingExhaustionProjectorCycle(ctx, 1000, 100, time.Now().UTC())
	if err != nil || cycle.Processed != 1 || cycle.Transitions != 1 || cycle.LastRequestLogID != recoveryID {
		t.Fatalf("recovery cycle = %+v, err=%v", cycle, err)
	}
	assertRoutingProjectorStateCount(t, repo, key.ID, 0)
	assertRoutingProjectorEventCounts(t, repo, key.ID, map[systemevent.Action]int{
		systemevent.ActionAPIKeyRoutingPoolExhausted: 1,
		systemevent.ActionAPIKeyRoutingPoolRecovered: 1,
	})
	assertRoutingProjectorEventFields(t, repo, key, triggerID, recoveryID, accountID, routingPool.ID)
}

func TestAdminRepositoryRoutingExhaustionProjectorRequiresRealUpstreamRecovery(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	key := createRoutingProjectorKey(t, repo, "strict-recovery")
	accountID := insertProviderAccount(t, repo.pool, "openai", "api_key", "routing-strict-account")
	seedRoutingProjectorCheckpoint(t, repo, 0)

	insertRoutingProjectorLog(t, repo, routingProjectorLog{
		Suffix:           "strict-trigger",
		ClientKeyID:      key.ID,
		StatusCode:       503,
		RoutingPoolError: "routing_pool_exhausted",
	})
	if cycle, err := repo.RunRoutingExhaustionProjectorCycle(ctx, 1000, 100, time.Now().UTC()); err != nil || cycle.Transitions != 1 {
		t.Fatalf("trigger cycle = %+v, err=%v", cycle, err)
	}

	invalid := []routingProjectorLog{
		{Suffix: "local-models-200", ClientKeyID: key.ID, StatusCode: 200, Route: "/v1/models"},
		{Suffix: "success-with-routing-error", ClientKeyID: key.ID, ProviderAccountID: accountID, StatusCode: 200, RoutingPoolError: "routing_pool_unavailable"},
		{Suffix: "redirect-with-upstream", ClientKeyID: key.ID, ProviderAccountID: accountID, StatusCode: 300},
		{Suffix: "upstream-failure", ClientKeyID: key.ID, ProviderAccountID: accountID, StatusCode: 503},
		{Suffix: "local-budget-rejection", ClientKeyID: key.ID, StatusCode: 429, Error: "api_key_request_budget_exceeded"},
	}
	var invalidLastID int64
	for _, entry := range invalid {
		invalidLastID = insertRoutingProjectorLog(t, repo, entry)
	}
	cycle, err := repo.RunRoutingExhaustionProjectorCycle(ctx, 1000, 100, time.Now().UTC())
	if err != nil || cycle.Processed != len(invalid) || cycle.Transitions != 0 || cycle.LastRequestLogID != invalidLastID {
		t.Fatalf("invalid recovery cycle = %+v, err=%v", cycle, err)
	}
	assertRoutingProjectorStateCount(t, repo, key.ID, 1)

	validID := insertRoutingProjectorLog(t, repo, routingProjectorLog{
		Suffix:            "real-upstream-success",
		ClientKeyID:       key.ID,
		ProviderAccountID: accountID,
		StatusCode:        299,
	})
	cycle, err = repo.RunRoutingExhaustionProjectorCycle(ctx, 1000, 100, time.Now().UTC())
	if err != nil || cycle.Processed != 1 || cycle.Transitions != 1 || cycle.LastRequestLogID != validID {
		t.Fatalf("valid recovery cycle = %+v, err=%v", cycle, err)
	}
	assertRoutingProjectorStateCount(t, repo, key.ID, 0)
}

func TestAdminRepositoryRoutingExhaustionProjectorIsolatesAPIKeys(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	keyA := createRoutingProjectorKey(t, repo, "isolation-a")
	keyB := createRoutingProjectorKey(t, repo, "isolation-b")
	accountID := insertProviderAccount(t, repo.pool, "openai", "api_key", "routing-isolation-account")
	seedRoutingProjectorCheckpoint(t, repo, 0)

	insertRoutingProjectorLog(t, repo, routingProjectorLog{Suffix: "isolation-a-trigger", ClientKeyID: keyA.ID, StatusCode: 503, RoutingPoolError: "routing_pool_exhausted"})
	insertRoutingProjectorLog(t, repo, routingProjectorLog{Suffix: "isolation-b-unrelated-success", ClientKeyID: keyB.ID, ProviderAccountID: accountID, StatusCode: 200})
	if cycle, err := repo.RunRoutingExhaustionProjectorCycle(ctx, 1000, 100, time.Now().UTC()); err != nil || cycle.Transitions != 1 {
		t.Fatalf("first isolation cycle = %+v, err=%v", cycle, err)
	}
	assertRoutingProjectorStateCount(t, repo, keyA.ID, 1)
	assertRoutingProjectorStateCount(t, repo, keyB.ID, 0)

	insertRoutingProjectorLog(t, repo, routingProjectorLog{Suffix: "isolation-b-trigger", ClientKeyID: keyB.ID, StatusCode: 503, RoutingPoolError: "routing_pool_exhausted"})
	insertRoutingProjectorLog(t, repo, routingProjectorLog{Suffix: "isolation-a-recovery", ClientKeyID: keyA.ID, ProviderAccountID: accountID, StatusCode: 200})
	if cycle, err := repo.RunRoutingExhaustionProjectorCycle(ctx, 1000, 100, time.Now().UTC()); err != nil || cycle.Transitions != 2 {
		t.Fatalf("second isolation cycle = %+v, err=%v", cycle, err)
	}
	assertRoutingProjectorStateCount(t, repo, keyA.ID, 0)
	assertRoutingProjectorStateCount(t, repo, keyB.ID, 1)
	assertRoutingProjectorEventCounts(t, repo, keyA.ID, map[systemevent.Action]int{
		systemevent.ActionAPIKeyRoutingPoolExhausted: 1,
		systemevent.ActionAPIKeyRoutingPoolRecovered: 1,
	})
	assertRoutingProjectorEventCounts(t, repo, keyB.ID, map[systemevent.Action]int{
		systemevent.ActionAPIKeyRoutingPoolExhausted: 1,
	})
}

func TestAdminRepositoryRoutingExhaustionProjectorBoundsWork(t *testing.T) {
	t.Run("batch size", func(t *testing.T) {
		repo := newTestAdminRepository(t)
		ctx := context.Background()
		key := createRoutingProjectorKey(t, repo, "batch-bound")
		seedRoutingProjectorCheckpoint(t, repo, 0)
		ids := insertRoutingProjectorLogs(t, repo, key.ID, 1001, false)

		first, err := repo.RunRoutingExhaustionProjectorCycle(ctx, 1000, 100, time.Now().UTC())
		if err != nil || first.Processed != 1000 || first.Transitions != 0 || first.LastRequestLogID != ids[999] {
			t.Fatalf("first batch-bound cycle = %+v, err=%v", first, err)
		}
		second, err := repo.RunRoutingExhaustionProjectorCycle(ctx, 1000, 100, time.Now().UTC())
		if err != nil || second.Processed != 1 || second.Transitions != 0 || second.LastRequestLogID != ids[1000] {
			t.Fatalf("second batch-bound cycle = %+v, err=%v", second, err)
		}
	})

	t.Run("transition limit", func(t *testing.T) {
		repo := newTestAdminRepository(t)
		ctx := context.Background()
		seedRoutingProjectorCheckpoint(t, repo, 0)
		ids := insertRoutingProjectorLogs(t, repo, 0, 101, true)

		first, err := repo.RunRoutingExhaustionProjectorCycle(ctx, 1000, 100, time.Now().UTC())
		if err != nil || first.Processed != 100 || first.Transitions != 100 || first.LastRequestLogID != ids[99] {
			t.Fatalf("first transition-bound cycle = %+v, err=%v", first, err)
		}
		var states int
		if err := repo.pool.QueryRow(ctx, `SELECT count(*) FROM api_key_routing_exhaustion_states`).Scan(&states); err != nil {
			t.Fatalf("count bounded states: %v", err)
		}
		if states != 100 {
			t.Fatalf("bounded states = %d, want 100", states)
		}
		second, err := repo.RunRoutingExhaustionProjectorCycle(ctx, 1000, 100, time.Now().UTC())
		if err != nil || second.Processed != 1 || second.Transitions != 1 || second.LastRequestLogID != ids[100] {
			t.Fatalf("second transition-bound cycle = %+v, err=%v", second, err)
		}
	})
}

func TestAdminRepositoryRoutingExhaustionProjectorSkipsAdvisoryLockContention(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	key := createRoutingProjectorKey(t, repo, "lock-contention")
	seedRoutingProjectorCheckpoint(t, repo, 0)
	insertRoutingProjectorLog(t, repo, routingProjectorLog{Suffix: "contended-trigger", ClientKeyID: key.ID, StatusCode: 503, RoutingPoolError: "routing_pool_exhausted"})

	conn, err := repo.pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire lock connection: %v", err)
	}
	defer conn.Release()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin lock transaction: %v", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, routingExhaustionProjectorAdvisoryLockID); err != nil {
		t.Fatalf("acquire routing projector advisory lock: %v", err)
	}

	cycle, err := repo.RunRoutingExhaustionProjectorCycle(ctx, 1000, 100, time.Now().UTC())
	if err != nil {
		t.Fatalf("contended cycle returned error: %v", err)
	}
	if !cycle.Contended || cycle.Processed != 0 || cycle.Transitions != 0 || cycle.LastRequestLogID != 0 {
		t.Fatalf("contended cycle = %+v, want normal skip", cycle)
	}
	if got := routingProjectorCheckpoint(t, repo); got != 0 {
		t.Fatalf("checkpoint after contention = %d, want 0", got)
	}
	assertRoutingProjectorStateCount(t, repo, key.ID, 0)
}

func TestAdminRepositoryRoutingExhaustionProjectorDoesNotSkipLateCommittedLowerID(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	key := createRoutingProjectorKey(t, repo, "late-commit")
	accountID := insertProviderAccount(t, repo.pool, "openai", "api_key", "routing-late-commit-account")
	seedRoutingProjectorCheckpoint(t, repo, 0)

	lateTx, err := repo.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin late request-log transaction: %v", err)
	}
	defer lateTx.Rollback(ctx)
	var triggerID int64
	if err := lateTx.QueryRow(ctx, `
		INSERT INTO request_logs (
			request_id, client_key_id, provider, route, method, status_code,
			latency_ms, routing_pool_error, created_at
		) VALUES ('req_routing_late_trigger', $1, 'openai', '/v1/responses', 'POST', 503,
			12, 'routing_pool_exhausted', now())
		RETURNING id
	`, key.ID).Scan(&triggerID); err != nil {
		t.Fatalf("insert uncommitted lower-ID trigger: %v", err)
	}
	recoveryID := insertRoutingProjectorLog(t, repo, routingProjectorLog{
		Suffix:            "early-recovery",
		ClientKeyID:       key.ID,
		ProviderAccountID: accountID,
		StatusCode:        200,
	})
	if recoveryID <= triggerID {
		t.Fatalf("committed recovery ID = %d, want greater than uncommitted trigger ID %d", recoveryID, triggerID)
	}

	contended, err := repo.RunRoutingExhaustionProjectorCycle(ctx, 1000, 100, time.Now().UTC())
	if err != nil || !contended.Contended || contended.Processed != 0 || contended.Transitions != 0 {
		t.Fatalf("cycle with in-flight lower ID = %+v, err=%v", contended, err)
	}
	if got := routingProjectorCheckpoint(t, repo); got != 0 {
		t.Fatalf("checkpoint with in-flight lower ID = %d, want 0", got)
	}
	if err := lateTx.Commit(ctx); err != nil {
		t.Fatalf("commit lower-ID trigger: %v", err)
	}

	cycle, err := repo.RunRoutingExhaustionProjectorCycle(ctx, 1000, 100, time.Now().UTC())
	if err != nil || cycle.Contended || cycle.Processed != 2 || cycle.Transitions != 2 || cycle.LastRequestLogID != recoveryID {
		t.Fatalf("cycle after ordered commits = %+v, err=%v", cycle, err)
	}
	assertRoutingProjectorStateCount(t, repo, key.ID, 0)
	assertRoutingProjectorEventCounts(t, repo, key.ID, map[systemevent.Action]int{
		systemevent.ActionAPIKeyRoutingPoolExhausted: 1,
		systemevent.ActionAPIKeyRoutingPoolRecovered: 1,
	})
}

func TestAdminRepositoryRoutingExhaustionProjectorRollsBackCheckpointAndStateWhenEventInsertFails(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	key := createRoutingProjectorKey(t, repo, "event-rollback")
	seedRoutingProjectorCheckpoint(t, repo, 0)
	triggerID := insertRoutingProjectorLog(t, repo, routingProjectorLog{Suffix: "rollback-trigger", ClientKeyID: key.ID, StatusCode: 503, RoutingPoolError: "routing_pool_exhausted"})

	if _, err := repo.pool.Exec(ctx, `
		CREATE OR REPLACE FUNCTION reject_routing_exhaustion_event() RETURNS trigger AS $$
		BEGIN
			IF NEW.action = 'api_key.routing_pool.exhausted' THEN
				RAISE EXCEPTION 'reject routing exhaustion event';
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER reject_routing_exhaustion_event_trigger
		BEFORE INSERT ON system_events
		FOR EACH ROW EXECUTE FUNCTION reject_routing_exhaustion_event();
	`); err != nil {
		t.Fatalf("install event failure trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = repo.pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS reject_routing_exhaustion_event_trigger ON system_events`)
		_, _ = repo.pool.Exec(context.Background(), `DROP FUNCTION IF EXISTS reject_routing_exhaustion_event()`)
	})

	if _, err := repo.RunRoutingExhaustionProjectorCycle(ctx, 1000, 100, time.Now().UTC()); err == nil {
		t.Fatal("projector cycle returned nil error, want event insert failure")
	}
	if got := routingProjectorCheckpoint(t, repo); got != 0 {
		t.Fatalf("checkpoint after failed event = %d, want rollback to 0", got)
	}
	assertRoutingProjectorStateCount(t, repo, key.ID, 0)
	assertRoutingProjectorEventCounts(t, repo, key.ID, map[systemevent.Action]int{})

	if _, err := repo.pool.Exec(ctx, `DROP TRIGGER reject_routing_exhaustion_event_trigger ON system_events`); err != nil {
		t.Fatalf("drop event failure trigger: %v", err)
	}
	cycle, err := repo.RunRoutingExhaustionProjectorCycle(ctx, 1000, 100, time.Now().UTC())
	if err != nil || cycle.Processed != 1 || cycle.Transitions != 1 || cycle.LastRequestLogID != triggerID {
		t.Fatalf("retry cycle = %+v, err=%v", cycle, err)
	}
}

func TestAdminRepositoryRevokeAPIKeyRecoversRoutingExhaustion(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	key := createRoutingProjectorKey(t, repo, "revoke-recovery")
	seedRoutingProjectorCheckpoint(t, repo, 0)
	insertRoutingProjectorLog(t, repo, routingProjectorLog{Suffix: "revoke-trigger", ClientKeyID: key.ID, StatusCode: 503, RoutingPoolError: "routing_pool_exhausted"})
	if cycle, err := repo.RunRoutingExhaustionProjectorCycle(ctx, 1000, 100, time.Now().UTC()); err != nil || cycle.Transitions != 1 {
		t.Fatalf("trigger cycle = %+v, err=%v", cycle, err)
	}

	if _, err := repo.RevokeAPIKey(ctx, key.ID); err != nil {
		t.Fatalf("RevokeAPIKey returned error: %v", err)
	}
	assertRoutingProjectorStateCount(t, repo, key.ID, 0)
	assertRoutingProjectorEventCounts(t, repo, key.ID, map[systemevent.Action]int{
		systemevent.ActionAPIKeyRoutingPoolExhausted: 1,
		systemevent.ActionAPIKeyRoutingPoolRecovered: 1,
	})
	var confirmations int
	if err := repo.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM system_events
		WHERE target_type = 'client_api_key'
			AND target_id = $1
			AND action = $2
			AND metadata ->> 'confirmation' = 'key_revoked'
	`, strconv.FormatInt(key.ID, 10), systemevent.ActionAPIKeyRoutingPoolRecovered).Scan(&confirmations); err != nil {
		t.Fatalf("count revoke recovery confirmations: %v", err)
	}
	if confirmations != 1 {
		t.Fatalf("revoke recovery confirmations = %d, want 1", confirmations)
	}
}

type routingProjectorLog struct {
	Suffix            string
	ClientKeyID       int64
	ProviderAccountID int64
	RoutingPoolID     int64
	FallbackDepth     int
	RoutingPoolError  string
	Route             string
	Error             string
	StatusCode        int
}

func createRoutingProjectorKey(t *testing.T, repo *AdminRepository, name string) admin.APIKey {
	t.Helper()
	key, err := repo.CreateAPIKey(context.Background(), name, "routing-hash-"+name, "n2api_", "encrypted-"+name, nil)
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}
	return key
}

func seedRoutingProjectorCheckpoint(t *testing.T, repo *AdminRepository, lastID int64) {
	t.Helper()
	if _, err := repo.pool.Exec(context.Background(), `
		INSERT INTO request_log_projector_checkpoints (projector_key, last_request_log_id, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (projector_key) DO UPDATE
		SET last_request_log_id = EXCLUDED.last_request_log_id, updated_at = EXCLUDED.updated_at
	`, routingExhaustionProjectorKey, lastID); err != nil {
		t.Fatalf("seed routing projector checkpoint: %v", err)
	}
}

func routingProjectorCheckpoint(t *testing.T, repo *AdminRepository) int64 {
	t.Helper()
	var lastID int64
	if err := repo.pool.QueryRow(context.Background(), `
		SELECT last_request_log_id
		FROM request_log_projector_checkpoints
		WHERE projector_key = $1
	`, routingExhaustionProjectorKey).Scan(&lastID); err != nil {
		t.Fatalf("load routing projector checkpoint: %v", err)
	}
	return lastID
}

func insertRoutingProjectorLog(t *testing.T, repo *AdminRepository, entry routingProjectorLog) int64 {
	t.Helper()
	if entry.Route == "" {
		entry.Route = "/v1/responses"
	}
	var id int64
	err := repo.pool.QueryRow(context.Background(), `
		INSERT INTO request_logs (
			request_id, client_key_id, provider_account_id, routing_pool_id,
			routing_pool_fallback_depth, routing_pool_error, provider, route,
			method, status_code, latency_ms, error, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, 'openai', $7, 'POST', $8, 12, $9, now())
		RETURNING id
	`, "req_routing_"+entry.Suffix, nullableTestID(entry.ClientKeyID), nullableTestID(entry.ProviderAccountID), nullableTestID(entry.RoutingPoolID), entry.FallbackDepth, entry.RoutingPoolError, entry.Route, entry.StatusCode, entry.Error).Scan(&id)
	if err != nil {
		t.Fatalf("insert routing projector request log %q: %v", entry.Suffix, err)
	}
	return id
}

func insertRoutingProjectorLogs(t *testing.T, repo *AdminRepository, keyID int64, count int, exhausted bool) []int64 {
	t.Helper()
	ctx := context.Background()
	if exhausted {
		rows, err := repo.pool.Query(ctx, `
			WITH keys AS (
				INSERT INTO client_api_keys (name, key_hash, prefix, encrypted_secret)
				SELECT 'routing-transition-' || value, 'routing-transition-hash-' || value, 'n2api_', 'encrypted'
				FROM generate_series(1, $1) AS value
				RETURNING id
			), inserted AS (
				INSERT INTO request_logs (
					request_id, client_key_id, provider, route, method, status_code,
					latency_ms, routing_pool_error, created_at
				)
				SELECT 'req_routing_transition_' || id, id, 'openai', '/v1/responses', 'POST', 503,
					12, 'routing_pool_exhausted', now()
				FROM keys
				ORDER BY id
				RETURNING id
			)
			SELECT id FROM inserted ORDER BY id
		`, count)
		if err != nil {
			t.Fatalf("insert bounded transition logs: %v", err)
		}
		return collectRoutingProjectorLogIDs(t, rows, count)
	}

	rows, err := repo.pool.Query(ctx, `
		WITH inserted AS (
			INSERT INTO request_logs (
				request_id, client_key_id, provider, route, method, status_code, latency_ms, created_at
			)
			SELECT 'req_routing_batch_' || value, $1, 'openai', '/v1/responses', 'POST', 503, 12, now()
			FROM generate_series(1, $2) AS value
			ORDER BY value
			RETURNING id
		)
		SELECT id FROM inserted ORDER BY id
	`, keyID, count)
	if err != nil {
		t.Fatalf("insert bounded batch logs: %v", err)
	}
	return collectRoutingProjectorLogIDs(t, rows, count)
}

func collectRoutingProjectorLogIDs(t *testing.T, rows pgx.Rows, want int) []int64 {
	t.Helper()
	defer rows.Close()
	ids := make([]int64, 0, want)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan bounded request log ID: %v", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate bounded request log IDs: %v", err)
	}
	if len(ids) != want {
		t.Fatalf("bounded request log IDs = %d, want %d", len(ids), want)
	}
	return ids
}

func nullableTestID(value int64) any {
	if value == 0 {
		return nil
	}
	return value
}

func assertRoutingProjectorStateCount(t *testing.T, repo *AdminRepository, keyID int64, want int) {
	t.Helper()
	var count int
	if err := repo.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM api_key_routing_exhaustion_states WHERE client_key_id = $1
	`, keyID).Scan(&count); err != nil {
		t.Fatalf("count routing exhaustion states: %v", err)
	}
	if count != want {
		t.Fatalf("routing exhaustion states = %d, want %d", count, want)
	}
}

func assertRoutingProjectorEventCounts(t *testing.T, repo *AdminRepository, keyID int64, wants map[systemevent.Action]int) {
	t.Helper()
	rows, err := repo.pool.Query(context.Background(), `
		SELECT action, count(*)
		FROM system_events
		WHERE target_type = 'client_api_key'
			AND target_id = $1
			AND action IN ($2, $3)
		GROUP BY action
	`, strconv.FormatInt(keyID, 10), systemevent.ActionAPIKeyRoutingPoolExhausted, systemevent.ActionAPIKeyRoutingPoolRecovered)
	if err != nil {
		t.Fatalf("query routing projector event counts: %v", err)
	}
	defer rows.Close()
	got := map[systemevent.Action]int{}
	for rows.Next() {
		var action systemevent.Action
		var count int
		if err := rows.Scan(&action, &count); err != nil {
			t.Fatalf("scan routing projector event count: %v", err)
		}
		got[action] = count
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate routing projector event counts: %v", err)
	}
	if !reflect.DeepEqual(got, wants) {
		t.Fatalf("routing projector event counts = %+v, want %+v", got, wants)
	}
}

func assertRoutingProjectorEventFields(t *testing.T, repo *AdminRepository, key admin.APIKey, triggerID, recoveryID, accountID, routingPoolID int64) {
	t.Helper()
	type expected struct {
		severity systemevent.Severity
		outcome  systemevent.Outcome
		logID    int64
		metadata map[string]any
	}
	wants := map[systemevent.Action]expected{
		systemevent.ActionAPIKeyRoutingPoolExhausted: {
			severity: systemevent.SeverityError,
			outcome:  systemevent.OutcomeFailure,
			logID:    triggerID,
			metadata: map[string]any{
				"client_key_id":   float64(key.ID),
				"request_log_id":  float64(triggerID),
				"routing_pool_id": float64(routingPoolID),
				"fallback_depth":  float64(2),
			},
		},
		systemevent.ActionAPIKeyRoutingPoolRecovered: {
			severity: systemevent.SeverityInfo,
			outcome:  systemevent.OutcomeSuccess,
			logID:    recoveryID,
			metadata: map[string]any{
				"client_key_id":       float64(key.ID),
				"request_log_id":      float64(recoveryID),
				"provider_account_id": float64(accountID),
			},
		},
	}
	rows, err := repo.pool.Query(context.Background(), `
		SELECT e.action, e.category, e.severity, e.outcome, e.target_type, e.target_id,
			e.target_name, e.metadata, e.occurred_at, l.created_at
		FROM system_events e
		JOIN request_logs l ON l.id = (e.metadata ->> 'request_log_id')::bigint
		WHERE e.target_type = 'client_api_key'
			AND e.target_id = $1
			AND e.action IN ($2, $3)
	`, strconv.FormatInt(key.ID, 10), systemevent.ActionAPIKeyRoutingPoolExhausted, systemevent.ActionAPIKeyRoutingPoolRecovered)
	if err != nil {
		t.Fatalf("query routing projector event fields: %v", err)
	}
	defer rows.Close()
	seen := map[systemevent.Action]bool{}
	for rows.Next() {
		var action systemevent.Action
		var category systemevent.Category
		var severity systemevent.Severity
		var outcome systemevent.Outcome
		var targetType, targetID, targetName string
		var metadataBytes []byte
		var occurredAt, sourceCreatedAt time.Time
		if err := rows.Scan(&action, &category, &severity, &outcome, &targetType, &targetID, &targetName, &metadataBytes, &occurredAt, &sourceCreatedAt); err != nil {
			t.Fatalf("scan routing projector event fields: %v", err)
		}
		want, ok := wants[action]
		if !ok || category != systemevent.CategoryRuntime || severity != want.severity || outcome != want.outcome ||
			targetType != "client_api_key" || targetID != strconv.FormatInt(key.ID, 10) || targetName != key.Name {
			t.Fatalf("routing projector event = %s/%s/%s target=%s/%s/%s", action, category, severity, targetType, targetID, targetName)
		}
		if !occurredAt.Equal(sourceCreatedAt) {
			t.Fatalf("routing projector %s occurred_at = %s, want source log %d time %s", action, occurredAt, want.logID, sourceCreatedAt)
		}
		var metadata map[string]any
		if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
			t.Fatalf("decode routing projector metadata: %v", err)
		}
		if !reflect.DeepEqual(metadata, want.metadata) {
			t.Fatalf("routing projector %s metadata = %+v, want %+v", action, metadata, want.metadata)
		}
		seen[action] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate routing projector event fields: %v", err)
	}
	if len(seen) != len(wants) {
		t.Fatalf("routing projector event actions = %+v, want %+v", seen, wants)
	}
}
