package admin

import (
	"context"
	"time"
)

type APIKeyBudgetState struct {
	ClientKeyID                 int64      `json:"clientKeyId"`
	InitializationStatus        string     `json:"initializationStatus"`
	RequestsUsed24h             int64      `json:"requestsUsed24h"`
	RequestsUsed30d             int64      `json:"requestsUsed30d"`
	ObservedTokensUsed24h       int64      `json:"observedTokensUsed24h"`
	ObservedTokensUsed30d       int64      `json:"observedTokensUsed30d"`
	ObservedCostMicrousdUsed24h int64      `json:"observedCostMicrousdUsed24h"`
	ObservedCostMicrousdUsed30d int64      `json:"observedCostMicrousdUsed30d"`
	InitializationAttempts      int        `json:"initializationAttempts"`
	InitializedAt               *time.Time `json:"initializedAt"`
	LastMaintenanceAt           *time.Time `json:"lastMaintenanceAt"`
	Version                     int64      `json:"version"`
	UpdatedAt                   time.Time  `json:"updatedAt"`
}

type APIKeyBudgetSettlementRequest struct {
	AdmissionID          string
	ObservedTokens       int64
	ObservedCostMicrousd int64
	UsageKnown           bool
	SettledAt            time.Time
}

type APIKeyBudgetExpiryCycleResult struct {
	Processed         int
	Expired24h        int
	Expired30d        int
	LastMaintenanceAt *time.Time
}

type APIKeyBudgetSettlementCycleResult struct {
	Processed int
}

type APIKeyBudgetAbandonedCycleResult struct {
	Processed int
}

type APIKeyBudgetInitializationCycleResult struct {
	ClientKeyID int64
	Processed   int
	Ready       bool
	HasPending  bool
}

// APIKeyBudgetLedgerStore is intentionally separate from the broad admin repository.
type APIKeyBudgetLedgerStore interface {
	APIKeyBudgetAdmissionStore
	GetAPIKeyBudgetState(ctx context.Context, clientKeyID int64) (APIKeyBudgetState, error)
	RunAPIKeyBudgetSettlementCycle(ctx context.Context, now time.Time, batchSize int) (APIKeyBudgetSettlementCycleResult, error)
	RunAPIKeyBudgetExpiryCycle(ctx context.Context, now time.Time, batchSize int) (APIKeyBudgetExpiryCycleResult, error)
	RunAPIKeyBudgetAbandonedCycle(ctx context.Context, cutoff, now time.Time, batchSize int) (APIKeyBudgetAbandonedCycleResult, error)
	RunAPIKeyBudgetInitializationCycle(ctx context.Context, now time.Time, batchSize int) (APIKeyBudgetInitializationCycleResult, error)
}
