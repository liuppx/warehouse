#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PROJECT_NAME="$(basename "${ROOT_DIR}")"
OUTPUT_DIR="${ROOT_DIR}/output"
ASSETS_DIR="${ROOT_DIR}/web/dist"
REQUESTED_TAG="${1:-}"
TARGET_GOOS="${TARGET_GOOS:-linux}"
TARGET_GOARCH="${TARGET_GOARCH:-amd64}"
TAG_PATTERN='^v[0-9]+\.[0-9]+\.[0-9]+$'
MAIN_BRANCH="main"
ORIGINAL_REF="$(git -C "${ROOT_DIR}" symbolic-ref --quiet --short HEAD || git -C "${ROOT_DIR}" rev-parse --verify HEAD)"
SWITCHED_REF=0
TARGET_TAG=""

cleanup() {
  if [[ "${SWITCHED_REF}" -eq 1 ]]; then
    echo "Restoring checkout: ${ORIGINAL_REF}"
    git -C "${ROOT_DIR}" checkout -q "${ORIGINAL_REF}"
  fi
}
trap cleanup EXIT

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "$1 is required" >&2
    exit 1
  fi
}

validate_tag() {
  local tag="$1"
  if [[ ! "${tag}" =~ ${TAG_PATTERN} ]]; then
    echo "Invalid tag format: ${tag}. Expected v<major>.<minor>.<patch>" >&2
    exit 1
  fi
}

ensure_clean_worktree_for_checkout() {
  local target="$1"
  local current
  current="$(git -C "${ROOT_DIR}" rev-parse --verify HEAD)"
  local target_commit
  target_commit="$(git -C "${ROOT_DIR}" rev-list -n 1 "${target}")"
  if [[ "${current}" == "${target_commit}" ]]; then
    return
  fi
  if ! git -C "${ROOT_DIR}" diff --quiet || ! git -C "${ROOT_DIR}" diff --cached --quiet; then
    echo "Working tree is not clean. Commit/stash changes before switching to ${target}." >&2
    exit 1
  fi
}

latest_semver_tag() {
  git -C "${ROOT_DIR}" tag --list 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname | head -n1
}

next_patch_tag() {
  local latest="$1"
  if [[ -z "${latest}" ]]; then
    echo "v0.0.1"
    return
  fi
  local version="${latest#v}"
  local major minor patch
  IFS='.' read -r major minor patch <<<"${version}"
  echo "v${major}.${minor}.$((patch + 1))"
}

prepare_target_tag() {
  git -C "${ROOT_DIR}" fetch --tags --quiet

  if [[ -n "${REQUESTED_TAG}" ]]; then
    validate_tag "${REQUESTED_TAG}"
    if ! git -C "${ROOT_DIR}" rev-parse -q --verify "refs/tags/${REQUESTED_TAG}" >/dev/null; then
      echo "Tag does not exist, skip packaging: ${REQUESTED_TAG}" >&2
      return 2
    fi
    TARGET_TAG="${REQUESTED_TAG}"
    return 0
  fi

  if ! git -C "${ROOT_DIR}" rev-parse -q --verify "refs/heads/${MAIN_BRANCH}" >/dev/null; then
    echo "Branch not found: ${MAIN_BRANCH}" >&2
    exit 1
  fi

  local latest_tag
  latest_tag="$(latest_semver_tag)"
  local main_hash
  main_hash="$(git -C "${ROOT_DIR}" rev-parse --verify "${MAIN_BRANCH}")"

  if [[ -n "${latest_tag}" ]]; then
    local latest_hash
    latest_hash="$(git -C "${ROOT_DIR}" rev-list -n 1 "${latest_tag}")"
    if [[ "${latest_hash}" == "${main_hash}" ]]; then
      local main_short expected_archive
      main_short="$(git -C "${ROOT_DIR}" rev-parse --short=7 "${main_hash}")"
      expected_archive="${OUTPUT_DIR}/${PROJECT_NAME}-${latest_tag}-${main_short}.tar.gz"
      if [[ -f "${expected_archive}" ]]; then
        echo "Latest tag ${latest_tag} already points to ${MAIN_BRANCH} HEAD and package exists, skip packaging." >&2
        return 2
      fi

      echo "Latest tag ${latest_tag} already points to ${MAIN_BRANCH} HEAD, but package is missing. Rebuild package with existing tag." >&2
      TARGET_TAG="${latest_tag}"
      return 0
    fi
  fi

  TARGET_TAG="$(next_patch_tag "${latest_tag}")"
  validate_tag "${TARGET_TAG}"
  echo "Creating tag ${TARGET_TAG} on ${MAIN_BRANCH}@${main_hash}" >&2
  git -C "${ROOT_DIR}" tag "${TARGET_TAG}" "${main_hash}"
  echo "Pushing tag ${TARGET_TAG} to origin" >&2
  git -C "${ROOT_DIR}" push origin "${TARGET_TAG}"
  return 0
}

