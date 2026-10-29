import hashlib
import json
from pathlib import Path
import subprocess
import tempfile
import unittest

from release import asset_names, plan, publish


REPO = "example/memory-mcp"
TAG = "v0.1.1"


class FakeGitHub:
    def __init__(self, assets=None):
        self.assets = dict(assets or {})
        self.calls = []
        self.fail_on = None

    def __call__(self, *args):
        self.calls.append(args)
        action = args[1]
        if action == "list":
            return json.dumps([{"tagName": TAG}])
        if action == "view":
            return json.dumps({"tagName": TAG, "isDraft": False,
                               "assets": [{"name": name} for name in self.assets]})
        if action == "download":
            name = args[args.index("--pattern") + 1]
            directory = Path(args[args.index("--dir") + 1])
            (directory / name).write_bytes(self.assets[name])
            return ""
        if action == "upload":
            path = Path(args[3])
            if path.name == self.fail_on:
                raise subprocess.CalledProcessError(1, args, stderr="temporary upload failure")
            if path.name in self.assets and "--clobber" not in args:
                raise AssertionError("Duplicate archive upload")
            if "--clobber" in args and path.name != "SHA256SUMS":
                raise AssertionError("Existing binary was overwritten")
            self.assets[path.name] = path.read_bytes()
            return ""
        raise AssertionError(args)


def build_fixture(directory):
    directory = Path(directory)
    rows = []
    for name in asset_names("0.1.1")[:-1]:
        data = ("new build: " + name).encode()
        (directory / name).write_bytes(data)
        rows.append(f"{hashlib.sha256(data).hexdigest()}  {name}\n")
    (directory / "SHA256SUMS").write_text("".join(rows))


class ReleaseTests(unittest.TestCase):
    def test_main_repairs_manual_release_using_tag_not_default_version(self):
        result = plan("push", {}, "refs/heads/main", "a" * 40, REPO, FakeGitHub())
        self.assertEqual(result, {"tag": TAG, "version": "0.1.1", "source_ref": TAG, "publish": "true"})

    def test_complete_release_is_not_republished_on_main(self):
        cli = FakeGitHub({name: b"existing" for name in asset_names("0.1.1")})
        result = plan("push", {}, "refs/heads/main", "a" * 40, REPO, cli)
        self.assertEqual(result["publish"], "false")
        self.assertEqual(result["source_ref"], "a" * 40)

    def test_release_tag_and_dispatch_events_use_exact_version(self):
        events = [
            ("release", {"action": "published", "release": {"tag_name": TAG}}, "refs/tags/" + TAG),
            ("push", {}, "refs/tags/" + TAG),
            ("workflow_dispatch", {"inputs": {"tag": TAG}}, "refs/heads/main"),
        ]
        for name, event, ref in events:
            with self.subTest(event=name):
                result = plan(name, event, ref, "a" * 40, REPO)
                self.assertEqual(result["source_ref"], TAG)
                self.assertEqual(result["version"], "0.1.1")

    def test_pr_never_queries_or_publishes_releases(self):
        def forbidden(*args):
            raise AssertionError("PR queried release state")
        result = plan("pull_request", {}, "refs/pull/2/merge", "a" * 40, REPO, forbidden)
        self.assertEqual(result["publish"], "false")

    def test_untrusted_tag_rejected_before_build_or_publish(self):
        for tag in ["v0.1.1\nother=value", "v0.1.1;echo bad", "main", "0.1.1"]:
            with self.subTest(tag=tag), self.assertRaises(ValueError):
                plan("workflow_dispatch", {"inputs": {"tag": tag}}, "refs/heads/main", "a" * 40, REPO)

    def test_retry_preserves_existing_archives_and_hashes_actual_bytes(self):
        with tempfile.TemporaryDirectory() as directory:
            build_fixture(directory)
            first, second, third = asset_names("0.1.1")[:3]
            cli = FakeGitHub({first: b"previous uploaded archive; different timestamps"})
            cli.fail_on = third
            with self.assertRaises(subprocess.CalledProcessError):
                publish(REPO, TAG, directory, directory, cli)
            self.assertIn(second, cli.assets)
            self.assertNotIn("SHA256SUMS", cli.assets)
            cli.fail_on = None
            publish(REPO, TAG, directory, directory, cli)
            self.assertEqual(set(cli.assets), set(asset_names("0.1.1")))
            self.assertEqual(cli.assets[first], b"previous uploaded archive; different timestamps")
            for line in cli.assets["SHA256SUMS"].decode().splitlines():
                digest, name = line.split("  ", 1)
                self.assertEqual(digest, hashlib.sha256(cli.assets[name]).hexdigest())
            uploads = [call for call in cli.calls if call[1] == "upload"]
            self.assertFalse(any(Path(call[3]).name == first for call in uploads))
            before = len(uploads)
            publish(REPO, TAG, directory, directory, cli)
            self.assertEqual(before, len([call for call in cli.calls if call[1] == "upload"]))

    def test_only_checksum_metadata_is_replaced_when_repairing(self):
        with tempfile.TemporaryDirectory() as directory:
            build_fixture(directory)
            cli = FakeGitHub({"SHA256SUMS": b"incomplete old manifest\n"})
            publish(REPO, TAG, directory, directory, cli)
            clobbered = [Path(c[3]).name for c in cli.calls if "--clobber" in c]
            self.assertEqual(clobbered, ["SHA256SUMS"])

    def test_corrupt_build_is_rejected_before_remote_changes(self):
        with tempfile.TemporaryDirectory() as directory:
            build_fixture(directory)
            (Path(directory) / asset_names("0.1.1")[0]).write_bytes(b"corrupt")
            cli = FakeGitHub()
            with self.assertRaisesRegex(ValueError, "checksum mismatch"):
                publish(REPO, TAG, directory, directory, cli)
            self.assertEqual(cli.calls, [])


if __name__ == "__main__":
    unittest.main()
