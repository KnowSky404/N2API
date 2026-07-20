import { copyText } from '$lib/clipboard.js';

/**
 * @typedef {object} APIKey
 * @property {number} id
 * @property {string} name
 * @property {string} modelPolicy
 * @property {string[]} allowedModels
 * @property {string | undefined} allowedModelsText
 * @property {string} prefix
 * @property {boolean} secretAvailable
 * @property {string} createdAt
 * @property {string | null} lastUsedAt
 * @property {string | null} revokedAt
 * @property {string | null | undefined} physicalDeleteAt
 * @property {string | null} disabledAt
 * @property {number} requestsPerMinute
 * @property {number} tokensPerMinute
 * @property {number} requestBudget24h
 * @property {number} tokenBudget24h
 * @property {number} costBudgetMicrousd24h
 * @property {number} requestBudget30d
 * @property {number} tokenBudget30d
 * @property {number} costBudgetMicrousd30d
 * @property {number} currentConcurrentRequests
 * @property {number} effectiveMaxConcurrentRequests
 * @property {boolean} concurrencyBlocked
 * @property {number} currentRequestsThisMinute
 * @property {number} effectiveRequestsPerMinute
 * @property {number} requestRateRemaining
 * @property {boolean} requestRateLimited
 * @property {number} currentTokensThisMinute
 * @property {number} effectiveTokensPerMinute
 * @property {number} tokenRateRemaining
 * @property {boolean} tokenRateLimited
 * @property {number} requestsUsed24h
 * @property {number} tokensUsed24h
 * @property {number} costMicrousd24h
 * @property {number} requestsUsed30d
 * @property {number} tokensUsed30d
 * @property {number} costMicrousd30d
 * @property {number | null} requestsRemaining24h
 * @property {number | null} tokensRemaining24h
 * @property {number | null} costRemainingMicrousd24h
 * @property {number | null} requestsRemaining30d
 * @property {number | null} tokensRemaining30d
 * @property {number | null} costRemainingMicrousd30d
 * @property {boolean} requestBudgetExceeded
 * @property {boolean} tokenBudgetExceeded
 * @property {boolean} costBudgetExceeded
 * @property {number | null} routingPoolId
 * @property {string} routingPoolName
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
 * @property {boolean} proxyUrlConfigured
 * @property {string} proxyUrlSummary
 * @property {boolean} enabled
 * @property {number} priority
 * @property {number} loadFactor
 * @property {number} maxConcurrentRequests
 * @property {number} currentConcurrentRequests
 * @property {number} effectiveMaxConcurrentRequests
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
 * @property {number | null} fingerprintProfileId
 */

/**
 * @typedef {object} RoutingPoolAccount
 * @property {number} accountId
 * @property {number} priority
 */

/**
 * @typedef {object} RoutingPool
 * @property {number} id
 * @property {string} name
 * @property {string} description
 * @property {boolean} enabled
 * @property {number | null} fallbackPoolId
 * @property {string} fallbackPoolName
 * @property {number[]} accountIds
 * @property {RoutingPoolAccount[]} accounts
 * @property {string} createdAt
 * @property {string} updatedAt
 */

/**
 * @typedef {object} RequestLog
 * @property {number} id
 * @property {string} requestId
 * @property {number} clientKeyId
 * @property {string} clientKey
 * @property {string} provider
 * @property {string} model
 * @property {number} providerAccountId
 * @property {string} providerAccountType
 * @property {string} providerAccountName
 * @property {number} routingPoolId
 * @property {string} routingPoolName
 * @property {number} routingPoolFallbackDepth
 * @property {string} routingPoolFallbackChain
 * @property {string} routingPoolError
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
 * @property {number} gatewayAttemptCount
 * @property {number} gatewayFallbackCount
 * @property {string} createdAt
 */

/**
 * @typedef {object} SystemEvent
 * @property {number} id
 * @property {string} occurredAt
 * @property {'audit' | 'security' | 'oauth' | 'scheduler' | 'runtime'} category
 * @property {'info' | 'warning' | 'error'} severity
 * @property {string} action
 * @property {'success' | 'failure' | 'partial'} outcome
 * @property {{ type: 'admin' | 'system', id?: number, name: string }} actor
 * @property {{ type: string, id: string, name: string }} target
 * @property {string} correlationId
 * @property {string} sourceIp
 * @property {string} httpMethod
 * @property {string} routePattern
 * @property {number | null} statusCode
 * @property {number} durationMs
 * @property {string} errorCode
 * @property {string} message
 * @property {Record<string, unknown>} metadata
 */

/**
 * @typedef {object} UsageSummaryRow
 * @property {string} id
 * @property {string} label
 * @property {number} requests
 * @property {number} inputTokens
 * @property {number} outputTokens
 * @property {number} totalTokens
 * @property {number} cachedInputTokens
 * @property {number} reasoningTokens
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
 * @property {number} totalCachedInputTokens
 * @property {number} totalReasoningTokens
 * @property {number} estimatedCostMicrousd
 * @property {UsageSummaryRow[]} rows
 */

/**
 * @typedef {object} UsagePricingRow
 * @property {string} model
 * @property {number} inputMicrousdPerMillion
 * @property {number} cachedInputMicrousdPerMillion
 * @property {number} outputMicrousdPerMillion
 * @property {number} longInputMicrousdPerMillion
 * @property {number} longCachedInputMicrousdPerMillion
 * @property {number} longOutputMicrousdPerMillion
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
 * @property {string | null} lastTestAt
 * @property {string} lastTestStatus
 * @property {number} lastTestHttpStatus
 * @property {number} lastTestLatencyMs
 * @property {string} lastError
 */

/**
 * @typedef {object} AccountModelTestResult
 * @property {number} accountId
 * @property {string} model
 * @property {'passed' | 'failed'} status
 * @property {string} errorCode
 * @property {number} httpStatus
 * @property {number} latencyMs
 * @property {string} message
 * @property {string} checkedAt
 */

/**
 * @typedef {object} AccountModelSyncSummary
 * @property {number} total
 * @property {number} new
 * @property {number} preserved
 * @property {number} skippedManual
 */

/**
 * @typedef {object} AccountModelsState
 * @property {boolean} loading
 * @property {boolean} saving
 * @property {string} error
 * @property {boolean} syncing
 * @property {string} syncError
 * @property {string} syncMessage
 * @property {AccountModelSyncSummary | null} syncSummary
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
 * @property {number[]} routingPoolIds
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
 * @property {string} scheduleReason
 * @property {boolean} schedulable
 * @property {string} unschedulableReason
 * @property {boolean} stickyBound
 * @property {number} currentConcurrentRequests
 * @property {number} effectiveMaxConcurrentRequests
 * @property {boolean} concurrencyBlocked
 */

/**
 * @typedef {object} ModelRoutingModel
 * @property {string} model
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
 * @property {number} requestLogRetentionDays
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
 * @property {string} scheduleReason
 * @property {boolean} selected
 * @property {boolean} stickyBound
 * @property {boolean} schedulable
 * @property {string} unschedulableReason
 * @property {number} currentConcurrentRequests
 * @property {number} effectiveMaxConcurrentRequests
 * @property {boolean} concurrencyBlocked
 */

/**
 * @typedef {object} SelectionPreview
 * @property {string} model
 * @property {string} sessionId
 * @property {number} selectedAccountId
 * @property {number} stickyBoundAccountId
 * @property {number} routingPoolId
 * @property {string} routingPoolName
 * @property {number} routingPoolFallbackDepth
 * @property {string} routingPoolFallbackChain
 * @property {string} routingPoolError
 * @property {string} diagnosisStatus
 * @property {string} diagnosisSummary
 * @property {string[]} diagnosisHints
 * @property {{ reason: string, count: number }[]} blockedReasonCounts
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
/** @type {{ loading: boolean, saving: boolean, error: string, items: RoutingPool[], newPoolName: string, newPoolDescription: string, newPoolFallbackPoolId: string }} */
export const routingPools = $state({
  loading: false,
  saving: false,
  error: '',
  items: [],
  newPoolName: '',
  newPoolDescription: '',
  newPoolFallbackPoolId: '0'
});
export const providerConnectForm = $state({ name: '', priority: 100, enabled: true, fingerprintProfileId: '0' });
export const providerAccountPauseForm = $state({ durationSeconds: 300 });
export const providerAccountBulkSchedulingForm = $state({ priority: '', loadFactor: '', maxConcurrentRequests: '' });
export const providerAccountBulkModelsForm = $state({ text: '' });
export const providerOAuth = $state({ authorizationUrl: '', callbackUrl: '', completing: false, copied: false });
export const apiUpstreamForm = $state({
  name: '',
  baseUrl: '',
  apiKey: '',
  proxyUrl: '',
  priority: 100,
  loadFactor: 1,
  enabled: true,
  fingerprintProfileId: '0',
  modelsText: '',
  submitting: false,
  error: ''
});
/** @type {{ loading: boolean, creating: boolean, saving: boolean, error: string, items: APIKey[], newKeyName: string, newKeyRoutingPoolId: number, oneTimeSecret: string }} */
export const apiKeys = $state({
  loading: false,
  creating: false,
  saving: false,
  error: '',
  items: [],
  newKeyName: '',
  newKeyRoutingPoolId: 0,
  oneTimeSecret: ''
});
/** @type {Record<string, boolean>} */
export const selectedAPIKeyIds = $state({});
/** @type {{ loading: boolean, saving: boolean, cleanupRunning: boolean, error: string, saved: boolean, cleanupResult: { retentionDays: number, deleted: number, before: string } | null, data: GatewaySettingsData | null }} */
export const gatewaySettings = $state({
  loading: false,
  saving: false,
  cleanupRunning: false,
  error: '',
  saved: false,
  cleanupResult: null,
  data: null
});
/** @type {{ loading: boolean, error: string, requestId: string, query: string, statusClass: string, statusCode: string, since: string, providerAccountId: string, routingPoolId: string, clientKeyId: string, model: string, sessionId: string, errorCode: string, usageSource: string, routingPoolError: string, routingPoolChain: string, gatewayFallbacks: boolean, items: RequestLog[] }} */
export const requestLogs = $state({
  loading: false,
  error: '',
  requestId: '',
  query: '',
  statusClass: 'all',
  statusCode: '',
  since: '',
  providerAccountId: 'all',
  routingPoolId: 'all',
  clientKeyId: 'all',
  model: '',
  sessionId: '',
  errorCode: '',
  usageSource: 'all',
  routingPoolError: 'all',
  routingPoolChain: '',
  gatewayFallbacks: false,
  items: []
});

