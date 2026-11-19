import datetime,hashlib,json,subprocess
from pathlib import Path
ROOT=Path('/data/ai-dev-data/wt-r62-extseat-groups-mutations')
OUT=Path(__file__).resolve().parent/'mutations';OUT.mkdir(exist_ok=True)
ENV=json.loads((OUT.parent/'env-aaed8aaee.json').read_text())
FILE=ROOT/'internal/identity/communication.go';ORIGINAL=FILE.read_bytes();SOURCE=ENV['SOURCE_COMMIT']
MUTATIONS=[
 ('M1-target-membership', 'JOIN np_group_member t ON t.code=g.code AND t.agent_id=$2', "JOIN np_group_member t ON t.code=g.code AND $2::text<>''", 'TestExternalGroupsOwnGroupDoesNotAuthorizeDifferentTargetPG'),
 ('M2-explicit-route', '(choice.Mode == "direct" || choice.Ref == ref)', 'true', 'TestExternalGroupsOrdinaryAndExplicitIsolationPG'),
 ('M3-ungrouped-source', 'JOIN np_group_member s ON s.code=g.code AND s.agent_id=$1', "JOIN np_group_member s ON s.code=g.code AND $1::text<>''", 'TestExternalGroupsNoPairNoGroupAndExternalPeerPG'),
 ('M4-owner-admin', "(a.owner_lc=$2 OR actor.type='platform_admin') AND s.revoked_at", 'true AND s.revoked_at', 'TestExternalGroupsControlledMembershipAndRollbackPG'),
]
def run(name,args):
 start=datetime.datetime.now(datetime.timezone.utc).isoformat()
 path=OUT/(name+'.log')
 with path.open('w') as log:p=subprocess.run(args,cwd=ROOT,env=ENV,stdout=log,stderr=subprocess.STDOUT)
 return {'rc':p.returncode,'command':args,'start_utc':start,'end_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'log':path.name,'log_sha256':hashlib.sha256(path.read_bytes()).hexdigest()}
results={'source':SOURCE,'original_sha256':hashlib.sha256(ORIGINAL).hexdigest(),'baseline':run('baseline',['go','test','./internal/extseat','-run','TestExternalGroups','-count=1','-v']),'mutations':[]}
try:
 if results['baseline']['rc']!=0:raise RuntimeError('mutation baseline not green')
 for name,old,new,test in MUTATIONS:
  original=ORIGINAL.decode()
  if original.count(old)!=1:raise RuntimeError('ambiguous mutation anchor '+name)
  try:
   FILE.write_text(original.replace(old,new,1))
   build=run(name+'-build',['go','test','./internal/extseat','-run','^$','-count=1'])
   reading=run(name,['go','test','./internal/extseat','-run','^'+test+'$','-count=1','-v']) if build['rc']==0 else None
   entry={'id':name,'path':str(FILE.relative_to(ROOT)),'old':old,'new':new,'test':test,'build':build,'reading':reading}
   if reading:
    text=(OUT/reading['log']).read_text();entry['failed_assertions']=[s for s in text.splitlines() if 'group_routing_pg_test.go:' in s];entry['business_red']=reading['rc']!=0 and bool(entry['failed_assertions']) and 'panic:' not in text and 'SQLSTATE' not in text and 'expected 1 arguments' not in text
   results['mutations'].append(entry)
  finally:FILE.write_bytes(ORIGINAL)
 results['final_baseline']=run('final-baseline',['go','test','./internal/extseat','-run','TestExternalGroups','-count=1','-v'])
finally:
 FILE.write_bytes(ORIGINAL);results['restored_sha256']=hashlib.sha256(FILE.read_bytes()).hexdigest();results['porcelain']=subprocess.check_output(['git','status','--porcelain'],cwd=ROOT,text=True)
 (OUT/'summary.json').write_text(json.dumps(results,ensure_ascii=False,indent=2)+'\n')
print(json.dumps({'baseline_rc':results['baseline']['rc'],'mutations':[(x['id'],x.get('business_red',False)) for x in results['mutations']],'final_rc':results.get('final_baseline',{}).get('rc'),'clean':results['porcelain']==''}))
