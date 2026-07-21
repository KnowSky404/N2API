package alerting

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

type recordingDeliveryAdapter struct {
	mu            sync.Mutex
	notifications []Notification
	actions       []ResolvedAction
	results       []DeliveryAttempt
	started       chan struct{}
	block         bool
}

func (adapter *recordingDeliveryAdapter) Deliver(ctx context.Context, action ResolvedAction, notification Notification) DeliveryAttempt {
	adapter.mu.Lock()
	adapter.notifications = append(adapter.notifications, notification)
	adapter.actions = append(adapter.actions, action)
	index := len(adapter.notifications) - 1
	var result DeliveryAttempt
	if index < len(adapter.results) {
		result = adapter.results[index]
	} else {
		result = DeliveryAttempt{Success: true}
	}
	if adapter.started != nil && index == 0 {
		close(adapter.started)
	}
	block := adapter.block
	adapter.mu.Unlock()
	if block {
		<-ctx.Done()
		return DeliveryAttempt{ErrorCode: "alert_delivery_canceled"}
	}
	return result
}

func (adapter *recordingDeliveryAdapter) actionSnapshot() []ResolvedAction {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	return append([]ResolvedAction(nil), adapter.actions...)
}

func (adapter *recordingDeliveryAdapter) snapshot() []Notification {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	return append([]Notification(nil), adapter.notifications...)
}

type memoryDeliveryRecorder struct {
	mu     sync.Mutex
	events []systemevent.Event
	err    error
}

type orderedBlockingAdapter struct {
	mu            sync.Mutex
	calls         int
	firstStarted  chan struct{}
	secondStarted chan struct{}
	releaseFirst  chan struct{}
}

func (adapter *orderedBlockingAdapter) Deliver(context.Context, ResolvedAction, Notification) DeliveryAttempt {
	adapter.mu.Lock()
	adapter.calls++
	call := adapter.calls
	adapter.mu.Unlock()
	if call == 1 {
		close(adapter.firstStarted)
		<-adapter.releaseFirst
	} else if call == 2 {
		close(adapter.secondStarted)
	}
	return DeliveryAttempt{Success: true}
}

func (recorder *memoryDeliveryRecorder) Insert(_ context.Context, event systemevent.Event) error {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.events = append(recorder.events, event)
	return recorder.err
}

func (recorder *memoryDeliveryRecorder) snapshot() []systemevent.Event {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return append([]systemevent.Event(nil), recorder.events...)
}

func TestDispatcherEvaluatesInOrderAndDeliversRecovery(t *testing.T) {
	service, rule := dispatcherService(t, 1)
	adapter := &recordingDeliveryAdapter{}
	dispatcher := NewDispatcher(DispatcherConfig{
		Enabled: true, Service: service, Adapter: adapter,
		EventQueueCapacity: 4, DeliveryQueueCapacity: 4, WorkerCount: 1,
		InitialBackoff: time.Millisecond, MaxBackoff: 2 * time.Millisecond,
	})
	dispatcher.Start()

	trigger := triggerEvent()
	trigger.ID = 11
	recovery := trigger
	recovery.ID = 12
	recovery.Action = systemevent.ActionOAuthRefreshAutomaticSucceeded
	recovery.Outcome = systemevent.OutcomeSuccess
	recovery.Severity = systemevent.SeverityInfo
	if !dispatcher.tryEnqueue(trigger) || !dispatcher.tryEnqueue(recovery) {
		t.Fatal("dispatcher rejected ordered events")
	}
	waitFor(t, time.Second, func() bool { return len(adapter.snapshot()) == 2 })
	notifications := adapter.snapshot()
	if notifications[0].Decision != DecisionNotify || notifications[1].Decision != DecisionRecover {
		t.Fatalf("delivery decisions = %q, %q", notifications[0].Decision, notifications[1].Decision)
	}
	if notifications[0].RuleID != rule.ID || notifications[0].DeliveryID == notifications[1].DeliveryID {
		t.Fatalf("notifications = %+v", notifications)
	}
	shutdownDispatcher(t, dispatcher)
	status := dispatcher.AlertDeliveryStatus()
	if status.Running || status.EnqueuedCount != 2 || status.DeliveredCount != 2 || status.FailedCount != 0 {
		t.Fatalf("delivery status = %+v", status)
	}
}

