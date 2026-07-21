package alerting

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

func TestRuleMatchesExactFiltersAndRejectsOversizedMetadata(t *testing.T) {
	rule := validRule()
	event := triggerEvent()
	matched, err := rule.MatchesTrigger(event)
	if err != nil || !matched {
		t.Fatalf("MatchesTrigger = %v, %v, want true", matched, err)
	}
	for name, mutate := range map[string]func(*systemevent.Event){
		"category": func(event *systemevent.Event) { event.Category = systemevent.CategoryRuntime },
		"severity": func(event *systemevent.Event) { event.Severity = systemevent.SeverityError },
		"action":   func(event *systemevent.Event) { event.Action = systemevent.ActionOAuthRefreshManualFailed },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := event
			mutate(&candidate)
			matched, err := rule.MatchesTrigger(candidate)
			if err != nil || matched {
				t.Fatalf("MatchesTrigger = %v, %v, want false", matched, err)
			}
		})
	}

	event.Metadata = map[string]any{"safe": strings.Repeat("x", systemevent.MaxMetadataEncodedSize)}
	if _, err := rule.MatchesTrigger(event); err == nil {
		t.Fatal("MatchesTrigger accepted oversized event metadata")
	}
}

func TestRuleNeverMatchesAlertDeliveryInternalEvents(t *testing.T) {
	rule := validRule()
	rule.Category = systemevent.CategoryRuntime
	rule.Severity = systemevent.SeverityError
	rule.EventAction = ""
	for _, action := range []systemevent.Action{
		systemevent.ActionAlertDeliveryFailed,
		systemevent.ActionAlertDeliveryQueueOverflow,
	} {
		event := triggerEvent()
		event.Category = systemevent.CategoryRuntime
		event.Action = action
		matched, err := rule.MatchesTrigger(event)
		if err != nil || matched {
			t.Fatalf("MatchesTrigger(%q) = %v, %v, want false", action, matched, err)
		}
	}
}

func TestRequestLogRetentionTemplateMatchesFullAndPartialFailuresAndRecovers(t *testing.T) {
	template, ok := ruleTemplate(RequestLogRetentionFailedTemplateKey)
	if !ok {
		t.Fatal("request log retention template is missing")
	}
	rule := template.rule(7)
	rule.ID = 11
	rule.Enabled = true
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	for _, severity := range []systemevent.Severity{systemevent.SeverityError, systemevent.SeverityWarning} {
		t.Run(string(severity), func(t *testing.T) {
			failure := triggerEvent()
			failure.Category = systemevent.CategoryScheduler
			failure.Severity = severity
			failure.Action = systemevent.ActionSchedulerRequestLogRetentionFailed
			failure.Target = systemevent.Target{Type: "request_log_collection", ID: "retention"}
			state := RuleState{
				RuleID: rule.ID, DeduplicationKeyHash: rule.DeduplicationKeyHash(failure), Phase: StatePhaseIdle,
			}
			state, decision, err := Evaluate(rule, state, failure, now)
			if err != nil || decision != DecisionNotify || state.Phase != StatePhaseFiring {
				t.Fatalf("failure evaluation state=%+v decision=%q err=%v", state, decision, err)
			}

			recovery := failure
			recovery.Severity = systemevent.SeverityInfo
			recovery.Action = systemevent.ActionSchedulerRequestLogRetentionSucceeded
			recovery.Outcome = systemevent.OutcomeSuccess
			state, decision, err = Evaluate(rule, state, recovery, now.Add(time.Minute))
			if err != nil || decision != DecisionRecover || state.Phase != StatePhaseIdle {
				t.Fatalf("recovery evaluation state=%+v decision=%q err=%v", state, decision, err)
			}
		})
	}
}

