#!/usr/bin/env bash

set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: render-codeql-config.sh <exceptions.json> <output.yml>" >&2
  exit 2
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
registry="$1"
output="$2"

mapfile -t identifiers < <(
  cd "$repo_root"
  go run dev/ci/validate-security-exceptions.go \
    --active-identifiers codeql source "$registry"
)

mkdir -p "$(dirname "$output")"
{
  echo 'name: N2API security'
  echo 'queries:'
  echo '  - uses: security-extended'
  if (( ${#identifiers[@]} > 0 )); then
    echo 'query-filters:'
    for identifier in "${identifiers[@]}"; do
      if [[ "$identifier" != rule:* ]]; then
        echo "CodeQL exception has an unsupported identity type" >&2
        exit 1
      fi
      rule="${identifier#rule:}"
      printf '  - exclude:\n      id: %s\n' "$(jq -Rn --arg rule "$rule" '$rule')"
    done
  fi
} > "$output"