/** @type {{ loading: boolean, loadingOlder: boolean, error: string, query: string, since: string, category: string, outcome: string, severity: string, action: string, actor: string, targetType: string, targetId: string, items: SystemEvent[], nextCursor: string, hasMore: boolean }} */
export const systemEvents = $state({
  loading: false,
  loadingOlder: false,
  error: '',
  query: '',
  since: '',
  category: 'all',
  outcome: 'all',
  severity: 'all',
  action: '',
  actor: '',
  targetType: '',
  targetId: '',
  items: [],
  nextCursor: '',
  hasMore: false
});
let systemEventRequestSequence = 0;

export function resetSystemEventFilters() {
  systemEvents.error = '';
  systemEvents.query = '';
  systemEvents.since = '';
  systemEvents.category = 'all';
  systemEvents.outcome = 'all';
  systemEvents.severity = 'all';
  systemEvents.action = '';
  systemEvents.actor = '';
  systemEvents.targetType = '';
  systemEvents.targetId = '';
  systemEvents.nextCursor = '';
  systemEvents.hasMore = false;
}

export function resetRequestLogFilters() {
  requestLogs.error = '';
  requestLogs.requestId = '';
  requestLogs.query = '';
  requestLogs.statusClass = 'all';
  requestLogs.statusCode = '';
  requestLogs.since = '';
  requestLogs.providerAccountId = 'all';
  requestLogs.routingPoolId = 'all';
  requestLogs.clientKeyId = 'all';
  requestLogs.model = '';
  requestLogs.sessionId = '';
  requestLogs.errorCode = '';
  requestLogs.usageSource = 'all';
  requestLogs.routingPoolError = 'all';
  requestLogs.routingPoolChain = '';
  requestLogs.gatewayFallbacks = false;
}
/** @type {{ loading: boolean, error: string, range: string, groupBy: string, summaries: Record<string, UsageSummary>, current: UsageSummary | null }} */
export const usage = $state({
  loading: false,
  error: '',
  range: '7d',
  groupBy: 'model',
  summaries: {},
  current: null
});
/** @type {{ loading: boolean, saving: boolean, syncing: boolean, removingShutdown: boolean, ignoringUpcoming: boolean, error: string, saved: boolean, syncMessage: string, removalMessage: string, upcomingShutdowns: Array<{model: string, shutdownDate: string, replacement: string}>, deletionCandidates: Array<{model: string, shutdownDate: string, replacement: string}>, version: number, currency: string, unit: string, rows: UsagePricingRow[] }} */
export const usagePricing = $state({
  loading: false,
  saving: false,
  syncing: false,
  removingShutdown: false,
  ignoringUpcoming: false,
  error: '',
  saved: false,
  syncMessage: '',
  removalMessage: '',
  upcomingShutdowns: [],
  deletionCandidates: [],
  version: 1,
  currency: 'USD',
  unit: '1M_tokens',
  rows: []
});
/** @type {{ loading: boolean, saving: boolean, error: string, saved: boolean, defaultModel: string }} */
export const modelSettings = $state({
  loading: false,
  saving: false,
  error: '',
  saved: false,
  defaultModel: ''
});
/** @type {Record<string, AccountModelsState>} */
export const accountModels = $state({});
/** @type {Record<string, AccountTestResultsState>} */
export const accountTestResults = $state({});
/** @type {Record<string, boolean>} */
export const selectedProviderAccountIds = $state({});
/** @type {{ loading: boolean, error: string, defaultModel: string, models: ModelRoutingModel[], warnings: string[] }} */
export const modelRouting = $state({
  loading: false,
  error: '',
  defaultModel: '',
  models: [],
  warnings: []
});
/** @type {{ loading: boolean, error: string, model: string, sessionId: string, routingPoolId: string, excludedAccountIds: string, result: SelectionPreview | null }} */
export const modelRoutingPreview = $state({
  loading: false,
  error: '',
  model: '',
  sessionId: '',
  routingPoolId: '0',
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
  return models.filter((item) => item.source === 'upstream' || item.model !== modelName);
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

/** @param {APIKey[]} [keys] */
export function getActiveKeys(keys = apiKeys.items) {
  return keys.filter((key) => !key.revokedAt && !key.disabledAt);
}

/** @param {APIKey[]} [keys] */
export function getGatewayReadyKeys(keys = apiKeys.items) {
  return getActiveKeys(keys).filter((key) => {
    const routingPoolID = Number(key.routingPoolId ?? 0);
    return Number.isInteger(routingPoolID) && routingPoolID > 0;
  });
}

/** @param {string | null | undefined} value @param {Date} [now] */
function isFutureTimestamp(value, now = new Date()) {
  if (!value) return false;
  const timestamp = Date.parse(value);
  return Number.isFinite(timestamp) && timestamp > now.getTime();
}

/** @param {string | null | undefined} value @param {Date} [now] */
function isElapsedTimestamp(value, now = new Date()) {
  if (!value) return false;
  const timestamp = Date.parse(value);
  return Number.isFinite(timestamp) && timestamp <= now.getTime();
}

/** @param {Partial<ProviderAccount>} account @param {Date} [now] */
export function providerAccountEffectiveStatus(account, now = new Date()) {
  const status = account.status ?? '';
  if (status === 'rate_limited' && isElapsedTimestamp(account.rateLimitedUntil, now)) return 'active';
  if (status === 'circuit_open' && isElapsedTimestamp(account.circuitOpenUntil, now)) return 'active';
  return status;
}

/** @param {Partial<ProviderAccount>} account */
function isProviderAccountSchedulable(account) {
  if (!account.enabled) return false;
  if (isFutureTimestamp(account.rateLimitedUntil) || isFutureTimestamp(account.circuitOpenUntil)) return false;
  return !['disabled', 'expired', 'rate_limited', 'circuit_open'].includes(providerAccountEffectiveStatus(account));
}

/** @param {Partial<ProviderAccount>} account */
function providerAccountUnschedulableReason(account) {
  if (!account.enabled) return 'disabled';
  if (isFutureTimestamp(account.rateLimitedUntil)) return 'rate_limited';
  if (isFutureTimestamp(account.circuitOpenUntil)) return 'circuit_open';
  const status = providerAccountEffectiveStatus(account);
  if (['disabled', 'expired', 'rate_limited', 'circuit_open'].includes(status)) return status;
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

/**
 * Build a providers page href filtered by unschedulable reason.
 * @param {string | null | undefined} reason
 */
export function unschedulableReasonHref(reason) {
  const value = String(reason ?? '').trim();
  const params = new URLSearchParams();
  if (value === 'disabled') {
    params.set('status', 'disabled');
  } else if (value === 'rate_limited') {
    params.set('status', 'rate_limited');
  } else if (value === 'circuit_open') {
    params.set('status', 'circuit_open');
  } else if (value === 'expired') {
    params.set('status', 'expired');
  } else {
    params.set('status', 'blocked');
  }
  const query = params.toString();
  return query ? `/providers?${query}` : '/providers';
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
  const keys = getGatewayReadyKeys(state.activeKeys ?? apiKeys.items);
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
    issues.push('No gateway-ready API key can call the gateway.');
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
 * @param {Array<Partial<RoutingPool> & { id: number }>} [pools]
 */
export function apiKeyModelWarnings(key, routingModels, pools = []) {
  if (key.revokedAt || key.modelPolicy !== 'selected') return [];
  const routingPoolID = Number(key.routingPoolId ?? 0);
  const routingPoolIDs = routingPoolChainIDs(routingPoolID, pools);
  const routable = new Set(
    routingModels
      .filter((model) => {
        if (routingPoolID <= 0) return Number(model.enabledCount ?? 0) > 0;
        return (model.accounts ?? []).some((account) => {
          if (!account.schedulable) return false;
          return (account.routingPoolIds ?? []).some((poolID) => routingPoolIDs.has(Number(poolID)));
        });
      })
      .map((model) => String(model.model ?? '').trim())
      .filter(Boolean)
  );
  return (key.allowedModels ?? [])
    .map((model) => String(model ?? '').trim())
    .filter((model, index, models) => model && models.indexOf(model) === index && !routable.has(model));
}

/**
 * @param {number} routingPoolID
 * @param {Array<Partial<RoutingPool> & { id: number }>} pools
 */
function routingPoolChainIDs(routingPoolID, pools) {
  const ids = new Set();
  if (routingPoolID <= 0) return ids;
  const poolByID = new Map(pools.map((pool) => [Number(pool.id), pool]));
  let currentID = routingPoolID;
  while (currentID > 0 && !ids.has(currentID)) {
    ids.add(currentID);
    const pool = poolByID.get(currentID);
    currentID = Number(pool?.fallbackPoolId ?? 0);
  }
  return ids;
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

/** @param {Partial<RequestLog> | null | undefined} log */
export function formatRequestLogCost(log) {
  if (
    log &&
    !log.pricingMatched &&
    (Number(log.inputTokens ?? 0) > 0 ||
      Number(log.outputTokens ?? 0) > 0 ||
      Number(log.totalTokens ?? 0) > 0)
  ) {
    return 'Unpriced';
  }
  return formatCostMicrousd(log?.estimatedCostMicrousd);
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

/** @param {number} id */
export async function copyAPIKeySecret(id) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  apiKeys.error = '';

  try {
    const payload = await requestJSON(`/api/admin/keys/${id}/secret`);
    if (!isCurrentAuthenticated(version)) return;
    const copied = await copyText(payload.secret);
    if (!isCurrentAuthenticated(version)) return;
    if (!copied) {
      apiKeys.error = 'Copy failed';
    }
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    apiKeys.error = error instanceof Error ? error.message : 'Failed to copy API key';
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
    saving: false,
    error: '',
    items: [],
    newKeyName: '',
    newKeyRoutingPoolId: 0,
    oneTimeSecret: ''
  });
}

function clearGatewaySettings() {
  replaceState(gatewaySettings, {
    loading: false,
    saving: false,
    cleanupRunning: false,
    error: '',
    saved: false,
    cleanupResult: null,
    data: null
  });
}

function clearRequestLogs() {
  replaceState(requestLogs, {
    loading: false,
    error: '',
    requestId: '',
    query: '',
    statusClass: 'all',
    statusCode: '',
    since: '',
    providerAccountId: 'all',
    routingPoolId: 'all',
    clientKeyId: 'all',
    model: '',
    sessionId: '',
    errorCode: '',
    usageSource: 'all',
    routingPoolError: 'all',
    routingPoolChain: '',
    gatewayFallbacks: false,
    items: []
  });
}

function clearSystemEvents() {
  systemEventRequestSequence += 1;
  replaceState(systemEvents, {
    loading: false,
    loadingOlder: false,
    error: '',
    query: '',
    since: '',
    category: 'all',
    outcome: 'all',
    severity: 'all',
    action: '',
    actor: '',
    targetType: '',
    targetId: '',
    items: [],
    nextCursor: '',
    hasMore: false
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
    syncing: false,
    removingShutdown: false,
    ignoringUpcoming: false,
    error: '',
    saved: false,
    syncMessage: '',
    removalMessage: '',
    upcomingShutdowns: [],
    deletionCandidates: [],
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
    defaultModel: ''
  });
  replaceState(modelRouting, {
    loading: false,
    error: '',
    defaultModel: '',
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
  replaceState(providerConnectForm, { name: '', priority: 100, enabled: true, fingerprintProfileId: '0' });
  replaceState(providerOAuth, { authorizationUrl: '', callbackUrl: '', completing: false, copied: false });
  resetAPIUpstreamForm();
}

function resetAPIUpstreamForm() {
  replaceState(apiUpstreamForm, {
    name: '',
    baseUrl: '',
    apiKey: '',
    proxyUrl: '',
    priority: 100,
    loadFactor: 1,
    enabled: true,
    fingerprintProfileId: '0',
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
      clearSystemEvents();
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
    await loadUsageSummary('24h', 'usage_source');
    await loadUsageSummary('24h', 'routing_pool');
    await loadUsageSummary('24h', 'routing_pool_chain');
    await loadUsageSummary('24h', 'client_key');
    await loadUsageSummary('24h', 'session');
    await loadUsageSummary('24h', usage.groupBy);
    await loadUsageSummary('30d', usage.groupBy);
    await loadUsageSummary('7d', usage.groupBy);
    await loadOpsDashboard(86400);
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
    clearSystemEvents();
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
  clearSystemEvents();
  clearUsage();
  loginForm.password = '';
}

export const changePasswordForm = $state({ currentPassword: '', newPassword: '', submitting: false, error: '', saved: false });

/**
 * @param {Event} event
 */
export async function changePassword(event) {
  event.preventDefault();
  changePasswordForm.error = '';
  changePasswordForm.saved = false;

  const currentPassword = changePasswordForm.currentPassword.trim();
  const newPassword = changePasswordForm.newPassword.trim();
  if (!currentPassword || !newPassword) {
    changePasswordForm.error = 'Both fields are required.';
    return;
  }
  if (newPassword.length < 8) {
    changePasswordForm.error = 'New password must be at least 8 characters.';
    return;
  }

  changePasswordForm.submitting = true;
  try {
    const response = await requestJSON('/api/admin/change-password', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ currentPassword, newPassword }),
    });
    if (response && response.ok === 'true') {
      changePasswordForm.saved = true;
      changePasswordForm.currentPassword = '';
      changePasswordForm.newPassword = '';
    } else {
      changePasswordForm.error = 'Password change failed.';
    }
  } catch (error) {
    changePasswordForm.error = error instanceof Error ? error.message : 'Failed to change password';
  } finally {
    changePasswordForm.submitting = false;
  }
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
      syncing: false,
      syncError: '',
      syncMessage: '',
      syncSummary: null,
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
  return modelListText(models.filter((item) => item.source !== 'upstream').map((item) => item.model));
}

/** @param {number} accountId */
export function getAccountModelsState(accountId) {
  return ensureAccountModelsState(accountId);
}

/**
 * @param {AccountModel[]} models
 * @param {AccountModelTestResult} result
 */
export function applyAccountModelTestResult(models, result) {
  return models.map((item) =>
    item.model === result.model
      ? {
          ...item,
          lastTestAt: result.checkedAt,
          lastTestStatus: result.status,
          lastTestHttpStatus: result.httpStatus,
          lastTestLatencyMs: result.latencyMs,
          lastError: result.status === 'passed' ? '' : result.message
        }
      : item
  );
}

/** @param {number} accountId */
export function getAccountTestResultsState(accountId) {
  return ensureAccountTestResultsState(accountId);
}

/**
 * @param {{ model: string; enabled: boolean; source?: string | null }[]} models
 */
export function accountModelSummary(models) {
  let total = 0;
  let synced = 0;
  let manual = 0;
  let enabled = 0;
  for (const m of models) {
    total++;
    if (m.source === 'upstream') synced++;
    else manual++;
    if (m.enabled) enabled++;
  }
  return { total, synced, manual, enabled };
}

/** @param {{ source?: string | null }} model */
export function sourceBadgeLabel(model) {
  return model.source === 'upstream' ? 'Synced' : 'Manual';
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
 * @param {string} model
 * @returns {Promise<AccountModelTestResult>}
 */
export async function testProviderAccountModel(accountId, model) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) {
    throw new Error('Authentication required');
  }

  const normalizedModel = String(model ?? '').trim();
  if (!normalizedModel) {
    throw new Error('Model is required');
  }

  const payload = await requestJSON(`/api/admin/provider-accounts/${accountId}/model-tests`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ model: normalizedModel })
  });
  if (!isCurrentAuthenticated(version)) {
    throw new Error('Authentication required');
  }

  const result = /** @type {AccountModelTestResult | undefined} */ (payload.result);
  if (!result) {
    throw new Error('Model test returned no result');
  }

  const state = ensureAccountModelsState(accountId);
  state.items = applyAccountModelTestResult(state.items, result);
  return result;
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
    const manualItems = state.items.filter((item) => item.source !== 'upstream');
    const payload = await requestJSON(`/api/admin/provider-accounts/${accountId}/models`, {
      method: 'PUT',
      body: JSON.stringify({ models: mergeAccountModelChanges(manualItems, text) })
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

/** @param {number} accountId */
export async function syncAccountModels(accountId) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;
  const state = ensureAccountModelsState(accountId);
  state.requestSeq += 1;
  const requestSeq = state.requestSeq;
  state.syncing = true;
  state.error = '';
  state.syncError = '';
  state.syncMessage = '';
  state.syncSummary = null;
  state.saved = false;
  try {
    const payload = await requestJSON(`/api/admin/provider-accounts/${accountId}/models/sync`, {
      method: 'POST'
    });
    if (!isCurrentAuthenticated(version)) return;
    if (!shouldApplyAccountModelsResponse(state, requestSeq)) return;
    const models = payload.models ?? [];
    state.items = models;
    state.text = accountModelsText(models);
    state.syncSummary = payload.synced ?? null;
    const added = Number(payload.synced?.new ?? 0);
    const total = Number(payload.synced?.total ?? models.length);
    if (added > 0) {
      state.syncMessage = `Synced ${total} models. ${added} new model${added === 1 ? ' was' : 's were'} added disabled.`;
    } else {
      state.syncMessage = `Synced ${total} models.`;
    }
    await loadModelRouting();
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    if (!shouldApplyAccountModelsResponse(state, requestSeq)) return;
    state.syncError = error instanceof Error ? error.message : 'Account model sync failed';
  } finally {
    if (isCurrentAuthenticated(version) && shouldApplyAccountModelsResponse(state, requestSeq)) {
      state.syncing = false;
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
  const screenInfo = globalThis.screen;
  return [
    navigator.userAgent,
    navigator.language,
    String(screenInfo?.width ?? ''),
    String(screenInfo?.height ?? ''),
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
        fingerprintProfileId: account ? account.fingerprintProfileId ?? 0 : Number(providerConnectForm.fingerprintProfileId),
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
 * @param {Partial<Pick<ProviderAccount, 'enabled' | 'priority' | 'loadFactor' | 'maxConcurrentRequests' | 'name' | 'baseUrl' | 'fingerprintProfileId'>> & { apiKey?: string, proxyUrl?: string }} patch
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
 * @param {string | number | null | undefined} poolId
 * @param {string | number | null | undefined} priorityValue
 */
export async function addSelectedProviderAccountsToRoutingPool(poolId, priorityValue = 0) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;
  const targetPoolId = Number(poolId ?? 0);
  if (!Number.isInteger(targetPoolId) || targetPoolId <= 0) {
    providerAccounts.error = 'Select a routing pool';
    return;
  }
  const accountIds = Object.keys(selectedProviderAccountIds)
    .map((id) => Number(id))
    .filter((id) => Number.isFinite(id) && id > 0);
  if (accountIds.length === 0) {
    providerAccounts.error = 'Select at least one provider account';
    return;
  }
  const pool = routingPools.items.find((item) => item.id === targetPoolId);
  if (!pool) {
    providerAccounts.error = 'Routing pool not found';
    return;
  }
  const priorityText = String(priorityValue ?? '').trim();
  const priority = priorityText === '' ? 0 : Number(priorityText);
  if (priorityText !== '' && (!/^\d+$/.test(priorityText) || !Number.isInteger(priority) || priority < 0)) {
    providerAccounts.error = 'Pool priority must be a non-negative whole number';
    return;
  }

  const accounts = [...(pool.accounts ?? [])];
  const existing = new Set(accounts.map((account) => Number(account.accountId)));
  for (const accountId of accountIds) {
    if (!existing.has(accountId)) {
      accounts.push({ accountId, priority });
      existing.add(accountId);
    }
  }

  providerAccounts.saving = true;
  providerAccounts.error = '';
  routingPools.error = '';
  try {
    const payload = await requestJSON(`/api/admin/routing-pools/${targetPoolId}/accounts`, {
      method: 'PUT',
      body: JSON.stringify({ accounts })
    });
    if (!isCurrentAuthenticated(version)) return;
    routingPools.items = routingPools.items.map((item) => (item.id === targetPoolId ? payload.pool : item));
    clearProviderAccountSelection();
    await loadRoutingPools();
    await loadModelRouting();
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    providerAccounts.error = error instanceof Error ? error.message : 'Failed to assign routing pool';
  } finally {
    if (isCurrentAuthenticated(version)) providerAccounts.saving = false;
  }
}

/** @param {string | number | null | undefined} poolId */
export async function removeSelectedProviderAccountsFromRoutingPool(poolId) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;
  const targetPoolId = Number(poolId ?? 0);
  if (!Number.isInteger(targetPoolId) || targetPoolId <= 0) {
    providerAccounts.error = 'Select a routing pool';
    return;
  }
  const accountIds = Object.keys(selectedProviderAccountIds)
    .map((id) => Number(id))
    .filter((id) => Number.isFinite(id) && id > 0);
  if (accountIds.length === 0) {
    providerAccounts.error = 'Select at least one provider account';
    return;
  }
  const pool = routingPools.items.find((item) => item.id === targetPoolId);
  if (!pool) {
    providerAccounts.error = 'Routing pool not found';
    return;
  }

  const removed = new Set(accountIds);
  const accounts = (pool.accounts ?? []).filter((account) => !removed.has(Number(account.accountId)));

  providerAccounts.saving = true;
  providerAccounts.error = '';
  routingPools.error = '';
  try {
    const payload = await requestJSON(`/api/admin/routing-pools/${targetPoolId}/accounts`, {
      method: 'PUT',
      body: JSON.stringify({ accounts })
    });
    if (!isCurrentAuthenticated(version)) return;
    routingPools.items = routingPools.items.map((item) => (item.id === targetPoolId ? payload.pool : item));
    clearProviderAccountSelection();
    await loadRoutingPools();
    await loadModelRouting();
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    providerAccounts.error = error instanceof Error ? error.message : 'Failed to remove routing pool membership';
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
  if (!isCurrentAuthenticated(version)) return false;

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
        proxyUrl: apiUpstreamForm.proxyUrl,
        priority: Number(apiUpstreamForm.priority) || 100,
        loadFactor: Number(apiUpstreamForm.loadFactor) || 1,
        enabled: apiUpstreamForm.enabled,
        fingerprintProfileId: Number(apiUpstreamForm.fingerprintProfileId),
        models: parseModelLines(apiUpstreamForm.modelsText)
      })
    });
    if (!isCurrentAuthenticated(version)) return false;
    resetAPIUpstreamForm();
    await loadProviderAccounts();
    await loadModelRouting();
    return true;
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return false;
    apiUpstreamForm.error = error instanceof Error ? error.message : 'API upstream account create failed';
    return false;
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


/** @param {any} account @param {string} fingerprintProfileId "0" means clear */
export async function updateProviderAccountFingerprintProfile(account, fingerprintProfileId) {
  const id = fingerprintProfileId === '0' ? null : (Number(fingerprintProfileId) || null);
  await updateProviderAccount(account, { fingerprintProfileId: id });
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
    const payload = await requestJSON(`/api/admin/provider-accounts/${account.id}/test`, {
      method: 'POST'
    });
    if (!isCurrentAuthenticated(version)) return null;
    await loadProviderAccounts();
    await loadModelRouting();
    await refreshExpandedAccountTestResults();
    return payload?.account ?? null;
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return null;
    const message = error instanceof Error ? error.message : 'Account test failed';
    providerAccounts.error = message;
    await loadProviderAccounts();
    if (!isCurrentAuthenticated(version)) return null;
    providerAccounts.error = message;
    return null;
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

export async function disconnectSelectedProviderAccounts() {
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
    await requestJSON('/api/admin/provider-accounts/bulk-disconnect', {
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
    const message = error instanceof Error ? error.message : 'Selected account disconnect failed';
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

export async function loadRoutingPools() {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  routingPools.loading = true;
  routingPools.error = '';
  try {
    const payload = await requestJSON('/api/admin/routing-pools');
    if (!isCurrentAuthenticated(version)) return;
    routingPools.items = payload.pools ?? [];
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    routingPools.error = error instanceof Error ? error.message : 'Failed to load routing pools';
  } finally {
    if (!isCurrentAuthenticated(version)) return;
    routingPools.loading = false;
  }
}

export async function createRoutingPool() {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  const name = routingPools.newPoolName.trim();
  if (!name) {
    routingPools.error = 'Routing pool name cannot be empty';
    return;
  }

  routingPools.saving = true;
  routingPools.error = '';
  const fallbackPoolId = Number(routingPools.newPoolFallbackPoolId || 0);
  if (!Number.isInteger(fallbackPoolId) || fallbackPoolId < 0) {
    routingPools.error = 'Fallback pool selection is invalid';
    routingPools.saving = false;
    return;
  }
  try {
    const payload = await requestJSON('/api/admin/routing-pools', {
      method: 'POST',
      body: JSON.stringify({
        name,
        description: routingPools.newPoolDescription,
        enabled: true,
        fallbackPoolId: fallbackPoolId > 0 ? fallbackPoolId : null
      })
    });
    if (!isCurrentAuthenticated(version)) return;
    routingPools.items = [...routingPools.items, payload.pool];
    routingPools.newPoolName = '';
    routingPools.newPoolDescription = '';
    routingPools.newPoolFallbackPoolId = '0';
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    routingPools.error = error instanceof Error ? error.message : 'Failed to create routing pool';
  } finally {
    if (!isCurrentAuthenticated(version)) return;
    routingPools.saving = false;
  }
}

/** @param {RoutingPool} pool */
export async function updateRoutingPool(pool) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  routingPools.saving = true;
  routingPools.error = '';
  const fallbackPoolId = Number(pool.fallbackPoolId ?? 0);
  if (!Number.isInteger(fallbackPoolId) || fallbackPoolId < 0 || fallbackPoolId === Number(pool.id)) {
    routingPools.error = 'Fallback pool selection is invalid';
    routingPools.saving = false;
    return;
  }
  try {
    const payload = await requestJSON(`/api/admin/routing-pools/${pool.id}`, {
      method: 'PATCH',
      body: JSON.stringify({
        name: pool.name,
        description: pool.description,
        enabled: Boolean(pool.enabled),
        fallbackPoolId: fallbackPoolId > 0 ? fallbackPoolId : null
      })
    });
    if (!isCurrentAuthenticated(version)) return;
    routingPools.items = routingPools.items.map((item) => (item.id === pool.id ? payload.pool : item));
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    routingPools.error = error instanceof Error ? error.message : 'Failed to update routing pool';
  } finally {
    if (!isCurrentAuthenticated(version)) return;
    routingPools.saving = false;
  }
}

/** @param {number} poolId */
export async function deleteRoutingPool(poolId) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  routingPools.saving = true;
  routingPools.error = '';
  try {
    await requestJSON(`/api/admin/routing-pools/${poolId}`, { method: 'DELETE' });
    if (!isCurrentAuthenticated(version)) return;
    routingPools.items = routingPools.items.filter((pool) => pool.id !== poolId);
    await loadRoutingPools();
    if (!isCurrentAuthenticated(version)) return;
    await loadKeys();
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    routingPools.error = error instanceof Error ? error.message : 'Failed to delete routing pool';
  } finally {
    if (!isCurrentAuthenticated(version)) return;
    routingPools.saving = false;
  }
}

/**
 * @param {number} poolId
 * @param {RoutingPoolAccount[]} accounts
 */
export async function replaceRoutingPoolAccounts(poolId, accounts) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  routingPools.saving = true;
  routingPools.error = '';
  try {
    const payload = await requestJSON(`/api/admin/routing-pools/${poolId}/accounts`, {
      method: 'PUT',
      body: JSON.stringify({ accounts })
    });
    if (!isCurrentAuthenticated(version)) return;
    routingPools.items = routingPools.items.map((pool) => (pool.id === poolId ? payload.pool : pool));
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    routingPools.error = error instanceof Error ? error.message : 'Failed to save pool membership';
  } finally {
    if (!isCurrentAuthenticated(version)) return;
    routingPools.saving = false;
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
      requestLogRetentionDays: Number(payload.requestLogRetentionDays ?? 0),
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
    providerAccountAutoTestIntervalSeconds: Number(gatewaySettings.data.providerAccountAutoTestIntervalSeconds),
    requestLogRetentionDays: Number(gatewaySettings.data.requestLogRetentionDays)
  };
  const numericValues = [
    payload.maxConcurrentGatewayRequests,
    payload.maxConcurrentRequestsPerAccount,
    payload.maxConcurrentRequestsPerKey,
    payload.requestsPerMinutePerKey,
    payload.tokensPerMinutePerKey,
    payload.providerAccountAutoTestIntervalSeconds,
    payload.requestLogRetentionDays
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
  gatewaySettings.cleanupResult = null;

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
      requestLogRetentionDays: Number(saved.requestLogRetentionDays ?? 0),
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

export async function cleanupRequestLogs() {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version) || !gatewaySettings.data) return;

  const retentionDays = Number(gatewaySettings.data.requestLogRetentionDays);
  if (!Number.isInteger(retentionDays) || retentionDays <= 0) {
    gatewaySettings.error = 'Request log retention must be greater than 0 before cleanup';
    gatewaySettings.saved = false;
    gatewaySettings.cleanupResult = null;
    return;
  }

  gatewaySettings.cleanupRunning = true;
  gatewaySettings.error = '';
  gatewaySettings.saved = false;
  gatewaySettings.cleanupResult = null;

  try {
    const result = await requestJSON('/api/admin/request-logs/cleanup', {
      method: 'POST'
    });
    if (!isCurrentAuthenticated(version)) return;
    gatewaySettings.cleanupResult = {
      retentionDays: Number(result.retentionDays ?? retentionDays),
      deleted: Number(result.deleted ?? 0),
      before: result.before ?? ''
    };
    await loadRequestLogs();
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    gatewaySettings.error = error instanceof Error ? error.message : 'Failed to clean request logs';
  } finally {
    if (!isCurrentAuthenticated(version)) return;
    gatewaySettings.cleanupRunning = false;
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
 * @param {string} name
 */
export async function updateAPIKeyName(keyId, name) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  const nextName = String(name ?? '').trim();
  if (!nextName) {
    apiKeys.error = 'API key name cannot be empty';
    return;
  }

  apiKeys.error = '';
  try {
    const payload = await requestJSON(`/api/admin/keys/${keyId}`, {
      method: 'PATCH',
      body: JSON.stringify({ name: nextName })
    });
    if (!isCurrentAuthenticated(version)) return;
    apiKeys.items = apiKeys.items.map((key) => (key.id === keyId ? payload.key : key));
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    apiKeys.error = error instanceof Error ? error.message : 'Failed to update API key name';
  }
}

/**
 * @param {number} keyId
 * @param {boolean} disabled
 */
export async function setAPIKeyDisabled(keyId, disabled) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  apiKeys.error = '';
  try {
    const payload = await requestJSON(`/api/admin/keys/${keyId}/disabled`, {
      method: 'PUT',
      body: JSON.stringify({ disabled: Boolean(disabled) })
    });
    if (!isCurrentAuthenticated(version)) return;
    apiKeys.items = apiKeys.items.map((key) => (key.id === keyId ? payload.key : key));
    await loadRequestLogs();
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    apiKeys.error = error instanceof Error ? error.message : 'Failed to update API key status';
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

/**
 * @param {number} keyId
 * @param {string | number} requestBudget24h
 * @param {string | number} tokenBudget24h
 * @param {string | number} costBudgetMicrousd24h
 * @param {string | number} requestBudget30d
 * @param {string | number} tokenBudget30d
 * @param {string | number} costBudgetMicrousd30d
 */
export async function updateAPIKeyBudgets(keyId, requestBudget24h, tokenBudget24h, costBudgetMicrousd24h, requestBudget30d, tokenBudget30d, costBudgetMicrousd30d) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  const payload = {
    requestBudget24h: Number(requestBudget24h),
    tokenBudget24h: Number(tokenBudget24h),
    costBudgetMicrousd24h: Number(costBudgetMicrousd24h),
    requestBudget30d: Number(requestBudget30d),
    tokenBudget30d: Number(tokenBudget30d),
    costBudgetMicrousd30d: Number(costBudgetMicrousd30d)
  };
  if (Object.values(payload).some((value) => !Number.isInteger(value) || value < 0)) {
    apiKeys.error = 'API key budgets must be non-negative whole numbers';
    return;
  }

  apiKeys.error = '';
  try {
    const response = await requestJSON(`/api/admin/keys/${keyId}/budgets`, {
      method: 'PUT',
      body: JSON.stringify(payload)
    });
    if (!isCurrentAuthenticated(version)) return;
    apiKeys.items = apiKeys.items.map((key) => (key.id === keyId ? { ...key, ...response.key } : key));
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    apiKeys.error = error instanceof Error ? error.message : 'Failed to update key budgets';
  }
}

/**
 * @param {number} keyId
 * @param {string | number | null | undefined} routingPoolId
 */
export async function updateAPIKeyRoutingPool(keyId, routingPoolId) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  const poolId = Number(routingPoolId ?? 0);
  if (!Number.isInteger(poolId) || poolId < 0) {
    apiKeys.error = 'Routing pool selection is invalid';
    return;
  }

  apiKeys.error = '';
  try {
    const response = await requestJSON(`/api/admin/keys/${keyId}/routing-pool`, {
      method: 'PUT',
      body: JSON.stringify({ routingPoolId: poolId > 0 ? poolId : null })
    });
    if (!isCurrentAuthenticated(version)) return;
    apiKeys.items = apiKeys.items.map((key) => (key.id === keyId ? { ...key, ...response.key } : key));
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    apiKeys.error = error instanceof Error ? error.message : 'Failed to update key routing pool';
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
  if (modelRoutingPreview.routingPoolId !== '0') {
    params.set('routingPoolId', modelRoutingPreview.routingPoolId);
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

  try {
    const payload = await requestJSON('/api/admin/model-settings', {
      method: 'PUT',
      body: JSON.stringify({
        defaultModel: modelSettings.defaultModel
      })
    });
    if (!isCurrentAuthenticated(version)) return;
    modelSettings.defaultModel = payload.defaultModel ?? '';
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
    const params = new URLSearchParams({ limit: '50' });
    const requestId = requestLogs.requestId.trim();
    if (requestId) {
      params.set('requestId', requestId);
    }
    const query = requestLogs.query.trim();
    if (query) {
      params.set('q', query);
    }
    if (requestLogs.statusClass && requestLogs.statusClass !== 'all') {
      params.set('statusClass', requestLogs.statusClass);
    }
    const statusCode = requestLogs.statusCode.trim();
    if (/^[1-5]\d\d$/.test(statusCode)) {
      params.set('statusCode', statusCode);
    }
    const since = requestLogs.since.trim();
    if (/^\d+$/.test(since)) {
      params.set('since', since);
    }
    if (requestLogs.providerAccountId && requestLogs.providerAccountId !== 'all') {
      params.set('providerAccountId', requestLogs.providerAccountId);
    }
    if (requestLogs.routingPoolId && requestLogs.routingPoolId !== 'all') {
      params.set('routingPoolId', requestLogs.routingPoolId);
    }
    if (requestLogs.clientKeyId && requestLogs.clientKeyId !== 'all') {
      params.set('clientKeyId', requestLogs.clientKeyId);
    }
    const model = requestLogs.model.trim();
    if (model) {
      params.set('model', model);
    }
    const sessionId = requestLogs.sessionId.trim();
    if (sessionId) {
      params.set('sessionId', sessionId);
    }
    const errorCode = requestLogs.errorCode.trim();
    if (errorCode) {
      params.set('error', errorCode);
    }
    if (requestLogs.usageSource && requestLogs.usageSource !== 'all') {
      params.set('usageSource', requestLogs.usageSource);
    }
    if (requestLogs.routingPoolError && requestLogs.routingPoolError !== 'all') {
      params.set('routingPoolError', requestLogs.routingPoolError);
    }
    const routingPoolChain = requestLogs.routingPoolChain.trim();
    if (routingPoolChain) {
      params.set('routingPoolChain', routingPoolChain);
    }
    if (requestLogs.gatewayFallbacks) {
      params.set('gatewayFallbacks', '1');
    }
    const payload = await requestJSON(`/api/admin/request-logs?${params.toString()}`);
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
 * @param {URLSearchParams} params
 * @returns {Promise<{ events?: SystemEvent[], nextCursor?: string, hasMore?: boolean }>}
 */
async function requestSystemEventsPage(params) {
  const response = await fetch(`/api/admin/system-events?${params.toString()}`);
  if (!response.ok) {
    const payload = await response.json().catch(() => ({}));
    const error = new Error(payload.error ?? `Request failed with ${response.status}`);
    // @ts-expect-error Attach the status so an expired keyset cursor can recover.
    error.status = response.status;
    throw error;
  }
  return response.json();
}

/**
 * @param {{ append?: boolean }} [options]
 */
export async function loadSystemEvents(options = {}) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  let append = options.append === true;
  if (append && (!systemEvents.hasMore || !systemEvents.nextCursor || systemEvents.loading || systemEvents.loadingOlder)) return;
  const requestSequence = ++systemEventRequestSequence;

  if (append) {
    systemEvents.loadingOlder = true;
  } else {
    systemEvents.loading = true;
  }
  systemEvents.error = '';

  const params = new URLSearchParams({ limit: '50' });
  const values = [
    ['q', systemEvents.query],
    ['action', systemEvents.action],
    ['actor', systemEvents.actor],
    ['targetType', systemEvents.targetType],
    ['targetId', systemEvents.targetId]
  ];
  for (const [key, value] of values) {
    const trimmed = value.trim();
    if (trimmed) params.set(key, trimmed);
  }
  if (/^\d+$/.test(systemEvents.since)) params.set('since', systemEvents.since);
  if (systemEvents.category !== 'all') params.set('category', systemEvents.category);
  if (systemEvents.outcome !== 'all') params.set('outcome', systemEvents.outcome);
  if (systemEvents.severity !== 'all') params.set('severity', systemEvents.severity);
  if (append) params.set('cursor', systemEvents.nextCursor);

  try {
    let payload;
    try {
      payload = await requestSystemEventsPage(params);
    } catch (error) {
      // Retention can invalidate an old cursor. Recover with the current filters.
      // @ts-expect-error Status is attached by requestSystemEventsPage.
      if (!append || error?.status !== 400) throw error;
      append = false;
      params.delete('cursor');
      payload = await requestSystemEventsPage(params);
    }
    if (!isCurrentAuthenticated(version) || requestSequence !== systemEventRequestSequence) return;

    const events = Array.isArray(payload.events) ? payload.events : [];
    if (append) {
      const existingIds = new Set(systemEvents.items.map((event) => String(event.id)));
      systemEvents.items = [
        ...systemEvents.items,
        ...events.filter((event) => !existingIds.has(String(event.id)))
      ];
    } else {
      systemEvents.items = events;
    }
    systemEvents.nextCursor = payload.nextCursor ?? '';
    systemEvents.hasMore = payload.hasMore === true;
  } catch (error) {
    if (!isCurrentAuthenticated(version) || requestSequence !== systemEventRequestSequence) return;
    systemEvents.error = error instanceof Error ? error.message : 'Failed to load system logs';
  } finally {
    if (!isCurrentAuthenticated(version) || requestSequence !== systemEventRequestSequence) return;
    systemEvents.loading = false;
    systemEvents.loadingOlder = false;
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
      outputMicrousdPerMillion: Number(price?.outputMicrousdPerMillion ?? 0),
      longInputMicrousdPerMillion: Number(price?.longInputMicrousdPerMillion ?? 0),
      longCachedInputMicrousdPerMillion: Number(price?.longCachedInputMicrousdPerMillion ?? 0),
      longOutputMicrousdPerMillion: Number(price?.longOutputMicrousdPerMillion ?? 0)
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
  return savePricingRows();
}

/**
 * Save current usagePricing.rows via PUT, then reload from the server.
 * @returns {Promise<boolean>}
 */
export async function savePricingRows() {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return false;

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
      outputMicrousdPerMillion: Number(row.outputMicrousdPerMillion) || 0,
      longInputMicrousdPerMillion: Number(row.longInputMicrousdPerMillion) || 0,
      longCachedInputMicrousdPerMillion: Number(row.longCachedInputMicrousdPerMillion) || 0,
      longOutputMicrousdPerMillion: Number(row.longOutputMicrousdPerMillion) || 0
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
    if (!isCurrentAuthenticated(version)) return false;
    usagePricing.version = payload.version ?? 1;
    usagePricing.currency = payload.currency ?? 'USD';
    usagePricing.unit = payload.unit ?? '1M_tokens';
    usagePricing.saved = true;
    await loadUsagePricing();
    await loadUsageSummary(usage.range, usage.groupBy);
    return true;
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return false;
    usagePricing.error = error instanceof Error ? error.message : 'Failed to save usage pricing';
    return false;
  } finally {
    if (!isCurrentAuthenticated(version)) return false;
    usagePricing.saving = false;
  }
}

/**
 * Sync official OpenAI pricing into the usage pricing table.
 * On success merges official prices and returns lifecycle notices.
 * @returns {Promise<boolean>}
 */
export async function syncOfficialUsagePricing() {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return false;

  usagePricing.syncing = true;
  usagePricing.error = '';
  usagePricing.saved = false;
  usagePricing.syncMessage = '';
  usagePricing.removalMessage = '';

  try {
    const payload = await requestJSON('/api/admin/usage-pricing/sync-official', { method: 'POST' });
    if (!isCurrentAuthenticated(version)) return false;
    usagePricing.version = payload.pricing?.version ?? 1;
    usagePricing.currency = payload.pricing?.currency ?? 'USD';
    usagePricing.unit = payload.pricing?.unit ?? '1M_tokens';
    usagePricing.rows = Object.entries(payload.pricing?.models ?? {}).map(([model, price]) => ({
      model,
      inputMicrousdPerMillion: Number(price?.inputMicrousdPerMillion ?? 0),
      cachedInputMicrousdPerMillion: Number(price?.cachedInputMicrousdPerMillion ?? 0),
      outputMicrousdPerMillion: Number(price?.outputMicrousdPerMillion ?? 0),
      longInputMicrousdPerMillion: Number(price?.longInputMicrousdPerMillion ?? 0),
      longCachedInputMicrousdPerMillion: Number(price?.longCachedInputMicrousdPerMillion ?? 0),
      longOutputMicrousdPerMillion: Number(price?.longOutputMicrousdPerMillion ?? 0)
    }));
    usagePricing.upcomingShutdowns = Array.isArray(payload.synced?.upcomingShutdowns) ? payload.synced.upcomingShutdowns : [];
    usagePricing.deletionCandidates = Array.isArray(payload.synced?.deletionCandidates) ? payload.synced.deletionCandidates : [];
    const added = Array.isArray(payload.synced?.added) ? payload.synced.added.length : 0;
    const updated = Array.isArray(payload.synced?.updated) ? payload.synced.updated.length : 0;
    usagePricing.syncMessage = `Official pricing synced: ${added} added, ${updated} updated.`;
    await loadUsagePricing();
    await loadUsageSummary(usage.range, usage.groupBy);
    return true;
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return false;
    usagePricing.error = error instanceof Error ? error.message : 'Failed to sync official pricing';
    return false;
  } finally {
    if (!isCurrentAuthenticated(version)) return false;
    usagePricing.syncing = false;
  }
}

/** @param {string[]} models */
export async function removeShutdownUsagePricing(models) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version) || !Array.isArray(models) || models.length === 0) return false;

  usagePricing.removingShutdown = true;
  usagePricing.error = '';
  usagePricing.saved = false;
  usagePricing.removalMessage = '';

  try {
    const payload = await requestJSON('/api/admin/usage-pricing/remove-shutdown', {
      method: 'POST',
      body: JSON.stringify({ models })
    });
    if (!isCurrentAuthenticated(version)) return false;
    const removed = Array.isArray(payload.removed) ? payload.removed : [];
    usagePricing.deletionCandidates = usagePricing.deletionCandidates.filter((item) => !removed.includes(item.model));
    usagePricing.removalMessage = `Removed ${removed.length} shut-down model${removed.length === 1 ? '' : 's'}.`;
    await loadUsagePricing();
    await loadUsageSummary(usage.range, usage.groupBy);
    return true;
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return false;
    usagePricing.error = error instanceof Error ? error.message : 'Failed to remove shut-down models';
    return false;
  } finally {
    if (!isCurrentAuthenticated(version)) return false;
    usagePricing.removingShutdown = false;
  }
}

/** @param {string[]} models */
export async function ignoreUpcomingUsagePricing(models) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version) || !Array.isArray(models) || models.length === 0) return false;

  usagePricing.ignoringUpcoming = true;
  usagePricing.error = '';
  usagePricing.saved = false;
  usagePricing.removalMessage = '';

  try {
    const payload = await requestJSON('/api/admin/usage-pricing/ignore-upcoming', {
      method: 'POST',
      body: JSON.stringify({ models })
    });
    if (!isCurrentAuthenticated(version)) return false;
    const ignored = Array.isArray(payload.ignored) ? payload.ignored : [];
    usagePricing.upcomingShutdowns = usagePricing.upcomingShutdowns.filter((item) => !ignored.includes(item.model));
    usagePricing.removalMessage = `Ignored ${ignored.length} upcoming-shutdown model${ignored.length === 1 ? '' : 's'}.`;
    await loadUsagePricing();
    await loadUsageSummary(usage.range, usage.groupBy);
    return true;
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return false;
    usagePricing.error = error instanceof Error ? error.message : 'Failed to ignore upcoming-shutdown models';
    return false;
  } finally {
    if (!isCurrentAuthenticated(version)) return false;
    usagePricing.ignoringUpcoming = false;
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
    const routingPoolId = Number(apiKeys.newKeyRoutingPoolId ?? 0);
    if (!Number.isInteger(routingPoolId) || routingPoolId < 0) {
      apiKeys.error = 'Routing pool selection is invalid';
      return;
    }
    const payload = await requestJSON('/api/admin/keys', {
      method: 'POST',
      body: JSON.stringify({
        name: apiKeys.newKeyName,
        routingPoolId: routingPoolId > 0 ? routingPoolId : null
      })
    });
    if (!isCurrentAuthenticated(version)) return;
    apiKeys.items = [payload.key, ...apiKeys.items];
    apiKeys.oneTimeSecret = payload.secret;
    apiKeys.newKeyName = '';
    apiKeys.newKeyRoutingPoolId = 0;
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

/** @param {number} id */
export async function deleteRevokedKey(id) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  apiKeys.error = '';

  try {
    await requestJSON(`/api/admin/keys/${id}`, { method: 'DELETE' });
    if (!isCurrentAuthenticated(version)) return;
    apiKeys.items = apiKeys.items.filter((key) => key.id !== id);
    delete selectedAPIKeyIds[String(id)];
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    apiKeys.error = error instanceof Error ? error.message : 'Failed to permanently delete API key';
  }
}



/**
 * @param {number | string} id
 * @param {boolean} selected
 */
export function toggleAPIKeySelection(id, selected) {
  const key = String(id);
  if (!key || key === '0') return;
  if (selected) {
    selectedAPIKeyIds[key] = true;
    return;
  }
  delete selectedAPIKeyIds[key];
}

export function clearAPIKeySelection() {
  for (const id of Object.keys(selectedAPIKeyIds)) {
    delete selectedAPIKeyIds[id];
  }
}

/**
 * @param {number[]} ids
 * @param {boolean} selected
 */
export function setAPIKeySelection(ids, selected) {
  for (const id of ids) {
    toggleAPIKeySelection(id, selected);
  }
}

function selectedEditableAPIKeyIDs() {
  const selected = new Set(Object.keys(selectedAPIKeyIds).map((id) => Number(id)));
  return apiKeys.items
    .filter((key) => selected.has(key.id) && !key.revokedAt)
    .map((key) => key.id);
}

function selectedRevokedAPIKeyIDs() {
  const selected = new Set(Object.keys(selectedAPIKeyIds).map((id) => Number(id)));
  return apiKeys.items
    .filter((key) => selected.has(key.id) && Boolean(key.revokedAt))
    .map((key) => key.id);
}

/**
 * @param {number[]} ids
 * @param {(id: number) => Promise<void>} action
 * @param {string} [emptyMessage]
 */
async function runAPIKeyBatch(ids, action, emptyMessage = 'Select at least one active or disabled API key') {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return false;
  if (ids.length === 0) {
    apiKeys.error = emptyMessage;
    return false;
  }

  apiKeys.saving = true;
  apiKeys.error = '';
  try {
    for (const id of ids) {
      if (!isCurrentAuthenticated(version)) return false;
      await action(id);
      if (!isCurrentAuthenticated(version)) return false;
      if (apiKeys.error) return false;
      delete selectedAPIKeyIds[String(id)];
    }
    return true;
  } finally {
    if (isCurrentAuthenticated(version)) {
      apiKeys.saving = false;
    }
  }
}

/**
 * @param {boolean} disabled
 */
export async function bulkSetSelectedAPIKeysDisabled(disabled) {
  return runAPIKeyBatch(selectedEditableAPIKeyIDs(), async (id) => {
    await setAPIKeyDisabled(id, disabled);
  });
}

export async function bulkRevokeSelectedAPIKeys() {
  return runAPIKeyBatch(selectedEditableAPIKeyIDs(), async (id) => {
    await revokeKey(id);
  });
}

export async function bulkDeleteSelectedRevokedAPIKeys() {
  return runAPIKeyBatch(
    selectedRevokedAPIKeyIDs(),
    async (id) => {
      await deleteRevokedKey(id);
    },
    'Select at least one deleted API key'
  );
}

/**
 * @param {{
 *   applyStatus?: boolean,
 *   targetDisabled?: boolean,
 *   applyModelPolicy?: boolean,
 *   targetModelPolicy?: string,
 *   targetModelsText?: string,
 *   applyRoutingPool?: boolean,
 *   targetRoutingPoolId?: number,
 *   applyLimits?: boolean,
 *   targetRequestsPerMinute?: string | number,
 *   targetTokensPerMinute?: string | number,
 *   applyBudgets?: boolean,
 *   targetRequestBudget24h?: string | number,
 *   targetTokenBudget24h?: string | number,
 *   targetCostBudgetMicrousd24h?: string | number,
 *   targetRequestBudget30d?: string | number,
 *   targetTokenBudget30d?: string | number,
 *   targetCostBudgetMicrousd30d?: string | number,
 * }} input
 */
export async function bulkUpdateSelectedAPIKeys(input) {
  const editableIds = selectedEditableAPIKeyIDs();
  if (editableIds.length === 0) {
    apiKeys.error = 'Select at least one active or disabled API key';
    return false;
  }

  const hasPatch =
    input.applyStatus === true ||
    input.applyModelPolicy === true ||
    input.applyRoutingPool === true ||
    input.applyLimits === true ||
    input.applyBudgets === true;

  if (!hasPatch) {
    apiKeys.error = 'Choose at least one bulk edit section';
    return false;
  }

  return runAPIKeyBatch(editableIds, async (id) => {
    const key = apiKeys.items.find((k) => k.id === id);
    if (!key || key.revokedAt) {
      apiKeys.error = 'Selected API key no longer exists';
      return;
    }
    if (input.applyStatus === true) {
      await setAPIKeyDisabled(id, Boolean(input.targetDisabled));
      if (apiKeys.error) return;
    }

    if (input.applyModelPolicy === true) {
      await updateAPIKeyModelPolicy(
        id,
        String(input.targetModelPolicy ?? 'all'),
        String(input.targetModelsText ?? '')
      );
      if (apiKeys.error) return;
    }

    if (input.applyRoutingPool === true) {
      await updateAPIKeyRoutingPool(id, input.targetRoutingPoolId ?? null);
      if (apiKeys.error) return;
    }

    if (input.applyLimits === true) {
      const ref = apiKeys.items.find((k) => k.id === id);
      const rpm = input.targetRequestsPerMinute !== undefined && input.targetRequestsPerMinute !== ''
        ? Number(input.targetRequestsPerMinute)
        : Number(ref?.requestsPerMinute ?? 0);
      const tpm = input.targetTokensPerMinute !== undefined && input.targetTokensPerMinute !== ''
        ? Number(input.targetTokensPerMinute)
        : Number(ref?.tokensPerMinute ?? 0);
      await updateAPIKeyLimits(id, rpm, tpm);
      if (apiKeys.error) return;
    }

    if (input.applyBudgets === true) {
      const ref = apiKeys.items.find((k) => k.id === id) ?? null;
      const req24 = input.targetRequestBudget24h !== undefined && input.targetRequestBudget24h !== ''
        ? Number(input.targetRequestBudget24h)
        : Number(ref?.requestBudget24h ?? 0);
      const tok24 = input.targetTokenBudget24h !== undefined && input.targetTokenBudget24h !== ''
        ? Number(input.targetTokenBudget24h)
        : Number(ref?.tokenBudget24h ?? 0);
      const cost24 = input.targetCostBudgetMicrousd24h !== undefined && input.targetCostBudgetMicrousd24h !== ''
        ? Number(input.targetCostBudgetMicrousd24h)
        : Number(ref?.costBudgetMicrousd24h ?? 0);
      const req30 = input.targetRequestBudget30d !== undefined && input.targetRequestBudget30d !== ''
        ? Number(input.targetRequestBudget30d)
        : Number(ref?.requestBudget30d ?? 0);
      const tok30 = input.targetTokenBudget30d !== undefined && input.targetTokenBudget30d !== ''
        ? Number(input.targetTokenBudget30d)
        : Number(ref?.tokenBudget30d ?? 0);
      const cost30 = input.targetCostBudgetMicrousd30d !== undefined && input.targetCostBudgetMicrousd30d !== ''
        ? Number(input.targetCostBudgetMicrousd30d)
        : Number(ref?.costBudgetMicrousd30d ?? 0);
      await updateAPIKeyBudgets(id, req24, tok24, cost24, req30, tok30, cost30);
    }
  });
}

export function initializeAdminState() {
  loadHealth();
  loadSession();
}

// --- Ops monitoring state ---

/** @type {{ loading: boolean; error: string; stats: any; throughput: any; errorTrend: any; latency: any; accountHealth: any; accountTests: any; costBreakdown: any }} */
export const opsMonitor = $state({
  loading: false,
  error: '',
  stats: null,
  throughput: null,
  errorTrend: null,
  latency: null,
  accountHealth: null,
  accountTests: null,
  costBreakdown: null,
});

/** @param {number} sinceSeconds */
export async function loadOpsErrorStats(sinceSeconds) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return false;

  try {
    const params = new URLSearchParams();
    if (sinceSeconds > 0) params.set('since', String(sinceSeconds));
    opsMonitor.stats = await requestJSON(`/api/admin/ops/error-stats?${params.toString()}`);
    return true;
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return false;
    opsMonitor.error = error instanceof Error ? error.message : 'Failed to load ops error stats';
    return false;
  }
}

/** @param {number} sinceSeconds @param {string} interval */
export async function loadOpsThroughputTrend(sinceSeconds, interval) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return false;

  try {
    const params = new URLSearchParams();
    if (sinceSeconds > 0) params.set('since', String(sinceSeconds));
    if (interval) params.set('interval', interval);
    opsMonitor.throughput = await requestJSON(`/api/admin/ops/throughput-trend?${params.toString()}`);
    return true;
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return false;
    opsMonitor.error = error instanceof Error ? error.message : 'Failed to load ops throughput trend';
    return false;
  }
}

