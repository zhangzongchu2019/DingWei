"""Verify frozen test-only scope and recorded concurrency without rerunning tests."""
import hashlib,json,subprocess,sys
ROOT='docs/reviews/coder-daemon-identity-race-20261022/'
ref=sys.argv[1] if len(sys.argv)>1 else 'HEAD'
def git(*a):return subprocess.check_output(['git',*a])
def read(p):return git('show',ref+':'+ROOT+p)
def obj(p):return json.loads(read(p))
def sha(b):return hashlib.sha256(b).hexdigest()
s=obj('SOURCE.json');assert git('rev-parse',s['source']+'^{tree}').decode().strip()==s['tree']
assert git('diff','--name-only',s['base'],ref,'--',':(exclude)docs').decode().splitlines()==[s['test_path']]
b=git('show',ref+':'+s['test_path']);assert len(b)==s['test_bytes'] and sha(b)==s['test_sha256']
assert b==git('show',s['source']+':'+s['test_path'])
m=obj('MANIFEST.json');files=git('ls-tree','-r','-z','--name-only',ref).decode().rstrip('\0').split('\0')
assert {ROOT+x['path'] for x in m['files']}|{ROOT+'MANIFEST.json'}=={x for x in files if x.startswith(ROOT)}
for x in m['files']:
 b=read(x['path']);assert len(b)==x['bytes'] and sha(b)==x['sha256'],x['path']
result=obj('raw/RESULT.json');assert result['source']==s['source'] and result['workers']==4 and result['module_runs']==20 and result['total_test_executions']==360 and result['failures']==0 and result['test_sha256']==s['test_sha256']
rows=obj('raw/STRESS.json')['rows'];assert len(rows)==20
for i in range(5):
 batch=rows[i*4:(i+1)*4];assert max(x['start_ns'] for x in batch)<min(x['end_ns'] for x in batch)
for row in rows:
 assert row['rc']==0 and row['source']==s['source'] and row['isolated_HOME'] and row['isolated_TMPDIR']
 text=read('raw/'+row['name']+'.stderr');assert b'Ran 18 tests' in text and text.rstrip().endswith(b'OK') and b'skipped=' not in text
 assert read('raw/'+row['name']+'.rc').strip()==b'0'
proofs=obj('raw/PROOFS.json')['rows'];assert [x['name'] for x in proofs]==['delay-positive','delay-withdrawal','delay-restored','missing-notification']
for x,rc in zip(proofs,[0,1,0,1]):
 assert x['rc']==rc and x['source']==s['source'] and read('raw/'+x['name']+'.rc').strip()==str(rc).encode()
 text=read('raw/'+x['name']+'.stderr')
 if rc:assert b'OBSERVE_REJECTED_CONNECTION' in text and b'AssertionError' in text and b'FAIL:' in text and b'ERROR:' not in text
 else:assert text.rstrip().endswith(b'OK') and x['test_copy_sha256']==s['test_sha256']
assert 4.5<(proofs[-1]['end_ns']-proofs[-1]['start_ns'])/1e9<8
print('VERIFY_OK test-only; 4 workers x 5 waves = 20 modules / 360 tests rc0; delayed withdrawal/restored and bounded missing-notification verified')
