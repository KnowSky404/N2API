package systemevent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
	"time"
)

const (
	MaxNameLength          = 128
	MaxCodeLength          = 100
	MaxMessageLength       = 500
	MaxMetadataEncodedSize = 8 * 1024
)

type Category string
type Severity string
type Outcome string
type ActorType string
type Action string

const (
	CategoryAudit     Category = "audit"
	CategorySecurity  Category = "security"
	CategoryOAuth     Category = "oauth"
	CategoryScheduler Category = "scheduler"
	CategoryRuntime   Category = "runtime"

	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"

	OutcomeSuccess Outcome = "success"
	OutcomeFailure Outcome = "failure"
	OutcomePartial Outcome = "partial"

	ActorAdmin  ActorType = "admin"
	ActorSystem ActorType = "system"
)

const (
	ActionAuthLoginSucceeded                 Action = "auth.login.succeeded"
	ActionAuthLoginFailed                    Action = "auth.login.failed"
	ActionAuthSessionRejected                Action = "auth.session.rejected"
	ActionAuthSessionRevoked                 Action = "auth.session.revoked"
	ActionAuthSessionsRevokedOthers          Action = "auth.sessions.revoked_others"
	ActionAuthLogoutSucceeded                Action = "auth.logout.succeeded"
	ActionAuthPasswordChanged                Action = "auth.password.changed"
	ActionAuthPasswordChangeFailed           Action = "auth.password.change_failed"
	ActionAuthBootstrapCreated               Action = "auth.bootstrap.created"
	ActionAuthBootstrapUsernameUpdated       Action = "auth.bootstrap.username_updated"
	ActionAPIKeyCreated                      Action = "api_key.created"
	ActionAPIKeySecretViewed                 Action = "api_key.secret.viewed"
	ActionAPIKeyRenamed                      Action = "api_key.renamed"
	ActionAPIKeyEnabled                      Action = "api_key.enabled"
	ActionAPIKeyDisabled                     Action = "api_key.disabled"
	ActionAPIKeyStatusUpdateFailed           Action = "api_key.status_update.failed"
	ActionAPIKeyModelPolicyUpdated           Action = "api_key.model_policy.updated"
	ActionAPIKeyLimitsUpdated                Action = "api_key.limits.updated"
	ActionAPIKeyBudgetsUpdated               Action = "api_key.budgets.updated"
	ActionAPIKeyRoutingPoolUpdated           Action = "api_key.routing_pool.updated"
	ActionAPIKeyRevoked                      Action = "api_key.revoked"
	ActionAPIKeyDeleted                      Action = "api_key.deleted"
	ActionAPIKeyPurged                       Action = "api_key.purged"
	ActionRoutingPoolCreated                 Action = "routing_pool.created"
	ActionRoutingPoolUpdated                 Action = "routing_pool.updated"
	ActionRoutingPoolDeleted                 Action = "routing_pool.deleted"
	ActionRoutingPoolAccountsReplaced        Action = "routing_pool.accounts.replaced"
	ActionRequestLogCleanupCompleted         Action = "request_log.cleanup.completed"
	ActionRequestLogExported                 Action = "request_log.exported"
	ActionRequestLogExportAccepted           Action = "request_log.export.accepted"
	ActionRequestLogExportCompleted          Action = "request_log.export.completed"
	ActionConfigurationExported              Action = "configuration.exported"
	ActionGatewaySettingsUpdated             Action = "gateway_settings.updated"
	ActionModelSettingsUpdated               Action = "model_settings.updated"
	ActionUsagePricingUpdated                Action = "usage_pricing.updated"
	ActionUsagePricingSynced                 Action = "usage_pricing.synced"
	ActionUsagePricingShutdownRemoved        Action = "usage_pricing.shutdown_removed"
	ActionUsagePricingUpcomingIgnored        Action = "usage_pricing.upcoming_ignored"
	ActionFingerprintProfileCreated          Action = "fingerprint_profile.created"
	ActionFingerprintProfileUpdated          Action = "fingerprint_profile.updated"
	ActionFingerprintProfileDeleted          Action = "fingerprint_profile.deleted"
	ActionErrorPassthroughRuleCreated        Action = "error_passthrough_rule.created"
	ActionErrorPassthroughRuleUpdated        Action = "error_passthrough_rule.updated"
	ActionErrorPassthroughRuleDeleted        Action = "error_passthrough_rule.deleted"
	ActionProviderAccountCreated             Action = "provider_account.created"
	ActionProviderAccountUpdated             Action = "provider_account.updated"
	ActionProviderAccountDisconnected        Action = "provider_account.disconnected"
	ActionProviderAccountDisconnectAll       Action = "provider_account.disconnect_all"
	ActionProviderAccountPaused              Action = "provider_account.paused"
	ActionProviderAccountStatusReset         Action = "provider_account.status_reset"
	ActionProviderAccountTested              Action = "provider_account.tested"
	ActionProviderAccountModelTested         Action = "provider_account.model_tested"
	ActionProviderAccountModelsReplaced      Action = "provider_account.models.replaced"
	ActionProviderAccountModelsSynced        Action = "provider_account.models.synced"
	ActionProviderAccountBatchUpdated        Action = "provider_account.batch.updated"
	ActionProviderAccountBatchDisconnected   Action = "provider_account.batch.disconnected"
	ActionProviderAccountBatchPaused         Action = "provider_account.batch.paused"
	ActionProviderAccountBatchStatusReset    Action = "provider_account.batch.status_reset"
	ActionProviderAccountBatchTested         Action = "provider_account.batch.tested"
	ActionProviderAccountBatchRefreshed      Action = "provider_account.batch.refreshed"
	ActionProviderAccountBatchModelsReplaced Action = "provider_account.batch.models_replaced"
	ActionOAuthConnectStarted                Action = "oauth.connect.started"
	ActionOAuthConnectFailed                 Action = "oauth.connect.failed"
	ActionOAuthCallbackCompleted             Action = "oauth.callback.completed"
	ActionOAuthCallbackFailed                Action = "oauth.callback.failed"
	ActionOAuthRefreshManualSucceeded        Action = "oauth.refresh.manual.succeeded"
	ActionOAuthRefreshManualFailed           Action = "oauth.refresh.manual.failed"
	ActionOAuthRefreshAutomaticSucceeded     Action = "oauth.refresh.automatic.succeeded"
	ActionOAuthRefreshAutomaticFailed        Action = "oauth.refresh.automatic.failed"
	ActionSchedulerProviderAutoTestCompleted Action = "scheduler.provider_account_auto_test.completed"
	ActionSchedulerAPIKeyPurgeCompleted      Action = "scheduler.api_key_purge.completed"
	ActionSchedulerEventRetentionCompleted   Action = "scheduler.system_event_retention.completed"
	ActionProviderAccountCircuitOpened       Action = "provider_account.circuit.opened"
	ActionProviderAccountRateLimited         Action = "provider_account.rate_limited"
	ActionProviderAccountExpired             Action = "provider_account.expired"
	ActionProviderAccountRecovered           Action = "provider_account.recovered"
)