/** @param {number} sinceSeconds @param {string} interval */
export async function loadOpsErrorTrend(sinceSeconds, interval) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return false;

  try {
    const params = new URLSearchParams();
    if (sinceSeconds > 0) params.set('since', String(sinceSeconds));
    if (interval) params.set('interval', interval);
    opsMonitor.errorTrend = await requestJSON(`/api/admin/ops/error-trend?${params.toString()}`);
    return true;
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return false;
    opsMonitor.error = error instanceof Error ? error.message : 'Failed to load ops error trend';
    return false;
  }
}

/** @param {number} sinceSeconds */
export async function loadOpsLatencyDistribution(sinceSeconds) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return false;

  try {
    const params = new URLSearchParams();
    if (sinceSeconds > 0) params.set('since', String(sinceSeconds));
    opsMonitor.latency = await requestJSON(`/api/admin/ops/latency-distribution?${params.toString()}`);
    return true;
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return false;
    opsMonitor.error = error instanceof Error ? error.message : 'Failed to load ops latency distribution';
    return false;
  }
}

/** @param {number} sinceSeconds */
export async function loadOpsAccountHealth(sinceSeconds) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return false;

  try {
    const params = new URLSearchParams();
    if (sinceSeconds > 0) params.set('since', String(sinceSeconds));
    opsMonitor.accountHealth = await requestJSON(`/api/admin/ops/account-health?${params.toString()}`);
    return true;
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return false;
    opsMonitor.error = error instanceof Error ? error.message : 'Failed to load ops account health';
    return false;
  }
}

