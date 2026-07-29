package main

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/config"
	"github.com/KnowSky404/N2API/backend/internal/lifecycle"
	"github.com/KnowSky404/N2API/backend/internal/metrics"
)

type runningRuntime struct {
	runtime    *runtimeShutdown
	mainURL    string
	metricsURL string
}

type observingAlertShutdown struct {
	backgroundDone <-chan struct{}
	metricsURL     string
	called         chan struct{}
}

func (shutdown *observingAlertShutdown) Shutdown(ctx context.Context) error {
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("shutdown context has no global deadline")
	}
	select {
	case <-shutdown.backgroundDone:
	default:
		return errors.New("alert shutdown started before background completed")
	}
	response, err := http.Get(shutdown.metricsURL)
	if err != nil {
		return errors.New("metrics stopped before alert shutdown")
	}
	body, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil || !strings.Contains(string(body), "n2api_draining 1") {
		return errors.New("draining metrics unavailable during alert shutdown")
	}
	close(shutdown.called)
	return nil
}

func startRuntimeForTest(t *testing.T, handler http.Handler, registry *metrics.Registry, totalTimeout, requestDrain time.Duration) runningRuntime {
	t.Helper()
	requestCtx, cancelRequests := context.WithCancel(context.Background())
	requestTracker := newRuntimeRequestTracker()
	mainServer := newHTTPServer(config.Config{}, requestTracker.Wrap(handler), requestCtx)
	mainListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen main server: %v", err)
	}
	servers := lifecycle.NewSupervisor(context.Background())
	if err := servers.Start("test_http", func(context.Context) error {
		err := mainServer.Serve(mainListener)
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}); err != nil {
		t.Fatalf("start main server: %v", err)
	}

	var metricsServer *http.Server
	var metricsURL string
	metricsRoot, cancelMetricsRoot := context.WithCancel(context.Background())
	if registry != nil {
		metricsListener, listenErr := net.Listen("tcp", "127.0.0.1:0")
		if listenErr != nil {
			t.Fatalf("listen metrics server: %v", listenErr)
		}
		metricsServer = metrics.NewHTTPServer(metricsListener.Addr().String(), "", registry.Handler(), metricsRoot)
		if err := servers.Start("test_metrics", func(context.Context) error {
			err := metricsServer.Serve(metricsListener)
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		}); err != nil {
			t.Fatalf("start metrics server: %v", err)
		}
		metricsURL = "http://" + metricsListener.Addr().String() + "/metrics"
	}

	readiness := lifecycle.NewReadiness()
	readiness.MarkReady()
	runtime := &runtimeShutdown{
		mainServer: mainServer, metricsServer: metricsServer, readiness: readiness,
		metrics: registry, servers: servers, requests: requestTracker, cancelRequests: cancelRequests,
		cancelMetricsRoot: cancelMetricsRoot, totalTimeout: totalTimeout, requestDrain: requestDrain,
	}
	t.Cleanup(func() {
		cancelRequests()
		cancelMetricsRoot()
		_ = mainServer.Close()
		if metricsServer != nil {
			_ = metricsServer.Close()
		}
		servers.Stop()
	})
	return runningRuntime{runtime: runtime, mainURL: "http://" + mainListener.Addr().String(), metricsURL: metricsURL}
}