func TestDispatcherPreservesOrderWithinRuleDeduplicationStreamAcrossWorkers(t *testing.T) {
	service, _ := dispatcherService(t, 1)
	adapter := &orderedBlockingAdapter{
		firstStarted: make(chan struct{}), secondStarted: make(chan struct{}), releaseFirst: make(chan struct{}),
	}
	dispatcher := NewDispatcher(DispatcherConfig{
		Enabled: true, Service: service, Adapter: adapter,
		EventQueueCapacity: 4, DeliveryQueueCapacity: 4, WorkerCount: 2,
	})
	dispatcher.Start()
	trigger := triggerEvent()
	trigger.ID = 13
	recovery := trigger
	recovery.ID = 14
	recovery.Action = systemevent.ActionOAuthRefreshAutomaticSucceeded
	recovery.Outcome = systemevent.OutcomeSuccess
	recovery.Severity = systemevent.SeverityInfo
	if !dispatcher.tryEnqueue(trigger) || !dispatcher.tryEnqueue(recovery) {
		t.Fatal("dispatcher rejected ordered events")
	}
	select {
	case <-adapter.firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first delivery did not start")
	}
	select {
	case <-adapter.secondStarted:
		t.Fatal("recovery delivery overtook blocked firing delivery")
	case <-time.After(25 * time.Millisecond):
	}
	close(adapter.releaseFirst)
	select {
	case <-adapter.secondStarted:
	case <-time.After(time.Second):
		t.Fatal("recovery delivery did not start after firing completed")
	}
	waitFor(t, time.Second, func() bool { return dispatcher.AlertDeliveryStatus().DeliveredCount == 2 })
	shutdownDispatcher(t, dispatcher)
}

type interleavedRuleService struct {
	mu            sync.RWMutex
	listed        Rule
	evaluated     Rule
	resolved      ResolvedAction
	evaluationErr error
	getRuleErr    error
	resolveErr    error
	evaluations   atomic.Int64
}

func (service *interleavedRuleService) ListRules(context.Context) ([]Rule, error) {
	return []Rule{service.listed}, nil
}

func (service *interleavedRuleService) GetRule(context.Context, int64) (Rule, error) {
	if service.getRuleErr != nil {
		return Rule{}, service.getRuleErr
	}
	return service.evaluated, nil
}

func (service *interleavedRuleService) ResolveActionForDelivery(context.Context, int64) (ResolvedAction, error) {
	service.mu.RLock()
	defer service.mu.RUnlock()
	if service.resolveErr != nil {
		return ResolvedAction{}, service.resolveErr
	}
	return service.resolved, nil
}

func (service *interleavedRuleService) setResolved(action ResolvedAction) {
	service.mu.Lock()
	defer service.mu.Unlock()
	service.resolved = action
}

func (service *interleavedRuleService) EvaluateRuleEventForDelivery(context.Context, int64, systemevent.Event, time.Time) (Evaluation, error) {
	service.evaluations.Add(1)
	if service.evaluationErr != nil {
		return Evaluation{}, service.evaluationErr
	}
	return Evaluation{Rule: service.evaluated, ActionEnabled: true, ActionUpdatedAt: service.resolved.UpdatedAt, Decision: DecisionNotify}, nil
}

func TestDispatcherDropsJobWhenActionRevisionChangesBeforeDelivery(t *testing.T) {
	oldTime := time.Date(2026, time.July, 21, 10, 0, 0, 0, time.UTC)
	newTime := oldTime.Add(time.Minute)
	rule := validRule()
	rule.ID, rule.ActionID, rule.UpdatedAt = 7, 11, oldTime
	service := &interleavedRuleService{
		listed: rule, evaluated: rule,
		resolved: ResolvedAction{
			ID: 11, Kind: ActionKindGenericWebhook, Enabled: true,
			Destination: "https://example.test/new", UpdatedAt: newTime,
		},
	}
	adapter := &recordingDeliveryAdapter{}
	dispatcher := NewDispatcher(DispatcherConfig{Enabled: true, Service: service, Adapter: adapter})
	dispatcher.deliver(context.Background(), deliveryJob{
		rule: rule, actionUpdatedAt: oldTime, decision: DecisionNotify, event: triggerEvent(),
	})
	if len(adapter.snapshot()) != 0 {
		t.Fatal("stale action revision was delivered")
	}
}

