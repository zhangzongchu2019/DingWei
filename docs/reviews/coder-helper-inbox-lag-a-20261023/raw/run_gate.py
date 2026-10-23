from pathlib import Path
import sys,subprocess,json,datetime,time,hashlib
root=Path('/data/ai-dev-data/helper-inbox-lag-a-fix');out=Path('/data/ai-dev-data/helper-inbox-lag-a-final-evidence');name=sys.argv[1];command=sys.argv[2:]
env={'HOME':'/home/dev','PATH':'/tmp/coder-0232-locked-venv/bin:/usr/bin:/bin','TMPDIR':'/data/ai-dev-data/t','GOTMPDIR':'/data/ai-dev-data/tmp-coder-go','GOCACHE':'/home/dev/.cache/go-build','GOMODCACHE':'/home/dev/go/pkg/mod','HUB_TEST_PG_STRICT':'1','HUB_TEST_PG_SKIP_OK':'0','HUB_TEST_PG_EMBEDDED':'1','LANG':'C.UTF-8','TZ':'UTC','DW_DESKTOP_INTEGRATION_PYTHON':'/tmp/coder-0232-locked-venv/bin/python3'}
if name in ['canonical','helper-full']:
 env['HOME']=str(out/(name+'-home'));env['TMPDIR']='/data/ai-dev-data/'+('c' if name=='canonical' else 'h');Path(env['HOME']).mkdir(exist_ok=True);Path(env['TMPDIR']).mkdir(exist_ok=True)
if name=='helper-full':env['PYTHONPATH']=str(root/'tools/sessionhelper')+':'+str(root/'view-service')
if name=='browser-inbox-lag':env['NODE_PATH']='/tmp/coder-a3-playwright/node_modules'
files=subprocess.check_output(['git','diff','--name-only','30463142','HEAD'],cwd=root,text=True).splitlines();sha={f:hashlib.sha256((root/f).read_bytes()).hexdigest() for f in files}
start=datetime.datetime.now(datetime.timezone.utc).isoformat();t=time.monotonic()
with (out/(name+'.stdout')).open('wb') as stdout,(out/(name+'.stderr')).open('wb') as stderr:
 try:rc=subprocess.run(command,cwd=root,env=env,stdout=stdout,stderr=stderr,timeout=1800).returncode
 except subprocess.TimeoutExpired:rc=124
record={'name':name,'command':command,'environment':env,'start_utc':start,'end_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'elapsed_seconds':round(time.monotonic()-t,3),'rc':rc,'source_commit':subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip(),'source_sha256':sha}
(out/(name+'.json')).write_text(json.dumps(record,indent=2)+'\n');print(json.dumps({'name':name,'rc':rc,'elapsed_seconds':record['elapsed_seconds']}),flush=True);sys.exit(rc)
