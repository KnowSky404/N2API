#!/usr/bin/env bash

set -euo pipefail

usage() {
  echo "usage: $0 [--syntax-only] [--container NAME] IMAGE@sha256:DIGEST" >&2
  exit 64
}

syntax_only=false
container=""
while (($# > 0)); do
  case "$1" in
    --syntax-only)
      syntax_only=true
      shift
      ;;
    --container)
      (($# >= 2)) || usage
      container="$2"
      shift 2
      ;;
    --*) usage ;;
    *) break ;;
  esac
done

(($# == 1)) || usage
image_ref="$1"
if [[ ! "$image_ref" =~ ^[^[:space:]@]+:[^/[:space:]@:]+@sha256:[0-9a-f]{64}$ ]]; then
  echo "release image must use a readable tag and immutable sha256 digest" >&2
  exit 1
fi
if $syntax_only; then
  printf 'valid release image reference: %s\n' "$image_ref"
  exit 0
fi

docker_bin=${DOCKER_BIN:-docker}
if ! target_id="$($docker_bin image inspect --format '{{.Id}}' "$image_ref" 2>/dev/null)"; then
  echo "release image is not available locally; pull the immutable reference first" >&2
  exit 1
fi
if [[ ! "$target_id" =~ ^sha256:[0-9a-f]{64}$ ]]; then
  echo "release image has an invalid local image ID" >&2
  exit 1
fi
printf 'release image ID: %s\n' "$target_id"

if [[ -n "$container" ]]; then
  if ! container_state="$($docker_bin inspect --type container --format '{{.State.Running}}|{{.Config.Image}}|{{.Image}}' "$container" 2>/dev/null)"; then
    echo "running container could not be inspected" >&2
    exit 1
  fi
  IFS='|' read -r is_running configured_ref running_id <<<"$container_state"
  if [[ "$is_running" != "true" ]]; then
    echo "release container is not running" >&2
    exit 1
  fi
  if [[ "$configured_ref" != "$image_ref" ]]; then
    echo "running container was not created from the requested immutable reference" >&2
    exit 1
  fi
  if [[ "$running_id" != "$target_id" ]]; then
    echo "running container image does not match the requested release image" >&2
    exit 1
  fi
  printf 'running container %s matches %s\n' "$container" "$image_ref"
fi
