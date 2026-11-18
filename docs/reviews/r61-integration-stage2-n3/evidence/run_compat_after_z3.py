from pathlib import Path
import subprocess,json,time,datetime,hashlib,os
A=Path(__file__).parent;T=Path('/data/ai-dev-data/r61-integration');E=json.loads((A/'env.json').read_text())
while not(A/'gate-acceptance.json').exists():
 time.sleep(5)
assert json.loads((A/'gate-acceptance.json').read_text())['accepted']
assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=T,text=True).strip()==E['SOURCE_COMMIT'];assert not subprocess.check_output(['git','status','--porcelain'],cwd=T).strip()
old=Path('/data/ai-dev-data/wt-r61-n1b-old501');assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=old,text=True).strip()=='501e5b8a185b20f5c931c8ebb89c16bfd633a01e'
assert not subprocess.check_output(['git','status','--porcelain'],cwd=old).strip()
(A/'bin').mkdir(exist_ok=True);oldbin=Path('/data/ai-dev-data/local-run-artifacts/coder56solzzc/r61-n1c-review-20261118/bin/old-hub-501');assert hashlib.sha256(oldbin.read_bytes()).hexdigest()=='bc45cb95b1e51e54d34b7f0e5512bc8582dc7a6603f168d65699056ba9ba1820';(A/'bin/old-hub-501').symlink_to(oldbin)
rows=[]
for name,cmd,cwd in [('integrated-hub-build',['go','build','-buildvcs=true','-o',str(A/'bin/integrated-hub'),'./cmd/workpulse'],T),('actual-process-compat',[E['DW_DESKTOP_INTEGRATION_PYTHON'],str(A/'compat_real.py')],A)]:
 r={'name':name,'source':E['SOURCE_COMMIT'],'command':cmd,'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
 with(A/(name+'.log')).open('wb')as f:r['rc']=subprocess.run(cmd,cwd=cwd,env=E,stdout=f,stderr=subprocess.STDOUT).returncode
 r['ended_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();r['sha256']=hashlib.sha256((A/(name+'.log')).read_bytes()).hexdigest();rows.append(r);(A/'compat-runs.json').write_text(json.dumps(rows,indent=2)+'\n');print(name,r['rc'],flush=True)
 if r['rc']:raise SystemExit(name)
if not all(r['rc']==0 for r in json.loads((A/'compat-results.json').read_text())):raise SystemExit('compat case failed; preserve outputs')
(A/'binary-shas.json').write_text(json.dumps({n:hashlib.sha256((A/'bin'/n).read_bytes()).hexdigest()for n in ['old-hub-501','integrated-hub']},indent=2)+'\n')
(A/'integrated-hub-go-version-m.txt').write_text(subprocess.check_output(['go','version','-m',str(A/'bin/integrated-hub')],env=E,text=True))
