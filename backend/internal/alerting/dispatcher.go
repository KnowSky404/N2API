package alerting

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

const (
	defaultEventQueueCapacity     = 256
	defaultDeliveryQueueCapacity  = 64
	defaultDeliveryWorkerCount    = 2
	defaultDeliveryMaxAttempts    = 3
	defaultDeliveryBackoff        = 250 * time.Millisecond
	defaultDeliveryMaxBackoff     = 2 * time.Second
	defaultListenerRetryDelay     = time.Second
	defaultOverflowReportInterval = time.Minute
)

type EventSubscription interface {
	Wait(context.Context) (int64, error)
	Close()
}

type DispatcherService interface {
	ListRules(context.Context) ([]Rule, error)
	GetRule(context.Context, int64) (Rule, error)
	ResolveActionForDelivery(context.Context, int64) (ResolvedAction, error)
	EvaluateRuleEventForDelivery(context.Context, int64, systemevent.Event, time.Time) (Evaluation, error)
}

type DeliveryEventRecorder interface {
	Insert(context.Context, systemevent.Event) error
}

type DispatcherConfig struct {
	Enabled                bool
	Service                DispatcherService
	Subscribe              func(context.Context) (EventSubscription, error)
	GetEvent               func(context.Context, int64) (systemevent.Event, error)
	Recorder               DeliveryEventRecorder
	Adapter                DeliveryAdapter
	EventQueueCapacity     int
	DeliveryQueueCapacity  int
	WorkerCount            int
	MaxAttempts            int
	InitialBackoff         time.Duration
	MaxBackoff             time.Duration
	ListenerRetryDelay     time.Duration
	OverflowReportInterval time.Duration
	Now                    func() time.Time
}

type deliveryJob struct {
	rule     Rule
	decision Decision
	event    systemevent.Event
}

type Dispatcher struct {
	cfg             DispatcherConfig
	eventQueue      chan systemevent.Event
	deliveryQueues  []chan deliveryJob
	queueMu         sync.RWMutex
	startOnce       sync.Once
	stopOnce        sync.Once
	accepting       atomic.Bool
	running         atomic.Bool
	activeWorkers   atomic.Int64
	enqueued        atomic.Uint64
	delivered       atomic.Uint64
	failed          atomic.Uint64
	dropped         atomic.Uint64
	droppedPending  atomic.Uint64
	retried         atomic.Uint64
	statusMu        sync.Mutex
	lastDeliveredAt *time.Time
	lastFailedAt    *time.Time
	lastErrorCode   string
	listenCancel    context.CancelFunc
	workCancel      context.CancelFunc
	listenerDone    chan struct{}
	evaluatorDone   chan struct{}
	workersDone     chan struct{}
	reporterDone    chan struct{}
	done            chan struct{}
}

func NewDispatcher(cfg DispatcherConfig) *Dispatcher {
	if cfg.EventQueueCapacity <= 0 {
		cfg.EventQueueCapacity = defaultEventQueueCapacity
	}
	if cfg.DeliveryQueueCapacity <= 0 {
		cfg.DeliveryQueueCapacity = defaultDeliveryQueueCapacity
	}
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = defaultDeliveryWorkerCount
	}
	if cfg.DeliveryQueueCapacity < cfg.WorkerCount {
		cfg.DeliveryQueueCapacity = cfg.WorkerCount
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = defaultDeliveryMaxAttempts
	}
	if cfg.InitialBackoff <= 0 {
		cfg.InitialBackoff = defaultDeliveryBackoff
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = defaultDeliveryMaxBackoff
	}
	if cfg.ListenerRetryDelay <= 0 {
		cfg.ListenerRetryDelay = defaultListenerRetryDelay
	}
	if cfg.OverflowReportInterval <= 0 {
		cfg.OverflowReportInterval = defaultOverflowReportInterval
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Adapter == nil {
		cfg.Adapter = NewHTTPAdapter(nil)
	}
	return &Dispatcher{
		cfg:            cfg,
		eventQueue:     make(chan systemevent.Event, cfg.EventQueueCapacity),
		deliveryQueues: newDeliveryQueues(cfg.DeliveryQueueCapacity, cfg.WorkerCount),
		listenerDone:   make(chan struct{}), evaluatorDone: make(chan struct{}),
		workersDone: make(chan struct{}), reporterDone: make(chan struct{}), done: make(chan struct{}),
	}
}

