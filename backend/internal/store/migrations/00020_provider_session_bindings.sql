-- +goose Up
CREATE TABLE IF NOT EXISTS provider_session_bindings (
    id BIGSERIAL PRIMARY KEY,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    session_id TEXT NOT NULL,
    account_id BIGINT NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
    last_used_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT provider_session_bindings_model_non_empty CHECK (length(trim(model)) > 0),
    CONSTRAINT provider_session_bindings_session_id_non_empty CHECK (length(trim(session_id)) > 0),
    CONSTRAINT provider_session_bindings_provider_model_session_unique UNIQUE (provider, model, session_id)
);

CREATE INDEX IF NOT EXISTS provider_session_bindings_provider_account_idx
    ON provider_session_bindings (provider, account_id);

-- +goose Down
DROP TABLE IF EXISTS provider_session_bindings;
