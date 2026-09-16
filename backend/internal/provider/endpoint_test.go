package provider

import (
	"context"
	"errors"
	"testing"
)

func TestEndpointAwareSelectionSkipsExplicitlyUnsupportedModelEndpoint(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "responses-unsupported"),
		testAccount(t, 2, true, 1, "responses-supported"),
	}
	repo.accountModels[1] = []AccountModel{{
		AccountID: 1,
		Provider:  "openai",
		Model:     "gpt-5",
		Enabled:   true,
		Metadata:  EndpointCapabilityMetadata(EndpointResponses, EndpointCapabilityUnsupported),
	}}
	repo.accountModels[2] = []AccountModel{{
		AccountID: 2,
		Provider:  "openai",
		Model:     "gpt-5",
		Enabled:   true,
		Metadata:  EndpointCapabilityMetadata(EndpointResponses, EndpointCapabilitySupported),
	}}
	service := newConfiguredService(repo, fakeOAuthClient{})

	selected, err := service.SelectAccountForModelAndEndpoint(context.Background(), "gpt-5", EndpointResponses)
	if err != nil {
		t.Fatalf("SelectAccountForModelAndEndpoint returned error: %v", err)
	}
	if selected.AccountID != 2 {
		t.Fatalf("selected account = %d, want supported account 2", selected.AccountID)
	}

	legacy, err := service.SelectAccountForModel(context.Background(), "gpt-5")
	if err != nil {
		t.Fatalf("legacy SelectAccountForModel returned error: %v", err)
	}
	if legacy.AccountID != 1 {
		t.Fatalf("legacy selected account = %d, want account 1 by existing ordering", legacy.AccountID)
	}

	preview, err := service.PreviewAccountSelectionForEndpoint(context.Background(), "gpt-5", EndpointResponses, "")
	if err != nil {
		t.Fatalf("PreviewAccountSelectionForEndpoint returned error: %v", err)
	}
	if preview.Endpoint != EndpointResponses || len(preview.Candidates) != 2 {
		t.Fatalf("preview = %+v, want responses endpoint and two candidates", preview)
	}
	for _, candidate := range preview.Candidates {
		switch candidate.ID {
		case 1:
			if candidate.Schedulable || candidate.UnschedulableReason != "endpoint unsupported" || candidate.EndpointCapability != EndpointCapabilityUnsupported {
				t.Fatalf("unsupported candidate = %+v", candidate)
			}
		case 2:
			if !candidate.Schedulable || !candidate.Selected || candidate.EndpointCapability != EndpointCapabilitySupported {
				t.Fatalf("supported candidate = %+v", candidate)
			}
		default:
			t.Fatalf("unexpected candidate = %+v", candidate)
		}
	}
}

func TestEndpointAwareSelectionReturnsEndpointUnavailableWhenAllModelsRejectEndpoint(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "unsupported-one"),
		testAccount(t, 2, true, 2, "unsupported-two"),
	}
	for _, account := range repo.accounts {
		repo.accountModels[account.ID] = []AccountModel{{
			AccountID: account.ID,
			Provider:  "openai",
			Model:     "gpt-5",
			Enabled:   true,
			Metadata:  EndpointCapabilityMetadata(EndpointChatCompletions, EndpointCapabilityUnsupported),
		}}
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	_, err := service.SelectAccountForModelAndEndpoint(context.Background(), "gpt-5", EndpointChatCompletions)
	if !errors.Is(err, ErrEndpointUnavailable) {
		t.Fatalf("selection error = %v, want ErrEndpointUnavailable", err)
	}
}

func TestEndpointAwareSelectionUsesSupportedFallbackPool(t *testing.T) {
	repo := newMemoryRepo()
	repo.routingPools[1] = RoutingPool{ID: 1, Name: "primary", Enabled: true, FallbackPoolID: ptrInt64(2)}
	repo.routingPools[2] = RoutingPool{ID: 2, Name: "secondary", Enabled: true}
	repo.accounts = []Account{
		testAccount(t, 10, true, 1, "primary-unsupported"),
		testAccount(t, 20, true, 1, "secondary-supported"),
	}
	repo.routingPoolAccounts[1] = []RoutingPoolAccount{{AccountID: 10, Priority: 0}}
	repo.routingPoolAccounts[2] = []RoutingPoolAccount{{AccountID: 20, Priority: 0}}
	repo.accountModels[10] = []AccountModel{{
		AccountID: 10,
		Provider:  "openai",
		Model:     "gpt-5",
		Enabled:   true,
		Metadata:  EndpointCapabilityMetadata(EndpointResponses, EndpointCapabilityUnsupported),
	}}
	repo.accountModels[20] = []AccountModel{{
		AccountID: 20,
		Provider:  "openai",
		Model:     "gpt-5",
		Enabled:   true,
		Metadata:  EndpointCapabilityMetadata(EndpointResponses, EndpointCapabilitySupported),
	}}
	service := newConfiguredService(repo, fakeOAuthClient{})

	selected, err := service.SelectAccountForModelInRoutingPoolChainAndEndpoint(context.Background(), 1, "gpt-5", EndpointResponses)
	if err != nil {
		t.Fatalf("fallback selection returned error: %v", err)
	}
	if selected.AccountID != 20 || selected.RoutingPoolID != 2 || selected.RoutingPoolFallbackDepth != 1 {
		t.Fatalf("selected = %+v, want supported account 20 in fallback pool", selected)
	}
}

func TestEndpointCapabilityDefaultsToUnknownAndNormalizesAliases(t *testing.T) {
	if got, err := NormalizeEndpoint(" /v1/responses "); err != nil || got != EndpointResponses {
		t.Fatalf("NormalizeEndpoint responses = %q, %v", got, err)
	}
	if got, err := NormalizeEndpoint("chat"); err != nil || got != EndpointChatCompletions {
		t.Fatalf("NormalizeEndpoint chat = %q, %v", got, err)
	}
	if got := EndpointCapability(nil, EndpointResponses); got != EndpointCapabilityUnknown {
		t.Fatalf("missing capability = %q, want unknown", got)
	}
	if got := EndpointCapability(map[string]string{"endpoint.responses": "invalid"}, EndpointResponses); got != EndpointCapabilityUnknown {
		t.Fatalf("invalid capability = %q, want unknown", got)
	}
}
