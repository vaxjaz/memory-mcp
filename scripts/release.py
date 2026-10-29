#!/usr/bin/env python3
"""Select a tagged source and fill missing GitHub Release assets without replacing binaries."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile

from package import TARGETS, VERSION, VERSION_FILE, normalize_version


def gh(*args):
    return subprocess.check_output(["gh", *map(str, args)], text=True, stderr=subprocess.PIPE)


def release_info(repo, tag, cli=gh):
    try:
        return json.loads(cli("release", "view", tag, "--repo", repo,
                              "--json", "tagName,assets,isDraft"))
    except subprocess.CalledProcessError as error:
        message = (error.stderr or "").lower()
        if "404" in message or "release not found" in message:
            return None
        raise


def asset_names(version):
    names = [f"memory-mcp-{version}-{target_os}-{arch}." +
             ("zip" if target_os == "windows" else "tar.gz")
             for target_os, arch in TARGETS]
    return names + [f"memory-mcp-{version}-source.zip", "SHA256SUMS"]


def tag_commit(repo, tag, cli=gh):
    try:
        obj = json.loads(cli("api", f"repos/{repo}/git/ref/tags/{tag}"))["object"]
    except subprocess.CalledProcessError as error:
        if "404" in (error.stderr or ""):
            return None
        raise
    for _ in range(10):
        if obj["type"] == "commit":
            return obj["sha"]
        if obj["type"] != "tag":
            break
        obj = json.loads(cli("api", f"repos/{repo}/git/tags/{obj['sha']}"))["object"]
    raise ValueError("Release tag does not resolve to a commit")


def ensure_tag(repo, tag, sha, cli=gh):
    existing = tag_commit(repo, tag, cli)
    if existing is None:
        try:
            cli("api", "--method", "POST", f"repos/{repo}/git/refs",
                "-f", f"ref=refs/tags/{tag}", "-f", f"sha={sha}")
            return
        except subprocess.CalledProcessError:
            # Another actor may have created it. Never force-move an existing tag.
            existing = tag_commit(repo, tag, cli)
            if existing is None:
                raise
    if existing != sha:
        raise ValueError(f"Tag {tag} already points to another commit; choose a new version")


def core_version(value):
    return tuple(int(part) for part in normalize_version(value).split("-", 1)[0].split("+", 1)[0].split("."))


def plan(event_name, event, ref, sha, repo, cli=gh, release_version=None):
    tag = ""
    if event_name == "release":
        if event.get("action") == "published" and not event["release"].get("draft"):
            tag = event["release"]["tag_name"]
    elif event_name == "push" and ref.startswith("refs/tags/"):
        tag = ref.removeprefix("refs/tags/")
    elif event_name == "workflow_dispatch":
        tag = event.get("inputs", {}).get("tag", "").strip()
    elif event_name == "push" and ref == "refs/heads/main":
        # Repair the most recent published release on a normal main update.
        # This also backfills releases created before the release-event workflow existed.
        releases = json.loads(cli("release", "list", "--repo", repo, "--exclude-drafts",
                                 "--limit", "1", "--json", "tagName"))
        if release_version:
            version = normalize_version(release_version)
            desired = "v" + version
            latest = releases[0]["tagName"] if releases else None
            try:
                forward = latest is None or core_version(version) >= core_version(latest)
            except ValueError:
                forward = False
            if forward:
                info = release_info(repo, desired, cli)
                if info is None:
                    existing = tag_commit(repo, desired, cli)
                    if existing is not None and existing != sha:
                        raise ValueError(f"Tag {desired} already points to another commit; choose a new version")
                    # Build/test the pinned commit FIRST. The publish step creates
                    # the tag only after all artifacts and checks have succeeded.
                    return {"tag": desired, "version": version, "source_ref": sha,
                            "source_sha": sha, "create_tag": "true", "publish": "true"}
                if set(asset_names(version)) - {a["name"] for a in info["assets"]}:
                    tag = desired
        if releases:
            candidate = releases[0]["tagName"]
            try:
                version = normalize_version(candidate)
            except ValueError:
                version = None
            if version:
                info = release_info(repo, candidate, cli)
                present = {asset["name"] for asset in info["assets"]} if info else set()
                if not tag and set(asset_names(version)) - present:
                    tag = candidate
    if not tag:
        return {"tag": "", "version": VERSION, "source_ref": sha, "publish": "false"}
    version = normalize_version(tag)
    # Require a canonical v-prefixed release tag, not a free-form shell/branch string.
    if tag != "v" + version:
        raise ValueError("Release tags must use vMAJOR.MINOR.PATCH")
    return {"tag": tag, "version": version, "source_ref": tag, "publish": "true"}


def publish(repo, tag, dist, source, cli=gh, create_tag=False, source_sha=None):
    version = normalize_version(tag)
    if tag != "v" + version:
        raise ValueError("Noncanonical release tag")
    dist = Path(dist)
    names = asset_names(version)
    for name in names:
        if not (dist / name).is_file():
            raise ValueError(f"Missing build output: {name}")
    # Never upload a mislabeled or corrupted local build.
    manifest = {}
    for line in (dist / "SHA256SUMS").read_text().splitlines():
        digest, name = line.split("  ", 1)
        manifest[name] = digest
    for name in names[:-1]:
        if hashlib.sha256((dist / name).read_bytes()).hexdigest() != manifest.get(name):
            raise ValueError(f"Build checksum mismatch: {name}")

    if create_tag:
        if not source_sha:
            raise ValueError("source_sha is required to create a release tag")
        ensure_tag(repo, tag, source_sha, cli)

    info = release_info(repo, tag, cli)
    if info is None:
        cli("release", "create", tag, "--repo", repo, "--verify-tag",
            "--title", f"memory-mcp {tag}", "--notes-file", str(Path(source) / "docs/RELEASE_NOTES.md"),
            *[str(dist / name) for name in names])
    else:
        present = {asset["name"] for asset in info["assets"]}
        # Existing archives can have different timestamps. Hash their actual bytes,
        # rather than publishing a checksum for a newly rebuilt but unuploaded file.
        with tempfile.TemporaryDirectory(prefix="memory-mcp-publish-") as temp:
            temp = Path(temp)
            checksums = []
            for name in names[:-1]:
                if name in present:
                    cli("release", "download", tag, "--repo", repo, "--pattern", name, "--dir", str(temp))
                    data = (temp / name).read_bytes()
                else:
                    cli("release", "upload", tag, str(dist / name), "--repo", repo)
                    data = (dist / name).read_bytes()
                checksums.append(f"{hashlib.sha256(data).hexdigest()}  {name}\n")
            checksum_file = temp / "SHA256SUMS"
            checksum_bytes = "".join(checksums).encode()
            unchanged = False
            if "SHA256SUMS" in present:
                cli("release", "download", tag, "--repo", repo, "--pattern", "SHA256SUMS", "--dir", str(temp))
                unchanged = checksum_file.read_bytes() == checksum_bytes
            if not unchanged:
                checksum_file.write_bytes(checksum_bytes)
                args = ["release", "upload", tag, str(checksum_file), "--repo", repo]
                if "SHA256SUMS" in present:
                    args.append("--clobber")  # Only the checksum metadata is replaced.
                cli(*args)
    result = release_info(repo, tag, cli)
    if result is None or set(names) - {a["name"] for a in result["assets"]}:
        raise RuntimeError("Release is missing required assets after upload")
    return f"https://github.com/{repo}/releases/tag/{tag}"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    sub.add_parser("plan")
    upload = sub.add_parser("publish")
    upload.add_argument("--tag", required=True)
    upload.add_argument("--dist", type=Path, required=True)
    upload.add_argument("--source-dir", type=Path, required=True)
    upload.add_argument("--create-tag", action="store_true")
    upload.add_argument("--source-sha")
    args = parser.parse_args()
    repo = os.environ["GITHUB_REPOSITORY"]
    if args.command == "plan":
        event = json.loads(Path(os.environ["GITHUB_EVENT_PATH"]).read_text())
        result = plan(os.environ["GITHUB_EVENT_NAME"], event, os.environ["GITHUB_REF"],
                      os.environ["GITHUB_SHA"], repo,
                      release_version=VERSION if VERSION_FILE.is_file() else None)
        with open(os.environ["GITHUB_OUTPUT"], "a") as output:
            for key, value in result.items():
                output.write(f"{key}={value}\n")
        print(json.dumps(result))
    else:
        print(publish(repo, args.tag, args.dist, args.source_dir,
                      create_tag=args.create_tag, source_sha=args.source_sha))


if __name__ == "__main__":
    main()
