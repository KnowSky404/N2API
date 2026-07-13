<script>
  import { page } from '$app/state';
  import {
    apiKeys,
    formatDate,
    loadKeys,
    loadModelRouting,
    loadModelRoutingPreview,
    loadRoutingPools,
    modelRouting,
    modelRoutingPreview,
    routingPools,
    session
  } from '$lib/admin-state.svelte.js';

  import AuthGate from '$lib/AuthGate.svelte';
  let modelRoutingRequested = $state(false);
  let appliedModelRoutingSearch = $state('');
  let modelSearch = $state('');
  let modelStatusFilter = $state('all');
  let modelProviderAccountId = $state('');
  let modelDiagnosticClientKeyId = $state('');

  /** @param {string} search */
  function applyModelRoutingURLFilters(search) {
    const params = new URLSearchParams(search);
    const providerAccountId = params.get('providerAccountId') ?? '';
    modelProviderAccountId = '';
    if (/^[1-9]\d*$/.test(providerAccountId)) {
      modelProviderAccountId = providerAccountId;
    }

    const clientKeyId = params.get('clientKeyId') ?? '';
    modelDiagnosticClientKeyId = '';
    if (/^[1-9]\d*$/.test(clientKeyId)) {
      modelDiagnosticClientKeyId = clientKeyId;
    }

    const model = params.get('model') ?? '';
    if (model.length > 0 && model.length <= 100) {
      modelSearch = model;
    }

    const status = params.get('status') ?? '';
    if (['all', 'routable', 'blocked', 'hidden', 'allowed'].includes(status)) {
      modelStatusFilter = status;
    }

    const previewModel = params.get('previewModel') ?? '';
    if (previewModel.length > 0 && previewModel.length <= 100) {
      modelRoutingPreview.model = previewModel;
    } else if (model.length > 0 && model.length <= 100) {
      modelRoutingPreview.model = model;
    }

    const sessionId = params.get('sessionId') ?? '';
    if (sessionId.length > 0 && sessionId.length <= 100) {
      modelRoutingPreview.sessionId = sessionId;
    }

    const routingPoolId = params.get('routingPoolId') ?? '';
    if (/^[1-9]\d*$/.test(routingPoolId)) {
      modelRoutingPreview.routingPoolId = routingPoolId;
    }

    const excludedAccountIds = params.get('excludedAccountIds') ?? '';
    if (excludedAccountIds.length > 0 && excludedAccountIds.length <= 200) {
      modelRoutingPreview.excludedAccountIds = excludedAccountIds;
    }
  }

  $effect(() => {
    if (!session.authenticated) {
      modelRoutingRequested = false;
      appliedModelRoutingSearch = '';
      modelProviderAccountId = '';
      modelDiagnosticClientKeyId = '';
      return;
    }
    if (appliedModelRoutingSearch !== page.url.search) {
      appliedModelRoutingSearch = page.url.search;
      applyModelRoutingURLFilters(page.url.search);
    }
    if (!modelRoutingRequested) {
      modelRoutingRequested = true;
      void loadModelRouting();
      void loadRoutingPools();
      void loadKeys();
    }
  });

  /** @param {string | null | undefined} value */
  function accountTypeLabel(value) {
    if (value === 'api_upstream') return 'API upstream';
    if (value === 'codex_oauth' || !value) return 'Codex OAuth';
    return value;
  }

  /** @param {string | null | undefined} value */
  function statusLabel(value) {
    return value ? value.replaceAll('_', ' ') : 'active';
  }

  /** @param {number | null | undefined} value */
  function previewConcurrencyLimitLabel(value) {
    const limit = Number(value ?? 0);
    return limit > 0 ? String(limit) : 'unlimited';
  }

  /** @param {string | null | undefined} value */
  function diagnosisStatusLabel(value) {
    if (value === 'routable') return 'Routable';
    if (value === 'degraded') return 'Degraded';
    if (value === 'blocked') return 'Blocked';
    return 'Unknown';
  }

  /** @param {string | null | undefined} value */
  function diagnosisStatusClass(value) {
    if (value === 'routable') return 'rounded-md border border-[#d8ece5] bg-[#e8f5f0] px-2.5 py-1 text-xs font-medium text-[#0a7a5e]';
    if (value === 'degraded') return 'rounded-md border border-amber-200 bg-amber-50 px-2.5 py-1 text-xs font-medium text-amber-800';
    if (value === 'blocked') return 'rounded-md border border-red-200 bg-red-50 px-2.5 py-1 text-xs font-medium text-red-700';
    return 'rounded-md border border-[#e5e5e5] bg-white px-2.5 py-1 text-xs font-medium text-[#6e6e6e]';
  }

  /** @param {{ id?: number | null }} account */
  function providerAccountHref(account) {
    const id = Number(account.id ?? 0);
    if (id <= 0) return '';
    return `/providers?providerAccountId=${encodeURIComponent(String(id))}`;
  }

  /** @param {{ routingPoolId?: number | null }} result */
  function previewRoutingPoolHref(result) {
    const id = Number(result.routingPoolId ?? 0);
    if (id <= 0) return '';
    return `/routing-pools?routingPoolId=${encodeURIComponent(String(id))}`;
  }

  /** @param {string | null | undefined} reason */
  function modelRoutingBlockedReasonHref(reason) {
    const value = String(reason ?? '').trim();
    const params = new URLSearchParams();
    if (value === 'disabled') {
      params.set('status', 'disabled');
    } else if (value === 'rate_limited') {
      params.set('status', 'rate_limited');
    } else if (value === 'circuit_open') {
      params.set('status', 'circuit_open');
    } else if (value === 'expired') {
      params.set('status', 'expired');
    } else {
      params.set('status', 'blocked');
    }
    return `/providers?${params.toString()}`;
  }

  /** @param {{ id?: number | null }} key */
  function diagnosticAPIKeyHref(key) {
    const id = Number(key.id ?? 0);
    if (id <= 0) return '';
    return `/api-keys?clientKeyId=${encodeURIComponent(String(key.id))}`;
  }

  /** @param {import('$lib/admin-state.svelte.js').ModelRoutingAccount} account */
  function routingAccountHoverDetail(account) {
    const lastError = account.lastError
      ? `${account.lastError}${account.lastErrorAt ? ` - ${formatDate(account.lastErrorAt)}` : ''}`
      : '';
    const lastTest = account.lastTestAt
      ? `Last test ${account.lastTestStatus || 'checked'} at ${formatDate(account.lastTestAt)}${account.lastTestError ? `: ${account.lastTestError}` : ''}`
      : '';
    return [
      account.displayName || `Account ${account.id}`,
      accountTypeLabel(account.accountType),
      `Priority ${account.priority}`,
      `Load ${account.loadFactor || 1}`,
      account.schedulable ? statusLabel(account.status) : account.unschedulableReason,
      lastTest,
      account.statusReason,
      lastError
    ]
      .filter(Boolean)
      .join('\n');
  }

  const blockedModels = $derived(modelRouting.models.filter((model) => model.enabledCount === 0));
  const blockedReasonSummary = $derived(
    Array.from(
      blockedModels
        .flatMap((model) => model.accounts ?? [])
        .filter((account) => !account.schedulable && account.unschedulableReason)
        .reduce((counts, account) => {
          const reason = account.unschedulableReason;
          counts.set(reason, (counts.get(reason) ?? 0) + 1);
          return counts;
        }, new Map())
        .entries()
    )
  );
  const selectedPreviewAccount = $derived(
    modelRoutingPreview.result?.candidates.find((account) => account.selected) ??
      modelRoutingPreview.result?.candidates.find((account) => account.id === modelRoutingPreview.result?.selectedAccountId) ??
      null
  );
  const selectedDiagnosticAPIKey = $derived(
    modelDiagnosticClientKeyId
      ? apiKeys.items.find((key) => String(key.id) === modelDiagnosticClientKeyId) ?? null
      : null
  );
  const visibleModelRoutingRows = $derived(
    modelRouting.models.filter((model) => {
      if (!modelMatchesStatusFilter(model, modelStatusFilter)) return false;
      if (modelProviderAccountId && !modelHasVisibleProviderAccount(model)) return false;
      const query = modelSearch.trim().toLowerCase();
      if (!query) return true;
      return modelSearchText(model).includes(query);
    })
  );

  /** @param {import('$lib/admin-state.svelte.js').ModelRoutingModel} model */
  function modelHasVisibleProviderAccount(model) {
    if (!modelProviderAccountId) return true;
    return (model.accounts ?? []).some((account) => String(account.id) === modelProviderAccountId);
  }

  /** @param {import('$lib/admin-state.svelte.js').ModelRoutingModel} model */
  function modelSearchText(model) {
    return [
      model.model,
      model.allowed ? 'allowed' : 'hidden',
      Number(model.enabledCount ?? 0) > 0 ? 'routable' : 'blocked',
      ...(model.accounts ?? []).flatMap((account) => [
        account.displayName,
        `account ${account.id}`,
        accountTypeLabel(account.accountType),
        account.scheduleReason,
        account.unschedulableReason,
        account.status,
        account.statusReason,
        account.lastError
      ])
    ]
      .filter(Boolean)
      .join(' ')
      .toLowerCase();
  }

  /**
   * @param {import('$lib/admin-state.svelte.js').ModelRoutingModel} model
   * @param {string} filter
   */
  function modelMatchesStatusFilter(model, filter) {
    if (filter === 'routable') return Number(model.enabledCount ?? 0) > 0;
    if (filter === 'blocked') return Number(model.enabledCount ?? 0) === 0;
    if (filter === 'hidden') return !model.allowed;
    if (filter === 'allowed') return model.allowed;
    return true;
  }

  /** @param {import('$lib/admin-state.svelte.js').ModelRoutingModel} model */
  function visibleModelAccounts(model) {
    const query = modelSearch.trim().toLowerCase();
    const accounts = (model.accounts ?? []).filter((account) => {
      if (!modelProviderAccountId) return true;
      return String(account.id) === modelProviderAccountId;
    });
    if (!query) return accounts;
    return accounts.filter((account) =>
      [
        account.displayName,
        `account ${account.id}`,
        accountTypeLabel(account.accountType),
        account.scheduleReason,
        account.unschedulableReason,
        account.status,
        account.statusReason,
        account.lastError
      ]
        .filter(Boolean)
        .join(' ')
        .toLowerCase()
        .includes(query)
    );
  }
