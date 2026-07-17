import { test } from 'bun:test';
import assert from '../../test/assert.js';
import { runModelTestsWithConcurrency } from '../../lib/model-test-queue.js';

globalThis.$state = (value) => value;
const {
  accountTestResults,
  apiUpstreamForm,
  apiKeys,
  apiKeyModelWarnings,
  accountModelSummary,
  accountModelsText,
  applyAccountModelTestResult,
  getAccountModelsState,
  futureTimeRemainingLabel,
  getAccountTestResultsState,
  connectProvider,
  createAPIUpstreamAccount,
  loadModelRoutingPreview,
  loadRequestLogs,
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
  providerConnectForm,
  requestLogs,
  routingPools,
  selectedProviderAccountIds,
  deleteRoutingPool,
  removeAccountModel,
  saveAccountModels,
  session,
  setAccountModelEnabled,
  shouldApplyAccountModelsResponse,
  shouldApplyAccountTestResultsResponse,
  sourceBadgeLabel,
  syncAccountModels,
  testProviderAccountModel,
  toggleProviderAccountSelection,
  updateAPIKeyRoutingPool,
  updateAPIKeyLimits,
  updateAPIKeyBudgets,
  addSelectedProviderAccountsToRoutingPool,
  removeSelectedProviderAccountsFromRoutingPool,
  clearProviderAccountSelection,
  validateProviderAccountPauseDuration
} = await import('../../lib/admin-state.svelte.js');

const source = await Bun.file('src/routes/providers/+page.svelte').text();
const modelsSource = await Bun.file('src/routes/models/+page.svelte').text();
const apiKeysSource = await Bun.file('src/routes/api-keys/+page.svelte').text();
const routingPoolsSource = await Bun.file('src/routes/routing-pools/+page.svelte').text();
const adminStateSource = await Bun.file('src/lib/admin-state.svelte.js').text();

test('model test queue runs only requested models with at most three concurrent tasks', async () => {
  const requested = ['gpt-5', 'gpt-5-mini', 'codex-mini', 'o1', 'o3'];
  const seen = [];
  let active = 0;
  let peak = 0;

  await runModelTestsWithConcurrency(requested, async (model) => {
    seen.push(model);
    active += 1;
    peak = Math.max(peak, active);
    await new Promise((resolve) => setTimeout(resolve, 5));
    active -= 1;
  });

  assert.deepEqual(seen.sort(), [...requested].sort());
  assert.equal(peak, 3);
});

test('account model test result updates only the matching shared model row', () => {
  const models = [
    { model: 'gpt-5', enabled: true, lastError: 'old' },
    { model: 'gpt-5-mini', enabled: false, lastError: '' }
  ];
  const updated = applyAccountModelTestResult(models, {
    accountId: 7,
    model: 'gpt-5',
    status: 'passed',
    errorCode: '',
    httpStatus: 200,
    latencyMs: 842,
    message: '',
    checkedAt: '2026-07-16T10:00:00Z'
  });

  assert.deepEqual(updated[0], {
    model: 'gpt-5',
    enabled: true,
    lastError: '',
    lastTestAt: '2026-07-16T10:00:00Z',
    lastTestStatus: 'passed',
    lastTestHttpStatus: 200,
    lastTestLatencyMs: 842
  });
  assert.equal(updated[1], models[1]);
});

test('testProviderAccountModel posts one selected model and refreshes shared model state', async () => {
  session.authenticated = true;
  const state = getAccountModelsState(71);
  state.items = [{ model: 'gpt-5-mini', enabled: false, lastError: '' }];
  let request = null;
  globalThis.fetch = async (path, options = {}) => {
    request = { path, options };
    return new Response(JSON.stringify({
      result: {
        accountId: 71,
        model: 'gpt-5-mini',
        status: 'failed',
        errorCode: 'model_not_found',
        httpStatus: 404,
        latencyMs: 91,
        message: 'Model was not found',
        checkedAt: '2026-07-16T10:01:00Z'
      }
    }), { status: 200, headers: { 'content-type': 'application/json' } });
  };

  const result = await testProviderAccountModel(71, ' gpt-5-mini ');

  assert.equal(request.path, '/api/admin/provider-accounts/71/model-tests');
  assert.equal(request.options.method, 'POST');
  assert.deepEqual(JSON.parse(request.options.body), { model: 'gpt-5-mini' });
  assert.equal(result.errorCode, 'model_not_found');
  assert.deepEqual(state.items[0], {
    model: 'gpt-5-mini',
    enabled: false,
    lastError: 'Model was not found',
    lastTestAt: '2026-07-16T10:01:00Z',
    lastTestStatus: 'failed',
    lastTestHttpStatus: 404,
    lastTestLatencyMs: 91
  });
});

