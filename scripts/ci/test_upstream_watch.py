"""No-network tests for safe upstream polling and CI gating."""
import importlib.util
import json
import tempfile
import unittest
from pathlib import Path

p = Path(__file__).with_name("upstream_watch.py")
spec = importlib.util.spec_from_file_location("upstream_watch", p)
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
        self.issue = None
        self.writes = []
        self.dispatches = []
        self.fail_upstream = False
        self.fail_dispatch_once = False

    def request(self, method, path, data=None):
        if method == "GET" and path.endswith("/branches/Alpha"):
            if self.fail_upstream:
                raise RuntimeError("simulated upstream outage")
            return {"commit": {"sha": self.head}}
        if method == "GET" and path.endswith("/releases/latest"):
            return {"tag_name": self.release}
        if method == "GET" and "/issues?" in path:
            return [self.issue] if self.issue else []
        if method == "POST" and path.endswith("/issues"):
            self.writes.append((method, data))
            self.issue = {"number": 17, "title": data["title"], "body": data["body"],
                          "html_url": "https://github.com/Zhanfg/mihomo/issues/17"}
            return self.issue
        if method == "PATCH" and path.endswith("/issues/17"):
            self.writes.append((method, data))
            self.issue["body"] = data["body"]
            return self.issue
        if method == "POST" and "/actions/workflows/" in path:
            if self.fail_dispatch_once:
                self.fail_dispatch_once = False
                raise RuntimeError("simulated actions failure")
            self.dispatches.append((path, data))
            return None
        raise AssertionError("unexpected request: %s %s" % (method, path))


class WatchTests(unittest.TestCase):
    def test_manifest(self):
        sources = watch.load_sources()
        self.assertGreaterEqual(len(sources), 8)
        self.assertEqual(len(set(x["name"] for x in sources)), len(sources))

    def test_duplicate_sources_rejected(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / "sources.json"
            path.write_text(json.dumps([SOURCES[0], SOURCES[0]]))
            with self.assertRaises(ValueError):
                watch.load_sources(path)

    def test_initial_snapshot_does_not_build(self):
        gh = FakeGitHub()
        self.assertEqual(watch.run(gh, SOURCES), [])
        self.assertEqual(len(gh.writes), 1)
        self.assertEqual(gh.dispatches, [])
        self.assertEqual(watch.read_state(gh.issue["body"])["heads"]["official-alpha"], A)

    def test_unchanged_upstream_does_not_write_or_build(self):
        gh = FakeGitHub()
        watch.run(gh, SOURCES)
        watch.run(gh, SOURCES)
        self.assertEqual(len(gh.writes), 1)
        self.assertFalse(gh.dispatches)

    def test_source_change_triggers_two_baseline_builds(self):
        gh = FakeGitHub()
        watch.run(gh, SOURCES)
        gh.head = B
        changes = watch.run(gh, SOURCES)
        self.assertEqual(changes[0]["old"], A)
        self.assertEqual(changes[0]["new"], B)
        self.assertEqual(len(gh.dispatches), len(watch.BUILDS))
        self.assertTrue(all(data["ref"] == watch.P1 for _, data in gh.dispatches))
        self.assertIn("/compare/" + A + "..." + B, gh.issue["body"])
        watch.run(gh, SOURCES)
        self.assertEqual(len(gh.dispatches), len(watch.BUILDS))

    def test_release_tag_change_is_observed(self):
        gh = FakeGitHub()
        watch.run(gh, SOURCES)
        gh.release = "v1.19.35"
        changes = watch.run(gh, SOURCES, dispatch=False)
        self.assertEqual(changes[0]["name"], "official-release")
        self.assertIn("/releases/tag/v1.19.35", gh.issue["body"])
        self.assertFalse(gh.dispatches)

    def test_upstream_outage_does_not_write_state(self):
        gh = FakeGitHub()
        watch.run(gh, SOURCES)
        gh.fail_upstream = True
        with self.assertRaisesRegex(RuntimeError, "upstream outage"):
            watch.run(gh, SOURCES)
        self.assertEqual(len(gh.writes), 1)

    def test_failed_build_dispatch_retries(self):
        gh = FakeGitHub()
        watch.run(gh, SOURCES)
        gh.head = B
        gh.fail_dispatch_once = True
        with self.assertRaisesRegex(RuntimeError, "actions failure"):
            watch.run(gh, SOURCES)
        self.assertTrue(watch.read_state(gh.issue["body"])["pending_ci"])
        watch.run(gh, SOURCES)
        self.assertFalse(watch.read_state(gh.issue["body"])["pending_ci"])
        self.assertEqual(len(gh.dispatches), len(watch.BUILDS))

    def test_corrupt_state_is_not_overwritten(self):
        gh = FakeGitHub()
        gh.issue = {"number": 17, "title": watch.TITLE,
                    "body": watch.MARKER + " broken",
                    "html_url": "https://github.com/Zhanfg/mihomo/issues/17"}
        with self.assertRaisesRegex(ValueError, "missing state"):
            watch.run(gh, SOURCES)
        self.assertEqual(len(gh.writes), 0)


if __name__ == "__main__":
    unittest.main()
