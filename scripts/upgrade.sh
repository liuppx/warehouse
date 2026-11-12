#!/usr/bin/env bash

set -euo pipefail

if [[ $# -ne 3 ]]; then
    printf 'Usage: %s <prepare|copy-config|post-copy|finalize> <current-dir> <target-dir>\n' "$0" >&2
    exit 1
fi

phase=$1
current_dir=$2
target_dir=$3

case "$phase" in
    prepare|post-copy|finalize) ;;
    copy-config)
        [[ -f "${current_dir}/config.yaml" ]] || { printf 'Config file not found: %s/config.yaml\n' "$current_dir" >&2; exit 1; }
        [[ -d "$target_dir" ]] || { printf 'Target directory not found: %s\n' "$target_dir" >&2; exit 1; }
        cp -f "${current_dir}/config.yaml" "${target_dir}/config.yaml"
        ;;
    *)
        printf 'Unsupported upgrade phase: %s\n' "$phase" >&2
        exit 1
        ;;
esac
