#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BIN="${ROOT_DIR}/mihomo-week-soak-test"
STATE="${ROOT_DIR}/.week-soak-state"
ART="${ROOT_DIR}/.week-soak-artifacts"
MAIN_DIR="${STATE}/main"
FAST_DIR="${STATE}/fast"
SLOW_DIR="${STATE}/slow"
PROVIDER="${STATE}/week-provider.yaml"
CFG="${STATE}/week.yaml"
FAST_CFG="${STATE}/fast.yaml"
SLOW_CFG="${STATE}/slow.yaml"

NS_CLIENT="mh-wk-client"
NS_WAN="mh-wk-wan"
NS_FAST="mh-wk-fast"
NS_SLOW="mh-wk-slow"

VC_H="mhwkclh"
VC_N="mhwkcln"
VW_H="mhwkwnh"
VW_N="mhwkwnn"
VF_H="mhwkfh"
VF_N="mhwkfn"
VS_H="mhwkslh"
VS_N="mhwksln"

TPROXY_PORT=29898
TPROXY_MARK=0x1
ROUTING_MARK_DEC=16384
CTRL=29090
DNS_PORT=21053
VIRTUAL_DAYS=7
VIRTUAL_HOURS=168
SOAK_SECONDS=210

rm -rf "$STATE" "$ART"
mkdir -p "$MAIN_DIR" "$FAST_DIR" "$SLOW_DIR" "$ART"

log(){ printf '\n[%s] %s\n' "$(date -u +%H:%M:%S)" "$*"; }
die(){ echo "ERROR: $*" >&2; exit 1; }

kill_ns_pids(){
  local ns="$1"
  sudo ip netns pids "$ns" 2>/dev/null | xargs -r sudo kill -TERM 2>/dev/null || true
  sleep 0.12
  sudo ip netns pids "$ns" 2>/dev/null | xargs -r sudo kill -KILL 2>/dev/null || true
}

cleanup(){
  set +e
  [[ -f "$STATE/main.pid" ]] && sudo kill "$(cat "$STATE/main.pid")" 2>/dev/null || true
  for ns in "$NS_CLIENT" "$NS_WAN" "$NS_FAST" "$NS_SLOW"; do kill_ns_pids "$ns"; done
  sudo iptables -t mangle -D PREROUTING -i "$VC_H" -p tcp -d 10.102.0.0/24 -j TPROXY --on-port "$TPROXY_PORT" --tproxy-mark "$TPROXY_MARK/$TPROXY_MARK" 2>/dev/null || true
  sudo iptables -t mangle -D PREROUTING -i "$VC_H" -p udp -d 10.102.0.0/24 -j TPROXY --on-port "$TPROXY_PORT" --tproxy-mark "$TPROXY_MARK/$TPROXY_MARK" 2>/dev/null || true
  sudo ip6tables -t mangle -D PREROUTING -i "$VC_H" -p tcp -d fd102::/64 -j TPROXY --on-port "$TPROXY_PORT" --tproxy-mark "$TPROXY_MARK/$TPROXY_MARK" 2>/dev/null || true
  sudo ip6tables -t mangle -D PREROUTING -i "$VC_H" -p udp -d fd102::/64 -j TPROXY --on-port "$TPROXY_PORT" --tproxy-mark "$TPROXY_MARK/$TPROXY_MARK" 2>/dev/null || true
  sudo ip rule del fwmark "$TPROXY_MARK/$TPROXY_MARK" table 100 priority 100 2>/dev/null || true
  sudo ip -6 rule del fwmark "$TPROXY_MARK/$TPROXY_MARK" table 100 priority 100 2>/dev/null || true
  sudo ip route flush table 100 2>/dev/null || true
  sudo ip -6 route flush table 100 2>/dev/null || true
  for ns in "$NS_CLIENT" "$NS_WAN" "$NS_FAST" "$NS_SLOW"; do sudo ip netns del "$ns" 2>/dev/null || true; done
}
trap cleanup EXIT

for c in ip iptables ip6tables tc curl jq dig python3; do command -v "$c" >/dev/null || die "missing command: $c"; done
[[ -x "$BIN" ]] || die "missing $BIN"

sudo modprobe xt_TPROXY 2>/dev/null || true
sudo modprobe nf_tproxy_ipv4 2>/dev/null || true
sudo modprobe nf_tproxy_ipv6 2>/dev/null || true
sudo sysctl -w net.ipv4.ip_forward=1 >/dev/null
sudo sysctl -w net.ipv6.conf.all.forwarding=1 >/dev/null
sudo sysctl -w net.ipv4.conf.all.rp_filter=0 >/dev/null || true

