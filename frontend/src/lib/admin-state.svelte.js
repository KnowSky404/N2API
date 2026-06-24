import { copyText } from '$lib/clipboard.js';

/**
 * @typedef {object} APIKey
 * @property {number} id
 * @property {string} name
 * @property {string} modelPolicy
 * @property {string[]} allowedModels
 * @property {string | undefined} allowedModelsText
 * @property {string} prefix
 * @property {string} createdAt
 * @property {string | null} lastUsedAt
 * @property {string | null} revokedAt
 * @property {number} requestsPerMinute
 * @property {number} tokensPerMinute
 */

/**
 * @typedef {object} ProviderStatus
 * @property {string} provider
 * @property {boolean} configured
 * @property {boolean} connected
 * @property {string} displayName
 * @property {string | null} accessTokenExpiresAt
 * @property {string | null} lastRefreshAt
 */

/**
 * @typedef {object} ProviderAccount
 * @property {number} id
 * @property {string} provider
 * @property {string} accountType
 * @property {string} subject
 * @property {string} name
 * @property {string} displayName
 * @property {string} baseUrl
 * @property {boolean} enabled
 * @property {number} priority
 * @property {number} loadFactor
 * @property {number} maxConcurrentRequests
 * @property {string} status
 * @property {string} statusReason
 * @property {string | null} circuitOpenUntil
 * @property {string | null} rateLimitedUntil
 * @property {string | null} accessTokenExpiresAt
 * @property {string | null} lastRefreshAt
 * @property {string | null} lastUsedAt
 * @property {string} lastError
 * @property {string | null} lastErrorAt
 * @property {string | null} lastTestAt
 * @property {string} lastTestStatus
 * @property {string} lastTestError
 */

/**
 * @typedef {object} RequestLog
 * @property {number} id
 * @property {string} requestId
 * @property {string} clientKey
 * @property {string} provider
 * @property {string} model
 * @property {number} providerAccountId
 * @property {string} providerAccountType
 * @property {string} providerAccountName
 * @property {string} sessionId
 * @property {string} route
 * @property {string} method
 * @property {number} statusCode
 * @property {number} latencyMs
 * @property {string} error
 * @property {number} inputTokens
 * @property {number} outputTokens
 * @property {number} totalTokens
 * @property {number} cachedInputTokens
 * @property {number} reasoningTokens
 * @property {string} usageSource
 * @property {number} estimatedCostMicrousd
 * @property {boolean} pricingMatched
 * @property {string} createdAt
 */

/**
 * @typedef {object} UsageSummaryRow
 * @property {string} id
 * @property {string} label
 * @property {number} requests
 * @property {number} inputTokens
 * @property {number} outputTokens
 * @property {number} totalTokens
 * @property {number} estimatedCostMicrousd
 */

/**
 * @typedef {object} UsageSummary
 * @property {string} range
 * @property {string} groupBy
 * @property {number} totalRequests
 * @property {number} totalInputTokens
 * @property {number} totalOutputTokens
 * @property {number} totalTokens
 * @property {number} estimatedCostMicrousd
 * @property {UsageSummaryRow[]} rows
 */

/**
 * @typedef {object} UsagePricingRow
 * @property {string} model
 * @property {number} inputMicrousdPerMillion
 * @property {number} cachedInputMicrousdPerMillion
 * @property {number} outputMicrousdPerMillion
 */

/**
 * @typedef {object} ModelSettingsData
 * @property {string} defaultModel
 * @property {string[]} allowedModels
 */

/**
 * @typedef {object} AccountModel
 * @property {number} id
 * @property {number} accountId
 * @property {string} provider
 * @property {string} model
 * @property {boolean} enabled
 * @property {string} source
 * @property {string | null} lastSeenAt
 * @property {string} lastError
 */

/**
 * @typedef {object} AccountModelsState
 * @property {boolean} loading
 * @property {boolean} saving
 * @property {string} error
 * @property {boolean} saved
 * @property {string} text
 * @property {AccountModel[]} items
 * @property {number} requestSeq
 */

/**
 * @typedef {object} ProviderAccountTestResult
 * @property {number} id
 * @property {number} accountId
 * @property {string} provider
 * @property {string} status
 * @property {string} message
 * @property {string} checkedAt
 * @property {string} createdAt
 */

/**
 * @typedef {object} AccountTestResultsState
 * @property {boolean} expanded
 * @property {boolean} loading
 * @property {string} error
 * @property {ProviderAccountTestResult[]} items
 * @property {number} requestSeq
 */

/**
 * @typedef {object} ModelRoutingAccount
 * @property {number} id
 * @property {string} displayName
 * @property {string} accountType
 * @property {boolean} enabled
 * @property {number} priority
 * @property {number} loadFactor
 * @property {string} status
 * @property {string} statusReason
 * @property {string} lastError
 * @property {string | null} lastErrorAt
 * @property {string | null} lastUsedAt
 * @property {string | null} lastTestAt
 * @property {string} lastTestStatus
 * @property {string} lastTestError
 * @property {number} scheduleRank
 * @property {boolean} schedulable
 * @property {string} unschedulableReason
 */

/**
 * @typedef {object} ModelRoutingModel
 * @property {string} model
 * @property {boolean} allowed
 * @property {number} configuredCount
 * @property {number} enabledCount
 * @property {ModelRoutingAccount[]} accounts
 */

/**
 * @typedef {object} GatewaySettingsData
 * @property {number} maxConcurrentGatewayRequests
 * @property {number} maxConcurrentRequestsPerAccount
 * @property {number} maxConcurrentRequestsPerKey
 * @property {number} requestsPerMinutePerKey
 * @property {number} tokensPerMinutePerKey
 * @property {boolean} providerAccountAutoTestEnabled
 * @property {number} providerAccountAutoTestIntervalSeconds
 * @property {ProviderAccountAutoTestStatus} providerAccountAutoTestStatus
 */

/**
 * @typedef {object} ProviderAccountAutoTestStatus
 * @property {boolean} running
 * @property {string | null} lastStartedAt
 * @property {string | null} lastFinishedAt
 * @property {number} lastAccountCount
 * @property {string} lastError
 */

/**
 * @typedef {object} SelectionPreviewCandidate
 * @property {number} id
 * @property {string} displayName
 * @property {string} accountType
 * @property {number} priority
 * @property {number} loadFactor
 * @property {string} status
 * @property {string | null} lastUsedAt
 * @property {string | null} lastTestAt
 * @property {string} lastTestStatus
 * @property {string} lastTestError
 * @property {number} scheduleRank
 * @property {boolean} selected
 * @property {boolean} stickyBound
 * @property {boolean} schedulable
 * @property {string} unschedulableReason
 */

/**
 * @typedef {object} SelectionPreview
 * @property {string} model
 * @property {string} sessionId
 * @property {number} selectedAccountId
 * @property {number} stickyBoundAccountId
 * @property {SelectionPreviewCandidate[]} candidates
 */

export const health = $state({
  loading: true,
  error: '',
  status: 'checking',
  database: 'checking'
});

export const session = $state({ loading: true, authenticated: false, username: '', error: '' });
let sessionVersion = $state(0);
export const loginForm = $state({ username: '', password: '', submitting: false, error: '' });
/** @type {{ loading: boolean, connecting: boolean, error: string, data: ProviderStatus | null }} */
export const provider = $state({
  loading: false,
  connecting: false,
  error: '',
  data: null
});
/** @type {{ loading: boolean, saving: boolean, error: string, items: ProviderAccount[] }} */
export const providerAccounts = $state({ loading: false, saving: false, error: '', items: [] });
export const providerConnectForm = $state({ name: '', priority: 100, enabled: true });
export const providerAccountPauseForm = $state({ durationSeconds: 300 });
export const providerAccountBulkSchedulingForm = $state({ priority: '', loadFactor: '', maxConcurrentRequests: '' });
export const providerAccountBulkModelsForm = $state({ text: '' });
export const providerOAuth = $state({ authorizationUrl: '', callbackUrl: '', completing: false, copied: false });
export const apiUpstreamForm = $state({
  name: '',
  baseUrl: '',
  apiKey: '',
  priority: 100,
  loadFactor: 1,
  enabled: true,
  modelsText: '',
  submitting: false,
  error: ''
});
/** @type {{ loading: boolean, creating: boolean, error: string, items: APIKey[], newKeyName: string, oneTimeSecret: string }} */
export const apiKeys = $state({
  loading: false,
  creating: false,
  error: '',
  items: [],
  newKeyName: '',
  oneTimeSecret: ''
});
/** @type {{ loading: boolean, saving: boolean, error: string, saved: boolean, data: GatewaySettingsData | null }} */
export const gatewaySettings = $state({
  loading: false,
  saving: false,
  error: '',
  saved: false,
  data: null
});
/** @type {{ loading: boolean, error: string, items: RequestLog[] }} */
export const requestLogs = $state({
  loading: false,
  error: '',
  items: []
});
/** @type {{ loading: boolean, error: string, range: string, groupBy: string, summaries: Record<string, UsageSummary>, current: UsageSummary | null }} */
export const usage = $state({
  loading: false,
  error: '',
  range: '7d',
  groupBy: 'model',
  summaries: {},
  current: null
});
/** @type {{ loading: boolean, saving: boolean, error: string, saved: boolean, version: number, currency: string, unit: string, rows: UsagePricingRow[] }} */
export const usagePricing = $state({
  loading: false,
  saving: false,
  error: '',
  saved: false,
  version: 1,
  currency: 'USD',
  unit: '1M_tokens',
  rows: []
});
/** @type {{ loading: boolean, saving: boolean, error: string, saved: boolean, defaultModel: string, allowedModelsText: string }} */
export const modelSettings = $state({
  loading: false,
  saving: false,
  error: '',
  saved: false,
  defaultModel: '',
  allowedModelsText: ''
});
/** @type {Record<string, AccountModelsState>} */
export const accountModels = $state({});
/** @type {Record<string, AccountTestResultsState>} */
export const accountTestResults = $state({});
/** @type {Record<string, boolean>} */
export const selectedProviderAccountIds = $state({});
/** @type {{ loading: boolean, error: string, defaultModel: string, allowedModels: string[], models: ModelRoutingModel[], warnings: string[] }} */
export const modelRouting = $state({
  loading: false,
  error: '',
  defaultModel: '',
  allowedModels: [],
  models: [],
  warnings: []
});
/** @type {{ loading: boolean, error: string, model: string, sessionId: string, excludedAccountIds: string, result: SelectionPreview | null }} */
export const modelRoutingPreview = $state({
  loading: false,
  error: '',
  model: '',
  sessionId: '',
  excludedAccountIds: '',
  result: null
});

