package store

import (
	"context"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/jackc/pgx/v5"
)

const maxAPIKeyBudgetLedgerBatchSize = 1000

var _ admin.APIKeyBudgetLedgerStore = (*AdminRepository)(nil)

type apiKeyBudgetLimits struct {
	Request24h int64
	Token24h   int64
	Cost24h    int64
	Request30d int64
	Token30d   int64
	Cost30d    int64
}

type apiKeyBudgetExpiryCandidate struct {
	ID              int64
	ClientKeyID     int64
	ObservedTokens  int64
	ObservedCost    int64
	UsageCharged24h bool
	UsageCharged30d bool
	Expire24h       bool
	Expire30d       bool
}

type apiKeyBudgetLegacyLog struct {
	ID             int64
	CreatedAt      time.Time
	ObservedTokens int64
	ObservedCost   int64
	UsageKnown     bool
}

type apiKeyBudgetPendingSettlement struct {
	AdmissionID          string
	ClientKeyID          int64
	Status               string
	Outcome              string
	UsageKnown           bool
	ObservedTokens       int64
	ObservedCostMicrousd int64
	SettledAt            *time.Time
	Expires24h           time.Time
	Expires30d           time.Time
	Expired24h           bool
	Expired30d           bool
}

func (r *AdminRepository) AdmitAPIKeyBudget(ctx context.Context, keyID int64, admissionID string, admittedAt time.Time) (admin.APIKeyBudgetAdmission, error) {
	if r == nil || r.pool == nil || keyID < 1 || admittedAt.IsZero() || !validLiveBudgetAdmissionID(admissionID) {
		return admin.APIKeyBudgetAdmission{}, admin.ErrInvalidInput
	}
	admittedAt = admittedAt.UTC()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return admin.APIKeyBudgetAdmission{}, err
	}
	defer tx.Rollback(ctx)

	var limits apiKeyBudgetLimits
	err = tx.QueryRow(ctx, `
		SELECT
			request_budget_24h, token_budget_24h, cost_budget_microusd_24h,
			request_budget_30d, token_budget_30d, cost_budget_microusd_30d
		FROM client_api_keys
		WHERE id = $1
			AND revoked_at IS NULL
			AND disabled_at IS NULL
		FOR SHARE
	`, keyID).Scan(
		&limits.Request24h, &limits.Token24h, &limits.Cost24h,
		&limits.Request30d, &limits.Token30d, &limits.Cost30d,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.APIKeyBudgetAdmission{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.APIKeyBudgetAdmission{}, err
	}
	var status string
	var state admin.APIKeyBudgetState
	err = tx.QueryRow(ctx, `
		SELECT initialization_status,
			requests_used_24h, requests_used_30d,
			observed_tokens_used_24h, observed_tokens_used_30d,
			observed_cost_microusd_used_24h, observed_cost_microusd_used_30d
		FROM api_key_budget_states
		WHERE client_key_id = $1
		FOR UPDATE
	`, keyID).Scan(
		&status,
		&state.RequestsUsed24h, &state.RequestsUsed30d,
		&state.ObservedTokensUsed24h, &state.ObservedTokensUsed30d,
		&state.ObservedCostMicrousdUsed24h, &state.ObservedCostMicrousdUsed30d,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.APIKeyBudgetAdmission{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.APIKeyBudgetAdmission{}, err
	}
	var existingKeyID int64
	var existingAdmittedAt time.Time
	err = tx.QueryRow(ctx, `
		SELECT client_key_id, admitted_at
		FROM api_key_budget_admissions
		WHERE admission_id = $1 AND source = 'live'
	`, admissionID).Scan(&existingKeyID, &existingAdmittedAt)
	if err == nil {
		if existingKeyID != keyID {
			return admin.APIKeyBudgetAdmission{}, admin.ErrInvalidInput
		}
		if err := tx.Commit(ctx); err != nil {
			return admin.APIKeyBudgetAdmission{}, err
		}
		return admin.APIKeyBudgetAdmission{
			ID: admissionID, KeyID: keyID, AdmittedAt: existingAdmittedAt.UTC(),
		}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return admin.APIKeyBudgetAdmission{}, err
	}
	if !budgetLimitsDisabled(limits) && status != admin.APIKeyBudgetInitializationReady {
		return admin.APIKeyBudgetAdmission{}, admin.ErrBudgetInitializing
	}
	if budgetLimitReached(limits.Request24h, state.RequestsUsed24h) || budgetLimitReached(limits.Request30d, state.RequestsUsed30d) {
		return admin.APIKeyBudgetAdmission{}, admin.ErrAPIKeyRequestBudgetExceeded
	}
	if budgetLimitReached(limits.Token24h, state.ObservedTokensUsed24h) || budgetLimitReached(limits.Token30d, state.ObservedTokensUsed30d) {
		return admin.APIKeyBudgetAdmission{}, admin.ErrAPIKeyTokenBudgetExceeded
	}
	if budgetLimitReached(limits.Cost24h, state.ObservedCostMicrousdUsed24h) || budgetLimitReached(limits.Cost30d, state.ObservedCostMicrousdUsed30d) {
		return admin.APIKeyBudgetAdmission{}, admin.ErrAPIKeyCostBudgetExceeded
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO api_key_budget_admissions (
			admission_id, client_key_id, source, status, settlement_outcome,
			admitted_at, request_24h_expires_at, request_30d_expires_at
		) VALUES (
			$1, $2, 'live', 'admitted', 'pending',
			$3::timestamptz, $3::timestamptz + INTERVAL '24 hours', $3::timestamptz + INTERVAL '30 days'
		)
	`, admissionID, keyID, admittedAt); err != nil {
		return admin.APIKeyBudgetAdmission{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE api_key_budget_states
		SET requests_used_24h = requests_used_24h + 1,
			requests_used_30d = requests_used_30d + 1,
			version = version + 1,
			updated_at = $2
		WHERE client_key_id = $1
	`, keyID, admittedAt); err != nil {
		return admin.APIKeyBudgetAdmission{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.APIKeyBudgetAdmission{}, err
	}
	return admin.APIKeyBudgetAdmission{ID: admissionID, KeyID: keyID, AdmittedAt: admittedAt}, nil
}

func (r *AdminRepository) SettleAPIKeyBudget(ctx context.Context, admissionID string, observedTokens, observedCostMicrousd int64, settledAt time.Time) (admin.APIKeyBudgetSettlement, error) {
	return r.SettleAPIKeyBudgetUsage(ctx, admin.APIKeyBudgetSettlementRequest{
		AdmissionID:          admissionID,
		ObservedTokens:       observedTokens,
		ObservedCostMicrousd: observedCostMicrousd,
		UsageKnown:           true,
		SettledAt:            settledAt,
	})
}

func (r *AdminRepository) SettleAPIKeyBudgetUsage(ctx context.Context, request admin.APIKeyBudgetSettlementRequest) (admin.APIKeyBudgetSettlement, error) {
	request.AdmissionID = strings.TrimSpace(request.AdmissionID)
	if r == nil || r.pool == nil || request.AdmissionID == "" || len(request.AdmissionID) > 128 || request.ObservedTokens < 0 || request.ObservedCostMicrousd < 0 || request.SettledAt.IsZero() {
		return admin.APIKeyBudgetSettlement{}, admin.ErrInvalidInput
	}
	request.SettledAt = request.SettledAt.UTC()
	if !request.UsageKnown {
		request.ObservedTokens = 0
		request.ObservedCostMicrousd = 0
	}
	queued, final, err := r.enqueueAPIKeyBudgetSettlement(ctx, request)
	if err != nil {
		return admin.APIKeyBudgetSettlement{}, err
	}
	if final {
		return queued, nil
	}
	return r.applyAPIKeyBudgetSettlement(ctx, request.AdmissionID, request.SettledAt, queued.AlreadySettled)
}

func (r *AdminRepository) enqueueAPIKeyBudgetSettlement(ctx context.Context, request admin.APIKeyBudgetSettlementRequest) (admin.APIKeyBudgetSettlement, bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return admin.APIKeyBudgetSettlement{}, false, err
	}
	defer tx.Rollback(ctx)
	record, err := loadAPIKeyBudgetPendingSettlement(ctx, tx, request.AdmissionID, false)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.APIKeyBudgetSettlement{}, false, admin.ErrNotFound
	}
	if err != nil {
		return admin.APIKeyBudgetSettlement{}, false, err
	}
	if record.Status != "admitted" {
		if err := tx.Commit(ctx); err != nil {
			return admin.APIKeyBudgetSettlement{}, false, err
		}
		return budgetSettlementFromRecord(record, true), record.Status != "settlement_pending", nil
	}
	outcome := "observed"
	if !request.UsageKnown {
		outcome = "missing"
	}
	if _, err := tx.Exec(ctx, `
		UPDATE api_key_budget_admissions
		SET status = 'settlement_pending', settlement_outcome = $2, usage_known = $3,
			observed_tokens = $4, observed_cost_microusd = $5,
			settled_at = $6, updated_at = $6
		WHERE admission_id = $1
	`, request.AdmissionID, outcome, request.UsageKnown, request.ObservedTokens, request.ObservedCostMicrousd, request.SettledAt); err != nil {
		return admin.APIKeyBudgetSettlement{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.APIKeyBudgetSettlement{}, false, err
	}
	return admin.APIKeyBudgetSettlement{
		AdmissionID: request.AdmissionID, Outcome: outcome,
		ObservedTokens: request.ObservedTokens, ObservedCostMicrousd: request.ObservedCostMicrousd,
	}, false, nil
}

func (r *AdminRepository) applyAPIKeyBudgetSettlement(ctx context.Context, admissionID string, appliedAt time.Time, alreadyQueued bool) (admin.APIKeyBudgetSettlement, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return admin.APIKeyBudgetSettlement{}, err
	}
	defer tx.Rollback(ctx)
	record, err := loadAPIKeyBudgetPendingSettlement(ctx, tx, admissionID, false)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.APIKeyBudgetSettlement{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.APIKeyBudgetSettlement{}, err
	}
	if record.Status != "settlement_pending" {
		if err := tx.Commit(ctx); err != nil {
			return admin.APIKeyBudgetSettlement{}, err
		}
		return budgetSettlementFromRecord(record, true), nil
	}
	if err := applyLockedAPIKeyBudgetSettlement(ctx, tx, record, appliedAt.UTC()); err != nil {
		return admin.APIKeyBudgetSettlement{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.APIKeyBudgetSettlement{}, err
	}
	return budgetSettlementFromRecord(record, alreadyQueued), nil
}

func (r *AdminRepository) RunAPIKeyBudgetSettlementCycle(ctx context.Context, now time.Time, batchSize int) (admin.APIKeyBudgetSettlementCycleResult, error) {
	if r == nil || r.pool == nil || now.IsZero() || !validAPIKeyBudgetBatchSize(batchSize) {
		return admin.APIKeyBudgetSettlementCycleResult{}, admin.ErrInvalidInput
	}
	result := admin.APIKeyBudgetSettlementCycleResult{}
	for result.Processed < batchSize {
		processed, err := r.applyNextAPIKeyBudgetSettlement(ctx, now.UTC())
		if err != nil {
			return result, err
		}
		if !processed {
			break
		}
		result.Processed++
	}
	return result, nil
}

func (r *AdminRepository) applyNextAPIKeyBudgetSettlement(ctx context.Context, appliedAt time.Time) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	record, err := loadAPIKeyBudgetPendingSettlement(ctx, tx, "", true)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := applyLockedAPIKeyBudgetSettlement(ctx, tx, record, appliedAt); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func loadAPIKeyBudgetPendingSettlement(ctx context.Context, tx pgx.Tx, admissionID string, next bool) (apiKeyBudgetPendingSettlement, error) {
	query := `
		SELECT admission_id, client_key_id, status, settlement_outcome, usage_known,
			observed_tokens, observed_cost_microusd, settled_at,
			request_24h_expires_at, request_30d_expires_at,
			request_24h_expired, request_30d_expired
		FROM api_key_budget_admissions
		WHERE admission_id = $1
		FOR UPDATE
	`
	args := []any{admissionID}
	if next {
		query = `
			SELECT admission_id, client_key_id, status, settlement_outcome, usage_known,
				observed_tokens, observed_cost_microusd, settled_at,
				request_24h_expires_at, request_30d_expires_at,
				request_24h_expired, request_30d_expired
			FROM api_key_budget_admissions
			WHERE status = 'settlement_pending'
			ORDER BY updated_at ASC, id ASC
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		`
		args = nil
	}
	var record apiKeyBudgetPendingSettlement
	err := tx.QueryRow(ctx, query, args...).Scan(
		&record.AdmissionID, &record.ClientKeyID, &record.Status, &record.Outcome, &record.UsageKnown,
		&record.ObservedTokens, &record.ObservedCostMicrousd, &record.SettledAt,
		&record.Expires24h, &record.Expires30d, &record.Expired24h, &record.Expired30d,
	)
	return record, err
}

func applyLockedAPIKeyBudgetSettlement(ctx context.Context, tx pgx.Tx, record apiKeyBudgetPendingSettlement, appliedAt time.Time) error {
	if record.SettledAt == nil {
		return errors.New("pending budget settlement has no observation time")
	}
	addTokens24h, addCost24h := int64(0), int64(0)
	addTokens30d, addCost30d := int64(0), int64(0)
	if !record.Expired24h && record.Expires24h.After(*record.SettledAt) {
		addTokens24h, addCost24h = record.ObservedTokens, record.ObservedCostMicrousd
	}
	if !record.Expired30d && record.Expires30d.After(*record.SettledAt) {
		addTokens30d, addCost30d = record.ObservedTokens, record.ObservedCostMicrousd
	}
	if _, err := tx.Exec(ctx, `
		UPDATE api_key_budget_states
		SET observed_tokens_used_24h = observed_tokens_used_24h + $2,
			observed_tokens_used_30d = observed_tokens_used_30d + $3,
			observed_cost_microusd_used_24h = observed_cost_microusd_used_24h + $4,
			observed_cost_microusd_used_30d = observed_cost_microusd_used_30d + $5,
			version = version + 1, updated_at = $6
		WHERE client_key_id = $1
	`, record.ClientKeyID, addTokens24h, addTokens30d, addCost24h, addCost30d, appliedAt); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE api_key_budget_admissions
		SET status = 'settled', updated_at = $2
		WHERE admission_id = $1 AND status = 'settlement_pending'
	`, record.AdmissionID, appliedAt)
	return err
}

func budgetSettlementFromRecord(record apiKeyBudgetPendingSettlement, alreadySettled bool) admin.APIKeyBudgetSettlement {
	return admin.APIKeyBudgetSettlement{
		AdmissionID: record.AdmissionID, AlreadySettled: alreadySettled, Outcome: record.Outcome,
		ObservedTokens: record.ObservedTokens, ObservedCostMicrousd: record.ObservedCostMicrousd,
	}
}

func (r *AdminRepository) GetAPIKeyBudgetState(ctx context.Context, clientKeyID int64) (admin.APIKeyBudgetState, error) {
	if r == nil || r.pool == nil || clientKeyID < 1 {
		return admin.APIKeyBudgetState{}, admin.ErrInvalidInput
	}
	var state admin.APIKeyBudgetState
	err := r.pool.QueryRow(ctx, `
		SELECT client_key_id, initialization_status,
			requests_used_24h, requests_used_30d,
			observed_tokens_used_24h, observed_tokens_used_30d,
			observed_cost_microusd_used_24h, observed_cost_microusd_used_30d,
			initialization_attempts, initialized_at, last_maintenance_at, version, updated_at
		FROM api_key_budget_states
		WHERE client_key_id = $1
	`, clientKeyID).Scan(
		&state.ClientKeyID, &state.InitializationStatus,
		&state.RequestsUsed24h, &state.RequestsUsed30d,
		&state.ObservedTokensUsed24h, &state.ObservedTokensUsed30d,
		&state.ObservedCostMicrousdUsed24h, &state.ObservedCostMicrousdUsed30d,
		&state.InitializationAttempts, &state.InitializedAt, &state.LastMaintenanceAt, &state.Version, &state.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.APIKeyBudgetState{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.APIKeyBudgetState{}, err
	}
	state.UpdatedAt = state.UpdatedAt.UTC()
	state.InitializedAt = utcTimePointer(state.InitializedAt)
	state.LastMaintenanceAt = utcTimePointer(state.LastMaintenanceAt)
	return state, nil
}

func (r *AdminRepository) RunAPIKeyBudgetExpiryCycle(ctx context.Context, now time.Time, batchSize int) (admin.APIKeyBudgetExpiryCycleResult, error) {
	if r == nil || r.pool == nil || now.IsZero() || !validAPIKeyBudgetBatchSize(batchSize) {
		return admin.APIKeyBudgetExpiryCycleResult{}, admin.ErrInvalidInput
	}
	now = now.UTC()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return admin.APIKeyBudgetExpiryCycleResult{}, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `
		SELECT id, client_key_id, observed_tokens, observed_cost_microusd,
			(status = 'settled' AND settled_at < request_24h_expires_at),
			(status = 'settled' AND settled_at < request_30d_expires_at),
			(NOT request_24h_expired AND request_24h_expires_at <= $1),
			(NOT request_30d_expired AND request_30d_expires_at <= $1)
		FROM api_key_budget_admissions
		WHERE (NOT request_24h_expired AND request_24h_expires_at <= $1)
			OR (NOT request_30d_expired AND request_30d_expires_at <= $1)
		ORDER BY LEAST(
			CASE WHEN request_24h_expired THEN 'infinity'::timestamptz ELSE request_24h_expires_at END,
			CASE WHEN request_30d_expired THEN 'infinity'::timestamptz ELSE request_30d_expires_at END
		) ASC, id ASC
		FOR UPDATE SKIP LOCKED
		LIMIT $2
	`, now, batchSize)
	if err != nil {
		return admin.APIKeyBudgetExpiryCycleResult{}, err
	}
	var candidates []apiKeyBudgetExpiryCandidate
	for rows.Next() {
		var candidate apiKeyBudgetExpiryCandidate
		if err := rows.Scan(
			&candidate.ID, &candidate.ClientKeyID,
			&candidate.ObservedTokens, &candidate.ObservedCost,
			&candidate.UsageCharged24h, &candidate.UsageCharged30d,
			&candidate.Expire24h, &candidate.Expire30d,
		); err != nil {
			rows.Close()
			return admin.APIKeyBudgetExpiryCycleResult{}, err
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return admin.APIKeyBudgetExpiryCycleResult{}, err
	}
	rows.Close()
	if err := lockAPIKeyBudgetStateRows(ctx, tx, candidates); err != nil {
		return admin.APIKeyBudgetExpiryCycleResult{}, err
	}

	result := admin.APIKeyBudgetExpiryCycleResult{Processed: len(candidates)}
	for _, candidate := range candidates {
		request24h, request30d := int64(0), int64(0)
		tokens24h, tokens30d := int64(0), int64(0)
		cost24h, cost30d := int64(0), int64(0)
		if candidate.Expire24h {
			result.Expired24h++
			request24h = 1
			if candidate.UsageCharged24h {
				tokens24h, cost24h = candidate.ObservedTokens, candidate.ObservedCost
			}
		}
		if candidate.Expire30d {
			result.Expired30d++
			request30d = 1
			if candidate.UsageCharged30d {
				tokens30d, cost30d = candidate.ObservedTokens, candidate.ObservedCost
			}
		}
		if _, err := tx.Exec(ctx, `
			UPDATE api_key_budget_states
			SET requests_used_24h = GREATEST(0, requests_used_24h - $2),
				requests_used_30d = GREATEST(0, requests_used_30d - $3),
				observed_tokens_used_24h = GREATEST(0, observed_tokens_used_24h - $4),
				observed_tokens_used_30d = GREATEST(0, observed_tokens_used_30d - $5),
				observed_cost_microusd_used_24h = GREATEST(0, observed_cost_microusd_used_24h - $6),
				observed_cost_microusd_used_30d = GREATEST(0, observed_cost_microusd_used_30d - $7),
				last_maintenance_at = $8, version = version + 1, updated_at = $8
			WHERE client_key_id = $1
		`, candidate.ClientKeyID, request24h, request30d, tokens24h, tokens30d, cost24h, cost30d, now); err != nil {
			return admin.APIKeyBudgetExpiryCycleResult{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE api_key_budget_admissions
			SET request_24h_expired = request_24h_expired OR $2,
				request_30d_expired = request_30d_expired OR $3,
				updated_at = $4
			WHERE id = $1
		`, candidate.ID, candidate.Expire24h, candidate.Expire30d, now); err != nil {
			return admin.APIKeyBudgetExpiryCycleResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.APIKeyBudgetExpiryCycleResult{}, err
	}
	if result.Processed > 0 {
		result.LastMaintenanceAt = &now
	}
	return result, nil
}

