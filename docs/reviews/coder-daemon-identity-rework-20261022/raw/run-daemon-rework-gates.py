import os,pathlib,json,subprocess,tempfile,datetime,concurrent.futures
r=pathlib.Path('/data/ai-dev-data/daemon-identity');o=pathlib.Path('/tmp/coder-daemon-identity-rework-gates-20261022');o.mkdir(mode=0o700)
source=subprocess.check_output(['git','rev-parse','HEAD'],cwd=r,text=True).strip();py='/tmp/coder-0232-locked-venv/bin/python'
def now(): return datetime.datetime.now(datetime.timezone.utc).isoformat()
def env(home,tmp):
 return dict(HOME=str(home),TMPDIR=str(tmp),PATH='/tmp/coder-0232-locked-venv/bin:/usr/lib/go-1.22/bin:/usr/bin:/bin',PYTHONPATH=str(r/'tools/sessionhelper')+':'+str(r/'tools/sessionhelper/tests'),PYTHONDONTWRITEBYTECODE='1',GOTOOLCHAIN='local',GOPATH='/home/dev/go',GOCACHE='/home/dev/.cache/go-build',GOMODCACHE='/home/dev/go/pkg/mod',GOPROXY='off',GOSUMDB='off',DW_TEST_EMBEDDED_PG_STRICT='1',DW_TEST_PG_SKIP_OK='0',DW_DESKTOP_INTEGRATION_PYTHON=py)
def run(name,cmd,e,timeout=1800):
 start=now()
 with (o/(name+'.stdout')).open('wb') as stdout,(o/(name+'.stderr')).open('wb') as stderr:
  p=subprocess.run(cmd,cwd=r,env=e,stdout=stdout,stderr=stderr,timeout=timeout)
 (o/(name+'.rc')).write_text(str(p.returncode)+'\n');(o/(name+'.json')).write_text(json.dumps(dict(source=source,command=cmd,start=start,end=now(),rc=p.returncode,env_i=True,isolated_HOME=True,strict_pg=True,skip_ok=False,dsn_set=False,locked_venv=py),indent=2)+'\n');print(name,p.returncode,flush=True);return p.returncode
def helper():
 with tempfile.TemporaryDirectory(prefix='did-rh-') as home,tempfile.TemporaryDirectory(prefix='did-rt-') as tmp:
  return run('helper',[py,'-B','-m','unittest','discover','-s','tools/sessionhelper/tests','-v'],env(home,tmp))
def canonical():
 with tempfile.TemporaryDirectory(prefix='did-rch-') as home,tempfile.TemporaryDirectory(prefix='did-rct-') as tmp:
  return run('canonical',['sh','scripts/run-build-tests.sh'],env(home,tmp))
def go():
 with tempfile.TemporaryDirectory(prefix='did-rgh-') as home,tempfile.TemporaryDirectory(prefix='did-rgt-') as tmp:
  e=env(home,tmp)
  if run('go-prewarm',['go','test','./internal/nodegate','-run','^TestGateOnConnectFires$','-count=1'],e):return 1
  if run('vet',['go','vet','./...'],e):return 1
  return run('go-full',['go','test','./...','-json','-count=1','-timeout=30m'],e)
with concurrent.futures.ThreadPoolExecutor(max_workers=3) as p:
 results=list(p.map(lambda f:f(),[helper,canonical,go]))
raise SystemExit(1 if any(results) else 0)
