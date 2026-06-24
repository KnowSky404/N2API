-- +goose Up
-- +goose StatementBegin
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS gateway_attempt_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS gateway_fallback_count INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE request_logs DROP COLUMN IF EXISTS gateway_fallback_count;
ALTER TABLE request_logs DROP COLUMN IF EXISTS gateway_attempt_count;
-- +goose StatementEnd
