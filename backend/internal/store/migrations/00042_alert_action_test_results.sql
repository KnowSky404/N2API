-- +goose Up

ALTER TABLE alert_actions
    ADD COLUMN last_test_started_at TIMESTAMPTZ,
    ADD COLUMN last_test_attempt_token TEXT NOT NULL DEFAULT '',
    ADD COLUMN last_test_attempt_config_updated_at TIMESTAMPTZ,
    ADD COLUMN last_tested_at TIMESTAMPTZ,
    ADD COLUMN last_test_config_updated_at TIMESTAMPTZ,
    ADD COLUMN last_test_status TEXT NOT NULL DEFAULT '',
    ADD COLUMN last_test_http_status INTEGER,
    ADD COLUMN last_test_latency_ms BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN last_test_error_code TEXT NOT NULL DEFAULT '',
    ADD COLUMN last_test_retryable BOOLEAN NOT NULL DEFAULT false,
    ADD CONSTRAINT alert_actions_last_test_status_check
        CHECK (last_test_status IN ('', 'passed', 'failed')),
    ADD CONSTRAINT alert_actions_last_test_http_status_check
        CHECK (last_test_http_status IS NULL OR last_test_http_status BETWEEN 100 AND 599),
    ADD CONSTRAINT alert_actions_last_test_latency_check
        CHECK (last_test_latency_ms >= 0),
    ADD CONSTRAINT alert_actions_last_test_error_code_check
        CHECK (octet_length(last_test_error_code) <= 128 AND last_test_error_code !~ E'[\\r\\n]'),
    ADD CONSTRAINT alert_actions_last_test_attempt_token_check
        CHECK (last_test_attempt_token = '' OR last_test_attempt_token ~ '^[0-9a-f]{32}$'),
    ADD CONSTRAINT alert_actions_last_test_attempt_consistency_check
        CHECK ((last_test_attempt_token = '' AND last_test_attempt_config_updated_at IS NULL)
            OR (last_test_attempt_token <> '' AND last_test_started_at IS NOT NULL AND last_test_attempt_config_updated_at IS NOT NULL)),
    ADD CONSTRAINT alert_actions_last_test_consistency_check
        CHECK ((last_tested_at IS NULL AND last_test_status = '' AND last_test_config_updated_at IS NULL)
            OR (last_tested_at IS NOT NULL AND last_test_status <> '' AND last_test_config_updated_at IS NOT NULL));

-- +goose Down

ALTER TABLE alert_actions
    DROP CONSTRAINT IF EXISTS alert_actions_last_test_consistency_check,
    DROP CONSTRAINT IF EXISTS alert_actions_last_test_attempt_consistency_check,
    DROP CONSTRAINT IF EXISTS alert_actions_last_test_attempt_token_check,
    DROP CONSTRAINT IF EXISTS alert_actions_last_test_error_code_check,
    DROP CONSTRAINT IF EXISTS alert_actions_last_test_latency_check,
    DROP CONSTRAINT IF EXISTS alert_actions_last_test_http_status_check,
    DROP CONSTRAINT IF EXISTS alert_actions_last_test_status_check,
    DROP COLUMN IF EXISTS last_test_retryable,
    DROP COLUMN IF EXISTS last_test_error_code,
    DROP COLUMN IF EXISTS last_test_latency_ms,
    DROP COLUMN IF EXISTS last_test_http_status,
    DROP COLUMN IF EXISTS last_test_status,
    DROP COLUMN IF EXISTS last_test_config_updated_at,
    DROP COLUMN IF EXISTS last_tested_at,
    DROP COLUMN IF EXISTS last_test_attempt_config_updated_at,
    DROP COLUMN IF EXISTS last_test_attempt_token,
    DROP COLUMN IF EXISTS last_test_started_at;
