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
const modelsPage = readFileSync('src/routes/models/+page.svelte', 'utf8');
const dashboardPage = readFileSync('src/routes/+page.svelte', 'utf8');
const adminState = readFileSync('src/lib/admin-state.svelte.js', 'utf8');

test('admin UI has focused routes behind a shared sidebar shell', () => {
  for (const file of expectedFiles) {
    assert.equal(existsSync(file), true, `${file} should exist`);
  }

  const layout = readFileSync('src/routes/+layout.svelte', 'utf8');
  for (const label of ['Dashboard', 'Providers', 'API Keys', 'Request Logs', 'Sign out']) {
    assert.match(layout, new RegExp(label.replace(' ', '\\s+')), `layout should include ${label}`);
  }
  assert.doesNotMatch(layout, /label:\s*'Models'/);
});

test('primary navigation moves model policy ownership to API keys', () => {
  const layout = readFileSync('src/routes/+layout.svelte', 'utf8');

  assert.doesNotMatch(layout, /href:\s*'\/models'/);
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

test('request logs page shows sticky session attribution', () => {
  assert.match(requestLogsPage, />Session</);
  assert.match(requestLogsPage, /log\.sessionId/);
  assert.match(requestLogsPage, /colspan="13"/);
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

test('models page shows scheduling diagnostics for routing candidates', () => {
  assert.match(modelsPage, /account\.schedulable/);
  assert.match(modelsPage, /account\.unschedulableReason/);
  assert.match(modelsPage, /No schedulable account/);
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
  for (const label of ['24h usage', 'Requests', 'Tokens', 'Estimated cost']) {
    assert.match(dashboardPage, new RegExp(label.replace(' ', '\\s+')), `dashboard should include ${label}`);
  }

  assert.match(dashboardPage, /usage\.summaries\['24h:/);
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
