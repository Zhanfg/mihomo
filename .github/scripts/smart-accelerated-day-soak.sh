#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BIN="${ROOT_DIR}/mihomo-soak-test"
STATE="${ROOT_DIR}/.soak-state"
ART="${ROOT_DIR}/.soak-artifacts"
MAIN_DIR="${STATE}/main"
PF_DIR="${STATE}/proxy-fast"
PS_DIR="${STATE}/proxy-slow"
PROVIDER="${MAIN_DIR}/daily-provider.yaml"
MAIN_CFG="${STATE}/main.yaml"
PF_CFG="${STATE}/proxy-fast.yaml"
PS_CFG="${STATE}/proxy-slow.yaml"

NS_CLIENT="mh-soak-client"
NS_WAN="mh-soak-wan"
NS_FAST="mh-soak-fast"
NS_SLOW="mh-soak-slow"

VC_H="mhsclh"
VC_N="mhscln"
VW_H="mhswnh"
VW_N="mhswnn"
VF_H="mhsfh"
VF_N="mhsfn"
VS_H="mhsslh"
VS_N="mhssln"
VF_W="mhsfw"
VW_F="mhswf"
VS_W="mhssw"
VW_S="mhsws"

TPROXY_PORT=19898
TPROXY_MARK=0x1
ROUTING_MARK_DEC=16384
ROUTING_MARK_HEX=0x4000
CTRL=19090
SOAK_SECONDS=60

rm -rf "$STATE" "$ART"
mkdir -p "$MAIN_DIR" "$PF_DIR" "$PS_DIR" "$ART"

log(){ printf '\n[%s] %s\n' "$(date -u +%H:%M:%S)" "$*"; }
die(){ echo "ERROR: $*" >&2; exit 1; }

kill_ns(){
  local ns="$1"
  sudo ip netns pids "$ns" 2>/dev/null | xargs -r sudo kill -TERM 2>/dev/null || true
  sleep 0.15
  sudo ip netns pids "$ns" 2>/dev/null | xargs -r sudo kill -KILL 2>/dev/null || true
}

cleanup(){
  set +e
  [[ -f "$STATE/main.pid" ]] && sudo kill "$(cat "$STATE/main.pid")" 2>/dev/null || true
  for ns in "$NS_CLIENT" "$NS_WAN" "$NS_FAST" "$NS_SLOW"; do kill_ns "$ns"; done
  sudo iptables -t mangle -D PREROUTING -i "$VC_H" -p tcp -d 10.92.0.0/24 -j TPROXY --on-port "$TPROXY_PORT" --tproxy-mark "$TPROXY_MARK/$TPROXY_MARK" 2>/dev/null || true
  sudo iptables -t mangle -D PREROUTING -i "$VC_H" -p udp -d 10.92.0.0/24 -j TPROXY --on-port "$TPROXY_PORT" --tproxy-mark "$TPROXY_MARK/$TPROXY_MARK" 2>/dev/null || true
  sudo ip6tables -t mangle -D PREROUTING -i "$VC_H" -p tcp -d fd92::/64 -j TPROXY --on-port "$TPROXY_PORT" --tproxy-mark "$TPROXY_MARK/$TPROXY_MARK" 2>/dev/null || true
  sudo ip6tables -t mangle -D PREROUTING -i "$VC_H" -p udp -d fd92::/64 -j TPROXY --on-port "$TPROXY_PORT" --tproxy-mark "$TPROXY_MARK/$TPROXY_MARK" 2>/dev/null || true
  sudo ip rule del fwmark "$TPROXY_MARK/$TPROXY_MARK" table 100 priority 100 2>/dev/null || true
  sudo ip -6 rule del fwmark "$TPROXY_MARK/$TPROXY_MARK" table 100 priority 100 2>/dev/null || true
  sudo ip route flush table 100 2>/dev/null || true
  sudo ip -6 route flush table 100 2>/dev/null || true
  for ns in "$NS_CLIENT" "$NS_WAN" "$NS_FAST" "$NS_SLOW"; do sudo ip netns del "$ns" 2>/dev/null || true; done
}
trap cleanup EXIT

