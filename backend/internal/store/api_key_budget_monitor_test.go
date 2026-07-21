package store

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

func TestAdminRepositoryAPIKeyBudgetMonitorCrossingLifecycle(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	key := createBudgetMonitorKey(t, repo, "lifecycle")
	if _, err := repo.UpdateAPIKeyBudgets(ctx, key.ID, 10, 100, 1000, 10, 100, 1000); err != nil {
		t.Fatalf("UpdateAPIKeyBudgets returned error: %v", err)
	}
	for index := range 8 {
		insertRequestLog(t, repo.pool, key.ID, now.Add(-time.Hour+time.Duration(index)*time.Nanosecond), 200, 10, 100)
	}

	result, err := repo.RunAPIKeyBudgetMonitorCycle(ctx, 0, 100, now)
	if err != nil || result.Processed != 1 || result.Transitions != 6 || result.NextAfterID != 0 {
		t.Fatalf("80 percent cycle = %+v, err=%v", result, err)
	}
	assertBudgetThresholdStateCount(t, repo, key.ID, 6)
	assertBudgetEventCounts(t, repo, key.ID, map[systemevent.Action]int{
		systemevent.ActionAPIKeyBudgetThreshold80Crossed: 6,
	})

	result, err = repo.RunAPIKeyBudgetMonitorCycle(ctx, 0, 100, now)
	if err != nil || result.Transitions != 0 {
		t.Fatalf("unchanged cycle = %+v, err=%v", result, err)
	}
	assertBudgetEventCounts(t, repo, key.ID, map[systemevent.Action]int{
		systemevent.ActionAPIKeyBudgetThreshold80Crossed: 6,
	})

	for index := range 2 {
		insertRequestLog(t, repo.pool, key.ID, now.Add(-30*time.Minute+time.Duration(index)*time.Nanosecond), 200, 10, 100)
	}
	result, err = repo.RunAPIKeyBudgetMonitorCycle(ctx, 0, 100, now)
	if err != nil || result.Transitions != 6 {
		t.Fatalf("100 percent cycle = %+v, err=%v", result, err)
	}
	assertBudgetThresholdStateCount(t, repo, key.ID, 12)

	if _, err := repo.pool.Exec(ctx, `DELETE FROM request_logs WHERE client_key_id = $1 AND created_at >= $2`, key.ID, now.Add(-45*time.Minute)); err != nil {
		t.Fatalf("delete newest request logs: %v", err)
	}
	result, err = repo.RunAPIKeyBudgetMonitorCycle(ctx, 0, 100, now)
	if err != nil || result.Transitions != 6 {
		t.Fatalf("100 percent recovery cycle = %+v, err=%v", result, err)
	}
	assertBudgetThresholdStateCount(t, repo, key.ID, 6)

	if _, err := repo.pool.Exec(ctx, `
		DELETE FROM request_logs
		WHERE id = (
			SELECT id FROM request_logs WHERE client_key_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1
		)
	`, key.ID); err != nil {
		t.Fatalf("delete request log below 80 percent: %v", err)
	}
	result, err = repo.RunAPIKeyBudgetMonitorCycle(ctx, 0, 100, now)
	if err != nil || result.Transitions != 6 {
		t.Fatalf("80 percent recovery cycle = %+v, err=%v", result, err)
	}
	assertBudgetThresholdStateCount(t, repo, key.ID, 0)
	assertBudgetEventCounts(t, repo, key.ID, map[systemevent.Action]int{
		systemevent.ActionAPIKeyBudgetThreshold80Crossed:    6,
		systemevent.ActionAPIKeyBudgetThreshold100Crossed:   6,
		systemevent.ActionAPIKeyBudgetThreshold100Recovered: 6,
		systemevent.ActionAPIKeyBudgetThreshold80Recovered:  6,
	})
	assertBudgetEventsAreSafeAndDistinct(t, repo, key.ID)
}

