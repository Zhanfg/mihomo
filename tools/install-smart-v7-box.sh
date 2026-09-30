#!/system/bin/sh
# Smart v7 Box for Root installer.
# No arguments are required. Run from the extracted Actions artifact directory.

set -u

BOX_DIR="/data/adb/box"
BIN_DIR="$BOX_DIR/bin"
RUN_DIR="$BOX_DIR/run"
SERVICE="$BOX_DIR/scripts/box.service"
TARGET="$BIN_DIR/mihomo"
BACKUP="$BIN_DIR/mihomo.smart-prev"
LOG="$RUN_DIR/smart-v7-install.log"

mkdir -p "$RUN_DIR" 2>/dev/null || true
: > "$LOG"
exec 3>&1
say() {
  printf '%s\n' "$*" | tee -a "$LOG" >&3
}
fail() { say "[Smart v7] ERROR: $*"; exit 1; }

if [ "$(id -u 2>/dev/null)" != "0" ]; then
  fail "需要 root 权限。请从 KernelSU/Magisk/APatch 授权的终端执行此脚本。"
fi

case "$(uname -m 2>/dev/null)" in
  aarch64|arm64) ;;
  *) fail "当前产物只支持 Android arm64/aarch64。" ;;
esac

[ -d "$BOX_DIR" ] || fail "未检测到 Box for Root: $BOX_DIR"
mkdir -p "$BIN_DIR" || fail "无法创建 $BIN_DIR"

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" 2>/dev/null && pwd)"
[ -n "$SCRIPT_DIR" ] || SCRIPT_DIR="."

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
    TMP_EXTRACT="$RUN_DIR/.mihomo-smart-v7.$$"
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

[ -n "$CORE" ] && [ -s "$CORE" ] || fail "脚本同目录未找到 Smart v7 arm64 核心"
chmod 0755 "$CORE" 2>/dev/null || true

VERSION="$("$CORE" -v 2>&1 | head -n 1)"
[ -n "$VERSION" ] || fail "新核心无法执行"
say "[Smart v7] 新核心: $VERSION"

CONFIG="$BOX_DIR/mihomo/config.yaml"
if [ ! -f "$CONFIG" ]; then
  CONFIG="$(find "$BOX_DIR/mihomo" -maxdepth 1 -type f \( -name '*.yaml' -o -name '*.yml' \) 2>/dev/null | head -n 1)"
fi

if [ -n "${CONFIG:-}" ] && [ -f "$CONFIG" ]; then
  say "[Smart v7] 校验现有配置: $CONFIG"
  "$CORE" -t -d "$BOX_DIR/mihomo" -f "$CONFIG" >/dev/null 2>&1 ||
    fail "新核心无法解析当前配置，未修改现有核心"
fi

CURRENT_CORE=""
if [ -f "$BOX_DIR/settings.ini" ]; then
  CURRENT_CORE="$(sed -n 's/^[[:space:]]*bin_name=["'\''"]*\([^"'\''[:space:]]*\).*/\1/p' "$BOX_DIR/settings.ini" | head -n 1)"
fi

WAS_RUNNING=0
if command -v pidof >/dev/null 2>&1 && pidof mihomo >/dev/null 2>&1; then
  WAS_RUNNING=1
elif command -v busybox >/dev/null 2>&1 && busybox pidof mihomo >/dev/null 2>&1; then
  WAS_RUNNING=1
fi

if [ "$WAS_RUNNING" -eq 1 ] && [ -x "$SERVICE" ]; then
  "$SERVICE" stop mihomo >/dev/null 2>&1 || "$SERVICE" stop >/dev/null 2>&1 || true
  sleep 1
fi

if [ -f "$TARGET" ]; then
  rm -f "$BACKUP"
  cp -fp "$TARGET" "$BACKUP" || fail "备份旧核心失败"
  chmod 0755 "$BACKUP" 2>/dev/null || true
fi

STAGED="$BIN_DIR/.mihomo-smart-v7.$$"
cleanup() {
  rm -f "$STAGED" "$TMP_EXTRACT" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

cp -f "$CORE" "$STAGED" || fail "写入临时核心失败"
chmod 0755 "$STAGED" || fail "设置核心权限失败"
chown 0:3005 "$STAGED" 2>/dev/null || chown 0:0 "$STAGED" 2>/dev/null || true
"$STAGED" -v >/dev/null 2>&1 || fail "写入后的核心自检失败"
mv -f "$STAGED" "$TARGET" || fail "原子替换核心失败"
chmod 0755 "$TARGET"
chown 0:3005 "$TARGET" 2>/dev/null || chown 0:0 "$TARGET" 2>/dev/null || true

rollback() {
  if [ -f "$BACKUP" ]; then
    cp -fp "$BACKUP" "$TARGET"
    chmod 0755 "$TARGET" 2>/dev/null || true
    if [ -x "$SERVICE" ]; then
      "$SERVICE" start mihomo >/dev/null 2>&1 || "$SERVICE" start >/dev/null 2>&1 || true
    fi
  fi
  fail "启动失败，已恢复旧核心"
}

if [ "$CURRENT_CORE" = "mihomo" ] || [ "$WAS_RUNNING" -eq 1 ]; then
  [ -x "$SERVICE" ] || rollback
  "$SERVICE" start mihomo >/dev/null 2>&1 || "$SERVICE" start >/dev/null 2>&1 || rollback
  sleep 2
  if command -v pidof >/dev/null 2>&1; then
    pidof mihomo >/dev/null 2>&1 || rollback
  elif command -v busybox >/dev/null 2>&1; then
    busybox pidof mihomo >/dev/null 2>&1 || rollback
  fi
else
  say "[Smart v7] 当前 Box 核心不是 mihomo；已安装到 $TARGET，但未自动切换。"
fi

say "[Smart v7] 安装完成"
say "[Smart v7] 当前核心: $("$TARGET" -v 2>&1 | head -n 1)"
say "[Smart v7] 回滚副本固定为: $BACKUP"
say "[Smart v7] 日志固定为: $LOG"
