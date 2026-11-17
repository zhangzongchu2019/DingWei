import datetime,hashlib,json,os,subprocess,sys
from pathlib import Path
R=Path('/data/ai-dev-data/local-run-artifacts/coder56solzzc/r60-formal-integration-20261117');R.mkdir(exist_ok=True)
S='a311aaf3beea90262c05ca2c316064b8a8856d06'
E={'HOME':str(R/'home'),'TMPDIR':'/tmp/r60-i','PATH':'/tmp/coder-0232-locked-venv/bin:/usr/local/bin:/usr/bin:/bin','LANG':'C.UTF-8','TZ':'UTC','PYTHONDONTWRITEBYTECODE':'1','HUB_TEST_PG_STRICT':'1','GOTOOLCHAIN':'local','GOCACHE':'/data/ai-dev-data/local-run-artifacts/coder56solzzc/go-cache','GOMODCACHE':'/tmp/busy-cf05/h-normal/go/pkg/mod','GOFLAGS':'-p=1','GOPROXY':'off','GOSUMDB':'off','OPENSPEC_TELEMETRY':'0','SOURCE_COMMIT':S,'DW_DESKTOP_INTEGRATION_PYTHON':'/tmp/coder-0232-locked-venv/bin/python3'}
Path(E['HOME']).mkdir(exist_ok=True);Path(E['TMPDIR']).mkdir(exist_ok=True)
def git(w,*args):return subprocess.check_output(['git',*args],cwd=w,text=True).strip()
def run(name,w,cmd,extra=None,mutation=False):
 env=E| (extra or {});head=git(w,'rev-parse','HEAD');dirty=git(w,'status','--porcelain');v={'source_commit':head if not dirty else None,'base_source':head,'mutation':mutation,'dirty_before':bool(dirty),'argv':cmd,'cwd':str(w),'uid':os.getuid(),'env':env,'start_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
 with (R/(name+'.log')).open('w') as f:p=subprocess.run(cmd,cwd=w,env=env,stdout=f,stderr=subprocess.STDOUT)
 raw=(R/(name+'.log')).read_bytes();v.update(rc=p.returncode,end_utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),raw_sha256=hashlib.sha256(raw).hexdigest(),dirty_after=bool(git(w,'status','--porcelain')),head_after=git(w,'rev-parse','HEAD'));(R/(name+'.json')).write_text(json.dumps(v,ensure_ascii=False,indent=2)+'\n');print(name,p.returncode,flush=True);return v
if __name__=='__main__':raise SystemExit(run(sys.argv[1],sys.argv[2],sys.argv[3:])['rc'])
