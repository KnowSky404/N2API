import { existsSync, readFileSync } from 'node:fs';
import { test } from 'node:test';
import assert from 'node:assert/strict';

const expectedFiles = [
  'src/lib/AuthGate.svelte',
  'src/lib/admin-state.svelte.js',
  'src/routes/+layout.svelte',
  'src/routes/+page.svelte',
  'src/routes/gateway/+page.svelte',
  'src/routes/providers/+page.svelte',
  'src/routes/routing-pools/+page.svelte',
  'src/routes/models/+page.svelte',
  'src/routes/api-keys/+page.svelte',
  'src/routes/request-logs/+page.svelte',
  'src/routes/ops/+page.svelte',
  'src/routes/fingerprints/+page.svelte',
  'src/routes/error-passthrough/+page.svelte'
];

const requestLogsPage = readFileSync('src/routes/request-logs/+page.svelte', 'utf8');
const modelsPage = readFileSync('src/routes/models/+page.svelte', 'utf8');
const gatewayPage = readFileSync('src/routes/gateway/+page.svelte', 'utf8');
const providersPage = readFileSync('src/routes/providers/+page.svelte', 'utf8');
const apiKeysPage = readFileSync('src/routes/api-keys/+page.svelte', 'utf8');
const dashboardPage = readFileSync('src/routes/+page.svelte', 'utf8');
const opsPage = readFileSync('src/routes/ops/+page.svelte', 'utf8');
const adminState = readFileSync('src/lib/admin-state.svelte.js', 'utf8');
const authGate = readFileSync('src/lib/AuthGate.svelte', 'utf8');

test('admin UI has focused routes behind a shared sidebar shell', () => {
  for (const file of expectedFiles) {
    assert.equal(existsSync(file), true, `${file} should exist`);
  }

  const layout = readFileSync('src/routes/+layout.svelte', 'utf8');
  for (const label of ['Dashboard', 'Gateway', 'Providers', 'Routing pools', 'API Keys', 'Request Logs', 'Ops', 'Fingerprints', 'Error rules', 'Sign out', 'Change password', 'Update', 'Current password', 'New password', 'min 8 chars']) {
    assert.match(layout, new RegExp(label.replace(' ', '\\s+')), `layout should include ${label}`);
  }
  assert.doesNotMatch(layout, /label:\s*'Models'/);
  assert.match(layout, /changePassword/);
  assert.match(layout, /changePasswordForm\.currentPassword/);
  assert.match(layout, /changePasswordForm\.newPassword/);
  assert.match(layout, /onsubmit={handleChangePassword}/);
});

test('primary navigation moves model policy ownership to API keys', () => {
  const layout = readFileSync('src/routes/+layout.svelte', 'utf8');

  assert.doesNotMatch(layout, /href:\s*'\/models'/);
  assert.doesNotMatch(layout, /label:\s*'Routing'/);
  assert.match(layout, /href:\s*'\/gateway'/);
  assert.match(layout, /href:\s*'\/api-keys'/);
});

