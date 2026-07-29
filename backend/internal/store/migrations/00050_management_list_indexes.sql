-- +goose Up
CREATE INDEX client_api_keys_management_page_idx
    ON client_api_keys (created_at DESC, id DESC);

CREATE INDEX routing_pools_management_page_idx
    ON routing_pools (created_at DESC, id DESC);

-- +goose Down
DROP INDEX IF EXISTS routing_pools_management_page_idx;
DROP INDEX IF EXISTS client_api_keys_management_page_idx;
