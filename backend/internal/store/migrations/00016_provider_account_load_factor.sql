-- +goose Up
-- +goose StatementBegin
ALTER TABLE provider_accounts ADD COLUMN IF NOT EXISTS load_factor INTEGER NOT NULL DEFAULT 1;

ALTER TABLE provider_accounts DROP CONSTRAINT IF EXISTS provider_accounts_load_factor_positive;
ALTER TABLE provider_accounts
	ADD CONSTRAINT provider_accounts_load_factor_positive CHECK (load_factor BETWEEN 1 AND 100);

DROP INDEX IF EXISTS provider_accounts_schedulable_idx;
CREATE INDEX IF NOT EXISTS provider_accounts_schedulable_idx
	ON provider_accounts (provider, enabled, status, priority, load_factor DESC, last_used_at, id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS provider_accounts_schedulable_idx;
CREATE INDEX IF NOT EXISTS provider_accounts_schedulable_idx
	ON provider_accounts (provider, enabled, status, priority, last_used_at, id);

ALTER TABLE provider_accounts DROP CONSTRAINT IF EXISTS provider_accounts_load_factor_positive;
ALTER TABLE provider_accounts DROP COLUMN IF EXISTS load_factor;
-- +goose StatementEnd