/** @param {string | null | undefined} value */
export function parseAccountModelsText(value) {
  const seen = new Set();
  return String(value ?? '')
    .split('\n')
    .map((model) => model.trim())
    .filter((model) => {
      if (!model || seen.has(model)) return false;
      seen.add(model);
      return true;
    })
    .map((model) => ({ model, enabled: true }));
}

/** @param {string | null | undefined} text */
export function parseModelLines(text) {
  const seen = new Set();
  return String(text ?? '')
    .split('\n')
    .map((line) => line.trim())
    .filter((model) => {
      if (!model || seen.has(model)) return false;
      seen.add(model);
      return true;
    });
}

/** @param {Array<string | null | undefined>} models */
export function modelListText(models) {
  return parseModelLines((models ?? []).join('\n')).join('\n');
}

/**
 * @param {Array<Partial<AccountModel> & { model: string, enabled?: boolean }>} models
 * @param {string | null | undefined} text
 */
export function mergeAccountModelChanges(models, text) {
  const seen = new Set();
  const merged = [];
  for (const item of models) {
    const model = String(item.model ?? '').trim();
    if (!model || seen.has(model)) continue;
    seen.add(model);
    merged.push({ model, enabled: item.enabled !== false });
  }
  for (const item of parseAccountModelsText(text)) {
    if (seen.has(item.model)) continue;
    seen.add(item.model);
    merged.push(item);
  }
  return merged;
}

/**
 * @param {AccountModel[]} models
 * @param {string} modelName
 * @param {boolean} enabled
 */
export function setAccountModelEnabled(models, modelName, enabled) {
  return models.map((item) => (item.model === modelName ? { ...item, enabled } : item));
}

/**
 * @param {AccountModel[]} models
 * @param {string} modelName
 */
export function removeAccountModel(models, modelName) {
  return models.filter((item) => item.model !== modelName);
}

/**
 * @param {{ requestSeq: number }} state
 * @param {number} requestSeq
 */
export function shouldApplyAccountModelsResponse(state, requestSeq) {
  return state.requestSeq === requestSeq;
}

/**
 * @param {{ requestSeq: number }} state
 * @param {number} requestSeq
 */
export function shouldApplyAccountTestResultsResponse(state, requestSeq) {
  return state.requestSeq === requestSeq;
}

/**
 * @param {Record<string, any>} states
 * @param {Array<number | string>} accountIds
 */
export function pruneAccountModelStates(states, accountIds) {
  const accountKeys = new Set(accountIds.map((id) => String(id)));
  for (const key of Object.keys(states)) {
    if (!accountKeys.has(key)) {
      delete states[key];
    }
  }
}

/**
 * @param {Record<string, any>} states
 * @param {Array<number | string>} accountIds
 */
export function pruneAccountTestResultStates(states, accountIds) {
  const accountKeys = new Set(accountIds.map((id) => String(id)));
  for (const key of Object.keys(states)) {
    if (!accountKeys.has(key)) {
      delete states[key];
    }
  }
}

/** @param {Array<number | string>} accountIds */
export function pruneSelectedProviderAccounts(accountIds) {
  const accountKeys = new Set(accountIds.map((id) => String(id)));
  for (const key of Object.keys(selectedProviderAccountIds)) {
    if (!accountKeys.has(key)) {
      delete selectedProviderAccountIds[key];
    }
  }
}

/**
 * @param {number | string} accountId
 * @param {boolean} selected
 */
export function toggleProviderAccountSelection(accountId, selected) {
  const key = String(accountId);
  if (!key || key === '0') return;
  if (selected) {
    selectedProviderAccountIds[key] = true;
    return;
  }
  delete selectedProviderAccountIds[key];
}

export function clearProviderAccountSelection() {
  for (const key of Object.keys(selectedProviderAccountIds)) {
    delete selectedProviderAccountIds[key];
  }
}

export function clearProviderAccountBulkSchedulingForm() {
  providerAccountBulkSchedulingForm.priority = '';
  providerAccountBulkSchedulingForm.loadFactor = '';
  providerAccountBulkSchedulingForm.maxConcurrentRequests = '';
}

export function clearProviderAccountBulkModelsForm() {
  providerAccountBulkModelsForm.text = '';
}

export function getProviderStateLabel() {
  return provider.data?.configured
    ? provider.data.connected
      ? 'connected'
      : 'not connected'
    : 'not configured';
}

export function getStatusItems() {
  return [
    { label: 'Backend', value: health.status },
    { label: 'Database', value: health.database },
    {
      label: 'Provider',
      value: session.authenticated ? (provider.data ? getProviderStateLabel() : 'checking') : 'login required'
    }
  ];
}

export function getActiveKeys() {
  return apiKeys.items.filter((key) => !key.revokedAt);
}

/** @param {string | null | undefined} value */
function isFutureTimestamp(value) {
  if (!value) return false;
  const timestamp = Date.parse(value);
  return Number.isFinite(timestamp) && timestamp > Date.now();
}

/** @param {Partial<ProviderAccount>} account */
function isProviderAccountSchedulable(account) {
  if (!account.enabled) return false;
  if (isFutureTimestamp(account.rateLimitedUntil) || isFutureTimestamp(account.circuitOpenUntil)) return false;
  return !['disabled', 'expired', 'rate_limited', 'circuit_open'].includes(account.status ?? '');
}

/** @param {Partial<ProviderAccount>} account */
function providerAccountUnschedulableReason(account) {
  if (!account.enabled) return 'disabled';
  if (isFutureTimestamp(account.rateLimitedUntil)) return 'rate_limited';
  if (isFutureTimestamp(account.circuitOpenUntil)) return 'circuit_open';
  if (['disabled', 'expired', 'rate_limited', 'circuit_open'].includes(account.status ?? '')) return account.status ?? '';
  return '';
}

/** @param {ProviderAccount[]} [accounts] */
export function getSchedulableProviderAccounts(accounts = providerAccounts.items) {
  return accounts.filter((account) => isProviderAccountSchedulable(account));
}

/** @param {ProviderAccount[]} [accounts] */
export function getUnschedulableProviderAccountSummary(accounts = providerAccounts.items) {
  const counts = new Map();
  for (const account of accounts) {
    const reason = providerAccountUnschedulableReason(account);
    if (!reason) continue;
    counts.set(reason, (counts.get(reason) ?? 0) + 1);
  }
  return Array.from(counts.entries()).map(([reason, count]) => ({
    reason,
    reasonLabel: statusLabel(reason),
    count
  }));
}

/** @param {Array<Partial<ModelRoutingModel>>} [models] */
export function getRoutableModelCount(models = modelRouting.models) {
  return models.filter((model) => Number(model.enabledCount ?? 0) > 0).length;
}

/**
 * @param {{
 *   providerAccounts?: ProviderAccount[],
 *   schedulableAccounts?: ProviderAccount[],
 *   routableModelCount?: number,
 *   activeKeys?: APIKey[]
 * }} [state]
 */
export function getGatewayReadinessIssues(state = {}) {
  const accounts = state.providerAccounts ?? providerAccounts.items;
  const schedulable = state.schedulableAccounts ?? getSchedulableProviderAccounts(accounts);
  const routableCount = Number(state.routableModelCount ?? getRoutableModelCount());
  const keys = state.activeKeys ?? getActiveKeys();
  const issues = [];

  if (accounts.length === 0) {
    issues.push('No provider account is connected.');
  }
  if (schedulable.length === 0) {
    issues.push('No provider account is currently schedulable.');
  }
  if (routableCount === 0) {
    issues.push('No model has a schedulable provider account.');
  }
  if (keys.length === 0) {
    issues.push('No active API key can call the gateway.');
  }

  return issues;
}

