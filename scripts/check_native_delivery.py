#!/usr/bin/env python3
"""Real Codex native transport trial with a deterministic local model endpoint.
Only synthetic fixture requests are examined; no real transcripts are read.
This verifies hook/result/context plumbing, NOT model understanding/behaviour.
"""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import sys
from contextlib import closing
if hasattr(sys.stdout, 'reconfigure'): sys.stdout.reconfigure(encoding="utf-8")
import tempfile
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


def rpc_write_fixture(binary, data, project, business=False):
    process = subprocess.Popen([binary, 'serve', '--data-dir', str(data)], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, encoding='utf-8')
    def call(msg):
        process.stdin.write(json.dumps(msg, ensure_ascii=False)+'\n'); process.stdin.flush()
        if 'id' not in msg: return
        while True:
            line = process.stdout.readline()
            if not line: raise RuntimeError('MCP exited: '+process.stderr.read())
            value = json.loads(line)
            if value.get('id')==msg['id']: return value
    call({'jsonrpc':'2.0','id':1,'method':'initialize','params':{'protocolVersion':'2025-11-25','capabilities':{},'clientInfo':{'name':'isolated-delivery-fixture','version':'1'}}})
    call({'jsonrpc':'2.0','method':'notifications/initialized'})
    experience = {'anchors':[], 'signals':[], 'pitfall':'合成实验误以为备注变化自动影响日志', 'counterexample':'合成实验中业务备注已更新，日志仍读取独立旧参数。', 'impact':'合成日志可能保留旧文本', 'applicability':'同一合成回馈业务', 'boundary':'条件不同或已修复时可以直接继续，无需修改或登记拒绝', 'evidence':[{'kind':'business','reference':'fixture:local-only','observation':'合成反例，不是真实事故'}], 'cues':[
        {'cue_id':'remark','label':'备注变化','text':'合成实验中备注更新后日志仍保留旧文本。','evidence_refs':['fixture:local-only'],'entrances':[{'kind':'file','value':'RemarkInput.java'},{'kind':'code_symbol','value':'WorkOrder.remark'}]},
        {'cue_id':'log_input','label':'取值入口','text':'合成实验中日志生成读取独立旧参数，备注更新未传入。','evidence_refs':['fixture:local-only'],'entrances':[{'kind':'file','value':'FeedbackLogBuilder.java'},{'kind':'code_symbol','value':'FeedbackLogBuilder.build'}]}]}
    if business:
        experience['object_id_prefixes']=['WXN']
        experience['cues'][1]['entrances'].append({'kind':'business_signal','value':'生成回馈日志'})
    result = call({'jsonrpc':'2.0','id':2,'method':'tools/call','params':{'name':'memory_experience_record','arguments':{'scope':'project:delivery-fixture','project':str(project),'title':'isolated delivery fixture','experience':experience}}})
    if result.get('error') or result.get('result',{}).get('isError'): raise RuntimeError(result)
    process.stdin.close();process.wait(timeout=10)


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--scenario',choices=['success','iserror','lifecycle','business'],default='success');parser.add_argument('--codex',required=True);parser.add_argument('--binary',required=True);parser.add_argument('--out',required=True)
    parser.add_argument('--business-envelope',choices=['json-text','structured'],default='json-text')
    args=parser.parse_args();binary=str(Path(args.binary).resolve());start=time.monotonic()
    with tempfile.TemporaryDirectory(prefix='memory-native-delivery-') as temp:
        root=Path(temp);project=root/'project';project.mkdir();home=root/'home';home.mkdir();data=root/'data'
        (project/'RemarkInput.java').write_text('// isolated fixture WorkOrder.remark\n',encoding='utf-8')
        (project/'FeedbackLogBuilder.java').write_text('// isolated fixture FeedbackLogBuilder.build\n',encoding='utf-8')
        setup=subprocess.run([binary,'setup','--host','codex','--config',str(home/'config.toml'),'--data-dir',str(data),'--project',str(project),'--scope','project:delivery-fixture','--server-name','delivery-fixture'],capture_output=True,text=True,encoding='utf-8')
        if setup.returncode: raise RuntimeError(setup.stderr)
        rpc_write_fixture(binary,data,project,args.scenario=='business')
        if args.scenario=='iserror':
            # Generate a real receipt, then simulate an MCP error before the
            # native host sees the result. No generated receipt may be credited.
            proxy=root/'error_proxy.py'
            proxy.write_text('import json,subprocess,sys\np=subprocess.Popen(sys.argv[1:],stdin=subprocess.PIPE,stdout=subprocess.PIPE)\nfor line in sys.stdin.buffer:\n r=json.loads(line);p.stdin.write(line);p.stdin.flush()\n if "id" not in r:continue\n while True:\n  raw=p.stdout.readline()\n  if not raw:raise RuntimeError("fixture backend closed")\n  value=json.loads(raw)\n  if value.get("id")!=r["id"]:continue\n  if r.get("params",{}).get("name")=="memory_experience_lookup":value["result"]={"isError":True,"content":[{"type":"text","text":"synthetic transport failure after response generation"}]}\n  sys.stdout.buffer.write((json.dumps(value)+"\\n").encode());sys.stdout.buffer.flush();break\n',encoding='utf-8')
            config=home/'config.toml'
            import tomllib
            content=config.read_text();command=tomllib.loads(content)['mcp_servers']['delivery-fixture']
            # setup emitted one command/args pair for this isolated service.
            content=re.sub(r'(?m)^command\s*=.*$', lambda _: 'command='+json.dumps(sys.executable), content)
            content=re.sub(r'(?m)^args\s*=.*$', lambda _: 'args='+json.dumps([str(proxy),binary,*command['args']]), content)
            config.write_text(content,encoding='utf-8')
        # A real read-only MCP fixture avoids depending on nested shell sandbox
        # capabilities. It cannot read outside the two synthetic file names.
        reader=root/'fixture_reader.py'
        reader.write_text('import json,sys\nfrom pathlib import Path\nroot=Path(sys.argv[1]);envelope=sys.argv[2]\nannotations={\'readOnlyHint\':True,\'destructiveHint\':False,\'openWorldHint\':False}\nfor line in sys.stdin:\n r=json.loads(line);method=r.get(\'method\');p=r.get(\'params\',{});result={}\n if \'id\' not in r:continue\n if method==\'initialize\':result={\'protocolVersion\':\'2025-11-25\',\'capabilities\':{\'tools\':{}},\'serverInfo\':{\'name\':\'fixture-files\',\'version\':\'1\'}}\n elif method==\'tools/list\':result={\'tools\':[{\'name\':\'read_file\',\'description\':\'Read one of two isolated synthetic files\',\'annotations\':annotations,\'inputSchema\':{\'type\':\'object\',\'properties\':{\'path\':{\'type\':\'string\'}},\'required\':[\'path\']}},{\'name\':\'query_orders\',\'description\':\'Return three constant synthetic records; no external data access\',\'annotations\':annotations,\'inputSchema\':{\'type\':\'object\',\'properties\':{}}},{\'name\':\'query\',\'description\':\'Inspect a synthetic fixture object by a constant local query; no SQL execution\',\'annotations\':annotations,\'inputSchema\':{\'type\':\'object\',\'properties\':{\'sql\':{\'type\':\'string\'}},\'required\':[\'sql\']}}]}\n elif method==\'tools/call\':\n  name=p[\'name\']\n  if name==\'read_file\':\n   file=p[\'arguments\'][\'path\']\n   if file not in (\'RemarkInput.java\',\'FeedbackLogBuilder.java\'):raise ValueError(\'outside fixture\')\n   result={\'content\':[{\'type\':\'text\',\'text\':(root/file).read_text()}]}\n  elif name==\'query_orders\':\n   rows={\'rows\':[{\'order_no\':\'WXN-001\',\'action\':\'生成回馈日志\'},{\'order_no\':\'WXN-002\',\'action\':\'普通查询\'},{\'order_no\':\'WXN-003\',\'action\':\'生成回馈日志\'}]}\n   result={\'content\':[{\'type\':\'text\',\'text\':json.dumps(rows)}]}\n   if envelope==\'structured\':result[\'structuredContent\']=rows;result[\'content\']=[{\'type\':\'text\',\'text\':\'生成回馈日志 WXN-002\'}]\n  elif name==\'query\':\n   sql=p[\'arguments\'][\'sql\'];object=\'WXN-001\' if "WXN-001" in sql else \'WXN-002\'\n   result={\'content\':[{\'type\':\'text\',\'text\':json.dumps({\'fixture_query_result\':object})}]}\n  else:raise ValueError(\'unknown fixture tool\')\n print(json.dumps({\'jsonrpc\':\'2.0\',\'id\':r[\'id\'],\'result\':result}),flush=True)\n',encoding='utf-8')
        with (home/'config.toml').open('a',encoding='utf-8') as f:
            f.write('\n[mcp_servers.fixture-files]\ncommand='+json.dumps(sys.executable)+'\nargs='+json.dumps([str(reader),str(project),args.business_envelope])+'\n')

        state={'requests':0,'stage':0,'tools':[], 'checks':[], 'challenge':None,'body_seen':False,'cue_seen':False,'material_bytes_by_stage':[]}
        class Handler(BaseHTTPRequestHandler):
            def log_message(self,*a):pass
            def do_GET(self):
                self.send_response(200);self.send_header('Content-Type','application/json');self.end_headers();self.wfile.write(b'{"data":[]}')
            def do_POST(self):
                raw=self.rfile.read(int(self.headers['Content-Length']));body=json.loads(raw)
                if self.path.endswith('/responses/compact'):
                    state['compact_endpoint_requests']=state.get('compact_endpoint_requests',0)+1
                    self.send_response(200);self.send_header('Content-Type','application/json');self.end_headers()
                    self.wfile.write(json.dumps({'id':'cmp_fixture','object':'response.compaction','created_at':1,'output':[{'type':'compaction','encrypted_content':'synthetic-isolated-compaction'}],'usage':{'input_tokens':1,'output_tokens':1,'total_tokens':2}}).encode());return
                if state.get('compacting'):
                    state['compaction_model_requests']=state.get('compaction_model_requests',0)+1
                    self.send_response(200);self.send_header('Content-Type','text/event-stream');self.end_headers()
                    item={'type':'message','id':'summary_fixture','role':'assistant','status':'completed','content':[{'type':'output_text','text':'Synthetic summary: continue the isolated task with current fixture evidence.','annotations':[]}]}
                    response={'id':'summary_response','object':'response','status':'in_progress','model':'fixture','output':[]}
                    events=[{'type':'response.created','response':response},{'type':'response.output_item.added','output_index':0,'item':dict(item,content=[])},{'type':'response.output_text.delta','item_id':item['id'],'output_index':0,'content_index':0,'delta':item['content'][0]['text']},{'type':'response.output_item.done','output_index':0,'item':item},{'type':'response.completed','response':dict(response,status='completed',output=[item],usage={'input_tokens':1,'output_tokens':1,'total_tokens':2})}]
                    for event in events:self.wfile.write(('data: '+json.dumps(event)+'\n\n').encode());self.wfile.flush()
                    return
                state['requests']+=1
                tools=body.get('tools',[])
                names=[]
                for tool in tools:
                    if tool.get('type')=='function':names.append(tool['name'])
                    elif tool.get('type')=='namespace':
                        for f in tool.get('tools',[]):names.append(tool['name']+'.'+f['name'])
                state['tools']=names
                serialized=json.dumps(body,ensure_ascii=False)
                outputs=[i for i in body.get('input',[]) if i.get('type')=='function_call_output']
                state['block_count']=sum('blocked by PreToolUse hook' in json.dumps(i,ensure_ascii=False) for i in outputs)
                state['body_result_count']=sum('"receipt_id"' in json.dumps(i,ensure_ascii=False).replace('\\"','"') for i in outputs)
                state['file_results']=sum('isolated fixture' in json.dumps(i,ensure_ascii=False) for i in outputs)
                state['cue_context_count']=sum('取值入口：合成实验中日志生成' in json.dumps(i,ensure_ascii=False) for i in body.get('input',[]) if i.get('role')=='developer')
                # Only aggregate fixture visibility flags and sizes are persisted.
                state['body_seen'] |= '合成实验中业务备注已更新' in serialized
                state['cue_seen'] |= '取值入口：合成实验中日志生成' in serialized
                matches=re.findall(r'challenge_id=\\?"([a-f0-9]{32})',serialized)
                if matches:state['challenge']=matches[-1]
                material=[]
                for item in body.get('input',[]):
                    encoded=json.dumps(item,ensure_ascii=False)
                    if (item.get('role')=='developer' and '取值入口：合成实验中日志生成' in encoded) or (item.get('type')=='function_call_output' and ('receipt_id' in encoded or 'blocked by PreToolUse hook' in encoded)):
                        material.append(encoded)
                state['material_bytes_by_stage'].append(sum(len(x.encode()) for x in material))
                state['cue_context_bytes']=sum(len(json.dumps(i,ensure_ascii=False).encode()) for i in body.get('input',[]) if i.get('role')=='developer' and '取值入口：合成实验中日志生成' in json.dumps(i,ensure_ascii=False))
                state['lookup_result_bytes']=sum(len(json.dumps(i,ensure_ascii=False).encode()) for i in outputs if 'receipt_id' in json.dumps(i,ensure_ascii=False))
                stage=state['stage'];state['stage']+=1
                lookup=next((n for n in names if n.endswith('memory_experience_lookup')),None)
                read=next((n for n in names if n.endswith('.read_file')),None)
                business_source=next((n for n in names if n.endswith('.query_orders')),None)
                business_query=next((n for n in names if n.endswith('.query')),None)
                if stage==0 or stage==2: target='RemarkInput.java';operation='A' if stage==0 else 'repeat_A'
                elif stage==3 or stage==4: target='FeedbackLogBuilder.java';operation='B' if stage==3 else 'repeat_B'
                else:target=None;operation='lookup' if stage==1 else 'done'
                if args.scenario=='lifecycle' and stage>=6:
                    target='RemarkInput.java' if stage in (6,8,10) else None
                    operation='continued_A' if stage==6 else ('post_compact_A' if stage==8 else ('post_compact_lookup' if stage==9 else ('post_compact_retry' if stage==10 else 'done')))
                if args.scenario=='iserror' and stage>=3:target=None;operation='done'
                if args.scenario=='business' and stage>=3:
                    target=None;operation={3:'business_source',4:'ordinary_object',5:'observed_B',6:'repeat_observed_B'}.get(stage,'done')
                if operation in ('lookup','post_compact_lookup') and state['challenge'] and lookup:
                    name=lookup;arguments={'scope':'project:delivery-fixture','project':str(project),'task':'隔离投递实验中定位备注和日志入口','questions':'合成历史反例与当前入口的条件是否相同','queries':[{'terms':['备注'],'match':'all'}],'challenge_id':state['challenge']}
                elif operation=='business_source' and business_source:
                    name=business_source;arguments={}
                elif operation in ('ordinary_object','observed_B','repeat_observed_B') and business_query:
                    object='WXN-002' if operation=='ordinary_object' else 'WXN-001'
                    alias='WorkOrder.remark' if operation=='observed_B' else ('FeedbackLogBuilder.build' if operation=='repeat_observed_B' else '')
                    name=business_query;arguments={'sql':"select status from fixture where order_no='"+object+"' /* "+alias+" */"}
                elif target and read:
                    name=read;arguments={'path':target}
                else:name=None;arguments=None
                state['checks'].append({'stage':stage,'operation':operation,'native_challenge_seen':bool(state['challenge']),'body_seen':state['body_seen'],'cue_seen':state['cue_seen'],'block_count':state['block_count'],'cue_context_count':state['cue_context_count']})
                self.send_response(200);self.send_header('Content-Type','text/event-stream');self.end_headers()
                def emit(event):self.wfile.write(('data: '+json.dumps(event,ensure_ascii=False)+'\n\n').encode());self.wfile.flush()
                response={'id':f'resp_{stage}','object':'response','status':'in_progress','model':'fixture','output':[]}
                emit({'type':'response.created','response':response})
                if name:
                    item={'type':'function_call','id':f'fc_{stage}','call_id':f'call_{stage}','name':name,'arguments':json.dumps(arguments,ensure_ascii=False)}
                    if '.' in name: item['namespace'],item['name']=name.split('.',1)
                    blank=dict(item,arguments='');emit({'type':'response.output_item.added','output_index':0,'item':blank})
                    emit({'type':'response.function_call_arguments.delta','item_id':item['id'],'output_index':0,'delta':item['arguments']})
                    emit({'type':'response.output_item.done','output_index':0,'item':item})
                else:
                    item={'type':'message','id':f'msg_{stage}','role':'assistant','status':'completed','content':[{'type':'output_text','text':'Synthetic native transport trial complete.','annotations':[]}]}
                    emit({'type':'response.output_item.added','output_index':0,'item':dict(item,content=[])})
                    emit({'type':'response.output_text.delta','item_id':item['id'],'output_index':0,'content_index':0,'delta':item['content'][0]['text']})
                    emit({'type':'response.output_item.done','output_index':0,'item':item})
                response.update(status='completed',output=[item],usage={'input_tokens':1,'output_tokens':1,'total_tokens':2})
                emit({'type':'response.completed','response':response})
        server=ThreadingHTTPServer(('127.0.0.1',0),Handler);threading.Thread(target=server.serve_forever,daemon=True).start()
        config=home/'config.toml'
        with config.open('a',encoding='utf-8') as f:
            f.write(f'\n[model_providers.fixture]\nname="fixture"\nbase_url="http://127.0.0.1:{server.server_port}/v1"\nwire_api="responses"\nrequires_openai_auth=false\n')
        env=dict(os.environ,CODEX_HOME=str(home),OPENAI_API_KEY='synthetic-fixture-only')
        cmd=[args.codex,'exec','--ephemeral','--skip-git-repo-check','--ignore-rules','--dangerously-bypass-hook-trust','-C',str(project),'-c','model_provider="fixture"','-c','model="fixture"','-c','model_reasoning_effort="low"','-c','sandbox_mode="read-only"','-c','approval_policy="never"','--json','Synthetic isolated native hook transport test. Only read the two fixture files and query delivery-fixture memory. Do not write business facts.']
        if args.scenario=='lifecycle':
            import queue,sqlite3
            pipe=subprocess.Popen([args.codex,'--dangerously-bypass-hook-trust','app-server','--stdio','-c','model_provider="fixture"','-c','model="fixture"','-c','model_reasoning_effort="low"','-c','sandbox_mode="read-only"','-c','approval_policy="never"'],env=env,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,encoding='utf-8')
            q=queue.Queue();diagnostics=[];methods=[];counter=[0]
            def readpipe():
                for line in pipe.stdout:
                    try:q.put(json.loads(line))
                    except json.JSONDecodeError:pass
            threading.Thread(target=readpipe,daemon=True).start()
            threading.Thread(target=lambda:diagnostics.extend(pipe.stderr.readlines()),daemon=True).start()
            def send(value):pipe.stdin.write(json.dumps(value,ensure_ascii=False)+'\n');pipe.stdin.flush()
            def take(predicate,timeout=50):
                deadline=time.monotonic()+timeout
                while time.monotonic()<deadline:
                    v=q.get(timeout=max(.1,deadline-time.monotonic()))
                    if 'method' in v:methods.append(v['method'])
                    if 'method' in v and 'id' in v:
                        send({'jsonrpc':'2.0','id':v['id'],'error':{'code':-32601,'message':'Unexpected request in isolated transport trial'}})
                    if predicate(v):return v
                raise TimeoutError('appserver notification timeout')
            def rpc(method,params):
                counter[0]+=1;rid=counter[0];send({'jsonrpc':'2.0','id':rid,'method':method,'params':params})
                out=take(lambda v:v.get('id')==rid)
                if 'error' in out:raise RuntimeError(out['error'])
                return out.get('result',{})
            try:
                rpc('initialize',{'clientInfo':{'name':'isolated-delivery-test','version':'1'},'capabilities':{'experimentalApi':True}})
                send({'jsonrpc':'2.0','method':'initialized'})
                # The app-server build ignores the CLI one-shot bypass flag.
                # Trust ONLY these harness-authored disposable hooks using hashes
                # reported by the native API. Real CODEX_HOME is never modified.
                inventory=rpc('hooks/list',{'cwds':[str(project)]})
                with config.open('a',encoding='utf-8') as f:
                    for entry in inventory['data']:
                        for hook in entry['hooks']:
                            assert Path(hook['sourcePath']).is_relative_to(home)
                            f.write("\n[hooks.state."+json.dumps(hook['key'])+"]\ntrusted_hash="+json.dumps(hook['currentHash'])+"\n")
                thread=rpc('thread/start',{'cwd':str(project),'model':'fixture','modelProvider':'fixture','ephemeral':True,'approvalPolicy':'never','sandbox':'read-only'})['thread']['id']
                def turn(text):
                    rpc('turn/start',{'threadId':thread,'input':[{'type':'text','text':text}]})
                    return take(lambda v:v.get('method')=='turn/completed')
                state['isolated_hook_trust']=[h['trustStatus'] for e in rpc('hooks/list',{'threadId':thread})['data'] for h in e['hooks']]
                turn('Synthetic isolated native transport test; only fixture tools.');state['initial_stage']=state['stage']
                turn('Continue the same synthetic task.');state['continued_stage']=state['stage']
                state['continued_block_count']=state.get('block_count')
                with closing(sqlite3.connect(data/'memory.db')) as db:before=db.execute('select generation from experience_epochs').fetchone()[0]
                state['compacting']=True
                rpc('thread/compact/start',{'threadId':thread})
                take(lambda v:v.get('method')=='thread/compacted' or (v.get('method')=='item/completed' and v.get('params',{}).get('item',{}).get('type')=='contextCompaction'))
                take(lambda v:v.get('method')=='turn/completed')
                state['compacting']=False
                turn('Continue after compact; read the same synthetic entrance.')
                with closing(sqlite3.connect(data/'memory.db')) as db:after=db.execute('select generation from experience_epochs').fetchone()[0]
                state['native_compact_generation_changed']=before!=after
                run=subprocess.CompletedProcess([],0,stdout='',stderr=''.join(diagnostics)[-2000:])
            except Exception as e:
                state['failure']=type(e).__name__+': '+str(e);run=subprocess.CompletedProcess([],1,stdout='',stderr=''.join(diagnostics)[-2000:])
            finally:
                state['appserver_notification_counts']={m:methods.count(m) for m in set(methods)}
                pipe.terminate();pipe.wait(timeout=10)
        else:
            try:run=subprocess.run(cmd,env=env,capture_output=True,text=True,encoding='utf-8',timeout=110)
            except subprocess.TimeoutExpired as e:
                state['failure']='native CLI timeout';run=None
        server.shutdown()
        state['elapsed_seconds']=round(time.monotonic()-start,3)
        if run:
            state['exit_code']=run.returncode
            # Diagnostics are CLI errors only, never full synthetic requests.
            state['diagnostic_tail']=run.stderr[-2000:]
            state['event_types']=[json.loads(line).get('type') for line in run.stdout.splitlines() if line.startswith('{')]
        import sqlite3
        with closing(sqlite3.connect(data/'memory.db')) as db:
            state['native_body_receipts']=db.execute('select count(*) from experience_body_deliveries').fetchone()[0]
            state['generated_receipts']=db.execute('select count(*) from experience_response_receipts').fetchone()[0]
            state['faulted_challenges']=db.execute('select count(*) from experience_challenges where faulted=1').fetchone()[0]
            state['native_cue_records']=db.execute('select count(*) from experience_cue_deliveries').fetchone()[0]
        state['scenario']=args.scenario
        sizes=state['material_bytes_by_stage']
        state['repeat_A_added_memory_bytes']=sizes[3]-sizes[2] if len(sizes)>3 else None
        state['repeat_B_added_memory_bytes']=sizes[5]-sizes[4] if len(sizes)>5 else None
        state['native_transport_pass']=bool(state['challenge'] and state['body_seen'] and state['cue_seen'] and state['stage']>=6 and state.get('block_count')==1 and state.get('file_results')==3 and state['native_body_receipts']==1 and state['native_cue_records']==2)
        if args.scenario=='iserror':
            state['native_failure_event_observed']=state['faulted_challenges']>0
            state['native_transport_pass']=bool(state.get('exit_code')==0 and state['stage']==4 and state['generated_receipts']==1 and state['native_body_receipts']==0 and state['native_cue_records']==0)
            state['failure_boundary']='No false delivery credit. This Codex build emits no matching failure event; the technical deadline, not immediate event release, is the fallback.'
        if args.scenario=='lifecycle':state['native_transport_pass']=bool(state.get('exit_code')==0 and state.get('continued_block_count')==1 and state.get('native_compact_generation_changed') and state['native_body_receipts']==2 and state['stage']==12)
        if args.scenario=='business':
            state['business_envelope']=args.business_envelope
            state['repeat_observed_B_added_memory_bytes']=sizes[7]-sizes[6] if len(sizes)>7 else None
            state['native_transport_pass']=bool(state.get('exit_code')==0 and state['stage']==8 and state['checks'][5]['cue_context_count']==0 and state['checks'][6]['cue_context_count']==1 and state['block_count']==1 and state['native_body_receipts']==1 and state['native_cue_records']==2 and state['repeat_observed_B_added_memory_bytes']==0)
        Path(args.out).write_text(json.dumps(state,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
        print(json.dumps(state,ensure_ascii=False,indent=2))
        if not state['native_transport_pass']:raise SystemExit(1)

if __name__=='__main__':main()
