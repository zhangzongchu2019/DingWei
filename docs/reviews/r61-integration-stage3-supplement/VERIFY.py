#!/usr/bin/env python3
"""只读核对本次追加证据及固定产品源；不执行探针或连接服务。"""
from pathlib import Path
import json,hashlib,gzip,subprocess
D=Path(__file__).resolve().parent;T=D.parents[2];E=D/'evidence'
def git(*a):return subprocess.check_output(['git',*a],cwd=T)
def js(n):return json.loads((E/n).read_text())
def sha(b):return hashlib.sha256(b).hexdigest()
SOURCE='4581b34ac1e4b9973d12c559cf6ac7772107bebf'
PRIOR='a85b6c7d2fbe3bc94ab337dda266d8033662cc26'
assert git('merge-base','--is-ancestor',PRIOR,'HEAD')==b''
changed=git('diff','--name-only',SOURCE,'HEAD').decode().splitlines()
assert all(p.startswith(('docs/','openspec/')) for p in changed),changed
assert git('rev-parse',SOURCE+'^{tree}').decode().strip()=='5c73e93df0255e73137428a9376b65321e91279a'
assert git('show',SOURCE+':VERSION').strip()==b'0.2.3.40'
for p in ['internal','cmd','tools','scripts','deploy','view-service','go.mod','go.sum','VERSION']:
 assert git('rev-parse',SOURCE+':'+p)==git('rev-parse','HEAD:'+p),p
for row in js('raw-logs.json'):
 b=(T/row['archive_path']).read_bytes();assert sha(b)==row['archive_sha256']
 assert sha(gzip.decompress(b))==row['published_sha256']
b=js('baseline0ab-newsession20.json')
assert b['source']=='0ab43c630c37fdf01ce68d5c66c0db0141a682af' and b['rc']==0 and b['top_pass']==20 and not b['failures'] and b['interrupted_lines']==0
assert b['sha256']==sha(gzip.decompress((E/'raw/baseline0ab-newsession20.log.gz').read_bytes()))
r=js('restart-result.json')
assert r['source']==SOURCE and r['full_event_equality'] and r['different_os_processes']
assert r['hub_process_before']!=r['hub_process_after'] and r['same_app_db'] and r['same_history_db']
assert r['before']==r['after'] and len(r['before']['events'])==2
provenance=[x['event']['provenance'] for x in r['before']['events']]
assert {p['display_name'] for p in provenance}=={'opus50ts','timer'}
assert all(p['kind']=='agent' and p['attested_by']=='hub' and p['trust_decision']=='trusted' for p in provenance)
for n in ['restart-build','restart-test','restart-browser-run']:
 j=js(n+'.json');assert j['source']==SOURCE and j['rc']==0
 assert j['sha256']==sha(gzip.decompress((E/'raw'/('restart-browser.log.gz' if n=='restart-browser-run' else n+'.log.gz')).read_bytes()))
for n in ['restart-build-overlay-vet-failed','restart-test-attempt1','restart-test-attempt2']:
 assert js(n+'.json')['rc']!=0
b=js('restart-browser-result.json');assert b['source']==SOURCE and b['rc']==0
assert len(b['readings'])==2 and all(x['icons']==2 and x['fresh_browser_context'] and not x['sse_replay'] for x in b['readings'])
assert b['readings'][0]['titles']==b['readings'][1]['titles']==['opus50ts','timer']
p=js('probe-build-provenance.json')
assert p['probe_sha256']==sha((E/'restart_probe_test.go').read_bytes()) and p['overlay_sha256']==sha((E/'overlay.json').read_bytes())
i=js('own-inbox-metadata.json');assert 'cannot individually map' in i['mapping_limit']
assert [x['state'] for x in i['evidence_rows']]==['admission_unknown','processed','admission_unknown']
# 旧全门由不可变Git证据单独取出，避免把新文档误称旧strict运行时的内容。
g=json.loads(git('show',PRIOR+':docs/reviews/r61-integration-stage2-n3/evidence/gates.json'))
assert all(x['source']==SOURCE for x in g)
assert {x['name']:x['rc'] for x in g}=={'canonical':0,'m8-store-full3':1,'focused-race':0,'browser-full':0,'strict':0}
if (D/'MANIFEST.json').exists():
 m=json.loads((D/'MANIFEST.json').read_text())
 for f,h in m['files'].items():assert sha((T/f).read_bytes())==h,f
 for f,h in m['product_files'].items():assert sha(git('show',SOURCE+':'+f))==h and sha((T/f).read_bytes())==h,f
if (E/'final-document-strict.json').exists():
 s=js('final-document-strict.json');assert s['rc']==0 and s['passed']==45 and s['failed']==0
 assert sha((E/'final-document-strict.txt').read_bytes())==s['sha256']
print('VERIFY_OK product',SOURCE,'baseline0ab=20/0 restart_http_equal=2 cold_browser_icons=2/2 prior_gates_preserved=true')