func TestProviderAutoTestTemplateAggregatesFullAndPartialFailuresAndRecovers(t *testing.T) {
	template, ok := ruleTemplate(ProviderAutoTestFailedTemplateKey)
	if !ok {
		t.Fatal("provider auto-test template is missing")
	}
	rule := template.rule(7)
	rule.ID = 12
	rule.Enabled = true
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	for _, severity := range []systemevent.Severity{systemevent.SeverityError, systemevent.SeverityWarning} {
		t.Run(string(severity), func(t *testing.T) {
			failure := triggerEvent()
			failure.Category = systemevent.CategoryScheduler
			failure.Severity = severity
			failure.Action = systemevent.ActionSchedulerProviderAutoTestFailed
			failure.Target = systemevent.Target{Type: "provider_account_scheduler", ID: "auto_test"}
			state := RuleState{RuleID: rule.ID, DeduplicationKeyHash: rule.DeduplicationKeyHash(failure), Phase: StatePhaseIdle}
			state, decision, err := Evaluate(rule, state, failure, now)
			if err != nil || decision != DecisionNone || state.WindowMatchCount != 1 {
				t.Fatalf("first failure state=%+v decision=%q err=%v", state, decision, err)
			}
			state, decision, err = Evaluate(rule, state, failure, now.Add(time.Minute))
			if err != nil || decision != DecisionNotify || state.Phase != StatePhaseFiring {
				t.Fatalf("second failure state=%+v decision=%q err=%v", state, decision, err)
			}

			recovery := failure
			recovery.Severity = systemevent.SeverityInfo
			recovery.Action = systemevent.ActionSchedulerProviderAutoTestCompleted
			recovery.Outcome = systemevent.OutcomeSuccess
			state, decision, err = Evaluate(rule, state, recovery, now.Add(2*time.Minute))
			if err != nil || decision != DecisionRecover || state.Phase != StatePhaseIdle {
				t.Fatalf("recovery state=%+v decision=%q err=%v", state, decision, err)
			}
		})
	}
}

func TestProviderAccountExpiredTemplateNotifiesPerTargetAndRecovers(t *testing.T) {
	template, ok := ruleTemplate(ProviderAccountExpiredTemplateKey)
	if !ok {
		t.Fatal("provider account expired template is missing")
	}
	rule := template.rule(7)
	rule.ID = 13
	rule.Enabled = true
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	failure := triggerEvent()
	failure.Category = systemevent.CategoryRuntime
	failure.Severity = systemevent.SeverityWarning
	failure.Action = systemevent.ActionProviderAccountExpired
	failure.Outcome = systemevent.OutcomeFailure
	failure.Target = systemevent.Target{Type: "provider_account", ID: "42", Name: "Work account"}
	state := RuleState{RuleID: rule.ID, DeduplicationKeyHash: rule.DeduplicationKeyHash(failure), Phase: StatePhaseIdle}
	state, decision, err := Evaluate(rule, state, failure, now)
	if err != nil || decision != DecisionNotify || state.Phase != StatePhaseFiring {
		t.Fatalf("expiry evaluation state=%+v decision=%q err=%v", state, decision, err)
	}
	state, decision, err = Evaluate(rule, state, failure, now.Add(time.Hour))
	if err != nil || decision != DecisionSuppress || state.Phase != StatePhaseFiring {
		t.Fatalf("cooldown evaluation state=%+v decision=%q err=%v", state, decision, err)
	}

	otherTarget := failure
	otherTarget.Target.ID = "43"
	if rule.DeduplicationKeyHash(failure) == rule.DeduplicationKeyHash(otherTarget) {
		t.Fatal("target-scoped template reused the same deduplication key for different accounts")
	}

	recovery := failure
	recovery.Severity = systemevent.SeverityInfo
	recovery.Action = systemevent.ActionProviderAccountRecovered
	recovery.Outcome = systemevent.OutcomeSuccess
	state, decision, err = Evaluate(rule, state, recovery, now.Add(2*time.Hour))
	if err != nil || decision != DecisionRecover || state.Phase != StatePhaseIdle {
		t.Fatalf("recovery evaluation state=%+v decision=%q err=%v", state, decision, err)
	}
}

