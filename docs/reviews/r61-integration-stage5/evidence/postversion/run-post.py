from pathlib import Path
import subprocess,sys,json,datetime
P=Path(__file__).resolve().parent
p=subprocess.run([sys.executable,str(P/'run.py'),'go','m8-version-full1','go','test','./internal/m8','-count=1','-p=1','-timeout=60m','-v']);q=subprocess.run([sys.executable,str(P/'run-go-policy.py')]);assert q.returncode==0,'postversion Go policy STOP'
p=subprocess.run([sys.executable,str(P/'run.py'),'canonical','canonical-version','./build.sh','--test-only']);assert p.returncode==0,'postversion canonical STOP'
(P/'post-acceptance.json').write_text(json.dumps({'source':json.loads((P/'env-go.json').read_text())['SOURCE_COMMIT'],'all_serial':True,'passed':True,'go_policy':json.loads((P/'go-policy.json').read_text()),'end_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()},indent=2)+'\n');print('POST_VERSION_ACCEPTED')
