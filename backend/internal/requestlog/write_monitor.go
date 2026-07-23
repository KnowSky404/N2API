package requestlog

import (
	"log/slog"
	"sync"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

const WriteFailedErrorCode = "request_log_write_failed"

type WriteStatus struct {
	LastSucceededAt     *time.Time `json:"lastSucceededAt,omitempty"`
	LastFailedAt        *time.Time `json:"lastFailedAt,omitempty"`
	LastErrorCode       string     `json:"lastErrorCode,omitempty"`
	ConsecutiveFailures uint64     `json:"consecutiveFailures"`
	TotalFailures       uint64     `json:"totalFailures"`
}

type WriteMonitor struct {
	mu       sync.RWMutex
	status   WriteStatus
	logger   *slog.Logger
	now      func() time.Time
	observer WriteObserver
}

type WriteObserver interface {
	ObserveRequestLogWrite(err error)
}

func NewWriteMonitor(logger *slog.Logger) *WriteMonitor {
	if logger == nil {
		logger = slog.Default()
	}
	return &WriteMonitor{logger: logger, now: time.Now}
}

func (m *WriteMonitor) SetObserver(observer WriteObserver) {
	if m != nil {
		m.observer = observer
	}
}

func (m *WriteMonitor) Observe(correlationID string, err error) {
	if m == nil {
		return
	}
	if m.observer != nil {
		m.observer.ObserveRequestLogWrite(err)
	}
	m.mu.Lock()
	now := m.now().UTC()
	if err == nil {
		m.status.LastSucceededAt = &now
		m.status.ConsecutiveFailures = 0
		m.mu.Unlock()
		return
	}
	m.status.LastFailedAt = &now
	m.status.LastErrorCode = WriteFailedErrorCode
	m.status.ConsecutiveFailures++
	m.status.TotalFailures++
	m.mu.Unlock()

	m.logger.Error(
		"request log write failed",
		"correlation_id", systemevent.NormalizeCorrelationID(correlationID),
		"error_code", WriteFailedErrorCode,
	)
}

func (m *WriteMonitor) RequestLogWriteStatus() WriteStatus {
	if m == nil {
		return WriteStatus{}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneWriteStatus(m.status)
}

func cloneWriteStatus(status WriteStatus) WriteStatus {
	if status.LastSucceededAt != nil {
		lastSucceededAt := *status.LastSucceededAt
		status.LastSucceededAt = &lastSucceededAt
	}
	if status.LastFailedAt != nil {
		lastFailedAt := *status.LastFailedAt
		status.LastFailedAt = &lastFailedAt
	}
	return status
}
