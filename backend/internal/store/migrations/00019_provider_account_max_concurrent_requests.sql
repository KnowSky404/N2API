-- +goose Up
-- +goose StatementBegin
ALTER TABLE provider_accounts ADD COLUMN IF NOT EXISTS max_concurrent_requests INTEGER NOT NULL DEFAULT 0;

ALTER TABLE provider_accounts DROP CONSTRAINT IF EXISTS provider_accounts_max_concurrent_requests_non_negative;
ALTER TABLE provider_accounts
	ADD CONSTRAINT provider_accounts_max_concurrent_requests_non_negative CHECK (max_concurrent_requests >= 0);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE provider_accounts DROP CONSTRAINT IF EXISTS provider_accounts_max_concurrent_requests_non_negative;
ALTER TABLE provider_accounts DROP COLUMN IF EXISTS max_concurrent_requests;
-- +goose StatementEnd
