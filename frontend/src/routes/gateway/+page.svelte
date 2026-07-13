<script>
  import {
    cleanupRequestLogs,
    formatCostMicrousd,
    formatDate,
    formatTokens,
    gatewayLimitLabel,
    gatewaySettings,
    getActiveKeys,
    getGatewayReadinessIssues,
    getRoutableModelCount,
    getSchedulableProviderAccounts,
    getUnschedulableProviderAccountSummary,
    unschedulableReasonHref,
    loadGatewaySettings,
    loadKeys,
    loadModelRouting,
    loadProviderAccounts,
    loadUsageSummary,
    modelRouting,
    providerAccounts,
    session,
    updateGatewaySettings,
    usage
  } from '$lib/admin-state.svelte.js';

  import AuthGate from '$lib/AuthGate.svelte';
  let gatewayRequested = $state(false);

  const activeKeys = $derived(getActiveKeys());
  const routableModelCount = $derived(getRoutableModelCount());
  const schedulableAccounts = $derived(getSchedulableProviderAccounts());
  const enabledProviderAccountCount = $derived(providerAccounts.items.filter((account) => account.enabled).length);
  const unschedulableAccountSummary = $derived(getUnschedulableProviderAccountSummary(providerAccounts.items));
  const unschedulableAccountCount = $derived(
    unschedulableAccountSummary.reduce((total, item) => total + item.count, 0)
  );
  const readinessIssues = $derived(
    getGatewayReadinessIssues({
      providerAccounts: providerAccounts.items,
      schedulableAccounts,
      routableModelCount,
      activeKeys
    })
  );
  const usage24hModels = $derived(usage.summaries['24h:model'] ?? null);
  const usage24hProviderAccounts = $derived(usage.summaries['24h:provider_account'] ?? null);
  const usage24hUsageSources = $derived(usage.summaries['24h:usage_source'] ?? null);
  const usage24hRoutingPools = $derived(usage.summaries['24h:routing_pool'] ?? null);
  const usage24hRoutingPoolChains = $derived(usage.summaries['24h:routing_pool_chain'] ?? null);
  const usage24hClientKeys = $derived(usage.summaries['24h:client_key'] ?? null);
  const usage24hSessions = $derived(usage.summaries['24h:session'] ?? null);

  $effect(() => {
    if (!session.authenticated) {
      gatewayRequested = false;
      return;
    }
    if (!gatewayRequested) {
      gatewayRequested = true;
      void loadGatewaySettings();
      void loadProviderAccounts();
      void loadModelRouting();
      void loadKeys();
      void loadUsageSummary('24h', 'model');
      void loadUsageSummary('24h', 'provider_account');
      void loadUsageSummary('24h', 'usage_source');
      void loadUsageSummary('24h', 'routing_pool');
      void loadUsageSummary('24h', 'routing_pool_chain');
      void loadUsageSummary('24h', 'client_key');
      void loadUsageSummary('24h', 'session');
    }
  });

  /**
   * @param {string} title
   * @param {import('$lib/admin-state.svelte.js').UsageSummary | null} summary
   */
  function usageRows(title, summary) {
    return { title, summary };
  }

  /** @param {string | number | null | undefined} id */
  function providerAccountUsageId(id) {
    const value = String(id ?? '');
    const parts = value.split('/');
    const accountId = parts[parts.length - 1] ?? '';
    return /^[1-9]\d*$/.test(accountId) ? accountId : '';
  }

  function gatewayUsageSinceParam() {
    return String(Math.max(0, Math.floor(Date.now() / 1000) - 86400));
  }

  /** @param {URLSearchParams} params */
  function gatewayUsageHrefWithSince(params) {
    params.set('since', gatewayUsageSinceParam());
    return `/request-logs?${params.toString()}`;
  }

  /**
   * @param {string} sectionTitle
   * @param {import('$lib/admin-state.svelte.js').UsageSummaryRow} row
   */
  function usageRowHref(sectionTitle, row) {
    const id = String(row?.id ?? '');
    if (!id || id === 'unknown') return '';
    const params = new URLSearchParams();
    if (sectionTitle === 'Top models') {
      params.set('model', id);
      return gatewayUsageHrefWithSince(params);
    }
    if (sectionTitle === 'Top provider accounts') {
      const accountId = providerAccountUsageId(id);
      if (!accountId) return '';
      params.set('providerAccountId', accountId);
      return gatewayUsageHrefWithSince(params);
    }
    if (sectionTitle === 'Top usage sources') {
      params.set('usageSource', id);
      return gatewayUsageHrefWithSince(params);
    }
    if (sectionTitle === 'Top routing pools' && /^[1-9]\d*$/.test(id)) {
      params.set('routingPoolId', id);
      return gatewayUsageHrefWithSince(params);
    }
    if (sectionTitle === 'Top routing pool chains' && id !== 'none') {
      params.set('routingPoolChain', id);
      return gatewayUsageHrefWithSince(params);
    }
    if (sectionTitle === 'Top client keys' && /^[1-9]\d*$/.test(id)) {
      params.set('clientKeyId', id);
      return gatewayUsageHrefWithSince(params);
    }
    if (sectionTitle === 'Top sessions' && id !== 'none') {
      params.set('sessionId', id);
      return gatewayUsageHrefWithSince(params);
    }
    return '';
  }

  const usageSections = $derived([
    usageRows('Top models', usage24hModels),
    usageRows('Top provider accounts', usage24hProviderAccounts),
    usageRows('Top usage sources', usage24hUsageSources),
    usageRows('Top routing pools', usage24hRoutingPools),
    usageRows('Top routing pool chains', usage24hRoutingPoolChains),
    usageRows('Top client keys', usage24hClientKeys),
    usageRows('Top sessions', usage24hSessions)
  ]);
