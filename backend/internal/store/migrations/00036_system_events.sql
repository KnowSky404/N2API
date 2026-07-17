-- +goose Up

CREATE TABLE IF NOT EXISTS system_events (
    id BIGSERIAL PRIMARY KEY,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    category TEXT NOT NULL,
    severity TEXT NOT NULL,
    action TEXT NOT NULL,
    outcome TEXT NOT NULL,
    actor_type TEXT NOT NULL,
    actor_id BIGINT,
    actor_name TEXT NOT NULL DEFAULT '',
    target_type TEXT NOT NULL DEFAULT '',
    target_id TEXT NOT NULL DEFAULT '',
    target_name TEXT NOT NULL DEFAULT '',
    correlation_id TEXT NOT NULL,
    source_ip INET,
    http_method TEXT NOT NULL DEFAULT '',
    route_pattern TEXT NOT NULL DEFAULT '',
    status_code INTEGER,
    duration_ms BIGINT NOT NULL DEFAULT 0,
    error_code TEXT NOT NULL DEFAULT '',
    message TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    CONSTRAINT system_events_category_check CHECK (category IN ('audit', 'security', 'oauth', 'scheduler', 'runtime')),
    CONSTRAINT system_events_severity_check CHECK (severity IN ('info', 'warning', 'error')),
    CONSTRAINT system_events_outcome_check CHECK (outcome IN ('success', 'failure', 'partial')),
    CONSTRAINT system_events_actor_type_check CHECK (actor_type IN ('admin', 'system')),
    CONSTRAINT system_events_actor_name_check CHECK (length(actor_name) <= 128),
    CONSTRAINT system_events_target_type_check CHECK (length(target_type) <= 128),
    CONSTRAINT system_events_target_id_check CHECK (length(target_id) <= 128),
    CONSTRAINT system_events_target_name_check CHECK (length(target_name) <= 128),
    CONSTRAINT system_events_correlation_id_check CHECK (correlation_id <> '' AND length(correlation_id) <= 100),
    CONSTRAINT system_events_status_code_check CHECK (status_code IS NULL OR status_code BETWEEN 100 AND 599),
    CONSTRAINT system_events_duration_check CHECK (duration_ms >= 0),
    CONSTRAINT system_events_error_code_check CHECK (length(error_code) <= 100),
    CONSTRAINT system_events_message_check CHECK (length(message) <= 500 AND message !~ E'[\\r\\n]'),
    CONSTRAINT system_events_metadata_object_check CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT system_events_metadata_size_check CHECK (pg_column_size(metadata) <= 8192)
);

CREATE INDEX IF NOT EXISTS system_events_occurred_id_idx
    ON system_events (occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS system_events_category_occurred_id_idx
    ON system_events (category, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS system_events_action_occurred_id_idx
    ON system_events (action, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS system_events_target_occurred_id_idx
    ON system_events (target_type, target_id, occurred_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS system_events_non_success_idx
    ON system_events (occurred_at DESC, id DESC)
    WHERE outcome <> 'success';

-- +goose Down

DROP TABLE IF EXISTS system_events;
