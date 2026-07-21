-- +goose Up

CREATE TABLE request_log_projector_checkpoints (
    projector_key TEXT PRIMARY KEY,
    last_request_log_id BIGINT NOT NULL CHECK (last_request_log_id >= 0),
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT request_log_projector_checkpoints_key_check
        CHECK (octet_length(projector_key) <= 128 AND projector_key ~ '^[a-z0-9][a-z0-9._-]{0,127}$')
);

LOCK TABLE request_logs IN SHARE MODE;

INSERT INTO request_log_projector_checkpoints (projector_key, last_request_log_id, updated_at)
SELECT 'routing_exhaustion_v1', COALESCE(MAX(id), 0), now()
FROM request_logs
ON CONFLICT (projector_key) DO NOTHING;

CREATE TABLE api_key_routing_exhaustion_states (
    client_key_id BIGINT PRIMARY KEY REFERENCES client_api_keys(id) ON DELETE CASCADE,
    trigger_request_log_id BIGINT NOT NULL CHECK (trigger_request_log_id > 0),
    triggered_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

-- +goose Down

DROP TABLE IF EXISTS api_key_routing_exhaustion_states;
DROP TABLE IF EXISTS request_log_projector_checkpoints;
