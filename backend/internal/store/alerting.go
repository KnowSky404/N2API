package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/alerting"
	"github.com/KnowSky404/N2API/backend/internal/systemevent"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AlertingRepository struct {
	pool *pgxpool.Pool
}

const alertRulesActionIDForeignKey = "alert_rules_action_id_fkey"

func NewAlertingRepository(pool *pgxpool.Pool) *AlertingRepository {
	return &AlertingRepository{pool: pool}
}

func (r *AlertingRepository) CreateAction(ctx context.Context, input alerting.ActionCreate) (alerting.Action, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return alerting.Action{}, fmt.Errorf("begin alert action create: %w", err)
	}
	defer tx.Rollback(ctx)
	action, err := scanAlertAction(tx.QueryRow(ctx, `
			INSERT INTO alert_actions (name, kind, encrypted_destination, enabled)
			VALUES ($1, $2, $3, $4)
			RETURNING `+alertActionColumnsSQL+`
		`, input.Name, input.Kind, input.EncryptedDestination, input.Enabled))
	if err != nil {
		return alerting.Action{}, fmt.Errorf("create alert action: %w", err)
	}
	if err := insertIntentSystemEvent(ctx, tx, alertActionTarget(action), nil); err != nil {
		return alerting.Action{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return alerting.Action{}, fmt.Errorf("commit alert action create: %w", err)
	}
	return action, nil
}

func (r *AlertingRepository) UpdateAction(ctx context.Context, id int64, input alerting.ActionUpdate) (alerting.Action, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return alerting.Action{}, fmt.Errorf("begin alert action update: %w", err)
	}
	defer tx.Rollback(ctx)
	var destination any
	if input.EncryptedDestination != nil {
		destination = *input.EncryptedDestination
	}
	action, err := scanAlertAction(tx.QueryRow(ctx, `
			UPDATE alert_actions
		SET name = $2,
			kind = $3,
			encrypted_destination = COALESCE($4, encrypted_destination),
			enabled = $5,
					updated_at = GREATEST(clock_timestamp(), updated_at + interval '1 microsecond')
			WHERE id = $1 AND updated_at = $6
			RETURNING `+alertActionColumnsSQL+`
		`, id, input.Name, input.Kind, destination, input.Enabled, input.ExpectedUpdatedAt))
	if err == pgx.ErrNoRows {
		return alerting.Action{}, staleOrMissingAlertAction(ctx, tx, id)
	}
	if err != nil {
		return alerting.Action{}, fmt.Errorf("update alert action: %w", err)
	}
	if err := insertIntentSystemEvent(ctx, tx, alertActionTarget(action), nil); err != nil {
		return alerting.Action{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return alerting.Action{}, fmt.Errorf("commit alert action update: %w", err)
	}
	return action, nil
}

func (r *AlertingRepository) DeleteAction(ctx context.Context, id int64) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin alert action delete: %w", err)
	}
	defer tx.Rollback(ctx)
	var deletedID int64
	var name string
	err = tx.QueryRow(ctx, `DELETE FROM alert_actions WHERE id = $1 RETURNING id, name`, id).Scan(&deletedID, &name)
	if err == pgx.ErrNoRows {
		return alerting.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == "23001" || pgErr.Code == "23503") && pgErr.ConstraintName == alertRulesActionIDForeignKey {
		return alerting.ErrConflict
	}
	if err != nil {
		return fmt.Errorf("delete alert action: %w", err)
	}
	target := systemevent.Target{Type: "alert_action", ID: strconv.FormatInt(deletedID, 10), Name: name}
	if err := insertIntentSystemEvent(ctx, tx, target, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *AlertingRepository) GetAction(ctx context.Context, id int64) (alerting.Action, error) {
	action, err := scanAlertAction(r.pool.QueryRow(ctx, alertActionSelectSQL+` WHERE id = $1`, id))
	if err == pgx.ErrNoRows {
		return alerting.Action{}, alerting.ErrNotFound
	}
	return action, err
}

