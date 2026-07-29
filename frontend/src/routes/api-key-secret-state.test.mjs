import { beforeEach, test } from 'bun:test';
import assert from '../test/assert.js';

globalThis.$state = (value) => value;

const {
  apiKeys,
  createKey,
  revealAPIKeySecret,
  session
} = await import('../lib/admin-state.svelte.js');

beforeEach(() => {
  Object.assign(session, { loading: false, authenticated: true, username: 'owner', error: '' });
  Object.assign(apiKeys, {
    loading: false,
    creating: false,
    saving: false,
    error: '',
    items: [],
    newKeyName: '',
    newKeyRoutingPoolId: 0
  });
});

test('reveal sends exact password bytes and returns the secret only to the caller', async () => {
  const currentPassword = ' \u00a0owner-password\u3000 ';
  const secret = 'n2api_dialog_local_secret';
  let request = null;
  globalThis.fetch = async (path, options = {}) => {
    request = { path: String(path), options };
    return Response.json({ secret });
  };

  const result = await revealAPIKeySecret(7, currentPassword);

  assert.equal(request.path, '/api/admin/keys/7/reveal-secret');
  assert.equal(request.options.method, 'POST');
  assert.deepEqual(JSON.parse(request.options.body), { currentPassword });
  assert.deepEqual(result, { ok: true, secret, error: '', retryAfterSeconds: 0 });
  assert.equal(Object.hasOwn(apiKeys, 'oneTimeSecret'), false);
  assert.doesNotMatch(JSON.stringify(apiKeys), new RegExp(secret));
});

test('reveal turns Retry-After into a friendly local-dialog result', async () => {
  globalThis.fetch = async () => Response.json(
    { error: 'rate_limited' },
    { status: 429, headers: { 'Retry-After': '12' } }
  );

  const result = await revealAPIKeySecret(7, 'owner-password');

  assert.deepEqual(result, {
    ok: false,
    secret: '',
    error: 'Reveal rate limit reached. Try again in 12 seconds.',
    retryAfterSeconds: 12
  });
  assert.equal(session.authenticated, true);
});

test('wrong reveal password remains a form error without clearing the session', async () => {
  globalThis.fetch = async () => Response.json({ error: 'invalid_current_password' }, { status: 400 });

  const result = await revealAPIKeySecret(7, 'wrong-password');

  assert.deepEqual(result, {
    ok: false,
    secret: '',
    error: 'Current password is incorrect.',
    retryAfterSeconds: 0
  });
  assert.equal(session.authenticated, true);
});

test('reveal 401 clears the authenticated session and returns no secret state', async () => {
  globalThis.fetch = async () => Response.json({ error: 'unauthorized' }, { status: 401 });

  const result = await revealAPIKeySecret(7, 'owner-password');

  assert.equal(result, null);
  assert.equal(session.authenticated, false);
  assert.equal(Object.hasOwn(apiKeys, 'oneTimeSecret'), false);
});

test('create returns its one-time secret without storing it in module state', async () => {
  const secret = 'n2api_created_dialog_secret';
  apiKeys.newKeyName = 'Codex workstation';
  globalThis.fetch = async (path, options = {}) => {
    if (String(path) === '/api/admin/keys') {
      return Response.json({
        key: { id: 8, name: 'Codex workstation', prefix: 'n2api_created', secretAvailable: true },
        secret
      }, { status: 201 });
    }
    if (String(path).startsWith('/api/admin/request-logs')) {
      return Response.json({ logs: [], nextCursor: '', hasMore: false });
    }
    throw new Error(`Unexpected request: ${path} ${options.method ?? 'GET'}`);
  };

  const result = await createKey({ preventDefault() {} });

  assert.equal(result, secret);
  assert.equal(Object.hasOwn(apiKeys, 'oneTimeSecret'), false);
  assert.doesNotMatch(JSON.stringify(apiKeys), new RegExp(secret));
});
