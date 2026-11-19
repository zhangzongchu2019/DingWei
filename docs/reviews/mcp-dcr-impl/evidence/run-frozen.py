from pathlib import Path
import subprocess,json,sys,datetime,hashlib
A=Path(__file__).resolve().parent;e=json.loads((A/'frozen-env.json').read_text());name=sys.argv[1];cmd=sys.argv[2:];start=datetime.datetime.now(datetime.timezone.utc).isoformat();w='/data/ai-dev-data/mcp-dcr'
source=e['SOURCE_COMMIT'];head=subprocess.check_output(['git','rev-parse','HEAD'],cwd=w,text=True).strip();assert head==source,(head,source)
assert subprocess.check_output(['git','diff','--name-only',source,'--','internal','cmd','tools','scripts','deploy','go.mod','go.sum','VERSION'],cwd=w,text=True)==''
(A/(name+'.command.json')).write_text(json.dumps({'command':cmd,'cwd':w,'source':source,'uid':1001},indent=2)+'\n')
with (A/(name+'.log')).open('w') as f:p=subprocess.run(cmd,cwd=w,env=e,stdout=f,stderr=subprocess.STDOUT)
r={'source':source,'rc':p.returncode,'start_utc':start,'end_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'log_sha256':hashlib.sha256((A/(name+'.log')).read_bytes()).hexdigest()};(A/(name+'.result.json')).write_text(json.dumps(r,indent=2)+'\n');print(name,json.dumps(r))
