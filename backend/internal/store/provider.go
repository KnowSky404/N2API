package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/provider"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ProviderRepository struct {
	pool *pgxpool.Pool
}

func NewProviderRepository(pool *pgxpool.Pool) *ProviderRepository {
	return &ProviderRepository{pool: pool}
}

const providerAccountColumns = `
	a.id, a.provider, a.account_type, a.subject, a.name, a.display_name, a.enabled, a.priority,
	a.last_used_at, a.last_error, a.last_error_at, a.status, a.status_reason, a.fingerprint_hash,
	a.user_agent_hash, a.ip_hash, a.failure_count, a.circuit_open_until, a.rate_limited_until,
	a.created_at, a.updated_at, c.credential_type, c.encrypted_access_token,
	c.encrypted_refresh_token, c.encrypted_id_token, c.access_token_expires_at,
	c.last_refresh_at, c.last_refresh_error, c.last_refresh_error_at, c.encrypted_api_key,
	c.base_url, c.metadata
`

const providerAccountModelColumns = `
	id, account_id, provider, model, enabled, source, last_seen_at, last_error, metadata, created_at, updated_at
`

func scanProviderAccount(row pgx.Row) (provider.Account, error) {
	var account provider.Account
	err := row.Scan(
		&account.ID,
		&account.Provider,
		&account.AccountType,
		&account.Subject,
		&account.Name,
		&account.DisplayName,
		&account.Enabled,
		&account.Priority,
		&account.LastUsedAt,
		&account.LastError,
		&account.LastErrorAt,
		&account.Status,
		&account.StatusReason,
		&account.FingerprintHash,
		&account.UserAgentHash,
		&account.IPHash,
		&account.FailureCount,
		&account.CircuitOpenUntil,
		&account.RateLimitedUntil,
		&account.CreatedAt,
		&account.UpdatedAt,
		&account.Credential.CredentialType,
		&account.Credential.EncryptedAccessToken,
		&account.Credential.EncryptedRefreshToken,
		&account.Credential.EncryptedIDToken,
		&account.Credential.AccessTokenExpiresAt,
		&account.Credential.LastRefreshAt,
		&account.Credential.LastRefreshError,
		&account.Credential.LastRefreshErrorAt,
		&account.Credential.EncryptedAPIKey,
		&account.Credential.BaseURL,
		&account.Credential.Metadata,
	)
	if err == nil {
		syncAccountLegacyFields(&account)
	}
	return account, err
}

func scanProviderAccountModel(row pgx.Row) (provider.AccountModel, error) {
	var model provider.AccountModel
	err := row.Scan(
		&model.ID,
		&model.AccountID,
		&model.Provider,
		&model.Model,
		&model.Enabled,
		&model.Source,
		&model.LastSeenAt,
		&model.LastError,
		&model.Metadata,
		&model.CreatedAt,
		&model.UpdatedAt,
	)
	return model, err
}

