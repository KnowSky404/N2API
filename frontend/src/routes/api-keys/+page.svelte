<script>
  import { page } from '$app/state';
  import { Copy, Pencil, ScrollText, Trash2 } from 'lucide-svelte';
  import {
    apiKeys,
    apiKeyModelWarnings,
    copyAPIKeySecret,
    copySecret,
    createKey,
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
    session,
    setAPIKeyDisabled,
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
  let modelRoutingRequested = $state(false);
  let createKeyModalOpen = $state(false);
  let editingKeyId = $state(0);
  const editingKey = $derived(apiKeys.items.find((key) => key.id === editingKeyId) ?? null);
  let logsKeyId = $state(0);
  const logsKey = $derived(apiKeys.items.find((key) => key.id === logsKeyId) ?? null);
  let appliedAPIKeySearch = $state('');
  const filteredAPIKeys = $derived(
    apiKeys.items.filter((key) => {
      if (keyStatusFilter === 'active' && (key.revokedAt || key.disabledAt)) return false;
      if (keyStatusFilter === 'disabled' && (!key.disabledAt || key.revokedAt)) return false;
      if (keyStatusFilter === 'revoked' && !key.revokedAt) return false;

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
    createKeyModalOpen = true;
  }

  function closeCreateKeyModal() {
    createKeyModalOpen = false;
    apiKeys.error = '';
    apiKeys.newKeyName = '';
  }

  /** @param {number} keyId */
  function openEditModal(keyId) {
    editingKeyId = keyId;
  }

  function closeEditModal() {
    editingKeyId = 0;
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
    const value = key.physicalDeleteAt ? formatDate(key.physicalDeleteAt) : '30 days after deletion';
    return `Physical delete after ${value}`;
  }

  /** @param {number} keyId */
  function openKeyLogsModal(keyId) {
    logsKeyId = keyId;
  }

  function closeKeyLogsModal() {
    logsKeyId = 0;
  }

  /** @param {SubmitEvent} event */
  async function submitCreateKey(event) {
    await createKey(event);
    if (!apiKeys.error) closeCreateKeyModal();
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
      key.routingPoolId ? 'routing pool' : 'global pool',
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
<section class="rounded-lg border border-[#ededed] bg-white p-6">
  <div class="flex flex-wrap items-center justify-between gap-4">
    <div>
<h2 class="text-2xl font-semibold leading-tight text-[#0d0d0d]">API keys</h2>
<p class="mt-1 text-sm text-[#6e6e6e]">
  Signed in as {session.username}. {activeKeys.length} active
  {activeKeys.length === 1 ? 'key' : 'keys'}.
</p>
    </div>
    <div>
      <button
        class="rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white"
        type="button"
        onclick={openCreateKeyModal}
      >
        Create key
      </button>
    </div>
  </div>

  {#if apiKeys.oneTimeSecret}
    <div class="mt-5 rounded-lg border border-[#cbe7dd] bg-[#e8f5f0] p-4">
<div class="flex flex-wrap items-center justify-between gap-3">
  <p class="text-sm font-medium text-[#0a7a5e]">
    Copy this key now. You can copy it again later from the Prefix column.
  </p>
  <button
    class="rounded-lg border border-[#b7d9cd] bg-white px-3 py-1.5 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]"
    type="button"
    onclick={copySecret}
  >
    Copy
  </button>
</div>
<code
  class="mt-3 block overflow-x-auto rounded-md bg-white px-3 py-2 font-mono text-[13px] leading-6 text-[#0d0d0d]"
>
  {apiKeys.oneTimeSecret}
</code>
    </div>
  {/if}

  {#if apiKeys.error && !createKeyModalOpen}
    <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">
{apiKeys.error}
    </p>
  {/if}

  {#if createKeyModalOpen}
    <!-- svelte-ignore a11y_click_events_have_key_events,a11y_no_static_element_interactions,a11y_interactive_supports_focus -->
    <div
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/30 p-4"
      onclick={(e) => e.target === e.currentTarget && closeCreateKeyModal()}
      role="dialog"
      aria-modal="true"
      aria-label="Create API key"
    >
      <div class="w-full max-w-lg max-h-[calc(100vh-4rem)] overflow-y-auto rounded-lg border border-[#ededed] bg-white p-6 shadow-lg">
        <div class="mb-4 flex items-center justify-between">
          <h3 class="text-lg font-semibold text-[#0d0d0d]">Create API key</h3>
          <button
            class="rounded-lg border border-[#d9d9d9] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d]"
            type="button"
            onclick={closeCreateKeyModal}
          >
            Cancel
          </button>
        </div>

        {#if apiKeys.error}
          <p class="mb-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">
            {apiKeys.error}
          </p>
        {/if}

        <form class="space-y-4 rounded-lg border border-[#ededed] bg-[#fafafa] p-4" onsubmit={submitCreateKey}>
          <label class="grid gap-2 text-sm font-medium text-[#3c3c3c]">
            New key name
            <input
              class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-base text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
              bind:value={apiKeys.newKeyName}
              placeholder="Codex workstation"
              required
            />
          </label>
          <button class="w-full rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white disabled:cursor-not-allowed disabled:opacity-60" disabled={apiKeys.creating}>
            {apiKeys.creating ? 'Creating' : 'Create key'}
          </button>
        </form>
      </div>
    </div>
  {/if}

  {#if editingKey}
    <!-- svelte-ignore a11y_click_events_have_key_events,a11y_no_static_element_interactions,a11y_interactive_supports_focus -->
    <div
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/30 p-4"
      onclick={(e) => e.target === e.currentTarget && closeEditModal()}
      role="dialog"
      aria-modal="true"
      aria-label="Edit API key"
    >
      <div class="w-full max-w-lg max-h-[calc(100vh-4rem)] overflow-y-auto rounded-lg border border-[#ededed] bg-white p-6 shadow-lg">
        <div class="mb-4 flex items-center justify-between">
          <h3 class="text-lg font-semibold text-[#0d0d0d]">Edit key · {editingKey.name}</h3>
          <button
            class="rounded-lg border border-[#d9d9d9] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d]"
            type="button"
            onclick={closeEditModal}
          >
            Close
          </button>
        </div>

        <form
          class="space-y-4 rounded-lg border border-[#ededed] bg-[#fafafa] p-4"
          onsubmit={(event) => {
            event.preventDefault();
            updateAPIKeyName(editingKey.id, editingKey.name);
          }}
        >
          <h4 class="text-sm font-semibold text-[#0d0d0d]">Name</h4>
          <label class="grid gap-2 text-sm font-medium text-[#3c3c3c]" for={`edit-api-key-name-${editingKey.id}`}>
            Key name
            <input
              id={`edit-api-key-name-${editingKey.id}`}
              class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
              bind:value={editingKey.name}
              disabled={Boolean(editingKey.revokedAt)}
            />
          </label>
          <button
            class="rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
            type="submit"
            disabled={Boolean(editingKey.revokedAt)}
          >
            Save name
          </button>
        </form>

        <form
          class="mt-4 space-y-4 rounded-lg border border-[#ededed] bg-[#fafafa] p-4"
          onsubmit={(event) => {
            event.preventDefault();
            updateAPIKeyModelPolicy(
              editingKey.id,
              editingKey.modelPolicy || 'all',
              editingKey.allowedModelsText ?? modelListText(editingKey.allowedModels ?? [])
            );
          }}
        >
          <h4 class="text-sm font-semibold text-[#0d0d0d]">Model access</h4>
          <label class="grid gap-2 text-sm font-medium text-[#3c3c3c]" for={`edit-api-key-model-policy-${editingKey.id}`}>
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
            <label class="grid gap-2 text-sm font-medium text-[#3c3c3c]" for={`edit-api-key-selected-models-${editingKey.id}`}>
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
            <p class="rounded-md border border-amber-200 bg-amber-50 p-2 text-xs leading-5 text-amber-800">
              No schedulable account:
              {#each unroutableModelsForKey(editingKey) as model, index}
                {#if index > 0}, {/if}
                <a class="font-medium underline-offset-2 hover:underline" href={modelRoutingHref(model, editingKey)}>
                  {model}
                </a>
              {/each}
            </p>
          {/if}
          <button
            class="rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
            type="submit"
            disabled={Boolean(editingKey.revokedAt)}
          >
            Save access
          </button>

          <div class="border-t border-[#ededed] pt-4">
            <label class="grid gap-2 text-sm font-medium text-[#3c3c3c]" for={`edit-api-key-routing-pool-${editingKey.id}`}>
              Routing pool
              <select
                id={`edit-api-key-routing-pool-${editingKey.id}`}
                class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
                value={editingKey.routingPoolId ?? 0}
                disabled={Boolean(editingKey.revokedAt) || routingPools.loading}
                onchange={(event) => updateAPIKeyRoutingPool(editingKey.id, Number(event.currentTarget.value || 0))}
              >
                <option value={0}>Global provider account pool</option>
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
                Global pool
              {/if}
              {#if routingPoolFallbackNameForKey(editingKey)}
                <span>· Fallback </span>
                {#if apiKeyRoutingPoolFallbackHref(editingKey)}
                  <a class="font-medium text-[#0d0d0d] underline-offset-2 hover:underline" href={apiKeyRoutingPoolFallbackHref(editingKey)}>
                    {routingPoolFallbackNameForKey(editingKey)}
                  </a>
                {:else}
                  <span>{routingPoolFallbackNameForKey(editingKey)}</span>
                {/if}
                {#if apiKeyRoutingPoolFallbackChainLogsHref(editingKey)}
                  <span>· </span>
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
        </form>

        <form
          class="mt-4 space-y-4 rounded-lg border border-[#ededed] bg-[#fafafa] p-4"
          onsubmit={(event) => {
            event.preventDefault();
            updateAPIKeyLimits(
              editingKey.id,
              editingKey.requestsPerMinute ?? 0,
              editingKey.tokensPerMinute ?? 0
            );
          }}
        >
          <h4 class="text-sm font-semibold text-[#0d0d0d]">Key limits</h4>
          <div class="grid gap-3 sm:grid-cols-2">
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
          <div>
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
          <button
            class="rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
            type="submit"
            disabled={Boolean(editingKey.revokedAt)}
          >
            Save limits
          </button>
        </form>

        <form
          class="mt-4 space-y-4 rounded-lg border border-[#ededed] bg-[#fafafa] p-4"
          onsubmit={(event) => {
            event.preventDefault();
            updateAPIKeyBudgets(
              editingKey.id,
              editingKey.requestBudget24h ?? 0,
              editingKey.tokenBudget24h ?? 0,
              editingKey.costBudgetMicrousd24h ?? 0,
              editingKey.requestBudget30d ?? 0,
              editingKey.tokenBudget30d ?? 0,
              editingKey.costBudgetMicrousd30d ?? 0
            );
          }}
        >
          <h4 class="text-sm font-semibold text-[#0d0d0d]">Key budgets</h4>
          <div class="grid gap-3 sm:grid-cols-2">
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
          <div>
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
          <button
            class="rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
            type="submit"
            disabled={Boolean(editingKey.revokedAt)}
          >
            Save budgets
          </button>
        </form>
      </div>
    </div>
  {/if}

  {#if logsKey}
    <!-- svelte-ignore a11y_click_events_have_key_events,a11y_no_static_element_interactions,a11y_interactive_supports_focus -->
    <div
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/30 p-4"
      onclick={(e) => e.target === e.currentTarget && closeKeyLogsModal()}
      role="dialog"
      aria-modal="true"
      aria-label="API key logs"
    >
      <div class="w-full max-w-2xl max-h-[calc(100vh-4rem)] overflow-y-auto rounded-lg border border-[#ededed] bg-white p-6 shadow-lg">
        <div class="mb-4 flex items-center justify-between gap-3">
          <div class="min-w-0">
            <h3 class="truncate text-lg font-semibold text-[#0d0d0d]">Logs · {logsKey.name}</h3>
            <p class="mt-1 font-mono text-xs text-[#6e6e6e]">{logsKey.prefix}</p>
          </div>
          <button
            class="rounded-lg border border-[#d9d9d9] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d]"
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
          placeholder="name, prefix, model, status"
        />
      </label>
      <label class="block text-sm font-medium text-[#3c3c3c]">
        Status filter
        <select
          class="mt-2 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={keyStatusFilter}
        >
          <option value="all">All keys</option>
          <option value="active">Active keys</option>
          <option value="disabled">Disabled keys</option>
          <option value="revoked">Deleted keys</option>
        </select>
      </label>
    </div>
    <p class="text-sm text-[#6e6e6e]">
      Showing {filteredAPIKeys.length} of {apiKeys.items.length}
    </p>
  </div>

  <div class="mt-6 overflow-x-auto rounded-lg border border-[#ededed]">
    <table class="w-full min-w-[860px] text-left text-sm">
<thead class="border-b border-[#e5e5e5] bg-[#f5f5f5] text-[#6e6e6e]">
  <tr>
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
      <td class="px-4 py-5 text-[#6e6e6e]" colspan="6">Loading API keys...</td>
    </tr>
  {:else if apiKeys.items.length === 0}
    <tr>
      <td class="px-4 py-5 text-[#6e6e6e]" colspan="6">No API keys created yet.</td>
    </tr>
  {:else if filteredAPIKeys.length === 0}
    <tr>
      <td class="px-4 py-5 text-[#6e6e6e]" colspan="6">No API keys match your filters.</td>
    </tr>
  {:else}
    {#each filteredAPIKeys as key}
      <tr class="bg-white">
        <td class="px-4 py-3 font-medium text-[#0d0d0d]">{key.name}</td>
        <td class="px-4 py-3 align-middle">
          <div class="inline-flex items-center gap-2 whitespace-nowrap">
            <span class="font-mono text-[13px] text-[#3c3c3c]">{key.prefix}</span>
            {#if !key.revokedAt}
              <button
                class="inline-flex size-7 items-center justify-center rounded-md border border-[#e5e5e5] bg-white text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
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
        <td class="px-4 py-3 text-[#3c3c3c]">{formatDate(key.createdAt)}</td>
        <td class="px-4 py-3 text-[#3c3c3c]">{formatDate(key.lastUsedAt)}</td>
        <td class="px-4 py-3 align-middle">
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
        <td class="whitespace-nowrap px-4 py-3 align-middle text-right">
          <div class="inline-flex items-center justify-end gap-1 whitespace-nowrap">
            <button
              class="inline-flex size-8 items-center justify-center rounded-md border border-[#e5e5e5] bg-white text-[#0d0d0d] hover:bg-[#f5f5f5]"
              type="button"
              onclick={() => openEditModal(key.id)}
              title="Edit key"
              aria-label="Edit key"
            >
              <Pencil class="size-4" aria-hidden="true" />
              <span class="sr-only">Edit key</span>
            </button>
            <button
              class="inline-flex size-8 items-center justify-center rounded-md border border-[#e5e5e5] bg-white text-[#0d0d0d] hover:bg-[#f5f5f5]"
              type="button"
              onclick={() => openKeyLogsModal(key.id)}
              title="View request logs"
              aria-label="View request logs"
            >
              <ScrollText class="size-4" aria-hidden="true" />
              <span class="sr-only">View request logs</span>
            </button>
            <button
              class="inline-flex size-8 items-center justify-center rounded-md border border-[#e5e5e5] bg-white text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
              type="button"
              disabled={Boolean(key.revokedAt)}
              onclick={() => revokeKey(key.id)}
              title="Delete key"
              aria-label="Delete key"
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
</section>

</AuthGate>
