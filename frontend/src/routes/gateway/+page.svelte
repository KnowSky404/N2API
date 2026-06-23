<script>
  import {
    formatCostMicrousd,
    formatTokens,
    gatewayLimitLabel,
    gatewaySettings,
    getActiveKeys,
    getGatewayReadinessIssues,
    getRoutableModelCount,
    getSchedulableProviderAccounts,
    loadGatewaySettings,
    loadKeys,
    loadModelRouting,
    loadProviderAccounts,
    loadUsageSummary,
    login,
    loginForm,
    modelRouting,
    providerAccounts,
    session,
    updateGatewaySettings,
    usage
  } from '$lib/admin-state.svelte.js';

  let gatewayRequested = $state(false);

  const activeKeys = $derived(getActiveKeys());
  const routableModelCount = $derived(getRoutableModelCount());
  const schedulableAccounts = $derived(getSchedulableProviderAccounts());
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

  const usageSections = $derived([
    usageRows('Top models', usage24hModels),
    usageRows('Top provider accounts', usage24hProviderAccounts),
    usageRows('Top client keys', usage24hClientKeys),
    usageRows('Top sessions', usage24hSessions)
  ]);
</script>

<svelte:head>
  <title>N2API Gateway</title>
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
  <div class="space-y-6">
    <section class="rounded-lg border border-[#ededed] bg-white p-6">
      <div class="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
        <div>
        <h2 class="text-2xl font-semibold leading-tight text-[#0d0d0d]">Gateway management</h2>
        <p class="mt-2 max-w-3xl text-sm leading-6 text-[#3c3c3c]">
          Runtime guardrails and short-window traffic distribution for the personal API gateway.
        </p>
        </div>
        <div class="grid gap-2 sm:grid-cols-2 lg:w-[28rem]">
          <a class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]" href="/providers">
            Provider accounts
          </a>
          <a class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]" href="/api-keys">
            API keys
          </a>
          <a class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]" href="/models">
            Routing diagnostics
          </a>
          <a class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]" href="/request-logs">
            Request logs
          </a>
        </div>
      </div>
      <div class="mt-5 rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
        <h3 class="text-sm font-semibold text-[#0d0d0d]">Gateway actions</h3>
        <p class="mt-1 text-sm text-[#6e6e6e]">
          Use these links to adjust account capacity, client key access, routing candidates, and recent gateway traffic.
        </p>
      </div>
    </section>

    <section class="rounded-lg border border-[#ededed] bg-white p-6">
      <div>
        <h3 class="text-base font-semibold text-[#0d0d0d]">Gateway readiness</h3>
        <p class="mt-1 text-sm text-[#6e6e6e]">Core capacity signals required before this gateway can serve daily traffic reliably.</p>
      </div>
      <dl class="mt-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
          <dt class="text-sm font-medium text-[#6e6e6e]">Provider accounts</dt>
          <dd class="mt-2 text-base font-semibold text-[#0d0d0d]">{providerAccounts.loading ? 'Loading' : providerAccounts.items.length}</dd>
        </div>
        <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
          <dt class="text-sm font-medium text-[#6e6e6e]">Schedulable accounts</dt>
          <dd class="mt-2 text-base font-semibold text-[#0d0d0d]">{providerAccounts.loading ? 'Loading' : schedulableAccounts.length}</dd>
        </div>
        <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
          <dt class="text-sm font-medium text-[#6e6e6e]">Routable models</dt>
          <dd class="mt-2 text-base font-semibold text-[#0d0d0d]">{modelRouting.loading ? 'Loading' : routableModelCount}</dd>
        </div>
        <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
          <dt class="text-sm font-medium text-[#6e6e6e]">Active API keys</dt>
          <dd class="mt-2 text-base font-semibold text-[#0d0d0d]">{activeKeys.length}</dd>
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

    <section class="rounded-lg border border-[#ededed] bg-white p-6">
      <div class="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h3 class="text-base font-semibold text-[#0d0d0d]">Runtime limits</h3>
          <p class="mt-1 text-sm text-[#6e6e6e]">Current concurrency and rate guards from the running service.</p>
        </div>
        {#if gatewaySettings.loading}
          <span class="text-sm text-[#6e6e6e]">Loading...</span>
        {/if}
      </div>
      {#if gatewaySettings.error}
        <p class="mt-3 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">
          {gatewaySettings.error}
        </p>
      {:else if gatewaySettings.loading || !gatewaySettings.data}
        <p class="mt-4 text-sm text-[#6e6e6e]">Loading gateway runtime limits...</p>
      {:else}
        <form
          class="mt-4"
          onsubmit={(event) => {
            event.preventDefault();
            updateGatewaySettings();
          }}
        >
        <dl class="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
          <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
            <dt class="text-xs font-medium uppercase tracking-wide text-[#6e6e6e]">Gateway concurrency</dt>
            <dd class="mt-2">
              <input
                class="w-full rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                type="number"
                min="0"
                bind:value={gatewaySettings.data.maxConcurrentGatewayRequests}
              />
              <span class="mt-1 block text-xs text-[#6e6e6e]">{gatewayLimitLabel(gatewaySettings.data.maxConcurrentGatewayRequests)}</span>
            </dd>
          </div>
          <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
            <dt class="text-xs font-medium uppercase tracking-wide text-[#6e6e6e]">Per account concurrency</dt>
            <dd class="mt-2">
              <input
                class="w-full rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                type="number"
                min="0"
                bind:value={gatewaySettings.data.maxConcurrentRequestsPerAccount}
              />
              <span class="mt-1 block text-xs text-[#6e6e6e]">{gatewayLimitLabel(gatewaySettings.data.maxConcurrentRequestsPerAccount)}</span>
            </dd>
          </div>
          <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
            <dt class="text-xs font-medium uppercase tracking-wide text-[#6e6e6e]">Per key concurrency</dt>
            <dd class="mt-2">
              <input
                class="w-full rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                type="number"
                min="0"
                bind:value={gatewaySettings.data.maxConcurrentRequestsPerKey}
              />
              <span class="mt-1 block text-xs text-[#6e6e6e]">{gatewayLimitLabel(gatewaySettings.data.maxConcurrentRequestsPerKey)}</span>
            </dd>
          </div>
          <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
            <dt class="text-xs font-medium uppercase tracking-wide text-[#6e6e6e]">Requests per minute</dt>
            <dd class="mt-2">
              <input
                class="w-full rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                type="number"
                min="0"
                bind:value={gatewaySettings.data.requestsPerMinutePerKey}
              />
              <span class="mt-1 block text-xs text-[#6e6e6e]">{gatewayLimitLabel(gatewaySettings.data.requestsPerMinutePerKey)}</span>
            </dd>
          </div>
          <div class="rounded-md border border-[#ededed] bg-[#fafafa] p-3">
            <dt class="text-xs font-medium uppercase tracking-wide text-[#6e6e6e]">Tokens per minute</dt>
            <dd class="mt-2">
              <input
                class="w-full rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                type="number"
                min="0"
                bind:value={gatewaySettings.data.tokensPerMinutePerKey}
              />
              <span class="mt-1 block text-xs text-[#6e6e6e]">{gatewayLimitLabel(gatewaySettings.data.tokensPerMinutePerKey)}</span>
            </dd>
          </div>
        </dl>
        <div class="mt-5 rounded-md border border-[#ededed] bg-[#fafafa] p-4">
          <div class="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
            <div>
              <h4 class="text-sm font-semibold text-[#0d0d0d]">Provider account auto tests</h4>
              <p class="mt-1 max-w-2xl text-sm text-[#6e6e6e]">
                Run the same probe as Test all accounts on a backend schedule.
              </p>
              <label class="mt-4 flex items-center gap-2 text-sm font-medium text-[#3c3c3c]">
                <input
                  class="h-4 w-4 rounded border-[#d9d9d9] text-[#10a37f] focus:ring-[#10a37f]"
                  type="checkbox"
                  bind:checked={gatewaySettings.data.providerAccountAutoTestEnabled}
                />
                Enable auto tests
              </label>
            </div>
            <label class="block w-full text-sm font-medium text-[#3c3c3c] lg:w-56">
              Interval seconds
              <input
                class="mt-2 w-full rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                type="number"
                min="0"
                bind:value={gatewaySettings.data.providerAccountAutoTestIntervalSeconds}
              />
            </label>
          </div>
        </div>
        <div class="mt-4 flex flex-wrap items-center gap-3">
          <button
            class="rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white disabled:cursor-not-allowed disabled:opacity-60"
            disabled={gatewaySettings.saving}
          >
            {gatewaySettings.saving ? 'Saving' : 'Save runtime limits'}
          </button>
          {#if gatewaySettings.saved}
            <span class="text-sm text-[#0a7a5e]">Runtime limits saved.</span>
          {/if}
        </div>
        </form>
      {/if}
    </section>

    <section class="rounded-lg border border-[#ededed] bg-white p-6">
      <div class="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h3 class="text-base font-semibold text-[#0d0d0d]">24h usage</h3>
          <p class="mt-1 text-sm text-[#6e6e6e]">Traffic distribution across accounts, client keys, and sticky sessions.</p>
        </div>
        {#if usage.loading}
          <span class="text-sm text-[#6e6e6e]">Loading...</span>
        {/if}
      </div>
      {#if usage.error}
        <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">{usage.error}</p>
      {/if}

      <div class="mt-5 grid gap-5 xl:grid-cols-3">
        {#each usageSections as section}
          <div class="rounded-lg border border-[#ededed]">
            <div class="border-b border-[#ededed] bg-[#f5f5f5] px-4 py-3">
              <h4 class="text-sm font-semibold text-[#0d0d0d]">{section.title}</h4>
              <p class="mt-1 text-xs text-[#6e6e6e]">
                {formatTokens(section.summary?.totalRequests)} requests · {formatCostMicrousd(section.summary?.estimatedCostMicrousd)}
              </p>
            </div>
            {#if !section.summary || section.summary.rows.length === 0}
              <p class="px-4 py-4 text-sm text-[#6e6e6e]">No usage in this range.</p>
            {:else}
              <div class="divide-y divide-[#ededed]">
                {#each section.summary.rows.slice(0, 5) as row}
                  <div class="grid gap-2 px-4 py-3 text-sm">
                    <span class="min-w-0 truncate font-medium text-[#0d0d0d]">{row.label || row.id}</span>
                    <span class="font-mono text-[13px] tabular-nums text-[#6e6e6e]">
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
{/if}
