"""Verify the final input/evidence manifests against immutable Git blobs."""
import hashlib,json,os
from pathlib import Path
import subprocess
BASE=Path(__file__).resolve().parent
ROOT=BASE.parents[2]
meta=json.loads((BASE/'FINAL-INPUT.json').read_text())
source=meta['source_commit'];tag=meta['source_tag']
assert subprocess.check_output(['git','rev-parse',tag+'^{commit}'],cwd=ROOT,text=True).strip()==source
assert subprocess.check_output(['git','rev-parse',source+'^{tree}'],cwd=ROOT,text=True).strip()==meta['source_tree']
subprocess.check_call(['git','merge-base','--is-ancestor',meta['base_commit'],source],cwd=ROOT)
items=json.loads((BASE/meta['source_manifest']).read_text())
for item in items:
 path=Path(item['path']);assert not path.is_absolute() and '..' not in path.parts
 target=ROOT/path
 raw=os.fsencode(os.readlink(target)) if item['mode']=='120000' else target.read_bytes()
 assert len(raw)==item['size'] and hashlib.sha256(raw).hexdigest()==item['sha256'],str(path)
 assert subprocess.check_output(['git','cat-file','blob',item['blob']],cwd=ROOT)==raw,str(path)
 assert subprocess.check_output(['git','rev-parse',source+':'+str(path)],cwd=ROOT,text=True).strip()==item['blob'],str(path)
evidence=json.loads((BASE/'EVIDENCE-MANIFEST.json').read_text())
for item in evidence:
 path=Path(item['path']);assert not path.is_absolute() and '..' not in path.parts
 target=BASE/path;assert not target.is_symlink()
 raw=target.read_bytes()
 assert len(raw)==item['size'] and hashlib.sha256(raw).hexdigest()==item['sha256'],str(path)
print('PRIVATE_ENDPOINT_VERIFY_OK source='+str(len(items))+' evidence='+str(len(evidence)))
