-- +goose Up
ALTER TABLE oauth_states ADD COLUMN IF NOT EXISTS pending_fingerprint_profile_id BIGINT REFERENCES fingerprint_profiles(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE oauth_states DROP COLUMN IF EXISTS pending_fingerprint_profile_id;