func normalizeAccountForSave(account *provider.Account) {
	if strings.TrimSpace(account.AccountType) == "" {
		account.AccountType = provider.AccountTypeCodexOAuth
	}
	if strings.TrimSpace(account.Credential.CredentialType) == "" {
		switch account.AccountType {
		case provider.AccountTypeAPIUpstream:
			account.Credential.CredentialType = provider.CredentialTypeAPIKey
		default:
			account.Credential.CredentialType = provider.CredentialTypeOAuthToken
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
	if account.Metadata == nil {
		account.Metadata = account.Credential.Metadata
	}
	syncAccountLegacyFields(account)
}

func syncAccountLegacyFields(account *provider.Account) {
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

func upsertProviderAccountCredential(ctx context.Context, tx pgx.Tx, account provider.Account) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO provider_account_credentials (
			account_id, credential_type, encrypted_access_token, encrypted_refresh_token,
			encrypted_id_token, access_token_expires_at, last_refresh_at, last_refresh_error,
			last_refresh_error_at, encrypted_api_key, base_url, metadata, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, now())
		ON CONFLICT (account_id)
		DO UPDATE SET
			credential_type = EXCLUDED.credential_type,
			encrypted_access_token = EXCLUDED.encrypted_access_token,
			encrypted_refresh_token = EXCLUDED.encrypted_refresh_token,
			encrypted_id_token = EXCLUDED.encrypted_id_token,
			access_token_expires_at = EXCLUDED.access_token_expires_at,
			last_refresh_at = EXCLUDED.last_refresh_at,
			last_refresh_error = EXCLUDED.last_refresh_error,
			last_refresh_error_at = EXCLUDED.last_refresh_error_at,
			encrypted_api_key = EXCLUDED.encrypted_api_key,
			base_url = EXCLUDED.base_url,
			metadata = provider_account_credentials.metadata || EXCLUDED.metadata,
			updated_at = now()
	`, account.ID,
		account.Credential.CredentialType,
		account.Credential.EncryptedAccessToken,
		account.Credential.EncryptedRefreshToken,
		account.Credential.EncryptedIDToken,
		account.Credential.AccessTokenExpiresAt,
		account.Credential.LastRefreshAt,
		account.Credential.LastRefreshError,
		account.Credential.LastRefreshErrorAt,
		account.Credential.EncryptedAPIKey,
		account.Credential.BaseURL,
		metadataJSON(account.Credential.Metadata),
	)
	return err
}

func (r *ProviderRepository) ListAccounts(ctx context.Context, providerName string) ([]provider.Account, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+providerAccountColumns+`
		FROM provider_accounts a
		JOIN provider_account_credentials c ON c.account_id = a.id
		WHERE a.provider = $1
		ORDER BY
			a.priority ASC,
			(a.last_error_at IS NOT NULL) ASC,
			a.last_used_at ASC NULLS FIRST,
			a.id ASC
	`, providerName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []provider.Account
	for rows.Next() {
		account, err := scanProviderAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

func (r *ProviderRepository) HasEnabledAccounts(ctx context.Context, providerName string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM provider_accounts
			WHERE provider = $1
				AND enabled = true
		)
	`, providerName).Scan(&exists)
	return exists, err
}

func (r *ProviderRepository) FindAccount(ctx context.Context, providerName string) (provider.Account, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT `+providerAccountColumns+`
		FROM provider_accounts a
		JOIN provider_account_credentials c ON c.account_id = a.id
		WHERE a.provider = $1
		ORDER BY
			a.priority ASC,
			(a.last_error_at IS NOT NULL) ASC,
			a.last_used_at ASC NULLS FIRST,
			a.id ASC
		LIMIT 1
	`, providerName)
	account, err := scanProviderAccount(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return provider.Account{}, provider.ErrNotConnected
	}
	if err != nil {
		return provider.Account{}, err
	}
	return account, nil
}

func (r *ProviderRepository) FindAccountByID(ctx context.Context, providerName string, id int64) (provider.Account, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT `+providerAccountColumns+`
		FROM provider_accounts a
		JOIN provider_account_credentials c ON c.account_id = a.id
		WHERE a.provider = $1
			AND a.id = $2
	`, providerName, id)
	account, err := scanProviderAccount(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return provider.Account{}, provider.ErrNotConnected
	}
	if err != nil {
		return provider.Account{}, err
	}
	return account, nil
}

