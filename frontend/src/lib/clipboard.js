/**
 * Copy text with a textarea fallback for browsers where Clipboard API is
 * unavailable in the current context.
 *
 * @param {string} text
 * @param {{ clipboard?: Clipboard, document?: Document }} [deps]
 * @returns {Promise<boolean>}
 */
export async function copyText(text, deps = {}) {
  if (!text) return false;

  const clipboard = deps.clipboard ?? globalThis.navigator?.clipboard;
  if (clipboard?.writeText) {
    try {
      await clipboard.writeText(text);
      return true;
    } catch {
      // Fall through to the legacy copy path.
    }
  }

  const doc = deps.document ?? globalThis.document;
  if (!doc?.body || typeof doc.createElement !== 'function' || typeof doc.execCommand !== 'function') {
    return false;
  }

  const textarea = doc.createElement('textarea');
  textarea.value = text;
  textarea.setAttribute('readonly', '');
  textarea.style.position = 'fixed';
  textarea.style.left = '-9999px';
  textarea.style.top = '0';
  doc.body.appendChild(textarea);
  textarea.focus();
  textarea.select();

  try {
    return doc.execCommand('copy');
  } catch {
    return false;
  } finally {
    doc.body.removeChild(textarea);
  }
}