func (r *AlertingRepository) ListActions(ctx context.Context) ([]alerting.Action, error) {
	rows, err := r.pool.Query(ctx, alertActionSelectSQL+` ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list alert actions: %w", err)
	}
	defer rows.Close()

	actions := make([]alerting.Action, 0)
	for rows.Next() {
		action, err := scanAlertAction(rows)
		if err != nil {
			return nil, err
		}
		actions = append(actions, action)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list alert actions: %w", err)
	}
	return actions, nil
}

func (r *AlertingRepository) GetEncryptedDestination(ctx context.Context, id int64) (string, error) {
	var destination string
	err := r.pool.QueryRow(ctx, `SELECT encrypted_destination FROM alert_actions WHERE id = $1`, id).Scan(&destination)
	if err == pgx.ErrNoRows {
		return "", alerting.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get encrypted alert destination: %w", err)
	}
	return destination, nil
}

func (r *AlertingRepository) GetActionForDelivery(ctx context.Context, id int64) (alerting.ActionForDelivery, error) {
	var action alerting.ActionForDelivery
	err := r.pool.QueryRow(ctx, `
		SELECT id, name, kind, enabled, encrypted_destination, updated_at
			FROM alert_actions
			WHERE id = $1
		`, id).Scan(&action.ID, &action.Name, &action.Kind, &action.Enabled, &action.EncryptedDestination, &action.UpdatedAt)
	if err == pgx.ErrNoRows {
		return alerting.ActionForDelivery{}, alerting.ErrNotFound
	}
	if err != nil {
		return alerting.ActionForDelivery{}, fmt.Errorf("get alert action for delivery: %w", err)
	}
	return action, nil
}

const alertActionColumnsSQL = `id, name, kind, enabled, encrypted_destination <> '',
	CASE WHEN last_test_config_updated_at = updated_at THEN last_tested_at END,
	CASE WHEN last_test_config_updated_at = updated_at THEN last_test_status ELSE '' END,
	CASE WHEN last_test_config_updated_at = updated_at THEN last_test_http_status END,
	CASE WHEN last_test_config_updated_at = updated_at THEN last_test_latency_ms ELSE 0 END,
	CASE WHEN last_test_config_updated_at = updated_at THEN last_test_error_code ELSE '' END,
	CASE WHEN last_test_config_updated_at = updated_at THEN last_test_retryable ELSE false END,
	created_at, updated_at`

const alertActionSelectSQL = `SELECT ` + alertActionColumnsSQL + ` FROM alert_actions`

func scanAlertAction(row rowScanner) (alerting.Action, error) {
	var action alerting.Action
	err := row.Scan(
		&action.ID, &action.Name, &action.Kind, &action.Enabled,
		&action.DestinationConfigured, &action.LastTestedAt, &action.LastTestStatus,
		&action.LastTestHTTPStatus, &action.LastTestLatencyMS, &action.LastTestErrorCode,
		&action.LastTestRetryable, &action.CreatedAt, &action.UpdatedAt,
	)
	if err != nil {
		return alerting.Action{}, err
	}
	return action, nil
}

func (r *AlertingRepository) BeginActionTest(ctx context.Context, id int64, expectedUpdatedAt time.Time, attemptToken string) (alerting.ActionTestStart, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return alerting.ActionTestStart{}, fmt.Errorf("begin alert action test admission: %w", err)
	}
	defer tx.Rollback(ctx)
	var action alerting.ActionForDelivery
	var retryAfterMicroseconds int64
	err = tx.QueryRow(ctx, `
		SELECT id, name, kind, enabled, encrypted_destination, updated_at,
			CASE WHEN last_test_started_at IS NULL THEN 0 ELSE
				GREATEST(CEIL(EXTRACT(EPOCH FROM (
					last_test_started_at + make_interval(secs => $2) - clock_timestamp()
				)) * 1000000), 0)::bigint
			END
		FROM alert_actions
		WHERE id = $1
		FOR UPDATE
	`, id, alerting.ActionTestAdmissionWindow.Seconds()).Scan(
		&action.ID, &action.Name, &action.Kind, &action.Enabled,
		&action.EncryptedDestination, &action.UpdatedAt, &retryAfterMicroseconds,
	)
	if err == pgx.ErrNoRows {
		return alerting.ActionTestStart{}, alerting.ErrNotFound
	}
	if err != nil {
		return alerting.ActionTestStart{}, fmt.Errorf("lock alert action for test admission: %w", err)
	}
	if !action.UpdatedAt.Equal(expectedUpdatedAt) {
		return alerting.ActionTestStart{}, alerting.ErrConflict
	}
	if retryAfterMicroseconds > 0 {
		return alerting.ActionTestStart{}, &alerting.RateLimitError{RetryAfter: time.Duration(retryAfterMicroseconds) * time.Microsecond}
	}
	var startedAt time.Time
	if err := tx.QueryRow(ctx, `
		UPDATE alert_actions
		SET last_test_started_at = clock_timestamp(), last_test_attempt_token = $2,
			last_test_attempt_config_updated_at = updated_at
		WHERE id = $1
		RETURNING last_test_started_at
	`, id, attemptToken).Scan(&startedAt); err != nil {
		return alerting.ActionTestStart{}, fmt.Errorf("admit alert action test: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return alerting.ActionTestStart{}, fmt.Errorf("commit alert action test admission: %w", err)
	}
	return alerting.ActionTestStart{Action: action, AttemptToken: attemptToken, StartedAt: startedAt}, nil
}

func (r *AlertingRepository) FinalizeActionTest(ctx context.Context, id int64, attemptToken string, result alerting.ActionTestResult) (alerting.Action, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return alerting.Action{}, fmt.Errorf("begin alert action test finalize: %w", err)
	}
	defer tx.Rollback(ctx)
	action, err := scanAlertAction(tx.QueryRow(ctx, `
			UPDATE alert_actions
			SET last_tested_at = $3, last_test_status = $4, last_test_http_status = $5,
				last_test_latency_ms = $6, last_test_error_code = $7, last_test_retryable = $8,
				last_test_config_updated_at = last_test_attempt_config_updated_at,
				last_test_attempt_token = '', last_test_attempt_config_updated_at = NULL
			WHERE id = $1 AND last_test_attempt_token = $2
			RETURNING `+alertActionColumnsSQL,
		id, attemptToken, result.TestedAt.UTC(), result.Status, result.HTTPStatus,
		result.LatencyMS, result.ErrorCode, result.Retryable,
	))
	if err == pgx.ErrNoRows {
		return alerting.Action{}, staleOrMissingAlertAction(ctx, tx, id)
	}
	if err != nil {
		return alerting.Action{}, fmt.Errorf("finalize alert action test: %w", err)
	}
	if err := insertIntentSystemEvent(ctx, tx, alertActionTarget(action), nil); err != nil {
		return alerting.Action{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return alerting.Action{}, fmt.Errorf("commit alert action test finalize: %w", err)
	}
	return action, nil
}

func alertActionTarget(action alerting.Action) systemevent.Target {
	return systemevent.Target{Type: "alert_action", ID: strconv.FormatInt(action.ID, 10), Name: action.Name}
}

func staleOrMissingAlertAction(ctx context.Context, tx pgx.Tx, id int64) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM alert_actions WHERE id = $1)`, id).Scan(&exists); err != nil {
		return fmt.Errorf("check alert action existence: %w", err)
	}
	if exists {
		return alerting.ErrConflict
	}
	return alerting.ErrNotFound
}

