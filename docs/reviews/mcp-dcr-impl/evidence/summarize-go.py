from pathlib import Path
import json,sys,re,collections
A=Path(__file__).resolve().parent
for name in sys.argv[1:]:
 path=A/(name+'.log');by={};outputs=collections.defaultdict(list);interrupts=[];bad=[]
 for lineno,line in enumerate(path.open(),1):
  try:e=json.loads(line)
  except json.JSONDecodeError:bad.append(lineno);continue
  pkg=e.get('Package','');test=e.get('Test','');action=e.get('Action');out=e.get('Output','');p=by.setdefault(pkg,dict(top_pass=0,all_pass=0,top_skip=0,all_skip=0,failures=[],skips=[],package_action=None,elapsed=None))
  if action=='output':
   outputs[(pkg,test)].append(out)
   if re.search(r'interrupted\s*\(9\)|SQLITE_INTERRUPT',out):interrupts.append(dict(line=lineno,package=pkg,test=test,output=out))
  if test and action=='pass':p['all_pass']+=1;p['top_pass']+=('/' not in test)
  if test and action=='skip':
   p['all_skip']+=1;p['top_skip']+=('/' not in test)
   reasons=[x.strip() for x in outputs[(pkg,test)] if re.search(r'\.go:\d+:',x)]
   p['skips'].append(dict(test=test,reasons=reasons))
  if test and action=='fail':p['failures'].append(dict(test=test,assertions=[x.strip() for x in outputs[(pkg,test)] if re.search(r'\.go:\d+:',x)]))
  if not test and action in ['pass','fail','skip']:p['package_action']=action;p['elapsed']=e.get('Elapsed')
 result={'log':name,'packages':by,'interrupted_lines':interrupts,'non_json_lines':bad,'totals':{k:sum(p[k] for p in by.values()) for k in ['top_pass','all_pass','top_skip','all_skip']}}
 (A/(name+'.summary.json')).write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n');print(name,json.dumps(result['totals']), 'failures',sum(len(p['failures']) for p in by.values()),'interrupts',len(interrupts),'nonjson',len(bad))
