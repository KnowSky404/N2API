#!/usr/bin/env bash

N2API_CHECKS_JSON='[]'

n2api_checks_reset() {
  N2API_CHECKS_JSON='[]'
}

n2api_check_add() {
  local name=$1 status=$2 reason_code=$3 summary=$4
  N2API_CHECKS_JSON="$(jq -c \
    --arg name "${name}" \
    --arg status "${status}" \
    --arg reason_code "${reason_code}" \
    --arg summary "${summary}" \
    '. + [{name:$name,status:$status,reason_code:$reason_code,summary:$summary}]' \
    <<<"${N2API_CHECKS_JSON}")"
}

n2api_checks_count() {
  local status=$1
  jq -r --arg status "${status}" '[.[] | select(.status == $status)] | length' <<<"${N2API_CHECKS_JSON}"
}

n2api_check_command() {
  local command=$1
  if command -v "${command}" >/dev/null 2>&1; then
    n2api_check_add "tool.${command}" "passed" "tool_available" "Required tool is available"
  else
    n2api_check_add "tool.${command}" "failed" "missing_required_tool" "Required tool is unavailable"
  fi
}

n2api_check_env_file() {
  if [[ ! -e "${N2API_ENV_FILE}" ]]; then
    n2api_check_add "env.file" "failed" "env_file_missing" "Environment file does not exist"
    return 1
  fi
  if [[ -L "${N2API_ENV_FILE}" ]]; then
    n2api_check_add "env.file" "failed" "env_file_symlink" "Environment file is a symbolic link"
    return 1
  fi
  if [[ ! -f "${N2API_ENV_FILE}" ]]; then
    n2api_check_add "env.file" "failed" "env_file_not_regular" "Environment file is not regular"
    return 1
  fi
  if ! n2api_env_file_is_safe "${N2API_ENV_FILE}"; then
    n2api_check_add "env.file" "failed" "env_file_permissions_unsafe" "Environment file must be owner-only"
    return 1
  fi
  if ! n2api_env_load "${N2API_ENV_FILE}"; then
    n2api_check_add "env.file" "failed" "env_file_parse_failed" "Environment file syntax is invalid"
    return 1
  fi
  n2api_check_add "env.file" "passed" "env_file_safe" "Environment file is a protected regular file"
}

n2api_check_state_path() {
  local parent
  if [[ -e "${N2API_STATE_DIR}" ]]; then
    if n2api_state_is_safe "${N2API_STATE_DIR}"; then
      n2api_check_add "state.directory" "passed" "state_directory_safe" "State directory is protected"
    else
      n2api_check_add "state.directory" "failed" "unsafe_state_directory" "State directory is unsafe"
    fi
    return
  fi
  parent="$(dirname -- "${N2API_STATE_DIR}")"
  while [[ ! -e "${parent}" && "${parent}" != '/' ]]; do
    parent="$(dirname -- "${parent}")"
  done
  if n2api_state_parent_is_safe "${parent}"; then
    n2api_check_add "state.directory" "passed" "state_directory_not_initialized" "State directory can be initialized safely"
  else
    n2api_check_add "state.directory" "failed" "unsafe_state_parent" "State directory parent is unsafe"
  fi
}

n2api_check_disk() {
  local path=$1 available_kb available_inodes inode_percent
  path="$(dirname -- "${path}")"
  while [[ ! -e "${path}" && "${path}" != '/' ]]; do
    path="$(dirname -- "${path}")"
  done
  available_kb="$(df -Pk -- "${path}" | awk 'NR==2 {print $4}')"
  if [[ "${available_kb}" =~ ^[0-9]+$ ]] && ((available_kb >= 2097152)); then
    n2api_check_add "disk.space" "passed" "disk_space_available" "At least 2 GiB is available"
  else
    n2api_check_add "disk.space" "failed" "disk_space_insufficient" "Less than 2 GiB is available"
  fi
  read -r available_inodes inode_percent < <(df -Pi -- "${path}" | awk 'NR==2 {gsub("%", "", $5); print $4, $5}')
  if [[ "${available_inodes}" =~ ^[0-9]+$ && "${inode_percent}" =~ ^[0-9]+$ ]] && ((available_inodes >= 10000 && inode_percent <= 95)); then
    n2api_check_add "disk.inodes" "passed" "inodes_available" "Inode availability is within bounds"
  else
    n2api_check_add "disk.inodes" "failed" "inode_capacity_risk" "Inode availability is unsafe"
  fi
}

n2api_check_backup_directory() {
  local backup_dir parent
  backup_dir="$(n2api_env_get N2API_BACKUP_DIR "${N2API_REPO_ROOT}/backups")"
  if [[ "${backup_dir}" == /var/lib/docker/volumes/* || "${backup_dir}" == */var/lib/postgresql* ]]; then
    n2api_check_add "backup.directory" "failed" "backup_directory_inside_volume" "Backup directory overlaps a container data path"
    return
  fi
  if [[ -e "${backup_dir}" ]]; then
    if [[ -d "${backup_dir}" && ! -L "${backup_dir}" && -w "${backup_dir}" ]]; then
      n2api_check_add "backup.directory" "passed" "backup_directory_writable" "Backup directory is writable and outside the database volume"
    else
      n2api_check_add "backup.directory" "failed" "backup_directory_unsafe" "Backup directory is not a writable regular directory"
    fi
    return
  fi
  parent="$(dirname -- "${backup_dir}")"
  while [[ ! -e "${parent}" && "${parent}" != '/' ]]; do
    parent="$(dirname -- "${parent}")"
  done
  if [[ -d "${parent}" && -w "${parent}" && ! -L "${parent}" ]]; then
    n2api_check_add "backup.directory" "attention" "backup_directory_not_created" "Backup directory can be created by an explicit backup operation"
  else
    n2api_check_add "backup.directory" "failed" "backup_directory_unwritable" "Backup directory cannot be created safely"
  fi
}

