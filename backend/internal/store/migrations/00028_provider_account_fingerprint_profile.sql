-- +goose Up
ALTER TABLE provider_accounts ADD COLUMN IF NOT EXISTS fingerprint_profile_id BIGINT REFERENCES fingerprint_profiles(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE provider_accounts DROP COLUMN IF EXISTS fingerprint_profile_id;
