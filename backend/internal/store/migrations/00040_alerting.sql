-- +goose Up

CREATE TABLE IF NOT EXISTS alert_actions (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    kind TEXT NOT NULL,
    encrypted_destination TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT alert_actions_name_check CHECK (name <> '' AND octet_length(name) <= 128 AND name !~ E'[\\r\\n]'),
    CONSTRAINT alert_actions_kind_check CHECK (kind IN ('generic_webhook', 'ntfy')),
    CONSTRAINT alert_actions_destination_check CHECK (encrypted_destination <> '' AND octet_length(encrypted_destination) <= 16384)
);

CREATE TABLE IF NOT EXISTS alert_rules (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    action_id BIGINT NOT NULL REFERENCES alert_actions(id) ON DELETE RESTRICT,
    enabled BOOLEAN NOT NULL DEFAULT true,
    category TEXT NOT NULL DEFAULT '',
    severity TEXT NOT NULL DEFAULT '',
    event_action TEXT NOT NULL DEFAULT '',
    recovery_action TEXT NOT NULL DEFAULT '',
    aggregation_count INTEGER NOT NULL DEFAULT 1,
    aggregation_window_seconds INTEGER NOT NULL DEFAULT 0,
    cooldown_seconds INTEGER NOT NULL DEFAULT 300,
    deduplication_scope TEXT NOT NULL DEFAULT 'target',
    notify_recovery BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT alert_rules_name_check CHECK (name <> '' AND octet_length(name) <= 128 AND name !~ E'[\\r\\n]'),
    CONSTRAINT alert_rules_category_check CHECK (category IN ('', 'audit', 'security', 'oauth', 'scheduler', 'runtime')),
    CONSTRAINT alert_rules_severity_check CHECK (severity IN ('', 'info', 'warning', 'error')),
    CONSTRAINT alert_rules_event_action_check CHECK (length(event_action) <= 128 AND event_action !~ E'[\\r\\n]'),
    CONSTRAINT alert_rules_recovery_action_check CHECK (length(recovery_action) <= 128 AND recovery_action !~ E'[\\r\\n]'),
    CONSTRAINT alert_rules_distinct_actions_check CHECK (
        event_action = '' OR recovery_action = '' OR event_action <> recovery_action
    ),
    CONSTRAINT alert_rules_trigger_filter_check CHECK (category <> '' OR severity <> '' OR event_action <> ''),
    CONSTRAINT alert_rules_aggregation_count_check CHECK (aggregation_count BETWEEN 1 AND 1024),
    CONSTRAINT alert_rules_aggregation_window_check CHECK (
        aggregation_window_seconds BETWEEN 0 AND 86400
        AND (aggregation_count = 1 OR aggregation_window_seconds > 0)
    ),
    CONSTRAINT alert_rules_cooldown_check CHECK (cooldown_seconds BETWEEN 0 AND 604800),
    CONSTRAINT alert_rules_deduplication_scope_check CHECK (deduplication_scope IN ('rule', 'target')),
    CONSTRAINT alert_rules_recovery_check CHECK (NOT notify_recovery OR recovery_action <> '')
);

CREATE INDEX IF NOT EXISTS alert_rules_action_id_idx
    ON alert_rules (action_id);

CREATE TABLE IF NOT EXISTS alert_rule_states (
    rule_id BIGINT NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
    deduplication_key_hash TEXT NOT NULL,
    phase TEXT NOT NULL DEFAULT 'idle',
    window_match_count INTEGER NOT NULL DEFAULT 0,
    window_started_at TIMESTAMPTZ,
    cooldown_until TIMESTAMPTZ,
    last_matched_at TIMESTAMPTZ,
    last_notified_at TIMESTAMPTZ,
    last_recovered_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (rule_id, deduplication_key_hash),
    CONSTRAINT alert_rule_states_hash_check CHECK (deduplication_key_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT alert_rule_states_phase_check CHECK (phase IN ('idle', 'firing')),
    CONSTRAINT alert_rule_states_window_match_count_check CHECK (window_match_count >= 0)
);

CREATE INDEX IF NOT EXISTS alert_rule_states_idle_eviction_idx
    ON alert_rule_states (rule_id, updated_at ASC, deduplication_key_hash ASC)
    WHERE phase = 'idle';

-- +goose Down

DROP TABLE IF EXISTS alert_rule_states;
DROP TABLE IF EXISTS alert_rules;
DROP TABLE IF EXISTS alert_actions;
