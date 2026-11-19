from pathlib import Path
import json,subprocess,sys,hashlib,datetime
D=Path(__file__).resolve().parent;E=json.loads((D/'env-canonical.json').read_text());P=E['DW_DESKTOP_INTEGRATION_PYTHON'];rows=[]
def run(kind,name,cmd,allow=False):
 print('START',name,flush=True);p=subprocess.run([sys.executable,str(D/'run.py'),kind,name,*cmd]);r=json.loads((D/(name+'.result.json')).read_text());rows.append({'name':name,**r});(D/'gate-sequence.json').write_text(json.dumps(rows,indent=2)+'\n');print('END',name,r['rc'],flush=True)
 if r['rc'] and not allow:raise SystemExit('STOP '+name)
 return r
run('canonical','canonical',['./build.sh','--test-only'])
run('go','m8-store-full3',['go','test','./internal/m8','./internal/store/...','-count=3','-p=1','-timeout=90m','-v'],True)
p=subprocess.run([sys.executable,str(D/'run-go-policy.py')]);assert p.returncode==0,'Go policy STOP'
run('race','focused-race',['go','test','-race','./internal/m8','./internal/store','-run','DeliveryACK|QueueFull|FailureNotice|ReliableSessionDeliveryBusyAck|HistoryProvenance|SessionHistory','-count=1','-p=1','-timeout=60m','-v'])
run('browser','browser-full',[P,'tools/run-browser-regressions.py','--receipt',str(D/'browser-receipt.txt')])
run('strict','strict',['/tmp/coder56solzzc-openspec-1.16.0/node_modules/.bin/openspec','validate','--all','--strict'])
old=Path('/data/ai-dev-data/local-run-artifacts/coder56solzzc/r61-n1c-review-20261118/bin/old-hub-501');assert hashlib.sha256(old.read_bytes()).hexdigest()=='bc45cb95b1e51e54d34b7f0e5512bc8582dc7a6603f168d65699056ba9ba1820'
(D/'bin').mkdir(exist_ok=True);(D/'bin/old-hub-501').symlink_to(old)
run('compat','integrated-hub-build',['go','build','-buildvcs=true','-o',str(D/'bin/integrated-hub'),'./cmd/workpulse'])
run('compat','actual-process-compat',[P,str(D/'compat_real.py')]);r=json.loads((D/'compat-results.json').read_text());assert len(r)==3 and all(x['rc']==0 for x in r),r
(D/'binary-shas.json').write_text(json.dumps({n:hashlib.sha256((D/'bin'/n).read_bytes()).hexdigest() for n in ['old-hub-501','integrated-hub']},indent=2)+'\n')
for n in ['old-hub-501','integrated-hub']:(D/(n+'-go-version-m.txt')).write_text(subprocess.check_output(['go','version','-m',str(D/'bin'/n)],env=E,text=True))
run('http','http-four',['go','test','./internal/mcpgw','-run','^TestR61MCPUnauthenticatedFourBaselinesOverRealHTTP$','-count=1','-v'])
(D/'full-acceptance.json').write_text(json.dumps({'source':E['SOURCE_COMMIT'],'all_serial':True,'passed':True,'go_policy':json.loads((D/'go-policy.json').read_text()),'gates':rows,'three_way_process_cases':3,'test_binaries_only_no_release':True,'end_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()},indent=2)+'\n');print('FULL_SERIAL_ACCEPTED',flush=True)
