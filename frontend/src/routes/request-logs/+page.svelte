<script>
  import {
    formatDate,
    formatCostMicrousd,
    formatTokens,
    loadUsagePricing,
    loadUsageSummary,
    loadRequestLogs,
    login,
    loginForm,
    requestLogs,
    saveUsagePricing,
    session,
    usage,
    usagePricing,
  } from '$lib/admin-state.svelte.js';

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
    return value || 'Missing';
  }

  const usageRanges = ['24h', '7d', '30d'];
  const usageGroups = [
    { value: 'model', label: 'Model' },
    { value: 'client_key', label: 'Client key' },
    { value: 'provider_account', label: 'Provider account' }
  ];

  /** @param {string} range */
  function summaryForRange(range) {
    return usage.summaries[`${range}:${usage.groupBy}`] ?? null;
  }

  /** @param {Event & { currentTarget: HTMLSelectElement }} event */
  function changeUsageGroup(event) {
    loadUsageSummary(usage.range, event.currentTarget.value);
  }

  /** @param {Event & { currentTarget: HTMLSelectElement }} event */
  function changeUsageRange(event) {
    loadUsageSummary(event.currentTarget.value, usage.groupBy);
  }

  function addPricingRow() {
    usagePricing.rows = [
      ...usagePricing.rows,
      {
        model: '',
        inputMicrousdPerMillion: 0,
        cachedInputMicrousdPerMillion: 0,
        outputMicrousdPerMillion: 0
      }
    ];
  }

  /** @param {number} index */
  function removePricingRow(index) {
    usagePricing.rows = usagePricing.rows.filter((_, rowIndex) => rowIndex !== index);
  }
</script>

<svelte:head>
  <title>N2API Request Logs</title>
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
  <div class="flex flex-wrap items-center justify-between gap-4">
    <div>
