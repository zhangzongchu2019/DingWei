from pathlib import Path
import subprocess,json,datetime,hashlib,re
A=Path(__file__).parent;T=Path('/data/ai-dev-data/r61-integration');E=json.loads((A/'env.json').read_text());rows=[]
allowed={'TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent','TestViewMentionRoute','TestViewMentionRoute/a_peer_without_delivery_ack_v1_is_not_sent_to','TestSessionNameReconnectKeepsNameAndReleaseReuses'}
def failures(s):return re.findall(r'^\s*--- FAIL: ([^ ]+)',s,re.M)
def check(s):
 f=failures(s)
 assert f and set(f)<=allowed,f
 assert 'interrupted (9)' in s,'no interrupted evidence'
 assert not any(x in s for x in ['panic:','DATA RACE','test timed out']), 'unrelated failure'
 return f
initial=json.loads((A/'gates.json').read_text());assert len(initial)==2 and initial[0]['rc']==0 and initial[1]['rc']!=0
check((A/'m8-store-full3.log').read_text());(A/'z3-initial-assessment.json').write_text(json.dumps({'source':E['SOURCE_COMMIT'],'initial_rc':initial[1]['rc'],'failures':failures((A/'m8-store-full3.log').read_text()),'qualification_required':True,'policy':'7e622c94d104; initial full3 preserved, fixed5 m8 count1; baseline targetedcount20; >=3green and only approved interrupted sites; no singlepass substitution'},indent=2)+'\n')
def run(name,cmd,cwd=T):
 assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=T,text=True).strip()==E['SOURCE_COMMIT'];assert not subprocess.check_output(['git','status','--porcelain'],cwd=T).strip()
 r={'name':name,'source':subprocess.check_output(['git','rev-parse','HEAD'],cwd=cwd,text=True).strip(),'integration_source':E['SOURCE_COMMIT'],'command':cmd,'cwd':str(cwd),'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()};print('START',name,flush=True)
 with(A/(name+'.log')).open('wb')as f:r['rc']=subprocess.run(cmd,cwd=cwd,env=E,stdout=f,stderr=subprocess.STDOUT).returncode
 r['ended_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();r['sha256']=hashlib.sha256((A/(name+'.log')).read_bytes()).hexdigest();rows.append(r);(A/'z3-followup-gates.json').write_text(json.dumps(rows,indent=2)+'\n');print('END',name,r['rc'],flush=True);return r
for name,cmd in [('focused-race',['go','test','-race','./internal/m8','./internal/store','-run','DeliveryACK|QueueFull|FailureNotice|ReliableSessionDeliveryBusyAck','-count=1','-p','1','-timeout','40m','-v']),('browser-full',[E['DW_DESKTOP_INTEGRATION_PYTHON'],'tools/run-browser-regressions.py','--receipt',str(A/'browser-receipt.txt')]),('strict',['/tmp/coder56solzzc-openspec-1.16.0/node_modules/.bin/openspec','validate','--all','--strict'])]:
 assert run(name,cmd)['rc']==0,'non Z3 gate failed'
B=Path('/data/ai-dev-data/r61i2-baseline')
assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=B,text=True).strip()=='206d41b26dab646c0e50a111eed5a45ccf7c72f6'
assert not subprocess.check_output(['git','status','--porcelain'],cwd=B).strip()
r=run('z3-baseline-target20',['go','test','./internal/m8','-run','^TestSessionNameReconnectKeepsNameAndReleaseReuses$','-count=20','-p','1','-timeout','30m','-v'],B)
if r['rc']:check((A/(r['name']+'.log')).read_text())
green=0;qualification=[]
for n in range(1,6):
 r=run('z3-m8-round'+str(n),['go','test','./internal/m8','-count=1','-p','1','-timeout','60m','-v']);s=(A/(r['name']+'.log')).read_text();r['failures']=failures(s);r['interrupted_lines']=sum('interrupted (9)'in l for l in s.splitlines());qualification.append(r)
 if r['rc']:check(s)
 else:assert not failures(s);green+=1
 (A/'z3-qualification.json').write_text(json.dumps({'source':E['SOURCE_COMMIT'],'rounds':qualification,'green_rounds':green,'complete':n==5,'passed':n==5 and green>=3},indent=2)+'\n')
assert green>=3,'Z3 qualification failed: fewer than3green'
(A/'gate-acceptance.json').write_text(json.dumps({'source':E['SOURCE_COMMIT'],'initial_full3_rc':initial[1]['rc'],'qualification_pass':True,'green_rounds':green,'all_other_gates_rc0':True,'policy':'initial FAIL remains; Z3 narrowly qualified, not an all-green initial full3'},indent=2)+'\n')
