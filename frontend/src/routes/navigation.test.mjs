import { existsSync, readFileSync } from 'node:fs';
import { test } from 'node:test';
import assert from 'node:assert/strict';

const expectedFiles = [
  'src/lib/admin-state.svelte.js',
  'src/routes/+layout.svelte',
  'src/routes/+page.svelte',
  'src/routes/providers/+page.svelte',
  'src/routes/models/+page.svelte',
  'src/routes/api-keys/+page.svelte',
  'src/routes/request-logs/+page.svelte'
];

test('admin UI has focused routes behind a shared sidebar shell', () => {
  for (const file of expectedFiles) {
    assert.equal(existsSync(file), true, `${file} should exist`);
  }

  const layout = readFileSync('src/routes/+layout.svelte', 'utf8');
  for (const label of ['Dashboard', 'Providers', 'API Keys', 'Request Logs', 'Sign out']) {
    assert.match(layout, new RegExp(label.replace(' ', '\\s+')), `layout should include ${label}`);
  }
});

test('primary navigation no longer exposes standalone models page', () => {
  const layout = readFileSync('src/routes/+layout.svelte', 'utf8');

  assert.doesNotMatch(layout, /href:\s*'\/models'/);
  assert.match(layout, /href:\s*'\/api-keys'/);
});