/** @param {number} sinceSeconds */
export async function loadOpsAccountTests(sinceSeconds) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return false;

  try {
    const params = new URLSearchParams({ limit: '20' });
    if (sinceSeconds > 0) params.set('since', String(sinceSeconds));
    opsMonitor.accountTests = await requestJSON(`/api/admin/ops/account-tests?${params.toString()}`);
    return true;
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return false;
    opsMonitor.error = error instanceof Error ? error.message : 'Failed to load ops account tests';
    return false;
  }
}

/** @param {number} sinceSeconds */
export async function loadOpsCostBreakdown(sinceSeconds) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return false;

  try {
    const params = new URLSearchParams();
    if (sinceSeconds > 0) params.set('since', String(sinceSeconds));
    opsMonitor.costBreakdown = await requestJSON(`/api/admin/ops/cost-breakdown?${params.toString()}`);
    return true;
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return false;
    opsMonitor.error = error instanceof Error ? error.message : 'Failed to load ops cost breakdown';
    return false;
  }
}

/** @param {number} rangeSeconds */
export async function loadOpsDashboard(rangeSeconds) {
  const since = rangeSeconds ? Math.floor(Date.now() / 1000) - rangeSeconds : 0;
  opsMonitor.loading = true;
  opsMonitor.error = '';
  try {
    const results = await Promise.all([
      loadOpsErrorStats(since),
      loadOpsThroughputTrend(since, 'hour'),
      loadOpsErrorTrend(since, 'hour'),
      loadOpsLatencyDistribution(since),
      loadOpsAccountHealth(since),
      loadOpsAccountTests(since),
      loadOpsCostBreakdown(since),
    ]);
    const loaded = results.every(Boolean);
    if (!loaded && !opsMonitor.error) {
      opsMonitor.error = 'Failed to load complete ops dashboard';
    }
    return loaded;
  } finally {
    opsMonitor.loading = false;
  }
}

