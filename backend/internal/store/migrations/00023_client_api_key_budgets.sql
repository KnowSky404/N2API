-- +goose Up
ALTER TABLE client_api_keys ADD COLUMN IF NOT EXISTS request_budget_24h INTEGER NOT NULL DEFAULT 0;
ALTER TABLE client_api_keys ADD COLUMN IF NOT EXISTS token_budget_24h INTEGER NOT NULL DEFAULT 0;
ALTER TABLE client_api_keys ADD COLUMN IF NOT EXISTS request_budget_30d INTEGER NOT NULL DEFAULT 0;
ALTER TABLE client_api_keys ADD COLUMN IF NOT EXISTS token_budget_30d INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE client_api_keys DROP COLUMN IF EXISTS token_budget_30d;
ALTER TABLE client_api_keys DROP COLUMN IF EXISTS request_budget_30d;
ALTER TABLE client_api_keys DROP COLUMN IF EXISTS token_budget_24h;
ALTER TABLE client_api_keys DROP COLUMN IF EXISTS request_budget_24h;
