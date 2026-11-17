exec(open('/tmp/r60-formal-run.py').read().split("if __name__")[0])
import re
w=Path('/data/ai-dev-data/r60-integration');assert git(w,'rev-parse','HEAD')==S and not git(w,'status','--porcelain')
changed=set(git(w,'diff','--name-only','110e2c1f63b1530d1ca05b92cd723312ef16c0f8',S).splitlines());testfiles={}
for p in (w/'internal/m8').glob('*_test.go'):
 for n in re.findall(r'^func (Test\w+)\(',p.read_text(),re.M):testfiles[n]=str(p.relative_to(w))
allow={'TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent','TestViewMentionRoute/a_peer_without_delivery_ack_v1_is_not_sent_to'}
summary={'source_commit':S,'authority':'leader 2026-11-17T01:37:18Z','runs':[],'gate':'RUNNING','no_deployment':True}
def save(): (R/'gate-summary.json').write_text(json.dumps(summary,ensure_ascii=False,indent=2)+'\n')
def analyze(v):
 counts={'top_pass':0,'all_pass':0,'all_skip':0};fail=[];skips=[];intr=[]
 for ln in (R/(v['name']+'.log')).read_text().splitlines():
  try:e=json.loads(ln)
  except ValueError:continue
  t=e.get('Test');a=e.get('Action')
  if t and a=='pass':counts['all_pass']+=1;counts['top_pass']+=('/' not in t)
  if t and a=='skip':counts['all_skip']+=1;skips.append(t)
  if t and a=='fail':fail.append({'test':t,'file':testfiles.get(t.split('/')[0],'unknown')})
  if re.search(r'interrupted\s*\(9\)|SQLITE_INTERRUPT',e.get('Output',''),re.I):intr.append({'test':t,'output':e['Output'].strip()})
 return dict(**counts,failures=fail,skips=skips,interrupted=intr,interrupted_lines=len(intr))
try:
 for i in range(1,6):
  summary['active']='m8-'+str(i);save()
  v=run('m8-'+str(i),w,['go','test','-json','./internal/m8','-count=1','-p','1','-timeout','60m']);v['name']='m8-'+str(i);v.update(analyze(v));summary['runs'].append(v);save()
  assert not v['dirty_before'] and not v['dirty_after'] and v['head_after']==S
  assert all(x['test'] in allow for x in v['interrupted']), 'interrupt outside allowed exact tests or unattributed'
  assert not any(x['file'] in changed for x in v['failures']), 'FAIL in R60 changed file'
  if v['rc']:
   assert v['interrupted'],'non-interrupt whole-package failure'
   allowedfails=allow|({'TestViewMentionRoute'} if any(x['test']=='TestViewMentionRoute/a_peer_without_delivery_ack_v1_is_not_sent_to' for x in v['interrupted']) else set())
   assert all(x['test'] in allowedfails for x in v['failures']), 'non-allowed failed test'
 assert sum(x['rc']==0 and not x['failures'] for x in summary['runs'])>=3,'fewer than three green complete runs'
 summary['m8_5_runs']='PASS';save()
 stages=[('go-other-four-trees',['go','test','-json','./internal/store/...','./internal/obs/...','./cmd/workpulse/...','-count=1','-p','1','-timeout','60m'],{}),('canonical-build',['sh','build.sh',str(R/'bin')],{}),('focused-race',['go','test','-race','./internal/m8','./internal/store','-run','TestR60OutboundControlBackpressure|TestViewerOffline|TestSessionKeepalive|TestSessionIngress|TestOutbound|TestAppConfigGuards','-count=1','-p','1','-v','-timeout','30m'],{}),('browser-all',['python3','tools/run-browser-regressions.py','--receipt',str(R/'browser-receipt.txt')],{'NODE_PATH':'/tmp/coder-a3-playwright/node_modules','PLAYWRIGHT_BROWSERS_PATH':'/home/dev/.cache/ms-playwright'}),('strict-all',['node','/home/dev/.npm/_npx/2e6f94dbd680ce26/node_modules/@fission-ai/openspec/bin/openspec.js','validate','--all','--strict'],{})]
 for name,cmd,extra in stages:
  summary['active']=name;save();v=run(name,w,cmd,extra);v['name']=name
  if name=='go-other-four-trees':v.update(analyze(v))
  summary[name]=v;save();assert v['rc']==0 and not v['dirty_after'] and v['head_after']==S,name+' gate failed'
 summary['active']=None;summary['gate']='PASS';save();print('ALL FORMAL GATES COMPLETE',flush=True)
except Exception as e:
 summary['gate']='STOP';summary['stop_reason']=str(e);save();raise
