import { beforeEach, test } from 'bun:test';
import assert from '../test/assert.js';

globalThis.$state = (value) => value;

const {
  apiKeys,
  loadKeys,
  loadMoreKeys,
  loadMoreRoutingPools,
  loadRoutingPools,
  routingPools,
  selectedAPIKeyIds,
  session,
  toggleAPIKeySelection
} = await import('../lib/admin-state.svelte.js');

beforeEach(() => {
  Object.assign(session, { loading: false, authenticated: true, username: 'owner', error: '' });
  Object.assign(apiKeys, {
    loading: false,
    loadingMore: false,
    error: '',
    items: [],
    nextCursor: '',
    hasMore: false,
    appliedQuery: ''
  });
  Object.assign(routingPools, {
    loading: false,
    loadingMore: false,
    error: '',
    items: [],
    nextCursor: '',
    hasMore: false,
    appliedQuery: ''
  });
  for (const key of Object.keys(selectedAPIKeyIds)) delete selectedAPIKeyIds[key];
});

test('API key load-more appends unique rows and preserves explicit loaded selection', async () => {
  const paths = [];
  globalThis.fetch = async (path) => {
    paths.push(String(path));
    if (paths.length === 1) {
      return Response.json({ keys: [{ id: 3 }, { id: 2 }], nextCursor: 'older-2', hasMore: true });
    }
    return Response.json({ keys: [{ id: 2 }, { id: 1 }], nextCursor: '', hasMore: false });
  };

  await loadKeys();
  toggleAPIKeySelection(3, true);
  await loadMoreKeys();

  assert.deepEqual(paths, [
    '/api/admin/keys?limit=50',
    '/api/admin/keys?limit=50&cursor=older-2'
  ]);
  assert.deepEqual(apiKeys.items.map((key) => key.id), [3, 2, 1]);
  assert.deepEqual(Object.keys(selectedAPIKeyIds), ['3']);
  assert.equal(apiKeys.hasMore, false);
  assert.equal(apiKeys.loadingMore, false);
});

test('failed API key append preserves the loaded partial page', async () => {
  Object.assign(apiKeys, { items: [{ id: 2 }], nextCursor: 'older', hasMore: true });
  globalThis.fetch = async () => Response.json({ error: 'internal_error' }, { status: 500 });

  await loadMoreKeys();

  assert.deepEqual(apiKeys.items, [{ id: 2 }]);
  assert.equal(apiKeys.nextCursor, 'older');
  assert.equal(apiKeys.hasMore, true);
  assert.match(apiKeys.error, /internal_error/);
});

test('API key load-more cannot race a fresh refresh with an old cursor', async () => {
  Object.assign(apiKeys, {
    loading: true,
    items: [{ id: 2 }],
    nextCursor: 'older',
    hasMore: true
  });
  let requests = 0;
  globalThis.fetch = async () => {
    requests += 1;
    return Response.json({ keys: [] });
  };

  await loadMoreKeys();

  assert.equal(requests, 0);
  assert.deepEqual(apiKeys.items, [{ id: 2 }]);
  assert.equal(apiKeys.nextCursor, 'older');
});

test('routing pool load-more appends without replacing the first page', async () => {
  const paths = [];
  globalThis.fetch = async (path) => {
    paths.push(String(path));
    if (paths.length === 1) {
      return Response.json({ pools: [{ id: 4 }], nextCursor: 'older-4', hasMore: true });
    }
    return Response.json({ pools: [{ id: 3 }], nextCursor: '', hasMore: false });
  };

  await loadRoutingPools();
  await loadMoreRoutingPools();

  assert.deepEqual(paths, [
    '/api/admin/routing-pools?limit=50',
    '/api/admin/routing-pools?limit=50&cursor=older-4'
  ]);
  assert.deepEqual(routingPools.items.map((pool) => pool.id), [4, 3]);
  assert.equal(routingPools.hasMore, false);
});

test('API key search replaces the page and load-more reuses the normalized query', async () => {
  const paths = [];
  globalThis.fetch = async (path) => {
    paths.push(String(path));
    if (paths.length === 1) {
      return Response.json({ keys: [{ id: 8 }], nextCursor: 'matching-8', hasMore: true });
    }
    return Response.json({ keys: [{ id: 7 }], nextCursor: '', hasMore: false });
  };

  Object.assign(apiKeys, { items: [{ id: 99 }], nextCursor: 'unfiltered-99', hasMore: true });
  await loadKeys({ query: '  CoDeX\tLaptop  ' });
  await loadMoreKeys();

  assert.deepEqual(paths, [
    '/api/admin/keys?limit=50&q=codex+laptop',
    '/api/admin/keys?limit=50&q=codex+laptop&cursor=matching-8'
  ]);
  assert.deepEqual(apiKeys.items.map((key) => key.id), [8, 7]);
  assert.equal(apiKeys.appliedQuery, 'codex laptop');
});

test('newer API key search wins over a stale response', async () => {
  const pending = new Map();
  globalThis.fetch = (path) => new Promise((resolve) => pending.set(String(path), resolve));

  const oldSearch = loadKeys({ query: 'old' });
  const newSearch = loadKeys({ query: 'new' });
  pending.get('/api/admin/keys?limit=50&q=new')(Response.json({ keys: [{ id: 2 }] }));
  await newSearch;
  pending.get('/api/admin/keys?limit=50&q=old')(Response.json({ keys: [{ id: 1 }] }));
  await oldSearch;

  assert.deepEqual(apiKeys.items, [{ id: 2 }]);
  assert.equal(apiKeys.appliedQuery, 'new');
});

test('routing pool search replaces the page and load-more reuses the normalized query', async () => {
  const paths = [];
  globalThis.fetch = async (path) => {
    paths.push(String(path));
    if (paths.length === 1) {
      return Response.json({ pools: [{ id: 8 }], nextCursor: 'matching-8', hasMore: true });
    }
    return Response.json({ pools: [{ id: 7 }], nextCursor: '', hasMore: false });
  };

  Object.assign(routingPools, { items: [{ id: 99 }], nextCursor: 'unfiltered-99', hasMore: true });
  await loadRoutingPools({ query: '  Primary\nPool  ' });
  await loadMoreRoutingPools();

  assert.deepEqual(paths, [
    '/api/admin/routing-pools?limit=50&q=primary+pool',
    '/api/admin/routing-pools?limit=50&q=primary+pool&cursor=matching-8'
  ]);
  assert.deepEqual(routingPools.items.map((pool) => pool.id), [8, 7]);
  assert.equal(routingPools.appliedQuery, 'primary pool');
});

test('newer routing pool search wins over a stale response', async () => {
  const pending = new Map();
  globalThis.fetch = (path) => new Promise((resolve) => pending.set(String(path), resolve));

  const oldSearch = loadRoutingPools({ query: 'old' });
  const newSearch = loadRoutingPools({ query: 'new' });
  pending.get('/api/admin/routing-pools?limit=50&q=new')(Response.json({ pools: [{ id: 2 }] }));
  await newSearch;
  pending.get('/api/admin/routing-pools?limit=50&q=old')(Response.json({ pools: [{ id: 1 }] }));
  await oldSearch;

  assert.deepEqual(routingPools.items, [{ id: 2 }]);
  assert.equal(routingPools.appliedQuery, 'new');
});
