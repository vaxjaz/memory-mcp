#!/usr/bin/env python3
"""Replay conditional Pre/Post tool probes against a disposable ZCode binding."""
from contextlib import closing
import json
from pathlib import Path
import re
import sqlite3
import subprocess
import sys
import tempfile
import time

from smoke import Host


def main():
    binary = str(Path(sys.argv[1]).resolve())
    scope = "project:context-probe-fixture"
    login = "mcp__review__login"
    query = "mcp__review__warehouse_query"
    with tempfile.TemporaryDirectory(prefix="memory-context-probes-") as temp:
        root = Path(temp)
        project, data, config = root / "project", root / "data", root / "config.json"
        project.mkdir()
        config.write_text("{}", encoding="utf-8")
        options = ["--host", "zcode", "--config", str(config), "--data-dir", str(data),
                   "--project", str(project), "--scope", scope]

        def cli(command, *extra):
            out = subprocess.check_output([binary, command, *options, *extra], encoding="utf-8")
            return json.loads(out)

        cli("setup")
        probes = {"context_facts": {scope: [
            {"name": "authenticated", "mcp_tool": "login",
             "key": ["input.service", "input.connection_id"],
             "when": {"all": [
                 {"field": "result.ok", "op": "eq", "value": True},
                 {"field": "result.state", "op": "contains", "value": "ready", "case_sensitive": False},
             ]}, "ttl_seconds": 60},
        ]}, "projects": {scope: [
            {"mcp_tool": "warehouse_query", "mode": "replace", "branches": [
                {"id": "change", "text_fields": ["sql", "note"], "when": {"all": [
                    {"fact": "authenticated", "key": ["input.service", "input.connection_id"]},
                    {"any": [{"field": "input.sql", "op": "token", "value": "UPDATE"},
                             {"field": "input.sql", "op": "token", "value": "DELETE"}]},
                    {"not": {"field": "input.dry_run", "op": "eq", "value": True}},
                ]}},
                {"id": "read", "text_fields": ["sql"],
                 "when": {"field": "input.sql", "op": "token", "value": "SELECT"}},
            ]},
        ]}}
        (data / "tool-probes.json").write_text(json.dumps(probes), encoding="utf-8")
        stale = cli("doctor")
        assert stale["probe_config_valid"] and stale["probe_matcher_mismatches"] >= 2, stale
        cli("setup")
        ready = cli("doctor")
        assert ready["probe_matcher_mismatches"] == 0 and ready["probe_context_fact_rules"] == 1, ready
        events = json.loads(config.read_text(encoding="utf-8"))["hooks"]["events"]
        assert re.search(events["PreToolUse"][0]["matcher"], login)
        assert re.search(events["PostToolUse"][0]["matcher"], login)
        assert not re.search(events["PostToolUse"][0]["matcher"], query)
        handler = events["PreToolUse"][0]["hooks"][0]

        def hook(event, session="main", turn="turn-1", **fields):
            payload = dict(hook_event_name=event, session_id=session, turn_id=turn, event_id=turn, cwd=str(project), **fields)
            result = subprocess.run([handler["command"], *handler["args"]],
                                    input=json.dumps(payload), encoding="utf-8", capture_output=True, check=True)
            return json.loads(result.stdout) if result.stdout.strip() else {}

        host = Host(binary, str(data))
        try:
            body = "This synthetic fixture distinguishes query contexts."
            host.tool("memory_experience_record", dict(scope=scope, project=str(project), title="Synthetic context probe",
                      experience=dict(pitfall="Infer context without a matching tool result", counterexample=body,
                                      impact="An unrelated query can trigger a reminder", applicability="Fixture only",
                                      boundary="No database was queried",
                                      evidence=[dict(kind="runtime", reference="context-fixture", observation=body)],
                                      cues=[dict(cue_id="query", label="Context", text=body, evidence_refs=["context-fixture"],
                                                 matches=[dict(op="token", entrances=["SettlementTarget", "ORDER UPDATE", "information_schema"])])])))
            assert "contextual_tool_probes" in host.tool("memory_bootstrap", {})["features"]
            identity = {"service": "fixture", "connection_id": "one"}

            def start(call, **event):
                return hook("PreToolUse", tool_name=login, tool_use_id=call, tool_input=identity, **event)

            def complete(call, success, **event):
                return hook("PostToolUse", tool_name=login, tool_use_id=call, tool_input=identity,
                            tool_response={"structuredContent": {"ok": success, "state": "LOGIN_READY",
                                                                 "secret": "DO_NOT_STORE_RESPONSE"}}, **event)

            def run(sql="UPDATE SettlementTarget", connection="one", service="fixture", note="", **event):
                dry_run = event.pop("dry_run", False)
                args = {"service": service, "connection_id": connection, "sql": sql, "note": note}
                if dry_run is not None:
                    args["dry_run"] = dry_run
                return hook("PreToolUse", tool_name=query, tool_input=args, **event)

            def challenge(output):
                return output.get("decision") == "block" or output.get("hookSpecificOutput", {}).get("permissionDecision") == "deny"

            def outcome(branch, session="main"):
                report = cli("doctor", "--session", session)
                for row in report["recent_events"]:
                    if row["result"] == "custom_probe_condition":
                        detail = json.loads(row["detail"])
                        if detail["branch"] == branch:
                            return detail["outcome"]
                raise AssertionError(report["recent_events"])

            hook("UserPromptSubmit")
            complete("missing-pre", True)
            assert run() == {} and outcome("change") == "unknown"
            assert challenge(run("SELECT information_schema"))
            start("login-1")
            complete("login-1", True)
            assert challenge(run())
            assert run(connection="two") == {} and outcome("change") == "unknown"
            assert run(service="other") == {} and outcome("change") == "unknown"
            assert run(session="other") == {}
            assert run(dry_run=True) == {} and outcome("change") == "not_met"
            assert run(dry_run=None) == {} and outcome("change") == "unknown"
            assert run("UPDATE example ORDER", note="UPDATE") == {} and outcome("change") == "passed"

            start("older")
            start("newer")
            complete("newer", False)
            complete("older", True)
            assert run() == {} and outcome("change") == "not_met"
            complete("newer", True)
            assert run() == {}

            start("older-than-expired")
            start("current-but-expired")
            complete("current-but-expired", False)
            with closing(sqlite3.connect(data / "memory.db")) as db:
                db.execute("UPDATE probe_context_facts SET expires=?", (int(time.time() * 1000) - 1,))
                db.commit()
            start("purge-expired")
            complete("older-than-expired", True)
            assert run() == {} and outcome("change") == "unknown"

            start("previous-input")
            hook("UserPromptSubmit", turn="turn-2")
            complete("previous-input", True, turn="turn-1")
            assert run(turn="turn-2") == {} and outcome("change") == "unknown"
            start("login-2", turn="turn-2")
            complete("login-2", True, turn="turn-2")
            assert challenge(run(turn="turn-2"))
            with closing(sqlite3.connect(data / "memory.db")) as db:
                rows = db.execute("SELECT * FROM probe_context_facts").fetchall()
                assert rows and "DO_NOT_STORE_RESPONSE" not in str(rows) and "fixture" not in str(rows), rows
                db.execute("UPDATE probe_context_facts SET expires=0")
                db.commit()
            assert run(turn="turn-2") == {} and outcome("change") == "unknown"
        finally:
            host.close()
    print("PASS: configured Pre/Post dispatch, branches, scoped facts, field boundaries, late results, and expiry")


if __name__ == "__main__":
    main()
