#!/usr/bin/env python3
"""Trigger matrix through the real executable: which tool calls reach a cue, and why the others do not.

Axes: carrier (Read, Grep, Bash, Write, Edit, MCP tool) x session directory (project root, a module,
another project) x path form (relative, absolute, sibling module, outside, the root itself, instruction
file, a junction leaving the project, an unexpanded home) x input shape (string, string array, object
array, element by index). It also checks that setup writes the default tool rules into tool-probes.json
and that a skipped call leaves its reasons behind.
"""
import argparse
import json
import os
import subprocess
import sys
import tempfile
from pathlib import Path

from check_scenarios import SCOPE_A, SCOPE_B, SERVER, Rig, Report, write

ENTRANCE, ASK = "entMatrix", "entAsk"


def experience(request_id, cue_id, matches):
    return {"scope": SCOPE_A, "title": "matrix " + cue_id, "request_id": request_id,
            "admission": {"future_decision": "Check the configuration source before changing it.", "evidence_ref": "ev1"},
            "experience": {"fact": "Changes touching %s need the owner's confirmation first." % ENTRANCE,
                           "evidence": [{"kind": "runtime", "reference": "ev1", "observation": "isolated matrix material"}],
                           "cues": [{"cue_id": cue_id, "label": "matrix", "evidence_refs": ["ev1"], "matches": matches}]}}


def probe(rig, tool, arguments, cwd, record, cue="c1"):
    return rig.probe({"tool_name": tool, "tool_input": arguments, "cwd": str(cwd)}, "--expect", record, "--expect-cue", cue)


def field_status(report, alias):
    for field in report.get("tool_input", {}).get("fields", []):
        if field["field"] == alias:
            return field["status"]
    return "absent"


def read_rules(rig):
    return json.loads((rig.library / "tool-probes.json").read_text(encoding="utf-8"))


def write_rules(rig, document, scope=SCOPE_A, project=None):
    """Store tool-probes.json and refresh the client matchers; return setup's (code, stdout, stderr).

    The file is written as bytes so it keeps LF line endings on every system, as setup writes it.
    """
    text = json.dumps(document, ensure_ascii=False, indent=2) + "\n"
    (rig.library / "tool-probes.json").write_bytes(text.encode("utf-8"))
    return rig.setup(scope, project or rig.a)


def make_junction(link, target):
    """Create a directory junction on Windows; report whether the system allowed it."""
    try:
        import _winapi
        _winapi.CreateJunction(str(target), str(link))
        return os.path.lexists(link)
    except (ImportError, OSError):
        return False