test('providers model test modal supports filtered persistent selection and row tests', () => {
  assert.match(source, /title="Test models"/);
  assert.match(source, /aria-label=\{`Model tests for \$\{accountLabel\(account\)\}`\}/);
  assert.match(source, /placeholder="Model name"/);
  assert.match(source, /bind:value=\{modelTestEnabledFilter\}/);
  assert.match(source, /bind:value=\{modelTestStatusFilter\}/);
  assert.match(source, /indeterminate=\{someFilteredModelTestsSelected\}/);
  assert.match(source, /Test selected \(\{selectedModelTestCount\}\)/);
  assert.match(source, /disabled=\{selectedModelTestCount === 0 \|\| modelTestRunActive\}/);
  assert.match(source, /runModelTestsWithConcurrency\(models,[\s\S]*?, 3\)/);

  const openSource = source.slice(source.indexOf('function openModelTests'), source.indexOf('function closeModelTests'));
  assert.match(openSource, /selectedModelTests = \{\}/);
  assert.match(openSource, /loadAccountModels\(account\.id\)/);

  const filteredSelectionSource = source.slice(
    source.indexOf('function toggleFilteredModelTestSelection'),
    source.indexOf('function clearModelTestSelection')
  );
  assert.match(filteredSelectionSource, /for \(const model of filteredModelTestModels\)/);
  assert.doesNotMatch(filteredSelectionSource, /modelTestModels/);

  const rowTestSource = source.slice(source.indexOf('function testOneModel'), source.indexOf('</script>'));
  assert.match(rowTestSource, /runModelTestQueue\(modelTestAccount\.id, \[model\]\)/);
  assert.doesNotMatch(rowTestSource, /selectedModelTests/);
});

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

test('loadRequestLogs includes usage source filter when selected', async () => {
  session.authenticated = true;
  requestLogs.usageSource = 'missing';
  let requestedPath = '';
  globalThis.fetch = async (path) => {
    requestedPath = path;
    return new Response(JSON.stringify({ logs: [] }), {
      status: 200,
      headers: { 'content-type': 'application/json' }
    });
  };

  await loadRequestLogs();

  assert.match(requestedPath, /usageSource=missing/);
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
  assert.match(adminStateSource, /bulkUpdateSelectedProviderAccounts/);
  assert.match(adminStateSource, /\/api\/admin\/provider-accounts\/bulk-update/);
  assert.match(adminStateSource, /accountIds/);
  assert.match(adminStateSource, /clearProviderAccountSelection/);
});

test('provider accounts expose per-account outbound proxy controls', () => {
  assert.match(adminStateSource, /proxyUrlConfigured/);
  assert.match(adminStateSource, /proxyUrlSummary/);
  assert.match(adminStateSource, /proxyUrl: apiUpstreamForm\.proxyUrl/);
  assert.match(source, /Proxy URL/);
  assert.match(source, /name="proxyUrl"/);
  assert.match(source, /credentialPatch\.proxyUrl = draft\.proxyUrl\.trim\(\)/);
});

test('provider account create forms can bind fingerprint profiles', () => {
  assert.match(source, /providerConnectForm\.fingerprintProfileId/);
  assert.match(source, /apiUpstreamForm\.fingerprintProfileId/);
  assert.match(source, /Fingerprint profile/);
  assert.match(source, /fingerprintProfiles\.items/);
  assert.match(adminStateSource, /fingerprintProfileId: account \? account\.fingerprintProfileId \?\? 0 : Number\(providerConnectForm\.fingerprintProfileId\)/);
  assert.match(adminStateSource, /fingerprintProfileId: Number\(apiUpstreamForm\.fingerprintProfileId\)/);
  assert.match(source, /<option value="0">Default Codex CLI<\/option>/);
  assert.match(source, /<option value="0">Default API upstream \(pass-through\)<\/option>/);

  // Confirm each context-specific label is correct via source slices
  const providerConnectSlice = source.slice(
    source.indexOf('providerConnectForm.fingerprintProfileId'),
    source.indexOf('providerConnectForm.fingerprintProfileId') + 300
  );
  assert.match(providerConnectSlice, /<option value="0">Default Codex CLI<\/option>/);

  const apiUpstreamSlice = source.slice(
    source.indexOf('apiUpstreamForm.fingerprintProfileId'),
    source.indexOf('apiUpstreamForm.fingerprintProfileId') + 300
  );
  assert.match(apiUpstreamSlice, /<option value="0">Default API upstream \(pass-through\)<\/option>/);

  const editAccountSlice = source.slice(
    source.lastIndexOf('bind:value={draft.fingerprintProfileId}'),
    source.lastIndexOf('bind:value={draft.fingerprintProfileId}') + 500
  );
  assert.match(editAccountSlice, /account\.accountType === 'api_upstream'/);
  assert.match(editAccountSlice, /Default API upstream \(pass-through\)/);
  assert.match(editAccountSlice, /Default Codex CLI/);
  assert.match(editAccountSlice, /<option value=\{0\}>/);
});

