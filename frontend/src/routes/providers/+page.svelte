<script>
  import {
    accountLabel,
    completeProviderCallback,
    connectProvider,
    copyAuthorizationURL,
    copySecret,
    createKey,
    disconnectProviderAccount,
    formatDate,
    getProviderStateLabel,
    loadProviderAccounts,
    login,
    loginForm,
    provider,
    providerAccounts,
    providerConnectForm,
    providerOAuth,
    refreshProviderAccount,
    session,
    statusLabel,
    updateProviderAccount,
    updateProviderAccountPriority
  } from '$lib/admin-state.svelte.js';

  const providerStateLabel = $derived(getProviderStateLabel());
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
<p class="mt-1 text-sm text-[#6e6e6e]">OpenAI/Codex account pool</p>
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

  <div class="mt-5 flex flex-wrap items-center justify-between gap-3">
    <p class="text-sm text-[#6e6e6e]">
Last refresh: {formatDate(provider.data?.lastRefreshAt)}
    </p>
    <div class="flex flex-wrap gap-2">
<button
  class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
  type="button"
  disabled={providerAccounts.loading}
  onclick={loadProviderAccounts}
>
  {providerAccounts.loading ? 'Refreshing' : 'Refresh'}
</button>
<button
  class="rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white disabled:cursor-not-allowed disabled:opacity-60"
  type="button"
  disabled={provider.loading || !provider.data?.configured || provider.connecting}
  onclick={() => connectProvider()}
>
  {provider.connecting ? 'Generating link' : 'Connect account'}
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

  <div class="mt-6 overflow-x-auto rounded-lg border border-[#ededed]">
    <table class="w-full min-w-[980px] text-left text-sm">
<thead class="border-b border-[#e5e5e5] bg-[#f5f5f5] text-[#6e6e6e]">
  <tr>
    <th class="px-4 py-3 font-medium">Account</th>
    <th class="px-4 py-3 font-medium">Status</th>
    <th class="px-4 py-3 font-medium">Enabled</th>
    <th class="px-4 py-3 font-medium">Priority</th>
    <th class="px-4 py-3 font-medium">Token expiry</th>
    <th class="px-4 py-3 font-medium">Last refresh</th>
    <th class="px-4 py-3 font-medium">Last used</th>
    <th class="px-4 py-3 text-right font-medium">Action</th>
  </tr>
</thead>
<tbody class="divide-y divide-[#ededed]">
  {#if providerAccounts.loading}
    <tr>
      <td class="px-4 py-5 text-[#6e6e6e]" colspan="8">Loading provider accounts...</td>
    </tr>
  {:else if providerAccounts.items.length === 0}
    <tr>
      <td class="px-4 py-5 text-[#6e6e6e]" colspan="8">No provider accounts connected yet.</td>
    </tr>
  {:else}
    {#each providerAccounts.items as account}
      <tr class="bg-white align-top">
        <td class="px-4 py-3">
          <p class="font-medium text-[#0d0d0d]">
            {accountLabel(account)}
          </p>
          {#if account.displayName && account.displayName !== accountLabel(account)}
            <p class="mt-1 text-[#3c3c3c]">{account.displayName}</p>
          {/if}
          <p class="mt-1 font-mono text-[13px] text-[#6e6e6e]">
            {account.subject || account.provider}
          </p>
          {#if account.lastError}
            <p class="mt-2 rounded-md border border-red-200 bg-red-50 p-2 text-sm text-red-700">
              {account.lastError}
              {#if account.lastErrorAt}
                <span class="text-red-600"> - {formatDate(account.lastErrorAt)}</span>
              {/if}
            </p>
          {/if}
        </td>
        <td class="px-4 py-3">
          <span
            class={[
              'inline-flex rounded-full px-2.5 py-1 text-xs font-medium capitalize',
              account.status === 'active' || !account.status
                ? 'bg-[#e8f5f0] text-[#0a7a5e]'
                : account.status === 'disabled'
                  ? 'bg-[#f5f5f5] text-[#6e6e6e]'
                  : 'bg-amber-50 text-amber-700'
            ]}
          >
            {statusLabel(account.status)}
          </span>
          {#if account.statusReason}
            <p class="mt-2 text-xs text-[#6e6e6e]">{account.statusReason}</p>
          {/if}
          {#if account.circuitOpenUntil}
            <p class="mt-2 text-xs text-amber-700">Circuit until {formatDate(account.circuitOpenUntil)}</p>
          {/if}
          {#if account.rateLimitedUntil}
            <p class="mt-2 text-xs text-amber-700">Rate limited until {formatDate(account.rateLimitedUntil)}</p>
          {/if}
        </td>
        <td class="px-4 py-3">
          <label class="inline-flex items-center gap-2 text-sm font-medium text-[#3c3c3c]">
            <input
              class="size-4 rounded border-[#e5e5e5] text-[#10a37f] focus:ring-[#10a37f]"
              type="checkbox"
              checked={account.enabled}
              disabled={providerAccounts.saving}
              onchange={(event) =>
                updateProviderAccount(account, {
                  enabled: event.currentTarget.checked
                })}
            />
            {account.enabled ? 'Enabled' : 'Disabled'}
          </label>
        </td>
        <td class="px-4 py-3">
          <label class="sr-only" for={`provider-account-priority-${account.id}`}>
            Priority for {account.displayName || account.subject || account.provider}
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
        <td class="px-4 py-3 text-[#3c3c3c]">{formatDate(account.accessTokenExpiresAt)}</td>
        <td class="px-4 py-3 text-[#3c3c3c]">{formatDate(account.lastRefreshAt)}</td>
        <td class="px-4 py-3 text-[#3c3c3c]">{formatDate(account.lastUsedAt)}</td>
        <td class="px-4 py-3">
          <div class="flex justify-end gap-2">
            <button
              class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-1.5 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
              type="button"
              disabled={providerAccounts.saving}
              onclick={() => refreshProviderAccount(account)}
            >
              Refresh
            </button>
            <button
              class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-1.5 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
              type="button"
              disabled={provider.connecting || providerAccounts.saving}
              onclick={() => connectProvider(account)}
            >
              Reauthorize
            </button>
            <button
              class="rounded-lg border border-red-200 bg-white px-3 py-1.5 text-sm font-medium text-red-700 hover:bg-red-50 disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
              type="button"
              disabled={providerAccounts.saving}
              onclick={() => disconnectProviderAccount(account)}
            >
              {providerAccounts.saving ? 'Saving' : 'Disconnect'}
            </button>
          </div>
        </td>
      </tr>
    {/each}
  {/if}
</tbody>
    </table>
  </div>
</section>

{/if}
