#!/usr/bin/env python3
"""Actual stdio executable + disposable native-event replays. No model claims."""
import argparse
from contextlib import closing
import json
from pathlib import Path
import re
import sqlite3
import statistics
import subprocess
import tempfile
import time

from smoke import Host


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary")
    parser.add_argument("--if-supported", action="store_true")
    opt = parser.parse_args()
    binary = str(Path(opt.binary).resolve())
    if "--experience-trial" not in subprocess.check_output([binary, "help"], text=True, encoding="utf-8"):
        if opt.if_supported:
            print("SKIP: historical executable predates experience trial")
            return
        raise AssertionError("experience trial not supported")
    timings = []
    for adapter in ("zcode", "codex", "qoder", "qoder-ide"):
        with tempfile.TemporaryDirectory(prefix="experience-isolated-") as temp:
            root = Path(temp)
            project = root / "business"
            project.mkdir()
            user = root / "user"
            user.mkdir()
            data = root / "disposable-library"
            config = user / ("config.toml" if adapter == "codex" else "config.json")
            config.write_text("" if adapter == "codex" else "{}", encoding="utf-8")
            rules = b"Existing routing remains owned by the project.\n"
            (project / "AGENTS.md").write_bytes(rules)
            (project / "Policy.java").write_text("class Policy {\n boolean canDispatchLynOrder(){ return true; }\n}\n", encoding="utf-8")
            scope = "project:experience-fixture"
            options = ["--host", adapter, "--config", str(config), "--data-dir", str(data),
                       "--project", str(project), "--scope", scope]

            def cli(*args):
                p = subprocess.run([binary, *args], capture_output=True, text=True, encoding="utf-8", timeout=30)
                assert p.returncode == 0, (args, p.stdout, p.stderr)
                return json.loads(p.stdout)

            setup = cli("setup", *options, "--experience-trial")
            assert setup["experience_trial"] and setup["experience_hooks_installed"] and not setup["raw_capture"]
            doctor = cli("doctor", *options)
            assert doctor["mode_configuration_consistent"] and doctor["experience_trial"], doctor
            assert doctor["experience_hooks_installed"] and doctor["experience_index"]["cold_start"], doctor
            hooks_file = config if adapter != "codex" else user / "hooks.json"
            cfg = json.loads(hooks_file.read_text(encoding="utf-8"))
            events = cfg["hooks"]["events"] if adapter == "zcode" else cfg["hooks"]
            assert not events.get("Stop"), events
            handler = events["PreToolUse"][0]["hooks"][0]
            matcher = events["PreToolUse"][0]["matcher"]
            assert re.search(matcher, "Grep") and re.search(matcher, "run_in_terminal")
            assert not re.search(matcher, "mcp__decision_memory__memory_experience_lookup")
            assert not re.search(matcher, "apply_patch")

            def hook(event, turn="turn-1", **fields):
                payload = dict(hook_event_name=event, cwd=str(project), session_id="real-executable-fixture",
                               turn_id=turn, event_id=turn, **fields)
                command = [handler["command"], *handler["args"]] if adapter == "zcode" else handler["command"]
                start = time.perf_counter()
                p = subprocess.run(command, shell=adapter != "zcode", input=json.dumps(payload, ensure_ascii=False),
                                   capture_output=True, text=True, encoding="utf-8", cwd=project, timeout=15)
                elapsed = (time.perf_counter() - start) * 1000
                assert p.returncode in (0, 2), (p.stdout, p.stderr)
                out = json.loads(p.stdout) if p.stdout.strip() else {}
                return p.returncode, out, p.stderr, elapsed

            host = Host(binary, str(data))
            try:
                boot = host.tool("memory_bootstrap", {})
                assert "compact_experiences" in boot["features"]
                draft = dict(scope=scope, project=str(project), title="Synthetic multi-entry risk",
                             request_id="experience-once",
                             experience=dict(anchors=["LYN", "canDispatchLynOrder"], signals=["创建准入"],
                                             pitfall="A local caller is assumed to cover every entry",
                                             counterexample="An explicitly synthetic isolated counterexample, not a real business rule",
                                             impact="One entry may miss a required admission check",
                                             applicability="Only when the same requirement must cover multiple entries",
                                             boundary="Other requirements and runtime conditions need fresh evidence",
                                             evidence=[dict(kind="code", reference="isolated fixture",
                                                            observation="Literal candidate method only",
                                                            path="Policy.java", symbol="canDispatchLynOrder")]))
                saved = host.tool("memory_experience_record", draft)
                assert cli("doctor", *options)["experience_index"]["eligible_cases"] == 1
                assert host.tool("memory_experience_record", draft)["id"] == saved["id"]
                assert hook("UserPromptSubmit", prompt="PRIVATE_PROMPT_DO_NOT_COPY")[1] == {}
                for _ in range(5):
                    code, out, _, elapsed = hook("PreToolUse", tool_name="Grep", tool_input={"pattern": "unrelated search"})
                    assert code == 0 and out == {}
                    timings.append((adapter, elapsed))
                assert hook("PreToolUse", tool_name="Read", tool_input={"path": "AGENTS.md"})[1] == {}
                payload = dict(tool_name="Grep", tool_input={"pattern": "LYN 创建准入"})
                code, out, stderr, _ = hook("PreToolUse", **payload)
                reason = stderr if code == 2 else out.get("reason", "")
                assert code == 2 or out.get("hookSpecificOutput", {}).get("permissionDecision") == "deny", (adapter, out, stderr)
                assert "A local caller" not in reason and "One entry may miss" not in reason
                token = re.search(r'challenge_id="([a-f0-9]{32})"', reason).group(1)
                again = hook("PreToolUse", **payload)
                assert again[0] == 2 or again[1].get("decision") == "block"
                host.tool("memory_experience_lookup", dict(scope=scope, task="", questions="coverage",
                          keywords=["LYN"], challenge_id=token), expect_error=True)
                lookup_args = dict(scope=scope,
                                     task="Current fixture multi-entry admission change",
                                     questions="Does the candidate method cover the current real entries?",
                                     keywords=["LYN"], challenge_id=token)
                wire = host.request("tools/call", {"name": "memory_experience_lookup", "arguments": lookup_args})
                assert not wire.get("isError") and "structuredContent" not in wire, wire
                assert len(wire["content"]) == 1, wire
                assert len(wire["content"][0]["text"].encode()) <= 8192
                response = json.loads(wire["content"][0]["text"])
                assert response["items"][0]["id"] == saved["id"]
                assert response["items"][0]["locations"][0]["lines"] == [2]
                assert response["challenge_outcome"] == "query_completed_not_applicability_verified"
                assert hook("PreToolUse", **payload)[1] == {}
                assert hook("Stop", last_assistant_message="Do not start an extra model round")[1] == {}
                with closing(sqlite3.connect(data / "memory.db")) as db:
                    assert db.execute("select count(*) from entries").fetchone()[0] == 1
                    assert "PRIVATE_PROMPT_DO_NOT_COPY" not in json.dumps(list(db.execute("select * from entries")))
                assert (project / "AGENTS.md").read_bytes() == rules
                # New native input cannot reuse the earlier query as a permit.
                hook("UserPromptSubmit", turn="turn-2", prompt="Second isolated request")
                gated = hook("PreToolUse", turn="turn-2", **payload)
                assert gated[0] == 2 or gated[1].get("decision") == "block"
                disabled = cli("setup", *options, "--disable-experience-trial")
                assert not disabled["experience_trial"] and not disabled["experience_hooks_installed"]
                assert host.tool("memory_read", dict(id=saved["id"]))["entry"]["current"]
            finally:
                host.close()
    print(json.dumps({"result": "PASS", "coverage": "four adapter configurations and actual subprocess protocols; no native/model-quality claim",
                      "no_match_hook_ms_by_adapter": {
                          name: {"median": round(statistics.median([t for a, t in timings if a == name]), 1),
                                 "max": round(max(t for a, t in timings if a == name), 1)}
                          for name in {a for a, _ in timings}}}, ensure_ascii=False))


if __name__ == "__main__":
    main()
