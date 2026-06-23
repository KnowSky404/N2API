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
    formatDate,
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
    updateProviderAccountPriority
  } from '$lib/admin-state.svelte.js';

  const statusItems = $derived(getStatusItems());
  const activeKeys = $derived(getActiveKeys());
  const schedulableAccounts = $derived(getSchedulableProviderAccounts());
  const unschedulableAccountSummary = $derived(getUnschedulableProviderAccountSummary());
  const routableModelCount = $derived(getRoutableModelCount());
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
