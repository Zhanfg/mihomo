#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BIN="${ROOT_DIR}/mihomo-profile-test"
STATE="${ROOT_DIR}/.profile-state"
ART="${ROOT_DIR}/.profile-artifacts"
CFG="${STATE}/profile.yaml"
PORT=19090

rm -rf "$STATE" "$ART"
mkdir -p "$STATE/providers" "$STATE/rules" "$ART"

log() { printf '\n[%s] %s\n' "$(date -u +%H:%M:%S)" "$*"; }
die() { echo "ERROR: $*" >&2; exit 1; }
trap '[[ -f "'"$STATE"'/pid" ]] && kill "$(cat "'"$STATE"'/pid")" 2>/dev/null || true' EXIT

[[ -x "$BIN" ]] || die "missing $BIN"

log "Create five provider files shaped like main-a..main-e"
for p in a b c d e; do
cat > "$STATE/providers/main-$p.yaml" <<YAML
proxies:
  - {name: "US-${p^^}-fast", type: socks5, server: 127.0.0.1, port: 21001, udp: true}
  - {name: "JP-${p^^}-mid", type: socks5, server: 127.0.0.1, port: 21002, udp: true}
  - {name: "HK-${p^^}-edge", type: socks5, server: 127.0.0.1, port: 21003, udp: true}
  - {name: "CN-${p^^}-control", type: socks5, server: 127.0.0.1, port: 21004, udp: true}
  - {name: "官网-剩余流量-${p^^}", type: socks5, server: 127.0.0.1, port: 21005, udp: true}
YAML
done

cat > "$STATE/rules/domain.yaml" <<'YAML'
payload:
  - '+.example-ai.test'
  - '+.example-social.test'
YAML
cat > "$STATE/rules/ip.yaml" <<'YAML'
payload:
  - '203.0.113.0/24'
  - '2001:db8:113::/48'
YAML
cat > "$STATE/rules/classical.yaml" <<'YAML'
payload:
  - 'DOMAIN-SUFFIX,example-stream.test'
  - 'DST-PORT,5228-5230'
YAML

EXCLUDE='(?i)(官网|邀请|到期|流量|更新|验证|剩余|频道|长期|推广|分享|周期|我的|套餐|Traffic|Expire|Reset|重置|距离|订阅|免费|公益|试用|WARP|Cloudflare|CF-WARP)'

cat > "$CFG" <<YAML
mixed-port: 17890
ipv6: true
mode: rule
allow-lan: false
unified-delay: true
tcp-concurrent: true
log-level: debug
external-controller: 127.0.0.1:$PORT
find-process-mode: strict
profile:
  store-selected: true
  store-fake-ip: true

proxy-providers:
  main-a: {type: file, path: ./providers/main-a.yaml}
  main-b: {type: file, path: ./providers/main-b.yaml}
  main-c: {type: file, path: ./providers/main-c.yaml}
  main-d: {type: file, path: ./providers/main-d.yaml}
  main-e: {type: file, path: ./providers/main-e.yaml}

sniffer:
  enable: true
  force-dns-mapping: true
  parse-pure-ip: true
  override-destination: false
  sniff:
    HTTP:
      ports: [80, 8080-8880]
      override-destination: true
    TLS:
      ports: [443, 5228, 8443]
    QUIC:
      ports: [443, 8443]
  force-domain: [+.example-ai.test, +.example-social.test]
  skip-domain: [+.bank.test, +.payment.test]

hosts:
  bank.test: 127.0.0.2
  payment.test: 127.0.0.3

dns:
  enable: true
  listen: 127.0.0.1:11053
  ipv6: true
  use-hosts: true
  use-system-hosts: false
  cache-algorithm: arc
  prefer-h3: false
  respect-rules: true
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16
  fake-ip-range6: fc00::/18
  fake-ip-filter-mode: rule
  fake-ip-filter:
    - DOMAIN,localhost,real-ip
    - DOMAIN-SUFFIX,bank.test,real-ip
    - DOMAIN-SUFFIX,payment.test,real-ip
    - DOMAIN-SUFFIX,example-ai.test,fake-ip
    - DOMAIN-SUFFIX,example-social.test,fake-ip
    - MATCH,fake-ip
  default-nameserver: [1.1.1.1]
  nameserver: [system]
  direct-nameserver: [system]
  direct-nameserver-follow-policy: true
  proxy-server-nameserver: [system]

