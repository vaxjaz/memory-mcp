#!/usr/bin/env python3
"""User scenarios on an isolated ZCode binding, driven only through public interfaces.

Covers install, write rejections, write-time comparison, recall, revisions, agreement references,
trigger and delivery receipts, maintenance, and the utility commands. No model behaviour is claimed.
"""
import argparse
import hashlib
import json
import re
import subprocess
import sys
import tempfile
from pathlib import Path

from smoke import Host

SCOPE_A, SCOPE_B, SERVER = "project:scenario/a", "project:scenario/b", "scenario"


class Report:
    def __init__(self):
        self.failed = []
        self.total = 0

    def check(self, name, ok, detail=""):
        self.total += 1
        if not ok:
            self.failed.append(name)
        print(("PASS  " if ok else "FAIL  ") + name + ("" if ok else "   <- " + " ".join(str(detail).split())[:420]), flush=True)


class Rig:
    """An isolated library, an isolated ZCode config and two bound projects."""

    def __init__(self, binary, root):
        self.binary, self.root = binary, Path(root)
        self.library, self.config = self.root / "library", self.root / "config.json"
        self.a, self.b = self.root / "projA", self.root / "projB"
        for path in (self.a / "mod-a", self.a / "mod-b", self.b):
            path.mkdir(parents=True)
        (self.a / "Sample.java").write_text("class Sample { void entranceA() {} }\n", encoding="utf-8")
        self.config.write_text("{}", encoding="utf-8")
        self.host, self.events = None, 0
        self.common = ["--host", "zcode", "--config", str(self.config), "--data-dir", str(self.library), "--server-name", SERVER]

    def cli(self, *args, stdin=None):
        done = subprocess.run([str(self.binary), *map(str, args)], input=stdin, capture_output=True, text=True,
                              encoding="utf-8", errors="replace", timeout=90)
        return done.returncode, done.stdout.strip(), done.stderr.strip()

    def setup(self, scope, project, *extra):
        return self.cli("setup", *self.common, "--scope", scope, "--project", project, *extra)

    def doctor(self, *extra, stdin=None):
        code, out, _ = self.cli("doctor", *self.common, *extra, stdin=stdin)
        try:
            return json.loads(out)
        except ValueError:
            return {"outcome": "unparsed", "exit": code}

    def probe(self, event, *extra):
        return self.doctor("--probe", "-", *extra, stdin=json.dumps(event, ensure_ascii=False))

    def mcp(self):
        if self.host is None:
            self.host = Host(str(self.binary), str(self.library))
        return self.host

    def call(self, tool, args):
        """Return (ok, parsed JSON or error text). Protocol-level failures still raise."""
        out = self.mcp().request("tools/call", {"name": tool, "arguments": args})
        text = out["content"][0]["text"]
        if out.get("isError"):
            return False, text
        try:
            return True, json.loads(text)
        except ValueError:
            return True, text

    def raw(self, tool, args):
        return self.mcp().request("tools/call", {"name": tool, "arguments": args})["content"][0]["text"]

    def hook(self, event, session, cwd, **fields):
        events = json.loads(self.config.read_text(encoding="utf-8"))["hooks"]["events"]
        handler = (events.get(event) or events["PreToolUse"])[0]["hooks"][0]
        self.events += 1
        payload = dict(hook_event_name=event, cwd=str(cwd), session_id=session, turn_id="t1", event_id="e%d" % self.events, **fields)
        done = subprocess.run([handler["command"], *handler.get("args", [])], input=json.dumps(payload, ensure_ascii=False),
                              capture_output=True, text=True, encoding="utf-8", errors="replace", timeout=30)
        return done.returncode, (json.loads(done.stdout) if done.stdout.strip() else {}), done.stderr

    def close(self):
        if self.host is not None:
            self.host.close()
            self.host = None


def draft(rig, fact, entrance, request_id, title="scenario fact"):
    return {"scope": SCOPE_A, "project": str(rig.a), "title": title, "request_id": request_id,
            "admission": {"future_decision": "Check the target environment's configuration source before writing it.", "evidence_ref": "ev1"},
            "experience": {"fact": fact, "evidence": [{"kind": "runtime", "reference": "ev1", "observation": "isolated scenario material"}],
                           "cues": [{"cue_id": "c1", "label": "scenario", "evidence_refs": ["ev1"],
                                     "matches": [{"op": "token", "entrances": [entrance]}]}]}}


def write(rig, args, on_compare="create"):
    """Submit a draft; a comparison request is resolved with the template by resending the SAME draft."""
    ok, out = rig.call("memory_experience_record", args)
    if ok and isinstance(out, dict) and out.get("status") == "needs_comparison" and on_compare:
        args["resolution"] = dict(out["resolution_template"], action=on_compare, difference="an independently standing scenario fact")
        ok, out = rig.call("memory_experience_record", args)
    return ok, out


def compact(value, size=300):
    return " ".join(str(value).split())[:size]


def section(name):
    print("\n== %s ==" % name, flush=True)


