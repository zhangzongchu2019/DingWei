"""串行门及联测后，补齐本轮实际中断落点对应的main对照，不丢弃已有辅助对照。"""
from pathlib import Path
import subprocess,json,datetime,hashlib,re,time
A=Path(__file__).parent;T=Path('/data/ai-dev-data/r61-integration');E=json.loads((A/'env.json').read_text());B=Path('/data/ai-dev-data/r61i2-baseline');allowed={'TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent','TestSessionNameReconnectKeepsNameAndReleaseReuses','TestViewMentionRoute/a_peer_without_delivery_ack_v1_is_not_sent_to','TestViewMentionRoute'}
while not(A/'compat-runs.json').exists()or len(json.loads((A/'compat-runs.json').read_text()))<2:time.sleep(5)
assert all(r['rc']==0 for r in json.loads((A/'compat-runs.json').read_text()));assert all(r['rc']==0 for r in json.loads((A/'compat-results.json').read_text()))
q=json.loads((A/'z3-qualification.json').read_text());rows=json.loads((A/'z3-followup-gates.json').read_text());target=set()
if q['required']:
 logs=[A/'m8-store-full3.log']+[A/(r['name']+'.log')for r in q['rounds']]
 for p in logs:target.update(re.findall(r'^\s*--- FAIL: ([^ ]+)',p.read_text(),re.M))
 assert target<=allowed,target;target.discard('TestViewMentionRoute')
 # The primary script already ran the reconnect target as an auxiliary control.
 target.discard('TestSessionNameReconnectKeepsNameAndReleaseReuses')
records=[]
for i,name in enumerate(sorted(target),1):
 assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=T,text=True).strip()==E['SOURCE_COMMIT'];assert not subprocess.check_output(['git','status','--porcelain'],cwd=T).strip();base=subprocess.check_output(['git','rev-parse','HEAD'],cwd=B,text=True).strip();assert base=='206d41b26dab646c0e50a111eed5a45ccf7c72f6'
 pattern='/'.join('^'+re.escape(x)+'$'for x in name.split('/'));cmd=['go','test','./internal/m8','-run',pattern,'-count=20','-p','1','-timeout','30m','-v'];r={'name':'z3-matching-baseline'+str(i)+'-20','target':name,'source':base,'integration_source':E['SOURCE_COMMIT'],'command':cmd,'cwd':str(B),'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()};print('START',r['name'],name,flush=True)
 p=A/(r['name']+'.log')
 with p.open('wb')as f:r['rc']=subprocess.run(cmd,cwd=B,env=E|{'SOURCE_COMMIT':base},stdout=f,stderr=subprocess.STDOUT).returncode
 r['ended_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();s=p.read_text();r['sha256']=hashlib.sha256(p.read_bytes()).hexdigest();r['failures']=re.findall(r'^\s*--- FAIL: ([^ ]+)',s,re.M)
 if r['rc']:assert set(r['failures'])<=allowed and 'interrupted (9)'in s and not any(t in s for t in ['panic:','DATA RACE','test timed out'])
 rows.append(r);records.append(r);(A/'z3-followup-gates.json').write_text(json.dumps(rows,indent=2)+'\n');print('END',r['name'],r['rc'],flush=True)
(A/'matching-baselines.json').write_text(json.dumps({'targets':sorted(target),'records':records,'complete':True,'prior_auxiliary_control':'reconnect count20 retained separately if Z3 required'},indent=2)+'\n');p=A/'gate-acceptance.json';d=json.loads(p.read_text());d['matching_baselines_complete']=True;d['matching_baselines']=records;p.write_text(json.dumps(d,indent=2)+'\n')
