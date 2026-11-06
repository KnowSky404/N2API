## Summary

<!-- What problem does this solve, and what behavior changes? -->

## Scope And Compatibility

<!-- Note affected components, compatibility, rollback, and operator impact. -->

- [ ] This is one atomic change without unrelated refactors or formatting churn.
- [ ] The change stays within N2API's personal, self-hosted V1 scope.
- [ ] Commit messages use Conventional Commits.
- [ ] Development used a dedicated task branch and Git worktree.
- [ ] Local Codex `/review` (or `codex review --base <target-ref>`) covered the full PR diff before every push and PR creation; actionable findings are fixed.

## Verification

<!-- List exact commands and results. Separate local, CI, and manual checks. -->

```text
Not run yet.
```

## Review And Merge

<!-- Record the local review result. After publication, inspect automatic Codex
review on the latest head. Wait if triggered; never manually trigger the bot.
If not triggered, proceed when CI and repository requirements pass. Recheck the
reviewed head before rebase merge; then delete the merged task branch and clean
worktree. Pending or failed review is not the same as no automatic trigger. -->

## Database And Migration

- [ ] No database migration is required.
- [ ] Or: a new ordered migration includes forward/rollback behavior and isolated PostgreSQL tests.
- [ ] Backup, lock, disk, compatibility, and maintenance-window implications are documented where applicable.

## Security And Diagnostics

- [ ] Public text and artifacts are sanitized.
- [ ] No secret, API key, OAuth token, cookie, callback code or complete callback URL, encryption key, database dump, or complete request/response body is included.
- [ ] Security-sensitive details, if applicable, were reported through a private Security Advisory instead of this pull request.

## Release Impact

- [ ] The [release checklist](../docs/release-checklist.md) is not affected.
- [ ] Or: the applicable release, image, migration, recovery, and operator gates are documented above.
