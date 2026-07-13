<script>
  import {
    apiKeys,
    accountLabel,
    completeProviderCallback,
    connectProvider,
    copyAuthorizationURL,
    copySecret,
    createKey,
    disconnectProviderAccount,
    formatCostMicrousd,
    formatDate,
    formatTokens,
    gatewayLimitLabel,
    gatewaySettings,
    getActiveKeys,
    getRoutableModelCount,
    getSchedulableProviderAccounts,
    getStatusItems,
    getUnschedulableProviderAccountSummary,
    health,
    loadModelSettings,
    loadOpsDashboard,
    loadProviderAccounts,
    loadRequestLogs,
    logout,
    modelSettings,
    modelRouting,
    opsMonitor,
    provider,
    providerAccounts,
    providerConnectForm,
    providerOAuth,
    refreshProviderAccount,
    requestLogs,
    revokeKey,
    saveModelSettings,
    session,
    statusLabel,
    updateProviderAccount,
    updateProviderAccountPriority,
    usage
  } from '$lib/admin-state.svelte.js';

  import AuthGate from '$lib/AuthGate.svelte';
  const statusItems = $derived(getStatusItems());
  const activeKeys = $derived(getActiveKeys());
  const schedulableAccounts = $derived(getSchedulableProviderAccounts());
  const unschedulableAccountSummary = $derived(getUnschedulableProviderAccountSummary());
  const routableModelCount = $derived(getRoutableModelCount());
  const usage24h = $derived(usage.summaries['24h:model'] ?? null);
  const usage24hProviderAccounts = $derived(usage.summaries['24h:provider_account'] ?? null);
  const usage24hUsageSources = $derived(usage.summaries['24h:usage_source'] ?? null);
  const usage24hRoutingPools = $derived(usage.summaries['24h:routing_pool'] ?? null);
  const usage24hRoutingPoolChains = $derived(usage.summaries['24h:routing_pool_chain'] ?? null);
  const usage24hClientKeys = $derived(usage.summaries['24h:client_key'] ?? null);
  const usage24hSessions = $derived(usage.summaries['24h:session'] ?? null);

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
</script>

<svelte:head>
  <title>N2API Dashboard</title>