func TestRuntimeShutdownLetsLongRequestFinishDuringDrain(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	canceled := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		close(started)
		select {
		case <-release:
			w.WriteHeader(http.StatusNoContent)
		case <-request.Context().Done():
			close(canceled)
		}
	})
	running := startRuntimeForTest(t, handler, nil, time.Second, 500*time.Millisecond)
	responseDone := make(chan error, 1)
	go func() {
		response, err := http.Get(running.mainURL)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode != http.StatusNoContent {
				err = errors.New("unexpected response status")
			}
		}
		responseDone <- err
	}()
	awaitTestSignal(t, started, "long request start")
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- running.runtime.run("signal") }()
	awaitRuntimePhase(t, running.runtime.readiness, lifecycle.PhaseDraining)
	select {
	case <-canceled:
		t.Fatal("request context was canceled before drain deadline")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	if err := awaitTestError(t, responseDone, "long request completion"); err != nil {
		t.Fatal(err)
	}
	if err := awaitTestError(t, shutdownDone, "runtime shutdown"); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestRuntimeShutdownDrainsAndEventuallyCancelsSSE(t *testing.T) {
	t.Run("natural completion", func(t *testing.T) {
		release := make(chan struct{})
		canceled := make(chan struct{})
		handler := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: ready\n\n")
			w.(http.Flusher).Flush()
			select {
			case <-release:
				_, _ = io.WriteString(w, "data: done\n\n")
			case <-request.Context().Done():
				close(canceled)
			}
		})
		running := startRuntimeForTest(t, handler, nil, time.Second, 400*time.Millisecond)
		response, err := http.Get(running.mainURL)
		if err != nil {
			t.Fatalf("open SSE: %v", err)
		}
		reader := bufio.NewReader(response.Body)
		if line, readErr := reader.ReadString('\n'); readErr != nil || line != "data: ready\n" {
			t.Fatalf("first SSE event = %q, %v", line, readErr)
		}
		shutdownDone := make(chan error, 1)
		go func() { shutdownDone <- running.runtime.run("signal") }()
		awaitRuntimePhase(t, running.runtime.readiness, lifecycle.PhaseDraining)
		close(release)
		_, _ = io.Copy(io.Discard, reader)
		_ = response.Body.Close()
		if err := awaitTestError(t, shutdownDone, "natural SSE shutdown"); err != nil {
			t.Fatalf("shutdown: %v", err)
		}
		select {
		case <-canceled:
			t.Fatal("naturally completed SSE was canceled")
		default:
		}
	})

	t.Run("deadline cancellation", func(t *testing.T) {
		canceled := make(chan time.Time, 1)
		handler := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: ready\n\n")
			w.(http.Flusher).Flush()
			<-request.Context().Done()
			canceled <- time.Now()
		})
		running := startRuntimeForTest(t, handler, nil, time.Second, 100*time.Millisecond)
		response, err := http.Get(running.mainURL)
		if err != nil {
			t.Fatalf("open SSE: %v", err)
		}
		reader := bufio.NewReader(response.Body)
		_, _ = reader.ReadString('\n')
		started := time.Now()
		shutdownErr := running.runtime.run("signal")
		canceledAt := <-canceled
		_ = response.Body.Close()
		if !errors.Is(shutdownErr, errRequestDrainTimedOut) {
			t.Fatalf("shutdown = %v, want request drain timeout", shutdownErr)
		}
		if elapsed := canceledAt.Sub(started); elapsed < 75*time.Millisecond {
			t.Fatalf("SSE canceled after %s, before drain deadline", elapsed)
		}
	})
}

func TestRuntimeShutdownForcesSlowUploadAtDrainDeadline(t *testing.T) {
	started := make(chan struct{})
	finished := make(chan struct{})
	handler := http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(started)
		_, _ = io.Copy(io.Discard, request.Body)
		close(finished)
	})
	running := startRuntimeForTest(t, handler, nil, time.Second, 100*time.Millisecond)
	connection, err := net.Dial("tcp", strings.TrimPrefix(running.mainURL, "http://"))
	if err != nil {
		t.Fatalf("dial slow upload: %v", err)
	}
	defer connection.Close()
	_, _ = io.WriteString(connection, "POST / HTTP/1.1\r\nHost: test\r\nContent-Length: 1024\r\nConnection: close\r\n\r\nx")
	awaitTestSignal(t, started, "slow upload start")
	startedAt := time.Now()
	err = running.runtime.run("signal")
	select {
	case <-finished:
	default:
		t.Fatal("runtime shutdown returned before slow upload handler exited")
	}
	if !errors.Is(err, errRequestDrainTimedOut) {
		t.Fatalf("shutdown = %v, want request drain timeout", err)
	}
	if elapsed := time.Since(startedAt); elapsed < 75*time.Millisecond {
		t.Fatalf("slow upload stopped after %s, before drain deadline", elapsed)
	}
}

func TestRuntimeShutdownCancelsWaitingUpstreamOnlyAtDeadline(t *testing.T) {
	canceled := make(chan time.Time, 1)
	started := make(chan struct{})
	finished := make(chan struct{})
	handler := http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		defer close(finished)
		close(started)
		<-request.Context().Done()
		canceled <- time.Now()
	})
	running := startRuntimeForTest(t, handler, nil, time.Second, 100*time.Millisecond)
	requestDone := make(chan struct{})
	go func() {
		_, _ = http.Get(running.mainURL)
		close(requestDone)
	}()
	awaitTestSignal(t, started, "upstream wait start")
	startedAt := time.Now()
	err := running.runtime.run("signal")
	canceledAt := <-canceled
	select {
	case <-finished:
	default:
		t.Fatal("runtime shutdown returned before waiting request exited")
	}
	if !errors.Is(err, errRequestDrainTimedOut) {
		t.Fatalf("shutdown = %v, want request drain timeout", err)
	}
	if elapsed := canceledAt.Sub(startedAt); elapsed < 75*time.Millisecond {
		t.Fatalf("upstream wait canceled after %s, before drain deadline", elapsed)
	}
	awaitTestSignal(t, requestDone, "waiting request client exit")
}

