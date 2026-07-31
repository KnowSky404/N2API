import { test } from 'bun:test';
import assert from '../test/assert.js';
import { dismissRelease, isReleaseDismissed, readDismissedRelease } from './update-dismissal.js';

/** @param {Record<string, string>} [initial] */
function memoryStorage(initial = {}) {
  const values = new Map(Object.entries(initial));
  return {
    /** @param {string} key */
    getItem: (key) => values.get(key) ?? null,
    /** @param {string} key @param {string} value */
    setItem: (key, value) => values.set(key, value)
  };
}

test('release dismissal applies only to the exact version', () => {
  const storage = memoryStorage();
  assert.equal(dismissRelease('2026073101', storage), true);
  const dismissed = readDismissedRelease(storage);
  assert.equal(dismissed, '2026073101');
  assert.equal(isReleaseDismissed('2026073101', dismissed), true);
  assert.equal(isReleaseDismissed('2026080101', dismissed), false);
});

test('release dismissal tolerates unavailable browser storage', () => {
  const storage = {
    getItem() { throw new Error('blocked'); },
    setItem() { throw new Error('blocked'); }
  };
  assert.equal(readDismissedRelease(storage), '');
  assert.equal(dismissRelease('2026073101', storage), false);
  assert.equal(isReleaseDismissed('', ''), false);
});