test('routing pools page manages account pools', () => {
  const layout = readFileSync('src/routes/+layout.svelte', 'utf8');
  const poolsPage = readFileSync('src/routes/routing-pools/+page.svelte', 'utf8');

  assert.match(layout, /href:\s*'\/routing-pools'/);
  for (const label of ['Routing pools', 'Enabled', 'Fallback pool', 'No fallback', 'Pool priority', 'Bound API keys', 'Schedulable members', 'Search pools', 'Status filter', 'All pools', 'Enabled pools', 'Disabled pools', 'Pools with fallback', 'Bound key pools', 'Empty pools']) {
    assert.match(poolsPage, new RegExp(label.replace(' ', '\\s+')), `routing pools page should include ${label}`);
  }
  assert.match(poolsPage, /apiKeys/);

  // Modal state exists
  assert.match(poolsPage, /showCreateModal/);

  // Opener button visible in header (not inside the modal if block)
  assert.match(poolsPage, /<button[\s\S]*?onclick[\s\S]*?showCreateModal\s*=\s*true/);

  // Cancel button in modal
  assert.match(poolsPage, new RegExp("Cancel"));

  // Form is guarded by {#if showCreateModal}
  // Single container dialog with aria-modal="true" (Providers modal pattern)
  assert.match(poolsPage, /aria-modal="true"/);
  // Backdrop close uses event target guard
  assert.match(poolsPage, /e\.target\s*===\s*e\.currentTarget/);


  // Create modal form must not use responsive multi-column grid
  // (scope to the form inside {#if showCreateModal} — other page grids may use sm:grid-cols-2)
  const _modalFormContent = poolsPage.match(/\{#if\s+showCreateModal\}[\s\S]*?<form[\s\S]*?<\/form>/)?.[0] ?? '';
  assert.doesNotMatch(_modalFormContent, /sm:grid-cols-2/, 'create modal form must not use sm:grid-cols-2');
  assert.doesNotMatch(_modalFormContent, /lg:grid-cols-\[/, 'create modal form must not use lg:grid-cols-[...]');

  // Modal form inside {#if showCreateModal} must use vertical single-column layout
  const _fullModalForm = poolsPage.match(/{#if\s+showCreateModal}[\s\S]*?<form[\s\S]*?<\/form>/)?.[0] ?? '';
  assert.ok(_fullModalForm, 'create modal form should exist');
  assert.match(_fullModalForm, /space-y-4/, 'create modal form should use vertical spacing (space-y-4)');

  // Modal labels must be block/grid containers so space-y-4 stacks them vertically
  // Count: Pool name, Description, Fallback pool = 3 label elements with grid gap-2
  const _modalLabelGrids = _fullModalForm.match(/class="grid gap-2 text-sm font-medium text-\[#3c3c3c\]"/g) ?? [];
  assert.equal(_modalLabelGrids.length, 3, 'create modal form labels should use grid gap-2 class (3 fields)');

  // Each label should NOT have leftover mt-2 (grid gap-2 handles spacing)
  assert.doesNotMatch(_fullModalForm, /label[\s\S]*?mt-2/, 'create modal form labels must not use mt-2 (grid gap-2 replaces it)');

  // Modal panel must have viewport max-height and overflow scroll
  assert.match(poolsPage, /max-h-\[calc\(100vh/, 'modal panel should have viewport max-height');
  assert.match(poolsPage, /overflow-y-auto/, 'modal panel should scroll on small screens');
  assert.match(poolsPage, /\{#if\s+showCreateModal\}/);

  // Form fields are inside the if block (after {#if showCreateModal}, before {/if})
  // Page-level error guard for non-modal state (load/update/delete/save-membership errors)
  assert.match(poolsPage, /routingPools\.error\s*&&\s*!showCreateModal/);

  // Check that Pool name input with placeholder "Primary Codex" appears after the if
  assert.match(poolsPage, /showCreateModal\}[\s\S]*Pool\s+name/);

  // Opener button is NOT inside {#if showCreateModal} (it should be before the if block)
  assert.match(poolsPage, /Create\s+pool[\s\S]*\{#if\s+showCreateModal\}/);

  // On successful create, close modal: showCreateModal = false
  assert.match(poolsPage, /showCreateModal\s*=\s*false/);

  // submit handler uses await (not void)
  assert.match(poolsPage, /await\s+createRoutingPool/);

  // Legacy assertions still hold
  assert.match(poolsPage, /Pool accounts/);
  assert.match(poolsPage, /Save membership/);


  assert.match(poolsPage, /loadKeys/);
  assert.match(poolsPage, /boundAPIKeyCount\(pool\)/);
  assert.match(poolsPage, /schedulablePoolMemberCount\(pool\)/);
  assert.match(poolsPage, /pool\.fallbackPoolId/);
  assert.match(poolsPage, /pool\.id === candidate\.id/);
  assert.match(poolsPage, /fallbackPoolHref\(pool\)/);
  assert.match(poolsPage, /href=\{fallbackPoolHref\(pool\)\}/);
  assert.match(poolsPage, /routingPoolId=\$\{encodeURIComponent/);
  assert.match(poolsPage, /routingPools\.newPoolFallbackPoolId/);
  assert.match(poolsPage, /Create with no fallback/);
  assert.match(poolsPage, /poolAccountRows\(pool\)/);
  assert.match(poolsPage, /poolHasAccount\(pool,\s*left\.id\)/);
  assert.match(poolsPage, /poolAccountPriority\(pool,\s*left\.id\)\s*-\s*poolAccountPriority\(pool,\s*right\.id\)/);
  assert.match(poolsPage, /from '\$app\/state'/);
  assert.match(poolsPage, /page\.url\.search/);
  assert.match(poolsPage, /appliedRoutingPoolSearch/);
  assert.match(poolsPage, /routingPoolId = params\.get\('routingPoolId'\)/);
  assert.match(poolsPage, /selectedRoutingPoolId = routingPoolId/);
  assert.match(poolsPage, /visibleRoutingPools/);
  assert.match(poolsPage, /routingPoolSearch/);
  assert.match(poolsPage, /routingPoolStatusFilter/);
  assert.match(poolsPage, /poolMatchesStatusFilter\(pool,\s*routingPoolStatusFilter\)/);
  assert.match(poolsPage, /poolSearchText\(pool\)\.includes\(query\)/);
  assert.match(poolsPage, /href=\{`\/request-logs\?routingPoolId=\$\{pool\.id\}`\}/);
  assert.match(poolsPage, /href=\{`\/api-keys\?routingPoolId=\$\{pool\.id\}`\}/);
  assert.match(poolsPage, /function routingPoolDiagnosticsHref/);
  assert.match(poolsPage, /href=\{routingPoolDiagnosticsHref\(pool\)\}/);
  assert.match(poolsPage, /\/models\?routingPoolId=\$\{encodeURIComponent/);
  assert.match(poolsPage, /function routingPoolFallbackChainLabel/);
  assert.match(poolsPage, /current\.name \|\| `pool \$\{current\.id\}`/);
  assert.match(poolsPage, /function routingPoolFallbackChainLogsHref/);
  assert.match(poolsPage, /routingPoolChain=\$\{encodeURIComponent\(chain\)\}/);
  assert.match(poolsPage, /href=\{routingPoolFallbackChainLogsHref\(pool\)\}/);
  assert.match(poolsPage, /href=\{`\/providers\?providerAccountId=\$\{account\.id\}`\}/);
  assert.match(poolsPage, /View request logs/);
  assert.match(poolsPage, /View API keys/);
  assert.match(poolsPage, /View routing diagnostics/);
  assert.match(poolsPage, /View fallback chain logs/);
  assert.match(poolsPage, /View provider account/);
  // Edit modal state and helpers
  assert.match(poolsPage, /editingRoutingPoolId/);
  assert.match(poolsPage, /const\s+editingRoutingPool\b/);
  assert.match(poolsPage, /function routingPoolLabel\(pool\)/);
  assert.match(poolsPage, /function openRoutingPoolEditor\(pool\)/);
  assert.match(poolsPage, /function closeRoutingPoolEditor/);
  assert.match(poolsPage, /editingRoutingPoolId\s*=\s*pool\.id/);
  assert.match(poolsPage, /editingRoutingPoolId = 0/);
  assert.match(poolsPage, /onclick=\{closeRoutingPoolEditor\}/);

  // Main table has min-w-[980px] for scanning
  assert.match(poolsPage, /min-w-\[980px\]/);

  // Row inline enabled switch (main table, not edit modal)
  assert.match(poolsPage, /role="switch"/);
  assert.match(poolsPage, /pool\.enabled = event\.currentTarget\.checked/);
  assert.match(poolsPage, /void updateRoutingPool\(pool\)/);

  // Sticky Actions column in main table
  assert.match(poolsPage, /sticky right-0.*Actions/);

  // Main table row Actions includes Delete that calls deleteRoutingPool(pool.id)
  assert.match(poolsPage, /deleteRoutingPool\(pool\.id\)/);

  // Edit modal with role="dialog" aria-modal="true"
  assert.match(poolsPage, /role="dialog"/);
  const editModalCount = (poolsPage.match(/aria-modal="true"/g) ?? []).length;
  assert.ok(editModalCount >= 2, `edit and create modals both have aria-modal, found ${editModalCount}`);

  // Save membership inside edit modal (after the modal opening)
  assert.match(poolsPage, /editingRoutingPool[\s\S]*Save membership/);

  assert.match(adminState, /loadRoutingPools/);
  assert.match(adminState, /createRoutingPool/);
  assert.doesNotMatch(adminState, /const fallbackPoolId = 0/);
  assert.match(adminState, /replaceRoutingPoolAccounts/);
});

test('gateway page manages runtime limits and usage visibility', () => {
  for (const label of [
    'Gateway management',
    'Gateway actions',
    'Gateway readiness',
    'Provider accounts',
    'Routing diagnostics',
    'Schedulable accounts',
    'Routable models',
    'Active API keys',
    'Scheduling health',
    'Enabled accounts',
    'Blocked accounts',
    'Blocked reasons',
    'No blocked provider accounts.',
    'Runtime limits',
    'Gateway concurrency',
    'Per account concurrency',
    'Per key concurrency',
    'Requests per minute',
    'Tokens per minute',
    'Provider account auto tests',
    'Enable auto tests',
    'Interval seconds',
    'Auto-test status',
    'Last finished',
    'Accounts tested',
    'Last error',
    'Not run yet',
    '24h usage',
    'Top models',
    'Top provider accounts',
    'Top usage sources',
    'Top routing pool chains',
    'Top client keys',
    'Top sessions'
  ]) {
    assert.match(gatewayPage, new RegExp(label.replace(' ', '\\s+')), `gateway page should include ${label}`);
  }

  for (const href of ['/providers', '/api-keys', '/request-logs', '/models']) {
    assert.match(gatewayPage, new RegExp(`href="${href}"`), `gateway page should link to ${href}`);
  }
  assert.match(gatewayPage, /href="\/providers\?status=active"/);
  assert.match(gatewayPage, /href="\/providers\?status=blocked"/);
  assert.match(gatewayPage, /href="\/providers\?status=all"/);
  assert.match(gatewayPage, /href="\/models\?status=routable"/);
  assert.match(gatewayPage, /href="\/api-keys\?status=active"/);
  assert.match(gatewayPage, /href="\/request-logs\?gatewayFallbacks=1"/);
  assert.match(gatewayPage, /function gatewayUsageSinceParam/);
  assert.match(gatewayPage, /function gatewayUsageHrefWithSince/);
  assert.match(gatewayPage, /params\.set\('since', gatewayUsageSinceParam\(\)\)/);

  assert.match(gatewayPage, /loadGatewaySettings/);
  assert.match(gatewayPage, /loadProviderAccounts/);
  assert.match(gatewayPage, /loadModelRouting/);
  assert.match(gatewayPage, /loadKeys/);
  assert.match(gatewayPage, /getSchedulableProviderAccounts/);
  assert.match(gatewayPage, /getUnschedulableProviderAccountSummary/);
  assert.match(gatewayPage, /unschedulableReasonHref/);
  assert.match(gatewayPage, /unschedulableAccountSummary/);
  assert.match(gatewayPage, /href={unschedulableReasonHref\(item\.reason\)}/);
  assert.match(gatewayPage, /enabledProviderAccountCount/);
  assert.match(gatewayPage, /getRoutableModelCount/);
  assert.match(gatewayPage, /getActiveKeys/);
  assert.match(gatewayPage, /loadUsageSummary\('24h', 'model'\)/);
  assert.match(gatewayPage, /loadUsageSummary\('24h', 'provider_account'\)/);
  assert.match(gatewayPage, /loadUsageSummary\('24h', 'usage_source'\)/);
  assert.match(gatewayPage, /loadUsageSummary\('24h', 'routing_pool'\)/);
  assert.match(gatewayPage, /loadUsageSummary\('24h', 'routing_pool_chain'\)/);
  assert.match(gatewayPage, /loadUsageSummary\('24h', 'client_key'\)/);
  assert.match(gatewayPage, /loadUsageSummary\('24h', 'session'\)/);
  assert.match(gatewayPage, /gatewayLimitLabel/);
  assert.match(gatewayPage, /updateGatewaySettings/);
  assert.match(gatewayPage, /Save runtime limits/);
  assert.match(gatewayPage, /bind:value=\{gatewaySettings\.data\.maxConcurrentGatewayRequests\}/);
  assert.match(gatewayPage, /bind:value=\{gatewaySettings\.data\.maxConcurrentRequestsPerAccount\}/);
  assert.match(gatewayPage, /bind:value=\{gatewaySettings\.data\.maxConcurrentRequestsPerKey\}/);
  assert.match(gatewayPage, /bind:value=\{gatewaySettings\.data\.requestsPerMinutePerKey\}/);
  assert.match(gatewayPage, /bind:value=\{gatewaySettings\.data\.tokensPerMinutePerKey\}/);
  assert.match(gatewayPage, /bind:checked=\{gatewaySettings\.data\.providerAccountAutoTestEnabled\}/);
  assert.match(gatewayPage, /bind:value=\{gatewaySettings\.data\.providerAccountAutoTestIntervalSeconds\}/);
  assert.match(gatewayPage, /Request log retention/);
  assert.match(gatewayPage, /Clean request logs/);
  assert.match(gatewayPage, /bind:value=\{gatewaySettings\.data\.requestLogRetentionDays\}/);
  assert.match(gatewayPage, /cleanupRequestLogs/);
  assert.match(gatewayPage, /gatewaySettings\.data\.providerAccountAutoTestStatus/);
  assert.match(gatewayPage, /lastFinishedAt/);
  assert.match(gatewayPage, /lastAccountCount/);
  assert.match(gatewayPage, /lastError/);
  assert.match(adminState, /export async function updateGatewaySettings/);
  assert.match(adminState, /\/api\/admin\/gateway-settings/);
  assert.match(adminState, /providerAccountAutoTestEnabled: Boolean/);
  assert.match(adminState, /providerAccountAutoTestIntervalSeconds: Number/);
  assert.match(adminState, /requestLogRetentionDays: Number/);
  assert.match(adminState, /export async function cleanupRequestLogs/);
  assert.match(adminState, /\/api\/admin\/request-logs\/cleanup/);
  assert.match(adminState, /providerAccountAutoTestStatus/);
  assert.match(adminState, /lastStartedAt/);
  assert.match(adminState, /lastFinishedAt/);
  assert.match(adminState, /lastAccountCount/);
  assert.match(adminState, /lastError/);
  assert.match(gatewayPage, /formatCostMicrousd/);
});

test('request logs page shows provider account attribution', () => {
  assert.match(requestLogsPage, /Provider account/);
  assert.match(requestLogsPage, /log\.provider\s*\|\|/);
  assert.match(requestLogsPage, /log\.providerAccountName/);
  assert.match(requestLogsPage, /log\.providerAccountType/);
  assert.match(requestLogsPage, /log\.providerAccountId/);
  assert.match(requestLogsPage, /Routing pool/);
  assert.match(requestLogsPage, /routing_pool_chain/);
  assert.match(requestLogsPage, /log\.routingPoolName/);
  assert.match(requestLogsPage, /log\.routingPoolId/);
  assert.match(requestLogsPage, /href=\{`\/routing-pools\?routingPoolId=\$\{log\.routingPoolId\}`\}/);
  assert.match(requestLogsPage, /View routing pool/);
  assert.match(requestLogsPage, /Global pool/);
});

test('request logs page shows request model', () => {
  assert.match(requestLogsPage, />Model</);
  assert.match(requestLogsPage, /log\.model/);
});

test('request logs page shows sticky session attribution', () => {
  assert.match(requestLogsPage, />Session</);
  assert.match(requestLogsPage, /log\.sessionId/);
  assert.match(requestLogsPage, /colspan="15"/);
});

test('request logs page shows token usage', () => {
  assert.match(requestLogsPage, />Tokens</);
  assert.match(requestLogsPage, />Usage</);
  assert.match(requestLogsPage, /log\.inputTokens/);
  assert.match(requestLogsPage, /log\.outputTokens/);
  assert.match(requestLogsPage, /log\.totalTokens/);
  assert.match(requestLogsPage, /log\.cachedInputTokens/);
  assert.match(requestLogsPage, /log\.reasoningTokens/);
  assert.match(requestLogsPage, /Cached/);
  assert.match(requestLogsPage, /Reasoning/);
  assert.match(requestLogsPage, /log\.usageSource/);
  assert.match(requestLogsPage, /gemini_usage_metadata/);
  assert.match(requestLogsPage, /Gemini/);
  assert.match(requestLogsPage, /anthropic_usage/);
  assert.match(requestLogsPage, /Anthropic/);
  assert.match(requestLogsPage, /formatRequestLogCost/);
  assert.match(requestLogsPage, /Unpriced/);
});

test('request logs page includes usage accounting UI', () => {
  for (const label of [
    'Usage summary',
    'Estimated cost',
    'Input tokens',
    'Output tokens',
    'Cached input tokens',
    'Reasoning tokens',
    'Pricing',
    'Session'
  ]) {
    assert.match(requestLogsPage, new RegExp(label.replace(' ', '\\s+')), `request logs page should include ${label}`);
  }
  assert.match(requestLogsPage, /function usageRowHref/);
  assert.match(requestLogsPage, /function usageRangeSinceParam/);
  assert.match(requestLogsPage, /params\.set\('since', usageRangeSinceParam\(\)\)/);
  assert.match(requestLogsPage, /params\.set\('model', id\)/);
  assert.match(requestLogsPage, /usage\.groupBy === 'model'/);
  assert.match(requestLogsPage, /usage\.groupBy === 'client_key'/);
  assert.match(requestLogsPage, /usage\.groupBy === 'provider_account'/);
  assert.match(requestLogsPage, /usage\.groupBy === 'routing_pool'/);
  assert.match(requestLogsPage, /usage\.groupBy === 'routing_pool_chain'/);
  assert.match(requestLogsPage, /usage\.groupBy === 'session'/);
  assert.match(requestLogsPage, /usage\.groupBy === 'usage_source'/);
  assert.match(requestLogsPage, /value: 'usage_source'/);
  assert.match(requestLogsPage, /row\.cachedInputTokens/);
  assert.match(requestLogsPage, /row\.reasoningTokens/);
  assert.match(requestLogsPage, /summary\?\.totalCachedInputTokens/);
  assert.match(requestLogsPage, /summary\?\.totalReasoningTokens/);
  assert.match(adminState, /totalCachedInputTokens/);
  assert.match(adminState, /totalReasoningTokens/);
  assert.match(requestLogsPage, /href=\{usageRowHref\(row\)\}/);
});

test('request logs export links request explicit formats', () => {
  assert.match(requestLogsPage, /href=\{exportRequestLogsURL\("csv"\)\}[\s\S]*Export CSV/);
  assert.match(requestLogsPage, /href=\{exportRequestLogsURL\("json"\)\}[\s\S]*Export JSON/);
  assert.match(requestLogsPage, /href=\{exportRequestLogsURL\("jsonl"\)\}[\s\S]*Export JSONL/);
});

test('request logs page formats gateway error codes for scanning', () => {
  assert.match(requestLogsPage, /function errorLabel/);
  assert.match(requestLogsPage, /function errorHref/);
  assert.match(requestLogsPage, /errorLabel\(log\.error\)/);
  assert.match(requestLogsPage, /title=\{log\.error/);
  assert.match(requestLogsPage, /href=\{errorHref\(log\)\}/);
  assert.match(requestLogsPage, /function requestLogDrilldownParams/);
  assert.match(requestLogsPage, /params\.set\('since', requestLogs\.since\)/);
  assert.match(requestLogsPage, /params\.set\('error', log\.error\)/);
  assert.match(requestLogsPage, /View same error logs/);
  assert.match(requestLogsPage, /bind:value=\{requestLogs\.errorCode\}/);
  assert.match(requestLogsPage, /Error code filter/);
  assert.match(adminState, /errorCode: ''/);
  assert.match(adminState, /requestLogs\.errorCode/);
  assert.match(adminState, /params\.set\('error'/);
  for (const code of [
    'api_key_request_rate_limited',
    'api_key_token_rate_limited',
    'gateway_concurrency_limited',
    'api_key_concurrency_limited',
    'provider_account_concurrency_limited'
  ]) {
    const label = code
      .split('_')
      .filter(Boolean)
      .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
      .join(' ');
    assert.equal(label.includes('_'), false, `${code} should render without underscores`);
  }
});

test('request logs page shows gateway fallback diagnostics', () => {
  assert.match(requestLogsPage, /Gateway diagnostics/);
  assert.match(requestLogsPage, /log\.gatewayAttemptCount/);
  assert.match(requestLogsPage, /log\.gatewayFallbackCount/);
  assert.match(requestLogsPage, /log\.routingPoolFallbackDepth/);
  assert.match(requestLogsPage, /log\.routingPoolFallbackChain/);
  assert.match(requestLogsPage, /function routingPoolFallbackChainHref/);
  assert.match(requestLogsPage, /params\.set\('routingPoolChain', log\.routingPoolFallbackChain\)/);
  assert.match(requestLogsPage, /href=\{routingPoolFallbackChainHref\(log\)\}/);
  assert.match(requestLogsPage, /View fallback chain logs/);
  assert.match(requestLogsPage, /log\.routingPoolError/);
  assert.match(requestLogsPage, /bind:value=\{requestLogs\.routingPoolError\}/);
  assert.match(requestLogsPage, /bind:value=\{requestLogs\.routingPoolChain\}/);
  assert.match(requestLogsPage, /bind:checked=\{requestLogs\.gatewayFallbacks\}/);
  assert.match(requestLogsPage, /Only fallback requests/);
  assert.match(requestLogsPage, /Fallback chain/);
  assert.match(adminState, /gatewayAttemptCount/);
  assert.match(adminState, /gatewayFallbackCount/);
  assert.match(adminState, /gatewayFallbacks: false/);
  assert.match(adminState, /routingPoolFallbackDepth/);
  assert.match(adminState, /routingPoolError: 'all'/);
  assert.match(adminState, /routingPoolChain: ''/);
  assert.match(adminState, /params\.set\('routingPoolError'/);
  assert.match(adminState, /params\.set\('routingPoolChain'/);
  assert.match(adminState, /params\.set\('gatewayFallbacks', '1'\)/);
});

test('request logs page filters by search and status class', () => {
  assert.match(requestLogsPage, /bind:value=\{requestLogs\.query\}/);
  assert.match(requestLogsPage, /bind:value=\{requestLogs\.requestId\}/);
  assert.match(requestLogsPage, /bind:value=\{requestLogs\.statusClass\}/);
  assert.match(requestLogsPage, /bind:value=\{requestLogs\.statusCode\}/);
  assert.match(requestLogsPage, /bind:value=\{requestLogs\.model\}/);
  assert.match(requestLogsPage, /bind:value=\{requestLogs\.sessionId\}/);
  assert.match(requestLogsPage, /value: 'routing_pool', label: 'Routing pool'/);
  assert.match(requestLogsPage, /routing_pool_empty/);
  assert.match(requestLogsPage, /Model filter/);
  assert.match(requestLogsPage, /Request ID filter/);
  assert.match(requestLogsPage, /Session filter/);
  assert.match(requestLogsPage, /Status code filter/);
  assert.match(requestLogsPage, /statusClass/);
  assert.match(adminState, /params\.set\('q'/);
  assert.match(adminState, /params\.set\('requestId'/);
  assert.match(adminState, /params\.set\('statusClass'/);
  assert.match(adminState, /params\.set\('model'/);
  assert.match(adminState, /params\.set\('sessionId'/);
  assert.match(adminState, /requestLogs\.query/);
  assert.match(adminState, /requestLogs\.statusClass/);
});

test('request logs page filters by provider account', () => {
  assert.match(requestLogsPage, /providerAccounts/);
  assert.match(requestLogsPage, /bind:value=\{requestLogs\.providerAccountId\}/);
  assert.match(requestLogsPage, /All provider accounts/);
  assert.match(requestLogsPage, /href=\{`\/api-keys\?clientKeyId=\$\{log\.clientKeyId\}`\}/);
  assert.match(requestLogsPage, /View API key/);
  assert.match(requestLogsPage, /href=\{`\/providers\?providerAccountId=\$\{log\.providerAccountId\}`\}/);
  assert.match(requestLogsPage, /View provider account/);
  assert.match(requestLogsPage, /routingPools/);
  assert.match(requestLogsPage, /loadRoutingPools\(\)/);
  assert.match(requestLogsPage, /bind:value=\{requestLogs\.routingPoolId\}/);
  assert.match(requestLogsPage, /All routing pools/);
  assert.match(requestLogsPage, /apiKeys/);
  assert.match(requestLogsPage, /loadKeys\(\)/);
  assert.match(requestLogsPage, /bind:value=\{requestLogs\.clientKeyId\}/);
  assert.match(requestLogsPage, /All API keys/);
  assert.match(requestLogsPage, /resetRequestLogFilters\(\)/);
  assert.match(adminState, /export function resetRequestLogFilters/);
  assert.match(adminState, /params\.set\('providerAccountId'/);
  assert.match(adminState, /params\.set\('routingPoolId'/);
  assert.match(adminState, /requestLogs\.providerAccountId/);
  assert.match(adminState, /requestLogs\.routingPoolId/);
  assert.match(requestLogsPage, /loadProviderAccounts\(\)/);
});

test('providers page initializes account search from provider account URL param', () => {
  assert.match(providersPage, /from '\$app\/state'/);
  assert.match(providersPage, /page\.url\.search/);
  assert.match(providersPage, /appliedProviderAccountSearch/);
  assert.match(providersPage, /providerAccountId = params\.get\('providerAccountId'\)/);
  assert.match(providersPage, /accountSearch = `id:\$\{providerAccountId\}`/);
  assert.match(providersPage, /status = params\.get\('status'\)/);
  assert.match(providersPage, /accountStatusFilter = status/);
  assert.match(providersPage, /String\(account\.id\) === idQuery/);
});

test('api keys page initializes key search from client key URL param', () => {
  assert.match(apiKeysPage, /from '\$app\/state'/);
  assert.match(apiKeysPage, /page\.url\.search/);
  assert.match(apiKeysPage, /appliedAPIKeySearch/);
  assert.match(apiKeysPage, /clientKeyId = params\.get\('clientKeyId'\)/);
  assert.match(apiKeysPage, /routingPoolId = params\.get\('routingPoolId'\)/);
  assert.match(apiKeysPage, /status = params\.get\('status'\)/);
  assert.match(apiKeysPage, /\['all', 'active', 'disabled', 'revoked'\]\.includes\(status\)/);
  assert.match(apiKeysPage, /keySearch = `id:\$\{clientKeyId\}`/);
  assert.match(apiKeysPage, /keySearch = `pool:\$\{routingPoolId\}`/);
  assert.match(apiKeysPage, /String\(key\.id\) === idQuery/);
  assert.match(apiKeysPage, /String\(key\.routingPoolId/);
});

test('api keys page links routing pool assignments to pool details', () => {
  assert.match(apiKeysPage, /function modelRoutingHref\(model,\s*key\)/);
  assert.match(apiKeysPage, /routingPoolId=\$\{encodeURIComponent\(String\(key\.routingPoolId\)\)\}/);
  assert.match(apiKeysPage, /href=\{modelRoutingHref\(model,\s*editingKey\)\}/);
  assert.match(apiKeysPage, /function apiKeyRoutingPoolHref/);
  assert.match(apiKeysPage, /function apiKeyRoutingPoolFallbackHref/);
  assert.match(apiKeysPage, /function apiKeyRoutingPoolFallbackChainLabel/);
  assert.match(apiKeysPage, /current\.name \|\| `pool \$\{current\.id\}`/);
  assert.match(apiKeysPage, /function apiKeyRoutingPoolFallbackChainLogsHref/);
  assert.match(apiKeysPage, /routingPoolId=\$\{encodeURIComponent/);
  assert.match(apiKeysPage, /clientKeyId=\$\{encodeURIComponent\(String\(key\.id\)\)\}/);
  assert.match(apiKeysPage, /routingPoolChain=\$\{encodeURIComponent\(chain\)\}/);
  assert.match(apiKeysPage, /href=\{apiKeyRoutingPoolHref\(editingKey\)\}/);
  assert.match(apiKeysPage, /href=\{apiKeyRoutingPoolFallbackHref\(editingKey\)\}/);
  assert.match(apiKeysPage, /href=\{apiKeyRoutingPoolFallbackChainLogsHref\(editingKey\)\}/);
  assert.match(apiKeysPage, /routingPoolFallbackNameForKey\(editingKey\)/);
  assert.match(apiKeysPage, /View fallback chain logs/);
});

test('request logs page initializes filters from URL params', () => {
  assert.match(requestLogsPage, /from '\$app\/state'/);
  assert.match(requestLogsPage, /page\.url\.search/);
  assert.match(requestLogsPage, /requestLogs\.providerAccountId = providerAccountId/);
  assert.match(requestLogsPage, /requestLogs\.requestId = requestId/);
  assert.match(requestLogsPage, /requestLogs\.routingPoolId = routingPoolId/);
  assert.match(requestLogsPage, /requestLogs\.clientKeyId = clientKeyId/);
  assert.match(requestLogsPage, /requestLogs\.model = model/);
  assert.match(requestLogsPage, /requestLogs\.sessionId = sessionId/);
  assert.match(requestLogsPage, /requestLogs\.errorCode = error/);
  assert.match(requestLogsPage, /requestLogs\.statusCode = statusCode/);
  assert.match(requestLogsPage, /requestLogs\.since = since/);
  assert.match(requestLogsPage, /requestLogs\.routingPoolChain = routingPoolChain/);
  assert.match(requestLogsPage, /requestLogs\.query = query/);
  assert.match(requestLogsPage, /requestLogs\.statusClass = statusClass/);
  assert.match(requestLogsPage, /requestLogs\.gatewayFallbacks = true/);
  assert.match(requestLogsPage, /void loadRequestLogs\(\)/);
  assert.match(adminState, /params\.set\('clientKeyId'/);
  assert.match(adminState, /params\.set\('statusCode'/);
  assert.match(adminState, /params\.set\('since'/);
  assert.match(adminState, /requestId: ''/);
  assert.match(adminState, /statusCode: ''/);
  assert.match(adminState, /since: ''/);
  assert.match(adminState, /routingPoolId: 'all'/);
  assert.match(adminState, /clientKeyId: 'all'/);
  assert.match(adminState, /model: ''/);
  assert.match(adminState, /sessionId: ''/);
});

test('gateway usage rows link to filtered request logs', () => {
  assert.match(gatewayPage, /usageRowHref/);
  assert.match(gatewayPage, /params\.set\('model', id\)/);
  assert.match(gatewayPage, /params\.set\('sessionId', id\)/);
  assert.match(gatewayPage, /params\.set\('providerAccountId', accountId\)/);
  assert.match(gatewayPage, /params\.set\('routingPoolId', id\)/);
  assert.match(gatewayPage, /params\.set\('routingPoolChain', id\)/);
  assert.match(gatewayPage, /params\.set\('clientKeyId', id\)/);
  assert.match(gatewayPage, /params\.set\('usageSource', id\)/);
  assert.match(gatewayPage, /providerAccountUsageId/);
  assert.match(gatewayPage, /href=\{href\}/);
});

test('models page shows scheduling diagnostics for routing candidates', () => {
  assert.match(modelsPage, /N2API Routing Diagnostics/);
  assert.match(modelsPage, /Routing diagnostics/);
  assert.match(modelsPage, /from '\$app\/state'/);
  assert.match(modelsPage, /page\.url\.search/);
  assert.match(modelsPage, /function applyModelRoutingURLFilters/);
  assert.match(modelsPage, /modelSearch = model/);
  assert.match(modelsPage, /modelStatusFilter = status/);
  assert.match(modelsPage, /modelRoutingPreview\.model = previewModel/);
  assert.match(modelsPage, /modelRoutingPreview\.sessionId = sessionId/);
  assert.match(modelsPage, /modelRoutingPreview\.routingPoolId = routingPoolId/);
  assert.match(modelsPage, /modelRoutingPreview\.excludedAccountIds = excludedAccountIds/);
  assert.match(modelsPage, /clientKeyId = params\.get\('clientKeyId'\)/);
  assert.match(modelsPage, /modelDiagnosticClientKeyId = clientKeyId/);
  assert.match(modelsPage, /providerAccountId = params\.get\('providerAccountId'\)/);
  assert.match(modelsPage, /modelProviderAccountId = providerAccountId/);
  for (const label of ['Search models', 'Status filter', 'All models', 'Routable models', 'Blocked models', 'Hidden models', 'Allowed models']) {
    assert.match(modelsPage, new RegExp(label.replace(' ', '\\s+')), `models page should include ${label}`);
  }
  assert.match(modelsPage, /modelSearch/);
  assert.match(modelsPage, /modelStatusFilter/);
  assert.match(modelsPage, /visibleModelRoutingRows/);
  assert.match(modelsPage, /modelProviderAccountId/);
  assert.match(modelsPage, /selectedDiagnosticAPIKey/);
  assert.match(modelsPage, /loadKeys\(\)/);
  assert.match(modelsPage, /href=\{diagnosticAPIKeyHref\(selectedDiagnosticAPIKey\)\}/);
  assert.match(modelsPage, /clientKeyId=\$\{encodeURIComponent\(String\(key\.id\)\)\}/);
  assert.match(modelsPage, /modelMatchesStatusFilter\(model,\s*modelStatusFilter\)/);
  assert.match(modelsPage, /modelSearchText\(model\)\.includes\(query\)/);
  assert.match(modelsPage, /visibleModelAccounts\(model\)/);
  assert.match(modelsPage, /modelHasVisibleProviderAccount\(model\)/);
  assert.match(modelsPage, /String\(account\.id\) === modelProviderAccountId/);
  assert.match(modelsPage, /Schedule rank/);
  assert.match(modelsPage, /Schedule reason/);
  assert.match(modelsPage, /account\.scheduleRank/);
  assert.match(modelsPage, /account\.scheduleReason/);
  assert.match(modelsPage, /account\.schedulable/);
  assert.match(modelsPage, /account\.unschedulableReason/);
  assert.match(modelsPage, /No schedulable account/);
  assert.match(modelsPage, /Sticky bound/);
  assert.match(modelsPage, /account\.stickyBound/);
  assert.match(adminState, /stickyBoundAccountId/);
  assert.match(adminState, /currentConcurrentRequests/);
  assert.match(adminState, /effectiveMaxConcurrentRequests/);
  assert.match(adminState, /concurrencyBlocked/);
  assert.match(adminState, /diagnosisStatus/);
  assert.match(adminState, /diagnosisSummary/);
  assert.match(adminState, /diagnosisHints/);
  assert.match(adminState, /blockedReasonCounts/);
  assert.match(adminState, /changePassword/);
  assert.match(adminState, /\/api\/admin\/change-password/);
  assert.match(adminState, /unschedulableReasonHref/);
  assert.match(adminState, /scheduleReason/);
  assert.match(modelsPage, /Excluded account IDs/);
  assert.match(modelsPage, /Routing pool/);
  assert.match(modelsPage, /bind:value=\{modelRoutingPreview\.routingPoolId\}/);
  assert.match(modelsPage, /bind:value=\{modelRoutingPreview\.excludedAccountIds\}/);
  assert.match(modelsPage, /providerAccountHref\(account\)/);
  assert.match(modelsPage, /href=\{providerAccountHref\(account\)\}/);
  assert.match(modelsPage, /providerAccountId=\$\{encodeURIComponent/);
  assert.match(modelsPage, /View provider account/);
  assert.match(modelsPage, /function previewRoutingPoolHref/);
  assert.match(modelsPage, /href=\{previewRoutingPoolHref\(modelRoutingPreview\.result\)\}/);
  assert.match(modelsPage, /routingPoolId=\$\{encodeURIComponent/);
  assert.match(modelsPage, /View routing pool/);
  assert.match(modelsPage, /excluding \{modelRoutingPreview\.excludedAccountIds\}/);
  assert.match(modelsPage, /previewConcurrencyLimitLabel/);
  assert.match(modelsPage, /Routing diagnosis/);
  assert.match(modelsPage, /diagnosisStatusLabel/);
  assert.match(modelsPage, /diagnosisStatusClass/);
  assert.match(modelsPage, /modelRoutingPreview\.result\.diagnosisStatus/);
  assert.match(modelsPage, /modelRoutingPreview\.result\.diagnosisSummary/);
  assert.match(modelsPage, /modelRoutingPreview\.result\.diagnosisHints/);
  assert.match(modelsPage, /modelRoutingPreview\.result\.blockedReasonCounts/);
  assert.match(modelsPage, /function modelRoutingBlockedReasonHref/);
  assert.match(modelsPage, /href=\{modelRoutingBlockedReasonHref\(reason\.reason\)\}/);
  assert.match(modelsPage, /href=\{modelRoutingBlockedReasonHref\(reason\)\}/);
  assert.match(modelsPage, /params\.set\('status', 'disabled'\)/);
  assert.match(modelsPage, /params\.set\('status', 'rate_limited'\)/);
  assert.match(modelsPage, /params\.set\('status', 'circuit_open'\)/);
  assert.match(modelsPage, /params\.set\('status', 'expired'\)/);
  assert.match(modelsPage, /Repair hints/);
  assert.match(modelsPage, /Blocked reasons/);
  assert.match(modelsPage, /Active/);
  assert.match(modelsPage, /Concurrency full/);
  assert.match(adminState, /excludedAccountIds: ''/);
  assert.match(adminState, /routingPoolId: '0'/);
  assert.match(adminState, /params\.set\('routingPoolId'/);
  assert.match(adminState, /routingPoolFallbackChain/);
  assert.match(modelsPage, /routingPoolFallbackChain/);
  assert.match(modelsPage, /Routing pool chain/);
  assert.match(adminState, /params\.set\('excludedAccountIds'/);
});

test('api keys page does not show top-level 24h key usage section', () => {
  // 24h key usage section must be removed from the page
  assert.doesNotMatch(apiKeysPage, /24h key usage/);
  assert.doesNotMatch(apiKeysPage, /usage24hClientKeys/);
  assert.doesNotMatch(apiKeysPage, /clientKeyUsageHref/);
  assert.doesNotMatch(apiKeysPage, /function clientKeyUsageSinceParam/);
  assert.doesNotMatch(apiKeysPage, /loadUsageSummary\('24h', 'client_key'\)/);
  assert.doesNotMatch(apiKeysPage, /clientKeyUsageSinceParam\(\)/);

  // Admin-state rate/concurrency helpers still exist (may be used in edit modal)
  assert.match(adminState, /currentConcurrentRequests/);
  assert.match(adminState, /effectiveMaxConcurrentRequests/);
  assert.match(adminState, /concurrencyBlocked/);
  assert.match(adminState, /currentRequestsThisMinute/);
  assert.match(adminState, /effectiveRequestsPerMinute/);
  assert.match(adminState, /requestRateRemaining/);
  assert.match(adminState, /requestRateLimited/);
  assert.match(adminState, /currentTokensThisMinute/);
  assert.match(adminState, /effectiveTokensPerMinute/);
  assert.match(adminState, /tokenRateRemaining/);
  assert.match(adminState, /tokenRateLimited/);

  // Per-key rate labels still in page source (may move to edit modal)
  assert.match(apiKeysPage, /keyRateWindowLimitLabel/);
  assert.match(apiKeysPage, /keyRateRemainingLabel/);
  assert.match(apiKeysPage, /requestRateRemaining/);
  assert.match(apiKeysPage, /tokenRateRemaining/);
});

test('api keys table has 7 visible columns with correct headers', () => {
  // Row empty/loading colspan must be 7 (NOT 6 or 8)
  assert.match(apiKeysPage, /colspan="7"/);

  // The 7 expected column headers: Select, Name, Prefix, Created, Last used, Status, Action
  assert.match(apiKeysPage, />Name</);
  assert.match(apiKeysPage, />Prefix</);
  assert.match(apiKeysPage, />Created</);
  assert.match(apiKeysPage, />Last used</);
  assert.match(apiKeysPage, />Status</);
  assert.match(apiKeysPage, />Action</);

  // Old inline table headers must not appear as <th> elements
  assert.doesNotMatch(apiKeysPage, />Model access<\/th>/);
  assert.doesNotMatch(apiKeysPage, />Key limits<\/th>/);
});

test('api keys prefix column can copy reusable secrets', () => {
  assert.match(apiKeysPage, /copyAPIKeySecret/);
  assert.match(apiKeysPage, /onclick=\{\(\) => copyAPIKeySecret\(key\.id\)\}/);
  assert.match(apiKeysPage, /Copy full API key/);
  assert.match(apiKeysPage, /You can copy it again later from the Prefix column\./);
  assert.doesNotMatch(apiKeysPage, /It will not be shown again\./);

  assert.match(adminState, /export async function copyAPIKeySecret/);
  assert.match(adminState, /\/api\/admin\/keys\/\$\{id\}\/secret/);
  assert.match(adminState, /payload\.secret/);
});

test('api keys page has an Edit action modal for per-key settings', () => {
  // An edit-specific modal exists (in addition to the create key modal)
  // Page should have at least two role="dialog" elements: create key + edit key
  const dialogCount = (apiKeysPage.match(/role="dialog"/g) ?? []).length;
  assert.ok(dialogCount >= 2, `expected >= 2 role="dialog" elements, found ${dialogCount}`);

  // Edit modal state variable exists
  assert.match(apiKeysPage, /editKeyModalOpen|editingKey|editingKeyId/);

  // aria-modal="true" should appear at least twice (create + edit)
  const ariaModalCount = (apiKeysPage.match(/aria-modal="true"/g) ?? []).length;
  assert.ok(ariaModalCount >= 2, `expected >= 2 aria-modal="true" elements, found ${ariaModalCount}`);
});

test('api keys page keeps budget exceeded diagnostics reachable', () => {
  // Budget exceeded diagnostic text (may appear in edit modal)
  assert.match(apiKeysPage, /request budget exceeded/);
  assert.match(apiKeysPage, /token budget exceeded/);
  assert.match(apiKeysPage, /cost budget exceeded/);
  assert.match(apiKeysPage, /key\.requestBudgetExceeded/);
  assert.match(apiKeysPage, /key\.tokenBudgetExceeded/);
  assert.match(apiKeysPage, /key\.costBudgetExceeded/);
  assert.match(apiKeysPage, /costBudgetMicrousd24h/);
  assert.match(apiKeysPage, /costBudgetMicrousd30d/);

  // Must not use old budget "full" language
  assert.doesNotMatch(apiKeysPage, /request budget full/);
  assert.doesNotMatch(apiKeysPage, /token budget full/);

  // Admin-state backend contract: updateAPIKeyBudgets callable
  assert.match(adminState, /export async function updateAPIKeyBudgets/);
  assert.match(adminState, /\/api\/admin\/keys\/\$\{keyId\}\/budgets/);
  assert.match(adminState, /costBudgetMicrousd24h/);
  assert.match(adminState, /costBudgetMicrousd30d/);
});

test('api keys page filters key list locally', () => {
  for (const label of ['Search keys', 'Status filter', 'All keys', 'Active keys', 'Disabled keys', 'Deleted keys']) {
    assert.match(apiKeysPage, new RegExp(label.replace(' ', '\\s+')), `api keys page should include ${label}`);
  }

  assert.match(apiKeysPage, /let keySearch = \$state\(''\)/);
  assert.match(apiKeysPage, /let keyStatusFilter = \$state\('all'\)/);
  assert.match(apiKeysPage, /filteredAPIKeys/);
  assert.match(apiKeysPage, /apiKeySearchText/);
  assert.match(apiKeysPage, /bind:value=\{keySearch\}/);
  assert.match(apiKeysPage, /bind:value=\{keyStatusFilter\}/);
  assert.match(apiKeysPage, /Showing \{filteredAPIKeys\.length\} of \{apiKeys\.items\.length\}/);
  assert.match(apiKeysPage, /No API keys match your filters\./);
});

test('api keys page disables keys reversibly', () => {
  for (const label of ['Disabled', 'Deleted', 'Enabled']) {
    assert.match(apiKeysPage, new RegExp(label), `api keys page should include ${label}`);
  }

  assert.match(apiKeysPage, /setAPIKeyDisabled/);
  assert.match(apiKeysPage, /key\.disabledAt/);
  assert.match(apiKeysPage, /keyStatusFilter === 'disabled'/);
  assert.match(apiKeysPage, /role="switch"/);
  assert.match(apiKeysPage, /checked=\{!key\.disabledAt\}/);
  assert.match(apiKeysPage, /onchange=\{\(\) => setAPIKeyDisabled\(key\.id, !key\.disabledAt\)\}/);
  assert.match(apiKeysPage, /\{#if key\.revokedAt\}/);
  assert.match(apiKeysPage, /\{:else\}/);
  assert.match(adminState, /@property \{string \| null\} disabledAt/);
  assert.match(adminState, /export async function setAPIKeyDisabled/);
  assert.match(adminState, /\/api\/admin\/keys\/\$\{keyId\}\/disabled/);
  assert.match(adminState, /!key\.revokedAt && !key\.disabledAt/);
});

test('api keys action buttons stay compact on one row', () => {
  assert.match(apiKeysPage, /whitespace-nowrap/);
  assert.match(apiKeysPage, /inline-flex items-center justify-end gap-1/);
  assert.match(apiKeysPage, /class="inline-flex size-8 items-center justify-center/);
  assert.match(apiKeysPage, /<Pencil class="size-4"/);
  assert.match(apiKeysPage, /<ScrollText class="size-4"/);
  assert.match(apiKeysPage, /<Trash2 class="size-4"/);
});

test('api keys page renames keys without rotating secrets', () => {
  assert.match(apiKeysPage, /updateAPIKeyName/);
  assert.match(apiKeysPage, /Save name/);
  assert.match(apiKeysPage, /bind:value=\{editingKey\.name\}/);
  assert.match(apiKeysPage, /updateAPIKeyName\(editingKey\.id, editingKey\.name\)/);
  assert.match(adminState, /export async function updateAPIKeyName/);
  assert.match(adminState, /\/api\/admin\/keys\/\$\{keyId\}/);
  assert.match(adminState, /method: 'PATCH'/);
});

test('dashboard shows gateway scheduling capacity', () => {
  for (const label of ['Provider accounts', 'Schedulable accounts', 'Unschedulable accounts', 'Routable models', 'Active API keys']) {
    assert.match(dashboardPage, new RegExp(label.replace(' ', '\\s+')), `dashboard should include ${label}`);
  }

  assert.match(dashboardPage, /getSchedulableProviderAccounts/);
  assert.match(dashboardPage, /getUnschedulableProviderAccountSummary/);
  assert.match(dashboardPage, /getRoutableModelCount/);
  assert.match(dashboardPage, /modelRouting/);
  assert.match(dashboardPage, /href="\/providers\?status=active"/);
  assert.match(dashboardPage, /href="\/providers\?status=blocked"/);
});

test('dashboard shows gateway runtime scheduling limits', () => {
  for (const label of [
    'Gateway runtime limits',
    'Gateway concurrency',
    'Per account concurrency',
    'Per key concurrency',
    'Requests per minute',
    'Tokens per minute'
  ]) {
    assert.match(dashboardPage, new RegExp(label.replace(' ', '\\s+')), `dashboard should include ${label}`);
  }

  assert.match(dashboardPage, /gatewaySettings/);
  assert.match(dashboardPage, /gatewayLimitLabel/);
  assert.match(adminState, /await loadGatewaySettings\(\)/);
});

test('dashboard shows 24h gateway usage snapshot', () => {
  for (const label of ['24h usage', 'Top models', 'Top provider accounts', 'Top usage sources', 'Top routing pools', 'Top routing pool chains', 'Top client keys', 'Top sessions', 'Requests', 'Tokens', 'Estimated cost']) {
    assert.match(dashboardPage, new RegExp(label.replace(' ', '\\s+')), `dashboard should include ${label}`);
  }

  assert.match(dashboardPage, /usage\.summaries\['24h:/);
  assert.match(dashboardPage, /section.data.rows/);
  assert.match(dashboardPage, /dashboardUsageHref/);
  assert.match(dashboardPage, /function dashboardUsageSinceParam/);
  assert.match(dashboardPage, /function dashboardUsageHrefWithSince/);
  assert.match(dashboardPage, /params\.set\('since', dashboardUsageSinceParam\(\)\)/);
  assert.match(dashboardPage, /providerAccountUsageId/);
  assert.match(dashboardPage, /params\.set\('model', id\)/);
  assert.match(dashboardPage, /params\.set\('providerAccountId', accountId\)/);
  assert.match(dashboardPage, /params\.set\('usageSource', id\)/);
  assert.match(dashboardPage, /params\.set\('routingPoolId', id\)/);
  assert.match(dashboardPage, /params\.set\('routingPoolChain', id\)/);
  assert.match(dashboardPage, /params\.set\('clientKeyId', id\)/);
  assert.match(dashboardPage, /params\.set\('sessionId', id\)/);
  assert.match(dashboardPage, /href="\/request-logs\?gatewayFallbacks=1"/);
  assert.match(adminState, /await loadUsageSummary\('24h', 'provider_account'\)/);
  assert.match(adminState, /await loadUsageSummary\('24h', 'usage_source'\)/);
  assert.match(adminState, /await loadUsageSummary\('24h', 'routing_pool'\)/);
  assert.match(adminState, /await loadUsageSummary\('24h', 'routing_pool_chain'\)/);
  assert.match(adminState, /await loadUsageSummary\('24h', 'client_key'\)/);
  assert.match(adminState, /await loadUsageSummary\('24h', 'session'\)/);
  assert.match(dashboardPage, /formatTokens/);
  assert.match(dashboardPage, /formatCostMicrousd/);
});

test('dashboard shows ops monitoring snapshot', () => {
  for (const label of ['Operations snapshot', 'Error rate', 'Client errors', 'Server errors', 'Rate limited', 'Upstream errors', 'Cost attribution', 'Top cost models', 'Top cost provider accounts', 'Top cost API keys', 'Open ops monitor']) {
    assert.match(dashboardPage, new RegExp(label.replace(' ', '\\s+')), `dashboard should include ${label}`);
  }

  assert.match(dashboardPage, /opsMonitor/);
  assert.match(dashboardPage, /loadOpsDashboard/);
  assert.match(dashboardPage, /opsMonitor\.stats/);
  assert.match(dashboardPage, /opsMonitor\.costBreakdown/);
  assert.match(dashboardPage, /function dashboardOpsErrorHref/);
  assert.match(dashboardPage, /function dashboardCostModelHref/);
  assert.match(dashboardPage, /function dashboardCostProviderAccountHref/);
  assert.match(dashboardPage, /function dashboardCostClientKeyHref/);
  assert.match(dashboardPage, /params\.set\('since', dashboardUsageSinceParam\(\)\)/);
  assert.match(dashboardPage, /params\.set\('error', key\)/);
  assert.match(dashboardPage, /params\.set\('model', key\)/);
  assert.match(dashboardPage, /params\.set\('providerAccountId', key\)/);
  assert.match(dashboardPage, /params\.set\('clientKeyId', key\)/);
  assert.match(dashboardPage, /href=\{dashboardOpsErrorHref\(bucket\)\}/);
  assert.match(dashboardPage, /href=\{dashboardCostModelHref\(bucket\)\}/);
  assert.match(dashboardPage, /href=\{dashboardCostProviderAccountHref\(bucket\)\}/);
  assert.match(dashboardPage, /href=\{dashboardCostClientKeyHref\(bucket\)\}/);
  assert.match(dashboardPage, /formatCostMicrousd\(bucket\.estimatedCostMicrousd/);
  assert.match(dashboardPage, /href="\/ops"/);
  assert.match(adminState, /await loadOpsDashboard\(86400\)/);
});

test('ops monitor links error buckets to filtered request logs', () => {
  for (const label of ['Operations monitor', 'Top errors', 'Upstream status codes', 'Rate-limited models', 'Error accounts', 'Cost attribution', 'Top cost models', 'Top cost provider accounts', 'Top cost API keys', 'Account health', 'Schedulable accounts', 'Scheduling blockers', 'Account tests', 'Recent account tests', 'Open providers']) {
    assert.match(opsPage, new RegExp(label.replace(' ', '\\s+')), `ops page should include ${label}`);
  }

  assert.match(adminState, /accountHealth:\s*null/);
  assert.match(adminState, /function loadOpsAccountHealth/);
  assert.match(adminState, /\/api\/admin\/ops\/account-health/);
  assert.match(adminState, /loadOpsAccountHealth\(since\)/);
  assert.match(adminState, /function loadOpsAccountTests/);
  assert.match(adminState, /\/api\/admin\/ops\/account-tests/);
  assert.match(adminState, /loadOpsAccountTests\(since\)/);
  assert.match(adminState, /costBreakdown:\s*null/);
  assert.match(adminState, /function loadOpsCostBreakdown/);
  assert.match(adminState, /\/api\/admin\/ops\/cost-breakdown/);
  assert.match(adminState, /loadOpsCostBreakdown\(since\)/);
  assert.match(opsPage, /opsMonitor\.accountHealth/);
  assert.match(opsPage, /opsMonitor\.accountTests\.tests/);
  assert.match(opsPage, /opsMonitor\.costBreakdown/);
  assert.match(opsPage, /\/providers\?providerAccountId=/);
  assert.match(opsPage, /href="\/providers\?status=disabled"/);
  assert.match(opsPage, /href="\/providers\?status=rate_limited"/);
  assert.match(opsPage, /href="\/providers\?status=circuit_open"/);
  assert.match(opsPage, /href="\/providers\?status=expired"/);
  assert.match(opsPage, /function opsErrorHref/);
  assert.match(opsPage, /function opsStatusCodeHref/);
  assert.match(opsPage, /function opsRateLimitedModelHref/);
  assert.match(opsPage, /function opsErrorAccountHref/);
  assert.match(opsPage, /function opsCostModelHref/);
  assert.match(opsPage, /function opsCostProviderAccountHref/);
  assert.match(opsPage, /function opsCostClientKeyHref/);
  assert.match(opsPage, /function opsSinceParam/);
  assert.match(opsPage, /function requestLogHrefWithSince/);
  assert.match(opsPage, /params\.set\('since', opsSinceParam\(\)\)/);
  assert.match(opsPage, /params\.set\('error', key\)/);
  assert.match(opsPage, /params\.set\('statusCode', key\)/);
  assert.match(opsPage, /params\.set\('model', key\)/);
  assert.match(opsPage, /params\.set\('providerAccountId', key\)/);
  assert.match(opsPage, /params\.set\('clientKeyId', key\)/);
  assert.match(opsPage, /href=\{opsErrorHref\(bucket\)\}/);
  assert.match(opsPage, /href=\{opsStatusCodeHref\(bucket\)\}/);
  assert.match(opsPage, /href=\{opsRateLimitedModelHref\(bucket\)\}/);
  assert.match(opsPage, /href=\{opsErrorAccountHref\(bucket\)\}/);
  assert.match(opsPage, /href=\{opsCostModelHref\(bucket\)\}/);
  assert.match(opsPage, /href=\{opsCostProviderAccountHref\(bucket\)\}/);
  assert.match(opsPage, /href=\{opsCostClientKeyHref\(bucket\)\}/);
  assert.match(opsPage, /formatCostMicrousd\(bucket\.estimatedCostMicrousd/);
  assert.match(opsPage, /View matching request logs/);
});

test('dashboard recent activity links to filtered request logs', () => {
  assert.match(dashboardPage, /function dashboardLogHref/);
  assert.match(dashboardPage, /requestId=\$\{encodeURIComponent/);
  assert.match(dashboardPage, /params\.set\('clientKeyId', String\(log\.clientKeyId\)\)/);
  assert.match(dashboardPage, /params\.set\('providerAccountId', String\(log\.providerAccountId\)\)/);
  assert.match(dashboardPage, /params\.set\('routingPoolId', String\(log\.routingPoolId\)\)/);
  assert.match(dashboardPage, /params\.set\('model', log\.model\)/);
  assert.match(dashboardPage, /params\.set\('sessionId', log\.sessionId\)/);
  assert.match(dashboardPage, /href=\{dashboardLogHref\(log\)\}/);
});

test('admin state derives schedulable gateway capacity', () => {
  assert.match(adminState, /export function getSchedulableProviderAccounts/);
  assert.match(adminState, /export function getUnschedulableProviderAccountSummary/);
  assert.match(adminState, /reasonLabel/);
  assert.match(adminState, /rateLimitedUntil/);
  assert.match(adminState, /circuitOpenUntil/);
  assert.match(adminState, /export function getRoutableModelCount/);
  assert.match(adminState, /enabledCount/);
});

test('shared AuthGate component owns loading and sign-in shell', () => {
  assert.equal(existsSync('src/lib/AuthGate.svelte'), true, 'AuthGate.svelte should exist');
  const authGateSrc = readFileSync('src/lib/AuthGate.svelte', 'utf8');
  assert.match(authGateSrc, /import.*login.*loginForm.*session.*from.*admin-state/);
  assert.match(authGateSrc, /session\.loading/);
  assert.match(authGateSrc, /!session\.authenticated/);
  assert.match(authGateSrc, /Loading/);
  assert.match(authGateSrc, /N2API/);
  assert.match(authGateSrc, /Sign in to manage this personal gateway/);
  assert.match(authGateSrc, /session\.error/);
  assert.match(authGateSrc, /loginForm\.username/);
  assert.match(authGateSrc, /loginForm\.password/);
  assert.match(authGateSrc, /loginForm\.error/);
  assert.match(authGateSrc, /loginForm\.submitting/);
  assert.match(authGateSrc, /onsubmit=\{login\}/);
  assert.match(authGateSrc, /Signing in/);
  assert.match(authGateSrc, /\{@render children\(\)\}/);
});

test('route pages use shared AuthGate instead of duplicate login forms', () => {
  const pages = [
    { name: 'dashboard', path: 'src/routes/+page.svelte' },
    { name: 'gateway', path: 'src/routes/gateway/+page.svelte' },
    { name: 'providers', path: 'src/routes/providers/+page.svelte' },
    { name: 'routing-pools', path: 'src/routes/routing-pools/+page.svelte' },
    { name: 'models', path: 'src/routes/models/+page.svelte' },
    { name: 'api-keys', path: 'src/routes/api-keys/+page.svelte' },
    { name: 'request-logs', path: 'src/routes/request-logs/+page.svelte' },
    { name: 'ops', path: 'src/routes/ops/+page.svelte' },
    { name: 'fingerprints', path: 'src/routes/fingerprints/+page.svelte' },
    { name: 'error-passthrough', path: 'src/routes/error-passthrough/+page.svelte' },
  ];
  for (const page of pages) {
    const src = readFileSync(page.path, 'utf8');
    assert.match(src, /AuthGate/, `${page.name} page should import AuthGate`);
    assert.doesNotMatch(src, /Admin access/, `${page.name} page should not repeat 'Admin access' heading`);
    assert.doesNotMatch(src, /Admin sign in/, `${page.name} page should not repeat 'Admin sign in' heading`);
  }
});

test('api keys page supports table filters and row selection', () => {
  for (const label of [
    'Routing pool filter',
    'Model policy filter',
    'Issue filter',
    'Global pool',
    'All model policies',
    'All routable models',
    'Selected models',
    'All issue states',
    'Only blocked or budget exceeded',
    'Select',
    'Edit selected',
    'Enable',
    'Disable',
    'Delete',
    'Clear'
  ]) {
    assert.match(apiKeysPage, new RegExp(label.replaceAll(' ', '\\s+')), `api keys page should include ${label}`);
  }

  assert.match(apiKeysPage, /selectedAPIKeyIds/);
  assert.match(apiKeysPage, /selectedAPIKeyCount/);
  assert.match(apiKeysPage, /selectedEditableAPIKeys/);
  assert.match(apiKeysPage, /allFilteredAPIKeysSelected/);
  assert.match(apiKeysPage, /toggleAPIKeySelection/);
  assert.match(apiKeysPage, /toggleFilteredAPIKeySelection/);
  assert.match(apiKeysPage, /clearAPIKeySelection/);
  assert.match(apiKeysPage, /bulkSetSelectedAPIKeysDisabled/);
  assert.match(apiKeysPage, /bulkRevokeSelectedAPIKeys/);
  assert.match(apiKeysPage, /openBulkEditModal/);
  assert.match(apiKeysPage, /bind:value=\{keyRoutingPoolFilter\}/);
  assert.match(apiKeysPage, /bind:value=\{keyModelPolicyFilter\}/);
  assert.match(apiKeysPage, /bind:value=\{keyIssueFilter\}/);
  assert.match(apiKeysPage, /keyRoutingPoolFilter === 'global'/);
  assert.match(apiKeysPage, /keyModelPolicyFilter === 'all_routable'/);
  assert.doesNotMatch(apiKeysPage, /No models/);
});

test('api keys page has a bulk edit modal with opt-in sections', () => {
  for (const label of [
    'Bulk edit API keys',
    'Selected keys',
    'Apply status',
    'Apply model access',
    'Apply routing pool',
    'Apply limits',
    'Apply budgets',
    'Leave unchanged',
    'Apply changes'
  ]) {
    assert.match(apiKeysPage, new RegExp(label.replaceAll(' ', '\\s+')), `bulk edit modal should include ${label}`);
  }

  assert.match(apiKeysPage, /bulkEditModalOpen/);
  assert.match(apiKeysPage, /bulkEditForm\.applyStatus/);
  assert.match(apiKeysPage, /bulkEditForm\.applyModelPolicy/);
  assert.match(apiKeysPage, /bulkEditForm\.applyRoutingPool/);
  assert.match(apiKeysPage, /bulkEditForm\.applyLimits/);
  assert.match(apiKeysPage, /bulkEditForm\.applyBudgets/);
  assert.match(apiKeysPage, /submitBulkEdit/);
  assert.match(apiKeysPage, /bulkUpdateSelectedAPIKeys/);
});

test('api key batch helpers reuse existing per-key endpoints', () => {
  assert.match(adminState, /selectedAPIKeyIds/);
  assert.match(adminState, /export function toggleAPIKeySelection/);
  assert.match(adminState, /export function clearAPIKeySelection/);
  assert.match(adminState, /export async function bulkSetSelectedAPIKeysDisabled/);
  assert.match(adminState, /export async function bulkRevokeSelectedAPIKeys/);
  assert.match(adminState, /export async function bulkUpdateSelectedAPIKeys/);
  assert.match(adminState, /await setAPIKeyDisabled\(id,\s*disabled\)/);
  assert.match(adminState, /await revokeKey\(id\)/);
  assert.match(adminState, /await updateAPIKeyModelPolicy/);
  assert.match(adminState, /await updateAPIKeyLimits/);
  assert.match(adminState, /await updateAPIKeyBudgets/);
  assert.match(adminState, /await updateAPIKeyRoutingPool/);
  assert.match(adminState, /saving: false,[\s\S]*?items: \[\]/);
  assert.match(adminState, /delete selectedAPIKeyIds\[String\(id\)\]/);
  assert.match(adminState, /Select at least one active or disabled API key/);
  assert.doesNotMatch(adminState, /\/api\/admin\/keys\/bulk/);
});
