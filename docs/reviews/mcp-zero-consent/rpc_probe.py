"""Ask actual Codex app-server for tool status and a read-only MCP call, no turn/start."""
import argparse,json,os,queue,subprocess,threading,time
from pathlib import Path
ap=argparse.ArgumentParser();ap.add_argument('--binary',required=True);ap.add_argument('--label',required=True);a=ap.parse_args()
run=Path(os.environ.get('INTEROP_RUN_DIR','/tmp/coder-r56-compat-native'));p=run/a.label;ca=str(run/'ca.pem')
env={'PATH':os.environ['PATH'],'HOME':str(p/'home'),'CODEX_HOME':str(p/'codex'),'TMPDIR':str(p/'tmp'),'SSL_CERT_FILE':ca,'CODEX_CA_CERTIFICATE':ca,'BROWSER':'true'}
log=(p/'app-server.stderr').open('wb');proc=subprocess.Popen([a.binary,'app-server'],env=env,cwd=p,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=log,text=True,bufsize=1)
messages=queue.Queue();transcript=[]
def read():
 for line in proc.stdout:
  try:messages.put(json.loads(line))
  except json.JSONDecodeError:messages.put({'unparsed':line})
threading.Thread(target=read,daemon=True).start()
def rpc(ident,method,params):
 request={'id':ident,'method':method,'params':params};transcript.append({'send':request});proc.stdin.write(json.dumps(request)+'\n');proc.stdin.flush();end=time.monotonic()+25
 while time.monotonic()<end:
  try:m=messages.get(timeout=.5)
  except queue.Empty:continue
  transcript.append({'receive':m})
  if m.get('id')==ident:return m
 raise TimeoutError(method)
try:
 rpc(1,'initialize',{'clientInfo':{'name':'isolated-codex-interop','version':'1'},'capabilities':{'experimentalApi':True}})
 proc.stdin.write('{"method":"initialized","params":{}}\n');proc.stdin.flush()
 thread=rpc(2,'thread/start',{'cwd':str(p),'ephemeral':True,'approvalPolicy':'never','sandbox':'read-only','baseInstructions':'Local transport test; no inference or turn/start.'})
 tid=thread.get('result',{}).get('thread',{}).get('id')
 if tid:
  rpc(3,'mcpServerStatus/list',{'threadId':tid,'detail':'full','limit':20})
  rpc(4,'mcpServer/tool/call',{'threadId':tid,'server':'dingwei','tool':'list_online','arguments':{}})
 else:rpc(3,'mcpServerStatus/list',{'detail':'full','limit':20})
finally:
 proc.terminate()
 try:proc.wait(timeout=5)
 except subprocess.TimeoutExpired:proc.kill();proc.wait(timeout=5)
 (p/'rpc.json').write_text(json.dumps({'transcript':transcript,'native_process_exit':proc.returncode,'turn_start_sent':False,'real_credentials_used':False},indent=2)+'\n')
print(json.dumps(transcript,indent=2))
