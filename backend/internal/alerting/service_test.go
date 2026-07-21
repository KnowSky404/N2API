package alerting

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/secret"
	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

type memoryRepository struct {
	actions              map[int64]Action
	destinations         map[int64]string
	rules                map[int64]Rule
	states               map[string]RuleState
	nextActionID         int64
	nextRuleID           int64
	lastActionCreate     ActionCreate
	lastActionUpdate     ActionUpdate
	lastRuleCreate       RuleCreate
	lastRuleUpdate       RuleUpdate
	err                  error
	stateCapacityReached bool
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{
		actions: make(map[int64]Action), destinations: make(map[int64]string), rules: make(map[int64]Rule), states: make(map[string]RuleState),
	}
}

func (r *memoryRepository) CreateAction(_ context.Context, input ActionCreate) (Action, error) {
	if r.err != nil {
		return Action{}, r.err
	}
	r.nextActionID++
	r.lastActionCreate = input
	action := Action{ID: r.nextActionID, Name: input.Name, Kind: input.Kind, Enabled: input.Enabled, DestinationConfigured: input.EncryptedDestination != "", CreatedAt: time.Unix(1, 0).UTC(), UpdatedAt: time.Unix(1, 0).UTC()}
	r.actions[action.ID] = action
	r.destinations[action.ID] = input.EncryptedDestination
	return action, nil
}

func (r *memoryRepository) UpdateAction(_ context.Context, id int64, input ActionUpdate) (Action, error) {
	if r.err != nil {
		return Action{}, r.err
	}
	action, ok := r.actions[id]
	if !ok {
		return Action{}, ErrNotFound
	}
	r.lastActionUpdate = input
	action.Name, action.Kind, action.Enabled = input.Name, input.Kind, input.Enabled
	if input.EncryptedDestination != nil {
		r.destinations[id] = *input.EncryptedDestination
	}
	action.DestinationConfigured = r.destinations[id] != ""
	action.UpdatedAt = time.Unix(2, 0).UTC()
	r.actions[id] = action
	return action, nil
}

func (r *memoryRepository) DeleteAction(_ context.Context, id int64) error {
	if r.err != nil {
		return r.err
	}
	if _, ok := r.actions[id]; !ok {
		return ErrNotFound
	}
	delete(r.actions, id)
	delete(r.destinations, id)
	return nil
}

func (r *memoryRepository) GetAction(_ context.Context, id int64) (Action, error) {
	action, ok := r.actions[id]
	if !ok {
		return Action{}, ErrNotFound
	}
	return action, nil
}

func (r *memoryRepository) ListActions(context.Context) ([]Action, error) {
	result := make([]Action, 0, len(r.actions))
	for _, action := range r.actions {
		result = append(result, action)
	}
	return result, r.err
}

func (r *memoryRepository) GetEncryptedDestination(_ context.Context, id int64) (string, error) {
	destination, ok := r.destinations[id]
	if !ok {
		return "", ErrNotFound
	}
	return destination, nil
}

func (r *memoryRepository) CreateRule(_ context.Context, input RuleCreate) (Rule, error) {
	if r.err != nil {
		return Rule{}, r.err
	}
	r.nextRuleID++
	r.lastRuleCreate = input
	rule := input.Rule
	rule.ID = r.nextRuleID
	rule.CreatedAt, rule.UpdatedAt = time.Unix(1, 0).UTC(), time.Unix(1, 0).UTC()
	r.rules[rule.ID] = rule
	return rule, nil
}

func (r *memoryRepository) UpdateRule(_ context.Context, id int64, input RuleUpdate) (Rule, error) {
	if r.err != nil {
		return Rule{}, r.err
	}
	if _, ok := r.rules[id]; !ok {
		return Rule{}, ErrNotFound
	}
	r.lastRuleUpdate = input
	rule := input.Rule
	rule.ID = id
	rule.UpdatedAt = time.Unix(2, 0).UTC()
	r.rules[id] = rule
	for key, state := range r.states {
		if state.RuleID == id {
			delete(r.states, key)
		}
	}
	return rule, nil
}

func (r *memoryRepository) DeleteRule(_ context.Context, id int64) error {
	if _, ok := r.rules[id]; !ok {
		return ErrNotFound
	}
	delete(r.rules, id)
	return r.err
}