</script>

<svelte:head>
  <title>N2API Gateway</title>
</svelte:head>

<AuthGate>
  <div class="space-y-6">
    <section class="rounded-lg border border-[#ededed] bg-white p-6">
      <div>
        <h3 class="text-base font-semibold text-[#0d0d0d]">Gateway readiness</h3>
        <p class="mt-1 text-sm text-[#6e6e6e]">Core capacity signals required before this gateway can serve daily traffic reliably.</p>
      </div>
      <dl class="mt-4 grid gap-3 grid-cols-2 sm:grid-cols-4">
        <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
          <dt class="text-sm font-medium text-[#6e6e6e]">Provider accounts</dt>
          <dd class="mt-2">
            <a class="text-base font-semibold text-[#0d0d0d] underline decoration-[#d9d9d9] underline-offset-4 hover:decoration-[#10a37f]" href="/providers?status=all">
              {providerAccounts.loading ? 'Loading' : providerAccounts.items.length}
            </a>
          </dd>
        </div>
        <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
          <dt class="text-sm font-medium text-[#6e6e6e]">Schedulable accounts</dt>
          <dd class="mt-2">
            <a class="text-base font-semibold text-[#0d0d0d] underline decoration-[#d9d9d9] underline-offset-4 hover:decoration-[#10a37f]" href="/providers?status=active">
              {providerAccounts.loading ? 'Loading' : schedulableAccounts.length}
            </a>
          </dd>
        </div>
        <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
          <dt class="text-sm font-medium text-[#6e6e6e]">Routable models</dt>
          <dd class="mt-2">
            <a class="text-base font-semibold text-[#0d0d0d] underline decoration-[#d9d9d9] underline-offset-4 hover:decoration-[#10a37f]" href="/models?status=routable">
              {modelRouting.loading ? 'Loading' : routableModelCount}
            </a>
          </dd>
        </div>
        <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
          <dt class="text-sm font-medium text-[#6e6e6e]">Active API keys</dt>
          <dd class="mt-2">
            <a class="text-base font-semibold text-[#0d0d0d] underline decoration-[#d9d9d9] underline-offset-4 hover:decoration-[#10a37f]" href="/api-keys?status=active">
              {activeKeys.length}
            </a>
          </dd>
        </div>
      </dl>
      {#if readinessIssues.length > 0}
        <div class="mt-4 space-y-2">
          {#each readinessIssues as issue}
            <p class="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800">{issue}</p>
          {/each}
        </div>
      {:else}
        <p class="mt-4 rounded-md border border-[#cbe7dd] bg-[#e8f5f0] px-3 py-2 text-sm text-[#0a7a5e]">
          Gateway has the minimum account, model, and API key prerequisites for routing traffic.
        </p>
      {/if}
    </section>

    <!-- Scheduling health -->
    <section class="rounded-lg border border-[#ededed] bg-white p-6">
      <div>
        <h3 class="text-base font-semibold text-[#0d0d0d]">Scheduling health</h3>
        <p class="mt-1 text-sm text-[#6e6e6e]">Provider account eligibility and local health state used by the gateway scheduler.</p>
      </div>
      <dl class="mt-4 grid gap-3 grid-cols-2 sm:grid-cols-3">
        <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
          <dt class="text-sm font-medium text-[#6e6e6e]">Enabled accounts</dt>
          <dd class="mt-2 text-base font-semibold text-[#0d0d0d]">{providerAccounts.loading ? 'Loading' : enabledProviderAccountCount}</dd>
        </div>
        <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
          <dt class="text-sm font-medium text-[#6e6e6e]">Schedulable accounts</dt>
          <dd class="mt-2">
            <a class="text-base font-semibold text-[#0d0d0d] underline decoration-[#d9d9d9] underline-offset-4 hover:decoration-[#10a37f]" href="/providers?status=active">
              {providerAccounts.loading ? 'Loading' : schedulableAccounts.length}
            </a>
          </dd>
        </div>
        <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
          <dt class="text-sm font-medium text-[#6e6e6e]">Blocked accounts</dt>
          <dd class="mt-2">
            <a class="text-base font-semibold text-[#0d0d0d] underline decoration-[#d9d9d9] underline-offset-4 hover:decoration-[#10a37f]" href="/providers?status=blocked">
              {providerAccounts.loading ? 'Loading' : unschedulableAccountCount}
            </a>
          </dd>
        </div>
      </dl>
      <div class="mt-4 rounded-md border border-[#ededed] bg-[#fafafa] p-3">
        <h4 class="text-sm font-semibold text-[#0d0d0d]">Blocked reasons</h4>
        {#if providerAccounts.loading}
          <p class="ui-loading-state mt-2 text-sm text-[#6e6e6e]" aria-live="polite">Loading provider account health...</p>
        {:else if unschedulableAccountSummary.length === 0}
          <p class="mt-2 text-sm text-[#6e6e6e]">No blocked provider accounts.</p>
        {:else}
          <div class="mt-3 flex flex-wrap gap-2">
            {#each unschedulableAccountSummary as item}
              <a
                class="ui-button ui-button--sm ui-button--secondary rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-sm text-[#3c3c3c] underline-offset-2 hover:underline"
                href={unschedulableReasonHref(item.reason)}
                aria-label="View provider accounts with this blocked reason"
              >
                {item.reasonLabel}: <span class="font-mono text-[#0d0d0d]">{item.count}</span>
              </a>
            {/each}
          </div>
        {/if}
      </div>
    </section>

    <!-- Runtime limits -->
    <section class="rounded-lg border border-[#ededed] bg-white p-6">
      <div class="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h3 class="text-base font-semibold text-[#0d0d0d]">Runtime limits</h3>
          <p class="mt-1 text-sm text-[#6e6e6e]">Current concurrency and rate guards from the running service.</p>
        </div>
        {#if gatewaySettings.loading}
          <span class="ui-loading-state text-sm text-[#6e6e6e]" aria-live="polite">Loading...</span>
        {/if}
      </div>
      {#if gatewaySettings.error}
        <p class="mt-3 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">
          {gatewaySettings.error}
        </p>
      {:else if gatewaySettings.loading || !gatewaySettings.data}
        <p class="ui-loading-state mt-4 text-sm text-[#6e6e6e]" aria-live="polite">Loading gateway runtime limits...</p>
      {:else}
        <form class="mt-4" onsubmit={(event) => { event.preventDefault(); updateGatewaySettings(); }}>
          <dl class="grid gap-3 grid-cols-2 sm:grid-cols-3 xl:grid-cols-6">
            <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
              <dt class="text-xs font-medium text-[#6e6e6e]">Gateway concurrency</dt>
              <dd class="mt-2">
                <input class="w-full rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" type="number" min="0" bind:value={gatewaySettings.data.maxConcurrentGatewayRequests} />
                <span class="mt-1 block text-xs text-[#6e6e6e]">{gatewayLimitLabel(gatewaySettings.data.maxConcurrentGatewayRequests)}</span>
              </dd>
            </div>
            <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
              <dt class="text-xs font-medium text-[#6e6e6e]">Per account concurrency</dt>
              <dd class="mt-2">
                <input class="w-full rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" type="number" min="0" bind:value={gatewaySettings.data.maxConcurrentRequestsPerAccount} />
                <span class="mt-1 block text-xs text-[#6e6e6e]">{gatewayLimitLabel(gatewaySettings.data.maxConcurrentRequestsPerAccount)}</span>
              </dd>
            </div>
            <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
              <dt class="text-xs font-medium text-[#6e6e6e]">Per key concurrency</dt>
              <dd class="mt-2">
                <input class="w-full rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" type="number" min="0" bind:value={gatewaySettings.data.maxConcurrentRequestsPerKey} />
                <span class="mt-1 block text-xs text-[#6e6e6e]">{gatewayLimitLabel(gatewaySettings.data.maxConcurrentRequestsPerKey)}</span>
              </dd>
            </div>
            <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
              <dt class="text-xs font-medium text-[#6e6e6e]">Requests per minute</dt>
              <dd class="mt-2">
                <input class="w-full rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" type="number" min="0" bind:value={gatewaySettings.data.requestsPerMinutePerKey} />
                <span class="mt-1 block text-xs text-[#6e6e6e]">{gatewayLimitLabel(gatewaySettings.data.requestsPerMinutePerKey)}</span>
              </dd>
            </div>
            <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
              <dt class="text-xs font-medium text-[#6e6e6e]">Tokens per minute</dt>
              <dd class="mt-2">
                <input class="w-full rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" type="number" min="0" bind:value={gatewaySettings.data.tokensPerMinutePerKey} />
                <span class="mt-1 block text-xs text-[#6e6e6e]">{gatewayLimitLabel(gatewaySettings.data.tokensPerMinutePerKey)}</span>
              </dd>
            </div>
            <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
              <dt class="text-xs font-medium text-[#6e6e6e]">Request log retention</dt>
              <dd class="mt-2">
                <input class="w-full rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" type="number" min="0" bind:value={gatewaySettings.data.requestLogRetentionDays} />
                <span class="mt-1 block text-xs text-[#6e6e6e]">{gatewaySettings.data.requestLogRetentionDays > 0 ? `${gatewaySettings.data.requestLogRetentionDays} days` : 'Disabled'}</span>
              </dd>
            </div>
          </dl>
          <div class="mt-5 rounded-md border border-[#ededed] bg-[#fafafa] p-4">
            <div class="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
              <div>
                <h4 class="text-sm font-semibold text-[#0d0d0d]">Provider account auto tests</h4>
                <p class="mt-1 max-w-2xl text-sm text-[#6e6e6e]">Run the same probe as Test all accounts on a backend schedule.</p>
                <label class="mt-4 flex items-center gap-2 text-sm font-medium text-[#3c3c3c]">
                  <input class="h-4 w-4 rounded border-[#d9d9d9] text-[#10a37f] focus:ring-[#10a37f]" type="checkbox" bind:checked={gatewaySettings.data.providerAccountAutoTestEnabled} />
                  Enable auto tests
                </label>
              </div>
              <label class="block w-full text-sm font-medium text-[#3c3c3c] lg:w-56">
                Interval seconds
                <input class="mt-2 w-full rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" type="number" min="0" bind:value={gatewaySettings.data.providerAccountAutoTestIntervalSeconds} />
              </label>
            </div>
            <div class="mt-4 grid gap-3 grid-cols-2 sm:grid-cols-4">
              <div class="rounded-md border border-[#ededed] bg-white p-3">
                <p class="text-xs font-medium text-[#6e6e6e]">Auto-test status</p>
                <p class="mt-2 text-sm font-semibold text-[#0d0d0d]">{gatewaySettings.data.providerAccountAutoTestStatus.running ? 'Running' : 'Idle'}</p>
              </div>
              <div class="rounded-md border border-[#ededed] bg-white p-3">
                <p class="text-xs font-medium text-[#6e6e6e]">Last finished</p>
                <p class="mt-2 text-sm font-semibold text-[#0d0d0d]">{gatewaySettings.data.providerAccountAutoTestStatus.lastFinishedAt ? formatDate(gatewaySettings.data.providerAccountAutoTestStatus.lastFinishedAt) : 'Not run yet'}</p>
              </div>
              <div class="rounded-md border border-[#ededed] bg-white p-3">
                <p class="text-xs font-medium text-[#6e6e6e]">Accounts tested</p>
                <p class="mt-2 font-mono text-sm font-semibold text-[#0d0d0d]">{gatewaySettings.data.providerAccountAutoTestStatus.lastAccountCount}</p>
              </div>
              <div class="rounded-md border border-[#ededed] bg-white p-3">
                <p class="text-xs font-medium text-[#6e6e6e]">Last error</p>
                <p class={gatewaySettings.data.providerAccountAutoTestStatus.lastError ? 'mt-2 min-w-0 break-words text-sm font-semibold text-red-700' : 'mt-2 text-sm font-semibold text-[#0d0d0d]'}>
                  {gatewaySettings.data.providerAccountAutoTestStatus.lastError || 'None'}
                </p>
              </div>
            </div>
          </div>
          <div class="mt-4 flex flex-wrap items-center gap-3">
            <button class="ui-button ui-button--sm ui-button--primary rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white disabled:cursor-not-allowed disabled:opacity-60" disabled={gatewaySettings.saving}>
              {gatewaySettings.saving ? 'Saving' : 'Save runtime limits'}
            </button>
            {#if gatewaySettings.saved}
              <span class="text-sm text-[#0a7a5e]">Runtime limits saved.</span>
            {/if}
            <button type="button" class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#d9d9d9] bg-white px-4 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:opacity-60" disabled={gatewaySettings.saving || gatewaySettings.cleanupRunning || gatewaySettings.data.requestLogRetentionDays <= 0} onclick={cleanupRequestLogs}>
              {gatewaySettings.cleanupRunning ? 'Cleaning' : 'Clean request logs'}
            </button>
            {#if gatewaySettings.cleanupResult}
              <span class="text-sm text-[#0a7a5e]">
                Removed {gatewaySettings.cleanupResult.deleted} logs before {formatDate(gatewaySettings.cleanupResult.before)}.
              </span>
            {/if}
          </div>
        </form>
      {/if}
    </section>

    <!-- 24h usage -->
    <section class="rounded-lg border border-[#ededed] bg-white p-6">
      <div class="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h3 class="text-base font-semibold text-[#0d0d0d]">24h usage</h3>
          <p class="mt-1 text-sm text-[#6e6e6e]">Traffic distribution across accounts, client keys, and sticky sessions.</p>
        </div>
        {#if usage.loading}
          <span class="ui-loading-state text-sm text-[#6e6e6e]" aria-live="polite">Loading...</span>
        {/if}
      </div>
      {#if usage.error}
        <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">{usage.error}</p>
      {/if}
      <div class="mt-5 grid gap-5 sm:grid-cols-2 xl:grid-cols-3">
        {#each usageSections as section}
          <div class="rounded-lg border border-[#ededed]">
            <div class="border-b border-[#ededed] bg-[#f5f5f5] px-4 py-3">
              <h4 class="text-sm font-semibold text-[#0d0d0d]">{section.title}</h4>
              <p class="mt-1 text-xs text-[#6e6e6e]">
                {formatTokens(section.summary?.totalRequests)} requests · {formatCostMicrousd(section.summary?.estimatedCostMicrousd)}
              </p>
            </div>
            {#if !section.summary?.rows?.length}
              <p class="px-4 py-4 text-sm text-[#6e6e6e]">No usage in this range.</p>
            {:else}
              <div class="divide-y divide-[#ededed]">
                {#each section.summary.rows.slice(0, 5) as row}
                  {@const href = usageRowHref(section.title, row)}
                  <div class="grid gap-2 px-4 py-3 text-sm grid-cols-[minmax(0,1fr)_auto]">
                    {#if href}
                      <a class="min-w-0 truncate font-medium text-[#0d0d0d] underline decoration-[#d9d9d9] underline-offset-4 hover:decoration-[#10a37f]" href={href}>{row.label || row.id}</a>
                    {:else}
                      <span class="min-w-0 truncate font-medium text-[#0d0d0d]">{row.label || row.id}</span>
                    {/if}
                    <span class="font-mono text-[13px] tabular-nums text-[#6e6e6e] whitespace-nowrap text-right">
                      {formatTokens(row.requests)} req · {formatTokens(row.totalTokens)} tokens · {formatCostMicrousd(row.estimatedCostMicrousd)}
                    </span>
                  </div>
                {/each}
              </div>
            {/if}
          </div>
        {/each}
      </div>
    </section>
  </div>
</AuthGate>