// --- Fingerprint profile state ---

/** @type {{ loading: boolean; error: string; items: any[]; saving: boolean }} */
export const fingerprintProfiles = $state({
  loading: false,
  error: '',
  items: [],
  saving: false,
});

export async function loadFingerprintProfiles() {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  fingerprintProfiles.loading = true;
  fingerprintProfiles.error = '';
  try {
    const data = await requestJSON('/api/admin/fingerprint-profiles');
    if (!isCurrentAuthenticated(version)) return;
    fingerprintProfiles.items = data.profiles ?? [];
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    fingerprintProfiles.error = error instanceof Error ? error.message : 'Failed to load fingerprint profiles';
  } finally {
    if (!isCurrentAuthenticated(version)) return;
    fingerprintProfiles.loading = false;
  }
}

/** @param {{name: string; description: string; userAgent: string; tlsFingerprint: string; headers: Record<string,string>; enabled: boolean}} input */
export async function createFingerprintProfile(input) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return false;

  fingerprintProfiles.saving = true;
  fingerprintProfiles.error = '';
  try {
    await requestJSON('/api/admin/fingerprint-profiles', {
      method: 'POST',
      body: JSON.stringify(input),
    });
    if (!isCurrentAuthenticated(version)) return false;
    await loadFingerprintProfiles();
    return true;
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return false;
    fingerprintProfiles.error = error instanceof Error ? error.message : 'Failed to create fingerprint profile';
    return false;
  } finally {
    if (!isCurrentAuthenticated(version)) return;
    fingerprintProfiles.saving = false;
  }
}

