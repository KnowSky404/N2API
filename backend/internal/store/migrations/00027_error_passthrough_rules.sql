-- +goose Up
CREATE TABLE IF NOT EXISTS error_passthrough_rules (
    id BIGSERIAL PRIMARY KEY,
    pattern TEXT NOT NULL DEFAULT '',
    match_type TEXT NOT NULL DEFAULT 'status_code',
    description TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT true,
    priority INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS error_passthrough_rules_enabled_idx
    ON error_passthrough_rules (enabled, priority);

-- +goose Down
DROP TABLE IF EXISTS error_passthrough_rules;
