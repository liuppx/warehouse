#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Load the local test configuration automatically. Values explicitly exported by
# the caller take precedence, so CI and production secret injection remain safe.
TEST_ENV_FILE="${TEST_ENV_FILE:-${PROJECT_DIR}/.env}"
if [[ -f "${TEST_ENV_FILE}" ]]; then
  EXPORTED_TEST_VARIABLES=()
  EXPORTED_TEST_VALUES=()
  while IFS= read -r variable_name; do
    if [[ -n "${variable_name}" ]]; then
      EXPORTED_TEST_VARIABLES+=("${variable_name}")
      EXPORTED_TEST_VALUES+=("${!variable_name}")
    fi
  done < <(compgen -e | grep -E '^(TEST_|WAREHOUSE_TEST_|WAREHOUSE_HEALTH_|HEALTH_)' || true)
  set -a
  # shellcheck disable=SC1090
  source "${TEST_ENV_FILE}"
  set +a
  for ((environment_index=0; environment_index<${#EXPORTED_TEST_VARIABLES[@]}; environment_index++)); do
    export "${EXPORTED_TEST_VARIABLES[$environment_index]}=${EXPORTED_TEST_VALUES[$environment_index]}"
  done
  unset variable_name environment_index EXPORTED_TEST_VARIABLES EXPORTED_TEST_VALUES
fi

SUITE="${TEST_SUITE:-unit}"
TIMEOUT="${TEST_TIMEOUT:-600}"
FORMAT="${TEST_FORMAT:-text}"
OUTPUT="${TEST_OUTPUT:-}"
ENVIRONMENT="${TEST_ENVIRONMENT:-local}"
BASE_URL="${TEST_BASE_URL:-${HEALTH_BASE_URL:-http://localhost:6065}}"
RUN_ID="${TEST_RUN_ID:-test-$(date -u '+%Y%m%dT%H%M%SZ')-$$}"
FAIL_FAST=0
KEEP_DATA=0
QUIET=0
VERBOSE=0
REQUESTED_CASES=()

CASE_IDS=()
CASE_NAMES=()
CASE_STATUSES=()
CASE_DURATIONS=()
CASE_MESSAGES=()
PASSED=0
FAILED=0
SKIPPED=0
TIMED_OUT=0
FRAMEWORK_ERROR=0

WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/warehouse-test.XXXXXX")" || {
  echo "test: failed to create temporary directory" >&2
  exit 3
}

cleanup() {
  rm -rf -- "${WORK_DIR}"
}
trap cleanup EXIT INT TERM

usage() {
  cat <<'EOF'
Usage: scripts/test.sh [options]

Options:
  --suite <suite>         unit, integration, smoke, e2e, or all (default: unit)
  --timeout <seconds>     Overall test timeout (default: 600)
  --format <format>       text, json, or junit (default: text)
  --output <path>         Write the final report to a file
  --case <id>             Run only a specific case; may be repeated
  --base-url <url>        Target Warehouse base URL for smoke tests
  --environment <name>    local, ci, test, staging, or prod
  --fail-fast             Stop scheduling cases after the first failure
  --keep-data             Accepted for debugging; current smoke suite is read-only
  --quiet                 Suppress per-case text output
  --verbose               Print captured command output to stderr
  --help                  Show this help

Common environment variables:
  TEST_SUITE, TEST_TIMEOUT, TEST_FORMAT, TEST_OUTPUT, TEST_ENVIRONMENT,
  TEST_BASE_URL, TEST_RUN_ID, TEST_ENV_FILE

Configuration file:
  .env                         Loaded automatically when present
  TEST_ENV_FILE                Override the configuration file path

Smoke credentials:
  WAREHOUSE_TEST_WEBDAV_USERNAME    Account username for SMOKE-WEBDAV-001
  WAREHOUSE_TEST_WEBDAV_PASSWORD    Account password for SMOKE-WEBDAV-001
  WAREHOUSE_TEST_WEBDAV_ROOT_PATH   Logical scope path; default: /personal
  WAREHOUSE_TEST_WEBDAV_PREFIX      WebDAV HTTP prefix; default: /dav
  WAREHOUSE_TEST_WEBDAV_ACCESS_KEY  WebDAV ak_* key for SMOKE-WEBDAV-AK-001
  WAREHOUSE_TEST_WEBDAV_SECRET_KEY  WebDAV sk_* secret for SMOKE-WEBDAV-AK-001
  WAREHOUSE_TEST_WEBDAV_ACCESS_KEY_ROOT_PATH  Logical binding path for the key
  WAREHOUSE_TEST_S3_ACCESS_KEY    Optional; enables SMOKE-S3-001
  WAREHOUSE_TEST_S3_SECRET_KEY    Optional; enables SMOKE-S3-001
  WAREHOUSE_TEST_S3_BASE_URL      Default: TEST_BASE_URL
  WAREHOUSE_TEST_S3_REGION        Default: us-east-1
EOF
}

usage_error() {
  echo "test: $1" >&2
  echo "Try --help for usage." >&2
  exit 2
}

require_value() {
  if [[ $# -lt 2 || -z "${2:-}" ]]; then
    usage_error "$1 requires a value"
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --suite) require_value "$@"; SUITE="$2"; shift 2 ;;
    --timeout) require_value "$@"; TIMEOUT="$2"; shift 2 ;;
    --format) require_value "$@"; FORMAT="$2"; shift 2 ;;
    --output) require_value "$@"; OUTPUT="$2"; shift 2 ;;
    --case) require_value "$@"; REQUESTED_CASES+=("$2"); shift 2 ;;
    --base-url) require_value "$@"; BASE_URL="$2"; shift 2 ;;
    --environment) require_value "$@"; ENVIRONMENT="$2"; shift 2 ;;
    --fail-fast) FAIL_FAST=1; shift ;;
    --keep-data) KEEP_DATA=1; shift ;;
    --quiet) QUIET=1; shift ;;
    --verbose) VERBOSE=1; shift ;;
    --help|-h) usage; exit 0 ;;
    *) usage_error "unknown argument: $1" ;;
  esac
