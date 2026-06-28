-- +goose Up
CREATE TABLE IF NOT EXISTS fingerprint_profiles (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    tls_fingerprint TEXT NOT NULL DEFAULT '',
    headers_json JSONB NOT NULL DEFAULT '{}',
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS fingerprint_profiles;
