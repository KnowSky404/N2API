package store

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/KnowSky404/N2API/backend/internal/systemevent"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AdminRepository struct {
	pool                   *pgxpool.Pool
	requestLogCursorSecret []byte
	managementCursorSecret []byte
	now                    func() time.Time
}

const modelSettingsKey = "model_settings"
const usagePricingKey = "usage_pricing"
const gatewaySettingsKey = "gateway_settings"
const requestLogCursorVersion = 1
const managementCursorVersion = 1
const managementCursorTTL = 24 * time.Hour
const requestLogRetentionAdvisoryLockID int64 = 0x4e32415049524c
const requestLogRetentionUnlockTimeout = 2 * time.Second
const apiKeySelectColumns = `
	k.id, k.name, k.prefix, k.encrypted_secret <> '', k.created_at, k.last_used_at, k.revoked_at, k.disabled_at,
	k.model_policy, k.requests_per_minute, k.tokens_per_minute,
	k.request_budget_24h, k.token_budget_24h, k.cost_budget_microusd_24h,
	k.request_budget_30d, k.token_budget_30d, k.cost_budget_microusd_30d,
	k.routing_pool_id, COALESCE(rp.name, '')
`

func NewAdminRepository(pool *pgxpool.Pool, cursorSecret string) *AdminRepository {
	requestLogKey := sha256.Sum256([]byte("n2api-request-log-cursor\x00" + cursorSecret))
	managementKey := sha256.Sum256([]byte("n2api-management-cursor\x00" + cursorSecret))
	return &AdminRepository{
		pool:                   pool,
		requestLogCursorSecret: requestLogKey[:],
		managementCursorSecret: managementKey[:],
		now:                    time.Now,
	}
}

func scanAPIKey(key *admin.APIKey) []any {
	return []any{
		&key.ID,
		&key.Name,
		&key.Prefix,
		&key.SecretAvailable,
		&key.CreatedAt,
		&key.LastUsedAt,
		&key.RevokedAt,
		&key.DisabledAt,
		&key.ModelPolicy,
		&key.RequestsPerMinute,
		&key.TokensPerMinute,
		&key.RequestBudget24h,
		&key.TokenBudget24h,
		&key.CostBudgetMicrousd24h,
		&key.RequestBudget30d,
		&key.TokenBudget30d,
		&key.CostBudgetMicrousd30d,
		&key.RoutingPoolID,
		&key.RoutingPoolName,
	}
}

