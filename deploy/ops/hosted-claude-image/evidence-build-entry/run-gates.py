from pathlib import Path
import json,subprocess,shutil,time,datetime,hashlib,tempfile
lab=Path(__file__).parent;repo=Path('/data/ai-dev-data/claude-build-entry-r26');home=lab/'home';home.mkdir(exist_ok=True)
shutil.copytree('/home/dev/.embedded-postgres-go',home/'.embedded-postgres-go',dirs_exist_ok=True)
env={'PATH':'/data/ai-dev-data/helper-deps-lock-venv/bin:/usr/local/bin:/usr/bin:/bin','HOME':str(home),'TMPDIR':tempfile.mkdtemp(prefix='cbe-'),'GOCACHE':str(lab.parent/'gocache'),'GOMODCACHE':'/home/dev/go/pkg/mod','GOPATH':str(lab/'gopath'),'GOPROXY':'off','LANG':'C.UTF-8','TZ':'UTC','GIT_CONFIG_NOSYSTEM':'1'}
(lab/'ENVIRONMENT.json').write_text(json.dumps(env,indent=2)+'\n');source=subprocess.check_output(['git','rev-parse','HEAD'],cwd=repo,text=True).strip();results=[]
for label,args in [('claude28',['python3','deploy/ops/hosted-claude-image/test_claude_start.py','-v']),('h3-regression',['python3','-m','unittest','discover','-s','deploy/ops/hosted-image','-p','test_*.py','-v']),('entrypoint-contract',['python3','deploy/test_build_entrypoints.py','-v']),('shell-syntax',['sh','-n','deploy/ops/hosted-claude-image/build-image.sh']),('canonical-unmodified',['./build.sh','--test-only'])]:
 path=lab/'raw'/(label+'.log');t=time.monotonic();start=datetime.datetime.now(datetime.timezone.utc).isoformat()
 with path.open('w') as out:r=subprocess.run(args,cwd=repo,env=env,stdout=out,stderr=subprocess.STDOUT)
 row={'source':source,'argv':args,'rc':r.returncode,'elapsed_seconds':round(time.monotonic()-t,3),'start':start,'end':datetime.datetime.now(datetime.timezone.utc).isoformat(),'sha256':hashlib.sha256(path.read_bytes()).hexdigest()};results.append(row);(lab/'RESULTS.json').write_text(json.dumps(results,indent=2)+'\n');print(json.dumps(row),flush=True)
 if r.returncode:raise SystemExit(r.returncode)
print('BUILD_ENTRY_ALL_GATES_OK',flush=True)
