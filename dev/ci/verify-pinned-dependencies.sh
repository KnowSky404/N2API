#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

require_digest() {
  local location="$1"
  local reference="$2"
  if [[ ! "$reference" =~ @sha256:[0-9a-f]{64}$ ]]; then
    echo "$location must use a readable version tag plus a sha256 digest: $reference" >&2
    return 1
  fi

  local tagged_reference="${reference%@sha256:*}"
  local image_and_tag="${tagged_reference##*/}"
  if [[ "$image_and_tag" != *:* ]]; then
    echo "$location must keep a readable version tag next to its digest: $reference" >&2
    return 1
  fi

  local image_name="${image_and_tag%%:*}"
  local image_tag="${image_and_tag#*:}"
  if [[ -z "$image_tag" || "$image_tag" == "latest" ]]; then
    echo "$location must not use a moving or empty image tag: $reference" >&2
    return 1
  fi

  case "$image_name" in
    alpine)
      [[ "$image_tag" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || {
        echo "$location must pin an exact Alpine patch release: $reference" >&2
        return 1
      }
      ;;
    postgres)
      [[ "$image_tag" =~ ^[0-9]+\.[0-9]+-alpine[0-9]+\.[0-9]+$ ]] || {
        echo "$location must pin exact PostgreSQL and Alpine release lines: $reference" >&2
        return 1
      }
      ;;
    python)
      [[ "$image_tag" =~ ^[0-9]+\.[0-9]+\.[0-9]+- ]] || {
        echo "$location must pin an exact Python patch release: $reference" >&2
        return 1
      }
      ;;
    uv)
      [[ "$image_tag" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || {
        echo "$location must pin an exact uv patch release: $reference" >&2
        return 1
      }
      ;;
  esac
}

declare -A build_stages=()
while IFS= read -r stage; do
  build_stages["$stage"]=1
done < <(
  awk '
    $1 == "FROM" {
      for (i = 3; i <= NF; i++) {
        if (toupper($i) == "AS" && (i + 1) <= NF) print FILENAME "|" $(i + 1)
      }
    }
  ' deploy/Dockerfile deploy/Dockerfile.e2e
)

while IFS='|' read -r dockerfile line reference; do
  location="$dockerfile:$line"
  if [[ "$reference" == "scratch" || -n "${build_stages[$dockerfile|$reference]+x}" ]]; then
    continue
  fi
  require_digest "$location" "$reference"
done < <(
  awk '
    $1 == "FROM" {
      reference = $2
      if (reference ~ /^--platform=/) reference = $3
      print FILENAME "|" FNR "|" reference
    }
  ' \
    deploy/Dockerfile deploy/Dockerfile.e2e
)

while IFS='|' read -r location reference; do
  if [[ "$reference" == '${N2API_IMAGE:'* ]]; then
    continue
  fi
  require_digest "$location" "$reference"
done < <(
  awk '$1 == "image:" { print FILENAME ":" FNR "|" $2 }' \
    deploy/compose.yaml deploy/compose.e2e.yaml deploy/compose.release.yaml
)

while IFS='|' read -r location reference; do
  require_digest "$location" "$reference"
done < <(
  awk '
    $1 == "image:" && $2 ~ /^postgres:/ { print FILENAME ":" FNR "|" $2 }
    $1 ~ /^postgres:.+/ { print FILENAME ":" FNR "|" $1 }
  ' .github/workflows/ci-image.yml
)

if ! grep -Fq 'image: ${N2API_IMAGE:?' deploy/compose.release.yaml; then
  echo "release Compose must require an explicit N2API_IMAGE" >&2
  exit 1
fi

bun_version="$(sed -n 's/.*"packageManager": "bun@\([^"]*\)".*/\1/p' frontend/package.json)"
ci_bun_version="$(awk '$1 == "bun-version:" { print $2 }' .github/workflows/ci-image.yml)"
docker_bun_count="$(awk '$1 == "FROM" && $2 ~ /^oven\/bun:/ { count++ } END { print count + 0 }' deploy/Dockerfile deploy/Dockerfile.e2e)"
if [[ ! "$bun_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ || "$ci_bun_version" != "$bun_version" || "$docker_bun_count" -eq 0 ]]; then
  echo "frontend packageManager and CI bun-version must use the same exact version" >&2
  exit 1
fi
if ! awk -v version="$bun_version" '
  $1 == "FROM" && $2 ~ /^oven\/bun:/ {
    prefix = "oven/bun:" version
    suffix = substr($2, length(prefix) + 1)
    if (index($2, prefix) != 1 || (suffix !~ /^-/ && suffix !~ /^@sha256:/)) exit 1
  }
' deploy/Dockerfile deploy/Dockerfile.e2e; then
  echo "Docker Bun images must match frontend packageManager bun@$bun_version" >&2
  exit 1
fi

go_version="$(awk '$1 == "go" { print $2 }' backend/go.mod)"
docker_go_count="$(awk '$1 == "FROM" && $2 ~ /^golang:/ { count++ } END { print count + 0 }' deploy/Dockerfile deploy/Dockerfile.e2e)"
if [[ ! "$go_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ || "$docker_go_count" -eq 0 ]]; then
  echo "backend/go.mod must declare an exact Go version" >&2
  exit 1
fi
if ! awk -v version="$go_version" '
  $1 == "FROM" && $2 ~ /^golang:/ && index($2, "golang:" version "-") != 1 { exit 1 }
' deploy/Dockerfile deploy/Dockerfile.e2e; then
  echo "Docker Go images must match backend/go.mod go $go_version" >&2
  exit 1
fi

echo "Pinned dependency references are consistent."
