<script>
  import {
    apiKeys,
    formatCostMicrousd,
    formatDate,
    formatTokens,
    gatewayLimitLabel,
    gatewaySettings,
    getGatewayReadyKeys,
    getGatewayReadinessIssues,
    getRoutableModelCount,
    getSchedulableProviderAccounts,
    getStatusItems,
    getUnschedulableProviderAccountSummary,
    health,
    loadOpsDashboard,
    modelRouting,
    opsMonitor,
    providerAccounts,
    requestLogs,
    usage
  } from '$lib/admin-state.svelte.js';

  import AuthGate from '$lib/AuthGate.svelte';
  import { ArrowRight, RefreshCw, TriangleAlert } from 'lucide-svelte';

  const statusItems = $derived(getStatusItems());
  const gatewayReadyKeys = $derived(getGatewayReadyKeys());
  const schedulableAccounts = $derived(getSchedulableProviderAccounts());
  const unschedulableAccountSummary = $derived(getUnschedulableProviderAccountSummary());
  const routableModelCount = $derived(getRoutableModelCount());
  const readinessIssues = $derived(getGatewayReadinessIssues());
  const usage24h = $derived(usage.summaries['24h:model'] ?? null);
  const usage24hProviderAccounts = $derived(usage.summaries['24h:provider_account'] ?? null);
  const usage24hUsageSources = $derived(usage.summaries['24h:usage_source'] ?? null);
  const usage24hRoutingPools = $derived(usage.summaries['24h:routing_pool'] ?? null);
  const usage24hRoutingPoolChains = $derived(usage.summaries['24h:routing_pool_chain'] ?? null);
  const usage24hClientKeys = $derived(usage.summaries['24h:client_key'] ?? null);
  const usage24hSessions = $derived(usage.summaries['24h:session'] ?? null);
  const usageSections = $derived([
    { key: 'models', title: 'Top models', data: usage24h },
    { key: 'accounts', title: 'Top provider accounts', data: usage24hProviderAccounts },
    { key: 'sources', title: 'Top usage sources', data: usage24hUsageSources },
    { key: 'pools', title: 'Top routing pools', data: usage24hRoutingPools },
    { key: 'chains', title: 'Top routing pool chains', data: usage24hRoutingPoolChains },
    { key: 'keys', title: 'Top client keys', data: usage24hClientKeys },
    { key: 'sessions', title: 'Top sessions', data: usage24hSessions }
  ]);

  let selectedUsageKey = $state('models');
  const selectedUsageSection = $derived(
    usageSections.find((section) => section.key === selectedUsageKey) ?? usageSections[0]
  );

  /** @param {string | number | null | undefined} id */
  function providerAccountUsageId(id) {
    const value = String(id ?? '');
    const parts = value.split('/');
    const accountId = parts[parts.length - 1] ?? '';
    return /^[1-9]\d*$/.test(accountId) ? accountId : '';
  }

  function dashboardUsageSinceParam() {
    return String(Math.max(0, Math.floor(Date.now() / 1000) - 86400));
  }

  /** @param {URLSearchParams} params */
  function dashboardUsageHrefWithSince(params) {
    params.set('since', dashboardUsageSinceParam());
    return `/request-logs?${params.toString()}`;
  }

  /** @param {{ key?: string | number | null }} bucket */
  function dashboardOpsErrorHref(bucket) {
    const key = String(bucket?.key ?? '').trim();
    const params = new URLSearchParams();
    if (key) {
      params.set('error', key);
    }
    return dashboardUsageHrefWithSince(params);
  }

  /** @param {{ key?: string | number | null }} bucket */
  function dashboardCostModelHref(bucket) {
    const key = String(bucket?.key ?? '').trim();
    const params = new URLSearchParams();
    if (key && key !== 'unknown') {
      params.set('model', key);
    }
    return dashboardUsageHrefWithSince(params);
  }

  /** @param {{ key?: string | number | null }} bucket */
  function dashboardCostProviderAccountHref(bucket) {
    const key = String(bucket?.key ?? '').trim();
    const params = new URLSearchParams();
    if (key && key !== 'unknown') {
      params.set('providerAccountId', key);
    }
    return dashboardUsageHrefWithSince(params);
  }

  /** @param {{ key?: string | number | null }} bucket */
  function dashboardCostClientKeyHref(bucket) {
    const key = String(bucket?.key ?? '').trim();
    const params = new URLSearchParams();
    if (key && key !== 'unknown') {
      params.set('clientKeyId', key);
    }
    return dashboardUsageHrefWithSince(params);
  }

  /**
   * @param {string} sectionTitle
   * @param {{ id?: string | number | null }} row
   */
  function dashboardUsageHref(sectionTitle, row) {
    const id = String(row?.id ?? '');
    if (!id || id === 'unknown') return '';
    const params = new URLSearchParams();
    if (sectionTitle === 'Top models') {
      params.set('model', id);
      return dashboardUsageHrefWithSince(params);
    }
    if (sectionTitle === 'Top provider accounts') {
      const accountId = providerAccountUsageId(id);
      if (!accountId) return '';
      params.set('providerAccountId', accountId);
      return dashboardUsageHrefWithSince(params);
    }
    if (sectionTitle === 'Top usage sources') {
      params.set('usageSource', id);
      return dashboardUsageHrefWithSince(params);
    }
    if (sectionTitle === 'Top routing pools' && /^[1-9]\d*$/.test(id)) {
      params.set('routingPoolId', id);
      return dashboardUsageHrefWithSince(params);
    }
    if (sectionTitle === 'Top routing pool chains' && id !== 'none') {
      params.set('routingPoolChain', id);
      return dashboardUsageHrefWithSince(params);
    }
    if (sectionTitle === 'Top client keys' && /^[1-9]\d*$/.test(id)) {
      params.set('clientKeyId', id);
      return dashboardUsageHrefWithSince(params);
    }
    if (sectionTitle === 'Top sessions' && id !== 'none') {
      params.set('sessionId', id);
      return dashboardUsageHrefWithSince(params);
    }
    return '';
  }

  /** @param {import('$lib/admin-state.svelte.js').RequestLog} log */
  function dashboardLogHref(log) {
    if (log.requestId) {
      return `/request-logs?requestId=${encodeURIComponent(log.requestId)}`;
    }
    const params = new URLSearchParams();
    if (log.clientKeyId) params.set('clientKeyId', String(log.clientKeyId));
    if (log.providerAccountId) params.set('providerAccountId', String(log.providerAccountId));
    if (log.routingPoolId) params.set('routingPoolId', String(log.routingPoolId));
    if (log.model) params.set('model', log.model);
    if (log.sessionId) params.set('sessionId', log.sessionId);
    const query = params.toString();
    if (!query) return '/request-logs';
    return `/request-logs?${query}`;
  }

  /** @param {string | null | undefined} value */
  function statusDotClass(value) {
    const normalized = String(value ?? '').toLowerCase();
    if (['ok', 'online', 'connected', 'healthy'].includes(normalized)) {
      return 'bg-[#10a37f]';
    }
    if (['checking', 'loading'].some((state) => normalized.includes(state))) {
      return 'bg-[#9b9b9b]';
    }
    return 'bg-amber-500';
  }

  /** @param {number | null | undefined} statusCode */
  function requestStatusClass(statusCode) {
    if ((statusCode ?? 0) >= 500) return 'text-red-700';
    if ((statusCode ?? 0) >= 400) return 'text-amber-700';
    return 'text-[#0a7a5e]';
  }

  /** @param {KeyboardEvent} event @param {number} index */
  function handleUsageTabKeydown(event, index) {
    let nextIndex = index;
    if (event.key === 'ArrowRight') nextIndex = (index + 1) % usageSections.length;
    else if (event.key === 'ArrowLeft') nextIndex = (index - 1 + usageSections.length) % usageSections.length;
    else if (event.key === 'Home') nextIndex = 0;
    else if (event.key === 'End') nextIndex = usageSections.length - 1;
    else return;

    event.preventDefault();
    selectedUsageKey = usageSections[nextIndex].key;
    const currentTab = /** @type {HTMLElement} */ (event.currentTarget);
    const tabs = currentTab.parentElement?.querySelectorAll('[role="tab"]');
    requestAnimationFrame(() => /** @type {HTMLElement | undefined} */ (tabs?.[nextIndex])?.focus());
  }

  /** @param {string} issue */
  function readinessIssueHref(issue) {
    if (issue.includes('API key')) return '/api-keys';
    if (issue.includes('model')) return '/models?status=blocked';
    if (issue.includes('schedulable')) return '/providers?status=blocked';
    return '/providers';
  }
