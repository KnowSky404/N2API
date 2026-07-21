-- +goose Up
-- +goose StatementBegin
DROP INDEX IF EXISTS request_logs_provider_account_usage_idx;
DROP INDEX IF EXISTS request_logs_model_usage_idx;
DROP INDEX IF EXISTS request_logs_provider_created_at_idx;

CREATE INDEX IF NOT EXISTS request_logs_client_key_created_at_id_idx
    ON request_logs (client_key_id, created_at DESC, id DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS request_logs_client_key_created_at_id_idx;

CREATE INDEX IF NOT EXISTS request_logs_provider_created_at_idx
    ON request_logs (provider, created_at DESC);
CREATE INDEX IF NOT EXISTS request_logs_provider_account_usage_idx
    ON request_logs (provider_account_id, created_at DESC);
CREATE INDEX IF NOT EXISTS request_logs_model_usage_idx
    ON request_logs (model, created_at DESC);
-- +goose StatementEnd
