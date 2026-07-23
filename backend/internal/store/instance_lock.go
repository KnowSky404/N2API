package store

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	instanceAdvisoryLockID       = int64(0x4e324150494e53)
	instanceLockMonitorInterval  = 2 * time.Second
	instanceLockOperationTimeout = 2 * time.Second
)

type InstanceLock struct {
	conn          *pgxpool.Conn
	acquireCtx    context.Context
	monitorCancel context.CancelFunc
	monitorDone   chan struct{}
	lost          chan struct{}
	mu            sync.Mutex
	closed        bool
}

func TryAcquireInstanceLock(ctx context.Context, pool *pgxpool.Pool) (*InstanceLock, bool, error) {
	return tryAcquireInstanceLock(ctx, pool, instanceLockMonitorInterval)
}

func tryAcquireInstanceLock(ctx context.Context, pool *pgxpool.Pool, monitorInterval time.Duration) (*InstanceLock, bool, error) {
	if pool == nil {
		return nil, false, errors.New("instance lock pool is not configured")
	}
	if monitorInterval <= 0 {
		monitorInterval = instanceLockMonitorInterval
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
	monitorCtx, monitorCancel := context.WithCancel(context.Background())
	lock := &InstanceLock{
		conn:          conn,
		acquireCtx:    ctx,
		monitorCancel: monitorCancel,
		monitorDone:   make(chan struct{}),
		lost:          make(chan struct{}),
	}
	go lock.monitor(monitorCtx, monitorInterval)
	return lock, true, nil
}

func (l *InstanceLock) Lost() <-chan struct{} {
	if l == nil {
		return nil
	}
	return l.lost
}

func (l *InstanceLock) monitor(ctx context.Context, interval time.Duration) {
	defer close(l.monitorDone)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	connectionClosed := l.conn.Conn().PgConn().CleanupDone()
	for {
		select {
		case <-ctx.Done():
			return
		case <-connectionClosed:
			close(l.lost)
			return
		case <-ticker.C:
		}

		l.mu.Lock()
		if l.closed {
			l.mu.Unlock()
			return
		}
		pingCtx, cancel := context.WithTimeout(ctx, instanceLockOperationTimeout)
		err := l.conn.Conn().Ping(pingCtx)
		cancel()
		stopping := ctx.Err() != nil || l.closed
		l.mu.Unlock()
		if err == nil {
			continue
		}
		if !stopping {
			close(l.lost)
		}
		return
	}
}

func (l *InstanceLock) Close() error {
	if l == nil || l.conn == nil {
		return nil
	}
	l.monitorCancel()
	<-l.monitorDone
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
	ctx, cancel := context.WithTimeout(context.WithoutCancel(l.acquireCtx), instanceLockOperationTimeout)
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
	ctx, cancel := context.WithTimeout(context.Background(), instanceLockOperationTimeout)
	defer cancel()
	_ = conn.Close(ctx)
}
