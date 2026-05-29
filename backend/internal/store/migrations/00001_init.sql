-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS admins (
    id BIGSERIAL PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS oauth_accounts (
    id BIGSERIAL PRIMARY KEY,
    provider TEXT NOT NULL,
    subject TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    encrypted_access_token TEXT NOT NULL,
    encrypted_refresh_token TEXT NOT NULL,
    access_token_expires_at TIMESTAMPTZ,
    last_refresh_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, subject)
);

CREATE TABLE IF NOT EXISTS client_api_keys (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    key_hash TEXT NOT NULL UNIQUE,
    prefix TEXT NOT NULL,
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS request_logs (
    id BIGSERIAL PRIMARY KEY,
    request_id TEXT NOT NULL,
    client_key_id BIGINT REFERENCES client_api_keys(id) ON DELETE SET NULL,
    provider TEXT NOT NULL,
    route TEXT NOT NULL,
    method TEXT NOT NULL,
    status_code INTEGER NOT NULL,
    latency_ms INTEGER NOT NULL,
    error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS request_logs_created_at_idx ON request_logs (created_at DESC);
CREATE INDEX IF NOT EXISTS request_logs_provider_created_at_idx ON request_logs (provider, created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS request_logs;
DROP TABLE IF EXISTS settings;
DROP TABLE IF EXISTS client_api_keys;
DROP TABLE IF EXISTS oauth_accounts;
DROP TABLE IF EXISTS admins;
-- +goose StatementEnd
