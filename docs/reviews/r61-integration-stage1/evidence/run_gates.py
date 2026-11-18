from pathlib import Path
import subprocess,json,datetime,hashlib
out=Path(__file__).parent;root=Path('/data/ai-dev-data/r61-integration');env=json.loads((out/'env.json').read_text());source=env['SOURCE_COMMIT'];rows=[]
steps=[('build-canonical',['./build.sh','--test-only']),('m8-full3',['go','test','./internal/m8','-count=3','-p','1','-timeout','90m','-v']),('store-full3',['go','test','./internal/store/...','-count=3','-p','1','-timeout','90m','-v']),('browser-full',['python3','tools/run-browser-regressions.py','--receipt',str(out/'browser-receipt.txt')]),('strict',['/tmp/coder56solzzc-openspec-1.16.0/node_modules/.bin/openspec','validate','--all','--strict'])]
for name,cmd in steps:
 assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip()==source
 assert not subprocess.check_output(['git','status','--porcelain'],cwd=root).strip()
 r={'name':name,'source_commit':source,'command':cmd,'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()};print('START',name,flush=True)
 with (out/(name+'.log')).open('wb') as f:r['rc']=subprocess.run(cmd,cwd=root,env=env,stdout=f,stderr=subprocess.STDOUT).returncode
 r['ended_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();r['raw_sha256']=hashlib.sha256((out/(name+'.log')).read_bytes()).hexdigest();rows.append(r);(out/'gates.json').write_text(json.dumps(rows,indent=2)+'\n');print('END',name,r['rc'],flush=True)
 if r['rc']:raise SystemExit('GATE STOP '+name)
