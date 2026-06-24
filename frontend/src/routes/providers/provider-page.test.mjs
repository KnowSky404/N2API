import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mock } from 'bun:test';

globalThis.$state = (value) => value;
mock.module('$lib/clipboard.js', () => ({ copyText: async () => false }));
const {
  accountTestResults,
  apiKeys,
  apiKeyModelWarnings,
  accountModelsText,
  futureTimeRemainingLabel,
  getAccountTestResultsState,
  loadModelRoutingPreview,
  modelListText,
  modelRoutingPreview,
  getGatewayReadinessIssues,
  mergeAccountModelChanges,
  parseAccountModelsText,
  parseModelLines,
  pruneAccountModelStates,
  pruneAccountTestResultStates,
  pruneSelectedProviderAccounts,
  providerAccounts,
  providerAccountPauseForm,
  routingPools,
  selectedProviderAccountIds,
  deleteRoutingPool,
  removeAccountModel,
  session,
  setAccountModelEnabled,
  shouldApplyAccountModelsResponse,
  shouldApplyAccountTestResultsResponse,
  toggleProviderAccountSelection,
  updateAPIKeyRoutingPool,
  updateAPIKeyLimits,
  updateAPIKeyBudgets,
  addSelectedProviderAccountsToRoutingPool,
  removeSelectedProviderAccountsFromRoutingPool,
  clearProviderAccountSelection,
  validateProviderAccountPauseDuration
} = await import('../../lib/admin-state.svelte.js');

const source = readFileSync('src/routes/providers/+page.svelte', 'utf8');
const modelsSource = readFileSync('src/routes/models/+page.svelte', 'utf8');
const apiKeysSource = readFileSync('src/routes/api-keys/+page.svelte', 'utf8');
const routingPoolsSource = readFileSync('src/routes/routing-pools/+page.svelte', 'utf8');
const adminStateSource = readFileSync('src/lib/admin-state.svelte.js', 'utf8');

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

