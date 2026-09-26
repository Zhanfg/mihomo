#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BIN="${1:-$ROOT_DIR/mihomo-linux-ebpf}"
STATE="$ROOT_DIR/.ebpf-state"
ART="$ROOT_DIR/.ebpf-artifacts"
CFG="$STATE/ebpf.yaml"
NS_CLIENT="mh-ep-client"
NS_WAN="mh-ep-wan"
IN_HOST="mhepin0"
IN_NS="mhepinc"
WAN_HOST="mhepwan0"
WAN_NS="mhepwanw"

rm -rf "$STATE" "$ART"
mkdir -p "$STATE" "$ART"

log(){ printf '\n[%s] %s\n' "$(date -u +%H:%M:%S)" "$*"; }
die(){ echo "ERROR: $*" >&2; exit 1; }

cleanup(){
  set +e
  [[ -f "$STATE/pid" ]] && sudo kill "$(cat "$STATE/pid")" 2>/dev/null || true
  for ns in "$NS_CLIENT" "$NS_WAN"; do
    sudo ip netns pids "$ns" 2>/dev/null | xargs -r sudo kill -KILL 2>/dev/null || true
    sudo ip netns del "$ns" 2>/dev/null || true
  done
}
trap cleanup EXIT

[[ -x "$BIN" ]] || die "missing eBPF binary: $BIN"
sudo sysctl -w net.ipv4.ip_forward=1 >/dev/null
sudo sysctl -w net.ipv6.conf.all.forwarding=1 >/dev/null

log "Create EP/eBPF client -> host -> WAN topology"
sudo ip netns add "$NS_CLIENT"
sudo ip netns add "$NS_WAN"
sudo ip -n "$NS_CLIENT" link set lo up
sudo ip -n "$NS_WAN" link set lo up

sudo ip link add "$IN_HOST" type veth peer name "$IN_NS"
sudo ip link set "$IN_NS" netns "$NS_CLIENT"
sudo ip addr add 10.77.0.1/24 dev "$IN_HOST"
sudo ip -6 addr add fd77::1/64 dev "$IN_HOST" nodad
sudo ip link set "$IN_HOST" up
sudo ip -n "$NS_CLIENT" addr add 10.77.0.2/24 dev "$IN_NS"
sudo ip -n "$NS_CLIENT" -6 addr add fd77::2/64 dev "$IN_NS" nodad
sudo ip -n "$NS_CLIENT" link set "$IN_NS" up
sudo ip -n "$NS_CLIENT" route add default via 10.77.0.1
sudo ip -n "$NS_CLIENT" -6 route add default via fd77::1

sudo ip link add "$WAN_HOST" type veth peer name "$WAN_NS"
sudo ip link set "$WAN_NS" netns "$NS_WAN"
sudo ip addr add 10.78.0.1/24 dev "$WAN_HOST"
sudo ip -6 addr add fd78::1/64 dev "$WAN_HOST" nodad
sudo ip link set "$WAN_HOST" up
sudo ip -n "$NS_WAN" addr add 10.78.0.2/24 dev "$WAN_NS"
sudo ip -n "$NS_WAN" -6 addr add fd78::2/64 dev "$WAN_NS" nodad
sudo ip -n "$NS_WAN" link set "$WAN_NS" up
sudo ip -n "$NS_WAN" route add default via 10.78.0.1
sudo ip -n "$NS_WAN" -6 route add default via fd78::1

sudo iptables -I FORWARD 1 -j ACCEPT
sudo ip6tables -I FORWARD 1 -j ACCEPT

cat > "$STATE/server.py" <<'PY'
import socket
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from threading import Thread

class H(BaseHTTPRequestHandler):
    def do_GET(self):
        b=f"peer={self.client_address[0]} path={self.path}\n".encode()
        self.send_response(200); self.send_header("Content-Length",str(len(b))); self.end_headers(); self.wfile.write(b)
    def log_message(self,*args): pass

class V6(ThreadingHTTPServer):
    address_family=socket.AF_INET6

Thread(target=lambda: ThreadingHTTPServer(("10.78.0.2",18080),H).serve_forever(),daemon=True).start()
V6(("fd78::2",18080),H).serve_forever()
PY
sudo ip netns exec "$NS_WAN" python3 "$STATE/server.py" >"$ART/server.log" 2>&1 &
sleep 0.4

