package requestlog

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWriteMonitorTracksFailuresAndRecoveryWithoutLoggingDetails(t *testing.T) {
	var output bytes.Buffer
	monitor := NewWriteMonitor(slog.New(slog.NewTextHandler(&output, nil)))
	now := time.Date(2026, time.July, 23, 14, 0, 0, 0, time.UTC)
	monitor.now = func() time.Time { return now }

	monitor.Observe("request-42", errors.New("postgres://secret@database/n2api SQL body-secret"))
	now = now.Add(time.Minute)
	monitor.Observe("request-43", errors.New("second sensitive failure"))

	failed := monitor.RequestLogWriteStatus()
	if failed.LastSucceededAt != nil || failed.LastFailedAt == nil || !failed.LastFailedAt.Equal(now) {
		t.Fatalf("failed status timestamps = %+v", failed)
	}
	if failed.LastErrorCode != WriteFailedErrorCode || failed.ConsecutiveFailures != 2 || failed.TotalFailures != 2 {
		t.Fatalf("failed status counters = %+v", failed)
	}
	logs := output.String()
	for _, prohibited := range []string{"postgres://", "secret@", "SQL", "body-secret", "second sensitive failure"} {
		if strings.Contains(logs, prohibited) {
			t.Fatalf("process log leaked %q: %s", prohibited, logs)
		}
	}
	for _, required := range []string{"correlation_id=request-42", "error_code=" + WriteFailedErrorCode} {
		if !strings.Contains(logs, required) {
			t.Fatalf("process log missing %q: %s", required, logs)
		}
	}

	now = now.Add(time.Minute)
	monitor.Observe("request-44", nil)
	recovered := monitor.RequestLogWriteStatus()
	if recovered.LastSucceededAt == nil || !recovered.LastSucceededAt.Equal(now) || recovered.ConsecutiveFailures != 0 {
		t.Fatalf("recovered status = %+v", recovered)
	}
	if recovered.LastFailedAt == nil || !recovered.LastFailedAt.Equal(*failed.LastFailedAt) || recovered.LastErrorCode != WriteFailedErrorCode || recovered.TotalFailures != 2 {
		t.Fatalf("recovered status lost failure history = %+v", recovered)
	}
}

func TestWriteMonitorSupportsConcurrentObservations(t *testing.T) {
	monitor := NewWriteMonitor(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	const failures = 64
	var wait sync.WaitGroup
	wait.Add(failures)
	for i := 0; i < failures; i++ {
		go func() {
			defer wait.Done()
			monitor.Observe("concurrent-request", errors.New("unavailable"))
		}()
	}
	wait.Wait()

	status := monitor.RequestLogWriteStatus()
	if status.ConsecutiveFailures != failures || status.TotalFailures != failures || status.LastFailedAt == nil {
		t.Fatalf("concurrent status = %+v", status)
	}
}
