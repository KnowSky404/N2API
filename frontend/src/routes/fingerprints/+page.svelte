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

  import AuthGate from '$lib/AuthGate.svelte';
  let requested = $state(false);
  let showForm = $state(false);
  let editingId = $state(/** @type {number|null} */ null);

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

  /** @param {any} fp */
  function edit(fp) {
    editingId = fp.id;
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
    form = { name: '', description: '', userAgent: '', tlsFingerprint: '', headersText: '', enabled: true };
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
  <div class="space-y-6">
    <!-- Header -->
    <section class="rounded-lg border border-[#ededed] bg-white p-6">
      <div class="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h2 class="text-2xl font-semibold leading-tight text-[#0d0d0d]">Fingerprint profiles</h2>
          <p class="mt-2 text-sm text-[#6e6e6e]">
            TLS and User-Agent fingerprint profiles for provider connection simulation.
          </p>
        </div>
        <button
          class="rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white"
          onclick={() => { resetForm(); showForm = true; }}
        >
          New profile
        </button>
      </div>
    </section>

    {#if fingerprintProfiles.error}
      <section class="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700">
        {fingerprintProfiles.error}
      </section>
    {/if}

    <!-- Form modal -->
    {#if showForm}
      <section class="rounded-lg border border-[#ededed] bg-white p-6">
        <h3 class="text-base font-semibold text-[#0d0d0d]">{editingId ? 'Edit' : 'New'} profile</h3>
        <form class="mt-4 space-y-4" onsubmit={handleSubmit}>
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
            <button class="rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white disabled:opacity-60" disabled={fingerprintProfiles.saving} type="submit">
              {fingerprintProfiles.saving ? 'Saving' : editingId ? 'Save' : 'Create'}
            </button>
            <button class="rounded-lg border border-[#e5e5e5] bg-white px-4 py-2 text-sm font-medium text-[#0d0d0d]" type="button" onclick={resetForm}>
              Cancel
            </button>
          </div>
        </form>
      </section>
    {/if}

    <!-- Profiles list -->
    <section class="rounded-lg border border-[#ededed] bg-white">
      {#if fingerprintProfiles.loading}
        <p class="p-6 text-sm text-[#6e6e6e]">Loading fingerprint profiles...</p>
      {:else if fingerprintProfiles.items.length === 0}
        <p class="p-6 text-sm text-[#6e6e6e]">No fingerprint profiles configured.</p>
      {:else}
        <div class="overflow-x-auto">
          <table class="w-full text-sm">
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
                  <td class="px-4 py-3 font-medium text-[#0d0d0d]">{fp.name}</td>
                  <td class="px-4 py-3 font-mono text-[13px] text-[#3c3c3c] max-w-[200px] truncate">{fp.userAgent || '-'}</td>
                  <td class="px-4 py-3 font-mono text-[13px] text-[#3c3c3c]">{fp.tlsFingerprint || '-'}</td>
                  <td class="px-4 py-3 font-mono text-[13px] text-[#3c3c3c]">{fp.headers && Object.keys(fp.headers).length ? Object.keys(fp.headers).length + ' entries' : '-'}</td>
                  <td class="px-4 py-3">
                    <span class={fp.enabled ? 'rounded-full bg-[#e8f5f0] px-2 py-0.5 text-xs font-medium text-[#0a7a5e]' : 'rounded-full bg-[#f5f5f5] px-2 py-0.5 text-xs font-medium text-[#6e6e6e]'}>
                      {fp.enabled ? 'Enabled' : 'Disabled'}
                    </span>
                  </td>
                  <td class="px-4 py-3 text-[#6e6e6e]">{formatDate(fp.createdAt)}</td>
                  <td class="px-4 py-3 text-right">
                    <button class="rounded-md px-2.5 py-1.5 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]" onclick={() => edit(fp)}>Edit</button>
                    <button class="ml-1 rounded-md px-2.5 py-1.5 text-sm font-medium text-red-700 hover:bg-red-50" onclick={() => handleDelete(fp.id, fp.name)}>Delete</button>
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