func TestDispatcherUsesRuleVersionLockedDuringEvaluation(t *testing.T) {
	oldTime := time.Date(2026, time.July, 21, 10, 0, 0, 0, time.UTC)
	newTime := oldTime.Add(time.Minute)
	oldRule := validRule()
	oldRule.ID, oldRule.ActionID, oldRule.UpdatedAt = 7, 11, oldTime
	currentRule := oldRule
	currentRule.ActionID, currentRule.UpdatedAt = 12, newTime
	service := &interleavedRuleService{
		listed: oldRule, evaluated: currentRule,
		resolved: ResolvedAction{ID: 12, Kind: ActionKindGenericWebhook, Enabled: true, Destination: "https://example.test/current"},
	}
	adapter := &recordingDeliveryAdapter{}
	dispatcher := NewDispatcher(DispatcherConfig{
		Enabled: true, Service: service, Adapter: adapter, EventQueueCapacity: 1,
		DeliveryQueueCapacity: 1, WorkerCount: 1,
	})
	dispatcher.Start()
	event := triggerEvent()
	event.ID = 15
	if !dispatcher.tryEnqueue(event) {
		t.Fatal("dispatcher rejected event")
	}
	waitFor(t, time.Second, func() bool { return len(adapter.actionSnapshot()) == 1 })
	if got := adapter.actionSnapshot()[0].ID; got != currentRule.ActionID {
		t.Fatalf("delivered action ID = %d, want atomically evaluated action %d", got, currentRule.ActionID)
	}
	if got := adapter.snapshot()[0].RuleID; got != currentRule.ID {
		t.Fatalf("delivered rule ID = %d, want %d", got, currentRule.ID)
	}
	shutdownDispatcher(t, dispatcher)
}

func TestDispatcherSilentlySkipsRuleDeletedAfterListing(t *testing.T) {
	rule := validRule()
	rule.ID, rule.ActionID = 7, 11
	service := &interleavedRuleService{listed: rule, evaluationErr: ErrNotFound}
	recorder := &memoryDeliveryRecorder{}
	dispatcher := NewDispatcher(DispatcherConfig{
		Enabled: true, Service: service, Recorder: recorder, Adapter: &recordingDeliveryAdapter{},
		EventQueueCapacity: 1, DeliveryQueueCapacity: 1, WorkerCount: 1,
	})
	dispatcher.Start()
	event := triggerEvent()
	event.ID = 16
	if !dispatcher.tryEnqueue(event) {
		t.Fatal("dispatcher rejected event")
	}
	waitFor(t, time.Second, func() bool { return service.evaluations.Load() == 1 })
	shutdownDispatcher(t, dispatcher)
	if events := recorder.snapshot(); len(events) != 0 {
		t.Fatalf("deleted rule emitted failure events: %+v", events)
	}
	if status := dispatcher.AlertDeliveryStatus(); status.FailedCount != 0 || status.LastErrorCode != "" {
		t.Fatalf("deleted rule polluted delivery status: %+v", status)
	}
}

func TestDispatcherDistinguishesDeliveryConfigDeletionFromRepositoryFailure(t *testing.T) {
	rule := validRule()
	rule.ID, rule.ActionID, rule.UpdatedAt = 7, 11, time.Now().UTC()
	for _, test := range []struct {
		name       string
		err        error
		wantEvents int
	}{
		{name: "deleted", err: ErrNotFound, wantEvents: 0},
		{name: "repository failure", err: ErrRepository, wantEvents: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := &memoryDeliveryRecorder{}
			service := &interleavedRuleService{evaluated: rule, getRuleErr: test.err}
			dispatcher := NewDispatcher(DispatcherConfig{Enabled: true, Service: service, Recorder: recorder})
			dispatcher.deliver(context.Background(), deliveryJob{rule: rule, decision: DecisionNotify, event: triggerEvent()})
			if got := len(recorder.snapshot()); got != test.wantEvents {
				t.Fatalf("failure events = %d, want %d", got, test.wantEvents)
			}
		})
	}
}

