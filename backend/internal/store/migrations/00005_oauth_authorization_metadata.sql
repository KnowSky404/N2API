-- +goose Up
-- +goose StatementBegin
ALTER TABLE oauth_states ADD COLUMN IF NOT EXISTS encrypted_code_verifier TEXT NOT NULL DEFAULT '';
ALTER TABLE oauth_states ADD COLUMN IF NOT EXISTS code_verifier_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE oauth_states ADD COLUMN IF NOT EXISTS client_id TEXT NOT NULL DEFAULT '';

ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS encrypted_id_token TEXT NOT NULL DEFAULT '';
ALTER TABLE oauth_accounts ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE INDEX IF NOT EXISTS oauth_accounts_metadata_gin_idx
	ON oauth_accounts USING GIN (metadata);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS oauth_accounts_metadata_gin_idx;
ALTER TABLE oauth_accounts DROP COLUMN IF EXISTS metadata;
ALTER TABLE oauth_accounts DROP COLUMN IF EXISTS encrypted_id_token;
ALTER TABLE oauth_states DROP COLUMN IF EXISTS client_id;
ALTER TABLE oauth_states DROP COLUMN IF EXISTS code_verifier_hash;
ALTER TABLE oauth_states DROP COLUMN IF EXISTS encrypted_code_verifier;
-- +goose StatementEnd
