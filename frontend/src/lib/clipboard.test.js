import assert from 'node:assert/strict';
import { describe, test } from 'node:test';

import { copyText } from './clipboard.js';

const unavailableClipboard = /** @type {Clipboard} */ (/** @type {unknown} */ ({}));

describe('copyText', () => {
  test('falls back to a selected textarea when clipboard is unavailable', async () => {
    const textarea = {
      value: '',
      setAttribute() {},
      style: {},
      focusCalled: false,
      selectCalled: false,
      focus() {
        this.focusCalled = true;
      },
      select() {
        this.selectCalled = true;
      }
    };
    /** @type {{ appended: unknown, removed: unknown, appendChild(node: unknown): void, removeChild(node: unknown): void }} */
    const body = {
      appended: null,
      removed: null,
      /** @param {unknown} node */
      appendChild(node) {
        this.appended = node;
      },
      /** @param {unknown} node */
      removeChild(node) {
        this.removed = node;
      }
    };
    const document = {
      body,
      /** @param {string} tag */
      createElement(tag) {
        assert.equal(tag, 'textarea');
        return textarea;
      },
      /** @param {string} command */
      execCommand(command) {
        assert.equal(command, 'copy');
        return true;
      }
    };

    const copied = await copyText('oauth-link', {
      clipboard: unavailableClipboard,
      document: /** @type {Document} */ (/** @type {unknown} */ (document))
    });

    assert.equal(copied, true);
    assert.equal(textarea.value, 'oauth-link');
    assert.equal(textarea.focusCalled, true);
    assert.equal(textarea.selectCalled, true);
    assert.equal(body.appended, textarea);
    assert.equal(body.removed, textarea);
  });

  test('returns false and cleans up when legacy copy throws', async () => {
    const textarea = {
      value: '',
      setAttribute() {},
      style: {},
      focus() {},
      select() {}
    };
    /** @type {{ appended: unknown, removed: unknown, appendChild(node: unknown): void, removeChild(node: unknown): void }} */
    const body = {
      appended: null,
      removed: null,
      /** @param {unknown} node */
      appendChild(node) {
        this.appended = node;
      },
      /** @param {unknown} node */
      removeChild(node) {
        this.removed = node;
      }
    };
    const document = {
      body,
      /** @param {string} tag */
      createElement(tag) {
        assert.equal(tag, 'textarea');
        return textarea;
      },
      execCommand() {
        throw new Error('copy blocked');
      }
    };

    const copied = await copyText('oauth-link', {
      clipboard: unavailableClipboard,
      document: /** @type {Document} */ (/** @type {unknown} */ (document))
    });

    assert.equal(copied, false);
    assert.equal(body.appended, textarea);
    assert.equal(body.removed, textarea);
  });
});
