<script>
  import {
    accountLabel,
    accountTypeLabel,
    bulkUpdateSelectedProviderAccountScheduling,
    bulkUpdateSelectedProviderAccounts,
    clearProviderAccountSelection,
    completeProviderCallback,
    connectProvider,
    copyAuthorizationURL,
    createAPIUpstreamAccount,
    disconnectProviderAccount,
    formatCostMicrousd,
    formatDate,
    formatTokens,
    futureTimeRemainingLabel,
    getAccountModelsState,
    getAccountTestResultsState,
    getProviderStateLabel,
    getSchedulableProviderAccounts,
    getUnschedulableProviderAccountSummary,
    loadProviderAccounts,
    loadUsageSummary,
    login,
    loginForm,
    apiUpstreamForm,
    provider,
    providerAccounts,
    providerAccountBulkSchedulingForm,
    providerAccountPauseForm,
    providerConnectForm,
    providerOAuth,
    pauseProviderAccount,
    pauseSelectedProviderAccounts,
    refreshProviderAccount,
    removeAccountModel,
    resetProviderAccountStatus,
    resetSelectedProviderAccountStatus,
    saveAccountModels,
    session,
    selectedProviderAccountIds,
    setAccountModelEnabled,
    statusLabel,
    testAllProviderAccounts,
    testProviderAccount,
    testSelectedProviderAccounts,
    toggleAccountTestHistory,
    toggleProviderAccountSelection,
    updateProviderAccount,
    updateProviderAccountLoadFactor,
    updateProviderAccountName,
    updateProviderAccountPriority,
    usage
  } from '$lib/admin-state.svelte.js';

  let accountSearch = $state('');
  let accountSort = $state({ key: 'priority', direction: 'asc' });
  let providerUsageRequested = $state(false);

  const providerStateLabel = $derived(getProviderStateLabel());
  const schedulableProviderAccounts = $derived(getSchedulableProviderAccounts());
  const unschedulableProviderAccountSummary = $derived(getUnschedulableProviderAccountSummary());
  const usage24hProviderAccounts = $derived(usage.summaries['24h:provider_account'] ?? null);
  const selectedProviderAccountCount = $derived(Object.keys(selectedProviderAccountIds).length);
  const filteredProviderAccounts = $derived(
    sortProviderAccounts(
      providerAccounts.items.filter((account) => {
        const query = accountSearch.trim().toLowerCase();
        if (!query) return true;
        return accountSearchText(account).includes(query);
      })
    )
  );

  $effect(() => {
    if (!session.authenticated) {
      providerUsageRequested = false;
      return;
    }
    if (!providerUsageRequested) {
      providerUsageRequested = true;
      void loadUsageSummary('24h', 'provider_account');
    }
  });

  /** @param {import('$lib/admin-state.svelte.js').ProviderAccount} account */
  function accountSearchText(account) {
    return [
      account.name,
      account.displayName,
      account.subject,
      account.baseUrl,
      account.provider,
      accountTypeLabel(account),
      statusLabel(account.status),
      account.statusReason,
      account.lastError
    ]
      .filter(Boolean)
      .join(' ')
      .toLowerCase();
  }

  /**
   * @param {import('$lib/admin-state.svelte.js').ProviderAccount[]} accounts
   */
  function sortProviderAccounts(accounts) {
    return [...accounts].sort((left, right) => {
      const leftValue = accountSortValue(left, accountSort.key);
      const rightValue = accountSortValue(right, accountSort.key);
      const result =
        typeof leftValue === 'number' && typeof rightValue === 'number'
          ? leftValue - rightValue
          : String(leftValue).localeCompare(String(rightValue), undefined, { numeric: true, sensitivity: 'base' });
      return accountSort.direction === 'asc' ? result : -result;
    });
  }

  /**
   * @param {import('$lib/admin-state.svelte.js').ProviderAccount} account
   * @param {string} key
   */
  function accountSortValue(account, key) {
    if (key === 'account') return accountLabel(account);
    if (key === 'status') return statusLabel(account.status);
    if (key === 'enabled') return account.enabled ? 0 : 1;
    if (key === 'priority') return account.priority ?? 0;
    if (key === 'loadFactor') return account.loadFactor ?? 1;
    if (key === 'expires') return Date.parse(account.accessTokenExpiresAt ?? '') || 0;
    if (key === 'refresh') return Date.parse(account.lastRefreshAt ?? '') || 0;
    if (key === 'used') return Date.parse(account.lastUsedAt ?? '') || 0;
    return '';
  }

  /** @param {string} key */
  function setProviderAccountSort(key) {
    accountSort =
      accountSort.key === key
        ? { key, direction: accountSort.direction === 'asc' ? 'desc' : 'asc' }
        : { key, direction: 'asc' };
  }

  /** @param {string} key */
  function providerAccountSortDirection(key) {
    if (accountSort.key !== key) return 'none';
    return accountSort.direction === 'asc' ? 'ascending' : 'descending';
  }

  /** @param {string} key */
  function sortIndicator(key) {
    if (accountSort.key !== key) return '';
    return accountSort.direction === 'asc' ? ' Asc' : ' Desc';
  }

  /** @param {import('$lib/admin-state.svelte.js').ProviderAccount} account */
  function accountSecondaryLabel(account) {
    const label = account.displayName?.trim();
    if (!label || label === accountLabel(account)) return '';

    const duplicateLabels = [statusLabel(account.status), account.statusReason]
      .filter(Boolean)
      .map((value) => value.toLowerCase());
    return duplicateLabels.includes(label.toLowerCase()) ? '' : label;
  }

  /** @param {import('$lib/admin-state.svelte.js').ProviderAccount} account */
  function accountHoverDetail(account) {
    return [accountLabel(account), accountSecondaryLabel(account), accountProviderDetail(account)]
      .filter(Boolean)
      .join('\n');
  }

  /** @param {import('$lib/admin-state.svelte.js').ProviderAccount} account */
  function accountProviderDetail(account) {
    const provider = account.provider || 'unknown';
    return account.subject ? `${provider} / ${account.subject}` : provider;
  }

  /** @param {import('$lib/admin-state.svelte.js').ProviderAccount} account */
  function isCodexOAuthAccount(account) {
    return account.accountType === 'codex_oauth' || !account.accountType;
  }

  /**
   * @param {import('$lib/admin-state.svelte.js').ProviderAccount} account
   * @param {SubmitEvent & { currentTarget: HTMLFormElement }} event
   */
  async function updateAPIUpstreamCredential(account, event) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    const baseUrl = String(data.get('baseUrl') ?? '');
    const apiKey = String(data.get('apiKey') ?? '');
    /** @type {{ baseUrl?: string, apiKey?: string }} */
    const patch = {};

    if (baseUrl.trim() && baseUrl.trim() !== (account.baseUrl ?? '').trim()) {
      patch.baseUrl = baseUrl;
    }
    if (apiKey.trim()) {
      patch.apiKey = apiKey;
    }
    if (Object.keys(patch).length === 0) return;

    await updateProviderAccount(account, patch);
    const apiKeyInput = form.elements.namedItem('apiKey');
    if (apiKeyInput instanceof HTMLInputElement) {
      apiKeyInput.value = '';
    }
  }

  /** @param {import('$lib/admin-state.svelte.js').ProviderAccount} account */
  function statusHoverDetail(account) {
    return [
      statusLabel(account.status),
      account.rateLimitedUntil ? `Rate limited until ${formatDate(account.rateLimitedUntil)}` : '',
      account.circuitOpenUntil ? `Circuit until ${formatDate(account.circuitOpenUntil)}` : '',
      account.lastTestAt
        ? `Last test ${account.lastTestStatus || 'checked'} at ${formatDate(account.lastTestAt)}${account.lastTestError ? `: ${account.lastTestError}` : ''}`
        : '',
      account.statusReason,
      account.lastError ? `${account.lastError}${account.lastErrorAt ? ` - ${formatDate(account.lastErrorAt)}` : ''}` : ''
    ]
      .filter(Boolean)
      .join('\n');
  }

  /** @param {import('$lib/admin-state.svelte.js').AccountModel[]} models */
  function enabledAccountModelCount(models) {
    return models.filter((model) => model.enabled).length;
  }

  /** @param {string | null | undefined} status */
  function testResultStatusClass(status) {
    if (status === 'passed') return 'bg-[#e8f5f0] text-[#0a7a5e]';
    if (status === 'failed') return 'bg-amber-50 text-amber-700';
    return 'bg-[#f5f5f5] text-[#6e6e6e]';
  }
