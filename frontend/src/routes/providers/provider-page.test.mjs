import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import assert from 'node:assert/strict';

const source = readFileSync('src/routes/providers/+page.svelte', 'utf8');

test('provider page has a single OAuth account creation entry point', () => {
  assert.equal(source.includes('Connect account'), false);
  assert.match(source, /Add OAuth account/);
});

test('provider account table supports search, sorting, and a pinned actions column', () => {
  assert.match(source, /placeholder="Search accounts"/);
  assert.match(source, /aria-sort=/);
  assert.match(source, /sortProviderAccounts/);
  assert.match(source, /sticky right-0/);
});
