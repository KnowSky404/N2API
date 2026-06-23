package provider

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

type fakeAutoTestService struct {
	calls   atomic.Int64
	started chan struct{}
	release chan struct{}
	err     error
}

func newFakeAutoTestService() *fakeAutoTestService {
	return &fakeAutoTestService{
		started: make(chan struct{}, 10),
		release: make(chan struct{}),
	}
}

func (s *fakeAutoTestService) TestAccounts(ctx context.Context) ([]Account, error) {
	call := s.calls.Add(1)
	s.started <- struct{}{}
	select {
	case <-s.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if s.err != nil {
		return nil, s.err
	}
	return []Account{{ID: call, Provider: "openai"}}, nil
}

func TestAutoTestRunnerDisabledDoesNotProbe(t *testing.T) {
	service := newFakeAutoTestService()
	runner := NewAutoTestRunner(service, AutoTestRunnerConfig{
		Enabled:  false,
		Interval: time.Minute,
	}, slog.Default())

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	runner.Run(ctx)

	if service.calls.Load() != 0 {
		t.Fatalf("calls = %d, want 0", service.calls.Load())
	}
}

func TestAutoTestRunnerConfigSourceCanEnableAfterDisabledCycle(t *testing.T) {
	service := newFakeAutoTestService()
	var reads atomic.Int64
	runner := NewAutoTestRunnerWithConfigSource(service, func(context.Context) (AutoTestRunnerConfig, error) {
		if reads.Add(1) == 1 {
			return AutoTestRunnerConfig{
				Enabled:  false,
				Interval: 10 * time.Millisecond,
			}, nil
		}
		return AutoTestRunnerConfig{
			Enabled:  true,
			Interval: 10 * time.Millisecond,
		}, nil
	}, slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})

	go func() {
		runner.Run(ctx)
		close(done)
	}()

	select {
	case <-service.started:
	case <-time.After(time.Second):
		t.Fatal("runner did not observe enabled config and start probe")
	}
	if reads.Load() < 2 {
		t.Fatalf("config source reads = %d, want at least 2", reads.Load())
	}
	service.release <- struct{}{}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runner did not stop after cancellation")
	}
}

func TestAutoTestRunnerRunsImmediateCycle(t *testing.T) {
	service := newFakeAutoTestService()
	runner := NewAutoTestRunner(service, AutoTestRunnerConfig{
		Enabled:  true,
		Interval: time.Hour,
	}, slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})

	go func() {
		runner.Run(ctx)
		close(done)
	}()

	select {
	case <-service.started:
	case <-time.After(time.Second):
		t.Fatal("runner did not start immediate probe")
	}
	service.release <- struct{}{}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runner did not stop after cancellation")
	}
	if service.calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", service.calls.Load())
	}
}

func TestAutoTestRunnerSkipsOverlappingTicks(t *testing.T) {
	service := newFakeAutoTestService()
	runner := NewAutoTestRunner(service, AutoTestRunnerConfig{
		Enabled:  true,
		Interval: 10 * time.Millisecond,
	}, slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})

	go func() {
		runner.Run(ctx)
		close(done)
	}()

	select {
	case <-service.started:
	case <-time.After(time.Second):
		t.Fatal("runner did not start immediate probe")
	}
	time.Sleep(50 * time.Millisecond)
	if service.calls.Load() != 1 {
		t.Fatalf("calls while first probe blocked = %d, want 1", service.calls.Load())
	}
	service.release <- struct{}{}
	select {
	case <-service.started:
	case <-time.After(time.Second):
		t.Fatal("runner did not run a later probe after first released")
	}
	service.release <- struct{}{}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runner did not stop after cancellation")
	}
}
