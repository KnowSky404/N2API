<script>
  import {
    formatCostMicrousd,
    formatDate,
    formatTokens,
    opsMonitor,
    loadOpsDashboard,
    session,
  } from '$lib/admin-state.svelte.js';

  import AuthGate from '$lib/AuthGate.svelte';
  let range = $state('24h');
  const maxLatCount = $derived(opsMonitor.latency?.buckets?.length ? Math.max(...opsMonitor.latency.buckets.map((/** @type {{count: number}} */ b) => b.count), 1) : 0);

  let requested = $state(false);

  const rangeOptions = [
    { value: '1h', label: '1h', seconds: 3600 },
    { value: '6h', label: '6h', seconds: 21600 },
    { value: '24h', label: '24h', seconds: 86400 },
    { value: '7d', label: '7d', seconds: 604800 },
  ];

  /** @param {string} value */
  function changeRange(value) {
    range = value;
    const option = rangeOptions.find((r) => r.value === value);
    if (option) {
      loadOpsDashboard(option.seconds);
    }
  }

  $effect(() => {
    if (!session.authenticated || requested) return;
    requested = true;
    const option = rangeOptions.find((r) => r.value === range);
    loadOpsDashboard(option ? option.seconds : 86400);
  });

  // SVG bar chart helper
  /** @param {number} count @param {number} max */
  function barWidth(count, max) {
    if (!max || max <= 0) return 0;
    return Math.min(100, (count / max) * 100);
  }

  function opsSinceParam() {
    const option = rangeOptions.find((r) => r.value === range);
    const seconds = option ? option.seconds : 86400;
    return String(Math.max(0, Math.floor(Date.now() / 1000) - seconds));
  }

  /** @param {URLSearchParams} params */
  function requestLogHrefWithSince(params) {
    params.set('since', opsSinceParam());
    return `/request-logs?${params.toString()}`;
  }

  /** @param {{ key?: string | number | null }} bucket */
  function opsErrorHref(bucket) {
    const params = new URLSearchParams();
    const key = String(bucket?.key ?? '').trim();
    if (key) params.set('error', key);
    return requestLogHrefWithSince(params);
  }

  /** @param {{ key?: string | number | null }} bucket */
  function opsStatusCodeHref(bucket) {
    const params = new URLSearchParams();
    const key = String(bucket?.key ?? '').trim();
    if (/^[1-5]\d\d$/.test(key)) params.set('statusCode', key);
    return requestLogHrefWithSince(params);
  }

  /** @param {{ key?: string | number | null }} bucket */
  function opsRateLimitedModelHref(bucket) {
    const params = new URLSearchParams({ statusCode: '429' });
    const key = String(bucket?.key ?? '').trim();
    if (key && key !== 'unknown') params.set('model', key);
    return requestLogHrefWithSince(params);
  }

  /** @param {{ key?: string | number | null }} bucket */
  function opsErrorAccountHref(bucket) {
    const params = new URLSearchParams();
    const key = String(bucket?.key ?? '').trim();
    if (!key || key === 'unknown') {
      params.set('statusClass', 'server_error');
    } else {
      params.set('providerAccountId', key);
    }
    return requestLogHrefWithSince(params);
  }

  /** @param {{ key?: string | number | null }} bucket */
  function opsCostModelHref(bucket) {
    const params = new URLSearchParams();
    const key = String(bucket?.key ?? '').trim();
    if (key && key !== 'unknown') params.set('model', key);
    return requestLogHrefWithSince(params);
  }

  /** @param {{ key?: string | number | null }} bucket */
  function opsCostProviderAccountHref(bucket) {
    const params = new URLSearchParams();
    const key = String(bucket?.key ?? '').trim();
    if (key && key !== 'unknown') params.set('providerAccountId', key);
    return requestLogHrefWithSince(params);
  }

  /** @param {{ key?: string | number | null }} bucket */
  function opsCostClientKeyHref(bucket) {
    const params = new URLSearchParams();
    const key = String(bucket?.key ?? '').trim();
    if (key && key !== 'unknown') params.set('clientKeyId', key);
    return requestLogHrefWithSince(params);
  }

  /** @param {string | null | undefined} status */
  function accountTestStatusClass(status) {
    if (status === 'passed') return 'text-[#0a7a5e]';
    if (status === 'failed') return 'text-red-700';
    return 'text-[#6e6e6e]';
  }
