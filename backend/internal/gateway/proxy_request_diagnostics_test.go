package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KnowSky404/N2API/backend/internal/provider"
)

func TestProxyLogsHTTP200ModelErrorAndMissingGenerationSeparately(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		route       string
		body        string
		wantError   string
		requestBody string
	}{
		{
			name:        "model error",
			route:       "/v1/responses",
			body:        `{"error":{"code":"model_error","message":"private detail"}}`,
			wantError:   "upstream_model_error",
			requestBody: `{"model":"gpt-5","input":"hi"}`,
		},
		{
			name:        "missing generation",
			route:       "/v1/chat/completions",
			body:        `{"object":"chat.completion"}`,
			wantError:   "upstream_generation_missing",
			requestBody: `{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			logger := &fakeRequestLogger{}
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(testCase.body)),
					Request:    r,
				}, nil
			})}
			proxy := NewProxyWithClient(
				&fakeAPIKeyAuthenticator{},
				&fakeSelectedAccountProvider{accounts: []SelectedAccount{{AccountID: 1, AccountType: provider.AccountTypeAPIUpstream, AuthorizationToken: "upstream-token"}}},
				Config{Logger: logger},
				client,
			)
			req := httptest.NewRequest(http.MethodPost, testCase.route, strings.NewReader(testCase.requestBody))
			req.Header.Set("Authorization", "Bearer n2api_client_secret")
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			proxy.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want HTTP 200 passthrough; body=%s", recorder.Code, recorder.Body.String())
			}
			if len(logger.entries) != 1 || logger.entries[0].Error != testCase.wantError {
				t.Fatalf("request log = %+v, want %s", logger.entries, testCase.wantError)
			}
			if len(logger.entries[0].Attempts) != 2 || logger.entries[0].Attempts[1].Error != testCase.wantError {
				t.Fatalf("attempt diagnostics = %+v, want HTTP 200 diagnostic %s", logger.entries[0].Attempts, testCase.wantError)
			}
			if logger.entries[0].ResponseTiming.HeaderWaitMS == nil || logger.entries[0].ResponseTiming.FirstUsefulOutputMS != nil || logger.entries[0].ResponseTiming.StreamFinishMS == nil {
				t.Fatalf("response timing = %+v, want header/finish and no useful output", logger.entries[0].ResponseTiming)
			}
		})
	}
}
