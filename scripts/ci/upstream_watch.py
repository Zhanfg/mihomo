#!/usr/bin/env python3
"""Monitor verified upstream heads; never merge upstream code or publish builds."""
import argparse
import json
import os
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime, timezone
from pathlib import Path

SOURCES_FILE = Path(__file__).with_name("upstream_sources.json")
MARKER = "<!-- mihomo-upstream-watch:v1 -->"
STATE_BEGIN = "<!-- mihomo-upstream-state:v1\n"
STATE_END = "\n-->"
TITLE = "[upstream-watch] 多上游同步候选队列（需审计和真机验收）"
P1 = "sync/p1-multisource-20261108"
BUILDS = ("androidarm64.yml", "boxproxy-native-ebpf-android.yml")
SHA = re.compile(r"^[0-9a-f]{40}$")
REPO = re.compile(r"^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$")
REF = re.compile(r"^[A-Za-z0-9._/-]+$")


def load_sources(path=SOURCES_FILE):
    sources = json.loads(Path(path).read_text(encoding="utf-8"))
    if not isinstance(sources, list) or not sources:
        raise ValueError("empty source manifest")
    names = set()
    for source in sources:
        name = source["name"]
        if not re.fullmatch(r"[a-z][a-z0-9_-]*", name) or name in names:
            raise ValueError("invalid or duplicated source name")
        names.add(name)
        if not REPO.fullmatch(source["repo"]):
            raise ValueError("invalid upstream repo")
        if source["kind"] not in ("branch", "release"):
            raise ValueError("invalid source kind")
        if source["kind"] == "branch" and not REF.fullmatch(source.get("ref", "")):
            raise ValueError("invalid upstream ref")
    return sources


class GitHub:
    def __init__(self, token, repo):
        if not REPO.fullmatch(repo):
            raise ValueError("invalid target repository")
        self.token, self.repo = token, repo

    def request(self, method, path, data=None):
        url = "https://api.github.com" + path
        payload = None if data is None else json.dumps(data).encode("utf-8")
        headers = {
            "Accept": "application/vnd.github+json",
            "Authorization": "Bearer " + self.token,
            "X-GitHub-Api-Version": "2022-11-28",
            "User-Agent": "mihomo-upstream-watch",
        }
        if payload is not None:
            headers["Content-Type"] = "application/json"
        for attempt in range(3):
            req = urllib.request.Request(url, method=method, headers=headers, data=payload)
            try:
                with urllib.request.urlopen(req, timeout=20) as response:
                    raw = response.read()
                    return json.loads(raw) if raw else None
            except urllib.error.HTTPError as error:
                if error.code in (429, 502, 503, 504) and attempt < 2:
                    time.sleep(attempt + 1)
                    continue
                raise RuntimeError("GitHub API failed: %s %s HTTP %s" % (method, path, error.code)) from error
            except (urllib.error.URLError, TimeoutError) as error:
                if attempt < 2:
                    time.sleep(attempt + 1)
                    continue
                raise RuntimeError("GitHub connection error: %s" % path) from error
        raise RuntimeError("upstream request exhausted retries")


def read_head(gh, source):
    upstream = source["repo"]
    if source["kind"] == "release":
        result = gh.request("GET", "/repos/%s/releases/latest" % upstream)
        tag = result["tag_name"]
        if not isinstance(tag, str) or not tag or len(tag) > 100:
            raise ValueError("invalid release tag")
        return tag
    branch = urllib.parse.quote(source["ref"], safe="")
    result = gh.request("GET", "/repos/%s/branches/%s" % (upstream, branch))
    sha = result["commit"]["sha"]
    if not SHA.fullmatch(sha):
        raise ValueError("invalid upstream SHA")
    return sha


def find_queue(gh):
    for page in range(1, 11):
        issues = gh.request("GET", "/repos/%s/issues?state=open&per_page=100&page=%d" % (gh.repo, page))
        for issue in issues:
            if issue.get("title") == TITLE and MARKER in (issue.get("body") or ""):
                return issue
        if len(issues) < 100:
            return None
    raise RuntimeError("queue not found in first 1000 issues; refusing to create duplicate")


def read_state(body):
    start = body.find(STATE_BEGIN)
    if start < 0:
        raise ValueError("managed issue missing state marker")
    start += len(STATE_BEGIN)
    end = body.find(STATE_END, start)
    if end < 0:
        raise ValueError("truncated managed issue marker")
    state = json.loads(body[start:end])
    if not isinstance(state.get("heads"), dict) or not isinstance(state.get("history"), list):
        raise ValueError("invalid managed issue state")
    return state


def source_url(source, old, new):
    root = "https://github.com/" + source["repo"]
    if source["kind"] == "release":
        return root + "/releases/tag/" + urllib.parse.quote(new, safe="")
    if old and SHA.fullmatch(old):
        return root + "/compare/" + old + "..." + new
    return root + "/commit/" + new