</script>

<svelte:head>
  <title>N2API Ops Monitor</title>
</svelte:head>

<AuthGate>
  <div class="ui-page">
    <header class="ui-page-header">
      <div class="ui-page-heading">
        <h1 class="ui-page-title">Operations monitor</h1>
        <p class="ui-page-description">Gateway error rates, throughput trends, latency distribution, and health signals.</p>
      </div>
      <div class="ui-page-actions">
        <div class="flex items-center gap-1 rounded-lg border border-[#e5e5e5] bg-white p-0.5">
          {#each rangeOptions as option}
            <button
              class="ui-button ui-button--md rounded-md px-3 py-1.5 text-sm font-medium transition-colors {range === option.value ? 'bg-[#0d0d0d] text-white' : 'text-[#3c3c3c] hover:bg-[#f5f5f5]'}"
              onclick={() => changeRange(option.value)}
            >
              {option.label}
            </button>
          {/each}
        </div>
      </div>
    </header>

    {#if opsMonitor.loading && !opsMonitor.stats}
      <section class="rounded-lg border border-[#ededed] bg-white p-6 text-sm text-[#6e6e6e]">
        Loading ops dashboard...
      </section>
    {:else if opsMonitor.error && !opsMonitor.stats}
      <section class="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700">
        {opsMonitor.error}
      </section>
    {:else}
      <!-- Error summary cards -->
      {#if opsMonitor.stats}
        <section class="grid gap-4 grid-cols-2 sm:grid-cols-4">
          <article class="rounded-lg border border-[#ededed] bg-white p-5">
            <p class="text-sm font-medium text-[#6e6e6e]">Total requests</p>
            <p class="mt-2 text-lg font-semibold tabular-nums text-[#0d0d0d]">{formatTokens(opsMonitor.stats.totalRequests)}</p>
          </article>
          <article class="rounded-lg border border-[#ededed] bg-white p-5">
            <p class="text-sm font-medium text-[#6e6e6e]">Error rate</p>
            <p class="mt-2 text-lg font-semibold tabular-nums {opsMonitor.stats.errorRate > 0.1 ? 'text-red-700' : opsMonitor.stats.errorRate > 0.02 ? 'text-amber-700' : 'text-[#0a7a5e]'}">
              {(opsMonitor.stats.errorRate * 100).toFixed(1)}%
            </p>
          </article>
          <article class="rounded-lg border border-[#ededed] bg-white p-5">
            <p class="text-sm font-medium text-[#6e6e6e]">Client errors (4xx)</p>
            <p class="mt-2 text-lg font-semibold tabular-nums text-[#0d0d0d]">{formatTokens(opsMonitor.stats.clientErrors)}</p>
          </article>
          <article class="rounded-lg border border-[#ededed] bg-white p-5">
            <p class="text-sm font-medium text-[#6e6e6e]">Server errors (5xx)</p>
            <p class="mt-2 text-lg font-semibold tabular-nums text-[#0d0d0d]">{formatTokens(opsMonitor.stats.serverErrors)}</p>
          </article>
        </section>

        <!-- Error dist + rl cards -->
        <div class="grid gap-4 sm:grid-cols-2">
          <section class="rounded-lg border border-[#ededed] bg-white p-5">
            <p class="text-sm font-medium text-[#6e6e6e]">Rate limited</p>
            <p class="mt-2 text-lg font-semibold tabular-nums text-[#0d0d0d]">{formatTokens(opsMonitor.stats.rateLimitErrors)}</p>
          </section>
          <section class="rounded-lg border border-[#ededed] bg-white p-5">
            <p class="text-sm font-medium text-[#6e6e6e]">Upstream errors</p>
            <p class="mt-2 text-lg font-semibold tabular-nums text-[#0d0d0d]">{formatTokens(opsMonitor.stats.upstreamErrors)}</p>
          </section>
        </div>

        <!-- Top error breakdown -->
        <section class="grid gap-6 lg:grid-cols-2">
          {#if opsMonitor.stats.topErrors.length > 0}
            <div class="rounded-lg border border-[#ededed] bg-white p-5">
              <h3 class="text-base font-semibold text-[#0d0d0d]">Top errors</h3>
              <div class="mt-3 space-y-2">
                {#each opsMonitor.stats.topErrors as bucket}
                  <div class="flex items-center gap-2">
                    <a
                      class="min-w-0 flex-1 truncate text-sm font-medium text-[#0d0d0d] underline-offset-2 hover:underline"
                      href={opsErrorHref(bucket)}
                      title="View matching request logs"
                    >
                      {bucket.label}
                    </a>
                    <span class="font-mono text-[13px] tabular-nums text-[#6e6e6e] shrink-0">{formatTokens(bucket.count)}</span>
                  </div>
                {/each}
              </div>
            </div>
          {/if}
          {#if opsMonitor.stats.topUpstreamStatuses.length > 0}
            <div class="rounded-lg border border-[#ededed] bg-white p-5">
              <h3 class="text-base font-semibold text-[#0d0d0d]">Upstream status codes</h3>
              <div class="mt-3 space-y-2">
                {#each opsMonitor.stats.topUpstreamStatuses as bucket}
                  <div class="flex items-center gap-2">
                    <a
                      class="font-mono text-[13px] font-medium text-[#0d0d0d] underline-offset-2 hover:underline"
                      href={opsStatusCodeHref(bucket)}
                      title="View matching request logs"
                    >
                      {bucket.key}
                    </a>
                    <span class="font-mono text-[13px] tabular-nums text-[#6e6e6e]">{formatTokens(bucket.count)}</span>
                  </div>
                {/each}
              </div>
            </div>
          {/if}
          {#if opsMonitor.stats.topRateLimitedModels.length > 0}
            <div class="rounded-lg border border-[#ededed] bg-white p-5">
              <h3 class="text-base font-semibold text-[#0d0d0d]">Rate-limited models</h3>
              <div class="mt-3 space-y-2">
                {#each opsMonitor.stats.topRateLimitedModels as bucket}
                  <div class="flex items-center gap-2">
                    <a
                      class="min-w-0 flex-1 truncate font-mono text-[13px] font-medium text-[#0d0d0d] underline-offset-2 hover:underline"
                      href={opsRateLimitedModelHref(bucket)}
                      title="View matching request logs"
                    >
                      {bucket.label}
                    </a>
                    <span class="font-mono text-[13px] tabular-nums text-[#6e6e6e] shrink-0">{formatTokens(bucket.count)}</span>
                  </div>
                {/each}
              </div>
            </div>
          {/if}
          {#if opsMonitor.stats.topErrorAccounts.length > 0}
            <div class="rounded-lg border border-[#ededed] bg-white p-5">
              <h3 class="text-base font-semibold text-[#0d0d0d]">Error accounts</h3>
              <div class="mt-3 space-y-2">
                {#each opsMonitor.stats.topErrorAccounts as bucket}
                  <div class="flex items-center gap-2">
                    <a
                      class="min-w-0 flex-1 truncate text-sm font-medium text-[#0d0d0d] underline-offset-2 hover:underline"
                      href={opsErrorAccountHref(bucket)}
                      title="View matching request logs"
                    >
                      {bucket.label}
                    </a>
                    <span class="font-mono text-[13px] tabular-nums text-[#6e6e6e] shrink-0">{formatTokens(bucket.count)}</span>
                  </div>
                {/each}
              </div>
            </div>
          {/if}
        </section>
      {/if}

      {#if opsMonitor.costBreakdown}
        <section class="rounded-lg border border-[#ededed] bg-white p-6">
          <div class="flex flex-wrap items-start justify-between gap-3">
            <div>
              <h3 class="text-base font-semibold text-[#0d0d0d]">Cost attribution</h3>
              <p class="mt-1 text-sm text-[#6e6e6e]">Estimated request cost by model, provider account, and API key.</p>
            </div>
            <p class="font-mono text-sm tabular-nums text-[#0d0d0d]">{formatCostMicrousd(opsMonitor.costBreakdown.estimatedCostMicrousd || 0)}</p>
          </div>

          <div class="mt-5 grid gap-4 lg:grid-cols-3">
            <div class="rounded-lg border border-[#ededed] bg-white p-4">
              <h4 class="text-sm font-semibold text-[#0d0d0d]">Top cost models</h4>
              <div class="mt-3 space-y-2">
                {#each opsMonitor.costBreakdown.topModels ?? [] as bucket}
                  <div class="flex items-center gap-2">
                    <a
                      class="min-w-0 flex-1 truncate font-mono text-[13px] font-medium text-[#0d0d0d] underline-offset-2 hover:underline"
                      href={opsCostModelHref(bucket)}
                      title="View matching request logs"
                    >
                      {bucket.label}
                    </a>
                    <span class="shrink-0 font-mono text-[13px] tabular-nums text-[#0d0d0d]">{formatCostMicrousd(bucket.estimatedCostMicrousd || 0)}</span>
                    <span class="shrink-0 font-mono text-[12px] tabular-nums text-[#6e6e6e]">{formatTokens(bucket.requests)}</span>
                  </div>
                {/each}
              </div>
            </div>

            <div class="rounded-lg border border-[#ededed] bg-white p-4">
              <h4 class="text-sm font-semibold text-[#0d0d0d]">Top cost provider accounts</h4>
              <div class="mt-3 space-y-2">
                {#each opsMonitor.costBreakdown.topProviderAccounts ?? [] as bucket}
                  <div class="flex items-center gap-2">
                    <a
                      class="min-w-0 flex-1 truncate text-sm font-medium text-[#0d0d0d] underline-offset-2 hover:underline"
                      href={opsCostProviderAccountHref(bucket)}
                      title="View matching request logs"
                    >
                      {bucket.label}
                    </a>
                    <span class="shrink-0 font-mono text-[13px] tabular-nums text-[#0d0d0d]">{formatCostMicrousd(bucket.estimatedCostMicrousd || 0)}</span>
                    <span class="shrink-0 font-mono text-[12px] tabular-nums text-[#6e6e6e]">{formatTokens(bucket.requests)}</span>
                  </div>
                {/each}
              </div>
            </div>

            <div class="rounded-lg border border-[#ededed] bg-white p-4">
              <h4 class="text-sm font-semibold text-[#0d0d0d]">Top cost API keys</h4>
              <div class="mt-3 space-y-2">
                {#each opsMonitor.costBreakdown.topClientKeys ?? [] as bucket}
                  <div class="flex items-center gap-2">
                    <a
                      class="min-w-0 flex-1 truncate text-sm font-medium text-[#0d0d0d] underline-offset-2 hover:underline"
                      href={opsCostClientKeyHref(bucket)}
                      title="View matching request logs"
                    >
                      {bucket.label}
                    </a>
                    <span class="shrink-0 font-mono text-[13px] tabular-nums text-[#0d0d0d]">{formatCostMicrousd(bucket.estimatedCostMicrousd || 0)}</span>
                    <span class="shrink-0 font-mono text-[12px] tabular-nums text-[#6e6e6e]">{formatTokens(bucket.requests)}</span>
                  </div>
                {/each}
              </div>
            </div>
          </div>
        </section>
      {/if}

      {#if opsMonitor.accountHealth}
        <section class="rounded-lg border border-[#ededed] bg-white p-6">
          <div class="flex flex-wrap items-start justify-between gap-3">
            <div>
              <h3 class="text-base font-semibold text-[#0d0d0d]">Account health</h3>
              <p class="mt-1 text-sm text-[#6e6e6e]">Provider account scheduling and test health in the selected window.</p>
            </div>
            <a class="text-sm font-medium text-[#0d0d0d] underline-offset-2 hover:underline" href="/providers">Open providers</a>
          </div>

          <div class="mt-5 grid gap-4 grid-cols-2 sm:grid-cols-4">
            <article class="rounded-lg border border-[#ededed] bg-white p-4">
              <p class="text-sm font-medium text-[#6e6e6e]">Total accounts</p>
              <p class="mt-2 text-lg font-semibold tabular-nums text-[#0d0d0d]">{formatTokens(opsMonitor.accountHealth.totalAccounts)}</p>
            </article>
            <article class="rounded-lg border border-[#ededed] bg-white p-4">
              <p class="text-sm font-medium text-[#6e6e6e]">Schedulable accounts</p>
              <p class="mt-2 text-lg font-semibold tabular-nums {opsMonitor.accountHealth.schedulable > 0 ? 'text-[#0a7a5e]' : 'text-red-700'}">{formatTokens(opsMonitor.accountHealth.schedulable)}</p>
            </article>
            <article class="rounded-lg border border-[#ededed] bg-white p-4">
              <p class="text-sm font-medium text-[#6e6e6e]">Enabled accounts</p>
              <p class="mt-2 text-lg font-semibold tabular-nums text-[#0d0d0d]">{formatTokens(opsMonitor.accountHealth.enabledAccounts)}</p>
            </article>
            <article class="rounded-lg border border-[#ededed] bg-white p-4">
              <p class="text-sm font-medium text-[#6e6e6e]">Recent test failures</p>
              <p class="mt-2 text-lg font-semibold tabular-nums {opsMonitor.accountHealth.recentTestFailure > 0 ? 'text-red-700' : 'text-[#0a7a5e]'}">{formatTokens(opsMonitor.accountHealth.recentTestFailure)}</p>
            </article>
          </div>

          <div class="mt-4 grid gap-4 lg:grid-cols-2">
            <div class="rounded-lg border border-[#ededed] bg-white p-4">
              <h4 class="text-sm font-semibold text-[#0d0d0d]">Scheduling blockers</h4>
              <dl class="mt-3 grid grid-cols-2 gap-3 text-sm">
                <div>
                  <dt class="text-[#6e6e6e]">Disabled</dt>
                  <dd class="mt-1">
                    <a class="font-mono tabular-nums text-[#0d0d0d] underline-offset-2 hover:underline" href="/providers?status=disabled">
                      {formatTokens(opsMonitor.accountHealth.disabled)}
                    </a>
                  </dd>
                </div>
                <div>
                  <dt class="text-[#6e6e6e]">Rate limited</dt>
                  <dd class="mt-1">
                    <a class="font-mono tabular-nums text-[#0d0d0d] underline-offset-2 hover:underline" href="/providers?status=rate_limited">
                      {formatTokens(opsMonitor.accountHealth.rateLimited)}
                    </a>
                  </dd>
                </div>
                <div>
                  <dt class="text-[#6e6e6e]">Circuit open</dt>
                  <dd class="mt-1">
                    <a class="font-mono tabular-nums text-[#0d0d0d] underline-offset-2 hover:underline" href="/providers?status=circuit_open">
                      {formatTokens(opsMonitor.accountHealth.circuitOpen)}
                    </a>
                  </dd>
                </div>
                <div>
                  <dt class="text-[#6e6e6e]">Expired</dt>
                  <dd class="mt-1">
                    <a class="font-mono tabular-nums text-[#0d0d0d] underline-offset-2 hover:underline" href="/providers?status=expired">
                      {formatTokens(opsMonitor.accountHealth.expired)}
                    </a>
                  </dd>
                </div>
              </dl>
            </div>

            <div class="rounded-lg border border-[#ededed] bg-white p-4">
              <h4 class="text-sm font-semibold text-[#0d0d0d]">Account tests</h4>
              <dl class="mt-3 grid grid-cols-2 gap-3 text-sm">
                <div>
                  <dt class="text-[#6e6e6e]">Tested</dt>
                  <dd class="mt-1 font-mono tabular-nums text-[#0d0d0d]">{formatTokens(opsMonitor.accountHealth.testedAccounts)}</dd>
                </div>
                <div>
                  <dt class="text-[#6e6e6e]">Missing tests</dt>
                  <dd class="mt-1 font-mono tabular-nums text-[#0d0d0d]">{formatTokens(opsMonitor.accountHealth.testMissing)}</dd>
                </div>
                <div>
                  <dt class="text-[#6e6e6e]">Passed</dt>
                  <dd class="mt-1 font-mono tabular-nums text-[#0a7a5e]">{formatTokens(opsMonitor.accountHealth.testPassed)}</dd>
                </div>
                <div>
                  <dt class="text-[#6e6e6e]">Failed</dt>
                  <dd class="mt-1 font-mono tabular-nums {opsMonitor.accountHealth.testFailed > 0 ? 'text-red-700' : 'text-[#0d0d0d]'}">{formatTokens(opsMonitor.accountHealth.testFailed)}</dd>
                </div>
              </dl>
            </div>
          </div>
        </section>
      {/if}

      {#if opsMonitor.accountTests}
        <section class="rounded-lg border border-[#ededed] bg-white p-6">
          <div class="flex flex-wrap items-start justify-between gap-3">
            <div>
              <h3 class="text-base font-semibold text-[#0d0d0d]">Recent account tests</h3>
              <p class="mt-1 text-sm text-[#6e6e6e]">Latest provider account test results in the selected window.</p>
            </div>
            <a class="text-sm font-medium text-[#0d0d0d] underline-offset-2 hover:underline" href="/providers">Open providers</a>
          </div>

          {#if opsMonitor.accountTests.tests?.length > 0}
            <div class="ui-table-shell mt-4 overflow-x-auto">
              <table class="ui-table w-full text-sm">
                <thead>
                  <tr class="border-b border-[#ededed] text-left text-xs font-medium text-[#6e6e6e]">
                    <th class="py-2 pr-4">Checked</th>
                    <th class="py-2 pr-4">Account</th>
                    <th class="py-2 pr-4">Type</th>
                    <th class="py-2 pr-4">Status</th>
                    <th class="py-2">Message</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-[#ededed]">
                  {#each opsMonitor.accountTests.tests as test}
                    <tr class="hover:bg-[#fafafa]">
                      <td class="py-2 pr-4 whitespace-nowrap font-mono text-[13px] text-[#3c3c3c]">{formatDate(test.checkedAt)}</td>
                      <td class="py-2 pr-4">
                        <a
                          class="font-medium text-[#0d0d0d] underline-offset-2 hover:underline"
                          href={`/providers?providerAccountId=${test.accountId}`}
                          title="Open provider account"
                        >
                          {test.accountName || `Account ${test.accountId}`}
                        </a>
                        <div class="mt-0.5 font-mono text-[12px] text-[#6e6e6e]">{test.provider}</div>
                      </td>
                      <td class="py-2 pr-4 font-mono text-[13px] text-[#3c3c3c]">{test.accountType}</td>
                      <td class="py-2 pr-4 font-mono text-[13px] font-semibold {accountTestStatusClass(test.status)}">{test.status}</td>
                      <td class="py-2 text-[#3c3c3c]">{test.message || '-'}</td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
          {:else}
            <p class="mt-4 rounded-lg border border-[#ededed] bg-[#fafafa] p-4 text-sm text-[#6e6e6e]">No account tests in this window.</p>
          {/if}
        </section>
      {/if}

      <!-- Throughput trend -->
      {#if opsMonitor.throughput?.points?.length > 0}
        <section class="rounded-lg border border-[#ededed] bg-white p-6">
          <h3 class="text-base font-semibold text-[#0d0d0d]">Request throughput</h3>
          <p class="mt-1 text-sm text-[#6e6e6e]">Requests, tokens, and estimated cost per time bucket.</p>
          <div class="ui-table-shell mt-4 overflow-x-auto">
            <table class="ui-table w-full text-sm">
              <thead>
                <tr class="border-b border-[#ededed] text-left text-xs font-medium text-[#6e6e6e]">
                  <th class="py-2 pr-4">Time</th>
                  <th class="py-2 pr-4 text-right">Requests</th>
                  <th class="py-2 pr-4 text-right">Tokens</th>
                  <th class="py-2 pr-4 text-right">Cost</th>
                  <th class="py-2 pr-4 text-right">Errors</th>
                  <th class="py-2 text-right">Avg latency</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-[#ededed]">
                {#each opsMonitor.throughput.points.slice(-48) as point}
                  <tr class="hover:bg-[#fafafa]">
                    <td class="py-2 pr-4 font-mono text-[13px] text-[#3c3c3c]">{formatDate(point.time)}</td>
                    <td class="py-2 pr-4 text-right font-mono text-[13px] tabular-nums text-[#0d0d0d]">{formatTokens(point.requests)}</td>
                    <td class="py-2 pr-4 text-right font-mono text-[13px] tabular-nums text-[#0d0d0d]">{formatTokens(point.totalTokens)}</td>
                    <td class="py-2 pr-4 text-right font-mono text-[13px] tabular-nums text-[#0d0d0d]">{formatCostMicrousd(point.costMicrousd)}</td>
                    <td class="py-2 pr-4 text-right font-mono text-[13px] tabular-nums {point.errorCount > 0 ? 'text-red-700' : 'text-[#6e6e6e]'}">{point.errorCount}</td>
                    <td class="py-2 text-right font-mono text-[13px] tabular-nums text-[#6e6e6e]">{point.avgLatencyMs.toFixed(0)}ms</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        </section>
      {/if}

      <!-- Error trend -->
      {#if opsMonitor.errorTrend?.points?.length > 0}
        <section class="rounded-lg border border-[#ededed] bg-white p-6">
          <h3 class="text-base font-semibold text-[#0d0d0d]">Error breakdown over time</h3>
          <p class="mt-1 text-sm text-[#6e6e6e]">Error counts by category per time bucket.</p>
          <div class="ui-table-shell mt-4 overflow-x-auto">
            <table class="ui-table w-full text-sm">
              <thead>
                <tr class="border-b border-[#ededed] text-left text-xs font-medium text-[#6e6e6e]">
                  <th class="py-2 pr-4">Time</th>
                  <th class="py-2 pr-4 text-right">Total</th>
                  <th class="py-2 pr-4 text-right">4xx</th>
                  <th class="py-2 pr-4 text-right">5xx</th>
                  <th class="py-2 pr-4 text-right">429</th>
                  <th class="py-2 pr-4 text-right">Upstream</th>
                  <th class="py-2 text-right">Gateway</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-[#ededed]">
                {#each opsMonitor.errorTrend.points.slice(-48) as point}
                  <tr class="hover:bg-[#fafafa]">
                    <td class="py-2 pr-4 font-mono text-[13px] text-[#3c3c3c]">{formatDate(point.time)}</td>
                    <td class="py-2 pr-4 text-right font-mono text-[13px] tabular-nums text-[#0d0d0d]">{point.total}</td>
                    <td class="py-2 pr-4 text-right font-mono text-[13px] tabular-nums text-amber-700">{point.clientErrors}</td>
                    <td class="py-2 pr-4 text-right font-mono text-[13px] tabular-nums text-red-700">{point.serverErrors}</td>
                    <td class="py-2 pr-4 text-right font-mono text-[13px] tabular-nums text-orange-700">{point.rateLimitErrors}</td>
                    <td class="py-2 pr-4 text-right font-mono text-[13px] tabular-nums text-red-700">{point.upstreamErrors}</td>
                    <td class="py-2 text-right font-mono text-[13px] tabular-nums text-[#6e6e6e]">{point.gatewayErrors}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        </section>
      {/if}

      <!-- Latency distribution -->
      
      {#if opsMonitor.latency?.buckets?.length > 0}
        <section class="rounded-lg border border-[#ededed] bg-white p-6">
          <h3 class="text-base font-semibold text-[#0d0d0d]">Latency distribution</h3>
          <p class="mt-1 text-sm text-[#6e6e6e]">Successful request latency buckets.</p>
          <!-- maxLatCount via barWidth -->
          <div class="mt-4 space-y-2">
            {#each opsMonitor.latency.buckets as bucket}
              <div class="flex items-center gap-3">
                <span class="w-20 shrink-0 text-right font-mono text-[13px] text-[#6e6e6e]">{bucket.range}</span>
                <div class="flex-1">
                  <div class="h-5 rounded bg-[#e8f5f0]" style="width:{barWidth(bucket.count, maxLatCount)}%"></div>
                </div>
                <span class="w-16 shrink-0 text-right font-mono text-[13px] tabular-nums text-[#0d0d0d]">{formatTokens(bucket.count)}</span>
              </div>
            {/each}
          </div>
        </section>
      {/if}
    {/if}
  </div>
</AuthGate>
