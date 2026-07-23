import { beforeEach, test } from 'bun:test';
import assert from '../test/assert.js';

globalThis.$state = (value) => value;

const {
  accountTestResults,
  adminSessions,
  changePassword,
  changePasswordForm,
  errorPassthroughRules,
  fingerprintProfiles,
  health,
  loadAdminSessions,
  loadHealth,
  loadSession,
  loadSystemEvents,
  login,
  loginForm,
  logout,
  MINIMUM_ADMIN_PASSWORD_BYTES,
  opsMonitor,
  providerAccountBulkModelsForm,
  providerAccountBulkSchedulingForm,
  providerAccountPauseForm,
  revokeAdminSession,
  revokeOtherAdminSessions,
  routingPools,
  selectedAPIKeyIds,
  selectedProviderAccountIds,
  session,
  systemEvents
} = await import('../lib/admin-state.svelte.js');

const currentSession = {
  id: 11,
  current: true,
  createdAt: '2026-07-21T10:00:00Z',
  lastUsedAt: '2026-07-21T11:00:00Z',
  expiresAt: '2026-07-28T10:00:00Z',
  createdIp: '192.0.2.10',
  userAgent: 'Firefox on Linux'
};

const otherSession = {
  id: 12,
  current: false,
  createdAt: '2026-07-20T10:00:00Z',
  lastUsedAt: null,
  expiresAt: '2026-07-27T10:00:00Z',
  createdIp: '',
  userAgent: ''
};

beforeEach(() => {
  Object.assign(session, { loading: false, authenticated: true, username: 'owner', error: '' });
  Object.assign(adminSessions, {
    loading: false,
    error: '',
    items: [],
    revokingId: null,
    revokingOthers: false
  });
  Object.assign(loginForm, { username: '', password: '', submitting: false, error: '' });
  Object.assign(changePasswordForm, { currentPassword: '', newPassword: '', submitting: false, error: '', saved: false, revokedOtherSessions: 0 });
  Object.assign(health, { loading: false, error: '', status: 'ok', database: 'ok', build: null, tasks: null });
  Object.assign(systemEvents, {
    loading: false,
    loadingOlder: false,
    error: '',
    query: '',
    since: '',
    category: 'all',
    outcome: 'all',
    severity: 'all',
    action: '',
    actor: '',
    targetType: '',
    targetId: '',
    items: [],
    nextCursor: '',
    hasMore: false
  });
});

test('loadAdminSessions maps the fixed API response on demand', async () => {
  let requestedPath = '';
  globalThis.fetch = async (path) => {
    requestedPath = String(path);
    return Response.json({ sessions: [currentSession, otherSession] });
  };

  const loaded = await loadAdminSessions();

  assert.equal(loaded, true);
  assert.equal(requestedPath, '/api/admin/sessions');
  assert.deepEqual(adminSessions.items, [currentSession, otherSession]);
  assert.equal(adminSessions.loading, false);
  assert.equal(adminSessions.error, '');
});

test('revokeAdminSession removes a non-current session in place', async () => {
  adminSessions.items = [currentSession, otherSession];
  let request = null;
  globalThis.fetch = async (path, options = {}) => {
    request = { path: String(path), options };
    return new Response(null, { status: 204 });
  };

  const revoked = await revokeAdminSession(otherSession.id);

  assert.equal(revoked, true);
  assert.equal(request.path, '/api/admin/sessions/12');
  assert.equal(request.options.method, 'DELETE');
  assert.deepEqual(adminSessions.items, [currentSession]);
  assert.equal(adminSessions.revokingId, null);
});

test('revokeAdminSession clears authentication after revoking current session', async () => {
  adminSessions.items = [currentSession, otherSession];
  globalThis.fetch = async () => new Response(null, { status: 204 });

  const revoked = await revokeAdminSession(currentSession.id);

  assert.equal(revoked, true);
  assert.equal(session.authenticated, false);
  assert.equal(session.username, '');
  assert.deepEqual(adminSessions.items, []);
});

test('revokeOtherAdminSessions preserves the current session and returns the count', async () => {
  adminSessions.items = [currentSession, otherSession];
  let request = null;
  globalThis.fetch = async (path, options = {}) => {
    request = { path: String(path), options };
    return Response.json({ revoked: 1 });
  };

  const revoked = await revokeOtherAdminSessions();

  assert.equal(revoked, 1);
  assert.equal(request.path, '/api/admin/sessions/revoke-others');
  assert.equal(request.options.method, 'POST');
  assert.deepEqual(adminSessions.items, [currentSession]);
  assert.equal(adminSessions.revokingOthers, false);
});

