#!/usr/bin/env python3
import argparse, base64, copy, gzip, hashlib, json, re, subprocess, threading, time
from collections import Counter, defaultdict
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
import yaml

EXPECTED_SHA256 = "cfbc0d61b5087cef2e7e3991db82fff77080b3cc79c34126fa5e09e16186a8bf"
EXPECTED_COUNTS = {"proxy_providers": 11, "proxy_groups": 88, "rule_providers": 71, "rules": 1357, "proxies": 5}
EXPECTED_GROUP_TYPES = {"select": 66, "url-test": 13, "smart": 8, "fallback": 1}
EXPECTED_RULE_TYPES = {
    "DOMAIN": 120, "DOMAIN-SUFFIX": 836, "DST-PORT": 1, "AND": 62,
    "RULE-SET": 70, "DOMAIN-KEYWORD": 9, "IP-CIDR": 28, "IP-CIDR6": 10,
    "PROCESS-NAME": 216, "DOMAIN-REGEX": 2, "GEOSITE": 1, "GEOIP": 1, "MATCH": 1,
}
EXPECTED_SMART = ["主力智能","安全智能","AI智能","Google智能","学术科研智能","社交低延迟","流媒体智能","静态资源智能"]
GOLDEN_RULES = {
    3: "DST-PORT,53,DNS_Hijack",
    54: "AND,((PROCESS-NAME,codex),(DOMAIN,api.openai.com)),Codex",
    56: "DOMAIN,api.openai.com,OpenAI API",
    57: "DOMAIN,api.anthropic.com,Anthropic API",
    59: "DOMAIN,grok.x.com,Grok",
    60: "AND,((PROCESS-NAME,ai.x.grok),(DOMAIN-SUFFIX,x.com)),Grok",
    98: "DOMAIN-SUFFIX,x.com,X",
    105: "DOMAIN-SUFFIX,paypal.com,PayPal",
    106: "DOMAIN-SUFFIX,stripe.com,Stripe",
    307: "AND,((PROCESS-NAME,org.telegram.messenger),(DOMAIN-SUFFIX,challenges.cloudflare.com)),Telegram",
    555: "DOMAIN-SUFFIX,challenges.cloudflare.com,人机验证",
    576: "DOMAIN-SUFFIX,chatgpt.com,ChatGPT",
    610: "DOMAIN-SUFFIX,anthropic.com,Claude",
    847: "DOMAIN-SUFFIX,bankofchina.com,🇨🇳 本地直连",
}
ALLOWED_SPECIAL = {"DIRECT","REJECT","REJECT-DROP","GLOBAL","COMPATIBLE","PASS"}
REGIONS = [
    "🇺🇸 US Shadow", "🇯🇵 JP Shadow", "🇭🇰 HK Shadow", "🇹🇼 TW Shadow",
    "🇸🇬 SG Shadow", "🇬🇧 UK Shadow", "🇨🇦 CA Shadow", "🇫🇷 FR Shadow",
    "IPv6 V6 Shadow",
]

def die(msg):
    raise SystemExit("ERROR: " + msg)

def load_fixture(path):
    raw_b64 = Path(path).read_text(encoding="utf-8").strip()
    text = gzip.decompress(base64.b64decode(raw_b64)).decode("utf-8")
    got = hashlib.sha256(text.encode()).hexdigest()
    if got != EXPECTED_SHA256:
        die(f"fixture sha256 drift: {got}")
    return text, yaml.safe_load(text)

def rule_type(rule):
    return rule.split(",",1)[0] if isinstance(rule,str) else "<non-string>"

