#!/usr/bin/env python3
import argparse, base64, gzip, json, re, subprocess, threading, time
from collections import Counter, defaultdict
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
import yaml

EXPECTED_COUNTS = {"proxy_providers": 11, "proxy_groups": 88, "rule_providers": 71, "rules": 1357}
EXPECTED_GROUP_TYPES = {"select": 66, "url-test": 13, "smart": 8, "fallback": 1}
EXPECTED_SMART = ["主力智能","安全智能","AI智能","Google智能","学术科研智能","社交低延迟","流媒体智能","静态资源智能"]
BUILTIN_PROXIES = [
    {"name":"🇨🇳 本地直连","type":"direct","udp":True},
    {"name":"🇨🇳 直连IPv6优先","type":"direct","udp":True,"ip-version":"ipv6-prefer"},
    {"name":"🇨🇳 仅IPv6直连","type":"direct","udp":True,"ip-version":"ipv6"},
    {"name":"⛔ 拒绝连接","type":"reject"},
    {"name":"DNS_Hijack","type":"dns"},
]
REGION_NODES = [
    "US Shadow","JP Shadow","HK Shadow","TW Shadow","SG Shadow",
    "UK Shadow","CA Shadow","FR Shadow","DE Shadow","IPv6 V6 Shadow"
]

def die(msg):
    raise SystemExit("ERROR: " + msg)

def load_manifest(path):
    raw = Path(path).read_text(encoding="utf-8").strip()
    return json.loads(gzip.decompress(base64.b64decode(raw)).decode("utf-8"))

def expand_types(rle):
    out=[]
    for kind,count in rle:
        out.extend([kind]*int(count))
    return out

def placeholder_rule(kind, idx, rpnames):
    target="主力智能"
    if kind=="DOMAIN":
        return f"DOMAIN,shadow-{idx}.test,{target}"
    if kind=="DOMAIN-SUFFIX":
        return f"DOMAIN-SUFFIX,shadow-sfx-{idx}.test,{target}"
    if kind=="DST-PORT":
        return f"DST-PORT,{10000 + (idx % 50000)},{target}"
    if kind=="AND":
        return f"AND,((NETWORK,TCP),(DST-PORT,{20000 + (idx % 30000)})),{target}"
    if kind=="RULE-SET":
        name=rpnames[idx % len(rpnames)]
        return f"RULE-SET,{name},{target}"
    if kind=="DOMAIN-KEYWORD":
        return f"DOMAIN-KEYWORD,shadowkw{idx},{target}"
    if kind=="IP-CIDR":
        return f"IP-CIDR,198.51.100.0/24,{target},no-resolve"
    if kind=="IP-CIDR6":
        return f"IP-CIDR6,2001:db8::/32,{target},no-resolve"
    if kind=="PROCESS-NAME":
        return f"PROCESS-NAME,shadow-process-{idx},{target}"
    if kind=="DOMAIN-REGEX":
        return f"DOMAIN-REGEX,^shadow{idx}\\.test$,{target}"
    if kind=="GEOSITE":
        return "GEOSITE,CN,🇨🇳 本地直连"
    if kind=="GEOIP":
        return "GEOIP,CN,🇨🇳 本地直连,no-resolve"
    if kind=="MATCH":
        return "MATCH,主力智能"
    die(f"unsupported rule type {kind}")

