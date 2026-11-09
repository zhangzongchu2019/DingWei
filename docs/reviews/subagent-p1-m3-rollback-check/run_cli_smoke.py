from pathlib import Path
import os,subprocess,json,datetime,sqlite3,hashlib
import argparse
parser=argparse.ArgumentParser();parser.add_argument('--output',type=Path,required=True);args=parser.parse_args()
root=Path(__file__).resolve().parents[3];out=args.output.resolve();out.mkdir(mode=0o700);home=out/'home';home.mkdir(mode=0o700)
env={key:subprocess.check_output(['go','env',key],text=True).strip() for key in ['GOCACHE','GOMODCACHE']};env.update(PATH='/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin',HOME=str(home),TMPDIR='/tmp',GOPROXY='off',GOTOOLCHAIN='local')
def run(name,command,extra=None):
 e=dict(env);e.update(extra or {});start=datetime.datetime.now(datetime.timezone.utc).isoformat()
 with (out/(name+'.stdout')).open('wb') as f:p=subprocess.run(command,cwd=root,env=e,stdout=f,stderr=subprocess.STDOUT)
 r=dict(command=command,rc=p.returncode,start=start,end=datetime.datetime.now(datetime.timezone.utc).isoformat(),environment_names=sorted(e),uid=os.getuid());(out/(name+'.json')).write_text(json.dumps(r,indent=2)+'\n');return p.returncode
ready=out/'prepared.sqlite';pending=out/'pending.sqlite'
assert run('fixture',['go','test','./internal/store','-run','^TestSubAgentOfflineRollbackLegacyClaimer$','-count=1','-v'],{'M3_ROLLBACK_DB':str(ready)})==0
with sqlite3.connect(ready) as a, sqlite3.connect(pending) as b:a.backup(b)
with sqlite3.connect(pending) as db:
 assert db.execute('DELETE FROM sub_agent_rollback_delivery WHERE delivery_id=?',('unexecuted-a',)).rowcount==1
 db.commit()
for name,path,want in [('complete',ready,0),('incomplete',pending,1)]:
 before=hashlib.sha256(path.read_bytes()).hexdigest()
 rc=run(name,['go','run','./cmd/subagent-rollback','--hub-db',str(path),'--check-all'])
 assert rc==want,(name,rc)
 after=hashlib.sha256(path.read_bytes()).hexdigest();assert before==after
 body=(out/(name+'.stdout')).read_text().splitlines()[0];data=json.loads(body)
 assert data['pending']==want,(name,data)
 (out/(name+'-integrity.json')).write_text(json.dumps(dict(before_sha256=before,after_sha256=after,unchanged=True,pending=data['pending']),indent=2)+'\n')
 print(name+' rc='+str(rc)+' '+body,flush=True)
