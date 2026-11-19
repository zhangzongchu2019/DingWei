from pathlib import Path
import json,subprocess,datetime
A=Path(__file__).resolve().parent;w=Path('/data/ai-dev-data/wt-r62-groups-baseline206')
assert subprocess.check_output(['git','status','--porcelain'],cwd=w)==b''
e=json.loads((A/'env-anchor-go.json').read_text());e.update(HOME='/data/ai-dev-data/mafabh',TMPDIR='/data/ai-dev-data/mafabt',PYTHONPATH=str(w/'tools/sessionhelper'),SOURCE_COMMIT='206d41b26dab646c0e50a111eed5a45ccf7c72f6')
for k in ['HOME','TMPDIR']:Path(e[k]).mkdir(parents=True,exist_ok=True)
(A/'env-z3-baseline.json').write_text(json.dumps(e,indent=2)+'\n');name='z3-baseline-count20';cmd=['go','test','./internal/m8','-run','^TestSessionNameReconnectKeepsNameAndReleaseReuses$','-count=20','-p=1','-v']
(A/(name+'.command.json')).write_text(json.dumps({'source':e['SOURCE_COMMIT'],'cwd':str(w),'command':cmd,'env_file':'env-z3-baseline.json','note':'known-site contextual comparison only; not a substitute for the candidate full-package gate or frequency inference'},indent=2)+'\n')
start=datetime.datetime.now(datetime.timezone.utc).isoformat()
with (A/(name+'.log')).open('w') as out:p=subprocess.run(cmd,cwd=w,env=e,stdout=out,stderr=subprocess.STDOUT)
l=(A/(name+'.log')).read_text(errors='replace').splitlines();r={'source':e['SOURCE_COMMIT'],'start_utc':start,'end_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'rc':p.returncode,'pass':sum(x.startswith('--- PASS:') for x in l),'fail':sum(x.startswith('--- FAIL:') for x in l),'interrupted_lines':sum('interrupted (9)' in x for x in l),'porcelain':subprocess.check_output(['git','status','--porcelain'],cwd=w).decode()};(A/(name+'.result.json')).write_text(json.dumps(r,indent=2)+'\n');print(json.dumps(r))