func (r *AdminRepository) loadAPIKey(ctx context.Context, id int64) (admin.APIKey, error) {
	var key admin.APIKey
	err := r.pool.QueryRow(ctx, `
		SELECT `+apiKeySelectColumns+`
		FROM client_api_keys k
		LEFT JOIN routing_pools rp ON rp.id = k.routing_pool_id
		WHERE k.id = $1
	`, id).Scan(scanAPIKey(&key)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.APIKey{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.APIKey{}, err
	}
	if key.ModelPolicy == admin.APIKeyModelPolicySelected {
		models, err := r.ListAPIKeyModels(ctx, key.ID)
		if err != nil {
			return admin.APIKey{}, err
		}
		key.AllowedModels = models
	}
	return key, nil
}

func (r *AdminRepository) FindBootstrapAdmin(ctx context.Context) (admin.Admin, error) {
	var found admin.Admin
	err := r.pool.QueryRow(ctx, `
		SELECT id, username, password_hash
		FROM admins
		ORDER BY id ASC
		LIMIT 1
	`).Scan(&found.ID, &found.Username, &found.PasswordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.Admin{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.Admin{}, err
	}
	return found, nil
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
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return admin.Admin{}, err
	}
	defer tx.Rollback(ctx)
	var created admin.Admin
	err = tx.QueryRow(ctx, `
		INSERT INTO admins (username, password_hash)
		VALUES ($1, $2)
		RETURNING id, username, password_hash
	`, username, passwordHash).Scan(&created.ID, &created.Username, &created.PasswordHash)
	if err != nil {
		return admin.Admin{}, err
	}
	if err := insertIntentSystemEvent(ctx, tx, systemevent.Target{Type: "admin", ID: strconv.FormatInt(created.ID, 10), Name: created.Username}, nil); err != nil {
		return admin.Admin{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.Admin{}, err
	}
	return created, nil
}

func (r *AdminRepository) UpdateAdminUsername(ctx context.Context, id int64, username string) (admin.Admin, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return admin.Admin{}, err
	}
	defer tx.Rollback(ctx)
	var updated admin.Admin
	err = tx.QueryRow(ctx, `
		UPDATE admins
		SET username = $2, updated_at = now()
		WHERE id = $1
		RETURNING id, username, password_hash
	`, id, username).Scan(&updated.ID, &updated.Username, &updated.PasswordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.Admin{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.Admin{}, err
	}
	if err := insertIntentSystemEvent(ctx, tx, systemevent.Target{Type: "admin", ID: strconv.FormatInt(updated.ID, 10), Name: updated.Username}, nil); err != nil {
		return admin.Admin{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.Admin{}, err
	}
	return updated, nil
}

func (r *AdminRepository) UpdateAdminPasswordAndRevokeOtherSessions(ctx context.Context, id int64, passwordHash, currentSessionHash string, revokedAt time.Time) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var username string
	err = tx.QueryRow(ctx, `
		UPDATE admins
		SET password_hash = $2, updated_at = now()
		WHERE id = $1
			AND EXISTS (
				SELECT 1
				FROM admin_sessions
				WHERE admin_id = $1
					AND token_hash = $3
					AND revoked_at IS NULL
					AND expires_at > $4
			)
		RETURNING username
	`, id, passwordHash, currentSessionHash, revokedAt).Scan(&username)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, admin.ErrUnauthorized
	}
	if err != nil {
		return 0, err
	}
	result, err := tx.Exec(ctx, `
		UPDATE admin_sessions
		SET revoked_at = $3
		WHERE admin_id = $1
			AND token_hash <> $2
			AND revoked_at IS NULL
			AND expires_at > $3
	`, id, currentSessionHash, revokedAt)
	if err != nil {
		return 0, err
	}
	revoked := result.RowsAffected()
	if err := insertIntentSystemEvent(ctx, tx, systemevent.Target{Type: "admin", ID: strconv.FormatInt(id, 10), Name: username}, map[string]any{
		"revoked_other_sessions": revoked,
	}); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return revoked, nil
}

func (r *AdminRepository) CompareAndSwapAdminPasswordHash(ctx context.Context, id int64, oldHash, newHash string) (bool, error) {
	result, err := r.pool.Exec(ctx, `
		UPDATE admins
		SET password_hash = $3, updated_at = now()
		WHERE id = $1 AND password_hash = $2
	`, id, oldHash, newHash)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() == 1, nil
}

func (r *AdminRepository) CreateSession(ctx context.Context, adminID int64, tokenHash string, metadata admin.SessionMetadata, createdAt, expiresAt time.Time) error {
	_, err := createSession(ctx, r.pool, adminID, "", tokenHash, metadata, createdAt, expiresAt, false)
	return err
}

func (r *AdminRepository) CreateSessionIfAdminPasswordHashMatches(ctx context.Context, adminID int64, passwordHash, tokenHash string, metadata admin.SessionMetadata, createdAt, expiresAt time.Time) (bool, error) {
	return createSession(ctx, r.pool, adminID, passwordHash, tokenHash, metadata, createdAt, expiresAt, true)
}

func createSession(ctx context.Context, pool *pgxpool.Pool, adminID int64, passwordHash, tokenHash string, metadata admin.SessionMetadata, createdAt, expiresAt time.Time, verifyPasswordHash bool) (bool, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if verifyPasswordHash {
		var matches bool
		err = tx.QueryRow(ctx, `
			SELECT password_hash = $2
			FROM admins
			WHERE id = $1
			FOR UPDATE
		`, adminID, passwordHash).Scan(&matches)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && !matches) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO admin_sessions (
			admin_id, token_hash, expires_at, created_at, last_used_at,
			created_ip_summary, user_agent_summary
		)
		VALUES ($1, $2, $3, $4, $4, $5, $6)
	`, adminID, tokenHash, expiresAt, createdAt, metadata.CreatedIP, metadata.UserAgent)
	if err != nil {
		return false, err
	}
	if err := insertIntentSystemEvent(ctx, tx, systemevent.Target{Type: "admin", ID: strconv.FormatInt(adminID, 10)}, map[string]any{"expires_at": expiresAt.UTC().Format(time.RFC3339)}); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (r *AdminRepository) FindAdminBySessionHash(ctx context.Context, tokenHash string, now time.Time) (admin.Admin, error) {
	var found admin.Admin
	err := r.pool.QueryRow(ctx, `
		WITH active_session AS MATERIALIZED (
			SELECT id, admin_id
			FROM admin_sessions
			WHERE token_hash = $1
				AND expires_at > $2
				AND revoked_at IS NULL
		), touched AS (
			UPDATE admin_sessions AS session
			SET last_used_at = $2
			FROM active_session
			WHERE session.id = active_session.id
				AND session.last_used_at <= $2 - INTERVAL '1 minute'
			RETURNING session.id
		)
		SELECT a.id, a.username, a.password_hash
		FROM active_session
		JOIN admins a ON a.id = active_session.admin_id
	`, tokenHash, now).Scan(&found.ID, &found.Username, &found.PasswordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.Admin{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.Admin{}, err
	}
	return found, nil
}

func (r *AdminRepository) RevokeSession(ctx context.Context, tokenHash string, revokedAt time.Time) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var adminID int64
	var username string
	err = tx.QueryRow(ctx, `
		WITH revoked AS (
			UPDATE admin_sessions
			SET revoked_at = $2
			WHERE token_hash = $1
				AND revoked_at IS NULL
			RETURNING admin_id
		)
		SELECT revoked.admin_id, admins.username
		FROM revoked
		JOIN admins ON admins.id = revoked.admin_id
	`, tokenHash, revokedAt).Scan(&adminID, &username)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := insertIntentSystemEvent(ctx, tx, systemevent.Target{Type: "admin_session", Name: username}, map[string]any{"admin_id": adminID}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *AdminRepository) ListAdminSessions(ctx context.Context, adminID int64, currentHash string, now time.Time) ([]admin.AdminSession, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			id, token_hash = $2, created_at, last_used_at, expires_at,
			created_ip_summary, user_agent_summary, token_hash
		FROM admin_sessions
		WHERE admin_id = $1
			AND revoked_at IS NULL
			AND expires_at > $3
		ORDER BY (token_hash = $2) DESC, last_used_at DESC, id DESC
	`, adminID, currentHash, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessions := make([]admin.AdminSession, 0)
	for rows.Next() {
		var session admin.AdminSession
		if err := rows.Scan(
			&session.ID, &session.Current, &session.CreatedAt, &session.LastUsedAt, &session.ExpiresAt,
			&session.CreatedIP, &session.UserAgent, &session.TokenHash,
		); err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return sessions, nil
}

func (r *AdminRepository) RevokeAdminSession(ctx context.Context, adminID, sessionID int64, revokedAt time.Time) (admin.AdminSession, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return admin.AdminSession{}, err
	}
	defer tx.Rollback(ctx)

	var session admin.AdminSession
	err = tx.QueryRow(ctx, `
		UPDATE admin_sessions
		SET revoked_at = $3
		WHERE admin_id = $1
			AND id = $2
			AND revoked_at IS NULL
			AND expires_at > $3
		RETURNING id, created_at, last_used_at, expires_at,
			created_ip_summary, user_agent_summary, token_hash
	`, adminID, sessionID, revokedAt).Scan(
		&session.ID, &session.CreatedAt, &session.LastUsedAt, &session.ExpiresAt,
		&session.CreatedIP, &session.UserAgent, &session.TokenHash,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.AdminSession{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.AdminSession{}, err
	}
	if err := insertIntentSystemEvent(ctx, tx, systemevent.Target{
		Type: "admin_session", ID: strconv.FormatInt(session.ID, 10),
	}, map[string]any{"admin_id": adminID}); err != nil {
		return admin.AdminSession{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.AdminSession{}, err
	}
	return session, nil
}

func (r *AdminRepository) RevokeOtherAdminSessions(ctx context.Context, adminID int64, currentHash string, revokedAt time.Time) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	result, err := tx.Exec(ctx, `
		UPDATE admin_sessions
		SET revoked_at = $3
		WHERE admin_id = $1
			AND token_hash <> $2
			AND revoked_at IS NULL
			AND expires_at > $3
	`, adminID, currentHash, revokedAt)
	if err != nil {
		return 0, err
	}
	count := result.RowsAffected()
	if err := insertIntentSystemEvent(ctx, tx, systemevent.Target{Type: "admin_session"}, map[string]any{
		"admin_id": adminID,
		"count":    count,
	}); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return count, nil
}

func (r *AdminRepository) CreateAPIKey(ctx context.Context, name, hash, prefix, encryptedSecret string, routingPoolID *int64) (admin.APIKey, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return admin.APIKey{}, err
	}
	defer tx.Rollback(ctx)
	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO client_api_keys (name, key_hash, prefix, encrypted_secret, routing_pool_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, name, hash, prefix, encryptedSecret, routingPoolID).Scan(&id)
	if err != nil {
		return admin.APIKey{}, err
	}
	if err := insertIntentSystemEvent(ctx, tx, systemevent.Target{Type: "client_api_key", ID: strconv.FormatInt(id, 10), Name: name}, nil); err != nil {
		return admin.APIKey{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.APIKey{}, err
	}
	return r.loadAPIKey(ctx, id)
}

func (r *AdminRepository) GetAPIKeyEncryptedSecret(ctx context.Context, id int64) (string, error) {
	var encryptedSecret string
	err := r.pool.QueryRow(ctx, `
		SELECT encrypted_secret
		FROM client_api_keys
		WHERE id = $1
			AND revoked_at IS NULL
	`, id).Scan(&encryptedSecret)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", admin.ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return encryptedSecret, nil
}

func (r *AdminRepository) ListAPIKeys(ctx context.Context) ([]admin.APIKey, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+apiKeySelectColumns+`
		FROM client_api_keys k
		LEFT JOIN routing_pools rp ON rp.id = k.routing_pool_id
		ORDER BY k.created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []admin.APIKey
	for rows.Next() {
		var key admin.APIKey
		if err := rows.Scan(scanAPIKey(&key)...); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := r.populateAPIKeyModels(ctx, keys); err != nil {
		return nil, err
	}

	return keys, nil
}

func (r *AdminRepository) ListAPIKeyPage(ctx context.Context, filter admin.ManagementListFilter, now time.Time) (admin.APIKeyPage, error) {
	if r == nil || r.pool == nil || filter.Limit < 1 || filter.Limit > admin.MaxManagementPageSize || now.IsZero() {
		return admin.APIKeyPage{}, admin.ErrInvalidInput
	}
	whereSQL, args, err := r.managementPageWhere(filter, "api_keys", "k", now)
	if err != nil {
		return admin.APIKeyPage{}, err
	}
	if filter.Query != "" {
		args = append(args, "%"+filter.Query+"%")
		whereSQL = appendManagementCondition(whereSQL, "(lower(k.name) LIKE $"+strconv.Itoa(len(args))+" OR lower(k.prefix) LIKE $"+strconv.Itoa(len(args))+")")
	}
	args = append(args, filter.Limit+1)
	rows, err := r.pool.Query(ctx, `
		SELECT `+apiKeySelectColumns+`
		FROM client_api_keys k
		LEFT JOIN routing_pools rp ON rp.id = k.routing_pool_id
		`+whereSQL+`
		ORDER BY k.created_at DESC, k.id DESC
		LIMIT $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return admin.APIKeyPage{}, err
	}
	defer rows.Close()

	keys := make([]admin.APIKey, 0, filter.Limit+1)
	for rows.Next() {
		var key admin.APIKey
		if err := rows.Scan(scanAPIKey(&key)...); err != nil {
			return admin.APIKeyPage{}, err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return admin.APIKeyPage{}, err
	}
	page := admin.APIKeyPage{Keys: keys}
	if len(keys) > filter.Limit {
		page.HasMore = true
		page.Keys = keys[:filter.Limit]
	}
	if err := r.populateAPIKeyModels(ctx, page.Keys); err != nil {
		return admin.APIKeyPage{}, err
	}
	page.BudgetUsage, err = r.listAPIKeyBudgetUsage(ctx, page.Keys, now.UTC())
	if err != nil {
		return admin.APIKeyPage{}, err
	}
	if page.HasMore {
		last := page.Keys[len(page.Keys)-1]
		page.NextCursor, err = r.encodeManagementCursor(managementCursor{
			Version: managementCursorVersion, Resource: "api_keys", CreatedAt: last.CreatedAt.UTC(), ID: last.ID,
			FilterDigest: managementFilterDigest("api_keys", filter), ExpiresAt: r.clockNow().Add(managementCursorTTL),
		})
		if err != nil {
			return admin.APIKeyPage{}, err
		}
	}
	return page, nil
}

func (r *AdminRepository) RevokeAPIKey(ctx context.Context, id int64) (admin.APIKey, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return admin.APIKey{}, err
	}
	defer tx.Rollback(ctx)
	var updatedID int64
	var name string
	var requestBudget24h, tokenBudget24h, requestBudget30d, tokenBudget30d int64
	var costBudget24h, costBudget30d int64
	err = tx.QueryRow(ctx, `
		UPDATE client_api_keys
		SET revoked_at = COALESCE(revoked_at, now())
		WHERE id = $1
		RETURNING id, name,
			request_budget_24h, token_budget_24h, cost_budget_microusd_24h,
			request_budget_30d, token_budget_30d, cost_budget_microusd_30d
	`, id).Scan(
		&updatedID, &name,
		&requestBudget24h, &tokenBudget24h, &costBudget24h,
		&requestBudget30d, &tokenBudget30d, &costBudget30d,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.APIKey{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.APIKey{}, err
	}
	now := time.Now().UTC()
	snapshot := apiKeyBudgetSnapshotFromValues(updatedID, name, requestBudget24h, tokenBudget24h, costBudget24h, requestBudget30d, tokenBudget30d, costBudget30d)
	if err := recoverAPIKeyBudgetThresholdsForRevocation(ctx, tx, snapshot, now); err != nil {
		return admin.APIKey{}, err
	}
	if err := recoverAPIKeyRoutingExhaustionForRevocation(ctx, tx, updatedID, name, now); err != nil {
		return admin.APIKey{}, err
	}
	if err := insertIntentSystemEvent(ctx, tx, systemevent.Target{Type: "client_api_key", ID: strconv.FormatInt(updatedID, 10), Name: name}, nil); err != nil {
		return admin.APIKey{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.APIKey{}, err
	}
	return r.loadAPIKey(ctx, updatedID)
}

func (r *AdminRepository) DeleteRevokedAPIKey(ctx context.Context, id int64) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var name string
	err = tx.QueryRow(ctx, `
		SELECT name
		FROM client_api_keys
		WHERE id = $1
			AND revoked_at IS NOT NULL
		FOR UPDATE
	`, id).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM client_api_keys WHERE id = $1`, id); err != nil {
		return err
	}
	if err := insertIntentSystemEvent(ctx, tx, systemevent.Target{Type: "client_api_key", ID: strconv.FormatInt(id, 10), Name: name}, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *AdminRepository) PurgeRevokedAPIKeys(ctx context.Context, cutoff time.Time) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
		WITH candidates AS MATERIALIZED (
			SELECT id
			FROM client_api_keys
			WHERE revoked_at IS NOT NULL
				AND revoked_at <= $1
			ORDER BY id ASC
			FOR UPDATE
		)
		DELETE FROM client_api_keys keys
		USING candidates
		WHERE keys.id = candidates.id
	`, cutoff)
	if err != nil {
		return 0, err
	}
	deleted := tag.RowsAffected()
	if err := insertIntentSystemEvent(ctx, tx, systemevent.Target{Type: "client_api_key_collection"}, map[string]any{"deleted_count": deleted}); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return deleted, nil
}

func (r *AdminRepository) AuthenticateAPIKeyByHash(ctx context.Context, hash string, now time.Time) (admin.APIKey, bool, error) {
	var found admin.APIKey
	var touched bool
	err := r.pool.QueryRow(ctx, `
		WITH active_key AS MATERIALIZED (
			SELECT `+apiKeySelectColumns+`,
				ARRAY(
					SELECT model
					FROM client_api_key_models
					WHERE client_key_id = k.id
					ORDER BY model
				) AS allowed_models
			FROM client_api_keys k
			LEFT JOIN routing_pools rp ON rp.id = k.routing_pool_id
			WHERE k.key_hash = $1
				AND k.revoked_at IS NULL
				AND k.disabled_at IS NULL
		), touched AS (
			UPDATE client_api_keys AS key
			SET last_used_at = $2::timestamptz
			FROM active_key
			WHERE key.id = active_key.id
				AND key.revoked_at IS NULL
				AND key.disabled_at IS NULL
				AND (key.last_used_at IS NULL OR key.last_used_at <= $2::timestamptz - INTERVAL '1 minute')
			RETURNING key.id
		)
		SELECT active_key.*, EXISTS (SELECT 1 FROM touched)
		FROM active_key
	`, hash, now).Scan(append(scanAPIKey(&found), &found.AllowedModels, &touched)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.APIKey{}, false, admin.ErrNotFound
	}
	if err != nil {
		return admin.APIKey{}, false, err
	}
	if found.ModelPolicy != admin.APIKeyModelPolicySelected {
		found.AllowedModels = nil
	}
	return found, touched, nil
}

func (r *AdminRepository) UpdateAPIKeyName(ctx context.Context, id int64, name string) (admin.APIKey, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return admin.APIKey{}, err
	}
	defer tx.Rollback(ctx)
	var updatedID int64
	err = tx.QueryRow(ctx, `
		UPDATE client_api_keys
		SET name = $2
		WHERE id = $1
			AND revoked_at IS NULL
		RETURNING id
	`, id, name).Scan(&updatedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.APIKey{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.APIKey{}, err
	}
	if err := insertIntentSystemEvent(ctx, tx, systemevent.Target{Type: "client_api_key", ID: strconv.FormatInt(updatedID, 10), Name: name}, nil); err != nil {
		return admin.APIKey{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.APIKey{}, err
	}
	return r.loadAPIKey(ctx, updatedID)
}

func (r *AdminRepository) SetAPIKeyDisabled(ctx context.Context, id int64, disabled bool) (admin.APIKey, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return admin.APIKey{}, err
	}
	defer tx.Rollback(ctx)
	var updatedID int64
	var name string
	err = tx.QueryRow(ctx, `
		UPDATE client_api_keys
		SET disabled_at = CASE WHEN $2 THEN COALESCE(disabled_at, now()) ELSE NULL END
		WHERE id = $1
			AND revoked_at IS NULL
		RETURNING id, name
	`, id, disabled).Scan(&updatedID, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.APIKey{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.APIKey{}, err
	}
	if err := insertIntentSystemEvent(ctx, tx, systemevent.Target{Type: "client_api_key", ID: strconv.FormatInt(updatedID, 10), Name: name}, nil); err != nil {
		return admin.APIKey{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.APIKey{}, err
	}
	return r.loadAPIKey(ctx, updatedID)
}

func (r *AdminRepository) UpdateAPIKeyModelPolicy(ctx context.Context, id int64, policy string, models []string) (admin.APIKey, error) {
	models = normalizeAPIKeyModels(models)
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return admin.APIKey{}, err
	}
	defer tx.Rollback(ctx)

	var updatedID int64
	err = tx.QueryRow(ctx, `
		UPDATE client_api_keys
		SET model_policy = $2
		WHERE id = $1
			AND revoked_at IS NULL
		RETURNING id
	`, id, policy).Scan(&updatedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.APIKey{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.APIKey{}, err
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM client_api_key_models
		WHERE client_key_id = $1
	`, id); err != nil {
		return admin.APIKey{}, err
	}
	for _, model := range models {
		if _, err := tx.Exec(ctx, `
			INSERT INTO client_api_key_models (client_key_id, model)
			VALUES ($1, $2)
		`, id, model); err != nil {
			return admin.APIKey{}, err
		}
	}
	if err := insertIntentSystemEvent(ctx, tx, systemevent.Target{Type: "client_api_key", ID: strconv.FormatInt(updatedID, 10)}, nil); err != nil {
		return admin.APIKey{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.APIKey{}, err
	}
	return r.loadAPIKey(ctx, updatedID)
}

func (r *AdminRepository) UpdateAPIKeyLimits(ctx context.Context, id int64, requestsPerMinute, tokensPerMinute int) (admin.APIKey, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return admin.APIKey{}, err
	}
	defer tx.Rollback(ctx)
	var updatedID int64
	var name string
	err = tx.QueryRow(ctx, `
		UPDATE client_api_keys
		SET requests_per_minute = $2,
			tokens_per_minute = $3
		WHERE id = $1
			AND revoked_at IS NULL
		RETURNING id, name
	`, id, requestsPerMinute, tokensPerMinute).Scan(&updatedID, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.APIKey{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.APIKey{}, err
	}
	if err := insertIntentSystemEvent(ctx, tx, systemevent.Target{Type: "client_api_key", ID: strconv.FormatInt(updatedID, 10), Name: name}, nil); err != nil {
		return admin.APIKey{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.APIKey{}, err
	}
	return r.loadAPIKey(ctx, updatedID)
}

func (r *AdminRepository) UpdateAPIKeyBudgets(ctx context.Context, id int64, requestBudget24h, tokenBudget24h int, costBudgetMicrousd24h int64, requestBudget30d, tokenBudget30d int, costBudgetMicrousd30d int64) (admin.APIKey, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return admin.APIKey{}, err
	}
	defer tx.Rollback(ctx)
	var name string
	var previous apiKeyBudgetLimits
	err = tx.QueryRow(ctx, `
		SELECT name,
			request_budget_24h, token_budget_24h, cost_budget_microusd_24h,
			request_budget_30d, token_budget_30d, cost_budget_microusd_30d
		FROM client_api_keys
		WHERE id = $1
			AND revoked_at IS NULL
		FOR UPDATE
	`, id).Scan(
		&name,
		&previous.Request24h, &previous.Token24h, &previous.Cost24h,
		&previous.Request30d, &previous.Token30d, &previous.Cost30d,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.APIKey{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.APIKey{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE client_api_keys
		SET request_budget_24h = $2,
			token_budget_24h = $3,
			cost_budget_microusd_24h = $4,
			request_budget_30d = $5,
			token_budget_30d = $6,
			cost_budget_microusd_30d = $7
		WHERE id = $1
			AND revoked_at IS NULL
	`, id, requestBudget24h, tokenBudget24h, costBudgetMicrousd24h, requestBudget30d, tokenBudget30d, costBudgetMicrousd30d); err != nil {
		return admin.APIKey{}, err
	}
	current := apiKeyBudgetLimits{
		Request24h: int64(requestBudget24h), Token24h: int64(tokenBudget24h), Cost24h: costBudgetMicrousd24h,
		Request30d: int64(requestBudget30d), Token30d: int64(tokenBudget30d), Cost30d: costBudgetMicrousd30d,
	}
	switch {
	case budgetLimitsDisabled(previous) && !budgetLimitsDisabled(current):
		_, err := tx.Exec(ctx, `
			UPDATE api_key_budget_states
			SET initialization_status = 'pending',
				initialization_window_start = now() - INTERVAL '30 days',
				initialization_window_end = now(),
				initialization_cursor_created_at = NULL,
				initialization_cursor_request_log_id = NULL,
				initialized_at = NULL,
				version = version + 1,
				updated_at = now()
			WHERE client_key_id = $1
				AND initialization_status = 'ready'
				AND EXISTS (
					SELECT 1
					FROM request_logs logs
					WHERE logs.client_key_id = $1
						AND logs.created_at >= now() - INTERVAL '30 days'
						AND logs.budget_backfill_eligible
						AND NOT EXISTS (
							SELECT 1
							FROM api_key_budget_admissions admissions
							WHERE admissions.admission_id = 'legacy:' || logs.id::text
						)
				)
		`, id)
		if err != nil {
			return admin.APIKey{}, err
		}
	}
	if err := insertIntentSystemEvent(ctx, tx, systemevent.Target{Type: "client_api_key", ID: strconv.FormatInt(id, 10), Name: name}, nil); err != nil {
		return admin.APIKey{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.APIKey{}, err
	}
	return r.loadAPIKey(ctx, id)
}

func (r *AdminRepository) ListRoutingPools(ctx context.Context) ([]admin.RoutingPool, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT p.id, p.name, p.description, p.enabled, p.fallback_pool_id,
			COALESCE(fp.name, ''), p.created_at, p.updated_at
		FROM routing_pools p
		LEFT JOIN routing_pools fp ON fp.id = p.fallback_pool_id
		ORDER BY p.name ASC, p.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pools []admin.RoutingPool
	for rows.Next() {
		var pool admin.RoutingPool
		if err := rows.Scan(&pool.ID, &pool.Name, &pool.Description, &pool.Enabled, &pool.FallbackPoolID, &pool.FallbackPoolName, &pool.CreatedAt, &pool.UpdatedAt); err != nil {
			return nil, err
		}
		pools = append(pools, pool)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := r.populateRoutingPoolAccounts(ctx, pools); err != nil {
		return nil, err
	}
	return pools, nil
}

func (r *AdminRepository) ListRoutingPoolPage(ctx context.Context, filter admin.ManagementListFilter) (admin.RoutingPoolPage, error) {
	if r == nil || r.pool == nil || filter.Limit < 1 || filter.Limit > admin.MaxManagementPageSize {
		return admin.RoutingPoolPage{}, admin.ErrInvalidInput
	}
	now := r.clockNow()
	whereSQL, args, err := r.managementPageWhere(filter, "routing_pools", "p", now)
	if err != nil {
		return admin.RoutingPoolPage{}, err
	}
	if filter.Query != "" {
		args = append(args, "%"+filter.Query+"%")
		param := "$" + strconv.Itoa(len(args))
		whereSQL = appendManagementCondition(whereSQL, "(lower(p.name) LIKE "+param+" OR lower(p.description) LIKE "+param+")")
	}
	args = append(args, filter.Limit+1)
	rows, err := r.pool.Query(ctx, `
		SELECT p.id, p.name, p.description, p.enabled, p.fallback_pool_id,
			COALESCE(fp.name, ''), p.created_at, p.updated_at
		FROM routing_pools p
		LEFT JOIN routing_pools fp ON fp.id = p.fallback_pool_id
		`+whereSQL+`
		ORDER BY p.created_at DESC, p.id DESC
		LIMIT $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return admin.RoutingPoolPage{}, err
	}
	defer rows.Close()
	pools := make([]admin.RoutingPool, 0, filter.Limit+1)
	for rows.Next() {
		var pool admin.RoutingPool
		if err := rows.Scan(&pool.ID, &pool.Name, &pool.Description, &pool.Enabled, &pool.FallbackPoolID, &pool.FallbackPoolName, &pool.CreatedAt, &pool.UpdatedAt); err != nil {
			return admin.RoutingPoolPage{}, err
		}
		pools = append(pools, pool)
	}
	if err := rows.Err(); err != nil {
		return admin.RoutingPoolPage{}, err
	}
	page := admin.RoutingPoolPage{Pools: pools}
	if len(pools) > filter.Limit {
		page.HasMore = true
		page.Pools = pools[:filter.Limit]
	}
	if err := r.populateRoutingPoolAccounts(ctx, page.Pools); err != nil {
		return admin.RoutingPoolPage{}, err
	}
	if page.HasMore {
		last := page.Pools[len(page.Pools)-1]
		page.NextCursor, err = r.encodeManagementCursor(managementCursor{
			Version: managementCursorVersion, Resource: "routing_pools", CreatedAt: last.CreatedAt.UTC(), ID: last.ID,
			FilterDigest: managementFilterDigest("routing_pools", filter), ExpiresAt: now.Add(managementCursorTTL),
		})
		if err != nil {
			return admin.RoutingPoolPage{}, err
		}
	}
	return page, nil
}

func (r *AdminRepository) CreateRoutingPool(ctx context.Context, name, description string, enabled bool, fallbackPoolID *int64) (admin.RoutingPool, error) {
	if err := r.validateRoutingPoolFallback(ctx, 0, fallbackPoolID); err != nil {
		return admin.RoutingPool{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return admin.RoutingPool{}, err
	}
	defer tx.Rollback(ctx)
	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO routing_pools (name, description, enabled, fallback_pool_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, name, description, enabled, fallbackPoolID).Scan(&id)
	if err != nil {
		return admin.RoutingPool{}, err
	}
	if err := insertIntentSystemEvent(ctx, tx, systemevent.Target{Type: "routing_pool", ID: strconv.FormatInt(id, 10), Name: name}, nil); err != nil {
		return admin.RoutingPool{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.RoutingPool{}, err
	}
	return r.getRoutingPool(ctx, id)
}

func (r *AdminRepository) UpdateRoutingPool(ctx context.Context, id int64, name, description string, enabled bool, fallbackPoolID *int64) (admin.RoutingPool, error) {
	if err := r.validateRoutingPoolFallback(ctx, id, fallbackPoolID); err != nil {
		return admin.RoutingPool{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return admin.RoutingPool{}, err
	}
	defer tx.Rollback(ctx)
	var updatedID int64
	err = tx.QueryRow(ctx, `
		UPDATE routing_pools
		SET name = $2,
			description = $3,
			enabled = $4,
			fallback_pool_id = $5,
			updated_at = now()
		WHERE id = $1
		RETURNING id
	`, id, name, description, enabled, fallbackPoolID).Scan(&updatedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.RoutingPool{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.RoutingPool{}, err
	}
	if err := insertIntentSystemEvent(ctx, tx, systemevent.Target{Type: "routing_pool", ID: strconv.FormatInt(updatedID, 10), Name: name}, nil); err != nil {
		return admin.RoutingPool{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.RoutingPool{}, err
	}
	return r.getRoutingPool(ctx, updatedID)
}

func (r *AdminRepository) validateRoutingPoolFallback(ctx context.Context, poolID int64, fallbackPoolID *int64) error {
	if fallbackPoolID == nil {
		return nil
	}
	if *fallbackPoolID <= 0 || *fallbackPoolID == poolID {
		return admin.ErrInvalidInput
	}
	seen := map[int64]struct{}{}
	if poolID > 0 {
		seen[poolID] = struct{}{}
	}
	for currentID := *fallbackPoolID; currentID > 0; {
		if _, ok := seen[currentID]; ok {
			return admin.ErrInvalidInput
		}
		seen[currentID] = struct{}{}
		var nextFallbackID *int64
		err := r.pool.QueryRow(ctx, `
			SELECT fallback_pool_id
			FROM routing_pools
			WHERE id = $1
		`, currentID).Scan(&nextFallbackID)
		if errors.Is(err, pgx.ErrNoRows) {
			return admin.ErrInvalidInput
		}
		if err != nil {
			return err
		}
		if nextFallbackID == nil {
			return nil
		}
		currentID = *nextFallbackID
	}
	return nil
}

func (r *AdminRepository) DeleteRoutingPool(ctx context.Context, id int64) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var name string
	err = tx.QueryRow(ctx, `DELETE FROM routing_pools WHERE id = $1 RETURNING name`, id).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := insertIntentSystemEvent(ctx, tx, systemevent.Target{Type: "routing_pool", ID: strconv.FormatInt(id, 10), Name: name}, nil); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *AdminRepository) ReplaceRoutingPoolAccounts(ctx context.Context, id int64, accounts []admin.RoutingPoolAccount) (admin.RoutingPool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return admin.RoutingPool{}, err
	}
	defer tx.Rollback(ctx)

	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM routing_pools WHERE id = $1)`, id).Scan(&exists); err != nil {
		return admin.RoutingPool{}, err
	}
	if !exists {
		return admin.RoutingPool{}, admin.ErrNotFound
	}

	if _, err := tx.Exec(ctx, `DELETE FROM routing_pool_accounts WHERE pool_id = $1`, id); err != nil {
		return admin.RoutingPool{}, err
	}
	for _, account := range accounts {
		if _, err := tx.Exec(ctx, `
			INSERT INTO routing_pool_accounts (pool_id, account_id, priority)
			VALUES ($1, $2, $3)
		`, id, account.AccountID, account.Priority); err != nil {
			return admin.RoutingPool{}, err
		}
	}
	if err := insertIntentSystemEvent(ctx, tx, systemevent.Target{Type: "routing_pool", ID: strconv.FormatInt(id, 10)}, nil); err != nil {
		return admin.RoutingPool{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.RoutingPool{}, err
	}
	return r.getRoutingPool(ctx, id)
}

func (r *AdminRepository) UpdateAPIKeyRoutingPool(ctx context.Context, id int64, routingPoolID *int64) (admin.APIKey, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return admin.APIKey{}, err
	}
	defer tx.Rollback(ctx)
	var updatedID int64
	var name string
	err = tx.QueryRow(ctx, `
		UPDATE client_api_keys
		SET routing_pool_id = $2
		WHERE id = $1
			AND revoked_at IS NULL
		RETURNING id, name
	`, id, routingPoolID).Scan(&updatedID, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.APIKey{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.APIKey{}, err
	}
	if err := insertIntentSystemEvent(ctx, tx, systemevent.Target{Type: "client_api_key", ID: strconv.FormatInt(updatedID, 10), Name: name}, nil); err != nil {
		return admin.APIKey{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.APIKey{}, err
	}
	return r.loadAPIKey(ctx, updatedID)
}

func (r *AdminRepository) getRoutingPool(ctx context.Context, id int64) (admin.RoutingPool, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			p.id, p.name, p.description, p.enabled, p.fallback_pool_id, COALESCE(fp.name, ''), p.created_at, p.updated_at,
			rpa.account_id, rpa.priority
		FROM routing_pools p
		LEFT JOIN routing_pools fp ON fp.id = p.fallback_pool_id
		LEFT JOIN routing_pool_accounts rpa ON rpa.pool_id = p.id
		WHERE p.id = $1
		ORDER BY rpa.priority ASC, rpa.account_id ASC
	`, id)
	if err != nil {
		return admin.RoutingPool{}, err
	}
	defer rows.Close()

	var pool admin.RoutingPool
	found := false
	for rows.Next() {
		var accountID *int64
		var priority *int
		if err := rows.Scan(
			&pool.ID,
			&pool.Name,
			&pool.Description,
			&pool.Enabled,
			&pool.FallbackPoolID,
			&pool.FallbackPoolName,
			&pool.CreatedAt,
			&pool.UpdatedAt,
			&accountID,
			&priority,
		); err != nil {
			return admin.RoutingPool{}, err
		}
		found = true
		if accountID != nil {
			account := admin.RoutingPoolAccount{AccountID: *accountID}
			if priority != nil {
				account.Priority = *priority
			}
			pool.Accounts = append(pool.Accounts, account)
			pool.AccountIDs = append(pool.AccountIDs, *accountID)
		}
	}
	if err := rows.Err(); err != nil {
		return admin.RoutingPool{}, err
	}
	if !found {
		return admin.RoutingPool{}, admin.ErrNotFound
	}
	return pool, nil
}

func (r *AdminRepository) GetAPIKeyBudgetUsage(ctx context.Context, keyID int64, now time.Time) (admin.APIKeyBudgetUsage, error) {
	if r == nil || r.pool == nil || keyID < 1 || now.IsZero() {
		return admin.APIKeyBudgetUsage{}, admin.ErrInvalidInput
	}
	now = now.UTC()
	usage := admin.APIKeyBudgetUsage{KeyID: keyID}
	err := r.pool.QueryRow(ctx, `
		SELECT
			initialization_status,
			initialization_status <> 'ready' OR EXISTS (
				SELECT 1
				FROM api_key_budget_admissions admissions
				WHERE admissions.client_key_id = api_key_budget_states.client_key_id
					AND (
						(NOT admissions.request_24h_expired AND admissions.request_24h_expires_at <= $2)
						OR (NOT admissions.request_30d_expired AND admissions.request_30d_expires_at <= $2)
					)
			),
			requests_used_24h,
			observed_tokens_used_24h,
			observed_cost_microusd_used_24h,
			requests_used_30d,
			observed_tokens_used_30d,
			observed_cost_microusd_used_30d
		FROM api_key_budget_states
		WHERE client_key_id = $1
	`, keyID, now).Scan(
		&usage.InitializationStatus,
		&usage.Stale,
		&usage.RequestsUsed24h,
		&usage.TokensUsed24h,
		&usage.CostMicrousd24h,
		&usage.RequestsUsed30d,
		&usage.TokensUsed30d,
		&usage.CostMicrousd30d,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.APIKeyBudgetUsage{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.APIKeyBudgetUsage{}, err
	}
	return usage, nil
}

func normalizeAPIKeyModels(models []string) []string {
	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(models))
	for _, raw := range models {
		model := strings.TrimSpace(raw)
		if model == "" {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		normalized = append(normalized, model)
	}
	return normalized
}

func (r *AdminRepository) ListAPIKeyModels(ctx context.Context, id int64) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT model
		FROM client_api_key_models
		WHERE client_key_id = $1
		ORDER BY model ASC
	`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var models []string
	for rows.Next() {
		var model string
		if err := rows.Scan(&model); err != nil {
			return nil, err
		}
		models = append(models, model)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return models, nil
}

func (r *AdminRepository) populateAPIKeyModels(ctx context.Context, keys []admin.APIKey) error {
	ids := make([]int64, 0, len(keys))
	keyIndexes := make(map[int64]int, len(keys))
	for i := range keys {
		keyIndexes[keys[i].ID] = i
		ids = append(ids, keys[i].ID)
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT client_key_id, model
		FROM client_api_key_models
		WHERE client_key_id = ANY($1)
		ORDER BY client_key_id ASC, model ASC
	`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var keyID int64
		var model string
		if err := rows.Scan(&keyID, &model); err != nil {
			return err
		}
		if index, ok := keyIndexes[keyID]; ok {
			keys[index].AllowedModels = append(keys[index].AllowedModels, model)
		}
	}
	return rows.Err()
}

func (r *AdminRepository) listAPIKeyBudgetUsage(ctx context.Context, keys []admin.APIKey, now time.Time) (map[int64]admin.APIKeyBudgetUsage, error) {
	usageByKey := make(map[int64]admin.APIKeyBudgetUsage, len(keys))
	if len(keys) == 0 {
		return usageByKey, nil
	}
	ids := make([]int64, len(keys))
	for i := range keys {
		ids[i] = keys[i].ID
	}
	rows, err := r.pool.Query(ctx, `
		SELECT states.client_key_id,
			states.initialization_status,
			states.initialization_status <> 'ready' OR EXISTS (
				SELECT 1 FROM api_key_budget_admissions admissions
				WHERE admissions.client_key_id = states.client_key_id
					AND ((NOT admissions.request_24h_expired AND admissions.request_24h_expires_at <= $2)
						OR (NOT admissions.request_30d_expired AND admissions.request_30d_expires_at <= $2))
			),
			states.requests_used_24h, states.observed_tokens_used_24h, states.observed_cost_microusd_used_24h,
			states.requests_used_30d, states.observed_tokens_used_30d, states.observed_cost_microusd_used_30d
		FROM api_key_budget_states states
		WHERE states.client_key_id = ANY($1)
	`, ids, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var usage admin.APIKeyBudgetUsage
		if err := rows.Scan(
			&usage.KeyID, &usage.InitializationStatus, &usage.Stale,
			&usage.RequestsUsed24h, &usage.TokensUsed24h, &usage.CostMicrousd24h,
			&usage.RequestsUsed30d, &usage.TokensUsed30d, &usage.CostMicrousd30d,
		); err != nil {
			return nil, err
		}
		usageByKey[usage.KeyID] = usage
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(usageByKey) != len(keys) {
		return nil, admin.ErrNotFound
	}
	return usageByKey, nil
}

func (r *AdminRepository) populateRoutingPoolAccounts(ctx context.Context, pools []admin.RoutingPool) error {
	if len(pools) == 0 {
		return nil
	}
	ids := make([]int64, len(pools))
	poolIndexes := make(map[int64]int, len(pools))
	for i := range pools {
		ids[i] = pools[i].ID
		poolIndexes[pools[i].ID] = i
	}
	rows, err := r.pool.Query(ctx, `
		SELECT pool_id, account_id, priority
		FROM routing_pool_accounts
		WHERE pool_id = ANY($1)
		ORDER BY pool_id ASC, priority ASC, account_id ASC
	`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var poolID, accountID int64
		var priority int
		if err := rows.Scan(&poolID, &accountID, &priority); err != nil {
			return err
		}
		if index, ok := poolIndexes[poolID]; ok {
			pools[index].Accounts = append(pools[index].Accounts, admin.RoutingPoolAccount{AccountID: accountID, Priority: priority})
			pools[index].AccountIDs = append(pools[index].AccountIDs, accountID)
		}
	}
	return rows.Err()
}

type managementCursor struct {
	Version      int       `json:"v"`
	Resource     string    `json:"r"`
	CreatedAt    time.Time `json:"createdAt"`
	ID           int64     `json:"id"`
	FilterDigest string    `json:"filter"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

func (r *AdminRepository) managementPageWhere(filter admin.ManagementListFilter, resource, alias string, now time.Time) (string, []any, error) {
	if filter.Cursor == "" {
		return "", nil, nil
	}
	cursor, err := r.decodeManagementCursor(filter.Cursor, resource, filter, now)
	if err != nil {
		return "", nil, err
	}
	return "WHERE (" + alias + ".created_at, " + alias + ".id) < ($1, $2)", []any{cursor.CreatedAt.UTC(), cursor.ID}, nil
}

func appendManagementCondition(whereSQL, condition string) string {
	if whereSQL == "" {
		return "WHERE " + condition
	}
	return whereSQL + " AND " + condition
}

func managementFilterDigest(resource string, filter admin.ManagementListFilter) string {
	digest := sha256.Sum256([]byte(resource + "\x00" + filter.Query))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func (r *AdminRepository) encodeManagementCursor(cursor managementCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, r.managementCursorSecret)
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(append(payload, mac.Sum(nil)...)), nil
}

func (r *AdminRepository) decodeManagementCursor(value, resource string, filter admin.ManagementListFilter, now time.Time) (managementCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) <= sha256.Size {
		return managementCursor{}, admin.ErrInvalidCursor
	}
	payload, signature := decoded[:len(decoded)-sha256.Size], decoded[len(decoded)-sha256.Size:]
	mac := hmac.New(sha256.New, r.managementCursorSecret)
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return managementCursor{}, admin.ErrInvalidCursor
	}
	var cursor managementCursor
	if err := json.Unmarshal(payload, &cursor); err != nil ||
		cursor.Version != managementCursorVersion || cursor.Resource != resource || cursor.CreatedAt.IsZero() || cursor.ID < 1 ||
		cursor.ExpiresAt.IsZero() || !now.Before(cursor.ExpiresAt) ||
		!hmac.Equal([]byte(cursor.FilterDigest), []byte(managementFilterDigest(resource, filter))) {
		return managementCursor{}, admin.ErrInvalidCursor
	}
	cursor.CreatedAt = cursor.CreatedAt.UTC()
	cursor.ExpiresAt = cursor.ExpiresAt.UTC()
	return cursor, nil
}

func (r *AdminRepository) clockNow() time.Time {
	if r != nil && r.now != nil {
		return r.now().UTC()
	}
	return time.Now().UTC()
}

type requestLogCursor struct {
	Version      int       `json:"v"`
	CreatedAt    time.Time `json:"t"`
	ID           int64     `json:"i"`
	FilterDigest string    `json:"f"`
}

const requestLogSelectSQL = `SELECT
	l.id,
	l.request_id,
	l.upstream_request_id,
	COALESCE(k.name || ' (' || k.prefix || ')', ''),
	l.provider,
	COALESCE(l.provider_account_id, 0),
	COALESCE(NULLIF(l.provider_account_type, ''), a.account_type, ''),
	COALESCE(NULLIF(l.provider_account_name, ''), NULLIF(a.display_name, ''), a.name, ''),
	COALESCE(l.routing_pool_id, 0),
	COALESCE(l.routing_pool_name, ''),
	COALESCE(l.routing_pool_fallback_depth, 0),
	COALESCE(l.routing_pool_fallback_chain, ''),
	COALESCE(l.routing_pool_error, ''),
	l.model,
	l.session_id,
	l.route,
	l.method,
	l.status_code,
	l.latency_ms,
	l.error,
	l.input_tokens,
	l.output_tokens,
	l.total_tokens,
	l.cached_input_tokens,
	l.reasoning_tokens,
	l.usage_source,
	l.estimated_cost_microusd,
	COALESCE((l.pricing_snapshot->>'matched')::boolean, false),
	COALESCE(l.gateway_attempt_count, 0),
	COALESCE(l.gateway_fallback_count, 0),
	l.created_at
FROM request_logs l
LEFT JOIN client_api_keys k ON k.id = l.client_key_id
LEFT JOIN provider_accounts a ON a.id = l.provider_account_id
`

func (r *AdminRepository) ListRequestLogs(ctx context.Context, filter admin.RequestLogFilter) (admin.RequestLogPage, error) {
	if filter.Limit < 1 || filter.Limit > 200 {
		return admin.RequestLogPage{}, admin.ErrInvalidInput
	}
	whereSQL, args := requestLogFilterSQL(filter)
	if filter.Cursor != "" {
		cursor, err := r.decodeRequestLogCursor(filter.Cursor, filter)
		if err != nil {
			return admin.RequestLogPage{}, err
		}
		args = append(args, cursor.CreatedAt.UTC(), cursor.ID)
		condition := "(l.created_at, l.id) < ($" + strconv.Itoa(len(args)-1) + ", $" + strconv.Itoa(len(args)) + ")"
		if whereSQL == "" {
			whereSQL = "WHERE " + condition
		} else {
			whereSQL += " AND " + condition
		}
	}
	args = append(args, filter.Limit+1)
	limitParam := len(args)

	rows, err := r.pool.Query(ctx, requestLogSelectSQL+whereSQL+`
		ORDER BY l.created_at DESC, l.id DESC
		LIMIT $`+strconv.Itoa(limitParam)+`
	`, args...)
	if err != nil {
		return admin.RequestLogPage{}, err
	}
	defer rows.Close()

	logs := make([]admin.RequestLog, 0, filter.Limit+1)
	for rows.Next() {
		log, err := scanRequestLog(rows)
		if err != nil {
			return admin.RequestLogPage{}, err
		}
		logs = append(logs, log)
	}
	if err := rows.Err(); err != nil {
		return admin.RequestLogPage{}, err
	}
	page := admin.RequestLogPage{Logs: logs}
	if len(logs) > filter.Limit {
		page.HasMore = true
		page.Logs = logs[:filter.Limit]
		last := page.Logs[len(page.Logs)-1]
		cursor := requestLogCursor{
			Version:      requestLogCursorVersion,
			CreatedAt:    last.CreatedAt.UTC(),
			ID:           last.ID,
			FilterDigest: requestLogFilterDigest(filter),
		}
		var err error
		page.NextCursor, err = r.encodeRequestLogCursor(cursor)
		if err != nil {
			return admin.RequestLogPage{}, err
		}
	}
	return page, nil
}

func (r *AdminRepository) StreamRequestLogs(ctx context.Context, filter admin.RequestLogFilter, maxRows int, visit func(admin.RequestLog) error) (admin.RequestLogExportResult, error) {
	if maxRows <= 0 || maxRows > admin.MaxRequestLogExportRows || visit == nil || filter.Cursor != "" || filter.Since.IsZero() || filter.Before.IsZero() {
		return admin.RequestLogExportResult{}, admin.ErrInvalidInput
	}
	whereSQL, args := requestLogFilterSQL(filter)
	args = append(args, maxRows+1)
	limitParam := len(args)
	rows, err := r.pool.Query(ctx, requestLogSelectSQL+whereSQL+`
		ORDER BY l.created_at DESC, l.id DESC
		LIMIT $`+strconv.Itoa(limitParam)+`
	`, args...)
	if err != nil {
		return admin.RequestLogExportResult{}, err
	}
	defer rows.Close()

	result := admin.RequestLogExportResult{}
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if result.RowCount == maxRows {
			result.LimitReached = true
			return result, nil
		}
		log, err := scanRequestLog(rows)
		if err != nil {
			return result, err
		}
		if err := visit(log); err != nil {
			return result, err
		}
		result.RowCount++
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	return result, nil
}

func scanRequestLog(row rowScanner) (admin.RequestLog, error) {
	var log admin.RequestLog
	err := row.Scan(
		&log.ID,
		&log.RequestID,
		&log.UpstreamRequestID,
		&log.ClientKey,
		&log.Provider,
		&log.ProviderAccountID,
		&log.ProviderAccountType,
		&log.ProviderAccountName,
		&log.RoutingPoolID,
		&log.RoutingPoolName,
		&log.RoutingPoolFallbackDepth,
		&log.RoutingPoolFallbackChain,
		&log.RoutingPoolError,
		&log.Model,
		&log.SessionID,
		&log.Route,
		&log.Method,
		&log.StatusCode,
		&log.LatencyMS,
		&log.Error,
		&log.InputTokens,
		&log.OutputTokens,
		&log.TotalTokens,
		&log.CachedInputTokens,
		&log.ReasoningTokens,
		&log.UsageSource,
		&log.EstimatedCostMicrousd,
		&log.PricingMatched,
		&log.GatewayAttemptCount,
		&log.GatewayFallbackCount,
		&log.CreatedAt,
	)
	if err != nil {
		return admin.RequestLog{}, err
	}
	log.CreatedAt = log.CreatedAt.UTC()
	return log, nil
}

func requestLogFilterDigest(filter admin.RequestLogFilter) string {
	since := ""
	if !filter.Since.IsZero() {
		since = filter.Since.UTC().Format(time.RFC3339Nano)
	}
	var before *string
	if !filter.Before.IsZero() {
		value := filter.Before.UTC().Format(time.RFC3339Nano)
		before = &value
	}
	payload, _ := json.Marshal(struct {
		Since             string  `json:"since"`
		Before            *string `json:"before,omitempty"`
		RequestID         string  `json:"requestId"`
		Query             string  `json:"query"`
		StatusClass       string  `json:"statusClass"`
		StatusCode        int     `json:"statusCode"`
		ProviderAccountID int64   `json:"providerAccountId"`
		RoutingPoolID     int64   `json:"routingPoolId"`
		ClientKeyID       int64   `json:"clientKeyId"`
		Model             string  `json:"model"`
		SessionID         string  `json:"sessionId"`
		Error             string  `json:"error"`
		UsageSource       string  `json:"usageSource"`
		RoutingPoolError  string  `json:"routingPoolError"`
		RoutingPoolChain  string  `json:"routingPoolChain"`
		GatewayFallbacks  bool    `json:"gatewayFallbacks"`
	}{
		Since: since, Before: before, RequestID: filter.RequestID, Query: filter.Query,
		StatusClass: filter.StatusClass, StatusCode: filter.StatusCode,
		ProviderAccountID: filter.ProviderAccountID, RoutingPoolID: filter.RoutingPoolID,
		ClientKeyID: filter.ClientKeyID, Model: filter.Model, SessionID: filter.SessionID,
		Error: filter.Error, UsageSource: filter.UsageSource, RoutingPoolError: filter.RoutingPoolError,
		RoutingPoolChain: filter.RoutingPoolChain, GatewayFallbacks: filter.GatewayFallbacks,
	})
	digest := sha256.Sum256(payload)
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func (r *AdminRepository) encodeRequestLogCursor(cursor requestLogCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, r.requestLogCursorSecret)
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(append(payload, mac.Sum(nil)...)), nil
}

func (r *AdminRepository) decodeRequestLogCursor(value string, filter admin.RequestLogFilter) (requestLogCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) <= sha256.Size {
		return requestLogCursor{}, admin.ErrInvalidInput
	}
	payload, signature := decoded[:len(decoded)-sha256.Size], decoded[len(decoded)-sha256.Size:]
	mac := hmac.New(sha256.New, r.requestLogCursorSecret)
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return requestLogCursor{}, admin.ErrInvalidInput
	}
	var cursor requestLogCursor
	if err := json.Unmarshal(payload, &cursor); err != nil ||
		cursor.Version != requestLogCursorVersion || cursor.CreatedAt.IsZero() || cursor.ID < 1 ||
		!hmac.Equal([]byte(cursor.FilterDigest), []byte(requestLogFilterDigest(filter))) {
		return requestLogCursor{}, admin.ErrInvalidInput
	}
	cursor.CreatedAt = cursor.CreatedAt.UTC()
	return cursor, nil
}

type requestLogRetentionLease struct {
	conn       *pgxpool.Conn
	acquireCtx context.Context
	mu         sync.Mutex
	closed     bool
}

func (r *AdminRepository) TryAcquireRequestLogRetention(ctx context.Context) (admin.RequestLogRetentionLease, bool, error) {
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return nil, false, err
	}
	var acquired bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", requestLogRetentionAdvisoryLockID).Scan(&acquired); err != nil {
		discardRequestLogRetentionConnection(conn)
		return nil, false, err
	}
	if !acquired {
		conn.Release()
		return nil, false, nil
	}
	return &requestLogRetentionLease{conn: conn, acquireCtx: ctx}, true, nil
}

func (l *requestLogRetentionLease) DeleteBeforeBatch(ctx context.Context, before time.Time, batchSize int) (int64, error) {
	if batchSize < 1 || batchSize > 10000 || before.IsZero() {
		return 0, admin.ErrInvalidInput
	}
	tx, err := l.conn.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), requestLogRetentionUnlockTimeout)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	tag, err := tx.Exec(ctx, `
		WITH candidates AS (
			SELECT id
			FROM request_logs
			WHERE created_at < $1
			ORDER BY created_at ASC, id ASC
			LIMIT $2
		)
		DELETE FROM request_logs AS logs
		USING candidates
		WHERE logs.id = candidates.id
	`, before.UTC(), batchSize)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	committed = true
	return tag.RowsAffected(), nil
}

func (l *requestLogRetentionLease) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true

	unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(l.acquireCtx), requestLogRetentionUnlockTimeout)
	defer cancel()
	var unlocked bool
	err := l.conn.QueryRow(unlockCtx, "SELECT pg_advisory_unlock($1)", requestLogRetentionAdvisoryLockID).Scan(&unlocked)
	if err == nil && unlocked {
		l.conn.Release()
		return nil
	}
	discardRequestLogRetentionConnection(l.conn)
	if err != nil {
		return err
	}
	return errors.New("request log retention advisory lock was not held")
}

