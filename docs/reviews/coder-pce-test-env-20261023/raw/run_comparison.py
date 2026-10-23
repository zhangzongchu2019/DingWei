from pathlib import Path
import datetime,hashlib,json,subprocess
out=Path('/data/ai-dev-data/pce-test-env-evidence');python='/tmp/coder-0232-locked-venv/bin/python3'
records=[]
for name,location,expected in [('before','pce-test-env-before',1),('after','pce-test-env',0)]:
    root=Path('/data/ai-dev-data')/location;fixture=root/'tools/sessionhelper/tests/pce_retention_crash_fixture.py'
    sha=hashlib.sha256(fixture.read_bytes()).hexdigest();home=out/('comparison-'+name+'-home');home.mkdir(exist_ok=True)
    env={'HOME':str(home),'PATH':'/tmp/coder-0232-locked-venv/bin:/usr/bin:/bin','TMPDIR':'/data/ai-dev-data/t','LANG':'C.UTF-8','TZ':'UTC'}
    command=[python,str(out/'missing_api_probe.py'),str(fixture)]
    start=datetime.datetime.now(datetime.timezone.utc).isoformat();result=subprocess.run(command,cwd=root,env=env,capture_output=True,timeout=40)
    (out/('comparison-'+name+'.stdout')).write_bytes(result.stdout);(out/('comparison-'+name+'.stderr')).write_bytes(result.stderr)
    record={'phase':name,'source_commit':subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip(),'fixture_sha256_before':sha,'fixture_sha256_after':hashlib.sha256(fixture.read_bytes()).hexdigest(),'command':command,'environment':env,'start_utc':start,'end_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'rc':result.returncode}
    records.append(record);assert record['fixture_sha256_before']==record['fixture_sha256_after'];assert result.returncode==expected
(out/'COMPARISON.json').write_text(json.dumps(records,indent=2)+'\n');print(json.dumps(records,indent=2))
