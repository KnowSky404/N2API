-- +goose Up
ALTER TABLE provider_account_credentials ADD COLUMN IF NOT EXISTS encrypted_proxy_url TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE provider_account_credentials DROP COLUMN IF EXISTS encrypted_proxy_url;
