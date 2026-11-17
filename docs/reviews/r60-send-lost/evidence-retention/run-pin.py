import os,sys,json,subprocess,datetime,hashlib
from pathlib import Path
root=Path('/tmp/r60-send-lost-20261116');name=sys.argv[1];cmd=sys.argv[2:]
env={'HOME':str(root/'home'),'TMPDIR':'/tmp/r60-a','PATH':'/tmp/coder-0232-locked-venv/bin:/usr/local/bin:/usr/bin:/bin','LANG':'C.UTF-8','TZ':'UTC','PYTHONDONTWRITEBYTECODE':'1','HUB_TEST_PG_STRICT':'1','GOTOOLCHAIN':'local','GOCACHE':'/data/ai-dev-data/local-run-artifacts/coder56solzzc/go-cache','GOMODCACHE':'/tmp/busy-cf05/h-normal/go/pkg/mod','GOFLAGS':'-p=1','GOPROXY':'off','GOSUMDB':'off','OPENSPEC_TELEMETRY':'0'}
head=subprocess.check_output(['git','rev-parse','HEAD'],cwd='/data/ai-dev-data/r60-send-lost-mutations',text=True).strip()
dirty=bool(subprocess.check_output(['git','status','--porcelain'],cwd='/data/ai-dev-data/r60-send-lost-mutations',text=True).strip())
v={'workspace_base':head,'workspace_dirty':dirty,'argv':cmd,'env':env,'uid':os.getuid(),'cwd':'/data/ai-dev-data/r60-send-lost-mutations','source_commit':None if dirty else head,'start_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
with (root/(name+'.log')).open('w') as f:r=subprocess.run(cmd,cwd=v['cwd'],env=env,stdout=f,stderr=subprocess.STDOUT)
endhead=subprocess.check_output(['git','rev-parse','HEAD'],cwd=v['cwd'],text=True).strip()
enddirty=bool(subprocess.check_output(['git','status','--porcelain'],cwd=v['cwd'],text=True).strip())
v.update(git_head_at_end=endhead,workspace_dirty_at_end=enddirty,workspace_changed_during_run=(endhead!=head or dirty or enddirty))
if v['workspace_changed_during_run']:v['source_commit']=None
v.update(rc=r.returncode,end_utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),raw_sha256=hashlib.sha256((root/(name+'.log')).read_bytes()).hexdigest());(root/(name+'.json')).write_text(json.dumps(v,indent=2)+'\n');print(name,r.returncode);raise SystemExit(r.returncode)
