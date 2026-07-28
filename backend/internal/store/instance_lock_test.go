package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const testInstanceAdvisoryLockID = int64(0x4e324150495453)

func TestInstanceLockSerializesProcessesAndReleases(t *testing.T) {
	repository := newTestAdminRepository(t)
	ctx := context.Background()

	first, acquired, err := acquireTestInstanceLock(ctx, repository.pool, instanceLockMonitorInterval)
	if err != nil || !acquired || first == nil {
		t.Fatalf("first acquire = lock:%v acquired:%v err:%v", first, acquired, err)
	}
	second, acquired, err := acquireTestInstanceLock(ctx, repository.pool, instanceLockMonitorInterval)
	if err != nil || acquired || second != nil {
		t.Fatalf("second acquire = lock:%v acquired:%v err:%v", second, acquired, err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first lock: %v", err)
	}

	third, acquired, err := acquireTestInstanceLock(ctx, repository.pool, instanceLockMonitorInterval)
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
	first, acquired, err := acquireTestInstanceLock(ctx, repository.pool, 10*time.Millisecond)
	if err != nil || !acquired {
		t.Fatalf("first acquire = acquired:%v err:%v", acquired, err)
	}
	if err := first.conn.Conn().PgConn().Close(ctx); err != nil {
		t.Fatalf("close lock connection: %v", err)
	}
	select {
	case <-first.Lost():
	case <-time.After(2 * time.Second):
		t.Fatal("lock connection loss was not reported")
	}
	_ = first.Close()

	second, acquired, err := acquireTestInstanceLock(ctx, repository.pool, instanceLockMonitorInterval)
	if err != nil || !acquired || second == nil {
		t.Fatalf("acquire after connection loss = lock:%v acquired:%v err:%v", second, acquired, err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("close second lock: %v", err)
	}
}

func TestInstanceLockCloseDoesNotReportConnectionLoss(t *testing.T) {
	repository := newTestAdminRepository(t)
	lock, acquired, err := acquireTestInstanceLock(context.Background(), repository.pool, 10*time.Millisecond)
	if err != nil || !acquired {
		t.Fatalf("acquire = acquired:%v err:%v", acquired, err)
	}
	if err := lock.Close(); err != nil {
		t.Fatalf("close lock: %v", err)
	}
	select {
	case <-lock.Lost():
		t.Fatal("normal close reported connection loss")
	default:
	}
}

func acquireTestInstanceLock(ctx context.Context, pool *pgxpool.Pool, monitorInterval time.Duration) (*InstanceLock, bool, error) {
	return tryAcquireInstanceLockWithID(ctx, pool, testInstanceAdvisoryLockID, monitorInterval)
}
