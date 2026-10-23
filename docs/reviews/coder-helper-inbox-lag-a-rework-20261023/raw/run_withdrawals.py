from pathlib import Path
import datetime, hashlib, json, subprocess, time
root=Path('/data/ai-dev-data/helper-inbox-lag-a-rework-checks');out=Path('/data/ai-dev-data/helper-inbox-lag-a-rework-evidence/withdrawals');out.mkdir(exist_ok=True)
py='/tmp/coder-0232-locked-venv/bin/python3'
home=out/'home';home.mkdir(exist_ok=True)
env={'HOME':str(home),'PATH':'/tmp/coder-0232-locked-venv/bin:/usr/bin:/bin','TMPDIR':'/data/ai-dev-data/t','GOTMPDIR':'/data/ai-dev-data/tmp-coder-go','GOCACHE':'/home/dev/.cache/go-build','GOMODCACHE':'/home/dev/go/pkg/mod','PYTHONPATH':str(root/'tools/sessionhelper')+':'+str(root/'view-service'),'NODE_PATH':'/tmp/coder-a3-playwright/node_modules','LANG':'C.UTF-8','TZ':'UTC'}
def unittest(name):return [py,'-B','-m','unittest','-v',name]
def gotest(name):return ['go','test','./internal/m8','-run','^'+name+'$','-count=1']
claude='tools.sessionhelper.tests.test_inbox_progress.InboxProgressTest.test_real_claude_submitted_receipt_is_not_admitted_before_replay_ack'
source='tools/sessionhelper/sessionhelper/'
cases=json.loads(Path('/data/ai-dev-data/helper-inbox-lag-a-rework-evidence/WITHDRAWAL-CASES.json').read_text())
records=[]
for name,file,old,new,cmd in cases:
 p=root/file;original=p.read_bytes();s=original.decode();assert s.count(old)==1,(name,s.count(old))
 sha=hashlib.sha256(original).hexdigest();mutant=s.replace(old,new,1).encode();record={'name':name,'source_commit':subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip(),'file':file,'original_sha256':sha,'mutant_sha256':hashlib.sha256(mutant).hexdigest(),'command':cmd,'environment':env,'runs':[]}
 try:
  for phase in ['green-before','withdrawn-red','restored-green']:
   p.write_bytes(mutant if phase=='withdrawn-red' else original)
   start=datetime.datetime.now(datetime.timezone.utc).isoformat();t=time.monotonic()
   r=subprocess.run(cmd,cwd=root,env=env,capture_output=True,timeout=120)
   (out/(name+'-'+phase+'.stdout')).write_bytes(r.stdout);(out/(name+'-'+phase+'.stderr')).write_bytes(r.stderr)
   text=(r.stdout+r.stderr).decode(errors='replace');semantic=('AssertionError' in text or '--- FAIL:' in text)
   record['runs'].append({'phase':phase,'start_utc':start,'end_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'elapsed_seconds':round(time.monotonic()-t,3),'rc':r.returncode,'semantic_assertion_failure':semantic})
   if phase=='withdrawn-red':assert r.returncode!=0 and semantic,(name,phase,text[-2500:])
   else:assert r.returncode==0,(name,phase,text[-2500:])
 finally:p.write_bytes(original)
 assert hashlib.sha256(p.read_bytes()).hexdigest()==sha
 record['restored_sha256']=sha;record['result']='GREEN_RED_GREEN';records.append(record);(out/(name+'.json')).write_text(json.dumps(record,indent=2)+'\n');print(name,'GREEN_RED_GREEN',flush=True)
(out/'SUMMARY.json').write_text(json.dumps(records,indent=2)+'\n')