const (
	ActionSchedulerRequestLogRetentionSucceeded Action = "scheduler.request_log_retention.succeeded"
	ActionSchedulerRequestLogRetentionFailed    Action = "scheduler.request_log_retention.failed"
)

var (
	ErrInvalidEvent  = errors.New("invalid system event")
	ErrInvalidCursor = errors.New("invalid system event cursor")
	requestIDRE      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,99}$`)
)

type Actor struct {
	Type ActorType `json:"type"`
	ID   int64     `json:"id,omitempty"`
	Name string    `json:"name"`
}

type RequestContext struct {
	CorrelationID string `json:"correlationId"`
	SourceIP      string `json:"sourceIp,omitempty"`
	HTTPMethod    string `json:"httpMethod,omitempty"`
	RoutePattern  string `json:"routePattern,omitempty"`
	Actor         Actor  `json:"actor"`
}

type Target struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

type EventIntent struct {
	Category  Category       `json:"category"`
	Severity  Severity       `json:"severity"`
	Action    Action         `json:"action"`
	Outcome   Outcome        `json:"outcome"`
	Target    Target         `json:"target"`
	ErrorCode string         `json:"errorCode,omitempty"`
	Message   string         `json:"message,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	StartedAt time.Time      `json:"-"`
}

type Event struct {
	ID            int64          `json:"id"`
	OccurredAt    time.Time      `json:"occurredAt"`
	Category      Category       `json:"category"`
	Severity      Severity       `json:"severity"`
	Action        Action         `json:"action"`
	Outcome       Outcome        `json:"outcome"`
	Actor         Actor          `json:"actor"`
	Target        Target         `json:"target"`
	CorrelationID string         `json:"correlationId"`
	SourceIP      string         `json:"sourceIp,omitempty"`
	HTTPMethod    string         `json:"httpMethod,omitempty"`
	RoutePattern  string         `json:"routePattern,omitempty"`
	StatusCode    *int           `json:"statusCode,omitempty"`
	DurationMS    int64          `json:"durationMs"`
	ErrorCode     string         `json:"errorCode,omitempty"`
	Message       string         `json:"message,omitempty"`
	Metadata      map[string]any `json:"metadata"`
}

