from pathlib import Path
import os,json,subprocess,shutil,datetime
lab=Path(__file__).parent;env=json.loads((lab/'env.json').read_text());wt=Path('/data/ai-dev-data/hold-release-r24');tmp=Path(env['TMPDIR'])
assert str(tmp).startswith('/tmp/h-r1-')
def size(p):
 if not p.exists():return 0
 if p.is_file():return p.lstat().st_size
 return sum(x.lstat().st_size for x in p.rglob('*') if x.is_file() and not x.is_symlink())
exclude={os.getpid()};pid=os.getppid()
while pid>0 and pid not in exclude:
 exclude.add(pid)
 try:pid=int(Path(f'/proc/{pid}/stat').read_text().rsplit(')',1)[1].split()[1])
 except (FileNotFoundError,ProcessLookupError):break
processes=[]
for path in Path('/proc').iterdir():
 if not path.name.isdigit() or int(path.name) in exclude:continue
 try:cmd=(path/'cmdline').read_bytes();comm=(path/'comm').read_text().strip()
 except (OSError,ProcessLookupError):continue
 if any(x.encode() in cmd for x in [str(lab),str(tmp)]):processes.append({'pid':int(path.name),'comm':comm})
assert not processes,processes
paths=[lab/'cache',lab/'home',tmp,lab/'mutant'];before={str(p):size(p) for p in paths}
p=subprocess.run(['go','clean','-cache','-testcache'],cwd=wt,env=env,capture_output=True,text=True);assert p.returncode==0,p.stderr
after_clean=size(lab/'cache')
subprocess.run(['git','worktree','remove','--force',str(lab/'mutant')],cwd=wt,check=True)
for p in [lab/'cache',lab/'home',tmp]:shutil.rmtree(p)
result={'utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'owned_processes':processes,'before_bytes':before,'cache_after_go_clean_bytes':after_clean,'after_bytes':{str(p):size(p) for p in paths},'shared_module_cache':'untouched','preserved':'main source checkout; local and Git evidence only'}
(lab/'CLEANUP.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result))