func discardRequestLogRetentionConnection(poolConn *pgxpool.Conn) {
	conn := poolConn.Hijack()
	closeCtx, closeCancel := context.WithTimeout(context.Background(), requestLogRetentionUnlockTimeout)
	defer closeCancel()
	_ = conn.Close(closeCtx)
}

func (r *AdminRepository) GetRequestLogRetentionStats(ctx context.Context, before time.Time) (admin.RequestLogRetentionStats, error) {
	if before.IsZero() {
		return admin.RequestLogRetentionStats{}, admin.ErrInvalidInput
	}
	var stats admin.RequestLogRetentionStats
	err := r.pool.QueryRow(ctx, `
		SELECT
			(SELECT min(created_at) FROM request_logs),
			(SELECT max(created_at) FROM request_logs),
			GREATEST(COALESCE((SELECT reltuples::bigint FROM pg_class WHERE oid = 'request_logs'::regclass), 0), 0),
			(SELECT count(*) FROM request_logs WHERE created_at < $1),
			GREATEST(pg_total_relation_size('request_logs'::regclass), 0)
	`, before.UTC()).Scan(&stats.OldestLogAt, &stats.NewestLogAt, &stats.TotalCountEstimate, &stats.EligibleCount, &stats.RelationSizeBytes)
	if err != nil {
		return admin.RequestLogRetentionStats{}, err
	}
	if stats.OldestLogAt != nil {
		oldest := stats.OldestLogAt.UTC()
		stats.OldestLogAt = &oldest
	}
	if stats.NewestLogAt != nil {
		newest := stats.NewestLogAt.UTC()
		stats.NewestLogAt = &newest
	}
	return stats, nil
}

