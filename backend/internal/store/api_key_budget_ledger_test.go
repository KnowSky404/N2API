package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAPIKeyBudgetLedgerNewKeyStartsReady(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	key := createBudgetLedgerTestKey(t, ctx, repo, "new-key", 0, 0, 0)

	state, err := repo.GetAPIKeyBudgetState(ctx, key.ID)
	if err != nil {
		t.Fatalf("GetAPIKeyBudgetState returned error: %v", err)
	}
	if state.InitializationStatus != admin.APIKeyBudgetInitializationReady || state.InitializedAt == nil {
		t.Fatalf("new key state = %+v, want ready with initializedAt", state)
	}
}

func TestAPIKeyBudgetLedgerPendingAdmissionFailsClosed(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	key := createBudgetLedgerTestKey(t, ctx, repo, "pending", 10, 0, 0)
	if _, err := repo.pool.Exec(ctx, `
		UPDATE api_key_budget_states
		SET initialization_status = 'pending', initialized_at = NULL,
			initialization_window_start = now() - INTERVAL '30 days',
			initialization_window_end = now()
		WHERE client_key_id = $1
	`, key.ID); err != nil {
		t.Fatalf("mark state pending: %v", err)
	}

	_, err := repo.AdmitAPIKeyBudget(ctx, key.ID, budgetLedgerAdmissionID(1), time.Now())
	if !errors.Is(err, admin.ErrBudgetInitializing) {
		t.Fatalf("AdmitAPIKeyBudget error = %v, want ErrBudgetInitializing", err)
	}
	if _, err := repo.UpdateAPIKeyBudgets(ctx, key.ID, 0, 0, 0, 0, 0, 0); err != nil {
		t.Fatalf("disable budgets: %v", err)
	}
	admission, err := repo.AdmitAPIKeyBudget(ctx, key.ID, budgetLedgerAdmissionID(2), time.Now())
	if err != nil {
		t.Fatalf("unbudgeted pending admission returned error: %v", err)
	}
	if admission.ID == "" || admission.KeyID != key.ID {
		t.Fatalf("unbudgeted admission = %+v, want tracked admission", admission)
	}
}

func TestAPIKeyBudgetLedgerBudgetEnableBackfillsOnlyPostAdmissionHistory(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	key, err := repo.CreateAPIKey(ctx, "budget enable", "budget-enable-hash", "n2api_", "encrypted", nil)
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}
	base := time.Now().UTC().Add(-2 * time.Hour)
	if _, err := repo.pool.Exec(ctx, `UPDATE client_api_keys SET created_at = $2 WHERE id = $1`, key.ID, base); err != nil {
		t.Fatalf("prepare unbudgeted history window: %v", err)
	}
	insertBudgetLedgerRequestLogWithEligibility(t, ctx, repo, key.ID, "executed", base.Add(time.Hour), 10, 100, "parsed", true, "")
	for index, errorCode := range []string{
		"budget_initializing",
		"api_key_request_rate_limited",
		"gateway_concurrency_limited",
		"api_key_concurrency_limited",
		"invalid_request_body",
	} {
		insertBudgetLedgerRequestLogWithEligibility(
			t, ctx, repo, key.ID, fmt.Sprintf("pre-admission-%d", index),
			base.Add(time.Hour+time.Duration(index+1)*time.Minute), 999, 999, "parsed", false, errorCode,
		)
	}
	if _, err := repo.UpdateAPIKeyBudgets(ctx, key.ID, 10, 1000, 1000, 10, 1000, 1000); err != nil {
		t.Fatalf("enable budgets: %v", err)
	}
	if _, err := repo.AdmitAPIKeyBudget(ctx, key.ID, budgetLedgerAdmissionID(50), time.Now()); !errors.Is(err, admin.ErrBudgetInitializing) {
		t.Fatalf("admission during enable initialization error = %v, want ErrBudgetInitializing", err)
	}
	result, err := repo.RunAPIKeyBudgetInitializationCycle(ctx, time.Now().Add(time.Minute), 100)
	if err != nil {
		t.Fatalf("RunAPIKeyBudgetInitializationCycle returned error: %v", err)
	}
	if result.ClientKeyID != key.ID || result.Processed != 1 || !result.Ready {
		t.Fatalf("initialization result = %+v, want one executed request", result)
	}
	state, err := repo.GetAPIKeyBudgetState(ctx, key.ID)
	if err != nil {
		t.Fatalf("GetAPIKeyBudgetState returned error: %v", err)
	}
	if state.RequestsUsed24h != 1 || state.RequestsUsed30d != 1 || state.ObservedTokensUsed24h != 10 || state.ObservedCostMicrousdUsed24h != 100 {
		t.Fatalf("enabled budget state = %+v, want only executed history", state)
	}

	if _, err := repo.UpdateAPIKeyBudgets(ctx, key.ID, 0, 0, 0, 0, 0, 0); err != nil {
		t.Fatalf("disable budgets: %v", err)
	}
	disabledAdmissionID := budgetLedgerAdmissionID(51)
	disabledAt := time.Now().UTC()
	if _, err := repo.AdmitAPIKeyBudget(ctx, key.ID, disabledAdmissionID, disabledAt); err != nil {
		t.Fatalf("admit while budgets disabled: %v", err)
	}
	if _, err := repo.SettleAPIKeyBudget(ctx, disabledAdmissionID, 20, 200, disabledAt.Add(time.Second)); err != nil {
		t.Fatalf("settle while budgets disabled: %v", err)
	}
	if _, err := repo.UpdateAPIKeyBudgets(ctx, key.ID, 10, 1000, 1000, 10, 1000, 1000); err != nil {
		t.Fatalf("re-enable budgets: %v", err)
	}
	state, err = repo.GetAPIKeyBudgetState(ctx, key.ID)
	if err != nil {
		t.Fatalf("GetAPIKeyBudgetState after re-enable returned error: %v", err)
	}
	if state.RequestsUsed24h != 2 || state.ObservedTokensUsed24h != 30 || state.ObservedCostMicrousdUsed24h != 300 {
		t.Fatalf("re-enabled budget state = %+v, want continuously tracked disabled-window usage", state)
	}
}

