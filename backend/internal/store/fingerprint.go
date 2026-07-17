package store

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/KnowSky404/N2API/backend/internal/systemevent"
	"github.com/jackc/pgx/v5"
)

func (r *AdminRepository) ListFingerprintProfiles(ctx context.Context) ([]admin.FingerprintProfile, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, system_key, name, description, user_agent, tls_fingerprint, headers_json, enabled, created_at, updated_at
		FROM fingerprint_profiles
		ORDER BY CASE WHEN system_key <> '' THEN 0 ELSE 1 END, name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	profiles := []admin.FingerprintProfile{}
	for rows.Next() {
		var fp admin.FingerprintProfile
		var headersRaw []byte
		if err := rows.Scan(
			&fp.ID, &fp.SystemKey, &fp.Name, &fp.Description, &fp.UserAgent, &fp.TLSFingerprint,
			&headersRaw, &fp.Enabled, &fp.CreatedAt, &fp.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if len(headersRaw) > 0 {
			_ = json.Unmarshal(headersRaw, &fp.Headers)
		}
		if fp.Headers == nil {
			fp.Headers = map[string]string{}
		}
		profiles = append(profiles, fp)
	}
	return profiles, rows.Err()
}

func (r *AdminRepository) CreateFingerprintProfile(ctx context.Context, input admin.FingerprintProfileInput) (admin.FingerprintProfile, error) {
	headersJSON, err := json.Marshal(input.Headers)
	if err != nil {
		return admin.FingerprintProfile{}, err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return admin.FingerprintProfile{}, err
	}
	defer tx.Rollback(ctx)
	var fp admin.FingerprintProfile
	err = tx.QueryRow(ctx, `
		INSERT INTO fingerprint_profiles (name, description, user_agent, tls_fingerprint, headers_json, enabled)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, system_key, name, description, user_agent, tls_fingerprint, headers_json, enabled, created_at, updated_at
	`, input.Name, input.Description, input.UserAgent, input.TLSFingerprint, headersJSON, input.Enabled).Scan(
		&fp.ID, &fp.SystemKey, &fp.Name, &fp.Description, &fp.UserAgent, &fp.TLSFingerprint,
		&headersJSON, &fp.Enabled, &fp.CreatedAt, &fp.UpdatedAt,
	)
	if err != nil {
		return admin.FingerprintProfile{}, err
	}
	if err := insertIntentSystemEvent(ctx, tx, systemevent.Target{Type: "fingerprint_profile", ID: strconv.FormatInt(fp.ID, 10), Name: fp.Name}, nil); err != nil {
		return admin.FingerprintProfile{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.FingerprintProfile{}, err
	}
	_ = json.Unmarshal(headersJSON, &fp.Headers)
	if fp.Headers == nil {
		fp.Headers = map[string]string{}
	}
	return fp, nil
}

func (r *AdminRepository) UpdateFingerprintProfile(ctx context.Context, id int64, input admin.FingerprintProfileInput) (admin.FingerprintProfile, error) {
	headersJSON, err := json.Marshal(input.Headers)
	if err != nil {
		return admin.FingerprintProfile{}, err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return admin.FingerprintProfile{}, err
	}
	defer tx.Rollback(ctx)
	var fp admin.FingerprintProfile
	err = tx.QueryRow(ctx, `
		UPDATE fingerprint_profiles
		SET name = $2, description = $3, user_agent = $4, tls_fingerprint = $5, headers_json = $6, enabled = $7, updated_at = now()
		WHERE id = $1 AND system_key = ''
		RETURNING id, system_key, name, description, user_agent, tls_fingerprint, headers_json, enabled, created_at, updated_at
	`, id, input.Name, input.Description, input.UserAgent, input.TLSFingerprint, headersJSON, input.Enabled).Scan(
		&fp.ID, &fp.SystemKey, &fp.Name, &fp.Description, &fp.UserAgent, &fp.TLSFingerprint,
		&headersJSON, &fp.Enabled, &fp.CreatedAt, &fp.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return admin.FingerprintProfile{}, admin.ErrNotFound
		}
		return admin.FingerprintProfile{}, err
	}
	if err := insertIntentSystemEvent(ctx, tx, systemevent.Target{Type: "fingerprint_profile", ID: strconv.FormatInt(fp.ID, 10), Name: fp.Name}, nil); err != nil {
		return admin.FingerprintProfile{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.FingerprintProfile{}, err
	}
	_ = json.Unmarshal(headersJSON, &fp.Headers)
	if fp.Headers == nil {
		fp.Headers = map[string]string{}
	}
	return fp, nil
}

func (r *AdminRepository) DeleteFingerprintProfile(ctx context.Context, id int64) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var name string
	err = tx.QueryRow(ctx, `DELETE FROM fingerprint_profiles WHERE id = $1 AND system_key = '' RETURNING name`, id).Scan(&name)
	if err == pgx.ErrNoRows {
		return admin.ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := insertIntentSystemEvent(ctx, tx, systemevent.Target{Type: "fingerprint_profile", ID: strconv.FormatInt(id, 10), Name: name}, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