func (r *ProviderRepository) FindAccountByIdentity(ctx context.Context, providerName string, identities provider.AccountIdentities) (provider.Account, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT `+providerAccountColumns+`
		FROM provider_accounts a
		JOIN provider_account_credentials c ON c.account_id = a.id
		WHERE a.provider = $1
			AND (
				($2 <> '' AND c.metadata->>'chatgpt_account_id' = $2)
				OR ($3 <> '' AND c.metadata->>'chatgpt_user_id' = $3)
				OR ($4 <> '' AND lower(c.metadata->>'email') = $4)
				OR ($5 <> '' AND c.metadata->>'access_token_sha256' = $5)
			)
		ORDER BY a.id ASC
		LIMIT 1
	`, providerName, identities.ChatGPTAccountID, identities.ChatGPTUserID, identities.Email, identities.AccessTokenSHA256)
	account, err := scanProviderAccount(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return provider.Account{}, provider.ErrNotConnected
	}
	if err != nil {
		return provider.Account{}, err
	}
	return account, nil
}

func (r *ProviderRepository) SaveAccount(ctx context.Context, account provider.Account) (provider.Account, error) {
	normalizeAccountForSave(&account)
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return provider.Account{}, err
	}
	defer tx.Rollback(ctx)

	if account.ID > 0 {
		var updatedID int64
		err := tx.QueryRow(ctx, `
			UPDATE provider_accounts
			SET
				account_type = $3,
				subject = $4,
				name = $5,
				display_name = $6,
				enabled = $7,
				priority = CASE WHEN $8 = 0 THEN 100 ELSE $8 END,
				last_error = '',
				last_error_at = NULL,
				status = COALESCE(NULLIF($9, ''), 'active'),
				status_reason = $10,
				fingerprint_hash = $11,
				user_agent_hash = $12,
				ip_hash = $13,
				failure_count = $14,
				circuit_open_until = $15,
				rate_limited_until = $16,
				updated_at = now()
			WHERE provider = $1
				AND id = $2
			RETURNING id
		`, account.Provider,
			account.ID,
			account.AccountType,
			account.Subject,
			account.Name,
			account.DisplayName,
			account.Enabled,
			account.Priority,
			account.Status,
			account.StatusReason,
			account.FingerprintHash,
			account.UserAgentHash,
			account.IPHash,
			account.FailureCount,
			account.CircuitOpenUntil,
			account.RateLimitedUntil,
		).Scan(&updatedID)
		if errors.Is(err, pgx.ErrNoRows) {
			return provider.Account{}, provider.ErrNotConnected
		}
		if err != nil {
			return provider.Account{}, err
		}
		if err := upsertProviderAccountCredential(ctx, tx, account); err != nil {
			return provider.Account{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return provider.Account{}, err
		}
		saved, err := r.FindAccountByID(ctx, account.Provider, account.ID)
		if err != nil {
			return provider.Account{}, err
		}
		return saved, nil
	}

	var savedID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO provider_accounts (
			provider, account_type, subject, name, display_name, enabled, priority, last_error,
			status, status_reason, fingerprint_hash, user_agent_hash, ip_hash, failure_count,
			circuit_open_until, rate_limited_until, updated_at
		)
		VALUES (
			$1, $2, $3, $4, $5,
			$6,
			CASE WHEN $7 = 0 THEN 100 ELSE $7 END,
			'', COALESCE(NULLIF($8, ''), 'active'), $9, $10, $11, $12, $13,
			$14, $15, now()
		)
		ON CONFLICT (provider, account_type, subject) WHERE subject <> ''
		DO UPDATE SET
			name = EXCLUDED.name,
			display_name = EXCLUDED.display_name,
			last_error = '',
			last_error_at = NULL,
			status = EXCLUDED.status,
			status_reason = EXCLUDED.status_reason,
			fingerprint_hash = EXCLUDED.fingerprint_hash,
			user_agent_hash = EXCLUDED.user_agent_hash,
			ip_hash = EXCLUDED.ip_hash,
			failure_count = EXCLUDED.failure_count,
			circuit_open_until = EXCLUDED.circuit_open_until,
			rate_limited_until = EXCLUDED.rate_limited_until,
			updated_at = now()
		RETURNING id
	`, account.Provider,
		account.AccountType,
		account.Subject,
		account.Name,
		account.DisplayName,
		account.Enabled,
		account.Priority,
		account.Status,
		account.StatusReason,
		account.FingerprintHash,
		account.UserAgentHash,
		account.IPHash,
		account.FailureCount,
		account.CircuitOpenUntil,
		account.RateLimitedUntil,
	).Scan(&savedID)
	if err != nil {
		return provider.Account{}, err
	}
	account.ID = savedID
	if err := upsertProviderAccountCredential(ctx, tx, account); err != nil {
		return provider.Account{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return provider.Account{}, err
	}
	saved, err := r.FindAccountByID(ctx, account.Provider, savedID)
	if err != nil {
		return provider.Account{}, err
	}
	return saved, nil
}