func requestLogFilterSQL(filter admin.RequestLogFilter) (string, []any) {
	var conditions []string
	var args []any

	switch filter.StatusClass {
	case admin.RequestLogStatusSuccess:
		conditions = append(conditions, "l.status_code >= 200 AND l.status_code < 400")
	case admin.RequestLogStatusClientError:
		conditions = append(conditions, "l.status_code >= 400 AND l.status_code < 500")
	case admin.RequestLogStatusServerError:
		conditions = append(conditions, "l.status_code >= 500")
	}

	if filter.RequestID != "" {
		args = append(args, filter.RequestID)
		conditions = append(conditions, "l.request_id = $"+strconv.Itoa(len(args)))
	}

	if !filter.Since.IsZero() {
		args = append(args, filter.Since.UTC())
		conditions = append(conditions, "l.created_at >= $"+strconv.Itoa(len(args)))
	}

	if !filter.Before.IsZero() {
		args = append(args, filter.Before.UTC())
		conditions = append(conditions, "l.created_at < $"+strconv.Itoa(len(args)))
	}

	if filter.StatusCode > 0 {
		args = append(args, filter.StatusCode)
		conditions = append(conditions, "l.status_code = $"+strconv.Itoa(len(args)))
	}

	if filter.ProviderAccountID > 0 {
		args = append(args, filter.ProviderAccountID)
		conditions = append(conditions, "l.provider_account_id = $"+strconv.Itoa(len(args)))
	}

	if filter.RoutingPoolID > 0 {
		args = append(args, filter.RoutingPoolID)
		conditions = append(conditions, "l.routing_pool_id = $"+strconv.Itoa(len(args)))
	}

	if filter.ClientKeyID > 0 {
		args = append(args, filter.ClientKeyID)
		conditions = append(conditions, "l.client_key_id = $"+strconv.Itoa(len(args)))
	}

	if filter.Model != "" {
		args = append(args, filter.Model)
		conditions = append(conditions, "l.model = $"+strconv.Itoa(len(args)))
	}

	if filter.SessionID != "" {
		args = append(args, filter.SessionID)
		conditions = append(conditions, "l.session_id = $"+strconv.Itoa(len(args)))
	}

	if filter.Error != "" {
		args = append(args, filter.Error)
		conditions = append(conditions, "l.error = $"+strconv.Itoa(len(args)))
	}

	if filter.UsageSource != "" {
		args = append(args, filter.UsageSource)
		conditions = append(conditions, "l.usage_source = $"+strconv.Itoa(len(args)))
	}

	if filter.RoutingPoolError != "" {
		args = append(args, filter.RoutingPoolError)
		conditions = append(conditions, "l.routing_pool_error = $"+strconv.Itoa(len(args)))
	}

	if filter.RoutingPoolChain != "" {
		args = append(args, filter.RoutingPoolChain)
		conditions = append(conditions, "l.routing_pool_fallback_chain = $"+strconv.Itoa(len(args)))
	}

	if filter.GatewayFallbacks {
		conditions = append(conditions, "l.gateway_fallback_count > 0")
	}

	if filter.Query != "" {
		args = append(args, filter.Query)
		param := "$" + strconv.Itoa(len(args))
		conditions = append(conditions, `(
			l.request_id ILIKE '%' || `+param+` || '%'
			OR COALESCE(k.name, '') ILIKE '%' || `+param+` || '%'
			OR COALESCE(k.prefix, '') ILIKE '%' || `+param+` || '%'
			OR COALESCE(l.provider, '') ILIKE '%' || `+param+` || '%'
			OR COALESCE(l.provider_account_type, '') ILIKE '%' || `+param+` || '%'
			OR COALESCE(l.provider_account_name, '') ILIKE '%' || `+param+` || '%'
			OR COALESCE(a.display_name, '') ILIKE '%' || `+param+` || '%'
			OR COALESCE(a.name, '') ILIKE '%' || `+param+` || '%'
			OR COALESCE(l.model, '') ILIKE '%' || `+param+` || '%'
			OR COALESCE(l.session_id, '') ILIKE '%' || `+param+` || '%'
			OR COALESCE(l.route, '') ILIKE '%' || `+param+` || '%'
			OR COALESCE(l.method, '') ILIKE '%' || `+param+` || '%'
			OR COALESCE(l.routing_pool_fallback_chain, '') ILIKE '%' || `+param+` || '%'
			OR COALESCE(l.routing_pool_error, '') ILIKE '%' || `+param+` || '%'
			OR COALESCE(l.error, '') ILIKE '%' || `+param+` || '%'
			OR l.status_code::text ILIKE '%' || `+param+` || '%'
		)`)
	}

	if len(conditions) == 0 {
		return "", nil
	}
	return "WHERE " + strings.Join(conditions, " AND "), args
}