/** @param {number | null | undefined} value */
export function gatewayLimitLabel(value) {
  const limit = Number(value ?? 0);
  return limit > 0 ? String(limit) : 'Disabled';
}

/**
 * @param {Partial<APIKey>} key
 * @param {Array<Partial<ModelRoutingModel> & { model: string }>} routingModels
 */
export function apiKeyModelWarnings(key, routingModels) {
  if (key.revokedAt || key.modelPolicy !== 'selected') return [];
  const routable = new Set(
    routingModels
      .filter((model) => Number(model.enabledCount ?? 0) > 0)
      .map((model) => String(model.model ?? '').trim())
      .filter(Boolean)
  );
  return (key.allowedModels ?? [])
    .map((model) => String(model ?? '').trim())
    .filter((model, index, models) => model && models.indexOf(model) === index && !routable.has(model));
}

/**
 * @param {Record<string, any>} target
 * @param {Record<string, any>} values
 */
function replaceState(target, values) {
  for (const key of Object.keys(target)) {
    if (!(key in values)) {
      delete target[key];
    }
  }
  Object.assign(target, values);
}

/**
 * @param {string} path
 * @param {RequestInit} options
 * @returns {Promise<any>}
 */
async function requestJSON(path, options = {}) {
  const response = await fetch(path, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...(options.headers ?? {})
    }
  });
  if (!response.ok) {
    const payload = await response.json().catch(() => ({}));
    throw new Error(payload.error ?? `Request failed with ${response.status}`);
  }
  if (response.status === 204) return null;
  return response.json();
}

/** @param {string | null | undefined} value */
export function formatDate(value) {
  if (!value) return 'Never';
  return new Date(value).toLocaleString();
}

/**
 * @param {string | null | undefined} value
 * @param {Date} [now]
 */
export function futureTimeRemainingLabel(value, now = new Date()) {
  const until = Date.parse(String(value ?? ''));
  const nowTime = now.getTime();
  if (!Number.isFinite(until) || !Number.isFinite(nowTime) || until <= nowTime) {
    return '';
  }

  const totalMinutes = Math.ceil((until - nowTime) / 60000);
  if (totalMinutes < 60) {
    return `${totalMinutes}m remaining`;
  }

  const totalHours = Math.floor(totalMinutes / 60);
  const minutes = totalMinutes % 60;
  if (totalHours < 24) {
    return minutes > 0 ? `${totalHours}h ${minutes}m remaining` : `${totalHours}h remaining`;
  }

  const days = Math.floor(totalHours / 24);
  const hours = totalHours % 24;
  return hours > 0 ? `${days}d ${hours}h remaining` : `${days}d remaining`;
}

/** @param {number | null | undefined} value */
export function formatTokens(value) {
  return Number(value ?? 0).toLocaleString();
}

/** @param {number | null | undefined} value */
export function formatCostMicrousd(value) {
  return `$${(Number(value ?? 0) / 1_000_000).toFixed(4)}`;
}

/** @param {Partial<ProviderAccountAutoTestStatus> | null | undefined} status */
function normalizeProviderAccountAutoTestStatus(status) {
  return {
    running: Boolean(status?.running),
    lastStartedAt: status?.lastStartedAt ?? null,
    lastFinishedAt: status?.lastFinishedAt ?? null,
    lastAccountCount: Number(status?.lastAccountCount ?? 0),
    lastError: String(status?.lastError ?? '')
  };
}

export async function copySecret() {
  if (!apiKeys.oneTimeSecret) return;
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  const copied = await copyText(apiKeys.oneTimeSecret);
  if (!isCurrentAuthenticated(version)) return;
  if (!copied) {
    apiKeys.error = 'Copy failed';
  }
}

export async function copyAuthorizationURL() {
  if (!providerOAuth.authorizationUrl) return;
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  const copied = await copyText(providerOAuth.authorizationUrl);
  if (!isCurrentAuthenticated(version)) return;
  if (copied) {
    providerOAuth.copied = true;
  } else {
    provider.error = 'Copy failed';
  }
}

function clearAPIKeys() {
  replaceState(apiKeys, {
    loading: false,
    creating: false,
    error: '',
    items: [],
    newKeyName: '',
    oneTimeSecret: ''
  });
}

function clearGatewaySettings() {
  replaceState(gatewaySettings, {
    loading: false,
    saving: false,
    error: '',
    saved: false,
    data: null
  });
}

function clearRequestLogs() {
  replaceState(requestLogs, {
    loading: false,
    error: '',
    items: []
  });
}

function clearUsage() {
  replaceState(usage, {
    loading: false,
    error: '',
    range: '7d',
    groupBy: 'model',
    summaries: {},
    current: null
  });
  replaceState(usagePricing, {
    loading: false,
    saving: false,
    error: '',
    saved: false,
    version: 1,
    currency: 'USD',
    unit: '1M_tokens',
    rows: []
  });
}

function clearModelSettings() {
  replaceState(modelSettings, {
    loading: false,
    saving: false,
    error: '',
    saved: false,
    defaultModel: '',
    allowedModelsText: ''
  });
  replaceState(modelRouting, {
    loading: false,
    error: '',
    defaultModel: '',
    allowedModels: [],
    models: [],
    warnings: []
  });
  replaceState(modelRoutingPreview, {
    loading: false,
    error: '',
    model: '',
    sessionId: '',
    result: null
  });
  replaceState(accountModels, {});
}

function clearProvider() {
  replaceState(provider, {
    loading: false,
    connecting: false,
    error: '',
    data: null
  });
  replaceState(providerAccounts, { loading: false, saving: false, error: '', items: [] });
  replaceState(accountModels, {});
  replaceState(providerConnectForm, { name: '', priority: 100, enabled: true });
  replaceState(providerOAuth, { authorizationUrl: '', callbackUrl: '', completing: false, copied: false });
  resetAPIUpstreamForm();
}

function resetAPIUpstreamForm() {
  replaceState(apiUpstreamForm, {
    name: '',
    baseUrl: '',
    apiKey: '',
    priority: 100,
    loadFactor: 1,
    enabled: true,
    modelsText: '',
    submitting: false,
    error: ''
  });
}

/** @param {number} version */
function isCurrentAuthenticated(version) {
  return version === sessionVersion && session.authenticated;
}

export async function loadHealth() {
  try {
    const response = await fetch('/api/admin/health');
    if (!response.ok) {
      throw new Error(`Health check failed with ${response.status}`);
    }
    const payload = await response.json();
      replaceState(health, {
        loading: false,
        error: '',
        status: payload.status ?? 'unknown',
        database: payload.database ?? 'unknown'
      });
    } catch (error) {
    replaceState(health, {
      loading: false,
      error: error instanceof Error ? error.message : 'Health check failed',
      status: 'unavailable',
      database: 'unknown'
    });
  }
}

export async function loadSession() {
  const version = sessionVersion;
  session.loading = true;
  session.error = '';

  try {
    const response = await fetch('/api/admin/me');
    if (version !== sessionVersion) return;

    if (response.status === 401) {
      sessionVersion += 1;
      replaceState(session, { loading: false, authenticated: false, username: '', error: '' });
      clearProvider();
      clearAPIKeys();
      clearModelSettings();
      clearGatewaySettings();
      clearRequestLogs();
      clearUsage();
      return;
    }
    if (!response.ok) {
      const payload = await response.json().catch(() => ({}));
      throw new Error(payload.error ?? `Session check failed with ${response.status}`);
    }

    const payload = await response.json();
    if (version !== sessionVersion) return;

    sessionVersion += 1;
    replaceState(session, {
      loading: false,
      authenticated: true,
      username: payload.username ?? '',
      error: ''
    });
    await loadProvider();
    await loadProviderAccounts();
    await loadModelSettings();
    await loadModelRouting();
    await loadGatewaySettings();
    await loadKeys();
    await loadRequestLogs();
    await loadUsagePricing();
    await loadUsageSummary('24h', 'provider_account');
    await loadUsageSummary('24h', 'client_key');
    await loadUsageSummary('24h', 'session');
    await loadUsageSummary('24h', usage.groupBy);
    await loadUsageSummary('30d', usage.groupBy);
    await loadUsageSummary('7d', usage.groupBy);
  } catch (error) {
    if (version !== sessionVersion) return;

    sessionVersion += 1;
    replaceState(session, {
      loading: false,
      authenticated: false,
      username: '',
      error: error instanceof Error ? error.message : 'Session check failed'
    });
    clearProvider();
    clearAPIKeys();
    clearModelSettings();
    clearGatewaySettings();
    clearRequestLogs();
    clearUsage();
  }
}

