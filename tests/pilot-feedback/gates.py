#!/usr/bin/env python3
"""Run local gates from an immutable Git export, with isolated HOME and offline deps."""
import argparse,datetime,io,json,subprocess,tarfile,tempfile
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
PY='/data/ai-dev-data/helper-deps-lock-venv/bin/python'
COMMANDS={
 'helper':[PY,'-B','-m','unittest','discover','-s','tools/sessionhelper/tests','-v'],
 'vet':['go','vet','./...'],
 'canonical':['bash','scripts/run-build-tests.sh'],
 'race':['go','test','-race','./internal/store','./internal/m8','./internal/identity','-count=1','-run','^TestPilotQuestion|^TestBlocker|^TestProjectContext','-timeout=10m'],
 'go':['go','test','./...','-json','-count=1','-timeout=30m'],
}
def run(args):
 commit=subprocess.check_output(['git','-C',str(ROOT),'rev-parse',args.commit+'^{commit}'],text=True).strip()
 out=Path(args.output).resolve();out.mkdir(parents=True,exist_ok=True)
 archive=subprocess.check_output(['git','-C',str(ROOT),'archive',commit])
 with tempfile.TemporaryDirectory(prefix='blocker-exact-gates-') as temp:
  root=Path(temp)/'source';root.mkdir();home=Path(temp)/'home';home.mkdir(mode=0o700)
  (home/'.embedded-postgres-go').symlink_to('/home/dev/.embedded-postgres-go',target_is_directory=True)
  with tarfile.open(fileobj=io.BytesIO(archive)) as tar:tar.extractall(root,filter='data')
  env={'HOME':str(home),'PATH':str(Path(PY).parent)+':/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin','PYTHONPATH':str(root/'tools/sessionhelper'),'PYTHONDONTWRITEBYTECODE':'1','TZ':'UTC','LANG':'C.UTF-8','GOCACHE':'/home/dev/.cache/go-build','GOMODCACHE':'/home/dev/go/pkg/mod','GOPATH':'/home/dev/go','GOPROXY':'off','GOSUMDB':'off','DW_DESKTOP_INTEGRATION_PYTHON':PY}
  for label in args.gates:
   cmd=COMMANDS[label];start=datetime.datetime.now(datetime.timezone.utc).isoformat();print(label,start,flush=True)
   with (out/(label+'.log')).open('w') as f:p=subprocess.run(cmd,cwd=root,env=env,stdout=f,stderr=subprocess.STDOUT,timeout=1900)
   data=dict(commit=commit,argv=cmd,rc=p.returncode,start=start,end=datetime.datetime.now(datetime.timezone.utc).isoformat(),git_export=True,isolated_home=True,cleared_environment=True,offline_dependencies=True,embedded_pg_binary_cache_only='/home/dev/.embedded-postgres-go',DSN_unset=True)
   (out/(label+'.json')).write_text(json.dumps(data,indent=2)+'\n');print(label,p.returncode,flush=True)
   if p.returncode:raise AssertionError('gate failed '+label)
if __name__=='__main__':
 p=argparse.ArgumentParser();p.add_argument('--commit',required=True);p.add_argument('--output',required=True);p.add_argument('--gates',nargs='+',choices=list(COMMANDS),default=list(COMMANDS));run(p.parse_args())
