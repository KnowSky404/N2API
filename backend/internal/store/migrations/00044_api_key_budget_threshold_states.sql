-- +goose Up

CREATE TABLE api_key_budget_threshold_states (
    client_key_id BIGINT NOT NULL REFERENCES client_api_keys(id) ON DELETE CASCADE,
    budget_kind TEXT NOT NULL CHECK (budget_kind IN ('request', 'token', 'cost')),
    window_name TEXT NOT NULL CHECK (window_name IN ('24h', '30d')),
    threshold_percent INTEGER NOT NULL CHECK (threshold_percent IN (80, 100)),
    crossed_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (client_key_id, budget_kind, window_name, threshold_percent)
);

-- +goose Down

DROP TABLE IF EXISTS api_key_budget_threshold_states;