/** @param {SubmitEvent} event */
export async function login(event) {
  event.preventDefault();
  loginForm.submitting = true;
  loginForm.error = '';

  try {
    await requestJSON('/api/admin/login', {
      method: 'POST',
      body: JSON.stringify({ username: loginForm.username, password: loginForm.password })
    });
    loginForm.password = '';
    sessionVersion += 1;
    await loadSession();
  } catch (error) {
    loginForm.error = error instanceof Error ? error.message : 'Login failed';
  } finally {
    loginForm.submitting = false;
  }
}

export async function logout() {
  sessionVersion += 1;
  await requestJSON('/api/admin/logout', { method: 'POST' }).catch(() => null);
  replaceState(session, { loading: false, authenticated: false, username: '', error: '' });
  clearProvider();
  clearAPIKeys();
  clearModelSettings();
  clearGatewaySettings();
  clearRequestLogs();
  clearUsage();
  loginForm.password = '';
}

export async function loadProvider() {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  provider.loading = true;
  provider.error = '';

  try {
    const payload = await requestJSON('/api/admin/provider-accounts/codex-oauth/status');
    if (!isCurrentAuthenticated(version)) return;
    provider.data = payload;
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    provider.error = error instanceof Error ? error.message : 'Failed to load provider status';
  } finally {
    if (!isCurrentAuthenticated(version)) return;
    provider.loading = false;
  }
}

export async function loadProviderAccounts() {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;
  providerAccounts.loading = true;
  providerAccounts.error = '';
  try {
    const payload = await requestJSON('/api/admin/provider-accounts');
    if (!isCurrentAuthenticated(version)) return;
    providerAccounts.items = payload.accounts ?? [];
    pruneSelectedProviderAccounts(providerAccounts.items.map((account) => account.id));
    pruneAccountModelStates(
      accountModels,
      providerAccounts.items.map((account) => account.id)
    );
    pruneAccountTestResultStates(
      accountTestResults,
      providerAccounts.items.map((account) => account.id)
    );
    for (const account of providerAccounts.items) {
      ensureAccountTestResultsState(account.id);
    }
    await Promise.all(providerAccounts.items.map((account) => loadAccountModels(account.id)));
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    providerAccounts.error = error instanceof Error ? error.message : 'Account load failed';
  } finally {
    if (isCurrentAuthenticated(version)) providerAccounts.loading = false;
  }
}

/** @param {number} accountId */
function ensureAccountModelsState(accountId) {
  const key = String(accountId);
  if (!accountModels[key]) {
    accountModels[key] = {
      loading: false,
      saving: false,
      error: '',
      saved: false,
      text: '',
      items: [],
      requestSeq: 0
    };
  }
  return accountModels[key];
}

/** @param {number} accountId */
function ensureAccountTestResultsState(accountId) {
  const key = String(accountId);
  if (!accountTestResults[key]) {
    accountTestResults[key] = {
      expanded: false,
      loading: false,
      error: '',
      items: [],
      requestSeq: 0
    };
  }
  return accountTestResults[key];
}

/** @param {AccountModel[]} models */
export function accountModelsText(models) {
  return modelListText(models.map((item) => item.model));
}

/** @param {number} accountId */
export function getAccountModelsState(accountId) {
  return ensureAccountModelsState(accountId);
}

/** @param {number} accountId */
export function getAccountTestResultsState(accountId) {
  return ensureAccountTestResultsState(accountId);
}

/** @param {number} accountId */
export async function loadAccountModels(accountId) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;
  const state = ensureAccountModelsState(accountId);
  state.requestSeq += 1;
  const requestSeq = state.requestSeq;
  state.loading = true;
  state.saving = false;
  state.error = '';
  state.saved = false;
  try {
    const payload = await requestJSON(`/api/admin/provider-accounts/${accountId}/models`);
    if (!isCurrentAuthenticated(version)) return;
    if (!shouldApplyAccountModelsResponse(state, requestSeq)) return;
    const models = payload.models ?? [];
    state.items = models;
    state.text = accountModelsText(models);
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    if (!shouldApplyAccountModelsResponse(state, requestSeq)) return;
    state.error = error instanceof Error ? error.message : 'Account model load failed';
  } finally {
    if (isCurrentAuthenticated(version) && shouldApplyAccountModelsResponse(state, requestSeq)) {
      state.loading = false;
    }
  }
}

/**
 * @param {number} accountId
 * @param {{ expand?: boolean }} options
 */
export async function loadAccountTestResults(accountId, options = {}) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;
  const state = ensureAccountTestResultsState(accountId);
  if (options.expand) state.expanded = true;
  state.requestSeq += 1;
  const requestSeq = state.requestSeq;
  state.loading = true;
  state.error = '';
  try {
    const payload = await requestJSON(`/api/admin/provider-accounts/${accountId}/test-results?limit=20`);
    if (!isCurrentAuthenticated(version)) return;
    if (!shouldApplyAccountTestResultsResponse(state, requestSeq)) return;
    state.items = payload.results ?? [];
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    if (!shouldApplyAccountTestResultsResponse(state, requestSeq)) return;
    state.error = error instanceof Error ? error.message : 'Account test history load failed';
  } finally {
    if (isCurrentAuthenticated(version) && shouldApplyAccountTestResultsResponse(state, requestSeq)) {
      state.loading = false;
    }
  }
}

/** @param {number} accountId */
export async function toggleAccountTestHistory(accountId) {
  const state = ensureAccountTestResultsState(accountId);
  if (state.expanded) {
    state.expanded = false;
    return;
  }
  await loadAccountTestResults(accountId, { expand: true });
}

/** @param {number} accountId */
async function refreshAccountTestResultsIfExpanded(accountId) {
  if (!accountTestResults[String(accountId)]?.expanded) return;
  await loadAccountTestResults(accountId);
}

export async function refreshExpandedAccountTestResults() {
  const expandedIds = Object.entries(accountTestResults)
    .filter(([, state]) => state.expanded)
    .map(([id]) => Number(id))
    .filter((id) => Number.isFinite(id) && id > 0);
  await Promise.all(expandedIds.map((id) => loadAccountTestResults(id)));
}

/**
 * @param {number} accountId
 * @param {string} text
 */
export async function saveAccountModels(accountId, text) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;
  const state = ensureAccountModelsState(accountId);
  state.requestSeq += 1;
  const requestSeq = state.requestSeq;
  state.loading = false;
  state.saving = true;
  state.error = '';
  state.saved = false;
  try {
    const payload = await requestJSON(`/api/admin/provider-accounts/${accountId}/models`, {
      method: 'PUT',
      body: JSON.stringify({ models: mergeAccountModelChanges(state.items, text) })
    });
    if (!isCurrentAuthenticated(version)) return;
    if (!shouldApplyAccountModelsResponse(state, requestSeq)) return;
    const models = payload.models ?? [];
    state.items = models;
    state.text = accountModelsText(models);
    state.saved = true;
    await loadModelRouting();
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    if (!shouldApplyAccountModelsResponse(state, requestSeq)) return;
    state.error = error instanceof Error ? error.message : 'Account model save failed';
  } finally {
    if (isCurrentAuthenticated(version) && shouldApplyAccountModelsResponse(state, requestSeq)) {
      state.saving = false;
    }
  }
}

/** @param {ProviderAccount} account */
export function accountLabel(account) {
  return account.name || account.displayName || account.subject || account.provider;
}

/** @param {ProviderAccount} account */
export function accountTypeLabel(account) {
  if (account.accountType === 'api_upstream') return 'API upstream';
  return 'Codex OAuth';
}

/** @param {string | null | undefined} status */
export function statusLabel(status) {
  if (!status) return 'active';
  return status.replaceAll('_', ' ');
}

function browserFingerprint() {
  if (typeof navigator === 'undefined') return '';
  return [
    navigator.userAgent,
    navigator.language,
    String(screen?.width ?? ''),
    String(screen?.height ?? ''),
    Intl.DateTimeFormat().resolvedOptions().timeZone
  ].join('|');
}

/**
 * @param {ProviderAccount | null} account
 */
export async function connectProvider(account = null) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  provider.connecting = true;
  provider.error = '';

  try {
    const payload = await requestJSON('/api/admin/provider-accounts/codex-oauth/connect', {
      method: 'POST',
      body: JSON.stringify({
        name: account ? account.name || account.displayName || '' : providerConnectForm.name,
        priority: account ? account.priority : Number(providerConnectForm.priority),
        enabled: account ? account.enabled : providerConnectForm.enabled,
        targetAccountId: account?.id ?? 0,
        fingerprint: browserFingerprint()
      })
    });
    if (!isCurrentAuthenticated(version)) return;
    providerOAuth.authorizationUrl = payload.authorizationUrl ?? '';
    providerOAuth.callbackUrl = '';
    providerOAuth.copied = false;
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    provider.error = error instanceof Error ? error.message : 'Failed to start provider connection';
  } finally {
    if (!isCurrentAuthenticated(version)) return;
    provider.connecting = false;
  }
}

