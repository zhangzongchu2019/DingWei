from pathlib import Path
import os,json,subprocess,hashlib,tempfile,time,datetime,shutil
lab=Path(__file__).parent;repo=Path('/data/ai-dev-data/hosted-claude-packager');home=lab/'home';home.mkdir(exist_ok=True)
cache=Path('/home/dev/.embedded-postgres-go')
if cache.is_dir():shutil.copytree(cache,home/'.embedded-postgres-go',dirs_exist_ok=True)
tmp=Path(tempfile.mkdtemp(prefix='cp-bc-'))
env={'PATH':'/data/ai-dev-data/helper-deps-lock-venv/bin:/usr/local/bin:/usr/bin:/bin','HOME':str(home),'TMPDIR':str(tmp),'GOCACHE':str(lab/'gocache'),'GOMODCACHE':'/home/dev/go/pkg/mod','GOPATH':str(lab/'gopath'),'GOPROXY':'off','LANG':'C.UTF-8','TZ':'UTC','GIT_CONFIG_NOSYSTEM':'1'}
assert 'PYTHONDONTWRITEBYTECODE' not in env and 'PYTHONPYCACHEPREFIX' not in env
(lab/'ENVIRONMENT.json').write_text(json.dumps(env,indent=2)+'\n');source=subprocess.check_output(['git','rev-parse','HEAD'],cwd=repo,text=True).strip();results=[]
for label,argv in [('claude-no-bytecode-env',['python3','-m','unittest','discover','-s','deploy/ops/hosted-claude-image','-p','test_claude_start.py','-v']),('h3-no-bytecode-env',['python3','-m','unittest','discover','-s','deploy/ops/hosted-image','-p','test_*.py','-v']),('canonical-unmodified',['./build.sh','--test-only'])]:
 start=datetime.datetime.now(datetime.timezone.utc).isoformat();t=time.monotonic();path=lab/'raw'/(label+'.log')
 with path.open('w') as stream:r=subprocess.run(argv,cwd=repo,env=env,stdout=stream,stderr=subprocess.STDOUT)
 item={'source':source,'argv':argv,'rc':r.returncode,'elapsed_seconds':round(time.monotonic()-t,3),'start':start,'end':datetime.datetime.now(datetime.timezone.utc).isoformat(),'sha256':hashlib.sha256(path.read_bytes()).hexdigest()};results.append(item);(lab/'RESULTS.json').write_text(json.dumps(results,indent=2)+'\n');print(json.dumps(item),flush=True)
 if r.returncode:raise SystemExit(r.returncode)
print('BYTECODE_CANONICAL_OK',flush=True)
