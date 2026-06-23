package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/secret"
)

func TestStatusReportsConfigurationAndConnection(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, fakeOAuthClient{}, Config{
		Provider:     "openai",
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RedirectURL:  "http://localhost/oauth/openai/callback",
		AuthURL:      "https://auth.example.test/authorize",
		TokenURL:     "https://auth.example.test/token",
		Secret:       "encryption-secret",
	})

	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if !status.Configured || status.Connected {
		t.Fatalf("status = %+v, want configured disconnected provider", status)
	}

	expiresAt := time.Now().Add(time.Hour).UTC()
	if _, err := repo.SaveAccount(context.Background(), Account{
		Provider:              "openai",
		Subject:               "acct_1",
		DisplayName:           "Codex Account",
		EncryptedAccessToken:  mustEncrypt(t, "encryption-secret", "access-token"),
		EncryptedRefreshToken: mustEncrypt(t, "encryption-secret", "refresh-token"),
		AccessTokenExpiresAt:  &expiresAt,
		LastRefreshAt:         nil,
	}); err != nil {
		t.Fatalf("SaveAccount returned error: %v", err)
	}

	status, err = service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if !status.Connected || status.DisplayName != "Codex Account" {
		t.Fatalf("status = %+v, want connected Codex Account", status)
	}
}

func TestStartConnectStoresHashedStateAndBuildsAuthorizationURL(t *testing.T) {
	repo := newMemoryRepo()
	service := newConfiguredService(repo, fakeOAuthClient{})

	result, err := service.StartConnect(context.Background(), ConnectOptions{RedirectAfter: "/"})
	if err != nil {
		t.Fatalf("StartConnect returned error: %v", err)
	}
	parsed, err := url.Parse(result.AuthorizationURL)
	if err != nil {
		t.Fatalf("authorization URL did not parse: %v", err)
	}
	if parsed.Query().Get("client_id") != "client-id" {
		t.Fatalf("client_id = %q", parsed.Query().Get("client_id"))
	}
	if parsed.Query().Get("redirect_uri") != "http://localhost/oauth/openai/callback" {
		t.Fatalf("redirect_uri = %q", parsed.Query().Get("redirect_uri"))
	}
	state := parsed.Query().Get("state")
	if state == "" {
		t.Fatal("state was empty")
	}
	if strings.Contains(repo.states[0].StateHash, state) {
		t.Fatal("state repository stored cleartext state")
	}
}

func TestStartConnectUsesBuiltInCodexPKCEConfig(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, fakeOAuthClient{}, Config{
		Provider: "openai",
		Secret:   "encryption-secret",
	})

	result, err := service.StartConnect(context.Background(), ConnectOptions{RedirectAfter: "/"})
	if err != nil {
		t.Fatalf("StartConnect returned error: %v", err)
	}
	parsed, err := url.Parse(result.AuthorizationURL)
	if err != nil {
		t.Fatalf("authorization URL did not parse: %v", err)
	}
	query := parsed.Query()
	if query.Get("client_id") == "" {
		t.Fatal("client_id was empty")
	}
	if query.Get("redirect_uri") != "http://localhost:1455/auth/callback" {
		t.Fatalf("redirect_uri = %q, want Codex callback", query.Get("redirect_uri"))
	}
	if query.Get("code_challenge") == "" {
		t.Fatal("code_challenge was empty")
	}
	if query.Get("code_challenge_method") != "S256" {
		t.Fatalf("code_challenge_method = %q, want S256", query.Get("code_challenge_method"))
	}
	if query.Get("response_type") != "code" {
		t.Fatalf("response_type = %q, want code", query.Get("response_type"))
	}
	if repo.states[0].CodeVerifierHash == "" {
		t.Fatal("state did not record code verifier hash")
	}
	if repo.states[0].CodeVerifier == "" {
		t.Fatal("memory repo did not retain verifier for token exchange")
	}
	if repo.states[0].EncryptedCodeVerifier == "" {
		t.Fatal("state did not record encrypted code verifier")
	}
	if strings.Contains(repo.states[0].CodeVerifierHash, repo.states[0].CodeVerifier) {
		t.Fatal("state stored cleartext code verifier in hash field")
	}
}

func TestStartConnectStoresPendingAccountOptionsAndFingerprintHashes(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, fakeOAuthClient{}, Config{
		Provider:    "openai",
		RedirectURL: "http://localhost:3000/oauth/openai/callback",
		Secret:      "encryption-secret",
	})

	result, err := service.StartConnect(context.Background(), ConnectOptions{
		RedirectAfter: "/",
		Name:          "Work Codex",
		Priority:      25,
		Enabled:       boolPtr(false),
		Fingerprint: Fingerprint{
			Value:     "browser-fingerprint",
			UserAgent: "Mozilla/5.0",
			IP:        "203.0.113.10",
		},
	})
	if err != nil {
		t.Fatalf("StartConnect returned error: %v", err)
	}
	if mustQuery(t, result.AuthorizationURL, "state") == "" {
		t.Fatal("authorization URL missing state")
	}
	state := repo.states[0]
	if state.PendingAccountName != "Work Codex" || state.PendingPriority != 25 || state.PendingEnabled == nil || *state.PendingEnabled {
		t.Fatalf("state pending account fields = %+v", state)
	}
	if state.FingerprintHash == "" || state.UserAgentHash == "" || state.IPHash == "" {
		t.Fatalf("state fingerprint hashes incomplete: %+v", state)
	}
	for _, cleartext := range []string{"browser-fingerprint", "Mozilla/5.0", "203.0.113.10"} {
		if strings.Contains(state.FingerprintHash+state.UserAgentHash+state.IPHash, cleartext) {
			t.Fatalf("fingerprint hash leaked cleartext %q", cleartext)
		}
	}
}

func TestCompleteCallbackRejectsInvalidState(t *testing.T) {
	service := newConfiguredService(newMemoryRepo(), fakeOAuthClient{})
	if _, err := service.CompleteCallback(context.Background(), "code", "missing-state"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("CompleteCallback error = %v, want ErrInvalidState", err)
	}
}

func TestCompleteCallbackStoresEncryptedTokensAndConsumesState(t *testing.T) {
	repo := newMemoryRepo()
	client := fakeOAuthClient{
		exchange: TokenResponse{
			AccessToken:  "access-token",
			RefreshToken: "refresh-token",
			ExpiresIn:    3600,
			Subject:      "acct_1",
			DisplayName:  "Codex Account",
		},
	}
	service := newConfiguredService(repo, client)

	started, err := service.StartConnect(context.Background(), ConnectOptions{RedirectAfter: "/"})
	if err != nil {
		t.Fatalf("StartConnect returned error: %v", err)
	}
	state := mustQuery(t, started.AuthorizationURL, "state")
	account, err := service.CompleteCallback(context.Background(), "auth-code", state)
	if err != nil {
		t.Fatalf("CompleteCallback returned error: %v", err)
	}
	if account.DisplayName != "Codex Account" {
		t.Fatalf("DisplayName = %q", account.DisplayName)
	}
	if repo.accounts[0].EncryptedAccessToken == "access-token" || repo.accounts[0].EncryptedRefreshToken == "refresh-token" {
		t.Fatal("repository stored cleartext tokens")
	}
	if repo.states[0].ConsumedAt == nil {
		t.Fatal("state was not consumed")
	}
}

func TestCompleteCallbackPassesPKCEVerifierAndStoresTokenMetadata(t *testing.T) {
	repo := newMemoryRepo()
	client := &captureExchangeOAuthClient{
		exchange: TokenResponse{
			AccessToken:  "access-token",
			RefreshToken: "refresh-token",
			IDToken:      "id-token",
			ExpiresIn:    3600,
			AccountID:    "acct_chatgpt",
			Email:        "owner@example.com",
			PlanType:     "plus",
			ClientID:     "codex-client",
		},
	}
	service := NewService(repo, client, Config{
		Provider:    "openai",
		RedirectURL: "http://localhost:3000/oauth/openai/callback",
		Secret:      "encryption-secret",
	})

	started, err := service.StartConnect(context.Background(), ConnectOptions{RedirectAfter: "/"})
	if err != nil {
		t.Fatalf("StartConnect returned error: %v", err)
	}
	state := mustQuery(t, started.AuthorizationURL, "state")
	account, err := service.CompleteCallback(context.Background(), "auth-code", state)
	if err != nil {
		t.Fatalf("CompleteCallback returned error: %v", err)
	}
	if client.gotCodeVerifier == "" {
		t.Fatal("ExchangeCode did not receive PKCE code verifier")
	}
	if account.Subject != "acct_chatgpt" || account.DisplayName != "owner@example.com" {
		t.Fatalf("account identity = %q/%q, want account id and email display", account.Subject, account.DisplayName)
	}
	if account.Metadata["email"] != "owner@example.com" || account.Metadata["account_id"] != "acct_chatgpt" || account.Metadata["plan_type"] != "plus" || account.Metadata["client_id"] != "codex-client" {
		t.Fatalf("metadata = %+v", account.Metadata)
	}
	idToken, err := secret.DecryptString("encryption-secret", account.EncryptedIDToken)
	if err != nil {
		t.Fatalf("decrypt id token returned error: %v", err)
	}
	if idToken != "id-token" {
		t.Fatalf("id token = %q, want id-token", idToken)
	}
}

