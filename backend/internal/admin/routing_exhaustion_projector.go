package admin

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultRoutingExhaustionProjectorInterval        = time.Minute
	defaultRoutingExhaustionProjectorBatchSize       = 1000
	defaultRoutingExhaustionProjectorTransitionLimit = 100
)

type RoutingExhaustionProjectorConfig struct {
	Interval        time.Duration
	BatchSize       int
	TransitionLimit int
}

type RoutingExhaustionProjectorCycleResult struct {
	Processed        int
	Transitions      int
	LastRequestLogID int64
	Contended        bool
}

type RoutingExhaustionProjectorStatus struct {
	Running          bool       `json:"running"`
	LastStartedAt    *time.Time `json:"lastStartedAt,omitempty"`
	LastSucceededAt  *time.Time `json:"lastSucceededAt,omitempty"`
	LastErrorAt      *time.Time `json:"lastErrorAt,omitempty"`
	LastErrorCode    string     `json:"lastErrorCode"`
	LastProcessed    int        `json:"lastProcessed"`
	LastTransitions  int        `json:"lastTransitions"`
	LastRequestLogID int64      `json:"lastRequestLogId"`
}

type routingExhaustionProjectorStore interface {
	RunRoutingExhaustionProjectorCycle(ctx context.Context, batchLimit, transitionLimit int, now time.Time) (RoutingExhaustionProjectorCycleResult, error)
}

type RoutingExhaustionProjector struct {
	store    routingExhaustionProjectorStore
	cfg      RoutingExhaustionProjectorConfig
	logger   *slog.Logger
	running  atomic.Bool
	statusMu sync.Mutex
	status   RoutingExhaustionProjectorStatus
	now      func() time.Time
	metrics  BackgroundTaskObserver
}

func NewRoutingExhaustionProjector(store routingExhaustionProjectorStore, cfg RoutingExhaustionProjectorConfig, logger *slog.Logger) *RoutingExhaustionProjector {
	if cfg.Interval <= 0 {
		cfg.Interval = defaultRoutingExhaustionProjectorInterval
	}
	if cfg.BatchSize <= 0 || cfg.BatchSize > defaultRoutingExhaustionProjectorBatchSize {
		cfg.BatchSize = defaultRoutingExhaustionProjectorBatchSize
	}
	if cfg.TransitionLimit <= 0 || cfg.TransitionLimit > defaultRoutingExhaustionProjectorTransitionLimit {
		cfg.TransitionLimit = defaultRoutingExhaustionProjectorTransitionLimit
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &RoutingExhaustionProjector{store: store, cfg: cfg, logger: logger, now: time.Now}
}

func (p *RoutingExhaustionProjector) Status() RoutingExhaustionProjectorStatus {
	if p == nil {
		return RoutingExhaustionProjectorStatus{}
	}
	p.statusMu.Lock()
	defer p.statusMu.Unlock()
	return p.status
}

func (p *RoutingExhaustionProjector) SetMetricsObserver(observer BackgroundTaskObserver) {
	if p != nil {
		p.metrics = observer
	}
}

func (p *RoutingExhaustionProjector) Run(ctx context.Context) {
	if p == nil || p.store == nil {
		return
	}
	for {
		p.runCycle(ctx)
		timer := time.NewTimer(p.cfg.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (p *RoutingExhaustionProjector) runCycle(ctx context.Context) {
	if !p.running.CompareAndSwap(false, true) {
		if p.metrics != nil {
			p.metrics.ObserveBackgroundTaskRun("routing_exhaustion_projector", "skipped", 0)
		}
		return
	}
	defer p.running.Store(false)
	outcome := "failure"
	finishMetrics := func(string) {}
	if p.metrics != nil {
		finishMetrics = p.metrics.BeginBackgroundTask("routing_exhaustion_projector")
	}
	defer func() { finishMetrics(outcome) }()

	started := p.now().UTC()
	p.statusMu.Lock()
	p.status.Running = true
	p.status.LastStartedAt = &started
	p.statusMu.Unlock()

	result, err := p.store.RunRoutingExhaustionProjectorCycle(ctx, p.cfg.BatchSize, p.cfg.TransitionLimit, started)
	finished := p.now().UTC()
	p.statusMu.Lock()
	p.status.Running = false
	if err == nil {
		if result.Contended {
			outcome = "skipped"
			p.statusMu.Unlock()
			return
		}
		outcome = "success"
		p.status.LastSucceededAt = &finished
		p.status.LastErrorAt = nil
		p.status.LastErrorCode = ""
		p.status.LastProcessed = result.Processed
		p.status.LastTransitions = result.Transitions
		p.status.LastRequestLogID = result.LastRequestLogID
		p.statusMu.Unlock()
		return
	}
	p.status.LastErrorAt = &finished
	p.status.LastErrorCode = "routing_exhaustion_projector_failed"
	if ctx.Err() != nil {
		outcome = "canceled"
		p.status.LastErrorCode = "routing_exhaustion_projector_canceled"
	} else if result.Processed > 0 || result.Transitions > 0 {
		outcome = "partial"
	}
	p.statusMu.Unlock()
	if ctx.Err() == nil {
		p.logger.Warn("routing exhaustion projector cycle failed", "error_code", "routing_exhaustion_projector_failed")
	}
}
