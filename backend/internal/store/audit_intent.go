package store

import (
	"context"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/systemevent"
	"github.com/jackc/pgx/v5"
)

func insertIntentSystemEvent(ctx context.Context, tx pgx.Tx, target systemevent.Target, metadata map[string]any) error {
	intent, ok := systemevent.IntentFromContext(ctx)
	if !ok {
		return nil
	}
	if metadata != nil {
		merged := make(map[string]any, len(intent.Metadata)+len(metadata))
		for key, value := range intent.Metadata {
			merged[key] = value
		}
		for key, value := range metadata {
			merged[key] = value
		}
		intent.Metadata = merged
	}
	duration := time.Duration(0)
	if !intent.StartedAt.IsZero() {
		duration = time.Since(intent.StartedAt)
	}
	event := systemevent.BuildEvent(ctx, intent, target, time.Now().UTC(), duration)
	return InsertSystemEventTx(ctx, tx, event)
}
