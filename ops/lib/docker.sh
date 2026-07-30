#!/usr/bin/env bash

N2API_RELEASE_REPOSITORY="ghcr.io/knowsky404/n2api"

n2api_compose_command() {
  N2API_COMPOSE_CMD=(docker compose --project-name "${N2API_PROJECT_NAME}" --env-file "${N2API_ENV_FILE}")
  local compose_file
  for compose_file in "${N2API_COMPOSE_FILES[@]}"; do
    N2API_COMPOSE_CMD+=(--file "${compose_file}")
  done
}

n2api_compose() {
  n2api_compose_command
  export N2API_ENV_FILE
  n2api_run_timeout "${N2API_COMPOSE_CMD[@]}" "$@"
}

n2api_host_platform() {
  local architecture
  architecture="$(uname -m)"
  case "${architecture}" in
    x86_64|amd64) printf 'linux/amd64\n' ;;
    aarch64|arm64) printf 'linux/arm64\n' ;;
    *) printf 'linux/%s\n' "${architecture}" ;;
  esac
}

n2api_calver_is_valid() {
  local value=$1 day sequence
  [[ "${value}" =~ ^20[0-9]{8}$ ]] || return 1
  day=${value:0:8}
  sequence=${value:8:2}
  [[ "$(date -u -d "${day:0:4}-${day:4:2}-${day:6:2}" +%Y%m%d 2>/dev/null || true)" == "${day}" ]] || return 1
  [[ "${sequence}" != "00" ]]
}

n2api_image_reference_is_valid() {
  local image=$1 tag
  [[ "${image}" =~ ^ghcr\.io/knowsky404/n2api:([0-9]{10})@sha256:[0-9a-f]{64}$ ]] || return 1
  tag=${BASH_REMATCH[1]}
  n2api_calver_is_valid "${tag}" || return 1
  "${N2API_REPO_ROOT}/dev/verification/verify-release-image.sh" --syntax-only "${image}" >/dev/null 2>&1
}

n2api_image_manifest_json() {
  local image=$1
  n2api_run_timeout docker manifest inspect "${image}" 2>/dev/null
}

n2api_image_inspect_json() {
  local image=$1 manifest platforms host_platform digest tag
  n2api_image_reference_is_valid "${image}" || return 1
  manifest="$(n2api_image_manifest_json "${image}")" || return 1
  platforms="$(jq -c '[.manifests[]?.platform | select(.os != null and .architecture != null) | (.os + "/" + .architecture)] | unique' <<<"${manifest}")" || return 1
  jq -e 'index("linux/amd64") != null and index("linux/arm64") != null' <<<"${platforms}" >/dev/null || return 1
  host_platform="$(n2api_host_platform)"
  jq -e --arg platform "${host_platform}" 'index($platform) != null' <<<"${platforms}" >/dev/null || return 1
  digest=${image##*@}
  tag=${image#*:}
  tag=${tag%@*}
  jq -cn \
    --arg image "${image}" \
    --arg repository "${N2API_RELEASE_REPOSITORY}" \
    --arg tag "${tag}" \
    --arg digest "${digest}" \
    --arg host_platform "${host_platform}" \
    --argjson platforms "${platforms}" \
    '{image:$image,repository:$repository,tag:$tag,digest:$digest,platforms:$platforms,host_platform:$host_platform,host_platform_available:true}'
}

n2api_resolve_version() {
  local version=$1 tag_ref repo_digests digest image
  n2api_calver_is_valid "${version}" || return 1
  tag_ref="${N2API_RELEASE_REPOSITORY}:${version}"
  n2api_run_timeout docker pull --quiet "${tag_ref}" >/dev/null || return 1
  repo_digests="$(docker image inspect --format '{{json .RepoDigests}}' "${tag_ref}" 2>/dev/null)" || return 1
  digest="$(jq -r --arg repository "${N2API_RELEASE_REPOSITORY}" '
    [.[] | select(startswith($repository + "@sha256:"))] | first // empty
  ' <<<"${repo_digests}")"
  [[ "${digest}" =~ ^ghcr\.io/knowsky404/n2api@sha256:[0-9a-f]{64}$ ]] || return 1
  image="${N2API_RELEASE_REPOSITORY}:${version}@${digest##*@}"
  n2api_image_inspect_json "${image}" >/dev/null || return 1
  printf '%s\n' "${image}"
}

n2api_manifest_backend_available() {
  if docker buildx version >/dev/null 2>&1; then
    printf 'buildx\n'
    return 0
  fi
  if docker manifest inspect --help >/dev/null 2>&1; then
    printf 'docker_manifest\n'
    return 0
  fi
  return 1
}
