import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mock } from 'bun:test';

globalThis.$state = (value) => value;
mock.module('$lib/clipboard.js', () => ({ copyText: async () => false }));
const {
  apiKeyModelWarnings,
  accountModelsText,
  getGatewayReadinessIssues,
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

test('accountModelsText lists configured model names for textarea editing', () => {
  assert.equal(
    accountModelsText([
      { model: 'gpt-5', enabled: true },
      { model: 'gpt-5-mini', enabled: false },
      { model: ' codex-mini ', enabled: true },
      { model: '', enabled: true },
      { model: 'gpt-5', enabled: true }
    ]),
    'gpt-5\ngpt-5-mini\ncodex-mini'
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

test('apiKeyModelWarnings reports selected models without schedulable accounts', () => {
  const warnings = apiKeyModelWarnings(
    {
      modelPolicy: 'selected',
      allowedModels: ['gpt-5', 'gpt-5-mini', 'codex-mini']
    },
    [
      { model: 'gpt-5', enabledCount: 1 },
      { model: 'gpt-5-mini', enabledCount: 0 }
    ]
  );

  assert.deepEqual(warnings, ['gpt-5-mini', 'codex-mini']);
});

test('apiKeyModelWarnings ignores all-model policy and revoked keys', () => {
  const routing = [{ model: 'gpt-5', enabledCount: 0 }];

  assert.deepEqual(apiKeyModelWarnings({ modelPolicy: 'all', allowedModels: ['gpt-5'] }, routing), []);
  assert.deepEqual(
    apiKeyModelWarnings({ modelPolicy: 'selected', allowedModels: ['gpt-5'], revokedAt: '2026-06-23T00:00:00Z' }, routing),
    []
  );
});

test('getGatewayReadinessIssues reports missing gateway prerequisites', () => {
  assert.deepEqual(
    getGatewayReadinessIssues({
      providerAccounts: [],
      activeKeys: [],
      routableModelCount: 0,
      schedulableAccounts: []
    }),
    [
      'No provider account is connected.',
      'No provider account is currently schedulable.',
      'No model has a schedulable provider account.',
      'No active API key can call the gateway.'
    ]
  );
});

test('getGatewayReadinessIssues is clear when gateway can serve traffic', () => {
  assert.deepEqual(
    getGatewayReadinessIssues({
      providerAccounts: [{ id: 7, enabled: true, status: 'active' }],
      activeKeys: [{ id: 11, revokedAt: null }],
      routableModelCount: 1,
      schedulableAccounts: [{ id: 7, enabled: true, status: 'active' }]
    }),
    []
  );
});

test('provider page has a single OAuth account creation entry point', () => {
  assert.equal(source.includes('Connect account'), false);
  assert.match(source, /Add OAuth account/);
});

test('provider account state uses unified codex oauth connect endpoint', () => {
  const adminStateSource = readFileSync('src/lib/admin-state.svelte.js', 'utf8');

  assert.match(adminStateSource, /\/api\/admin\/provider-accounts\/codex-oauth\/status/);
  assert.match(adminStateSource, /\/api\/admin\/provider-accounts\/codex-oauth\/connect/);
  assert.doesNotMatch(adminStateSource, /\/api\/admin\/providers\/openai'/);
  assert.doesNotMatch(adminStateSource, /\/api\/admin\/providers\/openai\/connect/);
});

test('provider account state uses unified account refresh endpoint', () => {
  const adminStateSource = readFileSync('src/lib/admin-state.svelte.js', 'utf8');

  assert.match(adminStateSource, /\/api\/admin\/provider-accounts\/\$\{account\.id\}\/refresh/);
  assert.doesNotMatch(adminStateSource, /\/api\/admin\/providers\/openai\/accounts\/\$\{account\.id\}\/refresh/);
});

test('provider account state uses unified codex oauth callback endpoint', () => {
  const adminStateSource = readFileSync('src/lib/admin-state.svelte.js', 'utf8');

  assert.match(adminStateSource, /\/api\/admin\/provider-accounts\/codex-oauth\/callback/);
  assert.doesNotMatch(adminStateSource, /\/api\/admin\/providers\/openai\/callback/);
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
  assert.match(source, /updateProviderAccountName\(account, event\)/);
  assert.match(source, /provider-account-name-\$\{account\.id\}/);
  assert.match(source, /Rename \$\{accountLabel\(account\)\}/);
  assert.match(source, /Manual models/);
  assert.match(source, /resetProviderAccountStatus\(account\)/);
  assert.match(source, /Reset local status/);
  assert.match(source, /account\.rateLimitedUntil/);
  assert.match(source, /account\.circuitOpenUntil/);
  assert.match(source, /disconnectProviderAccount\(account\)/);
  assert.match(source, /disabled=\{providerAccounts\.saving\}\s+onclick=\{\(\) => disconnectProviderAccount\(account\)\}/);
});

test('admin state can reset provider account local status', () => {
  const adminStateSource = readFileSync('src/lib/admin-state.svelte.js', 'utf8');

  assert.match(adminStateSource, /resetProviderAccountStatus/);
  assert.match(adminStateSource, /\/api\/admin\/provider-accounts\/\$\{account\.id\}\/reset-status/);
  assert.match(adminStateSource, /Account status reset failed/);
  assert.match(adminStateSource, /loadModelRouting/);
});

test('admin state can update provider account local name', () => {
  const adminStateSource = readFileSync('src/lib/admin-state.svelte.js', 'utf8');

  assert.match(adminStateSource, /updateProviderAccountName/);
  assert.match(adminStateSource, /Account name cannot be empty/);
  assert.match(adminStateSource, /updateProviderAccount\(account, \{ name \}\)/);
});

test('api keys page owns model policy and gateway default model', () => {
  assert.match(apiKeysSource, /Gateway default model/);
  assert.match(apiKeysSource, /Model access/);
  assert.match(apiKeysSource, /All routable models/);
  assert.match(apiKeysSource, /Selected models/);
  assert.match(apiKeysSource, /loadModelRouting/);
  assert.match(apiKeysSource, /apiKeyModelWarnings/);
  assert.match(apiKeysSource, /No schedulable account/);
});

test('api keys page surfaces gateway runtime limits', () => {
  const adminStateSource = readFileSync('src/lib/admin-state.svelte.js', 'utf8');

  assert.match(adminStateSource, /gatewaySettings/);
  assert.match(adminStateSource, /\/api\/admin\/gateway-settings/);
  assert.match(apiKeysSource, /Gateway runtime limits/);
  assert.match(apiKeysSource, /Gateway concurrency/);
  assert.match(apiKeysSource, /Per account concurrency/);
  assert.match(apiKeysSource, /Per key concurrency/);
  assert.match(apiKeysSource, /Requests per minute/);
  assert.match(apiKeysSource, /Tokens per minute/);
  assert.match(apiKeysSource, /loadGatewaySettings/);
});

test('models page points model access management to api keys', () => {
  assert.match(modelsSource, /API Keys/);
  assert.match(modelsSource, /href="\/api-keys"/);
});

test('models page surfaces model routing candidates', () => {
  assert.match(modelsSource, /loadModelRouting/);
  assert.match(modelsSource, /Routing readiness/);
  assert.match(modelsSource, /Blocked models/);
  assert.match(modelsSource, /Blocked reasons/);
  assert.match(modelsSource, /modelRouting\.models\.filter\(\(model\) => model\.enabledCount === 0\)/);
  assert.match(modelsSource, /blockedReasonSummary/);
  assert.match(modelsSource, /account\.unschedulableReason/);
  assert.match(modelsSource, /Routing candidates/);
  assert.match(modelsSource, /model\.accounts/);
  assert.match(modelsSource, /account\.displayName/);
  assert.match(modelsSource, /Priority \{account\.priority\}/);
  assert.match(modelsSource, /account\.accountType/);
  assert.match(modelsSource, /account\.status/);
  assert.match(modelsSource, /formatDate\(account\.lastUsedAt\)/);
});
