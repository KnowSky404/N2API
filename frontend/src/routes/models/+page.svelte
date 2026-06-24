<script>
  import {
    formatDate,
    loadModelRouting,
    loadModelRoutingPreview,
    login,
    loginForm,
    modelRouting,
    modelRoutingPreview,
    session
  } from '$lib/admin-state.svelte.js';

  let modelRoutingRequested = $state(false);

  $effect(() => {
    if (!session.authenticated) {
      modelRoutingRequested = false;
      return;
    }
    if (!modelRoutingRequested) {
      modelRoutingRequested = true;
      void loadModelRouting();
    }
  });

  /** @param {string | null | undefined} value */
  function accountTypeLabel(value) {
    if (value === 'api_upstream') return 'API upstream';
    if (value === 'codex_oauth' || !value) return 'Codex OAuth';
    return value;
  }

  /** @param {string | null | undefined} value */
  function statusLabel(value) {
    return value ? value.replaceAll('_', ' ') : 'active';
  }

  /** @param {import('$lib/admin-state.svelte.js').ModelRoutingAccount} account */
  function routingAccountHoverDetail(account) {
    const lastError = account.lastError
      ? `${account.lastError}${account.lastErrorAt ? ` - ${formatDate(account.lastErrorAt)}` : ''}`
      : '';
    const lastTest = account.lastTestAt
      ? `Last test ${account.lastTestStatus || 'checked'} at ${formatDate(account.lastTestAt)}${account.lastTestError ? `: ${account.lastTestError}` : ''}`
      : '';
    return [
      account.displayName || `Account ${account.id}`,
      accountTypeLabel(account.accountType),
      `Priority ${account.priority}`,
      `Load ${account.loadFactor || 1}`,
      account.schedulable ? statusLabel(account.status) : account.unschedulableReason,
      lastTest,
      account.statusReason,
      lastError
    ]
      .filter(Boolean)
      .join('\n');
  }

  const blockedModels = $derived(modelRouting.models.filter((model) => model.enabledCount === 0));
  const blockedReasonSummary = $derived(
    Array.from(
      blockedModels
        .flatMap((model) => model.accounts ?? [])
        .filter((account) => !account.schedulable && account.unschedulableReason)
        .reduce((counts, account) => {
          const reason = account.unschedulableReason;
          counts.set(reason, (counts.get(reason) ?? 0) + 1);
          return counts;
        }, new Map())
        .entries()
    )
  );
  const selectedPreviewAccount = $derived(
    modelRoutingPreview.result?.candidates.find((account) => account.selected) ??
      modelRoutingPreview.result?.candidates.find((account) => account.id === modelRoutingPreview.result?.selectedAccountId) ??
      null
  );
</script>

