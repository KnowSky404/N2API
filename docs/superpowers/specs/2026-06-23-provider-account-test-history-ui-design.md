# Provider Account Test History UI Design

## Goal

Expose recent provider account test history in the admin UI so manual tests and automatic backend probes are inspectable from the account management surface, not only through the admin API.

## Context

N2API now records provider account test results in `provider_account_test_results` and exposes them through:

`GET /api/admin/provider-accounts/{id}/test-results?limit=20`

sub2api has a deeper scheduled test plan/result system. N2API V1 currently has a global auto-test switch plus manual **Test account** and **Test all accounts** actions. The next useful UI step is to show the stored result history next to each account. Full per-account scheduled test plans remain out of scope for this slice.

## Scope

In scope:

- Add frontend state for account-scoped test history.
- Add a **History** action on each provider account row.
- Load history lazily when a row is expanded.
- Render loading, error, empty, and result states.
- Refresh expanded history after **Test account** and **Test all accounts** actions complete.
- Keep the existing latest status text in the account status cell.

Out of scope:

- New backend API or schema changes.
- Per-account scheduled test plan CRUD.
- Retention settings.
- Charts or long-term aggregation.
- Rendering history on the Routing diagnostics page.

## UI Behavior

Each account row gets a compact **History** action in the existing pinned actions column. Activating it toggles an expanded row directly below the account.

When opened:

- The UI calls `GET /api/admin/provider-accounts/{id}/test-results?limit=20`.
- While loading, it shows `Loading test history...`.
- On failure, it shows the request error inside the expanded row.
- With no results, it shows `No test history recorded yet.`
- With results, it shows a compact table with:
  - `Checked`
  - `Status`
  - `Message`
  - `Recorded`

Status rendering:

- `passed` uses the existing green operational styling.
- `failed` uses amber warning styling.
- Unknown statuses use neutral styling.

Refresh behavior:

- If an account history panel is expanded, **Test account** reloads that account's history after the test finishes.
- **Test all accounts** reloads all currently expanded histories after the bulk test finishes.
- Normal account list refresh does not automatically load every history panel. It only preserves expanded state and lets the visible expanded row reload on demand.

## State Model

Add a shared state map next to `accountModels`:

```js
export const accountTestResults = $state({});
```

Per account state:

```js
{
  expanded: false,
  loading: false,
  error: '',
  items: [],
  requestSeq: 0
}
```

Result shape:

```js
{
  id: 1,
  accountId: 7,
  provider: 'openai',
  status: 'passed',
  message: '',
  checkedAt: '2026-06-23T00:00:00Z',
  createdAt: '2026-06-23T00:00:00Z'
}
```

## Testing

Frontend unit/source tests must prove:

- `admin-state.svelte.js` uses `/api/admin/provider-accounts/${accountId}/test-results?limit=20`.
- Provider page imports and calls account history helpers.
- Provider page contains an accessible **History** row action.
- Provider page renders loading, error, empty, and result history states.
- Test actions refresh expanded histories after account tests complete.

Verification gates:

- `bun test src/routes/providers/provider-page.test.mjs`
- `bun run check`
- `bun run build`

If practical after implementation, run the admin UI in a browser-accessible dev server and perform one rendered smoke check for the Providers page.
