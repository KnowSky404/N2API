<script>
  import {
    ignoreUpcomingUsagePricing,
    removeShutdownUsagePricing,
    savePricingRows,
    syncOfficialUsagePricing,
    usagePricing,
  } from '$lib/admin-state.svelte.js';
  import AuthGate from '$lib/AuthGate.svelte';
  import { LoaderCircle, TriangleAlert, X } from 'lucide-svelte';

  function emptyPricingRow() {
    return {
      model: '',
      inputMicrousdPerMillion: 0,
      cachedInputMicrousdPerMillion: 0,
      outputMicrousdPerMillion: 0,
      longInputMicrousdPerMillion: 0,
      longCachedInputMicrousdPerMillion: 0,
      longOutputMicrousdPerMillion: 0
    };
  }

  /** @param {number} index */
  function removePricingRow(index) {
    usagePricing.rows = usagePricing.rows.filter((_, rowIndex) => rowIndex !== index);
    pricingPage = Math.min(pricingPage, Math.max(1, Math.ceil((usagePricing.rows || []).length / pricingPageSize)));
  }

  let showSyncConfirmModal = $state(false);
  let showShutdownRemovalModal = $state(false);
  let showUpcomingIgnoreModal = $state(false);
  let showAddPricingModal = $state(false);
  let addPricingError = $state('');
  let newPricingRow = $state(emptyPricingRow());
  let addPricingOriginalModel = $state('');
  let selectedShutdownModels = $state(/** @type {string[]} */ ([]));

  /** @type {import('$lib/admin-state.svelte.js').UsagePricingRow|null} */
  let editingPricingRow = $state(null);
  let deleteConfirmPricingPopover = $state(/** @type {{row: import('$lib/admin-state.svelte.js').UsagePricingRow, top: number, left: number}|null} */ (null));

  const pricingBusy = $derived(
    usagePricing.loading ||
    usagePricing.saving ||
    usagePricing.syncing ||
    usagePricing.removingShutdown ||
    usagePricing.ignoringUpcoming
  );
  const upcomingIgnoreActionLabel = $derived(
    `Remove ${usagePricing.upcomingShutdowns.length} model${usagePricing.upcomingShutdowns.length === 1 ? '' : 's'}`
  );

  let closeSyncMessage = $state('');

  $effect(() => {
    const msg = usagePricing.syncMessage;
    if (msg) {
      closeSyncMessage = msg;
      const timer = setTimeout(() => { closeSyncMessage = ''; }, 6000);
      return () => clearTimeout(timer);
    }
  });

  $effect(() => {
    const candidates = usagePricing.deletionCandidates || [];
    if (candidates.length > 0 && !usagePricing.syncing) {
      selectedShutdownModels = candidates.map((item) => item.model);
      showShutdownRemovalModal = true;
    }
  });

  $effect(() => {
    const msg = usagePricing.removalMessage;
    if (msg) {
      closeSyncMessage = msg;
      const timer = setTimeout(() => { closeSyncMessage = ''; }, 6000);
      return () => clearTimeout(timer);
    }
  });

  /** @param {import('$lib/admin-state.svelte.js').UsagePricingRow} row */
  function startEditingPricingRow(row) {
    editingPricingRow = row;
  }

  async function commitPricingRow() {
    if (await savePricingRows()) {
      editingPricingRow = null;
    }
  }

  /** @param {import('$lib/admin-state.svelte.js').UsagePricingRow} row */
  async function confirmRemovePricingRow(row) {
    const priorRows = [...(usagePricing.rows || [])];
    const index = (usagePricing.rows || []).indexOf(row);
    if (index < 0) {
      deleteConfirmPricingPopover = null;
      return;
    }
    removePricingRow(index);

    if (await savePricingRows()) {
      deleteConfirmPricingPopover = null;
    } else {
      usagePricing.rows = priorRows;
    }
  }

  /**
   * @param {import('$lib/admin-state.svelte.js').UsagePricingRow} row
   * @param {MouseEvent} event
   */
  function openDeleteConfirmPricingRow(row, event) {
    const rect = /** @type {HTMLElement} */ (event.currentTarget).getBoundingClientRect();
    const popoverWidth = 288;
    let left = rect.left + rect.width - popoverWidth;
    if (left < 8) left = 8;
    if (left + popoverWidth > window.innerWidth - 8) left = window.innerWidth - popoverWidth - 8;
    deleteConfirmPricingPopover = { row, top: rect.bottom + 8, left };
  }

  function closeDeleteConfirmPricingPopover() {
    deleteConfirmPricingPopover = null;
  }

  const deleteConfirmPricingRow = $derived(deleteConfirmPricingPopover?.row ?? null);

  let pricingSearch = $state('');
  let pricingPage = $state(1);
  let pricingPageSize = $state(10);

  const filteredPricingRows = $derived(
    (usagePricing.rows || []).filter((row) => {
      const query = pricingSearch.trim().toLowerCase();
      if (!query) return true;
      const searchText = [
        row.model,
        String(row.inputMicrousdPerMillion ?? 0),
        String(row.cachedInputMicrousdPerMillion ?? 0),
        String(row.outputMicrousdPerMillion ?? 0),
        String(row.longInputMicrousdPerMillion ?? 0),
        String(row.longCachedInputMicrousdPerMillion ?? 0),
        String(row.longOutputMicrousdPerMillion ?? 0)
      ].join(' ').toLowerCase();
      return searchText.includes(query);
    })
  );

  const sortedPricingRows = $derived(
    [...filteredPricingRows].sort((left, right) => {
      const inputDiff = Number(right.inputMicrousdPerMillion ?? 0) - Number(left.inputMicrousdPerMillion ?? 0);
      if (inputDiff !== 0) return inputDiff;
      const outputDiff = Number(right.outputMicrousdPerMillion ?? 0) - Number(left.outputMicrousdPerMillion ?? 0);
      if (outputDiff !== 0) return outputDiff;
      const longInputDiff = Number(right.longInputMicrousdPerMillion ?? 0) - Number(left.longInputMicrousdPerMillion ?? 0);
      if (longInputDiff !== 0) return longInputDiff;
      return String(left.model ?? '').localeCompare(right.model ?? '');
    })
  );

  const pricingPageCount = $derived(Math.max(1, Math.ceil(sortedPricingRows.length / pricingPageSize)));
  const normalizedPricingPage = $derived(Math.min(Math.max(pricingPage, 1), pricingPageCount));
  const paginatedPricingRows = $derived(
    sortedPricingRows.slice((normalizedPricingPage - 1) * pricingPageSize, normalizedPricingPage * pricingPageSize)
  );
  const pricingPageSummary = $derived(
    sortedPricingRows.length === 0
      ? '0'
      : `${(normalizedPricingPage - 1) * pricingPageSize + 1}-${(normalizedPricingPage - 1) * pricingPageSize + paginatedPricingRows.length}`
  );

  /** @param {Event & { currentTarget: HTMLInputElement }} event */
  function changePricingSearch(event) {
    pricingSearch = event.currentTarget.value;
    pricingPage = 1;
  }

  /** @param {Event & { currentTarget: HTMLSelectElement }} event */
  function changePricingPageSize(event) {
    pricingPageSize = Number(event.currentTarget.value) || 10;
    pricingPage = 1;
  }

  /** @param {number | null | undefined} value */
  function formatPricingInputValue(value) {
    const [whole, fraction] = (Number(value ?? 0) / 1_000_000).toFixed(6).split('.');
    return `${whole}.${fraction.replace(/0+$/, '').padEnd(2, '0')}`;
  }

  /** @param {number | null | undefined} value */
  function formatPricingValue(value) {
    return `$${formatPricingInputValue(value)}`;
  }

  /**
   * @param {import('$lib/admin-state.svelte.js').UsagePricingRow} row
   * @param {'inputMicrousdPerMillion'|'cachedInputMicrousdPerMillion'|'outputMicrousdPerMillion'|'longInputMicrousdPerMillion'|'longCachedInputMicrousdPerMillion'|'longOutputMicrousdPerMillion'} field
   * @param {Event & { currentTarget: HTMLInputElement }} event
   */
  function updatePricingValue(row, field, event) {
    const dollarsPerMillion = Number(event.currentTarget.value);
    row[field] = Number.isFinite(dollarsPerMillion) && dollarsPerMillion >= 0
      ? Math.round(dollarsPerMillion * 1_000_000)
      : 0;
  }

  function openAddPricingModal() {
    newPricingRow = emptyPricingRow();
    addPricingOriginalModel = '';
    addPricingError = '';
    showAddPricingModal = true;
  }

  function closeAddPricingModal() {
    if (pricingBusy) return;
    showAddPricingModal = false;
    addPricingError = '';
    newPricingRow = emptyPricingRow();
    addPricingOriginalModel = '';
  }

  /** @param {SubmitEvent} event */
  async function submitAddPricingModel(event) {
    event.preventDefault();
    if (pricingBusy) return;

    const model = String(newPricingRow.model ?? '').trim();
    if (!model) {
      addPricingError = 'Model name is required.';
      return;
    }
    if (
      (usagePricing.rows || []).some((row) => {
        const rowModel = String(row.model ?? '').trim();
        return rowModel === model && rowModel !== addPricingOriginalModel;
      })
    ) {
      addPricingError = 'A pricing row for this model already exists.';
      return;
    }

    addPricingError = '';
    const priorRows = [...(usagePricing.rows || [])];
    const row = { ...newPricingRow, model };
    if (addPricingOriginalModel) {
      const originalIndex = priorRows.findIndex(
        (item) => String(item.model ?? '').trim() === addPricingOriginalModel
      );
      if (originalIndex < 0) {
        addPricingError = 'The pricing row changed on the server. Reopen the dialog and try again.';
        return;
      }
      usagePricing.rows = priorRows.map((item, index) => (index === originalIndex ? row : item));
    } else {
      usagePricing.rows = [...priorRows, row];
    }

    if (!(await savePricingRows())) {
      usagePricing.rows = priorRows;
      addPricingError = usagePricing.error || 'Failed to add pricing model.';
      return;
    }

    pricingSearch = '';
    const sortedIndex = sortedPricingRows.findIndex((item) => item.model === model);
    pricingPage = sortedIndex >= 0 ? Math.floor(sortedIndex / pricingPageSize) + 1 : 1;
    const savedRow = (usagePricing.rows || []).find((item) => String(item.model ?? '').trim() === model);
    newPricingRow = savedRow ? { ...savedRow } : row;
    addPricingOriginalModel = model;
  }

  function openSyncConfirmModal() {
    showSyncConfirmModal = true;
  }

  function closeSyncConfirmModal() {
    if (!pricingBusy) showSyncConfirmModal = false;
  }

  function openUpcomingIgnoreModal() {
    showUpcomingIgnoreModal = true;
  }

  function closeUpcomingIgnoreModal() {
    if (!pricingBusy) showUpcomingIgnoreModal = false;
  }

  async function confirmUpcomingIgnore() {
    const models = (usagePricing.upcomingShutdowns || []).map((item) => item.model);
    if (await ignoreUpcomingUsagePricing(models)) {
      showUpcomingIgnoreModal = false;
    }
  }

  function openShutdownRemovalModal() {
    selectedShutdownModels = (usagePricing.deletionCandidates || []).map((item) => item.model);
    showShutdownRemovalModal = true;
  }

  function closeShutdownRemovalModal() {
    if (pricingBusy) return;
    showShutdownRemovalModal = false;
    selectedShutdownModels = [];
  }

  async function confirmSyncOfficial() {
    if (await syncOfficialUsagePricing()) {
      showSyncConfirmModal = false;
    }
  }

  /** @param {Event & { currentTarget: HTMLInputElement }} event */
  function toggleShutdownModel(event) {
    const model = event.currentTarget.value;
    if (event.currentTarget.checked) {
      if (!selectedShutdownModels.includes(model)) selectedShutdownModels = [...selectedShutdownModels, model];
    } else {
      selectedShutdownModels = selectedShutdownModels.filter((item) => item !== model);
    }
  }

  async function confirmShutdownRemoval() {
    if (await removeShutdownUsagePricing(selectedShutdownModels)) {
      closeShutdownRemovalModal();
    }
  }
