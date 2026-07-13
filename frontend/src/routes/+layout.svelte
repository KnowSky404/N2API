<script>
  import { onMount } from 'svelte';
  import { page } from '$app/state';
  import '../app.css';
  import {
    health,
    getProviderStateLabel,
    initializeAdminState,
    changePassword,
    changePasswordForm,
    logout,
    session
  } from '$lib/admin-state.svelte.js';
  import {
    LayoutDashboard,
    Shield,
    Server,
    Shuffle,
    Key,
    ScrollText,
    Activity,
    Fingerprint,
    PanelLeftClose,
    PanelLeftOpen,
    CircleUser,
    ChevronDown,
    Lock,
    LogOut,
    Menu
  } from 'lucide-svelte';

  let { children } = $props();

  const navItems = [
    { href: '/', label: 'Dashboard', icon: LayoutDashboard },
    { href: '/gateway', label: 'Gateway', icon: Shield },
    { href: '/providers', label: 'Providers', icon: Server },
    { href: '/routing-pools', label: 'Routing pools', icon: Shuffle },
    { href: '/api-keys', label: 'API Keys', icon: Key },
    { href: '/request-logs', label: 'Request Logs', icon: ScrollText },
    { href: '/ops', label: 'Ops', icon: Activity },
    { href: '/fingerprints', label: 'Fingerprints', icon: Fingerprint }
  ];

  const activePath = $derived(page.url.pathname);
  const shellStatus = $derived(health.loading ? 'Checking' : health.error ? 'Degraded' : 'Online');
  const providerStateLabel = $derived(getProviderStateLabel());

  /** @param {string} href */
  function isActive(href) {
    return href === '/' ? activePath === '/' : activePath.startsWith(href);
  }

  let sidebarCollapsed = $state(false);
  let userDropdownOpen = $state(false);
  let passwordModalOpen = $state(false);
  let mobileSidebarOpen = $state(false);
  let passwordInputEl = $state(/** @type {HTMLInputElement | null} */ (null));

  function toggleSidebar() {
    sidebarCollapsed = !sidebarCollapsed;
  }

  function toggleUserDropdown() {
    userDropdownOpen = !userDropdownOpen;
  }

  function closeUserDropdown() {
    userDropdownOpen = false;
  }

  function openPasswordModal() {
    userDropdownOpen = false;
    changePasswordForm.currentPassword = '';
    changePasswordForm.newPassword = '';
    changePasswordForm.error = '';
    changePasswordForm.saved = false;
    passwordModalOpen = true;
  }

  function closePasswordModal() {
    passwordModalOpen = false;
    changePasswordForm.currentPassword = '';
    changePasswordForm.newPassword = '';
    changePasswordForm.error = '';
    changePasswordForm.saved = false;
  }

  /** @param {SubmitEvent} event */
  async function handleChangePassword(event) {
    await changePassword(event);
    if (!changePasswordForm.error) {
      setTimeout(() => closePasswordModal(), 800);
    }
  }

  /** @param {KeyboardEvent} e */
  function handleGlobalKeydown(e) {
    if (e.key !== 'Escape') return;
    if (passwordModalOpen) {
      closePasswordModal();
      return;
    }
    if (userDropdownOpen) {
      closeUserDropdown();
      return;
    }
    if (mobileSidebarOpen) {
      mobileSidebarOpen = false;
      return;
    }
  }

  // Focus the password input when modal opens
  $effect(() => {
    if (passwordModalOpen && passwordInputEl) {
      passwordInputEl.focus();
    }
  });

  onMount(() => {
    initializeAdminState();
  });
</script>

<svelte:window onkeydown={handleGlobalKeydown} />