export async function completeProviderCallback() {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  providerOAuth.completing = true;
  provider.error = '';
  providerAccounts.error = '';
  try {
    await requestJSON('/api/admin/provider-accounts/codex-oauth/callback', {
      method: 'POST',
      body: JSON.stringify({ callbackUrl: providerOAuth.callbackUrl })
    });
    if (!isCurrentAuthenticated(version)) return;
    replaceState(providerOAuth, { authorizationUrl: '', callbackUrl: '', completing: false, copied: false });
    await loadProvider();
    await loadProviderAccounts();
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    provider.error = error instanceof Error ? error.message : 'Failed to complete provider connection';
  } finally {
    if (!isCurrentAuthenticated(version)) return;
    providerOAuth.completing = false;
  }
}

/**
 * @param {ProviderAccount} account
 * @param {Partial<Pick<ProviderAccount, 'enabled' | 'priority' | 'loadFactor' | 'maxConcurrentRequests' | 'name' | 'baseUrl'>> & { apiKey?: string }} patch
 */
export async function updateProviderAccount(account, patch) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;
  providerAccounts.saving = true;
  providerAccounts.error = '';
  try {
    await requestJSON(`/api/admin/provider-accounts/${account.id}`, {
      method: 'PATCH',
      body: JSON.stringify(patch)
    });
    if (!isCurrentAuthenticated(version)) return;
    await loadProviderAccounts();
    await loadModelRouting();
    await refreshAccountTestResultsIfExpanded(account.id);
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    const message = error instanceof Error ? error.message : 'Account update failed';
    providerAccounts.error = message;
    await loadProviderAccounts();
    if (!isCurrentAuthenticated(version)) return;
    providerAccounts.error = message;
  } finally {
    if (isCurrentAuthenticated(version)) providerAccounts.saving = false;
  }
}

/** @param {boolean} enabled */
export async function bulkUpdateSelectedProviderAccounts(enabled) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;
  const accountIds = Object.keys(selectedProviderAccountIds)
    .map((id) => Number(id))
    .filter((id) => Number.isFinite(id) && id > 0);
  if (accountIds.length === 0) {
    providerAccounts.error = 'Select at least one provider account';
    return;
  }

  providerAccounts.saving = true;
  providerAccounts.error = '';
  try {
    await requestJSON('/api/admin/provider-accounts/bulk-update', {
      method: 'POST',
      body: JSON.stringify({ accountIds, enabled })
    });
    if (!isCurrentAuthenticated(version)) return;
    clearProviderAccountSelection();
    await loadProviderAccounts();
    await loadModelRouting();
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    const message = error instanceof Error ? error.message : 'Bulk account update failed';
    providerAccounts.error = message;
    await loadProviderAccounts();
    if (!isCurrentAuthenticated(version)) return;
    providerAccounts.error = message;
  } finally {
    if (isCurrentAuthenticated(version)) providerAccounts.saving = false;
  }
}

export async function bulkUpdateSelectedProviderAccountScheduling() {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;
  const accountIds = Object.keys(selectedProviderAccountIds)
    .map((id) => Number(id))
    .filter((id) => Number.isFinite(id) && id > 0);
  if (accountIds.length === 0) {
    providerAccounts.error = 'Select at least one provider account';
    return;
  }

  /** @type {{ accountIds: number[], priority?: number, loadFactor?: number, maxConcurrentRequests?: number }} */
  const payload = { accountIds };
  const priorityText = String(providerAccountBulkSchedulingForm.priority ?? '').trim();
  const loadFactorText = String(providerAccountBulkSchedulingForm.loadFactor ?? '').trim();
  const maxConcurrentRequestsText = String(providerAccountBulkSchedulingForm.maxConcurrentRequests ?? '').trim();
  if (priorityText) {
    const priority = Number(priorityText);
    if (!/^\d+$/.test(priorityText) || !Number.isInteger(priority) || priority < 0) {
      providerAccounts.error = 'Bulk priority must be a non-negative whole number';
      return;
    }
    payload.priority = priority;
  }
  if (loadFactorText) {
    const loadFactor = Number(loadFactorText);
    if (!/^\d+$/.test(loadFactorText) || !Number.isInteger(loadFactor) || loadFactor < 1 || loadFactor > 100) {
      providerAccounts.error = 'Bulk load factor must be a whole number from 1 to 100';
      return;
    }
    payload.loadFactor = loadFactor;
  }
  if (maxConcurrentRequestsText) {
    const maxConcurrentRequests = Number(maxConcurrentRequestsText);
    if (!/^\d+$/.test(maxConcurrentRequestsText) || !Number.isInteger(maxConcurrentRequests) || maxConcurrentRequests < 0) {
      providerAccounts.error = 'Bulk max concurrency must be a non-negative whole number';
      return;
    }
    payload.maxConcurrentRequests = maxConcurrentRequests;
  }
  if (payload.priority === undefined && payload.loadFactor === undefined && payload.maxConcurrentRequests === undefined) {
    providerAccounts.error = 'Enter a bulk priority, load factor, or max concurrency';
    return;
  }

  providerAccounts.saving = true;
  providerAccounts.error = '';
  try {
    await requestJSON('/api/admin/provider-accounts/bulk-update', {
      method: 'POST',
      body: JSON.stringify(payload)
    });
    if (!isCurrentAuthenticated(version)) return;
    clearProviderAccountSelection();
    clearProviderAccountBulkSchedulingForm();
    await loadProviderAccounts();
    await loadModelRouting();
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    const message = error instanceof Error ? error.message : 'Bulk scheduling update failed';
    providerAccounts.error = message;
    await loadProviderAccounts();
    if (!isCurrentAuthenticated(version)) return;
    providerAccounts.error = message;
  } finally {
    if (isCurrentAuthenticated(version)) providerAccounts.saving = false;
  }
}

export async function bulkReplaceSelectedProviderAccountModels() {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;
  const accountIds = Object.keys(selectedProviderAccountIds)
    .map((id) => Number(id))
    .filter((id) => Number.isFinite(id) && id > 0);
  if (accountIds.length === 0) {
    providerAccounts.error = 'Select at least one provider account';
    return;
  }

  const models = parseAccountModelsText(providerAccountBulkModelsForm.text);
  if (models.length === 0) {
    providerAccounts.error = 'Enter at least one model for selected accounts';
    return;
  }

  providerAccounts.saving = true;
  providerAccounts.error = '';
  try {
    await requestJSON('/api/admin/provider-accounts/bulk-models', {
      method: 'POST',
      body: JSON.stringify({ accountIds, models })
    });
    if (!isCurrentAuthenticated(version)) return;
    for (const id of accountIds) {
      delete accountModels[String(id)];
    }
    clearProviderAccountSelection();
    clearProviderAccountBulkModelsForm();
    await loadProviderAccounts();
    await loadModelRouting();
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    const message = error instanceof Error ? error.message : 'Bulk model update failed';
    providerAccounts.error = message;
    await loadProviderAccounts();
    if (!isCurrentAuthenticated(version)) return;
    providerAccounts.error = message;
  } finally {
    if (isCurrentAuthenticated(version)) providerAccounts.saving = false;
  }
}

/**
 * @param {ProviderAccount} account
 * @param {Event & { currentTarget: HTMLInputElement }} event
 */
export async function updateProviderAccountName(account, event) {
  const name = event.currentTarget.value.trim();
  const current = (account.name || '').trim();

  if (name === current) {
    event.currentTarget.value = current;
    return;
  }
  if (!name) {
    providerAccounts.error = 'Account name cannot be empty';
    event.currentTarget.value = current;
    return;
  }

  await updateProviderAccount(account, { name });
}

export async function createAPIUpstreamAccount() {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  apiUpstreamForm.submitting = true;
  apiUpstreamForm.error = '';
  providerAccounts.error = '';

  try {
    await requestJSON('/api/admin/provider-accounts/api-upstream', {
      method: 'POST',
      body: JSON.stringify({
        name: apiUpstreamForm.name,
        baseUrl: apiUpstreamForm.baseUrl,
        apiKey: apiUpstreamForm.apiKey,
        priority: Number(apiUpstreamForm.priority) || 100,
        loadFactor: Number(apiUpstreamForm.loadFactor) || 1,
        enabled: apiUpstreamForm.enabled,
        models: parseModelLines(apiUpstreamForm.modelsText)
      })
    });
    if (!isCurrentAuthenticated(version)) return;
    resetAPIUpstreamForm();
    await loadProviderAccounts();
    await loadModelRouting();
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    apiUpstreamForm.error = error instanceof Error ? error.message : 'API upstream account create failed';
  } finally {
    if (isCurrentAuthenticated(version)) apiUpstreamForm.submitting = false;
  }
}

/**
 * @param {ProviderAccount} account
 * @param {Event & { currentTarget: HTMLInputElement }} event
 */
export async function updateProviderAccountPriority(account, event) {
  const rawValue = event.currentTarget.value.trim();
  const priority = Number(rawValue);

  if (!/^\d+$/.test(rawValue)) {
    providerAccounts.error = 'Priority must be a non-negative whole number';
    event.currentTarget.value = String(account.priority);
    return;
  }

  await updateProviderAccount(account, { priority });
}