func TestCompleteCallbackCreatesNamedAccountWithIsolatedFingerprint(t *testing.T) {
	repo := newMemoryRepo()
	client := &captureExchangeOAuthClient{
		exchange: TokenResponse{
			AccessToken:  "access-token",
			RefreshToken: "refresh-token",
			ExpiresIn:    3600,
			AccountID:    "acct_work",
			Email:        "work@example.com",
			ClientID:     "codex-client",
		},
	}
	service := NewService(repo, client, Config{
		Provider:    "openai",
		RedirectURL: "http://localhost:3000/oauth/openai/callback",
		Secret:      "encryption-secret",
	})

	started, err := service.StartConnect(context.Background(), ConnectOptions{
		RedirectAfter: "/",
		Name:          "Work Account",
		Priority:      7,
		Fingerprint: Fingerprint{
			Value:     "browser-fingerprint",
			UserAgent: "Mozilla/5.0",
			IP:        "203.0.113.10",
		},
	})
	if err != nil {
		t.Fatalf("StartConnect returned error: %v", err)
	}
	account, err := service.CompleteCallback(context.Background(), "auth-code", mustQuery(t, started.AuthorizationURL, "state"))
	if err != nil {
		t.Fatalf("CompleteCallback returned error: %v", err)
	}
	if account.Name != "Work Account" || account.Priority != 7 || account.Status != AccountStatusActive {
		t.Fatalf("account = %+v, want named active priority 7 account", account)
	}
	if account.FingerprintHash == "" || account.UserAgentHash == "" || account.IPHash == "" {
		t.Fatalf("account fingerprint hashes incomplete: %+v", account)
	}
	if account.Metadata["email"] != "work@example.com" || account.Metadata["chatgpt_account_id"] != "acct_work" || account.Metadata["access_token_sha256"] == "" {
		t.Fatalf("metadata = %+v", account.Metadata)
	}
}

func TestCompleteCallbackReauthorizesTargetAccountInsteadOfMatchingDifferentIdentity(t *testing.T) {
	repo := newMemoryRepo()
	existing, err := repo.SaveAccount(context.Background(), Account{
		Provider:              "openai",
		Subject:               "acct_old",
		Name:                  "Old Account",
		DisplayName:           "old@example.com",
		EncryptedAccessToken:  mustEncrypt(t, "encryption-secret", "old-access"),
		EncryptedRefreshToken: mustEncrypt(t, "encryption-secret", "old-refresh"),
		Enabled:               true,
		Priority:              30,
		Status:                AccountStatusActive,
		Metadata:              map[string]string{"chatgpt_account_id": "acct_old"},
	})
	if err != nil {
		t.Fatalf("SaveAccount returned error: %v", err)
	}
	client := &captureExchangeOAuthClient{
		exchange: TokenResponse{
			AccessToken:  "new-access",
			RefreshToken: "new-refresh",
			ExpiresIn:    3600,
			AccountID:    "acct_new_identity",
			Email:        "new@example.com",
		},
	}
	service := NewService(repo, client, Config{
		Provider:    "openai",
		RedirectURL: "http://localhost:3000/oauth/openai/callback",
		Secret:      "encryption-secret",
	})

	started, err := service.StartConnect(context.Background(), ConnectOptions{
		RedirectAfter:   "/",
		TargetAccountID: existing.ID,
		Name:            "Renamed Account",
	})
	if err != nil {
		t.Fatalf("StartConnect returned error: %v", err)
	}
	account, err := service.CompleteCallback(context.Background(), "auth-code", mustQuery(t, started.AuthorizationURL, "state"))
	if err != nil {
		t.Fatalf("CompleteCallback returned error: %v", err)
	}
	if account.ID != existing.ID {
		t.Fatalf("account ID = %d, want target account %d", account.ID, existing.ID)
	}
	if account.Name != "Renamed Account" || account.Metadata["chatgpt_account_id"] != "acct_new_identity" {
		t.Fatalf("account after reauth = %+v", account)
	}
	token, err := secret.DecryptString("encryption-secret", account.EncryptedAccessToken)
	if err != nil {
		t.Fatalf("DecryptString returned error: %v", err)
	}
	if token != "new-access" {
		t.Fatalf("token = %q, want new-access", token)
	}
}

func TestCompleteCallbackUpdatesExistingAccountByIdentityWhenNoTarget(t *testing.T) {
	repo := newMemoryRepo()
	existing, err := repo.SaveAccount(context.Background(), Account{
		Provider:              "openai",
		Subject:               "acct_same",
		Name:                  "Existing",
		DisplayName:           "same@example.com",
		EncryptedAccessToken:  mustEncrypt(t, "encryption-secret", "old-access"),
		EncryptedRefreshToken: mustEncrypt(t, "encryption-secret", "old-refresh"),
		Enabled:               true,
		Priority:              12,
		Status:                AccountStatusActive,
		Metadata:              map[string]string{"chatgpt_account_id": "acct_same"},
	})
	if err != nil {
		t.Fatalf("SaveAccount returned error: %v", err)
	}
	client := fakeOAuthClient{
		exchange: TokenResponse{
			AccessToken:  "new-access",
			RefreshToken: "new-refresh",
			ExpiresIn:    3600,
			AccountID:    "acct_same",
			Email:        "same@example.com",
		},
	}
	service := NewService(repo, client, Config{
		Provider:    "openai",
		RedirectURL: "http://localhost:3000/oauth/openai/callback",
		Secret:      "encryption-secret",
	})

	started, err := service.StartConnect(context.Background(), ConnectOptions{RedirectAfter: "/", Name: "Incoming Duplicate"})
	if err != nil {
		t.Fatalf("StartConnect returned error: %v", err)
	}
	account, err := service.CompleteCallback(context.Background(), "auth-code", mustQuery(t, started.AuthorizationURL, "state"))
	if err != nil {
		t.Fatalf("CompleteCallback returned error: %v", err)
	}
	if account.ID != existing.ID {
		t.Fatalf("account ID = %d, want existing account %d", account.ID, existing.ID)
	}
	if len(repo.accounts) != 1 {
		t.Fatalf("account count = %d, want duplicate update", len(repo.accounts))
	}
	if account.Name != "Existing" {
		t.Fatalf("account name = %q, want existing name preserved", account.Name)
	}
}

func TestCompleteCallbackExtractsIdentityMetadataFromIDTokenClaims(t *testing.T) {
	repo := newMemoryRepo()
	client := fakeOAuthClient{
		exchange: TokenResponse{
			AccessToken:  "access-token",
			RefreshToken: "refresh-token",
			IDToken: mustUnsignedIDToken(t, map[string]any{
				"sub":   "user-subject",
				"email": "owner@example.com",
				"https://api.openai.com/auth": map[string]any{
					"chatgpt_account_id": "acct_chatgpt",
					"chatgpt_plan_type":  "plus",
				},
			}),
			ExpiresIn: 3600,
			ClientID:  "codex-client",
		},
	}
	service := NewService(repo, client, Config{
		Provider:    "openai",
		RedirectURL: "http://localhost:3000/oauth/openai/callback",
		Secret:      "encryption-secret",
	})

	started, err := service.StartConnect(context.Background(), ConnectOptions{RedirectAfter: "/"})
	if err != nil {
		t.Fatalf("StartConnect returned error: %v", err)
	}
	state := mustQuery(t, started.AuthorizationURL, "state")
	account, err := service.CompleteCallback(context.Background(), "auth-code", state)
	if err != nil {
		t.Fatalf("CompleteCallback returned error: %v", err)
	}
	if account.Subject != "user-subject" || account.DisplayName != "owner@example.com" {
		t.Fatalf("account identity = %q/%q, want id token subject and email", account.Subject, account.DisplayName)
	}
	if account.Metadata["account_id"] != "acct_chatgpt" || account.Metadata["plan_type"] != "plus" {
		t.Fatalf("metadata = %+v", account.Metadata)
	}
}

func TestCompleteCallbackConsumesStateWhenTokenExchangeFails(t *testing.T) {
	repo := newMemoryRepo()
	service := newConfiguredService(repo, fakeOAuthClient{exchangeErr: errors.New("token endpoint unavailable")})

	started, err := service.StartConnect(context.Background(), ConnectOptions{RedirectAfter: "/"})
	if err != nil {
		t.Fatalf("StartConnect returned error: %v", err)
	}
	state := mustQuery(t, started.AuthorizationURL, "state")
	if _, err := service.CompleteCallback(context.Background(), "auth-code", state); err == nil {
		t.Fatal("CompleteCallback returned nil error, want token exchange error")
	}
	if repo.states[0].ConsumedAt == nil {
		t.Fatal("state was not consumed")
	}
}

func TestCompleteCallbackClaimsStateBeforeSavingTokens(t *testing.T) {
	repo := newMemoryRepo()
	client := fakeOAuthClient{
		exchange: TokenResponse{
			AccessToken:  "access-token",
			RefreshToken: "refresh-token",
			ExpiresIn:    3600,
			Subject:      "acct_1",
		},
	}
	service := newConfiguredService(repo, client)

	started, err := service.StartConnect(context.Background(), ConnectOptions{RedirectAfter: "/"})
	if err != nil {
		t.Fatalf("StartConnect returned error: %v", err)
	}
	state := mustQuery(t, started.AuthorizationURL, "state")
	if _, err := service.CompleteCallback(context.Background(), "auth-code", state); err != nil {
		t.Fatalf("CompleteCallback returned error: %v", err)
	}
	if _, err := service.CompleteCallback(context.Background(), "auth-code", state); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("replay error = %v, want ErrInvalidState", err)
	}
	if repo.saveCount != 1 {
		t.Fatalf("saveCount = %d, want 1", repo.saveCount)
	}
}

func TestCompleteCallbackGeneratesLocalSubjectsForBlankTokenSubject(t *testing.T) {
	repo := newMemoryRepo()
	service := newConfiguredService(repo, fakeOAuthClient{
		exchanges: []TokenResponse{
			{AccessToken: "access-token-1", RefreshToken: "refresh-token-1", ExpiresIn: 3600},
			{AccessToken: "access-token-2", RefreshToken: "refresh-token-2", ExpiresIn: 3600},
		},
	})

	for i := 0; i < 2; i++ {
		started, err := service.StartConnect(context.Background(), ConnectOptions{RedirectAfter: "/"})
		if err != nil {
			t.Fatalf("StartConnect %d returned error: %v", i, err)
		}
		state := mustQuery(t, started.AuthorizationURL, "state")
		if _, err := service.CompleteCallback(context.Background(), "auth-code", state); err != nil {
			t.Fatalf("CompleteCallback %d returned error: %v", i, err)
		}
	}

	if len(repo.accounts) != 2 {
		t.Fatalf("account count = %d, want 2", len(repo.accounts))
	}
	if strings.TrimSpace(repo.accounts[0].Subject) == "" || strings.TrimSpace(repo.accounts[1].Subject) == "" {
		t.Fatalf("subjects = %q/%q, want non-empty local subjects", repo.accounts[0].Subject, repo.accounts[1].Subject)
	}
	if repo.accounts[0].Subject == repo.accounts[1].Subject {
		t.Fatalf("subjects matched: %q", repo.accounts[0].Subject)
	}
}

