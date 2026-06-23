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

func TestGatewayDocumentationMentionsProviderAccountSchedulingPause(t *testing.T) {
	for _, path := range []string{"../../../README.md", "../../../deploy/README.md"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) returned error: %v", path, err)
		}
		text := string(content)
		for _, want := range []string{
			"Pause scheduling",
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
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q in provider account test result documentation", path, want)
			}
		}
	}
}
