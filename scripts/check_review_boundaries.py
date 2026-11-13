#!/usr/bin/env python3
"""Exercise consolidation/receipt boundaries with simulated host choices over stdio."""
import argparse
from contextlib import closing
import json
import os
from pathlib import Path
import sqlite3
import subprocess
import tempfile

from check_text_integrity import Wire
from smoke import fixture_profile, mode_args


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('binary')
    parser.add_argument('--if-supported', action='store_true', help='Skip older release binaries without linked_case_receipts')
    args = parser.parse_args()
    binary = str(Path(args.binary).resolve())
    with tempfile.TemporaryDirectory(prefix='memory-review-boundaries-') as tmp:
        root = Path(tmp)
        home, project, data = root / 'home', root / 'project', root / 'hook-data'
        home.mkdir(); project.mkdir()
        env = dict(os.environ, HOME=str(home), USERPROFILE=str(home))
        scope = 'project:review-boundary-test'
        wire = Wire(binary, data, env)
        try:
            boot = wire.tool('memory_bootstrap', {})
            admitted = {'profile': fixture_profile()} if 'quality_admission' in boot.get('features', []) else {}
            if args.if_supported and 'linked_case_receipts' not in boot.get('features', []):
                print('SKIP: historical release predates linked-case receipt protection')
                return
            assert 'linked_case_receipts' in boot['features']
            assert 'A stdio connection alone does not capture chat.' in boot['guide']
            assert 'same scope' in boot['guide']
            tools = {t['name']: t for t in wire.request('tools/list', {})['tools']}
            assert 'saved before Stop' in tools['memory_review_done']['description']
            note_description = tools['memory_review_done']['inputSchema']['properties']['note']['description']
            assert 'why no case needs creating or revising' in note_description
            subprocess.run([binary, 'setup', *mode_args(binary), '--host', 'zcode', '--config', str(home / '.zcode/cli/config.json'),
                            '--data-dir', str(data), '--project', str(project), '--scope', scope,
                            '--capture-user-text', '--review-on-stop'], env=env, check=True, capture_output=True)

            def hook(event, session, **fields):
                result = subprocess.run([binary, 'hook', *mode_args(binary), '--host', 'zcode', '--data-dir', str(data),
                                         '--capture-user-text', '--review-on-stop'], env=env, check=True, capture_output=True,
                                        input=json.dumps({'hook_event_name': event, 'session_id': session,
                                                          'cwd': str(project), **fields}, ensure_ascii=False).encode())
                return json.loads(result.stdout)

            def start(session, prompt):
                hook('UserPromptSubmit', session, prompt=prompt)
                for row in wire.tool('memory_search', {'scope': scope, 'kind': 'record', 'include_events': True, 'limit': 30})['results']:
                    entry = wire.tool('memory_read', {'id': row['id']})['entry']
                    if entry.get('session') == session and entry['body'] == prompt:
                        return entry
                raise AssertionError('No captured source for ' + session)

            def turn_state(source_id):
                with closing(sqlite3.connect(data / 'memory.db')) as db:
                    return db.execute('select status,note from hook_turns where source_id=?', (source_id,)).fetchone()

            def case_draft(title, body, *sources):
                return {'kind': 'case', 'scope': scope, 'title': title, 'body': body,
                        'origin': 'model_inference', 'links': [{'target_id': i, 'relation': 'context'} for i in sources], **admitted}

            # Only a record exists: kinds cannot be converted, but a first case
            # can cite that material and the newly captured original.
            old = wire.tool('memory_record', {'kind': 'record', 'scope': scope, 'title': '测试旧资料',
                                              'body': '[测试资料]旧材料尚未整理为案例。', 'origin': 'imported'})
            source = start('record-only', '[测试反馈]补充旧资料的适用范围。')
            stop = hook('Stop', 'record-only', last_assistant_message='测试客户端准备整理反馈。')
            assert '同项目且 current=true 的已有 case' in stop['reason']
            assert '首个案例' in stop['reason']
            rejected = wire.tool('memory_revise', {'previous_id': old['id'], 'draft': case_draft('测试转换', '不可直接改类型', source['id'])}, error=True)
            assert 'revision cannot change scope or kind' in rejected['content'][0]['text']
            assert wire.tool('memory_read', {'id': old['id']})['entry']['kind'] == 'record'
            revised_record = wire.tool('memory_revise', {'previous_id': old['id'], 'draft': {
                'kind': 'record', 'scope': scope, 'title': old['title'], 'body': '[测试资料]合法修订仍然是 record。', 'origin': 'imported'}})
            wire.tool('memory_review_done', {'scope': scope, 'source_id': source['id'],
                                            'case_ids': [revised_record['id']], 'note': '测试：record 不能冒充 case 回执。'}, error=True)
            assert turn_state(source['id'])[0] == 'review_requested'
            first = wire.tool('memory_record', case_draft('测试首个案例', '[测试案例]引用现有资料及本轮补充。', revised_record['id'], source['id']))
            receipt = wire.tool('memory_review_done', {'scope': scope, 'source_id': source['id'],
                                                      'case_ids': [first['id']], 'note': '只有旧资料，已创建首个案例并保留资料类型。'})
            assert receipt['status'] == 'reviewed'

            # A case was already saved before Stop: empty acknowledgement must
            # remain retryable, not commit an irreversible skipped receipt.
            source2 = start('saved-before-stop', '[测试反馈]本轮决定已经保存为案例。')
            saved_draft = case_draft('测试已保存案例', '[测试案例]在 Stop 前完成保存。', source2['id'])
            saved_draft['links'].append({'target_id': source2['id'], 'relation': 'supports'})
            saved = wire.tool('memory_record', saved_draft)
            hook('Stop', 'saved-before-stop', last_assistant_message='测试客户端已处理。')
            refs = wire.tool('memory_read', {'id': source2['id']})['referenced_by']
            assert any(r['entry_id'] == saved['id'] and r['kind'] == 'case' for r in refs)
            before = turn_state(source2['id'])
            bad = wire.tool('memory_review_done', {'scope': scope, 'source_id': source2['id'],
                                                  'case_ids': [], 'note': '已有覆盖，误传空列表。'}, error=True)
            assert saved['id'] in bad['content'][0]['text']
            assert bad['content'][0]['text'].count(saved['id']) == 1
            assert turn_state(source2['id']) == before
            good = {'scope': scope, 'source_id': source2['id'], 'case_ids': [saved['id']], 'note': '沿用本轮已保存案例，不重复创建。'}
            assert wire.tool('memory_review_done', good)['status'] == 'reviewed'
            assert wire.tool('memory_review_done', good)['status'] == 'reviewed'
            wire.tool('memory_review_done', dict(good, case_ids=[], note='不能覆盖已完成回执。'), error=True)
            assert turn_state(source2['id'])[0] == 'reviewed'

            # Historical coverage with no case linked to this new turn may skip;
            # the hook-captured original remains readable and no duplicate is made.
            source3 = start('historical-coverage', '[测试反馈]重复此前已保存的决定，没有实质新增。')
            hook('Stop', 'historical-coverage', last_assistant_message='测试客户端沿用历史决定。')
            count = len(wire.tool('memory_search', {'scope': scope, 'kind': 'case', 'limit': 30})['results'])
            skipped = {'scope': scope, 'source_id': source3['id'], 'case_ids': [], 'note': '仅历史案例覆盖，无需新建或修订。'}
            assert wire.tool('memory_review_done', skipped)['status'] == 'skipped'
            assert wire.tool('memory_review_done', skipped)['status'] == 'skipped'
            assert len(wire.tool('memory_search', {'scope': scope, 'kind': 'case', 'limit': 30})['results']) == count
            assert wire.tool('memory_read', {'id': source3['id']})['entry']['body'] == source3['body']
        finally:
            wire.close()

        # No hooks: reading a historical case and choosing not to write does not
        # persist the current conversation. Saving a record explicitly does.
        plain_data = root / 'plain-stdio-data'
        wire = Wire(binary, plain_data, env)
        try:
            historic_source = wire.tool('memory_record', {'kind': 'record', 'scope': scope, 'title': '历史测试材料', 'body': '独立测试材料，仅用于验证普通stdio不会自动捕获对话', 'origin': 'imported'}) if admitted else None
            historical = wire.tool('memory_record', {'kind': 'case', 'scope': scope, 'title': '测试历史约定',
                                                    'body': '[测试案例]此前已保存的交付约定。', 'origin': 'imported', **admitted, **({'links': [{'target_id': historic_source['id'], 'relation': 'context'}]} if historic_source else {})})
            wire.tool('memory_read', {'id': historical['id']})
        finally:
            wire.close()
        wire = Wire(binary, plain_data, env)
        try:
            records = wire.tool('memory_search', {'scope': scope, 'kind': 'record', 'include_events': True})['results']
            assert [r['id'] for r in records] == ([historic_source['id']] if historic_source else [])
            original = '[独立测试原话]重复反馈无需新建案例，但我需要保留这次原始表达。  '
            raw = wire.tool('memory_record', {'kind': 'record', 'scope': scope, 'title': '测试显式原话留存',
                                             'body': original, 'origin': 'host_quote'})
        finally:
            wire.close()
        wire = Wire(binary, plain_data, env)
        try:
            assert wire.tool('memory_read', {'id': raw['id']})['entry']['body'] == original
            cases = wire.tool('memory_search', {'scope': scope, 'kind': 'case'})['results']
            assert [r['id'] for r in cases] == [historical['id']]
        finally:
            wire.close()
        print('PASS: record-only first case; pre-Stop saved-case receipt guard/retry; historical-only skip; stdio originals require explicit records; note guidance. Host choices were simulated.')


if __name__ == '__main__':
    main()
