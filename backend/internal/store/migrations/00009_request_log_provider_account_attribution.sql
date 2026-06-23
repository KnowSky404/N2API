-- +goose Up
-- +goose StatementBegin
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS provider_account_id BIGINT REFERENCES provider_accounts(id) ON DELETE SET NULL;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS provider_account_type TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS request_logs_provider_account_created_at_idx
    ON request_logs (provider_account_id, created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS request_logs_provider_account_created_at_idx;
ALTER TABLE request_logs DROP COLUMN IF EXISTS provider_account_type;
ALTER TABLE request_logs DROP COLUMN IF EXISTS provider_account_id;
-- +goose StatementEnd