def build_shadow(m):
    providers={}
    for name in m["pp"]:
        providers[name]={
            "type":"http",
            "url":f"http://127.0.0.1:33000/providers/{name}.yaml",
            "path":f"./providers/{name}.yaml",
            "interval":3600,
            "health-check":{
                "enable":True,
                "url":"http://127.0.0.1:33000/health/204",
                "interval":300,
                "lazy":True,
                "timeout":500,
                "expected-status":204,
            },
        }

    groups=[]
    for entry in m["g"]:
        name,kind,use,proxies,flt,exclude,hidden=entry
        g={"name":name,"type":kind}
        if use: g["use"]=use
        if proxies: g["proxies"]=proxies
        if flt: g["filter"]=flt
        if exclude: g["exclude-filter"]=exclude
        if hidden: g["hidden"]=True
        if kind in ("smart","url-test","fallback"):
            g.update({
                "url":"http://127.0.0.1:33000/health/204",
                "interval":600,
                "lazy":True,
                "timeout":500,
                "expected-status":204,
            })
        if kind=="smart":
            g.update({"tolerance":100,"uselightgbm":True,"collectdata":False,"prefer-asn":True,"max-failed-times":3})
        elif kind=="url-test":
            g.update({"tolerance":150,"max-failed-times":2})
        groups.append(g)

    rps={}
    for rp_index,(name,behavior,proxy) in enumerate(m["rp"]):
        token=f"rp-{rp_index:03d}"
        rps[name]={
            "type":"http","behavior":behavior,"format":"yaml",
            "url":f"http://127.0.0.1:33000/rules/{token}.yaml",
            "path":f"./rules/{token}.yaml",
            "interval":3600,
            "proxy":proxy or "静态资源智能",
        }

    kinds=expand_types(m["rle"])
    if len(kinds)!=1357:
        die(f"manifest rule sequence length is {len(kinds)}, expected 1357")
    rpnames=list(rps)
    rules=[placeholder_rule(k,i,rpnames) for i,k in enumerate(kinds)]
    for idx,raw in m["gold"]:
        rules[int(idx)]=raw

    cfg={
        "mixed-port":17890,
        "redir-port":0,
        "tproxy-port":0,
        "external-controller":"127.0.0.1:29091",
        "allow-lan":True,
        "bind-address":"*",
        "mode":"rule",
        "log-level":"warning",
        "ipv6":True,
        "tcp-concurrent":True,
        "profile":{"store-selected":True,"store-fake-ip":True,"smart-collector-size":64},
        "geo-auto-update":False,
        "lgbm-auto-update":False,
        "tun":{"enable":False},
        "sniffer":{
            "enable":True,"force-dns-mapping":True,"parse-pure-ip":True,
            "sniff":{"HTTP":{"ports":[80,"8080-8880"],"override-destination":True},
                     "TLS":{"ports":[443,5228,8443]},
                     "QUIC":{"ports":[443,8443]}},
            "force-domain":["+.chatgpt.com","+.openai.com","+.anthropic.com","+.x.com"],
            "skip-domain":["+.bankofchina.com","+.paypal.com","+.stripe.com"],
        },
        "dns":{
            "enable":True,"listen":"127.0.0.1:11053","ipv6":True,
            "use-hosts":True,"use-system-hosts":False,
            "cache-algorithm":"arc","respect-rules":True,
            "enhanced-mode":"fake-ip","fake-ip-range":"198.18.0.1/16",
            "fake-ip-range6":"fc00::/18","fake-ip-filter-mode":"rule",
            "fake-ip-filter":["MATCH,fake-ip"],
            "default-nameserver":["1.1.1.1"],
            "nameserver":["system"],"direct-nameserver":["system"],
            "proxy-server-nameserver":["system"],
        },
        "proxies":BUILTIN_PROXIES,
        "proxy-providers":providers,
        "proxy-groups":groups,
        "rule-providers":rps,
        "rules":rules,
    }
    return cfg

def structural_checks(m,cfg):
    gt=Counter(g["type"] for g in cfg["proxy-groups"])
    actual={"proxy_providers":len(cfg["proxy-providers"]),"proxy_groups":len(cfg["proxy-groups"]),
            "rule_providers":len(cfg["rule-providers"]),"rules":len(cfg["rules"])}
    if actual!=EXPECTED_COUNTS:
        die(f"topology count drift: {actual}")
    if dict(gt)!=EXPECTED_GROUP_TYPES:
        die(f"group type drift: {dict(gt)}")
    smart=[g["name"] for g in cfg["proxy-groups"] if g["type"]=="smart"]
    if smart!=EXPECTED_SMART:
        die(f"Smart group drift: {smart}")
    if any(not g.get("uselightgbm") for g in cfg["proxy-groups"] if g["type"]=="smart"):
        die("all eight Smart groups must preserve uselightgbm=true")

    provider_names=set(cfg["proxy-providers"])
    group_names={g["name"] for g in cfg["proxy-groups"]}
    adapter_names=group_names | {p["name"] for p in BUILTIN_PROXIES} | {"DIRECT","REJECT","REJECT-DROP","GLOBAL","COMPATIBLE","PASS"}
    graph=defaultdict(set)
    dangling=[]
    for g in cfg["proxy-groups"]:
        for p in g.get("use",[]):
            if p not in provider_names: dangling.append(f"{g['name']} -> provider {p}")
        for p in g.get("proxies",[]):
            if p not in adapter_names: dangling.append(f"{g['name']} -> adapter {p}")
            elif p in group_names: graph[g["name"]].add(p)
    if dangling:
        die("dangling references: " + "; ".join(dangling[:8]))

    temp=set(); done=set()
    def visit(n,stack):
        if n in done: return
        if n in temp: die("group cycle: " + " -> ".join(stack+[n]))
        temp.add(n)
        for x in graph.get(n,()): visit(x,stack+[n])
        temp.remove(n); done.add(n)
    for n in group_names: visit(n,[])

    for name,rp in cfg["rule-providers"].items():
        if rp["proxy"]!="静态资源智能":
            die(f"rule provider {name} no longer bootstraps through 静态资源智能")

    for idx,raw in m["gold"]:
        if cfg["rules"][int(idx)]!=raw:
            die(f"golden rule drift at {idx}")

    return {
        "source_sha256":m.get("sha"),
        "counts":actual,
        "group_types":dict(gt),
        "smart_groups":smart,
        "rule_type_counts":dict(Counter(expand_types(m["rle"]))),
    }


