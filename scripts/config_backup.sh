#!/usr/bin/env bash

set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
MODULE_DIR="$(realpath "${SCRIPT_DIR}/..")"
MODULE_BASENAME="$(basename "$MODULE_DIR")"

if [[ "$MODULE_BASENAME" =~ ^(.+)-v[^-]+-[[:alnum:]]{7}$ ]]; then
    MODULE_NAME="${BASH_REMATCH[1]}"
else
    MODULE_NAME="$MODULE_BASENAME"
fi

LOGFILE=""

init_log_file() {
    local logfile_name=$1
    local logfile_dir="/opt/logs"

    LOGFILE="${logfile_dir}/${logfile_name}"
    mkdir -p "$logfile_dir"
    touch "$LOGFILE"

    local filesize=0
    filesize=$(stat -c "%s" "$LOGFILE" 2>/dev/null || echo 0)
    if [[ "$filesize" -ge 1048576 ]]; then
        printf 'clear old logs at %s to avoid log file too big\n' "$(date)" > "$LOGFILE"
    fi
}

log() {
    echo -e "[$(date '+%Y-%m-%d %H:%M:%S')] $*" | tee -a "$LOGFILE"
}

log_err() {
    echo -e "[$(date '+%Y-%m-%d %H:%M:%S')] $*" | tee -a "$LOGFILE" >&2
}

fail() {
    log_err "$*"
    cleanup
    exit 1
}

cleanup() {
    if [[ -n "${TMP_DIR:-}" && "$TMP_DIR" == /tmp/* && -e "$TMP_DIR" ]]; then
        rm -rf "$TMP_DIR"
    fi
}

main() {
    init_log_file "config-backup-${MODULE_NAME}.log"

    local conf_file="/data/${MODULE_NAME}/backup.conf"
    local passphrase_file="/data/${MODULE_NAME}/.passphrase-file"
    local source_config="${MODULE_DIR}/config.yaml"
    local backup_dir="/opt/backup"
    local backup_file=""

    BACKUP_CONF_FLAG="True"
    BACKUP_CONF_PREFIX=""
    BACKUP_CONF_SUFFIX=".conf.tar.gz.gpg"

    if [[ ! -f "$conf_file" ]]; then
        log_err "backup config not found: $conf_file"
        exit 1
    fi

    # shellcheck source=/dev/null
    source "$conf_file"

    if [[ "$BACKUP_CONF_FLAG" != "True" && "$BACKUP_CONF_FLAG" != "False" ]]; then
        log_err "invalid BACKUP_CONF_FLAG: $BACKUP_CONF_FLAG, expected True or False"
        exit 1
    fi

    if [[ "$BACKUP_CONF_FLAG" == "False" ]]; then
        log "config backup disabled by BACKUP_CONF_FLAG=False"
        exit 0
    fi

    backup_file="${backup_dir}/${BACKUP_CONF_PREFIX}${MODULE_BASENAME}${BACKUP_CONF_SUFFIX}"

    mkdir -p "$backup_dir" || fail "failed to create backup directory: $backup_dir"

    if [[ -f "$backup_file" ]]; then
        log "backup file already exists: $backup_file"
        exit 255
    fi

    [[ -f "$source_config" ]] || fail "config file not found: $source_config"
    [[ -f "$passphrase_file" ]] || fail "passphrase file not found: $passphrase_file"

    TMP_DIR="/tmp/${MODULE_BASENAME}-conf"
    rm -rf "$TMP_DIR"
    mkdir -p "$TMP_DIR" || fail "failed to create temp directory: $TMP_DIR"

    cp "$source_config" "$TMP_DIR/config.yaml" || fail "failed to copy config.yaml to temp directory"

    local nginx_conf_file=""
    for nginx_conf_file in \
        "/etc/nginx/conf.d/test-webdav.conf" \
        "/etc/nginx/conf.d/webdav.conf" \
        "/etc/nginx/conf.d/warehouse.conf"; do
        if [[ -f "$nginx_conf_file" ]]; then
            cp "$nginx_conf_file" "$TMP_DIR/$(basename "$nginx_conf_file")" \
                || fail "failed to copy $(basename "$nginx_conf_file") to temp directory"
        fi
    done

    log "start config backup: $MODULE_DIR -> $backup_file"

    if gpg --batch --yes --symmetric --cipher-algo AES256 \
        --passphrase-file "$passphrase_file" \
        -o "$backup_file" \
        < <(tar czf - -C "/tmp" "${MODULE_BASENAME}-conf"); then
        log "config backup completed: $backup_file"
        cleanup
        exit 0
    fi

    rm -f "$backup_file"
    fail "config backup failed"
}

trap cleanup EXIT
main "$@"
