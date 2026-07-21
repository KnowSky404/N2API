package admin

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

const defaultRequestLogRetentionInterval = 24 * time.Hour

type RequestLogRetentionRunnerConfig struct {
	Enabled   bool
	Interval  time.Duration
	BatchSize int
}

type RequestLogRetentionStatus struct {
	AutomaticEnabled bool       `json:"automaticEnabled"`
	Running          bool       `json:"running"`
	LastStartedAt    *time.Time `json:"lastStartedAt,omitempty"`
	LastSucceededAt  *time.Time `json:"lastSucceededAt,omitempty"`
	LastErrorAt      *time.Time `json:"lastErrorAt,omitempty"`
	LastErrorCode    string     `json:"lastErrorCode"`
	LastDeletedCount int64      `json:"lastDeletedCount"`
	LastBatchCount   int        `json:"lastBatchCount"`
	LastCutoff       *time.Time `json:"lastCutoff,omitempty"`
}

type requestLogRetentionService interface {
	GetGatewaySettings(ctx context.Context) (GatewaySettings, error)
	RunRequestLogRetention(ctx context.Context, retentionDays int, before time.Time, batchSize int) (RequestLogCleanupResult, error)
}

type requestLogRetentionEventRecorder interface {
	Insert(ctx context.Context, event systemevent.Event) error
}

type RequestLogRetentionRunner struct {
	service       requestLogRetentionService
	cfg           RequestLogRetentionRunnerConfig
	logger        *slog.Logger
	eventRecorder requestLogRetentionEventRecorder
	running       atomic.Bool
	statusMu      sync.Mutex
	status        RequestLogRetentionStatus
	now           func() time.Time
}

func NewRequestLogRetentionRunner(service requestLogRetentionService, cfg RequestLogRetentionRunnerConfig, logger *slog.Logger) *RequestLogRetentionRunner {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Interval <= 0 {
		cfg.Interval = defaultRequestLogRetentionInterval
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = DefaultRequestLogRetentionBatchSize
	}
	return &RequestLogRetentionRunner{
		service: service,
		cfg:     cfg,
		logger:  logger,
		status:  RequestLogRetentionStatus{AutomaticEnabled: cfg.Enabled},
		now:     time.Now,
	}
}

func (r *RequestLogRetentionRunner) SetSystemEventRecorder(recorder requestLogRetentionEventRecorder) {
	if r != nil {
		r.eventRecorder = recorder
	}
}

func (r *RequestLogRetentionRunner) RequestLogRetentionStatus() RequestLogRetentionStatus {
	if r == nil {
		return RequestLogRetentionStatus{}
	}
	r.statusMu.Lock()
	defer r.statusMu.Unlock()
	return r.status
}