for c in ip iptables ip6tables tc curl jq python3; do command -v "$c" >/dev/null || die "missing $c"; done
[[ -x "$BIN" ]] || die "missing $BIN"

sudo modprobe xt_TPROXY 2>/dev/null || true
sudo modprobe nf_tproxy_ipv4 2>/dev/null || true
sudo modprobe nf_tproxy_ipv6 2>/dev/null || true
sudo sysctl -w net.ipv4.ip_forward=1 >/dev/null
sudo sysctl -w net.ipv6.conf.all.forwarding=1 >/dev/null
sudo sysctl -w net.ipv4.conf.all.rp_filter=0 >/dev/null || true

log "Create accelerated-day namespace topology"
for ns in "$NS_CLIENT" "$NS_WAN" "$NS_FAST" "$NS_SLOW"; do
  sudo ip netns add "$ns"
  sudo ip -n "$ns" link set lo up
done

sudo ip link add "$VC_H" type veth peer name "$VC_N"
sudo ip link set "$VC_N" netns "$NS_CLIENT"
sudo ip addr add 10.91.0.1/24 dev "$VC_H"
sudo ip -6 addr add fd91::1/64 dev "$VC_H" nodad
sudo ip link set "$VC_H" up
sudo ip -n "$NS_CLIENT" addr add 10.91.0.2/24 dev "$VC_N"
sudo ip -n "$NS_CLIENT" -6 addr add fd91::2/64 dev "$VC_N" nodad
sudo ip -n "$NS_CLIENT" link set "$VC_N" up
sudo ip -n "$NS_CLIENT" route add default via 10.91.0.1
sudo ip -n "$NS_CLIENT" -6 route add default via fd91::1

sudo ip link add "$VW_H" type veth peer name "$VW_N"
sudo ip link set "$VW_N" netns "$NS_WAN"
sudo ip addr add 10.92.0.1/24 dev "$VW_H"
sudo ip -6 addr add fd92::1/64 dev "$VW_H" nodad
sudo ip link set "$VW_H" up
sudo ip -n "$NS_WAN" addr add 10.92.0.2/24 dev "$VW_N"
sudo ip -n "$NS_WAN" -6 addr add fd92::2/64 dev "$VW_N" nodad
for last in 10 11 12 13 14; do sudo ip -n "$NS_WAN" addr add "10.92.0.$last/24" dev "$VW_N"; done
for last in 10 11 12 13 14; do sudo ip -n "$NS_WAN" -6 addr add "fd92::$last/64" dev "$VW_N" nodad; done
sudo ip -n "$NS_WAN" link set "$VW_N" up
sudo ip -n "$NS_WAN" route add default via 10.92.0.1
sudo ip -n "$NS_WAN" -6 route add default via fd92::1

sudo ip link add "$VF_H" type veth peer name "$VF_N"
sudo ip link set "$VF_N" netns "$NS_FAST"
sudo ip addr add 10.93.0.1/24 dev "$VF_H"
sudo ip -6 addr add fd93::1/64 dev "$VF_H" nodad
sudo ip link set "$VF_H" up
sudo ip -n "$NS_FAST" addr add 10.93.0.2/24 dev "$VF_N"
sudo ip -n "$NS_FAST" -6 addr add fd93::2/64 dev "$VF_N" nodad
sudo ip -n "$NS_FAST" link set "$VF_N" up
sudo ip -n "$NS_FAST" route add default via 10.93.0.1
sudo ip -n "$NS_FAST" -6 route add default via fd93::1

sudo ip link add "$VS_H" type veth peer name "$VS_N"
sudo ip link set "$VS_N" netns "$NS_SLOW"
sudo ip addr add 10.94.0.1/24 dev "$VS_H"
sudo ip -6 addr add fd94::1/64 dev "$VS_H" nodad
sudo ip link set "$VS_H" up
sudo ip -n "$NS_SLOW" addr add 10.94.0.2/24 dev "$VS_N"
sudo ip -n "$NS_SLOW" -6 addr add fd94::2/64 dev "$VS_N" nodad
sudo ip -n "$NS_SLOW" link set "$VS_N" up
sudo ip -n "$NS_SLOW" route add default via 10.94.0.1
sudo ip -n "$NS_SLOW" -6 route add default via fd94::1

