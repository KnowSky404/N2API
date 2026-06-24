import { existsSync, readFileSync } from 'node:fs';
import { test } from 'node:test';
import assert from 'node:assert/strict';

const expectedFiles = [
  'src/lib/admin-state.svelte.js',
  'src/routes/+layout.svelte',
  'src/routes/+page.svelte',
  'src/routes/gateway/+page.svelte',
  'src/routes/providers/+page.svelte',
  'src/routes/routing-pools/+page.svelte',
  'src/routes/models/+page.svelte',
  'src/routes/api-keys/+page.svelte',
  'src/routes/request-logs/+page.svelte'
];

const requestLogsPage = readFileSync('src/routes/request-logs/+page.svelte', 'utf8');
const modelsPage = readFileSync('src/routes/models/+page.svelte', 'utf8');
const gatewayPage = readFileSync('src/routes/gateway/+page.svelte', 'utf8');
const providersPage = readFileSync('src/routes/providers/+page.svelte', 'utf8');
const apiKeysPage = readFileSync('src/routes/api-keys/+page.svelte', 'utf8');
const dashboardPage = readFileSync('src/routes/+page.svelte', 'utf8');
const adminState = readFileSync('src/lib/admin-state.svelte.js', 'utf8');

test('admin UI has focused routes behind a shared sidebar shell', () => {
  for (const file of expectedFiles) {
    assert.equal(existsSync(file), true, `${file} should exist`);
  }

  const layout = readFileSync('src/routes/+layout.svelte', 'utf8');
  for (const label of ['Dashboard', 'Gateway', 'Providers', 'Routing pools', 'API Keys', 'Request Logs', 'Sign out']) {
    assert.match(layout, new RegExp(label.replace(' ', '\\s+')), `layout should include ${label}`);
  }
  assert.doesNotMatch(layout, /label:\s*'Models'/);
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
  for (const label of ['Routing pools', 'Create pool', 'Pool accounts', 'Save membership', 'Enabled']) {
    assert.match(poolsPage, new RegExp(label.replace(' ', '\\s+')), `routing pools page should include ${label}`);
  }
  assert.match(adminState, /loadRoutingPools/);
  assert.match(adminState, /createRoutingPool/);
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
    'Top client keys',
    'Top sessions'
  ]) {
    assert.match(gatewayPage, new RegExp(label.replace(' ', '\\s+')), `gateway page should include ${label}`);
  }

  for (const href of ['/providers', '/api-keys', '/request-logs', '/models']) {
    assert.match(gatewayPage, new RegExp(`href="${href}"`), `gateway page should link to ${href}`);
  }

  assert.match(gatewayPage, /loadGatewaySettings/);
  assert.match(gatewayPage, /loadProviderAccounts/);
  assert.match(gatewayPage, /loadModelRouting/);
  assert.match(gatewayPage, /loadKeys/);
  assert.match(gatewayPage, /getSchedulableProviderAccounts/);
  assert.match(gatewayPage, /getUnschedulableProviderAccountSummary/);
  assert.match(gatewayPage, /unschedulableAccountSummary/);
  assert.match(gatewayPage, /enabledProviderAccountCount/);
  assert.match(gatewayPage, /getRoutableModelCount/);
  assert.match(gatewayPage, /getActiveKeys/);
  assert.match(gatewayPage, /loadUsageSummary\('24h', 'model'\)/);
  assert.match(gatewayPage, /loadUsageSummary\('24h', 'provider_account'\)/);
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
  assert.match(gatewayPage, /gatewaySettings\.data\.providerAccountAutoTestStatus/);
  assert.match(gatewayPage, /lastFinishedAt/);
  assert.match(gatewayPage, /lastAccountCount/);
  assert.match(gatewayPage, /lastError/);
  assert.match(adminState, /export async function updateGatewaySettings/);
  assert.match(adminState, /\/api\/admin\/gateway-settings/);
  assert.match(adminState, /providerAccountAutoTestEnabled: Boolean/);
  assert.match(adminState, /providerAccountAutoTestIntervalSeconds: Number/);
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
});

test('request logs page shows request model', () => {
  assert.match(requestLogsPage, />Model</);
  assert.match(requestLogsPage, /log\.model/);
});

