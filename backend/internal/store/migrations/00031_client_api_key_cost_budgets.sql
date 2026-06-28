-- +goose Up
ALTER TABLE client_api_keys ADD COLUMN IF NOT EXISTS cost_budget_microusd_24h BIGINT NOT NULL DEFAULT 0;
ALTER TABLE client_api_keys ADD COLUMN IF NOT EXISTS cost_budget_microusd_30d BIGINT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE client_api_keys DROP COLUMN IF EXISTS cost_budget_microusd_30d;
ALTER TABLE client_api_keys DROP COLUMN IF EXISTS cost_budget_microusd_24h;
