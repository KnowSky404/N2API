<script>
  import { page } from '$app/state';
  import {
    apiKeys,
    createRoutingPool,
    deleteRoutingPool,
    formatDate,
    getSchedulableProviderAccounts,
    loadKeys,
    loadProviderAccounts,
    loadRoutingPools,
    login,
    loginForm,
    providerAccounts,
    replaceRoutingPoolAccounts,
    routingPools,
    session,
    updateRoutingPool
  } from '$lib/admin-state.svelte.js';

  let requested = $state(false);
  let appliedRoutingPoolSearch = $state('');
  let selectedRoutingPoolId = $state('all');
  const visibleRoutingPools = $derived(
    selectedRoutingPoolId === 'all'
      ? routingPools.items
      : routingPools.items.filter((pool) => String(pool.id) === selectedRoutingPoolId)
  );

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
  function submitCreatePool(event) {
    event.preventDefault();
    void createRoutingPool();
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
  function saveMembership(pool) {
    const accounts = [...(pool.accounts ?? [])]
      .map((account) => ({
        accountId: Number(account.accountId),
        priority: Math.max(0, Number(account.priority || 0))
      }))
      .filter((account) => account.accountId > 0)
      .sort((a, b) => a.priority - b.priority || a.accountId - b.accountId);
    void replaceRoutingPoolAccounts(pool.id, accounts);
  }

  /** @param {import('$lib/admin-state.svelte.js').RoutingPool} pool */
  function boundAPIKeyCount(pool) {
    return apiKeys.items.filter((key) => Number(key.routingPoolId ?? 0) === pool.id && !key.revokedAt).length;
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
</script>

<svelte:head>
  <title>N2API Routing Pools</title>
</svelte:head>

{#if session.loading}
  <section class="rounded-lg border border-[#ededed] bg-white p-6 text-sm text-[#6e6e6e]">
    Loading admin session...
  </section>
{:else if !session.authenticated}
  <section class="grid gap-6 lg:grid-cols-[minmax(0,1fr)_minmax(320px,420px)]">
    <div class="rounded-lg border border-[#ededed] bg-white p-6">
      <h2 class="text-2xl font-semibold leading-tight text-[#0d0d0d]">Admin access</h2>
      <p class="mt-3 max-w-2xl text-sm leading-6 text-[#3c3c3c]">
        Sign in to manage this personal gateway.
      </p>
    </div>

    <form class="rounded-lg border border-[#ededed] bg-white p-6" onsubmit={login}>
      <h2 class="text-lg font-semibold text-[#0d0d0d]">Admin sign in</h2>
      <label class="mt-5 block text-sm font-medium text-[#3c3c3c]">
        Username
        <input class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-base text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={loginForm.username} autocomplete="username" required />
      </label>
      <label class="mt-4 block text-sm font-medium text-[#3c3c3c]">
        Password
        <input class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-base text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" type="password" bind:value={loginForm.password} autocomplete="current-password" required />
      </label>
      {#if loginForm.error}
        <p class="mt-3 text-sm text-red-700">{loginForm.error}</p>
      {/if}
      <button class="mt-5 rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white disabled:cursor-not-allowed disabled:opacity-60" disabled={loginForm.submitting}>
        {loginForm.submitting ? 'Signing in' : 'Sign in'}
      </button>
    </form>
  </section>
{:else}
  <section class="rounded-lg border border-[#ededed] bg-white p-6">
    <div class="flex flex-wrap items-center justify-between gap-4">
      <div>
        <h2 class="text-2xl font-semibold leading-tight text-[#0d0d0d]">Routing pools</h2>
        <p class="mt-1 text-sm text-[#6e6e6e]">
          Signed in as {session.username}. {visibleRoutingPools.length} of {routingPools.items.length} configured pools shown.
        </p>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        {#if selectedRoutingPoolId !== 'all'}
          <button
            class="rounded-lg border border-[#d9d9d9] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d]"
            type="button"
            onclick={() => (selectedRoutingPoolId = 'all')}
          >
            Show all pools
          </button>
        {/if}
        <button
          class="rounded-lg border border-[#d9d9d9] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] disabled:cursor-not-allowed disabled:opacity-60"
          disabled={routingPools.loading}
          onclick={() => loadRoutingPools()}
        >
          {routingPools.loading ? 'Refreshing' : 'Refresh'}
        </button>
      </div>
    </div>

    {#if routingPools.error}
      <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">
        {routingPools.error}
      </p>
    {/if}

    <form class="mt-6 grid gap-3 rounded-lg border border-[#ededed] bg-[#fafafa] p-4 lg:grid-cols-[minmax(180px,240px)_minmax(0,1fr)_minmax(180px,240px)_auto]" onsubmit={submitCreatePool}>
      <label class="text-sm font-medium text-[#3c3c3c]">
        Pool name
        <input
          class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-base text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={routingPools.newPoolName}
          placeholder="Primary Codex"
          required
        />
      </label>
      <label class="text-sm font-medium text-[#3c3c3c]">
        Description
        <input
          class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-base text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={routingPools.newPoolDescription}
          placeholder="Daily gateway pool"
        />
      </label>
      <label class="text-sm font-medium text-[#3c3c3c]">
        Fallback pool
        <select
          class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-base text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={routingPools.newPoolFallbackPoolId}
        >
          <option value="0">Create with no fallback</option>
          {#each routingPools.items as candidate}
            <option value={String(candidate.id)}>{candidate.name}</option>
          {/each}
        </select>
      </label>
      <div class="flex items-end">
        <button class="w-full rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white disabled:cursor-not-allowed disabled:opacity-60" disabled={routingPools.saving}>
          {routingPools.saving ? 'Saving' : 'Create pool'}
        </button>
      </div>
    </form>

    {#if routingPools.loading}
      <p class="mt-6 text-sm text-[#6e6e6e]">Loading routing pools...</p>
    {:else if routingPools.items.length === 0}
      <p class="mt-6 rounded-lg border border-dashed border-[#d9d9d9] bg-[#fafafa] p-6 text-sm text-[#6e6e6e]">
        No routing pools configured.
      </p>
    {:else if visibleRoutingPools.length === 0}
      <p class="mt-6 rounded-lg border border-dashed border-[#d9d9d9] bg-[#fafafa] p-6 text-sm text-[#6e6e6e]">
        No routing pool matches this link.
      </p>
    {:else}
      <div class="mt-6 grid gap-4">
        {#each visibleRoutingPools as pool (pool.id)}
          <article id={`routing-pool-${pool.id}`} class="scroll-mt-6 rounded-lg border border-[#ededed] bg-white p-4">
            <div class="grid gap-3 lg:grid-cols-[minmax(180px,260px)_minmax(0,1fr)_auto]">
              <label class="text-sm font-medium text-[#3c3c3c]">
                Name
                <input class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={pool.name} />
              </label>
              <label class="text-sm font-medium text-[#3c3c3c]">
                Description
                <input class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={pool.description} />
              </label>
              <label class="text-sm font-medium text-[#3c3c3c]">
                Fallback pool
                <select
                  class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                  bind:value={pool.fallbackPoolId}
                >
                  <option value={0}>No fallback</option>
                  {#each routingPools.items as candidate}
                    <option value={candidate.id} disabled={pool.id === candidate.id}>{candidate.name}</option>
                  {/each}
                </select>
                {#if fallbackPoolHref(pool)}
                  <a
                    class="mt-2 inline-flex text-xs font-medium text-[#0a7a5e] underline-offset-2 hover:underline"
                    href={fallbackPoolHref(pool)}
                  >
                    Open fallback pool
                  </a>
                {/if}
                {#if fallbackWarning(pool)}
                  <span class="mt-2 block rounded-md border border-amber-200 bg-amber-50 p-2 text-xs leading-5 text-amber-800">{fallbackWarning(pool)}</span>
                {/if}
              </label>
              <div class="flex items-end gap-2">
                <label class="flex items-center gap-2 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#3c3c3c]">
                  <input type="checkbox" bind:checked={pool.enabled} />
                  Enabled
                </label>
                <button class="rounded-lg bg-[#0d0d0d] px-3 py-2 text-sm font-medium text-white disabled:cursor-not-allowed disabled:opacity-60" disabled={routingPools.saving} onclick={() => updateRoutingPool(pool)}>
                  Save
                </button>
                <button class="rounded-lg border border-red-200 bg-white px-3 py-2 text-sm font-medium text-red-700 disabled:cursor-not-allowed disabled:opacity-60" disabled={routingPools.saving} onclick={() => deleteRoutingPool(pool.id)}>
                  Delete
                </button>
              </div>
            </div>

            <div class="mt-4 flex flex-wrap items-center justify-between gap-3 border-t border-[#ededed] pt-4">
              <div>
                <h3 class="text-base font-semibold text-[#0d0d0d]">Pool accounts</h3>
                <p class="mt-1 text-sm text-[#6e6e6e]">Created {formatDate(pool.createdAt)}. Pool priority overrides account priority inside this pool.</p>
              </div>
              <div class="flex flex-wrap gap-2">
                <a
                  class="rounded-lg border border-[#d9d9d9] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]"
                  href={`/request-logs?routingPoolId=${pool.id}`}
                  title="View request logs"
                  aria-label="View request logs"
                >
                  Logs
                </a>
                <a
                  class="rounded-lg border border-[#d9d9d9] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]"
                  href={`/api-keys?routingPoolId=${pool.id}`}
                  title="View API keys"
                  aria-label="View API keys"
                >
                  API keys
                </a>
                <button class="rounded-lg border border-[#d9d9d9] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] disabled:cursor-not-allowed disabled:opacity-60" disabled={routingPools.saving} onclick={() => saveMembership(pool)}>
                  Save membership
                </button>
              </div>
            </div>

            <dl class="mt-4 grid gap-3 sm:grid-cols-3">
              <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
                <dt class="text-xs font-medium uppercase text-[#6e6e6e]">Pool members</dt>
                <dd class="mt-2 font-mono text-sm font-semibold text-[#0d0d0d]">{(pool.accounts ?? []).length}</dd>
              </div>
              <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
                <dt class="text-xs font-medium uppercase text-[#6e6e6e]">Schedulable members</dt>
                <dd class="mt-2 font-mono text-sm font-semibold text-[#0d0d0d]">{providerAccounts.loading ? 'Loading' : schedulablePoolMemberCount(pool)}</dd>
              </div>
              <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
                <dt class="text-xs font-medium uppercase text-[#6e6e6e]">Bound API keys</dt>
                <dd class="mt-2 font-mono text-sm font-semibold text-[#0d0d0d]">{apiKeys.loading ? 'Loading' : boundAPIKeyCount(pool)}</dd>
              </div>
            </dl>

            {#if providerAccounts.loading}
              <p class="mt-4 text-sm text-[#6e6e6e]">Loading provider accounts...</p>
            {:else if providerAccounts.items.length === 0}
              <p class="mt-4 text-sm text-[#6e6e6e]">No provider accounts available.</p>
            {:else}
              <div class="mt-4 overflow-x-auto">
                <table class="min-w-full divide-y divide-[#ededed] text-left text-sm">
                  <thead class="text-xs uppercase text-[#6e6e6e]">
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
                            class="inline-flex rounded-lg border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]"
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
          </article>
        {/each}
      </div>
    {/if}
  </section>
{/if}