func (dispatcher *Dispatcher) Start() {
	if dispatcher == nil {
		return
	}
	dispatcher.startOnce.Do(func() {
		if !dispatcher.cfg.Enabled || dispatcher.cfg.Service == nil {
			close(dispatcher.listenerDone)
			close(dispatcher.evaluatorDone)
			close(dispatcher.workersDone)
			close(dispatcher.reporterDone)
			close(dispatcher.done)
			return
		}
		listenCtx, listenCancel := context.WithCancel(context.Background())
		workCtx, workCancel := context.WithCancel(context.Background())
		dispatcher.listenCancel = listenCancel
		dispatcher.workCancel = workCancel
		dispatcher.accepting.Store(true)
		dispatcher.running.Store(true)
		go dispatcher.runListener(listenCtx)
		go dispatcher.runEvaluator(workCtx)
		go dispatcher.runWorkers(workCtx)
		go dispatcher.runOverflowReporter(workCtx)
	})
}

func (dispatcher *Dispatcher) Shutdown(ctx context.Context) error {
	if dispatcher == nil {
		return nil
	}
	dispatcher.Start()
	dispatcher.stopOnce.Do(func() {
		if !dispatcher.cfg.Enabled || dispatcher.cfg.Service == nil {
			return
		}
		dispatcher.accepting.Store(false)
		dispatcher.listenCancel()
		go dispatcher.finishShutdown()
	})
	select {
	case <-dispatcher.done:
		return nil
	case <-ctx.Done():
		if dispatcher.workCancel != nil {
			dispatcher.workCancel()
		}
		return ctx.Err()
	}
}

func (dispatcher *Dispatcher) AlertDeliveryStatus() DeliveryStatus {
	if dispatcher == nil {
		return DeliveryStatus{}
	}
	dispatcher.statusMu.Lock()
	defer dispatcher.statusMu.Unlock()
	queueDepth, queueCapacity := len(dispatcher.eventQueue), cap(dispatcher.eventQueue)
	for _, queue := range dispatcher.deliveryQueues {
		queueDepth += len(queue)
		queueCapacity += cap(queue)
	}
	return DeliveryStatus{
		Enabled: dispatcher.cfg.Enabled, Running: dispatcher.running.Load(),
		QueueDepth:    queueDepth,
		QueueCapacity: queueCapacity,
		ActiveWorkers: int(dispatcher.activeWorkers.Load()), WorkerCount: dispatcher.cfg.WorkerCount,
		EnqueuedCount: dispatcher.enqueued.Load(), DeliveredCount: dispatcher.delivered.Load(),
		FailedCount: dispatcher.failed.Load(), DroppedCount: dispatcher.dropped.Load(), RetriedCount: dispatcher.retried.Load(),
		LastDeliveredAt: cloneTime(dispatcher.lastDeliveredAt), LastFailedAt: cloneTime(dispatcher.lastFailedAt),
		LastErrorCode: dispatcher.lastErrorCode,
	}
}

func (dispatcher *Dispatcher) finishShutdown() {
	<-dispatcher.listenerDone
	dispatcher.queueMu.Lock()
	close(dispatcher.eventQueue)
	dispatcher.queueMu.Unlock()
	<-dispatcher.evaluatorDone
	<-dispatcher.workersDone
	if dispatcher.workCancel != nil {
		dispatcher.workCancel()
	}
	<-dispatcher.reporterDone
	dispatcher.running.Store(false)
	close(dispatcher.done)
}

func (dispatcher *Dispatcher) runListener(ctx context.Context) {
	defer close(dispatcher.listenerDone)
	if dispatcher.cfg.Subscribe == nil || dispatcher.cfg.GetEvent == nil {
		<-ctx.Done()
		return
	}
	for ctx.Err() == nil {
		subscription, err := dispatcher.cfg.Subscribe(ctx)
		if err != nil {
			dispatcher.markFailure("alert_delivery_listener_unavailable")
			if !waitContext(ctx, dispatcher.cfg.ListenerRetryDelay) {
				return
			}
			continue
		}
		for ctx.Err() == nil {
			id, err := subscription.Wait(ctx)
			if err != nil {
				break
			}
			event, err := dispatcher.cfg.GetEvent(ctx, id)
			if err != nil {
				dispatcher.markFailure("alert_delivery_event_read_failed")
				continue
			}
			dispatcher.tryEnqueue(event)
		}
		subscription.Close()
		if ctx.Err() == nil && !waitContext(ctx, dispatcher.cfg.ListenerRetryDelay) {
			return
		}
	}
}

func (dispatcher *Dispatcher) tryEnqueue(event systemevent.Event) bool {
	if systemevent.IsAlertDeliveryInternalAction(event.Action) || !dispatcher.accepting.Load() {
		return false
	}
	dispatcher.queueMu.RLock()
	defer dispatcher.queueMu.RUnlock()
	if !dispatcher.accepting.Load() {
		return false
	}
	select {
	case dispatcher.eventQueue <- event:
		dispatcher.enqueued.Add(1)
		return true
	default:
		dispatcher.dropped.Add(1)
		dispatcher.droppedPending.Add(1)
		return false
	}
}

