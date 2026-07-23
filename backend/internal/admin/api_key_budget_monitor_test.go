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

type fakeAPIKeyBudgetMonitorStore struct {
	mu      sync.Mutex
	results []APIKeyBudgetMonitorCycleResult
	err     error
	calls   []int64
	callCh  chan struct{}
}

func (s *fakeAPIKeyBudgetMonitorStore) RunAPIKeyBudgetMonitorCycle(_ context.Context, afterID int64, _ int, _ time.Time) (APIKeyBudgetMonitorCycleResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, afterID)
	if s.callCh != nil {
		select {
		case s.callCh <- struct{}{}:
		default:
		}
	}
	if s.err != nil {
		return APIKeyBudgetMonitorCycleResult{}, s.err
	}
	if len(s.results) == 0 {
		return APIKeyBudgetMonitorCycleResult{}, nil
	}
	result := s.results[0]
	s.results = s.results[1:]
	return result, nil
}

func (s *fakeAPIKeyBudgetMonitorStore) afterIDs() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int64(nil), s.calls...)
}

func TestAPIKeyBudgetMonitorDefaultsAndCursorLifecycle(t *testing.T) {
	store := &fakeAPIKeyBudgetMonitorStore{results: []APIKeyBudgetMonitorCycleResult{
		{Processed: 100, Transitions: 3, NextAfterID: 120},
		{Processed: 4, Transitions: 1, NextAfterID: 0},
		{Processed: 2, NextAfterID: 0},
	}}
	monitor := NewAPIKeyBudgetMonitor(store, APIKeyBudgetMonitorConfig{}, slog.Default())
	if monitor.cfg.Interval != 5*time.Minute || monitor.cfg.BatchSize != 100 {
		t.Fatalf("monitor defaults = interval %s batch %d", monitor.cfg.Interval, monitor.cfg.BatchSize)
	}
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	monitor.now = func() time.Time { return now }
	monitor.runCycle(context.Background())
	monitor.runCycle(context.Background())
	monitor.runCycle(context.Background())

	if got := store.afterIDs(); len(got) != 3 || got[0] != 0 || got[1] != 120 || got[2] != 0 {
		t.Fatalf("cycle cursors = %v, want [0 120 0]", got)
	}
	status := monitor.Status()
	if status.Running || status.LastSucceededAt == nil || !status.LastSucceededAt.Equal(now) ||
		status.LastErrorCode != "" || status.LastProcessed != 2 || status.LastTransitions != 0 || status.CursorAfterID != 0 {
		t.Fatalf("monitor status = %+v", status)
	}
}

func TestAPIKeyBudgetMonitorReportsTaskMetrics(t *testing.T) {
	metrics := &captureAdminTaskMetrics{}
	monitor := NewAPIKeyBudgetMonitor(&fakeAPIKeyBudgetMonitorStore{}, APIKeyBudgetMonitorConfig{}, slog.Default())
	monitor.SetMetricsObserver(metrics)
	monitor.runCycle(context.Background())
	if len(metrics.runs) != 1 || metrics.runs[0] != [2]string{"api_key_budget_monitor", "success"} {
		t.Fatalf("task metrics = %+v", metrics.runs)
	}
}

func TestAPIKeyBudgetMonitorFailurePreservesCursorAndSanitizesLog(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	store := &fakeAPIKeyBudgetMonitorStore{results: []APIKeyBudgetMonitorCycleResult{{Processed: 100, NextAfterID: 55}}}
	monitor := NewAPIKeyBudgetMonitor(store, APIKeyBudgetMonitorConfig{}, logger)
	monitor.runCycle(context.Background())
	store.err = errors.New("SECRET_DATABASE_ERROR_CANARY")
	monitor.runCycle(context.Background())

	status := monitor.Status()
	if status.Running || status.CursorAfterID != 55 || status.LastErrorAt == nil || status.LastErrorCode != "api_key_budget_monitor_failed" {
		t.Fatalf("failure status = %+v", status)
	}
	if strings.Contains(output.String(), "SECRET_DATABASE_ERROR_CANARY") || !strings.Contains(output.String(), "api_key_budget_monitor_failed") {
		t.Fatalf("monitor log was not sanitized: %s", output.String())
	}
}

func TestAPIKeyBudgetMonitorShutdownCancellationIsQuiet(t *testing.T) {
	var output bytes.Buffer
	store := &fakeAPIKeyBudgetMonitorStore{err: context.Canceled}
	monitor := NewAPIKeyBudgetMonitor(store, APIKeyBudgetMonitorConfig{}, slog.New(slog.NewJSONHandler(&output, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	monitor.runCycle(ctx)

	if output.Len() != 0 {
		t.Fatalf("shutdown cancellation logged: %s", output.String())
	}
	if status := monitor.Status(); status.Running || status.LastErrorCode != "api_key_budget_monitor_canceled" {
		t.Fatalf("canceled status = %+v", status)
	}
}

func TestAPIKeyBudgetMonitorRunsImmediately(t *testing.T) {
	store := &fakeAPIKeyBudgetMonitorStore{callCh: make(chan struct{}, 1)}
	monitor := NewAPIKeyBudgetMonitor(store, APIKeyBudgetMonitorConfig{Interval: time.Hour}, slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		monitor.Run(ctx)
	}()
	select {
	case <-store.callCh:
		cancel()
	case <-time.After(time.Second):
		cancel()
		t.Fatal("monitor did not execute its startup cycle")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("monitor did not stop after cancellation")
	}
}

func TestAPIKeyBudgetMonitorRepeatsAtConfiguredInterval(t *testing.T) {
	store := &fakeAPIKeyBudgetMonitorStore{callCh: make(chan struct{}, 2)}
	monitor := NewAPIKeyBudgetMonitor(store, APIKeyBudgetMonitorConfig{Interval: 5 * time.Millisecond}, slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		monitor.Run(ctx)
	}()
	for cycle := 0; cycle < 2; cycle++ {
		select {
		case <-store.callCh:
		case <-time.After(time.Second):
			cancel()
			t.Fatalf("monitor did not execute cycle %d", cycle+1)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("monitor did not stop after interval test")
	}
}
