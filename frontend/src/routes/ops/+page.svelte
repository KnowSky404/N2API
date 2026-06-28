<script>
  import {
    formatCostMicrousd,
    formatDate,
    formatTokens,
    login,
    loginForm,
    opsMonitor,
    loadOpsDashboard,
    session,
  } from '$lib/admin-state.svelte.js';

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

  /** @param {{ key?: string | number | null }} bucket */
  function opsErrorHref(bucket) {
    const key = String(bucket?.key ?? '').trim();
    if (!key) return '/request-logs';
    return `/request-logs?error=${encodeURIComponent(key)}`;
  }
</script>

<svelte:head>
  <title>N2API Ops Monitor</title>
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
        Sign in to access ops monitoring.
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
  <div class="space-y-6">
    <!-- Header -->
    <section class="rounded-lg border border-[#ededed] bg-white p-6">
      <div class="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h2 class="text-2xl font-semibold leading-tight text-[#0d0d0d]">Operations monitor</h2>
          <p class="mt-2 text-sm text-[#6e6e6e]">
            Gateway error rates, throughput trends, latency distribution, and health signals.
          </p>
        </div>
        <div class="flex items-center gap-1 rounded-lg border border-[#e5e5e5] bg-white p-0.5">
          {#each rangeOptions as option}
            <button
              class="rounded-md px-3 py-1.5 text-sm font-medium transition-colors {range === option.value ? 'bg-[#0d0d0d] text-white' : 'text-[#3c3c3c] hover:bg-[#f5f5f5]'}"
              onclick={() => changeRange(option.value)}
            >
              {option.label}
            </button>
          {/each}
        </div>
      </div>
    </section>

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
                    <span class="font-mono text-[13px] font-medium text-[#0d0d0d]">{bucket.key}</span>
                    <span class="font-mono text-[13px] tabular-nums text-[#6e6e6e]">{formatTokens(bucket.count)}</span>
                  </div>
                {/each}
              </div>
            </div>
          {/if}
        </section>
      {/if}

      <!-- Throughput trend -->
      {#if opsMonitor.throughput?.points?.length > 0}
        <section class="rounded-lg border border-[#ededed] bg-white p-6">
          <h3 class="text-base font-semibold text-[#0d0d0d]">Request throughput</h3>
          <p class="mt-1 text-sm text-[#6e6e6e]">Requests, tokens, and estimated cost per time bucket.</p>
          <div class="mt-4 overflow-x-auto">
            <table class="w-full text-sm">
              <thead>
                <tr class="border-b border-[#ededed] text-left text-xs font-medium uppercase text-[#6e6e6e]">
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
          <div class="mt-4 overflow-x-auto">
            <table class="w-full text-sm">
              <thead>
                <tr class="border-b border-[#ededed] text-left text-xs font-medium uppercase text-[#6e6e6e]">
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
{/if}