done

case "${SUITE}" in
  unit|integration|smoke|e2e|all) ;;
  *) usage_error "invalid suite: ${SUITE}" ;;
esac
case "${FORMAT}" in
  text|json|junit) ;;
  *) usage_error "invalid format: ${FORMAT}" ;;
esac
case "${ENVIRONMENT}" in
  local|ci|test|staging|prod) ;;
  *) usage_error "invalid environment: ${ENVIRONMENT}" ;;
esac
if [[ ! "${TIMEOUT}" =~ ^[1-9][0-9]*$ ]]; then
  usage_error "timeout must be a positive integer"
fi
BASE_URL="${BASE_URL%/}"

now_ms() {
  if command -v python3 >/dev/null 2>&1; then
    python3 -c 'import time; print(int(time.time() * 1000))'
  else
    echo "$(($(date +%s) * 1000))"
  fi
}

STARTED_AT="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
START_MS="$(now_ms)"
DEADLINE_MS=$((START_MS + TIMEOUT * 1000))

VERSION="unknown"
if [[ -f "${PROJECT_DIR}/VERSION" ]]; then
  VERSION="$(head -n 1 "${PROJECT_DIR}/VERSION" | tr -d '\r\n')"
elif command -v git >/dev/null 2>&1 && git -C "${PROJECT_DIR}" rev-parse --git-dir >/dev/null 2>&1; then
  VERSION="$(git -C "${PROJECT_DIR}" describe --tags --always --dirty 2>/dev/null || echo unknown)"
fi

case_selected() {
  local id="$1" requested
  if [[ ${#REQUESTED_CASES[@]} -eq 0 ]]; then
    return 0
  fi
  for requested in "${REQUESTED_CASES[@]}"; do
    [[ "${requested}" == "${id}" ]] && return 0
  done
  return 1
}

record_case() {
  local id="$1" name="$2" status="$3" duration="$4" message="$5"
  CASE_IDS+=("${id}")
  CASE_NAMES+=("${name}")
  CASE_STATUSES+=("${status}")
  CASE_DURATIONS+=("${duration}")
  CASE_MESSAGES+=("${message}")
  case "${status}" in
    pass) PASSED=$((PASSED + 1)) ;;
    fail) FAILED=$((FAILED + 1)) ;;
    skip) SKIPPED=$((SKIPPED + 1)) ;;
  esac
}

remaining_seconds() {
  local remaining=$(((DEADLINE_MS - $(now_ms) + 999) / 1000))
  (( remaining > 0 )) || remaining=0
  echo "${remaining}"
}

