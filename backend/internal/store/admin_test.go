package store

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/KnowSky404/N2API/backend/internal/systemevent"
	"github.com/jackc/pgx/v5/pgxpool"
)

const truncateStoreTestDataSQL = `TRUNCATE
	alert_rule_states, alert_rules, alert_actions, api_key_budget_threshold_states,
	api_key_routing_exhaustion_states, request_log_projector_checkpoints,
	system_events, request_logs, provider_account_test_results, provider_session_bindings,
	routing_pool_accounts, routing_pools, client_api_key_models, client_api_keys,
	provider_account_models, provider_account_credentials, provider_accounts,
	oauth_account_models, oauth_accounts, oauth_states, admin_sessions, admins,
	error_passthrough_rules, fingerprint_profiles, settings
	RESTART IDENTITY CASCADE`

func TestAdminRepositoryImplementsInterface(t *testing.T) {
	var _ admin.Repository = (*AdminRepository)(nil)
}

func TestRequestLogCursorIsAuthenticatedAndFilterBound(t *testing.T) {
	repo := NewAdminRepository(nil, "cursor-secret")
	filter := admin.RequestLogFilter{
		Limit:       25,
		StatusClass: admin.RequestLogStatusAll,
		Model:       "gpt-5",
	}
	want := requestLogCursor{
		Version:      requestLogCursorVersion,
		CreatedAt:    time.Unix(1234, 567).UTC(),
		ID:           42,
		FilterDigest: requestLogFilterDigest(filter),
	}
	encoded, err := repo.encodeRequestLogCursor(want)
	if err != nil {
		t.Fatalf("encodeRequestLogCursor returned error: %v", err)
	}
	got, err := repo.decodeRequestLogCursor(encoded, filter)
	if err != nil {
		t.Fatalf("decodeRequestLogCursor returned error: %v", err)
	}
	if got != want {
		t.Fatalf("decoded cursor = %+v, want %+v", got, want)
	}

	changedLimit := filter
	changedLimit.Limit = 100
	if _, err := repo.decodeRequestLogCursor(encoded, changedLimit); err != nil {
		t.Fatalf("cursor must not bind page size: %v", err)
	}
	changedLimit.Cursor = "ignored-current-cursor"
	if _, err := repo.decodeRequestLogCursor(encoded, changedLimit); err != nil {
		t.Fatalf("cursor must not bind the current cursor value: %v", err)
	}

	equivalentSince := filter
	equivalentSince.Since = time.Date(2026, time.July, 21, 14, 0, 0, 0, time.FixedZone("CEST", 2*60*60))
	filterWithUTC := filter
	filterWithUTC.Since = time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	want.FilterDigest = requestLogFilterDigest(filterWithUTC)
	encodedWithSince, err := repo.encodeRequestLogCursor(want)
	if err != nil {
		t.Fatalf("encodeRequestLogCursor with since returned error: %v", err)
	}
	if _, err := repo.decodeRequestLogCursor(encodedWithSince, equivalentSince); err != nil {
		t.Fatalf("cursor must canonicalize equivalent since instants to UTC: %v", err)
	}

	tampered := encoded
	if tampered[0] == 'A' {
		tampered = "B" + tampered[1:]
	} else {
		tampered = "A" + tampered[1:]
	}
	if _, err := repo.decodeRequestLogCursor(tampered, filter); !errors.Is(err, admin.ErrInvalidInput) {
		t.Fatalf("tampered cursor error = %v, want ErrInvalidInput", err)
	}

	changedFilter := filter
	changedFilter.Model = "gpt-5-mini"
	if _, err := repo.decodeRequestLogCursor(encoded, changedFilter); !errors.Is(err, admin.ErrInvalidInput) {
		t.Fatalf("filter-mismatched cursor error = %v, want ErrInvalidInput", err)
	}
	if _, err := repo.decodeRequestLogCursor("not-a-cursor", filter); !errors.Is(err, admin.ErrInvalidInput) {
		t.Fatalf("malformed cursor error = %v, want ErrInvalidInput", err)
	}
	if _, err := NewAdminRepository(nil, "different-secret").decodeRequestLogCursor(encoded, filter); !errors.Is(err, admin.ErrInvalidInput) {
		t.Fatalf("wrong-secret cursor error = %v, want ErrInvalidInput", err)
	}
	for name, invalid := range map[string]requestLogCursor{
		"unknown version": {Version: requestLogCursorVersion + 1, CreatedAt: want.CreatedAt, ID: want.ID, FilterDigest: requestLogFilterDigest(filter)},
		"zero time":       {Version: requestLogCursorVersion, ID: want.ID, FilterDigest: requestLogFilterDigest(filter)},
		"zero id":         {Version: requestLogCursorVersion, CreatedAt: want.CreatedAt, FilterDigest: requestLogFilterDigest(filter)},
	} {
		t.Run(name, func(t *testing.T) {
			value, err := repo.encodeRequestLogCursor(invalid)
			if err != nil {
				t.Fatalf("encode invalid cursor: %v", err)
			}
			if _, err := repo.decodeRequestLogCursor(value, filter); !errors.Is(err, admin.ErrInvalidInput) {
				t.Fatalf("decode invalid cursor error = %v, want ErrInvalidInput", err)
			}
		})
	}
	for _, limit := range []int{0, 201} {
		if _, err := repo.ListRequestLogs(context.Background(), admin.RequestLogFilter{Limit: limit}); !errors.Is(err, admin.ErrInvalidInput) {
			t.Fatalf("ListRequestLogs limit %d error = %v, want ErrInvalidInput", limit, err)
		}
	}
}

func TestRequestLogFilterDigestCoversEveryContentFilter(t *testing.T) {
	base := admin.RequestLogFilter{}
	variants := map[string]admin.RequestLogFilter{
		"Since":             {Since: time.Unix(1, 0).UTC()},
		"Before":            {Before: time.Unix(2, 0).UTC()},
		"RequestID":         {RequestID: "req_1"},
		"Query":             {Query: "codex"},
		"StatusClass":       {StatusClass: admin.RequestLogStatusServerError},
		"StatusCode":        {StatusCode: 503},
		"ProviderAccountID": {ProviderAccountID: 1},
		"RoutingPoolID":     {RoutingPoolID: 1},
		"ClientKeyID":       {ClientKeyID: 1},
		"Model":             {Model: "gpt-5"},
		"SessionID":         {SessionID: "session-1"},
		"Error":             {Error: "upstream_error"},
		"UsageSource":       {UsageSource: "responses"},
		"RoutingPoolError":  {RoutingPoolError: "routing_pool_empty"},
		"RoutingPoolChain":  {RoutingPoolChain: "primary -> secondary"},
		"GatewayFallbacks":  {GatewayFallbacks: true},
	}
	baseDigest := requestLogFilterDigest(base)
	filterType := reflect.TypeOf(base)
	for i := range filterType.NumField() {
		name := filterType.Field(i).Name
		if name == "Limit" || name == "Cursor" {
			continue
		}
		variant, ok := variants[name]
		if !ok {
			t.Fatalf("RequestLogFilter field %s has no filter-digest coverage", name)
		}
		if got := requestLogFilterDigest(variant); got == baseDigest {
			t.Fatalf("RequestLogFilter field %s does not change the filter digest", name)
		}
	}
}