</script>

<svelte:head>
  <title>N2API Providers</title>
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
  <div class="flex flex-wrap items-start justify-between gap-4">
    <div>
<h2 class="text-2xl font-semibold leading-tight text-[#0d0d0d]">Provider accounts</h2>
<p class="mt-1 text-sm text-[#6e6e6e]">Codex OAuth and API upstream gateway exits</p>
    </div>
    <span
class={[
  'inline-flex rounded-full px-2.5 py-1 text-xs font-medium capitalize',
  provider.data?.connected
    ? 'bg-[#e8f5f0] text-[#0a7a5e]'
    : provider.data?.configured
      ? 'bg-[#f5f5f5] text-[#3c3c3c]'
      : 'bg-red-50 text-red-700'
]}
    >
{provider.loading ? 'Checking' : providerStateLabel}
    </span>
  </div>

  <div class="mt-5 grid gap-4 md:grid-cols-3">
    <article class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
<p class="text-sm font-medium text-[#6e6e6e]">Configuration</p>
<p class="mt-2 text-base font-semibold text-[#0d0d0d]">
  {provider.data?.configured ? 'Ready' : 'Missing'}
</p>
    </article>
    <article class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
<p class="text-sm font-medium text-[#6e6e6e]">Accounts</p>
<p class="mt-2 text-base font-semibold text-[#0d0d0d]">
  {providerAccounts.loading ? 'Loading' : providerAccounts.items.length}
</p>
    </article>
    <article class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
<p class="text-sm font-medium text-[#6e6e6e]">Enabled</p>
<p class="mt-2 text-base font-semibold text-[#0d0d0d]">
  {providerAccounts.items.filter((account) => account.enabled).length}
