package alerting

import (
	"context"
	"errors"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/secret"
	"github.com/KnowSky404/N2API/backend/internal/systemevent"
)

var ntfyTopicRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type Service struct {
	repository Repository
	keyring    *secret.Keyring
}

func NewService(repository Repository, keyring *secret.Keyring) *Service {
	return &Service{repository: repository, keyring: keyring}
}

func (service *Service) CreateAction(ctx context.Context, input ActionInput) (Action, error) {
	if err := validateAction(input.Name, input.Kind, input.Destination); err != nil {
		return Action{}, err
	}
	encrypted, err := service.keyring.EncryptStringFor(secret.SecretKindAlertActionDestination, input.Destination)
	if err != nil {
		return Action{}, ErrRepository
	}
	action, err := service.repository.CreateAction(ctx, ActionCreate{
		Name: strings.TrimSpace(input.Name), Kind: input.Kind, EncryptedDestination: encrypted, Enabled: input.Enabled,
	})
	if err != nil {
		return Action{}, repositoryError(err)
	}
	return action, nil
}

func (service *Service) UpdateAction(ctx context.Context, id int64, input ActionUpdateInput) (Action, error) {
	if id <= 0 || invalidName(input.Name, MaxActionNameLength) || !validActionKind(input.Kind) {
		return Action{}, ErrInvalidInput
	}
	var encrypted *string
	if input.Destination != nil {
		if err := validateDestination(input.Kind, *input.Destination); err != nil {
			return Action{}, err
		}
		value, err := service.keyring.EncryptStringFor(secret.SecretKindAlertActionDestination, *input.Destination)
		if err != nil {
			return Action{}, ErrRepository
		}
		encrypted = &value
	} else {
		current, err := service.repository.GetAction(ctx, id)
		if err != nil {
			return Action{}, repositoryError(err)
		}
		if current.Kind != input.Kind {
			destination, err := service.DestinationForDelivery(ctx, id)
			if err != nil {
				return Action{}, err
			}
			if err := validateDestination(input.Kind, destination); err != nil {
				return Action{}, err
			}
		}
	}
	action, err := service.repository.UpdateAction(ctx, id, ActionUpdate{
		Name: strings.TrimSpace(input.Name), Kind: input.Kind, EncryptedDestination: encrypted, Enabled: input.Enabled,
	})
	if err != nil {
		return Action{}, repositoryError(err)
	}
	return action, nil
}

func (service *Service) DeleteAction(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrInvalidInput
	}
	return repositoryError(service.repository.DeleteAction(ctx, id))
}

func (service *Service) GetAction(ctx context.Context, id int64) (Action, error) {
	if id <= 0 {
		return Action{}, ErrInvalidInput
	}
	action, err := service.repository.GetAction(ctx, id)
	if err != nil {
		return Action{}, repositoryError(err)
	}
	return action, nil
}

func (service *Service) ListActions(ctx context.Context) ([]Action, error) {
	actions, err := service.repository.ListActions(ctx)
	if err != nil {
		return nil, repositoryError(err)
	}
	return actions, nil
}

func (service *Service) DestinationForDelivery(ctx context.Context, actionID int64) (string, error) {
	action, err := service.ResolveActionForDelivery(ctx, actionID)
	if err != nil {
		return "", err
	}
	return action.Destination, nil
}

func (service *Service) ResolveActionForDelivery(ctx context.Context, actionID int64) (ResolvedAction, error) {
	if actionID <= 0 {
		return ResolvedAction{}, ErrInvalidInput
	}
	stored, err := service.repository.GetActionForDelivery(ctx, actionID)
	if err != nil {
		return ResolvedAction{}, repositoryError(err)
	}
	destination, err := service.keyring.DecryptStringFor(secret.SecretKindAlertActionDestination, stored.EncryptedDestination)
	if err != nil {
		return ResolvedAction{}, ErrRepository
	}
	if err := validateDestination(stored.Kind, destination); err != nil {
		return ResolvedAction{}, ErrRepository
	}
	return ResolvedAction{ID: stored.ID, Kind: stored.Kind, Enabled: stored.Enabled, Destination: destination}, nil
}

