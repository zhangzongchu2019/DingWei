from pathlib import Path
import subprocess,json,sys,datetime
A=Path(__file__).resolve().parent;e=json.loads((A/'env.json').read_text());name=sys.argv[1];cmd=sys.argv[2:];start=datetime.datetime.now(datetime.timezone.utc).isoformat();w='/data/ai-dev-data/mcp-dcr'
(A/(name+'.command.json')).write_text(json.dumps({'command':cmd,'cwd':w,'source':'development dirty products; not a frozen gate'},indent=2)+'\n')
with (A/(name+'.log')).open('w') as f:p=subprocess.run(cmd,cwd=w,env=e,stdout=f,stderr=subprocess.STDOUT)
r={'rc':p.returncode,'start_utc':start,'end_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()};(A/(name+'.result.json')).write_text(json.dumps(r,indent=2)+'\n');print(name,json.dumps(r))
