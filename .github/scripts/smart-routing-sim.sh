#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BIN="${ROOT_DIR}/mihomo-routing-test"
ART="${ROOT_DIR}/.route-artifacts"
STATE="${ROOT_DIR}/.route-state"
MAIN_DIR="${STATE}/main"
PF_DIR="${STATE}/proxy-fast"
PS_DIR="${STATE}/proxy-slow"
MAIN_CFG="${STATE}/main.yaml"
PF_CFG="${STATE}/proxy-fast.yaml"
PS_CFG="${STATE}/proxy-slow.yaml"

NS_CLIENT="mh-client"
NS_WAN="mh-wan"
NS_FAST="mh-pfast"
NS_SLOW="mh-pslow"

V_CLIENT_HOST="mhclh"
V_CLIENT_NS="mhcln"
V_WAN_HOST="mhwanh"
V_WAN_NS="mhwann"
V_FAST_HOST="mhpfh"
V_FAST_NS="mhpfn"
V_SLOW_HOST="mhpsh"
V_SLOW_NS="mhpsn"

TPROXY_MARK="0x1"
ROUTING_MARK_HEX="0x4000"
ROUTING_MARK_DEC="16384"
TPROXY_PORT="9898"

mkdir -p "${ART}" "${MAIN_DIR}" "${PF_DIR}" "${PS_DIR}"

log() { printf '\n[%s] %s\n' "$(date -u +%H:%M:%S)" "$*"; }
die() { echo "ERROR: $*" >&2; exit 1; }

ns_pids_kill() {
  local ns="$1"
  if sudo ip netns list | awk '{print $1}' | grep -qx "$ns"; then
    sudo ip netns pids "$ns" 2>/dev/null | xargs -r sudo kill -TERM 2>/dev/null || true
    sleep 0.3
    sudo ip netns pids "$ns" 2>/dev/null | xargs -r sudo kill -KILL 2>/dev/null || true
  fi
}

cleanup() {
  set +e
  if [[ -f "${STATE}/main.pid" ]]; then
    sudo kill "$(cat "${STATE}/main.pid")" 2>/dev/null || true
  fi
  for ns in "$NS_CLIENT" "$NS_WAN" "$NS_FAST" "$NS_SLOW"; do
    ns_pids_kill "$ns"
  done
  sudo iptables -t mangle -D PREROUTING -i "$V_CLIENT_HOST" -p tcp -d 10.20.0.0/24 -j TPROXY --on-port "$TPROXY_PORT" --tproxy-mark "${TPROXY_MARK}/${TPROXY_MARK}" 2>/dev/null || true
  sudo iptables -t mangle -D PREROUTING -i "$V_CLIENT_HOST" -p udp -d 10.20.0.0/24 -j TPROXY --on-port "$TPROXY_PORT" --tproxy-mark "${TPROXY_MARK}/${TPROXY_MARK}" 2>/dev/null || true
  sudo ip6tables -t mangle -D PREROUTING -i "$V_CLIENT_HOST" -p tcp -d 2001:db8:20::/64 -j TPROXY --on-port "$TPROXY_PORT" --tproxy-mark "${TPROXY_MARK}/${TPROXY_MARK}" 2>/dev/null || true
  sudo ip6tables -t mangle -D PREROUTING -i "$V_CLIENT_HOST" -p udp -d 2001:db8:20::/64 -j TPROXY --on-port "$TPROXY_PORT" --tproxy-mark "${TPROXY_MARK}/${TPROXY_MARK}" 2>/dev/null || true
  sudo iptables -D OUTPUT -d 10.30.0.0/24 -m mark ! --mark "$ROUTING_MARK_HEX" -j REJECT 2>/dev/null || true
  sudo iptables -D OUTPUT -d 10.31.0.0/24 -m mark ! --mark "$ROUTING_MARK_HEX" -j REJECT 2>/dev/null || true
  sudo iptables -t mangle -D OUTPUT -d 10.30.0.0/24 -m mark --mark "$ROUTING_MARK_HEX" -j ACCEPT 2>/dev/null || true
  sudo iptables -t mangle -D OUTPUT -d 10.31.0.0/24 -m mark --mark "$ROUTING_MARK_HEX" -j ACCEPT 2>/dev/null || true
  sudo ip rule del fwmark "${TPROXY_MARK}/${TPROXY_MARK}" table 100 priority 100 2>/dev/null || true
  sudo ip -6 rule del fwmark "${TPROXY_MARK}/${TPROXY_MARK}" table 100 priority 100 2>/dev/null || true
  sudo ip route flush table 100 2>/dev/null || true
  sudo ip -6 route flush table 100 2>/dev/null || true
  for ns in "$NS_CLIENT" "$NS_WAN" "$NS_FAST" "$NS_SLOW"; do
    sudo ip netns del "$ns" 2>/dev/null || true
  done
}
trap cleanup EXIT

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing command: $1"
}