func (r *memoryRepository) GetRule(_ context.Context, id int64) (Rule, error) {
	rule, ok := r.rules[id]
	if !ok {
		return Rule{}, ErrNotFound
	}
	return rule, nil
}

func (r *memoryRepository) ListRules(context.Context) ([]Rule, error) {
	result := make([]Rule, 0, len(r.rules))
	for _, rule := range r.rules {
		result = append(result, rule)
	}
	return result, r.err
}

func (r *memoryRepository) GetRuleState(_ context.Context, ruleID int64, hash string) (RuleState, error) {
	state, ok := r.states[stateMapKey(ruleID, hash)]
	if !ok {
		return RuleState{}, ErrNotFound
	}
	return state, nil
}

func (r *memoryRepository) SaveRuleState(_ context.Context, state RuleState) error {
	if r.stateCapacityReached {
		return ErrStateCapacity
	}
	r.states[stateMapKey(state.RuleID, state.DeduplicationKeyHash)] = state
	return r.err
}

func (r *memoryRepository) EvaluateRuleEvent(ctx context.Context, ruleID int64, event systemevent.Event, now time.Time) (RuleState, Decision, error) {
	rule, ok := r.rules[ruleID]
	if !ok {
		return RuleState{}, DecisionNone, ErrNotFound
	}
	hash := rule.DeduplicationKeyHash(event)
	state, ok := r.states[stateMapKey(ruleID, hash)]
	if !ok {
		state = RuleState{RuleID: ruleID, DeduplicationKeyHash: hash, Phase: StatePhaseIdle}
	}
	next, decision, err := Evaluate(rule, state, event, now)
	if err != nil {
		return RuleState{}, DecisionNone, err
	}
	if next != state {
		if err := r.SaveRuleState(ctx, next); err != nil {
			return RuleState{}, DecisionNone, err
		}
	}
	return next, decision, nil
}

func TestServiceEncryptsAndRedactsActionDestinations(t *testing.T) {
	repo := newMemoryRepository()
	service := NewService(repo, testKeyring(t))
	action, err := service.CreateAction(context.Background(), ActionInput{
		Name: "primary webhook", Kind: ActionKindGenericWebhook, Destination: "https://hooks.example.test/path?token=destination-canary", Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateAction returned error: %v", err)
	}
	if action.ID == 0 || !action.DestinationConfigured || strings.Contains(repo.lastActionCreate.EncryptedDestination, "destination-canary") {
		t.Fatalf("created action = %+v encrypted = %q", action, repo.lastActionCreate.EncryptedDestination)
	}
	if !strings.HasPrefix(repo.lastActionCreate.EncryptedDestination, "n2api:v1:current:alert-action-destination:") {
		t.Fatalf("encrypted destination = %q, want alert destination envelope", repo.lastActionCreate.EncryptedDestination)
	}
	encoded, err := json.Marshal(action)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	for _, forbidden := range []string{"destination-canary", "hooks.example.test", "encryptedDestination"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("action JSON leaked %q: %s", forbidden, encoded)
		}
	}
	repositoryPayload, err := json.Marshal(repo.lastActionCreate)
	if err != nil {
		t.Fatalf("Marshal repository payload returned error: %v", err)
	}
	if strings.Contains(string(repositoryPayload), "EncryptedDestination") || strings.Contains(string(repositoryPayload), repo.lastActionCreate.EncryptedDestination) {
		t.Fatalf("repository payload JSON leaked encrypted destination: %s", repositoryPayload)
	}

	destination, err := service.DestinationForDelivery(context.Background(), action.ID)
	if err != nil {
		t.Fatalf("DestinationForDelivery returned error: %v", err)
	}
	if destination != "https://hooks.example.test/path?token=destination-canary" {
		t.Fatalf("destination = %q", destination)
	}
}