func TestRequestLogFilterDigestPreservesLegacyBytesWithoutBefore(t *testing.T) {
	filter := admin.RequestLogFilter{Since: time.Unix(1, 0).UTC(), Query: "codex", StatusClass: admin.RequestLogStatusAll}
	legacyPayload, err := json.Marshal(struct {
		Since             string `json:"since"`
		RequestID         string `json:"requestId"`
		Query             string `json:"query"`
		StatusClass       string `json:"statusClass"`
		StatusCode        int    `json:"statusCode"`
		ProviderAccountID int64  `json:"providerAccountId"`
		RoutingPoolID     int64  `json:"routingPoolId"`
		ClientKeyID       int64  `json:"clientKeyId"`
		Model             string `json:"model"`
		SessionID         string `json:"sessionId"`
		Error             string `json:"error"`
		UsageSource       string `json:"usageSource"`
		RoutingPoolError  string `json:"routingPoolError"`
		RoutingPoolChain  string `json:"routingPoolChain"`
		GatewayFallbacks  bool   `json:"gatewayFallbacks"`
	}{Since: filter.Since.Format(time.RFC3339Nano), Query: filter.Query, StatusClass: filter.StatusClass})
	if err != nil {
		t.Fatalf("Marshal legacy payload returned error: %v", err)
	}
	want := sha256.Sum256(legacyPayload)
	if got := requestLogFilterDigest(filter); got != base64.RawURLEncoding.EncodeToString(want[:]) {
		t.Fatalf("digest = %q, want legacy digest", got)
	}
}

func TestAdminRepositoryStreamsRequestLogsInRangeOrderAndReportsLimit(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	key, err := repo.CreateAPIKey(ctx, "export-key", "hash-export-key", "n2api_", "encrypted-export-key", nil)
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}
	since := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	before := since.Add(4 * time.Minute)
	insertRequestLog(t, repo.pool, key.ID, since.Add(-time.Minute), 200, 10, 100)
	insertRequestLog(t, repo.pool, key.ID, since, 200, 10, 100)
	insertRequestLog(t, repo.pool, key.ID, since.Add(time.Minute), 200, 10, 100)
	insertRequestLog(t, repo.pool, key.ID, since.Add(2*time.Minute), 500, 10, 100)
	insertRequestLog(t, repo.pool, key.ID, since.Add(3*time.Minute), 200, 10, 100)
	insertRequestLog(t, repo.pool, key.ID, before, 200, 10, 100)

	var visited []time.Time
	result, err := repo.StreamRequestLogs(ctx, admin.RequestLogFilter{
		Since:       since,
		Before:      before,
		StatusClass: admin.RequestLogStatusSuccess,
	}, 2, func(log admin.RequestLog) error {
		visited = append(visited, log.CreatedAt)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamRequestLogs returned error: %v", err)
	}
	if result.RowCount != 2 || !result.LimitReached {
		t.Fatalf("result = %+v, want two rows and limit reached", result)
	}
	want := []time.Time{since.Add(3 * time.Minute), since.Add(time.Minute)}
	if !slices.Equal(visited, want) {
		t.Fatalf("visited times = %v, want %v", visited, want)
	}

	visited = nil
	result, err = repo.StreamRequestLogs(ctx, admin.RequestLogFilter{
		Since:       since,
		Before:      before,
		StatusClass: admin.RequestLogStatusSuccess,
	}, 10, func(log admin.RequestLog) error {
		visited = append(visited, log.CreatedAt)
		return nil
	})
	if err != nil {
		t.Fatalf("unbounded fixture StreamRequestLogs returned error: %v", err)
	}
	if result.RowCount != 3 || result.LimitReached {
		t.Fatalf("result = %+v, want three rows without limit reached", result)
	}
}

func TestAdminRepositoryStreamRequestLogsStopsOnVisitorErrorAndCanceledContext(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	key, err := repo.CreateAPIKey(ctx, "export-stop-key", "hash-export-stop-key", "n2api_", "encrypted-export-stop-key", nil)
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}
	insertRequestLog(t, repo.pool, key.ID, time.Now().UTC(), 200, 10, 100)
	wantErr := errors.New("visitor stopped")

	now := time.Now().UTC()
	rangeFilter := admin.RequestLogFilter{Since: now.Add(-time.Hour), Before: now.Add(time.Hour)}
	result, err := repo.StreamRequestLogs(ctx, rangeFilter, 10, func(admin.RequestLog) error {
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("visitor error = %v, want %v", err, wantErr)
	}
	if result.RowCount != 0 {
		t.Fatalf("visitor error result = %+v, want zero successful callbacks", result)
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := repo.StreamRequestLogs(canceled, rangeFilter, 10, func(admin.RequestLog) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled stream error = %v, want context.Canceled", err)
	}
}

func TestSessionValidationUsesConditionalAtomicTouch(t *testing.T) {
	source, err := os.ReadFile("admin.go")
	if err != nil {
		t.Fatalf("ReadFile admin.go returned error: %v", err)
	}
	contents := string(source)
	for _, want := range []string{
		"WITH active_session AS MATERIALIZED",
		"SET last_used_at = $2",
		"session.last_used_at <= $2 - INTERVAL '1 minute'",
		"AND expires_at > $2",
		"AND revoked_at IS NULL",
	} {
		if !strings.Contains(contents, want) {
			t.Fatalf("session validation source missing %q", want)
		}
	}
}

func TestUsageSummaryGroupSQLAllowsOnlyKnownGroups(t *testing.T) {
	for _, groupBy := range []string{"client_key", "provider_account", "routing_pool", "routing_pool_chain", "model", "session", "usage_source"} {
		t.Run(groupBy, func(t *testing.T) {
			groupExpr, labelExpr, _, ok := usageSummaryGroupSQL(groupBy)
			if !ok {
				t.Fatalf("usageSummaryGroupSQL(%q) ok = false, want true", groupBy)
			}
			if groupExpr == "" || labelExpr == "" {
				t.Fatalf("usageSummaryGroupSQL(%q) returned empty expressions", groupBy)
			}
		})
	}

	if _, _, _, ok := usageSummaryGroupSQL("status; DROP TABLE request_logs"); ok {
		t.Fatal("usageSummaryGroupSQL accepted an unknown group")
	}
}

func TestUsageSummaryRoutingPoolGroupUsesLoggedSnapshot(t *testing.T) {
	groupExpr, labelExpr, joinSQL, ok := usageSummaryGroupSQL("routing_pool")
	if !ok {
		t.Fatal("usageSummaryGroupSQL(routing_pool) ok = false, want true")
	}
	if !strings.Contains(groupExpr, "l.routing_pool_id") || !strings.Contains(labelExpr, "l.routing_pool_name") {
		t.Fatalf("routing pool group expressions = %q / %q, want logged routing pool snapshot", groupExpr, labelExpr)
	}
	if joinSQL != "" {
		t.Fatalf("routing pool group join SQL = %q, want no join", joinSQL)
	}
}

func TestUsageSummaryRoutingPoolChainGroupUsesLoggedFallbackChain(t *testing.T) {
	groupExpr, labelExpr, joinSQL, ok := usageSummaryGroupSQL("routing_pool_chain")
	if !ok {
		t.Fatal("usageSummaryGroupSQL(routing_pool_chain) ok = false, want true")
	}
	if !strings.Contains(groupExpr, "l.routing_pool_fallback_chain") || !strings.Contains(labelExpr, "l.routing_pool_fallback_chain") {
		t.Fatalf("routing pool chain group expressions = %q / %q, want logged fallback chain", groupExpr, labelExpr)
	}
	if !strings.Contains(labelExpr, "No fallback chain") {
		t.Fatalf("routing pool chain label expression = %q, want no-chain fallback label", labelExpr)
	}
	if joinSQL != "" {
		t.Fatalf("routing pool chain group join SQL = %q, want no join", joinSQL)
	}
}

func TestUsageSummarySessionGroupUsesLoggedSessionID(t *testing.T) {
	groupExpr, labelExpr, joinSQL, ok := usageSummaryGroupSQL("session")
	if !ok {
		t.Fatal("usageSummaryGroupSQL(session) ok = false, want true")
	}
	if !strings.Contains(groupExpr, "l.session_id") || !strings.Contains(labelExpr, "l.session_id") {
		t.Fatalf("session group expressions = %q / %q, want request log session_id", groupExpr, labelExpr)
	}
	if joinSQL != "" {
		t.Fatalf("session group join SQL = %q, want no join", joinSQL)
	}
}

func TestUsageSummaryUsageSourceGroupUsesLoggedUsageSource(t *testing.T) {
	groupExpr, labelExpr, joinSQL, ok := usageSummaryGroupSQL("usage_source")
	if !ok {
		t.Fatal("usageSummaryGroupSQL(usage_source) ok = false, want true")
	}
	if !strings.Contains(groupExpr, "l.usage_source") || !strings.Contains(labelExpr, "l.usage_source") {
		t.Fatalf("usage source group expressions = %q / %q, want request log usage_source", groupExpr, labelExpr)
	}
	if !strings.Contains(labelExpr, "Missing usage") {
		t.Fatalf("usage source label expression = %q, want missing usage fallback label", labelExpr)
	}
	if joinSQL != "" {
		t.Fatalf("usage source group join SQL = %q, want no join", joinSQL)
	}
}

func TestUsageSummarySelectsCachedAndReasoningTokens(t *testing.T) {
	source, err := os.ReadFile("admin.go")
	if err != nil {
		t.Fatalf("ReadFile admin.go returned error: %v", err)
	}
	sql := string(source)
	for _, want := range []string{
		"COALESCE(SUM(l.cached_input_tokens), 0)",
		"COALESCE(SUM(l.reasoning_tokens), 0)",
		"&row.CachedInputTokens",
		"&row.ReasoningTokens",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("GetUsageSummary source missing %q", want)
		}
	}
}