func TestAPIKeyBudgetLedgerStrictConcurrentRequestLimit(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	key := createBudgetLedgerTestKey(t, ctx, repo, "concurrent", 10, 0, 0)
	admittedAt := time.Date(2026, time.July, 29, 10, 0, 0, 0, time.UTC)

	var admitted atomic.Int64
	var rejected atomic.Int64
	errorsCh := make(chan error, 100)
	var wait sync.WaitGroup
	for i := 0; i < 100; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			_, err := repo.AdmitAPIKeyBudget(ctx, key.ID, budgetLedgerAdmissionID(index+1), admittedAt)
			switch {
			case err == nil:
				admitted.Add(1)
			case errors.Is(err, admin.ErrAPIKeyRequestBudgetExceeded):
				rejected.Add(1)
			default:
				errorsCh <- err
			}
		}(i)
	}
	wait.Wait()
	close(errorsCh)
	for err := range errorsCh {
		t.Errorf("concurrent admission returned unexpected error: %v", err)
	}
	if got := admitted.Load(); got != 10 {
		t.Fatalf("admitted = %d, want 10", got)
	}
	if got := rejected.Load(); got != 90 {
		t.Fatalf("rejected = %d, want 90", got)
	}
	state, err := repo.GetAPIKeyBudgetState(ctx, key.ID)
	if err != nil {
		t.Fatalf("GetAPIKeyBudgetState returned error: %v", err)
	}
	if state.RequestsUsed24h != 10 || state.RequestsUsed30d != 10 {
		t.Fatalf("request counters = (%d, %d), want (10, 10)", state.RequestsUsed24h, state.RequestsUsed30d)
	}
}