def install(report, rig):
    section("install")
    code, out, err = rig.setup(SCOPE_A, rig.a, "--dry-run")
    report.check("setup --dry-run writes nothing", code == 0 and rig.config.read_text() == "{}" and not (rig.library / "memory.db").exists(), out + err)
    code, out, err = rig.setup(SCOPE_A, rig.a)
    first = json.loads(out) if out.startswith("{") else {}
    report.check("first setup binds the project, enables wakeup and installs hooks",
                 code == 0 and first.get("wakeup_enabled") and first.get("experience_hooks_installed"), out + err)
    code, out, err = rig.setup(SCOPE_B, rig.b)
    bindings = rig.doctor("--scope", SCOPE_B, "--project", rig.b).get("bindings", [])
    report.check("a second project joins the same library", code == 0 and {(b["scope"]) for b in bindings} == {SCOPE_A, SCOPE_B}, bindings)
    rig.setup(SCOPE_A, rig.a, "--disable-wakeup")
    a, b = rig.doctor("--scope", SCOPE_A, "--project", rig.a), rig.doctor("--scope", SCOPE_B, "--project", rig.b)
    report.check("disabling wakeup for one project leaves the other and the hooks intact",
                 a.get("wakeup_enabled") is False and b.get("wakeup_enabled") is True and a.get("experience_hooks_installed") is True, (a.get("wakeup_enabled"), b.get("wakeup_enabled")))
    rig.setup(SCOPE_A, rig.a, "--enable-wakeup")
    # A partial upgrade moves the service to another program and leaves the hooks behind.
    saved = rig.config.read_text(encoding="utf-8")
    moved = json.loads(saved)
    moved["mcp"]["servers"][SERVER]["command"] = str(rig.root / "elsewhere" / "memory-mcp.exe")
    rig.config.write_text(json.dumps(moved), encoding="utf-8")
    split = rig.doctor().get("split_executables", [])
    report.check("doctor reports hooks that still launch another program",
                 rig.doctor().get("configuration_consistent") is False and bool(split) and all(item["hook_command"] == str(rig.binary) for item in split), split[:1])
    rig.config.write_text(saved, encoding="utf-8")
    report.check("doctor is satisfied again once service and hooks name the same program", rig.doctor().get("configuration_consistent") is True)
    for label, cwd, want in (("project A subdirectory", rig.a / "mod-a", SCOPE_A), ("project B", rig.b, SCOPE_B)):
        report.check("a tool call under the %s resolves to its project" % label,
                     rig.probe({"tool_name": "Read", "tool_input": {"file_path": "Sample.java"}, "cwd": str(cwd)}).get("scope") == want)
    report.check("a tool call under an unbound directory is not resolved",
                 rig.probe({"tool_name": "Read", "tool_input": {"file_path": "Sample.java"}, "cwd": str(rig.root)}).get("outcome") == "project_binding_mismatch")


def writes(report, rig):
    section("write")
    ok, saved = write(rig, draft(rig, "Scenario: settlement code beta is configured per environment.", "entAlpha", "w-1"))
    report.check("a fact is stored", ok and saved.get("id"), saved)
    same = draft(rig, "Scenario: settlement code beta is configured per environment.", "entAlpha", "w-1", "changed title")
    ok, out = rig.call("memory_experience_record", same)
    report.check("retrying a request ID with a changed payload conflicts", not ok and "request_id conflict" in out, out)
    same = draft(rig, "Scenario: settlement code beta is configured per environment.", "entAlpha", "w-1")
    ok, again = rig.call("memory_experience_record", same)
    report.check("retrying a request ID with the same payload replays the stored record", ok and again.get("id") == saved.get("id"), again)
    ok, counter = write(rig, {"scope": SCOPE_A, "project": str(rig.a), "title": "scenario counterexample", "request_id": "w-ce",
                              "admission": {"future_decision": "Check where the acceptance parameters come from before changing them.", "evidence_ref": "ev2"},
                              "experience": {"pitfall": "Assuming the acceptance parameters live in the code.", "counterexample": "They are delivered by the configuration service.",
                                             "evidence": [{"kind": "runtime", "reference": "ev2", "observation": "isolated scenario material"}],
                                             "cues": [{"cue_id": "c2", "label": "counterexample", "evidence_refs": ["ev2"], "matches": [{"op": "token", "entrances": ["entTrigger"]}]}]}})
    report.check("a counterexample is stored", ok and counter.get("id"), counter)

    def mutate(name, change, *expected):
        args = draft(rig, "Scenario: settlement code beta is configured per environment.", "entAlpha", "r-" + name.replace(" ", "-"))
        change(args)
        ok, out = write(rig, args, on_compare=None)
        text = out if isinstance(out, str) else json.dumps(out, ensure_ascii=False)
        report.check("rejected: " + name, not ok and all(part in text for part in expected), text)
    # Each expected fragment is what makes the message actionable: the field,
    # and for a binding problem the directory, the scope and the way out.
    mutate("missing admission", lambda d: d.pop("admission"), "admission is required")
    mutate("admission citing unknown evidence", lambda d: d["admission"].__setitem__("evidence_ref", "nope"), "admission.evidence_ref")
    mutate("blank future decision", lambda d: d["admission"].__setitem__("future_decision", " "), "admission.future_decision")
    mutate("fact plus a separate qualifier column", lambda d: d["experience"].__setitem__("impact", "x"), "experience.impact")
    mutate("fact plus pitfall", lambda d: d["experience"].__setitem__("pitfall", "x"), "mutually exclusive")
    mutate("neither body form", lambda d: d["experience"].pop("fact"), "requires one body form")
    mutate("authored cue text", lambda d: d["experience"]["cues"][0].__setitem__("text", "authored"), "experience.cues[0].text")
    mutate("upper-case cue ID", lambda d: d["experience"]["cues"][0].__setitem__("cue_id", "BadID"), "cue_id")
    mutate("49-character cue ID", lambda d: d["experience"]["cues"][0].__setitem__("cue_id", "a" * 49), "cue_id")
    mutate("25-character label", lambda d: d["experience"]["cues"][0].__setitem__("label", "字" * 25), "label")
    mutate("cue citing unknown evidence", lambda d: d["experience"]["cues"][0].__setitem__("evidence_refs", ["other"]), "evidence_refs")
    mutate("thirteen entrances", lambda d: d["experience"]["cues"][0]["matches"][0].__setitem__("entrances", ["w%d" % i for i in range(13)]), "matches[0]")
    mutate("no entrance", lambda d: d["experience"]["cues"][0]["matches"][0].__setitem__("entrances", []), "matches[0]")
    mutate("241-character fact", lambda d: d["experience"].__setitem__("fact", "长" * 241), "experience.fact")
    mutate("four evidence items", lambda d: d["experience"].__setitem__("evidence", [{"kind": "runtime", "reference": "e%d" % i, "observation": "o"} for i in range(4)]), "admission.evidence_ref")
    mutate("241-character reference", lambda d: (d["experience"]["evidence"][0].__setitem__("reference", "r" * 241), d["admission"].__setitem__("evidence_ref", "r" * 241), d["experience"]["cues"][0].__setitem__("evidence_refs", ["r" * 241])), "reference")
    mutate("181-character observation", lambda d: d["experience"]["evidence"][0].__setitem__("observation", "o" * 181), "observation")
    # This draft shares an entrance with the stored record: it must still be
    # refused here, not sent through a comparison first.
    mutate("blank title", lambda d: d.__setitem__("title", ""), "title: needs 1-200 Unicode characters")
    mutate("201-character title", lambda d: d.__setitem__("title", "题" * 201), "title: needs 1-200 Unicode characters, got 201")
    mutate("title that is a number", lambda d: d.__setitem__("title", 5), "title must be string")
    mutate("project left out", lambda d: d.pop("project"), "project is required")
    mutate("unbound project", lambda d: d.__setitem__("project", str(rig.root)), "binding mismatch", "is not bound to any scope", "memory-mcp setup --host")
    mutate("scope disagreeing with the bound project", lambda d: d.__setitem__("scope", "project:scenario/other"), "binding mismatch", "is bound to " + SCOPE_A, "send scope " + SCOPE_A)

    def code_evidence(path, sha=None):
        args = draft(rig, "Scenario: settlement code beta is configured per environment.", "entAlpha", "r-code-" + path.replace("\\", "/").replace(".", "-") + str(bool(sha)))
        item = {"kind": "code", "reference": "code1", "observation": "the sample method", "path": path, "symbol": "entranceA"}
        if sha:
            item["sha256"] = sha
        args["experience"]["evidence"] = [item]
        args["admission"]["evidence_ref"] = "code1"
        args["experience"]["cues"][0]["evidence_refs"] = ["code1"]
        return args
    digest = hashlib.sha256((rig.a / "Sample.java").read_bytes()).hexdigest()
    for name, args, expected in (("code evidence outside the project", code_evidence(str(rig.b / "Sample.java")), "project-relative"),
                                ("code evidence for a missing file", code_evidence("Missing.java"), 'experience.evidence[0].path: path "Missing.java": no such file in the bound project'),
                                ("code evidence whose content changed", code_evidence("Sample.java", "0" * 64), "code evidence changed")):
        ok, out = write(rig, args, on_compare=None)
        report.check("rejected: " + name, not ok and expected in str(out), out)
    ok, out = write(rig, code_evidence("Sample.java", digest))
    report.check("accepted: code evidence for an existing, unchanged file", ok and out.get("id"), out)


