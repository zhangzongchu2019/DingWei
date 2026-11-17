import sqlite3,subprocess,json,datetime,hashlib,os
from pathlib import Path
R=Path('/data/ai-dev-data/local-run-artifacts/coder56solzzc/r60-rollback-0077');P=R/'private/forward-gc.db';snap=json.loads((R/'binary-roundtrip.json').read_text())['after_forward']
with sqlite3.connect(R/'private/hub.db') as src,sqlite3.connect(P) as dst:src.backup(dst)
with sqlite3.connect(P) as c:
 c.execute('DELETE FROM outbound_intent');c.executemany('INSERT INTO outbound_intent VALUES(?,?,?,?,?,?)',snap['fences']);c.execute('DELETE FROM session_delivery WHERE source_message_id=?',('e'*32,));c.commit()
 def counts():
  cut=(datetime.datetime.now(datetime.timezone.utc)-datetime.timedelta(days=7)).strftime('%Y-%m-%dT%H:%M:%S.%f')+'000Z'
  W=c.execute('SELECT count(*) FROM outbound_intent WHERE created_at>=?',(cut,)).fetchone()[0]
  P=c.execute("SELECT count(*) FROM outbound_intent o WHERE o.created_at<? AND EXISTS (SELECT 1 FROM session_delivery d WHERE d.id=o.delivery_id AND d.state NOT IN ('processed','dead','cancelled','legacy_received'))",(cut,)).fetchone()[0]
  E=c.execute('SELECT count(*) FROM outbound_intent WHERE created_at<?',(cut,)).fetchone()[0]-P
  return {'W':W,'P':P,'E':E}
 before=counts()
env={'HOME':'/tmp/r60-i','TMPDIR':'/tmp/r60-i','PATH':'/usr/local/bin:/usr/bin:/bin','GOCACHE':'/data/ai-dev-data/local-run-artifacts/coder56solzzc/go-cache','GOMODCACHE':'/tmp/busy-cf05/h-normal/go/pkg/mod','GOPROXY':'off','GOSUMDB':'off','GOTOOLCHAIN':'local','GOFLAGS':'-p=1'};cmd=['go','run','./cmd/r60rollbackprobe',str(P)];beg=datetime.datetime.now(datetime.timezone.utc).isoformat()
with (R/'forward-gc-final.log').open('w') as f:p=subprocess.run(cmd,cwd='/data/ai-dev-data/r60-rollback-new-a311',env=env,stdout=f,stderr=subprocess.STDOUT)
with sqlite3.connect(P) as c:after=counts()
v={'base_source':'a311aaf3beea90262c05ca2c316064b8a8856d06','overlay':'local diagnostic cmd only, not release source','argv':cmd,'env':env,'uid':os.getuid(),'start_utc':beg,'end_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'rc':p.returncode,'raw_sha256':hashlib.sha256((R/'forward-gc-final.log').read_bytes()).hexdigest(),'probe_sha256':hashlib.sha256(Path('/data/ai-dev-data/r60-rollback-new-a311/cmd/r60rollbackprobe/main.go').read_bytes()).hexdigest(),'before':before,'after':after}
(R/'forward-gc-final.json').write_text(json.dumps(v,indent=2)+'\n');assert p.returncode==0 and before==dict(W=1,P=1,E=1) and after==dict(W=2,P=1,E=0);print('FORWARD_GC_OK',before,after)