func (r *AdminRepository) RunAPIKeyBudgetAbandonedCycle(ctx context.Context, cutoff, now time.Time, batchSize int) (admin.APIKeyBudgetAbandonedCycleResult, error) {
	if r == nil || r.pool == nil || cutoff.IsZero() || now.IsZero() || now.Before(cutoff) || !validAPIKeyBudgetBatchSize(batchSize) {
		return admin.APIKeyBudgetAbandonedCycleResult{}, admin.ErrInvalidInput
	}
	tag, err := r.pool.Exec(ctx, `
		WITH candidates AS (
			SELECT id
			FROM api_key_budget_admissions
			WHERE status = 'admitted' AND admitted_at <= $1
			ORDER BY admitted_at ASC, id ASC
			FOR UPDATE SKIP LOCKED
			LIMIT $3
		)
		UPDATE api_key_budget_admissions admissions
		SET status = 'abandoned', settlement_outcome = 'abandoned', settled_at = $2, updated_at = $2
		FROM candidates
		WHERE admissions.id = candidates.id
	`, cutoff.UTC(), now.UTC(), batchSize)
	if err != nil {
		return admin.APIKeyBudgetAbandonedCycleResult{}, err
	}
	return admin.APIKeyBudgetAbandonedCycleResult{Processed: int(tag.RowsAffected())}, nil
}

