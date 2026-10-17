from pathlib import Path
import subprocess,json,time,hashlib,datetime,sys
lab=Path(__file__).parent;wt=lab/'mutant';env=json.loads((lab/'env.json').read_text());source=subprocess.check_output(['git','rev-parse','HEAD'],cwd=wt,text=True).strip()
def run(label,pattern):
 args=['go','test','-p','1','-race','-json','-count=1','-timeout=15m','-run',pattern,'./internal/hosted']
 log=lab/'raw'/(label+'.log');start=datetime.datetime.now(datetime.timezone.utc).isoformat();at=time.monotonic()
 with log.open('w') as f:p=subprocess.run(args,cwd=wt,env=env,stdout=f,stderr=subprocess.STDOUT)
 rows=[]
 for line in log.read_text().splitlines():
  try:rows.append(json.loads(line))
  except ValueError:pass
 failed=[x.get('Test','PACKAGE') for x in rows if x.get('Action')=='fail'];skips=[x.get('Test','PACKAGE') for x in rows if x.get('Action')=='skip'];passed=[x.get('Test') for x in rows if x.get('Action')=='pass' and x.get('Test')]
 result={'source':source,'argv':args,'rc':p.returncode,'elapsed_seconds':round(time.monotonic()-at,3),'started_utc':start,'finished_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'sha256':hashlib.sha256(log.read_bytes()).hexdigest(),'top_level_pass':len([x for x in passed if '/' not in x]),'subcase_pass':len([x for x in passed if '/' in x]),'failures':failed,'skips':skips}
 (lab/'raw'/(label+'.result.json')).write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result),flush=True);return result
r=run('final-relevant-race',r'^Test(SoftHold|SoftQuota|SoftNode|Quota|Opus)');assert r['rc']==0 and not r['skips']
p=wt/'internal/hosted/node_quota.go';original=p.read_text();old=subprocess.check_output(['git','show','82743cb6:internal/hosted/node_quota.go'],cwd=wt,text=True)
cases=[('original-finding',old,r'^TestOpusReleaseWithUnackedHoldEventConverges$'),('receipt',original.replace('first == 1 &&','first >= 0 &&'),r'^TestSoftHoldLegacyEventStateAppliedOnce$'),('snapshot',original.replace('!hasSnapshot &&','(!hasSnapshot || hasSnapshot) &&'),r'^TestSoftHoldReleaseReplayPreservesCurrentHazards/ordinary_error_first_event$'),('convergence',original.replace("state IN ('stopped','error') AND state_detail='soft_quota_held'","state='stopped' AND state_detail='soft_quota_held'"),r'^TestSoftHoldReleasePendingEventConverges/(success|timeout)/legacy_held_error$')]
results=[]
for name,mut,pattern in cases:
 assert mut!=original
 item={'name':name,'source':source,'original_file_sha256':hashlib.sha256(original.encode()).hexdigest()}
 try:
  for phase in ['baseline','withdrawal','restored']:
   p.write_text(mut if phase=='withdrawal' else original)
   r=run('control-'+name+'-'+phase,pattern);r['file_sha256']=hashlib.sha256(p.read_bytes()).hexdigest();item[phase]=r
   assert r['rc']==(1 if phase=='withdrawal' else 0) and not r['skips']
   if phase=='withdrawal':assert any(x!='PACKAGE' for x in r['failures']),r
 finally:p.write_text(original)
 results.append(item);(lab/('controls-'+name+'.json')).write_text(json.dumps(item,indent=2)+'\n')
(lab/'CONTROLS.json').write_text(json.dumps(results,indent=2)+'\n')
print('REPLAY_GATES_OK',flush=True)