<h2 class="text-2xl font-semibold leading-tight text-[#0d0d0d]">Usage summary</h2>
<p class="mt-1 text-sm text-[#6e6e6e]">Gateway usage by time range and routing dimension.</p>
    </div>
    <div class="flex flex-wrap items-center gap-3">
      <label class="text-sm font-medium text-[#3c3c3c]">
        Range
        <select
          class="ml-2 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={usage.range}
          onchange={changeUsageRange}
        >
          {#each usageRanges as range}
            <option value={range}>{range}</option>
          {/each}
        </select>
      </label>
      <label class="text-sm font-medium text-[#3c3c3c]">
        Group
        <select
          class="ml-2 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          bind:value={usage.groupBy}
          onchange={changeUsageGroup}
        >
          {#each usageGroups as group}
            <option value={group.value}>{group.label}</option>
          {/each}
        </select>
      </label>
      <button
        class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
        type="button"
        disabled={usage.loading}
        onclick={() => loadUsageSummary(usage.range, usage.groupBy)}
      >
        {usage.loading ? 'Loading' : 'Refresh usage'}
      </button>
    </div>
  </div>

  {#if usage.error}
    <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">{usage.error}</p>
  {/if}

  <div class="mt-5 grid gap-3 md:grid-cols-3">
    {#each usageRanges as range}
      {@const summary = summaryForRange(range)}
      <button
        class={[
          'rounded-lg border p-4 text-left hover:bg-[#f5f5f5]',
          usage.range === range ? 'border-[#10a37f] bg-[#e8f5f0]' : 'border-[#ededed] bg-white'
        ]}
        type="button"
        onclick={() => loadUsageSummary(range, usage.groupBy)}
      >
        <p class="text-xs font-medium uppercase tracking-normal text-[#6e6e6e]">{range}</p>
        <p class="mt-2 text-2xl font-semibold tabular-nums text-[#0d0d0d]">{formatTokens(summary?.totalTokens)}</p>
        <p class="mt-1 text-sm text-[#6e6e6e]">{formatTokens(summary?.totalRequests)} requests · {formatCostMicrousd(summary?.estimatedCostMicrousd)}</p>
      </button>
    {/each}
  </div>

  <div class="mt-6 overflow-x-auto rounded-lg border border-[#ededed]">
    <table class="w-full min-w-[760px] text-left text-sm">
      <thead class="border-b border-[#e5e5e5] bg-[#f5f5f5] text-[#6e6e6e]">
        <tr>
          <th class="px-4 py-3 font-medium">Group</th>
          <th class="px-4 py-3 font-medium">Requests</th>
          <th class="px-4 py-3 font-medium">Input tokens</th>
          <th class="px-4 py-3 font-medium">Output tokens</th>
          <th class="px-4 py-3 font-medium">Total tokens</th>
          <th class="px-4 py-3 font-medium">Estimated cost</th>
        </tr>
      </thead>
      <tbody class="divide-y divide-[#ededed]">
        {#if usage.loading && !usage.current}
          <tr>
            <td class="px-4 py-5 text-[#6e6e6e]" colspan="6">Loading usage summary...</td>
          </tr>
        {:else if !usage.current || usage.current.rows.length === 0}
          <tr>
            <td class="px-4 py-5 text-[#6e6e6e]" colspan="6">No usage in this range.</td>
          </tr>
        {:else}
          {#each usage.current.rows as row}
            <tr>
              <td class="px-4 py-3 font-medium text-[#0d0d0d]">{row.label || row.id}</td>
              <td class="px-4 py-3 font-mono text-[13px] tabular-nums text-[#3c3c3c]">{formatTokens(row.requests)}</td>
              <td class="px-4 py-3 font-mono text-[13px] tabular-nums text-[#3c3c3c]">{formatTokens(row.inputTokens)}</td>
              <td class="px-4 py-3 font-mono text-[13px] tabular-nums text-[#3c3c3c]">{formatTokens(row.outputTokens)}</td>
              <td class="px-4 py-3 font-mono text-[13px] tabular-nums text-[#3c3c3c]">{formatTokens(row.totalTokens)}</td>
              <td class="px-4 py-3 font-mono text-[13px] tabular-nums text-[#3c3c3c]">{formatCostMicrousd(row.estimatedCostMicrousd)}</td>
            </tr>
          {/each}
        {/if}
      </tbody>
    </table>
  </div>
</section>

<section class="rounded-lg border border-[#ededed] bg-white p-6">
  <form onsubmit={saveUsagePricing}>
    <div class="flex flex-wrap items-center justify-between gap-4">
      <div>
        <h2 class="text-xl font-semibold leading-tight text-[#0d0d0d]">Pricing</h2>
        <p class="mt-1 text-sm text-[#6e6e6e]">USD micro-prices per 1M tokens for historical estimates.</p>
      </div>
      <div class="flex flex-wrap items-center gap-3">
        <button
          class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
          type="button"
          disabled={usagePricing.loading}
          onclick={loadUsagePricing}
        >
          {usagePricing.loading ? 'Loading' : 'Reload pricing'}
        </button>
        <button
          class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]"
          type="button"
          onclick={addPricingRow}
        >
          Add model
        </button>
        <button class="rounded-lg bg-[#0d0d0d] px-3 py-2 text-sm font-medium text-white disabled:cursor-not-allowed disabled:opacity-60" disabled={usagePricing.saving}>
          {usagePricing.saving ? 'Saving' : 'Save pricing'}
        </button>
      </div>
    </div>

    {#if usagePricing.error}
      <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">{usagePricing.error}</p>
    {:else if usagePricing.saved}
      <p class="mt-4 rounded-md border border-[#cce7db] bg-[#e8f5f0] p-3 text-sm text-[#0a7a5e]">Pricing saved.</p>
    {/if}

    <div class="mt-5 overflow-x-auto rounded-lg border border-[#ededed]">
      <table class="w-full min-w-[980px] text-left text-sm">
        <thead class="border-b border-[#e5e5e5] bg-[#f5f5f5] text-[#6e6e6e]">
          <tr>
            <th class="px-4 py-3 font-medium">Model</th>
            <th class="px-4 py-3 font-medium">Input tokens</th>
            <th class="px-4 py-3 font-medium">Cached input</th>
            <th class="px-4 py-3 font-medium">Output tokens</th>
            <th class="px-4 py-3 font-medium">Action</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-[#ededed]">
          {#if usagePricing.rows.length === 0}
            <tr>
              <td class="px-4 py-5 text-[#6e6e6e]" colspan="5">No pricing rows configured.</td>
            </tr>
          {:else}
            {#each usagePricing.rows as row, index}
              <tr>
                <td class="px-4 py-3">
                  <input class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={row.model} placeholder="gpt-5" />
                </td>
                <td class="px-4 py-3">
                  <input class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" type="number" min="0" step="1" bind:value={row.inputMicrousdPerMillion} />
                </td>
                <td class="px-4 py-3">
                  <input class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" type="number" min="0" step="1" bind:value={row.cachedInputMicrousdPerMillion} />
                </td>
                <td class="px-4 py-3">
                  <input class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" type="number" min="0" step="1" bind:value={row.outputMicrousdPerMillion} />
                </td>
                <td class="px-4 py-3">
                  <button class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]" type="button" onclick={() => removePricingRow(index)}>Remove</button>
                </td>
              </tr>
            {/each}
          {/if}
        </tbody>
      </table>
    </div>
  </form>
</section>

<section class="rounded-lg border border-[#ededed] bg-white p-6">
  <div class="flex flex-wrap items-center justify-between gap-4">
    <div>
<h2 class="text-2xl font-semibold leading-tight text-[#0d0d0d]">Request logs</h2>
<p class="mt-1 text-sm text-[#6e6e6e]">
  Recent OpenAI-compatible gateway requests.
</p>
    </div>
    <button
class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
type="button"
disabled={requestLogs.loading}
onclick={loadRequestLogs}
    >
{requestLogs.loading ? 'Refreshing' : 'Refresh'}
    </button>
  </div>

  {#if requestLogs.error}
    <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">
{requestLogs.error}
    </p>
  {/if}

  <div class="mt-6 overflow-x-auto rounded-lg border border-[#ededed]">
    <table class="w-full min-w-[1440px] text-left text-sm">
<thead class="border-b border-[#e5e5e5] bg-[#f5f5f5] text-[#6e6e6e]">
  <tr>
    <th class="px-4 py-3 font-medium">Time</th>
    <th class="px-4 py-3 font-medium">Key</th>
    <th class="px-4 py-3 font-medium">Provider account</th>
    <th class="px-4 py-3 font-medium">Model</th>
    <th class="px-4 py-3 font-medium">Tokens</th>
    <th class="px-4 py-3 font-medium">Estimated cost</th>
    <th class="px-4 py-3 font-medium">Usage</th>
    <th class="px-4 py-3 font-medium">Route</th>
    <th class="px-4 py-3 font-medium">Method</th>
    <th class="px-4 py-3 font-medium">Status</th>
    <th class="px-4 py-3 font-medium">Latency</th>
    <th class="px-4 py-3 font-medium">Error</th>
  </tr>
</thead>
<tbody class="divide-y divide-[#ededed]">
  {#if requestLogs.loading}
    <tr>
      <td class="px-4 py-5 text-[#6e6e6e]" colspan="12">Loading request logs...</td>
    </tr>
  {:else if requestLogs.items.length === 0}
    <tr>
      <td class="px-4 py-5 text-[#6e6e6e]" colspan="12">No gateway requests yet.</td>
    </tr>
  {:else}
    {#each requestLogs.items as log}
      <tr class="bg-white">
        <td class="px-4 py-3 text-[#3c3c3c]">{formatDate(log.createdAt)}</td>
        <td class="px-4 py-3 text-[#3c3c3c]">{log.clientKey || 'Unknown'}</td>
        <td class="px-4 py-3">
          {#if log.providerAccountId}
            <div class="max-w-[220px]">
              <p class="truncate font-medium text-[#0d0d0d]">{log.providerAccountName || `Account ${log.providerAccountId}`}</p>
              <p class="mt-1 text-xs text-[#6e6e6e]">
                {accountTypeLabel(log.providerAccountType)} · ID {log.providerAccountId}
              </p>
            </div>
          {:else}
            <span class="text-[#6e6e6e]">Unassigned</span>
          {/if}
        </td>
        <td class="px-4 py-3 font-mono text-[13px] text-[#3c3c3c]">{log.model || '-'}</td>
        <td class="px-4 py-3 font-mono text-[13px] tabular-nums text-[#3c3c3c]">
          {formatTokens(log.inputTokens)} in / {formatTokens(log.outputTokens)} out
          <p class="mt-1 text-xs text-[#6e6e6e]">{formatTokens(log.totalTokens)} total</p>
        </td>
        <td class="px-4 py-3 font-mono text-[13px] tabular-nums text-[#3c3c3c]">
          {#if !log.pricingMatched && (log.inputTokens || log.outputTokens || log.totalTokens)}
            <span class="font-sans text-sm text-[#6e6e6e]">Unpriced</span>
          {:else}
            {formatCostMicrousd(log.estimatedCostMicrousd)}
          {/if}
        </td>
        <td class="px-4 py-3 text-[#3c3c3c]">
          <span class="text-sm">{usageSourceLabel(log.usageSource)}</span>
        </td>
        <td class="px-4 py-3 font-mono text-[13px] text-[#0d0d0d]">{log.route}</td>
        <td class="px-4 py-3 font-mono text-[13px] text-[#3c3c3c]">{log.method}</td>
        <td class="px-4 py-3">
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
        </td>
        <td class="px-4 py-3 font-mono text-[13px] tabular-nums text-[#3c3c3c]">
          {log.latencyMs}ms
        </td>
        <td class="px-4 py-3 text-[#3c3c3c]">{log.error || '-'}</td>
      </tr>
    {/each}
  {/if}
</tbody>
    </table>
  </div>
</section>
</div>
{/if}
