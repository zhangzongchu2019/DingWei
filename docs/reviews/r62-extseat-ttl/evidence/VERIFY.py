#!/usr/bin/env python3
"""在仓库根运行本脚本；核冻结源、当前产品字节与原日志。"""
from pathlib import Path
import subprocess,json,hashlib,gzip
D=Path(__file__).resolve().parent
T=Path(subprocess.check_output(['git','rev-parse','--show-toplevel'],cwd=D,text=True).strip())
def sha(x):return hashlib.sha256(x).hexdigest()
def git(*args):return subprocess.check_output(['git',*args],cwd=T)
m=json.loads((D/'product-manifest.json').read_text());s=m['source']
assert git('rev-parse',s+'^{tree}').decode().strip()==m['tree']
assert git('rev-parse',s+'^').decode().strip()==m['parent']
for f in m['files']:
 b=git('show',s+':'+f['path']);assert sha(b)==f['sha256'],f
 assert sha((T/f['path']).read_bytes())==f['sha256'],f
 assert git('rev-parse',s+':'+f['path']).decode().strip()==f['blob']
for r in json.loads((D/'raw-log-manifest.json').read_text()):
 b=(D/r['stored_path']).read_bytes();assert sha(b)==r['stored_sha256']
 assert sha(gzip.decompress(b))==r['raw_sha256']
for r in json.loads((D/'evidence/runs.json').read_text()):
 assert r['source']==s
 assert sha(gzip.decompress((D/('evidence/'+r['name']+'.log.gz')).read_bytes()))==r['sha256']
 if r['name'] in {'canonical','full-go3','strict','mutation-baseline','mutation-restored-baseline'}:assert r['rc']==0
cuts=json.loads((D/'evidence/mutations.json').read_text());assert len(cuts)==2
for c in cuts:
 assert c['compile_rc']==0 and c['rc']!=0 and c['failed_tests'] and c['assertion_lines']
 assert c['original_sha256']==c['restored_sha256']
 assert sha(gzip.decompress((D/('evidence/'+c['log']+'.gz')).read_bytes()))==c['sha256']
for line in (D/'SHA256SUMS').read_text().splitlines():
 h,p=line.split('  ',1);assert sha((T/p).read_bytes())==h,p
print('VERIFY_OK',s,'products',len(m['files']),'mutations',len(cuts))
