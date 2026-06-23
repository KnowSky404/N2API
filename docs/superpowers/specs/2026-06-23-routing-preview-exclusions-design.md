# Routing Preview Exclusions Design

## Goal

Make Routing diagnostics able to preview gateway scheduling after excluding one or more provider accounts. This helps the administrator inspect fallback behavior without sending traffic or mutating account state.

## Context

N2API already supports account exclusion in the real gateway path and in provider service selection:

- `SelectAccountForModel(ctx, model, excludedAccountIDs...)`
- `SelectAccountForModelAndSession(ctx, model, sessionID, excludedAccountIDs...)`
- `PreviewAccountSelection(ctx, model, sessionID, excludedAccountIDs...)`

The missing part is operational access. `GET /api/admin/model-routing/preview` currently accepts `model` and `sessionId` only, and the Svelte Routing diagnostics page has no way to simulate a failed or excluded account. sub2api's scheduling tools expose scheduler behavior explicitly; this design brings the useful diagnostic part into N2API's smaller personal-gateway model.

## Scope

In scope:

- Add `excludedAccountIds` query support to `GET /api/admin/model-routing/preview`.
- Accept comma-separated account IDs, with whitespace ignored.
- Reject invalid, zero, or negative IDs with `400 bad_request`.
- Pass parsed IDs to `ProviderService.PreviewAccountSelection`.
- Add an **Excluded account IDs** input to Routing diagnostics selection preview.
- Show the excluded IDs in the returned preview summary when present.
- Document the diagnostic behavior.

Out of scope:

- Changing real gateway routing or fallback behavior.
- Persisting exclusion lists.
- Adding a new scheduler plan or history table.
- Excluding by account name, provider, status, or model capability.

## API Behavior

Examples:

- `/api/admin/model-routing/preview?model=gpt-5`
- `/api/admin/model-routing/preview?model=gpt-5&sessionId=workspace-123`
- `/api/admin/model-routing/preview?model=gpt-5&excludedAccountIds=7,8`

When exclusions are supplied, the response is still the existing `SelectionPreview` shape. Excluded accounts should appear in candidates as blocked diagnostic rows when possible, with `unschedulableReason` set by existing provider logic to `account excluded`.

## UI Behavior

The Routing diagnostics selection preview form gains:

- Label: `Excluded account IDs`
- Placeholder: `7, 8`

The UI stores the field in admin state as text. `loadModelRoutingPreview` sends the trimmed value as `excludedAccountIds` only when non-empty. The response rendering already displays blocked candidates and reasons, so no new table is needed. The selected summary should show `excluding 7, 8` when the request included exclusions.

## Validation

Backend validation is authoritative:

- Empty `excludedAccountIds` means no exclusions.
- Splitting on commas, trimming whitespace, and ignoring empty segments is allowed.
- Any non-integer, zero, or negative segment returns `400 bad_request`.

Frontend validation is minimal and user-facing:

- Preserve free-form comma-separated input.
- Let backend validation return the final error.

## Verification

Required gates:

- HTTP API test proves excluded IDs are parsed and passed to provider preview.
- HTTP API test proves invalid excluded IDs return `400`.
- Frontend source test proves the form field and query parameter exist.
- Documentation test proves README/deploy docs mention excluded account IDs in Routing diagnostics.
- `go test ./...`
- `bun test src/routes/navigation.test.mjs`
- `bun run check`
- `bun run build`
