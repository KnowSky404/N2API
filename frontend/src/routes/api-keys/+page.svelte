<script>
  import { page } from '$app/state';
  import { Copy, Pencil, Plus, ScrollText, SquareCheckBig, Trash2, X } from 'lucide-svelte';
  import {
    apiKeys,
    apiKeyModelWarnings,
    bulkDeleteSelectedRevokedAPIKeys,
    bulkRevokeSelectedAPIKeys,
    bulkSetSelectedAPIKeysDisabled,
    bulkUpdateSelectedAPIKeys,
    clearAPIKeySelection,
    copyAPIKeySecret,
    copySecret,
    createKey,
    deleteRevokedKey,
    formatCostMicrousd,
    formatDate,
    formatTokens,
    gatewaySettings,
    getActiveKeys,
    loadGatewaySettings,
    loadModelRouting,
    loadRoutingPools,
    modelListText,
    modelRouting,
    revokeKey,
    routingPools,
    selectedAPIKeyIds,
    session,
    setAPIKeyDisabled,
    toggleAPIKeySelection,
    updateAPIKeyBudgets,
    updateAPIKeyLimits,
    updateAPIKeyName,
    updateAPIKeyModelPolicy,
    updateAPIKeyRoutingPool,
  } from '$lib/admin-state.svelte.js';

  import AuthGate from '$lib/AuthGate.svelte';
  const activeKeys = $derived(getActiveKeys());
  let keySearch = $state('');
  let keyStatusFilter = $state('all');
  let keyRoutingPoolFilter = $state('all');
  let keyModelPolicyFilter = $state('all');
  let keyIssueFilter = $state('all');
  let modelRoutingRequested = $state(false);
  let createKeyModalOpen = $state(false);
  let editingKeyId = $state(0);
  let editingKeyDraft = $state(/** @type {import('$lib/admin-state.svelte.js').APIKey | null} */ (null));
  let editKeySaving = $state(false);
  const editingKey = $derived(editingKeyDraft);
  let logsKeyId = $state(0);
  const logsKey = $derived(apiKeys.items.find((key) => key.id === logsKeyId) ?? null);
  let deleteConfirmKeyPopover = $state(/** @type {{key: import('$lib/admin-state.svelte.js').APIKey|null, bulkAction: 'revoke'|'purge'|null, top: number, left: number}|null} */ (null));
  let deleteConfirmBusy = $state(false);
  const deleteConfirmKey = $derived(deleteConfirmKeyPopover?.key ?? null);
  let appliedAPIKeySearch = $state('');
  let bulkEditModalOpen = $state(false);
  const bulkEditForm = $state({
    applyStatus: false,
    targetDisabled: 'false',
    applyModelPolicy: false,
    targetModelPolicy: 'all',
    targetModelsText: '',
    applyRoutingPool: false,
    targetRoutingPoolId: 0,
    applyLimits: false,
    targetRequestsPerMinute: '',
    targetTokensPerMinute: '',
    applyBudgets: false,
    targetRequestBudget24h: '',
    targetTokenBudget24h: '',
    targetCostBudgetMicrousd24h: '',
    targetRequestBudget30d: '',
    targetTokenBudget30d: '',
    targetCostBudgetMicrousd30d: '',
  });

  const filteredAPIKeys = $derived(
    apiKeys.items.filter((key) => {
      if (keyStatusFilter === 'active' && (key.revokedAt || key.disabledAt)) return false;
      if (keyStatusFilter === 'disabled' && (!key.disabledAt || key.revokedAt)) return false;
      if (keyStatusFilter === 'revoked' && !key.revokedAt) return false;

      if (keyRoutingPoolFilter === 'unbound' && Number(key.routingPoolId ?? 0) > 0) return false;
      if (/^[1-9]\d*$/.test(keyRoutingPoolFilter)) {
        const poolId = Number(keyRoutingPoolFilter);
        if (Number(key.routingPoolId ?? 0) !== poolId) return false;
      }

      if (keyModelPolicyFilter === 'selected' && key.modelPolicy !== 'selected') return false;
      if (keyModelPolicyFilter === 'all_routable' && key.modelPolicy === 'selected') return false;

      if (keyIssueFilter === 'blocked_or_budget') {
        const hasIssue = key.concurrencyBlocked || key.requestRateLimited || key.tokenRateLimited
          || key.requestBudgetExceeded || key.tokenBudgetExceeded || key.costBudgetExceeded;
        if (!hasIssue) return false;
      }

      const query = keySearch.trim().toLowerCase();
      if (!query) return true;
      if (/^id:[1-9]\d*$/.test(query)) {
        const idQuery = query.slice(3);
        return String(key.id) === idQuery;
      }
      if (/^pool:[1-9]\d*$/.test(query)) {
        const poolQuery = query.slice(5);
        return String(key.routingPoolId ?? 0) === poolQuery;
      }
      return apiKeySearchText(key).includes(query);
    })
  );

  // --- Pagination state ---
  let keyPage = $state(1);
  let keyPageSize = $state(5);
  const keyPageCount = $derived(Math.max(1, Math.ceil(filteredAPIKeys.length / keyPageSize)));
  const normalizedKeyPage = $derived(Math.min(Math.max(keyPage, 1), keyPageCount));
  const paginatedAPIKeys = $derived(
    filteredAPIKeys.slice((normalizedKeyPage - 1) * keyPageSize, normalizedKeyPage * keyPageSize)
  );
  const apiKeyPageSummary = $derived(
    filteredAPIKeys.length === 0
      ? '0'
      : `${(normalizedKeyPage - 1) * keyPageSize + 1}-${(normalizedKeyPage - 1) * keyPageSize + paginatedAPIKeys.length}`
  );

  const selectedAPIKeyCount = $derived(Object.keys(selectedAPIKeyIds).length);

  const selectedEditableAPIKeys = $derived(
    Object.keys(selectedAPIKeyIds)
      .map(Number)
      .filter((id) => {
        const key = apiKeys.items.find((k) => k.id === id);
        return key && !key.revokedAt;
      })
  );

  const selectedRevokedAPIKeys = $derived(
    Object.keys(selectedAPIKeyIds)
      .map(Number)
      .filter((id) => {
        const key = apiKeys.items.find((k) => k.id === id);
        return key && Boolean(key.revokedAt);
      })
  );

  const allFilteredAPIKeysSelected = $derived(
    filteredAPIKeys.length > 0 && filteredAPIKeys.every((key) => Boolean(selectedAPIKeyIds[key.id]))
  );

  /**
   * @param {boolean} selected
   */
  function toggleFilteredAPIKeySelection(selected) {
    for (const key of filteredAPIKeys) {
      toggleAPIKeySelection(key.id, selected);
    }
  }

  /** @param {string} search */
  function applyAPIKeyURLFilters(search) {
    const params = new URLSearchParams(search);
    const clientKeyId = params.get('clientKeyId') ?? '';
    const routingPoolId = params.get('routingPoolId') ?? '';
    const status = params.get('status') ?? '';
    keySearch = '';
    if (['all', 'active', 'disabled', 'revoked'].includes(status)) {
      keyStatusFilter = status;
    }
    if (/^[1-9]\d*$/.test(clientKeyId)) {
      keySearch = `id:${clientKeyId}`;
    } else if (/^[1-9]\d*$/.test(routingPoolId)) {
      keySearch = `pool:${routingPoolId}`;
    }
  }

  /**
   * @param {string | null | undefined} model
   * @param {import('$lib/admin-state.svelte.js').APIKey} key
   */
  function modelRoutingHref(model, key) {
    const value = String(model ?? '').trim();
    if (!value) return '';
    const routingPoolId = Number(key.routingPoolId ?? 0);
    const poolQuery = routingPoolId > 0 ? `&routingPoolId=${encodeURIComponent(String(key.routingPoolId))}` : '';
    return `/models?model=${encodeURIComponent(value)}&status=blocked&clientKeyId=${encodeURIComponent(String(key.id))}${poolQuery}`;
  }

  function openCreateKeyModal() {
    apiKeys.error = '';
    apiKeys.newKeyName = '';
    apiKeys.newKeyRoutingPoolId = 0;
    apiKeys.oneTimeSecret = '';
    createKeyModalOpen = true;
  }

  function closeCreateKeyModal() {
    if (apiKeys.creating) return;
    createKeyModalOpen = false;
    apiKeys.error = '';
    apiKeys.newKeyName = '';
    apiKeys.newKeyRoutingPoolId = 0;
  }

  /** @param {number} keyId */
  function openEditModal(keyId) {
    apiKeys.error = '';
    const key = apiKeys.items.find((item) => item.id === keyId);
    if (!key) return;
    editingKeyId = keyId;
    editingKeyDraft = {
      ...key,
      allowedModels: [...(key.allowedModels ?? [])],
      allowedModelsText: key.allowedModelsText ?? modelListText(key.allowedModels ?? [])
    };
  }

  function closeEditModal() {
    if (editKeySaving) return;
    editingKeyId = 0;
    editingKeyDraft = null;
    apiKeys.error = '';
  }

  function openBulkEditModal() {
    apiKeys.error = '';
    bulkEditForm.applyStatus = false;
    bulkEditForm.targetDisabled = 'false';
    bulkEditForm.applyModelPolicy = false;
    bulkEditForm.targetModelPolicy = 'all';
    bulkEditForm.targetModelsText = '';
    bulkEditForm.applyRoutingPool = false;
    bulkEditForm.targetRoutingPoolId = 0;
    bulkEditForm.applyLimits = false;
    bulkEditForm.targetRequestsPerMinute = '';
    bulkEditForm.targetTokensPerMinute = '';
    bulkEditForm.applyBudgets = false;
    bulkEditForm.targetRequestBudget24h = '';
    bulkEditForm.targetTokenBudget24h = '';
    bulkEditForm.targetCostBudgetMicrousd24h = '';
    bulkEditForm.targetRequestBudget30d = '';
    bulkEditForm.targetTokenBudget30d = '';
    bulkEditForm.targetCostBudgetMicrousd30d = '';
    bulkEditModalOpen = true;
  }

  function closeBulkEditModal() {
    if (apiKeys.saving) return;
    bulkEditModalOpen = false;
  }

  /** @param {SubmitEvent} event */
  async function submitBulkEdit(event) {
    event.preventDefault();
    const selectedIds = [...selectedEditableAPIKeys];
    /** @type {{
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
     *   targetCostBudgetMicrousd30d?: string | number
     * }} */
    const patch = {};
    if (bulkEditForm.applyStatus) {
      patch.applyStatus = true;
      patch.targetDisabled = bulkEditForm.targetDisabled === 'true';
    }
    if (bulkEditForm.applyModelPolicy) {
      patch.applyModelPolicy = true;
      patch.targetModelPolicy = bulkEditForm.targetModelPolicy;
      patch.targetModelsText = bulkEditForm.targetModelsText;
    }
    if (bulkEditForm.applyRoutingPool) {
      patch.applyRoutingPool = true;
      patch.targetRoutingPoolId = bulkEditForm.targetRoutingPoolId;
    }
    if (bulkEditForm.applyLimits) {
      patch.applyLimits = true;
      patch.targetRequestsPerMinute = bulkEditForm.targetRequestsPerMinute;
      patch.targetTokensPerMinute = bulkEditForm.targetTokensPerMinute;
    }
    if (bulkEditForm.applyBudgets) {
      patch.applyBudgets = true;
      patch.targetRequestBudget24h = bulkEditForm.targetRequestBudget24h;
      patch.targetTokenBudget24h = bulkEditForm.targetTokenBudget24h;
      patch.targetCostBudgetMicrousd24h = bulkEditForm.targetCostBudgetMicrousd24h;
      patch.targetRequestBudget30d = bulkEditForm.targetRequestBudget30d;
      patch.targetTokenBudget30d = bulkEditForm.targetTokenBudget30d;
      patch.targetCostBudgetMicrousd30d = bulkEditForm.targetCostBudgetMicrousd30d;
    }
    const noSections =
      !patch.applyStatus &&
      !patch.applyModelPolicy &&
      !patch.applyRoutingPool &&
      !patch.applyLimits &&
      !patch.applyBudgets;
    if (noSections) {
      apiKeys.error = 'Select at least one section to apply';
      return;
    }
    await bulkUpdateSelectedAPIKeys(patch);
    const bulkError = apiKeys.error;
    for (const id of selectedIds) {
      if (apiKeys.items.some((key) => key.id === id && !key.revokedAt)) {
        selectedAPIKeyIds[String(id)] = true;
      }
    }
    if (bulkError) apiKeys.error = `Bulk save may have partially applied: ${bulkError}`;
  }

  /** @param {import('$lib/admin-state.svelte.js').APIKey} key */
  function keyStatusLabel(key) {
    if (key.revokedAt) return 'Deleted';
    if (key.disabledAt) return 'Disabled';
    return 'Active';
  }

  /** @param {import('$lib/admin-state.svelte.js').APIKey} key */
  function keyPhysicalDeleteTitle(key) {
    if (!key.revokedAt) return keyStatusLabel(key);
    const value = key.physicalDeleteAt ? formatDate(key.physicalDeleteAt) : '7 days after deletion';
    return `Physical delete after ${value}`;
  }

  /** @param {number} keyId */
  function openKeyLogsModal(keyId) {
    logsKeyId = keyId;
  }

  function closeKeyLogsModal() {
    logsKeyId = 0;
  }

  /** @param {MouseEvent} event */
  function deleteConfirmPosition(event) {
    const rect = /** @type {HTMLElement} */ (event.currentTarget).getBoundingClientRect();
    const popoverWidth = 288;
    let left = rect.left + rect.width - popoverWidth;
    if (left < 8) left = 8;
    if (left + popoverWidth > window.innerWidth - 8) left = window.innerWidth - popoverWidth - 8;
    return { top: rect.bottom + 8, left };
  }

  /**
   * @param {import('$lib/admin-state.svelte.js').APIKey} key
   * @param {MouseEvent} event
   */
  function openDeleteConfirmKey(key, event) {
    apiKeys.error = '';
    deleteConfirmKeyPopover = { key, bulkAction: null, ...deleteConfirmPosition(event) };
  }

  /** @param {MouseEvent} event */
  function openBulkDeleteConfirm(event) {
    if (selectedEditableAPIKeys.length === 0) return;
    apiKeys.error = '';
    deleteConfirmKeyPopover = { key: null, bulkAction: 'revoke', ...deleteConfirmPosition(event) };
  }

  /** @param {MouseEvent} event */
  function openBulkPermanentDeleteConfirm(event) {
    if (selectedRevokedAPIKeys.length === 0) return;
    apiKeys.error = '';
    deleteConfirmKeyPopover = { key: null, bulkAction: 'purge', ...deleteConfirmPosition(event) };
  }

  function closeDeleteConfirmKeyPopover() {
    if (!deleteConfirmBusy) deleteConfirmKeyPopover = null;
  }

  async function confirmDeleteKey() {
    const target = deleteConfirmKeyPopover;
    if (!target || deleteConfirmBusy) return;

    deleteConfirmBusy = true;
    apiKeys.error = '';
    try {
      if (target.bulkAction === 'revoke') {
        await bulkRevokeSelectedAPIKeys();
      } else if (target.bulkAction === 'purge') {
        await bulkDeleteSelectedRevokedAPIKeys();
      } else if (target.key?.revokedAt) {
        await deleteRevokedKey(target.key.id);
      } else if (target.key) {
        await revokeKey(target.key.id);
      }
      if (!apiKeys.error) deleteConfirmKeyPopover = null;
    } finally {
      deleteConfirmBusy = false;
    }
  }

  /** @param {SubmitEvent} event */
  async function submitCreateKey(event) {
    await createKey(event);
  }

  /** @param {SubmitEvent} event */
  async function submitEditKey(event) {
    event.preventDefault();
    if (!editingKeyDraft || editKeySaving) return;

    const snap = {
      id: editingKeyDraft.id,
      name: String(editingKeyDraft.name ?? '').trim(),
      revokedAt: editingKeyDraft.revokedAt,
      modelPolicy: editingKeyDraft.modelPolicy || 'all',
      allowedModelsText: editingKeyDraft.allowedModelsText ?? modelListText(editingKeyDraft.allowedModels ?? []),
      routingPoolId: editingKeyDraft.routingPoolId ?? 0,
      requestsPerMinute: editingKeyDraft.requestsPerMinute ?? 0,
      tokensPerMinute: editingKeyDraft.tokensPerMinute ?? 0,
      requestBudget24h: editingKeyDraft.requestBudget24h ?? 0,
      tokenBudget24h: editingKeyDraft.tokenBudget24h ?? 0,
      costBudgetMicrousd24h: editingKeyDraft.costBudgetMicrousd24h ?? 0,
      requestBudget30d: editingKeyDraft.requestBudget30d ?? 0,
      tokenBudget30d: editingKeyDraft.tokenBudget30d ?? 0,
      costBudgetMicrousd30d: editingKeyDraft.costBudgetMicrousd30d ?? 0,
    };
    if (snap.revokedAt) return;
    if (!snap.name) {
      apiKeys.error = 'API key name cannot be empty';
      return;
    }

    editKeySaving = true;
    try {
      await updateAPIKeyName(snap.id, snap.name);
      if (apiKeys.error) return;
      await updateAPIKeyModelPolicy(snap.id, snap.modelPolicy, snap.allowedModelsText);
      if (apiKeys.error) {
        apiKeys.error = `Name was saved, but model access failed: ${apiKeys.error}`;
        return;
      }
      await updateAPIKeyRoutingPool(snap.id, snap.routingPoolId);
      if (apiKeys.error) {
        apiKeys.error = `Earlier sections were saved, but routing pool failed: ${apiKeys.error}`;
        return;
      }
      await updateAPIKeyLimits(snap.id, snap.requestsPerMinute, snap.tokensPerMinute);
      if (apiKeys.error) {
        apiKeys.error = `Earlier sections were saved, but limits failed: ${apiKeys.error}`;
        return;
      }
      await updateAPIKeyBudgets(
        snap.id,
        snap.requestBudget24h,
        snap.tokenBudget24h,
        snap.costBudgetMicrousd24h,
        snap.requestBudget30d,
        snap.tokenBudget30d,
        snap.costBudgetMicrousd30d
      );
      if (apiKeys.error) {
        apiKeys.error = `Earlier sections were saved, but budgets failed: ${apiKeys.error}`;
        return;
      }
      openEditModal(snap.id);
    } finally {
      editKeySaving = false;
    }
  }

  $effect(() => {
    if (!session.authenticated) {
      modelRoutingRequested = false;
      appliedAPIKeySearch = '';
      keySearch = '';
      return;
    }
    if (appliedAPIKeySearch !== page.url.search) {
      appliedAPIKeySearch = page.url.search;
      applyAPIKeyURLFilters(page.url.search);
    }
    if (!modelRoutingRequested) {
      modelRoutingRequested = true;
      void loadModelRouting();
      void loadGatewaySettings();
      void loadRoutingPools();
    }
  });


  // Clamp page when total changes (e.g. filtering)
  $effect(() => {
    if (keyPage > keyPageCount) {
      keyPage = keyPageCount;
    } else if (keyPage < 1) {
      keyPage = 1;
    }
  });

  /** @param {number} page */
  function goToAPIKeyPage(page) {
    keyPage = Math.min(Math.max(page, 1), keyPageCount);
  }

  /** @param {import('$lib/admin-state.svelte.js').APIKey} key */
  function unroutableModelsForKey(key) {
    return apiKeyModelWarnings(key, modelRouting.models, routingPools.items);
  }

  /** @param {import('$lib/admin-state.svelte.js').APIKey} key */
  function apiKeySearchText(key) {
    return [
      key.name,
      key.prefix,
      key.routingPoolName,
      key.routingPoolId ? 'routing pool' : 'no routing pool no model access',
      key.modelPolicy === 'selected' ? 'selected models' : 'all routable models',
      ...(key.allowedModels ?? []),
      key.revokedAt ? 'revoked' : key.disabledAt ? 'disabled' : 'active',
      key.concurrencyBlocked ? 'concurrency full' : '',
      key.requestRateLimited ? 'request limit full' : '',
      key.tokenRateLimited ? 'token limit full' : '',
      key.requestBudgetExceeded ? 'request budget exceeded' : '',
      key.tokenBudgetExceeded ? 'token budget exceeded' : '',
      key.costBudgetExceeded ? 'cost budget exceeded' : ''
    ]
      .filter(Boolean)
      .join(' ')
      .toLowerCase();
  }

  /** @param {import('$lib/admin-state.svelte.js').APIKey} key */
  function routingPoolFallbackNameForKey(key) {
    const pool = routingPools.items.find((item) => item.id === key.routingPoolId);
    return pool?.fallbackPoolName ?? '';
  }

  /** @param {import('$lib/admin-state.svelte.js').APIKey} key */
  function apiKeyRoutingPoolHref(key) {
    const id = Number(key.routingPoolId ?? 0);
    if (id <= 0) return '';
    return `/routing-pools?routingPoolId=${encodeURIComponent(String(id))}`;
  }

  /** @param {import('$lib/admin-state.svelte.js').APIKey} key */
  function apiKeyRoutingPoolFallbackHref(key) {
    const pool = routingPools.items.find((item) => item.id === key.routingPoolId);
    const fallbackPoolId = Number(pool?.fallbackPoolId ?? 0);
    if (fallbackPoolId <= 0) return '';
    return `/routing-pools?routingPoolId=${encodeURIComponent(String(fallbackPoolId))}`;
  }

  /** @param {import('$lib/admin-state.svelte.js').APIKey} key */
  function apiKeyRoutingPoolFallbackChainLabel(key) {
    const names = [];
    const seen = new Set();
    let current = routingPools.items.find((item) => item.id === key.routingPoolId);
    while (current) {
      if (seen.has(current.id)) {
        names.push('cycle');
        break;
      }
      seen.add(current.id);
      names.push(current.name || `pool ${current.id}`);
      const fallbackID = Number(current.fallbackPoolId ?? 0);
      if (fallbackID <= 0) break;
      const next = routingPools.items.find((candidate) => candidate.id === fallbackID);
      if (!next) {
        names.push('missing fallback');
        break;
      }
      current = next;
    }
    return names.join(' -> ');
  }

  /** @param {import('$lib/admin-state.svelte.js').APIKey} key */
  function apiKeyRoutingPoolFallbackChainLogsHref(key) {
    const pool = routingPools.items.find((item) => item.id === key.routingPoolId);
    const fallbackPoolId = Number(pool?.fallbackPoolId ?? 0);
    if (fallbackPoolId <= 0) return '';
    const chain = apiKeyRoutingPoolFallbackChainLabel(key);
    return `/request-logs?clientKeyId=${encodeURIComponent(String(key.id))}&routingPoolChain=${encodeURIComponent(chain)}`;
  }

  /**
   * @param {number | null | undefined} value
   * @param {number | null | undefined} defaultValue
   */
  function keyLimitLabel(value, defaultValue) {
    const limit = Number(value ?? 0);
    if (limit > 0) return String(limit);

    const fallback = Number(defaultValue ?? 0);
    return fallback > 0 ? `Default (${fallback})` : 'Default (disabled)';
  }

  /** @param {number | null | undefined} value */
  function keyConcurrencyLimitLabel(value) {
    const limit = Number(value ?? 0);
    return limit > 0 ? String(limit) : 'unlimited';
  }

  /** @param {number | null | undefined} value */
  function keyRateWindowLimitLabel(value) {
    const limit = Number(value ?? 0);
    return limit > 0 ? String(limit) : 'unlimited';
  }

  /** @param {number | null | undefined} value */
  function keyRateRemainingLabel(value) {
    return `${Math.max(0, Number(value ?? 0))} remaining`;
  }

  /**
   * @param {number | null | undefined} used
   * @param {number | null | undefined} limit
   */
  function keyBudgetUsageLabel(used, limit) {
    const current = Number(used ?? 0);
    const cap = Number(limit ?? 0);
    return cap > 0 ? `${current} / ${cap}` : `${current} / unlimited`;
  }