def comparison(report, rig):
    section("write-time comparison")
    base = "Scenario: settlement code beta is configured per environment."
    ok, first = write(rig, draft(rig, "Comparison: the return channel is chosen per tenant.", "entCompare", "cmp-1"))
    forged = draft(rig, "Comparison: the return channel is chosen per tenant.", "entCompare", "cmp-forged")
    forged["resolution"] = {"action": "reuse", "target_id": first.get("id"), "difference": "x", "evidence_ref": "ev1", "comparison_token": "forged"}
    ok, out = rig.call("memory_experience_record", forged)
    report.check("a forged comparison token on a first submission writes nothing and asks for the judgment",
                 ok and isinstance(out, dict) and out.get("status") == "needs_comparison", out)
    similar = draft(rig, "Comparison: the return channel is chosen per tenant (restated).", "entCompare", "cmp-2")
    ok, asked = write(rig, similar, on_compare=None)
    candidates = asked.get("candidates", []) if isinstance(asked, dict) else []
    # Every scenario record cites one evidence reference, so the shared evidence also asks for the judgment.
    shared = next((c for c in candidates if c["id"] == first.get("id")), {})
    report.check("a draft sharing an entrance or evidence is compared before it is stored",
                 ok and asked.get("status") == "needs_comparison" and {"entrance", "evidence_ref"} <= set(shared.get("matched_on", [])), asked)
    template = asked.get("resolution_template", {}) if isinstance(asked, dict) else {}
    report.check("the comparison names each candidate's current fact and a prefilled resolution template",
                 candidates and all(c["experience"].get("fact") or (c["experience"].get("pitfall") and c["experience"].get("counterexample")) for c in candidates) and template.get("comparison_token")
                 and template.get("target_id") in {c["id"] for c in candidates} and template.get("evidence_ref") == "ev1", asked)
    similar["resolution"] = dict(template, action="reuse", target_id=first["id"], difference="a restatement of the same fact without new evidence")
    ok, reused = rig.call("memory_experience_record", similar)
    report.check("reuse returns the existing record and stores nothing new", ok and reused.get("id") == first.get("id") and reused.get("reused") is True, reused)
    deferred = draft(rig, "Comparison: the return channel is chosen per tenant (restated again).", "entCompare", "cmp-3")
    ok, asked = write(rig, deferred, on_compare=None)
    before = rig.call("memory_search", {"scope": SCOPE_A, "query": ""})[1]["results"]
    deferred["resolution"] = dict(asked["resolution_template"], action="defer", difference="the distinguishing evidence is missing")
    ok, out = rig.call("memory_experience_record", deferred)
    after = rig.call("memory_search", {"scope": SCOPE_A, "query": ""})[1]["results"]
    report.check("defer writes nothing", ok and len(after) == len(before), out)
    # Identity, not wording, asks for the judgment: another source and another entrance,
    # and a body that shares a few words with what is already stored.
    apart = dict(draft(rig, "Scenario: settlement code beta is configured per environment.", "entApart", "cmp-apart", "a note on the same subject"), title="a note on the same subject")
    apart["admission"] = dict(apart["admission"], evidence_ref="ev-apart")
    apart["experience"] = dict(apart["experience"], fact="Scenario: settlement code beta is read from a per-environment properties file and cached by the configuration service.",
                               evidence=[{"kind": "runtime", "reference": "ev-apart", "observation": "isolated scenario material"}])
    apart["experience"]["cues"] = [dict(apart["experience"]["cues"][0], evidence_refs=["ev-apart"])]
    ok, out = write(rig, apart, on_compare=None)
    report.check("a draft that only shares words with a stored record is written without a judgment",
                 ok and out.get("status") != "needs_comparison" and out.get("id"), out)
    restated = dict(apart, request_id="cmp-restated", title="the same note in other words")
    restated["admission"] = dict(apart["admission"], evidence_ref="ev-restated")
    restated["experience"] = dict(apart["experience"], fact="Scenario: settlement code beta is read from a per-environment properties file and cached by the configuration service in each region.",
                                  evidence=[{"kind": "runtime", "reference": "ev-restated", "observation": "isolated scenario material"}])
    restated["experience"]["cues"] = [dict(apart["experience"]["cues"][0], cue_id="c9", evidence_refs=["ev-restated"],
                                           matches=[{"op": "token", "entrances": ["entRestated"]}])]
    ok, asked = write(rig, restated, on_compare=None)
    shared = [c for c in asked.get("candidates", []) if c["id"] == out.get("id")] if isinstance(asked, dict) else []
    report.check("the same claim with other evidence and another entrance is compared because the bodies overlap by half",
                 ok and asked.get("status") == "needs_comparison" and shared and shared[0]["matched_on"] == ["body_overlap"], asked)

    created = draft(rig, "Comparison: the return channel is chosen per tenant (a second independent fact).", "entCompare", "cmp-4")
    ok, asked = write(rig, created, on_compare=None)
    created["resolution"] = dict(asked["resolution_template"], action="create", difference="an independently standing fact with its own decision")
    ok, out = rig.call("memory_experience_record", created)
    report.check("create stores a second root", ok and out.get("id") and out.get("id") != first.get("id") and out.get("root_id") == out.get("id"), out)


