#!/usr/bin/env bash

n2api_emit_text_value() {
  local prefix=$1 json=$2
  jq -r --arg prefix "${prefix}" '
    paths(scalars) as $path
    | ($path | map(tostring) | join(".")) as $key
    | "\($prefix)\($key)=\(getpath($path))"
  ' <<<"${json}"
}

n2api_envelope_json() {
  local command=$1 risk=$2 status=$3 changed=$4 reason_code=$5 summary=$6
  local current_json=$7 target_json=$8 artifacts_json=$9 next_actions_json=${10}
  local operation_id=${11:-} started_at=${12:-$(n2api_now)} finished_at
  local checks_json=${N2API_CHECKS_JSON:-[]}
  finished_at="$(n2api_now)"

  jq -cn \
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
    }'
}

n2api_emit_document() {
  local document=$1
  if [[ "${N2API_FORMAT}" == "json" ]]; then
    jq -c . <<<"${document}" >&"${N2API_OUTPUT_FD:-1}"
    return
  fi

  {
    jq -r '"command=\(.command)\nstatus=\(.status)\nreason_code=\(.reason_code)\nchanged=\(.changed)\nsummary=\(.summary)"' <<<"${document}"
    if [[ "$(jq -c '.current' <<<"${document}")" != '{}' ]]; then
      n2api_emit_text_value 'current.' "$(jq -c '.current' <<<"${document}")"
    fi
    if [[ "$(jq -c '.target' <<<"${document}")" != '{}' ]]; then
      n2api_emit_text_value 'target.' "$(jq -c '.target' <<<"${document}")"
    fi
  } >&"${N2API_OUTPUT_FD:-1}"
}

n2api_emit() {
  local document
  document="$(n2api_envelope_json "$@")"
  n2api_emit_document "${document}"
}