func (r *AlertingRepository) CreateRule(ctx context.Context, input alerting.RuleCreate) (alerting.Rule, error) {
	rule := input.Rule
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return alerting.Rule{}, fmt.Errorf("begin alert rule create: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := lockAlertAction(ctx, tx, rule.ActionID); err != nil {
		return alerting.Rule{}, err
	}
	created, err := scanAlertRule(tx.QueryRow(ctx, `
			INSERT INTO alert_rules (
			name, action_id, enabled, category, severity, event_action, recovery_action,
			aggregation_count, aggregation_window_seconds, cooldown_seconds,
			deduplication_scope, notify_recovery
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING `+alertRuleColumnsSQL,
		rule.Name, rule.ActionID, rule.Enabled, rule.Category, rule.Severity,
		rule.EventAction, rule.RecoveryAction, rule.AggregationCount,
		rule.AggregationWindowSeconds, rule.CooldownSeconds,
		rule.DeduplicationScope, rule.NotifyRecovery,
	))
	if err != nil {
		return alerting.Rule{}, fmt.Errorf("create alert rule: %w", err)
	}
	if err := insertIntentSystemEvent(ctx, tx, alertRuleTarget(created), nil); err != nil {
		return alerting.Rule{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return alerting.Rule{}, fmt.Errorf("commit alert rule create: %w", err)
	}
	return created, nil
}

func (r *AlertingRepository) InstallRuleTemplate(ctx context.Context, input alerting.RuleCreate) (alerting.Rule, bool, error) {
	rule := input.Rule
	if rule.TemplateKey == "" {
		return alerting.Rule{}, false, alerting.ErrInvalidInput
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return alerting.Rule{}, false, fmt.Errorf("begin alert rule template install: %w", err)
	}
	defer tx.Rollback(ctx)

	existing, err := scanAlertRule(tx.QueryRow(ctx, `
		SELECT `+alertRuleColumnsSQL+`
		FROM alert_rules
		WHERE template_key = $1
		FOR UPDATE
	`, rule.TemplateKey))
	if err == nil {
		return existing, false, nil
	}
	if err != pgx.ErrNoRows {
		return alerting.Rule{}, false, fmt.Errorf("find installed alert rule template: %w", err)
	}
	if err := lockAlertAction(ctx, tx, rule.ActionID); err != nil {
		return alerting.Rule{}, false, err
	}

	created, err := scanAlertRule(tx.QueryRow(ctx, `
		INSERT INTO alert_rules (
			template_key, name, action_id, enabled, category, severity, event_action,
			recovery_action, aggregation_count, aggregation_window_seconds,
			cooldown_seconds, deduplication_scope, notify_recovery
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (template_key) WHERE template_key <> '' DO NOTHING
		RETURNING `+alertRuleColumnsSQL,
		rule.TemplateKey, rule.Name, rule.ActionID, rule.Enabled, rule.Category,
		rule.Severity, rule.EventAction, rule.RecoveryAction, rule.AggregationCount,
		rule.AggregationWindowSeconds, rule.CooldownSeconds,
		rule.DeduplicationScope, rule.NotifyRecovery,
	))
	if err == pgx.ErrNoRows {
		existing, err := scanAlertRule(tx.QueryRow(ctx, `
			SELECT `+alertRuleColumnsSQL+`
			FROM alert_rules
			WHERE template_key = $1
			FOR UPDATE
		`, rule.TemplateKey))
		if err != nil {
			return alerting.Rule{}, false, fmt.Errorf("load concurrently installed alert rule template: %w", err)
		}
		return existing, false, nil
	}
	if err != nil {
		return alerting.Rule{}, false, fmt.Errorf("install alert rule template: %w", err)
	}
	if err := insertIntentSystemEvent(ctx, tx, alertRuleTarget(created), nil); err != nil {
		return alerting.Rule{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return alerting.Rule{}, false, fmt.Errorf("commit alert rule template install: %w", err)
	}
	return created, true, nil
}

func (r *AlertingRepository) UpdateRule(ctx context.Context, id int64, input alerting.RuleUpdate) (alerting.Rule, error) {
	rule := input.Rule
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return alerting.Rule{}, fmt.Errorf("begin alert rule update: %w", err)
	}
	defer tx.Rollback(ctx)
	var currentRevision time.Time
	if err := tx.QueryRow(ctx, `SELECT updated_at FROM alert_rules WHERE id = $1 FOR UPDATE`, id).Scan(&currentRevision); err == pgx.ErrNoRows {
		return alerting.Rule{}, alerting.ErrNotFound
	} else if err != nil {
		return alerting.Rule{}, fmt.Errorf("lock alert rule for update: %w", err)
	}
	if !currentRevision.Equal(input.ExpectedUpdatedAt) {
		return alerting.Rule{}, alerting.ErrConflict
	}
	if err := lockAlertAction(ctx, tx, rule.ActionID); err != nil {
		return alerting.Rule{}, err
	}

	updated, err := scanAlertRule(tx.QueryRow(ctx, `
		UPDATE alert_rules
		SET name = $2, action_id = $3, enabled = $4, category = $5, severity = $6,
			event_action = $7, recovery_action = $8, aggregation_count = $9,
			aggregation_window_seconds = $10, cooldown_seconds = $11,
				deduplication_scope = $12, notify_recovery = $13,
				updated_at = GREATEST(clock_timestamp(), updated_at + interval '1 microsecond')
			WHERE id = $1
		RETURNING `+alertRuleColumnsSQL,
		id, rule.Name, rule.ActionID, rule.Enabled, rule.Category, rule.Severity,
		rule.EventAction, rule.RecoveryAction, rule.AggregationCount,
		rule.AggregationWindowSeconds, rule.CooldownSeconds,
		rule.DeduplicationScope, rule.NotifyRecovery,
	))
	if err != nil {
		return alerting.Rule{}, fmt.Errorf("update alert rule: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM alert_rule_states WHERE rule_id = $1`, id); err != nil {
		return alerting.Rule{}, fmt.Errorf("reset alert rule states: %w", err)
	}
	if err := insertIntentSystemEvent(ctx, tx, alertRuleTarget(updated), nil); err != nil {
		return alerting.Rule{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return alerting.Rule{}, fmt.Errorf("commit alert rule update: %w", err)
	}
	return updated, nil
}

func (r *AlertingRepository) DeleteRule(ctx context.Context, id int64) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin alert rule delete: %w", err)
	}
	defer tx.Rollback(ctx)
	var deletedID int64
	var name string
	err = tx.QueryRow(ctx, `DELETE FROM alert_rules WHERE id = $1 RETURNING id, name`, id).Scan(&deletedID, &name)
	if err == pgx.ErrNoRows {
		return alerting.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("delete alert rule: %w", err)
	}
	target := systemevent.Target{Type: "alert_rule", ID: strconv.FormatInt(deletedID, 10), Name: name}
	if err := insertIntentSystemEvent(ctx, tx, target, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *AlertingRepository) GetRule(ctx context.Context, id int64) (alerting.Rule, error) {
	rule, err := scanAlertRule(r.pool.QueryRow(ctx, `SELECT `+alertRuleColumnsSQL+` FROM alert_rules WHERE id = $1`, id))
	if err == pgx.ErrNoRows {
		return alerting.Rule{}, alerting.ErrNotFound
	}
	return rule, err
}

func (r *AlertingRepository) ListRules(ctx context.Context) ([]alerting.Rule, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+alertRuleColumnsSQL+` FROM alert_rules ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list alert rules: %w", err)
	}
	defer rows.Close()

	rules := make([]alerting.Rule, 0)
	for rows.Next() {
		rule, err := scanAlertRule(rows)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list alert rules: %w", err)
	}
	return rules, nil
}

const alertRuleColumnsSQL = `id, template_key, name, action_id, enabled, category, severity,
	event_action, recovery_action, aggregation_count, aggregation_window_seconds,
	cooldown_seconds, deduplication_scope, notify_recovery, created_at, updated_at`

func scanAlertRule(row rowScanner) (alerting.Rule, error) {
	var rule alerting.Rule
	err := row.Scan(
		&rule.ID, &rule.TemplateKey, &rule.Name, &rule.ActionID, &rule.Enabled, &rule.Category, &rule.Severity,
		&rule.EventAction, &rule.RecoveryAction, &rule.AggregationCount,
		&rule.AggregationWindowSeconds, &rule.CooldownSeconds,
		&rule.DeduplicationScope, &rule.NotifyRecovery, &rule.CreatedAt, &rule.UpdatedAt,
	)
	if err != nil {
		return alerting.Rule{}, err
	}
	return rule, nil
}

func lockAlertAction(ctx context.Context, tx pgx.Tx, id int64) error {
	var actionID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM alert_actions WHERE id = $1 FOR KEY SHARE`, id).Scan(&actionID); err == pgx.ErrNoRows {
		return alerting.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("lock alert action: %w", err)
	}
	return nil
}

func alertRuleTarget(rule alerting.Rule) systemevent.Target {
	return systemevent.Target{Type: "alert_rule", ID: strconv.FormatInt(rule.ID, 10), Name: rule.Name}
}

func (r *AlertingRepository) GetRuleState(ctx context.Context, ruleID int64, hash string) (alerting.RuleState, error) {
	state, err := scanAlertRuleState(r.pool.QueryRow(ctx, `
		SELECT `+alertRuleStateColumnsSQL+`
		FROM alert_rule_states
		WHERE rule_id = $1 AND deduplication_key_hash = $2
	`, ruleID, hash))
	if err == pgx.ErrNoRows {
		return alerting.RuleState{}, alerting.ErrNotFound
	}
	return state, err
}

func (r *AlertingRepository) SaveRuleState(ctx context.Context, state alerting.RuleState) error {
	if state.UpdatedAt.IsZero() {
		state.UpdatedAt = time.Now().UTC()
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin alert state save: %w", err)
	}
	defer tx.Rollback(ctx)

	var ruleID int64
	err = tx.QueryRow(ctx, `SELECT id FROM alert_rules WHERE id = $1 FOR UPDATE`, state.RuleID).Scan(&ruleID)
	if err == pgx.ErrNoRows {
		return alerting.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock alert rule: %w", err)
	}

	if err := saveAlertRuleStateTx(ctx, tx, state); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit alert state save: %w", err)
	}
	return nil
}

