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
	id, provider, subject, name, display_name, encrypted_access_token, encrypted_refresh_token,
	encrypted_id_token, access_token_expires_at, last_refresh_at, enabled, priority, last_used_at,
	last_error, last_error_at, metadata, status, status_reason, fingerprint_hash, user_agent_hash,
	ip_hash, failure_count, circuit_open_until, rate_limited_until, last_refresh_error,
	last_refresh_error_at, created_at, updated_at
`

const providerAccountColumnsFromAlias = `
	a.id, a.provider, a.subject, a.name, a.display_name, a.encrypted_access_token, a.encrypted_refresh_token,
	a.encrypted_id_token, a.access_token_expires_at, a.last_refresh_at, a.enabled, a.priority, a.last_used_at,
	a.last_error, a.last_error_at, a.metadata, a.status, a.status_reason, a.fingerprint_hash, a.user_agent_hash,
	a.ip_hash, a.failure_count, a.circuit_open_until, a.rate_limited_until, a.last_refresh_error,
	a.last_refresh_error_at, a.created_at, a.updated_at
`

const providerAccountModelColumns = `
	id, account_id, provider, model, enabled, source, last_seen_at, last_error, metadata, created_at, updated_at
`

func scanProviderAccount(row pgx.Row) (provider.Account, error) {
	var account provider.Account
	err := row.Scan(
		&account.ID,
		&account.Provider,
		&account.Subject,
		&account.Name,
		&account.DisplayName,
		&account.EncryptedAccessToken,
		&account.EncryptedRefreshToken,
		&account.EncryptedIDToken,
		&account.AccessTokenExpiresAt,
		&account.LastRefreshAt,
		&account.Enabled,
		&account.Priority,
		&account.LastUsedAt,
		&account.LastError,
		&account.LastErrorAt,
		&account.Metadata,
		&account.Status,
		&account.StatusReason,
		&account.FingerprintHash,
		&account.UserAgentHash,
		&account.IPHash,
		&account.FailureCount,
		&account.CircuitOpenUntil,
		&account.RateLimitedUntil,
		&account.LastRefreshError,
		&account.LastRefreshErrorAt,
		&account.CreatedAt,
		&account.UpdatedAt,
	)
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

func (r *ProviderRepository) ListAccounts(ctx context.Context, providerName string) ([]provider.Account, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+providerAccountColumns+`
		FROM oauth_accounts
		WHERE provider = $1
		ORDER BY
			priority ASC,
			(last_error_at IS NOT NULL) ASC,
			last_used_at ASC NULLS FIRST,
			id ASC
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

