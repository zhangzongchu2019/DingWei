"""Verify source bindings, semantic withdrawal results and complete gate records."""
from pathlib import Path
import datetime,hashlib,json,re,subprocess

raw=Path(__file__).resolve().parent;report=raw.parent;root=report.parents[2]
source='6139e4bca7650697682d89c85fa217c0459ca890'
base='f1aabc6cbe3edbe887760f65e53a76a7b4062252'
def digest(path):return hashlib.sha256(path.read_bytes()).hexdigest()
def blob(commit,file):return subprocess.check_output(['git','show',commit+':'+file],cwd=root)
gates={}
for name in ['retention','canonical','helper-tests','helper-all','go-all-valid','vet']:
    data=json.loads((raw/(name+'.json')).read_text())
    assert data['source_commit']==source and data['rc']==0,name
    for file,sha in data['source_sha256'].items():assert digest(root/file)==sha,(name,file)
    gates[name]={'rc':data['rc'],'elapsed_seconds':data['elapsed_seconds']}
assert 'BUILD_TESTS_OK python=63 shell=2' in (raw/'canonical.stdout').read_text()
assert re.search(r'Ran 963 tests\b',(raw/'helper-tests.stderr').read_text())
assert re.search(r'Ran 9 tests\b',(raw/'helper-all.stderr').read_text())
assert re.search(r'Ran 29 tests\b',(raw/'retention.stderr').read_text())
go=(raw/'go-all-valid.stdout').read_text()
gates['go-all-valid']['tested_packages']=len(re.findall(r'^ok\s',go,re.M))
gates['go-all-valid']['no_test_packages']=len(re.findall(r'^\?\s',go,re.M))
assert gates['go-all-valid']['tested_packages']==53
assert gates['go-all-valid']['no_test_packages']==13
assert not (raw/'go-all-valid.stderr').read_bytes()
changes=subprocess.check_output(['git','diff','--name-only',base,source],cwd=root,text=True).splitlines()
assert sorted(changes)==sorted(['tools/sessionhelper/tests/pce_retention_crash_fixture.py','tools/sessionhelper/tests/test_pce_retention.py'])
bindings=json.loads((raw/'SOURCE-VERIFY.json').read_text())
for file,sha in bindings['unchanged_files'].items():
    assert hashlib.sha256(blob(base,file)).hexdigest()==sha==digest(root/file)
comparison=json.loads((raw/'COMPARISON.json').read_text())
for data,commit,rc in zip(comparison,[base,source],[1,0]):
    file='tools/sessionhelper/tests/pce_retention_crash_fixture.py'
    assert data['source_commit']==commit and data['rc']==rc
    assert data['fixture_sha256_before']==data['fixture_sha256_after']==hashlib.sha256(blob(commit,file)).hexdigest()
assert 'AttributeError' in (raw/'comparison-before.stderr').read_text()
assert '"rescued_fixture_children": 1' in (raw/'comparison-before.stderr').read_text()
assert '"rescued_fixture_children": 0' in (raw/'comparison-after.stderr').read_text()
assert all(json.loads((raw/'comparison-after.stdout').read_text()).values())
mutations=json.loads((raw/'mutations/SUMMARY.json').read_text())
assert len(mutations)==5
markers=['AttributeError','OWNER_CRASH_FIXTURE_LEAKED','ChildProcessError not raised','OWNER_CRASH_CHILD_CHANGED','OWNER_CRASH_RECLAIMED_RUNTIME']
for record,marker in zip(mutations,markers):
    assert record==json.loads((raw/'mutations'/(record['name']+'.json')).read_text())
    assert record['source_commit']==source
    assert record['original_sha256']==record['restored_sha256']==digest(root/record['file'])
    assert [p['rc'] for p in record['phases']]==[0,1,0]
    assert record['phases'][0]['sha256']==record['phases'][2]['sha256']==record['original_sha256']
    assert record['phases'][1]['sha256']!=record['original_sha256']
    assert marker in (raw/'mutations'/(record['name']+'-withdraw.stderr')).read_text()
result={'verified_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'source_commit':source,'base_commit':base,'gates':gates,'helper_tests':963,'root_helper_tests':9,'canonical':{'python':63,'shell':2},'retention_tests':29,'withdrawals':5,'comparison_rc':[1,0],'source_changes':changes,'result':'PASS'}
(report/'VERIFY.json').write_text(json.dumps(result,indent=2)+'\n')
print(json.dumps(result,indent=2))