test('failed revoke preserves rows and restores independent busy state', async () => {
  adminSessions.items = [currentSession, otherSession];
  globalThis.fetch = async () => Response.json({ error: 'session_store_unavailable' }, { status: 500 });

  const revoked = await revokeAdminSession(otherSession.id);

  assert.equal(revoked, false);
  assert.deepEqual(adminSessions.items, [currentSession, otherSession]);
  assert.equal(adminSessions.revokingId, null);
  assert.equal(adminSessions.revokingOthers, false);
  assert.equal(adminSessions.error, 'session_store_unavailable');
});

test('protected request 401 clears authentication and all session state', async () => {
  adminSessions.items = [currentSession, otherSession];
  routingPools.items = [{ id: 3 }];
  selectedAPIKeyIds['7'] = true;
  selectedProviderAccountIds['9'] = true;
  accountTestResults['9'] = { loading: true };
  providerAccountPauseForm.durationSeconds = 900;
  providerAccountBulkSchedulingForm.priority = '10';
  providerAccountBulkModelsForm.text = 'gpt-5';
  Object.assign(changePasswordForm, { currentPassword: 'old-secret', newPassword: 'new-secret', submitting: true, error: 'stale', saved: true, revokedOtherSessions: 2 });
  Object.assign(opsMonitor, { loading: true, error: 'stale', stats: { total: 1 } });
  Object.assign(fingerprintProfiles, { loading: true, error: 'stale', items: [{ id: 2 }], saving: true });
  Object.assign(errorPassthroughRules, { loading: true, error: 'stale', items: [{ id: 4 }], saving: true });
  globalThis.fetch = async () => Response.json({ error: 'unauthorized' }, { status: 401 });

  const loaded = await loadAdminSessions();

  assert.equal(loaded, false);
  assert.equal(session.authenticated, false);
  assert.deepEqual(adminSessions.items, []);
  assert.equal(adminSessions.error, '');
  assert.deepEqual(routingPools.items, []);
  assert.deepEqual(selectedAPIKeyIds, {});
  assert.deepEqual(selectedProviderAccountIds, {});
  assert.deepEqual(accountTestResults, {});
  assert.equal(providerAccountPauseForm.durationSeconds, 300);
  assert.deepEqual(providerAccountBulkSchedulingForm, { priority: '', loadFactor: '', maxConcurrentRequests: '' });
  assert.equal(providerAccountBulkModelsForm.text, '');
  assert.deepEqual(changePasswordForm, { currentPassword: '', newPassword: '', submitting: false, error: '', saved: false, revokedOtherSessions: 0 });
  assert.equal(opsMonitor.loading, false);
  assert.equal(opsMonitor.error, '');
  assert.equal(opsMonitor.stats, null);
  assert.deepEqual(fingerprintProfiles, { loading: false, error: '', items: [], saving: false });
  assert.deepEqual(errorPassthroughRules, { loading: false, error: '', items: [], saving: false });
});

test('stale authenticated health response cannot restore build identity after logout', async () => {
  let resolveHealth;
  globalThis.fetch = async (path) => {
    if (String(path) === '/api/admin/health') {
      return new Promise((resolve) => {
        resolveHealth = resolve;
      });
    }
    if (String(path) === '/api/admin/logout') {
      return new Response(null, { status: 204 });
    }
    throw new Error(`Unexpected request: ${path}`);
  };

  const pendingHealth = loadHealth();
  await Promise.resolve();
  assert.equal(typeof resolveHealth, 'function');

  await logout();
  resolveHealth(Response.json({
    status: 'ok',
    database: 'ok',
    build: { version: 'sha-stale', commit: 'stale-commit', builtAt: '2026-07-21T08:30:00Z' },
    tasks: { alertDelivery: { enabled: true, running: true } }
  }));
  await pendingHealth;

  assert.equal(session.authenticated, false);
  assert.equal(health.build, null);
  assert.equal(health.tasks, null);
});

test('public health response survives an earlier unauthenticated session result', async () => {
  let resolveHealth;
  Object.assign(session, { loading: true, authenticated: false, username: '', error: '' });
  Object.assign(health, { loading: true, error: '', status: 'checking', database: 'checking', build: null, tasks: null });
  globalThis.fetch = async (path) => {
    if (String(path) === '/api/admin/health') {
      return new Promise((resolve) => {
        resolveHealth = resolve;
      });
    }
    if (String(path) === '/api/admin/me') {
      return Response.json({ error: 'unauthorized' }, { status: 401 });
    }
    throw new Error(`Unexpected request: ${path}`);
  };

  const pendingHealth = loadHealth();
  await Promise.resolve();
  assert.equal(typeof resolveHealth, 'function');
  await loadSession();
  resolveHealth(Response.json({ status: 'ok', database: 'ok' }));
  await pendingHealth;

  assert.equal(session.authenticated, false);
  assert.deepEqual(health, {
    loading: false,
    error: '',
    status: 'ok',
    database: 'ok',
    build: null,
    tasks: null
  });
});

