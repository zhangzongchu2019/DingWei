"""Run required gates with a clean environment, isolated HOME/TMPDIR and raw logs."""
import argparse,concurrent.futures,json,os,subprocess,tempfile,time
from pathlib import Path
ROOT=Path(__file__).resolve().parents[3]
a=argparse.ArgumentParser();a.add_argument('--output',type=Path,required=True);a.add_argument('--only');args=a.parse_args();out=args.output.resolve();out.mkdir(parents=True,exist_ok=False)
checks=[('packages',['go','test','-p','1','-count=1','-json','-timeout=25m','./internal/mcpgw','./internal/extseat','./internal/extshared','./internal/extwire','./internal/feishulogin','./internal/store','./internal/store/migrate','./internal/identity','./cmd/workpulse']),('m8',['go','test','-p','1','-count=1','-json','-timeout=25m','./internal/m8']),('race',['go','test','-p','1','-race','-count=3','-v','-timeout=20m','./internal/extseat','./internal/feishulogin','./internal/mcpgw','./internal/extwire','./internal/store/migrate','-run','Consent']),('canonical',['bash','build.sh','--test-only'])]
if args.only:checks=[x for x in checks if x[0] in args.only.split(',')]
def run(item):
 name,cmd=item
 with tempfile.TemporaryDirectory(prefix='zc') as d:
  env={'PATH':'/tmp/coder-0232-locked-venv/bin:/usr/bin:/bin:/home/dev/.local/bin','HOME':d,'TMPDIR':d,'CODEX_HOME':d+'/codex','GOPROXY':'off','HUB_TEST_PG_STRICT':'1','GOCACHE':'/home/dev/.cache/go-build','GOMODCACHE':'/home/dev/go/pkg/mod','OPENSPEC_TELEMETRY':'0'}
  cache=Path('/home/dev/.embedded-postgres-go')
  if cache.exists():(Path(d)/'.embedded-postgres-go').symlink_to(cache,target_is_directory=True)
  start=time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime())
  with (out/(name+'.stdout')).open('wb') as log:p=subprocess.run(cmd,cwd=ROOT,env=env,stdout=log,stderr=subprocess.STDOUT)
  r={'command':cmd,'start':start,'end':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'rc':p.returncode,'environment_names':sorted(env),'uid':os.getuid()};(out/(name+'.json')).write_text(json.dumps(r,indent=2)+'\n');print(name,p.returncode,flush=True);return r
with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:r=list(pool.map(run,checks))
(out/'RESULT.json').write_text(json.dumps(r,indent=2)+'\n');raise SystemExit(int(any(x['rc'] for x in r)))