def structural_checks(cfg):
    providers = cfg.get("proxy-providers") or {}
    groups = cfg.get("proxy-groups") or []
    rule_providers = cfg.get("rule-providers") or {}
    rules = cfg.get("rules") or []
    proxies = cfg.get("proxies") or []
    actual = {
        "proxy_providers": len(providers), "proxy_groups": len(groups),
        "rule_providers": len(rule_providers), "rules": len(rules), "proxies": len(proxies),
    }
    if actual != EXPECTED_COUNTS:
        die(f"topology counts drift: {actual}")
    gt = Counter(g.get("type") for g in groups)
    if dict(gt) != EXPECTED_GROUP_TYPES:
        die(f"group type counts drift: {dict(gt)}")
    rt = Counter(rule_type(r) for r in rules)
    if dict(rt) != EXPECTED_RULE_TYPES:
        die(f"rule type counts drift: {dict(rt)}")
    if Counter(v.get("format") for v in rule_providers.values()) != Counter({"mrs": 71}):
        die("all 71 real rule providers must remain MRS")
    if Counter(v.get("behavior") for v in rule_providers.values()) != Counter({"domain": 60, "ipcidr": 11}):
        die("rule-provider behavior split drifted")
    if Counter(v.get("proxy") for v in rule_providers.values()) != Counter({"静态资源智能": 71}):
        die("all real rule providers must bootstrap through 静态资源智能")
    smart = [g for g in groups if g.get("type") == "smart"]
    if [g.get("name") for g in smart] != EXPECTED_SMART:
        die("Smart group names/order drifted")
    if any(g.get("uselightgbm") is not True for g in smart):
        die("all eight real Smart groups must retain uselightgbm=true")

    dns = cfg.get("dns") or {}
    required_dns = {
        "enable": True, "ipv6": True, "respect-rules": True,
        "enhanced-mode": "fake-ip", "fake-ip-range": "198.18.0.1/16",
        "fake-ip-range6": "fc00::/18", "fake-ip-filter-mode": "rule",
    }
    for k,v in required_dns.items():
        if dns.get(k) != v:
            die(f"DNS field drift: {k}={dns.get(k)!r}")
    sniffer = cfg.get("sniffer") or {}
    if not sniffer.get("enable") or not sniffer.get("force-dns-mapping") or not sniffer.get("parse-pure-ip"):
        die("sniffer safety surface drifted")
    for idx, raw in GOLDEN_RULES.items():
        if rules[idx] != raw:
            die(f"golden rule drift at {idx}: {rules[idx]!r}")

    group_names = {g["name"] for g in groups}
    proxy_names = {p["name"] for p in proxies}
    provider_names = set(providers)
    allowed = group_names | proxy_names | ALLOWED_SPECIAL
    dangling = []
    graph = defaultdict(set)
    for g in groups:
        name = g["name"]
        for p in g.get("use") or []:
            if p not in provider_names:
                dangling.append(f"group {name} uses missing provider {p}")
        for p in g.get("proxies") or []:
            if p not in allowed:
                dangling.append(f"group {name} references missing proxy/group {p}")
            elif p in group_names:
                graph[name].add(p)
    for r in rules:
        if not isinstance(r,str):
            continue
        parts = r.split(",")
        target = parts[-1]
        if target in {"no-resolve","src","dst"} and len(parts) >= 2:
            target = parts[-2]
        if target not in allowed:
            dangling.append(f"rule targets missing adapter {target}: {r}")
    if dangling:
        die("; ".join(dangling[:8]))

    temp, perm = set(), set()
    def visit(n, stack):
        if n in perm:
            return
        if n in temp:
            die("proxy-group cycle: " + " -> ".join(stack + [n]))
        temp.add(n)
        for m in graph.get(n, ()):
            visit(m, stack + [n])
        temp.remove(n)
        perm.add(n)
    for n in group_names:
        visit(n, [])

    pos = {r:i for i,r in enumerate(rules)}
    for specific, fallback in [
        (GOLDEN_RULES[54], GOLDEN_RULES[56]),
        (GOLDEN_RULES[60], GOLDEN_RULES[98]),
        (GOLDEN_RULES[307], GOLDEN_RULES[555]),
    ]:
        if pos[specific] >= pos[fallback]:
            die(f"precedence inverted: {specific} must precede {fallback}")
    return {
        "counts": actual,
        "group_types": dict(gt),
        "rule_types": dict(rt),
        "smart_groups": EXPECTED_SMART,
        "dns_fake_ip_filters": len(dns.get("fake-ip-filter") or []),
        "sniffer_force_domains": len(sniffer.get("force-domain") or []),
        "sniffer_skip_domains": len(sniffer.get("skip-domain") or []),
    }

