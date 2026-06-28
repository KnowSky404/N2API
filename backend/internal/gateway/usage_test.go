package gateway

import "testing"

func TestParseUsageFromChatCompletionsJSON(t *testing.T) {
	usage := ParseUsageFromJSON("/v1/chat/completions", []byte(`{
		"model":"gpt-5",
		"usage":{
			"prompt_tokens":100,
			"completion_tokens":25,
			"total_tokens":125,
			"prompt_tokens_details":{"cached_tokens":40},
			"completion_tokens_details":{"reasoning_tokens":10}
		}
	}`))

	if usage.Source != "chat_completions" || usage.Model != "gpt-5" {
		t.Fatalf("usage identity = %+v, want chat_completions gpt-5", usage)
	}
	if usage.InputTokens != 100 || usage.OutputTokens != 25 || usage.TotalTokens != 125 || usage.CachedInputTokens != 40 || usage.ReasoningTokens != 10 {
		t.Fatalf("usage tokens = %+v, want parsed chat completion tokens", usage)
	}
}

func TestParseUsageFromResponsesJSON(t *testing.T) {
	usage := ParseUsageFromJSON("/v1/responses", []byte(`{
		"model":"gpt-5-mini",
		"usage":{
			"input_tokens":50,
			"output_tokens":20,
			"total_tokens":70,
			"input_tokens_details":{"cached_tokens":15},
			"output_tokens_details":{"reasoning_tokens":8}
		}
	}`))

	if usage.Source != "responses" || usage.Model != "gpt-5-mini" {
		t.Fatalf("usage identity = %+v, want responses gpt-5-mini", usage)
	}
	if usage.InputTokens != 50 || usage.OutputTokens != 20 || usage.TotalTokens != 70 || usage.CachedInputTokens != 15 || usage.ReasoningTokens != 8 {
		t.Fatalf("usage tokens = %+v, want parsed responses tokens", usage)
	}
}

func TestParseUsageFromGeminiUsageMetadataJSON(t *testing.T) {
	usage := ParseUsageFromJSON("/v1/chat/completions", []byte(`{
		"model":"gemini-2.5-pro",
		"usageMetadata":{
			"promptTokenCount":120,
			"candidatesTokenCount":30,
			"totalTokenCount":150,
			"cachedContentTokenCount":40,
			"thoughtsTokenCount":12
		}
	}`))

	if usage.Source != "gemini_usage_metadata" || usage.Model != "gemini-2.5-pro" {
		t.Fatalf("usage identity = %+v, want gemini_usage_metadata gemini-2.5-pro", usage)
	}
	if usage.InputTokens != 120 || usage.OutputTokens != 30 || usage.TotalTokens != 150 || usage.CachedInputTokens != 40 || usage.ReasoningTokens != 12 {
		t.Fatalf("usage tokens = %+v, want parsed Gemini usage metadata", usage)
	}
}

func TestParseUsageFromAnthropicCompatibleJSON(t *testing.T) {
	usage := ParseUsageFromJSON("/v1/messages", []byte(`{
		"model":"claude-sonnet-4",
		"usage":{
			"input_tokens":80,
			"output_tokens":25,
			"cache_read_input_tokens":30,
			"cache_creation_input_tokens":5
		}
	}`))

	if usage.Source != "anthropic_usage" || usage.Model != "claude-sonnet-4" {
		t.Fatalf("usage identity = %+v, want anthropic_usage claude-sonnet-4", usage)
	}
	if usage.InputTokens != 80 || usage.OutputTokens != 25 || usage.TotalTokens != 105 || usage.CachedInputTokens != 35 {
		t.Fatalf("usage tokens = %+v, want parsed Anthropic-compatible usage", usage)
	}
}

func TestSSEUsageObserverParsesResponseCompletedEvent(t *testing.T) {
	observer := NewSSEUsageObserver("/v1/responses")

	observer.Observe([]byte(": keep-alive\n\n"))
	observer.Observe([]byte("data: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-5\",\"usage\":{\"input_tokens\":5,\"output_tokens\":7,\"total_tokens\":12,\"input_tokens_details\":{\"cached_tokens\":2},\"output_tokens_details\":{\"reasoning_tokens\":3}}}}\n\n"))

	usage := observer.Usage()
	if usage.Source != "stream" || usage.Model != "gpt-5" {
		t.Fatalf("usage identity = %+v, want stream gpt-5", usage)
	}
	if usage.InputTokens != 5 || usage.OutputTokens != 7 || usage.TotalTokens != 12 || usage.CachedInputTokens != 2 || usage.ReasoningTokens != 3 {
		t.Fatalf("usage tokens = %+v, want parsed stream tokens", usage)
	}
}
