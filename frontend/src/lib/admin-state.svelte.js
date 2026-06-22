import { copyText } from '$lib/clipboard.js';

/**
 * @typedef {object} APIKey
 * @property {number} id
 * @property {string} name
 * @property {string} prefix
 * @property {string} createdAt
 * @property {string | null} lastUsedAt
 * @property {string | null} revokedAt
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
 * @property {string} subject
 * @property {string} name
 * @property {string} displayName
 * @property {boolean} enabled
 * @property {number} priority
 * @property {string} status
 * @property {string} statusReason
 * @property {string | null} circuitOpenUntil
 * @property {string | null} rateLimitedUntil
 * @property {string | null} accessTokenExpiresAt
 * @property {string | null} lastRefreshAt
 * @property {string | null} lastUsedAt
 * @property {string} lastError
 * @property {string | null} lastErrorAt
 */

/**
 * @typedef {object} RequestLog
 * @property {number} id
 * @property {string} requestId
 * @property {string} clientKey
 * @property {string} provider
 * @property {string} route
 * @property {string} method
 * @property {number} statusCode
 * @property {number} latencyMs
 * @property {string} error
 * @property {string} createdAt
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
 * @typedef {object} ModelRoutingModel
 * @property {string} model
 * @property {boolean} allowed
 * @property {number} configuredCount
 * @property {number} enabledCount
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
export const providerOAuth = $state({ authorizationUrl: '', callbackUrl: '', completing: false, copied: false });
/** @type {{ loading: boolean, creating: boolean, error: string, items: APIKey[], newKeyName: string, oneTimeSecret: string }} */
export const apiKeys = $state({
  loading: false,
  creating: false,
  error: '',
  items: [],
  newKeyName: '',
  oneTimeSecret: ''
});
/** @type {{ loading: boolean, error: string, items: RequestLog[] }} */
export const requestLogs = $state({
  loading: false,
  error: '',
  items: []
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
/** @type {{ loading: boolean, error: string, defaultModel: string, allowedModels: string[], models: ModelRoutingModel[], warnings: string[] }} */
export const modelRouting = $state({
  loading: false,
  error: '',
  defaultModel: '',
  allowedModels: [],
  models: [],
  warnings: []
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

function clearRequestLogs() {
  replaceState(requestLogs, {
    loading: false,
    error: '',
    items: []
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
      clearRequestLogs();
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
    await loadKeys();
    await loadRequestLogs();
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
    clearRequestLogs();
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
  clearRequestLogs();
  loginForm.password = '';
}

export async function loadProvider() {
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  provider.loading = true;
  provider.error = '';

  try {
    const payload = await requestJSON('/api/admin/providers/openai');
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
    const payload = await requestJSON('/api/admin/providers/openai/accounts');
    if (!isCurrentAuthenticated(version)) return;
    providerAccounts.items = payload.accounts ?? [];
    pruneAccountModelStates(
      accountModels,
      providerAccounts.items.map((account) => account.id)
    );
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

/** @param {AccountModel[]} models */
function accountModelsText(models) {
  return '';
}

/** @param {number} accountId */
export function getAccountModelsState(accountId) {
  return ensureAccountModelsState(accountId);
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
    const payload = await requestJSON(`/api/admin/providers/openai/accounts/${accountId}/models`);
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
    const payload = await requestJSON(`/api/admin/providers/openai/accounts/${accountId}/models`, {
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
    const payload = await requestJSON('/api/admin/providers/openai/connect', {
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
    await requestJSON('/api/admin/providers/openai/callback', {
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
 * @param {Partial<Pick<ProviderAccount, 'enabled' | 'priority'>>} patch
 */
export async function updateProviderAccount(account, patch) {
  const version = sessionVersion;
  providerAccounts.saving = true;
  providerAccounts.error = '';
  try {
    await requestJSON(`/api/admin/providers/openai/accounts/${account.id}`, {
      method: 'PATCH',
      body: JSON.stringify(patch)
    });
    if (!isCurrentAuthenticated(version)) return;
    await loadProviderAccounts();
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

/** @param {ProviderAccount} account */
export async function refreshProviderAccount(account) {
  const version = sessionVersion;
  providerAccounts.saving = true;
  providerAccounts.error = '';
  try {
    await requestJSON(`/api/admin/providers/openai/accounts/${account.id}/refresh`, {
      method: 'POST'
    });
    if (!isCurrentAuthenticated(version)) return;
    await loadProvider();
    await loadProviderAccounts();
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
export async function disconnectProviderAccount(account) {
  const version = sessionVersion;
  providerAccounts.saving = true;
  providerAccounts.error = '';
  try {
    await requestJSON(`/api/admin/providers/openai/accounts/${account.id}/disconnect`, {
      method: 'POST'
    });
    if (!isCurrentAuthenticated(version)) return;
    await loadProvider();
    await loadProviderAccounts();
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

/** @param {SubmitEvent} event */
export async function saveModelSettings(event) {
  event.preventDefault();
  const version = sessionVersion;
  if (!isCurrentAuthenticated(version)) return;

  modelSettings.saving = true;
  modelSettings.error = '';
  modelSettings.saved = false;

  const allowedModels = modelSettings.allowedModelsText
    .split('\n')
    .map((model) => model.trim())
    .filter(Boolean);

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