func TestAdminRepositoryAPIKeyBudgetMonitorDirectJumpAndIntegerBoundary(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	key := createBudgetMonitorKey(t, repo, "integer-boundary")
	if _, err := repo.UpdateAPIKeyBudgets(ctx, key.ID, 3, 0, 0, 0, 0, 0); err != nil {
		t.Fatalf("UpdateAPIKeyBudgets returned error: %v", err)
	}
	for index := range 2 {
		insertRequestLog(t, repo.pool, key.ID, now.Add(-time.Hour+time.Duration(index)*time.Nanosecond), 200, 0, 0)
	}
	result, err := repo.RunAPIKeyBudgetMonitorCycle(ctx, 0, 100, now)
	if err != nil || result.Transitions != 0 {
		t.Fatalf("below ceil boundary cycle = %+v, err=%v", result, err)
	}
	insertRequestLog(t, repo.pool, key.ID, now.Add(-30*time.Minute), 200, 0, 0)
	result, err = repo.RunAPIKeyBudgetMonitorCycle(ctx, 0, 100, now)
	if err != nil || result.Transitions != 2 {
		t.Fatalf("direct 100 percent cycle = %+v, err=%v", result, err)
	}
	assertBudgetEventCounts(t, repo, key.ID, map[systemevent.Action]int{
		systemevent.ActionAPIKeyBudgetThreshold80Crossed:  1,
		systemevent.ActionAPIKeyBudgetThreshold100Crossed: 1,
	})
}

func TestAdminRepositoryAPIKeyBudgetMonitorZeroBudgetRecoversWithExactEvents(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	key := createBudgetMonitorKey(t, repo, "zero-budget")
	if _, err := repo.UpdateAPIKeyBudgets(ctx, key.ID, 1, 0, 0, 0, 0, 0); err != nil {
		t.Fatalf("UpdateAPIKeyBudgets returned error: %v", err)
	}
	insertRequestLog(t, repo.pool, key.ID, now.Add(-time.Hour), 200, 0, 0)
	result, err := repo.RunAPIKeyBudgetMonitorCycle(ctx, 0, 100, now)
	if err != nil || result.Transitions != 2 {
		t.Fatalf("crossing cycle = %+v, err=%v", result, err)
	}
	if _, err := repo.UpdateAPIKeyBudgets(ctx, key.ID, 0, 0, 0, 0, 0, 0); err != nil {
		t.Fatalf("clear API key budgets: %v", err)
	}
	result, err = repo.RunAPIKeyBudgetMonitorCycle(ctx, 0, 100, now)
	if err != nil || result.Processed != 1 || result.Transitions != 2 {
		t.Fatalf("zero-budget recovery cycle = %+v, err=%v", result, err)
	}
	assertBudgetThresholdStateCount(t, repo, key.ID, 0)

	type expectedFields struct {
		severity systemevent.Severity
		outcome  systemevent.Outcome
	}
	wants := map[systemevent.Action]expectedFields{
		systemevent.ActionAPIKeyBudgetThreshold80Crossed:    {severity: systemevent.SeverityWarning, outcome: systemevent.OutcomePartial},
		systemevent.ActionAPIKeyBudgetThreshold100Crossed:   {severity: systemevent.SeverityError, outcome: systemevent.OutcomeFailure},
		systemevent.ActionAPIKeyBudgetThreshold80Recovered:  {severity: systemevent.SeverityInfo, outcome: systemevent.OutcomeSuccess},
		systemevent.ActionAPIKeyBudgetThreshold100Recovered: {severity: systemevent.SeverityInfo, outcome: systemevent.OutcomeSuccess},
	}
	rows, err := repo.pool.Query(ctx, `
		SELECT action, category, severity, outcome, target_type, target_id, target_name
		FROM system_events
		WHERE target_type = 'client_api_key_budget' AND target_id LIKE $1
	`, fmtBudgetTargetPrefix(key.ID)+"%")
	if err != nil {
		t.Fatalf("query exact budget events: %v", err)
	}
	defer rows.Close()
	seen := map[systemevent.Action]int{}
	for rows.Next() {
		var action systemevent.Action
		var category systemevent.Category
		var severity systemevent.Severity
		var outcome systemevent.Outcome
		var targetType, targetID, targetName string
		if err := rows.Scan(&action, &category, &severity, &outcome, &targetType, &targetID, &targetName); err != nil {
			t.Fatalf("scan exact budget event: %v", err)
		}
		want, ok := wants[action]
		if !ok || category != systemevent.CategoryRuntime || severity != want.severity || outcome != want.outcome ||
			targetType != "client_api_key_budget" || targetID != fmtBudgetTargetPrefix(key.ID)+"request:24h" || targetName != key.Name {
			t.Fatalf("budget event fields = action=%s category=%s severity=%s outcome=%s target=%s/%s/%s", action, category, severity, outcome, targetType, targetID, targetName)
		}
		seen[action]++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate exact budget events: %v", err)
	}
	if len(seen) != len(wants) {
		t.Fatalf("exact budget event actions = %+v, want %+v", seen, wants)
	}
}

