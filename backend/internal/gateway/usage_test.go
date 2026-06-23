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
