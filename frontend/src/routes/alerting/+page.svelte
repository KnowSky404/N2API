<script>
  import { onDestroy } from 'svelte';
  import {
    alertActionTests,
    alertActions,
    alertRules,
    createAlertAction,
    createAlertRule,
    deleteAlertAction,
    deleteAlertRule,
    formatDate,
    health,
    loadAlertActions,
    loadAlertRules,
    loadHealth,
    session,
    testAlertAction,
    updateAlertAction,
    updateAlertRule
  } from '$lib/admin-state.svelte.js';
  import AuthGate from '$lib/AuthGate.svelte';
  import {
    BellRing,
    CircleCheck,
    CircleX,
    FlaskConical,
    LoaderCircle,
    Pencil,
    Plus,
    RefreshCw,
    Trash2,
    X
  } from 'lucide-svelte';

  let requested = $state(false);
  let actionModalOpen = $state(false);
  let actionEditingId = $state(0);
  let actionOriginalKind = $state('generic_webhook');
  let actionFormError = $state('');
  let ruleModalOpen = $state(false);
  let ruleEditingId = $state(0);
  let ruleFormError = $state('');
  let deleteTarget = $state(/** @type {{ type: 'action'|'rule', id: number, name: string } | null} */ (null));
  let notice = $state(/** @type {{ kind: 'success'|'warning'|'error', title: string, message: string } | null} */ (null));
  /** @type {ReturnType<typeof setTimeout> | null} */
  let noticeTimer = null;

  const actionDraft = $state(emptyActionDraft());
  const ruleDraft = $state(emptyRuleDraft());
  const delivery = $derived(health.tasks?.alertDelivery ?? null);
  const actionsBusy = $derived(
    alertActions.saving || alertActions.deletingId > 0 || alertActionTests.loading
  );
  const rulesBusy = $derived(alertRules.saving || alertRules.deletingId > 0);
  const deleteBusy = $derived(
    deleteTarget?.type === 'action'
      ? alertActions.deletingId === deleteTarget.id
      : deleteTarget?.type === 'rule' && alertRules.deletingId === deleteTarget.id
  );

  function emptyActionDraft() {
    return {
      name: '',
      kind: /** @type {'generic_webhook'|'ntfy'} */ ('generic_webhook'),
      destination: '',
      enabled: true,
      expectedUpdatedAt: ''
    };
  }

  function emptyRuleDraft() {
    return {
      name: '',
      actionId: 0,
      enabled: true,
      category: '',
      severity: '',
      eventAction: '',
      recoveryAction: '',
      aggregationCount: 1,
      aggregationWindowSeconds: 0,
      cooldownSeconds: 0,
      deduplicationScope: /** @type {'rule'|'target'} */ ('rule'),
      notifyRecovery: false,
      expectedUpdatedAt: ''
    };
  }

  /** @param {{ kind: 'success'|'warning'|'error', title: string, message: string }} next */
  function showNotice(next) {
    notice = next;
    if (noticeTimer) clearTimeout(noticeTimer);
    noticeTimer = setTimeout(() => { notice = null; }, 6000);
  }

  onDestroy(() => {
    if (noticeTimer) clearTimeout(noticeTimer);
  });

  $effect(() => {
    if (!session.authenticated) {
      requested = false;
      actionModalOpen = false;
      ruleModalOpen = false;
      deleteTarget = null;
      return;
    }
    if (!requested) {
      requested = true;
      void Promise.all([loadAlertActions(), loadAlertRules(), loadHealth()]);
    }
  });

  /** @param {KeyboardEvent} event */
  function handleKeydown(event) {
    if (event.key !== 'Escape') return;
    if (actionModalOpen && !alertActions.saving && !alertActionTests.loading) closeActionModal();
    else if (ruleModalOpen && !alertRules.saving) closeRuleModal();
    else if (deleteTarget && !deleteBusy) deleteTarget = null;
  }

  async function refreshAlerting() {
    const [actionsLoaded, rulesLoaded] = await Promise.all([loadAlertActions(), loadAlertRules(), loadHealth()]);
    if (actionsLoaded && rulesLoaded) {
      showNotice({ kind: 'success', title: 'Alerts refreshed', message: 'Actions, rules, and delivery status are current.' });
    }
  }

  function openCreateAction() {
    Object.assign(actionDraft, emptyActionDraft());
    actionEditingId = 0;
    actionOriginalKind = 'generic_webhook';
    actionFormError = '';
    alertActions.error = '';
    actionModalOpen = true;
  }

  /** @param {import('$lib/admin-state.svelte.js').AlertAction} action */
  function openEditAction(action) {
    Object.assign(actionDraft, {
      name: action.name,
      kind: action.kind,
      destination: '',
      enabled: action.enabled,
      expectedUpdatedAt: action.updatedAt
    });
    actionEditingId = action.id;
    actionOriginalKind = action.kind;
    actionFormError = '';
    alertActions.error = '';
    actionModalOpen = true;
  }

  function closeActionModal() {
    if (alertActions.saving || alertActionTests.loading) return;
    actionModalOpen = false;
    actionEditingId = 0;
    actionFormError = '';
    alertActions.error = '';
    Object.assign(actionDraft, emptyActionDraft());
  }

  /** @param {SubmitEvent} event */
  async function saveAction(event) {
    event.preventDefault();
    actionFormError = '';
    const name = actionDraft.name.trim();
    const destination = actionDraft.destination.trim();
    if (!name) {
      actionFormError = 'Action name is required.';
      return;
    }
    if (!actionEditingId && !destination) {
      actionFormError = 'Destination URL is required.';
      return;
    }
    if (actionEditingId && actionDraft.kind !== actionOriginalKind && !destination) {
      actionFormError = 'Enter a new destination when changing the adapter type.';
      return;
    }

    let saved;
    if (actionEditingId) {
      /** @type {{ name: string, kind: 'generic_webhook'|'ntfy', enabled: boolean, expectedUpdatedAt: string, destination?: string }} */
      const input = {
        name,
        kind: actionDraft.kind,
        enabled: actionDraft.enabled,
        expectedUpdatedAt: actionDraft.expectedUpdatedAt
      };
      if (destination) input.destination = destination;
      saved = await updateAlertAction(actionEditingId, input);
    } else {
      saved = await createAlertAction({ name, kind: actionDraft.kind, destination, enabled: actionDraft.enabled });
    }
    if (!saved) return;

    actionEditingId = saved.id;
    actionOriginalKind = saved.kind;
    Object.assign(actionDraft, {
      name: saved.name,
      kind: saved.kind,
      destination: '',
      enabled: saved.enabled,
      expectedUpdatedAt: saved.updatedAt
    });
    showNotice({ kind: 'success', title: 'Action saved', message: `${saved.name} is ready for rules and connectivity tests.` });
  }

  /** @param {import('$lib/admin-state.svelte.js').AlertAction} action */
  async function runActionTest(action) {
    const result = await testAlertAction(action.id, action.updatedAt);
    if (!result) {
      showNotice({ kind: 'error', title: 'Test unavailable', message: alertActionTests.error || 'The saved action could not be tested.' });
      return;
    }
    const refreshed = alertActions.items.find((item) => item.id === action.id);
    if (actionEditingId === action.id && refreshed) actionDraft.expectedUpdatedAt = refreshed.updatedAt;
    if (result.status === 'passed') {
      showNotice({ kind: 'success', title: 'Test delivered', message: `The saved ${actionKindLabel(action.kind)} destination accepted the notification.` });
    } else {
      showNotice({
        kind: result.retryable ? 'warning' : 'error',
        title: 'Test failed',
        message: result.errorCode
          ? `${result.errorCode}${result.retryable ? ' (retryable)' : ''}`
          : 'The destination rejected or did not accept the notification.'
      });
    }
  }

  async function testEditingAction() {
    const action = alertActions.items.find((item) => item.id === actionEditingId);
    if (action) await runActionTest(action);
  }

  function openCreateRule() {
    const draft = emptyRuleDraft();
    draft.actionId = alertActions.items[0]?.id ?? 0;
    Object.assign(ruleDraft, draft);
    ruleEditingId = 0;
    ruleFormError = '';
    alertRules.error = '';
    ruleModalOpen = true;
  }

  /** @param {import('$lib/admin-state.svelte.js').AlertRule} rule */
  function openEditRule(rule) {
    Object.assign(ruleDraft, {
      name: rule.name,
      actionId: rule.actionId,
      enabled: rule.enabled,
      category: rule.category || '',
      severity: rule.severity || '',
      eventAction: rule.eventAction || '',
      recoveryAction: rule.recoveryAction || '',
      aggregationCount: rule.aggregationCount,
      aggregationWindowSeconds: rule.aggregationWindowSeconds,
      cooldownSeconds: rule.cooldownSeconds,
      deduplicationScope: rule.deduplicationScope,
      notifyRecovery: rule.notifyRecovery,
      expectedUpdatedAt: rule.updatedAt
    });
    ruleEditingId = rule.id;
    ruleFormError = '';
    alertRules.error = '';
    ruleModalOpen = true;
  }

  function closeRuleModal() {
    if (alertRules.saving) return;
    ruleModalOpen = false;
    ruleEditingId = 0;
    ruleFormError = '';
    alertRules.error = '';
    Object.assign(ruleDraft, emptyRuleDraft());
  }

  /** @param {SubmitEvent} event */
  async function saveRule(event) {
    event.preventDefault();
    ruleFormError = '';
    const name = ruleDraft.name.trim();
    const eventAction = ruleDraft.eventAction.trim();
    const recoveryAction = ruleDraft.recoveryAction.trim();
    if (!name) {
      ruleFormError = 'Rule name is required.';
      return;
    }
    if (!ruleDraft.actionId) {
      ruleFormError = 'Select a delivery action.';
      return;
    }
    if (!ruleDraft.category && !ruleDraft.severity && !eventAction) {
      ruleFormError = 'Choose at least one category, severity, or event action filter.';
      return;
    }
    if (ruleDraft.notifyRecovery && !recoveryAction) {
      ruleFormError = 'Recovery event action is required when recovery notifications are enabled.';
      return;
    }
    if (Number(ruleDraft.aggregationCount) > 1 && Number(ruleDraft.aggregationWindowSeconds) === 0) {
      ruleFormError = 'Aggregation window must be greater than zero when the threshold is greater than one.';
      return;
    }

    const input = {
      name,
      actionId: Number(ruleDraft.actionId),
      enabled: ruleDraft.enabled,
      category: ruleDraft.category,
      severity: ruleDraft.severity,
      eventAction,
      recoveryAction,
      aggregationCount: Number(ruleDraft.aggregationCount),
      aggregationWindowSeconds: Number(ruleDraft.aggregationWindowSeconds),
      cooldownSeconds: Number(ruleDraft.cooldownSeconds),
      deduplicationScope: ruleDraft.deduplicationScope,
      notifyRecovery: ruleDraft.notifyRecovery
    };
    const saved = ruleEditingId
      ? await updateAlertRule(ruleEditingId, { ...input, expectedUpdatedAt: ruleDraft.expectedUpdatedAt })
      : await createAlertRule(input);
    if (!saved) return;

    ruleEditingId = saved.id;
    Object.assign(ruleDraft, { ...saved, expectedUpdatedAt: saved.updatedAt });
    showNotice({ kind: 'success', title: 'Rule saved', message: `${saved.name} will use the current exact-match filters.` });
  }

  async function confirmDelete() {
    if (!deleteTarget) return;
    const target = deleteTarget;
    const deleted = target.type === 'action'
      ? await deleteAlertAction(target.id)
      : await deleteAlertRule(target.id);
    if (!deleted) return;
    deleteTarget = null;
    showNotice({ kind: 'success', title: `${target.type === 'action' ? 'Action' : 'Rule'} deleted`, message: `${target.name} was removed.` });
  }

  /** @param {number} actionId */
  function actionName(actionId) {
    return alertActions.items.find((action) => action.id === actionId)?.name ?? `Action ${actionId}`;
  }

  /** @param {string} kind */
  function actionKindLabel(kind) {
    return kind === 'ntfy' ? 'ntfy' : 'Generic Webhook';
  }

  /** @param {import('$lib/admin-state.svelte.js').AlertAction} action */
  function lastTestSummary(action) {
    if (!action.lastTestedAt || !action.lastTestStatus) return 'Not tested';
    const details = [];
    if (action.lastTestHTTPStatus) details.push(`HTTP ${action.lastTestHTTPStatus}`);
    if (action.lastTestLatencyMs > 0) details.push(`${action.lastTestLatencyMs}ms`);
    if (action.lastTestErrorCode) details.push(action.lastTestErrorCode);
    return details.join(' / ') || action.lastTestStatus;
  }

  /** @param {import('$lib/admin-state.svelte.js').AlertRule} rule */
  function ruleFilters(rule) {
    return [rule.category, rule.severity, rule.eventAction].filter(Boolean).join(' / ') || 'No filters';
  }

  /** @param {number} seconds */
  function durationLabel(seconds) {
    const value = Number(seconds) || 0;
    if (value === 0) return 'None';
    if (value % 86400 === 0) return `${value / 86400}d`;
    if (value % 3600 === 0) return `${value / 3600}h`;
    if (value % 60 === 0) return `${value / 60}m`;
    return `${value}s`;
  }

  /** @param {import('$lib/admin-state.svelte.js').AlertRule} rule */
  function thresholdLabel(rule) {
    return rule.aggregationCount <= 1
      ? 'Every match'
      : `${rule.aggregationCount} in ${durationLabel(rule.aggregationWindowSeconds)}`;
  }
