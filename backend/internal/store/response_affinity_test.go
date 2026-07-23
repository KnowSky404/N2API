package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestResponseAffinityHashIsDomainSeparatedAndOpaque(t *testing.T) {
	repository := NewResponseAffinityRepository(nil, "response-affinity-test-secret")
	responseID := "resp_private-canary-value"

	digest := repository.hashResponseID(responseID)
	if len(digest) != 32 {
		t.Fatalf("response affinity digest length = %d, want 32", len(digest))
	}
	if bytes.Equal(digest, []byte(responseID)) || bytes.Contains(digest, []byte(responseID)) {
		t.Fatal("response affinity digest contains the raw identifier")
	}
	if bytes.Equal(digest, NewResponseAffinityRepository(nil, "different-secret").hashResponseID(responseID)) {
		t.Fatal("response affinity digest does not depend on its HMAC secret")
	}
	if bytes.Equal(digest, repository.hashResponseID("different-response")) {
		t.Fatal("response affinity digest does not depend on its response identifier")
	}
}

func TestResponseAffinityRepositoryRejectsInvalidInput(t *testing.T) {
	ctx := context.Background()
	repository := NewResponseAffinityRepository(nil, "secret")
	now := time.Now().UTC()

	if err := repository.UpsertResponseAffinity(ctx, "response", 1, 1, now.Add(time.Hour)); !errors.Is(err, ErrInvalidResponseAffinity) {
		t.Fatalf("unconfigured upsert error = %v, want ErrInvalidResponseAffinity", err)
	}
	if _, _, err := repository.FindResponseAffinity(ctx, "response", 1, now); !errors.Is(err, ErrInvalidResponseAffinity) {
		t.Fatalf("unconfigured lookup error = %v, want ErrInvalidResponseAffinity", err)
	}
	configured := NewResponseAffinityRepository(&pgxpool.Pool{}, "secret")
	if err := configured.UpsertResponseAffinity(ctx, "response", 1, 1, now.Add(-time.Minute)); !errors.Is(err, ErrInvalidResponseAffinity) {
		t.Fatalf("expired upsert error = %v, want ErrInvalidResponseAffinity", err)
	}
	for _, batchSize := range []int{0, maxResponseAffinityRetentionBatchSize + 1} {
		lease := &responseAffinityRetentionLease{}
		if _, err := lease.DeleteExpiredBatch(ctx, now, batchSize); !errors.Is(err, ErrInvalidResponseAffinity) {
			t.Fatalf("invalid batch size %d error = %v, want ErrInvalidResponseAffinity", batchSize, err)
		}
	}
}

func TestResponseAffinityRepositoryUpsertsFindsAndScopesOpaqueBindings(t *testing.T) {
	repository, pool := newResponseAffinityRepositoryForTest(t)
	ctx := context.Background()
	primaryPoolID := insertResponseAffinityRoutingPool(t, pool, "affinity-primary")
	otherPoolID := insertResponseAffinityRoutingPool(t, pool, "affinity-other")
	firstAccountID := insertProviderAccount(t, pool, "openai", "api_upstream", "affinity-first")
	secondAccountID := insertProviderAccount(t, pool, "openai", "api_upstream", "affinity-second")
	responseID := "resp_store-opaque-canary"
	expiresAt := time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour)

	if err := repository.UpsertResponseAffinity(ctx, responseID, firstAccountID, primaryPoolID, expiresAt); err != nil {
		t.Fatalf("UpsertResponseAffinity returned error: %v", err)
	}
	if err := repository.UpsertResponseAffinity(ctx, responseID, secondAccountID, primaryPoolID, expiresAt.Add(time.Hour)); err != nil {
		t.Fatalf("duplicate UpsertResponseAffinity returned error: %v", err)
	}
	if err := repository.UpsertResponseAffinity(ctx, responseID, secondAccountID, otherPoolID, expiresAt); err != nil {
		t.Fatalf("scoped UpsertResponseAffinity returned error: %v", err)
	}

	first, found, err := repository.FindResponseAffinity(ctx, responseID, primaryPoolID, expiresAt.Add(-time.Minute))
	if err != nil || !found || first.ProviderAccountID != firstAccountID {
		t.Fatalf("primary lookup = affinity:%+v found:%v err:%v, want account %d", first, found, err, firstAccountID)
	}
	second, found, err := repository.FindResponseAffinity(ctx, responseID, otherPoolID, expiresAt.Add(-time.Minute))
	if err != nil || !found || second.ProviderAccountID != secondAccountID {
		t.Fatalf("other-scope lookup = affinity:%+v found:%v err:%v, want account %d", second, found, err, secondAccountID)
	}
	if _, found, err := repository.FindResponseAffinity(ctx, responseID, primaryPoolID, expiresAt); err != nil || found {
		t.Fatalf("expired lookup = found:%v err:%v, want miss", found, err)
	}

	var storedHash []byte
	var storedCreatedAt, storedExpiresAt time.Time
	if err := pool.QueryRow(ctx, `
		SELECT response_id_hash, created_at, expires_at
		FROM response_affinities
		WHERE response_id_hash = $1 AND routing_pool_id = $2
	`, repository.hashResponseID(responseID), primaryPoolID).Scan(&storedHash, &storedCreatedAt, &storedExpiresAt); err != nil {
		t.Fatalf("inspect stored affinity: %v", err)
	}
	if len(storedHash) != 32 || bytes.Contains(storedHash, []byte(responseID)) {
		t.Fatal("stored affinity hash exposes the raw response identifier")
	}
	if storedCreatedAt.IsZero() || !storedExpiresAt.Equal(expiresAt) {
		t.Fatalf("stored timestamps = created:%s expires:%s, want expiry %s", storedCreatedAt, storedExpiresAt, expiresAt)
	}
}

