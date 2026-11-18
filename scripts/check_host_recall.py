#!/usr/bin/env python3
"""Actual MCP calls in a disposable library; not a model-quality claim."""
import argparse
import json
from pathlib import Path
import subprocess
import tempfile
import time
from smoke import Host


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary")
    opt = parser.parse_args()
    binary = str(Path(opt.binary).resolve())
    with tempfile.TemporaryDirectory(prefix="host-recall-isolated-") as temp:
        root = Path(temp)
        project = root / "business"
        project.mkdir()
        source = project / "agreement.txt"
        source.write_text("Synthetic owner-confirmed agreement, only for protocol tests.", encoding="utf-8")
        data = root / "library"
        config = root / "config.json"
        config.write_text("{}", encoding="utf-8")
        scope = "project:host-recall-fixture"
        setup = [binary, "setup", "--host", "zcode", "--config", str(config),
                 "--data-dir", str(data), "--project", str(project), "--scope", scope]
        subprocess.run(setup, check=True, capture_output=True)
        host = Host(binary, str(data))
        try:
            draft = dict(scope=scope, project=str(project), title="Synthetic failure recognition",
                         request_id="fixture-experience", experience=dict(
                             object_id_prefixes=["OBJ"],
                             cues=[dict(cue_id="cancellation",label="状态约束",text="合成取消实验中本地状态未覆盖关联影响限制。",evidence_refs=["fixture:runtime"],entrances=[dict(kind="business_signal",value="取消 OBJ-001")])],
                             pitfall="An isolated task assumes a local status represents every condition",
                             counterexample="Synthetic observation: 取消受状态约束与关联影响限制",
                             impact="An isolated state transition may miss a required condition",
                             applicability="Only the documented synthetic cancellation scenario",
                             boundary="Approval withdrawal and inspection alone are different tasks",
                             evidence=[dict(kind="runtime", reference="fixture:runtime",
                                            observation="Only a protocol fixture; not a real incident")]))
            obsolete = json.loads(json.dumps(draft))
            obsolete["experience"]["anchors"] = ["ObjectType"]
            host.tool("memory_experience_record", obsolete, expect_error=True)
            saved = host.tool("memory_experience_record", draft)
            assert saved["indexed"]
            assert host.tool("memory_experience_record", draft)["id"] == saved["id"]
            reference = dict(scope=scope, project=str(project), title="Synthetic confirmed convention",
                             request_id="fixture-agreement", agreement=dict(
                                 object_id_prefixes=["OBJ"],
                                 cues=[dict(cue_id="confirm", label="确认要求",
                                            text="Synthetic agreement: obtain current evidence before preparing an attachment",
                                            evidence_refs=["fixture:owner-confirmed"],
                                            entrances=[dict(kind="business_signal", value="取消 OBJ-001")])],
                                 requirement="Synthetic agreement: obtain current evidence before preparing an attachment",
                                 applicability="Only the isolated example business scope",
                                 boundary="No database access permission and no approval withdrawal",
                                 authority="isolated fixture owner", confirmation_reference="fixture:owner-confirmed",
                                 evidence=[dict(kind="business", reference="fixture:owner-confirmed",
                                                path="agreement.txt", observation="Synthetic existing agreement")]))
            obsolete = json.loads(json.dumps(reference))
            obsolete["agreement"]["signals"] = ["取消"]
            host.tool("memory_reference_record", obsolete, expect_error=True)
            agreement = host.tool("memory_reference_record", reference)
            assert agreement["indexed"]
            query = dict(scope=scope, project=str(project), task="Compare the current cancellation scenario",
                         questions="Which observed constraints challenge the current assumption?",
                         queries=[dict(terms=["状态约束"], match="all"),
                                  dict(terms=["关联影响"], match="all")])
            started = time.perf_counter()
            result = host.tool("memory_experience_lookup", query)
            elapsed = (time.perf_counter() - started) * 1000
            assert result["query_count"] == 2
            assert len(result["items"]) == 1 and result["items"][0]["id"] == saved["id"]
            assert result["items"][0]["matched_queries"] == [0, 1]
            assert len(json.dumps(result, ensure_ascii=False).encode()) <= 8192
            # Search and page the two source identities through the SAME tool.
            query.update(queries=[dict(terms=["取消"], match="all")], limit=1)
            page = host.tool("memory_experience_lookup", query)
            assert page["has_more"] and page["next_cursor"]
            first = page["items"][0]["id"]
            query["cursor"] = page["next_cursor"]
            second = host.tool("memory_experience_lookup", query)
            assert second["items"][0]["id"] != first and not second["has_more"]
            assert {first, second["items"][0]["id"]} == {saved["id"], agreement["id"]}
            bad = dict(query, queries=[dict(terms=["其他条件"])])
            host.tool("memory_experience_lookup", bad, expect_error=True)
            source.write_text("Changed synthetic agreement.", encoding="utf-8")
            query.pop("cursor")
            query["limit"] = 3
            drift = host.tool("memory_experience_lookup", query)
            ref = next(x for x in drift["items"] if x["id"] == agreement["id"])
            assert ref["source_kind"] == "agreement_reference" and ref["experience"] is None
            assert ref["locations"][0]["content_changed"]
            no_authority = json.loads(json.dumps(reference))
            no_authority["request_id"] = "unconfirmed"
            no_authority["agreement"]["authority"] = ""
            host.tool("memory_reference_record", no_authority, expect_error=True)
            # Run the configured executable hook, not only the store methods.
            events = json.loads(config.read_text())["hooks"]["events"]
            handler = events["PreToolUse"][0]["hooks"][0]
            def hook(name, **fields):
                payload = dict(hook_event_name=name, cwd=str(project), session_id="business-fixture",
                               turn_id="business-input", event_id="business-input", **fields)
                process = subprocess.run([handler["command"], *handler["args"]],
                                         input=json.dumps(payload, ensure_ascii=False),
                                         text=True, encoding="utf-8", capture_output=True, check=True)
                return json.loads(process.stdout) if process.stdout.strip() else {}
            assert hook("UserPromptSubmit", prompt="PRIVATE_PROMPT_SENTINEL") == {}
            assert hook("PostToolUse", tool_name="mcp__office__get_ticket",
                        tool_response={"content":[{"type":"text","text":"取消 OBJ-001 PRIVATE_RESULT_SENTINEL"}]}) == {}
            assert hook("PreToolUse", tool_name="mcp__mysql__query",
                        tool_input={"sql":"select status from t where object_no='OBJ-002'"}) == {}
            blocked = hook("PreToolUse", tool_name="mcp__mysql__query",
                           tool_input={"sql":"select status from t where object_no='OBJ-001'"})
            assert blocked["hookSpecificOutput"]["permissionDecision"] == "deny", blocked
            import re
            challenge = re.search(r'challenge_id="([a-f0-9]{32})"', blocked["reason"]).group(1)
            lookup_args = dict(scope=scope, task="Current synthetic cancellation investigation",
                questions="Compare source conditions against actual evidence",
                queries=[dict(terms=["状态约束"])], challenge_id=challenge)
            lookup_name="mcp__decision_memory__memory_experience_lookup"
            assert hook("PreToolUse",tool_name=lookup_name,tool_use_id="native-business-lookup",tool_input=lookup_args)=={}
            resolved = host.tool("memory_experience_lookup",lookup_args)
            assert resolved["challenge_outcome"] == "response_generated"
            assert {x["id"] for x in resolved["items"]} == {saved["id"], agreement["id"]}
            assert hook("PostToolUse",tool_name=lookup_name,tool_use_id="native-business-lookup",tool_response={"content":[{"type":"text","text":json.dumps(resolved)}]})=={}
            assert hook("PreToolUse", tool_name="mcp__mysql__query",
                        tool_input={"sql":"select status from t where object_no='OBJ-001'"}) == {}
            assert not host.tool("memory_search", dict(scope=scope, query="PRIVATE_PROMPT_SENTINEL PRIVATE_RESULT_SENTINEL",
                                                      include_events=True))["results"]
        finally:
            host.close()
        print(json.dumps(dict(result="PASS", coverage="actual executable MCP: indexed writes, multi-query recall, source identity, paging, drift and admission",
                              model_quality_verified=False, query_ms=round(elapsed, 2),
                              original_library_touched=False), ensure_ascii=False))


if __name__ == "__main__":
    main()
