package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
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
	if !first.conn.IsClosed() {
		t.Fatal("closed instance lock still owns an open connection")
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
	if err := first.conn.PgConn().Close(ctx); err != nil {
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

func TestInstanceLockClosesConnectionWhenLockIsUnavailable(t *testing.T) {
	repository := newTestAdminRepository(t)
	ctx := context.Background()
	first, acquired, err := acquireTestInstanceLock(ctx, repository.pool, instanceLockMonitorInterval)
	if err != nil || !acquired {
		t.Fatalf("first acquire = acquired:%v err:%v", acquired, err)
	}
	t.Cleanup(func() { _ = first.Close() })

	secondConn, err := connectTestControlConnection(ctx, repository.pool, PostgresApplicationNameInstanceLock)
	if err != nil {
		t.Fatalf("connect second lock connection: %v", err)
	}
	second, acquired, err := tryAcquireInstanceLockWithID(ctx, secondConn, testInstanceAdvisoryLockID, instanceLockMonitorInterval)
	if err != nil || acquired || second != nil {
		t.Fatalf("second acquire = lock:%v acquired:%v err:%v", second, acquired, err)
	}
	if !secondConn.IsClosed() {
		t.Fatal("unavailable instance lock connection remained open")
	}
}

func acquireTestInstanceLock(ctx context.Context, pool *pgxpool.Pool, monitorInterval time.Duration) (*InstanceLock, bool, error) {
	conn, err := connectTestControlConnection(ctx, pool, PostgresApplicationNameInstanceLock)
	if err != nil {
		return nil, false, err
	}
	return tryAcquireInstanceLockWithID(ctx, conn, testInstanceAdvisoryLockID, monitorInterval)
}

func connectTestControlConnection(ctx context.Context, pool *pgxpool.Pool, applicationName string) (*pgx.Conn, error) {
	config := pool.Config().ConnConfig.Copy()
	config.RuntimeParams["application_name"] = applicationName
	return pgx.ConnectConfig(ctx, config)
}
