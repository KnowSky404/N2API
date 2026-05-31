package store

import (
	"context"
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

func (r *ProviderRepository) FindAccount(ctx context.Context, providerName string) (provider.Account, error) {
	var account provider.Account
	err := r.pool.QueryRow(ctx, `
		SELECT provider, subject, display_name, encrypted_access_token, encrypted_refresh_token, access_token_expires_at, last_refresh_at
		FROM oauth_accounts
		WHERE provider = $1
		ORDER BY updated_at DESC, id DESC
		LIMIT 1
	`, providerName).Scan(
		&account.Provider,
		&account.Subject,
		&account.DisplayName,
		&account.EncryptedAccessToken,
		&account.EncryptedRefreshToken,
		&account.AccessTokenExpiresAt,
		&account.LastRefreshAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return provider.Account{}, provider.ErrNotConnected
	}
	if err != nil {
		return provider.Account{}, err
	}
	return account, nil
}

func (r *ProviderRepository) SaveAccount(ctx context.Context, account provider.Account) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO oauth_accounts (provider, subject, display_name, encrypted_access_token, encrypted_refresh_token, access_token_expires_at, last_refresh_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		ON CONFLICT (provider, subject)
		DO UPDATE SET
			display_name = EXCLUDED.display_name,
			encrypted_access_token = EXCLUDED.encrypted_access_token,
			encrypted_refresh_token = EXCLUDED.encrypted_refresh_token,
			access_token_expires_at = EXCLUDED.access_token_expires_at,
			last_refresh_at = EXCLUDED.last_refresh_at,
			updated_at = now()
	`, account.Provider,
		account.Subject,
		account.DisplayName,
		account.EncryptedAccessToken,
		account.EncryptedRefreshToken,
		account.AccessTokenExpiresAt,
		account.LastRefreshAt,
	)
	return err
}

func (r *ProviderRepository) DeleteAccount(ctx context.Context, providerName string) error {
	_, err := r.pool.Exec(ctx, `
		DELETE FROM oauth_accounts
		WHERE provider = $1
	`, providerName)
	return err
}

func (r *ProviderRepository) CreateState(ctx context.Context, state provider.OAuthState) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO oauth_states (provider, state_hash, redirect_after, expires_at)
		VALUES ($1, $2, $3, $4)
	`, state.Provider, state.StateHash, state.RedirectAfter, state.ExpiresAt)
	return err
}

func (r *ProviderRepository) FindState(ctx context.Context, providerName, stateHash string, now time.Time) (provider.OAuthState, error) {
	var state provider.OAuthState
	err := r.pool.QueryRow(ctx, `
		SELECT provider, state_hash, redirect_after, expires_at, consumed_at
		FROM oauth_states
		WHERE provider = $1
			AND state_hash = $2
			AND expires_at > $3
			AND consumed_at IS NULL
	`, providerName, stateHash, now).Scan(
		&state.Provider,
		&state.StateHash,
		&state.RedirectAfter,
		&state.ExpiresAt,
		&state.ConsumedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return provider.OAuthState{}, provider.ErrInvalidState
	}
	if err != nil {
		return provider.OAuthState{}, err
	}
	return state, nil
}

func (r *ProviderRepository) ConsumeState(ctx context.Context, providerName, stateHash string, now time.Time) error {
	var state provider.OAuthState
	err := r.pool.QueryRow(ctx, `
		UPDATE oauth_states
		SET consumed_at = $4
		WHERE provider = $1
			AND state_hash = $2
			AND expires_at > $3
			AND consumed_at IS NULL
		RETURNING provider, state_hash, redirect_after, expires_at, consumed_at
	`, providerName, stateHash, now, now).Scan(
		&state.Provider,
		&state.StateHash,
		&state.RedirectAfter,
		&state.ExpiresAt,
		&state.ConsumedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return provider.ErrInvalidState
	}
	if err != nil {
		return err
	}
	return nil
}
