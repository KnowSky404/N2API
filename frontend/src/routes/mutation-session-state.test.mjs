import { beforeEach, test } from 'bun:test';
import assert from '../test/assert.js';

globalThis.$state = (value) => value;

const {
  apiKeys,
  completeProviderCallback,
  deleteRevokedKey,
  providerOAuth,
  revokeKey,
  saveAccountModels,
  session,
  syncAccountModels,
  updateAPIKeyBudgets,
  updateAPIKeyLimits,
  updateAPIKeyModelPolicy,
  updateAPIKeyName,
  updateAPIKeyRoutingPool,
  updateProviderAccount
} = await import('../lib/admin-state.svelte.js');

function authenticate() {
  Object.assign(session, { loading: false, authenticated: true, username: 'owner', error: '' });
}

beforeEach(() => {
  authenticate();
  providerOAuth.callbackUrl = 'http://localhost/callback?code=test&state=test';
  apiKeys.items = [{ id: 7, name: 'test-key', revokedAt: null }];
  globalThis.fetch = async () => Response.json({ error: 'unauthorized' }, { status: 401 });
});

test('provider mutations return false when a protected request clears the session', async () => {
  assert.equal(await completeProviderCallback(), false);
  assert.equal(session.authenticated, false);

  authenticate();
  assert.equal(await updateProviderAccount({ id: 3 }, { name: 'updated' }), false);
  assert.equal(session.authenticated, false);

  authenticate();
  assert.equal(await saveAccountModels(3, 'gpt-5'), false);
  assert.equal(session.authenticated, false);

  authenticate();
  assert.equal(await syncAccountModels(3), false);
  assert.equal(session.authenticated, false);
});

test('API-key mutations return false when a protected request clears the session', async () => {
  const mutations = [
    () => updateAPIKeyName(7, 'updated'),
    () => updateAPIKeyModelPolicy(7, 'all', ''),
    () => updateAPIKeyRoutingPool(7, 0),
    () => updateAPIKeyLimits(7, 0, 0),
    () => updateAPIKeyBudgets(7, 0, 0, 0, 0, 0, 0),
    () => revokeKey(7),
    () => deleteRevokedKey(7)
  ];

  for (const mutate of mutations) {
    authenticate();
    assert.equal(await mutate(), false);
    assert.equal(session.authenticated, false);
  }
});
