package lifecycle

import "sync"

type Phase string

const (
	PhaseStarting Phase = "starting"
	PhaseReady    Phase = "ready"
	PhaseDraining Phase = "draining"
	PhaseFailed   Phase = "failed"
)

type ReadinessSnapshot struct {
	Phase  Phase
	Reason string
}

func (s ReadinessSnapshot) Ready() bool {
	return s.Phase == PhaseReady
}

type Readiness struct {
	mu       sync.RWMutex
	snapshot ReadinessSnapshot
}

func NewReadiness() *Readiness {
	return &Readiness{snapshot: ReadinessSnapshot{Phase: PhaseStarting}}
}

func (r *Readiness) Snapshot() ReadinessSnapshot {
	if r == nil {
		return ReadinessSnapshot{Phase: PhaseFailed, Reason: "runtime_not_configured"}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.snapshot
}

func (r *Readiness) MarkReady() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.snapshot.Phase != PhaseStarting {
		return false
	}
	r.snapshot = ReadinessSnapshot{Phase: PhaseReady}
	return true
}

func (r *Readiness) BeginDrain(reason string) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.snapshot.Phase == PhaseDraining || r.snapshot.Phase == PhaseFailed {
		return false
	}
	r.snapshot = ReadinessSnapshot{Phase: PhaseDraining, Reason: reason}
	return true
}

func (r *Readiness) MarkFailed(reason string) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.snapshot.Phase == PhaseFailed {
		return false
	}
	r.snapshot = ReadinessSnapshot{Phase: PhaseFailed, Reason: reason}
	return true
}
