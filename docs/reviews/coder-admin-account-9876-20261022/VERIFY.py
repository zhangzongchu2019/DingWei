"""Verify the narrow test-only diff and original Go JSON evidence from Git."""
import hashlib
import json
import subprocess
import sys

HERE='docs/reviews/coder-admin-account-9876-20261022'
ref=sys.argv[1] if len(sys.argv)>1 else 'HEAD'
def git(*args):return subprocess.check_output(['git',*args])
def blob(p):return git('show',ref+':'+p)
def doc(p):return blob(HERE+'/'+p)
s=json.loads(doc('SOURCE.json'))
assert git('rev-parse',s['source']+'^{tree}').decode().strip()==s['tree']
assert git('diff','--name-only',s['base'],s['source']).decode().splitlines()==[s['path']]
assert [p for p in git('diff','--name-only',s['base'],ref).decode().splitlines() if not p.startswith('docs/')]==[s['path']]
b=blob(s['path']);assert b==git('show',s['source']+':'+s['path'])
assert len(b)==s['bytes'] and hashlib.sha256(b).hexdigest()==s['sha256']
assert git('rev-parse',s['source']+':'+s['path']).decode().strip()==s['blob']
m=json.loads(doc('MANIFEST.json'))
for item in m['files']:
 d=doc(item['path']);assert len(d)==item['bytes'] and hashlib.sha256(d).hexdigest()==item['sha256'],item['path']
actual={p[len(HERE)+1:] for p in git('ls-tree','-rz','--name-only',ref,'--',HERE).decode().split('\0') if p}
assert actual=={i['path'] for i in m['files']}|{'MANIFEST.json'}
c=json.loads(doc('CHECKS.json'));assert c['source']==s['source'] and c['base']==s['base']
assert c['env_i'] and c['isolated_HOME'] and c['strict_pg'] and not c['skip_ok'] and not c['dsn_set']
assert [r['name'] for r in c['rows']]==['target-green','withdrawal-red','package-restored-green']
for row in c['rows']:
 name=row['name'];assert doc('raw/'+name+'.rc').strip()==str(row['rc']).encode()
 events=[json.loads(line) for line in doc('raw/'+name+'.stdout').decode().splitlines() if line]
 skips=[e.get('Test') for e in events if e.get('Action')=='skip' and e.get('Test')]
 assert skips==row['skips']
 fails=[e for e in events if e.get('Action')=='fail']
 if row['expected_red']:
  assert row['rc']==1 and row['named_red']
  assert any(e.get('Test')=='TestAdminAccountRoutes' for e in fails)
  text=''.join(e.get('Output','') for e in events)
  assert '管理列表不得含 last4／密钥片段' in text and 'bob-api-9876' in text
 else:
  assert row['rc']==0 and not fails
  assert any(e.get('Action')=='pass' and not e.get('Test') for e in events)
  assert any(e.get('Action')=='pass' and e.get('Test')=='TestAdminAccountRoutes' for e in events)
print('VERIFY_OK test_only=1 target=PASS withdrawal=ASSERTION_RED package=PASS evidence_files='+str(len(m['files'])))
