package gateway

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

type fakeResponseAffinityRetentionStore struct {
	lease    *fakeResponseAffinityRetentionLease
	acquired bool
	err      error
	calls    int
}

func (s *fakeResponseAffinityRetentionStore) TryAcquireResponseAffinityRetention(context.Context) (ResponseAffinityRetentionLease, bool, error) {
	s.calls++
	return s.lease, s.acquired, s.err
}

type fakeResponseAffinityRetentionLease struct {
	deleted   int64
	deleteErr error
	closeErr  error
	cutoff    time.Time
	batchSize int
	closed    bool
}

func (l *fakeResponseAffinityRetentionLease) DeleteExpiredBatch(_ context.Context, cutoff time.Time, batchSize int) (int64, error) {
	l.cutoff = cutoff
	l.batchSize = batchSize
	return l.deleted, l.deleteErr
}

func (l *fakeResponseAffinityRetentionLease) Close() error {
	l.closed = true
	return l.closeErr
}

type captureResponseAffinityRetentionEvents struct {
	events []systemevent.Event
}

type captureResponseAffinityTaskMetrics struct {
	runs [][2]string
}

func (m *captureResponseAffinityTaskMetrics) BeginBackgroundTask(task string) func(string) {
	return func(outcome string) { m.runs = append(m.runs, [2]string{task, outcome}) }
}

func (m *captureResponseAffinityTaskMetrics) ObserveBackgroundTaskRun(task, outcome string, _ time.Duration) {
	m.runs = append(m.runs, [2]string{task, outcome})
}

func (r *captureResponseAffinityRetentionEvents) Insert(_ context.Context, event systemevent.Event) error {
	r.events = append(r.events, event)
	return nil
}

func TestResponseAffinityRetentionRunnerDefaultsAndDisabledGate(t *testing.T) {
	store := &fakeResponseAffinityRetentionStore{}
	runner := NewResponseAffinityRetentionRunner(store, ResponseAffinityRetentionRunnerConfig{}, nil)
	if runner.cfg.Interval != 24*time.Hour || runner.cfg.BatchSize != 1000 {
		t.Fatalf("defaults = interval:%s batch:%d", runner.cfg.Interval, runner.cfg.BatchSize)
	}
	runner.Run(context.Background())
	if store.calls != 0 || runner.ResponseAffinityRetentionStatus().AutomaticEnabled {
		t.Fatalf("disabled runner = calls:%d status:%+v", store.calls, runner.ResponseAffinityRetentionStatus())
	}
}

func TestResponseAffinityRetentionRunnerRecordsSuccess(t *testing.T) {
	started := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	finished := started.Add(time.Second)
	lease := &fakeResponseAffinityRetentionLease{deleted: 37}
	store := &fakeResponseAffinityRetentionStore{lease: lease, acquired: true}
	recorder := &captureResponseAffinityRetentionEvents{}
	runner := NewResponseAffinityRetentionRunner(store, ResponseAffinityRetentionRunnerConfig{Enabled: true, BatchSize: 500}, nil)
	times := []time.Time{started, finished, finished}
	runner.now = func() time.Time {
		value := times[0]
		times = times[1:]
		return value
	}
	runner.SetSystemEventRecorder(recorder)
	metrics := &captureResponseAffinityTaskMetrics{}
	runner.SetMetricsObserver(metrics)

	runner.runCycle(context.Background())

	status := runner.ResponseAffinityRetentionStatus()
	if status.Running || status.LastStartedAt == nil || !status.LastStartedAt.Equal(started) || status.LastSucceededAt == nil || !status.LastSucceededAt.Equal(finished) || status.LastDeletedCount != 37 || status.LastErrorCode != "" {
		t.Fatalf("success status = %+v", status)
	}
	if !lease.closed || !lease.cutoff.Equal(started) || lease.batchSize != 500 {
		t.Fatalf("lease = closed:%v cutoff:%s batch:%d", lease.closed, lease.cutoff, lease.batchSize)
	}
	if len(recorder.events) != 1 || recorder.events[0].Action != systemevent.ActionSchedulerResponseAffinityRetentionSucceeded || recorder.events[0].Outcome != systemevent.OutcomeSuccess {
		t.Fatalf("success events = %+v", recorder.events)
	}
	if len(metrics.runs) != 1 || metrics.runs[0] != [2]string{"response_affinity_retention", "success"} {
		t.Fatalf("task metrics = %+v", metrics.runs)
	}
}

func TestResponseAffinityRetentionRunnerRecordsContentionWithoutFailure(t *testing.T) {
	started := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	store := &fakeResponseAffinityRetentionStore{}
	runner := NewResponseAffinityRetentionRunner(store, ResponseAffinityRetentionRunnerConfig{Enabled: true}, nil)
	runner.now = func() time.Time { return started }
	runner.runCycle(context.Background())
	status := runner.ResponseAffinityRetentionStatus()
	if status.Running || status.LastLockSkippedAt == nil || !status.LastLockSkippedAt.Equal(started) || status.LastFailedAt != nil {
		t.Fatalf("contended status = %+v", status)
	}
}

func TestResponseAffinityRetentionRunnerLogsStableFailureOnly(t *testing.T) {
	store := &fakeResponseAffinityRetentionStore{err: errors.New("postgres://user:password@db/private")}
	var logs bytes.Buffer
	runner := NewResponseAffinityRetentionRunner(store, ResponseAffinityRetentionRunnerConfig{Enabled: true}, slog.New(slog.NewTextHandler(&logs, nil)))
	now := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	runner.now = func() time.Time { return now }
	runner.runCycle(context.Background())
	status := runner.ResponseAffinityRetentionStatus()
	if status.LastFailedAt == nil || status.LastErrorCode != "response_affinity_retention_acquire_failed" {
		t.Fatalf("failure status = %+v", status)
	}
	if !strings.Contains(logs.String(), "response_affinity_retention_acquire_failed") || strings.Contains(logs.String(), "password") || strings.Contains(logs.String(), "postgres://") {
		t.Fatalf("unsafe failure log = %q", logs.String())
	}
}

func TestResponseAffinityRetentionRunnerHonorsCancellation(t *testing.T) {
	lease := &fakeResponseAffinityRetentionLease{deleteErr: context.Canceled}
	store := &fakeResponseAffinityRetentionStore{lease: lease, acquired: true}
	runner := NewResponseAffinityRetentionRunner(store, ResponseAffinityRetentionRunnerConfig{Enabled: true}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner.runCycle(ctx)
	status := runner.ResponseAffinityRetentionStatus()
	if status.Running || status.LastFailedAt == nil || status.LastErrorCode != "response_affinity_retention_canceled" || !lease.closed {
		t.Fatalf("canceled status = %+v lease_closed=%v", status, lease.closed)
	}
}