<main class="min-h-screen bg-white text-[#0d0d0d]">
  <div class="flex min-h-screen">
    <!-- Desktop sidebar -->
    <aside
      class="sticky top-0 hidden h-screen shrink-0 flex-col border-r border-[#ededed] bg-[#f9f9f9] transition-all duration-200 lg:flex"
      class:w-56={!sidebarCollapsed}
      class:w-14={sidebarCollapsed}
    >
      <!-- Header area -->
      <div class="flex items-center gap-2 px-3 py-3" class:px-2={sidebarCollapsed}>
        {#if !sidebarCollapsed}
          <div class="min-w-0">
            <p class="text-sm font-semibold text-[#6e6e6e]">N2API</p>
          </div>
        {/if}
        <button
          class="ui-button ui-button--icon flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-[#8e8e8e] hover:bg-[#e8e8e8] hover:text-[#3c3c3c]"
          class:ml-auto={!sidebarCollapsed}
          class:mx-auto={sidebarCollapsed}
          onclick={toggleSidebar}
          aria-label={sidebarCollapsed ? 'Expand sidebar' : 'Collapse sidebar'}
        >
          {#if sidebarCollapsed}
            <PanelLeftOpen class="h-4 w-4" />
          {:else}
            <PanelLeftClose class="h-4 w-4" />
          {/if}
        </button>
      </div>

      <!-- Navigation -->
      <nav class="flex flex-col gap-0.5 px-2" class:px-1.5={sidebarCollapsed}>
        {#each navItems as item}
          <a
            class={[
              'group flex items-center gap-2.5 rounded-lg px-3 py-2 text-sm font-medium transition',
              sidebarCollapsed ? 'justify-center px-0' : '',
              isActive(item.href)
                ? 'bg-[#f0f0f0] text-[#0d0d0d]'
                : 'text-[#8e8e8e] hover:bg-[#f0f0f0] hover:text-[#3c3c3c]'
            ]}
            href={item.href}
            title={sidebarCollapsed ? item.label : undefined}
          >
            <item.icon class="h-4.5 w-4.5 shrink-0" />
            {#if !sidebarCollapsed}
              <span>{item.label}</span>
            {/if}
          </a>
        {/each}
      </nav>

      <!-- Status (collapsed: green dot only) -->
      <div class="mt-4 px-3" class:px-2={sidebarCollapsed}>
        {#if sidebarCollapsed}
          <div class="flex justify-center" title="{shellStatus} &middot; Provider: {providerStateLabel}">
            <span
              class={[
                'h-2 w-2 rounded-full',
                health.error ? 'bg-red-500' : 'bg-[#10a37f]'
              ]}
            ></span>
          </div>
        {:else}
          <div class="rounded-lg px-2 py-1.5 text-xs">
            <div class="flex items-center gap-2">
              <span
                class={[
                  'h-1.5 w-1.5 rounded-full shrink-0',
                  health.error ? 'bg-red-500' : 'bg-[#10a37f]'
                ]}
              ></span>
              <span class="text-[#8e8e8e]">{shellStatus}</span>
            </div>
          </div>
        {/if}
      </div>

      <!-- User area -->
      <div class="mt-auto border-t border-[#ededed] p-2" class:px-1.5={sidebarCollapsed}>
        {#if session.authenticated}
          <button
            class={[
              'flex w-full items-center gap-2 rounded-lg px-2 py-2 text-sm hover:bg-[#f0f0f0]',
              sidebarCollapsed ? 'justify-center px-0' : ''
            ]}
            onclick={toggleUserDropdown}
          >
            <div class="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-[#e8e8e8]">
              <CircleUser class="h-4 w-4 text-[#6e6e6e]" />
            </div>
            {#if !sidebarCollapsed}
              <span class="truncate text-sm font-medium text-[#3c3c3c]">{session.username || 'admin'}</span>
              <span class="ml-auto transition-transform" class:rotate-180={userDropdownOpen}>
                <ChevronDown class="h-3.5 w-3.5 text-[#8e8e8e]" />
              </span>
            {/if}
          </button>
        {:else}
          <div class={sidebarCollapsed ? 'flex justify-center' : 'px-2'}>
            {#if sidebarCollapsed}
              <CircleUser class="h-4 w-4 text-[#8e8e8e]" />
            {:else}
              <p class="text-sm text-[#8e8e8e]">Sign in required</p>
            {/if}
          </div>
        {/if}
      </div>
    </aside>

    <!-- Main content area -->
    <section class="min-w-0 flex-1">
      <!-- Mobile header bar -->
      <header
        class="sticky top-0 z-10 flex items-center justify-between border-b border-[#ededed] bg-white/95 px-4 py-3 backdrop-blur lg:hidden"
      >
        <div class="flex items-center gap-2 min-w-0">
          <button
            class="ui-button ui-button--icon flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-[#6e6e6e] hover:bg-[#f5f5f5] hover:text-[#0d0d0d]"
            onclick={() => (mobileSidebarOpen = !mobileSidebarOpen)}
            aria-label="Toggle menu"
          >
            {#if mobileSidebarOpen}
              <PanelLeftClose class="h-4 w-4" />
            {:else}
              <Menu class="h-4 w-4" />
            {/if}
          </button>
          <div class="min-w-0">
            <p class="text-sm font-semibold text-[#6e6e6e]">N2API</p>
          </div>
        </div>
        <div class="flex shrink-0 items-center gap-2">
          {#if session.authenticated}
            <button
              class="ui-button ui-button--md flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium text-[#3c3c3c] hover:bg-[#f5f5f5]"
              onclick={toggleUserDropdown}
            >
              <CircleUser class="h-4 w-4 text-[#6e6e6e]" />
              <span class="hidden sm:inline-block max-w-[120px] truncate">{session.username || 'admin'}</span>
            </button>
          {/if}
        </div>
      </header>

      <!-- Mobile sidebar overlay -->
      {#if mobileSidebarOpen}
        <!-- svelte-ignore a11y_click_events_have_key_events,a11y_no_static_element_interactions -->
        <div class="fixed inset-0 z-20 bg-black/30 lg:hidden" onclick={() => (mobileSidebarOpen = false)}></div>
        <aside class="fixed left-0 top-0 z-30 flex h-full w-56 flex-col bg-[#f9f9f9] border-r border-[#ededed] lg:hidden">
          <div class="flex items-center gap-2 px-3 py-3">
            <div class="min-w-0">
              <p class="text-sm font-semibold text-[#6e6e6e]">N2API</p>
            </div>
            <button
              class="ui-button ui-button--icon ml-auto flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-[#8e8e8e] hover:bg-[#e8e8e8] hover:text-[#3c3c3c]"
              onclick={() => (mobileSidebarOpen = false)}
              aria-label="Close menu"
            >
              <PanelLeftClose class="h-4 w-4" />
            </button>
          </div>

          <nav class="flex flex-col gap-0.5 px-2">
            {#each navItems as item}
              <a
                class={[
                  'flex items-center gap-2.5 rounded-lg px-3 py-2 text-sm font-medium transition',
                  isActive(item.href)
                    ? 'bg-[#f0f0f0] text-[#0d0d0d]'
                    : 'text-[#8e8e8e] hover:bg-[#f0f0f0] hover:text-[#3c3c3c]'
                ]}
                href={item.href}
                onclick={() => (mobileSidebarOpen = false)}
              >
                <item.icon class="h-4.5 w-4.5 shrink-0" />
                <span>{item.label}</span>
              </a>
            {/each}
          </nav>

          <div class="px-3 pt-3">
            <div class="flex items-center gap-2 rounded-lg px-2 py-1.5 text-xs">
              <span
                class={[
                  'h-1.5 w-1.5 rounded-full shrink-0',
                  health.error ? 'bg-red-500' : 'bg-[#10a37f]'
                ]}
              ></span>
              <span class="text-[#8e8e8e]">{shellStatus}</span>
            </div>
          </div>

          <div class="mt-auto border-t border-[#ededed] p-2">
            {#if session.authenticated}
              <div class="px-2 py-2">
                <div class="flex items-center gap-2">
                  <div class="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-[#e8e8e8]">
                    <CircleUser class="h-4 w-4 text-[#6e6e6e]" />
                  </div>
                  <div>
                    <p class="text-sm font-medium text-[#3c3c3c]">{session.username || 'admin'}</p>
                    <p class="text-xs text-[#8e8e8e]">Signed in</p>
                  </div>
                </div>
              </div>
              <button
                class="ui-button ui-button--md ui-button--start flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-sm text-[#3c3c3c] hover:bg-[#f0f0f0]"
                onclick={openPasswordModal}
              >
                <Lock class="h-4 w-4 text-[#8e8e8e]" />
                Change password
              </button>
              <button
                class="ui-button ui-button--md ui-button--start flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-sm text-[#3c3c3c] hover:bg-[#f0f0f0]"
                onclick={logout}
              >
                <LogOut class="h-4 w-4 text-[#8e8e8e]" />
                Sign out
              </button>
            {:else}
              <p class="px-2 text-sm text-[#8e8e8e]">Sign in required</p>
            {/if}
          </div>
        </aside>
      {/if}

      <!-- Page content container -->
      <div class="mx-auto flex w-full max-w-6xl flex-col gap-6 px-4 py-6 sm:px-6 sm:py-8">
        {@render children()}
      </div>
    </section>
  </div>
</main>

<!-- User dropdown: rendered at top level so it works on both desktop and mobile -->
{#if userDropdownOpen}
  <!-- svelte-ignore a11y_click_events_have_key_events,a11y_no_static_element_interactions -->
  <div class="fixed inset-0 z-40" onclick={closeUserDropdown}></div>
  <div class="fixed z-50 min-w-[160px] rounded-lg border border-[#ededed] bg-white py-1 shadow-[0_4px_16px_rgba(13,13,13,0.06)]" style="bottom: 3.5rem; left: 1rem;">
    <div class="border-b border-[#ededed] px-3 py-2">
      <p class="text-sm font-medium text-[#0d0d0d]">{session.username || 'admin'}</p>
      <p class="text-xs text-[#8e8e8e]">Signed in</p>
    </div>
    <button
      class="ui-button ui-button--md ui-button--start flex w-full items-center gap-2.5 px-3 py-2 text-left text-sm text-[#3c3c3c] hover:bg-[#f5f5f5]"
      onclick={openPasswordModal}
    >
      <Lock class="h-4 w-4 text-[#6e6e6e]" />
      Change password
    </button>
    <button
      class="ui-button ui-button--md ui-button--start flex w-full items-center gap-2.5 px-3 py-2 text-left text-sm text-[#3c3c3c] hover:bg-[#f5f5f5]"
      onclick={logout}
    >
      <LogOut class="h-4 w-4 text-[#6e6e6e]" />
      Sign out
    </button>
  </div>
{/if}

<!-- Password change modal -->
{#if passwordModalOpen}
  <!-- svelte-ignore a11y_click_events_have_key_events,a11y_no_static_element_interactions -->
  <div class="ui-modal-backdrop fixed inset-0 z-50 flex items-center justify-center bg-black/30" role="dialog" aria-modal="true" aria-labelledby="change-password-title" tabindex="-1" onclick={(e) => e.target === e.currentTarget && closePasswordModal()} onkeydown={(e) => e.key === 'Escape' && closePasswordModal()}>
    <div class="ui-modal-panel ui-modal-panel--sm w-full max-w-sm rounded-xl border border-[#ededed] bg-white p-6 shadow-[0_4px_16px_rgba(13,13,13,0.06)]">
      <h2 id="change-password-title" class="text-lg font-semibold text-[#0d0d0d]">Change password</h2>
      <form class="mt-4" onsubmit={handleChangePassword}>
        <label class="block text-xs font-medium text-[#6e6e6e]">
          Current password
          <input
            class="mt-1.5 block w-full rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
            type="password"
            bind:value={changePasswordForm.currentPassword}
            bind:this={passwordInputEl}
            autocomplete="current-password"
          />
        </label>
        <label class="mt-3 block text-xs font-medium text-[#6e6e6e]">
          New password (min 8 chars)
          <input
            class="mt-1.5 block w-full rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
            type="password"
            bind:value={changePasswordForm.newPassword}
            autocomplete="new-password"
          />
        </label>
        {#if changePasswordForm.error}
          <p class="mt-3 text-xs text-red-700">{changePasswordForm.error}</p>
        {/if}
        {#if changePasswordForm.saved}
          <p class="mt-3 text-xs text-[#0a7a5e]">Password changed.</p>
        {/if}
        <div class="mt-4 flex gap-2">
          <button
            class="ui-button ui-button--sm ui-button--secondary flex-1 rounded-md border border-[#e5e5e5] bg-white px-3 py-1.5 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]"
            type="button"
            onclick={closePasswordModal}
          >
            Cancel
          </button>
          <button
            class="ui-button ui-button--sm ui-button--primary flex-1 rounded-md bg-[#0d0d0d] px-3 py-1.5 text-sm font-medium text-white disabled:cursor-not-allowed disabled:opacity-60"
            disabled={changePasswordForm.submitting}
          >
            {changePasswordForm.submitting ? 'Saving...' : 'Update'}
          </button>
        </div>
      </form>
    </div>
  </div>
{/if}
