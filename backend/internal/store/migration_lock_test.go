package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

const testMigrationAdvisoryLockID = int64(0x4e32415049544d)

func TestMigrationLockSerializesAndOwnsConnection(t *testing.T) {
	repository := newTestAdminRepository(t)
	ctx := context.Background()
	firstConn, err := connectTestControlConnection(ctx, repository.pool, PostgresApplicationNameMigrationLock)
	if err != nil {
		t.Fatalf("connect first migration connection: %v", err)
	}
	first, err := acquireMigrationLockWithID(ctx, firstConn, testMigrationAdvisoryLockID)
	if err != nil {
		t.Fatalf("acquire first migration lock: %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })

	secondConn, err := connectTestControlConnection(ctx, repository.pool, PostgresApplicationNameMigrationLock)
	if err != nil {
		t.Fatalf("connect second migration connection: %v", err)
	}
	result := make(chan struct {
		lock *MigrationLock
		err  error
	}, 1)
	go func() {
		lock, lockErr := acquireMigrationLockWithID(ctx, secondConn, testMigrationAdvisoryLockID)
		result <- struct {
			lock *MigrationLock
			err  error
		}{lock: lock, err: lockErr}
	}()

	select {
	case got := <-result:
		if got.lock != nil {
			_ = got.lock.Close()
		}
		t.Fatalf("second migration lock completed before release: %v", got.err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first migration lock: %v", err)
	}
	if !firstConn.IsClosed() {
		t.Fatal("closed migration lock still owns an open connection")
	}

	select {
	case got := <-result:
		if got.err != nil || got.lock == nil {
			t.Fatalf("second migration lock after release = lock:%v err:%v", got.lock, got.err)
		}
		if err := got.lock.Close(); err != nil {
			t.Fatalf("close second migration lock: %v", err)
		}
		if !secondConn.IsClosed() {
			t.Fatal("second migration lock connection remained open")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second migration lock did not acquire after release")
	}
}

func TestMigrationLockReportsConnectionLoss(t *testing.T) {
	repository := newTestAdminRepository(t)
	ctx := context.Background()
	conn, err := connectTestControlConnection(ctx, repository.pool, PostgresApplicationNameMigrationLock)
	if err != nil {
		t.Fatalf("connect migration lock connection: %v", err)
	}
	var backendPID int32
	if err := conn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&backendPID); err != nil {
		t.Fatalf("read migration lock backend PID: %v", err)
	}
	lock, err := acquireMigrationLockWithID(ctx, conn, testMigrationAdvisoryLockID)
	if err != nil {
		t.Fatalf("acquire migration lock: %v", err)
	}

	var terminated bool
	if err := repository.pool.QueryRow(ctx, `SELECT pg_terminate_backend($1)`, backendPID).Scan(&terminated); err != nil || !terminated {
		t.Fatalf("terminate migration lock backend = terminated:%v err:%v", terminated, err)
	}
	select {
	case <-lock.Lost():
	case <-time.After(2 * time.Second):
		t.Fatal("migration lock did not report connection loss")
	}
	if err := lock.Close(); !errors.Is(err, ErrMigrationLockLost) {
		t.Fatalf("Close after connection loss = %v, want ErrMigrationLockLost", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatalf("second Close after connection loss = %v", err)
	}
}

func TestMigrationLockClosesConnectionWhenAcquireFails(t *testing.T) {
	repository := newTestAdminRepository(t)
	ctx := context.Background()
	firstConn, err := connectTestControlConnection(ctx, repository.pool, PostgresApplicationNameMigrationLock)
	if err != nil {
		t.Fatalf("connect first migration connection: %v", err)
	}
	first, err := acquireMigrationLockWithID(ctx, firstConn, testMigrationAdvisoryLockID)
	if err != nil {
		t.Fatalf("acquire first migration lock: %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })

	secondConn, err := connectTestControlConnection(ctx, repository.pool, PostgresApplicationNameMigrationLock)
	if err != nil {
		t.Fatalf("connect second migration connection: %v", err)
	}
	acquireCtx, cancel := context.WithTimeout(ctx, 25*time.Millisecond)
	defer cancel()
	lock, err := acquireMigrationLockWithID(acquireCtx, secondConn, testMigrationAdvisoryLockID)
	if err == nil || lock != nil {
		t.Fatalf("timed out migration lock = lock:%v err:%v", lock, err)
	}
	if !secondConn.IsClosed() {
		t.Fatal("failed migration lock connection remained open")
	}
}

func TestMigrationLockCloseReportsObservedLoss(t *testing.T) {
	repository := newTestAdminRepository(t)
	ctx := context.Background()
	conn, err := connectTestControlConnection(ctx, repository.pool, PostgresApplicationNameMigrationLock)
	if err != nil {
		t.Fatalf("connect migration connection: %v", err)
	}
	lock, err := acquireMigrationLockWithID(ctx, conn, testMigrationAdvisoryLockID)
	if err != nil {
		t.Fatalf("acquire migration lock: %v", err)
	}
	lock.markLost()
	if err := lock.Close(); !errors.Is(err, ErrMigrationLockLost) {
		t.Fatalf("Close after observed loss error = %v, want ErrMigrationLockLost", err)
	}
	if !conn.IsClosed() {
		t.Fatal("observed-loss migration connection remained open")
	}
}