func TestListRequestLogsPrefersProviderAccountNameSnapshot(t *testing.T) {
	source, err := os.ReadFile("admin.go")
	if err != nil {
		t.Fatalf("ReadFile admin.go returned error: %v", err)
	}
	sql := string(source)
	if !strings.Contains(sql, "COALESCE(NULLIF(l.provider_account_name, ''), NULLIF(a.display_name, ''), a.name, '')") {
		t.Fatal("ListRequestLogs must prefer the logged provider account name snapshot before joining the current account row")
	}
}

func TestListRequestLogsSelectsGatewayFallbackDiagnostics(t *testing.T) {
	source, err := os.ReadFile("admin.go")
	if err != nil {
		t.Fatalf("ReadFile admin.go returned error: %v", err)
	}
	sql := string(source)
	for _, want := range []string{
		"COALESCE(l.gateway_attempt_count, 0)",
		"COALESCE(l.gateway_fallback_count, 0)",
		"&log.GatewayAttemptCount",
		"&log.GatewayFallbackCount",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("ListRequestLogs source missing %q", want)
		}
	}
}

func TestRequestLogRetentionUsesOrderedBoundedDeleteAndSessionLock(t *testing.T) {
	source, err := os.ReadFile("admin.go")
	if err != nil {
		t.Fatalf("ReadFile admin.go returned error: %v", err)
	}
	sql := string(source)
	for _, want := range []string{
		"pg_try_advisory_lock",
		"pg_advisory_unlock",
		"Hijack()",
		"discardRequestLogRetentionConnection(conn)",
		"ORDER BY created_at ASC, id ASC",
		"LIMIT $2",
		"DELETE FROM request_logs",
		"created_at < $1",
		"RowsAffected()",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("request log retention source missing %q", want)
		}
	}
}

func TestOpsErrorAccountBucketsUseAccountIDKeys(t *testing.T) {
	source, err := os.ReadFile("ops.go")
	if err != nil {
		t.Fatalf("ReadFile ops.go returned error: %v", err)
	}
	sql := string(source)
	for _, want := range []string{
		"l.provider_account_id::text",
		"COALESCE(NULLIF(l.provider_account_name, ''), 'unknown')",
		"bucket.Label",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("ops error account bucket source missing %q", want)
		}
	}
}

func TestOpsCostBreakdownRanksModelsAccountsAndAPIKeysByEstimatedCost(t *testing.T) {
	source, err := os.ReadFile("ops.go")
	if err != nil {
		t.Fatalf("ReadFile ops.go returned error: %v", err)
	}
	sql := string(source)
	for _, want := range []string{
		"func (r *AdminRepository) GetOpsCostBreakdown",
		"top_models",
		"top_provider_accounts",
		"top_client_keys",
		"SUM(l.estimated_cost_microusd)",
		"ORDER BY 3 DESC",
		"NULLIF(l.provider_account_name, '')",
		"NULLIF(k.name, '')",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("ops cost breakdown source missing %q", want)
		}
	}
}

func TestListRequestLogsSupportsParameterizedFilters(t *testing.T) {
	since := time.Unix(2000, 0).UTC()
	whereSQL, args := requestLogFilterSQL(admin.RequestLogFilter{
		RequestID:         "req_3",
		Since:             since,
		Query:             "codex",
		StatusClass:       admin.RequestLogStatusServerError,
		StatusCode:        503,
		ProviderAccountID: 7,
		RoutingPoolID:     9,
		ClientKeyID:       12,
		Model:             "gpt-5",
		SessionID:         "workspace-123",
		Error:             "api_key_token_rate_limited",
		UsageSource:       "missing",
		RoutingPoolError:  "routing_pool_unavailable",
		RoutingPoolChain:  "primary -> secondary",
		GatewayFallbacks:  true,
	})
	if len(args) != 13 || args[0] != "req_3" || args[1] != since || args[2] != 503 || args[3] != int64(7) || args[4] != int64(9) || args[5] != int64(12) || args[6] != "gpt-5" || args[7] != "workspace-123" || args[8] != "api_key_token_rate_limited" || args[9] != "missing" || args[10] != "routing_pool_unavailable" || args[11] != "primary -> secondary" || args[12] != "codex" {
		t.Fatalf("args = %+v, want request ID req_3, since, status code 503, provider account 7, routing pool 9, client key 12, model gpt-5, session workspace-123, api_key_token_rate_limited, missing usage source, routing_pool_unavailable, routing pool chain, and codex args", args)
	}
	for _, want := range []string{
		"ILIKE '%' || $",
		"l.request_id = $",
		"l.created_at >= $",
		"l.status_code >= 500",
		"l.status_code = $",
		"l.provider_account_id = $",
		"l.routing_pool_id = $",
		"l.client_key_id = $",
		"l.model = $",
		"l.session_id = $",
		"l.error = $",
		"l.usage_source = $",
		"l.routing_pool_error = $",
		"l.routing_pool_fallback_chain = $",
		"l.gateway_fallback_count > 0",
		"l.request_id",
		"l.routing_pool_fallback_chain",
		"l.routing_pool_error",
		"l.error",
		"l.status_code::text",
	} {
		if !strings.Contains(whereSQL, want) {
			t.Fatalf("requestLogFilterSQL missing %q in %s", want, whereSQL)
		}
	}

	whereSQL, _ = requestLogFilterSQL(admin.RequestLogFilter{StatusClass: admin.RequestLogStatusClientError})
	if !strings.Contains(whereSQL, "l.status_code >= 400 AND l.status_code < 500") {
		t.Fatalf("client error filter SQL = %s", whereSQL)
	}

	whereSQL, _ = requestLogFilterSQL(admin.RequestLogFilter{StatusClass: admin.RequestLogStatusSuccess})
	if !strings.Contains(whereSQL, "l.status_code >= 200 AND l.status_code < 400") {
		t.Fatalf("success filter SQL = %s", whereSQL)
	}
}

