import { beforeEach, test } from 'bun:test';
import assert from '../../test/assert.js';

globalThis.$state = (value) => value;

const {
  appliedRequestLogFilterParams,
  loadRequestLogs,
  requestLogFilterParams,
  requestLogs,
  resetRequestLogFilters,
  session
} = await import('../../lib/admin-state.svelte.js');

function resetRequestLogState() {
  resetRequestLogFilters();
  Object.assign(session, { loading: false, authenticated: true, username: 'owner', error: '' });
  Object.assign(requestLogs, {
    loading: false,
    loadingOlder: false,
    error: '',
    requestId: '',
    query: '',
    statusClass: 'all',
    statusCode: '',
    since: '',
    providerAccountId: 'all',
    routingPoolId: 'all',
    clientKeyId: 'all',
    model: '',
    sessionId: '',
    errorCode: '',
    usageSource: 'all',
    routingPoolError: 'all',
    routingPoolChain: '',
    gatewayFallbacks: false,
    items: [],
    nextCursor: '',
    hasMore: false,
    appliedFilterQuery: null
  });
}

beforeEach(() => {
  resetRequestLogState();
});

test('loadRequestLogs stores the first server page and cursor metadata', async () => {
  let requestedPath = '';
  globalThis.fetch = async (path) => {
    requestedPath = String(path);
    return Response.json({ logs: [{ id: 2 }], nextCursor: 'cursor-2', hasMore: true });
  };

  await loadRequestLogs();

  assert.match(requestedPath, /limit=50/);
  assert.doesNotMatch(requestedPath, /cursor=/);
  assert.deepEqual(requestLogs.items, [{ id: 2 }]);
  assert.equal(requestLogs.nextCursor, 'cursor-2');
  assert.equal(requestLogs.hasMore, true);
  assert.equal(requestLogs.loading, false);
});

test('requestLogFilterParams serializes every active content filter canonically', () => {
  Object.assign(requestLogs, {
    requestId: ' req_1 ',
    query: ' codex ',
    statusClass: 'server_error',
    statusCode: '503',
    since: '1234',
    providerAccountId: '7',
    routingPoolId: '8',
    clientKeyId: '9',
    model: ' gpt-5 ',
    sessionId: ' workspace ',
    errorCode: ' upstream_error ',
    usageSource: 'responses',
    routingPoolError: 'routing_pool_exhausted',
    routingPoolChain: ' primary -> secondary ',
    gatewayFallbacks: true
  });

  assert.deepEqual(Object.fromEntries(requestLogFilterParams()), {
    requestId: 'req_1',
    q: 'codex',
    statusClass: 'server_error',
    statusCode: '503',
    since: '1234',
    providerAccountId: '7',
    routingPoolId: '8',
    clientKeyId: '9',
    model: 'gpt-5',
    sessionId: 'workspace',
    error: 'upstream_error',
    usageSource: 'responses',
    routingPoolError: 'routing_pool_exhausted',
    routingPoolChain: 'primary -> secondary',
    gatewayFallbacks: '1'
  });
});

test('loadRequestLogs appends an older page without duplicate IDs', async () => {
  requestLogs.items = [{ id: 2 }];
  requestLogs.nextCursor = 'cursor-2';
  requestLogs.hasMore = true;
  let requestedPath = '';
  globalThis.fetch = async (path) => {
    requestedPath = String(path);
    return Response.json({ logs: [{ id: 2 }, { id: 1 }, { id: 1 }], nextCursor: '', hasMore: false });
  };

  await loadRequestLogs({ append: true });

  assert.match(requestedPath, /cursor=cursor-2/);
  assert.deepEqual(requestLogs.items, [{ id: 2 }, { id: 1 }]);
  assert.equal(requestLogs.nextCursor, '');
  assert.equal(requestLogs.hasMore, false);
  assert.equal(requestLogs.loadingOlder, false);
});

