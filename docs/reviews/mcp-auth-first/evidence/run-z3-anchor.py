import datetime,json,subprocess,time,re
from pathlib import Path
A=Path(__file__).resolve().parent
W=Path('/data/ai-dev-data/mcp-auth-first')
S='ce9b7a959e828ad717b8708651c9094a421272ec'
e=json.loads((A/'env-anchor-go.json').read_text())
allowed=['TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent','TestViewMentionRoute/a_peer_without_delivery_ack_v1_is_not_sent_to','TestSessionNameReconnectKeepsNameAndReleaseReuses']
def failures(log):
 lines=log.splitlines();out=[]
 for i,line in enumerate(lines):
  m=re.match(r'^\s*--- FAIL: (\S+) \(',line)
  if not m: continue
  name=m.group(1);testname=name
  if name=='TestViewMentionRoute':testname=allowed[1]
  idx=[j for j,x in enumerate(lines[:i]) if x=='=== RUN   '+name]
  start=idx[-1] if idx else max(0,i-100)
  body='\n'.join(lines[start:i+1])
  ok=testname in allowed and 'interrupted (9)' in body
  out.append({'name':name,'allowed':ok,'context':body[-2500:]})
 return out
while not (A/'go-count3-anchor.result.json').exists():time.sleep(2)
initial=json.loads((A/'go-count3-anchor.result.json').read_text())
f=failures((A/'go-count3-anchor.log').read_text(errors='replace'))
assert initial['source']==S and initial['rc']!=0 and f and all(x['allowed'] for x in f),(initial,f)
assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=W).decode().strip()==S
summary={'source':S,'policy':'leader Z3: fresh serial five; >=3 fully green; every other failure must be interrupted(9) at the three explicitly allowed sites','allowed':allowed,'initial_rc':initial['rc'],'initial_failures':f,'rounds':[],'state':'RUNNING'}
(A/'z3-anchor-summary.json').write_text(json.dumps(summary,ensure_ascii=False,indent=2)+'\n')
for n in range(1,6):
 name='z3-anchor-round-'+str(n);cmd=['go','test','./internal/m8','-count=1','-p=1','-timeout=60m','-v']
 (A/(name+'.command.json')).write_text(json.dumps({'source':S,'cwd':str(W),'command':cmd,'env_file':'env-anchor-go.json'},indent=2)+'\n')
 start=datetime.datetime.now(datetime.timezone.utc).isoformat()
 with (A/(name+'.log')).open('w') as out:p=subprocess.run(cmd,cwd=W,env=e,stdout=out,stderr=subprocess.STDOUT)
 lines=(A/(name+'.log')).read_text(errors='replace').splitlines();f=failures('\n'.join(lines))
 r={'source':S,'round':n,'rc':p.returncode,'start_utc':start,'end_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'pass_top':sum(x.startswith('--- PASS: ') for x in lines),'pass_all':sum(bool(re.match(r'^\s*--- PASS: ',x)) for x in lines),'skip_occurrences':sum(bool(re.match(r'^\s*--- SKIP: ',x)) for x in lines),'interrupted_lines':sum('interrupted (9)' in x for x in lines),'failures':f}
 (A/(name+'.result.json')).write_text(json.dumps(r,ensure_ascii=False,indent=2)+'\n');summary['rounds'].append(r)
 if p.returncode!=0 and (not f or not all(x['allowed'] for x in f)):
  summary['state']='STOP_NON_ALLOWED_FAILURE';(A/'z3-anchor-summary.json').write_text(json.dumps(summary,ensure_ascii=False,indent=2)+'\n');raise SystemExit(1)
 (A/'z3-anchor-summary.json').write_text(json.dumps(summary,ensure_ascii=False,indent=2)+'\n');print(name,p.returncode,flush=True)
summary['fully_green']=sum(x['rc']==0 for x in summary['rounds']);summary['state']='PASS_POLICY' if summary['fully_green']>=3 else 'STOP_INSUFFICIENT_GREEN'
(A/'z3-anchor-summary.json').write_text(json.dumps(summary,ensure_ascii=False,indent=2)+'\n');print(summary['state'],summary['fully_green'],flush=True)
raise SystemExit(0 if summary['state']=='PASS_POLICY' else 1)