def make_hermetic_runtime_cfg(cfg):
    # Preserve production Smart/geodata requirements in structural validation,
    # but keep the runtime shadow fully offline and reproducible.
    runtime=json.loads(json.dumps(cfg, ensure_ascii=False))
    for g in runtime["proxy-groups"]:
        if g.get("type")=="smart":
            g["uselightgbm"]=False
            g["prefer-asn"]=False
    for i,r in enumerate(runtime["rules"]):
        if r.startswith("GEOSITE,"):
            runtime["rules"][i]="DOMAIN-SUFFIX,shadow-geosite-cn.test,🇨🇳 本地直连"
        elif r.startswith("GEOIP,"):
            runtime["rules"][i]="IP-CIDR,203.0.113.0/24,🇨🇳 本地直连,no-resolve"

    # Production registers 71 rule providers but contains 70 RULE-SET rules.
    # Keep that exact fact in structural validation; only in the hermetic runtime
    # shadow, inject a temporary RULE-SET for each otherwise-unreferenced provider
    # so every provider is exercised through the real rule-loading path. This also
    # avoids the controller path-parameter limitation for names containing '/'.
    referenced=set()
    for r in runtime["rules"]:
        if r.startswith("RULE-SET,"):
            parts=r.split(",",3)
            if len(parts) >= 2:
                referenced.add(parts[1])
    missing=[name for name in runtime["rule-providers"] if name not in referenced]
    replaceable=[i for i,r in enumerate(runtime["rules"]) if r.startswith("DOMAIN,shadow-")]
    if len(replaceable) < len(missing):
        die("not enough placeholder DOMAIN rules to exercise unreferenced rule providers")
    for idx,name in zip(replaceable,missing):
        runtime["rules"][idx]=f"RULE-SET,{name},主力智能"
    runtime["_shadow_forced_rule_providers"]=missing
    return runtime

class State:
    lock=threading.Lock()
    provider_hits=Counter()
    rule_hits=Counter()
    fail_provider=None
    fail_rule=None
    behaviors={}

class Handler(BaseHTTPRequestHandler):
    def send_body(self,code,body=b"",ctype="text/plain"):
        self.send_response(code); self.send_header("Content-Type",ctype)
        self.send_header("Content-Length",str(len(body))); self.end_headers()
        if body: self.wfile.write(body)
    def do_GET(self):
        if self.path.startswith("/health/"):
            self.send_body(204); return
        m=re.match(r"^/providers/([^/?]+)\.yaml",self.path)
        if m:
            name=m.group(1)
            with State.lock:
                State.provider_hits[name]+=1; fail=(State.fail_provider==name)
            if fail: self.send_body(503,b"provider unavailable"); return
            body=yaml.safe_dump({"proxies":[{"name":f"{name}:{n}","type":"direct","udp":True} for n in REGION_NODES]},
                                allow_unicode=True,sort_keys=False).encode()
            self.send_body(200,body,"text/yaml"); return
        m=re.match(r"^/rules/([^/?]+)\.yaml",self.path)
        if m:
            name=m.group(1)
            with State.lock:
                State.rule_hits[name]+=1; fail=(State.fail_rule==name)
            if fail: self.send_body(503,b"rule unavailable"); return
            behavior=State.behaviors.get(name,"domain")
            payload=["203.0.113.0/24"] if behavior=="ipcidr" else [f"+.shadow-{abs(hash(name))%100000}.test"]
            body=yaml.safe_dump({"payload":payload},allow_unicode=True,sort_keys=False).encode()
            self.send_body(200,body,"text/yaml"); return
        self.send_body(404,b"not found")
    def log_message(self,*args): pass