/**
 * @param {ProviderAccount} account
 * @param {Event & { currentTarget: HTMLInputElement }} event
 */
export async function updateProviderAccountLoadFactor(account, event) {
  const rawValue = event.currentTarget.value.trim();
  const loadFactor = Number(rawValue);

  if (!/^\d+$/.test(rawValue) || loadFactor < 1 || loadFactor > 100) {
    providerAccounts.error = 'Load factor must be a whole number from 1 to 100';
    event.currentTarget.value = String(account.loadFactor || 1);
    return;
  }

  await updateProviderAccount(account, { loadFactor });
}

/**
 * @param {ProviderAccount} account
 * @param {Event & { currentTarget: HTMLInputElement }} event
 */
export async function updateProviderAccountMaxConcurrentRequests(account, event) {
  const rawValue = event.currentTarget.value.trim();
  const maxConcurrentRequests = Number(rawValue);

  if (!/^\d+$/.test(rawValue) || !Number.isInteger(maxConcurrentRequests) || maxConcurrentRequests < 0) {
    providerAccounts.error = 'Max concurrency must be a non-negative whole number';
    event.currentTarget.value = String(account.maxConcurrentRequests || 0);
    return;
  }

  await updateProviderAccount(account, { maxConcurrentRequests });
}

/** @param {ProviderAccount} account */
export async function refreshProviderAccount(account) {
  const version = sessionVersion;
  providerAccounts.saving = true;
  providerAccounts.error = '';
  try {
    await requestJSON(`/api/admin/provider-accounts/${account.id}/refresh`, {
      method: 'POST'
    });
    if (!isCurrentAuthenticated(version)) return;
    await loadProvider();
    await loadProviderAccounts();
    await loadModelRouting();
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    const message = error instanceof Error ? error.message : 'Account refresh failed';
    providerAccounts.error = message;
    await loadProviderAccounts();
    if (!isCurrentAuthenticated(version)) return;
    providerAccounts.error = message;
  } finally {
    if (isCurrentAuthenticated(version)) providerAccounts.saving = false;
  }
}

/** @param {ProviderAccount} account */
export async function testProviderAccount(account) {
  const version = sessionVersion;
  providerAccounts.saving = true;
  providerAccounts.error = '';
  try {
    await requestJSON(`/api/admin/provider-accounts/${account.id}/test`, {
      method: 'POST'
    });
    if (!isCurrentAuthenticated(version)) return;
    await loadProviderAccounts();
    await loadModelRouting();
    await refreshExpandedAccountTestResults();
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    const message = error instanceof Error ? error.message : 'Account test failed';
    providerAccounts.error = message;
    await loadProviderAccounts();
    if (!isCurrentAuthenticated(version)) return;
    providerAccounts.error = message;
  } finally {
    if (isCurrentAuthenticated(version)) providerAccounts.saving = false;
  }
}

export async function testSelectedProviderAccounts() {
	const version = sessionVersion;
	if (!isCurrentAuthenticated(version)) return;
	const accountIds = Object.keys(selectedProviderAccountIds)
    .map((id) => Number(id))
    .filter((id) => Number.isFinite(id) && id > 0);
  if (accountIds.length === 0) {
    providerAccounts.error = 'Select at least one provider account';
    return;
  }

  providerAccounts.saving = true;
  providerAccounts.error = '';
  try {
    await requestJSON('/api/admin/provider-accounts/bulk-test', {
      method: 'POST',
      body: JSON.stringify({ accountIds })
    });
    if (!isCurrentAuthenticated(version)) return;
    clearProviderAccountSelection();
    await loadProviderAccounts();
    await loadModelRouting();
    await refreshExpandedAccountTestResults();
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    const message = error instanceof Error ? error.message : 'Selected account tests failed';
    providerAccounts.error = message;
    await loadProviderAccounts();
    if (!isCurrentAuthenticated(version)) return;
    providerAccounts.error = message;
  } finally {
    if (isCurrentAuthenticated(version)) providerAccounts.saving = false;
	}
}

export async function refreshSelectedProviderAccounts() {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;
  const accountIds = Object.keys(selectedProviderAccountIds)
    .map((id) => Number(id))
    .filter((id) => Number.isFinite(id) && id > 0);
  if (accountIds.length === 0) {
    providerAccounts.error = 'Select at least one provider account';
    return;
  }

  providerAccounts.saving = true;
  providerAccounts.error = '';
  try {
    await requestJSON('/api/admin/provider-accounts/bulk-refresh', {
      method: 'POST',
      body: JSON.stringify({ accountIds })
    });
    if (!isCurrentAuthenticated(version)) return;
    clearProviderAccountSelection();
    await loadProvider();
    await loadProviderAccounts();
    await loadModelRouting();
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    const message = error instanceof Error ? error.message : 'Selected account refresh failed';
    providerAccounts.error = message;
    await loadProviderAccounts();
    if (!isCurrentAuthenticated(version)) return;
    providerAccounts.error = message;
  } finally {
    if (isCurrentAuthenticated(version)) providerAccounts.saving = false;
  }
}

export async function pauseSelectedProviderAccounts() {
	const version = sessionVersion;
	if (!isCurrentAuthenticated(version)) return;
	const durationSeconds = validateProviderAccountPauseDuration();
	if (durationSeconds === null) return;
	const accountIds = Object.keys(selectedProviderAccountIds)
		.map((id) => Number(id))
		.filter((id) => Number.isFinite(id) && id > 0);
	if (accountIds.length === 0) {
		providerAccounts.error = 'Select at least one provider account';
		return;
	}

	providerAccounts.saving = true;
	providerAccounts.error = '';
	try {
		await requestJSON('/api/admin/provider-accounts/bulk-pause', {
			method: 'POST',
			body: JSON.stringify({ accountIds, durationSeconds })
		});
		if (!isCurrentAuthenticated(version)) return;
		clearProviderAccountSelection();
		await loadProviderAccounts();
		await loadModelRouting();
	} catch (error) {
		if (!isCurrentAuthenticated(version)) return;
		const message = error instanceof Error ? error.message : 'Selected account pause failed';
		providerAccounts.error = message;
		await loadProviderAccounts();
		if (!isCurrentAuthenticated(version)) return;
		providerAccounts.error = message;
	} finally {
		if (isCurrentAuthenticated(version)) providerAccounts.saving = false;
	}
}

export async function resetSelectedProviderAccountStatus() {
	const version = sessionVersion;
	if (!isCurrentAuthenticated(version)) return;
	const accountIds = Object.keys(selectedProviderAccountIds)
		.map((id) => Number(id))
		.filter((id) => Number.isFinite(id) && id > 0);
	if (accountIds.length === 0) {
		providerAccounts.error = 'Select at least one provider account';
		return;
	}

	providerAccounts.saving = true;
	providerAccounts.error = '';
	try {
		await requestJSON('/api/admin/provider-accounts/bulk-reset-status', {
			method: 'POST',
			body: JSON.stringify({ accountIds })
		});
		if (!isCurrentAuthenticated(version)) return;
		clearProviderAccountSelection();
		await loadProviderAccounts();
		await loadModelRouting();
	} catch (error) {
		if (!isCurrentAuthenticated(version)) return;
		const message = error instanceof Error ? error.message : 'Selected account status reset failed';
		providerAccounts.error = message;
		await loadProviderAccounts();
		if (!isCurrentAuthenticated(version)) return;
		providerAccounts.error = message;
	} finally {
		if (isCurrentAuthenticated(version)) providerAccounts.saving = false;
	}
}

export async function testAllProviderAccounts() {
	const version = sessionVersion;
	providerAccounts.saving = true;
  providerAccounts.error = '';
  try {
    await requestJSON('/api/admin/provider-accounts/test', {
      method: 'POST'
    });
    if (!isCurrentAuthenticated(version)) return;
    await loadProviderAccounts();
    await loadModelRouting();
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    const message = error instanceof Error ? error.message : 'Account tests failed';
    providerAccounts.error = message;
    await loadProviderAccounts();
    if (!isCurrentAuthenticated(version)) return;
    providerAccounts.error = message;
  } finally {
    if (isCurrentAuthenticated(version)) providerAccounts.saving = false;
  }
}

/** @param {ProviderAccount} account */
export async function pauseProviderAccount(account) {
  const version = sessionVersion;
  const durationSeconds = validateProviderAccountPauseDuration();
  if (durationSeconds === null) return;

  providerAccounts.saving = true;
  providerAccounts.error = '';
  try {
    await requestJSON(`/api/admin/provider-accounts/${account.id}/pause`, {
      method: 'POST',
      body: JSON.stringify({ durationSeconds: durationSeconds })
    });
    if (!isCurrentAuthenticated(version)) return;
    await loadProviderAccounts();
    await loadModelRouting();
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    const message = error instanceof Error ? error.message : 'Account scheduling pause failed';
    providerAccounts.error = message;
    await loadProviderAccounts();
    if (!isCurrentAuthenticated(version)) return;
    providerAccounts.error = message;
  } finally {
    if (isCurrentAuthenticated(version)) providerAccounts.saving = false;
  }
}

