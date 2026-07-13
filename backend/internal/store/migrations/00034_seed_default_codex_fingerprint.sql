-- +goose Up
INSERT INTO fingerprint_profiles (
    system_key,
    name,
    description,
    user_agent,
    tls_fingerprint,
    headers_json,
    enabled
)
VALUES (
    'codex_cli_default',
    'Default Codex CLI',
    'System-managed Codex TUI outbound identity for OAuth accounts.',
    'codex-tui/0.135.0 (Mac OS 26.5.0; arm64) iTerm.app/3.6.10 (codex-tui; 0.135.0)',
    '',
    '{"Originator":"codex-tui","Version":"0.135.0"}'::jsonb,
    true
)
ON CONFLICT (system_key) WHERE system_key <> ''
DO UPDATE SET
    name = EXCLUDED.name,
    description = EXCLUDED.description,
    user_agent = EXCLUDED.user_agent,
    tls_fingerprint = EXCLUDED.tls_fingerprint,
    headers_json = EXCLUDED.headers_json,
    enabled = true,
    updated_at = now();

-- +goose Down
UPDATE fingerprint_profiles
SET description = 'Built-in Codex CLI-style outbound identity for OAuth accounts.',
    user_agent = 'codex_cli_rs/0.125.0 (Ubuntu 22.4.0; x86_64) xterm-256color',
    tls_fingerprint = '',
    headers_json = '{"Originator":"codex_cli_rs"}'::jsonb,
    updated_at = now()
WHERE system_key = 'codex_cli_default';