func (r *ProviderRepository) UpdateAccount(ctx context.Context, providerName string, id int64, update provider.AccountUpdate) (provider.Account, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE provider_accounts
		SET
			enabled = COALESCE($3, enabled),
			priority = COALESCE($4, priority),
			last_error = CASE WHEN $5 THEN '' ELSE last_error END,
			last_error_at = CASE WHEN $5 THEN NULL ELSE last_error_at END,
			status = CASE WHEN $5 THEN 'active' ELSE status END,
			status_reason = CASE WHEN $5 THEN '' ELSE status_reason END,
			failure_count = CASE WHEN $5 THEN 0 ELSE failure_count END,
			circuit_open_until = CASE WHEN $5 THEN NULL ELSE circuit_open_until END,
			rate_limited_until = CASE WHEN $5 THEN NULL ELSE rate_limited_until END,
			updated_at = now()
		WHERE provider = $1
			AND id = $2
		RETURNING id
	`, providerName, id, update.Enabled, update.Priority, update.ClearStatus)
	var updatedID int64
	err := row.Scan(&updatedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return provider.Account{}, provider.ErrNotConnected
	}
	if err != nil {
		return provider.Account{}, err
	}
	return r.FindAccountByID(ctx, providerName, updatedID)
}

func (r *ProviderRepository) DeleteAccount(ctx context.Context, providerName string, id int64) error {
	var deletedID int64
	err := r.pool.QueryRow(ctx, `
		DELETE FROM provider_accounts
		WHERE provider = $1
			AND id = $2
		RETURNING id
	`, providerName, id).Scan(&deletedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return provider.ErrNotConnected
	}
	return err
}

func (r *ProviderRepository) DeleteAccounts(ctx context.Context, providerName string) error {
	_, err := r.pool.Exec(ctx, `
		DELETE FROM provider_accounts
		WHERE provider = $1
	`, providerName)
	return err
}

func (r *ProviderRepository) MarkAccountUsed(ctx context.Context, providerName string, id int64, usedAt time.Time) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var updatedID int64
	err = tx.QueryRow(ctx, `
		UPDATE provider_accounts
		SET
			last_used_at = $3,
			last_error = '',
			last_error_at = NULL,
			status = 'active',
			status_reason = '',
			failure_count = 0,
			circuit_open_until = NULL,
			rate_limited_until = NULL,
			updated_at = now()
		WHERE provider = $1
			AND id = $2
		RETURNING id
	`, providerName, id, usedAt).Scan(&updatedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return provider.ErrNotConnected
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		UPDATE provider_account_credentials
		SET
			last_refresh_error = '',
			last_refresh_error_at = NULL,
			updated_at = now()
		WHERE account_id = $1
	`, id)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *ProviderRepository) MarkAccountError(ctx context.Context, providerName string, id int64, message string, at time.Time) error {
	var updatedID int64
	err := r.pool.QueryRow(ctx, `
		UPDATE provider_accounts
		SET
			last_error = $3,
			last_error_at = $4,
			updated_at = now()
		WHERE provider = $1
			AND id = $2
		RETURNING id
	`, providerName, id, message, at).Scan(&updatedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return provider.ErrNotConnected
	}
	return err
}

