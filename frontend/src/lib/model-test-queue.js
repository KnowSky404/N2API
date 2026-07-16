/**
 * @template T
 * @param {T[]} items
 * @param {(item: T) => Promise<void>} task
 * @param {number} [concurrency]
 */
export async function runModelTestsWithConcurrency(items, task, concurrency = 3) {
  const workerCount = Math.min(items.length, Math.max(1, Math.floor(concurrency)));
  let nextIndex = 0;

  async function worker() {
    while (nextIndex < items.length) {
      const item = items[nextIndex];
      nextIndex += 1;
      await task(item);
    }
  }

  await Promise.all(Array.from({ length: workerCount }, () => worker()));
}