def render(sources, state, stamp, repo):
    lines = [
        MARKER, "",
        "此队列记录上游更新，绝不表示更新已经合并或完成设备验证。",
        "检测变化后自动重新构建现有 P1 候选基线；新上游代码必须经人工审计、独立 Draft PR 和完整 CI 才能集成。",
        "",
        "最近检查（UTC）：%s" % stamp, "",
        "| 来源 | 关注程度 | 最近发现版本 | 入口 |",
        "| --- | --- | --- | --- |",
    ]
    for src in sources:
        head = state["heads"][src["name"]]
        value = head[:12] if src["kind"] == "branch" else head
        lines.append("| %s (%s) | %s | %s | [查看](%s) |" % (
            src["repo"], src["name"], src["priority"], value, source_url(src, "", head)
        ))
    lines.extend(["", "关注程度不代表 P0/P1/P2 清单完成状态。", "", "## 最近变化", ""])
    history = state["history"][-30:]
    if not history:
        lines.append("首次运行建立观察基线，后续更新才算新增事件。")
    for item in reversed(history):
        src = next(s for s in sources if s["name"] == item["name"])
        lines.append("- %s %s: %s -> %s ([比较](%s))" % (
            item["time"], item["name"], item["old"][:12], item["new"][:12],
            source_url(src, item["old"], item["new"])
        ))
    lines.extend([
        "", "## 必须满足的门禁", "",
        "1. 对照 P1 13 项、P2 9 项及后续 P3 审计分类，选择性移植，禁止无审计自动合并。",
        "2. 代码变更的最新 SHA 需通过 Android ARM64、Smart/eBPF、跨平台测试及 eBPF 对象校验。",
        "3. 真正的特权 eBPF attach、Android 17 网络切换、Doze 和长连接必须真机验收。",
        "", "[P0 #26](https://github.com/%s/pull/26) | [P1 #27](https://github.com/%s/pull/27)" % (repo, repo),
        "", STATE_BEGIN + json.dumps(state, sort_keys=True, separators=(",", ":")) + STATE_END
    ])
    return "\n".join(lines)


def output(changed, issue_url):
    summary = os.environ.get("GITHUB_STEP_SUMMARY")
    if summary:
        with open(summary, "a", encoding="utf-8") as f:
            f.write("## Upstream check\n\nChanged sources: %d\n\nQueue: %s\n\n" % (len(changed), issue_url))
            f.write("Dispatched builds test pre-existing P1 code, not newly integrated upstream commits.\n")
    print("Changed sources: %s. Queue: %s" % (", ".join(changed) or "none", issue_url))


def dispatch_p1(gh):
    for workflow in BUILDS:
        gh.request("POST", "/repos/%s/actions/workflows/%s/dispatches" % (gh.repo, workflow), {"ref": P1})
        print("Requested P1 baseline check: " + workflow)


def run(gh, sources, dispatch=True):
    stamp = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M:%S")
    # Fetch every source first. On any API failure, do not rewrite queue state.
    heads = {src["name"]: read_head(gh, src) for src in sources}
    issue = find_queue(gh)
    old = read_state(issue["body"]) if issue else {"heads": {}, "history": [], "pending_ci": False}
    changes = []
    if issue:
        for src in sources:
            name = src["name"]
            prev = old["heads"].get(name)
            if prev is not None and prev != heads[name]:
                changes.append({"name": name, "old": prev, "new": heads[name], "time": stamp})
    pending = bool(changes) or old.get("pending_ci", False)
    state = {
        "heads": heads,
        "history": (old["history"] + changes)[-30:],
        "pending_ci": pending,
    }
    if not issue or changes or old["heads"] != heads:
        body = render(sources, state, stamp, gh.repo)
        if issue:
            issue = gh.request("PATCH", "/repos/%s/issues/%s" % (gh.repo, issue["number"]), {"body": body})
        else:
            issue = gh.request("POST", "/repos/%s/issues" % gh.repo, {"title": TITLE, "body": body})
    if pending and dispatch:
        # Persist pending=true before dispatch so an API failure retries on next poll.
        dispatch_p1(gh)
        state["pending_ci"] = False
        body = render(sources, state, stamp, gh.repo)
        issue = gh.request("PATCH", "/repos/%s/issues/%s" % (gh.repo, issue["number"]), {"body": body})
    output([x["name"] for x in changes], issue["html_url"])
    return changes


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true", help="validate manifest only")
    parser.add_argument("--no-dispatch", action="store_true")
    args = parser.parse_args()
    sources = load_sources()
    if args.check:
        print("Validated %d upstream sources" % len(sources))
        return 0
    token = os.environ.get("GH_TOKEN")
    repo = os.environ.get("GITHUB_REPOSITORY")
    if not token or not repo:
        parser.error("GH_TOKEN and GITHUB_REPOSITORY are required")
    run(GitHub(token, repo), sources, dispatch=not args.no_dispatch)
    return 0


if __name__ == "__main__":
    sys.exit(main())
