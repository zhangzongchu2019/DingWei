from pathlib import Path
import json,subprocess,datetime,hashlib
A=Path(__file__).parent;T=Path('/data/ai-dev-data/r61-stage3-probe');E=json.loads((A/'env.json').read_text());E['STAGE3_OUTPUT']=str(A/'restart-state-3');assert not(A/'restart-state-3').exists();rows=[]
for name,cmd in [('restart-build',['go','test','-vet=off','-overlay',str(A/'overlay.json'),'-c','-o',str(A/'restart.test'),'./internal/m8']),('restart-test',[str(A/'restart.test'),'-test.run=^TestStage3HistorySurvivesHubProcessRestart$','-test.v','-test.timeout=5m'])]:
 r={'name':name,'source':E['SOURCE_COMMIT'],'command':cmd,'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()};p=A/(name+'.log')
 with p.open('wb')as f:r['rc']=subprocess.run(cmd,cwd=T,env=E,stdout=f,stderr=subprocess.STDOUT).returncode
 r['ended_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();r['sha256']=hashlib.sha256(p.read_bytes()).hexdigest();rows.append(r);(A/(name+'.json')).write_text(json.dumps(r,indent=2)+'\n');(A/'restart-runs.json').write_text(json.dumps(rows,indent=2)+'\n');print(name,r['rc'],flush=True)
 if r['rc']:print(p.read_text()[-1800:]);raise SystemExit(1)
