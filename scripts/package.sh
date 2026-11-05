#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/package-common.sh"
package_common_init warehouse "$@"
for command_name in go npm; do command -v "$command_name" >/dev/null 2>&1 || package_fail "$command_name is required"; done
(cd "$PACKAGE_ROOT_DIR/web" && npm install --no-audit --no-fund && npm run build)
build_dir="$PACKAGE_STAGE/build"
mkdir -p "$build_dir"
(cd "$PACKAGE_ROOT_DIR" && CGO_ENABLED="${CGO_ENABLED:-0}" GOOS="${GOOS:-linux}" GOARCH="${GOARCH:-amd64}" go build -trimpath -o "$build_dir/warehouse" ./cmd/warehouse)
package_make_root warehouse
mkdir -p "$PACKAGE_DIR/bin" "$PACKAGE_DIR/web"
install -m 0755 "$build_dir/warehouse" "$PACKAGE_DIR/bin/warehouse"
cp -R "$PACKAGE_ROOT_DIR/web/dist" "$PACKAGE_DIR/web/dist"
for file in config.yaml.template .env.template; do [[ ! -f "$PACKAGE_ROOT_DIR/$file" ]] || cp "$PACKAGE_ROOT_DIR/$file" "$PACKAGE_DIR/"; done
[[ ! -d "$PACKAGE_ROOT_DIR/resources" ]] || cp -R "$PACKAGE_ROOT_DIR/resources" "$PACKAGE_DIR/resources"
package_copy_scripts "$PACKAGE_DIR/scripts"
package_strip_build_files "$PACKAGE_DIR"
package_finish_artifact service-with-assets
package_make_root warehouse-web
PACKAGE_HEALTH_PATH=/index.html
cp -R "$PACKAGE_ROOT_DIR/web/dist/." "$PACKAGE_DIR/"
package_finish_artifact static
package_finish
