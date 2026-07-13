<script>
  import {
    createErrorPassthroughRule,
    deleteErrorPassthroughRule,
    errorPassthroughRules,
    formatDate,
    loadErrorPassthroughRules,
    session,
    updateErrorPassthroughRule,
  } from '$lib/admin-state.svelte.js';

  import AuthGate from '$lib/AuthGate.svelte';
  let requested = $state(false);
  let showForm = $state(false);
  let editingId = $state(/** @type {number|null} */ null);
  let form = $state({
    pattern: '',
    matchType: 'status_code',
    description: '',
    enabled: true,
    priority: 0,
  });

  const matchTypes = [
    { value: 'status_code', label: 'Status code' },
    { value: 'error_message', label: 'Error message' },
    { value: 'error_code', label: 'Error code' },
  ];

  $effect(() => {
    if (!session.authenticated || requested) return;
    requested = true;
    loadErrorPassthroughRules();
  });

  /** @param {any} rule */
  function edit(rule) {
    editingId = rule.id;
    form = { pattern: rule.pattern, matchType: rule.matchType, description: rule.description || '', enabled: rule.enabled, priority: rule.priority };
    showForm = true;
  }

  function resetForm() {
    editingId = null;
    form = { pattern: '', matchType: 'status_code', description: '', enabled: true, priority: 0 };
    showForm = false;
  }

  /** @param {Event} e */
  async function handleSubmit(e) {
    e.preventDefault();
    const input = { pattern: form.pattern.trim(), matchType: form.matchType, description: form.description.trim(), enabled: form.enabled, priority: form.priority };
    const ok = editingId ? await updateErrorPassthroughRule(editingId, input) : await createErrorPassthroughRule(input);
    if (ok) resetForm();
  }

  /** @param {number} id @param {string} pattern */
async function handleDelete(id, pattern) {
    if (!confirm(`Delete rule "${pattern}"?`)) return;
    await deleteErrorPassthroughRule(id);
  }

  /** @param {string} mt */
  function matchTypeLabel(mt) {
    const found = matchTypes.find((t) => t.value === mt);
    return found ? found.label : mt;
  }
</script>

<svelte:head>
  <title>N2API Error Passthrough</title>
</svelte:head>

<AuthGate>
  <div class="space-y-6">
    <section class="rounded-lg border border-[#ededed] bg-white p-6">
      <div class="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h2 class="text-2xl font-semibold text-[#0d0d0d]">Error passthrough rules</h2>
          <p class="mt-2 text-sm text-[#6e6e6e]">Define patterns for upstream error responses that should be passed through to clients.</p>
        </div>
        <button class="ui-button ui-button--sm ui-button--primary rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white" onclick={() => { resetForm(); showForm = true; }}>New rule</button>
      </div>
    </section>

    {#if errorPassthroughRules.error}
      <section class="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700">{errorPassthroughRules.error}</section>
    {/if}

    {#if showForm}
      <section class="rounded-lg border border-[#ededed] bg-white p-6">
        <h3 class="text-base font-semibold text-[#0d0d0d]">{editingId ? 'Edit' : 'New'} rule</h3>
        <form class="mt-4 space-y-4" onsubmit={handleSubmit}>
          <label class="block text-sm font-medium text-[#3c3c3c]">Pattern *
            <input class="mt-1 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={form.pattern} required placeholder="401 or quota_exceeded" />
          </label>
          <label class="block text-sm font-medium text-[#3c3c3c]">Match type
            <select class="mt-1 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={form.matchType}>
              {#each matchTypes as mt}
                <option value={mt.value}>{mt.label}</option>
              {/each}
            </select>
          </label>
          <label class="block text-sm font-medium text-[#3c3c3c]">Description
            <input class="mt-1 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={form.description} />
          </label>
          <div class="flex items-center gap-6">
            <label class="flex items-center gap-2 text-sm font-medium text-[#3c3c3c]">
              <input class="h-4 w-4 rounded border-[#d9d9d9] text-[#10a37f] focus:ring-[#10a37f]" type="checkbox" bind:checked={form.enabled} /> Enabled
            </label>
            <label class="flex items-center gap-2 text-sm font-medium text-[#3c3c3c]">Priority
              <input class="ml-1 w-20 rounded-lg border border-[#e5e5e5] bg-white px-2 py-1.5 font-mono text-[13px] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" type="number" min="0" bind:value={form.priority} />
            </label>
          </div>
          <div class="flex items-center gap-3">
            <button class="ui-button ui-button--sm ui-button--primary rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white disabled:opacity-60" disabled={errorPassthroughRules.saving} type="submit">{errorPassthroughRules.saving ? 'Saving' : editingId ? 'Save' : 'Create'}</button>
            <button class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#e5e5e5] bg-white px-4 py-2 text-sm font-medium text-[#0d0d0d]" type="button" onclick={resetForm}>Cancel</button>
          </div>
        </form>
      </section>
    {/if}

    <section class="rounded-lg border border-[#ededed] bg-white">
      {#if errorPassthroughRules.loading}
        <p class="ui-loading-state p-6 text-sm text-[#6e6e6e]" aria-live="polite">Loading...</p>
      {:else if errorPassthroughRules.items.length === 0}
        <p class="p-6 text-sm text-[#6e6e6e]">No error passthrough rules configured.</p>
      {:else}
        <div class="ui-table-shell overflow-x-auto">
          <table class="ui-table w-full text-sm">
            <thead>
              <tr class="border-b border-[#ededed] text-left text-xs font-medium text-[#6e6e6e]">
                <th class="px-4 py-3">Pattern</th>
                <th class="px-4 py-3">Match type</th>
                <th class="px-4 py-3">Description</th>
                <th class="px-4 py-3">Priority</th>
                <th class="px-4 py-3">Status</th>
                <th class="px-4 py-3 text-right">Actions</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-[#ededed]">
              {#each errorPassthroughRules.items as rule}
                <tr class="hover:bg-[#fafafa]">
                  <td class="px-4 py-3 font-mono text-[13px] font-medium text-[#0d0d0d]">{rule.pattern}</td>
                  <td class="px-4 py-3 text-[#3c3c3c]">{matchTypeLabel(rule.matchType)}</td>
                  <td class="px-4 py-3 text-[#3c3c3c] max-w-[200px] truncate">{rule.description || '-'}</td>
                  <td class="px-4 py-3 font-mono text-[13px] text-[#3c3c3c]">{rule.priority}</td>
                  <td class="px-4 py-3">
                    <span class={rule.enabled ? 'rounded-full bg-[#e8f5f0] px-2 py-0.5 text-xs font-medium text-[#0a7a5e]' : 'rounded-full bg-[#f5f5f5] px-2 py-0.5 text-xs font-medium text-[#6e6e6e]'}>{rule.enabled ? 'Enabled' : 'Disabled'}</span>
                  </td>
                  <td class="px-4 py-3 text-right">
                    <button class="ui-button ui-button--sm rounded-md px-2.5 py-1.5 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]" onclick={() => edit(rule)}>Edit</button>
                    <button class="ui-button ui-button--sm ml-1 rounded-md px-2.5 py-1.5 text-sm font-medium text-red-700 hover:bg-red-50" onclick={() => handleDelete(rule.id, rule.pattern)}>Delete</button>
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
