-- +goose Up
-- +goose StatementBegin
ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT '';
ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active';
ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS status_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS fingerprint_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS user_agent_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS ip_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS failure_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS circuit_open_until TIMESTAMPTZ;
ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS rate_limited_until TIMESTAMPTZ;
ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS last_refresh_error TEXT NOT NULL DEFAULT '';
ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS last_refresh_error_at TIMESTAMPTZ;

ALTER TABLE oauth_states ADD COLUMN IF NOT EXISTS target_account_id BIGINT;
ALTER TABLE oauth_states ADD COLUMN IF NOT EXISTS pending_account_name TEXT NOT NULL DEFAULT '';
ALTER TABLE oauth_states ADD COLUMN IF NOT EXISTS pending_priority INTEGER NOT NULL DEFAULT 0;
ALTER TABLE oauth_states ADD COLUMN IF NOT EXISTS pending_enabled BOOLEAN;
ALTER TABLE oauth_states ADD COLUMN IF NOT EXISTS fingerprint_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE oauth_states ADD COLUMN IF NOT EXISTS user_agent_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE oauth_states ADD COLUMN IF NOT EXISTS ip_hash TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS oauth_accounts_schedulable_idx
	ON oauth_accounts (provider, enabled, status, priority, last_used_at, id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS oauth_accounts_schedulable_idx;

ALTER TABLE oauth_states DROP COLUMN IF EXISTS ip_hash;
ALTER TABLE oauth_states DROP COLUMN IF EXISTS user_agent_hash;
ALTER TABLE oauth_states DROP COLUMN IF EXISTS fingerprint_hash;
ALTER TABLE oauth_states DROP COLUMN IF EXISTS pending_enabled;
ALTER TABLE oauth_states DROP COLUMN IF EXISTS pending_priority;
ALTER TABLE oauth_states DROP COLUMN IF EXISTS pending_account_name;
ALTER TABLE oauth_states DROP COLUMN IF EXISTS target_account_id;

ALTER TABLE oauth_accounts DROP COLUMN IF EXISTS last_refresh_error_at;
ALTER TABLE oauth_accounts DROP COLUMN IF EXISTS last_refresh_error;
ALTER TABLE oauth_accounts DROP COLUMN IF EXISTS rate_limited_until;
ALTER TABLE oauth_accounts DROP COLUMN IF EXISTS circuit_open_until;
ALTER TABLE oauth_accounts DROP COLUMN IF EXISTS failure_count;
ALTER TABLE oauth_accounts DROP COLUMN IF EXISTS ip_hash;
ALTER TABLE oauth_accounts DROP COLUMN IF EXISTS user_agent_hash;
ALTER TABLE oauth_accounts DROP COLUMN IF EXISTS fingerprint_hash;
ALTER TABLE oauth_accounts DROP COLUMN IF EXISTS status_reason;
ALTER TABLE oauth_accounts DROP COLUMN IF EXISTS status;
ALTER TABLE oauth_accounts DROP COLUMN IF EXISTS name;
-- +goose StatementEnd