log "Create seven-day aging topology"
for ns in "$NS_CLIENT" "$NS_WAN" "$NS_FAST" "$NS_SLOW"; do
  sudo ip netns add "$ns"
  sudo ip -n "$ns" link set lo up
done

sudo ip link add "$VC_H" type veth peer name "$VC_N"
sudo ip link set "$VC_N" netns "$NS_CLIENT"
sudo ip addr add 10.101.0.1/24 dev "$VC_H"
sudo ip -6 addr add fd101::1/64 dev "$VC_H" nodad
sudo ip link set "$VC_H" up
sudo ip -n "$NS_CLIENT" addr add 10.101.0.2/24 dev "$VC_N"
sudo ip -n "$NS_CLIENT" -6 addr add fd101::2/64 dev "$VC_N" nodad
sudo ip -n "$NS_CLIENT" link set "$VC_N" up
sudo ip -n "$NS_CLIENT" route add default via 10.101.0.1
sudo ip -n "$NS_CLIENT" -6 route add default via fd101::1

sudo ip link add "$VW_H" type veth peer name "$VW_N"
sudo ip link set "$VW_N" netns "$NS_WAN"
sudo ip addr add 10.102.0.1/24 dev "$VW_H"
sudo ip -6 addr add fd102::1/64 dev "$VW_H" nodad
sudo ip link set "$VW_H" up
sudo ip -n "$NS_WAN" addr add 10.102.0.2/24 dev "$VW_N"
sudo ip -n "$NS_WAN" -6 addr add fd102::2/64 dev "$VW_N" nodad
for last in 10 11 12 13 14 15; do sudo ip -n "$NS_WAN" addr add "10.102.0.$last/24" dev "$VW_N"; done
for last in 10 11 12 13 14 15; do sudo ip -n "$NS_WAN" -6 addr add "fd102::$last/64" dev "$VW_N" nodad; done
sudo ip -n "$NS_WAN" link set "$VW_N" up
sudo ip -n "$NS_WAN" route add default via 10.102.0.1
sudo ip -n "$NS_WAN" -6 route add default via fd102::1

sudo ip link add "$VF_H" type veth peer name "$VF_N"
sudo ip link set "$VF_N" netns "$NS_FAST"
sudo ip addr add 10.103.0.1/24 dev "$VF_H"
sudo ip -6 addr add fd103::1/64 dev "$VF_H" nodad
sudo ip link set "$VF_H" up
sudo ip -n "$NS_FAST" addr add 10.103.0.2/24 dev "$VF_N"
sudo ip -n "$NS_FAST" -6 addr add fd103::2/64 dev "$VF_N" nodad
sudo ip -n "$NS_FAST" link set "$VF_N" up
sudo ip -n "$NS_FAST" route add default via 10.103.0.1
sudo ip -n "$NS_FAST" -6 route add default via fd103::1

sudo ip link add "$VS_H" type veth peer name "$VS_N"
sudo ip link set "$VS_N" netns "$NS_SLOW"
sudo ip addr add 10.104.0.1/24 dev "$VS_H"
sudo ip -6 addr add fd104::1/64 dev "$VS_H" nodad
sudo ip link set "$VS_H" up
sudo ip -n "$NS_SLOW" addr add 10.104.0.2/24 dev "$VS_N"
sudo ip -n "$NS_SLOW" -6 addr add fd104::2/64 dev "$VS_N" nodad
sudo ip -n "$NS_SLOW" link set "$VS_N" up
sudo ip -n "$NS_SLOW" route add default via 10.104.0.1
sudo ip -n "$NS_SLOW" -6 route add default via fd104::1

sudo iptables -I FORWARD 1 -j ACCEPT
sudo ip6tables -I FORWARD 1 -j ACCEPT
sudo tc qdisc add dev "$VF_H" root netem delay 6ms 1ms
sudo tc qdisc add dev "$VS_H" root netem delay 70ms 10ms

cat > "$STATE/server.py" <<'PY'
import socket, threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

class V6(ThreadingHTTPServer):
    address_family=socket.AF_INET6
    def server_bind(self):
        self.socket.setsockopt(socket.IPPROTO_IPV6, socket.IPV6_V6ONLY, 1)
        super().server_bind()

class H(BaseHTTPRequestHandler):
    def do_GET(self):
        p=self.path
        size=4096
        if "stream" in p: size=262144
        elif "static" in p: size=65536
        elif "download" in p: size=1048576
        body=f"peer={self.client_address[0]} local={self.server.server_address[0]} path={p}\n".encode()
        if len(body)<size: body += b"x"*(size-len(body))
        self.send_response(200)
        self.send_header("Content-Length",str(len(body)))
        self.end_headers()
        self.wfile.write(body)
    def log_message(self,*a): pass