func TestUsageSummaryProviderAccountGroupPrefersLoggedNameSnapshot(t *testing.T) {
	groupExpr, labelExpr, joinSQL, ok := usageSummaryGroupSQL("provider_account")
	if !ok {
		t.Fatal("usageSummaryGroupSQL(provider_account) ok = false, want true")
	}
	if !strings.Contains(groupExpr, "l.provider") || !strings.Contains(groupExpr, "l.provider_account_id") {
		t.Fatalf("provider account group expression = %q, want provider plus account id", groupExpr)
	}
	if !strings.Contains(labelExpr, "NULLIF(l.provider_account_name, '')") {
		t.Fatalf("provider account label expression = %q, want logged name snapshot first", labelExpr)
	}
	if !strings.Contains(labelExpr, "l.provider") || !strings.Contains(labelExpr, " / ") {
		t.Fatalf("provider account label expression = %q, want provider-prefixed label", labelExpr)
	}
	if !strings.Contains(joinSQL, "LEFT JOIN provider_accounts") {
		t.Fatalf("provider account join SQL = %q, want current account fallback join", joinSQL)
	}
}

func TestAdminRepositoryUsagePricingSettings(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()

	if _, err := repo.GetUsagePricing(ctx); !errors.Is(err, admin.ErrNotFound) {
		t.Fatalf("GetUsagePricing empty error = %v, want ErrNotFound", err)
	}

	saved, err := repo.SaveUsagePricing(ctx, admin.UsagePricing{
		Version:       1,
		Currency:      "USD",
		Unit:          "1M_tokens",
		IgnoredModels: []string{"gpt-5.3-chat-latest"},
		Models: map[string]admin.UsagePrice{
			"gpt-5": {
				InputMicrousdPerMillion:       1_000_000,
				CachedInputMicrousdPerMillion: 100_000,
				OutputMicrousdPerMillion:      4_000_000,
			},
		},
	})
	if err != nil {
		t.Fatalf("SaveUsagePricing returned error: %v", err)
	}

	found, err := repo.GetUsagePricing(ctx)
	if err != nil {
		t.Fatalf("GetUsagePricing returned error: %v", err)
	}
	if found.Currency != saved.Currency || found.Unit != saved.Unit || found.Models["gpt-5"].OutputMicrousdPerMillion != 4_000_000 {
		t.Fatalf("pricing = %+v, want saved pricing", found)
	}
	if !slices.Equal(found.IgnoredModels, saved.IgnoredModels) {
		t.Fatalf("IgnoredModels = %v, want %v", found.IgnoredModels, saved.IgnoredModels)
	}
}

func TestAdminRepositoryAPIKeyModelPolicyBehavior(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()

	created, err := repo.CreateAPIKey(ctx, "codex laptop", "hash-model-policy", "n2api_", "encrypted-secret", nil)
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}
	if created.ModelPolicy != admin.APIKeyModelPolicyAll {
		t.Fatalf("ModelPolicy = %q, want all", created.ModelPolicy)
	}
	if len(created.AllowedModels) != 0 {
		t.Fatalf("AllowedModels = %+v, want empty default", created.AllowedModels)
	}

	updated, err := repo.UpdateAPIKeyModelPolicy(ctx, created.ID, admin.APIKeyModelPolicySelected, []string{" gpt-5 ", "gpt-5-mini", "gpt-5"})
	if err != nil {
		t.Fatalf("UpdateAPIKeyModelPolicy selected returned error: %v", err)
	}
	if updated.ModelPolicy != admin.APIKeyModelPolicySelected || !slices.Equal(updated.AllowedModels, []string{"gpt-5", "gpt-5-mini"}) {
		t.Fatalf("updated key = %+v, want selected models", updated)
	}

	models, err := repo.ListAPIKeyModels(ctx, created.ID)
	if err != nil {
		t.Fatalf("ListAPIKeyModels returned error: %v", err)
	}
	if !slices.Equal(models, []string{"gpt-5", "gpt-5-mini"}) {
		t.Fatalf("models = %+v, want persisted selected models", models)
	}

	keys, err := repo.ListAPIKeys(ctx)
	if err != nil {
		t.Fatalf("ListAPIKeys returned error: %v", err)
	}
	if len(keys) != 1 || keys[0].ModelPolicy != admin.APIKeyModelPolicySelected || !slices.Equal(keys[0].AllowedModels, []string{"gpt-5", "gpt-5-mini"}) {
		t.Fatalf("keys = %+v, want selected policy with models", keys)
	}

	renamed, err := repo.UpdateAPIKeyName(ctx, created.ID, "renamed codex laptop")
	if err != nil {
		t.Fatalf("UpdateAPIKeyName returned error: %v", err)
	}
	if renamed.Name != "renamed codex laptop" || renamed.ModelPolicy != admin.APIKeyModelPolicySelected || !slices.Equal(renamed.AllowedModels, []string{"gpt-5", "gpt-5-mini"}) {
		t.Fatalf("renamed key = %+v, want renamed selected-policy key", renamed)
	}

	found, err := repo.FindAPIKeyByHash(ctx, "hash-model-policy", created.CreatedAt)
	if err != nil {
		t.Fatalf("FindAPIKeyByHash returned error: %v", err)
	}
	if found.ModelPolicy != admin.APIKeyModelPolicySelected || !slices.Equal(found.AllowedModels, []string{"gpt-5", "gpt-5-mini"}) {
		t.Fatalf("found key = %+v, want selected policy with models", found)
	}

	disabled, err := repo.SetAPIKeyDisabled(ctx, created.ID, true)
	if err != nil {
		t.Fatalf("SetAPIKeyDisabled true returned error: %v", err)
	}
	if disabled.DisabledAt == nil || disabled.ModelPolicy != admin.APIKeyModelPolicySelected || !slices.Equal(disabled.AllowedModels, []string{"gpt-5", "gpt-5-mini"}) {
		t.Fatalf("disabled key = %+v, want disabled selected-policy key", disabled)
	}
	if _, err := repo.FindAPIKeyByHash(ctx, "hash-model-policy", created.CreatedAt); !errors.Is(err, admin.ErrNotFound) {
		t.Fatalf("FindAPIKeyByHash disabled error = %v, want ErrNotFound", err)
	}
	enabled, err := repo.SetAPIKeyDisabled(ctx, created.ID, false)
	if err != nil {
		t.Fatalf("SetAPIKeyDisabled false returned error: %v", err)
	}
	if enabled.DisabledAt != nil {
		t.Fatalf("DisabledAt = %v, want nil after enable", enabled.DisabledAt)
	}
	if _, err := repo.FindAPIKeyByHash(ctx, "hash-model-policy", created.CreatedAt); err != nil {
		t.Fatalf("FindAPIKeyByHash after enable returned error: %v", err)
	}

	budgeted, err := repo.UpdateAPIKeyBudgets(ctx, created.ID, 12, 1200, 1500000, 300, 30000, 9000000)
	if err != nil {
		t.Fatalf("UpdateAPIKeyBudgets returned error: %v", err)
	}
	if budgeted.RequestBudget24h != 12 || budgeted.TokenBudget24h != 1200 || budgeted.CostBudgetMicrousd24h != 1500000 || budgeted.RequestBudget30d != 300 || budgeted.TokenBudget30d != 30000 || budgeted.CostBudgetMicrousd30d != 9000000 {
		t.Fatalf("budgeted key = %+v", budgeted)
	}
	keys, err = repo.ListAPIKeys(ctx)
	if err != nil {
		t.Fatalf("ListAPIKeys after budgets returned error: %v", err)
	}
	if len(keys) != 1 || keys[0].RequestBudget24h != 12 || keys[0].CostBudgetMicrousd24h != 1500000 || keys[0].TokenBudget30d != 30000 || keys[0].CostBudgetMicrousd30d != 9000000 {
		t.Fatalf("listed budget fields = %+v", keys)
	}

	cleared, err := repo.UpdateAPIKeyModelPolicy(ctx, created.ID, admin.APIKeyModelPolicyAll, nil)
	if err != nil {
		t.Fatalf("UpdateAPIKeyModelPolicy all returned error: %v", err)
	}
	if cleared.ModelPolicy != admin.APIKeyModelPolicyAll || len(cleared.AllowedModels) != 0 {
		t.Fatalf("cleared key = %+v, want all policy with no models", cleared)
	}
	models, err = repo.ListAPIKeyModels(ctx, created.ID)
	if err != nil {
		t.Fatalf("ListAPIKeyModels after clear returned error: %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("models after clear = %+v, want empty", models)
	}

	revoked, err := repo.RevokeAPIKey(ctx, created.ID)
	if err != nil {
		t.Fatalf("RevokeAPIKey returned error: %v", err)
	}
	if revoked.ModelPolicy != admin.APIKeyModelPolicyAll {
		t.Fatalf("revoked ModelPolicy = %q, want all", revoked.ModelPolicy)
	}
	if _, err := repo.UpdateAPIKeyModelPolicy(ctx, created.ID, admin.APIKeyModelPolicySelected, []string{"gpt-5"}); !errors.Is(err, admin.ErrNotFound) {
		t.Fatalf("UpdateAPIKeyModelPolicy revoked error = %v, want ErrNotFound", err)
	}
	if _, err := repo.UpdateAPIKeyName(ctx, created.ID, "revoked rename"); !errors.Is(err, admin.ErrNotFound) {
		t.Fatalf("UpdateAPIKeyName revoked error = %v, want ErrNotFound", err)
	}
	if _, err := repo.SetAPIKeyDisabled(ctx, created.ID, true); !errors.Is(err, admin.ErrNotFound) {
		t.Fatalf("SetAPIKeyDisabled revoked error = %v, want ErrNotFound", err)
	}
	if _, err := repo.UpdateAPIKeyBudgets(ctx, created.ID, 1, 1, 1, 1, 1, 1); !errors.Is(err, admin.ErrNotFound) {
		t.Fatalf("UpdateAPIKeyBudgets revoked error = %v, want ErrNotFound", err)
	}
}

