package gateway

import (
	"encoding/json"
	"strings"
)

type Usage struct {
	Model             string
	InputTokens       int
	OutputTokens      int
	TotalTokens       int
	CachedInputTokens int
	ReasoningTokens   int
	Source            string
}

func ParseUsageFromJSON(route string, raw []byte) Usage {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return Usage{Source: "missing"}
	}
	usagePayload, ok := payload["usage"].(map[string]any)
	if !ok {
		return Usage{Source: "missing"}
	}
	model, _ := payload["model"].(string)
	switch route {
	case "/v1/chat/completions":
		usage := parseChatUsage(usagePayload)
		usage.Model = strings.TrimSpace(model)
		usage.Source = "chat_completions"
		return usage
	case "/v1/responses":
		usage := parseResponsesUsage(usagePayload)
		usage.Model = strings.TrimSpace(model)
		usage.Source = "responses"
		return usage
	default:
		usage := parseResponsesUsage(usagePayload)
		if usage.InputTokens == 0 && usage.OutputTokens == 0 && usage.TotalTokens == 0 {
			usage = parseChatUsage(usagePayload)
		}
		usage.Model = strings.TrimSpace(model)
		if usage.InputTokens == 0 && usage.OutputTokens == 0 && usage.TotalTokens == 0 {
			usage.Source = "missing"
		} else {
			usage.Source = "json"
		}
		return usage
	}
}

func parseChatUsage(payload map[string]any) Usage {
	usage := Usage{
		InputTokens:  intFromAny(payload["prompt_tokens"]),
		OutputTokens: intFromAny(payload["completion_tokens"]),
		TotalTokens:  intFromAny(payload["total_tokens"]),
	}
	if details, ok := payload["prompt_tokens_details"].(map[string]any); ok {
		usage.CachedInputTokens = intFromAny(details["cached_tokens"])
	}
	if details, ok := payload["completion_tokens_details"].(map[string]any); ok {
		usage.ReasoningTokens = intFromAny(details["reasoning_tokens"])
	}
	return usage
}

func parseResponsesUsage(payload map[string]any) Usage {
	usage := Usage{
		InputTokens:  intFromAny(payload["input_tokens"]),
		OutputTokens: intFromAny(payload["output_tokens"]),
		TotalTokens:  intFromAny(payload["total_tokens"]),
	}
	if details, ok := payload["input_tokens_details"].(map[string]any); ok {
		usage.CachedInputTokens = intFromAny(details["cached_tokens"])
	}
	if details, ok := payload["output_tokens_details"].(map[string]any); ok {
		usage.ReasoningTokens = intFromAny(details["reasoning_tokens"])
	}
	return usage
}

func intFromAny(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		i, _ := v.Int64()
		return int(i)
	default:
		return 0
	}
}