export function validateProviderAccountPauseDuration() {
  const durationSeconds = Number(providerAccountPauseForm.durationSeconds);
  if (!Number.isInteger(durationSeconds) || durationSeconds < 60 || durationSeconds > 86400) {
    providerAccounts.error = 'Pause duration must be a whole number between 60 and 86400 seconds';
    return null;
  }
  return durationSeconds;
}

/** @param {ProviderAccount} account */
export async function resetProviderAccountStatus(account) {
  const version = sessionVersion;
  providerAccounts.saving = true;
  providerAccounts.error = '';
  try {
    await requestJSON(`/api/admin/provider-accounts/${account.id}/reset-status`, {
      method: 'POST'
    });
    if (!isCurrentAuthenticated(version)) return;
    await loadProviderAccounts();
    await loadModelRouting();
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    const message = error instanceof Error ? error.message : 'Account status reset failed';
    providerAccounts.error = message;
    await loadProviderAccounts();
    if (!isCurrentAuthenticated(version)) return;
    providerAccounts.error = message;
  } finally {
    if (isCurrentAuthenticated(version)) providerAccounts.saving = false;
  }
}

/** @param {ProviderAccount} account */
export async function disconnectProviderAccount(account) {
  const version = sessionVersion;
  providerAccounts.saving = true;
  providerAccounts.error = '';
  try {
    await requestJSON(`/api/admin/provider-accounts/${account.id}`, { method: 'DELETE' });
    if (!isCurrentAuthenticated(version)) return;
    await loadProvider();
    await loadProviderAccounts();
    await loadModelRouting();
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    const message = error instanceof Error ? error.message : 'Account disconnect failed';
    providerAccounts.error = message;
    await loadProviderAccounts();
    if (!isCurrentAuthenticated(version)) return;
    providerAccounts.error = message;
  } finally {
    if (isCurrentAuthenticated(version)) providerAccounts.saving = false;
  }
}

export async function loadKeys() {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  apiKeys.loading = true;
  apiKeys.error = '';

  try {
    const payload = await requestJSON('/api/admin/keys');
    if (!isCurrentAuthenticated(version)) return;
    apiKeys.items = payload.keys ?? [];
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    apiKeys.error = error instanceof Error ? error.message : 'Failed to load API keys';
  } finally {
    if (!isCurrentAuthenticated(version)) return;
    apiKeys.loading = false;
  }
}

export async function loadGatewaySettings() {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  gatewaySettings.loading = true;
  gatewaySettings.error = '';
  gatewaySettings.saved = false;

  try {
    const payload = await requestJSON('/api/admin/gateway-settings');
    if (!isCurrentAuthenticated(version)) return;
    gatewaySettings.data = {
      maxConcurrentGatewayRequests: Number(payload.maxConcurrentGatewayRequests ?? 0),
      maxConcurrentRequestsPerAccount: Number(payload.maxConcurrentRequestsPerAccount ?? 0),
      maxConcurrentRequestsPerKey: Number(payload.maxConcurrentRequestsPerKey ?? 0),
      requestsPerMinutePerKey: Number(payload.requestsPerMinutePerKey ?? 0),
      tokensPerMinutePerKey: Number(payload.tokensPerMinutePerKey ?? 0),
      providerAccountAutoTestEnabled: Boolean(payload.providerAccountAutoTestEnabled),
      providerAccountAutoTestIntervalSeconds: Number(payload.providerAccountAutoTestIntervalSeconds ?? 300),
      providerAccountAutoTestStatus: normalizeProviderAccountAutoTestStatus(payload.providerAccountAutoTestStatus)
    };
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    gatewaySettings.error = error instanceof Error ? error.message : 'Failed to load gateway settings';
  } finally {
    if (!isCurrentAuthenticated(version)) return;
    gatewaySettings.loading = false;
  }
}

export async function updateGatewaySettings() {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version) || !gatewaySettings.data) return;

  const payload = {
    maxConcurrentGatewayRequests: Number(gatewaySettings.data.maxConcurrentGatewayRequests),
    maxConcurrentRequestsPerAccount: Number(gatewaySettings.data.maxConcurrentRequestsPerAccount),
    maxConcurrentRequestsPerKey: Number(gatewaySettings.data.maxConcurrentRequestsPerKey),
    requestsPerMinutePerKey: Number(gatewaySettings.data.requestsPerMinutePerKey),
    tokensPerMinutePerKey: Number(gatewaySettings.data.tokensPerMinutePerKey),
    providerAccountAutoTestEnabled: Boolean(gatewaySettings.data.providerAccountAutoTestEnabled),
    providerAccountAutoTestIntervalSeconds: Number(gatewaySettings.data.providerAccountAutoTestIntervalSeconds)
  };
  const numericValues = [
    payload.maxConcurrentGatewayRequests,
    payload.maxConcurrentRequestsPerAccount,
    payload.maxConcurrentRequestsPerKey,
    payload.requestsPerMinutePerKey,
    payload.tokensPerMinutePerKey,
    payload.providerAccountAutoTestIntervalSeconds
  ];
  if (
    numericValues.some((value) => !Number.isInteger(value) || value < 0)
  ) {
    gatewaySettings.error = 'Gateway settings must use non-negative whole numbers';
    gatewaySettings.saved = false;
    return;
  }
  if (payload.providerAccountAutoTestEnabled && payload.providerAccountAutoTestIntervalSeconds < 60) {
    gatewaySettings.error = 'Provider account auto test interval must be at least 60 seconds when enabled';
    gatewaySettings.saved = false;
    return;
  }

  gatewaySettings.saving = true;
  gatewaySettings.error = '';
  gatewaySettings.saved = false;

  try {
    const saved = await requestJSON('/api/admin/gateway-settings', {
      method: 'PUT',
      body: JSON.stringify(payload)
    });
    if (!isCurrentAuthenticated(version)) return;
    gatewaySettings.data = {
      maxConcurrentGatewayRequests: Number(saved.maxConcurrentGatewayRequests ?? 0),
      maxConcurrentRequestsPerAccount: Number(saved.maxConcurrentRequestsPerAccount ?? 0),
      maxConcurrentRequestsPerKey: Number(saved.maxConcurrentRequestsPerKey ?? 0),
      requestsPerMinutePerKey: Number(saved.requestsPerMinutePerKey ?? 0),
      tokensPerMinutePerKey: Number(saved.tokensPerMinutePerKey ?? 0),
      providerAccountAutoTestEnabled: Boolean(saved.providerAccountAutoTestEnabled),
      providerAccountAutoTestIntervalSeconds: Number(saved.providerAccountAutoTestIntervalSeconds ?? 300),
      providerAccountAutoTestStatus: normalizeProviderAccountAutoTestStatus(
        saved.providerAccountAutoTestStatus ?? gatewaySettings.data.providerAccountAutoTestStatus
      )
    };
    gatewaySettings.saved = true;
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    gatewaySettings.error = error instanceof Error ? error.message : 'Failed to update gateway settings';
  } finally {
    if (!isCurrentAuthenticated(version)) return;
    gatewaySettings.saving = false;
  }
}

/**
 * @param {number} keyId
 * @param {string} modelPolicy
 * @param {string} modelsText
 */
export async function updateAPIKeyModelPolicy(keyId, modelPolicy, modelsText) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  apiKeys.error = '';
  try {
    const payload = await requestJSON(`/api/admin/keys/${keyId}/model-policy`, {
      method: 'PUT',
      body: JSON.stringify({
        modelPolicy,
        models: parseModelLines(modelsText)
      })
    });
    if (!isCurrentAuthenticated(version)) return;
    apiKeys.items = apiKeys.items.map((key) => (key.id === keyId ? payload.key : key));
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    apiKeys.error = error instanceof Error ? error.message : 'Failed to update model access';
  }
}

/**
 * @param {number} keyId
 * @param {string | number} requestsPerMinute
 * @param {string | number} tokensPerMinute
 */
export async function updateAPIKeyLimits(keyId, requestsPerMinute, tokensPerMinute) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  const requestLimit = Number(requestsPerMinute);
  const tokenLimit = Number(tokensPerMinute);
  if (!Number.isInteger(requestLimit) || requestLimit < 0 || !Number.isInteger(tokenLimit) || tokenLimit < 0) {
    apiKeys.error = 'API key limits must be non-negative whole numbers';
    return;
  }

  apiKeys.error = '';
  try {
    const payload = await requestJSON(`/api/admin/keys/${keyId}/limits`, {
      method: 'PUT',
      body: JSON.stringify({
        requestsPerMinute: requestLimit,
        tokensPerMinute: tokenLimit
      })
    });
    if (!isCurrentAuthenticated(version)) return;
    apiKeys.items = apiKeys.items.map((key) => (key.id === keyId ? payload.key : key));
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    apiKeys.error = error instanceof Error ? error.message : 'Failed to update key limits';
  }
}

