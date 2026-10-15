from pathlib import Path
import tempfile,subprocess,json
root=Path.cwd();cases=[]
p=root/'cmd/workpulse/provision_retired.go';s=p.read_text();changed=s.replace('import "net/http"','import ("net/http";"os";"path/filepath")')
# Restore a real anonymous file read at the retired artifact path; no fake response.
changed=changed.replace('\thttp.NotFound(w, r)','\tif r.URL.Path == "/dl/artifact.tar.gz" { http.ServeFile(w,r,filepath.Join(os.Getenv("WP_PROVISION_DL_DIR"),"artifact.tar.gz"));return }\n\thttp.NotFound(w, r)')
cases.append(('download',p,changed,'./cmd/workpulse','^TestProvisionDownloadRetiredEvenWhenArtifactExists$','retired GET /dl/artifact.tar.gz'))
p=root/'internal/admin/admin.go';s=p.read_text()
a='''\tif strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type"))), "multipart/") {
\t\twritePublicUploadRetired(w)
\t\treturn
\t}
''';assert s.count(a)==1
cases.append(('upload-before-read',p,s.replace(a,''),'./internal/admin','^TestAdminPublicProvisionUploadRetiredBeforeBodyOrFileWrite$','reads='))
a='''\tif r.FormValue("op") == "package_self" {
\t\twritePublicUploadRetired(w)
\t\treturn
\t}
''';assert s.count(a)==1
cases.append(('package',p,s.replace(a,''),'./internal/admin','^TestAdminPublicProvisionUploadRetiredBeforeBodyOrFileWrite$','online packaging status='))
a='<p><button name=op value=send>下发已有制品地址</button></p>';assert s.count(a)==1
cases.append(('upload-ui',p,s.replace(a,a+'<input type=file name=artifact>'),'./internal/admin','^TestAdminProvisionPageRequiresAuthAndShowsOnlineSessions$','retired public producer still visible'))
results=[]
for label,p,source,pkg,test,needle in cases:
 with tempfile.TemporaryDirectory(prefix='coder-retired-withdraw-') as td:
  replacement=Path(td)/p.name;replacement.write_text(source);overlay=Path(td)/'overlay.json';overlay.write_text(json.dumps({'Replace':{str(p):str(replacement)}}))
  log=Path('/tmp/coder-self-update-retire-withdraw-'+label+'.log')
  with log.open('w') as out:r=subprocess.run(['go','test','-overlay='+str(overlay),pkg,'-run',test,'-count=1','-timeout','3m'],stdout=out,stderr=subprocess.STDOUT)
  result=dict(case=label,rc=r.returncode,intended=needle in log.read_text(),log=str(log));results.append(result);print(result,flush=True)
Path('/tmp/coder-self-update-retire-withdraw.json').write_text(json.dumps(results,indent=2)+'\n');assert all(x['rc']==1 and x['intended'] for x in results)
