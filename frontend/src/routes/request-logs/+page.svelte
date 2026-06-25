<script>
  import { page } from '$app/state';
  import {
    accountLabel,
    apiKeys,
    formatDate,
    formatCostMicrousd,
    formatRequestLogCost,
    formatTokens,
    loadKeys,
    loadProviderAccounts,
    loadRoutingPools,
    loadUsagePricing,
    loadUsageSummary,
    loadRequestLogs,
    login,
    loginForm,
    providerAccounts,
    requestLogs,
    resetRequestLogFilters,
    routingPools,
    saveUsagePricing,
    session,
    usage,
    usagePricing,
  } from '$lib/admin-state.svelte.js';

  let providerAccountsRequested = $state(false);
  let routingPoolsRequested = $state(false);
  let apiKeysRequested = $state(false);
  let appliedRequestLogSearch = $state('');

  /** @param {string} search */
  function applyRequestLogURLFilters(search) {
    resetRequestLogFilters();
    const params = new URLSearchParams(search);
    const providerAccountId = params.get('providerAccountId') ?? '';
    if (/^[1-9]\d*$/.test(providerAccountId)) {
      requestLogs.providerAccountId = providerAccountId;
    }

    const routingPoolId = params.get('routingPoolId') ?? '';
    if (/^[1-9]\d*$/.test(routingPoolId)) {
      requestLogs.routingPoolId = routingPoolId;
    }

    const clientKeyId = params.get('clientKeyId') ?? '';
    if (/^[1-9]\d*$/.test(clientKeyId)) {
      requestLogs.clientKeyId = clientKeyId;
    }

    const query = params.get('q');
    if (query !== null) {
      requestLogs.query = query;
    }

    const model = params.get('model') ?? '';
    if (model.length > 0 && model.length <= 100) {
      requestLogs.model = model;
    }

    const sessionId = params.get('sessionId') ?? '';
    if (sessionId.length > 0 && sessionId.length <= 100) {
      requestLogs.sessionId = sessionId;
    }

    const error = params.get('error') ?? '';
    if (error.length > 0 && error.length <= 100) {
      requestLogs.errorCode = error;
    }

    const statusClass = params.get('statusClass') ?? '';
    if (['all', 'success', 'client_error', 'server_error'].includes(statusClass)) {
      requestLogs.statusClass = statusClass;
    }

    const routingPoolError = params.get('routingPoolError') ?? '';
    if (
      [
        'all',
        'routing_pool_disabled',
        'routing_pool_unavailable',
        'routing_pool_empty',
        'routing_pool_cycle',
        'routing_pool_exhausted'
      ].includes(routingPoolError)
    ) {
      requestLogs.routingPoolError = routingPoolError;
    }

    const routingPoolChain = params.get('routingPoolChain') ?? '';
    if (routingPoolChain.length > 0 && routingPoolChain.length <= 200) {
      requestLogs.routingPoolChain = routingPoolChain;
    }
  }

  $effect(() => {
    if (!session.authenticated) {
      providerAccountsRequested = false;
      routingPoolsRequested = false;
      apiKeysRequested = false;
      appliedRequestLogSearch = '';
      return;
    }
    if (appliedRequestLogSearch !== page.url.search) {
      appliedRequestLogSearch = page.url.search;
      applyRequestLogURLFilters(page.url.search);
      void loadRequestLogs();
    }
    if (!providerAccountsRequested && providerAccounts.items.length === 0) {
      providerAccountsRequested = true;
      void loadProviderAccounts();
    }
    if (!routingPoolsRequested && routingPools.items.length === 0) {
      routingPoolsRequested = true;
      void loadRoutingPools();
    }
    if (!apiKeysRequested && apiKeys.items.length === 0) {
      apiKeysRequested = true;
      void loadKeys();
    }
  });

  /** @param {string | null | undefined} value */
  function accountTypeLabel(value) {
    if (value === 'api_upstream') return 'API upstream';
    if (value === 'codex_oauth') return 'Codex OAuth';
    return value || 'Unknown';
  }

  /** @param {string | null | undefined} value */
  function usageSourceLabel(value) {
    if (value === 'chat_completions') return 'Chat';
    if (value === 'responses') return 'Responses';
    if (value === 'stream') return 'Stream';
    return value || 'Missing';
  }

  /** @param {string | null | undefined} value */
  function errorLabel(value) {
    if (!value) return '-';

    return value
      .split('_')
      .filter(Boolean)
      .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
      .join(' ');
  }

  const usageRanges = ['24h', '7d', '30d'];
  const usageGroups = [
    { value: 'model', label: 'Model' },
    { value: 'client_key', label: 'Client key' },
    { value: 'provider_account', label: 'Provider account' },
    { value: 'routing_pool', label: 'Routing pool' },
    { value: 'routing_pool_chain', label: 'Routing pool chain' },
    { value: 'session', label: 'Session' }
  ];
  const requestLogStatusClasses = [
    { value: 'all', label: 'All statuses' },
    { value: 'success', label: 'Success' },
    { value: 'client_error', label: 'Client errors' },
    { value: 'server_error', label: 'Server errors' }
  ];
  const routingPoolErrorFilters = [
    { value: 'all', label: 'All routing errors' },
    { value: 'routing_pool_disabled', label: 'Routing pool disabled' },
    { value: 'routing_pool_unavailable', label: 'Routing pool unavailable' },
    { value: 'routing_pool_empty', label: 'Routing pool empty' },
    { value: 'routing_pool_cycle', label: 'Routing pool cycle' },
    { value: 'routing_pool_exhausted', label: 'Routing pool exhausted' }
  ];

  /** @param {string} range */
  function summaryForRange(range) {
    return usage.summaries[`${range}:${usage.groupBy}`] ?? null;
  }

  /** @param {import('$lib/admin-state.svelte.js').UsageSummaryRow} row */
  function usageRowHref(row) {
    const id = String(row?.id ?? '');
    if (!id || id === 'unknown' || id === 'none') return '';
    if (usage.groupBy === 'model') return `/request-logs?model=${encodeURIComponent(id)}`;
    if (usage.groupBy === 'client_key' && /^[1-9]\d*$/.test(id)) {
      return `/request-logs?clientKeyId=${encodeURIComponent(id)}`;
    }
    if (usage.groupBy === 'provider_account') {
      const accountId = id.split('/').pop() ?? '';
      return /^[1-9]\d*$/.test(accountId) ? `/request-logs?providerAccountId=${encodeURIComponent(accountId)}` : '';
    }
    if (usage.groupBy === 'routing_pool' && /^[1-9]\d*$/.test(id)) {
      return `/request-logs?routingPoolId=${encodeURIComponent(id)}`;
    }
    if (usage.groupBy === 'routing_pool_chain') {
      return `/request-logs?routingPoolChain=${encodeURIComponent(id)}`;
    }
    if (usage.groupBy === 'session') return `/request-logs?sessionId=${encodeURIComponent(id)}`;
    return '';
  }

  /** @param {Event & { currentTarget: HTMLSelectElement }} event */
  function changeUsageGroup(event) {
    loadUsageSummary(usage.range, event.currentTarget.value);
  }

  /** @param {Event & { currentTarget: HTMLSelectElement }} event */
  function changeUsageRange(event) {
    loadUsageSummary(event.currentTarget.value, usage.groupBy);
  }

  function addPricingRow() {
    usagePricing.rows = [
      ...usagePricing.rows,
      {
        model: '',
        inputMicrousdPerMillion: 0,
        cachedInputMicrousdPerMillion: 0,
        outputMicrousdPerMillion: 0
      }
    ];
  }

  /** @param {number} index */
  function removePricingRow(index) {
    usagePricing.rows = usagePricing.rows.filter((_, rowIndex) => rowIndex !== index);
  }