def unreachable(report, rig):
    section("a cue that could never fire is refused")
    base = draft(rig, "Unreachable: the acceptance account is issued by its owner.", "entOwner", "ur-1")
    refused_titles = set()
    for name, matches, expected in [
        ("a bare MCP name written as a native tool", [{"when": {"field": "tool", "op": "eq", "value": "login"}, "op": "token", "entrances": ["entOwner"]}], 'registers a tool named "login"'),
        ("a field no rule exposes", [{"field": "nope", "op": "exists", "op": "token", "entrances": ["entOwner"]}], 'field named "nope"'),
        ("a tool nobody registered", [{"when": {"field": "tool", "op": "eq", "value": "TodoWrite"}, "op": "token", "entrances": ["entOwner"]}], "TodoWrite"),
    ]:
        args = json.loads(json.dumps(base))
        args["request_id"], args["title"] = "ur-" + name, name
        refused_titles.add(name)
        args["experience"]["cues"][0]["matches"] = matches
        ok, out = rig.call("memory_experience_record", args)
        report.check("refused: " + name, not ok and expected in out and "no write performed" in out and "experience.cues[0]" in out, out)
    ok, page = rig.call("memory_search", {"scope": SCOPE_A, "query": "", "limit": 30})
    titles = {result["title"] for result in page.get("results", [])}
    report.check("no refused cue reached the library", ok and not titles & refused_titles, sorted(titles & refused_titles))
    # Registering the tool by its bare name makes the cue acceptable.
    path = rig.library / "tool-probes.json"
    document = json.loads(path.read_text(encoding="utf-8"))
    document.setdefault("projects", {}).setdefault(SCOPE_A, []).append({"mcp_tool": "login", "fields": {"text": "username"}})
    path.write_text(json.dumps(document), encoding="utf-8")
    rig.setup(SCOPE_A, rig.a)
    args = json.loads(json.dumps(base))
    args["experience"]["cues"][0]["matches"] = [{"when": {"field": "mcp_tool", "op": "eq", "value": "login"}, "op": "token", "entrances": ["entOwner"]}]
    native = json.loads(json.dumps(base))
    native["experience"]["cues"][0]["matches"] = [{"when": {"field": "tool", "op": "eq", "value": "login"}, "op": "token", "entrances": ["entOwner"]}]
    ok, out = rig.call("memory_experience_record", native)
    report.check("the refusal for the native spelling now points at the bare name",
                 not ok and '"field":"mcp_tool","op":"eq","value":"login"' in out, out)
    ok, out = write(rig, args)
    report.check("accepted once the MCP tool is registered", ok and out.get("id"), out)
    report.check("doctor finds nothing in this library that could never fire", rig.doctor("--scope", SCOPE_A, "--project", rig.a).get("unreachable_cues") == [], rig.doctor("--scope", SCOPE_A, "--project", rig.a).get("unreachable_cues"))


