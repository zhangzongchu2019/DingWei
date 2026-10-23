"""Withdraw version pins only in a detached worktree; record green/red/green."""
from pathlib import Path
import datetime,hashlib,json,subprocess,time
root=Path('/data/ai-dev-data/r39-version-02312-mutation')
out=Path('/data/ai-dev-data/r39-version-02312-evidence/mutations');out.mkdir(exist_ok=True)
groups=[('M1-root-only',['VERSION']),('M2-go-only',['internal/version/version.go']),('M3-helper-only',['tools/sessionhelper/sessionhelper/__init__.py']),('M4-coherent-rollback',['VERSION','internal/version/version.go','tools/sessionhelper/sessionhelper/__init__.py']),('M5-runtime-lock',['tools/sessionhelper/requirements-runtime.lock'])]
results=[]
for name,files in groups:
    originals={file:(root/file).read_bytes() for file in files}
    record={'name':name,'source_commit':subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip(),'original_sha256':{file:hashlib.sha256(data).hexdigest() for file,data in originals.items()},'phases':[]}
    try:
        for phase in ['before','withdraw','restore']:
            for file,data in originals.items():
                new=data
                if phase=='withdraw':
                    new=data+b'\n# withdrawal: dependency lock drift\n' if name=='M5-runtime-lock' else data.replace(b'0.2.3.12',b'0.2.3.11')
                    assert new!=data
                (root/file).write_bytes(new)
            home=out/(name+'-'+phase+'-home');home.mkdir(exist_ok=True)
            env={'HOME':str(home),'PATH':'/tmp/coder-0232-locked-venv/bin:/usr/bin:/bin','TMPDIR':'/data/ai-dev-data/t','GOTMPDIR':'/data/ai-dev-data/tmp-coder-go','GOCACHE':'/home/dev/.cache/go-build','GOMODCACHE':'/home/dev/go/pkg/mod','LANG':'C.UTF-8','TZ':'UTC','PYTHONDONTWRITEBYTECODE':'1'}
            command=['/usr/bin/go','test','./internal/version','-count=1','-v'];start=datetime.datetime.now(datetime.timezone.utc).isoformat();t=time.monotonic()
            result=subprocess.run(command,cwd=root,env=env,capture_output=True,timeout=90)
            (out/(name+'-'+phase+'.stdout')).write_bytes(result.stdout);(out/(name+'-'+phase+'.stderr')).write_bytes(result.stderr)
            record['phases'].append({'phase':phase,'rc':result.returncode,'start_utc':start,'end_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'elapsed_seconds':time.monotonic()-t,'command':command,'environment':env,'sha256':{file:hashlib.sha256((root/file).read_bytes()).hexdigest() for file in files}})
    finally:
        for file,data in originals.items():(root/file).write_bytes(data)
    record['restored_sha256']={file:hashlib.sha256((root/file).read_bytes()).hexdigest() for file in files}
    (out/(name+'.json')).write_text(json.dumps(record,indent=2)+'\n');results.append(record)
    assert [p['rc'] for p in record['phases']]==[0,1,0],name
    assert record['original_sha256']==record['restored_sha256'],name
    assert '--- FAIL: TestHelper02312ReleasePins' in (out/(name+'-withdraw.stdout')).read_text(),name
    print(name,'0 -> 1 -> 0',flush=True)
(out/'SUMMARY.json').write_text(json.dumps(results,indent=2)+'\n')