func TestResponseAffinityRepositoryConcurrentUpsertKeepsFirstBinding(t *testing.T) {
	repository, pool := newResponseAffinityRepositoryForTest(t)
	ctx := context.Background()
	routingPoolID := insertResponseAffinityRoutingPool(t, pool, "affinity-concurrent")
	accountIDs := []int64{
		insertProviderAccount(t, pool, "openai", "api_upstream", "affinity-concurrent-a"),
		insertProviderAccount(t, pool, "openai", "api_upstream", "affinity-concurrent-b"),
	}
	responseID := "resp_concurrent-affinity"
	expiresAt := time.Now().UTC().Add(time.Hour)
	start := make(chan struct{})
	errs := make(chan error, 24)
	var workers sync.WaitGroup
	for i := 0; i < cap(errs); i++ {
		accountID := accountIDs[i%len(accountIDs)]
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			errs <- repository.UpsertResponseAffinity(ctx, responseID, accountID, routingPoolID, expiresAt)
		}()
	}
	close(start)
	workers.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent UpsertResponseAffinity returned error: %v", err)
		}
	}

	affinity, found, err := repository.FindResponseAffinity(ctx, responseID, routingPoolID, time.Now().UTC())
	if err != nil || !found || (affinity.ProviderAccountID != accountIDs[0] && affinity.ProviderAccountID != accountIDs[1]) {
		t.Fatalf("concurrent lookup = affinity:%+v found:%v err:%v", affinity, found, err)
	}
	losingAccountID := accountIDs[0]
	if affinity.ProviderAccountID == losingAccountID {
		losingAccountID = accountIDs[1]
	}
	if err := repository.UpsertResponseAffinity(ctx, responseID, losingAccountID, routingPoolID, expiresAt.Add(time.Hour)); err != nil {
		t.Fatalf("post-concurrency upsert returned error: %v", err)
	}
	stable, found, err := repository.FindResponseAffinity(ctx, responseID, routingPoolID, time.Now().UTC())
	if err != nil || !found || stable.ProviderAccountID != affinity.ProviderAccountID {
		t.Fatalf("binding changed after duplicate upsert: before=%+v after=%+v found:%v err:%v", affinity, stable, found, err)
	}
}

