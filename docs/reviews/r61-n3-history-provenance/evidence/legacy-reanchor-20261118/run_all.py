import pathlib,subprocess,json,datetime,hashlib
out=pathlib.Path(__file__).parent;root=pathlib.Path('/data/ai-dev-data/r61-n3-history-provenance');env=json.loads((out/'env.json').read_text());source=env['SOURCE_COMMIT']
steps=[('new-mutations',['python3',str(out/'new_mutations.py')]),('all40-mutations',['python3',str(out/'mutations40.py')]),('browser-full',['python3','tools/run-browser-regressions.py','--receipt',str(out/'browser-receipt.json')]),('build-canonical',['./build.sh','--test-only']),('store-full3',['go','test','./internal/store/...','-count=3','-p','1','-timeout','90m','-v']),('m8-full3',['go','test','./internal/m8','-count=3','-p','1','-timeout','90m','-v']),('focused-race',['go','test','-race','./internal/store','./internal/m8','-run','TestHistoryProvenance|TestSessionHistory','-count=1','-p','1','-timeout','30m','-v']),('strict',['/tmp/coder56solzzc-openspec-1.16.0/node_modules/.bin/openspec','validate','--all','--strict'])]
rows=[]
for name,cmd in steps:
 assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip()==source
 assert not subprocess.check_output(['git','status','--porcelain'],cwd=root).strip()
 r={'name':name,'source_commit':source,'command':cmd,'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()};print('START',name,flush=True)
 with (out/(name+'.log')).open('wb') as f:r['rc']=subprocess.run(cmd,cwd=root,env=env,stdout=f,stderr=subprocess.STDOUT).returncode
 r['ended_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();r['raw_sha256']=hashlib.sha256((out/(name+'.log')).read_bytes()).hexdigest();rows.append(r);(out/'gates.json').write_text(json.dumps(rows,indent=2)+'\n');print('END',name,r['rc'],flush=True)
 if r['rc']:break
 if name=='all40-mutations':
  cuts=json.loads((out/'mutations/summary.json').read_text());assert cuts['valid']==cuts['killed']==40,cuts
