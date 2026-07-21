import { beforeEach, test } from 'bun:test';
import assert from '../../test/assert.js';

globalThis.$state = (value) => value;

const {
  alertActionTests,
  alertActions,
  alertRules,
  createAlertAction,
  deleteAlertAction,
  loadAlertActions,
  loadAlertRules,
  session,
  testAlertAction,
  updateAlertAction,
  updateAlertRule
} = await import('../../lib/admin-state.svelte.js');

function action(overrides = {}) {
  return {
    id: 7,
    name: 'Primary webhook',
    kind: 'generic_webhook',
    enabled: true,
    destinationConfigured: true,
    lastTestedAt: null,
    lastTestStatus: '',
    lastTestHTTPStatus: null,
    lastTestLatencyMs: 0,
    lastTestErrorCode: '',
    lastTestRetryable: false,
    createdAt: '2026-07-21T10:00:00Z',
    updatedAt: '2026-07-21T10:00:00Z',
    ...overrides
  };
}

function rule(overrides = {}) {
  return {
    id: 9,
    name: 'Provider failures',
    actionId: 7,
    enabled: true,
    category: 'runtime',
    severity: 'error',
    eventAction: '',
    recoveryAction: '',
    aggregationCount: 1,
    aggregationWindowSeconds: 0,
    cooldownSeconds: 300,
    deduplicationScope: 'target',
    notifyRecovery: false,
    createdAt: '2026-07-21T10:00:00Z',
    updatedAt: '2026-07-21T10:00:00Z',
    ...overrides
  };
}

beforeEach(() => {
  Object.assign(session, { loading: false, authenticated: true, username: 'owner', error: '' });
  Object.assign(alertActions, { loading: false, saving: false, deletingId: 0, error: '', items: [] });
  Object.assign(alertRules, { loading: false, saving: false, deletingId: 0, error: '', items: [] });
  Object.assign(alertActionTests, { actionId: 0, loading: false, error: '', retryAfterSeconds: 0, result: null });
});

test('loadAlertActions ignores an older response after a new load starts', async () => {
  let resolveFirst;
  let resolveSecond;
  let calls = 0;
  globalThis.fetch = async () => new Promise((resolve) => {
    calls += 1;
    if (calls === 1) resolveFirst = resolve;
    else resolveSecond = resolve;
  });

  const first = loadAlertActions();
  const second = loadAlertActions();
  resolveSecond(Response.json({ actions: [action({ id: 2, name: 'New' })] }));
  await second;
  resolveFirst(Response.json({ actions: [action({ id: 1, name: 'Old' })] }));
  await first;

  assert.equal(alertActions.items.length, 1);
  assert.equal(alertActions.items[0].name, 'New');
  assert.equal(alertActions.loading, false);
});

test('createAlertAction sends the destination once and stores only the redacted response', async () => {
  const requests = [];
  const saved = action();
  globalThis.fetch = async (path, options = {}) => {
    requests.push({ path: String(path), options });
    if (options.method === 'POST') return Response.json({ action: saved }, { status: 201 });
    return Response.json({ actions: [saved] });
  };

  const created = await createAlertAction({
    name: 'Primary webhook',
    kind: 'generic_webhook',
    destination: 'https://hooks.example.test/n2api?token=secret',
    enabled: true
  });

  assert.equal(created.id, 7);
  assert.equal(requests[0].path, '/api/admin/alert-actions');
  assert.deepEqual(JSON.parse(requests[0].options.body), {
    name: 'Primary webhook',
    kind: 'generic_webhook',
    destination: 'https://hooks.example.test/n2api?token=secret',
    enabled: true
  });
  assert.equal(requests[1].path, '/api/admin/alert-actions');
  assert.equal(alertActions.items[0].destinationConfigured, true);
  assert.equal('destination' in alertActions.items[0], false);
});

test('updateAlertAction omits an unchanged destination and sends the original revision', async () => {
  let updateBody;
  const updated = action({ name: 'Renamed', updatedAt: '2026-07-21T11:00:00Z' });
  globalThis.fetch = async (_path, options = {}) => {
    if (options.method === 'PATCH') {
      updateBody = JSON.parse(options.body);
      return Response.json({ action: updated });
    }
    return Response.json({ actions: [updated] });
  };

  const result = await updateAlertAction(7, {
    name: 'Renamed',
    kind: 'generic_webhook',
    enabled: true,
    expectedUpdatedAt: '2026-07-21T10:00:00Z'
  });

  assert.equal(result.updatedAt, '2026-07-21T11:00:00Z');
  assert.equal(updateBody.expectedUpdatedAt, '2026-07-21T10:00:00Z');
  assert.equal('destination' in updateBody, false);
});

