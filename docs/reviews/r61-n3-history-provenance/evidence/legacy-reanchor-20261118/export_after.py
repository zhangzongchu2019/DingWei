import pathlib,subprocess,json,datetime,time,hashlib
out=pathlib.Path(__file__).parent;root=pathlib.Path('/data/ai-dev-data/r61-n3-history-provenance')
while True:
 r=json.loads((out/'gates.json').read_text())
 if any(x['rc'] for x in r):raise SystemExit('gate failed: export not run')
 if r and r[-1]['name']=='strict':break
 time.sleep(2)
env=json.loads((out/'env.json').read_text());env['N3_HISTORY_FIXTURE_OUTPUT']=str(out/'history-fixture.json');cmd=['go','test','./internal/m8','-run','^TestHistoryProvenanceRealHubAgentTimerReplay$','-count=1','-v'];row={'source_commit':env['SOURCE_COMMIT'],'command':cmd,'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
with (out/'fixture-export.log').open('wb') as f:row['rc']=subprocess.run(cmd,cwd=root,env=env,stdout=f,stderr=subprocess.STDOUT).returncode
row['ended_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();row['raw_sha256']=hashlib.sha256((out/'fixture-export.log').read_bytes()).hexdigest();(out/'fixture-export.json').write_text(json.dumps(row,indent=2)+'\n');print(row,flush=True)
