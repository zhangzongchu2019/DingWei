import datetime,gzip,hashlib,json,re,shutil,subprocess
from pathlib import Path
W=Path('/data/ai-dev-data/r62-extseat-groups');R=Path(__file__).resolve().parent;D=W/'docs/reviews/r62-extseat-groups';D.mkdir(parents=True,exist_ok=True)
for p in sorted(R.rglob('*')):
 if not p.is_file() or p.suffix not in ('.json','.log','.py','.txt','.md'):continue
 rel=p.relative_to(R);dest=D/'evidence'/rel
 if p.suffix=='.log':dest=dest.with_suffix('.log.gz')
 dest.parent.mkdir(parents=True,exist_ok=True)
 if p.suffix=='.log':
  with dest.open('wb') as out:
   with gzip.GzipFile(filename='',mode='wb',fileobj=out,mtime=0) as gz:gz.write(p.read_bytes())
 else:shutil.copyfile(p,dest)
source='aaed8aaee6630e2683096c977a058da653fee7e2';paths=subprocess.check_output(['git','diff','--name-only','206d41b2',source],cwd=W,text=True).splitlines();items=[]
for p in paths:
 if p.startswith(('docs/','openspec/')):continue
 raw=subprocess.check_output(['git','show',source+':'+p],cwd=W);items.append({'path':p,'sha256':hashlib.sha256(raw).hexdigest()});assert (W/p).read_bytes()==raw,p
(D/'product-manifest.json').write_text(json.dumps({'source':source,'tree':subprocess.check_output(['git','rev-parse',source+'^{tree}'],cwd=W,text=True).strip(),'files':items},indent=2)+'\n')
print('products',len(items),'evidence files',sum(p.is_file() for p in (D/'evidence').rglob('*')))