# Dedicated proxy->WAN transit links keep soak results independent of hosted-runner FORWARD policy.
sudo ip link add "$VF_W" type veth peer name "$VW_F"
sudo ip link set "$VF_W" netns "$NS_FAST"
sudo ip link set "$VW_F" netns "$NS_WAN"
sudo ip -n "$NS_FAST" addr add 10.95.0.1/30 dev "$VF_W"
sudo ip -n "$NS_FAST" -6 addr add fd95::1/64 dev "$VF_W" nodad
sudo ip -n "$NS_WAN" addr add 10.95.0.2/30 dev "$VW_F"
sudo ip -n "$NS_WAN" -6 addr add fd95::2/64 dev "$VW_F" nodad
sudo ip -n "$NS_FAST" link set "$VF_W" up
sudo ip -n "$NS_WAN" link set "$VW_F" up
sudo ip -n "$NS_FAST" route replace 10.92.0.0/24 via 10.95.0.2 dev "$VF_W" src 10.93.0.2
sudo ip -n "$NS_FAST" -6 route replace fd92::/64 via fd95::2 dev "$VF_W" src fd93::2
sudo ip -n "$NS_WAN" route replace 10.93.0.0/24 via 10.95.0.1 dev "$VW_F"
sudo ip -n "$NS_WAN" -6 route replace fd93::/64 via fd95::1 dev "$VW_F"

sudo ip link add "$VS_W" type veth peer name "$VW_S"
sudo ip link set "$VS_W" netns "$NS_SLOW"
sudo ip link set "$VW_S" netns "$NS_WAN"
sudo ip -n "$NS_SLOW" addr add 10.96.0.1/30 dev "$VS_W"
sudo ip -n "$NS_SLOW" -6 addr add fd96::1/64 dev "$VS_W" nodad
sudo ip -n "$NS_WAN" addr add 10.96.0.2/30 dev "$VW_S"
sudo ip -n "$NS_WAN" -6 addr add fd96::2/64 dev "$VW_S" nodad
sudo ip -n "$NS_SLOW" link set "$VS_W" up
sudo ip -n "$NS_WAN" link set "$VW_S" up
sudo ip -n "$NS_SLOW" route replace 10.92.0.0/24 via 10.96.0.2 dev "$VS_W" src 10.94.0.2
sudo ip -n "$NS_SLOW" -6 route replace fd92::/64 via fd96::2 dev "$VS_W" src fd94::2
sudo ip -n "$NS_WAN" route replace 10.94.0.0/24 via 10.96.0.1 dev "$VW_S"
sudo ip -n "$NS_WAN" -6 route replace fd94::/64 via fd96::1 dev "$VW_S"

sudo iptables -I FORWARD 1 -j ACCEPT
sudo ip6tables -I FORWARD 1 -j ACCEPT
sudo tc qdisc add dev "$VF_H" root netem delay 7ms 1ms
sudo tc qdisc add dev "$VS_H" root netem delay 65ms 8ms

cat > "$STATE/server.py" <<'PY'
import socket, threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

class V6(ThreadingHTTPServer):
    address_family = socket.AF_INET6
    def server_bind(self):
        self.socket.setsockopt(socket.IPPROTO_IPV6, socket.IPV6_V6ONLY, 1)
        super().server_bind()

class H(BaseHTTPRequestHandler):
    def do_GET(self):
        p=self.path
        size=4096
        if "stream" in p: size=524288
        elif "static" in p: size=131072
        elif "google" in p: size=16384
        elif "ai" in p: size=8192
        elif "social" in p: size=4096
        body=(f"peer={self.client_address[0]} local={self.server.server_address[0]} path={p}\n".encode())
        if len(body) < size: body += b"x"*(size-len(body))
        self.send_response(200)
        self.send_header("Content-Length",str(len(body)))
        self.end_headers()
        self.wfile.write(body)
    def log_message(self,*a): pass

