from pathlib import Path
import subprocess,json,sys,datetime,hashlib,os
A=Path(__file__).resolve().parent;kind,name=sys.argv[1:3];cmd=sys.argv[3:];E=json.loads((A/('env-'+kind+'.json')).read_text());W='/data/ai-dev-data/r61-integration'
assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=W).decode().strip()==E['SOURCE_COMMIT'];assert subprocess.check_output(['git','status','--porcelain'],cwd=W)==b''
start=datetime.datetime.now(datetime.timezone.utc).isoformat();(A/(name+'.command.json')).write_text(json.dumps({'source':E['SOURCE_COMMIT'],'command':cmd,'cwd':W,'env_file':'env-'+kind+'.json','runner_pid':os.getpid()},indent=2)+'\n')
with (A/(name+'.log')).open('w') as out:p=subprocess.run(cmd,cwd=W,env=E,stdout=out,stderr=subprocess.STDOUT)
r={'source':E['SOURCE_COMMIT'],'rc':p.returncode,'start_utc':start,'end_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'sha256':hashlib.sha256((A/(name+'.log')).read_bytes()).hexdigest()};(A/(name+'.result.json')).write_text(json.dumps(r,indent=2)+'\n');print(name,p.returncode);raise SystemExit(0 if p.returncode==0 else 1)
