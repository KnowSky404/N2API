package admin

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultAPIKeyBudgetMonitorInterval  = 5 * time.Minute
	defaultAPIKeyBudgetMonitorBatchSize = 100
)

type APIKeyBudgetMonitorConfig struct {
	Interval  time.Duration
	BatchSize int
}

type APIKeyBudgetMonitorCycleResult struct {
	Processed   int
	Transitions int
	NextAfterID int64
}

type APIKeyBudgetMonitorStatus struct {
	Running         bool       `json:"running"`
	LastStartedAt   *time.Time `json:"lastStartedAt,omitempty"`
	LastSucceededAt *time.Time `json:"lastSucceededAt,omitempty"`
	LastErrorAt     *time.Time `json:"lastErrorAt,omitempty"`
	LastErrorCode   string     `json:"lastErrorCode"`
	LastProcessed   int        `json:"lastProcessed"`
	LastTransitions int        `json:"lastTransitions"`
	CursorAfterID   int64      `json:"cursorAfterId"`
}

type apiKeyBudgetMonitorStore interface {
	RunAPIKeyBudgetMonitorCycle(ctx context.Context, afterID int64, limit int, now time.Time) (APIKeyBudgetMonitorCycleResult, error)
}

type APIKeyBudgetMonitor struct {
	store    apiKeyBudgetMonitorStore
	cfg      APIKeyBudgetMonitorConfig
	logger   *slog.Logger
	running  atomic.Bool
	statusMu sync.Mutex
	status   APIKeyBudgetMonitorStatus
	now      func() time.Time
}

func NewAPIKeyBudgetMonitor(store apiKeyBudgetMonitorStore, cfg APIKeyBudgetMonitorConfig, logger *slog.Logger) *APIKeyBudgetMonitor {
	if cfg.Interval <= 0 {
		cfg.Interval = defaultAPIKeyBudgetMonitorInterval
	}
	if cfg.BatchSize <= 0 || cfg.BatchSize > defaultAPIKeyBudgetMonitorBatchSize {
		cfg.BatchSize = defaultAPIKeyBudgetMonitorBatchSize
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &APIKeyBudgetMonitor{store: store, cfg: cfg, logger: logger, now: time.Now}
}

func (m *APIKeyBudgetMonitor) Status() APIKeyBudgetMonitorStatus {
	if m == nil {
		return APIKeyBudgetMonitorStatus{}
	}
	m.statusMu.Lock()
	defer m.statusMu.Unlock()
	return m.status
}

func (m *APIKeyBudgetMonitor) Run(ctx context.Context) {
	if m == nil || m.store == nil {
		return
	}
	for {
		m.runCycle(ctx)
		timer := time.NewTimer(m.cfg.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (m *APIKeyBudgetMonitor) runCycle(ctx context.Context) {
	if !m.running.CompareAndSwap(false, true) {
		return
	}
	defer m.running.Store(false)

	started := m.now().UTC()
	m.statusMu.Lock()
	afterID := m.status.CursorAfterID
	m.status.Running = true
	m.status.LastStartedAt = &started
	m.statusMu.Unlock()

	result, err := m.store.RunAPIKeyBudgetMonitorCycle(ctx, afterID, m.cfg.BatchSize, started)
	finished := m.now().UTC()
	m.statusMu.Lock()
	m.status.Running = false
	if err == nil {
		m.status.LastSucceededAt = &finished
		m.status.LastErrorAt = nil
		m.status.LastErrorCode = ""
		m.status.LastProcessed = result.Processed
		m.status.LastTransitions = result.Transitions
		m.status.CursorAfterID = result.NextAfterID
		m.statusMu.Unlock()
		return
	}
	m.status.LastErrorAt = &finished
	m.status.LastErrorCode = "api_key_budget_monitor_failed"
	if ctx.Err() != nil {
		m.status.LastErrorCode = "api_key_budget_monitor_canceled"
	}
	m.statusMu.Unlock()
	if ctx.Err() == nil {
		m.logger.Warn("API key budget monitor cycle failed", "error_code", "api_key_budget_monitor_failed")
	}
}