func TestAccessTokenReturnsUnexpiredToken(t *testing.T) {
	repo := newMemoryRepo()
	expiresAt := time.Now().Add(time.Hour)
	if _, err := repo.SaveAccount(context.Background(), Account{
		Provider:              "openai",
		Subject:               "acct_1",
		EncryptedAccessToken:  mustEncrypt(t, "encryption-secret", "access-token"),
		EncryptedRefreshToken: mustEncrypt(t, "encryption-secret", "refresh-token"),
		AccessTokenExpiresAt:  &expiresAt,
	}); err != nil {
		t.Fatalf("SaveAccount returned error: %v", err)
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	token, err := service.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken returned error: %v", err)
	}
	if token != "access-token" {
		t.Fatalf("token = %q, want access-token", token)
	}
}

func TestAccessTokenRefreshesExpiredToken(t *testing.T) {
	repo := newMemoryRepo()
	expired := time.Now().Add(-time.Minute)
	if _, err := repo.SaveAccount(context.Background(), Account{
		Provider:              "openai",
		Subject:               "acct_1",
		EncryptedAccessToken:  mustEncrypt(t, "encryption-secret", "old-access"),
		EncryptedRefreshToken: mustEncrypt(t, "encryption-secret", "refresh-token"),
		AccessTokenExpiresAt:  &expired,
	}); err != nil {
		t.Fatalf("SaveAccount returned error: %v", err)
	}
	service := newConfiguredService(repo, fakeOAuthClient{
		refresh: TokenResponse{
			AccessToken:  "new-access",
			RefreshToken: "new-refresh",
			ExpiresIn:    3600,
		},
	})

	token, err := service.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken returned error: %v", err)
	}
	if token != "new-access" {
		t.Fatalf("token = %q, want new-access", token)
	}
	if repo.accounts[0].LastRefreshAt == nil {
		t.Fatal("LastRefreshAt was not updated")
	}
}

func TestAccessTokenForAccountRefreshPreservesExistingIdentityAndRefreshToken(t *testing.T) {
	repo := newMemoryRepo()
	expired := time.Now().Add(-time.Minute)
	repo.accounts = []Account{
		testExpiredAccount(t, 7, false, 3, "old-access", "old-refresh", expired),
	}
	repo.accounts[0].Subject = "acct_original"
	service := newConfiguredService(repo, fakeOAuthClient{
		refresh: TokenResponse{
			AccessToken: "new-access",
			ExpiresIn:   3600,
			Subject:     "acct_changed",
			DisplayName: "Updated Name",
		},
	})

	token, err := service.AccessTokenForAccount(context.Background(), repo.accounts[0])
	if err != nil {
		t.Fatalf("AccessTokenForAccount returned error: %v", err)
	}
	if token != "new-access" {
		t.Fatalf("token = %q, want new-access", token)
	}
	if len(repo.accounts) != 1 {
		t.Fatalf("account count = %d, want 1", len(repo.accounts))
	}
	account := repo.accounts[0]
	if account.ID != 7 || account.Subject != "acct_original" || account.Enabled || account.Priority != 3 {
		t.Fatalf("account = %+v, want original id/subject/enabled/priority preserved", account)
	}
	refreshToken, err := secret.DecryptString("encryption-secret", account.EncryptedRefreshToken)
	if err != nil {
		t.Fatalf("DecryptString returned error: %v", err)
	}
	if refreshToken != "old-refresh" {
		t.Fatalf("refreshToken = %q, want old-refresh", refreshToken)
	}
}

func TestAccessTokenForAccountSerializesConcurrentRefresh(t *testing.T) {
	repo := newMemoryRepo()
	expired := time.Now().Add(-time.Minute)
	repo.accounts = []Account{
		testExpiredAccount(t, 7, true, 3, "old-access", "old-refresh", expired),
	}
	client := &blockingOAuthClient{
		refresh: TokenResponse{
			AccessToken:  "new-access",
			RefreshToken: "new-refresh",
			ExpiresIn:    3600,
		},
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	service := newConfiguredService(repo, client)

	errs := make(chan error, 2)
	tokens := make(chan string, 2)
	for i := 0; i < 2; i++ {
		go func() {
			token, err := service.AccessTokenForAccount(context.Background(), repo.accounts[0])
			if err != nil {
				errs <- err
				return
			}
			tokens <- token
			errs <- nil
		}()
	}

	<-client.entered
	client.release <- struct{}{}
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("AccessTokenForAccount returned error: %v", err)
		}
	}
	for i := 0; i < 2; i++ {
		if token := <-tokens; token != "new-access" {
			t.Fatalf("token = %q, want new-access", token)
		}
	}
	if client.calls != 1 {
		t.Fatalf("refresh calls = %d, want 1", client.calls)
	}
}

func TestSelectAccountForModelSkipsDisabledAndUsesPriority(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 1, false, 1, "disabled-token"),
		testAccount(t, 2, true, 10, "enabled-token"),
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	selected, err := service.SelectAccountForModel(context.Background(), "")
	if err != nil {
		t.Fatalf("SelectAccountForModel returned error: %v", err)
	}
	if selected.AccountID != 2 || selected.AuthorizationToken != "enabled-token" {
		t.Fatalf("selected = %+v", selected)
	}
}

func TestSelectAccountForModelReturnsChatGPTAccountMetadata(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{testAccount(t, 7, true, 1, "access-token")}
	repo.accounts[0].Metadata = map[string]string{"chatgpt_account_id": "acct_chatgpt"}
	service := newConfiguredService(repo, fakeOAuthClient{})

	selected, err := service.SelectAccountForModel(context.Background(), "")
	if err != nil {
		t.Fatalf("SelectAccountForModel returned error: %v", err)
	}
	if selected.AccountID != 7 || selected.AuthorizationToken != "access-token" {
		t.Fatalf("selected = %+v, want account 7 authorization token", selected)
	}
	if selected.ChatGPTAccountID != "acct_chatgpt" {
		t.Fatalf("ChatGPTAccountID = %q, want metadata value", selected.ChatGPTAccountID)
	}
}

func TestSelectAccountForModelReturnsAPIUpstreamCredentialAndBaseURL(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{{
		ID:          11,
		Provider:    "openai",
		AccountType: AccountTypeAPIUpstream,
		Name:        "Upstream A",
		Subject:     "api-upstream",
		Credential: AccountCredential{
			CredentialType:  CredentialTypeAPIKey,
			EncryptedAPIKey: mustEncrypt(t, "encryption-secret", "sk-upstream"),
			BaseURL:         "https://upstream.example.test",
		},
		Enabled:  true,
		Priority: 1,
		Status:   AccountStatusActive,
		Metadata: map[string]string{},
	}}
	service := newConfiguredService(repo, fakeOAuthClient{refreshErr: errors.New("oauth refresh should not be called")})

	selected, err := service.SelectAccountForModel(context.Background(), "")
	if err != nil {
		t.Fatalf("SelectAccountForModel returned error: %v", err)
	}
	if selected.AccountID != 11 || selected.Provider != "openai" || selected.AccountType != AccountTypeAPIUpstream {
		t.Fatalf("selected account identity = %+v", selected)
	}
	if selected.DisplayName != "Upstream A" {
		t.Fatalf("DisplayName = %q, want account name snapshot", selected.DisplayName)
	}
	if selected.AuthorizationToken != "sk-upstream" {
		t.Fatalf("AuthorizationToken = %q, want upstream API key", selected.AuthorizationToken)
	}
	if selected.BaseURL != "https://upstream.example.test" {
		t.Fatalf("BaseURL = %q, want upstream base URL", selected.BaseURL)
	}
	if repo.accounts[0].LastUsedAt == nil {
		t.Fatal("API upstream account was not marked used")
	}
}

func TestSelectAccountForModelSkipsRateLimitedCircuitOpenAndExpiredAccounts(t *testing.T) {
	repo := newMemoryRepo()
	now := time.Now()
	expired := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "rate-limited-token"),
		testAccount(t, 2, true, 2, "circuit-open-token"),
		testExpiredAccount(t, 3, true, 3, "expired-token", "expired-refresh", expired),
		testAccount(t, 4, true, 4, "usable-token"),
	}
	repo.accounts[0].RateLimitedUntil = &future
	repo.accounts[1].CircuitOpenUntil = &future
	repo.accounts[2].Status = AccountStatusExpired
	service := newConfiguredService(repo, fakeOAuthClient{refreshErr: errors.New("refresh failed")})

	selected, err := service.SelectAccountForModel(context.Background(), "")
	if err != nil {
		t.Fatalf("SelectAccountForModel returned error: %v", err)
	}
	if selected.AccountID != 4 || selected.AuthorizationToken != "usable-token" {
		t.Fatalf("selected = %+v, want usable account 4", selected)
	}
}

func TestSelectAccountForModelAllowsExpiredRateLimitAndCircuitWindows(t *testing.T) {
	repo := newMemoryRepo()
	past := time.Now().Add(-time.Minute)
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "rate-limited-token"),
	}
	repo.accounts[0].Status = AccountStatusRateLimited
	repo.accounts[0].RateLimitedUntil = &past
	service := newConfiguredService(repo, fakeOAuthClient{})

	selected, err := service.SelectAccountForModel(context.Background(), "")
	if err != nil {
		t.Fatalf("SelectAccountForModel returned error: %v", err)
	}
	if selected.AccountID != 1 || selected.AuthorizationToken != "rate-limited-token" {
		t.Fatalf("selected = %+v, want recovered account 1", selected)
	}

	repo.accounts[0].Status = AccountStatusCircuitOpen
	repo.accounts[0].CircuitOpenUntil = &past
	selected, err = service.SelectAccountForModel(context.Background(), "")
	if err != nil {
		t.Fatalf("SelectAccountForModel after circuit window returned error: %v", err)
	}
	if selected.AccountID != 1 {
		t.Fatalf("selected = %+v, want recovered circuit account 1", selected)
	}
}