func (r *ProviderRepository) RecordRefreshFailure(ctx context.Context, providerName string, id int64, message string, at time.Time, openUntil *time.Time) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var updatedID int64
	err = tx.QueryRow(ctx, `
		UPDATE provider_accounts
		SET
			failure_count = failure_count + 1,
			status = CASE WHEN $3::timestamptz IS NULL THEN status ELSE 'circuit_open' END,
			status_reason = CASE WHEN $3::timestamptz IS NULL THEN status_reason ELSE $4 END,
			circuit_open_until = COALESCE($3, circuit_open_until),
			updated_at = now()
		WHERE provider = $1
			AND id = $2
		RETURNING id
	`, providerName, id, openUntil, message).Scan(&updatedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return provider.ErrNotConnected
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		UPDATE provider_account_credentials
		SET
			last_refresh_error = $2,
			last_refresh_error_at = $3,
			updated_at = now()
		WHERE account_id = $1
	`, id, message, at)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *ProviderRepository) RecordAccountStatus(ctx context.Context, providerName string, id int64, status, reason string, at time.Time, rateLimitedUntil, circuitOpenUntil *time.Time) error {
	var updatedID int64
	err := r.pool.QueryRow(ctx, `
		UPDATE provider_accounts
		SET
			status = $3,
			status_reason = $4,
			last_error = $4,
			last_error_at = $5,
			rate_limited_until = $6,
			circuit_open_until = $7,
			failure_count = CASE WHEN $3 = 'circuit_open' THEN failure_count + 1 ELSE failure_count END,
			updated_at = now()
		WHERE provider = $1
			AND id = $2
		RETURNING id
	`, providerName, id, status, reason, at, rateLimitedUntil, circuitOpenUntil).Scan(&updatedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return provider.ErrNotConnected
	}
	return err
}

func (r *ProviderRepository) ListAccountModels(ctx context.Context, providerName string, accountID int64) ([]provider.AccountModel, error) {
	var exists int
	err := r.pool.QueryRow(ctx, `
		SELECT 1
		FROM provider_accounts
		WHERE provider = $1
			AND id = $2
	`, providerName, accountID).Scan(&exists)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, provider.ErrNotConnected
	}
	if err != nil {
		return nil, err
	}

	rows, err := r.pool.Query(ctx, `
		SELECT `+providerAccountModelColumns+`
		FROM provider_account_models
		WHERE provider = $1
			AND account_id = $2
		ORDER BY model ASC
	`, providerName, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	models := []provider.AccountModel{}
	for rows.Next() {
		model, err := scanProviderAccountModel(rows)
		if err != nil {
			return nil, err
		}
		models = append(models, model)
	}
	return models, rows.Err()
}

func (r *ProviderRepository) ReplaceAccountModels(ctx context.Context, providerName string, accountID int64, inputs []provider.AccountModelInput) ([]provider.AccountModel, error) {
	models, err := normalizeAccountModelInputs(inputs)
	if err != nil {
		return nil, err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var existingID int64
	err = tx.QueryRow(ctx, `
		SELECT id
		FROM provider_accounts
		WHERE provider = $1
			AND id = $2
		FOR UPDATE
	`, providerName, accountID).Scan(&existingID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, provider.ErrNotConnected
	}
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec(ctx, `
		DELETE FROM provider_account_models
		WHERE provider = $1
			AND account_id = $2
			AND source = $3
	`, providerName, accountID, provider.AccountModelSourceManual)
	if err != nil {
		return nil, err
	}

	for _, model := range models {
		_, err = tx.Exec(ctx, `
			INSERT INTO provider_account_models (
				account_id, provider, model, enabled, source, metadata, updated_at
			)
			VALUES ($1, $2, $3, $4, $5, '{}'::jsonb, now())
		`, accountID, providerName, model.Model, model.Enabled, provider.AccountModelSourceManual)
		if err != nil {
			return nil, err
		}
	}

	rows, err := tx.Query(ctx, `
		SELECT `+providerAccountModelColumns+`
		FROM provider_account_models
		WHERE provider = $1
			AND account_id = $2
		ORDER BY model ASC
	`, providerName, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	saved := []provider.AccountModel{}
	for rows.Next() {
		model, err := scanProviderAccountModel(rows)
		if err != nil {
			return nil, err
		}
		saved = append(saved, model)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return saved, nil
}

func (r *ProviderRepository) ListExposedModels(ctx context.Context, providerName string, allowedModels []string) ([]provider.ExposedModel, error) {
	allowed := normalizeAllowedModels(allowedModels)
	if len(allowed) == 0 {
		return []provider.ExposedModel{}, nil
	}

	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT m.model
		FROM provider_account_models m
		JOIN provider_accounts a ON a.id = m.account_id
			AND a.provider = m.provider
		WHERE m.provider = $1
			AND m.enabled = true
			AND m.model = ANY($2)
			AND a.enabled = true
			AND (
				a.status IN ('', 'active')
				OR (a.status = 'rate_limited' AND a.rate_limited_until IS NOT NULL AND a.rate_limited_until <= now())
				OR (a.status = 'circuit_open' AND a.circuit_open_until IS NOT NULL AND a.circuit_open_until <= now())
			)
			AND (a.rate_limited_until IS NULL OR a.rate_limited_until <= now())
			AND (a.circuit_open_until IS NULL OR a.circuit_open_until <= now())
	`, providerName, allowed)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	available := map[string]bool{}
	for rows.Next() {
		var model string
		if err := rows.Scan(&model); err != nil {
			return nil, err
		}
		available[model] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	exposed := []provider.ExposedModel{}
	for _, model := range allowed {
		if available[model] {
			exposed = append(exposed, provider.ExposedModel{ID: model, OwnedBy: "openai"})
		}
	}
	return exposed, nil
}

