<script>
  import { onMount } from 'svelte';

  /**
   * @typedef {object} APIKey
   * @property {number} id
   * @property {string} name
   * @property {string} prefix
   * @property {string} createdAt
   * @property {string | null} lastUsedAt
   * @property {string | null} revokedAt
   */

  let health = $state({
    loading: true,
    error: '',
    status: 'checking',
    database: 'checking'
  });

  let session = $state({ loading: true, authenticated: false, username: '', error: '' });
  let sessionVersion = $state(0);
  let loginForm = $state({ username: '', password: '', submitting: false, error: '' });
  let canCopySecret = $state(false);
  /** @type {{ loading: boolean, creating: boolean, error: string, items: APIKey[], newKeyName: string, oneTimeSecret: string }} */
  let apiKeys = $state({
    loading: false,
    creating: false,
    error: '',
    items: [],
    newKeyName: '',
    oneTimeSecret: ''
  });

  const statusItems = $derived([
    { label: 'Backend', value: health.status },
    { label: 'Database', value: health.database },
    { label: 'Provider', value: 'Codex/OpenAI OAuth planned' }
  ]);
  const activeKeys = $derived(apiKeys.items.filter((key) => !key.revokedAt));

  /**
   * @param {string} path
   * @param {RequestInit} options
   * @returns {Promise<any>}
   */
  async function requestJSON(path, options = {}) {
    const response = await fetch(path, {
      ...options,
      headers: {
        'Content-Type': 'application/json',
        ...(options.headers ?? {})
      }
    });
    if (!response.ok) {
      const payload = await response.json().catch(() => ({}));
      throw new Error(payload.error ?? `Request failed with ${response.status}`);
    }
    if (response.status === 204) return null;
    return response.json();
  }

  /** @param {string | null | undefined} value */
  function formatDate(value) {
    if (!value) return 'Never';
    return new Date(value).toLocaleString();
  }

  async function copySecret() {
    if (!apiKeys.oneTimeSecret || !navigator.clipboard) return;
    const version = sessionVersion;
    if (!isCurrentAuthenticated(version)) return;

    try {
      await navigator.clipboard.writeText(apiKeys.oneTimeSecret);
    } catch {
      if (!isCurrentAuthenticated(version)) return;
      apiKeys.error = 'Copy failed';
    }
  }

  function clearAPIKeys() {
    apiKeys = {
      loading: false,
      creating: false,
      error: '',
      items: [],
      newKeyName: '',
      oneTimeSecret: ''
    };
  }

  /** @param {number} version */
  function isCurrentAuthenticated(version) {
    return version === sessionVersion && session.authenticated;
  }

  async function loadHealth() {
    try {
      const response = await fetch('/api/admin/health');
      if (!response.ok) {
        throw new Error(`Health check failed with ${response.status}`);
      }
      const payload = await response.json();
      health = {
        loading: false,
        error: '',
        status: payload.status ?? 'unknown',
        database: payload.database ?? 'unknown'
      };
    } catch (error) {
      health = {
        loading: false,
        error: error instanceof Error ? error.message : 'Health check failed',
        status: 'unavailable',
        database: 'unknown'
      };
    }
  }

  async function loadSession() {
    const version = sessionVersion;
    session.loading = true;
    session.error = '';

    try {
      const response = await fetch('/api/admin/me');
      if (version !== sessionVersion) return;

      if (response.status === 401) {
        sessionVersion += 1;
        session = { loading: false, authenticated: false, username: '', error: '' };
        clearAPIKeys();
        return;
      }
      if (!response.ok) {
        const payload = await response.json().catch(() => ({}));
        throw new Error(payload.error ?? `Session check failed with ${response.status}`);
      }

      const payload = await response.json();
      if (version !== sessionVersion) return;

      sessionVersion += 1;
      session = {
        loading: false,
        authenticated: true,
        username: payload.username ?? '',
        error: ''
      };
      await loadKeys();
    } catch (error) {
      if (version !== sessionVersion) return;

      sessionVersion += 1;
      session = {
        loading: false,
        authenticated: false,
        username: '',
        error: error instanceof Error ? error.message : 'Session check failed'
      };
      clearAPIKeys();
    }
  }

  /** @param {SubmitEvent} event */
  async function login(event) {
    event.preventDefault();
    loginForm.submitting = true;
    loginForm.error = '';

    try {
      await requestJSON('/api/admin/login', {
        method: 'POST',
        body: JSON.stringify({ username: loginForm.username, password: loginForm.password })
      });
      loginForm.password = '';
      sessionVersion += 1;
      await loadSession();
    } catch (error) {
      loginForm.error = error instanceof Error ? error.message : 'Login failed';
    } finally {
      loginForm.submitting = false;
    }
  }

  async function logout() {
    sessionVersion += 1;
    await requestJSON('/api/admin/logout', { method: 'POST' }).catch(() => null);
    session = { loading: false, authenticated: false, username: '', error: '' };
    clearAPIKeys();
    loginForm.password = '';
  }

  async function loadKeys() {
    const version = sessionVersion;
    if (!isCurrentAuthenticated(version)) return;

    apiKeys.loading = true;
    apiKeys.error = '';

    try {
      const payload = await requestJSON('/api/admin/keys');
      if (!isCurrentAuthenticated(version)) return;
      apiKeys.items = payload.keys ?? [];
    } catch (error) {
      if (!isCurrentAuthenticated(version)) return;
      apiKeys.error = error instanceof Error ? error.message : 'Failed to load API keys';
    } finally {
      if (!isCurrentAuthenticated(version)) return;
      apiKeys.loading = false;
    }
  }

  /** @param {SubmitEvent} event */
  async function createKey(event) {
    event.preventDefault();
    const version = sessionVersion;
    if (!isCurrentAuthenticated(version)) return;

    apiKeys.creating = true;
    apiKeys.error = '';
    apiKeys.oneTimeSecret = '';

    try {
      const payload = await requestJSON('/api/admin/keys', {
        method: 'POST',
        body: JSON.stringify({ name: apiKeys.newKeyName })
      });
      if (!isCurrentAuthenticated(version)) return;
      apiKeys.items = [payload.key, ...apiKeys.items];
      apiKeys.oneTimeSecret = payload.secret;
      apiKeys.newKeyName = '';
    } catch (error) {
      if (!isCurrentAuthenticated(version)) return;
      apiKeys.error = error instanceof Error ? error.message : 'Failed to create API key';
    } finally {
      if (!isCurrentAuthenticated(version)) return;
      apiKeys.creating = false;
    }
  }

  /** @param {number} id */
  async function revokeKey(id) {
    const version = sessionVersion;
    if (!isCurrentAuthenticated(version)) return;

    apiKeys.error = '';

    try {
      const payload = await requestJSON(`/api/admin/keys/${id}/revoke`, { method: 'POST' });
      if (!isCurrentAuthenticated(version)) return;
      apiKeys.items = apiKeys.items.map((key) => (key.id === id ? payload.key : key));
    } catch (error) {
      if (!isCurrentAuthenticated(version)) return;
      apiKeys.error = error instanceof Error ? error.message : 'Failed to revoke API key';
    }
  }

  onMount(() => {
    canCopySecret = Boolean(navigator.clipboard);
    loadHealth();
    loadSession();
  });