switch_to_tag() {
  local tag="$1"
  ensure_clean_worktree_for_checkout "${tag}"
  local current
  current="$(git -C "${ROOT_DIR}" rev-parse --verify HEAD)"
  local target
  target="$(git -C "${ROOT_DIR}" rev-list -n 1 "${tag}")"
  if [[ "${current}" != "${target}" ]]; then
    git -C "${ROOT_DIR}" checkout -q "${tag}"
    SWITCHED_REF=1
  fi
}

build_artifacts() {
  require_cmd npm
  require_cmd go
  local web_dir="${ROOT_DIR}/web"
  echo "Building frontend assets..."
  if [[ ! -x "${web_dir}/node_modules/.bin/vue-tsc" || ! -x "${web_dir}/node_modules/.bin/vite" ]]; then
    echo "Installing frontend dependencies (including dev dependencies)..."
    if ! (cd "${web_dir}" && env -u NODE_ENV npm install --include=dev); then
      echo "npm install --include=dev failed, retrying with npm install..." >&2
      (cd "${web_dir}" && env -u NODE_ENV npm install)
    fi
  fi
  echo "Refreshing frontend dependencies..."
  (cd "${web_dir}" && env -u NODE_ENV npm install)
  (cd "${web_dir}" && npm run build)

  echo "Building backend binary for ${TARGET_GOOS}/${TARGET_GOARCH}..."
  local version build_time git_commit ldflags
  version="$(git -C "${ROOT_DIR}" describe --tags --always --dirty)"
  build_time="$(date -u '+%Y-%m-%d_%H:%M:%S')"
  git_commit="$(git -C "${ROOT_DIR}" rev-parse --short HEAD)"
  ldflags="-X main.version=${version} -X main.buildTime=${build_time} -X main.gitCommit=${git_commit}"
  mkdir -p "${ROOT_DIR}/build"
  (cd "${ROOT_DIR}" && GOOS="${TARGET_GOOS}" GOARCH="${TARGET_GOARCH}" go build -ldflags "${ldflags}" -o build/warehouse ./cmd/warehouse)

  if [[ ! -x "${ROOT_DIR}/build/warehouse" ]]; then
    echo "warehouse binary not found: ${ROOT_DIR}/build/warehouse" >&2
    exit 1
  fi
  if [[ ! -d "${ASSETS_DIR}" ]]; then
    echo "frontend assets not found: ${ASSETS_DIR}" >&2
    exit 1
  fi
  if [[ ! -f "${ROOT_DIR}/scripts/starter.sh" ]]; then
    echo "starter script not found: ${ROOT_DIR}/scripts/starter.sh" >&2
    exit 1
  fi
  if [[ ! -x "${ROOT_DIR}/scripts/health-check.sh" ]]; then
    echo "health check script not found or not executable: ${ROOT_DIR}/scripts/health-check.sh" >&2
    exit 1
  fi
  if [[ ! -x "${ROOT_DIR}/scripts/test.sh" ]]; then
    echo "automated test script not found or not executable: ${ROOT_DIR}/scripts/test.sh" >&2
    exit 1
  fi
  if [[ ! -x "${ROOT_DIR}/scripts/copy-for-upgrade.sh" ]]; then
    echo "copy-for-upgrade script not found or not executable: ${ROOT_DIR}/scripts/copy-for-upgrade.sh" >&2
    exit 1
  fi
  if [[ ! -f "${ROOT_DIR}/scripts/backup.conf.template" ]]; then
    echo "backup config template not found: ${ROOT_DIR}/scripts/backup.conf.template" >&2
    exit 1
  fi
  if [[ ! -x "${ROOT_DIR}/scripts/config_backup.sh" ]]; then
    echo "config backup script not found or not executable: ${ROOT_DIR}/scripts/config_backup.sh" >&2
    exit 1
  fi
  if [[ ! -f "${ROOT_DIR}/scripts/.passphrase-file.template" ]]; then
    echo "passphrase file template not found: ${ROOT_DIR}/scripts/.passphrase-file.template" >&2
    exit 1
  fi
}

