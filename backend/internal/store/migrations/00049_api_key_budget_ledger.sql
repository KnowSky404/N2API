-- +goose Up
-- +goose StatementBegin
ALTER TABLE request_logs
    ADD COLUMN budget_backfill_eligible BOOLEAN NOT NULL DEFAULT true;

CREATE TABLE api_key_budget_states (
    client_key_id BIGINT PRIMARY KEY REFERENCES client_api_keys(id) ON DELETE CASCADE,
    initialization_status TEXT NOT NULL CHECK (initialization_status IN ('pending', 'ready')),
    initialization_window_start TIMESTAMPTZ,
    initialization_window_end TIMESTAMPTZ,
    initialization_cursor_created_at TIMESTAMPTZ,
    initialization_cursor_request_log_id BIGINT,
    initialization_attempts INTEGER NOT NULL DEFAULT 0 CHECK (initialization_attempts >= 0),
    initialized_at TIMESTAMPTZ,
    requests_used_24h BIGINT NOT NULL DEFAULT 0 CHECK (requests_used_24h >= 0),
    requests_used_30d BIGINT NOT NULL DEFAULT 0 CHECK (requests_used_30d >= 0),
    observed_tokens_used_24h BIGINT NOT NULL DEFAULT 0 CHECK (observed_tokens_used_24h >= 0),
    observed_tokens_used_30d BIGINT NOT NULL DEFAULT 0 CHECK (observed_tokens_used_30d >= 0),
    observed_cost_microusd_used_24h BIGINT NOT NULL DEFAULT 0 CHECK (observed_cost_microusd_used_24h >= 0),
    observed_cost_microusd_used_30d BIGINT NOT NULL DEFAULT 0 CHECK (observed_cost_microusd_used_30d >= 0),
    last_maintenance_at TIMESTAMPTZ,
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (
        (initialization_cursor_created_at IS NULL AND initialization_cursor_request_log_id IS NULL)
        OR
        (initialization_cursor_created_at IS NOT NULL AND initialization_cursor_request_log_id IS NOT NULL AND initialization_cursor_request_log_id > 0)
    ),
    CHECK (
        initialization_status <> 'pending'
        OR (initialization_window_start IS NOT NULL AND initialization_window_end IS NOT NULL AND initialization_window_start <= initialization_window_end)
    ),
    CHECK (initialization_status <> 'ready' OR initialized_at IS NOT NULL)
);

CREATE TABLE api_key_budget_admissions (
    id BIGSERIAL PRIMARY KEY,
    admission_id TEXT NOT NULL UNIQUE CHECK (octet_length(admission_id) BETWEEN 1 AND 128),
    client_key_id BIGINT NOT NULL REFERENCES client_api_keys(id) ON DELETE CASCADE,
    source TEXT NOT NULL CHECK (source IN ('live', 'legacy')),
    status TEXT NOT NULL CHECK (status IN ('admitted', 'settlement_pending', 'settled', 'abandoned')),
    settlement_outcome TEXT NOT NULL CHECK (settlement_outcome IN ('pending', 'observed', 'missing', 'abandoned')),
    usage_known BOOLEAN NOT NULL DEFAULT false,
    observed_tokens BIGINT NOT NULL DEFAULT 0 CHECK (observed_tokens >= 0),
    observed_cost_microusd BIGINT NOT NULL DEFAULT 0 CHECK (observed_cost_microusd >= 0),
    admitted_at TIMESTAMPTZ NOT NULL,
    settled_at TIMESTAMPTZ,
    request_24h_expires_at TIMESTAMPTZ NOT NULL,
    request_30d_expires_at TIMESTAMPTZ NOT NULL,
    request_24h_expired BOOLEAN NOT NULL DEFAULT false,
    request_30d_expired BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (request_24h_expires_at = admitted_at + INTERVAL '24 hours'),
    CHECK (request_30d_expires_at = admitted_at + INTERVAL '30 days'),
    CHECK (
        (status = 'admitted' AND settlement_outcome = 'pending' AND settled_at IS NULL AND NOT usage_known AND observed_tokens = 0 AND observed_cost_microusd = 0)
        OR
        (status IN ('settlement_pending', 'settled') AND (
            (settlement_outcome = 'observed' AND settled_at IS NOT NULL AND usage_known)
            OR
            (settlement_outcome = 'missing' AND settled_at IS NOT NULL AND NOT usage_known AND observed_tokens = 0 AND observed_cost_microusd = 0)
        ))
        OR
        (status = 'abandoned' AND settlement_outcome = 'abandoned' AND settled_at IS NOT NULL AND NOT usage_known AND observed_tokens = 0 AND observed_cost_microusd = 0)
    )
);