def wait_controller():
    import urllib.request
    end=time.time()+15
    while time.time()<end:
        try:
            urllib.request.urlopen("http://127.0.0.1:29091/version",timeout=.5).read()
            return
        except Exception:
            time.sleep(.1)
    die("controller readiness timeout")

def get_json(url,method="GET"):
    import urllib.request
    req=urllib.request.Request(url,method=method)
    with urllib.request.urlopen(req,timeout=6) as r:
        raw=r.read()
        return json.loads(raw) if raw else None

def put_expect_503(url):
    import urllib.request, urllib.error
    try:
        urllib.request.urlopen(urllib.request.Request(url,method="PUT"),timeout=6)
        die(f"expected 503 from {url}")
    except urllib.error.HTTPError as e:
        if e.code!=503: die(f"expected 503, got {e.code} from {url}")

def runtime_checks(bin_path,cfg,work,art):
    work.mkdir(parents=True,exist_ok=True); art.mkdir(parents=True,exist_ok=True)
    (work/"providers").mkdir(exist_ok=True); (work/"rules").mkdir(exist_ok=True)
    forced_meta=list(cfg.get("_shadow_forced_rule_providers", []))
    runtime_cfg=json.loads(json.dumps(cfg, ensure_ascii=False))
    runtime_cfg.pop("_shadow_forced_rule_providers", None)
    y=work/"shadow.yaml"
    y.write_text(yaml.safe_dump(runtime_cfg,allow_unicode=True,sort_keys=False,width=240),encoding="utf-8")
    State.behaviors={f"rp-{i:03d}":v["behavior"] for i,(k,v) in enumerate(cfg["rule-providers"].items())}
    server=ThreadingHTTPServer(("127.0.0.1",33000),Handler)
    threading.Thread(target=server.serve_forever,daemon=True).start()
    test=subprocess.run([bin_path,"-t","-d",str(work),"-f",str(y)],text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
    (art/"config-test.log").write_text(test.stdout,encoding="utf-8")
    if test.returncode: die("generated 11/88/71/1357 shadow config failed parser test")
    log=open(art/"mihomo.log","w",encoding="utf-8")
    proc=subprocess.Popen([bin_path,"-d",str(work),"-f",str(y)],stdout=log,stderr=subprocess.STDOUT,text=True)
    try:
        wait_controller()
        expected_groups={g["name"] for g in cfg["proxy-groups"]}
        expected_providers=set(cfg["proxy-providers"])
        expected_rule_providers=set(cfg["rule-providers"])
        deadline=time.time()+18
        last_missing={}
        while time.time()<deadline:
            proxies=get_json("http://127.0.0.1:29091/proxies")
            providers=get_json("http://127.0.0.1:29091/providers/proxies")
            rproviders=get_json("http://127.0.0.1:29091/providers/rules")
            rules=get_json("http://127.0.0.1:29091/rules")
            proxy_names=set((proxies or {}).get("proxies") or {})
            provider_names=set((providers or {}).get("providers") or {})
            rule_provider_names=set((rproviders or {}).get("providers") or {})
            missing_groups=expected_groups-proxy_names
            missing_providers=expected_providers-provider_names
            missing_rule_providers=expected_rule_providers-rule_provider_names
            rule_count=len(((rules or {}).get("rules") or []))
            last_missing={
                "groups":sorted(missing_groups),
                "providers":sorted(missing_providers),
                "rule_providers":sorted(missing_rule_providers),
                "rules":rule_count,
                "provider_api_total":len(provider_names),
                "rule_provider_api_total":len(rule_provider_names),
            }
            if not missing_groups and not missing_providers and not missing_rule_providers and rule_count==1357:
                break
            time.sleep(.2)
        (art/"runtime-readiness.json").write_text(json.dumps(last_missing,ensure_ascii=False,indent=2))
        (art/"proxies.json").write_text(json.dumps(proxies,ensure_ascii=False,indent=2))
        (art/"providers.json").write_text(json.dumps(providers,ensure_ascii=False,indent=2))
        (art/"rule-providers.json").write_text(json.dumps(rproviders,ensure_ascii=False,indent=2))
        (art/"rules.json").write_text(json.dumps(rules,ensure_ascii=False,indent=2))
        if last_missing["groups"]:
            die("runtime missing groups after convergence: " + ", ".join(last_missing["groups"][:8]))
        if last_missing["providers"]:
            die("runtime missing proxy providers after convergence: " + ", ".join(last_missing["providers"][:8]))
        if last_missing["rule_providers"]:
            die("runtime missing rule providers after convergence: " + ", ".join(last_missing["rule_providers"][:8]))
        if last_missing["rules"]!=1357:
            die(f"runtime rule count != 1357 after convergence: {last_missing['rules']}")
        forced_names=forced_meta
        (art/"runtime-forced-rule-provider-refs.json").write_text(json.dumps(forced_names,ensure_ascii=False,indent=2))
        for _ in range(120):
            with State.lock:
                ph=len(State.provider_hits); rh=len(State.rule_hits)
            if ph==11 and rh==71: break
            time.sleep(.1)
        with State.lock:
            ph=dict(State.provider_hits); rh=dict(State.rule_hits)
        (art/"mock-hits.json").write_text(json.dumps({
            "proxy":ph,
            "rules":rh,
        },ensure_ascii=False,indent=2))
        if len(ph)!=11: die(f"only {len(ph)}/11 proxy providers fetched")
        if len(rh)!=71: die(f"only {len(rh)}/71 rule providers fetched through rule-loading path")

        target=next(iter(cfg["proxy-providers"]))
        with State.lock: State.fail_provider=target
        put_expect_503(f"http://127.0.0.1:29091/providers/proxies/{target}")
        with State.lock: State.fail_provider=None
        pstate=get_json("http://127.0.0.1:29091/providers/proxies")["providers"][target]
        if not pstate.get("proxies"): die("provider failure destroyed last-known-good state")
        get_json(f"http://127.0.0.1:29091/providers/proxies/{target}",method="PUT")

        rtarget=next(iter(cfg["rule-providers"]))
        with State.lock: State.fail_rule=rtarget
        put_expect_503(f"http://127.0.0.1:29091/providers/rules/{rtarget}")
        with State.lock: State.fail_rule=None
        get_json(f"http://127.0.0.1:29091/providers/rules/{rtarget}",method="PUT")

        summary={"groups_loaded":88,"proxy_providers_loaded":11,"rule_providers_loaded":71,
                 "rules_loaded":1357,"smart_rule_bootstrap":"pass",
                 "provider_last_good_containment":"pass","rule_provider_recovery":"pass",
                 "external_downloads":"disabled-in-shadow-runtime"}
        (art/"runtime-summary.json").write_text(json.dumps(summary,ensure_ascii=False,indent=2))
        return summary
    finally:
        proc.terminate()
        try: proc.wait(timeout=4)
        except subprocess.TimeoutExpired: proc.kill(); proc.wait()
        log.close(); server.shutdown(); server.server_close()

def main():
    ap=argparse.ArgumentParser()
    ap.add_argument("--manifest",required=True)
    ap.add_argument("--bin",required=True)
    ap.add_argument("--work",default=".real-shadow-state")
    ap.add_argument("--artifacts",default=".real-shadow-artifacts")
    a=ap.parse_args()
    m=load_manifest(a.manifest)
    cfg=build_shadow(m)
    art=Path(a.artifacts); art.mkdir(parents=True,exist_ok=True)
    structural=structural_checks(m,cfg)
    (art/"structural-summary.json").write_text(json.dumps(structural,ensure_ascii=False,indent=2))
    runtime_cfg=make_hermetic_runtime_cfg(cfg)
    runtime=runtime_checks(a.bin,runtime_cfg,Path(a.work),art)
    print(json.dumps({"structural":structural,"runtime":runtime},ensure_ascii=False,indent=2))
    print("REAL_CONFIG_SHADOW_PASS")

if __name__=="__main__":
    main()
