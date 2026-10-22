"""Run frozen helper/CWD gates with private HOME; no deployment or network install."""
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime,timezone
import hashlib,json,os,subprocess,sys,tempfile,types
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]


def main(expected,output):
    head=subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip()
    assert head==expected,'SOURCE_ANCHOR_MISMATCH'
    assert not subprocess.check_output(['git','diff','--name-only',expected,'--',':(exclude)docs'],cwd=ROOT),'SOURCE_DIRTY'
    out=Path(output).resolve();out.mkdir(parents=True,exist_ok=False)
    source=[]
    for entry in subprocess.check_output(['git','ls-tree','-rz',expected],cwd=ROOT).split(b'\0'):
        if not entry:continue
        header,name=entry.split(b'\t',1);mode,kind,blob=header.decode().split();name=os.fsdecode(name)
        if kind!='blob' or name.startswith('docs/'):continue
        target=ROOT/name;raw=os.fsencode(os.readlink(target)) if mode=='120000' else target.read_bytes()
        assert raw==subprocess.check_output(['git','cat-file','blob',blob],cwd=ROOT)
        source.append(dict(path=name,mode=mode,blob=blob,size=len(raw),sha256=hashlib.sha256(raw).hexdigest()))
    (out/'SOURCE.json').write_text(json.dumps(source,indent=2)+'\n')
    lock=ROOT/'tools/sessionhelper/requirements-runtime.lock'
    assert lock.read_bytes()==subprocess.check_output(['git','show','29e63f99:tools/sessionhelper/requirements-runtime.lock'],cwd=ROOT)
    scratch=Path(tempfile.mkdtemp(prefix='cwd-final-'));scratch.chmod(0o700)
    rows=[]
    def run(label,command):
        home=scratch/label;home.mkdir(mode=0o700)
        env={'HOME':str(home),'PATH':str(Path(sys.executable).parent)+':/usr/bin:/bin',
             'LANG':'C.UTF-8','PYTHONDONTWRITEBYTECODE':'1','PYTHONPATH':str(ROOT/'tools/sessionhelper'),
             'GOTOOLCHAIN':'local','GOPATH':'/home/dev/go','GOMODCACHE':'/home/dev/go/pkg/mod',
             'GOCACHE':'/home/dev/.cache/go-build','DW_DESKTOP_INTEGRATION_PYTHON':sys.executable}
        start=datetime.now(timezone.utc).isoformat()
        with (out/(label+'.stdout')).open('wb') as stdout,(out/(label+'.stderr')).open('wb') as stderr:
            rc=subprocess.run(command,cwd=ROOT,env=env,stdout=stdout,stderr=stderr,timeout=1800).returncode
        row=dict(label=label,command=command,rc=rc,start=start,end=datetime.now(timezone.utc).isoformat(),
                 env_i=True,env_keys=sorted(env),isolated_HOME=str(home))
        (out/(label+'.rc')).write_text(str(rc)+'\n');rows.append(row);print(label+' rc='+str(rc),flush=True)
    commands=[('helper',[sys.executable,'-B','-m','unittest','discover','-s','tools/sessionhelper/tests','-v']),
              ('canonical',['sh','scripts/run-build-tests.sh']),
              ('withdrawals',[sys.executable,'-B','tests/helper-cwd-import/check.py',str(out/'withdrawals')])]
    with ThreadPoolExecutor(3) as pool:list(pool.map(lambda args:run(*args),commands))
    # Build only a local artifact; the existing verifier accepts unchanged roots.
    sys.path.insert(0,str(ROOT/'tools/sessionhelper'))
    import package_release
    from update_launcher.cwd_import import render_hook
    artifact=scratch/'candidate.tgz';pin=package_release.build(ROOT,expected,artifact)
    old=types.ModuleType('old_verifier')
    old_source=subprocess.check_output(['git','show','b5faf73c:tools/sessionhelper/package_release.py'],cwd=ROOT)
    exec(compile(old_source,'<git:b5faf73c>','exec'),old.__dict__)
    manifest,files=old.verify(artifact)
    assert not any(name.endswith('.pth') for name in files),'HOOK_LEAKED_IN_UPGRADE_ARCHIVE'
    (out/'PACKAGE.json').write_text(json.dumps(dict(pin=pin,old_verifier='b5faf73c',members=sorted(files),
        lock_sha256=hashlib.sha256(lock.read_bytes()).hexdigest(),hook_outside_archive=True),indent=2)+'\n')
    hook=render_hook('/opt/dingwei/sessionhelper-src','/opt/dingwei/sessionhelper-venv/bin/python')
    (out/'EXAMPLE-HOOK.pth').write_bytes(hook)
    (out/'ENVIRONMENT.json').write_text(json.dumps(dict(source=head,python=sys.version,executable=sys.executable,
        uid=os.getuid(),hostname=os.uname().nodename,example_hook_sha256=hashlib.sha256(hook).hexdigest(),
        example_hook_bytes=len(hook),example_paths_not_independently_verified_on_production=True),indent=2)+'\n')
    (out/'RESULT.json').write_text(json.dumps(dict(source=head,rows=rows,passed=all(r['rc']==0 for r in rows)),indent=2)+'\n')
    assert all(r['rc']==0 for r in rows),'GATE_FAILED'
    print('HELPER_CWD_IMPORT_FROZEN_GATES_PASS',flush=True)


if __name__=='__main__':main(sys.argv[1],sys.argv[2])