proxies:
  - {name: "🇨🇳 本地直连", type: direct, udp: true}
  - {name: "🇨🇳 直连IPv6优先", type: direct, udp: true, ip-version: ipv6-prefer}
  - {name: "🇨🇳 仅IPv6直连", type: direct, udp: true, ip-version: ipv6}
  - {name: "⛔ 拒绝连接", type: reject}
  - {name: "DNS_Hijack", type: dns}

proxy-groups:
  - {name: 手动选择, type: select, use: [main-a, main-b, main-c, main-d, main-e]}
  - name: 最低延迟
    type: url-test
    use: [main-a, main-b, main-c, main-d, main-e]
    filter: '(?i)(US|JP|HK)'
    exclude-filter: '$EXCLUDE'
    url: http://127.0.0.1:1/generate_204
    interval: 600
    lazy: true
    timeout: 100
    tolerance: 150
    expected-status: 204
    max-failed-times: 2

  - name: 主力智能
    type: smart
    use: [main-a, main-b, main-c, main-d, main-e]
    url: http://127.0.0.1:1/success
    timeout: 100
    uselightgbm: true
    collectdata: false
    prefer-asn: true
    exclude-filter: '$EXCLUDE'
    expected-status: 200
    interval: 900
    lazy: true
    max-failed-times: 3
    tolerance: 80

  - name: 安全智能
    type: smart
    use: [main-a, main-b, main-c, main-d, main-e]
    url: http://127.0.0.1:1/success
    timeout: 100
    policy-priority: '(?i:CN|China):0.35'
    uselightgbm: true
    collectdata: false
    prefer-asn: true
    exclude-filter: '$EXCLUDE'
    expected-status: 200
    interval: 1800
    lazy: true
    max-failed-times: 3
    tolerance: 100

  - name: AI智能
    type: smart
    use: [main-a, main-b, main-c, main-d, main-e]
    url: http://127.0.0.1:1/success
    timeout: 100
    policy-priority: '(?i:US):1.30;(?i:JP):1.03;(?i:HK):0.20;(?i:CN):0.05'
    uselightgbm: true
    collectdata: false
    prefer-asn: true
    exclude-filter: '$EXCLUDE'
    expected-status: 200
    interval: 1800
    lazy: true
    max-failed-times: 3
    tolerance: 100

  - name: Google智能
    type: smart
    use: [main-a, main-b, main-c, main-d, main-e]
    url: http://127.0.0.1:1/success
    timeout: 100
    policy-priority: '(?i:US):1.30;(?i:JP):1.08;(?i:HK):0.20;(?i:CN):0.05'
    uselightgbm: true
    collectdata: false
    prefer-asn: true
    exclude-filter: '$EXCLUDE'
    expected-status: 200
    interval: 1800
    lazy: true
    max-failed-times: 3
    tolerance: 60

  - name: 学术科研智能
    type: smart
    use: [main-a, main-b, main-c, main-d, main-e]
    url: http://127.0.0.1:1/success
    timeout: 100
    policy-priority: '(?i:US|UK|CA|FR|DE):1.15;(?i:JP):1.00;(?i:CN):0.20'
    uselightgbm: true
    collectdata: false
    prefer-asn: true
    exclude-filter: '$EXCLUDE'
    expected-status: 200
    interval: 1800
    lazy: true
    max-failed-times: 3
    tolerance: 100

  - name: 社交低延迟
    type: smart
    use: [main-a, main-b, main-c, main-d, main-e]
    filter: '(?i)(HK|JP|US)'
    exclude-filter: '$EXCLUDE'
    url: http://127.0.0.1:1/success
    interval: 1800
    tolerance: 80
    lazy: true
    timeout: 100
    expected-status: 200
    max-failed-times: 3
    uselightgbm: true
    collectdata: false
    prefer-asn: true

  - name: 流媒体智能
    type: smart
    use: [main-a, main-b, main-c, main-d, main-e]
    filter: '(?i)(HK|JP|US)'
    exclude-filter: '$EXCLUDE'
    url: http://127.0.0.1:1/success
    interval: 1800
    tolerance: 100
    lazy: true
    timeout: 100
    expected-status: 200
    max-failed-times: 3
    uselightgbm: true
    collectdata: false
    prefer-asn: true

  - name: 静态资源智能
    type: smart
    use: [main-a, main-b, main-c, main-d, main-e]
    filter: '(?i)(HK|JP|US)'
    exclude-filter: '$EXCLUDE'
    url: http://127.0.0.1:1/success
    interval: 1800
    tolerance: 80
    lazy: true
    timeout: 100
    expected-status: 200
    max-failed-times: 3
    uselightgbm: true
    collectdata: false
    prefer-asn: true

  - {name: DNS安全出口, type: select, hidden: true, proxies: [主力智能, Google智能, 安全智能, 手动选择]}
  - {name: AI服务, type: select, proxies: [AI智能, 手动选择, "🇨🇳 本地直连"]}
  - {name: Google, type: select, proxies: [Google智能, 手动选择, "🇨🇳 本地直连"]}
  - {name: 即时通讯, type: select, proxies: [社交低延迟, 主力智能, 手动选择]}
  - {name: 流媒体娱乐, type: select, proxies: [流媒体智能, 主力智能, 手动选择]}

