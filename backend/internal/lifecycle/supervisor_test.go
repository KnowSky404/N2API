package lifecycle

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSupervisorStopsAndWaitsForEveryComponent(t *testing.T) {
	supervisor := NewSupervisor(context.Background())
	stopped := make(chan string, 2)
	for _, name := range []string{"first", "second"} {
		name := name
		if err := supervisor.Start(name, func(ctx context.Context) error {
			<-ctx.Done()
			stopped <- name
			return nil
		}); err != nil {
			t.Fatalf("Start(%s): %v", name, err)
		}
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := supervisor.Wait(waitCtx); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	seen := map[string]bool{<-stopped: true, <-stopped: true}
	if !seen["first"] || !seen["second"] {
		t.Fatalf("stopped components = %+v", seen)
	}
	if err := supervisor.Start("late", func(context.Context) error { return nil }); !errors.Is(err, ErrSupervisorStopping) {
		t.Fatalf("late Start = %v, want ErrSupervisorStopping", err)
	}
}

func TestSupervisorReportsUnexpectedReturnAndPanic(t *testing.T) {
	for name, run := range map[string]func(context.Context) error{
		"return": func(context.Context) error { return nil },
		"error":  func(context.Context) error { return errors.New("failed") },
		"panic":  func(context.Context) error { panic("boom") },
	} {
		t.Run(name, func(t *testing.T) {
			supervisor := NewSupervisor(context.Background())
			if err := supervisor.Start(name, run); err != nil {
				t.Fatalf("Start: %v", err)
			}
			select {
			case err := <-supervisor.Failures():
				if !strings.Contains(err.Error(), name) {
					t.Fatalf("failure = %v, want component name", err)
				}
				if name == "return" && !errors.Is(err, ErrComponentStopped) {
					t.Fatalf("failure = %v, want ErrComponentStopped", err)
				}
				if name == "error" && !strings.Contains(err.Error(), "failed") {
					t.Fatalf("failure = %v, want component error", err)
				}
				if latest := supervisor.LastFailure(); latest == nil || latest.Error() != err.Error() {
					t.Fatalf("LastFailure = %v, want %v", latest, err)
				}
			case <-time.After(time.Second):
				t.Fatal("component failure was not reported")
			}
			supervisor.Stop()
		})
	}
}

func TestSupervisorKeepsFailureAfterNotificationIsConsumed(t *testing.T) {
	supervisor := NewSupervisor(context.Background())
	if err := supervisor.Start("critical", func(context.Context) error {
		return errors.New("critical failure")
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-supervisor.Failures()
	if err := supervisor.LastFailure(); err == nil || !strings.Contains(err.Error(), "critical failure") {
		t.Fatalf("LastFailure = %v, want critical failure", err)
	}
	supervisor.Stop()
}

func TestSupervisorPreservesCriticalFailureAfterStopBegins(t *testing.T) {
	supervisor := NewSupervisor(context.Background())
	release := make(chan struct{})
	if err := supervisor.Start("critical", func(context.Context) error {
		<-release
		return errors.New("critical failure")
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	supervisor.Stop()
	close(release)
	waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := supervisor.Wait(waitCtx)
	if err == nil || !strings.Contains(err.Error(), "critical failure") {
		t.Fatalf("Wait = %v, want critical failure", err)
	}
	select {
	case failure := <-supervisor.Failures():
		if !strings.Contains(failure.Error(), "critical failure") {
			t.Fatalf("failure = %v, want critical failure", failure)
		}
	case <-time.After(time.Second):
		t.Fatal("critical failure was suppressed after Stop")
	}
}

func TestSupervisorWaitUsesCallerDeadline(t *testing.T) {
	supervisor := NewSupervisor(context.Background())
	release := make(chan struct{})
	if err := supervisor.Start("blocked", func(context.Context) error { <-release; return nil }); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := supervisor.Wait(waitCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait = %v, want deadline exceeded", err)
	}
	close(release)
	finishCtx, finishCancel := context.WithTimeout(context.Background(), time.Second)
	defer finishCancel()
	if err := supervisor.Wait(finishCtx); err != nil {
		t.Fatalf("second Wait: %v", err)
	}
}

func TestSupervisorBeginStopPreventsStartsWithoutSuppressingUnexpectedExit(t *testing.T) {
	supervisor := NewSupervisor(context.Background())
	started := make(chan struct{})
	release := make(chan struct{})
	if err := supervisor.Start("listener", func(context.Context) error {
		close(started)
		<-release
		return nil
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-started
	supervisor.BeginStop()
	close(release)
	select {
	case failure := <-supervisor.Failures():
		if !errors.Is(failure, ErrComponentStopped) {
			t.Fatalf("failure = %v, want ErrComponentStopped", failure)
		}
	case <-time.After(time.Second):
		t.Fatal("BeginStop suppressed an unexpected component exit")
	}
	if err := supervisor.Start("late", func(context.Context) error { return nil }); !errors.Is(err, ErrSupervisorStopping) {
		t.Fatalf("late Start = %v, want ErrSupervisorStopping", err)
	}
	supervisor.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := supervisor.Wait(ctx); !errors.Is(err, ErrComponentStopped) {
		t.Fatalf("Wait = %v, want ErrComponentStopped", err)
	}
}
