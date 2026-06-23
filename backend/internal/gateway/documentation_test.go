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
