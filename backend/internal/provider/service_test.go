package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/secret"
	"github.com/KnowSky404/N2API/backend/internal/systemevent"
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
		EncryptedAccessToken:  mustEncrypt(t, "encryption-secret", secret.SecretKindOAuthAccessToken, "access-token"),
		EncryptedRefreshToken: mustEncrypt(t, "encryption-secret", secret.SecretKindOAuthRefreshToken, "refresh-token"),
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

func TestServiceEncryptionKeyringWritesCurrentAndReadsLegacyPreviousKey(t *testing.T) {
	const legacyCiphertext = "AAECAwQFBgcICQoLshPzMSnIGUlIyhB+W347vBUF57bAkCtXBN4l54ODVswuO/ASFnqXSM2t"
	keyring, err := secret.NewKeyring(
		secret.EncryptionKey{ID: "current-202607", Secret: "current-encryption-secret"},
		[]secret.EncryptionKey{{ID: "previous-legacy", Secret: "legacy-encryption-secret"}},
	)
	if err != nil {
		t.Fatalf("NewKeyring returned error: %v", err)
	}
	service := NewService(newMemoryRepo(), fakeOAuthClient{}, Config{EncryptionKeyring: keyring})

	encrypted, err := service.encryptString(secret.SecretKindOAuthAccessToken, "current-provider-token")
	if err != nil {
		t.Fatalf("encryptString returned error: %v", err)
	}
	if !strings.HasPrefix(encrypted, "n2api:v1:current-202607:oauth-access-token:") {
		t.Fatalf("encryptString = %q, want current key envelope", encrypted)
	}
	if _, err := service.decryptString(secret.SecretKindOAuthRefreshToken, encrypted); err == nil {
		t.Fatal("decryptString accepted an access-token envelope as a refresh token")
	}
	decrypted, err := service.decryptString(secret.SecretKindOAuthRefreshToken, legacyCiphertext)
	if err != nil {
		t.Fatalf("decryptString returned error for legacy previous key: %v", err)
	}
	if decrypted != "legacy-oauth-refresh-token" {
		t.Fatalf("decryptString = %q, want legacy-oauth-refresh-token", decrypted)
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
	profileID := int64(9)
	repo.fingerprintProfiles[profileID] = FingerprintProfileData{UserAgent: "Mozilla/5.0"}

	result, err := service.StartConnect(context.Background(), ConnectOptions{
		RedirectAfter:        "/",
		Name:                 "Work Codex",
		Priority:             25,
		Enabled:              boolPtr(false),
		FingerprintProfileID: &profileID,
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
	if state.PendingFingerprintProfileID == nil || *state.PendingFingerprintProfileID != profileID {
		t.Fatalf("state pending fingerprint profile = %+v, want %d", state.PendingFingerprintProfileID, profileID)
	}
	if state.FingerprintHash == "" || state.UserAgentHash == "" || state.IPHash == "" {
		t.Fatalf("state fingerprint hashes incomplete: %+v", state)
	}
	if repo.ensureDefaultFingerprintProfileCalls != 0 {
		t.Fatalf("EnsureDefaultCodexFingerprintProfile called %d times, want 0 (explicit profile provided)", repo.ensureDefaultFingerprintProfileCalls)
	}
	for _, cleartext := range []string{"browser-fingerprint", "Mozilla/5.0", "203.0.113.10"} {
		if strings.Contains(state.FingerprintHash+state.UserAgentHash+state.IPHash, cleartext) {
			t.Fatalf("fingerprint hash leaked cleartext %q", cleartext)
		}
	}
}

func TestStartConnectStoresDefaultFingerprintProfileWhenUnset(t *testing.T) {
	repo := newMemoryRepo()
	service := newConfiguredService(repo, fakeOAuthClient{})

	result, err := service.StartConnect(context.Background(), ConnectOptions{RedirectAfter: "/"})
	if err != nil {
		t.Fatalf("StartConnect returned error: %v", err)
	}
	if mustQuery(t, result.AuthorizationURL, "state") == "" {
		t.Fatal("authorization URL missing state")
	}
	state := repo.states[0]
	if state.PendingFingerprintProfileID == nil {
		t.Fatal("PendingFingerprintProfileID is nil, want default Codex fingerprint profile ID")
	}
	if *state.PendingFingerprintProfileID != repo.defaultFingerprintProfileID {
		t.Fatalf("PendingFingerprintProfileID = %d, want default %d", *state.PendingFingerprintProfileID, repo.defaultFingerprintProfileID)
	}
	if repo.ensureDefaultFingerprintProfileCalls != 1 {
		t.Fatalf("EnsureDefaultCodexFingerprintProfile called %d times, want 1", repo.ensureDefaultFingerprintProfileCalls)
	}
}

func TestStartConnectPreservesExplicitFingerprintProfile(t *testing.T) {
	repo := newMemoryRepo()
	service := newConfiguredService(repo, fakeOAuthClient{})
	customProfileID := int64(42)
	repo.fingerprintProfiles[customProfileID] = FingerprintProfileData{UserAgent: "custom-agent"}

	result, err := service.StartConnect(context.Background(), ConnectOptions{
		RedirectAfter:        "/",
		FingerprintProfileID: &customProfileID,
	})
	if err != nil {
		t.Fatalf("StartConnect returned error: %v", err)
	}
	if mustQuery(t, result.AuthorizationURL, "state") == "" {
		t.Fatal("authorization URL missing state")
	}
	state := repo.states[0]
	if state.PendingFingerprintProfileID == nil || *state.PendingFingerprintProfileID != customProfileID {
		t.Fatalf("PendingFingerprintProfileID = %+v, want %d", state.PendingFingerprintProfileID, customProfileID)
	}
	if repo.ensureDefaultFingerprintProfileCalls != 0 {
		t.Fatalf("EnsureDefaultCodexFingerprintProfile called %d times, want 0", repo.ensureDefaultFingerprintProfileCalls)
	}
}

func TestCompleteCallbackPreservesExistingFingerprintOnReconnect(t *testing.T) {
	repo := newMemoryRepo()
	existingProfileID := int64(7)
	repo.fingerprintProfiles[existingProfileID] = FingerprintProfileData{UserAgent: "Mozilla/5.0"}
	existing, err := repo.SaveAccount(context.Background(), Account{
		Provider:              "openai",
		Subject:               "acct_same",
		Name:                  "Existing",
		DisplayName:           "same@example.com",
		EncryptedAccessToken:  mustEncrypt(t, "encryption-secret", secret.SecretKindOAuthAccessToken, "old-access"),
		EncryptedRefreshToken: mustEncrypt(t, "encryption-secret", secret.SecretKindOAuthRefreshToken, "old-refresh"),
		Enabled:               true,
		Priority:              12,
		FingerprintProfileID:  &existingProfileID,
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

	started, err := service.StartConnect(context.Background(), ConnectOptions{RedirectAfter: "/"})
	if err != nil {
		t.Fatalf("StartConnect returned error: %v", err)
	}
	account, err := service.CompleteCallback(context.Background(), "auth-code", mustQuery(t, started.AuthorizationURL, "state"))
	if err != nil {
		t.Fatalf("CompleteCallback returned error: %v", err)
	}
	if account.ID != existing.ID {
		t.Fatalf("account ID = %d, want existing %d", account.ID, existing.ID)
	}
	if account.FingerprintProfileID == nil || *account.FingerprintProfileID != existingProfileID {
		t.Fatalf("account FingerprintProfileID = %+v, want existing %d", account.FingerprintProfileID, existingProfileID)
	}
}

func TestCompleteCallbackReauthorizationUpdatesFingerprintProfile(t *testing.T) {
	repo := newMemoryRepo()
	existing, err := repo.SaveAccount(context.Background(), Account{
		Provider:              "openai",
		Subject:               "acct_old",
		Name:                  "Old Account",
		DisplayName:           "old@example.com",
		EncryptedAccessToken:  mustEncrypt(t, "encryption-secret", secret.SecretKindOAuthAccessToken, "old-access"),
		EncryptedRefreshToken: mustEncrypt(t, "encryption-secret", secret.SecretKindOAuthRefreshToken, "old-refresh"),
		Enabled:               true,
		Priority:              30,
		Status:                AccountStatusActive,
		Metadata:              map[string]string{"chatgpt_account_id": "acct_old"},
	})
	if err != nil {
		t.Fatalf("SaveAccount returned error: %v", err)
	}
	client := fakeOAuthClient{
		exchange: TokenResponse{
			AccessToken:  "new-access",
			RefreshToken: "new-refresh",
			ExpiresIn:    3600,
			AccountID:    "acct_new",
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
	})
	if err != nil {
		t.Fatalf("StartConnect returned error: %v", err)
	}
	account, err := service.CompleteCallback(context.Background(), "auth-code", mustQuery(t, started.AuthorizationURL, "state"))
	if err != nil {
		t.Fatalf("CompleteCallback returned error: %v", err)
	}
	if account.ID != existing.ID {
		t.Fatalf("account ID = %d, want target %d", account.ID, existing.ID)
	}
	if account.FingerprintProfileID == nil || *account.FingerprintProfileID != repo.defaultFingerprintProfileID {
		t.Fatalf("account FingerprintProfileID = %+v, want default Codex profile %d", account.FingerprintProfileID, repo.defaultFingerprintProfileID)
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
	idToken, err := decryptForTest(t, "encryption-secret", secret.SecretKindOAuthIDToken, account.EncryptedIDToken)
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
		EncryptedAccessToken:  mustEncrypt(t, "encryption-secret", secret.SecretKindOAuthAccessToken, "old-access"),
		EncryptedRefreshToken: mustEncrypt(t, "encryption-secret", secret.SecretKindOAuthRefreshToken, "old-refresh"),
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
	token, err := decryptForTest(t, "encryption-secret", secret.SecretKindOAuthAccessToken, account.EncryptedAccessToken)
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
		EncryptedAccessToken:  mustEncrypt(t, "encryption-secret", secret.SecretKindOAuthAccessToken, "old-access"),
		EncryptedRefreshToken: mustEncrypt(t, "encryption-secret", secret.SecretKindOAuthRefreshToken, "old-refresh"),
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
		EncryptedAccessToken:  mustEncrypt(t, "encryption-secret", secret.SecretKindOAuthAccessToken, "access-token"),
		EncryptedRefreshToken: mustEncrypt(t, "encryption-secret", secret.SecretKindOAuthRefreshToken, "refresh-token"),
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

func TestRefreshAccountAuthorizationRefreshesRejectedUnexpiredToken(t *testing.T) {
	repo := newMemoryRepo()
	expiresAt := time.Now().Add(time.Hour)
	account := testExpiredAccount(t, 7, true, 3, "old-access", "old-refresh", expiresAt)
	repo.accounts = []Account{account}
	client := &captureRefreshOAuthClient{refresh: TokenResponse{
		AccessToken:  "new-access",
		RefreshToken: "new-refresh",
		ExpiresIn:    3600,
	}}
	service := newConfiguredService(repo, client)

	token, retry, failureRecorded, err := service.RefreshAccountAuthorization(
		context.Background(), account.ID, "old-access", http.StatusUnauthorized, "invalid access token",
	)
	if err != nil {
		t.Fatalf("RefreshAccountAuthorization returned error: %v", err)
	}
	if !retry || token != "new-access" {
		t.Fatalf("refresh result = token %q retry %v, want new-access/true", token, retry)
	}
	if failureRecorded {
		t.Fatal("successful refresh reported an account failure")
	}
	if client.calls != 1 {
		t.Fatalf("refresh calls = %d, want 1", client.calls)
	}
	if repo.accounts[0].LastRefreshAt == nil {
		t.Fatal("LastRefreshAt was not updated")
	}
}

func TestRefreshAccountAuthorizationReusesConcurrentlyRotatedToken(t *testing.T) {
	repo := newMemoryRepo()
	expiresAt := time.Now().Add(time.Hour)
	account := testExpiredAccount(t, 7, true, 3, "new-access", "new-refresh", expiresAt)
	repo.accounts = []Account{account}
	client := &captureRefreshOAuthClient{refresh: TokenResponse{AccessToken: "unexpected-refresh", ExpiresIn: 3600}}
	service := newConfiguredService(repo, client)

	token, retry, failureRecorded, err := service.RefreshAccountAuthorization(
		context.Background(), account.ID, "old-access", http.StatusUnauthorized, "invalid access token",
	)
	if err != nil {
		t.Fatalf("RefreshAccountAuthorization returned error: %v", err)
	}
	if !retry || token != "new-access" {
		t.Fatalf("refresh result = token %q retry %v, want new-access/true", token, retry)
	}
	if failureRecorded {
		t.Fatal("concurrently rotated token reported an account failure")
	}
	if client.calls != 0 {
		t.Fatalf("refresh calls = %d, want 0 for already rotated token", client.calls)
	}
}

func TestRefreshAccountAuthorizationSerializesConcurrentRejectedTokenRefresh(t *testing.T) {
	repo := newMemoryRepo()
	expiresAt := time.Now().Add(time.Hour)
	account := testExpiredAccount(t, 7, true, 3, "old-access", "old-refresh", expiresAt)
	repo.accounts = []Account{account}
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

	type result struct {
		token           string
		retry           bool
		failureRecorded bool
		err             error
	}
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() {
			token, retry, failureRecorded, err := service.RefreshAccountAuthorization(
				context.Background(), account.ID, "old-access", http.StatusUnauthorized, "invalid access token",
			)
			results <- result{token: token, retry: retry, failureRecorded: failureRecorded, err: err}
		}()
	}

	<-client.entered
	client.release <- struct{}{}
	for i := 0; i < 2; i++ {
		got := <-results
		if got.err != nil || !got.retry || got.failureRecorded || got.token != "new-access" {
			t.Fatalf("refresh result = token %q retry %v failureRecorded %v error %v", got.token, got.retry, got.failureRecorded, got.err)
		}
	}
	if client.calls != 1 {
		t.Fatalf("refresh calls = %d, want 1", client.calls)
	}
}

func TestRefreshAccountAuthorizationSkipsScopePermissionFailure(t *testing.T) {
	repo := newMemoryRepo()
	expiresAt := time.Now().Add(time.Hour)
	account := testExpiredAccount(t, 7, true, 3, "access-token", "refresh-token", expiresAt)
	repo.accounts = []Account{account}
	client := &captureRefreshOAuthClient{refresh: TokenResponse{AccessToken: "unexpected-refresh", ExpiresIn: 3600}}
	service := newConfiguredService(repo, client)

	token, retry, failureRecorded, err := service.RefreshAccountAuthorization(
		context.Background(), account.ID, "access-token", http.StatusForbidden, "missing scopes: api.responses.write",
	)
	if err != nil {
		t.Fatalf("RefreshAccountAuthorization returned error: %v", err)
	}
	if retry || token != "" {
		t.Fatalf("refresh result = token %q retry %v, want empty/false", token, retry)
	}
	if failureRecorded {
		t.Fatal("scope permission failure reported an OAuth refresh failure")
	}
	if client.calls != 0 {
		t.Fatalf("refresh calls = %d, want 0 for scope permission failure", client.calls)
	}
}

func TestRefreshAccountAuthorizationReportsPersistedRefreshFailure(t *testing.T) {
	repo := newMemoryRepo()
	expiresAt := time.Now().Add(time.Hour)
	account := testExpiredAccount(t, 7, true, 3, "access-token", "refresh-token", expiresAt)
	repo.accounts = []Account{account}
	service := newConfiguredService(repo, fakeOAuthClient{refreshErr: errors.New("refresh rejected")})

	token, retry, failureRecorded, err := service.RefreshAccountAuthorization(
		context.Background(), account.ID, "access-token", http.StatusUnauthorized, "invalid access token",
	)
	if err == nil {
		t.Fatal("RefreshAccountAuthorization returned nil error")
	}
	if token != "" || !retry || !failureRecorded {
		t.Fatalf("refresh result = token %q retry %v failureRecorded %v, want empty/true/true", token, retry, failureRecorded)
	}
	if repo.accounts[0].FailureCount != 1 {
		t.Fatalf("failure count = %d, want 1", repo.accounts[0].FailureCount)
	}
}

func TestRefreshAccountAuthorizationReportsUnpersistedCredentialError(t *testing.T) {
	repo := newMemoryRepo()
	expiresAt := time.Now().Add(time.Hour)
	account := testExpiredAccount(t, 7, true, 3, "access-token", "refresh-token", expiresAt)
	account.EncryptedRefreshToken = "invalid-ciphertext"
	repo.accounts = []Account{account}
	service := newConfiguredService(repo, fakeOAuthClient{})

	token, retry, failureRecorded, err := service.RefreshAccountAuthorization(
		context.Background(), account.ID, "access-token", http.StatusUnauthorized, "invalid access token",
	)
	if err == nil {
		t.Fatal("RefreshAccountAuthorization returned nil error")
	}
	if token != "" || !retry || failureRecorded {
		t.Fatalf("refresh result = token %q retry %v failureRecorded %v, want empty/true/false", token, retry, failureRecorded)
	}
	if repo.accounts[0].FailureCount != 0 {
		t.Fatalf("failure count = %d, want no provider refresh failure record", repo.accounts[0].FailureCount)
	}
}

func TestAccessTokenRefreshesExpiredToken(t *testing.T) {
	repo := newMemoryRepo()
	expired := time.Now().Add(-time.Minute)
	if _, err := repo.SaveAccount(context.Background(), Account{
		Provider:              "openai",
		Subject:               "acct_1",
		EncryptedAccessToken:  mustEncrypt(t, "encryption-secret", secret.SecretKindOAuthAccessToken, "old-access"),
		EncryptedRefreshToken: mustEncrypt(t, "encryption-secret", secret.SecretKindOAuthRefreshToken, "refresh-token"),
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
	refreshToken, err := decryptForTest(t, "encryption-secret", secret.SecretKindOAuthRefreshToken, account.EncryptedRefreshToken)
	if err != nil {
		t.Fatalf("DecryptString returned error: %v", err)
	}
	if refreshToken != "old-refresh" {
		t.Fatalf("refreshToken = %q, want old-refresh", refreshToken)
	}
}

func TestAccessTokenForAccountRefreshPassesConfiguredProxyURL(t *testing.T) {
	repo := newMemoryRepo()
	expired := time.Now().Add(-time.Minute)
	account := testExpiredAccount(t, 7, true, 3, "old-access", "old-refresh", expired)
	account.Credential.EncryptedProxyURL = mustEncrypt(t, "encryption-secret", secret.SecretKindProviderProxyURL, "http://proxy.example.test:8080")
	repo.accounts = []Account{account}
	client := &captureRefreshOAuthClient{refresh: TokenResponse{AccessToken: "new-access", ExpiresIn: 3600, Subject: "acct_7"}}
	service := newConfiguredService(repo, client)

	if _, err := service.AccessTokenForAccount(context.Background(), repo.accounts[0]); err != nil {
		t.Fatalf("AccessTokenForAccount returned error: %v", err)
	}
	if client.gotConfig.ProxyURL != "http://proxy.example.test:8080" {
		t.Fatalf("refresh proxy URL = %q, want account proxy URL", client.gotConfig.ProxyURL)
	}
}

func TestAccessTokenForAccountRefreshRejectsUnreadableProxyWithoutBypass(t *testing.T) {
	repo := newMemoryRepo()
	expired := time.Now().Add(-time.Minute)
	account := testExpiredAccount(t, 7, true, 3, "old-access", "old-refresh", expired)
	account.Credential.EncryptedProxyURL = "n2api:v1:missing:provider-proxy-url:AAAA"
	repo.accounts = []Account{account}
	client := &captureRefreshOAuthClient{refresh: TokenResponse{AccessToken: "new-access", ExpiresIn: 3600}}
	service := newConfiguredService(repo, client)

	if _, err := service.AccessTokenForAccount(context.Background(), account); err == nil {
		t.Fatal("AccessTokenForAccount returned nil error for unreadable proxy")
	}
	if client.calls != 0 {
		t.Fatalf("refresh calls = %d, want 0", client.calls)
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
	account := repo.accounts[0]

	errs := make(chan error, 2)
	tokens := make(chan string, 2)
	for i := 0; i < 2; i++ {
		go func() {
			token, err := service.AccessTokenForAccount(context.Background(), account)
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
			EncryptedAPIKey: mustEncrypt(t, "encryption-secret", secret.SecretKindProviderAPIKey, "sk-upstream"),
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
	if repo.accounts[0].LastUsedAt != nil {
		t.Fatal("API upstream account selection marked used before gateway acquired the account")
	}
}

func TestSelectAccountForModelSkipsAPIUpstreamWithInvalidBaseURL(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{
		{
			ID:          11,
			Provider:    "openai",
			AccountType: AccountTypeAPIUpstream,
			Name:        "Broken upstream",
			Credential: AccountCredential{
				CredentialType:  CredentialTypeAPIKey,
				EncryptedAPIKey: mustEncrypt(t, "encryption-secret", secret.SecretKindProviderAPIKey, "sk-broken"),
				BaseURL:         "://broken",
			},
			Enabled:  true,
			Priority: 1,
			Status:   AccountStatusActive,
			Metadata: map[string]string{},
		},
		{
			ID:          12,
			Provider:    "openai",
			AccountType: AccountTypeAPIUpstream,
			Name:        "Fallback upstream",
			Credential: AccountCredential{
				CredentialType:  CredentialTypeAPIKey,
				EncryptedAPIKey: mustEncrypt(t, "encryption-secret", secret.SecretKindProviderAPIKey, "sk-fallback"),
				BaseURL:         "https://fallback.example.test/v1",
			},
			Enabled:  true,
			Priority: 2,
			Status:   AccountStatusActive,
			Metadata: map[string]string{},
		},
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	selected, err := service.SelectAccountForModel(context.Background(), "")
	if err != nil {
		t.Fatalf("SelectAccountForModel returned error: %v", err)
	}
	if selected.AccountID != 12 || selected.AuthorizationToken != "sk-fallback" {
		t.Fatalf("selected = %+v, want fallback upstream 12", selected)
	}
	if repo.accounts[0].LastError == "" {
		t.Fatal("invalid API upstream account was not marked with an error")
	}
	if repo.accounts[0].Status != AccountStatusCircuitOpen || repo.accounts[0].CircuitOpenUntil == nil {
		t.Fatalf("invalid API upstream account status = %+v, want circuit_open with an open window", repo.accounts[0])
	}
	if repo.accounts[0].LastUsedAt != nil {
		t.Fatal("invalid API upstream account was marked used")
	}
}

func TestSelectAccountForModelSkipsAPIUpstreamWithHTTPBaseURLUnlessAllowed(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{
		{
			ID:          11,
			Provider:    "openai",
			AccountType: AccountTypeAPIUpstream,
			Name:        "Plaintext upstream",
			Credential: AccountCredential{
				CredentialType:  CredentialTypeAPIKey,
				EncryptedAPIKey: mustEncrypt(t, "encryption-secret", secret.SecretKindProviderAPIKey, "sk-http"),
				BaseURL:         "http://upstream.example.test/v1",
			},
			Enabled:  true,
			Priority: 1,
			Status:   AccountStatusActive,
			Metadata: map[string]string{},
		},
		{
			ID:          12,
			Provider:    "openai",
			AccountType: AccountTypeAPIUpstream,
			Name:        "HTTPS upstream",
			Credential: AccountCredential{
				CredentialType:  CredentialTypeAPIKey,
				EncryptedAPIKey: mustEncrypt(t, "encryption-secret", secret.SecretKindProviderAPIKey, "sk-https"),
				BaseURL:         "https://upstream.example.test/v1",
			},
			Enabled:  true,
			Priority: 2,
			Status:   AccountStatusActive,
			Metadata: map[string]string{},
		},
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	selected, err := service.SelectAccountForModel(context.Background(), "")
	if err != nil {
		t.Fatalf("SelectAccountForModel returned error: %v", err)
	}
	if selected.AccountID != 12 || selected.AuthorizationToken != "sk-https" {
		t.Fatalf("selected = %+v, want HTTPS fallback upstream 12", selected)
	}
	if repo.accounts[0].Status != AccountStatusCircuitOpen || repo.accounts[0].LastError == "" {
		t.Fatalf("HTTP upstream status = %+v, want circuit_open with error", repo.accounts[0])
	}

	repo.accounts[0].Status = AccountStatusActive
	repo.accounts[0].LastError = ""
	repo.accounts[0].LastErrorAt = nil
	repo.accounts[0].CircuitOpenUntil = nil
	allowed := NewService(repo, fakeOAuthClient{}, Config{
		Provider:              "openai",
		ClientID:              "client-id",
		ClientSecret:          "client-secret",
		RedirectURL:           "http://localhost/oauth/openai/callback",
		AuthURL:               "https://auth.example.test/authorize",
		TokenURL:              "https://auth.example.test/token",
		Secret:                "encryption-secret",
		AllowHTTPAPIUpstreams: true,
	})
	selected, err = allowed.SelectAccountForModel(context.Background(), "")
	if err != nil {
		t.Fatalf("SelectAccountForModel with HTTP allowed returned error: %v", err)
	}
	if selected.AccountID != 11 || selected.BaseURL != "http://upstream.example.test/v1" {
		t.Fatalf("selected = %+v, want HTTP upstream 11 when explicitly allowed", selected)
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
	repo.accounts[0].EncryptedRefreshToken = mustEncrypt(t, "encryption-secret", secret.SecretKindOAuthRefreshToken, "refresh-token")
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
	token, err := decryptForTest(t, "encryption-secret", secret.SecretKindOAuthAccessToken, account.EncryptedAccessToken)
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

func TestRefreshAccountRejectsAPIUpstreamAccount(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 7, true, 3, "upstream-key"),
	}
	repo.accounts[0].AccountType = AccountTypeAPIUpstream
	repo.accounts[0].Credential.CredentialType = CredentialTypeAPIKey
	repo.accounts[0].EncryptedRefreshToken = ""
	repo.accounts[0].Credential.EncryptedRefreshToken = ""
	service := newConfiguredService(repo, fakeOAuthClient{})

	if _, err := service.RefreshAccount(context.Background(), 7); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("RefreshAccount error = %v, want ErrInvalidInput", err)
	}
}

func TestRefreshAccountProbesLatestStatusAfterTokenRefresh(t *testing.T) {
	repo := newMemoryRepo()
	expiresAt := time.Now().Add(time.Hour)
	repo.accounts = []Account{
		testAccount(t, 7, true, 3, "old-access"),
	}
	repo.accounts[0].EncryptedRefreshToken = mustEncrypt(t, "encryption-secret", secret.SecretKindOAuthRefreshToken, "refresh-token")
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
	if account.LastTestStatus != AccountTestStatusFailed || account.LastTestError != "usage limit reached" {
		t.Fatalf("test status/error = %q/%q, want failed probe result", account.LastTestStatus, account.LastTestError)
	}
}

func TestRefreshAccountPreservesHealthWhenProbeCannotConfirmRecovery(t *testing.T) {
	repo := newMemoryRepo()
	expiresAt := time.Now().Add(time.Hour)
	account := testAccount(t, 7, true, 3, "old-access")
	account.EncryptedRefreshToken = mustEncrypt(t, "encryption-secret", secret.SecretKindOAuthRefreshToken, "refresh-token")
	account.AccessTokenExpiresAt = &expiresAt
	account.Status = AccountStatusCircuitOpen
	account.StatusReason = "existing circuit"
	account.LastError = "existing circuit"
	account.FailureCount = 3
	until := time.Now().Add(time.Hour)
	account.CircuitOpenUntil = &until
	repo.accounts = []Account{account}
	service := newConfiguredService(repo, fakeOAuthClient{
		refresh:  TokenResponse{AccessToken: "new-access", RefreshToken: "new-refresh", ExpiresIn: 3600},
		probeErr: errors.New("probe network unavailable"),
	})

	refreshed, err := service.RefreshAccount(context.Background(), account.ID)
	if err != nil {
		t.Fatalf("RefreshAccount returned error: %v", err)
	}
	if refreshed.Status != AccountStatusCircuitOpen || refreshed.StatusReason != "existing circuit" || refreshed.LastError != "existing circuit" ||
		refreshed.FailureCount != 3 || refreshed.CircuitOpenUntil == nil {
		t.Fatalf("account health = %+v, want preserved until confirmed probe recovery", refreshed)
	}
	if refreshed.LastTestStatus != AccountTestStatusFailed || refreshed.LastTestError != "probe network unavailable" {
		t.Fatalf("test status/error = %q/%q, want failed network probe result", refreshed.LastTestStatus, refreshed.LastTestError)
	}
	token, err := decryptForTest(t, "encryption-secret", secret.SecretKindOAuthAccessToken, refreshed.EncryptedAccessToken)
	if err != nil || token != "new-access" {
		t.Fatalf("refreshed token = %q, err=%v", token, err)
	}
}

func TestTestAccountProbesAPIUpstreamAndClearsFailureState(t *testing.T) {
	repo := newMemoryRepo()
	account := testAccount(t, 7, true, 3, "unused-oauth-token")
	account.AccountType = AccountTypeAPIUpstream
	account.Credential.CredentialType = CredentialTypeAPIKey
	account.Credential.EncryptedAPIKey = mustEncrypt(t, "encryption-secret", secret.SecretKindProviderAPIKey, "upstream-secret")
	account.Credential.BaseURL = "https://upstream.example.test"
	account.Status = AccountStatusCircuitOpen
	account.StatusReason = "previous failure"
	account.LastError = "previous failure"
	now := time.Now()
	circuitOpenUntil := now.Add(time.Minute)
	account.LastErrorAt = &now
	account.CircuitOpenUntil = &circuitOpenUntil
	account.FailureCount = 2
	repo.accounts = []Account{account}
	client := &captureProbeOAuthClient{probe: probeResult{statusCode: http.StatusOK}}
	service := newConfiguredService(repo, client)
	requestLogger := &captureAccountTestRequestLogger{err: errors.New("request log unavailable")}
	service.accountTestRequestLogger = requestLogger

	tested, err := service.TestAccount(context.Background(), 7)
	if err != nil {
		t.Fatalf("TestAccount returned error: %v", err)
	}

	if client.gotAccessToken != "upstream-secret" || client.gotConfig.APIBaseURL != "https://upstream.example.test" || client.gotConfig.ProbeChatGPTAccountID != "" {
		t.Fatalf("probe call token=%q apiBase=%q chatgpt=%q", client.gotAccessToken, client.gotConfig.APIBaseURL, client.gotConfig.ProbeChatGPTAccountID)
	}
	if tested.Status != AccountStatusActive || tested.StatusReason != "" || tested.LastError != "" || tested.LastErrorAt != nil || tested.CircuitOpenUntil != nil || tested.FailureCount != 0 {
		t.Fatalf("tested account = %+v, want local failure state cleared", tested)
	}
	if tested.LastTestAt == nil || tested.LastTestStatus != AccountTestStatusPassed || tested.LastTestError != "" {
		t.Fatalf("test result = at:%v status:%q error:%q, want passed result", tested.LastTestAt, tested.LastTestStatus, tested.LastTestError)
	}
	if len(requestLogger.entries) != 1 {
		t.Fatalf("request log count = %d, want 1", len(requestLogger.entries))
	}
	entry := requestLogger.entries[0]
	if entry.RequestID == "" || entry.Provider != "openai" || entry.ProviderAccountID != account.ID || entry.ProviderAccountType != AccountTypeAPIUpstream || entry.ProviderAccountName != account.DisplayName {
		t.Fatalf("request log attribution = %+v", entry)
	}
	if entry.Route != "/v1/models" || entry.Method != http.MethodGet || entry.StatusCode != http.StatusOK || entry.Error != "" || entry.Latency < 0 || entry.CreatedAt.IsZero() {
		t.Fatalf("request log probe fields = %+v", entry)
	}
}

func TestTestAccountOAuthRecoveryClearsRateLimitState(t *testing.T) {
	repo := newMemoryRepo()
	account := testAccount(t, 7, true, 3, "oauth-access-token")
	account.AccountType = AccountTypeCodexOAuth
	account.Metadata = map[string]string{"chatgpt_account_id": "acct_chatgpt"}
	account.Status = AccountStatusRateLimited
	account.StatusReason = "previous rate limit"
	account.LastError = "previous rate limit"
	now := time.Now()
	rateLimitedUntil := now.Add(-time.Minute)
	account.LastErrorAt = &now
	account.RateLimitedUntil = &rateLimitedUntil
	repo.accounts = []Account{account}
	client := &captureProbeOAuthClient{probe: probeResult{statusCode: http.StatusOK}}
	service := newConfiguredService(repo, client)

	tested, err := service.TestAccount(context.Background(), account.ID)
	if err != nil {
		t.Fatalf("TestAccount returned error: %v", err)
	}

	if client.gotConfig.ProbeChatGPTAccountID != "acct_chatgpt" || client.gotAccessToken != "oauth-access-token" {
		t.Fatalf("probe account/token = %q/%q, want acct_chatgpt/oauth-access-token", client.gotConfig.ProbeChatGPTAccountID, client.gotAccessToken)
	}
	if tested.Status != AccountStatusActive || tested.StatusReason != "" || tested.LastError != "" || tested.LastErrorAt != nil || tested.RateLimitedUntil != nil {
		t.Fatalf("tested account = %+v, want recovered active account", tested)
	}
	if tested.LastTestAt == nil || tested.LastTestStatus != AccountTestStatusPassed || tested.LastTestError != "" {
		t.Fatalf("test result = at:%v status:%q error:%q, want passed result", tested.LastTestAt, tested.LastTestStatus, tested.LastTestError)
	}
}

func TestTestAccountLogsModelOnlyForCodexResponsesProbe(t *testing.T) {
	testCases := []struct {
		name               string
		chatGPTAccountID   string
		wantModel          string
		wantRoute          string
		wantMethod         string
		wantProbeAccountID string
	}{
		{
			name:       "missing ChatGPT account ID",
			wantRoute:  "/v1/models",
			wantMethod: http.MethodGet,
		},
		{
			name:               "with ChatGPT account ID",
			chatGPTAccountID:   "acct_chatgpt",
			wantModel:          "gpt-5.4-mini",
			wantRoute:          "/backend-api/codex/responses",
			wantMethod:         http.MethodPost,
			wantProbeAccountID: "acct_chatgpt",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			repo := newMemoryRepo()
			account := testAccount(t, 7, true, 3, "oauth-access-token")
			account.AccountType = AccountTypeCodexOAuth
			if testCase.chatGPTAccountID != "" {
				account.Metadata = map[string]string{"chatgpt_account_id": testCase.chatGPTAccountID}
			}
			repo.accounts = []Account{account}
			client := &captureProbeOAuthClient{probe: probeResult{statusCode: http.StatusOK}}
			service := newConfiguredService(repo, client)
			requestLogger := &captureAccountTestRequestLogger{}
			service.accountTestRequestLogger = requestLogger

			if _, err := service.TestAccount(context.Background(), account.ID); err != nil {
				t.Fatalf("TestAccount returned error: %v", err)
			}
			if client.gotConfig.ProbeChatGPTAccountID != testCase.wantProbeAccountID {
				t.Fatalf("probe ChatGPT account ID = %q, want %q", client.gotConfig.ProbeChatGPTAccountID, testCase.wantProbeAccountID)
			}
			if len(requestLogger.entries) != 1 {
				t.Fatalf("request log count = %d, want 1", len(requestLogger.entries))
			}
			entry := requestLogger.entries[0]
			if entry.Model != testCase.wantModel || entry.Route != testCase.wantRoute || entry.Method != testCase.wantMethod {
				t.Fatalf("request log model/route/method = %q/%q/%q, want %q/%q/%q", entry.Model, entry.Route, entry.Method, testCase.wantModel, testCase.wantRoute, testCase.wantMethod)
			}
		})
	}
}

func TestTestAccountRecordsAPIUpstreamFailure(t *testing.T) {
	repo := newMemoryRepo()
	account := testAccount(t, 7, true, 3, "unused-oauth-token")
	account.AccountType = AccountTypeAPIUpstream
	account.Credential.CredentialType = CredentialTypeAPIKey
	account.Credential.EncryptedAPIKey = mustEncrypt(t, "encryption-secret", secret.SecretKindProviderAPIKey, "upstream-secret")
	account.Credential.BaseURL = "https://upstream.example.test"
	repo.accounts = []Account{account}
	client := &captureProbeOAuthClient{probe: probeResult{statusCode: http.StatusTooManyRequests, retryAfter: "120", message: "quota window"}}
	service := newConfiguredService(repo, client)
	requestLogger := &captureAccountTestRequestLogger{}
	service.accountTestRequestLogger = requestLogger

	tested, err := service.TestAccount(context.Background(), 7)
	if err != nil {
		t.Fatalf("TestAccount returned error: %v", err)
	}

	if tested.Status != AccountStatusRateLimited || tested.StatusReason != "quota window" || tested.LastError != "quota window" {
		t.Fatalf("tested account = %+v, want rate limited failure state", tested)
	}
	if tested.LastTestAt == nil || tested.LastTestStatus != AccountTestStatusFailed || tested.LastTestError != "quota window" {
		t.Fatalf("test result = at:%v status:%q error:%q, want failed result", tested.LastTestAt, tested.LastTestStatus, tested.LastTestError)
	}
	if tested.RateLimitedUntil == nil || !tested.RateLimitedUntil.After(time.Now().Add(100*time.Second)) {
		t.Fatalf("RateLimitedUntil = %v, want retry-after window", tested.RateLimitedUntil)
	}
	if len(requestLogger.entries) != 1 || requestLogger.entries[0].StatusCode != http.StatusTooManyRequests || requestLogger.entries[0].Error != "rate_limited" {
		t.Fatalf("failed request log = %+v, want HTTP 429 rate_limited", requestLogger.entries)
	}
}

func TestTestAccountRequiresTwoHundredStatusForRecovery(t *testing.T) {
	for _, statusCode := range []int{http.StatusFound, http.StatusBadRequest} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			repo := newMemoryRepo()
			account := testAccount(t, 7, true, 3, "access-token")
			account.Status = AccountStatusCircuitOpen
			account.StatusReason = "existing circuit"
			account.LastError = "existing circuit"
			account.FailureCount = 2
			until := time.Now().Add(time.Hour)
			account.CircuitOpenUntil = &until
			repo.accounts = []Account{account}
			client := &captureProbeOAuthClient{probe: probeResult{statusCode: statusCode, message: "non-success probe"}}
			service := newConfiguredService(repo, client)

			tested, err := service.TestAccount(context.Background(), account.ID)
			if err != nil {
				t.Fatalf("TestAccount returned error: %v", err)
			}
			if tested.Status != AccountStatusCircuitOpen || tested.LastTestStatus != AccountTestStatusFailed || tested.LastTestError != "non-success probe" {
				t.Fatalf("tested account = %+v, want failed probe without recovery", tested)
			}
			for _, intent := range repo.intents {
				if intent.Action == systemevent.ActionProviderAccountRecovered {
					t.Fatalf("unexpected recovery intent for status %d: %+v", statusCode, intent)
				}
			}
		})
	}
}

func TestTestAccountsProbesEveryProviderAccount(t *testing.T) {
	repo := newMemoryRepo()
	first := testAccount(t, 7, true, 3, "unused-oauth-token")
	first.AccountType = AccountTypeAPIUpstream
	first.Credential.CredentialType = CredentialTypeAPIKey
	first.Credential.EncryptedAPIKey = mustEncrypt(t, "encryption-secret", secret.SecretKindProviderAPIKey, "first-secret")
	first.Credential.BaseURL = "https://first.example.test"
	second := testAccount(t, 8, true, 4, "unused-oauth-token")
	second.AccountType = AccountTypeAPIUpstream
	second.Credential.CredentialType = CredentialTypeAPIKey
	second.Credential.EncryptedAPIKey = mustEncrypt(t, "encryption-secret", secret.SecretKindProviderAPIKey, "second-secret")
	second.Credential.BaseURL = "https://second.example.test"
	second.Status = AccountStatusCircuitOpen
	second.StatusReason = "previous failure"
	second.LastError = "previous failure"
	now := time.Now()
	until := now.Add(time.Minute)
	second.LastErrorAt = &now
	second.CircuitOpenUntil = &until
	repo.accounts = []Account{first, second}
	client := &captureProbeOAuthClient{probes: []probeResult{{statusCode: http.StatusOK}, {statusCode: http.StatusOK}}}
	service := newConfiguredService(repo, client)

	tested, err := service.TestAccounts(context.Background())
	if err != nil {
		t.Fatalf("TestAccounts returned error: %v", err)
	}

	if len(tested) != 2 {
		t.Fatalf("tested account count = %d, want 2", len(tested))
	}
	if strings.Join(client.gotAccessTokens, ",") != "first-secret,second-secret" {
		t.Fatalf("probe tokens = %v, want both API upstream secrets", client.gotAccessTokens)
	}
	if tested[1].ID != 8 || tested[1].Status != AccountStatusActive || tested[1].CircuitOpenUntil != nil || tested[1].LastError != "" {
		t.Fatalf("second tested account = %+v, want cleared active account", tested[1])
	}
}

func TestTestAccountPassesConfiguredProxyURLToProbe(t *testing.T) {
	repo := newMemoryRepo()
	account := testAccount(t, 7, true, 3, "unused-oauth-token")
	account.AccountType = AccountTypeAPIUpstream
	account.Credential.CredentialType = CredentialTypeAPIKey
	account.Credential.EncryptedAPIKey = mustEncrypt(t, "encryption-secret", secret.SecretKindProviderAPIKey, "api-secret")
	account.Credential.EncryptedProxyURL = mustEncrypt(t, "encryption-secret", secret.SecretKindProviderProxyURL, "http://proxy.example.test:8080")
	account.Credential.BaseURL = "https://upstream.example.test"
	repo.accounts = []Account{account}
	client := &captureProbeOAuthClient{probe: probeResult{statusCode: http.StatusOK}}
	service := newConfiguredService(repo, client)

	if _, err := service.TestAccount(context.Background(), account.ID); err != nil {
		t.Fatalf("TestAccount returned error: %v", err)
	}
	if client.gotConfig.ProxyURL != "http://proxy.example.test:8080" {
		t.Fatalf("probe proxy URL = %q, want account proxy URL", client.gotConfig.ProxyURL)
	}
}

func TestTestAccountModelUsesExactAPIUpstreamAndPersistsFailureWithoutChangingHealth(t *testing.T) {
	repo := newMemoryRepo()
	account := testAccount(t, 7, true, 3, "unused-oauth-token")
	account.AccountType = AccountTypeAPIUpstream
	account.Credential.CredentialType = CredentialTypeAPIKey
	account.Credential.EncryptedAPIKey = mustEncrypt(t, "encryption-secret", secret.SecretKindProviderAPIKey, "upstream-secret")
	account.Credential.EncryptedProxyURL = mustEncrypt(t, "encryption-secret", secret.SecretKindProviderProxyURL, "http://proxy.example.test:8080")
	account.Credential.BaseURL = "https://upstream.example.test/v1"
	account.Status = AccountStatusCircuitOpen
	account.StatusReason = "existing health state"
	account.LastError = "existing health state"
	until := time.Now().Add(time.Hour)
	account.CircuitOpenUntil = &until
	account.FailureCount = 4
	profileID := int64(9)
	account.FingerprintProfileID = &profileID
	repo.accounts = []Account{account}
	repo.fingerprintProfiles[profileID] = FingerprintProfileData{
		UserAgent:      "model-probe-agent",
		TLSFingerprint: "chrome",
		Headers:        map[string]string{"X-Probe": "profile"},
	}
	_, err := repo.ReplaceAccountModels(context.Background(), "openai", account.ID, []AccountModelInput{{Model: "gpt-test", Enabled: false}})
	if err != nil {
		t.Fatalf("ReplaceAccountModels returned error: %v", err)
	}
	prober := &captureAccountModelProber{result: modelProbeResult{
		statusCode: http.StatusTooManyRequests,
		errorCode:  "rate_limited",
		message:    " quota upstream-secret  window\nreached ",
	}}
	service := newConfiguredService(repo, fakeOAuthClient{})
	service.modelProber = prober
	requestLogger := &captureAccountTestRequestLogger{}
	service.accountTestRequestLogger = requestLogger

	result, err := service.TestAccountModel(context.Background(), account.ID, " gpt-test ")
	if err != nil {
		t.Fatalf("TestAccountModel returned error: %v", err)
	}
	if prober.model != "gpt-test" || prober.selected.AccountID != account.ID || prober.selected.AuthorizationToken != "upstream-secret" || prober.selected.BaseURL != "https://upstream.example.test/v1" || prober.selected.ProxyURL != "http://proxy.example.test:8080" {
		t.Fatalf("model probe selected account = %+v model=%q", prober.selected, prober.model)
	}
	if prober.selected.FingerprintUA != "model-probe-agent" || prober.selected.FingerprintTLS != "chrome" || prober.selected.FingerprintHeaders["X-Probe"] != "profile" {
		t.Fatalf("model probe fingerprint = ua:%q tls:%q headers:%v", prober.selected.FingerprintUA, prober.selected.FingerprintTLS, prober.selected.FingerprintHeaders)
	}
	if result.Status != AccountTestStatusFailed || result.ErrorCode != "rate_limited" || result.HTTPStatus != http.StatusTooManyRequests || result.Message != "quota [redacted] window reached" || result.CheckedAt.IsZero() {
		t.Fatalf("result = %+v", result)
	}
	models, err := repo.ListAccountModels(context.Background(), "openai", account.ID)
	if err != nil {
		t.Fatalf("ListAccountModels returned error: %v", err)
	}
	if len(models) != 1 || models[0].LastTestStatus != AccountTestStatusFailed || models[0].LastTestHTTPStatus != http.StatusTooManyRequests || models[0].LastError != "quota [redacted] window reached" || models[0].LastTestAt == nil {
		t.Fatalf("persisted model result = %+v", models)
	}
	unchanged, err := repo.FindAccountByID(context.Background(), "openai", account.ID)
	if err != nil {
		t.Fatalf("FindAccountByID returned error: %v", err)
	}
	if unchanged.Status != AccountStatusCircuitOpen || unchanged.StatusReason != "existing health state" || unchanged.FailureCount != 4 || unchanged.CircuitOpenUntil == nil {
		t.Fatalf("account health mutated by model diagnostic: %+v", unchanged)
	}
	if len(requestLogger.entries) != 1 {
		t.Fatalf("request log count = %d, want 1", len(requestLogger.entries))
	}
	entry := requestLogger.entries[0]
	if entry.Route != "/v1/chat/completions" || entry.Method != http.MethodPost || entry.Model != "gpt-test" || entry.StatusCode != http.StatusTooManyRequests || entry.Error != "rate_limited" {
		t.Fatalf("model test request log = %+v", entry)
	}
	if entry.ProviderAccountID != account.ID || entry.ProviderAccountType != AccountTypeAPIUpstream || entry.ProviderAccountName != account.DisplayName || entry.Latency < 0 || entry.CreatedAt.IsZero() {
		t.Fatalf("model test request log attribution = %+v", entry)
	}
}

func TestTestAccountModelRejectsModelsNotConfiguredForAccount(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{testAccount(t, 7, true, 3, "access-token")}
	_, err := repo.ReplaceAccountModels(context.Background(), "openai", 7, []AccountModelInput{{Model: "configured", Enabled: true}})
	if err != nil {
		t.Fatalf("ReplaceAccountModels returned error: %v", err)
	}
	prober := &captureAccountModelProber{}
	service := newConfiguredService(repo, fakeOAuthClient{})
	service.modelProber = prober

	_, err = service.TestAccountModel(context.Background(), 7, "other-model")
	if !errors.Is(err, ErrNotConnected) {
		t.Fatalf("TestAccountModel error = %v, want ErrNotConnected", err)
	}
	if prober.calls != 0 {
		t.Fatalf("model prober calls = %d, want 0", prober.calls)
	}
}

func TestTestAccountModelRefreshFailureDoesNotChangeAccountHealth(t *testing.T) {
	repo := newMemoryRepo()
	expiresAt := time.Now().Add(-time.Minute)
	account := testExpiredAccount(t, 7, true, 3, "expired-access", "refresh-token", expiresAt)
	account.Status = AccountStatusActive
	repo.accounts = []Account{account}
	_, err := repo.ReplaceAccountModels(context.Background(), "openai", 7, []AccountModelInput{{Model: "gpt-test", Enabled: true}})
	if err != nil {
		t.Fatalf("ReplaceAccountModels returned error: %v", err)
	}
	service := newConfiguredService(repo, fakeOAuthClient{refreshErr: errors.New("refresh unavailable")})

	_, err = service.TestAccountModel(context.Background(), 7, "gpt-test")
	if err == nil {
		t.Fatal("TestAccountModel returned nil error, want refresh failure")
	}
	unchanged, err := repo.FindAccountByID(context.Background(), "openai", 7)
	if err != nil {
		t.Fatalf("FindAccountByID returned error: %v", err)
	}
	if unchanged.Status != AccountStatusActive || unchanged.FailureCount != 0 || unchanged.LastRefreshError != "" {
		t.Fatalf("refresh failure changed diagnostic account health: %+v", unchanged)
	}
}

func TestTestAccountModelSuccessfulRefreshUpdatesOnlyCredentialState(t *testing.T) {
	repo := newMemoryRepo()
	expiresAt := time.Now().Add(-time.Minute)
	account := testExpiredAccount(t, 7, true, 3, "expired-access", "refresh-token", expiresAt)
	account.Status = AccountStatusCircuitOpen
	account.StatusReason = "existing health state"
	account.LastError = "existing health state"
	account.FailureCount = 4
	until := time.Now().Add(time.Hour)
	account.CircuitOpenUntil = &until
	repo.accounts = []Account{account}
	_, err := repo.ReplaceAccountModels(context.Background(), "openai", 7, []AccountModelInput{{Model: "gpt-test", Enabled: true}})
	if err != nil {
		t.Fatalf("ReplaceAccountModels returned error: %v", err)
	}
	prober := &captureAccountModelProber{result: modelProbeResult{statusCode: http.StatusOK}}
	service := newConfiguredService(repo, fakeOAuthClient{refresh: TokenResponse{
		AccessToken:  "refreshed-access",
		RefreshToken: "refreshed-refresh",
		ExpiresIn:    3600,
	}})
	service.modelProber = prober

	if _, err := service.TestAccountModel(context.Background(), 7, "gpt-test"); err != nil {
		t.Fatalf("TestAccountModel returned error: %v", err)
	}
	if prober.selected.AuthorizationToken != "refreshed-access" {
		t.Fatalf("model probe token = %q, want refreshed-access", prober.selected.AuthorizationToken)
	}
	unchanged, err := repo.FindAccountByID(context.Background(), "openai", 7)
	if err != nil {
		t.Fatalf("FindAccountByID returned error: %v", err)
	}
	if unchanged.Status != AccountStatusCircuitOpen || unchanged.StatusReason != "existing health state" || unchanged.LastError != "existing health state" || unchanged.FailureCount != 4 || unchanged.CircuitOpenUntil == nil {
		t.Fatalf("successful diagnostic refresh changed account health: %+v", unchanged)
	}
	refreshedToken, err := decryptForTest(t, "encryption-secret", secret.SecretKindOAuthAccessToken, unchanged.Credential.EncryptedAccessToken)
	if err != nil || refreshedToken != "refreshed-access" {
		t.Fatalf("refreshed credential token = %q error=%v", refreshedToken, err)
	}
}

func TestListAccountTestResultsValidatesAndNormalizesLimit(t *testing.T) {
	baseTime := time.Now().UTC().Truncate(time.Second)
	repo := newMemoryRepo()
	repo.accounts = []Account{testAccount(t, 1, true, 1, "token")}
	for i := 0; i < 101; i++ {
		checkedAt := baseTime.Add(time.Duration(i) * time.Second)
		repo.accountTestResults = append(repo.accountTestResults, AccountTestResult{
			ID:        int64(i + 1),
			AccountID: 1,
			Provider:  "openai",
			Status:    AccountTestStatusPassed,
			CheckedAt: checkedAt,
			CreatedAt: checkedAt,
		})
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	if _, err := service.ListAccountTestResults(context.Background(), 0, 20); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid id error = %v, want ErrInvalidInput", err)
	}
	defaulted, err := service.ListAccountTestResults(context.Background(), 1, 0)
	if err != nil {
		t.Fatalf("ListAccountTestResults default limit returned error: %v", err)
	}
	if len(defaulted) != 20 {
		t.Fatalf("default result count = %d, want 20", len(defaulted))
	}
	capped, err := service.ListAccountTestResults(context.Background(), 1, 500)
	if err != nil {
		t.Fatalf("ListAccountTestResults capped limit returned error: %v", err)
	}
	if len(capped) != 100 {
		t.Fatalf("capped result count = %d, want 100", len(capped))
	}
	if capped[0].ID != 101 || capped[99].ID != 2 {
		t.Fatalf("capped ordering = first:%d last:%d, want newest-first IDs 101..2", capped[0].ID, capped[99].ID)
	}
}

func TestPauseAccountSchedulingTemporarilyOpensCircuit(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{testAccount(t, 7, true, 3, "access-token")}
	service := newConfiguredService(repo, fakeOAuthClient{})

	paused, err := service.PauseAccountScheduling(context.Background(), 7, 5*time.Minute)
	if err != nil {
		t.Fatalf("PauseAccountScheduling returned error: %v", err)
	}

	if paused.Status != AccountStatusCircuitOpen || paused.StatusReason != "manually paused" || paused.LastError != "manually paused" {
		t.Fatalf("paused account = %+v, want manual circuit-open status", paused)
	}
	if paused.CircuitOpenUntil == nil || !paused.CircuitOpenUntil.After(time.Now().Add(4*time.Minute)) {
		t.Fatalf("CircuitOpenUntil = %v, want future pause window", paused.CircuitOpenUntil)
	}
	if AccountSchedulable(paused, time.Now()) {
		t.Fatalf("paused account is schedulable: %+v", paused)
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

func TestSelectAccountForModelUsesLoadFactorWithinPriority(t *testing.T) {
	repo := newMemoryRepo()
	first := testAccount(t, 1, true, 1, "low-capacity-token")
	first.LoadFactor = 1
	second := testAccount(t, 2, true, 1, "high-capacity-token")
	second.LoadFactor = 5
	repo.accounts = []Account{first, second}
	service := newConfiguredService(repo, fakeOAuthClient{})

	selected, err := service.SelectAccountForModel(context.Background(), "")
	if err != nil {
		t.Fatalf("SelectAccountForModel returned error: %v", err)
	}
	if selected.AccountID != 2 || selected.AuthorizationToken != "high-capacity-token" {
		t.Fatalf("selected = %+v, want high load factor account", selected)
	}
}

func TestSelectedAccountIncludesMaxConcurrentRequests(t *testing.T) {
	repo := newMemoryRepo()
	account := testAccount(t, 7, true, 1, "access-token")
	account.MaxConcurrentRequests = 2
	repo.accounts = []Account{account}
	service := newConfiguredService(repo, fakeOAuthClient{})

	selected, err := service.SelectAccountForModel(context.Background(), "")
	if err != nil {
		t.Fatalf("SelectAccountForModel returned error: %v", err)
	}
	if selected.MaxConcurrentRequests != 2 {
		t.Fatalf("MaxConcurrentRequests = %d, want 2", selected.MaxConcurrentRequests)
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

func TestUpdateAccountCanRotateAPIUpstreamCredential(t *testing.T) {
	repo := newMemoryRepo()
	now := time.Now()
	openUntil := now.Add(time.Hour)
	repo.accounts = []Account{{
		ID:          7,
		Provider:    "openai",
		AccountType: AccountTypeAPIUpstream,
		Name:        "Upstream",
		Credential: AccountCredential{
			CredentialType:  CredentialTypeAPIKey,
			EncryptedAPIKey: mustEncrypt(t, "encryption-secret", secret.SecretKindProviderAPIKey, "old-secret"),
			BaseURL:         "https://old.example.test",
		},
		Enabled:          true,
		Priority:         1,
		Status:           AccountStatusCircuitOpen,
		StatusReason:     "old upstream credential failed",
		LastError:        "old upstream credential failed",
		LastErrorAt:      &now,
		FailureCount:     3,
		CircuitOpenUntil: &openUntil,
	}}
	service := newConfiguredService(repo, fakeOAuthClient{})
	oldEncryptedAPIKey := repo.accounts[0].Credential.EncryptedAPIKey
	baseURL := "https://new.example.test/v1/"
	apiKey := "new-secret"

	account, err := service.UpdateAccount(context.Background(), 7, AccountUpdate{
		APIUpstreamBaseURL: &baseURL,
		APIUpstreamAPIKey:  &apiKey,
	})
	if err != nil {
		t.Fatalf("UpdateAccount returned error: %v", err)
	}
	if account.Credential.BaseURL != "https://new.example.test" {
		t.Fatalf("BaseURL = %q, want normalized upstream base URL", account.Credential.BaseURL)
	}
	if account.Credential.EncryptedAPIKey == "" || account.Credential.EncryptedAPIKey == "new-secret" || account.Credential.EncryptedAPIKey == oldEncryptedAPIKey {
		t.Fatalf("encrypted API key = %q, want new encrypted non-plaintext value", account.Credential.EncryptedAPIKey)
	}
	decrypted, err := decryptForTest(t, "encryption-secret", secret.SecretKindProviderAPIKey, account.Credential.EncryptedAPIKey)
	if err != nil {
		t.Fatalf("DecryptString returned error: %v", err)
	}
	if decrypted != "new-secret" {
		t.Fatalf("decrypted API key = %q, want new-secret", decrypted)
	}
	if account.Status != AccountStatusActive || account.LastError != "" || account.CircuitOpenUntil != nil || account.FailureCount != 0 {
		t.Fatalf("account status after credential rotation = %+v, want local failure state cleared", account)
	}
}

func TestUpdateAccountRejectsNegativeMaxConcurrentRequests(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{testAccount(t, 7, true, 1, "access-token")}
	service := newConfiguredService(repo, fakeOAuthClient{})
	enabled := true
	maxConcurrentRequests := -1

	if _, err := service.UpdateAccount(context.Background(), 7, AccountUpdate{Enabled: &enabled, MaxConcurrentRequests: &maxConcurrentRequests}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("UpdateAccount error = %v, want ErrInvalidInput", err)
	}
}

func TestUpdateAccountRejectsAPIUpstreamCredentialPatchForOAuthAccount(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{testAccount(t, 7, true, 1, "access-token")}
	service := newConfiguredService(repo, fakeOAuthClient{})
	baseURL := "https://new.example.test/v1"

	if _, err := service.UpdateAccount(context.Background(), 7, AccountUpdate{APIUpstreamBaseURL: &baseURL}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("UpdateAccount error = %v, want ErrInvalidInput", err)
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

func TestUpdateAccountRejectsUnknownFingerprintProfile(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{testAccount(t, 7, true, 1, "access-token")}
	service := newConfiguredService(repo, fakeOAuthClient{})
	profileID := int64(999)

	if _, err := service.UpdateAccount(context.Background(), 7, AccountUpdate{FingerprintProfileIDSet: true, FingerprintProfileID: &profileID}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("UpdateAccount error = %v, want ErrInvalidInput", err)
	}
}

func TestUpdateAccountCanSetKnownFingerprintProfile(t *testing.T) {
	profileID := int64(7)
	repo := newMemoryRepo()
	repo.accounts = []Account{testAccount(t, 7, true, 1, "access-token")}
	repo.fingerprintProfiles[profileID] = FingerprintProfileData{UserAgent: "Mozilla/5.0"}
	service := newConfiguredService(repo, fakeOAuthClient{})

	updated, err := service.UpdateAccount(context.Background(), 7, AccountUpdate{FingerprintProfileIDSet: true, FingerprintProfileID: &profileID})
	if err != nil {
		t.Fatalf("UpdateAccount returned error: %v", err)
	}
	if updated.FingerprintProfileID == nil || *updated.FingerprintProfileID != profileID {
		t.Fatalf("FingerprintProfileID = %+v, want %d", updated.FingerprintProfileID, profileID)
	}
}

func TestUpdateAccountAllowsClearingFingerprintProfile(t *testing.T) {
	profileID := int64(7)
	repo := newMemoryRepo()
	account := testAccount(t, 7, true, 1, "access-token")
	account.FingerprintProfileID = &profileID
	repo.accounts = []Account{account}
	service := newConfiguredService(repo, fakeOAuthClient{})

	updated, err := service.UpdateAccount(context.Background(), 7, AccountUpdate{FingerprintProfileIDSet: true})
	if err != nil {
		t.Fatalf("UpdateAccount returned error: %v", err)
	}
	if updated.FingerprintProfileID != nil {
		t.Fatalf("FingerprintProfileID = %v, want cleared nil", *updated.FingerprintProfileID)
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

func TestSelectAccountAppliesFingerprintProfileForOAuthAndAPIUpstream(t *testing.T) {
	profileID := int64(7)
	for _, tc := range []struct {
		name        string
		account     Account
		wantToken   string
		wantBaseURL string
	}{
		{
			name: "codex oauth",
			account: func() Account {
				account := testAccount(t, 1, true, 1, "oauth-token")
				account.FingerprintProfileID = &profileID
				return account
			}(),
			wantToken:   "oauth-token",
			wantBaseURL: "https://api.openai.example.test",
		},
		{
			name: "api upstream",
			account: Account{
				ID:                   2,
				Provider:             "openai",
				AccountType:          AccountTypeAPIUpstream,
				Subject:              "api-upstream",
				DisplayName:          "API upstream",
				Enabled:              true,
				Priority:             1,
				FingerprintProfileID: &profileID,
				Credential: AccountCredential{
					BaseURL:         "https://upstream.example.test/v1",
					EncryptedAPIKey: mustEncrypt(t, "encryption-secret", secret.SecretKindProviderAPIKey, "api-token"),
				},
			},
			wantToken:   "api-token",
			wantBaseURL: "https://upstream.example.test/v1",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newMemoryRepo()
			repo.accounts = []Account{tc.account}
			repo.accountModels[tc.account.ID] = []AccountModel{{AccountID: tc.account.ID, Provider: "openai", Model: "gpt-5", Enabled: true}}
			repo.fingerprintProfiles[profileID] = FingerprintProfileData{
				UserAgent:      "Mozilla/5.0",
				TLSFingerprint: "chrome",
				Headers:        map[string]string{"X-Fingerprint": "enabled"},
			}
			service := newConfiguredService(repo, fakeOAuthClient{})
			service.cfg.APIBaseURL = "https://api.openai.example.test"

			selected, err := service.SelectAccountForModel(context.Background(), "gpt-5")
			if err != nil {
				t.Fatalf("SelectAccountForModel returned error: %v", err)
			}

			if selected.AuthorizationToken != tc.wantToken || selected.BaseURL != tc.wantBaseURL {
				t.Fatalf("selected token/baseURL = %q/%q, want %q/%q", selected.AuthorizationToken, selected.BaseURL, tc.wantToken, tc.wantBaseURL)
			}
			if selected.FingerprintUA != "Mozilla/5.0" {
				t.Fatalf("fingerprint UA = %q, want Mozilla/5.0", selected.FingerprintUA)
			}
			if selected.FingerprintTLS != "chrome" {
				t.Fatalf("fingerprint TLS = %q, want chrome", selected.FingerprintTLS)
			}
			if selected.FingerprintHeaders["X-Fingerprint"] != "enabled" {
				t.Fatalf("fingerprint headers = %+v, want custom header", selected.FingerprintHeaders)
			}
		})
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

func TestSelectAccountForModelAndSessionPersistsAndReusesBinding(t *testing.T) {
	now := time.Now()
	recent := now.Add(-time.Minute)
	older := now.Add(-time.Hour)
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "first-token"),
		testAccount(t, 2, true, 1, "second-token"),
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
	binding := repo.sessionBindings[sessionBindingKey("openai", "gpt-5", "workspace-123")]
	if binding.AccountID != selected.AccountID {
		t.Fatalf("stored binding = %+v, want selected account %d", binding, selected.AccountID)
	}

	repo.accounts[0].LastUsedAt = nil
	repo.accounts[1].LastUsedAt = &recent
	again, err := service.SelectAccountForModelAndSession(context.Background(), "gpt-5", "workspace-123")
	if err != nil {
		t.Fatalf("SelectAccountForModelAndSession after reorder returned error: %v", err)
	}
	if again.AccountID != selected.AccountID {
		t.Fatalf("sticky account = %d after reorder, want stored binding account %d", again.AccountID, selected.AccountID)
	}
}

func TestSelectAccountForModelInRoutingPoolScopesCandidates(t *testing.T) {
	repo := newMemoryRepo()
	repo.routingPools[7] = RoutingPool{ID: 7, Name: "primary", Enabled: true}
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "global-token"),
		testAccount(t, 2, true, 50, "pool-token"),
	}
	repo.routingPoolAccounts[7] = []RoutingPoolAccount{{AccountID: 2, Priority: 0}}
	for i := range repo.accounts {
		repo.accountModels[repo.accounts[i].ID] = []AccountModel{
			{AccountID: repo.accounts[i].ID, Provider: "openai", Model: "gpt-5", Enabled: true},
		}
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	selected, err := service.SelectAccountForModelInRoutingPool(context.Background(), 7, "gpt-5")
	if err != nil {
		t.Fatalf("SelectAccountForModelInRoutingPool returned error: %v", err)
	}
	if selected.AccountID != 2 || selected.AuthorizationToken != "pool-token" {
		t.Fatalf("selected = %+v, want pool account 2", selected)
	}
}

func TestSelectAccountForModelInRoutingPoolUsesMembershipPriority(t *testing.T) {
	repo := newMemoryRepo()
	repo.routingPools[7] = RoutingPool{ID: 7, Name: "primary", Enabled: true}
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "global-high-priority-token"),
		testAccount(t, 2, true, 100, "pool-high-priority-token"),
	}
	repo.routingPoolAccounts[7] = []RoutingPoolAccount{
		{AccountID: 1, Priority: 50},
		{AccountID: 2, Priority: 0},
	}
	for i := range repo.accounts {
		repo.accountModels[repo.accounts[i].ID] = []AccountModel{
			{AccountID: repo.accounts[i].ID, Provider: "openai", Model: "gpt-5", Enabled: true},
		}
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	selected, err := service.SelectAccountForModelInRoutingPool(context.Background(), 7, "gpt-5")
	if err != nil {
		t.Fatalf("SelectAccountForModelInRoutingPool returned error: %v", err)
	}
	if selected.AccountID != 2 || selected.AuthorizationToken != "pool-high-priority-token" {
		t.Fatalf("selected = %+v, want pool membership priority account 2", selected)
	}
}

func TestSelectAccountForModelAndSessionInRoutingPoolKeepsStickyHashInsideGlobalPriorityTier(t *testing.T) {
	repo := newMemoryRepo()
	repo.routingPools[7] = RoutingPool{ID: 7, Name: "primary", Enabled: true}
	repo.accounts = []Account{
		testAccount(t, 1, true, 100, "lower-global-priority-token"),
		testAccount(t, 2, true, 1, "preferred-first-token"),
		testAccount(t, 3, true, 1, "preferred-second-token"),
	}
	repo.routingPoolAccounts[7] = []RoutingPoolAccount{
		{AccountID: 1, Priority: 0},
		{AccountID: 2, Priority: 0},
		{AccountID: 3, Priority: 0},
	}
	for i := range repo.accounts {
		repo.accountModels[repo.accounts[i].ID] = []AccountModel{
			{AccountID: repo.accounts[i].ID, Provider: "openai", Model: "gpt-5", Enabled: true},
		}
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	selectedIDs := map[int64]bool{}
	for i := 0; i < 100; i++ {
		selected, err := service.SelectAccountForModelAndSessionInRoutingPool(
			context.Background(),
			7,
			"gpt-5",
			"workspace-"+strconv.Itoa(i),
		)
		if err != nil {
			t.Fatalf("SelectAccountForModelAndSessionInRoutingPool returned error: %v", err)
		}
		if selected.AccountID == 1 {
			t.Fatalf("session %d selected lower global priority account 1", i)
		}
		wantAccountID := int64(2 + stickyAccountIndex("workspace-"+strconv.Itoa(i), 2))
		if selected.AccountID != wantAccountID {
			t.Fatalf("session %d selected account %d, want deterministic equal-tier account %d", i, selected.AccountID, wantAccountID)
		}
		selectedIDs[selected.AccountID] = true
	}
	if !selectedIDs[2] || !selectedIDs[3] {
		t.Fatalf("selected accounts = %+v, want deterministic hash to reach equal-tier accounts 2 and 3", selectedIDs)
	}
}

func TestSelectAccountForModelInRoutingPoolMissingPoolFailsClosed(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "global-token"),
	}
	repo.accountModels[1] = []AccountModel{{AccountID: 1, Provider: "openai", Model: "gpt-5", Enabled: true}}
	service := newConfiguredService(repo, fakeOAuthClient{})

	if _, err := service.SelectAccountForModelInRoutingPool(context.Background(), 99, "gpt-5"); !errors.Is(err, ErrRoutingPoolNotFound) {
		t.Fatalf("SelectAccountForModelInRoutingPool error = %v, want ErrRoutingPoolNotFound", err)
	}
}

func TestSelectAccountForModelInRoutingPoolChainFallsBackByModel(t *testing.T) {
	repo := newMemoryRepo()
	repo.routingPools[1] = RoutingPool{ID: 1, Name: "primary", Enabled: true, FallbackPoolID: ptrInt64(2)}
	repo.routingPools[2] = RoutingPool{ID: 2, Name: "secondary", Enabled: true}
	repo.accounts = []Account{
		testAccount(t, 10, true, 1, "primary-token"),
		testAccount(t, 20, true, 1, "secondary-token"),
	}
	repo.routingPoolAccounts[1] = []RoutingPoolAccount{{AccountID: 10, Priority: 0}}
	repo.routingPoolAccounts[2] = []RoutingPoolAccount{{AccountID: 20, Priority: 0}}
	repo.accountModels[10] = []AccountModel{{AccountID: 10, Provider: "openai", Model: "gpt-4", Enabled: true}}
	repo.accountModels[20] = []AccountModel{{AccountID: 20, Provider: "openai", Model: "gpt-5", Enabled: true}}
	service := newConfiguredService(repo, fakeOAuthClient{})

	selected, err := service.SelectAccountForModelInRoutingPoolChain(context.Background(), 1, "gpt-5")
	if err != nil {
		t.Fatalf("SelectAccountForModelInRoutingPoolChain returned error: %v", err)
	}
	if selected.AccountID != 20 || selected.RoutingPoolID != 2 || selected.RoutingPoolFallbackDepth != 1 {
		t.Fatalf("selected = %+v, want account 20 in fallback pool 2 depth 1", selected)
	}
	if selected.RoutingPoolFallbackChain != "primary -> secondary" {
		t.Fatalf("chain = %q, want primary -> secondary", selected.RoutingPoolFallbackChain)
	}
}

func TestSelectAccountForModelInRoutingPoolChainSkipsDisabledFallbackPool(t *testing.T) {
	repo := newMemoryRepo()
	repo.routingPools[1] = RoutingPool{ID: 1, Name: "primary", Enabled: true, FallbackPoolID: ptrInt64(2)}
	repo.routingPools[2] = RoutingPool{ID: 2, Name: "disabled fallback", Enabled: false, FallbackPoolID: ptrInt64(3)}
	repo.routingPools[3] = RoutingPool{ID: 3, Name: "emergency", Enabled: true}
	repo.accounts = []Account{
		testAccount(t, 10, true, 1, "primary-token"),
		testAccount(t, 20, true, 1, "disabled-token"),
		testAccount(t, 30, true, 1, "emergency-token"),
	}
	repo.routingPoolAccounts[1] = []RoutingPoolAccount{{AccountID: 10, Priority: 0}}
	repo.routingPoolAccounts[2] = []RoutingPoolAccount{{AccountID: 20, Priority: 0}}
	repo.routingPoolAccounts[3] = []RoutingPoolAccount{{AccountID: 30, Priority: 0}}
	repo.accountModels[10] = []AccountModel{{AccountID: 10, Provider: "openai", Model: "gpt-4", Enabled: true}}
	repo.accountModels[20] = []AccountModel{{AccountID: 20, Provider: "openai", Model: "gpt-5", Enabled: true}}
	repo.accountModels[30] = []AccountModel{{AccountID: 30, Provider: "openai", Model: "gpt-5", Enabled: true}}
	service := newConfiguredService(repo, fakeOAuthClient{})

	selected, err := service.SelectAccountForModelInRoutingPoolChain(context.Background(), 1, "gpt-5")
	if err != nil {
		t.Fatalf("SelectAccountForModelInRoutingPoolChain returned error: %v", err)
	}
	if selected.AccountID != 30 || selected.RoutingPoolID != 3 || selected.RoutingPoolFallbackDepth != 2 {
		t.Fatalf("selected = %+v, want emergency fallback account 30 at depth 2", selected)
	}
	if selected.RoutingPoolFallbackChain != "primary -> disabled fallback -> emergency" {
		t.Fatalf("chain = %q, want full fallback chain", selected.RoutingPoolFallbackChain)
	}
}

func TestSelectAccountForModelInRoutingPoolChainRejectsCycle(t *testing.T) {
	repo := newMemoryRepo()
	repo.routingPools[1] = RoutingPool{ID: 1, Name: "primary", Enabled: true, FallbackPoolID: ptrInt64(2)}
	repo.routingPools[2] = RoutingPool{ID: 2, Name: "secondary", Enabled: true, FallbackPoolID: ptrInt64(1)}
	service := newConfiguredService(repo, fakeOAuthClient{})

	selected, err := service.SelectAccountForModelInRoutingPoolChain(context.Background(), 1, "gpt-5")
	if !errors.Is(err, ErrRoutingPoolCycle) {
		t.Fatalf("cycle error = %v, want ErrRoutingPoolCycle", err)
	}
	if selected.RoutingPoolError != RoutingPoolErrorCycle {
		t.Fatalf("routing pool error = %q, want %s", selected.RoutingPoolError, RoutingPoolErrorCycle)
	}
}

func TestSelectAccountForModelInRoutingPoolChainMarksUnavailableDiagnostics(t *testing.T) {
	service := newConfiguredService(newMemoryRepo(), fakeOAuthClient{})

	selected, err := service.SelectAccountForModelInRoutingPoolChain(context.Background(), 99, "gpt-5")
	if !errors.Is(err, ErrRoutingPoolNotFound) {
		t.Fatalf("error = %v, want ErrRoutingPoolNotFound", err)
	}
	if selected.RoutingPoolError != RoutingPoolErrorUnavailable {
		t.Fatalf("routing pool error = %q, want %s", selected.RoutingPoolError, RoutingPoolErrorUnavailable)
	}
}

func TestSelectAccountForModelInRoutingPoolChainMarksMissingFallbackAsExhausted(t *testing.T) {
	repo := newMemoryRepo()
	repo.routingPools[1] = RoutingPool{ID: 1, Name: "primary", Enabled: true, FallbackPoolID: ptrInt64(2)}
	service := newConfiguredService(repo, fakeOAuthClient{})

	selected, err := service.SelectAccountForModelInRoutingPoolChain(context.Background(), 1, "gpt-5")
	if !errors.Is(err, ErrRoutingPoolExhausted) {
		t.Fatalf("error = %v, want ErrRoutingPoolExhausted", err)
	}
	if selected.RoutingPoolError != RoutingPoolErrorExhausted {
		t.Fatalf("routing pool error = %q, want %s", selected.RoutingPoolError, RoutingPoolErrorExhausted)
	}
	if selected.RoutingPoolFallbackChain != "primary" {
		t.Fatalf("chain = %q, want primary", selected.RoutingPoolFallbackChain)
	}
}

func TestSelectAccountForModelInRoutingPoolChainMarksExhaustedDiagnostics(t *testing.T) {
	repo := newMemoryRepo()
	repo.routingPools[1] = RoutingPool{ID: 1, Name: "primary", Enabled: true, FallbackPoolID: ptrInt64(2)}
	repo.routingPools[2] = RoutingPool{ID: 2, Name: "secondary", Enabled: true}
	repo.accounts = []Account{
		testAccount(t, 10, true, 1, "primary-token"),
		testAccount(t, 20, true, 1, "secondary-token"),
	}
	repo.routingPoolAccounts[1] = []RoutingPoolAccount{{AccountID: 10, Priority: 0}}
	repo.routingPoolAccounts[2] = []RoutingPoolAccount{{AccountID: 20, Priority: 0}}
	repo.accountModels[10] = []AccountModel{{AccountID: 10, Provider: "openai", Model: "gpt-4", Enabled: true}}
	repo.accountModels[20] = []AccountModel{{AccountID: 20, Provider: "openai", Model: "gpt-4", Enabled: true}}
	service := newConfiguredService(repo, fakeOAuthClient{})

	selected, err := service.SelectAccountForModelInRoutingPoolChain(context.Background(), 1, "gpt-5")
	if !errors.Is(err, ErrModelUnavailable) {
		t.Fatalf("error = %v, want ErrModelUnavailable", err)
	}
	if selected.RoutingPoolError != "routing_pool_exhausted" {
		t.Fatalf("routing pool error = %q, want routing_pool_exhausted", selected.RoutingPoolError)
	}
	if selected.RoutingPoolFallbackChain != "primary -> secondary" {
		t.Fatalf("chain = %q, want primary -> secondary", selected.RoutingPoolFallbackChain)
	}
}

func TestSelectAccountForModelInRoutingPoolChainMarksDisabledPrimaryDiagnostics(t *testing.T) {
	repo := newMemoryRepo()
	repo.routingPools[1] = RoutingPool{ID: 1, Name: "primary", Enabled: false, FallbackPoolID: ptrInt64(2)}
	repo.routingPools[2] = RoutingPool{ID: 2, Name: "secondary", Enabled: true}
	service := newConfiguredService(repo, fakeOAuthClient{})

	selected, err := service.SelectAccountForModelInRoutingPoolChain(context.Background(), 1, "gpt-5")
	if !errors.Is(err, ErrAccountsDisabled) {
		t.Fatalf("error = %v, want ErrAccountsDisabled", err)
	}
	if selected.RoutingPoolID != 1 || selected.RoutingPoolName != "primary" {
		t.Fatalf("selected = %+v, want primary pool diagnostics", selected)
	}
	if selected.RoutingPoolError != RoutingPoolErrorDisabled {
		t.Fatalf("routing pool error = %q, want %s", selected.RoutingPoolError, RoutingPoolErrorDisabled)
	}
}

func TestSelectAccountForModelInRoutingPoolChainMarksEmptyPrimaryDiagnostics(t *testing.T) {
	repo := newMemoryRepo()
	repo.routingPools[1] = RoutingPool{ID: 1, Name: "primary", Enabled: true}
	service := newConfiguredService(repo, fakeOAuthClient{})

	selected, err := service.SelectAccountForModelInRoutingPoolChain(context.Background(), 1, "gpt-5")
	if !errors.Is(err, ErrRoutingPoolEmpty) {
		t.Fatalf("error = %v, want ErrRoutingPoolEmpty", err)
	}
	if selected.RoutingPoolID != 1 || selected.RoutingPoolName != "primary" {
		t.Fatalf("selected = %+v, want primary pool diagnostics", selected)
	}
	if selected.RoutingPoolError != RoutingPoolErrorEmpty {
		t.Fatalf("routing pool error = %q, want %s", selected.RoutingPoolError, RoutingPoolErrorEmpty)
	}
}

func TestListExposedModelsForRoutingPoolChainIncludesFallbackModels(t *testing.T) {
	repo := newMemoryRepo()
	repo.routingPools[1] = RoutingPool{ID: 1, Name: "primary", Enabled: true, FallbackPoolID: ptrInt64(2)}
	repo.routingPools[2] = RoutingPool{ID: 2, Name: "secondary", Enabled: true}
	repo.accounts = []Account{
		testAccount(t, 10, true, 1, "primary-token"),
		testAccount(t, 20, true, 1, "secondary-token"),
		testAccount(t, 30, true, 1, "global-token"),
	}
	repo.routingPoolAccounts[1] = []RoutingPoolAccount{{AccountID: 10, Priority: 0}}
	repo.routingPoolAccounts[2] = []RoutingPoolAccount{{AccountID: 20, Priority: 0}}
	repo.accountModels[10] = []AccountModel{{AccountID: 10, Provider: "openai", Model: "gpt-4", Enabled: true}}
	repo.accountModels[20] = []AccountModel{{AccountID: 20, Provider: "openai", Model: "gpt-5", Enabled: true}}
	repo.accountModels[30] = []AccountModel{{AccountID: 30, Provider: "openai", Model: "global-only", Enabled: true}}
	service := newConfiguredService(repo, fakeOAuthClient{})

	models, err := service.ListExposedModelsForRoutingPoolChain(context.Background(), 1)
	if err != nil {
		t.Fatalf("ListExposedModelsForRoutingPoolChain returned error: %v", err)
	}
	if got := exposedModelIDs(models); !reflect.DeepEqual(got, []string{"gpt-4", "gpt-5"}) {
		t.Fatalf("models = %+v, want primary and fallback models only", got)
	}
}

func TestSelectAccountForModelAndSessionInRoutingPoolDoesNotCrossScope(t *testing.T) {
	repo := newMemoryRepo()
	repo.routingPools[7] = RoutingPool{ID: 7, Name: "primary", Enabled: true}
	repo.routingPools[8] = RoutingPool{ID: 8, Name: "secondary", Enabled: true}
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "first-token"),
		testAccount(t, 2, true, 1, "second-token"),
	}
	repo.routingPoolAccounts[7] = []RoutingPoolAccount{{AccountID: 1, Priority: 0}}
	repo.routingPoolAccounts[8] = []RoutingPoolAccount{{AccountID: 2, Priority: 0}}
	for i := range repo.accounts {
		repo.accountModels[repo.accounts[i].ID] = []AccountModel{
			{AccountID: repo.accounts[i].ID, Provider: "openai", Model: "gpt-5", Enabled: true},
		}
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	first, err := service.SelectAccountForModelAndSessionInRoutingPool(context.Background(), 7, "gpt-5", "workspace-123")
	if err != nil {
		t.Fatalf("pool 7 selection returned error: %v", err)
	}
	second, err := service.SelectAccountForModelAndSessionInRoutingPool(context.Background(), 8, "gpt-5", "workspace-123")
	if err != nil {
		t.Fatalf("pool 8 selection returned error: %v", err)
	}
	if first.AccountID != 1 || second.AccountID != 2 {
		t.Fatalf("pool scoped selections = %d/%d, want 1/2", first.AccountID, second.AccountID)
	}
}

func TestSelectAccountForModelAndSessionInRoutingPoolRebindsWhenBoundAccountExcluded(t *testing.T) {
	repo := newMemoryRepo()
	repo.routingPools[7] = RoutingPool{ID: 7, Name: "primary", Enabled: true}
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "bound-token"),
		testAccount(t, 2, true, 1, "fallback-token"),
	}
	repo.routingPoolAccounts[7] = []RoutingPoolAccount{
		{AccountID: 1, Priority: 0},
		{AccountID: 2, Priority: 0},
	}
	for i := range repo.accounts {
		repo.accountModels[repo.accounts[i].ID] = []AccountModel{
			{AccountID: repo.accounts[i].ID, Provider: "openai", Model: "gpt-5", Enabled: true},
		}
	}
	repo.sessionBindings[routingPoolSessionBindingKey("openai", 7, "gpt-5", "workspace-123")] = SessionBinding{
		ID:        1,
		Provider:  "openai",
		Model:     "gpt-5",
		SessionID: "workspace-123",
		AccountID: 1,
		CreatedAt: time.Now().Add(-time.Hour),
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	selected, err := service.SelectAccountForModelAndSessionInRoutingPool(context.Background(), 7, "gpt-5", "workspace-123", 1)
	if err != nil {
		t.Fatalf("SelectAccountForModelAndSessionInRoutingPool returned error: %v", err)
	}
	if selected.AccountID != 2 {
		t.Fatalf("selected account = %d, want fallback account 2", selected.AccountID)
	}
	binding := repo.sessionBindings[routingPoolSessionBindingKey("openai", 7, "gpt-5", "workspace-123")]
	if binding.AccountID != 2 {
		t.Fatalf("stored pool binding = %+v, want rebound account 2", binding)
	}
	if _, ok := repo.sessionBindings[sessionBindingKey("openai", "gpt-5", "workspace-123")]; ok {
		t.Fatal("pool-scoped selection wrote a global sticky binding")
	}
}

func TestSelectAccountForModelAndSessionRebindsWhenBoundAccountExcluded(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "first-token"),
		testAccount(t, 2, true, 1, "fallback-token"),
	}
	for i := range repo.accounts {
		repo.accountModels[repo.accounts[i].ID] = []AccountModel{
			{AccountID: repo.accounts[i].ID, Provider: "openai", Model: "gpt-5", Enabled: true},
		}
	}
	repo.sessionBindings[sessionBindingKey("openai", "gpt-5", "workspace-123")] = SessionBinding{
		ID:        1,
		Provider:  "openai",
		Model:     "gpt-5",
		SessionID: "workspace-123",
		AccountID: 1,
		CreatedAt: time.Now().Add(-time.Hour),
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	selected, err := service.SelectAccountForModelAndSession(context.Background(), "gpt-5", "workspace-123", 1)
	if err != nil {
		t.Fatalf("SelectAccountForModelAndSession returned error: %v", err)
	}
	if selected.AccountID != 2 {
		t.Fatalf("selected account = %d, want fallback account 2", selected.AccountID)
	}
	binding := repo.sessionBindings[sessionBindingKey("openai", "gpt-5", "workspace-123")]
	if binding.AccountID != 2 {
		t.Fatalf("stored binding = %+v, want rebound account 2", binding)
	}
}

func TestSelectAccountForModelDoesNotCreateSessionBinding(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "first-token"),
	}
	repo.accountModels[1] = []AccountModel{{AccountID: 1, Provider: "openai", Model: "gpt-5", Enabled: true}}
	service := newConfiguredService(repo, fakeOAuthClient{})

	if _, err := service.SelectAccountForModel(context.Background(), "gpt-5"); err != nil {
		t.Fatalf("SelectAccountForModel returned error: %v", err)
	}
	if len(repo.sessionBindings) != 0 {
		t.Fatalf("session bindings = %+v, want none for non-session selection", repo.sessionBindings)
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

func TestPreviewAccountSelectionMeasuresStrictLoadFactorTierAcrossTenThousandSessions(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "high-load-first-token"),
		testAccount(t, 2, true, 1, "high-load-second-token"),
		testAccount(t, 3, true, 1, "low-load-token"),
	}
	repo.accounts[0].LoadFactor = 10
	repo.accounts[1].LoadFactor = 10
	repo.accounts[2].LoadFactor = 1
	for i := range repo.accounts {
		repo.accountModels[repo.accounts[i].ID] = []AccountModel{
			{AccountID: repo.accounts[i].ID, Provider: "openai", Model: "gpt-5", Enabled: true},
		}
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	const sessionCount = 10_000
	counts := map[int64]int{}
	for i := 0; i < sessionCount; i++ {
		sessionID := "distribution-" + strconv.Itoa(i)
		preview, err := service.PreviewAccountSelection(context.Background(), "gpt-5", sessionID)
		if err != nil {
			t.Fatalf("PreviewAccountSelection(%q) returned error: %v", sessionID, err)
		}
		wantAccountID := int64(1 + stickyAccountIndex(sessionID, 2))
		if preview.SelectedAccountID != wantAccountID {
			t.Fatalf("session %q selected account %d, want deterministic FNV account %d", sessionID, preview.SelectedAccountID, wantAccountID)
		}
		counts[preview.SelectedAccountID]++
	}

	if counts[3] != 0 {
		t.Fatalf("low load-factor account selections = %d, want 0 because load factor is a strict tier", counts[3])
	}
	for _, accountID := range []int64{1, 2} {
		if counts[accountID] < 4_500 || counts[accountID] > 5_500 {
			t.Fatalf("equal-tier account %d selections = %d, want 4500..5500 of %d", accountID, counts[accountID], sessionCount)
		}
	}
}

func TestSelectAccountForModelAndSessionDoesNotPromoteErroredPriorityPeer(t *testing.T) {
	now := time.Now()
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "errored-token"),
		testAccount(t, 2, true, 1, "clean-token"),
	}
	repo.accounts[0].LastError = "temporary failure"
	repo.accounts[0].LastErrorAt = &now
	for i := range repo.accounts {
		repo.accountModels[repo.accounts[i].ID] = []AccountModel{
			{AccountID: repo.accounts[i].ID, Provider: "openai", Model: "gpt-5", Enabled: true},
		}
	}
	sessionID := ""
	for i := 0; i < 100; i++ {
		candidate := "workspace-" + strconv.Itoa(i)
		if stickyAccountIndex(candidate, 2) == 0 {
			sessionID = candidate
			break
		}
	}
	if sessionID == "" {
		t.Fatal("could not find deterministic sticky session fixture")
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	selected, err := service.SelectAccountForModelAndSession(context.Background(), "gpt-5", sessionID)
	if err != nil {
		t.Fatalf("SelectAccountForModelAndSession returned error: %v", err)
	}
	if selected.AccountID != 2 {
		t.Fatalf("selected account = %d, want clean account 2 before errored peer", selected.AccountID)
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
	lastTestAt := time.Now().Add(-2 * time.Minute).UTC().Truncate(time.Second)
	repo.accounts[0].LastTestAt = &lastTestAt
	repo.accounts[0].LastTestStatus = AccountTestStatusFailed
	repo.accounts[0].LastTestError = "quota window"
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
	var tested SelectionCandidate
	for _, candidate := range preview.Candidates {
		if candidate.ID == 1 {
			tested = candidate
			break
		}
	}
	if tested.LastTestAt == nil || !tested.LastTestAt.Equal(lastTestAt) {
		t.Fatalf("candidate LastTestAt = %v, want %v", tested.LastTestAt, lastTestAt)
	}
	if tested.LastTestStatus != AccountTestStatusFailed || tested.LastTestError != "quota window" {
		t.Fatalf("candidate test result = status:%q error:%q, want failed/quota window", tested.LastTestStatus, tested.LastTestError)
	}
	for _, account := range repo.accounts {
		if account.LastUsedAt == nil || !account.LastUsedAt.Equal(older) {
			t.Fatalf("account %d last used = %v, want unchanged %v", account.ID, account.LastUsedAt, older)
		}
	}
}

func TestPreviewAccountSelectionIncludesScheduleReasons(t *testing.T) {
	older := time.Now().Add(-time.Hour)
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "first-token"),
		testAccount(t, 2, true, 1, "bound-token"),
		testAccount(t, 3, true, 2, "next-token"),
	}
	for i := range repo.accounts {
		repo.accounts[i].LastUsedAt = &older
		repo.accountModels[repo.accounts[i].ID] = []AccountModel{
			{AccountID: repo.accounts[i].ID, Provider: "openai", Model: "gpt-5", Enabled: true},
		}
	}
	repo.sessionBindings[sessionBindingKey("openai", "gpt-5", "workspace-123")] = SessionBinding{
		ID:        1,
		Provider:  "openai",
		Model:     "gpt-5",
		SessionID: "workspace-123",
		AccountID: 2,
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	preview, err := service.PreviewAccountSelection(context.Background(), "gpt-5", "workspace-123")
	if err != nil {
		t.Fatalf("PreviewAccountSelection returned error: %v", err)
	}

	want := map[int64]string{
		2: "reused sticky session binding for account priority 1, load factor 1, recent-error tier clean; new sticky FNV hashes stay within the highest exactly equal scheduling tier; base tie-breakers least-recently-used then account ID 2",
		1: "ordered after sticky FNV hash, which only changes order within the highest exactly equal scheduling tier: account priority 1, load factor 1, recent-error tier clean; base tie-breakers least-recently-used then account ID 1",
		3: "ordered after sticky FNV hash, which only changes order within the highest exactly equal scheduling tier: account priority 2, load factor 1, recent-error tier clean; base tie-breakers least-recently-used then account ID 3",
	}
	for _, candidate := range preview.Candidates {
		if candidate.ScheduleReason != want[candidate.ID] {
			t.Fatalf("candidate %d ScheduleReason = %q, want %q", candidate.ID, candidate.ScheduleReason, want[candidate.ID])
		}
		delete(want, candidate.ID)
	}
	if len(want) > 0 {
		t.Fatalf("missing candidates: %+v", want)
	}
}

func TestPreviewAccountSelectionReportsStoredStickyBindingWithoutMutation(t *testing.T) {
	older := time.Now().Add(-time.Hour)
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "first-token"),
		testAccount(t, 2, true, 1, "bound-token"),
	}
	for i := range repo.accounts {
		repo.accounts[i].LastUsedAt = &older
		repo.accountModels[repo.accounts[i].ID] = []AccountModel{
			{AccountID: repo.accounts[i].ID, Provider: "openai", Model: "gpt-5", Enabled: true},
		}
	}
	createdAt := time.Now().Add(-time.Hour)
	repo.sessionBindings[sessionBindingKey("openai", "gpt-5", "workspace-123")] = SessionBinding{
		ID:         1,
		Provider:   "openai",
		Model:      "gpt-5",
		SessionID:  "workspace-123",
		AccountID:  2,
		CreatedAt:  createdAt,
		UpdatedAt:  createdAt,
		LastUsedAt: createdAt,
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	preview, err := service.PreviewAccountSelection(context.Background(), "gpt-5", "workspace-123")
	if err != nil {
		t.Fatalf("PreviewAccountSelection returned error: %v", err)
	}

	if preview.StickyBoundAccountID != 2 {
		t.Fatalf("StickyBoundAccountID = %d, want 2", preview.StickyBoundAccountID)
	}
	if preview.SelectedAccountID != 2 || len(preview.Candidates) < 1 || !preview.Candidates[0].StickyBound {
		t.Fatalf("preview = %+v, want bound account selected with sticky marker", preview)
	}
	if got := repo.sessionBindings[sessionBindingKey("openai", "gpt-5", "workspace-123")]; !got.UpdatedAt.Equal(createdAt) {
		t.Fatalf("binding UpdatedAt = %v, want unchanged %v", got.UpdatedAt, createdAt)
	}
}

func TestPreviewAccountSelectionReportsStoredStickyBindingForSingleCandidate(t *testing.T) {
	older := time.Now().Add(-time.Hour)
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 7, true, 1, "bound-token"),
	}
	repo.accounts[0].LastUsedAt = &older
	repo.accountModels[7] = []AccountModel{
		{AccountID: 7, Provider: "openai", Model: "gpt-5", Enabled: true},
	}
	createdAt := time.Now().Add(-time.Hour)
	repo.sessionBindings[sessionBindingKey("openai", "gpt-5", "workspace-123")] = SessionBinding{
		ID:         1,
		Provider:   "openai",
		Model:      "gpt-5",
		SessionID:  "workspace-123",
		AccountID:  7,
		CreatedAt:  createdAt,
		UpdatedAt:  createdAt,
		LastUsedAt: createdAt,
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	preview, err := service.PreviewAccountSelection(context.Background(), "gpt-5", "workspace-123")
	if err != nil {
		t.Fatalf("PreviewAccountSelection returned error: %v", err)
	}

	if preview.StickyBoundAccountID != 7 {
		t.Fatalf("StickyBoundAccountID = %d, want 7", preview.StickyBoundAccountID)
	}
	if len(preview.Candidates) != 1 || !preview.Candidates[0].StickyBound || preview.Candidates[0].ScheduleReason != "reused sticky session binding for account priority 1, load factor 1, recent-error tier clean; new sticky FNV hashes stay within the highest exactly equal scheduling tier; base tie-breakers least-recently-used then account ID 7" {
		t.Fatalf("candidate = %+v, want single sticky-bound candidate", preview.Candidates)
	}
	if got := repo.sessionBindings[sessionBindingKey("openai", "gpt-5", "workspace-123")]; !got.UpdatedAt.Equal(createdAt) {
		t.Fatalf("binding UpdatedAt = %v, want unchanged %v", got.UpdatedAt, createdAt)
	}
}

func TestPreviewAccountSelectionIncludesUnschedulableReasons(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "first-token"),
		testAccount(t, 2, false, 1, "disabled-token"),
		testAccount(t, 3, true, 1, "missing-model-token"),
		testAccount(t, 4, true, 1, "excluded-token"),
	}
	repo.accountModels[1] = []AccountModel{{AccountID: 1, Provider: "openai", Model: "gpt-5", Enabled: true}}
	repo.accountModels[2] = []AccountModel{{AccountID: 2, Provider: "openai", Model: "gpt-5", Enabled: true}}
	repo.accountModels[3] = []AccountModel{{AccountID: 3, Provider: "openai", Model: "gpt-4.1", Enabled: true}}
	repo.accountModels[4] = []AccountModel{{AccountID: 4, Provider: "openai", Model: "gpt-5", Enabled: true}}
	service := newConfiguredService(repo, fakeOAuthClient{})

	preview, err := service.PreviewAccountSelection(context.Background(), "gpt-5", "", 4)
	if err != nil {
		t.Fatalf("PreviewAccountSelection returned error: %v", err)
	}

	if len(preview.Candidates) != 4 {
		t.Fatalf("candidates = %+v, want eligible and unschedulable accounts", preview.Candidates)
	}
	want := map[int64]struct {
		schedulable bool
		reason      string
		selected    bool
	}{
		1: {schedulable: true, selected: true},
		2: {schedulable: false, reason: "account disabled"},
		3: {schedulable: false, reason: "model not configured"},
		4: {schedulable: false, reason: "account excluded"},
	}
	for _, candidate := range preview.Candidates {
		expected, ok := want[candidate.ID]
		if !ok {
			t.Fatalf("unexpected candidate %+v", candidate)
		}
		if candidate.Schedulable != expected.schedulable || candidate.UnschedulableReason != expected.reason || candidate.Selected != expected.selected {
			t.Fatalf("candidate %d = schedulable:%v reason:%q selected:%v, want schedulable:%v reason:%q selected:%v", candidate.ID, candidate.Schedulable, candidate.UnschedulableReason, candidate.Selected, expected.schedulable, expected.reason, expected.selected)
		}
		delete(want, candidate.ID)
	}
	if len(want) > 0 {
		t.Fatalf("missing candidates: %+v", want)
	}
}

func TestPreviewAccountSelectionReturnsBlockedCandidatesWhenNoneSchedulable(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 1, false, 1, "disabled-token"),
		testAccount(t, 2, true, 1, "missing-model-token"),
	}
	repo.accountModels[1] = []AccountModel{{AccountID: 1, Provider: "openai", Model: "gpt-5", Enabled: true}}
	repo.accountModels[2] = []AccountModel{{AccountID: 2, Provider: "openai", Model: "gpt-4.1", Enabled: true}}
	service := newConfiguredService(repo, fakeOAuthClient{})

	preview, err := service.PreviewAccountSelection(context.Background(), "gpt-5", "")
	if err != nil {
		t.Fatalf("PreviewAccountSelection returned error: %v", err)
	}

	if preview.Model != "gpt-5" || preview.SelectedAccountID != 0 {
		t.Fatalf("preview metadata = %+v, want model gpt-5 with no selected account", preview)
	}
	want := map[int64]string{
		1: "account disabled",
		2: "model not configured",
	}
	if len(preview.Candidates) != len(want) {
		t.Fatalf("candidates = %+v, want blocked candidates", preview.Candidates)
	}
	for _, candidate := range preview.Candidates {
		reason, ok := want[candidate.ID]
		if !ok {
			t.Fatalf("unexpected candidate %+v", candidate)
		}
		if candidate.Schedulable || candidate.Selected || candidate.UnschedulableReason != reason {
			t.Fatalf("candidate %d = schedulable:%v selected:%v reason:%q, want blocked reason %q", candidate.ID, candidate.Schedulable, candidate.Selected, candidate.UnschedulableReason, reason)
		}
		delete(want, candidate.ID)
	}
	if len(want) > 0 {
		t.Fatalf("missing blocked candidates: %+v", want)
	}
}

func TestPreviewAccountSelectionInRoutingPoolScopesCandidatesAndStickyBinding(t *testing.T) {
	repo := newMemoryRepo()
	repo.routingPools[7] = RoutingPool{ID: 7, Name: "primary", Enabled: true}
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "global-token"),
		testAccount(t, 2, true, 10, "pool-token"),
		testAccount(t, 3, false, 10, "pool-disabled-token"),
	}
	repo.routingPoolAccounts[7] = []RoutingPoolAccount{
		{AccountID: 2, Priority: 20},
		{AccountID: 3, Priority: 30},
	}
	for i := range repo.accounts {
		repo.accountModels[repo.accounts[i].ID] = []AccountModel{
			{AccountID: repo.accounts[i].ID, Provider: "openai", Model: "gpt-5", Enabled: true},
		}
	}
	createdAt := time.Now().Add(-time.Hour)
	repo.sessionBindings[routingPoolSessionBindingKey("openai", 7, "gpt-5", "workspace-123")] = SessionBinding{
		ID:         1,
		Provider:   "openai",
		Model:      "gpt-5",
		SessionID:  "workspace-123",
		AccountID:  2,
		CreatedAt:  createdAt,
		UpdatedAt:  createdAt,
		LastUsedAt: createdAt,
	}
	service := newConfiguredService(repo, fakeOAuthClient{})

	preview, err := service.PreviewAccountSelectionInRoutingPool(context.Background(), 7, "gpt-5", "workspace-123")
	if err != nil {
		t.Fatalf("PreviewAccountSelectionInRoutingPool returned error: %v", err)
	}

	if preview.Model != "gpt-5" || preview.SessionID != "workspace-123" || preview.SelectedAccountID != 2 || preview.StickyBoundAccountID != 2 {
		t.Fatalf("preview metadata = %+v, want pool-scoped sticky account 2", preview)
	}
	if len(preview.Candidates) != 2 {
		t.Fatalf("candidates = %+v, want pool account and blocked pool member only", preview.Candidates)
	}
	want := map[int64]struct {
		schedulable bool
		selected    bool
		priority    int
		reason      string
	}{
		2: {schedulable: true, selected: true, priority: 20},
		3: {schedulable: false, priority: 30, reason: "account disabled"},
	}
	for _, candidate := range preview.Candidates {
		expected, ok := want[candidate.ID]
		if !ok {
			t.Fatalf("unexpected candidate %+v", candidate)
		}
		if candidate.Schedulable != expected.schedulable || candidate.Selected != expected.selected || candidate.Priority != expected.priority || candidate.UnschedulableReason != expected.reason {
			t.Fatalf("candidate %d = schedulable:%v selected:%v priority:%d reason:%q, want schedulable:%v selected:%v priority:%d reason:%q", candidate.ID, candidate.Schedulable, candidate.Selected, candidate.Priority, candidate.UnschedulableReason, expected.schedulable, expected.selected, expected.priority, expected.reason)
		}
		if candidate.ID == 2 && candidate.ScheduleReason != "reused sticky session binding for pool priority 20, global account priority 10, load factor 1, recent-error tier clean; new sticky FNV hashes stay within the highest exactly equal scheduling tier; base tie-breakers least-recently-used then account ID 2" {
			t.Fatalf("pool candidate ScheduleReason = %q, want pool and global account tiers", candidate.ScheduleReason)
		}
		delete(want, candidate.ID)
	}
	if len(want) > 0 {
		t.Fatalf("missing candidates: %+v", want)
	}
	if _, ok := repo.sessionBindings[sessionBindingKey("openai", "gpt-5", "workspace-123")]; ok {
		t.Fatal("pool preview used global sticky binding scope")
	}
}

