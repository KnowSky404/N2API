package gateway

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

const (
	defaultResponseAffinityRetentionInterval  = 24 * time.Hour
	defaultResponseAffinityRetentionBatchSize = 1000
)

type ResponseAffinityRetentionLease interface {
	DeleteExpiredBatch(ctx context.Context, cutoff time.Time, batchSize int) (int64, error)
	Close() error
}

type ResponseAffinityRetentionStore interface {
	TryAcquireResponseAffinityRetention(ctx context.Context) (ResponseAffinityRetentionLease, bool, error)
}

type ResponseAffinityRetentionRunnerConfig struct {
	Enabled   bool
	Interval  time.Duration
	BatchSize int
}

type ResponseAffinityRetentionStatus struct {
	AutomaticEnabled  bool       `json:"automaticEnabled"`
	Running           bool       `json:"running"`
	LastStartedAt     *time.Time `json:"lastStartedAt,omitempty"`
	LastSucceededAt   *time.Time `json:"lastSucceededAt,omitempty"`
	LastFailedAt      *time.Time `json:"lastFailedAt,omitempty"`
	LastErrorCode     string     `json:"lastErrorCode"`
	LastDeletedCount  int64      `json:"lastDeletedCount"`
	LastLockSkippedAt *time.Time `json:"lastLockSkippedAt,omitempty"`
}

type responseAffinityRetentionEventRecorder interface {
	Insert(ctx context.Context, event systemevent.Event) error
}

type ResponseAffinityRetentionRunner struct {
	store         ResponseAffinityRetentionStore
	cfg           ResponseAffinityRetentionRunnerConfig
	logger        *slog.Logger
	eventRecorder responseAffinityRetentionEventRecorder
	running       atomic.Bool
	statusMu      sync.Mutex
	status        ResponseAffinityRetentionStatus
	now           func() time.Time
}

func NewResponseAffinityRetentionRunner(store ResponseAffinityRetentionStore, cfg ResponseAffinityRetentionRunnerConfig, logger *slog.Logger) *ResponseAffinityRetentionRunner {
	if cfg.Interval <= 0 {
		cfg.Interval = defaultResponseAffinityRetentionInterval
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = defaultResponseAffinityRetentionBatchSize
	}
	return &ResponseAffinityRetentionRunner{
		store:  store,
		cfg:    cfg,
		logger: normalizedProcessLogger(logger),
		status: ResponseAffinityRetentionStatus{AutomaticEnabled: cfg.Enabled},
		now:    time.Now,
	}
}

func (r *ResponseAffinityRetentionRunner) SetSystemEventRecorder(recorder responseAffinityRetentionEventRecorder) {
	if r != nil {
		r.eventRecorder = recorder
	}
}

func (r *ResponseAffinityRetentionRunner) ResponseAffinityRetentionStatus() ResponseAffinityRetentionStatus {
	if r == nil {
		return ResponseAffinityRetentionStatus{}
	}
	r.statusMu.Lock()
	defer r.statusMu.Unlock()
	return r.status
}