func (r *ProviderRepository) FindAccount(ctx context.Context, providerName string) (provider.Account, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT `+providerAccountColumns+`
		FROM oauth_accounts
		WHERE provider = $1
		ORDER BY
			priority ASC,
			(last_error_at IS NOT NULL) ASC,
			last_used_at ASC NULLS FIRST,
			id ASC
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
		FROM oauth_accounts
		WHERE provider = $1
			AND id = $2
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
		FROM oauth_accounts
		WHERE provider = $1
			AND (
				($2 <> '' AND metadata->>'chatgpt_account_id' = $2)
				OR ($3 <> '' AND metadata->>'chatgpt_user_id' = $3)
				OR ($4 <> '' AND lower(metadata->>'email') = $4)
				OR ($5 <> '' AND metadata->>'access_token_sha256' = $5)
			)
		ORDER BY id ASC
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
	if account.ID > 0 {
		row := r.pool.QueryRow(ctx, `
			UPDATE oauth_accounts
			SET
				subject = $3,
				name = $4,
				display_name = $5,
				encrypted_access_token = $6,
				encrypted_refresh_token = $7,
				encrypted_id_token = $8,
				access_token_expires_at = $9,
				last_refresh_at = $10,
				enabled = $11,
				priority = CASE WHEN $12 = 0 THEN 100 ELSE $12 END,
				last_error = '',
				last_error_at = NULL,
				metadata = oauth_accounts.metadata || $13,
				status = COALESCE(NULLIF($14, ''), 'active'),
				status_reason = $15,
				fingerprint_hash = $16,
				user_agent_hash = $17,
				ip_hash = $18,
				failure_count = $19,
				circuit_open_until = $20,
				rate_limited_until = $21,
				last_refresh_error = $22,
				last_refresh_error_at = $23,
				updated_at = now()
			WHERE provider = $1
				AND id = $2
			RETURNING `+providerAccountColumns+`
		`, account.Provider,
			account.ID,
			account.Subject,
			account.Name,
			account.DisplayName,
			account.EncryptedAccessToken,
			account.EncryptedRefreshToken,
			account.EncryptedIDToken,
			account.AccessTokenExpiresAt,
			account.LastRefreshAt,
			account.Enabled,
			account.Priority,
			metadataJSON(account.Metadata),
			account.Status,
			account.StatusReason,
			account.FingerprintHash,
			account.UserAgentHash,
			account.IPHash,
			account.FailureCount,
			account.CircuitOpenUntil,
			account.RateLimitedUntil,
			account.LastRefreshError,
			account.LastRefreshErrorAt,
		)
		saved, err := scanProviderAccount(row)
		if errors.Is(err, pgx.ErrNoRows) {
			return provider.Account{}, provider.ErrNotConnected
		}
		if err != nil {
			return provider.Account{}, err
		}
		return saved, nil
	}

	row := r.pool.QueryRow(ctx, `
		INSERT INTO oauth_accounts (
			provider, subject, name, display_name, encrypted_access_token, encrypted_refresh_token,
			encrypted_id_token, access_token_expires_at, last_refresh_at, enabled, priority, last_error,
			metadata, status, status_reason, fingerprint_hash, user_agent_hash, ip_hash, failure_count,
			circuit_open_until, rate_limited_until, last_refresh_error, last_refresh_error_at, updated_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9,
			$10,
			CASE WHEN $11 = 0 THEN 100 ELSE $11 END,
			'', $12, COALESCE(NULLIF($13, ''), 'active'), $14, $15, $16, $17, $18,
			$19, $20, $21, $22, now()
		)
		ON CONFLICT (provider, subject)
		DO UPDATE SET
			name = EXCLUDED.name,
			display_name = EXCLUDED.display_name,
			encrypted_access_token = EXCLUDED.encrypted_access_token,
			encrypted_refresh_token = EXCLUDED.encrypted_refresh_token,
			encrypted_id_token = EXCLUDED.encrypted_id_token,
			access_token_expires_at = EXCLUDED.access_token_expires_at,
			last_refresh_at = EXCLUDED.last_refresh_at,
			last_error = '',
			last_error_at = NULL,
			metadata = oauth_accounts.metadata || EXCLUDED.metadata,
			status = EXCLUDED.status,
			status_reason = EXCLUDED.status_reason,
			fingerprint_hash = EXCLUDED.fingerprint_hash,
			user_agent_hash = EXCLUDED.user_agent_hash,
			ip_hash = EXCLUDED.ip_hash,
			failure_count = EXCLUDED.failure_count,
			circuit_open_until = EXCLUDED.circuit_open_until,
			rate_limited_until = EXCLUDED.rate_limited_until,
			last_refresh_error = EXCLUDED.last_refresh_error,
			last_refresh_error_at = EXCLUDED.last_refresh_error_at,
			updated_at = now()
		RETURNING `+providerAccountColumns+`
	`, account.Provider,
		account.Subject,
		account.Name,
		account.DisplayName,
		account.EncryptedAccessToken,
		account.EncryptedRefreshToken,
		account.EncryptedIDToken,
		account.AccessTokenExpiresAt,
		account.LastRefreshAt,
		account.Enabled,
		account.Priority,
		metadataJSON(account.Metadata),
		account.Status,
		account.StatusReason,
		account.FingerprintHash,
		account.UserAgentHash,
		account.IPHash,
		account.FailureCount,
		account.CircuitOpenUntil,
		account.RateLimitedUntil,
		account.LastRefreshError,
		account.LastRefreshErrorAt,
	)
	saved, err := scanProviderAccount(row)
	if err != nil {
		return provider.Account{}, err
	}
	return saved, nil
}

func (r *ProviderRepository) UpdateAccount(ctx context.Context, providerName string, id int64, update provider.AccountUpdate) (provider.Account, error) {
	row := r.pool.QueryRow(ctx, `
		UPDATE oauth_accounts
		SET
			enabled = COALESCE($3, enabled),
			priority = COALESCE($4, priority),
			updated_at = now()
		WHERE provider = $1
			AND id = $2
		RETURNING `+providerAccountColumns+`
	`, providerName, id, update.Enabled, update.Priority)
	account, err := scanProviderAccount(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return provider.Account{}, provider.ErrNotConnected
	}
	if err != nil {
		return provider.Account{}, err
	}
	return account, nil
}

