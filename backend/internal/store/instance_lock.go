package store

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	instanceAdvisoryLockID = int64(0x4e324150494e53)
	instanceUnlockTimeout  = 2 * time.Second
)

type InstanceLock struct {
	conn       *pgxpool.Conn
	acquireCtx context.Context
	mu         sync.Mutex
	closed     bool
}

func TryAcquireInstanceLock(ctx context.Context, pool *pgxpool.Pool) (*InstanceLock, bool, error) {
	if pool == nil {
		return nil, false, errors.New("instance lock pool is not configured")
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, false, err
	}
	var acquired bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, instanceAdvisoryLockID).Scan(&acquired); err != nil {
		discardInstanceLockConnection(conn)
		return nil, false, err
	}
	if !acquired {
		conn.Release()
		return nil, false, nil
	}
	return &InstanceLock{conn: conn, acquireCtx: ctx}, true, nil
}

func (l *InstanceLock) Close() error {
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
	ctx, cancel := context.WithTimeout(context.WithoutCancel(l.acquireCtx), instanceUnlockTimeout)
	defer cancel()
	var unlocked bool
	err := l.conn.QueryRow(ctx, `SELECT pg_advisory_unlock($1)`, instanceAdvisoryLockID).Scan(&unlocked)
	if err == nil && unlocked {
		l.conn.Release()
		return nil
	}
	discardInstanceLockConnection(l.conn)
	if err != nil {
		return err
	}
	return errors.New("instance advisory lock was not held")
}

func discardInstanceLockConnection(poolConn *pgxpool.Conn) {
	conn := poolConn.Hijack()
	ctx, cancel := context.WithTimeout(context.Background(), instanceUnlockTimeout)
	defer cancel()
	_ = conn.Close(ctx)
}