func (r *AdminRepository) RunAPIKeyBudgetInitializationCycle(ctx context.Context, now time.Time, batchSize int) (admin.APIKeyBudgetInitializationCycleResult, error) {
	if r == nil || r.pool == nil || now.IsZero() || !validAPIKeyBudgetBatchSize(batchSize) {
		return admin.APIKeyBudgetInitializationCycleResult{}, admin.ErrInvalidInput
	}
	now = now.UTC()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return admin.APIKeyBudgetInitializationCycleResult{}, err
	}
	defer tx.Rollback(ctx)

	var keyID int64
	err = tx.QueryRow(ctx, `
		SELECT client_key_id
		FROM api_key_budget_states
		WHERE initialization_status = 'pending'
		ORDER BY client_key_id ASC
		LIMIT 1
	`).Scan(&keyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.APIKeyBudgetInitializationCycleResult{}, nil
	}
	if err != nil {
		return admin.APIKeyBudgetInitializationCycleResult{}, err
	}
	if err := tx.QueryRow(ctx, `
		SELECT id
		FROM client_api_keys
		WHERE id = $1
		FOR UPDATE
	`, keyID).Scan(&keyID); errors.Is(err, pgx.ErrNoRows) {
		return admin.APIKeyBudgetInitializationCycleResult{}, nil
	} else if err != nil {
		return admin.APIKeyBudgetInitializationCycleResult{}, err
	}

	var windowStart, windowEnd, cursorCreatedAt *time.Time
	var cursorLogID *int64
	err = tx.QueryRow(ctx, `
		SELECT initialization_window_start, initialization_window_end,
			initialization_cursor_created_at, initialization_cursor_request_log_id
		FROM api_key_budget_states
		WHERE client_key_id = $1
			AND initialization_status = 'pending'
		FOR UPDATE
	`, keyID).Scan(&windowStart, &windowEnd, &cursorCreatedAt, &cursorLogID)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.APIKeyBudgetInitializationCycleResult{}, nil
	}
	if err != nil {
		return admin.APIKeyBudgetInitializationCycleResult{}, err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('n2api-api-key-budget-init:' || ($1::bigint)::text, 0))`, keyID); err != nil {
		return admin.APIKeyBudgetInitializationCycleResult{}, err
	}
	if windowStart == nil || windowEnd == nil {
		value := now
		windowEnd = &value
		start := value.Add(-30 * 24 * time.Hour)
		windowStart = &start
	}

	args := []any{keyID, windowStart.UTC(), windowEnd.UTC()}
	cursorSQL := ""
	if cursorCreatedAt != nil && cursorLogID != nil {
		args = append(args, cursorCreatedAt.UTC(), *cursorLogID)
		cursorSQL = " AND (created_at, id) > ($4, $5)"
	}
	args = append(args, batchSize+1)
	rows, err := tx.Query(ctx, `
		SELECT id, created_at, GREATEST(total_tokens, 0), GREATEST(estimated_cost_microusd, 0), usage_source <> 'missing'
		FROM request_logs
		WHERE client_key_id = $1
			AND created_at >= $2
			AND created_at <= $3
			AND budget_backfill_eligible
			AND NOT EXISTS (
				SELECT 1
				FROM api_key_budget_admissions
				WHERE admission_id = 'legacy:' || request_logs.id::text
			)`+cursorSQL+`
		ORDER BY created_at ASC, id ASC
		LIMIT $`+strconv.Itoa(len(args))+`
	`, args...)
	if err != nil {
		return admin.APIKeyBudgetInitializationCycleResult{}, err
	}
	logs := make([]apiKeyBudgetLegacyLog, 0, batchSize+1)
	for rows.Next() {
		var log apiKeyBudgetLegacyLog
		if err := rows.Scan(&log.ID, &log.CreatedAt, &log.ObservedTokens, &log.ObservedCost, &log.UsageKnown); err != nil {
			rows.Close()
			return admin.APIKeyBudgetInitializationCycleResult{}, err
		}
		log.CreatedAt = log.CreatedAt.UTC()
		logs = append(logs, log)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return admin.APIKeyBudgetInitializationCycleResult{}, err
	}
	rows.Close()

	hasMore := len(logs) > batchSize
	if hasMore {
		logs = logs[:batchSize]
	}
	var requests24h, requests30d, tokens24h, tokens30d, cost24h, cost30d int64
	for _, log := range logs {
		if !log.UsageKnown {
			log.ObservedTokens = 0
			log.ObservedCost = 0
		}
		expires24h := log.CreatedAt.Add(24 * time.Hour)
		expires30d := log.CreatedAt.Add(30 * 24 * time.Hour)
		expired24h := !expires24h.After(now)
		expired30d := !expires30d.After(now)
		outcome := "observed"
		if !log.UsageKnown {
			outcome = "missing"
		}
		var insertedID int64
		err := tx.QueryRow(ctx, `
			INSERT INTO api_key_budget_admissions (
				admission_id, client_key_id, source, status, settlement_outcome, usage_known,
				observed_tokens, observed_cost_microusd, admitted_at, settled_at,
				request_24h_expires_at, request_30d_expires_at,
				request_24h_expired, request_30d_expired
			) VALUES (
				$1, $2, 'legacy', 'settled', $3, $4, $5, $6, $7, $7,
				$8, $9, $10, $11
			)
			ON CONFLICT (admission_id) DO NOTHING
			RETURNING id
		`, "legacy:"+strconv.FormatInt(log.ID, 10), keyID, outcome, log.UsageKnown,
			log.ObservedTokens, log.ObservedCost, log.CreatedAt,
			expires24h, expires30d, expired24h, expired30d,
		).Scan(&insertedID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return admin.APIKeyBudgetInitializationCycleResult{}, err
		}
		if !expired24h {
			requests24h++
			tokens24h += log.ObservedTokens
			cost24h += log.ObservedCost
		}
		if !expired30d {
			requests30d++
			tokens30d += log.ObservedTokens
			cost30d += log.ObservedCost
		}
	}

	ready := !hasMore
	status := admin.APIKeyBudgetInitializationPending
	var initializedAt *time.Time
	if ready {
		status = admin.APIKeyBudgetInitializationReady
		initializedAt = &now
	}
	var nextCursorCreatedAt *time.Time
	var nextCursorLogID *int64
	if len(logs) > 0 {
		last := logs[len(logs)-1]
		nextCursorCreatedAt = &last.CreatedAt
		nextCursorLogID = &last.ID
	} else {
		nextCursorCreatedAt = cursorCreatedAt
		nextCursorLogID = cursorLogID
	}
	if _, err := tx.Exec(ctx, `
		UPDATE api_key_budget_states
		SET initialization_status = $2,
			initialization_window_start = $3,
			initialization_window_end = $4,
			initialization_cursor_created_at = $5,
			initialization_cursor_request_log_id = $6,
			initialization_attempts = initialization_attempts + 1,
			initialized_at = $7,
			requests_used_24h = requests_used_24h + $8,
			requests_used_30d = requests_used_30d + $9,
			observed_tokens_used_24h = observed_tokens_used_24h + $10,
			observed_tokens_used_30d = observed_tokens_used_30d + $11,
			observed_cost_microusd_used_24h = observed_cost_microusd_used_24h + $12,
			observed_cost_microusd_used_30d = observed_cost_microusd_used_30d + $13,
			last_maintenance_at = $14,
			version = version + 1,
			updated_at = $14
		WHERE client_key_id = $1
	`, keyID, status, windowStart.UTC(), windowEnd.UTC(), nextCursorCreatedAt, nextCursorLogID, initializedAt,
		requests24h, requests30d, tokens24h, tokens30d, cost24h, cost30d, now,
	); err != nil {
		return admin.APIKeyBudgetInitializationCycleResult{}, err
	}
	var hasPending bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM api_key_budget_states WHERE initialization_status = 'pending')`).Scan(&hasPending); err != nil {
		return admin.APIKeyBudgetInitializationCycleResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.APIKeyBudgetInitializationCycleResult{}, err
	}
	return admin.APIKeyBudgetInitializationCycleResult{
		ClientKeyID: keyID, Processed: len(logs), Ready: ready, HasPending: hasPending,
	}, nil
}