func TestProviderAccountCircuitOpenTemplateNotifiesPerTargetAndRecovers(t *testing.T) {
	template, ok := ruleTemplate(ProviderAccountCircuitOpenTemplateKey)
	if !ok {
		t.Fatal("provider account circuit-open template is missing")
	}
	rule := template.rule(7)
	rule.ID = 14
	rule.Enabled = true
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	failure := triggerEvent()
	failure.Category = systemevent.CategoryRuntime
	failure.Severity = systemevent.SeverityWarning
	failure.Action = systemevent.ActionProviderAccountCircuitOpened
	failure.Outcome = systemevent.OutcomeFailure
	failure.Target = systemevent.Target{Type: "provider_account", ID: "42", Name: "Work account"}
	state := RuleState{RuleID: rule.ID, DeduplicationKeyHash: rule.DeduplicationKeyHash(failure), Phase: StatePhaseIdle}
	state, decision, err := Evaluate(rule, state, failure, now)
	if err != nil || decision != DecisionNotify || state.Phase != StatePhaseFiring {
		t.Fatalf("circuit-open evaluation state=%+v decision=%q err=%v", state, decision, err)
	}
	state, decision, err = Evaluate(rule, state, failure, now.Add(30*time.Minute))
	if err != nil || decision != DecisionSuppress || state.Phase != StatePhaseFiring {
		t.Fatalf("cooldown evaluation state=%+v decision=%q err=%v", state, decision, err)
	}

	otherTarget := failure
	otherTarget.Target.ID = "43"
	if rule.DeduplicationKeyHash(failure) == rule.DeduplicationKeyHash(otherTarget) {
		t.Fatal("target-scoped template reused the same deduplication key for different accounts")
	}

	recovery := failure
	recovery.Severity = systemevent.SeverityInfo
	recovery.Action = systemevent.ActionProviderAccountRecovered
	recovery.Outcome = systemevent.OutcomeSuccess
	state, decision, err = Evaluate(rule, state, recovery, now.Add(45*time.Minute))
	if err != nil || decision != DecisionRecover || state.Phase != StatePhaseIdle {
		t.Fatalf("recovery evaluation state=%+v decision=%q err=%v", state, decision, err)
	}
}

func TestAPIKeyBudget80PercentTemplateNotifiesPerStreamAndRecovers(t *testing.T) {
	template, ok := ruleTemplate(APIKeyBudget80PercentTemplateKey)
	if !ok {
		t.Fatal("API key 80 percent budget template is missing")
	}
	rule := template.rule(7)
	rule.ID = 15
	rule.Enabled = true
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	failure := triggerEvent()
	failure.Category = systemevent.CategoryRuntime
	failure.Severity = systemevent.SeverityWarning
	failure.Action = systemevent.ActionAPIKeyBudgetThreshold80Crossed
	failure.Outcome = systemevent.OutcomePartial
	failure.Target = systemevent.Target{Type: "client_api_key_budget", ID: "42:request:24h", Name: "Codex laptop"}
	state := RuleState{RuleID: rule.ID, DeduplicationKeyHash: rule.DeduplicationKeyHash(failure), Phase: StatePhaseIdle}
	state, decision, err := Evaluate(rule, state, failure, now)
	if err != nil || decision != DecisionNotify || state.Phase != StatePhaseFiring {
		t.Fatalf("80 percent evaluation state=%+v decision=%q err=%v", state, decision, err)
	}
	state, decision, err = Evaluate(rule, state, failure, now.Add(time.Hour))
	if err != nil || decision != DecisionSuppress || state.Phase != StatePhaseFiring {
		t.Fatalf("80 percent cooldown state=%+v decision=%q err=%v", state, decision, err)
	}

	otherStream := failure
	otherStream.Target.ID = "42:cost:24h"
	if rule.DeduplicationKeyHash(failure) == rule.DeduplicationKeyHash(otherStream) {
		t.Fatal("target-scoped template reused the same deduplication key for different budget streams")
	}

	recovery := failure
	recovery.Severity = systemevent.SeverityInfo
	recovery.Action = systemevent.ActionAPIKeyBudgetThreshold80Recovered
	recovery.Outcome = systemevent.OutcomeSuccess
	state, decision, err = Evaluate(rule, state, recovery, now.Add(2*time.Hour))
	if err != nil || decision != DecisionRecover || state.Phase != StatePhaseIdle {
		t.Fatalf("80 percent recovery state=%+v decision=%q err=%v", state, decision, err)
	}
}

