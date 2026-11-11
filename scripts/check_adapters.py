#!/usr/bin/env python3
"""Execute installed adapter commands. This simulates event delivery, not a native agent."""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile

from smoke import Host


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('binary')
    parser.add_argument('--if-supported', action='store_true')
    args = parser.parse_args()
    binary = str(Path(args.binary).resolve())
    inventory = subprocess.run([binary, 'adapters'], capture_output=True, text=True, encoding='utf-8')
    if inventory.returncode and args.if_supported:
        print('SKIP: historical version has no adapter inventory')
        return
    assert inventory.returncode == 0, inventory.stderr
    assert {a['name'] for a in json.loads(inventory.stdout)} == {'zcode', 'codex', 'qoder', 'qoder-ide'}
    for adapter in ('codex', 'qoder', 'qoder-ide'):
        with tempfile.TemporaryDirectory(prefix='memory adapters ') as tmp:
            root = Path(tmp)
            project = root / 'project'; project.mkdir()
            user = root / "user's 中文 %PATH% home"; user.mkdir()
            config = user / ('config.toml' if adapter == 'codex' else 'settings.json')
            data = user / 'original data'
            scope = 'project:adapter-test'
            def cli(*cmd):
                p = subprocess.run([binary, *cmd], capture_output=True, text=True, encoding='utf-8', timeout=30)
                assert p.returncode == 0, (cmd, p.stderr, p.stdout)
                return json.loads(p.stdout)
            options = ['--host', adapter, '--config', str(config), '--data-dir', str(data),
                       '--project', str(project), '--scope', scope, '--capture-user-text', '--review-on-stop']
            installed = cli('setup', *options)
            assert installed['binding_verified'] and not installed['host_event_verified']
            hooks = json.loads((user / 'hooks.json' if adapter == 'codex' else config).read_text(encoding='utf-8'))['hooks']
            command = hooks['UserPromptSubmit'][0]['hooks'][0]['command']
            mcp = Host(binary, str(data))
            try:
                def hook(event, turn='t1', prompt='', code=0, **extra):
                    payload = dict(hook_event_name=event, cwd=str(project), session_id='s',
                                   turn_id=turn, prompt=prompt, **extra)
                    p = subprocess.run(command, shell=True, input=json.dumps(payload, ensure_ascii=False),
                                       capture_output=True, text=True, encoding='utf-8', timeout=30, cwd=project)
                    assert p.returncode == code, (adapter, event, p.returncode, p.stdout, p.stderr)
                    return (p.stderr.strip() if code else json.loads(p.stdout))
                hook('SessionStart')
                prompt = '中文原话：只在这个项目保留原数据，不创建空库。'
                out = hook('UserPromptSubmit', prompt=prompt)
                context = out['hookSpecificOutput']['additionalContext']
                sid = re.search(r'本轮用户原文ID=([^。]+)', context).group(1)
                raw = mcp.tool('memory_read', {'id': sid})['entry']
                assert raw['host'] == adapter and raw['body'] == prompt, raw
                blocked = hook('PreToolUse', tool_name='search_replace', tool_input={'path': 'app.go'},
                               code=0 if adapter == 'codex' else 2)
                assert ('memory_context' in blocked if isinstance(blocked, str) else blocked['decision'] == 'block')
                mcp.tool('memory_context', dict(action='prepare', scope=scope, source_id=sid,
                         task='核对当前项目的原数据目录', questions='已有数据目录是否应复用？'))
                mcp.tool('memory_context', dict(action='complete', scope=scope, source_id=sid,
                         selected_ids=[], decision='没有历史案例；当前用户明确要求复用原库，候选留到回合末筛选。', save_candidate=True))
                hook('PreToolUse', tool_name='search_replace', tool_input={'path': 'app.go'})
                stop = hook('Stop', last_assistant_message='业务答复，中文和换行\n保持原样。',
                            code=0 if adapter == 'codex' else 2)
                reason = stop['reason'] if adapter == 'codex' else stop
                assert 'memory_review_done' in reason
                before = cli('status', '--data-dir', str(data))['entries_including_history']
                hook('UserPromptSubmit', turn='synthetic', prompt=reason)
                after = cli('status', '--data-dir', str(data))['entries_including_history']
                assert before == after, 'synthetic continuation captured as a user'
                hook('Stop', turn='synthetic', stop_hook_active=True)
                doctor = cli('doctor', '--host', adapter, '--config', str(config), '--data-dir', str(data),
                             '--project', str(project), '--scope', scope)
                assert doctor['context_check_handlers'] == 4 and doctor['binding_verified'], doctor
                assert doctor['adapter_receipts'] and not doctor['native_event_verified']
                assert not list(project.iterdir())
            finally:
                mcp.close()
        print(f'PASS: {adapter} installed command, UTF-8/quoted paths, block/allow, continuation, diagnostics (simulated host)')


if __name__ == '__main__':
    main()