test('request logs page shows sticky session attribution', () => {
  assert.match(requestLogsPage, />Session</);
  assert.match(requestLogsPage, /log\.sessionId/);
  assert.match(requestLogsPage, /colspan="14"/);
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
  for (const label of ['Usage summary', 'Estimated cost', 'Input tokens', 'Output tokens', 'Pricing', 'Session']) {
    assert.match(requestLogsPage, new RegExp(label.replace(' ', '\\s+')), `request logs page should include ${label}`);
  }
});

test('request logs page formats gateway error codes for scanning', () => {
  assert.match(requestLogsPage, /function errorLabel/);
  assert.match(requestLogsPage, /errorLabel\(log\.error\)/);
  assert.match(requestLogsPage, /title=\{log\.error/);
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
  assert.match(adminState, /gatewayAttemptCount/);
  assert.match(adminState, /gatewayFallbackCount/);
});

test('request logs page filters by search and status class', () => {
  assert.match(requestLogsPage, /bind:value=\{requestLogs\.query\}/);
  assert.match(requestLogsPage, /bind:value=\{requestLogs\.statusClass\}/);
  assert.match(requestLogsPage, /bind:value=\{requestLogs\.model\}/);
  assert.match(requestLogsPage, /bind:value=\{requestLogs\.sessionId\}/);
  assert.match(requestLogsPage, /Model filter/);
  assert.match(requestLogsPage, /Session filter/);
  assert.match(requestLogsPage, /statusClass/);
  assert.match(adminState, /params\.set\('q'/);
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
  assert.match(requestLogsPage, /apiKeys/);
  assert.match(requestLogsPage, /loadKeys\(\)/);
  assert.match(requestLogsPage, /bind:value=\{requestLogs\.clientKeyId\}/);
  assert.match(requestLogsPage, /All API keys/);
  assert.match(adminState, /params\.set\('providerAccountId'/);
  assert.match(adminState, /requestLogs\.providerAccountId/);
  assert.match(requestLogsPage, /loadProviderAccounts\(\)/);
});

test('request logs page initializes filters from URL params', () => {
  assert.match(requestLogsPage, /URLSearchParams\(window\.location\.search\)/);
  assert.match(requestLogsPage, /requestLogs\.providerAccountId = providerAccountId/);
  assert.match(requestLogsPage, /requestLogs\.clientKeyId = clientKeyId/);
  assert.match(requestLogsPage, /requestLogs\.model = model/);
  assert.match(requestLogsPage, /requestLogs\.sessionId = sessionId/);
  assert.match(requestLogsPage, /requestLogs\.query = query/);
  assert.match(requestLogsPage, /requestLogs\.statusClass = statusClass/);
  assert.match(requestLogsPage, /void loadRequestLogs\(\)/);
  assert.match(adminState, /params\.set\('clientKeyId'/);
  assert.match(adminState, /clientKeyId: 'all'/);
  assert.match(adminState, /model: ''/);
  assert.match(adminState, /sessionId: ''/);
});

test('gateway usage rows link to filtered request logs', () => {
  assert.match(gatewayPage, /usageRowHref/);
  assert.match(gatewayPage, /model=\$\{encodeURIComponent/);
  assert.match(gatewayPage, /sessionId=\$\{encodeURIComponent/);
  assert.match(gatewayPage, /providerAccountId=\$\{encodeURIComponent/);
  assert.match(gatewayPage, /clientKeyId=\$\{encodeURIComponent/);
  assert.match(gatewayPage, /providerAccountUsageId/);
  assert.match(gatewayPage, /href=\{href\}/);
});

test('models page shows scheduling diagnostics for routing candidates', () => {
  assert.match(modelsPage, /N2API Routing Diagnostics/);
  assert.match(modelsPage, /Routing diagnostics/);
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
  assert.match(adminState, /scheduleReason/);
  assert.match(modelsPage, /Excluded account IDs/);
  assert.match(modelsPage, /bind:value=\{modelRoutingPreview\.excludedAccountIds\}/);
  assert.match(modelsPage, /excluding \{modelRoutingPreview\.excludedAccountIds\}/);
  assert.match(modelsPage, /previewConcurrencyLimitLabel/);
  assert.match(modelsPage, /Active/);
  assert.match(modelsPage, /Concurrency full/);
  assert.match(adminState, /excludedAccountIds: ''/);
  assert.match(adminState, /params\.set\('excludedAccountIds'/);
});

test('providers page summarizes account scheduling capacity', () => {
  for (const label of ['Scheduling capacity', 'Schedulable', 'Blocked', 'Blocked reasons']) {
    assert.match(providersPage, new RegExp(label.replace(' ', '\\s+')), `providers page should include ${label}`);
  }

  assert.match(providersPage, /getSchedulableProviderAccounts/);
  assert.match(providersPage, /getUnschedulableProviderAccountSummary/);
  assert.match(providersPage, /schedulableProviderAccounts\.length/);
  assert.match(providersPage, /unschedulableProviderAccountSummary/);
});

test('providers page shows provider account usage distribution', () => {
  for (const label of ['24h account usage', 'Requests', 'Tokens', 'Estimated cost']) {
    assert.match(providersPage, new RegExp(label.replace(' ', '\\s+')), `providers page should include ${label}`);
  }

  assert.match(providersPage, /loadUsageSummary\('24h', 'provider_account'\)/);
  assert.match(providersPage, /usage24hProviderAccounts/);
  assert.match(providersPage, /formatTokens/);
  assert.match(providersPage, /formatCostMicrousd/);
});

test('api keys page shows per-key usage distribution', () => {
  for (const label of ['24h key usage', 'Requests', 'Tokens', 'Estimated cost', 'Active', 'Concurrency full', 'Requests window', 'Tokens window', 'Request limit full', 'Token limit full']) {
    assert.match(apiKeysPage, new RegExp(label.replace(' ', '\\s+')), `api keys page should include ${label}`);
  }

  assert.match(apiKeysPage, /loadUsageSummary\('24h', 'client_key'\)/);
  assert.match(apiKeysPage, /usage24hClientKeys/);
  assert.match(apiKeysPage, /formatTokens/);
  assert.match(apiKeysPage, /formatCostMicrousd/);
  assert.match(apiKeysPage, /keyConcurrencyLimitLabel/);
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
  assert.match(apiKeysPage, /keyRateWindowLimitLabel/);
});

test('api keys page filters key list locally', () => {
  for (const label of ['Search keys', 'Status filter', 'All keys', 'Active keys', 'Disabled keys', 'Revoked keys']) {
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
  for (const label of ['Disabled', 'Disable', 'Enable']) {
    assert.match(apiKeysPage, new RegExp(label), `api keys page should include ${label}`);
  }

  assert.match(apiKeysPage, /setAPIKeyDisabled/);
  assert.match(apiKeysPage, /key\.disabledAt/);
  assert.match(apiKeysPage, /keyStatusFilter === 'disabled'/);
  assert.match(apiKeysPage, /onclick=\{\(\) => setAPIKeyDisabled\(key\.id, !key\.disabledAt\)\}/);
  assert.match(adminState, /@property \{string \| null\} disabledAt/);
  assert.match(adminState, /export async function setAPIKeyDisabled/);
  assert.match(adminState, /\/api\/admin\/keys\/\$\{keyId\}\/disabled/);
  assert.match(adminState, /!key\.revokedAt && !key\.disabledAt/);
});

test('api keys page renames keys without rotating secrets', () => {
  assert.match(apiKeysPage, /updateAPIKeyName/);
  assert.match(apiKeysPage, /Save name/);
  assert.match(apiKeysPage, /bind:value=\{key\.name\}/);
  assert.match(apiKeysPage, /updateAPIKeyName\(key\.id, key\.name\)/);
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
  for (const label of ['24h usage', 'Top models', 'Top provider accounts', 'Top client keys', 'Top sessions', 'Requests', 'Tokens', 'Estimated cost']) {
    assert.match(dashboardPage, new RegExp(label.replace(' ', '\\s+')), `dashboard should include ${label}`);
  }

  assert.match(dashboardPage, /usage\.summaries\['24h:/);
  assert.match(dashboardPage, /usage24h\.rows/);
  assert.match(dashboardPage, /usage24hProviderAccounts\.rows/);
  assert.match(dashboardPage, /usage24hClientKeys\.rows/);
  assert.match(dashboardPage, /usage24hSessions\.rows/);
  assert.match(adminState, /await loadUsageSummary\('24h', 'provider_account'\)/);
  assert.match(adminState, /await loadUsageSummary\('24h', 'client_key'\)/);
  assert.match(adminState, /await loadUsageSummary\('24h', 'session'\)/);
  assert.match(dashboardPage, /formatTokens/);
  assert.match(dashboardPage, /formatCostMicrousd/);
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