test('testAlertAction sends the saved revision and persists a sanitized failed result', async () => {
  const result = {
    testedAt: '2026-07-21T12:00:00Z',
    status: 'failed',
    httpStatus: 503,
    latencyMs: 125,
    errorCode: 'alert_delivery_http_status',
    retryable: true
  };
  let testBody;
  globalThis.fetch = async (_path, options = {}) => {
    if (options.method === 'POST') {
      testBody = JSON.parse(options.body);
      return Response.json({ result });
    }
    return Response.json({ actions: [action({
      lastTestedAt: result.testedAt,
      lastTestStatus: result.status,
      lastTestHTTPStatus: result.httpStatus,
      lastTestLatencyMs: result.latencyMs,
      lastTestErrorCode: result.errorCode,
      lastTestRetryable: result.retryable
    })] });
  };

  const tested = await testAlertAction(7, '2026-07-21T10:00:00Z');

  assert.deepEqual(testBody, { expectedUpdatedAt: '2026-07-21T10:00:00Z' });
  assert.deepEqual(tested, result);
  assert.deepEqual(alertActionTests.result, result);
  assert.equal(alertActions.items[0].lastTestErrorCode, 'alert_delivery_http_status');
});

test('testAlertAction uses Retry-After for a friendly 429 message', async () => {
  globalThis.fetch = async () => Response.json(
    { error: 'rate_limited' },
    { status: 429, headers: { 'Retry-After': '12' } }
  );

  const tested = await testAlertAction(7, '2026-07-21T10:00:00Z');

  assert.equal(tested, null);
  assert.equal(alertActionTests.retryAfterSeconds, 12);
  assert.equal(alertActionTests.error, 'Test rate limit reached. Try again in 12 seconds.');
});

test('updateAlertRule sends expectedUpdatedAt and refreshes the rules list', async () => {
  let updateBody;
  const updated = rule({ cooldownSeconds: 600, updatedAt: '2026-07-21T11:00:00Z' });
  globalThis.fetch = async (_path, options = {}) => {
    if (options.method === 'PATCH') {
      updateBody = JSON.parse(options.body);
      return Response.json({ rule: updated });
    }
    return Response.json({ rules: [updated] });
  };

  const result = await updateAlertRule(9, {
    name: updated.name,
    actionId: updated.actionId,
    enabled: updated.enabled,
    category: updated.category,
    severity: updated.severity,
    eventAction: updated.eventAction,
    recoveryAction: updated.recoveryAction,
    aggregationCount: updated.aggregationCount,
    aggregationWindowSeconds: updated.aggregationWindowSeconds,
    cooldownSeconds: updated.cooldownSeconds,
    deduplicationScope: updated.deduplicationScope,
    notifyRecovery: updated.notifyRecovery,
    expectedUpdatedAt: '2026-07-21T10:00:00Z'
  });

  assert.equal(updateBody.expectedUpdatedAt, '2026-07-21T10:00:00Z');
  assert.equal(result.updatedAt, '2026-07-21T11:00:00Z');
  assert.equal(alertRules.items[0].cooldownSeconds, 600);
});

test('stale action updates keep the current state and expose a refresh instruction', async () => {
  alertActions.items = [action()];
  globalThis.fetch = async () => Response.json({ error: 'stale_update' }, { status: 409 });

  const result = await updateAlertAction(7, {
    name: 'Local draft',
    kind: 'generic_webhook',
    enabled: true,
    expectedUpdatedAt: '2026-07-21T10:00:00Z'
  });

  assert.equal(result, null);
  assert.equal(alertActions.items[0].name, 'Primary webhook');
  assert.match(alertActions.error, /changed on the server/);
  assert.match(alertActions.error, /Refresh and review/);
});

test('action delete conflicts explain that referenced rules must move first', async () => {
  globalThis.fetch = async () => Response.json({ error: 'conflict' }, { status: 409 });

  const deleted = await deleteAlertAction(7);

  assert.equal(deleted, false);
  assert.match(alertActions.error, /still used by a notification rule/);
});

test('a protected alert request 401 clears all alert and session state', async () => {
  alertActions.items = [action()];
  alertRules.items = [rule()];
  alertActionTests.result = { testedAt: '', status: 'passed', httpStatus: 204, latencyMs: 1, errorCode: '', retryable: false };
  globalThis.fetch = async () => Response.json({ error: 'unauthorized' }, { status: 401 });

  await loadAlertRules();

  assert.equal(session.authenticated, false);
  assert.deepEqual(alertActions.items, []);
  assert.deepEqual(alertRules.items, []);
  assert.equal(alertActionTests.result, null);
});
