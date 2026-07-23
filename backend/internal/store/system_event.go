package store

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/systemevent"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SystemEventFilter = systemevent.Filter
type SystemEventPage = systemevent.Page

type SystemEventRepository struct {
	pool          *pgxpool.Pool
	cursorSecret  []byte
	writeObserver systemevent.WriteObserver
}

func NewSystemEventRepository(pool *pgxpool.Pool, cursorSecret string) *SystemEventRepository {
	key := sha256.Sum256([]byte("n2api-system-event-cursor\x00" + cursorSecret))
	return &SystemEventRepository{pool: pool, cursorSecret: key[:]}
}

func (r *SystemEventRepository) Insert(ctx context.Context, event systemevent.Event) error {
	if r != nil {
		ctx = systemEventWriteContext(ctx, r.writeObserver)
	}
	return insertSystemEvent(ctx, r.pool, event)
}

func (r *SystemEventRepository) SetWriteObserver(observer systemevent.WriteObserver) {
	if r != nil {
		r.writeObserver = observer
	}
}

func systemEventWriteContext(ctx context.Context, fallback systemevent.WriteObserver) context.Context {
	if systemevent.WriteObserverFromContext(ctx) != nil || fallback == nil {
		return ctx
	}
	return systemevent.WithWriteObserver(ctx, fallback)
}

func (r *SystemEventRepository) GetByID(ctx context.Context, id int64) (systemevent.Event, error) {
	if id <= 0 {
		return systemevent.Event{}, systemevent.ErrInvalidEvent
	}
	if r == nil || r.pool == nil {
		return systemevent.Event{}, errors.New("system event repository is not configured")
	}
	event, err := scanSystemEvent(r.pool.QueryRow(ctx, systemEventSelectSQL+` WHERE id = $1`, id))
	if err != nil {
		return systemevent.Event{}, fmt.Errorf("get system event by ID: %w", err)
	}
	return event, nil
}

func InsertSystemEventTx(ctx context.Context, tx pgx.Tx, event systemevent.Event) error {
	return insertSystemEvent(ctx, tx, event)
}

func insertSystemEvent(ctx context.Context, executor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, event systemevent.Event) (err error) {
	if observer := systemevent.WriteObserverFromContext(ctx); observer != nil {
		defer func() { observer.ObserveSystemEventWrite(err) }()
	}
	if validationErr := systemevent.ValidateEvent(event); validationErr != nil {
		err = validationErr
		return err
	}
	metadataValue := event.Metadata
	if metadataValue == nil {
		metadataValue = map[string]any{}
	}
	metadata, err := json.Marshal(metadataValue)
	if err != nil {
		return fmt.Errorf("encode system event metadata: %w", err)
	}
	var actorID any
	if event.Actor.ID > 0 {
		actorID = event.Actor.ID
	}
	var sourceIP any
	if event.SourceIP != "" {
		sourceIP = event.SourceIP
	}
	_, err = executor.Exec(ctx, insertSystemEventSQL,
		event.OccurredAt, event.Category, event.Severity, event.Action, event.Outcome,
		event.Actor.Type, actorID, event.Actor.Name, event.Target.Type, event.Target.ID, event.Target.Name,
		event.CorrelationID, sourceIP, event.HTTPMethod, event.RoutePattern, event.StatusCode,
		event.DurationMS, event.ErrorCode, event.Message, metadata,
	)
	if err != nil {
		return fmt.Errorf("insert system event: %w", err)
	}
	return nil
}

const insertSystemEventSQL = `
	INSERT INTO system_events (
		occurred_at, category, severity, action, outcome,
		actor_type, actor_id, actor_name, target_type, target_id, target_name,
		correlation_id, source_ip, http_method, route_pattern, status_code,
		duration_ms, error_code, message, metadata
	) VALUES (
		$1, $2, $3, $4, $5,
		$6, $7, $8, $9, $10, $11,
		$12, $13, $14, $15, $16,
		$17, $18, $19, $20
	)`

type systemEventCursor struct {
	OccurredAt time.Time `json:"t"`
	ID         int64     `json:"i"`
}