type Filter struct {
	Limit      int
	Cursor     string
	Since      time.Time
	Category   Category
	Outcome    Outcome
	Severity   Severity
	Action     Action
	Actor      string
	TargetType string
	TargetID   string
	Query      string
}

type Page struct {
	Events     []Event `json:"events"`
	NextCursor string  `json:"nextCursor"`
	HasMore    bool    `json:"hasMore"`
}

type requestContextKey struct{}
type eventIntentKey struct{}

func WithRequestContext(ctx context.Context, request RequestContext) context.Context {
	return context.WithValue(ctx, requestContextKey{}, request)
}

func FromContext(ctx context.Context) (RequestContext, bool) {
	request, ok := ctx.Value(requestContextKey{}).(RequestContext)
	return request, ok
}

func WithIntent(ctx context.Context, intent EventIntent) context.Context {
	if intent.StartedAt.IsZero() {
		intent.StartedAt = time.Now()
	}
	return context.WithValue(ctx, eventIntentKey{}, intent)
}

func IntentFromContext(ctx context.Context) (EventIntent, bool) {
	intent, ok := ctx.Value(eventIntentKey{}).(EventIntent)
	return intent, ok
}

func BuildEvent(ctx context.Context, intent EventIntent, target Target, occurredAt time.Time, duration time.Duration) Event {
	request, ok := FromContext(ctx)
	if !ok {
		request = RequestContext{CorrelationID: NewCorrelationID(), Actor: Actor{Type: ActorSystem}}
	}
	if target == (Target{}) {
		target = intent.Target
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	return Event{
		OccurredAt: occurredAt.UTC(), Category: intent.Category, Severity: intent.Severity,
		Action: intent.Action, Outcome: intent.Outcome, Actor: request.Actor, Target: target,
		CorrelationID: request.CorrelationID, SourceIP: request.SourceIP, HTTPMethod: request.HTTPMethod,
		RoutePattern: request.RoutePattern, DurationMS: max(duration.Milliseconds(), 0), ErrorCode: intent.ErrorCode,
		Message: intent.Message, Metadata: cloneMetadata(intent.Metadata),
	}
}

func NewCorrelationID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(fmt.Sprintf("generate system event correlation ID: %v", err))
	}
	return hex.EncodeToString(value[:])
}

func ValidCorrelationID(value string) bool { return requestIDRE.MatchString(value) }

func NormalizeCorrelationID(value string) string {
	value = strings.TrimSpace(value)
	if ValidCorrelationID(value) {
		return value
	}
	return NewCorrelationID()
}

func ValidateIntent(intent EventIntent) error {
	return ValidateEvent(Event{
		OccurredAt: time.Now().UTC(), Category: intent.Category, Severity: intent.Severity,
		Action: intent.Action, Outcome: intent.Outcome, Actor: Actor{Type: ActorSystem}, Target: intent.Target,
		CorrelationID: NewCorrelationID(), ErrorCode: intent.ErrorCode, Message: intent.Message, Metadata: intent.Metadata,
	})
}

