-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS provider_account_test_results (
	id BIGSERIAL PRIMARY KEY,
	account_id BIGINT NOT NULL REFERENCES provider_accounts(id) ON DELETE CASCADE,
	provider TEXT NOT NULL,
	status TEXT NOT NULL,
	message TEXT NOT NULL DEFAULT '',
	checked_at TIMESTAMPTZ NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS provider_account_test_results_account_idx
	ON provider_account_test_results (account_id, checked_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS provider_account_test_results_provider_idx
	ON provider_account_test_results (provider, checked_at DESC, id DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS provider_account_test_results_provider_idx;
DROP INDEX IF EXISTS provider_account_test_results_account_idx;
DROP TABLE IF EXISTS provider_account_test_results;
-- +goose StatementEnd