for c in ip iptables ip6tables tc curl jq dig python3 timeout; do
  require_cmd "$c"
done

[[ -x "$BIN" ]] || die "missing test binary: $BIN"

log "Kernel/network capability snapshot"
{
  uname -a
  iptables --version
  ip6tables --version
  ip -V
  sysctl net.ipv4.ip_forward || true
  sysctl net.ipv6.conf.all.forwarding || true
} | tee "${ART}/environment.txt"

sudo modprobe xt_TPROXY 2>/dev/null || true
sudo modprobe nf_tproxy_ipv4 2>/dev/null || true
sudo modprobe nf_tproxy_ipv6 2>/dev/null || true
sudo sysctl -w net.ipv4.ip_forward=1 >/dev/null
sudo sysctl -w net.ipv6.conf.all.forwarding=1 >/dev/null
sudo sysctl -w net.ipv4.conf.all.rp_filter=0 >/dev/null || true
sudo sysctl -w net.ipv4.conf.default.rp_filter=0 >/dev/null || true

log "Create namespaces and routed veth topology"
for ns in "$NS_CLIENT" "$NS_WAN" "$NS_FAST" "$NS_SLOW"; do
  sudo ip netns add "$ns"
  sudo ip -n "$ns" link set lo up
done

sudo ip link add "$V_CLIENT_HOST" type veth peer name "$V_CLIENT_NS"
sudo ip link set "$V_CLIENT_NS" netns "$NS_CLIENT"
sudo ip addr add 10.10.0.1/24 dev "$V_CLIENT_HOST"
sudo ip -6 addr add 2001:db8:10::1/64 dev "$V_CLIENT_HOST"
sudo ip link set "$V_CLIENT_HOST" up
sudo ip -n "$NS_CLIENT" addr add 10.10.0.2/24 dev "$V_CLIENT_NS"
sudo ip -n "$NS_CLIENT" -6 addr add 2001:db8:10::2/64 dev "$V_CLIENT_NS"
sudo ip -n "$NS_CLIENT" link set "$V_CLIENT_NS" up
sudo ip -n "$NS_CLIENT" route add default via 10.10.0.1
sudo ip -n "$NS_CLIENT" -6 route add default via 2001:db8:10::1

sudo ip link add "$V_WAN_HOST" type veth peer name "$V_WAN_NS"
sudo ip link set "$V_WAN_NS" netns "$NS_WAN"
sudo ip addr add 10.20.0.1/24 dev "$V_WAN_HOST"
sudo ip -6 addr add 2001:db8:20::1/64 dev "$V_WAN_HOST"
sudo ip link set "$V_WAN_HOST" up
sudo ip -n "$NS_WAN" addr add 10.20.0.2/24 dev "$V_WAN_NS"
sudo ip -n "$NS_WAN" addr add 10.20.0.3/24 dev "$V_WAN_NS"
sudo ip -n "$NS_WAN" -6 addr add 2001:db8:20::2/64 dev "$V_WAN_NS"
sudo ip -n "$NS_WAN" -6 addr add 2001:db8:20::3/64 dev "$V_WAN_NS"
sudo ip -n "$NS_WAN" link set "$V_WAN_NS" up
sudo ip -n "$NS_WAN" route add default via 10.20.0.1
sudo ip -n "$NS_WAN" -6 route add default via 2001:db8:20::1