func TestAccessTokenForAccountRefreshFailureOpensCircuitAfterThreshold(t *testing.T) {
	repo := newMemoryRepo()
	expired := time.Now().Add(-time.Minute)
	account := testExpiredAccount(t, 7, true, 3, "old-access", "old-refresh", expired)
	repo.accounts = []Account{account}
	service := newConfiguredService(repo, fakeOAuthClient{refreshErr: errors.New("refresh failed")})

	for i := 0; i < refreshFailureCircuitThreshold; i++ {
		_, _ = service.AccessTokenForAccount(context.Background(), repo.accounts[0])
	}
	if repo.accounts[0].FailureCount != refreshFailureCircuitThreshold {
		t.Fatalf("FailureCount = %d, want %d", repo.accounts[0].FailureCount, refreshFailureCircuitThreshold)
	}
	if repo.accounts[0].CircuitOpenUntil == nil || !repo.accounts[0].CircuitOpenUntil.After(time.Now()) {
		t.Fatalf("CircuitOpenUntil = %v, want future circuit", repo.accounts[0].CircuitOpenUntil)
	}
	if repo.accounts[0].Status != AccountStatusCircuitOpen {
		t.Fatalf("Status = %q, want circuit_open", repo.accounts[0].Status)
	}
}

func TestRefreshAccountForcesOAuthTokenRefreshAndClearsFailureState(t *testing.T) {
	repo := newMemoryRepo()
	expiresAt := time.Now().Add(time.Hour)
	repo.accounts = []Account{
		testAccount(t, 7, true, 3, "old-access"),
	}
	repo.accounts[0].EncryptedRefreshToken = mustEncrypt(t, "encryption-secret", "refresh-token")
	repo.accounts[0].AccessTokenExpiresAt = &expiresAt
	repo.accounts[0].Status = AccountStatusCircuitOpen
	repo.accounts[0].StatusReason = "previous failure"
	repo.accounts[0].FailureCount = 3
	future := time.Now().Add(time.Hour)
	repo.accounts[0].CircuitOpenUntil = &future
	service := newConfiguredService(repo, fakeOAuthClient{
		refresh: TokenResponse{
			AccessToken:  "new-access",
			RefreshToken: "new-refresh",
			ExpiresIn:    3600,
		},
	})

	account, err := service.RefreshAccount(context.Background(), 7)
	if err != nil {
		t.Fatalf("RefreshAccount returned error: %v", err)
	}
	token, err := secret.DecryptString("encryption-secret", account.EncryptedAccessToken)
	if err != nil {
		t.Fatalf("DecryptString returned error: %v", err)
	}
	if token != "new-access" {
		t.Fatalf("token = %q, want new-access", token)
	}
	if account.Status != AccountStatusActive || account.FailureCount != 0 || account.CircuitOpenUntil != nil || account.LastRefreshAt == nil {
		t.Fatalf("account after refresh = %+v", account)
	}
}

func TestRefreshAccountProbesLatestStatusAfterTokenRefresh(t *testing.T) {
	repo := newMemoryRepo()
	expiresAt := time.Now().Add(time.Hour)
	repo.accounts = []Account{
		testAccount(t, 7, true, 3, "old-access"),
	}
	repo.accounts[0].EncryptedRefreshToken = mustEncrypt(t, "encryption-secret", "refresh-token")
	repo.accounts[0].AccessTokenExpiresAt = &expiresAt
	service := newConfiguredService(repo, fakeOAuthClient{
		refresh: TokenResponse{
			AccessToken:  "new-access",
			RefreshToken: "new-refresh",
			ExpiresIn:    3600,
		},
		probe: probeResult{
			statusCode: http.StatusTooManyRequests,
			retryAfter: "120",
			message:    "usage limit reached",
		},
	})

	account, err := service.RefreshAccount(context.Background(), 7)
	if err != nil {
		t.Fatalf("RefreshAccount returned error: %v", err)
	}
	if account.Status != AccountStatusRateLimited {
		t.Fatalf("Status = %q, want rate_limited", account.Status)
	}
	if account.StatusReason != "usage limit reached" || account.LastError != "usage limit reached" {
		t.Fatalf("account failure message not recorded: %+v", account)
	}
	if account.RateLimitedUntil == nil || !account.RateLimitedUntil.After(time.Now().Add(100*time.Second)) {
		t.Fatalf("RateLimitedUntil = %v, want retry-after window", account.RateLimitedUntil)
	}
}

func TestRecordAccountFailureMapsUpstreamStatusesToAccountState(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{testAccount(t, 7, true, 1, "access-token")}
	service := newConfiguredService(repo, fakeOAuthClient{})

	if err := service.RecordAccountFailure(context.Background(), 7, 429, "120", "rate limited"); err != nil {
		t.Fatalf("RecordAccountFailure rate limit returned error: %v", err)
	}
	if repo.accounts[0].Status != AccountStatusRateLimited || repo.accounts[0].RateLimitedUntil == nil || !repo.accounts[0].RateLimitedUntil.After(time.Now().Add(100*time.Second)) {
		t.Fatalf("rate limited account = %+v", repo.accounts[0])
	}
	if repo.accounts[0].LastError != "rate limited" || repo.accounts[0].LastErrorAt == nil {
		t.Fatalf("last error not recorded: %+v", repo.accounts[0])
	}

	if err := service.RecordAccountFailure(context.Background(), 7, 401, "", "invalid token"); err != nil {
		t.Fatalf("RecordAccountFailure unauthorized returned error: %v", err)
	}
	if repo.accounts[0].Status != AccountStatusExpired || repo.accounts[0].StatusReason != "invalid token" {
		t.Fatalf("expired account = %+v", repo.accounts[0])
	}

	if err := service.RecordAccountFailure(context.Background(), 7, 502, "", "upstream failed"); err != nil {
		t.Fatalf("RecordAccountFailure server error returned error: %v", err)
	}
	if repo.accounts[0].Status != AccountStatusCircuitOpen || repo.accounts[0].CircuitOpenUntil == nil || !repo.accounts[0].CircuitOpenUntil.After(time.Now()) {
		t.Fatalf("circuit account = %+v", repo.accounts[0])
	}
}

func TestRecordAccountFailureDoesNotExpireAccountForResponsesScopeForbidden(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{testAccount(t, 7, true, 1, "access-token")}
	repo.accounts[0].Status = AccountStatusActive
	service := newConfiguredService(repo, fakeOAuthClient{})
	message := "You have insufficient permissions for this operation. Missing scopes: api.responses.write."

	if err := service.RecordAccountFailure(context.Background(), 7, 403, "", message); err != nil {
		t.Fatalf("RecordAccountFailure returned error: %v", err)
	}
	if repo.accounts[0].Status != AccountStatusActive {
		t.Fatalf("status = %q, want active", repo.accounts[0].Status)
	}
	if repo.accounts[0].StatusReason != "" {
		t.Fatalf("status reason = %q, want empty", repo.accounts[0].StatusReason)
	}
	if repo.accounts[0].LastError != message || repo.accounts[0].LastErrorAt == nil {
		t.Fatalf("last error = %q at %v, want recorded message", repo.accounts[0].LastError, repo.accounts[0].LastErrorAt)
	}
}

func TestResetAccountStatusClearsLocalFailureWindows(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{testAccount(t, 7, true, 1, "access-token")}
	now := time.Now()
	future := now.Add(time.Hour)
	repo.accounts[0].Status = AccountStatusRateLimited
	repo.accounts[0].StatusReason = "rate limited"
	repo.accounts[0].LastError = "rate limited"
	repo.accounts[0].LastErrorAt = &now
	repo.accounts[0].RateLimitedUntil = &future
	repo.accounts[0].CircuitOpenUntil = &future
	repo.accounts[0].FailureCount = 3
	service := newConfiguredService(repo, fakeOAuthClient{})

	account, err := service.ResetAccountStatus(context.Background(), 7)
	if err != nil {
		t.Fatalf("ResetAccountStatus returned error: %v", err)
	}
	if account.ID != 7 || account.Status != AccountStatusActive || account.StatusReason != "" || account.LastError != "" || account.LastErrorAt != nil {
		t.Fatalf("account status fields = %+v, want active without errors", account)
	}
	if account.RateLimitedUntil != nil || account.CircuitOpenUntil != nil || account.FailureCount != 0 {
		t.Fatalf("account failure windows = %+v, want cleared", account)
	}
}

func TestSelectAccountForModelUsesPriorityOrder(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 1, true, 10, "low-priority-token"),
		testAccount(t, 2, true, 1, "high-priority-token"),
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	selected, err := service.SelectAccountForModel(context.Background(), "")
	if err != nil {
		t.Fatalf("SelectAccountForModel returned error: %v", err)
	}
	if selected.AccountID != 2 || selected.AuthorizationToken != "high-priority-token" {
		t.Fatalf("selected = %+v", selected)
	}
}

func TestUpdateAccountCanRenameLocalAccountLabel(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{testAccount(t, 7, true, 1, "access-token")}
	service := newConfiguredService(repo, fakeOAuthClient{})
	name := " Work Codex "

	account, err := service.UpdateAccount(context.Background(), 7, AccountUpdate{Name: &name})
	if err != nil {
		t.Fatalf("UpdateAccount returned error: %v", err)
	}
	if account.Name != "Work Codex" {
		t.Fatalf("Name = %q, want trimmed Work Codex", account.Name)
	}
	if account.DisplayName == "" {
		t.Fatalf("DisplayName = %q, want provider display name preserved", account.DisplayName)
	}
}