rule-providers:
  TEST-DOMAIN: {type: file, behavior: domain, path: ./rules/domain.yaml}
  TEST-IP: {type: file, behavior: ipcidr, path: ./rules/ip.yaml}
  TEST-CLASSICAL: {type: file, behavior: classical, path: ./rules/classical.yaml}

rules:
  - DST-PORT,53,DNS_Hijack
  - AND,((NETWORK,TCP),(DST-PORT,853)),DNS安全出口
  - AND,((NETWORK,UDP),(DST-PORT,784)),DNS安全出口
  - RULE-SET,TEST-DOMAIN,AI服务
  - RULE-SET,TEST-IP,主力智能,no-resolve
  - RULE-SET,TEST-CLASSICAL,流媒体娱乐
  - DOMAIN-SUFFIX,bank.test,🇨🇳 本地直连
  - DOMAIN-SUFFIX,payment.test,🇨🇳 本地直连
  - DOMAIN-SUFFIX,example-google.test,Google
  - PROCESS-NAME,curl,即时通讯
  - IP-CIDR,127.0.0.0/8,🇨🇳 本地直连,no-resolve
  - IP-CIDR,10.0.0.0/8,🇨🇳 本地直连,no-resolve
  - IP-CIDR,172.16.0.0/12,🇨🇳 本地直连,no-resolve
  - IP-CIDR,192.168.0.0/16,🇨🇳 本地直连,no-resolve
  - MATCH,主力智能
YAML

log "1/6 parser test"
"$BIN" -t -d "$STATE" -f "$CFG" 2>&1 | tee "$ART/config-test.log"

log "2/6 runtime/API inspection"
"$BIN" -d "$STATE" -f "$CFG" >"$ART/mihomo.log" 2>&1 &
PID=$!
echo "$PID" > "$STATE/pid"
for _ in $(seq 1 80); do
  curl -fsS "http://127.0.0.1:${PORT}/version" >"$ART/version.json" 2>/dev/null && break
  sleep 0.1
done
curl -fsS "http://127.0.0.1:${PORT}/proxies" >"$ART/proxies.json"
curl -fsS "http://127.0.0.1:${PORT}/providers/proxies" >"$ART/providers.json"
curl -fsS "http://127.0.0.1:${PORT}/rules" >"$ART/rules.json"

log "3/6 eight Smart groups and wrappers"
for g in 主力智能 安全智能 AI智能 Google智能 学术科研智能 社交低延迟 流媒体智能 静态资源智能; do
  jq -e --arg g "$g" '.proxies[$g] != null' "$ART/proxies.json" >/dev/null || die "missing Smart group: $g"
