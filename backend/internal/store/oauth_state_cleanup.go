package store

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/oauthstatecleanup"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	oauthStateCleanupAdvisoryLockID = int64(0x4e324150494f53)
	oauthStateCleanupUnlockTimeout  = 2 * time.Second
)

type OAuthStateCleanupRepository struct {
	pool *pgxpool.Pool
}

type oauthStateCleanupLease struct {
	conn       *pgxpool.Conn
	acquireCtx context.Context
	mu         sync.Mutex
	closed     bool
}

func NewOAuthStateCleanupRepository(pool *pgxpool.Pool) *OAuthStateCleanupRepository {
	return &OAuthStateCleanupRepository{pool: pool}
}

func (r *OAuthStateCleanupRepository) TryAcquire(ctx context.Context) (oauthstatecleanup.Lease, bool, error) {
	if r == nil || r.pool == nil {
		return nil, false, errors.New("oauth state cleanup repository is not configured")
	}
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return nil, false, err
	}
	var acquired bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, oauthStateCleanupAdvisoryLockID).Scan(&acquired); err != nil {
		discardOAuthStateCleanupConnection(conn)
		return nil, false, err
	}
	if !acquired {
		conn.Release()
		return nil, false, nil
	}
	return &oauthStateCleanupLease{conn: conn, acquireCtx: ctx}, true, nil
}

func (l *oauthStateCleanupLease) CountEligible(ctx context.Context, cutoff time.Time) (int64, error) {
	if l == nil || l.conn == nil || cutoff.IsZero() {
		return 0, errors.New("oauth state cleanup lease is invalid")
	}
	var count int64
	err := l.conn.QueryRow(ctx, `
		SELECT count(*)
		FROM oauth_states
		WHERE expires_at < $1 OR (consumed_at IS NOT NULL AND consumed_at < $1)
	`, cutoff.UTC()).Scan(&count)
	return count, err
}

func (l *oauthStateCleanupLease) DeleteEligibleBatch(ctx context.Context, cutoff time.Time, batchSize int) (int64, error) {
	if l == nil || l.conn == nil || cutoff.IsZero() || batchSize < 1 || batchSize > oauthstatecleanup.MaxBatchSize {
		return 0, errors.New("oauth state cleanup batch is invalid")
	}
	tx, err := l.conn.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), oauthStateCleanupUnlockTimeout)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	tag, err := tx.Exec(ctx, `
		WITH candidates AS (
			SELECT id
			FROM oauth_states
			WHERE expires_at < $1 OR (consumed_at IS NOT NULL AND consumed_at < $1)
			ORDER BY id ASC
			FOR UPDATE SKIP LOCKED
			LIMIT $2
		)
		DELETE FROM oauth_states AS states
		USING candidates
		WHERE states.id = candidates.id
	`, cutoff.UTC(), batchSize)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	committed = true
	return tag.RowsAffected(), nil
}

func (l *oauthStateCleanupLease) Close() error {
	if l == nil || l.conn == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	if l.conn.Conn().IsClosed() {
		l.conn.Release()
		return nil
	}
	unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(l.acquireCtx), oauthStateCleanupUnlockTimeout)
	defer cancel()
	var unlocked bool
	err := l.conn.QueryRow(unlockCtx, `SELECT pg_advisory_unlock($1)`, oauthStateCleanupAdvisoryLockID).Scan(&unlocked)
	if err == nil && unlocked {
		l.conn.Release()
		return nil
	}
	discardOAuthStateCleanupConnection(l.conn)
	if err != nil {
		return err
	}
	return errors.New("oauth state cleanup advisory lock was not held")
}

func discardOAuthStateCleanupConnection(poolConn *pgxpool.Conn) {
	conn := poolConn.Hijack()
	closeCtx, cancel := context.WithTimeout(context.Background(), oauthStateCleanupUnlockTimeout)
	defer cancel()
	_ = conn.Close(closeCtx)
}