def recall(report, rig):
    section("recall")
    lookup = {"scope": SCOPE_A, "project": str(rig.a), "task": "scenario lookup", "questions": "the return channel"}
    ok, out = rig.call("memory_experience_lookup", dict(lookup, queries=[{"terms": ["return", "channel"]}]))
    report.check("lookup with match=all finds the record containing every term", ok and out.get("items"), out)
    ok, out = rig.call("memory_experience_lookup", dict(lookup, queries=[{"terms": ["tenant", "zzz_absent"], "match": "any"}]))
    report.check("lookup with match=any finds the record containing one term", ok and out.get("items"), out)
    ok, out = rig.call("memory_experience_lookup", dict(lookup, queries=[{"terms": ["tenant", "表"]}]))
    report.check("an unsearchable single character is dropped and reported, not rejected", ok and out.get("items") and out.get("dropped_terms") == ["表"], out)
    ok, out = rig.call("memory_experience_lookup", dict(lookup, queries=[{"terms": ["tenant"]}], challenge_id="0" * 32))
    report.check("an invented challenge ID is rejected", not ok and "unknown challenge" in out, out)
    ok, out = rig.call("memory_experience_lookup", dict(lookup, scope=SCOPE_B, queries=[{"terms": ["tenant"]}]))
    report.check("a scope that disagrees with the project is rejected, naming both", not ok and "binding mismatch" in out and "is bound to " + SCOPE_A in out, out)
    bare = dict(lookup, queries=[{"terms": ["tenant"]}])
    del bare["project"]
    ok, out = rig.call("memory_experience_lookup", bare)
    report.check("lookup without the project directory is rejected with what to send", not ok and "project is required" in out, out)
    ok, out = rig.call("memory_experience_lookup", dict(lookup, queries=[{"terms": ["tenant"]}], limit=4))
    report.check("limit above three is rejected", not ok and "limit" in out, out)
    ok, page = rig.call("memory_search", {"scope": SCOPE_A, "query": ""})
    report.check("search with an empty query lists the current records", ok and len(page["results"]) >= 5, page)
    ok, cases = rig.call("memory_search", {"scope": SCOPE_A, "query": "", "kind": "case"})
    ok, records = rig.call("memory_search", {"scope": SCOPE_A, "query": "", "kind": "record"})
    report.check("experiences are stored as kind case", ok and {r["id"] for r in cases["results"]} == {r["id"] for r in page["results"]}, (len(cases.get("results", [])), len(page["results"])))
    ok, out = rig.call("memory_search", {"scope": SCOPE_A, "query": "", "limit": 31})
    report.check("search limit above thirty is rejected", not ok and "limit" in out, out)
    ok, out = rig.call("memory_search", {"scope": SCOPE_A, "query": "", "kind": "experience"})
    report.check("an unknown kind is rejected with the accepted values and what each covers", not ok and "kind must be case, record or hypothesis" in out and "case covers experiences" in out, out)
    ok, out = rig.call("memory_experience_record", dict(draft(rig, "Global scenario fact.", "entGlobal", "w-global"), scope="global"))
    report.check("an experience cannot be written to the global scope", not ok and "scope must be project:<identifier>" in out, out)


def revisions(report, rig):
    section("revision")
    ok, first = write(rig, draft(rig, "Revision: the export job runs nightly.", "entRevise", "rev-1"))
    alias = draft(rig, "Revision: the export job runs nightly.", "entRevise", "rev-2")
    alias["previous_id"] = first["id"]
    alias["experience"]["cues"][0]["matches"].append({"op": "token", "entrances": ["entReviseAlias"]})
    ok, out = write(rig, alias)
    report.check("adding only an entrance keeps the same version", ok and out.get("id") == first["id"], out)
    material = draft(rig, "Revision: the export job runs hourly since the last change.", "entRevise", "rev-3")
    material["previous_id"] = first["id"]
    ok, out = write(rig, material)
    ok, read = rig.call("memory_read", {"id": out.get("id")})
    entry = read.get("entry", {})
    report.check("changing the body creates a new version that names its predecessor",
                 ok and out.get("id") != first["id"] and entry.get("previous_id") == first["id"] and entry.get("current") is True, read)
    ok, old = rig.call("memory_read", {"id": first["id"]})
    report.check("the superseded version stays readable and is marked not current", ok and old["entry"]["current"] is False, old)


def agreement(report, rig):
    section("agreement reference")
    args = {"scope": SCOPE_A, "project": str(rig.a), "title": "scenario agreement", "request_id": "agr-1",
            "agreement": {"requirement": "Acceptance uses a read-only account.", "applicability": "acceptance environment only", "boundary": "production unconfirmed",
                          "authority": "the acceptance owner", "confirmation_reference": "minutes 2026-11-02",
                          "evidence": [{"kind": "business", "reference": "agr-ev", "observation": "the minutes"}],
                          "cues": [{"cue_id": "agr", "label": "read-only account", "text": "Acceptance uses a read-only account.", "evidence_refs": ["agr-ev"],
                                    "matches": [{"op": "token", "entrances": ["entAgreement"]}]}]}}
    ok, out = rig.call("memory_reference_record", args)
    ok, found = rig.call("memory_search", {"scope": SCOPE_A, "query": "read-only account", "kind": "record"})
    report.check("an agreement is stored and found by a literal search", ok and out.get("id") and found["results"], (out, found))
    ok, out = rig.call("memory_experience_lookup", {"scope": SCOPE_A, "project": str(rig.a), "task": "scenario lookup", "questions": "the acceptance account", "queries": [{"terms": ["read-only"]}]})
    report.check("an agreement is returned by lookup as an agreement reference", ok and any(i.get("source_kind") == "agreement_reference" for i in out.get("items", [])), out)


