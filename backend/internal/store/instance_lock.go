package store

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	instanceAdvisoryLockID       = int64(0x4e324150494e53)
	instanceLockMonitorInterval  = 2 * time.Second
	instanceLockOperationTimeout = 2 * time.Second
)

type InstanceLock struct {
	conn          *pgx.Conn
	lockID        int64
	acquireCtx    context.Context
	monitorCancel context.CancelFunc
	monitorDone   chan struct{}
	lost          chan struct{}
	mu            sync.Mutex
	closed        bool
}

// TryAcquireInstanceLock takes ownership of conn on every return path. A
// successful lock owns it until Close; failed acquisition closes it.
func TryAcquireInstanceLock(ctx context.Context, conn *pgx.Conn) (*InstanceLock, bool, error) {
	return tryAcquireInstanceLock(ctx, conn, instanceLockMonitorInterval)
}

func tryAcquireInstanceLock(ctx context.Context, conn *pgx.Conn, monitorInterval time.Duration) (*InstanceLock, bool, error) {
	return tryAcquireInstanceLockWithID(ctx, conn, instanceAdvisoryLockID, monitorInterval)
}

func tryAcquireInstanceLockWithID(ctx context.Context, conn *pgx.Conn, lockID int64, monitorInterval time.Duration) (*InstanceLock, bool, error) {
	if conn == nil {
		return nil, false, errors.New("instance lock connection is not configured")
	}
	if monitorInterval <= 0 {
		monitorInterval = instanceLockMonitorInterval
	}
	var acquired bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, lockID).Scan(&acquired); err != nil {
		closePostgresConnection(conn)
		return nil, false, err
	}
	if !acquired {
		closePostgresConnection(conn)
		return nil, false, nil
	}
	monitorCtx, monitorCancel := context.WithCancel(context.Background())
	lock := &InstanceLock{
		conn:          conn,
		lockID:        lockID,
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
	connectionClosed := l.conn.PgConn().CleanupDone()
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
		err := l.conn.Ping(pingCtx)
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
	if l.conn.IsClosed() {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(l.acquireCtx), instanceLockOperationTimeout)
	var unlocked bool
	err := l.conn.QueryRow(ctx, `SELECT pg_advisory_unlock($1)`, l.lockID).Scan(&unlocked)
	cancel()
	closeErr := closePostgresConnectionWithError(l.conn)
	if err != nil {
		return errors.Join(err, closeErr)
	}
	if !unlocked {
		return errors.Join(errors.New("instance advisory lock was not held"), closeErr)
	}
	return closeErr
}

func closePostgresConnection(conn *pgx.Conn) {
	_ = closePostgresConnectionWithError(conn)
}

func closePostgresConnectionWithError(conn *pgx.Conn) error {
	if conn == nil || conn.IsClosed() {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), instanceLockOperationTimeout)
	defer cancel()
	return conn.Close(ctx)
}
