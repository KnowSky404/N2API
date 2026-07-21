-- +goose Up

ALTER TABLE alert_rules
    ADD COLUMN template_key TEXT NOT NULL DEFAULT '',
    ADD CONSTRAINT alert_rules_template_key_check
        CHECK (template_key = '' OR template_key ~ '^[a-z0-9][a-z0-9._-]{0,127}$');

CREATE UNIQUE INDEX alert_rules_template_key_unique_idx
    ON alert_rules (template_key)
    WHERE template_key <> '';

-- +goose Down

DROP INDEX IF EXISTS alert_rules_template_key_unique_idx;

ALTER TABLE alert_rules
    DROP CONSTRAINT IF EXISTS alert_rules_template_key_check,
    DROP COLUMN IF EXISTS template_key;
