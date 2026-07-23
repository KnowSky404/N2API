package store

import (
	"context"
	"testing"
)

func TestInstanceLockSerializesProcessesAndReleases(t *testing.T) {
	repository := newTestAdminRepository(t)
	ctx := context.Background()

	first, acquired, err := TryAcquireInstanceLock(ctx, repository.pool)
	if err != nil || !acquired || first == nil {
		t.Fatalf("first acquire = lock:%v acquired:%v err:%v", first, acquired, err)
	}
	second, acquired, err := TryAcquireInstanceLock(ctx, repository.pool)
	if err != nil || acquired || second != nil {
		t.Fatalf("second acquire = lock:%v acquired:%v err:%v", second, acquired, err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first lock: %v", err)
	}

	third, acquired, err := TryAcquireInstanceLock(ctx, repository.pool)
	if err != nil || !acquired || third == nil {
		t.Fatalf("reacquire = lock:%v acquired:%v err:%v", third, acquired, err)
	}
	if err := third.Close(); err != nil {
		t.Fatalf("close reacquired lock: %v", err)
	}
}

func TestInstanceLockConnectionLossReleasesPostgresLock(t *testing.T) {
	repository := newTestAdminRepository(t)
	ctx := context.Background()
	first, acquired, err := TryAcquireInstanceLock(ctx, repository.pool)
	if err != nil || !acquired {
		t.Fatalf("first acquire = acquired:%v err:%v", acquired, err)
	}
	if err := first.conn.Conn().PgConn().Close(ctx); err != nil {
		t.Fatalf("close lock connection: %v", err)
	}
	_ = first.Close()

	second, acquired, err := TryAcquireInstanceLock(ctx, repository.pool)
	if err != nil || !acquired || second == nil {
		t.Fatalf("acquire after connection loss = lock:%v acquired:%v err:%v", second, acquired, err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("close second lock: %v", err)
	}
}
