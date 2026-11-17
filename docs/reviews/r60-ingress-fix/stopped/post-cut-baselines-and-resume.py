from pathlib import Path
import time,subprocess,os,json,datetime,hashlib,signal
r=Path('/data/ai-dev-data/local-run-artifacts/coder56solzzc/r60-ingress-fix');root=Path('/data/ai-dev-data/r60-ingress-fix-mutations');source='a24bb16335040ae06486f1c76be69ee542958214'
needle=str(r/'wait-run-candidate-cuts.py').encode()
def active():
 for p in Path('/proc').iterdir():
  if p.name.isdigit():
   try:args=(p/'cmdline').read_bytes().split(b'\0')
   except OSError:continue
   if len(args)>1 and args[1]==needle:return True
 return False
while active():time.sleep(1)
assert 'CANDIDATE_CUT_REPLAYS_COMPLETE' in (r/'candidate-cuts-progress.log').read_text()
assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip()==source
assert not subprocess.check_output(['git','status','--porcelain'],cwd=root,text=True).strip()
out=r/'final-candidate/restored-baselines';out.mkdir(exist_ok=True)
env={'HOME':'/tmp/rif-home','TMPDIR':'/tmp','PATH':'/tmp/coder-0232-locked-venv/bin:/usr/local/bin:/usr/bin:/bin','LANG':'C.UTF-8','TZ':'UTC','GOTOOLCHAIN':'local','GOPATH':'/home/dev/go','GOMODCACHE':'/home/dev/go/pkg/mod','GOCACHE':'/home/dev/.cache/go-build','GOPROXY':'off','GOSUMDB':'off','HUB_TEST_PG_STRICT':'1','PYTHONPATH':'tools/sessionhelper:tools/sessionhelper/tests','PYTHONDONTWRITEBYTECODE':'1','DW_DESKTOP_INTEGRATION_PYTHON':'/data/ai-dev-data/helper-deps-lock-venv/bin/python3'}
rows=[]
for name,args in [('go',['go','test','./internal/m8','./internal/store','./cmd/workpulse','-run','LegacyIngress|SessionDiagnostics|SessionIngress|SessionWrite|SessionKeepalive|ViewerOutput|ViewerOffline|SessionHistoryCancellation|Keepalive','-count=1','-p','1','-v','-timeout','3m']),('python',['python3','-m','unittest','test_terminal_start_observation','-v'])]:
 row={'name':name,'source_commit':source,'argv':args,'utc_start':datetime.datetime.now(datetime.timezone.utc).isoformat(),'env':env,'uid':os.getuid()};print(name,'START',flush=True)
 with (out/(name+'.log')).open('wb') as f:p=subprocess.run(args,cwd=root,env=env,stdout=f,stderr=subprocess.STDOUT,timeout=240)
 row.update(rc=p.returncode,utc_end=datetime.datetime.now(datetime.timezone.utc).isoformat(),raw_sha256=hashlib.sha256((out/(name+'.log')).read_bytes()).hexdigest());rows.append(row);(out/'summary.json').write_text(json.dumps(rows,indent=2)+'\n');assert p.returncode==0,(name,'restored baseline failed')
 print(name,'PASS',flush=True)
assert not subprocess.check_output(['git','status','--porcelain'],cwd=root,text=True).strip()
pid=1375364;args=(Path('/proc')/str(pid)/'cmdline').read_bytes().split(b'\0');assert args[1]==str(r/'run-candidate-gates-rerun.py').encode()
os.kill(pid,signal.SIGCONT)
p=r/'gate-dispatcher-pause.json';s=json.loads(p.read_text());s['utc_resume']=datetime.datetime.now(datetime.timezone.utc).isoformat();s['resume_reason']='Both full candidate cut batches and same restored worktree Go/Python baselines passed; now whole-m8 rounds can start without own targeted test load.';p.write_text(json.dumps(s,indent=2)+'\n');print('DISPATCHER_RESUMED_AFTER_RESTORED_BASELINES',flush=True)
