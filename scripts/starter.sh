#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BINARY="${ROOT_DIR}/bin/warehouse"
CONFIG="${ROOT_DIR}/config.yaml"
PID_DIR="${ROOT_DIR}/run"
PID_FILE="${PID_DIR}/warehouse.pid"
LOG_DIR="${ROOT_DIR}/logs"
LOG_FILE="${LOG_DIR}/warehouse.log"
STOP_TIMEOUT_SECONDS="${STOP_TIMEOUT_SECONDS:-30}"
FORCE_STOP_TIMEOUT_SECONDS="${FORCE_STOP_TIMEOUT_SECONDS:-5}"

ACTION="${1:-start}"

if [[ ! -f "${CONFIG}" ]]; then
  CONFIG="${ROOT_DIR}/config.yaml.template"
fi

is_running() {
  if [[ -f "${PID_FILE}" ]]; then
    local pid
    pid=$(cat "${PID_FILE}")
    if [[ -n "${pid}" ]] && kill -0 "${pid}" >/dev/null 2>&1; then
      local state
      state="$(ps -p "${pid}" -o stat= 2>/dev/null | tr -d '[:space:]')"
      [[ -n "${state}" && "${state:0:1}" != "Z" ]] && return 0
    fi
  fi
  return 1
}

validate_stop_timeouts() {
  [[ "${STOP_TIMEOUT_SECONDS}" =~ ^[0-9]+$ ]] || { echo "Invalid STOP_TIMEOUT_SECONDS: ${STOP_TIMEOUT_SECONDS}" >&2; exit 1; }
  [[ "${FORCE_STOP_TIMEOUT_SECONDS}" =~ ^[0-9]+$ ]] || { echo "Invalid FORCE_STOP_TIMEOUT_SECONDS: ${FORCE_STOP_TIMEOUT_SECONDS}" >&2; exit 1; }
}

start() {
  if [[ ! -x "${BINARY}" ]]; then
    echo "warehouse binary not found: ${BINARY}" >&2
    exit 1
  fi
  if [[ ! -f "${CONFIG}" ]]; then
    echo "config not found: ${CONFIG}" >&2
    exit 1
  fi
  if is_running; then
    echo "warehouse already running (pid=$(cat "${PID_FILE}"))"
    return
  fi

  mkdir -p "${PID_DIR}" "${LOG_DIR}"
  nohup "${BINARY}" -c "${CONFIG}" >>"${LOG_FILE}" 2>&1 &
  echo $! > "${PID_FILE}"

  sleep 1
  if ! is_running; then
    echo "failed to start warehouse, check ${LOG_FILE}" >&2
    exit 1
  fi
  echo "warehouse started (pid=$(cat "${PID_FILE}"))"
}

stop() {
  validate_stop_timeouts
  if ! is_running; then
    echo "warehouse not running"
    rm -f "${PID_FILE}"
    return
  fi

  local pid
  pid=$(cat "${PID_FILE}")
  kill -TERM "${pid}" >/dev/null 2>&1 || true

  local waited=0
  while is_running; do
    if (( waited >= STOP_TIMEOUT_SECONDS )); then
      break
    fi
    sleep 1
    waited=$((waited + 1))
  done

  if is_running; then
    echo "warehouse graceful stop timed out after ${STOP_TIMEOUT_SECONDS}s, sending SIGKILL" >&2
    kill -KILL "${pid}" >/dev/null 2>&1 || true
    waited=0
    while is_running; do
      if (( waited >= FORCE_STOP_TIMEOUT_SECONDS )); then
        echo "warehouse process ${pid} did not exit after SIGKILL; refusing to start" >&2
        return 1
      fi
      sleep 1
      waited=$((waited + 1))
    done
  fi

  rm -f "${PID_FILE}"
  echo "warehouse stopped"
}

restart() {
  if ! stop; then
    return 1
  fi
  start
}

case "${ACTION}" in
  start)
    start
    ;;
  stop)
    stop
    ;;
  restart)
    restart
    ;;
  *)
    echo "usage: $0 [start|stop|restart]" >&2
    exit 1
    ;;
esac