func TestUpdateAccountRejectsEmptyLocalAccountLabel(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{testAccount(t, 7, true, 1, "access-token")}
	service := newConfiguredService(repo, fakeOAuthClient{})
	name := " "

	if _, err := service.UpdateAccount(context.Background(), 7, AccountUpdate{Name: &name}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("UpdateAccount error = %v, want ErrInvalidInput", err)
	}
}

func TestSelectAccountForModelSkipsExcludedAccount(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "first-token"),
		testAccount(t, 2, true, 2, "fallback-token"),
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	selected, err := service.SelectAccountForModel(context.Background(), "", 1)
	if err != nil {
		t.Fatalf("SelectAccountForModel returned error: %v", err)
	}
	if selected.AccountID != 2 || selected.AuthorizationToken != "fallback-token" {
		t.Fatalf("selected = %+v, want account 2 fallback-token", selected)
	}
}

func TestSelectAccountForModelFiltersByRequestedModel(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "gpt-4.1-token"),
		testAccount(t, 2, true, 2, "gpt-5-token"),
	}
	repo.accountModels = map[int64][]AccountModel{
		1: {{AccountID: 1, Provider: "openai", Model: "gpt-4.1", Enabled: true}},
		2: {{AccountID: 2, Provider: "openai", Model: "gpt-5", Enabled: true}},
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	selected, err := service.SelectAccountForModel(context.Background(), "gpt-5")
	if err != nil {
		t.Fatalf("SelectAccountForModel returned error: %v", err)
	}
	if selected.AccountID != 2 || selected.AuthorizationToken != "gpt-5-token" {
		t.Fatalf("selected = %+v, want account 2 gpt-5-token", selected)
	}
}

func TestSelectAccountForModelReturnsModelUnavailableWhenNoAccountSupportsModel(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "access-token"),
	}
	repo.accountModels = map[int64][]AccountModel{
		1: {{AccountID: 1, Provider: "openai", Model: "gpt-4.1", Enabled: true}},
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	if _, err := service.SelectAccountForModel(context.Background(), "gpt-5"); !errors.Is(err, ErrModelUnavailable) {
		t.Fatalf("SelectAccountForModel error = %v, want ErrModelUnavailable", err)
	}
}

func TestSelectAccountForModelReturnsAccountsDisabledWhenAllModelAccountsDisabled(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 1, false, 1, "access-token"),
	}
	repo.accountModels = map[int64][]AccountModel{
		1: {{AccountID: 1, Provider: "openai", Model: "gpt-5", Enabled: true}},
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	if _, err := service.SelectAccountForModel(context.Background(), "gpt-5"); !errors.Is(err, ErrAccountsDisabled) {
		t.Fatalf("SelectAccountForModel error = %v, want ErrAccountsDisabled", err)
	}
}

func TestSelectAccountForModelReturnsAccountsUnavailableWhenOnlyModelAccountExcluded(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "access-token"),
	}
	repo.accountModels = map[int64][]AccountModel{
		1: {{AccountID: 1, Provider: "openai", Model: "gpt-5", Enabled: true}},
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	if _, err := service.SelectAccountForModel(context.Background(), "gpt-5", 1); !errors.Is(err, ErrAccountsUnavailable) {
		t.Fatalf("SelectAccountForModel error = %v, want ErrAccountsUnavailable", err)
	}
}

func TestSelectAccountForModelReturnsAccountsUnavailableWhenModelAccountsCannotProvideToken(t *testing.T) {
	repo := newMemoryRepo()
	badToken := testAccount(t, 1, true, 1, "access-token")
	badToken.EncryptedAccessToken = "not-encrypted"
	repo.accounts = []Account{badToken}
	repo.accountModels = map[int64][]AccountModel{
		1: {{AccountID: 1, Provider: "openai", Model: "gpt-5", Enabled: true}},
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	if _, err := service.SelectAccountForModel(context.Background(), "gpt-5"); !errors.Is(err, ErrAccountsUnavailable) {
		t.Fatalf("SelectAccountForModel error = %v, want ErrAccountsUnavailable", err)
	}
	if repo.accounts[0].LastError == "" {
		t.Fatal("account credential lookup failure was not marked")
	}
}

func TestSelectAccountForModelIgnoresDisabledModelRows(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "disabled-model-token"),
		testAccount(t, 2, true, 2, "enabled-model-token"),
	}
	repo.accountModels = map[int64][]AccountModel{
		1: {{AccountID: 1, Provider: "openai", Model: "gpt-5", Enabled: false}},
		2: {{AccountID: 2, Provider: "openai", Model: "gpt-5", Enabled: true}},
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	selected, err := service.SelectAccountForModel(context.Background(), "gpt-5")
	if err != nil {
		t.Fatalf("SelectAccountForModel returned error: %v", err)
	}
	if selected.AccountID != 2 || selected.AuthorizationToken != "enabled-model-token" {
		t.Fatalf("selected = %+v, want enabled model account 2", selected)
	}
}

func TestSelectAccountForModelRespectsExcludedAccountIDsWithModelFilter(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "first-token"),
		testAccount(t, 2, true, 2, "fallback-token"),
	}
	repo.accountModels = map[int64][]AccountModel{
		1: {{AccountID: 1, Provider: "openai", Model: "gpt-5", Enabled: true}},
		2: {{AccountID: 2, Provider: "openai", Model: "gpt-5", Enabled: true}},
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	selected, err := service.SelectAccountForModel(context.Background(), "gpt-5", 1)
	if err != nil {
		t.Fatalf("SelectAccountForModel returned error: %v", err)
	}
	if selected.AccountID != 2 || selected.AuthorizationToken != "fallback-token" {
		t.Fatalf("selected = %+v, want non-excluded account 2", selected)
	}
}

func TestSelectAccountForModelAndSessionUsesStickyHashAcrossCandidateOrder(t *testing.T) {
	now := time.Now()
	recent := now.Add(-time.Minute)
	older := now.Add(-time.Hour)
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "first-token"),
		testAccount(t, 2, true, 1, "second-token"),
		testAccount(t, 3, true, 1, "third-token"),
	}
	for i := range repo.accounts {
		repo.accounts[i].LastUsedAt = &older
		repo.accountModels[repo.accounts[i].ID] = []AccountModel{
			{AccountID: repo.accounts[i].ID, Provider: "openai", Model: "gpt-5", Enabled: true},
		}
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	selected, err := service.SelectAccountForModelAndSession(context.Background(), "gpt-5", "workspace-123")
	if err != nil {
		t.Fatalf("SelectAccountForModelAndSession returned error: %v", err)
	}
	repo.accounts[0].LastUsedAt = &recent
	repo.accounts[1].LastUsedAt = nil
	repo.accounts[2].LastUsedAt = &older
	again, err := service.SelectAccountForModelAndSession(context.Background(), "gpt-5", "workspace-123")
	if err != nil {
		t.Fatalf("SelectAccountForModelAndSession after reorder returned error: %v", err)
	}
	if again.AccountID != selected.AccountID {
		t.Fatalf("sticky account = %d after reorder, want original %d", again.AccountID, selected.AccountID)
	}
	fallback, err := service.SelectAccountForModelAndSession(context.Background(), "gpt-5", "workspace-123", selected.AccountID)
	if err != nil {
		t.Fatalf("SelectAccountForModelAndSession fallback returned error: %v", err)
	}
	if fallback.AccountID == selected.AccountID {
		t.Fatalf("fallback account = %d, want account different from excluded sticky account", fallback.AccountID)
	}
}

func TestSelectAccountForModelAndSessionKeepsStickySelectionInsideHighestPriorityGroup(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "high-priority-first"),
		testAccount(t, 2, true, 1, "high-priority-second"),
		testAccount(t, 3, true, 50, "low-priority"),
	}
	for i := range repo.accounts {
		repo.accountModels[repo.accounts[i].ID] = []AccountModel{
			{AccountID: repo.accounts[i].ID, Provider: "openai", Model: "gpt-5", Enabled: true},
		}
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	for _, sessionID := range []string{
		"workspace-1",
		"workspace-2",
		"workspace-3",
		"workspace-4",
		"workspace-5",
		"workspace-6",
	} {
		selected, err := service.SelectAccountForModelAndSession(context.Background(), "gpt-5", sessionID)
		if err != nil {
			t.Fatalf("SelectAccountForModelAndSession(%q) returned error: %v", sessionID, err)
		}
		if selected.AccountID == 3 {
			t.Fatalf("SelectAccountForModelAndSession(%q) selected low-priority account %+v", sessionID, selected)
		}
	}
}

func TestPreviewAccountSelectionUsesStickySessionWithoutMarkingAccountUsed(t *testing.T) {
	older := time.Now().Add(-time.Hour)
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "first-token"),
		testAccount(t, 2, true, 1, "second-token"),
		testAccount(t, 3, true, 50, "low-priority-token"),
	}
	for i := range repo.accounts {
		repo.accounts[i].LastUsedAt = &older
		repo.accountModels[repo.accounts[i].ID] = []AccountModel{
			{AccountID: repo.accounts[i].ID, Provider: "openai", Model: "gpt-5", Enabled: true},
		}
	}
	service := newConfiguredService(repo, fakeOAuthClient{})
	sessionID := "workspace-123"
	expectedStart := stickyAccountIndex(sessionID, 2) + 1

	preview, err := service.PreviewAccountSelection(context.Background(), "gpt-5", sessionID)
	if err != nil {
		t.Fatalf("PreviewAccountSelection returned error: %v", err)
	}

	if preview.Model != "gpt-5" || preview.SessionID != sessionID {
		t.Fatalf("preview metadata = %+v, want model and session", preview)
	}
	if preview.SelectedAccountID != int64(expectedStart) {
		t.Fatalf("selected account = %d, want sticky high-priority account %d", preview.SelectedAccountID, expectedStart)
	}
	if len(preview.Candidates) != 3 {
		t.Fatalf("candidates = %+v, want three candidates", preview.Candidates)
	}
	if !preview.Candidates[0].Selected || preview.Candidates[0].ID != preview.SelectedAccountID {
		t.Fatalf("first candidate = %+v, want selected account first", preview.Candidates[0])
	}
	if preview.Candidates[2].ID != 3 || preview.Candidates[2].Priority != 50 {
		t.Fatalf("last candidate = %+v, want low-priority account", preview.Candidates[2])
	}
	for _, account := range repo.accounts {
		if account.LastUsedAt == nil || !account.LastUsedAt.Equal(older) {
			t.Fatalf("account %d last used = %v, want unchanged %v", account.ID, account.LastUsedAt, older)
		}
	}
}

