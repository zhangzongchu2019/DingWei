#!/usr/bin/env python3
"""从仓根或报告目录执行；只读 Git 对象、证据与当前产品。"""
from pathlib import Path
import gzip,hashlib,json,subprocess,re
D=Path(__file__).resolve().parent
W=Path(subprocess.check_output(['git','rev-parse','--show-toplevel'],cwd=D).decode().strip())
def digest(b):return hashlib.sha256(b).hexdigest()
def git(*args):return subprocess.check_output(['git',*args],cwd=W)
manifest=json.loads((D/'product-manifest.json').read_text());S=manifest['source']
assert git('rev-parse',S+'^{tree}').decode().strip()==manifest['tree']
assert subprocess.run(['git','merge-base','--is-ancestor','206d41b26dab646c0e50a111eed5a45ccf7c72f6',S],cwd=W).returncode==0
for f in manifest['files']:
 b=git('show',S+':'+f['path']);assert digest(b)==f['sha256'];assert git('rev-parse',S+':'+f['path']).decode().strip()==f['blob'];assert (W/f['path']).read_bytes()==b
assert len(manifest['files'])==11
extra=git('diff','--name-only',S,'HEAD').decode().splitlines();assert all(p.startswith(('docs/','openspec/')) for p in extra)
index=json.loads((D/'RAW-INDEX.json').read_text());raw={}
for e in index:
 b=(D/e['stored_path']).read_bytes();assert digest(b)==e['stored_sha256'];b=gzip.decompress(b) if e['stored_path'].endswith('.gz') else b
 assert digest(b)==e['raw_sha256'] and len(b)==e['raw_bytes'];raw[e['raw_path']]=b
summary=json.loads((D/'gate-summary.json').read_text());assert summary['source']==S
for name in ['canonical-anchor','go-count3-anchor','strict-anchor']:
 result=json.loads(raw[name+'.result.json']);assert result['source']==S
 if name!='go-count3-anchor':assert result['rc']==0
 command=json.loads(raw[name+'.command.json']);assert command['source']==S
assert b'BUILD_TESTS_OK helper_profiles=2 python=27 shell=2' in raw['canonical-anchor.log']
if json.loads(raw['go-count3-anchor.result.json'])['rc']!=0:
 z=json.loads(raw['z3-anchor-summary.json']);assert z['source']==S and z['state']=='PASS_POLICY' and len(z['rounds'])==5 and z['fully_green']>=3
 assert all(x['allowed'] for x in z['initial_failures'])
 for i,r in enumerate(z['rounds'],1):
  assert r['source']==S and (r['rc']==0 or r['failures'] and all(x['allowed'] for x in r['failures']))
  assert json.loads(raw['z3-anchor-round-'+str(i)+'.result.json'])==r
else:assert not re.search(rb'^--- FAIL:',raw['go-count3-anchor.log'],re.M)
assert b'PostgreSQL) 18.3' in raw['anchor-pg-runtime.json']
for name in ['env-anchor-canonical.json','env-anchor-go.json']:
 e=json.loads(raw[name]);assert e['SOURCE_COMMIT']==S and e['HUB_TEST_PG_STRICT']=='1' and e['DW_DESKTOP_INTEGRATION_PYTHON'];assert e['PYTHONPATH'].endswith('/mcp-auth-first/tools/sessionhelper')
 assert not any(k.startswith(('SH_','DINGWEI_')) or any(x in k.upper() for x in ['SECRET','TOKEN','PASSWORD','COOKIE']) for k in e)
m=json.loads(raw['mutations-anchor/summary.json']);assert m['source']==S and len(m['mutations'])==12 and m['porcelain']==''
for part in ['baseline','final_baseline']:
 r=m[part];assert r['rc']==0 and digest(raw['mutations-anchor/'+r['log']])==r['sha256']
for x in m['mutations']:
 assert x['build']['rc']==0 and x['test']['rc']!=0 and x['business_red'] and x['failed_assertions']
 for part in ['build','test']:
  r=x[part];b=raw['mutations-anchor/'+r['log']];assert digest(b)==r['sha256'];assert b'[build failed]' not in b and b'panic:' not in b
 b=git('show',S+':'+x['path']);assert x['old'].encode() in b
negative=json.loads(raw['old-product-overlay-anchor.json']);assert negative['test_source']==S and negative['rc']!=0 and negative['compile_ok'] and negative['business_red'] and negative['porcelain']==''
assert digest(git('show',S+':internal/mcpgw/auth_first_test.go'))==negative['test_sha256'];assert digest(raw['old-product-overlay-anchor.log'])==negative['log_sha256']
print('VERIFY_OK',S,'products',len(manifest['files']),'raw_files',len(index),'mutations',len(m['mutations']),'final_gates',len(summary['gates']))