test('loadRequestLogs appends with the applied filters instead of unsubmitted drafts', async () => {
  requestLogs.query = 'applied';
  const requestedPaths = [];
  globalThis.fetch = async (path) => {
    requestedPaths.push(String(path));
    if (requestedPaths.length === 1) {
      return Response.json({ logs: [{ id: 2 }], nextCursor: 'cursor-2', hasMore: true });
    }
    return Response.json({ logs: [{ id: 1 }], nextCursor: '', hasMore: false });
  };

  await loadRequestLogs();
  requestLogs.query = 'draft';
  assert.deepEqual(Object.fromEntries(appliedRequestLogFilterParams()), { q: 'applied' });
  await loadRequestLogs({ append: true });

  assert.match(requestedPaths[0], /q=applied/);
  assert.match(requestedPaths[1], /q=applied/);
  assert.doesNotMatch(requestedPaths[1], /q=draft/);
  assert.match(requestedPaths[1], /cursor=cursor-2/);
  assert.deepEqual(requestLogs.items, [{ id: 2 }, { id: 1 }]);
});

test('loadRequestLogs recovers an invalid append cursor with a fresh first page', async () => {
  requestLogs.query = 'applied';
  const requestedPaths = [];
  globalThis.fetch = async (path) => {
    requestedPaths.push(String(path));
    if (requestedPaths.length === 1) {
      return Response.json({ logs: [{ id: 2 }], nextCursor: 'expired', hasMore: true });
    }
    if (requestedPaths.length === 2) {
      return Response.json({ error: 'invalid_input' }, { status: 400 });
    }
    return Response.json({ logs: [{ id: 9 }], nextCursor: 'fresh', hasMore: true });
  };

  await loadRequestLogs();
  requestLogs.query = 'draft';
  await loadRequestLogs({ append: true });

  assert.equal(requestedPaths.length, 3);
  assert.match(requestedPaths[1], /q=applied/);
  assert.match(requestedPaths[1], /cursor=expired/);
  assert.match(requestedPaths[2], /q=applied/);
  assert.doesNotMatch(requestedPaths[2], /q=draft/);
  assert.doesNotMatch(requestedPaths[2], /cursor=/);
  assert.deepEqual(requestLogs.items, [{ id: 9 }]);
  assert.equal(requestLogs.nextCursor, 'fresh');
  assert.equal(requestLogs.hasMore, true);
});

test('loadRequestLogs ignores an older response after a fresh request starts', async () => {
  requestLogs.items = [{ id: 2 }];
  requestLogs.nextCursor = 'cursor-2';
  requestLogs.hasMore = true;
  let resolveAppend;
  let resolveFresh;
  globalThis.fetch = async (path) => new Promise((resolve) => {
    if (String(path).includes('cursor=')) resolveAppend = resolve;
    else resolveFresh = resolve;
  });

  const appendRequest = loadRequestLogs({ append: true });
  const freshRequest = loadRequestLogs();
  resolveFresh(Response.json({ logs: [{ id: 10 }], nextCursor: '', hasMore: false }));
  await freshRequest;
  resolveAppend(Response.json({ logs: [{ id: 1 }], nextCursor: '', hasMore: false }));
  await appendRequest;

  assert.deepEqual(requestLogs.items, [{ id: 10 }]);
});

test('resetRequestLogFilters clears server pagination state', () => {
  requestLogs.nextCursor = 'cursor';
  requestLogs.hasMore = true;

  resetRequestLogFilters();

  assert.equal(requestLogs.nextCursor, '');
  assert.equal(requestLogs.hasMore, false);
});

test('loadRequestLogs clears page state when the session is revoked', async () => {
  requestLogs.items = [{ id: 2 }];
  requestLogs.nextCursor = 'cursor';
  requestLogs.hasMore = true;
  globalThis.fetch = async () => Response.json({ error: 'unauthorized' }, { status: 401 });

  await loadRequestLogs();

  assert.equal(session.authenticated, false);
  assert.deepEqual(requestLogs.items, []);
  assert.equal(requestLogs.nextCursor, '');
  assert.equal(requestLogs.hasMore, false);
});
