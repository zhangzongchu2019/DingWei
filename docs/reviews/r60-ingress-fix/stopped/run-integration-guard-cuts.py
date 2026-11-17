from pathlib import Path
import os,subprocess,json,time,datetime,hashlib,re
r=Path('/data/ai-dev-data/local-run-artifacts/coder56solzzc/r60-ingress-fix');root=Path('/data/ai-dev-data/r60-ingress-fix-mutations');source='a24bb16335040ae06486f1c76be69ee542958214';gate=r/'final-gates-rerun'
# Never add own targeted-test load during the five serial whole-m8 samples.
while not (gate/'COMPLETE.json').exists():
 if (gate/'STOP.json').exists():raise SystemExit('whole source gate STOP; no extra cuts started')
 try:args=(Path('/proc/1375364/cmdline')).read_bytes().split(b'\0')
 except OSError:raise SystemExit('source gate ended without COMPLETE')
 if len(args)<2 or args[1]!=str(r/'run-candidate-gates-rerun.py').encode():raise SystemExit('source gate PID no longer belongs to task')
 time.sleep(1)
assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip()==source
assert not subprocess.check_output(['git','status','--porcelain'],cwd=root,text=True).strip()
out=r/'final-candidate/integration-cuts';out.mkdir(exist_ok=True)
env={'HOME':'/tmp/rif-home','TMPDIR':'/tmp','PATH':'/tmp/coder-0232-locked-venv/bin:/usr/local/bin:/usr/bin:/bin','LANG':'C.UTF-8','TZ':'UTC','GOTOOLCHAIN':'local','GOPATH':'/home/dev/go','GOMODCACHE':'/home/dev/go/pkg/mod','GOCACHE':'/home/dev/.cache/go-build','GOPROXY':'off','GOSUMDB':'off','HUB_TEST_PG_STRICT':'1','PYTHONDONTWRITEBYTECODE':'1','PYTHONPATH':'tools/sessionhelper:tools/sessionhelper/tests','DW_DESKTOP_INTEGRATION_PYTHON':'/data/ai-dev-data/helper-deps-lock-venv/bin/python3'}
py=['python3','-m','unittest','test_session_heartbeat.PersistentHeartbeatReconnectTest.test_persistent_ipc_heartbeat_follows_current_ws_and_capability','-v']
go=['go','test','./internal/m8','-run','^TestR60OutboundControlBackpressureRecoversWithSendStatus$','-count=1','-v','-timeout','1m']
cuts=[('joint_receipt_failure_observable','internal/m8/outbound_receipt.go','c.obsConn.OutboundReceiptWriteFailed()','// removed receipt observation',go),('heartbeat_current','tools/sessionhelper/sessionhelper/app.py','heartbeat_handler=current(self._make_heartbeat_handler)','heartbeat_handler=self._make_heartbeat_handler(ws)',py)]
rows=[]
def run(name,args):
 with (out/(name+'.log')).open('wb') as f:p=subprocess.run(args,cwd=root,env=env,stdout=f,stderr=subprocess.STDOUT,timeout=120)
 return p.returncode,(out/(name+'.log')).read_bytes()
for name,path,before,after,cmd in cuts:
 p=root/path;orig=p.read_bytes();s=orig.decode();assert s.count(before)==1
 try:
  p.write_text(s.replace(before,after));compilecmd=['python3','-m','py_compile',path] if path.endswith('.py') else ['go','test','./internal/m8','-run','^$','-count=1','-timeout','1m']
  build,_=run(name+'-build',compilecmd);rc,raw=run(name,cmd) if build==0 else (None,b'');text=raw.decode(errors='replace');assertions=[l for l in text.splitlines() if re.search(r'(_test.go:\d+:|AssertionError:)',l)]
  row={'name':name,'source_commit':source,'path':path,'before':before,'after':after,'build_rc':build,'rc':rc,'failed_assertions':assertions,'raw_sha256':hashlib.sha256(raw).hexdigest(),'utc':datetime.datetime.now(datetime.timezone.utc).isoformat()};row['killed']=build==0 and rc not in (None,0) and bool(assertions) and 'panic:'not in text and 'panic serving'not in text;rows.append(row);(out/'summary.json').write_text(json.dumps(rows,ensure_ascii=False,indent=2)+'\n');assert row['killed'],row
 finally:p.write_bytes(orig)
 print(name,'KILLED',flush=True)
baselines=[]
for name,cmd in [('restored_joint',go),('restored_heartbeat',py)]:
 rc,raw=run(name,cmd);row={'name':name,'source_commit':source,'rc':rc,'raw_sha256':hashlib.sha256(raw).hexdigest()};baselines.append(row);(out/'baselines.json').write_text(json.dumps(baselines,indent=2)+'\n');assert rc==0
assert not subprocess.check_output(['git','status','--porcelain'],cwd=root,text=True).strip()
(out/'COMPLETE.json').write_text(json.dumps({'source_commit':source,'utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'cuts':2,'restored_baselines':2})+'\n');print('INTEGRATION_GUARD_CUTS_COMPLETE',flush=True)
