package store

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/KnowSky404/N2API/backend/internal/systemevent"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	routingExhaustionProjectorKey            = "routing_exhaustion_v1"
	routingExhaustionProjectorAdvisoryLockID = int64(0x4e324150495245)
	maxRoutingExhaustionProjectorBatchSize   = 1000
	maxRoutingExhaustionTransitionsPerCycle  = 100
	routingExhaustionLockCleanupTimeout      = 2 * time.Second
)

type routingExhaustionRequestLog struct {
	ID                int64
	ClientKeyID       *int64
	StatusCode        int
	ProviderAccountID *int64
	RoutingPoolID     *int64
	RoutingPoolError  string
	FallbackDepth     int
	CreatedAt         time.Time
}

func (r *AdminRepository) RunRoutingExhaustionProjectorCycle(ctx context.Context, batchLimit, transitionLimit int, now time.Time) (result admin.RoutingExhaustionProjectorCycleResult, err error) {
	if batchLimit <= 0 || batchLimit > maxRoutingExhaustionProjectorBatchSize ||
		transitionLimit <= 0 || transitionLimit > maxRoutingExhaustionTransitionsPerCycle || now.IsZero() {
		return admin.RoutingExhaustionProjectorCycleResult{}, admin.ErrInvalidInput
	}
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return admin.RoutingExhaustionProjectorCycleResult{}, err
	}
	var acquired bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, routingExhaustionProjectorAdvisoryLockID).Scan(&acquired); err != nil {
		discardRoutingExhaustionConnection(conn)
		return admin.RoutingExhaustionProjectorCycleResult{}, err
	}
	if !acquired {
		conn.Release()
		return admin.RoutingExhaustionProjectorCycleResult{Contended: true}, nil
	}
	defer func() {
		unlockErr := releaseRoutingExhaustionProjectorLock(conn, ctx)
		if err == nil && unlockErr != nil {
			result = admin.RoutingExhaustionProjectorCycleResult{}
			err = unlockErr
		}
	}()

	safeMax, contended, err := routingExhaustionSafeMaxRequestLogID(ctx, conn)
	if err != nil {
		return admin.RoutingExhaustionProjectorCycleResult{}, err
	}
	if contended {
		return admin.RoutingExhaustionProjectorCycleResult{Contended: true}, nil
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		return admin.RoutingExhaustionProjectorCycleResult{}, err
	}
	defer tx.Rollback(ctx)

	var checkpoint int64
	if err := tx.QueryRow(ctx, `
		SELECT last_request_log_id
		FROM request_log_projector_checkpoints
		WHERE projector_key = $1
		FOR UPDATE
	`, routingExhaustionProjectorKey).Scan(&checkpoint); err != nil {
		return admin.RoutingExhaustionProjectorCycleResult{}, err
	}
	logs, err := loadRoutingExhaustionRequestLogs(ctx, tx, checkpoint, safeMax, batchLimit)
	if err != nil {
		return admin.RoutingExhaustionProjectorCycleResult{}, err
	}

	result = admin.RoutingExhaustionProjectorCycleResult{LastRequestLogID: checkpoint}
	for _, requestLog := range logs {
		transitioned, stop, err := r.projectRoutingExhaustionRequestLog(ctx, tx, requestLog, result.Transitions, transitionLimit, now.UTC())
		if err != nil {
			return admin.RoutingExhaustionProjectorCycleResult{}, err
		}
		if stop {
			break
		}
		result.Processed++
		result.LastRequestLogID = requestLog.ID
		if transitioned {
			result.Transitions++
		}
	}
	if result.LastRequestLogID != checkpoint {
		if _, err := tx.Exec(ctx, `
			UPDATE request_log_projector_checkpoints
			SET last_request_log_id = $2, updated_at = $3
			WHERE projector_key = $1
		`, routingExhaustionProjectorKey, result.LastRequestLogID, now.UTC()); err != nil {
			return admin.RoutingExhaustionProjectorCycleResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.RoutingExhaustionProjectorCycleResult{}, err
	}
	return result, nil
}

