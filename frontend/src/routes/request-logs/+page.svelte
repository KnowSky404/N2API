<script>
  import { goto } from '$app/navigation';
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
    requestLogFilterParams,
    resetRequestLogFilters,
    routingPools,
    session,
  } from '$lib/admin-state.svelte.js';

  import AuthGate from '$lib/AuthGate.svelte';
  import { ChevronDown, Download, Eye, Search, SlidersHorizontal, X } from 'lucide-svelte';

  let providerAccountsRequested = $state(false);
  let routingPoolsRequested = $state(false);
  let apiKeysRequested = $state(false);
  /** @type {string | null} */
  let appliedRequestLogSearch = $state(null);
  let showAdvancedFilters = $state(false);
  let requestLogDateRange = $state('all');
  let selectedRequestLog = $state(/** @type {import('$lib/admin-state.svelte.js').RequestLog | null} */ (null));
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
  const requestLogExportRangeTitle = 'Export requires a bounded date range. Apply a date filter first.';
  const requestLogExportLoadingTitle = 'Wait for the current request log results to load.';
  let requestLogExportReady = $derived(requestLogs.appliedFilterQuery !== null);
  let appliedRequestLogExportSince = $derived(
    Number(new URLSearchParams(requestLogs.appliedFilterQuery ?? '').get('since') ?? 0)
  );
  let appliedRequestLogExportHasSince = $derived(
    requestLogExportReady
      && Number.isSafeInteger(appliedRequestLogExportSince)
      && appliedRequestLogExportSince > 0
      && appliedRequestLogExportSince < Math.floor(Date.now() / 1000)
  );

  /** @param {'csv' | 'json' | 'jsonl'} format @param {boolean} [gzip] */
  function exportRequestLogsURL(format, gzip = false) {
    const params = new URLSearchParams(requestLogs.appliedFilterQuery ?? '');
    params.set('format', format);
    if (appliedRequestLogExportHasSince) {
      params.set('before', String(Math.floor(Date.now() / 1000)));
    }
    if (format === 'json') params.set('limit', '200');
    if (gzip) params.set('gzip', '1');
    return '/api/admin/request-logs/export?' + params.toString();
  }

  /** @param {'csv' | 'json' | 'jsonl'} format */
  function requestLogExportEnabled(format) {
    return format === 'json' ? requestLogExportReady : appliedRequestLogExportHasSince;
  }

  /** @param {'csv' | 'json' | 'jsonl'} format */
  function requestLogExportTitle(format) {
    if (!requestLogExportReady) return requestLogExportLoadingTitle;
    return format === 'json' || appliedRequestLogExportHasSince ? undefined : requestLogExportRangeTitle;
  }

  /**
   * @param {MouseEvent & { currentTarget: HTMLAnchorElement }} event
   * @param {'csv' | 'json' | 'jsonl'} format
   * @param {boolean} [gzip]
   */
  function prepareRequestLogExport(event, format, gzip = false) {
    if (!requestLogExportEnabled(format)) {
      event.preventDefault();
      return;
    }
    event.currentTarget.href = exportRequestLogsURL(format, gzip);
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

  async function applyRequestLogFilters() {
    const search = requestLogFilterParams().toString();
    const nextSearch = search ? `?${search}` : '';
    if (page.url.search === nextSearch) {
      await loadRequestLogs();
      return;
    }
    await goto(search ? `/request-logs?${search}` : '/request-logs', {
      keepFocus: true,
      noScroll: true
    });
  }

  async function clearRequestLogFilters() {
    resetRequestLogFilters();
    requestLogDateRange = 'all';
    showAdvancedFilters = false;
    if (page.url.search === '') {
      await loadRequestLogs();
      return;
    }
    await goto('/request-logs', { keepFocus: true, noScroll: true });
  }

  /** @param {SubmitEvent} event */
  function submitRequestLogFilters(event) {
    event.preventDefault();
    void applyRequestLogFilters();
  }

  /** @param {import('$lib/admin-state.svelte.js').RequestLog} log */
  function openRequestLogDetails(log) {
    selectedRequestLog = log;
  }

  function closeRequestLogDetails() {
    selectedRequestLog = null;
  }

  /** @param {KeyboardEvent} event */
  function handleRequestLogKeydown(event) {
    if (event.key === 'Escape' && selectedRequestLog) closeRequestLogDetails();
  }

  $effect(() => {
    const selected = selectedRequestLog;
    if (!selected) return;
    const current = requestLogs.items.find((item) => item.id === selected.id);
    if (current && current !== selected) {
      selectedRequestLog = current;
    } else if (!current && !requestLogs.loading && !requestLogs.loadingOlder) {
      selectedRequestLog = null;
    }
  });

</script>

<svelte:window onkeydown={handleRequestLogKeydown} />

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
      <div class="absolute left-0 z-30 mt-2 w-44 max-w-[calc(100vw-2rem)] rounded-lg border border-[#e5e5e5] bg-white p-1 shadow-lg sm:left-auto sm:right-0">
        <a
          class="ui-button ui-button--sm ui-button--start w-full"
          data-sveltekit-reload
          class:cursor-not-allowed={!requestLogExportEnabled("json")}
          class:opacity-50={!requestLogExportEnabled("json")}
          href={requestLogExportEnabled("json") ? exportRequestLogsURL("json") : undefined}
          aria-disabled={!requestLogExportEnabled("json")}
          tabindex={requestLogExportEnabled("json") ? undefined : -1}
          title={requestLogExportTitle("json")}
          onpointerdown={(event) => prepareRequestLogExport(event, "json")}
          oncontextmenu={(event) => prepareRequestLogExport(event, "json")}
          onclick={(event) => prepareRequestLogExport(event, "json")}
        >JSON</a>
        <a
          class="ui-button ui-button--sm ui-button--start w-full"
          data-sveltekit-reload
          class:cursor-not-allowed={!requestLogExportEnabled("csv")}
          class:opacity-50={!requestLogExportEnabled("csv")}
          href={requestLogExportEnabled("csv") ? exportRequestLogsURL("csv") : undefined}
          aria-disabled={!requestLogExportEnabled("csv")}
          tabindex={requestLogExportEnabled("csv") ? undefined : -1}
          title={requestLogExportTitle("csv")}
          onpointerdown={(event) => prepareRequestLogExport(event, "csv")}
          oncontextmenu={(event) => prepareRequestLogExport(event, "csv")}
          onclick={(event) => prepareRequestLogExport(event, "csv")}
        >CSV</a>
        <a
          class="ui-button ui-button--sm ui-button--start w-full"
          data-sveltekit-reload
          class:cursor-not-allowed={!requestLogExportEnabled("csv")}
          class:opacity-50={!requestLogExportEnabled("csv")}
          href={requestLogExportEnabled("csv") ? exportRequestLogsURL("csv", true) : undefined}
          aria-disabled={!requestLogExportEnabled("csv")}
          tabindex={requestLogExportEnabled("csv") ? undefined : -1}
          title={requestLogExportTitle("csv")}
          onpointerdown={(event) => prepareRequestLogExport(event, "csv", true)}
          oncontextmenu={(event) => prepareRequestLogExport(event, "csv", true)}
          onclick={(event) => prepareRequestLogExport(event, "csv", true)}
        >CSV gzip</a>
        <a
          class="ui-button ui-button--sm ui-button--start w-full"
          data-sveltekit-reload
          class:cursor-not-allowed={!requestLogExportEnabled("jsonl")}
          class:opacity-50={!requestLogExportEnabled("jsonl")}
          href={requestLogExportEnabled("jsonl") ? exportRequestLogsURL("jsonl") : undefined}
          aria-disabled={!requestLogExportEnabled("jsonl")}
          tabindex={requestLogExportEnabled("jsonl") ? undefined : -1}
          title={requestLogExportTitle("jsonl")}
          onpointerdown={(event) => prepareRequestLogExport(event, "jsonl")}
          oncontextmenu={(event) => prepareRequestLogExport(event, "jsonl")}
          onclick={(event) => prepareRequestLogExport(event, "jsonl")}
        >JSONL</a>
        <a
          class="ui-button ui-button--sm ui-button--start w-full"
          data-sveltekit-reload
          class:cursor-not-allowed={!requestLogExportEnabled("jsonl")}
          class:opacity-50={!requestLogExportEnabled("jsonl")}
          href={requestLogExportEnabled("jsonl") ? exportRequestLogsURL("jsonl", true) : undefined}
          aria-disabled={!requestLogExportEnabled("jsonl")}
          tabindex={requestLogExportEnabled("jsonl") ? undefined : -1}
          title={requestLogExportTitle("jsonl")}
          onpointerdown={(event) => prepareRequestLogExport(event, "jsonl", true)}
          oncontextmenu={(event) => prepareRequestLogExport(event, "jsonl", true)}
          onclick={(event) => prepareRequestLogExport(event, "jsonl", true)}
        >JSONL gzip</a>
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
    <table class="ui-table ui-table--stacked min-w-[820px]">
      <thead class="sticky top-0 z-20">
        <tr>
          <th class="w-[140px]">Time</th>
          <th class="w-[230px]">Request</th>
          <th class="w-[180px]">Model</th>
          <th class="w-[170px]">Usage</th>
          <th class="w-[150px]">Result</th>
          <th class="w-[72px] text-center">Details</th>
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
          {#each requestLogs.items as log (log.id)}
            {@const requestLogCost = formatRequestLogCost(log)}
            <tr>
              <td class="whitespace-nowrap text-[#3c3c3c]" data-label="Time">{formatDate(log.createdAt)}</td>
              <td class="min-w-[220px]" data-label="Request">
                <div class="flex min-w-0 items-center gap-2">
                  <span class="shrink-0 rounded-md bg-[#f5f5f5] px-1.5 py-0.5 font-mono text-[11px] font-medium text-[#3c3c3c]">{log.method || '-'}</span>
                  <span class="min-w-0 truncate font-mono text-[13px] text-[#0d0d0d]" title={log.route || ''}>{log.route || '-'}</span>
                </div>
              </td>
              <td class="min-w-[170px]" data-label="Model">
                <p class="max-w-[180px] truncate font-mono text-[13px] text-[#3c3c3c]" title={log.model || ''}>{log.model || '-'}</p>
              </td>
              <td class="min-w-[160px] font-mono text-[13px] tabular-nums text-[#3c3c3c]" data-label="Usage">
                <div class="min-w-0">
                  <p>{formatTokens(log.totalTokens)} tokens</p>
                  <p class="mt-1 font-sans text-xs text-[#6e6e6e]">{requestLogCost}</p>
                </div>
              </td>
              <td class="min-w-[140px]" data-label="Result">
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
                  <span class="whitespace-nowrap font-mono text-xs tabular-nums text-[#6e6e6e]">{log.latencyMs}ms</span>
                </div>
              </td>
              <td class="sm:text-center" data-label="Details">
                <button
                  class="ui-button ui-button--icon ui-button--secondary justify-self-start rounded-md border border-[#e5e5e5] bg-white text-[#6e6e6e] hover:bg-[#f5f5f5] hover:text-[#0d0d0d] sm:mx-auto"
                  type="button"
                  aria-label={`View details for request ${log.requestId || log.id}`}
                  title="View request details"
                  onclick={() => openRequestLogDetails(log)}
                >
                  <Eye class="size-4" aria-hidden="true" />
                </button>
              </td>
            </tr>
          {/each}
        {/if}
      </tbody>
    </table>
  </div>
  <div class="ui-pagination mt-4 flex flex-col gap-3 text-sm text-[#6e6e6e] sm:flex-row sm:items-center sm:justify-between">
    <p>
      Showing {requestLogs.items.length} loaded {requestLogs.items.length === 1 ? 'request' : 'requests'}
      {#if requestLogs.hasMore}<span class="text-[#8e8e8e]"> · More available</span>{/if}
    </p>
    {#if requestLogs.hasMore}
      <button
        class="ui-button ui-button--sm ui-button--secondary rounded-md border border-[#e5e5e5] bg-white px-3 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:opacity-60"
        type="button"
        disabled={requestLogs.loadingOlder || requestLogs.loading}
        onclick={() => void loadRequestLogs({ append: true })}
      >
        <ChevronDown class="size-3.5" aria-hidden="true" />
        {requestLogs.loadingOlder ? 'Loading...' : 'Load older'}
      </button>
    {/if}
  </div>
</section>
</div>



</AuthGate>

{#if selectedRequestLog}
  {@const selectedRequestLogCost = formatRequestLogCost(selectedRequestLog)}
  <!-- svelte-ignore a11y_click_events_have_key_events,a11y_no_static_element_interactions -->
  <div
    class="ui-modal-backdrop ui-modal-backdrop--top fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/30 px-4 py-[6vh]"
    role="dialog"
    aria-modal="true"
    aria-labelledby="request-log-detail-title"
    tabindex="-1"
    onclick={(event) => event.target === event.currentTarget && closeRequestLogDetails()}
  >
    <section class="ui-modal-panel ui-modal-panel--lg max-h-[88vh] w-full max-w-3xl overflow-y-auto rounded-xl border border-[#ededed] bg-white p-5 shadow-xl">
      <div class="flex items-start justify-between gap-4">
        <div class="min-w-0">
          <h2 id="request-log-detail-title" class="text-lg font-semibold text-[#0d0d0d]">Request details</h2>
          <p class="mt-1 truncate font-mono text-[13px] text-[#6e6e6e]" title={selectedRequestLog.requestId || ''}>{selectedRequestLog.requestId || `Log ${selectedRequestLog.id}`}</p>
        </div>
        <button class="ui-button ui-button--icon shrink-0 rounded-md text-[#6e6e6e] hover:bg-[#f5f5f5] hover:text-[#0d0d0d]" type="button" aria-label="Close request details" onclick={closeRequestLogDetails}>
          <X class="size-4" aria-hidden="true" />
        </button>
      </div>

      <dl class="mt-5 grid gap-x-6 gap-y-4 sm:grid-cols-2">
        <div><dt class="text-xs font-medium text-[#6e6e6e]">Time</dt><dd class="mt-1 text-sm text-[#0d0d0d]">{formatDate(selectedRequestLog.createdAt)}</dd></div>
        <div>
          <dt class="text-xs font-medium text-[#6e6e6e]">Result</dt>
          <dd class="mt-1 flex items-center gap-2">
            <span class={['inline-flex rounded-full px-2.5 py-1 text-xs font-medium tabular-nums', selectedRequestLog.statusCode >= 500 ? 'bg-red-50 text-red-700' : selectedRequestLog.statusCode >= 400 ? 'bg-amber-50 text-amber-700' : 'bg-[#e8f5f0] text-[#0a7a5e]']}>{selectedRequestLog.statusCode}</span>
            <span class="font-mono text-xs tabular-nums text-[#6e6e6e]">{selectedRequestLog.latencyMs}ms</span>
          </dd>
        </div>
        <div class="sm:col-span-2"><dt class="text-xs font-medium text-[#6e6e6e]">Route</dt><dd class="mt-1 break-all font-mono text-[13px] text-[#0d0d0d]">{selectedRequestLog.method || '-'} {selectedRequestLog.route || '-'}</dd></div>
        <div><dt class="text-xs font-medium text-[#6e6e6e]">Model</dt><dd class="mt-1 break-all font-mono text-[13px] text-[#0d0d0d]">{selectedRequestLog.model || '-'}</dd></div>
        <div><dt class="text-xs font-medium text-[#6e6e6e]">Usage source</dt><dd class="mt-1 text-sm text-[#0d0d0d]">{usageSourceLabel(selectedRequestLog.usageSource)}</dd></div>
        <div class="sm:col-span-2"><dt class="text-xs font-medium text-[#6e6e6e]">Request ID</dt><dd class="mt-1 break-all font-mono text-[13px] text-[#0d0d0d]">{selectedRequestLog.requestId || '-'}</dd></div>
        <div class="sm:col-span-2"><dt class="text-xs font-medium text-[#6e6e6e]">Session ID</dt><dd class="mt-1 break-all font-mono text-[13px] text-[#0d0d0d]">{selectedRequestLog.sessionId || '-'}</dd></div>
      </dl>

      <section class="mt-6 border-t border-[#ededed] pt-5" aria-labelledby="request-log-attribution-title">
        <h3 id="request-log-attribution-title" class="text-sm font-semibold text-[#0d0d0d]">Attribution</h3>
        <dl class="mt-3 grid gap-x-6 gap-y-4 sm:grid-cols-2">
          <div>
            <dt class="text-xs font-medium text-[#6e6e6e]">API key</dt>
            <dd class="mt-1 text-sm text-[#0d0d0d]">
              {#if selectedRequestLog.clientKeyId}
                <a class="break-words font-medium hover:text-[#0a7a5e]" href={`/api-keys?clientKeyId=${selectedRequestLog.clientKeyId}`} title="View API key">{selectedRequestLog.clientKey || `Key ${selectedRequestLog.clientKeyId}`}</a>
              {:else}
                {selectedRequestLog.clientKey || 'Unknown key'}
              {/if}
            </dd>
          </div>
          <div>
            <dt class="text-xs font-medium text-[#6e6e6e]">Provider account</dt>
            <dd class="mt-1 text-sm text-[#0d0d0d]">
              {#if selectedRequestLog.providerAccountId}
                <a class="break-words font-medium hover:text-[#0a7a5e]" href={`/providers?providerAccountId=${selectedRequestLog.providerAccountId}`} title="View provider account">{selectedRequestLog.providerAccountName || `Account ${selectedRequestLog.providerAccountId}`}</a>
                <span class="mt-1 block text-xs text-[#6e6e6e]">{selectedRequestLog.provider || 'Unknown'} · {accountTypeLabel(selectedRequestLog.providerAccountType)}</span>
              {:else}
                Unassigned provider
              {/if}
            </dd>
          </div>
          <div>
            <dt class="text-xs font-medium text-[#6e6e6e]">Routing pool</dt>
            <dd class="mt-1 text-sm text-[#0d0d0d]">
              {#if selectedRequestLog.routingPoolId}
                <a class="break-words font-medium hover:text-[#0a7a5e]" href={`/routing-pools?routingPoolId=${selectedRequestLog.routingPoolId}`} title="View routing pool">{selectedRequestLog.routingPoolName || `Pool ${selectedRequestLog.routingPoolId}`}</a>
              {:else}
                Global pool
              {/if}
            </dd>
          </div>
        </dl>
      </section>

      <section class="mt-6 border-t border-[#ededed] pt-5" aria-labelledby="request-log-usage-title">
        <div class="flex flex-wrap items-baseline justify-between gap-2">
          <h3 id="request-log-usage-title" class="text-sm font-semibold text-[#0d0d0d]">Usage</h3>
          <span class="text-xs text-[#6e6e6e]">Estimated cost {selectedRequestLogCost}</span>
        </div>
        <dl class="mt-3 grid grid-cols-2 gap-x-6 gap-y-4 sm:grid-cols-5">
          <div><dt class="text-xs font-medium text-[#6e6e6e]">Input</dt><dd class="mt-1 font-mono text-[13px] tabular-nums text-[#0d0d0d]">{formatTokens(selectedRequestLog.inputTokens)}</dd></div>
          <div><dt class="text-xs font-medium text-[#6e6e6e]">Output</dt><dd class="mt-1 font-mono text-[13px] tabular-nums text-[#0d0d0d]">{formatTokens(selectedRequestLog.outputTokens)}</dd></div>
          <div><dt class="text-xs font-medium text-[#6e6e6e]">Cached</dt><dd class="mt-1 font-mono text-[13px] tabular-nums text-[#0d0d0d]">{formatTokens(selectedRequestLog.cachedInputTokens)}</dd></div>
          <div><dt class="text-xs font-medium text-[#6e6e6e]">Reasoning</dt><dd class="mt-1 font-mono text-[13px] tabular-nums text-[#0d0d0d]">{formatTokens(selectedRequestLog.reasoningTokens)}</dd></div>
          <div><dt class="text-xs font-medium text-[#6e6e6e]">Total</dt><dd class="mt-1 font-mono text-[13px] font-medium tabular-nums text-[#0d0d0d]">{formatTokens(selectedRequestLog.totalTokens)}</dd></div>
        </dl>
      </section>

      <section class="mt-6 border-t border-[#ededed] pt-5" aria-labelledby="request-log-routing-title">
        <h3 id="request-log-routing-title" class="text-sm font-semibold text-[#0d0d0d]">Gateway diagnostics</h3>
        <dl class="mt-3 grid gap-x-6 gap-y-4 sm:grid-cols-3">
          <div><dt class="text-xs font-medium text-[#6e6e6e]">Attempts</dt><dd class="mt-1 font-mono text-[13px] tabular-nums text-[#0d0d0d]">{selectedRequestLog.gatewayAttemptCount ?? 0}</dd></div>
          <div><dt class="text-xs font-medium text-[#6e6e6e]">Fallbacks</dt><dd class="mt-1 font-mono text-[13px] tabular-nums text-[#0d0d0d]">{selectedRequestLog.gatewayFallbackCount ?? 0}</dd></div>
          <div><dt class="text-xs font-medium text-[#6e6e6e]">Fallback depth</dt><dd class="mt-1 font-mono text-[13px] tabular-nums text-[#0d0d0d]">{selectedRequestLog.routingPoolFallbackDepth ?? 0}</dd></div>
          <div class="sm:col-span-3">
            <dt class="text-xs font-medium text-[#6e6e6e]">Fallback chain</dt>
            <dd class="mt-1 break-all font-mono text-[13px] text-[#0d0d0d]">
              {#if routingPoolFallbackChainHref(selectedRequestLog)}
                <a class="underline-offset-2 hover:text-[#0a7a5e] hover:underline" href={routingPoolFallbackChainHref(selectedRequestLog)}>{selectedRequestLog.routingPoolFallbackChain}</a>
              {:else}
                -
              {/if}
            </dd>
          </div>
          <div class="sm:col-span-3"><dt class="text-xs font-medium text-[#6e6e6e]">Routing error</dt><dd class="mt-1 break-all font-mono text-[13px] text-[#0d0d0d]">{selectedRequestLog.routingPoolError ? errorLabel(selectedRequestLog.routingPoolError) : '-'}</dd></div>
        </dl>
      </section>

      {#if selectedRequestLog.error}
        <section class="mt-6 border-t border-[#ededed] pt-5" aria-labelledby="request-log-error-title">
          <h3 id="request-log-error-title" class="text-sm font-semibold text-[#0d0d0d]">Error</h3>
          <a class="mt-2 block break-all font-mono text-[13px] text-red-700 underline-offset-2 hover:underline" href={errorHref(selectedRequestLog)} aria-label="View same error logs">{errorLabel(selectedRequestLog.error)}</a>
          <p class="mt-1 break-all font-mono text-xs text-[#6e6e6e]">{selectedRequestLog.error}</p>
        </section>
      {/if}
    </section>
  </div>
{/if}
