#!/system/bin/sh
# One-shot Magisk / KernelSU / APatch installer for Smart v7 + Box for Root.

SKIPUNZIP=0

ui() {
  if command -v ui_print >/dev/null 2>&1; then
    ui_print "$1"
  else
    echo "$1"
  fi
}

die() {
  ui "! $1"
  abort "$1"
}

case "$ARCH" in
  arm64|aarch64) ;;
  *) die "Smart v7 当前只提供 arm64/aarch64 核心" ;;
esac

BOX_DIR=/data/adb/box
BIN_DIR=$BOX_DIR/bin
TARGET=$BIN_DIR/mihomo
BACKUP=$BIN_DIR/mihomo.smart-v7-prev
SERVICE=$BOX_DIR/scripts/box.service
CORE=$MODPATH/core/mihomo
LOG=$BOX_DIR/run/smart-v7-module-install.log

[ -d "$BOX_DIR" ] || die "未检测到 Box for Root: $BOX_DIR"
[ -f "$CORE" ] || die "安装包缺少 Smart v7 核心"
mkdir -p "$BIN_DIR" "$BOX_DIR/run" || die "无法准备 Box 目录"
chmod 0755 "$CORE" || die "无法设置新核心权限"

VERSION="$("$CORE" -v 2>&1 | head -n 1)"
[ -n "$VERSION" ] || die "Smart v7 核心无法执行"
ui "- Smart v7: $VERSION"

CONFIG=$BOX_DIR/mihomo/config.yaml
if [ -f "$CONFIG" ]; then
  ui "- 校验现有 Mihomo 配置"
  "$CORE" -t -d "$BOX_DIR/mihomo" -f "$CONFIG" >/dev/null 2>&1 ||
    die "新核心无法解析当前配置；旧核心未修改"
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
  cp -fp "$TARGET" "$BACKUP" || die "备份旧 Mihomo 核心失败"
  chmod 0755 "$BACKUP" 2>/dev/null || true
fi

STAGED=$BIN_DIR/.mihomo-smart-v7.$$
cp -f "$CORE" "$STAGED" || die "暂存 Smart v7 核心失败"
chmod 0755 "$STAGED" || die "设置暂存核心权限失败"
chown 0:3005 "$STAGED" 2>/dev/null || chown 0:0 "$STAGED" 2>/dev/null || true
"$STAGED" -v >/dev/null 2>&1 || die "暂存核心执行自检失败"
mv -f "$STAGED" "$TARGET" || die "替换 Mihomo 核心失败"
chmod 0755 "$TARGET"
chown 0:3005 "$TARGET" 2>/dev/null || chown 0:0 "$TARGET" 2>/dev/null || true

rollback() {
  ui "! 启动失败，恢复旧核心"
  if [ -f "$BACKUP" ]; then
    cp -fp "$BACKUP" "$TARGET"
    chmod 0755 "$TARGET" 2>/dev/null || true
    if [ -x "$SERVICE" ]; then
      "$SERVICE" start mihomo >/dev/null 2>&1 || "$SERVICE" start >/dev/null 2>&1 || true
    fi
  fi
  die "Smart v7 启动验证失败"
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
fi

{
  echo "installed=$(date '+%Y-%m-%d %H:%M:%S' 2>/dev/null)"
  echo "version=$VERSION"
  echo "backup=$BACKUP"
} > "$LOG" 2>/dev/null || true

# This module is only an installer. Do not keep another ~47 MB core copy in
# /data/adb/modules after installation.
rm -rf "$MODPATH/core" 2>/dev/null || true

ui "- Smart v7 已安装到 $TARGET"
ui "- 旧核心备份: $BACKUP"
ui "- 安装日志: $LOG"
ui "- 模块本体安装后仅保留少量元数据，不重复占用核心空间"