func TestServiceValidatesGenericWebhookAndNtfyDestinations(t *testing.T) {
	service := NewService(newMemoryRepository(), testKeyring(t))
	valid := []ActionInput{
		{Name: "webhook", Kind: ActionKindGenericWebhook, Destination: "https://hooks.example.test/notify?key=secret"},
		{Name: "local webhook", Kind: ActionKindGenericWebhook, Destination: "http://127.0.0.1:8080/hook"},
		{Name: "ntfy", Kind: ActionKindNtfy, Destination: "https://ntfy.example.test/N2API_alerts?auth=secret"},
	}
	for _, input := range valid {
		if _, err := service.CreateAction(context.Background(), input); err != nil {
			t.Fatalf("CreateAction(%+v) returned error: %v", input, err)
		}
	}

	invalid := []ActionInput{
		{Name: "", Kind: ActionKindGenericWebhook, Destination: "https://example.test"},
		{Name: "bad kind", Kind: "telegram", Destination: "https://example.test"},
		{Name: "relative", Kind: ActionKindGenericWebhook, Destination: "/hook"},
		{Name: "userinfo", Kind: ActionKindGenericWebhook, Destination: "https://user:pass@example.test/hook"},
		{Name: "fragment", Kind: ActionKindGenericWebhook, Destination: "https://example.test/hook#secret"},
		{Name: "ftp", Kind: ActionKindGenericWebhook, Destination: "ftp://example.test/hook"},
		{Name: "public http", Kind: ActionKindGenericWebhook, Destination: "http://example.test/hook"},
		{Name: "ntfy missing topic", Kind: ActionKindNtfy, Destination: "https://ntfy.example.test"},
		{Name: "ntfy nested", Kind: ActionKindNtfy, Destination: "https://ntfy.example.test/base/topic"},
		{Name: "ntfy invalid topic", Kind: ActionKindNtfy, Destination: "https://ntfy.example.test/bad.topic"},
		{Name: "control", Kind: ActionKindGenericWebhook, Destination: "https://example.test/hook\nsecret"},
		{Name: "too long", Kind: ActionKindGenericWebhook, Destination: "https://example.test/" + strings.Repeat("x", MaxActionDestinationLength)},
	}
	for _, input := range invalid {
		t.Run(input.Name, func(t *testing.T) {
			_, err := service.CreateAction(context.Background(), input)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("CreateAction error = %v, want ErrInvalidInput", err)
			}
			if err != nil && strings.Contains(err.Error(), input.Destination) {
				t.Fatalf("error leaked destination: %v", err)
			}
		})
	}
}

func TestServiceUpdateRetainsOrReplacesEncryptedDestination(t *testing.T) {
	repo := newMemoryRepository()
	service := NewService(repo, testKeyring(t))
	action, err := service.CreateAction(context.Background(), ActionInput{Name: "webhook", Kind: ActionKindGenericWebhook, Destination: "https://one.example.test/hook", Enabled: true})
	if err != nil {
		t.Fatalf("CreateAction returned error: %v", err)
	}
	original := repo.destinations[action.ID]
	updated, err := service.UpdateAction(context.Background(), action.ID, ActionUpdateInput{Name: "renamed", Kind: ActionKindGenericWebhook, Enabled: false})
	if err != nil {
		t.Fatalf("UpdateAction retain returned error: %v", err)
	}
	if updated.Name != "renamed" || updated.Enabled || repo.destinations[action.ID] != original || repo.lastActionUpdate.EncryptedDestination != nil {
		t.Fatalf("retained update = %+v stored=%q", updated, repo.destinations[action.ID])
	}
	replacement := "https://two.example.test/new"
	_, err = service.UpdateAction(context.Background(), action.ID, ActionUpdateInput{Name: "renamed", Kind: ActionKindGenericWebhook, Destination: &replacement, Enabled: true})
	if err != nil {
		t.Fatalf("UpdateAction replace returned error: %v", err)
	}
	if repo.destinations[action.ID] == original || repo.lastActionUpdate.EncryptedDestination == nil {
		t.Fatal("UpdateAction did not replace encrypted destination")
	}
}

func TestServiceRejectsCrossKindDestinationCiphertextAndRedactsRepositoryErrors(t *testing.T) {
	repo := newMemoryRepository()
	keyring := testKeyring(t)
	service := NewService(repo, keyring)
	action, err := service.CreateAction(context.Background(), ActionInput{Name: "webhook", Kind: ActionKindGenericWebhook, Destination: "https://example.test/hook"})
	if err != nil {
		t.Fatalf("CreateAction returned error: %v", err)
	}
	repo.destinations[action.ID], err = keyring.EncryptStringFor(secret.SecretKindClientAPIKey, "https://ciphertext-canary.example.test")
	if err != nil {
		t.Fatalf("EncryptStringFor returned error: %v", err)
	}
	if _, err := service.DestinationForDelivery(context.Background(), action.ID); err == nil || strings.Contains(err.Error(), "ciphertext-canary") {
		t.Fatalf("DestinationForDelivery error = %v", err)
	}

	repo.err = errors.New("database-secret-canary")
	if _, err := service.CreateAction(context.Background(), ActionInput{Name: "other", Kind: ActionKindGenericWebhook, Destination: "https://example.test/other"}); err == nil || strings.Contains(err.Error(), "database-secret-canary") {
		t.Fatalf("CreateAction repository error = %v", err)
	}
}

