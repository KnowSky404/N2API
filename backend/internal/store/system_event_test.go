package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/systemevent"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSystemEventCursorIsAuthenticatedAndSelfContained(t *testing.T) {
	repo := NewSystemEventRepository(nil, "cursor-secret")
	want := systemEventCursor{OccurredAt: time.Unix(1234, 567).UTC(), ID: 42}
	encoded, err := repo.encodeCursor(want)
	if err != nil {
		t.Fatalf("encodeCursor returned error: %v", err)
	}
	got, err := repo.decodeCursor(encoded)
	if err != nil {
		t.Fatalf("decodeCursor returned error: %v", err)
	}
	if got.ID != want.ID || !got.OccurredAt.Equal(want.OccurredAt) {
		t.Fatalf("cursor = %+v, want %+v", got, want)
	}
	tampered := encoded[:len(encoded)-1] + "A"
	if _, err := repo.decodeCursor(tampered); err == nil {
		t.Fatal("tampered cursor accepted")
	}
}

func TestSystemEventSQLUsesDeterministicKeysetAndBoundedRetention(t *testing.T) {
	for _, want := range []string{"occurred_at", "correlation_id", "metadata"} {
		if !strings.Contains(insertSystemEventSQL, want) {
			t.Fatalf("insert SQL missing %q", want)
		}
	}
	if !strings.Contains(systemEventSelectSQL, "host(source_ip)") {
		t.Fatal("select SQL must return canonical source IP")
	}
	_ = systemevent.ActionAPIKeyCreated
}

func TestParseSystemEventNotificationID(t *testing.T) {
	for _, valid := range []string{"1", "42", strconv.FormatInt(int64(^uint64(0)>>1), 10)} {
		id, err := parseSystemEventNotificationID(valid)
		if err != nil || strconv.FormatInt(id, 10) != valid {
			t.Fatalf("parseSystemEventNotificationID(%q) = %d, %v", valid, id, err)
		}
	}
	for _, invalid := range []string{"", "0", "-1", "+1", " 1", "1 ", "abc", "9223372036854775808"} {
		if _, err := parseSystemEventNotificationID(invalid); !errors.Is(err, ErrInvalidSystemEventNotification) {
			t.Fatalf("parseSystemEventNotificationID(%q) error = %v, want ErrInvalidSystemEventNotification", invalid, err)
		}
	}
}

func TestClosedSystemEventSubscriptionRejectsWait(t *testing.T) {
	subscription := &postgresSystemEventSubscription{closed: make(chan struct{})}
	subscription.Close()
	subscription.Close()
	if _, err := subscription.Wait(context.Background()); !errors.Is(err, ErrSystemEventSubscriptionClosed) {
		t.Fatalf("Wait error = %v, want ErrSystemEventSubscriptionClosed", err)
	}
}

func TestSystemEventSubscriptionRejectsSingleConnectionPool(t *testing.T) {
	config, err := pgxpool.ParseConfig("postgres://n2api:unused@127.0.0.1:1/n2api?sslmode=disable")
	if err != nil {
		t.Fatalf("ParseConfig returned error: %v", err)
	}
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	t.Cleanup(pool.Close)
	repo := NewSystemEventRepository(pool, "cursor-secret")
	if _, err := repo.Subscribe(context.Background()); !errors.Is(err, ErrInsufficientSystemEventPoolCapacity) {
		t.Fatalf("Subscribe error = %v, want ErrInsufficientSystemEventPoolCapacity", err)
	}
}

func TestSystemEventSubscriptionObservesCommitAndIgnoresRollback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	repo := newTestSystemEventRepository(t, ctx)

	subscription, err := repo.Subscribe(ctx)
	if err != nil {
		t.Fatalf("Subscribe returned error: %v", err)
	}
	t.Cleanup(subscription.Close)

	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin system event transaction: %v", err)
	}
	committedEvent := testSystemEvent("committed-event")
	if err := InsertSystemEventTx(ctx, tx, committedEvent); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("InsertSystemEventTx returned error: %v", err)
	}

	type waitResult struct {
		id  int64
		err error
	}
	waited := make(chan waitResult, 1)
	go func() {
		id, waitErr := subscription.Wait(ctx)
		waited <- waitResult{id: id, err: waitErr}
	}()
	select {
	case result := <-waited:
		t.Fatalf("notification arrived before commit: %+v", result)
	case <-time.After(100 * time.Millisecond):
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit system event transaction: %v", err)
	}

	var result waitResult
	select {
	case result = <-waited:
	case <-ctx.Done():
		t.Fatalf("wait for committed notification: %v", ctx.Err())
	}
	if result.err != nil || result.id <= 0 {
		t.Fatalf("committed notification = %+v", result)
	}
	stored, err := repo.GetByID(ctx, result.id)
	if err != nil {
		t.Fatalf("GetByID returned error: %v", err)
	}
	if stored.CorrelationID != committedEvent.CorrelationID || stored.Action != committedEvent.Action {
		t.Fatalf("stored event = %+v, want correlation %q action %q", stored, committedEvent.CorrelationID, committedEvent.Action)
	}

	rolledBackTx, err := repo.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin rollback transaction: %v", err)
	}
	if err := InsertSystemEventTx(ctx, rolledBackTx, testSystemEvent("rolled-back-event")); err != nil {
		_ = rolledBackTx.Rollback(ctx)
		t.Fatalf("insert rolled-back system event: %v", err)
	}
	if err := rolledBackTx.Rollback(ctx); err != nil {
		t.Fatalf("rollback system event transaction: %v", err)
	}

	rollbackWaitCtx, rollbackWaitCancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer rollbackWaitCancel()
	if id, err := subscription.Wait(rollbackWaitCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait after rollback = %d, %v, want deadline exceeded", id, err)
	}
}

func testSystemEvent(correlationID string) systemevent.Event {
	return systemevent.Event{
		OccurredAt:    time.Now().UTC(),
		Category:      systemevent.CategoryAudit,
		Severity:      systemevent.SeverityInfo,
		Action:        systemevent.ActionAPIKeyCreated,
		Outcome:       systemevent.OutcomeSuccess,
		Actor:         systemevent.Actor{Type: systemevent.ActorSystem},
		Target:        systemevent.Target{Type: "client_api_key", ID: "1", Name: "test"},
		CorrelationID: correlationID,
		Metadata:      map[string]any{"source": "system_event_subscription_test"},
	}
}

func newTestSystemEventRepository(t *testing.T, ctx context.Context) *SystemEventRepository {
	t.Helper()
	dsn := os.Getenv("N2API_STORE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set N2API_STORE_TEST_DATABASE_URL to run PostgreSQL store integration tests")
	}

	adminPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect system event test database: %v", err)
	}
	t.Cleanup(adminPool.Close)
	requireStoreTestDatabase(t, ctx, adminPool)
	schema := fmt.Sprintf("system_event_store_%d", time.Now().UnixNano())
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create system event test schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, err := adminPool.Exec(cleanupCtx, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("drop system event test schema: %v", err)
		}
	})

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse system event test database URL: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	config.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("connect isolated system event test schema: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatalf("migrate isolated system event test schema: %v", err)
	}
	return NewSystemEventRepository(pool, "system-event-test-cursor-secret")
}
