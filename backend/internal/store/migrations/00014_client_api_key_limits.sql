-- +goose Up
ALTER TABLE client_api_keys ADD COLUMN IF NOT EXISTS requests_per_minute INTEGER NOT NULL DEFAULT 0;
ALTER TABLE client_api_keys ADD COLUMN IF NOT EXISTS tokens_per_minute INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE client_api_keys DROP COLUMN IF EXISTS tokens_per_minute;
ALTER TABLE client_api_keys DROP COLUMN IF EXISTS requests_per_minute;
