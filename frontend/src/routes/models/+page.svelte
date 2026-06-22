<script>
  import {
    loadModelSettings,
    loadModelRouting,
    login,
    loginForm,
    modelRouting,
    modelSettings,
    saveModelSettings,
    session,
  } from '$lib/admin-state.svelte.js';

  async function refreshModelsPage() {
    await Promise.all([loadModelSettings(), loadModelRouting()]);
  }
</script>

<svelte:head>
  <title>N2API Models</title>
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
<h2 class="text-2xl font-semibold leading-tight text-[#0d0d0d]">Model settings</h2>
<p class="mt-1 text-sm text-[#6e6e6e]">
  Default model and allowed model names for this gateway.
</p>
    </div>
    <button
class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
type="button"
disabled={modelSettings.loading}
onclick={refreshModelsPage}
    >
{modelSettings.loading || modelRouting.loading ? 'Refreshing' : 'Refresh'}
    </button>
  </div>

  <form class="mt-6 grid gap-4 lg:grid-cols-[minmax(220px,320px)_minmax(0,1fr)]" onsubmit={saveModelSettings}>
    <label class="block text-sm font-medium text-[#3c3c3c]">
Default model
<input
  class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] leading-6 text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
  bind:value={modelSettings.defaultModel}
  maxlength="128"
  placeholder="gpt-4.1"
  required
/>
    </label>

    <label class="block text-sm font-medium text-[#3c3c3c]">
Allowed models
<textarea
  class="mt-2 min-h-36 w-full resize-y rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] leading-6 text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
  bind:value={modelSettings.allowedModelsText}
  placeholder={'gpt-4.1\ngpt-4.1-mini'}
  required
></textarea>
    </label>

    <div class="lg:col-span-2 flex flex-wrap items-center justify-between gap-3">
<p class="text-sm text-[#6e6e6e]">
  Put one model name per line. The default model must also appear in the allowed list.
</p>
<button
  class="rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white disabled:cursor-not-allowed disabled:opacity-60"
  disabled={modelSettings.loading || modelSettings.saving}
>
  {modelSettings.saving ? 'Saving' : 'Save settings'}
</button>
    </div>
  </form>

  {#if modelSettings.saved}
    <p class="mt-4 rounded-md border border-[#cbe7dd] bg-[#e8f5f0] p-3 text-sm text-[#0a7a5e]">
Model settings saved.
    </p>
  {/if}

  {#if modelSettings.error}
    <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">
{modelSettings.error}
    </p>
  {/if}
</section>

<section class="mt-6 rounded-lg border border-[#ededed] bg-white p-6">
  <div class="flex flex-wrap items-center justify-between gap-4">
    <div>
<h2 class="text-xl font-semibold leading-tight text-[#0d0d0d]">Model routing status</h2>
<p class="mt-1 text-sm text-[#6e6e6e]">
  Aggregate availability across allowed models and connected accounts.
</p>
    </div>
    <button
class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
type="button"
disabled={modelRouting.loading}
onclick={loadModelRouting}
    >
{modelRouting.loading ? 'Refreshing' : 'Refresh routing'}
    </button>
  </div>

  {#if modelRouting.error}
    <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">
{modelRouting.error}
    </p>
  {/if}

  {#if modelRouting.warnings.length > 0}
    <div class="mt-4 rounded-md border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800">
      <p class="font-medium">Warnings</p>
      <ul class="mt-2 grid gap-1">
        {#each modelRouting.warnings as warning}
          <li>{warning}</li>
        {/each}
      </ul>
    </div>
  {/if}

  <div class="mt-5 overflow-x-auto rounded-lg border border-[#ededed]">
    <table class="w-full min-w-[760px] text-left text-sm">
<thead class="border-b border-[#e5e5e5] bg-[#f5f5f5] text-[#6e6e6e]">
  <tr>
    <th class="px-4 py-3 font-medium">Model</th>
    <th class="w-32 px-4 py-3 font-medium">Visibility</th>
    <th class="w-40 px-4 py-3 text-right font-medium">Configured accounts</th>
    <th class="w-36 px-4 py-3 text-right font-medium">Enabled accounts</th>
    <th class="w-56 px-4 py-3 font-medium">Warnings</th>
  </tr>
</thead>
<tbody class="divide-y divide-[#ededed]">
  {#if modelRouting.loading && modelRouting.models.length === 0}
    <tr>
      <td class="px-4 py-5 text-[#6e6e6e]" colspan="5">Loading model routing...</td>
    </tr>
  {:else if modelRouting.models.length === 0}
    <tr>
      <td class="px-4 py-5 text-[#6e6e6e]" colspan="5">No routing status loaded yet.</td>
    </tr>
  {:else}
    {#each modelRouting.models as row}
      <tr class="bg-white">
        <td class="px-4 py-3 font-mono text-[13px] text-[#0d0d0d]">{row.model}</td>
        <td class="px-4 py-3">
          <span
            class={[
              'inline-flex rounded-full px-2.5 py-1 text-xs font-medium',
              row.allowed ? 'bg-[#e8f5f0] text-[#0a7a5e]' : 'bg-[#f5f5f5] text-[#6e6e6e]'
            ]}
          >
            {row.allowed ? 'Allowed' : 'Hidden'}
          </span>
        </td>
        <td class="px-4 py-3 text-right tabular-nums text-[#3c3c3c]">{row.configuredCount}</td>
        <td class="px-4 py-3 text-right tabular-nums text-[#3c3c3c]">{row.enabledCount}</td>
        <td class="px-4 py-3 text-[#6e6e6e]">
          {#if row.allowed && row.enabledCount === 0}
            <span class="text-amber-700">No enabled account</span>
          {:else if !row.allowed}
            <span class="text-[#6e6e6e]">Hidden from /v1/models</span>
          {:else}
            <span>Ready</span>
          {/if}
        </td>
      </tr>
    {/each}
  {/if}
</tbody>
    </table>
  </div>
</section>

{/if}
