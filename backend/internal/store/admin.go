package store

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AdminRepository struct {
	pool *pgxpool.Pool
}

const modelSettingsKey = "model_settings"
const usagePricingKey = "usage_pricing"
const gatewaySettingsKey = "gateway_settings"
const apiKeySelectColumns = `
	k.id, k.name, k.prefix, k.created_at, k.last_used_at, k.revoked_at, k.disabled_at,
	k.model_policy, k.requests_per_minute, k.tokens_per_minute,
	k.request_budget_24h, k.token_budget_24h, k.request_budget_30d, k.token_budget_30d,
	k.routing_pool_id, COALESCE(rp.name, '')
`

func NewAdminRepository(pool *pgxpool.Pool) *AdminRepository {
	return &AdminRepository{pool: pool}
}

func scanAPIKey(key *admin.APIKey) []any {
	return []any{
		&key.ID,
		&key.Name,
		&key.Prefix,
		&key.CreatedAt,
		&key.LastUsedAt,
		&key.RevokedAt,
		&key.DisabledAt,
		&key.ModelPolicy,
		&key.RequestsPerMinute,
		&key.TokensPerMinute,
		&key.RequestBudget24h,
		&key.TokenBudget24h,
		&key.RequestBudget30d,
		&key.TokenBudget30d,
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

func (r *AdminRepository) UpdateAdminUsername(ctx context.Context, id int64, username string) (admin.Admin, error) {
	var updated admin.Admin
	err := r.pool.QueryRow(ctx, `
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
	return updated, nil
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
	var id int64
	err := r.pool.QueryRow(ctx, `
		INSERT INTO client_api_keys (name, key_hash, prefix)
		VALUES ($1, $2, $3)
		RETURNING id
	`, name, hash, prefix).Scan(&id)
	if err != nil {
		return admin.APIKey{}, err
	}
	return r.loadAPIKey(ctx, id)
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

func (r *AdminRepository) RevokeAPIKey(ctx context.Context, id int64) (admin.APIKey, error) {
	var updatedID int64
	err := r.pool.QueryRow(ctx, `
		UPDATE client_api_keys
		SET revoked_at = COALESCE(revoked_at, now())
		WHERE id = $1
		RETURNING id
	`, id).Scan(&updatedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.APIKey{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.APIKey{}, err
	}
	return r.loadAPIKey(ctx, updatedID)
}

func (r *AdminRepository) FindAPIKeyByHash(ctx context.Context, hash string, _ time.Time) (admin.APIKey, error) {
	var found admin.APIKey
	err := r.pool.QueryRow(ctx, `
		SELECT `+apiKeySelectColumns+`
		FROM client_api_keys k
		LEFT JOIN routing_pools rp ON rp.id = k.routing_pool_id
		WHERE k.key_hash = $1
			AND k.revoked_at IS NULL
			AND k.disabled_at IS NULL
	`, hash).Scan(scanAPIKey(&found)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.APIKey{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.APIKey{}, err
	}
	if found.ModelPolicy == admin.APIKeyModelPolicySelected {
		models, err := r.ListAPIKeyModels(ctx, found.ID)
		if err != nil {
			return admin.APIKey{}, err
		}
		found.AllowedModels = models
	}
	return found, nil
}

func (r *AdminRepository) UpdateAPIKeyName(ctx context.Context, id int64, name string) (admin.APIKey, error) {
	var updatedID int64
	err := r.pool.QueryRow(ctx, `
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
	return r.loadAPIKey(ctx, updatedID)
}

func (r *AdminRepository) SetAPIKeyDisabled(ctx context.Context, id int64, disabled bool) (admin.APIKey, error) {
	var updatedID int64
	err := r.pool.QueryRow(ctx, `
		UPDATE client_api_keys
		SET disabled_at = CASE WHEN $2 THEN COALESCE(disabled_at, now()) ELSE NULL END
		WHERE id = $1
			AND revoked_at IS NULL
		RETURNING id
	`, id, disabled).Scan(&updatedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.APIKey{}, admin.ErrNotFound
	}
	if err != nil {
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
	if err := tx.Commit(ctx); err != nil {
		return admin.APIKey{}, err
	}
	return r.loadAPIKey(ctx, updatedID)
}

func (r *AdminRepository) UpdateAPIKeyLimits(ctx context.Context, id int64, requestsPerMinute, tokensPerMinute int) (admin.APIKey, error) {
	var updatedID int64
	err := r.pool.QueryRow(ctx, `
		UPDATE client_api_keys
		SET requests_per_minute = $2,
			tokens_per_minute = $3
		WHERE id = $1
			AND revoked_at IS NULL
		RETURNING id
	`, id, requestsPerMinute, tokensPerMinute).Scan(&updatedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.APIKey{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.APIKey{}, err
	}
	return r.loadAPIKey(ctx, updatedID)
}

func (r *AdminRepository) UpdateAPIKeyBudgets(ctx context.Context, id int64, requestBudget24h, tokenBudget24h, requestBudget30d, tokenBudget30d int) (admin.APIKey, error) {
	var updatedID int64
	err := r.pool.QueryRow(ctx, `
		UPDATE client_api_keys
		SET request_budget_24h = $2,
			token_budget_24h = $3,
			request_budget_30d = $4,
			token_budget_30d = $5
		WHERE id = $1
			AND revoked_at IS NULL
		RETURNING id
	`, id, requestBudget24h, tokenBudget24h, requestBudget30d, tokenBudget30d).Scan(&updatedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.APIKey{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.APIKey{}, err
	}
	return r.loadAPIKey(ctx, updatedID)
}

func (r *AdminRepository) ListRoutingPools(ctx context.Context) ([]admin.RoutingPool, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id
		FROM routing_pools
		ORDER BY name ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	pools := make([]admin.RoutingPool, 0, len(ids))
	for _, id := range ids {
		pool, err := r.getRoutingPool(ctx, id)
		if err != nil {
			return nil, err
		}
		pools = append(pools, pool)
	}
	return pools, nil
}

func (r *AdminRepository) CreateRoutingPool(ctx context.Context, name, description string, enabled bool, fallbackPoolID *int64) (admin.RoutingPool, error) {
	if err := r.validateRoutingPoolFallback(ctx, 0, fallbackPoolID); err != nil {
		return admin.RoutingPool{}, err
	}
	var id int64
	err := r.pool.QueryRow(ctx, `
		INSERT INTO routing_pools (name, description, enabled, fallback_pool_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, name, description, enabled, fallbackPoolID).Scan(&id)
	if err != nil {
		return admin.RoutingPool{}, err
	}
	return r.getRoutingPool(ctx, id)
}

func (r *AdminRepository) UpdateRoutingPool(ctx context.Context, id int64, name, description string, enabled bool, fallbackPoolID *int64) (admin.RoutingPool, error) {
	if err := r.validateRoutingPoolFallback(ctx, id, fallbackPoolID); err != nil {
		return admin.RoutingPool{}, err
	}
	var updatedID int64
	err := r.pool.QueryRow(ctx, `
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
	tag, err := r.pool.Exec(ctx, `DELETE FROM routing_pools WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return admin.ErrNotFound
	}
	return nil
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
	if err := tx.Commit(ctx); err != nil {
		return admin.RoutingPool{}, err
	}
	return r.getRoutingPool(ctx, id)
}

func (r *AdminRepository) UpdateAPIKeyRoutingPool(ctx context.Context, id int64, routingPoolID *int64) (admin.APIKey, error) {
	var updatedID int64
	err := r.pool.QueryRow(ctx, `
		UPDATE client_api_keys
		SET routing_pool_id = $2
		WHERE id = $1
			AND revoked_at IS NULL
		RETURNING id
	`, id, routingPoolID).Scan(&updatedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.APIKey{}, admin.ErrNotFound
	}
	if err != nil {
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
	usage := admin.APIKeyBudgetUsage{KeyID: keyID}
	err := r.pool.QueryRow(ctx, `
		SELECT
			COALESCE(COUNT(*) FILTER (WHERE created_at >= $2), 0),
			COALESCE(SUM(total_tokens) FILTER (WHERE created_at >= $2), 0),
			COALESCE(COUNT(*) FILTER (WHERE created_at >= $3), 0),
			COALESCE(SUM(total_tokens) FILTER (WHERE created_at >= $3), 0)
		FROM request_logs
		WHERE client_key_id = $1
	`, keyID, now.Add(-24*time.Hour), now.Add(-30*24*time.Hour)).Scan(
		&usage.RequestsUsed24h,
		&usage.TokensUsed24h,
		&usage.RequestsUsed30d,
		&usage.TokensUsed30d,
	)
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
	for i := range keys {
		if keys[i].ModelPolicy != admin.APIKeyModelPolicySelected {
			continue
		}
		models, err := r.ListAPIKeyModels(ctx, keys[i].ID)
		if err != nil {
			return err
		}
		keys[i].AllowedModels = models
	}
	return nil
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

func (r *AdminRepository) ListRequestLogs(ctx context.Context, filter admin.RequestLogFilter) ([]admin.RequestLog, error) {
	whereSQL, args := requestLogFilterSQL(filter)
	args = append(args, filter.Limit)
	limitParam := len(args)

	rows, err := r.pool.Query(ctx, `
		SELECT
			l.id,
			l.request_id,
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
		`+whereSQL+`
		ORDER BY l.created_at DESC, l.id DESC
		LIMIT $`+strconv.Itoa(limitParam)+`
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []admin.RequestLog
	for rows.Next() {
		var log admin.RequestLog
		if err := rows.Scan(
			&log.ID,
			&log.RequestID,
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
		); err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return logs, nil
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
	_, err = r.pool.Exec(ctx, `
		INSERT INTO settings (key, value, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE
		SET value = EXCLUDED.value,
			updated_at = now()
	`, usagePricingKey, value)
	if err != nil {
		return admin.UsagePricing{}, err
	}
	return pricing, nil
}

func (r *AdminRepository) SaveModelSettings(ctx context.Context, settings admin.ModelSettings) (admin.ModelSettings, error) {
	value, err := json.Marshal(settings)
	if err != nil {
		return admin.ModelSettings{}, err
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO settings (key, value, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE
		SET value = EXCLUDED.value,
			updated_at = now()
	`, modelSettingsKey, value)
	if err != nil {
		return admin.ModelSettings{}, err
	}
	return settings, nil
}

func (r *AdminRepository) GetGatewaySettings(ctx context.Context) (admin.GatewaySettings, error) {
	var raw []byte
	err := r.pool.QueryRow(ctx, `
		SELECT value
		FROM settings
		WHERE key = $1
	`, gatewaySettingsKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.GatewaySettings{}, admin.ErrNotFound
	}
	if err != nil {
		return admin.GatewaySettings{}, err
	}

	var settings admin.GatewaySettings
	if err := json.Unmarshal(raw, &settings); err != nil {
		return admin.GatewaySettings{}, err
	}
	return settings, nil
}

func (r *AdminRepository) SaveGatewaySettings(ctx context.Context, settings admin.GatewaySettings) (admin.GatewaySettings, error) {
	value, err := json.Marshal(settings)
	if err != nil {
		return admin.GatewaySettings{}, err
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO settings (key, value, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE
		SET value = EXCLUDED.value,
			updated_at = now()
	`, gatewaySettingsKey, value)
	if err != nil {
		return admin.GatewaySettings{}, err
	}
	return settings, nil
}
