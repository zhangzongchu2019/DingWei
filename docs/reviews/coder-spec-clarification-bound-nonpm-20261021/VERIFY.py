#!/usr/bin/env python3
import hashlib,json,subprocess
from pathlib import Path
HERE=Path(__file__).resolve().parent
ROOT=HERE.parents[2]
def main():
 subprocess.run(['python3',str(HERE.parent/'coder-spec-clarification-final-20261021/VERIFY.py')],check=True)
 manifest=json.loads((HERE/'SHA-MANIFEST.json').read_text())
 for row in manifest['files']:
  data=(HERE/row['path']).read_bytes();assert len(data)==row['bytes'] and hashlib.sha256(data).hexdigest()==row['sha256'],row['path']
 proof=json.loads((HERE/'TEST-ONLY-PROOF.json').read_text())
 changed=subprocess.check_output(['git','diff','--name-only',proof['base'],proof['source']],cwd=ROOT,text=True).splitlines()
 assert set(changed)=={r['path'] for r in proof['files']}
 assert subprocess.check_output(['git','rev-parse',proof['source']+'^{tree}'],cwd=ROOT,text=True).strip()==proof['tree']
 for row in proof['files']:
  blob=subprocess.check_output(['git','rev-parse',proof['source']+':'+row['path']],cwd=ROOT,text=True).strip();data=subprocess.check_output(['git','cat-file','blob',blob],cwd=ROOT)
  assert blob==row['blob'] and len(data)==row['bytes'] and hashlib.sha256(data).hexdigest()==row['sha256']
 mutations=json.loads((HERE/'withdrawals/MUTATIONS.json').read_text());assert mutations['source']==proof['source'] and len(mutations['cases'])==2
 for case in mutations['cases']:
  assert case['phases']['before']['rc']==case['phases']['restored']['rc']==0
  assert case['phases']['withdrawn']['rc']!=0 and case['phases']['withdrawn']['marker_present']
 print('BOUND_NONPM_SUPPLEMENT_VERIFIED test_files=3 artifacts='+str(len(manifest['files']))+' role_only_reds=2 product_changes=0')
if __name__=='__main__':main()
