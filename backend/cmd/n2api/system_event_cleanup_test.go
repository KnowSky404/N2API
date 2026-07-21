package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

type systemEventDeleteResult struct {
	count int64
	err   error
}

type fakeSystemEventRetentionStore struct {
	mu            sync.Mutex
	deleteResults []systemEventDeleteResult
	deleteCalls   int
	events        []systemevent.Event
	insertErr     error
}

func (store *fakeSystemEventRetentionStore) DeleteBeforeBatch(ctx context.Context, _ time.Time, _ int) (int64, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.deleteCalls++
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if len(store.deleteResults) == 0 {
		return 0, nil
	}
	result := store.deleteResults[0]
	store.deleteResults = store.deleteResults[1:]
	return result.count, result.err
}

func (store *fakeSystemEventRetentionStore) Insert(_ context.Context, event systemevent.Event) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.events = append(store.events, event)
	return store.insertErr
}

func (store *fakeSystemEventRetentionStore) snapshot() (int, []systemevent.Event) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.deleteCalls, append([]systemevent.Event(nil), store.events...)
}

func TestRunSystemEventCleanupCycleDeletesMultipleBatchesAndPreservesSuccessEvent(t *testing.T) {
	store := &fakeSystemEventRetentionStore{deleteResults: []systemEventDeleteResult{{count: 1000}, {count: 1000}, {count: 3}}}
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)

	runSystemEventCleanupCycle(context.Background(), store, 30, slog.Default(), func() time.Time { return now })

	calls, events := store.snapshot()
	if calls != 3 || len(events) != 1 {
		t.Fatalf("delete calls/events = %d/%d, want 3/1", calls, len(events))
	}
	event := events[0]
	if event.Action != systemevent.ActionSchedulerEventRetentionCompleted || event.Category != systemevent.CategoryScheduler ||
		event.Severity != systemevent.SeverityInfo || event.Outcome != systemevent.OutcomeSuccess || event.ErrorCode != "" ||
		event.Target != (systemevent.Target{Type: "system_events", ID: "retention", Name: "System event retention"}) {
		t.Fatalf("success event = %+v", event)
	}
	if event.Metadata["deleted_count"] != int64(2003) || event.Metadata["retention_days"] != 30 ||
		event.Metadata["cutoff"] != now.Add(-30*24*time.Hour).Format(time.RFC3339) {
		t.Fatalf("success metadata = %#v", event.Metadata)
	}
}

func TestRunSystemEventCleanupCycleEmitsFullFailure(t *testing.T) {
	const deleteError = "delete failed: sql=secret-canary"
	store := &fakeSystemEventRetentionStore{deleteResults: []systemEventDeleteResult{{err: errors.New(deleteError)}}}
	var logs bytes.Buffer
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)

	runSystemEventCleanupCycle(context.Background(), store, 30, slog.New(slog.NewTextHandler(&logs, nil)), func() time.Time { return now })

	_, events := store.snapshot()
	if len(events) != 1 {
		t.Fatalf("failure events = %d, want 1", len(events))
	}
	assertSystemEventRetentionFailure(t, events[0], systemevent.SeverityError, systemevent.OutcomeFailure, 0, now, 30)
	if strings.Contains(logs.String(), deleteError) || strings.Contains(logs.String(), "secret-canary") {
		t.Fatalf("failure log leaked delete error: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "error_code=system_event_retention_failed") {
		t.Fatalf("failure log missing fixed error code: %s", logs.String())
	}
}

func TestRunSystemEventCleanupCycleEmitsPartialFailure(t *testing.T) {
	store := &fakeSystemEventRetentionStore{deleteResults: []systemEventDeleteResult{{count: 1000}, {err: errors.New("second batch failed")}}}
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)

	runSystemEventCleanupCycle(context.Background(), store, 14, slog.Default(), func() time.Time { return now })

	_, events := store.snapshot()
	if len(events) != 1 {
		t.Fatalf("failure events = %d, want 1", len(events))
	}
	assertSystemEventRetentionFailure(t, events[0], systemevent.SeverityWarning, systemevent.OutcomePartial, 1000, now, 14)
}

func TestRunSystemEventCleanupCycleSuppressesCancellation(t *testing.T) {
	store := &fakeSystemEventRetentionStore{}
	var logs bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	runSystemEventCleanupCycle(ctx, store, 30, slog.New(slog.NewTextHandler(&logs, nil)), time.Now)

	_, events := store.snapshot()
	if len(events) != 0 || logs.Len() != 0 {
		t.Fatalf("canceled cleanup events/logs = %d/%q", len(events), logs.String())
	}
}

func TestRunSystemEventCleanupCycleSanitizesFailureEventInsertError(t *testing.T) {
	const insertError = "insert failed: payload=secret-canary"
	store := &fakeSystemEventRetentionStore{
		deleteResults: []systemEventDeleteResult{{err: errors.New("delete unavailable")}},
		insertErr:     errors.New(insertError),
	}
	var logs bytes.Buffer

	runSystemEventCleanupCycle(context.Background(), store, 30, slog.New(slog.NewTextHandler(&logs, nil)), time.Now)

	if strings.Contains(logs.String(), insertError) || strings.Contains(logs.String(), "secret-canary") {
		t.Fatalf("insert failure log leaked storage error: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "error_code=system_event_retention_failure_event_record_failed") {
		t.Fatalf("insert failure log missing fixed error code: %s", logs.String())
	}
}

func TestRunSystemEventCleanupRunsImmediatelyAndOnInterval(t *testing.T) {
	store := &fakeSystemEventRetentionStore{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runSystemEventCleanup(ctx, store, 30, 5*time.Millisecond)
	}()

	deadline := time.Now().Add(time.Second)
	for {
		calls, _ := store.snapshot()
		if calls >= 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("retention runner did not stop after cancellation")
	}
	calls, events := store.snapshot()
	if calls < 2 || len(events) < 2 {
		t.Fatalf("retention calls/events = %d/%d, want immediate and interval cycles", calls, len(events))
	}
}

func assertSystemEventRetentionFailure(t *testing.T, event systemevent.Event, severity systemevent.Severity, outcome systemevent.Outcome, deleted int64, now time.Time, retentionDays int) {
	t.Helper()
	if event.Action != systemevent.ActionSchedulerEventRetentionFailed || event.Category != systemevent.CategoryScheduler ||
		event.Severity != severity || event.Outcome != outcome || event.ErrorCode != "system_event_retention_failed" ||
		event.Message != "System event retention failed" ||
		event.Target != (systemevent.Target{Type: "system_events", ID: "retention", Name: "System event retention"}) {
		t.Fatalf("failure event = %+v", event)
	}
	if event.Metadata["deleted_count"] != deleted || event.Metadata["retention_days"] != retentionDays ||
		event.Metadata["cutoff"] != now.Add(-time.Duration(retentionDays)*24*time.Hour).Format(time.RFC3339) {
		t.Fatalf("failure metadata = %#v", event.Metadata)
	}
	if err := systemevent.ValidateEvent(event); err != nil {
		t.Fatalf("failure event validation: %v", err)
	}
}
