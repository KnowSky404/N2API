package store

import (
	"context"
	"errors"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AdminRepository struct {
	pool *pgxpool.Pool
}

func NewAdminRepository(pool *pgxpool.Pool) *AdminRepository {
	return &AdminRepository{pool: pool}
}

func (r *AdminRepository) FindAdminByUsername(ctx context.Context, username string) (admin.Admin, error) {
	var found admin.Admin
	err := r.pool.QueryRow(ctx, `
		SELECT id, username, password_hash
		FROM admins
		WHERE username = $1
	`, username).Scan(&found.ID, &found.Username, &found.PasswordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.Admin{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.Admin{}, err
	}
	return found, nil
}

func (r *AdminRepository) CreateAdmin(ctx context.Context, username, passwordHash string) (admin.Admin, error) {
	var created admin.Admin
	err := r.pool.QueryRow(ctx, `
		INSERT INTO admins (username, password_hash)
		VALUES ($1, $2)
		RETURNING id, username, password_hash
	`, username, passwordHash).Scan(&created.ID, &created.Username, &created.PasswordHash)
	if err != nil {
		return admin.Admin{}, err
	}
	return created, nil
}

func (r *AdminRepository) CreateSession(ctx context.Context, adminID int64, tokenHash string, expiresAt time.Time) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO admin_sessions (admin_id, token_hash, expires_at)
		VALUES ($1, $2, $3)
	`, adminID, tokenHash, expiresAt)
	return err
}

func (r *AdminRepository) FindAdminBySessionHash(ctx context.Context, tokenHash string, now time.Time) (admin.Admin, error) {
	var found admin.Admin
	err := r.pool.QueryRow(ctx, `
		SELECT a.id, a.username, a.password_hash
		FROM admin_sessions s
		JOIN admins a ON a.id = s.admin_id
		WHERE s.token_hash = $1
			AND s.expires_at > $2
			AND s.revoked_at IS NULL
	`, tokenHash, now).Scan(&found.ID, &found.Username, &found.PasswordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.Admin{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.Admin{}, err
	}
	return found, nil
}

func (r *AdminRepository) RevokeSession(ctx context.Context, tokenHash string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE admin_sessions
		SET revoked_at = now()
		WHERE token_hash = $1
	`, tokenHash)
	return err
}

func (r *AdminRepository) CreateAPIKey(ctx context.Context, name, hash, prefix string) (admin.APIKey, error) {
	var created admin.APIKey
	err := r.pool.QueryRow(ctx, `
		INSERT INTO client_api_keys (name, key_hash, prefix)
		VALUES ($1, $2, $3)
		RETURNING id, name, prefix, created_at, last_used_at, revoked_at
	`, name, hash, prefix).Scan(
		&created.ID,
		&created.Name,
		&created.Prefix,
		&created.CreatedAt,
		&created.LastUsedAt,
		&created.RevokedAt,
	)
	if err != nil {
		return admin.APIKey{}, err
	}
	return created, nil
}

func (r *AdminRepository) ListAPIKeys(ctx context.Context) ([]admin.APIKey, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, name, prefix, created_at, last_used_at, revoked_at
		FROM client_api_keys
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []admin.APIKey
	for rows.Next() {
		var key admin.APIKey
		if err := rows.Scan(
			&key.ID,
			&key.Name,
			&key.Prefix,
			&key.CreatedAt,
			&key.LastUsedAt,
			&key.RevokedAt,
		); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return keys, nil
}

func (r *AdminRepository) RevokeAPIKey(ctx context.Context, id int64) (admin.APIKey, error) {
	var revoked admin.APIKey
	err := r.pool.QueryRow(ctx, `
		UPDATE client_api_keys
		SET revoked_at = COALESCE(revoked_at, now())
		WHERE id = $1
		RETURNING id, name, prefix, created_at, last_used_at, revoked_at
	`, id).Scan(
		&revoked.ID,
		&revoked.Name,
		&revoked.Prefix,
		&revoked.CreatedAt,
		&revoked.LastUsedAt,
		&revoked.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.APIKey{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.APIKey{}, err
	}
	return revoked, nil
}

func (r *AdminRepository) FindAPIKeyByHash(ctx context.Context, hash string, _ time.Time) (admin.APIKey, error) {
	var found admin.APIKey
	err := r.pool.QueryRow(ctx, `
		SELECT id, name, prefix, created_at, last_used_at, revoked_at
		FROM client_api_keys
		WHERE key_hash = $1
			AND revoked_at IS NULL
	`, hash).Scan(
		&found.ID,
		&found.Name,
		&found.Prefix,
		&found.CreatedAt,
		&found.LastUsedAt,
		&found.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.APIKey{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.APIKey{}, err
	}
	return found, nil
}

func (r *AdminRepository) TouchAPIKey(ctx context.Context, id int64, usedAt time.Time) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE client_api_keys
		SET last_used_at = $2
		WHERE id = $1
			AND revoked_at IS NULL
	`, id, usedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return admin.ErrNotFound
	}
	return nil
}