log "Preflight kernel forwarding without eBPF"
P4="$(sudo ip netns exec "$NS_CLIENT" curl -fsS --max-time 5 http://10.78.0.2:18080/preflight-v4)"
P6="$(sudo ip netns exec "$NS_CLIENT" curl -g -6 -fsS --max-time 5 'http://[fd78::2]:18080/preflight-v6')"
echo "$P4" | tee "$ART/preflight-v4.txt"
echo "$P6" | tee "$ART/preflight-v6.txt"
grep -q 'peer=10.77.0.2' <<<"$P4" || die "unexpected IPv4 preflight source"
grep -q 'peer=fd77::2' <<<"$P6" || die "unexpected IPv6 preflight source"

cat > "$CFG" <<YAML
mixed-port: 27890
external-controller: 127.0.0.1:29090
allow-lan: false
ipv6: true
mode: rule
log-level: debug
ebpf:
  enable: true
  interface-name: $IN_HOST
  auto-detect-interface: false
  bypass-private-address: false
  ipv6: true
  tc-priority: 1
rules:
  - IP-CIDR,10.78.0.2/32,DIRECT,no-resolve
  - IP-CIDR6,fd78::2/128,DIRECT,no-resolve
  - MATCH,DIRECT
YAML

log "Start latest-Alpha + eBPF patch core"
sudo "$BIN" -t -d "$STATE" -f "$CFG" 2>&1 | tee "$ART/config-test.log"
sudo "$BIN" -d "$STATE" -f "$CFG" >"$ART/mihomo.log" 2>&1 &
PID=$!
echo "$PID" > "$STATE/pid"
for _ in $(seq 1 80); do
  curl -fsS http://127.0.0.1:29090/version >"$ART/version.json" 2>/dev/null && break
  sleep 0.1
done
curl -fsS http://127.0.0.1:29090/configs | jq . >"$ART/configs.json"
sudo tc qdisc show dev "$IN_HOST" >"$ART/tc-qdisc.txt" 2>&1 || true
sudo tc filter show dev "$IN_HOST" ingress >"$ART/tc-ingress.txt" 2>&1 || true

# GitHub-hosted kernels can prohibit the TC/BPF attach operation even when the
# eBPF code builds and the configuration parses. Treat that as a runner
# capability gap, not as a Mihomo regression; Android/eBPF build checks still
# run after this script returns.
if grep -Eq '\[EBPF\] start failed: .*operation not supported' "$ART/mihomo.log"; then
  {
    echo "result=capability-skip"
    uname -a
    echo "--- configs.ebpf ---"
    jq '.ebpf' "$ART/configs.json" || true
    echo "--- tc qdisc ---"
    cat "$ART/tc-qdisc.txt" || true
    echo "--- tc ingress ---"
    cat "$ART/tc-ingress.txt" || true
  } | tee "$ART/capability-skip.txt"
  echo EBPF_RUNTIME_CAPABILITY_SKIP | tee "$ART/result.txt"
  exit 0
fi

jq -e '.ebpf.enable == true' "$ART/configs.json" >/dev/null || die "eBPF requested but runtime config is not enabled"
[[ -s "$ART/tc-ingress.txt" ]] || die "eBPF runtime started without an ingress TC filter"

log "Verify IPv4 is intercepted and re-originated by Mihomo"
R4="$(sudo ip netns exec "$NS_CLIENT" curl -fsS --max-time 8 http://10.78.0.2:18080/ep-v4)"
echo "$R4" | tee "$ART/ep-v4.txt"
grep -q 'peer=10.78.0.1' <<<"$R4" || die "IPv4 traffic was not re-originated by eBPF/Mihomo"

log "Verify IPv6 is intercepted and re-originated by Mihomo"
R6="$(sudo ip netns exec "$NS_CLIENT" curl -g -6 -fsS --max-time 8 'http://[fd78::2]:18080/ep-v6')"
echo "$R6" | tee "$ART/ep-v6.txt"
grep -q 'peer=fd78::1' <<<"$R6" || die "IPv6 traffic was not re-originated by eBPF/Mihomo"

log "Interface churn: flap ingress link and require recovery"
sudo ip link set "$IN_HOST" down
sleep 0.3
sudo ip link set "$IN_HOST" up
sleep 0.8
R4B="$(sudo ip netns exec "$NS_CLIENT" curl -fsS --max-time 8 http://10.78.0.2:18080/ep-after-flap)"
echo "$R4B" | tee "$ART/ep-after-flap.txt"
grep -q 'peer=10.78.0.1' <<<"$R4B" || die "eBPF path did not recover after interface flap"

echo EBPF_ROUTING_SMOKE_PASS | tee "$ART/result.txt"
