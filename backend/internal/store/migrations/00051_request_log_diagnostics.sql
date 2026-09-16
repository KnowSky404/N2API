-- +goose Up
-- +goose StatementBegin
ALTER TABLE request_logs
    ADD COLUMN IF NOT EXISTS attempts JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS attempt_timeline_truncated BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS header_wait_ms INTEGER,
    ADD COLUMN IF NOT EXISTS first_useful_output_ms INTEGER,
    ADD COLUMN IF NOT EXISTS stream_finish_ms INTEGER;

ALTER TABLE request_logs
    ADD CONSTRAINT request_logs_attempts_shape_check
        CHECK (jsonb_typeof(attempts) = 'array' AND octet_length(attempts::text) <= 65536),
    ADD CONSTRAINT request_logs_diagnostic_timing_check
        CHECK (
            (header_wait_ms IS NULL OR header_wait_ms >= 0)
            AND (first_useful_output_ms IS NULL OR first_useful_output_ms >= 0)
            AND (stream_finish_ms IS NULL OR stream_finish_ms >= 0)
        );
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE request_logs
    DROP CONSTRAINT IF EXISTS request_logs_diagnostic_timing_check,
    DROP CONSTRAINT IF EXISTS request_logs_attempts_shape_check,
    DROP COLUMN IF EXISTS stream_finish_ms,
    DROP COLUMN IF EXISTS first_useful_output_ms,
    DROP COLUMN IF EXISTS header_wait_ms,
    DROP COLUMN IF EXISTS attempt_timeline_truncated,
    DROP COLUMN IF EXISTS attempts;
-- +goose StatementEnd