func TestPurgeRevokedAPIKeysRemovesOnlyExpiredRevokedKeys(t *testing.T) {
	ctx := context.Background()
	repo := newTestAdminRepository(t)
	oldKey, err := repo.CreateAPIKey(ctx, "old deleted", "hash-old-deleted", "n2_old", "encrypted-old", nil)
	if err != nil {
		t.Fatalf("CreateAPIKey old returned error: %v", err)
	}
	recentKey, err := repo.CreateAPIKey(ctx, "recent deleted", "hash-recent-deleted", "n2_recent", "encrypted-recent", nil)
	if err != nil {
		t.Fatalf("CreateAPIKey recent returned error: %v", err)
	}
	disabledKey, err := repo.CreateAPIKey(ctx, "disabled", "hash-disabled", "n2_disabled", "encrypted-disabled", nil)
	if err != nil {
		t.Fatalf("CreateAPIKey disabled returned error: %v", err)
	}
	activeKey, err := repo.CreateAPIKey(ctx, "active", "hash-active", "n2_active", "encrypted-active", nil)
	if err != nil {
		t.Fatalf("CreateAPIKey active returned error: %v", err)
	}
	cutoff := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	if _, err := repo.pool.Exec(ctx, `UPDATE client_api_keys SET revoked_at = $2 WHERE id = $1`, oldKey.ID, cutoff.Add(-time.Second)); err != nil {
		t.Fatalf("mark old revoked: %v", err)
	}
	if _, err := repo.pool.Exec(ctx, `UPDATE client_api_keys SET revoked_at = $2 WHERE id = $1`, recentKey.ID, cutoff.Add(time.Second)); err != nil {
		t.Fatalf("mark recent revoked: %v", err)
	}
	if _, err := repo.SetAPIKeyDisabled(ctx, disabledKey.ID, true); err != nil {
		t.Fatalf("SetAPIKeyDisabled returned error: %v", err)
	}

	deleted, err := repo.PurgeRevokedAPIKeys(ctx, cutoff)
	if err != nil {
		t.Fatalf("PurgeRevokedAPIKeys returned error: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	keys, err := repo.ListAPIKeys(ctx)
	if err != nil {
		t.Fatalf("ListAPIKeys returned error: %v", err)
	}
	ids := map[int64]bool{}
	for _, key := range keys {
		ids[key.ID] = true
	}
	if ids[oldKey.ID] {
		t.Fatalf("old revoked key remained after purge")
	}
	if !ids[recentKey.ID] || !ids[disabledKey.ID] || !ids[activeKey.ID] {
		t.Fatalf("remaining keys = %+v, want recent, disabled, and active keys", ids)
	}
}

func TestDeleteRevokedAPIKeyRejectsActiveAndDeletesRevokedKey(t *testing.T) {
	ctx := context.Background()
	repo := newTestAdminRepository(t)
	active, err := repo.CreateAPIKey(ctx, "active", "hash-physical-active", "n2_active", "encrypted-active", nil)
	if err != nil {
		t.Fatalf("CreateAPIKey active returned error: %v", err)
	}
	revoked, err := repo.CreateAPIKey(ctx, "deleted", "hash-physical-deleted", "n2_deleted", "encrypted-deleted", nil)
	if err != nil {
		t.Fatalf("CreateAPIKey deleted returned error: %v", err)
	}
	if _, err := repo.RevokeAPIKey(ctx, revoked.ID); err != nil {
		t.Fatalf("RevokeAPIKey returned error: %v", err)
	}

	if err := repo.DeleteRevokedAPIKey(ctx, active.ID); !errors.Is(err, admin.ErrNotFound) {
		t.Fatalf("DeleteRevokedAPIKey active error = %v, want ErrNotFound", err)
	}
	if err := repo.DeleteRevokedAPIKey(ctx, revoked.ID); err != nil {
		t.Fatalf("DeleteRevokedAPIKey revoked returned error: %v", err)
	}

	keys, err := repo.ListAPIKeys(ctx)
	if err != nil {
		t.Fatalf("ListAPIKeys returned error: %v", err)
	}
	if len(keys) != 1 || keys[0].ID != active.ID {
		t.Fatalf("keys = %+v, want only active key %d", keys, active.ID)
	}
}

func TestAdminRepositoryAPIKeyBudgetUsageAggregatesWindows(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	now := time.Unix(20_000, 0).UTC()

	key, err := repo.CreateAPIKey(ctx, "budgeted", "hash-budgeted", "n2api_", "encrypted-budgeted", nil)
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}
	other, err := repo.CreateAPIKey(ctx, "other", "hash-other", "n2api_", "encrypted-other", nil)
	if err != nil {
		t.Fatalf("CreateAPIKey other returned error: %v", err)
	}
	insertRequestLog(t, repo.pool, key.ID, now.Add(-time.Hour), 200, 40, 400)
	insertRequestLog(t, repo.pool, key.ID, now.Add(-23*time.Hour), 200, 60, 600)
	insertRequestLog(t, repo.pool, key.ID, now.Add(-25*time.Hour), 200, 90, 900)
	insertRequestLog(t, repo.pool, key.ID, now.Add(-31*24*time.Hour), 200, 900, 9000)
	insertRequestLog(t, repo.pool, other.ID, now.Add(-time.Hour), 200, 700, 7000)

	usage, err := repo.GetAPIKeyBudgetUsage(ctx, key.ID, now)
	if err != nil {
		t.Fatalf("GetAPIKeyBudgetUsage returned error: %v", err)
	}
	if usage.KeyID != key.ID || usage.RequestsUsed24h != 2 || usage.TokensUsed24h != 100 || usage.CostMicrousd24h != 1000 || usage.RequestsUsed30d != 3 || usage.TokensUsed30d != 190 || usage.CostMicrousd30d != 1900 {
		t.Fatalf("usage = %+v, want key usage across 24h and 30d windows", usage)
	}
}

func TestAdminRepositoryRoutingPools(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()

	accountID := insertProviderAccount(t, repo.pool, "openai", "api_upstream", "upstream")

	pool, err := repo.CreateRoutingPool(ctx, "codex primary", "daily pool", true, nil)
	if err != nil {
		t.Fatalf("CreateRoutingPool returned error: %v", err)
	}
	if pool.Name != "codex primary" || pool.Description != "daily pool" || !pool.Enabled {
		t.Fatalf("pool = %+v, want created pool", pool)
	}

	pool, err = repo.ReplaceRoutingPoolAccounts(ctx, pool.ID, []admin.RoutingPoolAccount{{AccountID: accountID, Priority: 5}})
	if err != nil {
		t.Fatalf("ReplaceRoutingPoolAccounts returned error: %v", err)
	}
	if len(pool.Accounts) != 1 || pool.Accounts[0].AccountID != accountID || pool.Accounts[0].Priority != 5 {
		t.Fatalf("pool accounts = %+v, want account membership", pool.Accounts)
	}
	if len(pool.AccountIDs) != 1 || pool.AccountIDs[0] != accountID {
		t.Fatalf("pool account ids = %+v, want account id %d", pool.AccountIDs, accountID)
	}

	key, err := repo.CreateAPIKey(ctx, "codex laptop", "hash-routing-pool", "n2api_", "encrypted-routing-pool", &pool.ID)
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}
	if key.RoutingPoolID == nil || *key.RoutingPoolID != pool.ID || key.RoutingPoolName != "codex primary" {
		t.Fatalf("created key routing pool = %+v, want pool binding", key)
	}
	key, err = repo.UpdateAPIKeyRoutingPool(ctx, key.ID, &pool.ID)
	if err != nil {
		t.Fatalf("UpdateAPIKeyRoutingPool returned error: %v", err)
	}
	if key.RoutingPoolID == nil || *key.RoutingPoolID != pool.ID || key.RoutingPoolName != "codex primary" {
		t.Fatalf("key routing pool = %+v, want pool binding", key)
	}

	keys, err := repo.ListAPIKeys(ctx)
	if err != nil {
		t.Fatalf("ListAPIKeys returned error: %v", err)
	}
	if len(keys) != 1 || keys[0].RoutingPoolID == nil || *keys[0].RoutingPoolID != pool.ID || keys[0].RoutingPoolName != "codex primary" {
		t.Fatalf("keys = %+v, want pool binding in list", keys)
	}
}

