<script>
  import { goto } from '$app/navigation';
  import { page } from '$app/state';
  import AuthGate from '$lib/AuthGate.svelte';
  import {
    formatDate,
    loadSystemEvents,
    resetSystemEventFilters,
    session,
    systemEvents
  } from '$lib/admin-state.svelte.js';
  import {
    Activity,
    ChevronDown,
    ChevronRight,
    CircleCheck,
    CircleX,
    Clock3,
    Eye,
    KeyRound,
    RefreshCw,
    Search,
    ShieldCheck,
    TriangleAlert,
    X
  } from 'lucide-svelte';

  /** @typedef {import('$lib/admin-state.svelte.js').SystemEvent} SystemEvent */

  const categories = ['audit', 'security', 'oauth', 'scheduler', 'runtime'];
  const outcomes = ['success', 'failure', 'partial'];
  const severities = ['info', 'warning', 'error'];
  const timeRanges = [
    { value: 'all', label: 'All time', seconds: 0 },
    { value: '1h', label: 'Last hour', seconds: 3600 },
    { value: '24h', label: 'Last 24 hours', seconds: 86400 },
    { value: '7d', label: 'Last 7 days', seconds: 604800 },
    { value: '30d', label: 'Last 30 days', seconds: 2592000 }
  ];

  let appliedSearch = $state(/** @type {string | null} */ (null));
  let timeRange = $state('all');
  let selectedEvent = $state(/** @type {import('$lib/admin-state.svelte.js').SystemEvent | null} */ (null));
  let expandedBatchIds = $state(/** @type {Record<string, boolean>} */ ({}));
  let eventPage = $state(1);
  let eventPageSize = $state(5);

  const activeFilterCount = $derived([
    systemEvents.query,
    systemEvents.since,
    systemEvents.category === 'all' ? '' : systemEvents.category,
    systemEvents.outcome === 'all' ? '' : systemEvents.outcome,
    systemEvents.severity === 'all' ? '' : systemEvents.severity,
    systemEvents.action,
    systemEvents.actor,
    systemEvents.targetType,
    systemEvents.targetId
  ].filter(Boolean).length);

  const groupedEvents = $derived.by(() => {
    const batchChildren = new Map();
    const batchSummaries = new Map();

    for (const event of systemEvents.items) {
      const batchId = eventBatchId(event);
      if (!batchId) continue;
      if (isBatchSummary(event)) {
        batchSummaries.set(batchId, event);
      } else {
        const children = batchChildren.get(batchId) ?? [];
        children.push(event);
        batchChildren.set(batchId, children);
      }
    }

    const hiddenChildren = new Set();
    for (const [batchId, children] of batchChildren) {
      if (batchSummaries.has(batchId)) {
        for (const child of children) hiddenChildren.add(String(child.id));
      }
    }

    return systemEvents.items
      .filter((event) => !hiddenChildren.has(String(event.id)))
      .map((event) => {
        const batchId = eventBatchId(event);
        return {
          event,
          batchId,
          children: batchId && isBatchSummary(event) ? batchChildren.get(batchId) ?? [] : []
        };
      });
  });

  const eventPageCount = $derived(Math.max(1, Math.ceil(groupedEvents.length / eventPageSize)));
  const normalizedEventPage = $derived(Math.min(Math.max(eventPage, 1), eventPageCount));
  const paginatedGroupedEvents = $derived(
    groupedEvents.slice((normalizedEventPage - 1) * eventPageSize, normalizedEventPage * eventPageSize)
  );
  const eventPageSummary = $derived(
    groupedEvents.length === 0
      ? '0'
      : `${(normalizedEventPage - 1) * eventPageSize + 1}-${(normalizedEventPage - 1) * eventPageSize + paginatedGroupedEvents.length}`
  );

  /** @param {string} search */
  function applyURLFilters(search) {
    resetSystemEventFilters();
    const params = new URLSearchParams(search);
    const category = params.get('category') ?? '';
    const outcome = params.get('outcome') ?? '';
    const severity = params.get('severity') ?? '';
    const since = params.get('since') ?? '';

    if (categories.includes(category)) systemEvents.category = category;
    if (outcomes.includes(outcome)) systemEvents.outcome = outcome;
    if (severities.includes(severity)) systemEvents.severity = severity;
    if (/^\d+$/.test(since)) systemEvents.since = since;

    systemEvents.query = boundedParam(params, 'q', 200);
    systemEvents.action = boundedParam(params, 'action', 120);
    systemEvents.actor = boundedParam(params, 'actor', 120);
    systemEvents.targetType = boundedParam(params, 'targetType', 120);
    systemEvents.targetId = boundedParam(params, 'targetId', 128);

    timeRange = rangeForSince(systemEvents.since);
    expandedBatchIds = {};
    eventPage = 1;
  }

  /** @param {URLSearchParams} params @param {string} key @param {number} maxLength */
  function boundedParam(params, key, maxLength) {
    const value = params.get(key) ?? '';
    return value.length <= maxLength ? value : '';
  }

  /** @param {string} since */
  function rangeForSince(since) {
    if (!/^\d+$/.test(since)) return 'all';
    const age = Math.max(0, Math.floor(Date.now() / 1000) - Number(since));
    const match = timeRanges.find((range) => range.seconds > 0 && Math.abs(range.seconds - age) < 300);
    return match?.value ?? 'custom';
  }

  function filterParams() {
    const params = new URLSearchParams();
    if (systemEvents.query.trim()) params.set('q', systemEvents.query.trim());
    if (systemEvents.category !== 'all') params.set('category', systemEvents.category);
    if (systemEvents.outcome !== 'all') params.set('outcome', systemEvents.outcome);
    if (systemEvents.severity !== 'all') params.set('severity', systemEvents.severity);
    if (systemEvents.action.trim()) params.set('action', systemEvents.action.trim());
    if (systemEvents.actor.trim()) params.set('actor', systemEvents.actor.trim());
    if (systemEvents.targetType.trim()) params.set('targetType', systemEvents.targetType.trim());
    if (systemEvents.targetId.trim()) params.set('targetId', systemEvents.targetId.trim());

    if (timeRange === 'custom' && /^\d+$/.test(systemEvents.since)) {
      params.set('since', systemEvents.since);
    } else {
      const range = timeRanges.find((item) => item.value === timeRange);
      if (range?.seconds) params.set('since', String(Math.floor(Date.now() / 1000) - range.seconds));
    }
    return params;
  }

  /** @param {SubmitEvent} event */
  async function applyFilters(event) {
    event.preventDefault();
    const search = filterParams().toString();
    const nextSearch = search ? `?${search}` : '';
    if (page.url.search === nextSearch) {
      await loadSystemEvents();
      return;
    }
    await goto(search ? `/system-logs?${search}` : '/system-logs', {
      keepFocus: true,
      noScroll: true
    });
  }

  async function resetFilters() {
    resetSystemEventFilters();
    timeRange = 'all';
    eventPage = 1;
    await goto('/system-logs', { keepFocus: true, noScroll: true });
  }

  async function refreshSystemEvents() {
    eventPage = 1;
    await loadSystemEvents();
  }

  /** @param {number} targetPage */
  function goToEventPage(targetPage) {
    eventPage = Math.min(Math.max(targetPage, 1), eventPageCount);
  }

  /** @param {SystemEvent} event */
  function openDetails(event) {
    selectedEvent = event;
  }

  function closeDetails() {
    selectedEvent = null;
  }

  /** @param {KeyboardEvent} event */
  function handleKeydown(event) {
    if (event.key === 'Escape' && selectedEvent) closeDetails();
  }

  /** @param {string} batchId */
  function toggleBatch(batchId) {
    expandedBatchIds[batchId] = !expandedBatchIds[batchId];
  }

  /** @param {SystemEvent} event */
  function eventBatchId(event) {
    const value = event.metadata?.batch_id ?? event.metadata?.batchId;
    return typeof value === 'string' || typeof value === 'number' ? String(value) : '';
  }

  /** @param {SystemEvent} event */
  function isBatchSummary(event) {
    const metadata = event.metadata ?? {};
    return event.target.type.endsWith('_batch') ||
      event.target.type === 'batch' ||
      'requested' in metadata ||
      'requested_count' in metadata ||
      'requestedCount' in metadata;
  }

  /** @param {string} category */
  function categoryIcon(category) {
    if (category === 'oauth') return KeyRound;
    if (category === 'scheduler') return Clock3;
    if (category === 'runtime') return Activity;
    return ShieldCheck;
  }

  /** @param {string} outcome */
  function outcomeIcon(outcome) {
    if (outcome === 'success') return CircleCheck;
    if (outcome === 'partial') return TriangleAlert;
    return CircleX;
  }

  /** @param {string} outcome */
  function outcomeClass(outcome) {
    if (outcome === 'success') return 'text-[#0a7a5e]';
    if (outcome === 'partial') return 'text-amber-700';
    return 'text-red-700';
  }

  /** @param {string} outcome */
  function outcomeBadgeClass(outcome) {
    if (outcome === 'success') return 'border-[#cfe9df] bg-[#e8f5f0] text-[#0a7a5e]';
    if (outcome === 'partial') return 'border-amber-200 bg-amber-50 text-amber-700';
    return 'border-red-200 bg-red-50 text-red-700';
  }

  /** @param {string} value */
  function titleCase(value) {
    if (!value) return '-';
    return value
      .split(/[._-]/)
      .filter(Boolean)
      .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
      .join(' ');
  }

  /** @param {SystemEvent} event */
  function actorLabel(event) {
    return event.actor.name || titleCase(event.actor.type);
  }

  /** @param {SystemEvent} event */
  function targetLabel(event) {
    if (event.target.name) return event.target.name;
    if (event.target.type && event.target.id) return `${titleCase(event.target.type)} ${event.target.id}`;
    return event.target.type ? titleCase(event.target.type) : 'System';
  }

  /** @param {Record<string, unknown>} metadata */
  function metadataEntries(metadata) {
    /** @type {Array<[string, string]>} */
    const entries = [];
    flattenMetadata(metadata ?? {}, '', entries, 0);
    return entries;
  }

  /** @param {unknown} value @param {string} prefix @param {Array<[string, string]>} entries @param {number} depth */
  function flattenMetadata(value, prefix, entries, depth) {
    if (entries.length >= 50) return;
    if (value === null || typeof value !== 'object') {
      entries.push([prefix || 'value', formatMetadataValue(value)]);
      return;
    }
    if (Array.isArray(value)) {
      entries.push([prefix || 'value', value.map(formatMetadataValue).join(', ') || '-']);
      return;
    }
    for (const [key, nested] of Object.entries(value)) {
      const path = prefix ? `${prefix}.${key}` : key;
      if (nested && typeof nested === 'object' && !Array.isArray(nested) && depth < 2) {
        flattenMetadata(nested, path, entries, depth + 1);
      } else {
        entries.push([path, formatMetadataValue(nested)]);
      }
      if (entries.length >= 50) return;
    }
  }

  /** @param {unknown} value @returns {string} */
  function formatMetadataValue(value) {
    if (value === null || value === undefined || value === '') return '-';
    if (Array.isArray(value)) return value.map(formatMetadataValue).join(', ') || '-';
    if (typeof value === 'object') return 'Structured value';
    return String(value);
  }

  $effect(() => {
    if (!session.authenticated) {
      appliedSearch = null;
      return;
    }
    if (appliedSearch !== page.url.search) {
      appliedSearch = page.url.search;
      applyURLFilters(page.url.search);
      void loadSystemEvents();
    }
  });
