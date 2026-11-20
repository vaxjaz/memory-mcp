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
        probes = {"projects": {scope: [
            {"mcp_tool": "login", "observe": [
                {"name": "authenticated", "key": ["input.service", "input.connection_id"],
                 "fields": {"ok": "ok", "state": "state"}, "ttl_seconds": 60},
            ]},
            {"mcp_tool": "warehouse_query", "fields": {
                "text": "sql", "note": "note", "service": "service",
                "connection": "connection_id", "dry_run": "dry_run",
            }},
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
            change = {"when": {"all": [
                {"fact": {"name": "authenticated", "key": ["service", "connection"], "match": {"all": [
                    {"field": "ok", "op": "eq", "value": True},
                    {"field": "state", "op": "contains", "value": "ready"},
                ]}}},
                {"any": [{"field": "text", "op": "token", "value": "UPDATE"},
                         {"field": "text", "op": "token", "value": "DELETE"}]},
                {"not": {"field": "dry_run", "op": "eq", "value": True}},
            ]}, "field": "text", "op": "token", "entrances": ["SettlementTarget", "ORDER UPDATE"]}
            read = {"all": [
                {"field": "text", "op": "token", "value": "SELECT"},
                {"field": "text", "op": "token", "entrances": ["information_schema"]},
            ]}
            saved = host.tool("memory_experience_record", dict(scope=scope, project=str(project), title="Synthetic context probe",
                      admission=dict(future_decision="Check the earlier outcome for the same connection.",
                                     evidence_ref="context-fixture"),
                      experience=dict(pitfall="Infer context without a matching tool result", counterexample=body,
                                      impact="An unrelated query can trigger a reminder", applicability="Fixture only",
                                      boundary="No database was queried",
                                      evidence=[dict(kind="runtime", reference="context-fixture", observation=body)],
                                      cues=[dict(cue_id="change", label="Context", evidence_refs=["context-fixture"],
                                                 matches=[change]),
                                            dict(cue_id="read", label="Read",
                                                 evidence_refs=["context-fixture"], matches=[read])])))
            assert "tool_result_facts" in host.tool("memory_bootstrap", {})["features"]
            identity = {"service": "fixture", "connection_id": "one"}

            def start(call, **event):
                return hook("PreToolUse", tool_name=login, tool_use_id=call, tool_input=identity, **event)

            def complete(call, success, **event):
                return hook("PostToolUse", tool_name=login, tool_use_id=call, tool_input=identity,
                            tool_response={"structuredContent": {"ok": success, "state": "LOGIN_READY",
                                                                 "secret": "DO_NOT_STORE_RESPONSE"}}, **event)

            def query_input(sql, connection, service, note, dry_run):
                args = {"service": service, "connection_id": connection, "sql": sql, "note": note}
                if dry_run is not None:
                    args["dry_run"] = dry_run
                return args

            def run(sql="UPDATE SettlementTarget", connection="one", service="fixture", note="", **event):
                args = query_input(sql, connection, service, note, event.pop("dry_run", False))
                return hook("PreToolUse", tool_name=query, tool_input=args, **event)

            def challenge(output):
                return output.get("decision") == "block" or output.get("hookSpecificOutput", {}).get("permissionDecision") == "deny"

            def outcome(sql="UPDATE SettlementTarget", connection="one", service="fixture", note="",
                        dry_run=False, session="main", turn="turn-1"):
                payload = dict(tool_name=query, session_id=session, turn_id=turn, cwd=str(project),
                               tool_input=query_input(sql, connection, service, note, dry_run))
                result = subprocess.run([binary, "doctor", *options, "--probe=-", "--expect", saved["id"],
                                         "--expect-cue", "change"], input=json.dumps(payload), encoding="utf-8",
                                        capture_output=True, check=True)
                report = json.loads(result.stdout)
                return report["cue_checks"][0]["outcome"]

            hook("UserPromptSubmit")
            complete("missing-pre", True)
            assert run() == {} and outcome() == "unknown"
            assert challenge(run("SELECT information_schema"))
            start("login-1")
            complete("login-1", True)
            # Single request per challenge: the pending SELECT challenge already
            # covers this entry's body, so the covered hit proceeds; doctor
            # confirms the fact-gated cue itself matches with the login fact.
            assert run() == {} and outcome() == "matched"
            assert run(connection="two") == {} and outcome(connection="two") == "unknown"
            assert run(service="other") == {} and outcome(service="other") == "unknown"
            assert run(session="other") == {}
            assert run(dry_run=True) == {} and outcome(dry_run=True) == "not_matched"
            assert run(dry_run=None) == {} and outcome(dry_run=None) == "unknown"
            assert run("UPDATE example ORDER", note="UPDATE") == {} and outcome("UPDATE example ORDER", note="UPDATE") == "not_matched"
            assert run("SELECT SettlementTarget") == {} and outcome("SELECT SettlementTarget") == "not_matched"

            start("older")
            start("newer")
            complete("newer", False)
            complete("older", True)
            assert run() == {} and outcome() == "not_matched"
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
            assert run() == {} and outcome() == "unknown"

            start("previous-input")
            hook("UserPromptSubmit", turn="turn-2")
            complete("previous-input", True, turn="turn-1")
            assert run(turn="turn-2") == {} and outcome(turn="turn-2") == "unknown"
            start("login-2", turn="turn-2")
            complete("login-2", True, turn="turn-2")
            # Same session: the pending challenge still covers this entry's
            # body, so the hit proceeds while the fact gate stays verifiable.
            assert run(turn="turn-2") == {} and outcome(turn="turn-2") == "matched"
            with closing(sqlite3.connect(data / "memory.db")) as db:
                rows = db.execute("SELECT * FROM probe_context_facts").fetchall()
                assert rows and "DO_NOT_STORE_RESPONSE" not in str(rows) and "fixture" not in str(rows), rows
                db.execute("UPDATE probe_context_facts SET expires=0")
                db.commit()
            assert run(turn="turn-2") == {} and outcome(turn="turn-2") == "unknown"
        finally:
            host.close()
    print("PASS: configured Pre/Post dispatch, cue comparisons, scoped facts, field boundaries, late results, and expiry")


if __name__ == "__main__":
    main()