class MockState:
    lock = threading.Lock()
    provider_hits = Counter()
    rule_hits = Counter()
    fail_provider = None
    fail_rule = None

class Handler(BaseHTTPRequestHandler):
    def _send(self, code, body=b"", ctype="text/plain"):
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        if body:
            self.wfile.write(body)

    def do_GET(self):
        if self.path.startswith("/health/"):
            self._send(204)
            return
        m = re.match(r"^/providers/([^/?]+)\.yaml", self.path)
        if m:
            name = m.group(1)
            with MockState.lock:
                MockState.provider_hits[name] += 1
                fail = MockState.fail_provider == name
            if fail:
                self._send(503, b"provider unavailable")
                return
            payload = {"proxies":[{"name":n,"type":"direct","udp":True} for n in REGIONS]}
            body = yaml.safe_dump(payload, allow_unicode=True, sort_keys=False).encode()
            self._send(200, body, "text/yaml")
            return
        m = re.match(r"^/rules/([^/?]+)\.yaml", self.path)
        if m:
            name = m.group(1)
            with MockState.lock:
                MockState.rule_hits[name] += 1
                fail = MockState.fail_rule == name
            if fail:
                self._send(503, b"rule unavailable")
                return
            behavior = self.server.rule_behaviors.get(name, "domain")
            payload = {"payload":["203.0.113.0/24"]} if behavior == "ipcidr" else {"payload":[f"shadow-{abs(hash(name)) % 100000}.test"]}
            body = yaml.safe_dump(payload, allow_unicode=True, sort_keys=False).encode()
            self._send(200, body, "text/yaml")
            return
        self._send(404, b"not found")

    def log_message(self, fmt, *args):
        return

def runtime_config(src):
    cfg = copy.deepcopy(src)
    cfg.pop("external-ui", None)
    cfg.pop("external-ui-url", None)
    cfg.pop("secret", None)
    cfg["mixed-port"] = 17890
    cfg["redir-port"] = 0
    cfg["tproxy-port"] = 0
    cfg["external-controller"] = "127.0.0.1:29091"
    cfg["geo-auto-update"] = False
    cfg["lgbm-auto-update"] = False
    cfg["log-level"] = "warning"
    if isinstance(cfg.get("tun"), dict):
        cfg["tun"]["enable"] = False

    dns = cfg.get("dns") or {}
    dns["listen"] = "127.0.0.1:11053"
    dns["default-nameserver"] = ["1.1.1.1"]
    dns["nameserver"] = ["system"]
    dns["direct-nameserver"] = ["system"]
    dns["proxy-server-nameserver"] = ["system"]
    dns["nameserver-policy"] = {}
    cfg["dns"] = dns

    for p in (cfg.get("proxy-providers") or {}).values():
        hc = p.get("health-check") or {}
        hc["enable"] = True
        hc["url"] = "http://127.0.0.1:33000/health/204"
        hc["expected-status"] = 204
        hc["lazy"] = True
        p["health-check"] = hc
    for g in cfg.get("proxy-groups") or []:
        if "url" in g:
            g["url"] = "http://127.0.0.1:33000/health/204"
            g["expected-status"] = 204
            g["lazy"] = True

    for name, rp in (cfg.get("rule-providers") or {}).items():
        rp["format"] = "yaml"
        rp["url"] = f"http://127.0.0.1:33000/rules/{name}.yaml"
        rp["path"] = f"./rules/{name}.yaml"

    out_rules = []
    for r in cfg.get("rules") or []:
        if r.startswith("GEOSITE,CN,"):
            out_rules.append("DOMAIN-SUFFIX,shadow-cn.test,🇨🇳 本地直连")
        elif r.startswith("GEOIP,CN,"):
            out_rules.append("IP-CIDR,203.0.113.0/24,🇨🇳 本地直连,no-resolve")
        else:
            out_rules.append(r)
    cfg["rules"] = out_rules
    return cfg

