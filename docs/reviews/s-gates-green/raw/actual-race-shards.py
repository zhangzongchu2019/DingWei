from pathlib import Path
from concurrent.futures import ThreadPoolExecutor
from collections import Counter
import subprocess,json,re,os
raw=Path('/tmp/s-gates-green-20261108');root=Path('/data/ai-dev-data/s-gates-green')
names=[l for l in (raw/'race-inventory.log').read_text().splitlines() if re.fullmatch(r'(Test|Example|Fuzz)[A-Za-z0-9_]+',l)]
assert names and len(names)==len(set(names))
shards=[names[i::8] for i in range(8)]
plan={'all_tests':names,'assignments':shards,'count':3,'package':'./internal/store','source_manifest':'FROZEN-SOURCES.json'}
(raw/'RACE-SHARD-PLAN.json').write_text(json.dumps(plan,indent=2));print('RACE_SHARDS_START',len(names),'top-level cases',flush=True)
def run(i):
 pattern='^('+'|'.join(re.escape(x) for x in shards[i])+')$'
 return subprocess.run(['python3','/tmp/s-gates-green-run.py','race-store-shard-'+str(i),'go','test','-race','./internal/store','-count=3','-run',pattern,'-json','-timeout=120m']).returncode
with ThreadPoolExecutor(max_workers=8) as pool:rcs=list(pool.map(run,range(8)))
coverage={}
for i,group in enumerate(shards):
 assert rcs[i]==0,(i,rcs)
 counts={n:Counter() for n in group}
 for l in (raw/('race-store-shard-'+str(i)+'.log')).read_text().splitlines():
  try:e=json.loads(l)
  except ValueError:continue
  n=e.get('Test');a=e.get('Action')
  if n in counts and a in ('run','pass','skip','fail'):counts[n][a]+=1
 for n,c in counts.items():
  assert c['run']==3 and c['pass']+c['skip']==3 and c['fail']==0,(n,dict(c))
  assert n not in coverage;coverage[n]={'shard':i,**dict(c)}
assert set(coverage)==set(names)
(raw/'RACE-SHARD-COVERAGE.json').write_text(json.dumps({'count':3,'all_cases':len(names),'covered_cases':len(coverage),'package':'./internal/store','every_case_executed_three_times':True,'coverage':coverage},indent=2))
print('RACE_SHARDS_OK',len(coverage),'every case3',flush=True)