def udp(bind,label):
    fam=socket.AF_INET6 if ":" in bind else socket.AF_INET
    s=socket.socket(fam,socket.SOCK_DGRAM)
    if fam==socket.AF_INET6:
        s.setsockopt(socket.IPPROTO_IPV6,socket.IPV6_V6ONLY,1)
    s.bind((bind,18081))
    while True:
        d,p=s.recvfrom(65535)
        s.sendto((label+" ").encode()+d,p)

threading.Thread(target=lambda: ThreadingHTTPServer(("0.0.0.0",18080),H).serve_forever(),daemon=True).start()
threading.Thread(target=lambda: udp("0.0.0.0","udp4"),daemon=True).start()
threading.Thread(target=lambda: udp("::","udp6"),daemon=True).start()
V6(("::",18080),H).serve_forever()
PY
sudo ip netns exec "$NS_WAN" python3 "$STATE/server.py" >"$ART/server.log" 2>&1 &
for _ in $(seq 1 50); do sudo ip netns exec "$NS_WAN" ss -lnt | grep -q ':18080' && break; sleep 0.1; done

cat > "$FAST_CFG" <<'YAML'
socks-port: 1080
allow-lan: true
bind-address: '*'
ipv6: true
mode: rule
log-level: warning
rules:
  - MATCH,DIRECT
YAML
cp "$FAST_CFG" "$SLOW_CFG"

start_fast(){
  sudo ip netns exec "$NS_FAST" "$BIN" -d "$FAST_DIR" -f "$FAST_CFG" >>"$ART/fast.log" 2>&1 &
  for _ in $(seq 1 40); do sudo ip netns exec "$NS_FAST" ss -lnt | grep -q ':1080 ' && return 0; sleep 0.1; done
  return 1
}
start_slow(){
  sudo ip netns exec "$NS_SLOW" "$BIN" -d "$SLOW_DIR" -f "$SLOW_CFG" >>"$ART/slow.log" 2>&1 &
  for _ in $(seq 1 40); do sudo ip netns exec "$NS_SLOW" ss -lnt | grep -q ':1080 ' && return 0; sleep 0.1; done
  return 1
}
start_fast || die "fast proxy failed to start"
start_slow || die "slow proxy failed to start"

write_provider(){
  local day="$1" include_dead="$2"
  cat > "$PROVIDER" <<YAML
proxies:
  - {name: "US-fast-d$day", type: socks5, server: 10.103.0.2, port: 1080, udp: true}
  - {name: "JP-slow-d$day", type: socks5, server: 10.104.0.2, port: 1080, udp: true}
YAML
  if [[ "$include_dead" == "yes" ]]; then
    cat >> "$PROVIDER" <<YAML
  - {name: "DEAD-d$day", type: socks5, server: 10.103.0.2, port: 1099, udp: true}
YAML
  fi
}
write_provider 0 no

sudo ip rule add fwmark "$TPROXY_MARK/$TPROXY_MARK" table 100 priority 100
sudo ip route add local 0.0.0.0/0 dev lo table 100
sudo ip -6 rule add fwmark "$TPROXY_MARK/$TPROXY_MARK" table 100 priority 100
sudo ip -6 route add local ::/0 dev lo table 100
sudo iptables -t mangle -A PREROUTING -i "$VC_H" -p tcp -d 10.102.0.0/24 -j TPROXY --on-port "$TPROXY_PORT" --tproxy-mark "$TPROXY_MARK/$TPROXY_MARK"
sudo iptables -t mangle -A PREROUTING -i "$VC_H" -p udp -d 10.102.0.0/24 -j TPROXY --on-port "$TPROXY_PORT" --tproxy-mark "$TPROXY_MARK/$TPROXY_MARK"
sudo ip6tables -t mangle -A PREROUTING -i "$VC_H" -p tcp -d fd102::/64 -j TPROXY --on-port "$TPROXY_PORT" --tproxy-mark "$TPROXY_MARK/$TPROXY_MARK"
sudo ip6tables -t mangle -A PREROUTING -i "$VC_H" -p udp -d fd102::/64 -j TPROXY --on-port "$TPROXY_PORT" --tproxy-mark "$TPROXY_MARK/$TPROXY_MARK"

cat > "$CFG" <<YAML
mixed-port: 27890
tproxy-port: $TPROXY_PORT
external-controller: 127.0.0.1:$CTRL
allow-lan: true
bind-address: '*'
mode: rule
log-level: warning
ipv6: true
tcp-concurrent: true
routing-mark: $ROUTING_MARK_DEC
profile:
  store-selected: true
  store-fake-ip: true
  smart-collector-size: 64
