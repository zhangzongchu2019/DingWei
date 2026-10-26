from pathlib import Path
import sys,subprocess,json,datetime,time,hashlib
root=Path('/data/ai-dev-data/d2-wake-agent-id-20261026');out=Path('/data/ai-dev-data/d2-wake-agent-id-20261026-evidence');name=sys.argv[1];cmd=sys.argv[2:]
home=out/(name+'-home');home.mkdir(exist_ok=True)
env={'HOME':str(home),'PATH':'/tmp/coder-0232-locked-venv/bin:/usr/bin:/bin','TMPDIR':'/data/ai-dev-data/t','GOTMPDIR':'/data/ai-dev-data/tmp-coder-go','GOCACHE':'/home/dev/.cache/go-build','GOMODCACHE':'/home/dev/go/pkg/mod','HUB_TEST_PG_STRICT':'1','HUB_TEST_PG_SKIP_OK':'0','HUB_TEST_PG_EMBEDDED':'1','LANG':'C.UTF-8','TZ':'UTC','NODE_PATH':'/tmp/coder-a3-playwright/node_modules','PYTHONDONTWRITEBYTECODE':'1','DW_DESKTOP_INTEGRATION_PYTHON':'/tmp/coder-0232-locked-venv/bin/python3','PYTHONPATH':str(root/'tools/sessionhelper')+':'+str(root/'view-service')}
files=subprocess.check_output(['git','diff','--name-only','4546d26300fc6dd9e66a4e2fd3feccc74430a9f8','HEAD'],cwd=root,text=True).splitlines()+['tools/sessionhelper/requirements-runtime.lock','scripts/run-build-tests.sh'];sha={f:hashlib.sha256((root/f).read_bytes()).hexdigest() for f in files};commit=subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip();start=datetime.datetime.now(datetime.timezone.utc).isoformat();t=time.monotonic()
with (out/(name+'.stdout')).open('wb') as so,(out/(name+'.stderr')).open('wb') as se:
 try:rc=subprocess.run(cmd,cwd=root,env=env,stdout=so,stderr=se,timeout=1800).returncode
 except subprocess.TimeoutExpired:rc=124
rec={'name':name,'command':cmd,'environment':env,'start_utc':start,'end_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'elapsed_seconds':round(time.monotonic()-t,3),'rc':rc,'source_commit':commit,'source_sha256':sha};(out/(name+'.json')).write_text(json.dumps(rec,indent=2)+'\n');print(json.dumps({'name':name,'rc':rc,'elapsed_seconds':rec['elapsed_seconds']}));sys.exit(rc)