CREATE INDEX api_key_budget_states_pending_idx
    ON api_key_budget_states (client_key_id)
    WHERE initialization_status = 'pending';

CREATE INDEX api_key_budget_admissions_unsettled_idx
    ON api_key_budget_admissions (admitted_at, id)
    WHERE status = 'admitted';

CREATE INDEX api_key_budget_admissions_settlement_pending_idx
    ON api_key_budget_admissions (updated_at, id)
    WHERE status = 'settlement_pending';

CREATE INDEX api_key_budget_admissions_expiry_24h_idx
    ON api_key_budget_admissions (request_24h_expires_at, id)
    WHERE request_24h_expired = false;

CREATE INDEX api_key_budget_admissions_expiry_30d_idx
    ON api_key_budget_admissions (request_30d_expires_at, id)
    WHERE request_30d_expired = false;

CREATE INDEX api_key_budget_admissions_client_time_idx
    ON api_key_budget_admissions (client_key_id, admitted_at DESC, id DESC);

CREATE FUNCTION initialize_api_key_budget_state() RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO api_key_budget_states (
        client_key_id,
        initialization_status,
        initialization_window_start,
        initialization_window_end,
        initialized_at
    ) VALUES (
        NEW.id,
        'ready',
        NULL,
        NULL,
        now()
    ) ON CONFLICT (client_key_id) DO NOTHING;
    RETURN NEW;
END;
$$;

CREATE TRIGGER client_api_keys_initialize_budget_state
AFTER INSERT ON client_api_keys
FOR EACH ROW EXECUTE FUNCTION initialize_api_key_budget_state();

INSERT INTO api_key_budget_states (
    client_key_id,
    initialization_status,
    initialization_window_start,
    initialization_window_end,
    initialized_at
)
SELECT
    id,
    CASE
        WHEN request_budget_24h > 0 OR token_budget_24h > 0 OR cost_budget_microusd_24h > 0
          OR request_budget_30d > 0 OR token_budget_30d > 0 OR cost_budget_microusd_30d > 0
            THEN 'pending'
        ELSE 'ready'
    END,
    CASE
        WHEN request_budget_24h > 0 OR token_budget_24h > 0 OR cost_budget_microusd_24h > 0
          OR request_budget_30d > 0 OR token_budget_30d > 0 OR cost_budget_microusd_30d > 0
            THEN GREATEST(created_at, now() - INTERVAL '30 days')
        ELSE NULL
    END,
    CASE
        WHEN request_budget_24h > 0 OR token_budget_24h > 0 OR cost_budget_microusd_24h > 0
          OR request_budget_30d > 0 OR token_budget_30d > 0 OR cost_budget_microusd_30d > 0
            THEN now()
        ELSE NULL
    END,
    CASE
        WHEN request_budget_24h > 0 OR token_budget_24h > 0 OR cost_budget_microusd_24h > 0
          OR request_budget_30d > 0 OR token_budget_30d > 0 OR cost_budget_microusd_30d > 0
            THEN NULL
        ELSE now()
    END
FROM client_api_keys
ON CONFLICT (client_key_id) DO NOTHING;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM api_key_budget_admissions WHERE source = 'live')
       OR EXISTS (
           SELECT 1
           FROM request_logs
           WHERE client_key_id IS NOT NULL
             AND NOT budget_backfill_eligible
       ) THEN
        RAISE EXCEPTION 'cannot remove authoritative API key budget ledger after post-ledger activity';
    END IF;
END;
$$;

DROP TRIGGER IF EXISTS client_api_keys_initialize_budget_state ON client_api_keys;
DROP FUNCTION IF EXISTS initialize_api_key_budget_state();
DROP TABLE IF EXISTS api_key_budget_admissions;
DROP TABLE IF EXISTS api_key_budget_states;
ALTER TABLE request_logs DROP COLUMN IF EXISTS budget_backfill_eligible;
-- +goose StatementEnd
