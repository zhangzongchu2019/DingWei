"""Isolated feasibility probe; two owned listeners, one CODEX_HOME, no turns."""
import datetime,hashlib,json,os,pathlib,subprocess,sys,tempfile,time
ROOT=pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0,str(ROOT/'tools/sessionhelper'))
from sessionhelper.codexDriver import UnixWebSocketJsonRpc
from sessionhelper.daemon_identity import OwnedDaemonIdentity,process_starttime

def main(output):
 out=pathlib.Path(output);out.mkdir(mode=0o700,parents=True,exist_ok=False); rows=[]
 for version in ('0.161.0','0.163.0'):
  binary=pathlib.Path('/tmp/coder-stale-official-followup')/version/'codex'
  prior=json.loads((ROOT/f'docs/reviews/coder-helper-stale-thread-followup-20261022/raw/{version}/PROVENANCE.json').read_text())
  assert hashlib.sha256(binary.read_bytes()).hexdigest()==prior['binary_sha256']
  with tempfile.TemporaryDirectory(prefix='did-sp-') as tmp:
   base=pathlib.Path(tmp); home=base/'home';home.mkdir(mode=0o700); ch=base/'one-codex-home';ch.mkdir(mode=0o700)
   (ch/'config.toml').write_text('model_provider="offline"\n[model_providers.offline]\nname="offline"\nbase_url="http://127.0.0.1:9/v1"\nwire_api="responses"\nrequires_openai_auth=false\n')
   env=dict(HOME=str(home),CODEX_HOME=str(ch),PATH='/usr/bin:/bin',OTEL_SDK_DISABLED='true'); owned=[];clients=[];cleanup=[]
   try:
    for name in ('helper-a','helper-b'):
     folder=base/name;folder.mkdir(mode=0o700);sock=folder/'control.sock'
     stdout=(out/f'{version}-{name}.stdout').open('wb');stderr=(out/f'{version}-{name}.stderr').open('wb')
     proc=subprocess.Popen([str(binary),'app-server','--listen','unix://'+str(sock)],cwd=base,env=env,stdin=subprocess.DEVNULL,stdout=stdout,stderr=stderr)
     owned.append((proc,stdout,stderr,name));deadline=time.monotonic()+12
     while not sock.exists():
      assert proc.poll() is None and time.monotonic()<deadline
      time.sleep(.02)
     client=UnixWebSocketJsonRpc(str(sock),identity_check='enforce',expected_exe=str(binary),owned_identity=OwnedDaemonIdentity(proc.pid,process_starttime(proc.pid)))
     clients.append(client);client.start();client.request('initialize',{'clientInfo':{'name':'dingwei-private-path-probe','version':'0'},'capabilities':{'experimentalApi':True}},timeout=5)
     client.notify('initialized',{})
    assert len({c.peer_identity.pid for c in clients})==2
    assert all(p.poll() is None for p,_,_,_ in owned)
    rows.append(dict(version=version,result='PASS',binary_sha256=prior['binary_sha256'],same_HOME=True,same_CODEX_HOME=True,private_dirs_mode='0700',distinct_socket_paths=True,distinct_peer_pids=True,owned_peer_binding=True,initialize_each=1,model_turns=0,thread_methods=0,live_credentials_read=False,daemon_start_tested=False))
   finally:
    for client in clients: client.stop()
    for proc,stdout,stderr,name in owned:
     if proc.poll() is None:
      proc.terminate()
      try:proc.wait(timeout=8)
      except subprocess.TimeoutExpired:proc.kill();proc.wait(timeout=3)
     stdout.close();stderr.close();cleanup.append(dict(label=name,pid=proc.pid,reaped=proc.poll() is not None))
    (out/f'{version}-cleanup.json').write_text(json.dumps(cleanup,indent=2)+'\n')
 (out/'RESULT.json').write_text(json.dumps(dict(source=subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip(),script_sha256=hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest(),utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),rows=rows),indent=2)+'\n'); print('PRIVATE_LISTENERS_PASS versions=2 listeners_each=2 same_CODEX_HOME no_turns')
if __name__=='__main__':main(sys.argv[1])