</script>

<svelte:head>
  <title>N2API API Keys</title>
</svelte:head>

<AuthGate>
<div class="ui-page min-w-0 max-w-full overflow-x-hidden">
  <header class="ui-page-header">
    <div class="ui-page-heading">
      <h1 class="ui-page-title">API keys</h1>
      <p class="ui-page-description">
        Client access, routing policy, usage limits, and budgets.
        {#if apiKeys.loading}Loading keys.{:else}{activeKeys.length} active{activeKeys.length === 1 ? ' key' : ' keys'}.{/if}
      </p>
    </div>
    <div class="ui-page-actions">
      <button
        class="ui-button ui-button--sm ui-button--primary rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white"
        type="button"
        onclick={openCreateKeyModal}
      >
        <Plus class="size-4" aria-hidden="true" />
        Create key
      </button>
    </div>
  </header>

  {#if apiKeys.error && !createKeyModalOpen && !bulkEditModalOpen}
    <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">
{apiKeys.error}
    </p>
  {/if}

  {#if createKeyModalOpen}
    <!-- svelte-ignore a11y_click_events_have_key_events,a11y_no_static_element_interactions,a11y_interactive_supports_focus -->
    <div
      class="ui-modal-backdrop fixed inset-0 z-50 flex items-center justify-center bg-black/30 p-4"
      role="dialog"
      aria-modal="true"
      aria-label="Create API key"
    >
      <div class="ui-modal-panel ui-modal-panel--md w-full max-w-lg max-h-[calc(100vh-4rem)] overflow-y-auto rounded-lg border border-[#ededed] bg-white p-6 shadow-lg">
        <div class="mb-4 flex items-center justify-between">
          <h3 class="text-lg font-semibold text-[#0d0d0d]">Create API key</h3>
          <button
            class="ui-button ui-button--icon ui-button--secondary inline-flex size-8 items-center justify-center rounded-md border border-[#e5e5e5] bg-white text-[#6e6e6e] hover:bg-[#f5f5f5] hover:text-[#0d0d0d]"
            type="button"
            disabled={apiKeys.creating}
            onclick={closeCreateKeyModal}
            aria-label="Close create API key modal"
            title="Close"
          >
            <X class="size-4" aria-hidden="true" />
          </button>
        </div>

        {#if apiKeys.error}
          <p class="mb-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">
            {apiKeys.error}
          </p>
        {/if}

        <form class="space-y-4" onsubmit={submitCreateKey}>
          <div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
            <label class="grid gap-2 text-sm font-medium text-[#3c3c3c]">
              New key name
              <input
                class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-base text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                bind:value={apiKeys.newKeyName}
                placeholder="Codex workstation"
                required
              />
            </label>
            <label class="mt-4 grid gap-2 text-sm font-medium text-[#3c3c3c]">
              Routing pool
              <select
                class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
                bind:value={apiKeys.newKeyRoutingPoolId}
                disabled={apiKeys.creating || routingPools.loading}
              >
                <option value={0}>No routing pool</option>
                {#each routingPools.items as pool}
                  <option value={pool.id}>{pool.name}</option>
                {/each}
              </select>
            </label>
            <p class="mt-2 text-xs leading-5 text-[#6e6e6e]">No routing pool = no model access.</p>
          </div>
          {#if apiKeys.oneTimeSecret}
            <div class="rounded-lg border border-[#cbe7dd] bg-[#e8f5f0] p-4">
              <div class="flex flex-wrap items-center justify-between gap-3">
                <p class="text-sm font-medium text-[#0a7a5e]">Copy this key before saving another one.</p>
                <button class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#b7d9cd] bg-white px-3 py-1.5 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]" type="button" onclick={copySecret}>Copy</button>
              </div>
              <code class="mt-3 block overflow-x-auto rounded-md bg-white px-3 py-2 font-mono text-[13px] leading-6 text-[#0d0d0d]">{apiKeys.oneTimeSecret}</code>
            </div>
          {/if}
          <div class="ui-modal-actions flex justify-end gap-3">
            <button class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]" type="button" disabled={apiKeys.creating} onclick={closeCreateKeyModal}>Cancel</button>
            <button class="ui-button ui-button--sm ui-button--primary rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white disabled:cursor-not-allowed disabled:opacity-60" type="submit" disabled={apiKeys.creating}>
              {apiKeys.creating ? 'Saving' : 'Save'}
            </button>
          </div>
        </form>
      </div>
    </div>
  {/if}

  {#if bulkEditModalOpen}
    <!-- svelte-ignore a11y_click_events_have_key_events,a11y_no_static_element_interactions,a11y_interactive_supports_focus -->
    <div
      class="ui-modal-backdrop fixed inset-0 z-50 flex items-center justify-center bg-black/30 p-4"
      role="dialog"
      aria-modal="true"
      aria-label="Bulk edit API keys"
    >
      <div class="ui-modal-panel ui-modal-panel--md w-full max-w-lg max-h-[calc(100vh-4rem)] overflow-y-auto rounded-lg border border-[#ededed] bg-white p-6 shadow-lg">
        <div class="mb-4 flex items-center justify-between">
          <h3 class="text-lg font-semibold text-[#0d0d0d]">Bulk edit API keys</h3>
          <button
            class="ui-button ui-button--icon ui-button--secondary inline-flex size-8 items-center justify-center rounded-md border border-[#e5e5e5] bg-white text-[#6e6e6e] hover:bg-[#f5f5f5] hover:text-[#0d0d0d]"
            type="button"
            disabled={apiKeys.saving}
            onclick={closeBulkEditModal}
            aria-label="Close bulk edit API keys modal"
            title="Close"
          >
            <X class="size-4" aria-hidden="true" />
          </button>
        </div>

        <p class="mb-4 text-sm text-[#6e6e6e]">
          Selected keys: {selectedAPIKeyCount}. Editable: {selectedEditableAPIKeys.length}.
        </p>

        {#if apiKeys.error}
          <p class="mb-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">
            {apiKeys.error}
          </p>
        {/if}

        <form class="space-y-4" onsubmit={submitBulkEdit}>
          <div class="space-y-4 rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
          <label class="flex items-center gap-3 text-sm font-medium text-[#3c3c3c]">
            <input
              class="size-4 rounded border-[#d9d9d9] text-[#10a37f] focus:ring-[#10a37f]"
              type="checkbox"
              bind:checked={bulkEditForm.applyStatus}
            />
            <span>Apply status</span>
          </label>
          {#if bulkEditForm.applyStatus}
            <label class="grid gap-2 text-sm font-medium text-[#3c3c3c]">
              <select
                class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                bind:value={bulkEditForm.targetDisabled}
              >
                <option value="false">Enabled</option>
                <option value="true">Disabled</option>
              </select>
            </label>
          {/if}

          <label class="flex items-center gap-3 text-sm font-medium text-[#3c3c3c]">
            <input
              class="size-4 rounded border-[#d9d9d9] text-[#10a37f] focus:ring-[#10a37f]"
              type="checkbox"
              bind:checked={bulkEditForm.applyModelPolicy}
            />
            <span>Apply model access</span>
          </label>
          {#if bulkEditForm.applyModelPolicy}
            <label class="grid gap-2 text-sm font-medium text-[#3c3c3c]">
              Model policy
              <select
                class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                bind:value={bulkEditForm.targetModelPolicy}
              >
                <option value="all">All routable models</option>
                <option value="selected">Selected models</option>
              </select>
            </label>
            {#if bulkEditForm.targetModelPolicy === 'selected'}
              <label class="grid gap-2 text-sm font-medium text-[#3c3c3c]">
                Selected models (one per line)
                <textarea
                  class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                  rows="4"
                  bind:value={bulkEditForm.targetModelsText}
                ></textarea>
              </label>
            {/if}
          {/if}

          <label class="flex items-center gap-3 text-sm font-medium text-[#3c3c3c]">
            <input
              class="size-4 rounded border-[#d9d9d9] text-[#10a37f] focus:ring-[#10a37f]"
              type="checkbox"
              bind:checked={bulkEditForm.applyRoutingPool}
            />
            <span>Apply routing pool</span>
          </label>
          {#if bulkEditForm.applyRoutingPool}
            <label class="grid gap-2 text-sm font-medium text-[#3c3c3c]">
              Routing pool
              <select
                class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                bind:value={bulkEditForm.targetRoutingPoolId}
              >
                <option value={0}>No routing pool</option>
                {#each routingPools.items as pool}
                  <option value={pool.id}>{pool.name}</option>
                {/each}
              </select>
            </label>
            <p class="mt-2 text-xs leading-5 text-[#6e6e6e]">No routing pool = no model access for the selected keys.</p>
          {/if}

          <label class="flex items-center gap-3 text-sm font-medium text-[#3c3c3c]">
            <input
              class="size-4 rounded border-[#d9d9d9] text-[#10a37f] focus:ring-[#10a37f]"
              type="checkbox"
              bind:checked={bulkEditForm.applyLimits}
            />
            <span>Apply limits</span>
          </label>
          {#if bulkEditForm.applyLimits}
            <p class="text-xs text-[#6e6e6e]">Leave unchanged</p>
            <div class="grid grid-cols-2 gap-3">
              <label class="grid gap-1 text-xs font-medium text-[#6e6e6e]">
                Requests/min
                <input
                  class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                  type="number"
                  min="0"
                  step="1"
                  placeholder="Leave unchanged"
                  bind:value={bulkEditForm.targetRequestsPerMinute}
                />
              </label>
              <label class="grid gap-1 text-xs font-medium text-[#6e6e6e]">
                Tokens/min
                <input
                  class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                  type="number"
                  min="0"
                  step="1"
                  placeholder="Leave unchanged"
                  bind:value={bulkEditForm.targetTokensPerMinute}
                />
              </label>
            </div>
          {/if}

          <label class="flex items-center gap-3 text-sm font-medium text-[#3c3c3c]">
            <input
              class="size-4 rounded border-[#d9d9d9] text-[#10a37f] focus:ring-[#10a37f]"
              type="checkbox"
              bind:checked={bulkEditForm.applyBudgets}
            />
            <span>Apply budgets</span>
          </label>
          {#if bulkEditForm.applyBudgets}
            <p class="text-xs text-[#6e6e6e]">Leave unchanged</p>
            <div class="grid grid-cols-2 gap-3">
              <label class="grid gap-1 text-xs font-medium text-[#6e6e6e]">
                Req budget 24h
                <input
                  class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                  type="number"
                  min="0"
                  step="1"
                  placeholder="Leave unchanged"
                  bind:value={bulkEditForm.targetRequestBudget24h}
                />
              </label>
              <label class="grid gap-1 text-xs font-medium text-[#6e6e6e]">
                Token budget 24h
                <input
                  class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                  type="number"
                  min="0"
                  step="1"
                  placeholder="Leave unchanged"
                  bind:value={bulkEditForm.targetTokenBudget24h}
                />
              </label>
              <label class="grid gap-1 text-xs font-medium text-[#6e6e6e]">
                Cost budget 24h (microusd)
                <input
                  class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                  type="number"
                  min="0"
                  step="1"
                  placeholder="Leave unchanged"
                  bind:value={bulkEditForm.targetCostBudgetMicrousd24h}
                />
              </label>
              <label class="grid gap-1 text-xs font-medium text-[#6e6e6e]">
                Req budget 30d
                <input
                  class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                  type="number"
                  min="0"
                  step="1"
                  placeholder="Leave unchanged"
                  bind:value={bulkEditForm.targetRequestBudget30d}
                />
              </label>
              <label class="grid gap-1 text-xs font-medium text-[#6e6e6e]">
                Token budget 30d
                <input
                  class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                  type="number"
                  min="0"
                  step="1"
                  placeholder="Leave unchanged"
                  bind:value={bulkEditForm.targetTokenBudget30d}
                />
              </label>
              <label class="grid gap-1 text-xs font-medium text-[#6e6e6e]">
                Cost budget 30d (microusd)
                <input
                  class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                  type="number"
                  min="0"
                  step="1"
                  placeholder="Leave unchanged"
                  bind:value={bulkEditForm.targetCostBudgetMicrousd30d}
                />
              </label>
            </div>
          {/if}

          </div>
          <div class="ui-modal-actions flex justify-end gap-3">
            <button class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]" type="button" disabled={apiKeys.saving} onclick={closeBulkEditModal}>Cancel</button>
            <button
              class="ui-button ui-button--sm ui-button--primary rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white disabled:cursor-not-allowed disabled:opacity-60"
              type="submit"
              disabled={apiKeys.saving || selectedEditableAPIKeys.length === 0}
            >
              {apiKeys.saving ? 'Saving' : 'Save'}
            </button>
          </div>
        </form>
      </div>
    </div>
  {/if}
  {#if editingKey}
    <!-- svelte-ignore a11y_click_events_have_key_events,a11y_no_static_element_interactions,a11y_interactive_supports_focus -->
    <div
      class="ui-modal-backdrop fixed inset-0 z-50 flex items-center justify-center bg-black/30 p-4"
      role="dialog"
      aria-modal="true"
      aria-label="Edit API key"
    >
      <div class="ui-modal-panel ui-modal-panel--md w-full max-w-lg max-h-[calc(100vh-4rem)] overflow-y-auto rounded-lg border border-[#ededed] bg-white p-6 shadow-lg">
        <div class="mb-4 flex items-center justify-between">
          <h3 class="text-lg font-semibold text-[#0d0d0d]">Edit key &middot; {editingKey.name}</h3>
          <button
            class="ui-button ui-button--icon ui-button--secondary inline-flex size-8 items-center justify-center rounded-md border border-[#e5e5e5] bg-white text-[#6e6e6e] hover:bg-[#f5f5f5] hover:text-[#0d0d0d]"
            type="button"
            disabled={editKeySaving}
            onclick={closeEditModal}
            aria-label="Close edit API key modal"
            title="Close"
          >
            <X class="size-4" aria-hidden="true" />
          </button>
        </div>

        <form
          class="space-y-4"
          onsubmit={submitEditKey}
        >
          <!-- Name section -->
          <div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
            <h4 class="text-sm font-semibold text-[#0d0d0d]">Name</h4>
            <label class="mt-2 grid gap-2 text-sm font-medium text-[#3c3c3c]" for={`edit-api-key-name-${editingKey.id}`}>
              Key name
              <input
                id={`edit-api-key-name-${editingKey.id}`}
                class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
                bind:value={editingKey.name}
                disabled={Boolean(editingKey.revokedAt)}
              />
            </label>
          </div>

          <!-- Model access section -->
          <div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
            <h4 class="text-sm font-semibold text-[#0d0d0d]">Model access</h4>
            <label class="mt-2 grid gap-2 text-sm font-medium text-[#3c3c3c]" for={`edit-api-key-model-policy-${editingKey.id}`}>
              Model policy
              <select
                id={`edit-api-key-model-policy-${editingKey.id}`}
                class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
                bind:value={editingKey.modelPolicy}
                disabled={Boolean(editingKey.revokedAt)}
              >
                <option value="all">All routable models</option>
                <option value="selected">Selected models</option>
              </select>
            </label>
            {#if editingKey.modelPolicy === 'selected'}
              <label class="mt-2 grid gap-2 text-sm font-medium text-[#3c3c3c]" for={`edit-api-key-selected-models-${editingKey.id}`}>
                Selected models
                <textarea
                  id={`edit-api-key-selected-models-${editingKey.id}`}
                  class="min-h-20 w-full resize-y rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] leading-5 text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
                  placeholder={'gpt-4.1\ngpt-4.1-mini'}
                  value={editingKey.allowedModelsText ?? modelListText(editingKey.allowedModels ?? [])}
                  disabled={Boolean(editingKey.revokedAt)}
                  oninput={(event) => {
                    editingKey.allowedModelsText = event.currentTarget.value;
                  }}
                ></textarea>
              </label>
            {/if}
            {#if unroutableModelsForKey(editingKey).length}
              <p class="mt-2 rounded-md border border-amber-200 bg-amber-50 p-2 text-xs leading-5 text-amber-800">
                No schedulable account:
                {#each unroutableModelsForKey(editingKey) as model, index}
                  {#if index > 0}, {/if}
                  <a class="font-medium underline-offset-2 hover:underline" href={modelRoutingHref(model, editingKey)}>
                    {model}
                  </a>
                {/each}
              </p>
            {/if}
          </div>

          <!-- Routing pool section -->
          <div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
            <h4 class="text-sm font-semibold text-[#0d0d0d]">Routing pool</h4>
            <label class="mt-2 grid gap-2 text-sm font-medium text-[#3c3c3c]" for={`edit-api-key-routing-pool-${editingKey.id}`}>
              Routing pool
              <select
                id={`edit-api-key-routing-pool-${editingKey.id}`}
                class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
                value={editingKey.routingPoolId ?? 0}
                disabled={Boolean(editingKey.revokedAt) || routingPools.loading}
                onchange={(event) => {
                  editingKey.routingPoolId = Number(event.currentTarget.value || 0);
                }}
              >
                <option value={0}>No routing pool</option>
                {#each routingPools.items as pool}
                  <option value={pool.id}>{pool.name}</option>
                {/each}
              </select>
            </label>
            <p class="mt-1 text-xs text-[#6e6e6e]">
              {#if apiKeyRoutingPoolHref(editingKey)}
                <a class="font-medium text-[#0d0d0d] underline-offset-2 hover:underline" href={apiKeyRoutingPoolHref(editingKey)}>
                  {editingKey.routingPoolName || `Pool ${editingKey.routingPoolId}`}
                </a>
              {:else}
                No routing pool · No model access
              {/if}
              {#if routingPoolFallbackNameForKey(editingKey)}
                <span> · Fallback </span>
                {#if apiKeyRoutingPoolFallbackHref(editingKey)}
                  <a class="font-medium text-[#0d0d0d] underline-offset-2 hover:underline" href={apiKeyRoutingPoolFallbackHref(editingKey)}>
                    {routingPoolFallbackNameForKey(editingKey)}
                  </a>
                {:else}
                  <span>{routingPoolFallbackNameForKey(editingKey)}</span>
                {/if}
                {#if apiKeyRoutingPoolFallbackChainLogsHref(editingKey)}
                  <span> · </span>
                  <a
                    class="font-medium text-[#0d0d0d] underline-offset-2 hover:underline"
                    href={apiKeyRoutingPoolFallbackChainLogsHref(editingKey)}
                    title="View fallback chain logs"
                    aria-label="View fallback chain logs"
                  >
                    Chain logs
                  </a>
                {/if}
              {/if}
            </p>
          </div>

          <!-- Limits section -->
          <div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
            <h4 class="text-sm font-semibold text-[#0d0d0d]">Key limits</h4>
            <div class="mt-2 grid gap-3 sm:grid-cols-2">
              <label class="grid gap-1 text-xs font-medium text-[#6e6e6e]" for={`edit-api-key-requests-per-minute-${editingKey.id}`}>
                Requests /min
                <input
                  id={`edit-api-key-requests-per-minute-${editingKey.id}`}
                  class="rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
                  type="number"
                  min="0"
                  step="1"
                  value={editingKey.requestsPerMinute ?? 0}
                  disabled={Boolean(editingKey.revokedAt)}
                  oninput={(event) => {
                    editingKey.requestsPerMinute = Number(event.currentTarget.value || 0);
                  }}
                />
                <span class="text-[11px] font-normal">
                  {keyLimitLabel(editingKey.requestsPerMinute, gatewaySettings.data?.requestsPerMinutePerKey)}
                </span>
              </label>
              <label class="grid gap-1 text-xs font-medium text-[#6e6e6e]" for={`edit-api-key-tokens-per-minute-${editingKey.id}`}>
                Tokens /min
                <input
                  id={`edit-api-key-tokens-per-minute-${editingKey.id}`}
                  class="rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
                  type="number"
                  min="0"
                  step="1"
                  value={editingKey.tokensPerMinute ?? 0}
                  disabled={Boolean(editingKey.revokedAt)}
                  oninput={(event) => {
                    editingKey.tokensPerMinute = Number(event.currentTarget.value || 0);
                  }}
                />
                <span class="text-[11px] font-normal">
                  {keyLimitLabel(editingKey.tokensPerMinute, gatewaySettings.data?.tokensPerMinutePerKey)}
                </span>
              </label>
            </div>
            <div class="mt-2">
              <p class="text-xs text-[#6e6e6e]">
                Active {editingKey.currentConcurrentRequests || 0} / {keyConcurrencyLimitLabel(editingKey.effectiveMaxConcurrentRequests)}
              </p>
              <p class="mt-1 text-xs text-[#6e6e6e]">
                Requests window {editingKey.currentRequestsThisMinute || 0} / {keyRateWindowLimitLabel(editingKey.effectiveRequestsPerMinute)}
                {#if editingKey.effectiveRequestsPerMinute > 0}
                  <span>({keyRateRemainingLabel(editingKey.requestRateRemaining)})</span>
                {/if}
              </p>
              <p class="mt-1 text-xs text-[#6e6e6e]">
                Tokens window {formatTokens(editingKey.currentTokensThisMinute || 0)} / {keyRateWindowLimitLabel(editingKey.effectiveTokensPerMinute)}
                {#if editingKey.effectiveTokensPerMinute > 0}
                  <span>({keyRateRemainingLabel(editingKey.tokenRateRemaining)})</span>
                {/if}
              </p>
              {#if editingKey.concurrencyBlocked}
                <p class="mt-1 text-xs font-medium text-amber-700">Concurrency full</p>
              {/if}
              {#if editingKey.requestRateLimited}
                <p class="mt-1 text-xs font-medium text-amber-700">Request limit full</p>
              {/if}
              {#if editingKey.tokenRateLimited}
                <p class="mt-1 text-xs font-medium text-amber-700">Token limit full</p>
              {/if}
            </div>
          </div>

          <!-- Budgets section -->
          <div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
            <h4 class="text-sm font-semibold text-[#0d0d0d]">Key budgets</h4>
            <div class="mt-2 grid gap-3 sm:grid-cols-2">
              <label class="grid gap-1 text-xs font-medium text-[#6e6e6e]" for={`edit-api-key-request-budget-24h-${editingKey.id}`}>
                Requests 24h
                <input
                  id={`edit-api-key-request-budget-24h-${editingKey.id}`}
                  class="rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
                  type="number"
                  min="0"
                  step="1"
                  value={editingKey.requestBudget24h ?? 0}
                  disabled={Boolean(editingKey.revokedAt)}
                  oninput={(event) => {
                    editingKey.requestBudget24h = Number(event.currentTarget.value || 0);
                  }}
                />
              </label>
              <label class="grid gap-1 text-xs font-medium text-[#6e6e6e]" for={`edit-api-key-token-budget-24h-${editingKey.id}`}>
                Tokens 24h
                <input
                  id={`edit-api-key-token-budget-24h-${editingKey.id}`}
                  class="rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
                  type="number"
                  min="0"
                  step="1"
                  value={editingKey.tokenBudget24h ?? 0}
                  disabled={Boolean(editingKey.revokedAt)}
                  oninput={(event) => {
                    editingKey.tokenBudget24h = Number(event.currentTarget.value || 0);
                  }}
                />
              </label>
              <label class="grid gap-1 text-xs font-medium text-[#6e6e6e]" for={`edit-api-key-cost-budget-24h-${editingKey.id}`}>
                Cost 24h
                <input
                  id={`edit-api-key-cost-budget-24h-${editingKey.id}`}
                  class="rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
                  type="number"
                  min="0"
                  step="1"
                  value={editingKey.costBudgetMicrousd24h ?? 0}
                  disabled={Boolean(editingKey.revokedAt)}
                  oninput={(event) => {
                    editingKey.costBudgetMicrousd24h = Number(event.currentTarget.value || 0);
                  }}
                />
              </label>
              <label class="grid gap-1 text-xs font-medium text-[#6e6e6e]" for={`edit-api-key-request-budget-30d-${editingKey.id}`}>
                Requests 30d
                <input
                  id={`edit-api-key-request-budget-30d-${editingKey.id}`}
                  class="rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
                  type="number"
                  min="0"
                  step="1"
                  value={editingKey.requestBudget30d ?? 0}
                  disabled={Boolean(editingKey.revokedAt)}
                  oninput={(event) => {
                    editingKey.requestBudget30d = Number(event.currentTarget.value || 0);
                  }}
                />
              </label>
              <label class="grid gap-1 text-xs font-medium text-[#6e6e6e]" for={`edit-api-key-token-budget-30d-${editingKey.id}`}>
                Tokens 30d
                <input
                  id={`edit-api-key-token-budget-30d-${editingKey.id}`}
                  class="rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
                  type="number"
                  min="0"
                  step="1"
                  value={editingKey.tokenBudget30d ?? 0}
                  disabled={Boolean(editingKey.revokedAt)}
                  oninput={(event) => {
                    editingKey.tokenBudget30d = Number(event.currentTarget.value || 0);
                  }}
                />
              </label>
              <label class="grid gap-1 text-xs font-medium text-[#6e6e6e]" for={`edit-api-key-cost-budget-30d-${editingKey.id}`}>
                Cost 30d
                <input
                  id={`edit-api-key-cost-budget-30d-${editingKey.id}`}
                  class="rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
                  type="number"
                  min="0"
                  step="1"
                  value={editingKey.costBudgetMicrousd30d ?? 0}
                  disabled={Boolean(editingKey.revokedAt)}
                  oninput={(event) => {
                    editingKey.costBudgetMicrousd30d = Number(event.currentTarget.value || 0);
                  }}
                />
              </label>
            </div>
            <div class="mt-2">
              <p class="text-xs text-[#6e6e6e]">
                Requests 24h {keyBudgetUsageLabel(editingKey.requestsUsed24h, editingKey.requestBudget24h)}
                {#if editingKey.requestsRemaining24h !== null && editingKey.requestsRemaining24h !== undefined}
                  <span>({editingKey.requestsRemaining24h} remaining)</span>
                {/if}
              </p>
              <p class="mt-1 text-xs text-[#6e6e6e]">
                Tokens 24h {formatTokens(editingKey.tokensUsed24h || 0)} / {editingKey.tokenBudget24h > 0 ? formatTokens(editingKey.tokenBudget24h) : 'unlimited'}
                {#if editingKey.tokensRemaining24h !== null && editingKey.tokensRemaining24h !== undefined}
                  <span>({formatTokens(editingKey.tokensRemaining24h)} remaining)</span>
                {/if}
              </p>
              <p class="mt-1 text-xs text-[#6e6e6e]">
                Cost 24h {formatCostMicrousd(editingKey.costMicrousd24h || 0)} / {editingKey.costBudgetMicrousd24h > 0 ? formatCostMicrousd(editingKey.costBudgetMicrousd24h) : 'unlimited'}
                {#if editingKey.costRemainingMicrousd24h !== null && editingKey.costRemainingMicrousd24h !== undefined}
                  <span>({formatCostMicrousd(editingKey.costRemainingMicrousd24h)} remaining)</span>
                {/if}
              </p>
              <p class="mt-1 text-xs text-[#6e6e6e]">
                Requests 30d {keyBudgetUsageLabel(editingKey.requestsUsed30d, editingKey.requestBudget30d)}
                {#if editingKey.requestsRemaining30d !== null && editingKey.requestsRemaining30d !== undefined}
                  <span>({editingKey.requestsRemaining30d} remaining)</span>
                {/if}
              </p>
              <p class="mt-1 text-xs text-[#6e6e6e]">
                Tokens 30d {formatTokens(editingKey.tokensUsed30d || 0)} / {editingKey.tokenBudget30d > 0 ? formatTokens(editingKey.tokenBudget30d) : 'unlimited'}
                {#if editingKey.tokensRemaining30d !== null && editingKey.tokensRemaining30d !== undefined}
                  <span>({formatTokens(editingKey.tokensRemaining30d)} remaining)</span>
                {/if}
              </p>
              <p class="mt-1 text-xs text-[#6e6e6e]">
                Cost 30d {formatCostMicrousd(editingKey.costMicrousd30d || 0)} / {editingKey.costBudgetMicrousd30d > 0 ? formatCostMicrousd(editingKey.costBudgetMicrousd30d) : 'unlimited'}
                {#if editingKey.costRemainingMicrousd30d !== null && editingKey.costRemainingMicrousd30d !== undefined}
                  <span>({formatCostMicrousd(editingKey.costRemainingMicrousd30d)} remaining)</span>
                {/if}
              </p>
              {#if editingKey.requestBudgetExceeded}
                <p class="mt-1 text-xs font-medium text-amber-700">Request budget exceeded</p>
              {/if}
              {#if editingKey.tokenBudgetExceeded}
                <p class="mt-1 text-xs font-medium text-amber-700">Token budget exceeded</p>
              {/if}
              {#if editingKey.costBudgetExceeded}
                <p class="mt-1 text-xs font-medium text-amber-700">Cost budget exceeded</p>
              {/if}
            </div>
          </div>

          {#if apiKeys.error}
            <p class="rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">{apiKeys.error}</p>
          {/if}

          <div class="ui-modal-actions flex items-center justify-end gap-3">
            <button
              class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:opacity-60"
              type="button"
              disabled={editKeySaving}
              onclick={closeEditModal}
            >
              Cancel
            </button>
            <button
              class="ui-button ui-button--sm ui-button--primary rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white hover:bg-[#3c3c3c] disabled:cursor-not-allowed disabled:opacity-60"
              type="submit"
              disabled={editKeySaving || Boolean(editingKey.revokedAt)}
            >
              {editKeySaving ? 'Saving' : 'Save'}
            </button>
          </div>
        </form>
      </div>
    </div>
  {/if}

  {#if logsKey}
    <!-- svelte-ignore a11y_click_events_have_key_events,a11y_no_static_element_interactions,a11y_interactive_supports_focus -->
    <div
      class="ui-modal-backdrop fixed inset-0 z-50 flex items-center justify-center bg-black/30 p-4"
      onclick={(e) => e.target === e.currentTarget && closeKeyLogsModal()}
      role="dialog"
      aria-modal="true"
      aria-label="API key logs"
    >
      <div class="ui-modal-panel ui-modal-panel--lg w-full max-w-2xl max-h-[calc(100vh-4rem)] overflow-y-auto rounded-lg border border-[#ededed] bg-white p-6 shadow-lg">
        <div class="mb-4 flex items-center justify-between gap-3">
          <div class="min-w-0">
            <h3 class="truncate text-lg font-semibold text-[#0d0d0d]">Logs · {logsKey.name}</h3>
            <p class="mt-1 font-mono text-xs text-[#6e6e6e]">{logsKey.prefix}</p>
          </div>
          <button
            class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#d9d9d9] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d]"
            type="button"
            onclick={closeKeyLogsModal}
          >
            Close
          </button>
        </div>
        <div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
          <p class="text-sm font-medium text-[#0d0d0d]">Request log preview</p>
          <p class="mt-1 text-sm text-[#6e6e6e]">No log entries loaded.</p>
        </div>
      </div>
    </div>
  {/if}

  <div class="mt-6 grid grid-cols-1 sm:flex sm:flex-wrap sm:items-end sm:justify-between sm:gap-3">
    <div class="grid grid-cols-1 sm:flex sm:flex-wrap sm:items-end sm:gap-3">
      <label class="block text-sm font-medium text-[#3c3c3c]">
        Search keys
        <input
          class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          type="search"
          bind:value={keySearch}
          oninput={() => { keyPage = 1; }}
          placeholder="name, prefix, model, status"
        />
      </label>
      <label class="block text-sm font-medium text-[#3c3c3c]">
        Status filter
        <select
          class="mt-2 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={keyStatusFilter}
          onchange={() => { keyPage = 1; }}
        >
          <option value="all">All keys</option>
          <option value="active">Active keys</option>
          <option value="disabled">Disabled keys</option>
          <option value="revoked">Deleted keys</option>
        </select>
      </label>
      <label class="block text-sm font-medium text-[#3c3c3c]">
        Routing pool filter
        <select
          class="mt-2 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={keyRoutingPoolFilter}
          onchange={() => { keyPage = 1; }}
        >
          <option value="all">All routing pools</option>
          <option value="unbound">No routing pool</option>
          {#each routingPools.items as pool}
            <option value={String(pool.id)}>{pool.name}</option>
          {/each}
        </select>
      </label>
      <label class="block text-sm font-medium text-[#3c3c3c]">
        Model policy filter
        <select
          class="mt-2 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={keyModelPolicyFilter}
          onchange={() => { keyPage = 1; }}
        >
          <option value="all">All model policies</option>
          <option value="all_routable">All routable models</option>
          <option value="selected">Selected models</option>
        </select>
      </label>
      <label class="block text-sm font-medium text-[#3c3c3c]">
        Issue filter
        <select
          class="mt-2 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={keyIssueFilter}
          onchange={() => { keyPage = 1; }}
        >
          <option value="all">All issue states</option>
          <option value="blocked_or_budget">Only blocked or budget exceeded</option>
        </select>
      </label>
    </div>
  </div>

  {#if selectedAPIKeyCount > 0}
    <div class="mt-4 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-[#e5e5e5] bg-[#fafafa] p-3">
      <p class="text-sm text-[#3c3c3c]">
        {selectedAPIKeyCount} selected · {selectedEditableAPIKeys.length} editable · {selectedRevokedAPIKeys.length} deleted
      </p>
      <div class="flex flex-wrap gap-2">
        <button
          class="ui-button ui-button--sm ui-button--secondary inline-flex items-center gap-1.5 rounded-md border border-[#e5e5e5] bg-white px-3 py-1.5 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:opacity-60"
          type="button"
          disabled={apiKeys.saving}
          onclick={openBulkEditModal}
        >
          <SquareCheckBig class="size-3.5" aria-hidden="true" />
          Edit selected
        </button>
        <button
          class="ui-button ui-button--sm ui-button--secondary inline-flex items-center gap-1.5 rounded-md border border-[#e5e5e5] bg-white px-3 py-1.5 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:opacity-60"
          type="button"
          disabled={apiKeys.saving}
          onclick={() => bulkSetSelectedAPIKeysDisabled(false)}
        >
          Enable
        </button>
        <button
          class="ui-button ui-button--sm ui-button--secondary inline-flex items-center gap-1.5 rounded-md border border-[#e5e5e5] bg-white px-3 py-1.5 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:opacity-60"
          type="button"
          disabled={apiKeys.saving}
          onclick={() => bulkSetSelectedAPIKeysDisabled(true)}
        >
          Disable
        </button>
        <button
          class="ui-button ui-button--sm ui-button--secondary inline-flex items-center gap-1.5 rounded-md border border-[#e5e5e5] bg-white px-3 py-1.5 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:opacity-60"
          type="button"
          disabled={apiKeys.saving || deleteConfirmBusy || selectedEditableAPIKeys.length === 0}
          onclick={openBulkDeleteConfirm}
        >
          Delete
        </button>
        <button
          class="ui-button ui-button--sm ui-button--danger inline-flex items-center gap-1.5 rounded-md border border-red-200 bg-white px-3 py-1.5 text-sm font-medium text-red-700 hover:bg-red-50 disabled:cursor-not-allowed disabled:opacity-60"
          type="button"
          disabled={apiKeys.saving || deleteConfirmBusy || selectedRevokedAPIKeys.length === 0}
          onclick={openBulkPermanentDeleteConfirm}
        >
          Permanently delete
        </button>
        <button
          class="ui-button ui-button--sm ui-button--secondary inline-flex items-center gap-1.5 rounded-md border border-[#e5e5e5] bg-white px-3 py-1.5 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:opacity-60"
          type="button"
          disabled={apiKeys.saving}
          onclick={clearAPIKeySelection}
        >
          Clear
        </button>
      </div>
    </div>
  {/if}

  <div class="ui-table-shell mt-4 overflow-x-auto rounded-lg border border-[#ededed]">
    <table class="ui-table ui-table--stacked w-full min-w-[980px] text-left text-sm">
<thead class="border-b border-[#e5e5e5] bg-[#f5f5f5] text-[#6e6e6e]">
  <tr>
    <th class="w-12 px-4 py-3 font-medium">
      <label class="inline-flex items-center">
        <input
          class="size-4 rounded border-[#d9d9d9] text-[#10a37f] focus:ring-[#10a37f]"
          type="checkbox"
          checked={allFilteredAPIKeysSelected}
          disabled={apiKeys.loading || filteredAPIKeys.length === 0}
          onchange={(event) => toggleFilteredAPIKeySelection(event.currentTarget.checked)}
        />
        <span class="sr-only">Select</span>
      </label>
    </th>
    <th class="px-4 py-3 font-medium">Name</th>
    <th class="px-4 py-3 font-medium">Prefix</th>
    <th class="px-4 py-3 font-medium">Created</th>
    <th class="px-4 py-3 font-medium">Last used</th>
    <th class="px-4 py-3 font-medium">Status</th>
    <th class="px-4 py-3 text-right font-medium">Action</th>
  </tr>
</thead>
<tbody class="divide-y divide-[#ededed]">
  {#if apiKeys.loading}
    <tr>
      <td class="ui-table-empty ui-table-empty--loading px-4 py-5 text-[#6e6e6e]" colspan="7">Loading API keys...</td>
    </tr>
  {:else if apiKeys.items.length === 0}
    <tr>
      <td class="ui-table-empty px-4 py-5 text-[#6e6e6e]" colspan="7">No API keys created yet.</td>
    </tr>
  {:else if filteredAPIKeys.length === 0}
    <tr>
      <td class="ui-table-empty px-4 py-5 text-[#6e6e6e]" colspan="7">No API keys match your filters.</td>
    </tr>
  {:else}
    {#each paginatedAPIKeys as key}
      <tr class="bg-white">
        <td class="px-4 py-3 align-middle" data-label="Select">
          <label class="inline-flex items-center">
            <input
              class="size-4 rounded border-[#d9d9d9] text-[#10a37f] focus:ring-[#10a37f] disabled:cursor-not-allowed disabled:opacity-60"
              type="checkbox"
              checked={Boolean(selectedAPIKeyIds[key.id])}
              disabled={apiKeys.saving}
              onchange={(event) => toggleAPIKeySelection(key.id, event.currentTarget.checked)}
            />
            <span class="sr-only">Select {key.name}</span>
          </label>
        </td>
        <td class="px-4 py-3 font-medium text-[#0d0d0d]" data-label="Name">{key.name}</td>
        <td class="px-4 py-3 align-middle" data-label="Prefix">
          <div class="inline-flex items-center gap-2 whitespace-nowrap">
            <span class="font-mono text-[13px] text-[#3c3c3c]">{key.prefix}</span>
            {#if !key.revokedAt}
              <button
                class="ui-button ui-button--icon ui-button--secondary inline-flex size-7 items-center justify-center rounded-md border border-[#e5e5e5] bg-white text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
                type="button"
                disabled={!key.secretAvailable}
                onclick={() => copyAPIKeySecret(key.id)}
                title={key.secretAvailable ? 'Copy full API key' : 'Full API key is unavailable for legacy keys'}
                aria-label="Copy full API key"
              >
                <Copy class="size-3.5" aria-hidden="true" />
                <span class="sr-only">Copy full API key</span>
              </button>
            {/if}
          </div>
        </td>
        <td class="px-4 py-3 text-[#3c3c3c]" data-label="Created">{formatDate(key.createdAt)}</td>
        <td class="px-4 py-3 text-[#3c3c3c]" data-label="Last used">{formatDate(key.lastUsedAt)}</td>
        <td class="px-4 py-3 align-middle" data-label="Status">
          <div title={keyPhysicalDeleteTitle(key)}>
            {#if key.revokedAt}
              <span class="inline-flex rounded-full bg-red-50 px-2.5 py-1 text-xs font-medium text-red-700">
                Deleted
              </span>
            {:else}
              <label class="inline-flex items-center gap-2 text-sm font-medium text-[#3c3c3c]" title={key.disabledAt ? 'Disabled' : 'Enabled'}>
                <input
                  class="peer sr-only"
                  type="checkbox"
                  role="switch"
                  checked={!key.disabledAt}
                  aria-label={`Set ${key.name} ${key.disabledAt ? 'enabled' : 'disabled'}`}
                  onchange={() => setAPIKeyDisabled(key.id, !key.disabledAt)}
                />
                <span class="relative inline-flex h-5 w-9 shrink-0 rounded-full bg-[#d9d9d9] transition-colors after:absolute after:left-0.5 after:top-0.5 after:size-4 after:rounded-full after:bg-white after:shadow-sm after:transition-transform peer-checked:bg-[#10a37f] peer-checked:after:translate-x-4 peer-focus-visible:outline peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-[#10a37f]"></span>
                <span class="text-xs text-[#6e6e6e]">{key.disabledAt ? 'Disabled' : 'Enabled'}</span>
              </label>
            {/if}
          </div>
        </td>
        <td class="whitespace-nowrap px-4 py-3 align-middle text-right" data-label="Actions">
          <div class="inline-flex items-center justify-end gap-1 whitespace-nowrap">
            <button
              class="ui-button ui-button--icon ui-button--secondary inline-flex size-8 items-center justify-center rounded-md border border-[#e5e5e5] bg-white text-[#0d0d0d] hover:bg-[#f5f5f5]"
              type="button"
              onclick={() => openEditModal(key.id)}
              title="Edit key"
              aria-label="Edit key"
            >
              <Pencil class="size-4" aria-hidden="true" />
              <span class="sr-only">Edit key</span>
            </button>
            <button
              class="ui-button ui-button--icon ui-button--secondary inline-flex size-8 items-center justify-center rounded-md border border-[#e5e5e5] bg-white text-[#0d0d0d] hover:bg-[#f5f5f5]"
              type="button"
              onclick={() => openKeyLogsModal(key.id)}
              title="View request logs"
              aria-label="View request logs"
            >
              <ScrollText class="size-4" aria-hidden="true" />
              <span class="sr-only">View request logs</span>
            </button>
            <button
              class="ui-button ui-button--icon ui-button--secondary inline-flex size-8 items-center justify-center rounded-md border border-[#e5e5e5] bg-white text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
              type="button"
              disabled={apiKeys.saving || deleteConfirmBusy}
              onclick={(event) => openDeleteConfirmKey(key, event)}
              title={key.revokedAt ? 'Permanently delete key' : 'Delete key'}
              aria-label={key.revokedAt ? 'Permanently delete key' : 'Delete key'}
            >
              <Trash2 class="size-4" aria-hidden="true" />
              <span class="sr-only">Delete key</span>
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
      Showing {apiKeyPageSummary} of {filteredAPIKeys.length}
      {#if filteredAPIKeys.length !== apiKeys.items.length}
        filtered from {apiKeys.items.length}
      {/if}
      {#if selectedAPIKeyCount > 0}
        &middot; {selectedAPIKeyCount} selected
      {/if}
    </p>
    <div class="flex flex-wrap items-center gap-2">
      <label class="inline-flex items-center gap-2 text-xs font-medium text-[#3c3c3c]">
        Rows
        <select
          class="rounded-lg border border-[#e5e5e5] bg-white px-2 py-1.5 text-xs text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={keyPageSize}
          onchange={() => {
            keyPage = 1;
          }}
        >
          <option value={5}>5</option>
          <option value={10}>10</option>
          <option value={20}>20</option>
        </select>
      </label>
      <span class="text-xs tabular-nums text-[#6e6e6e]">Page {normalizedKeyPage} of {keyPageCount}</span>
      <button
        class="ui-button ui-button--sm ui-button--secondary rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
        type="button"
        disabled={normalizedKeyPage <= 1}
        onclick={() => goToAPIKeyPage(keyPage - 1)}
      >
        Previous
      </button>
      <button
        class="ui-button ui-button--sm ui-button--secondary rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
        type="button"
        disabled={normalizedKeyPage >= keyPageCount}
        onclick={() => goToAPIKeyPage(keyPage + 1)}
      >
        Next
      </button>
    </div>
  </div>
</div>

{#if deleteConfirmKeyPopover}
  <div
    class="fixed z-50 w-72 rounded-xl border border-[#ededed] bg-white p-4 shadow-[0_4px_16px_rgba(13,13,13,0.08)]"
    style="top: {deleteConfirmKeyPopover.top}px; left: {deleteConfirmKeyPopover.left}px;"
  >
    <p class="text-sm font-medium text-[#0d0d0d]">
      {deleteConfirmKeyPopover.bulkAction === 'purge'
        ? 'Permanently delete selected API keys?'
        : deleteConfirmKeyPopover.bulkAction === 'revoke'
          ? 'Delete selected API keys?'
        : deleteConfirmKey?.revokedAt
          ? 'Permanently delete this API key?'
          : 'Delete this API key?'}
    </p>
    <p class="mt-1 text-sm text-[#6e6e6e]">
      {deleteConfirmKeyPopover.bulkAction === 'purge'
        ? `${selectedRevokedAPIKeys.length} deleted key${selectedRevokedAPIKeys.length === 1 ? '' : 's'}. This cannot be undone.`
        : deleteConfirmKeyPopover.bulkAction === 'revoke'
          ? `${selectedEditableAPIKeys.length} selected key${selectedEditableAPIKeys.length === 1 ? '' : 's'}`
        : deleteConfirmKey?.revokedAt
          ? `${deleteConfirmKey.name}. This cannot be undone.`
          : deleteConfirmKey?.name || 'this key'}
    </p>
    <div class="mt-3 flex justify-end gap-2">
      <button
        class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:opacity-60"
        type="button"
        disabled={deleteConfirmBusy}
        onclick={closeDeleteConfirmKeyPopover}
      >
        Cancel
      </button>
      <button
        class="ui-button ui-button--sm ui-button--danger rounded-lg border border-red-200 bg-white px-2.5 py-1.5 text-xs font-medium text-red-700 hover:bg-red-50 disabled:cursor-not-allowed disabled:opacity-60"
        type="button"
        disabled={deleteConfirmBusy
          || (deleteConfirmKeyPopover.bulkAction === 'revoke' && selectedEditableAPIKeys.length === 0)
          || (deleteConfirmKeyPopover.bulkAction === 'purge' && selectedRevokedAPIKeys.length === 0)}
        onclick={confirmDeleteKey}
      >
        {deleteConfirmBusy
          ? 'Deleting'
          : deleteConfirmKeyPopover.bulkAction === 'purge'
            ? `Permanently delete ${selectedRevokedAPIKeys.length}`
            : deleteConfirmKeyPopover.bulkAction === 'revoke'
              ? `Delete ${selectedEditableAPIKeys.length}`
            : deleteConfirmKey?.revokedAt
              ? 'Permanently delete'
              : 'Delete'}
      </button>
    </div>
  </div>
{/if}

</AuthGate>
