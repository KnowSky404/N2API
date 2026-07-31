const storageKey = 'n2api.dismissedRelease';

/** @param {{ getItem(key: string): string | null }} storage */
export function readDismissedRelease(storage) {
  try {
    return String(storage.getItem(storageKey) ?? '').trim();
  } catch {
    return '';
  }
}

/** @param {string} version @param {{ setItem(key: string, value: string): void }} storage */
export function dismissRelease(version, storage) {
  const normalized = String(version ?? '').trim();
  if (!normalized) return false;
  try {
    storage.setItem(storageKey, normalized);
    return true;
  } catch {
    return false;
  }
}

/** @param {string} version @param {string} dismissedVersion */
export function isReleaseDismissed(version, dismissedVersion) {
  const normalized = String(version ?? '').trim();
  return normalized !== '' && normalized === String(dismissedVersion ?? '').trim();
}