sudo ip link add "$V_FAST_HOST" type veth peer name "$V_FAST_NS"
sudo ip link set "$V_FAST_NS" netns "$NS_FAST"
sudo ip addr add 10.30.0.1/24 dev "$V_FAST_HOST"
sudo ip -6 addr add 2001:db8:30::1/64 dev "$V_FAST_HOST"
sudo ip link set "$V_FAST_HOST" up
sudo ip -n "$NS_FAST" addr add 10.30.0.2/24 dev "$V_FAST_NS"
sudo ip -n "$NS_FAST" -6 addr add 2001:db8:30::2/64 dev "$V_FAST_NS"
sudo ip -n "$NS_FAST" link set "$V_FAST_NS" up
sudo ip -n "$NS_FAST" route add default via 10.30.0.1
sudo ip -n "$NS_FAST" -6 route add default via 2001:db8:30::1

sudo ip link add "$V_SLOW_HOST" type veth peer name "$V_SLOW_NS"
sudo ip link set "$V_SLOW_NS" netns "$NS_SLOW"
sudo ip addr add 10.31.0.1/24 dev "$V_SLOW_HOST"
sudo ip -6 addr add 2001:db8:31::1/64 dev "$V_SLOW_HOST"
sudo ip link set "$V_SLOW_HOST" up
sudo ip -n "$NS_SLOW" addr add 10.31.0.2/24 dev "$V_SLOW_NS"
sudo ip -n "$NS_SLOW" -6 addr add 2001:db8:31::2/64 dev "$V_SLOW_NS"
sudo ip -n "$NS_SLOW" link set "$V_SLOW_NS" up
sudo ip -n "$NS_SLOW" route add default via 10.31.0.1
sudo ip -n "$NS_SLOW" -6 route add default via 2001:db8:31::1

for dev in "$V_CLIENT_HOST" "$V_WAN_HOST" "$V_FAST_HOST" "$V_SLOW_HOST"; do
  sudo sysctl -w "net.ipv4.conf.${dev}.rp_filter=0" >/dev/null || true
done

sudo iptables -I FORWARD 1 -j ACCEPT
sudo ip6tables -I FORWARD 1 -j ACCEPT

sudo tc qdisc add dev "$V_FAST_HOST" root netem delay 8ms 1ms
sudo tc qdisc add dev "$V_SLOW_HOST" root netem delay 90ms 5ms

log "Create deterministic WAN HTTP/UDP endpoints"
cat > "${STATE}/peer_server.py" <<'PY'
import argparse, socket
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

class V6HTTPServer(ThreadingHTTPServer):
    address_family = socket.AF_INET6

class Handler(BaseHTTPRequestHandler):
    label = "unset"
    def do_GET(self):
        peer = self.client_address[0]
        body = f"{self.label} peer={peer} path={self.path}\n".encode()
        self.send_response(200)
        self.send_header("Content-Type", "text/plain")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)
    def log_message(self, fmt, *args):
        pass

def run_http(bind, port, label):
    Handler.label = label
    cls = V6HTTPServer if ":" in bind else ThreadingHTTPServer
    cls((bind, port), Handler).serve_forever()

def run_udp(bind, port, label):
    fam = socket.AF_INET6 if ":" in bind else socket.AF_INET
    s = socket.socket(fam, socket.SOCK_DGRAM)
    s.bind((bind, port))
    while True:
        data, peer = s.recvfrom(65535)
        s.sendto(f"{label} peer={peer[0]} data={data.decode(errors='replace')}\n".encode(), peer)

