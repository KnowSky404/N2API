<script>
  import { page } from '$app/state';
  import { Plus, RefreshCw, X } from 'lucide-svelte';
  import {
    apiKeys,
    createRoutingPool,
    deleteRoutingPool,
    formatDate,
    getSchedulableProviderAccounts,
    loadKeys,
    loadProviderAccounts,
    loadRoutingPools,
    providerAccounts,
    replaceRoutingPoolAccounts,
    routingPools,
    session,
    updateRoutingPool
  } from '$lib/admin-state.svelte.js';

  import AuthGate from '$lib/AuthGate.svelte';
  let requested = $state(false);
  let appliedRoutingPoolSearch = $state('');
  let selectedRoutingPoolId = $state('all');
  let routingPoolSearch = $state('');
  let routingPoolStatusFilter = $state('all');
  let showCreateModal = $state(false);
  let editingRoutingPoolId = $state(0);
  let editingRoutingPoolDraft = $state(/** @type {import('$lib/admin-state.svelte.js').RoutingPool | null} */ (null));
  const visibleRoutingPools = $derived(
    routingPools.items.filter((pool) => {
      if (selectedRoutingPoolId !== 'all' && String(pool.id) !== selectedRoutingPoolId) return false;
      if (!poolMatchesStatusFilter(pool, routingPoolStatusFilter)) return false;
      const query = routingPoolSearch.trim().toLowerCase();
      if (!query) return true;
      return poolSearchText(pool).includes(query);
    })
  );

  const editingRoutingPool = $derived(editingRoutingPoolDraft);

  /** @param {string} search */
  function applyRoutingPoolURLFilters(search) {
    const params = new URLSearchParams(search);
    const routingPoolId = params.get('routingPoolId') ?? '';
    selectedRoutingPoolId = 'all';
    if (/^[1-9]\d*$/.test(routingPoolId)) {
      selectedRoutingPoolId = routingPoolId;
    }
  }

  $effect(() => {
    if (!session.authenticated) {
      requested = false;
      appliedRoutingPoolSearch = '';
      selectedRoutingPoolId = 'all';
      return;
    }
    if (appliedRoutingPoolSearch !== page.url.search) {
      appliedRoutingPoolSearch = page.url.search;
      applyRoutingPoolURLFilters(page.url.search);
    }
    if (!requested) {
      requested = true;
      void loadRoutingPools();
      void loadProviderAccounts();
      void loadKeys();
    }
  });

  /** @param {SubmitEvent} event */
  async function submitCreatePool(event) {
    event.preventDefault();
    await createRoutingPool();
  }

  function openCreatePoolModal() {
    routingPools.error = '';
    routingPools.newPoolName = '';
    routingPools.newPoolDescription = '';
    routingPools.newPoolFallbackPoolId = '0';
    showCreateModal = true;
  }

  function closeCreatePoolModal() {
    if (routingPools.saving) return;
    showCreateModal = false;
    routingPools.error = '';
    routingPools.newPoolName = '';
    routingPools.newPoolDescription = '';
    routingPools.newPoolFallbackPoolId = '0';
  }

  /** @param {import('$lib/admin-state.svelte.js').RoutingPool} pool */
  function routingPoolLabel(pool) {
    return pool.name || `Pool ${pool.id}`;
  }

  /** @param {import('$lib/admin-state.svelte.js').RoutingPool} pool */
  function openRoutingPoolEditor(pool) {
    editingRoutingPoolId = pool.id;
    editingRoutingPoolDraft = {
      ...pool,
      fallbackPoolId: Number(pool.fallbackPoolId ?? 0),
      accounts: (pool.accounts ?? []).map((account) => ({ ...account })),
      accountIds: [...(pool.accountIds ?? [])]
    };
  }

  function closeRoutingPoolEditor() {
    if (routingPools.saving) return;
    editingRoutingPoolId = 0;
    editingRoutingPoolDraft = null;
  }

  /** @param {SubmitEvent} event */
  async function saveRoutingPool(event) {
    event.preventDefault();
    if (!editingRoutingPoolDraft || routingPools.saving) return;

    const draft = {
      ...editingRoutingPoolDraft,
      accounts: (editingRoutingPoolDraft.accounts ?? []).map((account) => ({ ...account })),
      accountIds: [...(editingRoutingPoolDraft.accountIds ?? [])]
    };
    await updateRoutingPool(draft);
    if (routingPools.error) return;

    const accounts = (draft.accounts ?? [])
      .map((account) => ({
        accountId: Number(account.accountId),
        priority: Math.max(0, Number(account.priority || 0))
      }))
      .filter((account) => account.accountId > 0)
      .sort((a, b) => a.priority - b.priority || a.accountId - b.accountId);
    await replaceRoutingPoolAccounts(draft.id, accounts);
    if (routingPools.error) {
      routingPools.error = `Pool details were saved, but membership failed: ${routingPools.error}`;
      const partiallySaved = routingPools.items.find((pool) => pool.id === draft.id);
      if (partiallySaved) openRoutingPoolEditor(partiallySaved);
      return;
    }

    const saved = routingPools.items.find((pool) => pool.id === draft.id);
    if (saved) openRoutingPoolEditor(saved);
  }

  /**
   * @param {import('$lib/admin-state.svelte.js').RoutingPool} pool
   * @param {number} accountId
   */
  function poolHasAccount(pool, accountId) {
    return (pool.accounts ?? []).some((account) => account.accountId === accountId);
  }

  /**
   * @param {import('$lib/admin-state.svelte.js').RoutingPool} pool
   * @param {number} accountId
   */
  function poolAccountPriority(pool, accountId) {
    return (pool.accounts ?? []).find((account) => account.accountId === accountId)?.priority ?? 0;
  }

  /** @param {import('$lib/admin-state.svelte.js').RoutingPool} pool */
  function poolAccountRows(pool) {
    return [...providerAccounts.items].sort((left, right) => {
      const leftIncluded = poolHasAccount(pool, left.id);
      const rightIncluded = poolHasAccount(pool, right.id);
      if (leftIncluded !== rightIncluded) return leftIncluded ? -1 : 1;
      if (leftIncluded && rightIncluded) {
        return (
          poolAccountPriority(pool, left.id) - poolAccountPriority(pool, right.id) ||
          left.priority - right.priority ||
          accountDisplayName(left).localeCompare(accountDisplayName(right), undefined, {
            numeric: true,
            sensitivity: 'base'
          }) ||
          left.id - right.id
        );
      }
      return (
        left.priority - right.priority ||
        accountDisplayName(left).localeCompare(accountDisplayName(right), undefined, {
          numeric: true,
          sensitivity: 'base'
        }) ||
        left.id - right.id
      );
    });
  }

  /** @param {import('$lib/admin-state.svelte.js').ProviderAccount} account */
  function accountDisplayName(account) {
    return account.displayName || account.name || account.subject || account.provider || `Account ${account.id}`;
  }

  /**
   * @param {import('$lib/admin-state.svelte.js').RoutingPool} pool
   * @param {number} accountId
   * @param {boolean} checked
   */
  function setPoolAccount(pool, accountId, checked) {
    const accounts = [...(pool.accounts ?? [])].filter((account) => account.accountId !== accountId);
    if (checked) accounts.push({ accountId, priority: 0 });
    pool.accounts = accounts;
    pool.accountIds = accounts.map((account) => account.accountId);
  }

  /**
   * @param {import('$lib/admin-state.svelte.js').RoutingPool} pool
   * @param {number} accountId
   * @param {string | number} value
   */
  function setPoolAccountPriority(pool, accountId, value) {
    const priority = Math.max(0, Number(value || 0));
    pool.accounts = (pool.accounts ?? []).map((account) =>
      account.accountId === accountId ? { ...account, priority } : account
    );
  }

  /** @param {import('$lib/admin-state.svelte.js').RoutingPool} pool */
  function boundAPIKeyCount(pool) {
    return apiKeys.items.filter((key) => Number(key.routingPoolId ?? 0) === pool.id && !key.revokedAt).length;
  }

  /** @param {import('$lib/admin-state.svelte.js').RoutingPool} pool */
  function poolSearchText(pool) {
    return [
      pool.name,
      pool.description,
      pool.fallbackPoolName,
      pool.enabled ? 'enabled' : 'disabled',
      (pool.accounts ?? []).map((account) => String(account.accountId)).join(' ')
    ]
      .filter(Boolean)
      .join(' ')
      .toLowerCase();
  }

  /**
   * @param {import('$lib/admin-state.svelte.js').RoutingPool} pool
   * @param {string} filter
   */
  function poolMatchesStatusFilter(pool, filter) {
    if (filter === 'enabled') return pool.enabled;
    if (filter === 'disabled') return !pool.enabled;
    if (filter === 'fallback') return Number(pool.fallbackPoolId ?? 0) > 0;
    if (filter === 'bound_keys') return boundAPIKeyCount(pool) > 0;
    if (filter === 'empty') return (pool.accounts ?? []).length === 0;
    return true;
  }

  /** @param {import('$lib/admin-state.svelte.js').RoutingPool} pool */
  function schedulablePoolMemberCount(pool) {
    const accountIDs = new Set((pool.accounts ?? []).map((account) => Number(account.accountId)));
    return getSchedulableProviderAccounts(providerAccounts.items).filter((account) => accountIDs.has(account.id)).length;
  }

  /** @param {import('$lib/admin-state.svelte.js').RoutingPool} pool */
  function fallbackWarning(pool) {
    const fallbackID = Number(pool.fallbackPoolId ?? 0);
    if (fallbackID <= 0) return '';
    const target = routingPools.items.find((candidate) => candidate.id === fallbackID);
    if (!target) return 'Fallback pool is missing.';
    if (!target.enabled) return 'Fallback pool is disabled.';
    return '';
  }

  /** @param {import('$lib/admin-state.svelte.js').RoutingPool} pool */
  function fallbackPoolHref(pool) {
    const fallbackID = Number(pool.fallbackPoolId ?? 0);
    if (fallbackID <= 0) return '';
    return `/routing-pools?routingPoolId=${encodeURIComponent(String(fallbackID))}`;
  }

  /** @param {import('$lib/admin-state.svelte.js').RoutingPool} pool */
  function routingPoolFallbackChainLabel(pool) {
    const names = [];
    const seen = new Set();
    let current = pool;
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

  /** @param {import('$lib/admin-state.svelte.js').RoutingPool} pool */
  function routingPoolFallbackChainLogsHref(pool) {
    const fallbackID = Number(pool.fallbackPoolId ?? 0);
    if (fallbackID <= 0) return '';
    const chain = routingPoolFallbackChainLabel(pool);
    return `/request-logs?routingPoolChain=${encodeURIComponent(chain)}`;
  }

  /** @param {import('$lib/admin-state.svelte.js').RoutingPool} pool */
  function routingPoolDiagnosticsHref(pool) {
    return `/models?routingPoolId=${encodeURIComponent(String(pool.id))}`;
  }
</script>

<svelte:head>
  <title>N2API Routing Pools</title>
</svelte:head>

<AuthGate>
  <div class="ui-page min-w-0">
    <header class="ui-page-header">
      <div class="ui-page-heading">
        <h1 class="ui-page-title">Routing pools</h1>
        <p class="ui-page-description">
          Group provider accounts into explicit scheduling boundaries. {visibleRoutingPools.length} of {routingPools.items.length} pools shown.
        </p>
      </div>
      <div class="ui-page-actions">
        <button
          class="ui-button ui-button--sm ui-button--primary rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white"
          type="button"
          onclick={openCreatePoolModal}
        >
          <Plus class="size-4" aria-hidden="true" />
          Create pool
        </button>
        {#if selectedRoutingPoolId !== 'all'}
          <button
            class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#d9d9d9] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d]"
            type="button"
            onclick={() => (selectedRoutingPoolId = 'all')}
          >
            Show all pools
          </button>
        {/if}
        <button
          class="ui-button ui-button--icon ui-button--secondary rounded-lg border border-[#d9d9d9] bg-white text-[#0d0d0d] disabled:cursor-not-allowed disabled:opacity-60"
          disabled={routingPools.loading}
          onclick={() => loadRoutingPools()}
          aria-label={routingPools.loading ? 'Refreshing routing pools' : 'Refresh routing pools'}
          title="Refresh routing pools"
        >
          <RefreshCw class={routingPools.loading ? 'size-4 animate-spin' : 'size-4'} aria-hidden="true" />
        </button>
      </div>
    </header>


    {#if routingPools.error && !showCreateModal}
      <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">
        {routingPools.error}
      </p>
    {/if}

    {#if showCreateModal}
      <!-- svelte-ignore a11y_click_events_have_key_events,a11y_no_static_element_interactions,a11y_interactive_supports_focus -->
      <div
        class="ui-modal-backdrop fixed inset-0 z-50 flex items-center justify-center bg-black/30 p-4"
        role="dialog"
        aria-modal="true"
        aria-label="Create routing pool"
      >
        <div class="ui-modal-panel ui-modal-panel--md w-full max-w-lg max-h-[calc(100vh-4rem)] overflow-y-auto rounded-lg border border-[#ededed] bg-white p-6 shadow-lg">
          <div class="mb-4 flex items-center justify-between">
            <h3 class="text-lg font-semibold text-[#0d0d0d]">Create routing pool</h3>
            <button
              class="ui-button ui-button--icon ui-button--secondary inline-flex size-8 items-center justify-center rounded-md border border-[#e5e5e5] bg-white text-[#6e6e6e] hover:bg-[#f5f5f5] hover:text-[#0d0d0d]"
              type="button"
              disabled={routingPools.saving}
              onclick={closeCreatePoolModal}
              aria-label="Close create routing pool modal"
              title="Close"
            >
              <X class="size-4" aria-hidden="true" />
            </button>
          </div>

          {#if routingPools.error}
            <p class="mb-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">
              {routingPools.error}
            </p>
          {/if}

          <form class="space-y-4" onsubmit={submitCreatePool}>
            <div class="space-y-4 rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
            <label class="grid gap-2 text-sm font-medium text-[#3c3c3c]">
              Pool name
              <input
                class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-base text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                bind:value={routingPools.newPoolName}
                placeholder="Primary Codex"
                required
              />
            </label>
            <label class="grid gap-2 text-sm font-medium text-[#3c3c3c]">
              Description
              <input
                class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-base text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                bind:value={routingPools.newPoolDescription}
                placeholder="Daily gateway pool"
              />
            </label>
            <label class="grid gap-2 text-sm font-medium text-[#3c3c3c]">
              Fallback pool
              <select
                class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-base text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                bind:value={routingPools.newPoolFallbackPoolId}
              >
                <option value="0">Create with no fallback</option>
                {#each routingPools.items as candidate}
                  <option value={String(candidate.id)}>{candidate.name}</option>
                {/each}
              </select>
            </label>
            </div>
            <div class="ui-modal-actions flex justify-end gap-3">
              <button class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]" type="button" disabled={routingPools.saving} onclick={closeCreatePoolModal}>Cancel</button>
              <button class="ui-button ui-button--sm ui-button--primary rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white disabled:cursor-not-allowed disabled:opacity-60" type="submit" disabled={routingPools.saving}>
                {routingPools.saving ? "Saving" : "Save"}
              </button>
            </div>
          </form>
        </div>
      </div>
    {/if}

    <div class="mt-5 grid gap-3 grid-cols-1 sm:grid-cols-[minmax(240px,1fr)_220px]">
      <label class="grid gap-1 text-sm font-medium text-[#3c3c3c]">
        Search pools
        <input
          class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          type="search"
          placeholder="Search pools"
          bind:value={routingPoolSearch}
        />
      </label>
      <label class="grid gap-1 text-sm font-medium text-[#3c3c3c]">
        Status filter
        <select
          class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={routingPoolStatusFilter}
        >
          <option value="all">All pools</option>
          <option value="enabled">Enabled pools</option>
          <option value="disabled">Disabled pools</option>
          <option value="fallback">Pools with fallback</option>
          <option value="bound_keys">Bound key pools</option>
          <option value="empty">Empty pools</option>
        </select>
      </label>
    </div>

    {#if routingPools.loading}
      <p class="ui-loading-state mt-6 text-sm text-[#6e6e6e]" aria-live="polite">Loading routing pools...</p>
    {:else if routingPools.items.length === 0}
      <p class="mt-6 rounded-lg border border-dashed border-[#d9d9d9] bg-[#fafafa] p-6 text-sm text-[#6e6e6e]">
        No routing pools configured.
      </p>
    {:else if visibleRoutingPools.length === 0}
      <p class="mt-6 rounded-lg border border-dashed border-[#d9d9d9] bg-[#fafafa] p-6 text-sm text-[#6e6e6e]">
        No routing pool matches this link.
      </p>
    {:else}
      <div class="ui-table-shell mt-6 overflow-x-auto rounded-lg border border-[#ededed]">
        <table class="ui-table w-full min-w-[980px] text-left text-sm">
          <thead class="border-b border-[#e5e5e5] bg-[#f5f5f5] text-[#6e6e6e]">
            <tr>
              <th class="px-4 py-3 font-medium">Pool</th>
              <th class="px-4 py-3 font-medium">Enabled</th>
              <th class="px-4 py-3 font-medium">Fallback</th>
              <th class="px-4 py-3 font-medium">Members</th>
              <th class="px-4 py-3 font-medium">Schedulable</th>
              <th class="px-4 py-3 font-medium">Bound keys</th>
              <th class="px-4 py-3 font-medium">Created</th>
              <th class="sticky right-0 z-10 bg-[#f5f5f5] px-3 py-3 text-right font-medium shadow-[-8px_0_12px_rgba(255,255,255,0.85)]">Actions</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-[#ededed]">
            {#each visibleRoutingPools as pool (pool.id)}
              <tr class="bg-white align-top">
                <td class="px-4 py-3 align-middle">
                  <p class="max-w-[22rem] truncate font-medium text-[#0d0d0d]">{routingPoolLabel(pool)}</p>
                  {#if pool.description}
                    <p class="mt-1 max-w-[22rem] truncate text-[#6e6e6e]">{pool.description}</p>
                  {/if}
                </td>
                <td class="px-4 py-3 align-middle">
                  <label class="inline-flex items-center gap-0 text-sm font-medium text-[#3c3c3c]" title={pool.enabled ? 'Enabled' : 'Disabled'}>
                    <input
                      class="peer sr-only"
                      type="checkbox"
                      role="switch"
                      checked={pool.enabled}
                      disabled={routingPools.saving}
                      aria-label={`Set ${routingPoolLabel(pool)} ${pool.enabled ? 'disabled' : 'enabled'}`}
                      onchange={(event) => {
                        pool.enabled = event.currentTarget.checked;
                        void updateRoutingPool(pool);
                      }}
                    />
                    <span class="relative inline-flex h-5 w-9 shrink-0 rounded-full bg-[#d9d9d9] transition-colors after:absolute after:left-0.5 after:top-0.5 after:size-4 after:rounded-full after:bg-white after:shadow-sm after:transition-transform peer-checked:bg-[#10a37f] peer-checked:after:translate-x-4 peer-focus-visible:outline peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-[#10a37f] peer-disabled:cursor-not-allowed peer-disabled:opacity-60"></span>
                  </label>
                </td>
                <td class="px-4 py-3 align-middle">
                  {#if pool.fallbackPoolName}
                    <a
                      class="text-[#0a7a5e] underline-offset-2 hover:underline"
                      href={fallbackPoolHref(pool)}
                    >
                      {pool.fallbackPoolName}
                    </a>
                    {#if fallbackWarning(pool)}
                      <span class="mt-1 block rounded-md bg-amber-50 p-1 text-xs leading-5 text-amber-700">{fallbackWarning(pool)}</span>
                    {/if}
                  {:else}
                    <span class="text-[#9b9b9b]">—</span>
                  {/if}
                </td>
                <td class="px-4 py-3 align-middle font-mono tabular-nums text-[#0d0d0d]">{(pool.accounts ?? []).length}</td>
                <td class="px-4 py-3 align-middle font-mono tabular-nums text-[#0d0d0d]">{schedulablePoolMemberCount(pool)}</td>
                <td class="px-4 py-3 align-middle font-mono tabular-nums text-[#0d0d0d]">{boundAPIKeyCount(pool)}</td>
                <td class="whitespace-nowrap px-4 py-3 align-middle text-[#3c3c3c]">{formatDate(pool.createdAt)}</td>
                <td class="sticky right-0 bg-white px-3 py-3 align-middle shadow-[-8px_0_12px_rgba(255,255,255,0.85)]">
                  <div class="relative flex justify-end gap-2 whitespace-nowrap">
                    <button
                      class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]"
                      type="button"
                      disabled={routingPools.saving}
                      onclick={() => openRoutingPoolEditor(pool)}
                      title="Edit pool"
                      aria-label="Edit pool"
                    >
                      Edit
                    </button>
                    <button
                      class="ui-button ui-button--sm ui-button--danger rounded-lg border border-red-200 bg-white px-2.5 py-1.5 text-xs font-medium text-red-700 hover:bg-red-50 disabled:cursor-not-allowed disabled:opacity-60"
                      type="button"
                      disabled={routingPools.saving}
                      onclick={() => deleteRoutingPool(pool.id)}
                      title="Delete pool"
                      aria-label="Delete pool"
                    >
                      Delete
                    </button>
                  </div>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  </div>

{#if editingRoutingPool}
  {@const pool = editingRoutingPool}
  <!-- svelte-ignore a11y_click_events_have_key_events,a11y_no_static_element_interactions,a11y_interactive_supports_focus -->
  <div
    class="ui-modal-backdrop ui-modal-backdrop--top fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/30 px-4 py-[6vh]"
    role="presentation"
  >
    <div class="ui-modal-panel ui-modal-panel--xl w-full max-w-4xl rounded-xl bg-white p-5 shadow-xl" role="dialog" aria-modal="true" aria-label={`Edit ${routingPoolLabel(pool)}`}>
      <form class="grid gap-5" onsubmit={saveRoutingPool}>
      <div class="flex items-start justify-between gap-4 border-b border-[#ededed] pb-4">
        <div class="min-w-0">
          <h2 class="truncate text-lg font-semibold text-[#0d0d0d]">Edit routing pool</h2>
          <p class="mt-1 truncate text-sm text-[#6e6e6e]">{routingPoolLabel(pool)}</p>
        </div>
        <button
          class="ui-button ui-button--icon ui-button--secondary inline-flex size-8 shrink-0 items-center justify-center rounded-md border border-[#e5e5e5] bg-white text-[#0d0d0d] hover:bg-[#f5f5f5]"
          type="button"
          disabled={routingPools.saving}
          onclick={closeRoutingPoolEditor}
          aria-label="Close edit pool modal"
          title="Close"
        >
          <X class="size-4" aria-hidden="true" />
        </button>
      </div>

      <div class="grid gap-5">
        <div class="grid gap-4 sm:grid-cols-2">
          <label class="grid gap-1 text-xs font-medium text-[#3c3c3c]" for={`routing-pool-name-${pool.id}`}>
            Name
            <input
              id={`routing-pool-name-${pool.id}`}
              class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
              bind:value={pool.name}
            />
          </label>
          <label class="grid gap-1 text-xs font-medium text-[#3c3c3c]" for={`routing-pool-description-${pool.id}`}>
            Description
            <input
              id={`routing-pool-description-${pool.id}`}
              class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
              bind:value={pool.description}
            />
          </label>
        </div>

        <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          <label class="grid gap-1 text-xs font-medium text-[#3c3c3c]" for={`routing-pool-fallback-${pool.id}`}>
            Fallback pool
            <select
              id={`routing-pool-fallback-${pool.id}`}
              class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
              bind:value={pool.fallbackPoolId}
            >
              <option value={0}>No fallback</option>
              {#each routingPools.items as candidate}
                <option value={candidate.id} disabled={pool.id === candidate.id}>{candidate.name}</option>
              {/each}
            </select>
            {#if fallbackPoolHref(pool)}
              <a
                class="mt-1 inline-flex text-xs font-medium text-[#0a7a5e] underline-offset-2 hover:underline"
                href={fallbackPoolHref(pool)}
              >
                Open fallback pool
              </a>
            {/if}
            {#if fallbackWarning(pool)}
              <span class="mt-1 block rounded-md border border-amber-200 bg-amber-50 p-2 text-xs leading-5 text-amber-800">{fallbackWarning(pool)}</span>
            {/if}
          </label>
          <label class="inline-flex items-center gap-2 text-sm font-medium text-[#3c3c3c]" title={pool.enabled ? 'Enabled' : 'Disabled'}>
            <input
              class="peer sr-only"
              type="checkbox"
              role="switch"
              bind:checked={pool.enabled}
              disabled={routingPools.saving}
              aria-label={`Set ${routingPoolLabel(pool)} ${pool.enabled ? 'disabled' : 'enabled'}`}
            />
            <span class="relative inline-flex h-5 w-9 shrink-0 rounded-full bg-[#d9d9d9] transition-colors after:absolute after:left-0.5 after:top-0.5 after:size-4 after:rounded-full after:bg-white after:shadow-sm after:transition-transform peer-checked:bg-[#10a37f] peer-checked:after:translate-x-4 peer-focus-visible:outline peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-[#10a37f] peer-disabled:cursor-not-allowed peer-disabled:opacity-60"></span>
            <span class="text-xs text-[#6e6e6e]">{pool.enabled ? 'Enabled' : 'Disabled'}</span>
          </label>
        </div>

        <dl class="grid gap-3 sm:grid-cols-3">
          <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
            <dt class="text-xs font-medium text-[#6e6e6e]">Pool members</dt>
            <dd class="mt-2 font-mono text-sm font-semibold text-[#0d0d0d]">{(pool.accounts ?? []).length}</dd>
          </div>
          <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
            <dt class="text-xs font-medium text-[#6e6e6e]">Schedulable members</dt>
            <dd class="mt-2 font-mono text-sm font-semibold text-[#0d0d0d]">{schedulablePoolMemberCount(pool)}</dd>
          </div>
          <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
            <dt class="text-xs font-medium text-[#6e6e6e]">Bound API keys</dt>
            <dd class="mt-2 font-mono text-sm font-semibold text-[#0d0d0d]">{boundAPIKeyCount(pool)}</dd>
          </div>
        </dl>
      </div>

      <div class="border-t border-[#ededed] pt-4">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h3 class="text-base font-semibold text-[#0d0d0d]">Pool accounts</h3>
            <p class="mt-1 text-sm text-[#6e6e6e]">Created {formatDate(pool.createdAt)}. Pool priority overrides account priority inside this pool.</p>
          </div>
          <div class="flex flex-wrap gap-2">
            <a
              class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#d9d9d9] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]"
              href={`/request-logs?routingPoolId=${pool.id}`}
              title="View request logs"
              aria-label="View request logs"
            >
              Logs
            </a>
            {#if routingPoolFallbackChainLogsHref(pool)}
              <a
                class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#d9d9d9] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]"
                href={routingPoolFallbackChainLogsHref(pool)}
                title="View fallback chain logs"
                aria-label="View fallback chain logs"
              >
                Chain logs
              </a>
            {/if}
            <a
              class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#d9d9d9] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]"
              href={`/api-keys?routingPoolId=${pool.id}`}
              title="View API keys"
              aria-label="View API keys"
            >
              API keys
            </a>
            <a
              class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#d9d9d9] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]"
              href={routingPoolDiagnosticsHref(pool)}
              title="View routing diagnostics"
              aria-label="View routing diagnostics"
            >
              Diagnostics
            </a>
          </div>
        </div>

        {#if providerAccounts.loading}
          <p class="ui-loading-state mt-4 text-sm text-[#6e6e6e]" aria-live="polite">Loading provider accounts...</p>
        {:else if providerAccounts.items.length === 0}
          <p class="mt-4 text-sm text-[#6e6e6e]">No provider accounts available.</p>
        {:else}
          <div class="ui-table-shell mt-4 overflow-x-auto">
            <table class="ui-table min-w-full divide-y divide-[#ededed] text-left text-sm">
              <thead class="text-xs text-[#6e6e6e]">
                <tr>
                  <th class="py-2 pr-3 font-medium">Include</th>
                  <th class="px-3 py-2 font-medium">Account</th>
                  <th class="px-3 py-2 font-medium">Type</th>
                  <th class="px-3 py-2 font-medium">Status</th>
                  <th class="px-3 py-2 font-medium">Pool priority</th>
                  <th class="px-3 py-2 text-right font-medium">Action</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-[#f3f3f3]">
                {#each poolAccountRows(pool) as account (account.id)}
                  <tr>
                    <td class="py-2 pr-3">
                      <input
                        type="checkbox"
                        checked={poolHasAccount(pool, account.id)}
                        onchange={(event) => setPoolAccount(pool, account.id, event.currentTarget.checked)}
                      />
                    </td>
                    <td class="px-3 py-2">
                      <div class="font-medium text-[#0d0d0d]">{accountDisplayName(account)}</div>
                      <div class="font-mono text-xs text-[#6e6e6e]">#{account.id}</div>
                    </td>
                    <td class="px-3 py-2 text-[#3c3c3c]">{account.accountType}</td>
                    <td class="px-3 py-2 text-[#3c3c3c]">{account.enabled ? account.status : 'disabled'}</td>
                    <td class="px-3 py-2">
                      <input
                        class="w-24 rounded-lg border border-[#e5e5e5] bg-white px-2 py-1 text-sm text-[#0d0d0d]"
                        type="number"
                        min="0"
                        value={poolAccountPriority(pool, account.id)}
                        disabled={!poolHasAccount(pool, account.id)}
                        onchange={(event) => setPoolAccountPriority(pool, account.id, event.currentTarget.value)}
                      />
                    </td>
                    <td class="px-3 py-2 text-right">
                      <a
                        class="ui-button ui-button--sm ui-button--secondary inline-flex rounded-lg border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]"
                        href={`/providers?providerAccountId=${account.id}`}
                        title="View provider account"
                        aria-label="View provider account"
                      >
                        Provider
                      </a>
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}

      </div>
      <div class="grid gap-3 border-t border-[#ededed] pt-4">
        {#if routingPools.error}
          <p class="rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700" role="alert">{routingPools.error}</p>
        {/if}
        <div class="ui-modal-actions flex justify-end gap-3">
          <button class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]" type="button" disabled={routingPools.saving} onclick={closeRoutingPoolEditor}>Cancel</button>
          <button class="ui-button ui-button--sm ui-button--primary rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white disabled:cursor-not-allowed disabled:opacity-60" type="submit" disabled={routingPools.saving}>{routingPools.saving ? 'Saving' : 'Save'}</button>
        </div>
      </div>
      </form>
    </div>
  </div>
{/if}
</AuthGate>
