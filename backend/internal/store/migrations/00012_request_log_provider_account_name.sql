-- +goose Up
-- +goose StatementBegin
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS provider_account_name TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE request_logs DROP COLUMN IF EXISTS provider_account_name;
-- +goose StatementEnd