def udp(bind, label):
    fam=socket.AF_INET6 if ":" in bind else socket.AF_INET
    s=socket.socket(fam,socket.SOCK_DGRAM)
    if fam == socket.AF_INET6:
        s.setsockopt(socket.IPPROTO_IPV6, socket.IPV6_V6ONLY, 1)
    s.bind((bind,18081))
    while True:
        data,peer=s.recvfrom(65535)
        payload=(f"{label} peer={peer[0]} ".encode()+data)
        s.sendto(payload,peer)

threading.Thread(target=lambda: ThreadingHTTPServer(("0.0.0.0",18080),H).serve_forever(),daemon=True).start()
threading.Thread(target=lambda: udp("0.0.0.0","udp4"),daemon=True).start()
threading.Thread(target=lambda: udp("::","udp6"),daemon=True).start()
V6(("::",18080),H).serve_forever()
PY
sudo ip netns exec "$NS_WAN" python3 "$STATE/server.py" >"$ART/wan-server.log" 2>&1 &
for _ in $(seq 1 50); do
  sudo ip netns exec "$NS_WAN" ss -lnt | grep -q ':18080' && break
  sleep 0.1
done

cat > "$PF_CFG" <<'YAML'
socks-port: 1080
allow-lan: true
bind-address: '*'
ipv6: true
mode: rule
log-level: warning
rules:
  - MATCH,DIRECT
YAML
cp "$PF_CFG" "$PS_CFG"

start_fast(){
  sudo ip netns exec "$NS_FAST" "$BIN" -d "$PF_DIR" -f "$PF_CFG" >>"$ART/proxy-fast.log" 2>&1 &
  for _ in $(seq 1 40); do sudo ip netns exec "$NS_FAST" ss -lnt | grep -q ':1080 ' && return 0; sleep 0.1; done
  return 1
}
start_slow(){
  sudo ip netns exec "$NS_SLOW" "$BIN" -d "$PS_DIR" -f "$PS_CFG" >>"$ART/proxy-slow.log" 2>&1 &
  for _ in $(seq 1 40); do sudo ip netns exec "$NS_SLOW" ss -lnt | grep -q ':1080 ' && return 0; sleep 0.1; done
  return 1
}
start_fast || die "fast proxy failed"
start_slow || die "slow proxy failed"

cat > "$PROVIDER" <<'YAML'
proxies:
  - {name: US-fast, type: socks5, server: 10.93.0.2, port: 1080, udp: true}
  - {name: JP-slow, type: socks5, server: 10.94.0.2, port: 1080, udp: true}
YAML

sudo ip rule add fwmark "$TPROXY_MARK/$TPROXY_MARK" table 100 priority 100
sudo ip route add local 0.0.0.0/0 dev lo table 100
sudo ip -6 rule add fwmark "$TPROXY_MARK/$TPROXY_MARK" table 100 priority 100
sudo ip -6 route add local ::/0 dev lo table 100
sudo iptables -t mangle -A PREROUTING -i "$VC_H" -p tcp -d 10.92.0.0/24 -j TPROXY --on-port "$TPROXY_PORT" --tproxy-mark "$TPROXY_MARK/$TPROXY_MARK"
sudo iptables -t mangle -A PREROUTING -i "$VC_H" -p udp -d 10.92.0.0/24 -j TPROXY --on-port "$TPROXY_PORT" --tproxy-mark "$TPROXY_MARK/$TPROXY_MARK"
sudo ip6tables -t mangle -A PREROUTING -i "$VC_H" -p tcp -d fd92::/64 -j TPROXY --on-port "$TPROXY_PORT" --tproxy-mark "$TPROXY_MARK/$TPROXY_MARK"
sudo ip6tables -t mangle -A PREROUTING -i "$VC_H" -p udp -d fd92::/64 -j TPROXY --on-port "$TPROXY_PORT" --tproxy-mark "$TPROXY_MARK/$TPROXY_MARK"