def wait_http(url, timeout=12):
    import urllib.request
    end = time.time() + timeout
    last = None
    while time.time() < end:
        try:
            with urllib.request.urlopen(url, timeout=0.5) as r:
                return r.read()
        except Exception as e:
            last = e
            time.sleep(0.1)
    die(f"controller did not become ready: {last}")

def http_json(url, method="GET"):
    import urllib.request
    req = urllib.request.Request(url, method=method)
    with urllib.request.urlopen(req, timeout=4) as r:
        raw = r.read()
        return json.loads(raw) if raw else None

def run_runtime(bin_path, cfg, work, art):
    work.mkdir(parents=True, exist_ok=True)
    art.mkdir(parents=True, exist_ok=True)
    runtime_yaml = work / "shadow-runtime.yaml"
    runtime_yaml.write_text(yaml.safe_dump(cfg, allow_unicode=True, sort_keys=False, width=240), encoding="utf-8")

    server = ThreadingHTTPServer(("127.0.0.1", 33000), Handler)
    server.rule_behaviors = {k:v.get("behavior") for k,v in cfg["rule-providers"].items()}
    threading.Thread(target=server.serve_forever, daemon=True).start()

    test = subprocess.run([bin_path, "-t", "-d", str(work), "-f", str(runtime_yaml)], text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    (art/"config-test.log").write_text(test.stdout, encoding="utf-8")
    if test.returncode:
        die("full shadow config parser test failed")

    logf = open(art/"mihomo.log","w",encoding="utf-8")
    proc = subprocess.Popen([bin_path, "-d", str(work), "-f", str(runtime_yaml)], stdout=logf, stderr=subprocess.STDOUT, text=True)
    try:
        wait_http("http://127.0.0.1:29091/version")
        proxies = http_json("http://127.0.0.1:29091/proxies")
        providers = http_json("http://127.0.0.1:29091/providers/proxies")
        rule_providers = http_json("http://127.0.0.1:29091/providers/rules")
        rules = http_json("http://127.0.0.1:29091/rules")
        (art/"proxies.json").write_text(json.dumps(proxies,ensure_ascii=False,indent=2))
        (art/"providers.json").write_text(json.dumps(providers,ensure_ascii=False,indent=2))
        (art/"rule-providers.json").write_text(json.dumps(rule_providers,ensure_ascii=False,indent=2))
        (art/"rules.json").write_text(json.dumps(rules,ensure_ascii=False,indent=2))

        if len(providers.get("providers",{})) != 11:
            die("runtime did not load all 11 proxy providers")
        if len(rule_providers.get("providers",{})) != 71:
            die("runtime did not load all 71 rule providers")
        for name in EXPECTED_SMART:
            if name not in proxies.get("proxies",{}):
                die(f"runtime missing Smart group {name}")
        missing_groups = [g["name"] for g in cfg["proxy-groups"] if g["name"] not in proxies.get("proxies",{})]
        if missing_groups:
            die("runtime missing groups: " + ", ".join(missing_groups[:8]))
        if len(rules.get("rules",[])) != 1357:
            die(f"runtime rule count drifted: {len(rules.get('rules',[]))}")

        normalized = rules["rules"]
        def exists(kind, payload=None, proxy=None):
            for r in normalized:
                if r.get("type") != kind:
                    continue
                if payload is not None and r.get("payload") != payload:
                    continue
                if proxy is not None and r.get("proxy") != proxy:
                    continue
                return True
            return False
        for kind,payload,proxy in [
            ("DstPort","53","DNS_Hijack"),
            ("Domain","api.openai.com","OpenAI API"),
            ("Domain","api.anthropic.com","Anthropic API"),
            ("Domain","grok.x.com","Grok"),
            ("DomainSuffix","chatgpt.com","ChatGPT"),
            ("DomainSuffix","anthropic.com","Claude"),
            ("DomainSuffix","paypal.com","PayPal"),
            ("DomainSuffix","stripe.com","Stripe"),
            ("DomainSuffix","bankofchina.com","🇨🇳 本地直连"),
        ]:
            if not exists(kind,payload,proxy):
                die(f"normalized golden rule missing: {kind}/{payload}->{proxy}")

        end = time.time() + 8
        while time.time() < end:
            with MockState.lock:
                pcount = len(MockState.provider_hits)
                rcount = len(MockState.rule_hits)
            if pcount == 11 and rcount == 71:
                break
            time.sleep(0.1)
        with MockState.lock:
            p_hits = dict(MockState.provider_hits)
            r_hits = dict(MockState.rule_hits)
        (art/"mock-hits.json").write_text(json.dumps({"proxy":p_hits,"rules":r_hits},ensure_ascii=False,indent=2))
        if len(p_hits) != 11:
            die(f"mock did not receive all proxy-provider fetches: {len(p_hits)}/11")
        if len(r_hits) != 71:
            die(f"mock did not receive all Smart-proxied rule-provider fetches: {len(r_hits)}/71")

        target = "main-a"
        with MockState.lock:
            MockState.fail_provider = target
        try:
            import urllib.request, urllib.error
            req=urllib.request.Request(f"http://127.0.0.1:29091/providers/proxies/{target}",method="PUT")
            try:
                urllib.request.urlopen(req,timeout=4)
                die("provider refresh unexpectedly succeeded during injected 503")
            except urllib.error.HTTPError as e:
                if e.code != 503:
                    die(f"provider failure returned {e.code}, expected 503")
        finally:
            with MockState.lock:
                MockState.fail_provider = None
        after_fail = http_json("http://127.0.0.1:29091/providers/proxies")["providers"][target]
        if not after_fail.get("proxies"):
            die("provider 503 destroyed last-known-good state")
        http_json(f"http://127.0.0.1:29091/providers/proxies/{target}", method="PUT")

        rtarget = next(iter(cfg["rule-providers"]))
        with MockState.lock:
            MockState.fail_rule = rtarget
        try:
            import urllib.request, urllib.error
            req=urllib.request.Request(f"http://127.0.0.1:29091/providers/rules/{rtarget}",method="PUT")
            try:
                urllib.request.urlopen(req,timeout=4)
                die("rule-provider refresh unexpectedly succeeded during injected 503")
            except urllib.error.HTTPError as e:
                if e.code != 503:
                    die(f"rule-provider failure returned {e.code}, expected 503")
        finally:
            with MockState.lock:
                MockState.fail_rule = None
        http_json(f"http://127.0.0.1:29091/providers/rules/{rtarget}", method="PUT")

        summary = {
            "proxy_providers_loaded": 11,
            "proxy_groups_loaded": 88,
            "rule_providers_loaded": 71,
            "rules_loaded": 1357,
            "http_provider_failure_containment": "pass",
            "smart_rule_provider_bootstrap": "pass",
            "golden_rule_surface": "pass",
        }
        (art/"runtime-summary.json").write_text(json.dumps(summary,ensure_ascii=False,indent=2))
        return summary
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=4)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait()
        logf.close()
        server.shutdown()
        server.server_close()

def main():
    ap=argparse.ArgumentParser()
    ap.add_argument("--fixture", required=True)
    ap.add_argument("--bin", required=True)
    ap.add_argument("--work", default=".real-shadow-state")
    ap.add_argument("--artifacts", default=".real-shadow-artifacts")
    args=ap.parse_args()
    _,cfg=load_fixture(args.fixture)
    structural=structural_checks(cfg)
    art=Path(args.artifacts)
    art.mkdir(parents=True,exist_ok=True)
    (art/"structural-summary.json").write_text(json.dumps(structural,ensure_ascii=False,indent=2))
    summary=run_runtime(args.bin,runtime_config(cfg),Path(args.work),art)
    print(json.dumps({"structural":structural,"runtime":summary},ensure_ascii=False,indent=2))
    print("REAL_CONFIG_SHADOW_PASS")

if __name__=="__main__":
    main()
