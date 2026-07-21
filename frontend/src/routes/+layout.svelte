<script>
  import { onMount } from 'svelte';
  import { page } from '$app/state';
  import '../app.css';
  import {
    adminSessions,
    health,
    formatDate,
    getProviderStateLabel,
    initializeAdminState,
    changePassword,
    changePasswordForm,
    loadAdminSessions,
    logout,
    revokeAdminSession,
    revokeOtherAdminSessions,
    session
  } from '$lib/admin-state.svelte.js';
  import {
    LayoutDashboard,
    Shield,
    Server,
    Shuffle,
    Key,
    ScrollText,
    History,
    BadgeDollarSign,
    Activity,
    Fingerprint,
    PanelLeftClose,
    PanelLeftOpen,
    CircleUser,
    ChevronDown,
    Lock,
    LogOut,
    LoaderCircle,
    Menu,
    MonitorSmartphone,
    X
  } from 'lucide-svelte';

  let { children } = $props();

  const navItems = [
    { href: '/', label: 'Dashboard', icon: LayoutDashboard },
    { href: '/gateway', label: 'Gateway', icon: Shield },
    { href: '/providers', label: 'Providers', icon: Server },
    { href: '/routing-pools', label: 'Routing pools', icon: Shuffle },
    { href: '/api-keys', label: 'API Keys', icon: Key },
    { href: '/request-logs', label: 'Request Logs', icon: ScrollText },
    { href: '/system-logs', label: 'System logs', icon: History },
    { href: '/pricing', label: 'Pricing', icon: BadgeDollarSign },
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
  let sessionsModalOpen = $state(false);
  let revokeOthersConfirm = $state(false);
  let revokeSessionConfirm = $state(/** @type {{ session: import('$lib/admin-state.svelte.js').AdminSession, top: number, left: number, width: number } | null} */ (null));
  let sessionNotice = $state(/** @type {string | null} */ (null));
  let mobileSidebarOpen = $state(false);
  let passwordInputEl = $state(/** @type {HTMLInputElement | null} */ (null));
  let sessionsCloseButtonEl = $state(/** @type {HTMLButtonElement | null} */ (null));
  let sessionsTriggerEl = $state(/** @type {HTMLButtonElement | null} */ (null));
  /** @type {ReturnType<typeof setTimeout> | null} */
  let sessionNoticeTimer = null;

  const otherSessionCount = $derived(adminSessions.items.filter((item) => !item.current).length);
  const sessionsMutationBusy = $derived(adminSessions.revokingId !== null || adminSessions.revokingOthers);

  function toggleSidebar() {
    sidebarCollapsed = !sidebarCollapsed;
  }

  function toggleUserDropdown() {
    userDropdownOpen = !userDropdownOpen;
  }

  function closeUserDropdown() {
    userDropdownOpen = false;
  }

  /** @param {MouseEvent} event */
  function openSessionsModal(event) {
    sessionsTriggerEl = /** @type {HTMLButtonElement} */ (event.currentTarget);
    userDropdownOpen = false;
    mobileSidebarOpen = false;
    revokeOthersConfirm = false;
    revokeSessionConfirm = null;
    adminSessions.error = '';
    sessionsModalOpen = true;
    void loadAdminSessions();
  }

  function closeSessionsModal() {
    if (sessionsMutationBusy || revokeOthersConfirm || revokeSessionConfirm) return;
    sessionsModalOpen = false;
    adminSessions.error = '';
    queueMicrotask(() => sessionsTriggerEl?.focus());
  }

  /**
   * @param {import('$lib/admin-state.svelte.js').AdminSession} target
   * @param {MouseEvent} event
   */
  function openSessionRevokeConfirm(target, event) {
    const rect = /** @type {HTMLElement} */ (event.currentTarget).getBoundingClientRect();
    const width = Math.min(288, window.innerWidth - 16);
    let left = rect.left + rect.width - width;
    if (left < 8) left = 8;
    if (left + width > window.innerWidth - 8) left = window.innerWidth - width - 8;
    const estimatedHeight = target.current ? 196 : 176;
    let top = rect.bottom + 8;
    if (top + estimatedHeight > window.innerHeight - 8) {
      top = Math.max(8, rect.top - estimatedHeight - 8);
    }
    revokeOthersConfirm = false;
    revokeSessionConfirm = { session: target, top, left, width };
  }

  function closeSessionRevokeConfirm() {
    if (sessionsMutationBusy) return;
    revokeSessionConfirm = null;
  }

  /** @param {string} message */
  function showSessionNotice(message) {
    sessionNotice = message;
    if (sessionNoticeTimer) clearTimeout(sessionNoticeTimer);
    sessionNoticeTimer = setTimeout(() => {
      sessionNotice = null;
      sessionNoticeTimer = null;
    }, 4000);
  }

  async function confirmSessionRevoke() {
    const target = revokeSessionConfirm?.session;
    if (!target) return;
    const revoked = await revokeAdminSession(target.id);
    if (!revoked) return;
    revokeSessionConfirm = null;
    if (target.current) {
      sessionsModalOpen = false;
      return;
    }
    showSessionNotice('Session revoked.');
  }

  async function confirmRevokeOtherSessions() {
    const revoked = await revokeOtherAdminSessions();
    if (revoked === null) return;
    revokeOthersConfirm = false;
    showSessionNotice(revoked === 1 ? '1 other session revoked.' : `${revoked} other sessions revoked.`);
  }

  /** @param {string | null | undefined} value @param {string} fallback */
  function sessionDate(value, fallback) {
    return value ? formatDate(value) : fallback;
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
    if (changePasswordForm.submitting) return;
    passwordModalOpen = false;
    changePasswordForm.currentPassword = '';
    changePasswordForm.newPassword = '';
    changePasswordForm.error = '';
    changePasswordForm.saved = false;
  }

  /** @param {SubmitEvent} event */
  async function handleChangePassword(event) {
    await changePassword(event);
  }

  /** @param {KeyboardEvent} e */
  function handleGlobalKeydown(e) {
    if (e.key !== 'Escape') return;
    if (revokeSessionConfirm && !sessionsMutationBusy) {
      revokeSessionConfirm = null;
      return;
    }
    if (revokeOthersConfirm && !sessionsMutationBusy) {
      revokeOthersConfirm = false;
      return;
    }
    if (sessionsModalOpen) {
      closeSessionsModal();
      return;
    }
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

  $effect(() => {
    if (sessionsModalOpen && sessionsCloseButtonEl) {
      sessionsCloseButtonEl.focus();
    }
  });

  $effect(() => {
    if (session.authenticated) return;
    userDropdownOpen = false;
    passwordModalOpen = false;
    sessionsModalOpen = false;
    revokeOthersConfirm = false;
    revokeSessionConfirm = null;
    mobileSidebarOpen = false;
  });

  onMount(() => {
    initializeAdminState();
    return () => {
      if (sessionNoticeTimer) clearTimeout(sessionNoticeTimer);
    };
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
            aria-haspopup="menu"
            aria-expanded={userDropdownOpen}
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
              aria-haspopup="menu"
              aria-expanded={userDropdownOpen}
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
                onclick={openSessionsModal}
              >
                <MonitorSmartphone class="h-4 w-4 text-[#8e8e8e]" />
                Active sessions
              </button>
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
      <div class="ui-content-shell mx-auto flex w-full flex-col gap-6 px-4 py-6 sm:px-6 sm:py-8">
        {@render children()}
      </div>
    </section>
  </div>
</main>

{#if sessionNotice}
  <div class="fixed right-4 top-4 z-[70] flex w-[min(22rem,calc(100vw-2rem))] items-start gap-3 rounded-lg border border-emerald-200 bg-white p-4 shadow-lg" role="status" aria-live="polite">
    <p class="min-w-0 flex-1 text-sm font-medium text-[#0d0d0d]">{sessionNotice}</p>
    <button
      class="ui-button ui-button--icon inline-flex size-7 shrink-0 items-center justify-center rounded-md text-[#6e6e6e] hover:bg-[#f5f5f5] hover:text-[#0d0d0d]"
      type="button"
      onclick={() => (sessionNotice = null)}
      title="Dismiss notification"
      aria-label="Dismiss session notification"
    >
      <X class="size-4" aria-hidden="true" />
    </button>
  </div>
{/if}

<!-- User dropdown: rendered at top level so it works on both desktop and mobile -->
{#if userDropdownOpen}
  <!-- svelte-ignore a11y_click_events_have_key_events,a11y_no_static_element_interactions -->
  <div class="fixed inset-0 z-40" onclick={closeUserDropdown}></div>
  <div class="fixed right-4 top-14 z-50 min-w-[180px] rounded-lg border border-[#ededed] bg-white py-1 shadow-[0_4px_16px_rgba(13,13,13,0.06)] lg:bottom-14 lg:left-4 lg:right-auto lg:top-auto" role="menu">
    <div class="border-b border-[#ededed] px-3 py-2">
      <p class="text-sm font-medium text-[#0d0d0d]">{session.username || 'admin'}</p>
      <p class="text-xs text-[#8e8e8e]">Signed in</p>
    </div>
    <button
      class="ui-button ui-button--md ui-button--start flex w-full items-center gap-2.5 px-3 py-2 text-left text-sm text-[#3c3c3c] hover:bg-[#f5f5f5]"
      onclick={openSessionsModal}
      role="menuitem"
    >
      <MonitorSmartphone class="h-4 w-4 text-[#6e6e6e]" />
      Active sessions
    </button>
    <button
      class="ui-button ui-button--md ui-button--start flex w-full items-center gap-2.5 px-3 py-2 text-left text-sm text-[#3c3c3c] hover:bg-[#f5f5f5]"
      onclick={openPasswordModal}
      role="menuitem"
    >
      <Lock class="h-4 w-4 text-[#6e6e6e]" />
      Change password
    </button>
    <button
      class="ui-button ui-button--md ui-button--start flex w-full items-center gap-2.5 px-3 py-2 text-left text-sm text-[#3c3c3c] hover:bg-[#f5f5f5]"
      onclick={logout}
      role="menuitem"
    >
      <LogOut class="h-4 w-4 text-[#6e6e6e]" />
      Sign out
    </button>
  </div>
{/if}

<!-- Active sessions modal -->
{#if sessionsModalOpen && session.authenticated}
  <div
    class="ui-modal-backdrop ui-modal-backdrop--top"
    role="dialog"
    aria-modal="true"
    aria-labelledby="active-sessions-title"
    aria-describedby="active-sessions-description"
    tabindex="-1"
  >
    <section class="ui-modal-panel ui-modal-panel--xl max-h-[calc(100dvh-2rem)] overflow-y-auto">
      <header class="flex items-start justify-between gap-4">
        <div class="min-w-0">
          <div class="flex flex-wrap items-center gap-2">
            <h2 id="active-sessions-title" class="text-lg font-semibold text-[#0d0d0d]">Active sessions</h2>
            {#if adminSessions.loading && adminSessions.items.length > 0}
              <span class="ui-loading-state text-xs"><LoaderCircle class="size-3.5 animate-spin" aria-hidden="true" /> Refreshing</span>
            {/if}
          </div>
          <p id="active-sessions-description" class="mt-1 text-sm text-[#6e6e6e]">Browsers and devices currently signed in to this admin account.</p>
        </div>
        <button
          class="ui-button ui-button--icon shrink-0"
          type="button"
          bind:this={sessionsCloseButtonEl}
          aria-label="Close active sessions"
          disabled={sessionsMutationBusy || revokeOthersConfirm || Boolean(revokeSessionConfirm)}
          onclick={closeSessionsModal}
        >
          <X class="size-4" aria-hidden="true" />
        </button>
      </header>

      {#if adminSessions.error}
        <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700" role="alert">{adminSessions.error}</p>
      {/if}

      <div class="ui-table-shell mt-5">
        <table class="ui-table ui-table--stacked sm:table-fixed">
          <colgroup class="hidden sm:table-column-group">
            <col class="w-[28%]" />
            <col class="w-[16%]" />
            <col class="w-[20%]" />
            <col class="w-[20%]" />
            <col class="w-[16%]" />
          </colgroup>
          <thead>
            <tr>
              <th>Client</th>
              <th>Created IP</th>
              <th>Last active</th>
              <th>Expires</th>
              <th class="text-right">Actions</th>
            </tr>
          </thead>
          <tbody>
            {#if adminSessions.loading && adminSessions.items.length === 0}
              <tr><td class="ui-table-empty ui-table-empty--loading" colspan="5">Loading active sessions...</td></tr>
            {:else if adminSessions.items.length === 0}
              <tr><td class="ui-table-empty" colspan="5">No active sessions.</td></tr>
            {:else}
              {#each adminSessions.items as adminSession (adminSession.id)}
                <tr>
                  <td data-label="Client" class="px-4 py-3 align-top">
                    <div class="min-w-0">
                      <div class="flex flex-wrap items-center gap-2">
                        <span class="break-words font-medium text-[#3c3c3c]">{adminSession.userAgent || 'Unknown client'}</span>
                        {#if adminSession.current}
                          <span class="inline-flex rounded-full bg-[#e8f5f0] px-2 py-0.5 text-xs font-medium text-[#0a7a5e]">Current</span>
                        {/if}
                      </div>
                      <p class="mt-0.5 text-xs text-[#8e8e8e]">Created {sessionDate(adminSession.createdAt, 'Unavailable')}</p>
                    </div>
                  </td>
                  <td data-label="Created IP" class="px-4 py-3 align-top"><span class="break-all font-mono text-xs text-[#3c3c3c]">{adminSession.createdIp || 'Unavailable'}</span></td>
                  <td data-label="Last active" class="whitespace-normal px-4 py-3 align-top text-xs tabular-nums">{sessionDate(adminSession.lastUsedAt, 'Not recorded')}</td>
                  <td data-label="Expires" class="whitespace-normal px-4 py-3 align-top text-xs tabular-nums">{sessionDate(adminSession.expiresAt, 'Unavailable')}</td>
                  <td data-label="Actions" class="px-4 py-3 text-right align-top">
                    <button
                      class="ui-button ui-button--sm ui-button--danger inline-flex items-center gap-1.5"
                      type="button"
                      disabled={sessionsMutationBusy}
                      onclick={(event) => openSessionRevokeConfirm(adminSession, event)}
                    >
                      <LogOut class="size-3.5" aria-hidden="true" />
                      {adminSession.current ? 'Sign out' : 'Revoke'}
                    </button>
                  </td>
                </tr>
              {/each}
            {/if}
          </tbody>
        </table>
      </div>

      {#if revokeOthersConfirm}
        <div class="mt-5 border-t border-[#ededed] pt-4">
          <p class="text-sm font-medium text-[#0d0d0d]">Revoke {otherSessionCount} other {otherSessionCount === 1 ? 'session' : 'sessions'}?</p>
          <p class="mt-1 text-sm text-[#6e6e6e]">This browser stays signed in. Every other session will need to authenticate again.</p>
          <div class="ui-modal-actions mt-4">
            <button class="ui-button ui-button--sm ui-button--secondary" type="button" disabled={sessionsMutationBusy} onclick={() => (revokeOthersConfirm = false)}>Cancel</button>
            <button class="ui-button ui-button--sm ui-button--danger-filled inline-flex items-center gap-1.5" type="button" disabled={sessionsMutationBusy} onclick={confirmRevokeOtherSessions}>
              {#if adminSessions.revokingOthers}<LoaderCircle class="size-3.5 animate-spin" aria-hidden="true" />{/if}
              {adminSessions.revokingOthers ? 'Revoking' : 'Revoke others'}
            </button>
          </div>
        </div>
      {:else}
        <div class="ui-modal-actions">
          <button class="ui-button ui-button--sm ui-button--secondary" type="button" disabled={sessionsMutationBusy} onclick={closeSessionsModal}>Close</button>
          {#if otherSessionCount > 0}
            <button class="ui-button ui-button--sm ui-button--danger" type="button" disabled={sessionsMutationBusy} onclick={() => (revokeOthersConfirm = true)}>Revoke other sessions</button>
          {/if}
        </div>
      {/if}
    </section>
  </div>
{/if}

{#if revokeSessionConfirm && sessionsModalOpen}
  <!-- svelte-ignore a11y_click_events_have_key_events,a11y_no_static_element_interactions -->
  <div class="fixed inset-0 z-[55]" onclick={closeSessionRevokeConfirm}></div>
  <div
    class="fixed z-[60] rounded-lg border border-[#ededed] bg-white p-4 shadow-[0_4px_16px_rgba(13,13,13,0.08)]"
    style="top: {revokeSessionConfirm.top}px; left: {revokeSessionConfirm.left}px; width: {revokeSessionConfirm.width}px;"
    role="alertdialog"
    aria-labelledby="revoke-session-confirm-title"
  >
    <p id="revoke-session-confirm-title" class="text-sm font-medium text-[#0d0d0d]">
      {revokeSessionConfirm.session.current ? 'Sign out this session?' : 'Revoke this session?'}
    </p>
    <p class="mt-1 break-words text-sm text-[#6e6e6e]">
      {revokeSessionConfirm.session.current
        ? 'This browser will return to the sign-in screen immediately.'
        : `${revokeSessionConfirm.session.userAgent || 'Unknown client'} will need to sign in again.`}
    </p>
    <div class="mt-3 flex flex-wrap justify-end gap-2">
      <button class="ui-button ui-button--sm ui-button--secondary" type="button" disabled={sessionsMutationBusy} onclick={closeSessionRevokeConfirm}>Cancel</button>
      <button class="ui-button ui-button--sm ui-button--danger-filled inline-flex items-center gap-1.5" type="button" disabled={sessionsMutationBusy} onclick={confirmSessionRevoke}>
        {#if adminSessions.revokingId === revokeSessionConfirm.session.id}<LoaderCircle class="size-3.5 animate-spin" aria-hidden="true" />{/if}
        {adminSessions.revokingId === revokeSessionConfirm.session.id
          ? 'Revoking'
          : revokeSessionConfirm.session.current ? 'Sign out now' : 'Revoke session'}
      </button>
    </div>
  </div>
{/if}

<!-- Password change modal -->
{#if passwordModalOpen}
  <div class="ui-modal-backdrop fixed inset-0 z-50 flex items-center justify-center bg-black/30" role="dialog" aria-modal="true" aria-labelledby="change-password-title">
    <div class="ui-modal-panel ui-modal-panel--sm w-full max-w-sm rounded-xl border border-[#ededed] bg-white p-6 shadow-[0_4px_16px_rgba(13,13,13,0.06)]">
      <div class="flex items-center justify-between gap-3">
        <h2 id="change-password-title" class="text-lg font-semibold text-[#0d0d0d]">Change password</h2>
        <button
          class="ui-button ui-button--icon flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-[#6e6e6e] hover:bg-[#f5f5f5] hover:text-[#0d0d0d] disabled:cursor-not-allowed disabled:opacity-60"
          type="button"
          aria-label="Close change password modal"
          disabled={changePasswordForm.submitting}
          onclick={closePasswordModal}
        >
          <X class="h-4 w-4" />
        </button>
      </div>
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
            class="ui-button ui-button--sm ui-button--secondary flex-1 rounded-md border border-[#e5e5e5] bg-white px-3 py-1.5 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:opacity-60"
            type="button"
            disabled={changePasswordForm.submitting}
            onclick={closePasswordModal}
          >
            Cancel
          </button>
          <button
            class="ui-button ui-button--sm ui-button--primary flex-1 rounded-md bg-[#0d0d0d] px-3 py-1.5 text-sm font-medium text-white disabled:cursor-not-allowed disabled:opacity-60"
            type="submit"
            disabled={changePasswordForm.submitting}
          >
            {changePasswordForm.submitting ? 'Saving...' : 'Save'}
          </button>
        </div>
      </form>
    </div>
  </div>
{/if}
