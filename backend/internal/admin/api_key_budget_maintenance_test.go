package admin

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
)

type fakeAPIKeyBudgetLedgerStore struct {
	initialization  []APIKeyBudgetInitializationCycleResult
	settlement      []APIKeyBudgetSettlementCycleResult
	expiry          []APIKeyBudgetExpiryCycleResult
	abandoned       []APIKeyBudgetAbandonedCycleResult
	err             error
	initCalls       int
	settlementCalls int
	expiryCalls     int
	abandonedCalls  int
}

func (s *fakeAPIKeyBudgetLedgerStore) AdmitAPIKeyBudget(context.Context, int64, string, time.Time) (APIKeyBudgetAdmission, error) {
	return APIKeyBudgetAdmission{}, nil
}

func (s *fakeAPIKeyBudgetLedgerStore) SettleAPIKeyBudgetUsage(context.Context, APIKeyBudgetSettlementRequest) (APIKeyBudgetSettlement, error) {
	return APIKeyBudgetSettlement{}, nil
}

func (s *fakeAPIKeyBudgetLedgerStore) GetAPIKeyBudgetState(context.Context, int64) (APIKeyBudgetState, error) {
	return APIKeyBudgetState{}, nil
}

func (s *fakeAPIKeyBudgetLedgerStore) RunAPIKeyBudgetSettlementCycle(context.Context, time.Time, int) (APIKeyBudgetSettlementCycleResult, error) {
	s.settlementCalls++
	if len(s.settlement) == 0 {
		return APIKeyBudgetSettlementCycleResult{}, nil
	}
	result := s.settlement[0]
	s.settlement = s.settlement[1:]
	return result, nil
}

func (s *fakeAPIKeyBudgetLedgerStore) RunAPIKeyBudgetInitializationCycle(context.Context, time.Time, int) (APIKeyBudgetInitializationCycleResult, error) {
	s.initCalls++
	if s.err != nil {
		return APIKeyBudgetInitializationCycleResult{}, s.err
	}
	if len(s.initialization) == 0 {
		return APIKeyBudgetInitializationCycleResult{}, nil
	}
	result := s.initialization[0]
	s.initialization = s.initialization[1:]
	return result, nil
}

func (s *fakeAPIKeyBudgetLedgerStore) RunAPIKeyBudgetExpiryCycle(context.Context, time.Time, int) (APIKeyBudgetExpiryCycleResult, error) {
	s.expiryCalls++
	if len(s.expiry) == 0 {
		return APIKeyBudgetExpiryCycleResult{}, nil
	}
	result := s.expiry[0]
	s.expiry = s.expiry[1:]
	return result, nil
}

func (s *fakeAPIKeyBudgetLedgerStore) RunAPIKeyBudgetAbandonedCycle(context.Context, time.Time, time.Time, int) (APIKeyBudgetAbandonedCycleResult, error) {
	s.abandonedCalls++
	if len(s.abandoned) == 0 {
		return APIKeyBudgetAbandonedCycleResult{}, nil
	}
	result := s.abandoned[0]
	s.abandoned = s.abandoned[1:]
	return result, nil
}

func TestAPIKeyBudgetMaintenanceRunsBoundedBacklogWork(t *testing.T) {
	store := &fakeAPIKeyBudgetLedgerStore{
		initialization: []APIKeyBudgetInitializationCycleResult{
			{Processed: 2, HasPending: true},
			{Processed: 1, Ready: true, HasPending: false},
		},
		expiry:     []APIKeyBudgetExpiryCycleResult{{Processed: 2}, {Processed: 1}},
		settlement: []APIKeyBudgetSettlementCycleResult{{Processed: 2}, {Processed: 1}},
		abandoned:  []APIKeyBudgetAbandonedCycleResult{{Processed: 2}, {Processed: 0}},
	}
	runner := NewAPIKeyBudgetMaintenance(store, APIKeyBudgetMaintenanceConfig{BatchSize: 2, MaxBatches: 3}, slog.Default())
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	runner.now = func() time.Time { return now }

	runner.runCycle(context.Background())

	status := runner.APIKeyBudgetMaintenanceStatus()
	if status.Running || status.LastSucceededAt == nil || status.LastErrorCode != "" || status.InitializationPending {
		t.Fatalf("status = %+v, want successful completed cycle", status)
	}
	if status.LastInitializedLogs != 3 || status.LastSettledRows != 3 || status.LastExpiredRows != 3 || status.LastAbandonedRows != 2 {
		t.Fatalf("work counts = %+v", status)
	}
	if store.initCalls != 2 || store.settlementCalls != 2 || store.expiryCalls != 2 || store.abandonedCalls != 2 {
		t.Fatalf("calls = init:%d settlement:%d expiry:%d abandoned:%d", store.initCalls, store.settlementCalls, store.expiryCalls, store.abandonedCalls)
	}
}

func TestAPIKeyBudgetMaintenanceBoundsPersistentInitializationBacklog(t *testing.T) {
	store := &fakeAPIKeyBudgetLedgerStore{initialization: []APIKeyBudgetInitializationCycleResult{
		{Processed: 1, HasPending: true}, {Processed: 1, HasPending: true}, {Processed: 1, HasPending: true},
	}}
	runner := NewAPIKeyBudgetMaintenance(store, APIKeyBudgetMaintenanceConfig{BatchSize: 1, MaxBatches: 2}, slog.Default())
	runner.runCycle(context.Background())

	status := runner.APIKeyBudgetMaintenanceStatus()
	if store.initCalls != 2 || !status.InitializationPending || status.LastInitializedLogs != 2 {
		t.Fatalf("calls/status = %d/%+v, want bounded pending work", store.initCalls, status)
	}
}

func TestAPIKeyBudgetMaintenanceSanitizesFailure(t *testing.T) {
	var output bytes.Buffer
	store := &fakeAPIKeyBudgetLedgerStore{err: errors.New("postgres raw secret detail")}
	runner := NewAPIKeyBudgetMaintenance(store, APIKeyBudgetMaintenanceConfig{}, slog.New(slog.NewJSONHandler(&output, nil)))
	runner.runCycle(context.Background())

	status := runner.APIKeyBudgetMaintenanceStatus()
	if status.LastErrorCode != "api_key_budget_maintenance_failed" || status.LastErrorAt == nil {
		t.Fatalf("status = %+v", status)
	}
	if bytes.Contains(output.Bytes(), []byte("postgres raw secret detail")) {
		t.Fatalf("log leaked raw error: %s", output.String())
	}
}