cat > "$MAIN_CFG" <<YAML
mixed-port: 17890
tproxy-port: $TPROXY_PORT
external-controller: 127.0.0.1:$CTRL
allow-lan: true
bind-address: '*'
mode: rule
log-level: warning
ipv6: true
tcp-concurrent: true
routing-mark: $ROUTING_MARK_DEC
hosts:
  invalid.test: 10.92.0.14
dns:
  enable: true
  listen: 0.0.0.0:1053
  ipv6: true
  use-hosts: true
  enhanced-mode: redir-host
  nameserver:
    - system
proxy-providers:
  daily:
    type: file
    path: $PROVIDER
proxy-groups:
  - {name: MAIN, type: smart, use: [daily], url: http://10.92.0.10:18080/main, interval: 30, lazy: true, timeout: 800, tolerance: 50, uselightgbm: false, collectdata: false, prefer-asn: false}
  - {name: AI, type: smart, use: [daily], url: http://10.92.0.10:18080/ai, interval: 30, lazy: true, timeout: 800, tolerance: 60, policy-priority: '(?i:US):1.30;(?i:JP):0.90', uselightgbm: false, collectdata: false, prefer-asn: false}
  - {name: GOOGLE, type: smart, use: [daily], url: http://10.92.0.11:18080/google, interval: 30, lazy: true, timeout: 800, tolerance: 60, policy-priority: '(?i:US):1.20;(?i:JP):1.00', uselightgbm: false, collectdata: false, prefer-asn: false}
  - {name: SOCIAL, type: smart, use: [daily], url: http://10.92.0.12:18080/social, interval: 30, lazy: true, timeout: 800, tolerance: 80, policy-priority: '(?i:JP):1.10;(?i:US):1.00', uselightgbm: false, collectdata: false, prefer-asn: false}
  - {name: STREAM, type: smart, use: [daily], url: http://10.92.0.13:18080/stream, interval: 30, lazy: true, timeout: 800, tolerance: 100, uselightgbm: false, collectdata: false, prefer-asn: false}
rules:
  - IP-CIDR,10.92.0.2/32,DIRECT,no-resolve
  - IP-CIDR,10.92.0.10/32,AI,no-resolve
  - IP-CIDR,10.92.0.11/32,GOOGLE,no-resolve
  - IP-CIDR,10.92.0.12/32,SOCIAL,no-resolve
  - IP-CIDR,10.92.0.13/32,STREAM,no-resolve
  - IP-CIDR,10.92.0.14/32,MAIN,no-resolve
  - IP-CIDR6,fd92::2/128,DIRECT,no-resolve
  - IP-CIDR6,fd92::10/128,AI,no-resolve
  - IP-CIDR6,fd92::11/128,GOOGLE,no-resolve
  - IP-CIDR6,fd92::12/128,SOCIAL,no-resolve
  - IP-CIDR6,fd92::13/128,STREAM,no-resolve
  - IP-CIDR6,fd92::14/128,MAIN,no-resolve
  - MATCH,MAIN
YAML

"$BIN" -t -d "$MAIN_DIR" -f "$MAIN_CFG" >"$ART/config-test.log" 2>&1
sudo "$BIN" -d "$MAIN_DIR" -f "$MAIN_CFG" >"$ART/mihomo-main.log" 2>&1 &
MAIN_PID=$!
echo "$MAIN_PID" > "$STATE/main.pid"
for _ in $(seq 1 80); do curl -fsS "http://127.0.0.1:$CTRL/version" >/dev/null 2>&1 && break; sleep 0.1; done
curl -fsS "http://127.0.0.1:$CTRL/version" | tee "$ART/version.json"

sample_metrics(){
  local tag="$1"
  local rss fd th conns
  rss="$(awk '/VmRSS:/ {print $2}' /proc/$MAIN_PID/status 2>/dev/null || echo 0)"
  fd="$(find /proc/$MAIN_PID/fd -maxdepth 1 -type l 2>/dev/null | wc -l)"
  th="$(awk '/Threads:/ {print $2}' /proc/$MAIN_PID/status 2>/dev/null || echo 0)"
  conns="$(curl -fsS "http://127.0.0.1:$CTRL/connections" 2>/dev/null | jq '.connections|length' 2>/dev/null || echo 0)"
  printf '%s,%s,%s,%s,%s\n' "$tag" "$rss" "$fd" "$th" "$conns" >> "$ART/metrics.csv"
}
echo "tag,rss_kb,fd,threads,connections" > "$ART/metrics.csv"
sample_metrics start
BASE_RSS="$(tail -n1 "$ART/metrics.csv" | cut -d, -f2)"
BASE_FD="$(tail -n1 "$ART/metrics.csv" | cut -d, -f3)"
BASE_TH="$(tail -n1 "$ART/metrics.csv" | cut -d, -f4)"

cat > "$STATE/load.py" <<'PY'
import concurrent.futures, socket, sys, time, random, json
hour=int(sys.argv[1]); count=int(sys.argv[2])
random.seed(20260926 + hour)
services=[
 ("direct","10.92.0.2","fd92::2",4096,18),
 ("ai","10.92.0.10","fd92::10",8192,18),
 ("google","10.92.0.11","fd92::11",16384,17),
 ("social","10.92.0.12","fd92::12",4096,17),
 ("stream","10.92.0.13","fd92::13",524288,15),
 ("static","10.92.0.14","fd92::14",131072,15),
]
weighted=[]
for x in services: weighted += [x]*x[4]

def http_one(i):
    name,v4,v6,expect,_=random.choice(weighted)
    ipv6=(i%5==0)
    addr=v6 if ipv6 else v4
    fam=socket.AF_INET6 if ipv6 else socket.AF_INET
    path=f"/{name}/h{hour:02d}/{i}"
    start=time.monotonic()
    try:
        s=socket.socket(fam,socket.SOCK_STREAM); s.settimeout(4)
        s.connect((addr,18080))
        req=f"GET {path} HTTP/1.1\r\nHost: {name}.test\r\nConnection: close\r\n\r\n".encode()
        s.sendall(req)
        data=b""
        while True:
            b=s.recv(65536)
            if not b: break
            data+=b
        s.close()
        ok=b"200 OK" in data and len(data)>=expect
        return ok,len(data),name,ipv6,time.monotonic()-start
    except Exception:
        return False,0,name,ipv6,time.monotonic()-start

def udp_one(i):
    ipv6=(i%4==0); addr="fd92::13" if ipv6 else "10.92.0.13"
    fam=socket.AF_INET6 if ipv6 else socket.AF_INET
    start=time.monotonic()
    try:
        s=socket.socket(fam,socket.SOCK_DGRAM); s.settimeout(3)
        payload=("u"*1024).encode()
        s.sendto(payload,(addr,18081)); data,_=s.recvfrom(4096); s.close()
        return len(data)>=1024,len(data),"udp",ipv6,time.monotonic()-start
    except Exception:
        return False,0,"udp",ipv6,time.monotonic()-start

jobs=[]
for i in range(count):
    jobs.append(("udp" if i%5==0 else "http",i))
res=[]
workers=min(128, max(32, count))
with concurrent.futures.ThreadPoolExecutor(max_workers=workers) as ex:
    futs=[ex.submit(udp_one if k=="udp" else http_one,i) for k,i in jobs]
    for f in concurrent.futures.as_completed(futs): res.append(f.result())
ok=sum(1 for r in res if r[0]); total=len(res); byt=sum(r[1] for r in res)
lat=[r[4] for r in res if r[0]]
lat.sort()
p95=lat[min(len(lat)-1,int(len(lat)*.95))] if lat else 9
print(json.dumps({"hour":hour,"ok":ok,"total":total,"bytes":byt,"p95":p95}))
PY

log "Run 24 virtual hours in about 60 seconds"
START_NS="$(date +%s%N)"
TOTAL_OK=0
TOTAL_REQ=0
TOTAL_BYTES=0
: > "$ART/hourly.jsonl"

for hour in $(seq 0 23); do
  case "$hour" in
    0|1|2|3|4|5) N=48 ;;
    6|7|8) N=80 ;;
    9|10|11) N=120 ;;
    12|13) N=96 ;;
    14|15|16|17) N=128 ;;
    18|19|20|21|22) N=176 ;;
    23) N=72 ;;
  esac

  case "$hour" in
    6)
      log "virtual 06:00 provider hot-update: add dead candidate"
      cat > "$PROVIDER" <<'YAML'