func TestAPIKeyBudget100PercentTemplateNotifiesPerStreamAndRecovers(t *testing.T) {
	template, ok := ruleTemplate(APIKeyBudget100PercentTemplateKey)
	if !ok {
		t.Fatal("API key 100 percent budget template is missing")
	}
	rule := template.rule(7)
	rule.ID = 16
	rule.Enabled = true
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	failure := triggerEvent()
	failure.Category = systemevent.CategoryRuntime
	failure.Severity = systemevent.SeverityError
	failure.Action = systemevent.ActionAPIKeyBudgetThreshold100Crossed
	failure.Outcome = systemevent.OutcomeFailure
	failure.Target = systemevent.Target{Type: "client_api_key_budget", ID: "42:cost:30d", Name: "Codex laptop"}
	state := RuleState{RuleID: rule.ID, DeduplicationKeyHash: rule.DeduplicationKeyHash(failure), Phase: StatePhaseIdle}
	state, decision, err := Evaluate(rule, state, failure, now)
	if err != nil || decision != DecisionNotify || state.Phase != StatePhaseFiring {
		t.Fatalf("100 percent evaluation state=%+v decision=%q err=%v", state, decision, err)
	}
	state, decision, err = Evaluate(rule, state, failure, now.Add(30*time.Minute))
	if err != nil || decision != DecisionSuppress || state.Phase != StatePhaseFiring {
		t.Fatalf("100 percent cooldown state=%+v decision=%q err=%v", state, decision, err)
	}

	otherStream := failure
	otherStream.Target.ID = "42:token:30d"
	if rule.DeduplicationKeyHash(failure) == rule.DeduplicationKeyHash(otherStream) {
		t.Fatal("target-scoped template reused the same deduplication key for different exhausted budget streams")
	}

	wrongRecovery := failure
	wrongRecovery.Severity = systemevent.SeverityInfo
	wrongRecovery.Action = systemevent.ActionAPIKeyBudgetThreshold80Recovered
	wrongRecovery.Outcome = systemevent.OutcomeSuccess
	state, decision, err = Evaluate(rule, state, wrongRecovery, now.Add(40*time.Minute))
	if err != nil || decision != DecisionNone || state.Phase != StatePhaseFiring {
		t.Fatalf("80 percent recovery affected 100 percent state=%+v decision=%q err=%v", state, decision, err)
	}

	recovery := wrongRecovery
	recovery.Action = systemevent.ActionAPIKeyBudgetThreshold100Recovered
	state, decision, err = Evaluate(rule, state, recovery, now.Add(45*time.Minute))
	if err != nil || decision != DecisionRecover || state.Phase != StatePhaseIdle {
		t.Fatalf("100 percent recovery state=%+v decision=%q err=%v", state, decision, err)
	}
}

func TestRoutingPoolExhaustedTemplateNotifiesPerAPIKeyAndRecoversExactly(t *testing.T) {
	template, ok := ruleTemplate(RoutingPoolExhaustedTemplateKey)
	if !ok {
		t.Fatal("routing pool exhausted template is missing")
	}
	rule := template.rule(7)
	rule.ID = 17
	rule.Enabled = true
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	failure := triggerEvent()
	failure.Category = systemevent.CategoryRuntime
	failure.Severity = systemevent.SeverityError
	failure.Action = systemevent.ActionAPIKeyRoutingPoolExhausted
	failure.Outcome = systemevent.OutcomeFailure
	failure.Target = systemevent.Target{Type: "client_api_key", ID: "42", Name: "Codex laptop"}
	state := RuleState{RuleID: rule.ID, DeduplicationKeyHash: rule.DeduplicationKeyHash(failure), Phase: StatePhaseIdle}
	state, decision, err := Evaluate(rule, state, failure, now)
	if err != nil || decision != DecisionNotify || state.Phase != StatePhaseFiring {
		t.Fatalf("routing exhaustion evaluation state=%+v decision=%q err=%v", state, decision, err)
	}
	state, decision, err = Evaluate(rule, state, failure, now.Add(30*time.Minute))
	if err != nil || decision != DecisionSuppress || state.Phase != StatePhaseFiring {
		t.Fatalf("routing exhaustion cooldown state=%+v decision=%q err=%v", state, decision, err)
	}

	otherKey := failure
	otherKey.Target.ID = "43"
	if rule.DeduplicationKeyHash(failure) == rule.DeduplicationKeyHash(otherKey) {
		t.Fatal("target-scoped template reused the same deduplication key for different API keys")
	}

	wrongRecovery := failure
	wrongRecovery.Severity = systemevent.SeverityInfo
	wrongRecovery.Action = systemevent.ActionAPIKeyBudgetThreshold100Recovered
	wrongRecovery.Outcome = systemevent.OutcomeSuccess
	state, decision, err = Evaluate(rule, state, wrongRecovery, now.Add(40*time.Minute))
	if err != nil || decision != DecisionNone || state.Phase != StatePhaseFiring {
		t.Fatalf("unrelated recovery affected routing exhaustion state=%+v decision=%q err=%v", state, decision, err)
	}

	recovery := wrongRecovery
	recovery.Action = systemevent.ActionAPIKeyRoutingPoolRecovered
	state, decision, err = Evaluate(rule, state, recovery, now.Add(45*time.Minute))
	if err != nil || decision != DecisionRecover || state.Phase != StatePhaseIdle {
		t.Fatalf("routing exhaustion recovery state=%+v decision=%q err=%v", state, decision, err)
	}
}

