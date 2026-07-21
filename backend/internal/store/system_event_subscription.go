package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	systemEventNotificationChannel = "n2api_system_events"
	systemEventUnlistenTimeout     = 2 * time.Second
)

var (
	ErrInvalidSystemEventNotification      = errors.New("invalid system event notification")
	ErrSystemEventSubscriptionClosed       = errors.New("system event subscription is closed")
	ErrInsufficientSystemEventPoolCapacity = errors.New("system event subscription requires at least two pool connections")
)

type SystemEventSubscription interface {
	Wait(ctx context.Context) (int64, error)
	Close()
}

type postgresSystemEventSubscription struct {
	conn      *pgxpool.Conn
	closeOnce sync.Once
	closed    chan struct{}
}

// Subscribe reserves one pool connection until the returned subscription is
// closed. Callers must leave enough pool capacity for ordinary application work.
func (r *SystemEventRepository) Subscribe(ctx context.Context) (SystemEventSubscription, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("system event repository is not configured")
	}
	if r.pool.Config().MaxConns < 2 {
		return nil, ErrInsufficientSystemEventPoolCapacity
	}
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire system event notification connection: %w", err)
	}
	if _, err := conn.Exec(ctx, "LISTEN "+systemEventNotificationChannel); err != nil {
		conn.Release()
		return nil, fmt.Errorf("listen for system event notifications: %w", err)
	}
	return &postgresSystemEventSubscription{
		conn:   conn,
		closed: make(chan struct{}),
	}, nil
}

func (s *postgresSystemEventSubscription) Wait(ctx context.Context) (int64, error) {
	if s == nil || s.conn == nil {
		return 0, ErrSystemEventSubscriptionClosed
	}
	select {
	case <-s.closed:
		return 0, ErrSystemEventSubscriptionClosed
	default:
	}

	for {
		notification, err := s.conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return 0, fmt.Errorf("wait for system event notification: %w", err)
		}
		if notification.Channel != systemEventNotificationChannel {
			continue
		}
		id, err := parseSystemEventNotificationID(notification.Payload)
		if err != nil {
			return 0, err
		}
		return id, nil
	}
}

// Close must be called after any active Wait call has returned.
func (s *postgresSystemEventSubscription) Close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		close(s.closed)
		if s.conn == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), systemEventUnlistenTimeout)
		defer cancel()
		_, _ = s.conn.Exec(ctx, "UNLISTEN "+systemEventNotificationChannel)
		s.conn.Release()
		s.conn = nil
	})
}

func parseSystemEventNotificationID(payload string) (int64, error) {
	id, err := strconv.ParseInt(payload, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != payload {
		return 0, ErrInvalidSystemEventNotification
	}
	return id, nil
}
