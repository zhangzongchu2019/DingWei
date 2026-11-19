#!/usr/bin/env python3
"""只读核交件Git对象与日志；不启动Hub、helper或数据库。"""
import gzip, hashlib, json, re, subprocess
from pathlib import Path
HERE=Path(__file__).resolve().parent
ROOT=HERE.parents[2]
def get(p):return json.loads((HERE/p).read_text())
def raw(p):
 p=HERE/p
 return p.read_bytes() if p.exists() else gzip.decompress(p.with_suffix(p.suffix+'.gz').read_bytes())
def obj(rev,p):return subprocess.check_output(['git','show',rev+':'+p],cwd=ROOT)
m=get('product-manifest.json');source=m['source']
subprocess.run(['git','cat-file','-e',source+'^{commit}'],cwd=ROOT,check=True)
for f in m['files']:
 b=obj(source,f['path']);assert hashlib.sha256(b).hexdigest()==f['sha256'],f
 assert obj('HEAD',f['path'])==b,f['path']
s=get('evidence/gate-summary.json')
for name,v in s.items():
 assert v['source']==source,name
 text=raw('evidence/'+name+'.log').decode()
 assert re.findall(r'^\s*--- FAIL:.*',text,re.M)==v['fail_lines'],name
 if name=='go-aaed8aaee':
  assert v['rc']==1 and len(v['fail_lines'])==1
  assert 'TestSessionNameReconnectKeepsNameAndReleaseReuses' in v['fail_lines'][0]
  assert len(v['interrupted_lines'])==1 and 'hub_test.go:3505' in v['interrupted_lines'][0]
 else:assert v['rc']==0 and not v['fail_lines'],name
assert 'BUILD_TESTS_OK helper_profiles=2 python=27 shell=2' in raw('evidence/canonical-aaed8aaee.log').decode()
receipt=raw('evidence/browser-aaed8aaee.receipt.txt').decode();rows=[x for x in receipt.splitlines() if x.startswith('suite.')]
assert len(rows)==46 and all(x.endswith('.rc=0') for x in rows) and 'aggregate_rc=0' in receipt
mu=get('evidence/mutations/summary.json');assert mu['source']==source and mu['baseline']['rc']==mu['final_baseline']['rc']==0
assert mu['original_sha256']==mu['restored_sha256'] and not mu['porcelain']
for e in mu['mutations']:
 assert e['build']['rc']==0 and e['business_red'] and e['reading']['rc']!=0,e['id']
 for r in [e['build'],e['reading']]:assert hashlib.sha256(raw('evidence/mutations/'+r['log'])).hexdigest()==r['log_sha256']
selfmu=get('evidence/self-mutation.json');assert selfmu['source']==source and selfmu['business_red'] and not selfmu['porcelain']
assert [s['rc'] for s in selfmu['results']]==[0,0,1]
assert 'external Agent self-invite granted membership' in raw('evidence/self-red.log').decode()
for d in get('evidence/n4-comparison.json'):
 text=raw('evidence/n4-'+d['label']+'.log').decode();assert text.count('--- PASS:')==d['pass'] and text.count('--- FAIL:')==d['fail']
 assert d['pass']+d['fail']==200
 assert all('TestSessionNameReconnectKeepsNameAndReleaseReuses' in line for line in text.splitlines() if line.startswith('--- FAIL:'))
assert get('evidence/strict-delivery.result.json')['rc']==0
print('VERIFY_OK',source,'products',len(m['files']),'mutations',len(mu['mutations'])+1,'known_N4_rc1_preserved')