/** @param {number} id @param {any} input */
export async function updateFingerprintProfile(id, input) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return false;

  fingerprintProfiles.saving = true;
  fingerprintProfiles.error = '';
  try {
    await requestJSON(`/api/admin/fingerprint-profiles/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(input),
    });
    if (!isCurrentAuthenticated(version)) return false;
    await loadFingerprintProfiles();
    return true;
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return false;
    fingerprintProfiles.error = error instanceof Error ? error.message : 'Failed to update fingerprint profile';
    return false;
  } finally {
    if (!isCurrentAuthenticated(version)) return;
    fingerprintProfiles.saving = false;
  }
}

/** @param {number} id */
export async function deleteFingerprintProfile(id) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return false;

  fingerprintProfiles.error = '';
  try {
    await requestJSON(`/api/admin/fingerprint-profiles/${id}`, { method: 'DELETE' });
    if (!isCurrentAuthenticated(version)) return false;
    await loadFingerprintProfiles();
    return true;
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return false;
    fingerprintProfiles.error = error instanceof Error ? error.message : 'Failed to delete fingerprint profile';
    return false;
  }
}

// --- Error passthrough rules state ---
/** @type {{ loading: boolean; error: string; items: any[]; saving: boolean }} */
export const errorPassthroughRules = $state({
  loading: false,
  error: '',
  items: [],
  saving: false,
});

export async function loadErrorPassthroughRules() {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;
  errorPassthroughRules.loading = true;
  errorPassthroughRules.error = '';
  try {
    const data = await requestJSON('/api/admin/error-passthrough-rules');
    if (!isCurrentAuthenticated(version)) return;
    errorPassthroughRules.items = data.rules ?? [];
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return;
    errorPassthroughRules.error = error instanceof Error ? error.message : 'Failed to load error passthrough rules';
  } finally {
    if (!isCurrentAuthenticated(version)) return;
    errorPassthroughRules.loading = false;
  }
}

/** @param {{pattern: string; matchType: string; description: string; enabled: boolean; priority: number}} input */
export async function createErrorPassthroughRule(input) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return false;
  errorPassthroughRules.saving = true;
  errorPassthroughRules.error = '';
  try {
    await requestJSON('/api/admin/error-passthrough-rules', { method: 'POST', body: JSON.stringify(input) });
    if (!isCurrentAuthenticated(version)) return false;
    await loadErrorPassthroughRules();
    return true;
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return false;
    errorPassthroughRules.error = error instanceof Error ? error.message : 'Failed to create error passthrough rule';
    return false;
  } finally {
    if (!isCurrentAuthenticated(version)) return;
    errorPassthroughRules.saving = false;
  }
}

/** @param {number} id @param {any} input */
export async function updateErrorPassthroughRule(id, input) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return false;
  errorPassthroughRules.saving = true;
  errorPassthroughRules.error = '';
  try {
    await requestJSON(`/api/admin/error-passthrough-rules/${id}`, { method: 'PATCH', body: JSON.stringify(input) });
    if (!isCurrentAuthenticated(version)) return false;
    await loadErrorPassthroughRules();
    return true;
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return false;
    errorPassthroughRules.error = error instanceof Error ? error.message : 'Failed to update error passthrough rule';
    return false;
  } finally {
    if (!isCurrentAuthenticated(version)) return;
    errorPassthroughRules.saving = false;
  }
}

/** @param {number} id */
export async function deleteErrorPassthroughRule(id) {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return false;
  errorPassthroughRules.error = '';
  try {
    await requestJSON(`/api/admin/error-passthrough-rules/${id}`, { method: 'DELETE' });
    if (!isCurrentAuthenticated(version)) return false;
    await loadErrorPassthroughRules();
    return true;
  } catch (error) {
    if (!isCurrentAuthenticated(version)) return false;
    errorPassthroughRules.error = error instanceof Error ? error.message : 'Failed to delete error passthrough rule';
    return false;
  }
}
