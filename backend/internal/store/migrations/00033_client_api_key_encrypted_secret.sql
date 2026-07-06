-- +goose Up
ALTER TABLE client_api_keys ADD COLUMN IF NOT EXISTS encrypted_secret TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE client_api_keys DROP COLUMN IF EXISTS encrypted_secret;