func ValidateEvent(event Event) error {
	if !validCategory(event.Category) || !validSeverity(event.Severity) || !validOutcome(event.Outcome) || !validActorType(event.Actor.Type) {
		return ErrInvalidEvent
	}
	if !IsKnownAction(event.Action) || !ValidCorrelationID(event.CorrelationID) {
		return ErrInvalidEvent
	}
	if len(event.Actor.Name) > MaxNameLength || len(event.Target.Type) > MaxNameLength || len(event.Target.ID) > MaxNameLength || len(event.Target.Name) > MaxNameLength {
		return ErrInvalidEvent
	}
	if len(event.ErrorCode) > MaxCodeLength || len(event.Message) > MaxMessageLength || strings.ContainsAny(event.Message, "\r\n") {
		return ErrInvalidEvent
	}
	if event.StatusCode != nil && (*event.StatusCode < 100 || *event.StatusCode > 599) || event.DurationMS < 0 {
		return ErrInvalidEvent
	}
	if event.SourceIP != "" {
		if _, err := netip.ParseAddr(event.SourceIP); err != nil {
			return ErrInvalidEvent
		}
	}
	metadata, err := json.Marshal(metadataOrEmpty(event.Metadata))
	if err != nil || len(metadata) > MaxMetadataEncodedSize || !encodedMetadataKeysSafe(metadata) {
		return ErrInvalidEvent
	}
	return nil
}

func IsKnownAction(action Action) bool {
	_, ok := knownActions[action]
	return ok
}

func IsValidCategory(value Category) bool { return validCategory(value) }
func IsValidSeverity(value Severity) bool { return validSeverity(value) }
func IsValidOutcome(value Outcome) bool   { return validOutcome(value) }

func SafeMetadata(values map[string]any, allowedKeys ...string) (map[string]any, error) {
	allowed := make(map[string]struct{}, len(allowedKeys))
	for _, key := range allowedKeys {
		allowed[key] = struct{}{}
	}
	for key := range values {
		if _, ok := allowed[key]; !ok || secretLikeKey(key) {
			return nil, ErrInvalidEvent
		}
	}
	result := cloneMetadata(values)
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > MaxMetadataEncodedSize || !encodedMetadataKeysSafe(encoded) {
		return nil, ErrInvalidEvent
	}
	return result, nil
}

func metadataOrEmpty(metadata map[string]any) map[string]any {
	if metadata == nil {
		return map[string]any{}
	}
	return metadata
}

func cloneMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return map[string]any{}
	}
	result := make(map[string]any, len(metadata))
	for key, value := range metadata {
		result[key] = value
	}
	return result
}

func metadataKeysSafe(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if secretLikeKey(key) || !metadataKeysSafe(child) {
				return false
			}
		}
	case map[string]string:
		for key := range typed {
			if secretLikeKey(key) {
				return false
			}
		}
	case []any:
		for _, child := range typed {
			if !metadataKeysSafe(child) {
				return false
			}
		}
	case []map[string]any:
		for _, child := range typed {
			if !metadataKeysSafe(child) {
				return false
			}
		}
	}
	return true
}

func encodedMetadataKeysSafe(encoded []byte) bool {
	var normalized any
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		return false
	}
	return metadataKeysSafe(normalized)
}

func secretLikeKey(key string) bool {
	normalized := strings.NewReplacer("-", "", "_", "", ".", "").Replace(strings.ToLower(key))
	for _, forbidden := range []string{"password", "secret", "token", "authorization", "cookie", "oauthcode", "state", "nonce", "verifier", "challenge", "requestbody", "responsebody", "proxyurl"} {
		if strings.Contains(normalized, forbidden) {
			return true
		}
	}
	return false
}

func validCategory(value Category) bool {
	return value == CategoryAudit || value == CategorySecurity || value == CategoryOAuth || value == CategoryScheduler || value == CategoryRuntime
}
func validSeverity(value Severity) bool {
	return value == SeverityInfo || value == SeverityWarning || value == SeverityError
}
func validOutcome(value Outcome) bool {
	return value == OutcomeSuccess || value == OutcomeFailure || value == OutcomePartial
}
func validActorType(value ActorType) bool { return value == ActorAdmin || value == ActorSystem }

