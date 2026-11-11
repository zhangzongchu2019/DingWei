import subprocess,json,datetime,sys,os
from pathlib import Path
w=Path('/data/ai-dev-data/hosted-test-time-c782');out=Path('/tmp/hosted-test-time-c782')
env=json.loads(Path('/tmp/r57-c75a/gate-env.json').read_text());env.update(HOME=str(out/'h'),TMPDIR='/tmp/htt782')
for k in ['HOME','TMPDIR']:Path(env[k]).mkdir(exist_ok=True)
(out/'env.json').write_text(json.dumps(env,indent=2)+'\n')
def run(name,cmd):
 r={'cmd':cmd,'cwd':str(w),'start':datetime.datetime.now(datetime.timezone.utc).isoformat()};print('START',name,flush=True)
 with (out/(name+'.log')).open('w') as f:p=subprocess.run(['env','-i',*[k+'='+v for k,v in env.items()],*cmd],cwd=w,stdout=f,stderr=subprocess.STDOUT)
 r.update(rc=p.returncode,end=datetime.datetime.now(datetime.timezone.utc).isoformat())
 events=[]
 for l in (out/(name+'.log')).open():
  try:x=json.loads(l)
  except ValueError:continue
  if isinstance(x,dict):events.append(x)
 r['packages']={pkg:{'elapsed':[x.get('Elapsed') for x in events if x.get('Package')==pkg and not x.get('Test') and x.get('Action') in ['pass','fail']],'counts':{a:sum(x.get('Action')==a and bool(x.get('Test')) for x in events if x.get('Package')==pkg) for a in ['pass','fail','skip']}} for pkg in sorted({x['Package'] for x in events if 'Package'in x})}
 h=[x for x in events if x.get('Package','').endswith('/internal/hosted')];r['hosted_top']=[{k:x.get(k) for k in ['Test','Action','Elapsed']} for x in h if x.get('Test') and '/' not in x['Test'] and x.get('Action') in ['pass','fail','skip']];r['hosted_slow15']=sorted(r['hosted_top'],key=lambda x:x.get('Elapsed',0),reverse=True)[:15]
 r['skips']=[{'package':x['Package'],'test':x['Test'],'condition':''.join(y.get('Output','') for y in events if y.get('Test')==x['Test'] and y.get('Package')==x['Package'])} for x in events if x.get('Action')=='skip' and x.get('Test')]
 (out/(name+'.json')).write_text(json.dumps(r,ensure_ascii=False,indent=2)+'\n');print('RESULT',name,json.dumps({k:v for k,v in r.items() if k not in ['skips','hosted_top','packages']},ensure_ascii=False),flush=True)
 return r
if __name__=='__main__':
 if sys.argv[1]=='baseline':
  for n,c in [('before-hosted',['go','test','-json','-count=1','-p','2','-timeout','40m','./internal/hosted']),('before-all',['go','test','-json','-count=1','-p','2','-timeout','40m','./...'])]:
   r=run(n,c)
   if r['rc']:sys.exit(r['rc'])