</script>

<svelte:window onkeydown={handleKeydown} />

<AuthGate>
  <div class="ui-page">
    <header class="ui-page-header">
      <div class="flex items-start justify-between gap-4">
        <div class="ui-page-heading">
          <h1 class="ui-page-title">System logs</h1>
          <p class="ui-page-description">Administrative changes, OAuth activity, security events, and background work.</p>
        </div>
        <button
          class="ui-button ui-button--icon ui-button--secondary shrink-0 rounded-md border border-[#e5e5e5] bg-white text-[#6e6e6e] hover:bg-[#f5f5f5] hover:text-[#0d0d0d] disabled:cursor-not-allowed disabled:opacity-60"
          type="button"
          aria-label="Refresh system logs"
          title="Refresh system logs"
          disabled={systemEvents.loading || systemEvents.loadingOlder}
          onclick={() => void refreshSystemEvents()}
        >
          <RefreshCw class={systemEvents.loading ? 'size-4 animate-spin motion-reduce:animate-none' : 'size-4'} aria-hidden="true" />
        </button>
      </div>
    </header>

    <section aria-labelledby="system-log-filters-title">
      <h2 id="system-log-filters-title" class="sr-only">System log filters</h2>
      <form class="grid gap-3" onsubmit={applyFilters}>
        <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
          <label class="grid gap-1 text-xs font-medium text-[#3c3c3c] lg:col-span-2">
            Search
            <span class="relative">
              <Search class="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-[#9b9b9b]" aria-hidden="true" />
              <input class="w-full rounded-lg border border-[#e5e5e5] bg-white py-2 pl-8 pr-3 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={systemEvents.query} maxlength="200" placeholder="Action, actor, target, error, or message" />
            </span>
          </label>
          <label class="grid gap-1 text-xs font-medium text-[#3c3c3c]">
            Category
            <select class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={systemEvents.category}>
              <option value="all">All categories</option>
              {#each categories as category}<option value={category}>{titleCase(category)}</option>{/each}
            </select>
          </label>
          <label class="grid gap-1 text-xs font-medium text-[#3c3c3c]">
            Outcome
            <select class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={systemEvents.outcome}>
              <option value="all">All outcomes</option>
              {#each outcomes as outcome}<option value={outcome}>{titleCase(outcome)}</option>{/each}
            </select>
          </label>
          <label class="grid gap-1 text-xs font-medium text-[#3c3c3c]">
            Time range
            <select class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={timeRange}>
              {#each timeRanges as range}<option value={range.value}>{range.label}</option>{/each}
              {#if timeRange === 'custom'}<option value="custom">Custom timestamp</option>{/if}
            </select>
          </label>
        </div>

        <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
          <label class="grid gap-1 text-xs font-medium text-[#3c3c3c]">
            Severity
            <select class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={systemEvents.severity}>
              <option value="all">All severities</option>
              {#each severities as severity}<option value={severity}>{titleCase(severity)}</option>{/each}
            </select>
          </label>
          <label class="grid gap-1 text-xs font-medium text-[#3c3c3c]">
            Action
            <input class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={systemEvents.action} maxlength="120" placeholder="api_key.created" />
          </label>
          <label class="grid gap-1 text-xs font-medium text-[#3c3c3c]">
            Actor
            <input class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={systemEvents.actor} maxlength="120" placeholder="admin" />
          </label>
          <label class="grid gap-1 text-xs font-medium text-[#3c3c3c]">
            Target type
            <input class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={systemEvents.targetType} maxlength="120" placeholder="provider_account" />
          </label>
          <label class="grid gap-1 text-xs font-medium text-[#3c3c3c]">
            Target ID
            <input class="rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={systemEvents.targetId} maxlength="128" placeholder="42" />
          </label>
        </div>

        <div class="flex flex-wrap items-center justify-between gap-3">
          <p class="text-xs text-[#6e6e6e]">
            {activeFilterCount === 0 ? 'Showing all retained events' : `${activeFilterCount} active ${activeFilterCount === 1 ? 'filter' : 'filters'}`}
          </p>
          <div class="flex items-center gap-2">
            {#if activeFilterCount > 0}
              <button class="ui-button ui-button--sm ui-button--secondary rounded-md border border-[#e5e5e5] bg-white px-3 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]" type="button" onclick={() => void resetFilters()}>Reset</button>
            {/if}
            <button class="ui-button ui-button--sm ui-button--primary rounded-md bg-[#0d0d0d] px-3 text-xs font-medium text-white hover:bg-[#3c3c3c]" type="submit">Apply filters</button>
          </div>
        </div>
      </form>
    </section>

    <section aria-labelledby="system-log-results-title">
      <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
        <h2 id="system-log-results-title" class="text-lg font-semibold text-[#0d0d0d]">Events</h2>
        {#if systemEvents.loading && systemEvents.items.length > 0}
          <p class="text-xs text-[#6e6e6e]" aria-live="polite">Refreshing existing results...</p>
        {/if}
      </div>

      {#if systemEvents.error}
        <div class="mb-3 flex flex-col gap-2 rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 sm:flex-row sm:items-center sm:justify-between" role="alert">
          <span>{systemEvents.error}{systemEvents.items.length > 0 ? ' Existing results may be stale.' : ''}</span>
          <button class="ui-button ui-button--sm ui-button--secondary shrink-0 rounded-md border border-red-200 bg-white px-3 text-xs font-medium text-red-700 hover:bg-red-50" type="button" onclick={() => void refreshSystemEvents()}>Retry</button>
        </div>
      {/if}

      <div class="ui-table-shell">
        <table class="ui-table ui-table--stacked min-w-[1080px] text-center">
          <thead>
            <tr>
              <th class="text-center">Time</th>
              <th class="text-center">Category</th>
              <th class="text-center">Action</th>
              <th class="text-center">Actor</th>
              <th class="text-center">Target</th>
              <th class="text-center">Outcome</th>
              <th class="text-center">Details</th>
            </tr>
          </thead>
          <tbody>
            {#if systemEvents.loading && systemEvents.items.length === 0}
              <tr><td class="ui-table-empty ui-table-empty--loading" colspan="7">Loading system logs...</td></tr>
            {:else if groupedEvents.length === 0}
              <tr><td class="ui-table-empty" colspan="7">No system events match these filters.</td></tr>
            {:else}
              {#each paginatedGroupedEvents as group (group.event.id)}
                {@const event = group.event}
                {@const CategoryIcon = categoryIcon(event.category)}
                {@const OutcomeIcon = outcomeIcon(event.outcome)}
                <tr>
                  <td class="whitespace-nowrap text-center tabular-nums text-[#3c3c3c]" data-label="Time">{formatDate(event.occurredAt)}</td>
                  <td data-label="Category">
                    <div class="flex min-w-0 flex-col items-center">
                      <span class="inline-flex items-center justify-center gap-1.5 rounded-md border border-[#ededed] bg-[#fafafa] px-2 py-1 text-[#3c3c3c]"><CategoryIcon class="size-4 text-[#6e6e6e]" aria-hidden="true" />{titleCase(event.category)}</span>
                      <p class="mt-1 text-xs text-[#8e8e8e]">{titleCase(event.severity)}</p>
                    </div>
                  </td>
                  <td class="font-mono text-[13px] text-[#0d0d0d]" data-label="Action">
                    <span class="mx-auto block max-w-[260px] break-words leading-5">{event.action}</span>
                    {#if group.children.length > 0}
                      <button class="ui-button ui-button--xs mx-auto mt-1 inline-flex items-center gap-1 text-xs text-[#6e6e6e] hover:text-[#0d0d0d]" type="button" aria-expanded={expandedBatchIds[group.batchId] === true} onclick={() => toggleBatch(group.batchId)}>
                        {#if expandedBatchIds[group.batchId]}<ChevronDown class="size-3.5" aria-hidden="true" />{:else}<ChevronRight class="size-3.5" aria-hidden="true" />{/if}
                        {group.children.length} related {group.children.length === 1 ? 'event' : 'events'}
                      </button>
                    {/if}
                  </td>
                  <td data-label="Actor">
                    <div class="flex min-w-0 flex-col items-center">
                      <span class="font-medium text-[#3c3c3c]">{actorLabel(event)}</span>
                      <p class="mt-1 text-xs text-[#8e8e8e]">{titleCase(event.actor.type)}</p>
                    </div>
                  </td>
                  <td data-label="Target">
                    <div class="flex min-w-0 flex-col items-center">
                      <span class="block max-w-[220px] truncate text-[#3c3c3c]" title={targetLabel(event)}>{targetLabel(event)}</span>
                      {#if event.target.type}<p class="mt-1 max-w-[220px] truncate font-mono text-xs text-[#8e8e8e]" title={`${event.target.type}${event.target.id ? ` / ${event.target.id}` : ''}`}>{event.target.type}{event.target.id ? ` / ${event.target.id}` : ''}</p>{/if}
                    </div>
                  </td>
                  <td data-label="Outcome">
                    <span class={['inline-flex items-center justify-center gap-1.5 rounded-md border px-2 py-1 font-medium', outcomeBadgeClass(event.outcome)]}><OutcomeIcon class="size-4" aria-hidden="true" />{titleCase(event.outcome)}</span>
                  </td>
                  <td class="text-center" data-label="Details">
                    <button class="ui-button ui-button--icon ui-button--secondary mx-auto rounded-md border border-[#e5e5e5] bg-white text-[#6e6e6e] hover:bg-[#f5f5f5] hover:text-[#0d0d0d]" type="button" aria-label={`View details for ${event.action}`} title="View event details" onclick={() => openDetails(event)}><Eye class="size-4" aria-hidden="true" /></button>
                  </td>
                </tr>
                {#if group.children.length > 0 && expandedBatchIds[group.batchId]}
                  {#each group.children as child (child.id)}
                    {@const ChildCategoryIcon = categoryIcon(child.category)}
                    {@const ChildOutcomeIcon = outcomeIcon(child.outcome)}
                    <tr class="bg-[#fafafa]">
                      <td class="whitespace-nowrap tabular-nums text-[#3c3c3c]" data-label="Time">{formatDate(child.occurredAt)}</td>
                      <td data-label="Category"><span class="inline-flex items-center justify-center gap-1.5 rounded-md border border-[#ededed] bg-white px-2 py-1 text-[#3c3c3c]"><ChildCategoryIcon class="size-4 text-[#6e6e6e]" aria-hidden="true" />{titleCase(child.category)}</span></td>
                      <td class="font-mono text-[13px] text-[#0d0d0d]" data-label="Action"><span class="mx-auto block max-w-[260px] break-words">{child.action}</span></td>
                      <td data-label="Actor">{actorLabel(child)}</td>
                      <td data-label="Target"><span class="mx-auto block max-w-[220px] truncate" title={targetLabel(child)}>{targetLabel(child)}</span></td>
                      <td data-label="Outcome"><span class={['inline-flex items-center justify-center gap-1.5 rounded-md border px-2 py-1 font-medium', outcomeBadgeClass(child.outcome)]}><ChildOutcomeIcon class="size-4" aria-hidden="true" />{titleCase(child.outcome)}</span></td>
                      <td class="text-center" data-label="Details"><button class="ui-button ui-button--icon ui-button--secondary mx-auto rounded-md border border-[#e5e5e5] bg-white text-[#6e6e6e] hover:bg-[#f5f5f5] hover:text-[#0d0d0d]" type="button" aria-label={`View details for ${child.action}`} title="View event details" onclick={() => openDetails(child)}><Eye class="size-4" aria-hidden="true" /></button></td>
                    </tr>
                  {/each}
                {/if}
              {/each}
            {/if}
          </tbody>
        </table>
      </div>

      <div class="ui-pagination mt-4 flex flex-col gap-3 text-sm text-[#6e6e6e] sm:flex-row sm:items-center sm:justify-between">
        <p>
          Showing {eventPageSummary} of {groupedEvents.length} loaded {groupedEvents.length === 1 ? 'event' : 'events'}
          {#if systemEvents.hasMore}<span class="text-[#8e8e8e]"> · More available</span>{/if}
        </p>
        <div class="flex flex-wrap items-center gap-2">
          <label class="inline-flex items-center gap-2 text-xs font-medium text-[#3c3c3c]">
            Rows
            <select
              class="rounded-lg border border-[#e5e5e5] bg-white px-2 py-1.5 text-xs text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
              bind:value={eventPageSize}
              onchange={() => {
                eventPage = 1;
              }}
            >
              <option value={5}>5</option>
              <option value={10}>10</option>
              <option value={20}>20</option>
            </select>
          </label>
          <span class="text-xs tabular-nums text-[#6e6e6e]">Page {normalizedEventPage} of {eventPageCount}</span>
          <button
            class="ui-button ui-button--sm ui-button--secondary rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
            type="button"
            disabled={normalizedEventPage <= 1}
            onclick={() => goToEventPage(normalizedEventPage - 1)}
          >
            Previous
          </button>
          <button
            class="ui-button ui-button--sm ui-button--secondary rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1.5 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:text-[#9b9b9b]"
            type="button"
            disabled={normalizedEventPage >= eventPageCount}
            onclick={() => goToEventPage(normalizedEventPage + 1)}
          >
            Next
          </button>
          {#if systemEvents.hasMore}
            <button class="ui-button ui-button--sm ui-button--secondary rounded-md border border-[#e5e5e5] bg-white px-3 text-xs font-medium text-[#0d0d0d] hover:bg-[#f5f5f5] disabled:cursor-not-allowed disabled:opacity-60" type="button" disabled={systemEvents.loadingOlder || systemEvents.loading} onclick={() => void loadSystemEvents({ append: true })}>
              {systemEvents.loadingOlder ? 'Loading...' : 'Load older'}
            </button>
          {/if}
        </div>
      </div>
    </section>
  </div>
</AuthGate>

{#if selectedEvent}
  <!-- svelte-ignore a11y_click_events_have_key_events,a11y_no_static_element_interactions -->
  <div class="ui-modal-backdrop ui-modal-backdrop--top fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/30 px-4 py-[6vh]" role="dialog" aria-modal="true" aria-labelledby="system-event-detail-title" tabindex="-1" onclick={(event) => event.target === event.currentTarget && closeDetails()}>
    <section class="ui-modal-panel ui-modal-panel--lg max-h-[88vh] w-full max-w-3xl overflow-y-auto rounded-xl border border-[#ededed] bg-white p-5 shadow-xl">
      <div class="flex items-start justify-between gap-4">
        <div class="min-w-0">
          <h2 id="system-event-detail-title" class="text-lg font-semibold text-[#0d0d0d]">Event details</h2>
          <p class="mt-1 break-words font-mono text-[13px] text-[#6e6e6e]">{selectedEvent.action}</p>
        </div>
        <button class="ui-button ui-button--icon shrink-0 rounded-md text-[#6e6e6e] hover:bg-[#f5f5f5] hover:text-[#0d0d0d]" type="button" aria-label="Close system event details" onclick={closeDetails}><X class="size-4" aria-hidden="true" /></button>
      </div>

      <dl class="mt-5 grid gap-x-6 gap-y-4 sm:grid-cols-2">
        <div><dt class="text-xs font-medium text-[#6e6e6e]">Time</dt><dd class="mt-1 text-sm text-[#0d0d0d]">{formatDate(selectedEvent.occurredAt)}</dd></div>
        <div><dt class="text-xs font-medium text-[#6e6e6e]">Outcome</dt><dd class={['mt-1 text-sm font-medium', outcomeClass(selectedEvent.outcome)]}>{titleCase(selectedEvent.outcome)} / {titleCase(selectedEvent.severity)}</dd></div>
        <div><dt class="text-xs font-medium text-[#6e6e6e]">Actor</dt><dd class="mt-1 text-sm text-[#0d0d0d]">{actorLabel(selectedEvent)} ({titleCase(selectedEvent.actor.type)})</dd></div>
        <div><dt class="text-xs font-medium text-[#6e6e6e]">Target</dt><dd class="mt-1 break-words text-sm text-[#0d0d0d]">{targetLabel(selectedEvent)}</dd></div>
        <div class="sm:col-span-2"><dt class="text-xs font-medium text-[#6e6e6e]">Correlation ID</dt><dd class="mt-1 break-all font-mono text-[13px] text-[#0d0d0d]">{selectedEvent.correlationId || '-'}</dd></div>
        <div><dt class="text-xs font-medium text-[#6e6e6e]">Source IP</dt><dd class="mt-1 font-mono text-[13px] text-[#0d0d0d]">{selectedEvent.sourceIp || '-'}</dd></div>
        <div><dt class="text-xs font-medium text-[#6e6e6e]">Route</dt><dd class="mt-1 break-words font-mono text-[13px] text-[#0d0d0d]">{selectedEvent.httpMethod || '-'} {selectedEvent.routePattern || '-'}</dd></div>
        <div><dt class="text-xs font-medium text-[#6e6e6e]">Status code</dt><dd class="mt-1 font-mono text-[13px] tabular-nums text-[#0d0d0d]">{selectedEvent.statusCode ?? '-'}</dd></div>
        <div><dt class="text-xs font-medium text-[#6e6e6e]">Duration</dt><dd class="mt-1 font-mono text-[13px] tabular-nums text-[#0d0d0d]">{selectedEvent.durationMs ?? 0}ms</dd></div>
        <div><dt class="text-xs font-medium text-[#6e6e6e]">Error code</dt><dd class="mt-1 break-words font-mono text-[13px] text-[#0d0d0d]">{selectedEvent.errorCode || '-'}</dd></div>
        <div class="sm:col-span-2"><dt class="text-xs font-medium text-[#6e6e6e]">Message</dt><dd class="mt-1 whitespace-pre-wrap break-words text-sm text-[#0d0d0d]">{selectedEvent.message || '-'}</dd></div>
      </dl>

      <div class="mt-6 border-t border-[#ededed] pt-5">
        <h3 class="text-sm font-semibold text-[#0d0d0d]">Metadata</h3>
        {#if metadataEntries(selectedEvent.metadata).length === 0}
          <p class="mt-2 text-sm text-[#6e6e6e]">No additional metadata.</p>
        {:else}
          <dl class="mt-3 grid gap-x-6 gap-y-3 sm:grid-cols-2">
            {#each metadataEntries(selectedEvent.metadata) as [key, value]}
              <div class="min-w-0">
                <dt class="break-words font-mono text-xs text-[#6e6e6e]">{key}</dt>
                <dd class="mt-1 whitespace-pre-wrap break-words text-sm text-[#0d0d0d]">{value}</dd>
              </div>
            {/each}
          </dl>
        {/if}
      </div>
    </section>
  </div>
{/if}
