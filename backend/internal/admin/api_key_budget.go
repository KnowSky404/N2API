package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

var (
	ErrBudgetInitializing          = errors.New("budget initializing")
	ErrAPIKeyRequestBudgetExceeded = errors.New("api key request budget exceeded")
	ErrAPIKeyTokenBudgetExceeded   = errors.New("api key token budget exceeded")
	ErrAPIKeyCostBudgetExceeded    = errors.New("api key cost budget exceeded")
)

const (
	APIKeyBudgetInitializationPending = "pending"
	APIKeyBudgetInitializationReady   = "ready"
)

type APIKeyBudgetAdmission struct {
	ID         string
	KeyID      int64
	AdmittedAt time.Time
}

type APIKeyBudgetSettlement struct {
	AdmissionID          string
	AlreadySettled       bool
	ObservedTokens       int64
	ObservedCostMicrousd int64
	Outcome              string
}

type APIKeyBudgetAdmissionStore interface {
	AdmitAPIKeyBudget(ctx context.Context, keyID int64, admissionID string, admittedAt time.Time) (APIKeyBudgetAdmission, error)
	SettleAPIKeyBudgetUsage(ctx context.Context, request APIKeyBudgetSettlementRequest) (APIKeyBudgetSettlement, error)
}

func (s *Service) AdmitAPIKeyBudget(ctx context.Context, keyID int64, admittedAt time.Time) (APIKeyBudgetAdmission, error) {
	if keyID <= 0 || admittedAt.IsZero() {
		return APIKeyBudgetAdmission{}, ErrInvalidInput
	}
	if s == nil || s.budgetLedger == nil {
		return APIKeyBudgetAdmission{}, errors.New("api key budget ledger is not configured")
	}
	admissionID, err := newAPIKeyBudgetAdmissionID()
	if err != nil {
		return APIKeyBudgetAdmission{}, err
	}
	return s.budgetLedger.AdmitAPIKeyBudget(ctx, keyID, admissionID, admittedAt.UTC())
}

func (s *Service) SettleAPIKeyBudget(ctx context.Context, admissionID string, observedTokens, observedCostMicrousd int64, settledAt time.Time) (APIKeyBudgetSettlement, error) {
	return s.SettleAPIKeyBudgetUsage(ctx, APIKeyBudgetSettlementRequest{
		AdmissionID:          admissionID,
		ObservedTokens:       observedTokens,
		ObservedCostMicrousd: observedCostMicrousd,
		UsageKnown:           true,
		SettledAt:            settledAt,
	})
}

func (s *Service) SettleAPIKeyBudgetUsage(ctx context.Context, request APIKeyBudgetSettlementRequest) (APIKeyBudgetSettlement, error) {
	request.AdmissionID = strings.TrimSpace(request.AdmissionID)
	if request.AdmissionID == "" || request.ObservedTokens < 0 || request.ObservedCostMicrousd < 0 || request.SettledAt.IsZero() {
		return APIKeyBudgetSettlement{}, ErrInvalidInput
	}
	if s == nil || s.budgetLedger == nil {
		return APIKeyBudgetSettlement{}, errors.New("api key budget ledger is not configured")
	}
	request.SettledAt = request.SettledAt.UTC()
	if !request.UsageKnown {
		request.ObservedTokens = 0
		request.ObservedCostMicrousd = 0
	}
	return s.budgetLedger.SettleAPIKeyBudgetUsage(ctx, request)
}

func newAPIKeyBudgetAdmissionID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}
