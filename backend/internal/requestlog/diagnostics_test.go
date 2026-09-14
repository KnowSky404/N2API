package requestlog

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBoundedTextRemovesControlsAndPreservesUTF8(t *testing.T) {
	got := BoundedText("  alpha\n\t世界\x00", 8)
	if len(got) > 8 {
		t.Fatalf("bounded text length = %d, want at most 8", len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatalf("bounded text is not valid UTF-8: %q", got)
	}
	if strings.ContainsAny(got, "\x00\n\t") {
		t.Fatalf("bounded text contains control characters: %q", got)
	}
}

func TestResponseTimingUsesNullForUnobservedPhases(t *testing.T) {
	encoded, err := json.Marshal(ResponseTiming{})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	if got, want := string(encoded), `{"headerWaitMs":null,"firstUsefulOutputMs":null,"streamFinishMs":null}`; got != want {
		t.Fatalf("encoded timing = %s, want %s", got, want)
	}
}

func TestRequestAttemptJSONContainsOnlyDiagnosticFields(t *testing.T) {
	attempt := RequestAttempt{
		Order: 1,
		Type:  "upstream_http",
		Error: "upstream_rate_limited",
	}
	encoded, err := json.Marshal(attempt)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	if strings.Contains(string(encoded), "prompt") || strings.Contains(string(encoded), "token") {
		t.Fatalf("diagnostic attempt contains payload-like field: %s", encoded)
	}
}
