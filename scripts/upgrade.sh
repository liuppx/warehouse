#!/usr/bin/env bash

set -euo pipefail

if [[ $# -ne 3 ]]; then
    printf 'Usage: %s <prepare|copy-config|post-copy|finalize> <current-dir> <target-dir>\n' "$0" >&2
    exit 1
fi

phase=$1
current_dir=$2
target_dir=$3
webdav_flag=${WEBDAV_FLAG:-all}

case "$phase" in
    prepare)
        case "$webdav_flag" in
            all|backend)
                ;;
            frontend)
                [[ -d "${target_dir}/web/dist" ]] || {
                    printf 'Missing web dist: %s\n' "${target_dir}/web/dist" >&2
                    exit 1
                }
                exit 10
                ;;
            *)
                printf 'Invalid WEBDAV_FLAG: %s, expected all|backend|frontend\n' "$webdav_flag" >&2
                exit 1
                ;;
        esac
        ;;
    copy-config)
        [[ -f "${current_dir}/config.yaml" ]] || { printf 'Config file not found: %s/config.yaml\n' "$current_dir" >&2; exit 1; }
        [[ -d "$target_dir" ]] || { printf 'Target directory not found: %s\n' "$target_dir" >&2; exit 1; }
        cp -f "${current_dir}/config.yaml" "${target_dir}/config.yaml"
        ;;
    post-copy)
        if [[ "$webdav_flag" == "all" || "$webdav_flag" == "frontend" ]]; then
            [[ -d "${target_dir}/web/dist" ]] || {
                printf 'Missing web dist: %s\n' "${target_dir}/web/dist" >&2
                exit 1
            }
            rm -rf /usr/share/nginx/html/webdav
            cp -r "${target_dir}/web/dist" /usr/share/nginx/html/webdav
            systemctl restart nginx
        fi
        ;;
    finalize)
        ;;
    *)
        printf 'Unsupported upgrade phase: %s\n' "$phase" >&2
        exit 1
        ;;
esac
