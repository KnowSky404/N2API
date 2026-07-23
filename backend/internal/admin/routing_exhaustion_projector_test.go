package admin

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeRoutingExhaustionProjectorStore struct {
	mu      sync.Mutex
	results []RoutingExhaustionProjectorCycleResult
	err     error
	calls   int
	limits  [][2]int
	callCh  chan struct{}
}

func (s *fakeRoutingExhaustionProjectorStore) RunRoutingExhaustionProjectorCycle(_ context.Context, batchLimit, transitionLimit int, _ time.Time) (RoutingExhaustionProjectorCycleResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.limits = append(s.limits, [2]int{batchLimit, transitionLimit})
	if s.callCh != nil {
		select {
		case s.callCh <- struct{}{}:
		default:
		}
	}
	if s.err != nil {
		return RoutingExhaustionProjectorCycleResult{}, s.err
	}
	if len(s.results) == 0 {
		return RoutingExhaustionProjectorCycleResult{}, nil
	}
	result := s.results[0]
	s.results = s.results[1:]
	return result, nil
}

func TestRoutingExhaustionProjectorDefaultsAndStatus(t *testing.T) {
	store := &fakeRoutingExhaustionProjectorStore{results: []RoutingExhaustionProjectorCycleResult{{
		Processed: 750, Transitions: 4, LastRequestLogID: 900,
	}}}
	projector := NewRoutingExhaustionProjector(store, RoutingExhaustionProjectorConfig{}, slog.Default())
	if projector.cfg.Interval != time.Minute || projector.cfg.BatchSize != 1000 || projector.cfg.TransitionLimit != 100 {
		t.Fatalf("projector defaults = interval %s batch %d transitions %d", projector.cfg.Interval, projector.cfg.BatchSize, projector.cfg.TransitionLimit)
	}
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	projector.now = func() time.Time { return now }
	projector.runCycle(context.Background())
	status := projector.Status()
	if status.Running || status.LastSucceededAt == nil || !status.LastSucceededAt.Equal(now) || status.LastErrorCode != "" ||
		status.LastProcessed != 750 || status.LastTransitions != 4 || status.LastRequestLogID != 900 {
		t.Fatalf("projector status = %+v", status)
	}
	if len(store.limits) != 1 || store.limits[0] != [2]int{1000, 100} {
		t.Fatalf("projector limits = %+v", store.limits)
	}
}

func TestRoutingExhaustionProjectorReportsTaskMetrics(t *testing.T) {
	metrics := &captureAdminTaskMetrics{}
	projector := NewRoutingExhaustionProjector(&fakeRoutingExhaustionProjectorStore{}, RoutingExhaustionProjectorConfig{}, slog.Default())
	projector.SetMetricsObserver(metrics)
	projector.runCycle(context.Background())
	if len(metrics.runs) != 1 || metrics.runs[0] != [2]string{"routing_exhaustion_projector", "success"} {
		t.Fatalf("task metrics = %+v", metrics.runs)
	}
}

func TestRoutingExhaustionProjectorTreatsContentionAsSkip(t *testing.T) {
	store := &fakeRoutingExhaustionProjectorStore{results: []RoutingExhaustionProjectorCycleResult{{Contended: true}}}
	projector := NewRoutingExhaustionProjector(store, RoutingExhaustionProjectorConfig{}, slog.Default())
	projector.runCycle(context.Background())
	status := projector.Status()
	if status.Running || status.LastSucceededAt != nil || status.LastErrorAt != nil || status.LastErrorCode != "" {
		t.Fatalf("contended projector status = %+v", status)
	}
}

func TestRoutingExhaustionProjectorFailureIsSanitized(t *testing.T) {
	var output bytes.Buffer
	store := &fakeRoutingExhaustionProjectorStore{err: errors.New("SECRET_DATABASE_ERROR_CANARY")}
	projector := NewRoutingExhaustionProjector(store, RoutingExhaustionProjectorConfig{}, slog.New(slog.NewJSONHandler(&output, nil)))
	projector.runCycle(context.Background())
	status := projector.Status()
	if status.Running || status.LastErrorAt == nil || status.LastErrorCode != "routing_exhaustion_projector_failed" {
		t.Fatalf("failed projector status = %+v", status)
	}
	if strings.Contains(output.String(), "SECRET_DATABASE_ERROR_CANARY") || !strings.Contains(output.String(), "routing_exhaustion_projector_failed") {
		t.Fatalf("projector log was not sanitized: %s", output.String())
	}
}

func TestRoutingExhaustionProjectorShutdownCancellationIsQuiet(t *testing.T) {
	var output bytes.Buffer
	store := &fakeRoutingExhaustionProjectorStore{err: context.Canceled}
	projector := NewRoutingExhaustionProjector(store, RoutingExhaustionProjectorConfig{}, slog.New(slog.NewJSONHandler(&output, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	projector.runCycle(ctx)
	if output.Len() != 0 {
		t.Fatalf("shutdown cancellation logged: %s", output.String())
	}
	if status := projector.Status(); status.Running || status.LastErrorCode != "routing_exhaustion_projector_canceled" {
		t.Fatalf("canceled projector status = %+v", status)
	}
}

func TestRoutingExhaustionProjectorRunsImmediatelyAndRepeats(t *testing.T) {
	store := &fakeRoutingExhaustionProjectorStore{callCh: make(chan struct{}, 2)}
	projector := NewRoutingExhaustionProjector(store, RoutingExhaustionProjectorConfig{Interval: 5 * time.Millisecond}, slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		projector.Run(ctx)
	}()
	for cycle := 0; cycle < 2; cycle++ {
		select {
		case <-store.callCh:
		case <-time.After(time.Second):
			cancel()
			t.Fatalf("projector did not execute cycle %d", cycle+1)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("projector did not stop after cancellation")
	}
}
