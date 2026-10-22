"""Verify immutable source and all supplied evidence; no installation or cleanup."""
import hashlib,json,os,subprocess
from pathlib import Path
BASE=Path(__file__).resolve().parent;ROOT=BASE.parents[2]
meta=json.loads((BASE/'FINAL-INPUT.json').read_text());source=meta['source_commit']
assert subprocess.check_output(['git','rev-parse',meta['source_tag']+'^{commit}'],cwd=ROOT,text=True).strip()==source
assert subprocess.check_output(['git','rev-parse',source+'^{tree}'],cwd=ROOT,text=True).strip()==meta['source_tree']
subprocess.check_call(['git','merge-base','--is-ancestor',meta['base_commit'],source],cwd=ROOT)
rows=json.loads((BASE/meta['gate_directory']/'SOURCE.json').read_text())
for row in rows:
 p=Path(row['path']);assert not p.is_absolute() and '..' not in p.parts
 target=ROOT/p;raw=os.fsencode(os.readlink(target)) if row['mode']=='120000' else target.read_bytes()
 assert len(raw)==row['size'] and hashlib.sha256(raw).hexdigest()==row['sha256'],str(p)
 assert subprocess.check_output(['git','cat-file','blob',row['blob']],cwd=ROOT)==raw,str(p)
 assert subprocess.check_output(['git','rev-parse',source+':'+str(p)],cwd=ROOT,text=True).strip()==row['blob'],str(p)
source_count=len(rows)
rows=json.loads((BASE/'EVIDENCE-MANIFEST.json').read_text())
for row in rows:
 p=Path(row['path']);assert not p.is_absolute() and '..' not in p.parts
 target=BASE/p;assert not target.is_symlink();raw=target.read_bytes()
 assert len(raw)==row['size'] and hashlib.sha256(raw).hexdigest()==row['sha256'],str(p)
print('HELPER_BOOT_ORIGIN_VERIFY_OK source='+str(source_count)+' evidence='+str(len(rows)))

result=json.loads((BASE/meta['gate_directory']/'RESULT.json').read_text())
assert result['source']==source and result['passed'] and all(row['rc']==0 for row in result['rows'])
withdrawals=json.loads((BASE/meta['gate_directory']/'withdrawals/RESULT.json').read_text())
assert withdrawals['mutations']==12
reds=[row for row in withdrawals['rows'] if row['label'].endswith('-withdrawn')]
assert len(reds)==12 and all(row['designated_red'] for row in reds)
assert all(row['rc']==0 for row in withdrawals['rows'] if not row['label'].endswith('-withdrawn'))
print('BOOT_ORIGIN_GATES_AND_12_WITHDRAWALS_VERIFIED')
