package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/alerting"
	"github.com/KnowSky404/N2API/backend/internal/systemevent"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAlertingRepositoryCRUDAndStateCapacity(t *testing.T) {
	ctx := context.Background()
	repo := newTestAlertingRepository(t, ctx)

	action, err := repo.CreateAction(ctx, alerting.ActionCreate{
		Name: "primary webhook", Kind: alerting.ActionKindGenericWebhook,
		EncryptedDestination: "encrypted-destination-one", Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateAction returned error: %v", err)
	}
	if action.ID == 0 || !action.DestinationConfigured || !action.Enabled {
		t.Fatalf("created action = %+v", action)
	}
	assertAlertActionHasNoDestination(t, action)

	storedDestination, err := repo.GetEncryptedDestination(ctx, action.ID)
	if err != nil || storedDestination != "encrypted-destination-one" {
		t.Fatalf("GetEncryptedDestination = %q, %v", storedDestination, err)
	}
	action, err = repo.UpdateAction(ctx, action.ID, alerting.ActionUpdate{
		Name: "renamed webhook", Kind: alerting.ActionKindGenericWebhook, Enabled: false,
	})
	if err != nil || action.Name != "renamed webhook" || action.Enabled {
		t.Fatalf("UpdateAction retaining destination = %+v, %v", action, err)
	}
	storedDestination, _ = repo.GetEncryptedDestination(ctx, action.ID)
	if storedDestination != "encrypted-destination-one" {
		t.Fatalf("retained destination = %q", storedDestination)
	}
	replacement := "encrypted-destination-two"
	action, err = repo.UpdateAction(ctx, action.ID, alerting.ActionUpdate{
		Name: "renamed webhook", Kind: alerting.ActionKindNtfy,
		EncryptedDestination: &replacement, Enabled: true,
	})
	if err != nil || action.Kind != alerting.ActionKindNtfy || !action.Enabled {
		t.Fatalf("UpdateAction replacing destination = %+v, %v", action, err)
	}
	storedDestination, _ = repo.GetEncryptedDestination(ctx, action.ID)
	if storedDestination != replacement {
		t.Fatalf("replaced destination = %q", storedDestination)
	}

	actions, err := repo.ListActions(ctx)
	if err != nil || len(actions) != 1 || actions[0].ID != action.ID {
		t.Fatalf("ListActions = %+v, %v", actions, err)
	}
	assertAlertActionHasNoDestination(t, actions[0])
	if _, err := repo.GetAction(ctx, action.ID); err != nil {
		t.Fatalf("GetAction returned error: %v", err)
	}

	ruleInput := alerting.Rule{
		Name: "oauth failures", ActionID: action.ID, Enabled: true,
		Category: "oauth", Severity: "error", EventAction: "oauth.refresh.automatic.failed",
		RecoveryAction: "oauth.refresh.automatic.succeeded", AggregationCount: 2,
		AggregationWindowSeconds: 60, CooldownSeconds: 300,
		DeduplicationScope: alerting.DeduplicationScopeTarget, NotifyRecovery: true,
	}
	rule, err := repo.CreateRule(ctx, alerting.RuleCreate{Rule: ruleInput})
	if err != nil {
		t.Fatalf("CreateRule returned error: %v", err)
	}
	if rule.ID == 0 || rule.Name != ruleInput.Name || rule.ActionID != action.ID {
		t.Fatalf("created rule = %+v", rule)
	}
	ruleInput.Name = "renamed oauth failures"
	ruleInput.Enabled = false
	rule, err = repo.UpdateRule(ctx, rule.ID, alerting.RuleUpdate{Rule: ruleInput})
	if err != nil || rule.Name != ruleInput.Name || rule.Enabled {
		t.Fatalf("UpdateRule = %+v, %v", rule, err)
	}
	rules, err := repo.ListRules(ctx)
	if err != nil || len(rules) != 1 || rules[0].ID != rule.ID {
		t.Fatalf("ListRules = %+v, %v", rules, err)
	}
	if _, err := repo.GetRule(ctx, rule.ID); err != nil {
		t.Fatalf("GetRule returned error: %v", err)
	}
	if err := repo.DeleteAction(ctx, action.ID); err == nil {
		t.Fatal("DeleteAction removed an action referenced by a rule")
	}

	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	state := alerting.RuleState{
		RuleID: rule.ID, DeduplicationKeyHash: strings.Repeat("a", 64), Phase: alerting.StatePhaseFiring,
		WindowMatchCount: 0, CooldownUntil: timeReference(now.Add(time.Minute)),
		LastMatchedAt: timeReference(now), LastNotifiedAt: timeReference(now), UpdatedAt: now,
	}
	if err := repo.SaveRuleState(ctx, state); err != nil {
		t.Fatalf("SaveRuleState returned error: %v", err)
	}
	loadedState, err := repo.GetRuleState(ctx, state.RuleID, state.DeduplicationKeyHash)
	if err != nil || loadedState.Phase != state.Phase || loadedState.LastMatchedAt == nil || !loadedState.LastMatchedAt.Equal(now) {
		t.Fatalf("GetRuleState = %+v, %v", loadedState, err)
	}

	capacityRule, err := repo.CreateRule(ctx, alerting.RuleCreate{Rule: ruleInput})
	if err != nil {
		t.Fatalf("CreateRule for capacity returned error: %v", err)
	}
	seedAlertRuleStates(t, ctx, repo.pool, capacityRule.ID, alerting.MaxRuleStatesPerRule)
	newHash := strings.Repeat("f", 64)
	if err := repo.SaveRuleState(ctx, alerting.RuleState{
		RuleID: capacityRule.ID, DeduplicationKeyHash: newHash,
		Phase: alerting.StatePhaseIdle, UpdatedAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("SaveRuleState with idle eviction returned error: %v", err)
	}
	assertAlertStateCount(t, ctx, repo.pool, capacityRule.ID, alerting.MaxRuleStatesPerRule)
	if _, err := repo.GetRuleState(ctx, capacityRule.ID, alertStateHash(1)); !errors.Is(err, alerting.ErrNotFound) {
		t.Fatalf("oldest idle state error = %v, want ErrNotFound", err)
	}
	if _, err := repo.GetRuleState(ctx, capacityRule.ID, newHash); err != nil {
		t.Fatalf("new state was not saved: %v", err)
	}

	if _, err := repo.pool.Exec(ctx, `UPDATE alert_rule_states SET phase = 'firing' WHERE rule_id = $1`, capacityRule.ID); err != nil {
		t.Fatalf("mark all states firing: %v", err)
	}
	err = repo.SaveRuleState(ctx, alerting.RuleState{
		RuleID: capacityRule.ID, DeduplicationKeyHash: strings.Repeat("e", 64),
		Phase: alerting.StatePhaseIdle, UpdatedAt: now.Add(2 * time.Hour),
	})
	if !errors.Is(err, alerting.ErrStateCapacity) {
		t.Fatalf("SaveRuleState full firing error = %v, want ErrStateCapacity", err)
	}
	assertAlertStateCount(t, ctx, repo.pool, capacityRule.ID, alerting.MaxRuleStatesPerRule)

	if _, err := repo.pool.Exec(ctx, `
		INSERT INTO alert_actions (name, kind, encrypted_destination)
		VALUES ('invalid', 'telegram', 'ciphertext')
	`); err == nil {
		t.Fatal("alert action kind constraint accepted unsupported kind")
	}
	if _, err := repo.pool.Exec(ctx, `
		INSERT INTO alert_rules (name, action_id, aggregation_count, deduplication_scope)
		VALUES ('no filter', $1, 1, 'target')
	`, action.ID); err == nil {
		t.Fatal("alert rule trigger constraint accepted empty filters")
	}
	if _, err := repo.pool.Exec(ctx, `
		INSERT INTO alert_rules (name, action_id, event_action, recovery_action)
		VALUES ('same actions', $1, 'oauth.refresh.automatic.failed', 'oauth.refresh.automatic.failed')
	`, action.ID); err == nil {
		t.Fatal("alert rule accepted the same trigger and recovery action")
	}

	if err := repo.DeleteRule(ctx, rule.ID); err != nil {
		t.Fatalf("DeleteRule returned error: %v", err)
	}
	if _, err := repo.GetRuleState(ctx, rule.ID, state.DeduplicationKeyHash); !errors.Is(err, alerting.ErrNotFound) {
		t.Fatalf("cascaded state error = %v, want ErrNotFound", err)
	}
	if err := repo.DeleteRule(ctx, capacityRule.ID); err != nil {
		t.Fatalf("DeleteRule for capacity rule returned error: %v", err)
	}
	if err := repo.DeleteAction(ctx, action.ID); err != nil {
		t.Fatalf("DeleteAction after rule deletion returned error: %v", err)
	}
	if _, err := repo.GetAction(ctx, action.ID); !errors.Is(err, alerting.ErrNotFound) {
		t.Fatalf("deleted action error = %v, want ErrNotFound", err)
	}
	if _, err := repo.UpdateAction(ctx, action.ID, alerting.ActionUpdate{}); !errors.Is(err, alerting.ErrNotFound) {
		t.Fatalf("missing action update error = %v, want ErrNotFound", err)
	}
	if _, err := repo.UpdateRule(ctx, rule.ID, alerting.RuleUpdate{Rule: ruleInput}); !errors.Is(err, alerting.ErrNotFound) {
		t.Fatalf("missing rule update error = %v, want ErrNotFound", err)
	}
}

func TestAlertingRepositorySerializesEvaluationAndResetsStateOnRuleUpdate(t *testing.T) {
	ctx := context.Background()
	repo := newTestAlertingRepository(t, ctx)
	action, err := repo.CreateAction(ctx, alerting.ActionCreate{
		Name: "concurrency webhook", Kind: alerting.ActionKindGenericWebhook,
		EncryptedDestination: "encrypted-concurrency-destination", Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateAction returned error: %v", err)
	}
	rule, err := repo.CreateRule(ctx, alerting.RuleCreate{Rule: alerting.Rule{
		Name: "concurrent oauth failures", ActionID: action.ID, Enabled: true,
		Category: systemevent.CategoryOAuth, Severity: systemevent.SeverityError,
		EventAction:      systemevent.ActionOAuthRefreshAutomaticFailed,
		RecoveryAction:   systemevent.ActionOAuthRefreshAutomaticSucceeded,
		AggregationCount: 2, AggregationWindowSeconds: 60, CooldownSeconds: 300,
		DeduplicationScope: alerting.DeduplicationScopeTarget, NotifyRecovery: true,
	}})
	if err != nil {
		t.Fatalf("CreateRule returned error: %v", err)
	}
	now := time.Date(2026, time.July, 21, 13, 0, 0, 0, time.UTC)
	trigger := alertingStoreEvent(systemevent.ActionOAuthRefreshAutomaticFailed, systemevent.SeverityError, systemevent.OutcomeFailure, now)
	hash := rule.DeduplicationKeyHash(trigger)

	unmatched := trigger
	unmatched.Category = systemevent.CategoryRuntime
	if _, decision, err := repo.EvaluateRuleEvent(ctx, rule.ID, unmatched, now); err != nil || decision != alerting.DecisionNone {
		t.Fatalf("unmatched evaluation decision=%q error=%v", decision, err)
	}
	if _, err := repo.GetRuleState(ctx, rule.ID, hash); !errors.Is(err, alerting.ErrNotFound) {
		t.Fatalf("unmatched event persisted state: %v", err)
	}

	recovery := alertingStoreEvent(systemevent.ActionOAuthRefreshAutomaticSucceeded, systemevent.SeverityInfo, systemevent.OutcomeSuccess, now)
	if _, decision, err := repo.EvaluateRuleEvent(ctx, rule.ID, recovery, now); err != nil || decision != alerting.DecisionNone {
		t.Fatalf("idle recovery decision=%q error=%v", decision, err)
	}
	if _, err := repo.GetRuleState(ctx, rule.ID, hash); !errors.Is(err, alerting.ErrNotFound) {
		t.Fatalf("idle recovery persisted state: %v", err)
	}

	type evaluationResult struct {
		decision alerting.Decision
		err      error
	}
	start := make(chan struct{})
	results := make(chan evaluationResult, 2)
	var workers sync.WaitGroup
	workers.Add(2)
	for range 2 {
		go func() {
			defer workers.Done()
			<-start
			_, decision, err := repo.EvaluateRuleEvent(ctx, rule.ID, trigger, now)
			results <- evaluationResult{decision: decision, err: err}
		}()
	}
	close(start)
	workers.Wait()
	close(results)

	notifyCount := 0
	noneCount := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent EvaluateRuleEvent returned error: %v", result.err)
		}
		switch result.decision {
		case alerting.DecisionNotify:
			notifyCount++
		case alerting.DecisionNone:
			noneCount++
		default:
			t.Fatalf("concurrent decision = %q", result.decision)
		}
	}
	if notifyCount != 1 || noneCount != 1 {
		t.Fatalf("concurrent decisions notify=%d none=%d, want 1 each", notifyCount, noneCount)
	}
	state, err := repo.GetRuleState(ctx, rule.ID, hash)
	if err != nil || state.Phase != alerting.StatePhaseFiring || state.LastNotifiedAt == nil {
		t.Fatalf("final concurrent state = %+v, %v", state, err)
	}

	rule.AggregationCount = 3
	if _, err := repo.UpdateRule(ctx, rule.ID, alerting.RuleUpdate{Rule: rule}); err != nil {
		t.Fatalf("UpdateRule returned error: %v", err)
	}
	if _, err := repo.GetRuleState(ctx, rule.ID, hash); !errors.Is(err, alerting.ErrNotFound) {
		t.Fatalf("state after UpdateRule error = %v, want ErrNotFound", err)
	}
}

func newTestAlertingRepository(t *testing.T, ctx context.Context) *AlertingRepository {
	t.Helper()
	dsn := os.Getenv("N2API_STORE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set N2API_STORE_TEST_DATABASE_URL to run PostgreSQL store integration tests")
	}

	adminPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect alerting test database: %v", err)
	}
	t.Cleanup(adminPool.Close)
	schema := fmt.Sprintf("alerting_store_%d", time.Now().UnixNano())
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create alerting test schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := adminPool.Exec(cleanupCtx, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("drop alerting test schema: %v", err)
		}
	})

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse alerting test database URL: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	config.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("connect isolated alerting test schema: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatalf("migrate isolated alerting test schema: %v", err)
	}
	return NewAlertingRepository(pool)
}

