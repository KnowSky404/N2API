<script>
  import {
    createFingerprintProfile,
    deleteFingerprintProfile,
    fingerprintProfiles,
    formatDate,
    loadFingerprintProfiles,
    session,
    updateFingerprintProfile,
  } from '$lib/admin-state.svelte.js';
  import { Plus } from 'lucide-svelte';

  import AuthGate from '$lib/AuthGate.svelte';

  const SYSTEM_DEFAULT_KEY = 'codex_cli_default';

  let requested = $state(false);
  let showForm = $state(false);
  let editingId = $state(/** @type {number|null} */ null);
  let selectedTemplate = $state('');

  let form = $state({
    name: '',
    description: '',
    userAgent: '',
    tlsFingerprint: '',
    headersText: '',
    enabled: true,
  });

  $effect(() => {
    if (!session.authenticated || requested) return;
    requested = true;
    loadFingerprintProfiles();
  });

  function blankForm() {
    return { name: '', description: '', userAgent: '', tlsFingerprint: '', headersText: '', enabled: true };
  }

  function systemDefaultProfile() {
    return fingerprintProfiles.items.find((profile) => profile.systemKey === SYSTEM_DEFAULT_KEY);
  }

  function nextTemplateName() {
    const names = new Set(fingerprintProfiles.items.map((profile) => profile.name));
    const base = 'Codex CLI custom';
    if (!names.has(base)) return base;
    let suffix = 2;
    while (names.has(`${base} ${suffix}`)) suffix += 1;
    return `${base} ${suffix}`;
  }

  /** @param {string} templateKey */
  function applyTemplate(templateKey) {
    selectedTemplate = templateKey;
    if (!templateKey) {
      form = blankForm();
      return;
    }
    const template = systemDefaultProfile();
    if (!template) {
      fingerprintProfiles.error = 'System default fingerprint profile is unavailable';
      selectedTemplate = '';
      form = blankForm();
      return;
    }
    fingerprintProfiles.error = '';
    form = {
      name: nextTemplateName(),
      description: `Custom profile based on ${template.name}.`,
      userAgent: template.userAgent || '',
      tlsFingerprint: template.tlsFingerprint || '',
      headersText: template.headers ? JSON.stringify(template.headers, null, 2) : '',
      enabled: true,
    };
  }

  function openCreateForm() {
    editingId = null;
    showForm = true;
    applyTemplate(systemDefaultProfile() ? SYSTEM_DEFAULT_KEY : '');
  }

  /** @param {any} fp */
  function edit(fp) {
    if (fp.systemKey) return;
    editingId = fp.id;
    selectedTemplate = '';
    form = {
      name: fp.name,
      description: fp.description || '',
      userAgent: fp.userAgent || '',
      tlsFingerprint: fp.tlsFingerprint || '',
      headersText: fp.headers ? JSON.stringify(fp.headers, null, 2) : '',
      enabled: fp.enabled,
    };
    showForm = true;
  }

  function resetForm() {
    editingId = null;
    selectedTemplate = '';
    form = blankForm();
    showForm = false;
  }

  /** @param {Event} e */
  async function handleSubmit(e) {
    e.preventDefault();
    let headers = /** @type {Record<string, string>} */ ({});
    try {
      if (form.headersText.trim()) {
        headers = JSON.parse(form.headersText);
      }
    } catch {
      fingerprintProfiles.error = 'Headers must be valid JSON';
      return;
    }

    const input = {
      name: form.name.trim(),
      description: form.description.trim(),
      userAgent: form.userAgent.trim(),
      tlsFingerprint: form.tlsFingerprint.trim(),
      headers,
      enabled: form.enabled,
    };

    let ok;
    if (editingId) {
      ok = await updateFingerprintProfile(editingId, input);
    } else {
      ok = await createFingerprintProfile(input);
    }
    if (ok) resetForm();
  }

  /** @param {number} id @param {string} name */
  async function handleDelete(id, name) {
    if (!confirm(`Delete fingerprint profile "${name}"?`)) return;
    await deleteFingerprintProfile(id);
  }
</script>

<svelte:head>
  <title>N2API Fingerprint Profiles</title>
</svelte:head>

