package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/KnowSky404/N2API/backend/internal/systemevent"
	"github.com/jackc/pgx/v5"
)

const maxAPIKeyBudgetMonitorBatchSize = 100

type apiKeyBudgetSnapshot struct {
	ID                    int64
	Name                  string
	RequestBudget24h      int64
	TokenBudget24h        int64
	CostBudgetMicrousd24h int64
	RequestBudget30d      int64
	TokenBudget30d        int64
	CostBudgetMicrousd30d int64
}

type apiKeyBudgetStream struct {
	Kind   string
	Window string
	Used   int64
	Limit  int64
}

type apiKeyBudgetThresholdKey struct {
	Kind      string
	Window    string
	Threshold int
}

func (r *AdminRepository) RunAPIKeyBudgetMonitorCycle(ctx context.Context, afterID int64, limit int, now time.Time) (admin.APIKeyBudgetMonitorCycleResult, error) {
	if afterID < 0 || limit <= 0 || limit > maxAPIKeyBudgetMonitorBatchSize || now.IsZero() {
		return admin.APIKeyBudgetMonitorCycleResult{}, admin.ErrInvalidInput
	}
	rows, err := r.pool.Query(ctx, `
		SELECT k.id
		FROM client_api_keys k
		WHERE k.id > $1
			AND k.revoked_at IS NULL
			AND (
				k.request_budget_24h > 0 OR k.token_budget_24h > 0 OR k.cost_budget_microusd_24h > 0 OR
				k.request_budget_30d > 0 OR k.token_budget_30d > 0 OR k.cost_budget_microusd_30d > 0 OR
				EXISTS (
					SELECT 1 FROM api_key_budget_threshold_states state WHERE state.client_key_id = k.id
				)
			)
		ORDER BY k.id ASC
		LIMIT $2
	`, afterID, limit+1)
	if err != nil {
		return admin.APIKeyBudgetMonitorCycleResult{}, err
	}
	ids := make([]int64, 0, limit+1)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return admin.APIKeyBudgetMonitorCycleResult{}, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return admin.APIKeyBudgetMonitorCycleResult{}, err
	}
	rows.Close()

	result := admin.APIKeyBudgetMonitorCycleResult{}
	if len(ids) > limit {
		ids = ids[:limit]
		result.NextAfterID = ids[len(ids)-1]
	}
	for _, id := range ids {
		transitions, err := r.evaluateAPIKeyBudgetThresholds(ctx, id, now.UTC())
		if err != nil {
			return result, err
		}
		result.Processed++
		result.Transitions += transitions
	}
	return result, nil
}

