#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

OUT_DIR="$ROOT/dist/boxproxy-app"
rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR"

HEAD_SHA="$(git rev-parse HEAD)"
HEAD_SHORT="$(printf '%s' "$HEAD_SHA" | cut -c1-7)"
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
VERSION="smart-boxproxy-$HEAD_SHORT"
RAW="$OUT_DIR/mihomo"
ARCHIVE="$OUT_DIR/mihomo-android-arm64-v8-alpha-smart-$HEAD_SHORT.gz"

CGO_ENABLED=0 GOOS=android GOARCH=arm64 \
  go build -buildvcs=false -tags "with_gvisor with_ebpf" -trimpath \
  -ldflags "-X 'github.com/metacubex/mihomo/constant.Version=$VERSION' -X 'github.com/metacubex/mihomo/constant.BuildTime=$BUILD_TIME' -w -s -buildid=" \
  -o "$RAW"

test -s "$RAW"
gzip -n -9 -c "$RAW" > "$ARCHIVE"
gzip -t "$ARCHIVE"

case "$(basename "$ARCHIVE")" in
  mihomo-android-arm64-v8-alpha-smart-[0-9a-fA-F]*.gz) ;;
  *)
    echo "unexpected BoxProxy core filename: $(basename "$ARCHIVE")" >&2
    exit 1
    ;;
esac

sha256sum "$RAW" "$ARCHIVE" > "$OUT_DIR/SHA256SUMS"
{
  echo "mihomo_source=$HEAD_SHA"
  echo "mihomo_version=$VERSION"
  echo "build_time=$BUILD_TIME"
  echo "target=android/arm64-v8a"
  echo "cgo=0"
  echo "tags=with_gvisor with_ebpf"
  echo "boxproxy_import=$(basename "$ARCHIVE")"
  echo "boxproxy_runtime_repo=https://github.com/boxproxy/boxproxy"
  echo "boxproxy_runtime_ref=${BOXPROXY_RUNTIME_SHA:-unverified-local-build}"
  echo "boxproxy_app_home=/data/user/0/com.boxproxy.box/files/box"
  echo "boxproxy_core_target=bin/mihomo"
} > "$OUT_DIR/BUILD_INFO"

cat > "$OUT_DIR/IMPORT.txt" <<EOF
BoxProxy APK local core import

Import this file:
$(basename "$ARCHIVE")

Expected target managed by BoxProxy:
  /data/user/0/com.boxproxy.box/files/box/bin/mihomo

The archive name matches the Mihomo Smart filename pattern embedded in the
BoxProxy APK. Use BoxProxy's local core import function; do not flash this APK
or core file as a KernelSU/Magisk module.
EOF

printf 'BoxProxy import core: %s\n' "$ARCHIVE"