func (dispatcher *Dispatcher) runEvaluator(ctx context.Context) {
	defer close(dispatcher.evaluatorDone)
	defer func() {
		for _, queue := range dispatcher.deliveryQueues {
			close(queue)
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-dispatcher.eventQueue:
			if !ok {
				return
			}
			dispatcher.evaluateEvent(ctx, event)
		}
	}
}

func (dispatcher *Dispatcher) evaluateEvent(ctx context.Context, event systemevent.Event) {
	if systemevent.IsAlertDeliveryInternalAction(event.Action) {
		return
	}
	rules, err := dispatcher.cfg.Service.ListRules(ctx)
	if err != nil {
		dispatcher.recordFailureEvent(0, 0, 0, 0, "alert_delivery_rules_unavailable")
		return
	}
	for _, rule := range rules {
		evaluation, err := dispatcher.cfg.Service.EvaluateRuleEventForDelivery(ctx, rule.ID, event, dispatcher.cfg.Now().UTC())
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			dispatcher.recordFailureEvent(rule.ID, rule.ActionID, 0, 0, "alert_delivery_evaluation_failed")
			continue
		}
		if !evaluation.ActionEnabled || (evaluation.Decision != DecisionNotify && evaluation.Decision != DecisionRecover) {
			continue
		}
		queue := dispatcher.deliveryQueues[deliveryWorkerIndex(evaluation.Rule, event, len(dispatcher.deliveryQueues))]
		select {
		case queue <- deliveryJob{rule: evaluation.Rule, decision: evaluation.Decision, event: event}:
		case <-ctx.Done():
			return
		}
	}
}

func (dispatcher *Dispatcher) runWorkers(ctx context.Context) {
	var workers sync.WaitGroup
	workers.Add(dispatcher.cfg.WorkerCount)
	for _, queue := range dispatcher.deliveryQueues {
		go func(queue <-chan deliveryJob) {
			defer workers.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case job, ok := <-queue:
					if !ok {
						return
					}
					dispatcher.deliver(ctx, job)
				}
			}
		}(queue)
	}
	workers.Wait()
	close(dispatcher.workersDone)
}

func newDeliveryQueues(totalCapacity, workers int) []chan deliveryJob {
	queues := make([]chan deliveryJob, workers)
	base, remainder := totalCapacity/workers, totalCapacity%workers
	for index := range queues {
		capacity := base
		if index < remainder {
			capacity++
		}
		queues[index] = make(chan deliveryJob, capacity)
	}
	return queues
}

func deliveryWorkerIndex(rule Rule, event systemevent.Event, workers int) int {
	if workers <= 1 {
		return 0
	}
	hash := rule.DeduplicationKeyHash(event)
	value, err := strconv.ParseUint(hash[:16], 16, 64)
	if err != nil {
		return int(rule.ID % int64(workers))
	}
	return int(value % uint64(workers))
}

func (dispatcher *Dispatcher) deliver(ctx context.Context, job deliveryJob) {
	dispatcher.activeWorkers.Add(1)
	defer dispatcher.activeWorkers.Add(-1)
	rule, err := dispatcher.cfg.Service.GetRule(ctx, job.rule.ID)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			dispatcher.recordFailureEvent(job.rule.ID, job.rule.ActionID, 0, 0, "alert_delivery_rule_unavailable")
		}
		return
	}
	if !rule.Enabled || !rule.UpdatedAt.Equal(job.rule.UpdatedAt) {
		return
	}
	action, err := dispatcher.cfg.Service.ResolveActionForDelivery(ctx, rule.ActionID)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			dispatcher.recordFailureEvent(rule.ID, rule.ActionID, 0, 0, "alert_delivery_action_unavailable")
		}
		return
	}
	if !action.Enabled {
		return
	}
	notification := notificationFor(rule, job.decision, job.event)
	var result DeliveryAttempt
	for attempt := 1; attempt <= dispatcher.cfg.MaxAttempts; attempt++ {
		result = dispatcher.cfg.Adapter.Deliver(ctx, action, notification)
		if result.Success {
			dispatcher.delivered.Add(1)
			dispatcher.markDelivered()
			return
		}
		if !result.Retryable || attempt == dispatcher.cfg.MaxAttempts || ctx.Err() != nil {
			if ctx.Err() == nil {
				dispatcher.recordFailureEvent(rule.ID, action.ID, attempt, result.StatusCode, result.ErrorCode)
			}
			return
		}
		dispatcher.retried.Add(1)
		delay := retryDelay(dispatcher.cfg.InitialBackoff, dispatcher.cfg.MaxBackoff, attempt, result.RetryAfter)
		if !waitContext(ctx, delay) {
			return
		}
	}
}