test('authenticated health exposes sanitized alert delivery task status', async () => {
  const alertDelivery = {
    enabled: true,
    running: true,
    queueDepth: 1,
    queueCapacity: 64,
    activeWorkers: 2,
    workerCount: 2,
    deliveredCount: 3,
    failedCount: 1,
    droppedCount: 0,
    retriedCount: 2,
    lastErrorCode: 'alert_delivery_http_status'
  };
  globalThis.fetch = async () => Response.json({ status: 'ok', database: 'ok', tasks: { alertDelivery } });

  await loadHealth();

  assert.deepEqual(health.tasks, { alertDelivery });
});

test('login 401 reports invalid credentials without clearing authenticated state', async () => {
  adminSessions.items = [currentSession];
  loginForm.username = 'owner';
  loginForm.password = 'wrong';
  globalThis.fetch = async () => Response.json({ error: 'invalid_credentials' }, { status: 401 });

  await login({ preventDefault() {} });

  assert.equal(session.authenticated, true);
  assert.deepEqual(adminSessions.items, [currentSession]);
  assert.equal(loginForm.error, 'invalid_credentials');
  assert.equal(loginForm.submitting, false);
});

test('protected change-password 401 cannot repopulate cleared form state', async () => {
  Object.assign(changePasswordForm, {
    currentPassword: 'old-secret',
    newPassword: 'new-password',
    submitting: false,
    error: '',
    saved: false,
    revokedOtherSessions: 0
  });
  globalThis.fetch = async () => Response.json({ error: 'unauthorized' }, { status: 401 });

  await changePassword({ preventDefault() {} });

  assert.equal(session.authenticated, false);
  assert.deepEqual(changePasswordForm, {
    currentPassword: '',
    newPassword: '',
    submitting: false,
    error: '',
    saved: false,
    revokedOtherSessions: 0
  });
});

test('wrong current password remains a form error without clearing the valid session', async () => {
  adminSessions.items = [currentSession];
  Object.assign(changePasswordForm, {
    currentPassword: 'wrong',
    newPassword: 'new-password',
    submitting: false,
    error: '',
    saved: false,
    revokedOtherSessions: 0
  });
  globalThis.fetch = async () => Response.json({ error: 'invalid_current_password' }, { status: 400 });

  await changePassword({ preventDefault() {} });

  assert.equal(session.authenticated, true);
  assert.deepEqual(adminSessions.items, [currentSession]);
  assert.equal(changePasswordForm.error, 'invalid_current_password');
  assert.equal(changePasswordForm.submitting, false);
});

test('password change reports the number of other sessions revoked', async () => {
  adminSessions.items = [currentSession, { ...currentSession, id: 10, current: false }];
  Object.assign(changePasswordForm, {
    currentPassword: 'current-password',
    newPassword: 'new-password',
    submitting: false,
    error: '',
    saved: false,
    revokedOtherSessions: 0
  });
  globalThis.fetch = async () => Response.json({ ok: true, revokedOtherSessions: 1 });

  await changePassword({ preventDefault() {} });

  assert.equal(session.authenticated, true);
  assert.equal(changePasswordForm.saved, true);
  assert.equal(changePasswordForm.revokedOtherSessions, 1);
  assert.equal(changePasswordForm.currentPassword, '');
  assert.equal(changePasswordForm.newPassword, '');
});

test('change password rejects a new password shorter than the shared byte minimum', async () => {
  let fetchCalls = 0;
  Object.assign(changePasswordForm, {
    currentPassword: 'current-password',
    newPassword: 'x'.repeat(MINIMUM_ADMIN_PASSWORD_BYTES - 1),
    submitting: false,
    error: '',
    saved: false,
    revokedOtherSessions: 0
  });
  globalThis.fetch = async () => {
    fetchCalls += 1;
    return Response.json({ ok: 'true' });
  };

  await changePassword({ preventDefault() {} });

  assert.equal(fetchCalls, 0);
  assert.equal(changePasswordForm.error, `New password must be at least ${MINIMUM_ADMIN_PASSWORD_BYTES} bytes.`);
  assert.equal(changePasswordForm.submitting, false);
});

test('system log requests clear authentication when the session is revoked remotely', async () => {
  systemEvents.items = [{ id: 41 }];
  systemEvents.hasMore = true;
  systemEvents.nextCursor = 'cursor';
  globalThis.fetch = async () => Response.json({ error: 'unauthorized' }, { status: 401 });

  await loadSystemEvents();

  assert.equal(session.authenticated, false);
  assert.deepEqual(systemEvents.items, []);
  assert.equal(systemEvents.nextCursor, '');
  assert.equal(systemEvents.error, '');
});
