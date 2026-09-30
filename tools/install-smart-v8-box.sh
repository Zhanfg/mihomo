#!/system/bin/sh
# Smart Box v8 installer.
# No arguments are required. It supports both legacy Box for Root scripts and
# the newer BoxProxy app runtime (boxctl + database/box.db).

set -u

BOX_DIR="${BOX_DIR_OVERRIDE:-/data/adb/box}"
BIN_DIR="$BOX_DIR/bin"
RUN_DIR="$BOX_DIR/run"
LOG="$RUN_DIR/smart-box-v8-install.log"
TARGET_DEFAULT="$BIN_DIR/mihomo"
BACKUP_SUFFIX=".smart-prev"

mkdir -p "$RUN_DIR" 2>/dev/null || true
: > "$LOG" 2>/dev/null || true
exec 3>&1
say() { printf '%s\n' "$*" | tee -a "$LOG" >&3; }
fail() { say "[Smart Box v8] ERROR: $*"; exit 1; }

if [ "${SMART_INSTALL_TEST:-0}" != "1" ]; then
  [ "$(id -u 2>/dev/null)" = "0" ] || fail "需要 root 权限。请从 KernelSU/Magisk/APatch 授权的终端执行。"
  case "$(uname -m 2>/dev/null)" in
    aarch64|arm64) ;;
    *) fail "当前产物只支持 Android arm64/aarch64。" ;;
  esac
fi

[ -d "$BOX_DIR" ] || fail "未检测到 Box for Root: $BOX_DIR"
mkdir -p "$BIN_DIR" "$RUN_DIR" || fail "无法准备 Box 目录"

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" 2>/dev/null && pwd)"
[ -n "$SCRIPT_DIR" ] || SCRIPT_DIR="."

# Locate the delivered core. Prefer the raw executable; accept the gzip from the
# Actions artifact so the script remains one-tap/no-argument on Android.
CORE=""
for f in "$SCRIPT_DIR"/mihomo-a64-smart-ebpf-*; do
  [ -f "$f" ] || continue
  case "$f" in
    *.gz|*.zip|*.txt) continue ;;
  esac
  CORE="$f"
  break
done

TMP_EXTRACT=""
if [ -z "$CORE" ]; then
  for gz in "$SCRIPT_DIR"/mihomo-a64-smart-ebpf-*.gz; do
    [ -f "$gz" ] || continue
    TMP_EXTRACT="$RUN_DIR/.mihomo-smart-v8.$$"
    if command -v gzip >/dev/null 2>&1; then
      gzip -dc "$gz" > "$TMP_EXTRACT" || fail "解压核心失败"
    elif command -v busybox >/dev/null 2>&1; then
      busybox gzip -dc "$gz" > "$TMP_EXTRACT" || fail "解压核心失败"
    else
      fail "找不到 gzip/busybox，无法解压核心"
    fi
    CORE="$TMP_EXTRACT"
    break
  done
fi
[ -n "$CORE" ] && [ -s "$CORE" ] || fail "脚本同目录未找到 Smart arm64 核心"
chmod 0755 "$CORE" 2>/dev/null || true

# Verify the artifact when SHA256SUMS is shipped next to it. Verification is
# best-effort only when no sha256 tool exists; the core execution check below is
# always mandatory.
if [ -f "$SCRIPT_DIR/SHA256SUMS" ]; then
  if command -v sha256sum >/dev/null 2>&1; then
    (
      cd "$SCRIPT_DIR" || exit 1
      sha256sum -c SHA256SUMS >/dev/null 2>&1
    ) || fail "SHA256 校验失败，拒绝安装"
    say "[Smart Box v8] SHA256 校验通过"
  elif command -v busybox >/dev/null 2>&1 && busybox sha256sum --help >/dev/null 2>&1; then
    (
      cd "$SCRIPT_DIR" || exit 1
      busybox sha256sum -c SHA256SUMS >/dev/null 2>&1
    ) || fail "SHA256 校验失败，拒绝安装"
    say "[Smart Box v8] SHA256 校验通过"
  fi
