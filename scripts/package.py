#!/usr/bin/env python3
"""Build offline end-user bundles; the builder needs Go and Python, users do not."""
import argparse
import hashlib
import json
import os
import re
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parents[1]
GO = os.environ.get("GO", "go")
VERSION_FILE = ROOT / "cmd/memory-mcp/VERSION"
TARGETS = [("linux", "amd64"), ("linux", "arm64"),
           ("darwin", "amd64"), ("darwin", "arm64"), ("windows", "amd64")]


def bundle_tar_mode(info):
    """A Windows cross-builder must still ship runnable Unix executables."""
    info.mode = 0o755 if info.isdir() or Path(info.name).name == 'memory-mcp' else 0o644
    return info


def packaged_documentation(root):
    docs = {path.relative_to(root): path for path in sorted((root / "docs").rglob("*"))
            if path.is_file()}
    guide = root / "internal/server/guide.md"
    if guide.is_file():
        docs[Path("docs/AGENT_GUIDE.md")] = guide
    return docs


def normalize_version(value):
    value = value.removeprefix("v")
    if not re.fullmatch(r"\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?", value):
        raise ValueError("Expected a version such as 0.1.1 or v0.1.1")
    return value


def verified_source_commit(root, supplied=None, allow_dirty=False):
    try:
        head = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root,
                                       text=True, stderr=subprocess.DEVNULL).strip()
    except (OSError, subprocess.CalledProcessError):
        head = "unknown"
    if supplied:
        if not re.fullmatch(r"[0-9a-f]{40}", supplied):
            raise ValueError("source-commit must be an exact lowercase 40-character commit SHA")
        marker = root / ".source-commit"
        if not marker.is_file() or marker.read_text().strip() != supplied:
            raise ValueError("archived source must contain a matching .source-commit marker")
        if head != "unknown" and head != supplied:
            raise ValueError("source-commit conflicts with checkout HEAD")
        return supplied
    if head != "unknown" and not allow_dirty:
        # The build compiles the WORKING TREE while SOURCE_COMMIT records HEAD;
        # an uncommitted change would ship code its own provenance cannot point
        # to. Commit first, or explicitly accept the broken chain.
        status = subprocess.run(["git", "status", "--porcelain"], cwd=root,
                                capture_output=True, text=True)
        if status.returncode == 0 and status.stdout.strip():
            raise ValueError("working tree has uncommitted changes; commit them so SOURCE_COMMIT matches the built code (or pass --allow-dirty to accept a stale provenance stamp)")
    return head


# Keep old tags buildable when they predate the version file.
VERSION = normalize_version(VERSION_FILE.read_text().strip() if VERSION_FILE.is_file() else "0.1.0")