func newTestAdminRepository(t *testing.T) *AdminRepository {
	t.Helper()

	dsn := os.Getenv("N2API_STORE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("N2API_STORE_TEST_DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool.New returned error: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := RunMigrations(context.Background(), pool); err != nil {
		t.Fatalf("RunMigrations returned error: %v", err)
	}

	if _, err := pool.Exec(context.Background(), truncateStoreTestDataSQL); err != nil {
		t.Fatalf("test database cleanup failed: %v", err)
	}
	return NewAdminRepository(pool, "store-test-cursor-secret")
}

func TestAdminRepositoryPagesRequestLogsWithoutDuplicatesOrOmissions(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	key, err := repo.CreateAPIKey(ctx, "cursor-key", "hash-cursor-key", "n2api_", "encrypted-cursor-key", nil)
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}
	base := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	for _, createdAt := range []time.Time{
		base.Add(-3 * time.Minute),
		base.Add(-2 * time.Minute),
		base.Add(-time.Minute),
		base,
		base,
	} {
		insertRequestLog(t, repo.pool, key.ID, createdAt, 200, 10, 100)
	}
	expectedIDs := map[int64]bool{}
	rows, err := repo.pool.Query(ctx, "SELECT id FROM request_logs")
	if err != nil {
		t.Fatalf("query original request log IDs: %v", err)
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatalf("scan original request log ID: %v", err)
		}
		expectedIDs[id] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatalf("iterate original request log IDs: %v", err)
	}
	rows.Close()

	filter := admin.RequestLogFilter{Limit: 2, StatusClass: admin.RequestLogStatusAll}
	first, err := repo.ListRequestLogs(ctx, filter)
	if err != nil {
		t.Fatalf("ListRequestLogs first page returned error: %v", err)
	}
	if len(first.Logs) != 2 || !first.HasMore || first.NextCursor == "" {
		t.Fatalf("first page = %+v, want two logs and a next cursor", first)
	}
	if !first.Logs[0].CreatedAt.Equal(first.Logs[1].CreatedAt) || first.Logs[0].ID <= first.Logs[1].ID {
		t.Fatalf("equal-timestamp order = (%s,%d), (%s,%d), want ID descending", first.Logs[0].CreatedAt, first.Logs[0].ID, first.Logs[1].CreatedAt, first.Logs[1].ID)
	}

	if _, err := repo.pool.Exec(ctx, "DELETE FROM request_logs WHERE id = $1", first.Logs[1].ID); err != nil {
		t.Fatalf("delete page boundary row: %v", err)
	}
	insertRequestLog(t, repo.pool, key.ID, base.Add(time.Minute), 200, 10, 100)
	var concurrentID int64
	if err := repo.pool.QueryRow(ctx, "SELECT max(id) FROM request_logs").Scan(&concurrentID); err != nil {
		t.Fatalf("query concurrently inserted request log ID: %v", err)
	}

	filter.Cursor = first.NextCursor
	second, err := repo.ListRequestLogs(ctx, filter)
	if err != nil {
		t.Fatalf("ListRequestLogs second page returned error: %v", err)
	}
	if len(second.Logs) != 2 || !second.HasMore || second.NextCursor == "" {
		t.Fatalf("second page = %+v, want two logs and a next cursor", second)
	}
	filter.Cursor = second.NextCursor
	third, err := repo.ListRequestLogs(ctx, filter)
	if err != nil {
		t.Fatalf("ListRequestLogs third page returned error: %v", err)
	}
	if len(third.Logs) != 1 || third.HasMore || third.NextCursor != "" {
		t.Fatalf("third page = %+v, want one terminal log", third)
	}

	seen := map[int64]bool{}
	for _, page := range []admin.RequestLogPage{first, second, third} {
		for _, log := range page.Logs {
			if seen[log.ID] {
				t.Fatalf("request log %d appeared more than once", log.ID)
			}
			seen[log.ID] = true
		}
	}
	if len(seen) != 5 {
		t.Fatalf("traversed %d unique logs, want 5", len(seen))
	}
	if seen[concurrentID] {
		t.Fatalf("newer request log %d appeared in older-page traversal", concurrentID)
	}
	for id := range expectedIDs {
		if !seen[id] {
			t.Fatalf("original request log %d was omitted", id)
		}
	}

	mismatched := admin.RequestLogFilter{
		Limit:       2,
		Cursor:      first.NextCursor,
		StatusClass: admin.RequestLogStatusServerError,
	}
	if _, err := repo.ListRequestLogs(ctx, mismatched); !errors.Is(err, admin.ErrInvalidInput) {
		t.Fatalf("filter-mismatched ListRequestLogs error = %v, want ErrInvalidInput", err)
	}
}

