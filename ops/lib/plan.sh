#!/usr/bin/env bash

N2API_PLAN_READ_REASON=""

n2api_compose_inputs_json() {
  local path resolved hmac items='[]'
  for path in "${N2API_COMPOSE_FILES[@]}"; do
    [[ -f "${path}" && ! -L "${path}" && -r "${path}" ]] || return 1
    resolved="$(readlink -f -- "${path}")" || return 1
    hmac="$(n2api_state_hmac_file "${resolved}")" || return 1
    items="$(jq -c --arg path "${resolved}" --arg hmac "${hmac}" '. + [{path:$path,hmac:$hmac}]' <<<"${items}")"
  done
  jq -cS . <<<"${items}"
}

n2api_plan_context_json() {
  local runtime=$1 env_hmac compose_inputs compose_inputs_hmac config_hmac git platform services_fingerprint
  local volume_name volume_exists=false configured_image image_id schema_version services_count
  env_hmac="$(n2api_state_hmac_file "${N2API_ENV_FILE}")" || return 1
  compose_inputs="$(n2api_compose_inputs_json)" || return 1
  compose_inputs_hmac="$(n2api_state_hmac_json "${compose_inputs}")" || return 1
  config_hmac="$(n2api_state_hmac_json "$(jq -cn \
    --arg project "${N2API_PROJECT_NAME}" \
    --arg env_hmac "${env_hmac}" \
    --arg compose_inputs_hmac "${compose_inputs_hmac}" \
    '{project:$project,environment_hmac:$env_hmac,compose_inputs_hmac:$compose_inputs_hmac}')")" || return 1
  git="$(n2api_git_status_json)" || return 1
  platform="$(n2api_host_platform)"
  services_fingerprint="$(n2api_state_hmac_json "$(jq -cS '.compose.services' <<<"${runtime}")")" || return 1
  services_count="$(jq -r '.compose.services | length' <<<"${runtime}")"
  configured_image="$(jq -r '.n2api.configured_image // empty' <<<"${runtime}")"
  image_id="$(jq -r '.n2api.image_id // empty' <<<"${runtime}")"
  schema_version="$(jq -r '.postgres.schema.value // empty' <<<"${runtime}")"
  volume_name="${N2API_PROJECT_NAME}_n2api-postgres"
  if docker volume inspect "${volume_name}" >/dev/null 2>&1; then
    volume_exists=true
  fi
  jq -cn \
    --argjson git "${git}" \
    --arg project "${N2API_PROJECT_NAME}" \
    --argjson compose_inputs "${compose_inputs}" \
    --arg compose_inputs_hmac "${compose_inputs_hmac}" \
    --arg config_hmac "${config_hmac}" \
    --arg env_path "$(readlink -f -- "${N2API_ENV_FILE}")" \
    --arg env_hmac "${env_hmac}" \
    --arg platform "${platform}" \
    --arg services_fingerprint "${services_fingerprint}" \
    --argjson services_count "${services_count}" \
    --arg configured_image "${configured_image}" \
    --arg image_id "${image_id}" \
    --arg schema_version "${schema_version}" \
    --arg volume_name "${volume_name}" \
    --argjson volume_exists "${volume_exists}" \
    '{
      source:{
        git:$git,
        compose:{
          project:$project,
          files:[$compose_inputs[].path],
          inputs_hmac:$compose_inputs_hmac,
          config_hmac:$config_hmac
        },
        environment:{path:$env_path,hmac:$env_hmac},
        host:{platform:$platform},
        runtime:{
          services_fingerprint:$services_fingerprint,
          services_count:$services_count,
          configured_image:(if $configured_image == "" then null else $configured_image end),
          image_id:(if $image_id == "" then null else $image_id end),
          schema_version:(if ($schema_version | test("^[0-9]+$")) then ($schema_version | tonumber) else null end),
          postgres_volume_name:$volume_name,
          postgres_volume_exists:$volume_exists
        }
      },
      invariants:{
        environment_hmac:$env_hmac,
        compose_inputs_hmac:$compose_inputs_hmac,
        compose_config_hmac:$config_hmac,
        git_commit:$git.commit,
        git_dirty:$git.dirty,
        services_fingerprint:$services_fingerprint,
        current_image:(if $configured_image == "" then null else $configured_image end),
        current_image_id:(if $image_id == "" then null else $image_id end),
        schema_version:(if ($schema_version | test("^[0-9]+$")) then ($schema_version | tonumber) else null end),
        postgres_volume_name:$volume_name,
        postgres_volume_exists:$volume_exists
      }
    }'
}