dns:
  enable: true
  listen: 0.0.0.0:$DNS_PORT
  ipv6: true
  use-hosts: true
  cache-algorithm: arc
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16
  fake-ip-range6: fc00::/18
  fake-ip-filter-mode: rule
  fake-ip-filter:
    - MATCH,fake-ip
  nameserver:
    - system
proxy-providers:
  week:
    type: file
    path: $PROVIDER
proxy-groups:
  - name: MAIN
    type: smart
    use: [week]
    url: http://10.102.0.10:18080/main
    interval: 20
    lazy: true
    timeout: 700
    tolerance: 50
    uselightgbm: false
    collectdata: false
    prefer-asn: false
  - name: AI
    type: smart
    use: [week]
    url: http://10.102.0.11:18080/ai
    interval: 20
    lazy: true
    timeout: 700
    tolerance: 60
    policy-priority: '(?i:US):1.30;(?i:JP):0.90'
    uselightgbm: false
    collectdata: false
    prefer-asn: false
  - name: SOCIAL
    type: smart
    use: [week]
    url: http://10.102.0.12:18080/social
    interval: 20
    lazy: true
    timeout: 700
    tolerance: 80
    policy-priority: '(?i:JP):1.10;(?i:US):1.00'
    uselightgbm: false
    collectdata: false
    prefer-asn: false
  - name: STREAM
    type: smart
    use: [week]
    url: http://10.102.0.13:18080/stream
    interval: 20
    lazy: true
    timeout: 700
    tolerance: 100
    uselightgbm: false
    collectdata: false
    prefer-asn: false
rules:
  - IP-CIDR,10.102.0.2/32,DIRECT,no-resolve
  - IP-CIDR,10.102.0.10/32,AI,no-resolve
  - IP-CIDR,10.102.0.11/32,MAIN,no-resolve
  - IP-CIDR,10.102.0.12/32,SOCIAL,no-resolve
  - IP-CIDR,10.102.0.13/32,STREAM,no-resolve
  - IP-CIDR,10.102.0.14/32,MAIN,no-resolve
  - IP-CIDR,10.102.0.15/32,MAIN,no-resolve
  - IP-CIDR6,fd102::2/128,DIRECT,no-resolve
  - IP-CIDR6,fd102::10/128,AI,no-resolve
  - IP-CIDR6,fd102::11/128,MAIN,no-resolve
  - IP-CIDR6,fd102::12/128,SOCIAL,no-resolve
  - IP-CIDR6,fd102::13/128,STREAM,no-resolve
  - IP-CIDR6,fd102::14/128,MAIN,no-resolve
  - IP-CIDR6,fd102::15/128,MAIN,no-resolve
  - MATCH,MAIN
YAML

start_main(){
  sudo "$BIN" -d "$MAIN_DIR" -f "$CFG" >>"$ART/mihomo-main.log" 2>&1 &
  MAIN_PID=$!
  echo "$MAIN_PID" > "$STATE/main.pid"
  for _ in $(seq 1 80); do
    curl -fsS "http://127.0.0.1:$CTRL/version" >/dev/null 2>&1 && return 0
    sleep 0.1
  done
  return 1
}

"$BIN" -t -d "$MAIN_DIR" -f "$CFG" >"$ART/config-test.log" 2>&1
start_main || die "main core failed to start"

echo "tag,rss_kb,fd,threads,connections,cache_bytes" > "$ART/metrics.csv"
sample_metrics(){
  local tag="$1"
  local rss fd th conns cb
  rss="$(awk '/VmRSS:/ {print $2}' /proc/$MAIN_PID/status 2>/dev/null || echo 0)"
  fd="$(find /proc/$MAIN_PID/fd -maxdepth 1 -type l 2>/dev/null | wc -l)"
  th="$(awk '/Threads:/ {print $2}' /proc/$MAIN_PID/status 2>/dev/null || echo 0)"
  conns="$(curl -fsS "http://127.0.0.1:$CTRL/connections" 2>/dev/null | jq '.connections|length' 2>/dev/null || echo 0)"
  cb="$(stat -c %s "$MAIN_DIR/cache.db" 2>/dev/null || echo 0)"
  printf '%s,%s,%s,%s,%s,%s\n' "$tag" "$rss" "$fd" "$th" "$conns" "$cb" >> "$ART/metrics.csv"
}

sample_metrics start
BASE_RSS="$(tail -n1 "$ART/metrics.csv" | cut -d, -f2)"
BASE_FD="$(tail -n1 "$ART/metrics.csv" | cut -d, -f3)"
BASE_TH="$(tail -n1 "$ART/metrics.csv" | cut -d, -f4)"

