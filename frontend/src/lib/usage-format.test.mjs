import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mock } from 'bun:test';

globalThis.$state = (value) => value;
mock.module('$lib/clipboard.js', () => ({ copyText: async () => false }));
const { formatCostMicrousd, formatRequestLogCost, formatTokens } = await import('./admin-state.svelte.js');

test('formatTokens uses compact tabular-friendly counts', () => {
  assert.equal(formatTokens(0), '0');
  assert.equal(formatTokens(1234), '1,234');
});

test('formatCostMicrousd renders approximate USD', () => {
  assert.equal(formatCostMicrousd(0), '$0.0000');
  assert.equal(formatCostMicrousd(1234), '$0.0012');
  assert.equal(formatCostMicrousd(1234567), '$1.2346');
});

test('formatRequestLogCost marks unpriced usage with tokens', () => {
  assert.equal(
    formatRequestLogCost({
      pricingMatched: false,
      inputTokens: 1,
      outputTokens: 0,
      totalTokens: 1,
      estimatedCostMicrousd: 0
    }),
    'Unpriced'
  );
  assert.equal(
    formatRequestLogCost({
      pricingMatched: false,
      inputTokens: 0,
      outputTokens: 0,
      totalTokens: 0,
      estimatedCostMicrousd: 0
    }),
    '$0.0000'
  );
  assert.equal(
    formatRequestLogCost({
      pricingMatched: true,
      inputTokens: 1,
      outputTokens: 2,
      totalTokens: 3,
      estimatedCostMicrousd: 1234
    }),
    '$0.0012'
  );
});
