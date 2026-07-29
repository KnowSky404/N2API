package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

const (
	migrationAdvisoryLockID       = int64(0x4e324150494d47)
	migrationLockMonitorInterval  = 250 * time.Millisecond
	migrationLockOperationTimeout = 2 * time.Second
)

var ErrMigrationLockLost = errors.New("migration advisory lock connection lost")

type MigrationLock struct {
	conn          *pgx.Conn
	lockID        int64
	acquireCtx    context.Context
	monitorCancel context.CancelFunc
	monitorDone   chan struct{}
	lost          chan struct{}
	lostOnce      sync.Once
	mu            sync.Mutex
	closed        bool
}

// AcquireMigrationLock takes ownership of conn on every return path. The lock
// remains held until Close so callers can serialize migrations and bootstrap.
func AcquireMigrationLock(ctx context.Context, conn *pgx.Conn) (*MigrationLock, error) {
	return acquireMigrationLockWithID(ctx, conn, migrationAdvisoryLockID)
}

func acquireMigrationLockWithID(ctx context.Context, conn *pgx.Conn, lockID int64) (*MigrationLock, error) {
	if conn == nil {
		return nil, errors.New("migration lock connection is not configured")
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, lockID); err != nil {
		closePostgresConnection(conn)
		return nil, fmt.Errorf("acquire migration advisory lock: %w", err)
	}
	monitorCtx, monitorCancel := context.WithCancel(context.Background())
	lock := &MigrationLock{
		conn:          conn,
		lockID:        lockID,
		acquireCtx:    ctx,
		monitorCancel: monitorCancel,
		monitorDone:   make(chan struct{}),
		lost:          make(chan struct{}),
	}
	go lock.monitor(monitorCtx, migrationLockMonitorInterval)
	return lock, nil
}

func (l *MigrationLock) Lost() <-chan struct{} {
	if l == nil {
		return nil
	}
	return l.lost
}

func (l *MigrationLock) monitor(ctx context.Context, interval time.Duration) {
	defer close(l.monitorDone)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	connectionClosed := l.conn.PgConn().CleanupDone()
	for {
		select {
		case <-ctx.Done():
			return
		case <-connectionClosed:
			l.markLost()
			return
		case <-ticker.C:
		}

		l.mu.Lock()
		if l.closed {
			l.mu.Unlock()
			return
		}
		pingCtx, cancel := context.WithTimeout(ctx, migrationLockOperationTimeout)
		err := l.conn.Ping(pingCtx)
		cancel()
		stopping := ctx.Err() != nil || l.closed
		l.mu.Unlock()
		if err == nil {
			continue
		}
		if !stopping {
			l.markLost()
		}
		return
	}
}

func (l *MigrationLock) markLost() {
	l.lostOnce.Do(func() { close(l.lost) })
}

func (l *MigrationLock) Close() error {
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
	lost := l.isLost()
	if l.conn.IsClosed() {
		l.markLost()
		return ErrMigrationLockLost
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(l.acquireCtx), migrationLockOperationTimeout)
	var unlocked bool
	err := l.conn.QueryRow(ctx, `SELECT pg_advisory_unlock($1)`, l.lockID).Scan(&unlocked)
	cancel()
	closeErr := closePostgresConnectionWithError(l.conn)
	if err != nil {
		return errors.Join(err, closeErr)
	}
	if !unlocked {
		return errors.Join(errors.New("migration advisory lock was not held"), closeErr)
	}
	if lost {
		return errors.Join(ErrMigrationLockLost, closeErr)
	}
	return closeErr
}

func (l *MigrationLock) isLost() bool {
	select {
	case <-l.lost:
		return true
	default:
		return false
	}
}

func MigrationSQL(name string) (string, error) {
	data, err := migrationFS.ReadFile("migrations/" + name)
	if err != nil {
		return "", fmt.Errorf("read migration %s: %w", name, err)
	}
	return string(data), nil
}

func RunMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	return runMigrations(ctx, db)
}

func runMigrations(ctx context.Context, db *sql.DB) error {
	provider, err := newMigrationProvider(db)
	if err != nil {
		return err
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}

// RunMigrationsDownTo rolls back applied migrations newer than targetVersion.
func RunMigrationsDownTo(ctx context.Context, pool *pgxpool.Pool, targetVersion int64) error {
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	provider, err := newMigrationProvider(db)
	if err != nil {
		return err
	}
	if _, err := provider.DownTo(ctx, targetVersion); err != nil {
		return fmt.Errorf("roll back migrations to version %d: %w", targetVersion, err)
	}
	return nil
}

func newMigrationProvider(db *sql.DB) (*goose.Provider, error) {
	migrations, err := migrationDirFS()
	if err != nil {
		return nil, err
	}
	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		db,
		migrations,
		goose.WithTableName("schema_migrations"),
		goose.WithDisableGlobalRegistry(true),
	)
	if err != nil {
		return nil, fmt.Errorf("create migration provider: %w", err)
	}
	return provider, nil
}

func migrationDirFS() (fs.FS, error) {
	migrations, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("open migration directory: %w", err)
	}
	return migrations, nil
}