run_command_case() {
  local id="$1" name="$2"; shift 2
  case_selected "${id}" || return 0
  (( FAIL_FAST == 1 && FAILED > 0 )) && return 0

  local remaining start end duration log_file rc command_pid timer_pid
  remaining="$(remaining_seconds)"
  if (( remaining <= 0 )); then
    TIMED_OUT=1
    record_case "${id}" "${name}" fail 0 "overall test timeout reached"
    return 0
  fi
  start="$(now_ms)"
  log_file="${WORK_DIR}/${id}.log"
  (cd "${PROJECT_DIR}" && "$@") >"${log_file}" 2>&1 &
  command_pid=$!
  (
    sleep_pid=""
    stop_timer() {
      [[ -z "${sleep_pid}" ]] || kill "${sleep_pid}" >/dev/null 2>&1 || true
    }
    trap stop_timer TERM INT EXIT
    sleep "${remaining}" &
    sleep_pid=$!
    wait "${sleep_pid}" || exit 0
    sleep_pid=""
    kill -TERM "${command_pid}" >/dev/null 2>&1 || true
  ) </dev/null >/dev/null 2>&1 &
  timer_pid=$!
  set +e
  wait "${command_pid}"
  rc=$?
  set -e
  kill "${timer_pid}" >/dev/null 2>&1 || true
  wait "${timer_pid}" >/dev/null 2>&1 || true
  end="$(now_ms)"
  duration=$((end - start))
  if (( VERBOSE == 1 )); then
    sed "s/^/[${id}] /" "${log_file}" >&2
  fi
  if [[ ${rc} -eq 0 ]]; then
    record_case "${id}" "${name}" pass "${duration}" "command completed successfully"
  elif [[ ${rc} -eq 143 || $(now_ms) -ge ${DEADLINE_MS} ]]; then
    TIMED_OUT=1
    record_case "${id}" "${name}" fail "${duration}" "case exceeded overall timeout"
  else
    local summary
    summary="$(grep -E '^\[FAIL\]' "${log_file}" | head -n 1 | tr -d '\r' | cut -c1-300 || true)"
    [[ -n "${summary}" ]] || summary="$(tail -n 1 "${log_file}" | tr -d '\r' | cut -c1-300)"
    [[ -n "${summary}" ]] || summary="command exited with status ${rc}"
    record_case "${id}" "${name}" fail "${duration}" "${summary}"
  fi
}

require_command() {
  local command="$1"
  if ! command -v "${command}" >/dev/null 2>&1; then
    echo "test: required command not found: ${command}" >&2
    exit 3
  fi
}

