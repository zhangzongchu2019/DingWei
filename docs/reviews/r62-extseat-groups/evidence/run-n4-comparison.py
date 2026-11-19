import datetime,json,subprocess,time
from pathlib import Path
R=Path(__file__).resolve().parent
while not (R/'go-aaed8aaee.result.json').exists():time.sleep(3)
base=json.loads((R/'env-aaed8aaee.json').read_text());items=[]
for label,source,work in [('baseline206','206d41b26dab646c0e50a111eed5a45ccf7c72f6','/data/ai-dev-data/wt-r62-groups-baseline206'),('candidate-aaed8aaee','aaed8aaee6630e2683096c977a058da653fee7e2','/data/ai-dev-data/r62-extseat-groups')]:
 assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=work,text=True).strip()==source
 env=dict(base,SOURCE_COMMIT=source);cmd=['go','test','./internal/m8','-run','^TestSessionNameReconnectKeepsNameAndReleaseReuses$','-count=200','-p=1','-timeout=20m','-v'];start=datetime.datetime.now(datetime.timezone.utc).isoformat()
 with (R/('n4-'+label+'.log')).open('w') as f:p=subprocess.run(cmd,cwd=work,env=env,stdout=f,stderr=subprocess.STDOUT)
 text=(R/('n4-'+label+'.log')).read_text();item={'label':label,'source':source,'command':cmd,'start_utc':start,'end_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'rc':p.returncode,'pass':text.count('--- PASS:'),'fail':text.count('--- FAIL:'),'interrupted_lines':sum('interrupted (9)' in l for l in text.splitlines())};items.append(item);(R/'n4-comparison.json').write_text(json.dumps(items,indent=2)+'\n')
print(json.dumps(items))