done
for g in DNS安全出口 AI服务 Google 即时通讯 流媒体娱乐; do
  jq -e --arg g "$g" '.proxies[$g] != null' "$ART/proxies.json" >/dev/null || die "missing service group: $g"
done

log "4/6 provider fan-in, filters and exclusions"
for p in main-a main-b main-c main-d main-e; do
  jq -e --arg p "$p" '.providers[$p] != null' "$ART/providers.json" >/dev/null || die "provider did not load: $p"
done
jq -e '.proxies["主力智能"].all | length >= 15' "$ART/proxies.json" >/dev/null || die "main Smart group did not aggregate providers"
jq -e '[.proxies["主力智能"].all[] | select(test("官网|剩余流量"; "i"))] | length == 0' "$ART/proxies.json" >/dev/null || die "exclude-filter failed"
jq -e '[.proxies["社交低延迟"].all[] | select(test("^CN-"; "i"))] | length == 0' "$ART/proxies.json" >/dev/null || die "social filter admitted CN nodes"
jq -e '[.proxies["社交低延迟"].all[] | select(test("^(US|JP|HK)-"; "i"))] | length >= 10' "$ART/proxies.json" >/dev/null || die "social filter removed valid nodes"

log "4b/6 provider hot-update, invalid-update containment and recovery"
cat > "$STATE/providers/main-a.yaml" <<'YAML'
proxies:
  - {name: "US-A-new", type: socks5, server: 127.0.0.1, port: 21101, udp: true}
  - {name: "JP-A-mid", type: socks5, server: 127.0.0.1, port: 21002, udp: true}
  - {name: "HK-A-edge", type: socks5, server: 127.0.0.1, port: 21003, udp: true}
YAML
HTTP_CODE="$(curl -sS -o "$ART/provider-main-a-update.txt" -w '%{http_code}' -X PUT "http://127.0.0.1:${PORT}/providers/proxies/main-a")"
[[ "$HTTP_CODE" == "204" ]] || die "main-a hot update returned $HTTP_CODE"
for _ in $(seq 1 30); do
  curl -fsS "http://127.0.0.1:${PORT}/proxies" >"$ART/proxies-after-provider-update.json"
  if jq -e '.proxies["主力智能"].all | index("US-A-new") != null' "$ART/proxies-after-provider-update.json" >/dev/null; then break; fi
  sleep 0.1
done
jq -e '.proxies["主力智能"].all | index("US-A-new") != null' "$ART/proxies-after-provider-update.json" >/dev/null || die "new provider node did not propagate to Smart"
jq -e '.proxies["主力智能"].all | index("US-A-fast") == null' "$ART/proxies-after-provider-update.json" >/dev/null || die "removed provider node remained in Smart"

cp "$STATE/providers/main-b.yaml" "$STATE/providers/main-b.good.yaml"
printf 'proxies: [this is: invalid: yaml\n' > "$STATE/providers/main-b.yaml"
BAD_CODE="$(curl -sS -o "$ART/provider-main-b-invalid.txt" -w '%{http_code}' -X PUT "http://127.0.0.1:${PORT}/providers/proxies/main-b")"
[[ "$BAD_CODE" == "503" ]] || die "invalid provider update should return 503, got $BAD_CODE"
curl -fsS "http://127.0.0.1:${PORT}/proxies" >"$ART/proxies-after-invalid-provider.json"
jq -e '.proxies["主力智能"].all | index("US-B-fast") != null' "$ART/proxies-after-invalid-provider.json" >/dev/null || die "invalid update destroyed last known-good provider state"
mv "$STATE/providers/main-b.good.yaml" "$STATE/providers/main-b.yaml"
RECOVER_CODE="$(curl -sS -o "$ART/provider-main-b-recover.txt" -w '%{http_code}' -X PUT "http://127.0.0.1:${PORT}/providers/proxies/main-b")"
[[ "$RECOVER_CODE" == "204" ]] || die "provider recovery returned $RECOVER_CODE"