proxies:
  - {name: US-fast, type: socks5, server: 10.93.0.2, port: 1080, udp: true}
  - {name: JP-slow, type: socks5, server: 10.94.0.2, port: 1080, udp: true}
  - {name: DEAD-candidate, type: socks5, server: 10.93.0.2, port: 1099, udp: true}
YAML
      curl -fsS -X PUT "http://127.0.0.1:$CTRL/providers/proxies/daily" -o /dev/null || die "provider hot-update failed"
      ;;
    9)
      log "virtual 09:00 DNS burst"
      rm -f "$ART/dns-burst.ok"
      : > "$ART/dns-burst.ok"
      dns_pids=()
      for i in $(seq 1 64); do
        (
          ans="$(sudo ip netns exec "$NS_CLIENT" dig @10.91.0.1 -p 1053 invalid.test A +short +time=1 +tries=1 | tail -n1)"
          [[ "$ans" == "10.92.0.14" ]] && echo "$i" >> "$ART/dns-burst.ok"
        ) &
        dns_pids+=("$!")
      done
      for pid in "${dns_pids[@]}"; do wait "$pid" || true; done
      DNS_BURST_OK="$(wc -l < "$ART/dns-burst.ok")"
      echo "dns_burst=$DNS_BURST_OK/64" | tee "$ART/dns-burst-summary.txt"
      (( DNS_BURST_OK >= 63 )) || die "DNS burst success below 63/64"
      ;;
    12)
      log "virtual 12:00 remove dead candidate"
      cat > "$PROVIDER" <<'YAML'