create_package() {
  local tag="$1"
  local git_hash
  git_hash="$(git -C "${ROOT_DIR}" rev-parse --short=7 HEAD)"
  mkdir -p "${OUTPUT_DIR}"
  echo "Cleaning output directory: ${OUTPUT_DIR}"
  find "${OUTPUT_DIR}" -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +

  local package_name="${PROJECT_NAME}-${tag}-${git_hash}"
  local staging_dir="${OUTPUT_DIR}/${package_name}"

  echo "Packaging ${package_name}.tar.gz"
  rm -rf "${staging_dir}"
  mkdir -p "${staging_dir}/bin" "${staging_dir}/scripts" "${staging_dir}/web"

  cp "${ROOT_DIR}/build/warehouse" "${staging_dir}/bin/"
  cp "${ROOT_DIR}/config.yaml.template" "${staging_dir}/"
  cp "${ROOT_DIR}/scripts/starter.sh" "${staging_dir}/scripts/"
  cp "${ROOT_DIR}/scripts/health-check.sh" "${staging_dir}/scripts/"
  cp "${ROOT_DIR}/scripts/test.sh" "${staging_dir}/scripts/"
  cp "${ROOT_DIR}/scripts/copy-for-upgrade.sh" "${staging_dir}/scripts/"
  cp "${ROOT_DIR}/scripts/backup.conf.template" "${staging_dir}/scripts/"
  cp "${ROOT_DIR}/scripts/config_backup.sh" "${staging_dir}/scripts/"
  cp "${ROOT_DIR}/scripts/.passphrase-file.template" "${staging_dir}/scripts/"
  cp "${ROOT_DIR}/.env.template" "${staging_dir}/"
  if [[ -d "${ROOT_DIR}/resources" ]]; then
    cp -R "${ROOT_DIR}/resources" "${staging_dir}/"
  fi
  cp -R "${ASSETS_DIR}" "${staging_dir}/web/"

  local archive_path="${OUTPUT_DIR}/${package_name}.tar.gz"
  if [[ "$(uname -s)" == "Darwin" ]]; then
    # Prevent Apple metadata (._* and xattr headers) from leaking into release tarballs.
    if command -v xattr >/dev/null 2>&1; then
      xattr -rc "${staging_dir}" || true
    fi
    find "${staging_dir}" -name '._*' -delete || true

    if command -v gtar >/dev/null 2>&1; then
      COPYFILE_DISABLE=1 COPY_EXTENDED_ATTRIBUTES_DISABLE=1 \
        gtar --format=ustar --no-xattrs --no-acls \
        -C "${OUTPUT_DIR}" -czf "${archive_path}" "${package_name}"
    else
      local tar_cmd=(tar --format=ustar)
      if tar --help 2>&1 | grep -q -- '--no-mac-metadata'; then
        tar_cmd+=(--no-mac-metadata)
      fi
      if tar --help 2>&1 | grep -q -- '--no-xattrs'; then
        tar_cmd+=(--no-xattrs)
      fi
      if tar --help 2>&1 | grep -q -- '--no-acls'; then
        tar_cmd+=(--no-acls)
      fi
      COPYFILE_DISABLE=1 COPY_EXTENDED_ATTRIBUTES_DISABLE=1 \
        "${tar_cmd[@]}" -C "${OUTPUT_DIR}" -czf "${archive_path}" "${package_name}"
    fi
  else
    tar -C "${OUTPUT_DIR}" -czf "${archive_path}" "${package_name}"
  fi

  if tar -tf "${archive_path}" | grep -E -q '(^|/)\._'; then
    echo "package contains AppleDouble (._*) files, aborting." >&2
    exit 1
  fi
  if command -v strings >/dev/null 2>&1 && strings "${archive_path}" | grep -q 'LIBARCHIVE.xattr'; then
    echo "package still contains LIBARCHIVE.xattr metadata, aborting." >&2
    exit 1
  fi
  rm -rf "${staging_dir}"
  echo "Package created: ${archive_path}"
}

main() {
  local rc
  if ! prepare_target_tag; then
    rc=$?
    if [[ "${rc}" -eq 2 ]]; then
      exit 0
    fi
    exit "${rc}"
  fi
  local tag="${TARGET_TAG}"
  switch_to_tag "${tag}"
  build_artifacts
  create_package "${tag}"
}

main