func TestDispatcherRetriesBoundedlyAndRecordsSanitizedFailure(t *testing.T) {
	service, _ := dispatcherService(t, 1)
	recorder := &memoryDeliveryRecorder{}
	adapter := &recordingDeliveryAdapter{results: []DeliveryAttempt{
		{Retryable: true, StatusCode: 503, ErrorCode: "alert_delivery_http_status"},
		{Retryable: true, StatusCode: 503, ErrorCode: "alert_delivery_http_status"},
		{StatusCode: 503, ErrorCode: "alert_delivery_http_status"},
	}}
	dispatcher := NewDispatcher(DispatcherConfig{
		Enabled: true, Service: service, Recorder: recorder, Adapter: adapter,
		EventQueueCapacity: 2, DeliveryQueueCapacity: 2, WorkerCount: 1, MaxAttempts: 3,
		InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond,
	})
	dispatcher.Start()
	event := triggerEvent()
	event.ID = 21
	if !dispatcher.tryEnqueue(event) {
		t.Fatal("dispatcher rejected event")
	}
	waitFor(t, time.Second, func() bool {
		return dispatcher.AlertDeliveryStatus().FailedCount == 1 && len(recorder.snapshot()) == 1
	})
	if got := len(adapter.snapshot()); got != 3 {
		t.Fatalf("delivery attempts = %d, want 3", got)
	}
	events := recorder.snapshot()
	if len(events) != 1 || events[0].Action != systemevent.ActionAlertDeliveryFailed || events[0].ErrorCode != "alert_delivery_http_status" {
		t.Fatalf("failure events = %+v", events)
	}
	if _, exists := events[0].Metadata["destination"]; exists {
		t.Fatalf("failure event leaked destination: %+v", events[0].Metadata)
	}
	status := dispatcher.AlertDeliveryStatus()
	if status.RetriedCount != 2 || status.LastErrorCode != "alert_delivery_http_status" {
		t.Fatalf("delivery status = %+v", status)
	}
	shutdownDispatcher(t, dispatcher)
}

func TestDispatcherStopsRetryWhenActionChangesDuringBackoff(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(ResolvedAction) ResolvedAction
	}{
		{name: "disabled", mutate: func(action ResolvedAction) ResolvedAction { action.Enabled = false; return action }},
		{name: "revision changed", mutate: func(action ResolvedAction) ResolvedAction {
			action.UpdatedAt = action.UpdatedAt.Add(time.Second)
			action.Destination = "https://example.test/reconfigured"
			return action
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			revision := time.Date(2026, time.July, 21, 10, 0, 0, 0, time.UTC)
			rule := validRule()
			rule.ID, rule.ActionID, rule.UpdatedAt = 7, 11, revision
			action := ResolvedAction{
				ID: 11, Kind: ActionKindGenericWebhook, Enabled: true,
				Destination: "https://example.test/original", UpdatedAt: revision,
			}
			service := &interleavedRuleService{evaluated: rule, resolved: action}
			adapter := &recordingDeliveryAdapter{results: []DeliveryAttempt{
				{Retryable: true, StatusCode: 503, ErrorCode: "alert_delivery_http_status"},
				{Success: true},
			}}
			dispatcher := NewDispatcher(DispatcherConfig{
				Enabled: true, Service: service, Adapter: adapter, MaxAttempts: 2,
				InitialBackoff: 100 * time.Millisecond, MaxBackoff: 100 * time.Millisecond,
			})
			done := make(chan struct{})
			go func() {
				dispatcher.deliver(context.Background(), deliveryJob{
					rule: rule, actionUpdatedAt: revision, decision: DecisionNotify, event: triggerEvent(),
				})
				close(done)
			}()
			waitFor(t, time.Second, func() bool { return len(adapter.snapshot()) == 1 })
			service.setResolved(test.mutate(action))
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("delivery did not stop after action changed")
			}
			if got := len(adapter.snapshot()); got != 1 {
				t.Fatalf("delivery attempts = %d, want 1", got)
			}
		})
	}
}

