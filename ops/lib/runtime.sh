#!/usr/bin/env bash

n2api_probe_container() {
  local container=$1 path=$2 body
  body="$(timeout "${N2API_TIMEOUT_SECONDS}" docker exec "${container}" \
    wget -qO- "http://127.0.0.1:3000${path}" 2>/dev/null)" || return 1
  jq -ce . <<<"${body}" 2>/dev/null
}

n2api_container_runtime_json() {
  local container=$1 raw name running health configured_image image_id read_only cap_drop security_opt tmpfs
  raw="$(docker inspect --type container --format \
    '{{.Name}}|{{.State.Running}}|{{if .State.Health}}{{.State.Health.Status}}{{end}}|{{.Config.Image}}|{{.Image}}|{{.HostConfig.ReadonlyRootfs}}|{{json .HostConfig.CapDrop}}|{{json .HostConfig.SecurityOpt}}|{{json .HostConfig.Tmpfs}}' \
    "${container}" 2>/dev/null)" || return 1
  IFS='|' read -r name running health configured_image image_id read_only cap_drop security_opt tmpfs <<<"${raw}"
  jq -cn \
    --arg name "${name#/}" \
    --argjson running "${running}" \
    --arg health "${health:-unavailable}" \
    --arg configured_image "${configured_image}" \
    --arg image_id "${image_id}" \
    --argjson read_only "${read_only}" \
    --argjson cap_drop "${cap_drop:-null}" \
    --argjson security_opt "${security_opt:-null}" \
    --argjson tmpfs "${tmpfs:-null}" \
    '{
      name:$name,
      running:$running,
      health:$health,
      configured_image:$configured_image,
      image_id:$image_id,
      manifest_digest:(if ($configured_image | contains("@sha256:")) then ($configured_image | split("@") | last) else null end),
      security:{read_only_root:$read_only,cap_drop:$cap_drop,security_opt:$security_opt,tmpfs:$tmpfs}
    }'
}

n2api_schema_version() {
  local user database value
  user="$(n2api_env_get POSTGRES_USER n2api)"
  database="$(n2api_env_get POSTGRES_DB n2api)"
  value="$(n2api_compose exec --no-TTY postgres psql \
    --username "${user}" \
    --dbname "${database}" \
    --tuples-only \
    --no-align \
    --command 'SELECT COALESCE(max(version_id), 0) FROM schema_migrations' 2>/dev/null \
    | tr -d '[:space:]')" || return 1
  [[ "${value}" =~ ^[0-9]+$ ]] || return 1
  printf '%s\n' "${value}"
}