</script>

<svelte:head>
  <title>N2API Routing Diagnostics</title>
</svelte:head>

<AuthGate>
  <div class="space-y-5">
    <section class="rounded-lg border border-[#ededed] bg-white p-6">
      <div class="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
        <div>
          <h2 class="text-2xl font-semibold leading-tight text-[#0d0d0d]">Routing diagnostics</h2>
          <p class="mt-2 max-w-2xl text-sm leading-6 text-[#3c3c3c]">
            Gateway default model and API key model access are managed from API Keys. Per-account manual models are managed from Provider accounts. This page shows scheduler-visible routing candidates and block reasons.
          </p>
          {#if modelDiagnosticClientKeyId}
            <p class="mt-3 text-sm text-[#6e6e6e]">
              API key scope
              {#if selectedDiagnosticAPIKey}
                <a
                  class="font-medium text-[#0d0d0d] underline-offset-2 hover:underline"
                  href={diagnosticAPIKeyHref(selectedDiagnosticAPIKey)}
                  aria-label="View API key"
                >
                  {selectedDiagnosticAPIKey.name || `Key ${selectedDiagnosticAPIKey.id}`}
                </a>
              {:else}
                Key {modelDiagnosticClientKeyId}
              {/if}
            </p>
          {/if}
        </div>
        <div class="flex flex-wrap gap-2">
          <a
            class="ui-button ui-button--sm ui-button--primary rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white"
            href="/api-keys"
          >
            Open API Keys
          </a>
          <a
            class="ui-button ui-button--sm ui-button--secondary rounded-lg border border-[#e5e5e5] bg-white px-4 py-2 text-sm font-medium text-[#0d0d0d] hover:bg-[#f5f5f5]"
            href="/providers"
          >
            Open Provider accounts
          </a>
        </div>
      </div>
      <div class="mt-5 grid gap-3 sm:grid-cols-3">
        <div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
          <p class="text-xs font-medium text-[#6e6e6e]">Default</p>
          <p class="mt-2 truncate text-sm font-semibold text-[#0d0d0d]">{modelRouting.defaultModel || 'Not set'}</p>
        </div>
        <div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
          <p class="text-xs font-medium text-[#6e6e6e]">Allowed</p>
          <p class="mt-2 text-sm font-semibold text-[#0d0d0d]">{modelRouting.allowedModels.length}</p>
        </div>
        <div class="rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
          <p class="text-xs font-medium text-[#6e6e6e]">Routable</p>
          <p class="mt-2 text-sm font-semibold text-[#0d0d0d]">{modelRouting.models.filter((model) => model.enabledCount > 0).length}</p>
        </div>
      </div>
      <div class="mt-5 rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
        <h3 class="text-sm font-semibold text-[#0d0d0d]">Routing readiness</h3>
        <div class="mt-3 grid gap-3 sm:grid-cols-2">
          <div>
            <p class="text-xs font-medium text-[#6e6e6e]">Blocked models</p>
            <p class="mt-2 text-sm font-semibold text-[#0d0d0d]">{blockedModels.length}</p>
          </div>
          <div>
            <p class="text-xs font-medium text-[#6e6e6e]">Ready models</p>
            <p class="mt-2 text-sm font-semibold text-[#0d0d0d]">{modelRouting.models.length - blockedModels.length}</p>
          </div>
        </div>
        <div class="mt-4">
          <p class="text-xs font-medium text-[#6e6e6e]">Blocked reasons</p>
          {#if blockedReasonSummary.length > 0}
            <div class="mt-2 flex flex-wrap gap-2">
              {#each blockedReasonSummary as [reason, count]}
                <a
                  class="ui-button ui-button--sm rounded-md border border-amber-200 bg-amber-50 px-2.5 py-1 text-xs font-medium text-amber-800 underline-offset-2 hover:underline"
                  href={modelRoutingBlockedReasonHref(reason)}
                  aria-label="View provider accounts with this blocked reason"
                >
                  {statusLabel(reason)}: {count}
                </a>
              {/each}
            </div>
          {:else}
            <p class="mt-2 text-sm text-[#6e6e6e]">No blocked account reasons.</p>
          {/if}
        </div>
        {#if blockedModels.length > 0}
          <p class="mt-3 text-sm text-amber-800 break-words">
            {blockedModels.map((model) => model.model).join(', ')} cannot receive model-routed traffic yet.
          </p>
        {:else}
          <p class="mt-3 text-sm text-[#0a7a5e]">All configured routing models have at least one schedulable account.</p>
        {/if}
      </div>
    </section>

    <section class="rounded-lg border border-[#ededed] bg-white p-6">
      <div class="grid gap-4 grid-cols-1 lg:grid-cols-[minmax(0,1fr)_minmax(320px,420px)]">
        <div>
          <h3 class="text-lg font-semibold text-[#0d0d0d]">Selection preview</h3>
          <p class="mt-1 text-sm leading-6 text-[#6e6e6e]">
            Preview which provider account the gateway would choose for a model and optional sticky session ID without sending traffic or marking an account as used.
          </p>
        </div>
        <form
          class="grid gap-3 rounded-lg border border-[#ededed] bg-[#fafafa] p-4"
          onsubmit={(event) => {
            event.preventDefault();
            loadModelRoutingPreview();
          }}
        >
          <label class="block text-sm font-medium text-[#3c3c3c]">
            Model
            <input
              class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] leading-6 text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
              bind:value={modelRoutingPreview.model}
              placeholder={modelRouting.defaultModel || 'gpt-5'}
              required
            />
          </label>
          <label class="block text-sm font-medium text-[#3c3c3c]">
            Session ID
            <input
              class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] leading-6 text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
              bind:value={modelRoutingPreview.sessionId}
              placeholder="workspace-123"
            />
          </label>
          <label class="block text-sm font-medium text-[#3c3c3c]">
            Routing pool
            <select
              class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm leading-6 text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
              bind:value={modelRoutingPreview.routingPoolId}
              disabled={routingPools.loading}
            >
              <option value="0">Global provider pool</option>
              {#each routingPools.items as pool}
                <option value={String(pool.id)}>{pool.name}</option>
              {/each}
            </select>
          </label>
          <label class="block text-sm font-medium text-[#3c3c3c]">
            Excluded account IDs
            <input
              class="mt-2 w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] leading-6 text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
              bind:value={modelRoutingPreview.excludedAccountIds}
              placeholder="7, 8"
            />
          </label>
          <button
            class="ui-button ui-button--sm ui-button--primary justify-self-start rounded-lg bg-[#0d0d0d] px-4 py-2 text-sm font-medium text-white disabled:cursor-not-allowed disabled:opacity-60"
            disabled={modelRoutingPreview.loading}
          >
            {modelRoutingPreview.loading ? 'Previewing' : 'Preview selection'}
          </button>
        </form>
      </div>

      {#if modelRoutingPreview.error}
        <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">{modelRoutingPreview.error}</p>
      {/if}

      {#if modelRoutingPreview.result}
        <div class="mt-5 rounded-lg border border-[#ededed] bg-[#fafafa] p-4">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div>
              <p class="text-xs font-medium text-[#6e6e6e]">Selected account</p>
              <p class="mt-1 text-sm font-semibold text-[#0d0d0d]">
                {#if selectedPreviewAccount}
                  {selectedPreviewAccount.displayName || `Account ${selectedPreviewAccount.id}`}
                  <span class="font-normal text-[#6e6e6e]">
                    {accountTypeLabel(selectedPreviewAccount.accountType)} · ID {selectedPreviewAccount.id}
                  </span>
                {:else if modelRoutingPreview.result.selectedAccountId}
                  Account {modelRoutingPreview.result.selectedAccountId}
                {:else}
                  No schedulable account
                {/if}
                {#if modelRoutingPreview.result.sessionId}
                  for session {modelRoutingPreview.result.sessionId}
                {/if}
                {#if modelRoutingPreview.excludedAccountIds.trim()}
                  excluding {modelRoutingPreview.excludedAccountIds}
                {/if}
                {#if modelRoutingPreview.result.routingPoolId}
                  in routing pool
                  <a
                    class="underline-offset-2 hover:underline"
                    href={previewRoutingPoolHref(modelRoutingPreview.result)}
                    aria-label="View routing pool"
                  >
                    {modelRoutingPreview.result.routingPoolName || modelRoutingPreview.result.routingPoolId}
                  </a>
                {/if}
              </p>
              {#if modelRoutingPreview.result.routingPoolFallbackChain}
                <p class="mt-1 text-xs text-[#6e6e6e]">
                  Routing pool chain {modelRoutingPreview.result.routingPoolFallbackChain}
                </p>
              {/if}
            </div>
            <span class="rounded-md border border-[#d8ece5] bg-[#e8f5f0] px-2.5 py-1 text-xs font-medium text-[#0a7a5e]">
              Model {modelRoutingPreview.result.model}
            </span>
          </div>
          {#if modelRoutingPreview.result.diagnosisStatus || modelRoutingPreview.result.diagnosisSummary}
            <div class="mt-4 rounded-lg border border-[#e5e5e5] bg-white p-3">
              <p class="text-xs font-medium text-[#6e6e6e]">Routing diagnosis</p>
              <div class="mt-2 flex flex-wrap items-center gap-2">
                <span class={diagnosisStatusClass(modelRoutingPreview.result.diagnosisStatus)}>
                  {diagnosisStatusLabel(modelRoutingPreview.result.diagnosisStatus)}
                </span>
                {#if modelRoutingPreview.result.diagnosisSummary}
                  <p class="min-w-[220px] flex-1 text-sm leading-6 text-[#3c3c3c]">{modelRoutingPreview.result.diagnosisSummary}</p>
                {/if}
              </div>
              {#if modelRoutingPreview.result.diagnosisHints?.length}
                <div class="mt-3">
                  <p class="text-xs font-medium text-[#6e6e6e]">Repair hints</p>
                  <div class="mt-2 flex flex-wrap gap-2">
                    {#each modelRoutingPreview.result.diagnosisHints as hint}
                      <span class="rounded-md border border-[#e5e5e5] bg-[#fafafa] px-2.5 py-1 text-xs leading-5 text-[#3c3c3c]">{hint}</span>
                    {/each}
                  </div>
                </div>
              {/if}
              {#if modelRoutingPreview.result.blockedReasonCounts?.length}
                <div class="mt-3">
                  <p class="text-xs font-medium text-[#6e6e6e]">Blocked reasons</p>
                  <div class="mt-2 flex flex-wrap gap-2">
                    {#each modelRoutingPreview.result.blockedReasonCounts as reason}
                      <a
                        class="ui-button ui-button--sm rounded-md border border-amber-200 bg-amber-50 px-2.5 py-1 text-xs leading-5 text-amber-800 underline-offset-2 hover:underline"
                        href={modelRoutingBlockedReasonHref(reason.reason)}
                        aria-label="View provider accounts with this blocked reason"
                      >
                        {reason.reason}: {reason.count}
                      </a>
                    {/each}
                  </div>
                </div>
              {/if}
            </div>
          {/if}
          <div class="mt-4 flex flex-wrap gap-2">
            {#each modelRoutingPreview.result.candidates as account}
              <span
                class={[
                  'inline-flex max-w-[400px] flex-wrap items-center gap-2 rounded-md border px-2.5 py-1.5 text-xs',
                  account.selected
                    ? 'border-[#d8ece5] bg-[#e8f5f0] text-[#0a7a5e]'
                    : account.schedulable === false
                      ? 'border-amber-200 bg-amber-50 text-amber-800'
                    : 'border-[#ededed] bg-white text-[#3c3c3c]'
                ]}
              >
                <span class="font-mono text-[11px] font-semibold text-[#0d0d0d]">
                  {account.schedulable === false ? 'Blocked' : `Rank #${account.scheduleRank}`}
                </span>
                <a
                  class="truncate font-medium text-[#0d0d0d] underline-offset-2 hover:underline"
                  href={providerAccountHref(account)}
                  aria-label="View provider account"
                >
                  {account.displayName || `Account ${account.id}`}
                </a>
                <span>{accountTypeLabel(account.accountType)}</span>
                <span>Priority {account.priority}</span>
                <span>Load {account.loadFactor || 1}</span>
                <span>Active {account.currentConcurrentRequests || 0} / {previewConcurrencyLimitLabel(account.effectiveMaxConcurrentRequests)}</span>
                {#if account.scheduleReason}
                  <span>Schedule reason {account.scheduleReason}</span>
                {/if}
                <span>Used {formatDate(account.lastUsedAt)}</span>
                {#if account.lastTestAt}
                  <span>Test {account.lastTestStatus || 'checked'} {formatDate(account.lastTestAt)}</span>
                {/if}
                {#if account.lastTestError}
                  <span class="font-medium text-amber-800">{account.lastTestError}</span>
                {/if}
                {#if account.selected}
                  <span class="font-medium text-[#0a7a5e]">Selected</span>
                {/if}
                {#if account.stickyBound}
                  <span class="font-medium text-[#0a7a5e]">Sticky bound</span>
                {/if}
                {#if account.concurrencyBlocked}
                  <span class="font-medium text-amber-800">Concurrency full</span>
                {/if}
                {#if account.schedulable === false && account.unschedulableReason}
                  <span class="font-medium text-amber-800">{account.unschedulableReason}</span>
                {/if}
              </span>
            {/each}
          </div>
        </div>
      {/if}
    </section>

    <section class="rounded-lg border border-[#ededed] bg-white p-6">
      <div class="flex items-start justify-between gap-4">
        <div>
          <h3 class="text-lg font-semibold text-[#0d0d0d]">Routing candidates</h3>
          <p class="mt-1 text-sm text-[#6e6e6e]">Candidate accounts are ordered the same way the gateway scheduler will consider them.</p>
        </div>
        {#if modelRouting.loading}
          <span class="ui-loading-state text-sm text-[#6e6e6e]" aria-live="polite">Loading...</span>
        {/if}
      </div>

      {#if modelRouting.error}
        <p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700">{modelRouting.error}</p>
      {/if}

      {#if modelRouting.warnings.length}
        <div class="mt-4 space-y-2">
          {#each modelRouting.warnings as warning}
            <p class="rounded-md border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800">{warning}</p>
          {/each}
        </div>
      {/if}

      <div class="mt-5 grid gap-3 grid-cols-1 sm:grid-cols-[minmax(240px,1fr)_220px]">
        <label class="grid gap-1 text-sm font-medium text-[#3c3c3c]">
          Search models
          <input
            class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
            type="search"
            placeholder="Search models or accounts"
            bind:value={modelSearch}
          />
        </label>
        <label class="grid gap-1 text-sm font-medium text-[#3c3c3c]">
          Status filter
          <select
            class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]"
            bind:value={modelStatusFilter}
          >
            <option value="all">All models</option>
            <option value="routable">Routable models</option>
            <option value="blocked">Blocked models</option>
            <option value="hidden">Hidden models</option>
            <option value="allowed">Allowed models</option>
          </select>
        </label>
      </div>

      <div class="ui-table-shell mt-5 overflow-x-auto rounded-lg border border-[#ededed]">
        <table class="ui-table min-w-full divide-y divide-[#ededed] text-left text-sm">
          <thead class="bg-[#fafafa] text-xs text-[#6e6e6e]">
            <tr>
              <th class="px-4 py-3 font-medium">Model</th>
              <th class="px-4 py-3 font-medium">Policy</th>
              <th class="px-4 py-3 font-medium">Accounts</th>
              <th class="px-4 py-3 font-medium">Candidates</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-[#ededed]">
            {#if modelRouting.loading && modelRouting.models.length === 0}
              <tr>
                <td class="ui-table-empty ui-table-empty--loading px-4 py-5 text-[#6e6e6e]" colspan="4">Loading model routing...</td>
              </tr>
            {:else if modelRouting.models.length === 0}
              <tr>
                <td class="ui-table-empty px-4 py-5 text-[#6e6e6e]" colspan="4">No model routing policy configured yet.</td>
              </tr>
            {:else if visibleModelRoutingRows.length === 0}
              <tr>
                <td class="ui-table-empty px-4 py-5 text-[#6e6e6e]" colspan="4">No model routing rows match your filters.</td>
              </tr>
            {:else}
              {#each visibleModelRoutingRows as model}
                <tr class="align-top">
                  <td class="px-4 py-4">
                    <p class="font-medium text-[#0d0d0d]">{model.model}</p>
                    {#if model.enabledCount === 0}
                      <p class="mt-1 text-xs text-amber-700">No schedulable account</p>
                    {/if}
                  </td>
                  <td class="px-4 py-4 text-[#3c3c3c]">
                    {model.allowed ? 'Allowed' : 'Hidden'}
                  </td>
                  <td class="px-4 py-4 text-[#3c3c3c]">
                    {model.enabledCount} / {model.configuredCount}
                  </td>
                  <td class="px-4 py-4">
                    {#if visibleModelAccounts(model).length}
                      <div class="flex flex-wrap gap-2">
                        {#each visibleModelAccounts(model) as account}
                          <span
                            class={[
                              'inline-flex max-w-[400px] flex-wrap items-center gap-2 rounded-md border px-2.5 py-1.5 text-xs',
                              account.schedulable
                                ? 'border-[#ededed] bg-[#fafafa] text-[#3c3c3c]'
                                : 'border-amber-200 bg-amber-50 text-amber-800'
                            ]}
                            title={routingAccountHoverDetail(account)}
                          >
                            <span class="font-mono text-[11px] font-semibold text-[#0d0d0d]">Schedule rank #{account.scheduleRank}</span>
                            <a
                              class="truncate font-medium text-[#0d0d0d] underline-offset-2 hover:underline"
                              href={providerAccountHref(account)}
                              aria-label="View provider account"
                            >
                              {account.displayName || `Account ${account.id}`}
                            </a>
                            <span class="text-[#6e6e6e]">{accountTypeLabel(account.accountType)}</span>
                            <span class="text-[#6e6e6e]">Priority {account.priority}</span>
                            <span class="text-[#6e6e6e]">Load {account.loadFactor || 1}</span>
                            <span class="text-[#6e6e6e]">Used {formatDate(account.lastUsedAt)}</span>
                            {#if account.lastTestAt}
                              <span class="text-[#6e6e6e]">Test {account.lastTestStatus || 'checked'} {formatDate(account.lastTestAt)}</span>
                            {/if}
                            {#if account.lastTestError}
                              <span class="font-medium text-amber-800">{account.lastTestError}</span>
                            {/if}
                            <span class={account.schedulable ? 'text-[#6e6e6e]' : 'font-medium text-amber-800'}>
                              {account.schedulable ? statusLabel(account.status) : account.unschedulableReason}
                            </span>
                          </span>
                        {/each}
                      </div>
                    {:else}
                      <span class="text-sm text-[#6e6e6e]">No candidates</span>
                    {/if}
                  </td>
                </tr>
              {/each}
            {/if}
          </tbody>
        </table>
      </div>
    </section>
  </div>
</AuthGate>
