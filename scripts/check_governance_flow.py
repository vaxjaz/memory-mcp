#!/usr/bin/env python3
"""Real executable + installed hook args. Host semantic decisions are simulated."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import tempfile

from smoke import Host, fixture_profile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('binary')
    parser.add_argument('--if-supported', action='store_true')
    opt = parser.parse_args()
    binary = str(Path(opt.binary).resolve())
    with tempfile.TemporaryDirectory(prefix='memory-governance-') as tmp:
        root = Path(tmp)
        project = root / 'actual-project'; project.mkdir()
        user = root / 'user'; user.mkdir()
        data = user / 'memory'
        config = user / 'zcode.json'
        scope = 'project:governance-fixture'
        host = Host(binary, str(data))
        try:
            boot = host.tool('memory_bootstrap', {})
            if 'contextual_retrieval' not in boot.get('features', []):
                assert opt.if_supported
                print('SKIP: historical release predates governance workflow')
                return
            def command(*args, payload=None):
                result = subprocess.run([binary, *args], input=None if payload is None else json.dumps(payload, ensure_ascii=False),
                                        text=True, encoding='utf-8', capture_output=True, timeout=30)
                assert result.returncode == 0, (args, result.stdout, result.stderr)
                return json.loads(result.stdout)
            report = command('setup', '--host', 'zcode', '--config', str(config), '--data-dir', str(data),
                             '--project', str(project), '--scope', scope, '--capture-user-text', '--review-on-stop')
            assert report['binding_verified'] and report['context_check'] and not report['host_event_verified']
            cfg = json.loads(config.read_text(encoding='utf-8'))
            installed = cfg['hooks']['events']['PreToolUse'][0]['hooks'][0]
            assert '--context-check' in installed['args']
            assert cfg['hooks']['events']['PreToolUse'][0]['matcher'] == '.*'
            def hook(event, prompt='', **fields):
                return command(*installed['args'], payload={'hook_event_name': event, 'cwd': str(project),
                               'session_id': 'context-session', 'prompt': prompt, **fields})
            def source(prompt):
                out = hook('UserPromptSubmit', prompt)
                assert 'memory_context' in out['hookSpecificOutput']['additionalContext']
                rows = host.tool('memory_search', {'scope': scope, 'include_events': True, 'kind': 'record', 'limit': 30})['results']
                return next(row['id'] for row in rows if host.tool('memory_read', {'id': row['id']})['entry']['body'] == prompt)
            def record(body):
                return host.tool('memory_record', {'kind': 'record', 'scope': scope, 'title': 'Fixture source', 'body': body, 'origin': 'user_supplied'})

            raw = record('In this test environment, no-argument login hangs; explicit arguments succeed.')
            draft = {'kind': 'case', 'scope': scope, 'title': 'Login argument constraint',
                     'body': 'Use explicit login arguments only in the tested environment; other environments are unknown.',
                     'origin': 'model_inference', 'links': [{'target_id': raw['id'], 'relation': 'supports'}]}
            host.tool('memory_record', draft, expect_error=True)
            draft['profile'] = fixture_profile()
            draft['profile']['search_hints'] = ['token request hangs', 'login has no response']
            case = host.tool('memory_record', draft)

            sid = source('The token request is not returning here either.')
            assert hook('PreToolUse', tool_name='development_login', tool_input={})['decision'] == 'block'
            prepared = host.tool('memory_context', {'action': 'prepare', 'scope': scope, 'source_id': sid,
                                  'task': 'Current project test-environment login; inspect prior configuration-dependent hangs',
                                  'questions': 'Verify environment and exceptions, not all login clients', 'keywords': ['token request hangs']})
            assert case['id'] in [item['id'] for item in prepared['directory']]
            complete = {'action': 'complete', 'scope': scope, 'source_id': sid, 'selected_ids': [case['id']],
                        'decision': 'Supply explicit arguments only after checking this is the tested environment', 'save_candidate': False}
            host.tool('memory_context', complete, expect_error=True)
            host.tool('memory_read', {'id': case['id'], 'context_source_id': sid})
            host.tool('memory_context', complete)
            assert hook('PreToolUse', tool_name='development_login', tool_input={'account': 'synthetic'}) == {}
            host.tool('memory_review_done', {'scope': scope, 'source_id': sid, 'reused_case_ids': [case['id']], 'note': 'Same decision, no changed condition'})
            assert hook('Stop', last_assistant_message='Synthetic business result') == {}
            assert host.tool('memory_read', {'id': case['id']})['current_id'] == case['id']

            ordinary = source('Thanks, that is all.')
            host.tool('memory_context', {'action': 'skip', 'scope': scope, 'source_id': ordinary,
                                        'task': 'End conversation', 'decision': 'No decision or reusable new knowledge'})
            assert hook('Stop', last_assistant_message='Okay.') == {}

            setting = source('After 500 more knowledge changes, show one cleanup preview.')
            result = host.tool('memory_cleanup', {'action': 'configure', 'scope': scope,
                               'policy': {'scope': scope, 'interval': 500, 'enabled': True, 'repeating': False, 'source_id': setting}})
            assert result['maintenance']['next_at'] - result['maintenance']['changes'] == 500
            assert result['maintenance']['repeating'] is False
            preview = host.tool('memory_cleanup', {'action': 'preview', 'scope': scope, 'operations': [{
                'entry_id': raw['id'], 'action': 'supersede', 'replacement_id': case['id'],
                'reason': 'In this synthetic fixture the case is the explicit complete replacement', 'evidence_ids': [raw['id'], case['id']]}]})['plan']
            decision = {'action': 'apply', 'scope': scope, 'plan_id': preview['id'],
                        'approval_source_id': setting, 'approval_quote': 'After 500 more knowledge changes'}
            host.tool('memory_cleanup', decision, expect_error=True)
            yes = source('Apply this displayed cleanup plan.')
            decision.update(approval_source_id=yes, approval_quote='Apply this displayed cleanup plan.')
            applied = host.tool('memory_cleanup', decision)['plan']
            assert Path(applied['backup_path']).is_file()
            original = host.tool('memory_read', {'id': raw['id']})
            assert original['entry']['body'] == raw['body'] and original['effective_current_id'] == case['id']
            undo_source = source('Undo this cleanup.')
            restored = host.tool('memory_cleanup', {'action': 'undo', 'scope': scope, 'plan_id': preview['id'],
                                 'approval_source_id': undo_source, 'approval_quote': 'Undo this cleanup.'})
            assert restored['plan']['status'] == 'undone'
            assert host.tool('memory_read', {'id': raw['id']})['entry']['management']['state'] == 'active'

            doctor = command('doctor', '--config', str(config), '--data-dir', str(data), '--project', str(project), '--scope', scope, '--reviews')
            assert doctor['binding_verified'] and doctor['resolved_scope'] == scope
            assert doctor['context_check_handlers'] == 4
            assert any(service['matches_opened_data_dir'] for service in doctor['memory_services'])
            assert not list(project.iterdir())
        finally:
            host.close()
    print('PASS: installed contextual hooks, admission, pre-action read gate, no ordinary Stop continuation, reuse, configurable maintenance, fresh cleanup approval, backup/undo and project binding diagnostics.')
    print('Host semantic choices were simulated; this does not prove real GLM understanding or prevented errors.')


if __name__ == '__main__':
    main()
