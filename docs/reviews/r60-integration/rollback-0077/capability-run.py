import subprocess,os,json,hashlib,datetime
from pathlib import Path
R=Path('/data/ai-dev-data/local-run-artifacts/coder56solzzc/r60-rollback-0077');out=[]
for name,w,old in [('old-capability','/data/ai-dev-data/r60-rollback-old-110','1'),('new-capability','/data/ai-dev-data/r60-rollback-new-a311','0')]:
 env={'HOME':'/tmp/r60-i','TMPDIR':'/tmp/r60-i','PATH':'/usr/local/bin:/usr/bin:/bin','GOCACHE':'/data/ai-dev-data/local-run-artifacts/coder56solzzc/go-cache','GOMODCACHE':'/tmp/busy-cf05/h-normal/go/pkg/mod','GOPROXY':'off','GOSUMDB':'off','GOTOOLCHAIN':'local','GOFLAGS':'-p=1','ROLLBACK_DB_TEMPLATE':str(R/'private/hub.db'),'ROLLBACK_EXPECT_OLD':old}
 cmd=['go','test','./internal/m8','-run','^TestRollback0077CapabilityProbe$','-count=1','-v','-timeout','3m'];beg=datetime.datetime.now(datetime.timezone.utc).isoformat()
 with (R/(name+'.log')).open('w') as f:p=subprocess.run(cmd,cwd=w,env=env,stdout=f,stderr=subprocess.STDOUT)
 out.append(dict(name=name,source=subprocess.check_output(['git','rev-parse','HEAD'],cwd=w,text=True).strip(),probe_sha256=hashlib.sha256(Path('/tmp/r60-rollback-capability-probe.go').read_bytes()).hexdigest(),argv=cmd,uid=os.getuid(),env=env,start_utc=beg,end_utc=datetime.datetime.now(datetime.timezone.utc).isoformat(),rc=p.returncode,raw_sha256=hashlib.sha256((R/(name+'.log')).read_bytes()).hexdigest()));(R/'capability-probe.json').write_text(json.dumps(out,indent=2)+'\n');print(name,p.returncode,flush=True)
 if p.returncode:raise SystemExit(p.returncode)
