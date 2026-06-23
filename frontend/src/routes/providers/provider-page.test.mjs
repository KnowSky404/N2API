import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mock } from 'bun:test';

globalThis.$state = (value) => value;
mock.module('$lib/clipboard.js', () => ({ copyText: async () => false }));
const {
  mergeAccountModelChanges,
  parseAccountModelsText,
  parseModelLines,
  pruneAccountModelStates,
  removeAccountModel,
  setAccountModelEnabled,
  shouldApplyAccountModelsResponse
} = await import('../../lib/admin-state.svelte.js');

const source = readFileSync('src/routes/providers/+page.svelte', 'utf8');
const modelsSource = readFileSync('src/routes/models/+page.svelte', 'utf8');
const apiKeysSource = readFileSync('src/routes/api-keys/+page.svelte', 'utf8');

test('parseAccountModelsText trims blanks and dedupes by first occurrence', () => {
  assert.deepEqual(parseAccountModelsText('  gpt-5\n\n gpt-5-mini \ngpt-5\n codex-mini \n'), [
    { model: 'gpt-5', enabled: true },
    { model: 'gpt-5-mini', enabled: true },
    { model: 'codex-mini', enabled: true }
  ]);
});

test('parseModelLines trims blanks and dedupes plain model names', () => {
  assert.deepEqual(parseModelLines('  gpt-5\n\n gpt-5-mini \ngpt-5\n codex-mini \n'), [
    'gpt-5',
    'gpt-5-mini',
    'codex-mini'
  ]);
});

test('mergeAccountModelChanges preserves disabled rows and adds textarea rows enabled', () => {
  assert.deepEqual(
    mergeAccountModelChanges(
      [
        { model: 'gpt-5', enabled: false },
        { model: 'gpt-5-mini', enabled: true }
      ],
      ' gpt-5 \n codex-mini \n'
    ),
    [
      { model: 'gpt-5', enabled: false },
      { model: 'gpt-5-mini', enabled: true },
      { model: 'codex-mini', enabled: true }
    ]
  );
});

test('account model helpers toggle and remove configured rows without changing other rows', () => {
  const rows = [
    { model: 'gpt-5', enabled: false },
    { model: 'gpt-5-mini', enabled: true },
    { model: 'codex-mini', enabled: true }
  ];

  assert.deepEqual(setAccountModelEnabled(rows, 'gpt-5', true), [
    { model: 'gpt-5', enabled: true },
    { model: 'gpt-5-mini', enabled: true },
    { model: 'codex-mini', enabled: true }
  ]);
  assert.deepEqual(removeAccountModel(rows, 'gpt-5-mini'), [
    { model: 'gpt-5', enabled: false },
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

test('provider account state uses unified codex oauth connect endpoint', () => {
  const adminStateSource = readFileSync('src/lib/admin-state.svelte.js', 'utf8');

  assert.match(adminStateSource, /\/api\/admin\/provider-accounts\/codex-oauth\/connect/);
  assert.doesNotMatch(adminStateSource, /\/api\/admin\/providers\/openai\/connect/);
});

test('provider account state uses unified account refresh endpoint', () => {
  const adminStateSource = readFileSync('src/lib/admin-state.svelte.js', 'utf8');

  assert.match(adminStateSource, /\/api\/admin\/provider-accounts\/\$\{account\.id\}\/refresh/);
  assert.doesNotMatch(adminStateSource, /\/api\/admin\/providers\/openai\/accounts\/\$\{account\.id\}\/refresh/);
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
  assert.match(source, /setAccountModelEnabled/);
  assert.match(source, /removeAccountModel/);
  assert.match(source, /cannot receive model-routed POST traffic/);
});

test('providers page is account-oriented and supports api upstream accounts', () => {
  assert.match(source, /Provider accounts/);
  assert.match(source, /Codex OAuth/);
  assert.match(source, /API upstream/);
  assert.match(source, /Base URL/);
  assert.match(source, /Manual models/);
  assert.match(source, /disconnectProviderAccount\(account\)/);
  assert.match(source, /disabled=\{providerAccounts\.saving\}\s+onclick=\{\(\) => disconnectProviderAccount\(account\)\}/);
});

test('api keys page owns model policy and gateway default model', () => {
  assert.match(apiKeysSource, /Gateway default model/);
  assert.match(apiKeysSource, /Model access/);
  assert.match(apiKeysSource, /All routable models/);
  assert.match(apiKeysSource, /Selected models/);
});

test('models page points model access management to api keys', () => {
  assert.match(modelsSource, /API Keys/);
  assert.match(modelsSource, /href="\/api-keys"/);
});