def trigger(report, rig):
    section("trigger and delivery")
    session = "scenario-session"
    code, out, _ = rig.hook("SessionStart", session, rig.a)
    report.check("SessionStart returns context", code == 0 and out.get("hookSpecificOutput", {}).get("additionalContext"), out)
    code, out, _ = rig.hook("PreToolUse", session, rig.a, tool_name="Read", tool_input={"file_path": "entTrigger.txt"}, tool_use_id="read-1")
    reason = out.get("reason", "")
    challenge = (re.search(r'challenge_id="([a-f0-9]{32})"', reason) or [None, None])[1]
    report.check("reading a path with an undelivered entrance blocks once and names the lookup call",
                 out.get("decision") == "block" and challenge and "memory_experience_lookup" in reason, out)
    arguments = {"scope": SCOPE_A, "project": str(rig.a), "task": "the triggered lookup", "questions": "entTrigger", "queries": [{"terms": ["entTrigger"]}], "challenge_id": challenge}
    name, call_id = "mcp__%s__memory_experience_lookup" % SERVER, "lookup-1"
    rig.hook("PreToolUse", session, rig.a, tool_name=name, tool_use_id=call_id, tool_input=arguments)
    text = rig.raw("memory_experience_lookup", arguments)
    result = json.loads(text)
    report.check("the challenged lookup returns the record", result.get("items") and result.get("challenge_outcome") == "response_generated", result)
    pending = rig.doctor("--challenge", challenge).get("stage")
    rig.hook("PostToolUse", session, rig.a, tool_name=name, tool_use_id=call_id, tool_input=arguments,
             tool_response={"content": [{"type": "text", "text": text}]})
    report.check("the native result credits delivery", pending == "response_pending_observation" and rig.doctor("--challenge", challenge).get("stage") == "delivered", (pending, rig.doctor("--challenge", challenge)))
    code, out, _ = rig.hook("PreToolUse", session, rig.a, tool_name="Read", tool_input={"file_path": "entTrigger.txt"}, tool_use_id="read-2")
    report.check("the same entrance is silent after the body was delivered", code == 0 and out == {}, out)
    code, out, _ = rig.hook("PreToolUse", "other-session", rig.root, tool_name="Read", tool_input={"file_path": "entTrigger.txt"}, tool_use_id="read-3")
    report.check("a tool call under an unbound directory returns nothing", code == 0 and out == {}, out)
    rig.setup(SCOPE_A, rig.a, "--disable-wakeup")
    code, out, _ = rig.hook("PreToolUse", "fresh-session", rig.a, tool_name="Read", tool_input={"file_path": "entTrigger.txt"}, tool_use_id="read-4")
    report.check("a project with wakeup disabled returns nothing", code == 0 and out == {}, out)
    rig.setup(SCOPE_A, rig.a, "--enable-wakeup")
    code, out, _ = rig.hook("PreToolUse", "fresh-session", rig.a, tool_name="Read", tool_input={"file_path": "entTrigger.txt"}, tool_use_id="read-5")
    report.check("reenabling wakeup restores the trigger", out.get("decision") == "block", out)


def schema_parameters(schema):
    """Every parameter of a tool schema: path -> (property, required).

    A path is written as the reference writes it: name, a.b, a[].b. Nullable
    parameters carry their shape under anyOf/oneOf.
    """
    found = {}

    def walk(node, prefix):
        for key in ("anyOf", "oneOf"):
            for option in node.get(key) or []:
                if isinstance(option, dict):
                    walk(option, prefix)
        for name, prop in (node.get("properties") or {}).items():
            found[prefix + name] = (prop, name in (node.get("required") or []))
            walk(prop, prefix + name + ".")
        if isinstance(node.get("items"), dict):
            walk(node["items"], prefix[:-1] + "[].")

    walk(schema, "")
    return found


def selfDescription(report, rig):
    section("every tool parameter says what it is")
    for tool in rig.mcp().request("tools/list", {})["tools"]:
        missing = [path for path, (prop, _) in schema_parameters(tool["inputSchema"]).items() if not prop.get("description")]
        report.check("every parameter of %s is described" % tool["name"], not missing, missing)


DOCS = Path(__file__).resolve().parent.parent / "docs"
PATH_TOKEN = re.compile(r"`([a-z_][a-z0-9_]*(?:\[\])?(?:\.[a-z_][a-z0-9_]*(?:\[\])?)*)`")
FLAG_TOKEN = re.compile(r"--([a-z][a-z-]*)")


def cells_of(line):
    """The cells of one table row; an escaped pipe inside a cell is not a border."""
    return [c.strip() for c in line.replace("\\|", "\uff5c").split("|")[1:-1]]


def tool_sections(text):
    """Signature and body of each tool's section in the reference."""
    parts = re.split(r"^### `(memory_\w+)\((.*)\)`[ \t]*$", text, flags=re.M)
    return {parts[i]: (parts[i + 1], re.split(r"^#{2,3} ", parts[i + 2], flags=re.M)[0]) for i in range(1, len(parts), 3)}


def documented_parameters(signature, text):
    """What one tool's reference lists: path -> whether it is called required.

    Top-level parameters take their mark from the signature's star, nested ones
    from the word 必填 in the type column of their table row.
    """
    paths = {}
    for item in signature.split(","):
        item = item.strip()
        if item:
            paths[item.rstrip("*?")] = item.endswith("*")
    for cells in parameter_rows(text):
        for name in PATH_TOKEN.findall(cells[0]):
            paths[name] = paths.get(name, False) or "必填" in cells[1]
    return paths


def parameter_rows(text):
    return [cells for cells in (cells_of(line) for line in text.splitlines())
            if len(cells) >= 3 and cells[0].startswith("`") and PATH_TOKEN.search(cells[0])]


