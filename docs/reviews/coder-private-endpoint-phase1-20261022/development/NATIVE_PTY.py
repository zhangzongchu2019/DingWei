"""Independent locked-native capability probe, not approval of product alias policy."""
import hashlib,json,os,pathlib,socket,subprocess,sys,tempfile,time
import pexpect
from sessionhelper.codexDriver import UnixWebSocketJsonRpc
from sessionhelper.daemon_identity import OwnedDaemonIdentity,process_starttime
version=sys.argv[1];out=pathlib.Path(sys.argv[2]);out.mkdir(mode=0o700,parents=True,exist_ok=False)
root=pathlib.Path(tempfile.mkdtemp(prefix='dwpe-pty-'));root.chmod(0o700)
home=root/'h';home.mkdir(mode=0o700);ch=home/'ch';ch.mkdir(mode=0o700);app=root/'app.sock'
binary='/tmp/coder-stale-official-followup/'+version+'/codex'
expected={'0.161.0':'9a820c17865fa825d04db416818679a9d63bd72e50835c396f496e5684626c9c','0.163.0':'399ff69c431c066e40de318fba45c2b07f45a76b19939d247ee4828fc70c4215'}
assert hashlib.sha256(pathlib.Path(binary).read_bytes()).hexdigest()==expected[version]
config='''model_provider="local_probe"
model="probe-no-inference"
check_for_update_on_startup=false
[model_providers.local_probe]
name="local probe"
base_url="http://127.0.0.1:9/v1"
wire_api="responses"
requires_openai_auth=false
supports_websockets=false
request_max_retries=0
stream_max_retries=0
'''+ '\n[projects.'+json.dumps(str(root))+']\ntrust_level="trusted"\n'
(ch/'config.toml').write_text(config)
env={'HOME':str(home),'CODEX_HOME':str(ch),'PATH':'/usr/bin:/bin','TERM':'xterm-256color','LANG':'C.UTF-8','RUST_LOG':'error','OTEL_SDK_DISABLED':'true'}
serverlog=(out/'server.stderr').open('wb');proc=subprocess.Popen([binary,'app-server','--listen','unix://'+str(app)],env=env,cwd=root,stdin=subprocess.DEVNULL,stdout=subprocess.DEVNULL,stderr=serverlog)
rpc=None;tui=None;result={'scope':'NATIVE_CAPABILITY_ONLY_NOT_PRODUCT_GATE','version':version,'binary_sha256':expected[version],'turn_start_calls':0}
try:
 until=time.monotonic()+10
 while not app.exists():
  assert proc.poll() is None and time.monotonic()<until,'native app unavailable';time.sleep(.02)
 rpc=UnixWebSocketJsonRpc(str(app),identity_check='enforce',expected_exe=binary,owned_identity=OwnedDaemonIdentity(proc.pid,process_starttime(proc.pid)))
 rpc.start();rpc.request('initialize',{'clientInfo':{'name':'private-native-pty-test','version':'1'},'capabilities':{'experimentalApi':True}},timeout=5);rpc.notify('initialized')
 thread=rpc.request('thread/start',{'cwd':str(root),'ephemeral':False,'sandbox':'read-only','approvalPolicy':'never'},timeout=5)['thread'];tid=thread['id']
 result['thread_id']=tid;result['owned_pid']=proc.pid
 # One explicitly local failed inference materializes the real rollout.
 # The configured sole provider is loopback port 9, with retries disabled.
 rpc.request('turn/start',{'threadId':tid,'input':[{'type':'text','text':'LOCAL_PTY_PERSISTENCE_FIXTURE'}]},timeout=5)
 result['turn_start_calls']=1
 rollout=pathlib.Path(thread['path']);until=time.monotonic()+10
 while not (rollout.exists() and rollout.stat().st_size>0):
  assert time.monotonic()<until,'local rollout not materialized';time.sleep(.05)
 result['materialized_status']=rpc.request('thread/read',{'threadId':tid,'includeTurns':False},timeout=5)['thread']['status']
 rpc.stop();rpc=None
 proc.terminate()
 try:proc.wait(timeout=8)
 except subprocess.TimeoutExpired:proc.kill();proc.wait(timeout=5);result['fixture_restart_escalated']=True
 result['first_child_reaped']=True
 proc=subprocess.Popen([binary,'app-server','--listen','unix://'+str(app)],env=env,cwd=root,stdin=subprocess.DEVNULL,stdout=subprocess.DEVNULL,stderr=serverlog)
 until=time.monotonic()+10
 while True:
  try:
   rpc=UnixWebSocketJsonRpc(str(app),identity_check='enforce',expected_exe=binary,owned_identity=OwnedDaemonIdentity(proc.pid,process_starttime(proc.pid)))
   rpc.start();break
  except Exception:
   if rpc is not None:rpc.stop();rpc=None
   assert proc.poll() is None and time.monotonic()<until,'restart unavailable';time.sleep(.03)
 rpc.request('initialize',{'clientInfo':{'name':'private-native-pty-test','version':'1'},'capabilities':{'experimentalApi':True}},timeout=5);rpc.notify('initialized')
 resumed=rpc.request('thread/resume',{'threadId':tid,'excludeTurns':True},timeout=5)['thread']
 assert resumed['id']==tid,'restart changed thread'
 result['owned_pid_after_restart']=proc.pid
 result['loaded_before']=rpc.request('thread/loaded/list',{'limit':4096},timeout=5)
 result['read_before']=rpc.request('thread/read',{'threadId':tid,'includeTurns':False},timeout=5)['thread']['status']
 tui=pexpect.spawn(binary,['--remote','unix://'+str(app),'resume',tid],cwd=str(root),env=env,encoding='utf-8',codec_errors='replace',timeout=1,dimensions=(36,120))
 with (out/'pty.txt').open('w') as log:
  tui.logfile_read=log;until=time.monotonic()+25;seen=False
  while time.monotonic()<until:
   match=tui.expect(['\x1b\\[6n', '\x1b\\[c', 'probe-no-inference', 'LOCAL_PTY_PERSISTENCE_FIXTURE', pexpect.EOF, pexpect.TIMEOUT],timeout=1)
   if match==0:tui.send('\x1b[1;1R')
   elif match==1:tui.send('\x1b[?1;2c')
   elif match in (2,3):seen=True;time.sleep(1);break
   elif match==4:break
  result['pty_ready_marker']=seen;result['tui_alive_before_close']=tui.isalive()
  result['read_during']=rpc.request('thread/read',{'threadId':tid,'includeTurns':False},timeout=5)['thread']['status']
  tui.sendcontrol('c');time.sleep(.3)
  if tui.isalive():tui.sendcontrol('c')
  try:tui.expect(pexpect.EOF,timeout=5)
  except pexpect.TIMEOUT:tui.terminate(force=False);tui.expect(pexpect.EOF,timeout=5)
  tui.close();result['tui_exitstatus']=tui.exitstatus;result['tui_signalstatus']=tui.signalstatus
 result['same_native_peer_after']=rpc.peer_identity.pid==proc.pid and rpc.healthy()
 result['read_after']=rpc.request('thread/read',{'threadId':tid,'includeTurns':False},timeout=5)['thread']['status']
 result['loaded_after']=rpc.request('thread/loaded/list',{'limit':4096},timeout=5)
 result['pty_contains_same_resume_id']=tid in (out/'pty.txt').read_text()
 assert seen and result['tui_alive_before_close'] and result['same_native_peer_after'] and result['tui_exitstatus']==0,'real PTY not established'
 print(json.dumps(result,indent=2))
finally:
 if tui is not None and tui.isalive():tui.terminate(force=True);tui.close()
 if rpc is not None:rpc.stop()
 if proc.poll() is None:
  proc.terminate()
  try:proc.wait(timeout=8)
  except subprocess.TimeoutExpired:proc.kill();proc.wait(timeout=5);result['fixture_final_cleanup_escalated']=True
 serverlog.close();result['child_reaped']=proc.poll() is not None
 (out/'RESULT.json').write_text(json.dumps(result,indent=2)+'\n')