</script>

<svelte:head>
  <title>N2API Request Logs</title>
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
<div class="space-y-6">
<section class="rounded-lg border border-[#ededed] bg-white p-6">
  <div class="flex flex-wrap items-center justify-between gap-4">
    <div>
<h2 class="text-2xl font-semibold leading-tight text-[#0d0d0d]">Usage summary</h2>
<p class="mt-1 text-sm text-[#6e6e6e]">Gateway usage by time range and routing dimension.</p>
    </div>
    <div class="flex flex-wrap items-center gap-3">
      <label class="text-sm font-medium text-[#3c3c3c]">
        Range
        <select
          class="ml-2 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={usage.range}
          onchange={changeUsageRange}
        >
          {#each usageRanges as range}
            <option value={range}>{range}</option>
          {/each}
        </select>
      </label>
      <label class="text-sm font-medium text-[#3c3c3c]">
        Group
        <select
          class="ml-2 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={usage.groupBy}
          onchange={changeUsageGroup}
        >
          {#each usageGroups as group}
            <option value={group.value}>{group.label}</option>
          {/each}
        </select>
      </label>
      <button
        class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
        type="button"
        disabled={usage.loading}
        onclick={() => loadUsageSummary(usage.range, usage.groupBy)}
      >
        {usage.loading ? 'Loading' : 'Refresh usage'}
      </button>
    </div>
  </div>

  {#if usage.error}
    <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">{usage.error}</p>
  {/if}

  <div class="mt-5 grid gap-3 md:grid-cols-3">
    {#each usageRanges as range}
      {@const summary = summaryForRange(range)}
      <button
        class={[
          'rounded-lg border p-4 text-left hover:bg-[#f5f5f5]',
          usage.range === range ? 'border-[#10a37f] bg-[#e8f5f0]' : 'border-[#ededed] bg-white'
        ]}
        type="button"
        onclick={() => loadUsageSummary(range, usage.groupBy)}
      >
        <p class="text-xs font-medium uppercase tracking-normal text-[#6e6e6e]">{range}</p>
        <p class="mt-2 text-2xl font-semibold tabular-nums text-[#0d0d0d]">{formatTokens(summary?.totalTokens)}</p>
        <p class="mt-1 text-sm text-[#6e6e6e]">{formatTokens(summary?.totalRequests)} requests · {formatCostMicrousd(summary?.estimatedCostMicrousd)}</p>
      </button>
    {/each}
  </div>

  <div class="mt-6 overflow-x-auto rounded-lg border border-[#ededed]">
    <table class="w-full min-w-[760px] text-left text-sm">
      <thead class="border-b border-[#e5e5e5] bg-[#f5f5f5] text-[#6e6e6e]">
        <tr>
          <th class="px-4 py-3 font-medium">Group</th>
          <th class="px-4 py-3 font-medium">Requests</th>
          <th class="px-4 py-3 font-medium">Input tokens</th>
          <th class="px-4 py-3 font-medium">Output tokens</th>
          <th class="px-4 py-3 font-medium">Total tokens</th>
          <th class="px-4 py-3 font-medium">Estimated cost</th>
        </tr>
      </thead>
      <tbody class="divide-y divide-[#ededed]">
        {#if usage.loading && !usage.current}
          <tr>
            <td class="px-4 py-5 text-[#6e6e6e]" colspan="6">Loading usage summary...</td>
          </tr>
        {:else if !usage.current || usage.current.rows.length === 0}
          <tr>
            <td class="px-4 py-5 text-[#6e6e6e]" colspan="6">No usage in this range.</td>
          </tr>
        {:else}
          {#each usage.current.rows as row}
            <tr>
              <td class="px-4 py-3 font-medium text-[#0d0d0d]">
                {#if usageRowHref(row)}
                  <a class="max-w-[260px] truncate underline-offset-2 hover:underline" href={usageRowHref(row)}>
                    {row.label || row.id}
                  </a>
                {:else}
                  {row.label || row.id}
                {/if}
              </td>
              <td class="px-4 py-3 font-mono text-[13px] tabular-nums text-[#3c3c3c]">{formatTokens(row.requests)}</td>
              <td class="px-4 py-3 font-mono text-[13px] tabular-nums text-[#3c3c3c]">{formatTokens(row.inputTokens)}</td>
              <td class="px-4 py-3 font-mono text-[13px] tabular-nums text-[#3c3c3c]">{formatTokens(row.outputTokens)}</td>
              <td class="px-4 py-3 font-mono text-[13px] tabular-nums text-[#3c3c3c]">{formatTokens(row.totalTokens)}</td>
              <td class="px-4 py-3 font-mono text-[13px] tabular-nums text-[#3c3c3c]">{formatCostMicrousd(row.estimatedCostMicrousd)}</td>
            </tr>
          {/each}
        {/if}
      </tbody>
    </table>
  </div>
</section>

<section class="rounded-lg border border-[#ededed] bg-white p-6">
  <form onsubmit={saveUsagePricing}>
    <div class="flex flex-wrap items-center justify-between gap-4">
      <div>
        <h2 class="text-xl font-semibold leading-tight text-[#0d0d0d]">Pricing</h2>
        <p class="mt-1 text-sm text-[#6e6e6e]">USD micro-prices per 1M tokens for historical estimates.</p>
      </div>
      <div class="flex flex-wrap items-center gap-3">
        <button
          class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
          type="button"
          disabled={usagePricing.loading}
          onclick={loadUsagePricing}
        >
          {usagePricing.loading ? 'Loading' : 'Reload pricing'}
        </button>
        <button
          class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]"
          type="button"
          onclick={addPricingRow}
        >
          Add model
        </button>
        <button class="rounded-lg bg-[#0d0d0d] px-3 py-2 text-sm font-medium text-white disabled:cursor-not-allowed disabled:opacity-60" disabled={usagePricing.saving}>
          {usagePricing.saving ? 'Saving' : 'Save pricing'}
        </button>
      </div>
    </div>

    {#if usagePricing.error}
      <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">{usagePricing.error}</p>
    {:else if usagePricing.saved}
      <p class="mt-4 rounded-md border border-[#cce7db] bg-[#e8f5f0] p-3 text-sm text-[#0a7a5e]">Pricing saved.</p>
    {/if}

    <div class="mt-5 overflow-x-auto rounded-lg border border-[#ededed]">
      <table class="w-full min-w-[980px] text-left text-sm">
        <thead class="border-b border-[#e5e5e5] bg-[#f5f5f5] text-[#6e6e6e]">
          <tr>
            <th class="px-4 py-3 font-medium">Model</th>
            <th class="px-4 py-3 font-medium">Input tokens</th>
            <th class="px-4 py-3 font-medium">Cached input</th>
            <th class="px-4 py-3 font-medium">Output tokens</th>
            <th class="px-4 py-3 font-medium">Action</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-[#ededed]">
          {#if usagePricing.rows.length === 0}
            <tr>
              <td class="px-4 py-5 text-[#6e6e6e]" colspan="5">No pricing rows configured.</td>
            </tr>
          {:else}
            {#each usagePricing.rows as row, index}
              <tr>
                <td class="px-4 py-3">
                  <input class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={row.model} placeholder="gpt-5" />
                </td>
                <td class="px-4 py-3">
                  <input class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" type="number" min="0" step="1" bind:value={row.inputMicrousdPerMillion} />
                </td>
                <td class="px-4 py-3">
                  <input class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" type="number" min="0" step="1" bind:value={row.cachedInputMicrousdPerMillion} />
                </td>
                <td class="px-4 py-3">
                  <input class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" type="number" min="0" step="1" bind:value={row.outputMicrousdPerMillion} />
                </td>
                <td class="px-4 py-3">
                  <button class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]" type="button" onclick={() => removePricingRow(index)}>Remove</button>
                </td>
              </tr>
            {/each}
          {/if}
        </tbody>
      </table>
    </div>
  </form>
</section>

<section class="rounded-lg border border-[#ededed] bg-white p-6">
  <div class="flex flex-wrap items-center justify-between gap-4">
    <div>
<h2 class="text-2xl font-semibold leading-tight text-[#0d0d0d]">Request logs</h2>
<p class="mt-1 text-sm text-[#6e6e6e]">
  Recent OpenAI-compatible gateway requests.
</p>
    </div>
    <div class="flex flex-wrap items-end gap-3">
      <label class="block text-sm font-medium text-[#3c3c3c]">
        Search
        <input
          class="mt-2 w-64 max-w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={requestLogs.query}
          placeholder="key, account, model, route, error"
        />
      </label>
      <label class="block text-sm font-medium text-[#3c3c3c]">
        Model filter
        <input
          class="mt-2 w-44 max-w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={requestLogs.model}
          placeholder="gpt-5"
        />
      </label>
      <label class="block text-sm font-medium text-[#3c3c3c]">
        Session filter
        <input
          class="mt-2 w-52 max-w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={requestLogs.sessionId}
          placeholder="workspace-123"
        />
      </label>
      <label class="block text-sm font-medium text-[#3c3c3c]">
        Error code filter
        <input
          class="mt-2 w-56 max-w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={requestLogs.errorCode}
          placeholder="api_key_token_rate_limited"
        />
      </label>
      <label class="block text-sm font-medium text-[#3c3c3c]">
        Status
        <select
          class="mt-2 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={requestLogs.statusClass}
        >
          {#each requestLogStatusClasses as statusClass}
            <option value={statusClass.value}>{statusClass.label}</option>
          {/each}
        </select>
      </label>
      <label class="block text-sm font-medium text-[#3c3c3c]">
        Routing error
        <select
          class="mt-2 max-w-[240px] rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={requestLogs.routingPoolError}
        >
          {#each routingPoolErrorFilters as routingPoolError}
            <option value={routingPoolError.value}>{routingPoolError.label}</option>
          {/each}
        </select>
      </label>
      <label class="block text-sm font-medium text-[#3c3c3c]">
        Fallback chain
        <input
          class="mt-2 w-56 max-w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={requestLogs.routingPoolChain}
          placeholder="primary -> secondary"
        />
      </label>
      <label class="block text-sm font-medium text-[#3c3c3c]">
        Provider account
        <select
          class="mt-2 max-w-[260px] rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={requestLogs.providerAccountId}
        >
          <option value="all">All provider accounts</option>
          {#each providerAccounts.items as account}
            <option value={String(account.id)}>{accountLabel(account)}</option>
          {/each}
        </select>
      </label>
      <label class="block text-sm font-medium text-[#3c3c3c]">
        Routing pool
        <select
          class="mt-2 max-w-[240px] rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={requestLogs.routingPoolId}
        >
          <option value="all">All routing pools</option>
          {#each routingPools.items as pool}
            <option value={String(pool.id)}>{pool.name}</option>
          {/each}
        </select>
      </label>
      <label class="block text-sm font-medium text-[#3c3c3c]">
        API key
        <select
          class="mt-2 max-w-[240px] rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={requestLogs.clientKeyId}
        >
          <option value="all">All API keys</option>
          {#each apiKeys.items as key}
            <option value={String(key.id)}>{key.name} ({key.prefix})</option>
          {/each}
        </select>
      </label>
      <button
        class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
        type="button"
        disabled={requestLogs.loading}
        onclick={loadRequestLogs}
      >
        {requestLogs.loading ? 'Refreshing' : 'Refresh'}
      </button>
    </div>
  </div>

  {#if requestLogs.error}
    <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">
{requestLogs.error}
    </p>
  {/if}

  <div class="mt-6 overflow-x-auto rounded-lg border border-[#ededed]">
    <table class="w-full min-w-[1560px] text-left text-sm">
<thead class="border-b border-[#e5e5e5] bg-[#f5f5f5] text-[#6e6e6e]">
  <tr>
    <th class="px-4 py-3 font-medium">Time</th>
    <th class="px-4 py-3 font-medium">Key</th>
    <th class="px-4 py-3 font-medium">Provider account</th>
    <th class="px-4 py-3 font-medium">Routing pool</th>
    <th class="px-4 py-3 font-medium">Model</th>
    <th class="px-4 py-3 font-medium">Session</th>
    <th class="px-4 py-3 font-medium">Tokens</th>
    <th class="px-4 py-3 font-medium">Estimated cost</th>
    <th class="px-4 py-3 font-medium">Usage</th>
    <th class="px-4 py-3 font-medium">Gateway diagnostics</th>
    <th class="px-4 py-3 font-medium">Route</th>
    <th class="px-4 py-3 font-medium">Method</th>
    <th class="px-4 py-3 font-medium">Status</th>
    <th class="px-4 py-3 font-medium">Latency</th>
    <th class="px-4 py-3 font-medium">Error</th>
  </tr>
</thead>
<tbody class="divide-y divide-[#ededed]">
  {#if requestLogs.loading}
    <tr>
      <td class="px-4 py-5 text-[#6e6e6e]" colspan="15">Loading request logs...</td>
    </tr>
  {:else if requestLogs.items.length === 0}
    <tr>
      <td class="px-4 py-5 text-[#6e6e6e]" colspan="15">No gateway requests yet.</td>
    </tr>
  {:else}
    {#each requestLogs.items as log}
      {@const requestLogCost = formatRequestLogCost(log)}
      <tr class="bg-white">
        <td class="px-4 py-3 text-[#3c3c3c]">{formatDate(log.createdAt)}</td>
        <td class="px-4 py-3 text-[#3c3c3c]">
          {#if log.clientKeyId}
            <a
              class="block max-w-[180px] truncate font-medium text-[#0d0d0d] hover:text-[#0a7a5e]"
              href={`/api-keys?clientKeyId=${log.clientKeyId}`}
              title="View API key"
              aria-label="View API key"
            >
              {log.clientKey || `Key ${log.clientKeyId}`}
            </a>
          {:else}
            {log.clientKey || 'Unknown'}
          {/if}
        </td>
        <td class="px-4 py-3">
          {#if log.providerAccountId}
            <div class="max-w-[220px]">
              <a
                class="block truncate font-medium text-[#0d0d0d] hover:text-[#0a7a5e]"
                href={`/providers?providerAccountId=${log.providerAccountId}`}
                title="View provider account"
                aria-label="View provider account"
              >
                {log.providerAccountName || `Account ${log.providerAccountId}`}
              </a>
              <p class="mt-1 text-xs text-[#6e6e6e]">
                {log.provider || 'Unknown'} · {accountTypeLabel(log.providerAccountType)} · ID {log.providerAccountId}
              </p>
            </div>
          {:else}
            <span class="text-[#6e6e6e]">Unassigned</span>
          {/if}
        </td>
        <td class="px-4 py-3 text-[#3c3c3c]">
          {#if log.routingPoolId}
            <div class="max-w-[180px]">
              <a
                class="block truncate font-medium text-[#0d0d0d] hover:text-[#0a7a5e]"
                href={`/routing-pools?routingPoolId=${log.routingPoolId}`}
                title="View routing pool"
                aria-label="View routing pool"
              >
                {log.routingPoolName || `Pool ${log.routingPoolId}`}
              </a>
              <p class="mt-1 text-xs text-[#6e6e6e]">ID {log.routingPoolId}</p>
              {#if log.routingPoolFallbackDepth > 0}
                <p class="mt-1 text-xs text-[#6e6e6e]">Fallback depth {log.routingPoolFallbackDepth}</p>
              {/if}
              {#if log.routingPoolFallbackChain}
                <a
                  class="mt-1 block max-w-[180px] truncate text-xs text-[#6e6e6e] hover:text-[#0a7a5e]"
                  href={`/request-logs?routingPoolChain=${encodeURIComponent(log.routingPoolFallbackChain)}`}
                  title={log.routingPoolFallbackChain}
                  aria-label="View fallback chain logs"
                >
                  Fallback chain {log.routingPoolFallbackChain}
                </a>
              {/if}
            </div>
          {:else}
            <span class="text-[#6e6e6e]">Global pool</span>
          {/if}
        </td>
        <td class="px-4 py-3 font-mono text-[13px] text-[#3c3c3c]">{log.model || '-'}</td>
        <td class="px-4 py-3 font-mono text-[13px] text-[#3c3c3c]">
          <span class="block max-w-[180px] truncate">{log.sessionId || '-'}</span>
        </td>
        <td class="px-4 py-3 font-mono text-[13px] tabular-nums text-[#3c3c3c]">
          {formatTokens(log.inputTokens)} in / {formatTokens(log.outputTokens)} out
          <p class="mt-1 text-xs text-[#6e6e6e]">{formatTokens(log.totalTokens)} total</p>
        </td>
        <td class="px-4 py-3 font-mono text-[13px] tabular-nums text-[#3c3c3c]">
          {#if requestLogCost === 'Unpriced'}
            <span class="font-sans text-sm text-[#6e6e6e]">{requestLogCost}</span>
          {:else}
            {requestLogCost}
          {/if}
        </td>
        <td class="px-4 py-3 text-[#3c3c3c]">
          <span class="text-sm">{usageSourceLabel(log.usageSource)}</span>
        </td>
        <td class="px-4 py-3 text-[#3c3c3c]">
          <span class="text-sm tabular-nums">Attempts {log.gatewayAttemptCount ?? 0}</span>
          <p class="mt-1 text-xs text-[#6e6e6e]">Fallbacks {log.gatewayFallbackCount ?? 0}</p>
          {#if log.routingPoolError}
            <p class="mt-1 text-xs font-medium text-amber-700">{errorLabel(log.routingPoolError)}</p>
          {/if}
        </td>
        <td class="px-4 py-3 font-mono text-[13px] text-[#0d0d0d]">{log.route}</td>
        <td class="px-4 py-3 font-mono text-[13px] text-[#3c3c3c]">{log.method}</td>
        <td class="px-4 py-3">
          <span
            class={[
              'inline-flex rounded-full px-2.5 py-1 text-xs font-medium tabular-nums',
              log.statusCode >= 500
                ? 'bg-red-50 text-red-700'
                : log.statusCode >= 400
                  ? 'bg-amber-50 text-amber-700'
                  : 'bg-[#e8f5f0] text-[#0a7a5e]'
            ]}
          >
            {log.statusCode}
          </span>
        </td>
        <td class="px-4 py-3 font-mono text-[13px] tabular-nums text-[#3c3c3c]">
          {log.latencyMs}ms
        </td>
        <td class="px-4 py-3 text-[#3c3c3c]">
          <span title={log.error || ''}>{errorLabel(log.error)}</span>
        </td>
      </tr>
    {/each}
  {/if}
</tbody>
    </table>
  </div>
</section>
</div>
{/if}