n2api_plan_write() {
  local document=$1 operation_id target tmp integrity_hmac
  operation_id="$(jq -r '.operation_id' <<<"${document}")"
  n2api_validate_operation_id "${operation_id}" || return 1
  n2api_state_init || return 1
  integrity_hmac="$(n2api_state_hmac_json "${document}")" || return 1
  document="$(jq -ce --arg integrity_hmac "${integrity_hmac}" '. + {integrity_hmac:$integrity_hmac}' <<<"${document}")" || return 1
  target="${N2API_STATE_DIR}/plans/${operation_id}.json"
  [[ ! -e "${target}" ]] || return 1
  tmp="$(mktemp "${N2API_STATE_DIR}/plans/.${operation_id}.XXXXXX")" || return 1
  if ! jq -ce . <<<"${document}" >"${tmp}"; then
    rm -f -- "${tmp}"
    return 1
  fi
  chmod 600 -- "${tmp}" || { rm -f -- "${tmp}"; return 1; }
  mv -- "${tmp}" "${target}" || { rm -f -- "${tmp}"; return 1; }
  printf '%s\n' "${target}"
}

n2api_plan_file_is_safe() {
  local path=$1 owner mode
  [[ -f "${path}" && ! -L "${path}" && -r "${path}" ]] || return 1
  owner="$(n2api_path_owner "${path}")" || return 1
  mode="$(n2api_path_mode "${path}")" || return 1
  [[ "${owner}" == "$(id -u)" ]] || return 1
  ! n2api_mode_has_group_or_other_access "${mode}"
}

n2api_plan_read() {
  local path=$1 expected_operation=$2 output_variable=$3 document expected_hmac actual_hmac
  N2API_PLAN_READ_REASON="plan_file_unsafe"
  n2api_plan_file_is_safe "${path}" || return 1
  [[ "$(stat -c '%s' -- "${path}")" -le 1048576 ]] || return 1
  N2API_PLAN_READ_REASON="plan_schema_invalid"
  document="$(jq -ce '
    select(
      ((keys | sort) == (["blocked_reasons","checks","created_at","evidence","expires_at","integrity_hmac","invariants","operation","operation_id","risk","schema_version","source","target"] | sort)) and
      .schema_version == "n2api.ops.plan/v1" and
      (.operation_id | type == "string" and test("^op-[0-9]{8}T[0-9]{6}Z-[0-9a-f]{12}$")) and
      (.operation == "deploy" or .operation == "upgrade" or .operation == "rollback") and
      (.created_at | type == "string") and (.expires_at | type == "string") and
      (.blocked_reasons | type == "array") and (.checks | type == "array") and
      (.integrity_hmac | type == "string" and test("^[0-9a-f]{64}$"))
    )' "${path}" 2>/dev/null)" || return 1
  [[ "$(jq -r '.operation' <<<"${document}")" == "${expected_operation}" ]] || {
    N2API_PLAN_READ_REASON="plan_operation_mismatch"
    return 1
  }
  N2API_PLAN_READ_REASON="plan_integrity_invalid"
  expected_hmac="$(jq -r '.integrity_hmac' <<<"${document}")"
  actual_hmac="$(n2api_state_hmac_json "${document}")" || return 1
  [[ "${expected_hmac}" == "${actual_hmac}" ]] || return 1
  N2API_PLAN_READ_REASON=""
  printf -v "${output_variable}" '%s' "${document}"
}

n2api_plan_blocked_reasons() {
  jq -c '[.[] | select(.status == "failed" or .status == "blocked") | .reason_code] | unique' <<<"${N2API_CHECKS_JSON}"
}