func (r *ResponseAffinityRetentionRunner) Run(ctx context.Context) {
	if r == nil || r.store == nil || !r.cfg.Enabled {
		return
	}
	for {
		r.runCycle(ctx)
		timer := time.NewTimer(r.cfg.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (r *ResponseAffinityRetentionRunner) runCycle(ctx context.Context) {
	if !r.running.CompareAndSwap(false, true) {
		return
	}
	defer r.running.Store(false)

	started := r.now().UTC()
	r.start(started)
	lease, acquired, err := r.store.TryAcquireResponseAffinityRetention(ctx)
	if err != nil {
		r.fail(ctx, started, 0, "response_affinity_retention_acquire_failed")
		return
	}
	if !acquired {
		r.skip(started)
		return
	}

	deleted, deleteErr := lease.DeleteExpiredBatch(ctx, started, r.cfg.BatchSize)
	closeErr := lease.Close()
	if deleteErr != nil {
		if errors.Is(deleteErr, context.Canceled) || ctx.Err() != nil {
			r.finishFailure(r.now().UTC(), deleted, "response_affinity_retention_canceled")
			return
		}
		r.fail(ctx, started, deleted, "response_affinity_retention_failed")
		return
	}
	if closeErr != nil {
		r.fail(ctx, started, deleted, "response_affinity_retention_release_failed")
		return
	}

	finished := r.now().UTC()
	r.finishSuccess(finished, deleted)
	r.recordEvent(ctx, started, deleted, true, "")
	r.logger.Info("response affinity retention completed", "deleted_count", deleted)
}

func (r *ResponseAffinityRetentionRunner) fail(ctx context.Context, started time.Time, deleted int64, errorCode string) {
	finished := r.now().UTC()
	r.finishFailure(finished, deleted, errorCode)
	if ctx.Err() != nil {
		return
	}
	r.recordEvent(ctx, started, deleted, false, errorCode)
	r.logger.Warn("response affinity retention failed", "error_code", errorCode, "deleted_count", deleted)
}

func (r *ResponseAffinityRetentionRunner) recordEvent(ctx context.Context, started time.Time, deleted int64, succeeded bool, errorCode string) {
	if r.eventRecorder == nil {
		return
	}
	action := systemevent.ActionSchedulerResponseAffinityRetentionSucceeded
	severity := systemevent.SeverityInfo
	outcome := systemevent.OutcomeSuccess
	if !succeeded {
		action = systemevent.ActionSchedulerResponseAffinityRetentionFailed
		severity = systemevent.SeverityError
		outcome = systemevent.OutcomeFailure
		if deleted > 0 {
			severity = systemevent.SeverityWarning
			outcome = systemevent.OutcomePartial
		}
	}
	metadata, _ := systemevent.SafeMetadata(map[string]any{
		"deleted_count": deleted,
		"batch_size":    r.cfg.BatchSize,
		"cutoff":        started.Format(time.RFC3339),
	}, "deleted_count", "batch_size", "cutoff")
	intent := systemevent.EventIntent{
		Category: systemevent.CategoryScheduler,
		Severity: severity,
		Action:   action,
		Outcome:  outcome,
		Target: systemevent.Target{
			Type: "response_affinity_collection",
			ID:   "retention",
			Name: "Response affinity retention",
		},
		ErrorCode: errorCode,
		Metadata:  metadata,
	}
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), responseAffinityWriteTimeout)
	defer cancel()
	recordCtx = systemevent.WithRequestContext(recordCtx, systemevent.RequestContext{
		CorrelationID: systemevent.NewCorrelationID(),
		Actor:         systemevent.Actor{Type: systemevent.ActorSystem, Name: "response_affinity_retention"},
	})
	finished := r.now().UTC()
	event := systemevent.BuildEvent(recordCtx, intent, intent.Target, finished, finished.Sub(started))
	if err := r.eventRecorder.Insert(recordCtx, event); err != nil {
		r.logger.Warn("response affinity retention system event failed", "error_code", "response_affinity_retention_event_write_failed")
	}
}

func (r *ResponseAffinityRetentionRunner) start(started time.Time) {
	r.statusMu.Lock()
	defer r.statusMu.Unlock()
	r.status.Running = true
	r.status.LastStartedAt = &started
}

func (r *ResponseAffinityRetentionRunner) finishSuccess(finished time.Time, deleted int64) {
	r.statusMu.Lock()
	defer r.statusMu.Unlock()
	r.status.Running = false
	r.status.LastSucceededAt = &finished
	r.status.LastFailedAt = nil
	r.status.LastErrorCode = ""
	r.status.LastDeletedCount = deleted
}

func (r *ResponseAffinityRetentionRunner) finishFailure(finished time.Time, deleted int64, errorCode string) {
	r.statusMu.Lock()
	defer r.statusMu.Unlock()
	r.status.Running = false
	r.status.LastFailedAt = &finished
	r.status.LastErrorCode = errorCode
	r.status.LastDeletedCount = deleted
}

func (r *ResponseAffinityRetentionRunner) skip(skipped time.Time) {
	r.statusMu.Lock()
	defer r.statusMu.Unlock()
	r.status.Running = false
	r.status.LastLockSkippedAt = &skipped
}
