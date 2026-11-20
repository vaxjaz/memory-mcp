#!/usr/bin/env python3
"""Actual stdio executable + disposable native-event replays. No model claims."""
import argparse
from concurrent.futures import ThreadPoolExecutor
from contextlib import closing
import json
from pathlib import Path
import re
import sqlite3
import statistics
import subprocess
import tempfile
import time

from smoke import Host, rejects_cue_text



def check_zcode_probe_matrix(host, hook, scope, project, data, sql_tool, cue_text_rejected):
    sql = "SELECT PRIVATE_SQL_MARKER FROM information_schema.columns"
    evidence = dict(kind="runtime", reference="synthetic-sql", observation="Isolated Hook replay marker")
    entrances = ["information_schema"]
    cue = dict(cue_id="metadata", label="Metadata",
               evidence_refs=["synthetic-sql"], matches=[
                   dict(op="token", entrances=entrances),
                   dict(op="token", normalize="whitespace", entrances=["SHOW COLUMNS", "刷数据"]),
               ])
    # Historical-tag binaries predate the write boundary: they require cue
    # text and legitimately accept it. Newer targets omit it and must reject
    # an authored copy.
    if not cue_text_rejected:
        cue["text"] = "Fixture requires a prior metadata query"
    case = dict(scope=scope, project=str(project), title="Synthetic SQL metadata rule",
                admission=dict(future_decision="Verify columns before proposing a query.", evidence_ref="synthetic-sql"),
                experience=dict(pitfall="A lookup uses an unverified column name",
                                counterexample="Fixture requires a prior metadata query",
                                impact="An unsuitable query can fail",
                                applicability="Synthetic metadata lookups",
                                boundary="Only this disposable fixture",
                                evidence=[evidence], cues=[cue]))
    saved = host.tool("memory_experience_record", case)
    if cue_text_rejected:
        authored = json.loads(json.dumps(case))
        authored["experience"]["cues"][0]["text"] = "Fixture requires a prior metadata query"
        rejected = host.tool("memory_experience_record", authored, expect_error=True)
        assert "experience.cues[0].text" in rejected["content"][0]["text"], rejected

    for label, tool, args, expected in (
            ("bash-command", "Bash", {"command": "grep information_schema fixture.sql"}, True),
            ("bash-description", "Bash", {"command": "grep unrelated fixture.sql", "description": sql}, False),
            ("read-path", "Read", {"file_path": str(project / "information_schema.sql")}, True),
            ("grep-pattern", "Grep", {"pattern": "information_schema"}, True),
            ("mysql-sql", sql_tool, {"sql": sql}, True),
            ("mysql-command", sql_tool, {"command": sql}, False),
            ("write-command", "Write", {"command": sql}, False),
            ("bash-business-entrance", "Bash", {"command": "grep '刷数据' fixture.sql"}, True),
            ("bash-ordinary-command", "Bash", {"command": "echo '刷数据'"}, False),
            ("bare-select-unregistered", "Bash", {"command": "grep SELECT fixture.sql"}, False),
            ("show-columns", sql_tool, {"sql": "show    columns from fixture"}, True),
            ("bare-mysql-query", "mysql_query", {"query": sql}, True),
            ("private-field", sql_tool, {"sql": "SELECT 1", "password": sql}, False),
            ("unknown-mcp-tool", "mcp__unrelated__arbitrary_sql", {"sql": sql}, False)):
        output = hook("PreToolUse", session=label, tool_name=tool, tool_input=args)[1]
        assert (output.get("decision") == "block") == expected, (label, output)

    prompt = hook("UserPromptSubmit", session="task-word-in-prompt", prompt="刷数据")[1]
    assert prompt == {}, prompt
    started = hook("SessionStart", session="start-banner")[1]["hookSpecificOutput"]["additionalContext"]
    assert "记忆经验" in started and "Fixture requires" not in started, started

    with ThreadPoolExecutor(max_workers=2) as pool:
        same = list(pool.map(lambda tool: hook("PreToolUse", session="parallel-same-candidate",
                                              tool_name=tool, tool_input={"sql": sql})[1],
                             (sql_tool, "mcp__second-instance__mysql_query")))
    assert sum(result.get("decision") == "block" for result in same) == 1, same

    blocked = hook("PreToolUse", session="missing-result", tool_name=sql_tool, tool_input={"sql": sql})[1]
    token = re.search(r'challenge_id="([a-f0-9]{32})"', blocked["reason"]).group(1)
    lookup_name = "mcp__decision-memory__memory_experience_lookup"
    args = dict(scope=scope, task="Synthetic SQL replay", questions="Check metadata",
                keywords=["metadata"], challenge_id=token)
    lookup_event = dict(session="missing-result", tool_name=lookup_name, tool_use_id="lookup-with-no-result",
                        tool_input=args)
    assert hook("PreToolUse", **lookup_event)[1] == {}
    generated = host.tool("memory_experience_lookup", args)
    assert generated["receipt_id"] and generated["challenge_outcome"] == "response_generated"
    code, output, stderr, _ = hook("PostToolUse", **lookup_event)
    assert (code, output, stderr) == (0, {}, ""), (code, output, stderr)
    assert hook("PreToolUse", session="missing-result", tool_name=sql_tool, tool_input={"sql": sql})[1] == {}

    with closing(sqlite3.connect(data / "memory.db")) as db:
        runs = list(db.execute("SELECT result, detail FROM hook_runs"))
        assert db.execute("SELECT count(*) FROM experience_challenges WHERE session='parallel-same-candidate'").fetchone() == (1,)
        delivered = db.execute("SELECT count(*) FROM experience_body_deliveries").fetchone()[0]
        observed = db.execute("SELECT receipt_id FROM experience_lookup_calls WHERE call_id='lookup-with-no-result'").fetchone()
    assert delivered == 0 and observed == ("",), (delivered, observed)
    assert ("lookup_result_missing", lookup_name) in runs, runs
    assert ("business_probe_no_clues", sql_tool) in runs, runs
    assert ("business_probe_no_match", sql_tool) in runs, runs
    assert not any("PRIVATE_SQL_MARKER" in detail for _, detail in runs), runs

    # Prove the six-character SQL keyword can match if explicitly indexed.
    # Keep broad keywords confined to this fixture.
    entrances.append("SELECT")
    updated = host.tool("memory_experience_record", dict(case, previous_id=saved["id"]))
    assert updated["id"] == saved["id"], updated
    for label, command, expected in (
            ("explicit-select", "grep select fixture.sql", True),
            ("select-substring", "grep SELECTED fixture.sql", False),
            ("select-ordinary-command", "echo SELECT", False)):
        output = hook("PreToolUse", session=label, tool_name="Bash", tool_input={"command": command})[1]
        assert (output.get("decision") == "block") == expected, (label, output)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary")
    parser.add_argument("--if-supported", action="store_true")
    opt = parser.parse_args()
    binary = str(Path(opt.binary).resolve())
    if "--enable-wakeup" not in subprocess.check_output([binary, "help"], text=True, encoding="utf-8"):
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

            setup = cli("setup", *options)
            assert setup["wakeup_enabled"] and setup["experience_hooks_installed"]
            doctor = cli("doctor", *options)
            assert doctor["configuration_consistent"] and doctor["wakeup_enabled"], doctor
            assert doctor["experience_hooks_installed"] and doctor["experience_index"]["cold_start"], doctor
            hooks_file = config if adapter != "codex" else user / "hooks.json"
            cfg = json.loads(hooks_file.read_text(encoding="utf-8"))
            events = cfg["hooks"]["events"] if adapter == "zcode" else cfg["hooks"]
            assert not events.get("Stop"), events
            handler = events["PreToolUse"][0]["hooks"][0]
            # Reinstall must remove an owned legacy Stop handler, preserving
            # historical contents rather than maintaining a parallel workflow.
            events["Stop"] = [{"hooks": [dict(handler)]}]
            hooks_file.write_text(json.dumps(cfg, ensure_ascii=False), encoding="utf-8")
            cli("setup", *options)
            current_cfg = json.loads(hooks_file.read_text(encoding="utf-8"))
            current_events = current_cfg["hooks"]["events"] if adapter == "zcode" else current_cfg["hooks"]
            assert not current_events.get("Stop"), current_events
            with closing(sqlite3.connect(data / "memory.db")) as db:
                db.execute("INSERT INTO entries(id,root_id,previous_id,kind,scope,title,body,origin,source_ref,host,model,session,created_at,current,needs_review) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
                           ("legacy-source", "legacy-source", "", "record", scope,
                            "Preserved legacy fixture", "Historical source must survive the workflow change",
                            "user_supplied", "isolated historical fixture", "", "", "",
                            "2026-01-01T00:00:00Z", 1, 0))
                db.commit()
            matcher = events["PreToolUse"][0]["matcher"]
            assert re.search(matcher, "Grep") and re.search(matcher, "run_in_terminal")
            assert re.search(matcher, "mcp__decision_memory__memory_experience_lookup")
            assert not re.search(matcher, "apply_patch")

            def hook(event, turn="turn-1", session="real-executable-fixture", **fields):
                payload = dict(hook_event_name=event, cwd=str(project), session_id=session,
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
                assert "tool_field_mapping" in boot["features"]
                assert subprocess.check_output([binary, "guide"], text=True, encoding="utf-8") == boot["guide"]
                assert boot["workflow"] == "verified_experience"
                assert host.tool("memory_read", {"id": "legacy-source"})["entry"]["body"] == "Historical source must survive the workflow change"
                if adapter == "zcode":
                    assert "--experience-hooks" not in handler["args"], handler
                    sql_tool = "mcp__mysql-test__mysql_query"
                    assert re.search(matcher, sql_tool)
                    assert re.search(matcher, "mcp__another-service__mysql_query")
                    assert not re.search(matcher, "mcp__some_service__arbitrary_sql")
                    failure = current_events["PostToolUseFailure"][0]["matcher"]
                    assert re.search(failure, "mcp__decision-memory__memory_experience_lookup")
                    assert not re.search(failure, sql_tool)
                    check_zcode_probe_matrix(host, hook, scope, project, data, sql_tool, rejects_cue_text(binary))
                else:
                    assert "PostToolUseFailure" not in current_events, current_events
                draft = dict(scope=scope, project=str(project), title="Synthetic multi-entry risk",
                             admission=dict(future_decision="Check each entry point for the required validation.",
                                            evidence_ref="isolated fixture"),
                             request_id="experience-once",
                             experience=dict(cues=[dict(cue_id="admission",label="入口准入",evidence_refs=["isolated fixture"],matches=[dict(op="token",entrances=["canDispatchLynOrder"]),dict(op="token",normalize="whitespace",entrances=["LYN 创建准入"])])],
                                             pitfall="A local caller is assumed to cover every entry",
                                             counterexample="An explicitly synthetic isolated counterexample, not a real business rule",
                                             impact="One entry may miss a required admission check",
                                             applicability="Only when the same requirement must cover multiple entries",
                                             boundary="Other requirements and runtime conditions need fresh evidence",
                                             evidence=[dict(kind="code", reference="isolated fixture",
                                                            observation="Literal candidate method only",
                                                            path="Policy.java", symbol="canDispatchLynOrder")]))
                if not rejects_cue_text(binary):
                    draft["experience"]["cues"][0]["text"] = "合成多入口实验中本地调用未覆盖另一入口的准入条件。"
                saved = host.tool("memory_experience_record", draft)
                # Exercise the actual managed matcher and runtime using the same
                # bootstrap example across all adapters, with the actual scope.
                probes = data / "tool-probes.json"
                template = json.loads(boot["guide"].split("```json\n", 1)[1].split("\n```", 1)[0])
                probes.write_text(json.dumps({"projects": {scope: template["projects"]["project:example/team-repo"]}}),
                                  encoding="utf-8")
                stale = cli("doctor", *options)
                assert stale["probe_config_valid"] and stale["probe_matcher_mismatches"] == 1, stale
                configured = cli("setup", *options)
                assert configured["custom_probe_rules"] == 2, configured
                checked = cli("doctor", *options)
                assert checked["custom_probe_rules"] == 2 and checked["configuration_consistent"], checked
                updated_cfg = json.loads(hooks_file.read_text(encoding="utf-8"))
                updated_events = updated_cfg["hooks"]["events"] if adapter == "zcode" else updated_cfg["hooks"]
                pre = updated_events["PreToolUse"][0]["matcher"]
                post = updated_events["PostToolUse"][0]["matcher"]
                assert re.search(pre, "Write") and re.search(pre, "mcp__custom-server__warehouse_query"), pre
                assert not re.search(pre, "TodoWrite") and not re.search(post, "Write"), (pre, post)
                for tool, args in (
                        ("Write", {"file_path": "nested/new.sql", "content": "LYN 创建准入"}),
                        ("mcp__custom-server__warehouse_query", {"arguments": {"statement": "LYN 创建准入"}})):
                    code, output, _, _ = hook("PreToolUse", session=f"custom-probe-{tool}", tool_name=tool, tool_input=args)
                    assert code == 2 or output.get("decision") == "block", (adapter, tool, output)
                probes.write_text('{"projects": {}, "unknown": true}', encoding="utf-8")
                before = hooks_file.read_bytes()
                rejected = subprocess.run([binary, "setup", *options], capture_output=True, encoding="utf-8", timeout=30)
                assert rejected.returncode != 0 and hooks_file.read_bytes() == before
                if adapter == "zcode":
                    session = "custom-probe-Write"
                    code, output, _, _ = hook("PreToolUse", session=session, tool_name="Write",
                                              tool_input={"file_path": "nested/new.sql", "content": "LYN 创建准入"})
                    assert code == 0 and output == {}, output
                    diagnostic = cli("doctor", *options, "--session", session)
                    assert not diagnostic["probe_config_valid"], diagnostic
                    recent = diagnostic["recent_events"]
                    assert all(row["session"] == session for row in recent), recent
                    assert recent[0]["result"] == "probe_config_invalid", recent
                    assert recent[1]["result"] == "experience_probe", recent
                    code, output, _, _ = hook("PreToolUse", session="invalid-preset", tool_name="Grep",
                                              tool_input={"pattern": "LYN 创建准入"})
                    assert code == 0 and not output, output
                else:
                    assert not cli("doctor", *options)["probe_config_valid"]
                probes.unlink()
                assert cli("doctor", *options)["probe_matcher_mismatches"] == 1
                cli("setup", *options)
                assert cli("doctor", *options)["configuration_consistent"]
                case_count = 2 if adapter == "zcode" else 1
                assert cli("doctor", *options)["experience_index"]["eligible_cases"] == case_count
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
                assert again[:3] == (0, {}, ""), again
                host.tool("memory_experience_lookup", dict(scope=scope, task="", questions="coverage",
                          keywords=["LYN"], challenge_id=token), expect_error=True)
                lookup_args = dict(scope=scope,
                                     task="Current fixture multi-entry admission change",
                                     questions="Does the candidate method cover the current real entries?",
                                     keywords=["LYN"], challenge_id=token)
                lookup_name="mcp__decision_memory__memory_experience_lookup"
                assert hook("PreToolUse",tool_name=lookup_name,tool_use_id="native-1",tool_input=lookup_args)[1]=={}
                wire = host.request("tools/call", {"name": "memory_experience_lookup", "arguments": lookup_args})
                assert not wire.get("isError") and "structuredContent" not in wire, wire
                assert len(wire["content"]) == 1, wire
                assert len(wire["content"][0]["text"].encode()) <= 8192
                response = json.loads(wire["content"][0]["text"])
                assert response["items"][0]["id"] == saved["id"]
                assert response["items"][0]["locations"][0]["lines"] == [2]
                assert response["challenge_outcome"] == "response_generated"
                waiting = hook("PreToolUse", **payload)
                assert waiting[:3] == (0, {}, ""), waiting
                with closing(sqlite3.connect(data / "memory.db")) as db:
                    before_status = tuple(db.iterdump())
                state = cli("doctor", *options, "--challenge", token)
                assert state["challenge_id"] == token and state["stage"] == "response_pending_observation" and state["read_only"], state
                assert state["candidates"] == [{"id": saved["id"], "cue_id": "admission"}], state
                with closing(sqlite3.connect(data / "memory.db")) as db:
                    assert tuple(db.iterdump()) == before_status
                    assert db.execute("SELECT count(*) FROM experience_body_deliveries").fetchone() == (0,)
                assert hook("PostToolUse",tool_name=lookup_name,tool_use_id="native-1",tool_response=wire)[1]=={}
                assert cli("doctor", *options, "--challenge", token)["stage"] == "delivered"
                assert hook("PreToolUse", **payload)[1] == {}
                assert hook("Stop", last_assistant_message="Do not start an extra model round")[1] == {}
                with closing(sqlite3.connect(data / "memory.db")) as db:
                    assert db.execute("select count(*) from entries").fetchone()[0] == case_count + 1
                    assert "PRIVATE_PROMPT_DO_NOT_COPY" not in json.dumps(list(db.execute("select * from entries")))
                    assert db.execute(
                        "select count(*) from hook_runs where event='PostToolUse' and result='lookup_result_observed'"
                    ).fetchone()[0] == 1
                assert (project / "AGENTS.md").read_bytes() == rules
                # A new native input preserves body/cue delivery.
                hook("UserPromptSubmit", turn="turn-2", prompt="Second isolated request")
                gated = hook("PreToolUse", turn="turn-2", **payload)
                assert gated[1] == {}, gated
                if adapter == "zcode":
                    diagnostic = cli("doctor", *options, "--session", "real-executable-fixture")
                    assert diagnostic["probe_config_valid"], diagnostic
                    recent = diagnostic["recent_events"]
                    assert all(row["session"] == "real-executable-fixture" for row in recent), recent
                    assert recent[0]["result"] == "experience_probe", recent
                    assert json.loads(recent[0]["detail"])["outcome"] == "silent", recent
                    assert cli("doctor", *options, "--session", "empty-session")["recent_events"] == []
                disabled = cli("setup", *options, "--disable-wakeup")
                assert not disabled["wakeup_enabled"] and not disabled["experience_hooks_installed"]
                assert host.tool("memory_read", dict(id=saved["id"]))["entry"]["current"]
                cli("setup", *options)
                assert not cli("doctor", *options)["wakeup_enabled"], "reinstall reset an explicit disabled policy"
                assert hook("UserPromptSubmit", turn="disabled-input", prompt="No fallback reminder")[1] == {}
                assert hook("PreToolUse", turn="disabled-input", **payload)[1] == {}
                if adapter == "zcode":
                    plan = host.tool("memory_cleanup", dict(action="preview", scope=scope,
                                     operations=[dict(entry_id=saved["id"], action="distill",
                                         summary="Isolated verified pattern with the same scope and boundary",
                                         reason="Exercise explicit maintenance without a generic write tool",
                                         evidence_ids=[saved["id"]])]))["plan"]
                    approval = host.tool("memory_cleanup", dict(action="confirm", scope=scope,
                                         plan_id=plan["id"], purpose="apply",
                                         approval_quote="Apply this exact displayed fixture plan",
                                         request_id="fixture-approval"))["approval_source_id"]
                    applied = host.tool("memory_cleanup", dict(action="apply", scope=scope,
                                        plan_id=plan["id"], approval_source_id=approval,
                                        approval_quote="Apply this exact displayed fixture plan"))
                    assert applied["plan"]["status"] == "applied"
                    host.tool("memory_cleanup", dict(action="undo", scope=scope,
                              plan_id=plan["id"], approval_source_id=approval,
                              approval_quote="Apply this exact displayed fixture plan"), expect_error=True)
                    undo = host.tool("memory_cleanup", dict(action="confirm", scope=scope,
                                     plan_id=plan["id"], purpose="undo",
                                     approval_quote="Undo this exact displayed fixture plan",
                                     request_id="fixture-undo"))["approval_source_id"]
                    undone = host.tool("memory_cleanup", dict(action="undo", scope=scope,
                                       plan_id=plan["id"], approval_source_id=undo,
                                       approval_quote="Undo this exact displayed fixture plan"))
                    assert undone["plan"]["status"] == "undone"
                    assert not host.tool("memory_search", dict(scope=scope, query="displayed",
                                         include_events=True))["results"]
            finally:
                host.close()
    print(json.dumps({"result": "PASS", "coverage": "four adapter configurations and actual subprocess protocols; no native/model-quality claim",
                      "no_match_hook_ms_by_adapter": {
                          name: {"median": round(statistics.median([t for a, t in timings if a == name]), 1),
                                 "max": round(max(t for a, t in timings if a == name), 1)}
                          for name in {a for a, _ in timings}}}, ensure_ascii=False))


if __name__ == "__main__":
    main()
