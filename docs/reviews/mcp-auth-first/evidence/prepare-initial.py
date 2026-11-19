from pathlib import Path
import json,subprocess,hashlib,gzip,datetime
A=Path(__file__).resolve().parent;W=Path('/data/ai-dev-data/mcp-auth-first');D=W/'docs/reviews/mcp-auth-first';S='ce9b7a959e828ad717b8708651c9094a421272ec'
def git(*args):return subprocess.check_output(['git',*args],cwd=W)
def sha(b):return hashlib.sha256(b).hexdigest()
assert git('rev-parse','HEAD').decode().strip()==S
D.mkdir(parents=True,exist_ok=True)
manifest=[]
for f in git('diff','--name-only','206d41b26dab646c0e50a111eed5a45ccf7c72f6',S).decode().splitlines():
 if f.startswith(('docs/','openspec/')):continue
 b=git('show',S+':'+f);manifest.append({'path':f,'blob':git('rev-parse',S+':'+f).decode().strip(),'sha256':sha(b)})
assert len(manifest)==11
(D/'product-manifest.json').write_text(json.dumps({'source':S,'tree':git('rev-parse',S+'^{tree}').decode().strip(),'files':manifest},indent=2)+'\n')
# Snapshot only completed/stopped attempts. The live m8 log is deliberately omitted.
idx=[]
for f in sorted(A.rglob('*')):
 if not f.is_file():continue
 rel=f.relative_to(A)
 if f.name in ['REPORT-DRAFT.md','VERIFY-DRAFT.py','prepare-evidence.py','prepare-initial.py']:continue
 if str(rel).startswith(('go-count3-anchor.','z3-anchor-')):continue
 b=f.read_bytes();out=D/'evidence'/rel;payload=b
 if f.suffix=='.log':out=out.with_suffix('.log.gz');payload=gzip.compress(b,mtime=0)
 out.parent.mkdir(parents=True,exist_ok=True);out.write_bytes(payload);idx.append({'raw_path':str(rel),'stored_path':str(out.relative_to(D)),'raw_bytes':len(b),'raw_sha256':sha(b),'stored_sha256':sha(payload)})
(D/'RAW-INDEX.json').write_text(json.dumps(idx,indent=2)+'\n')
r={n:json.loads((A/(n+'.result.json')).read_text()) for n in ['canonical-anchor','strict-anchor']}
m=json.loads((A/'mutations-anchor/summary.json').read_text());assert len(m['mutations'])==12 and all(x['build']['rc']==0 and x['business_red'] for x in m['mutations']);assert all(x['rc']==0 for x in r.values())
(D/'gate-summary.json').write_text(json.dumps({'source':S,'snapshot_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'status':'INITIAL_REVIEW_CODE_FIXED_M8_Z3_PENDING','gates':r,'mutations':12,'m8':'count3 command still finishing; one known N4 test failure observed; fresh serial five queued, not a gate PASS','baseline_count20':json.loads((A/'z3-baseline-count20.result.json').read_text())},ensure_ascii=False,indent=2)+'\n')
head='> 初交审快照：遵组长2026-11-19T05:10:12Z令先推固定产品／测试ce9b7a95。canonical、strict、12刀与还原基线已完成；m8三遍出现已知N4，Z3全新五轮仍待完成。**不记全门PASS。** 复核与五轮并行，后续仅追加文档。\n\n'
(D/'REPORT.md').write_text(head+(A/'REPORT-DRAFT.md').read_text())
(D/'INITIAL-VERIFY.py').write_text('''from pathlib import Path\nimport json,gzip,hashlib,subprocess\np=Path(__file__).resolve().parent\ndef sha(b):return hashlib.sha256(b).hexdigest()\nfor x in json.loads((p/'RAW-INDEX.json').read_text()):\n b=(p/x['stored_path']).read_bytes();assert sha(b)==x['stored_sha256'];b=gzip.decompress(b) if x['stored_path'].endswith('.gz') else b;assert sha(b)==x['raw_sha256']\nw=Path(subprocess.check_output(['git','rev-parse','--show-toplevel'],cwd=p).decode().strip());m=json.loads((p/'product-manifest.json').read_text())\nfor x in m['files']:\n b=subprocess.check_output(['git','show',m['source']+':'+x['path']],cwd=w);assert sha(b)==x['sha256'] and b==(w/x['path']).read_bytes()\nprint('INITIAL_VERIFY_OK code-fixed; full m8 Z3 pending, NOT_FULL_PASS')\n''')
# SHA list is rooted at repository root.
entries=[]
for f in sorted(D.rglob('*')):
 if f.is_file() and f.name!='SHA256SUMS':entries.append(sha(f.read_bytes())+'  '+str(f.relative_to(W)))
(D/'SHA256SUMS').write_text('\n'.join(entries)+'\n');print('initial evidence files',len(entries))