</script>

<svelte:head>
  <title>N2API Alerts</title>
</svelte:head>

<svelte:window onkeydown={handleKeydown} />

<AuthGate>
  {#if notice}
    <div
      class={[
        'fixed right-4 top-4 z-[70] flex w-[min(24rem,calc(100vw-2rem))] items-start gap-3 rounded-lg border bg-white p-4 shadow-lg',
        notice.kind === 'success' ? 'border-emerald-200' : notice.kind === 'warning' ? 'border-amber-200' : 'border-red-200'
      ]}
      role={notice.kind === 'error' ? 'alert' : 'status'}
      aria-live="polite"
    >
      <div class="min-w-0 flex-1">
        <p class="text-sm font-semibold text-[#0d0d0d]">{notice.title}</p>
        <p class="mt-1 text-sm leading-5 text-[#6e6e6e]">{notice.message}</p>
      </div>
      <button class="ui-button ui-button--icon size-7 shrink-0 text-[#6e6e6e] hover:bg-[#f5f5f5]" type="button" onclick={() => { notice = null; }} aria-label="Dismiss alert notification" title="Dismiss notification"><X class="size-4" aria-hidden="true" /></button>
    </div>
  {/if}

  <div class="ui-page min-w-0">
    <header class="ui-page-header">
      <div class="ui-page-heading">
        <h1 class="ui-page-title">Alerts</h1>
        <p class="ui-page-description">Notification destinations, exact-match System Event rules, and bounded delivery status.</p>
      </div>
      <div class="ui-page-actions">
        <button class="ui-button ui-button--sm ui-button--secondary" type="button" disabled={alertActions.loading || alertRules.loading || actionsBusy || rulesBusy} onclick={refreshAlerting}>
          <RefreshCw class={alertActions.loading || alertRules.loading ? 'size-4 animate-spin' : 'size-4'} aria-hidden="true" />
          Refresh
        </button>
        <button class="ui-button ui-button--sm ui-button--primary" type="button" onclick={openCreateAction}>
          <Plus class="size-4" aria-hidden="true" />
          Add action
        </button>
      </div>
    </header>

    <section aria-labelledby="delivery-status-title">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <h2 id="delivery-status-title" class="ui-section-title">Delivery status</h2>
        <span class={['inline-flex items-center gap-2 text-xs font-medium', delivery?.running ? 'text-[#0a7a5e]' : delivery?.enabled ? 'text-amber-700' : 'text-[#6e6e6e]']}>
          <span class={['size-2 rounded-full', delivery?.running ? 'bg-[#10a37f]' : delivery?.enabled ? 'bg-amber-500' : 'bg-[#9b9b9b]']}></span>
          {delivery?.running ? 'Running' : delivery?.enabled ? 'Enabled, not running' : 'Disabled'}
        </span>
      </div>
      <dl class="mt-3 grid gap-x-6 gap-y-3 border-y border-[#ededed] py-4 sm:grid-cols-2 lg:grid-cols-4 xl:grid-cols-6">
        <div><dt class="text-xs font-medium text-[#6e6e6e]">Queue</dt><dd class="mt-1 text-sm tabular-nums text-[#0d0d0d]">{delivery ? `${delivery.queueDepth} / ${delivery.queueCapacity}` : '-'}</dd></div>
        <div><dt class="text-xs font-medium text-[#6e6e6e]">Workers</dt><dd class="mt-1 text-sm tabular-nums text-[#0d0d0d]">{delivery ? `${delivery.activeWorkers} / ${delivery.workerCount}` : '-'}</dd></div>
        <div><dt class="text-xs font-medium text-[#6e6e6e]">Delivered</dt><dd class="mt-1 text-sm tabular-nums text-[#0d0d0d]">{delivery?.deliveredCount ?? '-'}</dd></div>
        <div><dt class="text-xs font-medium text-[#6e6e6e]">Failed / dropped</dt><dd class="mt-1 text-sm tabular-nums text-[#0d0d0d]">{delivery ? `${delivery.failedCount} / ${delivery.droppedCount}` : '-'}</dd></div>
        <div><dt class="text-xs font-medium text-[#6e6e6e]">Retried</dt><dd class="mt-1 text-sm tabular-nums text-[#0d0d0d]">{delivery?.retriedCount ?? '-'}</dd></div>
        <div><dt class="text-xs font-medium text-[#6e6e6e]">Last result</dt><dd class="mt-1 truncate text-sm text-[#0d0d0d]" title={delivery?.lastErrorCode || undefined}>{delivery?.lastErrorCode || (delivery?.lastDeliveredAt ? formatDate(delivery.lastDeliveredAt) : 'No delivery yet')}</dd></div>
      </dl>
    </section>

    <section class="relative" aria-labelledby="alert-actions-title">
      {#if actionsBusy}
        <div class="ui-loading-overlay" aria-label="Alert action operation in progress">
          <LoaderCircle class="size-7 animate-spin text-[#10a37f]" aria-hidden="true" />
          <span class="text-sm font-medium text-[#6e6e6e]">thinking</span>
        </div>
      {/if}
      <div class="mb-3 flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 id="alert-actions-title" class="ui-section-title">Delivery actions</h2>
          <p class="mt-1 text-sm text-[#6e6e6e]">Destinations remain encrypted and are never returned to this page.</p>
        </div>
      </div>
      {#if alertActions.error && !actionModalOpen && deleteTarget?.type !== 'action'}
        <div class="mb-3 flex flex-col gap-2 rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 sm:flex-row sm:items-center sm:justify-between" role="alert">
          <span>{alertActions.error}</span>
          <button class="ui-button ui-button--sm ui-button--secondary border-red-200 text-red-700" type="button" onclick={loadAlertActions}>Retry</button>
        </div>
      {/if}
      <div class="ui-table-shell">
        <table class="ui-table ui-table--stacked min-w-[980px]">
          <thead><tr><th>Name</th><th>Type</th><th>Destination</th><th>Enabled</th><th>Last test</th><th>Updated</th><th class="text-right">Actions</th></tr></thead>
          <tbody>
            {#if alertActions.loading && alertActions.items.length === 0}
              <tr><td class="ui-table-empty ui-table-empty--loading" colspan="7">Loading delivery actions...</td></tr>
            {:else if alertActions.items.length === 0}
              <tr><td class="ui-table-empty" colspan="7">No delivery actions configured.</td></tr>
            {:else}
              {#each alertActions.items as action (action.id)}
                <tr>
                  <td class="font-medium text-[#0d0d0d]" data-label="Name">{action.name}</td>
                  <td data-label="Type"><span class="inline-flex rounded-full bg-[#f5f5f5] px-2.5 py-1 text-xs font-medium text-[#3c3c3c]">{actionKindLabel(action.kind)}</span></td>
                  <td data-label="Destination"><span class={action.destinationConfigured ? 'text-[#0a7a5e]' : 'text-red-700'}>{action.destinationConfigured ? 'Configured' : 'Missing'}</span></td>
                  <td data-label="Enabled"><span class={action.enabled ? 'text-[#0a7a5e]' : 'text-[#6e6e6e]'}>{action.enabled ? 'Enabled' : 'Disabled'}</span></td>
                  <td data-label="Last test">
                    <div class="flex min-w-0 items-start gap-2">
                      {#if action.lastTestStatus === 'passed'}<CircleCheck class="mt-0.5 size-4 shrink-0 text-[#10a37f]" aria-hidden="true" />{:else if action.lastTestStatus === 'failed'}<CircleX class="mt-0.5 size-4 shrink-0 text-red-600" aria-hidden="true" />{/if}
                      <div class="min-w-0"><p class="truncate text-sm text-[#3c3c3c]" title={lastTestSummary(action)}>{lastTestSummary(action)}</p>{#if action.lastTestedAt}<p class="mt-0.5 text-xs text-[#8e8e8e]">{formatDate(action.lastTestedAt)}</p>{/if}</div>
                    </div>
                  </td>
                  <td class="whitespace-nowrap tabular-nums text-[#6e6e6e]" data-label="Updated">{formatDate(action.updatedAt)}</td>
                  <td class="text-right" data-label="Actions">
                    <div class="flex justify-end gap-1">
                      <button class="ui-button ui-button--icon ui-button--secondary" type="button" disabled={actionsBusy} onclick={() => runActionTest(action)} aria-label={`Test ${action.name}`} title="Test saved destination"><FlaskConical class="size-4" aria-hidden="true" /></button>
                      <button class="ui-button ui-button--icon ui-button--secondary" type="button" disabled={actionsBusy} onclick={() => openEditAction(action)} aria-label={`Edit ${action.name}`} title="Edit action"><Pencil class="size-4" aria-hidden="true" /></button>
                      <button class="ui-button ui-button--icon ui-button--danger" type="button" disabled={actionsBusy} onclick={() => { deleteTarget = { type: 'action', id: action.id, name: action.name }; alertActions.error = ''; }} aria-label={`Delete ${action.name}`} title="Delete action"><Trash2 class="size-4" aria-hidden="true" /></button>
                    </div>
                  </td>
                </tr>
              {/each}
            {/if}
          </tbody>
        </table>
      </div>
    </section>

    <section class="relative" aria-labelledby="alert-rules-title">
      {#if rulesBusy}
        <div class="ui-loading-overlay" aria-label="Notification rule operation in progress"><LoaderCircle class="size-7 animate-spin text-[#10a37f]" aria-hidden="true" /><span class="text-sm font-medium text-[#6e6e6e]">thinking</span></div>
      {/if}
      <div class="mb-3 flex flex-wrap items-center justify-between gap-3">
        <div><h2 id="alert-rules-title" class="ui-section-title">Notification rules</h2><p class="mt-1 text-sm text-[#6e6e6e]">Exact System Event filters with bounded aggregation, cooldown, and recovery.</p></div>
        <button class="ui-button ui-button--sm ui-button--primary" type="button" disabled={alertActions.items.length === 0} onclick={openCreateRule} title={alertActions.items.length === 0 ? 'Create a delivery action first' : undefined}><Plus class="size-4" aria-hidden="true" />Add rule</button>
      </div>
      {#if alertRules.error && !ruleModalOpen && deleteTarget?.type !== 'rule'}
        <div class="mb-3 flex flex-col gap-2 rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 sm:flex-row sm:items-center sm:justify-between" role="alert"><span>{alertRules.error}</span><button class="ui-button ui-button--sm ui-button--secondary border-red-200 text-red-700" type="button" onclick={loadAlertRules}>Retry</button></div>
      {/if}
      <div class="ui-table-shell">
        <table class="ui-table ui-table--stacked min-w-[1040px]">
          <thead><tr><th>Name</th><th>Action</th><th>Filters</th><th>Threshold</th><th>Cooldown</th><th>Scope</th><th>Recovery</th><th>Enabled</th><th class="text-right">Actions</th></tr></thead>
          <tbody>
            {#if alertRules.loading && alertRules.items.length === 0}
              <tr><td class="ui-table-empty ui-table-empty--loading" colspan="9">Loading notification rules...</td></tr>
            {:else if alertRules.items.length === 0}
              <tr><td class="ui-table-empty" colspan="9">No notification rules configured.</td></tr>
            {:else}
              {#each alertRules.items as rule (rule.id)}
                <tr>
                  <td class="font-medium text-[#0d0d0d]" data-label="Name">{rule.name}</td>
                  <td data-label="Action">{actionName(rule.actionId)}</td>
                  <td class="max-w-[280px] break-words font-mono text-[13px]" data-label="Filters">{ruleFilters(rule)}</td>
                  <td class="whitespace-nowrap tabular-nums" data-label="Threshold">{thresholdLabel(rule)}</td>
                  <td class="whitespace-nowrap tabular-nums" data-label="Cooldown">{durationLabel(rule.cooldownSeconds)}</td>
                  <td class="capitalize" data-label="Scope">{rule.deduplicationScope}</td>
                  <td data-label="Recovery">{rule.notifyRecovery ? rule.recoveryAction : 'Off'}</td>
                  <td data-label="Enabled"><span class={rule.enabled ? 'text-[#0a7a5e]' : 'text-[#6e6e6e]'}>{rule.enabled ? 'Enabled' : 'Disabled'}</span></td>
                  <td class="text-right" data-label="Actions"><div class="flex justify-end gap-1"><button class="ui-button ui-button--icon ui-button--secondary" type="button" disabled={rulesBusy} onclick={() => openEditRule(rule)} aria-label={`Edit ${rule.name}`} title="Edit rule"><Pencil class="size-4" aria-hidden="true" /></button><button class="ui-button ui-button--icon ui-button--danger" type="button" disabled={rulesBusy} onclick={() => { deleteTarget = { type: 'rule', id: rule.id, name: rule.name }; alertRules.error = ''; }} aria-label={`Delete ${rule.name}`} title="Delete rule"><Trash2 class="size-4" aria-hidden="true" /></button></div></td>
                </tr>
              {/each}
            {/if}
          </tbody>
        </table>
      </div>
    </section>
  </div>
</AuthGate>

{#if actionModalOpen}
  <div class="ui-modal-backdrop ui-modal-backdrop--top" role="presentation">
    <div class="ui-modal-panel ui-modal-panel--lg" aria-modal="true" aria-labelledby="alert-action-dialog-title" role="dialog">
      <form onsubmit={saveAction}>
      <div class="flex items-start justify-between gap-4 border-b border-[#ededed] pb-4">
        <div><h2 id="alert-action-dialog-title" class="text-lg font-semibold text-[#0d0d0d]">{actionEditingId ? 'Edit delivery action' : 'Add delivery action'}</h2><p class="mt-1 text-sm text-[#6e6e6e]">Destinations are encrypted after Save and never returned.</p></div>
        <button class="ui-button ui-button--icon ui-button--secondary" type="button" disabled={alertActions.saving || alertActionTests.loading} onclick={closeActionModal} aria-label="Close delivery action modal" title="Close"><X class="size-4" aria-hidden="true" /></button>
      </div>
      <div class="mt-5 grid gap-4">
        <label class="grid gap-2 text-sm font-medium text-[#3c3c3c]">Name<input class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-base text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={actionDraft.name} maxlength="128" required /></label>
        <label class="grid gap-2 text-sm font-medium text-[#3c3c3c]">Adapter<select class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={actionDraft.kind}><option value="generic_webhook">Generic Webhook</option><option value="ntfy">ntfy</option></select></label>
        <label class="grid gap-2 text-sm font-medium text-[#3c3c3c]">Destination URL<input class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" type="url" bind:value={actionDraft.destination} autocomplete="off" placeholder={actionEditingId ? 'Leave blank to keep the saved destination' : actionDraft.kind === 'ntfy' ? 'https://ntfy.sh/n2api-alerts' : 'https://example.com/webhook'} required={!actionEditingId} /><span class="text-xs font-normal leading-5 text-[#6e6e6e]">{actionEditingId ? 'The saved destination is configured but cannot be revealed. Changing adapter requires a replacement URL.' : 'HTTPS is required except for loopback development endpoints.'}</span></label>
        <label class="inline-flex items-center gap-2 text-sm font-medium text-[#3c3c3c]"><input class="size-4 rounded border-[#d9d9d9] text-[#10a37f] focus:ring-[#10a37f]" type="checkbox" bind:checked={actionDraft.enabled} />Enabled for rule delivery</label>
        {#if actionFormError || alertActions.error}<p class="rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700" role="alert">{actionFormError || alertActions.error}</p>{/if}
      </div>
      <div class="ui-modal-actions">
        {#if actionEditingId}<button class="ui-button ui-button--sm ui-button--secondary mr-auto" type="button" disabled={alertActions.saving || alertActionTests.loading} onclick={testEditingAction}><FlaskConical class="size-4" aria-hidden="true" />{alertActionTests.loading && alertActionTests.actionId === actionEditingId ? 'Testing' : 'Test saved'}</button>{/if}
        <button class="ui-button ui-button--sm ui-button--secondary" type="button" disabled={alertActions.saving || alertActionTests.loading} onclick={closeActionModal}>Cancel</button>
        <button class="ui-button ui-button--sm ui-button--primary" type="submit" disabled={alertActions.saving || alertActionTests.loading}>{alertActions.saving ? 'Saving' : 'Save'}</button>
      </div>
      </form>
    </div>
  </div>
{/if}

{#if ruleModalOpen}
  <div class="ui-modal-backdrop ui-modal-backdrop--top" role="presentation">
    <div class="ui-modal-panel ui-modal-panel--xl" aria-modal="true" aria-labelledby="alert-rule-dialog-title" role="dialog">
      <form onsubmit={saveRule}>
      <div class="flex items-start justify-between gap-4 border-b border-[#ededed] pb-4"><div><h2 id="alert-rule-dialog-title" class="text-lg font-semibold text-[#0d0d0d]">{ruleEditingId ? 'Edit notification rule' : 'Add notification rule'}</h2><p class="mt-1 text-sm text-[#6e6e6e]">All populated event fields are exact-match filters.</p></div><button class="ui-button ui-button--icon ui-button--secondary" type="button" disabled={alertRules.saving} onclick={closeRuleModal} aria-label="Close notification rule modal" title="Close"><X class="size-4" aria-hidden="true" /></button></div>
      <div class="mt-5 grid gap-5">
        <div class="grid gap-4 sm:grid-cols-2">
          <label class="grid gap-2 text-sm font-medium text-[#3c3c3c]">Name<input class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-base text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={ruleDraft.name} maxlength="128" required /></label>
          <label class="grid gap-2 text-sm font-medium text-[#3c3c3c]">Delivery action<select class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={ruleDraft.actionId} required>{#each alertActions.items as action}<option value={action.id}>{action.name}{action.enabled ? '' : ' (disabled)'}</option>{/each}</select></label>
        </div>
        <div><h3 class="text-sm font-semibold text-[#0d0d0d]">Event filters</h3><div class="mt-3 grid gap-4 sm:grid-cols-2 lg:grid-cols-3"><label class="grid gap-2 text-sm font-medium text-[#3c3c3c]">Category<select class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={ruleDraft.category}><option value="">Any category</option><option value="audit">Audit</option><option value="security">Security</option><option value="oauth">OAuth</option><option value="scheduler">Scheduler</option><option value="runtime">Runtime</option></select></label><label class="grid gap-2 text-sm font-medium text-[#3c3c3c]">Severity<select class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={ruleDraft.severity}><option value="">Any severity</option><option value="info">Info</option><option value="warning">Warning</option><option value="error">Error</option></select></label><label class="grid gap-2 text-sm font-medium text-[#3c3c3c]">Event action<input class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={ruleDraft.eventAction} placeholder="provider_account.tested" /></label></div></div>
        <div><h3 class="text-sm font-semibold text-[#0d0d0d]">Threshold and cooldown</h3><div class="mt-3 grid gap-4 sm:grid-cols-2 lg:grid-cols-4"><label class="grid gap-2 text-sm font-medium text-[#3c3c3c]">Match count<input class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" type="number" min="1" max="1024" step="1" bind:value={ruleDraft.aggregationCount} required /></label><label class="grid gap-2 text-sm font-medium text-[#3c3c3c]">Window (seconds)<input class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" type="number" min="0" max="86400" step="1" bind:value={ruleDraft.aggregationWindowSeconds} required /></label><label class="grid gap-2 text-sm font-medium text-[#3c3c3c]">Cooldown (seconds)<input class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm tabular-nums text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" type="number" min="0" max="604800" step="1" bind:value={ruleDraft.cooldownSeconds} required /></label><label class="grid gap-2 text-sm font-medium text-[#3c3c3c]">Deduplication scope<select class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 text-sm text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={ruleDraft.deduplicationScope}><option value="rule">Rule</option><option value="target">Event target</option></select></label></div></div>
        <div class="grid gap-3 border-t border-[#ededed] pt-4"><label class="inline-flex items-center gap-2 text-sm font-medium text-[#3c3c3c]"><input class="size-4 rounded border-[#d9d9d9] text-[#10a37f] focus:ring-[#10a37f]" type="checkbox" bind:checked={ruleDraft.notifyRecovery} />Send recovery notification</label>{#if ruleDraft.notifyRecovery}<label class="grid gap-2 text-sm font-medium text-[#3c3c3c]">Recovery event action<input class="w-full rounded-lg border border-[#e5e5e5] bg-white px-3 py-2 font-mono text-[13px] text-[#0d0d0d] outline-none focus:border-[#10a37f] focus:ring-2 focus:ring-[#e8f5f0]" bind:value={ruleDraft.recoveryAction} placeholder="provider_account.recovered" required /></label>{/if}<label class="inline-flex items-center gap-2 text-sm font-medium text-[#3c3c3c]"><input class="size-4 rounded border-[#d9d9d9] text-[#10a37f] focus:ring-[#10a37f]" type="checkbox" bind:checked={ruleDraft.enabled} />Enabled</label></div>
        {#if ruleFormError || alertRules.error}<p class="rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700" role="alert">{ruleFormError || alertRules.error}</p>{/if}
      </div>
      <div class="ui-modal-actions"><button class="ui-button ui-button--sm ui-button--secondary" type="button" disabled={alertRules.saving} onclick={closeRuleModal}>Cancel</button><button class="ui-button ui-button--sm ui-button--primary" type="submit" disabled={alertRules.saving}>{alertRules.saving ? 'Saving' : 'Save'}</button></div>
      </form>
    </div>
  </div>
{/if}

{#if deleteTarget}
  <div class="ui-modal-backdrop" role="presentation">
    <div class="ui-modal-panel ui-modal-panel--sm" role="alertdialog" aria-modal="true" aria-labelledby="alert-delete-title">
      <div class="flex items-start justify-between gap-4"><div><h2 id="alert-delete-title" class="text-lg font-semibold text-[#0d0d0d]">Delete {deleteTarget.type}?</h2><p class="mt-2 text-sm leading-6 text-[#6e6e6e]">{deleteTarget.name} will be removed. Alert actions referenced by rules cannot be deleted.</p></div><button class="ui-button ui-button--icon" type="button" disabled={deleteBusy} onclick={() => { deleteTarget = null; }} aria-label="Close delete alert dialog"><X class="size-4" aria-hidden="true" /></button></div>
      {#if deleteTarget.type === 'action' ? alertActions.error : alertRules.error}<p class="mt-4 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-700" role="alert">{deleteTarget.type === 'action' ? alertActions.error : alertRules.error}</p>{/if}
      <div class="ui-modal-actions"><button class="ui-button ui-button--sm ui-button--secondary" type="button" disabled={deleteBusy} onclick={() => { deleteTarget = null; }}>Cancel</button><button class="ui-button ui-button--sm ui-button--danger-filled" type="button" disabled={deleteBusy} onclick={confirmDelete}>{deleteBusy ? 'Deleting' : 'Delete'}</button></div>
    </div>
  </div>
{/if}
