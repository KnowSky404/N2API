# Release Image Identity Compatibility

## Evidence and scope

Release run 42580798250 built and smoke-tested both architectures, then failed
publication because the exported Docker image ID equalled the remote manifest
digest rather than its config digest. Docker image-store implementations can
expose either identity. The workflow incorrectly named and compared every local
ID as a config digest.

## Acceptance criteria

- Keep checksum verification and the exported/loaded local image ID comparison.
- Accept a published platform only when its config digest or manifest digest
  equals that exact tested local ID.
- Reject unrelated image IDs and malformed manifest digests.
- Preserve existing immutable platform tags and all smoke, provenance, SBOM,
  vulnerability, and stable-promotion gates.

## Implementation plan

1. Add mock-driven coverage to the existing extracted workflow security tests:
   config IDs, manifest IDs, unrelated IDs, and invalid manifest digests.
2. Rename the archive identity and variables to image ID, resolve and validate
   the remote manifest digest, and compare both supported identities.
3. Run focused checks and the required managed development check, then review,
   commit, and deliver through the normal PR workflow before retrying release.