cat >> "$STATE/rules/domain.yaml" <<'YAML'
  - '+.provider-refresh.test'
YAML
RULE_CODE="$(curl -sS -o "$ART/rule-provider-update.txt" -w '%{http_code}' -X PUT "http://127.0.0.1:${PORT}/providers/rules/TEST-DOMAIN")"
[[ "$RULE_CODE" == "204" ]] || die "rule-provider update returned $RULE_CODE"

log "5/6 fake-IP dual stack + real-IP exclusions"
A="$(dig @127.0.0.1 -p 11053 example-ai.test A +short | tail -n1)"
AAAA="$(dig @127.0.0.1 -p 11053 example-ai.test AAAA +short | tail -n1)"
BANK="$(dig @127.0.0.1 -p 11053 bank.test A +short | tail -n1)"
ip -6 route show >"$ART/host-ipv6-routes.txt" 2>&1 || true
printf 'A=%s\nAAAA=%s\nBANK=%s\n' "$A" "$AAAA" "$BANK" | tee "$ART/dns-results.txt"

python3 - "$A" "$BANK" <<'PY'
import ipaddress, sys
a, bank = map(ipaddress.ip_address, sys.argv[1:3])
assert a in ipaddress.ip_network("198.18.0.0/16"), a
assert str(bank) == "127.0.0.2", bank
PY

if [[ -n "$AAAA" ]]; then
  python3 - "$AAAA" <<'PY'
import ipaddress, sys
aaaa = ipaddress.ip_address(sys.argv[1])
assert aaaa in ipaddress.ip_network("fc00::/18"), aaaa
PY
  echo "fakeip6=active:$AAAA" | tee "$ART/fakeip6-capability.txt"
elif ip -6 route show default | grep -q '^default'; then
  die "AAAA fake-IP missing even though runner has an IPv6 default route"
else
  # parseIPV6 intentionally clears fake-ip-range6 when the host has no usable
  # IPv6 capability. The IPv6 fakeip pool itself is covered by repository unit
  # tests (component/fakeip TestPool_BasicV6); do not mislabel this runner
  # limitation as a DNS regression.
  echo "fakeip6=capability-skip:no-ipv6-default-route" | tee "$ART/fakeip6-capability.txt"
fi

log "6/6 rule surface + selected-state persistence"
jq -e '.rules[] | select(.type == "ProcessName" and .payload == "curl" and .proxy == "即时通讯")' "$ART/rules.json" >/dev/null || die "process rules missing"
jq -e '.rules[] | select(.type == "RuleSet" and .payload == "TEST-DOMAIN")' "$ART/rules.json" >/dev/null || die "rule-provider rules missing"
jq -e '.rules[] | select(.type == "DstPort" and .payload == "53" and .proxy == "DNS_Hijack")' "$ART/rules.json" >/dev/null || die "port rules missing"

SELECT_CODE="$(curl -sS -o "$ART/select-manual.txt" -w '%{http_code}' \
  -X PUT -H 'Content-Type: application/json' \
  -d '{"name":"JP-A-mid"}' "http://127.0.0.1:${PORT}/proxies/%E6%89%8B%E5%8A%A8%E9%80%89%E6%8B%A9")"
[[ "$SELECT_CODE" == "204" ]] || die "manual selection update returned $SELECT_CODE"
sleep 0.2
kill "$PID"
wait "$PID" 2>/dev/null || true
"$BIN" -d "$STATE" -f "$CFG" >"$ART/mihomo-restart.log" 2>&1 &
PID=$!
echo "$PID" > "$STATE/pid"
for _ in $(seq 1 80); do
  curl -fsS "http://127.0.0.1:${PORT}/proxies/%E6%89%8B%E5%8A%A8%E9%80%89%E6%8B%A9" >"$ART/manual-after-restart.json" 2>/dev/null && break
  sleep 0.1
done
jq -e '.now == "JP-A-mid"' "$ART/manual-after-restart.json" >/dev/null || die "store-selected did not survive restart"

echo PROFILE_REGRESSION_PASS | tee "$ART/result.txt"
