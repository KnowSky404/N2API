import { beforeEach, test } from 'bun:test';
import assert from '../../test/assert.js';

globalThis.$state = (value) => value;

const { gatewaySettings, session, updateGatewaySettings } = await import('../../lib/admin-state.svelte.js');

function settingsPayload(retentionDays, cutoff, eligibleCount) {
  return {
    maxConcurrentGatewayRequests: 0,
    maxConcurrentRequestsPerAccount: 0,
    maxConcurrentRequestsPerKey: 0,
    requestsPerMinutePerKey: 0,
    tokensPerMinutePerKey: 0,
    providerAccountAutoTestEnabled: false,
    providerAccountAutoTestIntervalSeconds: 300,
    requestLogRetentionDays: retentionDays,
    requestLogRetentionStatus: {
      automaticEnabled: false,
      running: false,
      lastErrorCode: '',
      lastDeletedCount: 0,
      lastBatchCount: 0
    },
    requestLogRetentionStats: {
      cutoff,
      totalCountEstimate: 100,
      eligibleCount,
      observedAt: '2026-07-21T12:00:00Z'
    }
  };
}

beforeEach(() => {
  Object.assign(session, { loading: false, authenticated: true, username: 'owner', error: '' });
  Object.assign(gatewaySettings, {
    loading: false,
    saving: false,
    cleanupRunning: false,
    error: '',
    saved: false,
    cleanupResult: null,
    data: settingsPayload(7, '2026-07-14T12:00:00Z', 20)
  });
});

test('saving retention reloads cutoff and eligible count for the new policy', async () => {
  gatewaySettings.data.requestLogRetentionDays = 30;
  const requests = [];
  globalThis.fetch = async (path, options = {}) => {
    requests.push({ path: String(path), method: options.method ?? 'GET' });
    if (options.method === 'PUT') {
      return Response.json(settingsPayload(30, null, 0));
    }
    return Response.json(settingsPayload(30, '2026-06-21T12:00:00Z', 8));
  };

  await updateGatewaySettings();

  assert.deepEqual(requests, [
    { path: '/api/admin/gateway-settings', method: 'PUT' },
    { path: '/api/admin/gateway-settings', method: 'GET' }
  ]);
  assert.equal(gatewaySettings.data.requestLogRetentionDays, 30);
  assert.equal(gatewaySettings.data.requestLogRetentionStats.cutoff, '2026-06-21T12:00:00Z');
  assert.equal(gatewaySettings.data.requestLogRetentionStats.eligibleCount, 8);
  assert.equal(gatewaySettings.saved, true);
  assert.equal(gatewaySettings.error, '');
});
