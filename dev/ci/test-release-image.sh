#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
verify="$repo_root/dev/verification/verify-release-image.sh"
digest="sha256:$(printf 'a%.0s' {1..64})"
ref="ghcr.io/knowsky404/n2api:2026.07.29@$digest"

"$verify" --syntax-only "$ref" >/dev/null
for invalid in \
  "ghcr.io/knowsky404/n2api:2026.07.29" \
  "ghcr.io/knowsky404/n2api@$digest" \
  "ghcr.io/knowsky404/n2api:latest@sha256:short"; do
  if "$verify" --syntax-only "$invalid" >/dev/null 2>&1; then
    echo "accepted invalid release image reference: $invalid" >&2
    exit 1
  fi
done

tmp_dir="$(mktemp -d)"
trap 'rm -rf -- "$tmp_dir"' EXIT
cat <<'EOF' >"$tmp_dir/docker"
#!/usr/bin/env bash
set -euo pipefail
if [[ "$1 $2" == "image inspect" ]]; then
  [[ "${FAKE_IMAGE_INSPECT_FAIL:-false}" != "true" ]] || exit 1
  printf '%s\n' "${FAKE_IMAGE_ID:?}"
elif [[ "$1" == "inspect" ]]; then
  printf '%s|%s|%s\n' "${FAKE_RUNNING:-true}" "${FAKE_CONFIGURED_REF:?}" "${FAKE_RUNNING_ID:?}"
else
  exit 2
fi
EOF
chmod 700 "$tmp_dir/docker"

export DOCKER_BIN="$tmp_dir/docker"
export FAKE_IMAGE_ID="$digest"
export FAKE_RUNNING_ID="$digest"
export FAKE_CONFIGURED_REF="$ref"
"$verify" --container n2api "$ref" >/dev/null

FAKE_IMAGE_INSPECT_FAIL=true
export FAKE_IMAGE_INSPECT_FAIL
if "$verify" "$ref" >/dev/null 2>&1; then
  echo "accepted a missing local release image" >&2
  exit 1
fi
FAKE_IMAGE_INSPECT_FAIL=false
export FAKE_IMAGE_INSPECT_FAIL

FAKE_RUNNING=false
export FAKE_RUNNING
if "$verify" --container n2api "$ref" >/dev/null 2>&1; then
  echo "accepted a stopped release container" >&2
  exit 1
fi
FAKE_RUNNING=true
export FAKE_RUNNING

FAKE_CONFIGURED_REF="ghcr.io/knowsky404/n2api:2026.07.29"
export FAKE_CONFIGURED_REF
if "$verify" --container n2api "$ref" >/dev/null 2>&1; then
  echo "accepted a container created from a mutable reference" >&2
  exit 1
fi
FAKE_CONFIGURED_REF="$ref"
export FAKE_CONFIGURED_REF

FAKE_RUNNING_ID="sha256:$(printf 'b%.0s' {1..64})"
export FAKE_RUNNING_ID
if "$verify" --container n2api "$ref" >/dev/null 2>&1; then
  echo "accepted a mismatched running image" >&2
  exit 1
fi

echo "Release image verification tests passed."
