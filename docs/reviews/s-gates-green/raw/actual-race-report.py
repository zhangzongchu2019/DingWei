from pathlib import Path
from collections import Counter,defaultdict
import subprocess,json,re
raw=Path('/tmp/s-gates-green-20261108');plan=json.loads((raw/'RACE-SHARD-PLAN.json').read_text())
assert len(plan['all_tests'])==379
allnames=sorted(plan['all_tests']);assigned=[n for g in plan['assignments'] for n in g]
assert len(assigned)==len(set(assigned))
(raw/'race-list-all.txt').write_text('\n'.join(allnames)+'\n')
(raw/'race-list-union.txt').write_text('\n'.join(sorted(set(assigned)))+'\n')
r=subprocess.run(['diff','-u',str(raw/'race-list-all.txt'),str(raw/'race-list-union.txt')],capture_output=True,text=True)
(raw/'race-inventory.diff').write_text(r.stdout+r.stderr);assert r.returncode==0
report={'list_diff_argv':['diff','-u','race-list-all.txt','race-list-union.txt'],'list_diff_rc':r.returncode,'list_diff_bytes':len(r.stdout+r.stderr),'overlap':[],'unique_cases':len(allnames),'expected_top_level_outcomes':len(allnames)*3,'shards':[]};skips=[];all_counts=Counter();actual_names=[]
for i,group in enumerate(plan['assignments']):
 meta=json.loads((raw/f'race-store-shard-{i}.json').read_text());assert meta.get('rc')==0
 counts={n:Counter() for n in group};actual=set();output=defaultdict(list);child_skips=Counter()
 for l in (raw/f'race-store-shard-{i}.log').read_text().splitlines():
  try:e=json.loads(l)
  except ValueError:continue
  n=e.get('Test','');a=e.get('Action')
  if a=='output' and n:output[n].append(e.get('Output',''))
  if n and '/' not in n and a=='run':actual.add(n)
  if n in counts and a in ('run','pass','skip','fail'):counts[n][a]+=1
  if n and a=='skip':child_skips[n]+=1
 assert actual==set(group),(i,actual^set(group))
 actual_names.extend(sorted(actual))
 sums=Counter()
 for n,c in counts.items():
  assert c['run']==3 and c['pass']+c['skip']+c['fail']==3 and c['fail']==0,(i,n,c)
  sums.update({k:c[k] for k in ('pass','skip','fail')})
 for n,k in child_skips.items():
  messages=sorted(set(x.strip() for x in ''.join(output[n]).splitlines() if re.search(r'_test\.go:\d+:',x)))
  skips.append({'shard':i,'name':n,'skip_count':k,'conditions':messages,'top_level':'/' not in n})
 assert sum(sums.values())==len(group)*3
 all_counts.update(sums);report['shards'].append({'shard':i,'rc':meta['rc'],'unique_cases':len(group),'outcomes':dict(sums),'case_outcomes':{n:dict(c) for n,c in counts.items()}})
assert sum(all_counts.values())==379*3
assert len(actual_names)==379 and len(set(actual_names))==379
(raw/'race-list-union.txt').write_text('\n'.join(sorted(actual_names))+'\n')
r=subprocess.run(['diff','-u',str(raw/'race-list-all.txt'),str(raw/'race-list-union.txt')],capture_output=True,text=True)
(raw/'race-inventory.diff').write_text(r.stdout+r.stderr);assert r.returncode==0
report.update(list_diff_rc=r.returncode,list_diff_bytes=len(r.stdout+r.stderr),union_source='actual top-level run actions in all shard JSON logs')
report['totals']=dict(all_counts);report['skip_cases']=skips
# The independent extseat full package has no -run restriction.
meta=json.loads((raw/'race-extseat-full.json').read_text());assert meta.get('rc')==0
ext=defaultdict(Counter)
for l in (raw/'race-extseat-full.log').read_text().splitlines():
 try:e=json.loads(l)
 except ValueError:continue
 n=e.get('Test','');a=e.get('Action')
 if n and '/' not in n and a in ('run','pass','skip','fail'):ext[n][a]+=1
assert ext and all(v['run']==3 and v['pass']==3 and not v['skip'] and not v['fail'] for v in ext.values())
report['extseat_full']={'rc':0,'argv':meta['argv'],'unique_cases':len(ext),'outcomes':{'pass':len(ext)*3,'fail':0,'skip':0}}
(raw/'RACE-FINAL-REPORT.json').write_text(json.dumps(report,indent=2)+'\n')
s='# race分片跳过项\n\n统计顶层结果时仅计go test-list中的379项，避免父/子重复计数；子用例若跳过仍在下表另列。每片为独立进程、片内默认串行。\n\n| 片 | 用例 | Skip轮次 | 条件 |\n| --- | --- | --- | --- |\n'
for x in skips:s+=f"| {x['shard']} | {x['name']} | {x['skip_count']} | "+'; '.join(x['conditions']).replace('|','\\|')+' |\n'
(raw/'RACE-SKIPS.md').write_text(s)
print('RACE_REPORT_OK',dict(all_counts),'sum',sum(all_counts.values()),'extseat',len(ext)*3)
