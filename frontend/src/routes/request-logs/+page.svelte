<script>
  import {
    formatDate,
    loadRequestLogs,
    login,
    loginForm,
    requestLogs,
    session,
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
    <table class="w-full min-w-[1280px] text-left text-sm">
<thead class="border-b border-[#e5e5e5] bg-[#f5f5f5] text-[#6e6e6e]">
  <tr>
    <th class="px-4 py-3 font-medium">Time</th>
    <th class="px-4 py-3 font-medium">Key</th>
    <th class="px-4 py-3 font-medium">Provider account</th>
    <th class="px-4 py-3 font-medium">Model</th>
    <th class="px-4 py-3 font-medium">Tokens</th>
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
      <td class="px-4 py-5 text-[#6e6e6e]" colspan="11">Loading request logs...</td>
    </tr>
  {:else if requestLogs.items.length === 0}
    <tr>
      <td class="px-4 py-5 text-[#6e6e6e]" colspan="11">No gateway requests yet.</td>
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
          {log.inputTokens || 0}/{log.outputTokens || 0}/{log.totalTokens || 0}
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
{/if}
