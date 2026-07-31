import { beforeEach, test } from 'bun:test';
import assert from '../test/assert.js';

globalThis.$state = (value) => value;

const { loadUpdateStatus, refreshUpdateStatus, session, updateStatus } = await import('../lib/admin-state.svelte.js');

const snapshot = {
  status: 'update_available',
  current: { version: 'sha-current', commit: '1'.repeat(40), builtAt: '2026-07-24T00:00:00Z' },
  latest: {
    version: '2026073101',
    name: 'N2API 2026073101',
    publishedAt: '2026-07-31T02:14:12Z',
    url: 'https://github.com/KnowSky404/N2API/releases/tag/2026073101',
    targetCommit: '2'.repeat(40),
    notes: '### Features',
    image: 'ghcr.io/knowsky404/n2api:2026073101'
  },
  checkedAt: '2026-07-31T08:00:00Z',
  refreshAllowedAt: '2026-07-31T08:01:00Z',
  stale: false
};

beforeEach(() => {
  Object.assign(session, { loading: false, authenticated: true, username: 'owner', error: '' });
  Object.assign(updateStatus, {
    loading: false, refreshing: false, error: '', status: 'unavailable', current: null,
    latest: null, checkedAt: '', refreshAllowedAt: '', stale: false, errorCode: ''
  });
});

test('loadUpdateStatus maps the authenticated update snapshot', async () => {
  let requestedPath = '';
  globalThis.fetch = async (path) => {
    requestedPath = String(path);
    return Response.json(snapshot);
  };
  assert.equal(await loadUpdateStatus(), true);
  assert.equal(requestedPath, '/api/admin/update-status');
  assert.equal(updateStatus.status, 'update_available');
  assert.equal(updateStatus.latest.version, '2026073101');
  assert.equal(updateStatus.loading, false);
  assert.equal(updateStatus.error, '');
});

test('refreshUpdateStatus posts once and applies the returned snapshot', async () => {
  let request = null;
  globalThis.fetch = async (path, options = {}) => {
    request = { path: String(path), method: options.method };
    return Response.json({ ...snapshot, status: 'up_to_date' });
  };
  assert.equal(await refreshUpdateStatus(), true);
  assert.deepEqual(request, { path: '/api/admin/update-status/refresh', method: 'POST' });
  assert.equal(updateStatus.status, 'up_to_date');
  assert.equal(updateStatus.refreshing, false);
});

test('refresh cooldown preserves release data and exposes a local message', async () => {
  Object.assign(updateStatus, { ...snapshot, loading: false, refreshing: false, error: '', errorCode: '' });
  globalThis.fetch = async () => Response.json(
    { error: 'update_check_rate_limited' },
    { status: 429, headers: { 'Retry-After': '30' } }
  );
  assert.equal(await refreshUpdateStatus(), false);
  assert.equal(updateStatus.latest.version, '2026073101');
  assert.equal(updateStatus.error, 'A release check just ran. Try again shortly.');
  assert.equal(updateStatus.refreshing, false);
});

test('update status 401 clears authenticated update state', async () => {
  Object.assign(updateStatus, { ...snapshot, loading: false, refreshing: false, error: '', errorCode: '' });
  globalThis.fetch = async () => Response.json({ error: 'unauthorized' }, { status: 401 });
  assert.equal(await loadUpdateStatus(), false);
  assert.equal(session.authenticated, false);
  assert.equal(updateStatus.latest, null);
  assert.equal(updateStatus.status, 'unavailable');
});