func TestAdminRepositoryRequestLogRetentionDeletesBoundedBatchesAndReportsStats(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	key, err := repo.CreateAPIKey(ctx, "retention-key", "hash-retention-key", "n2api_", "encrypted-retention-key", nil)
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}
	cutoff := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	for i := 5; i >= 1; i-- {
		insertRequestLog(t, repo.pool, key.ID, cutoff.Add(-time.Duration(i)*time.Minute), 200, 10, 100)
	}
	insertRequestLog(t, repo.pool, key.ID, cutoff, 200, 10, 100)
	insertRequestLog(t, repo.pool, key.ID, cutoff.Add(time.Minute), 200, 10, 100)
	if _, err := repo.pool.Exec(ctx, "ANALYZE request_logs"); err != nil {
		t.Fatalf("ANALYZE request_logs returned error: %v", err)
	}

	stats, err := repo.GetRequestLogRetentionStats(ctx, cutoff)
	if err != nil {
		t.Fatalf("GetRequestLogRetentionStats returned error: %v", err)
	}
	if stats.EligibleCount != 5 || stats.TotalCountEstimate != 7 {
		t.Fatalf("stats counts = eligible %d estimate %d, want 5 and 7", stats.EligibleCount, stats.TotalCountEstimate)
	}
	if stats.OldestLogAt == nil || !stats.OldestLogAt.Equal(cutoff.Add(-5*time.Minute)) ||
		stats.NewestLogAt == nil || !stats.NewestLogAt.Equal(cutoff.Add(time.Minute)) {
		t.Fatalf("stats timestamps = oldest %v newest %v", stats.OldestLogAt, stats.NewestLogAt)
	}

	lease, acquired, err := repo.TryAcquireRequestLogRetention(ctx)
	if err != nil || !acquired {
		t.Fatalf("TryAcquireRequestLogRetention = acquired %v error %v", acquired, err)
	}
	for batch, want := range []int64{2, 2, 1, 0} {
		deleted, err := lease.DeleteBeforeBatch(ctx, cutoff, 2)
		if err != nil {
			t.Fatalf("DeleteBeforeBatch %d returned error: %v", batch, err)
		}
		if deleted != want {
			t.Fatalf("DeleteBeforeBatch %d deleted %d, want %d", batch, deleted, want)
		}
	}
	if err := lease.Close(); err != nil {
		t.Fatalf("retention lease Close returned error: %v", err)
	}
	var remaining int
	if err := repo.pool.QueryRow(ctx, "SELECT count(*) FROM request_logs").Scan(&remaining); err != nil {
		t.Fatalf("count remaining request logs: %v", err)
	}
	if remaining != 2 {
		t.Fatalf("remaining request logs = %d, want cutoff boundary and newer row", remaining)
	}
}

func TestAdminRepositoryRequestLogRetentionLockContentionAndReacquire(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	first, acquired, err := repo.TryAcquireRequestLogRetention(ctx)
	if err != nil || !acquired {
		t.Fatalf("first acquire = %v, %v", acquired, err)
	}
	second, acquired, err := repo.TryAcquireRequestLogRetention(ctx)
	if err != nil {
		t.Fatalf("contended acquire returned error: %v", err)
	}
	if acquired || second != nil {
		t.Fatal("contended acquire unexpectedly obtained the advisory lock")
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first Close returned error: %v", err)
	}
	third, acquired, err := repo.TryAcquireRequestLogRetention(ctx)
	if err != nil || !acquired {
		t.Fatalf("reacquire = %v, %v", acquired, err)
	}
	if err := third.Close(); err != nil {
		t.Fatalf("third Close returned error: %v", err)
	}
}

