-- +goose Up
ALTER TABLE provider_accounts ADD COLUMN IF NOT EXISTS last_test_at TIMESTAMPTZ;
ALTER TABLE provider_accounts ADD COLUMN IF NOT EXISTS last_test_status TEXT NOT NULL DEFAULT '';
ALTER TABLE provider_accounts ADD COLUMN IF NOT EXISTS last_test_error TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE provider_accounts DROP COLUMN IF EXISTS last_test_error;
ALTER TABLE provider_accounts DROP COLUMN IF EXISTS last_test_status;
ALTER TABLE provider_accounts DROP COLUMN IF EXISTS last_test_at;