func TestAPIKeyPurgeFailedTemplateNotifiesForCollectionAndRecoversExactly(t *testing.T) {
	template, ok := ruleTemplate(APIKeyPurgeFailedTemplateKey)
	if !ok {
		t.Fatal("API key purge failure template is missing")
	}
	rule := template.rule(7)
	rule.ID = 18
	rule.Enabled = true
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	failure := triggerEvent()
	failure.Category = systemevent.CategoryScheduler
	failure.Severity = systemevent.SeverityError
	failure.Action = systemevent.ActionSchedulerAPIKeyPurgeFailed
	failure.Outcome = systemevent.OutcomeFailure
	failure.Target = systemevent.Target{Type: "client_api_key_collection"}
	state := RuleState{RuleID: rule.ID, DeduplicationKeyHash: rule.DeduplicationKeyHash(failure), Phase: StatePhaseIdle}
	state, decision, err := Evaluate(rule, state, failure, now)
	if err != nil || decision != DecisionNotify || state.Phase != StatePhaseFiring {
		t.Fatalf("API key purge failure evaluation state=%+v decision=%q err=%v", state, decision, err)
	}
	state, decision, err = Evaluate(rule, state, failure, now.Add(12*time.Hour))
	if err != nil || decision != DecisionSuppress || state.Phase != StatePhaseFiring {
		t.Fatalf("API key purge failure cooldown state=%+v decision=%q err=%v", state, decision, err)
	}

	otherTarget := failure
	otherTarget.Target.ID = "other"
	if rule.DeduplicationKeyHash(failure) == rule.DeduplicationKeyHash(otherTarget) {
		t.Fatal("target-scoped template reused the same deduplication key for different collections")
	}

	wrongRecovery := failure
	wrongRecovery.Severity = systemevent.SeverityInfo
	wrongRecovery.Action = systemevent.ActionSchedulerEventRetentionCompleted
	wrongRecovery.Outcome = systemevent.OutcomeSuccess
	state, decision, err = Evaluate(rule, state, wrongRecovery, now.Add(13*time.Hour))
	if err != nil || decision != DecisionNone || state.Phase != StatePhaseFiring {
		t.Fatalf("unrelated recovery affected API key purge failure state=%+v decision=%q err=%v", state, decision, err)
	}

	recovery := wrongRecovery
	recovery.Action = systemevent.ActionSchedulerAPIKeyPurgeCompleted
	state, decision, err = Evaluate(rule, state, recovery, now.Add(14*time.Hour))
	if err != nil || decision != DecisionRecover || state.Phase != StatePhaseIdle {
		t.Fatalf("API key purge recovery state=%+v decision=%q err=%v", state, decision, err)
	}
}