def seeding(report, rig):
    print("\n== tool-probes.json is written by setup ==")
    # A new library needs its own client configuration; an existing service never changes its data directory.
    fresh = rig.root / "fresh-library"
    fresh_config = rig.root / "fresh-config.json"
    fresh_config.write_text("{}", encoding="utf-8")
    code, out, err = rig.cli("setup", "--host", "zcode", "--config", str(fresh_config), "--data-dir", str(fresh), "--server-name", SERVER,
                             "--scope", SCOPE_A, "--project", rig.a, "--dry-run")
    fresh_report = json.loads(out) if code == 0 else {}
    report.check("setup --dry-run reports the tools it would list and writes nothing",
                 code == 0 and fresh_report.get("tool_rules", {}).get("created") is True and not (fresh / "tool-probes.json").exists(), out + err)
    path = rig.library / "tool-probes.json"
    listed = {rule.get("tool") or "mcp:" + rule.get("mcp_tool", ""): rule for rule in read_rules(rig).get("tools", [])}
    report.check("first setup creates tool-probes.json with the default tools", {"Bash", "Read", "Grep", "Write", "Edit", "mcp:mysql_query"} <= set(listed), sorted(listed))
    report.check("writing and editing tools are listed but disabled", listed.get("Write", {}).get("enabled") is False and listed.get("Edit", {}).get("enabled") is False)
    report.check("the shell rule lists the commands it reads", {"head", "cat", "grep"} <= set(listed.get("Bash", {}).get("scan", [])) and "echo" not in listed["Bash"]["scan"])
    report.check("the file uses LF line endings and ends with a newline", b"\r" not in path.read_bytes() and path.read_bytes().endswith(b"\n"))
    before = path.read_bytes()
    rig.setup(SCOPE_A, rig.a)
    report.check("setup leaves a complete file alone", path.read_bytes() == before)

    document = read_rules(rig)
    document["tools"] = [rule for rule in document["tools"] if rule.get("tool") != "Read"]
    for rule in document["tools"]:
        if rule.get("tool") == "Bash":
            rule["scan"] = ["git"]
    document["projects"] = {SCOPE_A: [{"tool": "Write", "fields": {"text": "content", "path": "file_path"}}]}
    code, out, err = write_rules(rig, document)
    result = json.loads(out) if code == 0 else {}
    report.check("setup lists a removed default again and backs up the file first",
                 code == 0 and result.get("tool_rules", {}).get("added") == ["Read"] and bool(result.get("tool_rules", {}).get("backup")), out + err)
    after = read_rules(rig)
    bash = next(rule for rule in after["tools"] if rule.get("tool") == "Bash")
    report.check("setup keeps the project's own edit of a default", bash["scan"] == ["git"], bash)
    report.check("setup keeps the project's own rules", after["projects"][SCOPE_A][0]["tool"] == "Write")
    sources = {item["tool"]: item["source"] for item in rig.doctor("--scope", SCOPE_A, "--project", rig.a).get("probe_surface", [])}
    report.check("doctor lists the effective rules and where each comes from", sources.get("Write") == "project" and sources.get("Read") == "tools", sources)
    # The replay report used to say "tools" whatever rule was in effect.
    replayed = probe(rig, "Write", {"file_path": "entMatrix.sql", "content": "no entrance here"}, rig.a, CURRENT[0]).get("tool_input", {}).get("source")
    report.check("a replay names the rule that is really in effect", replayed == "project" and probe(rig, "Read", {"file_path": "entMatrix.md"}, rig.a, CURRENT[0]).get("tool_input", {}).get("source") == "tools", replayed)

    del document["tools"]
    code, out, err = write_rules(rig, document)
    result = json.loads(out) if code == 0 else {}
    report.check("a file from an earlier version gets every default listed and is backed up",
                 code == 0 and len(result.get("tool_rules", {}).get("added", [])) >= 20 and bool(result.get("tool_rules", {}).get("backup")), out + err)
    report.check("the project rule survived", read_rules(rig)["projects"][SCOPE_A][0]["tool"] == "Write")

    document = read_rules(rig)
    del document["tools"]
    (rig.library / "tool-probes.json").write_text(json.dumps(document), encoding="utf-8")
    implicit = probe(rig, "Read", {"file_path": "entMatrix.md"}, rig.a, CURRENT[0])
    report.check("without a tools section the defaults still apply and doctor says so",
                 rig.doctor().get("probe_tools_implicit") is True and implicit.get("outcome") == "direct_match" and implicit.get("tool_input", {}).get("source") == "default", implicit.get("tool_input"))
    (rig.library / "tool-probes.json").unlink()
    report.check("without the file the defaults still apply and doctor reports it missing",
                 rig.doctor().get("probe_config_missing") is True and probe(rig, "Read", {"file_path": "entMatrix.md"}, rig.a, CURRENT[0]).get("outcome") == "direct_match")
    (rig.library / "tool-probes.json").write_text(json.dumps({"tools": [], "projects": {}}), encoding="utf-8")
    report.check("an explicitly empty tools section scans no default tool until setup lists the defaults again",
                 probe(rig, "Read", {"file_path": "entMatrix.md"}, rig.a, CURRENT[0]).get("outcome") == "tool_not_registered")

    code, out, err = write_rules(rig, {"tools": [{"tool": "ask", "observe": [{"name": "ok", "key": ["input.connection_id"], "fields": {"ok": "ok"}}]}], "projects": {}})
    report.check("an observation under tools is refused with the reason", code != 0 and "projects.<scope>" in out + err, out + err)

    (rig.library / "tool-probes.json").unlink()
    rig.setup(SCOPE_A, rig.a)
    rig.setup(SCOPE_B, rig.b)
    document = read_rules(rig)
    document["projects"] = {SCOPE_A: [
        {"tool": "Write", "fields": {"text": "content", "path": "file_path"}},
        {"mcp_tool": "ask", "fields": {"text": ["missing", "questions.*.question"]}},
        {"mcp_tool": "tag", "fields": {"text": "tags"}},
        {"mcp_tool": "pick", "fields": {"text": "questions.1.question"}}]}
    code, out, err = write_rules(rig, document)
    report.check("the remaining checks start from a valid configuration", code == 0, out + err)