</script>

<svelte:head>
  <title>N2API Admin</title>
</svelte:head>

<main class="min-h-screen bg-[#fafafa] text-[#0d0d0d]">
  <section class="mx-auto flex w-full max-w-6xl flex-col gap-8 px-4 py-8 sm:px-6">
    <header class="flex flex-wrap items-center justify-between gap-4 border-b border-[#e5e5e5] pb-6">
      <div>
        <p class="text-sm font-medium text-[#6e6e6e]">Personal AI Gateway</p>
        <h1 class="mt-1 text-[32px] font-semibold leading-[1.15] tracking-normal text-[#0d0d0d]">
          N2API
        </h1>
      </div>
      <div
        class={[
          'rounded-md border px-3 py-2 text-sm font-medium',
          health.error
            ? 'border-red-200 bg-red-50 text-red-700'
            : 'border-[#cbe7dd] bg-[#e8f5f0] text-[#0a7a5e]'
        ]}
      >
        {health.loading ? 'Checking' : health.error ? 'Degraded' : 'Online'}
      </div>
    </header>

    <div class="grid gap-4 md:grid-cols-3">
      {#each statusItems as item}
        <article class="rounded-lg border border-[#ededed] bg-white p-5">
          <p class="text-sm font-medium text-[#6e6e6e]">{item.label}</p>
          <p class="mt-2 text-lg font-semibold capitalize text-[#0d0d0d]">{item.value}</p>
        </article>
      {/each}
    </div>

    {#if health.error}
      <section class="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-800">
        {health.error}
      </section>
    {/if}

    {#if session.loading}
      <section class="rounded-lg border border-[#ededed] bg-white p-6 text-sm text-[#6e6e6e]">
        Loading admin session...
      </section>
    {:else if !session.authenticated}
      <section class="grid gap-6 lg:grid-cols-[minmax(0,1fr)_minmax(320px,420px)]">
        <div class="rounded-lg border border-[#ededed] bg-white p-6">
          <h2 class="text-2xl font-semibold leading-tight text-[#0d0d0d]">Admin access</h2>
          <p class="mt-3 max-w-2xl text-sm leading-6 text-[#3c3c3c]">
            Sign in to create and revoke OpenAI-compatible client keys for this personal
            gateway.
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
            <input
              class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-base text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
              bind:value={loginForm.username}
              autocomplete="username"
              required
            />
          </label>
          <label class="mt-4 block text-sm font-medium text-[#3c3c3c]">
            Password
            <input
              class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-base text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
              type="password"
              bind:value={loginForm.password}
              autocomplete="current-password"
              required
            />
          </label>
          {#if loginForm.error}
            <p class="mt-3 text-sm text-red-700">{loginForm.error}</p>
          {/if}
          <button
            class="mt-5 rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white disabled:cursor-not-allowed disabled:opacity-60"
            disabled={loginForm.submitting}
          >
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
          <button
            class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]"
            onclick={logout}
          >
            Logout
          </button>
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
              {#if canCopySecret}
                <button
                  class="rounded-lg border border-[#b7d9cd] bg-white px-3 py-1.5 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]"
                  type="button"
                  onclick={copySecret}
                >
                  Copy
                </button>
              {/if}
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
  </section>
</main>
