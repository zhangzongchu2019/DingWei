from pathlib import Path
import subprocess,json,sys,datetime
A=Path(__file__).resolve().parent;env=json.loads((A/'frozen-env.json').read_text());source=env['SOURCE_COMMIT'];cuts=json.loads((A/'mutations/summary.json').read_text());assert len(cuts)==41 and all(x['accepted'] and x['source']==source for x in cuts)
phases=[
 ('canonical-final',['./build.sh','--test-only']),
 ('go-packages-count3',['go','test','./internal/mcpgw','./internal/extseat','./internal/m8','-count=3','-p','1','-v','-json','-timeout=60m']),
 ('migration-full',['go','test','./internal/store/migrate','-count=1','-p','1','-v','-json','-timeout=15m']),
 ('cmd-focused',['go','test','./cmd/workpulse','-run','TestDCR|TestOptionalMCPGateway|TestMCPGateway','-count=3','-p','1','-v','-json','-timeout=15m']),
 ('focused-race',['go','test','-race','./internal/mcpgw','./cmd/workpulse','-run','TestDCR|TestMCPAuthFirst|TestPGStore','-count=1','-p','1','-v','-json','-timeout=15m']),
 ('strict-final',['/tmp/coder56solzzc-openspec-1.16.0/node_modules/.bin/openspec','validate','--all','--strict'])]
results=[]
for name,cmd in phases:
 print('START',name,datetime.datetime.now(datetime.timezone.utc).isoformat(),flush=True)
 p=subprocess.run([sys.executable,str(A/'run-frozen.py'),name,*cmd]);r=json.loads((A/(name+'.result.json')).read_text());results.append(r|{'phase':name});(A/'gate-summary.json').write_text(json.dumps(results,indent=2)+'\n')
 if p.returncode!=0 or r['rc']!=0:print('STOP',name,flush=True);sys.exit(1)
print('GATES_OK',source,flush=True)
