import { expect } from 'bun:test';

const assert = {
  /** @param {unknown} actual @param {unknown} expected */
  equal(actual, expected) {
    expect(actual).toBe(expected);
  },

  /** @param {unknown} actual @param {unknown} expected */
  notEqual(actual, expected) {
    expect(actual).not.toBe(expected);
  },

  /** @param {unknown} actual @param {unknown} expected */
  deepEqual(actual, expected) {
    expect(actual).toEqual(expected);
  },

  /** @param {string} actual @param {string | RegExp} expected */
  match(actual, expected) {
    expect(actual).toMatch(expected);
  },

  /** @param {string} actual @param {string | RegExp} expected */
  doesNotMatch(actual, expected) {
    expect(actual).not.toMatch(expected);
  },

  /** @param {unknown} value */
  ok(value) {
    expect(value).toBeTruthy();
  }
};

export default assert;
