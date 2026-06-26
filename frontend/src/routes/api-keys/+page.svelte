<script>
  import { page } from '$app/state';
  import {
    apiKeys,
    apiKeyModelWarnings,
    copySecret,
    createKey,
    formatCostMicrousd,
    formatDate,
    formatTokens,
    gatewayLimitLabel,
    gatewaySettings,
    getActiveKeys,
    loadGatewaySettings,
    loadModelRouting,
    loadRoutingPools,
    loadUsageSummary,
    login,
    loginForm,
    modelListText,
    modelRouting,
    modelSettings,
    revokeKey,
    routingPools,
    saveModelSettings,
    session,
    setAPIKeyDisabled,
    updateAPIKeyBudgets,
    updateAPIKeyLimits,
    updateAPIKeyName,
    updateAPIKeyModelPolicy,
    updateAPIKeyRoutingPool,
    usage,
  } from '$lib/admin-state.svelte.js';

  const activeKeys = $derived(getActiveKeys());
  const usage24hClientKeys = $derived(usage.summaries['24h:client_key'] ?? null);
  let keySearch = $state('');
  let keyStatusFilter = $state('all');
  let modelRoutingRequested = $state(false);
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

  /** @param {import('$lib/admin-state.svelte.js').UsageSummaryRow} row */
  function clientKeyUsageHref(row) {
    const id = String(row.id ?? '').split('/').pop() ?? '';
    if (!/^[1-9]\d*$/.test(id)) return '';
    return `/request-logs?clientKeyId=${encodeURIComponent(id)}`;
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
    return `/models?model=${encodeURIComponent(value)}&status=blocked${poolQuery}`;
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
      void loadUsageSummary('24h', 'client_key');
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
      key.tokenBudgetExceeded ? 'token budget exceeded' : ''
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
      names.push(current.name || `Pool ${current.id}`);
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
{#if session.error}
  <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">
    {session.error}
  </p>
{/if}
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
<h2 class="text-2xl font-semibold leading-tight text-[#0d0d0d]">API keys</h2>
<p class="mt-1 text-sm text-[#6e6e6e]">
  Signed in as {session.username}. {activeKeys.length} active
  {activeKeys.length === 1 ? 'key' : 'keys'}.
</p>
    </div>
  </div>

  <form class="mt-6 flex flex-col gap-3 sm:flex-row" onsubmit={createKey}>
    <label class="min-w-0 flex-1">
<span class="text-sm font-medium text-[#3c3c3c]">New key name</span>
<input
  class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-base text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
  bind:value={apiKeys.newKeyName}
  placeholder="Codex workstation"
  required
/>
    </label>
    <div class="flex items-end">
<button
  class="w-full rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white disabled:cursor-not-allowed disabled:opacity-60 sm:w-auto"
  disabled={apiKeys.creating}
>
  {apiKeys.creating ? 'Creating' : 'Create key'}
</button>
    </div>
  </form>

  <section class="mt-6 rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div>
        <h3 class="text-base font-semibold text-[#0d0d0d]">Gateway runtime limits</h3>
        <p class="mt-1 text-sm text-[#6e6e6e]">Current concurrency and rate guards loaded from the running service.</p>
      </div>
    </div>
    {#if gatewaySettings.error}
      <p class="mt-3 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">
        {gatewaySettings.error}
      </p>
    {:else if gatewaySettings.loading || !gatewaySettings.data}
      <p class="mt-4 text-sm text-[#6e6e6e]">Loading gateway runtime limits...</p>
    {:else}
      <dl class="mt-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
        <div class="rounded-md border border-[#ededed] bg-white p-3">
          <dt class="text-xs font-medium uppercase tracking-wide text-[#6e6e6e]">Gateway concurrency</dt>
          <dd class="mt-2 font-mono text-lg text-[#0d0d0d]">{gatewayLimitLabel(gatewaySettings.data.maxConcurrentGatewayRequests)}</dd>
        </div>
        <div class="rounded-md border border-[#ededed] bg-white p-3">
          <dt class="text-xs font-medium uppercase tracking-wide text-[#6e6e6e]">Per account concurrency</dt>
          <dd class="mt-2 font-mono text-lg text-[#0d0d0d]">{gatewayLimitLabel(gatewaySettings.data.maxConcurrentRequestsPerAccount)}</dd>
        </div>
        <div class="rounded-md border border-[#ededed] bg-white p-3">
          <dt class="text-xs font-medium uppercase tracking-wide text-[#6e6e6e]">Per key concurrency</dt>
          <dd class="mt-2 font-mono text-lg text-[#0d0d0d]">{gatewayLimitLabel(gatewaySettings.data.maxConcurrentRequestsPerKey)}</dd>
        </div>
        <div class="rounded-md border border-[#ededed] bg-white p-3">
          <dt class="text-xs font-medium uppercase tracking-wide text-[#6e6e6e]">Requests per minute</dt>
          <dd class="mt-2 font-mono text-lg text-[#0d0d0d]">{gatewayLimitLabel(gatewaySettings.data.requestsPerMinutePerKey)}</dd>
        </div>
        <div class="rounded-md border border-[#ededed] bg-white p-3">
          <dt class="text-xs font-medium uppercase tracking-wide text-[#6e6e6e]">Tokens per minute</dt>
          <dd class="mt-2 font-mono text-lg text-[#0d0d0d]">{gatewayLimitLabel(gatewaySettings.data.tokensPerMinutePerKey)}</dd>
        </div>
      </dl>
    {/if}
  </section>

  <form class="mt-6 rounded-lg border border-[#ededed] bg-[#fafafa] p-4" onsubmit={saveModelSettings}>
    <div class="grid gap-4 lg:grid-cols-[minmax(220px,320px)_minmax(0,1fr)]">
      <label class="block text-sm font-medium text-[#3c3c3c]">
Gateway default model
<input
  class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] leading-6 text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
  bind:value={modelSettings.defaultModel}
  maxlength="128"
  placeholder="gpt-4.1"
  required
/>
      </label>

      <label class="block text-sm font-medium text-[#3c3c3c]">
All routable models
<textarea
  class="mt-2 min-h-28 w-full resize-y rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] leading-6 text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
  bind:value={modelSettings.allowedModelsText}
  placeholder={'gpt-4.1\ngpt-4.1-mini'}
  required
></textarea>
      </label>
    </div>
    <div class="mt-4 flex flex-wrap items-center justify-between gap-3">
      <p class="text-sm text-[#6e6e6e]">
        Client keys can use all routable models or a selected subset from this gateway list.
      </p>
      <button
        class="rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white disabled:cursor-not-allowed disabled:opacity-60"
        disabled={modelSettings.loading || modelSettings.saving}
      >
        {modelSettings.saving ? 'Saving' : 'Save model settings'}
      </button>
    </div>
    {#if modelSettings.saved}
      <p class="mt-3 text-sm text-[#0a7a5e]">Model settings saved.</p>
    {/if}
    {#if modelSettings.error}
      <p class="mt-3 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">
        {modelSettings.error}
      </p>
    {/if}
  </form>

  <section class="mt-6 rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <h3 class="text-base font-semibold text-[#0d0d0d]">24h key usage</h3>
        <p class="mt-1 text-sm text-[#6e6e6e]">Gateway traffic distribution by client API key.</p>
      </div>
      {#if usage.loading}
        <span class="text-sm text-[#6e6e6e]">Loading...</span>
      {/if}
    </div>
    {#if usage.error}
      <p class="mt-3 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">{usage.error}</p>
    {:else if !usage24hClientKeys || usage24hClientKeys.rows.length === 0}
      <p class="mt-4 text-sm text-[#6e6e6e]">No API key usage in the last 24h.</p>
    {:else}
      <div class="mt-4 overflow-x-auto rounded-lg border border-[#ededed] bg-white">
        <table class="w-full min-w-[640px] text-left text-sm">
          <thead class="border-b border-[#e5e5e5] bg-[#f5f5f5] text-[#6e6e6e]">
            <tr>
              <th class="px-4 py-3 font-medium">API key</th>
              <th class="px-4 py-3 font-medium">Requests</th>
              <th class="px-4 py-3 font-medium">Tokens</th>
              <th class="px-4 py-3 font-medium">Estimated cost</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-[#ededed]">
            {#each usage24hClientKeys.rows.slice(0, 8) as row}
              <tr>
                <td class="px-4 py-3 font-medium text-[#0d0d0d]">
                  {#if clientKeyUsageHref(row)}
                    <a class="inline-block max-w-[260px] truncate underline-offset-2 hover:underline" href={clientKeyUsageHref(row)}>
                      {row.label || row.id}
                    </a>
                  {:else}
                    {row.label || row.id}
                  {/if}
                </td>
                <td class="px-4 py-3 font-mono text-[13px] tabular-nums text-[#3c3c3c]">{formatTokens(row.requests)}</td>
                <td class="px-4 py-3 font-mono text-[13px] tabular-nums text-[#3c3c3c]">{formatTokens(row.totalTokens)}</td>
                <td class="px-4 py-3 font-mono text-[13px] tabular-nums text-[#3c3c3c]">{formatCostMicrousd(row.estimatedCostMicrousd)}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  </section>

  {#if apiKeys.oneTimeSecret}
    <div class="mt-5 rounded-lg border border-[#cbe7dd] bg-[#e8f5f0] p-4">
<div class="flex flex-wrap items-center justify-between gap-3">
  <p class="text-sm font-medium text-[#0a7a5e]">
    Copy this key now. It will not be shown again.
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

  {#if apiKeys.error}
    <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">
{apiKeys.error}
    </p>
  {/if}

  <div class="mt-6 flex flex-wrap items-end justify-between gap-3">
    <div class="flex flex-wrap items-end gap-3">
      <label class="block text-sm font-medium text-[#3c3c3c]">
        Search keys
        <input
          class="mt-2 w-64 max-w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
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
          <option value="revoked">Revoked keys</option>
        </select>
      </label>
    </div>
    <p class="text-sm text-[#6e6e6e]">
      Showing {filteredAPIKeys.length} of {apiKeys.items.length}
    </p>
  </div>

  <div class="mt-6 overflow-x-auto rounded-lg border border-[#ededed]">
    <table class="w-full min-w-[1280px] text-left text-sm">
<thead class="border-b border-[#e5e5e5] bg-[#f5f5f5] text-[#6e6e6e]">
  <tr>
    <th class="px-4 py-3 font-medium">Name</th>
    <th class="px-4 py-3 font-medium">Prefix</th>
    <th class="w-80 px-4 py-3 font-medium">Model access</th>
    <th class="w-72 px-4 py-3 font-medium">Key limits</th>
    <th class="px-4 py-3 font-medium">Created</th>
    <th class="px-4 py-3 font-medium">Last used</th>
    <th class="px-4 py-3 font-medium">Status</th>
    <th class="px-4 py-3 text-right font-medium">Action</th>
  </tr>
</thead>
<tbody class="divide-y divide-[#ededed]">
  {#if apiKeys.loading}
    <tr>
      <td class="px-4 py-5 text-[#6e6e6e]" colspan="8">Loading API keys...</td>
    </tr>
  {:else if apiKeys.items.length === 0}
    <tr>
      <td class="px-4 py-5 text-[#6e6e6e]" colspan="8">No API keys created yet.</td>
    </tr>
  {:else if filteredAPIKeys.length === 0}
    <tr>
      <td class="px-4 py-5 text-[#6e6e6e]" colspan="8">No API keys match your filters.</td>
    </tr>
  {:else}
    {#each filteredAPIKeys as key}
      <tr class="bg-white">
        <td class="px-4 py-3">
          <form
            class="grid gap-2"
            onsubmit={(event) => {
              event.preventDefault();
              updateAPIKeyName(key.id, key.name);
            }}
          >
            <label class="sr-only" for={`api-key-name-${key.id}`}>Name for {key.name}</label>
            <input
              id={`api-key-name-${key.id}`}
              class="w-full min-w-40 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
              bind:value={key.name}
              disabled={Boolean(key.revokedAt)}
            />
            <button
              class="justify-self-start rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
              type="submit"
              disabled={Boolean(key.revokedAt)}
            >
              Save name
            </button>
          </form>
        </td>
        <td class="px-4 py-3 font-mono text-[13px] text-[#3c3c3c]">{key.prefix}</td>
        <td class="px-4 py-3">
          <form
            class="grid gap-2"
            onsubmit={(event) => {
              event.preventDefault();
              updateAPIKeyModelPolicy(
                key.id,
                key.modelPolicy || 'all',
                key.allowedModelsText ?? modelListText(key.allowedModels ?? [])
              );
            }}
          >
            <label class="sr-only" for={`api-key-model-policy-${key.id}`}>Model access for {key.name}</label>
            <select
              id={`api-key-model-policy-${key.id}`}
              class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
              bind:value={key.modelPolicy}
              disabled={Boolean(key.revokedAt)}
            >
              <option value="all">All routable models</option>
              <option value="selected">Selected models</option>
            </select>
            {#if key.modelPolicy === 'selected'}
              <label class="sr-only" for={`api-key-selected-models-${key.id}`}>Selected models for {key.name}</label>
              <textarea
                id={`api-key-selected-models-${key.id}`}
                class="min-h-20 w-full resize-y rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] leading-5 text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
                placeholder={'gpt-4.1\ngpt-4.1-mini'}
                value={key.allowedModelsText ?? modelListText(key.allowedModels ?? [])}
                disabled={Boolean(key.revokedAt)}
                oninput={(event) => {
                  key.allowedModelsText = event.currentTarget.value;
                }}
              ></textarea>
            {/if}
            {#if unroutableModelsForKey(key).length}
              <p class="rounded-md border border-amber-200 bg-amber-50 p-2 text-xs leading-5 text-amber-800">
                No schedulable account:
                {#each unroutableModelsForKey(key) as model, index}
                  {#if index > 0}, {/if}
                  <a class="font-medium underline-offset-2 hover:underline" href={modelRoutingHref(model, key)}>
                    {model}
                  </a>
                {/each}
              </p>
            {/if}
            <button
              class="justify-self-start rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
              type="submit"
              disabled={Boolean(key.revokedAt)}
            >
              Save access
            </button>
          </form>
          <div class="mt-4 border-t border-[#ededed] pt-4">
            <label class="block text-xs font-medium text-[#6e6e6e]" for={`api-key-routing-pool-${key.id}`}>
              Routing pool
              <select
                id={`api-key-routing-pool-${key.id}`}
                class="mt-1 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
                value={key.routingPoolId ?? 0}
                disabled={Boolean(key.revokedAt) || routingPools.loading}
                onchange={(event) => updateAPIKeyRoutingPool(key.id, Number(event.currentTarget.value || 0))}
              >
                <option value={0}>Global provider account pool</option>
                {#each routingPools.items as pool}
                  <option value={pool.id}>{pool.name}</option>
                {/each}
              </select>
            </label>
            <p class="mt-1 text-xs text-[#6e6e6e]">
              {#if apiKeyRoutingPoolHref(key)}
                <a class="font-medium text-[#0d0d0d] underline-offset-2 hover:underline" href={apiKeyRoutingPoolHref(key)}>
                  {key.routingPoolName || `Pool ${key.routingPoolId}`}
                </a>
              {:else}
                Global pool
              {/if}
              {#if routingPoolFallbackNameForKey(key)}
                <span>· Fallback </span>
                {#if apiKeyRoutingPoolFallbackHref(key)}
                  <a class="font-medium text-[#0d0d0d] underline-offset-2 hover:underline" href={apiKeyRoutingPoolFallbackHref(key)}>
                    {routingPoolFallbackNameForKey(key)}
                  </a>
                {:else}
                  <span>{routingPoolFallbackNameForKey(key)}</span>
                {/if}
                {#if apiKeyRoutingPoolFallbackChainLogsHref(key)}
                  <span>· </span>
                  <a
                    class="font-medium text-[#0d0d0d] underline-offset-2 hover:underline"
                    href={apiKeyRoutingPoolFallbackChainLogsHref(key)}
                    title="View fallback chain logs"
                    aria-label="View fallback chain logs"
                  >
                    Chain logs
                  </a>
                {/if}
              {/if}
            </p>
          </div>
        </td>
        <td class="px-4 py-3">
          <form
            class="grid gap-2"
            onsubmit={(event) => {
              event.preventDefault();
              updateAPIKeyLimits(
                key.id,
                key.requestsPerMinute ?? 0,
                key.tokensPerMinute ?? 0
              );
            }}
          >
            <div class="grid gap-2 sm:grid-cols-2">
              <label class="block text-xs font-medium text-[#6e6e6e]" for={`api-key-requests-per-minute-${key.id}`}>
                Requests /min
                <input
                  id={`api-key-requests-per-minute-${key.id}`}
                  class="mt-1 w-full rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
                  type="number"
                  min="0"
                  step="1"
                  value={key.requestsPerMinute ?? 0}
                  disabled={Boolean(key.revokedAt)}
                  oninput={(event) => {
                    key.requestsPerMinute = Number(event.currentTarget.value || 0);
                  }}
                />
                <span class="mt-1 block text-[11px] font-normal text-[#6e6e6e]">
                  {keyLimitLabel(key.requestsPerMinute, gatewaySettings.data?.requestsPerMinutePerKey)}
                </span>
              </label>
              <label class="block text-xs font-medium text-[#6e6e6e]" for={`api-key-tokens-per-minute-${key.id}`}>
                Tokens /min
                <input
                  id={`api-key-tokens-per-minute-${key.id}`}
                  class="mt-1 w-full rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
                  type="number"
                  min="0"
                  step="1"
                  value={key.tokensPerMinute ?? 0}
                  disabled={Boolean(key.revokedAt)}
                  oninput={(event) => {
                    key.tokensPerMinute = Number(event.currentTarget.value || 0);
                  }}
                />
                <span class="mt-1 block text-[11px] font-normal text-[#6e6e6e]">
                  {keyLimitLabel(key.tokensPerMinute, gatewaySettings.data?.tokensPerMinutePerKey)}
                </span>
              </label>
            </div>
            <div>
              <p class="text-xs text-[#6e6e6e]">
                Active {key.currentConcurrentRequests || 0} / {keyConcurrencyLimitLabel(key.effectiveMaxConcurrentRequests)}
              </p>
              <p class="mt-1 text-xs text-[#6e6e6e]">
                Requests window {key.currentRequestsThisMinute || 0} / {keyRateWindowLimitLabel(key.effectiveRequestsPerMinute)}
                {#if key.effectiveRequestsPerMinute > 0}
                  <span>({keyRateRemainingLabel(key.requestRateRemaining)})</span>
                {/if}
              </p>
              <p class="mt-1 text-xs text-[#6e6e6e]">
                Tokens window {formatTokens(key.currentTokensThisMinute || 0)} / {keyRateWindowLimitLabel(key.effectiveTokensPerMinute)}
                {#if key.effectiveTokensPerMinute > 0}
                  <span>({keyRateRemainingLabel(key.tokenRateRemaining)})</span>
                {/if}
              </p>
              {#if key.concurrencyBlocked}
                <p class="mt-1 text-xs font-medium text-amber-700">Concurrency full</p>
              {/if}
              {#if key.requestRateLimited}
                <p class="mt-1 text-xs font-medium text-amber-700">Request limit full</p>
              {/if}
              {#if key.tokenRateLimited}
                <p class="mt-1 text-xs font-medium text-amber-700">Token limit full</p>
              {/if}
            </div>
            <button
              class="justify-self-start rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
              type="submit"
              disabled={Boolean(key.revokedAt)}
            >
              Save limits
            </button>
          </form>
          <form
            class="mt-4 grid gap-2 border-t border-[#ededed] pt-4"
            onsubmit={(event) => {
              event.preventDefault();
              updateAPIKeyBudgets(
                key.id,
                key.requestBudget24h ?? 0,
                key.tokenBudget24h ?? 0,
                key.requestBudget30d ?? 0,
                key.tokenBudget30d ?? 0
              );
            }}
          >
            <h4 class="text-xs font-semibold text-[#0d0d0d]">Key budgets</h4>
            <div class="grid gap-2 sm:grid-cols-2">
              <label class="block text-xs font-medium text-[#6e6e6e]" for={`api-key-request-budget-24h-${key.id}`}>
                Requests 24h
                <input
                  id={`api-key-request-budget-24h-${key.id}`}
                  class="mt-1 w-full rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
                  type="number"
                  min="0"
                  step="1"
                  value={key.requestBudget24h ?? 0}
                  disabled={Boolean(key.revokedAt)}
                  oninput={(event) => {
                    key.requestBudget24h = Number(event.currentTarget.value || 0);
                  }}
                />
              </label>
              <label class="block text-xs font-medium text-[#6e6e6e]" for={`api-key-token-budget-24h-${key.id}`}>
                Tokens 24h
                <input
                  id={`api-key-token-budget-24h-${key.id}`}
                  class="mt-1 w-full rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
                  type="number"
                  min="0"
                  step="1"
                  value={key.tokenBudget24h ?? 0}
                  disabled={Boolean(key.revokedAt)}
                  oninput={(event) => {
                    key.tokenBudget24h = Number(event.currentTarget.value || 0);
                  }}
                />
              </label>
              <label class="block text-xs font-medium text-[#6e6e6e]" for={`api-key-request-budget-30d-${key.id}`}>
                Requests 30d
                <input
                  id={`api-key-request-budget-30d-${key.id}`}
                  class="mt-1 w-full rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
                  type="number"
                  min="0"
                  step="1"
                  value={key.requestBudget30d ?? 0}
                  disabled={Boolean(key.revokedAt)}
                  oninput={(event) => {
                    key.requestBudget30d = Number(event.currentTarget.value || 0);
                  }}
                />
              </label>
              <label class="block text-xs font-medium text-[#6e6e6e]" for={`api-key-token-budget-30d-${key.id}`}>
                Tokens 30d
                <input
                  id={`api-key-token-budget-30d-${key.id}`}
                  class="mt-1 w-full rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
                  type="number"
                  min="0"
                  step="1"
                  value={key.tokenBudget30d ?? 0}
                  disabled={Boolean(key.revokedAt)}
                  oninput={(event) => {
                    key.tokenBudget30d = Number(event.currentTarget.value || 0);
                  }}
                />
              </label>
            </div>
            <div>
              <p class="text-xs text-[#6e6e6e]">
                Requests 24h {keyBudgetUsageLabel(key.requestsUsed24h, key.requestBudget24h)}
                {#if key.requestsRemaining24h !== null && key.requestsRemaining24h !== undefined}
                  <span>({key.requestsRemaining24h} remaining)</span>
                {/if}
              </p>
              <p class="mt-1 text-xs text-[#6e6e6e]">
                Tokens 24h {formatTokens(key.tokensUsed24h || 0)} / {key.tokenBudget24h > 0 ? formatTokens(key.tokenBudget24h) : 'unlimited'}
                {#if key.tokensRemaining24h !== null && key.tokensRemaining24h !== undefined}
                  <span>({formatTokens(key.tokensRemaining24h)} remaining)</span>
                {/if}
              </p>
              <p class="mt-1 text-xs text-[#6e6e6e]">
                Requests 30d {keyBudgetUsageLabel(key.requestsUsed30d, key.requestBudget30d)}
                {#if key.requestsRemaining30d !== null && key.requestsRemaining30d !== undefined}
                  <span>({key.requestsRemaining30d} remaining)</span>
                {/if}
              </p>
              <p class="mt-1 text-xs text-[#6e6e6e]">
                Tokens 30d {formatTokens(key.tokensUsed30d || 0)} / {key.tokenBudget30d > 0 ? formatTokens(key.tokenBudget30d) : 'unlimited'}
                {#if key.tokensRemaining30d !== null && key.tokensRemaining30d !== undefined}
                  <span>({formatTokens(key.tokensRemaining30d)} remaining)</span>
                {/if}
              </p>
              {#if key.requestBudgetExceeded}
                <p class="mt-1 text-xs font-medium text-amber-700">Request budget exceeded</p>
              {/if}
              {#if key.tokenBudgetExceeded}
                <p class="mt-1 text-xs font-medium text-amber-700">Token budget exceeded</p>
              {/if}
            </div>
            <button
              class="justify-self-start rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
              type="submit"
              disabled={Boolean(key.revokedAt)}
            >
              Save budgets
            </button>
          </form>
        </td>
        <td class="px-4 py-3 text-[#3c3c3c]">{formatDate(key.createdAt)}</td>
        <td class="px-4 py-3 text-[#3c3c3c]">{formatDate(key.lastUsedAt)}</td>
        <td class="px-4 py-3">
          <span
            class={[
              'inline-flex rounded-full px-2.5 py-1 text-xs font-medium',
              key.revokedAt
                ? 'bg-red-50 text-red-700'
                : key.disabledAt
                  ? 'bg-amber-50 text-amber-700'
                : 'bg-[#e8f5f0] text-[#0a7a5e]'
            ]}
          >
            {key.revokedAt ? 'Revoked' : key.disabledAt ? 'Disabled' : 'Active'}
          </span>
        </td>
        <td class="px-4 py-3 text-right">
          <a
            class="mr-2 inline-flex rounded-lg border border-[#e5e5e5] bg-white px-3 py-1.5 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]"
            href={`/request-logs?clientKeyId=${key.id}`}
            title="View request logs"
            aria-label="View request logs"
          >
            Logs
          </a>
          {#if !key.revokedAt}
            <button
              class="mr-2 rounded-lg border border-[#e5e5e5] bg-white px-3 py-1.5 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]"
              type="button"
              onclick={() => setAPIKeyDisabled(key.id, !key.disabledAt)}
            >
              {key.disabledAt ? 'Enable' : 'Disable'}
            </button>
          {/if}
          <button
            class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-1.5 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
            type="button"
            disabled={Boolean(key.revokedAt)}
            onclick={() => revokeKey(key.id)}
          >
            Revoke
          </button>
        </td>
      </tr>
    {/each}
  {/if}
</tbody>
    </table>
  </div>
</section>

{/if}
