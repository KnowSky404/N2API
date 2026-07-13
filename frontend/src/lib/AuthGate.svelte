<script>
  import { login, loginForm, session } from '$lib/admin-state.svelte.js';
  let { children } = $props();
</script>

{#if session.loading}
  <div class="flex min-h-[60vh] items-center justify-center">
    <p class="text-sm text-[#8e8e8e]">Loading&hellip;</p>
  </div>
{:else if !session.authenticated}
  <div class="flex min-h-[60vh] items-center justify-center">
    <div class="w-full max-w-sm">
      <div class="text-center">
        <h2 class="text-xl font-semibold text-[#0d0d0d]">N2API</h2>
        <p class="mt-2 text-sm text-[#6e6e6e]">Sign in to manage this personal gateway.</p>
      </div>
      {#if session.error}
        <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">
          {session.error}
        </p>
      {/if}
      <form class="mt-6" onsubmit={login}>
        <label class="block text-sm font-medium text-[#3c3c3c]">
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
        <button class="ui-button ui-button--md ui-button--primary mt-5 w-full rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white disabled:cursor-not-allowed disabled:opacity-60" disabled={loginForm.submitting}>
          {loginForm.submitting ? 'Signing in' : 'Sign in'}
        </button>
      </form>
    </div>
  </div>
{:else}
  {@render children()}
{/if}
