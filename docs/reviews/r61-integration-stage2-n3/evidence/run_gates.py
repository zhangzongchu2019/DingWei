from pathlib import Path
import subprocess,json,datetime,hashlib,re
A=Path(__file__).parent;T=Path('/data/ai-dev-data/r61-integration');E=json.loads((A/'env.json').read_text());rows=[];extra=[]
allowed={'TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent','TestViewMentionRoute','TestViewMentionRoute/a_peer_without_delivery_ack_v1_is_not_sent_to','TestSessionNameReconnectKeepsNameAndReleaseReuses'}
def failure(s):return re.findall(r'^\s*--- FAIL: ([^ ]+)',s,re.M)
def check_known(s):
 f=failure(s);assert f and set(f)<=allowed,f;assert 'interrupted (9)'in s;assert not any(t in s for t in ['panic:','DATA RACE','test timed out']);return f

def run(name,cmd,cwd=T,more=False):
 assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=T,text=True).strip()==E['SOURCE_COMMIT'];assert not subprocess.check_output(['git','status','--porcelain'],cwd=T).strip()
 testsource=subprocess.check_output(['git','rev-parse','HEAD'],cwd=cwd,text=True).strip();r={'name':name,'source':testsource,'integration_source':E['SOURCE_COMMIT'],'command':cmd,'cwd':str(cwd),'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()};print('START',name,flush=True)
 with(A/(name+'.log')).open('wb')as f:r['rc']=subprocess.run(cmd,cwd=cwd,env=E|{'SOURCE_COMMIT':testsource},stdout=f,stderr=subprocess.STDOUT).returncode
 r['ended_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();r['sha256']=hashlib.sha256((A/(name+'.log')).read_bytes()).hexdigest();(extra if more else rows).append(r);(A/('z3-followup-gates.json'if more else 'gates.json')).write_text(json.dumps(extra if more else rows,indent=2)+'\n');print('END',name,r['rc'],flush=True);return r

r=run('canonical',['./build.sh','--test-only']);assert r['rc']==0,'canonical failed'
r=run('m8-store-full3',['go','test','./internal/m8','./internal/store/...','-count=3','-p','1','-timeout','90m','-v']);required=r['rc']!=0
if required:check_known((A/'m8-store-full3.log').read_text());print('Z3 qualification required; preserve initial FAIL',flush=True)
for n,c in [('focused-race',['go','test','-race','./internal/m8','./internal/store','-run','DeliveryACK|QueueFull|FailureNotice|ReliableSessionDeliveryBusyAck|HistoryProvenance|SessionHistory','-count=1','-p','1','-timeout','60m','-v']),('browser-full',[E['DW_DESKTOP_INTEGRATION_PYTHON'],'tools/run-browser-regressions.py','--receipt',str(A/'browser-receipt.txt')]),('strict',['/tmp/coder56solzzc-openspec-1.16.0/node_modules/.bin/openspec','validate','--all','--strict'])]:assert run(n,c)['rc']==0,'nonZ3 gate failed'
qualification={'source':E['SOURCE_COMMIT'],'required':required,'complete':True,'passed':not required,'rounds':[]}
if required:
 B=Path('/data/ai-dev-data/r61i2-baseline');assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=B,text=True).strip()=='206d41b26dab646c0e50a111eed5a45ccf7c72f6';assert not subprocess.check_output(['git','status','--porcelain'],cwd=B).strip()
 r=run('z3-baseline-target20',['go','test','./internal/m8','-run','^TestSessionNameReconnectKeepsNameAndReleaseReuses$','-count=20','-p','1','-timeout','30m','-v'],B,True)
 if r['rc']:check_known((A/(r['name']+'.log')).read_text())
 q=[];green=0
 for n in range(1,6):
  r=run('z3-m8-round'+str(n),['go','test','./internal/m8','-count=1','-p','1','-timeout','60m','-v'],more=True);s=(A/(r['name']+'.log')).read_text();r['failures']=failure(s);r['interrupted_lines']=sum('interrupted (9)'in l for l in s.splitlines());q.append(r)
  if r['rc']:check_known(s)
  else:assert not failure(s);green+=1
  qualification={'source':E['SOURCE_COMMIT'],'required':True,'rounds':q,'green_rounds':green,'complete':n==5,'passed':n==5 and green>=3};(A/'z3-qualification.json').write_text(json.dumps(qualification,indent=2)+'\n')
 assert green>=3,'Z3 gate stop fewer3green'
else:(A/'z3-qualification.json').write_text(json.dumps(qualification,indent=2)+'\n');(A/'z3-followup-gates.json').write_text('[]\n')
(A/'gate-acceptance.json').write_text(json.dumps({'source':E['SOURCE_COMMIT'],'accepted':True,'initial_full3_rc':rows[1]['rc'],'z3_required':required,'qualification':qualification,'all_other_gates_rc0':True},indent=2)+'\n')
