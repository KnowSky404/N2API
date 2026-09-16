package gateway

import (
	"net/http"
	"testing"

	"github.com/KnowSky404/N2API/backend/internal/provider"
)

func TestEndpointForRequestClassifiesSupportedRoutes(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		want   string
	}{
		{name: "chat completions", method: http.MethodPost, path: "/v1/chat/completions", want: provider.EndpointChatCompletions},
		{name: "responses create", method: http.MethodPost, path: "/v1/responses", want: provider.EndpointResponses},
		{name: "responses retrieve", method: http.MethodGet, path: "/v1/responses/resp_123", want: provider.EndpointResponses},
		{name: "responses input items", method: http.MethodGet, path: "/v1/responses/resp_123/input_items", want: provider.EndpointResponses},
		{name: "models", method: http.MethodGet, path: "/v1/models", want: ""},
		{name: "unsupported post", method: http.MethodPost, path: "/v1/embeddings", want: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req, err := http.NewRequest(test.method, "http://example.test"+test.path, nil)
			if err != nil {
				t.Fatalf("http.NewRequest returned error: %v", err)
			}
			if got := endpointForRequest(req); got != test.want {
				t.Fatalf("endpointForRequest(%s %s) = %q, want %q", test.method, test.path, got, test.want)
			}
		})
	}
}
