import { test } from 'bun:test';
import assert from '../../test/assert.js';

const source = await Bun.file('src/routes/fingerprints/+page.svelte').text();

test('fingerprint profiles expose the system default and seed new profiles from it', () => {
  for (const label of [
    'Fingerprint profiles',
    'New profile',
    'Template',
    'Blank profile',
    'System default · Codex CLI',
    'System default',
    'System sending defaults',
    'Codex OAuth',
    'API upstream',
    'Default API upstream (pass-through)',
    'Client headers',
    'Default transport',
    'Managed by system'
  ]) {
    const pattern = label.replace(/[.*+?^${}()|[\]\\]/g, '\\$&').replaceAll(' ', '\\s+');
    assert.match(source, new RegExp(pattern), `fingerprints page should include ${label}`);
  }
  assert.match(source, /SYSTEM_DEFAULT_KEY\s*=\s*'codex_cli_default'/);
  assert.match(source, /profile\.systemKey === SYSTEM_DEFAULT_KEY/);
  assert.match(source, /applyTemplate\(systemDefaultProfile\(\) \? SYSTEM_DEFAULT_KEY : ''\)/);
  assert.match(source, /if \(fp\.systemKey\) return/);
  assert.match(source, /\{#if fp\.systemKey\}[\s\S]*Managed by system/);
});

test('fingerprint create edit and delete flows use accessible dialogs', () => {
  assert.match(source, /fingerprint-form-title/);
  assert.match(source, /delete-fingerprint-title/);
  assert.match(source, /role="dialog"/);
  assert.match(source, /aria-modal="true"/);
  assert.match(source, /formError/);
  assert.match(source, /Headers must be valid JSON object syntax/);
  assert.match(source, /confirmDeleteProfile/);
  assert.doesNotMatch(source, /\bconfirm\(/);
});