func routingExhaustionSafeMaxRequestLogID(ctx context.Context, conn *pgxpool.Conn) (int64, bool, error) {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `LOCK TABLE request_logs IN SHARE MODE NOWAIT`); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "55P03" {
			return 0, true, nil
		}
		return 0, false, err
	}
	var safeMax int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(id), 0) FROM request_logs`).Scan(&safeMax); err != nil {
		return 0, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, false, err
	}
	return safeMax, false, nil
}

func releaseRoutingExhaustionProjectorLock(conn *pgxpool.Conn, acquireCtx context.Context) error {
	unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(acquireCtx), routingExhaustionLockCleanupTimeout)
	defer cancel()
	var unlocked bool
	err := conn.QueryRow(unlockCtx, `SELECT pg_advisory_unlock($1)`, routingExhaustionProjectorAdvisoryLockID).Scan(&unlocked)
	if err == nil && unlocked {
		conn.Release()
		return nil
	}
	discardRoutingExhaustionConnection(conn)
	if err != nil {
		return err
	}
	return errors.New("routing exhaustion projector advisory lock was not held")
}

func discardRoutingExhaustionConnection(poolConn *pgxpool.Conn) {
	conn := poolConn.Hijack()
	closeCtx, cancel := context.WithTimeout(context.Background(), routingExhaustionLockCleanupTimeout)
	defer cancel()
	_ = conn.Close(closeCtx)
}

func loadRoutingExhaustionRequestLogs(ctx context.Context, tx pgx.Tx, checkpoint, safeMax int64, limit int) ([]routingExhaustionRequestLog, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, client_key_id, status_code, provider_account_id, routing_pool_id,
			routing_pool_error, routing_pool_fallback_depth, created_at
		FROM request_logs
		WHERE id > $1 AND id <= $2
		ORDER BY id ASC
		LIMIT $3
	`, checkpoint, safeMax, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	logs := make([]routingExhaustionRequestLog, 0, limit)
	for rows.Next() {
		var requestLog routingExhaustionRequestLog
		if err := rows.Scan(
			&requestLog.ID, &requestLog.ClientKeyID, &requestLog.StatusCode,
			&requestLog.ProviderAccountID, &requestLog.RoutingPoolID,
			&requestLog.RoutingPoolError, &requestLog.FallbackDepth, &requestLog.CreatedAt,
		); err != nil {
			return nil, err
		}
		logs = append(logs, requestLog)
	}
	return logs, rows.Err()
}

