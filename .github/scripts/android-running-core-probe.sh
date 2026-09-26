#!/system/bin/sh
set -u

EXPECTED_SHA256="0ab2bba3dc3b0a6cf45d17d4b93a78faa99c49b772beb394277bfdbf94ef9964"

echo "=== Mihomo running-core probe ==="
date
echo

found=0
for p in /proc/[0-9]*; do
  [ -r "$p/cmdline" ] || continue
  cmd="$(tr '\\000' ' ' < "$p/cmdline" 2>/dev/null)"
  case "$cmd" in
    *mihomo*|*clash*)
      pid="$(basename "$p")"
      exe="$(readlink "$p/exe" 2>/dev/null || true)"
      [ -n "$exe" ] || continue
      found=1
      echo "[PID] $pid"
      echo "[CMD] $cmd"
      echo "[EXE] $exe"
      if command -v sha256sum >/dev/null 2>&1; then
        sum="$(sha256sum "$p/exe" 2>/dev/null | awk '{print $1}')"
        echo "[SHA256 running] $sum"
        if [ "$sum" = "$EXPECTED_SHA256" ]; then
          echo "[MATCH] running core IS the verified 89cdd92 Smart+eBPF binary"
        else
          echo "[MISMATCH] running core is NOT the verified 89cdd92 binary"
        fi
      fi
      echo "[VERSION]"
      "$p/exe" -v 2>&1 | head -n 5 || true
      echo
      ;;
  esac
done

if [ "$found" -eq 0 ]; then
  echo "No running mihomo/clash process found."
fi

echo "=== Candidate core files ==="
for root in /data/adb/box /data/adb/modules /data/local/tmp; do
  [ -d "$root" ] || continue
  find "$root" -type f \( -iname '*mihomo*' -o -iname '*clash*' \) 2>/dev/null | while IFS= read -r f; do
    [ -f "$f" ] || continue
    sum=""
    if command -v sha256sum >/dev/null 2>&1; then
      sum="$(sha256sum "$f" 2>/dev/null | awk '{print $1}')"
    fi
    echo "$f"
    [ -n "$sum" ] && echo "  sha256=$sum"
    [ "$sum" = "$EXPECTED_SHA256" ] && echo "  >>> VERIFIED 89cdd92 MATCH <<<"
  done
done
