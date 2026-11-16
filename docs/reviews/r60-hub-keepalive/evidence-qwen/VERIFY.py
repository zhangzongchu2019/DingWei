#!/usr/bin/env python3
"""Verify this review directory against frozen Git objects and current files."""
import gzip,hashlib,json,subprocess
from pathlib import Path
here=Path(__file__).resolve().parent
root=Path(subprocess.check_output(['git','rev-parse','--show-toplevel'],text=True).strip())
def sha(b):return hashlib.sha256(b).hexdigest()
m=json.loads((here/'product-manifest.json').read_text())
for f in m['files']:
 original=subprocess.check_output(['git','show',m['source_commit']+':'+f['path']],cwd=root)
 assert sha(original)==f['sha256'],('Git object',f['path'])
 assert sha((root/f['path']).read_bytes())==f['sha256'],('current product',f['path'])
for v in json.loads((here/'gate-summary.json').read_text()):
 assert v['rc']==0 and v['source_commit']==m['source_commit']
 assert sha(gzip.decompress((here/v['raw_file']).read_bytes()))==v['raw_sha256']
for v in json.loads((here/'mutations.json').read_text()):
 assert v['build_rc']==0 and v['rc']!=0 and v['failed_assertions']
 assert v['source_commit']==m['source_commit']
 assert sha(gzip.decompress((here/v['raw_file']).read_bytes()))==v['raw_sha256']
print('VERIFY_OK',m['source_commit'],'products',len(m['files']))
