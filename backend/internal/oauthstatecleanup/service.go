package oauthstatecleanup

import (
	"context"
	"errors"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

var ErrCleanupFailed = errors.New("oauth state cleanup failed")

const (
	StatusDryRun    = "dry_run"
	StatusCompleted = "completed"
	StatusContended = "contended"

	MaxBatchSize = 10000
)

type Options struct {
	Cutoff    time.Time
	BatchSize int
	DryRun    bool
}

type Result struct {
	Status        string    `json:"status"`
	DryRun        bool      `json:"dryRun"`
	Cutoff        time.Time `json:"cutoff"`
	BatchSize     int       `json:"batchSize"`
	EligibleCount int64     `json:"eligibleCount"`
	DeletedCount  int64     `json:"deletedCount"`
	BatchCount    int       `json:"batchCount"`
}

type Lease interface {
	CountEligible(ctx context.Context, cutoff time.Time) (int64, error)
	DeleteEligibleBatch(ctx context.Context, cutoff time.Time, batchSize int) (int64, error)
	Close() error
}

type Repository interface {
	TryAcquire(ctx context.Context) (Lease, bool, error)
}

type EventRecorder interface {
	Insert(ctx context.Context, event systemevent.Event) error
}

func Run(ctx context.Context, repo Repository, events EventRecorder, options Options, now func() time.Time) (result Result, err error) {
	if repo == nil || now == nil || options.Cutoff.IsZero() || options.BatchSize < 1 || options.BatchSize > MaxBatchSize {
		return Result{}, ErrCleanupFailed
	}
	options.Cutoff = options.Cutoff.UTC()
	started := now().UTC()
	if options.Cutoff.After(started) {
		return Result{}, ErrCleanupFailed
	}
	lease, acquired, err := repo.TryAcquire(ctx)
	if err != nil {
		return Result{}, cleanupError(ctx)
	}
	if !acquired {
		return Result{Status: StatusContended, DryRun: options.DryRun, Cutoff: options.Cutoff, BatchSize: options.BatchSize}, nil
	}
	leaseClosed := false
	defer func() {
		if leaseClosed {
			return
		}
		if closeErr := lease.Close(); err == nil && closeErr != nil {
			result = Result{}
			err = ErrCleanupFailed
		}
	}()

	eligible, err := lease.CountEligible(ctx, options.Cutoff)
	if err != nil {
		return Result{}, cleanupError(ctx)
	}
	result = Result{
		Status: StatusDryRun, DryRun: options.DryRun, Cutoff: options.Cutoff,
		BatchSize: options.BatchSize, EligibleCount: eligible,
	}
	if options.DryRun {
		return result, nil
	}

	result.Status = StatusCompleted
	for {
		deleted, err := lease.DeleteEligibleBatch(ctx, options.Cutoff, options.BatchSize)
		if err != nil {
			return result, cleanupError(ctx)
		}
		if deleted > 0 {
			result.DeletedCount += deleted
			result.BatchCount++
		}
		if deleted < int64(options.BatchSize) {
			break
		}
	}
	if events == nil {
		return result, ErrCleanupFailed
	}
	if err := lease.Close(); err != nil {
		return result, ErrCleanupFailed
	}
	leaseClosed = true
	finished := now().UTC()
	metadata, err := systemevent.SafeMetadata(map[string]any{
		"cutoff": options.Cutoff.Format(time.RFC3339), "deleted_count": result.DeletedCount,
		"batch_count": result.BatchCount, "batch_size": options.BatchSize,
	}, "cutoff", "deleted_count", "batch_count", "batch_size")
	if err != nil {
		return result, ErrCleanupFailed
	}
	eventCtx := systemevent.WithRequestContext(ctx, systemevent.RequestContext{
		CorrelationID: systemevent.NewCorrelationID(),
		Actor:         systemevent.Actor{Type: systemevent.ActorSystem, Name: "oauth_state_cleanup"},
	})
	event := systemevent.BuildEvent(eventCtx, systemevent.EventIntent{
		Category: systemevent.CategoryOAuth, Severity: systemevent.SeverityInfo,
		Action: systemevent.ActionOAuthStateCleanupCompleted, Outcome: systemevent.OutcomeSuccess,
		Target:  systemevent.Target{Type: "oauth_state_collection"},
		Message: "Expired OAuth state cleanup completed", Metadata: metadata,
	}, systemevent.Target{}, finished, finished.Sub(started))
	if err := events.Insert(ctx, event); err != nil {
		return result, cleanupError(ctx)
	}
	return result, nil
}

func cleanupError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrCleanupFailed
}