def paths(report, rig):
    print("\n== Read: path forms and the session directory ==")
    record = CURRENT[0]
    root, module, sibling = rig.a, rig.a / "mod-a", rig.a / "mod-b"
    outside = rig.root / "outside"
    outside.mkdir()
    for name, arguments, cwd, want in [
        ("a relative path", {"file_path": "docs/entMatrix.md"}, root, "direct_match"),
        ("an absolute path", {"file_path": str(root / "docs" / "entMatrix.md")}, root, "direct_match"),
        ("a file that does not exist yet", {"file_path": "docs/new/entMatrix.md"}, root, "direct_match"),
        ("a sibling module while the session sits in another module", {"file_path": str(sibling / "entMatrix.go")}, module, "direct_match"),
        ("a relative path into a sibling module", {"file_path": os.path.join("..", "mod-b", "entMatrix.go")}, module, "direct_match"),
        ("a path spelled with another case", {"file_path": str(root / "docs" / "ENTMATRIX.md")}, root, "direct_match"),
        ("outside the project", {"file_path": str(outside / "entMatrix.md")}, root, "no_scanned_fields"),
        ("a relative path that leaves the project", {"file_path": os.path.join("..", "..", "outside", "entMatrix.md")}, module, "no_scanned_fields"),
        ("an unexpanded home directory", {"file_path": "~/entMatrix.md"}, root, "no_scanned_fields"),
        ("an unexpanded environment variable", {"file_path": "$HOME/entMatrix.md"}, root, "no_scanned_fields"),
        ("an instruction file", {"file_path": "entMatrix/AGENTS.md"}, root, "no_scanned_fields"),
        ("an instruction directory written relative", {"file_path": ".codex/entMatrix.json"}, root, "no_scanned_fields"),
        ("a merely similar file name", {"file_path": "docs/AGENTS-entMatrix.md"}, root, "direct_match"),
    ]:
        report.check("Read " + name, probe(rig, "Read", arguments, cwd, record).get("outcome") == want)
    report.check("Read under another project does not reach this project's record",
                 probe(rig, "Read", {"file_path": "entMatrix.md"}, rig.b, record).get("outcome") == "expected_record_ineligible")
    link, inner = module / "escape", module / "alias"
    if make_junction(link, outside) and make_junction(inner, sibling):
        report.check("Read through a junction that leaves the project", probe(rig, "Read", {"file_path": "escape/entMatrix.md"}, module, record).get("outcome") == "no_scanned_fields")
        report.check("Read through a junction that stays inside the project", probe(rig, "Read", {"file_path": "alias/entMatrix.go"}, module, record).get("outcome") == "direct_match")
        os.rmdir(link)
        os.rmdir(inner)
    else:
        report.check("Read through a junction", True, "skipped: the system does not provide directory junctions here")


def reasons(report, rig):
    print("\n== the reason for a skip ==")
    record = CURRENT[0]
    outside = rig.root / "outside"
    for name, arguments, alias, want in [
        ("an outside path", {"file_path": str(outside / "entMatrix.md")}, "path", "path_outside_project"),
        ("an unexpanded home directory", {"file_path": "~/entMatrix.md"}, "path", "path_unresolved"),
        ("an instruction file", {"file_path": "entMatrix/AGENTS.md"}, "path", "instruction_file"),
    ]:
        report.check("Read " + name + " is reported as such", field_status(probe(rig, "Read", arguments, rig.a, record), alias) == want)
    mixed = probe(rig, "Grep", {"pattern": ENTRANCE, "path": str(outside)}, rig.a, record)
    report.check("Grep: an outside search scope leaves the pattern", mixed.get("outcome") == "direct_match" and field_status(mixed, "path") == "path_outside_project", mixed.get("tool_input"))
    scope = probe(rig, "Grep", {"pattern": ENTRANCE, "path": "."}, rig.a, record)
    report.check("Grep: the project root is a valid search scope", scope.get("outcome") == "direct_match" and field_status(scope, "path") == "present", scope.get("tool_input"))
    empty = probe(rig, "Write", {"file_path": "x.sql", "content": ""}, rig.a, record)
    report.check("Write: empty content is reported and the path is still read", field_status(empty, "text") == "empty" and field_status(empty, "path") == "present", empty.get("tool_input"))
    missing = probe(rig, "Write", {"content": ENTRANCE}, rig.a, record)
    report.check("Write: a missing path is reported and the content is still read", field_status(missing, "path") == "missing" and missing.get("outcome") == "direct_match", missing.get("tool_input"))
    big = probe(rig, "Write", {"file_path": "big.sql", "content": ENTRANCE + "x" * 40000}, rig.a, record)
    report.check("Write: oversized content is left out whole and named", field_status(big, "text") == "field_too_large" and big.get("outcome") == "cue_missed", big.get("tool_input"))
    shell = probe(rig, "Bash", {"command": "echo entMatrix"}, rig.a, record)
    report.check("Bash: an unlisted command is reported as not scanned", shell.get("outcome") == "no_scanned_fields" and shell.get("tool_input", {}).get("outcome") == "operation_not_scanned", shell.get("tool_input"))


