package store

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"sync"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/gateway"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	responseAffinityHMACDomain              = "n2api:responses-affinity:v1\x00"
	responseAffinityRetentionAdvisoryLockID = int64(0x4e324150495241)
	responseAffinityRetentionUnlockTimeout  = 2 * time.Second
	maxResponseAffinityRetentionBatchSize   = 10000
)

var ErrInvalidResponseAffinity = errors.New("invalid response affinity")

type ResponseAffinityRepository struct {
	pool       *pgxpool.Pool
	hmacSecret []byte
}

var _ gateway.ResponseAffinityStore = (*ResponseAffinityRepository)(nil)

type responseAffinityRetentionLease struct {
	conn       *pgxpool.Conn
	acquireCtx context.Context
	mu         sync.Mutex
	closed     bool
}

func NewResponseAffinityRepository(pool *pgxpool.Pool, hmacSecret string) *ResponseAffinityRepository {
	return &ResponseAffinityRepository{
		pool:       pool,
		hmacSecret: []byte(hmacSecret),
	}
}

func (r *ResponseAffinityRepository) UpsertResponseAffinity(ctx context.Context, responseID string, providerAccountID, routingPoolID int64, expiresAt time.Time) error {
	createdAt := time.Now().UTC()
	if err := r.validateLookupInput(responseID, routingPoolID); err != nil || providerAccountID < 1 || !expiresAt.After(createdAt) {
		return ErrInvalidResponseAffinity
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO response_affinities (
			response_id_hash, routing_pool_id, provider_account_id, created_at, expires_at
		) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (response_id_hash, routing_pool_id) DO NOTHING
	`, r.hashResponseID(responseID), routingPoolID, providerAccountID, createdAt, expiresAt.UTC())
	return err
}

func (r *ResponseAffinityRepository) FindResponseAffinity(ctx context.Context, responseID string, routingPoolID int64, now time.Time) (gateway.ResponseAffinity, bool, error) {
	if err := r.validateLookupInput(responseID, routingPoolID); err != nil || now.IsZero() {
		return gateway.ResponseAffinity{}, false, ErrInvalidResponseAffinity
	}
	var affinity gateway.ResponseAffinity
	err := r.pool.QueryRow(ctx, `
		SELECT provider_account_id
		FROM response_affinities
		WHERE response_id_hash = $1
			AND routing_pool_id = $2
			AND expires_at > $3
	`, r.hashResponseID(responseID), routingPoolID, now.UTC()).Scan(&affinity.ProviderAccountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return gateway.ResponseAffinity{}, false, nil
	}
	if err != nil {
		return gateway.ResponseAffinity{}, false, err
	}
	return affinity, true, nil
}

func (r *ResponseAffinityRepository) TryAcquireRetention(ctx context.Context) (*responseAffinityRetentionLease, bool, error) {
	if r == nil || r.pool == nil || len(r.hmacSecret) == 0 {
		return nil, false, errors.New("response affinity repository is not configured")
	}
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return nil, false, err
	}
	var acquired bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, responseAffinityRetentionAdvisoryLockID).Scan(&acquired); err != nil {
		discardResponseAffinityRetentionConnection(conn)
		return nil, false, err
	}
	if !acquired {
		conn.Release()
		return nil, false, nil
	}
	return &responseAffinityRetentionLease{conn: conn, acquireCtx: ctx}, true, nil
}

func (l *responseAffinityRetentionLease) DeleteExpiredBatch(ctx context.Context, cutoff time.Time, batchSize int) (int64, error) {
	if l == nil || l.conn == nil || cutoff.IsZero() || batchSize < 1 || batchSize > maxResponseAffinityRetentionBatchSize {
		return 0, ErrInvalidResponseAffinity
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
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), responseAffinityRetentionUnlockTimeout)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	tag, err := tx.Exec(ctx, `
		WITH candidates AS (
			SELECT response_id_hash, routing_pool_id
			FROM response_affinities
			WHERE expires_at <= $1
			ORDER BY expires_at ASC, response_id_hash ASC, routing_pool_id ASC
			FOR UPDATE SKIP LOCKED
			LIMIT $2
		)
		DELETE FROM response_affinities AS affinities
		USING candidates
		WHERE affinities.response_id_hash = candidates.response_id_hash
			AND affinities.routing_pool_id = candidates.routing_pool_id
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

func (l *responseAffinityRetentionLease) Close() error {
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
	unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(l.acquireCtx), responseAffinityRetentionUnlockTimeout)
	defer cancel()
	var unlocked bool
	err := l.conn.QueryRow(unlockCtx, `SELECT pg_advisory_unlock($1)`, responseAffinityRetentionAdvisoryLockID).Scan(&unlocked)
	if err == nil && unlocked {
		l.conn.Release()
		return nil
	}
	discardResponseAffinityRetentionConnection(l.conn)
	if err != nil {
		return err
	}
	return errors.New("response affinity retention advisory lock was not held")
}

func (r *ResponseAffinityRepository) validateLookupInput(responseID string, routingPoolID int64) error {
	if r == nil || r.pool == nil || len(r.hmacSecret) == 0 || responseID == "" || routingPoolID < 1 {
		return ErrInvalidResponseAffinity
	}
	return nil
}

func (r *ResponseAffinityRepository) hashResponseID(responseID string) []byte {
	mac := hmac.New(sha256.New, r.hmacSecret)
	_, _ = mac.Write([]byte(responseAffinityHMACDomain))
	_, _ = mac.Write([]byte(responseID))
	return mac.Sum(nil)
}

func discardResponseAffinityRetentionConnection(poolConn *pgxpool.Conn) {
	conn := poolConn.Hijack()
	closeCtx, cancel := context.WithTimeout(context.Background(), responseAffinityRetentionUnlockTimeout)
	defer cancel()
	_ = conn.Close(closeCtx)
}