</svelte:head>
<AuthGate>
  <!-- Status row -->
  <div class="flex flex-wrap gap-6">
    {#each statusItems as item}
      <div>
        <p class="text-sm font-medium text-[#6e6e6e]">{item.label}</p>
        <p class="mt-1 text-lg font-semibold capitalize text-[#0d0d0d]">{item.value}</p>
      </div>
    {/each}
  </div>

  {#if health.error}
    <section class="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-800">
      {health.error}
    </section>
  {/if}

  <!-- Two-column layout: Gateway overview + 24h usage -->
  <section class="grid gap-6 lg:grid-cols-2">
    <!-- Gateway overview -->
    <article class="rounded-lg border border-[#ededed] bg-white p-6">
      <h2 class="text-2xl font-semibold leading-tight text-[#0d0d0d]">Gateway overview</h2>
      <p class="mt-2 text-sm text-[#6e6e6e]">Provider capacity and client access at a glance.</p>
      <div class="mt-5 grid gap-4 grid-cols-2 sm:grid-cols-3">
        <div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
          <p class="text-sm font-medium text-[#6e6e6e]">Provider accounts</p>
          <p class="mt-2 text-base font-semibold text-[#0d0d0d]">{providerAccounts.loading ? 'Loading' : providerAccounts.items.length}</p>
        </div>
        <div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
          <p class="text-sm font-medium text-[#6e6e6e]">Schedulable accounts</p>
          <a class="mt-2 block text-base font-semibold text-[#0d0d0d] underline-offset-2 hover:underline" href="/providers?status=active">
            {providerAccounts.loading ? 'Loading' : schedulableAccounts.length}
          </a>
        </div>
        <div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
          <p class="text-sm font-medium text-[#6e6e6e]">Unschedulable accounts</p>
          <a class="mt-2 block text-base font-semibold text-[#0d0d0d] underline-offset-2 hover:underline" href="/providers?status=blocked">
            {providerAccounts.loading ? 'Loading' : providerAccounts.items.length - schedulableAccounts.length}
          </a>
          {#if !providerAccounts.loading && unschedulableAccountSummary.length > 0}
            <p class="mt-2 text-xs text-[#6e6e6e]">
              {unschedulableAccountSummary.map((item) => `${item.reasonLabel}: ${item.count}`).join(' · ')}
            </p>
          {/if}
        </div>
        <div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
          <p class="text-sm font-medium text-[#6e6e6e]">Routable models</p>
          <p class="mt-2 text-base font-semibold text-[#0d0d0d]">{modelRouting.loading ? 'Loading' : routableModelCount}</p>
        </div>
        <div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
          <p class="text-sm font-medium text-[#6e6e6e]">Active API keys</p>
          <p class="mt-2 text-base font-semibold text-[#0d0d0d]">{activeKeys.length}</p>
        </div>
      </div>
      <div class="mt-6 border-t border-[#ededed] pt-5">
        <h3 class="text-base font-semibold text-[#0d0d0d]">Gateway runtime limits</h3>
        <p class="mt-1 text-sm text-[#6e6e6e]">Current concurrency and rate guards from the running service.</p>
        {#if gatewaySettings.error}
          <p class="mt-3 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">
            {gatewaySettings.error}
          </p>
        {:else if gatewaySettings.loading || !gatewaySettings.data}
          <p class="ui-loading-state mt-4 text-sm text-[#6e6e6e]" aria-live="polite">Loading gateway runtime limits...</p>
        {:else}
          <dl class="mt-4 grid gap-3 grid-cols-2 sm:grid-cols-3">
            <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
              <dt class="text-sm font-medium text-[#6e6e6e]">Gateway concurrency</dt>
              <dd class="mt-2 font-mono text-lg text-[#0d0d0d]">{gatewayLimitLabel(gatewaySettings.data.maxConcurrentGatewayRequests)}</dd>
            </div>
            <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
              <dt class="text-sm font-medium text-[#6e6e6e]">Per account concurrency</dt>
              <dd class="mt-2 font-mono text-lg text-[#0d0d0d]">{gatewayLimitLabel(gatewaySettings.data.maxConcurrentRequestsPerAccount)}</dd>
            </div>
            <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
              <dt class="text-sm font-medium text-[#6e6e6e]">Per key concurrency</dt>
              <dd class="mt-2 font-mono text-lg text-[#0d0d0d]">{gatewayLimitLabel(gatewaySettings.data.maxConcurrentRequestsPerKey)}</dd>
            </div>
            <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
              <dt class="text-sm font-medium text-[#6e6e6e]">Requests per minute</dt>
              <dd class="mt-2 font-mono text-lg text-[#0d0d0d]">{gatewayLimitLabel(gatewaySettings.data.requestsPerMinutePerKey)}</dd>
            </div>
            <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
              <dt class="text-sm font-medium text-[#6e6e6e]">Tokens per minute</dt>
              <dd class="mt-2 font-mono text-lg text-[#0d0d0d]">{gatewayLimitLabel(gatewaySettings.data.tokensPerMinutePerKey)}</dd>
            </div>
          </dl>
        {/if}
      </div>
    </article>

    <!-- 24h usage -->
    <article class="rounded-lg border border-[#ededed] bg-white p-6">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 class="text-2xl font-semibold leading-tight text-[#0d0d0d]">24h usage</h2>
          <p class="mt-2 text-sm text-[#6e6e6e]">Gateway usage and estimated spend in the last day.</p>
        </div>
        <a
          class="ui-button ui-button--md ui-button--secondary rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] shrink-0"
          href="/request-logs?gatewayFallbacks=1"
        >
          Fallback logs
        </a>
      </div>
      {#if usage.error}
        <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">{usage.error}</p>
      {:else if usage.loading && !usage24h}
        <p class="ui-loading-state mt-5 text-sm text-[#6e6e6e]" aria-live="polite">Loading usage summary...</p>
      {:else}
        <dl class="mt-5 grid gap-4 grid-cols-2 sm:grid-cols-3">
          <div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
            <dt class="text-sm font-medium text-[#6e6e6e]">Requests</dt>
            <dd class="mt-2 text-base font-semibold tabular-nums text-[#0d0d0d]">{formatTokens(usage24h?.totalRequests)}</dd>
          </div>
          <div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
            <dt class="text-sm font-medium text-[#6e6e6e]">Tokens</dt>
            <dd class="mt-2 text-base font-semibold tabular-nums text-[#0d0d0d]">{formatTokens(usage24h?.totalTokens)}</dd>
          </div>
          <div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4 col-span-2 sm:col-span-1">
            <dt class="text-sm font-medium text-[#6e6e6e]">Estimated cost</dt>
            <dd class="mt-2 text-base font-semibold tabular-nums text-[#0d0d0d]">{formatCostMicrousd(usage24h?.estimatedCostMicrousd)}</dd>
          </div>
        </dl>

        <!-- Usage breakdown sections -->
        {#each [
          { title: 'Top models', data: usage24h },
          { title: 'Top provider accounts', data: usage24hProviderAccounts },
          { title: 'Top usage sources', data: usage24hUsageSources },
          { title: 'Top routing pools', data: usage24hRoutingPools },
          { title: 'Top routing pool chains', data: usage24hRoutingPoolChains },
          { title: 'Top client keys', data: usage24hClientKeys },
          { title: 'Top sessions', data: usage24hSessions }
        ] as section}
          <div class="mt-5 rounded-lg border border-[#ededed]">
            <div class="border-b border-[#ededed] bg-[#f5f5f5] px-4 py-3">
              <h3 class="text-sm font-semibold text-[#0d0d0d]">{section.title}</h3>
            </div>
            {#if !section.data?.rows?.length}
              <p class="px-4 py-4 text-sm text-[#6e6e6e]">No usage in this range.</p>
            {:else}
              <div class="divide-y divide-[#ededed]">
                {#each section.data.rows.slice(0, 5) as row}
                  {@const href = dashboardUsageHref(section.title, row)}
                  <div class="grid gap-2 px-4 py-3 text-sm grid-cols-[minmax(0,1fr)_auto]">
                    {#if href}
                      <a class="min-w-0 truncate font-medium text-[#0d0d0d] underline-offset-2 hover:underline" href={href}>{row.label || row.id}</a>
                    {:else}
                      <span class="min-w-0 truncate font-medium text-[#0d0d0d]">{row.label || row.id}</span>
                    {/if}
                    <span class="font-mono text-[13px] tabular-nums text-[#6e6e6e] text-right">
                      {formatTokens(row.requests)} req · {formatTokens(row.totalTokens)} tokens
                    </span>
                  </div>
                {/each}
              </div>
            {/if}
          </div>
        {/each}
      {/if}
    </article>

    <!-- Operations snapshot -->
    <article class="rounded-lg border border-[#ededed] bg-white p-6">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 class="text-2xl font-semibold leading-tight text-[#0d0d0d]">Operations snapshot</h2>
          <p class="mt-2 text-sm text-[#6e6e6e]">24h gateway error pressure and upstream health.</p>
        </div>
        <div class="flex shrink-0 items-center gap-2">
          <button
            class="ui-button ui-button--md ui-button--secondary rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
            type="button"
            disabled={opsMonitor.loading}
            onclick={() => loadOpsDashboard(86400)}
          >
            {opsMonitor.loading ? 'Refreshing' : 'Refresh'}
          </button>
          <a
            class="ui-button ui-button--md ui-button--secondary rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]"
            href="/ops"
          >
            Open ops monitor
          </a>
        </div>
      </div>
      {#if opsMonitor.error && !opsMonitor.stats}
        <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">{opsMonitor.error}</p>
      {:else if opsMonitor.loading && !opsMonitor.stats}
        <p class="ui-loading-state mt-5 text-sm text-[#6e6e6e]" aria-live="polite">Loading operations snapshot...</p>
      {:else}
        <dl class="mt-5 grid gap-4 grid-cols-2 sm:grid-cols-3">
          <div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
            <dt class="text-sm font-medium text-[#6e6e6e]">Error rate</dt>
            <dd class="mt-2 text-base font-semibold tabular-nums {opsMonitor.stats?.errorRate > 0.1 ? 'text-red-700' : opsMonitor.stats?.errorRate > 0.02 ? 'text-amber-700' : 'text-[#0a7a5e]'}">
              {(((opsMonitor.stats?.errorRate ?? 0) * 100).toFixed(1))}%
            </dd>
          </div>
          <div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
            <dt class="text-sm font-medium text-[#6e6e6e]">Client errors</dt>
            <dd class="mt-2 text-base font-semibold tabular-nums text-[#0d0d0d]">{formatTokens(opsMonitor.stats?.clientErrors)}</dd>
          </div>
          <div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
            <dt class="text-sm font-medium text-[#6e6e6e]">Server errors</dt>
            <dd class="mt-2 text-base font-semibold tabular-nums text-[#0d0d0d]">{formatTokens(opsMonitor.stats?.serverErrors)}</dd>
          </div>
          <div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
            <dt class="text-sm font-medium text-[#6e6e6e]">Rate limited</dt>
            <dd class="mt-2 text-base font-semibold tabular-nums text-[#0d0d0d]">{formatTokens(opsMonitor.stats?.rateLimitErrors)}</dd>
          </div>
          <div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
            <dt class="text-sm font-medium text-[#6e6e6e]">Upstream errors</dt>
            <dd class="mt-2 text-base font-semibold tabular-nums text-[#0d0d0d]">{formatTokens(opsMonitor.stats?.upstreamErrors)}</dd>
          </div>
          <div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
            <dt class="text-sm font-medium text-[#6e6e6e]">Total requests</dt>
            <dd class="mt-2 text-base font-semibold tabular-nums text-[#0d0d0d]">{formatTokens(opsMonitor.stats?.totalRequests)}</dd>
          </div>
        </dl>
        {#if opsMonitor.stats?.topErrors?.length > 0}
          <div class="mt-5 rounded-lg border border-[#ededed]">
            <div class="border-b border-[#ededed] bg-[#f5f5f5] px-4 py-3">
              <h3 class="text-sm font-semibold text-[#0d0d0d]">Top errors</h3>
            </div>
            <div class="divide-y divide-[#ededed]">
              {#each opsMonitor.stats.topErrors.slice(0, 5) as bucket}
                <div class="grid grid-cols-[minmax(0,1fr)_auto] gap-2 px-4 py-3 text-sm">
                  <a class="min-w-0 truncate font-medium text-[#0d0d0d] underline-offset-2 hover:underline" href={dashboardOpsErrorHref(bucket)}>
                    {bucket.label}
                  </a>
                  <span class="font-mono text-[13px] tabular-nums text-[#6e6e6e]">{formatTokens(bucket.count)}</span>
                </div>
              {/each}
            </div>
          </div>
        {/if}

        {#if opsMonitor.costBreakdown}
          <div class="mt-5 rounded-lg border border-[#ededed]">
            <div class="border-b border-[#ededed] bg-[#f5f5f5] px-4 py-3">
              <div class="flex items-center justify-between gap-3">
                <h3 class="text-sm font-semibold text-[#0d0d0d]">Cost attribution</h3>
                <span class="font-mono text-[13px] tabular-nums text-[#6e6e6e]">{formatCostMicrousd(opsMonitor.costBreakdown.estimatedCostMicrousd || 0)}</span>
              </div>
            </div>
            <div class="grid gap-0 lg:grid-cols-3">
              <div class="border-b border-[#ededed] p-4 lg:border-b-0 lg:border-r">
                <h4 class="text-sm font-semibold text-[#6e6e6e]">Top cost models</h4>
                <div class="mt-3 space-y-2">
                  {#each (opsMonitor.costBreakdown.topModels ?? []).slice(0, 3) as bucket}
                    <div class="grid grid-cols-[minmax(0,1fr)_auto] gap-2 text-sm">
                      <a class="min-w-0 truncate font-mono text-[13px] font-medium text-[#0d0d0d] underline-offset-2 hover:underline" href={dashboardCostModelHref(bucket)}>
                        {bucket.label}
                      </a>
                      <span class="font-mono text-[13px] tabular-nums text-[#6e6e6e]">{formatCostMicrousd(bucket.estimatedCostMicrousd || 0)}</span>
                    </div>
                  {/each}
                </div>
              </div>
              <div class="border-b border-[#ededed] p-4 lg:border-b-0 lg:border-r">
                <h4 class="text-sm font-semibold text-[#6e6e6e]">Top cost provider accounts</h4>
                <div class="mt-3 space-y-2">
                  {#each (opsMonitor.costBreakdown.topProviderAccounts ?? []).slice(0, 3) as bucket}
                    <div class="grid grid-cols-[minmax(0,1fr)_auto] gap-2 text-sm">
                      <a class="min-w-0 truncate font-medium text-[#0d0d0d] underline-offset-2 hover:underline" href={dashboardCostProviderAccountHref(bucket)}>
                        {bucket.label}
                      </a>
                      <span class="font-mono text-[13px] tabular-nums text-[#6e6e6e]">{formatCostMicrousd(bucket.estimatedCostMicrousd || 0)}</span>
                    </div>
                  {/each}
                </div>
              </div>
              <div class="p-4">
                <h4 class="text-sm font-semibold text-[#6e6e6e]">Top cost API keys</h4>
                <div class="mt-3 space-y-2">
                  {#each (opsMonitor.costBreakdown.topClientKeys ?? []).slice(0, 3) as bucket}
                    <div class="grid grid-cols-[minmax(0,1fr)_auto] gap-2 text-sm">
                      <a class="min-w-0 truncate font-medium text-[#0d0d0d] underline-offset-2 hover:underline" href={dashboardCostClientKeyHref(bucket)}>
                        {bucket.label}
                      </a>
                      <span class="font-mono text-[13px] tabular-nums text-[#6e6e6e]">{formatCostMicrousd(bucket.estimatedCostMicrousd || 0)}</span>
                    </div>
                  {/each}
                </div>
              </div>
            </div>
          </div>
        {/if}
      {/if}
    </article>

    <!-- Recent activity -->
    <article class="rounded-lg border border-[#ededed] bg-white p-6">
      <h2 class="text-2xl font-semibold leading-tight text-[#0d0d0d]">Recent activity</h2>
      <p class="mt-2 text-sm text-[#6e6e6e]">Latest request log snapshot.</p>
      <div class="mt-5 overflow-hidden rounded-lg border border-[#ededed]">
        {#if requestLogs.loading}
          <p class="ui-loading-state p-4 text-sm text-[#6e6e6e]" aria-live="polite">Loading request logs...</p>
        {:else if requestLogs.items.length === 0}
          <p class="p-4 text-sm text-[#6e6e6e]">No gateway requests yet.</p>
        {:else}
          <div class="divide-y divide-[#ededed]">
            {#each requestLogs.items.slice(0, 5) as log}
              <div class="grid gap-1 bg-white p-4 text-sm grid-cols-[minmax(0,1fr)_auto]">
                <a class="font-mono text-[13px] text-[#0d0d0d] underline-offset-2 hover:underline truncate block" href={dashboardLogHref(log)}>
                  {log.route}
                </a>
                <span class="tabular-nums text-[#6e6e6e] shrink-0">{log.statusCode} · {log.latencyMs}ms</span>
              </div>
            {/each}
          </div>
        {/if}
      </div>
    </article>
  </section>
</AuthGate>
