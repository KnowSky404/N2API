package admin

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

type fakeRequestLogRetentionService struct {
	mu          sync.Mutex
	settings    GatewaySettings
	settingsErr error
	result      RequestLogCleanupResult
	runErr      error
	runCalls    int
	callCh      chan struct{}
}

func (s *fakeRequestLogRetentionService) GetGatewaySettings(context.Context) (GatewaySettings, error) {
	return s.settings, s.settingsErr
}

func (s *fakeRequestLogRetentionService) RunRequestLogRetention(_ context.Context, _ int, _ time.Time, _ int) (RequestLogCleanupResult, error) {
	s.mu.Lock()
	s.runCalls++
	s.mu.Unlock()
	if s.callCh != nil {
		select {
		case s.callCh <- struct{}{}:
		default:
		}
	}
	return s.result, s.runErr
}

func (s *fakeRequestLogRetentionService) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runCalls
}

type captureRequestLogRetentionEvents struct {
	events []systemevent.Event
}

type captureAdminTaskMetrics struct {
	runs [][2]string
}

func (m *captureAdminTaskMetrics) BeginBackgroundTask(task string) func(string) {
	return func(outcome string) { m.runs = append(m.runs, [2]string{task, outcome}) }
}
func (m *captureAdminTaskMetrics) ObserveBackgroundTaskRun(task, outcome string, _ time.Duration) {
	m.runs = append(m.runs, [2]string{task, outcome})
}

func (r *captureRequestLogRetentionEvents) Insert(_ context.Context, event systemevent.Event) error {
	r.events = append(r.events, event)
	return nil
}

func TestRequestLogRetentionRunnerDefaultsAndDisabledGate(t *testing.T) {
	service := &fakeRequestLogRetentionService{settings: GatewaySettings{RequestLogRetentionDays: 30}}
	runner := NewRequestLogRetentionRunner(service, RequestLogRetentionRunnerConfig{}, slog.Default())
	if runner.cfg.Interval != 24*time.Hour || runner.cfg.BatchSize != 1000 {
		t.Fatalf("runner defaults = interval %s batch %d", runner.cfg.Interval, runner.cfg.BatchSize)
	}
	runner.Run(context.Background())
	if service.calls() != 0 || runner.RequestLogRetentionStatus().AutomaticEnabled {
		t.Fatalf("disabled runner calls = %d status = %+v", service.calls(), runner.RequestLogRetentionStatus())
	}
}

func TestRequestLogRetentionRunnerSkipsDisabledSavedPolicy(t *testing.T) {
	service := &fakeRequestLogRetentionService{}
	recorder := &captureRequestLogRetentionEvents{}
	runner := NewRequestLogRetentionRunner(service, RequestLogRetentionRunnerConfig{Enabled: true}, slog.Default())
	runner.SetSystemEventRecorder(recorder)
	runner.runCycle(context.Background())
	if service.calls() != 0 || len(recorder.events) != 0 {
		t.Fatalf("disabled policy calls = %d events = %d", service.calls(), len(recorder.events))
	}
	if status := runner.RequestLogRetentionStatus(); status.Running || status.LastStartedAt != nil {
		t.Fatalf("disabled policy status = %+v", status)
	}
}

func TestRequestLogRetentionRunnerRecordsSuccessStatusAndEvent(t *testing.T) {
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-30 * 24 * time.Hour)
	service := &fakeRequestLogRetentionService{
		settings: GatewaySettings{RequestLogRetentionDays: 30},
		result:   RequestLogCleanupResult{RetentionDays: 30, Deleted: 2500, Batches: 3, Before: cutoff},
	}
	recorder := &captureRequestLogRetentionEvents{}
	runner := NewRequestLogRetentionRunner(service, RequestLogRetentionRunnerConfig{Enabled: true, BatchSize: 1000}, slog.Default())
	runner.now = func() time.Time { return now }
	runner.SetSystemEventRecorder(recorder)
	runner.runCycle(context.Background())

	status := runner.RequestLogRetentionStatus()
	if status.Running || status.LastStartedAt == nil || !status.LastStartedAt.Equal(now) ||
		status.LastSucceededAt == nil || !status.LastSucceededAt.Equal(now) || status.LastErrorCode != "" ||
		status.LastDeletedCount != 2500 || status.LastBatchCount != 3 || status.LastCutoff == nil || !status.LastCutoff.Equal(cutoff) {
		t.Fatalf("success status = %+v", status)
	}
	if len(recorder.events) != 1 || recorder.events[0].Action != systemevent.ActionSchedulerRequestLogRetentionSucceeded || recorder.events[0].Outcome != systemevent.OutcomeSuccess {
		t.Fatalf("success events = %+v", recorder.events)
	}
}

func TestRequestLogRetentionRunnerReportsSuccessMetricsOutcome(t *testing.T) {
	metrics := &captureAdminTaskMetrics{}
	runner := NewRequestLogRetentionRunner(&fakeRequestLogRetentionService{
		settings: GatewaySettings{RequestLogRetentionDays: 30},
	}, RequestLogRetentionRunnerConfig{Enabled: true}, slog.Default())
	runner.SetMetricsObserver(metrics)
	runner.runCycle(context.Background())
	if len(metrics.runs) != 1 || metrics.runs[0] != [2]string{"request_log_retention", "success"} {
		t.Fatalf("task metrics = %+v", metrics.runs)
	}
}