func TestReplaceAndListAccountModelsNormalizeInputs(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 7, true, 1, "access-token"),
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	models, err := service.ReplaceAccountModels(context.Background(), 7, []AccountModelInput{
		{Model: " gpt-5 ", Enabled: false},
		{Model: "", Enabled: true},
		{Model: "gpt-4.1", Enabled: true},
		{Model: "gpt-5", Enabled: true},
	})
	if err != nil {
		t.Fatalf("ReplaceAccountModels returned error: %v", err)
	}
	if got := modelNamesAndEnabled(models); strings.Join(got, ",") != "gpt-4.1:true,gpt-5:false" {
		t.Fatalf("models = %v, want normalized sorted unique models", got)
	}

	listed, err := service.ListAccountModels(context.Background(), 7)
	if err != nil {
		t.Fatalf("ListAccountModels returned error: %v", err)
	}
	if got := modelNamesAndEnabled(listed); strings.Join(got, ",") != "gpt-4.1:true,gpt-5:false" {
		t.Fatalf("listed models = %v, want saved normalized models", got)
	}

	if _, err := service.ReplaceAccountModels(context.Background(), 7, []AccountModelInput{{Model: strings.Repeat("x", maxModelNameLen+1), Enabled: true}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ReplaceAccountModels long model error = %v, want ErrInvalidInput", err)
	}
	tooMany := make([]AccountModelInput, maxAccountModels+1)
	for i := range tooMany {
		tooMany[i] = AccountModelInput{Model: "model-" + strconv.Itoa(i), Enabled: true}
	}
	if _, err := service.ReplaceAccountModels(context.Background(), 7, tooMany); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ReplaceAccountModels too many models error = %v, want ErrInvalidInput", err)
	}
}

func TestCreateAPIUpstreamAccountSavesEncryptedKeyAndEnabledModels(t *testing.T) {
	repo := newMemoryRepo()
	service := newConfiguredService(repo, fakeOAuthClient{})

	account, err := service.CreateAPIUpstreamAccount(context.Background(), APIUpstreamInput{
		Name:     "  OpenAI proxy  ",
		BaseURL:  "https://upstream.example.test/v1/ ",
		APIKey:   " upstream-secret ",
		Enabled:  boolPtr(true),
		Priority: 12,
		Models: []string{
			" gpt-5 ",
			"gpt-4.1",
		},
	})
	if err != nil {
		t.Fatalf("CreateAPIUpstreamAccount returned error: %v", err)
	}
	if account.ID == 0 || account.Provider != "openai" || account.AccountType != AccountTypeAPIUpstream {
		t.Fatalf("account identity = %+v", account)
	}
	if account.Name != "OpenAI proxy" || account.Credential.BaseURL != "https://upstream.example.test" {
		t.Fatalf("account = %+v, want trimmed name and normalized base URL", account)
	}
	if !account.Enabled || account.Priority != 12 || account.Status != AccountStatusActive {
		t.Fatalf("account scheduling = %+v, want enabled priority 12 active", account)
	}
	if account.Credential.CredentialType != CredentialTypeAPIKey {
		t.Fatalf("credential type = %q, want api key", account.Credential.CredentialType)
	}
	if account.Credential.EncryptedAPIKey == "" || account.Credential.EncryptedAPIKey == "upstream-secret" {
		t.Fatalf("encrypted API key = %q, want encrypted non-plaintext value", account.Credential.EncryptedAPIKey)
	}
	decrypted, err := secret.DecryptString("encryption-secret", account.Credential.EncryptedAPIKey)
	if err != nil {
		t.Fatalf("DecryptString returned error: %v", err)
	}
	if decrypted != "upstream-secret" {
		t.Fatalf("decrypted API key = %q, want upstream-secret", decrypted)
	}
	if saved := repo.accounts[0]; saved.Credential.EncryptedAPIKey != account.Credential.EncryptedAPIKey || saved.Credential.BaseURL != "https://upstream.example.test" {
		t.Fatalf("saved account credential = %+v", saved.Credential)
	}
	models, err := service.ListAccountModels(context.Background(), account.ID)
	if err != nil {
		t.Fatalf("ListAccountModels returned error: %v", err)
	}
	if got := modelNamesAndEnabled(models); strings.Join(got, ",") != "gpt-4.1:true,gpt-5:true" {
		t.Fatalf("models = %v, want enabled normalized models", got)
	}
}

func TestCreateAPIUpstreamAccountDefaultsEnabledWhenOmitted(t *testing.T) {
	repo := newMemoryRepo()
	service := newConfiguredService(repo, fakeOAuthClient{})

	account, err := service.CreateAPIUpstreamAccount(context.Background(), APIUpstreamInput{
		Name:    "OpenAI proxy",
		BaseURL: "https://upstream.example.test/v1",
		APIKey:  "upstream-secret",
	})
	if err != nil {
		t.Fatalf("CreateAPIUpstreamAccount returned error: %v", err)
	}
	if !repo.lastSavedAccount.Enabled {
		t.Fatalf("raw saved account.Enabled = false, want service to default omitted enabled before persistence")
	}
	if !account.Enabled {
		t.Fatalf("account.Enabled = false, want omitted enabled to default true")
	}
}

func TestCreateAPIUpstreamAccountDeletesAccountWhenInitialModelsFail(t *testing.T) {
	repo := newMemoryRepo()
	repo.replaceModelsErr = errors.New("replace models failed")
	service := newConfiguredService(repo, fakeOAuthClient{})

	if _, err := service.CreateAPIUpstreamAccount(context.Background(), APIUpstreamInput{
		Name:    "Upstream",
		BaseURL: "https://upstream.example.test/v1",
		APIKey:  "secret",
		Models:  []string{"gpt-5"},
	}); !errors.Is(err, repo.replaceModelsErr) {
		t.Fatalf("CreateAPIUpstreamAccount error = %v, want replaceModelsErr", err)
	}
	if len(repo.accounts) != 0 {
		t.Fatalf("accounts = %+v, want saved account removed after model failure", repo.accounts)
	}
}

