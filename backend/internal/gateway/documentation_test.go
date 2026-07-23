package gateway

import (
	"os"
	"strings"
	"testing"
)

func TestGatewayModelDocumentationMentionsAPIKeyPolicyFiltering(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"/v1/models",
			"API key",
			"selected",
			"intersection",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in /v1/models API key policy documentation", path, want)
			}
		}
	}
}

func TestGatewayModelDocumentationMentionsRoutingPoolFiltering(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"/v1/models",
			"unbound",
			"routing-pool fallback chain",
			"selected",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in /v1/models routing pool documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsStickySessionProxyHeaderRequirement(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"session_id",
			"X-N2API-Session-ID",
			"underscores_in_headers on;",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in sticky session proxy documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsPersistentStickySessionBindings(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"persisted by provider, model, and `session_id`",
			"bound account",
			"rebind",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in persistent sticky session documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsAPIKeyLimitInheritance(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Per-key values set to `0` inherit",
			"gateway default",
			"do not disable",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in API key limit inheritance documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMatchesRequestBodyBoundaryContract(t *testing.T) {
	checks := map[string][]string{
		"../../../.env.example": {
			"N2API_GATEWAY_MAX_ACCEPTED_REQUEST_BODY_BYTES=4194304",
			"N2API_GATEWAY_MAX_IN_MEMORY_REPLAY_BODY_BYTES=1048576",
			"N2API_GATEWAY_MAX_UPSTREAM_RESPONSE_BODY_BYTES=8388608",
		},
		"../../../docs/manual.md": {
			"hard limit for both known-length and chunked requests",
			"stable code `request_too_large`",
			"at most one upstream attempt",
			"admission occurs before the complete body read",
			"stable code `upstream_response_too_large`",
			"SSE remains streaming",
			"per-request read deadline",
			"`408` with `request_body_timeout`",
			"global HTTP `WriteTimeout`",
			"stable code `upstream_timeout`",
			"continuous no-data limit",
			"`upstream_sse_idle_timeout`",
		},
	}
	for path, wants := range checks {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		for _, want := range wants {
			if !strings.Contains(string(content), want) {
				t.Fatalf("%s missing %q in request body boundary documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsAPIKeyActiveConcurrency(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"API Keys page shows active concurrency",
			"process-local",
			"Concurrency full",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in API key active concurrency documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsAPIKeyRateWindowVisibility(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Requests window",
			"Tokens window",
			"remaining capacity",
			"process-local fixed one-minute",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in API key rate window visibility documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsAPIKeyListFiltering(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"API Keys page supports local search and status filtering",
			"name, prefix, model policy, selected model, active/disabled/deleted status, and limiter state",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in API key list filtering documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsAPIKeyRename(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"API key names can be renamed",
			"without rotating the secret",
			"encrypted reusable secret",
			"Prefix column on the API Keys page can copy the full API key again after creation",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in API key rename documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsAPIKeyDisable(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"API keys have three visible states",
			"disabled keys cannot authenticate gateway requests",
			"7 day retention window",
			"physically removed by startup and hourly cleanup",
			"physically deleted immediately with a second confirmed Delete action",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in API key disable documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsAPIKeyBudgets(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"API key budgets",
			"request, token, and estimated cost budgets over rolling 24h and 30d windows",
			"cost budgets use stored estimated request cost",
			"`0` disables a budget field",
			"`api_key_request_budget_exceeded`",
			"`api_key_token_budget_exceeded`",
			"`api_key_cost_budget_exceeded`",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in API key budget documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsRoutingPools(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Routing pools",
			"unbound",
			"explicit fallback chain",
			"cannot route model requests",
			"`routing_pool_required`",
			"`routing_pool_unavailable`",
			"`routing_pool_empty`",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in routing pool documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsPreciseLocalLimitLogReasons(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Request Logs",
			"`rate_limit_exceeded`",
			"`api_key_request_rate_limited`",
			"`api_key_token_rate_limited`",
			"`gateway_concurrency_limited`",
			"`api_key_concurrency_limited`",
			"`provider_account_concurrency_limited`",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in local limit request-log documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsRequestLogFallbackDiagnostics(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Request Logs",
			"gateway fallback diagnostics",
			"attempts",
			"fallbacks",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in request-log fallback diagnostics documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsRoutingPoolFallback(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Routing pool fallback",
			"no implicit fallback outside the chain",
			"`routing_pool_disabled`",
			"`routing_pool_empty`",
			"`routing_pool_cycle`",
			"`routing_pool_exhausted`",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in routing pool fallback documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsAllUsageLogDrilldowns(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Top provider accounts",
			"Top usage sources",
			"Top routing pools",
			"Top routing pool chains",
			"Top client keys",
			"Gateway management and Dashboard",
			"provider-account, usage-source, routing-pool, routing-pool-chain, API-key, model, and sticky-session",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in gateway usage log drilldown documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsReadinessRefresh(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Gateway management refreshes provider accounts, model routing, and API keys",
			"opened directly",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in gateway readiness refresh documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsAPIUpstreamCredentialRotation(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"API upstream credentials can be updated",
			"base URL",
			"per-account outbound proxy URL",
			"redacted proxy summary",
			"clears local failure status",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in API upstream credential rotation documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsProviderAccountFingerprintProfileCreation(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Fingerprint profile",
			"at creation time",
			"pending OAuth state",
			"callback completion",
			"API upstream selections",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in provider account fingerprint profile creation documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationOmitsGlobalAllowedModelList(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		if strings.Contains(strings.ToLower(text), "global allowed model list") {
			t.Fatalf("%s still documents the removed global allowed model list", path)
		}
	}
}

func TestGatewayDocumentationMentionsProviderAccountLoadFactor(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"load factor",
			"strict descending preference tier",
			"not a proportional request weight",
			"least-recently-used time and account ID",
			"sticky FNV hashing only changes order inside the highest tier",
			"concurrency-full accounts are excluded",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in provider account load factor documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsProviderAccountConcurrencyOverride(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Max concurrency",
			"inherits the gateway default",
			"per-account concurrency",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in provider account concurrency documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsProviderAccountActiveConcurrency(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"active concurrency",
			"process-local",
			"unlimited",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in provider account active concurrency documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsProviderAccountDisconnect(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Disconnect account",
			"deletes the provider account",
			"stops scheduling it",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in provider account disconnect documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsRoutingPreviewConcurrency(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Routing preview",
			"Routing pool",
			"active concurrency",
			"Concurrency full",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in routing preview concurrency documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsRoutingPreviewScheduleReasons(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Schedule reason",
			"diagnostic text",
			"pool/global priority tier",
			"recent-error tier",
			"least-recently-used tie-breaker",
			"does not change scheduler behavior",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in routing preview schedule reason documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsProviderAccountTestProbe(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Test account",
			"probes one provider account",
			"Only a 2xx response confirms recovery",
			"network errors and every non-2xx response record a failed test",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in provider account test probe documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsConfirmedProviderAccountRecovery(t *testing.T) {
	content, err := os.ReadFile("../../../docs/manual.md")
	if err != nil {
		t.Fatalf("ReadFile manual returned error: %v", err)
	}
	text := string(content)
	for _, want := range []string{
		"Gateway account selection records only an attempt timestamp",
		"final upstream response is 2xx",
		"only the account that produced the final 2xx response recovers",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("manual missing %q in confirmed provider account recovery documentation", want)
		}
	}
}

func TestGatewayDocumentationMentionsExplicitProviderAccountRecoveryPaths(t *testing.T) {
	content, err := os.ReadFile("../../../docs/manual.md")
	if err != nil {
		t.Fatalf("ReadFile manual returned error: %v", err)
	}
	text := string(content)
	for _, want := range []string{
		"Reauthorization replaces the saved OAuth credentials but preserves current account health",
		"Rotating the encrypted API key, base URL, or per-account outbound proxy URL preserves current account health",
		"Reset is an explicit operator override",
		"records both the status-reset audit event and a runtime recovery event",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("manual missing %q in explicit provider account recovery documentation", want)
		}
	}
}

func TestGatewayDocumentationMentionsProviderAccountBulkEnable(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"bulk enable or disable provider accounts",
			"Enable selected",
			"Disable selected",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in provider account bulk enable documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsProviderAccountBulkScheduling(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Apply scheduling",
			"bulk priority",
			"bulk load factor",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in provider account bulk scheduling documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsProviderAccountBulkModels(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Apply models",
			"selected provider accounts",
			"same model capability list",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in provider account bulk model documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsProviderAccountBulkRoutingPoolMembership(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Apply pool",
			"Remove pool",
			"Pool priority",
			"add or remove selected provider accounts",
			"new pool members",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in provider account bulk routing pool documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsSelectedProviderAccountTests(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Test selected",
			"selected provider accounts",
			"without probing the whole account pool",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in selected provider account test documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsProviderAccountBulkRefresh(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Refresh selected",
			"selected provider accounts",
			"force credential refresh",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in provider account bulk refresh documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsProviderAccountBulkDisconnect(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Disconnect selected",
			"selected provider accounts",
			"deletes the selected provider accounts",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in provider account bulk disconnect documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsProviderAccountBulkStatusActions(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Pause selected",
			"Reset selected",
			"Selected provider accounts can be paused and reset together",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in provider account bulk status documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsSchedulingHealthSummary(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Scheduling health",
			"blocked provider accounts",
			"Blocked reasons",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in gateway scheduling health documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsRequestLogModelSessionDrilldown(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Model filter",
			"Session filter",
			"Top models",
			"Top sessions",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in request log model/session drill-down documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsProviderAccountAutoTests(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md", "../../../.env.example"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"N2API_PROVIDER_ACCOUNT_AUTO_TEST_ENABLED",
			"N2API_PROVIDER_ACCOUNT_AUTO_TEST_INTERVAL_SECONDS",
			"disabled by default",
			"startup defaults",
			"Gateway Settings",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in provider account auto test documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsProviderAccountAutoTestStatus(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Auto-test status",
			"last finished",
			"last error",
			"accounts tested",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in provider account auto test status documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsRequestLogRetentionCleanup(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Request log retention",
			"Clean request logs",
			"0 disables",
			"older than the saved retention window",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in request log retention documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsProviderAccountSchedulingPause(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Pause scheduling",
			"Pause duration seconds",
			"remaining scheduling block",
			"temporarily opens the account circuit",
			"Reset local status",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in provider account scheduling pause documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsProviderAccountTestResults(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"last test status",
			"last test time",
			"last test error",
			"test-results",
			"test history",
			"History action",
			"Recent test history",
			"Recent account tests",
			"ops/account-tests",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in provider account test result documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsRoutingPreviewExclusions(t *testing.T) {
	for _, path := range []string{"../../../docs/manual.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Excluded account IDs",
			"account excluded",
			"Routing diagnostics",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in routing preview exclusion documentation", path, want)
			}
		}
	}
}

func TestImageEvidenceDocumentationMatchesWorkflowContract(t *testing.T) {
	checks := map[string][]string{
		"../../../docs/manual.md": {
			"linux/amd64",
			"linux/arm64",
			"same immutable parent manifest",
			"without rebuilding the image",
			"report-only",
			"14 days",
		},
		"../../../docs/release-checklist.md": {
			"exact tested digest",
			"repository workflow",
			"source commit",
			"Trivy JSON",
			"report-only counts",
		},
		"../../../.github/workflows/ci-image.yml": {
			"needs: manifest",
			"SYFT_PLATFORM: ${{ matrix.platform }}",
			"TRIVY_PLATFORM: ${{ matrix.platform }}",
			"push-to-registry: true",
			"retention-days: 14",
		},
		"../../../.github/workflows/release.yml": {
			"gh attestation verify",
			"--predicate-type https://spdx.dev/Document",
			"--signer-workflow",
			"--source-digest",
		},
	}

	for path, wants := range checks {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in image evidence contract", path, want)
			}
		}
	}
}

func TestEncryptionEnvelopeDocumentationMatchesRuntimeContract(t *testing.T) {
	checks := map[string][]string{
		"../../../.env.example": {
			"N2API_ENCRYPTION_KEY_ID=default",
			"N2API_ENCRYPTION_PREVIOUS_KEYS=[]",
		},
		"../../../deploy/compose.release.yaml": {
			"N2API_ENCRYPTION_KEY_ID",
			"N2API_ENCRYPTION_PREVIOUS_KEYS",
		},
		"../../../deploy/compose.restore-test.yaml": {
			"N2API_RESTORE_ENCRYPTION_KEY_ID",
			"N2API_RESTORE_ENCRYPTION_PREVIOUS_KEYS",
		},
		"../../../docs/manual.md": {
			"n2api:v1:<key-id>:<secret-kind>:<payload>",
			"New writes always use the current key",
			"moving an access-token envelope",
			"older image cannot read that new envelope",
			"Previous encryption keys do not keep old cursors valid",
		},
		"../../../docs/plans/2026-07-21-encryption-key-rotation.md": {
			"Task status: completed locally on 2026-07-21",
			"No database rows are rewritten by this task",
		},
	}

	for path, wants := range checks {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in encryption-envelope contract", path, want)
			}
		}
	}
}

func TestEncryptionInventoryDocumentationMatchesRuntimeContract(t *testing.T) {
	checks := map[string][]string{
		"../../../docs/manual.md": {
			"/app/n2api admin verify-encryption",
			"read-only dry run",
			"does not run migrations",
			`"lifecycleStatus": "unreadable_required"`,
			"unreadable_expired_or_purgeable",
			"unauthenticated key IDs",
			"Exit code `0`",
			"Exit code `1`",
			"Exit code `2`",
			"Do not begin re-encryption",
			"cleanup-expired-oauth-states",
			"oauth.state_cleanup.completed",
			"another concurrent worker returns `contended`",
			"/app/n2api admin check-encryption-rotation",
			"always a dry run",
			"24 hours",
			"mandatory gate",
		},
		"../../../docs/plans/2026-07-21-encryption-key-rotation.md": {
			"Tasks 1-2 completed locally on 2026-07-21",
			"lifecycle-aware inventory completed locally on 2026-07-23",
			"operator backup and isolated",
			"all eight non-empty secret columns",
			"all six lifecycle states",
			"No migration or data rewrite is part of this task",
			"Task 2A: Clean Expired OAuth State Secrets Safely",
			"Migration 46 adds a partial `consumed_at` index",
			"Task 2B: Gate Re-encryption Preconditions",
			"always-dry-run preflight",
			"preflight gate implemented locally",
			"found 14 unreadable values",
			"No proxy value was present and no",
			"alert action destination was present",
			"database value was modified",
		},
	}

	for path, wants := range checks {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in encryption-inventory contract", path, want)
			}
		}
	}
}

func TestAlertingDocumentationMatchesRuntimeContract(t *testing.T) {
	checks := map[string][]string{
		"../../../docs/manual.md": {
			"alert-action-destination",
			"`generic_webhook` and `ntfy`",
			"Each rule is limited to 1024 deduplication states",
			"Event evaluation and state admission are serialized per rule",
			"`N2API_ALERT_DELIVERY_ENABLED=false`",
			"rolled-back",
			"transactions are never sent",
			"intentionally not a",
			"durable outbox",
			"`tasks.alertDelivery`",
			"`/alerting`",
			"`/api/admin/alert-actions`",
			"`expectedUpdatedAt` revision",
			"`409 stale_update`",
			"persisted 30-second cooldown",
			"latest sanitized result",
			"always-on monitor that runs once at startup and every five minutes",
			"`api_key.budget.threshold_80.crossed`",
			"`api_key.budget.threshold_100.crossed`",
			"no rule is installed or enabled automatically",
			"`api-key-budget-80-percent-v1` template",
			"`api-key-budget-100-percent-v1` template",
			"always-on monitor that runs at startup and every minute",
			"`api_key.routing_pool.exhausted`",
			"`api_key.routing_pool.recovered`",
			"no routing exhaustion rule is installed or enabled automatically",
			"`routing-pool-exhausted-v1` template",
			"lower sequence",
			"Startup establishes `LISTEN` synchronously",
			"`scheduler.api_key_purge.failed`",
			"`client_api_key_collection`",
			"Normal shutdown cancellation emits no failure",
			"`api-key-purge-failed-v1` template",
			"`scheduler.system_event_retention.failed`",
			"`system_events/retention` target",
			"committed batch is error/failure",
			"No System Event retention failure rule",
			"`system-event-retention-failed-v1` template",
		},
		"../../../docs/plans/2026-07-21-system-event-alerting.md": {
			"Tasks 1-3 and the first sixteen Task 4 slices completed locally on 2026-07-21",
			"oldest idle state at capacity",
			"No default rules, dispatcher, outbound request",
			"dedicated pgx listener",
			"event after commit",
			"stably shards each rule/deduplication stream",
			"Persistent delivery is deferred",
			"`oauth-refresh-repeated-v1`",
			"`request-log-retention-failed-v1`",
			"`provider-auto-test-failed-v1`",
			"`provider-account-expired-v1`",
			"`provider-account-circuit-open-v1`",
			"`api-key-budget-80-percent-v1`",
			"`api-key-budget-100-percent-v1`",
			"`api_key_budget_threshold_states`",
			"Eighth-slice source-event status: completed locally on 2026-07-21",
			"Ninth-slice status: completed locally on 2026-07-21",
			"Tenth-slice status: completed locally on 2026-07-21",
			"`routing_exhaustion_v1` Request Log",
			"`api_key.routing_pool.exhausted`",
			"`routing-pool-exhausted-v1`",
			"Eleventh source-event status: completed locally on 2026-07-21",
			"Twelfth-slice status: completed locally on 2026-07-21",
			"Thirteenth source-event status: completed locally on 2026-07-21",
			"`api-key-purge-failed-v1`",
			"Fourteenth-slice status: completed locally on 2026-07-21",
			"Fifteenth source-event status: completed locally on 2026-07-21",
			"`scheduler.system_event_retention.failed`",
			"`system-event-retention-failed-v1`",
			"Sixteenth-slice status: completed locally on 2026-07-21",
			"`SHARE ... NOWAIT`",
			"`oauth.refresh.diagnostic.failed`",
		},
	}

	for path, wants := range checks {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in alerting storage contract", path, want)
			}
		}
	}
}

func TestPortableConfigurationDocumentationMatchesAlertExportContract(t *testing.T) {
	checks := map[string][]string{
		"../../../docs/manual.md": {
			"`destinationConfigured: true`",
			"complete alert rule",
			"file-local `actionRef` values",
			"there is no `destination` placeholder",
			"`unsupportedSections: []`",
			"`alertActionDestinations`",
		},
		"../../../docs/plans/2026-07-21-backup-restore-verification.md": {
			"Status: completed locally on 2026-07-21",
			"`alert_action:<id>`",
			"`alert_rule:<id>`",
			"`actionRef` that must resolve to an exported action",
			"`encrypted_destination`, test results",
			"reports `unsupportedSections: []`",
		},
	}

	for path, wants := range checks {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in portable alert configuration contract", path, want)
			}
		}
	}
}
