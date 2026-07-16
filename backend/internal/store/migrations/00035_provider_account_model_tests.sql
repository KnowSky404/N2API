-- +goose Up
-- +goose StatementBegin
ALTER TABLE provider_account_models
    ADD COLUMN IF NOT EXISTS last_test_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_test_status TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS last_test_http_status INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_test_latency_ms BIGINT NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE provider_account_models
    DROP COLUMN IF EXISTS last_test_latency_ms,
    DROP COLUMN IF EXISTS last_test_http_status,
    DROP COLUMN IF EXISTS last_test_status,
    DROP COLUMN IF EXISTS last_test_at;
-- +goose StatementEnd