func TestRuntimeShutdownWaitsForCanceledHandlerCleanup(t *testing.T) {
	started := make(chan struct{})
	cleanupFinished := make(chan struct{})
	handler := http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(started)
		<-request.Context().Done()
		time.Sleep(30 * time.Millisecond)
		close(cleanupFinished)
	})
	running := startRuntimeForTest(t, handler, nil, time.Second, 50*time.Millisecond)
	go func() { _, _ = http.Get(running.mainURL) }()
	awaitTestSignal(t, started, "cleanup request start")
	if err := running.runtime.run("signal"); !errors.Is(err, errRequestDrainTimedOut) {
		t.Fatalf("shutdown = %v, want request drain timeout", err)
	}
	select {
	case <-cleanupFinished:
	default:
		t.Fatal("runtime shutdown returned before deferred handler cleanup")
	}
}

func TestRuntimeShutdownKeepsMetricsReadableDuringMainDrain(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(started)
		<-release
	})
	registry := metrics.New(nil)
	running := startRuntimeForTest(t, handler, registry, time.Second, 500*time.Millisecond)
	requestDone := make(chan struct{})
	go func() {
		_, _ = http.Get(running.mainURL)
		close(requestDone)
	}()
	awaitTestSignal(t, started, "main request start")
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- running.runtime.run("signal") }()
	awaitRuntimePhase(t, running.runtime.readiness, lifecycle.PhaseDraining)
	response, err := http.Get(running.metricsURL)
	if err != nil {
		t.Fatalf("scrape metrics during drain: %v", err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatalf("read metrics during drain: %v", err)
	}
	if !strings.Contains(string(body), "n2api_draining 1") {
		t.Fatal("metrics did not expose draining state")
	}
	close(release)
	awaitTestSignal(t, requestDone, "main request completion")
	if err := awaitTestError(t, shutdownDone, "metrics-aware shutdown"); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestRuntimeShutdownStopsBackgroundBeforeAlertsAndMetrics(t *testing.T) {
	registry := metrics.New(nil)
	running := startRuntimeForTest(t, http.NotFoundHandler(), registry, time.Second, 500*time.Millisecond)
	backgroundDone := make(chan struct{})
	background := lifecycle.NewSupervisor(context.Background())
	if err := background.Start("producer", func(ctx context.Context) error {
		<-ctx.Done()
		close(backgroundDone)
		return nil
	}); err != nil {
		t.Fatalf("start background producer: %v", err)
	}
	alertsCalled := make(chan struct{})
	running.runtime.background = background
	running.runtime.alerts = &observingAlertShutdown{
		backgroundDone: backgroundDone, metricsURL: running.metricsURL, called: alertsCalled,
	}
	if err := running.runtime.run("signal"); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	awaitTestSignal(t, alertsCalled, "ordered alert shutdown")
	if _, err := http.Get(running.metricsURL); err == nil {
		t.Fatal("metrics listener remained reachable after shutdown")
	}
}

func TestRuntimeShutdownFinalizesResourcesWithinGlobalDeadline(t *testing.T) {
	running := startRuntimeForTest(t, http.NotFoundHandler(), nil, time.Second, 500*time.Millisecond)
	finalized := make(chan struct{})
	running.runtime.finalizeResources = func(ctx context.Context) error {
		if _, ok := ctx.Deadline(); !ok {
			return errors.New("finalizer context has no global deadline")
		}
		close(finalized)
		return nil
	}
	if err := running.runtime.run("signal"); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	awaitTestSignal(t, finalized, "resource finalization")
}

func TestRuntimeShutdownDoesNotBlockOnResourceFinalizerPastGlobalDeadline(t *testing.T) {
	running := startRuntimeForTest(t, http.NotFoundHandler(), nil, 40*time.Millisecond, 10*time.Millisecond)
	release := make(chan struct{})
	running.runtime.finalizeResources = func(context.Context) error {
		<-release
		return nil
	}
	startedAt := time.Now()
	err := running.runtime.run("signal")
	close(release)
	if err == nil || !strings.Contains(err.Error(), "resource shutdown deadline exceeded") {
		t.Fatalf("shutdown = %v, want resource shutdown deadline exceeded", err)
	}
	if elapsed := time.Since(startedAt); elapsed > 500*time.Millisecond {
		t.Fatalf("shutdown blocked for %s after global deadline", elapsed)
	}
}

func awaitRuntimePhase(t *testing.T, readiness *lifecycle.Readiness, phase lifecycle.Phase) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if readiness.Snapshot().Phase == phase {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("runtime phase = %s, want %s", readiness.Snapshot().Phase, phase)
}

func awaitTestSignal(t *testing.T, signal <-chan struct{}, operation string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", operation)
	}
}

func awaitTestError(t *testing.T, result <-chan error, operation string) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", operation)
		return nil
	}
}
