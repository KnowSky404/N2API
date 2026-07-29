package main

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/lifecycle"
)

var errRequestDrainTimedOut = errors.New("request drain timed out")

type runtimeDrainMetrics interface {
	SetDraining(bool)
	SetReadiness(component string, ready bool)
}

type runtimeAlertDispatcher interface {
	Shutdown(context.Context) error
}

type runtimeRequestTracker struct {
	mu        sync.Mutex
	accepting bool
	active    int
	drained   chan struct{}
}

func newRuntimeRequestTracker() *runtimeRequestTracker {
	return &runtimeRequestTracker{accepting: true, drained: make(chan struct{})}
}

func (tracker *runtimeRequestTracker) Wrap(next http.Handler) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		tracker.mu.Lock()
		if !tracker.accepting {
			tracker.mu.Unlock()
			http.Error(response, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		tracker.active++
		tracker.mu.Unlock()
		defer tracker.finish()
		next.ServeHTTP(response, request)
	})
}

func (tracker *runtimeRequestTracker) finish() {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	tracker.active--
	if !tracker.accepting && tracker.active == 0 {
		close(tracker.drained)
	}
}

func (tracker *runtimeRequestTracker) StopAccepting() {
	if tracker == nil {
		return
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if !tracker.accepting {
		return
	}
	tracker.accepting = false
	if tracker.active == 0 {
		close(tracker.drained)
	}
}

func (tracker *runtimeRequestTracker) Wait(ctx context.Context) error {
	if tracker == nil {
		return nil
	}
	tracker.StopAccepting()
	select {
	case <-tracker.drained:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type runtimeShutdown struct {
	mainServer        *http.Server
	metricsServer     *http.Server
	readiness         *lifecycle.Readiness
	metrics           runtimeDrainMetrics
	background        *lifecycle.Supervisor
	servers           *lifecycle.Supervisor
	alerts            runtimeAlertDispatcher
	requests          *runtimeRequestTracker
	cancelRequests    context.CancelFunc
	cancelMetricsRoot context.CancelFunc
	finalizeResources func(context.Context) error
	totalTimeout      time.Duration
	requestDrain      time.Duration
}

func (runtime *runtimeShutdown) run(reason string) error {
	if runtime == nil {
		return nil
	}
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), runtime.totalTimeout)
	defer cancelShutdown()

	if runtime.readiness != nil {
		runtime.readiness.BeginDrain(reason)
	}
	if runtime.metrics != nil {
		runtime.metrics.SetDraining(true)
		runtime.metrics.SetReadiness("runtime", false)
		runtime.metrics.SetReadiness("overall", false)
	}
	if runtime.servers != nil {
		runtime.servers.BeginStop()
	}

	var shutdownErrors []error
	if runtime.mainServer != nil {
		drainCtx, cancelDrain := context.WithTimeout(shutdownCtx, runtime.requestDrain)
		err := runtime.mainServer.Shutdown(drainCtx)
		cancelDrain()
		if runtime.requests != nil {
			runtime.requests.StopAccepting()
		}
		if err != nil {
			shutdownErrors = append(shutdownErrors, errRequestDrainTimedOut)
			if runtime.cancelRequests != nil {
				runtime.cancelRequests()
			}
			if closeErr := runtime.mainServer.Close(); closeErr != nil && !errors.Is(closeErr, http.ErrServerClosed) {
				shutdownErrors = append(shutdownErrors, errors.New("main server forced close failed"))
			}
		}
	}
	if runtime.cancelRequests != nil {
		runtime.cancelRequests()
	}
	if runtime.requests != nil {
		if err := runtime.requests.Wait(shutdownCtx); err != nil {
			shutdownErrors = append(shutdownErrors, errors.New("request handler shutdown failed"))
		}
	}

	if runtime.background != nil {
		runtime.background.Stop()
		if err := runtime.background.Wait(shutdownCtx); err != nil {
			shutdownErrors = append(shutdownErrors, errors.New("background shutdown failed"))
		}
	}
	if runtime.alerts != nil {
		if err := runtime.alerts.Shutdown(shutdownCtx); err != nil {
			shutdownErrors = append(shutdownErrors, errors.New("alert delivery shutdown failed"))
		}
	}
	if runtime.metricsServer != nil {
		if err := runtime.metricsServer.Shutdown(shutdownCtx); err != nil {
			shutdownErrors = append(shutdownErrors, errors.New("metrics server shutdown failed"))
			_ = runtime.metricsServer.Close()
		}
	}
	if runtime.cancelMetricsRoot != nil {
		runtime.cancelMetricsRoot()
	}
	if runtime.servers != nil {
		runtime.servers.Stop()
		if err := runtime.servers.Wait(shutdownCtx); err != nil {
			shutdownErrors = append(shutdownErrors, errors.New("listener shutdown failed"))
		}
	}
	if runtime.finalizeResources != nil {
		finalizeDone := make(chan error, 1)
		go func() { finalizeDone <- runtime.finalizeResources(shutdownCtx) }()
		select {
		case err := <-finalizeDone:
			if err != nil {
				shutdownErrors = append(shutdownErrors, errors.New("resource shutdown failed"))
			}
		case <-shutdownCtx.Done():
			shutdownErrors = append(shutdownErrors, errors.New("resource shutdown deadline exceeded"))
		}
	}

	if len(shutdownErrors) > 0 && runtime.readiness != nil {
		runtime.readiness.MarkFailed("shutdown_failed")
	}
	return errors.Join(shutdownErrors...)
}