cat > "$STATE/load.py" <<'PY'
import concurrent.futures, socket, sys, random, json, time
day=int(sys.argv[1]); hour=int(sys.argv[2]); n=int(sys.argv[3])
rnd=random.Random(2026092600 + day*24 + hour)
services=[
 ("direct","10.102.0.2","fd102::2",4096,12),
 ("ai","10.102.0.10","fd102::10",8192,20),
 ("main","10.102.0.11","fd102::11",16384,18),
 ("social","10.102.0.12","fd102::12",4096,18),
 ("stream","10.102.0.13","fd102::13",262144,17),
 ("static","10.102.0.14","fd102::14",65536,12),
 ("download","10.102.0.15","fd102::15",1048576,3),
]
weighted=[]
for x in services: weighted += [x]*x[4]

def http_one(i):
    name,v4,v6,expect,_=rnd.choice(weighted)
    ipv6=(i%6==0)
    addr=v6 if ipv6 else v4
    fam=socket.AF_INET6 if ipv6 else socket.AF_INET
    start=time.monotonic()
    try:
        s=socket.socket(fam,socket.SOCK_STREAM); s.settimeout(3.5)
        s.connect((addr,18080))
        p=f"/{name}/d{day}/h{hour}/{i}"
        s.sendall(f"GET {p} HTTP/1.1\r\nHost: {name}.test\r\nConnection: close\r\n\r\n".encode())
        data=b""
        while True:
            b=s.recv(65536)
            if not b: break
            data+=b
        s.close()
        return (b"200 OK" in data and len(data)>=expect),len(data),time.monotonic()-start
    except Exception:
        return False,0,time.monotonic()-start

def udp_one(i):
    ipv6=(i%5==0); addr="fd102::12" if ipv6 else "10.102.0.12"
    fam=socket.AF_INET6 if ipv6 else socket.AF_INET
    start=time.monotonic()
    try:
        s=socket.socket(fam,socket.SOCK_DGRAM); s.settimeout(2.5)
        payload=(f"d{day}h{hour}u{i}-".encode()+b"u"*768)
        s.sendto(payload,(addr,18081)); data,_=s.recvfrom(4096); s.close()
        return len(data)>=len(payload),len(data),time.monotonic()-start
    except Exception:
        return False,0,time.monotonic()-start

jobs=[(udp_one if i%6==0 else http_one,i) for i in range(n)]
res=[]
with concurrent.futures.ThreadPoolExecutor(max_workers=min(96,max(24,n))) as ex:
    fs=[ex.submit(fn,i) for fn,i in jobs]
    for f in concurrent.futures.as_completed(fs): res.append(f.result())
ok=sum(1 for x in res if x[0]); byt=sum(x[1] for x in res)
lat=sorted(x[2] for x in res if x[0])
p95=lat[min(len(lat)-1,int(len(lat)*.95))] if lat else 9.0
print(json.dumps({"day":day,"hour":hour,"ok":ok,"total":len(res),"bytes":byt,"p95":p95}))
PY

fakeip_query(){
  local domain="$1" typ="$2"
  sudo ip netns exec "$NS_CLIENT" dig @10.101.0.1 -p "$DNS_PORT" "$domain" "$typ" +short +time=1 +tries=1 | tail -n1
}

cache_status(){
  local endpoint="$1" out="$2"
  local code
  code="$(curl -sS -o "$out" -w '%{http_code}' -X POST "http://127.0.0.1:$CTRL$endpoint")"
  [[ "$code" == "204" ]] || die "cache endpoint $endpoint returned $code"
}

log "Prime fake-IP and verify restart persistence baseline"
BASE_FAKE4="$(fakeip_query weekly-persist.test A)"
[[ "$BASE_FAKE4" =~ ^198\.18\. ]] || die "fake-IP A allocation failed: $BASE_FAKE4"
echo "$BASE_FAKE4" > "$ART/fakeip-baseline.txt"

TOTAL_OK=0
TOTAL_REQ=0
TOTAL_BYTES=0
DAY_FAILS=0
FAKE6_FAILS=0
START_NS="$(date +%s%N)"
: > "$ART/hourly.jsonl"
: > "$ART/events.log"

