/**
 * Provider account-model normalization and presentation helpers.
 *
 * These helpers are deliberately stateless so provider pages and the shared
 * admin state can use the same model-policy rules without coupling them to
 * request lifecycle state.
 */

/** @typedef {import('./admin-state.svelte.js').AccountModel} AccountModel */

/** @param {string | null | undefined} value */
export function parseAccountModelsText(value) {
  const seen = new Set();
  return String(value ?? '')
    .split('\n')
    .map((model) => model.trim())
    .filter((model) => {
      if (!model || seen.has(model)) return false;
      seen.add(model);
      return true;
    })
    .map((model) => ({ model, enabled: true }));
}

/** @param {string | null | undefined} text */
export function parseModelLines(text) {
  const seen = new Set();
  return String(text ?? '')
    .split('\n')
    .map((line) => line.trim())
    .filter((model) => {
      if (!model || seen.has(model)) return false;
      seen.add(model);
      return true;
    });
}

/** @param {Array<string | null | undefined>} models */
export function modelListText(models) {
  return parseModelLines((models ?? []).join('\n')).join('\n');
}

/**
 * @param {Array<{ model?: string | null, enabled?: boolean }>} models
 * @param {string | null | undefined} text
 */
export function mergeAccountModelChanges(models, text) {
  const seen = new Set();
  const merged = [];
  for (const item of models) {
    const model = String(item.model ?? '').trim();
    if (!model || seen.has(model)) continue;
    seen.add(model);
    merged.push({ model, enabled: item.enabled !== false });
  }
  for (const item of parseAccountModelsText(text)) {
    if (seen.has(item.model)) continue;
    seen.add(item.model);
    merged.push(item);
  }
  return merged;
}

/**
 * @param {AccountModel[]} models
 * @param {string} modelName
 * @param {boolean} enabled
 * @returns {AccountModel[]}
 */
export function setAccountModelEnabled(models, modelName, enabled) {
  return models.map((item) =>
    item.model === modelName
      ? /** @type {AccountModel} */ ({ ...item, enabled })
      : item
  );
}

/**
 * @param {AccountModel[]} models
 * @param {string} modelName
 * @returns {AccountModel[]}
 */
export function removeAccountModel(models, modelName) {
  return models.filter((item) => isSyncedAccountModel(item) || item.model !== modelName);
}

/** @param {{ source?: string | null }} model */
export function isSyncedAccountModel(model) {
  return model.source === 'upstream' || model.source === 'oauth_catalog';
}

/** @param {AccountModel[]} models */
export function accountModelsText(models) {
  return modelListText(models.filter((item) => !isSyncedAccountModel(item)).map((item) => item.model));
}

/**
 * @param {AccountModel[]} models
 */
export function accountModelSummary(models) {
  let total = 0;
  let synced = 0;
  let manual = 0;
  let enabled = 0;
  for (const model of models) {
    total++;
    if (isSyncedAccountModel(model)) synced++;
    else manual++;
    if (model.enabled) enabled++;
  }
  return { total, synced, manual, enabled };
}

/** @param {{ source?: string | null }} model */
export function sourceBadgeLabel(model) {
  if (model.source === 'oauth_catalog') return 'OpenAI';
  return model.source === 'upstream' ? 'Synced' : 'Manual';
}