func (r *AlertingRepository) EvaluateRuleEvent(ctx context.Context, ruleID int64, event systemevent.Event, now time.Time) (alerting.RuleState, alerting.Decision, error) {
	evaluation, err := r.evaluateRuleEvent(ctx, ruleID, event, now, false)
	return evaluation.State, evaluation.Decision, err
}

func (r *AlertingRepository) EvaluateRuleEventForDelivery(ctx context.Context, ruleID int64, event systemevent.Event, now time.Time) (alerting.Evaluation, error) {
	return r.evaluateRuleEvent(ctx, ruleID, event, now, true)
}

func (r *AlertingRepository) evaluateRuleEvent(ctx context.Context, ruleID int64, event systemevent.Event, now time.Time, requireEnabledAction bool) (alerting.Evaluation, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return alerting.Evaluation{}, fmt.Errorf("begin alert rule evaluation: %w", err)
	}
	defer tx.Rollback(ctx)

	rule, err := scanAlertRule(tx.QueryRow(ctx, `
		SELECT `+alertRuleColumnsSQL+`
		FROM alert_rules
		WHERE id = $1
		FOR UPDATE
	`, ruleID))
	if err == pgx.ErrNoRows {
		return alerting.Evaluation{}, alerting.ErrNotFound
	}
	if err != nil {
		return alerting.Evaluation{}, fmt.Errorf("lock alert rule for evaluation: %w", err)
	}

	evaluation := alerting.Evaluation{Rule: rule, ActionEnabled: true, Decision: alerting.DecisionNone}
	if requireEnabledAction {
		if err := tx.QueryRow(ctx, `SELECT enabled, updated_at FROM alert_actions WHERE id = $1 FOR UPDATE`, rule.ActionID).Scan(&evaluation.ActionEnabled, &evaluation.ActionUpdatedAt); err == pgx.ErrNoRows {
			return alerting.Evaluation{}, alerting.ErrNotFound
		} else if err != nil {
			return alerting.Evaluation{}, fmt.Errorf("lock alert action for evaluation: %w", err)
		}
		if !evaluation.ActionEnabled {
			return evaluation, nil
		}
	}

	hash := rule.DeduplicationKeyHash(event)
	state, err := scanAlertRuleState(tx.QueryRow(ctx, `
		SELECT `+alertRuleStateColumnsSQL+`
		FROM alert_rule_states
		WHERE rule_id = $1 AND deduplication_key_hash = $2
	`, rule.ID, hash))
	if err == pgx.ErrNoRows {
		state = alerting.RuleState{
			RuleID: rule.ID, DeduplicationKeyHash: hash, Phase: alerting.StatePhaseIdle,
		}
	} else if err != nil {
		return alerting.Evaluation{}, fmt.Errorf("get alert rule state for evaluation: %w", err)
	}

	previous := state
	evaluated, decision, err := alerting.Evaluate(rule, state, event, now)
	if err != nil {
		return alerting.Evaluation{}, err
	}
	evaluation.State = evaluated
	evaluation.Decision = decision
	if evaluated == previous {
		return evaluation, nil
	}
	if err := saveAlertRuleStateTx(ctx, tx, evaluated); err != nil {
		return alerting.Evaluation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return alerting.Evaluation{}, fmt.Errorf("commit alert rule evaluation: %w", err)
	}
	return evaluation, nil
}

