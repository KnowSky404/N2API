-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS oauth_account_models (
    id BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES oauth_accounts(id) ON DELETE CASCADE,
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

CREATE INDEX IF NOT EXISTS oauth_account_models_provider_model_enabled_idx
    ON oauth_account_models (provider, model, enabled, account_id);

CREATE INDEX IF NOT EXISTS oauth_account_models_account_enabled_idx
    ON oauth_account_models (account_id, enabled, model);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS oauth_account_models_account_enabled_idx;
DROP INDEX IF EXISTS oauth_account_models_provider_model_enabled_idx;
DROP TABLE IF EXISTS oauth_account_models;
-- +goose StatementEnd
