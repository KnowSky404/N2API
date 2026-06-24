package gateway

import (
	"os"
	"strings"
	"testing"
)

func TestGatewayModelDocumentationMentionsAPIKeyPolicyFiltering(t *testing.T) {
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
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

func TestGatewayDocumentationMentionsStickySessionProxyHeaderRequirement(t *testing.T) {
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
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
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
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
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
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

func TestGatewayDocumentationMentionsAPIKeyActiveConcurrency(t *testing.T) {
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
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
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Requests window",
			"Tokens window",
			"process-local fixed one-minute",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in API key rate window visibility documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsReadinessRefresh(t *testing.T) {
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
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
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"API upstream credentials can be updated",
			"base URL",
			"clears local failure status",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in API upstream credential rotation documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsSingleAccountModelBackfill(t *testing.T) {
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"single connected provider account",
			"backfills",
			"global allowed model list",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in single-account model backfill documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsProviderAccountLoadFactor(t *testing.T) {
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"load factor",
			"same priority",
			"higher load factor",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in provider account load factor documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsProviderAccountConcurrencyOverride(t *testing.T) {
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
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
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
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

func TestGatewayDocumentationMentionsRoutingPreviewConcurrency(t *testing.T) {
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Routing preview",
			"active concurrency",
			"Concurrency full",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in routing preview concurrency documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsProviderAccountTestProbe(t *testing.T) {
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Test account",
			"probes one provider account",
			"records upstream failure status",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in provider account test probe documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsProviderAccountBulkEnable(t *testing.T) {
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
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
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
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
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
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

func TestGatewayDocumentationMentionsSelectedProviderAccountTests(t *testing.T) {
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
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
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
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

func TestGatewayDocumentationMentionsProviderAccountBulkStatusActions(t *testing.T) {
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
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

func TestGatewayDocumentationMentionsProviderAccountAutoTests(t *testing.T) {
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md", "../../../.env.example"} {
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
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
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

func TestGatewayDocumentationMentionsProviderAccountSchedulingPause(t *testing.T) {
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
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
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
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
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in provider account test result documentation", path, want)
			}
		}
	}
}

func TestGatewayDocumentationMentionsRoutingPreviewExclusions(t *testing.T) {
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
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