</script>

<svelte:head>
  <title>N2API Pricing</title>
</svelte:head>

<svelte:window onscroll={closeDeleteConfirmPricingPopover} onresize={closeDeleteConfirmPricingPopover} />

<AuthGate>
<div class="space-y-6">
<section class="relative rounded-lg border border-[#ededed] bg-white p-6">
  <div>
    {#if pricingBusy}
      <div class="ui-loading-overlay absolute inset-0 z-40 flex flex-col items-center justify-center gap-2 rounded-lg bg-white/85 backdrop-blur-[2px]" aria-label="Pricing operation in progress">
        <LoaderCircle class="h-8 w-8 animate-spin text-[#10a37f]" aria-hidden="true" />
        <span class="text-sm font-medium text-[#6e6e6e]">thinking</span>
      </div>
    {/if}
    <div class="flex flex-wrap items-center justify-between gap-4">
      <div>
        <h2 class="text-xl font-semibold leading-tight text-[#0d0d0d]">Pricing</h2>
      <p class="mt-1 text-sm text-[#6e6e6e]">Official OpenAI Standard pricing — prices shown in USD per 1M tokens for historical estimates.</p>
      </div>
      <div class="ml-auto flex flex-wrap items-center justify-end gap-3">
        {#if usagePricing.deletionCandidates?.length}
          <button
            class="ui-button ui-button--sm ui-button--warning rounded-lg border border-amber-200 bg-amber-50 px-2.5 py-1.5 text-xs font-medium text-amber-900 hover:bg-amber-100 disabled:cursor-not-allowed disabled:opacity-60"
            type="button"
            disabled={pricingBusy}
            onclick={openShutdownRemovalModal}
          >
            Review shutdowns ({usagePricing.deletionCandidates.length})
          </button>
        {/if}
        {#if usagePricing.upcomingShutdowns?.length}
          <button
            class="ui-button ui-button--icon ui-button--warning relative grid h-8 w-8 shrink-0 place-items-center rounded-lg border border-amber-200 bg-amber-50 text-amber-800 hover:bg-amber-100 disabled:cursor-not-allowed disabled:opacity-60"
            type="button"
            aria-label="Review upcoming model shutdowns"
            title="Review upcoming model shutdowns"
            disabled={pricingBusy}
            onclick={openUpcomingIgnoreModal}
          >
            <TriangleAlert class="h-4 w-4" aria-hidden="true" />
            <span class="absolute -right-1.5 -top-1.5 grid min-h-4 min-w-4 place-items-center rounded-full border-2 border-white bg-amber-700 px-1 text-[9px] font-semibold leading-none text-white">
              {usagePricing.upcomingShutdowns.length}
            </span>
          </button>
        {/if}
        <button
          class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
          type="button"
          disabled={pricingBusy}
          onclick={openSyncConfirmModal}
        >
          {usagePricing.syncing ? 'Syncing' : 'Sync official'}
        </button>
        <button
          class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
          type="button"
          disabled={pricingBusy}
          onclick={openAddPricingModal}
        >
          Add model
        </button>
      </div>
    </div>

    {#if usagePricing.error}
      <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">{usagePricing.error}</p>
    {:else if usagePricing.saved}
      <p class="mt-4 rounded-md border border-[#cce7db] bg-[#e8f5f0] p-3 text-sm text-[#0a7a5e]">Pricing saved.</p>
    {/if}

    <div class="mt-5 grid gap-3" style="grid-template-columns: 1fr auto">
      <label class="grid gap-1 text-sm font-medium text-[#3c3c3c]">
        Search pricing rows
        <input
          class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
          type="search"
          placeholder="Search model name or prices"
          value={pricingSearch}
          oninput={changePricingSearch}
        />
      </label>
      <div class="flex items-end">
        <p class="text-sm text-[#6e6e6e] tabular-nums">
          {sortedPricingRows.length} / {(usagePricing.rows || []).length} rows
          {#if pricingSearch.trim()}
            matching
          {/if}
        </p>
      </div>
    </div>

    <div class="ui-table-shell ui-table-shell--scroll mt-5 overflow-auto max-h-[65vh] rounded-lg border border-[#ededed]">
      <table class="ui-table w-full min-w-[1280px] text-left text-sm">
        <thead class="border-b border-[#e5e5e5] bg-[#f5f5f5] text-[#6e6e6e]">
          <tr>
            <th class="sticky left-0 top-0 z-20 bg-[#f5f5f5] px-4 py-3 font-medium shadow-[8px_0_12px_rgba(255,255,255,0.85)]">Model</th>
            <th class="sticky top-0 z-10 bg-[#f5f5f5] px-4 py-3 font-medium">Input ($/1M tokens)</th>
            <th class="sticky top-0 z-10 bg-[#f5f5f5] px-4 py-3 font-medium">Cached input ($/1M tokens)</th>
            <th class="sticky top-0 z-10 bg-[#f5f5f5] px-4 py-3 font-medium">Output ($/1M tokens)</th>
            <th class="sticky top-0 z-10 bg-[#f5f5f5] px-4 py-3 font-medium">Long input ($/1M tokens)</th>
            <th class="sticky top-0 z-10 bg-[#f5f5f5] px-4 py-3 font-medium">Long cached input ($/1M tokens)</th>
            <th class="sticky top-0 z-10 bg-[#f5f5f5] px-4 py-3 font-medium">Long output ($/1M tokens)</th>
            <th class="top-0 z-20 bg-[#f5f5f5] px-3 py-3 text-right font-medium md:sticky md:right-0 md:shadow-[-8px_0_12px_rgba(255,255,255,0.85)]">Actions</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-[#ededed]">
          {#if !usagePricing.rows?.length}
            <tr>
              <td class="ui-table-empty px-4 py-5 text-[#6e6e6e]" colspan="8">No pricing rows configured. Add a model or sync official OpenAI Standard pricing.</td>
            </tr>
          {:else if sortedPricingRows.length === 0}
            <tr>
              <td class="ui-table-empty px-4 py-5 text-[#6e6e6e]" colspan="8">No pricing rows match your search.</td>
            </tr>
          {:else}
            {#each paginatedPricingRows as row, index (row.model + '-' + (usagePricing.rows || []).indexOf(row))}
              {@const isEditing = editingPricingRow === row}
              <tr>
                <td class="sticky left-0 z-[5] bg-white px-4 py-3 shadow-[8px_0_12px_rgba(255,255,255,0.85)]">
                  {#if isEditing}
                    <input class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={row.model} placeholder="gpt-5" />
                  {:else}
                    <span class="block max-w-[220px] truncate font-mono text-[13px] text-[#0d0d0d]">{row.model || '-'}</span>
                  {/if}
                </td>
                <td class="px-4 py-3">
                  {#if isEditing}
                    <input class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" type="number" min="0" step="0.000001" value={formatPricingInputValue(row.inputMicrousdPerMillion)} oninput={(event) => updatePricingValue(row, 'inputMicrousdPerMillion', event)} />
                  {:else}
                    <span class="font-mono text-[13px] tabular-nums text-[#3c3c3c]">{formatPricingValue(row.inputMicrousdPerMillion)}</span>
                  {/if}
                </td>
                <td class="px-4 py-3">
                  {#if isEditing}
                    <input class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" type="number" min="0" step="0.000001" value={formatPricingInputValue(row.cachedInputMicrousdPerMillion)} oninput={(event) => updatePricingValue(row, 'cachedInputMicrousdPerMillion', event)} />
                  {:else}
                    <span class="font-mono text-[13px] tabular-nums text-[#3c3c3c]">{formatPricingValue(row.cachedInputMicrousdPerMillion)}</span>
                  {/if}
                </td>
                <td class="px-4 py-3">
                  {#if isEditing}
                    <input class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" type="number" min="0" step="0.000001" value={formatPricingInputValue(row.outputMicrousdPerMillion)} oninput={(event) => updatePricingValue(row, 'outputMicrousdPerMillion', event)} />
                  {:else}
                    <span class="font-mono text-[13px] tabular-nums text-[#3c3c3c]">{formatPricingValue(row.outputMicrousdPerMillion)}</span>
                  {/if}
                </td>
                <td class="px-4 py-3">
                  {#if isEditing}
                    <input class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" type="number" min="0" step="0.000001" value={formatPricingInputValue(row.longInputMicrousdPerMillion)} oninput={(event) => updatePricingValue(row, 'longInputMicrousdPerMillion', event)} />
                  {:else}
                    <span class="font-mono text-[13px] tabular-nums text-[#3c3c3c]">{formatPricingValue(row.longInputMicrousdPerMillion)}</span>
                  {/if}
                </td>
                <td class="px-4 py-3">
                  {#if isEditing}
                    <input class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" type="number" min="0" step="0.000001" value={formatPricingInputValue(row.longCachedInputMicrousdPerMillion)} oninput={(event) => updatePricingValue(row, 'longCachedInputMicrousdPerMillion', event)} />
                  {:else}
                    <span class="font-mono text-[13px] tabular-nums text-[#3c3c3c]">{formatPricingValue(row.longCachedInputMicrousdPerMillion)}</span>
                  {/if}
                </td>
                <td class="px-4 py-3">
                  {#if isEditing}
                    <input class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" type="number" min="0" step="0.000001" value={formatPricingInputValue(row.longOutputMicrousdPerMillion)} oninput={(event) => updatePricingValue(row, 'longOutputMicrousdPerMillion', event)} />
                  {:else}
                    <span class="font-mono text-[13px] tabular-nums text-[#3c3c3c]">{formatPricingValue(row.longOutputMicrousdPerMillion)}</span>
                  {/if}
                </td>
                <td class="bg-white px-3 py-3 md:sticky md:right-0 md:shadow-[-8px_0_12px_rgba(255,255,255,0.85)]">
                  <div class="flex justify-end gap-2 whitespace-nowrap">
                    {#if isEditing}
                      <button class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]" type="button" disabled={pricingBusy} onclick={commitPricingRow}>Done</button>
                    {:else}
                      <button class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]" type="button" disabled={pricingBusy} onclick={() => startEditingPricingRow(row)}>Edit</button>
                        <button class="ui-button ui-button--sm ui-button--danger rounded-lg border border-red-200 bg-white px-2.5 py-1.5 text-xs font-medium text-red-700 hover:bg-red-50 disabled:cursor-not-allowed disabled:opacity-60" type="button" disabled={pricingBusy} onclick={(e) => openDeleteConfirmPricingRow(row, e)}>Remove</button>
                    {/if}
                  </div>
                </td>
              </tr>
            {/each}
          {/if}
        </tbody>
      </table>
    </div>

    <div class="ui-pagination mt-4 flex flex-col gap-3 text-sm text-[#6e6e6e] sm:flex-row sm:items-center sm:justify-between">
      <p class="tabular-nums">Showing {pricingPageSummary} of {sortedPricingRows.length}</p>
      <div class="flex flex-wrap items-center gap-3">
        <label class="flex items-center gap-2 font-medium text-[#3c3c3c]">
          Rows
          <select
            class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
            value={pricingPageSize}
            onchange={changePricingPageSize}
          >
            <option value={5}>5</option>
            <option value={10}>10</option>
            <option value={20}>20</option>
          </select>
        </label>
        <div class="flex items-center gap-2">
          <button
            class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
            type="button"
            disabled={normalizedPricingPage <= 1}
            onclick={() => pricingPage = Math.max(1, normalizedPricingPage - 1)}
          >
            Previous
          </button>
          <span class="tabular-nums text-[#3c3c3c]">Page {normalizedPricingPage} / {pricingPageCount}</span>
          <button
            class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
            type="button"
            disabled={normalizedPricingPage >= pricingPageCount}
            onclick={() => pricingPage = Math.min(pricingPageCount, normalizedPricingPage + 1)}
          >
            Next
          </button>
        </div>
      </div>
    </div>
  </div>



  {#if closeSyncMessage}
    <div class="fixed right-4 top-20 z-50 max-w-sm rounded-lg border border-[#cce7db] bg-[#e8f5f0] p-4 shadow-lg">
      <div class="flex items-start justify-between gap-2">
        <p class="text-sm text-[#0a7a5e]">{closeSyncMessage}</p>
        <button class="ui-button ml-2 shrink-0 text-[#0a7a5e] hover:text-[#08694a]" type="button" onclick={() => { closeSyncMessage = ''; }} aria-label="Close sync message">&times;</button>
      </div>
    </div>
  {/if}

  {#if showSyncConfirmModal}
    <div class="ui-modal-backdrop fixed inset-0 z-50 flex items-center justify-center bg-black/40" role="dialog" aria-modal="true" aria-labelledby="sync-pricing-title">
      <div class="ui-modal-panel ui-modal-panel--sm mx-4 w-full max-w-md rounded-xl border border-[#ededed] bg-white p-6 shadow-lg">
        <div class="flex items-start justify-between gap-4">
          <h3 id="sync-pricing-title" class="text-lg font-semibold text-[#0d0d0d]">Sync official OpenAI pricing</h3>
          <button
            class="ui-button grid h-8 w-8 shrink-0 place-items-center rounded-md text-[#6e6e6e] hover:bg-[#f5f5f5] hover:text-[#0d0d0d] disabled:cursor-not-allowed disabled:opacity-60"
            type="button"
            aria-label="Close sync pricing dialog"
            disabled={pricingBusy}
            onclick={closeSyncConfirmModal}
          >
            <X class="h-4 w-4" aria-hidden="true" />
          </button>
        </div>
        <p class="mt-3 text-sm text-[#3c3c3c]">
          Compatible OpenAI Standard prices will be added or updated. Local-only pricing rows remain unchanged.
        </p>
        <div class="mt-3 space-y-1 text-sm text-[#6e6e6e]">
          <p><a class="text-[#0a7a5e] underline hover:text-[#08694a]" href="https://developers.openai.com/api/docs/models/all" target="_blank" rel="noopener noreferrer">Models catalog</a></p>
          <p><a class="text-[#0a7a5e] underline hover:text-[#08694a]" href="https://developers.openai.com/api/docs/pricing" target="_blank" rel="noopener noreferrer">Standard pricing</a></p>
          <p><a class="text-[#0a7a5e] underline hover:text-[#08694a]" href="https://developers.openai.com/api/docs/deprecations" target="_blank" rel="noopener noreferrer">Deprecations</a></p>
        </div>
        <div class="ui-modal-actions mt-6 flex justify-end gap-3">
          <button
            class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]"
            type="button"
            disabled={pricingBusy}
            onclick={closeSyncConfirmModal}
          >
            Cancel
          </button>
          <button
            class="ui-button ui-button--sm ui-button--primary rounded-lg bg-[#0d0d0d] px-2.5 py-1.5 text-xs font-medium text-white hover:bg-[#3c3c3c] disabled:cursor-not-allowed disabled:opacity-60"
            type="button"
            disabled={pricingBusy}
            onclick={confirmSyncOfficial}
          >
            Confirm sync
          </button>
        </div>
      </div>
    </div>
  {/if}

  {#if showAddPricingModal}
    <div
      class="ui-modal-backdrop fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="add-pricing-title"
    >
      <form class="ui-modal-panel ui-modal-panel--lg max-h-[calc(100vh-2rem)] w-full max-w-2xl overflow-y-auto rounded-lg border border-[#ededed] bg-white p-5 shadow-xl sm:p-6" onsubmit={submitAddPricingModel}>
        <div class="flex items-start justify-between gap-4">
          <div>
            <h3 id="add-pricing-title" class="text-lg font-semibold text-[#0d0d0d]">{addPricingOriginalModel ? 'Edit pricing model' : 'Add pricing model'}</h3>
            <p class="mt-1 text-sm text-[#6e6e6e]">
              {addPricingOriginalModel ? 'Update this saved model and save again.' : 'Enter prices in USD per 1M tokens.'}
            </p>
          </div>
          <button
            class="ui-button grid h-8 w-8 shrink-0 place-items-center rounded-md text-[#6e6e6e] hover:bg-[#f5f5f5] hover:text-[#0d0d0d] disabled:cursor-not-allowed disabled:opacity-60"
            type="button"
            aria-label="Close pricing model dialog"
            disabled={pricingBusy}
            onclick={closeAddPricingModal}
          >
            <X class="h-4 w-4" aria-hidden="true" />
          </button>
        </div>

        <div class="mt-5 grid gap-4 sm:grid-cols-2">
          <label class="grid gap-1.5 text-sm font-medium text-[#3c3c3c] sm:col-span-2">
            Model
            <input class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={newPricingRow.model} placeholder="gpt-5" required disabled={pricingBusy} />
          </label>
          <label class="grid gap-1.5 text-sm font-medium text-[#3c3c3c]">
            Input
            <input class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" type="number" min="0" step="0.000001" value={formatPricingInputValue(newPricingRow.inputMicrousdPerMillion)} oninput={(event) => updatePricingValue(newPricingRow, 'inputMicrousdPerMillion', event)} required disabled={pricingBusy} />
          </label>
          <label class="grid gap-1.5 text-sm font-medium text-[#3c3c3c]">
            Cached input
            <input class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" type="number" min="0" step="0.000001" value={formatPricingInputValue(newPricingRow.cachedInputMicrousdPerMillion)} oninput={(event) => updatePricingValue(newPricingRow, 'cachedInputMicrousdPerMillion', event)} required disabled={pricingBusy} />
          </label>
          <label class="grid gap-1.5 text-sm font-medium text-[#3c3c3c]">
            Output
            <input class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" type="number" min="0" step="0.000001" value={formatPricingInputValue(newPricingRow.outputMicrousdPerMillion)} oninput={(event) => updatePricingValue(newPricingRow, 'outputMicrousdPerMillion', event)} required disabled={pricingBusy} />
          </label>
          <label class="grid gap-1.5 text-sm font-medium text-[#3c3c3c]">
            Long input
            <input class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" type="number" min="0" step="0.000001" value={formatPricingInputValue(newPricingRow.longInputMicrousdPerMillion)} oninput={(event) => updatePricingValue(newPricingRow, 'longInputMicrousdPerMillion', event)} required disabled={pricingBusy} />
          </label>
          <label class="grid gap-1.5 text-sm font-medium text-[#3c3c3c]">
            Long cached input
            <input class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" type="number" min="0" step="0.000001" value={formatPricingInputValue(newPricingRow.longCachedInputMicrousdPerMillion)} oninput={(event) => updatePricingValue(newPricingRow, 'longCachedInputMicrousdPerMillion', event)} required disabled={pricingBusy} />
          </label>
          <label class="grid gap-1.5 text-sm font-medium text-[#3c3c3c]">
            Long output
            <input class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" type="number" min="0" step="0.000001" value={formatPricingInputValue(newPricingRow.longOutputMicrousdPerMillion)} oninput={(event) => updatePricingValue(newPricingRow, 'longOutputMicrousdPerMillion', event)} required disabled={pricingBusy} />
          </label>
        </div>

        {#if addPricingError}
          <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700" role="alert">{addPricingError}</p>
        {/if}

        <div class="ui-modal-actions mt-6 flex justify-end gap-3">
          <button class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:opacity-60" type="button" disabled={pricingBusy} onclick={closeAddPricingModal}>Cancel</button>
          <button class="ui-button ui-button--sm ui-button--primary rounded-lg bg-[#0d0d0d] px-2.5 py-1.5 text-xs font-medium text-white hover:bg-[#3c3c3c] disabled:cursor-not-allowed disabled:opacity-60" type="submit" disabled={pricingBusy}>{usagePricing.saving ? 'Saving' : 'Save'}</button>
        </div>
      </form>
    </div>
  {/if}

  {#if showUpcomingIgnoreModal}
    <div
      class="ui-modal-backdrop fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="upcoming-ignore-title"
    >
      <div class="ui-modal-panel ui-modal-panel--md grid w-full max-w-lg gap-4 rounded-lg bg-white p-5 shadow-xl">
        <div class="flex items-start justify-between gap-4">
          <div class="flex items-start gap-3">
            <div class="grid h-9 w-9 shrink-0 place-items-center rounded-lg bg-amber-50 text-amber-800">
              <TriangleAlert class="h-5 w-5" aria-hidden="true" />
            </div>
            <div>
              <h3 id="upcoming-ignore-title" class="text-lg font-semibold text-[#0d0d0d]">Upcoming model shutdowns</h3>
              <p class="mt-1 text-sm text-[#6e6e6e]">Remove and keep these models out of future official pricing syncs.</p>
            </div>
          </div>
          <button
            class="ui-button grid h-8 w-8 shrink-0 place-items-center rounded-md text-[#6e6e6e] hover:bg-[#f5f5f5] hover:text-[#0d0d0d] disabled:cursor-not-allowed disabled:opacity-60"
            type="button"
            aria-label="Close upcoming shutdowns dialog"
            disabled={pricingBusy}
            onclick={closeUpcomingIgnoreModal}
          >
            <X class="h-4 w-4" aria-hidden="true" />
          </button>
        </div>
        <div class="max-h-72 overflow-y-auto rounded-lg border border-[#ededed]">
          {#each usagePricing.upcomingShutdowns as item (item.model)}
            <div class="border-t border-[#ededed] px-3 py-3 first:border-t-0">
              <p class="font-mono text-[13px] font-medium text-[#0d0d0d]">{item.model}</p>
              <p class="mt-1 text-xs text-[#6e6e6e]">
                Shutdown {item.shutdownDate}{item.replacement ? ` · Use ${item.replacement}` : ''}
              </p>
            </div>
          {/each}
        </div>
        <div class="ui-modal-actions flex justify-end gap-2">
          <button
            class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:opacity-60"
            type="button"
            disabled={pricingBusy}
            onclick={closeUpcomingIgnoreModal}
          >
            Cancel
          </button>
          <button
            class="ui-button ui-button--sm ui-button--danger-filled rounded-lg bg-[#ef4146] px-2.5 py-1.5 text-xs font-medium text-white hover:bg-[#d7373c] disabled:opacity-60"
            type="button"
            disabled={pricingBusy || !usagePricing.upcomingShutdowns.length}
            onclick={confirmUpcomingIgnore}
          >
            {upcomingIgnoreActionLabel}
          </button>
        </div>
      </div>
    </div>
  {/if}

  {#if showShutdownRemovalModal}
    <div class="ui-modal-backdrop fixed inset-0 z-50 flex items-center justify-center bg-black/40" role="dialog" aria-modal="true" aria-labelledby="shutdown-removal-title">
      <div class="ui-modal-panel ui-modal-panel--md mx-4 w-full max-w-lg rounded-lg border border-[#ededed] bg-white p-6 shadow-lg">
        <div class="flex items-start justify-between gap-4">
          <h3 id="shutdown-removal-title" class="text-lg font-semibold text-[#0d0d0d]">Remove shut-down models</h3>
          <button
            class="ui-button grid h-8 w-8 shrink-0 place-items-center rounded-md text-[#6e6e6e] hover:bg-[#f5f5f5] hover:text-[#0d0d0d] disabled:cursor-not-allowed disabled:opacity-60"
            type="button"
            aria-label="Close shut-down model removal dialog"
            disabled={pricingBusy}
            onclick={closeShutdownRemovalModal}
          >
            <X class="h-4 w-4" aria-hidden="true" />
          </button>
        </div>
        <div class="mt-4 max-h-[50vh] overflow-y-auto rounded-lg border border-[#ededed] divide-y divide-[#ededed]">
          {#each usagePricing.deletionCandidates as item (item.model)}
            <label class="flex items-start gap-3 px-4 py-3 text-sm">
              <input class="mt-0.5 h-4 w-4 accent-[#0d0d0d]" type="checkbox" value={item.model} checked={selectedShutdownModels.includes(item.model)} onchange={toggleShutdownModel} />
              <span class="min-w-0">
                <span class="block truncate font-mono font-medium text-[#0d0d0d]">{item.model}</span>
                <span class="mt-1 block text-[#6e6e6e]">Shut down {item.shutdownDate}{item.replacement ? ` · Use ${item.replacement}` : ''}</span>
              </span>
            </label>
          {/each}
        </div>
        <div class="ui-modal-actions mt-6 flex justify-end gap-3">
          <button class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:opacity-60" type="button" disabled={pricingBusy} onclick={closeShutdownRemovalModal}>Cancel</button>
          <button class="ui-button ui-button--sm ui-button--danger rounded-lg border border-red-200 bg-red-50 px-2.5 py-1.5 text-xs font-medium text-red-700 hover:bg-red-100 disabled:cursor-not-allowed disabled:opacity-60" type="button" disabled={pricingBusy || selectedShutdownModels.length === 0} onclick={confirmShutdownRemoval}>Remove {selectedShutdownModels.length} models</button>
        </div>
      </div>
    </div>
  {/if}
</section>

</div>

{#if deleteConfirmPricingPopover}
  <div class="fixed z-50 w-72 rounded-xl border border-[#ededed] bg-white p-4 shadow-[0_4px_16px_rgba(13,13,13,0.08)]" style="top: {deleteConfirmPricingPopover.top}px; left: {deleteConfirmPricingPopover.left}px;">
    <p class="text-sm font-medium text-[#0d0d0d]">Remove this pricing row?</p>
    <p class="mt-1 text-sm text-[#6e6e6e]">{deleteConfirmPricingRow?.model || 'this row'}</p>
    <div class="mt-3 flex justify-end gap-2">
      <button class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]" type="button" onclick={closeDeleteConfirmPricingPopover}>Cancel</button>
      <button class="ui-button ui-button--sm ui-button--danger rounded-lg border border-red-200 bg-white px-2.5 py-1.5 text-xs font-medium text-red-700 hover:bg-red-50" type="button" onclick={() => deleteConfirmPricingRow && confirmRemovePricingRow(deleteConfirmPricingRow)}>Remove</button>
    </div>
  </div>
{/if}

</AuthGate>