func TestServiceCreatesAndValidatesRules(t *testing.T) {
	repo := newMemoryRepository()
	service := NewService(repo, testKeyring(t))
	rule := validRule()
	created, err := service.CreateRule(context.Background(), rule)
	if err != nil {
		t.Fatalf("CreateRule returned error: %v", err)
	}
	if created.ID == 0 || repo.lastRuleCreate.Rule.Name != rule.Name {
		t.Fatalf("created rule = %+v", created)
	}

	invalid := []Rule{
		{},
		func() Rule { value := validRule(); value.Name = ""; return value }(),
		func() Rule { value := validRule(); value.ActionID = 0; return value }(),
		func() Rule { value := validRule(); value.Category = "unknown"; return value }(),
		func() Rule { value := validRule(); value.Severity = "critical"; return value }(),
		func() Rule { value := validRule(); value.EventAction = "unknown.action"; return value }(),
		func() Rule {
			value := validRule()
			value.Category, value.Severity, value.EventAction = "", "", ""
			return value
		}(),
		func() Rule {
			value := validRule()
			value.AggregationCount = 2
			value.AggregationWindowSeconds = 0
			return value
		}(),
		func() Rule { value := validRule(); value.AggregationCount = MaxAggregationCount + 1; return value }(),
		func() Rule { value := validRule(); value.CooldownSeconds = MaxCooldownSeconds + 1; return value }(),
		func() Rule { value := validRule(); value.DeduplicationScope = "metadata"; return value }(),
		func() Rule {
			value := validRule()
			value.NotifyRecovery = true
			value.RecoveryAction = ""
			return value
		}(),
		func() Rule { value := validRule(); value.RecoveryAction = "unknown.action"; return value }(),
		func() Rule { value := validRule(); value.RecoveryAction = value.EventAction; return value }(),
	}
	for index, candidate := range invalid {
		if _, err := service.CreateRule(context.Background(), candidate); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid rule %d error = %v", index, err)
		}
	}
}

func TestServiceEvaluatesAndPersistsRuleEventsThroughRepository(t *testing.T) {
	repo := newMemoryRepository()
	service := NewService(repo, testKeyring(t))
	rule, err := service.CreateRule(context.Background(), validRule())
	if err != nil {
		t.Fatalf("CreateRule returned error: %v", err)
	}
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	event := triggerEvent()
	state, decision, err := service.EvaluateRuleEvent(context.Background(), rule.ID, event, now)
	if err != nil || decision != DecisionNone || state.WindowMatchCount != 1 {
		t.Fatalf("first evaluation state=%+v decision=%q err=%v", state, decision, err)
	}
	state, decision, err = service.EvaluateRuleEvent(context.Background(), rule.ID, event, now.Add(time.Second))
	if err != nil || decision != DecisionNotify || state.Phase != StatePhaseFiring {
		t.Fatalf("second evaluation state=%+v decision=%q err=%v", state, decision, err)
	}
	if _, _, err := service.EvaluateRuleEvent(context.Background(), 0, event, now); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid rule evaluation error = %v", err)
	}
}

func validRule() Rule {
	return Rule{
		Name: "oauth refresh failures", ActionID: 1, Enabled: true,
		Category: "oauth", Severity: "error", EventAction: "oauth.refresh.automatic.failed",
		RecoveryAction: "oauth.refresh.automatic.succeeded", AggregationCount: 2,
		AggregationWindowSeconds: 60, CooldownSeconds: 300,
		DeduplicationScope: DeduplicationScopeTarget, NotifyRecovery: true,
	}
}

func testKeyring(t *testing.T) *secret.Keyring {
	t.Helper()
	keyring, err := secret.NewKeyring(secret.EncryptionKey{ID: "current", Secret: "alerting-encryption-secret-at-least-32-bytes"}, nil)
	if err != nil {
		t.Fatalf("NewKeyring returned error: %v", err)
	}
	return keyring
}

func stateMapKey(ruleID int64, hash string) string {
	return strings.Join([]string{string(rune(ruleID)), hash}, ":")
}
