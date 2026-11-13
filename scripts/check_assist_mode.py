#!/usr/bin/env python3
"""Default assist + strict migration with real executables and simulated host events.
This checks mechanics, not native delivery, semantic recall or AGENTS compliance.
"""
import argparse
from contextlib import closing
import hashlib
import json
from pathlib import Path
import re
import sqlite3
import subprocess
import tempfile
import time

from smoke import Host, fixture_profile, supports_modes


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('binary')
    parser.add_argument('--if-supported',action='store_true')
    opt=parser.parse_args(); binary=str(Path(opt.binary).resolve())
    if not supports_modes(binary) and opt.if_supported:
        print('SKIP: historical executable predates assist mode');return
    for adapter in ('zcode','codex','qoder','qoder-ide'):
        with tempfile.TemporaryDirectory(prefix='assist 中文 migration ') as temp:
            root=Path(temp);project=root/'business';project.mkdir()
            other_project=root/'other-business';other_project.mkdir()
            rules=b'Existing project routing: SQL and tests -> develop. Delivery -> existing policy.\n'
            (project/'AGENTS.md').write_bytes(rules)
            user=root/'user';user.mkdir();data=root/'original-library'
            config=user/('config.toml' if adapter=='codex' else 'config.json')
            if adapter=='codex':
                config.write_text('model = "fixture"\n[mcp_servers.unrelated]\ncommand = "unrelated-tool"\n',encoding='utf-8')
                hook_file=user/'hooks.json';hook_file.write_text(json.dumps({'hooks':{'Stop':[{'hooks':[{'type':'command','command':'unrelated-hook'}]}]}}),encoding='utf-8')
            elif adapter=='zcode':
                config.write_text(json.dumps({'mcp':{'servers':{'unrelated':{'command':'unrelated-tool'}}},'hooks':{'events':{'Stop':[{'hooks':[{'type':'process','command':'unrelated-hook','args':[]}]}]}}}),encoding='utf-8')
                hook_file=config
            else:
                config.write_text(json.dumps({'mcpServers':{'unrelated':{'command':'unrelated-tool'}},'hooks':{'Stop':[{'hooks':[{'type':'command','command':'unrelated-hook'}]}]}}),encoding='utf-8');hook_file=config
            scope='project:assist-fixture'
            options=['--host',adapter,'--config',str(config),'--data-dir',str(data),'--project',str(project),'--scope',scope]
            def cli(*args,success=True):
                p=subprocess.run([binary,*args],capture_output=True,text=True,encoding='utf-8',timeout=30)
                assert (p.returncode==0)==success,(args,p.stdout,p.stderr)
                return json.loads(p.stdout) if success else p
            def events():
                c=json.loads(hook_file.read_text(encoding='utf-8'))
                return c['hooks']['events'] if adapter=='zcode' else c['hooks']
            def owned(event):
                found=[]
                for group in events().get(event,[]):
                    for h in group['hooks']:
                        if h['command']!='unrelated-hook':found.append(h)
                return found
            def hook(event,turn='turn1',**fields):
                handler=owned('UserPromptSubmit')[0]
                payload=json.dumps(dict(hook_event_name=event,cwd=str(project),session_id='fixture-session',turn_id=turn,event_id=turn,**fields),ensure_ascii=False)
                command=[handler['command'],*handler['args']] if adapter=='zcode' else handler['command']
                start=time.perf_counter()
                p=subprocess.run(command,shell=adapter!='zcode',cwd=project,input=payload,capture_output=True,text=True,encoding='utf-8',timeout=15)
                assert p.returncode==0,(event,p.stdout,p.stderr)
                return json.loads(p.stdout),round((time.perf_counter()-start)*1000,1)
            strict=cli('setup',*options,'--mode','strict','--capture-user-text')
            assert strict['mode']=='strict'
            out,_=hook('UserPromptSubmit',prompt='Synthetic explicit pending finding, not a production rule')
            sid=re.search(r'本轮用户原文ID=([^。]+)',out['hookSpecificOutput']['additionalContext']).group(1)
            old=Host(binary,str(data),mode='strict')
            try:
                evidence=old.tool('memory_record',dict(kind='record',scope=scope,title='Synthetic evidence',body='Exact fixture evidence 中文🙂',origin='user_supplied'))
                draft=dict(kind='case',scope=scope,title='Fixture earlier decision',body='Verified fixture conditions only',origin='model_inference',profile=fixture_profile(),links=[dict(relation='supports',target_id=evidence['id'])])
                saved=old.tool('memory_record',draft)
                old.tool('memory_context',dict(action='candidate',scope=scope,source_id=sid,save_candidate=True,decision='Explicit unprocessed fixture finding'))
            finally:old.close()
            migrated=cli('setup',*options)  # No --mode: actual new default must be assist.
            assert migrated['mode']=='assist' and not migrated['raw_capture'] and not migrated['context_check']
            assert migrated['mode_prompt_required'] is False
            assert len(owned('SessionStart'))==1 and len(owned('UserPromptSubmit'))==1
            assert not owned('PreToolUse') and not owned('Stop')
            assert 'unrelated-tool' in config.read_text(encoding='utf-8') and 'unrelated-hook' in hook_file.read_text(encoding='utf-8')
            # The mode is service-wide and applies to retained and newly bound projects.
            other_options=list(options);other_options[other_options.index('--project')+1]=str(other_project);other_options[other_options.index('--scope')+1]='project:other-fixture'
            cli('setup',*other_options)
            doctor=cli('doctor',*options)
            assert doctor['binding_verified'] and doctor['context_check_handlers']==0 and doctor['assist_handlers']==2,doctor
            assert doctor['mode_configuration_consistent'] and doctor['memory_services'][0]['mode']=='assist'
            assert not doctor['native_event_verified']
            assert len(doctor['bindings'])==2
            host=Host(binary,str(data))
            try:
                boot=host.tool('memory_bootstrap',{})
                assert boot['mode']=='assist' and boot['integration_protocol']=='3'
                assert 'assist_mode' in boot['features'] and 'before_action_check' not in boot['features']
                pending=lambda:host.tool('memory_context',dict(action='pending',scope=scope)).get('pending_candidates',[])
                assert [x['source_id'] for x in pending()]==[sid], 'migration falsely completed pending work'
                assert host.tool('memory_read',dict(id=saved['id']))['entry']['body']==draft['body']
                def counts():
                    with closing(sqlite3.connect(data/'memory.db')) as db:
                        return tuple(db.execute('select count(*) from '+t).fetchone()[0] for t in ('entries','context_checks','context_reads','memory_candidates'))
                before=counts()
                lookup=host.tool('memory_context',dict(action='lookup',scope=scope,task='Use prior fixture decisions in current project context',questions='Which verified conditions apply?',keywords=['fixture']))
                assert lookup['status']=='candidates' and lookup['next_action']=='read_relevant_evidence_if_needed'
                assert len(json.dumps(lookup,ensure_ascii=False).encode())<=8192
                assert not lookup['source_id'] and counts()==before
                assert host.tool('memory_read',dict(id=saved['id']))['content_role']=='historical_evidence'
                reminder,elapsed=hook('UserPromptSubmit',turn='ordinary',prompt='Continue unchanged fixture task')
                assert len(reminder['hookSpecificOutput']['additionalContext'].encode())<1300
                assert hook('PreToolUse',tool_name='Edit',tool_input={'path':'fixture.go'})[0]=={}
                assert hook('Stop',last_assistant_message='Ordinary unchanged answer')[0]=={}
                assert counts()==before and [x['source_id'] for x in pending()]==[sid]
                new=dict(kind='record',scope=scope,title='Actual fixture feedback',body='Changed fixture condition only',origin='user_supplied')
                source=host.tool('memory_record',new)
                assert source['response_mode']=='compact' and 'body' not in source
                revised=host.tool('memory_revise',dict(previous_id=saved['id'],draft=dict(draft,context_source_id=source['id'],body='Changed verified condition in the disposable fixture')))
                assert revised['recorded_source_id']==source['id'] and not revised.get('checked_context_source_id')
                assert revised['body_sha256']==hashlib.sha256('Changed verified condition in the disposable fixture'.encode()).hexdigest()
                assert not host.tool('memory_read',dict(id=saved['id']))['entry']['current']
                assert host.tool('memory_read',dict(id=revised['id']))['entry']['management']['state']=='active'
                host.tool('memory_record',dict(draft,context_source_id='f'*27),True)
                host.tool('memory_record',dict(draft,links=[dict(relation='supports',target_id='f'*27)]),True)
                host.tool('memory_context',dict(action='candidate',scope=scope,source_id=source['id'],decision='Explicit fixture only',save_candidate=True,review_at_stop=True),True)
                assert [x['source_id'] for x in pending()]==[sid]
                assert (project/'AGENTS.md').read_bytes()==rules
                # Optional capture never restores the strict gate or Stop model review.
                cli('setup',*options,'--mode','assist','--capture-user-text')
                assert len(owned('Stop'))==1 and not owned('PreToolUse')
                captured,_=hook('UserPromptSubmit',turn='explicit-capture',prompt='Explicitly captured disposable source')
                actual_sid=re.search(r'原文ID=([^。]+)',captured['hookSpecificOutput']['additionalContext']).group(1)
                assert host.tool('memory_read',dict(id=actual_sid))['entry']['body']=='Explicitly captured disposable source'
                assert hook('Stop',turn='explicit-capture',last_assistant_message='Exact optional captured answer')[0]=={}
                assert [x['source_id'] for x in pending()]==[sid]
                diag=cli('doctor',*options)
                assert diag['mode_configuration_consistent'] and diag['context_check_handlers']==0
                # Deliberate partial upgrade is diagnosed; configuration success is not runtime proof.
                if adapter=='zcode':
                    c=json.loads(config.read_text(encoding='utf-8'));c['mcp']['servers']['decision-memory']['args'][2]='strict';config.write_text(json.dumps(c),encoding='utf-8')
                    mismatch=cli('doctor',*options)
                    assert not mismatch['mode_configuration_consistent'] and mismatch['mode_mismatches']
                print(f'PASS: {adapter} default assist/migration/persistent scope/reminder-only/pending/evidence/optional capture; simulated prompt process {elapsed}ms')
            finally:host.close()


if __name__=='__main__':main()
