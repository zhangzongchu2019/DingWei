"""Verify frozen Hub-only source and complete local gate evidence from Git."""
import gzip
import hashlib
import json
import subprocess
import sys

HERE = 'docs/reviews/coder-feishu-single-tenant-20261022'
ref = sys.argv[1] if len(sys.argv) > 1 else 'HEAD'
repo = sys.argv[2] if len(sys.argv) > 2 else '.'
def git(*args):
    return subprocess.check_output(['git','-C',repo,*args])
def blob(path):
    return git('show',ref+':'+path)
def doc(path):
    return blob(HERE+'/'+path)
source = json.loads(doc('SOURCE-MANIFEST.json'))
assert git('rev-parse',source['source']+'^{tree}').decode().strip()==source['tree']
for item in source['files']:
    data = blob(item['path'])
    assert len(data)==item['bytes'] and hashlib.sha256(data).hexdigest()==item['sha256'],item['path']
    assert data==git('show',source['source']+':'+item['path']),item['path']
    assert git('rev-parse',source['source']+':'+item['path']).decode().strip()==item['blob']
assert {p for p in git('ls-tree','-rz','--name-only',ref).decode().split('\0') if p and not p.startswith('docs/')}=={f['path'] for f in source['files']}
assert blob('docs/designs/S-FEISHU-SINGLE-TENANT.md')==git('show',source['source']+':docs/designs/S-FEISHU-SINGLE-TENANT.md')
for path in ['tools/sessionhelper','VERSION','internal/version','scripts/run-build-tests.sh','internal/store/migrations','internal/identity/migrations','go.mod','go.sum']:
    assert not git('diff',source['base'],source['source'],'--',path),('out-of-scope drift',path)
evidence = json.loads(doc('EVIDENCE-MANIFEST.json'))
for item in evidence['files']:
    data=doc(item['path'])
    assert len(data)==item['bytes'] and hashlib.sha256(data).hexdigest()==item['sha256'],item['path']
present={p[len(HERE)+1:] for p in git('ls-tree','-rz','--name-only',ref,'--',HERE).decode().split('\0') if p}
assert present=={f['path'] for f in evidence['files']}|{'EVIDENCE-MANIFEST.json'}
for name in ['vet','canonical','go-final','go-final-prewarm','nodegate-same-environment','mutations']:
    assert doc('raw/gates/'+name+'.rc').strip()==b'0',name
for item in json.loads(doc('LOG-COMPRESSION.json')):
    data=gzip.decompress(doc(item['stored_path']))
    assert len(data)==item['raw_bytes'] and hashlib.sha256(data).hexdigest()==item['raw_sha256']
rows=json.loads(doc('raw/mutations/CHECKS.json'))['rows']
red=[r for r in rows if r['marker']]
assert len(red)==18 and all(r['assertion_red'] and r['rc']==1 for r in red)
assert len(rows)==37 and all(r['rc']==0 for r in rows if not r['marker'])
assert json.loads(doc('raw/mutations/RESULT.json'))['source']==source['source']
assert 'BUILD_TESTS_OK python=56 shell=2' in doc('raw/gates/canonical.stdout').decode()
runtime_env=json.loads(doc('raw/gates/go-final-environment.json'))
assert runtime_env['source']==source['source'] and runtime_env['single_package_pg_cache_prewarmed']
assert runtime_env['env']['HUB_TEST_PG_STRICT']=='1' and runtime_env['env']['HUB_TEST_PG_SKIP_OK']=='0'
assert 'HUB_TEST_PG_DSN' not in runtime_env['env'] and 'DW_DESKTOP_INTEGRATION_PYTHON' not in runtime_env['env']
summary=json.loads(doc('GO-FINAL-SUMMARY.json'))
assert summary['rc']==0 and not summary['failures']
assert summary['test_packages_passed']==53 and summary['no_test_packages']==13
actual_events=[json.loads(line) for line in gzip.decompress(doc('raw/gates/go-final.stdout.gz')).decode().splitlines() if line]
package_states={r['Package']:r['Action'] for r in actual_events if r.get('Action') in ('pass','fail','skip') and not r.get('Test')}
started={r['Package'] for r in actual_events if r.get('Action')=='start'}
assert started==set(package_states), 'incomplete full Go stream'
assert sum(v=='pass' for v in package_states.values())==53
assert sum(v=='skip' for v in package_states.values())==13
assert not any(r.get('Action')=='fail' for r in actual_events)
skipped=[r for r in actual_events if r.get('Action')=='skip' and r.get('Test')]
assert len(skipped)==summary['conditional_tests_skipped']
assert not any(r['Test'].startswith('TestSingleTenant') for r in skipped)
assert any(r['Test']=='TestPilotQuestionAcrossRealHTTPHelperIPCMCP' for r in skipped), 'optional baseline-red fixture must not be represented as tested green'
controls=json.loads(doc('raw/gates/d3-baseline-control.json'))
assert len(controls)==2 and all(r['rc']==1 and r['same_keyerror'] for r in controls)
print('VERIFY_OK source_files='+str(len(source['files']))+' evidence_files='+str(len(evidence['files']))+' mutations=18 full_go=53/53 helper_unchanged=True')