func TestSystemEventRetentionFailedTemplateMatchesFullAndPartialFailuresAndRecoversExactly(t *testing.T) {
	template, ok := ruleTemplate(SystemEventRetentionFailedTemplateKey)
	if !ok {
		t.Fatal("System Event retention failure template is missing")
	}
	rule := template.rule(7)
	rule.ID = 19
	rule.Enabled = true
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	for _, severity := range []systemevent.Severity{systemevent.SeverityError, systemevent.SeverityWarning} {
		t.Run(string(severity), func(t *testing.T) {
			failure := triggerEvent()
			failure.Category = systemevent.CategoryScheduler
			failure.Severity = severity
			failure.Action = systemevent.ActionSchedulerEventRetentionFailed
			failure.Outcome = systemevent.OutcomeFailure
			if severity == systemevent.SeverityWarning {
				failure.Outcome = systemevent.OutcomePartial
			}
			failure.Target = systemevent.Target{Type: "system_events", ID: "retention"}
			state := RuleState{RuleID: rule.ID, DeduplicationKeyHash: rule.DeduplicationKeyHash(failure), Phase: StatePhaseIdle}
			state, decision, err := Evaluate(rule, state, failure, now)
			if err != nil || decision != DecisionNotify || state.Phase != StatePhaseFiring {
				t.Fatalf("retention failure evaluation state=%+v decision=%q err=%v", state, decision, err)
			}
			state, decision, err = Evaluate(rule, state, failure, now.Add(12*time.Hour))
			if err != nil || decision != DecisionSuppress || state.Phase != StatePhaseFiring {
				t.Fatalf("retention failure cooldown state=%+v decision=%q err=%v", state, decision, err)
			}

			wrongRecovery := failure
			wrongRecovery.Severity = systemevent.SeverityInfo
			wrongRecovery.Action = systemevent.ActionSchedulerAPIKeyPurgeCompleted
			wrongRecovery.Outcome = systemevent.OutcomeSuccess
			state, decision, err = Evaluate(rule, state, wrongRecovery, now.Add(13*time.Hour))
			if err != nil || decision != DecisionNone || state.Phase != StatePhaseFiring {
				t.Fatalf("unrelated recovery affected retention failure state=%+v decision=%q err=%v", state, decision, err)
			}

			recovery := wrongRecovery
			recovery.Action = systemevent.ActionSchedulerEventRetentionCompleted
			state, decision, err = Evaluate(rule, state, recovery, now.Add(14*time.Hour))
			if err != nil || decision != DecisionRecover || state.Phase != StatePhaseIdle {
				t.Fatalf("retention recovery state=%+v decision=%q err=%v", state, decision, err)
			}
		})
	}
}

func TestEvaluateAggregatesNotifiesSuppressesAndRecoversAtExactBoundaries(t *testing.T) {
	rule := validRule()
	rule.ID = 9
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	event := triggerEvent()
	key := rule.DeduplicationKeyHash(event)
	if len(key) != 64 {
		t.Fatalf("deduplication hash = %q", key)
	}
	if _, err := hex.DecodeString(key); err != nil {
		t.Fatalf("deduplication hash is not lowercase hex: %q", key)
	}

	state := RuleState{RuleID: rule.ID, DeduplicationKeyHash: key, Phase: StatePhaseIdle}
	state, decision, err := Evaluate(rule, state, event, now)
	if err != nil || decision != DecisionNone || state.WindowMatchCount != 1 || state.Phase != StatePhaseIdle {
		t.Fatalf("first match state=%+v decision=%q err=%v", state, decision, err)
	}
	state, decision, err = Evaluate(rule, state, event, now.Add(60*time.Second))
	if err != nil || decision != DecisionNotify || state.Phase != StatePhaseFiring || state.CooldownUntil == nil || !state.CooldownUntil.Equal(now.Add(360*time.Second)) {
		t.Fatalf("threshold state=%+v decision=%q err=%v", state, decision, err)
	}
	state, decision, err = Evaluate(rule, state, event, now.Add(359*time.Second))
	if err != nil || decision != DecisionSuppress {
		t.Fatalf("cooldown state=%+v decision=%q err=%v", state, decision, err)
	}
	state, decision, err = Evaluate(rule, state, event, now.Add(360*time.Second))
	if err != nil || decision != DecisionNotify || state.CooldownUntil == nil || !state.CooldownUntil.Equal(now.Add(660*time.Second)) {
		t.Fatalf("cooldown boundary state=%+v decision=%q err=%v", state, decision, err)
	}

	recovery := event
	recovery.Action = systemevent.ActionOAuthRefreshAutomaticSucceeded
	recovery.Outcome = systemevent.OutcomeSuccess
	recovery.Severity = systemevent.SeverityInfo
	state, decision, err = Evaluate(rule, state, recovery, now.Add(361*time.Second))
	if err != nil || decision != DecisionRecover || state.Phase != StatePhaseIdle || state.WindowMatchCount != 0 || state.CooldownUntil != nil || state.LastRecoveredAt == nil {
		t.Fatalf("recovery state=%+v decision=%q err=%v", state, decision, err)
	}
	state, decision, err = Evaluate(rule, state, recovery, now.Add(362*time.Second))
	if err != nil || decision != DecisionNone {
		t.Fatalf("idle recovery state=%+v decision=%q err=%v", state, decision, err)
	}
}

