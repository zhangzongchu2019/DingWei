#!/usr/bin/env python3
"""从固定 Git 对象、当前产品与原始门日志核验本轮作者证据。"""
import gzip,hashlib,json,subprocess
from pathlib import Path
here=Path(__file__).resolve().parent
root=Path(subprocess.check_output(['git','rev-parse','--show-toplevel'],text=True).strip())
def sha(b):return hashlib.sha256(b).hexdigest()
def raw(v,field='raw_file',digest='raw_sha256'):
 assert sha(gzip.decompress((here/v[field]).read_bytes()))==v[digest],v[field]
m=json.loads((here/'product-manifest.json').read_text());source=m['source_commit']
for f in m['files']:
 assert sha(subprocess.check_output(['git','show',source+':'+f['path']],cwd=root))==f['sha256'],('Git',f['path'])
 assert sha((root/f['path']).read_bytes())==f['sha256'],('current product',f['path'])
for v in json.loads((here/'gate-summary.json').read_text()):
 assert v['source_commit']==source and v['rc']==0 and not v['workspace_changed_during_run'];raw(v)
for v in json.loads((here/'gate-history.json').read_text()):raw(v)
mut=json.loads((here/'mutations.json').read_text());assert len(mut)==55 and len({v['name'] for v in mut})==55
for v in mut:
 assert v['source_commit']==source and v['build_rc']==0 and v['rc']!=0 and v['failed_assertions'];raw(v);raw(v,'compile_raw_file','compile_raw_sha256')
for v in json.loads((here/'mutation-history.json').read_text()):raw(v);raw(v,'compile_raw_file','compile_raw_sha256')
b=[v for v in json.loads((here/'mutation-baselines.json').read_text()) if v['attempt_label']=='final'];assert len(b)==2
for v in b:assert v['source_commit']==source and v['rc']==0;raw(v)
print('VERIFY_OK',source,'products',len(m['files']),'mutations',len(mut),'final_baselines',len(b))
