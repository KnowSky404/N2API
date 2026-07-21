package store

import (
	"context"
	"strings"
	"testing"

	"github.com/KnowSky404/N2API/backend/internal/secret"
)

func TestEncryptionInventoryQueryCoversEveryCredentialClass(t *testing.T) {
	for _, want := range []string{
		"oauth_states", "encrypted_code_verifier", string(secret.SecretKindOAuthCodeVerifier),
		"provider_account_credentials", "encrypted_access_token", string(secret.SecretKindOAuthAccessToken),
		"encrypted_refresh_token", string(secret.SecretKindOAuthRefreshToken),
		"encrypted_id_token", string(secret.SecretKindOAuthIDToken),
		"encrypted_api_key", string(secret.SecretKindProviderAPIKey),
		"encrypted_proxy_url", string(secret.SecretKindProviderProxyURL),
		"client_api_keys", "encrypted_secret", string(secret.SecretKindClientAPIKey),
		"ORDER BY class_order, row_id",
	} {
		if !strings.Contains(encryptionInventoryQuery, want) {
			t.Fatalf("inventory query missing %q", want)
		}
	}
}

func TestEncryptionInventoryRepositoryListsAllCredentialClasses(t *testing.T) {
	adminRepo := newTestAdminRepository(t)
	ctx := context.Background()

	var stateID int64
	err := adminRepo.pool.QueryRow(ctx, `
		INSERT INTO oauth_states (provider, state_hash, redirect_after, expires_at, encrypted_code_verifier, code_verifier_hash)
		VALUES ('openai', 'inventory-state', '/', now() - interval '1 hour', 'code-verifier-ciphertext', 'verifier-hash')
		RETURNING id
	`).Scan(&stateID)
	if err != nil {
		t.Fatalf("insert oauth state: %v", err)
	}

	var accountID int64
	err = adminRepo.pool.QueryRow(ctx, `
		INSERT INTO provider_accounts (provider, account_type, name, enabled)
		VALUES ('openai', 'api_upstream', 'inventory-account', false)
		RETURNING id
	`).Scan(&accountID)
	if err != nil {
		t.Fatalf("insert provider account: %v", err)
	}
	_, err = adminRepo.pool.Exec(ctx, `
		INSERT INTO provider_account_credentials (
			account_id, credential_type, encrypted_access_token, encrypted_refresh_token,
			encrypted_id_token, encrypted_api_key, encrypted_proxy_url
		) VALUES ($1, 'api_key', 'access-ciphertext', 'refresh-ciphertext', 'id-ciphertext', 'api-ciphertext', 'proxy-ciphertext')
	`, accountID)
	if err != nil {
		t.Fatalf("insert provider credentials: %v", err)
	}

	var keyID int64
	err = adminRepo.pool.QueryRow(ctx, `
		INSERT INTO client_api_keys (name, key_hash, prefix, encrypted_secret, revoked_at)
		VALUES ('inventory-key', 'inventory-key-hash', 'n2api_', 'client-ciphertext', now())
		RETURNING id
	`).Scan(&keyID)
	if err != nil {
		t.Fatalf("insert client key: %v", err)
	}

	values, err := NewEncryptionInventoryRepository(adminRepo.pool).ListEncryptedValues(ctx)
	if err != nil {
		t.Fatalf("ListEncryptedValues returned error: %v", err)
	}
	if len(values) != 7 {
		t.Fatalf("value count = %d, want 7: %+v", len(values), values)
	}
	if values[0].RowID != stateID || values[0].Type != secret.SecretKindOAuthCodeVerifier {
		t.Fatalf("first value = %+v, want oauth state %d", values[0], stateID)
	}
	if values[6].RowID != keyID || values[6].Type != secret.SecretKindClientAPIKey {
		t.Fatalf("last value = %+v, want client key %d", values[6], keyID)
	}
}
