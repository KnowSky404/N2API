package provider

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

type autoTestService interface {
	TestAccounts(ctx context.Context) ([]Account, error)
}

type AutoTestRunnerConfig struct {
	Enabled  bool
	Interval time.Duration
}

type AutoTestRunnerConfigSource func(ctx context.Context) (AutoTestRunnerConfig, error)

type AutoTestRunner struct {
	service      autoTestService
	cfg          AutoTestRunnerConfig
	configSource AutoTestRunnerConfigSource
	logger       *slog.Logger
	running      atomic.Bool
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
	accounts, err := r.service.TestAccounts(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		r.logger.Warn("provider account auto test failed", "error", err, "duration", time.Since(started))
		return
	}
	r.logger.Info("provider account auto test completed", "accounts", len(accounts), "duration", time.Since(started))
}