func TestCreateAPIUpstreamAccountRejectsInvalidInput(t *testing.T) {
	service := newConfiguredService(newMemoryRepo(), fakeOAuthClient{})
	valid := APIUpstreamInput{Name: "Upstream", BaseURL: "https://upstream.example.test/v1", APIKey: "secret"}

	for _, tc := range []struct {
		name  string
		input APIUpstreamInput
	}{
		{name: "missing name", input: APIUpstreamInput{Name: " ", BaseURL: valid.BaseURL, APIKey: valid.APIKey}},
		{name: "missing base URL", input: APIUpstreamInput{Name: valid.Name, BaseURL: " ", APIKey: valid.APIKey}},
		{name: "invalid base URL", input: APIUpstreamInput{Name: valid.Name, BaseURL: "://bad", APIKey: valid.APIKey}},
		{name: "relative base URL", input: APIUpstreamInput{Name: valid.Name, BaseURL: "/v1", APIKey: valid.APIKey}},
		{name: "host without scheme", input: APIUpstreamInput{Name: valid.Name, BaseURL: "upstream.example.test", APIKey: valid.APIKey}},
		{name: "file scheme", input: APIUpstreamInput{Name: valid.Name, BaseURL: "file:///tmp/upstream", APIKey: valid.APIKey}},
		{name: "mailto scheme", input: APIUpstreamInput{Name: valid.Name, BaseURL: "mailto:test@example.com", APIKey: valid.APIKey}},
		{name: "https without host", input: APIUpstreamInput{Name: valid.Name, BaseURL: "https:///v1", APIKey: valid.APIKey}},
		{name: "missing API key", input: APIUpstreamInput{Name: valid.Name, BaseURL: valid.BaseURL, APIKey: " "}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := service.CreateAPIUpstreamAccount(context.Background(), tc.input); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("CreateAPIUpstreamAccount error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestSelectAccountForModelReturnsUnavailableWhenAllEnabledAccountsExcluded(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "only-token"),
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	if _, err := service.SelectAccountForModel(context.Background(), "", 1); !errors.Is(err, ErrAccountsUnavailable) {
		t.Fatalf("SelectAccountForModel error = %v, want accounts unavailable", err)
	}
}

func TestSelectAccountForModelFallsBackWhenRefreshFails(t *testing.T) {
	repo := newMemoryRepo()
	expired := time.Now().Add(-time.Minute)
	repo.accounts = []Account{
		testExpiredAccount(t, 1, true, 1, "old-token", "bad-refresh", expired),
		testAccount(t, 2, true, 2, "fallback-token"),
	}
	service := newConfiguredService(repo, fakeOAuthClient{refreshErr: errors.New("refresh failed")})

	selected, err := service.SelectAccountForModel(context.Background(), "")
	if err != nil {
		t.Fatalf("SelectAccountForModel returned error: %v", err)
	}
	if selected.AccountID != 2 || selected.AuthorizationToken != "fallback-token" {
		t.Fatalf("selected = %+v", selected)
	}
	if repo.accounts[0].LastError == "" {
		t.Fatal("first account error was not marked")
	}
}

func TestSelectAccountForModelReturnsMarkAccountErrorFailure(t *testing.T) {
	repo := newMemoryRepo()
	expired := time.Now().Add(-time.Minute)
	repo.accounts = []Account{
		testExpiredAccount(t, 1, true, 1, "old-token", "bad-refresh", expired),
		testAccount(t, 2, true, 2, "fallback-token"),
	}
	repo.markAccountErrorErr = errors.New("mark account error failed")
	service := newConfiguredService(repo, fakeOAuthClient{refreshErr: errors.New("refresh failed")})

	if _, err := service.SelectAccountForModel(context.Background(), ""); !errors.Is(err, repo.markAccountErrorErr) {
		t.Fatalf("SelectAccountForModel error = %v, want mark account error failure", err)
	}
}

func TestSelectAccountForModelReturnsMarkAccountUsedFailure(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "access-token"),
	}
	repo.markAccountUsedErr = errors.New("mark account used failed")
	service := newConfiguredService(repo, fakeOAuthClient{})

	if _, err := service.SelectAccountForModel(context.Background(), ""); !errors.Is(err, repo.markAccountUsedErr) {
		t.Fatalf("SelectAccountForModel error = %v, want mark account used failure", err)
	}
}

type memoryRepo struct {
	accounts      []Account
	accountModels map[int64][]AccountModel
	states        []OAuthState

	saveCount           int
	nextID              int64
	markAccountErrorErr error
	markAccountUsedErr  error
	replaceModelsErr    error
	lastSavedAccount    Account
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{
		accountModels: make(map[int64][]AccountModel),
		nextID:        1,
	}
}

func (r *memoryRepo) ListAccounts(ctx context.Context, providerName string) ([]Account, error) {
	var accounts []Account
	for _, account := range r.accounts {
		if account.Provider == providerName {
			accounts = append(accounts, account)
		}
	}
	sort.SliceStable(accounts, func(i, j int) bool {
		if accounts[i].Priority != accounts[j].Priority {
			return accounts[i].Priority < accounts[j].Priority
		}
		iHasError := accounts[i].LastErrorAt != nil
		jHasError := accounts[j].LastErrorAt != nil
		if iHasError != jHasError {
			return !iHasError
		}
		if accounts[i].LastUsedAt == nil && accounts[j].LastUsedAt != nil {
			return true
		}
		if accounts[i].LastUsedAt != nil && accounts[j].LastUsedAt == nil {
			return false
		}
		if accounts[i].LastUsedAt != nil && accounts[j].LastUsedAt != nil && !accounts[i].LastUsedAt.Equal(*accounts[j].LastUsedAt) {
			return accounts[i].LastUsedAt.Before(*accounts[j].LastUsedAt)
		}
		return accounts[i].ID < accounts[j].ID
	})
	return accounts, nil
}

func (r *memoryRepo) HasEnabledAccounts(ctx context.Context, providerName string) (bool, error) {
	for _, account := range r.accounts {
		if account.Provider == providerName && account.Enabled {
			return true, nil
		}
	}
	return false, nil
}

func (r *memoryRepo) FindAccount(ctx context.Context, providerName string) (Account, error) {
	accounts, err := r.ListAccounts(ctx, providerName)
	if err != nil {
		return Account{}, err
	}
	if len(accounts) == 0 {
		return Account{}, ErrNotConnected
	}
	return accounts[0], nil
}

func (r *memoryRepo) FindAccountByID(ctx context.Context, providerName string, id int64) (Account, error) {
	for _, account := range r.accounts {
		if account.Provider == providerName && account.ID == id {
			return account, nil
		}
	}
	return Account{}, ErrNotConnected
}

func (r *memoryRepo) FindAccountByIdentity(ctx context.Context, providerName string, identities AccountIdentities) (Account, error) {
	for _, account := range r.accounts {
		if account.Provider != providerName {
			continue
		}
		if identities.ChatGPTAccountID != "" && account.Metadata["chatgpt_account_id"] == identities.ChatGPTAccountID {
			return account, nil
		}
		if identities.ChatGPTUserID != "" && account.Metadata["chatgpt_user_id"] == identities.ChatGPTUserID {
			return account, nil
		}
		if identities.Email != "" && strings.EqualFold(account.Metadata["email"], identities.Email) {
			return account, nil
		}
		if identities.AccessTokenSHA256 != "" && account.Metadata["access_token_sha256"] == identities.AccessTokenSHA256 {
			return account, nil
		}
	}
	return Account{}, ErrNotConnected
}

func (r *memoryRepo) SaveAccount(ctx context.Context, account Account) (Account, error) {
	r.saveCount++
	r.lastSavedAccount = account
	normalizeMemoryAccount(&account)
	now := time.Now()
	for i := range r.accounts {
		if r.accounts[i].Provider == account.Provider && (r.accounts[i].Subject == account.Subject || (account.ID > 0 && r.accounts[i].ID == account.ID)) {
			account.ID = valueOrDefaultInt64(account.ID, r.accounts[i].ID)
			if !account.Enabled {
				account.Enabled = r.accounts[i].Enabled
			}
			if account.Priority == 0 {
				account.Priority = r.accounts[i].Priority
			}
			account.CreatedAt = r.accounts[i].CreatedAt
			account.UpdatedAt = now
			r.accounts[i] = account
			return account, nil
		}
	}
	if account.ID == 0 {
		account.ID = r.nextID
		r.nextID++
	}
	if !account.Enabled {
		account.Enabled = true
	}
	if account.Priority == 0 {
		account.Priority = 100
	}
	account.CreatedAt = now
	account.UpdatedAt = now
	r.accounts = append(r.accounts, account)
	return account, nil
}

func normalizeMemoryAccount(account *Account) {
	if strings.TrimSpace(account.AccountType) == "" {
		account.AccountType = AccountTypeCodexOAuth
	}
	if strings.TrimSpace(account.Credential.CredentialType) == "" {
		switch account.AccountType {
		case AccountTypeAPIUpstream:
			account.Credential.CredentialType = CredentialTypeAPIKey
		default:
			account.Credential.CredentialType = CredentialTypeOAuthToken
		}
	}
	if account.Credential.EncryptedAccessToken == "" {
		account.Credential.EncryptedAccessToken = account.EncryptedAccessToken
	}
	if account.Credential.EncryptedRefreshToken == "" {
		account.Credential.EncryptedRefreshToken = account.EncryptedRefreshToken
	}
	if account.Credential.EncryptedIDToken == "" {
		account.Credential.EncryptedIDToken = account.EncryptedIDToken
	}
	if account.Credential.AccessTokenExpiresAt == nil {
		account.Credential.AccessTokenExpiresAt = account.AccessTokenExpiresAt
	}
	if account.Credential.LastRefreshAt == nil {
		account.Credential.LastRefreshAt = account.LastRefreshAt
	}
	if account.Credential.LastRefreshError == "" {
		account.Credential.LastRefreshError = account.LastRefreshError
	}
	if account.Credential.LastRefreshErrorAt == nil {
		account.Credential.LastRefreshErrorAt = account.LastRefreshErrorAt
	}
	if account.Credential.Metadata == nil {
		account.Credential.Metadata = account.Metadata
	}
	account.EncryptedAccessToken = account.Credential.EncryptedAccessToken
	account.EncryptedRefreshToken = account.Credential.EncryptedRefreshToken
	account.EncryptedIDToken = account.Credential.EncryptedIDToken
	account.AccessTokenExpiresAt = account.Credential.AccessTokenExpiresAt
	account.LastRefreshAt = account.Credential.LastRefreshAt
	account.LastRefreshError = account.Credential.LastRefreshError
	account.LastRefreshErrorAt = account.Credential.LastRefreshErrorAt
	account.Metadata = account.Credential.Metadata
	if account.Metadata == nil {
		account.Metadata = map[string]string{}
	}
	if account.Credential.Metadata == nil {
		account.Credential.Metadata = account.Metadata
	}
}

func (r *memoryRepo) UpdateAccount(ctx context.Context, providerName string, id int64, update AccountUpdate) (Account, error) {
	for i := range r.accounts {
		if r.accounts[i].Provider != providerName || r.accounts[i].ID != id {
			continue
		}
		if update.Enabled != nil {
			r.accounts[i].Enabled = *update.Enabled
		}
		if update.Priority != nil {
			r.accounts[i].Priority = *update.Priority
		}
		if update.Name != nil {
			r.accounts[i].Name = *update.Name
		}
		if update.ClearStatus {
			r.accounts[i].Status = AccountStatusActive
			r.accounts[i].StatusReason = ""
			r.accounts[i].LastError = ""
			r.accounts[i].LastErrorAt = nil
			r.accounts[i].RateLimitedUntil = nil
			r.accounts[i].CircuitOpenUntil = nil
			r.accounts[i].FailureCount = 0
		}
		r.accounts[i].UpdatedAt = time.Now()
		return r.accounts[i], nil
	}
	return Account{}, ErrNotConnected
}

func (r *memoryRepo) DeleteAccount(ctx context.Context, providerName string, id int64) error {
	for i := range r.accounts {
		if r.accounts[i].Provider == providerName && r.accounts[i].ID == id {
			r.accounts = append(r.accounts[:i], r.accounts[i+1:]...)
			return nil
		}
	}
	return ErrNotConnected
}

func (r *memoryRepo) DeleteAccounts(ctx context.Context, providerName string) error {
	kept := r.accounts[:0]
	for _, account := range r.accounts {
		if account.Provider != providerName {
			kept = append(kept, account)
		}
	}
	r.accounts = kept
	return nil
}

func (r *memoryRepo) MarkAccountUsed(ctx context.Context, providerName string, id int64, usedAt time.Time) error {
	if r.markAccountUsedErr != nil {
		return r.markAccountUsedErr
	}
	for i := range r.accounts {
		if r.accounts[i].Provider == providerName && r.accounts[i].ID == id {
			r.accounts[i].LastUsedAt = &usedAt
			r.accounts[i].LastError = ""
			r.accounts[i].LastErrorAt = nil
			return nil
		}
	}
	return ErrNotConnected
}

func (r *memoryRepo) MarkAccountError(ctx context.Context, providerName string, id int64, message string, at time.Time) error {
	if r.markAccountErrorErr != nil {
		return r.markAccountErrorErr
	}
	for i := range r.accounts {
		if r.accounts[i].Provider == providerName && r.accounts[i].ID == id {
			r.accounts[i].LastError = message
			r.accounts[i].LastErrorAt = &at
			return nil
		}
	}
	return ErrNotConnected
}

func (r *memoryRepo) RecordRefreshFailure(ctx context.Context, providerName string, id int64, message string, at time.Time, openUntil *time.Time) error {
	for i := range r.accounts {
		if r.accounts[i].Provider == providerName && r.accounts[i].ID == id {
			r.accounts[i].FailureCount++
			r.accounts[i].LastRefreshError = message
			r.accounts[i].LastRefreshErrorAt = &at
			if openUntil != nil {
				r.accounts[i].CircuitOpenUntil = openUntil
				r.accounts[i].Status = AccountStatusCircuitOpen
				r.accounts[i].StatusReason = message
			}
			return nil
		}
	}
	return ErrNotConnected
}

func (r *memoryRepo) RecordAccountStatus(ctx context.Context, providerName string, id int64, status, reason string, at time.Time, rateLimitedUntil, circuitOpenUntil *time.Time) error {
	for i := range r.accounts {
		if r.accounts[i].Provider == providerName && r.accounts[i].ID == id {
			r.accounts[i].Status = status
			r.accounts[i].StatusReason = reason
			r.accounts[i].LastError = reason
			r.accounts[i].LastErrorAt = &at
			r.accounts[i].RateLimitedUntil = rateLimitedUntil
			r.accounts[i].CircuitOpenUntil = circuitOpenUntil
			if status == AccountStatusCircuitOpen {
				r.accounts[i].FailureCount++
			}
			return nil
		}
	}
	return ErrNotConnected
}

func (r *memoryRepo) ListAccountModels(ctx context.Context, providerName string, accountID int64) ([]AccountModel, error) {
	models := append([]AccountModel(nil), r.accountModels[accountID]...)
	filtered := models[:0]
	for _, model := range models {
		if model.Provider == providerName && model.AccountID == accountID {
			filtered = append(filtered, model)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].Model < filtered[j].Model
	})
	return filtered, nil
}

