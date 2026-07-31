<script>
  import { onMount } from 'svelte';
  import {
    ArrowUpRight,
    Check,
    Copy,
    LoaderCircle,
    PackageOpen,
    RefreshCw,
    X
  } from 'lucide-svelte';
  import { copyText } from './clipboard.js';
  import { formatDate, loadUpdateStatus, refreshUpdateStatus, session, updateStatus } from './admin-state.svelte.js';
  import { renderReleaseNotes } from './release-notes.js';
  import { dismissRelease, isReleaseDismissed, readDismissedRelease } from './update-dismissal.js';

  let { modalOpen = false, onopen = () => {}, onclose = () => {} } = $props();
  let dismissedVersion = $state('');
  let copied = $state(false);
  let closeButton = $state(/** @type {HTMLButtonElement | null} */ (null));
  let noticeButton = $state(/** @type {HTMLButtonElement | null} */ (null));
  const latest = $derived(updateStatus.latest);
  const renderedNotes = $derived(renderReleaseNotes(latest?.notes ?? ''));
  const showNotice = $derived(
    session.authenticated &&
    updateStatus.status === 'update_available' &&
    Boolean(latest?.version) &&
    !isReleaseDismissed(latest?.version ?? '', dismissedVersion)
  );

  onMount(() => {
    dismissedVersion = readDismissedRelease(localStorage);
    const timer = setInterval(() => {
      if (session.authenticated) void loadUpdateStatus();
    }, 5 * 60 * 1000);
    return () => clearInterval(timer);
  });

  $effect(() => {
    if (modalOpen && closeButton) closeButton.focus();
  });

  function openDetails() {
    copied = false;
    onopen();
  }

  function closeDetails() {
    onclose();
    queueMicrotask(() => noticeButton?.focus());
  }

  function dismissCurrentRelease() {
    if (!latest?.version) return;
    dismissRelease(latest.version, localStorage);
    dismissedVersion = latest.version;
  }

  async function copyImage() {
    if (!latest?.image) return;
    copied = await copyText(latest.image);
  }

  async function refresh() {
    copied = false;
    await refreshUpdateStatus();
  }
</script>