func (r *AdminRepository) evaluateAPIKeyBudgetThresholds(ctx context.Context, keyID int64, now time.Time) (int, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	snapshot, err := loadAPIKeyBudgetSnapshotForUpdate(ctx, tx, keyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	usage, err := loadAPIKeyBudgetUsageTx(ctx, tx, keyID, now)
	if err != nil {
		return 0, err
	}
	current, err := loadAPIKeyBudgetThresholdStatesTx(ctx, tx, keyID)
	if err != nil {
		return 0, err
	}
	streams := apiKeyBudgetStreams(snapshot, usage)
	transitions := 0
	for _, stream := range streams {
		for _, threshold := range []int{80, 100} {
			stateKey := apiKeyBudgetThresholdKey{Kind: stream.Kind, Window: stream.Window, Threshold: threshold}
			if !apiKeyBudgetThresholdCrossed(stream.Used, stream.Limit, threshold) {
				continue
			}
			if _, exists := current[stateKey]; exists {
				continue
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO api_key_budget_threshold_states (
					client_key_id, budget_kind, window_name, threshold_percent, crossed_at, updated_at
				) VALUES ($1, $2, $3, $4, $5, $5)
			`, keyID, stream.Kind, stream.Window, threshold, now); err != nil {
				return 0, err
			}
			event, err := buildAPIKeyBudgetThresholdEvent(ctx, snapshot, stream, threshold, true, now, "")
			if err != nil {
				return 0, err
			}
			if err := InsertSystemEventTx(ctx, tx, event); err != nil {
				return 0, err
			}
			transitions++
		}
	}
	for _, stream := range streams {
		for _, threshold := range []int{100, 80} {
			stateKey := apiKeyBudgetThresholdKey{Kind: stream.Kind, Window: stream.Window, Threshold: threshold}
			if _, exists := current[stateKey]; !exists || apiKeyBudgetThresholdCrossed(stream.Used, stream.Limit, threshold) {
				continue
			}
			if _, err := tx.Exec(ctx, `
				DELETE FROM api_key_budget_threshold_states
				WHERE client_key_id = $1 AND budget_kind = $2 AND window_name = $3 AND threshold_percent = $4
			`, keyID, stream.Kind, stream.Window, threshold); err != nil {
				return 0, err
			}
			event, err := buildAPIKeyBudgetThresholdEvent(ctx, snapshot, stream, threshold, false, now, "")
			if err != nil {
				return 0, err
			}
			if err := InsertSystemEventTx(ctx, tx, event); err != nil {
				return 0, err
			}
			transitions++
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return transitions, nil
}

func loadAPIKeyBudgetSnapshotForUpdate(ctx context.Context, tx pgx.Tx, keyID int64) (apiKeyBudgetSnapshot, error) {
	var snapshot apiKeyBudgetSnapshot
	err := tx.QueryRow(ctx, `
		SELECT id, name,
			request_budget_24h, token_budget_24h, cost_budget_microusd_24h,
			request_budget_30d, token_budget_30d, cost_budget_microusd_30d
		FROM client_api_keys
		WHERE id = $1 AND revoked_at IS NULL
		FOR UPDATE
	`, keyID).Scan(
		&snapshot.ID, &snapshot.Name,
		&snapshot.RequestBudget24h, &snapshot.TokenBudget24h, &snapshot.CostBudgetMicrousd24h,
		&snapshot.RequestBudget30d, &snapshot.TokenBudget30d, &snapshot.CostBudgetMicrousd30d,
	)
	return snapshot, err
}

func loadAPIKeyBudgetUsageTx(ctx context.Context, tx pgx.Tx, keyID int64, now time.Time) (admin.APIKeyBudgetUsage, error) {
	usage := admin.APIKeyBudgetUsage{KeyID: keyID}
	err := tx.QueryRow(ctx, `
		SELECT
			COALESCE(COUNT(*) FILTER (WHERE created_at >= $2), 0),
			COALESCE(SUM(total_tokens) FILTER (WHERE created_at >= $2), 0),
			COALESCE(SUM(estimated_cost_microusd) FILTER (WHERE created_at >= $2), 0),
			COALESCE(COUNT(*) FILTER (WHERE created_at >= $3), 0),
			COALESCE(SUM(total_tokens) FILTER (WHERE created_at >= $3), 0),
			COALESCE(SUM(estimated_cost_microusd) FILTER (WHERE created_at >= $3), 0)
		FROM request_logs
		WHERE client_key_id = $1 AND created_at >= $3
	`, keyID, now.Add(-24*time.Hour), now.Add(-30*24*time.Hour)).Scan(
		&usage.RequestsUsed24h, &usage.TokensUsed24h, &usage.CostMicrousd24h,
		&usage.RequestsUsed30d, &usage.TokensUsed30d, &usage.CostMicrousd30d,
	)
	return usage, err
}

func loadAPIKeyBudgetThresholdStatesTx(ctx context.Context, tx pgx.Tx, keyID int64) (map[apiKeyBudgetThresholdKey]struct{}, error) {
	rows, err := tx.Query(ctx, `
		SELECT budget_kind, window_name, threshold_percent
		FROM api_key_budget_threshold_states
		WHERE client_key_id = $1
	`, keyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	states := map[apiKeyBudgetThresholdKey]struct{}{}
	for rows.Next() {
		var state apiKeyBudgetThresholdKey
		if err := rows.Scan(&state.Kind, &state.Window, &state.Threshold); err != nil {
			return nil, err
		}
		states[state] = struct{}{}
	}
	return states, rows.Err()
}

func apiKeyBudgetStreams(snapshot apiKeyBudgetSnapshot, usage admin.APIKeyBudgetUsage) []apiKeyBudgetStream {
	return []apiKeyBudgetStream{
		{Kind: "request", Window: "24h", Used: usage.RequestsUsed24h, Limit: snapshot.RequestBudget24h},
		{Kind: "token", Window: "24h", Used: usage.TokensUsed24h, Limit: snapshot.TokenBudget24h},
		{Kind: "cost", Window: "24h", Used: usage.CostMicrousd24h, Limit: snapshot.CostBudgetMicrousd24h},
		{Kind: "request", Window: "30d", Used: usage.RequestsUsed30d, Limit: snapshot.RequestBudget30d},
		{Kind: "token", Window: "30d", Used: usage.TokensUsed30d, Limit: snapshot.TokenBudget30d},
		{Kind: "cost", Window: "30d", Used: usage.CostMicrousd30d, Limit: snapshot.CostBudgetMicrousd30d},
	}
}

func apiKeyBudgetThresholdCrossed(used, limit int64, threshold int) bool {
	if limit <= 0 || used < 0 {
		return false
	}
	required := (limit/100)*int64(threshold) + ((limit%100)*int64(threshold)+99)/100
	return used >= required
}

func buildAPIKeyBudgetThresholdEvent(ctx context.Context, snapshot apiKeyBudgetSnapshot, stream apiKeyBudgetStream, threshold int, crossed bool, occurredAt time.Time, confirmation string) (systemevent.Event, error) {
	action := systemevent.ActionAPIKeyBudgetThreshold80Crossed
	severity := systemevent.SeverityWarning
	outcome := systemevent.OutcomePartial
	verb := "reached"
	if threshold == 100 {
		action = systemevent.ActionAPIKeyBudgetThreshold100Crossed
		severity = systemevent.SeverityError
		outcome = systemevent.OutcomeFailure
	}
	if !crossed {
		action = systemevent.ActionAPIKeyBudgetThreshold80Recovered
		if threshold == 100 {
			action = systemevent.ActionAPIKeyBudgetThreshold100Recovered
		}
		severity = systemevent.SeverityInfo
		outcome = systemevent.OutcomeSuccess
		verb = "recovered below"
	}
	metadataValues := map[string]any{
		"client_key_id":     snapshot.ID,
		"budget_kind":       stream.Kind,
		"window":            stream.Window,
		"threshold_percent": threshold,
		"used":              stream.Used,
		"limit":             stream.Limit,
	}
	allowedMetadata := []string{"client_key_id", "budget_kind", "window", "threshold_percent", "used", "limit"}
	if confirmation != "" {
		metadataValues["confirmation"] = confirmation
		allowedMetadata = append(allowedMetadata, "confirmation")
	}
	metadata, err := systemevent.SafeMetadata(metadataValues, allowedMetadata...)
	if err != nil {
		return systemevent.Event{}, err
	}
	if _, ok := systemevent.FromContext(ctx); !ok {
		ctx = systemevent.WithRequestContext(ctx, systemevent.RequestContext{
			CorrelationID: systemevent.NewCorrelationID(),
			Actor:         systemevent.Actor{Type: systemevent.ActorSystem, Name: "api_key_budget_monitor"},
		})
	}
	target := systemevent.Target{
		Type: "client_api_key_budget",
		ID:   fmt.Sprintf("%d:%s:%s", snapshot.ID, stream.Kind, stream.Window),
		Name: snapshot.Name,
	}
	intent := systemevent.EventIntent{
		Category: systemevent.CategoryRuntime,
		Severity: severity,
		Action:   action,
		Outcome:  outcome,
		Target:   target,
		Message:  fmt.Sprintf("API key %s %s budget %s %d percent", stream.Kind, stream.Window, verb, threshold),
		Metadata: metadata,
	}
	return systemevent.BuildEvent(ctx, intent, target, occurredAt, 0), nil
}

func recoverAPIKeyBudgetThresholdsForRevocation(ctx context.Context, tx pgx.Tx, snapshot apiKeyBudgetSnapshot, now time.Time) error {
	current, err := loadAPIKeyBudgetThresholdStatesTx(ctx, tx, snapshot.ID)
	if err != nil {
		return err
	}
	if len(current) == 0 {
		return nil
	}
	usage, err := loadAPIKeyBudgetUsageTx(ctx, tx, snapshot.ID, now)
	if err != nil {
		return err
	}
	streams := apiKeyBudgetStreams(snapshot, usage)
	for _, stream := range streams {
		for _, threshold := range []int{100, 80} {
			stateKey := apiKeyBudgetThresholdKey{Kind: stream.Kind, Window: stream.Window, Threshold: threshold}
			if _, exists := current[stateKey]; !exists {
				continue
			}
			event, err := buildAPIKeyBudgetThresholdEvent(ctx, snapshot, stream, threshold, false, now, "key_revoked")
			if err != nil {
				return err
			}
			if err := InsertSystemEventTx(ctx, tx, event); err != nil {
				return err
			}
		}
	}
	_, err = tx.Exec(ctx, `DELETE FROM api_key_budget_threshold_states WHERE client_key_id = $1`, snapshot.ID)
	return err
}

func apiKeyBudgetSnapshotFromValues(id int64, name string, request24h, token24h, cost24h, request30d, token30d, cost30d int64) apiKeyBudgetSnapshot {
	return apiKeyBudgetSnapshot{
		ID: id, Name: name,
		RequestBudget24h: request24h, TokenBudget24h: token24h, CostBudgetMicrousd24h: cost24h,
		RequestBudget30d: request30d, TokenBudget30d: token30d, CostBudgetMicrousd30d: cost30d,
	}
}
