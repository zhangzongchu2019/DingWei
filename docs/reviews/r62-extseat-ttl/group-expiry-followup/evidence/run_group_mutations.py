from pathlib import Path
import json,subprocess,hashlib,datetime
A=Path(__file__).parent;K=Path('/data/ai-dev-data/r62-extseat-ttl-knives');S='0b9a10f9227a35eaa01117cb767b9c883e6fb9ec';E=json.loads((A.parent/'env.json').read_text());E['SOURCE_COMMIT']=S
subprocess.run(['git','checkout','--detach',S],cwd=K,check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
rows=[];cuts=[]
def run(n,cmd):
 r={'name':n,'source':S,'command':cmd,'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
 with (A/(n+'.log')).open('wb')as f:r['rc']=subprocess.run(cmd,cwd=K,env=E,stdout=f,stderr=subprocess.STDOUT).returncode
 r.update(ended_utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),sha256=hashlib.sha256((A/(n+'.log')).read_bytes()).hexdigest());rows.append(r);(A/'group-mutation-runs.json').write_text(json.dumps(rows,indent=2)+'\n');return r
cmd=['go','test','./internal/tempgroup','-run','^TestR62ExpiredGroupCannotDeliverUnexpiredQueuedMessagesPG$','-count=1','-p','1','-timeout','10m','-v']
assert run('group-extra-baseline',cmd)['rc']==0
for n,p,b,a,want in [('expired-group-outbox-reason','internal/extseat/outbox.go','return dead("group_unavailable")','return dead("message_expired")','unprojected outbox did not terminate group_unavailable'),('group-finalizer-inbox','internal/extseat/inbox.go',"WHERE ag_code=$1 AND state IN ('queued','leased')","WHERE ag_code=$1 AND false AND state IN ('queued','leased')",'group finalizer did not settle unreachable inbox')]:
 f=K/p;raw=f.read_bytes();assert b in raw.decode();mut=raw.decode().replace(b,a,1).encode()
 try:
  f.write_bytes(mut);cr=run(n+'-compile',['go','test','./internal/tempgroup','-run','^$','-count=1','-p','1']);assert cr['rc']==0
  r=run(n,cmd);s=(A/(n+'.log')).read_text();assert r['rc']!=0 and want in s and 'panic:'not in s and '[build failed]'not in s
 finally:f.write_bytes(raw)
 cuts.append({'name':n,'file':p,'before':b,'after':a,'compile_rc':cr['rc'],'rc':r['rc'],'assertion':want,'original_sha256':hashlib.sha256(raw).hexdigest(),'mutated_sha256':hashlib.sha256(mut).hexdigest(),'restored_sha256':hashlib.sha256(f.read_bytes()).hexdigest(),'log_sha256':r['sha256']})
 (A/'group-mutations.json').write_text(json.dumps(cuts,indent=2)+'\n')
assert run('group-extra-restored',cmd)['rc']==0
assert not subprocess.check_output(['git','status','--porcelain'],cwd=K).strip()
print('group extra 2 mutations business red and restored baseline0')
