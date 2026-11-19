from pathlib import Path
import json,subprocess,time,re,datetime
A=Path(__file__).resolve().parent;W=Path('/data/ai-dev-data/r61-integration');E=json.loads((A/'env-go.json').read_text());S=E['SOURCE_COMMIT']
allowed=['TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent','TestViewMentionRoute/a_peer_without_delivery_ack_v1_is_not_sent_to','TestSessionNameReconnectKeepsNameAndReleaseReuses']
def failures(log):
 l=log.splitlines();out=[]
 for i,line in enumerate(l):
  m=re.match(r'^\s*--- FAIL: (\S+) \(',line)
  if not m:continue
  name=m.group(1);idx=[j for j,x in enumerate(l[:i]) if x=='=== RUN   '+name];start=idx[-1] if idx else max(0,i-100);body='\n'.join(l[start:i+1]);mapped=allowed[1] if name=='TestViewMentionRoute' else name
  ok=mapped in allowed and 'interrupted (9)' in body
  if name=='TestViewMentionRoute':ok=ok and ('--- FAIL: '+allowed[1]) in body
  out.append({'name':name,'allowed':ok,'context':body[-3000:]})
 return out
while not(A/'m8-store-full3.result.json').exists():time.sleep(2)
r=json.loads((A/'m8-store-full3.result.json').read_text());assert r['source']==S
q={'source':S,'initial_full3_rc':r['rc'],'required':r['rc']!=0,'allowed':allowed,'rounds':[],'state':'RUNNING'}
f=failures((A/'m8-store-full3.log').read_text(errors='replace'));q['initial_failures']=f
if r['rc']==0:
 assert not f;q.update(state='PASS_ALL_GREEN',passed=True);(A/'go-policy.json').write_text(json.dumps(q,ensure_ascii=False,indent=2)+'\n');print('GO_POLICY full3 all green');raise SystemExit
if not f or not all(x['allowed'] for x in f):
 q.update(state='STOP_NON_ALLOWED_FAILURE',passed=False);(A/'go-policy.json').write_text(json.dumps(q,ensure_ascii=False,indent=2)+'\n');raise SystemExit(1)
(A/'go-policy.json').write_text(json.dumps(q,ensure_ascii=False,indent=2)+'\n')
# Same previously accepted integration source, not a modified baseline.
B=Path('/data/ai-dev-data/wt-r61-stage5-base530');base='5305358fd2e37f2eabe6f5349877eba7b015d143'
if not B.exists():subprocess.run(['git','worktree','add','--detach',str(B),base],cwd=W,check=True,stdout=subprocess.DEVNULL)
assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=B).decode().strip()==base;assert subprocess.check_output(['git','status','--porcelain'],cwd=B)==b''
be=E|{'SOURCE_COMMIT':base,'PYTHONPATH':str(B/'tools/sessionhelper'),'HOME':'/data/ai-dev-data/r61s5sbaseh','TMPDIR':'/data/ai-dev-data/r61s5sbaset'}
for k in ['HOME','TMPDIR']:Path(be[k]).mkdir(parents=True,exist_ok=True)
(A/'env-z3-baseline.json').write_text(json.dumps(be,indent=2)+'\n')
name=f[0]['name'];name=allowed[1] if name=='TestViewMentionRoute' else name
cmd=['go','test','./internal/m8','-run','^'+name+'$','-count=20','-p=1','-v'];start=datetime.datetime.now(datetime.timezone.utc).isoformat()
(A/'z3-baseline.command.json').write_text(json.dumps({'source':base,'command':cmd,'cwd':str(B),'env_file':'env-z3-baseline.json'},indent=2)+'\n')
with(A/'z3-baseline.log').open('w')as out:p=subprocess.run(cmd,cwd=B,env=be,stdout=out,stderr=subprocess.STDOUT)
bl=(A/'z3-baseline.log').read_text(errors='replace');bf=failures(bl);br={'source':base,'rc':p.returncode,'start_utc':start,'end_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'failures':bf}
(A/'z3-baseline.result.json').write_text(json.dumps(br,ensure_ascii=False,indent=2)+'\n');assert p.returncode==0 or bf and all(x['allowed'] for x in bf)
for n in range(1,6):
 name='z3-round-'+str(n);cmd=['go','test','./internal/m8','-count=1','-p=1','-timeout=60m','-v']
 p=subprocess.run(['python3',str(A/'run.py'),'go',name,*cmd]);rr=json.loads((A/(name+'.result.json')).read_text());l=(A/(name+'.log')).read_text(errors='replace').splitlines();ff=failures('\n'.join(l));rr.update(round=n,failures=ff,interrupted_lines=sum('interrupted (9)'in x for x in l),pass_top=sum(x.startswith('--- PASS:')for x in l),pass_all=sum(bool(re.match(r'^\s*--- PASS:',x))for x in l),skip_occurrences=sum(bool(re.match(r'^\s*--- SKIP:',x))for x in l));q['rounds'].append(rr)
 if rr['rc']!=0 and (not ff or not all(x['allowed']for x in ff)):
  q.update(state='STOP_NON_ALLOWED_FAILURE',passed=False);(A/'go-policy.json').write_text(json.dumps(q,ensure_ascii=False,indent=2)+'\n');raise SystemExit(1)
 (A/'go-policy.json').write_text(json.dumps(q,ensure_ascii=False,indent=2)+'\n')
q['green_rounds']=sum(x['rc']==0 for x in q['rounds']);q.update(state='PASS_POLICY'if q['green_rounds']>=3 else 'STOP_INSUFFICIENT_GREEN',passed=q['green_rounds']>=3)
(A/'go-policy.json').write_text(json.dumps(q,ensure_ascii=False,indent=2)+'\n');print(q['state'],q['green_rounds']);raise SystemExit(0 if q['passed'] else 1)
