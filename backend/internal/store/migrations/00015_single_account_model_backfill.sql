-- +goose Up
-- +goose StatementBegin
WITH single_provider_accounts AS (
    SELECT provider, MIN(id) AS account_id
    FROM provider_accounts
    GROUP BY provider
    HAVING COUNT(*) = 1
),
allowed_models AS (
    SELECT DISTINCT TRIM(model_value) AS model
    FROM settings,
         jsonb_array_elements_text(value->'allowedModels') AS model_value
    WHERE key = 'model_settings'
)
INSERT INTO provider_account_models (
    account_id, provider, model, enabled, source, metadata
)
SELECT
    single_provider_accounts.account_id,
    single_provider_accounts.provider,
    allowed_models.model,
    true,
    'manual',
    '{"backfilled_from":"single_account_model_backfill"}'::jsonb
FROM single_provider_accounts
CROSS JOIN allowed_models
WHERE allowed_models.model <> ''
    AND NOT EXISTS (
        SELECT 1
        FROM provider_account_models existing
        WHERE existing.account_id = single_provider_accounts.account_id
    )
ON CONFLICT (account_id, model) DO NOTHING;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM provider_account_models
WHERE metadata->>'backfilled_from' = 'single_account_model_backfill';
-- +goose StatementEnd
