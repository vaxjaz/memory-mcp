#!/usr/bin/env python3
"""Verify the user-facing read-only cue-check workflow on an isolated library."""
from contextlib import closing
import json
from pathlib import Path
import sqlite3
import subprocess
import sys
import tempfile

from smoke import Host


def main():
    binary = str(Path(sys.argv[1]).resolve())
    scope = "project:cue-check"
    with tempfile.TemporaryDirectory(prefix="memory-cue-check-") as temp:
        root = Path(temp)
        project, data, config = root / "project", root / "library", root / "config.json"
        project.mkdir()
        data.mkdir()
        config.write_text("{}", encoding="utf-8")
        rules = data / "tool-probes.json"
        rules.write_text(json.dumps({"projects": {scope: [
            {"tool": "Write", "fields": {"path": "file_path", "text": "content"}},
        ]}}), encoding="utf-8")
        options = ["--host", "zcode", "--config", str(config), "--data-dir", str(data),
                   "--project", str(project), "--scope", scope]
        subprocess.run([binary, "setup", *options], capture_output=True, check=True)
        gate, short, full = "UPDATE", "fix_third", "work_order_feedback_fix_third"
        host = Host(binary, str(data))
        def record(title, entrance):
            return dict(scope=scope, project=str(project), title=title, experience=dict(
                pitfall=title, counterexample="Synthetic cue verification fixture", impact="Could miss the intended cue",
                applicability="Isolated CLI test", boundary="No business tool executed",
                evidence=[dict(kind="runtime", reference="fixture", observation="Verified in the fixture")],
                cues=[dict(cue_id="write", label="Fixture", text="The synthetic operation has an observable entrance.",
                           evidence_refs=["fixture"], matches=[dict(op="token", entrances=[entrance])])]))
        def snapshot():
            with closing(sqlite3.connect((data / "memory.db").as_uri() + "?mode=ro", uri=True)) as db:
                return tuple(db.iterdump())
        sample_marker = "NOT_SAVED_PROBE_INPUT"
        def check(record_id, tool="Write", path="example.sql", content=f"{gate} table SET {full}=1; /* {sample_marker} */"):
            body = dict(tool_name=tool, tool_input=dict(file_path=str(path), content=content))
            response = subprocess.run([binary, "doctor", *options, "--probe=-", "--expect", record_id, "--expect-cue", "write"],
                                      input=json.dumps(body), encoding="utf-8", capture_output=True, check=True)
            assert sample_marker not in response.stdout, response.stdout
            return json.loads(response.stdout)
        try:
            general = host.tool("memory_experience_record", record("Generic fixture gate", gate))
            draft = record("Specific full identifier fixture", short)
            specific = host.tool("memory_experience_record", draft)
            before = snapshot()
            miss = check(specific["id"])
            assert miss["outcome"] == "cue_missed" and "full identifier" in miss["next"], miss
            assert [item["id"] for item in miss["candidates"]] == [general["id"]], miss
            assert miss["expected"]["trigger_preview"]["entrances"][0]["value"] == short, miss
            assert miss["tool_input"]["fields"][1] == {"field": "text", "input_path": "content", "status": "present",
                                                          "bytes": len(f"{gate} table SET {full}=1; /* {sample_marker} */")}, miss
            assert miss["cue_checks"][0]["outcome"] == "not_matched", miss
            assert {field["path"]: field["type"] for field in miss["observed_fields"]} == {"content": "string", "file_path": "string"}, miss
            assert miss["read_only"] and not miss["native_event_verified"], miss
            assert snapshot() == before

            draft["previous_id"] = specific["id"]
            draft["experience"]["cues"][0]["matches"][0]["entrances"][0] = full
            updated = host.tool("memory_experience_record", draft)
            assert updated["id"] == specific["id"], updated
            before = snapshot()
            direct = check(updated["id"])
            assert direct["outcome"] == "direct_match" and len(direct["candidates"]) == 2, direct
            assert all(item["source"] == "direct" for item in direct["candidates"]), direct
            unregistered = check(updated["id"], tool="TodoWrite")
            assert unregistered["outcome"] == "tool_not_registered" and len(unregistered["observed_fields"]) == 2, unregistered
            outside = check(updated["id"], path=root / "separate.sql")
            assert outside["outcome"] == "direct_match", outside
            assert snapshot() == before and config.is_file()

            draft["previous_id"] = updated["id"]
            draft["experience"]["cues"][0]["matches"] = [
                {"op": "contains", "case_sensitive": True, "entrances": ["fix_Second", "FIX_SECOND"]},
            ]
            sensitive = host.tool("memory_experience_record", draft)
            assert sensitive["id"] == updated["id"], sensitive
            assert [a["value"] for a in sensitive["trigger_preview"]["entrances"]] == ["fix_Second", "FIX_SECOND"]
            assert check(sensitive["id"], content="UPDATE table SET work_order_feedback_fix_second=1")["outcome"] == "cue_missed"
            upper = check(sensitive["id"], content="UPDATE table SET work_order_feedback_fix_Second=1")
            assert upper["outcome"] == "direct_match", upper
            chosen = next(row for row in upper["candidates"] if row["id"] == sensitive["id"])
            assert chosen["op"] == "contains" and chosen["case_sensitive"] and chosen["entrance"] == "fix_Second", chosen

            draft["previous_id"] = sensitive["id"]
            draft["experience"]["cues"][0]["matches"] = [{"all": [
                {"field": "path", "op": "suffix", "value": ".sql"},
                {"field": "text", "op": "token", "value": "update"},
                {"field": "text", "op": "contains", "entrances": ["fix_third"]},
            ]}]
            combined = host.tool("memory_experience_record", draft)
            assert combined["id"] == sensitive["id"] and combined["trigger_preview"]["entrances"][0]["conditional"], combined
            before = snapshot()
            positive = check(combined["id"], path=root / "EXAMPLE.SQL",
                             content="UPDATE demo SET work_order_feedback_FIX_THIRD=1")
            assert positive["outcome"] == "direct_match" and len(positive["cue_checks"][0]["checks"]) == 3, positive
            select = check(combined["id"], content="SELECT work_order_feedback_FIX_THIRD FROM demo")
            assert select["outcome"] == "cue_missed" and select["cue_checks"][0]["checks"][-1]["value"] == "update", select
            renamed = check(combined["id"], path="example.txt")
            assert renamed["outcome"] == "cue_missed" and renamed["cue_checks"][0]["checks"][-1]["field"] == "path", renamed
            only_path = check(combined["id"], path="fix_third.sql", content="UPDATE demo SET unrelated=1")
            assert only_path["outcome"] == "cue_missed", only_path
            missing = check(combined["id"], content=None)
            assert missing["outcome"] == "cue_missed" and missing["cue_checks"][0]["outcome"] == "unknown", missing
            assert snapshot() == before and config.is_file()
            bad = json.loads(json.dumps(draft))
            bad["experience"]["cues"][0]["matches"][0]["all"][1]["case_sensitiv"] = True
            host.tool("memory_experience_record", bad, expect_error=True)
            assert snapshot() == before
        finally:
            host.close()
    print("PASS: expected cue, whole identifier correction, actual routing, input diagnostics, and unchanged library")


if __name__ == "__main__":
    main()
