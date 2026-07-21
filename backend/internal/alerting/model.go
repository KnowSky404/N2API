package alerting

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

const (
	MaxActionNameLength         = 128
	MaxActionDestinationLength  = 4096
	MaxRuleNameLength           = 128
	MaxRuleStatesPerRule        = 1024
	MaxAggregationCount         = 1024
	MaxAggregationWindowSeconds = 24 * 60 * 60
	MaxCooldownSeconds          = 7 * 24 * 60 * 60
	ActionTestAdmissionWindow   = 30 * time.Second
)

type ActionTestStatus string

const (
	ActionTestStatusPassed ActionTestStatus = "passed"
	ActionTestStatusFailed ActionTestStatus = "failed"
)

type ActionKind string

const (
	ActionKindGenericWebhook ActionKind = "generic_webhook"
	ActionKindNtfy           ActionKind = "ntfy"
)

type DeduplicationScope string

const (
	DeduplicationScopeRule   DeduplicationScope = "rule"
	DeduplicationScopeTarget DeduplicationScope = "target"
)

type StatePhase string

const (
	StatePhaseIdle   StatePhase = "idle"
	StatePhaseFiring StatePhase = "firing"
)

type Decision string

const (
	DecisionNone     Decision = "none"
	DecisionNotify   Decision = "notify"
	DecisionSuppress Decision = "suppress"
	DecisionRecover  Decision = "recover"
)

type DeliveryStatus struct {
	Enabled         bool       `json:"enabled"`
	Running         bool       `json:"running"`
	QueueDepth      int        `json:"queueDepth"`
	QueueCapacity   int        `json:"queueCapacity"`
	ActiveWorkers   int        `json:"activeWorkers"`
	WorkerCount     int        `json:"workerCount"`
	EnqueuedCount   uint64     `json:"enqueuedCount"`
	DeliveredCount  uint64     `json:"deliveredCount"`
	FailedCount     uint64     `json:"failedCount"`
	DroppedCount    uint64     `json:"droppedCount"`
	RetriedCount    uint64     `json:"retriedCount"`
	LastDeliveredAt *time.Time `json:"lastDeliveredAt,omitempty"`
	LastFailedAt    *time.Time `json:"lastFailedAt,omitempty"`
	LastErrorCode   string     `json:"lastErrorCode"`
}

var (
	ErrInvalidInput  = errors.New("invalid alerting input")
	ErrNotFound      = errors.New("alerting record not found")
	ErrRepository    = errors.New("alerting repository operation failed")
	ErrStateCapacity = errors.New("alerting rule state capacity reached")
	ErrConflict      = errors.New("alerting record conflict")
)

type Action struct {
	ID                    int64            `json:"id"`
	Name                  string           `json:"name"`
	Kind                  ActionKind       `json:"kind"`
	Enabled               bool             `json:"enabled"`
	DestinationConfigured bool             `json:"destinationConfigured"`
	LastTestedAt          *time.Time       `json:"lastTestedAt"`
	LastTestStatus        ActionTestStatus `json:"lastTestStatus"`
	LastTestHTTPStatus    *int             `json:"lastTestHTTPStatus"`
	LastTestLatencyMS     int64            `json:"lastTestLatencyMs"`
	LastTestErrorCode     string           `json:"lastTestErrorCode"`
	LastTestRetryable     bool             `json:"lastTestRetryable"`
	CreatedAt             time.Time        `json:"createdAt"`
	UpdatedAt             time.Time        `json:"updatedAt"`
}

type ActionInput struct {
	Name        string
	Kind        ActionKind
	Destination string `json:"-"`
	Enabled     bool
}

type ActionUpdateInput struct {
	Name              string
	Kind              ActionKind
	Destination       *string `json:"-"`
	Enabled           bool
	ExpectedUpdatedAt time.Time
}

type ActionCreate struct {
	Name                 string
	Kind                 ActionKind
	EncryptedDestination string `json:"-"`
	Enabled              bool
}

type ActionUpdate struct {
	Name                 string
	Kind                 ActionKind
	EncryptedDestination *string `json:"-"`
	Enabled              bool
	ExpectedUpdatedAt    time.Time
}

type ActionForDelivery struct {
	ID                   int64
	Name                 string
	Kind                 ActionKind
	Enabled              bool
	EncryptedDestination string `json:"-"`
	UpdatedAt            time.Time
}

type ResolvedAction struct {
	ID          int64
	Name        string
	Kind        ActionKind
	Enabled     bool
	Destination string `json:"-"`
	UpdatedAt   time.Time
}

type ActionTestStart struct {
	Action       ActionForDelivery
	AttemptToken string
	StartedAt    time.Time
}

type ActionTestAttempt struct {
	Action       ResolvedAction
	AttemptToken string
	StartedAt    time.Time
}

type ActionTestResult struct {
	TestedAt   time.Time        `json:"testedAt"`
	Status     ActionTestStatus `json:"status"`
	HTTPStatus *int             `json:"httpStatus"`
	LatencyMS  int64            `json:"latencyMs"`
	ErrorCode  string           `json:"errorCode"`
	Retryable  bool             `json:"retryable"`
}

