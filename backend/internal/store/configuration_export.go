package store

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/KnowSky404/N2API/backend/internal/admin"
	"github.com/jackc/pgx/v5"
)

func (r *AdminRepository) ExportConfigurationSnapshot(ctx context.Context) (admin.ConfigurationSnapshot, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return admin.ConfigurationSnapshot{}, err
	}
	defer tx.Rollback(ctx)

	snapshot := admin.ConfigurationSnapshot{}
	if snapshot.RoutingPools, err = exportRoutingPools(ctx, tx); err != nil {
		return admin.ConfigurationSnapshot{}, err
	}
	if snapshot.APIKeyTemplates, err = exportAPIKeyTemplates(ctx, tx); err != nil {
		return admin.ConfigurationSnapshot{}, err
	}
	if snapshot.ProviderAccounts, err = exportProviderAccounts(ctx, tx); err != nil {
		return admin.ConfigurationSnapshot{}, err
	}
	if err = exportConfigurationSettings(ctx, tx, &snapshot); err != nil {
		return admin.ConfigurationSnapshot{}, err
	}
	if snapshot.FingerprintProfiles, err = exportFingerprintProfiles(ctx, tx); err != nil {
		return admin.ConfigurationSnapshot{}, err
	}
	if snapshot.ErrorPassthroughRules, err = exportErrorPassthroughRules(ctx, tx); err != nil {
		return admin.ConfigurationSnapshot{}, err
	}
	if err = validateConfigurationSnapshotReferences(snapshot); err != nil {
		return admin.ConfigurationSnapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.ConfigurationSnapshot{}, err
	}
	return snapshot, nil
}

func configurationRef(kind string, id int64) string {
	return kind + ":" + strconv.FormatInt(id, 10)
}

