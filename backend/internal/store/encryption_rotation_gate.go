package store

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/encryptioninventory"
	"github.com/KnowSky404/N2API/backend/internal/encryptionrotation"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	encryptionRotationAdvisoryLockID = int64(0x4e32415049524f)
	encryptionRotationUnlockTimeout  = 2 * time.Second
)

type EncryptionRotationGateRepository struct {
	pool *pgxpool.Pool
}

type encryptionRotationGateLease struct {
	conn       *pgxpool.Conn
	acquireCtx context.Context
	mu         sync.Mutex
	closed     bool
}

func NewEncryptionRotationGateRepository(pool *pgxpool.Pool) *EncryptionRotationGateRepository {
	return &EncryptionRotationGateRepository{pool: pool}
}

func (repository *EncryptionRotationGateRepository) TryAcquire(ctx context.Context) (encryptionrotation.Lease, bool, error) {
	if repository == nil || repository.pool == nil {
		return nil, false, errors.New("encryption rotation gate repository is not configured")
	}
	conn, err := repository.pool.Acquire(ctx)
	if err != nil {
		return nil, false, err
	}
	var acquired bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, encryptionRotationAdvisoryLockID).Scan(&acquired); err != nil {
		discardEncryptionRotationConnection(conn)
		return nil, false, err
	}
	if !acquired {
		conn.Release()
		return nil, false, nil
	}
	return &encryptionRotationGateLease{conn: conn, acquireCtx: ctx}, true, nil
}

func (lease *encryptionRotationGateLease) ListEncryptedValues(ctx context.Context) ([]encryptioninventory.EncryptedValue, error) {
	if lease == nil || lease.conn == nil {
		return nil, errors.New("encryption rotation gate lease is invalid")
	}
	return listEncryptedValues(ctx, lease.conn)
}

func (lease *encryptionRotationGateLease) Close() error {
	if lease == nil || lease.conn == nil {
		return nil
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if lease.closed {
		return nil
	}
	lease.closed = true
	if lease.conn.Conn().IsClosed() {
		lease.conn.Release()
		return nil
	}
	unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(lease.acquireCtx), encryptionRotationUnlockTimeout)
	defer cancel()
	var unlocked bool
	err := lease.conn.QueryRow(unlockCtx, `SELECT pg_advisory_unlock($1)`, encryptionRotationAdvisoryLockID).Scan(&unlocked)
	if err == nil && unlocked {
		lease.conn.Release()
		return nil
	}
	discardEncryptionRotationConnection(lease.conn)
	if err != nil {
		return err
	}
	return errors.New("encryption rotation advisory lock was not held")
}

func discardEncryptionRotationConnection(poolConn *pgxpool.Conn) {
	conn := poolConn.Hijack()
	closeCtx, cancel := context.WithTimeout(context.Background(), encryptionRotationUnlockTimeout)
	defer cancel()
	_ = conn.Close(closeCtx)
}