p = argparse.ArgumentParser()
p.add_argument("mode", choices=["http", "udp"])
p.add_argument("bind")
p.add_argument("port", type=int)
p.add_argument("label")
a = p.parse_args()
run_http(a.bind, a.port, a.label) if a.mode == "http" else run_udp(a.bind, a.port, a.label)
PY

sudo ip netns exec "$NS_WAN" python3 "${STATE}/peer_server.py" http 10.20.0.2 18080 domestic-v4 >"${ART}/wan-domestic-v4.log" 2>&1 &
sudo ip netns exec "$NS_WAN" python3 "${STATE}/peer_server.py" http 10.20.0.3 18080 foreign-v4 >"${ART}/wan-foreign-v4.log" 2>&1 &
sudo ip netns exec "$NS_WAN" python3 "${STATE}/peer_server.py" http 2001:db8:20::2 18080 domestic-v6 >"${ART}/wan-domestic-v6.log" 2>&1 &
sudo ip netns exec "$NS_WAN" python3 "${STATE}/peer_server.py" http 2001:db8:20::3 18080 foreign-v6 >"${ART}/wan-foreign-v6.log" 2>&1 &
sudo ip netns exec "$NS_WAN" python3 "${STATE}/peer_server.py" udp 10.20.0.3 18081 foreign-udp-v4 >"${ART}/wan-udp-v4.log" 2>&1 &
sudo ip netns exec "$NS_WAN" python3 "${STATE}/peer_server.py" udp 2001:db8:20::3 18081 foreign-udp-v6 >"${ART}/wan-udp-v6.log" 2>&1 &

sleep 0.5
log "Preflight WAN IPv4/IPv6 endpoints before transparent interception"
sudo ip netns exec "$NS_WAN" ss -lntup | tee "${ART}/wan-listeners.txt"
curl --noproxy '*' -fsS --max-time 5 http://10.20.0.2:18080/preflight-v4 | tee "${ART}/preflight-host-v4.txt"
curl --noproxy '*' -g -6 -v --max-time 5 'http://[2001:db8:20::2]:18080/preflight-v6' \
  >"${ART}/preflight-host-v6.txt" 2>"${ART}/preflight-host-v6.stderr"
grep -q 'domestic-v6' "${ART}/preflight-host-v6.txt" || {
  echo "IPv6 WAN server preflight failed before TProxy" >&2
  cat "${ART}/preflight-host-v6.stderr" >&2 || true
  exit 1
}

cat > "$PF_CFG" <<'YAML'
socks-port: 1080
allow-lan: true
bind-address: '*'
ipv6: true
mode: rule
log-level: info
rules:
  - MATCH,DIRECT
YAML
cp "$PF_CFG" "$PS_CFG"

log "Start two real Mihomo SOCKS5 proxy nodes"
sudo ip netns exec "$NS_FAST" "$BIN" -d "$PF_DIR" -f "$PF_CFG" >"${ART}/proxy-fast.log" 2>&1 &
sudo ip netns exec "$NS_SLOW" "$BIN" -d "$PS_DIR" -f "$PS_CFG" >"${ART}/proxy-slow.log" 2>&1 &

for ns in "$NS_FAST" "$NS_SLOW"; do
  for _ in $(seq 1 50); do
    if sudo ip netns exec "$ns" ss -lnt | grep -q ':1080 '; then break; fi
    sleep 0.1
  done
  sudo ip netns exec "$ns" ss -lnt | grep -q ':1080 ' || die "SOCKS node did not listen in $ns"
done

