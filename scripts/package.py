#!/usr/bin/env python3
"""Build offline end-user bundles; the builder needs Go and Python, users do not."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parents[1]
GO = os.environ.get("GO", "go")
VERSION = "0.1.0"
TARGETS = [("linux", "amd64"), ("linux", "arm64"),
           ("darwin", "amd64"), ("darwin", "arm64"), ("windows", "amd64")]


def notices():
    goroot = Path(subprocess.check_output([GO, "env", "GOROOT"], text=True).strip())
    go_version = subprocess.check_output([GO, "version"], text=True).strip()
    output = subprocess.check_output(
        [GO, "list", "-buildvcs=false", "-deps", "-f", "{{if .Module}}{{.Module.Path}}|{{.Module.Version}}|{{.Module.Dir}}{{end}}", "./cmd/memory-mcp"],
        cwd=ROOT, text=True)
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
    dist = ROOT / "dist"
    dist.mkdir(exist_ok=True)
    licenses = notices()
    artifacts = []
    for target_os, arch in TARGETS:
        name = f"memory-mcp-{VERSION}-{target_os}-{arch}"
        with tempfile.TemporaryDirectory(prefix="memory-mcp-package-") as tmp:
            bundle = Path(tmp) / name
            bundle.mkdir()
            exe = "memory-mcp.exe" if target_os == "windows" else "memory-mcp"
            env = dict(os.environ, GOOS=target_os, GOARCH=arch, CGO_ENABLED="0")
            subprocess.run([GO, "build", "-buildvcs=false", "-trimpath", "-ldflags", f"-s -w -X main.version={VERSION}",
                            "-o", str(bundle / exe), "./cmd/memory-mcp"], cwd=ROOT, env=env, check=True)
            shutil.copy2(ROOT / "README.md", bundle / "README.md")
            shutil.copytree(ROOT / "docs", bundle / "docs")
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
                    t.add(bundle, arcname=name)
            artifacts.append(archive)
            print(f"Built {archive.name}", flush=True)
    source = dist / f"memory-mcp-{VERSION}-source.zip"
    with zipfile.ZipFile(source, "w", zipfile.ZIP_DEFLATED) as z:
        for file in sorted(ROOT.rglob("*")):
            relative = file.relative_to(ROOT)
            if file.is_file() and relative.parts[0] in {"go.mod", "go.sum", "README.md", ".gitignore", ".github", "cmd", "internal", "docs", "scripts"} and "__pycache__" not in relative.parts:
                z.write(file, Path(f"memory-mcp-{VERSION}") / relative)
    artifacts.append(source)
    (dist / "SHA256SUMS").write_text("".join(
        f"{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.name}\n" for p in artifacts))


if __name__ == "__main__":
    main()
