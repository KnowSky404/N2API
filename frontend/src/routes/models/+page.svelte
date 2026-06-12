<script>
  import {
    loadModelSettings,
    login,
    loginForm,
    modelSettings,
    saveModelSettings,
    session,
  } from '$lib/admin-state.svelte.js';
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
onclick={loadModelSettings}
    >
{modelSettings.loading ? 'Refreshing' : 'Refresh'}
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

{/if}
