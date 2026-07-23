package gateway

import (
	"bytes"
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
	model, _ := payload["model"].(string)
	usagePayload, ok := payload["usage"].(map[string]any)
	if !ok {
		if usageMetadata, ok := payload["usageMetadata"].(map[string]any); ok {
			usage := parseGeminiUsageMetadata(usageMetadata)
			usage.Model = strings.TrimSpace(model)
			if usage.InputTokens == 0 && usage.OutputTokens == 0 && usage.TotalTokens == 0 {
				usage.Source = "missing"
			} else {
				usage.Source = "gemini_usage_metadata"
			}
			return usage
		}
		return Usage{Source: "missing"}
	}
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
		if usage.TotalTokens == 0 && usage.InputTokens > 0 && usage.OutputTokens > 0 {
			usage.Source = "anthropic_usage"
			usage.TotalTokens = usage.InputTokens + usage.OutputTokens
			usage.CachedInputTokens = intFromAny(usagePayload["cache_read_input_tokens"]) + intFromAny(usagePayload["cache_creation_input_tokens"])
		}
		usage.Model = strings.TrimSpace(model)
		if usage.InputTokens == 0 && usage.OutputTokens == 0 && usage.TotalTokens == 0 {
			usage.Source = "missing"
		} else if usage.Source == "" {
			usage.Source = "json"
		}
		return usage
	}
}

type SSEUsageObserver struct {
	route      string
	buf        []byte
	usage      Usage
	responseID string
}

func NewSSEUsageObserver(route string) *SSEUsageObserver {
	return &SSEUsageObserver{route: route, usage: Usage{Source: "missing"}}
}

func (o *SSEUsageObserver) Observe(chunk []byte) {
	if o == nil || len(chunk) == 0 {
		return
	}
	o.buf = append(o.buf, chunk...)
	for {
		index, delimiterLength := nextSSEEventDelimiter(o.buf)
		if index < 0 {
			return
		}
		event := append([]byte(nil), o.buf[:index]...)
		o.buf = o.buf[index+delimiterLength:]
		o.observeEvent(event)
	}
}

func nextSSEEventDelimiter(buf []byte) (int, int) {
	lfIndex := bytes.Index(buf, []byte("\n\n"))
	crlfIndex := bytes.Index(buf, []byte("\r\n\r\n"))
	if crlfIndex >= 0 && (lfIndex < 0 || crlfIndex < lfIndex) {
		return crlfIndex, 4
	}
	if lfIndex >= 0 {
		return lfIndex, 2
	}
	return -1, 0
}

func (o *SSEUsageObserver) Usage() Usage {
	if o == nil {
		return Usage{Source: "missing"}
	}
	return o.usage
}

func (o *SSEUsageObserver) ResponseID() string {
	if o == nil {
		return ""
	}
	return o.responseID
}

func (o *SSEUsageObserver) observeEvent(event []byte) {
	eventName := ""
	dataLines := make([][]byte, 0, 1)
	for _, line := range bytes.Split(event, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if bytes.HasPrefix(line, []byte("event:")) {
			eventName = string(bytes.TrimSpace(bytes.TrimPrefix(line, []byte("event:"))))
			continue
		}
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
			continue
		}
		dataLines = append(dataLines, append([]byte(nil), data...))
	}
	for _, data := range dataLines {
		usage := ParseUsageFromSSEData(o.route, data)
		if usage.Source != "missing" {
			o.usage = usage
		}
		if responseID := responseIDFromSSEData(eventName, data); responseID != "" {
			o.responseID = responseID
		}
	}
}

func responseIDFromJSON(route string, raw []byte) string {
	if route != "/v1/responses" && !strings.HasPrefix(route, "/v1/responses/") {
		return ""
	}
	var payload struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.ID)
}

func responseIDFromSSEData(eventName string, raw []byte) string {
	var payload struct {
		Type     string `json:"type"`
		Response struct {
			ID string `json:"id"`
		} `json:"response"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	eventType := strings.TrimSpace(payload.Type)
	if eventType == "" {
		eventType = strings.TrimSpace(eventName)
	}
	if eventType != "response.created" && eventType != "response.completed" {
		return ""
	}
	return strings.TrimSpace(payload.Response.ID)
}

func ParseUsageFromSSEData(route string, raw []byte) Usage {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return Usage{Source: "missing"}
	}
	if response, ok := payload["response"].(map[string]any); ok {
		raw, err := json.Marshal(response)
		if err != nil {
			return Usage{Source: "missing"}
		}
		usage := ParseUsageFromJSON(route, raw)
		if usage.Source != "missing" {
			usage.Source = "stream"
		}
		return usage
	}
	usage := ParseUsageFromJSON(route, raw)
	if usage.Source != "missing" {
		usage.Source = "stream"
	}
	return usage
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

func parseGeminiUsageMetadata(payload map[string]any) Usage {
	return Usage{
		InputTokens:       intFromAny(payload["promptTokenCount"]),
		OutputTokens:      intFromAny(payload["candidatesTokenCount"]),
		TotalTokens:       intFromAny(payload["totalTokenCount"]),
		CachedInputTokens: intFromAny(payload["cachedContentTokenCount"]),
		ReasoningTokens:   intFromAny(payload["thoughtsTokenCount"]),
	}
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