fi

VERSION="$("$CORE" -v 2>&1 | head -n 1)"
[ -n "$VERSION" ] || VERSION="$("$CORE" --version 2>&1 | head -n 1)"
[ -n "$VERSION" ] || fail "新核心无法执行"
say "[Smart Box v8] 新核心: $VERSION"

# Modern BoxProxy uses boxctl and database/box.db. Legacy Box for Root uses the
# shell service. Detect both instead of assuming one control plane.
BOXCTL=""
for p in "$BIN_DIR/boxctl" "$BOX_DIR/boxctl" "$BOX_DIR/scripts/boxctl"; do
  if [ -x "$p" ]; then BOXCTL="$p"; break; fi
done

DB=""
for p in "$BOX_DIR/database/box.db" "$BOX_DIR/box.db"; do
  if [ -f "$p" ]; then DB="$p"; break; fi
done
if [ -z "$DB" ]; then
  DB="$(find "$BOX_DIR" -maxdepth 3 -type f -name 'box.db' 2>/dev/null | head -n 1)"
fi

SERVICE=""
for p in "$BOX_DIR/scripts/box.service" "$BOX_DIR/scripts/start.sh"; do
  if [ -x "$p" ]; then SERVICE="$p"; break; fi
done

# Resolve the currently selected core without mutating BoxProxy state. Newer
# app builds keep it in runtime_profile; legacy builds keep bin_name in config.
CURRENT_CORE=""
if [ -n "$DB" ] && command -v sqlite3 >/dev/null 2>&1; then
  CURRENT_CORE="$(sqlite3 "$DB" 'select core_name from runtime_profile where id=1;' 2>/dev/null | head -n 1)"
fi
if [ -z "$CURRENT_CORE" ]; then
  for cfg in "$BOX_DIR/settings.ini" "$BOX_DIR/scripts/box.config"; do
    [ -f "$cfg" ] || continue
    CURRENT_CORE="$(sed -n 's/^[[:space:]]*bin_name[[:space:]]*=[[:space:]]*["'\''"]*\([^"'\''[:space:]]*\).*/\1/p' "$cfg" | head -n 1)"
    [ -n "$CURRENT_CORE" ] && break
  done
fi

# If a Mihomo-family binary is currently running from Box, preserve its exact
# filename. This covers BoxProxy channels that expose "Mihomo Smart" while still
# using the same runtime control plane.
running_core_path() {
  for exe in /proc/[0-9]*/exe; do
    [ -e "$exe" ] || continue
    path="$(readlink "$exe" 2>/dev/null || true)"
    case "$path" in
      "$BIN_DIR"/mihomo|"$BIN_DIR"/mihomo-*|"$BIN_DIR"/mihomo_*)
        printf '%s\n' "$path"
        return 0
        ;;
    esac
  done
  return 1
}

RUNNING_PATH="$(running_core_path 2>/dev/null || true)"
TARGET="$TARGET_DEFAULT"
if [ -n "$RUNNING_PATH" ]; then
  TARGET="$RUNNING_PATH"
elif [ -n "$CURRENT_CORE" ]; then
  case "$CURRENT_CORE" in
    mihomo|mihomo-*|mihomo_*)
      [ -e "$BIN_DIR/$CURRENT_CORE" ] && TARGET="$BIN_DIR/$CURRENT_CORE"
      ;;
  esac
fi

WAS_RUNNING=0
[ -n "$RUNNING_PATH" ] && WAS_RUNNING=1
if [ "$WAS_RUNNING" -eq 0 ] && command -v pidof >/dev/null 2>&1; then
  pidof mihomo >/dev/null 2>&1 && WAS_RUNNING=1
