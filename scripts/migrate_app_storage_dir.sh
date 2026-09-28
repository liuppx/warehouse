#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  migrate_app_storage_dir.sh --root <webdav-root> [--old <dir>] [--new <app-id>]
  migrate_app_storage_dir.sh --root <webdav-root> [--old <dir>] [--new <app-id>] --apply

The default mode is dry-run. Use --apply only after reviewing the plan.
Only directories matching <root>/*/apps/<old> are considered.
EOF
}

ROOT=""
OLD="chat.yeying.pub"
NEW="a5f11471-dcdd-4685-b4a6-a6bde8e8ac82"
APPLY=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --root)
      [[ $# -ge 2 ]] || { usage >&2; exit 2; }
      ROOT=$2
      shift 2
      ;;
    --old)
      [[ $# -ge 2 ]] || { usage >&2; exit 2; }
      OLD=$2
      shift 2
      ;;
    --new)
      [[ $# -ge 2 ]] || { usage >&2; exit 2; }
      NEW=$2
      shift 2
      ;;
    --apply)
      APPLY=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "$ROOT" || -z "$OLD" || -z "$NEW" ]]; then
  usage >&2
  exit 2
fi

case "$ROOT" in
  */) ROOT=${ROOT%/} ;;
esac

if [[ "$OLD" == */* || "$NEW" == */* || "$OLD" == "." || "$NEW" == "." || "$OLD" == ".." || "$NEW" == ".." ]]; then
  echo "old/new must be single directory names" >&2
  exit 2
fi

[[ -d "$ROOT" ]] || { echo "root is not a directory: $ROOT" >&2; exit 2; }

plan_file=$(mktemp "${TMPDIR:-/tmp}/warehouse-app-migration.XXXXXX")
trap 'rm -f "$plan_file"' EXIT

found=0
conflicts=0

while IFS= read -r -d '' source; do
  apps_dir=$(dirname "$source")
  if [[ "$(basename "$apps_dir")" != "apps" ]]; then
    continue
  fi

  target="$apps_dir/$NEW"
  printf '%s\t%s\n' "$source" "$target" >> "$plan_file"
  found=$((found + 1))

  if [[ -e "$target" || -L "$target" ]]; then
    echo "CONFLICT target already exists: $target" >&2
    conflicts=$((conflicts + 1))
  else
    echo "PLAN $source -> $target"
  fi
done < <(find "$ROOT" -type d -path "*/apps/$OLD" -prune -print0)

if [[ "$found" -eq 0 ]]; then
  echo "No matching directories found under $ROOT"
  exit 0
fi

if [[ "$conflicts" -gt 0 ]]; then
  echo "Aborted: $conflicts target conflict(s) found; no directory was moved." >&2
  exit 3
fi

if [[ "$APPLY" -ne 1 ]]; then
  echo "Dry-run only: $found directory(ies) would be moved. Re-run with --apply to execute."
  exit 0
fi

echo "Applying migration for $found directory(ies)..."
moved=0
while IFS=$'\t' read -r source target; do
  [[ -d "$source" ]] || { echo "Source disappeared: $source" >&2; exit 4; }
  [[ ! -e "$target" && ! -L "$target" ]] || { echo "Target appeared: $target" >&2; exit 4; }
  mv -- "$source" "$target"
  echo "MOVED $source -> $target"
  moved=$((moved + 1))
done < "$plan_file"

echo "Migration complete: $moved directory(ies) moved."