<AuthGate>
  <div class="ui-page">
    <header class="ui-page-header">
      <div class="ui-page-heading">
        <h1 class="ui-page-title">Fingerprint profiles</h1>
        <p class="ui-page-description">TLS and User-Agent identities applied to provider connections.</p>
      </div>
      <div class="ui-page-actions">
        <button
          class="ui-button ui-button--sm ui-button--primary rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white"
          onclick={openCreateForm}
          disabled={fingerprintProfiles.loading}
        >
          <Plus class="size-4" aria-hidden="true" />
          New profile
        </button>
      </div>
    </header>

    {#if fingerprintProfiles.error}
      <section class="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700">
        {fingerprintProfiles.error}
      </section>
    {/if}

    <section class="rounded-lg border border-[#ededed] bg-white">
      <div class="border-b border-[#ededed] px-4 py-3">
        <h2 class="ui-section-title">System sending defaults</h2>
      </div>
      <div class="ui-table-shell overflow-x-auto">
        <table class="ui-table w-full min-w-[720px] text-sm">
          <thead>
            <tr class="border-b border-[#ededed] text-left text-xs font-medium text-[#6e6e6e]">
              <th class="px-4 py-3">Account type</th>
              <th class="px-4 py-3">Default identity</th>
              <th class="px-4 py-3">HTTP headers</th>
              <th class="px-4 py-3">TLS</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-[#ededed]">
            <tr>
              <td class="px-4 py-3 font-medium text-[#0d0d0d]">Codex OAuth</td>
              <td class="px-4 py-3 text-[#3c3c3c]">Default Codex CLI</td>
              <td class="px-4 py-3 text-[#3c3c3c]">Codex TUI headers</td>
              <td class="px-4 py-3 text-[#3c3c3c]">Default transport</td>
            </tr>
            <tr>
              <td class="px-4 py-3 font-medium text-[#0d0d0d]">API upstream</td>
              <td class="px-4 py-3 text-[#3c3c3c]">Default API upstream (pass-through)</td>
              <td class="px-4 py-3 text-[#3c3c3c]">Client headers; upstream bearer token</td>
              <td class="px-4 py-3 text-[#3c3c3c]">Default transport</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <!-- Form modal -->
    {#if showForm}
      <section class="rounded-lg border border-[#ededed] bg-white p-6">
        <h3 class="text-base font-semibold text-[#0d0d0d]">{editingId ? 'Edit' : 'New'} profile</h3>
        <form class="mt-4 space-y-4" onsubmit={handleSubmit}>
          {#if !editingId}
            <label class="block text-sm font-medium text-[#3c3c3c]">
              Template
              <select
                class="mt-1 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
                value={selectedTemplate}
                onchange={(event) => applyTemplate(event.currentTarget.value)}
              >
                <option value="">Blank profile</option>
                <option value={SYSTEM_DEFAULT_KEY}>System default · Codex CLI</option>
              </select>
            </label>
          {/if}
          <label class="block text-sm font-medium text-[#3c3c3c]">
            Name *
            <input class="mt-1 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={form.name} required />
          </label>
          <label class="block text-sm font-medium text-[#3c3c3c]">
            Description
            <input class="mt-1 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={form.description} />
          </label>
          <label class="block text-sm font-medium text-[#3c3c3c]">
            User-Agent
            <input class="mt-1 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={form.userAgent} placeholder="Mozilla/5.0 (X11; Linux x86_64)..." />
          </label>
          <label class="block text-sm font-medium text-[#3c3c3c]">
            TLS preset
            <input class="mt-1 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={form.tlsFingerprint} placeholder="chrome, firefox, safari, ios, android, edge, randomized" />
          </label>
          <label class="block text-sm font-medium text-[#3c3c3c]">
            Custom headers (JSON object)
            <textarea class="mt-1 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" rows="4" bind:value={form.headersText} ></textarea>
          </label>
          <label class="flex items-center gap-2 text-sm font-medium text-[#3c3c3c]">
            <input class="h-4 w-4 rounded border-[#d9d9d9] text-[#10a37f] focus:ring-[#10a37f]" type="checkbox" bind:checked={form.enabled} />
            Enabled
          </label>
          <div class="flex items-center gap-3">
            <button class="ui-button ui-button--sm ui-button--primary rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white disabled:opacity-60" disabled={fingerprintProfiles.saving} type="submit">
              {fingerprintProfiles.saving ? 'Saving' : editingId ? 'Save' : 'Create'}
            </button>
            <button class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#e5e5e5] bg-white px-4 py-2 text-sm font-medium text-[#0d0d0d]" type="button" onclick={resetForm}>
              Cancel
            </button>
          </div>
        </form>
      </section>
    {/if}

    <!-- Profiles list -->
    <section class="rounded-lg border border-[#ededed] bg-white">
      {#if fingerprintProfiles.loading}
        <p class="ui-loading-state p-6 text-sm text-[#6e6e6e]" aria-live="polite">Loading fingerprint profiles...</p>
      {:else if fingerprintProfiles.items.length === 0}
        <p class="p-6 text-sm text-[#6e6e6e]">No fingerprint profiles configured.</p>
      {:else}
        <div class="ui-table-shell overflow-x-auto">
          <table class="ui-table w-full text-sm">
            <thead>
              <tr class="border-b border-[#ededed] text-left text-xs font-medium text-[#6e6e6e]">
                <th class="px-4 py-3">Name</th>
                <th class="px-4 py-3">User-Agent</th>
                <th class="px-4 py-3">TLS</th>
                <th class="px-4 py-3">Headers</th>
                <th class="px-4 py-3">Status</th>
                <th class="px-4 py-3">Created</th>
                <th class="px-4 py-3 text-right">Actions</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-[#ededed]">
              {#each fingerprintProfiles.items as fp}
                <tr class="hover:bg-[#fafafa]">
                  <td class="px-4 py-3 align-top">
                    <div class="flex flex-wrap items-center gap-2">
                      <span class="font-medium text-[#0d0d0d]">{fp.name}</span>
                      {#if fp.systemKey}
                        <span class="rounded-full bg-[#e8f5f0] px-2 py-0.5 text-xs font-medium text-[#0a7a5e]">System default</span>
                      {/if}
                    </div>
                    {#if fp.description}
                      <p class="mt-1 max-w-[260px] text-xs leading-5 text-[#6e6e6e]">{fp.description}</p>
                    {/if}
                  </td>
                  <td class="max-w-[360px] break-all px-4 py-3 align-top font-mono text-[13px] leading-5 text-[#3c3c3c]">{fp.userAgent || '-'}</td>
                  <td class="px-4 py-3 align-top font-mono text-[13px] text-[#3c3c3c]">{fp.tlsFingerprint || 'Default transport'}</td>
                  <td class="px-4 py-3 align-top font-mono text-[13px] text-[#3c3c3c]">
                    {#if fp.headers && Object.keys(fp.headers).length}
                      <div class="space-y-1">
                        {#each Object.entries(fp.headers) as [key, value]}
                          <div><span class="text-[#6e6e6e]">{key}:</span> {value}</div>
                        {/each}
                      </div>
                    {:else}
                      -
                    {/if}
                  </td>
                  <td class="px-4 py-3">
                    <span class={fp.enabled ? 'rounded-full bg-[#e8f5f0] px-2 py-0.5 text-xs font-medium text-[#0a7a5e]' : 'rounded-full bg-[#f5f5f5] px-2 py-0.5 text-xs font-medium text-[#6e6e6e]'}>
                      {fp.enabled ? 'Enabled' : 'Disabled'}
                    </span>
                  </td>
                  <td class="px-4 py-3 text-[#6e6e6e]">{formatDate(fp.createdAt)}</td>
                  <td class="px-4 py-3 text-right align-top">
                    {#if fp.systemKey}
                      <span class="text-xs font-medium text-[#8e8e8e]">Managed by system</span>
                    {:else}
                      <button class="ui-button ui-button--sm rounded-md px-2.5 py-1.5 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]" onclick={() => edit(fp)}>Edit</button>
                      <button class="ui-button ui-button--sm ml-1 rounded-md px-2.5 py-1.5 text-sm font-medium text-red-700 hover:bg-red-50" onclick={() => handleDelete(fp.id, fp.name)}>Delete</button>
                    {/if}
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </section>
  </div>
</AuthGate>
