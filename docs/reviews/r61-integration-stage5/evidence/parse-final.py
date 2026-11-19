from pathlib import Path
import re,json
A=Path(__file__).resolve().parent
out={};skips={}
for phase in ['serial','postversion']:
 D=A/phase
 if not D.exists():continue
 for result in D.glob('*.result.json'):
  name=result.name.removesuffix('.result.json');p=D/(name+'.log')
  if not p.exists():continue
  ls=p.read_text(errors='replace').splitlines();key=phase+'/'+name
  r=json.loads(result.read_text());r.update(top_pass=sum(x.startswith('--- PASS:')for x in ls),all_pass=sum('--- PASS:'in x for x in ls),top_fail=sum(x.startswith('--- FAIL:')for x in ls),all_fail=sum('--- FAIL:'in x for x in ls),top_skip=sum(x.startswith('--- SKIP:')for x in ls),all_skip=sum('--- SKIP:'in x for x in ls),interrupted_lines=[{'line':i+1,'text':x}for i,x in enumerate(ls)if 'interrupted (9)'in x],data_race=sum('WARNING: DATA RACE'in x for x in ls),package_summary=[x for x in ls if re.match(r'^(ok\s|FAIL\s|\?\s)',x)])
  out[key]=r;skips[key]=[]
  for i,x in enumerate(ls):
   m=re.search(r'--- SKIP: (\S+)',x)
   if not m:continue
   n=m[1];j=i-1
   while j>=0 and not ls[j].startswith('=== RUN   '+n):j-=1
   context=[v for v in ls[max(j+1,i-25):i]if re.search(r'\.(go|py):\d+:',v)]
   skips[key].append({'name':n,'line':i+1,'condition_lines':context,'raw_log':phase+'/'+name+'.log'})
(A/'parsed-final-gates.json').write_text(json.dumps(out,ensure_ascii=False,indent=2)+'\n');(A/'SKIPS.json').write_text(json.dumps(skips,ensure_ascii=False,indent=2)+'\n')
print(json.dumps({k:{x:v[x]for x in ['rc','top_pass','all_pass','top_fail','all_skip','data_race']}for k,v in out.items()},indent=2))
