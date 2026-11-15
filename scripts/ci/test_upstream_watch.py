"""Offline safety regressions: persistent state, deduplication, retry and no main writes."""
import base64
import importlib.util
import json
import tempfile
import unittest
from pathlib import Path

path = Path(__file__).with_name("upstream_watch.py")
spec = importlib.util.spec_from_file_location("upstream_watch", path)
watch = importlib.util.module_from_spec(spec)
spec.loader.exec_module(watch)

A = "a" * 40
B = "b" * 40
SOURCES = [
    {"name": "official-alpha", "repo": "MetaCubeX/mihomo", "kind": "branch", "ref": "Alpha", "priority": "critical"},
    {"name": "official-release", "repo": "MetaCubeX/mihomo", "kind": "release", "priority": "high"},
]


class FakeGitHub:
    repo = "Zhanfg/mihomo"

    def __init__(self):
        self.head = A
        self.release = "v1.19.34"
        self.branch = False
        self.state = None
        self.writes = []
        self.dispatches = []
        self.comments = []
        self.fail_upstream = False
        self.fail_dispatch_once = False

    def call(self, method, path, data=None):
        if method == "GET" and path.endswith("/branches/Alpha"):
            if self.fail_upstream:
                raise RuntimeError("simulated upstream outage")
            return {"commit": {"sha": self.head}}
        if method == "GET" and path.endswith("/releases/latest"):
            return {"tag_name": self.release}
        if method == "GET" and "/git/ref/heads/automation/upstream-watch-state" in path:
            if not self.branch:
                raise watch.APIError(method, path, 404)
            return {"object": {"sha": A}}
        if method == "GET" and path.endswith("/git/ref/heads/main"):
            return {"object": {"sha": A}}
        if method == "POST" and path.endswith("/git/refs"):
            self.branch = True
            self.writes.append(("create_branch", data))
            return {"ref": data["ref"]}
        if method == "GET" and "/contents/" in path:
            if self.state is None:
                raise watch.APIError(method, path, 404)
            return {"sha": B, "content": base64.b64encode(json.dumps(self.state).encode()).decode()}
        if method == "PUT" and "/contents/" in path:
            self.assert_branch(data)
            self.state = json.loads(base64.b64decode(data["content"]))
            self.writes.append(("save_state", data))
            return {"content": {"sha": B}}
        if method == "POST" and "/actions/workflows/" in path:
            if self.fail_dispatch_once:
                self.fail_dispatch_once = False
                raise RuntimeError("simulated actions failure")
            self.dispatches.append((path, data))
            return None
        if method == "POST" and path.endswith("/issues/27/comments"):
            self.comments.append(data["body"])
            return {"id": 1}
        raise AssertionError("unexpected API: " + method + " " + path)

    @staticmethod
    def assert_branch(data):
        assert data["branch"] == watch.STATE_BRANCH, "state must not target main"


class WatchTests(unittest.TestCase):
    def test_manifest_valid(self):
        sources = watch.load_sources()
        self.assertGreaterEqual(len(sources), 10)
        self.assertEqual(len({x["name"] for x in sources}), len(sources))

    def test_reject_duplicate_sources(self):
        with tempfile.TemporaryDirectory() as root:
            f = Path(root) / "sources.json"
            f.write_text(json.dumps([SOURCES[0], SOURCES[0]]))
            with self.assertRaisesRegex(ValueError, "duplicate"):
                watch.load_sources(f)

    def test_initial_snapshot_isolated_branch_no_ci(self):
        gh = FakeGitHub()
        self.assertEqual(watch.watch(gh, SOURCES), [])
        self.assertTrue(gh.branch)
        self.assertEqual(gh.state["heads"]["official-alpha"], A)
        self.assertEqual(len(gh.writes), 2)
        self.assertEqual(gh.dispatches, [])
        self.assertEqual(gh.comments, [])

    def test_no_change_no_new_commits_or_ci(self):
        gh = FakeGitHub()
        watch.watch(gh, SOURCES)
        watch.watch(gh, SOURCES)
        self.assertEqual(len(gh.writes), 2)
        self.assertFalse(gh.dispatches)

    def test_change_records_diff_and_builds_existing_p1(self):
        gh = FakeGitHub()
        watch.watch(gh, SOURCES)
        gh.head = B
        changed = watch.watch(gh, SOURCES)
        self.assertEqual(changed[0]["old"], A)
        self.assertEqual(changed[0]["new"], B)
        self.assertEqual(len(gh.dispatches), 2)
        self.assertTrue(all(data["ref"] == watch.P1_BRANCH for _, data in gh.dispatches))
        self.assertEqual(len(gh.comments), 1)
        self.assertIn("/compare/" + A + "..." + B, gh.comments[0])
        self.assertFalse(gh.state["pending_ci"])
        writes = len(gh.writes)
        watch.watch(gh, SOURCES)
        self.assertEqual(len(gh.writes), writes)

    def test_release_detection_without_dispatch(self):
        gh = FakeGitHub()
        watch.watch(gh, SOURCES)
        gh.release = "v1.19.35"
        changed = watch.watch(gh, SOURCES, do_dispatch=False)
        self.assertEqual(changed[0]["name"], "official-release")
        self.assertIn("/releases/tag/v1.19.35", gh.comments[-1])
        self.assertEqual(len(gh.dispatches), 0)
        self.assertTrue(gh.state["pending_ci"])

    def test_api_failure_before_any_mutation(self):
        gh = FakeGitHub()
        watch.watch(gh, SOURCES)
        writes = len(gh.writes)
        gh.fail_upstream = True
        with self.assertRaisesRegex(RuntimeError, "upstream outage"):
            watch.watch(gh, SOURCES)
        self.assertEqual(len(gh.writes), writes)

    def test_dispatch_failure_keeps_retry_marker(self):
        gh = FakeGitHub()
        watch.watch(gh, SOURCES)
        gh.head = B
        gh.fail_dispatch_once = True
        with self.assertRaisesRegex(RuntimeError, "actions failure"):
            watch.watch(gh, SOURCES)
        self.assertTrue(gh.state["pending_ci"])
        watch.watch(gh, SOURCES)
        self.assertFalse(gh.state["pending_ci"])
        self.assertEqual(len(gh.dispatches), 2)

    def test_corrupt_state_not_overwritten(self):
        gh = FakeGitHub()
        gh.branch = True
        gh.state = {"version": 9, "heads": {}, "history": []}
        with self.assertRaisesRegex(ValueError, "malformed"):
            watch.watch(gh, SOURCES)
        self.assertFalse(gh.writes)


if __name__ == "__main__":
    unittest.main()