{#if showNotice && latest}
  <section class="flex flex-col gap-3 rounded-lg border border-[#cce7de] bg-[#f5fbf9] px-4 py-3 sm:flex-row sm:items-center" aria-labelledby="release-update-notice-title">
    <div class="flex min-w-0 flex-1 items-start gap-3 sm:items-center">
      <span class="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-lg bg-[#e8f5f0] text-[#0a7a5e] sm:mt-0">
        <PackageOpen class="size-4" aria-hidden="true" />
      </span>
      <div class="min-w-0">
        <p id="release-update-notice-title" class="text-sm font-semibold text-[#0d0d0d]">N2API {latest.version} is available</p>
        <p class="mt-0.5 text-xs text-[#6e6e6e]">Published {formatDate(latest.publishedAt)}</p>
      </div>
    </div>
    <div class="flex shrink-0 items-center gap-2 pl-11 sm:pl-0">
      <button class="ui-button ui-button--sm ui-button--secondary" type="button" bind:this={noticeButton} onclick={openDetails}>What's new</button>
      <button class="ui-button ui-button--icon" type="button" onclick={dismissCurrentRelease} title="Dismiss this release" aria-label={`Dismiss release ${latest.version}`}>
        <X class="size-4" aria-hidden="true" />
      </button>
    </div>
  </section>
{/if}

{#if modalOpen && session.authenticated}
  <div class="ui-modal-backdrop ui-modal-backdrop--top" role="dialog" aria-modal="true" aria-labelledby="release-update-title" aria-describedby="release-update-description">
    <section class="ui-modal-panel ui-modal-panel--xl max-h-[calc(100dvh-2rem)] overflow-y-auto">
      <header class="flex items-start justify-between gap-4">
        <div class="min-w-0">
          <div class="flex flex-wrap items-center gap-2">
            <h2 id="release-update-title" class="text-lg font-semibold text-[#0d0d0d]">Release updates</h2>
            {#if latest}
              <span class="rounded-md bg-[#f5f5f5] px-2 py-1 font-mono text-xs text-[#3c3c3c]">{latest.version}</span>
            {/if}
            {#if updateStatus.stale}
              <span class="rounded-md bg-amber-50 px-2 py-1 text-xs font-medium text-amber-800">Cached</span>
            {/if}
          </div>
          <p id="release-update-description" class="mt-1 text-sm text-[#6e6e6e]">Official N2API release status and change history.</p>
        </div>
        <div class="flex shrink-0 items-center gap-1">
          <button class="ui-button ui-button--icon" type="button" disabled={updateStatus.refreshing} onclick={refresh} title="Check for updates" aria-label="Check for release updates">
            <RefreshCw class={updateStatus.refreshing ? 'size-4 animate-spin' : 'size-4'} aria-hidden="true" />
          </button>
          <button class="ui-button ui-button--icon" type="button" bind:this={closeButton} onclick={closeDetails} aria-label="Close release updates">
            <X class="size-4" aria-hidden="true" />
          </button>
        </div>
      </header>

      {#if updateStatus.error}
        <p class="mt-4 rounded-md border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800" role="status">{updateStatus.error}</p>
      {/if}

      {#if updateStatus.loading && !latest}
        <div class="ui-loading-state mt-6"><LoaderCircle class="size-4 animate-spin" aria-hidden="true" /> Loading release status...</div>
      {:else if updateStatus.status === 'disabled'}
        <p class="mt-6 text-sm text-[#6e6e6e]">Release checks are disabled for this deployment.</p>
      {:else if !latest}
        <p class="mt-6 text-sm text-[#6e6e6e]">Release status is not available yet.</p>
      {:else}
        <dl class="mt-6 grid gap-4 border-y border-[#ededed] py-4 sm:grid-cols-2">
          <div class="min-w-0">
            <dt class="text-xs font-medium uppercase text-[#8e8e8e]">Current build</dt>
            <dd class="mt-1 truncate font-mono text-sm text-[#0d0d0d]" title={updateStatus.current?.commit ?? ''}>{updateStatus.current?.version ?? 'Unknown'}</dd>
            {#if updateStatus.current?.builtAt}<dd class="mt-1 text-xs text-[#6e6e6e]">Built {formatDate(updateStatus.current.builtAt)}</dd>{/if}
          </div>
          <div class="min-w-0">
            <dt class="text-xs font-medium uppercase text-[#8e8e8e]">Latest release</dt>
            <dd class="mt-1 font-mono text-sm text-[#0d0d0d]">{latest.version}</dd>
            <dd class="mt-1 text-xs text-[#6e6e6e]">Published {formatDate(latest.publishedAt)}</dd>
          </div>
        </dl>

        <section class="mt-6" aria-labelledby="release-notes-title">
          <h3 id="release-notes-title" class="text-base font-semibold text-[#0d0d0d]">What's new</h3>
          {#if renderedNotes}
            <div class="ui-release-notes mt-4">{@html renderedNotes}</div>
          {:else}
            <p class="mt-3 text-sm text-[#6e6e6e]">No release notes were provided.</p>
          {/if}
        </section>

        <section class="mt-6 border-t border-[#ededed] pt-5" aria-labelledby="release-image-title">
          <h3 id="release-image-title" class="text-sm font-semibold text-[#0d0d0d]">Immutable image</h3>
          <div class="mt-2 flex min-w-0 items-center gap-2 rounded-lg bg-[#f7f7f7] p-2">
            <code class="min-w-0 flex-1 overflow-x-auto whitespace-nowrap px-1 font-mono text-xs text-[#3c3c3c]">{latest.image}</code>
            <button class="ui-button ui-button--icon shrink-0" type="button" onclick={copyImage} title="Copy image reference" aria-label="Copy immutable image reference">
              {#if copied}<Check class="size-4 text-[#0a7a5e]" aria-hidden="true" />{:else}<Copy class="size-4" aria-hidden="true" />{/if}
            </button>
          </div>
          <p class="mt-2 text-xs text-[#6e6e6e]">Deployment remains gated by the existing backup, restore-drill, and upgrade plan workflow.</p>
        </section>

        <footer class="ui-modal-actions">
          <button class="ui-button ui-button--sm ui-button--secondary" type="button" onclick={closeDetails}>Close</button>
          <a class="ui-button ui-button--sm ui-button--primary inline-flex items-center gap-1.5" href={latest.url} target="_blank" rel="noopener noreferrer">
            View on GitHub
            <ArrowUpRight class="size-3.5" aria-hidden="true" />
          </a>
        </footer>
      {/if}
    </section>
  </div>
{/if}