</script>

<svelte:head>
  <title>N2API Dashboard</title>
</svelte:head>
<AuthGate>
  <header class="flex flex-col gap-5 border-b border-[#ededed] pb-6 sm:flex-row sm:items-end sm:justify-between">
    <div>
      <h1 class="text-[32px] font-semibold leading-[1.15] text-[#0d0d0d]">Dashboard</h1>
      <p class="mt-2 text-sm text-[#6e6e6e]">Gateway health, capacity, and usage over the last 24 hours.</p>
    </div>

    <dl class="relative flex flex-wrap items-center gap-x-5 gap-y-2 text-sm">
      {#each statusItems as item (item.label)}
        <div class="flex items-center gap-2">
          <span class={`h-2 w-2 shrink-0 rounded-full ${statusDotClass(item.value)}`}></span>
          <dt class="text-[#6e6e6e]">{item.label}</dt>
          <dd class="font-medium capitalize text-[#0d0d0d]">{item.value}</dd>
        </div>
      {/each}
      {#if health.build}
        <div class="flex min-w-0 items-center gap-2">
          <dt class="text-[#6e6e6e]">Build</dt>
          <dd class="min-w-0">
            <details>
              <summary
                class="max-w-48 cursor-help list-none truncate rounded-sm font-mono text-xs font-medium text-[#0d0d0d] outline-none focus-visible:ring-2 focus-visible:ring-[#10a37f] focus-visible:ring-offset-2 [&::-webkit-details-marker]:hidden"
                title={`Commit ${health.build.commit} | Built ${formatDate(health.build.builtAt)}`}
                aria-describedby="dashboard-build-details"
                aria-label={`Build ${health.build.version}, commit ${health.build.commit}, built ${formatDate(health.build.builtAt)}`}
              >
                {health.build.version}
              </summary>
              <div
                id="dashboard-build-details"
                class="pointer-events-none absolute right-0 top-full z-20 mt-2 w-80 max-w-[calc(100vw-2rem)] rounded-md border border-[#dedede] bg-white p-3 text-xs text-[#3c3c3c] shadow-lg"
              >
                <p class="break-all font-mono">Commit {health.build.commit}</p>
                <p class="mt-1">Built {formatDate(health.build.builtAt)}</p>
              </div>
            </details>
          </dd>
        </div>
      {/if}
    </dl>
  </header>

  {#if health.error}
    <section class="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-800" aria-live="polite">
      {health.error}
    </section>
  {/if}

  {#if !health.error && !providerAccounts.loading && !providerAccounts.error && !modelRouting.loading && !modelRouting.error && !apiKeys.loading && !apiKeys.error && readinessIssues.length > 0}
    <section class="overflow-hidden rounded-lg border border-amber-200 bg-amber-50" aria-labelledby="gateway-attention-title">
      <div class="flex items-center gap-2 border-b border-amber-200 px-4 py-3">
        <TriangleAlert class="h-4 w-4 shrink-0 text-amber-700" aria-hidden="true" />
        <h2 id="gateway-attention-title" class="text-sm font-semibold text-amber-900">Gateway attention</h2>
      </div>
      <div class="divide-y divide-amber-200">
        {#each readinessIssues as issue (issue)}
          <div class="flex items-center justify-between gap-4 px-4 py-3 text-sm">
            <p class="min-w-0 text-amber-900">{issue}</p>
            <a class="shrink-0 font-medium text-amber-900 underline underline-offset-2" href={readinessIssueHref(issue)} aria-label={`Resolve: ${issue}`}>Resolve</a>
          </div>
        {/each}
      </div>
    </section>
  {/if}

  <section class="overflow-hidden rounded-lg border border-[#ededed] bg-white" aria-labelledby="gateway-overview-title">
    <div class="flex flex-col gap-1 border-b border-[#ededed] px-4 py-4 sm:px-5">
      <h2 id="gateway-overview-title" class="text-lg font-semibold text-[#0d0d0d]">Gateway overview</h2>
      <p class="text-sm text-[#6e6e6e]">Capacity and 24h traffic in one operational summary.</p>
    </div>

    <div class="border-b border-[#ededed] px-4 py-3 sm:px-5">
      <p class="text-[13px] font-medium text-[#6e6e6e]">Gateway capacity</p>
    </div>
    <dl class="grid grid-cols-2 gap-px bg-[#ededed] sm:grid-cols-3 lg:grid-cols-5">
      <div class="min-w-0 bg-white p-4">
        <dt class="text-[13px] font-medium text-[#6e6e6e]">Provider accounts</dt>
        <dd class="mt-2 text-xl font-semibold tabular-nums text-[#0d0d0d]">{providerAccounts.loading ? '—' : providerAccounts.items.length}</dd>
      </div>
      <div class="min-w-0 bg-white p-4">
        <dt class="text-[13px] font-medium text-[#6e6e6e]">Schedulable accounts</dt>
        <dd class="mt-2">
          <a class="text-xl font-semibold tabular-nums text-[#0d0d0d] underline-offset-2 hover:underline" href="/providers?status=active">
            {providerAccounts.loading ? '—' : schedulableAccounts.length}
          </a>
        </dd>
      </div>
      <div class="min-w-0 bg-white p-4">
        <dt class="text-[13px] font-medium text-[#6e6e6e]">Unschedulable accounts</dt>
        <dd class="mt-2">
          <a class={`text-xl font-semibold tabular-nums underline-offset-2 hover:underline ${providerAccounts.items.length - schedulableAccounts.length > 0 ? 'text-amber-700' : 'text-[#0d0d0d]'}`} href="/providers?status=blocked">
            {providerAccounts.loading ? '—' : providerAccounts.items.length - schedulableAccounts.length}
          </a>
        </dd>
        {#if !providerAccounts.loading && unschedulableAccountSummary.length > 0}
          <p class="mt-1 break-words text-xs text-[#6e6e6e]">
            {unschedulableAccountSummary.map((item) => `${item.reasonLabel}: ${item.count}`).join(' · ')}
          </p>
        {/if}
      </div>
      <div class="min-w-0 bg-white p-4">
        <dt class="text-[13px] font-medium text-[#6e6e6e]">Routable models</dt>
        <dd class="mt-2 text-xl font-semibold tabular-nums text-[#0d0d0d]">{modelRouting.loading ? '—' : routableModelCount}</dd>
      </div>
      <div class="col-span-2 min-w-0 bg-white p-4 sm:col-span-1">
        <dt class="text-[13px] font-medium text-[#6e6e6e]">Gateway-ready API keys</dt>
        <dd class="mt-2 text-xl font-semibold tabular-nums text-[#0d0d0d]">{apiKeys.loading || apiKeys.error ? '-' : gatewayReadyKeys.length}</dd>
      </div>
    </dl>

    <div class="border-y border-[#ededed] px-4 py-3 sm:px-5">
      <p class="text-[13px] font-medium text-[#6e6e6e]">24h usage</p>
    </div>
    <dl class="grid grid-cols-2 gap-px bg-[#ededed] sm:grid-cols-4">
      <div class="min-w-0 bg-white p-4">
        <dt class="text-[13px] font-medium text-[#6e6e6e]">Requests</dt>
        <dd class="mt-2 text-xl font-semibold tabular-nums text-[#0d0d0d]">{usage24h ? formatTokens(usage24h.totalRequests) : '-'}</dd>
      </div>
      <div class="min-w-0 bg-white p-4">
        <dt class="text-[13px] font-medium text-[#6e6e6e]">Tokens</dt>
        <dd class="mt-2 text-xl font-semibold tabular-nums text-[#0d0d0d]">{usage24h ? formatTokens(usage24h.totalTokens) : '-'}</dd>
      </div>
      <div class="min-w-0 bg-white p-4">
        <dt class="text-[13px] font-medium text-[#6e6e6e]">Estimated cost</dt>
        <dd class="mt-2 text-xl font-semibold tabular-nums text-[#0d0d0d]">{usage24h ? formatCostMicrousd(usage24h.estimatedCostMicrousd) : '-'}</dd>
      </div>
      <div class="min-w-0 bg-white p-4">
        <dt class="text-[13px] font-medium text-[#6e6e6e]">Error rate</dt>
        <dd class={`mt-2 text-xl font-semibold tabular-nums ${!opsMonitor.stats ? 'text-[#0d0d0d]' : opsMonitor.stats.errorRate > 0.1 ? 'text-red-700' : opsMonitor.stats.errorRate > 0.02 ? 'text-amber-700' : 'text-[#0a7a5e]'}`}>
          {opsMonitor.stats ? `${((opsMonitor.stats.errorRate ?? 0) * 100).toFixed(1)}%` : '-'}
        </dd>
      </div>
    </dl>
  </section>

  <section class="grid items-start gap-6 lg:grid-cols-[minmax(0,2fr)_minmax(18rem,1fr)]">
    <article class="min-w-0 overflow-hidden rounded-lg border border-[#ededed] bg-white">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div class="px-4 pt-4 sm:px-5 sm:pt-5">
          <h2 class="text-lg font-semibold text-[#0d0d0d]">Usage breakdown</h2>
          <p class="mt-1 text-sm text-[#6e6e6e]">Compare the busiest dimensions, then open matching request logs.</p>
        </div>
        <a
          class="ui-button ui-button--sm ui-button--secondary mr-4 mt-4 shrink-0 sm:mr-5 sm:mt-5"
          href="/request-logs?gatewayFallbacks=1"
        >
          Fallback logs
          <ArrowRight class="h-3.5 w-3.5" aria-hidden="true" />
        </a>
      </div>

      <div class="mt-4 overflow-x-auto border-y border-[#ededed] bg-[#fafafa] px-3 py-2" role="tablist" aria-label="Usage breakdown dimension">
        <div class="flex min-w-max gap-1">
          {#each usageSections as section, index (section.key)}
            <button
              class={[
                'ui-button ui-button--md rounded-md border-0 px-3 text-sm',
                selectedUsageKey === section.key
                  ? 'bg-[#e8e8e8] text-[#0d0d0d]'
                  : 'bg-transparent text-[#6e6e6e] hover:bg-[#f0f0f0] hover:text-[#0d0d0d]'
              ]}
              type="button"
              role="tab"
              id={`usage-tab-${section.key}`}
              aria-controls="usage-breakdown-panel"
              aria-selected={selectedUsageKey === section.key}
              tabindex={selectedUsageKey === section.key ? 0 : -1}
              onclick={() => (selectedUsageKey = section.key)}
              onkeydown={(event) => handleUsageTabKeydown(event, index)}
            >
              {section.title.replace('Top ', '')}
            </button>
          {/each}
        </div>
      </div>

      <div
        id="usage-breakdown-panel"
        role="tabpanel"
        aria-labelledby={`usage-tab-${selectedUsageKey}`}
        tabindex="0"
        class="focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-[#10a37f]"
      >
        {#if usage.error}
          <p class="m-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700 sm:m-5">{usage.error}</p>
        {:else if !selectedUsageSection?.data}
          <p class="ui-loading-state m-5 text-sm text-[#6e6e6e]" aria-live="polite">Loading usage summary...</p>
        {:else if !selectedUsageSection?.data?.rows?.length}
          <p class="px-4 py-8 text-sm text-[#6e6e6e] sm:px-5">No usage in this range.</p>
        {:else}
        <div class="ui-table-shell hidden rounded-none border-0 sm:block">
          <table class="ui-table">
            <thead>
              <tr>
                <th scope="col">{selectedUsageSection.title}</th>
                <th scope="col" class="text-right">Requests</th>
                <th scope="col" class="text-right">Tokens</th>
                <th scope="col" class="text-right">Estimated cost</th>
              </tr>
            </thead>
            <tbody>
              {#each selectedUsageSection.data.rows.slice(0, 5) as row (row.id)}
                {@const href = dashboardUsageHref(selectedUsageSection.title, row)}
                <tr>
                  <td class="min-w-0 px-4 py-3">
                    {#if href}
                      <a class="block max-w-[22rem] truncate font-medium text-[#0d0d0d] underline-offset-2 hover:underline" href={href}>{row.label || row.id}</a>
                    {:else}
                      <span class="block max-w-[22rem] truncate font-medium text-[#0d0d0d]">{row.label || row.id}</span>
                    {/if}
                  </td>
                  <td class="px-4 py-3 text-right font-mono text-[13px] tabular-nums text-[#3c3c3c]">{formatTokens(row.requests)}</td>
                  <td class="px-4 py-3 text-right font-mono text-[13px] tabular-nums text-[#3c3c3c]">{formatTokens(row.totalTokens)}</td>
                  <td class="px-4 py-3 text-right font-mono text-[13px] tabular-nums text-[#3c3c3c]">{formatCostMicrousd(row.estimatedCostMicrousd)}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>

        <div class="divide-y divide-[#ededed] sm:hidden">
          {#each selectedUsageSection.data.rows.slice(0, 5) as row (row.id)}
            {@const href = dashboardUsageHref(selectedUsageSection.title, row)}
            <div class="px-4 py-3">
              {#if href}
                <a class="block truncate text-sm font-medium text-[#0d0d0d] underline-offset-2 hover:underline" href={href}>{row.label || row.id}</a>
              {:else}
                <span class="block truncate text-sm font-medium text-[#0d0d0d]">{row.label || row.id}</span>
              {/if}
              <p class="mt-1 text-xs tabular-nums text-[#6e6e6e]">
                {formatTokens(row.requests)} requests · {formatTokens(row.totalTokens)} tokens · {formatCostMicrousd(row.estimatedCostMicrousd)}
              </p>
            </div>
          {/each}
        </div>
        {/if}
      </div>
    </article>

    <aside class="grid min-w-0 gap-6">
      <article class="overflow-hidden rounded-lg border border-[#ededed] bg-white">
        <div class="flex items-start justify-between gap-3 border-b border-[#ededed] px-4 py-4">
          <div>
            <h2 class="text-lg font-semibold text-[#0d0d0d]">Operations snapshot</h2>
            <p class="mt-1 text-sm text-[#6e6e6e]">24h error pressure and upstream health.</p>
          </div>
          <button
            class="ui-button ui-button--icon ui-button--secondary"
            type="button"
            aria-label={opsMonitor.loading ? 'Refreshing operations snapshot' : 'Refresh operations snapshot'}
            title="Refresh"
            disabled={opsMonitor.loading}
            onclick={() => loadOpsDashboard(86400)}
          >
            <RefreshCw class={`h-4 w-4 ${opsMonitor.loading ? 'animate-spin motion-reduce:animate-none' : ''}`} aria-hidden="true" />
          </button>
        </div>
        {#if opsMonitor.error && !opsMonitor.stats}
          <p class="m-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">{opsMonitor.error}</p>
        {:else if opsMonitor.loading && !opsMonitor.stats}
          <p class="ui-loading-state m-4 text-sm text-[#6e6e6e]" aria-live="polite">Loading operations snapshot...</p>
        {:else}
          <dl class="grid grid-cols-2 gap-px bg-[#ededed]">
            {#each [
              { label: 'Client errors', value: opsMonitor.stats?.clientErrors },
              { label: 'Server errors', value: opsMonitor.stats?.serverErrors },
              { label: 'Rate limited', value: opsMonitor.stats?.rateLimitErrors },
              { label: 'Upstream errors', value: opsMonitor.stats?.upstreamErrors },
              { label: 'Total requests', value: opsMonitor.stats?.totalRequests }
            ] as item (item.label)}
              <div class="bg-white p-4" class:col-span-2={item.label === 'Total requests'}>
                <dt class="text-[13px] font-medium text-[#6e6e6e]">{item.label}</dt>
                <dd class="mt-1 text-base font-semibold tabular-nums text-[#0d0d0d]">{formatTokens(item.value)}</dd>
              </div>
            {/each}
          </dl>

          {#if opsMonitor.stats?.topErrors?.length > 0}
            <div class="border-t border-[#ededed]">
              <h3 class="px-4 pt-4 text-sm font-semibold text-[#0d0d0d]">Top errors</h3>
              <div class="mt-2 divide-y divide-[#ededed]">
                {#each opsMonitor.stats.topErrors.slice(0, 3) as bucket (bucket.key)}
                  <div class="grid grid-cols-[minmax(0,1fr)_auto] gap-2 px-4 py-2.5 text-sm">
                    <a class="min-w-0 truncate font-medium text-[#0d0d0d] underline-offset-2 hover:underline" href={dashboardOpsErrorHref(bucket)}>{bucket.label}</a>
                    <span class="font-mono text-[13px] tabular-nums text-[#6e6e6e]">{formatTokens(bucket.count)}</span>
                  </div>
                {/each}
              </div>
            </div>
          {/if}

          {#if opsMonitor.costBreakdown}
            <details class="border-t border-[#ededed] px-4 py-3">
              <summary class="cursor-pointer text-sm font-semibold text-[#0d0d0d] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[#10a37f]">
                Cost attribution · {formatCostMicrousd(opsMonitor.costBreakdown.estimatedCostMicrousd || 0)}
              </summary>
              <div class="mt-4 grid gap-4">
                {#each [
                  { title: 'Top cost models', rows: opsMonitor.costBreakdown.topModels ?? [], href: dashboardCostModelHref },
                  { title: 'Top cost provider accounts', rows: opsMonitor.costBreakdown.topProviderAccounts ?? [], href: dashboardCostProviderAccountHref },
                  { title: 'Top cost API keys', rows: opsMonitor.costBreakdown.topClientKeys ?? [], href: dashboardCostClientKeyHref }
                ] as section (section.title)}
                  <div>
                    <h4 class="text-[13px] font-medium text-[#6e6e6e]">{section.title}</h4>
                    <div class="mt-2 grid gap-2">
                      {#each section.rows.slice(0, 3) as bucket (bucket.key)}
                        <div class="grid grid-cols-[minmax(0,1fr)_auto] gap-2 text-sm">
                          <a class="min-w-0 truncate font-medium text-[#0d0d0d] underline-offset-2 hover:underline" href={section.href(bucket)}>{bucket.label}</a>
                          <span class="font-mono text-[13px] tabular-nums text-[#6e6e6e]">{formatCostMicrousd(bucket.estimatedCostMicrousd || 0)}</span>
                        </div>
                      {/each}
                    </div>
                  </div>
                {/each}
              </div>
            </details>
          {/if}
        {/if}

        <div class="border-t border-[#ededed] p-3">
          <a class="ui-button ui-button--sm ui-button--secondary w-full" href="/ops">
            Open ops monitor
            <ArrowRight class="h-3.5 w-3.5" aria-hidden="true" />
          </a>
        </div>
      </article>

      <article class="overflow-hidden rounded-lg border border-[#ededed] bg-white">
        <div class="border-b border-[#ededed] px-4 py-4">
          <h2 class="text-lg font-semibold text-[#0d0d0d]">Gateway runtime limits</h2>
          <p class="mt-1 text-sm text-[#6e6e6e]">Current concurrency and rate guards.</p>
        </div>
        {#if gatewaySettings.error}
          <p class="m-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">{gatewaySettings.error}</p>
        {:else if gatewaySettings.loading || !gatewaySettings.data}
          <p class="ui-loading-state m-4 text-sm text-[#6e6e6e]" aria-live="polite">Loading gateway runtime limits...</p>
        {:else}
          <dl class="divide-y divide-[#ededed]">
            {#each [
              { label: 'Gateway concurrency', value: gatewaySettings.data.maxConcurrentGatewayRequests },
              { label: 'Per account concurrency', value: gatewaySettings.data.maxConcurrentRequestsPerAccount },
              { label: 'Per key concurrency', value: gatewaySettings.data.maxConcurrentRequestsPerKey },
              { label: 'Requests per minute', value: gatewaySettings.data.requestsPerMinutePerKey },
              { label: 'Tokens per minute', value: gatewaySettings.data.tokensPerMinutePerKey }
            ] as item (item.label)}
              <div class="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-3 px-4 py-3">
                <dt class="min-w-0 text-[13px] font-medium text-[#6e6e6e]">{item.label}</dt>
                <dd class="font-mono text-[13px] tabular-nums text-[#0d0d0d]">{gatewayLimitLabel(item.value)}</dd>
              </div>
            {/each}
          </dl>
        {/if}
      </article>
    </aside>
  </section>

  <section class="overflow-hidden rounded-lg border border-[#ededed] bg-white" aria-labelledby="recent-activity-title">
    <div class="flex flex-wrap items-start justify-between gap-3 border-b border-[#ededed] px-4 py-4 sm:px-5">
      <div>
        <h2 id="recent-activity-title" class="text-lg font-semibold text-[#0d0d0d]">Recent activity</h2>
        <p class="mt-1 text-sm text-[#6e6e6e]">Latest request log snapshot.</p>
      </div>
      <a class="ui-button ui-button--sm ui-button--secondary" href="/request-logs">
        View request logs
        <ArrowRight class="h-3.5 w-3.5" aria-hidden="true" />
      </a>
    </div>

    {#if requestLogs.error}
      <p class="m-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700 sm:m-5">{requestLogs.error}</p>
    {:else if requestLogs.loading}
      <p class="ui-loading-state m-4 text-sm text-[#6e6e6e] sm:m-5" aria-live="polite">Loading request logs...</p>
    {:else if requestLogs.items.length === 0}
      <p class="px-4 py-8 text-sm text-[#6e6e6e] sm:px-5">No gateway requests yet.</p>
    {:else}
      <div class="ui-table-shell hidden rounded-none border-0 sm:block">
        <table class="ui-table min-w-[46rem]">
          <thead>
            <tr>
              <th scope="col">Time</th>
              <th scope="col">Route</th>
              <th scope="col">Model</th>
              <th scope="col" class="text-right">Status</th>
              <th scope="col" class="text-right">Latency</th>
            </tr>
          </thead>
          <tbody>
            {#each requestLogs.items.slice(0, 5) as log (log.id)}
              <tr>
                <td class="whitespace-nowrap px-4 py-3 text-[13px] text-[#6e6e6e]">{formatDate(log.createdAt)}</td>
                <td class="max-w-[22rem] px-4 py-3">
                  <a class="block truncate font-mono text-[13px] text-[#0d0d0d] underline-offset-2 hover:underline" href={dashboardLogHref(log)}>{log.route}</a>
                </td>
                <td class="max-w-[15rem] px-4 py-3 font-mono text-[13px] text-[#3c3c3c]"><span class="block truncate">{log.model || '—'}</span></td>
                <td class={`px-4 py-3 text-right font-mono text-[13px] font-medium tabular-nums ${requestStatusClass(log.statusCode)}`}>{log.statusCode}</td>
                <td class="whitespace-nowrap px-4 py-3 text-right font-mono text-[13px] tabular-nums text-[#3c3c3c]">{log.latencyMs} ms</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>

      <div class="divide-y divide-[#ededed] sm:hidden">
        {#each requestLogs.items.slice(0, 5) as log (log.id)}
          <div class="px-4 py-3">
            <div class="grid grid-cols-[minmax(0,1fr)_auto] gap-3">
              <a class="block truncate font-mono text-[13px] text-[#0d0d0d] underline-offset-2 hover:underline" href={dashboardLogHref(log)}>{log.route}</a>
              <span class={`font-mono text-[13px] font-medium tabular-nums ${requestStatusClass(log.statusCode)}`}>{log.statusCode}</span>
            </div>
            <p class="mt-1 truncate text-xs text-[#6e6e6e]">{log.model || 'No model'} · {log.latencyMs} ms · {formatDate(log.createdAt)}</p>
          </div>
        {/each}
      </div>
    {/if}
  </section>
</AuthGate>