proxies:
  - {name: US-fast, type: socks5, server: 10.93.0.2, port: 1080, udp: true}
  - {name: JP-slow, type: socks5, server: 10.94.0.2, port: 1080, udp: true}
YAML
      curl -fsS -X PUT "http://127.0.0.1:$CTRL/providers/proxies/daily" -o /dev/null || die "provider recovery update failed"
      ;;
    14)
      log "virtual 14:00 inject severe jitter"
      sudo tc qdisc change dev "$VF_H" root netem delay 120ms 35ms loss 0.5%
      ;;
    16)
      log "virtual 16:00 restore normal fast-link RTT"
      sudo tc qdisc change dev "$VF_H" root netem delay 7ms 1ms
      ;;
    18)
      log "virtual 18:00 peak-hour fast node outage"
      kill_ns "$NS_FAST"
      ;;
    20)
      log "virtual 20:00 fast node recovery"
      start_fast || die "fast proxy did not recover"
      ;;
    22)
      log "virtual 22:00 microburst"
      N=256
      ;;
  esac

  OUT="$(sudo ip netns exec "$NS_CLIENT" python3 "$STATE/load.py" "$hour" "$N")"
  echo "$OUT" | tee -a "$ART/hourly.jsonl"
  OK="$(jq -r .ok <<<"$OUT")"; REQ="$(jq -r .total <<<"$OUT")"; BY="$(jq -r .bytes <<<"$OUT")"
  TOTAL_OK=$((TOTAL_OK+OK)); TOTAL_REQ=$((TOTAL_REQ+REQ)); TOTAL_BYTES=$((TOTAL_BYTES+BY))
  sample_metrics "h$hour"

  TARGET_NS=$(( START_NS + (hour+1)*SOAK_SECONDS*1000000000/24 ))
  NOW_NS="$(date +%s%N)"
  if (( NOW_NS < TARGET_NS )); then
    SLEEP_NS=$((TARGET_NS-NOW_NS))
    python3 - "$SLEEP_NS" <<'PY'
import sys,time
time.sleep(int(sys.argv[1])/1e9)
PY
  fi
done

