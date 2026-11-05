#!/usr/bin/env bash
set -euo pipefail

PACKAGE_ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PACKAGE_REPOSITORY=""
PACKAGE_REF=""
PACKAGE_VERSION=""
PACKAGE_OUTPUT_DIR=""
PACKAGE_COMMIT=""
PACKAGE_SHORT_COMMIT=""
PACKAGE_STAGE=""
PACKAGE_ARTIFACTS=""
PACKAGE_SHA_COMMAND=""

package_fail() { printf 'package: %s\n' "$*" >&2; exit 1; }

package_common_init() {
  PACKAGE_REPOSITORY="$1"
  shift
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --ref|--version|--output-dir)
        [[ $# -ge 2 && -n "$2" ]] || package_fail "$1 requires a value"
        case "$1" in
          --ref) PACKAGE_REF="$2" ;;
          --version) PACKAGE_VERSION="$2" ;;
          --output-dir) PACKAGE_OUTPUT_DIR="$2" ;;
        esac
        shift 2
        ;;
      -h|--help)
        printf 'Usage: scripts/package.sh --ref COMMIT --version MAJOR.MINOR.PATCH --output-dir DIR\n'
        exit 0
        ;;
      *) package_fail "unsupported argument: $1" ;;
    esac
  done
  [[ -n "$PACKAGE_REF" && -n "$PACKAGE_VERSION" && -n "$PACKAGE_OUTPUT_DIR" ]] || package_fail 'all of --ref, --version, and --output-dir are required'
  [[ "$PACKAGE_VERSION" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || package_fail "version must be MAJOR.MINOR.PATCH: $PACKAGE_VERSION"
  command -v git >/dev/null 2>&1 || package_fail 'git is required'
  command -v jq >/dev/null 2>&1 || package_fail 'jq is required'
  command -v tar >/dev/null 2>&1 || package_fail 'tar is required'
  if command -v sha256sum >/dev/null 2>&1; then PACKAGE_SHA_COMMAND=sha256sum
  elif command -v shasum >/dev/null 2>&1; then PACKAGE_SHA_COMMAND=shasum
  else package_fail 'sha256sum or shasum is required'
  fi
  PACKAGE_COMMIT="$(git -C "$PACKAGE_ROOT_DIR" rev-parse --verify "${PACKAGE_REF}^{commit}")" || package_fail "ref is not a commit: $PACKAGE_REF"
  [[ "$(git -C "$PACKAGE_ROOT_DIR" rev-parse HEAD)" == "$PACKAGE_COMMIT" ]] || package_fail 'source checkout does not match --ref'
  [[ -z "$(git -C "$PACKAGE_ROOT_DIR" status --porcelain --untracked-files=all)" ]] || package_fail 'source checkout must be clean'
  PACKAGE_SHORT_COMMIT="${PACKAGE_COMMIT:0:7}"
  mkdir -p "$PACKAGE_OUTPUT_DIR"
  PACKAGE_OUTPUT_DIR="$(cd "$PACKAGE_OUTPUT_DIR" && pwd -P)"
  [[ -z "$(find "$PACKAGE_OUTPUT_DIR" -mindepth 1 -maxdepth 1 -print -quit)" ]] || package_fail 'output directory must be empty; refusing to overwrite files'
  PACKAGE_STAGE="$(mktemp -d "${TMPDIR:-/tmp}/${PACKAGE_REPOSITORY}-package.XXXXXX")"
  PACKAGE_ARTIFACTS="$PACKAGE_STAGE/artifacts.jsonl"
  : > "$PACKAGE_ARTIFACTS"
  trap 'rm -rf "$PACKAGE_STAGE"' EXIT HUP INT TERM
}

package_make_root() {
  local unit="$1"
  [[ "$unit" =~ ^[a-z0-9]+(-[a-z0-9]+)*$ ]] || package_fail "invalid unit id: $unit"
  PACKAGE_UNIT="$unit"
  PACKAGE_NAME="${unit}-v${PACKAGE_VERSION}-${PACKAGE_SHORT_COMMIT}.tar.gz"
  PACKAGE_ROOT_NAME="${PACKAGE_NAME%.tar.gz}"
  PACKAGE_DIR="$PACKAGE_STAGE/$PACKAGE_ROOT_NAME"
  mkdir -p "$PACKAGE_DIR"
}

package_archive_source() {
  local destination="$1"
  mkdir -p "$destination"
  git -C "$PACKAGE_ROOT_DIR" archive "$PACKAGE_COMMIT" | tar -xf - -C "$destination"
}

package_finish_artifact() {
  local type="$1"
  local artifact_json checksum
  [[ "$type" == service || "$type" == service-with-assets || "$type" == static || "$type" == desktop ]] || package_fail "invalid artifact type: $type"
  [[ -d "$PACKAGE_DIR" ]] || package_fail "package directory is missing: $PACKAGE_DIR"
  find "$PACKAGE_DIR" -type d -name __pycache__ -prune -exec rm -rf {} +
  find "$PACKAGE_DIR" -type f -name '*.pyc' -delete
  if [[ "$type" == service || "$type" == service-with-assets ]]; then
    for path in scripts/starter.sh scripts/health-check.sh scripts/test.sh; do
      [[ -f "$PACKAGE_DIR/$path" ]] || package_fail "$PACKAGE_UNIT is missing $path"
    done
  fi
  tar -czf "$PACKAGE_STAGE/$PACKAGE_NAME" -C "$PACKAGE_STAGE" "$PACKAGE_ROOT_NAME"
  if [[ "$PACKAGE_SHA_COMMAND" == sha256sum ]]; then checksum="$(sha256sum "$PACKAGE_STAGE/$PACKAGE_NAME" | awk '{print $1}')"
  else checksum="$(shasum -a 256 "$PACKAGE_STAGE/$PACKAGE_NAME" | awk '{print $1}')"
  fi
  if [[ "$type" == static ]]; then
    artifact_json="$(jq -cnS --arg unit "$PACKAGE_UNIT" --arg version "$PACKAGE_VERSION" --arg package "$PACKAGE_NAME" --arg sha "$checksum" --arg root "$PACKAGE_ROOT_NAME" --arg site "$PACKAGE_UNIT" --arg health "$PACKAGE_HEALTH_PATH" '{unit_id:$unit,type:"static",version:$version,package:$package,sha256:$sha,package_root:$root,static_targets:[{site_id:$site,path:("/opt/deploy/static/"+$site),health_check_path:$health}]}')"
  else
    artifact_json="$(jq -cnS --arg unit "$PACKAGE_UNIT" --arg type "$type" --arg version "$PACKAGE_VERSION" --arg package "$PACKAGE_NAME" --arg sha "$checksum" --arg root "$PACKAGE_ROOT_NAME" '{unit_id:$unit,type:$type,version:$version,package:$package,sha256:$sha,package_root:$root}')"
  fi
  printf '%s\n' "$artifact_json" >> "$PACKAGE_ARTIFACTS"
}

package_finish() {
  [[ -s "$PACKAGE_ARTIFACTS" ]] || package_fail 'no artifacts were created'
  jq -cnS --arg repository "$PACKAGE_REPOSITORY" --arg commit "$PACKAGE_COMMIT" --slurpfile artifacts "$PACKAGE_ARTIFACTS" '{schema_version:1,repository:$repository,source_commit:$commit,artifacts:$artifacts}' > "$PACKAGE_STAGE/release-manifest.json"
  for file in "$PACKAGE_STAGE"/*.tar.gz "$PACKAGE_STAGE/release-manifest.json"; do
    [[ -f "$file" ]] || continue
    [[ ! -e "$PACKAGE_OUTPUT_DIR/$(basename "$file")" ]] || package_fail "refusing to overwrite: $(basename "$file")"
  done
  for file in "$PACKAGE_STAGE"/*.tar.gz "$PACKAGE_STAGE/release-manifest.json"; do
    [[ -f "$file" ]] || continue
    mv "$file" "$PACKAGE_OUTPUT_DIR/"
  done
  printf 'Created %s artifact(s) for %s at %s\n' "$(jq -r '.artifacts | length' "$PACKAGE_OUTPUT_DIR/release-manifest.json")" "$PACKAGE_REPOSITORY" "$PACKAGE_OUTPUT_DIR"
}

package_copy_scripts() {
  local destination="$1"
  mkdir -p "$destination"
  cp -R "$PACKAGE_ROOT_DIR/scripts/." "$destination/"
  find "$destination" -type f -name '*.sh' -exec chmod 0755 {} +
  find "$destination" -type d -name __pycache__ -prune -exec rm -rf {} +
  find "$destination" -type f -name '*.pyc' -delete
}

package_strip_build_files() {
  local package_dir="$1"
  rm -f "$package_dir/scripts/package.sh" "$package_dir/scripts/package-common.sh" "$package_dir/scripts/package-api.json"
}
