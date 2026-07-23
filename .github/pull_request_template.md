## Summary

<!-- What problem does this solve, and what behavior changes? -->

## Scope And Compatibility

<!-- Note affected components, compatibility, rollback, and operator impact. -->

- [ ] This is one atomic change without unrelated refactors or formatting churn.
- [ ] The change stays within N2API's personal, self-hosted V1 scope.
- [ ] Commit messages use Conventional Commits.

## Verification

<!-- List exact commands and results. Separate local, CI, and manual checks. -->

```text
Not run yet.
```

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