test('provider account state sends fingerprint profile on new account creation', async () => {
  session.authenticated = true;
  providerConnectForm.name = 'Work Codex';
  providerConnectForm.priority = 7;
  providerConnectForm.enabled = true;
  providerConnectForm.fingerprintProfileId = '9';
  apiUpstreamForm.name = 'Upstream';
  apiUpstreamForm.baseUrl = 'https://upstream.example.test/v1';
  apiUpstreamForm.apiKey = 'secret';
  apiUpstreamForm.proxyUrl = '';
  apiUpstreamForm.priority = 8;
  apiUpstreamForm.loadFactor = 1;
  apiUpstreamForm.enabled = true;
  apiUpstreamForm.modelsText = 'gpt-5';
  apiUpstreamForm.fingerprintProfileId = '9';

  const requests = [];
  globalThis.fetch = async (path, options = {}) => {
    requests.push({ path, options });
    if (path === '/api/admin/provider-accounts/codex-oauth/connect') {
      return new Response(JSON.stringify({ authorizationUrl: 'https://auth.example.test' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' }
      });
    }
    if (path === '/api/admin/provider-accounts/api-upstream') {
      return new Response(JSON.stringify({ account: { id: 12 } }), {
        status: 201,
        headers: { 'Content-Type': 'application/json' }
      });
    }
    return new Response(JSON.stringify({ accounts: [], models: [] }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' }
    });
  };

  await connectProvider();
  const created = await createAPIUpstreamAccount();
  assert.equal(created, true);

  const oauthRequest = requests.find((request) => request.path === '/api/admin/provider-accounts/codex-oauth/connect');
  const upstreamRequest = requests.find((request) => request.path === '/api/admin/provider-accounts/api-upstream');
  assert.ok(oauthRequest);
  assert.ok(upstreamRequest);
  assert.equal(JSON.parse(oauthRequest.options.body).fingerprintProfileId, 9);
  assert.equal(JSON.parse(upstreamRequest.options.body).fingerprintProfileId, 9);
});

test('provider account state can bulk update selected scheduling fields', () => {
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
  assert.match(adminStateSource, /testSelectedProviderAccounts/);
  assert.match(adminStateSource, /\/api\/admin\/provider-accounts\/bulk-test/);
  assert.match(adminStateSource, /accountIds/);
  assert.match(adminStateSource, /refreshExpandedAccountTestResults/);
});

test('provider account state can refresh selected accounts', () => {
  assert.match(adminStateSource, /refreshSelectedProviderAccounts/);
  assert.match(adminStateSource, /\/api\/admin\/provider-accounts\/bulk-refresh/);
  assert.match(adminStateSource, /accountIds/);
  assert.match(adminStateSource, /loadProvider\(\)/);
  assert.match(adminStateSource, /loadModelRouting\(\)/);
});

test('provider account state can disconnect selected accounts', () => {
  assert.match(adminStateSource, /disconnectSelectedProviderAccounts/);
  assert.match(adminStateSource, /\/api\/admin\/provider-accounts\/bulk-disconnect/);
  assert.match(adminStateSource, /accountIds/);
  assert.match(adminStateSource, /clearProviderAccountSelection/);
  assert.match(adminStateSource, /loadProvider\(\)/);
  assert.match(adminStateSource, /loadModelRouting\(\)/);
});

test('provider account state can bulk pause and reset selected accounts', () => {
  assert.match(adminStateSource, /pauseSelectedProviderAccounts/);
  assert.match(adminStateSource, /\/api\/admin\/provider-accounts\/bulk-pause/);
  assert.match(adminStateSource, /resetSelectedProviderAccountStatus/);
  assert.match(adminStateSource, /\/api\/admin\/provider-accounts\/bulk-reset-status/);
  assert.match(adminStateSource, /clearProviderAccountSelection/);
});

test('provider account state can bulk replace selected account models', () => {
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

test('provider page has a unified add account entry point with tabbed modal', () => {
  assert.equal(source.includes('Connect account'), false);
  assert.match(source, /Add account/);
  assert.match(source, /OAuth account/);
});

test('provider account state uses unified codex oauth connect endpoint', () => {
  assert.match(adminStateSource, /\/api\/admin\/provider-accounts\/codex-oauth\/status/);
  assert.match(adminStateSource, /\/api\/admin\/provider-accounts\/codex-oauth\/connect/);
  assert.doesNotMatch(adminStateSource, /\/api\/admin\/providers\/openai'/);
  assert.doesNotMatch(adminStateSource, /\/api\/admin\/providers\/openai\/connect/);
});

test('provider account state uses unified account refresh endpoint', () => {
  assert.match(adminStateSource, /\/api\/admin\/provider-accounts\/\$\{account\.id\}\/refresh/);
  assert.doesNotMatch(adminStateSource, /\/api\/admin\/providers\/openai\/accounts\/\$\{account\.id\}\/refresh/);
});

test('provider account state uses unified test-results endpoint', () => {
  assert.match(adminStateSource, /\/api\/admin\/provider-accounts\/\$\{accountId\}\/test-results\?limit=20/);
  assert.doesNotMatch(adminStateSource, /\/api\/admin\/providers\/openai\/accounts\/\$\{accountId\}\/test-results/);
});

test('provider account pause duration defaults and validates before request', () => {
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
  assert.match(adminStateSource, /for \(const account of providerAccounts\.items\) \{\s*ensureAccountTestResultsState\(account\.id\);\s*\}/);
});

test('provider account state uses unified codex oauth callback endpoint', () => {
  assert.match(adminStateSource, /\/api\/admin\/provider-accounts\/codex-oauth\/callback/);
  assert.doesNotMatch(adminStateSource, /\/api\/admin\/providers\/openai\/callback/);
});

test('provider account table supports search, sorting, and a pinned actions column', () => {
  assert.match(source, /placeholder="Account name"/);
  assert.match(source, /bind:value=\{accountStatusFilter\}/);
  assert.match(source, /bind:value=\{accountTypeFilter\}/);
  assert.match(source, /bind:value=\{accountEnabledFilter\}/);
  for (const label of ['All types', 'Codex OAuth', 'API upstream', 'All statuses', 'Active', 'Blocked', 'Rate limited', 'Circuit open', 'Expired', 'All', 'Enabled', 'Disabled']) {
    assert.match(source, new RegExp(label));
  }
  assert.match(source, /accountMatchesStatusFilter\(account, accountStatusFilter\)/);
  assert.match(source, /accountMatchesTypeFilter\(account, accountTypeFilter\)/);
  assert.match(source, /accountMatchesEnabledFilter\(account, accountEnabledFilter\)/);
  assert.match(source, /aria-sort=/);
  assert.match(source, /sortProviderAccounts/);
  assert.match(source, /setProviderAccountSort\('type'\)/);
  assert.match(source, /setProviderAccountSort\('enabled'\)/);
  assert.match(source, /accountTypeLabel\(account\)/);
  assert.match(source, /role="switch"/);
  assert.match(source, /updateProviderAccount\(account,\s*\{\s*enabled: event\.currentTarget\.checked\s*\}\)/);
  assert.match(source, /loadFactor/);
  assert.match(source, /testAllProviderAccounts/);
  assert.match(source, />\s*Test all accounts\s*</);
  assert.match(source, /sticky right-0/);
  assert.match(source, /colspan="8"/);
  assert.doesNotMatch(source, /setProviderAccountSort\('priority'\)/);
  assert.doesNotMatch(source, /setProviderAccountSort\('loadFactor'\)/);
  assert.doesNotMatch(source, /setProviderAccountSort\('expires'\)/);
});

test('provider account table paginates rows and shows summary controls below the table', () => {
  assert.match(source, /let accountPage = \$state\(1\)/);
  assert.match(source, /let accountPageSize = \$state\(5\)/);
  assert.match(source, /const accountPageCount = \$derived/);
  assert.match(source, /const paginatedProviderAccounts = \$derived/);
  assert.match(source, /const providerAccountPageSummary = \$derived/);
  assert.match(source, /#each paginatedProviderAccounts as account/);
  assert.match(source, /Showing \{providerAccountPageSummary\} of \{filteredProviderAccounts\.length\}/);
  assert.match(source, /bind:value=\{accountPageSize\}/);
  assert.match(source, /<option value=\{5\}>5<\/option>/);
  assert.match(source, /<option value=\{10\}>10<\/option>/);
  assert.match(source, /<option value=\{20\}>20<\/option>/);
  assert.doesNotMatch(source, /<option value=\{25\}>25<\/option>/);
  assert.match(source, /onclick=\{\(\) => goToProviderAccountPage\(accountPage - 1\)\}/);
  assert.match(source, /onclick=\{\(\) => goToProviderAccountPage\(accountPage \+ 1\)\}/);
  assert.doesNotMatch(source, /<p class="mt-3 text-sm text-\[#6e6e6e\]">\s*Showing \{filteredProviderAccounts\.length\} of \{providerAccounts\.items\.length\}/);
});

test('provider account table exposes per-row selection checkboxes', () => {
  assert.match(source, /selectedProviderAccountIds/);
  assert.match(source, /toggleProviderAccountSelection/);
  assert.match(source, /Select \{accountLabel\(account\)\}/);
});

test('provider account rows use compact controls and hover details', () => {
  assert.match(source, /function accountEmailLabel/);
  assert.match(source, /accountEmailLabel\(account\)/);
  assert.match(source, /function accountHoverDetail/);
  assert.match(source, /\[accountLabel\(account\), accountEmailLabel\(account\)\]/);
  assert.match(source, /const editingProviderAccount = \$derived/);
  assert.match(source, /openAccountEditor\(account\)/);
  assert.match(source, /closeAccountEditor/);
  assert.match(source, /role="dialog" aria-modal="true" aria-label=\{`Edit \$\{accountLabel\(account\)\}`\}/);
  assert.match(source, /title="Edit account"/);
  assert.match(source, /toggleDeleteConfirmation\(account\)/);
  assert.match(source, /async function saveAddAccount[\s\S]*?await createAPIUpstreamAccount/);
  assert.match(source, /function closeAddAccountModal/);
  assert.match(source, /Delete this account\?/);
  assert.match(source, /role="dialog"[\s\S]*?aria-modal="true"[\s\S]*?aria-label=\{`Confirm deleting \$\{accountLabel\(account\)\}`\}/);
  assert.match(source, /confirmDisconnectProviderAccount\(account\)/);
  assert.match(source, /\{#if deletingProviderAccount\}/);
  assert.match(source, /const deletingProviderAccount = \$derived/);
  assert.match(source, /fixed inset-0 z-50/);
  assert.match(source, /bg-black\/30/);
  assert.doesNotMatch(source, /absolute right-0 top-10 z-30 w-56/);
  assert.match(source, /role="switch"/);
  assert.match(source, /Load factor/);
  assert.match(source, /Max concurrency/);
  assert.match(source, /concurrencyLimitLabel/);
  assert.match(source, /Active/);
  assert.match(source, /account\.currentConcurrentRequests/);
  assert.match(source, /account\.effectiveMaxConcurrentRequests/);
  assert.match(source, /bind:value=\{draft\.loadFactor\}/);
  assert.match(source, /bind:value=\{draft\.maxConcurrentRequests\}/);
  assert.match(source, /provider-account-max-concurrency/);
  assert.match(source, /testProviderAccount/);
  assert.match(source, /href=\{`\/request-logs\?providerAccountId=\$\{account\.id\}`\}/);
  assert.match(source, /Request logs/);
  assert.match(source, /pauseProviderAccount/);
  assert.match(source, />Pause</);
  assert.match(source, />Refresh</);
  assert.match(source, /disconnectProviderAccount/);
  assert.match(source, /Delete account/);
  assert.match(source, /title=\{accountHoverDetail\(account\)\}/);
  assert.match(source, /title=\{statusHoverDetail\(account\)\}/);
  assert.doesNotMatch(source, /account\.lastTestAt/);
  assert.doesNotMatch(source, /account\.lastTestStatus/);
  assert.doesNotMatch(source, /account\.lastTestError/);
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
  assert.match(source, /function statusHoverDetail/);
  assert.match(source, /account\.status === 'rate_limited' && account\.rateLimitedUntil/);
  assert.match(source, /Rate limited until \$\{formatDate\(account\.rateLimitedUntil\)\}/);
  assert.match(source, /title=\{statusHoverDetail\(account\)\}/);
  assert.doesNotMatch(source, /futureTimeRemainingLabel/);
  assert.doesNotMatch(source, /Rate limited \{futureTimeRemainingLabel/);
});

test('provider account rows expose expandable test history', () => {
  assert.match(source, /toggleAccountTestHistory\(account\.id\)/);
  assert.match(source, /getAccountTestResultsState\(account\.id\)/);
  assert.match(source, />History</);
  assert.match(source, /\{#if historyState\.expanded\}/);
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
  assert.match(source, /function modelRoutingHref/);
  assert.match(source, /href=\{modelRoutingHref\(configuredModel\.model,\s*account\)\}/);
  assert.match(source, /model=\$\{encodeURIComponent/);
  assert.match(source, /providerAccountId=\$\{encodeURIComponent\(String\(account\.id\)\)\}/);
  assert.match(source, /<h3[^>]*>Models<\/h3>/);
});

test('provider account rows hide routing pool memberships from the compact table', () => {
  assert.doesNotMatch(source, /loadRoutingPools/);
  assert.doesNotMatch(source, /accountRoutingPools\(account\.id\)/);
  assert.doesNotMatch(source, /accountRoutingPoolPriority\(pool,\s*account\.id\)/);
  assert.doesNotMatch(source, /Routing pools/);
  assert.doesNotMatch(source, /href=\{`\/routing-pools\?routingPoolId=\$\{pool\.id\}`\}/);
  assert.match(routingPoolsSource, /routingPoolId = params\.get\('routingPoolId'\)/);
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
  assert.doesNotMatch(source, /Save upstream/);
  assert.match(source, /saveEditingProviderAccount/);
  assert.match(source, /loadFactor/);
  assert.match(source, /credentialPatch\.baseUrl/);
  assert.match(source, /bind:value=\{draft\.name\}/);
  assert.match(source, /provider-account-name-\$\{account\.id\}/);
  assert.match(source, /Rename \$\{accountLabel\(account\)\}/);
  assert.match(source, /Manual models/);
  assert.match(source, /resetProviderAccountStatus\(account\)/);
  assert.match(source, /Reset local status/);
  assert.match(source, /account\.rateLimitedUntil/);
  assert.match(source, /account\.circuitOpenUntil/);
  assert.match(source, /disconnectProviderAccount\(account\)/);
  assert.match(source, /confirmDisconnectProviderAccount\(account\)/);
  assert.match(source, /toggleDeleteConfirmation\(account\)/);
  assert.match(source, /async function saveAddAccount[\s\S]*?await createAPIUpstreamAccount/);
  assert.match(source, /function closeAddAccountModal/);
});

test('admin state can reset provider account local status', () => {
  assert.match(adminStateSource, /resetProviderAccountStatus/);
  assert.match(adminStateSource, /\/api\/admin\/provider-accounts\/\$\{account\.id\}\/reset-status/);
  assert.match(adminStateSource, /Account status reset failed/);
  assert.match(adminStateSource, /loadModelRouting/);
});

test('admin state can update provider account local name', () => {
  assert.match(adminStateSource, /updateProviderAccountName/);
  assert.match(adminStateSource, /Account name cannot be empty/);
  assert.match(adminStateSource, /updateProviderAccount\(account, \{ name \}\)/);
});

test('admin state refreshes model routing after provider account scheduling changes', () => {
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
  // Model access select and routing still on the page (in edit modal)
  assert.match(apiKeysSource, /Model access/);
  assert.match(apiKeysSource, /All routable models/);
  assert.match(apiKeysSource, /Selected models/);
  assert.match(apiKeysSource, /loadModelRouting/);
  assert.match(apiKeysSource, /apiKeyModelWarnings/);
  assert.match(apiKeysSource, /No schedulable account/);
  assert.match(apiKeysSource, /function modelRoutingHref/);
  assert.match(apiKeysSource, /href=\{modelRoutingHref\(model,\s*editingKey\)\}/);
  const modelRoutingHrefSource = apiKeysSource.match(
    /function modelRoutingHref\(model,\s*key\) \{[\s\S]*?\n  \}/
  )?.[0] ?? '';
  assert.match(modelRoutingHrefSource, /model=\$\{encodeURIComponent/);
  assert.match(modelRoutingHrefSource, /clientKeyId=\$\{encodeURIComponent\(String\(key\.id\)\)\}/);

  // Model access must NOT be a main table <th> (moves to edit modal)
  assert.doesNotMatch(apiKeysSource, />Model access<\/th>/);
});
test('api keys page loads gateway settings for per-key limit fallback', () => {
  assert.match(adminStateSource, /gatewaySettings/);
  assert.match(adminStateSource, /\/api\/admin\/gateway-settings/);
  // Gateway runtime limits UI section removed; gateway settings still loaded for per-key fallback
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
  apiKeys.items = [{ id: 7, name: 'codex laptop', requestBudget24h: 0, tokenBudget24h: 0, costBudgetMicrousd24h: 0, requestBudget30d: 0, tokenBudget30d: 0, costBudgetMicrousd30d: 0 }];
  let request = null;
  globalThis.fetch = async (path, options) => {
    request = { path, options };
    return new Response(
      JSON.stringify({
        key: { id: 7, name: 'codex laptop', requestBudget24h: 10, tokenBudget24h: 1000, costBudgetMicrousd24h: 1500000, requestBudget30d: 300, tokenBudget30d: 30000, costBudgetMicrousd30d: 9000000 }
      }),
      { status: 200, headers: { 'Content-Type': 'application/json' } }
    );
  };

  await updateAPIKeyBudgets(7, '10', '1000', '1500000', '300', '30000', '9000000');

  assert.equal(request.path, '/api/admin/keys/7/budgets');
  assert.equal(request.options.method, 'PUT');
  assert.deepEqual(JSON.parse(request.options.body), { requestBudget24h: 10, tokenBudget24h: 1000, costBudgetMicrousd24h: 1500000, requestBudget30d: 300, tokenBudget30d: 30000, costBudgetMicrousd30d: 9000000 });
  assert.equal(apiKeys.items[0].requestBudget24h, 10);
  assert.equal(apiKeys.items[0].costBudgetMicrousd24h, 1500000);
  assert.equal(apiKeys.items[0].tokenBudget30d, 30000);
  assert.equal(apiKeys.items[0].costBudgetMicrousd30d, 9000000);
});

test('api keys page edits per-key gateway limits', () => {
  // Logs action stays on the API Keys page.
  assert.match(apiKeysSource, /openKeyLogsModal\(key\.id\)/);
  assert.match(apiKeysSource, /View request logs/);

  // Key limits and budgets still in page source (move to edit modal)
  assert.match(apiKeysSource, /keyLimitLabel/);
  assert.match(apiKeysSource, /Default/);
  assert.match(apiKeysSource, /requestsPerMinute/);
  assert.match(apiKeysSource, /tokensPerMinute/);
  assert.match(apiKeysSource, /updateAPIKeyLimits/);
  assert.match(apiKeysSource, /updateAPIKeyBudgets/);
  assert.match(apiKeysSource, /requestsRemaining24h/);
  assert.match(apiKeysSource, /tokenBudgetExceeded/);

  // Key limits must NOT be a main table <th> (moves to edit modal)
  assert.doesNotMatch(apiKeysSource, />Key limits<\/th>/);
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

test('accountModelSummary counts synced, manual, and enabled rows', () => {
  const summary = accountModelSummary([
    { model: 'gpt-5', enabled: true, source: 'upstream' },
    { model: 'gpt-5-mini', enabled: false, source: 'upstream' },
    { model: 'codex-mini', enabled: true, source: '' },
    { model: 'o1', enabled: true, source: null }
  ]);
  assert.equal(summary.total, 4);
  assert.equal(summary.synced, 2);
  assert.equal(summary.manual, 2);
  assert.equal(summary.enabled, 3);
});

test('sourceBadgeLabel maps account model sources', () => {
  assert.equal(sourceBadgeLabel({ source: 'upstream' }), 'Synced');
  assert.equal(sourceBadgeLabel({ source: '' }), 'Manual');
  assert.equal(sourceBadgeLabel({ source: null }), 'Manual');
  assert.equal(sourceBadgeLabel({ source: 'manual' }), 'Manual');
});

test('saveAccountModels excludes synced rows from manual save payload', async () => {
  session.authenticated = true;
  const state = getAccountModelsState(9);
  state.error = '';
  state.items = [
    { model: 'gpt-5', enabled: true, source: 'upstream' },
    { model: 'gpt-4.1', enabled: false, source: 'manual' }
  ];
  state.text = 'gpt-4.1\ncodex-mini';
  state.saved = false;

  const requests = [];
  globalThis.fetch = async (path, options = {}) => {
    requests.push({ path, options });
    if (path === '/api/admin/provider-accounts/9/models') {
      return new Response(
        JSON.stringify({
          models: [
            { model: 'gpt-4.1', enabled: false, source: 'manual' },
            { model: 'codex-mini', enabled: true, source: 'manual' }
          ]
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } }
      );
    }
    if (path === '/api/admin/model-routing') {
      return new Response(JSON.stringify({ defaultModel: '', allowedModels: [], models: [], warnings: [] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' }
      });
    }
    throw new Error(`unexpected request ${path}`);
  };

  await saveAccountModels(9, state.text);

  const saveRequest = requests.find((request) => request.path === '/api/admin/provider-accounts/9/models');
  assert.ok(saveRequest);
  assert.deepEqual(JSON.parse(saveRequest.options.body), {
    models: [
      { model: 'gpt-4.1', enabled: false },
      { model: 'codex-mini', enabled: true }
    ]
  });
});

test('syncAccountModels calls sync endpoint and refreshes routing state', async () => {
  session.authenticated = true;
  const state = getAccountModelsState(7);
  state.error = 'old error';
  state.syncing = false;
  state.syncError = '';
  state.syncMessage = '';
  state.syncSummary = null;
  state.items = [];
  state.text = '';
  state.saved = true;

  const requests = [];
  globalThis.fetch = async (path, options) => {
    requests.push({ path, method: options?.method ?? 'GET' });
    if (path === '/api/admin/provider-accounts/7/models/sync') {
      return new Response(
        JSON.stringify({
          models: [
            { model: 'gpt-5', enabled: false, source: 'upstream' },
            { model: 'gpt-5-mini', enabled: false, source: 'upstream' }
          ],
          synced: { total: 2, new: 1, preserved: 1, skippedManual: 0 }
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } }
      );
    }
    if (path === '/api/admin/model-routing') {
      return new Response(
        JSON.stringify({ defaultModel: '', allowedModels: [], models: [], warnings: [] }),
        { status: 200, headers: { 'Content-Type': 'application/json' } }
      );
    }
    throw new Error(`unexpected request ${path}`);
  };

  await syncAccountModels(7);

  const syncRequest = requests.find((r) => r.path === '/api/admin/provider-accounts/7/models/sync');
  assert.ok(syncRequest, 'sync endpoint was called');
  assert.equal(syncRequest.method, 'POST');

  const routingRequest = requests.find((r) => r.path === '/api/admin/model-routing');
  assert.ok(routingRequest, 'model routing was refreshed');

  assert.equal(state.items.length, 2);
  assert.equal(state.items[0].model, 'gpt-5');
  assert.equal(state.items[0].source, 'upstream');
  assert.equal(state.error, '');
  assert.equal(state.saved, false);
  assert.equal(state.syncing, false);
  assert.equal(state.syncError, '');
  assert.equal(state.syncMessage, 'Synced 2 models. 1 new model was added disabled.');
  assert.deepEqual(state.syncSummary, { total: 2, new: 1, preserved: 1, skippedManual: 0 });
  assert.equal(state.text, '');
});

test('syncAccountModels stale response does not overwrite newer result', async () => {
  session.authenticated = true;
  const state = getAccountModelsState(8);

  const syncResolvers = [];
  let syncCallIndex = 0;

  globalThis.fetch = async (path, options) => {
    if (path === '/api/admin/provider-accounts/8/models/sync') {
      syncCallIndex++;
      const index = syncCallIndex;
      return new Promise((resolve) => {
        syncResolvers.push(() => {
          const modelName = index === 1 ? 'first-model' : 'second-model';
          resolve(new Response(JSON.stringify({
            models: [{ model: modelName, enabled: false, source: 'upstream' }],
            synced: { total: 1, new: 1, preserved: 0, skippedManual: 0 }
          }), { status: 200, headers: { 'Content-Type': 'application/json' } }));
        });
      });
    }
    if (path === '/api/admin/model-routing') {
      return new Response(JSON.stringify({ defaultModel: '', allowedModels: [], models: [], warnings: [] }), {
        status: 200, headers: { 'Content-Type': 'application/json' }
      });
    }
    throw new Error(`unexpected request ${path}`);
  };

  // Start both syncs concurrently
  const promise1 = syncAccountModels(8);
  const promise2 = syncAccountModels(8);

  // Resolve second response first
  syncResolvers[1]();
  await promise2;

  // Now resolve first response (should be rejected as stale)
  syncResolvers[0]();
  await promise1;

  assert.equal(state.items.length, 1);
  assert.equal(state.items[0].model, 'second-model');
  assert.equal(state.syncMessage, 'Synced 1 models. 1 new model was added disabled.');
  assert.equal(state.syncing, false);
  assert.equal(state.syncError, '');
});
test('provider account edit modal exposes account model sync controls', () => {
  assert.match(source, /Sync from upstream/);
  assert.doesNotMatch(source, /Save manual/);
  assert.match(source, /saveEditingProviderAccount[\s\S]*?saveAccountModels/);
  assert.match(source, /accountModelSummary/);
  assert.match(source, /bind:checked=\{draft\.syncModelsOnSave\}/);
  assert.match(source, /syncAccountModels\(account\.id\)/);
  assert.doesNotMatch(source, /onclick=\{\(\) => syncAccountModels/);
  assert.match(source, /sourceBadgeLabel\(configuredModel\)/);
  assert.match(source, /const priorModelItems = modelState\.items\.map/);
  assert.match(source, /modelState\.items = priorModelItems/);
  assert.match(source, /Account settings were saved, but models failed/);
});

test('provider account model list only offers remove for manual models', () => {
  assert.match(source, /configuredModel\.source !== 'upstream'/);
  assert.match(source, /Manual models/);
});

test('provider account model list disables synced row toggles', () => {
  const toggleLabelIndex = source.indexOf("aria-label={`${configuredModel.enabled ? 'Disable' : 'Enable'} ${configuredModel.model}`}");
  assert.notEqual(toggleLabelIndex, -1);
  const checkboxSource = source.slice(Math.max(0, toggleLabelIndex - 500), toggleLabelIndex + 200);
  assert.match(checkboxSource, /configuredModel\.source === 'upstream'/);
});

test('api keys edit modal bundles per-key settings', () => {
  // Edit modal state variable exists
  assert.match(apiKeysSource, /editKeyModalOpen|editingKey|editingKeyId/);

  // Multiple dialog roles: create key modal + edit key modal
  const dialogCount = (apiKeysSource.match(/role="dialog"/g) ?? []).length;
  assert.ok(dialogCount >= 2, `expected >= 2 role="dialog" elements, found ${dialogCount}`);

  // Model access select, routing pool select, key limits, budgets, rename still in page source
  // (they live inside the edit modal but the source-level assertions remain valid)
  assert.match(apiKeysSource, /updateAPIKeyRoutingPool/);
  assert.match(apiKeysSource, /updateAPIKeyLimits/);
  assert.match(apiKeysSource, /updateAPIKeyBudgets/);
  assert.match(apiKeysSource, /updateAPIKeyName/);
  assert.match(apiKeysSource, /setAPIKeyDisabled/);
  assert.match(apiKeysSource, /revokeKey/);
});

test('api keys table keeps lifecycle actions in status and action cells', () => {
  assert.match(apiKeysSource, /physicalDeleteAt/);
  assert.match(apiKeysSource, /keyPhysicalDeleteTitle/);
  assert.match(apiKeysSource, /role="switch"/);
  assert.match(apiKeysSource, /onchange=\{\(\) => setAPIKeyDisabled\(key\.id, !key\.disabledAt\)\}/);
  assert.match(apiKeysSource, /Delete/);
  assert.match(apiKeysSource, /deleteRevokedKey/);
  assert.match(apiKeysSource, /Permanently delete/);
  assert.match(apiKeysSource, /deleteConfirmKeyPopover/);
  assert.match(apiKeysSource, /openDeleteConfirmKey/);
  assert.match(apiKeysSource, /openBulkDeleteConfirm/);
  assert.match(apiKeysSource, /openBulkPermanentDeleteConfirm/);
  assert.match(apiKeysSource, /confirmDeleteKey/);
  assert.match(apiKeysSource, /Delete this API key\?/);
  assert.match(apiKeysSource, /Permanently delete this API key\?/);
  assert.match(apiKeysSource, /Delete selected API keys\?/);
  assert.match(apiKeysSource, /Permanently delete selected API keys\?/);
  assert.match(apiKeysSource, /bulkDeleteSelectedRevokedAPIKeys/);
  assert.doesNotMatch(apiKeysSource, /\bconfirm\(/);
  // JS identifiers bulkRevokeSelectedAPIKeys / revokeKey contain 'Revoke'; check visible text only
  assert.doesNotMatch(apiKeysSource, />\s*Revoke/);
  assert.doesNotMatch(apiKeysSource, /href=\{`\/request-logs\?clientKeyId=\$\{key\.id\}`\}/);
  assert.match(apiKeysSource, /openKeyLogsModal\(key\.id\)/);
  assert.match(apiKeysSource, /aria-label="API key logs"/);
});

test('api keys page uses modal to create keys and removes gateway model-settings section', () => {
  // Modal state and trigger
  assert.match(apiKeysSource, /createKeyModalOpen/);
  assert.match(apiKeysSource, /Create key/);
  assert.match(apiKeysSource, /role="dialog"/);
  assert.match(apiKeysSource, /aria-modal="true"/);
  assert.match(apiKeysSource, /Create API key/);
  assert.match(apiKeysSource, /submitCreateKey/);

  // Removed sections
  assert.doesNotMatch(apiKeysSource, /Gateway runtime limits/);
  assert.doesNotMatch(apiKeysSource, /Gateway default model/);

  // Removed imports (must not import modelSettings / saveModelSettings / gatewayLimitLabel)
  assert.doesNotMatch(apiKeysSource, /\bmodelSettings\b/);
  assert.doesNotMatch(apiKeysSource, /\bsaveModelSettings\b/);
  assert.doesNotMatch(apiKeysSource, /\bgatewayLimitLabel\b/);

  // Still loads model routing, gateway settings (for per-key fallback), routing pools, usage summary
  assert.match(apiKeysSource, /loadModelRouting/);
  assert.match(apiKeysSource, /loadGatewaySettings/);
  assert.match(apiKeysSource, /loadRoutingPools/);
  assert.doesNotMatch(apiKeysSource, /loadUsageSummary/);

  // oneTimeSecret stays on page (not inside modal)
  assert.match(apiKeysSource, /oneTimeSecret/);
});