func TestPreviewAccountSelectionInRoutingPoolMarksMissingFallbackAsExhausted(t *testing.T) {
	repo := newMemoryRepo()
	repo.routingPools[1] = RoutingPool{ID: 1, Name: "primary", Enabled: true, FallbackPoolID: ptrInt64(2)}
	service := newConfiguredService(repo, fakeOAuthClient{})

	preview, err := service.PreviewAccountSelectionInRoutingPool(context.Background(), 1, "gpt-5", "")
	if !errors.Is(err, ErrRoutingPoolExhausted) {
		t.Fatalf("error = %v, want ErrRoutingPoolExhausted", err)
	}
	if preview.RoutingPoolError != RoutingPoolErrorExhausted {
		t.Fatalf("routing pool error = %q, want %s", preview.RoutingPoolError, RoutingPoolErrorExhausted)
	}
	if preview.RoutingPoolFallbackChain != "primary" {
		t.Fatalf("chain = %q, want primary", preview.RoutingPoolFallbackChain)
	}
}

func TestPreviewAccountSelectionInRoutingPoolFollowsFallbackChain(t *testing.T) {
	repo := newMemoryRepo()
	repo.routingPools[1] = RoutingPool{ID: 1, Name: "primary", Enabled: true, FallbackPoolID: ptrInt64(2)}
	repo.routingPools[2] = RoutingPool{ID: 2, Name: "secondary", Enabled: true}
	repo.accounts = []Account{
		testAccount(t, 10, true, 1, "primary-token"),
		testAccount(t, 20, true, 1, "secondary-token"),
	}
	repo.routingPoolAccounts[1] = []RoutingPoolAccount{{AccountID: 10, Priority: 0}}
	repo.routingPoolAccounts[2] = []RoutingPoolAccount{{AccountID: 20, Priority: 0}}
	repo.accountModels[10] = []AccountModel{{AccountID: 10, Provider: "openai", Model: "gpt-4.1", Enabled: true}}
	repo.accountModels[20] = []AccountModel{{AccountID: 20, Provider: "openai", Model: "gpt-5", Enabled: true}}
	service := newConfiguredService(repo, fakeOAuthClient{})

	preview, err := service.PreviewAccountSelectionInRoutingPool(context.Background(), 1, "gpt-5", "")
	if err != nil {
		t.Fatalf("PreviewAccountSelectionInRoutingPool returned error: %v", err)
	}

	if preview.SelectedAccountID != 20 {
		t.Fatalf("selected account = %d, want fallback pool account 20", preview.SelectedAccountID)
	}
	if preview.RoutingPoolID != 2 || preview.RoutingPoolName != "secondary" || preview.RoutingPoolFallbackDepth != 1 || preview.RoutingPoolFallbackChain != "primary -> secondary" {
		t.Fatalf("routing pool metadata = id:%d name:%q depth:%d chain:%q, want secondary fallback chain", preview.RoutingPoolID, preview.RoutingPoolName, preview.RoutingPoolFallbackDepth, preview.RoutingPoolFallbackChain)
	}
	if len(preview.Candidates) != 2 || preview.Candidates[0].ID != 20 || !preview.Candidates[0].Selected || preview.Candidates[1].ID != 10 || preview.Candidates[1].UnschedulableReason != "model not configured" {
		t.Fatalf("candidates = %+v, want selected fallback account then blocked primary account", preview.Candidates)
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
	decrypted, err := decryptForTest(t, "encryption-secret", secret.SecretKindProviderAPIKey, account.Credential.EncryptedAPIKey)
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

func TestCreateAPIUpstreamAccountStoresEncryptedProxyURL(t *testing.T) {
	repo := newMemoryRepo()
	service := newConfiguredService(repo, fakeOAuthClient{})

	account, err := service.CreateAPIUpstreamAccount(context.Background(), APIUpstreamInput{
		Name:     "Upstream",
		BaseURL:  "https://upstream.example.test/v1",
		APIKey:   "sk-upstream",
		ProxyURL: " http://proxy-user:proxy-pass@proxy.example.test:8080 ",
	})
	if err != nil {
		t.Fatalf("CreateAPIUpstreamAccount returned error: %v", err)
	}
	if account.Credential.EncryptedProxyURL == "" {
		t.Fatal("proxy URL was not stored encrypted")
	}
	if strings.Contains(account.Credential.EncryptedProxyURL, "proxy-pass") || !account.ProxyURLConfigured {
		t.Fatalf("proxy fields leaked or missing configured flag: %+v", account)
	}

	selected, err := service.SelectAccountForModel(context.Background(), "")
	if err != nil {
		t.Fatalf("SelectAccountForModel returned error: %v", err)
	}
	if selected.ProxyURL != "http://proxy-user:proxy-pass@proxy.example.test:8080" {
		t.Fatalf("selected proxy URL = %q, want trimmed cleartext for outbound use", selected.ProxyURL)
	}
}

func TestCreateAPIUpstreamAccountSetsKnownFingerprintProfile(t *testing.T) {
	repo := newMemoryRepo()
	service := newConfiguredService(repo, fakeOAuthClient{})
	profileID := int64(9)
	repo.fingerprintProfiles[profileID] = FingerprintProfileData{UserAgent: "Mozilla/5.0"}

	account, err := service.CreateAPIUpstreamAccount(context.Background(), APIUpstreamInput{
		Name:                 "Upstream",
		BaseURL:              "https://upstream.example.test/v1",
		APIKey:               "sk-upstream",
		FingerprintProfileID: &profileID,
	})
	if err != nil {
		t.Fatalf("CreateAPIUpstreamAccount returned error: %v", err)
	}
	if account.FingerprintProfileID == nil || *account.FingerprintProfileID != profileID {
		t.Fatalf("FingerprintProfileID = %+v, want %d", account.FingerprintProfileID, profileID)
	}
	if repo.accounts[0].FingerprintProfileID == nil || *repo.accounts[0].FingerprintProfileID != profileID {
		t.Fatalf("saved FingerprintProfileID = %+v, want %d", repo.accounts[0].FingerprintProfileID, profileID)
	}
}

func TestUpdateAccountCanSetAndClearProxyURL(t *testing.T) {
	repo := newMemoryRepo()
	service := newConfiguredService(repo, fakeOAuthClient{})
	account, err := service.CreateAPIUpstreamAccount(context.Background(), APIUpstreamInput{
		Name:    "Upstream",
		BaseURL: "https://upstream.example.test/v1",
		APIKey:  "sk-upstream",
	})
	if err != nil {
		t.Fatalf("CreateAPIUpstreamAccount returned error: %v", err)
	}

	proxyURL := "https://proxy.example.test:8443"
	updated, err := service.UpdateAccount(context.Background(), account.ID, AccountUpdate{ProxyURL: &proxyURL})
	if err != nil {
		t.Fatalf("UpdateAccount set proxy returned error: %v", err)
	}
	if !updated.ProxyURLConfigured || updated.ProxyURLSummary != "https://proxy.example.test:8443" {
		t.Fatalf("updated proxy summary = configured:%v summary:%q", updated.ProxyURLConfigured, updated.ProxyURLSummary)
	}

	clear := ""
	updated, err = service.UpdateAccount(context.Background(), account.ID, AccountUpdate{ProxyURL: &clear})
	if err != nil {
		t.Fatalf("UpdateAccount clear proxy returned error: %v", err)
	}
	if updated.ProxyURLConfigured || updated.ProxyURLSummary != "" || updated.Credential.EncryptedProxyURL != "" {
		t.Fatalf("cleared proxy fields = %+v", updated)
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
		{name: "http scheme without opt-in", input: APIUpstreamInput{Name: valid.Name, BaseURL: "http://upstream.example.test/v1", APIKey: valid.APIKey}},
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

func TestCreateAPIUpstreamAccountAllowsHTTPBaseURLWhenConfigured(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, fakeOAuthClient{}, Config{
		Provider:              "openai",
		ClientID:              "client-id",
		ClientSecret:          "client-secret",
		RedirectURL:           "http://localhost/oauth/openai/callback",
		AuthURL:               "https://auth.example.test/authorize",
		TokenURL:              "https://auth.example.test/token",
		Secret:                "encryption-secret",
		AllowHTTPAPIUpstreams: true,
	})

	account, err := service.CreateAPIUpstreamAccount(context.Background(), APIUpstreamInput{
		Name:    "Local upstream",
		BaseURL: "http://127.0.0.1:8080/v1",
		APIKey:  "secret",
	})
	if err != nil {
		t.Fatalf("CreateAPIUpstreamAccount returned error: %v", err)
	}
	if account.Credential.BaseURL != "http://127.0.0.1:8080" {
		t.Fatalf("BaseURL = %q, want normalized HTTP upstream", account.Credential.BaseURL)
	}
}

func TestSyncUpstreamAccountModelsFetchesAndSyncs(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/v1/models" {
			t.Errorf("path = %s, want /v1/models", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer sk-upstream-key" {
			t.Errorf("Authorization = %q, want Bearer sk-upstream-key", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":[{"id":"gpt-4"},{"id":"gpt-4-turbo"},{"id":"gpt-4"},{"id":"  "},{"id":"gpt-5"}]}`))
	}))
	defer ts.Close()

	repo := newMemoryRepo()
	service := NewService(repo, fakeOAuthClient{}, Config{
		Provider:              "openai",
		ClientID:              "client-id",
		ClientSecret:          "client-secret",
		RedirectURL:           "http://localhost/oauth/openai/callback",
		AuthURL:               "https://auth.example.test/authorize",
		TokenURL:              "https://auth.example.test/token",
		Secret:                "encryption-secret",
		AllowHTTPAPIUpstreams: true,
	})

	account, err := service.CreateAPIUpstreamAccount(context.Background(), APIUpstreamInput{
		Name:    "Test Upstream",
		BaseURL: ts.URL + "/v1",
		APIKey:  "sk-upstream-key",
	})
	if err != nil {
		t.Fatalf("CreateAPIUpstreamAccount returned error: %v", err)
	}

	models, summary, err := service.SyncUpstreamAccountModels(context.Background(), account.ID)
	if err != nil {
		t.Fatalf("SyncUpstreamAccountModels returned error: %v", err)
	}
	if summary.Total != 3 {
		t.Fatalf("summary.Total = %d, want 3 (deduped)", summary.Total)
	}
	if summary.New != 3 {
		t.Fatalf("summary.New = %d, want 3", summary.New)
	}
	if len(models) != 3 {
		t.Fatalf("len(models) = %d, want 3", len(models))
	}
	names := make([]string, 0, len(models))
	for _, m := range models {
		names = append(names, m.Model)
	}
	got := strings.Join(names, ",")
	if got != "gpt-4,gpt-4-turbo,gpt-5" {
		t.Fatalf("models = %s, want gpt-4,gpt-4-turbo,gpt-5", got)
	}
	// New synced rows should be disabled by default.
	for _, m := range models {
		if m.Enabled {
			t.Fatalf("model %s is enabled, want disabled for new upstream sync rows", m.Model)
		}
	}
	// Second sync should preserve existing rows.
	models2, summary2, err := service.SyncUpstreamAccountModels(context.Background(), account.ID)
	if err != nil {
		t.Fatalf("second SyncUpstreamAccountModels returned error: %v", err)
	}
	if summary2.New != 0 || summary2.Preserved != 3 {
		t.Fatalf("second summary: new=%d preserved=%d, want new=0 preserved=3", summary2.New, summary2.Preserved)
	}
	for _, m := range models2 {
		if m.Enabled {
			t.Fatalf("model %s is enabled on second sync, want preserved as disabled", m.Model)
		}
	}
}

func TestSyncUpstreamAccountModelsDoesNotDoubleV1BaseURL(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path = %s, want /v1/models", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":[{"id":"gpt-4"}]}`))
	}))
	defer ts.Close()

	repo := newMemoryRepo()
	service := NewService(repo, fakeOAuthClient{}, Config{
		Provider:              "openai",
		ClientID:              "client-id",
		ClientSecret:          "client-secret",
		RedirectURL:           "http://localhost/oauth/openai/callback",
		AuthURL:               "https://auth.example.test/authorize",
		TokenURL:              "https://auth.example.test/token",
		Secret:                "encryption-secret",
		AllowHTTPAPIUpstreams: true,
	})

	account, err := repo.SaveAccount(context.Background(), Account{
		Provider:    "openai",
		AccountType: AccountTypeAPIUpstream,
		Name:        "Legacy upstream",
		DisplayName: "Legacy upstream",
		Enabled:     true,
		Priority:    100,
		LoadFactor:  1,
		Status:      AccountStatusActive,
		Credential: AccountCredential{
			CredentialType:  CredentialTypeAPIKey,
			EncryptedAPIKey: mustEncrypt(t, "encryption-secret", secret.SecretKindProviderAPIKey, "sk-upstream-key"),
			BaseURL:         ts.URL + "/v1",
		},
	})
	if err != nil {
		t.Fatalf("SaveAccount returned error: %v", err)
	}

	if _, _, err := service.SyncUpstreamAccountModels(context.Background(), account.ID); err != nil {
		t.Fatalf("SyncUpstreamAccountModels returned error: %v", err)
	}
}

func TestSyncUpstreamAccountModelsRejectsNonAPIUpstream(t *testing.T) {
	repo := newMemoryRepo()
	account := testAccount(t, 5, true, 1, "oauth-token")
	account.AccountType = AccountTypeCodexOAuth
	repo.accounts = []Account{account}
	service := newConfiguredService(repo, fakeOAuthClient{})

	_, _, err := service.SyncUpstreamAccountModels(context.Background(), 5)
	if err != ErrInvalidInput {
		t.Fatalf("err = %v, want ErrInvalidInput for non-api_upstream account", err)
	}
}

func TestSyncUpstreamAccountModelsNon2xxDoesNotUpdateRows(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer ts.Close()

	repo := newMemoryRepo()
	service := NewService(repo, fakeOAuthClient{}, Config{
		Provider:              "openai",
		ClientID:              "client-id",
		ClientSecret:          "client-secret",
		RedirectURL:           "http://localhost/oauth/openai/callback",
		AuthURL:               "https://auth.example.test/authorize",
		TokenURL:              "https://auth.example.test/token",
		Secret:                "encryption-secret",
		AllowHTTPAPIUpstreams: true,
	})

	account, err := service.CreateAPIUpstreamAccount(context.Background(), APIUpstreamInput{
		Name:    "Bad Upstream",
		BaseURL: ts.URL + "/v1",
		APIKey:  "sk-bad",
	})
	if err != nil {
		t.Fatalf("CreateAPIUpstreamAccount returned error: %v", err)
	}

	_, _, err = service.SyncUpstreamAccountModels(context.Background(), account.ID)
	if err == nil {
		t.Fatal("expected error for non-2xx response")
	}

	// Verify no models were synced.
	models, err := service.ListAccountModels(context.Background(), account.ID)
	if err != nil {
		t.Fatalf("ListAccountModels returned error: %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("len(models) = %d, want 0 (no rows synced after non-2xx)", len(models))
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

func TestRecordAccountUsedOnlyMarksAttemptWithoutClearingHealth(t *testing.T) {
	repo := newMemoryRepo()
	account := testAccount(t, 1, true, 1, "access-token")
	now := time.Now()
	until := now.Add(time.Hour)
	account.Status = AccountStatusCircuitOpen
	account.StatusReason = "upstream unavailable"
	account.LastError = "upstream unavailable"
	account.LastErrorAt = &now
	account.FailureCount = 3
	account.CircuitOpenUntil = &until
	account.LastRefreshError = "refresh diagnostic"
	repo.accounts = []Account{account}
	service := newConfiguredService(repo, fakeOAuthClient{})

	if err := service.RecordAccountUsed(context.Background(), 1); err != nil {
		t.Fatalf("RecordAccountUsed returned error: %v", err)
	}
	if repo.accounts[0].LastUsedAt == nil {
		t.Fatal("account was not marked used")
	}
	if repo.accounts[0].Status != AccountStatusCircuitOpen || repo.accounts[0].StatusReason != "upstream unavailable" ||
		repo.accounts[0].LastError != "upstream unavailable" || repo.accounts[0].LastErrorAt == nil ||
		repo.accounts[0].FailureCount != 3 || repo.accounts[0].CircuitOpenUntil == nil ||
		repo.accounts[0].LastRefreshError != "refresh diagnostic" {
		t.Fatalf("account health changed on attempt: %+v", repo.accounts[0])
	}
	if len(repo.intents) != 0 {
		t.Fatalf("attempt intents = %+v, want none", repo.intents)
	}
}

func TestRecordAccountRecoveredClearsHealthWithRecoveryIntent(t *testing.T) {
	repo := newMemoryRepo()
	account := testAccount(t, 1, true, 1, "access-token")
	now := time.Now()
	until := now.Add(time.Hour)
	account.Status = AccountStatusRateLimited
	account.StatusReason = "rate limited"
	account.LastError = "rate limited"
	account.LastErrorAt = &now
	account.FailureCount = 2
	account.RateLimitedUntil = &until
	account.LastRefreshError = "preserved refresh diagnostic"
	repo.accounts = []Account{account}
	service := newConfiguredService(repo, fakeOAuthClient{})

	if err := service.RecordAccountRecovered(context.Background(), 1); err != nil {
		t.Fatalf("RecordAccountRecovered returned error: %v", err)
	}
	if repo.accounts[0].Status != AccountStatusActive || repo.accounts[0].StatusReason != "" || repo.accounts[0].LastError != "" ||
		repo.accounts[0].LastErrorAt != nil || repo.accounts[0].FailureCount != 0 || repo.accounts[0].RateLimitedUntil != nil {
		t.Fatalf("recovered account = %+v, want active health", repo.accounts[0])
	}
	if repo.accounts[0].LastRefreshError != "preserved refresh diagnostic" {
		t.Fatalf("LastRefreshError = %q, want preserved diagnostic", repo.accounts[0].LastRefreshError)
	}
	if len(repo.intents) != 1 || repo.intents[0].Action != systemevent.ActionProviderAccountRecovered || repo.intents[0].Category != systemevent.CategoryRuntime {
		t.Fatalf("recovery intents = %+v", repo.intents)
	}
}

func TestRecordAccountUsedReturnsMarkAccountUsedFailure(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{
		testAccount(t, 1, true, 1, "access-token"),
	}
	repo.markAccountUsedErr = errors.New("mark account used failed")
	service := newConfiguredService(repo, fakeOAuthClient{})

	if err := service.RecordAccountUsed(context.Background(), 1); !errors.Is(err, repo.markAccountUsedErr) {
		t.Fatalf("RecordAccountUsed error = %v, want mark account used failure", err)
	}
}

func TestProviderServiceAttachesLifecycleIntentsAndPreservesBatchID(t *testing.T) {
	repo := newMemoryRepo()
	repo.accounts = []Account{testAccount(t, 7, true, 3, "access-token")}
	service := newConfiguredService(repo, fakeOAuthClient{})
	ctx := systemevent.WithIntent(context.Background(), systemevent.EventIntent{
		Metadata: map[string]any{"batch_id": "batch-123"},
	})
	enabled := false
	if _, err := service.UpdateAccount(ctx, 7, AccountUpdate{Enabled: &enabled}); err != nil {
		t.Fatalf("UpdateAccount returned error: %v", err)
	}
	if len(repo.intents) != 1 {
		t.Fatalf("captured intents = %d, want 1", len(repo.intents))
	}
	intent := repo.intents[0]
	if intent.Action != systemevent.ActionProviderAccountUpdated || intent.Target.ID != "7" || intent.Metadata["batch_id"] != "batch-123" {
		t.Fatalf("intent = %+v, want provider update with batch ID", intent)
	}
	if err := systemevent.ValidateIntent(intent); err != nil {
		t.Fatalf("ValidateIntent returned error: %v", err)
	}
}

func TestOAuthRefreshIntentsDistinguishManualAndAutomaticWithoutSecrets(t *testing.T) {
	expired := time.Now().Add(-time.Minute)
	for _, testCase := range []struct {
		name         string
		refresh      func(*Service, Account) error
		wantAction   systemevent.Action
		wantTrigger  string
		wantSeverity systemevent.Severity
	}{
		{
			name: "manual",
			refresh: func(service *Service, account Account) error {
				_, err := service.RefreshAccount(context.Background(), account.ID)
				return err
			},
			wantAction:   systemevent.ActionOAuthRefreshManualFailed,
			wantTrigger:  string(RefreshTriggerManual),
			wantSeverity: systemevent.SeverityWarning,
		},
		{
			name: "gateway request",
			refresh: func(service *Service, account Account) error {
				_, err := service.AccessTokenForAccount(context.Background(), account)
				return err
			},
			wantAction:   systemevent.ActionOAuthRefreshAutomaticFailed,
			wantTrigger:  string(RefreshTriggerGatewayRequest),
			wantSeverity: systemevent.SeverityWarning,
		},
		{
			name: "model test",
			refresh: func(service *Service, account Account) error {
				_, err := service.selectedAccountForDiagnostic(context.Background(), account)
				return err
			},
			wantAction:   systemevent.ActionOAuthRefreshDiagnosticFailed,
			wantTrigger:  string(RefreshTriggerModelTest),
			wantSeverity: systemevent.SeverityWarning,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			repo := newMemoryRepo()
			account := testExpiredAccount(t, 7, true, 3, "old-access", "old-refresh", expired)
			repo.accounts = []Account{account}
			service := newConfiguredService(repo, fakeOAuthClient{refreshErr: errors.New("raw upstream refresh failure")})
			if err := testCase.refresh(service, account); err == nil {
				t.Fatal("refresh returned nil error, want upstream failure")
			}
			if len(repo.intents) == 0 {
				t.Fatal("refresh did not attach an event intent")
			}
			intent := repo.intents[len(repo.intents)-1]
			if intent.Action != testCase.wantAction || intent.Metadata["trigger"] != testCase.wantTrigger || intent.Severity != testCase.wantSeverity {
				t.Fatalf("intent = %+v, want action %q trigger %q", intent, testCase.wantAction, testCase.wantTrigger)
			}
			encoded, err := json.Marshal(intent)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{"raw upstream refresh failure", "old-access", "old-refresh"} {
				if strings.Contains(string(encoded), forbidden) {
					t.Fatalf("intent contains sensitive/raw value %q: %s", forbidden, encoded)
				}
			}
			if err := systemevent.ValidateIntent(intent); err != nil {
				t.Fatalf("ValidateIntent returned error: %v", err)
			}
		})
	}
}

func TestDiagnosticRefreshSuccessUsesAutomaticRecoveryAction(t *testing.T) {
	expired := time.Now().Add(-time.Minute)
	repo := newMemoryRepo()
	account := testExpiredAccount(t, 7, true, 3, "old-access", "old-refresh", expired)
	repo.accounts = []Account{account}
	service := newConfiguredService(repo, fakeOAuthClient{refresh: TokenResponse{
		AccessToken: "new-access", RefreshToken: "new-refresh", ExpiresIn: 3600,
	}})

	if _, err := service.selectedAccountForDiagnostic(context.Background(), account); err != nil {
		t.Fatalf("selectedAccountForDiagnostic returned error: %v", err)
	}
	if len(repo.intents) == 0 {
		t.Fatal("diagnostic refresh success did not attach an event intent")
	}
	intent := repo.intents[len(repo.intents)-1]
	if intent.Action != systemevent.ActionOAuthRefreshAutomaticSucceeded || intent.Severity != systemevent.SeverityInfo || intent.Metadata["trigger"] != string(RefreshTriggerModelTest) {
		t.Fatalf("intent = %+v, want automatic refresh recovery", intent)
	}
}

func TestStartConnectAttachesOAuthIntentWithoutAuthorizationMaterial(t *testing.T) {
	repo := newMemoryRepo()
	repo.defaultFingerprintProfileID = 9
	service := newConfiguredService(repo, fakeOAuthClient{})
	if _, err := service.StartConnect(context.Background(), ConnectOptions{}); err != nil {
		t.Fatalf("StartConnect returned error: %v", err)
	}
	if len(repo.intents) != 1 || repo.intents[0].Action != systemevent.ActionOAuthConnectStarted {
		t.Fatalf("intents = %+v, want oauth connect started", repo.intents)
	}
	encoded, err := json.Marshal(repo.intents[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"authorize", "state", "verifier", "challenge"} {
		if strings.Contains(strings.ToLower(string(encoded)), forbidden) {
			t.Fatalf("connect intent contains forbidden material %q: %s", forbidden, encoded)
		}
	}
}

type memoryRepo struct {
	accounts            []Account
	accountModels       map[int64][]AccountModel
	accountTestResults  []AccountTestResult
	sessionBindings     map[string]SessionBinding
	routingPools        map[int64]RoutingPool
	routingPoolAccounts map[int64][]RoutingPoolAccount
	fingerprintProfiles map[int64]FingerprintProfileData
	states              []OAuthState

	saveCount                            int
	nextID                               int64
	defaultFingerprintProfileID          int64
	ensureDefaultFingerprintProfileCalls int
	markAccountErrorErr                  error
	markAccountUsedErr                   error
	replaceModelsErr                     error
	lastSavedAccount                     Account
	intents                              []systemevent.EventIntent
}

func (r *memoryRepo) captureIntent(ctx context.Context) {
	if intent, ok := systemevent.IntentFromContext(ctx); ok {
		r.intents = append(r.intents, intent)
	}
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{
		accountModels:       make(map[int64][]AccountModel),
		sessionBindings:     make(map[string]SessionBinding),
		routingPools:        make(map[int64]RoutingPool),
		routingPoolAccounts: make(map[int64][]RoutingPoolAccount),
		fingerprintProfiles: make(map[int64]FingerprintProfileData),
		nextID:              1,
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
		if normalizedLoadFactor(accounts[i].LoadFactor) != normalizedLoadFactor(accounts[j].LoadFactor) {
			return normalizedLoadFactor(accounts[i].LoadFactor) > normalizedLoadFactor(accounts[j].LoadFactor)
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
	r.captureIntent(ctx)
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
	account.ProxyURLConfigured = account.Credential.EncryptedProxyURL != ""
	account.Metadata = account.Credential.Metadata
	if account.Metadata == nil {
		account.Metadata = map[string]string{}
	}
	if account.Credential.Metadata == nil {
		account.Credential.Metadata = account.Metadata
	}
	if account.BaseURL == "" {
		account.BaseURL = account.Credential.BaseURL
	}
	if account.Credential.BaseURL == "" {
		account.Credential.BaseURL = account.BaseURL
	}
}

func (r *memoryRepo) UpdateAccount(ctx context.Context, providerName string, id int64, update AccountUpdate) (Account, error) {
	r.captureIntent(ctx)
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
		if update.LoadFactor != nil {
			r.accounts[i].LoadFactor = *update.LoadFactor
		}
		if update.MaxConcurrentRequests != nil {
			r.accounts[i].MaxConcurrentRequests = *update.MaxConcurrentRequests
		}
		if update.Name != nil {
			r.accounts[i].Name = *update.Name
		}
		if update.APIUpstreamBaseURL != nil {
			r.accounts[i].Credential.BaseURL = *update.APIUpstreamBaseURL
			r.accounts[i].BaseURL = *update.APIUpstreamBaseURL
		}
		if update.EncryptedAPIUpstreamAPIKey != nil {
			r.accounts[i].Credential.EncryptedAPIKey = *update.EncryptedAPIUpstreamAPIKey
		}
		if update.EncryptedProxyURL != nil {
			r.accounts[i].Credential.EncryptedProxyURL = *update.EncryptedProxyURL
			r.accounts[i].ProxyURLConfigured = *update.EncryptedProxyURL != ""
		}
		if update.FingerprintProfileIDSet {
			r.accounts[i].FingerprintProfileID = update.FingerprintProfileID
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
	r.captureIntent(ctx)
	for i := range r.accounts {
		if r.accounts[i].Provider == providerName && r.accounts[i].ID == id {
			r.accounts = append(r.accounts[:i], r.accounts[i+1:]...)
			return nil
		}
	}
	return ErrNotConnected
}

func (r *memoryRepo) DeleteAccounts(ctx context.Context, providerName string) error {
	r.captureIntent(ctx)
	kept := r.accounts[:0]
	for _, account := range r.accounts {
		if account.Provider != providerName {
			kept = append(kept, account)
		}
	}
	r.accounts = kept
	return nil
}

func (r *memoryRepo) FindFingerprintProfileByID(_ context.Context, id int64) (FingerprintProfileData, error) {
	profile, ok := r.fingerprintProfiles[id]
	if !ok {
		return FingerprintProfileData{}, ErrNotConnected
	}
	return profile, nil
}

func (r *memoryRepo) EnsureDefaultCodexFingerprintProfile(_ context.Context) (int64, error) {
	r.ensureDefaultFingerprintProfileCalls++
	if r.defaultFingerprintProfileID == 0 {
		r.defaultFingerprintProfileID = 9001
	}
	if r.fingerprintProfiles == nil {
		r.fingerprintProfiles = make(map[int64]FingerprintProfileData)
	}
	r.fingerprintProfiles[r.defaultFingerprintProfileID] = FingerprintProfileData{
		UserAgent: DefaultCodexFingerprintUserAgent,
		Headers:   DefaultCodexFingerprintHeaders(),
	}
	return r.defaultFingerprintProfileID, nil
}

func (r *memoryRepo) MarkAccountUsed(ctx context.Context, providerName string, id int64, usedAt time.Time) error {
	if r.markAccountUsedErr != nil {
		return r.markAccountUsedErr
	}
	for i := range r.accounts {
		if r.accounts[i].Provider == providerName && r.accounts[i].ID == id {
			r.accounts[i].LastUsedAt = &usedAt
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
	r.captureIntent(ctx)
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

func (r *memoryRepo) RecordOAuthRefreshFailureEvent(ctx context.Context, _ string, _ int64) error {
	r.captureIntent(ctx)
	return nil
}

func (r *memoryRepo) RecordAccountStatus(ctx context.Context, providerName string, id int64, status, reason string, at time.Time, rateLimitedUntil, circuitOpenUntil *time.Time) error {
	r.captureIntent(ctx)
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

func (r *memoryRepo) RecordAccountTestResult(ctx context.Context, providerName string, id int64, status, message string, at time.Time) error {
	r.captureIntent(ctx)
	for i := range r.accounts {
		if r.accounts[i].Provider == providerName && r.accounts[i].ID == id {
			r.accounts[i].LastTestAt = &at
			r.accounts[i].LastTestStatus = status
			r.accounts[i].LastTestError = message
			r.accountTestResults = append(r.accountTestResults, AccountTestResult{
				ID:        int64(len(r.accountTestResults) + 1),
				AccountID: id,
				Provider:  providerName,
				Status:    status,
				Message:   message,
				CheckedAt: at,
				CreatedAt: time.Now(),
			})
			return nil
		}
	}
	return ErrNotConnected
}

func (r *memoryRepo) UpdateOAuthCredential(ctx context.Context, providerName string, accountID int64, credential AccountCredential) error {
	r.captureIntent(ctx)
	for i := range r.accounts {
		if r.accounts[i].Provider != providerName || r.accounts[i].ID != accountID {
			continue
		}
		account := normalizeAccountCredentialFields(r.accounts[i])
		account.Credential.EncryptedAccessToken = credential.EncryptedAccessToken
		account.Credential.EncryptedRefreshToken = credential.EncryptedRefreshToken
		account.Credential.EncryptedIDToken = credential.EncryptedIDToken
		account.Credential.AccessTokenExpiresAt = credential.AccessTokenExpiresAt
		account.Credential.LastRefreshAt = credential.LastRefreshAt
		account.Credential.LastRefreshError = credential.LastRefreshError
		account.Credential.LastRefreshErrorAt = credential.LastRefreshErrorAt
		if account.Credential.Metadata == nil {
			account.Credential.Metadata = map[string]string{}
		}
		for key, value := range credential.Metadata {
			account.Credential.Metadata[key] = value
		}
		account.EncryptedAccessToken = credential.EncryptedAccessToken
		account.EncryptedRefreshToken = credential.EncryptedRefreshToken
		account.EncryptedIDToken = credential.EncryptedIDToken
		account.AccessTokenExpiresAt = credential.AccessTokenExpiresAt
		account.LastRefreshAt = credential.LastRefreshAt
		account.LastRefreshError = credential.LastRefreshError
		account.LastRefreshErrorAt = credential.LastRefreshErrorAt
		account.Metadata = account.Credential.Metadata
		r.accounts[i] = normalizeAccountCredentialFields(account)
		return nil
	}
	return ErrNotConnected
}

func (r *memoryRepo) RecordAccountModelTestResult(ctx context.Context, providerName string, result AccountModelTestResult) error {
	r.captureIntent(ctx)
	models := r.accountModels[result.AccountID]
	for i := range models {
		if models[i].Provider == providerName && models[i].Model == result.Model {
			checkedAt := result.CheckedAt
			models[i].LastTestAt = &checkedAt
			models[i].LastTestStatus = result.Status
			models[i].LastTestHTTPStatus = result.HTTPStatus
			models[i].LastTestLatencyMS = result.LatencyMS
			models[i].LastError = result.Message
			r.accountModels[result.AccountID] = models
			return nil
		}
	}
	return ErrNotConnected
}

func (r *memoryRepo) ListAccountTestResults(ctx context.Context, providerName string, accountID int64, limit int) ([]AccountTestResult, error) {
	if _, err := r.FindAccountByID(ctx, providerName, accountID); err != nil {
		return nil, err
	}
	results := make([]AccountTestResult, 0, len(r.accountTestResults))
	for _, result := range r.accountTestResults {
		if result.Provider == providerName && result.AccountID == accountID {
			results = append(results, result)
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		if !results[i].CheckedAt.Equal(results[j].CheckedAt) {
			return results[i].CheckedAt.After(results[j].CheckedAt)
		}
		return results[i].ID > results[j].ID
	})
	if limit < len(results) {
		results = results[:limit]
	}
	return results, nil
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
	r.captureIntent(ctx)
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

func (r *memoryRepo) SyncAccountModels(ctx context.Context, providerName string, accountID int64, inputs []AccountModelInput, seenAt time.Time) ([]AccountModel, AccountModelSyncSummary, error) {
	r.captureIntent(ctx)
	normalized, err := normalizeAccountModelInputs(inputs)
	if err != nil {
		return nil, AccountModelSyncSummary{}, err
	}
	if _, err := r.FindAccountByID(ctx, providerName, accountID); err != nil {
		return nil, AccountModelSyncSummary{}, err
	}
	existing := r.accountModels[accountID]
	upstreamEnabled := map[string]bool{}
	manual := map[string]bool{}
	for _, row := range existing {
		if row.Source == AccountModelSourceUpstream {
			upstreamEnabled[row.Model] = row.Enabled
		}
		if row.Source == AccountModelSourceManual || row.Source == "" {
			manual[row.Model] = true
		}
	}

	now := time.Now()
	if seenAt.IsZero() {
		seenAt = now
	}
	seenAtUTC := seenAt.UTC()

	summary := AccountModelSyncSummary{Total: len(normalized)}
	merged := make([]AccountModel, 0, len(normalized)+len(manual))
	// First carry forward manual rows.
	for _, row := range existing {
		if row.Source == AccountModelSourceManual || row.Source == "" {
			merged = append(merged, row)
		}
	}
	// Process synced inputs.
	for _, input := range normalized {
		if manual[input.Model] {
			summary.SkippedManual++
			continue
		}
		enabled, ok := upstreamEnabled[input.Model]
		if ok {
			summary.Preserved++
		} else {
			enabled = false
			summary.New++
		}
		merged = append(merged, AccountModel{
			ID:         int64(len(merged) + 1),
			AccountID:  accountID,
			Provider:   providerName,
			Model:      input.Model,
			Enabled:    enabled,
			Source:     AccountModelSourceUpstream,
			LastSeenAt: &seenAtUTC,
			Metadata:   map[string]string{},
			CreatedAt:  now,
			UpdatedAt:  now,
		})
	}
	r.accountModels[accountID] = merged
	listed, err := r.ListAccountModels(ctx, providerName, accountID)
	return listed, summary, err
}

func (r *memoryRepo) ListExposedModelsForRoutingPools(ctx context.Context, providerName string, poolIDs []int64) ([]ExposedModel, error) {
	poolAccounts := map[int64]bool{}
	for _, poolID := range poolIDs {
		for _, poolAccount := range r.routingPoolAccounts[poolID] {
			poolAccounts[poolAccount.AccountID] = true
		}
	}

	available := map[string]bool{}
	now := time.Now()
	for _, account := range r.accounts {
		if !poolAccounts[account.ID] || account.Provider != providerName || !accountSchedulable(account, now) {
			continue
		}
		for _, accountModel := range r.accountModels[account.ID] {
			if accountModel.Provider == providerName && accountModel.Enabled {
				available[accountModel.Model] = true
			}
		}
	}

	exposed := []ExposedModel{}
	for model := range available {
		exposed = append(exposed, ExposedModel{ID: model, OwnedBy: "openai"})
	}
	sort.Slice(exposed, func(i, j int) bool { return exposed[i].ID < exposed[j].ID })
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

func (r *memoryRepo) FindRoutingPool(ctx context.Context, poolID int64) (RoutingPool, error) {
	pool, ok := r.routingPools[poolID]
	if !ok {
		return RoutingPool{}, ErrRoutingPoolNotFound
	}
	return pool, nil
}

func (r *memoryRepo) RoutingPoolHasAccounts(ctx context.Context, poolID int64) (bool, error) {
	return len(r.routingPoolAccounts[poolID]) > 0, nil
}

func (r *memoryRepo) ListAccountsForRoutingPool(ctx context.Context, providerName string, poolID int64, model string, excludedAccountIDs []int64, now time.Time) ([]Account, error) {
	excluded := map[int64]bool{}
	for _, id := range excludedAccountIDs {
		if id > 0 {
			excluded[id] = true
		}
	}
	poolAccounts := r.routingPoolAccounts[poolID]
	poolAccountIDs := map[int64]int{}
	for _, account := range poolAccounts {
		poolAccountIDs[account.AccountID] = account.Priority
	}
	accounts, err := r.ListAccounts(ctx, providerName)
	if err != nil {
		return nil, err
	}
	eligible := []Account{}
	for _, account := range accounts {
		poolPriority, inPool := poolAccountIDs[account.ID]
		if !inPool || excluded[account.ID] || !accountSchedulable(account, now) {
			continue
		}
		if model != "" {
			hasModel := false
			for _, accountModel := range r.accountModels[account.ID] {
				if accountModel.Provider == providerName && accountModel.Model == model && accountModel.Enabled {
					hasModel = true
					break
				}
			}
			if !hasModel {
				continue
			}
		}
		account.GlobalPriority = account.Priority
		account.Priority = poolPriority
		account.RoutingPoolPriority = new(int)
		*account.RoutingPoolPriority = poolPriority
		eligible = append(eligible, account)
	}
	sortRoutingPoolAccounts(eligible)
	return eligible, nil
}

func (r *memoryRepo) ListRoutingPoolAccounts(ctx context.Context, providerName string, poolID int64) ([]Account, error) {
	poolAccountPriority := map[int64]int{}
	for _, account := range r.routingPoolAccounts[poolID] {
		poolAccountPriority[account.AccountID] = account.Priority
	}
	accounts, err := r.ListAccounts(ctx, providerName)
	if err != nil {
		return nil, err
	}
	members := []Account{}
	for _, account := range accounts {
		priority, ok := poolAccountPriority[account.ID]
		if !ok {
			continue
		}
		account.GlobalPriority = account.Priority
		account.Priority = priority
		account.RoutingPoolPriority = new(int)
		*account.RoutingPoolPriority = priority
		members = append(members, account)
	}
	sortRoutingPoolAccounts(members)
	return members, nil
}

func sortRoutingPoolAccounts(accounts []Account) {
	sort.SliceStable(accounts, func(i, j int) bool {
		if selectionPriority(accounts[i]) != selectionPriority(accounts[j]) {
			return selectionPriority(accounts[i]) < selectionPriority(accounts[j])
		}
		if globalAccountPriority(accounts[i]) != globalAccountPriority(accounts[j]) {
			return globalAccountPriority(accounts[i]) < globalAccountPriority(accounts[j])
		}
		if normalizedLoadFactor(accounts[i].LoadFactor) != normalizedLoadFactor(accounts[j].LoadFactor) {
			return normalizedLoadFactor(accounts[i].LoadFactor) > normalizedLoadFactor(accounts[j].LoadFactor)
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
}

func (r *memoryRepo) FindSessionBinding(ctx context.Context, providerName string, model string, sessionID string) (SessionBinding, error) {
	binding, ok := r.sessionBindings[sessionBindingKey(providerName, model, sessionID)]
	if !ok {
		return SessionBinding{}, ErrSessionBindingNotFound
	}
	return binding, nil
}

func (r *memoryRepo) UpsertSessionBinding(ctx context.Context, providerName string, model string, sessionID string, accountID int64) error {
	now := time.Now()
	key := sessionBindingKey(providerName, model, sessionID)
	binding := r.sessionBindings[key]
	if binding.ID == 0 {
		binding.ID = int64(len(r.sessionBindings) + 1)
		binding.CreatedAt = now
	}
	binding.Provider = providerName
	binding.Model = model
	binding.SessionID = sessionID
	binding.AccountID = accountID
	binding.LastUsedAt = now
	binding.UpdatedAt = now
	r.sessionBindings[key] = binding
	return nil
}

func (r *memoryRepo) FindSessionBindingInRoutingPool(ctx context.Context, providerName string, routingPoolID int64, model string, sessionID string) (SessionBinding, error) {
	binding, ok := r.sessionBindings[routingPoolSessionBindingKey(providerName, routingPoolID, model, sessionID)]
	if !ok {
		return SessionBinding{}, ErrSessionBindingNotFound
	}
	return binding, nil
}

func (r *memoryRepo) UpsertSessionBindingInRoutingPool(ctx context.Context, providerName string, routingPoolID int64, model string, sessionID string, accountID int64) error {
	now := time.Now()
	key := routingPoolSessionBindingKey(providerName, routingPoolID, model, sessionID)
	binding := r.sessionBindings[key]
	if binding.ID == 0 {
		binding.ID = int64(len(r.sessionBindings) + 1)
		binding.CreatedAt = now
	}
	binding.Provider = providerName
	binding.Model = model
	binding.SessionID = sessionID
	binding.AccountID = accountID
	binding.LastUsedAt = now
	binding.UpdatedAt = now
	r.sessionBindings[key] = binding
	return nil
}

func sessionBindingKey(providerName string, model string, sessionID string) string {
	return providerName + "\x00" + model + "\x00" + sessionID
}

func routingPoolSessionBindingKey(providerName string, routingPoolID int64, model string, sessionID string) string {
	return providerName + "\x00" + strconv.FormatInt(routingPoolID, 10) + "\x00" + model + "\x00" + sessionID
}

func (r *memoryRepo) CreateState(ctx context.Context, state OAuthState) error {
	r.captureIntent(ctx)
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
	probeErr    error
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

type captureProbeOAuthClient struct {
	probe           probeResult
	probes          []probeResult
	gotConfig       Config
	gotAccessToken  string
	gotAccessTokens []string
}

type captureAccountModelProber struct {
	result   modelProbeResult
	selected SelectedAccount
	model    string
	calls    int
}

type captureAccountTestRequestLogger struct {
	entries []AccountTestRequestLog
	err     error
}

func (l *captureAccountTestRequestLogger) CreateAccountTestRequestLog(_ context.Context, entry AccountTestRequestLog) error {
	l.entries = append(l.entries, entry)
	return l.err
}

func (p *captureAccountModelProber) ProbeAccountModel(_ context.Context, _ Config, selected SelectedAccount, model string) modelProbeResult {
	p.calls++
	p.selected = selected
	p.model = model
	return p.result
}

func (c *captureProbeOAuthClient) ExchangeCode(ctx context.Context, cfg Config, code string) (TokenResponse, error) {
	return TokenResponse{}, errors.New("unexpected exchange")
}

func (c *captureProbeOAuthClient) RefreshToken(ctx context.Context, cfg Config, refreshToken string) (TokenResponse, error) {
	return TokenResponse{}, errors.New("unexpected refresh")
}

func (c *captureProbeOAuthClient) ProbeAccountStatus(ctx context.Context, cfg Config, accessToken string) (probeResult, error) {
	c.gotConfig = cfg
	c.gotAccessToken = accessToken
	c.gotAccessTokens = append(c.gotAccessTokens, accessToken)
	if len(c.probes) > 0 {
		probe := c.probes[0]
		c.probes = c.probes[1:]
		if probe.statusCode == 0 {
			return probeResult{statusCode: http.StatusOK}, nil
		}
		return probe, nil
	}
	if c.probe.statusCode == 0 {
		return probeResult{statusCode: http.StatusOK}, nil
	}
	return c.probe, nil
}

type captureRefreshOAuthClient struct {
	refresh   TokenResponse
	gotConfig Config
	calls     int
}

func (c *captureRefreshOAuthClient) ExchangeCode(ctx context.Context, cfg Config, code string) (TokenResponse, error) {
	return TokenResponse{}, errors.New("unexpected exchange")
}

func (c *captureRefreshOAuthClient) RefreshToken(ctx context.Context, cfg Config, refreshToken string) (TokenResponse, error) {
	c.calls++
	c.gotConfig = cfg
	return c.refresh, nil
}

func (c fakeOAuthClient) RefreshToken(ctx context.Context, cfg Config, refreshToken string) (TokenResponse, error) {
	if c.refreshErr != nil {
		return TokenResponse{}, c.refreshErr
	}
	return c.refresh, nil
}

func (c fakeOAuthClient) ProbeAccountStatus(ctx context.Context, cfg Config, accessToken string) (probeResult, error) {
	if c.probeErr != nil {
		return probeResult{}, c.probeErr
	}
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

func mustEncrypt(t *testing.T, encryptionSecret string, kind secret.SecretKind, value string) string {
	t.Helper()
	keyring, err := secret.NewKeyring(secret.EncryptionKey{ID: secret.DefaultEncryptionKeyID, Secret: encryptionSecret}, nil)
	if err != nil {
		t.Fatalf("NewKeyring returned error: %v", err)
	}
	encrypted, err := keyring.EncryptStringFor(kind, value)
	if err != nil {
		t.Fatalf("EncryptString returned error: %v", err)
	}
	return encrypted
}

func decryptForTest(t *testing.T, encryptionSecret string, kind secret.SecretKind, value string) (string, error) {
	t.Helper()
	keyring, err := secret.NewKeyring(secret.EncryptionKey{ID: secret.DefaultEncryptionKeyID, Secret: encryptionSecret}, nil)
	if err != nil {
		t.Fatalf("NewKeyring returned error: %v", err)
	}
	return keyring.DecryptStringFor(kind, value)
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

func exposedModelIDs(models []ExposedModel) []string {
	values := make([]string, 0, len(models))
	for _, model := range models {
		values = append(values, model.ID)
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
		EncryptedAccessToken:  mustEncrypt(t, "encryption-secret", secret.SecretKindOAuthAccessToken, accessToken),
		EncryptedRefreshToken: mustEncrypt(t, "encryption-secret", secret.SecretKindOAuthRefreshToken, refreshToken),
		AccessTokenExpiresAt:  &expiresAt,
		Enabled:               enabled,
		Priority:              priority,
		LoadFactor:            1,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
}

func ptrInt64(value int64) *int64 {
	return &value
}

func valueOrDefaultInt64(value, fallback int64) int64 {
	if value == 0 {
		return fallback
	}
	return value
}