</p>
    </article>
  </div>

  <section class="mt-5 rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
    <h3 class="text-base font-semibold text-[#0d0d0d]">Scheduling capacity</h3>
    <div class="mt-3 grid gap-3 sm:grid-cols-2 xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_16rem]">
      <article class="rounded-md border border-[#ededed] bg-white p-3">
        <p class="text-xs font-medium uppercase tracking-wide text-[#6e6e6e]">Schedulable</p>
        <p class="mt-2 text-lg font-semibold text-[#0d0d0d]">{providerAccounts.loading ? 'Loading' : schedulableProviderAccounts.length}</p>
      </article>
      <article class="rounded-md border border-[#ededed] bg-white p-3">
        <p class="text-xs font-medium uppercase tracking-wide text-[#6e6e6e]">Blocked</p>
        <p class="mt-2 text-lg font-semibold text-[#0d0d0d]">
          {providerAccounts.loading ? 'Loading' : providerAccounts.items.length - schedulableProviderAccounts.length}
        </p>
        {#if !providerAccounts.loading && unschedulableProviderAccountSummary.length > 0}
          <p class="mt-3 text-xs font-medium uppercase tracking-wide text-[#6e6e6e]">Blocked reasons</p>
          <p class="mt-2 text-xs text-[#6e6e6e]">
            {unschedulableProviderAccountSummary.map((item) => `${item.reasonLabel}: ${item.count}`).join(' · ')}
          </p>
        {/if}
      </article>
      <label class="rounded-md border border-[#ededed] bg-white p-3 text-sm font-medium text-[#3c3c3c] sm:col-span-2 xl:col-span-1">
        Pause duration seconds
        <input
          class="mt-2 w-full rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          type="number"
          min="60"
          max="86400"
          step="1"
          bind:value={providerAccountPauseForm.durationSeconds}
        />
        <span class="mt-1 block text-xs font-normal text-[#6e6e6e]">Used by each Pause scheduling action.</span>
      </label>
    </div>
  </section>

  <section class="mt-5 rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <h3 class="text-base font-semibold text-[#0d0d0d]">24h account usage</h3>
        <p class="mt-1 text-sm text-[#6e6e6e]">Gateway traffic distribution by provider account.</p>
      </div>
      {#if usage.loading}
        <span class="text-sm text-[#6e6e6e]">Loading...</span>
      {/if}
    </div>
    {#if usage.error}
      <p class="mt-3 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">{usage.error}</p>
    {:else if !usage24hProviderAccounts || usage24hProviderAccounts.rows.length === 0}
      <p class="mt-4 text-sm text-[#6e6e6e]">No provider account usage in the last 24h.</p>
    {:else}
      <div class="mt-4 overflow-x-auto rounded-lg border border-[#ededed] bg-white">
        <table class="w-full min-w-[640px] text-left text-sm">
          <thead class="border-b border-[#e5e5e5] bg-[#f5f5f5] text-[#6e6e6e]">
            <tr>
              <th class="px-4 py-3 font-medium">Provider account</th>
              <th class="px-4 py-3 font-medium">Requests</th>
              <th class="px-4 py-3 font-medium">Tokens</th>
              <th class="px-4 py-3 font-medium">Estimated cost</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-[#ededed]">
            {#each usage24hProviderAccounts.rows.slice(0, 8) as row}
              <tr>
                <td class="px-4 py-3 font-medium text-[#0d0d0d]">{row.label || row.id}</td>
                <td class="px-4 py-3 font-mono text-[13px] tabular-nums text-[#3c3c3c]">{formatTokens(row.requests)}</td>
                <td class="px-4 py-3 font-mono text-[13px] tabular-nums text-[#3c3c3c]">{formatTokens(row.totalTokens)}</td>
                <td class="px-4 py-3 font-mono text-[13px] tabular-nums text-[#3c3c3c]">{formatCostMicrousd(row.estimatedCostMicrousd)}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  </section>

  <div class="mt-5 flex flex-wrap items-center justify-between gap-3">
    <p class="text-sm text-[#6e6e6e]">
Last refresh: {formatDate(provider.data?.lastRefreshAt)}
    </p>
    <div class="flex flex-wrap gap-2">
<button
  class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
  type="button"
  disabled={providerAccounts.loading || providerAccounts.saving || providerAccounts.items.length === 0}
  onclick={testAllProviderAccounts}
>
  Test all accounts
</button>
<button
  class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
  type="button"
  disabled={providerAccounts.loading}
  onclick={loadProviderAccounts}
>
  {providerAccounts.loading ? 'Refreshing' : 'Refresh'}
</button>
    </div>
  </div>

  <form
    class="mt-5 grid gap-3 rounded-lg border border-[#ededed] bg-[#fafafa] p-4 lg:grid-cols-[minmax(220px,1fr)_140px_minmax(170px,auto)_auto]"
    onsubmit={(event) => {
event.preventDefault();
connectProvider();
    }}
  >
    <label class="grid min-w-0 gap-1 text-sm font-medium text-[#3c3c3c]">
Account name
<input
  class="w-full min-w-0 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
  type="text"
  placeholder="Work Codex"
  bind:value={providerConnectForm.name}
/>
    </label>
    <label class="grid min-w-0 gap-1 text-sm font-medium text-[#3c3c3c]">
Priority
<input
  class="w-full min-w-0 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
  type="number"
  min="0"
  step="1"
  bind:value={providerConnectForm.priority}
/>
    </label>
    <label class="inline-flex h-10 self-end whitespace-nowrap items-center gap-2 text-sm font-medium text-[#3c3c3c]">
<input
  class="size-4 shrink-0 rounded border-[#e5e5e5] text-[#10a37f] focus:ring-[#10a37f]"
  type="checkbox"
  bind:checked={providerConnectForm.enabled}
/>
Enable after login
    </label>
    <button
class="self-end rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white disabled:cursor-not-allowed disabled:opacity-60"
type="submit"
      disabled={provider.loading || !provider.data?.configured || provider.connecting}
    >
{provider.connecting ? 'Generating link' : 'Add OAuth account'}
    </button>
  </form>

  <form
    class="mt-5 grid gap-3 rounded-lg border border-[#ededed] bg-[#fafafa] p-4 lg:grid-cols-[minmax(180px,1fr)_minmax(240px,1.3fr)_minmax(180px,1fr)_120px_120px_auto]"
    onsubmit={(event) => {
      event.preventDefault();
      createAPIUpstreamAccount();
    }}
  >
    <div class="lg:col-span-6">
      <h3 class="text-base font-semibold text-[#0d0d0d]">API upstream</h3>
      <p class="mt-1 text-sm text-[#6e6e6e]">Add an OpenAI-compatible upstream by API key and base URL.</p>
    </div>
    <label class="grid min-w-0 gap-1 text-sm font-medium text-[#3c3c3c]">
Name
<input
  class="w-full min-w-0 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
  type="text"
  placeholder="OpenAI API"
  bind:value={apiUpstreamForm.name}
  required
/>
    </label>
    <label class="grid min-w-0 gap-1 text-sm font-medium text-[#3c3c3c]">
Base URL
<input
  class="w-full min-w-0 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
  type="url"
  placeholder="https://api.openai.com/v1"
  bind:value={apiUpstreamForm.baseUrl}
  required
/>
<span class="text-xs font-normal text-[#6e6e6e]">HTTPS is required unless HTTP upstreams are explicitly enabled in the server environment.</span>
    </label>
    <label class="grid min-w-0 gap-1 text-sm font-medium text-[#3c3c3c]">
API key
<input
  class="w-full min-w-0 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
  type="password"
  autocomplete="off"
  bind:value={apiUpstreamForm.apiKey}
  required
/>
    </label>
    <label class="grid min-w-0 gap-1 text-sm font-medium text-[#3c3c3c]">
Priority
<input
  class="w-full min-w-0 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
  type="number"
  min="0"
  step="1"
  bind:value={apiUpstreamForm.priority}
/>
    </label>
    <label class="grid min-w-0 gap-1 text-sm font-medium text-[#3c3c3c]">
Load factor
<input
  class="w-full min-w-0 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
  type="number"
  min="1"
  max="100"
  step="1"
  bind:value={apiUpstreamForm.loadFactor}
/>
    </label>
    <label class="inline-flex h-10 self-end whitespace-nowrap items-center gap-2 text-sm font-medium text-[#3c3c3c]">
<input
  class="size-4 shrink-0 rounded border-[#e5e5e5] text-[#10a37f] focus:ring-[#10a37f]"
  type="checkbox"
  bind:checked={apiUpstreamForm.enabled}
/>
Enabled
    </label>
    <label class="grid min-w-0 gap-1 text-sm font-medium text-[#3c3c3c] lg:col-span-5">
Manual models
<textarea
  class="min-h-20 w-full min-w-0 resize-y rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] leading-5 text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
  placeholder={'gpt-4.1\ngpt-4.1-mini'}
  bind:value={apiUpstreamForm.modelsText}
></textarea>
    </label>
    <button
class="self-end rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white disabled:cursor-not-allowed disabled:opacity-60"
type="submit"
disabled={apiUpstreamForm.submitting}
    >
{apiUpstreamForm.submitting ? 'Adding' : 'Add API upstream'}
    </button>
    {#if apiUpstreamForm.error}
      <p class="lg:col-span-5 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">
        {apiUpstreamForm.error}
      </p>
    {/if}
  </form>

  {#if providerOAuth.authorizationUrl}
    <div class="mt-5 rounded-lg border border-[#cbe7dd] bg-[#e8f5f0] p-4">
<div class="flex flex-wrap items-center justify-between gap-3">
  <p class="text-sm font-medium text-[#0a7a5e]">OAuth authorization link</p>
  <button
    class="rounded-lg border border-[#b7d9cd] bg-white px-3 py-1.5 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]"
    type="button"
    onclick={copyAuthorizationURL}
  >
    {providerOAuth.copied ? 'Copied' : 'Copy'}
  </button>
</div>
<code class="mt-3 block overflow-x-auto rounded-md bg-white px-3 py-2 font-mono text-[13px] leading-6 text-[#0d0d0d]">
  {providerOAuth.authorizationUrl}
</code>
<form
  class="mt-4 grid gap-3 lg:grid-cols-[minmax(0,1fr)_auto]"
  onsubmit={(event) => {
    event.preventDefault();
    completeProviderCallback();
  }}
>
  <label class="grid min-w-0 gap-1 text-sm font-medium text-[#3c3c3c]">
    Callback URL
    <input
      class="w-full min-w-0 rounded-lg border border-[#b7d9cd] bg-white px-3 py-2 font-mono text-[13px] leading-6 text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#cbe7dd]"
      type="url"
      placeholder="http://localhost:1455/auth/callback?code=...&state=..."
      bind:value={providerOAuth.callbackUrl}
      required
    />
  </label>
  <button
    class="self-end rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white disabled:cursor-not-allowed disabled:opacity-60"
    type="submit"
    disabled={providerOAuth.completing || !providerOAuth.callbackUrl.trim()}
  >
    {providerOAuth.completing ? 'Completing' : 'Complete OAuth'}
  </button>
</form>
    </div>
  {/if}

  {#if provider.error}
    <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">
{provider.error}
    </p>
  {/if}

  {#if providerAccounts.error}
    <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">
{providerAccounts.error}
    </p>
  {/if}

  <div class="mt-6 flex flex-wrap items-end justify-between gap-3">
    <label class="grid min-w-[240px] flex-1 gap-1 text-sm font-medium text-[#3c3c3c]">
Search
<input
  class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
  type="search"
  placeholder="Search accounts"
  bind:value={accountSearch}
/>
    </label>
    <div class="flex flex-wrap items-center justify-end gap-2 pb-1">
      <p class="mr-2 text-sm text-[#6e6e6e]">
Showing {filteredProviderAccounts.length} of {providerAccounts.items.length}
        {#if selectedProviderAccountCount > 0}
          · {selectedProviderAccountCount} selected
        {/if}
      </p>
      <button
        class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
        type="button"
        disabled={selectedProviderAccountCount === 0 || providerAccounts.saving}
        onclick={testSelectedProviderAccounts}
      >
        Test selected
      </button>
      <button
        class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
        type="button"
        disabled={selectedProviderAccountCount === 0 || providerAccounts.saving}
        onclick={pauseSelectedProviderAccounts}
      >
        Pause selected
      </button>
      <button
        class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
        type="button"
        disabled={selectedProviderAccountCount === 0 || providerAccounts.saving}
        onclick={resetSelectedProviderAccountStatus}
      >
        Reset selected
      </button>
      <button
        class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
        type="button"
        disabled={selectedProviderAccountCount === 0 || providerAccounts.saving}
        onclick={() => bulkUpdateSelectedProviderAccounts(true)}
      >
        Enable selected
      </button>
      <button
        class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
        type="button"
        disabled={selectedProviderAccountCount === 0 || providerAccounts.saving}
        onclick={() => bulkUpdateSelectedProviderAccounts(false)}
      >
        Disable selected
      </button>
      <label class="grid w-28 gap-1 text-xs font-medium text-[#3c3c3c]">
        Bulk priority
        <input
          class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
          type="number"
          min="0"
          step="1"
          bind:value={providerAccountBulkSchedulingForm.priority}
          disabled={providerAccounts.saving}
        />
      </label>
      <label class="grid w-32 gap-1 text-xs font-medium text-[#3c3c3c]">
        Bulk load factor
        <input
          class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
          type="number"
          min="1"
          max="100"
          step="1"
          bind:value={providerAccountBulkSchedulingForm.loadFactor}
          disabled={providerAccounts.saving}
        />
      </label>
      <button
        class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
        type="button"
        disabled={selectedProviderAccountCount === 0 || providerAccounts.saving}
        onclick={bulkUpdateSelectedProviderAccountScheduling}
      >
        Apply scheduling
      </button>
      <button
        class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
        type="button"
        disabled={selectedProviderAccountCount === 0 || providerAccounts.saving}
        onclick={clearProviderAccountSelection}
      >
        Clear selection
      </button>
    </div>
  </div>

  <div class="mt-6 overflow-x-auto rounded-lg border border-[#ededed]">
    <table class="w-full min-w-[1320px] text-left text-sm">
<thead class="border-b border-[#e5e5e5] bg-[#f5f5f5] text-[#6e6e6e]">
  <tr>
    <th class="w-12 px-4 py-3 font-medium">Select</th>
    <th class="px-4 py-3 font-medium" aria-sort={providerAccountSortDirection('account')}>
      <button class="inline-flex items-center gap-1 text-left font-medium hover:text-[#0d0d0d]" type="button" onclick={() => setProviderAccountSort('account')}>
        Account<span class="text-[11px]">{sortIndicator('account')}</span>
      </button>
    </th>
    <th class="w-36 px-4 py-3 font-medium">Type</th>
    <th class="w-44 px-4 py-3 font-medium" aria-sort={providerAccountSortDirection('status')}>
      <button class="inline-flex items-center gap-1 text-left font-medium hover:text-[#0d0d0d]" type="button" onclick={() => setProviderAccountSort('status')}>
        Status<span class="text-[11px]">{sortIndicator('status')}</span>
      </button>
    </th>
    <th class="w-32 px-4 py-3 font-medium" aria-sort={providerAccountSortDirection('enabled')}>
      <button class="inline-flex items-center gap-1 text-left font-medium hover:text-[#0d0d0d]" type="button" onclick={() => setProviderAccountSort('enabled')}>
        Enabled<span class="text-[11px]">{sortIndicator('enabled')}</span>
      </button>
    </th>
    <th class="w-28 px-4 py-3 font-medium" aria-sort={providerAccountSortDirection('priority')}>
      <button class="inline-flex items-center gap-1 text-left font-medium hover:text-[#0d0d0d]" type="button" onclick={() => setProviderAccountSort('priority')}>
        Priority<span class="text-[11px]">{sortIndicator('priority')}</span>
      </button>
    </th>
    <th class="w-28 px-4 py-3 font-medium" aria-sort={providerAccountSortDirection('loadFactor')}>
      <button class="inline-flex items-center gap-1 text-left font-medium hover:text-[#0d0d0d]" type="button" onclick={() => setProviderAccountSort('loadFactor')}>
        Load factor<span class="text-[11px]">{sortIndicator('loadFactor')}</span>
      </button>
    </th>
    <th class="w-44 px-4 py-3 font-medium" aria-sort={providerAccountSortDirection('expires')}>
      <button class="inline-flex items-center gap-1 text-left font-medium hover:text-[#0d0d0d]" type="button" onclick={() => setProviderAccountSort('expires')}>
        Token expiry<span class="text-[11px]">{sortIndicator('expires')}</span>
      </button>
    </th>
    <th class="w-44 px-4 py-3 font-medium" aria-sort={providerAccountSortDirection('refresh')}>
      <button class="inline-flex items-center gap-1 text-left font-medium hover:text-[#0d0d0d]" type="button" onclick={() => setProviderAccountSort('refresh')}>
        Last refresh<span class="text-[11px]">{sortIndicator('refresh')}</span>
      </button>
    </th>
    <th class="w-44 px-4 py-3 font-medium" aria-sort={providerAccountSortDirection('used')}>
      <button class="inline-flex items-center gap-1 text-left font-medium hover:text-[#0d0d0d]" type="button" onclick={() => setProviderAccountSort('used')}>
        Last used<span class="text-[11px]">{sortIndicator('used')}</span>
      </button>
    </th>
    <th class="w-72 px-4 py-3 font-medium">Manual models</th>
    <th class="sticky right-0 z-10 w-36 bg-[#f5f5f5] px-3 py-3 text-right font-medium shadow-[-8px_0_12px_rgba(255,255,255,0.85)]">Actions</th>
  </tr>
</thead>
<tbody class="divide-y divide-[#ededed]">
  {#if providerAccounts.loading}
    <tr>
      <td class="px-4 py-5 text-[#6e6e6e]" colspan="12">Loading provider accounts...</td>
    </tr>
  {:else if providerAccounts.items.length === 0}
    <tr>
      <td class="px-4 py-5 text-[#6e6e6e]" colspan="12">No provider accounts connected yet.</td>
    </tr>
  {:else if filteredProviderAccounts.length === 0}
    <tr>
      <td class="px-4 py-5 text-[#6e6e6e]" colspan="12">No accounts match your search.</td>
    </tr>
  {:else}
    {#each filteredProviderAccounts as account}
      {@const modelState = getAccountModelsState(account.id)}
      {@const historyState = getAccountTestResultsState(account.id)}
      {@const enabledModels = enabledAccountModelCount(modelState.items)}
      <tr class="bg-white align-top">
        <td class="px-4 py-3 align-middle">
          <label class="inline-flex items-center">
            <input
              class="size-4 rounded border-[#d9d9d9] text-[#10a37f] focus:ring-[#10a37f] disabled:cursor-not-allowed disabled:opacity-60"
              type="checkbox"
              checked={Boolean(selectedProviderAccountIds[account.id])}
              disabled={providerAccounts.saving}
              onchange={(event) => toggleProviderAccountSelection(account.id, event.currentTarget.checked)}
            />
            <span class="sr-only">Select {accountLabel(account)}</span>
          </label>
        </td>
        <td class="px-4 py-3 align-middle" title={accountHoverDetail(account)}>
          <label class="sr-only" for={`provider-account-name-${account.id}`}>
            Account name for {accountLabel(account)}
          </label>
          <input
            id={`provider-account-name-${account.id}`}
            class="w-full max-w-[18rem] rounded-md border border-transparent bg-transparent px-0 py-1 text-sm font-medium text-[#0d0d0d] outline-none hover:border-[#e5e5e5] hover:bg-white hover:px-2 focus:border-[#10a37f] focus:bg-white focus:px-2 focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
            value={accountLabel(account)}
            disabled={providerAccounts.saving}
            aria-label={`Rename ${accountLabel(account)}`}
            onchange={(event) => updateProviderAccountName(account, event)}
          />
          {#if accountSecondaryLabel(account)}
            <p class="mt-1 max-w-[18rem] truncate text-[#3c3c3c]">{accountSecondaryLabel(account)}</p>
          {/if}
          <p class="mt-1 max-w-[18rem] truncate font-mono text-[13px] text-[#6e6e6e]">
            {accountProviderDetail(account)}
          </p>
          {#if account.accountType === 'api_upstream'}
            <form
              class="mt-3 grid max-w-[18rem] gap-2"
              onsubmit={(event) => updateAPIUpstreamCredential(account, event)}
            >
              <label class="grid gap-1 text-xs font-medium text-[#3c3c3c]">
                Base URL
                <input
                  class="w-full rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-[12px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
                  name="baseUrl"
                  type="url"
                  value={account.baseUrl || ''}
                  placeholder="https://api.openai.com/v1"
                  disabled={providerAccounts.saving}
                />
              </label>
              <div class="grid grid-cols-[minmax(0,1fr)_auto] gap-2">
                <label class="grid min-w-0 gap-1 text-xs font-medium text-[#3c3c3c]">
                  API key
                  <input
                    class="w-full rounded-md border border-[#e5e5e5] bg-white px-2 py-1.5 text-xs text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
                    name="apiKey"
                    type="password"
                    autocomplete="off"
                    placeholder="Leave blank to keep current key"
                    disabled={providerAccounts.saving}
                  />
                </label>
                <button
                  class="self-end rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
                  type="submit"
                  disabled={providerAccounts.saving}
                >
                  Save upstream
                </button>
              </div>
            </form>
          {/if}
        </td>
        <td class="px-4 py-3 align-middle">
          <span class="inline-flex whitespace-nowrap rounded-full bg-[#f5f5f5] px-2.5 py-1 text-xs font-medium text-[#3c3c3c]">
            {accountTypeLabel(account)}
          </span>
        </td>
        <td class="px-4 py-3 align-middle" title={statusHoverDetail(account)}>
          <span
            class={[
              'inline-flex max-w-full whitespace-nowrap rounded-full px-2.5 py-1 text-xs font-medium capitalize',
              account.status === 'active' || !account.status
                ? 'bg-[#e8f5f0] text-[#0a7a5e]'
                : account.status === 'disabled'
                  ? 'bg-[#f5f5f5] text-[#6e6e6e]'
                  : 'bg-amber-50 text-amber-700'
            ]}
          >
            {statusLabel(account.status)}
          </span>
          {#if account.rateLimitedUntil}
            <p class="mt-1 max-w-[11rem] truncate text-xs text-amber-700">
              Rate limited {futureTimeRemainingLabel(account.rateLimitedUntil) || `until ${formatDate(account.rateLimitedUntil)}`}
            </p>
          {:else if account.circuitOpenUntil}
            <p class="mt-1 max-w-[11rem] truncate text-xs text-amber-700">
              Circuit {futureTimeRemainingLabel(account.circuitOpenUntil) || `until ${formatDate(account.circuitOpenUntil)}`}
            </p>
          {/if}
          {#if account.lastTestAt}
            <p class="mt-1 max-w-[11rem] truncate text-xs text-[#6e6e6e]">
              Test {account.lastTestStatus || 'checked'} {formatDate(account.lastTestAt)}
            </p>
            {#if account.lastTestError}
              <p class="mt-1 max-w-[11rem] truncate text-xs text-amber-700">{account.lastTestError}</p>
            {/if}
          {/if}
        </td>
        <td class="px-4 py-3 align-middle">
          <label class="inline-flex items-center gap-2 text-sm font-medium text-[#3c3c3c]" title={account.enabled ? 'Enabled' : 'Disabled'}>
            <input
              class="peer sr-only"
              type="checkbox"
              role="switch"
              checked={account.enabled}
              disabled={providerAccounts.saving}
              aria-label={`Set ${accountLabel(account)} ${account.enabled ? 'disabled' : 'enabled'}`}
              onchange={(event) =>
                updateProviderAccount(account, {
                  enabled: event.currentTarget.checked
                })}
            />
            <span class="relative inline-flex h-5 w-9 shrink-0 rounded-full bg-[#d9d9d9] transition-colors after:absolute after:left-0.5 after:top-0.5 after:size-4 after:rounded-full after:bg-white after:shadow-sm after:transition-transform peer-checked:bg-[#10a37f] peer-checked:after:translate-x-4 peer-focus-visible:outline peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-[#10a37f] peer-disabled:cursor-not-allowed peer-disabled:opacity-60"></span>
            <span class="w-14 text-xs text-[#6e6e6e]">{account.enabled ? 'On' : 'Off'}</span>
          </label>
        </td>
        <td class="px-4 py-3 align-middle">
          <label class="sr-only" for={`provider-account-priority-${account.id}`}>
            Priority for {accountLabel(account)}
          </label>
          <input
            id={`provider-account-priority-${account.id}`}
            class="w-24 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
            type="number"
            min="0"
            step="1"
            value={account.priority}
            disabled={providerAccounts.saving}
            onchange={(event) => updateProviderAccountPriority(account, event)}
          />
        </td>
        <td class="px-4 py-3 align-middle">
          <label class="sr-only" for={`provider-account-load-factor-${account.id}`}>
            Load factor for {accountLabel(account)}
          </label>
          <input
            id={`provider-account-load-factor-${account.id}`}
            class="w-24 rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
            type="number"
            min="1"
            max="100"
            step="1"
            value={account.loadFactor || 1}
            disabled={providerAccounts.saving}
            onchange={(event) => updateProviderAccountLoadFactor(account, event)}
          />
        </td>
        <td class="whitespace-nowrap px-4 py-3 align-middle text-[#3c3c3c]">{formatDate(account.accessTokenExpiresAt)}</td>
        <td class="whitespace-nowrap px-4 py-3 align-middle text-[#3c3c3c]">{formatDate(account.lastRefreshAt)}</td>
        <td class="whitespace-nowrap px-4 py-3 align-middle text-[#3c3c3c]">{formatDate(account.lastUsedAt)}</td>
        <td class="px-4 py-3 align-top">
          <form
            class="grid gap-2"
            onsubmit={(event) => {
              event.preventDefault();
              saveAccountModels(account.id, modelState.text);
            }}
          >
            <div class="flex flex-wrap items-center justify-between gap-2">
              <p class="text-xs font-medium text-[#3c3c3c]">
                {modelState.loading ? 'Loading models' : `${enabledModels} enabled model${enabledModels === 1 ? '' : 's'}`}
              </p>
              <button
                class="rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
                type="submit"
                disabled={modelState.loading || modelState.saving}
              >
                {modelState.saving ? 'Saving' : 'Save'}
              </button>
            </div>
            <label class="sr-only" for={`provider-account-models-${account.id}`}>
              Manual models for {accountLabel(account)}
            </label>
            <textarea
              id={`provider-account-models-${account.id}`}
              class="min-h-16 w-full resize-y rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] leading-5 text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0] disabled:cursor-not-allowed disabled:bg-[#f5f5f5] disabled:text-[#9b9b9b]"
              placeholder={'gpt-4.1\ngpt-4.1-mini'}
              bind:value={modelState.text}
              disabled={modelState.loading || modelState.saving}
            ></textarea>
            <p class="text-xs text-[#6e6e6e]">Add new models, one per line. Existing rows keep their enabled state.</p>
            {#if modelState.items.length > 0}
              <div class="grid max-h-44 gap-1 overflow-y-auto rounded-lg border border-[#ededed] bg-[#fafafa] p-2">
                {#each modelState.items as configuredModel (configuredModel.model)}
                  <div class="grid grid-cols-[minmax(0,1fr)_auto_auto] items-center gap-2">
                    <label class="inline-flex min-w-0 items-center gap-2 text-xs text-[#3c3c3c]">
                      <input
                        class="size-4 shrink-0 rounded border-[#d9d9d9] text-[#10a37f] focus:ring-[#10a37f] disabled:cursor-not-allowed disabled:opacity-60"
                        type="checkbox"
                        checked={configuredModel.enabled}
                        disabled={modelState.loading || modelState.saving}
                        aria-label={`${configuredModel.enabled ? 'Disable' : 'Enable'} ${configuredModel.model}`}
                        onchange={(event) => {
                          modelState.items = setAccountModelEnabled(
                            modelState.items,
                            configuredModel.model,
                            event.currentTarget.checked
                          );
                          modelState.saved = false;
                        }}
                      />
                      <span class="truncate font-mono text-[13px] text-[#0d0d0d]">{configuredModel.model}</span>
                    </label>
                    <span class="w-12 text-xs text-[#6e6e6e]">{configuredModel.enabled ? 'On' : 'Off'}</span>
                    <button
                      class="rounded-md border border-[#e5e5e5] bg-white px-2 py-1 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
                      type="button"
                      disabled={modelState.loading || modelState.saving}
                      onclick={() => {
                        modelState.items = removeAccountModel(modelState.items, configuredModel.model);
                        modelState.saved = false;
                      }}
                    >
                      Remove
                    </button>
                  </div>
                {/each}
              </div>
            {/if}
            {#if !modelState.loading && enabledModels === 0}
              <p class="rounded-md border border-amber-200 bg-amber-50 p-2 text-xs text-amber-700">
                This account cannot receive model-routed POST traffic until at least one enabled model is saved.
              </p>
            {/if}
            {#if modelState.saved}
              <p class="text-xs text-[#0a7a5e]">Saved.</p>
            {/if}
            {#if modelState.error}
              <p class="text-xs text-red-700">{modelState.error}</p>
            {/if}
          </form>
        </td>
        <td class="sticky right-0 bg-white px-3 py-3 align-middle shadow-[-8px_0_12px_rgba(255,255,255,0.85)]">
          <div class="flex justify-end gap-1.5 whitespace-nowrap">
            <button
              class="inline-flex size-8 items-center justify-center rounded-md border border-[#e5e5e5] bg-white text-sm font-semibold text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
              type="button"
              disabled={providerAccounts.saving}
              onclick={() => testProviderAccount(account)}
              title="Test account"
              aria-label="Test account"
            >
              <span aria-hidden="true">T</span>
              <span class="sr-only">Test account</span>
            </button>
            <button
              class="inline-flex size-8 items-center justify-center rounded-md border border-[#e5e5e5] bg-white text-sm font-semibold text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
              type="button"
              disabled={providerAccounts.saving}
              onclick={() => toggleAccountTestHistory(account.id)}
              title="Test history"
              aria-label="Test history"
            >
              <span aria-hidden="true">H</span>
              <span class="sr-only">Test history</span>
            </button>
            <button
              class="inline-flex size-8 items-center justify-center rounded-md border border-[#e5e5e5] bg-white text-sm font-semibold text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
              type="button"
              disabled={providerAccounts.saving}
              onclick={() => pauseProviderAccount(account)}
              title="Pause scheduling"
              aria-label="Pause scheduling"
            >
              <span aria-hidden="true">P</span>
              <span class="sr-only">Pause scheduling</span>
            </button>
            <button
              class="inline-flex size-8 items-center justify-center rounded-md border border-[#e5e5e5] bg-white text-sm font-semibold text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
              type="button"
              disabled={providerAccounts.saving || !isCodexOAuthAccount(account)}
              onclick={() => refreshProviderAccount(account)}
              title="Refresh account"
              aria-label="Refresh account"
            >
              <span aria-hidden="true">R</span>
              <span class="sr-only">Refresh account</span>
            </button>
            <button
              class="inline-flex size-8 items-center justify-center rounded-md border border-[#e5e5e5] bg-white text-sm font-semibold text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
              type="button"
              disabled={providerAccounts.saving || (!account.rateLimitedUntil && !account.circuitOpenUntil && !account.lastError)}
              onclick={() => resetProviderAccountStatus(account)}
              title="Reset local status"
              aria-label="Reset local status"
            >
              <span aria-hidden="true">S</span>
              <span class="sr-only">Reset local status</span>
            </button>
            <button
              class="inline-flex size-8 items-center justify-center rounded-md border border-[#e5e5e5] bg-white text-sm font-semibold text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
              type="button"
              disabled={provider.connecting || providerAccounts.saving || !isCodexOAuthAccount(account)}
              onclick={() => connectProvider(account)}
              title="Reauthorize account"
              aria-label="Reauthorize account"
            >
              <span aria-hidden="true">A</span>
              <span class="sr-only">Reauthorize account</span>
            </button>
            <button
              class="inline-flex size-8 items-center justify-center rounded-md border border-red-200 bg-white text-sm font-semibold text-red-700 hover:bg-red-50 disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
              type="button"
              disabled={providerAccounts.saving}
              onclick={() => disconnectProviderAccount(account)}
              title="Disconnect account"
              aria-label="Disconnect account"
            >
              <span aria-hidden="true">D</span>
              <span class="sr-only">Disconnect account</span>
            </button>
          </div>
        </td>
      </tr>
      {#if historyState.expanded}
        <tr class="bg-[#fafafa]">
          <td class="px-4 py-4" colspan="12">
            <div class="rounded-lg border border-[#ededed] bg-white p-4">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <h3 class="text-sm font-semibold text-[#0d0d0d]">Recent test history</h3>
                {#if historyState.loading}
                  <span class="text-xs text-[#6e6e6e]">Loading test history...</span>
                {/if}
              </div>
              {#if historyState.error}
                <p class="mt-3 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">{historyState.error}</p>
              {:else if !historyState.loading && historyState.items.length === 0}
                <p class="mt-3 text-sm text-[#6e6e6e]">No test history recorded yet.</p>
              {:else if historyState.items.length > 0}
                <div class="mt-3 overflow-x-auto rounded-lg border border-[#ededed]">
                  <table class="w-full min-w-[560px] text-left text-sm">
                    <thead class="border-b border-[#e5e5e5] bg-[#f5f5f5] text-[#6e6e6e]">
                      <tr>
                        <th class="px-3 py-2 font-medium">Checked</th>
                        <th class="px-3 py-2 font-medium">Status</th>
                        <th class="px-3 py-2 font-medium">Message</th>
                        <th class="px-3 py-2 font-medium">Recorded</th>
                      </tr>
                    </thead>
                    <tbody class="divide-y divide-[#ededed]">
                      {#each historyState.items as result (result.id)}
                        <tr>
                          <td class="whitespace-nowrap px-3 py-2 text-[#3c3c3c]">{formatDate(result.checkedAt)}</td>
                          <td class="px-3 py-2">
                            <span class={['inline-flex rounded-full px-2 py-0.5 text-xs font-medium', testResultStatusClass(result.status)]}>
                              {result.status || 'unknown'}
                            </span>
                          </td>
                          <td class="max-w-[28rem] px-3 py-2 text-[#3c3c3c]">{result.message || 'No message'}</td>
                          <td class="whitespace-nowrap px-3 py-2 text-[#6e6e6e]">{formatDate(result.createdAt)}</td>
                        </tr>
                      {/each}
                    </tbody>
                  </table>
                </div>
              {/if}
            </div>
          </td>
        </tr>
      {/if}
    {/each}
  {/if}
</tbody>
    </table>
  </div>
</section>

{/if}
