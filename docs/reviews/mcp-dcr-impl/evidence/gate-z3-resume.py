from pathlib import Path
import subprocess,json,sys,datetime,hashlib
A=Path(__file__).resolve().parent;W=Path('/data/ai-dev-data/mcp-dcr');env=json.loads((A/'frozen-env.json').read_text());source=env['SOURCE_COMMIT']
allowed={'TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent','TestViewMentionRoute/a_peer_without_delivery_ack_v1_is_not_sent_to','TestSessionNameReconnectKeepsNameAndReleaseReuses'}
def inspect(name):
 subprocess.run([sys.executable,str(A/'summarize-go.py'),name],check=True)
 s=json.loads((A/(name+'.summary.json')).read_text());fail=[f['test'] for p in s['packages'].values() for f in p['failures']]
 leaf=[f for f in fail if not any(x.startswith(f+'/') for x in fail)]
 if any(f not in allowed for f in leaf):raise RuntimeError(('unexpected failing site',name,leaf))
 if any('interrupted' not in a and '回执=[]' not in a for p in s['packages'].values() for f in p['failures'] if f['test'] in leaf for a in f['assertions'] if ': ' in a):raise RuntimeError(('unexpected assertion',name,leaf))
 if fail and not s['interrupted_lines']:raise RuntimeError(('known site but no interrupted diagnostic',name))
 return s,fail
r=json.loads((A/'go-packages-count3.result.json').read_text());assert r['source']==source and r['rc']!=0
initial,fail=inspect('go-packages-count3');assert fail
base='4a349ee59a1d8d7b8b9c562c462bded2f8b0f192';B=Path('/data/ai-dev-data/wt-mcp-dcr-old-4a349ee5');assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=B,text=True).strip()==base
cmd=['go','test','./internal/m8','-run','^TestViewMentionRoute$','-count=20','-p','1','-v','-json','-timeout=15m'];name='z3-baseline4a-view20';start=datetime.datetime.now(datetime.timezone.utc).isoformat();be=env|{'SOURCE_COMMIT':base};(A/(name+'.command.json')).write_text(json.dumps({'source':base,'cwd':str(B),'command':cmd},indent=2)+'\n')
with (A/(name+'.log')).open('w') as out:p=subprocess.run(cmd,cwd=B,env=be,stdout=out,stderr=subprocess.STDOUT)
(A/(name+'.result.json')).write_text(json.dumps({'source':base,'rc':p.returncode,'start_utc':start,'end_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'log_sha256':hashlib.sha256((A/(name+'.log')).read_bytes()).hexdigest()},indent=2)+'\n');inspect(name)
rows=[]
for i in range(1,6):
 name=f'z3-fresh-{i}';print('START',name,datetime.datetime.now(datetime.timezone.utc).isoformat(),flush=True)
 subprocess.run([sys.executable,str(A/'run-frozen.py'),name,'go','test','./internal/m8','-count=1','-p','1','-v','-json','-timeout=30m'],check=True)
 r=json.loads((A/(name+'.result.json')).read_text());s,fail=inspect(name);rows.append(r|{'phase':name,'failures':fail,'interrupted_lines':len(s['interrupted_lines']),'top_pass':s['totals']['top_pass'],'all_pass':s['totals']['all_pass']});(A/'z3-summary.json').write_text(json.dumps({'source':source,'runs':rows,'green':sum(x['rc']==0 for x in rows),'required_green':3,'complete':len(rows)==5},indent=2)+'\n')
 if r['rc']!=0 and not fail:raise RuntimeError(('non-test failure',name))
assert sum(x['rc']==0 for x in rows)>=3,'fewer than3green; STOP'
print('Z3_ACCEPTED',sum(x['rc']==0 for x in rows),'/5',flush=True)
results=json.loads((A/'gate-summary.json').read_text())
for name,cmd in [
 ('migration-full',['go','test','./internal/store/migrate','-count=1','-p','1','-v','-json','-timeout=15m']),
 ('cmd-focused',['go','test','./cmd/workpulse','-run','TestDCR|TestOptionalMCPGateway|TestMCPGateway','-count=3','-p','1','-v','-json','-timeout=15m']),
 ('focused-race',['go','test','-race','./internal/mcpgw','./cmd/workpulse','-run','TestDCR|TestMCPAuthFirst|TestPGStore','-count=1','-p','1','-v','-json','-timeout=15m']),
 ('strict-final',['/tmp/coder56solzzc-openspec-1.16.0/node_modules/.bin/openspec','validate','--all','--strict'])]:
 print('START',name,datetime.datetime.now(datetime.timezone.utc).isoformat(),flush=True);subprocess.run([sys.executable,str(A/'run-frozen.py'),name,*cmd],check=True);r=json.loads((A/(name+'.result.json')).read_text());results.append(r|{'phase':name});(A/'gate-summary.json').write_text(json.dumps(results,indent=2)+'\n');assert r['rc']==0,('STOP',name)
print('GATES_COMPLETE_WITH_DISCLOSED_Z3',source,flush=True)
