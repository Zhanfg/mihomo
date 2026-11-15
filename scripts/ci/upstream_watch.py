#!/usr/bin/env python3
"""Poll vetted upstreams; store state on an isolated branch, never merge code."""
import argparse
import base64
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

MANIFEST = Path(__file__).with_name("upstream_sources.json")
STATE_BRANCH = "automation/upstream-watch-state"
STATE_PATH = ".github/upstream/watch-state.json"
P1_BRANCH = "sync/p1-multisource-20261108"
BASELINE_CI = ("androidarm64.yml", "boxproxy-native-ebpf-android.yml")
SHA = re.compile(r"^[a-f0-9]{40}$")
REPO = re.compile(r"^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$")
REF = re.compile(r"^[A-Za-z0-9._/-]+$")


def load_sources(path=MANIFEST):
    sources = json.loads(Path(path).read_text(encoding="utf-8"))
    if not isinstance(sources, list) or not sources:
        raise ValueError("empty upstream manifest")
    names = set()
    for src in sources:
        name = src["name"]
        if not re.fullmatch(r"[a-z][a-z0-9_-]*", name) or name in names:
            raise ValueError("invalid/duplicate upstream name")
        names.add(name)
        if not REPO.fullmatch(src["repo"]) or src["kind"] not in ("branch", "release"):
            raise ValueError("invalid upstream source")
        if src["kind"] == "branch" and not REF.fullmatch(src.get("ref", "")):
            raise ValueError("invalid upstream branch")
    return sources


class APIError(RuntimeError):
    def __init__(self, method, path, status):
        super().__init__("GitHub API %s %s: HTTP %s" % (method, path, status))
        self.status = status


class GitHub:
    def __init__(self, token, repo):
        if not REPO.fullmatch(repo):
            raise ValueError("invalid GITHUB_REPOSITORY")
        self.token, self.repo = token, repo

    def call(self, method, path, data=None):
        url = "https://api.github.com" + path
        payload = None if data is None else json.dumps(data).encode("utf-8")
        headers = {
            "Accept": "application/vnd.github+json",
            "Authorization": "Bearer " + self.token,
            "X-GitHub-Api-Version": "2022-11-28",
            "User-Agent": "mihomo-safe-upstream-watch",
        }
        if payload is not None:
            headers["Content-Type"] = "application/json"
        for attempt in range(3):
            req = urllib.request.Request(url, method=method, data=payload, headers=headers)
            try:
                with urllib.request.urlopen(req, timeout=20) as reply:
                    raw = reply.read()
                    return json.loads(raw) if raw else None
            except urllib.error.HTTPError as err:
                if err.code in (429, 502, 503, 504) and attempt < 2:
                    time.sleep(attempt + 1)
                    continue
                raise APIError(method, path, err.code) from err
            except (urllib.error.URLError, TimeoutError) as err:
                if attempt < 2:
                    time.sleep(attempt + 1)
                    continue
                raise RuntimeError("GitHub transport failure for " + path) from err
        raise RuntimeError("retry limit exceeded")


def current_head(gh, source):
    repo = source["repo"]
    if source["kind"] == "release":
        data = gh.call("GET", "/repos/%s/releases/latest" % repo)
        tag = data["tag_name"]
        if not isinstance(tag, str) or not tag or len(tag) > 100:
            raise ValueError("invalid release tag")
        return tag
    branch = urllib.parse.quote(source["ref"], safe="")
    data = gh.call("GET", "/repos/%s/branches/%s" % (repo, branch))
    sha = data["commit"]["sha"]
    if not SHA.fullmatch(sha):
        raise ValueError("invalid upstream SHA")
    return sha


def ensure_state_branch(gh):
    endpoint = "/repos/%s/git/ref/heads/%s" % (gh.repo, STATE_BRANCH)
    try:
        gh.call("GET", endpoint)
        return
    except APIError as err:
        if err.status != 404:
            raise
    base = gh.call("GET", "/repos/%s/git/ref/heads/main" % gh.repo)
    gh.call("POST", "/repos/%s/git/refs" % gh.repo, {
        "ref": "refs/heads/" + STATE_BRANCH,
        "sha": base["object"]["sha"],
    })


def read_state(gh):
    branch = urllib.parse.quote(STATE_BRANCH, safe="")
    endpoint = "/repos/%s/contents/%s?ref=%s" % (gh.repo, STATE_PATH, branch)
    try:
        record = gh.call("GET", endpoint)
    except APIError as err:
        if err.status == 404:
            return None, None
        raise
    raw = base64.b64decode(record["content"], validate=False)
    state = json.loads(raw)
    if (state.get("version") != 1 or not isinstance(state.get("heads"), dict)
            or not isinstance(state.get("history"), list)):
        raise ValueError("managed state malformed; refusing to overwrite")
    return state, record["sha"]