n2api_config_host_checks() {
  local image public_url bind accepted_risks port direct file source_file failed=0
  n2api_checks_reset
  n2api_check_env_file || return 1

  if n2api_env_contains_placeholder; then
    n2api_check_add "config.placeholders" "failed" "configuration_placeholder_present" "Configuration contains a template value"
  else
    n2api_check_add "config.placeholders" "passed" "configuration_has_no_placeholders" "No template value remains"
  fi

  image="$(n2api_env_get N2API_IMAGE)"
  if n2api_image_reference_is_valid "${image}"; then
    n2api_check_add "config.image" "passed" "immutable_image_valid" "Production image is an exact CalVer digest"
  else
    n2api_check_add "config.image" "failed" "immutable_image_required" "Production image must be an exact CalVer digest"
  fi

  public_url="$(n2api_env_get N2API_PUBLIC_URL)"
  if n2api_public_url_is_valid "${public_url}"; then
    n2api_check_add "config.public_url" "passed" "public_url_valid" "Public URL is valid"
  else
    n2api_check_add "config.public_url" "failed" "public_url_invalid" "Public URL is invalid"
  fi

  bind="$(n2api_env_get N2API_BIND_ADDRESS 127.0.0.1)"
  if n2api_bind_address_is_valid "${bind}"; then
    n2api_check_add "config.bind" "passed" "bind_address_valid" "Bind address is valid"
  else
    n2api_check_add "config.bind" "failed" "bind_address_invalid" "Bind address is invalid"
  fi

  accepted_risks="$(n2api_env_get N2API_ACCEPT_RISKS)"
  if n2api_accepted_risks_are_valid "${accepted_risks}"; then
    n2api_check_add "config.accepted_risks" "passed" "accepted_risks_valid" "Accepted risks are explicit"
  else
    n2api_check_add "config.accepted_risks" "failed" "accepted_risks_invalid" "Accepted risks are invalid"
  fi

  port="$(n2api_env_get N2API_PORT 3000)"
  if [[ "${port}" == "3000" ]]; then
    n2api_check_add "config.internal_port" "passed" "internal_port_compatible" "Application and Compose health ports match"
  else
    n2api_check_add "config.internal_port" "failed" "internal_port_mismatch" "Production Compose requires N2API_PORT=3000"
  fi

  for direct in DATABASE_URL POSTGRES_PASSWORD N2API_ADMIN_PASSWORD N2API_ENCRYPTION_SECRET OPENAI_OAUTH_CLIENT_SECRET N2API_METRICS_BEARER_TOKEN; do
    file="${direct}_FILE"
    if [[ -n "$(n2api_env_get "${direct}")" && -n "$(n2api_env_get "${file}")" ]]; then
      n2api_check_add "config.secret.${direct}" "failed" "secret_direct_file_conflict" "Direct and file secret forms conflict"
    fi
  done

  for source_file in N2API_DATABASE_URL_SOURCE_FILE N2API_ADMIN_PASSWORD_SOURCE_FILE N2API_ENCRYPTION_SECRET_SOURCE_FILE N2API_POSTGRES_PASSWORD_SOURCE_FILE N2API_METRICS_BEARER_TOKEN_SOURCE_FILE; do
    file="$(n2api_env_get "${source_file}")"
    [[ -n "${file}" ]] || continue
    if n2api_secret_file_is_safe "${file}"; then
      n2api_check_add "config.secret_file.${source_file}" "passed" "secret_file_safe" "Secret source file is protected"
    else
      n2api_check_add "config.secret_file.${source_file}" "failed" "secret_file_unsafe" "Secret source file is unsafe"
    fi
  done

  if [[ -n "$(n2api_env_get N2API_ADMIN_PASSWORD)" && "$(n2api_env_get N2API_ADMIN_PASSWORD)" == "$(n2api_env_get N2API_ENCRYPTION_SECRET)" ]]; then
    n2api_check_add "config.secret_independence" "failed" "secrets_not_independent" "Administrator and encryption secrets must differ"
  else
    n2api_check_add "config.secret_independence" "passed" "secrets_independent" "Administrator and encryption secrets are distinct"
  fi

  if n2api_compose config --quiet >/dev/null 2>&1; then
    n2api_check_add "compose.config" "passed" "compose_config_valid" "Compose configuration is valid"
  else
    n2api_check_add "compose.config" "failed" "compose_config_invalid" "Compose configuration is invalid"
  fi

  failed="$(n2api_checks_count failed)"
  ((failed == 0))
}