for vh in $(seq 0 167); do
  day=$((vh/24))
  hour=$((vh%24))

  case "$hour" in
    0|1|2|3|4|5) N=20 ;;
    6|7|8) N=28 ;;
    9|10|11) N=36 ;;
    12|13) N=32 ;;
    14|15|16|17) N=42 ;;
    18|19|20|21|22) N=56 ;;
    23) N=26 ;;
  esac

  # Daily provider rotation: new names exercise Smart candidate/history lifecycle.
  if [[ "$hour" == "0" ]]; then
    include_dead=no
    (( day % 2 == 1 )) && include_dead=yes
    write_provider "$day" "$include_dead"
    code="$(curl -sS -o "$ART/provider-d$day.txt" -w '%{http_code}' -X PUT "http://127.0.0.1:$CTRL/providers/proxies/week")"
    [[ "$code" == "204" ]] || die "provider update day $day returned $code"
    echo "day=$day event=provider-rotate dead=$include_dead" >> "$ART/events.log"
  fi

  # One malformed update per week; last-known-good must survive.
  if [[ "$day" == "2" && "$hour" == "4" ]]; then
    cp "$PROVIDER" "$PROVIDER.good"
    printf 'proxies: [broken: yaml: here\n' > "$PROVIDER"
    bad="$(curl -sS -o "$ART/provider-invalid.txt" -w '%{http_code}' -X PUT "http://127.0.0.1:$CTRL/providers/proxies/week")"
    [[ "$bad" == "503" ]] || die "malformed provider update returned $bad, expected 503"
    mv "$PROVIDER.good" "$PROVIDER"
    rec="$(curl -sS -o "$ART/provider-recover.txt" -w '%{http_code}' -X PUT "http://127.0.0.1:$CTRL/providers/proxies/week")"
    [[ "$rec" == "204" ]] || die "provider recovery returned $rec"
    echo "day=2 hour=4 event=provider-invalid-recover" >> "$ART/events.log"
  fi

  # DNS/fake-IP cache churn every morning: 96 unique names + stable-name check.
  if [[ "$hour" == "7" ]]; then
    : > "$ART/dns-d$day.ok"
    for i in $(seq 1 96); do
      (
        a="$(fakeip_query "d$day-h$hour-n$i.week.test" A)"
        [[ "$a" =~ ^198\.18\. ]] && echo "$i" >> "$ART/dns-d$day.ok"
      ) &
    done
    wait
    dns_ok="$(wc -l < "$ART/dns-d$day.ok")"
    (( dns_ok >= 94 )) || die "day $day fake-IP burst below 94/96: $dns_ok"
    before="$(fakeip_query stable-d$day.week.test A)"
    v6_before="$(fakeip_query stable-d$day.week.test AAAA)"
    if [[ ! "$v6_before" =~ ^fc ]]; then
      FAKE6_FAILS=$((FAKE6_FAILS+1))
      echo "day=$day event=fakeip6-missing value=$v6_before" >> "$ART/events.log"
    fi
    cache_status /cache/dns/flush "$ART/dns-flush-d$day.txt"
    after="$(fakeip_query stable-d$day.week.test A)"
    v6_after="$(fakeip_query stable-d$day.week.test AAAA)"
    [[ "$before" == "$after" ]] || die "DNS-cache flush changed fake-IP mapping on day $day"
    if [[ "$v6_before" != "$v6_after" ]]; then
      echo "day=$day event=fakeip6-changed before=$v6_before after=$v6_after" >> "$ART/events.log"
      FAKE6_FAILS=$((FAKE6_FAILS+1))
    fi
    echo "day=$day event=dns-flush fakeip4=$before fakeip6=$v6_before" >> "$ART/events.log"
  fi

  # Flush fake-IP pool twice during the week and require recovery.
  if { [[ "$day" == "2" ]] || [[ "$day" == "5" ]]; } && [[ "$hour" == "11" ]]; then
    cache_status /cache/fakeip/flush "$ART/fakeip-flush-d$day.txt"
    fresh="$(fakeip_query "post-flush-d$day.week.test" A)"
    fresh6="$(fakeip_query "post-flush-d$day.week.test" AAAA)"
    [[ "$fresh" =~ ^198\.18\. ]] || die "fake-IP did not recover after flush on day $day"
    if [[ ! "$fresh6" =~ ^fc ]]; then
      FAKE6_FAILS=$((FAKE6_FAILS+1))
      echo "day=$day hour=11 event=fakeip6-postflush-missing value=$fresh6" >> "$ART/events.log"
    fi
    echo "day=$day hour=11 event=fakeip-flush fresh4=$fresh fresh6=$fresh6" >> "$ART/events.log"
  fi

  # Smart cache/history flush on alternating days, then traffic must relearn immediately.
  if (( day % 2 == 1 )) && [[ "$hour" == "13" ]]; then
    cache_status /cache/smart/flush "$ART/smart-flush-d$day.txt"
    echo "day=$day hour=13 event=smart-flush" >> "$ART/events.log"
  fi

  # Link degradation rotates by day.
  if [[ "$hour" == "14" ]]; then
    fast_delay=$((60 + day*12))
    sudo tc qdisc change dev "$VF_H" root netem delay "${fast_delay}ms" "$((10+day*2))ms" loss "0.$((day+1))%"
    echo "day=$day hour=14 event=jitter delay=$fast_delay" >> "$ART/events.log"
  fi
  if [[ "$hour" == "16" ]]; then
    sudo tc qdisc change dev "$VF_H" root netem delay 6ms 1ms
  fi

  # Alternate node outage direction to exercise history invalidation.
  if [[ "$hour" == "18" ]]; then
    if (( day % 2 == 0 )); then
      kill_ns_pids "$NS_FAST"
      echo "day=$day hour=18 event=fast-down" >> "$ART/events.log"
    else
      kill_ns_pids "$NS_SLOW"
      echo "day=$day hour=18 event=slow-down" >> "$ART/events.log"
    fi
  fi
  if [[ "$hour" == "20" ]]; then
    if (( day % 2 == 0 )); then start_fast || die "fast recovery failed day $day"; else start_slow || die "slow recovery failed day $day"; fi
    echo "day=$day hour=20 event=node-recovered" >> "$ART/events.log"
  fi

  # Restart core on days 1, 3, 5; persisted fake-IP mapping must survive.
  if { [[ "$day" == "1" ]] || [[ "$day" == "3" ]] || [[ "$day" == "5" ]]; } && [[ "$hour" == "23" ]]; then
    persist_before="$(fakeip_query weekly-persist.test A)"
    sudo kill "$MAIN_PID" 2>/dev/null || true
    wait "$MAIN_PID" 2>/dev/null || true
    start_main || die "main restart failed day $day"
    persist_after="$(fakeip_query weekly-persist.test A)"
    [[ "$persist_before" == "$persist_after" ]] || die "store-fake-ip failed across restart day $day: $persist_before -> $persist_after"
    echo "day=$day hour=23 event=core-restart fakeip=$persist_after" >> "$ART/events.log"
  fi

  OUT="$(sudo ip netns exec "$NS_CLIENT" python3 "$STATE/load.py" "$day" "$hour" "$N")"
  echo "$OUT" >> "$ART/hourly.jsonl"
  OK="$(jq -r .ok <<<"$OUT")"; REQ="$(jq -r .total <<<"$OUT")"; BY="$(jq -r .bytes <<<"$OUT")"
  TOTAL_OK=$((TOTAL_OK+OK)); TOTAL_REQ=$((TOTAL_REQ+REQ)); TOTAL_BYTES=$((TOTAL_BYTES+BY))
  hour_bp=$((OK*10000/REQ))
  if (( hour_bp < 9000 )); then DAY_FAILS=$((DAY_FAILS+1)); fi

  if [[ "$hour" == "23" ]]; then
    sample_metrics "d$day-end"
    curl -fsS "http://127.0.0.1:$CTRL/proxies/MAIN" > "$ART/main-d$day.json" || true
    curl -fsS "http://127.0.0.1:$CTRL/providers/proxies/week" > "$ART/provider-state-d$day.json" || true
  fi

  TARGET_NS=$(( START_NS + (vh+1)*SOAK_SECONDS*1000000000/VIRTUAL_HOURS ))
  NOW_NS="$(date +%s%N)"
  if (( NOW_NS < TARGET_NS )); then
    python3 - "$((TARGET_NS-NOW_NS))" <<'PY'
