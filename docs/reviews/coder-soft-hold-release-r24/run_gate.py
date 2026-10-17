import subprocess,json,sys,time,hashlib,datetime
from pathlib import Path
lab=Path(__file__).parent
label=sys.argv[1];args=sys.argv[2:]
env=json.loads((lab/'env.json').read_text());env['PATH']='/data/ai-dev-data/helper-deps-lock-venv/bin:'+env['PATH'];env['PYTHONDONTWRITEBYTECODE']='1'
source=subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip()
started=datetime.datetime.now(datetime.timezone.utc).isoformat();t=time.monotonic()
log=lab/'raw'/(label+'.log')
with log.open('w') as f:p=subprocess.run(args,env=env,stdout=f,stderr=subprocess.STDOUT)
result={'source':source,'argv':args,'rc':p.returncode,'elapsed_seconds':round(time.monotonic()-t,3),'started_utc':started,'finished_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'sha256':hashlib.sha256(log.read_bytes()).hexdigest()}
(lab/'raw'/(label+'.result.json')).write_text(json.dumps(result,indent=2)+'\n')
print(json.dumps(result));print(log.read_text(errors='replace')[-1800:]);sys.exit(p.returncode)