func (service *Service) CreateRule(ctx context.Context, input Rule) (Rule, error) {
	input.ID = 0
	input.Name = strings.TrimSpace(input.Name)
	input.CreatedAt = time.Time{}
	input.UpdatedAt = time.Time{}
	if err := input.validate(); err != nil {
		return Rule{}, err
	}
	rule, err := service.repository.CreateRule(ctx, RuleCreate{Rule: input})
	if err != nil {
		return Rule{}, repositoryError(err)
	}
	return rule, nil
}

func (service *Service) UpdateRule(ctx context.Context, id int64, input Rule) (Rule, error) {
	if id <= 0 {
		return Rule{}, ErrInvalidInput
	}
	input.ID = 0
	input.Name = strings.TrimSpace(input.Name)
	input.CreatedAt = time.Time{}
	input.UpdatedAt = time.Time{}
	if err := input.validate(); err != nil {
		return Rule{}, err
	}
	rule, err := service.repository.UpdateRule(ctx, id, RuleUpdate{Rule: input})
	if err != nil {
		return Rule{}, repositoryError(err)
	}
	return rule, nil
}

func (service *Service) DeleteRule(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrInvalidInput
	}
	return repositoryError(service.repository.DeleteRule(ctx, id))
}

func (service *Service) GetRule(ctx context.Context, id int64) (Rule, error) {
	if id <= 0 {
		return Rule{}, ErrInvalidInput
	}
	rule, err := service.repository.GetRule(ctx, id)
	if err != nil {
		return Rule{}, repositoryError(err)
	}
	return rule, nil
}

func (service *Service) ListRules(ctx context.Context) ([]Rule, error) {
	rules, err := service.repository.ListRules(ctx)
	if err != nil {
		return nil, repositoryError(err)
	}
	return rules, nil
}

func (service *Service) EvaluateRuleEvent(ctx context.Context, ruleID int64, event systemevent.Event, now time.Time) (RuleState, Decision, error) {
	if err := validateEvaluationInput(ruleID, event, now); err != nil {
		return RuleState{}, DecisionNone, ErrInvalidInput
	}
	state, decision, err := service.repository.EvaluateRuleEvent(ctx, ruleID, event, now.UTC())
	if err != nil {
		return RuleState{}, DecisionNone, repositoryError(err)
	}
	return state, decision, nil
}

func (service *Service) EvaluateRuleEventForDelivery(ctx context.Context, ruleID int64, event systemevent.Event, now time.Time) (Evaluation, error) {
	if err := validateEvaluationInput(ruleID, event, now); err != nil {
		return Evaluation{}, err
	}
	evaluation, err := service.repository.EvaluateRuleEventForDelivery(ctx, ruleID, event, now.UTC())
	if err != nil {
		return Evaluation{}, repositoryError(err)
	}
	return evaluation, nil
}

func validateEvaluationInput(ruleID int64, event systemevent.Event, now time.Time) error {
	if ruleID <= 0 || now.IsZero() || systemevent.ValidateEvent(event) != nil {
		return ErrInvalidInput
	}
	return nil
}

func validateAction(name string, kind ActionKind, destination string) error {
	if invalidName(name, MaxActionNameLength) || !validActionKind(kind) {
		return ErrInvalidInput
	}
	return validateDestination(kind, destination)
}

func validActionKind(kind ActionKind) bool {
	return kind == ActionKindGenericWebhook || kind == ActionKindNtfy
}

func validateDestination(kind ActionKind, destination string) error {
	if destination == "" || len(destination) > MaxActionDestinationLength || destination != strings.TrimSpace(destination) || strings.ContainsAny(destination, "\x00\r\n") {
		return ErrInvalidInput
	}
	parsed, err := url.Parse(destination)
	if err != nil || parsed.IsAbs() == false || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return ErrInvalidInput
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && loopbackHost(parsed.Hostname())) {
		return ErrInvalidInput
	}
	if kind == ActionKindNtfy {
		topic := strings.TrimPrefix(parsed.EscapedPath(), "/")
		if strings.Contains(topic, "/") || !ntfyTopicRE.MatchString(topic) {
			return ErrInvalidInput
		}
	}
	return nil
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func repositoryError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, ErrInvalidInput):
		return ErrInvalidInput
	case errors.Is(err, ErrNotFound):
		return ErrNotFound
	case errors.Is(err, ErrStateCapacity):
		return ErrStateCapacity
	default:
		return ErrRepository
	}
}
