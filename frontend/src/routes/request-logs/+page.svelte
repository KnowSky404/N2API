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
    providerAccounts,
    requestLogs,
    resetRequestLogFilters,
    routingPools,
    saveUsagePricing,
    session,
    syncOfficialUsagePricing,
    usage,
    usagePricing,
  } from '$lib/admin-state.svelte.js';

  import AuthGate from '$lib/AuthGate.svelte';
  let providerAccountsRequested = $state(false);
  let routingPoolsRequested = $state(false);
  let apiKeysRequested = $state(false);
  let appliedRequestLogSearch = $state('');

  /** @param {string} search */
  function applyRequestLogURLFilters(search) {
    resetRequestLogFilters();
    const params = new URLSearchParams(search);
    const requestId = params.get('requestId') ?? '';
    if (requestId.length > 0 && requestId.length <= 100) {
      requestLogs.requestId = requestId;
    }

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

    const usageSource = params.get('usageSource') ?? '';
    if (
      [
        'all',
        'missing',
        'chat_completions',
        'responses',
        'stream',
        'gemini_usage_metadata',
        'anthropic_usage'
      ].includes(usageSource)
    ) {
      requestLogs.usageSource = usageSource;
    }

    const statusClass = params.get('statusClass') ?? '';
    if (['all', 'success', 'client_error', 'server_error'].includes(statusClass)) {
      requestLogs.statusClass = statusClass;
    }

    const statusCode = params.get('statusCode') ?? '';
    if (/^[1-5]\d\d$/.test(statusCode)) {
      requestLogs.statusCode = statusCode;
    }

    const since = params.get('since') ?? '';
    if (/^\d+$/.test(since)) {
      requestLogs.since = since;
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

    const gatewayFallbacks = params.get('gatewayFallbacks') ?? '';
    if (gatewayFallbacks === '1' || gatewayFallbacks === 'true') {
      requestLogs.gatewayFallbacks = true;
    }
  }
  /** @param {string} [format] */
  function exportRequestLogsURL(format) {
    const params = new URLSearchParams();
    if (requestLogs.requestId) params.set('requestId', requestLogs.requestId);
    if (requestLogs.query) params.set('q', requestLogs.query);
    if (requestLogs.statusClass && requestLogs.statusClass !== 'all') params.set('statusClass', requestLogs.statusClass);
    if (/^[1-5]\d\d$/.test(requestLogs.statusCode)) params.set('statusCode', requestLogs.statusCode);
    if (/^\d+$/.test(requestLogs.since)) params.set('since', requestLogs.since);
    if (requestLogs.providerAccountId) params.set('providerAccountId', requestLogs.providerAccountId);
    if (requestLogs.routingPoolId) params.set('routingPoolId', requestLogs.routingPoolId);
    if (requestLogs.clientKeyId) params.set('clientKeyId', requestLogs.clientKeyId);
    if (requestLogs.model) params.set('model', requestLogs.model);
    if (requestLogs.sessionId) params.set('sessionId', requestLogs.sessionId);
    if (requestLogs.errorCode) params.set('error', requestLogs.errorCode);
    if (requestLogs.usageSource && requestLogs.usageSource !== 'all') params.set('usageSource', requestLogs.usageSource);
    if (requestLogs.routingPoolError) params.set('routingPoolError', requestLogs.routingPoolError);
    if (requestLogs.routingPoolChain) params.set('routingPoolChain', requestLogs.routingPoolChain);
    if (requestLogs.gatewayFallbacks) params.set('gatewayFallbacks', '1');
    if (format) params.set('format', format);
    return '/api/admin/request-logs/export?' + params.toString();
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
    if (value === 'gemini_usage_metadata') return 'Gemini';
    if (value === 'anthropic_usage') return 'Anthropic';
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

  function requestLogDrilldownParams() {
    const params = new URLSearchParams();
    if (/^\d+$/.test(requestLogs.since)) params.set('since', requestLogs.since);
    return params;
  }

  /** @param {import('$lib/admin-state.svelte.js').RequestLog} log */
  function errorHref(log) {
    if (!log.error) return '';
    const params = requestLogDrilldownParams();
    params.set('error', log.error);
    return `/request-logs?${params.toString()}`;
  }

  /** @param {import('$lib/admin-state.svelte.js').RequestLog} log */
  function routingPoolFallbackChainHref(log) {
    if (!log.routingPoolFallbackChain) return '';
    const params = requestLogDrilldownParams();
    params.set('routingPoolChain', log.routingPoolFallbackChain);
    return `/request-logs?${params.toString()}`;
  }

  const usageRanges = ['24h', '7d', '30d'];
  const usageGroups = [
    { value: 'model', label: 'Model' },
    { value: 'client_key', label: 'Client key' },
    { value: 'provider_account', label: 'Provider account' },
    { value: 'routing_pool', label: 'Routing pool' },
    { value: 'routing_pool_chain', label: 'Routing pool chain' },
    { value: 'session', label: 'Session' },
    { value: 'usage_source', label: 'Usage source' }
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
  const usageSourceFilters = [
    { value: 'all', label: 'All usage sources' },
    { value: 'missing', label: 'Missing usage' },
    { value: 'chat_completions', label: 'Chat completions' },
    { value: 'responses', label: 'Responses' },
    { value: 'stream', label: 'Stream' },
    { value: 'gemini_usage_metadata', label: 'Gemini metadata' },
    { value: 'anthropic_usage', label: 'Anthropic usage' }
  ];

  /** @param {string} range */
  function summaryForRange(range) {
    return usage.summaries[`${range}:${usage.groupBy}`] ?? null;
  }

  function usageRangeSinceParam() {
    let seconds = 86400;
    if (usage.range === '7d') seconds = 604800;
    if (usage.range === '30d') seconds = 2592000;
    return String(Math.max(0, Math.floor(Date.now() / 1000) - seconds));
  }

  /** @param {import('$lib/admin-state.svelte.js').UsageSummaryRow} row */
  function usageRowHref(row) {
    const id = String(row?.id ?? '');
    if (!id || id === 'unknown' || id === 'none') return '';
    const params = new URLSearchParams();
    if (usage.groupBy === 'model') {
      params.set('model', id);
      params.set('since', usageRangeSinceParam());
      return `/request-logs?${params.toString()}`;
    }
    if (usage.groupBy === 'client_key' && /^[1-9]\d*$/.test(id)) {
      params.set('clientKeyId', id);
      params.set('since', usageRangeSinceParam());
      return `/request-logs?${params.toString()}`;
    }
    if (usage.groupBy === 'provider_account') {
      const accountId = id.split('/').pop() ?? '';
      if (!/^[1-9]\d*$/.test(accountId)) return '';
      params.set('providerAccountId', accountId);
      params.set('since', usageRangeSinceParam());
      return `/request-logs?${params.toString()}`;
    }
    if (usage.groupBy === 'routing_pool' && /^[1-9]\d*$/.test(id)) {
      params.set('routingPoolId', id);
      params.set('since', usageRangeSinceParam());
      return `/request-logs?${params.toString()}`;
    }
    if (usage.groupBy === 'routing_pool_chain') {
      params.set('routingPoolChain', id);
      params.set('since', usageRangeSinceParam());
      return `/request-logs?${params.toString()}`;
    }
    if (usage.groupBy === 'session') {
      params.set('sessionId', id);
      params.set('since', usageRangeSinceParam());
      return `/request-logs?${params.toString()}`;
    }
    if (usage.groupBy === 'usage_source') {
      params.set('usageSource', id);
      params.set('since', usageRangeSinceParam());
      return `/request-logs?${params.toString()}`;
    }
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

<AuthGate>
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
        <p class="text-xs font-medium text-[#6e6e6e]">{range}</p>
        <p class="mt-2 text-2xl font-semibold tabular-nums text-[#0d0d0d]">{formatTokens(summary?.totalTokens)}</p>
        <p class="mt-1 text-sm text-[#6e6e6e]">{formatTokens(summary?.totalRequests)} requests · {formatCostMicrousd(summary?.estimatedCostMicrousd)}</p>
        <div class="mt-3 grid gap-2 text-xs text-[#6e6e6e] sm:grid-cols-2">
          <div>
            <p class="font-medium text-[#3c3c3c]">Cached input tokens</p>
            <p class="mt-1 font-mono tabular-nums">{formatTokens(summary?.totalCachedInputTokens)}</p>
          </div>
          <div>
            <p class="font-medium text-[#3c3c3c]">Reasoning tokens</p>
            <p class="mt-1 font-mono tabular-nums">{formatTokens(summary?.totalReasoningTokens)}</p>
          </div>
        </div>
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
          <th class="px-4 py-3 font-medium">Cached input</th>
          <th class="px-4 py-3 font-medium">Reasoning</th>
          <th class="px-4 py-3 font-medium">Estimated cost</th>
        </tr>
      </thead>
      <tbody class="divide-y divide-[#ededed]">
        {#if usage.loading && !usage.current}
          <tr>
            <td class="px-4 py-5 text-[#6e6e6e]" colspan="8">Loading usage summary...</td>
          </tr>
        {:else if !usage.current?.rows?.length}
          <tr>
            <td class="px-4 py-5 text-[#6e6e6e]" colspan="8">No usage in this range.</td>
          </tr>
        {:else}
          {#each usage.current.rows as row}
            <tr>
              <td class="px-4 py-3 font-medium text-[#0d0d0d]">
                {#if usageRowHref(row)}
                  <a class="max-w-[260px] truncate inline-block underline-offset-2 hover:underline" href={usageRowHref(row)}>
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
              <td class="px-4 py-3 font-mono text-[13px] tabular-nums text-[#3c3c3c]">{formatTokens(row.cachedInputTokens)}</td>
              <td class="px-4 py-3 font-mono text-[13px] tabular-nums text-[#3c3c3c]">{formatTokens(row.reasoningTokens)}</td>
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
          class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
          type="button"
          disabled={usagePricing.loading || usagePricing.saving || usagePricing.syncing}
          onclick={syncOfficialUsagePricing}
        >
          {usagePricing.syncing ? 'Syncing' : 'Sync official'}
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
    {:else if usagePricing.syncMessage}
      <p class="mt-4 rounded-md border border-[#cce7db] bg-[#e8f5f0] p-3 text-sm text-[#0a7a5e]">{usagePricing.syncMessage}</p>
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
          {#if !usagePricing.rows?.length}
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
      <div class="flex items-center gap-2 mt-2">
        <a
          class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] shrink-0"
          href={exportRequestLogsURL("csv")}
          target="_blank" rel="noopener noreferrer"
        >
          Export CSV
        </a>
        <a
          class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] shrink-0"
          href={exportRequestLogsURL("json")}
          target="_blank" rel="noopener noreferrer"
        >
          Export JSON
        </a>
        <a
          class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] shrink-0"
          href={exportRequestLogsURL("jsonl")}
          target="_blank" rel="noopener noreferrer"
        >
          Export JSONL
        </a>
      </div>
    </div>
    <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
      <label class="block text-sm font-medium text-[#3c3c3c]">
        Search
        <input
          class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={requestLogs.query}
          placeholder="key, account, model, route, error"
        />
      </label>
      <label class="block text-sm font-medium text-[#3c3c3c]">
        Request ID filter
        <input
          class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={requestLogs.requestId}
          placeholder="req_..."
        />
      </label>
      <label class="block text-sm font-medium text-[#3c3c3c]">
        Model filter
        <input
          class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={requestLogs.model}
          placeholder="gpt-5"
        />
      </label>
      <label class="block text-sm font-medium text-[#3c3c3c]">
        Session filter
        <input
          class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={requestLogs.sessionId}
          placeholder="workspace-123"
        />
      </label>
      <label class="block text-sm font-medium text-[#3c3c3c]">
        Error code filter
        <input
          class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
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
        Status code filter
        <input
          class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={requestLogs.statusCode}
          placeholder="503"
          inputmode="numeric"
          pattern="[1-5][0-9][0-9]"
        />
      </label>
      <label class="block text-sm font-medium text-[#3c3c3c]">
        Since
        <input
          class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={requestLogs.since}
          placeholder="Unix seconds"
          inputmode="numeric"
          pattern="[0-9]*"
        />
      </label>
      <label class="block text-sm font-medium text-[#3c3c3c]">
        Usage source
        <select
          class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={requestLogs.usageSource}
        >
          {#each usageSourceFilters as usageSource}
            <option value={usageSource.value}>{usageSource.label}</option>
          {/each}
        </select>
      </label>
      <label class="block text-sm font-medium text-[#3c3c3c]">
        Routing error
        <select
          class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
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
          class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={requestLogs.routingPoolChain}
          placeholder="primary -> secondary"
        />
      </label>
      <label class="block text-sm font-medium text-[#3c3c3c]">
        Provider account
        <select
          class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
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
          class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
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
          class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={requestLogs.clientKeyId}
        >
          <option value="all">All API keys</option>
          {#each apiKeys.items as key}
            <option value={String(key.id)}>{key.name} ({key.prefix})</option>
          {/each}
        </select>
      </label>
      <label class="flex items-center gap-2 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#3c3c3c]">
        <input
          class="h-4 w-4 rounded border-[#d9d9d9] text-[#10a37f] focus:ring-[#10a37f]"
          type="checkbox"
          bind:checked={requestLogs.gatewayFallbacks}
        />
        Only fallback requests
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
                  href={routingPoolFallbackChainHref(log)}
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
          <p class="mt-1 text-xs text-[#6e6e6e]">{formatTokens(log.cachedInputTokens)} Cached</p>
          <p class="mt-1 text-xs text-[#6e6e6e]">{formatTokens(log.reasoningTokens)} Reasoning</p>
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
          {#if errorHref(log)}
            <a
              class="underline-offset-2 hover:underline"
              href={errorHref(log)}
              title={log.error || ''}
              aria-label="View same error logs"
            >
              {errorLabel(log.error)}
            </a>
          {:else}
            <span title={log.error || ''}>{errorLabel(log.error)}</span>
          {/if}
        </td>
      </tr>
    {/each}
  {/if}
</tbody>
    </table>
  </div>
</section>
</div>
</AuthGate>