func (r *AdminRepository) projectRoutingExhaustionRequestLog(ctx context.Context, tx pgx.Tx, requestLog routingExhaustionRequestLog, transitions, transitionLimit int, now time.Time) (transitioned, stop bool, err error) {
	if requestLog.ClientKeyID == nil {
		return false, false, nil
	}
	isTrigger := requestLog.RoutingPoolError == "routing_pool_exhausted"
	isRecovery := requestLog.StatusCode >= 200 && requestLog.StatusCode <= 299 &&
		requestLog.ProviderAccountID != nil && requestLog.RoutingPoolError == ""
	if !isTrigger && !isRecovery {
		return false, false, nil
	}

	stateExists, err := routingExhaustionStateExists(ctx, tx, *requestLog.ClientKeyID)
	if err != nil {
		return false, false, err
	}
	if (isTrigger && stateExists) || (isRecovery && !stateExists) {
		return false, false, nil
	}
	if transitions >= transitionLimit {
		return false, true, nil
	}

	keyName, err := lockActiveAPIKeyName(ctx, tx, *requestLog.ClientKeyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	stateExists, err = routingExhaustionStateExists(ctx, tx, *requestLog.ClientKeyID)
	if err != nil {
		return false, false, err
	}
	if (isTrigger && stateExists) || (isRecovery && !stateExists) {
		return false, false, nil
	}

	if isTrigger {
		if _, err := tx.Exec(ctx, `
			INSERT INTO api_key_routing_exhaustion_states (
				client_key_id, trigger_request_log_id, triggered_at, updated_at
			) VALUES ($1, $2, $3, $4)
		`, *requestLog.ClientKeyID, requestLog.ID, requestLog.CreatedAt.UTC(), now); err != nil {
			return false, false, err
		}
	} else {
		if _, err := tx.Exec(ctx, `
			DELETE FROM api_key_routing_exhaustion_states
			WHERE client_key_id = $1
		`, *requestLog.ClientKeyID); err != nil {
			return false, false, err
		}
	}
	event, err := buildAPIKeyRoutingExhaustionEvent(ctx, keyName, requestLog, isTrigger, requestLog.CreatedAt.UTC(), "")
	if err != nil {
		return false, false, err
	}
	if err := InsertSystemEventTx(ctx, tx, event); err != nil {
		return false, false, err
	}
	return true, false, nil
}

func routingExhaustionStateExists(ctx context.Context, tx pgx.Tx, keyID int64) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM api_key_routing_exhaustion_states WHERE client_key_id = $1
		)
	`, keyID).Scan(&exists)
	return exists, err
}

func lockActiveAPIKeyName(ctx context.Context, tx pgx.Tx, keyID int64) (string, error) {
	var name string
	err := tx.QueryRow(ctx, `
		SELECT name
		FROM client_api_keys
		WHERE id = $1 AND revoked_at IS NULL
		FOR UPDATE
	`, keyID).Scan(&name)
	return name, err
}

func buildAPIKeyRoutingExhaustionEvent(ctx context.Context, keyName string, requestLog routingExhaustionRequestLog, exhausted bool, occurredAt time.Time, confirmation string) (systemevent.Event, error) {
	metadataValues := map[string]any{
		"client_key_id": *requestLog.ClientKeyID,
	}
	allowedMetadata := []string{"client_key_id"}
	if requestLog.ID > 0 {
		metadataValues["request_log_id"] = requestLog.ID
		allowedMetadata = append(allowedMetadata, "request_log_id")
	}
	if exhausted {
		if requestLog.RoutingPoolID != nil {
			metadataValues["routing_pool_id"] = *requestLog.RoutingPoolID
			allowedMetadata = append(allowedMetadata, "routing_pool_id")
		}
		metadataValues["fallback_depth"] = requestLog.FallbackDepth
		allowedMetadata = append(allowedMetadata, "fallback_depth")
	} else if requestLog.ProviderAccountID != nil {
		metadataValues["provider_account_id"] = *requestLog.ProviderAccountID
		allowedMetadata = append(allowedMetadata, "provider_account_id")
	}
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
			Actor:         systemevent.Actor{Type: systemevent.ActorSystem, Name: "routing_exhaustion_projector"},
		})
	}
	target := systemevent.Target{Type: "client_api_key", ID: strconv.FormatInt(*requestLog.ClientKeyID, 10), Name: keyName}
	intent := systemevent.EventIntent{
		Category: systemevent.CategoryRuntime,
		Severity: systemevent.SeverityInfo,
		Action:   systemevent.ActionAPIKeyRoutingPoolRecovered,
		Outcome:  systemevent.OutcomeSuccess,
		Target:   target,
		Message:  "API key routing pool recovered",
		Metadata: metadata,
	}
	if exhausted {
		intent.Severity = systemevent.SeverityError
		intent.Action = systemevent.ActionAPIKeyRoutingPoolExhausted
		intent.Outcome = systemevent.OutcomeFailure
		intent.Message = "API key routing pool exhausted"
	}
	return systemevent.BuildEvent(ctx, intent, target, occurredAt, 0), nil
}

func recoverAPIKeyRoutingExhaustionForRevocation(ctx context.Context, tx pgx.Tx, keyID int64, keyName string, now time.Time) error {
	result, err := tx.Exec(ctx, `
		DELETE FROM api_key_routing_exhaustion_states
		WHERE client_key_id = $1
	`, keyID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return nil
	}
	requestLog := routingExhaustionRequestLog{ClientKeyID: &keyID}
	event, err := buildAPIKeyRoutingExhaustionEvent(ctx, keyName, requestLog, false, now.UTC(), "key_revoked")
	if err != nil {
		return err
	}
	return InsertSystemEventTx(ctx, tx, event)
}