var knownActions = map[Action]struct{}{
	ActionAuthLoginSucceeded: {}, ActionAuthLoginFailed: {}, ActionAuthSessionRejected: {}, ActionAuthSessionRevoked: {},
	ActionAuthSessionsRevokedOthers: {}, ActionAuthLogoutSucceeded: {},
	ActionAuthPasswordChanged: {}, ActionAuthPasswordChangeFailed: {}, ActionAuthBootstrapCreated: {}, ActionAuthBootstrapUsernameUpdated: {},
	ActionAPIKeyCreated: {}, ActionAPIKeySecretViewed: {}, ActionAPIKeyRenamed: {}, ActionAPIKeyEnabled: {}, ActionAPIKeyDisabled: {}, ActionAPIKeyStatusUpdateFailed: {},
	ActionAPIKeyModelPolicyUpdated: {}, ActionAPIKeyLimitsUpdated: {}, ActionAPIKeyBudgetsUpdated: {}, ActionAPIKeyRoutingPoolUpdated: {},
	ActionAPIKeyRevoked: {}, ActionAPIKeyDeleted: {}, ActionAPIKeyPurged: {}, ActionRoutingPoolCreated: {}, ActionRoutingPoolUpdated: {},
	ActionRoutingPoolDeleted: {}, ActionRoutingPoolAccountsReplaced: {}, ActionRequestLogCleanupCompleted: {}, ActionRequestLogExported: {},
	ActionRequestLogExportAccepted: {}, ActionRequestLogExportCompleted: {}, ActionConfigurationExported: {},
	ActionGatewaySettingsUpdated: {}, ActionModelSettingsUpdated: {}, ActionUsagePricingUpdated: {}, ActionUsagePricingSynced: {},
	ActionUsagePricingShutdownRemoved: {}, ActionUsagePricingUpcomingIgnored: {}, ActionFingerprintProfileCreated: {}, ActionFingerprintProfileUpdated: {},
	ActionFingerprintProfileDeleted: {}, ActionErrorPassthroughRuleCreated: {}, ActionErrorPassthroughRuleUpdated: {}, ActionErrorPassthroughRuleDeleted: {},
	ActionProviderAccountCreated: {}, ActionProviderAccountUpdated: {}, ActionProviderAccountDisconnected: {}, ActionProviderAccountDisconnectAll: {},
	ActionProviderAccountPaused: {}, ActionProviderAccountStatusReset: {}, ActionProviderAccountTested: {}, ActionProviderAccountModelTested: {},
	ActionProviderAccountModelsReplaced: {}, ActionProviderAccountModelsSynced: {}, ActionProviderAccountBatchUpdated: {}, ActionProviderAccountBatchDisconnected: {},
	ActionProviderAccountBatchPaused: {}, ActionProviderAccountBatchStatusReset: {}, ActionProviderAccountBatchTested: {},
	ActionProviderAccountBatchRefreshed: {}, ActionProviderAccountBatchModelsReplaced: {},
	ActionOAuthConnectStarted: {}, ActionOAuthConnectFailed: {}, ActionOAuthCallbackCompleted: {}, ActionOAuthCallbackFailed: {}, ActionOAuthRefreshManualSucceeded: {}, ActionOAuthRefreshManualFailed: {},
	ActionOAuthRefreshAutomaticSucceeded: {}, ActionOAuthRefreshAutomaticFailed: {}, ActionSchedulerProviderAutoTestCompleted: {},
	ActionSchedulerAPIKeyPurgeCompleted: {}, ActionSchedulerEventRetentionCompleted: {},
	ActionSchedulerRequestLogRetentionSucceeded: {}, ActionSchedulerRequestLogRetentionFailed: {}, ActionProviderAccountCircuitOpened: {},
	ActionProviderAccountRateLimited: {}, ActionProviderAccountExpired: {}, ActionProviderAccountRecovered: {},
}
