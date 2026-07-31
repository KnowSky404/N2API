# N2API In-App Release Update Notification Specification

Status: implementation contract
Date: 2026-07-31
Scope: read-only release discovery and administrator notification

## 1. Objective

N2API will notify an authenticated administrator when a newer official release
is available and will show the corresponding GitHub Release notes inside the
admin shell. The feature is informational: it discovers releases and hands the
operator off to the existing guarded upgrade workflow.

The application will not update itself, control Docker, mount the Docker
socket, mutate the deployment environment, create backups, or bypass
`./ops/n2api upgrade plan|apply`.

## 2. Existing Foundations

- Release images contain an exact source commit and build timestamp.
- `GET /version` and authenticated admin health expose current build identity.
- GitHub Releases use `YYYYMMDDNN` CalVer tags and generated release notes.
- A formal CalVer image is the already-tested `sha-*` manifest promoted without
  rebuilding it.
- The repository operator CLI already owns backup, restore-drill, upgrade plan,
  apply, verification, and rollback safety.

Because the promoted image retains its `sha-*` build identity, update status
must be derived from commit ancestry. Comparing the displayed build version to
the CalVer tag as strings is invalid.

## 3. Architecture

```text
GitHub latest release + commit comparison
                  |
         updatecheck.Service
       cache, ETag, timeout, state
                  |
       authenticated admin API
                  |
   global notice + release notes modal
                  |
 existing ./ops/n2api upgrade workflow
```

The Go backend is the only GitHub API client. Browsers do not call GitHub
directly. The service uses a fixed official repository and HTTPS API origin so
configuration cannot introduce SSRF.

## 4. Release Source Contract

The checker requests the latest published release from
`KnowSky404/N2API`. It accepts only:

- a ten-digit CalVer `tag_name`;
- a 40-character lowercase hexadecimal target commit;
- an HTTPS release URL under the official GitHub repository;
- bounded UTF-8 release notes;
- a valid publication timestamp.

Requests send an explicit GitHub media type, API version, and stable generic
User-Agent. No GitHub token is required for the public repository. Responses
are bounded, the request timeout is five seconds, and the latest-release ETag
is reused for conditional requests.

The default check interval is six hours. The first check runs asynchronously at
startup. A manual administrator refresh is allowed with a one-minute cooldown.
No update-check state is persisted in PostgreSQL; a restart safely performs a
new check.

## 5. Version Classification

The checker compares `currentCommit...latestTargetCommit`, where current is the
running build and latest is the GitHub Release target:

| Condition | Public status | UI behavior |
| --- | --- | --- |
| Commits are identical | `up_to_date` | No update notice. |
| Latest commit is ahead of current | `update_available` | Show the notice. |
| Current commit is ahead of latest | `running_ahead` | No downgrade notice. |
| Builds diverged | `unknown` | No update notice. |
| Current build is `dev` or commit is unknown | `unknown` | No update notice. |
| GitHub has no successful response | `unavailable` | No update notice. |

An update-check failure never changes liveness, readiness, gateway behavior,
or database health. After a previous success, failures retain the last useful
snapshot and mark it stale.

## 6. Admin API

`GET /api/admin/update-status` returns the cached snapshot. `POST
/api/admin/update-status/refresh` requests a bounded refresh and returns the
result. Both endpoints require a valid administrator session.

The response contains:

- normalized status;
- current build version, commit, and build time;
- latest release version, name, publication time, URL, target commit, and
  Markdown notes when available;
- last successful check time, staleness, and a stable non-sensitive error code;
- the next time a manual refresh is allowed.

Remote response bodies and transport errors are not exposed to the browser.

## 7. Admin Experience

When `update_available` is returned, the authenticated app shell shows a quiet
global update notice with the target CalVer and an action to open the release
details. The notice works in desktop, collapsed-sidebar, and mobile layouts
without covering navigation or page content.

The details modal follows `DESIGN.md`: a white, viewport-bounded panel with a
hairline border, accessible title, Escape handling, an icon close button, and
internal scrolling. It shows current and latest identities, publication time,
sanitized rendered Markdown, a GitHub Release link, and the immutable image
reference expected by the operator workflow.

Dismissal is stored locally as the exact release version. It suppresses only
the global notice for that version; the release remains discoverable from the
build/update control, and a later version is shown automatically.

Markdown is parsed with raw HTML disabled or removed and sanitized before any
HTML reaches Svelte. External links cannot gain opener access.

## 8. Configuration And Privacy

`N2API_UPDATE_CHECK_ENABLED` defaults to `true`. Setting it to `false` prevents
all GitHub update-check traffic and returns a disabled status. The checker does
not transmit deployment configuration, provider data, credentials, database
state, administrator identity, or the current N2API version in its User-Agent.

## 9. Acceptance Criteria

1. An older release commit that is an ancestor of the latest release returns
   `update_available` with the correct release metadata and notes.
2. The latest release commit returns `up_to_date`.
3. Development, ahead, diverged, malformed, timeout, rate-limit, and HTTP error
   states never produce a false update notice.
4. ETag `304 Not Modified` responses reuse the cached result.
5. Invalid and missing administrator sessions cannot read or refresh update
   status.
6. Dismissing one release does not suppress a later release.
7. Release notes cannot execute scripts or unsafe HTML.
8. The notice and modal pass desktop and mobile rendered checks with no console
   errors, clipping, overlap, or keyboard-close regression.
9. `make test` passes, the local Compose stack is rebuilt without cache, and
   container-local liveness, readiness, version, and UI smoke checks pass.

## 10. Non-Goals

- Automatic deployment or restart.
- A web-based Docker or host control plane.
- Automatic backup, restore, rollback, release publication, or GitHub login.
- Prerelease channels, fork-specific repositories, release subscriptions, or
  multi-user acknowledgement storage.