func TestDispatcherQueueSaturationIsNonBlockingAndAggregatesOverflow(t *testing.T) {
	recorder := &memoryDeliveryRecorder{}
	dispatcher := NewDispatcher(DispatcherConfig{
		Enabled: true, Service: &Service{}, Recorder: recorder, EventQueueCapacity: 1,
		DeliveryQueueCapacity: 1, WorkerCount: 1,
	})
	dispatcher.accepting.Store(true)
	first := triggerEvent()
	first.ID = 31
	second := first
	second.ID = 32
	if !dispatcher.tryEnqueue(first) {
		t.Fatal("first enqueue failed")
	}
	started := time.Now()
	if dispatcher.tryEnqueue(second) {
		t.Fatal("saturated queue accepted second event")
	}
	if elapsed := time.Since(started); elapsed > 50*time.Millisecond {
		t.Fatalf("saturated enqueue blocked for %s", elapsed)
	}
	dispatcher.reportOverflow()
	events := recorder.snapshot()
	if len(events) != 1 || events[0].Action != systemevent.ActionAlertDeliveryQueueOverflow || events[0].Metadata["dropped_count"] != uint64(1) {
		t.Fatalf("overflow events = %+v", events)
	}
	dispatcher.reportOverflow()
	if got := len(recorder.snapshot()); got != 1 {
		t.Fatalf("overflow event count = %d, want one aggregate", got)
	}
	status := dispatcher.AlertDeliveryStatus()
	if status.DroppedCount != 1 || status.QueueDepth != 1 {
		t.Fatalf("overflow status = %+v", status)
	}
}

func TestDispatcherRejectsInternalEventsBeforeQueueing(t *testing.T) {
	dispatcher := NewDispatcher(DispatcherConfig{Enabled: true, Service: &Service{}, EventQueueCapacity: 1})
	dispatcher.accepting.Store(true)
	for _, action := range []systemevent.Action{systemevent.ActionAlertDeliveryFailed, systemevent.ActionAlertDeliveryQueueOverflow} {
		event := triggerEvent()
		event.Action = action
		if dispatcher.tryEnqueue(event) {
			t.Fatalf("internal action %q was queued", action)
		}
	}
	if status := dispatcher.AlertDeliveryStatus(); status.QueueDepth != 0 || status.DroppedCount != 0 {
		t.Fatalf("internal event status = %+v", status)
	}
}

func TestDispatcherShutdownCancelsBlockedDelivery(t *testing.T) {
	service, _ := dispatcherService(t, 1)
	adapter := &recordingDeliveryAdapter{started: make(chan struct{}), block: true}
	dispatcher := NewDispatcher(DispatcherConfig{
		Enabled: true, Service: service, Adapter: adapter, EventQueueCapacity: 1,
		DeliveryQueueCapacity: 1, WorkerCount: 1,
	})
	dispatcher.Start()
	event := triggerEvent()
	event.ID = 41
	if !dispatcher.tryEnqueue(event) {
		t.Fatal("dispatcher rejected event")
	}
	select {
	case <-adapter.started:
	case <-time.After(time.Second):
		t.Fatal("delivery did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := dispatcher.Shutdown(ctx); err == nil {
		t.Fatal("Shutdown returned nil while delivery was blocked")
	}
	waitFor(t, time.Second, func() bool { return !dispatcher.AlertDeliveryStatus().Running })
}

func TestRetryDelayUsesExponentialCapAndBoundedRetryAfter(t *testing.T) {
	if got := retryDelay(100*time.Millisecond, time.Second, 1, 0); got != 100*time.Millisecond {
		t.Fatalf("attempt 1 delay = %s", got)
	}
	if got := retryDelay(100*time.Millisecond, time.Second, 4, 0); got != 800*time.Millisecond {
		t.Fatalf("attempt 4 delay = %s", got)
	}
	if got := retryDelay(100*time.Millisecond, time.Second, 2, 30*time.Second); got != time.Second {
		t.Fatalf("Retry-After delay = %s, want cap", got)
	}
}

func dispatcherService(t *testing.T, aggregationCount int) (*Service, Rule) {
	t.Helper()
	repository := newMemoryRepository()
	service := NewService(repository, testKeyring(t))
	action, err := service.CreateAction(context.Background(), ActionInput{
		Name: "test webhook", Kind: ActionKindGenericWebhook, Destination: "https://example.test/hook?auth=secret", Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateAction: %v", err)
	}
	rule := validRule()
	rule.ActionID = action.ID
	rule.AggregationCount = aggregationCount
	if aggregationCount == 1 {
		rule.AggregationWindowSeconds = 0
	}
	rule, err = service.CreateRule(context.Background(), rule)
	if err != nil {
		t.Fatalf("CreateRule: %v", err)
	}
	return service, rule
}

func shutdownDispatcher(t *testing.T, dispatcher *Dispatcher) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := dispatcher.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition was not satisfied before timeout")
		}
		time.Sleep(time.Millisecond)
	}
}