func seedAlertRuleStates(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ruleID int64, count int) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO alert_rule_states (
			rule_id, deduplication_key_hash, phase, updated_at
		)
		SELECT $1, lpad(to_hex(n), 64, '0'),
			CASE WHEN n = 1 THEN 'idle' ELSE 'firing' END,
			TIMESTAMPTZ '2026-07-21 00:00:00+00' + n * INTERVAL '1 second'
		FROM generate_series(1, $2) AS states(n)
	`, ruleID, count)
	if err != nil {
		t.Fatalf("seed alert rule states: %v", err)
	}
}

func assertAlertStateCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ruleID int64, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM alert_rule_states WHERE rule_id = $1`, ruleID).Scan(&got); err != nil {
		t.Fatalf("count alert rule states: %v", err)
	}
	if got != want {
		t.Fatalf("alert rule state count = %d, want %d", got, want)
	}
}

func assertAlertActionHasNoDestination(t *testing.T, action alerting.Action) {
	t.Helper()
	value := fmt.Sprintf("%+v", action)
	if strings.Contains(value, "encrypted-destination") {
		t.Fatalf("action leaked encrypted destination: %s", value)
	}
}

func alertStateHash(value int) string {
	return fmt.Sprintf("%064x", value)
}

func timeReference(value time.Time) *time.Time {
	return &value
}

func alertingStoreEvent(action systemevent.Action, severity systemevent.Severity, outcome systemevent.Outcome, occurredAt time.Time) systemevent.Event {
	return systemevent.Event{
		OccurredAt: occurredAt, Category: systemevent.CategoryOAuth, Severity: severity,
		Action: action, Outcome: outcome, Actor: systemevent.Actor{Type: systemevent.ActorSystem},
		Target:        systemevent.Target{Type: "provider_account", ID: "42"},
		CorrelationID: "alerting-store-evaluation", Metadata: map[string]any{},
	}
}
