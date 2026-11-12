#!/usr/bin/env python3
"""Reproduce historical-source checkpoint failures against a real isolated executable."""
import argparse
from contextlib import closing
import hashlib
import json
from pathlib import Path
import re
import sqlite3
import subprocess
import tempfile

from smoke import Host, fixture_profile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('binary')
    parser.add_argument('--if-supported', action='store_true')
    args = parser.parse_args()
    binary = str(Path(args.binary).resolve())
    with tempfile.TemporaryDirectory(prefix='memory diagnostic 中文 # ') as tmp:
        root = Path(tmp)
        data = root / 'library'
        project = root / 'project'; project.mkdir()
        config = root / 'user' / 'config.toml'
        scope = 'project:isolated-context-regression'
        host = Host(binary, str(data))
        try:
            bootstrap = host.tool('memory_bootstrap', {})
            if 'explicit_write_context' not in bootstrap['features'] and args.if_supported:
                print('SKIP: historical executable has no explicit write checkpoint')
                return
            def cli(*cmd, success=True):
                p = subprocess.run([binary, *cmd], capture_output=True, text=True, encoding='utf-8', timeout=30)
                assert (p.returncode == 0) == success, (cmd, p.stdout, p.stderr)
                return p
            cli('setup', '--host', 'codex', '--config', str(config), '--data-dir', str(data),
                '--project', str(project), '--scope', scope, '--capture-user-text', '--review-on-stop')
            command = json.loads((config.parent / 'hooks.json').read_text(encoding='utf-8'))['hooks']['UserPromptSubmit'][0]['hooks'][0]['command']
            def hook(turn):
                payload = dict(hook_event_name='UserPromptSubmit', cwd=str(project), session_id='epm-fixture',
                               turn_id=turn, prompt='Isolated fixture: ' + turn)
                p = subprocess.run(command, shell=True, input=json.dumps(payload), cwd=project,
                                   capture_output=True, text=True, encoding='utf-8', timeout=30)
                assert p.returncode == 0, (p.stdout, p.stderr)
                context = json.loads(p.stdout)['hookSpecificOutput']['additionalContext']
                assert 'data_dir=' in context and 'epm-fixture' in context, context
                return re.search(r'本轮用户原文ID=([^。]+)', context).group(1)
            old = hook('old-contract-request')
            current = hook('current-contract-verification')
            host.tool('memory_context', dict(action='prepare', scope=scope, source_id=current, task='Verify fixture contract'))
            # Reproduce nonempty selected_ids with an actually tracked full read.
            host.tool('memory_read', dict(id=old, context_source_id=current))
            complete = host.tool('memory_context', dict(action='complete', scope=scope, source_id=current,
                selected_ids=[old], decision='Historical source read; verified fixture contract is reusable', save_candidate=True))
            assert complete['status'] == 'complete'
            draft = dict(kind='case', scope=scope, title='Isolated contract fixture', body='Verified fixture material only',
                         origin='model_inference', profile=fixture_profile(), links=[dict(relation='supports', target_id=old)])
            saved = host.tool('memory_record', draft)
            assert saved['checked_context_source_id'] == current and saved['management']['state'] == 'active', saved
            assert host.tool('memory_read', dict(id=saved['id']))['entry']['management'] == saved['management']
            stale = host.tool('memory_context', dict(action='complete', scope=scope, source_id=old, decision='Must remain rejected'), True)
            assert 'latest user input' in json.dumps(stale)
            revised = dict(draft, body='Updated fixture contract', context_source_id=current)
            updated = host.tool('memory_revise', dict(previous_id=saved['id'], draft=revised))
            assert updated['checked_context_source_id'] == current
            bad = dict(revised, context_source_id=old)
            err = host.tool('memory_revise', dict(previous_id=updated['id'], draft=bad), True)
            assert 'stale_write_context' in json.dumps(err)
            missing = 'f' * 27
            bad = dict(revised, links=[dict(relation='context', target_id=missing), dict(relation='supports', target_id=old)])
            err = host.tool('memory_revise', dict(previous_id=updated['id'], draft=bad), True)
            text = json.dumps(err)
            assert missing in text and 'draft.links[0].target_id' in text and 'data_dir=' in text, err
            assert host.tool('memory_read', dict(id=updated['id']))['entry']['current']
            err = host.tool('memory_context', dict(action='skip', scope=scope, source_id=missing,
                task='Inspect nonexistent fixture', decision='No memory applies'), True)
            assert 'user_source_not_found' in json.dumps(err) and missing in json.dumps(err)
            raw = host.tool('memory_record', dict(kind='record', scope=scope, title='Manual source', body='Fixture user wording', origin='user_supplied'))
            status = host.tool('memory_context', dict(action='status', scope=scope, source_id=raw['id']))
            assert status['status'] == 'not_prepared'
            err = host.tool('memory_context', dict(action='complete', scope=scope, source_id=raw['id'], decision='Cannot bypass prepare'), True)
            assert 'context_not_prepared' in json.dumps(err)
            newer = hook('new-pending-task')
            err = host.tool('memory_record', draft, True)
            assert 'write_context_not_complete' in json.dumps(err) and newer in json.dumps(err)
        finally:
            host.close()
        # The read-only diagnostic must not seed state or mutate the examined DB.
        dbpath = data / 'memory.db'
        with closing(sqlite3.connect(dbpath)) as db:
            with db:
                db.execute('DELETE FROM operation_clock')
        digest = hashlib.sha256(dbpath.read_bytes()).hexdigest()
        output = root / 'report.json'
        cli('diagnose', '--data-dir', str(data), '--scope', scope, '--entry-id', updated['id'],
            '--probe-ids', ','.join([old, current, missing]), '--out', str(output))
        report = json.loads(output.read_text(encoding='utf-8'))['diagnostics']
        assert report['read_only'] and not report['write_clock_present'] and report['missing_targets'] == 0
        assert report['probe_sources'][0]['hook_turn_exists'] and report['probe_sources'][0]['capture_logged_in_this_library']
        assert not report['probe_sources'][2]['exists'] and not report['probe_sources'][2]['format_32_hex']
        assert 'Verified fixture material only' not in output.read_text(encoding='utf-8')
        assert hashlib.sha256(dbpath.read_bytes()).hexdigest() == digest
        cli('diagnose', '--data-dir', str(data), '--out', str(output), success=False)
        print('PASS: current/historical checkpoint separation, missing-ID diagnostics, active receipt, read-only audit (isolated stdio/event replay)')


if __name__ == '__main__':
    main()
