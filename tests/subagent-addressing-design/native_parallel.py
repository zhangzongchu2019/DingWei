"""锁定0.159.2：同一app-server的两个并列thread；仅loopback合成SSE。"""
import argparse
import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import time
from unittest.mock import patch

ROOT=Path(__file__).resolve().parents[2]
sys.path.insert(0,str(ROOT/'tools/sessionhelper'))
from sessionhelper.codexDriver import StdioJsonRpc

SHA='1748767b230ebfc3d4ab7e4e254920d0c0ad9691fd8c11f190e7d44511a4a92e'


def main():
 p=argparse.ArgumentParser();p.add_argument('--binary',required=True);p.add_argument('--output',required=True);args=p.parse_args()
 binary=str(Path(args.binary).resolve());assert hashlib.sha256(Path(binary).read_bytes()).hexdigest()==SHA
 out=Path(args.output).resolve();out.mkdir(parents=True,exist_ok=False)
 release=threading.Event();condition=threading.Condition();hits=[]
 class Handler(BaseHTTPRequestHandler):
  protocol_version='HTTP/1.1'
  def log_message(self,*a):pass
  def do_POST(self):
   count=int(self.headers.get('Content-Length','0'))
   if count>1024*1024:self.send_error(413);return
   if count:self.rfile.read(count)
   with condition:
    hits.append(self.path);number=len(hits);condition.notify_all()
   self.send_response(200);self.send_header('Content-Type','text/event-stream');self.send_header('Connection','close');self.end_headers()
   event={'type':'response.created','response':{'id':'resp_probe_'+str(number),'object':'response','created_at':int(time.time()),'status':'in_progress','model':'probe-local','output':[]}}
   try:
    self.wfile.write(('event: response.created\ndata: '+json.dumps(event)+'\n\n').encode());self.wfile.flush();release.wait(20)
   except (BrokenPipeError,ConnectionResetError):pass
 server=ThreadingHTTPServer(('127.0.0.1',0),Handler);server.daemon_threads=True
 serving=threading.Thread(target=server.serve_forever,daemon=True);serving.start()
 calls=[];rpc=None;result={}
 try:
  with tempfile.TemporaryDirectory(prefix='subagent-native-',dir='/tmp') as tmp:
   ch=Path(tmp)/'codex';ch.mkdir()
   (ch/'config.toml').write_text(f'''model_provider="local_probe"
model="probe-local"
[model_providers.local_probe]
name="local probe"
base_url="http://127.0.0.1:{server.server_port}/v1"
wire_api="responses"
requires_openai_auth=false
supports_websockets=false
request_max_retries=0
stream_max_retries=0
''')
   clean={'HOME':tmp,'CODEX_HOME':str(ch),'PATH':'/usr/bin:/bin','RUST_LOG':'error'}
   with patch.dict(os.environ,clean,clear=True):
    rpc=StdioJsonRpc([binary,'app-server','--stdio'],cwd=tmp,env=clean);rpc.start()
    def call(method,params):
     try:
      response=rpc.request(method,params,timeout=15)
     except Exception as exc:
      calls.append({'method':method,'params':params,'error':type(exc).__name__});raise
     calls.append({'method':method,'params':params,'result':response});return response
    call('initialize',{'clientInfo':{'name':'dingwei_design_probe','version':'0.1'},'capabilities':{'experimentalApi':True}});rpc.notify('initialized')
    threads=[];turns=[]
    for n in range(2):
     thread=call('thread/start',{'cwd':tmp,'ephemeral':False,'sandbox':'read-only','approvalPolicy':'never'})['thread']['id'];threads.append(thread)
     turn=call('turn/start',{'threadId':thread,'clientUserMessageId':'design-'+str(n),'input':[{'type':'text','text':'LOCAL_SYNTHETIC_NO_REAL_INFERENCE_'+str(n)}]})['turn']['id'];turns.append(turn)
    with condition:
     assert condition.wait_for(lambda:len(hits)>=2,timeout=12),'NATIVE_DID_NOT_ISSUE_TWO_PARALLEL_REQUESTS'
    statuses=[]
    for tid,turn in zip(threads,turns):
     page=call('thread/turns/list',{'threadId':tid,'limit':10,'sortDirection':'asc','itemsView':'full'})
     statuses.append(next(t['status'] for t in page['data'] if t['id']==turn))
    assert statuses==['inProgress','inProgress'],'NATIVE_THREADS_NOT_SIMULTANEOUSLY_ACTIVE'
    call('turn/interrupt',{'threadId':threads[0],'turnId':turns[0]})
    call('thread/unsubscribe',{'threadId':threads[0]})
    still=call('thread/turns/list',{'threadId':threads[1],'limit':10,'sortDirection':'asc','itemsView':'full'})
    assert next(t['status'] for t in still['data'] if t['id']==turns[1])=='inProgress','INTERRUPTING_ONE_THREAD_STOPPED_SIBLING'
    call('turn/interrupt',{'threadId':threads[1],'turnId':turns[1]})
    # 仅生成锁版协议schema；没有API key、真实推理或在线daemon。
    schema=Path(tmp)/'schema'
    generated=subprocess.run([binary,'app-server','generate-json-schema','--experimental','--out',str(schema)],env=clean,text=True,capture_output=True,timeout=20)
    assert generated.returncode==0,generated.stderr
    extracts={}
    for path in schema.rglob('*.json'):
     if path.stem in ('ThreadStartParams','DynamicToolCallParams','DynamicToolSpec','TurnStartParams'):
      extracts[str(path.relative_to(schema))]=json.loads(path.read_text())
    (out/'native-schema.json').write_text(json.dumps(extracts,indent=2)+'\n')
    result={'binary_sha256':SHA,'one_app_server':True,'distinct_threads':len(set(threads)),'parallel_native_states':statuses,'loopback_provider_requests':len(hits),'sibling_survives_interrupt_and_unsubscribe':True,'real_inference':False,'schema_extracts':list(extracts)}
    print(json.dumps(result,indent=2))
 finally:
  release.set()
  if rpc is not None:rpc.stop()
  server.shutdown();server.server_close();serving.join(3)
  (out/'native-rpc.json').write_text(json.dumps(calls,indent=2)+'\n')
  (out/'RESULT.json').write_text(json.dumps(result,indent=2)+'\n')


if __name__=='__main__':main()
