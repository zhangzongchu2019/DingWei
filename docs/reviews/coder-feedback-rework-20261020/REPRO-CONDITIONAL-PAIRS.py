#!/usr/bin/env python3
"""Three baseline/final exact-tree conditional comparisons, local tests only."""
import subprocess,pathlib,datetime,tarfile,tempfile,io,json
ROOT=pathlib.Path(__file__).resolve().parents[3]
BASE='48befea2bfbd8cbe0685f35be5a8c5fbbfad4576';FINAL='5175f2cee711a68ccd0ee3982213ed61bf9a6c85'
PY='/data/ai-dev-data/helper-deps-lock-venv/bin/python'
out=ROOT/'docs/reviews/coder-feedback-rework-20261020/conditional-pairs';out.mkdir(parents=True,exist_ok=True)
results=[]
with tempfile.TemporaryDirectory(prefix='feedback-conditional-') as temp:
 roots={}
 for name,ref in [('baseline',BASE),('final',FINAL)]:
  root=pathlib.Path(temp)/name;root.mkdir();raw=subprocess.check_output(['git','archive',ref],cwd=ROOT)
  with tarfile.open(fileobj=io.BytesIO(raw))as t:t.extractall(root,filter='data')
  roots[name]=root
 for index in range(1,4):
  for name,ref in [('baseline',BASE),('final',FINAL)]:
   home=pathlib.Path(temp)/f'home-{index}-{name}';home.mkdir(mode=0o700);(home/'.embedded-postgres-go').symlink_to('/home/dev/.embedded-postgres-go',target_is_directory=True)
   env={'HOME':str(home),'PATH':str(pathlib.Path(PY).parent)+':/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin','PYTHONPATH':str(roots[name]/'tools/sessionhelper'),'PYTHONDONTWRITEBYTECODE':'1','LANG':'C.UTF-8','TZ':'UTC','GOCACHE':'/home/dev/.cache/go-build','GOMODCACHE':'/home/dev/go/pkg/mod','GOPATH':'/home/dev/go','GOPROXY':'off','GOSUMDB':'off','DW_DESKTOP_INTEGRATION_PYTHON':PY}
   cmd=['go','test','./internal/consolemux','./internal/m8','-json','-count=1','-run','^TestBudgetRealPGDeadlineAndCapacity$|^TestTerminalViewEmptyBufferRequestsRefresh$','-timeout=3m']
   start=datetime.datetime.now(datetime.timezone.utc).isoformat()
   with (out/f'{index}-{name}.log').open('w')as f:p=subprocess.run(cmd,cwd=roots[name],env=env,stdout=f,stderr=subprocess.STDOUT,timeout=240)
   results.append(dict(pair=index,source=ref,label=name,argv=cmd,rc=p.returncode,start=start,end=datetime.datetime.now(datetime.timezone.utc).isoformat(),isolated_home=True,offline=True,external_dsn=False))
   (out/'RESULTS.json').write_text(json.dumps(results,indent=2)+'\n');print(index,name,p.returncode,flush=True)
