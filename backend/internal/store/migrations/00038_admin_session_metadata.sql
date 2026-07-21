-- +goose Up
-- +goose StatementBegin

ALTER TABLE admin_sessions
    ADD COLUMN IF NOT EXISTS last_used_at TIMESTAMPTZ;

UPDATE admin_sessions
SET last_used_at = created_at
WHERE last_used_at IS NULL;

ALTER TABLE admin_sessions
    ALTER COLUMN last_used_at SET DEFAULT now(),
    ALTER COLUMN last_used_at SET NOT NULL,
    ADD COLUMN IF NOT EXISTS created_ip_summary TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS user_agent_summary TEXT NOT NULL DEFAULT '';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'admin_sessions_created_ip_summary_size_check'
          AND conrelid = 'admin_sessions'::regclass
    ) THEN
        ALTER TABLE admin_sessions
            ADD CONSTRAINT admin_sessions_created_ip_summary_size_check
            CHECK (octet_length(created_ip_summary) <= 64);
    END IF;
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'admin_sessions_user_agent_summary_size_check'
          AND conrelid = 'admin_sessions'::regclass
    ) THEN
        ALTER TABLE admin_sessions
            ADD CONSTRAINT admin_sessions_user_agent_summary_size_check
            CHECK (octet_length(user_agent_summary) <= 256);
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS admin_sessions_active_admin_last_used_idx
    ON admin_sessions (admin_id, last_used_at DESC, id DESC)
    WHERE revoked_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS admin_sessions_active_admin_last_used_idx;

ALTER TABLE admin_sessions
    DROP CONSTRAINT IF EXISTS admin_sessions_created_ip_summary_size_check,
    DROP CONSTRAINT IF EXISTS admin_sessions_user_agent_summary_size_check,
    DROP COLUMN IF EXISTS user_agent_summary,
    DROP COLUMN IF EXISTS created_ip_summary,
    DROP COLUMN IF EXISTS last_used_at;
-- +goose StatementEnd
