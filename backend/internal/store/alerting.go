package store

import (
	"context"
	"fmt"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/alerting"
	"github.com/KnowSky404/N2API/backend/internal/systemevent"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AlertingRepository struct {
	pool *pgxpool.Pool
}

func NewAlertingRepository(pool *pgxpool.Pool) *AlertingRepository {
	return &AlertingRepository{pool: pool}
}

func (r *AlertingRepository) CreateAction(ctx context.Context, input alerting.ActionCreate) (alerting.Action, error) {
	return scanAlertAction(r.pool.QueryRow(ctx, `
		INSERT INTO alert_actions (name, kind, encrypted_destination, enabled)
		VALUES ($1, $2, $3, $4)
		RETURNING id, name, kind, enabled, encrypted_destination <> '', created_at, updated_at
	`, input.Name, input.Kind, input.EncryptedDestination, input.Enabled))
}

func (r *AlertingRepository) UpdateAction(ctx context.Context, id int64, input alerting.ActionUpdate) (alerting.Action, error) {
	var destination any
	if input.EncryptedDestination != nil {
		destination = *input.EncryptedDestination
	}
	action, err := scanAlertAction(r.pool.QueryRow(ctx, `
		UPDATE alert_actions
		SET name = $2,
			kind = $3,
			encrypted_destination = COALESCE($4, encrypted_destination),
			enabled = $5,
			updated_at = now()
		WHERE id = $1
		RETURNING id, name, kind, enabled, encrypted_destination <> '', created_at, updated_at
	`, id, input.Name, input.Kind, destination, input.Enabled))
	if err == pgx.ErrNoRows {
		return alerting.Action{}, alerting.ErrNotFound
	}
	return action, err
}

func (r *AlertingRepository) DeleteAction(ctx context.Context, id int64) error {
	var deletedID int64
	err := r.pool.QueryRow(ctx, `DELETE FROM alert_actions WHERE id = $1 RETURNING id`, id).Scan(&deletedID)
	if err == pgx.ErrNoRows {
		return alerting.ErrNotFound
	}
	return err
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

const alertActionSelectSQL = `SELECT
	id, name, kind, enabled, encrypted_destination <> '', created_at, updated_at
FROM alert_actions`

func scanAlertAction(row rowScanner) (alerting.Action, error) {
	var action alerting.Action
	err := row.Scan(
		&action.ID, &action.Name, &action.Kind, &action.Enabled,
		&action.DestinationConfigured, &action.CreatedAt, &action.UpdatedAt,
	)
	if err != nil {
		return alerting.Action{}, err
	}
	return action, nil
}

func (r *AlertingRepository) CreateRule(ctx context.Context, input alerting.RuleCreate) (alerting.Rule, error) {
	rule := input.Rule
	return scanAlertRule(r.pool.QueryRow(ctx, `
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
}

func (r *AlertingRepository) UpdateRule(ctx context.Context, id int64, input alerting.RuleUpdate) (alerting.Rule, error) {
	rule := input.Rule
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return alerting.Rule{}, fmt.Errorf("begin alert rule update: %w", err)
	}
	defer tx.Rollback(ctx)

	updated, err := scanAlertRule(tx.QueryRow(ctx, `
		UPDATE alert_rules
		SET name = $2, action_id = $3, enabled = $4, category = $5, severity = $6,
			event_action = $7, recovery_action = $8, aggregation_count = $9,
			aggregation_window_seconds = $10, cooldown_seconds = $11,
			deduplication_scope = $12, notify_recovery = $13, updated_at = now()
		WHERE id = $1
		RETURNING `+alertRuleColumnsSQL,
		id, rule.Name, rule.ActionID, rule.Enabled, rule.Category, rule.Severity,
		rule.EventAction, rule.RecoveryAction, rule.AggregationCount,
		rule.AggregationWindowSeconds, rule.CooldownSeconds,
		rule.DeduplicationScope, rule.NotifyRecovery,
	))
	if err == pgx.ErrNoRows {
		return alerting.Rule{}, alerting.ErrNotFound
	}
	if err != nil {
		return alerting.Rule{}, fmt.Errorf("update alert rule: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM alert_rule_states WHERE rule_id = $1`, id); err != nil {
		return alerting.Rule{}, fmt.Errorf("reset alert rule states: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return alerting.Rule{}, fmt.Errorf("commit alert rule update: %w", err)
	}
	return updated, nil
}

func (r *AlertingRepository) DeleteRule(ctx context.Context, id int64) error {
	var deletedID int64
	err := r.pool.QueryRow(ctx, `DELETE FROM alert_rules WHERE id = $1 RETURNING id`, id).Scan(&deletedID)
	if err == pgx.ErrNoRows {
		return alerting.ErrNotFound
	}
	return err
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

const alertRuleColumnsSQL = `id, name, action_id, enabled, category, severity,
	event_action, recovery_action, aggregation_count, aggregation_window_seconds,
	cooldown_seconds, deduplication_scope, notify_recovery, created_at, updated_at`

func scanAlertRule(row rowScanner) (alerting.Rule, error) {
	var rule alerting.Rule
	err := row.Scan(
		&rule.ID, &rule.Name, &rule.ActionID, &rule.Enabled, &rule.Category, &rule.Severity,
		&rule.EventAction, &rule.RecoveryAction, &rule.AggregationCount,
		&rule.AggregationWindowSeconds, &rule.CooldownSeconds,
		&rule.DeduplicationScope, &rule.NotifyRecovery, &rule.CreatedAt, &rule.UpdatedAt,
	)
	if err != nil {
		return alerting.Rule{}, err
	}
	return rule, nil
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
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return alerting.RuleState{}, alerting.DecisionNone, fmt.Errorf("begin alert rule evaluation: %w", err)
	}
	defer tx.Rollback(ctx)

	rule, err := scanAlertRule(tx.QueryRow(ctx, `
		SELECT `+alertRuleColumnsSQL+`
		FROM alert_rules
		WHERE id = $1
		FOR UPDATE
	`, ruleID))
	if err == pgx.ErrNoRows {
		return alerting.RuleState{}, alerting.DecisionNone, alerting.ErrNotFound
	}
	if err != nil {
		return alerting.RuleState{}, alerting.DecisionNone, fmt.Errorf("lock alert rule for evaluation: %w", err)
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
		return alerting.RuleState{}, alerting.DecisionNone, fmt.Errorf("get alert rule state for evaluation: %w", err)
	}

	previous := state
	evaluated, decision, err := alerting.Evaluate(rule, state, event, now)
	if err != nil {
		return alerting.RuleState{}, alerting.DecisionNone, err
	}
	if evaluated == previous {
		return evaluated, decision, nil
	}
	if err := saveAlertRuleStateTx(ctx, tx, evaluated); err != nil {
		return alerting.RuleState{}, alerting.DecisionNone, err
	}
	if err := tx.Commit(ctx); err != nil {
		return alerting.RuleState{}, alerting.DecisionNone, fmt.Errorf("commit alert rule evaluation: %w", err)
	}
	return evaluated, decision, nil
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
