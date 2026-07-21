package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/alerting"
	"github.com/KnowSky404/N2API/backend/internal/systemevent"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	originalActionRevision := action.UpdatedAt
	action, err = repo.UpdateAction(ctx, action.ID, alerting.ActionUpdate{
		Name: "renamed webhook", Kind: alerting.ActionKindGenericWebhook, Enabled: false, ExpectedUpdatedAt: action.UpdatedAt,
	})
	if err != nil || action.Name != "renamed webhook" || action.Enabled {
		t.Fatalf("UpdateAction retaining destination = %+v, %v", action, err)
	}
	if !action.UpdatedAt.After(originalActionRevision) {
		t.Fatalf("action revision = %s, want after %s", action.UpdatedAt, originalActionRevision)
	}
	if _, err := repo.UpdateAction(ctx, action.ID, alerting.ActionUpdate{
		Name: action.Name, Kind: action.Kind, Enabled: action.Enabled, ExpectedUpdatedAt: originalActionRevision,
	}); !errors.Is(err, alerting.ErrConflict) {
		t.Fatalf("stale UpdateAction error = %v, want ErrConflict", err)
	}
	storedDestination, _ = repo.GetEncryptedDestination(ctx, action.ID)
	if storedDestination != "encrypted-destination-one" {
		t.Fatalf("retained destination = %q", storedDestination)
	}
	replacement := "encrypted-destination-two"
	action, err = repo.UpdateAction(ctx, action.ID, alerting.ActionUpdate{
		Name: "renamed webhook", Kind: alerting.ActionKindNtfy,
		EncryptedDestination: &replacement, Enabled: true, ExpectedUpdatedAt: action.UpdatedAt,
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
	if rule.ID == 0 || rule.Name != ruleInput.Name || rule.ActionID != action.ID || rule.TemplateKey != "" {
		t.Fatalf("created rule = %+v", rule)
	}
	ruleInput.Name = "renamed oauth failures"
	ruleInput.Enabled = false
	originalRuleRevision := rule.UpdatedAt
	rule, err = repo.UpdateRule(ctx, rule.ID, alerting.RuleUpdate{Rule: ruleInput, ExpectedUpdatedAt: originalRuleRevision})
	if err != nil || rule.Name != ruleInput.Name || rule.Enabled || rule.TemplateKey != "" {
		t.Fatalf("UpdateRule = %+v, %v", rule, err)
	}
	if !rule.UpdatedAt.After(originalRuleRevision) {
		t.Fatalf("rule revision = %s, want after %s", rule.UpdatedAt, originalRuleRevision)
	}
	if _, err := repo.UpdateRule(ctx, rule.ID, alerting.RuleUpdate{Rule: ruleInput, ExpectedUpdatedAt: originalRuleRevision}); !errors.Is(err, alerting.ErrConflict) {
		t.Fatalf("stale UpdateRule error = %v, want ErrConflict", err)
	}
	rules, err := repo.ListRules(ctx)
	if err != nil || len(rules) != 1 || rules[0].ID != rule.ID {
		t.Fatalf("ListRules = %+v, %v", rules, err)
	}
	if _, err := repo.GetRule(ctx, rule.ID); err != nil {
		t.Fatalf("GetRule returned error: %v", err)
	}
	if err := repo.DeleteAction(ctx, action.ID); !errors.Is(err, alerting.ErrConflict) {
		t.Fatalf("DeleteAction referenced action error = %v, want ErrConflict", err)
	}
	if _, err := repo.CreateRule(ctx, alerting.RuleCreate{Rule: alerting.Rule{
		Name: "missing action", ActionID: action.ID + 999, Enabled: true,
		Severity: systemevent.SeverityError, AggregationCount: 1,
		CooldownSeconds: 300, DeduplicationScope: alerting.DeduplicationScopeTarget,
	}}); !errors.Is(err, alerting.ErrNotFound) {
		t.Fatalf("CreateRule missing action error = %v, want ErrNotFound", err)
	}

	configRevision := action.UpdatedAt
	statusCode := 503
	testResult := alerting.ActionTestResult{
		TestedAt: time.Date(2026, time.July, 21, 11, 59, 0, 0, time.UTC), Status: alerting.ActionTestStatusFailed,
		HTTPStatus: &statusCode, LatencyMS: 125, ErrorCode: "alert_delivery_http_status", Retryable: true,
	}
	testStart, err := repo.BeginActionTest(ctx, action.ID, configRevision, "0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("BeginActionTest returned error: %v", err)
	}
	testedAction, err := repo.FinalizeActionTest(ctx, action.ID, testStart.AttemptToken, testResult)
	if err != nil {
		t.Fatalf("FinalizeActionTest returned error: %v", err)
	}
	if !testedAction.UpdatedAt.Equal(configRevision) || testedAction.LastTestedAt == nil || testedAction.LastTestStatus != alerting.ActionTestStatusFailed || testedAction.LastTestHTTPStatus == nil || *testedAction.LastTestHTTPStatus != statusCode {
		t.Fatalf("tested action = %+v, config revision=%s", testedAction, configRevision)
	}
	if _, err := repo.BeginActionTest(ctx, action.ID, configRevision.Add(-time.Second), "1123456789abcdef0123456789abcdef"); !errors.Is(err, alerting.ErrConflict) {
		t.Fatalf("stale BeginActionTest error = %v, want ErrConflict", err)
	}
	if _, err := repo.FinalizeActionTest(ctx, action.ID, testStart.AttemptToken, testResult); !errors.Is(err, alerting.ErrConflict) {
		t.Fatalf("repeated FinalizeActionTest error = %v, want ErrConflict", err)
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
	if _, err := repo.UpdateAction(ctx, action.ID, alerting.ActionUpdate{ExpectedUpdatedAt: now}); !errors.Is(err, alerting.ErrNotFound) {
		t.Fatalf("missing action update error = %v, want ErrNotFound", err)
	}
	if _, err := repo.UpdateRule(ctx, rule.ID, alerting.RuleUpdate{Rule: ruleInput, ExpectedUpdatedAt: now}); !errors.Is(err, alerting.ErrNotFound) {
		t.Fatalf("missing rule update error = %v, want ErrNotFound", err)
	}
}

func TestAlertingRepositoryAuditEventCommitsAtomically(t *testing.T) {
	ctx := context.Background()
	repo := newTestAlertingRepository(t, ctx)
	requestCtx := systemevent.WithRequestContext(ctx, systemevent.RequestContext{
		CorrelationID: systemevent.NewCorrelationID(),
		Actor:         systemevent.Actor{Type: systemevent.ActorAdmin, ID: 1, Name: "owner"},
	})
	createCtx := systemevent.WithIntent(requestCtx, systemevent.EventIntent{
		Category: systemevent.CategoryAudit, Severity: systemevent.SeverityInfo,
		Action: systemevent.ActionAlertActionCreated, Outcome: systemevent.OutcomeSuccess,
		Target: systemevent.Target{Type: "alert_action"},
	})
	action, err := repo.CreateAction(createCtx, alerting.ActionCreate{
		Name: "audited action", Kind: alerting.ActionKindGenericWebhook,
		EncryptedDestination: "encrypted-audited-destination", Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateAction returned error: %v", err)
	}
	var actionName string
	var actorType string
	if err := repo.pool.QueryRow(ctx, `
		SELECT target_name, actor_type
		FROM system_events
		WHERE action = $1 AND target_id = $2
	`, systemevent.ActionAlertActionCreated, strconv.FormatInt(action.ID, 10)).Scan(&actionName, &actorType); err != nil {
		t.Fatalf("load alert action audit event: %v", err)
	}
	if actionName != action.Name || actorType != string(systemevent.ActorAdmin) {
		t.Fatalf("audit target=%q actor=%q", actionName, actorType)
	}
	var count int
	testedAt := time.Date(2026, time.July, 21, 13, 0, 0, 0, time.UTC)
	testCtx := systemevent.WithIntent(requestCtx, systemevent.EventIntent{
		Category: systemevent.CategoryAudit, Severity: systemevent.SeverityInfo,
		Action: systemevent.ActionAlertDeliveryTested, Outcome: systemevent.OutcomeSuccess,
		Target:   systemevent.Target{Type: "alert_action"},
		Metadata: map[string]any{"latency_ms": int64(15), "retryable": false},
	})
	testStart, err := repo.BeginActionTest(ctx, action.ID, action.UpdatedAt, "2123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("BeginActionTest returned error: %v", err)
	}
	if _, err := repo.FinalizeActionTest(testCtx, action.ID, testStart.AttemptToken, alerting.ActionTestResult{
		TestedAt: testedAt, Status: alerting.ActionTestStatusPassed, LatencyMS: 15,
	}); err != nil {
		t.Fatalf("FinalizeActionTest returned error: %v", err)
	}
	if err := repo.pool.QueryRow(ctx, `SELECT count(*) FROM system_events WHERE action = $1 AND target_id = $2`,
		systemevent.ActionAlertDeliveryTested, strconv.FormatInt(action.ID, 10)).Scan(&count); err != nil || count != 1 {
		t.Fatalf("test audit event count=%d err=%v", count, err)
	}

	invalidCtx := systemevent.WithIntent(requestCtx, systemevent.EventIntent{
		Category: systemevent.CategoryAudit, Severity: systemevent.SeverityInfo,
		Action: systemevent.Action("unknown.alert.action"), Outcome: systemevent.OutcomeSuccess,
		Target: systemevent.Target{Type: "alert_action"},
	})
	if _, err := repo.CreateAction(invalidCtx, alerting.ActionCreate{
		Name: "rolled back action", Kind: alerting.ActionKindGenericWebhook,
		EncryptedDestination: "encrypted-rolled-back-destination", Enabled: true,
	}); err == nil {
		t.Fatal("CreateAction with invalid audit intent returned nil error")
	}
	if err := repo.pool.QueryRow(ctx, `SELECT count(*) FROM alert_actions WHERE name = 'rolled back action'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rolled back actions = %d, want 0", count)
	}
	failedStatus := 503
	if _, err := repo.pool.Exec(ctx, `UPDATE alert_actions SET last_test_started_at = clock_timestamp() - interval '31 seconds' WHERE id = $1`, action.ID); err != nil {
		t.Fatal(err)
	}
	invalidStart, err := repo.BeginActionTest(ctx, action.ID, action.UpdatedAt, "3123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("BeginActionTest for invalid audit returned error: %v", err)
	}
	if _, err := repo.FinalizeActionTest(invalidCtx, action.ID, invalidStart.AttemptToken, alerting.ActionTestResult{
		TestedAt: testedAt.Add(time.Minute), Status: alerting.ActionTestStatusFailed,
		HTTPStatus: &failedStatus, LatencyMS: 25, ErrorCode: "alert_delivery_http_status", Retryable: true,
	}); err == nil {
		t.Fatal("FinalizeActionTest with invalid audit intent returned nil error")
	}
	reloaded, err := repo.GetAction(ctx, action.ID)
	if err != nil || reloaded.LastTestedAt == nil || !reloaded.LastTestedAt.Equal(testedAt) || reloaded.LastTestStatus != alerting.ActionTestStatusPassed {
		t.Fatalf("test result after audit rollback = %+v, err=%v", reloaded, err)
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
		Category: systemevent.CategoryOAuth, Severity: systemevent.SeverityWarning,
		EventAction:      systemevent.ActionOAuthRefreshAutomaticFailed,
		RecoveryAction:   systemevent.ActionOAuthRefreshAutomaticSucceeded,
		AggregationCount: 2, AggregationWindowSeconds: 60, CooldownSeconds: 300,
		DeduplicationScope: alerting.DeduplicationScopeTarget, NotifyRecovery: true,
	}})
	if err != nil {
		t.Fatalf("CreateRule returned error: %v", err)
	}
	now := time.Date(2026, time.July, 21, 13, 0, 0, 0, time.UTC)
	trigger := alertingStoreEvent(systemevent.ActionOAuthRefreshAutomaticFailed, systemevent.SeverityWarning, systemevent.OutcomeFailure, now)
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
	if _, err := repo.UpdateRule(ctx, rule.ID, alerting.RuleUpdate{Rule: rule, ExpectedUpdatedAt: rule.UpdatedAt}); err != nil {
		t.Fatalf("UpdateRule returned error: %v", err)
	}
	if _, err := repo.GetRuleState(ctx, rule.ID, hash); !errors.Is(err, alerting.ErrNotFound) {
		t.Fatalf("state after UpdateRule error = %v, want ErrNotFound", err)
	}
}

func TestAlertingRepositoryDeliveryEvaluationUsesLockedRuleAndAction(t *testing.T) {
	ctx := context.Background()
	repo := newTestAlertingRepository(t, ctx)
	action, err := repo.CreateAction(ctx, alerting.ActionCreate{
		Name: "disabled webhook", Kind: alerting.ActionKindGenericWebhook,
		EncryptedDestination: "encrypted-disabled-destination", Enabled: false,
	})
	if err != nil {
		t.Fatalf("CreateAction: %v", err)
	}
	rule, err := repo.CreateRule(ctx, alerting.RuleCreate{Rule: alerting.Rule{
		Name: "single failure", ActionID: action.ID, Enabled: true,
		Category: systemevent.CategoryOAuth, Severity: systemevent.SeverityWarning,
		EventAction:      systemevent.ActionOAuthRefreshAutomaticFailed,
		AggregationCount: 1, CooldownSeconds: 300,
		DeduplicationScope: alerting.DeduplicationScopeTarget,
	}})
	if err != nil {
		t.Fatalf("CreateRule: %v", err)
	}
	now := time.Date(2026, time.July, 21, 14, 0, 0, 0, time.UTC)
	event := alertingStoreEvent(systemevent.ActionOAuthRefreshAutomaticFailed, systemevent.SeverityWarning, systemevent.OutcomeFailure, now)
	evaluation, err := repo.EvaluateRuleEventForDelivery(ctx, rule.ID, event, now)
	if err != nil || evaluation.ActionEnabled || evaluation.Decision != alerting.DecisionNone || evaluation.Rule.ID != rule.ID || !evaluation.ActionUpdatedAt.Equal(action.UpdatedAt) {
		t.Fatalf("disabled action evaluation = %+v, %v", evaluation, err)
	}
	if _, err := repo.GetRuleState(ctx, rule.ID, rule.DeduplicationKeyHash(event)); !errors.Is(err, alerting.ErrNotFound) {
		t.Fatalf("disabled action advanced rule state: %v", err)
	}

	enabledAction, err := repo.UpdateAction(ctx, action.ID, alerting.ActionUpdate{
		Name: action.Name, Kind: action.Kind, Enabled: true, ExpectedUpdatedAt: action.UpdatedAt,
	})
	if err != nil {
		t.Fatalf("enable action: %v", err)
	}
	evaluation, err = repo.EvaluateRuleEventForDelivery(ctx, rule.ID, event, now.Add(time.Second))
	if err != nil || !evaluation.ActionEnabled || evaluation.Decision != alerting.DecisionNotify ||
		evaluation.Rule.ActionID != action.ID || evaluation.State.Phase != alerting.StatePhaseFiring || !evaluation.ActionUpdatedAt.Equal(enabledAction.UpdatedAt) {
		t.Fatalf("enabled action evaluation = %+v, %v", evaluation, err)
	}
}

func TestAlertingRepositoryBeginActionTestSerializesAcrossRepositories(t *testing.T) {
	ctx := context.Background()
	repoOne := newTestAlertingRepository(t, ctx)
	repoTwo := NewAlertingRepository(repoOne.pool)
	action, err := repoOne.CreateAction(ctx, alerting.ActionCreate{
		Name: "admission webhook", Kind: alerting.ActionKindGenericWebhook,
		EncryptedDestination: "encrypted-admission-destination", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	type beginResult struct {
		start alerting.ActionTestStart
		err   error
	}
	ready := make(chan struct{})
	results := make(chan beginResult, 2)
	for index, repository := range []*AlertingRepository{repoOne, repoTwo} {
		token := fmt.Sprintf("%032x", index+1)
		go func(repository *AlertingRepository, token string) {
			<-ready
			start, err := repository.BeginActionTest(ctx, action.ID, action.UpdatedAt, token)
			results <- beginResult{start: start, err: err}
		}(repository, token)
	}
	close(ready)
	var admitted, rateLimited int
	for range 2 {
		result := <-results
		if result.err == nil {
			admitted++
			if result.start.AttemptToken == "" || result.start.StartedAt.IsZero() {
				t.Fatalf("invalid admitted start: %+v", result.start)
			}
			continue
		}
		var rateLimit *alerting.RateLimitError
		if !errors.As(result.err, &rateLimit) || rateLimit.RetryAfter <= 0 || rateLimit.RetryAfter > alerting.ActionTestAdmissionWindow {
			t.Fatalf("second begin error = %#v", result.err)
		}
		rateLimited++
	}
	if admitted != 1 || rateLimited != 1 {
		t.Fatalf("admitted=%d rateLimited=%d, want 1 each", admitted, rateLimited)
	}
}

func TestAlertingRepositoryActionTestFinalizeInterleavingsAndAttemptToken(t *testing.T) {
	ctx := context.Background()
	repo := newTestAlertingRepository(t, ctx)
	action, err := repo.CreateAction(ctx, alerting.ActionCreate{
		Name: "interleaving webhook", Kind: alerting.ActionKindGenericWebhook,
		EncryptedDestination: "encrypted-interleaving-destination", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	result := alerting.ActionTestResult{
		TestedAt: time.Date(2026, time.July, 21, 16, 0, 0, 0, time.UTC),
		Status:   alerting.ActionTestStatusPassed, LatencyMS: 10,
	}

	first, err := repo.BeginActionTest(ctx, action.ID, action.UpdatedAt, "4123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := repo.UpdateAction(ctx, action.ID, alerting.ActionUpdate{
		Name: action.Name, Kind: action.Kind, Enabled: false, ExpectedUpdatedAt: action.UpdatedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	finalized, err := repo.FinalizeActionTest(ctx, action.ID, first.AttemptToken, result)
	if err != nil {
		t.Fatalf("finalize after update returned error: %v", err)
	}
	if finalized.LastTestedAt != nil || finalized.LastTestStatus != "" {
		t.Fatalf("old configuration result was visible: %+v", finalized)
	}
	var storedStatus string
	var resultRevision time.Time
	if err := repo.pool.QueryRow(ctx, `SELECT last_test_status, last_test_config_updated_at FROM alert_actions WHERE id = $1`, action.ID).Scan(&storedStatus, &resultRevision); err != nil {
		t.Fatal(err)
	}
	if storedStatus != string(alerting.ActionTestStatusPassed) || !resultRevision.Equal(action.UpdatedAt) || resultRevision.Equal(updated.UpdatedAt) {
		t.Fatalf("stored status=%q result revision=%s old=%s new=%s", storedStatus, resultRevision, action.UpdatedAt, updated.UpdatedAt)
	}

	if _, err := repo.pool.Exec(ctx, `UPDATE alert_actions SET last_test_started_at = clock_timestamp() - interval '31 seconds' WHERE id = $1`, action.ID); err != nil {
		t.Fatal(err)
	}
	second, err := repo.BeginActionTest(ctx, action.ID, updated.UpdatedAt, "5123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FinalizeActionTest(ctx, action.ID, second.AttemptToken, result); err != nil {
		t.Fatalf("finalize before update returned error: %v", err)
	}
	visible, err := repo.GetAction(ctx, action.ID)
	if err != nil || visible.LastTestedAt == nil || visible.LastTestStatus != alerting.ActionTestStatusPassed {
		t.Fatalf("current result = %+v, err=%v", visible, err)
	}
	afterFinalizeUpdate, err := repo.UpdateAction(ctx, action.ID, alerting.ActionUpdate{
		Name: updated.Name, Kind: updated.Kind, Enabled: true, ExpectedUpdatedAt: updated.UpdatedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if afterFinalizeUpdate.LastTestedAt != nil || afterFinalizeUpdate.LastTestStatus != "" {
		t.Fatalf("result remained visible after config update: %+v", afterFinalizeUpdate)
	}

	if _, err := repo.pool.Exec(ctx, `UPDATE alert_actions SET last_test_started_at = clock_timestamp() - interval '31 seconds' WHERE id = $1`, action.ID); err != nil {
		t.Fatal(err)
	}
	staleAttempt, err := repo.BeginActionTest(ctx, action.ID, afterFinalizeUpdate.UpdatedAt, "6123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.pool.Exec(ctx, `UPDATE alert_actions SET last_test_started_at = clock_timestamp() - interval '31 seconds' WHERE id = $1`, action.ID); err != nil {
		t.Fatal(err)
	}
	currentAttempt, err := repo.BeginActionTest(ctx, action.ID, afterFinalizeUpdate.UpdatedAt, "7123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FinalizeActionTest(ctx, action.ID, staleAttempt.AttemptToken, result); !errors.Is(err, alerting.ErrConflict) {
		t.Fatalf("stale attempt finalize error = %v, want ErrConflict", err)
	}
	if _, err := repo.FinalizeActionTest(ctx, action.ID, currentAttempt.AttemptToken, result); err != nil {
		t.Fatalf("current attempt finalize error = %v", err)
	}

	deletedAction, err := repo.CreateAction(ctx, alerting.ActionCreate{
		Name: "deleted during test", Kind: alerting.ActionKindGenericWebhook,
		EncryptedDestination: "encrypted-deleted-destination", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	deletedAttempt, err := repo.BeginActionTest(ctx, deletedAction.ID, deletedAction.UpdatedAt, "8123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteAction(ctx, deletedAction.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FinalizeActionTest(ctx, deletedAction.ID, deletedAttempt.AttemptToken, result); !errors.Is(err, alerting.ErrNotFound) {
		t.Fatalf("deleted action finalize error = %v, want ErrNotFound", err)
	}
}

func TestAlertingRepositoryInstallsRuleTemplateIdempotentlyWithoutOverwritingEdits(t *testing.T) {
	ctx := context.Background()
	repo := newTestAlertingRepository(t, ctx)
	firstAction, err := repo.CreateAction(ctx, alerting.ActionCreate{
		Name: "first template action", Kind: alerting.ActionKindGenericWebhook,
		EncryptedDestination: "encrypted-template-one", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	secondAction, err := repo.CreateAction(ctx, alerting.ActionCreate{
		Name: "second template action", Kind: alerting.ActionKindGenericWebhook,
		EncryptedDestination: "encrypted-template-two", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.InstallRuleTemplate(ctx, alerting.RuleCreate{Rule: alerting.Rule{ActionID: firstAction.ID}}); !errors.Is(err, alerting.ErrInvalidInput) {
		t.Fatalf("empty template key error = %v, want ErrInvalidInput", err)
	}
	template := alerting.Rule{
		TemplateKey: "oauth-refresh-repeated-v1", Name: "Repeated OAuth refresh failures",
		ActionID: firstAction.ID, Enabled: false, Category: systemevent.CategoryOAuth,
		Severity: systemevent.SeverityWarning, EventAction: systemevent.ActionOAuthRefreshAutomaticFailed,
		RecoveryAction:   systemevent.ActionOAuthRefreshAutomaticSucceeded,
		AggregationCount: 3, AggregationWindowSeconds: 900, CooldownSeconds: 3600,
		DeduplicationScope: alerting.DeduplicationScopeTarget, NotifyRecovery: true,
	}
	auditCtx := systemevent.WithIntent(ctx, systemevent.EventIntent{
		Category: systemevent.CategoryAudit, Severity: systemevent.SeverityInfo,
		Action: systemevent.ActionAlertRuleCreated, Outcome: systemevent.OutcomeSuccess,
		Target: systemevent.Target{Type: "alert_rule"},
	})
	installed, created, err := repo.InstallRuleTemplate(auditCtx, alerting.RuleCreate{Rule: template})
	if err != nil || !created || installed.ID == 0 || installed.TemplateKey != template.TemplateKey || installed.Enabled {
		t.Fatalf("first install = %+v, created=%t, err=%v", installed, created, err)
	}

	editedInput := installed
	editedInput.Name = "Owner-reviewed OAuth failures"
	editedInput.Enabled = true
	updateCtx := systemevent.WithIntent(ctx, systemevent.EventIntent{
		Category: systemevent.CategoryAudit, Severity: systemevent.SeverityInfo,
		Action: systemevent.ActionAlertRuleUpdated, Outcome: systemevent.OutcomeSuccess,
		Target: systemevent.Target{Type: "alert_rule"},
	})
	edited, err := repo.UpdateRule(updateCtx, installed.ID, alerting.RuleUpdate{Rule: editedInput, ExpectedUpdatedAt: installed.UpdatedAt})
	if err != nil {
		t.Fatal(err)
	}
	if edited.TemplateKey != template.TemplateKey {
		t.Fatalf("updated template key = %q", edited.TemplateKey)
	}

	template.ActionID = secondAction.ID
	reinstalled, created, err := repo.InstallRuleTemplate(auditCtx, alerting.RuleCreate{Rule: template})
	if err != nil || created {
		t.Fatalf("reinstall = %+v, created=%t, err=%v", reinstalled, created, err)
	}
	if reinstalled.ID != edited.ID || reinstalled.ActionID != firstAction.ID || reinstalled.Name != edited.Name || !reinstalled.Enabled {
		t.Fatalf("reinstall overwrote edited rule: %+v", reinstalled)
	}
	retentionTemplate := template
	retentionTemplate.TemplateKey = "request-log-retention-failed-v1"
	retentionTemplate.Name = "Request log retention failures"
	retentionTemplate.ActionID = secondAction.ID
	retentionTemplate.Category = systemevent.CategoryScheduler
	retentionTemplate.Severity = ""
	retentionTemplate.EventAction = systemevent.ActionSchedulerRequestLogRetentionFailed
	retentionTemplate.RecoveryAction = systemevent.ActionSchedulerRequestLogRetentionSucceeded
	retentionTemplate.AggregationCount = 1
	retentionTemplate.AggregationWindowSeconds = 0
	retentionTemplate.CooldownSeconds = 86400
	retentionTemplate.DeduplicationScope = alerting.DeduplicationScopeRule
	retentionRule, created, err := repo.InstallRuleTemplate(auditCtx, alerting.RuleCreate{Rule: retentionTemplate})
	if err != nil || !created || retentionRule.ID == edited.ID || retentionRule.TemplateKey != retentionTemplate.TemplateKey || retentionRule.ActionID != secondAction.ID {
		t.Fatalf("second template install = %+v, created=%t, err=%v", retentionRule, created, err)
	}
	autoTestTemplate := retentionTemplate
	autoTestTemplate.TemplateKey = "provider-auto-test-failed-v1"
	autoTestTemplate.Name = "Provider account auto-test failures"
	autoTestTemplate.ActionID = firstAction.ID
	autoTestTemplate.EventAction = systemevent.ActionSchedulerProviderAutoTestFailed
	autoTestTemplate.RecoveryAction = systemevent.ActionSchedulerProviderAutoTestCompleted
	autoTestTemplate.AggregationCount = 2
	autoTestTemplate.AggregationWindowSeconds = 900
	autoTestTemplate.CooldownSeconds = 3600
	autoTestTemplate.DeduplicationScope = alerting.DeduplicationScopeTarget
	autoTestRule, created, err := repo.InstallRuleTemplate(auditCtx, alerting.RuleCreate{Rule: autoTestTemplate})
	if err != nil || !created || autoTestRule.ID == edited.ID || autoTestRule.ID == retentionRule.ID || autoTestRule.TemplateKey != autoTestTemplate.TemplateKey || autoTestRule.ActionID != firstAction.ID {
		t.Fatalf("third template install = %+v, created=%t, err=%v", autoTestRule, created, err)
	}
	expiredTemplate := autoTestTemplate
	expiredTemplate.TemplateKey = "provider-account-expired-v1"
	expiredTemplate.Name = "Provider account expiry"
	expiredTemplate.ActionID = secondAction.ID
	expiredTemplate.Category = systemevent.CategoryRuntime
	expiredTemplate.Severity = systemevent.SeverityWarning
	expiredTemplate.EventAction = systemevent.ActionProviderAccountExpired
	expiredTemplate.RecoveryAction = systemevent.ActionProviderAccountRecovered
	expiredTemplate.AggregationCount = 1
	expiredTemplate.AggregationWindowSeconds = 0
	expiredTemplate.CooldownSeconds = 86400
	expiredRule, created, err := repo.InstallRuleTemplate(auditCtx, alerting.RuleCreate{Rule: expiredTemplate})
	if err != nil || !created || expiredRule.ID == edited.ID || expiredRule.ID == retentionRule.ID || expiredRule.ID == autoTestRule.ID || expiredRule.TemplateKey != expiredTemplate.TemplateKey || expiredRule.ActionID != secondAction.ID {
		t.Fatalf("fourth template install = %+v, created=%t, err=%v", expiredRule, created, err)
	}
	circuitTemplate := expiredTemplate
	circuitTemplate.TemplateKey = "provider-account-circuit-open-v1"
	circuitTemplate.Name = "Provider account circuit open"
	circuitTemplate.ActionID = firstAction.ID
	circuitTemplate.EventAction = systemevent.ActionProviderAccountCircuitOpened
	circuitTemplate.CooldownSeconds = 3600
	circuitRule, created, err := repo.InstallRuleTemplate(auditCtx, alerting.RuleCreate{Rule: circuitTemplate})
	if err != nil || !created || circuitRule.ID == edited.ID || circuitRule.ID == retentionRule.ID || circuitRule.ID == autoTestRule.ID || circuitRule.ID == expiredRule.ID || circuitRule.TemplateKey != circuitTemplate.TemplateKey || circuitRule.ActionID != firstAction.ID {
		t.Fatalf("fifth template install = %+v, created=%t, err=%v", circuitRule, created, err)
	}
	budget80Template := circuitTemplate
	budget80Template.TemplateKey = "api-key-budget-80-percent-v1"
	budget80Template.Name = "API key budget at 80 percent"
	budget80Template.ActionID = secondAction.ID
	budget80Template.EventAction = systemevent.ActionAPIKeyBudgetThreshold80Crossed
	budget80Template.RecoveryAction = systemevent.ActionAPIKeyBudgetThreshold80Recovered
	budget80Template.CooldownSeconds = 86400
	budget80Rule, created, err := repo.InstallRuleTemplate(auditCtx, alerting.RuleCreate{Rule: budget80Template})
	if err != nil || !created || budget80Rule.ID == edited.ID || budget80Rule.ID == retentionRule.ID || budget80Rule.ID == autoTestRule.ID || budget80Rule.ID == expiredRule.ID || budget80Rule.ID == circuitRule.ID || budget80Rule.TemplateKey != budget80Template.TemplateKey || budget80Rule.ActionID != secondAction.ID {
		t.Fatalf("sixth template install = %+v, created=%t, err=%v", budget80Rule, created, err)
	}
	budget100Template := budget80Template
	budget100Template.TemplateKey = "api-key-budget-100-percent-v1"
	budget100Template.Name = "API key budget exhausted"
	budget100Template.ActionID = firstAction.ID
	budget100Template.Severity = systemevent.SeverityError
	budget100Template.EventAction = systemevent.ActionAPIKeyBudgetThreshold100Crossed
	budget100Template.RecoveryAction = systemevent.ActionAPIKeyBudgetThreshold100Recovered
	budget100Template.CooldownSeconds = 3600
	budget100Rule, created, err := repo.InstallRuleTemplate(auditCtx, alerting.RuleCreate{Rule: budget100Template})
	if err != nil || !created || budget100Rule.ID == edited.ID || budget100Rule.ID == retentionRule.ID || budget100Rule.ID == autoTestRule.ID || budget100Rule.ID == expiredRule.ID || budget100Rule.ID == circuitRule.ID || budget100Rule.ID == budget80Rule.ID || budget100Rule.TemplateKey != budget100Template.TemplateKey || budget100Rule.ActionID != firstAction.ID {
		t.Fatalf("seventh template install = %+v, created=%t, err=%v", budget100Rule, created, err)
	}
	routingTemplate := budget100Template
	routingTemplate.TemplateKey = alerting.RoutingPoolExhaustedTemplateKey
	routingTemplate.Name = "API key routing pool exhausted"
	routingTemplate.ActionID = secondAction.ID
	routingTemplate.EventAction = systemevent.ActionAPIKeyRoutingPoolExhausted
	routingTemplate.RecoveryAction = systemevent.ActionAPIKeyRoutingPoolRecovered
	routingRule, created, err := repo.InstallRuleTemplate(auditCtx, alerting.RuleCreate{Rule: routingTemplate})
	if err != nil || !created || routingRule.ID == edited.ID || routingRule.ID == retentionRule.ID || routingRule.ID == autoTestRule.ID || routingRule.ID == expiredRule.ID || routingRule.ID == circuitRule.ID || routingRule.ID == budget80Rule.ID || routingRule.ID == budget100Rule.ID || routingRule.TemplateKey != routingTemplate.TemplateKey || routingRule.ActionID != secondAction.ID {
		t.Fatalf("eighth template install = %+v, created=%t, err=%v", routingRule, created, err)
	}

	var createdAudits int
	if err := repo.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM system_events
		WHERE action = $1 AND target_type = 'alert_rule' AND target_id = $2
	`, systemevent.ActionAlertRuleCreated, strconv.FormatInt(installed.ID, 10)).Scan(&createdAudits); err != nil {
		t.Fatal(err)
	}
	if createdAudits != 1 {
		t.Fatalf("created audit count = %d, want 1", createdAudits)
	}
}

func TestAlertingRepositorySerializesConcurrentRuleTemplateInstall(t *testing.T) {
	ctx := context.Background()
	repo := newTestAlertingRepository(t, ctx)
	action, err := repo.CreateAction(ctx, alerting.ActionCreate{
		Name: "concurrent template action", Kind: alerting.ActionKindGenericWebhook,
		EncryptedDestination: "encrypted-template", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	template := alerting.Rule{
		TemplateKey: "oauth-refresh-repeated-v1", Name: "Repeated OAuth refresh failures",
		ActionID: action.ID, Enabled: false, Category: systemevent.CategoryOAuth,
		Severity: systemevent.SeverityWarning, EventAction: systemevent.ActionOAuthRefreshAutomaticFailed,
		RecoveryAction:   systemevent.ActionOAuthRefreshAutomaticSucceeded,
		AggregationCount: 3, AggregationWindowSeconds: 900, CooldownSeconds: 3600,
		DeduplicationScope: alerting.DeduplicationScopeTarget, NotifyRecovery: true,
	}

	type result struct {
		rule    alerting.Rule
		created bool
		err     error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for range 2 {
		go func() {
			<-start
			rule, created, err := repo.InstallRuleTemplate(ctx, alerting.RuleCreate{Rule: template})
			results <- result{rule: rule, created: created, err: err}
		}()
	}
	close(start)
	first := <-results
	second := <-results
	if first.err != nil || second.err != nil || first.rule.ID == 0 || first.rule.ID != second.rule.ID {
		t.Fatalf("concurrent installs = %+v / %+v", first, second)
	}
	if first.created == second.created {
		t.Fatalf("concurrent created flags = %t/%t, want exactly one", first.created, second.created)
	}
	var count int
	if err := repo.pool.QueryRow(ctx, `SELECT count(*) FROM alert_rules WHERE template_key = $1`, template.TemplateKey).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("template rule count = %d, want 1", count)
	}
}

func TestAlertingRepositoryUpdateRuleLocksRuleBeforeTargetAction(t *testing.T) {
	ctx := context.Background()
	repo := newTestAlertingRepository(t, ctx)
	firstAction, err := repo.CreateAction(ctx, alerting.ActionCreate{
		Name: "first action", Kind: alerting.ActionKindGenericWebhook,
		EncryptedDestination: "encrypted-first", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	secondAction, err := repo.CreateAction(ctx, alerting.ActionCreate{
		Name: "second action", Kind: alerting.ActionKindGenericWebhook,
		EncryptedDestination: "encrypted-second", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ruleInput := alerting.Rule{
		Name: "lock order", ActionID: firstAction.ID, Enabled: true,
		Severity: systemevent.SeverityError, AggregationCount: 1, CooldownSeconds: 30,
		DeduplicationScope: alerting.DeduplicationScopeTarget,
	}
	rule, err := repo.CreateRule(ctx, alerting.RuleCreate{Rule: ruleInput})
	if err != nil {
		t.Fatal(err)
	}
	actionLock, err := repo.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer actionLock.Rollback(ctx)
	if _, err := actionLock.Exec(ctx, `SELECT id FROM alert_actions WHERE id = $1 FOR UPDATE`, secondAction.ID); err != nil {
		t.Fatal(err)
	}
	ruleInput.ActionID = secondAction.ID
	updateDone := make(chan error, 1)
	go func() {
		_, err := repo.UpdateRule(ctx, rule.ID, alerting.RuleUpdate{Rule: ruleInput, ExpectedUpdatedAt: rule.UpdatedAt})
		updateDone <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	lockedRuleObserved := false
	for time.Now().Before(deadline) {
		_, lockErr := repo.pool.Exec(ctx, `SELECT id FROM alert_rules WHERE id = $1 FOR UPDATE NOWAIT`, rule.ID)
		var pgErr *pgconn.PgError
		if errors.As(lockErr, &pgErr) && pgErr.Code == "55P03" {
			lockedRuleObserved = true
			break
		}
		if lockErr != nil {
			t.Fatalf("probe rule lock: %v", lockErr)
		}
		time.Sleep(time.Millisecond)
	}
	if !lockedRuleObserved {
		t.Fatal("UpdateRule did not lock rule before waiting for target action")
	}
	if err := actionLock.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-updateDone:
		if err != nil {
			t.Fatalf("UpdateRule returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("UpdateRule remained blocked")
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