func TestAdminRepositoryAPIKeyBudgetMonitorBoundsCursorAndSerializes(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	keys := make([]admin.APIKey, 3)
	for index := range keys {
		keys[index] = createBudgetMonitorKey(t, repo, "cursor-"+string(rune('a'+index)))
		if _, err := repo.UpdateAPIKeyBudgets(ctx, keys[index].ID, 10, 0, 0, 0, 0, 0); err != nil {
			t.Fatalf("UpdateAPIKeyBudgets returned error: %v", err)
		}
	}
	first, err := repo.RunAPIKeyBudgetMonitorCycle(ctx, 0, 2, now)
	if err != nil || first.Processed != 2 || first.NextAfterID != keys[1].ID {
		t.Fatalf("first bounded cycle = %+v, err=%v", first, err)
	}
	second, err := repo.RunAPIKeyBudgetMonitorCycle(ctx, first.NextAfterID, 2, now)
	if err != nil || second.Processed != 1 || second.NextAfterID != 0 {
		t.Fatalf("second bounded cycle = %+v, err=%v", second, err)
	}

	key := keys[0]
	for index := range 8 {
		insertRequestLog(t, repo.pool, key.ID, now.Add(-time.Hour+time.Duration(index)*time.Nanosecond), 200, 0, 0)
	}
	var wg sync.WaitGroup
	results := make(chan admin.APIKeyBudgetMonitorCycleResult, 2)
	errors := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, runErr := repo.RunAPIKeyBudgetMonitorCycle(ctx, 0, 100, now)
			results <- result
			errors <- runErr
		}()
	}
	wg.Wait()
	close(results)
	close(errors)
	transitions := 0
	for runErr := range errors {
		if runErr != nil {
			t.Fatalf("concurrent cycle returned error: %v", runErr)
		}
	}
	for result := range results {
		transitions += result.Transitions
	}
	if transitions != 1 {
		t.Fatalf("concurrent transitions = %d, want one request-budget crossing", transitions)
	}
	assertBudgetEventCounts(t, repo, key.ID, map[systemevent.Action]int{
		systemevent.ActionAPIKeyBudgetThreshold80Crossed: 1,
	})
}

func TestAdminRepositoryAPIKeyBudgetMonitorRollsBackStateWhenEventInsertFails(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	key := createBudgetMonitorKey(t, repo, "rollback")
	if _, err := repo.UpdateAPIKeyBudgets(ctx, key.ID, 1, 0, 0, 0, 0, 0); err != nil {
		t.Fatalf("UpdateAPIKeyBudgets returned error: %v", err)
	}
	insertRequestLog(t, repo.pool, key.ID, now.Add(-time.Hour), 200, 0, 0)
	if _, err := repo.pool.Exec(ctx, `
		CREATE OR REPLACE FUNCTION reject_budget_threshold_event() RETURNS trigger AS $$
		BEGIN
			IF NEW.action LIKE 'api_key.budget.threshold_%' THEN
				RAISE EXCEPTION 'reject budget threshold event';
			END IF;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER reject_budget_threshold_event_trigger
		BEFORE INSERT ON system_events
		FOR EACH ROW EXECUTE FUNCTION reject_budget_threshold_event();
	`); err != nil {
		t.Fatalf("install failure trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = repo.pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS reject_budget_threshold_event_trigger ON system_events`)
		_, _ = repo.pool.Exec(context.Background(), `DROP FUNCTION IF EXISTS reject_budget_threshold_event()`)
	})

	if _, err := repo.RunAPIKeyBudgetMonitorCycle(ctx, 0, 100, now); err == nil {
		t.Fatal("monitor cycle returned nil error, want event insert failure")
	}
	assertBudgetThresholdStateCount(t, repo, key.ID, 0)
}

