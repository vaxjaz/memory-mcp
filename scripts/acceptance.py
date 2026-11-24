#!/usr/bin/env python3
"""Run every release gate with one command and print one table.

    python scripts/acceptance.py                       build a temporary binary, then run every gate
    python scripts/acceptance.py --binary FILE         test an existing executable instead of building
    python scripts/acceptance.py --package             also build the offline bundles and verify their checksums
    python scripts/acceptance.py --codex FILE          also run the real Codex transport scenarios
    python scripts/acceptance.py --upgrade-source DIR --old-binary FILE
                                                        also rehearse an upgrade and a rollback on a copy of DIR

A gate that cannot run is reported as SKIP together with the reason. Any FAIL makes the exit status 1,
so packaging and releases can depend on this one command.
"""
import argparse
import hashlib
import json
import os
import shutil
import subprocess
import sys
import tempfile
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SCRIPTS = ROOT / "scripts"


def source_version():
    return (ROOT / "cmd" / "memory-mcp" / "VERSION").read_text(encoding="utf-8").strip().lstrip("v")


def run(command, cwd, env, timeout):
    started = time.monotonic()
    try:
        done = subprocess.run(command, cwd=str(cwd), env=env, capture_output=True, text=True, encoding="utf-8", errors="replace", timeout=timeout)
        output = (done.stdout or "") + (("\n" + done.stderr) if done.stderr else "")
        return done.returncode == 0, time.monotonic() - started, output
    except subprocess.TimeoutExpired as error:
        return False, time.monotonic() - started, "timed out after %ss" % timeout
    except OSError as error:
        return False, time.monotonic() - started, "%s: %s" % (type(error).__name__, error)


def build(go, env, work):
    """Build the way package.py does, so the tested binary is the shipped one."""
    try:
        commit = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=str(ROOT), text=True, stderr=subprocess.DEVNULL).strip()
    except (OSError, subprocess.CalledProcessError):
        commit = "unknown"
    exe = work / ("memory-mcp.exe" if os.name == "nt" else "memory-mcp")
    version = source_version()
    ok, seconds, output = run([go, "build", "-buildvcs=false", "-trimpath", "-ldflags",
                               "-X main.version=%s -X main.buildCommit=%s" % (version, commit), "-o", str(exe), "./cmd/memory-mcp"], ROOT, env, 600)
    return (exe if ok else None), seconds, output


