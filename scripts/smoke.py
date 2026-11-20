#!/usr/bin/env python3
"""Exercise the current packaged MCP executable in an isolated library."""
import argparse,json,queue,subprocess,tempfile,threading
from pathlib import Path

def rejects_cue_text(binary):
 """True when this binary's experience write boundary rejects authored cue
 text. Historical-tag binaries legitimately accept it (pre 0.6.4-dev.2);
 release acceptance scripts pick their assertions accordingly."""
 try: version=subprocess.check_output([str(binary),"version"],text=True,encoding="utf-8").strip()
 except Exception: return True
 parts=version.split("-")
 try: base=[int(x) for x in parts[0].split(".")]
 except ValueError: return True
 if base>[0,6,4]: return True
 if base<[0,6,4]: return False
 if len(parts)>=2 and parts[1].startswith("dev."):
  try: return int(parts[1].split(".")[1])>=2
  except (ValueError,IndexError): return True
 return True

class Host:
 def __init__(self,binary,data_dir):
  self.p=subprocess.Popen([binary,"serve","--data-dir",data_dir],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,encoding="utf-8")
  self.lines=queue.Queue();self.errors=[];self.counter=0
  self.reader=threading.Thread(target=self._read,daemon=True);self.reader.start()
  self.err_reader=threading.Thread(target=lambda:self.errors.extend(self.p.stderr.readlines()),daemon=True);self.err_reader.start()
  self.request("initialize",{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"isolated-test","version":"1"}})
  self.send({"jsonrpc":"2.0","method":"notifications/initialized"})
 def _read(self):
  for line in self.p.stdout:self.lines.put(line)
  self.lines.put(None)
 def send(self,value):
  self.p.stdin.write(json.dumps(value,ensure_ascii=False)+"\n");self.p.stdin.flush()
 def request(self,method,params,accept_error=False):
  self.counter+=1;self.send({"jsonrpc":"2.0","id":self.counter,"method":method,"params":params})
  while True:
   line=self.lines.get(timeout=20);assert line is not None,"server closed"
   out=json.loads(line)
   if out.get("id")==self.counter:
    if accept_error:return out
    assert "error" not in out,out
    return out["result"]
 def tool(self,name,args,expect_error=False):
  out=self.request("tools/call",{"name":name,"arguments":args})
  assert bool(out.get("isError",False))==expect_error,out
  return out if expect_error else json.loads(out["content"][0]["text"])
 def close(self):
  self.p.stdin.close();code=self.p.wait(timeout=10);self.reader.join(timeout=1);self.err_reader.join(timeout=1);self.p.stdout.close();self.p.stderr.close();assert code==0,"".join(self.errors)
def main():
 p=argparse.ArgumentParser();p.add_argument("binary");p.add_argument("--expected-version");a=p.parse_args();binary=str(Path(a.binary).resolve())
 if a.expected_version:assert subprocess.check_output([binary,"version"],text=True).strip()==a.expected_version
 with tempfile.TemporaryDirectory(prefix="experience-smoke-") as tmp:
  h=Host(binary,str(Path(tmp)/"data"))
  try:
   tools=h.request("tools/list",{})["tools"];names={x["name"] for x in tools}
   assert names=={"memory_bootstrap","memory_experience_record","memory_reference_record","memory_experience_lookup","memory_search","memory_read","memory_cleanup"},names
   boot=h.tool("memory_bootstrap",{});assert boot["workflow"]=="verified_experience" and boot["integration_protocol"]=="12" and "cue_match_when" in boot["features"] and "experience_write_admission" in boot["features"] and "challenge_status_diagnostics" in boot["features"] and "legacy_history_cursor" in boot["features"] and "experience_body_forms" in boot["features"] and "body_derived_cue_facts" in boot["features"]
   assert not any("mode" in f or "reminder" in f or "checkpoint" in f for f in boot["features"])
   guide=boot["guide"].lower();assert "a native userpromptsubmit hook supplies a short reminder" not in guide
   page=h.tool("memory_search",{"scope":"project:fixture","query":""});assert not page["results"]
   for old in ["memory_record","memory_revise","memory_context","memory_review_done"]:
    out=h.request("tools/call",{"name":old,"arguments":{}},accept_error=True)
    assert "error" in out or out.get("result",{}).get("isError"),out
  finally:h.close()
 for flag in ["--mode","--capture-user-text","--context-check","--review-on-stop"]:
  command=[binary,"serve",flag]+(["assist"] if flag=="--mode" else [])
  p=subprocess.run(command,capture_output=True,text=True,timeout=10)
  assert p.returncode!=0,(flag,p.stdout,p.stderr)
 print("PASS: exact seven-tool catalog, old tools/flags rejected, single workflow bootstrap and historical access")
if __name__=="__main__":main()
