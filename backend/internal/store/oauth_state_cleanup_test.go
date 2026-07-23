package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestOAuthStateCleanupRepositoryDeletesOnlyEligibleRowsInBatches(t *testing.T) {
	adminRepo := newTestAdminRepository(t)
	repo := NewOAuthStateCleanupRepository(adminRepo.pool)
	ctx := context.Background()
	cutoff := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	insertOAuthCleanupState(t, adminRepo.pool, "expired", cutoff.Add(-time.Hour), nil)
	consumedAt := cutoff.Add(-time.Minute)
	insertOAuthCleanupState(t, adminRepo.pool, "consumed", cutoff.Add(time.Hour), &consumedAt)
	insertOAuthCleanupState(t, adminRepo.pool, "active", cutoff.Add(time.Hour), nil)
	futureConsumedAt := cutoff.Add(time.Minute)
	insertOAuthCleanupState(t, adminRepo.pool, "future-consumed", cutoff.Add(time.Hour), &futureConsumedAt)

	lease, acquired, err := repo.TryAcquire(ctx)
	if err != nil || !acquired {
		t.Fatalf("TryAcquire = acquired:%v err:%v", acquired, err)
	}
	count, err := lease.CountEligible(ctx, cutoff)
	if err != nil || count != 2 {
		t.Fatalf("CountEligible = %d, %v", count, err)
	}
	for batch, want := range []int64{1, 1, 0, 0} {
		deleted, err := lease.DeleteEligibleBatch(ctx, cutoff, 1)
		if err != nil || deleted != want {
			t.Fatalf("batch %d = deleted:%d err:%v, want %d", batch, deleted, err, want)
		}
	}
	if err := lease.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	var remaining int
	if err := adminRepo.pool.QueryRow(ctx, `SELECT count(*) FROM oauth_states`).Scan(&remaining); err != nil || remaining != 2 {
		t.Fatalf("remaining=%d err=%v", remaining, err)
	}
}

func TestOAuthStateCleanupRepositorySerializesWorkersAndHonorsCancellation(t *testing.T) {
	adminRepo := newTestAdminRepository(t)
	repo := NewOAuthStateCleanupRepository(adminRepo.pool)
	ctx := context.Background()
	first, acquired, err := repo.TryAcquire(ctx)
	if err != nil || !acquired {
		t.Fatalf("first acquire = %v, %v", acquired, err)
	}
	second, acquired, err := repo.TryAcquire(ctx)
	if err != nil || acquired || second != nil {
		t.Fatalf("second acquire = lease:%v acquired:%v err:%v", second, acquired, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := first.DeleteEligibleBatch(canceled, time.Now(), 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled delete error = %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first Close returned error: %v", err)
	}
	third, acquired, err := repo.TryAcquire(ctx)
	if err != nil || !acquired {
		t.Fatalf("reacquire = %v, %v", acquired, err)
	}
	if err := third.Close(); err != nil {
		t.Fatalf("third Close returned error: %v", err)
	}
}

func insertOAuthCleanupState(t *testing.T, pool interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, suffix string, expiresAt time.Time, consumedAt *time.Time) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO oauth_states (
			provider, state_hash, redirect_after, expires_at, consumed_at,
			encrypted_code_verifier, code_verifier_hash
		) VALUES ('openai', $1, '/', $2, $3, 'cleanup-ciphertext', $4)
	`, "cleanup-"+suffix, expiresAt, consumedAt, "cleanup-verifier-"+suffix)
	if err != nil {
		t.Fatalf("insert OAuth state %s: %v", suffix, err)
	}
}
