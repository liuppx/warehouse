#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

LEVEL="${HEALTH_LEVEL:-readiness}"
TIMEOUT="${HEALTH_TIMEOUT:-10}"
RETRIES="${HEALTH_RETRIES:-0}"
INTERVAL="${HEALTH_INTERVAL:-2}"
FORMAT="${HEALTH_FORMAT:-text}"
CONFIG="${HEALTH_CONFIG:-}"
BASE_URL="${HEALTH_BASE_URL:-}"
PID_FILE="${WAREHOUSE_HEALTH_PID_FILE:-${PROJECT_DIR}/run/warehouse.pid}"
QUIET=0
LOGFILE=""

CHECK_NAMES=()
CHECK_STATUSES=()
CHECK_DURATIONS=()
CHECK_MESSAGES=()
PASSED=0
WARNED=0
FAILED=0
SKIPPED=0
TIMED_OUT=0

init_log_file() {
  local logfile_name=$1
  local logfile_dir="/opt/logs"
  local logfile_path="${logfile_dir}/${logfile_name}"

  if ! mkdir -p "${logfile_dir}" 2>/dev/null || ! touch "${logfile_path}" 2>/dev/null; then
    printf 'health-check: failed to initialize log file: %s\n' "${logfile_path}" >&2
    LOGFILE=""
    return 0
  fi

  LOGFILE="${logfile_path}"

  local filesize=0
  filesize=$(stat -c "%s" "${LOGFILE}" 2>/dev/null || echo 0)
  if [[ "${filesize}" -ge 1048576 ]]; then
    printf 'clear old logs at %s to avoid log file too big\n' "$(date)" >"${LOGFILE}"
  fi
}

log() {
  [[ -n "${LOGFILE}" ]] || return 0
  printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$*" >>"${LOGFILE}"
}

log_err() {
  local message
  message="[$(date '+%Y-%m-%d %H:%M:%S')] $*"
  if [[ -n "${LOGFILE}" ]]; then
    printf '%s\n' "${message}" | tee -a "${LOGFILE}" >&2
  else
    printf '%s\n' "${message}" >&2
  fi
}

init_log_file "health-check-warehouse.log"

usage() {
  cat <<'EOF'
Usage: scripts/health-check.sh [options]

Options:
  --level <level>       liveness, readiness, dependency, or all (default: readiness)
  --timeout <seconds>   Per-check timeout (default: 10)
  --retries <count>     Retries after the first failed attempt (default: 0)
  --interval <seconds>  Delay between retries (default: 2)
  --format <format>     text or json (default: text)
  --config <path>       Warehouse config file used to resolve the local endpoint
  --base-url <url>      Override the Warehouse HTTP base URL
  --quiet               Only print the final result and errors
  --help                Show this help

Environment variables:
  HEALTH_LEVEL, HEALTH_TIMEOUT, HEALTH_RETRIES, HEALTH_INTERVAL,
  HEALTH_FORMAT, HEALTH_CONFIG, HEALTH_BASE_URL

Warehouse-specific environment variables:
  WAREHOUSE_HEALTH_PID_FILE     PID file path (default: <project>/run/warehouse.pid)
EOF
}

usage_error() {
  log_err "health-check: $1"
  log_err "Try --help for usage."
  exit 2
}

require_value() {
  if [[ $# -lt 2 || -z "${2:-}" ]]; then
    usage_error "$1 requires a value"
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --level)
      require_value "$@"
      LEVEL="$2"
      shift 2
      ;;
    --timeout)
      require_value "$@"
      TIMEOUT="$2"
      shift 2
      ;;
    --retries)
      require_value "$@"
      RETRIES="$2"
      shift 2
      ;;
    --interval)
      require_value "$@"
      INTERVAL="$2"
      shift 2
      ;;
    --format)
      require_value "$@"
      FORMAT="$2"
      shift 2
      ;;
    --config)
      require_value "$@"
      CONFIG="$2"
      shift 2
      ;;
    --base-url)
      require_value "$@"
      BASE_URL="$2"
      shift 2
      ;;
    --quiet)
      QUIET=1
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      usage_error "unknown argument: $1"
      ;;
  esac
done

case "${LEVEL}" in
  liveness|readiness|dependency|all) ;;
  *) usage_error "invalid level: ${LEVEL}" ;;
esac

case "${FORMAT}" in
  text|json) ;;
  *) usage_error "invalid format: ${FORMAT}" ;;
