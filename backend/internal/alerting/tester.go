package alerting

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

const actionTestFinalizeTimeout = 2 * time.Second

var ErrRateLimited = errors.New("alert action test rate limited")

type actionTestService interface {
	BeginActionTest(context.Context, int64, time.Time) (ActionTestAttempt, error)
	FinalizeActionTest(context.Context, ActionTestAttempt, ActionTestResult) error
}

type ActionTester struct {
	service actionTestService
	adapter DeliveryAdapter
	now     func() time.Time
	active  chan struct{}
}

type RateLimitError struct {
	RetryAfter time.Duration
}

func (err *RateLimitError) Error() string { return ErrRateLimited.Error() }
func (err *RateLimitError) Unwrap() error { return ErrRateLimited }

func NewActionTester(service actionTestService, adapter DeliveryAdapter) *ActionTester {
	if adapter == nil {
		adapter = NewHTTPAdapter(nil)
	}
	return &ActionTester{service: service, adapter: adapter, now: time.Now, active: make(chan struct{}, 1)}
}

func (tester *ActionTester) TestAction(ctx context.Context, actionID int64, expectedUpdatedAt time.Time) (ActionTestResult, error) {
	if tester == nil || tester.service == nil {
		return ActionTestResult{}, ErrRepository
	}
	if actionID <= 0 || expectedUpdatedAt.IsZero() {
		return ActionTestResult{}, ErrInvalidInput
	}
	select {
	case tester.active <- struct{}{}:
		defer func() { <-tester.active }()
	default:
		return ActionTestResult{}, &RateLimitError{RetryAfter: time.Second}
	}

	testAttempt, err := tester.service.BeginActionTest(ctx, actionID, expectedUpdatedAt)
	if err != nil {
		return ActionTestResult{}, err
	}

	started := tester.now()
	deliveryAttempt := tester.adapter.Deliver(ctx, testAttempt.Action, actionTestNotification(testAttempt.Action, testAttempt.StartedAt))
	testedAt := tester.now().UTC()
	result := ActionTestResult{
		TestedAt: testedAt, Status: ActionTestStatusFailed, LatencyMS: max(testedAt.Sub(started).Milliseconds(), 0),
		ErrorCode: deliveryAttempt.ErrorCode, Retryable: deliveryAttempt.Retryable,
	}
	if deliveryAttempt.StatusCode > 0 {
		statusCode := deliveryAttempt.StatusCode
		result.HTTPStatus = &statusCode
	}
	if deliveryAttempt.Success {
		result.Status = ActionTestStatusPassed
		result.ErrorCode = ""
		result.Retryable = false
	} else if result.ErrorCode == "" {
		result.ErrorCode = "alert_delivery_failed"
	}

	finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), actionTestFinalizeTimeout)
	defer cancel()
	finalizeCtx = withActionTestIntent(finalizeCtx, testAttempt.Action, result)
	if err := tester.service.FinalizeActionTest(finalizeCtx, testAttempt, result); err != nil {
		return ActionTestResult{}, err
	}
	return result, nil
}

func actionTestNotification(action ResolvedAction, now time.Time) Notification {
	correlationID := systemevent.NewCorrelationID()
	return Notification{
		DeliveryID: correlationID, Decision: DecisionNotify, RuleName: "Test notification",
		OccurredAt: now, Category: systemevent.CategoryAudit, Severity: systemevent.SeverityInfo,
		Action: systemevent.ActionAlertDeliveryTested, Outcome: systemevent.OutcomeSuccess,
		Target:  systemevent.Target{Type: "alert_action", ID: auditID(action.ID), Name: action.Name},
		Message: "N2API alert action test", CorrelationID: correlationID,
	}
}

func withActionTestIntent(ctx context.Context, action ResolvedAction, result ActionTestResult) context.Context {
	severity := systemevent.SeverityInfo
	outcome := systemevent.OutcomeSuccess
	if result.Status == ActionTestStatusFailed {
		severity = systemevent.SeverityWarning
		outcome = systemevent.OutcomeFailure
	}
	values := map[string]any{"latency_ms": result.LatencyMS, "retryable": result.Retryable}
	allowed := []string{"latency_ms", "retryable"}
	if result.HTTPStatus != nil {
		values["status_code"] = *result.HTTPStatus
		allowed = append(allowed, "status_code")
	}
	metadata, _ := systemevent.SafeMetadata(values, allowed...)
	return systemevent.WithIntent(ctx, systemevent.EventIntent{
		Category: systemevent.CategoryAudit, Severity: severity, Action: systemevent.ActionAlertDeliveryTested,
		Outcome: outcome, Target: systemevent.Target{Type: "alert_action", ID: auditID(action.ID), Name: action.Name},
		ErrorCode: result.ErrorCode, Metadata: metadata,
	})
}

func auditID(id int64) string {
	if id <= 0 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}
