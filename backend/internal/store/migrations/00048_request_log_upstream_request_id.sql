-- +goose Up
ALTER TABLE request_logs
    ADD COLUMN upstream_request_id TEXT NOT NULL DEFAULT '',
    ADD CONSTRAINT request_logs_upstream_request_id_length_check
        CHECK (octet_length(upstream_request_id) <= 200);

-- +goose Down
ALTER TABLE request_logs
    DROP CONSTRAINT IF EXISTS request_logs_upstream_request_id_length_check,
    DROP COLUMN IF EXISTS upstream_request_id;