n2api_backup_directory() {
  local value
  value="$(n2api_env_get N2API_BACKUP_DIR "${N2API_REPO_ROOT}/backups")"
  if [[ "${value}" == /* ]]; then
    printf '%s\n' "${value}"
  else
    printf '%s/%s\n' "${N2API_REPO_ROOT}" "${value#./}"
  fi
}

n2api_latest_backup_json() {
  local backup_dir latest
  backup_dir="$(n2api_backup_directory)"
  [[ -d "${backup_dir}" && ! -L "${backup_dir}" ]] || {
    jq -cn --arg directory "${backup_dir}" '{availability:"unavailable",directory:$directory}'
    return
  }
  latest="$(find "${backup_dir}" -maxdepth 1 -type f -name 'n2api-*.metadata.json' -print 2>/dev/null | sort | tail -n 1)"
  if [[ -z "${latest}" ]]; then
    jq -cn --arg directory "${backup_dir}" '{availability:"unavailable",directory:$directory}'
    return
  fi
  if ! jq -e '
    .schema_version == "n2api.ops.backup/v1" and
    (.backup_id | type == "string") and
    (.created_at | type == "string") and
    (.checksum | test("^[0-9a-f]{64}$")) and
    (.integrity_hmac | test("^[0-9a-f]{64}$")) and
    (.evidence_class == "ci_fixture" or .evidence_class == "real_operator") and
    .off_host_status == "attention_missing"
  ' "${latest}" >/dev/null 2>&1; then
    jq -cn --arg directory "${backup_dir}" '{availability:"invalid",directory:$directory}'
    return
  fi
  jq -c --arg directory "${backup_dir}" '{
    availability:"available",
    directory:$directory,
    backup_id,
    created_at,
    size_bytes,
    checksum,
    source_image,
    schema_version_value:.source_schema_version,
    evidence_class,
    verified,
    off_host_status,
    integrity_status:"unverified_snapshot"
  }' "${latest}"
}

n2api_latest_operation_summary_json() {
  local latest
  if ! n2api_state_is_safe "${N2API_STATE_DIR}" 2>/dev/null; then
    jq -cn '{availability:"unavailable"}'
    return
  fi
  latest="$(find "${N2API_STATE_DIR}/operations" -maxdepth 1 -type f -name 'op-*.json' -print 2>/dev/null | sort | tail -n 1)"
  if [[ -z "${latest}" ]] || ! jq -e . "${latest}" >/dev/null 2>&1; then
    jq -cn '{availability:"unavailable"}'
    return
  fi
  jq -c '{availability:"available",operation_id,command,status,changed,finished_at,reason_code}' "${latest}"
}

n2api_latest_restore_evidence_json() {
  local latest
  if ! n2api_state_is_safe "${N2API_STATE_DIR}" 2>/dev/null; then
    jq -cn '{availability:"unavailable"}'
    return
  fi
  latest="$(find "${N2API_STATE_DIR}/operations" -maxdepth 1 -type f -name 'op-*.json' -print 2>/dev/null \
    | sort -r \
    | while IFS= read -r path; do
        if jq -e '.command == "restore.drill" and .status == "succeeded"' "${path}" >/dev/null 2>&1; then
          printf '%s\n' "${path}"
          break
        fi
      done)"
  if [[ -z "${latest}" ]]; then
    jq -cn '{availability:"unavailable"}'
    return
  fi
  jq -c '{
    availability:"available",
    operation_id,
    finished_at,
    evidence_class:(.current.evidence_class // "unknown"),
    image:(.target.image // null),
    backup_id:(.current.backup_id // null),
    archive_checksum:(.current.archive_checksum // null),
    status
  }' "${latest}"
}

n2api_lock_status_json() {
  local metadata lock_file pid operation_id held="unknown" lock_fd
  if ! n2api_state_is_safe "${N2API_STATE_DIR}" 2>/dev/null; then
    jq -cn '{availability:"unavailable",held:false}'
    return
  fi
  metadata="${N2API_STATE_DIR}/locks/operator.json"
  lock_file="${N2API_STATE_DIR}/locks/operator.lock"
  if [[ ! -f "${metadata}" || -L "${metadata}" || ! -f "${lock_file}" || -L "${lock_file}" ]]; then
    jq -cn '{availability:"available",held:false}'
    return
  fi
  pid="$(jq -r '.pid // empty' "${metadata}" 2>/dev/null || true)"
  operation_id="$(jq -r '.operation_id // empty' "${metadata}" 2>/dev/null || true)"
  if [[ "${pid}" =~ ^[0-9]+$ && -d "/proc/${pid}" ]] && n2api_validate_operation_id "${operation_id}"; then
    exec {lock_fd}<"${lock_file}"
    if flock -n "${lock_fd}"; then
      held=false
      flock -u "${lock_fd}"
    else
      held=true
    fi
    exec {lock_fd}>&-
  fi
  jq -cn \
    --arg availability "available" \
    --arg held "${held}" \
    --arg operation_id "${operation_id}" \
    --arg pid "${pid}" \
    '{
      availability:$availability,
      held:(if $held == "true" then true elif $held == "false" then false else "unknown" end),
      operation_id:(if $operation_id == "" then null else $operation_id end),
      pid:(if ($pid | test("^[0-9]+$")) then ($pid | tonumber) else null end)
    }'
}

n2api_git_status_json() {
  local commit tag dirty=false
  commit="$(git -C "${N2API_REPO_ROOT}" rev-parse HEAD 2>/dev/null || true)"
  tag="$(git -C "${N2API_REPO_ROOT}" describe --tags --exact-match HEAD 2>/dev/null || true)"
  [[ -z "$(git -C "${N2API_REPO_ROOT}" status --porcelain 2>/dev/null)" ]] || dirty=true
  jq -cn --arg commit "${commit}" --arg tag "${tag}" --argjson dirty "${dirty}" \
    '{commit:(if $commit == "" then null else $commit end),tag:(if $tag == "" then null else $tag end),dirty:$dirty}'
}

n2api_runtime_snapshot_json() {
  local ps_raw services n2api_container="" runtime='{"availability":"unavailable"}' schema="" schema_json
  local livez='{"availability":"unavailable"}' readyz='{"availability":"unavailable"}' version='{"availability":"unavailable"}'
  local backup restore git lock operation compose_files

  ps_raw="$(n2api_compose ps --all --format json 2>/dev/null)" || return 1
  if [[ -n "${ps_raw}" ]]; then
    services="$(jq -sc 'map({id:.ID,name:.Name,project:.Project,service:.Service,state:.State,health:(if .Health == "" then "unavailable" else .Health end),exit_code:.ExitCode,publishers:.Publishers})' <<<"${ps_raw}")" || return 1
  else
    services='[]'
  fi
  n2api_container="$(jq -r '[.[] | select(.service == "n2api" and .state == "running")][0].id // empty' <<<"${services}")"
  if [[ -n "${n2api_container}" ]]; then
    runtime="$(n2api_container_runtime_json "${n2api_container}")" || runtime='{"availability":"unavailable"}'
    runtime="$(jq -c '. + {availability:"available"}' <<<"${runtime}")"
    if probe="$(n2api_probe_container "${n2api_container}" /livez)"; then
      livez="$(jq -cn --argjson body "${probe}" '{availability:"available",status:"passed",body:$body}')"
    else
      livez='{"availability":"available","status":"failed"}'
    fi
    if probe="$(n2api_probe_container "${n2api_container}" /readyz)"; then
      readyz="$(jq -cn --argjson body "${probe}" '{availability:"available",status:"passed",body:$body}')"
    else
      readyz='{"availability":"available","status":"failed"}'
    fi
    if probe="$(n2api_probe_container "${n2api_container}" /version)"; then
      version="$(jq -cn --argjson body "${probe}" '{availability:"available",status:"passed",body:$body}')"
    else
      version='{"availability":"available","status":"failed"}'
    fi
  fi

  if schema="$(n2api_schema_version)"; then
    schema_json="$(jq -cn --argjson value "${schema}" '{availability:"available",value:$value}')"
  else
    schema_json='{"availability":"unavailable","value":null}'
  fi
  backup="$(n2api_latest_backup_json)"
  restore="$(n2api_latest_restore_evidence_json)"
  git="$(n2api_git_status_json)"
  lock="$(n2api_lock_status_json)"
  operation="$(n2api_latest_operation_summary_json)"
  compose_files="$(printf '%s\n' "${N2API_COMPOSE_FILES[@]}" | jq -Rsc 'split("\n") | map(select(length > 0))')"

  jq -cn \
    --arg project "${N2API_PROJECT_NAME}" \
    --arg env_file "${N2API_ENV_FILE}" \
    --argjson compose_files "${compose_files}" \
    --argjson services "${services}" \
    --argjson runtime "${runtime}" \
    --argjson livez "${livez}" \
    --argjson readyz "${readyz}" \
    --argjson version "${version}" \
    --argjson schema "${schema_json}" \
    --argjson backup "${backup}" \
    --argjson restore "${restore}" \
    --argjson git "${git}" \
    --argjson lock "${lock}" \
    --argjson operation "${operation}" \
    '{
      compose:{project:$project,files:$compose_files,env_file:$env_file,services:$services},
      n2api:$runtime,
      probes:{livez:$livez,readyz:$readyz,version:$version},
      postgres:{health:([ $services[] | select(.service == "postgres") | .health ][0] // "unavailable"),schema:$schema},
      backup:$backup,
      restore_drill:$restore,
      git:$git,
      operation_lock:$lock,
      latest_operation:$operation
    }'
}

n2api_read_protected_secret() {
  local path=$1 value
  n2api_secret_file_is_safe "${path}" || return 1
  [[ "$(stat -c '%s' -- "${path}")" -le 65536 ]] || return 1
  value="$(<"${path}")"
  value="${value%$'\r'}"
  value="${value%$'\n'}"
  [[ -n "${value}" ]] || return 1
  printf '%s' "${value}"
}

n2api_redact_stream() {
  sed -E \
    -e 's#(postgres(ql)?://)[^@[:space:]]+@#\1[REDACTED]@#gI' \
    -e 's#(Authorization|Cookie|password|secret|token|api[_-]?key)(["=:[:space:]]+)[^,[:space:]"}]+#\1\2[REDACTED]#gI'
}
