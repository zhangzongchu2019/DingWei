from pathlib import Path
import subprocess,json,datetime,hashlib,re
A=Path(__file__).parent;T=Path('/data/ai-dev-data/r62-extseat-ttl');K=Path('/data/ai-dev-data/r62-extseat-ttl-knives');S='2fd6aebacbb3dae3ac3fa82d84968367552151ca';E=json.loads((A/'env.json').read_text());E['SOURCE_COMMIT']=S;(A/'env.json').write_text(json.dumps(E,indent=2)+'\n');rows=[];cuts=[]
def utc():return datetime.datetime.now(datetime.timezone.utc).isoformat()
def run(name,cmd,tree=T):
 r={'name':name,'source':S,'command':cmd,'cwd':str(tree),'started_utc':utc()};print('START',name,flush=True)
 with (A/(name+'.log')).open('wb')as f:r['rc']=subprocess.run(cmd,cwd=tree,env=E,stdout=f,stderr=subprocess.STDOUT).returncode
 r.update(ended_utc=utc(),sha256=hashlib.sha256((A/(name+'.log')).read_bytes()).hexdigest());rows.append(r);(A/'runs.json').write_text(json.dumps(rows,indent=2)+'\n');print('END',name,r['rc'],flush=True);return r
base=['go','test','./internal/extshared','./internal/tempgroup','-run','TestTTLConstants|TestR62','-count=1','-p','1','-timeout','20m','-v']
assert run('mutation-baseline',base,K)['rc']==0
p=K/'internal/extshared/extshared.go';original=p.read_bytes()
for name,before,after,pattern in [('group-old-three-days','GroupTTL = 7 * 24 * time.Hour','GroupTTL = 3 * 24 * time.Hour','TestTTLConstants|TestR62SevenDayGroupBoundaryPG'),('message-old-one-day','MessageTTL = 7 * 24 * time.Hour','MessageTTL = 24 * time.Hour','TestTTLConstants|TestR62SevenDayMessageBoundaryIndependentOfRenewalsPG')]:
 s=original.decode();assert s.count(before)==1
 try:
  mut=s.replace(before,after).encode();p.write_bytes(mut)
  build=run('mutation-'+name+'-compile',['go','test','./internal/extshared','./internal/tempgroup','-run','^$','-count=1','-p','1'],K);assert build['rc']==0
  r=run('mutation-'+name,['go','test','./internal/extshared','./internal/tempgroup','-run',pattern,'-count=1','-p','1','-timeout','20m','-v'],K)
  log=(A/('mutation-'+name+'.log')).read_text();cut={'name':name,'file':'internal/extshared/extshared.go','before':before,'after':after,'compile_rc':build['rc'],'rc':r['rc'],'log':r['name']+'.log','sha256':r['sha256'],'original_sha256':hashlib.sha256(original).hexdigest(),'mutated_sha256':hashlib.sha256(mut).hexdigest(),'failed_tests':re.findall(r'^\s*--- FAIL: (.*?) \(',log,re.M),'assertion_lines':[l for l in log.splitlines()if 'expiry='in l or 'TTL constants drifted'in l]}
 finally:p.write_bytes(original)
 cut['restored_sha256']=hashlib.sha256(p.read_bytes()).hexdigest();assert cut['rc']!=0 and cut['failed_tests'] and cut['assertion_lines'] and '[build failed]'not in log and 'panic:'not in log;cuts.append(cut);(A/'mutations.json').write_text(json.dumps(cuts,indent=2)+'\n')
assert not subprocess.check_output(['git','status','--porcelain'],cwd=K).strip()
assert run('mutation-restored-baseline',base,K)['rc']==0
for name,cmd in [('canonical',['./build.sh','--test-only']),('full-go3',['go','test','./internal/extshared','./internal/extseat','./internal/tempgroup','./internal/m8','-count=3','-p','1','-timeout','90m','-v']),('strict',['/tmp/coder56solzzc-openspec-1.16.0/node_modules/.bin/openspec','validate','--all','--strict'])]:
 assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=T,text=True).strip()==S
 assert not subprocess.check_output(['git','status','--porcelain'],cwd=T).strip()
 run(name,cmd)