log "Install transparent-routing policy and self-capture guard"
sudo ip rule add fwmark "${TPROXY_MARK}/${TPROXY_MARK}" table 100 priority 100
sudo ip route add local 0.0.0.0/0 dev lo table 100
sudo ip -6 rule add fwmark "${TPROXY_MARK}/${TPROXY_MARK}" table 100 priority 100
sudo ip -6 route add local ::/0 dev lo table 100

sudo iptables -t mangle -A PREROUTING -i "$V_CLIENT_HOST" -p tcp -d 10.20.0.0/24 -j TPROXY --on-port "$TPROXY_PORT" --tproxy-mark "${TPROXY_MARK}/${TPROXY_MARK}"
sudo iptables -t mangle -A PREROUTING -i "$V_CLIENT_HOST" -p udp -d 10.20.0.0/24 -j TPROXY --on-port "$TPROXY_PORT" --tproxy-mark "${TPROXY_MARK}/${TPROXY_MARK}"
sudo ip6tables -t mangle -A PREROUTING -i "$V_CLIENT_HOST" -p tcp -d 2001:db8:20::/64 -j TPROXY --on-port "$TPROXY_PORT" --tproxy-mark "${TPROXY_MARK}/${TPROXY_MARK}"
sudo ip6tables -t mangle -A PREROUTING -i "$V_CLIENT_HOST" -p udp -d 2001:db8:20::/64 -j TPROXY --on-port "$TPROXY_PORT" --tproxy-mark "${TPROXY_MARK}/${TPROXY_MARK}"

sudo iptables -t mangle -A OUTPUT -d 10.30.0.0/24 -m mark --mark "$ROUTING_MARK_HEX" -j ACCEPT
sudo iptables -t mangle -A OUTPUT -d 10.31.0.0/24 -m mark --mark "$ROUTING_MARK_HEX" -j ACCEPT
sudo iptables -A OUTPUT -d 10.30.0.0/24 -m mark ! --mark "$ROUTING_MARK_HEX" -j REJECT
sudo iptables -A OUTPUT -d 10.31.0.0/24 -m mark ! --mark "$ROUTING_MARK_HEX" -j REJECT

cat > "$MAIN_CFG" <<YAML
mixed-port: 7890
tproxy-port: ${TPROXY_PORT}
allow-lan: true
bind-address: '*'
external-controller: 127.0.0.1:9090
mode: rule
log-level: debug
ipv6: true
tcp-concurrent: true
routing-mark: ${ROUTING_MARK_DEC}
hosts:
  domestic.test: 10.20.0.2
  foreign.test: 10.20.0.3
dns:
  enable: true
  listen: 0.0.0.0:1053
  ipv6: true
  use-hosts: true
  enhanced-mode: redir-host
  nameserver:
    - system
proxies:
  - name: proxy-fast
    type: socks5
    server: 10.30.0.2
    port: 1080
    udp: true
  - name: proxy-slow
    type: socks5
    server: 10.31.0.2
    port: 1080
    udp: true
proxy-groups:
  - name: SMART
    type: smart
    proxies:
      - proxy-fast
      - proxy-slow
    url: http://10.20.0.3:18080/health
    interval: 15
    tolerance: 20
    uselightgbm: false
    collectdata: false
    prefer-asn: false
rules:
  - IP-CIDR,10.20.0.2/32,DIRECT,no-resolve
  - IP-CIDR,10.20.0.3/32,SMART,no-resolve
  - IP-CIDR6,2001:db8:20::2/128,DIRECT,no-resolve
  - IP-CIDR6,2001:db8:20::3/128,SMART,no-resolve
  - MATCH,SMART
YAML

log "Validate and start latest Smart Alpha"
sudo "$BIN" -t -d "$MAIN_DIR" -f "$MAIN_CFG" 2>&1 | tee "${ART}/config-test.log"
sudo "$BIN" -d "$MAIN_DIR" -f "$MAIN_CFG" >"${ART}/mihomo-main.log" 2>&1 &
MAIN_PID=$!
echo "$MAIN_PID" > "${STATE}/main.pid"

