# Provider Account Selected Test Design

## Goal

Let the admin test only the currently selected provider accounts from the Provider accounts page. This extends the sub2api-inspired bulk account management workflow while keeping N2API V1 focused on personal gateway operations.

## Context

N2API already supports:

- single-account **Test account**
- full-pool **Test all accounts**
- row selection for bulk enable and disable
- persisted provider account test history

The missing operational middle ground is testing a chosen subset after filtering or selecting affected accounts. Testing the whole pool can be noisy when only a few accounts were edited, paused, or restored.

## Scope

In scope:

- Add an admin JSON endpoint for testing selected provider accounts by ID.
- Validate the request uses `accountIds`, requires at least one ID, rejects non-positive IDs, and caps the request at 100 IDs.
- Deduplicate repeated IDs while preserving first occurrence order.
- Sequentially call the existing provider `TestAccount` behavior for each selected ID.
- Return the updated account rows.
- Add a **Test selected** button next to the existing selected-account bulk controls.
- Refresh provider account state, model routing state, and expanded test histories after the selected tests finish.
- Document the selected-test workflow.

Out of scope:

- New recurring test plans.
- Parallel probe execution.
- Per-account test scheduling rules.
- Auto-enabling, auto-disabling, or changing account priority based on test result.
- Public monitoring or availability timelines.

## API

`POST /api/admin/provider-accounts/bulk-test`

Request:

```json
{
  "accountIds": [7, 8]
}
```

Response:

```json
{
  "accounts": []
}
```

Errors follow the existing admin provider-account conventions:

- malformed JSON returns `400 bad_request`
- empty list, non-positive ID, or more than 100 IDs returns `400 invalid_input`
- provider service errors are mapped through existing provider account error handling

## UI

The Provider accounts page keeps the current row selection model. When one or more rows are selected, **Test selected** is enabled alongside **Enable selected**, **Disable selected**, and **Clear selection**.

After a successful selected test, the UI clears the selection, reloads provider accounts and model routing, and refreshes any expanded test-history rows. On failure, it reloads provider accounts and preserves the error message so the admin can see partial state changes.

## Verification

- HTTP API tests prove selected IDs are deduplicated and passed to `TestAccount`.
- HTTP API tests prove invalid input is rejected.
- Frontend source tests prove selected-test state and UI controls exist.
- Documentation tests prove README and deploy notes mention **Test selected**.
- Standard verification remains `go test ./...`, `bun run check`, and `bun run build`.
