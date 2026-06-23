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

const requestLogsPage = readFileSync('src/routes/request-logs/+page.svelte', 'utf8');

test('admin UI has focused routes behind a shared sidebar shell', () => {
  for (const file of expectedFiles) {
    assert.equal(existsSync(file), true, `${file} should exist`);
  }

  const layout = readFileSync('src/routes/+layout.svelte', 'utf8');
  for (const label of ['Dashboard', 'Providers', 'Models', 'API Keys', 'Request Logs', 'Sign out']) {
    assert.match(layout, new RegExp(label.replace(' ', '\\s+')), `layout should include ${label}`);
  }
});

test('primary navigation exposes model routing page', () => {
  const layout = readFileSync('src/routes/+layout.svelte', 'utf8');

  assert.match(layout, /href:\s*'\/models'/);
  assert.match(layout, /href:\s*'\/api-keys'/);
});

test('request logs page shows provider account attribution', () => {
  assert.match(requestLogsPage, /Provider account/);
  assert.match(requestLogsPage, /log\.providerAccountName/);
  assert.match(requestLogsPage, /log\.providerAccountType/);
  assert.match(requestLogsPage, /log\.providerAccountId/);
});

test('request logs page shows request model', () => {
  assert.match(requestLogsPage, />Model</);
  assert.match(requestLogsPage, /log\.model/);
});

test('request logs page shows token usage', () => {
  assert.match(requestLogsPage, />Tokens</);
  assert.match(requestLogsPage, />Usage</);
  assert.match(requestLogsPage, /log\.inputTokens/);
  assert.match(requestLogsPage, /log\.outputTokens/);
  assert.match(requestLogsPage, /log\.totalTokens/);
  assert.match(requestLogsPage, /log\.usageSource/);
  assert.match(requestLogsPage, /log\.estimatedCostMicrousd/);
  assert.match(requestLogsPage, /log\.pricingMatched/);
  assert.match(requestLogsPage, /Unpriced/);
});

test('request logs page includes usage accounting UI', () => {
  for (const label of ['Usage summary', 'Estimated cost', 'Input tokens', 'Output tokens', 'Pricing']) {
    assert.match(requestLogsPage, new RegExp(label.replace(' ', '\\s+')), `request logs page should include ${label}`);
  }
});
