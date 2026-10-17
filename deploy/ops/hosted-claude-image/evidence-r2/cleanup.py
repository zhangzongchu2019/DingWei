from pathlib import Path
import subprocess,json,shutil,os,datetime
lab=Path(__file__).parent;wt=Path('/data/ai-dev-data/hosted-claude-packager');env=json.loads((lab/'ENVIRONMENT.json').read_text());tmp=Path(env['TMPDIR']);assert str(tmp).startswith('/tmp/cp-r2-')
assert not subprocess.check_output(['git','diff','--name-only'],cwd=lab/'mutant',text=True).strip()
def size(p):return sum(x.lstat().st_size for x in p.rglob('*') if x.is_file() and not x.is_symlink()) if p.exists() else 0
excluded={os.getpid()};pid=os.getppid()
while pid>0 and pid not in excluded:
 excluded.add(pid)
 try:pid=int(Path('/proc',str(pid),'stat').read_text().rsplit(')',1)[1].split()[1])
 except OSError:break
processes=[]
for d in Path('/proc').iterdir():
 if not d.name.isdigit() or int(d.name) in excluded:continue
 try:cmd=(d/'cmdline').read_bytes()
 except OSError:continue
 if any(str(p).encode() in cmd for p in [lab,tmp]):processes.append(int(d.name))
assert not processes,processes
owned=[lab/'mutant',lab/'home',tmp];before={str(p):size(p) for p in owned};tmp_members=[p.name for p in tmp.iterdir()];assert not tmp_members,tmp_members
subprocess.run(['git','worktree','remove','--force',str(lab/'mutant')],cwd=wt,check=True)
for p in [lab/'home',tmp]:shutil.rmtree(p)
Path('/tmp/coder-packager-tests.txt').unlink(missing_ok=True)
result={'utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'owned_processes':processes,'before_bytes':before,'after_bytes':{str(p):size(p) for p in owned},'tmp_after_all_test_cleanups':tmp_members,'Go_cache':'not used','scope':'no real Docker/pip/helper/CLI/PG/host execution; synthetic fixture archives removed by TemporaryDirectory cleanup; source/evidence only retained'}
(lab/'CLEANUP.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result))