test('modelListText normalizes model names for selected API key policy editing', () => {
  assert.equal(modelListText([' gpt-5 ', '', 'gpt-5-mini', 'gpt-5', ' codex-mini ']), 'gpt-5\ngpt-5-mini\ncodex-mini');
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

test('futureTimeRemainingLabel formats scheduling block windows', () => {
  const now = new Date('2026-06-23T00:00:00Z');

  assert.equal(futureTimeRemainingLabel('2026-06-23T00:05:00Z', now), '5m remaining');
  assert.equal(futureTimeRemainingLabel('2026-06-23T02:15:00Z', now), '2h 15m remaining');
  assert.equal(futureTimeRemainingLabel('2026-06-24T03:00:00Z', now), '1d 3h remaining');
  assert.equal(futureTimeRemainingLabel('2026-06-22T23:59:00Z', now), '');
  assert.equal(futureTimeRemainingLabel('not-a-date', now), '');
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

test('account test result state initializes and rejects stale responses', () => {
  const state = getAccountTestResultsState(7);

  assert.equal(state.expanded, false);
  assert.equal(state.loading, false);
  assert.equal(state.error, '');
  assert.deepEqual(state.items, []);
  assert.equal(shouldApplyAccountTestResultsResponse({ requestSeq: 3 }, 3), true);
  assert.equal(shouldApplyAccountTestResultsResponse({ requestSeq: 4 }, 3), false);
});

test('pruneAccountTestResultStates removes state for missing accounts', () => {
  accountTestResults[7] = { requestSeq: 1 };
  accountTestResults[8] = { requestSeq: 1 };
  accountTestResults[12] = { requestSeq: 1 };

  pruneAccountTestResultStates(accountTestResults, [8, 12]);

  assert.deepEqual(Object.keys(accountTestResults), ['8', '12']);
});

test('provider account bulk selection state toggles, clears, and prunes ids', () => {
  clearProviderAccountSelection();

  toggleProviderAccountSelection(7, true);
  toggleProviderAccountSelection(8, true);
  toggleProviderAccountSelection(8, false);
  assert.deepEqual(Object.keys(selectedProviderAccountIds), ['7']);

  toggleProviderAccountSelection(9, true);
  pruneSelectedProviderAccounts([9, 12]);
  assert.deepEqual(Object.keys(selectedProviderAccountIds), ['9']);

  clearProviderAccountSelection();
  assert.deepEqual(Object.keys(selectedProviderAccountIds), []);
});

test('provider account state can bulk update selected account enabled state', () => {
  const adminStateSource = readFileSync('src/lib/admin-state.svelte.js', 'utf8');

  assert.match(adminStateSource, /bulkUpdateSelectedProviderAccounts/);
  assert.match(adminStateSource, /\/api\/admin\/provider-accounts\/bulk-update/);
  assert.match(adminStateSource, /accountIds/);
  assert.match(adminStateSource, /clearProviderAccountSelection/);
});

test('provider account state can bulk update selected scheduling fields', () => {
  const adminStateSource = readFileSync('src/lib/admin-state.svelte.js', 'utf8');

  assert.match(adminStateSource, /providerAccountBulkSchedulingForm/);
  assert.match(adminStateSource, /bulkUpdateSelectedProviderAccountScheduling/);
  assert.match(adminStateSource, /priority/);
  assert.match(adminStateSource, /loadFactor/);
  assert.match(adminStateSource, /maxConcurrentRequests/);
  assert.match(adminStateSource, /currentConcurrentRequests/);
  assert.match(adminStateSource, /effectiveMaxConcurrentRequests/);
  assert.match(adminStateSource, /\/api\/admin\/provider-accounts\/bulk-update/);
});

test('provider account state can test selected accounts', () => {
  const adminStateSource = readFileSync('src/lib/admin-state.svelte.js', 'utf8');

  assert.match(adminStateSource, /testSelectedProviderAccounts/);
  assert.match(adminStateSource, /\/api\/admin\/provider-accounts\/bulk-test/);
  assert.match(adminStateSource, /accountIds/);
  assert.match(adminStateSource, /refreshExpandedAccountTestResults/);
});

test('provider account state can refresh selected accounts', () => {
  const adminStateSource = readFileSync('src/lib/admin-state.svelte.js', 'utf8');

  assert.match(adminStateSource, /refreshSelectedProviderAccounts/);
  assert.match(adminStateSource, /\/api\/admin\/provider-accounts\/bulk-refresh/);
  assert.match(adminStateSource, /accountIds/);
  assert.match(adminStateSource, /loadProvider\(\)/);
  assert.match(adminStateSource, /loadModelRouting\(\)/);
});

test('provider account state can disconnect selected accounts', () => {
  const adminStateSource = readFileSync('src/lib/admin-state.svelte.js', 'utf8');

  assert.match(adminStateSource, /disconnectSelectedProviderAccounts/);
  assert.match(adminStateSource, /\/api\/admin\/provider-accounts\/bulk-disconnect/);
  assert.match(adminStateSource, /accountIds/);
  assert.match(adminStateSource, /clearProviderAccountSelection/);
  assert.match(adminStateSource, /loadProvider\(\)/);
  assert.match(adminStateSource, /loadModelRouting\(\)/);
});

test('provider account state can bulk pause and reset selected accounts', () => {
  const adminStateSource = readFileSync('src/lib/admin-state.svelte.js', 'utf8');

  assert.match(adminStateSource, /pauseSelectedProviderAccounts/);
  assert.match(adminStateSource, /\/api\/admin\/provider-accounts\/bulk-pause/);
  assert.match(adminStateSource, /resetSelectedProviderAccountStatus/);
  assert.match(adminStateSource, /\/api\/admin\/provider-accounts\/bulk-reset-status/);
  assert.match(adminStateSource, /clearProviderAccountSelection/);
});

test('provider account state can bulk replace selected account models', () => {
  const adminStateSource = readFileSync('src/lib/admin-state.svelte.js', 'utf8');

  assert.match(adminStateSource, /providerAccountBulkModelsForm/);
  assert.match(adminStateSource, /bulkReplaceSelectedProviderAccountModels/);
  assert.match(adminStateSource, /parseAccountModelsText/);
  assert.match(adminStateSource, /\/api\/admin\/provider-accounts\/bulk-models/);
  assert.match(adminStateSource, /loadModelRouting/);
});

test('provider account state can add selected accounts to a routing pool', async () => {
  session.authenticated = true;
  clearProviderAccountSelection();
  providerAccounts.error = '';
  routingPools.error = '';
  routingPools.items = [
    { id: 3, name: 'primary', accounts: [{ accountId: 7, priority: 5 }], accountIds: [7] }
  ];
  toggleProviderAccountSelection(7, true);
  toggleProviderAccountSelection(8, true);
  const requests = [];
  globalThis.fetch = async (path, options) => {
    requests.push({ path, options });
    if (path === '/api/admin/routing-pools') {
      return new Response(
        JSON.stringify({
          pools: [
            {
              id: 3,
              name: 'primary',
              accounts: [
                { accountId: 7, priority: 5 },
                { accountId: 8, priority: 4 }
              ],
              accountIds: [7, 8]
            }
          ]
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } }
      );
    }
    return new Response(
      JSON.stringify({
        pool: {
          id: 3,
          name: 'primary',
          accounts: [
            { accountId: 7, priority: 5 },
            { accountId: 8, priority: 4 }
          ],
          accountIds: [7, 8]
        }
      }),
      { status: 200, headers: { 'Content-Type': 'application/json' } }
    );
  };

  await addSelectedProviderAccountsToRoutingPool('3', '4');

  const membershipRequest = requests.find((request) => request.path === '/api/admin/routing-pools/3/accounts');
  assert.ok(membershipRequest);
  assert.equal(membershipRequest.options.method, 'PUT');
  assert.deepEqual(JSON.parse(membershipRequest.options.body), {
    accounts: [
      { accountId: 7, priority: 5 },
      { accountId: 8, priority: 4 }
    ]
  });
  assert.deepEqual(Object.keys(selectedProviderAccountIds), []);
  assert.deepEqual(routingPools.items[0].accountIds, [7, 8]);
});

test('provider account state rejects invalid routing pool bulk priority', async () => {
  session.authenticated = true;
  clearProviderAccountSelection();
  providerAccounts.error = '';
  routingPools.error = '';
  routingPools.items = [
    { id: 3, name: 'primary', accounts: [{ accountId: 7, priority: 5 }], accountIds: [7] }
  ];
  toggleProviderAccountSelection(8, true);
  const requests = [];
  globalThis.fetch = async (path, options) => {
    requests.push({ path, options });
    return new Response(JSON.stringify({ pool: routingPools.items[0] }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' }
    });
  };

  await addSelectedProviderAccountsToRoutingPool('3', 'bad-priority');

  assert.deepEqual(requests, []);
  assert.equal(providerAccounts.error, 'Pool priority must be a non-negative whole number');
  assert.deepEqual(Object.keys(selectedProviderAccountIds), ['8']);
});

test('provider account state can remove selected accounts from a routing pool', async () => {
  session.authenticated = true;
  clearProviderAccountSelection();
  providerAccounts.error = '';
  routingPools.error = '';
  routingPools.items = [
    {
      id: 3,
      name: 'primary',
      accounts: [
        { accountId: 7, priority: 5 },
        { accountId: 8, priority: 0 },
        { accountId: 9, priority: 2 }
      ],
      accountIds: [7, 8, 9]
    }
  ];
  toggleProviderAccountSelection(8, true);
  toggleProviderAccountSelection(9, true);
  const requests = [];
  globalThis.fetch = async (path, options) => {
    requests.push({ path, options });
    if (path === '/api/admin/routing-pools') {
      return new Response(
        JSON.stringify({
          pools: [
            {
              id: 3,
              name: 'primary',
              accounts: [{ accountId: 7, priority: 5 }],
              accountIds: [7]
            }
          ]
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } }
      );
    }
    return new Response(
      JSON.stringify({
        pool: {
          id: 3,
          name: 'primary',
          accounts: [{ accountId: 7, priority: 5 }],
          accountIds: [7]
        }
      }),
      { status: 200, headers: { 'Content-Type': 'application/json' } }
    );
  };

  await removeSelectedProviderAccountsFromRoutingPool('3');

  const membershipRequest = requests.find((request) => request.path === '/api/admin/routing-pools/3/accounts');
  assert.ok(membershipRequest);
  assert.equal(membershipRequest.options.method, 'PUT');
  assert.deepEqual(JSON.parse(membershipRequest.options.body), {
    accounts: [{ accountId: 7, priority: 5 }]
  });
  assert.deepEqual(Object.keys(selectedProviderAccountIds), []);
  assert.deepEqual(routingPools.items[0].accountIds, [7]);
});

test('provider account page exposes bulk model replacement controls', () => {
  assert.match(source, /providerAccountBulkModelsForm/);
  assert.match(source, /bulkReplaceSelectedProviderAccountModels/);
  assert.match(source, /Bulk models/);
  assert.match(source, /Apply models/);
});

test('provider account page exposes bulk routing pool assignment controls', () => {
  assert.match(source, /bulkRoutingPoolId/);
  assert.match(source, /bulkRoutingPoolPriority/);
  assert.match(source, /addSelectedProviderAccountsToRoutingPool/);
  assert.match(source, /removeSelectedProviderAccountsFromRoutingPool/);
  assert.match(source, /Bulk routing pool/);
  assert.match(source, /Pool priority/);
  assert.match(source, /Apply pool/);
  assert.match(source, /Remove pool/);
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

test('apiKeyModelWarnings respects routing pool scoped schedulability', () => {
  const warnings = apiKeyModelWarnings(
    {
      modelPolicy: 'selected',
      routingPoolId: 3,
      allowedModels: ['gpt-5', 'gpt-5-mini']
    },
    [
      {
        model: 'gpt-5',
        enabledCount: 1,
        accounts: [{ id: 7, schedulable: true, routingPoolIds: [4] }]
      },
      {
        model: 'gpt-5-mini',
        enabledCount: 1,
        accounts: [{ id: 8, schedulable: true, routingPoolIds: [3] }]
      }
    ]
  );

  assert.deepEqual(warnings, ['gpt-5']);
});

test('apiKeyModelWarnings treats fallback pool accounts as routable for bound keys', () => {
  const warnings = apiKeyModelWarnings(
    {
      modelPolicy: 'selected',
      routingPoolId: 3,
      allowedModels: ['gpt-5']
    },
    [
      {
        model: 'gpt-5',
        enabledCount: 1,
        accounts: [{ id: 8, schedulable: true, routingPoolIds: [4] }]
      }
    ],
    [
      { id: 3, name: 'primary', fallbackPoolId: 4 },
      { id: 4, name: 'secondary', fallbackPoolId: null }
    ]
  );

  assert.deepEqual(warnings, []);
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

test('provider account state uses unified test-results endpoint', () => {
  const adminStateSource = readFileSync('src/lib/admin-state.svelte.js', 'utf8');

  assert.match(adminStateSource, /\/api\/admin\/provider-accounts\/\$\{accountId\}\/test-results\?limit=20/);
  assert.doesNotMatch(adminStateSource, /\/api\/admin\/providers\/openai\/accounts\/\$\{accountId\}\/test-results/);
});

test('provider account pause duration defaults and validates before request', () => {
  const adminStateSource = readFileSync('src/lib/admin-state.svelte.js', 'utf8');

  assert.equal(providerAccountPauseForm.durationSeconds, 300);
  assert.match(adminStateSource, /providerAccountPauseForm = \$state\(\{ durationSeconds: 300 \}\)/);
  assert.match(adminStateSource, /export function validateProviderAccountPauseDuration/);
  assert.match(adminStateSource, /durationSeconds: durationSeconds/);
  assert.match(adminStateSource, /Pause duration must be a whole number between 60 and 86400 seconds/);

  providerAccountPauseForm.durationSeconds = 59;
  assert.equal(validateProviderAccountPauseDuration(), null);
  providerAccountPauseForm.durationSeconds = 600;
  assert.equal(validateProviderAccountPauseDuration(), 600);
  providerAccountPauseForm.durationSeconds = 300;
});

test('provider account state preinitializes test history state outside templates', () => {
  const adminStateSource = readFileSync('src/lib/admin-state.svelte.js', 'utf8');

  assert.match(adminStateSource, /for \(const account of providerAccounts\.items\) \{\s*ensureAccountTestResultsState\(account\.id\);\s*\}/);
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
  assert.match(source, /loadFactor/);
  assert.match(source, /testAllProviderAccounts/);
  assert.match(source, />\s*Test all accounts\s*</);
  assert.match(source, /sticky right-0/);
});

test('provider account table exposes bulk selection controls', () => {
  assert.match(source, /selectedProviderAccountIds/);
  assert.match(source, /toggleProviderAccountSelection/);
  assert.match(source, /bulkUpdateSelectedProviderAccounts\(true\)/);
  assert.match(source, /bulkUpdateSelectedProviderAccounts\(false\)/);
  assert.match(source, /bulkUpdateSelectedProviderAccountScheduling/);
  assert.match(source, /providerAccountBulkSchedulingForm/);
  assert.match(source, /testSelectedProviderAccounts/);
  assert.match(source, /refreshSelectedProviderAccounts/);
  assert.match(source, /disconnectSelectedProviderAccounts/);
  assert.match(source, /pauseSelectedProviderAccounts/);
  assert.match(source, /resetSelectedProviderAccountStatus/);
  assert.match(source, /clearProviderAccountSelection/);
  assert.match(source, />\s*Test selected\s*</);
  assert.match(source, />\s*Refresh selected\s*</);
  assert.match(source, />\s*Disconnect selected\s*</);
  assert.match(source, />\s*Pause selected\s*</);
  assert.match(source, />\s*Reset selected\s*</);
  assert.match(source, />\s*Enable selected\s*</);
  assert.match(source, />\s*Disable selected\s*</);
  assert.match(source, />\s*Apply scheduling\s*</);
  assert.match(source, /Bulk priority/);
  assert.match(source, /Bulk load factor/);
  assert.match(source, /Bulk max concurrency/);
  assert.match(source, />\s*Clear selection\s*</);
  assert.match(source, /Select \{accountLabel\(account\)\}/);
});

test('provider account rows use compact controls and hover details', () => {
  assert.match(source, /role="switch"/);
  assert.match(source, /Load factor/);
  assert.match(source, /Max concurrency/);
  assert.match(source, /concurrencyLimitLabel/);
  assert.match(source, /Active/);
  assert.match(source, /account\.currentConcurrentRequests/);
  assert.match(source, /account\.effectiveMaxConcurrentRequests/);
  assert.match(source, /updateProviderAccountLoadFactor/);
  assert.match(source, /updateProviderAccountMaxConcurrentRequests/);
  assert.match(source, /provider-account-max-concurrency/);
  assert.match(source, /testProviderAccount/);
  assert.match(source, /href=\{`\/request-logs\?providerAccountId=\$\{account\.id\}`\}/);
  assert.match(source, /View request logs/);
  assert.match(source, /sr-only">Test account/);
  assert.match(source, /pauseProviderAccount/);
  assert.match(source, /sr-only">Pause scheduling/);
  assert.match(source, /sr-only">Refresh account/);
  assert.match(source, /disconnectProviderAccount/);
  assert.match(source, /sr-only">Disconnect account/);
  assert.match(source, /title=\{accountHoverDetail\(account\)\}/);
  assert.match(source, /title=\{statusHoverDetail\(account\)\}/);
  assert.match(source, /account\.lastTestAt/);
  assert.match(source, /account\.lastTestStatus/);
  assert.match(source, /account\.lastTestError/);
  assert.match(source, /account\.provider\s*\|\|\s*'unknown'/);
  assert.doesNotMatch(source, /account\.subject\s*\|\|\s*account\.provider/);
  assert.doesNotMatch(source, />\s*\{account\.lastError\}\s*</);
});

test('provider account state can disconnect a single account', () => {
  assert.match(adminStateSource, /export async function disconnectProviderAccount/);
  assert.match(adminStateSource, /\/api\/admin\/provider-accounts\/\$\{account\.id\}/);
  assert.match(adminStateSource, /method:\s*'DELETE'/);
  assert.match(adminStateSource, /loadProviderAccounts\(\)/);
  assert.match(adminStateSource, /loadModelRouting\(\)/);
});

test('provider account rows show remaining scheduling block windows', () => {
  assert.match(source, /futureTimeRemainingLabel/);
  assert.match(source, /futureTimeRemainingLabel\(account\.rateLimitedUntil\)/);
  assert.match(source, /futureTimeRemainingLabel\(account\.circuitOpenUntil\)/);
  assert.match(source, /Rate limited \{futureTimeRemainingLabel\(account\.rateLimitedUntil\)/);
  assert.match(source, /Circuit \{futureTimeRemainingLabel\(account\.circuitOpenUntil\)/);
});

test('provider accounts page exposes configurable scheduling pause duration', () => {
  assert.match(source, /Pause duration seconds/);
  assert.match(source, /providerAccountPauseForm\.durationSeconds/);
  assert.match(source, /min="60"/);
  assert.match(source, /max="86400"/);
});

test('provider account rows expose expandable test history', () => {
  assert.match(source, /toggleAccountTestHistory\(account\.id\)/);
  assert.match(source, /getAccountTestResultsState\(account\.id\)/);
  assert.match(source, /sr-only">Test history/);
  assert.match(source, /Loading test history/);
  assert.match(source, /No test history recorded yet/);
  assert.match(source, /historyState\.items/);
  assert.match(source, /result\.checkedAt/);
  assert.match(source, /result\.createdAt/);
  assert.match(source, /result\.message/);
});

test('provider account rows expose manual model controls and routing warning', () => {
  assert.match(source, /Manual models/);
  assert.match(source, /saveAccountModels\(account\.id/);
  assert.match(source, /setAccountModelEnabled/);
  assert.match(source, /removeAccountModel/);
  assert.match(source, /cannot receive model-routed POST traffic/);
});

test('provider account rows show routing pool memberships', () => {
  assert.match(source, /loadRoutingPools/);
  assert.match(source, /routingPools/);
  assert.match(source, /accountRoutingPools\(account\.id\)/);
  assert.match(source, /accountRoutingPoolPriority\(pool,\s*account\.id\)/);
  assert.match(source, /\.sort\(\(left,\s*right\)/);
  assert.match(source, /accountRoutingPoolPriority\(left,\s*accountId\)\s*-\s*accountRoutingPoolPriority\(right,\s*accountId\)/);
  assert.match(source, /Routing pools/);
  assert.match(source, /p\{accountRoutingPoolPriority\(pool,\s*account\.id\)\}/);
  assert.match(source, /href=\{`\/routing-pools#routing-pool-\$\{pool\.id\}`\}/);
  assert.match(routingPoolsSource, /id=\{`routing-pool-\$\{pool\.id\}`\}/);
});

test('providers page is account-oriented and supports api upstream accounts', () => {
  assert.match(source, /Provider accounts/);
  assert.match(source, /Codex OAuth/);
  assert.match(source, /API upstream/);
  assert.match(source, /Base URL/);
  assert.match(source, /HTTPS is required unless HTTP upstreams are explicitly enabled/);
  assert.match(source, /name="baseUrl"/);
  assert.match(source, /name="apiKey"/);
  assert.match(source, /Leave blank to keep current key/);
  assert.match(source, /Save upstream/);
  assert.match(source, /Scheduling capacity/);
  assert.match(source, /loadFactor/);
  assert.match(source, /updateAPIUpstreamCredential\(account, event\)/);
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

test('admin state refreshes model routing after provider account scheduling changes', () => {
  const adminStateSource = readFileSync('src/lib/admin-state.svelte.js', 'utf8');
  const updateProviderAccountSource = adminStateSource.match(
    /export async function updateProviderAccount\(account, patch\) \{[\s\S]*?\n\}/
  )?.[0] ?? '';
  const refreshProviderAccountSource = adminStateSource.match(
    /export async function refreshProviderAccount\(account\) \{[\s\S]*?\n\}/
  )?.[0] ?? '';

  assert.match(updateProviderAccountSource, /loadProviderAccounts\(\)/);
  assert.match(updateProviderAccountSource, /loadModelRouting\(\)/);
  assert.match(refreshProviderAccountSource, /loadProviderAccounts\(\)/);
  assert.match(refreshProviderAccountSource, /loadModelRouting\(\)/);
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
  assert.match(apiKeysSource, /Requests window/);
  assert.match(apiKeysSource, /Tokens window/);
  assert.match(apiKeysSource, /loadGatewaySettings/);
});

test('api key state can save per-key gateway limits', async () => {
  session.authenticated = true;
  apiKeys.error = '';
  apiKeys.items = [{ id: 7, name: 'codex laptop', requestsPerMinute: 0, tokensPerMinute: 0 }];
  let request = null;
  globalThis.fetch = async (path, options) => {
    request = { path, options };
    return new Response(
      JSON.stringify({
        key: { id: 7, name: 'codex laptop', requestsPerMinute: 12, tokensPerMinute: 40000 }
      }),
      { status: 200, headers: { 'Content-Type': 'application/json' } }
    );
  };

  await updateAPIKeyLimits(7, '12', '40000');

  assert.equal(request.path, '/api/admin/keys/7/limits');
  assert.equal(request.options.method, 'PUT');
  assert.deepEqual(JSON.parse(request.options.body), { requestsPerMinute: 12, tokensPerMinute: 40000 });
  assert.equal(apiKeys.items[0].requestsPerMinute, 12);
  assert.equal(apiKeys.items[0].tokensPerMinute, 40000);
});

test('api key state can save per-key budgets', async () => {
  session.authenticated = true;
  apiKeys.error = '';
  apiKeys.items = [{ id: 7, name: 'codex laptop', requestBudget24h: 0, tokenBudget24h: 0, requestBudget30d: 0, tokenBudget30d: 0 }];
  let request = null;
  globalThis.fetch = async (path, options) => {
    request = { path, options };
    return new Response(
      JSON.stringify({
        key: { id: 7, name: 'codex laptop', requestBudget24h: 10, tokenBudget24h: 1000, requestBudget30d: 300, tokenBudget30d: 30000 }
      }),
      { status: 200, headers: { 'Content-Type': 'application/json' } }
    );
  };

  await updateAPIKeyBudgets(7, '10', '1000', '300', '30000');

  assert.equal(request.path, '/api/admin/keys/7/budgets');
  assert.equal(request.options.method, 'PUT');
  assert.deepEqual(JSON.parse(request.options.body), { requestBudget24h: 10, tokenBudget24h: 1000, requestBudget30d: 300, tokenBudget30d: 30000 });
  assert.equal(apiKeys.items[0].requestBudget24h, 10);
  assert.equal(apiKeys.items[0].tokenBudget30d, 30000);
});

test('api keys page edits per-key gateway limits', () => {
  assert.match(apiKeysSource, /href=\{`\/request-logs\?clientKeyId=\$\{key\.id\}`\}/);
  assert.match(apiKeysSource, /View request logs/);
  assert.match(apiKeysSource, /Key limits/);
  assert.match(apiKeysSource, /keyLimitLabel/);
  assert.match(apiKeysSource, /Default/);
  assert.match(apiKeysSource, /requestsPerMinute/);
  assert.match(apiKeysSource, /tokensPerMinute/);
  assert.match(apiKeysSource, /updateAPIKeyLimits/);
  assert.match(apiKeysSource, /updateAPIKeyBudgets/);
  assert.match(apiKeysSource, /Key budgets/);
  assert.match(apiKeysSource, /Requests 24h/);
  assert.match(apiKeysSource, /Tokens 30d/);
  assert.match(apiKeysSource, /requestsRemaining24h/);
  assert.match(apiKeysSource, /tokenBudgetExceeded/);
  assert.match(apiKeysSource, /\/min/);
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
  assert.match(modelsSource, /Load \{account\.loadFactor/);
  assert.match(modelsSource, /account\.accountType/);
  assert.match(modelsSource, /account\.status/);
  assert.match(modelsSource, /routingAccountHoverDetail/);
  assert.match(modelsSource, /account\.statusReason/);
  assert.match(modelsSource, /account\.lastError/);
  assert.match(modelsSource, /formatDate\(account\.lastUsedAt\)/);
});

test('admin state can load model routing preview for a sticky session', async () => {
  session.authenticated = true;
  modelRoutingPreview.error = '';
  modelRoutingPreview.model = 'gpt-5';
  modelRoutingPreview.sessionId = 'workspace-123';
  let requestPath = '';
  globalThis.fetch = async (path) => {
    requestPath = path;
    return new Response(
      JSON.stringify({
        model: 'gpt-5',
        sessionId: 'workspace-123',
        selectedAccountId: 8,
        candidates: [
          { id: 8, displayName: 'Sticky', priority: 1, scheduleRank: 1, selected: true },
          { id: 7, displayName: 'Fallback', priority: 1, scheduleRank: 2, selected: false }
        ]
      }),
      { status: 200, headers: { 'Content-Type': 'application/json' } }
    );
  };

  await loadModelRoutingPreview();

  assert.equal(requestPath, '/api/admin/model-routing/preview?model=gpt-5&sessionId=workspace-123');
  assert.equal(modelRoutingPreview.result.selectedAccountId, 8);
  assert.equal(modelRoutingPreview.result.candidates[0].selected, true);
});

test('models page can preview sticky session routing', () => {
  assert.match(modelsSource, /Selection preview/);
  assert.match(modelsSource, /modelRoutingPreview/);
  assert.match(modelsSource, /loadModelRoutingPreview/);
  assert.match(modelsSource, /Session ID/);
  assert.match(modelsSource, /Selected account/);
  assert.match(modelsSource, /selectedPreviewAccount/);
  assert.match(modelsSource, /selectedPreviewAccount\.displayName/);
  assert.match(modelsSource, /accountTypeLabel\(selectedPreviewAccount\.accountType\)/);
  assert.match(modelsSource, /modelRoutingPreview\.result\.selectedAccountId/);
  assert.match(modelsSource, /No schedulable account/);
  assert.match(modelsSource, /account\.schedulable === false/);
  assert.match(modelsSource, /account\.unschedulableReason/);
  assert.match(modelsSource, /account\.scheduleReason/);
  assert.match(modelsSource, /Schedule reason/);
  assert.match(modelsSource, /account\.lastTestAt/);
  assert.match(modelsSource, /account\.lastTestStatus/);
  assert.match(modelsSource, /account\.lastTestError/);
  assert.match(modelsSource, /Blocked/);
});

test('api key state can save routing pool binding', async () => {
  session.authenticated = true;
  apiKeys.error = '';
  apiKeys.items = [{ id: 7, name: 'codex laptop', routingPoolId: null }];
  let request = null;
  globalThis.fetch = async (path, options) => {
    request = { path, options };
    return new Response(JSON.stringify({ key: { id: 7, name: 'codex laptop', routingPoolId: 3, routingPoolName: 'primary' } }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' }
    });
  };

  await updateAPIKeyRoutingPool(7, '3');

  assert.equal(request.path, '/api/admin/keys/7/routing-pool');
  assert.equal(request.options.method, 'PUT');
  assert.deepEqual(JSON.parse(request.options.body), { routingPoolId: 3 });
  assert.equal(apiKeys.items[0].routingPoolId, 3);
});

test('routing pool state refreshes fallback references after deleting a pool', async () => {
  session.authenticated = true;
  routingPools.items = [
    { id: 1, name: 'primary', fallbackPoolId: 2, fallbackPoolName: 'secondary' },
    { id: 2, name: 'secondary', fallbackPoolId: null, fallbackPoolName: '' }
  ];
  const requests = [];
  globalThis.fetch = async (path, options = {}) => {
    requests.push({ path, options });
    if (path === '/api/admin/routing-pools/2' && options.method === 'DELETE') {
      return new Response(null, { status: 204 });
    }
    if (path === '/api/admin/routing-pools') {
      return new Response(
        JSON.stringify({
          pools: [{ id: 1, name: 'primary', fallbackPoolId: null, fallbackPoolName: '' }]
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } }
      );
    }
    if (path === '/api/admin/keys') {
      return new Response(JSON.stringify({ keys: [] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' }
      });
    }
    throw new Error(`unexpected request ${path}`);
  };

  await deleteRoutingPool(2);

  assert.deepEqual(
    requests.map((request) => request.path),
    ['/api/admin/routing-pools/2', '/api/admin/routing-pools', '/api/admin/keys']
  );
  assert.deepEqual(routingPools.items, [{ id: 1, name: 'primary', fallbackPoolId: null, fallbackPoolName: '' }]);
});

test('routing pool state sends fallback configuration', () => {
  assert.match(adminStateSource, /fallbackPoolId/);
  assert.match(adminStateSource, /fallbackPoolName/);
  assert.match(adminStateSource, /fallbackPoolId: fallbackPoolId > 0 \? fallbackPoolId : null/);
});
