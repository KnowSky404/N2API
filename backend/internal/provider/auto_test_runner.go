package provider

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

type autoTestService interface {
	TestAccounts(ctx context.Context) ([]Account, error)
}

type SystemEventRecorder interface {
	Insert(ctx context.Context, event systemevent.Event) error
}

type AutoTestRunnerConfig struct {
	Enabled  bool
	Interval time.Duration
}

type AutoTestStatus struct {
	Running          bool       `json:"running"`
	LastStartedAt    *time.Time `json:"lastStartedAt,omitempty"`
	LastFinishedAt   *time.Time `json:"lastFinishedAt,omitempty"`
	LastAccountCount int        `json:"lastAccountCount"`
	LastError        string     `json:"lastError"`
}

type AutoTestRunnerConfigSource func(ctx context.Context) (AutoTestRunnerConfig, error)

type AutoTestRunner struct {
	service       autoTestService
	cfg           AutoTestRunnerConfig
	configSource  AutoTestRunnerConfigSource
	logger        *slog.Logger
	eventRecorder SystemEventRecorder
	running       atomic.Bool
	statusMu      sync.Mutex
	status        AutoTestStatus
}

func (r *AutoTestRunner) SetSystemEventRecorder(recorder SystemEventRecorder) {
	if r != nil {
		r.eventRecorder = recorder
	}
}

func NewAutoTestRunner(service autoTestService, cfg AutoTestRunnerConfig, logger *slog.Logger) *AutoTestRunner {
	if logger == nil {
		logger = slog.Default()
	}
	return &AutoTestRunner{
		service: service,
		cfg:     cfg,
		logger:  logger,
	}
}

func NewAutoTestRunnerWithConfigSource(service autoTestService, source AutoTestRunnerConfigSource, logger *slog.Logger) *AutoTestRunner {
	if logger == nil {
		logger = slog.Default()
	}
	return &AutoTestRunner{
		service:      service,
		configSource: source,
		logger:       logger,
	}
}

func (r *AutoTestRunner) ProviderAccountAutoTestStatus() AutoTestStatus {
	if r == nil {
		return AutoTestStatus{}
	}
	r.statusMu.Lock()
	defer r.statusMu.Unlock()
	return r.status
}

func (r *AutoTestRunner) Run(ctx context.Context) {
	if r == nil || r.service == nil {
		return
	}
	if r.configSource == nil && !r.cfg.Enabled {
		return
	}

	for {
		cfg, err := r.currentConfig(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			r.logger.Warn("provider account auto test settings unavailable", "error", err)
		}
		interval := autoTestInterval(cfg.Interval)
		if err == nil && cfg.Enabled {
			r.runCycle(ctx)
		}
		if !waitAutoTestInterval(ctx, interval) {
			return
		}
	}
}

func (r *AutoTestRunner) currentConfig(ctx context.Context) (AutoTestRunnerConfig, error) {
	if r.configSource == nil {
		return r.cfg, nil
	}
	return r.configSource(ctx)
}

func autoTestInterval(interval time.Duration) time.Duration {
	if interval <= 0 {
		return 5 * time.Minute
	}
	return interval
}

func waitAutoTestInterval(ctx context.Context, interval time.Duration) bool {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (r *AutoTestRunner) runCycle(ctx context.Context) {
	if !r.running.CompareAndSwap(false, true) {
		r.logger.Debug("provider account auto test skipped because previous cycle is still running")
		return
	}
	defer r.running.Store(false)

	started := time.Now()
	r.setStatusStarted(started)
	cycleCtx := systemevent.WithRequestContext(ctx, systemevent.RequestContext{
		CorrelationID: systemevent.NewCorrelationID(),
		Actor:         systemevent.Actor{Type: systemevent.ActorSystem, Name: "provider_auto_test"},
	})
	accounts, err := r.service.TestAccounts(cycleCtx)
	if err != nil {
		if ctx.Err() != nil {
			r.setStatusFinished(time.Now(), len(accounts), ctx.Err().Error())
			r.recordCycleEvent(cycleCtx, started, len(accounts), err)
			return
		}
		r.setStatusFinished(time.Now(), len(accounts), err.Error())
		r.recordCycleEvent(cycleCtx, started, len(accounts), err)
		r.logger.Warn("provider account auto test failed", "error", err, "duration", time.Since(started))
		return
	}
	r.setStatusFinished(time.Now(), len(accounts), "")
	r.recordCycleEvent(cycleCtx, started, len(accounts), nil)
	r.logger.Info("provider account auto test completed", "accounts", len(accounts), "duration", time.Since(started))
}

func (r *AutoTestRunner) recordCycleEvent(ctx context.Context, started time.Time, accountCount int, cycleErr error) {
	if r.eventRecorder == nil {
		return
	}
	severity := systemevent.SeverityInfo
	outcome := systemevent.OutcomeSuccess
	errorCode := ""
	metadata := map[string]any{"account_count": accountCount}
	if cycleErr != nil {
		severity = systemevent.SeverityError
		outcome = systemevent.OutcomeFailure
		errorCode = "provider_auto_test_failed"
		var batchErr *accountBatchError
		if errors.As(cycleErr, &batchErr) {
			metadata["requested_count"] = batchErr.Requested
			metadata["attempted_count"] = batchErr.Attempted
			metadata["succeeded_count"] = batchErr.Succeeded
			metadata["failed_count"] = batchErr.Failed
			metadata["skipped_count"] = batchErr.Skipped
			if batchErr.Succeeded > 0 {
				outcome = systemevent.OutcomePartial
				severity = systemevent.SeverityWarning
			}
		}
	}
	intent := systemevent.EventIntent{
		Category:  systemevent.CategoryScheduler,
		Severity:  severity,
		Action:    systemevent.ActionSchedulerProviderAutoTestCompleted,
		Outcome:   outcome,
		Target:    systemevent.Target{Type: "provider_account_scheduler", ID: "auto_test", Name: "Provider account auto test"},
		ErrorCode: errorCode,
		Metadata:  metadata,
	}
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	event := systemevent.BuildEvent(recordCtx, intent, intent.Target, time.Now().UTC(), time.Since(started))
	if err := r.eventRecorder.Insert(recordCtx, event); err != nil {
		r.logger.Warn("record provider account auto test system event", "error", err)
	}
}

func (r *AutoTestRunner) setStatusStarted(started time.Time) {
	r.statusMu.Lock()
	defer r.statusMu.Unlock()
	r.status.Running = true
	r.status.LastStartedAt = &started
	r.status.LastFinishedAt = nil
	r.status.LastAccountCount = 0
	r.status.LastError = ""
}

func (r *AutoTestRunner) setStatusFinished(finished time.Time, accountCount int, lastError string) {
	r.statusMu.Lock()
	defer r.statusMu.Unlock()
	r.status.Running = false
	r.status.LastFinishedAt = &finished
	r.status.LastAccountCount = accountCount
	r.status.LastError = lastError
}
