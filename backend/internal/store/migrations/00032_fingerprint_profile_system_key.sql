-- +goose Up
ALTER TABLE fingerprint_profiles ADD COLUMN IF NOT EXISTS system_key TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS fingerprint_profiles_system_key_unique_idx
    ON fingerprint_profiles (system_key)
    WHERE system_key <> '';

-- +goose Down
DROP INDEX IF EXISTS fingerprint_profiles_system_key_unique_idx;

ALTER TABLE fingerprint_profiles DROP COLUMN IF EXISTS system_key;
