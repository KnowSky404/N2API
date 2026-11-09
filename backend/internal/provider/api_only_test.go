package provider

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestAPIOnlyProviderRejectsStoredOAuthAccounts(t *testing.T) {
	repo := newMemoryRepo()
	oauth := testAccount(t, 1, true, 100, "legacy-token")
	oauth.AccountType = AccountTypeCodexOAuth
	api := testAccount(t, 2, true, 1, "api-key")
	repo.accounts = []Account{oauth, api}
	service := newConfiguredService(repo, nil)
	accounts, err := service.ListAccounts(context.Background())
	if err != nil || len(accounts) != 1 || accounts[0].ID != api.ID {
		t.Fatalf("API accounts = %+v, %v", accounts, err)
	}
	if AccountSchedulable(oauth, time.Now()) {
		t.Fatal("legacy OAuth account remains schedulable")
	}
	if _, err := service.selectedAccount(context.Background(), oauth); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("OAuth selection error = %v", err)
	}
	selected, err := service.SelectAccountForModel(context.Background(), "")
	if err != nil || selected.AccountID != api.ID || selected.AuthorizationToken != "api-key" {
		t.Fatalf("selected = %+v, %v", selected, err)
	}
	if len(repo.accounts) != 2 {
		t.Fatal("selection deleted historical accounts")
	}
}

func TestAPIOnlyManagementPreservesStoredOAuthAccounts(t *testing.T) {
	ctx := context.Background()
	enabled, priority, name, proxy := false, 2, "renamed", "http://proxy.example:8080"
	tests := []struct {
		name string
		run  func(*Service) error
	}{
		{"update enabled", func(s *Service) error {
			_, err := s.UpdateAccount(ctx, 1, AccountUpdate{Enabled: &enabled})
			return err
		}},
		{"update name", func(s *Service) error { _, err := s.UpdateAccount(ctx, 1, AccountUpdate{Name: &name}); return err }},
		{"update priority", func(s *Service) error {
			_, err := s.UpdateAccount(ctx, 1, AccountUpdate{Priority: &priority})
			return err
		}},
		{"update proxy", func(s *Service) error { _, err := s.UpdateAccount(ctx, 1, AccountUpdate{ProxyURL: &proxy}); return err }},
		{"reset", func(s *Service) error { _, err := s.ResetAccountStatus(ctx, 1); return err }},
		{"pause", func(s *Service) error { _, err := s.PauseAccountScheduling(ctx, 1, time.Minute); return err }},
		{"list models", func(s *Service) error { _, err := s.ListAccountModels(ctx, 1); return err }},
		{"list models batch", func(s *Service) error { _, err := s.ListAccountModelsForAccounts(ctx, []int64{1}); return err }},
		{"replace models", func(s *Service) error {
			_, err := s.ReplaceAccountModels(ctx, 1, []AccountModelInput{{Model: "replacement", Enabled: true}})
			return err
		}},
		{"test history", func(s *Service) error { _, err := s.ListAccountTestResults(ctx, 1, 20); return err }},
		{"disconnect", func(s *Service) error { return s.DisconnectAccount(ctx, 1) }},
		{"test account", func(s *Service) error { _, err := s.TestAccount(ctx, 1); return err }},
		{"test model", func(s *Service) error { _, err := s.TestAccountModel(ctx, 1, "legacy"); return err }},
		{"sync models", func(s *Service) error { _, _, err := s.SyncUpstreamAccountModels(ctx, 1); return err }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := newMemoryRepo()
			oauth := testAccount(t, 1, true, 100, "legacy-token")
			oauth.AccountType = AccountTypeCodexOAuth
			repo.accounts = []Account{oauth}
			models := []AccountModel{{AccountID: 1, Provider: "openai", Model: "legacy", Enabled: true}}
			repo.accountModels[1] = append([]AccountModel(nil), models...)
			if err := test.run(newConfiguredService(repo, nil)); !errors.Is(err, ErrNotConnected) {
				t.Fatalf("error = %v, want ErrNotConnected", err)
			}
			if !reflect.DeepEqual(repo.accounts, []Account{oauth}) || !reflect.DeepEqual(repo.accountModels[1], models) || len(repo.intents) != 0 || len(repo.accountTestResults) != 0 {
				t.Fatal("rejected operation changed archived account, credentials, models, or history")
			}
		})
	}
}

func TestAPIOnlyPreviewOmitsStoredOAuthAccounts(t *testing.T) {
	repo := newMemoryRepo()
	oauth := testAccount(t, 1, true, 100, "legacy")
	oauth.AccountType = AccountTypeCodexOAuth
	api := testAccount(t, 2, true, 1, "key")
	repo.accounts = []Account{oauth, api}
	repo.routingPools[1] = RoutingPool{ID: 1, Name: "primary", Enabled: true}
	repo.routingPoolAccounts[1] = []RoutingPoolAccount{{AccountID: 1}, {AccountID: 2}}
	service := newConfiguredService(repo, nil)
	for _, poolID := range []int64{0, 1} {
		preview, err := service.PreviewAccountSelectionInRoutingPool(context.Background(), poolID, "", "session")
		if err != nil || len(preview.Candidates) != 1 || preview.Candidates[0].ID != api.ID {
			t.Fatalf("pool %d preview = %+v, %v", poolID, preview, err)
		}
	}
}