curl_config_escape() {
  local value="$1"
  value=${value//\\/\\\\}
  value=${value//\"/\\\"}
  printf '%s' "${value}"
}

webdav_request_path() {
  local root_path="$1"
  local prefix="${WAREHOUSE_TEST_WEBDAV_PREFIX:-/dav}"
  prefix="/$(printf '%s' "${prefix}" | sed -E 's#^/+##; s#/+$##; s#/{2,}#/#g')"
  [[ "${prefix}" != "/" ]] || prefix=""
  root_path="/$(printf '%s' "${root_path}" | sed -E 's#^/+##; s#/+$##; s#/{2,}#/#g')"

  # Backward compatibility: old *_PATH values included the /dav prefix.
  if [[ -n "${prefix}" && ( "${root_path}" == "${prefix}" || "${root_path}" == "${prefix}/"* ) ]]; then
    root_path="${root_path#${prefix}}"
    [[ -n "${root_path}" ]] || root_path="/"
  fi
  if [[ "${root_path}" == "/" ]]; then
    printf '%s/\n' "${prefix}"
  else
    printf '%s%s/\n' "${prefix}" "${root_path}"
  fi
}

run_smoke_health() {
  local id="SMOKE-HEALTH-001" name="production readiness check"
  case_selected "${id}" || return 0
  (( FAIL_FAST == 1 && FAILED > 0 )) && return 0
  run_command_case "${id}" "${name}" "${SCRIPT_DIR}/health-check.sh" --level readiness --base-url "${BASE_URL}" --timeout 10
}

run_webdav_propfind_case() {
  local id="$1" name="$2" username="$3" password="$4" path="$5" missing_message="$6"
  case_selected "${id}" || return 0
  (( FAIL_FAST == 1 && FAILED > 0 )) && return 0
  if [[ -z "${username}" && -z "${password}" ]]; then
    record_case "${id}" "${name}" skip 0 "${missing_message}"
    return 0
  fi
  if [[ -z "${username}" || -z "${password}" ]]; then
    record_case "${id}" "${name}" fail 0 "credential identifier and secret must both be configured"
    return 0
  fi
  require_command curl
  local start end duration config_file body_file headers_file rc status
  start="$(now_ms)"
  config_file="${WORK_DIR}/${id}.curl.conf"
  body_file="${WORK_DIR}/${id}.body"
  headers_file="${WORK_DIR}/${id}.headers"
  printf 'user = "%s:%s"\n' "$(curl_config_escape "${username}")" "$(curl_config_escape "${password}")" >"${config_file}"
  chmod 600 "${config_file}"
  set +e
  status="$(curl --silent --show-error --config "${config_file}" --request PROPFIND \
    --header 'Depth: 0' --header 'Content-Type: application/xml' \
    --connect-timeout 10 --max-time "$(remaining_seconds)" \
    --dump-header "${headers_file}" --output "${body_file}" --write-out '%{http_code}' \
    "${BASE_URL}${path}" 2>"${WORK_DIR}/${id}.error")"
  rc=$?
  set -e
  end="$(now_ms)"; duration=$((end - start))
  if [[ ${rc} -eq 28 ]]; then
    TIMED_OUT=1
    record_case "${id}" "${name}" fail "${duration}" "WebDAV PROPFIND timed out"
  elif [[ ${rc} -ne 0 ]]; then
    record_case "${id}" "${name}" fail "${duration}" "WebDAV PROPFIND failed (curl exit ${rc})"
  elif [[ "${status}" != "207" ]]; then
    record_case "${id}" "${name}" fail "${duration}" "WebDAV PROPFIND returned HTTP ${status}"
  elif ! grep -Eiq '<[^>]*multistatus' "${body_file}"; then
    record_case "${id}" "${name}" fail "${duration}" "WebDAV response did not contain multistatus XML"
  else
    record_case "${id}" "${name}" pass "${duration}" "authenticated PROPFIND returned HTTP 207 and multistatus XML"
  fi
}

run_smoke_webdav_basic() {
  local root_path="${WAREHOUSE_TEST_WEBDAV_ROOT_PATH:-${WAREHOUSE_TEST_WEBDAV_PATH:-/personal}}"
  run_webdav_propfind_case \
    "SMOKE-WEBDAV-001" \
    "WebDAV directory listing with account password" \
    "${WAREHOUSE_TEST_WEBDAV_USERNAME:-}" \
    "${WAREHOUSE_TEST_WEBDAV_PASSWORD:-}" \
    "$(webdav_request_path "${root_path}")" \
    "WebDAV account username/password are not configured"
}

run_smoke_webdav_access_key() {
  local root_path="${WAREHOUSE_TEST_WEBDAV_ACCESS_KEY_ROOT_PATH:-${WAREHOUSE_TEST_WEBDAV_ACCESS_KEY_PATH:-${WAREHOUSE_TEST_WEBDAV_ROOT_PATH:-${WAREHOUSE_TEST_WEBDAV_PATH:-/personal}}}}"
  run_webdav_propfind_case \
    "SMOKE-WEBDAV-AK-001" \
    "WebDAV directory listing with access key" \
    "${WAREHOUSE_TEST_WEBDAV_ACCESS_KEY:-}" \
    "${WAREHOUSE_TEST_WEBDAV_SECRET_KEY:-}" \
    "$(webdav_request_path "${root_path}")" \
    "WebDAV access key/secret are not configured"
}

run_smoke_s3() {
  local id="SMOKE-S3-001" name="authenticated S3 bucket listing"
  case_selected "${id}" || return 0
  (( FAIL_FAST == 1 && FAILED > 0 )) && return 0
  local access_key="${WAREHOUSE_TEST_S3_ACCESS_KEY:-}"
  local secret_key="${WAREHOUSE_TEST_S3_SECRET_KEY:-}"
  if [[ -z "${access_key}" && -z "${secret_key}" ]]; then
    record_case "${id}" "${name}" skip 0 "S3 smoke credentials are not configured"
    return 0
  fi
  if [[ -z "${access_key}" || -z "${secret_key}" ]]; then
    record_case "${id}" "${name}" fail 0 "both S3 access key and secret key are required"
    return 0
  fi
  require_command curl
  if ! curl --help all 2>/dev/null | grep -q -- '--aws-sigv4'; then
    record_case "${id}" "${name}" skip 0 "installed curl does not support --aws-sigv4"
    return 0
  fi
  local region="${WAREHOUSE_TEST_S3_REGION:-us-east-1}"
  local s3_base="${WAREHOUSE_TEST_S3_BASE_URL:-${BASE_URL}}"
  local start end duration config_file body_file rc status
  start="$(now_ms)"
  config_file="${WORK_DIR}/s3.curl.conf"
  body_file="${WORK_DIR}/s3.body"
  printf 'user = "%s:%s"\n' "$(curl_config_escape "${access_key}")" "$(curl_config_escape "${secret_key}")" >"${config_file}"
  chmod 600 "${config_file}"
  set +e
  status="$(curl --silent --show-error --config "${config_file}" \
    --aws-sigv4 "aws:amz:${region}:s3" --connect-timeout 10 --max-time "$(remaining_seconds)" \
    --output "${body_file}" --write-out '%{http_code}' "${s3_base%/}/" 2>"${WORK_DIR}/s3.error")"
  rc=$?
  set -e
  end="$(now_ms)"; duration=$((end - start))
  if [[ ${rc} -eq 28 ]]; then
    TIMED_OUT=1
    record_case "${id}" "${name}" fail "${duration}" "S3 ListBuckets timed out"
  elif [[ ${rc} -ne 0 ]]; then
    record_case "${id}" "${name}" fail "${duration}" "S3 ListBuckets failed (curl exit ${rc})"
  elif [[ ! "${status}" =~ ^2[0-9][0-9]$ ]]; then
    record_case "${id}" "${name}" fail "${duration}" "S3 ListBuckets returned HTTP ${status}"
  elif ! grep -Eiq '<[^>]*ListAllMyBucketsResult' "${body_file}"; then
    record_case "${id}" "${name}" fail "${duration}" "S3 response did not contain ListAllMyBucketsResult XML"
  else
    record_case "${id}" "${name}" pass "${duration}" "signed ListBuckets returned a valid S3 XML response"
  fi
}

run_unit_suite() {
  require_command go
  require_command npm
  run_command_case "UNIT-GO-001" "Go unit and package tests" go test ./...
  run_command_case "UNIT-WEB-001" "frontend unit tests" bash -c 'cd web && npm run test:run -- --passWithNoTests'
  run_command_case "UNIT-WEB-002" "frontend type check and production build" bash -c 'cd web && npm run build'
}

run_integration_suite() {
  require_command go
  run_command_case "INT-GO-001" "service, infrastructure, HTTP, and S3 integration tests" \
    go test ./internal/application/service/... ./internal/infrastructure/... ./internal/interface/...
}

run_smoke_suite() {
  require_command curl
  run_smoke_health
  run_smoke_webdav_basic
  run_smoke_webdav_access_key
  run_smoke_s3
}

case "${SUITE}" in
  unit) run_unit_suite ;;
  integration) run_integration_suite ;;
  smoke) run_smoke_suite ;;
  e2e) usage_error "e2e suite is not implemented; use a dedicated staging workflow" ;;
  all)
    run_unit_suite
    run_integration_suite
    run_smoke_suite
    ;;