esac

if [[ ! "${TIMEOUT}" =~ ^[1-9][0-9]*$ ]]; then
  usage_error "timeout must be a positive integer"
fi
if [[ ! "${RETRIES}" =~ ^[0-9]+$ ]]; then
  usage_error "retries must be a non-negative integer"
fi
if [[ ! "${INTERVAL}" =~ ^[0-9]+([.][0-9]+)?$ ]]; then
  usage_error "interval must be a non-negative number"
fi

if ! command -v curl >/dev/null 2>&1; then
  log_err "health-check: required command not found: curl"
  exit 3
fi

if [[ -z "${CONFIG}" ]]; then
  if [[ -f "${PROJECT_DIR}/config.yaml" ]]; then
    CONFIG="${PROJECT_DIR}/config.yaml"
  elif [[ -f "${PROJECT_DIR}/config.yaml.template" ]]; then
    CONFIG="${PROJECT_DIR}/config.yaml.template"
  fi
elif [[ "${CONFIG}" != /* ]]; then
  CONFIG="${PROJECT_DIR}/${CONFIG}"
fi

config_server_value() {
  local key="$1"
  [[ -n "${CONFIG}" && -f "${CONFIG}" ]] || return 1
  awk -v wanted="${key}" '
    /^[[:space:]]*server:[[:space:]]*$/ { in_server=1; next }
    in_server && /^[^[:space:]#]/ { exit }
    in_server {
      line=$0
      sub(/[[:space:]]+#.*/, "", line)
      if (line ~ "^[[:space:]]*" wanted ":[[:space:]]*") {
        sub("^[[:space:]]*" wanted ":[[:space:]]*", "", line)
        gsub(/^[[:space:]\"]+|[[:space:]\"]+$/, "", line)
        print line
        exit
      }
    }
  ' "${CONFIG}"
}

if [[ -z "${BASE_URL}" ]]; then
  SERVER_ADDRESS="$(config_server_value address 2>/dev/null || true)"
  SERVER_PORT="$(config_server_value port 2>/dev/null || true)"
  SERVER_TLS="$(config_server_value tls 2>/dev/null || true)"
  [[ -n "${SERVER_ADDRESS}" ]] || SERVER_ADDRESS="localhost"
  [[ -n "${SERVER_PORT}" ]] || SERVER_PORT="6065"
  case "${SERVER_ADDRESS}" in
    0.0.0.0|::|"[::]") SERVER_ADDRESS="localhost" ;;
  esac
  SCHEME="http"
  [[ "${SERVER_TLS}" == "true" ]] && SCHEME="https"
  if [[ "${SERVER_ADDRESS}" == *:* && "${SERVER_ADDRESS}" != \[*\] ]]; then
    SERVER_ADDRESS="[${SERVER_ADDRESS}]"
  fi
  BASE_URL="${SCHEME}://${SERVER_ADDRESS}:${SERVER_PORT}"
fi
BASE_URL="${BASE_URL%/}"
log "start health check: level=${LEVEL} timeout=${TIMEOUT}s retries=${RETRIES} interval=${INTERVAL}s format=${FORMAT} base_url=${BASE_URL} pid_file=${PID_FILE}"

now_ms() {
  if command -v python3 >/dev/null 2>&1; then
    python3 -c 'import time; print(int(time.time() * 1000))'
  else
    echo "$(($(date +%s) * 1000))"
  fi
}

STARTED_AT="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
START_MS="$(now_ms)"

VERSION="unknown"
if [[ -f "${PROJECT_DIR}/VERSION" ]]; then
  VERSION="$(head -n 1 "${PROJECT_DIR}/VERSION" | tr -d '\r\n')"
elif command -v git >/dev/null 2>&1 && git -C "${PROJECT_DIR}" rev-parse --git-dir >/dev/null 2>&1; then
  VERSION="$(git -C "${PROJECT_DIR}" describe --tags --always --dirty 2>/dev/null || echo unknown)"
fi

