from pathlib import Path
import subprocess,json,hashlib,os,datetime,time
root=Path('/data/ai-dev-data/d2-wake-agent-id-20261026-withdraw');out=Path('/data/ai-dev-data/d2-wake-agent-id-20261026-evidence')
app=root/'tools/sessionhelper/sessionhelper/app.py';hub=root/'internal/m8/agent_ws.go'
original={app:app.read_text(),hub:hub.read_text()}
env={'HOME':str(out/'withdraw-home'),'PATH':'/tmp/coder-0232-locked-venv/bin:/usr/bin:/bin','TMPDIR':'/data/ai-dev-data/t','GOTMPDIR':'/data/ai-dev-data/tmp-coder-go','GOCACHE':'/home/dev/.cache/go-build','GOMODCACHE':'/home/dev/go/pkg/mod','PYTHONPATH':str(root/'tools/sessionhelper'),'PYTHONDONTWRITEBYTECODE':'1','LANG':'C.UTF-8','TZ':'UTC','HUB_TEST_PG_STRICT':'1','HUB_TEST_PG_SKIP_OK':'0','HUB_TEST_PG_EMBEDDED':'1'}
Path(env['HOME']).mkdir(exist_ok=True)
py=['/tmp/coder-0232-locked-venv/bin/python3','-B','-m','unittest','tools.sessionhelper.tests.test_task_blocker.BlockerCredentialIdentityTest','-v']
go=['/usr/bin/go','test','-p','1','-count=1','./internal/m8','-run','^TestAgentWSAdvertisesTrustedHelperUpdateSource$','-v']
records=[]
def run(name,cmd):
 start=datetime.datetime.now(datetime.timezone.utc).isoformat()
 with (out/(name+'.stdout')).open('wb') as so,(out/(name+'.stderr')).open('wb') as se:
  r=subprocess.run(cmd,cwd=root,env=env,stdout=so,stderr=se,timeout=300)
 rec={'name':name,'command':cmd,'start_utc':start,'end_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'rc':r.returncode,'app_sha256':hashlib.sha256(app.read_bytes()).hexdigest(),'hub_sha256':hashlib.sha256(hub.read_bytes()).hexdigest()}
 records.append(rec);(out/(name+'.json')).write_text(json.dumps(rec,indent=2)+'\n');print(json.dumps(rec),flush=True);return r.returncode
try:
 assert run('withdraw-python-before',py)==0
 assert run('withdraw-hub-before',go)==0
 mutations=[('withdraw-worker-credential-agent-id',app,'safe,reason=await asyncio.to_thread(self._prepare_blocker_driver_wake,env)','ws_url,headers=self._connection()\n                        safe,reason=await asyncio.to_thread(prepare_wake,ws_url,headers,credential.agent_id,env)'),('withdraw-driver-credential-agent-id',app,'return prepare_wake(ws_url,headers,agent_id,env)','return prepare_wake(ws_url,headers,credential.agent_id,env)'),('withdraw-missing-identity-guard',app,'if not agent_id:','if False:'),('withdraw-trusted-header',hub,'\tw.Header().Set("X-DW-Agent-ID", ag.ID)','\t// withdrawn identity header')]
 for name,path,old,new in mutations:
  text=original[path]
  assert text.count(old)==1,(name,text.count(old))
  if name=='withdraw-worker-credential-agent-id':text=text.replace('credential=getattr(self,"enroll_credential",None)\n                    if self.cfg.mode', 'from .task_blocker import prepare_wake\n                    credential=getattr(self,"enroll_credential",None)\n                    if self.cfg.mode')
  path.write_text(text.replace(old,new))
  assert run(name,go if path==hub else py)!=0,name
  path.write_text(original[path])
  assert run(name+'-restored',go if path==hub else py)==0,name
finally:
 for p,s in original.items():p.write_text(s)
 (out/'withdraw-summary.json').write_text(json.dumps({'records':records,'restored':all(p.read_text()==s for p,s in original.items())},indent=2)+'\n')
