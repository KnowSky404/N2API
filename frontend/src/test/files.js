/**
 * @param {string[]} paths
 * @returns {Promise<(path: string) => string>}
 */
export async function preloadTextFiles(paths) {
  /** @type {Map<string, string>} */
  const files = new Map();
  await Promise.all(paths.map(async (path) => files.set(path, await Bun.file(path).text())));

  return (path) => {
    const contents = files.get(path);
    if (contents === undefined) throw new Error(`Text fixture was not preloaded: ${path}`);
    return contents;
  };
}

/**
 * @param {string[]} paths
 * @returns {Promise<(path: string) => boolean>}
 */
export async function preloadFileExistence(paths) {
  /** @type {Map<string, boolean>} */
  const files = new Map();
  await Promise.all(paths.map(async (path) => files.set(path, await Bun.file(path).exists())));
  return (path) => files.get(path) ?? false;
}
