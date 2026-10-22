import pathlib,subprocess,os,tempfile,time,json,hashlib,stat,sys
from sessionhelper.daemon_identity import verify_peer,OwnedDaemonIdentity,process_starttime
import socket
out=pathlib.Path('/tmp/coder-private-alias-measure-20261022');out.mkdir(mode=0o700,exist_ok=True);rows=[]
for version in ('0.161.0','0.163.0'):
 with tempfile.TemporaryDirectory(prefix='pc-alias-') as t:
  root=pathlib.Path(t);home=root/'h';home.mkdir(mode=0o700);ch=home/'.codex';ch.mkdir(mode=0o700); endpoint=root/'app.sock'
  (ch/'config.toml').write_text('model_provider="offline"\n[model_providers.offline]\nname="offline"\nbase_url="http://127.0.0.1:9/v1"\nwire_api="responses"\nrequires_openai_auth=false\n')
  binary='/tmp/coder-stale-official-followup/'+version+'/codex';env=dict(HOME=str(home),CODEX_HOME=str(ch),PATH='/usr/bin:/bin',OTEL_SDK_DISABLED='true')
  with (out/(version+'.stdout')).open('wb') as stdout,(out/(version+'.stderr')).open('wb') as stderr:
   proc=subprocess.Popen([binary,'app-server','--listen','unix://'+str(endpoint)],env=env,cwd=t,stdin=subprocess.DEVNULL,stdout=stdout,stderr=stderr)
   try:
    until=time.monotonic()+10
    while not endpoint.exists():
     assert proc.poll() is None and time.monotonic()<until;time.sleep(.02)
    info=endpoint.lstat();target=os.readlink(endpoint) if endpoint.is_symlink() else None
    real=endpoint.resolve(strict=True);actual=real.lstat();binding=OwnedDaemonIdentity(proc.pid,process_starttime(proc.pid))
    sock=socket.socket(socket.AF_UNIX);sock.settimeout(3);sock.connect(str(endpoint));verified=verify_peer(sock,binary,binding);sock.close()
    rows.append(dict(version=version,binary_sha256=hashlib.sha256(pathlib.Path(binary).read_bytes()).hexdigest(),requested_endpoint_kind='symlink' if stat.S_ISLNK(info.st_mode) else 'socket',requested_mode=oct(stat.S_IMODE(info.st_mode)),target=target,target_kind='socket' if stat.S_ISSOCK(actual.st_mode) else 'other',target_mode=oct(stat.S_IMODE(actual.st_mode)),target_parent_mode=oct(stat.S_IMODE(real.parent.stat().st_mode)),requested_device=info.st_dev,target_device=actual.st_dev,peer_matches_owned=verified.pid==proc.pid,turns=0))
   finally:
    if proc.poll() is None:proc.terminate();proc.wait(timeout=5)
    rows[-1]['child_reaped']=proc.poll() is not None
(out/'RESULT.json').write_text(json.dumps(rows,indent=2)+'\n');print(json.dumps(rows,indent=2))
