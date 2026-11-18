from pathlib import Path
import re,json
A=Path(__file__).parent;out={}
for name in ['mutation-baseline','mutation-restored-baseline','canonical','full-go3','strict']:
 p=A/(name+'.log')
 if not p.exists():continue
 text=p.read_text();lines=text.splitlines();skips=[]
 for i,l in enumerate(lines):
  if re.match(r'^\s*--- SKIP:',l):skips.append({'name':l.split('SKIP: ',1)[1].rsplit(' (',1)[0],'context':lines[max(0,i-3):i]})
 out[name]={'top_pass':len(re.findall(r'^--- PASS:',text,re.M)),'all_pass':len(re.findall(r'^\s*--- PASS:',text,re.M)),'top_fail':len(re.findall(r'^--- FAIL:',text,re.M)),'all_fail':len(re.findall(r'^\s*--- FAIL:',text,re.M)),'failures':re.findall(r'^\s*--- FAIL: (.*?) \(',text,re.M),'top_skip':len(re.findall(r'^--- SKIP:',text,re.M)),'all_skip':len(skips),'skips':skips,'interrupted_lines':[l for l in lines if 'interrupted (9)'in l],'package_results':[l for l in lines if l.startswith(('ok  ','FAIL\t'))],'helper_results':[json.loads(l.split('HELPER_GATE_RESULT ',1)[1])for l in lines if l.startswith('HELPER_GATE_RESULT ')],'pg_version_lines':[l for l in lines if 'server_version_num='in l]}
(A/'parsed-gates.json').write_text(json.dumps(out,indent=2)+'\n');print(json.dumps({k:{n:v for n,v in r.items()if n not in ['skips','interrupted_lines','pg_version_lines']}for k,r in out.items()},indent=2))
