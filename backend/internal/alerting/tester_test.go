package alerting

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

type fakeActionTestService struct {
	action         ResolvedAction
	beginErr       error
	finalizeCtxErr error
	saved          ActionTestResult
	finalizeCtx    context.Context
	finalizeErr    error
	deadline       time.Time
	hasDeadline    bool
	expected       time.Time
	attempt        ActionTestAttempt
}

func (service *fakeActionTestService) BeginActionTest(_ context.Context, _ int64, expected time.Time) (ActionTestAttempt, error) {
	service.expected = expected
	if !service.action.UpdatedAt.Equal(expected) {
		return ActionTestAttempt{}, ErrConflict
	}
	if service.beginErr != nil {
		return ActionTestAttempt{}, service.beginErr
	}
	service.attempt = ActionTestAttempt{
		Action: service.action, AttemptToken: "0123456789abcdef0123456789abcdef",
		StartedAt: expected.Add(time.Minute),
	}
	return service.attempt, nil
}

func (service *fakeActionTestService) FinalizeActionTest(ctx context.Context, attempt ActionTestAttempt, result ActionTestResult) error {
	service.finalizeCtx = ctx
	service.finalizeCtxErr = ctx.Err()
	service.deadline, service.hasDeadline = ctx.Deadline()
	service.attempt = attempt
	service.saved = result
	return service.finalizeErr
}

type countingTestAdapter struct {
	mu      sync.Mutex
	count   int
	result  DeliveryAttempt
	started chan struct{}
	release chan struct{}
	cancel  context.CancelFunc
}

func (adapter *countingTestAdapter) Deliver(context.Context, ResolvedAction, Notification) DeliveryAttempt {
	adapter.mu.Lock()
	adapter.count++
	if adapter.started != nil && adapter.count == 1 {
		close(adapter.started)
	}
	adapter.mu.Unlock()
	if adapter.release != nil {
		<-adapter.release
	}
	if adapter.cancel != nil {
		adapter.cancel()
	}
	return adapter.result
}

func (adapter *countingTestAdapter) calls() int {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	return adapter.count
}

func TestActionTesterAllowsDisabledActionAndPersistsSanitizedSingleAttempt(t *testing.T) {
	revision := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	now := revision.Add(time.Minute)
	service := &fakeActionTestService{action: ResolvedAction{
		ID: 7, Name: "disabled webhook", Kind: ActionKindGenericWebhook, Enabled: false,
		Destination: "https://secret.example.test/hook?token=canary", UpdatedAt: revision,
	}}
	adapter := &countingTestAdapter{result: DeliveryAttempt{
		StatusCode: 503, ErrorCode: "alert_delivery_http_status", Retryable: true,
	}}
	tester := NewActionTester(service, adapter)
	times := []time.Time{now, now.Add(125 * time.Millisecond)}
	tester.now = func() time.Time {
		value := times[0]
		if len(times) > 1 {
			times = times[1:]
		}
		return value
	}
	result, err := tester.TestAction(context.Background(), 7, revision)
	if err != nil {
		t.Fatalf("TestAction error = %v", err)
	}
	if adapter.calls() != 1 || result.Status != ActionTestStatusFailed || result.HTTPStatus == nil || *result.HTTPStatus != 503 || result.LatencyMS != 125 || !result.Retryable {
		t.Fatalf("result = %+v calls=%d", result, adapter.calls())
	}
	intent, ok := systemevent.IntentFromContext(service.finalizeCtx)
	if !ok || intent.Action != systemevent.ActionAlertDeliveryTested || intent.Outcome != systemevent.OutcomeFailure || intent.ErrorCode != "alert_delivery_http_status" {
		t.Fatalf("test intent = %+v, ok=%v", intent, ok)
	}
	if _, ok := intent.Metadata["status_code"]; !ok || len(intent.Metadata) != 3 {
		t.Fatalf("test metadata = %#v", intent.Metadata)
	}
	if service.expected != revision || service.attempt.AttemptToken == "" {
		t.Fatalf("test admission = %+v expected=%s", service.attempt, service.expected)
	}
}

func TestActionTesterRejectsStaleAndPersistedCooldownBeforeDelivery(t *testing.T) {
	revision := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	service := &fakeActionTestService{action: ResolvedAction{
		ID: 7, Kind: ActionKindGenericWebhook, Destination: "https://example.test/hook",
		UpdatedAt: revision,
	}}
	adapter := &countingTestAdapter{}
	tester := NewActionTester(service, adapter)
	if _, err := tester.TestAction(context.Background(), 7, revision.Add(-time.Second)); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale test error = %v, want ErrConflict", err)
	}
	service.beginErr = &RateLimitError{RetryAfter: 20 * time.Second}
	_, err := tester.TestAction(context.Background(), 7, revision)
	var rateLimit *RateLimitError
	if !errors.As(err, &rateLimit) || rateLimit.RetryAfter != 20*time.Second {
		t.Fatalf("cooldown error = %#v, want 20s rate limit", err)
	}
	if adapter.calls() != 0 {
		t.Fatalf("delivery calls = %d, want 0", adapter.calls())
	}
}

func TestActionTesterFinalizesAfterRequestCancellation(t *testing.T) {
	revision := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	service := &fakeActionTestService{action: ResolvedAction{
		ID: 7, Name: "webhook", Kind: ActionKindGenericWebhook,
		Destination: "https://example.test/hook", UpdatedAt: revision,
	}}
	requestCtx, cancel := context.WithCancel(context.Background())
	adapter := &countingTestAdapter{result: DeliveryAttempt{Success: true}, cancel: cancel}
	tester := NewActionTester(service, adapter)
	tester.now = func() time.Time { return revision.Add(time.Minute) }
	if _, err := tester.TestAction(requestCtx, 7, revision); err != nil {
		t.Fatalf("TestAction error = %v", err)
	}
	if requestCtx.Err() != context.Canceled || service.finalizeCtx == nil || service.finalizeCtxErr != nil {
		t.Fatalf("request err=%v finalize err at call=%v", requestCtx.Err(), service.finalizeCtxErr)
	}
	if !service.hasDeadline || time.Until(service.deadline) > actionTestFinalizeTimeout {
		t.Fatalf("finalize deadline=%s ok=%v", service.deadline, service.hasDeadline)
	}
}

func TestActionTesterAllowsOnlyOneConcurrentTestGlobally(t *testing.T) {
	revision := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	service := &fakeActionTestService{action: ResolvedAction{
		ID: 7, Kind: ActionKindGenericWebhook, Destination: "https://example.test/hook", UpdatedAt: revision,
	}}
	adapter := &countingTestAdapter{
		result: DeliveryAttempt{Success: true}, started: make(chan struct{}), release: make(chan struct{}),
	}
	tester := NewActionTester(service, adapter)
	tester.now = func() time.Time { return revision.Add(time.Minute) }
	firstDone := make(chan error, 1)
	go func() {
		_, err := tester.TestAction(context.Background(), 7, revision)
		firstDone <- err
	}()
	<-adapter.started
	if _, err := tester.TestAction(context.Background(), 8, revision); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("concurrent test error = %v, want ErrRateLimited", err)
	}
	close(adapter.release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first test error = %v", err)
	}
}