func exportRoutingPools(ctx context.Context, tx pgx.Tx) ([]admin.ConfigurationRoutingPool, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, name, description, enabled, fallback_pool_id
		FROM routing_pools
		ORDER BY name ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	pools := []admin.ConfigurationRoutingPool{}
	for rows.Next() {
		var id int64
		var fallbackID *int64
		var pool admin.ConfigurationRoutingPool
		if err := rows.Scan(&id, &pool.Name, &pool.Description, &pool.Enabled, &fallbackID); err != nil {
			return nil, err
		}
		pool.Ref = configurationRef("routing_pool", id)
		pool.Accounts = []admin.ConfigurationRoutingPoolMembership{}
		if fallbackID != nil {
			pool.FallbackRef = configurationRef("routing_pool", *fallbackID)
		}
		pools = append(pools, pool)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	byRef := make(map[string]*admin.ConfigurationRoutingPool, len(pools))
	for i := range pools {
		byRef[pools[i].Ref] = &pools[i]
	}
	memberships, err := tx.Query(ctx, `
		SELECT pool_id, account_id, priority
		FROM routing_pool_accounts
		ORDER BY pool_id ASC, priority ASC, account_id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer memberships.Close()
	for memberships.Next() {
		var poolID, accountID int64
		var priority int
		if err := memberships.Scan(&poolID, &accountID, &priority); err != nil {
			return nil, err
		}
		pool := byRef[configurationRef("routing_pool", poolID)]
		if pool == nil {
			return nil, fmt.Errorf("configuration export routing pool membership references missing pool")
		}
		pool.Accounts = append(pool.Accounts, admin.ConfigurationRoutingPoolMembership{
			AccountRef: configurationRef("provider_account", accountID), Priority: priority,
		})
	}
	if err := memberships.Err(); err != nil {
		return nil, err
	}
	return pools, nil
}

func exportAPIKeyTemplates(ctx context.Context, tx pgx.Tx) ([]admin.ConfigurationAPIKeyTemplate, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, name, disabled_at IS NULL, model_policy,
			requests_per_minute, tokens_per_minute,
			request_budget_24h, token_budget_24h, cost_budget_microusd_24h,
			request_budget_30d, token_budget_30d, cost_budget_microusd_30d,
			routing_pool_id
		FROM client_api_keys
		WHERE revoked_at IS NULL
		ORDER BY name ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := []admin.ConfigurationAPIKeyTemplate{}
	for rows.Next() {
		var id int64
		var routingPoolID *int64
		var key admin.ConfigurationAPIKeyTemplate
		if err := rows.Scan(
			&id, &key.Name, &key.Enabled, &key.ModelPolicy,
			&key.RequestsPerMinute, &key.TokensPerMinute,
			&key.RequestBudget24h, &key.TokenBudget24h, &key.CostBudgetMicrousd24h,
			&key.RequestBudget30d, &key.TokenBudget30d, &key.CostBudgetMicrousd30d,
			&routingPoolID,
		); err != nil {
			return nil, err
		}
		key.Ref = configurationRef("api_key_template", id)
		key.AllowedModels = []string{}
		if routingPoolID != nil {
			key.RoutingPoolRef = configurationRef("routing_pool", *routingPoolID)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	byRef := make(map[string]int, len(keys))
	for i := range keys {
		byRef[keys[i].Ref] = i
	}
	models, err := tx.Query(ctx, `
		SELECT client_key_id, model
		FROM client_api_key_models
		ORDER BY client_key_id ASC, model ASC
	`)
	if err != nil {
		return nil, err
	}
	defer models.Close()
	for models.Next() {
		var keyID int64
		var model string
		if err := models.Scan(&keyID, &model); err != nil {
			return nil, err
		}
		if index, ok := byRef[configurationRef("api_key_template", keyID)]; ok {
			keys[index].AllowedModels = append(keys[index].AllowedModels, model)
		}
	}
	return keys, models.Err()
}

func exportProviderAccounts(ctx context.Context, tx pgx.Tx) ([]admin.ConfigurationProviderAccount, error) {
	rows, err := tx.Query(ctx, `
		SELECT a.id, a.provider, a.account_type, a.name, COALESCE(c.base_url, ''),
			a.enabled, a.priority, a.load_factor, a.max_concurrent_requests, a.fingerprint_profile_id
		FROM provider_accounts a
		LEFT JOIN provider_account_credentials c ON c.account_id = a.id
		ORDER BY a.provider ASC, a.account_type ASC, a.name ASC, a.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := []admin.ConfigurationProviderAccount{}
	for rows.Next() {
		var id int64
		var fingerprintID *int64
		var account admin.ConfigurationProviderAccount
		if err := rows.Scan(
			&id, &account.Provider, &account.AccountType, &account.Name, &account.BaseURL,
			&account.Enabled, &account.Priority, &account.LoadFactor, &account.MaxConcurrentRequests, &fingerprintID,
		); err != nil {
			return nil, err
		}
		account.Ref = configurationRef("provider_account", id)
		account.BaseURL = sanitizeConfigurationBaseURL(account.BaseURL)
		account.Models = []admin.ConfigurationProviderModel{}
		if fingerprintID != nil {
			account.FingerprintProfileRef = configurationRef("fingerprint_profile", *fingerprintID)
		}
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	byRef := make(map[string]int, len(accounts))
	for i := range accounts {
		byRef[accounts[i].Ref] = i
	}
	models, err := tx.Query(ctx, `
		SELECT account_id, model, enabled, source
		FROM provider_account_models
		ORDER BY account_id ASC, model ASC
	`)
	if err != nil {
		return nil, err
	}
	defer models.Close()
	for models.Next() {
		var accountID int64
		var model admin.ConfigurationProviderModel
		if err := models.Scan(&accountID, &model.Model, &model.Enabled, &model.Source); err != nil {
			return nil, err
		}
		index, ok := byRef[configurationRef("provider_account", accountID)]
		if !ok {
			return nil, fmt.Errorf("configuration export model references missing provider account")
		}
		accounts[index].Models = append(accounts[index].Models, model)
	}
	return accounts, models.Err()
}

func sanitizeConfigurationBaseURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/")
}

func exportConfigurationSettings(ctx context.Context, tx pgx.Tx, snapshot *admin.ConfigurationSnapshot) error {
	rows, err := tx.Query(ctx, `
		SELECT key, value
		FROM settings
		WHERE key = ANY($1)
		ORDER BY key ASC
	`, []string{gatewaySettingsKey, modelSettingsKey, usagePricingKey})
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var value []byte
		if err := rows.Scan(&key, &value); err != nil {
			return err
		}
		switch key {
		case modelSettingsKey:
			if err := json.Unmarshal(value, &snapshot.ModelSettings); err != nil {
				return err
			}
			snapshot.ModelSettingsPresent = true
		case usagePricingKey:
			if err := json.Unmarshal(value, &snapshot.UsagePricing); err != nil {
				return err
			}
			snapshot.UsagePricingPresent = true
		case gatewaySettingsKey:
			if err := json.Unmarshal(value, &snapshot.GatewaySettings); err != nil {
				return err
			}
			snapshot.GatewaySettingsPresent = true
		}
	}
	return rows.Err()
}

func exportFingerprintProfiles(ctx context.Context, tx pgx.Tx) ([]admin.ConfigurationFingerprintProfile, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, system_key, name, description, user_agent, tls_fingerprint, headers_json, enabled
		FROM fingerprint_profiles
		ORDER BY CASE WHEN system_key <> '' THEN 0 ELSE 1 END, name ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	profiles := []admin.ConfigurationFingerprintProfile{}
	for rows.Next() {
		var id int64
		var headersRaw []byte
		var profile admin.ConfigurationFingerprintProfile
		if err := rows.Scan(
			&id, &profile.SystemKey, &profile.Name, &profile.Description, &profile.UserAgent,
			&profile.TLSFingerprint, &headersRaw, &profile.Enabled,
		); err != nil {
			return nil, err
		}
		profile.Ref = configurationRef("fingerprint_profile", id)
		profile.Headers, err = redactConfigurationHeaders(headersRaw)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	return profiles, rows.Err()
}

func exportErrorPassthroughRules(ctx context.Context, tx pgx.Tx) ([]admin.ConfigurationErrorPassthroughRule, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, pattern, match_type, description, enabled, priority
		FROM error_passthrough_rules
		ORDER BY priority ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	rules := []admin.ConfigurationErrorPassthroughRule{}
	for rows.Next() {
		var id int64
		var rule admin.ConfigurationErrorPassthroughRule
		if err := rows.Scan(&id, &rule.Pattern, &rule.MatchType, &rule.Description, &rule.Enabled, &rule.Priority); err != nil {
			return nil, err
		}
		rule.Ref = configurationRef("error_passthrough_rule", id)
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func redactConfigurationHeaders(raw []byte) (map[string]string, error) {
	headers := map[string]string{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &headers); err != nil {
			return nil, err
		}
	}
	redacted := make(map[string]string, len(headers))
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		redacted[key] = admin.ConfigurationRedactedValue
	}
	return redacted, nil
}

func validateConfigurationSnapshotReferences(snapshot admin.ConfigurationSnapshot) error {
	pools := make(map[string]struct{}, len(snapshot.RoutingPools))
	accounts := make(map[string]struct{}, len(snapshot.ProviderAccounts))
	profiles := make(map[string]struct{}, len(snapshot.FingerprintProfiles))
	for _, pool := range snapshot.RoutingPools {
		pools[pool.Ref] = struct{}{}
	}
	for _, account := range snapshot.ProviderAccounts {
		accounts[account.Ref] = struct{}{}
	}
	for _, profile := range snapshot.FingerprintProfiles {
		profiles[profile.Ref] = struct{}{}
	}
	for _, pool := range snapshot.RoutingPools {
		if pool.FallbackRef != "" {
			if _, ok := pools[pool.FallbackRef]; !ok {
				return fmt.Errorf("configuration export routing pool references missing fallback pool")
			}
		}
		for _, membership := range pool.Accounts {
			if _, ok := accounts[membership.AccountRef]; !ok {
				return fmt.Errorf("configuration export routing pool references missing provider account")
			}
		}
	}
	for _, key := range snapshot.APIKeyTemplates {
		if key.RoutingPoolRef != "" {
			if _, ok := pools[key.RoutingPoolRef]; !ok {
				return fmt.Errorf("configuration export API key template references missing routing pool")
			}
		}
	}
	for _, account := range snapshot.ProviderAccounts {
		if account.FingerprintProfileRef != "" {
			if _, ok := profiles[account.FingerprintProfileRef]; !ok {
				return fmt.Errorf("configuration export provider account references missing fingerprint profile")
			}
		}
	}
	return nil
}