def notices(root=ROOT):
    goroot = Path(subprocess.check_output([GO, "env", "GOROOT"], text=True).strip())
    go_version = subprocess.check_output([GO, "version"], text=True).strip()
    output = subprocess.check_output(
        [GO, "list", "-buildvcs=false", "-deps", "-f", "{{if .Module}}{{.Module.Path}}|{{.Module.Version}}|{{.Module.Dir}}{{end}}", "./cmd/memory-mcp"],
        cwd=root, text=True)
    sections = [f"\n{'=' * 72}\nGo runtime and standard library ({go_version})\n",
                (goroot / "LICENSE").read_text() + "\n"]
    for line in sorted(set(output.splitlines())):
        if not line or line.startswith("github.com/vaxjaz/memory-mcp||"):
            continue
        name, version, directory = line.split("|", 2)
        if not directory:
            raise RuntimeError(f"Missing module directory: {name}")
        files = [p for p in Path(directory).iterdir()
                 if p.is_file() and p.name.upper().startswith(("LICENSE", "LICENCE", "COPYING", "NOTICE"))]
        if not files:
            raise RuntimeError(f"Review missing license notices for {name}")
        sections.append(f"\n{'=' * 72}\n{name} {version}\n")
        for p in sorted(files):
            sections.append(f"\n--- {p.name} ---\n{p.read_text(errors='replace')}\n")
    return "Third-party notices for bundled dependencies.\n" + "".join(sections)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", default=VERSION, type=normalize_version)
    parser.add_argument("--source-dir", type=Path, default=ROOT,
                        help="Source checkout to build, independently of this packaging helper")
    parser.add_argument("--output-dir", type=Path,
                        help="Output directory (default: source-dir/dist)")
    parser.add_argument("--source-commit", help="Verified archived source SHA; requires matching .source-commit")
    parser.add_argument("--allow-dirty", action="store_true",
                        help="build despite uncommitted changes; SOURCE_COMMIT then points at HEAD, not the built tree")
    args = parser.parse_args()
    version = args.version
    root = args.source_dir.resolve()
    if not (root / "go.mod").is_file():
        parser.error("source-dir must contain go.mod")
    try:
        source_commit = verified_source_commit(root, args.source_commit, args.allow_dirty)
    except ValueError as error:
        parser.error(str(error))
    dist = (args.output_dir or root / "dist").resolve()
    dist.mkdir(parents=True, exist_ok=True)
    licenses = notices(root)
    docs = packaged_documentation(root)
    artifacts = []
    for target_os, arch in TARGETS:
        name = f"memory-mcp-{version}-{target_os}-{arch}"
        with tempfile.TemporaryDirectory(prefix="memory-mcp-package-") as tmp:
            bundle = Path(tmp) / name
            bundle.mkdir()
            exe = "memory-mcp.exe" if target_os == "windows" else "memory-mcp"
            env = dict(os.environ, GOOS=target_os, GOARCH=arch, CGO_ENABLED="0")
            subprocess.run([GO, "build", "-buildvcs=false", "-trimpath", "-ldflags", f"-s -w -X main.version={version} -X main.buildCommit={source_commit}",
                            "-o", str(bundle / exe), "./cmd/memory-mcp"], cwd=root, env=env, check=True)
            shutil.copy2(root / "README.md", bundle / "README.md")
            for relative, file in docs.items():
                destination = bundle / relative
                destination.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(file, destination)
            (bundle / "SOURCE_COMMIT").write_text(source_commit + "\n")
            (bundle / "THIRD_PARTY_NOTICES.txt").write_text(licenses)
            (bundle / "mcp.example.json").write_text(json.dumps({"mcpServers": {"decision-memory": {
                "command": "REPLACE_WITH_ABSOLUTE_PATH_TO_" + exe, "args": ["serve"]}}}, indent=2) + "\n")
            if target_os == "windows":
                archive = dist / (name + ".zip")
                with zipfile.ZipFile(archive, "w", zipfile.ZIP_DEFLATED) as z:
                    for file in sorted(bundle.rglob("*")):
                        if file.is_file():
                            z.write(file, file.relative_to(bundle.parent))
            else:
                archive = dist / (name + ".tar.gz")
                with tarfile.open(archive, "w:gz") as t:
                    t.add(bundle, arcname=name, filter=bundle_tar_mode)
            artifacts.append(archive)
            print(f"Built {archive.name}", flush=True)
    source = dist / f"memory-mcp-{version}-source.zip"
    with zipfile.ZipFile(source, "w", zipfile.ZIP_DEFLATED) as z:
        z.writestr(f"memory-mcp-{version}/.source-commit", source_commit + "\n")
        for file in sorted(root.rglob("*")):
            relative = file.relative_to(root)
            if file.is_file() and relative.parts[0] in {"go.mod", "go.sum", "README.md", ".gitignore", ".github", "cmd", "internal", "docs", "scripts"} and "__pycache__" not in relative.parts:
                z.write(file, Path(f"memory-mcp-{version}") / relative)
    artifacts.append(source)
    checksums = "".join(
        f"{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.name}\n" for p in artifacts)
    upgrade_guide = root / "docs/AGENT_UPGRADE_PACKAGE.md"
    if upgrade_guide.is_file():
        upgrade = dist / f"memory-mcp-{version}-agent-upgrade.zip"
        with zipfile.ZipFile(upgrade, "w", zipfile.ZIP_DEFLATED) as z:
            z.write(upgrade_guide, "UPGRADE_AGENT.md")
            for relative, file in docs.items():
                z.write(file, relative)
            z.writestr("SOURCE_COMMIT", source_commit + "\n")
            z.writestr("SHA256SUMS", checksums)
            for artifact in artifacts:
                z.write(artifact, "packages/" + artifact.name)
        checksums += f"{hashlib.sha256(upgrade.read_bytes()).hexdigest()}  {upgrade.name}\n"
        print(f"Built {upgrade.name}", flush=True)
    (dist / "SHA256SUMS").write_text(checksums)


if __name__ == "__main__":
    main()
