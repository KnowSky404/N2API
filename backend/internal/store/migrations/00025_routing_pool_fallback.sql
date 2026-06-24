-- +goose Up
ALTER TABLE routing_pools ADD COLUMN IF NOT EXISTS fallback_pool_id BIGINT REFERENCES routing_pools(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS routing_pools_fallback_pool_idx
    ON routing_pools (fallback_pool_id)
    WHERE fallback_pool_id IS NOT NULL;

ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS routing_pool_fallback_depth INTEGER NOT NULL DEFAULT 0;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS routing_pool_fallback_chain TEXT NOT NULL DEFAULT '';
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS routing_pool_error TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE request_logs DROP COLUMN IF EXISTS routing_pool_error;
ALTER TABLE request_logs DROP COLUMN IF EXISTS routing_pool_fallback_chain;
ALTER TABLE request_logs DROP COLUMN IF EXISTS routing_pool_fallback_depth;
DROP INDEX IF EXISTS routing_pools_fallback_pool_idx;
ALTER TABLE routing_pools DROP COLUMN IF EXISTS fallback_pool_id;