type Rule struct {
	ID                       int64                `json:"id"`
	Name                     string               `json:"name"`
	ActionID                 int64                `json:"actionId"`
	Enabled                  bool                 `json:"enabled"`
	Category                 systemevent.Category `json:"category,omitempty"`
	Severity                 systemevent.Severity `json:"severity,omitempty"`
	EventAction              systemevent.Action   `json:"eventAction,omitempty"`
	RecoveryAction           systemevent.Action   `json:"recoveryAction,omitempty"`
	AggregationCount         int                  `json:"aggregationCount"`
	AggregationWindowSeconds int                  `json:"aggregationWindowSeconds"`
	CooldownSeconds          int                  `json:"cooldownSeconds"`
	DeduplicationScope       DeduplicationScope   `json:"deduplicationScope"`
	NotifyRecovery           bool                 `json:"notifyRecovery"`
	CreatedAt                time.Time            `json:"createdAt"`
	UpdatedAt                time.Time            `json:"updatedAt"`
}

type RuleCreate struct {
	Rule Rule
}

type RuleUpdate struct {
	Rule              Rule
	ExpectedUpdatedAt time.Time
}

type RuleState struct {
	RuleID               int64      `json:"ruleId"`
	DeduplicationKeyHash string     `json:"deduplicationKeyHash"`
	Phase                StatePhase `json:"phase"`
	WindowStartedAt      *time.Time `json:"windowStartedAt,omitempty"`
	WindowMatchCount     int        `json:"windowMatchCount"`
	CooldownUntil        *time.Time `json:"cooldownUntil,omitempty"`
	LastMatchedAt        *time.Time `json:"lastMatchedAt,omitempty"`
	LastNotifiedAt       *time.Time `json:"lastNotifiedAt,omitempty"`
	LastRecoveredAt      *time.Time `json:"lastRecoveredAt,omitempty"`
	UpdatedAt            time.Time  `json:"updatedAt"`
}

type Evaluation struct {
	Rule            Rule
	ActionEnabled   bool
	ActionUpdatedAt time.Time
	State           RuleState
	Decision        Decision
}

type Repository interface {
	CreateAction(context.Context, ActionCreate) (Action, error)
	UpdateAction(context.Context, int64, ActionUpdate) (Action, error)
	DeleteAction(context.Context, int64) error
	GetAction(context.Context, int64) (Action, error)
	ListActions(context.Context) ([]Action, error)
	GetEncryptedDestination(context.Context, int64) (string, error)
	GetActionForDelivery(context.Context, int64) (ActionForDelivery, error)
	BeginActionTest(context.Context, int64, time.Time, string) (ActionTestStart, error)
	FinalizeActionTest(context.Context, int64, string, ActionTestResult) (Action, error)

	CreateRule(context.Context, RuleCreate) (Rule, error)
	UpdateRule(context.Context, int64, RuleUpdate) (Rule, error)
	DeleteRule(context.Context, int64) error
	GetRule(context.Context, int64) (Rule, error)
	ListRules(context.Context) ([]Rule, error)

	GetRuleState(context.Context, int64, string) (RuleState, error)
	SaveRuleState(context.Context, RuleState) error
	EvaluateRuleEvent(context.Context, int64, systemevent.Event, time.Time) (RuleState, Decision, error)
	EvaluateRuleEventForDelivery(context.Context, int64, systemevent.Event, time.Time) (Evaluation, error)
}

func (rule Rule) validate() error {
	if invalidName(rule.Name, MaxRuleNameLength) || rule.ActionID <= 0 {
		return ErrInvalidInput
	}
	if rule.Category == "" && rule.Severity == "" && rule.EventAction == "" {
		return ErrInvalidInput
	}
	if rule.Category != "" && !systemevent.IsValidCategory(rule.Category) {
		return ErrInvalidInput
	}
	if rule.Severity != "" && !systemevent.IsValidSeverity(rule.Severity) {
		return ErrInvalidInput
	}
	if rule.EventAction != "" && !systemevent.IsKnownAction(rule.EventAction) {
		return ErrInvalidInput
	}
	if rule.RecoveryAction != "" && !systemevent.IsKnownAction(rule.RecoveryAction) {
		return ErrInvalidInput
	}
	if systemevent.IsAlertDeliveryInternalAction(rule.EventAction) || systemevent.IsAlertDeliveryInternalAction(rule.RecoveryAction) {
		return ErrInvalidInput
	}
	if rule.EventAction != "" && rule.RecoveryAction == rule.EventAction {
		return ErrInvalidInput
	}
	if rule.NotifyRecovery && rule.RecoveryAction == "" {
		return ErrInvalidInput
	}
	if rule.AggregationCount < 1 || rule.AggregationCount > MaxAggregationCount {
		return ErrInvalidInput
	}
	if rule.AggregationWindowSeconds < 0 || rule.AggregationWindowSeconds > MaxAggregationWindowSeconds {
		return ErrInvalidInput
	}
	if rule.AggregationCount > 1 && rule.AggregationWindowSeconds == 0 {
		return ErrInvalidInput
	}
	if rule.CooldownSeconds < 0 || rule.CooldownSeconds > MaxCooldownSeconds {
		return ErrInvalidInput
	}
	if rule.DeduplicationScope != DeduplicationScopeRule && rule.DeduplicationScope != DeduplicationScopeTarget {
		return ErrInvalidInput
	}
	return nil
}

func invalidName(value string, maxLength int) bool {
	if strings.TrimSpace(value) == "" || len(value) > maxLength {
		return true
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}