func TestAPIKeyBudgetLedgerAdmissionAndUsageDoNotDependOnRequestLogs(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	key := createBudgetLedgerTestKey(t, ctx, repo, "request-log-independent", 1, 0, 0)
	lockTx, err := repo.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin request log lock transaction: %v", err)
	}
	defer lockTx.Rollback(context.Background())
	if _, err := lockTx.Exec(ctx, `LOCK TABLE request_logs IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatalf("lock request logs: %v", err)
	}

	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, err := repo.AdmitAPIKeyBudget(queryCtx, key.ID, budgetLedgerAdmissionID(1), time.Now().UTC()); err != nil {
		t.Fatalf("admission while request logs are locked: %v", err)
	}
	usage, err := repo.GetAPIKeyBudgetUsage(queryCtx, key.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("usage while request logs are locked: %v", err)
	}
	if usage.RequestsUsed24h != 1 || usage.RequestsUsed30d != 1 {
		t.Fatalf("usage while request logs are locked = %+v, want admitted request", usage)
	}
}

func TestAPIKeyBudgetLedgerStrictLimitAcrossProcesses(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	key := createBudgetLedgerTestKey(t, ctx, repo, "processes", 10, 0, 0)
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve store test executable: %v", err)
	}
	resultDir := t.TempDir()
	processCtx, cancelProcesses := context.WithTimeout(ctx, 15*time.Second)
	defer cancelProcesses()
	type child struct {
		command *exec.Cmd
		output  bytes.Buffer
		path    string
	}
	children := make([]child, 2)
	for index := range children {
		resultPath := filepath.Join(resultDir, fmt.Sprintf("worker-%d.json", index))
		command := exec.CommandContext(processCtx, executable, "-test.run=^TestAPIKeyBudgetLedgerProcessWorker$", "-test.count=1")
		command.Env = append(os.Environ(),
			"N2API_BUDGET_PROCESS_WORKER=1",
			"N2API_BUDGET_PROCESS_KEY_ID="+strconv.FormatInt(key.ID, 10),
			"N2API_BUDGET_PROCESS_OFFSET="+strconv.Itoa((index+1)*1000),
			"N2API_BUDGET_PROCESS_RESULT="+resultPath,
		)
		children[index] = child{command: command, path: resultPath}
		command.Stdout = &children[index].output
		command.Stderr = &children[index].output
		if err := command.Start(); err != nil {
			t.Fatalf("start budget process worker %d: %v", index, err)
		}
	}
	for index := range children {
		if err := children[index].command.Wait(); err != nil {
			t.Fatalf("budget process worker %d: %v\n%s", index, err, children[index].output.String())
		}
	}
	var total budgetProcessResult
	for index := range children {
		encoded, err := os.ReadFile(children[index].path)
		if err != nil {
			t.Fatalf("read budget process worker %d result: %v", index, err)
		}
		var result budgetProcessResult
		if err := json.Unmarshal(encoded, &result); err != nil {
			t.Fatalf("decode budget process worker %d result: %v", index, err)
		}
		total.Admitted += result.Admitted
		total.Rejected += result.Rejected
	}
	if total.Admitted != 10 || total.Rejected != 90 {
		t.Fatalf("cross-process result = %+v, want admitted 10 and rejected 90", total)
	}
}

type budgetProcessResult struct {
	Admitted int `json:"admitted"`
	Rejected int `json:"rejected"`
}

func TestAPIKeyBudgetLedgerProcessWorker(t *testing.T) {
	if os.Getenv("N2API_BUDGET_PROCESS_WORKER") != "1" {
		t.Skip("budget process worker only")
	}
	databaseURL := os.Getenv("N2API_STORE_TEST_DATABASE_URL")
	keyID, err := strconv.ParseInt(os.Getenv("N2API_BUDGET_PROCESS_KEY_ID"), 10, 64)
	if err != nil {
		t.Fatalf("parse process key ID: %v", err)
	}
	offset, err := strconv.Atoi(os.Getenv("N2API_BUDGET_PROCESS_OFFSET"))
	if err != nil {
		t.Fatalf("parse process admission offset: %v", err)
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open process worker pool: %v", err)
	}
	defer pool.Close()
	repo := NewAdminRepository(pool, "budget-process-worker")
	result := budgetProcessResult{}
	admittedAt := time.Date(2026, time.July, 29, 10, 0, 0, 0, time.UTC)
	for index := range 50 {
		_, err := repo.AdmitAPIKeyBudget(context.Background(), keyID, budgetLedgerAdmissionID(offset+index), admittedAt)
		switch {
		case err == nil:
			result.Admitted++
		case errors.Is(err, admin.ErrAPIKeyRequestBudgetExceeded):
			result.Rejected++
		default:
			t.Fatalf("process admission %d: %v", index, err)
		}
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("encode process result: %v", err)
	}
	if err := os.WriteFile(os.Getenv("N2API_BUDGET_PROCESS_RESULT"), encoded, 0o600); err != nil {
		t.Fatalf("write process result: %v", err)
	}
}

func TestAPIKeyBudgetLedgerAdmissionAndSettlementAreIdempotent(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	key := createBudgetLedgerTestKey(t, ctx, repo, "idempotent", 1, 1000, 1000)
	admittedAt := time.Date(2026, time.July, 29, 10, 0, 0, 0, time.UTC)
	id := budgetLedgerAdmissionID(1)

	first, err := repo.AdmitAPIKeyBudget(ctx, key.ID, id, admittedAt)
	if err != nil {
		t.Fatalf("first AdmitAPIKeyBudget returned error: %v", err)
	}
	repeated, err := repo.AdmitAPIKeyBudget(ctx, key.ID, id, admittedAt.Add(time.Minute))
	if err != nil {
		t.Fatalf("repeated AdmitAPIKeyBudget returned error: %v", err)
	}
	if repeated != first {
		t.Fatalf("repeated admission = %+v, want %+v", repeated, first)
	}

	settledAt := admittedAt.Add(time.Hour)
	firstSettlement, err := repo.SettleAPIKeyBudget(ctx, id, 12, 34, settledAt)
	if err != nil {
		t.Fatalf("first SettleAPIKeyBudget returned error: %v", err)
	}
	if firstSettlement.AlreadySettled || firstSettlement.Outcome != "observed" {
		t.Fatalf("first settlement = %+v, want fresh observed settlement", firstSettlement)
	}
	repeatedSettlement, err := repo.SettleAPIKeyBudget(ctx, id, 99, 88, settledAt.Add(time.Minute))
	if err != nil {
		t.Fatalf("repeated SettleAPIKeyBudget returned error: %v", err)
	}
	if !repeatedSettlement.AlreadySettled || repeatedSettlement.ObservedTokens != 12 || repeatedSettlement.ObservedCostMicrousd != 34 {
		t.Fatalf("repeated settlement = %+v, want prior values", repeatedSettlement)
	}

	state, err := repo.GetAPIKeyBudgetState(ctx, key.ID)
	if err != nil {
		t.Fatalf("GetAPIKeyBudgetState returned error: %v", err)
	}
	if state.RequestsUsed24h != 1 || state.ObservedTokensUsed24h != 12 || state.ObservedCostMicrousdUsed24h != 34 {
		t.Fatalf("state after retries = %+v", state)
	}
}

func TestAPIKeyBudgetLedgerSettlementRetrySurvivesRepositoryRestart(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	key := createBudgetLedgerTestKey(t, ctx, repo, "settlement-retry", 10, 1000, 1000)
	admittedAt := time.Now().UTC().Add(-time.Hour)
	id := budgetLedgerAdmissionID(71)
	if _, err := repo.AdmitAPIKeyBudget(ctx, key.ID, id, admittedAt); err != nil {
		t.Fatalf("admit retry test request: %v", err)
	}
	installBudgetSettlementFailureTrigger(t, ctx, repo)
	_, err := repo.SettleAPIKeyBudget(ctx, id, 12, 34, admittedAt.Add(time.Minute))
	if err == nil {
		t.Fatal("settlement apply returned nil error with failure trigger")
	}

	var status, outcome string
	var usageKnown bool
	var tokens, cost int64
	if err := repo.pool.QueryRow(ctx, `
		SELECT status, settlement_outcome, usage_known, observed_tokens, observed_cost_microusd
		FROM api_key_budget_admissions
		WHERE admission_id = $1
	`, id).Scan(&status, &outcome, &usageKnown, &tokens, &cost); err != nil {
		t.Fatalf("read durable pending settlement: %v", err)
	}
	if status != "settlement_pending" || outcome != "observed" || !usageKnown || tokens != 12 || cost != 34 {
		t.Fatalf("durable pending settlement = %s/%s/%v/%d/%d", status, outcome, usageKnown, tokens, cost)
	}
	state, err := repo.GetAPIKeyBudgetState(ctx, key.ID)
	if err != nil {
		t.Fatalf("load state after failed apply: %v", err)
	}
	if state.ObservedTokensUsed24h != 0 || state.ObservedCostMicrousdUsed24h != 0 {
		t.Fatalf("failed apply leaked counter update: %+v", state)
	}
	removeBudgetSettlementFailureTrigger(t, ctx, repo)

	restarted := NewAdminRepository(repo.pool, "settlement-restart")
	cycle, err := restarted.RunAPIKeyBudgetSettlementCycle(ctx, time.Now().UTC(), 10)
	if err != nil {
		t.Fatalf("settlement recovery cycle: %v", err)
	}
	if cycle.Processed != 1 {
		t.Fatalf("settlement recovery cycle = %+v, want one", cycle)
	}
	repeated, err := restarted.SettleAPIKeyBudget(ctx, id, 999, 888, time.Now().UTC())
	if err != nil {
		t.Fatalf("repeat recovered settlement: %v", err)
	}
	if !repeated.AlreadySettled || repeated.ObservedTokens != 12 || repeated.ObservedCostMicrousd != 34 {
		t.Fatalf("repeated recovered settlement = %+v, want first durable payload", repeated)
	}
	state, err = restarted.GetAPIKeyBudgetState(ctx, key.ID)
	if err != nil {
		t.Fatalf("load recovered state: %v", err)
	}
	if state.ObservedTokensUsed24h != 12 || state.ObservedCostMicrousdUsed24h != 34 {
		t.Fatalf("recovered state = %+v, want one settlement charge", state)
	}
	if second, err := restarted.RunAPIKeyBudgetSettlementCycle(ctx, time.Now().UTC(), 10); err != nil || second.Processed != 0 {
		t.Fatalf("idempotent recovery cycle = %+v, err=%v", second, err)
	}
}

func TestAPIKeyBudgetLedgerPendingSettlementAfterExpiryDoesNotRestoreExpiredUsage(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	key := createBudgetLedgerTestKey(t, ctx, repo, "settlement-expiry", 10, 1000, 1000)
	admittedAt := time.Now().UTC().Add(-25 * time.Hour)
	id := budgetLedgerAdmissionID(72)
	if _, err := repo.AdmitAPIKeyBudget(ctx, key.ID, id, admittedAt); err != nil {
		t.Fatalf("admit expiry test request: %v", err)
	}
	installBudgetSettlementFailureTrigger(t, ctx, repo)
	if _, err := repo.SettleAPIKeyBudget(ctx, id, 25, 50, admittedAt.Add(time.Hour)); err == nil {
		t.Fatal("settlement apply returned nil error with failure trigger")
	}
	if _, err := repo.RunAPIKeyBudgetExpiryCycle(ctx, time.Now().UTC(), 10); err != nil {
		t.Fatalf("expire pending settlement: %v", err)
	}
	removeBudgetSettlementFailureTrigger(t, ctx, repo)
	if result, err := repo.RunAPIKeyBudgetSettlementCycle(ctx, time.Now().UTC(), 10); err != nil || result.Processed != 1 {
		t.Fatalf("apply expired pending settlement = %+v, err=%v", result, err)
	}
	state, err := repo.GetAPIKeyBudgetState(ctx, key.ID)
	if err != nil {
		t.Fatalf("load expiry state: %v", err)
	}
	if state.RequestsUsed24h != 0 || state.ObservedTokensUsed24h != 0 || state.ObservedCostMicrousdUsed24h != 0 {
		t.Fatalf("expired 24h usage was restored: %+v", state)
	}
	if state.RequestsUsed30d != 1 || state.ObservedTokensUsed30d != 25 || state.ObservedCostMicrousdUsed30d != 50 {
		t.Fatalf("live 30d usage missing after retry: %+v", state)
	}
}

func TestAPIKeyBudgetLedgerObservesModificationDisableRestartAndRevoke(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	key := createBudgetLedgerTestKey(t, ctx, repo, "lifecycle", 2, 0, 0)
	admittedAt := time.Date(2026, time.July, 29, 10, 0, 0, 0, time.UTC)
	if _, err := repo.AdmitAPIKeyBudget(ctx, key.ID, budgetLedgerAdmissionID(1), admittedAt); err != nil {
		t.Fatalf("initial admission: %v", err)
	}
	if _, err := repo.SetAPIKeyDisabled(ctx, key.ID, true); err != nil {
		t.Fatalf("disable key: %v", err)
	}
	if _, err := repo.AdmitAPIKeyBudget(ctx, key.ID, budgetLedgerAdmissionID(2), admittedAt); !errors.Is(err, admin.ErrNotFound) {
		t.Fatalf("disabled admission error = %v, want ErrNotFound", err)
	}
	if _, err := repo.SetAPIKeyDisabled(ctx, key.ID, false); err != nil {
		t.Fatalf("enable key: %v", err)
	}
	if _, err := repo.UpdateAPIKeyBudgets(ctx, key.ID, 1, 0, 0, 1, 0, 0); err != nil {
		t.Fatalf("lower request budgets: %v", err)
	}
	if _, err := repo.AdmitAPIKeyBudget(ctx, key.ID, budgetLedgerAdmissionID(3), admittedAt); !errors.Is(err, admin.ErrAPIKeyRequestBudgetExceeded) {
		t.Fatalf("lowered-budget admission error = %v, want request budget exceeded", err)
	}
	if _, err := repo.UpdateAPIKeyBudgets(ctx, key.ID, 2, 0, 0, 2, 0, 0); err != nil {
		t.Fatalf("raise request budgets: %v", err)
	}
	restarted := NewAdminRepository(repo.pool, "restarted-budget-repository")
	if _, err := restarted.AdmitAPIKeyBudget(ctx, key.ID, budgetLedgerAdmissionID(4), admittedAt); err != nil {
		t.Fatalf("admission after repository restart: %v", err)
	}
	if _, err := repo.RevokeAPIKey(ctx, key.ID); err != nil {
		t.Fatalf("revoke key: %v", err)
	}
	if _, err := restarted.AdmitAPIKeyBudget(ctx, key.ID, budgetLedgerAdmissionID(5), admittedAt); !errors.Is(err, admin.ErrNotFound) {
		t.Fatalf("revoked admission error = %v, want ErrNotFound", err)
	}
}

func TestAPIKeyBudgetLedgerMissingUsageSettlesAsZero(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	key := createBudgetLedgerTestKey(t, ctx, repo, "missing", 10, 1000, 1000)
	admittedAt := time.Date(2026, time.July, 29, 10, 0, 0, 0, time.UTC)
	id := budgetLedgerAdmissionID(1)
	if _, err := repo.AdmitAPIKeyBudget(ctx, key.ID, id, admittedAt); err != nil {
		t.Fatalf("AdmitAPIKeyBudget returned error: %v", err)
	}

	settlement, err := repo.SettleAPIKeyBudgetUsage(ctx, admin.APIKeyBudgetSettlementRequest{
		AdmissionID: id, ObservedTokens: 500, ObservedCostMicrousd: 600,
		UsageKnown: false, SettledAt: admittedAt.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("SettleAPIKeyBudgetUsage returned error: %v", err)
	}
	if settlement.Outcome != "missing" || settlement.ObservedTokens != 0 || settlement.ObservedCostMicrousd != 0 {
		t.Fatalf("missing settlement = %+v, want zero usage", settlement)
	}
	state, err := repo.GetAPIKeyBudgetState(ctx, key.ID)
	if err != nil {
		t.Fatalf("GetAPIKeyBudgetState returned error: %v", err)
	}
	if state.ObservedTokensUsed24h != 0 || state.ObservedCostMicrousdUsed24h != 0 {
		t.Fatalf("missing usage changed observed counters: %+v", state)
	}
}

func TestAPIKeyBudgetLedgerExpiresAtExactWindowBoundaries(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	key := createBudgetLedgerTestKey(t, ctx, repo, "expiry", 10, 1000, 1000)
	admittedAt := time.Date(2026, time.June, 1, 10, 0, 0, 0, time.UTC)
	id := budgetLedgerAdmissionID(1)
	if _, err := repo.AdmitAPIKeyBudget(ctx, key.ID, id, admittedAt); err != nil {
		t.Fatalf("AdmitAPIKeyBudget returned error: %v", err)
	}
	if _, err := repo.SettleAPIKeyBudget(ctx, id, 25, 50, admittedAt.Add(time.Hour)); err != nil {
		t.Fatalf("SettleAPIKeyBudget returned error: %v", err)
	}

	before, err := repo.RunAPIKeyBudgetExpiryCycle(ctx, admittedAt.Add(24*time.Hour-time.Nanosecond), 10)
	if err != nil {
		t.Fatalf("expiry before boundary returned error: %v", err)
	}
	if before.Processed != 0 {
		t.Fatalf("expiry before boundary = %+v, want no work", before)
	}
	at24h, err := repo.RunAPIKeyBudgetExpiryCycle(ctx, admittedAt.Add(24*time.Hour), 10)
	if err != nil {
		t.Fatalf("expiry at 24h returned error: %v", err)
	}
	if at24h.Processed != 1 || at24h.Expired24h != 1 || at24h.Expired30d != 0 {
		t.Fatalf("24h expiry = %+v", at24h)
	}
	state, err := repo.GetAPIKeyBudgetState(ctx, key.ID)
	if err != nil {
		t.Fatalf("GetAPIKeyBudgetState after 24h returned error: %v", err)
	}
	if state.RequestsUsed24h != 0 || state.ObservedTokensUsed24h != 0 || state.RequestsUsed30d != 1 || state.ObservedTokensUsed30d != 25 {
		t.Fatalf("state after 24h expiry = %+v", state)
	}

	at30d, err := repo.RunAPIKeyBudgetExpiryCycle(ctx, admittedAt.Add(30*24*time.Hour), 10)
	if err != nil {
		t.Fatalf("expiry at 30d returned error: %v", err)
	}
	if at30d.Processed != 1 || at30d.Expired24h != 0 || at30d.Expired30d != 1 {
		t.Fatalf("30d expiry = %+v", at30d)
	}
	state, err = repo.GetAPIKeyBudgetState(ctx, key.ID)
	if err != nil {
		t.Fatalf("GetAPIKeyBudgetState after 30d returned error: %v", err)
	}
	if state.RequestsUsed30d != 0 || state.ObservedTokensUsed30d != 0 || state.ObservedCostMicrousdUsed30d != 0 {
		t.Fatalf("state after 30d expiry = %+v", state)
	}
}

func TestAPIKeyBudgetLedgerLateSettlementDoesNotSubtractUnchargedUsage(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	key := createBudgetLedgerTestKey(t, ctx, repo, "late-settlement", 100, 10_000, 10_000)
	base := time.Date(2026, time.July, 1, 10, 0, 0, 0, time.UTC)

	if _, err := repo.AdmitAPIKeyBudget(ctx, key.ID, budgetLedgerAdmissionID(1), base); err != nil {
		t.Fatalf("admit first due usage: %v", err)
	}
	if _, err := repo.SettleAPIKeyBudget(ctx, budgetLedgerAdmissionID(1), 100, 100, base.Add(time.Hour)); err != nil {
		t.Fatalf("settle first due usage: %v", err)
	}
	if _, err := repo.AdmitAPIKeyBudget(ctx, key.ID, budgetLedgerAdmissionID(2), base); err != nil {
		t.Fatalf("admit late usage: %v", err)
	}
	if _, err := repo.SettleAPIKeyBudget(ctx, budgetLedgerAdmissionID(2), 50, 50, base.Add(25*time.Hour)); err != nil {
		t.Fatalf("settle late usage: %v", err)
	}
	if _, err := repo.AdmitAPIKeyBudget(ctx, key.ID, budgetLedgerAdmissionID(3), base.Add(2*time.Hour)); err != nil {
		t.Fatalf("admit still-live usage: %v", err)
	}
	if _, err := repo.SettleAPIKeyBudget(ctx, budgetLedgerAdmissionID(3), 80, 80, base.Add(3*time.Hour)); err != nil {
		t.Fatalf("settle still-live usage: %v", err)
	}

	if _, err := repo.RunAPIKeyBudgetExpiryCycle(ctx, base.Add(25*time.Hour), 100); err != nil {
		t.Fatalf("run 24h expiry: %v", err)
	}
	state, err := repo.GetAPIKeyBudgetState(ctx, key.ID)
	if err != nil {
		t.Fatalf("GetAPIKeyBudgetState returned error: %v", err)
	}
	if state.RequestsUsed24h != 1 || state.ObservedTokensUsed24h != 80 || state.ObservedCostMicrousdUsed24h != 80 {
		t.Fatalf("24h state after late settlement expiry = %+v, want only still-live usage", state)
	}
	if state.RequestsUsed30d != 3 || state.ObservedTokensUsed30d != 230 || state.ObservedCostMicrousdUsed30d != 230 {
		t.Fatalf("30d state after late settlement expiry = %+v, want all observed usage", state)
	}
}

func TestAPIKeyBudgetLedgerAbandonedAdmissionKeepsRequestCharge(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	key := createBudgetLedgerTestKey(t, ctx, repo, "abandoned", 10, 1000, 1000)
	admittedAt := time.Date(2026, time.July, 29, 10, 0, 0, 0, time.UTC)
	id := budgetLedgerAdmissionID(1)
	if _, err := repo.AdmitAPIKeyBudget(ctx, key.ID, id, admittedAt); err != nil {
		t.Fatalf("AdmitAPIKeyBudget returned error: %v", err)
	}
	cycle, err := repo.RunAPIKeyBudgetAbandonedCycle(ctx, admittedAt, admittedAt.Add(time.Hour), 10)
	if err != nil {
		t.Fatalf("RunAPIKeyBudgetAbandonedCycle returned error: %v", err)
	}
	if cycle.Processed != 1 {
		t.Fatalf("abandoned cycle = %+v, want one", cycle)
	}
	state, err := repo.GetAPIKeyBudgetState(ctx, key.ID)
	if err != nil {
		t.Fatalf("GetAPIKeyBudgetState returned error: %v", err)
	}
	if state.RequestsUsed24h != 1 || state.RequestsUsed30d != 1 {
		t.Fatalf("abandoned admission refunded requests: %+v", state)
	}
	settlement, err := repo.SettleAPIKeyBudget(ctx, id, 100, 200, admittedAt.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("settle abandoned admission returned error: %v", err)
	}
	if !settlement.AlreadySettled || settlement.Outcome != "abandoned" {
		t.Fatalf("settlement after abandon = %+v", settlement)
	}
}

func TestAPIKeyBudgetLedgerLegacyInitializationIsBatchedAndRestartable(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	key := createBudgetLedgerTestKey(t, ctx, repo, "legacy", 10, 1000, 1000)
	now := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	insertBudgetLedgerRequestLog(t, ctx, repo, key.ID, "old-observed", now.Add(-25*time.Hour), 20, 200, "parsed")
	insertBudgetLedgerRequestLog(t, ctx, repo, key.ID, "missing", now.Add(-2*time.Hour), 999, 999, "missing")
	insertBudgetLedgerRequestLog(t, ctx, repo, key.ID, "recent-observed", now.Add(-time.Hour), 10, 100, "parsed")
	if _, err := repo.pool.Exec(ctx, `
		UPDATE api_key_budget_states
		SET initialization_status = 'pending', initialized_at = NULL,
			initialization_window_start = $2::timestamptz - INTERVAL '30 days',
			initialization_window_end = $2::timestamptz,
			initialization_cursor_created_at = NULL,
			initialization_cursor_request_log_id = NULL,
			requests_used_24h = 0, requests_used_30d = 0,
			observed_tokens_used_24h = 0, observed_tokens_used_30d = 0,
			observed_cost_microusd_used_24h = 0, observed_cost_microusd_used_30d = 0
		WHERE client_key_id = $1
	`, key.ID, now); err != nil {
		t.Fatalf("prepare pending state: %v", err)
	}

	first, err := repo.RunAPIKeyBudgetInitializationCycle(ctx, now, 2)
	if err != nil {
		t.Fatalf("first initialization cycle returned error: %v", err)
	}
	if first.ClientKeyID != key.ID || first.Processed != 2 || first.Ready || !first.HasPending {
		t.Fatalf("first initialization cycle = %+v", first)
	}
	second, err := repo.RunAPIKeyBudgetInitializationCycle(ctx, now.Add(time.Minute), 2)
	if err != nil {
		t.Fatalf("second initialization cycle returned error: %v", err)
	}
	if second.ClientKeyID != key.ID || second.Processed != 1 || !second.Ready || second.HasPending {
		t.Fatalf("second initialization cycle = %+v", second)
	}
	state, err := repo.GetAPIKeyBudgetState(ctx, key.ID)
	if err != nil {
		t.Fatalf("GetAPIKeyBudgetState returned error: %v", err)
	}
	if state.RequestsUsed24h != 2 || state.RequestsUsed30d != 3 ||
		state.ObservedTokensUsed24h != 10 || state.ObservedTokensUsed30d != 30 ||
		state.ObservedCostMicrousdUsed24h != 100 || state.ObservedCostMicrousdUsed30d != 300 {
		t.Fatalf("initialized state = %+v", state)
	}
	third, err := repo.RunAPIKeyBudgetInitializationCycle(ctx, now.Add(2*time.Minute), 2)
	if err != nil {
		t.Fatalf("idempotent initialization cycle returned error: %v", err)
	}
	if third != (admin.APIKeyBudgetInitializationCycleResult{}) {
		t.Fatalf("idempotent initialization cycle = %+v, want empty", third)
	}
}

func TestAPIKeyBudgetLedgerInitializationLocksKeyBeforeState(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	key, err := repo.CreateAPIKey(ctx, "init lock order", "init-lock-order-hash", "n2api_", "encrypted", nil)
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}
	if _, err := repo.UpdateAPIKeyBudgets(ctx, key.ID, 1, 0, 0, 1, 0, 0); err != nil {
		t.Fatalf("enable budgets: %v", err)
	}
	if _, err := repo.pool.Exec(ctx, `
		UPDATE api_key_budget_states
		SET initialization_status = 'pending', initialized_at = NULL,
			initialization_window_start = now() - INTERVAL '30 days',
			initialization_window_end = now()
		WHERE client_key_id = $1
	`, key.ID); err != nil {
		t.Fatalf("mark budget initialization pending: %v", err)
	}

	blocker, err := repo.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin key blocker: %v", err)
	}
	defer blocker.Rollback(context.Background())
	if _, err := blocker.Exec(ctx, `SELECT id FROM client_api_keys WHERE id = $1 FOR UPDATE`, key.ID); err != nil {
		t.Fatalf("lock key: %v", err)
	}
	workerRepo, applicationName := newBudgetLedgerLockTestRepository(t, repo, "init-key-before-state")
	done := make(chan error, 1)
	go func() {
		_, err := workerRepo.RunAPIKeyBudgetInitializationCycle(ctx, time.Now(), 10)
		done <- err
	}()
	waitForBudgetLedgerLockWait(t, ctx, repo, applicationName)

	probe, err := repo.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin state probe: %v", err)
	}
	if _, err := probe.Exec(ctx, `SELECT client_key_id FROM api_key_budget_states WHERE client_key_id = $1 FOR UPDATE NOWAIT`, key.ID); err != nil {
		_ = probe.Rollback(context.Background())
		t.Fatalf("initialization held state while waiting for key: %v", err)
	}
	if err := probe.Rollback(ctx); err != nil {
		t.Fatalf("release state probe: %v", err)
	}
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatalf("release key blocker: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("initialization after key release: %v", err)
	}
}

func TestAPIKeyBudgetLedgerExpiryLocksStatesByClientKeyID(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	first := createBudgetLedgerTestKey(t, ctx, repo, "expiry-lock-first", 10, 0, 0)
	second := createBudgetLedgerTestKey(t, ctx, repo, "expiry-lock-second", 10, 0, 0)
	base := time.Now().UTC().Add(-48 * time.Hour)
	if _, err := repo.AdmitAPIKeyBudget(ctx, second.ID, budgetLedgerAdmissionID(201), base.Add(-time.Hour)); err != nil {
		t.Fatalf("admit second-key earlier candidate: %v", err)
	}
	if _, err := repo.AdmitAPIKeyBudget(ctx, first.ID, budgetLedgerAdmissionID(202), base); err != nil {
		t.Fatalf("admit first-key later candidate: %v", err)
	}

	blocker, err := repo.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin higher-key blocker: %v", err)
	}
	defer blocker.Rollback(context.Background())
	if _, err := blocker.Exec(ctx, `SELECT client_key_id FROM api_key_budget_states WHERE client_key_id = $1 FOR UPDATE`, second.ID); err != nil {
		t.Fatalf("lock higher key state: %v", err)
	}
	workerRepo, applicationName := newBudgetLedgerLockTestRepository(t, repo, "expiry-client-key-order")
	done := make(chan error, 1)
	go func() {
		_, err := workerRepo.RunAPIKeyBudgetExpiryCycle(ctx, time.Now(), 10)
		done <- err
	}()
	waitForBudgetLedgerLockWait(t, ctx, repo, applicationName)

	probe, err := repo.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin lower-key state probe: %v", err)
	}
	if _, err := probe.Exec(ctx, `SELECT client_key_id FROM api_key_budget_states WHERE client_key_id = $1 FOR UPDATE NOWAIT`, first.ID); err == nil {
		_ = probe.Rollback(context.Background())
		t.Fatal("expiry did not lock the lower client_key_id before waiting on the higher client_key_id")
	}
	_ = probe.Rollback(context.Background())
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatalf("release higher-key blocker: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("expiry after higher-key release: %v", err)
	}
}

func TestAPIKeyBudgetLedgerFailsClosedWhenDatabaseIsClosed(t *testing.T) {
	repo := newTestAdminRepository(t)
	keyID := createBudgetLedgerTestKey(t, context.Background(), repo, "closed", 10, 0, 0).ID
	repo.pool.Close()

	if _, err := repo.AdmitAPIKeyBudget(context.Background(), keyID, budgetLedgerAdmissionID(1), time.Now()); err == nil {
		t.Fatal("AdmitAPIKeyBudget returned nil error after database close")
	}
}

func TestAPIKeyBudgetLedgerRejectsInvalidBatchSizes(t *testing.T) {
	repo := NewAdminRepository(nil, "test")
	now := time.Now()
	for _, batchSize := range []int{-1, 0, maxAPIKeyBudgetLedgerBatchSize + 1} {
		if _, err := repo.RunAPIKeyBudgetExpiryCycle(context.Background(), now, batchSize); !errors.Is(err, admin.ErrInvalidInput) {
			t.Errorf("expiry batch size %d error = %v", batchSize, err)
		}
		if _, err := repo.RunAPIKeyBudgetSettlementCycle(context.Background(), now, batchSize); !errors.Is(err, admin.ErrInvalidInput) {
			t.Errorf("settlement batch size %d error = %v", batchSize, err)
		}
		if _, err := repo.RunAPIKeyBudgetAbandonedCycle(context.Background(), now.Add(-time.Hour), now, batchSize); !errors.Is(err, admin.ErrInvalidInput) {
			t.Errorf("abandoned batch size %d error = %v", batchSize, err)
		}
		if _, err := repo.RunAPIKeyBudgetInitializationCycle(context.Background(), now, batchSize); !errors.Is(err, admin.ErrInvalidInput) {
			t.Errorf("initialization batch size %d error = %v", batchSize, err)
		}
	}
}

func createBudgetLedgerTestKey(t *testing.T, ctx context.Context, repo *AdminRepository, suffix string, requests, tokens int, cost int64) admin.APIKey {
	t.Helper()
	key, err := repo.CreateAPIKey(ctx, "budget "+suffix, "budget-hash-"+suffix, "n2api_", "encrypted", nil)
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}
	if requests > 0 || tokens > 0 || cost > 0 {
		key, err = repo.UpdateAPIKeyBudgets(ctx, key.ID, requests, tokens, cost, requests, tokens, cost)
		if err != nil {
			t.Fatalf("UpdateAPIKeyBudgets returned error: %v", err)
		}
		state, err := repo.GetAPIKeyBudgetState(ctx, key.ID)
		if err != nil {
			t.Fatalf("load test key budget state: %v", err)
		}
		if state.InitializationStatus == admin.APIKeyBudgetInitializationPending {
			result, err := repo.RunAPIKeyBudgetInitializationCycle(ctx, time.Now().Add(time.Minute), maxAPIKeyBudgetLedgerBatchSize)
			if err != nil {
				t.Fatalf("initialize test key budget: %v", err)
			}
			if result.ClientKeyID != key.ID || !result.Ready {
				t.Fatalf("initialize test key result = %+v, want ready key %d", result, key.ID)
			}
		} else if state.InitializationStatus != admin.APIKeyBudgetInitializationReady {
			t.Fatalf("test key state = %+v, want ready or pending", state)
		}
	}
	return key
}

func insertBudgetLedgerRequestLog(t *testing.T, ctx context.Context, repo *AdminRepository, keyID int64, requestID string, createdAt time.Time, tokens int64, cost int64, usageSource string) {
	t.Helper()
	insertBudgetLedgerRequestLogWithEligibility(t, ctx, repo, keyID, requestID, createdAt, tokens, cost, usageSource, true, "")
}

func insertBudgetLedgerRequestLogWithEligibility(t *testing.T, ctx context.Context, repo *AdminRepository, keyID int64, requestID string, createdAt time.Time, tokens int64, cost int64, usageSource string, budgetEligible bool, errorCode string) {
	t.Helper()
	if _, err := repo.pool.Exec(ctx, `
		INSERT INTO request_logs (
			request_id, client_key_id, provider, route, method, status_code, latency_ms,
			total_tokens, estimated_cost_microusd, usage_source, budget_backfill_eligible, error, created_at
		) VALUES ($1, $2, 'openai', '/v1/responses', 'POST', 200, 1, $3, $4, $5, $6, $7, $8)
	`, requestID, keyID, tokens, cost, usageSource, budgetEligible, errorCode, createdAt); err != nil {
		t.Fatalf("insert request log %q: %v", requestID, err)
	}
}

func newBudgetLedgerLockTestRepository(t *testing.T, repo *AdminRepository, suffix string) (*AdminRepository, string) {
	t.Helper()
	config := repo.pool.Config().Copy()
	applicationName := fmt.Sprintf("n2api-budget-ledger-%s-%d", suffix, time.Now().UnixNano())
	config.ConnConfig.RuntimeParams["application_name"] = applicationName
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("create lock test pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return NewAdminRepository(pool, "budget-lock-test"), applicationName
}

func waitForBudgetLedgerLockWait(t *testing.T, ctx context.Context, repo *AdminRepository, applicationName string) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := repo.pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM pg_stat_activity
				WHERE application_name = $1
					AND wait_event_type = 'Lock'
			)
		`, applicationName).Scan(&waiting); err != nil {
			t.Fatalf("inspect lock wait: %v", err)
		}
		if waiting {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("worker did not enter lock wait: %v", ctx.Err())
		case <-ticker.C:
		}
	}
}