func (dispatcher *Dispatcher) runOverflowReporter(ctx context.Context) {
	defer close(dispatcher.reporterDone)
	ticker := time.NewTicker(dispatcher.cfg.OverflowReportInterval)
	defer ticker.Stop()
	defer dispatcher.reportOverflow()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			dispatcher.reportOverflow()
		}
	}
}

func (dispatcher *Dispatcher) reportOverflow() {
	count := dispatcher.droppedPending.Swap(0)
	if count == 0 || dispatcher.cfg.Recorder == nil {
		return
	}
	metadata, _ := systemevent.SafeMetadata(map[string]any{
		"dropped_count": count, "queue_capacity": cap(dispatcher.eventQueue),
	}, "dropped_count", "queue_capacity")
	event := dispatcher.deliveryEvent(systemevent.ActionAlertDeliveryQueueOverflow, systemevent.SeverityWarning,
		"alert_delivery_queue_overflow", systemevent.Target{Type: "alert_delivery", ID: "dispatcher"}, metadata)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := dispatcher.cfg.Recorder.Insert(ctx, event); err != nil {
		dispatcher.droppedPending.Add(count)
		dispatcher.markFailure("alert_delivery_overflow_event_failed")
	}
}

func (dispatcher *Dispatcher) recordFailureEvent(ruleID, actionID int64, attempts, statusCode int, errorCode string) {
	if errorCode == "" {
		errorCode = "alert_delivery_failed"
	}
	dispatcher.failed.Add(1)
	dispatcher.markFailure(errorCode)
	if dispatcher.cfg.Recorder == nil {
		return
	}
	values := map[string]any{"rule_id": ruleID, "action_id": actionID, "attempt_count": attempts}
	allowed := []string{"rule_id", "action_id", "attempt_count"}
	if statusCode > 0 {
		values["status_code"] = statusCode
		allowed = append(allowed, "status_code")
	}
	metadata, _ := systemevent.SafeMetadata(values, allowed...)
	target := systemevent.Target{Type: "alert_delivery", ID: strconv.FormatInt(actionID, 10)}
	event := dispatcher.deliveryEvent(systemevent.ActionAlertDeliveryFailed, systemevent.SeverityError, errorCode, target, metadata)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = dispatcher.cfg.Recorder.Insert(ctx, event)
}

func (dispatcher *Dispatcher) deliveryEvent(action systemevent.Action, severity systemevent.Severity, errorCode string, target systemevent.Target, metadata map[string]any) systemevent.Event {
	now := dispatcher.cfg.Now().UTC()
	message := "Alert delivery failed"
	if action == systemevent.ActionAlertDeliveryQueueOverflow {
		message = "Alert delivery queue overflowed"
	}
	ctx := systemevent.WithRequestContext(context.Background(), systemevent.RequestContext{
		CorrelationID: systemevent.NewCorrelationID(), Actor: systemevent.Actor{Type: systemevent.ActorSystem, Name: "alert_delivery"},
	})
	return systemevent.BuildEvent(ctx, systemevent.EventIntent{
		Category: systemevent.CategoryRuntime, Severity: severity, Action: action, Outcome: systemevent.OutcomeFailure,
		Target: target, ErrorCode: errorCode, Message: message, Metadata: metadata,
	}, target, now, 0)
}

func (dispatcher *Dispatcher) markDelivered() {
	now := dispatcher.cfg.Now().UTC()
	dispatcher.statusMu.Lock()
	dispatcher.lastDeliveredAt = &now
	dispatcher.statusMu.Unlock()
}

func (dispatcher *Dispatcher) markFailure(errorCode string) {
	now := dispatcher.cfg.Now().UTC()
	dispatcher.statusMu.Lock()
	dispatcher.lastFailedAt = &now
	dispatcher.lastErrorCode = errorCode
	dispatcher.statusMu.Unlock()
}

func retryDelay(initial, maximum time.Duration, attempt int, retryAfter time.Duration) time.Duration {
	delay := initial
	for range attempt - 1 {
		if delay >= maximum/2 {
			delay = maximum
			break
		}
		delay *= 2
	}
	if retryAfter > delay {
		delay = retryAfter
	}
	if delay > maximum {
		return maximum
	}
	return delay
}

func waitContext(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
