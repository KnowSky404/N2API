-- +goose Up
-- +goose StatementBegin
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS session_id TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE request_logs DROP COLUMN IF EXISTS session_id;
-- +goose StatementEnd