def help_parameters(text):
    """The flags a command's help names in its usage line and its parameter list; prose is not a parameter."""
    formal = []
    block = False
    for line in text.splitlines():
        if line.strip() in ("Usage:", "Parameters:"):
            block = True
        elif not line.strip():
            block = False
        elif block:
            formal.append(line)
    return set(FLAG_TOKEN.findall("\n".join(formal)))


def reference(report, rig):
    """docs/API.md against the program itself: tool schemas and command help."""
    section("reference documents")
    api = (DOCS / "API.md").read_text(encoding="utf-8")
    sections = tool_sections(api)
    tools = rig.mcp().request("tools/list", {})["tools"]
    report.check("API.md has one section per tool and nothing else", {t["name"] for t in tools} == set(sections),
                 sorted({t["name"] for t in tools} ^ set(sections)))
    for tool in tools:
        schema = {path: required for path, (_, required) in schema_parameters(tool["inputSchema"]).items()}
        signature, text = sections.get(tool["name"], ("", ""))
        doc = documented_parameters(signature, text)
        wanted, marked = {p for p in schema if schema[p]}, {p for p in doc if doc[p]}
        report.check("API.md lists exactly the parameters of %s" % tool["name"], set(schema) == set(doc),
                     "undocumented: %s; not in the program: %s" % (sorted(set(schema) - set(doc)), sorted(set(doc) - set(schema))))
        report.check("API.md marks as required exactly what %s requires" % tool["name"], wanted == marked,
                     "required but unmarked: %s; marked but optional: %s" % (sorted(wanted - marked), sorted(marked - wanted)))
        thin = [cells[0] for cells in parameter_rows(text) if not cells[1] or not cells[2]]
        report.check("every documented parameter of %s has a type and an explanation" % tool["name"], not thin, thin)

    code, out, _ = rig.cli("help")
    listed = set(re.findall(r"^  ([a-z-]+)\s{2,}", out, re.M))
    table = api.split("## 命令行\n", 1)[1].split("\n## ", 1)[0]
    rows = {}
    for cells in (cells_of(line) for line in table.splitlines()):
        if len(cells) == 3 and re.fullmatch(r"(?:`[a-z-]+`\u3001?)+", cells[0]):
            for name in re.findall(r"`([a-z-]+)`", cells[0]):
                rows[name] = set(FLAG_TOKEN.findall(cells[1]))
    report.check("API.md lists every command of the program and nothing else", listed == set(rows), sorted(listed ^ set(rows)))
    for name in sorted(listed & set(rows)):
        code, out, _ = rig.cli(name, "--help")
        shown = help_parameters(out)
        report.check("--help of %s lists exactly the parameters API.md documents" % name, code == 0 and shown == rows[name],
                     "only in --help: %s; only in API.md: %s" % (sorted(shown - rows[name]), sorted(rows[name] - shown)))
    code, out, err = rig.cli("setup", "--session", "x")
    report.check("a parameter of another command is rejected instead of ignored", code != 0 and "not defined: -session" in out + err, out + err)
    boot = rig.call("memory_bootstrap", {})[1]
    reported = re.search(r"\u96c6\u6210\u534f\u8bae (\d+)", api).group(1)
    report.check("API.md states the integration protocol the program reports", boot.get("integration_protocol") == reported, (boot.get("integration_protocol"), reported))


