<script>
  import { page } from '$app/state';
  import {
    accountLabel,
    apiKeys,
    formatDate,
    formatRequestLogCost,
    formatTokens,
    loadKeys,
    loadProviderAccounts,
    loadRoutingPools,
    loadRequestLogs,
    providerAccounts,
    requestLogs,
    resetRequestLogFilters,
    routingPools,
    session,
  } from '$lib/admin-state.svelte.js';

  import AuthGate from '$lib/AuthGate.svelte';
  import { ChevronDown, Download, Search, SlidersHorizontal, X } from 'lucide-svelte';

  let providerAccountsRequested = $state(false);
  let routingPoolsRequested = $state(false);
  let apiKeysRequested = $state(false);
  /** @type {string | null} */
  let appliedRequestLogSearch = $state(null);
  let showAdvancedFilters = $state(false);
  let requestLogDateRange = $state('all');
  let requestLogPage = $state(1);
  let requestLogPageSize = $state(10);

  let requestLogPageCount = $derived(Math.max(1, Math.ceil(requestLogs.items.length / requestLogPageSize)));
  let normalizedRequestLogPage = $derived(Math.min(Math.max(requestLogPage, 1), requestLogPageCount));
  let paginatedRequestLogs = $derived(
    requestLogs.items.slice(
      (normalizedRequestLogPage - 1) * requestLogPageSize,
      normalizedRequestLogPage * requestLogPageSize
    )
  );
  let requestLogPageSummary = $derived(
    requestLogs.items.length === 0
      ? '0'
      : `${(normalizedRequestLogPage - 1) * requestLogPageSize + 1}-${Math.min(
          normalizedRequestLogPage * requestLogPageSize,
          requestLogs.items.length
        )}`
  );
  let advancedRequestLogFilterCount = $derived(
    [
      requestLogs.requestId.trim(),
      requestLogs.model.trim(),
      requestLogs.sessionId.trim(),
      requestLogs.errorCode.trim(),
      requestLogs.statusCode.trim(),
      requestLogs.usageSource !== 'all',
      requestLogs.routingPoolError !== 'all',
      requestLogs.routingPoolChain.trim(),
      requestLogs.providerAccountId !== 'all',
      requestLogs.routingPoolId !== 'all',
      requestLogs.clientKeyId !== 'all',
      requestLogs.gatewayFallbacks
    ].filter(Boolean).length
  );
  let activeRequestLogFilterCount = $derived(
    advancedRequestLogFilterCount
      + (requestLogs.query.trim() ? 1 : 0)
      + (requestLogs.statusClass !== 'all' ? 1 : 0)
      + (requestLogs.since ? 1 : 0)
  );

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
        'provider_test',
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
    requestLogDateRange = requestLogDateRangeForSince(requestLogs.since);

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

    showAdvancedFilters = [
      'requestId', 'providerAccountId', 'routingPoolId', 'clientKeyId', 'model', 'sessionId',
      'error', 'usageSource', 'statusCode', 'routingPoolError', 'routingPoolChain', 'gatewayFallbacks'
    ].some((key) => params.has(key));
  }
  /** @param {string} [format] */
  function exportRequestLogsURL(format) {
    const params = new URLSearchParams();
    if (requestLogs.requestId) params.set('requestId', requestLogs.requestId);
    if (requestLogs.query) params.set('q', requestLogs.query);
    if (requestLogs.statusClass && requestLogs.statusClass !== 'all') params.set('statusClass', requestLogs.statusClass);
    if (/^[1-5]\d\d$/.test(requestLogs.statusCode)) params.set('statusCode', requestLogs.statusCode);
    if (/^\d+$/.test(requestLogs.since)) params.set('since', requestLogs.since);
    if (requestLogs.providerAccountId && requestLogs.providerAccountId !== 'all') params.set('providerAccountId', requestLogs.providerAccountId);
    if (requestLogs.routingPoolId && requestLogs.routingPoolId !== 'all') params.set('routingPoolId', requestLogs.routingPoolId);
    if (requestLogs.clientKeyId && requestLogs.clientKeyId !== 'all') params.set('clientKeyId', requestLogs.clientKeyId);
    if (requestLogs.model) params.set('model', requestLogs.model);
    if (requestLogs.sessionId) params.set('sessionId', requestLogs.sessionId);
    if (requestLogs.errorCode) params.set('error', requestLogs.errorCode);
    if (requestLogs.usageSource && requestLogs.usageSource !== 'all') params.set('usageSource', requestLogs.usageSource);
    if (requestLogs.routingPoolError && requestLogs.routingPoolError !== 'all') params.set('routingPoolError', requestLogs.routingPoolError);
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
      appliedRequestLogSearch = null;
      return;
    }
    if (appliedRequestLogSearch !== page.url.search) {
      appliedRequestLogSearch = page.url.search;
      applyRequestLogURLFilters(page.url.search);
      requestLogPage = 1;
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
    if (value === 'provider_test') return 'Provider test';
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

  const requestLogDateRanges = [
    { value: 'all', label: 'All time', seconds: 0 },
    { value: '24h', label: 'Last 24 hours', seconds: 86400 },
    { value: '7d', label: 'Last 7 days', seconds: 604800 },
    { value: '30d', label: 'Last 30 days', seconds: 2592000 }
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
    { value: 'provider_test', label: 'Provider test' },
    { value: 'gemini_usage_metadata', label: 'Gemini metadata' },
    { value: 'anthropic_usage', label: 'Anthropic usage' }
  ];

  /** @param {string} since */
  function requestLogDateRangeForSince(since) {
    if (!/^\d+$/.test(since)) return 'all';
    const age = Math.floor(Date.now() / 1000) - Number(since);
    const matchedRange = requestLogDateRanges.find(
      (range) => range.seconds > 0 && Math.abs(range.seconds - age) <= 300
    );
    return matchedRange?.value ?? 'custom';
  }

  /** @param {Event & { currentTarget: HTMLSelectElement }} event */
  function changeRequestLogDateRange(event) {
    requestLogDateRange = event.currentTarget.value;
    const selectedRange = requestLogDateRanges.find((range) => range.value === requestLogDateRange);
    requestLogs.since = selectedRange?.seconds
      ? String(Math.max(0, Math.floor(Date.now() / 1000) - selectedRange.seconds))
      : '';
  }

  function applyRequestLogFilters() {
    requestLogPage = 1;
    void loadRequestLogs();
  }

  function clearRequestLogFilters() {
    resetRequestLogFilters();
    requestLogDateRange = 'all';
    showAdvancedFilters = false;
    requestLogPage = 1;
    void loadRequestLogs();
  }

  /** @param {SubmitEvent} event */
  function submitRequestLogFilters(event) {
    event.preventDefault();
    applyRequestLogFilters();
  }

  /** @param {number} targetPage */
  function goToRequestLogPage(targetPage) {
    requestLogPage = Math.min(Math.max(targetPage, 1), requestLogPageCount);
  }

</script>

<svelte:head>
  <title>N2API Request Logs</title>
</svelte:head>


<AuthGate>
<div class="ui-page">
<header class="ui-page-header">
  <div class="ui-page-heading">
    <h1 class="ui-page-title">Request logs</h1>
    <p class="ui-page-description">Search gateway requests and inspect routing, latency, token usage, and failures.</p>
  </div>
  <div class="ui-page-actions">
    <details class="group relative">
      <summary class="ui-button ui-button--sm ui-button--secondary cursor-pointer list-none [&::-webkit-details-marker]:hidden">
        <Download class="size-4" aria-hidden="true" />
        Export
        <ChevronDown class="size-3.5 transition-transform group-open:rotate-180" aria-hidden="true" />
      </summary>
      <div class="absolute right-0 z-30 mt-2 w-40 rounded-lg border border-[#e5e5e5] bg-white p-1 shadow-lg">
        <a class="ui-button ui-button--sm ui-button--start w-full" href={exportRequestLogsURL("csv")} target="_blank" rel="noopener noreferrer">CSV</a>
        <a class="ui-button ui-button--sm ui-button--start w-full" href={exportRequestLogsURL("json")} target="_blank" rel="noopener noreferrer">JSON</a>
        <a class="ui-button ui-button--sm ui-button--start w-full" href={exportRequestLogsURL("jsonl")} target="_blank" rel="noopener noreferrer">JSONL</a>
      </div>
    </details>
  </div>
</header>
<section aria-label="Request log search">
  <form class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4" onsubmit={submitRequestLogFilters}>
    <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-[minmax(18rem,1fr)_11rem_11rem_auto_auto] xl:items-end">
      <label class="grid gap-1 text-sm font-medium text-[#3c3c3c]">
        Search
        <input
          class="h-9 w-full min-w-0 rounded-lg border border-[#e5e5e5] bg-white px-3 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={requestLogs.query}
          placeholder="key, account, model, route, error"
        />
      </label>
      <label class="grid gap-1 text-sm font-medium text-[#3c3c3c]">
        Status
        <select
          class="h-9 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={requestLogs.statusClass}
        >
          {#each requestLogStatusClasses as statusClass}
            <option value={statusClass.value}>{statusClass.label}</option>
          {/each}
        </select>
      </label>
      <label class="grid gap-1 text-sm font-medium text-[#3c3c3c]">
        Date
        <select
          class="h-9 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          value={requestLogDateRange}
          onchange={changeRequestLogDateRange}
        >
          {#if requestLogDateRange === 'custom'}
            <option value="custom" disabled>Custom start time</option>
          {/if}
          {#each requestLogDateRanges as range}
            <option value={range.value}>{range.label}</option>
          {/each}
        </select>
      </label>
      <button
        class="ui-button ui-button--sm ui-button--secondary"
        type="button"
        aria-expanded={showAdvancedFilters}
        aria-controls="request-log-advanced-filters"
        onclick={() => (showAdvancedFilters = !showAdvancedFilters)}
      >
        <SlidersHorizontal class="size-4" aria-hidden="true" />
        {showAdvancedFilters ? 'Fewer filters' : 'More filters'}{advancedRequestLogFilterCount ? ` (${advancedRequestLogFilterCount})` : ''}
      </button>
      <button
        class="ui-button ui-button--sm ui-button--primary"
        type="submit"
        disabled={requestLogs.loading}
      >
        <Search class="size-4" aria-hidden="true" />
        {requestLogs.loading ? 'Searching' : 'Search'}
      </button>
    </div>

    {#if showAdvancedFilters}
    <div id="request-log-advanced-filters" class="mt-4 grid grid-cols-1 gap-3 border-t border-[#e5e5e5] pt-4 sm:grid-cols-2 lg:grid-cols-3">
      <label class="grid gap-1 text-sm font-medium text-[#3c3c3c]">
        Request ID filter
        <input
          class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={requestLogs.requestId}
          placeholder="req_..."
        />
      </label>
      <label class="grid gap-1 text-sm font-medium text-[#3c3c3c]">
        Model filter
        <input
          class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={requestLogs.model}
          placeholder="gpt-5"
        />
      </label>
      <label class="grid gap-1 text-sm font-medium text-[#3c3c3c]">
        Session filter
        <input
          class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={requestLogs.sessionId}
          placeholder="workspace-123"
        />
      </label>
      <label class="grid gap-1 text-sm font-medium text-[#3c3c3c]">
        Error code filter
        <input
          class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={requestLogs.errorCode}
          placeholder="api_key_token_rate_limited"
        />
      </label>
      <label class="grid gap-1 text-sm font-medium text-[#3c3c3c]">
        Status code filter
        <input
          class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={requestLogs.statusCode}
          placeholder="503"
          inputmode="numeric"
          pattern="[1-5][0-9][0-9]"
        />
      </label>
      <label class="grid gap-1 text-sm font-medium text-[#3c3c3c]">
        Usage source
        <select
          class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={requestLogs.usageSource}
        >
          {#each usageSourceFilters as usageSource}
            <option value={usageSource.value}>{usageSource.label}</option>
          {/each}
        </select>
      </label>
      <label class="grid gap-1 text-sm font-medium text-[#3c3c3c]">
        Routing error
        <select
          class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={requestLogs.routingPoolError}
        >
          {#each routingPoolErrorFilters as routingPoolError}
            <option value={routingPoolError.value}>{routingPoolError.label}</option>
          {/each}
        </select>
      </label>
      <label class="grid gap-1 text-sm font-medium text-[#3c3c3c]">
        Fallback chain
        <input
          class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={requestLogs.routingPoolChain}
          placeholder="primary -> secondary"
        />
      </label>
      <label class="grid gap-1 text-sm font-medium text-[#3c3c3c]">
        Provider account
        <select
          class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={requestLogs.providerAccountId}
        >
          <option value="all">All provider accounts</option>
          {#each providerAccounts.items as account}
            <option value={String(account.id)}>{accountLabel(account)}</option>
          {/each}
        </select>
      </label>
      <label class="grid gap-1 text-sm font-medium text-[#3c3c3c]">
        Routing pool
        <select
          class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={requestLogs.routingPoolId}
        >
          <option value="all">All routing pools</option>
          {#each routingPools.items as pool}
            <option value={String(pool.id)}>{pool.name}</option>
          {/each}
        </select>
      </label>
      <label class="grid gap-1 text-sm font-medium text-[#3c3c3c]">
        API key
        <select
          class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
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
    </div>
    {/if}
    {#if activeRequestLogFilterCount > 0}
      <div class="mt-4 flex flex-wrap items-center justify-between gap-2 border-t border-[#e5e5e5] pt-3 text-xs text-[#6e6e6e]">
        <p>{activeRequestLogFilterCount} {activeRequestLogFilterCount === 1 ? 'filter' : 'filters'} active</p>
        <button class="ui-button ui-button--sm ui-button--secondary" type="button" onclick={clearRequestLogFilters}>
          <X class="size-3.5" aria-hidden="true" />
          Clear filters
        </button>
      </div>
    {/if}
  </form>

  {#if requestLogs.error}
    <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">
{requestLogs.error}
    </p>
  {/if}

  <div class="mt-5 flex flex-wrap items-center justify-between gap-2">
    <h2 class="ui-section-title">Requests</h2>
    <p class="text-xs text-[#6e6e6e]">{requestLogs.items.length} loaded</p>
  </div>

  <div class="ui-table-shell ui-table-shell--scroll mt-3">
    <table class="ui-table ui-table--stacked min-w-[1040px]">
      <thead class="sticky top-0 z-20">
        <tr>
          <th class="w-[140px]">Time</th>
          <th class="w-[180px]">Request</th>
          <th class="w-[240px]">Attribution</th>
          <th class="w-[160px]">Model</th>
          <th class="w-[220px]">Usage</th>
          <th class="w-[180px]">Result</th>
        </tr>
      </thead>
      <tbody>
        {#if requestLogs.loading && requestLogs.items.length === 0}
          <tr>
            <td class="ui-table-empty ui-table-empty--loading" colspan="6">Loading request logs...</td>
          </tr>
        {:else if requestLogs.items.length === 0}
          <tr>
            <td class="ui-table-empty" colspan="6">
              <div class="flex flex-wrap items-center justify-between gap-3">
                <span>{activeRequestLogFilterCount ? 'No requests match the current filters.' : 'No gateway requests yet.'}</span>
                {#if activeRequestLogFilterCount}
                  <button class="ui-button ui-button--sm ui-button--secondary" type="button" onclick={clearRequestLogFilters}>Clear filters</button>
                {/if}
              </div>
            </td>
          </tr>
        {:else}
          {#each paginatedRequestLogs as log}
            {@const requestLogCost = formatRequestLogCost(log)}
            <tr>
              <td class="whitespace-nowrap text-[#3c3c3c]" data-label="Time">{formatDate(log.createdAt)}</td>
              <td class="min-w-[190px]" data-label="Request">
                <div class="min-w-0">
                  <p class="font-mono text-[13px] text-[#0d0d0d]">{log.method} {log.route}</p>
                  <p class="mt-1 max-w-[240px] truncate font-mono text-xs text-[#6e6e6e]" title={log.requestId || ''}>{log.requestId || 'No request ID'}</p>
                </div>
              </td>
              <td class="min-w-[240px] text-[#3c3c3c]" data-label="Attribution">
                <div class="grid gap-1.5">
                  {#if log.clientKeyId}
                    <a class="max-w-[220px] truncate font-medium text-[#0d0d0d] hover:text-[#0a7a5e]" href={`/api-keys?clientKeyId=${log.clientKeyId}`} title="View API key" aria-label="View API key">
                      {log.clientKey || `Key ${log.clientKeyId}`}
                    </a>
                  {:else}
                    <span>{log.clientKey || 'Unknown key'}</span>
                  {/if}
                  {#if log.providerAccountId}
                    <a class="max-w-[220px] truncate text-xs text-[#6e6e6e] hover:text-[#0a7a5e]" href={`/providers?providerAccountId=${log.providerAccountId}`} title="View provider account" aria-label="View provider account">
                      {log.providerAccountName || `Account ${log.providerAccountId}`} · {log.provider || 'Unknown'} · {accountTypeLabel(log.providerAccountType)}
                    </a>
                  {:else}
                    <span class="text-xs text-[#6e6e6e]">Unassigned provider</span>
                  {/if}
                  {#if log.routingPoolId}
                    <a class="max-w-[220px] truncate text-xs text-[#6e6e6e] hover:text-[#0a7a5e]" href={`/routing-pools?routingPoolId=${log.routingPoolId}`} title="View routing pool" aria-label="View routing pool">
                      {log.routingPoolName || `Pool ${log.routingPoolId}`}
                    </a>
                  {:else}
                    <span class="text-xs text-[#6e6e6e]">Global pool</span>
                  {/if}
                </div>
              </td>
              <td class="min-w-[170px]" data-label="Model">
                <div class="min-w-0">
                  <p class="font-mono text-[13px] text-[#3c3c3c]">{log.model || '-'}</p>
                  <p class="mt-1 max-w-[180px] truncate font-mono text-xs text-[#6e6e6e]" title={log.sessionId || ''}>Session {log.sessionId || '-'}</p>
                </div>
              </td>
              <td class="min-w-[190px] font-mono text-[13px] tabular-nums text-[#3c3c3c]" data-label="Usage">
                <div class="min-w-0">
                  <p>{formatTokens(log.inputTokens)} in / {formatTokens(log.outputTokens)} out</p>
                  <p class="mt-1 text-xs text-[#6e6e6e]">{formatTokens(log.totalTokens)} total · {formatTokens(log.cachedInputTokens)} cached</p>
                  <p class="mt-1 text-xs text-[#6e6e6e]">{formatTokens(log.reasoningTokens)} reasoning · {usageSourceLabel(log.usageSource)}</p>
                  <p class="mt-1 font-sans text-xs text-[#6e6e6e]">Estimated cost {requestLogCost}</p>
                </div>
              </td>
              <td class="min-w-[180px]" data-label="Result">
                <div class="min-w-0">
                  <div class="flex items-center gap-2">
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
                    <span class="font-mono text-xs tabular-nums text-[#6e6e6e]">{log.latencyMs}ms</span>
                  </div>
                  <div class="mt-2 text-xs text-[#3c3c3c]">
                  {#if errorHref(log)}
                    <a class="underline-offset-2 hover:underline" href={errorHref(log)} title={log.error || ''} aria-label="View same error logs">{errorLabel(log.error)}</a>
                  {:else}
                    <span title={log.error || ''}>{errorLabel(log.error)}</span>
                  {/if}
                  </div>
                  <p class="mt-2 text-xs tabular-nums text-[#6e6e6e]">{log.gatewayAttemptCount ?? 0} attempts · {log.gatewayFallbackCount ?? 0} fallbacks</p>
                  {#if log.routingPoolFallbackDepth > 0}
                    <p class="mt-1 text-xs text-[#6e6e6e]">Fallback depth {log.routingPoolFallbackDepth}</p>
                  {/if}
                  {#if log.routingPoolFallbackChain}
                    <a class="mt-1 block max-w-[170px] truncate text-xs text-[#6e6e6e] hover:text-[#0a7a5e]" href={routingPoolFallbackChainHref(log)} title={log.routingPoolFallbackChain} aria-label="View fallback chain logs">
                      {log.routingPoolFallbackChain}
                    </a>
                  {/if}
                  {#if log.routingPoolError}
                    <p class="mt-1 text-xs font-medium text-amber-700">{errorLabel(log.routingPoolError)}</p>
                  {/if}
                </div>
              </td>
            </tr>
          {/each}
        {/if}
      </tbody>
    </table>
  </div>
  <div class="ui-pagination mt-4 flex flex-col gap-3 text-sm text-[#6e6e6e] sm:flex-row sm:items-center sm:justify-between">
    <p>Showing {requestLogPageSummary} of {requestLogs.items.length} loaded</p>
    <div class="flex flex-wrap items-center gap-2">
      <label class="inline-flex items-center gap-2 text-xs font-medium text-[#3c3c3c]">
        Rows
        <select
          class="rounded-lg border border-[#e5e5e5] bg-white px-2 py-1.5 text-xs text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={requestLogPageSize}
          onchange={() => {
            requestLogPage = 1;
          }}
        >
          <option value={10}>10</option>
          <option value={20}>20</option>
          <option value={50}>50</option>
        </select>
      </label>
      <span class="text-xs tabular-nums text-[#6e6e6e]">Page {normalizedRequestLogPage} of {requestLogPageCount}</span>
      <button
        class="ui-button ui-button--sm ui-button--secondary rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
        type="button"
        disabled={normalizedRequestLogPage <= 1}
        onclick={() => goToRequestLogPage(normalizedRequestLogPage - 1)}
      >
        Previous
      </button>
      <button
        class="ui-button ui-button--sm ui-button--secondary rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
        type="button"
        disabled={normalizedRequestLogPage >= requestLogPageCount}
        onclick={() => goToRequestLogPage(normalizedRequestLogPage + 1)}
      >
        Next
      </button>
    </div>
  </div>
</section>
</div>



</AuthGate>
