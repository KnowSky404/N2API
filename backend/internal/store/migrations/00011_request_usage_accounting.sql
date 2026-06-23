-- +goose Up
-- +goose StatementBegin
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS input_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS output_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS total_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS cached_input_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS reasoning_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS estimated_cost_microusd BIGINT NOT NULL DEFAULT 0;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS pricing_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS usage_source TEXT NOT NULL DEFAULT 'missing';

CREATE INDEX IF NOT EXISTS request_logs_provider_account_usage_idx
    ON request_logs (provider_account_id, created_at DESC);
CREATE INDEX IF NOT EXISTS request_logs_model_usage_idx
    ON request_logs (model, created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS request_logs_model_usage_idx;
DROP INDEX IF EXISTS request_logs_provider_account_usage_idx;

ALTER TABLE request_logs DROP COLUMN IF EXISTS usage_source;
ALTER TABLE request_logs DROP COLUMN IF EXISTS pricing_snapshot;
ALTER TABLE request_logs DROP COLUMN IF EXISTS estimated_cost_microusd;
ALTER TABLE request_logs DROP COLUMN IF EXISTS reasoning_tokens;
ALTER TABLE request_logs DROP COLUMN IF EXISTS cached_input_tokens;
ALTER TABLE request_logs DROP COLUMN IF EXISTS total_tokens;
ALTER TABLE request_logs DROP COLUMN IF EXISTS output_tokens;
ALTER TABLE request_logs DROP COLUMN IF EXISTS input_tokens;
-- +goose StatementEnd