func TestResponseAffinityRepositoryCascadesDeletedScopeAndAccount(t *testing.T) {
	repository, pool := newResponseAffinityRepositoryForTest(t)
	ctx := context.Background()
	routingPoolID := insertResponseAffinityRoutingPool(t, pool, "affinity-cascade-account")
	accountID := insertProviderAccount(t, pool, "openai", "api_upstream", "affinity-cascade-account")
	expiresAt := time.Now().UTC().Add(time.Hour)
	if err := repository.UpsertResponseAffinity(ctx, "resp_account-cascade", accountID, routingPoolID, expiresAt); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM provider_accounts WHERE id = $1`, accountID); err != nil {
		t.Fatalf("delete provider account: %v", err)
	}
	if _, found, err := repository.FindResponseAffinity(ctx, "resp_account-cascade", routingPoolID, time.Now().UTC()); err != nil || found {
		t.Fatalf("account cascade lookup = found:%v err:%v, want miss", found, err)
	}

	otherPoolID := insertResponseAffinityRoutingPool(t, pool, "affinity-cascade-pool")
	otherAccountID := insertProviderAccount(t, pool, "openai", "api_upstream", "affinity-cascade-pool")
	if err := repository.UpsertResponseAffinity(ctx, "resp_pool-cascade", otherAccountID, otherPoolID, expiresAt); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM routing_pools WHERE id = $1`, otherPoolID); err != nil {
		t.Fatalf("delete routing pool: %v", err)
	}
	var remaining int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM response_affinities`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("remaining affinities after cascades = %d, err:%v", remaining, err)
	}
}

func TestResponseAffinityRetentionDeletesOldestBoundedBatch(t *testing.T) {
	repository, pool := newResponseAffinityRepositoryForTest(t)
	ctx := context.Background()
	routingPoolID := insertResponseAffinityRoutingPool(t, pool, "affinity-retention")
	accountID := insertProviderAccount(t, pool, "openai", "api_upstream", "affinity-retention")
	now := time.Now().UTC()
	responseIDs := []string{"resp_retention-oldest", "resp_retention-middle", "resp_retention-newest"}
	for index, responseID := range responseIDs {
		if err := repository.UpsertResponseAffinity(ctx, responseID, accountID, routingPoolID, now.Add(time.Duration(index+1)*time.Minute)); err != nil {
			t.Fatalf("insert retention fixture %d: %v", index, err)
		}
	}
	lease, acquired, err := repository.TryAcquireRetention(ctx)
	if err != nil || !acquired {
		t.Fatalf("TryAcquireRetention = acquired:%v err:%v", acquired, err)
	}
	t.Cleanup(func() { _ = lease.Close() })
	deleted, err := lease.DeleteExpiredBatch(ctx, now.Add(4*time.Minute), 2)
	if err != nil || deleted != 2 {
		t.Fatalf("DeleteExpiredBatch = deleted:%d err:%v, want 2", deleted, err)
	}
	for index, responseID := range responseIDs {
		var exists bool
		if err := pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM response_affinities
				WHERE response_id_hash = $1 AND routing_pool_id = $2
			)
		`, repository.hashResponseID(responseID), routingPoolID).Scan(&exists); err != nil {
			t.Fatalf("inspect retention fixture %d: %v", index, err)
		}
		if exists != (index == len(responseIDs)-1) {
			t.Fatalf("retention fixture %d exists = %v", index, exists)
		}
	}
	deleted, err = lease.DeleteExpiredBatch(ctx, now.Add(4*time.Minute), 2)
	if err != nil || deleted != 1 {
		t.Fatalf("second DeleteExpiredBatch = deleted:%d err:%v, want 1", deleted, err)
	}
}

func TestResponseAffinityRetentionSerializesWorkersAndHonorsCancellation(t *testing.T) {
	repository, _ := newResponseAffinityRepositoryForTest(t)
	ctx := context.Background()
	first, acquired, err := repository.TryAcquireRetention(ctx)
	if err != nil || !acquired {
		t.Fatalf("first retention acquire = acquired:%v err:%v", acquired, err)
	}
	second, acquired, err := repository.TryAcquireRetention(ctx)
	if err != nil || acquired || second != nil {
		t.Fatalf("contended retention acquire = lease:%v acquired:%v err:%v", second, acquired, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := first.DeleteExpiredBatch(canceled, time.Now().UTC(), 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled retention delete error = %v, want context.Canceled", err)
	}
	if _, err := first.DeleteExpiredBatch(ctx, time.Time{}, 1); !errors.Is(err, ErrInvalidResponseAffinity) {
		t.Fatalf("zero cutoff error = %v, want ErrInvalidResponseAffinity", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first retention lease: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	var third *responseAffinityRetentionLease
	for !acquired && time.Now().Before(deadline) {
		third, acquired, err = repository.TryAcquireRetention(ctx)
		if err != nil {
			t.Fatalf("retention reacquire returned error: %v", err)
		}
		if !acquired {
			time.Sleep(5 * time.Millisecond)
		}
	}
	if !acquired || third == nil {
		t.Fatal("retention lock was not reacquired after release")
	}
	if err := third.Close(); err != nil {
		t.Fatalf("close reacquired retention lease: %v", err)
	}
}

func newResponseAffinityRepositoryForTest(t *testing.T) (*ResponseAffinityRepository, *pgxpool.Pool) {
	t.Helper()
	adminRepository := newTestAdminRepository(t)
	return NewResponseAffinityRepository(adminRepository.pool, "response-affinity-store-test-secret"), adminRepository.pool
}

func insertResponseAffinityRoutingPool(t *testing.T, pool *pgxpool.Pool, suffix string) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO routing_pools (name, description)
		VALUES ($1, $2)
		RETURNING id
	`, fmt.Sprintf("pool-%s", suffix), "response affinity test pool").Scan(&id); err != nil {
		t.Fatalf("insert response affinity routing pool: %v", err)
	}
	return id
}
