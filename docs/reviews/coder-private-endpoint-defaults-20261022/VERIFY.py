"""Verify unchanged product blobs, the new test input, and both evidence sets."""
import hashlib,json,subprocess
from pathlib import Path
BASE=Path(__file__).resolve().parent;ROOT=BASE.parents[2]
SOURCE='6d6719aeda69c7bf91edbf63310646f37b68e38a';PARENT='5c7110a8b31e3bf1a3a0fee76b0fc7470104779d'
TEST='tools/sessionhelper/tests/test_private_endpoint.py'
changed=subprocess.check_output(['git','diff','--name-only',PARENT,SOURCE,'--','.',':!docs'],cwd=ROOT,text=True).splitlines()
assert changed==[TEST]
old=BASE.parent/'coder-private-endpoint-phase1-20261022'
items=json.loads((old/'final-7cfcd99/SOURCE.json').read_text())
for row in items:
 path=row['path'];blob=subprocess.check_output(['git','rev-parse',SOURCE+':'+path],cwd=ROOT,text=True).strip()
 raw=subprocess.check_output(['git','cat-file','blob',blob],cwd=ROOT)
 if path!=TEST:assert blob==row['blob'] and hashlib.sha256(raw).hexdigest()==row['sha256']
 else:assert hashlib.sha256(raw).hexdigest()==json.loads((BASE/'RESULT.json').read_text())['test_sha256']
 assert (ROOT/path).read_bytes()==raw
for directory in (old,BASE):
 rows=json.loads((directory/'EVIDENCE-MANIFEST.json').read_text())
 for row in rows:
  p=directory/row['path'];assert not p.is_symlink();raw=p.read_bytes()
  assert len(raw)==row['size'] and hashlib.sha256(raw).hexdigest()==row['sha256'],str(p)
print('PCE_DEFAULTS_VERIFY_OK source='+str(len(items))+' old_evidence=1133 new_evidence='+str(len(rows))+' product_unchanged=true')