fi
if [ "${SMART_INSTALL_TEST:-0}" = "1" ] && [ "${SMART_INSTALL_RUNNING_OVERRIDE:-0}" = "1" ]; then
  WAS_RUNNING=1
fi

say "[Smart Box v8] Box 目录: $BOX_DIR"
[ -n "$BOXCTL" ] && [ -n "$DB" ] && say "[Smart Box v8] 控制面: boxctl + $(basename "$DB")"
[ -n "$SERVICE" ] && say "[Smart Box v8] 兼容控制: $SERVICE"
[ -n "$CURRENT_CORE" ] && say "[Smart Box v8] Box 当前核心: $CURRENT_CORE"
say "[Smart Box v8] 安装目标: $TARGET"

# Validate against the active Mihomo configuration without rewriting it.
CONFIG=""
if [ -f "$BOX_DIR/mihomo/config.yaml" ]; then
  CONFIG="$BOX_DIR/mihomo/config.yaml"
else
  CONFIG="$(find "$BOX_DIR/mihomo" -maxdepth 1 -type f \( -name '*.yaml' -o -name '*.yml' \) 2>/dev/null | head -n 1)"
fi
if [ -n "${CONFIG:-}" ] && [ -f "$CONFIG" ]; then
  say "[Smart Box v8] 校验现有配置: $CONFIG"
  "$CORE" -t -d "$BOX_DIR/mihomo" -f "$CONFIG" >/dev/null 2>&1 ||
    fail "新核心无法解析当前配置；BoxProxy 数据和旧核心均未修改"
fi

# If the running core must be restarted, require at least one known Box control
# path before touching the binary.
if [ "$WAS_RUNNING" -eq 1 ] && [ -z "$BOXCTL" ] && [ -z "$SERVICE" ]; then
  fail "检测到 Mihomo 正在运行，但找不到 BoxProxy/Box for Root 的控制入口，拒绝热替换"
fi

BACKUP="$TARGET$BACKUP_SUFFIX"
STAGED="$BIN_DIR/.mihomo-smart-v8.$$"
TMP_META="$RUN_DIR/.smart-v8-meta.$$"
cleanup() {
  rm -f "$STAGED" "$TMP_EXTRACT" "$TMP_META" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

# Keep exactly one rollback copy. Preserve original ownership/mode/SELinux
# context instead of hard-coding a group that may differ between Box versions.
if [ -f "$TARGET" ]; then
  rm -f "$BACKUP"
  cp -fp "$TARGET" "$BACKUP" || fail "备份旧核心失败"
  chmod --reference="$TARGET" "$BACKUP" 2>/dev/null || chmod 0755 "$BACKUP" 2>/dev/null || true
  chown --reference="$TARGET" "$BACKUP" 2>/dev/null || true
  if command -v chcon >/dev/null 2>&1; then
    chcon --reference="$TARGET" "$BACKUP" 2>/dev/null || true
  fi
fi

cp -f "$CORE" "$STAGED" || fail "写入临时核心失败"
if [ -f "$TARGET" ]; then
  chmod --reference="$TARGET" "$STAGED" 2>/dev/null || chmod 0755 "$STAGED"
  chown --reference="$TARGET" "$STAGED" 2>/dev/null || true
  if command -v chcon >/dev/null 2>&1; then
    chcon --reference="$TARGET" "$STAGED" 2>/dev/null || true
  fi
else
  chmod 0755 "$STAGED"
  chown 0:0 "$STAGED" 2>/dev/null || true
fi
"$STAGED" -v >/dev/null 2>&1 || "$STAGED" --version >/dev/null 2>&1 || fail "写入后的核心自检失败"

mv -f "$STAGED" "$TARGET" || fail "原子替换核心失败"

# Modern BoxProxy restart path. Invalid command variants are harmless and are
# tried only as compatibility fallbacks because boxctl has changed across app
# revisions. We never rewrite runtime_profile ourselves.
modern_restart() {
  [ -n "$BOXCTL" ] && [ -x "$BOXCTL" ] && [ -n "$DB" ] && [ -f "$DB" ] || return 1
  "$BOXCTL" --db "$DB" restart >>"$LOG" 2>&1 && return 0
  "$BOXCTL" --db "$DB" service restart >>"$LOG" 2>&1 && return 0
  return 1
}

legacy_restart() {
  [ -n "$SERVICE" ] && [ -x "$SERVICE" ] || return 1
  case "$(basename "$SERVICE")" in
    box.service)
      "$SERVICE" restart >>"$LOG" 2>&1 && return 0
      "$SERVICE" stop >>"$LOG" 2>&1 || true
      "$SERVICE" start >>"$LOG" 2>&1 && return 0
      ;;
    start.sh)
      # start.sh is not a stop controller; use it only when Box is already
      # stopped. Running installs should use boxctl or box.service.
      [ "$WAS_RUNNING" -eq 0 ] && "$SERVICE" >>"$LOG" 2>&1 && return 0
      ;;
  esac
  return 1
}

