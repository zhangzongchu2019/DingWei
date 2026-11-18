from pathlib import Path
import subprocess,json,datetime,hashlib
A=Path(__file__).parent;T=Path('/data/ai-dev-data/r61-integration');E=json.loads((A/'env.json').read_text());rows=[]
steps=[('canonical',['./build.sh','--test-only']),('m8-store-full3',['go','test','./internal/m8','./internal/store/...','-count=3','-p','1','-timeout','90m','-v']),('focused-race',['go','test','-race','./internal/m8','./internal/store','-run','DeliveryACK|QueueFull|FailureNotice|ReliableSessionDeliveryBusyAck','-count=1','-p','1','-timeout','40m','-v']),('browser-full',[E['DW_DESKTOP_INTEGRATION_PYTHON'],'tools/run-browser-regressions.py','--receipt',str(A/'browser-receipt.txt')]),('strict',['/tmp/coder56solzzc-openspec-1.16.0/node_modules/.bin/openspec','validate','--all','--strict'])]
for name,cmd in steps:
 assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=T,text=True).strip()==E['SOURCE_COMMIT'];assert not subprocess.check_output(['git','status','--porcelain'],cwd=T).strip()
 r={'name':name,'source':E['SOURCE_COMMIT'],'command':cmd,'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()};print('START',name,flush=True)
 with(A/(name+'.log')).open('wb')as f:r['rc']=subprocess.run(cmd,cwd=T,env=E,stdout=f,stderr=subprocess.STDOUT).returncode
 r['ended_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();r['sha256']=hashlib.sha256((A/(name+'.log')).read_bytes()).hexdigest();rows.append(r);(A/'gates.json').write_text(json.dumps(rows,indent=2)+'\n');print('END',name,r['rc'],flush=True)
 if r['rc']:raise SystemExit('GATE_STOP '+name+'; preserve logs, evaluate explicit Z3 only if applicable')
