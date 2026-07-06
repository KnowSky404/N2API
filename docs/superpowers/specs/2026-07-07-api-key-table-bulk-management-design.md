# API Key Table Bulk Management Design

Date: 2026-07-07

## Goal

Make the API Keys page usable for larger personal deployments by adding table-oriented filtering, row selection, and batch operations while preserving the current single-key edit capabilities.

## Existing Context

The API Keys page already has local search, status filtering, an in-page create modal, an in-page single-key edit modal, inline enable/disable toggles, a logical delete action backed by the existing revoke endpoint, and a basic logs modal. The backend exposes per-key admin endpoints for name, disabled state, model policy, rate limits, budgets, routing pool, and revoke. There is no API key bulk endpoint today.

Provider accounts already have a table selection pattern that can be reused visually: a select column, checkbox state, a selected count in the table footer, and bulk actions that operate only on selected rows.

## Recommended Approach

Use a frontend-first bulk workflow that reuses existing single-key admin APIs. This avoids expanding backend contracts for this slice and keeps the change scoped to API key page state, frontend helper functions, and source-level tests.

Batch operations should call the existing per-key endpoints sequentially. If a request fails, the UI should stop the batch, keep the remaining selection visible, and show the existing `apiKeys.error` message. Successfully updated keys should already be reflected by the per-key helper responses.

## Scope

- Add row selection to the API Keys table.
- Add select-all for the currently filtered row set.
- Add a selected count and clear-selection action.
- Add table filtering controls that cover:
  - keyword search by name, prefix, model, pool, and status text;
  - lifecycle status: all, active, disabled, deleted;
  - routing pool: all, global pool, or a concrete routing pool;
  - model policy: all, all routable models, selected models;
  - limiter/budget issue state: all, blocked or budget-exceeded only.
- Preserve existing URL-driven filters for `clientKeyId`, `routingPoolId`, and `status`.
- Add a batch action surface for selected keys:
  - enable selected;
  - disable selected;
  - delete selected;
  - open a bulk edit modal.
- Add a bulk edit modal that can uniformly apply:
  - lifecycle status: enabled or disabled;
  - model policy and selected model list;
  - routing pool;
  - request/token per-minute limits;
  - 24h and 30d request/token/cost budgets.
- Keep the current single-key edit modal and move it toward the same visual grouping as the bulk modal where practical.
- Do not bulk-edit key names.

## Non-goals

- No new backend API key bulk endpoints in this slice.
- No transactional all-or-nothing bulk update semantics.
- No restore operation for deleted keys.
- No permanent physical delete control.
- No billing, tenant, registration, or SaaS behavior.
- No copy or reveal behavior for multiple key secrets.

## UI Behavior

The table gets a first column for selection. Deleted keys can be selected for visibility consistency, but bulk edit controls that cannot apply to deleted keys should ignore them and show a clear message if no editable selected key remains. Bulk delete should be disabled or no-op for already deleted keys.

The toolbar stays compact and operational. Filters should sit above the table as form controls, not as marketing-style cards. Bulk actions should appear only when at least one key is selected. Button labels should be short and concrete: `Enable`, `Disable`, `Delete`, `Edit selected`, and `Clear`.

The bulk edit modal should display the selected count and explain that empty numeric fields mean "leave unchanged" while explicit `0` means "use default/unlimited" for fields that already use zero that way. The modal should use opt-in checkboxes per section so an operator can update only routing pool, only budgets, or only model policy without accidentally overwriting unrelated fields.

The single-key edit modal should remain available from the row action button. Name editing stays single-key only. Status, access, routing pool, limits, and budgets should continue to behave as they do today.

## Data Flow

Add API key selection state beside the existing provider account selection pattern, for example:

```js
export const selectedAPIKeyIds = $state({});
```

The API Keys page derives:

- `filteredAPIKeys` from `apiKeys.items` and local filter state;
- `selectedAPIKeyCount` from `selectedAPIKeyIds`;
- `selectedEditableAPIKeys` by excluding revoked/deleted keys for edit/status actions;
- `allFilteredAPIKeysSelected` for the select-all checkbox.

Frontend batch helpers should gather selected ids and call existing helpers such as `setAPIKeyDisabled`, `revokeKey`, `updateAPIKeyModelPolicy`, `updateAPIKeyLimits`, `updateAPIKeyBudgets`, and `updateAPIKeyRoutingPool` one key at a time. Helpers should set a saving/bulk-running flag so the table does not allow overlapping operations.

After a successful batch action, clear selected ids for rows that were updated. If a batch fails partway through, keep the current selection and expose the error through `apiKeys.error`.

## Error Handling

- Empty selection: show `Select at least one API key`.
- Batch edit with only deleted selected keys: show `Select at least one active or disabled API key`.
- Invalid numeric bulk values: reuse the existing non-negative whole-number validation wording.
- Selected model policy with no selected models: reuse the existing invalid input path from `updateAPIKeyModelPolicy`.
- Partial failure: stop at the first failed request, keep selection, and show the per-key helper error.

## Testing

Frontend source tests should cover:

- API Keys page includes a select column, selected count, clear selection, and bulk action labels.
- Filter controls include lifecycle, routing pool, model policy, and issue-state filters.
- Bulk edit modal includes opt-in sections for status, model access, routing pool, limits, and budgets.
- Single-key edit remains present and still includes name editing.
- Batch helpers exist in `admin-state.svelte.js` and call the existing per-key helper names rather than new backend endpoints.

Verification commands:

```bash
cd frontend && bun test src/routes/navigation.test.mjs
cd frontend && bun run check
cd frontend && bun run build
```

Because this is a frontend-only feature slice, backend `go test ./...` is optional unless implementation touches Go files.

## Rollout Notes

After implementation, rebuild and recreate the local Docker Compose development stack so the running admin UI reflects the updated Svelte bundle.