func (r *ProviderRepository) DeleteAccount(ctx context.Context, providerName string, id int64) error {
	var deletedID int64
	err := r.pool.QueryRow(ctx, `
		DELETE FROM oauth_accounts
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
		DELETE FROM oauth_accounts
		WHERE provider = $1
	`, providerName)
	return err
}

func (r *ProviderRepository) MarkAccountUsed(ctx context.Context, providerName string, id int64, usedAt time.Time) error {
	var updatedID int64
	err := r.pool.QueryRow(ctx, `
		UPDATE oauth_accounts
		SET
			last_used_at = $3,
			last_error = '',
			last_error_at = NULL,
			status = 'active',
			status_reason = '',
			failure_count = 0,
			circuit_open_until = NULL,
			last_refresh_error = '',
			last_refresh_error_at = NULL,
			updated_at = now()
		WHERE provider = $1
			AND id = $2
		RETURNING id
	`, providerName, id, usedAt).Scan(&updatedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return provider.ErrNotConnected
	}
	return err
}

func (r *ProviderRepository) MarkAccountError(ctx context.Context, providerName string, id int64, message string, at time.Time) error {
	var updatedID int64
	err := r.pool.QueryRow(ctx, `
		UPDATE oauth_accounts
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
	var updatedID int64
	err := r.pool.QueryRow(ctx, `
		UPDATE oauth_accounts
		SET
			failure_count = failure_count + 1,
			last_refresh_error = $3,
			last_refresh_error_at = $4,
			status = CASE WHEN $5::timestamptz IS NULL THEN status ELSE 'circuit_open' END,
			status_reason = CASE WHEN $5::timestamptz IS NULL THEN status_reason ELSE $3 END,
			circuit_open_until = COALESCE($5, circuit_open_until),
			updated_at = now()
		WHERE provider = $1
			AND id = $2
		RETURNING id
	`, providerName, id, message, at, openUntil).Scan(&updatedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return provider.ErrNotConnected
	}
	return err
}

func (r *ProviderRepository) RecordAccountStatus(ctx context.Context, providerName string, id int64, status, reason string, at time.Time, rateLimitedUntil, circuitOpenUntil *time.Time) error {
	var updatedID int64
	err := r.pool.QueryRow(ctx, `
		UPDATE oauth_accounts
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
	rows, err := r.pool.Query(ctx, `
		SELECT `+providerAccountModelColumns+`
		FROM oauth_account_models
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
		FROM oauth_accounts
		WHERE provider = $1
			AND id = $2
	`, providerName, accountID).Scan(&existingID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, provider.ErrNotConnected
	}
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec(ctx, `
		DELETE FROM oauth_account_models
		WHERE provider = $1
			AND account_id = $2
			AND source = $3
	`, providerName, accountID, provider.AccountModelSourceManual)
	if err != nil {
		return nil, err
	}

	for _, model := range models {
		_, err = tx.Exec(ctx, `
			INSERT INTO oauth_account_models (
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
		FROM oauth_account_models
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
		FROM oauth_account_models m
		JOIN oauth_accounts a ON a.id = m.account_id
			AND a.provider = m.provider
		WHERE m.provider = $1
			AND m.enabled = true
			AND m.model = ANY($2)
			AND a.enabled = true
			AND a.status <> 'disabled'
			AND (a.access_token_expires_at IS NULL OR a.access_token_expires_at > now())
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
		SELECT `+providerAccountColumnsFromAlias+`
		FROM oauth_accounts a
		JOIN oauth_account_models m ON m.account_id = a.id
			AND m.provider = a.provider
		WHERE a.provider = $1
			AND m.model = $2
			AND m.enabled = true
			AND a.enabled = true
			AND a.status <> 'disabled'
			AND (a.access_token_expires_at IS NULL OR a.access_token_expires_at > $3)
			AND (a.rate_limited_until IS NULL OR a.rate_limited_until <= $3)
			AND (a.circuit_open_until IS NULL OR a.circuit_open_until <= $3)
			AND ($4::bigint[] IS NULL OR cardinality($4::bigint[]) = 0 OR NOT (a.id = ANY($4::bigint[])))
		ORDER BY
			a.priority ASC,
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