def carriers(report, rig):
    print("\n== shell, writing tools and MCP tools ==")
    record, ask = CURRENT[0], CURRENT[1]
    for name, command, want in [
        ("a search", "rg -n entMatrix src", "direct_match"),
        ("reading a file", "head -20 entMatrix.txt", "direct_match"),
        ("finding a file", "find . -name '*entMatrix*'", "direct_match"),
        ("git grep", "git grep entMatrix", "direct_match"),
        ("a pipeline", "cd src && cat entMatrix.txt | wc -l", "direct_match"),
        ("an unlisted command", "echo entMatrix", "no_scanned_fields"),
        ("git log --grep, which the list does not name", "git log --grep entMatrix", "no_scanned_fields"),
        ("reading an instruction file", "cat entMatrix/AGENTS.md", "no_scanned_fields"),
    ]:
        report.check("Bash " + name, probe(rig, "Bash", {"command": command}, rig.a, record).get("outcome") == want)
    report.check("Edit stays disabled until the project lists it", probe(rig, "Edit", {"file_path": "entMatrix.sql", "old_string": "a", "new_string": "b"}, rig.a, record).get("outcome") == "tool_disabled")
    report.check("Write is scanned once the project lists it", probe(rig, "Write", {"file_path": "x.sql", "content": "update entMatrix"}, rig.a, record).get("outcome") == "direct_match")
    report.check("a tool nobody listed is reported as unregistered", probe(rig, "TodoWrite", {"x": "entMatrix"}, rig.a, record).get("outcome") == "tool_not_registered")
    # (name, tool, arguments, record index, cue, expected outcome)
    for name, tool, arguments, which, cue, want in [
        ("every element's field", "mcp__asker__ask", {"questions": [{"question": "Is entAsk right?", "header": "h"}, {"question": "second"}]}, 1, "c2", "direct_match"),
        ("the first candidate that is present", "mcp__asker__ask", {"questions": [{"question": "first"}, {"question": "Is entAsk right?"}]}, 1, "c2", "direct_match"),
        ("whichever server sends it", "mcp__other-server__ask", {"questions": [{"question": "entAsk"}]}, 1, "c2", "direct_match"),
        ("a string array", "mcp__tagger__tag", {"tags": ["plain", ENTRANCE]}, 0, "c1", "direct_match"),
        ("an empty string array", "mcp__tagger__tag", {"tags": []}, 0, "c1", "no_scanned_fields"),
        ("one element by index", "mcp__picker__pick", {"questions": [{"question": "first"}, {"question": "Changes touching %s" % ENTRANCE}]}, 0, "c1", "direct_match"),
        ("an entrance outside the chosen index", "mcp__picker__pick", {"questions": [{"question": "Changes touching %s" % ENTRANCE}, {"question": "second"}]}, 0, "c1", "cue_missed"),
        # A cue that names the field must see every element, not only the first.
        ("an entrance in a later element, the field named in the cue", "mcp__asker__ask", {"questions": [{"question": "first"}, {"question": "entPick"}]}, 2, "c3", "direct_match"),
        ("an entrance in a later element under another tool", "mcp__tagger__tag", {"tags": ["plain", "entPick"]}, 2, "c3", "cue_missed"),
    ]:
        report.check("MCP " + name, probe(rig, tool, arguments, rig.a, CURRENT[which], cue).get("outcome") == want)
    report.check("the bare-name rule does not capture a native tool of the same name",
                 probe(rig, "ask", {"questions": [{"question": "entAsk"}]}, rig.a, ask, "c2").get("outcome") == "tool_not_registered")
    shape = probe(rig, "mcp__unknown__ask", {"questions": [{"question": "PRIVATE entAsk"}], "tags": ["PRIVATE"]}, rig.a, ask, "c2")
    shown = [field["path"] for field in shape.get("observed_fields", [])]
    report.check("inspection shows the path inside an array and no value", "questions.*.question" in shown and "tags.*" in shown, shown)
    code, out, err = rig.cli("doctor", *rig.common, "--probe", "-", stdin=json.dumps({"tool_name": "Read", "tool_input": {"file_path": "x"}}))
    report.check("a probe sample without its directory is refused rather than guessed", code != 0 and "cwd" in out + err, out + err)


