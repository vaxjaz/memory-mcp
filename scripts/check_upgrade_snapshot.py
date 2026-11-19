#!/usr/bin/env python3
"""Read-only source snapshot; migrate ONLY its disposable copy."""
import argparse
from contextlib import closing
import hashlib
import json
from pathlib import Path
import sqlite3
import shutil
import subprocess
import tempfile
import time


def tables_digest(db, tables):
    digest = hashlib.sha256()
    counts = {}
    for table in sorted(tables):
        quoted = '"' + table.replace('"', '""') + '"'
        rows = [json.dumps(list(row), ensure_ascii=False, sort_keys=True, default=repr)
                for row in db.execute("SELECT * FROM " + quoted)]
        counts[table] = len(rows)
        digest.update((table + "\n" + "\n".join(sorted(rows)) + "\n").encode())
    return digest.hexdigest(), counts


def cue_profiles(db, before=None, target=None):
    table_exists = db.execute(
        "SELECT count(*) FROM sqlite_master WHERE name='entry_profiles' AND type='table'").fetchone()[0]
    profiles, pending = {}, set()
    if not table_exists:
        return profiles, pending
    for entry_id, raw in db.execute("SELECT entry_id, profile_json FROM entry_profiles"):
        profile = json.loads(raw)
        if before is not None:
            for key in ("experience", "agreement"):
                model = profile.get(key)
                if not isinstance(model, dict):
                    continue
                if before < 6:
                    if "object_id_prefixes" not in model and isinstance(model.get("anchor_prefixes"), list):
                        model["object_id_prefixes"] = model["anchor_prefixes"]
                    for obsolete in ("anchors", "signals", "anchor_prefixes"):
                        model.pop(obsolete, None)
                    if not model.get("cues"):
                        pending.add(entry_id)
                if target >= 7:
                    for cue in model.get("cues") or []:
                        if "entrances" not in cue:
                            continue
                        groups = []
                        for entrance in cue.pop("entrances") or []:
                            value = entrance["value"]
                            rule = {"op": "token"}
                            if entrance["kind"] == "business_signal":
                                rule["normalize"] = "whitespace"
                            elif "/" in value or "\\" in value:
                                rule = {"op": "contains", "normalize": "path"}
                            elif not value.isascii():
                                rule["op"] = "contains"
                            if groups and {k: v for k, v in groups[-1].items() if k != "entrances"} == rule:
                                if value not in groups[-1]["entrances"]:
                                    groups[-1]["entrances"].append(value)
                            else:
                                groups.append(dict(rule, entrances=[value]))
                        cue["matches"] = groups
        profiles[entry_id] = profile
    return profiles, pending


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary")
    parser.add_argument("source_library")
    parser.add_argument("--old-binary", help="Verify rollback against this original executable")
    parser.add_argument("--out", type=Path)
    args = parser.parse_args()
    binary = str(Path(args.binary).resolve())
    source = Path(args.source_library).resolve() / "memory.db"
    target_schema = json.loads(subprocess.check_output([binary, "build-info"], encoding="utf-8"))["schema_version"]
    with tempfile.TemporaryDirectory(prefix="memory-readonly-migration-") as temp:
        data = Path(temp)
        copied = data / "memory.db"
        with closing(sqlite3.connect(source.as_uri() + "?mode=ro", uri=True)) as original:
            with closing(sqlite3.connect(copied)) as snapshot:
                original.backup(snapshot)
        with closing(sqlite3.connect(copied)) as db:
            version_before = db.execute("PRAGMA user_version").fetchone()[0]
            tables = [row[0] for row in db.execute(
                "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'")]
            delivered_before = db.execute("SELECT count(*) FROM experience_body_deliveries").fetchone()[0] if "experience_body_deliveries" in tables else 0
            # Only these obsolete/derived runtime/index tables may change. Every
            # other preexisting table, including unknown historical tables,
            # must preserve its logical rows exactly.
            derived = {"experience_keys", "experience_epochs", "experience_events",
                       "experience_challenges", "experience_wakeups", "experience_cooldowns",
                       "experience_cooldown_settings", "recall_responses", "recall_wakeup_responses",
                       "business_observations", "recall_documents", "recall_postings",
                       "recall_terms", "recall_index_meta", "recall_index_version", "experience_entrances"}
            explicit = 0 < version_before < target_schema and target_schema >= 6
            if explicit:
                derived.update({"entries", "entry_profiles"})
                expected_profiles, pending = cue_profiles(db, version_before, target_schema)
                entry_columns = [row[1] for row in db.execute("PRAGMA table_info(entries)")]
                selected_columns = ", ".join('"' + name.replace('"', '""') + '"' for name in entry_columns)
                original_entries = list(db.execute(f"SELECT {selected_columns} FROM entries ORDER BY id"))
                expected_entries = []
                for row in original_entries:
                    entry = dict(zip(entry_columns, row))
                    if entry["current"] and entry["id"] in pending:
                        entry["needs_review"] = 1
                    expected_entries.append(tuple(entry.values()))
            tables = [t for t in tables if t not in derived]
            before, counts = tables_digest(db, tables)
        rollback = data / "rollback"
        rollback.mkdir()
        shutil.copy2(copied, rollback / "memory.db")
        start = time.perf_counter()
        migration = {}
        if explicit:
            preview = json.loads(subprocess.check_output(
                [binary, "migrate", "--data-dir", str(data)], encoding="utf-8"))
            assert preview["from_schema"] == version_before and not preview["applied"], preview
            migration = json.loads(subprocess.check_output(
                [binary, "migrate", "--data-dir", str(data), "--apply", "--backup", str(data / "before-migrate.db")],
                encoding="utf-8"))
            assert (migration["applied"] and migration["current_entries_needing_cues"] ==
                    preview["current_entries_needing_cues"]), migration
            shutil.copy2(migration["backup"], rollback / "memory.db")
        process = subprocess.run([binary, "status", "--data-dir", str(data)],
                                 check=True, capture_output=True, text=True, encoding="utf-8")
        elapsed = (time.perf_counter() - start) * 1000
        status = json.loads(process.stdout)
        with closing(sqlite3.connect(copied)) as db:
            after, after_counts = tables_digest(db, tables)
            assert before == after and counts == after_counts, "source tables changed outside the migration plan"
            if explicit:
                assert list(db.execute(f"SELECT {selected_columns} FROM entries ORDER BY id")) == expected_entries
                assert cue_profiles(db)[0] == expected_profiles, "unexpected rewrite of profile data"
            assert db.execute("PRAGMA integrity_check").fetchone()[0] == "ok"
            indexed = db.execute("SELECT count(*) FROM recall_documents").fetchone()[0]
            delivered_after = db.execute("SELECT count(*) FROM experience_body_deliveries").fetchone()[0]
            assert delivered_after == delivered_before, "upgrade reset or invented body delivery"
            input_events_available = db.execute("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='experience_input_events'").fetchone()[0] == 1
            assert input_events_available
            legacy = db.execute("SELECT count(*) FROM entry_profiles WHERE profile_json LIKE '%experience%'").fetchone()[0]
        assert status["schema_version"] == target_schema
        restored = False
        if args.old_binary:
            subprocess.run([str(Path(args.old_binary).resolve()), "status", "--data-dir", str(rollback)],
                           check=True, capture_output=True, text=True, encoding="utf-8")
            with closing(sqlite3.connect(rollback / "memory.db")) as db:
                restored_digest, restored_counts = tables_digest(db, tables)
                assert restored_digest == before and restored_counts == counts
                if explicit:
                    assert list(db.execute(f"SELECT {selected_columns} FROM entries ORDER BY id")) == original_entries
                assert db.execute("PRAGMA integrity_check").fetchone()[0] == "ok"
            restored = True
        result = dict(result="PASS", source_opened_read_only=True,
                              migrated_disposable_copy=True, schema_before=version_before,
                              schema_after=status["schema_version"], source_tables_unchanged=True,
                              source_table_counts=counts, logical_sha256=after,
                              indexed_documents=indexed, structured_profiles=legacy,
                              body_delivery_count_preserved=delivered_after,
                              input_event_table_available=input_events_available,
                              old_binary_backup_restore_verified=restored,
                              migration_ms=round(elapsed, 1))
        if explicit:
            result["cue_schema_migration"] = True
            result["current_entries_needing_cues"] = len(migration["current_entries_needing_cues"])
            result["source_tables_unchanged"] = False
            result["transformed_entries_and_profiles_verified"] = True
        if args.out:
            args.out.write_text(json.dumps(result, ensure_ascii=False, indent=2)+"\n", encoding="utf-8")
        print(json.dumps(result))


if __name__ == "__main__":
    main()