esac

if [[ ${#REQUESTED_CASES[@]} -gt 0 ]]; then
  for requested in "${REQUESTED_CASES[@]}"; do
    found=0
    for id in "${CASE_IDS[@]}"; do
      [[ "${id}" == "${requested}" ]] && found=1
    done
    [[ ${found} -eq 1 ]] || usage_error "case is not available in suite ${SUITE}: ${requested}"
  done
fi

if [[ ${#CASE_IDS[@]} -eq 0 ]]; then
  usage_error "no test cases were executed"
fi

END_MS="$(now_ms)"
DURATION_MS=$((END_MS - START_MS))
RESULT_STATUS="pass"
(( FAILED > 0 )) && RESULT_STATUS="fail"

json_escape() {
  local value="$1"
  value=${value//\\/\\\\}; value=${value//\"/\\\"}
  value=${value//$'\n'/\\n}; value=${value//$'\r'/\\r}; value=${value//$'\t'/\\t}
  printf '%s' "${value}"
}

xml_escape() {
  printf '%s' "$1" | sed \
    -e 's/&/\&amp;/g' \
    -e 's/</\&lt;/g' \
    -e 's/>/\&gt;/g' \
    -e 's/"/\&quot;/g' \
    -e "s/'/\&apos;/g"
}

seconds_from_ms() {
  awk -v ms="$1" 'BEGIN { printf "%.3f", ms / 1000 }'
}

REPORT_FILE="${WORK_DIR}/report"
if [[ "${FORMAT}" == "json" ]]; then
  {
    printf '{"schema_version":"1.0","type":"automated_test","project":"warehouse",'
    printf '"version":"%s","environment":"%s","suite":"%s","run_id":"%s",' \
      "$(json_escape "${VERSION}")" "$(json_escape "${ENVIRONMENT}")" "${SUITE}" "$(json_escape "${RUN_ID}")"
    printf '"status":"%s","started_at":"%s","duration_ms":%s,' "${RESULT_STATUS}" "${STARTED_AT}" "${DURATION_MS}"
    printf '"summary":{"total":%s,"passed":%s,"failed":%s,"skipped":%s},"cases":[' \
      "${#CASE_IDS[@]}" "${PASSED}" "${FAILED}" "${SKIPPED}"
    for ((i=0; i<${#CASE_IDS[@]}; i++)); do
      if (( i > 0 )); then printf ','; fi
      printf '{"id":"%s","name":"%s","status":"%s","duration_ms":%s,"message":"%s"}' \
        "$(json_escape "${CASE_IDS[$i]}")" "$(json_escape "${CASE_NAMES[$i]}")" "${CASE_STATUSES[$i]}" \
        "${CASE_DURATIONS[$i]}" "$(json_escape "${CASE_MESSAGES[$i]}")"
    done
    printf ']}\n'
  } >"${REPORT_FILE}"
elif [[ "${FORMAT}" == "junit" ]]; then
  {
    printf '<?xml version="1.0" encoding="UTF-8"?>\n'
    printf '<testsuite name="warehouse-%s" tests="%s" failures="%s" skipped="%s" time="%s">\n' \
      "$(xml_escape "${SUITE}")" "${#CASE_IDS[@]}" "${FAILED}" "${SKIPPED}" "$(seconds_from_ms "${DURATION_MS}")"
    for ((i=0; i<${#CASE_IDS[@]}; i++)); do
      printf '  <testcase classname="warehouse.%s" name="%s %s" time="%s">' \
        "$(xml_escape "${SUITE}")" "$(xml_escape "${CASE_IDS[$i]}")" "$(xml_escape "${CASE_NAMES[$i]}")" \
        "$(seconds_from_ms "${CASE_DURATIONS[$i]}")"
      if [[ "${CASE_STATUSES[$i]}" == "fail" ]]; then
        printf '<failure message="%s" />' "$(xml_escape "${CASE_MESSAGES[$i]}")"
      elif [[ "${CASE_STATUSES[$i]}" == "skip" ]]; then
        printf '<skipped message="%s" />' "$(xml_escape "${CASE_MESSAGES[$i]}")"
      fi
      printf '</testcase>\n'
    done
    printf '</testsuite>\n'
  } >"${REPORT_FILE}"
else
  {
    if (( QUIET == 0 )); then
      for ((i=0; i<${#CASE_IDS[@]}; i++)); do
        printf '[%s] %s %s: %s (%s ms)\n' \
          "$(printf '%s' "${CASE_STATUSES[$i]}" | tr '[:lower:]' '[:upper:]')" \
          "${CASE_IDS[$i]}" "${CASE_NAMES[$i]}" "${CASE_MESSAGES[$i]}" "${CASE_DURATIONS[$i]}"
      done
    fi
    printf 'RESULT status=%s suite=%s run_id=%s passed=%s failed=%s skipped=%s duration_ms=%s\n' \
      "${RESULT_STATUS}" "${SUITE}" "${RUN_ID}" "${PASSED}" "${FAILED}" "${SKIPPED}" "${DURATION_MS}"
  } >"${REPORT_FILE}"
fi

if [[ -n "${OUTPUT}" ]]; then
  if [[ "${OUTPUT}" != /* ]]; then OUTPUT="${PROJECT_DIR}/${OUTPUT}"; fi
  mkdir -p "$(dirname "${OUTPUT}")"
  cp "${REPORT_FILE}" "${OUTPUT}"
else
  cat "${REPORT_FILE}"
fi

if (( TIMED_OUT == 1 )); then exit 4; fi
if (( FRAMEWORK_ERROR == 1 )); then exit 3; fi
if (( FAILED > 0 )); then exit 1; fi
exit 0
