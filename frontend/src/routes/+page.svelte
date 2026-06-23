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
    loadProviderAccounts,
    loadRequestLogs,
    login,
    loginForm,
    logout,
    modelSettings,
    modelRouting,
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

  const statusItems = $derived(getStatusItems());
  const activeKeys = $derived(getActiveKeys());
  const schedulableAccounts = $derived(getSchedulableProviderAccounts());
  const unschedulableAccountSummary = $derived(getUnschedulableProviderAccountSummary());
  const routableModelCount = $derived(getRoutableModelCount());
  const usage24h = $derived(usage.summaries['24h:model'] ?? null);
  const usage24hProviderAccounts = $derived(usage.summaries['24h:provider_account'] ?? null);
  const usage24hClientKeys = $derived(usage.summaries['24h:client_key'] ?? null);
</script>

<svelte:head>
  <title>N2API Dashboard</title>
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
<div class="grid gap-4 md:grid-cols-3">
  {#each statusItems as item}
    <article class="rounded-lg border border-[#ededed] bg-white p-5">
<p class="text-sm font-medium text-[#6e6e6e]">{item.label}</p>
<p class="mt-2 text-lg font-semibold capitalize text-[#0d0d0d]">{item.value}</p>
    </article>
  {/each}
</div>

{#if health.error}
  <section class="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-800">
    {health.error}
  </section>
{/if}

<section class="grid gap-6 lg:grid-cols-2">
  <article class="rounded-lg border border-[#ededed] bg-white p-6">
    <h2 class="text-2xl font-semibold leading-tight text-[#0d0d0d]">Gateway overview</h2>
    <p class="mt-2 text-sm text-[#6e6e6e]">Provider capacity and client access at a glance.</p>
    <div class="mt-5 grid gap-4 sm:grid-cols-2 xl:grid-cols-5">
<div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
  <p class="text-sm font-medium text-[#6e6e6e]">Provider accounts</p>
  <p class="mt-2 text-base font-semibold text-[#0d0d0d]">{providerAccounts.loading ? 'Loading' : providerAccounts.items.length}</p>
</div>
<div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
  <p class="text-sm font-medium text-[#6e6e6e]">Schedulable accounts</p>
  <p class="mt-2 text-base font-semibold text-[#0d0d0d]">{providerAccounts.loading ? 'Loading' : schedulableAccounts.length}</p>
</div>
<div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
  <p class="text-sm font-medium text-[#6e6e6e]">Unschedulable accounts</p>
  <p class="mt-2 text-base font-semibold text-[#0d0d0d]">{providerAccounts.loading ? 'Loading' : providerAccounts.items.length - schedulableAccounts.length}</p>
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
        <p class="mt-4 text-sm text-[#6e6e6e]">Loading gateway runtime limits...</p>
      {:else}
        <dl class="mt-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
          <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
            <dt class="text-xs font-medium uppercase tracking-wide text-[#6e6e6e]">Gateway concurrency</dt>
            <dd class="mt-2 font-mono text-lg text-[#0d0d0d]">{gatewayLimitLabel(gatewaySettings.data.maxConcurrentGatewayRequests)}</dd>
          </div>
          <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
            <dt class="text-xs font-medium uppercase tracking-wide text-[#6e6e6e]">Per account concurrency</dt>
            <dd class="mt-2 font-mono text-lg text-[#0d0d0d]">{gatewayLimitLabel(gatewaySettings.data.maxConcurrentRequestsPerAccount)}</dd>
          </div>
          <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
            <dt class="text-xs font-medium uppercase tracking-wide text-[#6e6e6e]">Per key concurrency</dt>
            <dd class="mt-2 font-mono text-lg text-[#0d0d0d]">{gatewayLimitLabel(gatewaySettings.data.maxConcurrentRequestsPerKey)}</dd>
          </div>
          <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
            <dt class="text-xs font-medium uppercase tracking-wide text-[#6e6e6e]">Requests per minute</dt>
            <dd class="mt-2 font-mono text-lg text-[#0d0d0d]">{gatewayLimitLabel(gatewaySettings.data.requestsPerMinutePerKey)}</dd>
          </div>
          <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
            <dt class="text-xs font-medium uppercase tracking-wide text-[#6e6e6e]">Tokens per minute</dt>
            <dd class="mt-2 font-mono text-lg text-[#0d0d0d]">{gatewayLimitLabel(gatewaySettings.data.tokensPerMinutePerKey)}</dd>
          </div>
        </dl>
      {/if}
    </div>
  </article>
  <article class="rounded-lg border border-[#ededed] bg-white p-6">
    <h2 class="text-2xl font-semibold leading-tight text-[#0d0d0d]">24h usage</h2>
    <p class="mt-2 text-sm text-[#6e6e6e]">Gateway usage and estimated spend in the last day.</p>
    {#if usage.error}
      <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">{usage.error}</p>
    {:else if usage.loading && !usage24h}
      <p class="mt-5 text-sm text-[#6e6e6e]">Loading usage summary...</p>
    {:else}
      <dl class="mt-5 grid gap-4 sm:grid-cols-3">
        <div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
          <dt class="text-sm font-medium text-[#6e6e6e]">Requests</dt>
          <dd class="mt-2 text-base font-semibold tabular-nums text-[#0d0d0d]">{formatTokens(usage24h?.totalRequests)}</dd>
        </div>
        <div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
          <dt class="text-sm font-medium text-[#6e6e6e]">Tokens</dt>
          <dd class="mt-2 text-base font-semibold tabular-nums text-[#0d0d0d]">{formatTokens(usage24h?.totalTokens)}</dd>
        </div>
        <div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
          <dt class="text-sm font-medium text-[#6e6e6e]">Estimated cost</dt>
          <dd class="mt-2 text-base font-semibold tabular-nums text-[#0d0d0d]">{formatCostMicrousd(usage24h?.estimatedCostMicrousd)}</dd>
        </div>
      </dl>
      <div class="mt-5 rounded-lg border border-[#ededed]">
        <div class="border-b border-[#ededed] bg-[#f5f5f5] px-4 py-3">
          <h3 class="text-sm font-semibold text-[#0d0d0d]">Top models</h3>
        </div>
        {#if !usage24h || usage24h.rows.length === 0}
          <p class="px-4 py-4 text-sm text-[#6e6e6e]">No usage in this range.</p>
        {:else}
          <div class="divide-y divide-[#ededed]">
            {#each usage24h.rows.slice(0, 5) as row}
              <div class="grid gap-2 px-4 py-3 text-sm sm:grid-cols-[minmax(0,1fr)_auto]">
                <span class="min-w-0 truncate font-medium text-[#0d0d0d]">{row.label || row.id}</span>
                <span class="font-mono text-[13px] tabular-nums text-[#6e6e6e]">
                  {formatTokens(row.requests)} req · {formatTokens(row.totalTokens)} tokens
                </span>
              </div>
            {/each}
          </div>
        {/if}
      </div>
      <div class="mt-5 rounded-lg border border-[#ededed]">
        <div class="border-b border-[#ededed] bg-[#f5f5f5] px-4 py-3">
          <h3 class="text-sm font-semibold text-[#0d0d0d]">Top provider accounts</h3>
        </div>
        {#if !usage24hProviderAccounts || usage24hProviderAccounts.rows.length === 0}
          <p class="px-4 py-4 text-sm text-[#6e6e6e]">No usage in this range.</p>
        {:else}
          <div class="divide-y divide-[#ededed]">
            {#each usage24hProviderAccounts.rows.slice(0, 5) as row}
              <div class="grid gap-2 px-4 py-3 text-sm sm:grid-cols-[minmax(0,1fr)_auto]">
                <span class="min-w-0 truncate font-medium text-[#0d0d0d]">{row.label || row.id}</span>
                <span class="font-mono text-[13px] tabular-nums text-[#6e6e6e]">
                  {formatTokens(row.requests)} req · {formatTokens(row.totalTokens)} tokens
                </span>
              </div>
            {/each}
          </div>
        {/if}
      </div>
      <div class="mt-5 rounded-lg border border-[#ededed]">
        <div class="border-b border-[#ededed] bg-[#f5f5f5] px-4 py-3">
          <h3 class="text-sm font-semibold text-[#0d0d0d]">Top client keys</h3>
        </div>
        {#if !usage24hClientKeys || usage24hClientKeys.rows.length === 0}
          <p class="px-4 py-4 text-sm text-[#6e6e6e]">No usage in this range.</p>
        {:else}
          <div class="divide-y divide-[#ededed]">
            {#each usage24hClientKeys.rows.slice(0, 5) as row}
              <div class="grid gap-2 px-4 py-3 text-sm sm:grid-cols-[minmax(0,1fr)_auto]">
                <span class="min-w-0 truncate font-medium text-[#0d0d0d]">{row.label || row.id}</span>
                <span class="font-mono text-[13px] tabular-nums text-[#6e6e6e]">
                  {formatTokens(row.requests)} req · {formatTokens(row.totalTokens)} tokens
                </span>
              </div>
            {/each}
          </div>
        {/if}
      </div>
    {/if}
  </article>
  <article class="rounded-lg border border-[#ededed] bg-white p-6">
    <h2 class="text-2xl font-semibold leading-tight text-[#0d0d0d]">Recent activity</h2>
    <p class="mt-2 text-sm text-[#6e6e6e]">Latest request log snapshot.</p>
    <div class="mt-5 overflow-hidden rounded-lg border border-[#ededed]">
{#if requestLogs.loading}
  <p class="p-4 text-sm text-[#6e6e6e]">Loading request logs...</p>
{:else if requestLogs.items.length === 0}
  <p class="p-4 text-sm text-[#6e6e6e]">No gateway requests yet.</p>
{:else}
  <div class="divide-y divide-[#ededed]">
    {#each requestLogs.items.slice(0, 5) as log}
<div class="grid gap-1 bg-white p-4 text-sm sm:grid-cols-[1fr_auto]">
  <span class="font-mono text-[13px] text-[#0d0d0d]">{log.route}</span>
  <span class="tabular-nums text-[#6e6e6e]">{log.statusCode} · {log.latencyMs}ms</span>
</div>
    {/each}
  </div>
{/if}
    </div>
  </article>
</section>
{/if}
