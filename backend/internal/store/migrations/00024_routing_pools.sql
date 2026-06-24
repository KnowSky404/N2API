-- +goose Up
CREATE TABLE IF NOT EXISTS routing_pools (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS routing_pool_accounts (
    pool_id BIGINT NOT NULL REFERENCES routing_pools(id) ON DELETE CASCADE,
    account_id BIGINT NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
    priority INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (pool_id, account_id)
);

CREATE INDEX IF NOT EXISTS routing_pool_accounts_account_idx
    ON routing_pool_accounts (account_id);

CREATE INDEX IF NOT EXISTS routing_pool_accounts_pool_priority_idx
    ON routing_pool_accounts (pool_id, priority);

ALTER TABLE client_api_keys ADD COLUMN IF NOT EXISTS routing_pool_id BIGINT REFERENCES routing_pools(id) ON DELETE SET NULL;

ALTER TABLE provider_session_bindings ADD COLUMN IF NOT EXISTS routing_pool_id BIGINT REFERENCES routing_pools(id) ON DELETE CASCADE;

ALTER TABLE provider_session_bindings
    DROP CONSTRAINT IF EXISTS provider_session_bindings_provider_model_session_unique;

CREATE UNIQUE INDEX IF NOT EXISTS provider_session_bindings_pool_scope_idx
    ON provider_session_bindings (provider, model, session_id, COALESCE(routing_pool_id, 0));

ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS routing_pool_id BIGINT REFERENCES routing_pools(id) ON DELETE SET NULL;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS routing_pool_name TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS request_logs_routing_pool_created_at_idx
    ON request_logs (routing_pool_id, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS request_logs_routing_pool_created_at_idx;
ALTER TABLE request_logs DROP COLUMN IF EXISTS routing_pool_name;
ALTER TABLE request_logs DROP COLUMN IF EXISTS routing_pool_id;
DROP INDEX IF EXISTS provider_session_bindings_pool_scope_idx;
ALTER TABLE provider_session_bindings
    ADD CONSTRAINT provider_session_bindings_provider_model_session_unique UNIQUE (provider, model, session_id);
ALTER TABLE provider_session_bindings DROP COLUMN IF EXISTS routing_pool_id;
ALTER TABLE client_api_keys DROP COLUMN IF EXISTS routing_pool_id;
DROP TABLE IF EXISTS routing_pool_accounts;
DROP TABLE IF EXISTS routing_pools;
