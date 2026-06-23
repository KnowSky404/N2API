-- +goose Up
-- +goose StatementBegin
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS model TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS request_logs_model_created_at_idx
    ON request_logs (model, created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS request_logs_model_created_at_idx;
ALTER TABLE request_logs DROP COLUMN IF EXISTS model;
-- +goose StatementEnd
