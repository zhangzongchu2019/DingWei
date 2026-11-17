from pathlib import Path
import os, subprocess, json, datetime, hashlib, re, sys, time
root=Path('/data/ai-dev-data/r60-ingress-fix');out=Path('/data/ai-dev-data/local-run-artifacts/coder56solzzc/r60-ingress-fix/final-gates-rerun');out.mkdir(exist_ok=True)
source='a24bb16335040ae06486f1c76be69ee542958214'
env={'HOME':'/tmp/rif-home','TMPDIR':'/tmp/rif-final','PATH':'/tmp/coder-0232-locked-venv/bin:/usr/local/bin:/usr/bin:/bin','LANG':'C.UTF-8','TZ':'UTC','PYTHONDONTWRITEBYTECODE':'1','HUB_TEST_PG_STRICT':'1','GOTOOLCHAIN':'local','GOPATH':'/home/dev/go','GOCACHE':'/home/dev/.cache/go-build','GOMODCACHE':'/home/dev/go/pkg/mod','GOFLAGS':'-p=1','GOPROXY':'off','GOSUMDB':'off','OPENSPEC_TELEMETRY':'0','DW_DESKTOP_INTEGRATION_PYTHON':'/data/ai-dev-data/helper-deps-lock-venv/bin/python3','SOURCE_COMMIT':source}
Path(env['TMPDIR']).mkdir(exist_ok=True)
records=[]
def now():return datetime.datetime.now(datetime.timezone.utc).isoformat()
def run(name,args,timeout=5000):
 assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip()==source
 assert not subprocess.check_output(['git','status','--porcelain'],cwd=root,text=True).strip()
 row={'name':name,'source_commit':source,'argv':args,'env':env,'uid':os.getuid(),'cwd':str(root),'utc_start':now()};records.append(row);(out/'progress.json').write_text(json.dumps(records,indent=2)+'\n');print(name,'START',row['utc_start'],flush=True)
 with (out/(name+'.log')).open('wb') as f:p=subprocess.run(args,cwd=root,env=env,stdout=f,stderr=subprocess.STDOUT,timeout=timeout)
 raw=(out/(name+'.log')).read_bytes();row.update(rc=p.returncode,utc_end=now(),raw_sha256=hashlib.sha256(raw).hexdigest(),bytes=len(raw));events=[]
 for line in raw.decode(errors='replace').splitlines():
  try:e=json.loads(line)
  except ValueError:continue
  if isinstance(e,dict) and 'Action'in e:events.append(e)
 if events:
  row['top_pass']=sum(e.get('Action')=='pass' and bool(e.get('Test')) and '/' not in e['Test'] for e in events)
  row['all_pass']=sum(e.get('Action')=='pass' and bool(e.get('Test')) for e in events)
  row['failed_tests']=[e['Test'] for e in events if e.get('Action')=='fail' and e.get('Test')]
  row['skipped_tests']=[e['Test'] for e in events if e.get('Action')=='skip' and e.get('Test')]
  row['interrupted']=[{'test':e.get('Test'),'output':e['Output']} for e in events if 'interrupted (9)'in e.get('Output','')]
 row['data_race']=raw.count(b'WARNING: DATA RACE')
 (out/(name+'.json')).write_text(json.dumps(row,ensure_ascii=False,indent=2)+'\n');(out/'progress.json').write_text(json.dumps(records,ensure_ascii=False,indent=2)+'\n');print(name,'END rc',p.returncode,'at',row['utc_end'],flush=True);return row
r=run('build',['./build.sh',str(out/'build')]);assert r['rc']==0,'build failed'
# Pure source/interface checks; no version bump or artifact upload.
read={'source_commit':source,'heartbeat_current':'heartbeat_handler=current(self._make_heartbeat_handler)'in(root/'tools/sessionhelper/sessionhelper/app.py').read_text(),'inventory_lines':len((root/'scripts/helper-test-files.txt').read_text().splitlines()),'provenance':'function retainCanonicalTrustedProvenance('in(root/'view-service/renderer-canary-sse.html').read_text(),'versions':{p:(root/p).read_text().strip() for p in ['VERSION','tools/sessionhelper/sessionhelper/__init__.py']}}
(out/'source-interface-checks.json').write_text(json.dumps(read,indent=2)+'\n');assert read['heartbeat_current'] and read['inventory_lines']==97 and read['provenance']
allowed=('TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent','TestViewMentionRoute/a_peer_without_delivery_ack_v1_is_not_sent_to')
green=0
for n in range(1,6):
 r=run('m8-'+str(n),['go','test','-json','./internal/m8/','-count=1','-p','1','-timeout','60m'])
 failures=r.get('failed_tests',[]);interrupt=r.get('interrupted',[])
 invalid=[t for t in failures if not(t==allowed[0] or t==allowed[1] or (t=='TestViewMentionRoute' and allowed[1] in failures))]
 unknown_interrupt=[e for e in interrupt if not(e['test'] and (e['test']==allowed[0] or e['test']==allowed[1]))]
 if invalid or unknown_interrupt or (r['rc'] and not interrupt):
  (out/'STOP.json').write_text(json.dumps({'stage':r['name'],'invalid_failures':invalid,'interrupt_outside_allowed_context':unknown_interrupt,'reason':'Z-3 stop; do not substitute single runs'},ensure_ascii=False,indent=2)+'\n');raise SystemExit('Z-3 STOP')
 if r['rc']==0 and not failures:green+=1
assert green>=3,'Z-3 fewer than three completely green rounds'
r=run('go-other-trees',['go','test','-json','./internal/store/...','./internal/obs/...','./cmd/workpulse/...','-count=1','-p','1','-timeout','60m']);assert r['rc']==0,'other trees failed'
r=run('focused-race',['go','test','-json','-race','./internal/m8','./internal/store','-run','LegacyIngress|SessionDiagnostics|SessionIngress|SessionWrite|SessionKeepalive|ViewerOutput|ViewerOffline|R60OutboundControl|HostedBodySendSameRequestID|Outbound|AppConfigGuards','-count=1','-p','1','-timeout','30m']);assert r['rc']==0 and r['data_race']==0,'focused race failed'
# Named gates must really execute with the isolated Python; no SKIP allowed.
r=run('joint-and-dual-cli',['go','test','-json','./internal/m8','-run','^(TestR60OutboundControlBackpressureRecoversWithSendStatus|TestHostedBodySendSameRequestIDAcrossCLIProcesses)$','-count=1','-v','-p','1','-timeout','2m']);assert r['rc']==0 and not r.get('skipped_tests') and r.get('top_pass')==2,'required CLI gates skipped or failed'
r=run('browser',['python3','tools/run-browser-regressions.py','--receipt',str(out/'browser-receipt.json')]);assert r['rc']==0,'browser failed'
r=run('strict',['node','/home/dev/.npm/_npx/2e6f94dbd680ce26/node_modules/@fission-ai/openspec/bin/openspec.js','validate','--all','--strict']);assert r['rc']==0,'strict failed'
run('go-version-m',['go','version','-m',str(out/'build/workpulse')])
(out/'COMPLETE.json').write_text(json.dumps({'source_commit':source,'utc':now(),'m8_rounds':5,'m8_whole_green':green,'gates':len(records),'uid':os.getuid()},indent=2)+'\n');print('ALL_FINAL_GATES_COMPLETE',flush=True)
