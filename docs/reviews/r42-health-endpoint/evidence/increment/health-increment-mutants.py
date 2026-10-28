from pathlib import Path
import tempfile,subprocess,json,hashlib,time,os
from datetime import datetime,timezone
w=Path('/data/ai-dev-data/r42-health-endpoint');out=Path('/data/ai-dev-data/r42-dev-evidence');results=[]
with tempfile.TemporaryDirectory(prefix='r42-health-version-mutant-') as td:
 root=Path(td);v=root/'view-service';v.mkdir();(root/'VERSION').write_bytes((w/'VERSION').read_bytes())
 for name in ('view_service.py','view_page_store.py','test_health_auth.py'):(v/name).write_bytes((w/'view-service'/name).read_bytes())
 p=v/'view_service.py';source=p.read_text();assert source.count('        return version\n')==1;p.write_text(source.replace('        return version\n','        return "0.2.3.15"\n'))
 cases=[('version-source-literal',['/tmp/coder-0232-locked-venv/bin/python3','-B','-m','unittest','-v','test_health_auth.HealthAuthTest.test_version_follows_deployment_without_source_change'],v,{'PATH':'/usr/bin:/bin','HOME':td,'LANG':'C.UTF-8'})]
 terminal=w/'internal/m8/terminal.go';copy=root/'terminal.go';text=terminal.read_text();old='publicPath := path.Clean(strings.TrimPrefix(r.URL.Path, "/view/"+sessionName))';assert old in text;copy.write_text(text.replace(old,'publicPath := strings.TrimPrefix(r.URL.Path, "/view/"+sessionName); _ = path.Clean'))
 overlay=root/'overlay.json';overlay.write_text(json.dumps({'Replace':{str(terminal):str(copy)}}))
 cases.append(('proxy-without-clean',['/usr/bin/go','test','-overlay',str(overlay),'./internal/m8','-run','^TestHealthAuthViewProxyNeverReachesInternalUpstream$','-count=1','-v','-timeout=1m'],w,{'PATH':'/usr/bin:/bin','HOME':td,'LANG':'C.UTF-8','GOCACHE':'/home/dev/.cache/go-build','GOPATH':'/home/dev/go','GOMODCACHE':'/home/dev/go/pkg/mod'}))
 for label,cmd,cwd,env in cases:
  start=datetime.now(timezone.utc).isoformat();t=time.monotonic()
  r=subprocess.run(cmd,cwd=cwd,env=env,capture_output=True,text=True,timeout=90);log=out/('health-increment-'+label+'.log');log.write_text(r.stdout+r.stderr)
  criterion=('9.8.7.6' in r.stderr and '0.2.3.15' in r.stderr) if label=='version-source-literal' else ('/view/test//internal/health/auth status 200' in r.stdout)
  results.append({'label':label,'command':cmd,'utc':start,'rc':r.returncode,'seconds':round(time.monotonic()-t,3),'criterion':criterion,'log_sha256':hashlib.sha256(log.read_bytes()).hexdigest()})
  assert r.returncode==1 and criterion,results[-1]
(out/'health-increment-mutants.json').write_text(json.dumps(results,indent=2)+'\n');print(json.dumps(results,indent=2))