target_is_running() {
  target_inode="$(stat -c %i "$TARGET" 2>/dev/null || true)"
  [ -n "$target_inode" ] || return 1
  for exe in /proc/[0-9]*/exe; do
    [ -e "$exe" ] || continue
    exe_inode="$(stat -Lc %i "$exe" 2>/dev/null || true)"
    [ -n "$exe_inode" ] && [ "$exe_inode" = "$target_inode" ] && return 0
  done
  return 1
}

rollback() {
  say "[Smart Box v8] 新核心启动验证失败，开始回滚"
  if [ -f "$BACKUP" ]; then
    cp -fp "$BACKUP" "$TARGET" 2>/dev/null || true
    chmod --reference="$BACKUP" "$TARGET" 2>/dev/null || chmod 0755 "$TARGET" 2>/dev/null || true
    chown --reference="$BACKUP" "$TARGET" 2>/dev/null || true
    if command -v chcon >/dev/null 2>&1; then
      chcon --reference="$BACKUP" "$TARGET" 2>/dev/null || true
    fi
    modern_restart || legacy_restart || true
  fi
  fail "启动失败，已尽力恢复旧核心；日志: $LOG"
}

if [ "$WAS_RUNNING" -eq 1 ]; then
  say "[Smart Box v8] 由 Box 控制面重启当前服务"
  modern_restart || legacy_restart || rollback
  sleep 2
  if [ "${SMART_INSTALL_TEST:-0}" != "1" ] || [ "${SMART_INSTALL_SKIP_RUNNING_VERIFY:-0}" != "1" ]; then
    target_is_running || rollback
    say "[Smart Box v8] 已确认新 inode 正在运行"
  else
    say "[Smart Box v8] 测试模式：跳过 /proc inode 运行态确认"
  fi
else
  say "[Smart Box v8] Box 当前未运行；只更新核心文件，不擅自启动/切换运行配置"
fi

# Do not accumulate installer backups. One previous core plus one install log is
# the entire installer-owned persistent footprint.
find "$BIN_DIR" -maxdepth 1 -type f -name 'mihomo*.smart-prev.*' -delete 2>/dev/null || true
find "$RUN_DIR" -maxdepth 1 -type f -name 'smart-box-v8-install.log.*' -delete 2>/dev/null || true

say "[Smart Box v8] 安装完成"
say "[Smart Box v8] 当前文件版本: $("$TARGET" -v 2>&1 | head -n 1)"
say "[Smart Box v8] 回滚副本: $BACKUP"
say "[Smart Box v8] 日志: $LOG"
if [ -n "$BOXCTL" ] && [ -n "$DB" ]; then
  say "[Smart Box v8] 已保留 BoxProxy runtime_profile；未修改模式/TUN/TPROXY/eBPF/资源限制设置"
fi
