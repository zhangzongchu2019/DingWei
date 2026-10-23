"""Check release tree, complete gates and each restored withdrawal binding."""
from pathlib import Path
import datetime,hashlib,json,re,subprocess
raw=Path(__file__).resolve().parent;report=raw.parent;root=report.parents[2]
base='20cdb2e5095c4a5217fb724e3b971a484ba5fb41';source='ee494bf7ab978e8991f61a2b26a4aa98284df83c'
def git(*args):return subprocess.check_output(['git',*args],cwd=root,text=True).strip()
def digest(path):return hashlib.sha256(path.read_bytes()).hexdigest()
bindings=json.loads((raw/'SOURCE-BINDING.json').read_text())
assert bindings['base']==base and bindings['source']==source and git('rev-parse',source+'^')==base
paths=git('diff','--name-only',base,source).splitlines()
assert paths==git('diff','--name-only','01f87ac2^','01f87ac2').splitlines()
assert paths==git('diff','--name-only','a84c1a08^','a84c1a08').splitlines()
assert len(paths)==7 and set(paths)==set(bindings['source_files'])
for file,metadata in bindings['source_files'].items():
    assert metadata['sha256']==digest(root/file)
    assert metadata['blob']==git('rev-parse',source+':'+file)
for file,sha in bindings['unchanged'].items():
    assert sha==digest(root/file)
    assert git('rev-parse',base+':'+file)==git('rev-parse',source+':'+file)
assert digest(root/'tools/sessionhelper/requirements-runtime.lock')=='5ef8d5815239c3e165ae6ede14559fb545cce4f59cc144f49e20d8162453d2a0'
gates={}
for name in ['version-packages','console-mcp-version','browser-version-pin','canonical','helper-full','go-all','vet']:
    data=json.loads((raw/(name+'.json')).read_text())
    assert data['source_commit']==source and data['rc']==0,name
    assert data['environment']['HOME'].startswith('/data/ai-dev-data/r39-version-02312-evidence/')
    assert data['environment']['PATH'].startswith('/tmp/coder-0232-locked-venv/bin:')
    for file,sha in data['source_sha256'].items():assert digest(root/file)==sha,(name,file)
    gates[name]={'rc':data['rc'],'elapsed_seconds':data['elapsed_seconds']}
version=(raw/'version-packages.stdout').read_text();assert len(re.findall(r'^--- PASS:',version,re.M))==4
console=(raw/'console-mcp-version.stdout').read_text()
assert len(re.findall(r'^--- PASS:',console,re.M))==11
assert len(re.findall(r'^    --- PASS:',console,re.M))==4
assert len(re.findall(r'^--- SKIP:',console,re.M))==1
assert '--- SKIP: TestHelper0232MixedVersionEndToEnd' in console
assert 'explicit immutable release pins required' in console
assert '--- PASS: TestProjectContextAcrossRealHTTPHelperIPCMCP' in console
assert 'TOTAL 14 Chromium helper-update UI checks' in (raw/'browser-version-pin.stdout').read_text()
assert 'BUILD_TESTS_OK python=66 shell=2' in (raw/'canonical.stdout').read_text()
assert re.search(r'Ran 982 tests\b',(raw/'helper-full.stderr').read_text())
assert re.search(r'\nOK\s*$',(raw/'helper-full.stderr').read_text())
go=(raw/'go-all.stdout').read_text()
assert len(re.findall(r'^ok\s',go,re.M))==53
assert len(re.findall(r'^\?\s',go,re.M))==13
assert not (raw/'go-all.stderr').read_bytes()
mutations=json.loads((raw/'mutations/SUMMARY.json').read_text());assert len(mutations)==5
for record in mutations:
    name=record['name'];assert record==json.loads((raw/'mutations'/(name+'.json')).read_text())
    assert record['source_commit']==source and [p['rc'] for p in record['phases']]==[0,1,0]
    assert record['original_sha256']==record['restored_sha256']
    assert record['phases'][0]['sha256']==record['phases'][2]['sha256']==record['original_sha256']
    for file,sha in record['original_sha256'].items():assert digest(root/file)==sha
    assert record['phases'][1]['sha256']!=record['original_sha256']
    red=(raw/'mutations'/(name+'-withdraw.stdout')).read_text()
    assert '--- FAIL: TestHelper02312ReleasePins' in red
    if name=='M4-coherent-rollback':
        assert red.count('--- FAIL:')==1
        for test in ['TestVersionMatchesRepositoryVersionFile','TestVersionIsAheadOfLastReleasedVersion','TestHelperVersionMatchesGoVersion']:assert '--- PASS: '+test in red
    if name=='M5-runtime-lock':assert 'runtime lock changed' in red
result={'verified_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'source':source,'base':base,'source_files':7,'reference_paths_equal':True,'dependency_lock_unchanged':True,'capability_files_unchanged':True,'gates':gates,'version_tests':4,'console_top_level_pass':11,'console_subtests_pass':4,'historical_mixed_release_skipped':1,'browser_checks':14,'canonical_python':66,'canonical_shell':2,'helper_tests':982,'go_tested_packages':53,'go_no_tests':13,'withdrawals':5,'withdrawal_rc':'all 0 -> 1 -> 0','restored_sha':'all equal original/current','result':'PASS'}
(report/'VERIFY.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result,indent=2))