def maintenance(report, rig):
    section("maintenance")
    ok, page = rig.call("memory_search", {"scope": SCOPE_A, "query": "", "kind": "case"})
    target = page["results"][-1]["id"]
    # A wrong step is refused in words that say what to do instead, never as an internal error.
    for name, args, expected in [
        ("apply without a plan ID", {"action": "apply", "scope": SCOPE_A, "approval_source_id": "x", "approval_quote": "x"}, "a plan ID is required"),
        ("confirm without a preview", {"action": "confirm", "scope": SCOPE_A, "plan_id": "missing", "purpose": "apply", "approval_quote": "x", "request_id": "c0"}, "no cleanup plan has the ID"),
        ("read_plan for an unknown plan", {"action": "read_plan", "scope": SCOPE_A, "plan_id": "missing"}, "no cleanup plan has the ID"),
    ]:
        ok, out = rig.call("memory_cleanup", args)
        report.check("refused with the reason: " + name, not ok and expected in out and "sql:" not in out, out)
    ok, preview = rig.call("memory_cleanup", {"action": "preview", "scope": SCOPE_A, "operations": [
        dict(entry_id=target, action="distill", summary="The isolated scenario conclusion", reason="exercise explicit maintenance", evidence_ids=[target])]})
    plan = preview.get("plan", {})
    ok, out = rig.call("memory_cleanup", {"action": "apply", "scope": SCOPE_A, "plan_id": plan.get("id"), "approval_source_id": "missing", "approval_quote": "x"})
    report.check("apply with a plan but no approval is refused and points at confirm", not ok and "must be the value returned by confirm" in out, out)
    ok, out = rig.call("memory_cleanup", {"action": "undo", "scope": SCOPE_A, "plan_id": plan.get("id"), "approval_source_id": "x", "approval_quote": "x"})
    report.check("undo before apply is refused and names both states", not ok and 'needs a plan in state "applied"' in out and 'in state "preview"' in out, out)
    ok, approval = rig.call("memory_cleanup", {"action": "confirm", "scope": SCOPE_A, "plan_id": plan.get("id"), "purpose": "apply",
                                               "approval_quote": "Apply this exact plan", "request_id": "c1"})
    ok, applied = rig.call("memory_cleanup", {"action": "apply", "scope": SCOPE_A, "plan_id": plan.get("id"),
                                              "approval_source_id": approval.get("approval_source_id"), "approval_quote": "Apply this exact plan"})
    report.check("preview, confirm, then apply", ok and applied.get("plan", {}).get("status") == "applied", applied or approval)
    ok, out = rig.call("memory_cleanup", {"action": "undo", "scope": SCOPE_A, "plan_id": plan.get("id"),
                                          "approval_source_id": approval.get("approval_source_id"), "approval_quote": "Apply this exact plan"})
    report.check("undo needs its own approval", not ok, out)
    ok, undo = rig.call("memory_cleanup", {"action": "confirm", "scope": SCOPE_A, "plan_id": plan.get("id"), "purpose": "undo",
                                           "approval_quote": "Undo this exact plan", "request_id": "c2"})
    ok, undone = rig.call("memory_cleanup", {"action": "undo", "scope": SCOPE_A, "plan_id": plan.get("id"),
                                             "approval_source_id": undo.get("approval_source_id"), "approval_quote": "Undo this exact plan"})
    report.check("confirm undo, then undo restores", ok and undone.get("plan", {}).get("status") == "undone", undone)

    def policy():
        return rig.call("memory_cleanup", {"action": "inspect", "scope": SCOPE_A})[1].get("inventory", {}).get("maintenance", {})
    ok, out = rig.call("memory_cleanup", {"action": "configure", "scope": SCOPE_A, "policy": {
        "scope": SCOPE_A, "interval": 5, "enabled": True, "repeating": True, "source_id": "remind me every 5"}})
    report.check("configure without a confirmed user instruction is refused and names the two steps",
                 not ok and "not an approval recorded in this library" in out and "purpose is configure" in out, out)
    ok, intent = rig.call("memory_cleanup", {"action": "confirm", "scope": SCOPE_A, "purpose": "configure",
                                             "approval_quote": "Remind me every 5 stored experiences", "request_id": "cfg-1"})
    ok, out = rig.call("memory_cleanup", {"action": "configure", "scope": SCOPE_A, "policy": {
        "scope": SCOPE_A, "interval": 5, "enabled": True, "repeating": True, "source_id": intent.get("approval_source_id")}})
    report.check("confirm the configuration intent, then configure the interval", ok and policy().get("interval") == 5, out or policy())
    ok, out = rig.call("memory_cleanup", {"action": "defer", "scope": SCOPE_A, "defer_changes": 20})
    report.check("defer moves the next reminder", ok and policy().get("next_at") == policy().get("changes", 0) + 20, policy())
    ok, out = rig.call("memory_cleanup", {"action": "acknowledge", "scope": SCOPE_A})
    report.check("acknowledge resets the reminder to one interval", ok and policy().get("next_at") == policy().get("changes", 0) + 5, policy())
    ok, intent = rig.call("memory_cleanup", {"action": "confirm", "scope": SCOPE_A, "purpose": "configure",
                                             "approval_quote": "Turn the cleanup reminder off", "request_id": "cfg-2"})
    ok, out = rig.call("memory_cleanup", {"action": "configure", "scope": SCOPE_A, "policy": {
        "scope": SCOPE_A, "interval": 5, "enabled": False, "repeating": True, "source_id": intent.get("approval_source_id")}})
    report.check("the reminder can be switched off", ok and policy().get("enabled") is False, policy())


def utilities(report, rig):
    section("utility commands")
    rig.close()
    library = ["--data-dir", str(rig.library)]
    code, out, _ = rig.cli("status", *library)
    report.check("status reports the schema and counts", code == 0 and json.loads(out).get("schema_version") == 10, out)
    code, out, _ = rig.cli("backup", "--out", rig.root / "backup.db", *library)
    report.check("backup writes a standalone snapshot", code == 0 and (rig.root / "backup.db").is_file(), out)
    code, out, _ = rig.cli("export", "--out", rig.root / "export.json", *library)
    report.check("export writes a JSON snapshot", code == 0 and (rig.root / "export.json").is_file(), out)
    code, out, _ = rig.cli("diagnose", *library)
    report.check("diagnose is read-only metadata", code == 0 and json.loads(out).get("diagnostics", {}).get("read_only") is True, out)
    code, out, err = rig.cli("doctor", *rig.common, "--challenge", "0" * 32)
    report.check("doctor --challenge for an unknown ID fails with a reason", code != 0 and "challenge not found" in out + err, out + err)
    code, out, _ = rig.cli("adapters")
    report.check("adapters lists the implemented hosts", code == 0 and [a["name"] for a in json.loads(out)] == ["zcode", "codex", "qoder", "qoder-ide"], out)
    rig.mcp()
    ok, page = rig.call("memory_search", {"scope": SCOPE_A, "query": ""})
    victim = page["results"][0]["id"]
    rig.close()
    code, out, _ = rig.cli("forget", "--id", victim, "--yes", *library)
    rig.mcp()
    ok, page = rig.call("memory_search", {"scope": SCOPE_A, "query": ""})
    report.check("forget removes the record and its lineage", code == 0 and victim not in {r["id"] for r in page["results"]}, out)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary")
    parser.add_argument("--work-dir", help="place the disposable workspace here instead of the system temp directory")
    args = parser.parse_args()
    report = Report()
    with tempfile.TemporaryDirectory(prefix="memory-scenarios-", dir=args.work_dir) as tmp:
        rig = Rig(Path(args.binary).resolve(), tmp)
        for step in (install, writes, comparison, unreachable, recall, revisions, agreement, selfDescription, reference, trigger, maintenance, utilities):
            try:
                step(report, rig)
            except Exception as error:  # keep going: one broken scenario must not hide the rest
                report.check(step.__name__ + " completed", False, "%s: %s" % (type(error).__name__, error))
        rig.close()
    print("\n%d checks, %d failed" % (report.total, len(report.failed)), flush=True)
    return 1 if report.failed else 0


if __name__ == "__main__":
    sys.exit(main())
