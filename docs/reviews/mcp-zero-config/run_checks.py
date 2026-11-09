"""Clean-environment local checks with raw logs and exact commands/return codes."""
import argparse, concurrent.futures, hashlib, json, os, subprocess, tempfile, time
from pathlib import Path
ROOT=Path(__file__).resolve().parents[3]
ap=argparse.ArgumentParser()
ap.add_argument('--output',type=Path,required=True)
ap.add_argument('--only',choices=['mcpgw','m8','canonical'])
a=ap.parse_args()
a.output=a.output.resolve()
a.output.mkdir(parents=True,exist_ok=False)
def run(item):
 name,command=item
 # Go t.TempDir adds long test names; keep AF_UNIX paths below 108 bytes.
 with tempfile.TemporaryDirectory(prefix='z') as d:
  env={'PATH':'/tmp/coder-0232-locked-venv/bin:/usr/bin:/bin:/home/dev/.local/bin','HOME':d,'TMPDIR':d,'CODEX_HOME':d+'/codex','GOCACHE':'/home/dev/.cache/go-build','GOMODCACHE':'/home/dev/go/pkg/mod','OPENSPEC_TELEMETRY':'0'}
  cache=Path('/home/dev/.embedded-postgres-go')
  if cache.exists(): (Path(d)/'.embedded-postgres-go').symlink_to(cache,target_is_directory=True)
  start=time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime())
  with (a.output/(name+'.stdout')).open('wb') as log:
   p=subprocess.run(command,cwd=ROOT,env=env,stdout=log,stderr=subprocess.STDOUT)
  result={'name':name,'command':command,'start_utc':start,'end_utc':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'rc':p.returncode,'environment_names':sorted(env),'uid':os.getuid()}
  (a.output/(name+'.json')).write_text(json.dumps(result,indent=2)+'\n')
  print(json.dumps(result),flush=True)
  return result
checks=[('mcpgw',['go','test','./internal/mcpgw','-count=1','-json']),('m8',['go','test','./internal/m8','-count=1','-json','-timeout=20m']),('canonical',['sh','scripts/run-build-tests.sh'])]
if a.only: checks=[item for item in checks if item[0]==a.only]
with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:
 results=list(pool.map(run,checks))
(a.output/'RESULT.json').write_text(json.dumps(results,indent=2)+'\n')
raise SystemExit(int(any(r['rc'] for r in results)))
