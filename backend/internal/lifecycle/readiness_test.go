package lifecycle

import (
	"sync"
	"testing"
)

func TestReadinessTransitionsAreMonotonic(t *testing.T) {
	readiness := NewReadiness()
	if snapshot := readiness.Snapshot(); snapshot.Phase != PhaseStarting || snapshot.Ready() {
		t.Fatalf("initial snapshot = %+v", snapshot)
	}
	if !readiness.MarkReady() || !readiness.Snapshot().Ready() {
		t.Fatal("readiness did not transition to ready")
	}
	if !readiness.BeginDrain("signal") {
		t.Fatal("readiness did not transition to draining")
	}
	if readiness.MarkReady() {
		t.Fatal("draining readiness transitioned back to ready")
	}
	if snapshot := readiness.Snapshot(); snapshot.Phase != PhaseDraining || snapshot.Reason != "signal" {
		t.Fatalf("draining snapshot = %+v", snapshot)
	}
	if !readiness.MarkFailed("shutdown_timeout") {
		t.Fatal("readiness did not transition to failed")
	}
	if readiness.BeginDrain("other") || readiness.MarkReady() {
		t.Fatal("failed readiness accepted a later transition")
	}
}

func TestReadinessSupportsConcurrentSnapshots(t *testing.T) {
	readiness := NewReadiness()
	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range 100 {
				_ = readiness.Snapshot()
			}
		}()
	}
	readiness.MarkReady()
	readiness.BeginDrain("test")
	wait.Wait()
}
