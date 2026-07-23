package oauthstatecleanup

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

type fakeLease struct {
	mu            sync.Mutex
	eligible      int64
	countErr      error
	deleteResults []deleteResult
	deleteCalls   int
	closeCalls    int
	closeErr      error
}

type deleteResult struct {
	count int64
	err   error
}

func (l *fakeLease) CountEligible(context.Context, time.Time) (int64, error) {
	return l.eligible, l.countErr
}

func (l *fakeLease) DeleteEligibleBatch(ctx context.Context, _ time.Time, _ int) (int64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	l.deleteCalls++
	if len(l.deleteResults) == 0 {
		return 0, nil
	}
	result := l.deleteResults[0]
	l.deleteResults = l.deleteResults[1:]
	return result.count, result.err
}

func (l *fakeLease) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closeCalls++
	return l.closeErr
}

type fakeRepository struct {
	lease    Lease
	acquired bool
	err      error
}

func (r fakeRepository) TryAcquire(context.Context) (Lease, bool, error) {
	return r.lease, r.acquired, r.err
}

type fakeEvents struct {
	events []systemevent.Event
	err    error
}

func (r *fakeEvents) Insert(_ context.Context, event systemevent.Event) error {
	r.events = append(r.events, event)
	return r.err
}

func TestRunDryRunCountsWithoutDeletingOrRecordingEvent(t *testing.T) {
	lease := &fakeLease{eligible: 7}
	events := &fakeEvents{}
	cutoff := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	result, err := Run(context.Background(), fakeRepository{lease: lease, acquired: true}, events, Options{
		Cutoff: cutoff, BatchSize: 250, DryRun: true,
	}, func() time.Time { return cutoff.Add(time.Hour) })
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Status != StatusDryRun || result.EligibleCount != 7 || result.DeletedCount != 0 || result.BatchCount != 0 {
		t.Fatalf("result = %+v", result)
	}
	if lease.deleteCalls != 0 || lease.closeCalls != 1 || len(events.events) != 0 {
		t.Fatalf("delete=%d close=%d events=%d", lease.deleteCalls, lease.closeCalls, len(events.events))
	}
}

func TestRunDeletesBoundedBatchesAndRecordsStableEvent(t *testing.T) {
	lease := &fakeLease{eligible: 5, deleteResults: []deleteResult{{count: 2}, {count: 2}, {count: 1}}}
	events := &fakeEvents{}
	cutoff := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	started := cutoff.Add(time.Minute)
	finished := started.Add(1500 * time.Millisecond)
	times := []time.Time{started, finished}
	result, err := Run(context.Background(), fakeRepository{lease: lease, acquired: true}, events, Options{
		Cutoff: cutoff, BatchSize: 2,
	}, func() time.Time {
		value := times[0]
		times = times[1:]
		return value
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Status != StatusCompleted || result.EligibleCount != 5 || result.DeletedCount != 5 || result.BatchCount != 3 {
		t.Fatalf("result = %+v", result)
	}
	if lease.deleteCalls != 3 || lease.closeCalls != 1 || len(events.events) != 1 {
		t.Fatalf("delete=%d close=%d events=%d", lease.deleteCalls, lease.closeCalls, len(events.events))
	}
	event := events.events[0]
	if event.Action != systemevent.ActionOAuthStateCleanupCompleted || event.Category != systemevent.CategoryOAuth ||
		event.Severity != systemevent.SeverityInfo || event.Outcome != systemevent.OutcomeSuccess ||
		event.Target.Type != "oauth_state_collection" || event.Actor.Type != systemevent.ActorSystem ||
		event.Actor.Name != "oauth_state_cleanup" || event.DurationMS != 1500 {
		t.Fatalf("event = %+v", event)
	}
	if event.Metadata["cutoff"] != cutoff.Format(time.RFC3339) || event.Metadata["deleted_count"] != int64(5) ||
		event.Metadata["batch_count"] != 3 || event.Metadata["batch_size"] != 2 {
		t.Fatalf("event metadata = %#v", event.Metadata)
	}
	if err := systemevent.ValidateEvent(event); err != nil {
		t.Fatalf("ValidateEvent returned error: %v", err)
	}
}

func TestRunIsIdempotentWhenNoRowsRemain(t *testing.T) {
	lease := &fakeLease{}
	events := &fakeEvents{}
	result, err := Run(context.Background(), fakeRepository{lease: lease, acquired: true}, events, Options{
		Cutoff: time.Now(), BatchSize: 100,
	}, time.Now)
	if err != nil || result.DeletedCount != 0 || result.BatchCount != 0 || lease.deleteCalls != 1 || len(events.events) != 1 {
		t.Fatalf("result=%+v err=%v delete=%d events=%d", result, err, lease.deleteCalls, len(events.events))
	}
}

func TestRunReportsConcurrentWorkerContentionWithoutMutation(t *testing.T) {
	now := time.Now()
	result, err := Run(context.Background(), fakeRepository{acquired: false}, nil, Options{
		Cutoff: now, BatchSize: 100,
	}, func() time.Time { return now })
	if err != nil || result.Status != StatusContended || result.DeletedCount != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestRunRejectsFutureCutoffBeforeAcquiringLease(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	repo := fakeRepository{lease: &fakeLease{}, acquired: true}
	_, err := Run(context.Background(), repo, nil, Options{Cutoff: now.Add(time.Second), BatchSize: 100, DryRun: true}, func() time.Time { return now })
	if !errors.Is(err, ErrCleanupFailed) {
		t.Fatalf("error=%v, want future cutoff rejection", err)
	}
}

func TestRunHonorsCancellationAndSanitizesRepositoryFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	lease := &fakeLease{deleteResults: []deleteResult{{err: errors.New("postgres-password-canary")}}}
	_, err := Run(ctx, fakeRepository{lease: lease, acquired: true}, &fakeEvents{}, Options{
		Cutoff: time.Now(), BatchSize: 100,
	}, time.Now)
	if !errors.Is(err, context.Canceled) || lease.closeCalls != 1 {
		t.Fatalf("error=%v close=%d", err, lease.closeCalls)
	}

	_, err = Run(context.Background(), fakeRepository{err: errors.New("database-dsn-canary")}, nil, Options{
		Cutoff: time.Now(), BatchSize: 100,
	}, time.Now)
	if !errors.Is(err, ErrCleanupFailed) || err.Error() != ErrCleanupFailed.Error() {
		t.Fatalf("error=%v, want stable sanitized error", err)
	}
}

func TestRunFailsWhenEventOrLockReleaseCannotBeRecorded(t *testing.T) {
	for name, tt := range map[string]struct {
		lease  *fakeLease
		events *fakeEvents
	}{
		"event":  {lease: &fakeLease{}, events: &fakeEvents{err: errors.New("event-store-canary")}},
		"unlock": {lease: &fakeLease{closeErr: errors.New("unlock-canary")}, events: &fakeEvents{}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Run(context.Background(), fakeRepository{lease: tt.lease, acquired: true}, tt.events, Options{
				Cutoff: time.Now(), BatchSize: 100,
			}, time.Now)
			if !errors.Is(err, ErrCleanupFailed) || err.Error() != ErrCleanupFailed.Error() {
				t.Fatalf("error=%v, want stable failure", err)
			}
		})
	}
}