def save_state(gh, state, blob_sha):
    data = {
        "branch": STATE_BRANCH,
        "message": "ci(upstream): record reviewed upstream watch snapshot",
        "content": base64.b64encode(
            (json.dumps(state, ensure_ascii=False, indent=2) + "\n").encode("utf-8")
        ).decode("ascii"),
    }
    if blob_sha:
        data["sha"] = blob_sha
    record = gh.call("PUT", "/repos/%s/contents/%s" % (gh.repo, STATE_PATH), data)
    return record["content"]["sha"]


def link_for(source, old, new):
    root = "https://github.com/" + source["repo"]
    if source["kind"] == "release":
        return root + "/releases/tag/" + urllib.parse.quote(new, safe="")
    if old and SHA.fullmatch(old):
        return root + "/compare/" + old + "..." + new
    return root + "/commit/" + new


def report(sources, changed, heads, repo):
    lines = [
        "## Upstream watch (review required)", "",
        "CI monitoring is not an automatic source merge or production approval.",
        "State: https://github.com/%s/blob/%s/%s" % (repo, STATE_BRANCH, STATE_PATH),
        "", "| Source | Priority | Observed ref |",
        "| --- | --- | --- |",
    ]
    for src in sources:
        value = heads[src["name"]]
        display = value[:12] if src["kind"] == "branch" else value
        lines.append("| %s (%s) | %s | %s |" % (
            src["repo"], src["name"], src["priority"], display
        ))
    lines.extend(["", "### Detected changes", ""])
    if not changed:
        lines.append("None: initial baseline or unchanged refs.")
    for entry in changed:
        src = next(x for x in sources if x["name"] == entry["name"])
        lines.append("- %s: %s -> %s ([compare](%s))" % (
            entry["name"], entry["old"][:12], entry["new"][:12],
            link_for(src, entry["old"], entry["new"])
        ))
    lines.extend([
        "", "Triggered builds check EXISTING P1 code only; upstream changes remain unmerged.",
        "Release gate: current-SHA Android ARM64, Smart/eBPF, cross-platform tests,",
        "privileged eBPF attach and OnePlus 13 Android 17 network/Doze/soak verification.",
    ])
    return "\n".join(lines)


def dispatch_existing_p1(gh):
    for workflow in BASELINE_CI:
        gh.call("POST", "/repos/%s/actions/workflows/%s/dispatches" % (gh.repo, workflow), {
            "ref": P1_BRANCH
        })
        print("Dispatched existing P1 baseline, not new upstream code: " + workflow)


def announce(gh, markdown):
    # A PR supports issue-style comments even when repository Issues are disabled.
    # Notification is best-effort: permanent state and CI do not depend on it.
    try:
        gh.call("POST", "/repos/%s/issues/27/comments" % gh.repo, {"body": markdown})
    except APIError as err:
        print("PR #27 notification unavailable: " + str(err), file=sys.stderr)


def watch(gh, sources, do_dispatch=True):
    observed_at = datetime.now(timezone.utc).isoformat(timespec="seconds")
    # Read EVERY upstream before any writable operation; fail closed on errors.
    heads = {src["name"]: current_head(gh, src) for src in sources}
    ensure_state_branch(gh)
    state, blob_sha = read_state(gh)
    initial = state is None
    old_heads = dict(state["heads"]) if state else {}
    if initial:
        state = {"version": 1, "heads": {}, "history": [], "pending_ci": False}
    changed = []
    if not initial:
        for src in sources:
            name = src["name"]
            old = state["heads"].get(name)
            if old is not None and old != heads[name]:
                changed.append({"name": name, "old": old, "new": heads[name],
                                "time": observed_at})
    pending = bool(changed) or bool(state.get("pending_ci", False))
    state = {
        "version": 1, "heads": heads, "history": (state["history"] + changed)[-30:],
        "pending_ci": pending, "last_changed": observed_at if changed else state.get("last_changed"),
    }
    # Save only real changes; no commit every five minutes.
    if initial or changed or pending or heads != old_heads:
        blob_sha = save_state(gh, state, blob_sha)
    summary = report(sources, changed, heads, gh.repo)
    if changed:
        announce(gh, summary)
    if pending and do_dispatch:
        # If dispatch fails, persisted pending_ci is retried on the next poll.
        dispatch_existing_p1(gh)
        state["pending_ci"] = False
        save_state(gh, state, blob_sha)
    path = os.environ.get("GITHUB_STEP_SUMMARY")
    if path:
        with open(path, "a", encoding="utf-8") as output:
            output.write(summary + "\n")
    print("Observed %d upstreams, changed %d, baseline %s" % (
        len(sources), len(changed), initial
    ))
    return changed


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true")
    parser.add_argument("--no-dispatch", action="store_true")
    args = parser.parse_args()
    sources = load_sources()
    if args.check:
        print("Validated %d upstream sources" % len(sources))
        return 0
    token, repo = os.environ.get("GH_TOKEN"), os.environ.get("GITHUB_REPOSITORY")
    if not token or not repo:
        parser.error("GH_TOKEN and GITHUB_REPOSITORY must be set")
    watch(GitHub(token, repo), sources, not args.no_dispatch)
    return 0


if __name__ == "__main__":
    sys.exit(main())
