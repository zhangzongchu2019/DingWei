from pathlib import Path
import json,subprocess,hashlib
R=Path(__file__).resolve().parent;W=Path('/data/ai-dev-data/wt-r62-extseat-groups-mutations');E=json.loads((R/'env-aaed8aaee.json').read_text());P=W/'internal/identity/group.go';b=P.read_bytes();old='store.JoinExternalGroup(ctx, code, agentID, Lower(actorLC),';new='store.JoinExternalGroup(ctx, code, agentID, agent.OwnerLC,';assert b.decode().count(old)==1
steps=[]
try:
 for name,cmd in [('self-baseline',['go','test','./internal/extseat','-run','^TestExternalGroupsControlledMembershipAndRollbackPG$','-count=1','-v']),('self-build',['go','test','./internal/extseat','-run','^$','-count=1']),('self-red',['go','test','./internal/extseat','-run','^TestExternalGroupsControlledMembershipAndRollbackPG$','-count=1','-v'])]:
  if name=='self-build':P.write_text(b.decode().replace(old,new,1))
  with (R/(name+'.log')).open('w') as f:p=subprocess.run(cmd,cwd=W,env=E,stdout=f,stderr=subprocess.STDOUT)
  steps.append({'name':name,'rc':p.returncode,'command':cmd})
finally:P.write_bytes(b)
text=(R/'self-red.log').read_text();d={'source':E['SOURCE_COMMIT'],'path':str(P.relative_to(W)),'old':old,'new':new,'results':steps,'business_red':steps[-1]['rc']!=0 and 'external Agent self-invite granted membership' in text and 'panic:' not in text,'restored_sha256':hashlib.sha256(P.read_bytes()).hexdigest(),'porcelain':subprocess.check_output(['git','status','--porcelain'],cwd=W,text=True)};(R/'self-mutation.json').write_text(json.dumps(d,indent=2)+'\n');print(json.dumps(d))