func lockAPIKeyBudgetStateRows(ctx context.Context, tx pgx.Tx, candidates []apiKeyBudgetExpiryCandidate) error {
	keySet := make(map[int64]struct{}, len(candidates))
	for _, candidate := range candidates {
		keySet[candidate.ClientKeyID] = struct{}{}
	}
	keyIDs := make([]int64, 0, len(keySet))
	for keyID := range keySet {
		keyIDs = append(keyIDs, keyID)
	}
	sort.Slice(keyIDs, func(i, j int) bool { return keyIDs[i] < keyIDs[j] })
	for _, keyID := range keyIDs {
		if err := tx.QueryRow(ctx, `
			SELECT client_key_id
			FROM api_key_budget_states
			WHERE client_key_id = $1
			FOR UPDATE
		`, keyID).Scan(&keyID); err != nil {
			return err
		}
	}
	return nil
}

func validLiveBudgetAdmissionID(value string) bool {
	if len(value) != 32 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 16
}

func budgetLimitsDisabled(limits apiKeyBudgetLimits) bool {
	return limits.Request24h <= 0 && limits.Token24h <= 0 && limits.Cost24h <= 0 &&
		limits.Request30d <= 0 && limits.Token30d <= 0 && limits.Cost30d <= 0
}

func budgetLimitReached(limit, used int64) bool {
	return limit > 0 && used >= limit
}

func validAPIKeyBudgetBatchSize(batchSize int) bool {
	return batchSize > 0 && batchSize <= maxAPIKeyBudgetLedgerBatchSize
}

func utcTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}
