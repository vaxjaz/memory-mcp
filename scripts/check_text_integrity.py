#!/usr/bin/env python3
"""Exercise real MCP bytes: split UTF-8, bad UTF-8, surrogates, body and review note."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import queue
import subprocess
import tempfile
import threading
from smoke import fixture_profile, mode_args


class Wire:
    def __init__(self, binary, data, env):
        self.proc = subprocess.Popen([binary, 'serve', '--data-dir', str(data), *mode_args(binary)], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env)
        self.lines = queue.Queue()
        def read():
            for line in self.proc.stdout:
                self.lines.put(line)
            self.lines.put(None)
        self.reader = threading.Thread(target=read, daemon=True)
        self.reader.start()
        self.seq = 0
        self.request('initialize', {'protocolVersion': '2024-11-05', 'capabilities': {}, 'clientInfo': {'name': 'text-integrity-test', 'version': '1'}})
        self.proc.stdin.write(b'{"jsonrpc":"2.0","method":"notifications/initialized"}\n')
        self.proc.stdin.flush()

    def request(self, method, params, transform=None, fragmented=False):
        self.seq += 1
        frame = json.dumps({'jsonrpc': '2.0', 'id': self.seq, 'method': method, 'params': params}, ensure_ascii=False).encode('utf-8')
        if transform:
            frame = transform(frame)
        payload = frame + b'\n'
        step = 7 if fragmented else len(payload)
        for offset in range(0, len(payload), step):
            self.proc.stdin.write(payload[offset:offset + step])
            self.proc.stdin.flush()
        line = self.lines.get(timeout=30)
        assert line is not None, 'Server closed the connection'
        result = json.loads(line.decode('utf-8', errors='strict'))
        assert result['id'] == self.seq, result
        assert 'error' not in result, result
        return result['result']

    def tool(self, name, args, *, error=False, transform=None, fragmented=False):
        result = self.request('tools/call', {'name': name, 'arguments': args}, transform=transform, fragmented=fragmented)
        assert bool(result.get('isError')) == error, result
        if error:
            return result
        text = json.loads(result['content'][0]['text'])
        assert result.get('structuredContent', text) == text
        return text

    def close(self):
        self.proc.stdin.close()
        self.proc.wait(timeout=10)
        assert self.proc.returncode == 0, self.proc.stderr.read().decode('utf-8', errors='replace')
        self.reader.join(timeout=1)
        self.proc.stdout.close()
        self.proc.stderr.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('binary')
    parser.add_argument('--if-supported', action='store_true')
    opt = parser.parse_args()
    binary = str(Path(opt.binary).resolve())
    with tempfile.TemporaryDirectory(prefix='memory-text-integrity-') as tmp:
        root = Path(tmp)
        home, project, data = root / 'user', root / 'project', root / 'memory'
        home.mkdir(); project.mkdir()
        env = dict(os.environ, HOME=str(home), USERPROFILE=str(home))
        config = home / '.zcode' / 'cli' / 'config.json'
        wire = Wire(binary, data, env)
        try:
            boot = wire.tool('memory_bootstrap', {})
            if opt.if_supported and 'strict_text_integrity' not in boot.get('features', []):
                print('SKIP: historical release predates strict text integrity')
                return
            scope = 'project:encoding-test'
            draft = {'kind': 'case', 'scope': scope, 'title': '完整中文', 'body': ('关键信息、助手、同级——、须追到、用户未表态😀\n' * 300) + '末尾两个空格  ', 'origin': 'model_inference', 'request_id': 'unicode-fragmented'}
            if 'quality_admission' in boot.get('features', []):
                source = wire.tool('memory_record', {'kind': 'record', 'scope': scope, 'title': '编码测试原材料', 'body': draft['body'], 'origin': 'imported'})
                draft['profile'] = fixture_profile()
                draft['links'] = [{'target_id': source['id'], 'relation': 'supports'}]
            good = wire.tool('memory_record', draft, fragmented=True)
            assert good['body'] == draft['body']
            assert good['body_bytes'] == len(draft['body'].encode('utf-8'))
            assert good['body_sha256'] == hashlib.sha256(draft['body'].encode('utf-8')).hexdigest()
            assert wire.tool('memory_read', {'id': good['id']})['entry']['body'] == draft['body']
            before = len(wire.tool('memory_search', {'scope': scope})['results'])
            for damaged in ['关键信���', '��手', '同级���—', '须��到', '用��未表态']:
                bad = dict(draft, body=damaged, request_id='bad-' + str(len(damaged)) + damaged)
                wire.tool('memory_record', bad, error=True)
            sentinel = dict(draft, body='ENCODING_SENTINEL', request_id='wire-bad-utf8')
            damaged_bytes = wire.tool('memory_record', sentinel, error=True, transform=lambda b: b.replace(b'ENCODING_SENTINEL', b'\xe4\xbf'))
            assert 'before tool decoding' in damaged_bytes['content'][0]['text']
            for escaped in [b'\\ud800', b'\\udc00', b'\\ud800\\u0041']:
                invalid_surrogate = wire.tool('memory_record', dict(sentinel, request_id='surrogate-' + escaped.decode()), error=True, transform=lambda b, e=escaped: b.replace(b'ENCODING_SENTINEL', e))
                assert 'before tool decoding' in invalid_surrogate['content'][0]['text']
            assert len(wire.tool('memory_search', {'scope': scope})['results']) == before
            revision = dict(draft, body='须��到', request_id='unicode-revision')
            wire.tool('memory_revise', {'previous_id': good['id'], 'draft': revision}, error=True)
            unchanged = wire.tool('memory_read', {'id': good['id']})
            assert unchanged['current_id'] == good['id']
            repaired_text = '从原始材料重新核对后的完整信息，助手没有替用户确认——保留历史。'
            revised = wire.tool('memory_revise', {'previous_id': good['id'], 'draft': dict(revision, body=repaired_text)})
            assert revised['body'] == repaired_text
            history = wire.tool('memory_read', {'id': good['id']})
            assert history['entry']['body'] == draft['body'] and history['current_id'] == revised['id']
            assert wire.tool('memory_revise', {'previous_id': good['id'], 'draft': dict(revision, body=repaired_text)})['id'] == revised['id']
            fixed = wire.tool('memory_record', dict(sentinel, body='从原文重传的信息'))
            assert fixed['body'] == '从原文重传的信息'
            literal = wire.tool('memory_record', dict(draft, body='有意记录替换字符 �', request_id='literal-replacement', allow_replacement_characters=True))
            assert literal['text_issues'][0]['kind'] == 'replacement_character'
            assert wire.tool('memory_record', dict(draft, body='字面反斜杠序列 \\ud800', request_id='literal-escape'))['body'].endswith('\\ud800')

            subprocess.run([binary, 'setup', *mode_args(binary), '--host', 'zcode', '--config', str(config), '--data-dir', str(data), '--project', str(project), '--scope', scope, '--capture-user-text', '--review-on-stop'], env=env, check=True, capture_output=True)
            def source(session):
                event = {'hook_event_name': 'UserPromptSubmit', 'session_id': session, 'cwd': str(project), 'prompt': '这是中文回执校验 ' + session}
                subprocess.run([binary, 'hook', *mode_args(binary), '--host', 'zcode', '--data-dir', str(data), '--capture-user-text'], input=json.dumps(event, ensure_ascii=False).encode('utf-8'), env=env, check=True, capture_output=True)
                rows = wire.tool('memory_search', {'scope': scope, 'kind': 'record', 'include_events': True})['results']
                return next(r['id'] for r in rows if wire.tool('memory_read', {'id': r['id']})['entry']['session'] == session)
            raw_id = source('note-test')
            note = {'scope': scope, 'source_id': raw_id, 'note': '助手推断、用��未表态'}
            wire.tool('memory_review_done', note, error=True)
            wire.tool('memory_review_done', dict(note, note='ENCODING_SENTINEL'), error=True, transform=lambda b: b.replace(b'ENCODING_SENTINEL', b'\xe6\x88'))
            intact = '助手推断，用户未表态；须追到原文——保留完整信息😀。'
            result = wire.tool('memory_review_done', dict(note, note=intact), fragmented=True)
            assert result['status'] == 'skipped'
            assert result['review_note'] == intact
            assert result['review_note_bytes'] == len(intact.encode('utf-8'))
            assert result['review_note_sha256'] == hashlib.sha256(intact.encode('utf-8')).hexdigest()
            audit = json.loads(subprocess.check_output([binary, 'doctor', '--config', str(config), '--data-dir', str(data), '--check-text'], env=env))['text_integrity']
            assert literal['id'] in [f['id'] for f in audit['findings']]
            assert good['id'] not in [f['id'] for f in audit['findings']]
        finally:
            wire.close()
        wire = Wire(binary, data, env)
        try:
            assert wire.tool('memory_read', {'id': good['id']})['entry']['body'] == draft['body']
        finally:
            wire.close()
        print('PASS: exact Chinese/emoji bytes across fragmented stdio and restart; malformed UTF-8/surrogates and damaged body/note rejected without writes; note receipts and read-only audit verified.')


if __name__ == '__main__':
    main()