export async function loadModelSettings() {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  modelSettings.loading = true;
  modelSettings.error = '';
  modelSettings.saved = false;

  try {
    const payload = await requestJSON('/api/admin/model-settings');
    if (!isCurrentAuthenticated(version)) return;
    modelSettings.defaultModel = payload.defaultModel ?? '';
    modelSettings.allowedModelsText = (payload.allowedModels ?? []).join('\n');
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    modelSettings.error = error instanceof Error ? error.message : 'Failed to load model settings';
  } finally {
    if (!isCurrentAuthenticated(version)) return;
    modelSettings.loading = false;
  }
}

export async function loadModelRouting() {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  modelRouting.loading = true;
  modelRouting.error = '';

  try {
    const payload = await requestJSON('/api/admin/model-routing');
    if (!isCurrentAuthenticated(version)) return;
    modelRouting.defaultModel = payload.defaultModel ?? '';
    modelRouting.allowedModels = payload.allowedModels ?? [];
    modelRouting.models = payload.models ?? [];
    modelRouting.warnings = payload.warnings ?? [];
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    modelRouting.error = error instanceof Error ? error.message : 'Failed to load model routing';
  } finally {
    if (!isCurrentAuthenticated(version)) return;
    modelRouting.loading = false;
  }
}

export async function loadModelRoutingPreview() {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;
  const model = modelRoutingPreview.model.trim();
  const sessionId = modelRoutingPreview.sessionId.trim();
  const excludedAccountIds = modelRoutingPreview.excludedAccountIds.trim();
  if (!model) {
    modelRoutingPreview.error = 'Model is required for selection preview';
    modelRoutingPreview.result = null;
    return;
  }

  modelRoutingPreview.loading = true;
  modelRoutingPreview.error = '';

  const params = new URLSearchParams({ model });
  if (sessionId) {
    params.set('sessionId', sessionId);
  }
  if (excludedAccountIds) {
    params.set('excludedAccountIds', excludedAccountIds);
  }

  try {
    const payload = await requestJSON(`/api/admin/model-routing/preview?${params.toString()}`);
    if (!isCurrentAuthenticated(version)) return;
    modelRoutingPreview.result = payload;
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    modelRoutingPreview.error = error instanceof Error ? error.message : 'Failed to preview model routing';
    modelRoutingPreview.result = null;
  } finally {
    if (!isCurrentAuthenticated(version)) return;
    modelRoutingPreview.loading = false;
  }
}

/** @param {SubmitEvent} event */
export async function saveModelSettings(event) {
  event.preventDefault();
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  modelSettings.saving = true;
  modelSettings.error = '';
  modelSettings.saved = false;

  const allowedModels = parseModelLines(modelSettings.allowedModelsText);

  try {
    const payload = await requestJSON('/api/admin/model-settings', {
      method: 'PUT',
      body: JSON.stringify({
        defaultModel: modelSettings.defaultModel,
        allowedModels
      })
    });
    if (!isCurrentAuthenticated(version)) return;
    modelSettings.defaultModel = payload.defaultModel ?? '';
    modelSettings.allowedModelsText = (payload.allowedModels ?? []).join('\n');
    modelSettings.saved = true;
    await loadModelRouting();
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    modelSettings.error = error instanceof Error ? error.message : 'Failed to save model settings';
  } finally {
    if (!isCurrentAuthenticated(version)) return;
    modelSettings.saving = false;
  }
}

export async function loadRequestLogs() {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  requestLogs.loading = true;
  requestLogs.error = '';

  try {
    const payload = await requestJSON('/api/admin/request-logs?limit=50');
    if (!isCurrentAuthenticated(version)) return;
    requestLogs.items = payload.logs ?? [];
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    requestLogs.error = error instanceof Error ? error.message : 'Failed to load request logs';
  } finally {
    if (!isCurrentAuthenticated(version)) return;
    requestLogs.loading = false;
  }
}

/**
 * @param {string} range
 * @param {string} groupBy
 */
function usageSummaryKey(range, groupBy) {
  return `${range}:${groupBy}`;
}

/**
 * @param {string} [range]
 * @param {string} [groupBy]
 */
export async function loadUsageSummary(range = usage.range, groupBy = usage.groupBy) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  usage.loading = true;
  usage.error = '';

  try {
    const params = new URLSearchParams({ range, groupBy });
    const payload = await requestJSON(`/api/admin/usage-summary?${params.toString()}`);
    if (!isCurrentAuthenticated(version)) return;
    usage.range = payload.range ?? range;
    usage.groupBy = payload.groupBy ?? groupBy;
    usage.current = payload;
    usage.summaries[usageSummaryKey(usage.range, usage.groupBy)] = payload;
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    usage.error = error instanceof Error ? error.message : 'Failed to load usage summary';
  } finally {
    if (!isCurrentAuthenticated(version)) return;
    usage.loading = false;
  }
}

export async function loadUsagePricing() {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  usagePricing.loading = true;
  usagePricing.error = '';
  usagePricing.saved = false;

  try {
    const payload = await requestJSON('/api/admin/usage-pricing');
    if (!isCurrentAuthenticated(version)) return;
    usagePricing.version = payload.version ?? 1;
    usagePricing.currency = payload.currency ?? 'USD';
    usagePricing.unit = payload.unit ?? '1M_tokens';
    usagePricing.rows = Object.entries(payload.models ?? {}).map(([model, price]) => ({
      model,
      inputMicrousdPerMillion: Number(price?.inputMicrousdPerMillion ?? 0),
      cachedInputMicrousdPerMillion: Number(price?.cachedInputMicrousdPerMillion ?? 0),
      outputMicrousdPerMillion: Number(price?.outputMicrousdPerMillion ?? 0)
    }));
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    usagePricing.error = error instanceof Error ? error.message : 'Failed to load usage pricing';
  } finally {
    if (!isCurrentAuthenticated(version)) return;
    usagePricing.loading = false;
  }
}

/** @param {SubmitEvent} event */
export async function saveUsagePricing(event) {
  event.preventDefault();
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  usagePricing.saving = true;
  usagePricing.error = '';
  usagePricing.saved = false;

  /** @type {Record<string, Omit<UsagePricingRow, 'model'>>} */
  const models = {};
  for (const row of usagePricing.rows) {
    const model = String(row.model ?? '').trim();
    if (!model) continue;
    models[model] = {
      inputMicrousdPerMillion: Number(row.inputMicrousdPerMillion) || 0,
      cachedInputMicrousdPerMillion: Number(row.cachedInputMicrousdPerMillion) || 0,
      outputMicrousdPerMillion: Number(row.outputMicrousdPerMillion) || 0
    };
  }

  try {
    const payload = await requestJSON('/api/admin/usage-pricing', {
      method: 'PUT',
      body: JSON.stringify({
        version: usagePricing.version,
        currency: usagePricing.currency,
        unit: usagePricing.unit,
        models
      })
    });
    if (!isCurrentAuthenticated(version)) return;
    usagePricing.version = payload.version ?? 1;
    usagePricing.currency = payload.currency ?? 'USD';
    usagePricing.unit = payload.unit ?? '1M_tokens';
    usagePricing.rows = Object.entries(payload.models ?? {}).map(([model, price]) => ({
      model,
      inputMicrousdPerMillion: Number(price?.inputMicrousdPerMillion ?? 0),
      cachedInputMicrousdPerMillion: Number(price?.cachedInputMicrousdPerMillion ?? 0),
      outputMicrousdPerMillion: Number(price?.outputMicrousdPerMillion ?? 0)
    }));
    usagePricing.saved = true;
    await loadUsageSummary(usage.range, usage.groupBy);
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    usagePricing.error = error instanceof Error ? error.message : 'Failed to save usage pricing';
  } finally {
    if (!isCurrentAuthenticated(version)) return;
    usagePricing.saving = false;
  }
}

/** @param {SubmitEvent} event */
export async function createKey(event) {
  event.preventDefault();
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  apiKeys.creating = true;
  apiKeys.error = '';
  apiKeys.oneTimeSecret = '';

  try {
    const payload = await requestJSON('/api/admin/keys', {
      method: 'POST',
      body: JSON.stringify({ name: apiKeys.newKeyName })
    });
    if (!isCurrentAuthenticated(version)) return;
    apiKeys.items = [payload.key, ...apiKeys.items];
    apiKeys.oneTimeSecret = payload.secret;
    apiKeys.newKeyName = '';
    await loadRequestLogs();
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    apiKeys.error = error instanceof Error ? error.message : 'Failed to create API key';
  } finally {
    if (!isCurrentAuthenticated(version)) return;
    apiKeys.creating = false;
  }
}

/** @param {number} id */
export async function revokeKey(id) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  apiKeys.error = '';

  try {
    const payload = await requestJSON(`/api/admin/keys/${id}/revoke`, { method: 'POST' });
    if (!isCurrentAuthenticated(version)) return;
    apiKeys.items = apiKeys.items.map((key) => (key.id === id ? payload.key : key));
    await loadRequestLogs();
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    apiKeys.error = error instanceof Error ? error.message : 'Failed to revoke API key';
  }
}


export function initializeAdminState() {
  loadHealth();
  loadSession();
}