func TestAdminRepositoryRevokeAPIKeyRecoversBudgetThresholds(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	key := createBudgetMonitorKey(t, repo, "revoke")
	if _, err := repo.UpdateAPIKeyBudgets(ctx, key.ID, 1, 0, 0, 0, 0, 0); err != nil {
		t.Fatalf("UpdateAPIKeyBudgets returned error: %v", err)
	}
	insertRequestLog(t, repo.pool, key.ID, now.Add(-time.Hour), 200, 0, 0)
	if _, err := repo.RunAPIKeyBudgetMonitorCycle(ctx, 0, 100, now); err != nil {
		t.Fatalf("RunAPIKeyBudgetMonitorCycle returned error: %v", err)
	}
	if _, err := repo.RevokeAPIKey(ctx, key.ID); err != nil {
		t.Fatalf("RevokeAPIKey returned error: %v", err)
	}
	assertBudgetThresholdStateCount(t, repo, key.ID, 0)
	assertBudgetEventCounts(t, repo, key.ID, map[systemevent.Action]int{
		systemevent.ActionAPIKeyBudgetThreshold80Crossed:    1,
		systemevent.ActionAPIKeyBudgetThreshold100Crossed:   1,
		systemevent.ActionAPIKeyBudgetThreshold80Recovered:  1,
		systemevent.ActionAPIKeyBudgetThreshold100Recovered: 1,
	})
	var confirmations int
	if err := repo.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM system_events
		WHERE target_type = 'client_api_key_budget'
			AND target_id LIKE $1
			AND action LIKE '%.recovered'
			AND metadata ->> 'confirmation' = 'key_revoked'
	`, fmtBudgetTargetPrefix(key.ID)+"%").Scan(&confirmations); err != nil {
		t.Fatalf("count revoke confirmations: %v", err)
	}
	if confirmations != 2 {
		t.Fatalf("revoke confirmations = %d, want 2", confirmations)
	}
}

func createBudgetMonitorKey(t *testing.T, repo *AdminRepository, name string) admin.APIKey {
	t.Helper()
	key, err := repo.CreateAPIKey(context.Background(), name, "hash-"+name, "n2api_", "encrypted-"+name, nil)
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}
	return key
}

func assertBudgetThresholdStateCount(t *testing.T, repo *AdminRepository, keyID int64, want int) {
	t.Helper()
	var count int
	if err := repo.pool.QueryRow(context.Background(), `SELECT count(*) FROM api_key_budget_threshold_states WHERE client_key_id = $1`, keyID).Scan(&count); err != nil {
		t.Fatalf("count budget threshold states: %v", err)
	}
	if count != want {
		t.Fatalf("budget threshold states = %d, want %d", count, want)
	}
}

func assertBudgetEventCounts(t *testing.T, repo *AdminRepository, keyID int64, wants map[systemevent.Action]int) {
	t.Helper()
	rows, err := repo.pool.Query(context.Background(), `
		SELECT action, count(*)
		FROM system_events
		WHERE target_type = 'client_api_key_budget' AND target_id LIKE $1
		GROUP BY action
	`, fmtBudgetTargetPrefix(keyID)+"%")
	if err != nil {
		t.Fatalf("query budget event counts: %v", err)
	}
	defer rows.Close()
	got := map[systemevent.Action]int{}
	for rows.Next() {
		var action systemevent.Action
		var count int
		if err := rows.Scan(&action, &count); err != nil {
			t.Fatalf("scan budget event count: %v", err)
		}
		got[action] = count
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate budget event counts: %v", err)
	}
	if len(got) != len(wants) {
		t.Fatalf("budget event counts = %+v, want %+v", got, wants)
	}
	for action, want := range wants {
		if got[action] != want {
			t.Fatalf("budget event %s count = %d, want %d (all=%+v)", action, got[action], want, got)
		}
	}
}

func assertBudgetEventsAreSafeAndDistinct(t *testing.T, repo *AdminRepository, keyID int64) {
	t.Helper()
	rows, err := repo.pool.Query(context.Background(), `
		SELECT target_id, message, metadata
		FROM system_events
		WHERE target_type = 'client_api_key_budget' AND target_id LIKE $1
	`, fmtBudgetTargetPrefix(keyID)+"%")
	if err != nil {
		t.Fatalf("query budget events: %v", err)
	}
	defer rows.Close()
	targets := map[string]struct{}{}
	for rows.Next() {
		var targetID, message string
		var metadataBytes []byte
		if err := rows.Scan(&targetID, &message, &metadataBytes); err != nil {
			t.Fatalf("scan budget event: %v", err)
		}
		var metadata map[string]any
		if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
			t.Fatalf("decode budget event metadata: %v", err)
		}
		for _, key := range []string{"client_key_id", "budget_kind", "window", "threshold_percent", "used", "limit"} {
			if _, ok := metadata[key]; !ok {
				t.Fatalf("budget event metadata missing %q: %+v", key, metadata)
			}
		}
		if message == "" {
			t.Fatal("budget event message is empty")
		}
		targets[targetID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate budget events: %v", err)
	}
	if len(targets) != 6 {
		t.Fatalf("budget event targets = %v, want six streams", targets)
	}
}

func fmtBudgetTargetPrefix(keyID int64) string {
	return strconv.FormatInt(keyID, 10) + ":"
}
