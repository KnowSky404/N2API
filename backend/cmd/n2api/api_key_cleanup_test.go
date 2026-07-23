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

type fakeAPIKeyCleanupService struct {
	mu      sync.Mutex
	calls   int
	deleted int64
	err     error
}

func (service *fakeAPIKeyCleanupService) PurgeExpiredAPIKeys(context.Context) (int64, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	service.calls++
	return service.deleted, service.err
}

func (service *fakeAPIKeyCleanupService) callCount() int {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.calls
}

type fakeAPIKeyCleanupEventRecorder struct {
	mu     sync.Mutex
	events []systemevent.Event
	err    error
}

func (recorder *fakeAPIKeyCleanupEventRecorder) Insert(_ context.Context, event systemevent.Event) error {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.events = append(recorder.events, event)
	return recorder.err
}

func (recorder *fakeAPIKeyCleanupEventRecorder) recordedEvents() []systemevent.Event {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return append([]systemevent.Event(nil), recorder.events...)
}

func TestRunAPIKeyCleanupCycleEmitsSanitizedFailureEvent(t *testing.T) {
	const storageError = "postgres password=must-not-appear"
	service := &fakeAPIKeyCleanupService{err: errors.New(storageError)}
	recorder := &fakeAPIKeyCleanupEventRecorder{}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	started := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	finished := started.Add(1250 * time.Millisecond)
	times := []time.Time{started, finished}
	now := func() time.Time {
		value := times[0]
		times = times[1:]
		return value
	}

	runAPIKeyCleanupCycle(context.Background(), service, recorder, logger, now)

	events := recorder.recordedEvents()
	if len(events) != 1 {
		t.Fatalf("failure events = %d, want 1", len(events))
	}
	event := events[0]
	if event.Category != systemevent.CategoryScheduler || event.Severity != systemevent.SeverityError ||
		event.Action != systemevent.ActionSchedulerAPIKeyPurgeFailed || event.Outcome != systemevent.OutcomeFailure ||
		event.ErrorCode != "api_key_purge_failed" || event.Message != "API key purge failed" {
		t.Fatalf("failure event = %+v", event)
	}
	if event.Target != (systemevent.Target{Type: "client_api_key_collection"}) {
		t.Fatalf("failure target = %+v", event.Target)
	}
	if event.Actor.Type != systemevent.ActorSystem || event.DurationMS != 1250 || !systemevent.ValidCorrelationID(event.CorrelationID) {
		t.Fatalf("failure context = actor %+v duration %d correlation %q", event.Actor, event.DurationMS, event.CorrelationID)
	}
	if len(event.Metadata) != 1 || event.Metadata["retention_days"] != int64(7) {
		t.Fatalf("failure metadata = %#v", event.Metadata)
	}
	if err := systemevent.ValidateEvent(event); err != nil {
		t.Fatalf("failure event validation: %v", err)
	}
	if strings.Contains(logs.String(), storageError) || strings.Contains(logs.String(), "password") {
		t.Fatalf("failure log leaked storage error: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "error_code=api_key_purge_failed") {
		t.Fatalf("failure log missing fixed error code: %s", logs.String())
	}
}

func TestRunAPIKeyCleanupCycleSuppressesCanceledFailure(t *testing.T) {
	service := &fakeAPIKeyCleanupService{err: context.Canceled}
	recorder := &fakeAPIKeyCleanupEventRecorder{}
	var logs bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	runAPIKeyCleanupCycle(ctx, service, recorder, slog.New(slog.NewTextHandler(&logs, nil)), time.Now)

	if events := recorder.recordedEvents(); len(events) != 0 {
		t.Fatalf("canceled failure events = %d, want 0", len(events))
	}
	if logs.Len() != 0 {
		t.Fatalf("canceled cleanup logged %q", logs.String())
	}
}

func TestRunAPIKeyCleanupCycleReportsCanceledMetricsOutcome(t *testing.T) {
	metrics := &captureMainTaskMetrics{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runAPIKeyCleanupCycle(ctx, &fakeAPIKeyCleanupService{err: context.Canceled}, nil, slog.Default(), time.Now, metrics)
	if len(metrics.runs) != 1 || metrics.runs[0] != [2]string{"api_key_purge", "canceled"} {
		t.Fatalf("task metrics = %+v", metrics.runs)
	}
}

func TestRunAPIKeyCleanupCycleSanitizesEventRecordingFailure(t *testing.T) {
	const storageError = "insert failed: secret sql detail"
	service := &fakeAPIKeyCleanupService{err: errors.New("purge failed")}
	recorder := &fakeAPIKeyCleanupEventRecorder{err: errors.New(storageError)}
	var logs bytes.Buffer

	runAPIKeyCleanupCycle(context.Background(), service, recorder, slog.New(slog.NewTextHandler(&logs, nil)), time.Now)

	if strings.Contains(logs.String(), storageError) || strings.Contains(logs.String(), "secret sql detail") {
		t.Fatalf("event failure log leaked storage error: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "error_code=api_key_purge_event_record_failed") {
		t.Fatalf("event failure log missing fixed error code: %s", logs.String())
	}
}

func TestRunAPIKeyCleanupRunsImmediatelyAndOnInterval(t *testing.T) {
	service := &fakeAPIKeyCleanupService{}
	recorder := &fakeAPIKeyCleanupEventRecorder{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runAPIKeyCleanup(ctx, service, recorder, 5*time.Millisecond)
	}()

	deadline := time.Now().Add(time.Second)
	for service.callCount() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cleanup runner did not stop after cancellation")
	}
	if calls := service.callCount(); calls < 2 {
		t.Fatalf("cleanup calls = %d, want immediate and interval calls", calls)
	}
	if events := recorder.recordedEvents(); len(events) != 0 {
		t.Fatalf("success path emitted %d extra events, want Store-owned completion only", len(events))
	}
}
