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
            {"tool": "Write", "mode": "replace", "path_field": "file_path",
             "text_fields": ["content"], "extensions": [".sql"]},
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
            assert miss["steps"][0]["detail"]["text_fields"][0]["status"] == "present", miss
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
            assert check(updated["id"], tool="TodoWrite")["outcome"] == "tool_not_routed"
            outside = check(updated["id"], path=root / "separate.sql")
            assert outside["outcome"] == "no_scanned_fields" and "separate repository" in outside["next"], outside
            assert outside["steps"][0]["detail"]["outcome"] == "path_outside_project", outside
            assert check(updated["id"], path="example.txt")["steps"][0]["detail"]["outcome"] == "extension_mismatch"
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

            config_rules = json.loads(rules.read_text(encoding="utf-8"))
            rule = config_rules["projects"][scope][0]
            del rule["extensions"], rule["path_field"]
            rule["when"] = {"field": "input.file_path", "op": "suffix", "value": ".sql", "case_sensitive": False}
            rules.write_text(json.dumps(config_rules), encoding="utf-8")
            uppercase_suffix = check(sensitive["id"], path="EXAMPLE.SQL", content="UPDATE table SET work_order_feedback_FIX_SECOND=1")
            assert uppercase_suffix["outcome"] == "direct_match", uppercase_suffix
        finally:
            host.close()
    print("PASS: expected cue, whole identifier correction, actual routing, input diagnostics, and unchanged library")


if __name__ == "__main__":
    main()
