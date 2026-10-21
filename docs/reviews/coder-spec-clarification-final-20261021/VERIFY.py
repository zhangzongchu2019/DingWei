#!/usr/bin/env python3
"""Verify retained artifacts and every frozen Git input without executing the product."""
import hashlib,json,subprocess
from pathlib import Path
HERE=Path(__file__).resolve().parent
ROOT=HERE.parents[2]
def main():
 evidence=json.loads((HERE/'SHA-MANIFEST.json').read_text())
 for row in evidence['files']:
  path=HERE/row['path'];data=path.read_bytes()
  assert len(data)==row['bytes'] and hashlib.sha256(data).hexdigest()==row['sha256'],row['path']
 source=json.loads((HERE/'SOURCE-MANIFEST.json').read_text())
 tree=subprocess.check_output(['git','rev-parse',source['source']+'^{tree}'],cwd=ROOT,text=True).strip()
 assert tree==source['tree']
 expected={}
 for item in subprocess.check_output(['git','ls-tree','-rz',source['source']],cwd=ROOT).split(b'\0'):
  if not item:continue
  header,path=item.split(b'\t',1);mode,kind,blob=header.decode().split();path=path.decode()
  if kind=='blob' and not path.startswith(('docs/','evidence/')):expected[path]=(mode,blob)
 assert len(expected)==source['count']==len(source['files'])
 p=subprocess.Popen(['git','cat-file','--batch'],cwd=ROOT,stdin=subprocess.PIPE,stdout=subprocess.PIPE)
 for row in source['files']:
  assert expected.pop(row['path'])==(row['mode'],row['blob'])
  p.stdin.write((row['blob']+'\n').encode());p.stdin.flush()
  parts=p.stdout.readline().decode().split();size=int(parts[2]);data=p.stdout.read(size);assert p.stdout.read(1)==b'\n'
  assert size==row['bytes'] and hashlib.sha256(data).hexdigest()==row['sha256'],row['path']
 p.stdin.close();p.stdout.close();assert p.wait()==0 and not expected
 checks=json.loads((HERE/'CHECKS.json').read_text())
 assert checks['source']==source['source']
 mutations=json.loads((HERE/'withdrawals/MUTATIONS.json').read_text())
 assert mutations['source']==source['source'] and len(mutations['rows'])==38
 for row in mutations['rows']:
  assert row['phases']['before']['rc']==0 and row['phases']['restored']['rc']==0
  assert row['phases']['withdrawn']['rc']!=0 and row['phases']['withdrawn']['marker_present']
 print('CLARIFICATION_EVIDENCE_VERIFIED source_files='+str(source['count'])+' artifacts='+str(len(evidence['files']))+' withdrawals=38')
if __name__=='__main__':main()