func TestEvaluateResetsExpiredAggregationWindowAndHandlesSilentRecovery(t *testing.T) {
	rule := validRule()
	rule.ID = 10
	rule.NotifyRecovery = false
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	event := triggerEvent()
	state := RuleState{RuleID: rule.ID, DeduplicationKeyHash: rule.DeduplicationKeyHash(event), Phase: StatePhaseIdle}
	state, _, _ = Evaluate(rule, state, event, now)
	state, decision, err := Evaluate(rule, state, event, now.Add(61*time.Second))
	if err != nil || decision != DecisionNone || state.WindowMatchCount != 1 || state.WindowStartedAt == nil || !state.WindowStartedAt.Equal(now.Add(61*time.Second)) {
		t.Fatalf("expired window state=%+v decision=%q err=%v", state, decision, err)
	}
	state, decision, _ = Evaluate(rule, state, event, now.Add(62*time.Second))
	if decision != DecisionNotify || state.Phase != StatePhaseFiring {
		t.Fatalf("second window state=%+v decision=%q", state, decision)
	}
	recovery := event
	recovery.Action = systemevent.ActionOAuthRefreshAutomaticSucceeded
	state, decision, err = Evaluate(rule, state, recovery, now.Add(63*time.Second))
	if err != nil || decision != DecisionNone || state.Phase != StatePhaseIdle {
		t.Fatalf("silent recovery state=%+v decision=%q err=%v", state, decision, err)
	}
}

func TestRuleScopeDeduplicatesDifferentTargetsTogether(t *testing.T) {
	rule := validRule()
	rule.ID = 11
	rule.DeduplicationScope = DeduplicationScopeRule
	first := triggerEvent()
	second := triggerEvent()
	second.Target.ID = "other-account"
	if rule.DeduplicationKeyHash(first) != rule.DeduplicationKeyHash(second) {
		t.Fatal("rule scope produced different hashes")
	}
	otherRule := rule
	otherRule.ID++
	if rule.DeduplicationKeyHash(first) == otherRule.DeduplicationKeyHash(first) {
		t.Fatal("different rules produced identical rule-scoped hashes")
	}
	rule.DeduplicationScope = DeduplicationScopeTarget
	if rule.DeduplicationKeyHash(first) == rule.DeduplicationKeyHash(second) {
		t.Fatal("target scope produced identical hashes")
	}
}

func TestTargetScopeUsesUnambiguousLengthPrefixedComponents(t *testing.T) {
	rule := validRule()
	rule.ID = 12
	first := triggerEvent()
	first.Target.Type = "provider\x00account"
	first.Target.ID = "42"
	second := triggerEvent()
	second.Target.Type = "provider"
	second.Target.ID = "account\x0042"
	if rule.DeduplicationKeyHash(first) == rule.DeduplicationKeyHash(second) {
		t.Fatal("length-ambiguous targets produced identical hashes")
	}
}

func triggerEvent() systemevent.Event {
	return systemevent.Event{
		ID: 1, OccurredAt: time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC),
		Category: systemevent.CategoryOAuth, Severity: systemevent.SeverityWarning,
		Action: systemevent.ActionOAuthRefreshAutomaticFailed, Outcome: systemevent.OutcomeFailure,
		Actor:         systemevent.Actor{Type: systemevent.ActorSystem},
		Target:        systemevent.Target{Type: "provider_account", ID: "42"},
		CorrelationID: "alerting-test-correlation", Metadata: map[string]any{},
	}
}
