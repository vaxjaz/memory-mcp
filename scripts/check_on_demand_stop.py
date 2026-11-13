#!/usr/bin/env python3
"""Exercise default fast Stop and transactional turn accounting in an isolated executable."""
import argparse
import json
from pathlib import Path
import re
import subprocess
import tempfile
import time

from smoke import Host, fixture_profile, mode_args


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('binary')
    parser.add_argument('--if-supported', action='store_true')
    args = parser.parse_args()
    binary = str(Path(args.binary).resolve())
    with tempfile.TemporaryDirectory(prefix='memory demand 中文 ') as tmp:
        root=Path(tmp); data=root/'data'; project=root/'project'; project.mkdir()
        config=root/'zcode.json'; scope='project:demand-fixture'
        host=Host(binary,str(data), mode="strict")
        try:
            if 'on_demand_stop' not in host.tool('memory_bootstrap',{})['features'] and args.if_supported:
                print('SKIP: historical executable has no on-demand Stop')
                return
            p=subprocess.run([binary,'setup',*mode_args(binary),'--host','zcode','--config',str(config),'--data-dir',str(data),
                '--project',str(project),'--scope',scope,'--capture-user-text'],capture_output=True,text=True,encoding='utf-8',timeout=30)
            assert p.returncode==0,p.stderr
            assert json.loads(p.stdout)['review_on_stop'] is False
            hook_args=json.loads(config.read_text(encoding='utf-8'))['hooks']['events']['Stop'][0]['hooks'][0]['args']
            assert '--review-on-stop' not in hook_args
            def hook(event,**fields):
                start=time.perf_counter()
                p=subprocess.run([binary,*hook_args],input=json.dumps(dict(hook_event_name=event,cwd=str(project),session_id='fixture-session',**fields)),
                    capture_output=True,text=True,encoding='utf-8',timeout=15)
                assert p.returncode==0,(p.stdout,p.stderr)
                return json.loads(p.stdout),round((time.perf_counter()-start)*1000,1)
            def start(label):
                out,_=hook('UserPromptSubmit',prompt='Synthetic fixture: '+label,event_id=label)
                return re.search(r'本轮用户原文ID=([^。]+)',out['hookSpecificOutput']['additionalContext']).group(1)
            def prepare(sid,positive=False,at_stop=False):
                host.tool('memory_context',dict(action='prepare',scope=scope,source_id=sid,task='Apply isolated fixture conditions'))
                host.tool('memory_context',dict(action='complete',scope=scope,source_id=sid,decision='Synthetic future-use decision only',save_candidate=positive,review_at_stop=at_stop))
            def pending():
                return host.tool('memory_context',dict(action='pending',scope=scope)).get('pending_candidates',[])
            ordinary=start('ordinary')
            out,ordinary_ms=hook('Stop',last_assistant_message='Unchanged ordinary answer')
            assert out=={} and not pending(), 'ordinary input manufactured a pending task'
            evidence=host.tool('memory_record',dict(kind='record',scope=scope,title='Fixture evidence',body='Verified disposable evidence',origin='user_supplied'))
            sid=start('failed-write')
            prepare(sid,True)
            draft=dict(kind='case',scope=scope,title='Fixture conclusion',body='New future-use finding in disposable fixture',
                origin='model_inference',profile=fixture_profile(),context_source_id=sid,response_mode='compact',
                links=[dict(relation='supports',target_id='f'*27)])
            host.tool('memory_record',draft,True)
            out,pending_ms=hook('Stop',last_assistant_message='Deliver despite unfinished memory candidate')
            assert out=={} and [x['source_id'] for x in pending()]==[sid]
            host.close(); host=Host(binary,str(data), mode="strict")
            state=host.tool('memory_context',dict(action='status',scope=scope,source_id=sid))
            assert state['memory_review']['state']=='pending'
            current=start('meaningful-save')
            prepare(current)
            host.tool('memory_context',dict(action='candidate',scope=scope,source_id=current,save_candidate=True,
                decision='New tested condition changes a future fixture action'))
            draft.update(context_source_id=current,links=[dict(relation='supports',target_id=evidence['id'])])
            saved=host.tool('memory_record',draft)
            assert saved['recorded_source_id']==current and saved['link_count']==1
            read=host.tool('memory_read',dict(id=saved['id']))['entry']
            assert read['links']==draft['links'], 'turn association polluted evidence links'
            assert not host.tool('memory_read',dict(id=current))['referenced_by']
            # The original reported record -> failed receipt -> revise chain is now one write + one successful receipt.
            ack=host.tool('memory_review_done',dict(scope=scope,source_id=current,case_ids=[saved['id']],
                note='One source-bound write; no evidence-link revision required',response_mode='compact'))
            assert ack['status']=='reviewed'
            out,saved_ms=hook('Stop',last_assistant_message='Saved business answer, no replay needed')
            assert out=={} and host.tool('memory_read',dict(id=saved['id']))['entry']['current']
            # Updating an old pending item uses an actual current source, not a fabricated old-turn completion.
            host.tool('memory_context',dict(action='candidate',scope=scope,source_id=current,candidate_source_id=sid,
                save_candidate=False,decision='Compared the real evidence; this earlier fixture candidate needs no separate case'))
            assert not pending()
            assert host.tool('memory_context',dict(action='status',scope=scope,source_id=sid))['memory_review']['state']=='dismissed'
            # Saved before Stop: automatic bookkeeping, no model continuation and no extra case revision.
            auto=start('saved-before-stop'); prepare(auto,True)
            draft.update(context_source_id=auto)
            auto_case=host.tool('memory_record',draft)
            out,automatic_ms=hook('Stop',last_assistant_message='Already saved; finish directly')
            assert out=={} and host.tool('memory_read',dict(id=auto_case['id']))['entry']['current']
            # Explicit per-candidate opt-in still permits exactly one extra model turn.
            explicit=start('explicit-one-review'); prepare(explicit,True,True)
            out,_=hook('Stop',last_assistant_message='Business answer before explicitly requested review')
            assert out.get('decision')=='block'
            again,_=hook('Stop',last_assistant_message='Business answer before explicitly requested review',stop_hook_active=True)
            assert again=={}
            # Case write and candidate completion fail atomically when the source is invalid.
            host.tool('memory_record',dict(draft,context_source_id='0'*32),True)
            assert any(x['source_id']==explicit for x in pending())
            print(json.dumps({'isolated_replay_only':True,'ordinary_stop_ms':ordinary_ms,'pending_stop_ms':pending_ms,
                'saved_stop_ms':saved_ms,'auto_bookkeeping_stop_ms':automatic_ms,'ordinary_or_saved_model_continuations':0}))
        finally:
            host.close()
    print('PASS: default fast Stop, explicit one-review opt-in, durable/withdrawable pending state, separate source bookkeeping and first-attempt receipt (isolated replay)')


if __name__=='__main__':
    main()
