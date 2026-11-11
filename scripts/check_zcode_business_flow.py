#!/usr/bin/env python3
"""Real binary/MCP/hooks; host-model decisions are explicitly simulated, not an AI eval."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile

from smoke import Host, fixture_profile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('binary')
    parser.add_argument('--if-supported', action='store_true', help='Skip old release binaries during release-repair workflows')
    args = parser.parse_args()
    binary = str(Path(args.binary).resolve())
    if args.if_supported and 'memory-mcp setup' not in subprocess.check_output([binary, 'help'], text=True):
        print('SKIP: this historical release predates the hook workflow')
        return
    with tempfile.TemporaryDirectory(prefix='memory-business-flow-') as tmp:
        root = Path(tmp)
        project = root / 'business-project'
        project.mkdir()
        home = root / 'user'
        home.mkdir()
        data = home / 'memory'
        config = home / '.zcode' / 'cli' / 'config.json'
        env = dict(os.environ, HOME=str(home), USERPROFILE=str(home))
        scope = 'project:business-acceptance'

        def command(*command_args, payload=None):
            result = subprocess.run([binary, *command_args], input=None if payload is None else json.dumps(payload, ensure_ascii=False),
                                    text=True, capture_output=True, env=env, timeout=30)
            assert result.returncode == 0, (command_args, result.stderr, result.stdout)
            return json.loads(result.stdout)

        setup = ['setup', '--host', 'zcode', '--config', str(config), '--data-dir', str(data),
                 '--project', str(project), '--scope', scope, '--capture-user-text', '--review-on-stop']
        report = command(*setup)
        assert report['host_event_verified'] is False and report['restart_required']
        command(*setup)
        cfg = json.loads(config.read_text())
        assert len(cfg['hooks']['events']['Stop']) == 1
        assert not list(project.iterdir())

        def hook(event, session, **fields):
            return command('hook', '--host', 'zcode', '--data-dir', str(data), '--capture-user-text', '--review-on-stop',
                           payload={'hook_event_name': event, 'cwd': str(project), 'session_id': session, **fields})

        host = Host(binary, str(data))
        try:
            features = host.tool('memory_bootstrap', {})['features']
            assert 'review_receipts' in features
            admitted = {'profile': fixture_profile()} if 'quality_admission' in features else {}
            original = '日期安排详情和编辑归在安排模块，不增加一级菜单。按这个原则给我一版方案。\n保留原话空格  '
            assert hook('UserPromptSubmit', 'decision-session', prompt=original) == {}
            raw = host.tool('memory_search', {'scope': scope, 'include_events': True})['results'][0]
            raw_entry = host.tool('memory_read', {'id': raw['id']})['entry']
            assert raw_entry['body'] == original
            assert raw_entry['body_sha256'] == hashlib.sha256(original.encode()).hexdigest()
            assert host.tool('memory_search', {'scope': scope})['results'] == []
            stop = hook('Stop', 'decision-session', last_assistant_message='方案：详情与编辑沿用安排模块的入口。')
            assert stop['decision'] == 'block' and raw['id'] in stop['reason']
            assert hook('Stop', 'decision-session', last_assistant_message='方案不变') == {}

            # Explicit host-model simulation: preserves actual source; no fixed retrieval answer.
            decision = host.tool('memory_record', {'kind': 'case', 'scope': scope, 'title': '日期安排页面与菜单归属',
                'body': '用户明确约束：日期安排详情与编辑属于安排模块，不另加一级菜单。适用范围为该业务页面。',
                'origin': 'model_inference', 'links': [{'target_id': raw['id'], 'relation': 'supports'}], 'request_id': 'business-case-1', **admitted})
            ack = host.tool('memory_review_done', {'scope': scope, 'source_id': raw['id'], 'case_ids': [decision['id']], 'note': '保存本轮明确的页面归属约束'})
            assert ack['status'] == 'reviewed'
            recalled = hook('UserPromptSubmit', 'later-session', prompt='给日期安排新增编辑页面，你怎么安排菜单入口？')
            assert decision['id'] in recalled['hookSpecificOutput']['additionalContext']

            correction = '补充纠偏：独立业务域可以新增一级入口，详情和编辑仍不单独增加菜单。'
            hook('UserPromptSubmit', 'later-session', prompt=correction)
            raw_rows = host.tool('memory_search', {'scope': scope, 'include_events': True, 'kind': 'record'})['results']
            correction_source = next(row for row in raw_rows if host.tool('memory_read', {'id': row['id']})['entry']['body'] == correction)
            current = host.tool('memory_revise', {'previous_id': decision['id'], 'draft': {'kind': 'case', 'scope': scope,
                'title': '日期安排页面归属与独立业务域例外', 'body': decision['body'] + '\n后续纠偏：独立业务域可新增一级入口，页面归属原则不能泛化为禁止新增模块。',
                'origin': 'model_inference', 'links': [{'target_id': raw['id'], 'relation': 'context'}, {'target_id': correction_source['id'], 'relation': 'supports'}], 'request_id': 'business-correction-1', **admitted}})
            host.tool('memory_review_done', {'scope': scope, 'source_id': correction_source['id'], 'case_ids': [current['id']], 'note': '通过修订保留业务规则及独立模块例外'})
            historical = host.tool('memory_read', {'id': decision['id']})
            assert historical['entry']['body'] == decision['body'] and historical['current_id'] == current['id']
            revised_recall = hook('UserPromptSubmit', 'new-session', prompt='独立业务域新增一级菜单是否允许，详情页面呢？')
            assert current['id'] in revised_recall['hookSpecificOutput']['additionalContext']

            host.tool('memory_record', {'kind': 'case', 'scope': scope, 'title': '数据库迁移约束',
                'body': '存量不强制迁移，数据库新结构应兼容现有数据。', 'origin': 'model_inference', 'links': [{'target_id': raw['id'], 'relation': 'context'}], **admitted})
            migration = hook('PreToolUse', 'new-session', tool_name='Edit', tool_input={'file_path': str(project / 'migrations' / '001_schema.sql')})
            assert '存量不强制迁移' in migration['hookSpecificOutput']['additionalContext']
            assert hook('PreToolUse', 'new-session', tool_name='Edit', tool_input={'file_path': str(project / 'migrations' / '001_schema.sql')}) == {}
            assert host.tool('memory_search', {'scope': 'project:other', 'query': '菜单'})['results'] == []
            hook('UserPromptSubmit', 'ordinary-chat', prompt='谢谢，今天先到这里。')
            recent = host.tool('memory_search', {'scope': scope, 'kind': 'record', 'include_events': True})['results']
            ordinary = next(row for row in recent if host.tool('memory_read', {'id': row['id']})['entry']['body'] == '谢谢，今天先到这里。')
            skipped = host.tool('memory_review_done', {'scope': scope, 'source_id': ordinary['id'], 'note': '普通结束语，不是业务决定'})
            assert skipped['status'] == 'skipped'
            doctor = command('doctor', '--data-dir', str(data), '--config', str(config))
            assert any(e['result'] == 'review_requested' for e in doctor['recent_events'])
            assert any(e['result'] == 'recall_completed' for e in doctor['recent_events'])
        finally:
            host.close()
        assert not list(project.iterdir())
        print('PASS: real Go binary + MCP: raw capture, one review request, business case, new-session recall, correction/history, pre-write recall, skip, isolation, setup preservation.')
        print('Host model choices were simulated. Real ZCode/Windows Stop review and business use remain to be tested.')


if __name__ == '__main__':
    main()