for _ in $(seq 1 80); do
  if curl -fsS http://127.0.0.1:9090/version >"${ART}/version.json" 2>/dev/null; then break; fi
  sleep 0.1
done
curl -fsS http://127.0.0.1:9090/version | tee "${ART}/version.json"
curl -fsS http://127.0.0.1:9090/proxies/SMART | jq . > "${ART}/smart-initial.json" || true

log "A: explicit mixed-port path"
EXPLICIT="$(curl --noproxy '' -fsS --max-time 10 -x http://127.0.0.1:7890 http://10.20.0.3:18080/explicit)"
echo "$EXPLICIT" | tee "${ART}/explicit-mixed.txt"
grep -q 'foreign-v4' <<<"$EXPLICIT" || die "explicit mixed-port did not reach foreign endpoint"
grep -Eq 'peer=10\.(30|31)\.0\.2' <<<"$EXPLICIT" || die "explicit mixed-port bypassed Smart proxy nodes"

log "B: transparent IPv4 DIRECT"
DIRECT4="$(sudo ip netns exec "$NS_CLIENT" env -u ALL_PROXY -u HTTPS_PROXY -u HTTP_PROXY -u NO_PROXY curl -fsS --max-time 10 http://10.20.0.2:18080/direct-v4)"
echo "$DIRECT4" | tee "${ART}/transparent-direct-v4.txt"
grep -q 'domestic-v4' <<<"$DIRECT4" || die "transparent DIRECT v4 failed"
grep -q 'peer=10.20.0.1' <<<"$DIRECT4" || die "DIRECT v4 did not egress from Mihomo host"

log "C: transparent IPv4 Smart"
FOREIGN4=""
for _ in $(seq 1 5); do
  if FOREIGN4="$(sudo ip netns exec "$NS_CLIENT" env -u ALL_PROXY -u HTTPS_PROXY -u HTTP_PROXY -u NO_PROXY curl -fsS --max-time 10 http://10.20.0.3:18080/foreign-v4 2>/dev/null)"; then break; fi
  sleep 1
done
echo "$FOREIGN4" | tee "${ART}/transparent-smart-v4.txt"
grep -q 'foreign-v4' <<<"$FOREIGN4" || die "transparent Smart v4 failed"
grep -Eq 'peer=10\.(30|31)\.0\.2' <<<"$FOREIGN4" || die "transparent Smart v4 did not traverse a proxy node"

SELECTED_V4="slow"
if grep -q 'peer=10.30.0.2' <<<"$FOREIGN4"; then SELECTED_V4="fast"; fi
echo "$SELECTED_V4" > "${ART}/selected-before-failover.txt"

log "D: kill selected Smart node and require failover"
if [[ "$SELECTED_V4" == "fast" ]]; then
  ns_pids_kill "$NS_FAST"
  EXPECT_PEER='peer=10.31.0.2'
else
  ns_pids_kill "$NS_SLOW"
  EXPECT_PEER='peer=10.30.0.2'
fi

FAILOVER=""
for _ in $(seq 1 8); do
  if FAILOVER="$(sudo ip netns exec "$NS_CLIENT" env -u ALL_PROXY -u HTTPS_PROXY -u HTTP_PROXY -u NO_PROXY curl -fsS --max-time 8 http://10.20.0.3:18080/failover 2>/dev/null)" && grep -q "$EXPECT_PEER" <<<"$FAILOVER"; then break; fi
  sleep 1
done
echo "$FAILOVER" | tee "${ART}/failover-v4.txt"
grep -q "$EXPECT_PEER" <<<"$FAILOVER" || die "Smart did not fail over to surviving node"

