-- +goose Up
ALTER TABLE client_api_keys ADD COLUMN IF NOT EXISTS disabled_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE client_api_keys DROP COLUMN IF EXISTS disabled_at;
