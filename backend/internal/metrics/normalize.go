package metrics

import "strings"

func normalize(value string, allowed []string) string {
	value = strings.TrimSpace(value)
	for _, candidate := range allowed {
		if value == candidate {
			return value
		}
	}
	return "other"
}

func normalizeRoute(route string) string {
	route = strings.TrimSpace(route)
	switch route {
	case "/v1/models":
		return "models"
	case "/v1/chat/completions":
		return "chat_completions"
	case "/v1/responses":
		return "responses_create"
	}
	const responsesPrefix = "/v1/responses/"
	if !strings.HasPrefix(route, responsesPrefix) {
		return "other"
	}
	parts := strings.Split(strings.TrimPrefix(route, responsesPrefix), "/")
	if len(parts) == 1 && parts[0] != "" {
		return "responses_retrieve"
	}
	if len(parts) == 2 && parts[0] != "" && parts[1] == "input_items" {
		return "responses_input_items"
	}
	return "other"
}

func normalizeStatusClass(status int) string {
	switch {
	case status >= 200 && status <= 299:
		return "2xx"
	case status >= 400 && status <= 499:
		return "4xx"
	case status >= 500 && status <= 599:
		return "5xx"
	default:
		return "other"
	}
}

func normalizeAccountType(accountType string) string {
	switch strings.TrimSpace(accountType) {
	case "codex_oauth":
		return "codex_oauth"
	case "api_key", "api_upstream":
		return "api_key"
	case "", "none":
		return "none"
	default:
		return "other"
	}
}
