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


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary")
    parser.add_argument("source_library")
    parser.add_argument("--old-binary", help="Verify rollback against this original executable")
    parser.add_argument("--out", type=Path)
    args = parser.parse_args()
    binary = str(Path(args.binary).resolve())
    source = Path(args.source_library).resolve() / "memory.db"
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
            # Only these obsolete/derived runtime/index tables may change. Every
            # other preexisting table, including unknown historical tables,
            # must preserve its logical rows exactly.
            derived = {"experience_keys", "experience_epochs", "experience_events",
                       "experience_challenges", "experience_wakeups", "experience_cooldowns",
                       "experience_cooldown_settings", "recall_responses", "recall_wakeup_responses",
                       "business_observations", "recall_documents", "recall_postings",
                       "recall_terms", "recall_index_meta", "experience_entrances"}
            tables = [t for t in tables if t not in derived]
            before, counts = tables_digest(db, tables)
        rollback = data / "rollback"
        rollback.mkdir()
        shutil.copy2(copied, rollback / "memory.db")
        start = time.perf_counter()
        process = subprocess.run([binary, "status", "--data-dir", str(data)],
                                 check=True, capture_output=True, text=True, encoding="utf-8")
        elapsed = (time.perf_counter() - start) * 1000
        status = json.loads(process.stdout)
        with closing(sqlite3.connect(copied)) as db:
            after, after_counts = tables_digest(db, tables)
            assert before == after and counts == after_counts, "original source tables changed"
            assert db.execute("PRAGMA integrity_check").fetchone()[0] == "ok"
            indexed = db.execute("SELECT count(*) FROM recall_documents").fetchone()[0]
            assert db.execute("SELECT count(*) FROM experience_body_deliveries").fetchone()[0] == 0
            legacy = db.execute("SELECT count(*) FROM entry_profiles WHERE profile_json LIKE '%experience%'").fetchone()[0]
        assert status["schema_version"] == 5
        restored = False
        if args.old_binary:
            subprocess.run([str(Path(args.old_binary).resolve()), "status", "--data-dir", str(rollback)],
                           check=True, capture_output=True, text=True, encoding="utf-8")
            with closing(sqlite3.connect(rollback / "memory.db")) as db:
                restored_digest, restored_counts = tables_digest(db, tables)
                assert restored_digest == before and restored_counts == counts
                assert db.execute("PRAGMA integrity_check").fetchone()[0] == "ok"
            restored = True
        result = dict(result="PASS", source_opened_read_only=True,
                              migrated_disposable_copy=True, schema_before=version_before,
                              schema_after=status["schema_version"], source_tables_unchanged=True,
                              source_table_counts=counts, logical_sha256=after,
                              indexed_documents=indexed, structured_profiles=legacy,
                              old_binary_backup_restore_verified=restored,
                              migration_ms=round(elapsed, 1))
        if args.out:
            args.out.write_text(json.dumps(result, ensure_ascii=False, indent=2)+"\n", encoding="utf-8")
        print(json.dumps(result))


if __name__ == "__main__":
    main()
