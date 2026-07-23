-- +goose Up
CREATE TABLE response_affinities (
    response_id_hash BYTEA NOT NULL,
    routing_pool_id BIGINT NOT NULL REFERENCES routing_pools(id) ON DELETE CASCADE,
    provider_account_id BIGINT NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT response_affinities_response_id_hash_length_check
        CHECK (octet_length(response_id_hash) = 32),
    CONSTRAINT response_affinities_expiration_check
        CHECK (expires_at > created_at),
    PRIMARY KEY (response_id_hash, routing_pool_id)
);

CREATE INDEX response_affinities_expires_at_idx
    ON response_affinities (expires_at, response_id_hash, routing_pool_id);

CREATE INDEX response_affinities_provider_account_idx
    ON response_affinities (provider_account_id);

CREATE INDEX response_affinities_routing_pool_idx
    ON response_affinities (routing_pool_id);

-- +goose Down
DROP TABLE IF EXISTS response_affinities;
