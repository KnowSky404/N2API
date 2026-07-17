package store

import (
	"context"
	"strconv"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/KnowSky404/N2API/backend/internal/systemevent"
	"github.com/jackc/pgx/v5"
)

func (r *AdminRepository) ListErrorPassthroughRules(ctx context.Context) ([]admin.ErrorPassthroughRule, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, pattern, match_type, description, enabled, priority, created_at, updated_at
		FROM error_passthrough_rules
		ORDER BY priority ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rules := []admin.ErrorPassthroughRule{}
	for rows.Next() {
		var rule admin.ErrorPassthroughRule
		if err := rows.Scan(&rule.ID, &rule.Pattern, &rule.MatchType, &rule.Description, &rule.Enabled, &rule.Priority, &rule.CreatedAt, &rule.UpdatedAt); err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (r *AdminRepository) CreateErrorPassthroughRule(ctx context.Context, input admin.ErrorPassthroughRuleInput) (admin.ErrorPassthroughRule, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return admin.ErrorPassthroughRule{}, err
	}
	defer tx.Rollback(ctx)
	var rule admin.ErrorPassthroughRule
	err = tx.QueryRow(ctx, `
		INSERT INTO error_passthrough_rules (pattern, match_type, description, enabled, priority)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, pattern, match_type, description, enabled, priority, created_at, updated_at
	`, input.Pattern, input.MatchType, input.Description, input.Enabled, input.Priority).Scan(
		&rule.ID, &rule.Pattern, &rule.MatchType, &rule.Description, &rule.Enabled, &rule.Priority, &rule.CreatedAt, &rule.UpdatedAt,
	)
	if err != nil {
		return admin.ErrorPassthroughRule{}, err
	}
	if err := insertIntentSystemEvent(ctx, tx, systemevent.Target{Type: "error_passthrough_rule", ID: strconv.FormatInt(rule.ID, 10)}, nil); err != nil {
		return admin.ErrorPassthroughRule{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.ErrorPassthroughRule{}, err
	}
	return rule, nil
}

func (r *AdminRepository) UpdateErrorPassthroughRule(ctx context.Context, id int64, input admin.ErrorPassthroughRuleInput) (admin.ErrorPassthroughRule, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return admin.ErrorPassthroughRule{}, err
	}
	defer tx.Rollback(ctx)
	var rule admin.ErrorPassthroughRule
	err = tx.QueryRow(ctx, `
		UPDATE error_passthrough_rules
		SET pattern = $2, match_type = $3, description = $4, enabled = $5, priority = $6, updated_at = now()
		WHERE id = $1
		RETURNING id, pattern, match_type, description, enabled, priority, created_at, updated_at
	`, id, input.Pattern, input.MatchType, input.Description, input.Enabled, input.Priority).Scan(
		&rule.ID, &rule.Pattern, &rule.MatchType, &rule.Description, &rule.Enabled, &rule.Priority, &rule.CreatedAt, &rule.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return admin.ErrorPassthroughRule{}, admin.ErrNotFound
		}
		return admin.ErrorPassthroughRule{}, err
	}
	if err := insertIntentSystemEvent(ctx, tx, systemevent.Target{Type: "error_passthrough_rule", ID: strconv.FormatInt(rule.ID, 10)}, nil); err != nil {
		return admin.ErrorPassthroughRule{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.ErrorPassthroughRule{}, err
	}
	return rule, nil
}

func (r *AdminRepository) DeleteErrorPassthroughRule(ctx context.Context, id int64) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var deletedID int64
	err = tx.QueryRow(ctx, `DELETE FROM error_passthrough_rules WHERE id = $1 RETURNING id`, id).Scan(&deletedID)
	if err == pgx.ErrNoRows {
		return admin.ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := insertIntentSystemEvent(ctx, tx, systemevent.Target{Type: "error_passthrough_rule", ID: strconv.FormatInt(deletedID, 10)}, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