func saveAlertRuleStateTx(ctx context.Context, tx pgx.Tx, state alerting.RuleState) error {
	tag, err := tx.Exec(ctx, `
		UPDATE alert_rule_states
		SET phase = $3, window_started_at = $4, window_match_count = $5,
			cooldown_until = $6, last_matched_at = $7, last_notified_at = $8,
			last_recovered_at = $9, updated_at = $10
		WHERE rule_id = $1 AND deduplication_key_hash = $2
	`, state.RuleID, state.DeduplicationKeyHash, state.Phase, state.WindowStartedAt,
		state.WindowMatchCount, state.CooldownUntil, state.LastMatchedAt,
		state.LastNotifiedAt, state.LastRecoveredAt, state.UpdatedAt.UTC())
	if err != nil {
		return fmt.Errorf("update alert rule state: %w", err)
	}
	if tag.RowsAffected() == 0 {
		if err := ensureAlertStateCapacity(ctx, tx, state.RuleID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO alert_rule_states (
				rule_id, deduplication_key_hash, phase, window_started_at,
				window_match_count, cooldown_until, last_matched_at,
				last_notified_at, last_recovered_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		`, state.RuleID, state.DeduplicationKeyHash, state.Phase, state.WindowStartedAt,
			state.WindowMatchCount, state.CooldownUntil, state.LastMatchedAt,
			state.LastNotifiedAt, state.LastRecoveredAt, state.UpdatedAt.UTC())
		if err != nil {
			return fmt.Errorf("insert alert rule state: %w", err)
		}
	}
	return nil
}

func ensureAlertStateCapacity(ctx context.Context, tx pgx.Tx, ruleID int64) error {
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM alert_rule_states WHERE rule_id = $1`, ruleID).Scan(&count); err != nil {
		return fmt.Errorf("count alert rule states: %w", err)
	}
	if count < alerting.MaxRuleStatesPerRule {
		return nil
	}

	var evictedHash string
	err := tx.QueryRow(ctx, `
		DELETE FROM alert_rule_states
		WHERE (rule_id, deduplication_key_hash) = (
			SELECT rule_id, deduplication_key_hash
			FROM alert_rule_states
			WHERE rule_id = $1 AND phase = 'idle'
			ORDER BY updated_at ASC, deduplication_key_hash ASC
			LIMIT 1
		)
		RETURNING deduplication_key_hash
	`, ruleID).Scan(&evictedHash)
	if err == pgx.ErrNoRows {
		return alerting.ErrStateCapacity
	}
	if err != nil {
		return fmt.Errorf("evict idle alert rule state: %w", err)
	}
	return nil
}

const alertRuleStateColumnsSQL = `rule_id, deduplication_key_hash, phase,
	window_started_at, window_match_count, cooldown_until, last_matched_at,
	last_notified_at, last_recovered_at, updated_at`

func scanAlertRuleState(row rowScanner) (alerting.RuleState, error) {
	var state alerting.RuleState
	err := row.Scan(
		&state.RuleID, &state.DeduplicationKeyHash, &state.Phase,
		&state.WindowStartedAt, &state.WindowMatchCount, &state.CooldownUntil,
		&state.LastMatchedAt, &state.LastNotifiedAt, &state.LastRecoveredAt, &state.UpdatedAt,
	)
	if err != nil {
		return alerting.RuleState{}, err
	}
	return state, nil
}

var _ alerting.Repository = (*AlertingRepository)(nil)
