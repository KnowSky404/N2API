-- +goose Up
-- +goose StatementBegin
ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS enabled BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS priority INTEGER NOT NULL DEFAULT 100;
ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS last_used_at TIMESTAMPTZ;
ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS last_error TEXT NOT NULL DEFAULT '';
ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS last_error_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS oauth_accounts_pool_order_idx
	ON oauth_accounts (provider, enabled, priority, last_error_at, last_used_at, id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS oauth_accounts_pool_order_idx;
ALTER TABLE oauth_accounts DROP COLUMN IF EXISTS last_error_at;
ALTER TABLE oauth_accounts DROP COLUMN IF EXISTS last_error;
ALTER TABLE oauth_accounts DROP COLUMN IF EXISTS last_used_at;
ALTER TABLE oauth_accounts DROP COLUMN IF EXISTS priority;
ALTER TABLE oauth_accounts DROP COLUMN IF EXISTS enabled;
-- +goose StatementEnd