def hooks(report, rig):
    print("\n== the hook and its trace ==")
    session = "matrix-session"
    outside = rig.root / "outside"
    code, out, err = rig.hook("PreToolUse", session, rig.a, tool_name="Read", tool_input={"file_path": str(outside / "entMatrix.md")}, tool_use_id="r1")
    report.check("a call outside the project returns nothing", code == 0 and out == {}, (code, out, err))
    code, out, err = rig.hook("PreToolUse", session, rig.a, tool_name="Read", tool_input={"file_path": "docs/entMatrix.md"}, tool_use_id="r2")
    report.check("a call inside the project blocks once", out.get("decision") == "block", out)
    rows = rig.doctor("--session", session).get("recent_events", [])
    skipped = [row for row in rows if row.get("result") == "probe_skipped"]
    detail = skipped[0].get("detail", "") if skipped else ""
    report.check("the skipped call left a trace with its reason and without the value",
                 bool(skipped) and "path_outside_project" in detail and "entMatrix" not in detail, [row.get("result") for row in rows])
    code, out, err = rig.hook("PreToolUse", "matrix-session-2", rig.a / "mod-a", tool_name="Read", tool_input={"file_path": str(rig.a / "mod-b" / "entMatrix.go")}, tool_use_id="r3")
    report.check("a module session reading a sibling module triggers", out.get("decision") == "block", out)
    code, out, err = rig.hook("PreToolUse", "matrix-session-3", rig.a, tool_name="Bash", tool_input={"command": "head -20 entMatrix.txt"}, tool_use_id="b1")
    report.check("reading through head triggers", out.get("decision") == "block", out)
    code, out, err = rig.hook("PreToolUse", "matrix-session-4", rig.a, tool_name="mcp__asker__ask", tool_input={"questions": [{"question": "entAsk"}]}, tool_use_id="a1")
    report.check("an MCP call with an array argument triggers", out.get("decision") == "block", out)


def notations(report, rig):
    print("\n== a wrong mapping is explained ==")
    good = read_rules(rig)
    for notation in ("questions[].question", "questions[*].question", "questions..question"):
        document = json.loads(json.dumps(good))
        document["projects"][SCOPE_A].append({"mcp_tool": "fill", "fields": {"text": notation}})
        code, out, err = write_rules(rig, document)
        report.check("setup rejects %s and shows the allowed form" % notation, code != 0 and "questions.*.question" in out + err, out + err)
    write_rules(rig, good)


CURRENT = [None, None, None]


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("binary")
    parser.add_argument("--work-dir", help="place the disposable workspace here instead of the system temp directory")
    args = parser.parse_args()
    report = Report()
    with tempfile.TemporaryDirectory(prefix="memory-matrix-", dir=args.work_dir) as tmp:
        rig = Rig(Path(args.binary).resolve(), tmp)
        try:
            rig.setup(SCOPE_A, rig.a)
            # A cue may only name a tool that a rule of the project registers.
            document = read_rules(rig)
            document["projects"] = {SCOPE_A: [{"mcp_tool": "ask", "fields": {"text": ["missing", "questions.*.question"]}}]}
            write_rules(rig, document)
            ok, first = write(rig, dict(experience("m-1", "c1", [{"op": "token", "entrances": [ENTRANCE]}]), project=str(rig.a)))
            ok2, second = write(rig, dict(experience("m-2", "c2", [
                {"when": {"field": "mcp_tool", "op": "eq", "value": "ask"}, "op": "token", "entrances": [ASK]}]), project=str(rig.a)))
            ok3, third = write(rig, dict(experience("m-3", "c3", [
                {"when": {"field": "mcp_tool", "op": "eq", "value": "ask"}, "field": "text", "op": "contains", "entrances": ["entPick"]}]), project=str(rig.a)))
            CURRENT[:] = [first.get("id") if ok else None, second.get("id") if ok2 else None, third.get("id") if ok3 else None]
            report.check("three records with a cue are stored", all(CURRENT), (first, second, third))
            for step in (seeding, paths, reasons, carriers, hooks, notations):
                try:
                    step(report, rig)
                except Exception as error:
                    report.check(step.__name__ + " completed", False, "%s: %s" % (type(error).__name__, error))
        finally:
            rig.close()
    print("\n%d checks, %d failed" % (report.total, len(report.failed)), flush=True)
    return 1 if report.failed else 0


if __name__ == "__main__":
    sys.exit(main())
