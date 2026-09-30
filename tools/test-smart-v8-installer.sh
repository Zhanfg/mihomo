#!/usr/bin/env sh
set -eu

ROOT="$(mktemp -d)"
trap 'rm -rf "$ROOT"' EXIT INT TERM

INSTALLER_SRC="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/install-smart-v8-box.sh"

make_core() {
  path="$1"
  version="$2"
  cat > "$path" <<EOF
#!/bin/sh
case "${1:-}" in
  -v|--version|version) echo "$version"; exit 0 ;;
  -t) exit 0 ;;
  *) exit 0 ;;
esac
EOF
  chmod 0755 "$path"
}

run_modern_success() {
  case_dir="$ROOT/modern"
  pkg="$case_dir/pkg"
  box="$case_dir/box"
  mkdir -p "$pkg" "$box/bin" "$box/database" "$box/run" "$box/mihomo"
  cp "$INSTALLER_SRC" "$pkg/install.sh"
  make_core "$pkg/mihomo-a64-smart-ebpf-test" "smart-v8-test"
  make_core "$box/bin/mihomo" "old-core"
  printf 'mode: rule\n' > "$box/mihomo/config.yaml"
  printf 'db-sentinel-modern\n' > "$box/database/box.db"
  db_before="$(sha256sum "$box/database/box.db" | awk '{print $1}')"

  cat > "$box/bin/boxctl" <<EOF
#!/bin/sh
printf '%s\n' "\$*" >> "$case_dir/boxctl.calls"
if [ "\$1" = "--db" ] && [ "\$3" = "restart" ]; then
  exit 0
fi
exit 1
EOF
  chmod 0755 "$box/bin/boxctl"

  SMART_INSTALL_TEST=1 \
  SMART_INSTALL_RUNNING_OVERRIDE=1 \
  SMART_INSTALL_SKIP_RUNNING_VERIFY=1 \
  BOX_DIR_OVERRIDE="$box" \
    sh "$pkg/install.sh" >"$case_dir/install.out" 2>&1

  [ "$("$box/bin/mihomo" -v)" = "smart-v8-test" ]
  [ "$("$box/bin/mihomo.smart-prev" -v)" = "old-core" ]
  grep -F -- "--db $box/database/box.db restart" "$case_dir/boxctl.calls" >/dev/null
  db_after="$(sha256sum "$box/database/box.db" | awk '{print $1}')"
  [ "$db_before" = "$db_after" ]
  [ "$(find "$box/bin" -maxdepth 1 -name 'mihomo*.smart-prev*' | wc -l)" -eq 1 ]
}

run_modern_rollback() {
  case_dir="$ROOT/rollback"
  pkg="$case_dir/pkg"
  box="$case_dir/box"
  mkdir -p "$pkg" "$box/bin" "$box/database" "$box/run" "$box/mihomo"
  cp "$INSTALLER_SRC" "$pkg/install.sh"
  make_core "$pkg/mihomo-a64-smart-ebpf-test" "broken-runtime-candidate"
  make_core "$box/bin/mihomo" "rollback-old-core"
  printf 'mode: rule\n' > "$box/mihomo/config.yaml"
  printf 'db-sentinel-rollback\n' > "$box/database/box.db"

  cat > "$box/bin/boxctl" <<EOF
#!/bin/sh
printf '%s\n' "\$*" >> "$case_dir/boxctl.calls"
exit 0
EOF
  chmod 0755 "$box/bin/boxctl"

  set +e
  SMART_INSTALL_TEST=1 \
  SMART_INSTALL_RUNNING_OVERRIDE=1 \
  BOX_DIR_OVERRIDE="$box" \
    sh "$pkg/install.sh" >"$case_dir/install.out" 2>&1
  rc=$?
  set -e

  [ "$rc" -ne 0 ]
  [ "$("$box/bin/mihomo" -v)" = "rollback-old-core" ]
  grep -F "开始回滚" "$case_dir/install.out" >/dev/null
}

run_legacy_success() {
  case_dir="$ROOT/legacy"
  pkg="$case_dir/pkg"
  box="$case_dir/box"
  mkdir -p "$pkg" "$box/bin" "$box/run" "$box/scripts" "$box/mihomo"
  cp "$INSTALLER_SRC" "$pkg/install.sh"
  make_core "$pkg/mihomo-a64-smart-ebpf-test" "smart-v8-legacy-test"
  make_core "$box/bin/mihomo" "legacy-old-core"
  printf 'mode: rule\n' > "$box/mihomo/config.yaml"
  printf 'bin_name=mihomo\n' > "$box/settings.ini"

  cat > "$box/scripts/box.service" <<EOF
#!/bin/sh
printf '%s\n' "\$*" >> "$case_dir/service.calls"
exit 0
EOF
  chmod 0755 "$box/scripts/box.service"

  SMART_INSTALL_TEST=1 \
  SMART_INSTALL_RUNNING_OVERRIDE=1 \
  SMART_INSTALL_SKIP_RUNNING_VERIFY=1 \
  BOX_DIR_OVERRIDE="$box" \
    sh "$pkg/install.sh" >"$case_dir/install.out" 2>&1

  [ "$("$box/bin/mihomo" -v)" = "smart-v8-legacy-test" ]
  [ "$("$box/bin/mihomo.smart-prev" -v)" = "legacy-old-core" ]
  grep -F "restart" "$case_dir/service.calls" >/dev/null
}

run_modern_success
run_modern_rollback
run_legacy_success

echo "Smart Box v8 installer integration tests: PASS"
