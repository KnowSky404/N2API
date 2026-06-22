import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mock } from 'bun:test';

globalThis.$state = (value) => value;
mock.module('$lib/clipboard.js', () => ({ copyText: async () => false }));
const { parseAccountModelsText, pruneAccountModelStates, shouldApplyAccountModelsResponse } = await import(
  '../../lib/admin-state.svelte.js'
);

const source = readFileSync('src/routes/providers/+page.svelte', 'utf8');
const modelsSource = readFileSync('src/routes/models/+page.svelte', 'utf8');

test('parseAccountModelsText trims blanks and dedupes by first occurrence', () => {
  assert.deepEqual(parseAccountModelsText('  gpt-5\n\n gpt-5-mini \ngpt-5\n codex-mini \n'), [
    { model: 'gpt-5', enabled: true },
    { model: 'gpt-5-mini', enabled: true },
    { model: 'codex-mini', enabled: true }
  ]);
});

test('shouldApplyAccountModelsResponse rejects stale account model responses', () => {
  assert.equal(shouldApplyAccountModelsResponse({ requestSeq: 3 }, 3), true);
  assert.equal(shouldApplyAccountModelsResponse({ requestSeq: 4 }, 3), false);
});

test('pruneAccountModelStates removes account model state for missing accounts', () => {
  const states = {
    7: { requestSeq: 1 },
    8: { requestSeq: 1 },
    12: { requestSeq: 1 }
  };

  pruneAccountModelStates(states, [8, 12]);

  assert.deepEqual(Object.keys(states), ['8', '12']);
});

test('provider page has a single OAuth account creation entry point', () => {
  assert.equal(source.includes('Connect account'), false);
  assert.match(source, /Add OAuth account/);
});

test('provider account table supports search, sorting, and a pinned actions column', () => {
  assert.match(source, /placeholder="Search accounts"/);
  assert.match(source, /aria-sort=/);
  assert.match(source, /sortProviderAccounts/);
  assert.match(source, /sticky right-0/);
});

test('provider account rows use compact controls and hover details', () => {
  assert.match(source, /role="switch"/);
  assert.match(source, /sr-only">Refresh account/);
  assert.match(source, /title=\{accountHoverDetail\(account\)\}/);
  assert.match(source, /title=\{statusHoverDetail\(account\)\}/);
  assert.doesNotMatch(source, />\s*\{account\.lastError\}\s*</);
});

test('provider account rows expose manual model controls and routing warning', () => {
  assert.match(source, /Manual models/);
  assert.match(source, /saveAccountModels\(account\.id/);
  assert.match(source, /cannot receive model-routed POST traffic/);
});

test('models page shows aggregate model routing status', () => {
  assert.match(modelsSource, /Model routing status/);
  assert.match(modelsSource, /Configured accounts/);
  assert.match(modelsSource, /Enabled accounts/);
  assert.match(modelsSource, /modelRouting/);
});
