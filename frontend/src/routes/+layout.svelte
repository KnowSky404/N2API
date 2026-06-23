<script>
  import { onMount } from 'svelte';
  import { page } from '$app/state';
  import '../app.css';
  import {
    health,
    getProviderStateLabel,
    initializeAdminState,
    logout,
    session
  } from '$lib/admin-state.svelte.js';

  let { children } = $props();

  const navItems = [
    { href: '/', label: 'Dashboard' },
    { href: '/providers', label: 'Providers' },
    { href: '/models', label: 'Routing' },
    { href: '/api-keys', label: 'API Keys' },
    { href: '/request-logs', label: 'Request Logs' }
  ];

  const activePath = $derived(page.url.pathname);
  const shellStatus = $derived(health.loading ? 'Checking' : health.error ? 'Degraded' : 'Online');
  const providerStateLabel = $derived(getProviderStateLabel());

  /** @param {string} href */
  function isActive(href) {
    return href === '/' ? activePath === '/' : activePath.startsWith(href);
  }

  onMount(() => {
    initializeAdminState();
  });
</script>

<main class="min-h-screen bg-[#fafafa] text-[#0d0d0d]">
  <div class="flex min-h-screen">
    <aside
      class="sticky top-0 hidden h-screen w-64 shrink-0 flex-col border-r border-[#ededed] bg-white px-3 py-4 lg:flex"
    >
      <div class="px-2">
        <p class="text-sm font-medium text-[#6e6e6e]">Personal AI Gateway</p>
        <h1 class="mt-1 text-xl font-semibold leading-tight tracking-normal text-[#0d0d0d]">N2API</h1>
      </div>

      <nav class="mt-6 flex flex-col gap-1">
        {#each navItems as item}
          <a
            class={[
              'rounded-lg px-3 py-2 text-sm font-medium transition',
              isActive(item.href)
                ? 'bg-[#0d0d0d] text-white'
                : 'text-[#3c3c3c] hover:bg-[#f5f5f5] hover:text-[#0d0d0d]'
            ]}
            href={item.href}
          >
            {item.label}
          </a>
        {/each}
      </nav>

      <div class="mt-6 rounded-lg border border-[#ededed] bg-[#fafafa] p-3 text-sm">
        <div class="flex items-center justify-between gap-3">
          <span class="font-medium text-[#3c3c3c]">Status</span>
          <span
            class={[
              'rounded-full px-2 py-0.5 text-xs font-medium',
              health.error ? 'bg-red-50 text-red-700' : 'bg-[#e8f5f0] text-[#0a7a5e]'
            ]}
          >
            {shellStatus}
          </span>
        </div>
        <p class="mt-2 text-xs capitalize text-[#6e6e6e]">Provider: {providerStateLabel}</p>
      </div>

      <div class="mt-auto border-t border-[#ededed] pt-4">
        {#if session.authenticated}
          <div class="px-2">
            <p class="truncate text-sm font-medium text-[#0d0d0d]">{session.username || 'admin'}</p>
            <p class="text-xs text-[#6e6e6e]">Signed in</p>
          </div>
          <button
            class="mt-3 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-left text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]"
            type="button"
            onclick={logout}
          >
            Sign out
          </button>
        {:else}
          <p class="px-2 text-sm text-[#6e6e6e]">Sign in required</p>
        {/if}
      </div>
    </aside>

    <section class="min-w-0 flex-1">
      <header
        class="sticky top-0 z-10 flex flex-wrap items-center justify-between gap-3 border-b border-[#ededed] bg-white/95 px-4 py-3 backdrop-blur lg:hidden"
      >
        <div>
          <p class="text-sm font-medium text-[#6e6e6e]">Personal AI Gateway</p>
          <h1 class="text-xl font-semibold leading-tight tracking-normal text-[#0d0d0d]">N2API</h1>
        </div>
        {#if session.authenticated}
          <button
            class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d]"
            type="button"
            onclick={logout}
          >
            Sign out
          </button>
        {/if}
        <nav class="flex w-full gap-2 overflow-x-auto pb-1">
          {#each navItems as item}
            <a
              class={[
                'shrink-0 rounded-lg px-3 py-2 text-sm font-medium',
                isActive(item.href) ? 'bg-[#0d0d0d] text-white' : 'bg-[#f5f5f5] text-[#3c3c3c]'
              ]}
              href={item.href}
            >
              {item.label}
            </a>
          {/each}
        </nav>
      </header>

      <div class="mx-auto flex w-full max-w-6xl flex-col gap-8 px-4 py-8 sm:px-6">
        {@render children()}
      </div>
    </section>
  </div>
</main>
