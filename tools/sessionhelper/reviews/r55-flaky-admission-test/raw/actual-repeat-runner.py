import datetime,hashlib,json,os,subprocess,sys,time
from pathlib import Path
out=Path('/tmp/r55-flaky-50');out.mkdir(exist_ok=False)
root=Path.cwd();env={**os.environ,'PYTHONPATH':str(root/'tools/sessionhelper')}
records=[]
for n in range(1,51):
 started=datetime.datetime.now(datetime.timezone.utc).isoformat();load=os.getloadavg();t=time.monotonic()
 result=subprocess.run([sys.executable,'-m','unittest','-v','tools.sessionhelper.tests.test_dispatch_lifecycle'],env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
 (out/f'{n:02d}.stdout').write_bytes(result.stdout)
 r={'iteration':n,'started_utc':started,'load_before':load,'load_after':os.getloadavg(),'elapsed_seconds':time.monotonic()-t,'rc':result.returncode}
 records.append(r);(out/'RESULT.json').write_text(json.dumps({'requested':50,'completed':len(records),'failures':sum(x['rc']!=0 for x in records),'runs':records},indent=2)+'\n')
 print(json.dumps(r),flush=True)
 if result.returncode:sys.exit(result.returncode)
