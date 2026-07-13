import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import assert from 'node:assert/strict';

const source = readFileSync('src/routes/fingerprints/+page.svelte', 'utf8');

test('fingerprint profiles expose the system default and seed new profiles from it', () => {
  for (const label of [
    'Fingerprint profiles',
    'New profile',
    'Template',
    'Blank profile',
    'System default · Codex CLI',
    'System default',
    'Default transport',
    'Managed by system'
  ]) {
    assert.match(source, new RegExp(label.replace(' ', '\\s+')), `fingerprints page should include ${label}`);
  }
  assert.match(source, /SYSTEM_DEFAULT_KEY\s*=\s*'codex_cli_default'/);
  assert.match(source, /profile\.systemKey === SYSTEM_DEFAULT_KEY/);
  assert.match(source, /applyTemplate\(systemDefaultProfile\(\) \? SYSTEM_DEFAULT_KEY : ''\)/);
  assert.match(source, /if \(fp\.systemKey\) return/);
  assert.match(source, /\{#if fp\.systemKey\}[\s\S]*Managed by system/);
});