n2api_deploy_plan() {
  (($# == 0)) || n2api_usage_error "deploy_plan_unexpected_argument"
  local operation_id started_at expires_at target_image target_manifest runtime context blocked plan path document
  local services_count volume_exists bind port listener status reason summary exit_code=0 artifacts
  operation_id="$(n2api_operation_id)"
  started_at="$(n2api_now)"
  expires_at="$(date -u -d '+15 minutes' +'%Y-%m-%dT%H:%M:%SZ')"
  n2api_state_init || {
    n2api_emit "deploy.plan" "local_write" "failed" false "operation_state_unsafe" "Operator state could not be initialized safely" '{}' '{}' '[]' '[]'
    exit "${N2API_EXIT_FAILED}"
  }
  if ! n2api_config_host_checks; then
    :
  fi
  if n2api_run_timeout docker info >/dev/null 2>&1; then
    n2api_check_add "docker.daemon" "passed" "docker_daemon_available" "Docker daemon is available"
  else
    n2api_check_add "docker.daemon" "failed" "docker_daemon_unavailable" "Docker daemon is unavailable"
  fi
  n2api_check_disk "${N2API_STATE_DIR}"
  n2api_check_backup_directory
  target_image="$(n2api_env_get N2API_IMAGE)"
  if target_manifest="$(n2api_image_inspect_json "${target_image}")"; then
    n2api_check_add "deploy.target" "passed" "target_image_available" "Target image manifest and platform are available"
  else
    target_manifest='{}'
    n2api_check_add "deploy.target" "failed" "target_image_unavailable" "Target image manifest or platform is unavailable"
  fi
  runtime="$(n2api_runtime_snapshot_json)" || {
    n2api_emit "deploy.plan" "local_write" "failed" false "runtime_snapshot_failed" "Runtime snapshot failed" '{}' '{}' '[]' '[]'
    exit "${N2API_EXIT_FAILED}"
  }
  context="$(n2api_plan_context_json "${runtime}")" || {
    n2api_emit "deploy.plan" "local_write" "failed" false "plan_context_failed" "Plan context could not be protected" '{}' '{}' '[]' '[]'
    exit "${N2API_EXIT_FAILED}"
  }
  services_count="$(jq -r '.source.runtime.services_count' <<<"${context}")"
  volume_exists="$(jq -r '.source.runtime.postgres_volume_exists' <<<"${context}")"
  if ((services_count == 0)); then
    n2api_check_add "deploy.services" "passed" "first_deploy_services_absent" "No existing Compose services were found"
  else
    n2api_check_add "deploy.services" "failed" "first_deploy_service_exists" "Existing Compose services prevent first deployment"
  fi
  if [[ "${volume_exists}" == false ]]; then
    n2api_check_add "deploy.volume" "passed" "first_deploy_volume_absent" "No existing production database volume was found"
  else
    n2api_check_add "deploy.volume" "failed" "first_deploy_volume_exists" "Existing production data prevents first deployment"
  fi
  bind="$(n2api_env_get N2API_BIND_ADDRESS 127.0.0.1)"
  port="$(n2api_env_get N2API_PORT 3000)"
  listener="$(ss -H -ltn "sport = :${port}" 2>/dev/null || true)"
  if [[ -z "${listener}" ]]; then
    n2api_check_add "deploy.port" "passed" "port_available" "Configured host port is available"
  else
    n2api_check_add "deploy.port" "failed" "port_unavailable" "Configured host port is already in use"
  fi
  n2api_check_add "deploy.owner_gate" "attention" "reverse_proxy_tls_owner_gate" "Reverse proxy, TLS, firewall, and DNS remain owner responsibilities"
  blocked="$(n2api_plan_blocked_reasons)"
  plan="$(jq -cn \
    --arg schema_version "${N2API_PLAN_SCHEMA_VERSION}" \
    --arg operation_id "${operation_id}" \
    --arg created_at "${started_at}" \
    --arg expires_at "${expires_at}" \
    --argjson context "${context}" \
    --arg target_image "${target_image}" \
    --argjson manifest "${target_manifest}" \
    --argjson checks "${N2API_CHECKS_JSON}" \
    --argjson blocked "${blocked}" \
    '{
      schema_version:$schema_version,
      operation_id:$operation_id,
      operation:"deploy",
      created_at:$created_at,
      expires_at:$expires_at,
      risk:"service_change",
      source:$context.source,
      target:{
        image:{
          reference:$target_image,
          repository:($manifest.repository // "unavailable"),
          tag:($manifest.tag // "unavailable"),
          digest:($manifest.digest // "sha256:0000000000000000000000000000000000000000000000000000000000000000"),
          platforms:($manifest.platforms // [])
        },
        migration_possible:true,
        rollback_image:null
      },
      evidence:{backup:{availability:"not_applicable"},current_restore:{availability:"not_applicable"},candidate_restore:{availability:"not_applicable"},rollback_compatibility:{status:"not_applicable"}},
      invariants:($context.invariants + {target_manifest_digest:($manifest.digest // null)}),
      checks:$checks,
      blocked_reasons:$blocked
    }')"
  path="$(n2api_plan_write "${plan}")" || {
    n2api_emit "deploy.plan" "local_write" "failed" false "plan_write_failed" "Deploy plan could not be written safely" '{}' '{}' '[]' '[]'
    exit "${N2API_EXIT_FAILED}"
  }
  if [[ "$(jq -r 'length' <<<"${blocked}")" -gt 0 ]]; then
    status="blocked"; reason="deploy_plan_blocked"; summary="Deploy plan contains blocking checks"; exit_code=${N2API_EXIT_BLOCKED}
  else
    status="succeeded"; reason="deploy_plan_created"; summary="Deploy plan is ready for apply"
  fi
  artifacts="$(jq -cn --arg path "${path}" '[{type:"operation_plan",path:$path,mode:"0600"}]')"
  document="$(n2api_envelope_json "deploy.plan" "local_write" "${status}" true "${reason}" "${summary}" \
    "$(jq -cn --arg plan_operation_id "${operation_id}" --arg expires_at "${expires_at}" '{plan_operation_id:$plan_operation_id,expires_at:$expires_at}')" \
    "$(jq -cn --arg image "${target_image}" '{image:$image}')" "${artifacts}" '["Review checks, then run deploy apply --plan PATH"]' \
    "${operation_id}" "${started_at}")"
  n2api_operation_write "${operation_id}" "${document}" || {
    n2api_emit "deploy.plan" "local_write" "failed" true "operation_receipt_failed" "Plan was written but its receipt failed" '{}' '{}' "${artifacts}" '[]'
    exit "${N2API_EXIT_FAILED}"
  }
  n2api_emit_document "${document}"
  exit "${exit_code}"
}

n2api_upgrade_plan() {
  local target_image="" operation_id started_at expires_at runtime context source_image source_schema target_manifest
  local backup='{}' current_restore='{}' candidate_restore='{}' rollback_manifest='{}' blocked plan path document
  local status reason summary exit_code=0 artifacts pull_status same_target=false
  while (($# > 0)); do
    case "$1" in
      --image) (($# >= 2)) || n2api_usage_error "missing_upgrade_image"; target_image=$2; shift 2 ;;
      *) n2api_usage_error "unknown_upgrade_plan_option" ;;
    esac
  done
  [[ -n "${target_image}" ]] || n2api_usage_error "upgrade_plan_requires_image"
  n2api_image_reference_is_valid "${target_image}" || n2api_usage_error "invalid_upgrade_image"
  operation_id="$(n2api_operation_id)"
  started_at="$(n2api_now)"
  expires_at="$(date -u -d '+15 minutes' +'%Y-%m-%dT%H:%M:%SZ')"
  n2api_state_init || {
    n2api_emit "upgrade.plan" "local_write" "failed" false "operation_state_unsafe" "Operator state could not be initialized safely" '{}' '{}' '[]' '[]'
    exit "${N2API_EXIT_FAILED}"
  }
  if ! n2api_config_host_checks; then :; fi
  runtime="$(n2api_runtime_snapshot_json)" || {
    n2api_emit "upgrade.plan" "local_write" "failed" false "runtime_snapshot_failed" "Runtime snapshot failed" '{}' '{}' '[]' '[]'
    exit "${N2API_EXIT_FAILED}"
  }
  context="$(n2api_plan_context_json "${runtime}")" || {
    n2api_emit "upgrade.plan" "local_write" "failed" false "plan_context_failed" "Plan context could not be protected" '{}' '{}' '[]' '[]'
    exit "${N2API_EXIT_FAILED}"
  }
  source_image="$(jq -r '.source.runtime.configured_image // empty' <<<"${context}")"
  source_schema="$(jq -r '.source.runtime.schema_version // empty' <<<"${context}")"
  if n2api_image_reference_is_valid "${source_image}" && [[ "${source_schema}" =~ ^[1-9][0-9]*$ ]]; then
    n2api_check_add "upgrade.source" "passed" "upgrade_source_verified" "Running source image and schema are available"
  else
    n2api_check_add "upgrade.source" "failed" "upgrade_source_unavailable" "Running source image or schema is unavailable"
  fi
  if [[ "${target_image}" == "${source_image}" ]] && n2api_runtime_exact_target_healthy "${runtime}" "${target_image}"; then
    same_target=true
    n2api_check_add "upgrade.target" "passed" "target_already_healthy" "Upgrade target is already running and healthy"
    target_manifest="$(n2api_image_inspect_json "${target_image}")" || target_manifest='{}'
  else
    set +e
    n2api_run_timeout docker pull --quiet "${target_image}" >/dev/null 2>&1
    pull_status=$?
    set -e
    if [[ ${pull_status} -eq 0 ]] && target_manifest="$(n2api_image_inspect_json "${target_image}")" && n2api_local_image_digest_matches "${target_image}"; then
      n2api_check_add "upgrade.target" "passed" "candidate_image_available" "Candidate image was pulled and verified without recreating services"
    elif n2api_status_is_timeout "${pull_status}"; then
      target_manifest='{}'
      n2api_check_add "upgrade.target" "failed" "candidate_image_pull_timeout" "Candidate image pull exceeded its timeout"
    else
      target_manifest='{}'
      n2api_check_add "upgrade.target" "failed" "candidate_image_unavailable" "Candidate image could not be pulled and verified"
    fi
  fi
  if [[ -z "${target_manifest:-}" ]]; then
    target_manifest="$(n2api_image_inspect_json "${target_image}" 2>/dev/null || printf '{}')"
  fi
  if [[ "${same_target}" == false && -n "${source_image}" && "${source_schema}" =~ ^[1-9][0-9]*$ ]]; then
    if backup="$(n2api_real_backup_evidence_json "${source_image}" "${source_schema}")"; then
      n2api_check_add "upgrade.backup" "passed" "backup_evidence_valid" "Fresh real-operator backup evidence is valid"
      if [[ "$(jq -r '.off_host_status' <<<"${backup}")" == attention_missing ]]; then
        n2api_check_add "upgrade.off_host" "attention" "backup_off_host_attention" "Encrypted off-host custody remains an owner gate"
      fi
      if current_restore="$(n2api_real_restore_evidence_json "${source_image}" "$(jq -r '.backup_id' <<<"${backup}")" "$(jq -r '.checksum' <<<"${backup}")")"; then
        n2api_check_add "upgrade.current_restore" "passed" "current_restore_evidence_valid" "Current-image restore evidence is valid"
      else
        current_restore='{"availability":"unavailable"}'
        n2api_check_add "upgrade.current_restore" "failed" "current_restore_missing" "Current-image real restore evidence is missing or stale"
      fi
      if candidate_restore="$(n2api_real_restore_evidence_json "${target_image}" "$(jq -r '.backup_id' <<<"${backup}")" "$(jq -r '.checksum' <<<"${backup}")")"; then
        n2api_check_add "upgrade.candidate_restore" "passed" "candidate_restore_evidence_valid" "Candidate-image restore evidence is valid"
      else
        candidate_restore='{"availability":"unavailable"}'
        n2api_check_add "upgrade.candidate_restore" "failed" "candidate_restore_missing" "Candidate-image real restore evidence is missing or stale"
      fi
    else
      backup='{"availability":"unavailable"}'
      current_restore='{"availability":"unavailable"}'
      candidate_restore='{"availability":"unavailable"}'
      n2api_check_add "upgrade.backup" "failed" "backup_missing" "Fresh signed real-operator backup evidence is missing"
    fi
    if rollback_manifest="$(n2api_image_inspect_json "${source_image}")"; then
      n2api_check_add "upgrade.rollback" "passed" "rollback_image_available" "Exact source image remains available as rollback target"
    else
      rollback_manifest='{}'
      n2api_check_add "upgrade.rollback" "failed" "rollback_image_unavailable" "Exact source image is unavailable"
    fi
  else
    backup='{"availability":"not_applicable"}'
    current_restore='{"availability":"not_applicable"}'
    candidate_restore='{"availability":"not_applicable"}'
    rollback_manifest="${target_manifest}"
  fi
  blocked="$(n2api_plan_blocked_reasons)"
  plan="$(jq -cn \
    --arg schema_version "${N2API_PLAN_SCHEMA_VERSION}" \
    --arg operation_id "${operation_id}" \
    --arg created_at "${started_at}" \
    --arg expires_at "${expires_at}" \
    --argjson context "${context}" \
    --arg target_image "${target_image}" \
    --arg source_image "${source_image}" \
    --argjson manifest "${target_manifest}" \
    --argjson backup "${backup}" \
    --argjson current_restore "${current_restore}" \
    --argjson candidate_restore "${candidate_restore}" \
    --argjson checks "${N2API_CHECKS_JSON}" \
    --argjson blocked "${blocked}" \
    '{
      schema_version:$schema_version,
      operation_id:$operation_id,
      operation:"upgrade",
      created_at:$created_at,
      expires_at:$expires_at,
      risk:"service_change",
      source:$context.source,
      target:{
        image:{reference:$target_image,repository:($manifest.repository // "unavailable"),tag:($manifest.tag // "unavailable"),digest:($manifest.digest // "sha256:0000000000000000000000000000000000000000000000000000000000000000"),platforms:($manifest.platforms // [])},
        migration_possible:true,
        rollback_image:(if $source_image == "" then null else $source_image end)
      },
      evidence:{backup:$backup,current_restore:$current_restore,candidate_restore:$candidate_restore,rollback_compatibility:{status:"source_snapshot",basis:"pre_upgrade_schema"}},
      invariants:($context.invariants + {
        target_manifest_digest:($manifest.digest // null),
        backup_checksum:($backup.checksum // null),
        backup_metadata_hmac:($backup.metadata_hmac // null),
        current_restore_hmac:($current_restore.receipt_hmac // null),
        candidate_restore_hmac:($candidate_restore.receipt_hmac // null),
        rollback_manifest_digest:(if $source_image == "" then null else ($source_image | split("@") | last) end)
      }),
      checks:$checks,
      blocked_reasons:$blocked
    }')"
  path="$(n2api_plan_write "${plan}")" || {
    n2api_emit "upgrade.plan" "local_write" "failed" false "plan_write_failed" "Upgrade plan could not be written safely" '{}' '{}' '[]' '[]'
    exit "${N2API_EXIT_FAILED}"
  }
  if [[ "${same_target}" == true && "$(jq -r 'length' <<<"${blocked}")" -eq 0 ]]; then
    status="noop"; reason="target_already_healthy"; summary="Upgrade target is already healthy"
  elif [[ "$(jq -r 'length' <<<"${blocked}")" -gt 0 ]]; then
    status="blocked"; reason="upgrade_plan_blocked"; summary="Upgrade plan contains blocking checks"; exit_code=${N2API_EXIT_BLOCKED}
  else
    status="succeeded"; reason="upgrade_plan_created"; summary="Upgrade plan is ready for apply"
  fi
  artifacts="$(jq -cn --arg path "${path}" '[{type:"operation_plan",path:$path,mode:"0600"}]')"
  document="$(n2api_envelope_json "upgrade.plan" "local_write" "${status}" true "${reason}" "${summary}" \
    "$(jq -cn --arg plan_operation_id "${operation_id}" --arg expires_at "${expires_at}" '{plan_operation_id:$plan_operation_id,expires_at:$expires_at}')" \
    "$(jq -cn --arg image "${target_image}" --arg rollback_image "${source_image}" '{image:$image,rollback_image:$rollback_image}')" \
    "${artifacts}" '["Review evidence and checks, then run upgrade apply --plan PATH"]' "${operation_id}" "${started_at}")"
  n2api_operation_write "${operation_id}" "${document}" || {
    n2api_emit "upgrade.plan" "local_write" "failed" true "operation_receipt_failed" "Plan was written but its receipt failed" '{}' '{}' "${artifacts}" '[]'
    exit "${N2API_EXIT_FAILED}"
  }
  n2api_emit_document "${document}"
  exit "${exit_code}"
}

n2api_plan_operation() {
  local operation=$1
  shift
  case "${operation}" in
    deploy) n2api_deploy_plan "$@" ;;
    upgrade) n2api_upgrade_plan "$@" ;;
    *) n2api_usage_error "unsupported_plan_operation" ;;
  esac
}