func (r *AdminRepository) GetUsageSummary(ctx context.Context, since time.Time, groupBy string) (admin.UsageSummary, error) {
	groupExpr, labelExpr, joinSQL, ok := usageSummaryGroupSQL(groupBy)
	if !ok {
		return admin.UsageSummary{}, admin.ErrInvalidInput
	}
	rows, err := r.pool.Query(ctx, `
		SELECT
			`+groupExpr+`,
			`+labelExpr+`,
			COUNT(*),
			COALESCE(SUM(l.input_tokens), 0),
			COALESCE(SUM(l.output_tokens), 0),
			COALESCE(SUM(l.total_tokens), 0),
			COALESCE(SUM(l.cached_input_tokens), 0),
			COALESCE(SUM(l.reasoning_tokens), 0),
			COALESCE(SUM(l.estimated_cost_microusd), 0)
		FROM request_logs l
		`+joinSQL+`
		WHERE l.created_at >= $1
		GROUP BY 1, 2
		ORDER BY COALESCE(SUM(l.estimated_cost_microusd), 0) DESC, COUNT(*) DESC, `+labelExpr+` ASC
	`, since)
	if err != nil {
		return admin.UsageSummary{}, err
	}
	defer rows.Close()

	var summary admin.UsageSummary
	for rows.Next() {
		var row admin.UsageSummaryRow
		if err := rows.Scan(
			&row.ID,
			&row.Label,
			&row.Requests,
			&row.InputTokens,
			&row.OutputTokens,
			&row.TotalTokens,
			&row.CachedInputTokens,
			&row.ReasoningTokens,
			&row.EstimatedCostMicrousd,
		); err != nil {
			return admin.UsageSummary{}, err
		}
		summary.Rows = append(summary.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return admin.UsageSummary{}, err
	}
	return summary, nil
}

func usageSummaryGroupSQL(groupBy string) (groupExpr, labelExpr, joinSQL string, ok bool) {
	switch groupBy {
	case "client_key":
		return "COALESCE(k.id::text, 'unknown')", "COALESCE(k.name || ' (' || k.prefix || ')', 'Unknown key')", "LEFT JOIN client_api_keys k ON k.id = l.client_key_id", true
	case "provider_account":
		providerExpr := "COALESCE(NULLIF(l.provider, ''), NULLIF(a.provider, ''), 'unknown')"
		accountLabelExpr := "COALESCE(NULLIF(l.provider_account_name, ''), NULLIF(a.display_name, ''), a.name, 'Unassigned')"
		return providerExpr + " || '/' || COALESCE(l.provider_account_id::text, 'unassigned')", providerExpr + " || ' / ' || " + accountLabelExpr, "LEFT JOIN provider_accounts a ON a.id = l.provider_account_id", true
	case "routing_pool":
		return "COALESCE(l.routing_pool_id::text, 'global')", "COALESCE(NULLIF(l.routing_pool_name, ''), 'Global pool')", "", true
	case "routing_pool_chain":
		return "COALESCE(NULLIF(l.routing_pool_fallback_chain, ''), 'none')", "COALESCE(NULLIF(l.routing_pool_fallback_chain, ''), 'No fallback chain')", "", true
	case "model":
		return "COALESCE(NULLIF(l.model, ''), 'unknown')", "COALESCE(NULLIF(l.model, ''), 'Unknown model')", "", true
	case "session":
		return "COALESCE(NULLIF(l.session_id, ''), 'none')", "COALESCE(NULLIF(l.session_id, ''), 'No session')", "", true
	case "usage_source":
		return "COALESCE(NULLIF(l.usage_source, ''), 'missing')", "COALESCE(NULLIF(l.usage_source, ''), 'Missing usage')", "", true
	default:
		return "", "", "", false
	}
}

func (r *AdminRepository) GetModelSettings(ctx context.Context) (admin.ModelSettings, error) {
	var raw []byte
	err := r.pool.QueryRow(ctx, `
		SELECT value
		FROM settings
		WHERE key = $1
	`, modelSettingsKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.ModelSettings{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.ModelSettings{}, err
	}

	var settings admin.ModelSettings
	if err := json.Unmarshal(raw, &settings); err != nil {
		return admin.ModelSettings{}, err
	}
	return settings, nil
}

func (r *AdminRepository) GetUsagePricing(ctx context.Context) (admin.UsagePricing, error) {
	var raw []byte
	err := r.pool.QueryRow(ctx, `
		SELECT value
		FROM settings
		WHERE key = $1
	`, usagePricingKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.UsagePricing{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.UsagePricing{}, err
	}

	var pricing admin.UsagePricing
	if err := json.Unmarshal(raw, &pricing); err != nil {
		return admin.UsagePricing{}, err
	}
	return pricing, nil
}

func (r *AdminRepository) SaveUsagePricing(ctx context.Context, pricing admin.UsagePricing) (admin.UsagePricing, error) {
	value, err := json.Marshal(pricing)
	if err != nil {
		return admin.UsagePricing{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return admin.UsagePricing{}, err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `
		INSERT INTO settings (key, value, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE
		SET value = EXCLUDED.value,
			updated_at = now()
	`, usagePricingKey, value)
	if err != nil {
		return admin.UsagePricing{}, err
	}
	if err := insertIntentSystemEvent(ctx, tx, systemevent.Target{Type: "usage_pricing", ID: "default"}, nil); err != nil {
		return admin.UsagePricing{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.UsagePricing{}, err
	}
	return pricing, nil
}

func (r *AdminRepository) SaveModelSettings(ctx context.Context, settings admin.ModelSettings) (admin.ModelSettings, error) {
	value, err := json.Marshal(settings)
	if err != nil {
		return admin.ModelSettings{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return admin.ModelSettings{}, err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `
		INSERT INTO settings (key, value, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE
		SET value = EXCLUDED.value,
			updated_at = now()
	`, modelSettingsKey, value)
	if err != nil {
		return admin.ModelSettings{}, err
	}
	if err := insertIntentSystemEvent(ctx, tx, systemevent.Target{Type: "model_settings", ID: "default"}, nil); err != nil {
		return admin.ModelSettings{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.ModelSettings{}, err
	}
	return settings, nil
}

func (r *AdminRepository) LoadGatewaySettings(ctx context.Context) (admin.GatewaySettingsRecord, error) {
	var raw []byte
	var updatedAt time.Time
	err := r.pool.QueryRow(ctx, `
		SELECT value, updated_at
		FROM settings
		WHERE key = $1
	`, gatewaySettingsKey).Scan(&raw, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.GatewaySettingsRecord{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.GatewaySettingsRecord{}, err
	}

	return decodeGatewaySettingsRecord(raw, updatedAt)
}

func decodeGatewaySettingsRecord(raw []byte, updatedAt time.Time) (admin.GatewaySettingsRecord, error) {
	var settings admin.GatewaySettings
	if err := json.Unmarshal(raw, &settings); err != nil {
		return admin.GatewaySettingsRecord{}, admin.ErrGatewaySettingsInvalid
	}
	return admin.GatewaySettingsRecord{Settings: settings, UpdatedAt: updatedAt.UTC()}, nil
}

func (r *AdminRepository) SaveGatewaySettings(ctx context.Context, settings admin.GatewaySettings) (admin.GatewaySettingsRecord, error) {
	value, err := json.Marshal(settings)
	if err != nil {
		return admin.GatewaySettingsRecord{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return admin.GatewaySettingsRecord{}, err
	}
	defer tx.Rollback(ctx)
	var updatedAt time.Time
	err = tx.QueryRow(ctx, `
		INSERT INTO settings (key, value, updated_at)
		VALUES ($1, $2, clock_timestamp())
		ON CONFLICT (key) DO UPDATE
		SET value = EXCLUDED.value,
			updated_at = clock_timestamp()
		RETURNING updated_at
	`, gatewaySettingsKey, value).Scan(&updatedAt)
	if err != nil {
		return admin.GatewaySettingsRecord{}, err
	}
	if err := insertIntentSystemEvent(ctx, tx, systemevent.Target{Type: "gateway_settings", ID: "default"}, nil); err != nil {
		return admin.GatewaySettingsRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.GatewaySettingsRecord{}, err
	}
	return admin.GatewaySettingsRecord{Settings: settings, UpdatedAt: updatedAt.UTC()}, nil
}