def verify_bundles(dist):
    """Recompute every checksum and require LF, so `sha256sum -c` works on every OS."""
    sums = dist / "SHA256SUMS"
    if not sums.is_file():
        return "SHA256SUMS is missing"
    raw = sums.read_bytes()
    if b"\r" in raw:
        return "SHA256SUMS contains CR bytes"
    problems = []
    for line in raw.decode("utf-8").splitlines():
        expected, _, name = line.partition("  ")
        artifact = dist / name.strip()
        if not artifact.is_file():
            problems.append(name.strip() + " is missing")
        elif hashlib.sha256(artifact.read_bytes()).hexdigest() != expected:
            problems.append(name.strip() + " does not match its checksum")
    return "; ".join(problems) or ""


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--binary", type=Path, help="test this executable instead of building one")
    parser.add_argument("--go", help="Go toolchain (default: the GO environment variable, then `go`)")
    parser.add_argument("--work-dir", type=Path, help="put temporary files here instead of the system temp directory")
    parser.add_argument("--package", action="store_true", help="also build the offline bundles and verify their checksums")
    parser.add_argument("--codex", type=Path, help="also run the real Codex transport scenarios with this executable")
    parser.add_argument("--upgrade-source", type=Path, help="also rehearse an upgrade on a copy of this pre-upgrade library directory")
    parser.add_argument("--old-binary", type=Path, help="the executable to verify the rollback with")
    parser.add_argument("--only", action="append", help="run only the gates whose name contains this text (repeatable)")
    parser.add_argument("--timeout", type=int, default=900, help="per-gate timeout in seconds")
    parser.add_argument("--report", type=Path, help="also write the results as JSON")
    args = parser.parse_args()

    go = args.go or os.environ.get("GO", "go")
    if shutil.which(go) is None:
        print("Go toolchain not found: %r. Put it on PATH or pass --go." % go, file=sys.stderr)
        return 2
    env = dict(os.environ, GO=go, PYTHONIOENCODING="utf-8", PYTHONUTF8="1")
    work = Path(tempfile.mkdtemp(prefix="memory-acceptance-", dir=args.work_dir))
    rows = []
    try:
        binary = args.binary.resolve() if args.binary else None
        if binary is None:
            built, seconds, output = build(go, env, work)
            rows.append(("build", "PASS" if built else "FAIL", seconds, output.strip().splitlines()[-1] if output.strip() else ""))
            binary = built or Path("__build_failed__")
        version = source_version()
        python = sys.executable
        scenario_work = ["--work-dir", str(work)] if args.work_dir else []

        def gate(name, command, cwd=ROOT):
            if args.only and not any(text in name for text in args.only):
                rows.append((name, "SKIP", 0.0, "not selected"))
                return
            ok, seconds, output = run(command, cwd, env, args.timeout)
            lines = [line for line in output.strip().splitlines() if line.strip()]
            # A JSON report ends with a bare brace; prefer the line that states the verdict.
            verdict = [line for line in lines if '"result"' in line or '"native_transport_pass"' in line]
            text = verdict[-1] if verdict else (lines[-1] if lines else "")
            state = "PASS" if ok else "FAIL"
            if ok and text.startswith("SKIP"):
                # A gate that excused itself did not verify anything, whatever its exit status.
                state, text = "SKIP", "the gate skipped itself: " + text
            rows.append((name, state, seconds, text))

        def skip(name, reason):
            rows.append((name, "SKIP", 0.0, reason))

        gate("go vet", [go, "vet", "./..."])
        gate("go test", [go, "test", "./..."])
        gate("packaging and release unit tests", [python, "-m", "unittest", "test_package", "test_release"], SCRIPTS)
        # The matrix is the list of promises; its overview must equal its rows and every row must carry evidence.
        gate("capability matrix", [python, str(SCRIPTS / "check_capability_matrix.py")])
        if str(binary) == "__build_failed__":
            skip("binary gates", "the build failed")
        else:
            gate("smoke", [python, str(SCRIPTS / "smoke.py"), str(binary), "--expected-version", version])
            # A release candidate must support wakeup; this gate is never excused.
            gate("experience trial", [python, str(SCRIPTS / "check_experience_trial.py"), str(binary)])
            gate("probe usage", [python, str(SCRIPTS / "check_probe_usage.py"), str(binary)])
            gate("host recall", [python, str(SCRIPTS / "check_host_recall.py"), str(binary)])
            gate("context probes", [python, str(SCRIPTS / "check_context_probes.py"), str(binary)])
            gate("user scenarios", [python, str(SCRIPTS / "check_scenarios.py"), str(binary), *scenario_work])
            if (SCRIPTS / "check_tool_matrix.py").is_file():
                gate("tool matrix", [python, str(SCRIPTS / "check_tool_matrix.py"), str(binary), *scenario_work])
        if args.upgrade_source and args.old_binary:
            gate("upgrade and rollback", [python, str(SCRIPTS / "check_upgrade_snapshot.py"), str(binary), str(args.upgrade_source),
                                          "--old-binary", str(args.old_binary), "--out", str(work / "upgrade.json")])
        else:
            skip("upgrade and rollback", "needs --upgrade-source and --old-binary")
        if args.codex:
            for scenario, envelope in (("success", "json-text"), ("iserror", "json-text"), ("lifecycle", "json-text"), ("business", "json-text"), ("business", "structured")):
                gate("native Codex: %s%s" % (scenario, "" if envelope == "json-text" else " " + envelope),
                     [python, str(SCRIPTS / "check_native_delivery.py"), "--scenario", scenario, "--business-envelope", envelope,
                      "--codex", str(args.codex), "--binary", str(binary), "--out", str(work / ("native-%s-%s.json" % (scenario, envelope)))])
        else:
            skip("native Codex transport", "needs --codex")
        if args.package:
            dist = work / "dist"
            gate("offline bundles", [python, str(SCRIPTS / "package.py"), "--output-dir", str(dist)])
            if dist.is_dir():
                problem = verify_bundles(dist)
                rows.append(("bundle checksums", "FAIL" if problem else "PASS", 0.0, problem or "every checksum matches; LF line endings"))
        else:
            skip("offline bundles", "needs --package")
    finally:
        shutil.rmtree(work, ignore_errors=True)

    print()
    print("%-44s %-5s %8s  %s" % ("gate", "state", "seconds", "result"))
    for name, state, seconds, note in rows:
        print("%-44s %-5s %8.1f  %s" % (name, state, seconds, " ".join(str(note).split())[:150]))
    failed = [name for name, state, _, _ in rows if state == "FAIL"]
    counts = {state: sum(1 for row in rows if row[1] == state) for state in ("PASS", "FAIL", "SKIP")}
    print("\n%d passed, %d failed, %d skipped" % (counts["PASS"], counts["FAIL"], counts["SKIP"]))
    if args.report:
        args.report.write_text(json.dumps([dict(gate=n, state=s, seconds=round(t, 1), result=str(r)) for n, s, t, r in rows],
                                          ensure_ascii=False, indent=1) + "\n", encoding="utf-8")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