import sys,time
time.sleep(int(sys.argv[1])/1e9)
PY
  fi
done

log "Weekly terminal pressure and cleanup"
cat > "$STATE/hold.py" <<'PY'
import socket,time,pathlib
socks=[]
for i in range(192):
    try:
        s=socket.socket(); s.settimeout(3); s.connect(("10.102.0.13",18080))
        s.sendall(f"GET /week-hold/{i} HTTP/1.1\r\nHost: stream.test\r\n".encode())
        socks.append(s)
    except Exception: pass
pathlib.Path("/tmp/mh-week-hold").write_text(str(len(socks)))
time.sleep(2.2)
for s in socks:
    try:s.close()
    except:pass
PY
sudo rm -f /tmp/mh-week-hold
sudo ip netns exec "$NS_CLIENT" python3 "$STATE/hold.py" >"$ART/hold.log" 2>&1 &
HOLD_PID=$!
for _ in $(seq 1 50); do [[ -f /tmp/mh-week-hold ]] && break; sleep 0.05; done
HOLD_OPEN="$(sudo cat /tmp/mh-week-hold 2>/dev/null || echo 0)"
sample_metrics hold-peak
HOLD_CONN="$(tail -n1 "$ART/metrics.csv" | cut -d, -f5)"
printf 'opened=%s\napi_connections=%s\n' "$HOLD_OPEN" "$HOLD_CONN" > "$ART/hold-summary.txt"
(( HOLD_OPEN >= 160 )) || die "weekly terminal pressure opened fewer than 160/192"
(( HOLD_CONN >= 130 )) || die "controller saw too few held connections"
wait "$HOLD_PID" || true