func (r *SystemEventRepository) List(ctx context.Context, filter SystemEventFilter) (SystemEventPage, error) {
	limit := filter.Limit
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 {
		return SystemEventPage{}, systemevent.ErrInvalidEvent
	}
	args := make([]any, 0, 12)
	where := make([]string, 0, 10)
	add := func(column string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	if !filter.Since.IsZero() {
		args = append(args, filter.Since.UTC())
		where = append(where, fmt.Sprintf("occurred_at >= $%d", len(args)))
	}
	if filter.Category != "" {
		add("category", filter.Category)
	}
	if filter.Outcome != "" {
		add("outcome", filter.Outcome)
	}
	if filter.Severity != "" {
		add("severity", filter.Severity)
	}
	if filter.Action != "" {
		add("action", filter.Action)
	}
	if filter.TargetType != "" {
		add("target_type", filter.TargetType)
	}
	if filter.TargetID != "" {
		add("target_id", filter.TargetID)
	}
	if filter.Actor != "" {
		args = append(args, "%"+escapeLike(filter.Actor)+"%")
		where = append(where, fmt.Sprintf("actor_name ILIKE $%d ESCAPE '\\'", len(args)))
	}
	if filter.Query != "" {
		args = append(args, "%"+escapeLike(filter.Query)+"%")
		position := len(args)
		where = append(where, fmt.Sprintf("(action ILIKE $%d ESCAPE '\\' OR actor_name ILIKE $%d ESCAPE '\\' OR target_type ILIKE $%d ESCAPE '\\' OR target_id ILIKE $%d ESCAPE '\\' OR target_name ILIKE $%d ESCAPE '\\' OR error_code ILIKE $%d ESCAPE '\\' OR message ILIKE $%d ESCAPE '\\')", position, position, position, position, position, position, position))
	}
	if filter.Cursor != "" {
		cursor, err := r.decodeCursor(filter.Cursor)
		if err != nil {
			return SystemEventPage{}, err
		}
		args = append(args, cursor.OccurredAt.UTC(), cursor.ID)
		where = append(where, fmt.Sprintf("(occurred_at, id) < ($%d, $%d)", len(args)-1, len(args)))
	}
	query := systemEventSelectSQL
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	args = append(args, limit+1)
	query += fmt.Sprintf(" ORDER BY occurred_at DESC, id DESC LIMIT $%d", len(args))
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return SystemEventPage{}, fmt.Errorf("list system events: %w", err)
	}
	defer rows.Close()
	events := make([]systemevent.Event, 0, limit+1)
	for rows.Next() {
		event, err := scanSystemEvent(rows)
		if err != nil {
			return SystemEventPage{}, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return SystemEventPage{}, fmt.Errorf("list system events: %w", err)
	}
	page := SystemEventPage{Events: events}
	if len(events) > limit {
		page.HasMore = true
		page.Events = events[:limit]
		last := page.Events[len(page.Events)-1]
		page.NextCursor, err = r.encodeCursor(systemEventCursor{OccurredAt: last.OccurredAt, ID: last.ID})
		if err != nil {
			return SystemEventPage{}, err
		}
	}
	return page, nil
}

const systemEventSelectSQL = `SELECT
	id, occurred_at, category, severity, action, outcome,
	actor_type, actor_id, actor_name, target_type, target_id, target_name,
	correlation_id, host(source_ip), http_method, route_pattern, status_code,
	duration_ms, error_code, message, metadata
FROM system_events`

type rowScanner interface{ Scan(...any) error }

func scanSystemEvent(row rowScanner) (systemevent.Event, error) {
	var event systemevent.Event
	var actorID *int64
	var sourceIP *string
	var metadata []byte
	err := row.Scan(&event.ID, &event.OccurredAt, &event.Category, &event.Severity, &event.Action, &event.Outcome,
		&event.Actor.Type, &actorID, &event.Actor.Name, &event.Target.Type, &event.Target.ID, &event.Target.Name,
		&event.CorrelationID, &sourceIP, &event.HTTPMethod, &event.RoutePattern, &event.StatusCode,
		&event.DurationMS, &event.ErrorCode, &event.Message, &metadata)
	if err != nil {
		return systemevent.Event{}, fmt.Errorf("scan system event: %w", err)
	}
	if actorID != nil {
		event.Actor.ID = *actorID
	}
	if sourceIP != nil {
		event.SourceIP = *sourceIP
	}
	if err := json.Unmarshal(metadata, &event.Metadata); err != nil {
		return systemevent.Event{}, fmt.Errorf("decode system event metadata: %w", err)
	}
	return event, nil
}

func (r *SystemEventRepository) DeleteBeforeBatch(ctx context.Context, before time.Time, batchSize int) (int64, error) {
	if before.IsZero() || batchSize < 1 || batchSize > 10000 {
		return 0, systemevent.ErrInvalidEvent
	}
	tag, err := r.pool.Exec(ctx, `WITH candidates AS (
		SELECT id FROM system_events WHERE occurred_at < $1 ORDER BY occurred_at, id LIMIT $2
	) DELETE FROM system_events WHERE id IN (SELECT id FROM candidates)`, before.UTC(), batchSize)
	if err != nil {
		return 0, fmt.Errorf("delete expired system events: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *SystemEventRepository) encodeCursor(cursor systemEventCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, r.cursorSecret)
	_, _ = mac.Write(payload)
	value := append(payload, mac.Sum(nil)...)
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func (r *SystemEventRepository) decodeCursor(value string) (systemEventCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) <= sha256.Size {
		return systemEventCursor{}, systemevent.ErrInvalidCursor
	}
	payload, signature := decoded[:len(decoded)-sha256.Size], decoded[len(decoded)-sha256.Size:]
	mac := hmac.New(sha256.New, r.cursorSecret)
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return systemEventCursor{}, systemevent.ErrInvalidCursor
	}
	var cursor systemEventCursor
	if err := json.Unmarshal(payload, &cursor); err != nil || cursor.OccurredAt.IsZero() || cursor.ID < 1 {
		return systemEventCursor{}, systemevent.ErrInvalidCursor
	}
	return cursor, nil
}

func escapeLike(value string) string {
	return strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(value)
}