func installBudgetSettlementFailureTrigger(t *testing.T, ctx context.Context, repo *AdminRepository) {
	t.Helper()
	if _, err := repo.pool.Exec(ctx, `
		CREATE OR REPLACE FUNCTION n2api_test_fail_budget_settlement() RETURNS trigger
		LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.status = 'settled' AND OLD.status = 'settlement_pending' THEN
				RAISE EXCEPTION 'injected settlement apply failure';
			END IF;
			RETURN NEW;
		END;
		$$;
		CREATE TRIGGER n2api_test_fail_budget_settlement
		BEFORE UPDATE OF status ON api_key_budget_admissions
		FOR EACH ROW EXECUTE FUNCTION n2api_test_fail_budget_settlement()
	`); err != nil {
		t.Fatalf("install settlement failure trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = repo.pool.Exec(context.Background(), `
			DROP TRIGGER IF EXISTS n2api_test_fail_budget_settlement ON api_key_budget_admissions;
			DROP FUNCTION IF EXISTS n2api_test_fail_budget_settlement()
		`)
	})
}

func removeBudgetSettlementFailureTrigger(t *testing.T, ctx context.Context, repo *AdminRepository) {
	t.Helper()
	if _, err := repo.pool.Exec(ctx, `
		DROP TRIGGER IF EXISTS n2api_test_fail_budget_settlement ON api_key_budget_admissions;
		DROP FUNCTION IF EXISTS n2api_test_fail_budget_settlement()
	`); err != nil {
		t.Fatalf("remove settlement failure trigger: %v", err)
	}
}

func budgetLedgerAdmissionID(sequence int) string {
	return fmt.Sprintf("%032x", sequence)
}
