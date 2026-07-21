package alerting

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"strconv"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

func (rule Rule) MatchesTrigger(event systemevent.Event) (bool, error) {
	if err := rule.validate(); err != nil {
		return false, err
	}
	if err := systemevent.ValidateEvent(event); err != nil {
		return false, ErrInvalidInput
	}
	if systemevent.IsAlertDeliveryInternalAction(event.Action) {
		return false, nil
	}
	if !rule.Enabled {
		return false, nil
	}
	return (rule.Category == "" || rule.Category == event.Category) &&
		(rule.Severity == "" || rule.Severity == event.Severity) &&
		(rule.EventAction == "" || rule.EventAction == event.Action), nil
}

func (rule Rule) MatchesRecovery(event systemevent.Event) (bool, error) {
	if err := rule.validate(); err != nil {
		return false, err
	}
	if err := systemevent.ValidateEvent(event); err != nil {
		return false, ErrInvalidInput
	}
	if systemevent.IsAlertDeliveryInternalAction(event.Action) {
		return false, nil
	}
	if !rule.Enabled {
		return false, nil
	}
	return rule.RecoveryAction != "" && rule.RecoveryAction == event.Action, nil
}

func (rule Rule) DeduplicationKeyHash(event systemevent.Event) string {
	digest := sha256.New()
	writeComponent := func(value string) {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		_, _ = digest.Write(length[:])
		_, _ = digest.Write([]byte(value))
	}
	writeComponent("n2api-alert-deduplication-v1")
	writeComponent(strconv.FormatInt(rule.ID, 10))
	writeComponent(string(rule.DeduplicationScope))
	if rule.DeduplicationScope == DeduplicationScopeTarget {
		writeComponent(event.Target.Type)
		writeComponent(event.Target.ID)
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func Evaluate(rule Rule, state RuleState, event systemevent.Event, now time.Time) (RuleState, Decision, error) {
	if err := rule.validate(); err != nil || rule.ID <= 0 || now.IsZero() {
		return state, DecisionNone, ErrInvalidInput
	}
	if state.RuleID != rule.ID || state.DeduplicationKeyHash != rule.DeduplicationKeyHash(event) ||
		(state.Phase != StatePhaseIdle && state.Phase != StatePhaseFiring) {
		return state, DecisionNone, ErrInvalidInput
	}
	now = now.UTC()

	recovery, err := rule.MatchesRecovery(event)
	if err != nil {
		return state, DecisionNone, err
	}
	if recovery {
		if state.Phase != StatePhaseFiring {
			return state, DecisionNone, nil
		}
		state.Phase = StatePhaseIdle
		state.WindowStartedAt = nil
		state.WindowMatchCount = 0
		state.CooldownUntil = nil
		state.LastRecoveredAt = timePointer(now)
		state.UpdatedAt = now
		if rule.NotifyRecovery {
			return state, DecisionRecover, nil
		}
		return state, DecisionNone, nil
	}

	matched, err := rule.MatchesTrigger(event)
	if err != nil {
		return state, DecisionNone, err
	}
	if !matched {
		return state, DecisionNone, nil
	}
	state.LastMatchedAt = timePointer(now)
	state.UpdatedAt = now

	if state.Phase == StatePhaseFiring {
		if state.CooldownUntil != nil && now.Before(*state.CooldownUntil) {
			return state, DecisionSuppress, nil
		}
		markNotified(&state, now, rule.CooldownSeconds)
		return state, DecisionNotify, nil
	}

	if state.WindowStartedAt == nil || aggregationWindowExpired(state, rule, now) {
		state.WindowStartedAt = timePointer(now)
		state.WindowMatchCount = 1
	} else {
		state.WindowMatchCount++
	}
	if state.WindowMatchCount < rule.AggregationCount {
		return state, DecisionNone, nil
	}

	state.Phase = StatePhaseFiring
	state.WindowStartedAt = nil
	state.WindowMatchCount = 0
	markNotified(&state, now, rule.CooldownSeconds)
	return state, DecisionNotify, nil
}

func aggregationWindowExpired(state RuleState, rule Rule, now time.Time) bool {
	if state.WindowStartedAt == nil {
		return true
	}
	if rule.AggregationWindowSeconds == 0 {
		return false
	}
	deadline := state.WindowStartedAt.Add(time.Duration(rule.AggregationWindowSeconds) * time.Second)
	return now.After(deadline)
}

func markNotified(state *RuleState, now time.Time, cooldownSeconds int) {
	state.LastNotifiedAt = timePointer(now)
	state.CooldownUntil = timePointer(now.Add(time.Duration(cooldownSeconds) * time.Second))
	state.UpdatedAt = now
}

func timePointer(value time.Time) *time.Time {
	value = value.UTC()
	return &value
}