func (r *RequestLogRetentionRunner) Run(ctx context.Context) {
	if r == nil || r.service == nil || !r.cfg.Enabled {
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

func (r *RequestLogRetentionRunner) runCycle(ctx context.Context) {
	if !r.running.CompareAndSwap(false, true) {
		return
	}
	defer r.running.Store(false)

	settings, err := r.service.GetGatewaySettings(ctx)
	if err != nil {
		if ctx.Err() == nil {
			started := r.now().UTC()
			r.start(started, time.Time{})
			r.finishFailure(r.now(), RequestLogCleanupResult{}, "request_log_retention_settings_failed")
			r.recordEvent(ctx, started, RequestLogCleanupResult{}, errors.New("settings unavailable"), "request_log_retention_settings_failed")
			r.logger.Warn("request log retention settings unavailable", "error", err)
		}
		return
	}
	if settings.RequestLogRetentionDays <= 0 {
		return
	}

	started := r.now().UTC()
	cutoff := started.Add(-time.Duration(settings.RequestLogRetentionDays) * 24 * time.Hour)
	r.start(started, cutoff)
	result, runErr := r.service.RunRequestLogRetention(ctx, settings.RequestLogRetentionDays, cutoff, r.cfg.BatchSize)
	if errors.Is(runErr, ErrConflict) {
		r.finishSkipped()
		return
	}
	if runErr != nil {
		errorCode := "request_log_retention_failed"
		if ctx.Err() != nil {
			errorCode = "request_log_retention_canceled"
		}
		finished := r.now().UTC()
		r.finishFailure(finished, result, errorCode)
		r.recordEvent(ctx, started, result, runErr, errorCode)
		if ctx.Err() == nil {
			r.logger.Warn("request log retention failed", "error", runErr, "deleted_count", result.Deleted, "batch_count", result.Batches)
		}
		return
	}
	finished := r.now().UTC()
	r.finishSuccess(finished, result)
	r.recordEvent(ctx, started, result, nil, "")
	r.logger.Info("request log retention completed", "deleted_count", result.Deleted, "batch_count", result.Batches)
}

func (r *RequestLogRetentionRunner) recordEvent(ctx context.Context, started time.Time, result RequestLogCleanupResult, runErr error, errorCode string) {
	if r.eventRecorder == nil {
		return
	}
	severity := systemevent.SeverityInfo
	outcome := systemevent.OutcomeSuccess
	action := systemevent.ActionSchedulerRequestLogRetentionSucceeded
	if runErr != nil {
		severity = systemevent.SeverityError
		outcome = systemevent.OutcomeFailure
		action = systemevent.ActionSchedulerRequestLogRetentionFailed
		if result.Deleted > 0 {
			severity = systemevent.SeverityWarning
			outcome = systemevent.OutcomePartial
		}
	}
	metadataValues := map[string]any{"deleted_count": result.Deleted, "batch_count": result.Batches}
	if !result.Before.IsZero() {
		metadataValues["cutoff"] = result.Before.UTC().Format(time.RFC3339)
	}
	if result.RetentionDays > 0 {
		metadataValues["retention_days"] = result.RetentionDays
	}
	metadata, _ := systemevent.SafeMetadata(metadataValues, "cutoff", "retention_days", "deleted_count", "batch_count")
	intent := systemevent.EventIntent{
		Category: systemevent.CategoryScheduler, Severity: severity, Action: action, Outcome: outcome,
		Target:    systemevent.Target{Type: "request_log_collection", ID: "retention", Name: "Request log retention"},
		ErrorCode: errorCode, Metadata: metadata,
	}
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	recordCtx = systemevent.WithRequestContext(recordCtx, systemevent.RequestContext{
		CorrelationID: systemevent.NewCorrelationID(), Actor: systemevent.Actor{Type: systemevent.ActorSystem, Name: "request_log_retention"},
	})
	finished := r.now().UTC()
	event := systemevent.BuildEvent(recordCtx, intent, intent.Target, finished, finished.Sub(started))
	if err := r.eventRecorder.Insert(recordCtx, event); err != nil {
		r.logger.Warn("record request log retention system event", "error", err)
	}
}

func (r *RequestLogRetentionRunner) start(started, cutoff time.Time) {
	r.statusMu.Lock()
	defer r.statusMu.Unlock()
	r.status.Running = true
	r.status.LastStartedAt = &started
	if !cutoff.IsZero() {
		r.status.LastCutoff = &cutoff
	}
}

func (r *RequestLogRetentionRunner) finishSuccess(finished time.Time, result RequestLogCleanupResult) {
	r.statusMu.Lock()
	defer r.statusMu.Unlock()
	r.status.Running = false
	r.status.LastSucceededAt = &finished
	r.status.LastErrorAt = nil
	r.status.LastErrorCode = ""
	r.status.LastDeletedCount = result.Deleted
	r.status.LastBatchCount = result.Batches
}

func (r *RequestLogRetentionRunner) finishFailure(finished time.Time, result RequestLogCleanupResult, errorCode string) {
	r.statusMu.Lock()
	defer r.statusMu.Unlock()
	r.status.Running = false
	r.status.LastErrorAt = &finished
	r.status.LastErrorCode = errorCode
	r.status.LastDeletedCount = result.Deleted
	r.status.LastBatchCount = result.Batches
}

func (r *RequestLogRetentionRunner) finishSkipped() {
	r.statusMu.Lock()
	defer r.statusMu.Unlock()
	r.status.Running = false
}