func (r *memoryRepo) ReplaceAccountModels(ctx context.Context, providerName string, accountID int64, inputs []AccountModelInput) ([]AccountModel, error) {
	if r.replaceModelsErr != nil {
		return nil, r.replaceModelsErr
	}
	if _, err := r.FindAccountByID(ctx, providerName, accountID); err != nil {
		return nil, err
	}
	now := time.Now()
	models := make([]AccountModel, 0, len(inputs))
	for i, input := range inputs {
		models = append(models, AccountModel{
			ID:        int64(i + 1),
			AccountID: accountID,
			Provider:  providerName,
			Model:     input.Model,
			Enabled:   input.Enabled,
			Source:    AccountModelSourceManual,
			Metadata:  map[string]string{},
			CreatedAt: now,
			UpdatedAt: now,
		})
	}
	r.accountModels[accountID] = models
	return r.ListAccountModels(ctx, providerName, accountID)
}

func (r *memoryRepo) ListExposedModels(ctx context.Context, providerName string, allowedModels []string) ([]ExposedModel, error) {
	available := map[string]bool{}
	now := time.Now()
	for _, account := range r.accounts {
		if account.Provider != providerName || !accountSchedulable(account, now) {
			continue
		}
		for _, accountModel := range r.accountModels[account.ID] {
			if accountModel.Provider == providerName && accountModel.Enabled {
				available[accountModel.Model] = true
			}
		}
	}

	seen := map[string]bool{}
	exposed := []ExposedModel{}
	for _, allowed := range allowedModels {
		model := strings.TrimSpace(allowed)
		if model == "" || seen[model] || !available[model] {
			continue
		}
		seen[model] = true
		exposed = append(exposed, ExposedModel{ID: model, OwnedBy: "openai"})
	}
	return exposed, nil
}

func (r *memoryRepo) ListEligibleAccountsForModel(ctx context.Context, providerName string, model string, excludedAccountIDs []int64, now time.Time) ([]Account, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return []Account{}, nil
	}
	excluded := map[int64]bool{}
	for _, id := range excludedAccountIDs {
		if id > 0 {
			excluded[id] = true
		}
	}

	accounts, err := r.ListAccounts(ctx, providerName)
	if err != nil {
		return nil, err
	}
	eligible := []Account{}
	for _, account := range accounts {
		if excluded[account.ID] || !accountSchedulable(account, now) {
			continue
		}
		for _, accountModel := range r.accountModels[account.ID] {
			if accountModel.Provider == providerName && accountModel.Model == model && accountModel.Enabled {
				eligible = append(eligible, account)
				break
			}
		}
	}
	return eligible, nil
}

func (r *memoryRepo) CreateState(ctx context.Context, state OAuthState) error {
	if state.CodeVerifier != "" && state.CodeVerifierHash == "" {
		panic("state with code verifier must also include code verifier hash")
	}
	if state.CodeVerifier != "" && state.EncryptedCodeVerifier == "" {
		panic("state with code verifier must also include encrypted code verifier")
	}
	r.states = append(r.states, state)
	return nil
}

func (r *memoryRepo) ClaimState(ctx context.Context, providerName, stateHash string, now time.Time) (OAuthState, error) {
	for i := range r.states {
		state := &r.states[i]
		if state.Provider != providerName || state.StateHash != stateHash || state.ConsumedAt != nil || !state.ExpiresAt.After(now) {
			continue
		}
		consumedAt := now
		state.ConsumedAt = &consumedAt
		return *state, nil
	}
	return OAuthState{}, ErrInvalidState
}

type fakeOAuthClient struct {
	exchange    TokenResponse
	exchanges   []TokenResponse
	exchangeErr error
	refresh     TokenResponse
	probe       probeResult
	refreshErr  error
}

func (c fakeOAuthClient) ExchangeCode(ctx context.Context, cfg Config, code string) (TokenResponse, error) {
	if c.exchangeErr != nil {
		return TokenResponse{}, c.exchangeErr
	}
	if len(c.exchanges) > 0 {
		return c.exchanges[0], nil
	}
	return c.exchange, nil
}

type captureExchangeOAuthClient struct {
	exchange        TokenResponse
	gotCodeVerifier string
}

func (c *captureExchangeOAuthClient) ExchangeCode(ctx context.Context, cfg Config, code string) (TokenResponse, error) {
	c.gotCodeVerifier = cfg.CodeVerifier
	return c.exchange, nil
}

func (c *captureExchangeOAuthClient) RefreshToken(ctx context.Context, cfg Config, refreshToken string) (TokenResponse, error) {
	return TokenResponse{}, errors.New("unexpected refresh")
}

func (c fakeOAuthClient) RefreshToken(ctx context.Context, cfg Config, refreshToken string) (TokenResponse, error) {
	if c.refreshErr != nil {
		return TokenResponse{}, c.refreshErr
	}
	return c.refresh, nil
}

func (c fakeOAuthClient) ProbeAccountStatus(ctx context.Context, cfg Config, accessToken string) (probeResult, error) {
	if c.probe.statusCode == 0 {
		return probeResult{statusCode: http.StatusOK}, nil
	}
	return c.probe, nil
}

type blockingOAuthClient struct {
	refresh TokenResponse
	entered chan struct{}
	release chan struct{}
	calls   int
}

func (c *blockingOAuthClient) ExchangeCode(ctx context.Context, cfg Config, code string) (TokenResponse, error) {
	return TokenResponse{}, errors.New("unexpected exchange")
}

func (c *blockingOAuthClient) RefreshToken(ctx context.Context, cfg Config, refreshToken string) (TokenResponse, error) {
	c.calls++
	if c.calls == 1 {
		c.entered <- struct{}{}
		<-c.release
	}
	return c.refresh, nil
}

func newConfiguredService(repo Repository, client OAuthClient) *Service {
	return NewService(repo, client, Config{
		Provider:     "openai",
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RedirectURL:  "http://localhost/oauth/openai/callback",
		AuthURL:      "https://auth.example.test/authorize",
		TokenURL:     "https://auth.example.test/token",
		Secret:       "encryption-secret",
	})
}

func mustEncrypt(t *testing.T, encryptionSecret, value string) string {
	t.Helper()
	encrypted, err := secret.EncryptString(encryptionSecret, value)
	if err != nil {
		t.Fatalf("EncryptString returned error: %v", err)
	}
	return encrypted
}

func boolPtr(value bool) *bool {
	return &value
}

func mustQuery(t *testing.T, rawURL, key string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("url.Parse returned error: %v", err)
	}
	return parsed.Query().Get(key)
}

func mustUnsignedIDToken(t *testing.T, claims map[string]any) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": "none", "typ": "JWT"})
	if err != nil {
		t.Fatalf("json.Marshal header returned error: %v", err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("json.Marshal claims returned error: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload) + "."
}

func modelNamesAndEnabled(models []AccountModel) []string {
	values := make([]string, 0, len(models))
	for _, model := range models {
		values = append(values, model.Model+":"+strconv.FormatBool(model.Enabled))
	}
	return values
}

func testAccount(t *testing.T, id int64, enabled bool, priority int, accessToken string) Account {
	t.Helper()
	expiresAt := time.Now().Add(time.Hour)
	return testExpiredAccount(t, id, enabled, priority, accessToken, "refresh-token", expiresAt)
}

func testExpiredAccount(t *testing.T, id int64, enabled bool, priority int, accessToken, refreshToken string, expiresAt time.Time) Account {
	t.Helper()
	now := time.Now()
	return Account{
		ID:                    id,
		Provider:              "openai",
		Subject:               "acct_" + string(rune('0'+id)),
		DisplayName:           "Account",
		EncryptedAccessToken:  mustEncrypt(t, "encryption-secret", accessToken),
		EncryptedRefreshToken: mustEncrypt(t, "encryption-secret", refreshToken),
		AccessTokenExpiresAt:  &expiresAt,
		Enabled:               enabled,
		Priority:              priority,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
}

func valueOrDefaultInt64(value, fallback int64) int64 {
	if value == 0 {
		return fallback
	}
	return value
}