json_escape() {
  local value="$1"
  value=${value//\\/\\\\}
  value=${value//\"/\\\"}
  value=${value//$'\n'/\\n}
  value=${value//$'\r'/\\r}
  value=${value//$'\t'/\\t}
  printf '%s' "${value}"
}

record_check() {
  local name="$1" status="$2" duration="$3" message="$4"
  CHECK_NAMES+=("${name}")
  CHECK_STATUSES+=("${status}")
  CHECK_DURATIONS+=("${duration}")
  CHECK_MESSAGES+=("${message}")
  case "${status}" in
    pass) PASSED=$((PASSED + 1)) ;;
    warn) WARNED=$((WARNED + 1)) ;;
    fail) FAILED=$((FAILED + 1)) ;;
    skip) SKIPPED=$((SKIPPED + 1)) ;;
  esac
  log "check ${name}: status=${status} duration_ms=${duration} message=${message}"
}

check_process() {
  local check_start check_end duration pid process_state
  check_start="$(now_ms)"

  if [[ ! -f "${PID_FILE}" ]]; then
    check_end="$(now_ms)"
    duration=$((check_end - check_start))
    record_check "process" fail "${duration}" "PID file is missing: ${PID_FILE}"
    return 1
  fi

  pid="$(tr -d '[:space:]' <"${PID_FILE}" 2>/dev/null || true)"
  if [[ ! "${pid}" =~ ^[1-9][0-9]*$ ]]; then
    check_end="$(now_ms)"
    duration=$((check_end - check_start))
    record_check "process" fail "${duration}" "PID file does not contain a valid process ID"
    return 1
  fi
  if ! kill -0 "${pid}" >/dev/null 2>&1; then
    check_end="$(now_ms)"
    duration=$((check_end - check_start))
    record_check "process" fail "${duration}" "warehouse process ${pid} is not running"
    return 1
  fi

  if command -v ps >/dev/null 2>&1; then
    process_state="$(ps -o stat= -p "${pid}" 2>/dev/null | tr -d '[:space:]' || true)"
    if [[ "${process_state}" == Z* ]]; then
      check_end="$(now_ms)"
      duration=$((check_end - check_start))
      record_check "process" fail "${duration}" "warehouse process ${pid} is a zombie"
      return 1
    fi
  fi

  check_end="$(now_ms)"
  duration=$((check_end - check_start))
  record_check "process" pass "${duration}" "warehouse process ${pid} is running"
}

run_http_check() {
  local name="$1" path="$2" success_message="$3" validator="$4"
  local attempt=0 max_attempts=$((RETRIES + 1))
  local check_start check_end duration body_file http_code curl_rc message
  check_start="$(now_ms)"
  body_file="$(mktemp "${TMPDIR:-/tmp}/warehouse-health.XXXXXX")" || {
    log_err "health-check: failed to create temporary file"
    exit 3
  }

  while (( attempt < max_attempts )); do
    attempt=$((attempt + 1))
    : >"${body_file}"
    curl_rc=0
    http_code="$(curl --silent --show-error --output "${body_file}" --write-out '%{http_code}' \
      --connect-timeout "${TIMEOUT}" --max-time "${TIMEOUT}" \
      --header 'Accept: application/json' "${BASE_URL}${path}" 2>/dev/null)" || curl_rc=$?

    if [[ ${curl_rc} -eq 0 && "${http_code}" =~ ^2[0-9][0-9]$ ]] && "${validator}" "${body_file}"; then
      check_end="$(now_ms)"
      duration=$((check_end - check_start))
      rm -f "${body_file}"
      record_check "${name}" pass "${duration}" "${success_message}"
      return 0
    fi

    if (( attempt < max_attempts )); then
      sleep "${INTERVAL}"
    fi
  done

  check_end="$(now_ms)"
  duration=$((check_end - check_start))
  if [[ ${curl_rc} -eq 28 ]]; then
    TIMED_OUT=1
    message="GET ${path} timed out after ${TIMEOUT}s"
  elif [[ ${curl_rc} -ne 0 ]]; then
    message="GET ${path} failed to connect (curl exit ${curl_rc})"
  elif [[ ! "${http_code}" =~ ^2[0-9][0-9]$ ]]; then
    message="GET ${path} returned HTTP ${http_code}"
  else
    message="GET ${path} returned an unexpected response"
  fi
  rm -f "${body_file}"
  record_check "${name}" fail "${duration}" "${message}"
  return 1
}

validate_heartbeat() {
  grep -Eq '"status"[[:space:]]*:[[:space:]]*"healthy"' "$1"
}

validate_readiness() {
  grep -Eq '"status"[[:space:]]*:[[:space:]]*"ready"' "$1"
}

validate_dependencies() {
  local body="$1"
  validate_readiness "${body}" &&
    grep -Eq '"name"[[:space:]]*:[[:space:]]*"database"' "${body}" &&
    grep -Eq '"name"[[:space:]]*:[[:space:]]*"webdav_directory"' "${body}" &&
    [[ "$(grep -Eo '"status"[[:space:]]*:[[:space:]]*"ready"' "${body}" | wc -l | tr -d ' ')" -ge 3 ]]
}

check_liveness() {
  check_process || true
  run_http_check "http_liveness" "/api/v1/public/health/heartbeat" \
    "heartbeat endpoint returned healthy" validate_heartbeat || true
}

check_readiness() {
  run_http_check "http_readiness" "/api/v1/public/health/readiness" \
    "readiness endpoint returned ready" validate_readiness || true
}

check_dependencies() {
  run_http_check "required_dependencies" "/api/v1/public/health/readiness" \
    "PostgreSQL and WebDAV directory are ready" validate_dependencies || true
}

case "${LEVEL}" in
  liveness)
    check_liveness
    ;;
  readiness)
    check_liveness
    check_readiness
    ;;
  dependency)
    check_dependencies
    ;;
  all)
    check_liveness
    check_readiness
    check_dependencies
    ;;
