<script>
  import {
    apiKeys,
    copySecret,
    createKey,
    formatDate,
    getActiveKeys,
    login,
    loginForm,
    revokeKey,
    session,
  } from '$lib/admin-state.svelte.js';

  const activeKeys = $derived(getActiveKeys());
</script>

<svelte:head>
  <title>N2API API Keys</title>
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
<h2 class="text-2xl font-semibold leading-tight text-[#0d0d0d]">API keys</h2>
<p class="mt-1 text-sm text-[#6e6e6e]">
  Signed in as {session.username}. {activeKeys.length} active
  {activeKeys.length === 1 ? 'key' : 'keys'}.
</p>
    </div>
  </div>

  <form class="mt-6 flex flex-col gap-3 sm:flex-row" onsubmit={createKey}>
    <label class="min-w-0 flex-1">
<span class="text-sm font-medium text-[#3c3c3c]">New key name</span>
<input
  class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-base text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
  bind:value={apiKeys.newKeyName}
  placeholder="Codex workstation"
  required
/>
    </label>
    <div class="flex items-end">
<button
  class="w-full rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white disabled:cursor-not-allowed disabled:opacity-60 sm:w-auto"
  disabled={apiKeys.creating}
>
  {apiKeys.creating ? 'Creating' : 'Create key'}
</button>
    </div>
  </form>

  {#if apiKeys.oneTimeSecret}
    <div class="mt-5 rounded-lg border border-[#cbe7dd] bg-[#e8f5f0] p-4">
<div class="flex flex-wrap items-center justify-between gap-3">
  <p class="text-sm font-medium text-[#0a7a5e]">
    Copy this key now. It will not be shown again.
  </p>
  <button
    class="rounded-lg border border-[#b7d9cd] bg-white px-3 py-1.5 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]"
    type="button"
    onclick={copySecret}
  >
    Copy
  </button>
</div>
<code
  class="mt-3 block overflow-x-auto rounded-md bg-white px-3 py-2 font-mono text-[13px] leading-6 text-[#0d0d0d]"
>
  {apiKeys.oneTimeSecret}
</code>
    </div>
  {/if}

  {#if apiKeys.error}
    <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">
{apiKeys.error}
    </p>
  {/if}

  <div class="mt-6 overflow-x-auto rounded-lg border border-[#ededed]">
    <table class="w-full min-w-[760px] text-left text-sm">
<thead class="border-b border-[#e5e5e5] bg-[#f5f5f5] text-[#6e6e6e]">
  <tr>
    <th class="px-4 py-3 font-medium">Name</th>
    <th class="px-4 py-3 font-medium">Prefix</th>
    <th class="px-4 py-3 font-medium">Created</th>
    <th class="px-4 py-3 font-medium">Last used</th>
    <th class="px-4 py-3 font-medium">Status</th>
    <th class="px-4 py-3 text-right font-medium">Action</th>
  </tr>
</thead>
<tbody class="divide-y divide-[#ededed]">
  {#if apiKeys.loading}
    <tr>
      <td class="px-4 py-5 text-[#6e6e6e]" colspan="6">Loading API keys...</td>
    </tr>
  {:else if apiKeys.items.length === 0}
    <tr>
      <td class="px-4 py-5 text-[#6e6e6e]" colspan="6">No API keys created yet.</td>
    </tr>
  {:else}
    {#each apiKeys.items as key}
      <tr class="bg-white">
        <td class="px-4 py-3 font-medium text-[#0d0d0d]">{key.name}</td>
        <td class="px-4 py-3 font-mono text-[13px] text-[#3c3c3c]">{key.prefix}</td>
        <td class="px-4 py-3 text-[#3c3c3c]">{formatDate(key.createdAt)}</td>
        <td class="px-4 py-3 text-[#3c3c3c]">{formatDate(key.lastUsedAt)}</td>
        <td class="px-4 py-3">
          <span
            class={[
              'inline-flex rounded-full px-2.5 py-1 text-xs font-medium',
              key.revokedAt
                ? 'bg-red-50 text-red-700'
                : 'bg-[#e8f5f0] text-[#0a7a5e]'
            ]}
          >
            {key.revokedAt ? 'Revoked' : 'Active'}
          </span>
        </td>
        <td class="px-4 py-3 text-right">
          <button
            class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-1.5 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
            type="button"
            disabled={Boolean(key.revokedAt)}
            onclick={() => revokeKey(key.id)}
          >
            Revoke
          </button>
        </td>
      </tr>
    {/each}
  {/if}
</tbody>
    </table>
  </div>
</section>

{/if}