log "E: transparent IPv6 DIRECT and Smart"
sudo ip -6 addr show | tee "${ART}/host-ip6-addr.txt"
sudo ip -6 route show table all | tee "${ART}/host-ip6-route-all.txt"
sudo ip netns exec "$NS_CLIENT" ip -6 addr show | tee "${ART}/client-ip6-addr.txt"
sudo ip netns exec "$NS_CLIENT" ip -6 route show table all | tee "${ART}/client-ip6-route-all-pre.txt"
sudo ip6tables -t mangle -L PREROUTING -v -n -x | tee "${ART}/ip6tables-prerouting-before-v6.txt"

set +e
sudo ip netns exec "$NS_CLIENT" env -u ALL_PROXY -u HTTPS_PROXY -u HTTP_PROXY -u NO_PROXY \
  curl -g -6 -v --max-time 10 'http://[2001:db8:20::2]:18080/direct-v6' \
  >"${ART}/transparent-direct-v6.txt" 2>"${ART}/transparent-direct-v6.stderr"
DIRECT6_RC=$?
set -e
DIRECT6="$(cat "${ART}/transparent-direct-v6.txt")"
cat "${ART}/transparent-direct-v6.stderr" >&2 || true
echo "$DIRECT6"
sudo ip6tables -t mangle -L PREROUTING -v -n -x | tee "${ART}/ip6tables-prerouting-after-direct-v6.txt"
if [[ "$DIRECT6_RC" -ne 0 ]] || ! grep -q 'domestic-v6' <<<"$DIRECT6"; then
  echo "IPv6 DIRECT transparent path failed rc=$DIRECT6_RC" >&2
  curl -fsS http://127.0.0.1:9090/connections | jq . > "${ART}/connections-after-v6-failure.json" 2>/dev/null || true
  exit 52
fi

FOREIGN6=""
for _ in $(seq 1 5); do
  if FOREIGN6="$(sudo ip netns exec "$NS_CLIENT" env -u ALL_PROXY -u HTTPS_PROXY -u HTTP_PROXY -u NO_PROXY curl -g -6 -fsS --max-time 10 'http://[2001:db8:20::3]:18080/foreign-v6' 2>/dev/null)"; then break; fi
  sleep 1
done
echo "$FOREIGN6" | tee "${ART}/transparent-smart-v6.txt"
grep -q 'foreign-v6' <<<"$FOREIGN6" || die "transparent Smart v6 failed"
grep -Eq 'peer=2001:db8:(30|31)::2' <<<"$FOREIGN6" || die "transparent Smart v6 did not traverse proxy namespace"

log "F: DNS listener/hosts"
DNS4="$(sudo ip netns exec "$NS_CLIENT" dig @10.10.0.1 -p 1053 foreign.test A +short | tail -n1)"
echo "$DNS4" | tee "${ART}/dns-a.txt"
[[ "$DNS4" == "10.20.0.3" ]] || die "DNS A path failed: $DNS4"

log "G: Smart UDP IPv4"
UDP4="$(sudo ip netns exec "$NS_CLIENT" python3 - <<'PY'
import socket
s=socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
s.settimeout(8)
s.sendto(b'udp-v4', ('10.20.0.3', 18081))
print(s.recvfrom(4096)[0].decode(), end='')
PY
)"
echo "$UDP4" | tee "${ART}/udp-v4.txt"
grep -q 'foreign-udp-v4' <<<"$UDP4" || die "Smart UDP v4 failed"
grep -Eq 'peer=10\.(30|31)\.0\.2' <<<"$UDP4" || die "Smart UDP v4 did not traverse proxy node"

log "H: Smart UDP IPv6"
UDP6="$(sudo ip netns exec "$NS_CLIENT" python3 - <<'PY'
import socket
s=socket.socket(socket.AF_INET6, socket.SOCK_DGRAM)
s.settimeout(8)
s.sendto(b'udp-v6', ('2001:db8:20::3', 18081))
print(s.recvfrom(4096)[0].decode(), end='')
PY
)"
echo "$UDP6" | tee "${ART}/udp-v6.txt"
grep -q 'foreign-udp-v6' <<<"$UDP6" || die "Smart UDP v6 failed"
grep -Eq 'peer=2001:db8:(30|31)::2' <<<"$UDP6" || die "Smart UDP v6 did not traverse proxy namespace"