esac

END_MS="$(now_ms)"
DURATION_MS=$((END_MS - START_MS))
RESULT_STATUS="pass"
if (( FAILED > 0 )); then
  RESULT_STATUS="fail"
elif (( WARNED > 0 )); then
  RESULT_STATUS="warn"
fi

if [[ "${FORMAT}" == "json" ]]; then
  printf '{'
  printf '"schema_version":"1.0",'
  printf '"type":"health_check",'
  printf '"project":"warehouse",'
  printf '"version":"%s",' "$(json_escape "${VERSION}")"
  printf '"environment":"%s",' "$(json_escape "${WAREHOUSE_ENVIRONMENT:-unknown}")"
  printf '"level":"%s",' "${LEVEL}"
  printf '"status":"%s",' "${RESULT_STATUS}"
  printf '"started_at":"%s",' "${STARTED_AT}"
  printf '"duration_ms":%s,' "${DURATION_MS}"
  printf '"summary":{"passed":%s,"warned":%s,"failed":%s,"skipped":%s},' \
    "${PASSED}" "${WARNED}" "${FAILED}" "${SKIPPED}"
  printf '"checks":['
  for ((i = 0; i < ${#CHECK_NAMES[@]}; i++)); do
    if (( i > 0 )); then
      printf ','
    fi
    printf '{"name":"%s","status":"%s","duration_ms":%s,"message":"%s"}' \
      "$(json_escape "${CHECK_NAMES[$i]}")" \
      "${CHECK_STATUSES[$i]}" \
      "${CHECK_DURATIONS[$i]}" \
      "$(json_escape "${CHECK_MESSAGES[$i]}")"
  done
  printf ']}'
  printf '\n'
else
  if (( QUIET == 0 )); then
    for ((i = 0; i < ${#CHECK_NAMES[@]}; i++)); do
      printf '[%s] %s: %s (%s ms)\n' \
        "$(printf '%s' "${CHECK_STATUSES[$i]}" | tr '[:lower:]' '[:upper:]')" \
        "${CHECK_NAMES[$i]}" \
        "${CHECK_MESSAGES[$i]}" \
        "${CHECK_DURATIONS[$i]}"
    done
  fi
  printf 'RESULT status=%s passed=%s warned=%s failed=%s skipped=%s duration_ms=%s\n' \
    "${RESULT_STATUS}" "${PASSED}" "${WARNED}" "${FAILED}" "${SKIPPED}" "${DURATION_MS}"
fi

if (( FAILED == 0 )); then
  log "finish health check: status=${RESULT_STATUS} exit_code=0 passed=${PASSED} warned=${WARNED} failed=${FAILED} skipped=${SKIPPED} duration_ms=${DURATION_MS}"
  exit 0
fi
if (( TIMED_OUT == 1 )); then
  log "finish health check: status=${RESULT_STATUS} exit_code=4 passed=${PASSED} warned=${WARNED} failed=${FAILED} skipped=${SKIPPED} duration_ms=${DURATION_MS}"
  exit 4
fi
log "finish health check: status=${RESULT_STATUS} exit_code=1 passed=${PASSED} warned=${WARNED} failed=${FAILED} skipped=${SKIPPED} duration_ms=${DURATION_MS}"
exit 1
