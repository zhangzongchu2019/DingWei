import datetime,json,subprocess,sys
from pathlib import Path
r=Path(__file__).resolve().parent;kind=sys.argv[1];e=json.loads((r/('env-shipping-'+kind+'.json')).read_text());name=sys.argv[2];cmd=sys.argv[3:]
start=datetime.datetime.now(datetime.timezone.utc).isoformat()
(r/(name+'.command.json')).write_text(json.dumps({'cwd':str(Path.cwd()),'source':e['SOURCE_COMMIT'],'command':cmd,'env_file':'env-shipping-'+kind+'.json'},indent=2)+'\n')
with (r/(name+'.log')).open('w') as out:p=subprocess.run(cmd,env=e,stdout=out,stderr=subprocess.STDOUT)
(r/(name+'.result.json')).write_text(json.dumps({'source':e['SOURCE_COMMIT'],'start_utc':start,'end_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'rc':p.returncode},indent=2)+'\n');print(name,'rc',p.returncode)
