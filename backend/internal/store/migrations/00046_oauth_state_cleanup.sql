-- +goose Up
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS oauth_states_consumed_at_idx
    ON oauth_states (consumed_at)
    WHERE consumed_at IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS oauth_states_consumed_at_idx;
-- +goose StatementEnd
