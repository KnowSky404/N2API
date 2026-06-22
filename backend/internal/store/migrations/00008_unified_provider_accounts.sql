-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS provider_accounts (
    id BIGSERIAL PRIMARY KEY,
    provider TEXT NOT NULL,
    account_type TEXT NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    subject TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT true,
    priority INTEGER NOT NULL DEFAULT 100,
    status TEXT NOT NULL DEFAULT 'active',
    status_reason TEXT NOT NULL DEFAULT '',
    last_used_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    last_error_at TIMESTAMPTZ,
    failure_count INTEGER NOT NULL DEFAULT 0,
    circuit_open_until TIMESTAMPTZ,
    rate_limited_until TIMESTAMPTZ,
    fingerprint_hash TEXT NOT NULL DEFAULT '',
    user_agent_hash TEXT NOT NULL DEFAULT '',
    ip_hash TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS provider_accounts_provider_type_subject_idx
    ON provider_accounts (provider, account_type, subject)
    WHERE subject <> '';

CREATE INDEX IF NOT EXISTS provider_accounts_schedulable_idx
    ON provider_accounts (provider, enabled, status, priority, last_used_at, id);

CREATE TABLE IF NOT EXISTS provider_account_credentials (
    account_id BIGINT PRIMARY KEY REFERENCES provider_accounts(id) ON DELETE CASCADE,
    credential_type TEXT NOT NULL,
    encrypted_access_token TEXT NOT NULL DEFAULT '',
    encrypted_refresh_token TEXT NOT NULL DEFAULT '',
    encrypted_id_token TEXT NOT NULL DEFAULT '',
    access_token_expires_at TIMESTAMPTZ,
    last_refresh_at TIMESTAMPTZ,
    last_refresh_error TEXT NOT NULL DEFAULT '',
    last_refresh_error_at TIMESTAMPTZ,
    encrypted_api_key TEXT NOT NULL DEFAULT '',
    base_url TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS provider_account_models (
    id BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    source TEXT NOT NULL DEFAULT 'manual',
    last_seen_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (account_id, model)
);

CREATE INDEX IF NOT EXISTS provider_account_models_provider_model_enabled_idx
    ON provider_account_models (provider, model, enabled, account_id);

CREATE INDEX IF NOT EXISTS provider_account_models_account_enabled_idx
    ON provider_account_models (account_id, enabled, model);

ALTER TABLE client_api_keys ADD COLUMN IF NOT EXISTS model_policy TEXT NOT NULL DEFAULT 'all';

CREATE TABLE IF NOT EXISTS client_api_key_models (
    id BIGSERIAL PRIMARY KEY,
    client_key_id BIGINT NOT NULL REFERENCES client_api_keys(id) ON DELETE CASCADE,
    model TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (client_key_id, model)
);

INSERT INTO provider_accounts (
    id, provider, account_type, name, subject, display_name, enabled, priority,
    status, status_reason, last_used_at, last_error, last_error_at, failure_count,
    circuit_open_until, rate_limited_until, fingerprint_hash, user_agent_hash, ip_hash,
    created_at, updated_at
)
SELECT
    id, provider, 'codex_oauth', name, subject, display_name, enabled, priority,
    status, status_reason, last_used_at, last_error, last_error_at, failure_count,
    circuit_open_until, rate_limited_until, fingerprint_hash, user_agent_hash, ip_hash,
    created_at, updated_at
FROM oauth_accounts
ON CONFLICT (id) DO NOTHING;

INSERT INTO provider_account_credentials (
    account_id, credential_type, encrypted_access_token, encrypted_refresh_token, encrypted_id_token,
    access_token_expires_at, last_refresh_at, last_refresh_error, last_refresh_error_at,
    metadata, created_at, updated_at
)
SELECT
    id, 'oauth_token', encrypted_access_token, encrypted_refresh_token, encrypted_id_token,
    access_token_expires_at, last_refresh_at, last_refresh_error, last_refresh_error_at,
    metadata, created_at, updated_at
FROM oauth_accounts
ON CONFLICT (account_id) DO NOTHING;

INSERT INTO provider_account_models (
    id, account_id, provider, model, enabled, source, last_seen_at, last_error, metadata, created_at, updated_at
)
SELECT id, account_id, provider, model, enabled, source, last_seen_at, last_error, metadata, created_at, updated_at
FROM oauth_account_models
ON CONFLICT DO NOTHING;

SELECT setval(pg_get_serial_sequence('provider_accounts', 'id'), COALESCE((SELECT MAX(id) FROM provider_accounts), 1), (SELECT MAX(id) FROM provider_accounts) IS NOT NULL);
SELECT setval(pg_get_serial_sequence('provider_account_models', 'id'), COALESCE((SELECT MAX(id) FROM provider_account_models), 1), (SELECT MAX(id) FROM provider_account_models) IS NOT NULL);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS client_api_key_models;
ALTER TABLE client_api_keys DROP COLUMN IF EXISTS model_policy;
DROP INDEX IF EXISTS provider_account_models_account_enabled_idx;
DROP INDEX IF EXISTS provider_account_models_provider_model_enabled_idx;
DROP TABLE IF EXISTS provider_account_models;
DROP TABLE IF EXISTS provider_account_credentials;
DROP INDEX IF EXISTS provider_accounts_schedulable_idx;
DROP INDEX IF EXISTS provider_accounts_provider_type_subject_idx;
DROP TABLE IF EXISTS provider_accounts;
-- +goose StatementEnd