func TestRequestLogRetentionRunnerSettingsFailurePreservesLastCutoff(t *testing.T) {
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-30 * 24 * time.Hour)
	service := &fakeRequestLogRetentionService{
		settings: GatewaySettings{RequestLogRetentionDays: 30},
		result:   RequestLogCleanupResult{RetentionDays: 30, Before: cutoff},
	}
	runner := NewRequestLogRetentionRunner(service, RequestLogRetentionRunnerConfig{Enabled: true}, slog.Default())
	runner.now = func() time.Time { return now }
	runner.runCycle(context.Background())
	service.settingsErr = errors.New("settings unavailable")
	runner.runCycle(context.Background())

	status := runner.RequestLogRetentionStatus()
	if status.LastCutoff == nil || !status.LastCutoff.Equal(cutoff) || status.LastErrorCode != "request_log_retention_settings_failed" {
		t.Fatalf("status after settings failure = %+v", status)
	}
}

func TestRequestLogRetentionRunnerRecordsSafePartialFailure(t *testing.T) {
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	service := &fakeRequestLogRetentionService{
		settings: GatewaySettings{RequestLogRetentionDays: 7},
		result:   RequestLogCleanupResult{RetentionDays: 7, Deleted: 1000, Batches: 1, Before: now.Add(-7 * 24 * time.Hour)},
		runErr:   errors.New("SECRET_SQL_ERROR_CANARY"),
	}
	recorder := &captureRequestLogRetentionEvents{}
	runner := NewRequestLogRetentionRunner(service, RequestLogRetentionRunnerConfig{Enabled: true}, slog.Default())
	runner.now = func() time.Time { return now }
	runner.SetSystemEventRecorder(recorder)
	runner.runCycle(context.Background())

	status := runner.RequestLogRetentionStatus()
	if status.Running || status.LastErrorAt == nil || status.LastErrorCode != "request_log_retention_failed" || status.LastDeletedCount != 1000 {
		t.Fatalf("failure status = %+v", status)
	}
	if len(recorder.events) != 1 || recorder.events[0].Action != systemevent.ActionSchedulerRequestLogRetentionFailed ||
		recorder.events[0].Outcome != systemevent.OutcomePartial || recorder.events[0].ErrorCode != "request_log_retention_failed" {
		t.Fatalf("failure event = %+v", recorder.events)
	}
	encoded, err := json.Marshal(struct {
		Status RequestLogRetentionStatus
		Event  systemevent.Event
	}{status, recorder.events[0]})
	if err != nil {
		t.Fatalf("marshal status and event: %v", err)
	}
	if string(encoded) == "" || containsString(string(encoded), "SECRET_SQL_ERROR_CANARY") {
		t.Fatalf("safe status/event leaked raw error: %s", encoded)
	}
}

func TestRequestLogRetentionRunnerTreatsLockContentionAsSkip(t *testing.T) {
	service := &fakeRequestLogRetentionService{
		settings: GatewaySettings{RequestLogRetentionDays: 7},
		runErr:   ErrConflict,
	}
	recorder := &captureRequestLogRetentionEvents{}
	runner := NewRequestLogRetentionRunner(service, RequestLogRetentionRunnerConfig{Enabled: true}, slog.Default())
	runner.SetSystemEventRecorder(recorder)
	runner.runCycle(context.Background())
	status := runner.RequestLogRetentionStatus()
	if status.Running || status.LastErrorAt != nil || status.LastSucceededAt != nil || len(recorder.events) != 0 {
		t.Fatalf("contention status = %+v events = %d", status, len(recorder.events))
	}
}

func TestRequestLogRetentionRunnerDoesNotRecordShutdownCancellationAsFailure(t *testing.T) {
	service := &fakeRequestLogRetentionService{
		settings: GatewaySettings{RequestLogRetentionDays: 7},
		runErr:   context.Canceled,
	}
	recorder := &captureRequestLogRetentionEvents{}
	runner := NewRequestLogRetentionRunner(service, RequestLogRetentionRunnerConfig{Enabled: true}, slog.Default())
	runner.SetSystemEventRecorder(recorder)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	runner.runCycle(ctx)

	status := runner.RequestLogRetentionStatus()
	if status.Running || status.LastErrorAt == nil || status.LastErrorCode != "request_log_retention_canceled" {
		t.Fatalf("canceled status = %+v", status)
	}
	if len(recorder.events) != 0 {
		t.Fatalf("shutdown cancellation events = %+v, want none", recorder.events)
	}
}

func TestRequestLogRetentionRunnerRunsImmediately(t *testing.T) {
	service := &fakeRequestLogRetentionService{
		settings: GatewaySettings{RequestLogRetentionDays: 7},
		result:   RequestLogCleanupResult{RetentionDays: 7, Before: time.Now().Add(-7 * 24 * time.Hour)},
		callCh:   make(chan struct{}, 1),
	}
	runner := NewRequestLogRetentionRunner(service, RequestLogRetentionRunnerConfig{Enabled: true, Interval: time.Hour}, slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runner.Run(ctx)
	}()
	select {
	case <-service.callCh:
		cancel()
	case <-time.After(time.Second):
		cancel()
		t.Fatal("runner did not execute its startup cycle")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runner did not stop after cancellation")
	}
}

func containsString(value, substring string) bool {
	for i := 0; i+len(substring) <= len(value); i++ {
		if value[i:i+len(substring)] == substring {
			return true
		}
	}
	return false
}