func (r *ProviderRepository) ListEligibleAccountsForModel(ctx context.Context, providerName string, model string, excludedAccountIDs []int64, now time.Time) ([]provider.Account, error) {
	model = strings.TrimSpace(model)
	if model == "" {
		return []provider.Account{}, nil
	}

	rows, err := r.pool.Query(ctx, `
		SELECT `+providerAccountColumns+`
		FROM provider_accounts a
		JOIN provider_account_credentials c ON c.account_id = a.id
		JOIN provider_account_models m ON m.account_id = a.id
			AND m.provider = a.provider
		WHERE a.provider = $1
			AND m.model = $2
			AND m.enabled = true
			AND a.enabled = true
			AND (
				a.status IN ('', 'active')
				OR (a.status = 'rate_limited' AND a.rate_limited_until IS NOT NULL AND a.rate_limited_until <= $3)
				OR (a.status = 'circuit_open' AND a.circuit_open_until IS NOT NULL AND a.circuit_open_until <= $3)
			)
			AND (a.rate_limited_until IS NULL OR a.rate_limited_until <= $3)
			AND (a.circuit_open_until IS NULL OR a.circuit_open_until <= $3)
			AND ($4::bigint[] IS NULL OR cardinality($4::bigint[]) = 0 OR NOT (a.id = ANY($4::bigint[])))
		ORDER BY
			a.priority ASC,
			(a.last_error_at IS NOT NULL) ASC,
			a.last_used_at ASC NULLS FIRST,
			a.id ASC
	`, providerName, model, now, excludedAccountIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	accounts := []provider.Account{}
	for rows.Next() {
		account, err := scanProviderAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

func (r *ProviderRepository) CreateState(ctx context.Context, state provider.OAuthState) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO oauth_states (
			provider, state_hash, redirect_after, expires_at, encrypted_code_verifier, code_verifier_hash,
			client_id, target_account_id, pending_account_name, pending_priority, pending_enabled,
			fingerprint_hash, user_agent_hash, ip_hash
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, 0), $9, $10, $11, $12, $13, $14)
	`, state.Provider, state.StateHash, state.RedirectAfter, state.ExpiresAt, state.EncryptedCodeVerifier, state.CodeVerifierHash, state.ClientID, state.TargetAccountID, state.PendingAccountName, state.PendingPriority, state.PendingEnabled, state.FingerprintHash, state.UserAgentHash, state.IPHash)
	return err
}

func (r *ProviderRepository) ClaimState(ctx context.Context, providerName, stateHash string, now time.Time) (provider.OAuthState, error) {
	var state provider.OAuthState
	err := r.pool.QueryRow(ctx, `
		UPDATE oauth_states
		SET consumed_at = $4
		WHERE provider = $1
			AND state_hash = $2
			AND expires_at > $3
			AND consumed_at IS NULL
		RETURNING provider, state_hash, redirect_after, expires_at, consumed_at, encrypted_code_verifier,
			code_verifier_hash, client_id, COALESCE(target_account_id, 0), pending_account_name,
			pending_priority, pending_enabled, fingerprint_hash, user_agent_hash, ip_hash
	`, providerName, stateHash, now, now).Scan(
		&state.Provider,
		&state.StateHash,
		&state.RedirectAfter,
		&state.ExpiresAt,
		&state.ConsumedAt,
		&state.EncryptedCodeVerifier,
		&state.CodeVerifierHash,
		&state.ClientID,
		&state.TargetAccountID,
		&state.PendingAccountName,
		&state.PendingPriority,
		&state.PendingEnabled,
		&state.FingerprintHash,
		&state.UserAgentHash,
		&state.IPHash,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return provider.OAuthState{}, provider.ErrInvalidState
	}
	if err != nil {
		return provider.OAuthState{}, err
	}
	return state, nil
}

func normalizeAccountModelInputs(inputs []provider.AccountModelInput) ([]provider.AccountModelInput, error) {
	models := make([]provider.AccountModelInput, 0, len(inputs))
	seen := map[string]bool{}
	for _, input := range inputs {
		model := strings.TrimSpace(input.Model)
		if model == "" {
			continue
		}
		if len(model) > 128 {
			return nil, provider.ErrInvalidInput
		}
		if seen[model] {
			continue
		}
		seen[model] = true
		models = append(models, provider.AccountModelInput{
			Model:   model,
			Enabled: input.Enabled,
		})
		if len(models) > 100 {
			return nil, provider.ErrInvalidInput
		}
	}
	return models, nil
}

func normalizeAllowedModels(inputs []string) []string {
	models := make([]string, 0, len(inputs))
	seen := map[string]bool{}
	for _, input := range inputs {
		model := strings.TrimSpace(input)
		if model == "" || seen[model] {
			continue
		}
		seen[model] = true
		models = append(models, model)
	}
	return models
}

func metadataJSON(metadata map[string]string) []byte {
	if metadata == nil {
		metadata = map[string]string{}
	}
	payload, err := json.Marshal(metadata)
	if err != nil {
		return []byte(`{}`)
	}
	return payload
}
