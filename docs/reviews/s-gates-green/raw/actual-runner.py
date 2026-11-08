import subprocess,os,json,datetime,sys,time
from pathlib import Path
root=Path('/data/ai-dev-data/s-gates-green');out=Path('/tmp/s-gates-green-20261108');out.mkdir(exist_ok=True)
name=sys.argv[1];args=sys.argv[2:]
if args and args[0]=="--root":
 root=Path(args[1]);args=args[2:]
env={'PATH':'/tmp/coder-0232-locked-venv/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin','HOME':'/home/dev','USER':'dev','LOGNAME':'dev','LANG':'C.UTF-8','GOPATH':'/home/dev/go','GOCACHE':'/home/dev/.cache/go-build','GOTOOLCHAIN':'local','OPENSPEC_TELEMETRY':'0'}
start=datetime.datetime.now(datetime.timezone.utc).isoformat()
meta={'name':name,'argv':args,'cwd':str(root),'thread':os.environ.get('CODEX_THREAD_ID'),'start_utc':start,'source':subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip(),'env_keys':sorted(env),'PATH':env['PATH']}
meta['working_status']=subprocess.check_output(['git','status','--porcelain'],cwd=root,text=True)
meta['working_diff_sha256']=__import__('hashlib').sha256(subprocess.check_output(['git','diff','HEAD'],cwd=root)).hexdigest()
(out/(name+'.json')).write_text(json.dumps(meta,indent=2))
with (out/(name+'.log')).open('w') as log:
 log.write(json.dumps(meta)+'\n');log.flush()
 result=subprocess.run(args,cwd=root,env=env,stdout=log,stderr=subprocess.STDOUT)
 meta.update(rc=result.returncode,end_utc=datetime.datetime.now(datetime.timezone.utc).isoformat());log.write('\nRESULT '+json.dumps(meta)+'\n')
(out/(name+'.json')).write_text(json.dumps(meta,indent=2));print(json.dumps(meta));sys.exit(result.returncode)
