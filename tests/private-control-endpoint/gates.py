"""Frozen input gate runner. Only isolated local development fixtures, no rollout."""
from concurrent.futures import ThreadPoolExecutor,as_completed
from datetime import datetime,timezone
import hashlib,json,os
from pathlib import Path
import shutil,subprocess,sys,tempfile
ROOT=Path(__file__).resolve().parents[2]
PY='/tmp/coder-0232-locked-venv/bin/python'


def main(expected,output):
    source=subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip()
    assert source==expected,'SOURCE_ANCHOR_MISMATCH'
    out=Path(output).resolve();out.mkdir(parents=True,exist_ok=False)
    def source_manifest():
        result=[]
        entries=subprocess.check_output(['git','ls-tree','-r','-z',expected],cwd=ROOT).split(b'\0')
        for line in filter(None,entries):
            header,raw_path=line.split(b'\t',1);mode,kind,blob=header.decode().split();path=os.fsdecode(raw_path)
            if kind!='blob' or path.startswith('docs/'):continue
            target=ROOT/path
            raw=os.fsencode(os.readlink(target)) if mode=='120000' else target.read_bytes()
            assert subprocess.check_output(['git','cat-file','blob',blob],cwd=ROOT)==raw,'SOURCE_BLOB_MISMATCH '+path
            result.append({'path':path,'mode':mode,'blob':blob,'size':len(raw),'sha256':hashlib.sha256(raw).hexdigest()})
        dirty=[os.fsdecode(v) for v in subprocess.check_output(['git','diff','--name-only','-z',expected],cwd=ROOT).split(b'\0') if v]
        untracked=[os.fsdecode(v) for v in subprocess.check_output(['git','ls-files','--others','--exclude-standard','-z'],cwd=ROOT).split(b'\0') if v]
        assert not [name for name in dirty+untracked if not name.startswith('docs/')],'NON_DOC_DIRTY'
        return result
    manifest=source_manifest()
    (out/'SOURCE.json').write_text(json.dumps(manifest,indent=2)+'\n')
    scratch=Path(tempfile.mkdtemp(prefix='dwpe-g-'));scratch.chmod(0o700)
    home=scratch/'h';home.mkdir(mode=0o700);tmp=scratch/'t';tmp.mkdir(mode=0o700)
    # Reuse only the already-cached public binary archive in a private cache;
    # serial PG prewarm below avoids concurrent cold extraction races.
    archive=Path('/home/dev/.embedded-postgres-go/embedded-postgres-binaries-linux-amd64-18.3.0.txz')
    pgcache=home/'.embedded-postgres-go';pgcache.mkdir(mode=0o700)
    shutil.copy2(archive,pgcache/archive.name)
    env={'HOME':str(home),'TMPDIR':str(tmp),'PATH':str(Path(PY).parent)+':/usr/bin:/bin',
         'LANG':'C.UTF-8','PYTHONDONTWRITEBYTECODE':'1','PYTHONPATH':str(ROOT/'tools/sessionhelper'),
         'GOTOOLCHAIN':'local','GOPATH':'/home/dev/go','GOMODCACHE':'/home/dev/go/pkg/mod',
         'GOCACHE':'/home/dev/.cache/go-build','XDG_CACHE_HOME':str(home/'.cache'),
         'HUB_TEST_PG_STRICT':'1','HUB_TEST_PG_SKIP_OK':'0','DW_DESKTOP_INTEGRATION_PYTHON':PY,
         'NODE_PATH':'/tmp/coder-a3-playwright/node_modules','PW_CHANNEL':'chrome','SH_PROVISION_NO_EXIT':'1'}
    (out/'ENVIRONMENT.json').write_text(json.dumps({'source':source,'uid':os.getuid(),'gid':os.getgid(),
        'hostname':os.uname().nodename,'cores':os.cpu_count(),'env_i':True,'env_keys':sorted(env),
        'isolated_HOME':str(home),'tmp':str(tmp),'python':PY,'python_version':subprocess.check_output([PY,'--version'],text=True).strip(),
        'go_version':subprocess.check_output(['go','version'],env=env,text=True).strip(),
        'strict_pg':True,'skip_ok':False,'dsn_set':False,
        'runtime_lock_sha256':hashlib.sha256((ROOT/'tools/sessionhelper/requirements-runtime.lock').read_bytes()).hexdigest(),
        'pg_cached_archive_sha256':hashlib.sha256(archive.read_bytes()).hexdigest()},indent=2)+'\n')
    rows=[]
    def run(label,command,timeout=1800):
        start=datetime.now(timezone.utc).isoformat()
        with (out/(label+'.stdout')).open('wb') as stdout,(out/(label+'.stderr')).open('wb') as stderr:
            try:rc=subprocess.run(command,cwd=ROOT,env=env,stdout=stdout,stderr=stderr,timeout=timeout).returncode
            except subprocess.TimeoutExpired:rc=124
        row={'label':label,'command':command,'source':source,'start':start,'end':datetime.now(timezone.utc).isoformat(),'rc':rc}
        (out/(label+'.rc')).write_text(str(rc)+'\n');(out/(label+'.json')).write_text(json.dumps(row,indent=2)+'\n');rows.append(row)
        print(label+' rc='+str(rc),flush=True)
        return row
    warm=run('pg-prewarm',['go','test','./internal/store/pgtest','-run','^TestPool_RoundTrip$','-count=1','-timeout=5m'],timeout=360)
    assert warm['rc']==0,'PG_PREWARM_FAILED'
    commands=[
        ('vet',['go','vet','./...'],600),
        ('go-full',['go','test','./...','-json','-count=1','-timeout=30m'],1900),
        ('helper',[PY,'-B','-m','unittest','discover','-s','tools/sessionhelper/tests','-v'],1800),
        ('canonical',['sh','scripts/run-build-tests.sh'],1800),
        ('withdrawals',[PY,'-B','tests/private-control-endpoint/check.py',str(out/'withdrawals')],600),
        ('native-0161',[PY,'-B','tests/private-control-endpoint/real.py','0.161.0',str(out/'native-0.161.0')],180),
        ('native-0163',[PY,'-B','tests/private-control-endpoint/real.py','0.163.0',str(out/'native-0.163.0')],180),
    ]
    # Source is immutable while these run. Each native gate has its own HOME
    # and endpoint; helper/canonical fixtures isolate their own child profiles.
    with ThreadPoolExecutor(5) as pool:
        futures=[pool.submit(run,*item) for item in commands]
        for future in as_completed(futures):future.result()
    for label,name in [('browser-admission','renderer-model-admission-regression.js'),
                       ('browser-mention','renderer-mention-reply-regression.js'),
                       ('browser-backfill','stale-thread-backfill-browser-regression.js')]:
        run(label,['node','view-service/'+name],timeout=180)
    assert source_manifest()==manifest,'FINAL_SOURCE_DRIFT'
    (out/'RESULT.json').write_text(json.dumps({'source':source,'rows':rows,'passed':all(row['rc']==0 for row in rows)},indent=2)+'\n')
    assert all(row['rc']==0 for row in rows),'ONE_OR_MORE_GATES_FAILED'
    print('PRIVATE_ENDPOINT_FROZEN_GATES_PASS',flush=True)


if __name__=='__main__':main(sys.argv[1],sys.argv[2])
