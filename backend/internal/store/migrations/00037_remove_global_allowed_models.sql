-- +goose Up

UPDATE settings
SET value = value - 'allowedModels'
WHERE key = 'model_settings'
    AND jsonb_typeof(value) = 'object'
    AND value ? 'allowedModels';

-- +goose Down

-- The removed global allowlist cannot be reconstructed safely.
SELECT 1;
