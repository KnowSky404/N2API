package store

import (
	"context"
	"encoding/json"
	"errors"
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
	id, provider, subject, display_name, encrypted_access_token, encrypted_refresh_token,
	encrypted_id_token, access_token_expires_at, last_refresh_at, enabled, priority, last_used_at,
	last_error, last_error_at, metadata, created_at, updated_at
`

func scanProviderAccount(row pgx.Row) (provider.Account, error) {
	var account provider.Account
	err := row.Scan(
		&account.ID,
		&account.Provider,
		&account.Subject,
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
		&account.CreatedAt,
		&account.UpdatedAt,
	)
	return account, err
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

func (r *ProviderRepository) SaveAccount(ctx context.Context, account provider.Account) (provider.Account, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO oauth_accounts (
			provider, subject, display_name, encrypted_access_token, encrypted_refresh_token,
			encrypted_id_token, access_token_expires_at, last_refresh_at, enabled, priority, last_error, metadata, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, true, 100, '', $9, now())
		ON CONFLICT (provider, subject)
		DO UPDATE SET
			display_name = EXCLUDED.display_name,
			encrypted_access_token = EXCLUDED.encrypted_access_token,
			encrypted_refresh_token = EXCLUDED.encrypted_refresh_token,
			encrypted_id_token = EXCLUDED.encrypted_id_token,
			access_token_expires_at = EXCLUDED.access_token_expires_at,
			last_refresh_at = EXCLUDED.last_refresh_at,
			last_error = '',
			last_error_at = NULL,
			metadata = oauth_accounts.metadata || EXCLUDED.metadata,
			updated_at = now()
		RETURNING `+providerAccountColumns+`
	`, account.Provider,
		account.Subject,
		account.DisplayName,
		account.EncryptedAccessToken,
		account.EncryptedRefreshToken,
		account.EncryptedIDToken,
		account.AccessTokenExpiresAt,
		account.LastRefreshAt,
		metadataJSON(account.Metadata),
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

func (r *ProviderRepository) CreateState(ctx context.Context, state provider.OAuthState) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO oauth_states (provider, state_hash, redirect_after, expires_at, encrypted_code_verifier, code_verifier_hash, client_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, state.Provider, state.StateHash, state.RedirectAfter, state.ExpiresAt, state.EncryptedCodeVerifier, state.CodeVerifierHash, state.ClientID)
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
		RETURNING provider, state_hash, redirect_after, expires_at, consumed_at, encrypted_code_verifier, code_verifier_hash, client_id
	`, providerName, stateHash, now, now).Scan(
		&state.Provider,
		&state.StateHash,
		&state.RedirectAfter,
		&state.ExpiresAt,
		&state.ConsumedAt,
		&state.EncryptedCodeVerifier,
		&state.CodeVerifierHash,
		&state.ClientID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return provider.OAuthState{}, provider.ErrInvalidState
	}
	if err != nil {
		return provider.OAuthState{}, err
	}
	return state, nil
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
