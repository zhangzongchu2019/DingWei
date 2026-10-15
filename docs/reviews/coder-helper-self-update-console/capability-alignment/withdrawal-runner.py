from pathlib import Path
import tempfile,json,subprocess,os
root=Path.cwd();p=root/'internal/m8/console_helper_updates.go';s=p.read_text()
origin='''\tif r.Method != http.MethodGet && (h.PublicOrigin == "" || r.Header.Get("Origin") != h.PublicOrigin) {
\t\thelperConsoleError(w, 403, "origin_mismatch")
\t\treturn "", false
\t}
''';assert s.count(origin)==1
cases=[('origin',p,s.replace(origin,''),'^TestConsoleHelperUpdateHandlerOriginDefense$','unguarded handler origin='),('ownership',p,s.replace('err != nil || ag == nil || ag.OwnerLC != actor','err != nil || ag == nil'),'^TestConsoleHelperUpdateAuthorizationAndCatalogOnly$','foreign='),('catalog-input',p,s.replace('\td.DisallowUnknownFields()',''),'^TestConsoleHelperUpdateAuthorizationAndCatalogOnly$','injection status')]
p=root/'internal/m8/console_auth.go';s=p.read_text();old='''\tif h.Onboarding != nil {
\t\tpage += "<script>" + consoleHelperUpdatesJS + "</script>"
\t}
''';assert s.count(old)==1;cases.append(('page-asset',p,s.replace(old,''),'^TestConsoleHelperUpdateProductionAsset$','production page omitted'))
results=[];env=os.environ.copy();env.update(GOTMPDIR='/tmp',TMPDIR='/dev/shm')
for label,p,src,test,needle in cases:
 with tempfile.TemporaryDirectory(prefix='console-withdraw-',dir='/dev/shm') as td:
  q=Path(td)/p.name;q.write_text(src);o=Path(td)/'overlay.json';o.write_text(json.dumps({'Replace':{str(p):str(q)}}));log=Path('/tmp/coder-console-update-withdraw-'+label+'.log')
  with log.open('w') as out:r=subprocess.run(['go','test','-overlay='+str(o),'./internal/m8','-run',test,'-count=1','-timeout','2m'],env=env,stdout=out,stderr=subprocess.STDOUT)
  result=dict(case=label,rc=r.returncode,intended=needle in log.read_text(),log=str(log));results.append(result);print(result,flush=True)
Path('/tmp/coder-console-update-withdraw.json').write_text(json.dumps(results,indent=2)+'\n');assert all(r['rc']==1 and r['intended'] for r in results)