func TestAdminRepositoryRequestLogRetentionCancellationKeepsCommittedBatch(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	key, err := repo.CreateAPIKey(ctx, "cancel-retention-key", "hash-cancel-retention-key", "n2api_", "encrypted-cancel-retention-key", nil)
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}
	cutoff := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	for i := 1; i <= 3; i++ {
		insertRequestLog(t, repo.pool, key.ID, cutoff.Add(-time.Duration(i)*time.Minute), 200, 10, 100)
	}
	lease, acquired, err := repo.TryAcquireRequestLogRetention(ctx)
	if err != nil || !acquired {
		t.Fatalf("acquire = %v, %v", acquired, err)
	}
	if deleted, err := lease.DeleteBeforeBatch(ctx, cutoff, 1); err != nil || deleted != 1 {
		t.Fatalf("first batch = deleted %d, error %v", deleted, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := lease.DeleteBeforeBatch(canceled, cutoff, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled batch error = %v, want context.Canceled", err)
	}
	_ = lease.Close()
	var remaining int
	if err := repo.pool.QueryRow(ctx, "SELECT count(*) FROM request_logs").Scan(&remaining); err != nil {
		t.Fatalf("count remaining request logs: %v", err)
	}
	if remaining != 2 {
		t.Fatalf("remaining request logs = %d, want two after one committed batch", remaining)
	}
	next, acquired, err := repo.TryAcquireRequestLogRetention(ctx)
	if err != nil || !acquired {
		t.Fatalf("reacquire after canceled connection = %v, %v", acquired, err)
	}
	if err := next.Close(); err != nil {
		t.Fatalf("Close reacquired lease returned error: %v", err)
	}
}

func TestAdminRepositoryRequestLogRetentionStatsAreEmptySafe(t *testing.T) {
	repo := newTestAdminRepository(t)
	stats, err := repo.GetRequestLogRetentionStats(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatalf("GetRequestLogRetentionStats returned error: %v", err)
	}
	if stats.OldestLogAt != nil || stats.NewestLogAt != nil || stats.EligibleCount != 0 || stats.TotalCountEstimate != 0 {
		t.Fatalf("empty stats = %+v", stats)
	}
}

func TestAdminRepositoryManagesOwnedActiveSessions(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := context.Background()
	owner, err := repo.CreateAdmin(ctx, "session-owner", "hash")
	if err != nil {
		t.Fatalf("CreateAdmin returned error: %v", err)
	}
	otherOwner, err := repo.CreateAdmin(ctx, "other-owner", "hash")
	if err != nil {
		t.Fatalf("CreateAdmin(other) returned error: %v", err)
	}
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	metadata := admin.SessionMetadata{CreatedIP: "192.0.2.0/24", UserAgent: "test-agent"}
	if err := repo.CreateSession(ctx, owner.ID, "current-hash", metadata, now.Add(-2*time.Minute), now.Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession(current) returned error: %v", err)
	}
	if err := repo.CreateSession(ctx, owner.ID, "other-hash", metadata, now.Add(-time.Minute), now.Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession(other) returned error: %v", err)
	}
	if err := repo.CreateSession(ctx, owner.ID, "expired-hash", metadata, now.Add(-2*time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatalf("CreateSession(expired) returned error: %v", err)
	}

	if _, err := repo.FindAdminBySessionHash(ctx, "current-hash", now); err != nil {
		t.Fatalf("FindAdminBySessionHash returned error: %v", err)
	}
	var touchedAt time.Time
	if err := repo.pool.QueryRow(ctx, `SELECT last_used_at FROM admin_sessions WHERE token_hash = 'current-hash'`).Scan(&touchedAt); err != nil {
		t.Fatalf("query touched session: %v", err)
	}
	if !touchedAt.Equal(now) {
		t.Fatalf("last_used_at = %v, want %v", touchedAt, now)
	}
	if _, err := repo.FindAdminBySessionHash(ctx, "current-hash", now.Add(30*time.Second)); err != nil {
		t.Fatalf("second FindAdminBySessionHash returned error: %v", err)
	}
	if err := repo.pool.QueryRow(ctx, `SELECT last_used_at FROM admin_sessions WHERE token_hash = 'current-hash'`).Scan(&touchedAt); err != nil {
		t.Fatalf("query conditionally touched session: %v", err)
	}
	if !touchedAt.Equal(now) {
		t.Fatalf("last_used_at = %v after 30 seconds, want unchanged %v", touchedAt, now)
	}

	sessions, err := repo.ListAdminSessions(ctx, owner.ID, "current-hash", now)
	if err != nil {
		t.Fatalf("ListAdminSessions returned error: %v", err)
	}
	if len(sessions) != 2 || !sessions[0].Current || sessions[0].TokenHash != "current-hash" {
		t.Fatalf("sessions = %+v, want current active session first", sessions)
	}
	if _, err := repo.RevokeAdminSession(ctx, otherOwner.ID, sessions[1].ID, now); !errors.Is(err, admin.ErrNotFound) {
		t.Fatalf("cross-owner revoke error = %v, want ErrNotFound", err)
	}
	var expiredID int64
	if err := repo.pool.QueryRow(ctx, `SELECT id FROM admin_sessions WHERE token_hash = 'expired-hash'`).Scan(&expiredID); err != nil {
		t.Fatalf("query expired session ID: %v", err)
	}
	if _, err := repo.RevokeAdminSession(ctx, owner.ID, expiredID, now); !errors.Is(err, admin.ErrNotFound) {
		t.Fatalf("expired session revoke error = %v, want ErrNotFound", err)
	}

	revokeCtx := systemevent.WithRequestContext(ctx, systemevent.RequestContext{
		CorrelationID: "revoke-owned-session",
		Actor:         systemevent.Actor{Type: systemevent.ActorAdmin, ID: owner.ID, Name: owner.Username},
	})
	revokeCtx = systemevent.WithIntent(revokeCtx, systemevent.EventIntent{
		Category: systemevent.CategorySecurity, Severity: systemevent.SeverityInfo,
		Action: systemevent.ActionAuthSessionRevoked, Outcome: systemevent.OutcomeSuccess,
	})
	revoked, err := repo.RevokeAdminSession(revokeCtx, owner.ID, sessions[1].ID, now)
	if err != nil {
		t.Fatalf("RevokeAdminSession returned error: %v", err)
	}
	if revoked.TokenHash != "other-hash" {
		t.Fatalf("revoked token hash = %q, want other-hash", revoked.TokenHash)
	}
	if _, err := repo.RevokeAdminSession(revokeCtx, owner.ID, sessions[1].ID, now); !errors.Is(err, admin.ErrNotFound) {
		t.Fatalf("repeat revoke error = %v, want ErrNotFound", err)
	}

	if err := repo.CreateSession(ctx, owner.ID, "new-other-hash", metadata, now, now.Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession(new other) returned error: %v", err)
	}
	othersCtx := systemevent.WithRequestContext(ctx, systemevent.RequestContext{
		CorrelationID: "revoke-other-sessions",
		Actor:         systemevent.Actor{Type: systemevent.ActorAdmin, ID: owner.ID, Name: owner.Username},
	})
	othersCtx = systemevent.WithIntent(othersCtx, systemevent.EventIntent{
		Category: systemevent.CategorySecurity, Severity: systemevent.SeverityInfo,
		Action: systemevent.ActionAuthSessionsRevokedOthers, Outcome: systemevent.OutcomeSuccess,
	})
	count, err := repo.RevokeOtherAdminSessions(othersCtx, owner.ID, "current-hash", now)
	if err != nil {
		t.Fatalf("RevokeOtherAdminSessions returned error: %v", err)
	}
	if count != 1 {
		t.Fatalf("RevokeOtherAdminSessions count = %d, want 1", count)
	}
	if _, err := repo.FindAdminBySessionHash(ctx, "current-hash", now); err != nil {
		t.Fatalf("current session was revoked: %v", err)
	}

	var metadataJSON string
	if err := repo.pool.QueryRow(ctx, `
		SELECT metadata::text FROM system_events
		WHERE action = $1 ORDER BY id DESC LIMIT 1
	`, systemevent.ActionAuthSessionRevoked).Scan(&metadataJSON); err != nil {
		t.Fatalf("query revoke audit metadata: %v", err)
	}
	for _, forbidden := range []string{"other-hash", metadata.CreatedIP, metadata.UserAgent} {
		if strings.Contains(metadataJSON, forbidden) {
			t.Fatalf("audit metadata %s contains sensitive session value %q", metadataJSON, forbidden)
		}
	}
}

func TestAdminRepositoryMutationCommitsSystemEventAtomically(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := systemevent.WithRequestContext(context.Background(), systemevent.RequestContext{
		CorrelationID: "admin-atomic-success",
		Actor:         systemevent.Actor{Type: systemevent.ActorAdmin, ID: 1, Name: "admin"},
	})
	ctx = systemevent.WithIntent(ctx, systemevent.EventIntent{
		Category: systemevent.CategoryAudit,
		Severity: systemevent.SeverityInfo,
		Action:   systemevent.ActionAPIKeyCreated,
		Outcome:  systemevent.OutcomeSuccess,
	})

	key, err := repo.CreateAPIKey(ctx, "atomic key", "atomic-hash", "n2api_", "encrypted", nil)
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}

	var targetID, targetName string
	if err := repo.pool.QueryRow(context.Background(), `
		SELECT target_id, target_name FROM system_events WHERE action = $1
	`, systemevent.ActionAPIKeyCreated).Scan(&targetID, &targetName); err != nil {
		t.Fatalf("query system event: %v", err)
	}
	if targetID != strconv.FormatInt(key.ID, 10) || targetName != "atomic key" {
		t.Fatalf("event target = %q/%q, want %d/atomic key", targetID, targetName, key.ID)
	}
}

func TestAdminRepositoryInvalidSystemEventRollsBackMutation(t *testing.T) {
	repo := newTestAdminRepository(t)
	ctx := systemevent.WithIntent(context.Background(), systemevent.EventIntent{
		Category: systemevent.CategoryAudit,
		Severity: systemevent.SeverityInfo,
		Action:   systemevent.ActionAPIKeyCreated,
		Outcome:  systemevent.OutcomeSuccess,
		Metadata: map[string]any{"access_token": "must-not-be-stored"},
	})

	if _, err := repo.CreateAPIKey(ctx, "rollback key", "rollback-hash", "n2api_", "encrypted", nil); err == nil {
		t.Fatal("CreateAPIKey returned nil error, want invalid system event error")
	}

	var count int
	if err := repo.pool.QueryRow(context.Background(), `SELECT count(*) FROM client_api_keys WHERE name = 'rollback key'`).Scan(&count); err != nil {
		t.Fatalf("count client API keys: %v", err)
	}
	if count != 0 {
		t.Fatalf("client API key count = %d, want rollback", count)
	}
}

func insertProviderAccount(t *testing.T, pool *pgxpool.Pool, providerName, accountType, name string) int64 {
	t.Helper()

	var accountID int64
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO provider_accounts (provider, account_type, name, subject, display_name, enabled, priority)
		VALUES ($1, $2, $3, $4, $5, true, 100)
		RETURNING id
	`, providerName, accountType, name, name+"-subject", name).Scan(&accountID); err != nil {
		t.Fatalf("insert provider account failed: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO provider_account_credentials (account_id, credential_type, encrypted_api_key, base_url)
		VALUES ($1, 'api_key', 'encrypted', 'https://upstream.example.test')
	`, accountID); err != nil {
		t.Fatalf("insert provider account credentials failed: %v", err)
	}
	return accountID
}

func insertRequestLog(t *testing.T, pool *pgxpool.Pool, keyID int64, createdAt time.Time, statusCode, totalTokens int, costMicrousd int64) {
	t.Helper()

	requestID := "req_budget_" + strconv.FormatInt(createdAt.UnixNano(), 10)
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (
			request_id, client_key_id, provider, route, method, status_code, latency_ms, total_tokens, estimated_cost_microusd, created_at
		)
		VALUES ($1, $2, 'openai', '/v1/responses', 'POST', $3, 12, $4, $5, $6)
	`, requestID, keyID, statusCode, totalTokens, costMicrousd, createdAt); err != nil {
		t.Fatalf("insert request log failed: %v", err)
	}
}
