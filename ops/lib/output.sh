#!/usr/bin/env bash

n2api_emit_text_value() {
  local prefix=$1 json=$2
  jq -r --arg prefix "${prefix}" '
    paths(scalars) as $path
    | ($path | map(tostring) | join(".")) as $key
    | "\($prefix)\($key)=\(getpath($path))"
  ' <<<"${json}"
}

n2api_emit() {
  local command=$1 risk=$2 status=$3 changed=$4 reason_code=$5 summary=$6
  local current_json=$7 target_json=$8 artifacts_json=$9 next_actions_json=${10}
  local operation_id=${11:-} started_at=${12:-$(n2api_now)} finished_at
  local checks_json=${N2API_CHECKS_JSON:-[]} document
  finished_at="$(n2api_now)"

  document="$(jq -cn \
    --arg schema_version "${N2API_OPS_SCHEMA_VERSION}" \
    --arg operation_id "${operation_id}" \
    --arg command "${command}" \
    --arg risk "${risk}" \
    --arg status "${status}" \
    --argjson changed "${changed}" \
    --arg started_at "${started_at}" \
    --arg finished_at "${finished_at}" \
    --arg reason_code "${reason_code}" \
    --arg summary "${summary}" \
    --argjson checks "${checks_json}" \
    --argjson current "${current_json}" \
    --argjson target "${target_json}" \
    --argjson artifacts "${artifacts_json}" \
    --argjson next_actions "${next_actions_json}" \
    '{
      schema_version: $schema_version,
      operation_id: (if $operation_id == "" then null else $operation_id end),
      command: $command,
      risk: $risk,
      status: $status,
      changed: $changed,
      started_at: $started_at,
      finished_at: $finished_at,
      reason_code: $reason_code,
      summary: $summary,
      checks: $checks,
      current: $current,
      target: $target,
      artifacts: $artifacts,
      next_actions: $next_actions
    }')"

  if [[ "${N2API_FORMAT}" == "json" ]]; then
    jq -c . <<<"${document}"
    return
  fi

  printf 'command=%s\nstatus=%s\nreason_code=%s\nchanged=%s\nsummary=%s\n' \
    "${command}" "${status}" "${reason_code}" "${changed}" "${summary}"
  if [[ "${current_json}" != '{}' ]]; then
    n2api_emit_text_value 'current.' "${current_json}"
  fi
  if [[ "${target_json}" != '{}' ]]; then
    n2api_emit_text_value 'target.' "${target_json}"
  fi
}
