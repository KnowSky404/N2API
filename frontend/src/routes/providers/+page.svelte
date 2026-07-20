<script>
  import { page } from '$app/state';
  import { FlaskConical, LoaderCircle, Pencil, Plus, RefreshCw, Trash2, X } from 'lucide-svelte';
  import {
    accountLabel,
    accountTypeLabel,
    completeProviderCallback,
    connectProvider,
    copyAuthorizationURL,
    createAPIUpstreamAccount,
    disconnectProviderAccount,
    formatDate,
    getAccountModelsState,
    getAccountTestResultsState,
    getProviderStateLabel,
    loadAccountModels,
    loadProviderAccounts,
    apiUpstreamForm,
    provider,
    providerAccounts,
    providerConnectForm,
    providerOAuth,
    pauseProviderAccount,
    providerAccountEffectiveStatus,
    refreshProviderAccount,
    removeAccountModel,
    resetProviderAccountStatus,
    saveAccountModels,
    accountModelSummary,
    accountModelsText,
    session,
    sourceBadgeLabel,
    syncAccountModels,
    setAccountModelEnabled,
    statusLabel,
    testAllProviderAccounts,
    testProviderAccount,
    testProviderAccountModel,
    toggleAccountTestHistory,
    updateProviderAccount,
    fingerprintProfiles,
    loadFingerprintProfiles
  } from '$lib/admin-state.svelte.js';

  import AuthGate from '$lib/AuthGate.svelte';
  import { runModelTestsWithConcurrency } from '$lib/model-test-queue.js';
  const DEFAULT_CODEX_FINGERPRINT_SYSTEM_KEY = 'codex_cli_default';
  let accountSearch = $state('');
  let accountStatusFilter = $state('all');
  let accountTypeFilter = $state('all');
  let accountEnabledFilter = $state('all');
  let accountSort = $state({ key: 'account', direction: 'asc' });
  let accountPage = $state(1);
  let accountPageSize = $state(5);
  let addAccountModalOpen = $state(false);
  /** @type {'oauth' | 'api_upstream'} */
  let addAccountModalTab = $state('oauth');
  let fingerprintProfilesRequested = $state(false);
  let appliedProviderAccountSearch = $state('');
  let editingProviderAccountId = $state(0);
  let editingProviderAccountDraft = $state(/** @type {{ name: string, enabled: boolean, priority: number, loadFactor: number, maxConcurrentRequests: number, fingerprintProfileId: number, baseUrl: string, proxyUrl: string, apiKey: string, modelsText: string, modelItems: import('$lib/admin-state.svelte.js').AccountModel[], syncModelsOnSave: boolean } | null} */ (null));
  let editingProviderAccountError = $state('');
  let deletingProviderAccountId = $state(0);
  let modelTestAccountId = $state(0);
  let modelTestSearch = $state('');
  let modelTestEnabledFilter = $state('all');
  let modelTestStatusFilter = $state('all');
  /** @type {Record<string, boolean>} */
  let selectedModelTests = $state({});
  /** @type {Record<string, { status: string, errorCode: string, httpStatus: number, latencyMs: number, message: string, checkedAt: string }>} */
  let modelTestRuns = $state({});
  let modelTestRunActive = $state(false);
  let recoveryTestAccountId = $state(0);
  let recoveryNotice = $state(/** @type {{ kind: 'success' | 'warning' | 'error', title: string, message: string } | null} */ (null));

  const providerStateLabel = $derived(getProviderStateLabel());
  const editingProviderAccount = $derived(
    providerAccounts.items.find((account) => account.id === editingProviderAccountId) ?? null
  );
  const deletingProviderAccount = $derived(
    providerAccounts.items.find((account) => account.id === deletingProviderAccountId) ?? null
  );
  const defaultCodexFingerprintProfile = $derived(
    fingerprintProfiles.items.find((profile) => profile.systemKey === DEFAULT_CODEX_FINGERPRINT_SYSTEM_KEY) ?? null
  );
  const oauthFingerprintProfiles = $derived(
    fingerprintProfiles.items.filter((profile) => profile.systemKey !== DEFAULT_CODEX_FINGERPRINT_SYSTEM_KEY)
  );
  const modelTestAccount = $derived(
    providerAccounts.items.find((account) => account.id === modelTestAccountId) ?? null
  );
  const modelTestModels = $derived(
    modelTestAccount ? getAccountModelsState(modelTestAccount.id).items : []
  );
  const filteredModelTestModels = $derived(
    modelTestModels.filter((model) => {
      const query = modelTestSearch.trim().toLowerCase();
      if (query && !model.model.toLowerCase().includes(query)) return false;
      if (modelTestEnabledFilter === 'enabled' && !model.enabled) return false;
      if (modelTestEnabledFilter === 'disabled' && model.enabled) return false;
      if (modelTestStatusFilter === 'not_tested' && model.lastTestStatus) return false;
      if (modelTestStatusFilter === 'passed' && model.lastTestStatus !== 'passed') return false;
      if (modelTestStatusFilter === 'failed' && model.lastTestStatus !== 'failed') return false;
      return true;
    })
  );
  const selectedModelTestCount = $derived(Object.keys(selectedModelTests).length);
  const filteredSelectedModelTestCount = $derived(
    filteredModelTestModels.filter((model) => selectedModelTests[model.model]).length
  );
  const allFilteredModelTestsSelected = $derived(
    filteredModelTestModels.length > 0 && filteredSelectedModelTestCount === filteredModelTestModels.length
  );

  $effect(() => {
    const defaultProfileID = Number(defaultCodexFingerprintProfile?.id ?? 0);
    if (
      defaultProfileID > 0 &&
      editingProviderAccount &&
      editingProviderAccountDraft &&
      isCodexOAuthAccount(editingProviderAccount) &&
      Number(editingProviderAccountDraft.fingerprintProfileId) === defaultProfileID
    ) {
      editingProviderAccountDraft.fingerprintProfileId = 0;
    }
  });
  const someFilteredModelTestsSelected = $derived(
    filteredSelectedModelTestCount > 0 && !allFilteredModelTestsSelected
  );
  const filteredProviderAccounts = $derived(
    sortProviderAccounts(
      providerAccounts.items.filter((account) => {
        if (!accountMatchesTypeFilter(account, accountTypeFilter)) return false;
        if (!accountMatchesStatusFilter(account, accountStatusFilter)) return false;
        if (!accountMatchesEnabledFilter(account, accountEnabledFilter)) return false;
        const query = accountSearch.trim().toLowerCase();
        if (!query) return true;
        if (/^id:[1-9]\d*$/.test(query)) {
          const idQuery = query.slice(3);
          return String(account.id) === idQuery;
        }
        return (account.name ?? '').toLowerCase().includes(query);
      })
    )
  );
  const accountPageCount = $derived(Math.max(1, Math.ceil(filteredProviderAccounts.length / accountPageSize)));
  const normalizedAccountPage = $derived(Math.min(Math.max(accountPage, 1), accountPageCount));
  const paginatedProviderAccounts = $derived(
    filteredProviderAccounts.slice((normalizedAccountPage - 1) * accountPageSize, normalizedAccountPage * accountPageSize)
  );
  const providerAccountPageSummary = $derived(
    filteredProviderAccounts.length === 0
      ? '0'
      : `${(normalizedAccountPage - 1) * accountPageSize + 1}-${(normalizedAccountPage - 1) * accountPageSize + paginatedProviderAccounts.length}`
  );

  /** @param {string} search */
  function applyProviderAccountURLFilters(search) {
    const params = new URLSearchParams(search);
    const providerAccountId = params.get('providerAccountId') ?? '';
    const status = params.get('status') ?? '';
    const type = params.get('type') ?? '';
    const enabled = params.get('enabled') ?? '';
    accountSearch = '';
    accountStatusFilter = 'all';
    accountTypeFilter = 'all';
    accountEnabledFilter = 'all';
    if (/^[1-9]\d*$/.test(providerAccountId)) {
      accountSearch = `id:${providerAccountId}`;
    }
    if (['all', 'active', 'blocked', 'rate_limited', 'circuit_open', 'expired'].includes(status)) {
      accountStatusFilter = status;
    }
    // backward compat: old combined status values map to new independent filters
    if (!type || type === 'all') {
      if (status === 'disabled') { accountEnabledFilter = 'disabled'; accountStatusFilter = 'all'; }
      if (status === 'api_upstream') { accountTypeFilter = 'api_upstream'; accountStatusFilter = 'all'; }
      if (status === 'codex_oauth') { accountTypeFilter = 'codex_oauth'; accountStatusFilter = 'all'; }
    }
    if (['all', 'api_upstream', 'codex_oauth'].includes(type)) {
      accountTypeFilter = type;
    }
    if (['all', 'enabled', 'disabled'].includes(enabled)) {
      accountEnabledFilter = enabled;
    }
  }


  /**
   * @param {string | null | undefined} model
   * @param {import('$lib/admin-state.svelte.js').ProviderAccount} account
   */
  function modelRoutingHref(model, account) {
    const value = String(model ?? '').trim();
    if (!value) return '';
    return `/models?model=${encodeURIComponent(value)}&providerAccountId=${encodeURIComponent(String(account.id))}`;
  }

  $effect(() => {
    if (!session.authenticated) {
      fingerprintProfilesRequested = false;
      appliedProviderAccountSearch = '';
      return;
    }
    if (appliedProviderAccountSearch !== page.url.search) {
      appliedProviderAccountSearch = page.url.search;
      applyProviderAccountURLFilters(page.url.search);
    }
    if (!fingerprintProfilesRequested) {
      fingerprintProfilesRequested = true;
      void loadFingerprintProfiles();
    }
  });

  $effect(() => {
    if (accountPage > accountPageCount) {
      accountPage = accountPageCount;
    } else if (accountPage < 1) {
      accountPage = 1;
    }
  });

  /**
   * @param {import('$lib/admin-state.svelte.js').ProviderAccount} account
   * @param {string} filter
   */
  function accountMatchesStatusFilter(account, filter) {
    const status = providerAccountEffectiveStatus(account);
    if (filter === 'active') return status === 'active';
    if (filter === 'blocked') return status !== 'active';
    if (filter === 'rate_limited') return status === 'rate_limited';
    if (filter === 'circuit_open') return status === 'circuit_open';
    if (filter === 'expired') return status === 'expired';
    return true;
  }

  /**
   * @param {import('$lib/admin-state.svelte.js').ProviderAccount} account
   * @param {string} filter
   */
  function accountMatchesTypeFilter(account, filter) {
    if (filter === 'api_upstream') return account.accountType === 'api_upstream';
    if (filter === 'codex_oauth') return isCodexOAuthAccount(account);
    return true;
  }

  /**
   * @param {import('$lib/admin-state.svelte.js').ProviderAccount} account
   * @param {string} filter
   */
  function accountMatchesEnabledFilter(account, filter) {
    if (filter === 'enabled') return account.enabled;
    if (filter === 'disabled') return !account.enabled;
    return true;
  }

  /**
   * @param {import('$lib/admin-state.svelte.js').ProviderAccount[]} accounts
   */
  function sortProviderAccounts(accounts) {
    return [...accounts].sort((left, right) => {
      const leftValue = accountSortValue(left, accountSort.key);
      const rightValue = accountSortValue(right, accountSort.key);
      const result =
        typeof leftValue === 'number' && typeof rightValue === 'number'
          ? leftValue - rightValue
          : String(leftValue).localeCompare(String(rightValue), undefined, { numeric: true, sensitivity: 'base' });
      return accountSort.direction === 'asc' ? result : -result;
    });
  }

  /**
   * @param {import('$lib/admin-state.svelte.js').ProviderAccount} account
   * @param {string} key
   */
  function accountSortValue(account, key) {
    if (key === 'account') return accountLabel(account);
    if (key === 'type') return accountTypeLabel(account);
    if (key === 'status') return statusLabel(providerAccountEffectiveStatus(account));
    if (key === 'enabled') return account.enabled ? 0 : 1;
    if (key === 'refresh') return Date.parse(account.lastRefreshAt ?? '') || 0;
    if (key === 'used') return Date.parse(account.lastUsedAt ?? '') || 0;
    return '';
  }

  /** @param {string} key */
  function setProviderAccountSort(key) {
    accountSort =
      accountSort.key === key
        ? { key, direction: accountSort.direction === 'asc' ? 'desc' : 'asc' }
        : { key, direction: 'asc' };
    accountPage = 1;
  }

  /** @param {number} page */
  function goToProviderAccountPage(page) {
    accountPage = Math.min(Math.max(page, 1), accountPageCount);
  }

  /** @param {string} key */
  function providerAccountSortDirection(key) {
    if (accountSort.key !== key) return 'none';
    return accountSort.direction === 'asc' ? 'ascending' : 'descending';
  }

  /** @param {string} key */
  function sortIndicator(key) {
    if (accountSort.key !== key) return '';
    return accountSort.direction === 'asc' ? ' Asc' : ' Desc';
  }

  /** @param {number | null | undefined} value */
  function concurrencyLimitLabel(value) {
    const limit = Number(value ?? 0);
    return limit > 0 ? String(limit) : 'unlimited';
  }

  /** @param {import('$lib/admin-state.svelte.js').ProviderAccount} account */
  function isCodexOAuthAccount(account) {
    return account.accountType === 'codex_oauth' || !account.accountType;
  }

  /** @param {import('$lib/admin-state.svelte.js').ProviderAccount} account */
  function accountEmailLabel(account) {
    if (!isCodexOAuthAccount(account)) return '';
    return account.displayName?.includes('@') ? account.displayName : account.subject || '';
  }

  /** @param {import('$lib/admin-state.svelte.js').ProviderAccount} account */
  function accountHoverDetail(account) {
    return [accountLabel(account), accountEmailLabel(account)].filter(Boolean).join('\n');
  }

  /** @param {import('$lib/admin-state.svelte.js').ProviderAccount} account */
  function openAccountEditor(account) {
    const modelState = getAccountModelsState(account.id);
    modelState.saved = false;
    modelState.error = '';
    editingProviderAccountDraft = createAccountDraft(account, modelState);
    editingProviderAccountError = '';
    providerAccounts.error = '';
    editingProviderAccountId = account.id;
    deletingProviderAccountId = 0;
    modelTestAccountId = 0;
  }

  function closeAccountEditor() {
    const modelState = editingProviderAccountId ? getAccountModelsState(editingProviderAccountId) : null;
    if (providerAccounts.saving || modelState?.saving || modelState?.syncing) return;
    editingProviderAccountId = 0;
    editingProviderAccountDraft = null;
    editingProviderAccountError = '';
  }

  /**
   * @param {import('$lib/admin-state.svelte.js').ProviderAccount} account
   * @param {ReturnType<typeof getAccountModelsState>} modelState
   */
  function createAccountDraft(account, modelState) {
    const fingerprintProfileID = Number(account.fingerprintProfileId ?? 0);
    const defaultProfileID = Number(defaultCodexFingerprintProfile?.id ?? 0);
    return {
      name: accountLabel(account),
      enabled: Boolean(account.enabled),
      priority: Number(account.priority ?? 0),
      loadFactor: Number(account.loadFactor || 1),
      maxConcurrentRequests: Number(account.maxConcurrentRequests || 0),
      fingerprintProfileId:
        isCodexOAuthAccount(account) && defaultProfileID > 0 && fingerprintProfileID === defaultProfileID
          ? 0
          : fingerprintProfileID,
      baseUrl: account.baseUrl || '',
      proxyUrl: account.proxyUrlSummary || '',
      apiKey: '',
      modelsText: modelState.text,
      modelItems: modelState.items.map((item) => ({ ...item })),
      syncModelsOnSave: false
    };
  }

  /**
   * @param {import('$lib/admin-state.svelte.js').ProviderAccount} account
   * @param {number} selectedProfileID
   */
  function resolveFingerprintProfileID(account, selectedProfileID) {
    const profileID = Number(selectedProfileID) || 0;
    if (profileID > 0) return profileID;
    if (isCodexOAuthAccount(account)) {
      return Number(defaultCodexFingerprintProfile?.id ?? 0) || null;
    }
    return null;
  }

  /** @param {number} accountId */
  function refreshAccountDraft(accountId) {
    const account = providerAccounts.items.find((item) => item.id === accountId);
    if (!account || editingProviderAccountId !== accountId) return;
    editingProviderAccountDraft = createAccountDraft(account, getAccountModelsState(accountId));
  }

  function addAccountBusy() {
    return provider.connecting || providerOAuth.completing || apiUpstreamForm.submitting;
  }

  function resetAddAccountDraft() {
    Object.assign(providerConnectForm, {
      name: '',
      priority: 100,
      enabled: true,
      fingerprintProfileId: '0'
    });
    Object.assign(providerOAuth, {
      authorizationUrl: '',
      callbackUrl: '',
      completing: false,
      copied: false
    });
    Object.assign(apiUpstreamForm, {
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
    provider.error = '';
    providerAccounts.error = '';
  }

  function openAddAccountModal() {
    resetAddAccountDraft();
    addAccountModalTab = 'oauth';
    addAccountModalOpen = true;
  }

  function closeAddAccountModal() {
    if (addAccountBusy()) return;
    addAccountModalOpen = false;
    resetAddAccountDraft();
  }

  /** @param {SubmitEvent} event */
  async function saveAddAccount(event) {
    event.preventDefault();
    if (addAccountModalTab === 'api_upstream') {
      await createAPIUpstreamAccount();
      return;
    }
    if (providerOAuth.authorizationUrl) {
      await completeProviderCallback();
      return;
    }
    await connectProvider();
  }

  function addAccountSaveLabel() {
    if (addAccountModalTab === 'api_upstream') return apiUpstreamForm.submitting ? 'Saving' : 'Save';
    if (providerOAuth.authorizationUrl) return providerOAuth.completing ? 'Saving' : 'Save';
    return provider.connecting ? 'Saving' : 'Save';
  }

  function addAccountSaveDisabled() {
    if (addAccountModalTab === 'api_upstream') return apiUpstreamForm.submitting;
    if (providerOAuth.authorizationUrl) return providerOAuth.completing || !providerOAuth.callbackUrl.trim();
    return provider.loading || !provider.data?.configured || provider.connecting;
  }

  async function saveEditingProviderAccount() {
    const account = editingProviderAccount;
    const draft = editingProviderAccountDraft;
    if (!account || !draft || providerAccounts.saving) return;

    editingProviderAccountError = '';
    providerAccounts.error = '';
    const name = draft.name.trim();
    if (!name) {
      editingProviderAccountError = 'Account name cannot be empty';
      return;
    }
    if (!Number.isInteger(Number(draft.priority)) || Number(draft.priority) < 0) {
      editingProviderAccountError = 'Priority must be a non-negative whole number';
      return;
    }
    if (!Number.isInteger(Number(draft.loadFactor)) || Number(draft.loadFactor) < 1 || Number(draft.loadFactor) > 100) {
      editingProviderAccountError = 'Load factor must be a whole number from 1 to 100';
      return;
    }
    if (!Number.isInteger(Number(draft.maxConcurrentRequests)) || Number(draft.maxConcurrentRequests) < 0) {
      editingProviderAccountError = 'Max concurrency must be a non-negative whole number';
      return;
    }

    await updateProviderAccount(account, {
      name,
      enabled: draft.enabled,
      priority: Number(draft.priority),
      loadFactor: Number(draft.loadFactor),
      maxConcurrentRequests: Number(draft.maxConcurrentRequests),
      fingerprintProfileId: resolveFingerprintProfileID(account, draft.fingerprintProfileId)
    });
    if (providerAccounts.error) {
      editingProviderAccountError = providerAccounts.error;
      refreshAccountDraft(account.id);
      return;
    }

    /** @type {{ baseUrl?: string, apiKey?: string, proxyUrl?: string }} */
    const credentialPatch = {};
    if (draft.proxyUrl.trim() !== (account.proxyUrlSummary || '').trim()) {
      credentialPatch.proxyUrl = draft.proxyUrl.trim();
    }
    if (account.accountType === 'api_upstream') {
      if (draft.baseUrl.trim() !== (account.baseUrl || '').trim()) {
        credentialPatch.baseUrl = draft.baseUrl.trim();
      }
      if (draft.apiKey.trim()) credentialPatch.apiKey = draft.apiKey.trim();
    }
    if (Object.keys(credentialPatch).length > 0) {
      await updateProviderAccount(account, credentialPatch);
      if (providerAccounts.error) {
        editingProviderAccountError = `Account settings were saved, but credentials failed: ${providerAccounts.error}`;
        refreshAccountDraft(account.id);
        return;
      }
    }

    const modelState = getAccountModelsState(account.id);
    if (draft.syncModelsOnSave && account.accountType === 'api_upstream') {
      await syncAccountModels(account.id);
      if (modelState.syncError) {
        editingProviderAccountError = `Account settings were saved, but model sync failed: ${modelState.syncError}`;
        refreshAccountDraft(account.id);
        return;
      }
      draft.modelItems = [
        ...modelState.items.filter((item) => item.source === 'upstream').map((item) => ({ ...item })),
        ...draft.modelItems.filter((item) => item.source !== 'upstream').map((item) => ({ ...item }))
      ];
    }
    const priorModelItems = modelState.items.map((item) => ({ ...item }));
    const priorModelsText = modelState.text;
    modelState.items = draft.modelItems.map((item) => ({ ...item }));
    await saveAccountModels(account.id, draft.modelsText);
    if (modelState.error) {
      modelState.items = priorModelItems;
      modelState.text = priorModelsText;
      editingProviderAccountError = `Account settings were saved, but models failed: ${modelState.error}`;
      refreshAccountDraft(account.id);
      return;
    }
    refreshAccountDraft(account.id);
  }

  /** @param {import('$lib/admin-state.svelte.js').ProviderAccount} account */
  function toggleDeleteConfirmation(account) {
    providerAccounts.error = '';
    editingProviderAccountId = 0;
    modelTestAccountId = 0;
    deletingProviderAccountId = deletingProviderAccountId === account.id ? 0 : account.id;
  }

  /** @param {import('$lib/admin-state.svelte.js').ProviderAccount} account */
  function openModelTests(account) {
    editingProviderAccountId = 0;
    deletingProviderAccountId = 0;
    modelTestAccountId = account.id;
    modelTestSearch = '';
    modelTestEnabledFilter = 'all';
    modelTestStatusFilter = 'all';
    selectedModelTests = {};
    modelTestRuns = {};
    void loadAccountModels(account.id);
  }

  function closeModelTests() {
    modelTestAccountId = 0;
  }

  /** @param {string} model */
  function toggleModelTestSelection(model) {
    if (selectedModelTests[model]) {
      delete selectedModelTests[model];
      return;
    }
    selectedModelTests[model] = true;
  }

  function toggleFilteredModelTestSelection() {
    if (allFilteredModelTestsSelected) {
      for (const model of filteredModelTestModels) {
        delete selectedModelTests[model.model];
      }
      return;
    }
    for (const model of filteredModelTestModels) {
      selectedModelTests[model.model] = true;
    }
  }

  function clearModelTestSelection() {
    selectedModelTests = {};
  }

  /** @param {import('$lib/admin-state.svelte.js').AccountModel} model */
  function displayedModelTestStatus(model) {
    return modelTestRuns[model.model]?.status || model.lastTestStatus || 'not_tested';
  }

  /** @param {import('$lib/admin-state.svelte.js').AccountModel} model */
  function displayedModelTestLatency(model) {
    const run = modelTestRuns[model.model];
    return run?.latencyMs ?? model.lastTestLatencyMs ?? 0;
  }

  /** @param {import('$lib/admin-state.svelte.js').AccountModel} model */
  function displayedModelTestCheckedAt(model) {
    return modelTestRuns[model.model]?.checkedAt || model.lastTestAt || null;
  }

  /** @param {import('$lib/admin-state.svelte.js').AccountModel} model */
  function modelTestDetail(model) {
    const run = modelTestRuns[model.model];
    const httpStatus = run?.httpStatus ?? model.lastTestHttpStatus ?? 0;
    return [
      httpStatus > 0 ? `HTTP ${httpStatus}` : '',
      run?.errorCode || '',
      run?.message || model.lastError || ''
    ].filter(Boolean).join(' · ');
  }

  /** @param {string} status */
  function modelTestStatusLabel(status) {
    if (status === 'not_tested') return 'Not tested';
    return status.charAt(0).toUpperCase() + status.slice(1);
  }

  /** @param {string} status */
  function modelTestStatusClass(status) {
    if (status === 'passed') return 'bg-[#e8f5f0] text-[#0a7a5e]';
    if (status === 'failed' || status === 'timeout') return 'bg-red-50 text-red-700';
    if (status === 'queued' || status === 'testing') return 'bg-blue-50 text-blue-700';
    return 'bg-[#f5f5f5] text-[#6e6e6e]';
  }

  /**
   * @param {number} accountId
   * @param {string} model
   */
  async function executeModelTest(accountId, model) {
    modelTestRuns[model] = {
      status: 'testing',
      errorCode: '',
      httpStatus: 0,
      latencyMs: 0,
      message: '',
      checkedAt: ''
    };
    try {
      const result = await testProviderAccountModel(accountId, model);
      modelTestRuns[model] = {
        ...result,
        status: result.status === 'failed' && result.errorCode === 'timeout' ? 'timeout' : result.status
      };
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Model test failed';
      modelTestRuns[model] = {
        status: message.toLowerCase().includes('timeout') ? 'timeout' : 'failed',
        errorCode: '',
        httpStatus: 0,
        latencyMs: 0,
        message,
        checkedAt: new Date().toISOString()
      };
    }
  }

  /**
   * @param {number} accountId
   * @param {string[]} models
   */
  async function runModelTestQueue(accountId, models) {
    if (modelTestRunActive || models.length === 0) return;
    modelTestRunActive = true;
    for (const model of models) {
      modelTestRuns[model] = {
        status: 'queued',
        errorCode: '',
        httpStatus: 0,
        latencyMs: 0,
        message: '',
        checkedAt: ''
      };
    }

    try {
      await runModelTestsWithConcurrency(models, (model) => executeModelTest(accountId, model), 3);
    } finally {
      modelTestRunActive = false;
    }
  }

  function testSelectedModels() {
    if (!modelTestAccount) return;
    const models = modelTestModels
      .filter((model) => selectedModelTests[model.model])
      .map((model) => model.model);
    void runModelTestQueue(modelTestAccount.id, models);
  }

  /** @param {string} model */
  function testOneModel(model) {
    if (!modelTestAccount) return;
    void runModelTestQueue(modelTestAccount.id, [model]);
  }

  /** @param {import('$lib/admin-state.svelte.js').ProviderAccount} account */
  async function confirmDisconnectProviderAccount(account) {
    await disconnectProviderAccount(account);
    if (!providerAccounts.error) deletingProviderAccountId = 0;
  }

  /** @param {import('$lib/admin-state.svelte.js').ProviderAccount} account */
  function statusHoverDetail(account) {
    if (account.status === 'rate_limited' && account.rateLimitedUntil) {
      if (providerAccountEffectiveStatus(account) === 'active') {
        return `Rate limit elapsed at ${formatDate(account.rateLimitedUntil)}. Test recovery to confirm upstream health.`;
      }
      return `Rate limited until ${formatDate(account.rateLimitedUntil)}`;
    }
    if (account.status === 'circuit_open' && account.circuitOpenUntil && providerAccountEffectiveStatus(account) === 'active') {
      return `Circuit window elapsed at ${formatDate(account.circuitOpenUntil)}. Test recovery to confirm upstream health.`;
    }
    return statusLabel(account.status);
  }

  /** @param {import('$lib/admin-state.svelte.js').ProviderAccount} account */
  async function testAccountRecovery(account) {
    if (!isCodexOAuthAccount(account) || providerAccounts.saving || recoveryTestAccountId) return;
    recoveryTestAccountId = account.id;
    recoveryNotice = null;
    const tested = await testProviderAccount(account);
    if (providerAccounts.error || !tested) {
      recoveryNotice = {
        kind: 'error',
        title: 'Recovery test failed',
        message: providerAccounts.error || `${accountLabel(account)} could not be tested.`
      };
    } else if (tested.status === 'active') {
      recoveryNotice = {
        kind: 'success',
        title: 'Recovery confirmed',
        message: `${accountLabel(account)} is active and its local failure state was cleared.`
      };
    } else {
      recoveryNotice = {
        kind: 'warning',
        title: 'Recovery not confirmed',
        message: tested.statusReason || tested.lastError || `${accountLabel(account)} is still ${statusLabel(tested.status)}.`
      };
    }
    recoveryTestAccountId = 0;
  }

  /** @param {import('$lib/admin-state.svelte.js').AccountModel[]} models */
  function enabledAccountModelCount(models) {
    return models.filter((model) => model.enabled).length;
  }

  /** @param {string | null | undefined} status */
  function testResultStatusClass(status) {
    if (status === 'passed') return 'bg-[#e8f5f0] text-[#0a7a5e]';
    if (status === 'failed') return 'bg-amber-50 text-amber-700';
    return 'bg-[#f5f5f5] text-[#6e6e6e]';
  }
</script>

<svelte:head>
  <title>N2API Providers</title>
</svelte:head>

<AuthGate>
{#if recoveryNotice}
  <div
    class={[
      'fixed right-4 top-4 z-[70] flex w-[min(24rem,calc(100vw-2rem))] items-start gap-3 rounded-lg border bg-white p-4 shadow-lg',
      recoveryNotice.kind === 'success'
        ? 'border-emerald-200'
        : recoveryNotice.kind === 'warning'
          ? 'border-amber-200'
          : 'border-red-200'
    ]}
    role={recoveryNotice.kind === 'error' ? 'alert' : 'status'}
    aria-live="polite"
  >
    <div class="min-w-0 flex-1">
      <p class="text-sm font-semibold text-[#0d0d0d]">{recoveryNotice.title}</p>
      <p class="mt-1 text-sm leading-5 text-[#6e6e6e]">{recoveryNotice.message}</p>
    </div>
    <button
      class="ui-button ui-button--icon inline-flex size-7 shrink-0 items-center justify-center rounded-md text-[#6e6e6e] hover:bg-[#f5f5f5] hover:text-[#0d0d0d]"
      type="button"
      onclick={() => { recoveryNotice = null; }}
      title="Dismiss notification"
      aria-label="Dismiss notification"
    >
      <X class="size-4" aria-hidden="true" />
    </button>
  </div>
{/if}
<section class="ui-page min-w-0">
  <header class="ui-page-header">
    <div class="ui-page-heading">
      <h1 class="ui-page-title">Provider accounts</h1>
      <p class="ui-page-description">Codex OAuth and API upstream gateway exits. Last refresh: {formatDate(provider.data?.lastRefreshAt)}.</p>
    </div>
    <div class="ui-page-actions">
    <span
class={[
  'inline-flex items-center gap-2 text-xs font-medium capitalize',
  provider.data?.connected
    ? 'text-[#0a7a5e]'
    : provider.data?.configured
      ? 'text-[#3c3c3c]'
      : 'text-red-700'
]}
    >
      <span class={provider.data?.connected ? 'size-2 rounded-full bg-[#10a37f]' : 'size-2 rounded-full bg-amber-500'}></span>
{provider.loading ? 'Checking' : providerStateLabel}
    </span>
      <button
        class="ui-button ui-button--sm ui-button--primary rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white hover:bg-[#1f2933] disabled:cursor-not-allowed disabled:opacity-60"
        type="button"
        onclick={openAddAccountModal}
      >
        <Plus class="size-4" aria-hidden="true" />
        Add account
      </button>
<button
  class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
  type="button"
  disabled={providerAccounts.loading || providerAccounts.saving || providerAccounts.items.length === 0}
  onclick={testAllProviderAccounts}
>
  <FlaskConical class="size-4" aria-hidden="true" />
  Test all accounts
</button>
<button
  class="ui-button ui-button--icon ui-button--secondary rounded-lg border border-[#e5e5e5] bg-white text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
  type="button"
  disabled={providerAccounts.loading}
  onclick={loadProviderAccounts}
  aria-label={providerAccounts.loading ? 'Refreshing provider accounts' : 'Refresh provider accounts'}
  title="Refresh provider accounts"
>
  <RefreshCw class={providerAccounts.loading ? 'size-4 animate-spin' : 'size-4'} aria-hidden="true" />
</button>
    </div>
  </header>

  <!-- Add account modal -->
  {#if addAccountModalOpen}
    <!-- svelte-ignore a11y_click_events_have_key_events,a11y_no_static_element_interactions,a11y_interactive_supports_focus -->
    <div class="ui-modal-backdrop ui-modal-backdrop--top fixed inset-0 z-50 flex items-start justify-center bg-black/30 pt-[10vh] overflow-y-auto" role="dialog" aria-modal="true" aria-label="Add account">
      <div class="ui-modal-panel ui-modal-panel--lg ui-modal-panel--flush w-full max-w-xl rounded-xl border border-[#ededed] bg-white shadow-[0_4px_16px_rgba(13,13,13,0.06)]">
        <!-- Header -->
        <div class="flex items-center justify-between border-b border-[#ededed] px-6 py-4">
          <h2 class="text-lg font-semibold text-[#0d0d0d]">Add account</h2>
          <button
            class="ui-button ui-button--icon ui-button--secondary inline-flex size-8 items-center justify-center rounded-md border border-[#e5e5e5] bg-white text-[#6e6e6e] hover:bg-[#f5f5f5] hover:text-[#0d0d0d]"
            type="button"
            disabled={addAccountBusy()}
            onclick={closeAddAccountModal}
            aria-label="Close add account modal"
            title="Close"
          >
            <X class="size-4" aria-hidden="true" />
          </button>
        </div>

        <!-- Tabs -->
        <div class="flex border-b border-[#ededed]">
          <button
            class="ui-button ui-button--md flex-1 px-6 py-3 text-sm font-medium transition-colors {addAccountModalTab === 'oauth' ? 'border-b-2 border-[#10a37f] text-[#10a37f]' : 'text-[#6e6e6e] hover:text-[#3c3c3c]'}"
            type="button"
            onclick={() => { addAccountModalTab = 'oauth'; }}
          >
            OAuth account
          </button>
          <button
            class="ui-button ui-button--md flex-1 px-6 py-3 text-sm font-medium transition-colors {addAccountModalTab === 'api_upstream' ? 'border-b-2 border-[#10a37f] text-[#10a37f]' : 'text-[#6e6e6e] hover:text-[#3c3c3c]'}"
            type="button"
            onclick={() => { addAccountModalTab = 'api_upstream'; }}
          >
            API upstream
          </button>
        </div>

        <!-- OAuth Tab -->
        {#if addAccountModalTab === 'oauth'}
          <div class="p-6">
            <p class="text-sm text-[#6e6e6e]">Generate a Codex OAuth authorization link to connect a new account.</p>

            <form
              id="add-account-oauth-generate-form"
              class="mt-4 grid gap-4"
              onsubmit={saveAddAccount}
            >
              <div class="grid gap-4 sm:grid-cols-2">
                <label class="grid min-w-0 gap-1 text-sm font-medium text-[#3c3c3c]">
                  Account name
                  <input
                    class="w-full min-w-0 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                    type="text"
                    placeholder="Work Codex"
                    bind:value={providerConnectForm.name}
                  />
                </label>
                <label class="grid min-w-0 gap-1 text-sm font-medium text-[#3c3c3c]">
                  Priority
                  <input
                    class="w-full min-w-0 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                    type="number"
                    min="0"
                    step="1"
                    bind:value={providerConnectForm.priority}
                  />
                </label>
              </div>
              <label class="grid min-w-0 gap-1 text-sm font-medium text-[#3c3c3c]">
                Fingerprint profile
                <select
                  class="w-full min-w-0 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                  bind:value={providerConnectForm.fingerprintProfileId}
                >
                  <option value="0">Default Codex CLI</option>
                  {#each oauthFingerprintProfiles as fp}
                    <option value={String(fp.id)}>{fp.name}</option>
                  {/each}
                </select>
              </label>
              <label class="inline-flex items-center gap-2 text-sm font-medium text-[#3c3c3c]">
                <input
                  class="size-4 shrink-0 rounded border-[#e5e5e5] text-[#10a37f] focus:ring-[#10a37f]"
                  type="checkbox"
                  bind:checked={providerConnectForm.enabled}
                />
                Enable after login
              </label>

              <!-- Authorization URL (appears after generation) -->
              {#if providerOAuth.authorizationUrl}
                <div class="rounded-lg border border-[#cbe7dd] bg-[#e8f5f0] p-4">
                  <div class="flex flex-wrap items-center justify-between gap-3">
                    <p class="text-sm font-medium text-[#0a7a5e]">OAuth authorization link</p>
                    <button
                      class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#b7d9cd] bg-white px-3 py-1.5 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]"
                      type="button"
                      onclick={copyAuthorizationURL}
                    >
                      {providerOAuth.copied ? 'Copied' : 'Copy'}
                    </button>
                  </div>
                  <code class="mt-3 block overflow-x-auto rounded-md bg-white px-3 py-2 font-mono text-[13px] leading-6 text-[#0d0d0d] break-all">
                    {providerOAuth.authorizationUrl}
                  </code>
                </div>
              {/if}
            </form>

            <!-- Callback completion (appears after OAuth flow) -->
            {#if providerOAuth.authorizationUrl}
              <form
                id="add-account-oauth-complete-form"
                class="mt-4 grid gap-3"
                onsubmit={saveAddAccount}
              >
                <label class="grid min-w-0 gap-1 text-sm font-medium text-[#3c3c3c]">
                  Callback URL
                  <input
                    class="w-full min-w-0 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] leading-6 text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                    type="url"
                    placeholder="http://localhost:1455/auth/callback?code=...&state=..."
                    bind:value={providerOAuth.callbackUrl}
                    required
                  />
                </label>
              </form>
            {/if}
            {#if provider.error}
              <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">{provider.error}</p>
            {/if}
          </div>

        <!-- API Upstream Tab -->
        {:else}
          <div class="p-6">
            <p class="text-sm text-[#6e6e6e]">Add an OpenAI-compatible upstream by API key and base URL.</p>

            <form
              id="add-account-api-upstream-form"
              class="mt-4 grid gap-4"
              onsubmit={saveAddAccount}
            >
              <div class="grid gap-4 sm:grid-cols-2">
                <label class="grid min-w-0 gap-1 text-sm font-medium text-[#3c3c3c]">
                  Name
                  <input
                    class="w-full min-w-0 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                    type="text"
                    placeholder="OpenAI API"
                    bind:value={apiUpstreamForm.name}
                    required
                  />
                </label>
                <label class="grid min-w-0 gap-1 text-sm font-medium text-[#3c3c3c]">
                  Base URL
                  <input
                    class="w-full min-w-0 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                    type="url"
                    placeholder="https://api.openai.com/v1"
                    bind:value={apiUpstreamForm.baseUrl}
                    required
                  />
                </label>
              </div>
              <p class="text-xs text-[#6e6e6e] -mt-2">HTTPS is required unless HTTP upstreams are explicitly enabled in the server environment.</p>

              <div class="grid gap-4 sm:grid-cols-2">
                <label class="grid min-w-0 gap-1 text-sm font-medium text-[#3c3c3c]">
                  API key
                  <input
                    class="w-full min-w-0 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                    type="password"
                    autocomplete="off"
                    bind:value={apiUpstreamForm.apiKey}
                    required
                  />
                </label>
                <label class="grid min-w-0 gap-1 text-sm font-medium text-[#3c3c3c]">
                  Proxy URL
                  <input
                    class="w-full min-w-0 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                    type="url"
                    placeholder="http://proxy.example.test:8080"
                    bind:value={apiUpstreamForm.proxyUrl}
                  />
                </label>
              </div>
              <p class="text-xs text-[#6e6e6e] -mt-2">Optional HTTP or HTTPS outbound proxy for this account.</p>

              <div class="grid gap-4 sm:grid-cols-2">
                <label class="grid min-w-0 gap-1 text-sm font-medium text-[#3c3c3c]">
                  Priority
                  <input
                    class="w-full min-w-0 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                    type="number"
                    min="0"
                    step="1"
                    bind:value={apiUpstreamForm.priority}
                  />
                </label>
                <label class="grid min-w-0 gap-1 text-sm font-medium text-[#3c3c3c]">
                  Load factor
                  <input
                    class="w-full min-w-0 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                    type="number"
                    min="1"
                    max="100"
                    step="1"
                    bind:value={apiUpstreamForm.loadFactor}
                  />
                </label>
              </div>

              <label class="grid min-w-0 gap-1 text-sm font-medium text-[#3c3c3c]">
                Fingerprint profile
                <select
                  class="w-full min-w-0 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                  bind:value={apiUpstreamForm.fingerprintProfileId}
                >
                  <option value="0">Default API upstream (pass-through)</option>
                  {#each fingerprintProfiles.items as fp}
                    <option value={String(fp.id)}>{fp.name}</option>
                  {/each}
                </select>
              </label>

              <label class="inline-flex items-center gap-2 text-sm font-medium text-[#3c3c3c]">
                <input
                  class="size-4 shrink-0 rounded border-[#e5e5e5] text-[#10a37f] focus:ring-[#10a37f]"
                  type="checkbox"
                  bind:checked={apiUpstreamForm.enabled}
                />
                Enabled
              </label>

              <label class="grid min-w-0 gap-1 text-sm font-medium text-[#3c3c3c]">
                Manual models
                <textarea
                  class="min-h-20 w-full min-w-0 resize-y rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] leading-5 text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                  placeholder={'gpt-4.1\ngpt-4.1-mini'}
                  bind:value={apiUpstreamForm.modelsText}
                ></textarea>
              </label>

              {#if apiUpstreamForm.error}
                <p class="rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">
                  {apiUpstreamForm.error}
                </p>
              {/if}
            </form>
          </div>
        {/if}
        <div class="flex items-center justify-end gap-3 border-t border-[#ededed] px-6 py-4">
          <button
            class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#e5e5e5] bg-white px-4 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]"
            type="button"
            disabled={addAccountBusy()}
            onclick={closeAddAccountModal}
          >
            Cancel
          </button>
          <button
            class="ui-button ui-button--sm ui-button--primary rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white disabled:cursor-not-allowed disabled:opacity-60"
            type="submit"
            form={addAccountModalTab === 'api_upstream'
              ? 'add-account-api-upstream-form'
              : providerOAuth.authorizationUrl
                ? 'add-account-oauth-complete-form'
                : 'add-account-oauth-generate-form'}
            disabled={addAccountSaveDisabled()}
          >
            {addAccountSaveLabel()}
          </button>
        </div>
      </div>
    </div>
  {/if}

  {#if provider.error && !addAccountModalOpen}
    <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">
{provider.error}
    </p>
  {/if}

  {#if providerAccounts.error}
    <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">
{providerAccounts.error}
    </p>
  {/if}

  <div class="mt-6 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
    <label class="grid min-w-0 gap-1 text-sm font-medium text-[#3c3c3c]">
Search
<input
  class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
  type="search"
  placeholder="Account name"
  bind:value={accountSearch}
/>
    </label>
    <label class="grid min-w-0 gap-1 text-sm font-medium text-[#3c3c3c]">
Type
<select
  class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
  bind:value={accountTypeFilter}
>
  <option value="all">All types</option>
  <option value="codex_oauth">Codex OAuth</option>
  <option value="api_upstream">API upstream</option>
</select>
    </label>
    <label class="grid min-w-0 gap-1 text-sm font-medium text-[#3c3c3c]">
Status
<select
  class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
  bind:value={accountStatusFilter}
>
  <option value="all">All statuses</option>
  <option value="active">Active</option>
  <option value="blocked">Blocked</option>
  <option value="rate_limited">Rate limited</option>
  <option value="circuit_open">Circuit open</option>
  <option value="expired">Expired</option>
</select>
    </label>
    <label class="grid min-w-0 gap-1 text-sm font-medium text-[#3c3c3c]">
Enabled
<select
  class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
  bind:value={accountEnabledFilter}
>
  <option value="all">All</option>
  <option value="enabled">Enabled</option>
  <option value="disabled">Disabled</option>
</select>
    </label>
  </div>

  <div class="ui-table-shell mt-6 overflow-x-auto rounded-lg border border-[#ededed]">
    <table class="ui-table ui-table--stacked w-full min-w-[1100px] text-left text-sm">
<thead class="border-b border-[#e5e5e5] bg-[#f5f5f5] text-[#6e6e6e]">
  <tr>
    <th class="px-4 py-3 font-medium" aria-sort={providerAccountSortDirection('account')}>
      <button class="ui-button inline-flex items-center gap-1 text-left font-medium hover:text-[#0d0d0d]" type="button" onclick={() => setProviderAccountSort('account')}>
        Account<span class="text-[11px]">{sortIndicator('account')}</span>
      </button>
    </th>
    <th class="w-36 px-4 py-3 font-medium" aria-sort={providerAccountSortDirection('type')}>
      <button class="ui-button inline-flex items-center gap-1 text-left font-medium hover:text-[#0d0d0d]" type="button" onclick={() => setProviderAccountSort('type')}>
        Type<span class="text-[11px]">{sortIndicator('type')}</span>
      </button>
    </th>
    <th class="w-44 px-4 py-3 font-medium" aria-sort={providerAccountSortDirection('status')}>
      <button class="ui-button inline-flex items-center gap-1 text-left font-medium hover:text-[#0d0d0d]" type="button" onclick={() => setProviderAccountSort('status')}>
        Status<span class="text-[11px]">{sortIndicator('status')}</span>
      </button>
    </th>
    <th class="w-32 px-4 py-3 font-medium" aria-sort={providerAccountSortDirection('enabled')}>
      <button class="ui-button inline-flex items-center gap-1 text-left font-medium hover:text-[#0d0d0d]" type="button" onclick={() => setProviderAccountSort('enabled')}>
        Enabled<span class="text-[11px]">{sortIndicator('enabled')}</span>
      </button>
    </th>
    <th class="w-44 px-4 py-3 font-medium" aria-sort={providerAccountSortDirection('refresh')}>
      <button class="ui-button inline-flex items-center gap-1 text-left font-medium hover:text-[#0d0d0d]" type="button" onclick={() => setProviderAccountSort('refresh')}>
        Last refresh<span class="text-[11px]">{sortIndicator('refresh')}</span>
      </button>
    </th>
    <th class="w-44 px-4 py-3 font-medium" aria-sort={providerAccountSortDirection('used')}>
      <button class="ui-button inline-flex items-center gap-1 text-left font-medium hover:text-[#0d0d0d]" type="button" onclick={() => setProviderAccountSort('used')}>
        Last used<span class="text-[11px]">{sortIndicator('used')}</span>
      </button>
    </th>
    <th class="sticky right-0 z-10 w-40 bg-[#f5f5f5] px-3 py-3 text-right font-medium shadow-[-8px_0_12px_rgba(255,255,255,0.85)]">Actions</th>
  </tr>
</thead>
<tbody class="divide-y divide-[#ededed]">
  {#if providerAccounts.loading}
    <tr>
      <td class="ui-table-empty ui-table-empty--loading px-4 py-5 text-[#6e6e6e]" colspan="7">Loading provider accounts...</td>
    </tr>
  {:else if providerAccounts.items.length === 0}
    <tr>
      <td class="ui-table-empty px-4 py-5 text-[#6e6e6e]" colspan="7">No provider accounts connected yet.</td>
    </tr>
  {:else if filteredProviderAccounts.length === 0}
    <tr>
      <td class="ui-table-empty px-4 py-5 text-[#6e6e6e]" colspan="7">No accounts match your search.</td>
    </tr>
  {:else}
    {#each paginatedProviderAccounts as account}
      {@const modelState = getAccountModelsState(account.id)}
      {@const historyState = getAccountTestResultsState(account.id)}
      {@const enabledModels = enabledAccountModelCount(modelState.items)}
      {@const effectiveStatus = providerAccountEffectiveStatus(account)}
      <tr class="bg-white align-top">
        <td class="px-4 py-3 align-middle" data-label="Account" title={accountHoverDetail(account)}>
          <p class="max-w-[22rem] truncate font-medium text-[#0d0d0d]">{accountLabel(account)}</p>
          {#if accountEmailLabel(account)}
            <p class="mt-1 max-w-[22rem] truncate text-[#6e6e6e]">{accountEmailLabel(account)}</p>
          {/if}
        </td>
        <td class="px-4 py-3 align-middle" data-label="Type">
          <span class="inline-flex whitespace-nowrap rounded-full bg-[#f5f5f5] px-2.5 py-1 text-xs font-medium text-[#3c3c3c]">
            {accountTypeLabel(account)}
          </span>
        </td>
        <td class="px-4 py-3 align-middle" data-label="Status" title={statusHoverDetail(account)}>
          <span
            class={[
              'inline-flex max-w-full whitespace-nowrap rounded-full px-2.5 py-1 text-xs font-medium capitalize',
              effectiveStatus === 'active' || !effectiveStatus
                ? 'bg-[#e8f5f0] text-[#0a7a5e]'
                : effectiveStatus === 'disabled'
                  ? 'bg-[#f5f5f5] text-[#6e6e6e]'
                  : 'bg-amber-50 text-amber-700'
            ]}
          >
            {statusLabel(effectiveStatus)}
          </span>
        </td>
        <td class="px-4 py-3 align-middle" data-label="Enabled">
          <label class="inline-flex items-center gap-2 text-sm font-medium text-[#3c3c3c]" title={account.enabled ? 'Enabled' : 'Disabled'}>
            <input
              class="peer sr-only"
              type="checkbox"
              role="switch"
              checked={account.enabled}
              disabled={providerAccounts.saving}
              aria-label={`Set ${accountLabel(account)} ${account.enabled ? 'disabled' : 'enabled'}`}
              onchange={(event) =>
                updateProviderAccount(account, {
                  enabled: event.currentTarget.checked
                })}
            />
            <span class="relative inline-flex h-5 w-9 shrink-0 rounded-full bg-[#d9d9d9] transition-colors after:absolute after:left-0.5 after:top-0.5 after:size-4 after:rounded-full after:bg-white after:shadow-sm after:transition-transform peer-checked:bg-[#10a37f] peer-checked:after:translate-x-4 peer-focus-visible:outline peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-[#10a37f] peer-disabled:cursor-not-allowed peer-disabled:opacity-60"></span>
            <span class="text-xs text-[#6e6e6e]">{account.enabled ? 'Enabled' : 'Disabled'}</span>
          </label>
        </td>
        <td class="whitespace-nowrap px-4 py-3 align-middle text-[#3c3c3c]" data-label="Last refresh">{formatDate(account.lastRefreshAt)}</td>
        <td class="whitespace-nowrap px-4 py-3 align-middle text-[#3c3c3c]" data-label="Last used">{formatDate(account.lastUsedAt)}</td>
        <td class="sticky right-0 bg-white px-3 py-3 align-middle shadow-[-8px_0_12px_rgba(255,255,255,0.85)]" data-label="Actions">
          <div class="relative flex justify-end gap-2 whitespace-nowrap">
            {#if isCodexOAuthAccount(account)}
              <button
                class="ui-button ui-button--icon ui-button--secondary inline-flex size-8 items-center justify-center rounded-md border border-[#e5e5e5] bg-white text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
                type="button"
                disabled={providerAccounts.saving || recoveryTestAccountId !== 0}
                onclick={() => testAccountRecovery(account)}
                title="Test recovery"
                aria-label={`Test recovery for ${accountLabel(account)}`}
              >
                <RefreshCw class={recoveryTestAccountId === account.id ? 'size-4 animate-spin' : 'size-4'} aria-hidden="true" />
                <span class="sr-only">Test recovery</span>
              </button>
            {/if}
            <button
              class="ui-button ui-button--icon ui-button--secondary inline-flex size-8 items-center justify-center rounded-md border border-[#e5e5e5] bg-white text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
              type="button"
              disabled={providerAccounts.saving || modelTestRunActive}
              onclick={() => openModelTests(account)}
              title="Test models"
              aria-label="Test models"
            >
              <FlaskConical class="size-4" aria-hidden="true" />
              <span class="sr-only">Test models</span>
            </button>
            <button
              class="ui-button ui-button--icon ui-button--secondary inline-flex size-8 items-center justify-center rounded-md border border-[#e5e5e5] bg-white text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
              type="button"
              disabled={providerAccounts.saving}
              onclick={() => openAccountEditor(account)}
              title="Edit account"
              aria-label="Edit account"
            >
              <Pencil class="size-4" aria-hidden="true" />
              <span class="sr-only">Edit account</span>
            </button>
            <button
              class="ui-button ui-button--icon ui-button--danger inline-flex size-8 items-center justify-center rounded-md border border-red-200 bg-white text-red-700 hover:bg-red-50 disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
              type="button"
              disabled={providerAccounts.saving}
              onclick={() => toggleDeleteConfirmation(account)}
              title="Delete account"
              aria-label="Delete account"
            >
              <Trash2 class="size-4" aria-hidden="true" />
              <span class="sr-only">Delete account</span>
            </button>
          </div>
        </td>
      </tr>
    {/each}
  {/if}
</tbody>
    </table>
  </div>

  <div class="ui-pagination mt-4 flex flex-col gap-3 text-sm text-[#6e6e6e] sm:flex-row sm:items-center sm:justify-between">
    <p>
      Showing {providerAccountPageSummary} of {filteredProviderAccounts.length}
      {#if providerAccounts.items.length !== filteredProviderAccounts.length}
        filtered from {providerAccounts.items.length}
      {/if}
    </p>
    <div class="flex flex-wrap items-center gap-2">
      <label class="inline-flex items-center gap-2 text-xs font-medium text-[#3c3c3c]">
        Rows
        <select
          class="rounded-lg border border-[#e5e5e5] bg-white px-2 py-1.5 text-xs text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={accountPageSize}
          onchange={() => {
            accountPage = 1;
          }}
        >
          <option value={5}>5</option>
          <option value={10}>10</option>
          <option value={20}>20</option>
        </select>
      </label>
      <span class="text-xs tabular-nums text-[#6e6e6e]">Page {normalizedAccountPage} of {accountPageCount}</span>
      <button
        class="ui-button ui-button--sm ui-button--secondary rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
        type="button"
        disabled={normalizedAccountPage <= 1}
        onclick={() => goToProviderAccountPage(accountPage - 1)}
      >
        Previous
      </button>
      <button
        class="ui-button ui-button--sm ui-button--secondary rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
        type="button"
        disabled={normalizedAccountPage >= accountPageCount}
        onclick={() => goToProviderAccountPage(accountPage + 1)}
      >
        Next
      </button>
    </div>
  </div>
</section>

  {#if modelTestAccount}
    {@const account = modelTestAccount}
    {@const modelState = getAccountModelsState(account.id)}
    <div
      class="ui-modal-backdrop ui-modal-backdrop--top fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/30 px-4 py-[6vh]"
      role="presentation"
      onclick={(event) => event.target === event.currentTarget && closeModelTests()}
    >
      <div class="ui-modal-panel ui-modal-panel--xl grid w-full max-w-5xl gap-4 rounded-xl bg-white p-5 shadow-xl" role="dialog" aria-modal="true" aria-label={`Model tests for ${accountLabel(account)}`}>
        <div class="flex items-start justify-between gap-4 border-b border-[#ededed] pb-4">
          <div class="min-w-0">
            <h2 class="truncate text-lg font-semibold text-[#0d0d0d]">Model tests</h2>
            <p class="mt-1 truncate text-sm text-[#6e6e6e]">{accountLabel(account)} &middot; {accountTypeLabel(account)}</p>
          </div>
          <button
            class="ui-button ui-button--icon ui-button--secondary inline-flex size-8 shrink-0 items-center justify-center rounded-md border border-[#e5e5e5] bg-white text-[#0d0d0d] hover:bg-[#f5f5f5]"
            type="button"
            onclick={closeModelTests}
            aria-label="Close model tests modal"
            title="Close"
          >
            <X class="size-4" aria-hidden="true" />
          </button>
        </div>

        <div class="grid gap-3 sm:grid-cols-3">
          <label class="grid min-w-0 gap-1 text-xs font-medium text-[#3c3c3c]">
            Search
            <input
              class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
              type="search"
              placeholder="Model name"
              bind:value={modelTestSearch}
            />
          </label>
          <label class="grid min-w-0 gap-1 text-xs font-medium text-[#3c3c3c]">
            Enabled
            <select
              class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
              bind:value={modelTestEnabledFilter}
            >
              <option value="all">All</option>
              <option value="enabled">Enabled</option>
              <option value="disabled">Disabled</option>
            </select>
          </label>
          <label class="grid min-w-0 gap-1 text-xs font-medium text-[#3c3c3c]">
            Latest result
            <select
              class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
              bind:value={modelTestStatusFilter}
            >
              <option value="all">All</option>
              <option value="not_tested">Not tested</option>
              <option value="passed">Passed</option>
              <option value="failed">Failed</option>
            </select>
          </label>
        </div>

        <div class="flex flex-wrap items-center justify-between gap-3">
          <p class="text-sm text-[#6e6e6e]">
            {filteredModelTestModels.length} shown &middot; {selectedModelTestCount} selected
          </p>
          <div class="flex flex-wrap items-center gap-2">
            <button
              class="ui-button ui-button--sm ui-button--secondary rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
              type="button"
              disabled={selectedModelTestCount === 0 || modelTestRunActive}
              onclick={clearModelTestSelection}
            >Clear selection</button>
            <button
              class="ui-button ui-button--sm ui-button--primary inline-flex items-center gap-1.5 rounded-md bg-[#0d0d0d] px-3 py-1.5 text-xs font-medium text-white hover:bg-[#1f2933] disabled:cursor-not-allowed disabled:opacity-60"
              type="button"
              disabled={selectedModelTestCount === 0 || modelTestRunActive}
              onclick={testSelectedModels}
            >
              {#if modelTestRunActive}<LoaderCircle class="size-3.5 animate-spin" aria-hidden="true" />{/if}
              Test selected ({selectedModelTestCount})
            </button>
          </div>
        </div>

        {#if modelState.error}
          <p class="rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">{modelState.error}</p>
        {/if}

        <div class="ui-table-shell max-h-[55vh] overflow-auto rounded-lg border border-[#ededed]">
          <table class="ui-table w-full min-w-[840px] text-left text-sm">
            <thead class="sticky top-0 z-10 border-b border-[#e5e5e5] bg-[#f5f5f5] text-[#6e6e6e]">
              <tr>
                <th class="w-12 px-3 py-2 font-medium">
                  <label class="inline-flex items-center">
                    <input
                      class="size-4 rounded border-[#d9d9d9] text-[#10a37f] focus:ring-[#10a37f] disabled:cursor-not-allowed disabled:opacity-60"
                      type="checkbox"
                      checked={allFilteredModelTestsSelected}
                      indeterminate={someFilteredModelTestsSelected}
                      aria-checked={someFilteredModelTestsSelected ? 'mixed' : allFilteredModelTestsSelected}
                      aria-label="Select filtered models"
                      disabled={filteredModelTestModels.length === 0 || modelTestRunActive}
                      onchange={toggleFilteredModelTestSelection}
                    />
                    <span class="sr-only">Select filtered models</span>
                  </label>
                </th>
                <th class="px-3 py-2 font-medium">Model</th>
                <th class="w-24 px-3 py-2 font-medium">Enabled</th>
                <th class="w-32 px-3 py-2 font-medium">Last result</th>
                <th class="w-24 px-3 py-2 font-medium">Latency</th>
                <th class="w-44 px-3 py-2 font-medium">Checked</th>
                <th class="w-20 px-3 py-2 text-right font-medium">Action</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-[#ededed]">
              {#if modelState.loading}
                <tr><td class="ui-table-empty ui-table-empty--loading px-3 py-5 text-[#6e6e6e]" colspan="7">Loading models...</td></tr>
              {:else if modelState.items.length === 0}
                <tr><td class="ui-table-empty px-3 py-5 text-[#6e6e6e]" colspan="7">No configured models.</td></tr>
              {:else if filteredModelTestModels.length === 0}
                <tr><td class="ui-table-empty px-3 py-5 text-[#6e6e6e]" colspan="7">No models match the current filters.</td></tr>
              {:else}
                {#each filteredModelTestModels as configuredModel (configuredModel.model)}
                  {@const displayedStatus = displayedModelTestStatus(configuredModel)}
                  {@const displayedLatency = displayedModelTestLatency(configuredModel)}
                  <tr class="bg-white">
                    <td class="px-3 py-2 align-middle">
                      <label class="inline-flex items-center">
                        <input
                          class="size-4 rounded border-[#d9d9d9] text-[#10a37f] focus:ring-[#10a37f] disabled:cursor-not-allowed disabled:opacity-60"
                          type="checkbox"
                          checked={Boolean(selectedModelTests[configuredModel.model])}
                          disabled={modelTestRunActive}
                          aria-label={`Select ${configuredModel.model}`}
                          onchange={() => toggleModelTestSelection(configuredModel.model)}
                        />
                        <span class="sr-only">Select {configuredModel.model}</span>
                      </label>
                    </td>
                    <td class="max-w-[24rem] px-3 py-2 align-middle">
                      <p class="truncate font-mono text-[13px] text-[#0d0d0d]" title={configuredModel.model}>{configuredModel.model}</p>
                    </td>
                    <td class="px-3 py-2 align-middle text-[#3c3c3c]">{configuredModel.enabled ? 'Enabled' : 'Disabled'}</td>
                    <td class="px-3 py-2 align-middle" title={modelTestDetail(configuredModel)}>
                      <span class={['inline-flex rounded-full px-2 py-0.5 text-xs font-medium', modelTestStatusClass(displayedStatus)]}>
                        {#if displayedStatus === 'testing'}<LoaderCircle class="mr-1 size-3 animate-spin" aria-hidden="true" />{/if}
                        {modelTestStatusLabel(displayedStatus)}
                      </span>
                    </td>
                    <td class="whitespace-nowrap px-3 py-2 align-middle tabular-nums text-[#3c3c3c]">{displayedLatency > 0 ? `${displayedLatency} ms` : '-'}</td>
                    <td class="whitespace-nowrap px-3 py-2 align-middle text-[#6e6e6e]">{formatDate(displayedModelTestCheckedAt(configuredModel))}</td>
                    <td class="px-3 py-2 text-right align-middle">
                      <button
                        class="ui-button ui-button--icon ui-button--secondary inline-flex size-8 items-center justify-center rounded-md border border-[#e5e5e5] bg-white text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
                        type="button"
                        disabled={modelTestRunActive}
                        onclick={() => testOneModel(configuredModel.model)}
                        title={`Test ${configuredModel.model}`}
                        aria-label={`Test ${configuredModel.model}`}
                      >
                        <FlaskConical class="size-4" aria-hidden="true" />
                      </button>
                    </td>
                  </tr>
                {/each}
              {/if}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  {/if}

  {#if deletingProviderAccount}
    {@const account = deletingProviderAccount}
    <!-- svelte-ignore a11y_click_events_have_key_events,a11y_no_static_element_interactions,a11y_interactive_supports_focus -->
    <div
      class="ui-modal-backdrop fixed inset-0 z-50 flex items-center justify-center bg-black/30"
      role="dialog"
      aria-modal="true"
      aria-label={`Confirm deleting ${accountLabel(account)}`}
    >
      <div class="ui-modal-panel ui-modal-panel--sm w-full max-w-sm rounded-xl border border-[#ededed] bg-white p-6 shadow-[0_4px_16px_rgba(13,13,13,0.06)]">
        <div class="flex items-start justify-between gap-3">
          <p class="text-sm font-medium text-[#0d0d0d]">Delete this account?</p>
          <button
            class="ui-button ui-button--icon ui-button--secondary inline-flex size-8 shrink-0 items-center justify-center rounded-md border border-[#e5e5e5] bg-white text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:opacity-60"
            type="button"
            disabled={providerAccounts.saving}
            onclick={() => { deletingProviderAccountId = 0; }}
            aria-label="Close delete account modal"
            title="Close"
          >
            <X class="size-4" aria-hidden="true" />
          </button>
        </div>
        <p class="mt-1 text-sm leading-5 text-[#6e6e6e]">{accountLabel(account)}</p>
        {#if providerAccounts.error}
          <p class="mt-3 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">{providerAccounts.error}</p>
        {/if}
        <div class="mt-4 flex justify-end gap-2">
          <button
            class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:opacity-60"
            type="button"
            disabled={providerAccounts.saving}
            onclick={() => { deletingProviderAccountId = 0; }}
          >
            Cancel
          </button>
          <button
            class="ui-button ui-button--sm ui-button--danger rounded-lg border border-red-200 bg-white px-3 py-2 text-sm font-medium text-red-700 hover:bg-red-50 disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
            type="button"
            disabled={providerAccounts.saving}
            onclick={() => confirmDisconnectProviderAccount(account)}
          >
            Delete
          </button>
        </div>
      </div>
    </div>
  {/if}

{#if editingProviderAccount && editingProviderAccountDraft}
  {@const account = editingProviderAccount}
  {@const draft = editingProviderAccountDraft}
  {@const modelState = getAccountModelsState(account.id)}
  {@const historyState = getAccountTestResultsState(account.id)}
  {@const enabledModels = enabledAccountModelCount(draft.modelItems)}
  {@const modelSummary = accountModelSummary(draft.modelItems)}
  <div
    class="ui-modal-backdrop ui-modal-backdrop--top fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/30 px-4 py-[6vh]"
    role="presentation"
  >
    <div class="ui-modal-panel ui-modal-panel--xl grid w-full max-w-5xl gap-5 rounded-xl bg-white p-5 shadow-xl" role="dialog" aria-modal="true" aria-label={`Edit ${accountLabel(account)}`}>
      <div class="flex items-start justify-between gap-4 border-b border-[#ededed] pb-4">
        <div class="min-w-0">
          <h2 class="truncate text-lg font-semibold text-[#0d0d0d]">Edit account</h2>
          <p class="mt-1 truncate text-sm text-[#6e6e6e]">{accountLabel(account)}</p>
        </div>
        <button
          class="ui-button ui-button--icon ui-button--secondary inline-flex size-8 shrink-0 items-center justify-center rounded-md border border-[#e5e5e5] bg-white text-[#0d0d0d] hover:bg-[#f5f5f5]"
          type="button"
          disabled={providerAccounts.saving || modelState.saving || modelState.syncing}
          onclick={closeAccountEditor}
          aria-label="Close edit account modal"
          title="Close"
        >
          <X class="size-4" aria-hidden="true" />
        </button>
      </div>

      <div class="grid gap-5 lg:grid-cols-[minmax(0,1fr)_minmax(280px,360px)]">
        <div class="grid gap-3">
          <h3 class="text-sm font-semibold text-[#0d0d0d]">Account settings</h3>
          <label class="grid gap-1 text-xs font-medium text-[#3c3c3c]" for={`provider-account-name-${account.id}`}>
            Name
            <input
              id={`provider-account-name-${account.id}`}
              class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
              bind:value={draft.name}
              disabled={providerAccounts.saving}
              aria-label={`Rename ${accountLabel(account)}`}
            />
          </label>
          <div class="grid gap-3 sm:grid-cols-3">
            <label class="inline-flex items-center gap-2 text-sm font-medium text-[#3c3c3c]" title={draft.enabled ? 'Enabled' : 'Disabled'}>
              <input
                class="peer sr-only"
                type="checkbox"
                role="switch"
                bind:checked={draft.enabled}
                disabled={providerAccounts.saving}
                aria-label={`Set ${accountLabel(account)} ${draft.enabled ? 'disabled' : 'enabled'}`}
              />
              <span class="relative inline-flex h-5 w-9 shrink-0 rounded-full bg-[#d9d9d9] transition-colors after:absolute after:left-0.5 after:top-0.5 after:size-4 after:rounded-full after:bg-white after:shadow-sm after:transition-transform peer-checked:bg-[#10a37f] peer-checked:after:translate-x-4 peer-focus-visible:outline peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-[#10a37f] peer-disabled:cursor-not-allowed peer-disabled:opacity-60"></span>
              <span class="text-xs text-[#6e6e6e]">{draft.enabled ? 'Enabled' : 'Disabled'}</span>
            </label>
            <label class="grid gap-1 text-xs font-medium text-[#3c3c3c]" for={`provider-account-priority-${account.id}`}>
              Priority
              <input
                id={`provider-account-priority-${account.id}`}
                class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
                type="number"
                min="0"
                step="1"
                bind:value={draft.priority}
                disabled={providerAccounts.saving}
              />
            </label>
            <label class="grid gap-1 text-xs font-medium text-[#3c3c3c]" for={`provider-account-load-factor-${account.id}`}>
              Load factor
              <input
                id={`provider-account-load-factor-${account.id}`}
                class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
                type="number"
                min="1"
                max="100"
                step="1"
                bind:value={draft.loadFactor}
                disabled={providerAccounts.saving}
              />
            </label>
          </div>
          <div class="grid gap-3 sm:grid-cols-2">
            <label class="grid gap-1 text-xs font-medium text-[#3c3c3c]" for={`provider-account-max-concurrency-${account.id}`}>
              Max concurrency
              <input
                id={`provider-account-max-concurrency-${account.id}`}
                class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
                type="number"
                min="0"
                step="1"
                bind:value={draft.maxConcurrentRequests}
                disabled={providerAccounts.saving}
              />
              <span class="text-xs text-[#6e6e6e]">Active {account.currentConcurrentRequests || 0} / {concurrencyLimitLabel(account.effectiveMaxConcurrentRequests)}</span>
            </label>
            <label class="grid gap-1 text-xs font-medium text-[#3c3c3c]">
              Fingerprint profile
              <select
                class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5]"
                disabled={providerAccounts.saving}
                bind:value={draft.fingerprintProfileId}
              >
                <option value={0}>{account.accountType === 'api_upstream' ? 'Default API upstream (pass-through)' : 'Default Codex CLI'}</option>
                {#each account.accountType === 'api_upstream' ? fingerprintProfiles.items : oauthFingerprintProfiles as fp}
                  <option value={fp.id}>{fp.name}</option>
                {/each}
              </select>
            </label>
          </div>
        </div>
        <div class="grid content-start gap-3">
          <h3 class="text-sm font-semibold text-[#0d0d0d]">Account actions</h3>
          <div class="flex flex-wrap gap-2">
            <a class="ui-button ui-button--sm ui-button--secondary rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]" href={`/request-logs?providerAccountId=${account.id}`}>Request logs</a>
            <button class="ui-button ui-button--sm ui-button--secondary rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]" type="button" disabled={providerAccounts.saving} onclick={() => isCodexOAuthAccount(account) ? testAccountRecovery(account) : testProviderAccount(account)}>{isCodexOAuthAccount(account) ? 'Test recovery' : 'Test'}</button>
            <button class="ui-button ui-button--sm ui-button--secondary rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]" type="button" disabled={providerAccounts.saving} onclick={() => toggleAccountTestHistory(account.id)}>History</button>
            <button class="ui-button ui-button--sm ui-button--secondary rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]" type="button" disabled={providerAccounts.saving} onclick={() => pauseProviderAccount(account)}>Pause</button>
            <button class="ui-button ui-button--sm ui-button--secondary rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]" type="button" disabled={providerAccounts.saving || !isCodexOAuthAccount(account)} onclick={() => refreshProviderAccount(account)}>Refresh</button>
            <button class="ui-button ui-button--sm ui-button--secondary rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]" type="button" disabled={providerAccounts.saving || (!account.rateLimitedUntil && !account.circuitOpenUntil && !account.lastError)} onclick={() => resetProviderAccountStatus(account)}>Reset local status</button>
            <button class="ui-button ui-button--sm ui-button--secondary rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]" type="button" disabled={provider.connecting || providerAccounts.saving || !isCodexOAuthAccount(account)} onclick={() => connectProvider(account)}>Reauthorize</button>
          </div>
          <dl class="grid gap-2 text-xs text-[#6e6e6e]">
            <div><dt class="font-medium text-[#3c3c3c]">Type</dt><dd>{accountTypeLabel(account)}</dd></div>
            <div><dt class="font-medium text-[#3c3c3c]">Token expiry</dt><dd>{formatDate(account.accessTokenExpiresAt)}</dd></div>
          </dl>
        </div>
      </div>

      <div class="grid gap-5 lg:grid-cols-[minmax(0,1fr)_minmax(320px,420px)]">
        {#if account.accountType === 'api_upstream'}
          <div class="grid gap-2">
            <h3 class="text-sm font-semibold text-[#0d0d0d]">Upstream credential</h3>
            <label class="grid gap-1 text-xs font-medium text-[#3c3c3c]">
              Base URL
              <input class="w-full rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-[12px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]" name="baseUrl" type="url" bind:value={draft.baseUrl} placeholder="https://api.openai.com/v1" disabled={providerAccounts.saving} />
            </label>
            <label class="grid gap-1 text-xs font-medium text-[#3c3c3c]">
              Proxy URL
              <input class="w-full rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-[12px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]" name="proxyUrl" type="url" bind:value={draft.proxyUrl} placeholder="Leave blank to clear proxy" disabled={providerAccounts.saving} />
            </label>
            <div class="grid gap-2">
              <label class="grid min-w-0 gap-1 text-xs font-medium text-[#3c3c3c]">
                API key
                <input class="w-full rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 text-xs text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]" name="apiKey" type="password" autocomplete="off" bind:value={draft.apiKey} placeholder="Leave blank to keep current key" disabled={providerAccounts.saving} />
              </label>
            </div>
          </div>
        {:else}
          <div class="grid content-start gap-2">
            <h3 class="text-sm font-semibold text-[#0d0d0d]">Proxy</h3>
            <label class="grid gap-1 text-xs font-medium text-[#3c3c3c]">
              Proxy URL
              <input class="w-full rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-[12px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]" name="proxyUrl" type="url" bind:value={draft.proxyUrl} placeholder="Leave blank to clear proxy" disabled={providerAccounts.saving} />
            </label>
          </div>
        {/if}
        <div class="grid gap-2">
          <div class="flex flex-wrap items-center justify-between gap-2">
            <h3 class="text-sm font-semibold text-[#0d0d0d]">Models</h3>
            <div class="flex flex-wrap items-center gap-2">
              {#if account.accountType === 'api_upstream'}
                <label class="inline-flex items-center gap-2 text-xs font-medium text-[#3c3c3c]">
                  <input
                    class="size-4 rounded border-[#d9d9d9] text-[#10a37f] focus:ring-[#10a37f]"
                    type="checkbox"
                    bind:checked={draft.syncModelsOnSave}
                    disabled={modelState.loading || modelState.saving || modelState.syncing}
                  />
                  Sync from upstream on Save
                </label>
              {/if}
            </div>
          </div>
          <p class="text-xs text-[#6e6e6e]">{modelSummary.total} total &middot; {modelSummary.synced} synced &middot; {modelSummary.manual} manual &middot; {modelSummary.enabled} enabled</p>
          <label class="grid gap-1 text-xs font-medium text-[#3c3c3c]" for={`provider-account-models-${account.id}`}>
            Manual models
            <textarea
              id={`provider-account-models-${account.id}`}
              class="min-h-16 w-full resize-y rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] leading-5 text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
              placeholder={'gpt-4.1\ngpt-4.1-mini'}
              bind:value={draft.modelsText}
              disabled={modelState.loading || modelState.saving || modelState.syncing}
            ></textarea>
          </label>
          {#if draft.modelItems.length > 0}
            <div class="grid max-h-44 gap-1 overflow-y-auto rounded-lg border border-[#ededed] bg-[#fafafa] p-2">
              {#each draft.modelItems as configuredModel (configuredModel.model)}
                <div class="grid grid-cols-[minmax(0,1fr)_auto_auto_auto] items-center gap-2">
                  <label class="inline-flex min-w-0 items-center gap-2 text-xs text-[#3c3c3c]">
                    <input
                      class="size-4 shrink-0 rounded border-[#d9d9d9] text-[#10a37f] focus:ring-[#10a37f] disabled:cursor-not-allowed disabled:opacity-60"
                      type="checkbox"
                      checked={configuredModel.enabled}
                      disabled={modelState.loading || modelState.saving || modelState.syncing || configuredModel.source === 'upstream'}
                      aria-label={`${configuredModel.enabled ? 'Disable' : 'Enable'} ${configuredModel.model}`}
                      onchange={(event) => {
                        draft.modelItems = setAccountModelEnabled(draft.modelItems, configuredModel.model, event.currentTarget.checked);
                        modelState.saved = false;
                      }}
                    />
                    <a class="truncate font-mono text-[13px] text-[#0d0d0d] underline-offset-2 hover:underline" href={modelRoutingHref(configuredModel.model, account)}>{configuredModel.model}</a>
                  </label>
                  <span class="text-xs text-[#6e6e6e]">{configuredModel.enabled ? 'On' : 'Off'}</span>
                  <span class="inline-flex items-center rounded-full border border-[#e5e5e5] bg-white px-2 py-0.5 text-[11px] font-medium text-[#6e6e6e]">{sourceBadgeLabel(configuredModel)}</span>
                  {#if configuredModel.source !== 'upstream'}
                    <button
                      class="ui-button ui-button--sm ui-button--secondary rounded-md border border-[#e5e5e5] bg-white px-2 py-1 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
                      type="button"
                      disabled={modelState.loading || modelState.saving || modelState.syncing}
                      onclick={() => {
                        draft.modelItems = removeAccountModel(draft.modelItems, configuredModel.model);
                        draft.modelsText = accountModelsText(draft.modelItems);
                        modelState.saved = false;
                      }}
                    >Remove</button>
                  {:else}
                    <span class="w-[4.5rem]" aria-hidden="true"></span>
                  {/if}
                </div>
              {/each}
            </div>
          {/if}
          {#if !modelState.loading && enabledModels === 0}
            <p class="rounded-md border border-amber-200 bg-amber-50 p-2 text-xs text-amber-700">This account cannot receive model-routed POST traffic until at least one enabled model is saved.</p>
          {/if}
          {#if modelState.saved}<p class="text-xs text-[#0a7a5e]">Saved.</p>{/if}
          {#if modelState.error}<p class="text-xs text-red-700">{modelState.error}</p>{/if}
          {#if modelState.syncMessage}<p class="text-xs text-[#0a7a5e]">{modelState.syncMessage}</p>{/if}
          {#if modelState.syncError}<p class="text-xs text-red-700">{modelState.syncError}</p>{/if}
        </div>
      </div>

      {#if historyState.expanded}
        <div class="rounded-lg border border-[#ededed] bg-white p-4">
          <div class="flex flex-wrap items-center justify-between gap-2">
            <h3 class="text-sm font-semibold text-[#0d0d0d]">Recent test history</h3>
            {#if historyState.loading}
              <span class="ui-loading-state text-xs text-[#6e6e6e]" aria-live="polite">Loading test history...</span>
            {/if}
          </div>
          {#if historyState.error}
            <p class="mt-3 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">{historyState.error}</p>
          {:else if !historyState.loading && historyState.items.length === 0}
            <p class="mt-3 text-sm text-[#6e6e6e]">No test history recorded yet.</p>
          {:else if historyState.items.length > 0}
            <div class="ui-table-shell mt-3 overflow-x-auto rounded-lg border border-[#ededed]">
              <table class="ui-table w-full min-w-[560px] text-left text-sm">
                <thead class="border-b border-[#e5e5e5] bg-[#f5f5f5] text-[#6e6e6e]">
                  <tr>
                    <th class="px-3 py-2 font-medium">Checked</th>
                    <th class="px-3 py-2 font-medium">Status</th>
                    <th class="px-3 py-2 font-medium">Message</th>
                    <th class="px-3 py-2 font-medium">Recorded</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-[#ededed]">
                  {#each historyState.items as result (result.id)}
                    <tr>
                      <td class="whitespace-nowrap px-3 py-2 text-[#3c3c3c]">{formatDate(result.checkedAt)}</td>
                      <td class="px-3 py-2">
                        <span class={['inline-flex rounded-full px-2 py-0.5 text-xs font-medium', testResultStatusClass(result.status)]}>
                          {result.status || 'unknown'}
                        </span>
                      </td>
                      <td class="max-w-[28rem] px-3 py-2 text-[#3c3c3c]">{result.message || 'No message'}</td>
                      <td class="whitespace-nowrap px-3 py-2 text-[#6e6e6e]">{formatDate(result.createdAt)}</td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
          {/if}
        </div>
      {/if}
      <div class="flex flex-wrap items-center justify-between gap-3 border-t border-[#ededed] pt-4">
        <div class="min-w-0 flex-1">
          {#if editingProviderAccountError}
            <p class="rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">{editingProviderAccountError}</p>
          {/if}
        </div>
        <div class="flex items-center gap-3">
          <button
            class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#e5e5e5] bg-white px-4 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:opacity-60"
            type="button"
            disabled={providerAccounts.saving || modelState.saving || modelState.syncing}
            onclick={closeAccountEditor}
          >
            Cancel
          </button>
          <button
            class="ui-button ui-button--sm ui-button--primary rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white disabled:cursor-not-allowed disabled:opacity-60"
            type="button"
            disabled={providerAccounts.saving || modelState.loading || modelState.saving || modelState.syncing}
            onclick={saveEditingProviderAccount}
          >
            {providerAccounts.saving || modelState.saving ? 'Saving' : 'Save'}
          </button>
        </div>
      </div>
    </div>
  </div>
{/if}

</AuthGate>