<svelte:head>
  <title>N2API Routing Diagnostics</title>
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
  <div class="space-y-5">
    <section class="rounded-lg border border-[#ededed] bg-white p-6">
      <div class="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
        <div>
          <h2 class="text-2xl font-semibold leading-tight text-[#0d0d0d]">Routing diagnostics</h2>
          <p class="mt-2 max-w-2xl text-sm leading-6 text-[#3c3c3c]">
            Gateway default model and API key model access are managed from API Keys. Per-account manual models are managed from Provider accounts. This page shows scheduler-visible routing candidates and block reasons.
          </p>
        </div>
        <div class="flex flex-wrap gap-2">
          <a
            class="rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white"
            href="/api-keys"
          >
            Open API Keys
          </a>
          <a
            class="rounded-lg border border-[#e5e5e5] bg-white px-4 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]"
            href="/providers"
          >
            Open Provider accounts
          </a>
        </div>
      </div>
      <div class="mt-5 grid gap-3 sm:grid-cols-3">
        <div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
          <p class="text-xs font-medium uppercase tracking-[0.08em] text-[#6e6e6e]">Default</p>
          <p class="mt-2 truncate text-sm font-semibold text-[#0d0d0d]">{modelRouting.defaultModel || 'Not set'}</p>
        </div>
        <div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
          <p class="text-xs font-medium uppercase tracking-[0.08em] text-[#6e6e6e]">Allowed</p>
          <p class="mt-2 text-sm font-semibold text-[#0d0d0d]">{modelRouting.allowedModels.length}</p>
        </div>
        <div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
          <p class="text-xs font-medium uppercase tracking-[0.08em] text-[#6e6e6e]">Routable</p>
          <p class="mt-2 text-sm font-semibold text-[#0d0d0d]">{modelRouting.models.filter((model) => model.enabledCount > 0).length}</p>
        </div>
      </div>
      <div class="mt-5 rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
        <h3 class="text-sm font-semibold text-[#0d0d0d]">Routing readiness</h3>
        <div class="mt-3 grid gap-3 sm:grid-cols-2">
          <div>
            <p class="text-xs font-medium uppercase tracking-[0.08em] text-[#6e6e6e]">Blocked models</p>
            <p class="mt-2 text-sm font-semibold text-[#0d0d0d]">{blockedModels.length}</p>
          </div>
          <div>
            <p class="text-xs font-medium uppercase tracking-[0.08em] text-[#6e6e6e]">Ready models</p>
            <p class="mt-2 text-sm font-semibold text-[#0d0d0d]">{modelRouting.models.length - blockedModels.length}</p>
          </div>
        </div>
        <div class="mt-4">
          <p class="text-xs font-medium uppercase tracking-[0.08em] text-[#6e6e6e]">Blocked reasons</p>
          {#if blockedReasonSummary.length > 0}
            <div class="mt-2 flex flex-wrap gap-2">
              {#each blockedReasonSummary as [reason, count]}
                <span class="rounded-md border border-amber-200 bg-amber-50 px-2.5 py-1 text-xs font-medium text-amber-800">
                  {statusLabel(reason)}: {count}
                </span>
              {/each}
            </div>
          {:else}
            <p class="mt-2 text-sm text-[#6e6e6e]">No blocked account reasons.</p>
          {/if}
        </div>
        {#if blockedModels.length > 0}
          <p class="mt-3 text-sm text-amber-800">
            {blockedModels.map((model) => model.model).join(', ')} cannot receive model-routed traffic yet.
          </p>
        {:else}
          <p class="mt-3 text-sm text-[#0a7a5e]">All configured routing models have at least one schedulable account.</p>
        {/if}
      </div>
    </section>

    <section class="rounded-lg border border-[#ededed] bg-white p-6">
      <div class="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(320px,420px)]">
        <div>
          <h3 class="text-lg font-semibold text-[#0d0d0d]">Selection preview</h3>
          <p class="mt-1 text-sm leading-6 text-[#6e6e6e]">
            Preview which provider account the gateway would choose for a model and optional sticky session ID without sending traffic or marking an account as used.
          </p>
        </div>
        <form
          class="grid gap-3 rounded-lg border border-[#ededed] bg-[#fafafa] p-4"
          onsubmit={(event) => {
            event.preventDefault();
            loadModelRoutingPreview();
          }}
        >
          <label class="block text-sm font-medium text-[#3c3c3c]">
            Model
            <input
              class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] leading-6 text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
              bind:value={modelRoutingPreview.model}
              placeholder={modelRouting.defaultModel || 'gpt-5'}
              required
            />
          </label>
          <label class="block text-sm font-medium text-[#3c3c3c]">
            Session ID
            <input
              class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] leading-6 text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
              bind:value={modelRoutingPreview.sessionId}
              placeholder="workspace-123"
            />
          </label>
          <label class="block text-sm font-medium text-[#3c3c3c]">
            Excluded account IDs
            <input
              class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] leading-6 text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
              bind:value={modelRoutingPreview.excludedAccountIds}
              placeholder="7, 8"
            />
          </label>
          <button
            class="justify-self-start rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white disabled:cursor-not-allowed disabled:opacity-60"
            disabled={modelRoutingPreview.loading}
          >
            {modelRoutingPreview.loading ? 'Previewing' : 'Preview selection'}
          </button>
        </form>
      </div>

      {#if modelRoutingPreview.error}
        <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">{modelRoutingPreview.error}</p>
      {/if}

      {#if modelRoutingPreview.result}
        <div class="mt-5 rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div>
              <p class="text-xs font-medium uppercase tracking-[0.08em] text-[#6e6e6e]">Selected account</p>
              <p class="mt-1 text-sm font-semibold text-[#0d0d0d]">
                {#if selectedPreviewAccount}
                  {selectedPreviewAccount.displayName || `Account ${selectedPreviewAccount.id}`}
                  <span class="font-normal text-[#6e6e6e]">
                    {accountTypeLabel(selectedPreviewAccount.accountType)} · ID {selectedPreviewAccount.id}
                  </span>
                {:else if modelRoutingPreview.result.selectedAccountId}
                  Account {modelRoutingPreview.result.selectedAccountId}
                {:else}
                  No schedulable account
                {/if}
                {#if modelRoutingPreview.result.sessionId}
                  for session {modelRoutingPreview.result.sessionId}
                {/if}
                {#if modelRoutingPreview.excludedAccountIds.trim()}
                  excluding {modelRoutingPreview.excludedAccountIds}
                {/if}
              </p>
            </div>
            <span class="rounded-md border border-[#d8ece5] bg-[#e8f5f0] px-2.5 py-1 text-xs font-medium text-[#0a7a5e]">
              Model {modelRoutingPreview.result.model}
            </span>
          </div>
          <div class="mt-4 flex flex-wrap gap-2">
            {#each modelRoutingPreview.result.candidates as account}
              <span
                class={[
                  'inline-flex max-w-[300px] items-center gap-2 rounded-md border px-2.5 py-1.5 text-xs',
                  account.selected
                    ? 'border-[#d8ece5] bg-[#e8f5f0] text-[#0a7a5e]'
                    : account.schedulable === false
                      ? 'border-amber-200 bg-amber-50 text-amber-800'
                    : 'border-[#ededed] bg-white text-[#3c3c3c]'
                ]}
              >
                <span class="font-mono text-[11px] font-semibold text-[#0d0d0d]">
                  {account.schedulable === false ? 'Blocked' : `Rank #${account.scheduleRank}`}
                </span>
                <span class="truncate font-medium text-[#0d0d0d]">{account.displayName || `Account ${account.id}`}</span>
                <span>{accountTypeLabel(account.accountType)}</span>
                <span>Priority {account.priority}</span>
                <span>Load {account.loadFactor || 1}</span>
                <span>Used {formatDate(account.lastUsedAt)}</span>
                {#if account.lastTestAt}
                  <span>Test {account.lastTestStatus || 'checked'} {formatDate(account.lastTestAt)}</span>
                {/if}
                {#if account.lastTestError}
                  <span class="font-medium text-amber-800">{account.lastTestError}</span>
                {/if}
                {#if account.selected}
                  <span class="font-medium text-[#0a7a5e]">Selected</span>
                {/if}
                {#if account.stickyBound}
                  <span class="font-medium text-[#0a7a5e]">Sticky bound</span>
                {/if}
                {#if account.schedulable === false && account.unschedulableReason}
                  <span class="font-medium text-amber-800">{account.unschedulableReason}</span>
                {/if}
              </span>
            {/each}
          </div>
        </div>
      {/if}
    </section>

    <section class="rounded-lg border border-[#ededed] bg-white p-6">
      <div class="flex items-start justify-between gap-4">
        <div>
          <h3 class="text-lg font-semibold text-[#0d0d0d]">Routing candidates</h3>
          <p class="mt-1 text-sm text-[#6e6e6e]">Candidate accounts are ordered the same way the gateway scheduler will consider them.</p>
        </div>
        {#if modelRouting.loading}
          <span class="text-sm text-[#6e6e6e]">Loading...</span>
        {/if}
      </div>

      {#if modelRouting.error}
        <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">{modelRouting.error}</p>
      {/if}

      {#if modelRouting.warnings.length}
        <div class="mt-4 space-y-2">
          {#each modelRouting.warnings as warning}
            <p class="rounded-md border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800">{warning}</p>
          {/each}
        </div>
      {/if}

      <div class="mt-5 overflow-x-auto rounded-lg border border-[#ededed]">
        <table class="min-w-full divide-y divide-[#ededed] text-left text-sm">
          <thead class="bg-[#fafafa] text-xs uppercase tracking-[0.08em] text-[#6e6e6e]">
            <tr>
              <th class="px-4 py-3 font-medium">Model</th>
              <th class="px-4 py-3 font-medium">Policy</th>
              <th class="px-4 py-3 font-medium">Accounts</th>
              <th class="px-4 py-3 font-medium">Candidates</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-[#ededed]">
            {#if modelRouting.loading && modelRouting.models.length === 0}
              <tr>
                <td class="px-4 py-5 text-[#6e6e6e]" colspan="4">Loading model routing...</td>
              </tr>
            {:else if modelRouting.models.length === 0}
              <tr>
                <td class="px-4 py-5 text-[#6e6e6e]" colspan="4">No model routing policy configured yet.</td>
              </tr>
            {:else}
              {#each modelRouting.models as model}
                <tr class="align-top">
                  <td class="px-4 py-4">
                    <p class="font-medium text-[#0d0d0d]">{model.model}</p>
                    {#if model.enabledCount === 0}
                      <p class="mt-1 text-xs text-amber-700">No schedulable account</p>
                    {/if}
                  </td>
                  <td class="px-4 py-4 text-[#3c3c3c]">
                    {model.allowed ? 'Allowed' : 'Hidden'}
                  </td>
                  <td class="px-4 py-4 text-[#3c3c3c]">
                    {model.enabledCount} / {model.configuredCount}
                  </td>
                  <td class="px-4 py-4">
                    {#if model.accounts?.length}
                      <div class="flex flex-wrap gap-2">
                        {#each model.accounts as account}
                          <span
                            class={[
                              'inline-flex max-w-[300px] items-center gap-2 rounded-md border px-2.5 py-1.5 text-xs',
                              account.schedulable
                                ? 'border-[#ededed] bg-[#fafafa] text-[#3c3c3c]'
                                : 'border-amber-200 bg-amber-50 text-amber-800'
                            ]}
                            title={routingAccountHoverDetail(account)}
                          >
                            <span class="font-mono text-[11px] font-semibold text-[#0d0d0d]">Schedule rank #{account.scheduleRank}</span>
                            <span class="truncate font-medium text-[#0d0d0d]">{account.displayName || `Account ${account.id}`}</span>
                            <span class="text-[#6e6e6e]">{accountTypeLabel(account.accountType)}</span>
                            <span class="text-[#6e6e6e]">Priority {account.priority}</span>
                            <span class="text-[#6e6e6e]">Load {account.loadFactor || 1}</span>
                            <span class="text-[#6e6e6e]">Used {formatDate(account.lastUsedAt)}</span>
                            {#if account.lastTestAt}
                              <span class="text-[#6e6e6e]">Test {account.lastTestStatus || 'checked'} {formatDate(account.lastTestAt)}</span>
                            {/if}
                            {#if account.lastTestError}
                              <span class="font-medium text-amber-800">{account.lastTestError}</span>
                            {/if}
                            <span class={account.schedulable ? 'text-[#6e6e6e]' : 'font-medium text-amber-800'}>
                              {account.schedulable ? statusLabel(account.status) : account.unschedulableReason}
                            </span>
                          </span>
                        {/each}
                      </div>
                    {:else}
                      <span class="text-sm text-[#6e6e6e]">No candidates</span>
                    {/if}
                  </td>
                </tr>
              {/each}
            {/if}
          </tbody>
        </table>
      </div>
    </section>
  </div>
{/if}