log "I: inspect routing mark / TProxy counters"
sudo iptables -t mangle -L OUTPUT -v -n -x | tee "${ART}/iptables-mangle-output.txt"
sudo iptables -t mangle -L PREROUTING -v -n -x | tee "${ART}/iptables-mangle-prerouting.txt"
sudo ip6tables -t mangle -L PREROUTING -v -n -x | tee "${ART}/ip6tables-mangle-prerouting.txt"
sudo ip rule show | tee "${ART}/ip-rule.txt"
sudo ip -6 rule show | tee "${ART}/ip6-rule.txt"
sudo ip route show table 100 | tee "${ART}/route-table-100.txt"
sudo ip -6 route show table 100 | tee "${ART}/route6-table-100.txt"

MARKED_PKTS="$(sudo iptables -t mangle -L OUTPUT -v -n -x | awk '/10\.30\.0\.0\/24|10\.31\.0\.0\/24/ && /0x4000/ {sum += $1} END {print sum+0}')"
echo "$MARKED_PKTS" | tee "${ART}/routing-mark-packets.txt"
(( MARKED_PKTS > 0 )) || die "no proxy-node packets observed with routing-mark 0x4000"

TP4_PKTS="$(sudo iptables -t mangle -L PREROUTING -v -n -x | awk '/TPROXY/ && /10\.20\.0\.0\/24/ {sum += $1} END {print sum+0}')"
TP6_PKTS="$(sudo ip6tables -t mangle -L PREROUTING -v -n -x | awk '/TPROXY/ && /2001:db8:20::\/64/ {sum += $1} END {print sum+0}')"
printf 'ipv4=%s\nipv6=%s\n' "$TP4_PKTS" "$TP6_PKTS" | tee "${ART}/tproxy-counters.txt"
(( TP4_PKTS > 0 )) || die "IPv4 TProxy rule was not exercised"
(( TP6_PKTS > 0 )) || die "IPv6 TProxy rule was not exercised"

curl -fsS http://127.0.0.1:9090/proxies/SMART | jq . > "${ART}/smart-final.json" || true
sudo ip netns exec "$NS_CLIENT" ip route show > "${ART}/client-route-v4.txt"
sudo ip netns exec "$NS_CLIENT" ip -6 route show > "${ART}/client-route-v6.txt"

log "J: negative control: remove routing-mark; harness must fail the proxy path"
sudo kill "$MAIN_PID" 2>/dev/null || true
sleep 0.5
sed -i "s/^routing-mark: ${ROUTING_MARK_DEC}$/routing-mark: 0/" "$MAIN_CFG"
rm -rf "$MAIN_DIR"
mkdir -p "$MAIN_DIR"
sudo "$BIN" -d "$MAIN_DIR" -f "$MAIN_CFG" >"${ART}/mihomo-negative-control.log" 2>&1 &
NEG_PID=$!
echo "$NEG_PID" > "${STATE}/main.pid"
for _ in $(seq 1 50); do
  curl -fsS http://127.0.0.1:9090/version >/dev/null 2>&1 && break
  sleep 0.1
done

set +e
timeout 8s curl --noproxy '' -fsS --max-time 6 -x http://127.0.0.1:7890 http://10.20.0.3:18080/negative-control >"${ART}/negative-control-output.txt" 2>"${ART}/negative-control-error.txt"
NEG_RC=$?
set -e
if [[ "$NEG_RC" -eq 0 ]]; then
  die "negative control unexpectedly succeeded; self-bypass guard is not sensitive"
fi
echo "negative-control expected failure rc=${NEG_RC}" | tee "${ART}/negative-control-result.txt"

log "Routing simulation passed"