log "Extreme connection-table pressure: 256 simultaneous Smart sessions"
cat > "$STATE/hold_connections.py" <<'PY'
import socket,time,pathlib
socks=[]
for i in range(256):
    try:
        s=socket.socket(socket.AF_INET,socket.SOCK_STREAM)
        s.settimeout(4)
        s.connect(("10.92.0.13",18080))
        s.sendall(f"GET /hold/{i} HTTP/1.1\r\nHost: stream.test\r\n".encode())
        socks.append(s)
    except Exception:
        pass
pathlib.Path("/tmp/mh-soak-hold-ready").write_text(str(len(socks)))
time.sleep(2.5)
for s in socks:
    try: s.close()
    except Exception: pass
PY
sudo rm -f /tmp/mh-soak-hold-ready
sudo ip netns exec "$NS_CLIENT" python3 "$STATE/hold_connections.py" >"$ART/hold-connections.log" 2>&1 &
HOLD_PID=$!
for _ in $(seq 1 50); do
  [[ -f /tmp/mh-soak-hold-ready ]] && break
  sleep 0.05
done
HOLD_OPEN="$(sudo cat /tmp/mh-soak-hold-ready 2>/dev/null || echo 0)"
sample_metrics hold_peak
HOLD_API="$(tail -n1 "$ART/metrics.csv" | cut -d, -f5)"
printf 'opened=%s\napi_connections=%s\n' "$HOLD_OPEN" "$HOLD_API" | tee "$ART/hold-summary.txt"
(( HOLD_OPEN >= 220 )) || die "connection pressure fixture opened fewer than 220/256 sessions"
(( HOLD_API >= 180 )) || die "Mihomo connection table did not observe enough concurrent sessions"
wait "$HOLD_PID" || true

log "Post-day quiesce and leak checks"
sleep 2
sample_metrics end
END_RSS="$(tail -n1 "$ART/metrics.csv" | cut -d, -f2)"
END_FD="$(tail -n1 "$ART/metrics.csv" | cut -d, -f3)"
END_TH="$(tail -n1 "$ART/metrics.csv" | cut -d, -f4)"
END_CONN="$(tail -n1 "$ART/metrics.csv" | cut -d, -f5)"
RSS_DELTA=$((END_RSS-BASE_RSS))
FD_DELTA=$((END_FD-BASE_FD))
TH_DELTA=$((END_TH-BASE_TH))
SUCCESS_BP=$((TOTAL_OK*10000/TOTAL_REQ))

cat > "$ART/summary.txt" <<EOF
virtual_hours=24
target_wall_seconds=$SOAK_SECONDS
requests=$TOTAL_REQ
success=$TOTAL_OK
success_basis_points=$SUCCESS_BP
bytes=$TOTAL_BYTES
base_rss_kb=$BASE_RSS
end_rss_kb=$END_RSS
rss_delta_kb=$RSS_DELTA
base_fd=$BASE_FD
end_fd=$END_FD
fd_delta=$FD_DELTA
base_threads=$BASE_TH
end_threads=$END_TH
thread_delta=$TH_DELTA
end_connections=$END_CONN
EOF
cat "$ART/summary.txt"

kill -0 "$MAIN_PID" 2>/dev/null || die "Mihomo died during accelerated day"
(( SUCCESS_BP >= 9700 )) || die "accelerated-day success rate below 97%"
(( RSS_DELTA <= 131072 )) || die "RSS retained >128MiB after quiesce"
(( FD_DELTA <= 64 )) || die "file descriptor leak >64"
(( TH_DELTA <= 32 )) || die "thread growth >32"
(( END_CONN <= 8 )) || die "too many residual connections after quiesce"

sudo iptables -t mangle -L PREROUTING -v -n -x > "$ART/iptables-prerouting.txt"
sudo ip6tables -t mangle -L PREROUTING -v -n -x > "$ART/ip6tables-prerouting.txt"
curl -fsS "http://127.0.0.1:$CTRL/proxies" > "$ART/proxies-final.json"
echo ACCELERATED_DAY_PASS | tee "$ART/result.txt"