cache_status /cache/dns/flush "$ART/final-dns-flush.txt"
cache_status /cache/fakeip/flush "$ART/final-fakeip-flush.txt"
cache_status /cache/smart/flush "$ART/final-smart-flush.txt"
sleep 3
sample_metrics end

END_RSS="$(tail -n1 "$ART/metrics.csv" | cut -d, -f2)"
END_FD="$(tail -n1 "$ART/metrics.csv" | cut -d, -f3)"
END_TH="$(tail -n1 "$ART/metrics.csv" | cut -d, -f4)"
END_CONN="$(tail -n1 "$ART/metrics.csv" | cut -d, -f5)"
END_CACHE="$(tail -n1 "$ART/metrics.csv" | cut -d, -f6)"
RSS_DELTA=$((END_RSS-BASE_RSS))
FD_DELTA=$((END_FD-BASE_FD))
TH_DELTA=$((END_TH-BASE_TH))
SUCCESS_BP=$((TOTAL_OK*10000/TOTAL_REQ))

MAX_RSS="$(awk -F, 'NR>1{if($2>m)m=$2} END{print m+0}' "$ART/metrics.csv")"
MAX_FD="$(awk -F, 'NR>1{if($3>m)m=$3} END{print m+0}' "$ART/metrics.csv")"
MAX_TH="$(awk -F, 'NR>1{if($4>m)m=$4} END{print m+0}' "$ART/metrics.csv")"
MAX_CACHE="$(awk -F, 'NR>1{if($6>m)m=$6} END{print m+0}' "$ART/metrics.csv")"

cat > "$ART/summary.txt" <<EOF
virtual_days=$VIRTUAL_DAYS
virtual_hours=$VIRTUAL_HOURS
target_wall_seconds=$SOAK_SECONDS
requests=$TOTAL_REQ
success=$TOTAL_OK
success_basis_points=$SUCCESS_BP
bytes=$TOTAL_BYTES
low_success_hours=$DAY_FAILS
fakeip6_failures=$FAKE6_FAILS
base_rss_kb=$BASE_RSS
end_rss_kb=$END_RSS
rss_delta_kb=$RSS_DELTA
max_rss_kb=$MAX_RSS
base_fd=$BASE_FD
end_fd=$END_FD
fd_delta=$FD_DELTA
max_fd=$MAX_FD
base_threads=$BASE_TH
end_threads=$END_TH
thread_delta=$TH_DELTA
max_threads=$MAX_TH
end_connections=$END_CONN
end_cache_bytes=$END_CACHE
max_cache_bytes=$MAX_CACHE
EOF
cat "$ART/summary.txt"

kill -0 "$MAIN_PID" 2>/dev/null || die "Mihomo died during accelerated week"
(( SUCCESS_BP >= 9600 )) || die "weekly success rate below 96%"
(( DAY_FAILS <= 8 )) || die "too many virtual hours below 90% success"
(( FAKE6_FAILS == 0 )) || die "IPv6 fake-IP failed $FAKE6_FAILS checks during the virtual week"
(( RSS_DELTA <= 196608 )) || die "RSS retained >192MiB after weekly cleanup"
(( FD_DELTA <= 80 )) || die "FD retained >80 after weekly cleanup"
(( TH_DELTA <= 40 )) || die "threads retained >40 after weekly cleanup"
(( END_CONN <= 10 )) || die "residual connections >10 after cleanup"
(( END_CACHE <= 33554432 )) || die "cache.db exceeds 32MiB after final cache flush"

# Final functional recovery after all caches/history were flushed.
RECOVERY="$(sudo ip netns exec "$NS_CLIENT" curl -fsS --max-time 6 http://10.102.0.11:18080/final-recovery)"
grep -q 'path=/final-recovery' <<<"$RECOVERY" || die "Smart failed to recover after final history/cache flush"
FINAL_FAKE="$(fakeip_query final-recovery.week.test A)"
[[ "$FINAL_FAKE" =~ ^198\.18\. ]] || die "fake-IP failed after final cleanup"

sudo iptables -t mangle -L PREROUTING -v -n -x > "$ART/iptables-prerouting.txt"
sudo ip6tables -t mangle -L PREROUTING -v -n -x > "$ART/ip6tables-prerouting.txt"
curl -fsS "http://127.0.0.1:$CTRL/proxies" > "$ART/proxies-final.json"
curl -fsS "http://127.0.0.1:$CTRL/providers/proxies" > "$ART/providers-final.json"

echo ACCELERATED_WEEK_PASS | tee "$ART/result.txt"
