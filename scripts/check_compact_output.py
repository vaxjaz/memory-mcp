#!/usr/bin/env python3
"""Measure bounded navigation/receipts against the actual executable in a disposable library."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess
import tempfile

from smoke import Host, fixture_profile, mode_args


def size(value):
    return len(json.dumps(value, ensure_ascii=False, separators=(',', ':')).encode('utf-8'))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('binary')
    parser.add_argument('--if-supported', action='store_true')
    args = parser.parse_args()
    binary = str(Path(args.binary).resolve())
    with tempfile.TemporaryDirectory(prefix='memory compact 中文 ') as tmp:
        root = Path(tmp); data = root / 'library'
        host = Host(binary, str(data), mode="strict")
        try:
            boot = host.tool('memory_bootstrap', {})
            if 'compact_context' not in boot['features'] and args.if_supported:
                print('SKIP: historical executable has no compact output')
                return
            scope = 'project:compact-fixture'
            body = 'Synthetic source only. 中文🙂\n' * 400
            raw_args = dict(kind='record', scope=scope, title='Fixture source', body=body,
                            origin='user_supplied', request_id='source', response_mode='compact')
            raw = host.tool('memory_record', raw_args)
            assert 'body' not in raw and size(raw) < 1500
            assert raw['body_sha256'] == hashlib.sha256(body.encode()).hexdigest()
            same = host.tool('memory_record', dict(raw_args, response_mode='full'))
            assert same['id'] == raw['id'] and same['body'] == body
            assert host.tool('memory_read', dict(id=raw['id']))['entry']['body'] == body
            profile = fixture_profile()
            for field in ['future_use', 'counter_check', 'applicability', 'boundary']:
                profile[field] = field + ' ' + '隔离测试限定条件' * 120
            cases = set()
            draft = dict(kind='case', scope=scope, title='needle synthetic fixture', body='needle ' + body,
                         origin='model_inference', profile=profile, response_mode='compact',
                         links=[dict(relation='supports', target_id=raw['id'])])
            for i in range(35):
                saved = host.tool('memory_record', dict(draft, title=f'needle fixture {i}'))
                assert 'body' not in saved and 'profile' not in saved and saved['link_count'] == 1
                cases.add(saved['id'])
            task = dict(action='prepare', scope=scope, source_id=raw['id'], task='Inspect exact fixture applicability',
                        questions='Compare fixture conditions only', keywords=['needle'])
            prepared = host.tool('memory_context', task)
            full = host.tool('memory_context', dict(task, response_mode='full'))
            compact = host.tool('memory_context', task)
            assert compact['response_mode'] == 'compact' and size(compact) <= 8192
            assert len(compact.get('matches', [])) <= 8 and len(compact.get('directory', [])) <= 8
            assert 'task' not in compact and 'questions' not in compact
            assert full['task'] == task['task'] and any(x.get('profile') for x in full['directory'])
            text = host.request('tools/call', {'name': 'memory_context', 'arguments': task})
            assert len(text['content'][0]['text'].encode()) <= 8192
            assert text['content'][0]['text'].find('"matches"') < text['content'][0]['text'].find('"next_cursor"')
            seen = {x['id'] for x in compact.get('matches', []) + compact.get('directory', [])}
            cursor = compact.get('next_cursor')
            for _ in range(20):
                if not cursor:
                    break
                page = host.tool('memory_context', dict(task, cursor=cursor))
                assert size(page) <= 8192 and page['keyword_search']['status'] == 'not_repeated'
                assert not page.get('matches')
                seen.update(x['id'] for x in page.get('directory', []))
                cursor = page.get('next_cursor')
            assert not cursor and cases <= seen, 'candidate pagination lost entries'
            chosen = next(iter(cases))
            completion = dict(action='complete', scope=scope, source_id=raw['id'], selected_ids=[chosen],
                              decision='Use only the isolated fixture conditions. ' * 80)
            host.tool('memory_context', completion, True)  # previews are not full reads
            host.tool('memory_read', dict(id=chosen, context_source_id=raw['id']))
            done = host.tool('memory_context', completion)
            assert done['status'] == 'complete' and 'decision' not in done and size(done) < 1500
            status = host.tool('memory_context', dict(action='status', scope=scope, source_id=raw['id'], response_mode='full'))
            assert status['decision'] == completion['decision']
            revised = host.tool('memory_revise', dict(previous_id=chosen, draft=dict(draft,
                context_source_id=raw['id'], body='Updated fixture ' + body)))
            assert 'body' not in revised and revised['checked_context_source_id'] == raw['id']
            assert host.tool('memory_read', dict(id=revised['id']))['entry']['body'] == 'Updated fixture ' + body
            host.tool('memory_record', dict(raw_args, request_id='invalid-mode', response_mode='invalid'), True)
            # Actual managed-command replay: reuse closes the turn without another revision/Stop continuation.
            project = root / 'project'; project.mkdir()
            config = root / 'user' / 'config.toml'
            p = subprocess.run([binary, 'setup', *mode_args(binary), '--host', 'codex', '--config', str(config),
                '--data-dir', str(data), '--project', str(project), '--scope', scope,
                '--capture-user-text', '--review-on-stop'], capture_output=True, text=True, encoding='utf-8', timeout=30)
            assert p.returncode == 0, p.stderr
            command = json.loads((config.parent/'hooks.json').read_text(encoding='utf-8'))['hooks']['UserPromptSubmit'][0]['hooks'][0]['command']
            def hook(event, **extra):
                payload = dict(hook_event_name=event, cwd=str(project), session_id='reuse', turn_id='turn1', **extra)
                result = subprocess.run(command, shell=True, cwd=project, input=json.dumps(payload),
                    capture_output=True, text=True, encoding='utf-8', timeout=30)
                assert result.returncode == 0, (result.stdout, result.stderr)
                return json.loads(result.stdout)
            prompt = hook('UserPromptSubmit', prompt='Reuse the unchanged isolated fixture decision')['hookSpecificOutput']['additionalContext']
            assert 'response_mode=compact' in prompt and '必须传 context_source_id' in prompt
            sid = re.search(r'本轮用户原文ID=([^。]+)', prompt).group(1)
            host.tool('memory_context', dict(action='prepare', scope=scope, source_id=sid, task='Reuse fixture'))
            host.tool('memory_read', dict(id=revised['id'], context_source_id=sid))
            host.tool('memory_context', dict(action='complete', scope=scope, source_id=sid,
                selected_ids=[revised['id']], decision='No material addition; reuse only', save_candidate=True))
            note = 'Synthetic reuse explanation. ' * 80
            review = host.tool('memory_review_done', dict(scope=scope, source_id=sid,
                reused_case_ids=[revised['id']], note=note, response_mode='compact'))
            assert review['reused_case_ids'] == [revised['id']] and review['status'] == 'reviewed'
            assert 'note' not in review and 'query' not in review and size(review) < 1500
            assert review['review_note_sha256'] == hashlib.sha256(note.encode()).hexdigest()
            assert hook('Stop', last_assistant_message='Fixture answer') == {}
            assert host.tool('memory_read', dict(id=revised['id']))['entry']['current']
            refs = host.tool('memory_read', dict(id=sid))['referenced_by']
            assert not any(x['kind'] == 'case' for x in refs), 'reuse modified case links'
            print(json.dumps({'fixture_only': True, 'prepare_full_json_bytes': size(full),
                'prepare_compact_json_bytes': size(prepared), 'write_receipt_bytes': size(raw),
                'complete_receipt_bytes': size(done), 'review_receipt_bytes': size(review)}, ensure_ascii=False))
        finally:
            host.close()
    print('PASS: bounded previews and pagination, no false full-read receipt, stored evidence/decisions intact, legacy echo/retry, reuse without Stop revision (isolated replay)')


if __name__ == '__main__':
    main()
